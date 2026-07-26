package gh

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Repo is the subset of a GitHub repository we render.
type Repo struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	FullName string `json:"full_name"`
	Owner    struct {
		Login string `json:"login"`
	} `json:"owner"`
	Private       bool      `json:"private"`
	Fork          bool      `json:"fork"`
	Archived      bool      `json:"archived"`
	Disabled      bool      `json:"disabled"`
	HTMLURL       string    `json:"html_url"`
	Description   string    `json:"description"`
	DefaultBranch string    `json:"default_branch"`
	PushedAt      time.Time `json:"pushed_at"`
	OpenIssues    int       `json:"open_issues_count"`

	// TokenName records which identity saw this repo, so page filters can group
	// by account.
	TokenName string `json:"-"`
}

// maxRepoPages caps discovery at 300 repos per token. Beyond that a user should
// name repos explicitly in config.yaml.
const maxRepoPages = 3

// Repos returns the repositories a token should track. When names is non-empty
// those are fetched directly; otherwise everything the token can see is
// discovered, newest push first.
func (c *Client) Repos(ctx context.Context, tok *Token, names []string) ([]Repo, error) {
	if len(names) > 0 {
		return c.reposByName(ctx, tok, names)
	}
	return c.discoverRepos(ctx, tok)
}

// reposByName returns both the repos it could read and every error it hit.
// Partial success beats no dashboard, so the repos come back even when the error
// is non-nil — but the error is never swallowed: a repo named in config.yaml that
// the token cannot see (typo, or simply not granted) has to be visible, otherwise
// it silently does nothing forever.
func (c *Client) reposByName(ctx context.Context, tok *Token, names []string) ([]Repo, error) {
	out := make([]Repo, 0, len(names))
	var errs []error
	for _, n := range names {
		n = strings.TrimSpace(strings.Trim(n, "/"))
		if n == "" || !strings.Contains(n, "/") {
			continue
		}
		var r Repo
		if _, err := c.get(ctx, tok, "/repos/"+n, &r); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", n, err))
			continue
		}
		r.TokenName = tok.Name
		out = append(out, r)
	}
	return out, errors.Join(errs...)
}

// discoverRepos pages explicitly with &page=N rather than following the Link
// header. GitHub does not send Link on a 304, so a header-driven chain stops
// after page one as soon as the cache is warm — discovery would silently shrink
// from every repo to the first hundred. A full page means "ask for the next one",
// and that works the same whether the body came from GitHub or from SQLite.
func (c *Client) discoverRepos(ctx context.Context, tok *Token) ([]Repo, error) {
	const perPage = 100

	var all []Repo
	for page := 1; page <= maxRepoPages; page++ {
		url := fmt.Sprintf("/user/repos?per_page=%d&page=%d&sort=pushed&direction=desc"+
			"&affiliation=owner,collaborator,organization_member", perPage, page)

		var batch []Repo
		if _, err := c.get(ctx, tok, url, &batch); err != nil {
			if len(all) > 0 {
				break // keep the pages we have
			}
			return nil, err
		}
		all = append(all, batch...)
		if len(batch) < perPage {
			break // last page
		}
	}

	out := make([]Repo, 0, len(all))
	for _, r := range all {
		if r.Archived || r.Disabled {
			continue
		}
		r.TokenName = tok.Name
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PushedAt.After(out[j].PushedAt) })
	return out, nil
}

// RateLimit primes the token's budget state at startup. The endpoint does not
// count against the limit, and its response headers already carry the numbers —
// so we ask for no body and let readRateHeaders do the work. Reading the body
// would be wrong here: on a 304 it would be a stale snapshot.
func (c *Client) RateLimit(ctx context.Context, tok *Token) error {
	if _, err := c.get(ctx, tok, "/rate_limit", nil); err != nil {
		return fmt.Errorf("rate_limit for %s: %w", tok.Name, err)
	}
	return nil
}
