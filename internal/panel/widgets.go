package panel

import (
	"context"
	"crypto/tls"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/niclasedge/git-planner-go/internal/config"
)

// ---------------------------------------------------------------------------
// monitor — HTTP uptime checks
// ---------------------------------------------------------------------------

type Monitor struct {
	Base `yaml:",inline"`
	// Sites is the flat form, for a widget that needs no grouping. Groups is
	// the grouped one — "runs here" and "runs on the server" are different
	// questions and a single list answers neither.
	Sites  []*Site      `yaml:"sites"`
	Groups []*SiteGroup `yaml:"groups"`
	// FailuresOnly hides healthy sites, which is what you want once the list
	// gets long.
	FailuresOnly bool `yaml:"failures-only"`

	// PublicInterval is how often the public addresses are re-checked. Far less
	// often than the internal probe: a Traefik route changes when someone
	// changes it, and these requests leave the machine and hit the service from
	// the outside.
	PublicInterval config.Duration `yaml:"public-interval"`

	// shotsDir is where an external screenshot job drops its PNGs. Injected
	// after the widgets are built, so it is configured in one place rather than
	// repeated per widget.
	shotsDir string
}

type SiteGroup struct {
	Title string  `yaml:"title"`
	Sites []*Site `yaml:"sites"`
	// External marks services that are not ours. "Is this exposed to the
	// internet" is a question about our own hosts; answering it for github.com
	// would be labelling someone else's deployment, so these get no badge.
	External bool `yaml:"external"`
}

// Visible is the rows to render. A group whose sites are all up disappears
// entirely under failures-only, heading included.
func (g *SiteGroup) Visible(failuresOnly bool) []*Site {
	if !failuresOnly {
		return g.Sites
	}
	var out []*Site
	for _, s := range g.Sites {
		if !s.Up() {
			out = append(out, s)
		}
	}
	return out
}

type Site struct {
	Title string `yaml:"title"`
	URL   string `yaml:"url"`
	// CheckURL is probed instead of URL when the link target and the health
	// endpoint differ.
	CheckURL string `yaml:"check-url"`
	// PublicURL is the same service as the world sees it — the Traefik route,
	// not the tailnet address. Set it and the widget answers "is this exposed",
	// which for a box that also serves things over Tailscale only is not
	// something the internal check can tell you. Left empty the service counts
	// as internal, matching services.yml: no domain, no public route.
	PublicURL     string          `yaml:"public-url"`
	Icon          string          `yaml:"icon"`
	Timeout       config.Duration `yaml:"timeout"`
	AllowInsecure bool            `yaml:"allow-insecure"`

	// Results, written by Update and read by the template. The widget's lock
	// covers these.
	Status  int
	Latency time.Duration
	Error   string
	// PublicStatus and PublicError are the result of probing PublicURL, and
	// publicAt when that happened — the public route changes about as often as
	// Traefik is reconfigured, so it is not worth re-checking every minute.
	PublicStatus int
	PublicError  string
	publicAt     time.Time
	// shot is the screenshot file name plus a cache-busting stamp, or empty.
	shot string
	// external comes from the group: someone else's service, so the exposure
	// question does not apply to it.
	external bool
	// group is the enclosing group's title, set by Init. Part of the slug because
	// the same service name appears in more than one group — "SearXNG" runs both
	// here and on the server, and one file name for both would show the wrong
	// picture for one of them.
	group string
}

func (s *Site) target() string {
	if s.CheckURL != "" {
		return s.CheckURL
	}
	return s.URL
}

func (s *Site) Up() bool {
	return s.Error == "" && s.Status >= 200 && s.Status < 400
}

// Slug is the site's file name for a screenshot, group included. Derived from
// the titles rather than the URL, because a port number is not a name — and
// stable across a moved service, which a URL is not.
func (s *Site) Slug() string {
	name := slugify(s.Title)
	if g := slugify(s.group); g != "" {
		return g + "-" + name
	}
	return name
}

// slugify reduces a title to a file name: lowercase, ASCII, dashes between
// words. Umlauts are transliterated rather than dropped, or "Größe" and "Grosse"
// would collide.
func slugify(title string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(title) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		case r == 'ä':
			b.WriteString("ae")
			dash = false
		case r == 'ö':
			b.WriteString("oe")
			dash = false
		case r == 'ü':
			b.WriteString("ue")
			dash = false
		case r == 'ß':
			b.WriteString("ss")
			dash = false
		default:
			// Collapse runs of punctuation and spaces into one dash, and never
			// start with one.
			if !dash && b.Len() > 0 {
				b.WriteByte('-')
				dash = true
			}
		}
	}
	return strings.TrimRight(b.String(), "-")
}

// Shot is the URL of this site's screenshot, empty when there is none yet. Set
// by the widget from what is actually on disk, so a service that has never been
// reachable simply has no thumbnail.
func (s *Site) Shot() string {
	if s.shot == "" {
		return ""
	}
	return "/shots/" + s.shot
}

// Exposure says how the world can reach this service:
//
//	""        not our service — the question does not apply
//	internal  no public route configured — tailnet only
//	public    answers on its public address, no authentication in the way
//	guarded   answers publicly but demands credentials (401/403)
//	broken    a public route is configured but does not answer
//
// "guarded" is worth its own state rather than being folded into public: both
// mean the port is exposed, but only one of them means anyone can walk in.
func (s *Site) Exposure() string {
	if s.PublicURL == "" {
		if s.external {
			return ""
		}
		return "internal"
	}
	switch {
	case s.PublicError != "":
		return "broken"
	case s.PublicStatus == http.StatusUnauthorized, s.PublicStatus == http.StatusForbidden:
		return "guarded"
	case s.PublicStatus >= 200 && s.PublicStatus < 400:
		return "public"
	case s.PublicStatus == 0:
		// Configured but not probed yet — do not claim it is broken.
		return "internal"
	default:
		return "broken"
	}
}

// ExposureLabel is Exposure in the words the widget shows.
func (s *Site) ExposureLabel() string {
	switch s.Exposure() {
	case "public":
		return "öffentlich"
	case "guarded":
		return "öffentlich · Auth"
	case "broken":
		return "Route defekt"
	default:
		return "intern"
	}
}

// Outcome mirrors the vocabulary the Actions page uses, so both pages can share
// the same status colours.
func (s *Site) Outcome() string {
	switch {
	case s.Error != "":
		return "failure"
	case s.Status == 0:
		return "queued"
	case s.Up():
		return "success"
	default:
		return "failure"
	}
}

func (m *Monitor) Kind() string { return "monitor" }

func (m *Monitor) Init() error {
	// The flat form is the grouped one with a single untitled group, so
	// everything below only has to deal with groups.
	if len(m.Sites) > 0 {
		m.Groups = append([]*SiteGroup{{Sites: m.Sites}}, m.Groups...)
	}
	m.Sites = nil
	for _, g := range m.Groups {
		for _, s := range g.Sites {
			s.group = g.Title
			s.external = g.External
		}
		m.Sites = append(m.Sites, g.Sites...)
	}

	if len(m.Sites) == 0 {
		return fmt.Errorf("monitor needs at least one site")
	}
	if m.WTitle == "" {
		m.WTitle = "Monitor"
	}
	for _, s := range m.Sites {
		if s.URL == "" && s.CheckURL == "" {
			return fmt.Errorf("site %q has no url", s.Title)
		}
		if s.Title == "" {
			s.Title = s.URL
		}
		if s.Timeout.Std() <= 0 {
			s.Timeout = config.Duration(5 * time.Second)
		}
	}
	if m.PublicInterval.Std() <= 0 {
		m.PublicInterval = config.Duration(15 * time.Minute)
	}
	return nil
}

func (m *Monitor) Update(ctx context.Context) {
	results := make([]struct {
		status  int
		latency time.Duration
		err     string
		// public is only filled for the sites due a public check this round; the
		// others keep what they had.
		publicDone   bool
		publicStatus int
		publicErr    string
	}, len(m.Sites))

	now := time.Now()
	var wg sync.WaitGroup
	for i, s := range m.Sites {
		wg.Add(1)
		go func(i int, s *Site) {
			defer wg.Done()
			status, latency, err := probe(ctx, s)
			results[i].status = status
			results[i].latency = latency
			if err != nil {
				results[i].err = err.Error()
			}

			if !m.publicDue(s, now) {
				return
			}
			results[i].publicDone = true
			code, _, perr := probeURL(ctx, s, s.PublicURL)
			results[i].publicStatus = code
			if perr != nil {
				results[i].publicErr = perr.Error()
			}
		}(i, s)
	}
	wg.Wait()

	// Screenshots are looked up here rather than while rendering: a stat per site
	// per page view would put the filesystem in the request path for no reason.
	shots := m.lookupShots()

	m.mu.Lock()
	for i, r := range results {
		m.Sites[i].Status = r.status
		m.Sites[i].Latency = r.latency
		m.Sites[i].Error = r.err
		m.Sites[i].shot = shots[i]
		if r.publicDone {
			m.Sites[i].PublicStatus = r.publicStatus
			m.Sites[i].PublicError = r.publicErr
			m.Sites[i].publicAt = now
		}
	}
	m.mu.Unlock()

	// A monitor never reports an error upward: a site being down is the data,
	// not a failure of the widget.
	m.done(nil)
}

// publicDue reports whether this site's public address should be probed now.
// Reading publicAt without the lock is safe here: Update is the only writer and
// the scheduler never runs two of them at once.
func (m *Monitor) publicDue(s *Site, now time.Time) bool {
	if s.PublicURL == "" {
		return false
	}
	return now.Sub(s.publicAt) >= m.PublicInterval.Std()
}

// lookupShots returns each site's screenshot file name, with the file's
// modification time appended as a query. Browsers cache these aggressively — the
// URL has to change when the picture does, or a day-old shot stays on screen
// forever.
func (m *Monitor) lookupShots() []string {
	out := make([]string, len(m.Sites))
	if m.shotsDir == "" {
		return out
	}
	for i, s := range m.Sites {
		name := s.Slug() + ".png"
		info, err := os.Stat(filepath.Join(m.shotsDir, name))
		if err != nil || info.IsDir() || info.Size() == 0 {
			continue
		}
		out[i] = name + "?v=" + strconv.FormatInt(info.ModTime().Unix(), 10)
	}
	return out
}

// ShotTarget is one screenshot job: where to point the browser and what to call
// the file.
type ShotTarget struct {
	Slug string `json:"slug"`
	URL  string `json:"url"`
}

// ShotTargets is the sites worth photographing right now — reachable ones with a
// browsable URL. A service that is down would only yield a picture of an error
// page, and overwriting yesterday's working screenshot with that loses the one
// thing the thumbnail is for.
func (m *Monitor) ShotTargets() []ShotTarget {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var out []ShotTarget
	for _, s := range m.Sites {
		if s.URL == "" || !s.Up() {
			continue
		}
		out = append(out, ShotTarget{Slug: s.Slug(), URL: s.URL})
	}
	return out
}

// insecureTransport is shared by every allow-insecure site, so we do not build a
// TLS config per check.
var (
	insecureOnce      sync.Once
	insecureTransport *http.Transport
)

func probe(ctx context.Context, s *Site) (int, time.Duration, error) {
	return probeURL(ctx, s, s.target())
}

// probeURL is the check itself, with the site supplying only the timeout and the
// TLS policy — so the public address goes through exactly the same code as the
// internal one and cannot drift from it.
func probeURL(ctx context.Context, s *Site, url string) (int, time.Duration, error) {
	ctx, cancel := context.WithTimeout(ctx, s.Timeout.Std())
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, 0, err
	}
	req.Header.Set("User-Agent", "git-planner-go/monitor")

	client := &http.Client{Timeout: s.Timeout.Std()}
	if s.AllowInsecure {
		insecureOnce.Do(func() {
			insecureTransport = &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			}
		})
		client.Transport = insecureTransport
	}

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return 0, time.Since(start), err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<10))

	return resp.StatusCode, time.Since(start), nil
}

// ---------------------------------------------------------------------------
// bookmarks — static links
// ---------------------------------------------------------------------------

type Bookmarks struct {
	Base   `yaml:",inline"`
	Groups []BookmarkGroup `yaml:"groups"`
}

type BookmarkGroup struct {
	Title string     `yaml:"title"`
	Color string     `yaml:"color"`
	Links []Bookmark `yaml:"links"`
}

type Bookmark struct {
	Title       string `yaml:"title"`
	URL         string `yaml:"url"`
	Description string `yaml:"description"`
	Icon        string `yaml:"icon"`
}

func (b *Bookmarks) Kind() string { return "bookmarks" }

func (b *Bookmarks) Init() error {
	if len(b.Groups) == 0 {
		return fmt.Errorf("bookmarks needs at least one group")
	}
	if b.WTitle == "" {
		b.WTitle = "Bookmarks"
	}
	return nil
}

// Update is a no-op: static config, nothing to fetch. It still marks itself
// done so the scheduler stops looking at it.
func (b *Bookmarks) Update(context.Context) { b.done(nil) }

// ---------------------------------------------------------------------------
// iframe — embed anything
// ---------------------------------------------------------------------------

type IFrame struct {
	Base   `yaml:",inline"`
	Source string `yaml:"source"`
	Height int    `yaml:"height"`
}

func (f *IFrame) Kind() string { return "iframe" }

func (f *IFrame) Init() error {
	if f.Source == "" {
		return fmt.Errorf("iframe needs a source")
	}
	if f.Height <= 0 {
		f.Height = 400
	}
	return nil
}

func (f *IFrame) Update(context.Context) { f.done(nil) }

// ---------------------------------------------------------------------------
// html — raw markup from the config
// ---------------------------------------------------------------------------

type HTML struct {
	Base   `yaml:",inline"`
	Source string `yaml:"source"`
}

func (h *HTML) Kind() string { return "html" }

func (h *HTML) Init() error {
	if h.Source == "" {
		return fmt.Errorf("html widget needs a source")
	}
	return nil
}

func (h *HTML) Update(context.Context) { h.done(nil) }

// Content is injected unescaped. That is deliberate: config.yaml is a local file
// the operator wrote, so it is as trusted as the binary itself.
func (h *HTML) Content() template.HTML { return template.HTML(h.Source) }
