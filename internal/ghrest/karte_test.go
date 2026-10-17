package ghrest_test

// Sub-Issues, Summenfelder und alle Blocker fuer die Karte (repo-karte, #36).

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

func TestSubIssuesUndSummen(t *testing.T) {
	protokollGitHub(t, func(a aufruf, w http.ResponseWriter) {
		switch a.Pfad {
		case "/repos/acme/alpha/issues/8/sub_issues":
			fmt.Fprint(w, `[{"number":9,"title":"neun","state":"closed","created_at":"2026-09-02T00:00:00Z","closed_at":"2026-09-05T00:00:00Z"},
			 {"number":10,"title":"zehn","state":"open","created_at":"2026-09-03T00:00:00Z"}]`)
		case "/repos/acme/alpha/issues/11":
			fmt.Fprint(w, `{"number":11,"state":"open","created_at":"2026-09-04T00:00:00Z",
			  "sub_issues_summary":{"total":4,"completed":3},"parent_issue_url":"https://api.github.com/repos/acme/alpha/issues/8",
			  "issue_dependencies_summary":{"blocked_by":0,"total_blocked_by":2}}`)
		case "/repos/acme/alpha/issues/11/dependencies/blocked_by":
			fmt.Fprint(w, `[{"number":10,"state":"open","repository_url":"https://api.github.com/repos/acme/alpha"},
			 {"number":5,"state":"closed","repository_url":"https://api.github.com/repos/acme/alpha"},
			 {"number":7,"state":"closed","repository_url":"https://api.github.com/repos/andere/repo"}]`)
		}
	})
	c := ghrest.Client{Token: "tok-lang-genug"}
	subs, err := c.SubIssues(context.Background(), "acme/alpha", 8)
	if err != nil || len(subs) != 2 || subs[0].Nummer != 9 || subs[0].Zustand != "closed" || subs[0].Erstellt != "2026-09-02T00:00:00Z" ||
		subs[0].Geschlossen != "2026-09-05T00:00:00Z" || subs[1].Zustand != "open" {
		t.Fatalf("SubIssues %+v %v", subs, err)
	}
	i, err := c.Issue(context.Background(), "acme/alpha", 11)
	if err != nil || i.SubGesamt != 4 || i.SubErledigt != 3 || i.ParentNummer != 8 || i.AlleBlocker != 2 || i.Erstellt == "" {
		t.Errorf("Felder %+v %v", i, err)
	}
	alle, err := c.AlleBlockerVon(context.Background(), "acme/alpha", 11)
	if err != nil || fmt.Sprint(alle) != "[5 10]" {
		t.Errorf("AlleBlockerVon %v %v, erwartet [5 10] (auch geschlossene, nur dieses Repo)", alle, err)
	}
	if _, err := c.SubIssues(context.Background(), "../x", 8); err == nil || !strings.Contains(err.Error(), "ungueltig") {
		t.Errorf("ungueltiges Repo: %v", err)
	}
}

func TestGeschlosseneIssuesSeit(t *testing.T) {
	var since string
	fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		since = r.URL.Query().Get("since")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[{"number":8,"state":"closed","closed_at":"2026-09-20T00:00:00Z"},
		 {"number":4,"state":"closed","closed_at":"2026-09-01T00:00:00Z","updated_at":"2026-09-19T00:00:00Z"},
		 {"number":9,"state":"closed","closed_at":"2026-09-21T00:00:00Z","pull_request":{}}]`)
	})
	seit := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
	zu, err := ghrest.Client{Token: "tok-lang-genug"}.GeschlosseneIssuesSeit(context.Background(), "acme/alpha", seit)
	if err != nil {
		t.Fatal(err)
	}
	if since != "2026-09-10T00:00:00Z" {
		t.Errorf("since=%q", since)
	}
	if len(zu) != 1 || zu[0].Nummer != 8 {
		t.Errorf("geschlossen %+v, erwartet nur #8 (nach seit geschlossen, kein PR)", zu)
	}
}
