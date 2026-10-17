package ghrest_test

// Die Labels eines Repos (karte-gruppen, #121): aus ihnen entscheidet die
// Arbeitsliste, ob das Repo Rollen-Labels nutzt.

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

func TestRepoLabels(t *testing.T) {
	alle := protokollGitHub(t, func(a aufruf, w http.ResponseWriter) {
		switch a.Pfad {
		case "/repos/acme/alpha/labels":
			fmt.Fprint(w, `[{"name":"bug","color":"d73a4a"},{"name":"ready-for-agent","color":"0e8a16"}]`)
		case "/repos/acme/leer/labels":
			fmt.Fprint(w, `[]`)
		default:
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"message":"Not Found"}`)
		}
	})
	c := ghrest.Client{Token: "t"}
	ls, err := c.RepoLabels(context.Background(), "acme/alpha")
	if err != nil {
		t.Fatal(err)
	}
	if len(ls) != 2 || ls[0].Name != "bug" || ls[0].Farbe != "d73a4a" || ls[1].Name != "ready-for-agent" {
		t.Errorf("Labels %+v", ls)
	}
	leer, err := c.RepoLabels(context.Background(), "acme/leer")
	if err != nil || leer == nil || len(leer) != 0 {
		t.Errorf("leeres Repo %v %v, erwartet leere Liste ohne Fehler", leer, err)
	}
	if _, err := c.RepoLabels(context.Background(), "acme/weg"); err == nil {
		t.Errorf("404 ohne Fehler")
	}
	if _, err := c.RepoLabels(context.Background(), "kein repo"); err == nil {
		t.Errorf("ungueltiges Repo ohne Fehler")
	}
	if n := len(*alle); n != 3 {
		t.Errorf("%d Aufrufe, erwartet 3 (das ungueltige Repo fragt nicht)", n)
	}
	if (*alle)[0].Methode != http.MethodGet {
		t.Errorf("Aufruf %+v", (*alle)[0])
	}
}
