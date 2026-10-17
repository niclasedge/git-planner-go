package ghrest_test

// Die Repo-Aktivitaet (karte-einzeilig, #124): Pushes und Merges ueber alle
// Branches, gelesen aus dem gekuerzten echten Beispiel von
// GET /repos/example-org/git-planner/activity?per_page=2; dazu der
// Autor eines Commits fuer die Zeile "Letzter Commit".

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"testing"

	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

func TestAktivitaetAusEchtemBeispiel(t *testing.T) {
	beispiel, err := os.ReadFile("testdata/aktivitaet.json")
	if err != nil {
		t.Fatal(err)
	}
	alle := protokollGitHub(t, func(a aufruf, w http.ResponseWriter) {
		if a.Pfad == "/repos/acme/alpha/activity" {
			w.Write(beispiel)
			return
		}
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"message":"Not Found"}`)
	})
	akt, err := ghrest.Client{Token: "t"}.Aktivitaet(context.Background(), "acme/alpha")
	if err != nil {
		t.Fatalf("Aktivitaet: %v (Aufrufe %+v)", err, *alle)
	}
	if len(akt) != 2 {
		t.Fatalf("%d Eintraege, erwartet 2: %+v", len(akt), akt)
	}
	a := akt[0]
	if a.Ref != "refs/heads/claude/upbeat-planck-cxrti4" || a.Zeit != "2026-09-30T17:58:58Z" ||
		a.Nachher != "6d49131fec6ac4318e7c0a389862db38df0d4e39" || a.Vorher != "dcf7be6a49f5b6cdd6298a6e856ef950d2337bba" ||
		a.Art != "push" || a.Akteur != "work-account" {
		t.Errorf("erster Eintrag %+v", a)
	}
	if akt[1].Ref != "refs/heads/main" || akt[1].Art != "pr_merge" {
		t.Errorf("zweiter Eintrag %+v", akt[1])
	}
	if len(*alle) != 1 || (*alle)[0].Pfad != "/repos/acme/alpha/activity" {
		t.Errorf("Aufrufe %+v, erwartet genau einen an /repos/acme/alpha/activity", *alle)
	}
	if _, err := (ghrest.Client{Token: "t"}).Aktivitaet(context.Background(), "kein-repo"); err == nil {
		t.Errorf("Aktivitaet mit ungueltigem Repo ohne Fehler")
	}
}

func TestAktivitaetFehler(t *testing.T) {
	protokollGitHub(t, func(a aufruf, w http.ResponseWriter) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"message":"kaputt"}`)
	})
	if _, err := (ghrest.Client{Token: "t"}).Aktivitaet(context.Background(), "acme/alpha"); err == nil {
		t.Errorf("Aktivitaet bei 500 ohne Fehler")
	}
}

func TestCommitTraegtAutor(t *testing.T) {
	mitLogin := "1111111111111111111111111111111111111111"
	ohneLogin := "2222222222222222222222222222222222222222"
	protokollGitHub(t, func(a aufruf, w http.ResponseWriter) {
		switch a.Pfad {
		case "/repos/acme/alpha/commits/" + mitLogin:
			fmt.Fprint(w, `{"sha":"`+mitLogin+`","author":{"login":"work-account"},"commit":{"message":"eins",
			  "author":{"name":"Alex Example","date":"2026-09-30T17:58:51Z"},"committer":{"date":"2026-09-30T17:58:51Z"}}}`)
		case "/repos/acme/alpha/commits/" + ohneLogin:
			fmt.Fprint(w, `{"sha":"`+ohneLogin+`","author":null,"commit":{"message":"zwei",
			  "author":{"name":"Alex Example","date":"2026-09-30T10:00:00Z"},"committer":{"date":"2026-09-30T10:00:00Z"}}}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	c := ghrest.Client{Token: "t"}
	k, err := c.Commit(context.Background(), "acme/alpha", mitLogin)
	if err != nil || k.Autor != "work-account" {
		t.Errorf("Commit mit Login %+v %v, erwartet Autor work-account", k, err)
	}
	k, err = c.Commit(context.Background(), "acme/alpha", ohneLogin)
	if err != nil || k.Autor != "Alex Example" {
		t.Errorf("Commit ohne Login %+v %v, erwartet Autor Alex Example", k, err)
	}
}

// Die Zeitleiste fragt dreissig Eintraege (aktionen-zeitleiste, #128).
func TestAktivitaetFragtDreissig(t *testing.T) {
	var query string
	fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[]`)
	})
	if _, err := (ghrest.Client{Token: "t"}).Aktivitaet(context.Background(), "acme/alpha"); err != nil {
		t.Fatal(err)
	}
	if query != "per_page=30" || ghrest.AktivitaetTiefe != 30 {
		t.Errorf("Anfrage %q, Tiefe %d, erwartet per_page=30", query, ghrest.AktivitaetTiefe)
	}
}
