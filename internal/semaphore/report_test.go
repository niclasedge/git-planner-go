package semaphore

import (
	"strings"
	"testing"
	"time"
)

func task(id, tmpl int, status, created string) Task {
	t := Task{ID: id, TemplateID: tmpl, Status: status}
	if created != "" {
		v, err := time.Parse(time.RFC3339, created)
		if err != nil {
			panic(err)
		}
		t.Created = apiTime{v}
	}
	return t
}

// The whole point of the reduction: a template that failed yesterday and
// succeeded today is green, and must not show up twice.
func TestLastRunPerTemplate(t *testing.T) {
	tasks := []Task{
		task(10, 1, "error", "2026-07-25T08:00:00Z"),
		task(11, 1, "success", "2026-07-26T08:00:00Z"),
		task(12, 2, "error", "2026-07-27T09:00:00Z"),
		task(9, 2, "success", "2026-07-20T09:00:00Z"),
		task(13, 3, "running", "2026-07-27T10:00:00Z"),
	}

	got := lastRunPerTemplate(tasks)
	if len(got) != 3 {
		t.Fatalf("expected one run per template, got %d", len(got))
	}
	// Newest first: 13 (10:00), 12 (09:00), 11 (yesterday).
	wantIDs := []int{13, 12, 11}
	for i, want := range wantIDs {
		if got[i].ID != want {
			t.Fatalf("expected order %v, got %d at %d", wantIDs, got[i].ID, i)
		}
	}
}

// Equal timestamps happen when Semaphore truncates to the second; map iteration
// order must not leak into the output.
func TestLastRunPerTemplate_StableOnTies(t *testing.T) {
	tasks := []Task{
		task(1, 1, "success", "2026-07-27T09:00:00Z"),
		task(2, 2, "success", "2026-07-27T09:00:00Z"),
		task(3, 3, "success", "2026-07-27T09:00:00Z"),
	}
	for i := 0; i < 20; i++ {
		got := lastRunPerTemplate(tasks)
		if got[0].ID != 3 || got[1].ID != 2 || got[2].ID != 1 {
			t.Fatalf("run %d: unstable order %v", i, []int{got[0].ID, got[1].ID, got[2].ID})
		}
	}
}

// A task with no Created at all still has to survive the sort.
func TestLastRunPerTemplate_NoTimestamp(t *testing.T) {
	got := lastRunPerTemplate([]Task{
		task(1, 1, "error", ""),
		task(2, 2, "success", "2026-07-27T09:00:00Z"),
	})
	if len(got) != 2 || got[0].ID != 2 || got[1].ID != 1 {
		t.Fatalf("expected the dated run first, got %v", got)
	}
}

// The real failure from the live instance: git retries the submodule clone, so
// the same two lines arrive four times.
func TestErrorExcerpt_DedupesAndKeepsOrder(t *testing.T) {
	lines := []string{
		"PLAY [Claude Research] ***",
		"TASK [checkout] ***",
		"git@github.com: Permission denied (publickey).",
		"fatal: Could not read from remote repository.",
		"git@github.com: Permission denied (publickey).",
		"fatal: Could not read from remote repository.",
		"PLAY RECAP ***",
		"localhost : ok=2 changed=0 unreachable=0 failed=1",
	}

	got := ErrorExcerpt(lines, 12)
	want := []string{
		"git@github.com: Permission denied (publickey).",
		"fatal: Could not read from remote repository.",
		"localhost : ok=2 changed=0 unreachable=0 failed=1",
	}
	if len(got) != len(want) {
		t.Fatalf("expected %d lines, got %d: %v", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("line %d: expected %q, got %q", i, want[i], got[i])
		}
	}
}

// unreachable=0 appears in every green recap; matching it would bury the cause.
func TestErrorExcerpt_IgnoresGreenRecap(t *testing.T) {
	lines := []string{
		"localhost : ok=5 changed=1 unreachable=0 failed=0",
		"ERROR! the playbook could not be parsed",
	}
	got := ErrorExcerpt(lines, 12)
	if len(got) != 1 || !strings.HasPrefix(got[0], "ERROR!") {
		t.Fatalf("expected only the ERROR! line, got %v", got)
	}
}

func TestErrorExcerpt_FallsBackToTail(t *testing.T) {
	var lines []string
	for i := 0; i < 30; i++ {
		lines = append(lines, "line")
	}
	lines = append(lines, "", "   ", "last")

	got := ErrorExcerpt(lines, 12)
	if len(got) != fallbackLines {
		t.Fatalf("expected %d tail lines, got %d", fallbackLines, len(got))
	}
	if got[len(got)-1] != "last" {
		t.Fatalf("the tail must end at the last non-empty line, got %q", got[len(got)-1])
	}
	if len(ErrorExcerpt(nil, 12)) != 0 {
		t.Fatal("no output must yield no excerpt")
	}
}

func TestErrorExcerpt_Limit(t *testing.T) {
	lines := []string{"fatal: a", "fatal: b", "fatal: c", "fatal: d"}
	if got := ErrorExcerpt(lines, 2); len(got) != 2 {
		t.Fatalf("expected the limit to hold, got %v", got)
	}
}

func TestRowOutcome(t *testing.T) {
	cases := map[string]string{
		"success": "success", "error": "failure", "stopped": "cancelled",
		"running": "running", "waiting": "queued", "nonsense": "unknown",
	}
	for status, want := range cases {
		r := Row{Task: Task{Status: status}}
		if got := r.Outcome(); got != want {
			t.Fatalf("%s: expected %s, got %s", status, want, got)
		}
		if r.Bad() != (status == "error" || status == "stopped") {
			t.Fatalf("%s: Bad() disagrees with the failure list", status)
		}
	}
}

func TestRowURLAndWhen(t *testing.T) {
	r := Row{TemplateID: 7, base: "http://host:3001", projectID: 2}
	if want := "http://host:3001/project/2/templates/7"; r.URL() != want {
		t.Fatalf("expected %s, got %s", want, r.URL())
	}

	// Without a template it can only link to the project's history.
	r.TemplateID = 0
	if want := "http://host:3001/project/2/history"; r.URL() != want {
		t.Fatalf("expected %s, got %s", want, r.URL())
	}
	if (Row{}).URL() != "" {
		t.Fatal("a row with no instance must not produce a link")
	}
	if (Row{}).When() != "—" {
		t.Fatal("a run with no timestamp must not render as a date")
	}
}

// Semaphore sends timestamps as strings, and an empty one for a task that never
// started. A parse failure must not fail the report.
func TestAPITime(t *testing.T) {
	for _, raw := range []string{`""`, `null`, `"not a date"`} {
		var at apiTime
		if err := at.UnmarshalJSON([]byte(raw)); err != nil {
			t.Fatalf("%s: unexpected error %v", raw, err)
		}
		if !at.IsZero() {
			t.Fatalf("%s: expected the zero time, got %v", raw, at)
		}
	}

	var at apiTime
	if err := at.UnmarshalJSON([]byte(`"2026-07-27T07:00:00.123456Z"`)); err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	if at.Year() != 2026 || at.Minute() != 0 {
		t.Fatalf("expected 2026-07-27 07:00, got %v", at)
	}
}

func TestTaskDuration(t *testing.T) {
	start, _ := time.Parse(time.RFC3339, "2026-07-27T07:00:00Z")
	tk := Task{Start: apiTime{start}, End: apiTime{start.Add(2 * time.Second)}}
	if tk.Duration() != 2*time.Second {
		t.Fatalf("expected 2s, got %v", tk.Duration())
	}
	// A running task has no End; it must read as unknown, not as a huge number.
	if (Task{Start: apiTime{start}}).Duration() != 0 {
		t.Fatal("an unfinished task has no duration")
	}
	// And a clock that went backwards must not produce a negative one.
	if (Task{Start: apiTime{start}, End: apiTime{start.Add(-time.Hour)}}).Duration() != 0 {
		t.Fatal("a negative duration must be clamped")
	}
}
