package gh

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
)

type User struct {
	Login     string `json:"login"`
	AvatarURL string `json:"avatar_url"`
	HTMLURL   string `json:"html_url"`
}

type Label struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

type Milestone struct {
	Title  string     `json:"title"`
	Number int        `json:"number"`
	DueOn  *time.Time `json:"due_on"` // the only due date GitHub itself models
}

// Issue is one issue. Note that /repos/{owner}/{repo}/issues also returns pull
// requests; anything with a non-nil PullRequest is dropped before it reaches the
// UI.
type Issue struct {
	Number    int    `json:"number"`
	Title     string `json:"title"`
	State     string `json:"state"`
	HTMLURL   string `json:"html_url"`
	Body      string `json:"body"`
	Comments  int    `json:"comments"`
	Locked    bool   `json:"locked"`
	User      User   `json:"user"`
	Assignees []User `json:"assignees"`
	Labels    []Label
	Milestone *Milestone `json:"milestone"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	ClosedAt  *time.Time `json:"closed_at"`

	PullRequest *struct {
		HTMLURL string `json:"html_url"`
	} `json:"pull_request"`

	// Sub is GitHub's own sub-issue rollup and comes with the list response, so
	// it costs nothing. Completed counts children that are closed — which is why
	// the planner can show "2/7 done" while rendering only the open children.
	// The payload has no parent pointer, so the child numbers need SubIssues().
	Sub struct {
		Total     int `json:"total"`
		Completed int `json:"completed"`
	} `json:"sub_issues_summary"`

	// Enriched locally so the UI can render and filter a flat, cross-repo list.
	Repo      string `json:"-"`
	TokenName string `json:"-"`

	// Due is DueDate() computed once at fetch time. The agenda needs a date for
	// every issue, and re-running the body patterns over a few hundred bodies on
	// every render would cost more than the whole page does today.
	Due *time.Time `json:"-"`
}

func (i Issue) IsPR() bool { return i.PullRequest != nil }

// Age returns how long ago the issue was last touched.
func (i Issue) Age() time.Duration { return time.Since(i.UpdatedAt) }

// StaleAfter is when an untouched issue starts reading as stale. It lives here,
// next to Age, so the row and the "still" stat cannot drift apart.
const StaleAfter = 30 * 24 * time.Hour

// Stale is the row-level form of the same threshold the issues page counts with,
// so a template can mark the date without knowing the number.
func (i Issue) Stale() bool { return i.Age() > StaleAfter }

// IssueQuery describes what to fetch per repo.
type IssueQuery struct {
	State   string // open | closed | all
	PerRepo int
	Labels  []string
	// Assignee filters server-side. "*" means any, "none" means unassigned.
	Assignee string
}

// IssueSet is the outcome of one refresh, including which repos failed. A repo
// that errors degrades the page instead of emptying it.
type IssueSet struct {
	Issues []Issue
	// PRs are what the issues endpoint returns alongside the issues. Separate
	// list rather than a flag, so the issues page stays a list of issues.
	PRs []Issue
	// FromCache counts repos whose response was a 304 — i.e. cost nothing.
	FromCache int
	Fetched   int
	// Skipped counts repos that have the feature switched off. That is a
	// permanent property of the repo, not a failure, so it must not show up as an
	// error the user is asked to worry about.
	Skipped int
	Errors  []error
}

// Issues fetches issues for every repo in parallel and returns one flat list,
// newest activity first.
func (c *Client) Issues(ctx context.Context, tok *Token, repos []Repo, q IssueQuery) IssueSet {
	if q.State == "" {
		q.State = "open"
	}
	if q.PerRepo <= 0 {
		q.PerRepo = 50
	}

	type repoResult struct {
		issues    []Issue
		prs       []Issue
		fromCache bool
	}

	results := fanOut(repos, defaultConcurrency, func(r Repo) (repoResult, error) {
		path := fmt.Sprintf("/repos/%s/issues?state=%s&per_page=%d&sort=updated&direction=desc",
			r.FullName, q.State, q.PerRepo)
		if len(q.Labels) > 0 {
			path += "&labels=" + url.QueryEscape(strings.Join(q.Labels, ","))
		}
		if q.Assignee != "" {
			path += "&assignee=" + url.QueryEscape(q.Assignee)
		}

		var raw []Issue
		res, err := c.get(ctx, tok, path, &raw)
		if err != nil {
			return repoResult{}, fmt.Errorf("%s: %w", r.FullName, err)
		}

		out := make([]Issue, 0, len(raw))
		var prs []Issue
		for _, is := range raw {
			is.Repo = r.FullName
			is.TokenName = tok.Name
			if d, ok := is.DueDate(); ok {
				is.Due = &d
			}
			// The issues endpoint mixes PRs in. They used to be dropped here;
			// keeping them costs no request and is where the PR counts come from.
			if is.IsPR() {
				prs = append(prs, is)
				continue
			}
			out = append(out, is)
		}
		return repoResult{issues: out, prs: prs, fromCache: res.fromCache}, nil
	})

	set := IssueSet{}
	for _, r := range results {
		if r.Err != nil {
			if errors.Is(r.Err, ErrNotFound) {
				set.Skipped++ // issues disabled on this repo
			} else {
				set.Errors = append(set.Errors, r.Err)
			}
			continue
		}
		if r.Value.fromCache {
			set.FromCache++
		} else {
			set.Fetched++
		}
		set.Issues = append(set.Issues, r.Value.issues...)
		set.PRs = append(set.PRs, r.Value.prs...)
	}

	sort.Slice(set.Issues, func(i, j int) bool {
		return set.Issues[i].UpdatedAt.After(set.Issues[j].UpdatedAt)
	})
	sort.Slice(set.PRs, func(i, j int) bool {
		return set.PRs[i].UpdatedAt.After(set.PRs[j].UpdatedAt)
	})
	return set
}
