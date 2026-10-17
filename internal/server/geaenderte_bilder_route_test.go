package server_test

// GET /api/geaenderte-bilder gegen die Fake-GitHub-API aus repo_api_test.go,
// ergaenzt um die juengsten Commits auf main und deren Dateien
// (beleg-bilder, #105). Der archivierte Change x traegt den Bildbeleg
// 0-x.png mit Blob b-png; seine Kopie unter docs/belege/ faellt weg.

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
)

type gbAbfragen struct {
	mu     sync.Mutex
	listen []string
	je     map[string]int
}

func (a *gbAbfragen) liste() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.listen...)
}

func (a *gbAbfragen) abrufe(sha string) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.je[sha]
}

func gbFakeGH(t *testing.T) (*fakeGH, *gbAbfragen) {
	t.Helper()
	f := neuesFakeGH(t)
	a := &gbAbfragen{je: map[string]int{}}
	kopf := func(sha, zeit, betreff string) string {
		return fmt.Sprintf(`{"sha":%q,"commit":{"message":%q,"committer":{"date":%q}}}`, sha, betreff, zeit)
	}
	einzeln := map[string]string{
		"k2": `{"sha":"k2","commit":{"message":"Entwurf nachgezogen","committer":{"date":"2026-09-27T10:00:00Z"}},
		 "files":[{"filename":"docs/entwurf.png","sha":"b-entwurf","status":"modified"},{"filename":"main.go","sha":"b-go","status":"modified"}]}`,
		"k1": `{"sha":"k1","commit":{"message":"Beleg kopiert","committer":{"date":"2026-09-26T10:00:00Z"}},
		 "files":[{"filename":"docs/belege/x.png","sha":"b-png","status":"added"},{"filename":"docs/plan.svg","sha":"b-plan","status":"added"},
		          {"filename":"openspec/changes/archive/2026-09-10-x/belege/assets/0-x.png","sha":"b-png","status":"added"}]}`,
	}
	innen := f.srv.Config.Handler
	f.srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if r.Header.Get("Authorization") != "Bearer "+tokWork {
			innen.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if p == "/repos/acme/alpha/commits" {
			a.mu.Lock()
			a.listen = append(a.listen, r.URL.RawQuery)
			a.mu.Unlock()
			q := r.URL.Query()
			if q.Get("sha") != "main" || q.Get("per_page") != "30" {
				w.WriteHeader(http.StatusBadRequest)
				fmt.Fprint(w, `{"message":"sha oder per_page falsch"}`)
				return
			}
			fmt.Fprint(w, "["+kopf("k2", "2026-09-27T10:00:00Z", "Entwurf nachgezogen")+","+kopf("k1", "2026-09-26T10:00:00Z", "Beleg kopiert")+"]")
			return
		}
		if sha, ok := strings.CutPrefix(p, "/repos/acme/alpha/commits/"); ok {
			if body, ok := einzeln[sha]; ok {
				a.mu.Lock()
				a.je[sha]++
				a.mu.Unlock()
				fmt.Fprint(w, body)
				return
			}
		}
		innen.ServeHTTP(w, r)
	})
	return f, a
}

type testGeaendert struct {
	Repo   string `json:"repo"`
	Grund  string `json:"grund"`
	Bilder []struct {
		Pfad    string `json:"pfad"`
		Name    string `json:"name"`
		Typ     string `json:"typ"`
		SHA     string `json:"sha"`
		Zeit    string `json:"zeit"`
		Commit  string `json:"commit"`
		Betreff string `json:"betreff"`
		Bild    string `json:"bild"`
		Link    string `json:"link"`
	} `json:"bilder"`
}

func TestApiGeaenderteBilder(t *testing.T) {
	f, a := gbFakeGH(t)
	h, _, _ := repoHandler(t, f)

	for lauf := 1; lauf <= 2; lauf++ {
		var g testGeaendert
		rec := holeJSON(t, h, "/api/geaenderte-bilder?repo=acme/alpha", &g)
		if rec.Code != http.StatusOK {
			t.Fatalf("Lauf %d: Status %d: %s", lauf, rec.Code, rec.Body.String())
		}
		if g.Repo != "acme/alpha" || g.Grund != "" {
			t.Errorf("Lauf %d: repo %q grund %q", lauf, g.Repo, g.Grund)
		}
		var pfade []string
		for _, b := range g.Bilder {
			pfade = append(pfade, b.Pfad)
		}
		if strings.Join(pfade, ",") != "docs/entwurf.png,docs/plan.svg" {
			t.Fatalf("Lauf %d: Bilder %v, erwartet docs/entwurf.png, docs/plan.svg", lauf, pfade)
		}
		e := g.Bilder[0]
		if e.Name != "entwurf.png" || e.Typ != "png" || e.SHA != "b-entwurf" || e.Commit != "k2" || e.Betreff != "Entwurf nachgezogen" || e.Zeit != "2026-09-27T10:00:00Z" {
			t.Errorf("Lauf %d: entwurf.png %+v", lauf, e)
		}
		if !strings.HasPrefix(e.Bild, "/api/blob?") || !strings.Contains(e.Bild, "sha=b-entwurf") || !strings.Contains(e.Bild, "name=entwurf.png") {
			t.Errorf("Lauf %d: Bild-URL %q", lauf, e.Bild)
		}
		if e.Link != "https://github.com/acme/alpha/blob/k2/docs/entwurf.png" {
			t.Errorf("Lauf %d: Link %q", lauf, e.Link)
		}
	}
	for _, q := range a.liste() {
		if !strings.Contains(q, "sha=main") || !strings.Contains(q, "per_page=30") {
			t.Errorf("Commit-Liste mit %q, erwartet sha=main und per_page=30", q)
		}
	}
	if len(a.liste()) == 0 {
		t.Error("keine Commit-Liste abgefragt")
	}
	for _, sha := range []string{"k1", "k2"} {
		if n := a.abrufe(sha); n != 1 {
			t.Errorf("Commit %s %d-mal einzeln geholt, erwartet 1", sha, n)
		}
	}

	if rec := getRaw(t, h, "/api/geaenderte-bilder?repo=acme/unbekannt"); rec.Code != http.StatusNotFound {
		t.Errorf("unbekanntes Repo: Status %d, erwartet 404", rec.Code)
	}
	if rec := getRaw(t, h, "/api/geaenderte-bilder"); rec.Code != http.StatusBadRequest {
		t.Errorf("ohne repo: Status %d, erwartet 400", rec.Code)
	}
}

// Der Beleg der Galerie traegt seinen Blob-SHA — daran haengt das
// Deduplizieren der Kopie unter docs/belege/.
func TestApiBelegGalerieMitSHA(t *testing.T) {
	f := neuesFakeGH(t)
	h, _, _ := repoHandler(t, f)
	var g struct {
		Belege []struct {
			Name string `json:"name"`
			SHA  string `json:"sha"`
		} `json:"belege"`
	}
	holeJSON(t, h, "/api/beleg-galerie?repo=acme/alpha", &g)
	if len(g.Belege) != 1 || g.Belege[0].SHA != "b-png" {
		t.Errorf("Belege %+v, erwartet 0-x.png mit sha b-png", g.Belege)
	}
}

// Scheitert die Commit-Liste, antwortet die Route mit 200, leerer Liste und
// Grund.
func TestApiGeaenderteBilderFehler(t *testing.T) {
	f := neuesFakeGH(t)
	h, _, _ := repoHandler(t, f)
	var g testGeaendert
	rec := holeJSON(t, h, "/api/geaenderte-bilder?repo=acme/alpha", &g)
	if rec.Code != http.StatusOK {
		t.Fatalf("Status %d: %s", rec.Code, rec.Body.String())
	}
	if g.Grund == "" || len(g.Bilder) != 0 || !strings.Contains(rec.Body.String(), `"bilder":[]`) {
		t.Errorf("erwartet leere Liste mit Grund: %s", rec.Body.String())
	}
}
