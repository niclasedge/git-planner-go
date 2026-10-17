package server_test

// Der Lebenslauf je Issue und PR in /api/arbeit gegen eine Fake-GitHub-API
// (pr-lebenslauf, #83): der gemergte PR kommt nur aus pulls?state=closed.

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func neuesLLGH(t *testing.T) *prGH {
	t.Helper()
	f := &prGH{}
	gemergt := `{"number":7,"title":"fuenf","body":"Closes #5","html_url":"u7","draft":false,"node_id":"PR_7","state":"closed",
	  "merged":false,"merged_at":"2026-09-24T12:00:00Z","closed_at":"2026-09-24T12:00:00Z","created_at":"2026-09-21T09:00:00Z",
	  "user":{"login":"cy"},"head":{"ref":"change/fuenf","sha":"sha7","repo":{"full_name":"acme/alpha"}},"base":{"ref":"main"}}`
	entwurf := `{"number":8,"title":"sechs","body":"Closes #6","html_url":"u8","draft":true,"node_id":"PR_8","state":"open",
	  "merged":false,"mergeable":true,"mergeable_state":"draft","created_at":"2026-09-22T09:00:00Z",
	  "user":{"login":"cy"},"head":{"ref":"sechs","sha":"sha8","repo":{"full_name":"acme/alpha"}},"base":{"ref":"main"}}`
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
				fmt.Fprint(w, `[]`)
				return
			}
			fmt.Fprint(w, `[{"number":5,"title":"fuenf","state":"open"},{"number":6,"title":"sechs","state":"open"},
			  {"number":8,"title":"sechs","pull_request":{}}]`)
		case p == "/repos/acme/alpha/branches":
			fmt.Fprint(w, `[{"name":"main","commit":{"sha":"shamain"}},{"name":"change/fuenf","commit":{"sha":"sha7"}},
			  {"name":"sechs","commit":{"sha":"sha8"}}]`)
		case p == "/repos/acme/alpha/pulls":
			if r.URL.Query().Get("state") == "closed" {
				fmt.Fprint(w, "["+gemergt+"]")
				return
			}
			fmt.Fprint(w, "["+entwurf+"]")
		case p == "/repos/acme/alpha/pulls/8":
			fmt.Fprint(w, entwurf)
		default:
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"message":"Not Found"}`)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

type llLauf struct {
	Stufe  string `json:"stufe"`
	Stufen []struct {
		Name    string `json:"name"`
		Zustand string `json:"zustand"`
		Grund   string `json:"grund"`
		Zeit    string `json:"zeit"`
	} `json:"stufen"`
	Nachsorge []struct {
		Art      string `json:"art"`
		Nummer   int    `json:"nummer"`
		Branch   string `json:"branch"`
		Erledigt bool   `json:"erledigt"`
	} `json:"nachsorge"`
}

func TestArbeitLiefertLebenslauf(t *testing.T) {
	f := neuesLLGH(t)
	basis := prServer(t, f)
	var a struct {
		Reihe []struct {
			Nummer     int     `json:"nummer"`
			Lebenslauf *llLauf `json:"lebenslauf"`
		} `json:"reihe"`
		PRLaeufe map[string]llLauf `json:"pr_laeufe"`
		Quellen  []struct {
			Quelle string `json:"quelle"`
			Grund  string `json:"grund"`
		} `json:"quellen"`
	}
	if st := holeVon(t, basis+"/api/arbeit?repo=acme/alpha", &a); st != http.StatusOK {
		t.Fatalf("Status %d", st)
	}
	je := map[int]*llLauf{}
	for _, e := range a.Reihe {
		je[e.Nummer] = e.Lebenslauf
	}
	fuenf := je[5]
	if fuenf == nil || fuenf.Stufe != "aufraeumen" {
		t.Fatalf("#5 %+v", fuenf)
	}
	if len(fuenf.Nachsorge) != 2 || fuenf.Nachsorge[0].Art != "issue_schliessen" || fuenf.Nachsorge[0].Nummer != 5 ||
		fuenf.Nachsorge[1].Art != "branch_loeschen" || fuenf.Nachsorge[1].Nummer != 7 || fuenf.Nachsorge[1].Branch != "change/fuenf" {
		t.Errorf("#5 Nachsorge %+v", fuenf.Nachsorge)
	}
	for _, s := range fuenf.Stufen {
		if s.Name == "merge" && (s.Zustand != "erledigt" || s.Zeit != "2026-09-24T12:00:00Z") {
			t.Errorf("#5 merge %+v", s)
		}
		if s.Name == "pr" && s.Zeit != "2026-09-21T09:00:00Z" {
			t.Errorf("#5 pr ohne created_at %+v", s)
		}
	}
	if sechs := je[6]; sechs == nil || sechs.Stufe != "pr" {
		t.Errorf("#6 %+v", sechs)
	}
	if l, ok := a.PRLaeufe["8"]; !ok || l.Stufe != "pr" {
		t.Errorf("pr_laeufe %+v", a.PRLaeufe)
	}
	gesehen := false
	for _, q := range a.Quellen {
		if q.Quelle == "prs_geschlossen" {
			gesehen = true
			if q.Grund != "" {
				t.Errorf("prs_geschlossen: %s", q.Grund)
			}
		}
	}
	if !gesehen {
		t.Errorf("Quelle prs_geschlossen fehlt: %+v", a.Quellen)
	}
	n := 0
	f.mu.Lock()
	for _, x := range f.aufrufe {
		if x.Pfad == "/repos/acme/alpha/pulls" && x.Query == "state=closed&sort=updated&direction=desc&per_page=30" {
			n++
		}
	}
	f.mu.Unlock()
	if n != 1 {
		t.Errorf("geschlossene PRs %d-mal geholt, erwartet 1", n)
	}
}
