package server_test

// GET /api/zeitleiste gegen die Fake-GitHub-API aus repo_api_test.go,
// ergaenzt um Commits je Pfad und je Commit den Patch
// (aufgaben-zeitleiste, #55). Kein Netz, kein gh.

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
)

type zlAbfragen struct {
	mu     sync.Mutex
	listen []string       // Query der Commit-Listen
	je     map[string]int // sha -> Abrufe von commits/SHA
}

func (a *zlAbfragen) liste() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.listen...)
}

func (a *zlAbfragen) abrufe(sha string) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.je[sha]
}

// zlFakeGH legt vor die Fake-API die Commits: vier auf change/vier (s-vier)
// mit drei Commits, x archiviert auf main (s-main) mit der Historie unter dem
// alten Pfad.
func zlFakeGH(t *testing.T) (*fakeGH, *zlAbfragen) {
	t.Helper()
	f := neuesFakeGH(t)
	a := &zlAbfragen{je: map[string]int{}}
	commit := func(sha, zeit, betreff string) string {
		return fmt.Sprintf(`{"sha":%q,"commit":{"message":%q,"author":{"date":%q},"committer":{"date":%q}}}`, sha, betreff, zeit, zeit)
	}
	listen := map[string]string{
		"s-vier openspec/changes/vier/tasks.md": "[" + commit("v3", "2026-09-22T10:00:00Z", "b abhaken") + "," +
			commit("v2", "2026-09-21T10:00:00Z", "a abhaken") + "," + commit("v1", "2026-09-20T10:00:00Z", "vier anlegen") + "]",
		"s-main openspec/changes/archive/2026-09-10-x/tasks.md": "[" + commit("x3", "2026-09-10T12:00:00Z", "x archivieren") + "]",
		"s-main openspec/changes/x/tasks.md": "[" + commit("x2", "2026-09-09T12:00:00Z", "x fertig") + "," +
			commit("x1", "2026-09-08T12:00:00Z", "x anlegen") + "]",
	}
	datei := func(sha, zeit, betreff, name, patch string) string {
		return fmt.Sprintf(`{"sha":%q,"commit":{"message":%q,"committer":{"date":%q}},"files":[{"filename":%q,"patch":%q}]}`,
			sha, betreff, zeit, name, patch)
	}
	vier := "openspec/changes/vier/tasks.md"
	einzeln := map[string]string{
		"v1": datei("v1", "2026-09-20T10:00:00Z", "vier anlegen", vier, "@@ -0,0 +1,3 @@\n+- [ ] a\n+- [ ] b\n+- [ ] c"),
		"v2": datei("v2", "2026-09-21T10:00:00Z", "a abhaken", vier, "@@ -1 +1 @@\n-- [ ] a\n+- [x] a"),
		"v3": datei("v3", "2026-09-22T10:00:00Z", "b abhaken", vier, "@@ -2 +2 @@\n-- [ ] b\n+- [x] b"),
		"x1": datei("x1", "2026-09-08T12:00:00Z", "x anlegen", "openspec/changes/x/tasks.md", "@@ -0,0 +1 @@\n+- [ ] fertig"),
		"x2": datei("x2", "2026-09-09T12:00:00Z", "x fertig", "openspec/changes/x/tasks.md", "@@ -1 +1 @@\n-- [ ] fertig\n+- [x] fertig"),
		"x3": datei("x3", "2026-09-10T12:00:00Z", "x archivieren", "openspec/changes/archive/2026-09-10-x/tasks.md", "@@ -0,0 +1 @@\n+- [x] fertig"),
	}
	innen := f.srv.Config.Handler
	f.srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if r.Header.Get("Authorization") != "Bearer "+tokWork {
			innen.ServeHTTP(w, r)
			return
		}
		if p == "/repos/acme/alpha/commits" {
			q := r.URL.Query()
			a.mu.Lock()
			a.listen = append(a.listen, r.URL.RawQuery)
			a.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			if q.Get("per_page") != "30" {
				w.WriteHeader(http.StatusBadRequest)
				fmt.Fprint(w, `{"message":"per_page fehlt"}`)
				return
			}
			if l, ok := listen[q.Get("sha")+" "+q.Get("path")]; ok {
				fmt.Fprint(w, l)
				return
			}
			fmt.Fprint(w, `[]`)
			return
		}
		if sha, ok := strings.CutPrefix(p, "/repos/acme/alpha/commits/"); ok {
			if body, ok := einzeln[sha]; ok {
				a.mu.Lock()
				a.je[sha]++
				a.mu.Unlock()
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, body)
				return
			}
		}
		innen.ServeHTTP(w, r)
	})
	return f, a
}

type zlAntwort struct {
	Repo      string `json:"repo"`
	Kennung   string `json:"kennung"`
	Ref       string `json:"ref"`
	Commits   int    `json:"commits"`
	Grund     string `json:"grund"`
	Eintraege []struct {
		Aufgabe string `json:"aufgabe"`
		Nummer  string `json:"nummer"`
		Zeit    string `json:"zeit"`
		SHA     string `json:"sha"`
		KurzSHA string `json:"kurz_sha"`
		Betreff string `json:"betreff"`
	} `json:"eintraege"`
}

func TestApiZeitleisteBranch(t *testing.T) {
	f, a := zlFakeGH(t)
	h, _, _ := repoHandler(t, f)

	for lauf := 1; lauf <= 2; lauf++ {
		var z zlAntwort
		if rec := holeJSON(t, h, "/api/zeitleiste?repo=acme/alpha&kennung=vier", &z); rec.Code != http.StatusOK {
			t.Fatalf("Lauf %d: Status %d: %s", lauf, rec.Code, rec.Body.String())
		}
		if z.Grund != "" {
			t.Fatalf("Lauf %d: Grund %q", lauf, z.Grund)
		}
		if z.Repo != "acme/alpha" || z.Kennung != "vier" || z.Ref != "change/vier" || z.Commits != 3 {
			t.Errorf("Lauf %d: Kopf %+v", lauf, z)
		}
		if len(z.Eintraege) != 2 {
			t.Fatalf("Lauf %d: Eintraege %+v, erwartet b und a", lauf, z.Eintraege)
		}
		b, e := z.Eintraege[0], z.Eintraege[1]
		if b.Aufgabe != "b" || b.SHA != "v3" || b.KurzSHA != "v3" || b.Betreff != "b abhaken" || b.Zeit != "2026-09-22T10:00:00Z" {
			t.Errorf("Lauf %d: erster Eintrag %+v", lauf, b)
		}
		if e.Aufgabe != "a" || e.SHA != "v2" || e.Betreff != "a abhaken" {
			t.Errorf("Lauf %d: zweiter Eintrag %+v", lauf, e)
		}
	}
	listen := a.liste()
	if len(listen) != 1 {
		t.Fatalf("Commit-Listen %v, erwartet genau eine Abfrage (zweiter Lauf aus dem Speicher)", listen)
	}
	if !strings.Contains(listen[0], "sha=s-vier") || !strings.Contains(listen[0], "per_page=30") ||
		!strings.Contains(listen[0], "path=openspec%2Fchanges%2Fvier%2Ftasks.md") {
		t.Errorf("Commit-Liste mit %q", listen[0])
	}
	for _, sha := range []string{"v1", "v2", "v3"} {
		if n := a.abrufe(sha); n != 1 {
			t.Errorf("commits/%s %d-mal abgerufen, erwartet genau einmal", sha, n)
		}
	}
}

func TestApiZeitleisteArchiviert(t *testing.T) {
	f, a := zlFakeGH(t)
	h, _, _ := repoHandler(t, f)

	var z zlAntwort
	if rec := holeJSON(t, h, "/api/zeitleiste?repo=acme/alpha&kennung=x", &z); rec.Code != http.StatusOK {
		t.Fatalf("Status %d: %s", rec.Code, rec.Body.String())
	}
	if z.Grund != "" {
		t.Fatalf("Grund %q", z.Grund)
	}
	pfade := map[string]bool{}
	for _, l := range a.liste() {
		if !strings.Contains(l, "sha=s-main") {
			t.Errorf("Commit-Liste %q nicht ab s-main", l)
		}
		for _, p := range []string{"openspec%2Fchanges%2Farchive%2F2026-09-10-x%2Ftasks.md", "openspec%2Fchanges%2Fx%2Ftasks.md"} {
			if strings.Contains(l, "path="+p) {
				pfade[p] = true
			}
		}
	}
	if len(pfade) != 2 {
		t.Errorf("abgefragte Pfade %v, erwartet Archivpfad und alten Pfad (Listen %v)", pfade, a.liste())
	}
	if len(z.Eintraege) != 1 || z.Eintraege[0].Aufgabe != "fertig" || z.Eintraege[0].SHA != "x2" {
		t.Errorf("Eintraege %+v, erwartet fertig aus x2", z.Eintraege)
	}
	if z.Commits != 3 {
		t.Errorf("commits %d, erwartet 3", z.Commits)
	}
}

func TestApiZeitleisteUnbekannt(t *testing.T) {
	f, _ := zlFakeGH(t)
	h, _, _ := repoHandler(t, f)
	if rec := holeJSON(t, h, "/api/zeitleiste?repo=acme/alpha&kennung=gibtsnicht", nil); rec.Code != http.StatusNotFound {
		t.Errorf("Status %d, erwartet 404: %s", rec.Code, rec.Body.String())
	}
	if rec := holeJSON(t, h, "/api/zeitleiste?repo=kaputt&kennung=vier", nil); rec.Code != http.StatusBadRequest {
		t.Errorf("ungueltiges Repo: Status %d, erwartet 400", rec.Code)
	}
}
