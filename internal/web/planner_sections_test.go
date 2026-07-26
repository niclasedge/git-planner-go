package web

import (
	"testing"

	"github.com/niclasedge/git-planner-go/internal/gh"
)

// buildIssueSections is a pure function, so the whole test is table-driven
// without any HTTP or network fixture.

func wfMap(number int) gh.Issue {
	return gh.Issue{
		Number: number,
		Repo:   "o/r",
		Labels: []gh.Label{{Name: "wayfinder:map", Color: "ff0000"}},
	}
}

func wfTask(number int) gh.Issue {
	return gh.Issue{
		Number: number,
		Repo:   "o/r",
		Labels: []gh.Label{{Name: "wayfinder:task", Color: "00ff00"}},
	}
}

func plain(number int) gh.Issue {
	return gh.Issue{
		Number: number,
		Repo:   "o/r",
		Labels: []gh.Label{{Name: "bug", Color: "ff0000"}},
	}
}

func TestBuildIssueSections_ChildNotTopLevel(t *testing.T) {
	// #1 is a map with sub-issue #2. #2 should appear only under #1 at depth 1,
	// never at the top level.
	issues := []gh.Issue{
		func() gh.Issue { is := wfMap(1); is.Sub.Total = 2; is.Sub.Completed = 1; return is }(),
		wfTask(2),
	}
	kids := map[string][]int{
		gh.IssueKey("o/r", 1): {2},
	}

	sections := buildIssueSections(issues, kids)

	if len(sections) != 1 {
		t.Fatalf("expected 1 section, got %d", len(sections))
	}
	if sections[0].Title != "Wayfinder" {
		t.Fatalf("expected Wayfinder section, got %q", sections[0].Title)
	}
	if len(sections[0].Rows) != 2 {
		t.Fatalf("expected 2 rows (parent + child), got %d", len(sections[0].Rows))
	}
	// Parent first.
	if sections[0].Rows[0].Issue.Number != 1 {
		t.Fatalf("expected parent #1 first, got #%d", sections[0].Rows[0].Issue.Number)
	}
	if sections[0].Rows[0].Depth != 0 {
		t.Fatalf("parent should be depth 0, got %d", sections[0].Rows[0].Depth)
	}
	// Child second, depth 1.
	if sections[0].Rows[1].Issue.Number != 2 {
		t.Fatalf("expected child #2 second, got #%d", sections[0].Rows[1].Issue.Number)
	}
	if sections[0].Rows[1].Depth != 1 {
		t.Fatalf("child should be depth 1, got %d", sections[0].Rows[1].Depth)
	}
}

func TestBuildIssueSections_WayfinderBeforeNormal(t *testing.T) {
	// A wayfinder map should appear in the Wayfinder section first, followed by
	// the normal section.
	issues := []gh.Issue{
		plain(1),
		wfMap(2),
	}
	sections := buildIssueSections(issues, nil)

	if len(sections) != 2 {
		t.Fatalf("expected 2 sections, got %d", len(sections))
	}
	if sections[0].Title != "Wayfinder" {
		t.Fatalf("expected Wayfinder first, got %q", sections[0].Title)
	}
	if sections[1].Title != "Issues" {
		t.Fatalf("expected Issues second, got %q", sections[1].Title)
	}
}

func TestBuildIssueSections_MapBeforeOtherWayfinder(t *testing.T) {
	// Within the Wayfinder section, the map must come before other wayfinder labels.
	issues := []gh.Issue{
		wfTask(1),
		wfMap(2),
	}
	sections := buildIssueSections(issues, nil)

	if len(sections) != 1 {
		t.Fatalf("expected 1 section, got %d", len(sections))
	}
	if sections[0].Rows[0].Issue.Number != 2 {
		t.Fatalf("map #2 should come before task #1, got #%d first", sections[0].Rows[0].Issue.Number)
	}
	if sections[0].Rows[1].Issue.Number != 1 {
		t.Fatalf("task #1 should come second, got #%d", sections[0].Rows[1].Issue.Number)
	}
}

func TestBuildIssueSections_ClosedChildNotRendered(t *testing.T) {
	// A child that is not in the issue list (closed) produces no row, but the
	// parent's Sub summary stays intact.
	parent := wfMap(1)
	parent.Sub.Total = 2
	parent.Sub.Completed = 1
	issues := []gh.Issue{parent}
	// kids references #2, but #2 is not in the issue list (it's closed).
	kids := map[string][]int{
		gh.IssueKey("o/r", 1): {2},
	}

	sections := buildIssueSections(issues, kids)

	if len(sections) != 1 {
		t.Fatalf("expected 1 section, got %d", len(sections))
	}
	if len(sections[0].Rows) != 1 {
		t.Fatalf("expected only the parent row, got %d rows", len(sections[0].Rows))
	}
	if sections[0].Rows[0].Issue.Number != 1 {
		t.Fatalf("expected parent #1, got #%d", sections[0].Rows[0].Issue.Number)
	}
	// Sub summary must be unchanged — the bar still shows "1/2".
	if sections[0].Rows[0].Issue.Sub.Total != 2 || sections[0].Rows[0].Issue.Sub.Completed != 1 {
		t.Fatal("parent's Sub summary must not be touched")
	}
}

func TestBuildIssueSections_NormalParentWithKids(t *testing.T) {
	// A parent without wayfinder labels lands in the normal section, with children
	// indented beneath.
	parent := plain(1)
	parent.Sub.Total = 1
	issues := []gh.Issue{parent, plain(2)}
	kids := map[string][]int{
		gh.IssueKey("o/r", 1): {2},
	}

	sections := buildIssueSections(issues, kids)

	if len(sections) != 1 {
		t.Fatalf("expected 1 section, got %d", len(sections))
	}
	if sections[0].Title != "Issues" {
		t.Fatalf("expected Issues section, got %q", sections[0].Title)
	}
	if len(sections[0].Rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(sections[0].Rows))
	}
	if sections[0].Rows[1].Depth != 1 {
		t.Fatalf("child should be depth 1, got %d", sections[0].Rows[1].Depth)
	}
}

func TestBuildIssueSections_CycleTerminates(t *testing.T) {
	// A cycle (A→B, B→A) must not loop forever and each issue must appear at
	// most once.
	issues := []gh.Issue{wfMap(1), wfTask(2)}
	kids := map[string][]int{
		gh.IssueKey("o/r", 1): {2},
		gh.IssueKey("o/r", 2): {1},
	}

	sections := buildIssueSections(issues, kids)

	if len(sections) != 1 {
		t.Fatalf("expected 1 section, got %d", len(sections))
	}
	if len(sections[0].Rows) > 2 {
		t.Fatalf("cycle should stop at 2 rows, got %d", len(sections[0].Rows))
	}
	// Verify no duplicate numbers.
	seen := map[int]bool{}
	for _, r := range sections[0].Rows {
		if seen[r.Issue.Number] {
			t.Fatalf("issue #%d appears more than once", r.Issue.Number)
		}
		seen[r.Issue.Number] = true
	}
}

func TestBuildIssueSections_ForeignRepoParentIgnored(t *testing.T) {
	// kids holds every repo's parents, not just the selected one's. An issue must
	// not be treated as a child because *another* repo's parent happens to have a
	// child with the same number — it would vanish from the pane entirely, since
	// its supposed parent is not in the list.
	issues := []gh.Issue{plain(12)}
	kids := map[string][]int{
		gh.IssueKey("o/other", 5): {12},
	}

	sections := buildIssueSections(issues, kids)

	if len(sections) != 1 || len(sections[0].Rows) != 1 {
		t.Fatalf("expected #12 as a single top-level row, got %+v", sections)
	}
	if sections[0].Rows[0].Depth != 0 {
		t.Fatalf("expected depth 0, got %d", sections[0].Rows[0].Depth)
	}
}

func TestBuildIssueSections_Empty(t *testing.T) {
	sections := buildIssueSections(nil, nil)
	if len(sections) != 0 {
		t.Fatalf("expected no sections, got %d", len(sections))
	}
}

func TestRowLabels_StripsWayfinderPrefix(t *testing.T) {
	d := &plannerData{}
	is := gh.Issue{
		Labels: []gh.Label{
			{Name: "wayfinder:map", Color: "ff0000"},
		},
	}
	lb := d.RowLabels(is)
	if len(lb.Shown) != 1 {
		t.Fatalf("expected 1 label, got %d", len(lb.Shown))
	}
	if lb.Shown[0].Name != "map" {
		t.Fatalf("expected stripped name 'map', got %q", lb.Shown[0].Name)
	}
	if lb.Shown[0].Color != "ff0000" {
		t.Fatalf("color should survive stripping")
	}
}

func TestRowLabels_TruncatesAtMax(t *testing.T) {
	d := &plannerData{}
	is := gh.Issue{
		Labels: []gh.Label{
			{Name: "a"}, {Name: "b"}, {Name: "c"}, {Name: "d"},
		},
	}
	lb := d.RowLabels(is)
	if len(lb.Shown) != rowLabelMax {
		t.Fatalf("expected %d shown, got %d", rowLabelMax, len(lb.Shown))
	}
	if lb.Extra != 1 {
		t.Fatalf("expected 1 extra, got %d", lb.Extra)
	}
}

func TestIssueKey(t *testing.T) {
	if got := gh.IssueKey("o/r", 42); got != "o/r#42" {
		t.Fatalf("expected o/r#42, got %q", got)
	}
}
