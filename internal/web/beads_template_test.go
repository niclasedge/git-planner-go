package web

import (
	"strings"
	"testing"

	"github.com/niclasedge/git-planner-go/internal/panel"
)

// The widget template is duck-typed: it only calls Sections() and ID(), so a
// stub stands in for the widget and the test owns the data.
type beadsStub struct{ secs []*panel.BeadsRepo }

func (s beadsStub) Sections() []*panel.BeadsRepo { return s.secs }
func (s beadsStub) ID() int                      { return 7 }

func TestRenderWidgetBeads(t *testing.T) {
	child := &panel.Bead{
		ID: "x-1.2", Title: "Blocked child", Status: "open", Type: "task",
		Priority: 3, BlockedBy: []string{"x-1.1"},
	}
	epic := &panel.Bead{
		ID: "x-1", Title: "The epic", Status: "open", Type: "epic", Priority: 2,
		GHURL:       "https://github.com/o/r/issues/66",
		Repo:        "o/r",
		Labels:      []string{"wayfinder:map"},
		Description: "Der Plan.",
		Children:    []*panel.Bead{child},
	}
	out := render(t, "widget-beads", beadsStub{secs: []*panel.BeadsRepo{
		{Name: "o/r", Roots: []*panel.Bead{epic}, Open: 2, Ready: 0, Closed: 1},
		{Name: "o/empty", Missing: true},
	}})

	for _, want := range []string{
		"The epic",
		"https://github.com/o/r/issues/66", // migrated issues link back
		"Blocked child",
		"wartet auf x-1.1", // the blocker is named, not just a colour
		"o-queued",         // and colours the dot as waiting
		"2 offen",
		"keine Beads-DB", // a listed repo without an export stays visible
		"wayfinder:map",
		"Repositories", // the planner-style three panes
		"bd-detail",
		"o/r · x-1", // detail head names repo and bead
		"Der Plan.", // description lands in the detail pane
		"2/0/1",     // repo trio: offen / ready / erledigt
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered widget-beads is missing %q", want)
		}
	}

	// The child indents under its epic.
	if !strings.Contains(out, "bead-child") {
		t.Error("child rows must carry the bead-child indent")
	}
	// Every bead renders a detail article that the click handler toggles.
	if !strings.Contains(out, `data-bead="x-1"`) || !strings.Contains(out, `data-bead="x-1.2"`) {
		t.Error("bead rows and detail articles must carry their data-bead")
	}
}
