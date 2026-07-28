package semaphore

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

// A task is finished once it reaches one of these; everything else is still
// moving and must not be counted as green.
var doneStatus = map[string]bool{"success": true, "error": true, "stopped": true}

// badStatus is what goes on top of the widget.
var badStatus = map[string]bool{"error": true, "stopped": true}

// Row is one template's most recent run.
type Row struct {
	Template   string
	TemplateID int
	Task       Task
	// Excerpt is filled for failed rows only, and only for the first few of
	// them: every excerpt costs one extra API call.
	Excerpt []string

	base      string
	projectID int
}

func (r Row) Status() string { return r.Task.Status }
func (r Row) Bad() bool      { return badStatus[r.Task.Status] }
func (r Row) Done() bool     { return doneStatus[r.Task.Status] }

// Outcome maps Semaphore's vocabulary onto the status colours the Actions page
// already defines, so both pages read the same way.
func (r Row) Outcome() string {
	switch r.Task.Status {
	case "success":
		return "success"
	case "error":
		return "failure"
	case "stopped", "stopping":
		return "cancelled"
	case "running", "starting":
		return "running"
	case "waiting", "confirmation":
		return "queued"
	default:
		return "unknown"
	}
}

func (r Row) Duration() time.Duration { return r.Task.Duration() }

// When is the local timestamp, short: the year is noise on a dashboard.
func (r Row) When() string {
	t := r.Task.When()
	if t.IsZero() {
		return "—"
	}
	return t.Local().Format("02.01. 15:04")
}

// URL deep-links into Semaphore's web UI. Both routes below are real SPA paths.
func (r Row) URL() string {
	if r.base == "" || r.projectID == 0 {
		return ""
	}
	if r.TemplateID == 0 {
		return fmt.Sprintf("%s/project/%d/history", r.base, r.projectID)
	}
	return fmt.Sprintf("%s/project/%d/templates/%d", r.base, r.projectID, r.TemplateID)
}

// Report is the shape the widget renders: what is red, then everything else.
type Report struct {
	Project   string
	ProjectID int
	BaseURL   string
	Bad       []Row
	OK        []Row
}

func (r *Report) Total() int   { return len(r.Bad) + len(r.OK) }
func (r *Report) Failing() int { return len(r.Bad) }

// Row finds a run by task id. Only rows that are on the page can have their log
// requested, which keeps the log endpoint from being a reader for the whole
// instance.
func (r *Report) Row(taskID int) *Row {
	for i := range r.Bad {
		if r.Bad[i].Task.ID == taskID {
			return &r.Bad[i]
		}
	}
	for i := range r.OK {
		if r.OK[i].Task.ID == taskID {
			return &r.OK[i]
		}
	}
	return nil
}

// HistoryURL is the "all runs" page of the project.
func (r *Report) HistoryURL() string {
	if r.BaseURL == "" || r.ProjectID == 0 {
		return ""
	}
	return fmt.Sprintf("%s/project/%d/history", r.BaseURL, r.ProjectID)
}

// maxExcerpts limits how many failed jobs get their output fetched. Each one is
// a separate request against a machine that is already having a bad day, and
// past the first few nobody reads the detail anyway.
const maxExcerpts = 6

// Report fetches the newest run of every template in project and splits it into
// failing and non-failing rows, newest first.
func (c *Client) Report(ctx context.Context, project string, limit int) (*Report, error) {
	if limit <= 0 {
		limit = 400
	}

	pid, err := c.projectID(ctx, project)
	if err != nil {
		return nil, err
	}
	names, err := c.templateNames(ctx, pid)
	if err != nil {
		return nil, err
	}
	tasks, err := c.tasks(ctx, pid, limit)
	if err != nil {
		return nil, err
	}

	rep := &Report{Project: project, ProjectID: pid, BaseURL: c.base}
	for _, t := range lastRunPerTemplate(tasks) {
		name := names[t.TemplateID]
		if name == "" {
			// A deleted template still has history. Showing the id beats
			// dropping the run, which might be the red one.
			name = fmt.Sprintf("Template #%d", t.TemplateID)
		}
		row := Row{Template: name, TemplateID: t.TemplateID, Task: t, base: c.base, projectID: pid}
		if row.Bad() {
			rep.Bad = append(rep.Bad, row)
		} else {
			rep.OK = append(rep.OK, row)
		}
	}

	// Templates that never ran at all are invisible here, which is correct:
	// there is no run to report on.
	for i := range rep.Bad {
		if i >= maxExcerpts {
			break
		}
		lines, err := c.Output(ctx, pid, rep.Bad[i].Task.ID)
		if err != nil {
			// The row itself is the news; a missing excerpt must not hide it.
			rep.Bad[i].Excerpt = []string{"Log nicht lesbar: " + err.Error()}
			continue
		}
		rep.Bad[i].Excerpt = ErrorExcerpt(lines, 12)
	}
	return rep, nil
}

// lastRunPerTemplate keeps the newest task per template, newest template first.
func lastRunPerTemplate(tasks []Task) []Task {
	newest := map[int]Task{}
	for _, t := range tasks {
		if prev, ok := newest[t.TemplateID]; ok && !t.Sortable().After(prev.Sortable()) {
			continue
		}
		newest[t.TemplateID] = t
	}

	out := make([]Task, 0, len(newest))
	for _, t := range newest {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool {
		ti, tj := out[i].Sortable(), out[j].Sortable()
		if ti.Equal(tj) {
			return out[i].ID > out[j].ID // map order is random; keep it stable
		}
		return ti.After(tj)
	})
	return out
}

// errorMarkers are the lines worth reading in a failed Ansible run. Ported from
// IaC-Stack's semaphore_tasks.py, including its warning: match "UNREACHABLE!"
// as Ansible spells it and not a bare "unreachable", which would also hit the
// unreachable=0 of every green recap.
var errorMarkers = []*regexp.Regexp{
	regexp.MustCompile(`(?i)^fatal:`),
	regexp.MustCompile(`ERROR!`),
	regexp.MustCompile(`FAILED!`),
	regexp.MustCompile(`failed=[1-9]`),
	regexp.MustCompile(`unreachable=[1-9]`),
	regexp.MustCompile(`UNREACHABLE!`),
	regexp.MustCompile(`(?i)Permission denied`),
	regexp.MustCompile(`(?i)^error:`),
	regexp.MustCompile(`Could not read from remote repository`),
	regexp.MustCompile(`(?i)\baborting\b`),
}

// fallbackLines is how much tail to show when no marker matched.
const fallbackLines = 10

// ErrorExcerpt picks the lines that explain a failure, in original order.
//
// Deduplicated: git retries a failing submodule clone, so the same four lines
// arrive four times and a naive filter reports the cause four times over. Falls
// back to the tail when nothing matches — a job can die in a way none of these
// patterns describe, and silence would be worse than ten lines of context.
func ErrorExcerpt(lines []string, limit int) []string {
	if limit <= 0 {
		limit = 12
	}
	var hits []string
	seen := map[string]bool{}
	for _, line := range lines {
		text := strings.TrimRight(line, " \t\r\n")
		if strings.TrimSpace(text) == "" || !matchesMarker(text) {
			continue
		}
		key := strings.TrimSpace(text)
		if seen[key] {
			continue
		}
		seen[key] = true
		hits = append(hits, text)
		if len(hits) == limit {
			return hits
		}
	}
	if len(hits) > 0 {
		return hits
	}

	var tail []string
	for _, line := range lines {
		if text := strings.TrimRight(line, " \t\r\n"); strings.TrimSpace(text) != "" {
			tail = append(tail, text)
		}
	}
	if len(tail) > fallbackLines {
		tail = tail[len(tail)-fallbackLines:]
	}
	return tail
}

func matchesMarker(line string) bool {
	for _, m := range errorMarkers {
		if m.MatchString(line) {
			return true
		}
	}
	return false
}
