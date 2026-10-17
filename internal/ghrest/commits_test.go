package ghrest_test

// Commit und Vergleich fuer die Branches eines Issues (issue-branches, #50).

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

func TestCommitUndVergleich(t *testing.T) {
	sha := "abcdef1234567890abcdef1234567890abcdef12"
	alle := protokollGitHub(t, func(a aufruf, w http.ResponseWriter) {
		switch a.Pfad {
		case "/repos/acme/alpha/commits/" + sha:
			fmt.Fprint(w, `{"sha":"`+sha+`","commit":{"message":"Marke zeichnen\n\nzweite Zeile",
			  "author":{"date":"2026-09-20T08:00:00Z"},"committer":{"date":"2026-09-24T10:30:00Z"}}}`)
		case "/repos/acme/alpha/compare/main..." + sha:
			fmt.Fprint(w, `{"status":"diverged","ahead_by":3,"behind_by":2,"total_commits":3}`)
		default:
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"message":"Not Found"}`)
		}
	})
	c := ghrest.Client{Token: "t"}
	k, err := c.Commit(context.Background(), "acme/alpha", sha)
	if err != nil {
		t.Fatalf("Commit: %v (Aufrufe %+v)", err, *alle)
	}
	if k.SHA != sha || k.Zeit != "2026-09-24T10:30:00Z" || k.Betreff != "Marke zeichnen" {
		t.Errorf("Commit %+v", k)
	}
	voraus, zurueck, err := c.Vergleich(context.Background(), "acme/alpha", "main", sha)
	if err != nil || voraus != 3 || zurueck != 2 {
		t.Errorf("Vergleich voraus=%d zurueck=%d err=%v", voraus, zurueck, err)
	}
	if _, _, err := c.Vergleich(context.Background(), "kein-repo", "main", sha); err == nil {
		t.Errorf("Vergleich mit ungueltigem Repo ohne Fehler")
	}
}

func TestPRTraegtText(t *testing.T) {
	protokollGitHub(t, func(a aufruf, w http.ResponseWriter) {
		fmt.Fprint(w, `[{"number":5,"title":"login","body":"Closes #7","head":{"ref":"feature/login","sha":"s5"},"base":{"ref":"main"}}]`)
	})
	prs, err := ghrest.Client{Token: "t"}.PullRequests(context.Background(), "acme/alpha")
	if err != nil || len(prs) != 1 || prs[0].Text != "Closes #7" {
		t.Fatalf("PullRequests %+v %v", prs, err)
	}
}
