package web

import (
	"strings"
	"testing"

	"github.com/niclasedge/git-planner-go/internal/panel"
)

// The widget template is duck-typed: it only calls Sections(), so a stub
// stands in for the widget and the test owns the data.
type beadsStub struct{ secs []*panel.BeadsRepo }

func (s beadsStub) Sections() []*panel.BeadsRepo { return s.secs }

func TestRenderWidgetBeads(t *testing.T) {
	child := &panel.Bead{
		ID: "x-1.2", Title: "Blocked child", Status: "open", Type: "task",
		Priority: 3, BlockedBy: []string{"x-1.1"},
	}
	epic := &panel.Bead{
		ID: "x-1", Title: "The epic", Status: "open", Type: "epic", Priority: 2,
		GHURL:    "https://github.com/o/r/issues/66",
		Labels:   []string{"wayfinder:map"},
		Children: []*panel.Bead{child},
	}
	out := render(t, "widget-beads", beadsStub{secs: []*panel.BeadsRepo{
		{Name: "o/r", Roots: []*panel.Bead{epic}, Open: 2, Ready: 0, Closed: 1},
		{Name: "o/empty", Missing: true},
	}})

	for _, want := range []string{
		"The epic",
		"https://github.com/o/r/issues/66", // migrated issues link back
		"Blocked child",
		"wartet auf x-1.1",     // the blocker is named, not just a colour
		"o-queued",             // and colours the dot as waiting
		"2 offen",
		"keine Beads-DB",       // a listed repo without an export stays visible
		"wayfinder:map",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered widget-beads is missing %q", want)
		}
	}

	// The child indents under its epic.
	if !strings.Contains(out, "bead-child") {
		t.Error("child rows must carry the bead-child indent")
	}
}
