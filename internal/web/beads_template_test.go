package web

import (
	"strings"
	"testing"
	"time"

	"github.com/niclasedge/git-planner-go/internal/panel"
)

// The widget template is duck-typed: it only calls Sections(), Agenda() and
// ID(), so a stub stands in for the widget and the test owns the data.
type beadsStub struct {
	secs []*panel.BeadsRepo
	ag   *panel.BeadAgenda
}

func (s beadsStub) Sections() []*panel.BeadsRepo { return s.secs }
func (s beadsStub) Agenda() *panel.BeadAgenda    { return s.ag }
func (s beadsStub) ID() int                      { return 7 }

// mustReadCSS reads the embedded stylesheet, so tests can assert that a
// CSS-drawn effect (the nesting indent) still has its rule.
func mustReadCSS(t *testing.T) []byte {
	t.Helper()
	b, err := staticFS.ReadFile("static/css/app.css")
	if err != nil {
		t.Fatalf("reading app.css: %v", err)
	}
	return b
}

func TestRenderWidgetBeads(t *testing.T) {
	child := &panel.Bead{
		ID: "x-1.2", Title: "Blocked child", Status: "open", Type: "task",
		Priority: 3, BlockedBy: []string{"x-1.1"},
	}
	waiter := &panel.Bead{
		ID: "x-1.3", Title: "Comes after the epic", Status: "open", Type: "task",
		Priority: 1, Repo: "o/r", BlockedBy: []string{"x-1"},
	}
	epic := &panel.Bead{
		ID: "x-1", Title: "The epic", Status: "open", Type: "epic", Priority: 2,
		GHURL:       "https://github.com/o/r/issues/66",
		Repo:        "o/r",
		Labels:      []string{"wayfinder:map"},
		Description: "Der Plan.",
		Children:    []*panel.Bead{child},
		Waiters:     []*panel.Bead{waiter},
	}
	ready := &panel.Bead{ID: "x-3", Title: "Do it now", Status: "open", Type: "task", Priority: 1, Repo: "o/r"}
	// A fixed date in the past keeps the badge deterministic: dueBadge answers
	// "über" for anything before today, and the chip goes red.
	overdue := time.Date(2020, 3, 4, 0, 0, 0, 0, time.UTC)
	dated := &panel.Bead{
		ID: "x-9", Title: "Has a date", Status: "open", Type: "task",
		Priority: 2, Repo: "o/r", Due: &overdue,
	}
	out := render(t, "widget-beads", beadsStub{
		secs: []*panel.BeadsRepo{
			{
				Name: "o/r", Roots: []*panel.Bead{epic, ready, dated},
				ReadyList: []*panel.Bead{ready},
				// All is what the detail pane renders from — every open bead,
				// including the waiter three levels down.
				All:  []*panel.Bead{epic, child, waiter, ready, dated},
				Open: 5, Ready: 1, Closed: 1,
			},
			{Name: "o/empty", Missing: true},
		},
		ag: &panel.BeadAgenda{Total: 1, Groups: []panel.BeadAgendaGroup{
			{Title: "Überfällig", Overdue: true, Items: []*panel.Bead{dated}},
		}},
	})

	for _, want := range []string{
		"The epic",
		"https://github.com/o/r/issues/66", // migrated issues link back
		"Blocked child",
		"wartet auf x-1.1", // the blocker is named, not just a colour
		"o-blocked",        // and the dot is a hollow amber ring, not a filled grey one
		"5 offen",
		"keine Beads-DB", // a listed repo without an export stays visible
		"wayfinder:map",
		"Repositories", // the planner-style three panes
		"bd-detail",
		"o/r · x-1",                    // detail head names repo and bead
		"Der Plan.",                    // description lands in the detail pane
		"1 ready",                      // the rail leads with what is actionable, not with a trio
		"ohne Blocker, sofort machbar", // and the ready head says why it is first
		"Do it now",
		"Baum",                 // and the full tree follows under its own head
		"Comes after the epic", // a waiter nests under its blocker
		// The agenda leads the pane and spans repos, so a date is delivered
		// rather than merely stored.
		"Agenda",
		"Überfällig",
		"bd-sec-agenda",
		`title="fällig 4.3.2020"`, // the exact date stays reachable on hover
		"due-red",                 // and an overdue chip is red, not blue
		">4.3.<",                  // overdue reads as the date, not as "über"
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered widget-beads is missing %q", want)
		}
	}

	// Two chips for the one dated bead: its agenda row and its tree row. The
	// four undated beads carry none — a bead without a date looks exactly as it
	// did before the field existed.
	if got := strings.Count(out, `title="fällig 4.3.2020"`); got != 2 {
		t.Errorf("dated bead should get a chip in the agenda and in the tree, got %d", got)
	}

	// Children and waiters share the one nesting style: both indent with
	// bd-nest, and the class has its rule in app.css.
	if strings.Count(out, `class="bd-nest"`) < 2 {
		t.Error("child and waiter rows must both carry the bd-nest indent")
	}
	if !strings.Contains(string(mustReadCSS(t)), ".bd-nest") {
		t.Error("bd-nest needs its indent rule in app.css")
	}
	// Every bead renders a detail article that the click handler toggles.
	if !strings.Contains(out, `data-bead="x-1"`) || !strings.Contains(out, `data-bead="x-1.2"`) {
		t.Error("bead rows and detail articles must carry their data-bead")
	}
	// Row plus detail for the waiter. The detail pane renders from All, not from
	// a walk over Roots — a waiter hangs off Waiters, and such a walk would
	// leave it with a row that opens nothing.
	if got := strings.Count(out, `data-bead="x-1.3"`); got != 2 {
		t.Errorf("waiter needs a row and a detail article, got %d", got)
	}
	// The dated bead gets a third: its agenda row. That row carries data-bead so
	// the same click handler opens the same detail — an agenda entry you cannot
	// open would be a dead end.
	if got := strings.Count(out, `data-bead="x-9"`); got != 3 {
		t.Errorf("dated bead needs an agenda row, a tree row and a detail, got %d", got)
	}
}
