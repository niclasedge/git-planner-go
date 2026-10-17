package server_test

// "Zuletzt erledigt" mit gemergtem PR ueber /api/arbeit gegen eine
// Fake-GitHub-API (zuletzt-erledigt-pr, #116): die Timeline nur fuer die
// fuenf Eintraege von zuletzt, gespeichert je Issue und Schliesszeit, ein
// Fehlschlag in der Quelle timeline.

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func neuesZEGH(t *testing.T) *prGH {
	t.Helper()
	f := &prGH{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.aufrufe = append(f.aufrufe, ghAufruf{Methode: r.Method, Pfad: r.URL.Path, Query: r.URL.RawQuery})
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		p := r.URL.Path
		switch {
		case p == "/user/repos":
			fmt.Fprint(w, `[{"full_name":"acme/alpha","name":"alpha","owner":{"login":"acme"},"default_branch":"main"}]`)
		case p == "/repos/acme/alpha/issues":
			if r.URL.Query().Get("state") == "closed" {
				var teile []string
				for nr := 15; nr >= 10; nr-- {
					teile = append(teile, fmt.Sprintf(`{"number":%d,"title":"Issue %d","state":"closed","html_url":"u%d",
					  "created_at":"2026-09-01T08:00:00Z","closed_at":"2026-09-2%dT10:00:00Z"}`, nr, nr, nr, nr-10))
				}
				fmt.Fprint(w, "["+strings.Join(teile, ",")+"]")
				return
			}
			fmt.Fprint(w, `[]`)
		case p == "/repos/acme/alpha/issues/15/timeline":
			fmt.Fprint(w, `[{"event":"closed","created_at":"2026-09-25T10:00:00Z","commit_id":"sha15"}]`)
		case p == "/repos/acme/alpha/issues/14/timeline":
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprint(w, `{"message":"kaputt"}`)
		case strings.HasSuffix(p, "/timeline"):
			fmt.Fprint(w, `[]`)
		case p == "/repos/acme/alpha/commits/sha15/pulls":
			fmt.Fprint(w, `[{"number":90,"title":"fuenfzehn fertig","html_url":"https://github.com/acme/alpha/pull/90","state":"closed",
			  "merged_at":"2026-09-25T09:59:00Z","head":{"ref":"claude/xyz","sha":"s90"},"base":{"ref":"main"}}]`)
		case p == "/repos/acme/alpha/pulls":
			fmt.Fprint(w, `[]`)
		case p == "/repos/acme/alpha/branches":
			fmt.Fprint(w, `[{"name":"main","commit":{"sha":"shamain"}}]`)
		default:
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"message":"Not Found"}`)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

type zeAntwort struct {
	Zuletzt []struct {
		Nummer          int    `json:"nummer"`
		GeschlossenText string `json:"geschlossen_text"`
		GeschlossenVoll string `json:"geschlossen_voll"`
		GemergtMit      *struct {
			Nummer    int    `json:"nummer"`
			Titel     string `json:"titel"`
			URL       string `json:"url"`
			GemergtAm string `json:"gemergt_am"`
		} `json:"gemergt_mit"`
	} `json:"zuletzt"`
	Quellen []struct {
		Quelle string `json:"quelle"`
		Grund  string `json:"grund"`
	} `json:"quellen"`
}

func TestArbeitZuletztGemergtMit(t *testing.T) {
	f := neuesZEGH(t)
	basis := prServer(t, f)
	var a zeAntwort
	if st := holeVon(t, basis+"/api/arbeit?repo=acme/alpha", &a); st != http.StatusOK {
		t.Fatalf("Status %d", st)
	}
	if st := holeVon(t, basis+"/api/arbeit?repo=acme/alpha&frisch=1", &a); st != http.StatusOK {
		t.Fatalf("Status frisch %d", st)
	}
	if len(a.Zuletzt) != 5 {
		t.Fatalf("zuletzt %d Eintraege, erwartet 5", len(a.Zuletzt))
	}
	je := map[int]int{}
	for i, e := range a.Zuletzt {
		je[e.Nummer] = i
		if e.GeschlossenText == "" || e.GeschlossenVoll == "" {
			t.Errorf("#%d ohne geschlossen_text/geschlossen_voll", e.Nummer)
		}
	}
	f15 := a.Zuletzt[je[15]]
	if f15.GemergtMit == nil || f15.GemergtMit.Nummer != 90 || f15.GemergtMit.URL != "https://github.com/acme/alpha/pull/90" ||
		f15.GemergtMit.Titel != "fuenfzehn fertig" || f15.GemergtMit.GemergtAm != "2026-09-25T09:59:00Z" {
		t.Errorf("#15 gemergt_mit %+v", f15.GemergtMit)
	}
	if g := a.Zuletzt[je[14]].GemergtMit; g != nil {
		t.Errorf("#14 gemergt_mit %+v, erwartet keins", *g)
	}
	grund, gesehen := "", false
	for _, q := range a.Quellen {
		if q.Quelle == "timeline" {
			grund, gesehen = q.Grund, true
		}
	}
	if !gesehen || !strings.Contains(grund, "500") {
		t.Errorf("Quelle timeline %v %q, erwartet einen Grund mit 500", gesehen, grund)
	}

	f.mu.Lock()
	zaehler := map[string]int{}
	for _, x := range f.aufrufe {
		zaehler[x.Pfad]++
	}
	f.mu.Unlock()
	if n := zaehler["/repos/acme/alpha/issues/10/timeline"]; n != 0 {
		t.Errorf("Timeline von #10 %d-mal gefragt, #10 steht nicht in zuletzt", n)
	}
	if n := zaehler["/repos/acme/alpha/issues/15/timeline"]; n != 1 {
		t.Errorf("Timeline von #15 %d-mal gefragt, erwartet 1 (gespeichert je Issue und Schliesszeit)", n)
	}
	if n := zaehler["/repos/acme/alpha/commits/sha15/pulls"]; n != 1 {
		t.Errorf("commits/sha15/pulls %d-mal gefragt, erwartet 1", n)
	}
}
