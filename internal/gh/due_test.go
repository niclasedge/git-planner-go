package gh

import (
	"testing"
	"time"
)

// assertDate runs one parse case against a parser under test.
func assertDate(t *testing.T, parse func(string) (time.Time, bool), body, want string) {
	t.Helper()
	got, ok := parse(body)
	if want == "" {
		if ok {
			t.Fatalf("expected no date, got %s", got.Format("2006-01-02"))
		}
		return
	}
	if !ok {
		t.Fatalf("expected %s, got none", want)
	}
	if s := got.Format("2006-01-02"); s != want {
		t.Fatalf("expected %s, got %s", want, s)
	}
	if got.Location() != time.UTC {
		t.Fatalf("expected UTC, got %s", got.Location())
	}
}

// Target = planned. The soft date — ParseDue must not pick these up.
func TestParsePlanned(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string // "" means: no date
	}{
		{"canonical", "target date: 2026-08-01", "2026-08-01"},
		{"canonical in text", "Ablauf klären.\n\ntarget date: 2026-08-01\n\nDanach Review.", "2026-08-01"},
		{"target_date", "target_date: 2026-08-01", "2026-08-01"},
		{"target-date", "target-date : 2026-08-01", "2026-08-01"},
		{"uppercase", "Target Date: 2026-08-01", "2026-08-01"},
		{"leading space", "   target date: 2026-08-01   ", "2026-08-01"},
		{"crlf", "target date: 2026-08-01\r\nnext", "2026-08-01"},
		{"german day-first", "target: 01.08.2026", "2026-08-01"},

		{"empty", "", ""},
		{"no date", "Kein Datum hier drin.", ""},
		// A regexp is happy with these; a date parser must not be.
		{"impossible day", "target date: 2026-02-31", ""},
		{"impossible month", "target date: 2026-13-01", ""},
		// Not anchored and not one of the keywords: must not be picked up.
		{"unrelated date", "Siehe Release 2026-08-01 im Changelog", ""},
		{"prose mentioning target", "Das target date steht noch nicht fest", ""},
		// A due line is the other kind of date.
		{"due is not planned", "due: 2026-08-01", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { assertDate(t, ParsePlanned, c.body, c.want) })
	}
}

// Due = deadline. The hard date — ParsePlanned must not pick these up.
func TestParseDue(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{"canonical", "due: 2026-08-01", "2026-08-01"},
		{"due date line", "Due date: 2026-08-01", "2026-08-01"},
		{"due date inline", "Bitte bis due date: 2026-08-01 fertig", "2026-08-01"},
		{"at-due", "Task @due(2026-08-01) erledigen", "2026-08-01"},
		{"emoji", "Deadline 📅 2026-08-01", "2026-08-01"},
		{"german", "fällig: 01.08.2026", "2026-08-01"},
		{"german ae", "faellig: 01.08.2026", "2026-08-01"},

		{"empty", "", ""},
		{"no date", "Kein Datum hier drin.", ""},
		{"impossible day", "due: 2026-02-31", ""},
		{"unrelated date", "Siehe Release 2026-08-01 im Changelog", ""},
		// A target line is the other kind of date.
		{"planned is not due", "target date: 2026-08-01", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { assertDate(t, ParseDue, c.body, c.want) })
	}
}

// The canonical lines (planned and due) are stripped so the dates are not
// shown twice; the inline forms are part of a sentence and must survive.
func TestStripDue(t *testing.T) {
	cases := []struct{ name, body, want string }{
		{"strips the line", "Titeltext\n\ntarget date: 2026-08-01\n\nRest", "Titeltext\n\n\n\nRest"},
		{"only the line", "target date: 2026-08-01", ""},
		{"strips the due line", "Titel\n\nDue: 2026-08-15", "Titel"},
		{"strips both lines", "target date: 2026-08-01\ndue: 2026-08-15\n\nRest", "Rest"},
		{"keeps inline due", "Bitte bis due: 2026-08-01 fertig", "Bitte bis due: 2026-08-01 fertig"},
		{"keeps prose", "Kein Datum hier.", "Kein Datum hier."},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := StripDue(c.body); got != c.want {
				t.Fatalf("expected %q, got %q", c.want, got)
			}
		})
	}
}

// The body wins over the milestone: the line is about this issue, the milestone
// is about a batch of them. A target line is a plan, not a deadline — it must
// not feed DueDate.
func TestDueDateMilestoneFallback(t *testing.T) {
	ms := time.Date(2026, 9, 15, 7, 0, 0, 0, time.UTC)

	withBody := Issue{Body: "due: 2026-08-01", Milestone: &Milestone{DueOn: &ms}}
	got, ok := withBody.DueDate()
	if !ok || got.Format("2006-01-02") != "2026-08-01" {
		t.Fatalf("body should win, got %v (%v)", got, ok)
	}

	planned := Issue{Body: "target date: 2026-08-01", Milestone: &Milestone{DueOn: &ms}}
	got, ok = planned.DueDate()
	if !ok || got.Format("2006-01-02") != "2026-09-15" {
		t.Fatalf("a target line is not a deadline; expected milestone, got %v (%v)", got, ok)
	}
	if p, ok := planned.PlannedDate(); !ok || p.Format("2006-01-02") != "2026-08-01" {
		t.Fatalf("PlannedDate should read the target line, got %v (%v)", p, ok)
	}

	onlyMilestone := Issue{Body: "kein Datum", Milestone: &Milestone{DueOn: &ms}}
	got, ok = onlyMilestone.DueDate()
	if !ok || got.Format("2006-01-02") != "2026-09-15" {
		t.Fatalf("milestone fallback failed, got %v (%v)", got, ok)
	}
	if got.Hour() != 0 {
		t.Fatalf("milestone due should be truncated to a day, got %v", got)
	}

	none := Issue{Body: "kein Datum", Milestone: &Milestone{Title: "v2"}}
	if _, ok := none.DueDate(); ok {
		t.Fatal("milestone without due_on should not produce a date")
	}
	if _, ok := (Issue{}).DueDate(); ok {
		t.Fatal("empty issue should not produce a date")
	}
}

// SetDue is the write side of the same convention: the date picker owns the
// canonical line, everything else in the body is left alone.
func TestSetDue(t *testing.T) {
	aug := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	sep := time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)

	if got := SetDue("", aug); got != "target date: 2026-08-01" {
		t.Fatalf("empty body: got %q", got)
	}
	if got := SetDue("Beschreibung.", aug); got != "target date: 2026-08-01\n\nBeschreibung." {
		t.Fatalf("insert: got %q", got)
	}
	// Replacing must not leave the old line's blank behind.
	if got := SetDue("target date: 2026-08-01\n\nBeschreibung.", sep); got != "target date: 2026-09-30\n\nBeschreibung." {
		t.Fatalf("replace: got %q", got)
	}
	// A line further down still counts as the canonical one.
	if got := SetDue("Beschreibung.\n\ntarget date: 2026-08-01", sep); got != "target date: 2026-09-30\n\nBeschreibung." {
		t.Fatalf("replace from below: got %q", got)
	}
	// A CRLF body: the stray carriage returns end up inside the trim, so the
	// rewritten body comes out with plain newlines rather than a mixture.
	if got := SetDue("target date: 2026-08-01\r\n\r\nBeschreibung.", sep); got != "target date: 2026-09-30\n\nBeschreibung." {
		t.Fatalf("crlf: got %q", got)
	}
	// A zero date clears the line and nothing else.
	if got := SetDue("target date: 2026-08-01\n\nBeschreibung.", time.Time{}); got != "Beschreibung." {
		t.Fatalf("clear: got %q", got)
	}
	if got := SetDue("Bitte bis due: 2026-08-01 fertig", time.Time{}); got != "Bitte bis due: 2026-08-01 fertig" {
		t.Fatalf("clear must not touch prose: got %q", got)
	}

	// Round trip: SetDue writes the planned (target) line, so ParsePlanned
	// reads it back — and ParseDue must not mistake it for a deadline.
	body := SetDue("Irgendein Text\n\n- [ ] Aufgabe", sep)
	got, ok := ParsePlanned(body)
	if !ok || !got.Equal(sep) {
		t.Fatalf("round trip failed: %v (%v) from %q", got, ok, body)
	}
	if _, ok := ParseDue(body); ok {
		t.Fatalf("a written target line must not parse as a deadline: %q", body)
	}
	if StripDue(body) != "Irgendein Text\n\n- [ ] Aufgabe" {
		t.Fatalf("StripDue should undo SetDue, got %q", StripDue(body))
	}
}
