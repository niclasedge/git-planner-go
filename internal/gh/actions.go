package gh

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"
)

// Run is one workflow run.
type Run struct {
	ID           int64     `json:"id"`
	Name         string    `json:"name"`
	DisplayTitle string    `json:"display_title"`
	HeadBranch   string    `json:"head_branch"`
	HeadSHA      string    `json:"head_sha"`
	RunNumber    int       `json:"run_number"`
	RunAttempt   int       `json:"run_attempt"`
	Event        string    `json:"event"`
	Status       string    `json:"status"`     // queued | in_progress | completed
	Conclusion   string    `json:"conclusion"` // success | failure | cancelled | skipped | …
	WorkflowID   int64     `json:"workflow_id"`
	HTMLURL      string    `json:"html_url"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	RunStartedAt time.Time `json:"run_started_at"`
	Actor        User      `json:"actor"`

	Repo      string `json:"-"`
	TokenName string `json:"-"`
	// Jobs is populated only for the newest few runs — see Actions.JobsPerRepo.
	Jobs []Job `json:"-"`
}

type Job struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Status      string    `json:"status"`
	Conclusion  string    `json:"conclusion"`
	StartedAt   time.Time `json:"started_at"`
	CompletedAt time.Time `json:"completed_at"`
	HTMLURL     string    `json:"html_url"`
	Steps       []Step    `json:"steps"`
}

type Step struct {
	Name        string    `json:"name"`
	Status      string    `json:"status"`
	Conclusion  string    `json:"conclusion"`
	Number      int       `json:"number"`
	StartedAt   time.Time `json:"started_at"`
	CompletedAt time.Time `json:"completed_at"`
}

// Outcome collapses status and conclusion into the one word the UI colours by.
func (r Run) Outcome() string { return outcome(r.Status, r.Conclusion) }

func (j Job) Outcome() string { return outcome(j.Status, j.Conclusion) }

func (s Step) Outcome() string { return outcome(s.Status, s.Conclusion) }

// Quiet marks a run that never did any work — skipped by a path filter,
// cancelled, or neutral. It lives next to Outcome so the vocabulary stays in one
// place. A repo whose recent history is mostly skips otherwise reads as busy,
// and the one run that actually built something has to be hunted for.
func (r Run) Quiet() bool {
	switch r.Outcome() {
	case "skipped", "cancelled", "neutral", "stale":
		return true
	}
	return false
}

func outcome(status, conclusion string) string {
	switch status {
	case "completed":
		if conclusion == "" {
			return "unknown"
		}
		return conclusion
	case "in_progress":
		return "running"
	case "queued", "waiting", "requested", "pending":
		return "queued"
	default:
		if conclusion != "" {
			return conclusion
		}
		return "unknown"
	}
}

// Duration is wall-clock time of the run. A run still going is measured against
// now, so the number keeps climbing while you watch it.
func (r Run) Duration() time.Duration {
	start := r.RunStartedAt
	if start.IsZero() {
		start = r.CreatedAt
	}
	if start.IsZero() {
		return 0
	}
	if r.Status == "completed" {
		if r.UpdatedAt.After(start) {
			return r.UpdatedAt.Sub(start)
		}
		return 0
	}
	return time.Since(start)
}

func (j Job) Duration() time.Duration {
	if j.StartedAt.IsZero() {
		return 0
	}
	if j.CompletedAt.After(j.StartedAt) {
		return j.CompletedAt.Sub(j.StartedAt)
	}
	if j.Status == "completed" {
		return 0
	}
	return time.Since(j.StartedAt)
}

// StepDot is one dot in a run row: every step of every job, in order.
type StepDot struct {
	Label   string
	Outcome string
}

// Dots flattens the run's jobs into a single sequence for the step-dot row.
func (r Run) Dots() []StepDot {
	var dots []StepDot
	for _, j := range r.Jobs {
		for _, s := range j.Steps {
			label := s.Name
			if len(r.Jobs) > 1 {
				label = j.Name + " / " + s.Name
			}
			dots = append(dots, StepDot{Label: label, Outcome: s.Outcome()})
		}
	}
	return dots
}

// Title is what to show as the run's headline.
func (r Run) Title() string {
	if r.DisplayTitle != "" {
		return r.DisplayTitle
	}
	if r.Name != "" {
		return r.Name
	}
	return fmt.Sprintf("run #%d", r.RunNumber)
}

func (r Run) ShortSHA() string {
	if len(r.HeadSHA) < 7 {
		return r.HeadSHA
	}
	return r.HeadSHA[:7]
}

// RepoRuns groups a repo's runs, which is how page 2 is laid out — one card per
// repo.
type RepoRuns struct {
	Repo      string
	TokenName string
	Runs      []Run
}

// SuccessRate is the share of concluded runs that succeeded, in percent.
// Cancelled and skipped runs are ignored: they say nothing about health.
func (rr RepoRuns) SuccessRate() int {
	var counted, ok int
	for _, r := range rr.Runs {
		switch r.Outcome() {
		case "success":
			counted++
			ok++
		case "failure", "timed_out", "action_required", "startup_failure":
			counted++
		}
	}
	if counted == 0 {
		return -1 // no signal; the UI shows a dash
	}
	return ok * 100 / counted
}

// Chronological returns the runs oldest first. The API hands them back
// newest-first, which is right for a list but backwards for a sparkline — time
// should run left to right.
func (rr RepoRuns) Chronological() []Run {
	out := make([]Run, len(rr.Runs))
	for i, r := range rr.Runs {
		out[len(rr.Runs)-1-i] = r
	}
	return out
}

func (rr RepoRuns) LastRun() *Run {
	if len(rr.Runs) == 0 {
		return nil
	}
	return &rr.Runs[0]
}

// MaxDuration is the longest run in the group, used to scale the sparkline.
func (rr RepoRuns) MaxDuration() time.Duration {
	var max time.Duration
	for _, r := range rr.Runs {
		if d := r.Duration(); d > max {
			max = d
		}
	}
	return max
}

// RunSet is the outcome of one Actions refresh.
type RunSet struct {
	Repos     []RepoRuns
	FromCache int
	Fetched   int
	// Skipped counts repos with Actions switched off. Plenty of repos have no
	// CI at all, so this is the normal case, not a fault.
	Skipped int
	Errors  []error
}

// Totals aggregates across all repos for the header stats row.
func (rs RunSet) Totals() (runs, success, failure, running int) {
	for _, rr := range rs.Repos {
		for _, r := range rr.Runs {
			runs++
			switch r.Outcome() {
			case "success":
				success++
			case "failure", "timed_out", "startup_failure":
				failure++
			case "running", "queued":
				running++
			}
		}
	}
	return
}

func (rs RunSet) SuccessRate() int {
	_, success, failure, _ := rs.Totals()
	if success+failure == 0 {
		return -1
	}
	return success * 100 / (success + failure)
}

// ActionQuery controls how much Actions data to pull.
type ActionQuery struct {
	RunsPerRepo int
	// JobsPerRepo limits how many of the newest runs get their jobs fetched.
	// Each one is an extra request, so this is the cost knob for step dots.
	JobsPerRepo int
	Branch      string
	Status      string
}

// Runs fetches workflow runs for every repo in parallel. Repos without Actions
// simply come back empty and are dropped.
func (c *Client) Runs(ctx context.Context, tok *Token, repos []Repo, q ActionQuery) RunSet {
	if q.RunsPerRepo <= 0 {
		q.RunsPerRepo = 10
	}

	type repoResult struct {
		runs      RepoRuns
		fromCache bool
		// extra counts the job requests, which are not free.
		extraFetched, extraCached int
	}

	results := fanOut(repos, defaultConcurrency, func(r Repo) (repoResult, error) {
		path := fmt.Sprintf("/repos/%s/actions/runs?per_page=%d&exclude_pull_requests=true",
			r.FullName, q.RunsPerRepo)
		if q.Branch != "" {
			path += "&branch=" + q.Branch
		}
		if q.Status != "" && q.Status != "all" {
			path += "&status=" + q.Status
		}

		var body struct {
			TotalCount   int   `json:"total_count"`
			WorkflowRuns []Run `json:"workflow_runs"`
		}
		res, err := c.get(ctx, tok, path, &body)
		if err != nil {
			return repoResult{}, fmt.Errorf("%s: %w", r.FullName, err)
		}

		runs := body.WorkflowRuns
		for i := range runs {
			runs[i].Repo = r.FullName
			runs[i].TokenName = tok.Name
		}

		out := repoResult{
			runs:      RepoRuns{Repo: r.FullName, TokenName: tok.Name, Runs: runs},
			fromCache: res.fromCache,
		}

		// Jobs for the newest runs only. Sequential on purpose — the outer fan-out
		// already provides parallelism, and nesting pools would multiply load.
		limit := min(q.JobsPerRepo, len(runs))
		for i := range limit {
			jobs, cached, err := c.jobs(ctx, tok, r.FullName, runs[i].ID)
			if err != nil {
				continue // step dots are decoration; never fail the card for them
			}
			runs[i].Jobs = jobs
			if cached {
				out.extraCached++
			} else {
				out.extraFetched++
			}
		}
		return out, nil
	})

	set := RunSet{}
	for _, r := range results {
		if r.Err != nil {
			if errors.Is(r.Err, ErrNotFound) {
				set.Skipped++ // Actions disabled on this repo
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
		set.FromCache += r.Value.extraCached
		set.Fetched += r.Value.extraFetched

		if len(r.Value.runs.Runs) == 0 {
			continue // no workflows in this repo
		}
		set.Repos = append(set.Repos, r.Value.runs)
	}

	// Most recently active repo first — that is what you look at.
	sort.SliceStable(set.Repos, func(i, j int) bool {
		a, b := set.Repos[i].LastRun(), set.Repos[j].LastRun()
		if a == nil || b == nil {
			return b == nil
		}
		return a.CreatedAt.After(b.CreatedAt)
	})
	return set
}

func (c *Client) jobs(ctx context.Context, tok *Token, fullName string, runID int64) ([]Job, bool, error) {
	path := fmt.Sprintf("/repos/%s/actions/runs/%d/jobs?per_page=20", fullName, runID)
	var body struct {
		TotalCount int   `json:"total_count"`
		Jobs       []Job `json:"jobs"`
	}
	res, err := c.get(ctx, tok, path, &body)
	if err != nil {
		return nil, false, err
	}
	return body.Jobs, res.fromCache, nil
}
