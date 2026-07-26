package hub

import (
	"testing"
	"time"

	"github.com/niclasedge/git-planner-go/internal/config"
	"github.com/niclasedge/git-planner-go/internal/gh"
)

// The merge path is the one place where a bug is invisible: the page still
// renders, it just shows something that is no longer true. Hence the tests.

func issue(number int, state string, updated time.Time) gh.Issue {
	return gh.Issue{Number: number, State: state, UpdatedAt: updated, Repo: "o/r"}
}

func testHub(state string) *Hub {
	h := &Hub{issueStore: map[string]*repoIssues{}}
	h.cfg = &config.Config{}
	h.cfg.GitHub.Issues.State = state
	return h
}

func TestUpsertByNumber(t *testing.T) {
	t0 := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	list := []gh.Issue{issue(1, "open", t0), issue(2, "open", t0)}

	if !upsertByNumber(&list, issue(3, "open", t0)) {
		t.Fatal("appending a new issue should report a change")
	}
	if len(list) != 3 {
		t.Fatalf("want 3 issues, got %d", len(list))
	}

	newer := issue(2, "open", t0.Add(time.Minute))
	newer.Title = "updated"
	if !upsertByNumber(&list, newer) {
		t.Fatal("a newer copy should report a change")
	}
	if len(list) != 3 {
		t.Fatalf("upsert must replace, not append: got %d", len(list))
	}
	if list[1].Title != "updated" {
		t.Fatalf("want the newer copy, got %q", list[1].Title)
	}

	// The overlap window deliberately re-fetches what the last poll already saw.
	// Re-publishing the view for those would claim movement that did not happen.
	stale := issue(2, "open", t0)
	stale.Title = "stale"
	if upsertByNumber(&list, stale) {
		t.Fatal("an older copy must not report a change")
	}
	if list[1].Title != "updated" {
		t.Fatalf("older copy overwrote newer: %q", list[1].Title)
	}
	if upsertByNumber(&list, newer) {
		t.Fatal("the same timestamp must not report a change")
	}
}

func TestRemoveByNumber(t *testing.T) {
	t0 := time.Now()
	list := []gh.Issue{issue(1, "open", t0), issue(2, "open", t0), issue(3, "open", t0)}

	if !removeByNumber(&list, 2) {
		t.Fatal("removing a present issue should report a change")
	}
	if len(list) != 2 || list[0].Number != 1 || list[1].Number != 3 {
		t.Fatalf("wrong remainder: %+v", list)
	}
	if removeByNumber(&list, 99) {
		t.Fatal("removing an absent issue must not report a change")
	}
}

func TestMergeIssueDropsClosedWhenTrackingOpen(t *testing.T) {
	t0 := time.Now()
	h := testHub("open")
	h.issueStore["o/r"] = &repoIssues{Issues: []gh.Issue{issue(7, "open", t0)}}

	if !h.mergeIssue(issue(7, "closed", t0.Add(time.Minute))) {
		t.Fatal("closing a tracked issue is a change")
	}
	if got := len(h.issueStore["o/r"].Issues); got != 0 {
		t.Fatalf("a closed issue must leave the open list, still %d there", got)
	}
}

func TestMergeIssueKeepsClosedWhenTrackingAll(t *testing.T) {
	t0 := time.Now()
	h := testHub("all")
	h.issueStore["o/r"] = &repoIssues{Issues: []gh.Issue{issue(7, "open", t0)}}

	h.mergeIssue(issue(7, "closed", t0.Add(time.Minute)))
	list := h.issueStore["o/r"].Issues
	if len(list) != 1 || list[0].State != "closed" {
		t.Fatalf("state:all must keep the issue and record the closure: %+v", list)
	}
}

func TestMergeIssueAddsUnknownRepo(t *testing.T) {
	// A repo with no open issues at sweep time has no entry at all, so the first
	// issue opened in it arrives with nowhere to go unless the store grows.
	h := testHub("open")
	if !h.mergeIssue(issue(1, "open", time.Now())) {
		t.Fatal("a first issue in a repo is a change")
	}
	if got := len(h.issueStore["o/r"].Issues); got != 1 {
		t.Fatalf("want 1 issue in the new repo entry, got %d", got)
	}
}

func TestMergePRRemovesOnClose(t *testing.T) {
	t0 := time.Now()
	h := testHub("open")
	pr := issue(12, "open", t0)
	h.issueStore["o/r"] = &repoIssues{PRs: []gh.Issue{pr}}

	// A merged PR arrives as state "closed" — same handling as a closed one.
	if !h.mergePR(issue(12, "closed", t0.Add(time.Minute))) {
		t.Fatal("a merged PR is a change")
	}
	if got := len(h.issueStore["o/r"].PRs); got != 0 {
		t.Fatalf("a closed PR must leave the list, still %d there", got)
	}
}
