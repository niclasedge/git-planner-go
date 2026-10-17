package ghrest_test

// Branch-Schutz und geschlossene PRs (branch-aufraeumen, #78).

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

func TestBranchesTragenProtected(t *testing.T) {
	fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[{"name":"main","protected":true,"commit":{"sha":"s1"}},{"name":"alt","protected":false,"commit":{"sha":"s2"}}]`)
	})
	b, err := ghrest.Client{Token: "t"}.Branches(context.Background(), "acme/alpha")
	if err != nil || len(b) != 2 {
		t.Fatalf("Branches %+v %v", b, err)
	}
	if !b[0].Geschuetzt || b[0].SHA != "s1" || b[1].Geschuetzt || b[1].Name != "alt" {
		t.Errorf("Branches %+v", b)
	}
}

func TestGeschlossenePullRequests(t *testing.T) {
	var abfrage string
	fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/repos/acme/alpha/pulls" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		abfrage = r.URL.RawQuery
		fmt.Fprint(w, `[
		  {"number":5,"title":"Squash","html_url":"u5","state":"closed","merged_at":"2026-09-10T10:00:00Z",
		   "head":{"ref":"squash","sha":"s5","repo":{"full_name":"acme/alpha"}},"base":{"ref":"main"}},
		  {"number":7,"title":"Versuch","html_url":"u7","state":"closed","merged_at":null,"body":"Closes #12",
		   "head":{"ref":"weg","sha":"s7","repo":null},"base":{"ref":"main"}}]`)
	})
	prs, err := ghrest.Client{Token: "t"}.GeschlossenePullRequests(context.Background(), "acme/alpha")
	if err != nil || len(prs) != 2 {
		t.Fatalf("GeschlossenePullRequests %+v %v", prs, err)
	}
	if abfrage != "state=closed&per_page=100" {
		t.Errorf("Abfrage %q", abfrage)
	}
	if p := prs[0]; p.Nummer != 5 || !p.Gemergt || p.Zustand != "closed" || p.Head != "squash" || p.HeadRepo != "acme/alpha" || p.URL != "u5" {
		t.Errorf("#5 %+v", p)
	}
	if p := prs[1]; p.Nummer != 7 || p.Gemergt || p.Text != "Closes #12" || p.HeadRepo != "" {
		t.Errorf("#7 %+v", p)
	}
	if _, err := (ghrest.Client{}).GeschlossenePullRequests(context.Background(), "kein repo"); err == nil {
		t.Error("ungueltiges Repo ohne Fehler")
	}
}
