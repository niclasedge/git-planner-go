package server_test

// Erledigte Blocker ueber /api/arbeit (reihe-erledigte-blocker, #130): ein
// Issue, dessen einziger Blocker geschlossen ist, wird nach seinen Blockern
// gefragt, und der Blocker steht erledigt darueber.

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func TestErledigterBlockerUeberDieRoute(t *testing.T) {
	var mu sync.Mutex
	var pfade []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		pfade = append(pfade, r.URL.Path)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		p, q := r.URL.Path, r.URL.Query()
		switch {
		case p == "/user/repos":
			fmt.Fprint(w, `[{"full_name":"acme/alpha","name":"alpha","owner":{"login":"acme"},"default_branch":"main"}]`)
		case p == "/repos/acme/alpha/issues" && q.Get("state") == "closed":
			fmt.Fprint(w, `[]`)
		case p == "/repos/acme/alpha/issues":
			fmt.Fprint(w, `[{"number":3,"title":"drei","state":"open","html_url":"u3","user":{"login":"ana"},"labels":[],"assignees":[],
			  "issue_dependencies_summary":{"blocked_by":0,"total_blocked_by":1,"blocking":0,"total_blocking":0}},
			 {"number":1,"title":"eins","state":"open","html_url":"u1","user":{"login":"ana"},"labels":[],"assignees":[]}]`)
		case p == "/repos/acme/alpha/issues/3/dependencies/blocked_by":
			fmt.Fprint(w, `[{"number":4,"title":"vier","state":"closed","closed_at":"2026-10-01T07:12:00Z","html_url":"u4",
			  "repository_url":"http://x/repos/acme/alpha","issue_dependencies_summary":{"blocking":0,"total_blocking":1}},
			 {"number":8,"title":"fremd","state":"closed","closed_at":"2026-10-01T07:00:00Z","html_url":"u8",
			  "repository_url":"http://x/repos/acme/beta"}]`)
		case p == "/repos/acme/alpha/pulls":
			fmt.Fprint(w, `[]`)
		case p == "/repos/acme/alpha/branches":
			fmt.Fprint(w, `[]`)
		case p == "/repos/acme/alpha/activity":
			fmt.Fprint(w, `[]`)
		default:
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"message":"Not Found"}`)
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("GKP_ZONE", "Europe/Berlin")
	basis := prServer(t, &prGH{srv: srv})

	var a struct {
		Reihe []struct {
			Nummer           int    `json:"nummer"`
			Status           string `json:"status"`
			Tiefe            int    `json:"tiefe"`
			Unter            int    `json:"unter"`
			GeschlossenText  string `json:"geschlossen_text"`
			ErledigteBlocker []int  `json:"erledigte_blocker"`
			Abhaengige       *struct {
				Aufgeklappt bool `json:"aufgeklappt"`
			} `json:"abhaengige"`
		} `json:"reihe"`
		Quellen []struct{ Quelle, Grund string } `json:"quellen"`
	}
	if st := holeVon(t, basis+"/api/arbeit?repo=acme/alpha", &a); st != http.StatusOK {
		t.Fatalf("Status %d", st)
	}
	mu.Lock()
	gefragt := 0
	for _, p := range pfade {
		if p == "/repos/acme/alpha/issues/3/dependencies/blocked_by" {
			gefragt++
		}
	}
	mu.Unlock()
	if gefragt == 0 {
		t.Errorf("dependencies/blocked_by von #3 nicht gefragt")
	}
	var folge []string
	for _, e := range a.Reihe {
		folge = append(folge, fmt.Sprintf("%d:%s", e.Nummer, e.Status))
	}
	if len(a.Reihe) != 3 || a.Reihe[0].Nummer != 4 || a.Reihe[0].Status != "erledigt" || a.Reihe[0].GeschlossenText == "" ||
		a.Reihe[0].Abhaengige == nil || !a.Reihe[0].Abhaengige.Aufgeklappt {
		t.Fatalf("reihe %v, erwartet #4 erledigt und aufgeklappt vorn, dann #3, dann #1", folge)
	}
	if e := a.Reihe[1]; e.Nummer != 3 || e.Tiefe != 1 || e.Unter != 4 || fmt.Sprint(e.ErledigteBlocker) != "[4]" || e.Status == "blockiert" {
		t.Errorf("#3 %+v, erwartet unter #4 mit erledigte_blocker [4], nicht blockiert", e)
	}
	for _, q := range a.Quellen {
		if q.Quelle == "abhaengigkeiten" && q.Grund != "" {
			t.Errorf("abhaengigkeiten mit Grund %q", q.Grund)
		}
	}
}
