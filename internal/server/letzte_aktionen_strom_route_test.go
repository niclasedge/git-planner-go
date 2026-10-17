package server_test

// "Letzte Aktionen" als Zeitstrom ueber /api/arbeit (letzte-aktionen, #126):
// die Aktivitaet wird bei jedem Aufruf im Hintergrund erneuert, unabhaengig
// vom Speicher der Arbeitsliste, und CI, Bilder und HTML-Dateien kommen aus
// den Speichern ihrer Abschnitte, ohne eigenen Abruf.

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// lsGH ist die Fake-API: ein Repo acme/alpha mit main auf dem Commit lsK1,
// der docs/karte.png und bericht.html anlegt, ein geschlossenes Issue, ein
// abgeschlossener Workflow-Run und eine veraenderbare Aktivitaet.
type lsGH struct {
	mu         sync.Mutex
	srv        *httptest.Server
	aufrufe    []ghAufruf
	aktivitaet string
	// k1Zeit ist die Zeit von lsK1, geschlossen die closed_at von #3.
	k1Zeit, geschlossen string
}

const (
	lsK1   = "1111111111111111111111111111111111111111"
	lsPush = "2222222222222222222222222222222222222222"
	lsMrg  = "3333333333333333333333333333333333333333"
)

func lsZeit(vor time.Duration) string { return time.Now().Add(-vor).UTC().Format(time.RFC3339) }

func lsAkt(art, ref, zeit, sha string) string {
	return fmt.Sprintf(`{"ref":%q,"timestamp":%q,"before":"0000000000000000000000000000000000000000","after":%q,
	  "activity_type":%q,"actor":{"login":"work-account"}}`, ref, zeit, sha, art)
}

func (f *lsGH) setzeAktivitaet(eintraege ...string) {
	f.mu.Lock()
	f.aktivitaet = "[" + strings.Join(eintraege, ",") + "]"
	f.mu.Unlock()
}

// zaehle zaehlt die Aufrufe ab Index ab, deren Pfad passt annimmt.
func (f *lsGH) zaehle(ab int, passt func(ghAufruf) bool) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, a := range f.aufrufe[ab:] {
		if passt(a) {
			n++
		}
	}
	return n
}

func (f *lsGH) anzahl() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.aufrufe)
}

func neuesLSGH(t *testing.T) *lsGH {
	t.Helper()
	f := &lsGH{k1Zeit: lsZeit(time.Minute), geschlossen: lsZeit(5 * time.Second)}
	f.setzeAktivitaet(lsAkt("push", "refs/heads/change/x", lsZeit(10*time.Minute), lsPush))
	commit := func(sha, zeit, betreff, dateien string) string {
		return fmt.Sprintf(`{"sha":%q,"author":{"login":"alex"},"commit":{"message":%q,
		  "author":{"name":"Alex","date":%q},"committer":{"date":%q}},"files":[%s]}`, sha, betreff, zeit, zeit, dateien)
	}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.aufrufe = append(f.aufrufe, ghAufruf{Methode: r.Method, Pfad: r.URL.Path, Query: r.URL.RawQuery})
		akt, k1Zeit, geschlossen := f.aktivitaet, f.k1Zeit, f.geschlossen
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		p, q := r.URL.Path, r.URL.Query()
		k1 := commit(lsK1, k1Zeit, "Karte und Bericht", `{"filename":"docs/karte.png","sha":"b-karte","status":"added"},
		  {"filename":"bericht.html","sha":"b-bericht","status":"added"}`)
		switch {
		case p == "/user/repos":
			fmt.Fprint(w, `[{"full_name":"acme/alpha","name":"alpha","owner":{"login":"acme"},"default_branch":"main"}]`)
		case p == "/repos/acme/alpha/issues":
			if q.Get("state") == "closed" {
				fmt.Fprintf(w, `[{"number":3,"title":"drei","state":"closed","html_url":"u3","created_at":"2026-09-01T08:00:00Z","closed_at":%q}]`, geschlossen)
				return
			}
			fmt.Fprint(w, `[]`)
		case p == "/repos/acme/alpha/pulls":
			fmt.Fprint(w, `[]`)
		case p == "/repos/acme/alpha/branches":
			fmt.Fprint(w, `[{"name":"main","commit":{"sha":"`+lsK1+`"}}]`)
		case p == "/repos/acme/alpha/activity":
			fmt.Fprint(w, akt)
		case p == "/repos/acme/alpha/commits" && q.Get("path") != "":
			fmt.Fprint(w, "["+k1+"]")
		case p == "/repos/acme/alpha/commits":
			fmt.Fprint(w, "["+k1+"]")
		case p == "/repos/acme/alpha/commits/"+lsK1:
			fmt.Fprint(w, k1)
		case p == "/repos/acme/alpha/commits/"+lsPush:
			fmt.Fprint(w, commit(lsPush, lsZeit(10*time.Minute), "x: Schritt", ""))
		case p == "/repos/acme/alpha/commits/"+lsMrg:
			fmt.Fprint(w, commit(lsMrg, lsZeit(time.Minute), "Merge pull request #9 from acme/change/x", ""))
		case p == "/repos/acme/alpha/git/trees/"+lsK1:
			fmt.Fprint(w, `{"sha":"`+lsK1+`","truncated":false,"tree":[{"path":"bericht.html","type":"blob","sha":"b-bericht","size":9},
			  {"path":"docs","type":"tree","sha":"t-docs"},{"path":"docs/karte.png","type":"blob","sha":"b-karte","size":4}]}`)
		case p == "/repos/acme/alpha/git/blobs/b-bericht":
			fmt.Fprintf(w, `{"sha":"b-bericht","encoding":"base64","content":%q}`, base64.StdEncoding.EncodeToString([]byte("<p>hi</p>")))
		case p == "/repos/acme/alpha/git/blobs/b-karte":
			fmt.Fprintf(w, `{"sha":"b-karte","encoding":"base64","content":%q}`, base64.StdEncoding.EncodeToString([]byte("PNG!")))
		case p == "/repos/acme/alpha/actions/runs":
			fmt.Fprintf(w, `{"total_count":1,"workflow_runs":[{"id":77,"name":"test","event":"push","head_branch":"main","head_sha":%q,
			  "display_title":"Karte","run_number":5,"run_attempt":1,"status":"completed","conclusion":"success",
			  "run_started_at":%q,"updated_at":%q,"html_url":"https://github.com/acme/alpha/actions/runs/77"}]}`,
				lsK1, lsZeit(3*time.Minute), lsZeit(2*time.Minute))
		case p == "/repos/acme/alpha/actions/runs/77/jobs":
			fmt.Fprint(w, `{"total_count":0,"jobs":[]}`)
		default:
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"message":"Not Found"}`)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

type lsAntwort struct {
	LetzteAktionen []struct {
		Art      string `json:"art"`
		Wort     string `json:"wort"`
		Branch   string `json:"branch"`
		SHA      string `json:"sha"`
		Betreff  string `json:"betreff"`
		Text     string `json:"text"`
		Juengste bool   `json:"juengste"`
		Name     string `json:"name"`
		Punkt    string `json:"punkt"`
		Adresse  string `json:"adresse"`
		Bild     string `json:"bild"`
		Nummer   int    `json:"nummer"`
	} `json:"letzte_aktionen"`
	Zuletzt []struct {
		Nummer          int    `json:"nummer"`
		GeschlossenText string `json:"geschlossen_text"`
	} `json:"zuletzt"`
	Quellen []struct {
		Quelle string `json:"quelle"`
		Grund  string `json:"grund"`
	} `json:"quellen"`
}

func (a lsAntwort) arten() string {
	var out []string
	for _, x := range a.LetzteAktionen {
		out = append(out, x.Art)
	}
	return strings.Join(out, ",")
}

func lsServer(t *testing.T, f *lsGH) string {
	t.Helper()
	t.Setenv("GKP_SYNC_TTL", "3600")
	t.Setenv("GKP_ZONE", "Europe/Berlin")
	return prServer(t, &prGH{srv: f.srv})
}

// warteAufSync wartet, bis die Quelle einmal gelaufen ist und nicht mehr laeuft.
func warteAufSync(t *testing.T, basis, quelle string) {
	t.Helper()
	for i := 0; i < 200; i++ {
		var s struct {
			Quellen []struct {
				Name   string `json:"name"`
				Laeuft bool   `json:"laeuft"`
				Zeit   string `json:"zeit"`
			} `json:"quellen"`
		}
		holeVon(t, basis+"/api/sync", &s)
		for _, q := range s.Quellen {
			if q.Name == quelle && !q.Laeuft && q.Zeit != "" {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("Quelle %s ist nicht fertig geworden", quelle)
}

func TestLetzteAktionenNeuerMergeImHintergrund(t *testing.T) {
	f := neuesLSGH(t)
	// Das geschlossene Issue liegt vor dem Push, damit der Push die juengste
	// Zeile ist.
	f.geschlossen = lsZeit(time.Hour)
	basis := lsServer(t, f)
	istAktivitaet := func(a ghAufruf) bool { return a.Pfad == "/repos/acme/alpha/activity" }
	istOffen := func(a ghAufruf) bool {
		return a.Pfad == "/repos/acme/alpha/issues" && !strings.Contains(a.Query, "state=closed")
	}

	var a lsAntwort
	if st := holeVon(t, basis+"/api/arbeit?repo=acme/alpha", &a); st != http.StatusOK {
		t.Fatalf("Status %d", st)
	}
	if len(a.LetzteAktionen) == 0 || a.LetzteAktionen[0].Art != "push" || !a.LetzteAktionen[0].Juengste ||
		a.LetzteAktionen[0].Branch != "change/x" || a.LetzteAktionen[0].Betreff != "x: Schritt" {
		t.Fatalf("erste Antwort %+v, erwartet den Push auf change/x als juengste Zeile; Quellen %+v", a.LetzteAktionen, a.Quellen)
	}
	if n := f.zaehle(0, istAktivitaet); n != 1 {
		t.Errorf("/activity beim ersten Aufbau %d-mal gefragt, erwartet 1", n)
	}

	f.setzeAktivitaet(lsAkt("pr_merge", "refs/heads/main", lsZeit(time.Minute), lsMrg),
		lsAkt("push", "refs/heads/change/x", lsZeit(10*time.Minute), lsPush))
	vorher := f.anzahl()
	if st := holeVon(t, basis+"/api/arbeit?repo=acme/alpha", &a); st != http.StatusOK {
		t.Fatalf("Status zweiter Aufruf %d", st)
	}
	warteAufSync(t, basis, "aktivitaet:acme/alpha")
	if n := f.zaehle(vorher, istAktivitaet); n != 1 {
		t.Errorf("/activity nach dem zweiten Aufruf %d-mal gefragt, erwartet genau 1 im Hintergrund", n)
	}
	if n := f.zaehle(0, istOffen); n != 1 {
		t.Errorf("offene Issues %d-mal gefragt, erwartet 1 (die Arbeitsliste wird nicht neu gebaut)", n)
	}

	var c lsAntwort
	if st := holeVon(t, basis+"/api/arbeit?repo=acme/alpha", &c); st != http.StatusOK {
		t.Fatalf("Status dritter Aufruf %d", st)
	}
	if len(c.LetzteAktionen) < 2 || c.LetzteAktionen[0].Art != "merge" || !c.LetzteAktionen[0].Juengste ||
		c.LetzteAktionen[0].Branch != "main" || c.LetzteAktionen[1].Juengste {
		t.Errorf("dritte Antwort %+v, erwartet den Merge auf main als juengste Zeile", c.LetzteAktionen)
	}
	if c.LetzteAktionen[0].Betreff != "Merge pull request #9 from acme/change/x" {
		t.Errorf("Betreff des Merges %q, erwartet aus commits/{sha}", c.LetzteAktionen[0].Betreff)
	}
}

func TestLetzteAktionenAusDenSpeichernDerAbschnitte(t *testing.T) {
	f := neuesLSGH(t)
	basis := lsServer(t, f)

	var a lsAntwort
	if st := holeVon(t, basis+"/api/arbeit?repo=acme/alpha", &a); st != http.StatusOK {
		t.Fatalf("Status %d", st)
	}
	if got := a.arten(); strings.Contains(got, "ci") || strings.Contains(got, "bild") || strings.Contains(got, "html") {
		t.Errorf("erste Antwort mit Arten %q, erwartet ohne ci, bild und html (kein Speicher)", got)
	}
	if !strings.Contains(a.arten(), "issue") {
		t.Errorf("erste Antwort ohne das geschlossene Issue: %q", a.arten())
	}
	for _, pfad := range []string{"/api/ci", "/api/geaenderte-bilder", "/api/html-dateien"} {
		if st := holeVon(t, basis+pfad+"?repo=acme/alpha", nil); st != http.StatusOK {
			t.Fatalf("%s: Status %d", pfad, st)
		}
	}
	vorher := f.anzahl()
	var b lsAntwort
	if st := holeVon(t, basis+"/api/arbeit?repo=acme/alpha", &b); st != http.StatusOK {
		t.Fatalf("Status %d", st)
	}
	warteAufSync(t, basis, "aktivitaet:acme/alpha")
	gefunden := map[string]bool{}
	for _, x := range b.LetzteAktionen {
		gefunden[x.Art] = true
		switch x.Art {
		case "ci":
			if x.Punkt != "gruen" {
				t.Errorf("CI-Zeile %+v, erwartet punkt gruen", x)
			}
		case "bild":
			if x.Name != "karte.png" || x.Bild == "" {
				t.Errorf("Bildzeile %+v", x)
			}
		case "html":
			if x.Name != "bericht.html" || !strings.HasPrefix(x.Adresse, "/api/datei/acme/alpha/") {
				t.Errorf("HTML-Zeile %+v", x)
			}
		}
	}
	for _, art := range []string{"ci", "bild", "html"} {
		if !gefunden[art] {
			t.Errorf("letzte Antwort ohne Zeile %s: %q", art, b.arten())
		}
	}
	n := f.zaehle(vorher, func(a ghAufruf) bool {
		return strings.Contains(a.Pfad, "/actions/") || strings.Contains(a.Pfad, "/git/trees/") ||
			a.Pfad == "/repos/acme/alpha/commits/"+lsK1 || a.Pfad == "/repos/acme/alpha/commits"
	})
	if n != 0 {
		t.Errorf("%d Abrufe von Runs, Baeumen oder Commit-Dateien beim Zusammensetzen, erwartet 0", n)
	}
}
