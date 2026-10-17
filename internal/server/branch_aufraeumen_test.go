package server_test

// /api/branches und /action/branch/loeschen gegen eine Fake-GitHub-API
// (branch-aufraeumen, #78).

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/niclasedge/git-planner-go/internal/assets"
	"github.com/niclasedge/git-planner-go/internal/render"
	"github.com/niclasedge/git-planner-go/internal/server"
)

type baAufruf struct {
	Methode, Pfad, Query, WennNicht string
}

type branchGH struct {
	mu      sync.Mutex
	aufrufe []baAufruf
	srv     *httptest.Server
}

func (f *branchGH) alle() []baAufruf {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]baAufruf{}, f.aufrufe...)
}

func (f *branchGH) loeschungen() []string {
	var out []string
	for _, a := range f.alle() {
		if a.Methode == http.MethodDelete {
			out = append(out, strings.TrimPrefix(a.Pfad, "/repos/acme/alpha/git/refs/heads/"))
		}
	}
	return out
}

func neuesBranchGH(t *testing.T) *branchGH {
	t.Helper()
	f := &branchGH{}
	voraus := map[string]int{"sha-alt": 0, "sha-squash": 3, "sha-neu": 1}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.aufrufe = append(f.aufrufe, baAufruf{Methode: r.Method, Pfad: r.URL.Path, Query: r.URL.RawQuery, WennNicht: r.Header.Get("If-None-Match")})
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		p := r.URL.Path
		switch {
		case p == "/user/repos":
			fmt.Fprint(w, `[{"full_name":"acme/alpha","name":"alpha","owner":{"login":"acme"},"default_branch":"main"}]`)
		case p == "/repos/acme/alpha/branches":
			fmt.Fprint(w, `[{"name":"main","protected":true,"commit":{"sha":"sha-main"}},
			  {"name":"alt","commit":{"sha":"sha-alt"}},{"name":"squash","commit":{"sha":"sha-squash"}},
			  {"name":"neu","commit":{"sha":"sha-neu"}}]`)
		case strings.HasPrefix(p, "/repos/acme/alpha/commits/"):
			sha := strings.TrimPrefix(p, "/repos/acme/alpha/commits/")
			fmt.Fprintf(w, `{"sha":%q,"commit":{"message":"Betreff %s\n\nText","committer":{"date":"2026-09-2%dT10:00:00Z"}}}`, sha, sha, voraus[sha])
		case strings.HasPrefix(p, "/repos/acme/alpha/compare/main..."):
			sha := strings.TrimPrefix(p, "/repos/acme/alpha/compare/main...")
			etag := `"e-` + sha + `"`
			if r.Header.Get("If-None-Match") == etag {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			w.Header().Set("ETag", etag)
			fmt.Fprintf(w, `{"ahead_by":%d,"behind_by":2}`, voraus[sha])
		case p == "/repos/acme/alpha/pulls":
			if r.URL.Query().Get("state") == "closed" {
				fmt.Fprint(w, `[{"number":5,"title":"Squash","html_url":"u5","state":"closed","merged_at":"2026-09-10T10:00:00Z",
				  "head":{"ref":"squash","sha":"sha-squash","repo":{"full_name":"acme/alpha"}},"base":{"ref":"main"}}]`)
				return
			}
			fmt.Fprint(w, `[]`)
		case p == "/repos/acme/alpha/issues":
			fmt.Fprint(w, `[]`)
		case strings.HasPrefix(p, "/repos/acme/alpha/git/refs/heads/") && r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"message":"Not Found"}`)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

type baAntwort struct {
	Repo    string `json:"repo"`
	Anzahl  int    `json:"anzahl"`
	Gruppen []struct {
		Name     string `json:"name"`
		Branches []struct {
			Name      string `json:"name"`
			SHA       string `json:"sha"`
			KurzSHA   string `json:"kurz_sha"`
			Betreff   string `json:"betreff"`
			Voraus    int    `json:"voraus"`
			Loeschbar bool   `json:"loeschbar"`
			Grund     string `json:"grund"`
			PR        *struct {
				Nummer  int    `json:"nummer"`
				Zustand string `json:"zustand"`
			} `json:"pr"`
		} `json:"branches"`
	} `json:"gruppen"`
}

func TestBranchesRouteLiefertGruppen(t *testing.T) {
	f := neuesBranchGH(t)
	basis, _ := schreibServer(t, &schreibGH{srv: f.srv})
	var a baAntwort
	if st := holeVon(t, basis+"/api/branches?repo=acme/alpha", &a); st != http.StatusOK {
		t.Fatalf("Status %d", st)
	}
	je := map[string][]string{}
	for _, g := range a.Gruppen {
		for _, b := range g.Branches {
			je[g.Name] = append(je[g.Name], b.Name)
		}
	}
	if len(a.Gruppen) != 4 || strings.Join(je["gemergt"], ",") != "squash,alt" || strings.Join(je["aktiv"], ",") != "neu" ||
		strings.Join(je["geschuetzt"], ",") != "main" || len(je["verlassen"]) != 0 {
		t.Fatalf("Gruppen %v (%+v)", je, a)
	}
	if a.Anzahl != 3 || a.Repo != "acme/alpha" {
		t.Errorf("Anzahl %d, Repo %q", a.Anzahl, a.Repo)
	}
	sq := a.Gruppen[0].Branches[0]
	if sq.SHA != "sha-squash" || sq.Voraus != 3 || sq.Betreff != "Betreff sha-squash" || !sq.Loeschbar || sq.PR == nil || sq.PR.Nummer != 5 || sq.PR.Zustand != "gemergt" {
		t.Errorf("squash %+v", sq)
	}

	// Der zweite Abruf fragt bedingt.
	if st := holeVon(t, basis+"/api/branches?repo=acme/alpha", &a); st != http.StatusOK {
		t.Fatalf("zweiter Abruf: Status %d", st)
	}
	var vergleiche []baAufruf
	for _, x := range f.alle() {
		if x.Pfad == "/repos/acme/alpha/compare/main...sha-alt" {
			vergleiche = append(vergleiche, x)
		}
	}
	if len(vergleiche) < 2 || vergleiche[0].WennNicht != "" || vergleiche[len(vergleiche)-1].WennNicht != `"e-sha-alt"` {
		t.Errorf("Vergleiche fuer alt %+v, erwartet zuletzt If-None-Match", vergleiche)
	}
	if n := len(f.loeschungen()); n != 0 {
		t.Errorf("Leseroute hat %d Branches geloescht", n)
	}
	if st := holeVon(t, basis+"/api/branches?repo=kaputt", nil); st != http.StatusBadRequest {
		t.Errorf("ohne gueltiges Repo: Status %d", st)
	}
}

func TestBranchAufraeumenLoeschenRoute(t *testing.T) {
	f := neuesBranchGH(t)
	basis, log := schreibServer(t, &schreibGH{srv: f.srv})

	resp, rumpf := postJSON(t, basis, "/action/branch/loeschen", `{"repo":"acme/alpha","branch":"alt","sha":"sha-alt"}`, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("alt: Status %d: %s", resp.StatusCode, rumpf)
	}
	if m := alsJSON(t, resp, rumpf); m["ok"] != true {
		t.Errorf("Antwort %s", rumpf)
	}
	if l := f.loeschungen(); strings.Join(l, ",") != "alt" {
		t.Fatalf("Loeschungen %v, erwartet alt", l)
	}
	if !strings.Contains(log.String(), "branch-loeschen") || strings.Contains(log.String(), tokWork) {
		t.Errorf("Protokoll %q", log.String())
	}
}

func TestBranchAufraeumenLoeschenGeschuetztUndVeraltet(t *testing.T) {
	f := neuesBranchGH(t)
	basis, _ := schreibServer(t, &schreibGH{srv: f.srv})
	faelle := []struct {
		rumpf  string
		status int
	}{
		{`{"repo":"acme/alpha","branch":"main","sha":"sha-main"}`, http.StatusForbidden},
		{`{"repo":"acme/alpha","branch":"alt","sha":"sha-alt-alt"}`, http.StatusConflict},
		{`{"repo":"acme/alpha","branch":"gibtsnicht","sha":"x"}`, http.StatusConflict},
		{`{"repo":"acme/alpha","branch":"alt"}`, http.StatusBadRequest},
		{`{"repo":"acme/alpha","branch":"alt","sha":"sha-alt","fremd":1}`, http.StatusBadRequest},
	}
	for _, fall := range faelle {
		resp, rumpf := postJSON(t, basis, "/action/branch/loeschen", fall.rumpf, nil)
		if resp.StatusCode != fall.status {
			t.Errorf("%s: Status %d, erwartet %d (%s)", fall.rumpf, resp.StatusCode, fall.status, rumpf)
		}
	}
	if l := f.loeschungen(); len(l) != 0 {
		t.Errorf("abgelehnte Anfragen haben geloescht: %v", l)
	}
	resp, _ := http.Get(basis + "/action/branch/loeschen")
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("GET: Status %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestBranchAufraeumenLoeschenMehrereNacheinander(t *testing.T) {
	f := neuesBranchGH(t)
	basis, _ := schreibServer(t, &schreibGH{srv: f.srv})
	resp, rumpf := postJSON(t, basis, "/action/branch/loeschen",
		`{"repo":"acme/alpha","branches":[{"branch":"alt","sha":"sha-alt"},{"branch":"squash","sha":"sha-squash"},{"branch":"main","sha":"sha-main"}]}`, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Status %d: %s", resp.StatusCode, rumpf)
	}
	var a struct {
		Ergebnisse []struct {
			Branch string `json:"branch"`
			Status int    `json:"status"`
			OK     bool   `json:"ok"`
			Fehler string `json:"fehler"`
		} `json:"ergebnisse"`
	}
	if err := json.Unmarshal(rumpf, &a); err != nil {
		t.Fatal(err)
	}
	if len(a.Ergebnisse) != 3 {
		t.Fatalf("Ergebnisse %+v", a.Ergebnisse)
	}
	e := a.Ergebnisse
	if e[0].Branch != "alt" || e[0].Status != 200 || !e[0].OK || e[1].Branch != "squash" || e[1].Status != 200 || !e[1].OK ||
		e[2].Branch != "main" || e[2].Status != 403 || e[2].OK || e[2].Fehler == "" {
		t.Errorf("Ergebnisse %+v", e)
	}
	if l := f.loeschungen(); strings.Join(l, ",") != "alt,squash" {
		t.Errorf("Loeschungen %v, erwartet alt,squash in dieser Folge", l)
	}
}

func TestBranchAufraeumenLoeschenSchutz(t *testing.T) {
	f := neuesBranchGH(t)
	basis, _ := schreibServer(t, &schreibGH{srv: f.srv})
	rumpf := `{"repo":"acme/alpha","branch":"alt","sha":"sha-alt"}`
	if resp, _ := postJSON(t, basis, "/action/branch/loeschen", rumpf, map[string]string{"Origin": "http://evil.example"}); resp.StatusCode != http.StatusForbidden {
		t.Errorf("fremde Origin: Status %d, erwartet 403", resp.StatusCode)
	}

	t.Setenv("CC_AREAS_FILE", filepath.Join(t.TempDir(), "areas.json"))
	t.Setenv("CC_KNOWLEDGE_ROOTS", t.TempDir())
	t.Setenv("CC_KNOWLEDGE_CACHE", filepath.Join(t.TempDir(), "cache.json"))
	h := server.NewHandler(server.AppOptions{
		Cwd:     t.TempDir(),
		Assets:  render.Assets{DashCSS: assets.DashCSS, BrowserCSS: assets.BrowserCSS, BrowserJS: assets.BrowserJS},
		AssetFS: assets.FS(),
	})
	netz := startOn(t, "127.0.0.2", h)
	if resp, antwort := postJSON(t, netz, "/action/branch/loeschen", rumpf, nil); resp.StatusCode != http.StatusForbidden {
		t.Errorf("Netz ohne Freigabe: Status %d, erwartet 403 (%s)", resp.StatusCode, antwort)
	}
	if l := f.loeschungen(); len(l) != 0 {
		t.Errorf("geloescht trotz Schutz: %v", l)
	}
}
