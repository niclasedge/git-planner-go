package panel

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fixture mirrors what `bd export` writes: one issue per line, dependencies
// inline, parent-child expressed as a dependency of the child.
const beadsFixture = `{"_type":"issue","id":"x-epic","title":"The map","status":"open","priority":2,"issue_type":"epic","labels":["wayfinder:map"],"external_ref":"gh-66"}
{"_type":"issue","id":"x-epic.1","title":"Second stage","status":"open","priority":2,"issue_type":"task","dependencies":[{"issue_id":"x-epic.1","depends_on_id":"x-epic","type":"parent-child"}]}
{"_type":"issue","id":"x-epic.2","title":"Third stage","status":"open","priority":3,"issue_type":"task","dependencies":[{"issue_id":"x-epic.2","depends_on_id":"x-epic","type":"parent-child"},{"issue_id":"x-epic.2","depends_on_id":"x-epic.1","type":"blocks"}]}
{"_type":"issue","id":"x-done","title":"Finished","status":"closed","priority":1,"issue_type":"task"}
{"_type":"issue","id":"x-freed","title":"Blocker is closed","status":"open","priority":1,"issue_type":"bug","external_ref":"gh-61","dependencies":[{"issue_id":"x-freed","depends_on_id":"x-done","type":"blocks"}]}
{"_type":"issue","id":"x-rel","title":"Merely related","status":"in_progress","priority":4,"issue_type":"task","dependencies":[{"issue_id":"x-rel","depends_on_id":"x-freed","type":"related"}]}
{"_type":"message","id":"x-msg","title":"not an issue","status":"open"}
`

func TestParseBeads_TreeAndBlocking(t *testing.T) {
	roots, readyList, open, ready, closed, err := parseBeads(strings.NewReader(beadsFixture), "o/r")
	if err != nil {
		t.Fatalf("parseBeads: %v", err)
	}
	if open != 5 || closed != 1 {
		t.Fatalf("open=%d closed=%d, want 5/1", open, closed)
	}

	// Epic first, then the two loose issues by priority.
	if len(roots) != 3 || roots[0].ID != "x-epic" || roots[1].ID != "x-freed" || roots[2].ID != "x-rel" {
		ids := []string{}
		for _, r := range roots {
			ids = append(ids, r.ID)
		}
		t.Fatalf("roots = %v, want [x-epic x-freed x-rel]", ids)
	}

	epic := roots[0]
	if len(epic.Children) != 2 || epic.Children[0].ID != "x-epic.1" || epic.Children[1].ID != "x-epic.2" {
		t.Fatalf("epic children = %+v", epic.Children)
	}

	// x-epic.2 waits on the open x-epic.1; x-freed's blocker is closed and
	// blocks nothing; a related dependency never blocks.
	if got := epic.Children[1].BlockedBy; len(got) != 1 || got[0] != "x-epic.1" {
		t.Fatalf("x-epic.2 blocked by %v, want [x-epic.1]", got)
	}
	if roots[1].Blocked() {
		t.Fatalf("x-freed must not be blocked by a closed issue")
	}
	if roots[2].Blocked() {
		t.Fatalf("a related dependency must not block")
	}

	// Ready = open, unblocked, childless: x-epic.1 and x-freed. The epic has
	// children, x-rel is in_progress, x-epic.2 is blocked.
	if ready != 2 {
		t.Fatalf("ready = %d, want 2", ready)
	}
	if len(readyList) != 2 || readyList[0].ID != "x-freed" || readyList[1].ID != "x-epic.1" {
		ids := []string{}
		for _, b := range readyList {
			ids = append(ids, b.ID)
		}
		t.Fatalf("readyList = %v, want [x-freed x-epic.1] (priority order)", ids)
	}
}

func TestParseBeads_GHURLOnlyForGhRefs(t *testing.T) {
	roots, _, _, _, _, err := parseBeads(strings.NewReader(beadsFixture), "o/r")
	if err != nil {
		t.Fatalf("parseBeads: %v", err)
	}
	if got := roots[0].GHURL; got != "https://github.com/o/r/issues/66" {
		t.Fatalf("epic GHURL = %q", got)
	}
	if got := roots[2].GHURL; got != "" {
		t.Fatalf("issue without external_ref got GHURL %q", got)
	}
	if got := ghIssueURL("o/r", "jira-ABC"); got != "" {
		t.Fatalf("non-gh ref resolved to %q", got)
	}
}

func TestBeadsInit_Validation(t *testing.T) {
	if err := (&Beads{}).Init(); err == nil {
		t.Fatal("no repos must be rejected")
	}
	if err := (&Beads{Repos: []string{"not-a-repo"}, TokenEnv: "X"}).Init(); err == nil {
		t.Fatal("repo without owner/ must be rejected")
	}
	if err := (&Beads{Repos: []string{"o/r"}}).Init(); err == nil {
		t.Fatal("missing token-env must be rejected")
	}

	// An empty variable is a banner, not a dead widget.
	t.Setenv("BEADS_TEST_EMPTY", "")
	w := &Beads{Repos: []string{"o/r"}, TokenEnv: "BEADS_TEST_EMPTY"}
	if err := w.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	w.Update(context.Background())
	if w.Err() == nil || !strings.Contains(w.Err().Error(), "BEADS_TEST_EMPTY") {
		t.Fatalf("expected the banner to name the variable, got %v", w.Err())
	}
}

// The fetch cycle: a 200 fills the section and stores the ETag, the next round
// sends If-None-Match and a 304 keeps the data, a 404 empties it into
// "Missing", and a 500 keeps the old tree next to the error.
func TestBeadsUpdate_ConditionalFetch(t *testing.T) {
	var status int
	var gotIfNoneMatch string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/contents/.beads/issues.jsonl" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		if got := r.Header.Get("Accept"); got != "application/vnd.github.raw+json" {
			t.Errorf("Accept = %q", got)
		}
		gotIfNoneMatch = r.Header.Get("If-None-Match")
		switch status {
		case http.StatusOK:
			w.Header().Set("ETag", `"v1"`)
			w.Write([]byte(beadsFixture))
		default:
			w.WriteHeader(status)
		}
	}))
	defer srv.Close()

	t.Setenv("BEADS_TEST_TOKEN", "t")
	w := &Beads{Repos: []string{"o/r"}, TokenEnv: "BEADS_TEST_TOKEN", apiBase: srv.URL}
	if err := w.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}

	status = http.StatusOK
	w.Update(context.Background())
	sec := w.Sections()[0]
	if sec.Err != "" || sec.Missing || len(sec.Roots) != 3 || sec.Open != 5 {
		t.Fatalf("after 200: %+v", sec)
	}

	status = http.StatusNotModified
	w.Update(context.Background())
	if gotIfNoneMatch != `"v1"` {
		t.Fatalf("second round sent If-None-Match %q, want the stored ETag", gotIfNoneMatch)
	}
	if sec = w.Sections()[0]; len(sec.Roots) != 3 || sec.Err != "" {
		t.Fatalf("a 304 must keep the data, got %+v", sec)
	}

	status = http.StatusInternalServerError
	w.Update(context.Background())
	if sec = w.Sections()[0]; sec.Err == "" || len(sec.Roots) != 3 {
		t.Fatalf("a 500 must keep the old tree beside the error, got %+v", sec)
	}

	status = http.StatusNotFound
	w.Update(context.Background())
	if sec = w.Sections()[0]; !sec.Missing || sec.Err != "" || len(sec.Roots) != 0 {
		t.Fatalf("after 404: %+v", sec)
	}
}
