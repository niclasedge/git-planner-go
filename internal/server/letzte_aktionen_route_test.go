package server_test

// "Letzte Aktionen" ueber /api/arbeit gegen eine Fake-GitHub-API
// (karte-einzeilig, #124): der letzte Commit aus der Repo-Aktivitaet, sein
// Commit einmal gefragt und je SHA gespeichert; faellt /activity aus, der
// Rueckfall ueber die Branch-Koepfe mit hoechstens 10 nachgeholten Zeiten;
// ein nicht abrufbarer Commit in der Quelle commits. Dazu karte_kopf.

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

const laSHA = "6d49131fec6ac4318e7c0a389862db38df0d4e39"

type laAntwort struct {
	LetzterCommit *struct {
		Branch   string `json:"branch"`
		Standard bool   `json:"standard"`
		SHA      string `json:"sha"`
		Kurz     string `json:"kurz"`
		Art      string `json:"art"`
		Autor    string `json:"autor"`
		Betreff  string `json:"betreff"`
		Text     string `json:"text"`
		Titel    string `json:"titel"`
		PR       *struct {
			Nummer int    `json:"nummer"`
			URL    string `json:"url"`
		} `json:"pr"`
	} `json:"letzter_commit"`
	KarteKopf *struct {
		Erledigt int `json:"erledigt"`
		Fragen   int `json:"fragen"`
	} `json:"karte_kopf"`
	Quellen []struct {
		Quelle string `json:"quelle"`
		Grund  string `json:"grund"`
	} `json:"quellen"`
}

func (a laAntwort) quelle(name string) (string, bool) {
	for _, q := range a.Quellen {
		if q.Quelle == name {
			return q.Grund, true
		}
	}
	return "", false
}

// neuesLAGH ist die Fake-API; aktivitaet ist der Rumpf von /activity (leer:
// 500), branches die Branch-Liste, commit liefert je SHA den Rumpf von
// commits/{sha} (leer: 500).
func neuesLAGH(t *testing.T, aktivitaet, branches string, commit func(sha string) string) *prGH {
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
				fmt.Fprint(w, `[{"number":3,"title":"drei","state":"closed","html_url":"u3","created_at":"2026-09-01T08:00:00Z","closed_at":"2026-09-29T10:00:00Z"}]`)
				return
			}
			fmt.Fprint(w, `[]`)
		case p == "/repos/acme/alpha/pulls":
			if r.URL.Query().Get("state") == "closed" {
				fmt.Fprint(w, `[]`)
				return
			}
			fmt.Fprint(w, `[{"number":123,"title":"upbeat","html_url":"https://github.com/acme/alpha/pull/123","state":"open",
			  "head":{"ref":"claude/upbeat-planck-cxrti4","sha":"`+laSHA+`"},"base":{"ref":"main"}}]`)
		case p == "/repos/acme/alpha/branches":
			fmt.Fprint(w, branches)
		case p == "/repos/acme/alpha/activity":
			if aktivitaet == "" {
				w.WriteHeader(http.StatusInternalServerError)
				fmt.Fprint(w, `{"message":"kaputt"}`)
				return
			}
			// Seit aktionen-zeitleiste (#128) dreissig Eintraege.
			if r.URL.Query().Get("per_page") != "30" {
				t.Errorf("/activity mit %q, erwartet per_page=30", r.URL.RawQuery)
			}
			fmt.Fprint(w, aktivitaet)
		case strings.HasPrefix(p, "/repos/acme/alpha/commits/") && strings.Count(p, "/") == 5:
			rumpf := commit(strings.TrimPrefix(p, "/repos/acme/alpha/commits/"))
			if rumpf == "" {
				w.WriteHeader(http.StatusInternalServerError)
				fmt.Fprint(w, `{"message":"kaputt"}`)
				return
			}
			fmt.Fprint(w, rumpf)
		default:
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"message":"Not Found"}`)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *prGH) zaehleCommits() map[string]int {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string]int{}
	for _, a := range f.aufrufe {
		if sha, ok := strings.CutPrefix(a.Pfad, "/repos/acme/alpha/commits/"); ok && !strings.Contains(sha, "/") {
			out[sha]++
		}
	}
	return out
}

func laCommit(sha, zeit, betreff string) string {
	return `{"sha":"` + sha + `","author":{"login":"work-account"},"commit":{"message":"` + betreff + `",
	  "author":{"name":"Alex","date":"` + zeit + `"},"committer":{"date":"` + zeit + `"}}}`
}

func TestArbeitLetzterCommitAusAktivitaet(t *testing.T) {
	beispiel, err := os.ReadFile("../ghrest/testdata/aktivitaet.json")
	if err != nil {
		t.Fatal(err)
	}
	f := neuesLAGH(t, string(beispiel), `[{"name":"main","commit":{"sha":"`+laSHA+`"}}]`, func(sha string) string {
		if sha == laSHA {
			return laCommit(sha, "2026-09-30T17:58:51Z", "Merge pull request #123 from example-org/claude/upbeat-planck-cxrti4")
		}
		return ""
	})
	basis := prServer(t, f)
	var a laAntwort
	if st := holeVon(t, basis+"/api/arbeit?repo=acme/alpha", &a); st != http.StatusOK {
		t.Fatalf("Status %d", st)
	}
	if st := holeVon(t, basis+"/api/arbeit?repo=acme/alpha&frisch=1", &a); st != http.StatusOK {
		t.Fatalf("Status frisch %d", st)
	}
	l := a.LetzterCommit
	if l == nil {
		t.Fatalf("kein letzter_commit; Quellen %+v", a.Quellen)
	}
	if l.Branch != "claude/upbeat-planck-cxrti4" || l.Standard || l.Kurz != "6d49131" || l.SHA != laSHA || l.Art != "gepusht" {
		t.Errorf("letzter_commit %+v", *l)
	}
	if l.Autor != "work-account" || !strings.HasPrefix(l.Betreff, "Merge pull request #123") || l.Text == "" {
		t.Errorf("autor %q betreff %q text %q", l.Autor, l.Betreff, l.Text)
	}
	if !strings.HasPrefix(l.Titel, "gepusht ") || !strings.HasSuffix(l.Titel, laSHA) {
		t.Errorf("titel %q", l.Titel)
	}
	if l.PR == nil || l.PR.Nummer != 123 || l.PR.URL != "https://github.com/acme/alpha/pull/123" {
		t.Errorf("pr %+v, erwartet #123 ueber den Head-Branch", l.PR)
	}
	if grund, da := a.quelle("commits"); !da || grund != "" {
		t.Errorf("Quelle commits %v %q, erwartet vorhanden ohne Grund", da, grund)
	}
	if a.KarteKopf == nil || a.KarteKopf.Erledigt != 1 {
		t.Errorf("karte_kopf %+v, erwartet erledigt 1", a.KarteKopf)
	}
	// Der Branch-Kopf von main ist derselbe SHA, haengt aber an keinem Issue;
	// gefragt wird der Commit genau einmal ueber beide Aufbauten.
	if n := f.zaehleCommits()[laSHA]; n != 1 {
		t.Errorf("commits/%s %d-mal gefragt, erwartet genau einmal (gespeichert je SHA)", laSHA, n)
	}
}

func TestArbeitLetzterCommitRueckfall(t *testing.T) {
	var teile []string
	sha := func(i int) string { return fmt.Sprintf("%040d", i) }
	for i := 1; i <= 12; i++ {
		teile = append(teile, fmt.Sprintf(`{"name":"f%02d","commit":{"sha":"%s"}}`, i, sha(i)))
	}
	teile = append(teile, `{"name":"main","commit":{"sha":"`+strings.Repeat("a", 40)+`"}}`)
	f := neuesLAGH(t, "", "["+strings.Join(teile, ",")+"]", func(s string) string {
		zeit := "2026-09-20T08:00:00Z"
		if s == sha(3) {
			zeit = "2026-09-29T08:00:00Z"
		}
		return laCommit(s, zeit, "Stand "+s[len(s)-2:])
	})
	basis := prServer(t, f)
	var a laAntwort
	if st := holeVon(t, basis+"/api/arbeit?repo=acme/alpha", &a); st != http.StatusOK {
		t.Fatalf("Status %d", st)
	}
	if a.LetzterCommit == nil || a.LetzterCommit.Branch != "f03" || a.LetzterCommit.Art != "Commit" {
		t.Fatalf("letzter_commit %+v, erwartet f03 mit art Commit; Quellen %+v", a.LetzterCommit, a.Quellen)
	}
	erst := f.zaehleCommits()
	if len(erst) != 10 || erst[strings.Repeat("a", 40)] != 1 {
		t.Errorf("commits/{sha} fuer %d Branches gefragt (%v), erwartet 10 samt main", len(erst), erst)
	}
	for s, n := range erst {
		if n != 1 {
			t.Errorf("commits/%s %d-mal gefragt", s, n)
		}
	}
	if st := holeVon(t, basis+"/api/arbeit?repo=acme/alpha&frisch=1", &a); st != http.StatusOK {
		t.Fatalf("Status frisch %d", st)
	}
	danach := f.zaehleCommits()
	for s, n := range danach {
		if n != erst[s] {
			t.Errorf("commits/%s beim zweiten Aufbau erneut gefragt (%d statt %d)", s, n, erst[s])
		}
	}
	if a.LetzterCommit == nil || a.LetzterCommit.Branch != "f03" {
		t.Errorf("zweiter Aufbau letzter_commit %+v, erwartet f03", a.LetzterCommit)
	}
}

func TestArbeitLetzterCommitNichtAbrufbar(t *testing.T) {
	akt := `[{"ref":"refs/heads/main","timestamp":"2026-09-30T10:00:00Z","after":"` + laSHA + `","activity_type":"push","actor":{"login":"x"}}]`
	f := neuesLAGH(t, akt, `[{"name":"main","commit":{"sha":"`+laSHA+`"}}]`, func(string) string { return "" })
	basis := prServer(t, f)
	var a laAntwort
	if st := holeVon(t, basis+"/api/arbeit?repo=acme/alpha", &a); st != http.StatusOK {
		t.Fatalf("Status %d", st)
	}
	if a.LetzterCommit != nil {
		t.Errorf("letzter_commit %+v, erwartet keins", *a.LetzterCommit)
	}
	if grund, da := a.quelle("commits"); !da || !strings.Contains(grund, "500") {
		t.Errorf("Quelle commits %v %q, erwartet einen Grund mit 500", da, grund)
	}
}
