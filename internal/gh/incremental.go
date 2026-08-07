package gh

// Incremental issue polling.
//
// The full sweep asks every repo whether anything changed: 225 conditional
// requests to learn that five repos moved. GET /issues answers the same question
// in one request — it is cross-repo and takes a ?since= cursor, so a poll returns
// only what actually changed, in the order it changed.
//
// Measured against the live API on a 48-hour window: 89 issues across 5 repos in
// a single 800 KB response, versus 225 requests for the equivalent sweep.
//
// What it does not cover is why the full sweep still runs hourly. GET /issues is
// scoped to issues the token's user created, was assigned, was mentioned in or is
// subscribed to — which for a personal account is nearly everything, but not an
// issue a stranger opened in a repo the user watches but never touched. The
// hourly sweep is the backstop that reconciles anything the cursor missed.

import (
	"context"
	"fmt"
	"net/url"
	"time"
)

const (
	// incrementalPerPage is the API maximum; a poll should be one request.
	incrementalPerPage = 100
	// incrementalMaxPages bounds a single poll. Hitting it means the window held
	// more change than an incremental merge should be trusted with — the caller
	// falls back to a full sweep instead of merging a truncated picture.
	incrementalMaxPages = 5
)

// IncrementalSet is one poll's worth of changed issues. PRs are kept separate,
// as everywhere else.
type IncrementalSet struct {
	Issues []Issue
	PRs    []Issue
	// Requests is what the poll cost. One, unless the window was busy.
	Requests int
	// Truncated means the page cap was reached and this set is not the whole
	// window. Merging it would leave the view quietly wrong, so callers should
	// force a full sweep instead.
	Truncated bool
}

// IssuesSince returns every issue and PR the token can see that changed after
// since, newest first.
//
// Always state=all, regardless of what the caller keeps in memory: a poll that
// asked only for open issues could never learn that one was closed, and the
// closure would sit in the list until the next full sweep.
func (c *Client) IssuesSince(ctx context.Context, tok *Token, since time.Time) (IncrementalSet, error) {
	set := IncrementalSet{}

	for page := 1; page <= incrementalMaxPages; page++ {
		// filter=all is the widest the endpoint offers: created, assigned,
		// mentioned and subscribed combined.
		path := fmt.Sprintf("/issues?filter=all&state=all&since=%s&per_page=%d&page=%d&sort=updated&direction=desc",
			url.QueryEscape(since.UTC().Format(time.RFC3339)), incrementalPerPage, page)

		var raw []struct {
			Issue
			// The cross-repo endpoint carries the repository inline — which is what
			// makes one request enough. The per-repo endpoint has no such field, so
			// this shape is local to this call.
			Repository struct {
				FullName string `json:"full_name"`
			} `json:"repository"`
		}
		if _, err := c.getFresh(ctx, tok, path, &raw); err != nil {
			return set, fmt.Errorf("incremental poll: %w", err)
		}
		set.Requests++

		for _, r := range raw {
			is := r.Issue
			is.Repo = r.Repository.FullName
			is.TokenName = tok.Name
			if d, ok := is.PlannedDate(); ok {
				is.Planned = &d
			}
			if d, ok := is.DueDate(); ok {
				is.Due = &d
			}
			if is.IsPR() {
				set.PRs = append(set.PRs, is)
				continue
			}
			set.Issues = append(set.Issues, is)
		}

		if len(raw) < incrementalPerPage {
			return set, nil
		}
	}

	set.Truncated = true
	return set, nil
}
