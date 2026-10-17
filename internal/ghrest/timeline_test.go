package ghrest_test

// Timeline eines Issues und die PRs zu einem Commit (zuletzt-erledigt-pr,
// #116). Das Format folgt der echten Antwort von
// GET /repos/{o}/{r}/issues/{n}/timeline: closed traegt commit_id oder null,
// cross-referenced traegt source.issue samt pull_request.merged_at.

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

func TestIssueTimelineUndPRsZuCommit(t *testing.T) {
	alle := protokollGitHub(t, func(a aufruf, w http.ResponseWriter) {
		switch a.Pfad {
		case "/repos/acme/alpha/issues/112/timeline":
			fmt.Fprint(w, `[
			  {"event":"labeled","created_at":"2026-09-28T16:21:33Z","commit_id":null},
			  {"event":"referenced","created_at":"2026-09-28T16:34:52Z","commit_id":"9a1a"},
			  {"event":"cross-referenced","created_at":"2026-09-28T16:34:54Z",
			   "source":{"type":"issue","issue":{"number":113,"title":"html-dateien: HTML (#112)","body":"geht zurück auf #112",
			     "html_url":"https://github.com/acme/alpha/pull/113","state":"closed",
			     "pull_request":{"merged_at":"2026-09-28T16:34:59Z","html_url":"https://github.com/acme/alpha/pull/113"},
			     "repository":{"full_name":"acme/alpha"}}}},
			  {"event":"cross-referenced","created_at":"2026-09-28T17:29:19Z",
			   "source":{"type":"issue","issue":{"number":114,"title":"ein Issue","body":"","html_url":"https://github.com/acme/alpha/issues/114",
			     "state":"closed","repository":{"full_name":"acme/alpha"}}}},
			  {"event":"closed","created_at":"2026-09-28T16:35:01Z","commit_id":null},
			  {"event":"closed","created_at":"2026-09-28T17:42:12Z","commit_id":"deea"}
			]`)
		case "/repos/acme/alpha/commits/deea/pulls":
			fmt.Fprint(w, `[{"number":115,"title":"html-dateien-baum (#114)","body":"Behebt #114","html_url":"https://github.com/acme/alpha/pull/115",
			  "state":"closed","merged_at":"2026-09-28T17:42:10Z","head":{"ref":"claude/upbeat","sha":"s115"},"base":{"ref":"main"}}]`)
		}
	})
	c := ghrest.Client{Token: "t"}
	es, err := c.IssueTimeline(context.Background(), "acme/alpha", 112)
	if err != nil {
		t.Fatal(err)
	}
	if len(es) != 6 {
		t.Fatalf("%d Ereignisse, erwartet 6: %+v", len(es), es)
	}
	if es[1].Ereignis != "referenced" || es[1].CommitID != "9a1a" || es[1].Zeit != "2026-09-28T16:34:52Z" {
		t.Errorf("referenced %+v", es[1])
	}
	q := es[2].Quelle
	if es[2].Ereignis != "cross-referenced" || q == nil || es[2].QuelleRepo != "acme/alpha" {
		t.Fatalf("cross-referenced %+v", es[2])
	}
	if q.Nummer != 113 || q.Titel != "html-dateien: HTML (#112)" || q.Text != "geht zurück auf #112" ||
		q.URL != "https://github.com/acme/alpha/pull/113" || !q.Gemergt || q.GemergtAm != "2026-09-28T16:34:59Z" {
		t.Errorf("Quelle #113 %+v", *q)
	}
	if es[3].Quelle != nil {
		t.Errorf("ein Issue als Quelle ist kein PR: %+v", *es[3].Quelle)
	}
	if es[4].Ereignis != "closed" || es[4].CommitID != "" || es[5].CommitID != "deea" {
		t.Errorf("closed %+v %+v", es[4], es[5])
	}

	prs, err := c.PRsZuCommit(context.Background(), "acme/alpha", "deea")
	if err != nil || len(prs) != 1 {
		t.Fatalf("PRsZuCommit %+v %v", prs, err)
	}
	if p := prs[0]; p.Nummer != 115 || !p.Gemergt || p.GemergtAm != "2026-09-28T17:42:10Z" || p.URL != "https://github.com/acme/alpha/pull/115" {
		t.Errorf("PR %+v", p)
	}
	pfade := map[string]bool{}
	for _, x := range *alle {
		pfade[x.Pfad] = true
	}
	if !pfade["/repos/acme/alpha/issues/112/timeline"] || !pfade["/repos/acme/alpha/commits/deea/pulls"] {
		t.Errorf("Aufrufe %+v", *alle)
	}
}

func TestIssueTimelineFehler(t *testing.T) {
	protokollGitHub(t, func(a aufruf, w http.ResponseWriter) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"message":"kaputt"}`)
	})
	c := ghrest.Client{Token: "t"}
	if _, err := c.IssueTimeline(context.Background(), "acme/alpha", 1); err == nil {
		t.Error("500 ohne Fehler")
	}
	if _, err := c.IssueTimeline(context.Background(), "kein-repo", 1); err == nil {
		t.Error("ungueltiges Repo ohne Fehler")
	}
}
