package server_test

// GET /api/detail?art=change liefert den Abgleich (dateien-abgleich, #53):
// vier liegt auf change/vier und weicht von main ab, sein Vorschlag sagt a.go
// und b.go zu, der Vergleich main...change/vier nennt a.go und c.go. drei
// liegt nur auf main; dort gibt es nichts zu vergleichen.

import (
	"fmt"
	"net/http"
	"reflect"
	"testing"
)

type testAbgleich struct {
	Abgleich *struct {
		Capabilities []string `json:"capabilities"`
		Unveraendert bool     `json:"unveraendert"`
		AuchIn       []string `json:"auch_in"`
		Dateien      struct {
			Zugesagt      []string `json:"zugesagt"`
			Gedeckt       []string `json:"gedeckt"`
			OhneZusage    []string `json:"ohne_zusage"`
			OhneAenderung []string `json:"ohne_aenderung"`
			Grund         string   `json:"grund"`
		} `json:"dateien"`
	} `json:"abgleich"`
}

// fakeMitVergleich legt die Compare-API vor die Fake-GitHub-API.
func fakeMitVergleich(t *testing.T) *fakeGH {
	t.Helper()
	f := neuesFakeGH(t)
	f.blobs["b-vier-prop"] = "Geht zurueck auf GitHub-Issue #4.\n\n## Beruehrte Dateien\n\n- `a.go`\n- `b.go`\n"
	alt := f.srv.Config.Handler
	f.srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/acme/alpha/compare/main...s-vier" {
			// Gezaehlt wird nur der Vergleich mit Dateien; die Branch-Koepfe
			// fragen denselben Pfad mit per_page=1 (issue-branches, #50).
			if r.URL.Query().Get("per_page") != "1" {
				f.mu.Lock()
				f.pfade = append(f.pfade, "dateien:"+r.URL.Path)
				f.mu.Unlock()
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"ahead_by":1,"behind_by":0,"files":[{"filename":"c.go"},{"filename":"a.go"},{"filename":"openspec/changes/vier/tasks.md"}]}`)
			return
		}
		alt.ServeHTTP(w, r)
	})
	return f
}

func TestApiDetailAbgleichAufBranch(t *testing.T) {
	f := fakeMitVergleich(t)
	h, _, _ := repoHandler(t, f)
	var d testAbgleich
	rec := holeJSON(t, h, "/api/detail?repo=acme/alpha&art=change&kennung=vier", &d)
	if rec.Code != http.StatusOK {
		t.Fatalf("Status %d: %s", rec.Code, rec.Body.String())
	}
	if d.Abgleich == nil {
		t.Fatalf("kein abgleich in %s", rec.Body.String())
	}
	a := d.Abgleich
	pruefe := func(was string, ist, soll []string) {
		t.Helper()
		if !reflect.DeepEqual(ist, soll) {
			t.Errorf("%s = %q, erwartet %q", was, ist, soll)
		}
	}
	pruefe("gedeckt", a.Dateien.Gedeckt, []string{"a.go"})
	pruefe("ohne_zusage", a.Dateien.OhneZusage, []string{"c.go"})
	pruefe("ohne_aenderung", a.Dateien.OhneAenderung, []string{"b.go"})
	pruefe("auch_in", a.AuchIn, []string{"main"})
	if a.Unveraendert || a.Dateien.Grund != "" {
		t.Errorf("unveraendert=%v grund=%q", a.Unveraendert, a.Dateien.Grund)
	}
	if n := f.anfragen("dateien:"); n != 1 {
		t.Errorf("Vergleich %d-mal abgefragt, erwartet 1", n)
	}
}

func TestApiDetailAbgleichNurMain(t *testing.T) {
	f := fakeMitVergleich(t)
	h, _, _ := repoHandler(t, f)
	var d testAbgleich
	rec := holeJSON(t, h, "/api/detail?repo=acme/alpha&art=change&kennung=drei", &d)
	if rec.Code != http.StatusOK || d.Abgleich == nil {
		t.Fatalf("Status %d ohne abgleich: %s", rec.Code, rec.Body.String())
	}
	a := d.Abgleich
	if !a.Unveraendert || len(a.AuchIn) != 0 {
		t.Errorf("unveraendert=%v auch_in=%q", a.Unveraendert, a.AuchIn)
	}
	if !reflect.DeepEqual(a.Capabilities, []string{"kern"}) {
		t.Errorf("capabilities %q", a.Capabilities)
	}
	if a.Dateien.Grund == "" {
		t.Errorf("ohne Branch kein Grund")
	}
	if n := f.anfragen("dateien:"); n != 0 {
		t.Errorf("Vergleich %d-mal abgefragt, erwartet keinen", n)
	}
}
