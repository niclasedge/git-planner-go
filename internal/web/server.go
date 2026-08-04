// Package web serves the dashboard. Requests never touch the GitHub API: they
// read whatever the hub last refreshed, so a page render is a template execution
// and nothing else.
package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/niclasedge/git-planner-go/internal/config"
	"github.com/niclasedge/git-planner-go/internal/gh"
	"github.com/niclasedge/git-planner-go/internal/hub"
	"github.com/niclasedge/git-planner-go/internal/panel"
	"github.com/niclasedge/git-planner-go/internal/store"
)

type Server struct {
	cfg    *config.Config
	hub    *hub.Hub
	panels *panel.Set
	api    *gh.Client
	store  *store.Store
	log    *slog.Logger

	tmpl *template.Template
	nav  []navItem
	mux  *http.ServeMux
}

type navItem struct {
	Title  string
	Slug   string
	URL    string
	Type   string
	Active bool
}

func New(cfg *config.Config, h *hub.Hub, p *panel.Set, api *gh.Client, st *store.Store, log *slog.Logger) (*Server, error) {
	s := &Server{cfg: cfg, hub: h, panels: p, api: api, store: st, log: log}

	// render dispatches to a template by name, which is what lets a widget pick
	// its own markup from its Kind() without a switch in the layout.
	fm := template.FuncMap{}
	for k, v := range funcs {
		fm[k] = v
	}
	fm["render"] = func(name string, data any) (template.HTML, error) {
		var buf bytes.Buffer
		if err := s.tmpl.ExecuteTemplate(&buf, name, data); err != nil {
			return "", fmt.Errorf("rendering %s: %w", name, err)
		}
		return template.HTML(buf.String()), nil
	}
	fm["widget"] = func(w panel.Widget) (template.HTML, error) {
		var buf bytes.Buffer
		if err := s.tmpl.ExecuteTemplate(&buf, "widget-"+w.Kind(), w); err != nil {
			return "", fmt.Errorf("rendering widget %s: %w", w.Kind(), err)
		}
		return template.HTML(buf.String()), nil
	}

	tmpl, err := template.New("").Funcs(fm).ParseFS(templateFS, "templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("parsing templates: %w", err)
	}
	s.tmpl = tmpl

	for _, p := range cfg.Pages {
		s.nav = append(s.nav, navItem{
			Title: p.Title,
			Slug:  p.Slug,
			Type:  p.Type,
			URL:   pageURL(p),
		})
	}

	s.routes()
	return s, nil
}

func pageURL(p config.Page) string {
	switch p.Type {
	case "issues":
		return "/issues"
	case "actions":
		return "/actions"
	case "planner":
		return "/planner"
	default:
		return "/p/" + p.Slug
	}
}

func (s *Server) routes() {
	m := http.NewServeMux()

	m.Handle("GET /static/"+staticHash+"/", staticHandler())

	m.HandleFunc("GET /{$}", s.handleHome)
	m.HandleFunc("GET /issues", s.handleIssuesPage)
	m.HandleFunc("GET /actions", s.handleActionsPage)
	m.HandleFunc("GET /planner", s.handlePlannerPage)
	m.HandleFunc("GET /p/{slug}", s.handleWidgetPage)

	// HTMX fragments. Same data, no layout — this is what filters and polling
	// swap in.
	m.HandleFunc("GET /htmx/issues", s.handleIssuesFragment)
	m.HandleFunc("GET /htmx/actions", s.handleActionsFragment)
	m.HandleFunc("GET /htmx/planner/detail", s.handlePlannerDetail)
	m.HandleFunc("GET /htmx/widget/{id}", s.handleWidgetFragment)
	m.HandleFunc("GET /htmx/widget/{id}/log/{task}", s.handleSemaphoreLog)

	// The edit path. GET renders a form, POST writes to GitHub — the only requests
	// in the app that leave the read cache behind and cost rate limit.
	m.HandleFunc("GET /htmx/planner/edit", s.handlePlannerEdit)
	m.HandleFunc("POST /htmx/planner/edit", s.handlePlannerSave)
	m.HandleFunc("GET /htmx/planner/comments", s.handlePlannerComments)
	m.HandleFunc("POST /htmx/planner/comments", s.handlePlannerComment)

	m.HandleFunc("POST /refresh/{what}", s.handleRefresh)
	m.HandleFunc("GET /api/status", s.handleStatus)

	// The screenshot job asks what to photograph and drops the files where
	// handleShot reads them. The app never runs a browser itself.
	m.HandleFunc("GET /api/shots", s.handleShotTargets)
	m.HandleFunc("GET /shots/{name}", s.handleShot)
	m.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok\n"))
	})

	s.mux = m
}

func (s *Server) Handler() http.Handler { return s.logging(s.mux) }

func (s *Server) logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		s.log.Debug("request", "method", r.Method, "path", r.URL.Path, "took", time.Since(start))
	})
}

// pageData is what every full page render receives. The payload fields are
// mutually exclusive; the body template reads the one it needs.
type pageData struct {
	Title      string
	Nav        []navItem
	ActiveSlug string
	Body       string
	// Full swaps the centred column for a viewport-filling layout. Only the
	// planner wants it; every other page reads better in a fixed width.
	Full bool

	Issues     *issueData
	Actions    *actionData
	Planner    *plannerData
	WidgetPage *panel.Page
	// Beads is set when the requested widget page is dedicated to a beads
	// widget: that page renders full-screen like the planner, with the widget's
	// panes filling the viewport instead of a card in the centred column.
	Beads *panel.Beads
}

func (s *Server) render(w http.ResponseWriter, data pageData) {
	for i := range s.nav {
		s.nav[i].Active = s.nav[i].Slug == data.ActiveSlug
	}
	data.Nav = s.nav

	var buf bytes.Buffer
	if err := s.tmpl.ExecuteTemplate(&buf, "layout", data); err != nil {
		s.log.Error("render failed", "page", data.ActiveSlug, "err", err)
		http.Error(w, "template error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	buf.WriteTo(w)
}

// fragment renders a named template without the layout, for HTMX swaps.
func (s *Server) fragment(w http.ResponseWriter, name string, data any) {
	var buf bytes.Buffer
	if err := s.tmpl.ExecuteTemplate(&buf, name, data); err != nil {
		s.log.Error("fragment failed", "template", name, "err", err)
		http.Error(w, "template error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	buf.WriteTo(w)
}

func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	if len(s.cfg.Pages) == 0 {
		http.Error(w, "no pages configured", http.StatusInternalServerError)
		return
	}
	first := s.cfg.Pages[0]
	switch first.Type {
	case "actions":
		s.handleActionsPage(w, r)
	case "planner":
		s.handlePlannerPage(w, r)
	case "widgets":
		s.renderWidgetPage(w, first.Slug)
	default:
		s.handleIssuesPage(w, r)
	}
}

func (s *Server) handleWidgetPage(w http.ResponseWriter, r *http.Request) {
	s.renderWidgetPage(w, r.PathValue("slug"))
}

func (s *Server) renderWidgetPage(w http.ResponseWriter, slug string) {
	p := s.panels.Page(slug)
	if p == nil {
		http.NotFound(w, &http.Request{})
		return
	}
	// A page dedicated to a beads widget gets the planner treatment: the three
	// panes want the viewport, so the card chrome is skipped and the widget is
	// rendered directly into the full-height .panes grid. The scheduler keeps
	// it fresh either way.
	for _, col := range p.Columns {
		for _, wd := range col.Widgets {
			if b, ok := wd.(*panel.Beads); ok {
				s.render(w, pageData{
					Title:      p.Title,
					ActiveSlug: slug,
					Full:       true,
					Body:       "page-beads",
					Beads:      b,
				})
				return
			}
		}
	}
	s.render(w, pageData{
		Title:      p.Title,
		ActiveSlug: slug,
		Body:       "page-widgets",
		WidgetPage: p,
	})
}

func (s *Server) handleWidgetFragment(w http.ResponseWriter, r *http.Request) {
	wd := s.widgetByID(r.PathValue("id"))
	if wd == nil {
		http.NotFound(w, r)
		return
	}
	// Refresh on demand: the user asked for this one specifically.
	wd.Update(r.Context())
	s.fragment(w, "widget-frame", wd)
}

func (s *Server) widgetByID(id string) panel.Widget {
	for _, p := range s.panels.Pages {
		for _, col := range p.Columns {
			for _, wd := range col.Widgets {
				if fmt.Sprint(wd.ID()) == id {
					return wd
				}
			}
		}
	}
	return nil
}

// handleSemaphoreLog serves the full output of one run, lazily: the rows expand
// on click and 20 logs per refresh would be 20 requests nobody asked for.
func (s *Server) handleSemaphoreLog(w http.ResponseWriter, r *http.Request) {
	sw, ok := s.widgetByID(r.PathValue("id")).(*panel.Semaphore)
	if !ok {
		http.NotFound(w, r)
		return
	}
	taskID, err := strconv.Atoi(r.PathValue("task"))
	if err != nil {
		http.NotFound(w, r)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()

	v := sw.Log(ctx, taskID)
	if v == nil {
		http.NotFound(w, r)
		return
	}
	s.fragment(w, "semaphore-log", v)
}

func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	what := r.PathValue("what")
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	// Refresh inline rather than just marking it due: every request is
	// conditional, so this is fast, and the response can show the result.
	s.hub.RefreshNow(ctx, what)

	switch what {
	case "issues":
		s.handleIssuesFragment(w, r)
	case "actions":
		s.handleActionsFragment(w, r)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

type statusResponse struct {
	Tokens []gh.TokenStatus `json:"tokens"`
	Cache  struct {
		Entries int   `json:"entries"`
		Bytes   int64 `json:"bytes"`
	} `json:"cache"`
	Sections map[string]sectionStatus `json:"sections"`
}

type sectionStatus struct {
	UpdatedAt time.Time `json:"updated_at"`
	Error     string    `json:"error,omitempty"`
	Items     int       `json:"items"`
	CacheHits int       `json:"cache_hits_304"`
	Fetched   int       `json:"fetched_200"`
	// Delta and Polls are the incremental issue poll: how many issues the last one
	// merged, and what it cost. This is where the five-minute refresh shows up —
	// the two numbers above belong to the hourly sweep.
	Delta int `json:"last_poll_changed,omitempty"`
	Polls int `json:"last_poll_requests,omitempty"`
}

// handleStatus is the instrument for the design's core claim. Watch
// cache_hits_304 climb while remaining stays flat: that is a refresh costing
// nothing.
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	resp := statusResponse{
		Tokens:   s.api.Status(),
		Sections: map[string]sectionStatus{},
	}
	if entries, bytes, err := s.store.Stats(); err == nil {
		resp.Cache.Entries = entries
		resp.Cache.Bytes = bytes
	}

	iv, iAt, iErr := s.hub.Issues()
	is := sectionStatus{UpdatedAt: iAt, Items: len(iv.Issues), CacheHits: iv.CacheHits, Fetched: iv.Fetched,
		Delta: iv.Delta, Polls: iv.Polls}
	if iErr != nil {
		is.Error = iErr.Error()
	}
	resp.Sections["issues"] = is

	rv, rAt, rErr := s.hub.Runs()
	rs := sectionStatus{UpdatedAt: rAt, Items: len(rv.Repos), CacheHits: rv.CacheHits, Fetched: rv.Fetched}
	if rErr != nil {
		rs.Error = rErr.Error()
	}
	resp.Sections["actions"] = rs

	// Counts has no cache_hits column on purpose: GraphQL is POST, so every sweep
	// costs. Fetched carries the query count instead.
	cv, cAt, cErr := s.hub.Counts()
	cs := sectionStatus{UpdatedAt: cAt, Items: len(cv.Counts), Fetched: cv.Queries}
	if cErr != nil {
		cs.Error = cErr.Error()
	}
	resp.Sections["counts"] = cs

	repos, repoAt, repoErr := s.hub.Repos()
	ps := sectionStatus{UpdatedAt: repoAt, Items: len(repos)}
	if repoErr != nil {
		ps.Error = repoErr.Error()
	}
	resp.Sections["repos"] = ps

	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.Encode(resp)
}
