package web

import (
	"net/http"
	"strings"
	"time"

	"github.com/niclasedge/git-planner-go/internal/gh"
)

type actionFilter struct {
	Repo   string
	Token  string
	Status string // all | success | failure | running
}

func (f actionFilter) Any() bool {
	return f.Repo != "" || f.Token != "" || (f.Status != "" && f.Status != "all")
}

type actionData struct {
	Repos  []gh.RepoRuns
	Filter actionFilter

	RepoNames []string
	Tokens    []tokenOption

	UpdatedAt time.Time
	CacheHits int
	Fetched   int
	Errors    []string
	Warning   string

	Runs        int
	Success     int
	Failure     int
	Running     int
	SuccessRate int
}

func parseActionFilter(r *http.Request) actionFilter {
	q := r.URL.Query()
	f := actionFilter{
		Repo:   strings.TrimSpace(q.Get("repo")),
		Token:  strings.TrimSpace(q.Get("token")),
		Status: strings.TrimSpace(q.Get("status")),
	}
	if f.Status == "" {
		f.Status = "all"
	}
	return f
}

// buildActionData filters the cached runs. Repo cards with nothing left after
// filtering drop out entirely, so a "failure" filter shows exactly the repos
// that need attention.
func (s *Server) buildActionData(f actionFilter) *actionData {
	view, updatedAt, err := s.hub.Runs()

	d := &actionData{
		Filter:    f,
		Tokens:    s.tokenOptions(),
		UpdatedAt: updatedAt,
		CacheHits: view.CacheHits,
		Fetched:   view.Fetched,
		Errors:    view.Errors,
	}
	if err != nil {
		d.Warning = err.Error()
	}

	for _, rr := range view.Repos {
		d.RepoNames = append(d.RepoNames, rr.Repo)

		if f.Repo != "" && rr.Repo != f.Repo {
			continue
		}
		if f.Token != "" && rr.TokenName != f.Token {
			continue
		}

		kept := rr
		kept.Runs = filterRuns(rr.Runs, f.Status)
		if len(kept.Runs) == 0 {
			continue
		}
		d.Repos = append(d.Repos, kept)

		for _, run := range kept.Runs {
			d.Runs++
			switch run.Outcome() {
			case "success":
				d.Success++
			case "failure", "timed_out", "startup_failure":
				d.Failure++
			case "running", "queued":
				d.Running++
			}
		}
	}

	if d.Success+d.Failure > 0 {
		d.SuccessRate = d.Success * 100 / (d.Success + d.Failure)
	} else {
		d.SuccessRate = -1
	}
	return d
}

func filterRuns(runs []gh.Run, status string) []gh.Run {
	if status == "" || status == "all" {
		return runs
	}
	out := make([]gh.Run, 0, len(runs))
	for _, r := range runs {
		switch status {
		case "success":
			if r.Outcome() == "success" {
				out = append(out, r)
			}
		case "failure":
			switch r.Outcome() {
			case "failure", "timed_out", "startup_failure", "action_required":
				out = append(out, r)
			}
		case "running":
			switch r.Outcome() {
			case "running", "queued":
				out = append(out, r)
			}
		default:
			out = append(out, r)
		}
	}
	return out
}

func (s *Server) handleActionsPage(w http.ResponseWriter, r *http.Request) {
	data := s.buildActionData(parseActionFilter(r))
	s.render(w, pageData{
		Title:      "Actions",
		ActiveSlug: "actions",
		Body:       "page-actions",
		Actions:    data,
	})
}

func (s *Server) handleActionsFragment(w http.ResponseWriter, r *http.Request) {
	s.fragment(w, "actions-body", s.buildActionData(parseActionFilter(r)))
}
