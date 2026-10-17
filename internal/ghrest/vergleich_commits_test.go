package ghrest_test

// Commits und Dateien eines Vergleichs fuer die Zeitleiste und die Zaehler
// blocking/total_blocking am Issue (aktionen-zeitleiste, #128).

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

func TestVergleichMitCommits(t *testing.T) {
	var pfade []string
	fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		pfade = append(pfade, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/repos/acme/alpha/compare/a0...a2" {
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"message":"Not Found"}`)
			return
		}
		fmt.Fprint(w, `{"ahead_by":2,"behind_by":0,"total_commits":2,
		 "commits":[
		  {"sha":"a1","commit":{"message":"eins\n\nRumpf","author":{"name":"Alex","date":"2026-09-30T10:00:00Z"},
		   "committer":{"date":"2026-09-30T10:01:00Z"}},"author":{"login":"alex"}},
		  {"sha":"a2","commit":{"message":"zwei","author":{"name":"Ohne Login","date":"2026-09-30T11:00:00Z"},
		   "committer":{"date":"2026-09-30T11:01:00Z"}},"author":null}
		 ],
		 "files":[{"filename":"openspec/changes/k/belege/assets/0-k.png","status":"added","sha":"b1"},
		  {"filename":"alt.png","status":"removed","sha":"x1"}]}`)
	})
	v, err := ghrest.Client{Token: "t"}.VergleichMitCommits(context.Background(), "acme/alpha", "a0", "a2")
	if err != nil {
		t.Fatalf("VergleichMitCommits: %v (Aufrufe %v)", err, pfade)
	}
	if len(v.Commits) != 2 {
		t.Fatalf("%d Commits, erwartet 2: %+v", len(v.Commits), v.Commits)
	}
	if c := v.Commits[0]; c.SHA != "a1" || c.Betreff != "eins" || c.Autor != "alex" || c.Zeit != "2026-09-30T10:01:00Z" {
		t.Errorf("erster Commit %+v", c)
	}
	if c := v.Commits[1]; c.SHA != "a2" || c.Betreff != "zwei" || c.Autor != "Ohne Login" {
		t.Errorf("zweiter Commit %+v", c)
	}
	if len(v.Dateien) != 2 || v.Dateien[0].Pfad != "openspec/changes/k/belege/assets/0-k.png" || v.Dateien[0].Status != "added" ||
		v.Dateien[0].SHA != "b1" || v.Dateien[1].Status != "removed" {
		t.Errorf("Dateien %+v", v.Dateien)
	}
	if _, err := (ghrest.Client{Token: "t"}).VergleichMitCommits(context.Background(), "acme/alpha", "x0", "x1"); err == nil {
		t.Errorf("Vergleich bei 404 ohne Fehler")
	}
}

func TestIssueTraegtBlocktZaehler(t *testing.T) {
	fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[{"number":4,"title":"Vier","html_url":"u4","user":{"login":"ana"},"labels":[],"assignees":[],
		  "issue_dependencies_summary":{"blocked_by":0,"total_blocked_by":1,"blocking":2,"total_blocking":3}}]`)
	})
	alle, err := ghrest.Client{Token: "t"}.OffeneIssues(context.Background(), "acme/alpha")
	if err != nil || len(alle) != 1 {
		t.Fatalf("OffeneIssues %v %+v", err, alle)
	}
	if i := alle[0]; i.Blockt != 2 || i.AlleBlockt != 3 || i.AlleBlocker != 1 {
		t.Errorf("Zaehler blockt %d alle_blockt %d alle_blocker %d, erwartet 2, 3, 1", i.Blockt, i.AlleBlockt, i.AlleBlocker)
	}
}
