package web

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/niclasedge/git-planner-go/internal/gh"
)

type issueFilter struct {
	Repo     string
	Token    string
	Label    string
	Assignee string
	Query    string
}

func (f issueFilter) Any() bool {
	return f.Repo != "" || f.Token != "" || f.Label != "" || f.Assignee != "" || f.Query != ""
}

type tokenOption struct {
	Name  string
	Label string
}

type issueData struct {
	Issues []gh.Issue
	Total  int
	Filter issueFilter

	RepoNames []string
	Labels    []string
	Tokens    []tokenOption

	UpdatedAt time.Time
	CacheHits int
	Fetched   int
	Errors    []string
	Warning   string

	Repos    int
	Assigned int
	Stale    int
}

func (s *Server) tokenOptions() []tokenOption {
	out := make([]tokenOption, 0, len(s.cfg.Tokens))
	for _, t := range s.cfg.Tokens {
		out = append(out, tokenOption{Name: t.Name, Label: t.Label})
	}
	return out
}

func parseIssueFilter(r *http.Request) issueFilter {
	q := r.URL.Query()
	return issueFilter{
		Repo:     strings.TrimSpace(q.Get("repo")),
		Token:    strings.TrimSpace(q.Get("token")),
		Label:    strings.TrimSpace(q.Get("label")),
		Assignee: strings.TrimSpace(q.Get("assignee")),
		Query:    strings.TrimSpace(q.Get("q")),
	}
}

// buildIssueData applies the filters in memory. The full list is already local,
// so filtering thousands of issues is microseconds — no reason to spend a rate
// limit request on it.
func (s *Server) buildIssueData(f issueFilter) *issueData {
	view, updatedAt, err := s.hub.Issues()

	d := &issueData{
		Total:     len(view.Issues),
		Filter:    f,
		RepoNames: view.RepoNames,
		Labels:    view.Labels,
		Tokens:    s.tokenOptions(),
		UpdatedAt: updatedAt,
		CacheHits: view.CacheHits,
		Fetched:   view.Fetched,
		Errors:    view.Errors,
	}
	if err != nil {
		d.Warning = err.Error()
	}

	needle := strings.ToLower(f.Query)
	repos := map[string]bool{}

	for _, is := range view.Issues {
		if f.Repo != "" && is.Repo != f.Repo {
			continue
		}
		if f.Token != "" && is.TokenName != f.Token {
			continue
		}
		if f.Label != "" && !hasLabel(is, f.Label) {
			continue
		}
		if f.Assignee != "" && !matchAssignee(is, f.Assignee) {
			continue
		}
		if needle != "" && !matchText(is, needle) {
			continue
		}

		d.Issues = append(d.Issues, is)
		repos[is.Repo] = true
		if len(is.Assignees) > 0 {
			d.Assigned++
		}
		if is.Stale() {
			d.Stale++
		}
	}
	d.Repos = len(repos)
	return d
}

func hasLabel(is gh.Issue, name string) bool {
	for _, l := range is.Labels {
		if strings.EqualFold(l.Name, name) {
			return true
		}
	}
	return false
}

func matchAssignee(is gh.Issue, who string) bool {
	if who == "none" {
		return len(is.Assignees) == 0
	}
	for _, a := range is.Assignees {
		if strings.EqualFold(a.Login, who) {
			return true
		}
	}
	return false
}

func matchText(is gh.Issue, needle string) bool {
	if strings.Contains(strings.ToLower(is.Title), needle) {
		return true
	}
	if strings.Contains(strings.ToLower(is.Repo), needle) {
		return true
	}
	// "#42" or plain "42" should find the issue number.
	if n, err := strconv.Atoi(strings.TrimPrefix(needle, "#")); err == nil && n == is.Number {
		return true
	}
	return false
}

func (s *Server) handleIssuesPage(w http.ResponseWriter, r *http.Request) {
	data := s.buildIssueData(parseIssueFilter(r))
	s.render(w, pageData{
		Title:      "Issues",
		ActiveSlug: "issues",
		Body:       "page-issues",
		Issues:     data,
	})
}

func (s *Server) handleIssuesFragment(w http.ResponseWriter, r *http.Request) {
	s.fragment(w, "issues-body", s.buildIssueData(parseIssueFilter(r)))
}
