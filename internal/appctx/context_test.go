package appctx

import "testing"

// TestBuildContext_ReportsCardSkippedInOverview confirms the "reports" card
// follows the same anyOr(skip, na, ...) gate as project/git/knowledge/etc. —
// the overview view (SkipProject=true) must never shell out to
// git/bd/dot for a repo that isn't even the active project.
func TestBuildContext_ReportsCardSkippedInOverview(t *testing.T) {
	dir := t.TempDir()
	ctx := BuildContext(dir, "", BuildOpts{SkipProject: true})
	cards, _ := ctx["cards"].(map[string]any)
	reportsCard, _ := cards["reports"].(map[string]any)
	if aBool(reportsCard["available"]) {
		t.Fatalf("expected the reports card to stay unavailable when SkipProject=true, got %+v", reportsCard)
	}
}

// TestBuildContext_ReportsCardComputedForProjectView confirms the reports
// card DOES compute (fail-open, since dir has no .beads/) when the project
// view actually asks for it.
func TestBuildContext_ReportsCardComputedForProjectView(t *testing.T) {
	dir := t.TempDir()
	ctx := BuildContext(dir, "", BuildOpts{SkipProject: false})
	cards, _ := ctx["cards"].(map[string]any)
	reportsCard, ok := cards["reports"].(ReportsView)
	if !ok {
		t.Fatalf("expected cards[\"reports\"] to be a ReportsView, got %T", cards["reports"])
	}
	if reportsCard.Available {
		t.Fatalf("expected Available=false for a dir without .beads/, got %+v", reportsCard)
	}
	if reportsCard.Error == "" {
		t.Fatalf("expected a fail-open Error message, got none: %+v", reportsCard)
	}
}

// TestFmtCostRund_KeineNachkommastellen haelt fest, was die Projekt-Rail
// zeigen soll: gerundete ganze Dollar, keine k-Form, und ein $0 statt eines
// Absturzes bei Unsinn. Der Rundungsfall ist der interessante — FmtCost
// schneidet nichts ab, hier wird kaufmaennisch gerundet.
func TestFmtCostRund_KeineNachkommastellen(t *testing.T) {
	faelle := []struct {
		in   any
		will string
	}{
		{145.02, "$145"},
		{145.5, "$146"},
		{145.49, "$145"},
		{0.0, "$0"},
		{0.4, "$0"},
		{1234.56, "$1235"}, // keine k-Form: "$1235" ist so kurz wie "$1.23k"
		{"87.3", "$87"},    // JSON liefert Kosten auch mal als String
		{nil, "$0"},
		{"keine zahl", "$0"},
	}
	for _, f := range faelle {
		if got := FmtCostRund(f.in); got != f.will {
			t.Errorf("FmtCostRund(%v) = %q, will %q", f.in, got, f.will)
		}
	}
}
