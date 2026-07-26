package gh

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/niclasedge/git-planner-go/internal/config"
)

// TestLiveIssuesSince talks to the real API. It exists because the incremental
// poll has a silent failure mode: if the repository field did not decode, every
// issue would be dropped as "not tracked" and the poll would report zero changes
// forever — indistinguishable from a quiet hour.
//
// Skipped without GITHUB_PAT, so the normal `go test ./...` stays offline. Run it
// with: set -a && . ./.env && set +a && go test ./internal/gh -run Live -v
func TestLiveIssuesSince(t *testing.T) {
	secret := os.Getenv("GITHUB_PAT")
	if secret == "" {
		t.Skip("GITHUB_PAT not set — skipping the live API check")
	}

	c := New([]config.Token{{Name: "live", Env: "GITHUB_PAT", Secret: secret}}, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	set, err := c.IssuesSince(ctx, c.Token("live"), time.Now().Add(-14*24*time.Hour))
	if err != nil {
		t.Fatalf("IssuesSince: %v", err)
	}
	if set.Requests == 0 {
		t.Fatal("no request was made")
	}
	total := len(set.Issues) + len(set.PRs)
	if total == 0 {
		t.Skip("no issue activity in the last 14 days — nothing to verify")
	}
	t.Logf("%d issues + %d PRs in %d request(s), truncated=%v",
		len(set.Issues), len(set.PRs), set.Requests, set.Truncated)

	for _, is := range append(append([]Issue{}, set.Issues...), set.PRs...) {
		if is.Repo == "" {
			t.Fatalf("issue #%d decoded without a repository — the poll would drop it", is.Number)
		}
		if is.Number == 0 {
			t.Fatal("issue decoded without a number")
		}
		if is.TokenName != "live" {
			t.Fatalf("issue #%d not tagged with its token", is.Number)
		}
	}
	for _, pr := range set.PRs {
		if !pr.IsPR() {
			t.Fatalf("#%d landed in PRs but is not one", pr.Number)
		}
	}
	for _, is := range set.Issues {
		if is.IsPR() {
			t.Fatalf("#%d is a PR but landed in Issues", is.Number)
		}
	}
}
