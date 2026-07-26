package web

import (
	"strconv"
	"strings"
	"testing"

	"github.com/niclasedge/git-planner-go/internal/gh"
)

// guideStr renders a row's gutter so a failure prints the tree instead of a
// slice of numbers.
func guideStr(gs []treeGuide) string {
	parts := make([]string, len(gs))
	for i, g := range gs {
		parts[i] = g.String()
	}
	return "[" + strings.Join(parts, " ") + "]"
}

// wantTree asserts the whole flattened tree in one go: issue number plus gutter
// per row, in order.
func wantTree(t *testing.T, rows []issueRow, want []string) {
	t.Helper()
	got := make([]string, len(rows))
	for i, r := range rows {
		got[i] = "#" + strconv.Itoa(r.Issue.Number) + guideStr(r.Guides)
	}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("tree mismatch\n got: %s\nwant: %s", strings.Join(got, " "), strings.Join(want, " "))
	}
}

func TestTreeGuides_TeeThenElbow(t *testing.T) {
	// #1 with two children: the first gets a tee (a sibling follows), the last an
	// elbow.
	issues := []gh.Issue{wfMap(1), wfTask(2), wfTask(3)}
	kids := map[string][]int{gh.IssueKey("o/r", 1): {2, 3}}

	sections := buildIssueSections(issues, kids)
	if len(sections) != 1 {
		t.Fatalf("expected 1 section, got %d", len(sections))
	}
	wantTree(t, sections[0].Rows, []string{"#1[]", "#2[tee]", "#3[elbow]"})
}

func TestTreeGuides_GrandchildLineWhenParentHasSiblings(t *testing.T) {
	// #2 is a middle child, so the column above #4 has to keep drawing a trunk —
	// otherwise #3 would look like it belongs to #4's group.
	issues := []gh.Issue{wfMap(1), wfTask(2), wfTask(3), wfTask(4)}
	kids := map[string][]int{
		gh.IssueKey("o/r", 1): {2, 3},
		gh.IssueKey("o/r", 2): {4},
	}

	sections := buildIssueSections(issues, kids)
	wantTree(t, sections[0].Rows, []string{
		"#1[]", "#2[tee]", "#4[line elbow]", "#3[elbow]",
	})
}

func TestTreeGuides_GrandchildBlankWhenParentIsLast(t *testing.T) {
	// #2 is the last child, so nothing continues below it: #4's first column is
	// empty, not a trunk.
	issues := []gh.Issue{wfMap(1), wfTask(2), wfTask(4)}
	kids := map[string][]int{
		gh.IssueKey("o/r", 1): {2},
		gh.IssueKey("o/r", 2): {4},
	}

	sections := buildIssueSections(issues, kids)
	wantTree(t, sections[0].Rows, []string{"#1[]", "#2[elbow]", "#4[blank elbow]"})
}

// TestTreeGuides_ElbowFollowsDrawnChildren is the reason walk pre-filters: GitHub
// says #1 has three children, but #3 is closed and therefore not in this list.
// The elbow has to land on #4, the last child actually drawn.
func TestTreeGuides_ElbowFollowsDrawnChildren(t *testing.T) {
	issues := []gh.Issue{wfMap(1), wfTask(2), wfTask(4)}
	kids := map[string][]int{gh.IssueKey("o/r", 1): {2, 3, 4}}

	sections := buildIssueSections(issues, kids)
	wantTree(t, sections[0].Rows, []string{"#1[]", "#2[tee]", "#4[elbow]"})
}

// Depth stays len(Guides): the CSS uses the guides, but .ir-child still keys off
// depth.
func TestTreeGuides_DepthMatchesGuideCount(t *testing.T) {
	issues := []gh.Issue{plain(1), plain(2), plain(3)}
	kids := map[string][]int{
		gh.IssueKey("o/r", 1): {2},
		gh.IssueKey("o/r", 2): {3},
	}

	sections := buildIssueSections(issues, kids)
	for _, r := range sections[0].Rows {
		if r.Depth != len(r.Guides) {
			t.Fatalf("#%d: depth %d but %d guides", r.Issue.Number, r.Depth, len(r.Guides))
		}
	}
}

// childTrail must not alias its input — every row keeps the slice it was given.
func TestChildTrail_DoesNotAliasParent(t *testing.T) {
	parent := []treeGuide{guideTee}
	first := childTrail(parent, false)
	childTrail(parent, true)

	if parent[0] != guideTee {
		t.Fatalf("parent trail was mutated: %s", guideStr(parent))
	}
	if first[0] != guideLine || first[1] != guideTee {
		t.Fatalf("first child trail changed under us: %s", guideStr(first))
	}
}

func TestSplitLogins(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"alice", []string{"alice"}},
		{"@alice, @bob", []string{"alice", "bob"}},
		{"alice bob   alice", []string{"alice", "bob"}}, // duplicates collapse
		{" , ; ", nil},
	}
	for _, c := range cases {
		got := splitLogins(c.in)
		if len(got) != len(c.want) {
			t.Fatalf("splitLogins(%q) = %v, want %v", c.in, got, c.want)
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Fatalf("splitLogins(%q) = %v, want %v", c.in, got, c.want)
			}
		}
	}
}

func TestSameSet(t *testing.T) {
	if !sameSet([]string{"Bug", "wip"}, []string{"wip", "bug"}) {
		t.Fatal("expected case- and order-insensitive equality")
	}
	if sameSet([]string{"bug"}, []string{"bug", "wip"}) {
		t.Fatal("different lengths must not compare equal")
	}
	if !sameSet(nil, []string{}) {
		t.Fatal("nil and empty are the same set")
	}
}
