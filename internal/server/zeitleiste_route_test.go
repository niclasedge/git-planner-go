package server_test

// Die Zeitleiste ueber /api/arbeit (aktionen-zeitleiste, #128): die Commits
// und Belege eines Pushes auf einem anderen Branch kommen aus
// compare/{before}...{after}, je Repo und Paar einmal gefragt; geschlossene
// PRs kommen aus der Arbeitsliste; die Aktivitaet wird mit per_page=30
// gefragt.

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	ztVor  = "a000000000000000000000000000000000000000"
	ztNach = "a200000000000000000000000000000000000000"
	ztEins = "a100000000000000000000000000000000000000"
)

type ztGH struct {
	mu      sync.Mutex
	srv     *httptest.Server
	aufrufe []ghAufruf
}

func (f *ztGH) zaehle(passt func(ghAufruf) bool) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, a := range f.aufrufe {
		if passt(a) {
			n++
		}
	}
	return n
}

func neuesZTGH(t *testing.T) *ztGH {
	t.Helper()
	f := &ztGH{}
	push := lsZeit(10 * time.Minute)
	commit := func(sha, zeit, betreff string) string {
		return fmt.Sprintf(`{"sha":%q,"author":{"login":"alex"},"commit":{"message":%q,
		  "author":{"name":"Alex","date":%q},"committer":{"date":%q}}}`, sha, betreff, zeit, zeit)
	}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.aufrufe = append(f.aufrufe, ghAufruf{Methode: r.Method, Pfad: r.URL.Path, Query: r.URL.RawQuery})
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		p, q := r.URL.Path, r.URL.Query()
		switch {
		case p == "/user/repos":
			fmt.Fprint(w, `[{"full_name":"acme/alpha","name":"alpha","owner":{"login":"acme"},"default_branch":"main"}]`)
		case p == "/repos/acme/alpha/issues":
			fmt.Fprint(w, `[]`)
		case p == "/repos/acme/alpha/pulls" && q.Get("state") == "closed":
			fmt.Fprintf(w, `[{"number":9,"title":"Neun","html_url":"https://github.com/acme/alpha/pull/9","state":"closed","merged":true,
			  "user":{"login":"alex"},"labels":[],"head":{"ref":"change/neun","sha":"n9"},"base":{"ref":"main"},
			  "merged_at":%q,"closed_at":%q}]`, lsZeit(20*time.Minute), lsZeit(20*time.Minute))
		case p == "/repos/acme/alpha/pulls":
			fmt.Fprint(w, `[]`)
		case p == "/repos/acme/alpha/branches":
			fmt.Fprint(w, `[{"name":"main","commit":{"sha":"`+lsK1+`"}},{"name":"claude/x","commit":{"sha":"`+ztNach+`"}}]`)
		case p == "/repos/acme/alpha/activity":
			fmt.Fprintf(w, `[{"ref":"refs/heads/claude/x","timestamp":%q,"before":%q,"after":%q,
			  "activity_type":"push","actor":{"login":"work-account"}}]`, push, ztVor, ztNach)
		case p == "/repos/acme/alpha/commits/"+ztNach:
			fmt.Fprint(w, commit(ztNach, push, "zwei"))
		case p == "/repos/acme/alpha/compare/"+ztVor+"..."+ztNach:
			fmt.Fprintf(w, `{"ahead_by":2,"behind_by":0,"commits":[%s,%s],
			  "files":[{"filename":"openspec/changes/k/belege/assets/0-k.png","status":"added","sha":"b-k"}]}`,
				commit(ztEins, lsZeit(12*time.Minute), "eins"), commit(ztNach, lsZeit(11*time.Minute), "zwei"))
		case p == "/repos/acme/alpha/git/trees/"+lsK1:
			fmt.Fprint(w, `{"sha":"`+lsK1+`","truncated":false,"tree":[]}`)
		case p == "/repos/acme/alpha/git/trees/"+ztNach:
			fmt.Fprint(w, `{"sha":"`+ztNach+`","truncated":false,"tree":[]}`)
		default:
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"message":"Not Found"}`)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

type ztAntwort struct {
	LetzteAktionen []struct {
		Art        string `json:"art"`
		Branch     string `json:"branch"`
		Kennung    string `json:"kennung"`
		Name       string `json:"name"`
		Bild       string `json:"bild"`
		Nummer     int    `json:"nummer"`
		PRTitel    string `json:"pr_titel"`
		Tag        string `json:"tag"`
		Uhr        string `json:"uhr"`
		CommitZahl int    `json:"commit_zahl"`
		Commits    []struct {
			Kurz    string `json:"kurz"`
			Betreff string `json:"betreff"`
		} `json:"commits"`
	} `json:"letzte_aktionen"`
}

func TestZeitleisteUeberDieRoute(t *testing.T) {
	f := neuesZTGH(t)
	t.Setenv("GKP_SYNC_TTL", "3600")
	t.Setenv("GKP_ZONE", "Europe/Berlin")
	basis := prServer(t, &prGH{srv: f.srv})

	var a ztAntwort
	if st := holeVon(t, basis+"/api/arbeit?repo=acme/alpha", &a); st != http.StatusOK {
		t.Fatalf("Status %d", st)
	}
	var arten []string
	gefunden := map[string]bool{}
	for _, x := range a.LetzteAktionen {
		arten = append(arten, x.Art)
		if x.Tag == "" || x.Uhr == "" {
			t.Errorf("Zeile %s ohne tag oder uhr: %+v", x.Art, x)
		}
		switch x.Art {
		case "push":
			gefunden["push"] = true
			var betreffe []string
			for _, c := range x.Commits {
				betreffe = append(betreffe, c.Betreff)
			}
			if x.Branch != "claude/x" || x.CommitZahl != 2 || strings.Join(betreffe, ",") != "zwei,eins" {
				t.Errorf("Push %+v, erwartet claude/x mit commit_zahl 2 und zwei,eins", x)
			}
		case "beleg":
			gefunden["beleg"] = true
			if x.Kennung != "k" || x.Branch != "claude/x" || x.Name != "0-k.png" || x.Bild == "" {
				t.Errorf("Beleg %+v, erwartet k · 0-k.png auf claude/x mit Bildadresse", x)
			}
		case "pr":
			gefunden["pr"] = true
			if x.Nummer != 9 || x.PRTitel != "Neun" {
				t.Errorf("PR %+v, erwartet #9 Neun", x)
			}
		}
	}
	for _, art := range []string{"push", "beleg", "pr"} {
		if !gefunden[art] {
			t.Errorf("keine Zeile %s in %v", art, arten)
		}
	}

	if st := holeVon(t, basis+"/api/arbeit?repo=acme/alpha", &a); st != http.StatusOK {
		t.Fatalf("Status zweiter Aufruf %d", st)
	}
	warteAufSync(t, basis, "aktivitaet:acme/alpha")
	if st := holeVon(t, basis+"/api/arbeit?repo=acme/alpha", &a); st != http.StatusOK {
		t.Fatalf("Status dritter Aufruf %d", st)
	}
	if n := f.zaehle(func(a ghAufruf) bool { return strings.HasPrefix(a.Pfad, "/repos/acme/alpha/compare/") }); n != 1 {
		t.Errorf("Vergleich %d-mal gefragt, erwartet genau einmal ueber drei Aufrufe", n)
	}
	if n := f.zaehle(func(a ghAufruf) bool { return a.Pfad == "/repos/acme/alpha/activity" && a.Query != "per_page=30" }); n != 0 {
		t.Errorf("%d Anfragen an /activity ohne per_page=30", n)
	}
}
