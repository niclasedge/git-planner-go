package ghrest_test

// Offene und geschlossene Blocker aus dependencies/blocked_by
// (reihe-erledigte-blocker, #130).

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

func TestBlockerOffenUndGeschlossen(t *testing.T) {
	fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/repos/acme/alpha/issues/3/dependencies/blocked_by" {
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"message":"Not Found"}`)
			return
		}
		fmt.Fprint(w, `[
		 {"number":4,"title":"Vier","state":"closed","closed_at":"2026-10-01T07:12:00Z","html_url":"u4",
		  "repository_url":"https://api.github.com/repos/acme/alpha","issue_dependencies_summary":{"blocking":0,"total_blocking":3}},
		 {"number":2,"title":"Zwei","state":"open","html_url":"u2","repository_url":"https://api.github.com/repos/acme/alpha"},
		 {"number":8,"title":"Fremd","state":"closed","html_url":"u8","repository_url":"https://api.github.com/repos/acme/beta"}]`)
	})
	c := ghrest.Client{Token: "t"}
	alle, err := c.Blocker(context.Background(), "acme/alpha", 3)
	if err != nil {
		t.Fatalf("Blocker: %v", err)
	}
	if len(alle) != 2 || alle[0].Nummer != 2 || alle[1].Nummer != 4 {
		t.Fatalf("Blocker %+v, erwartet #2 und #4 nach Nummer, ohne das fremde #8", alle)
	}
	if v := alle[1]; v.Zustand != "closed" || v.Geschlossen != "2026-10-01T07:12:00Z" || v.Titel != "Vier" || v.AlleBlockt != 3 {
		t.Errorf("#4 %+v, erwartet geschlossen mit Zeit, Titel und total_blocking", v)
	}
	offen, err := c.BlockiertDurch(context.Background(), "acme/alpha", 3)
	if err != nil || fmt.Sprint(offen) != "[2]" {
		t.Errorf("BlockiertDurch %v %v, erwartet nur das offene #2", offen, err)
	}
}
