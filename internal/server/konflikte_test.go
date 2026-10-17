package server_test

// Konfliktzustand je PR in /api/arbeit und POST /action/pr/base-holen gegen
// eine Fake-GitHub-API (konflikte-anzeigen, #62). Nie gegen echtes GitHub.

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/niclasedge/git-planner-go/internal/assets"
	"github.com/niclasedge/git-planner-go/internal/render"
	"github.com/niclasedge/git-planner-go/internal/server"
)

func konfliktPR(n int, head, mergeable, mstate string) string {
	return fmt.Sprintf(`{"number":%d,"title":"PR %d","html_url":"u%d","draft":false,"node_id":"PR_%d","state":"open","merged":false,
	  "body":"closes #62","mergeable":%s,"mergeable_state":%q,"user":{"login":"cy"},
	  "head":{"ref":%q,"sha":"sha%d","repo":{"full_name":"acme/alpha"}},"base":{"ref":"main"}}`,
		n, n, n, n, mergeable, mstate, head, n)
}

func neuesKonfliktGH(t *testing.T) *prGH {
	t.Helper()
	f := &prGH{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		a := ghAufruf{Methode: r.Method, Pfad: r.URL.Path, Query: r.URL.RawQuery}
		if len(b) > 0 {
			_ = json.Unmarshal(b, &a.Rumpf)
		}
		f.mu.Lock()
		f.aufrufe = append(f.aufrufe, a)
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
			fmt.Fprint(w, `[{"number":62,"title":"konflikte-anzeigen","state":"open","labels":[],"assignees":[]},
			  {"number":20,"title":"PR 20","pull_request":{}},{"number":21,"title":"PR 21","pull_request":{}},{"number":22,"title":"PR 22","pull_request":{}}]`)
		case p == "/repos/acme/alpha/branches":
			fmt.Fprint(w, `[{"name":"main","commit":{"sha":"shamain"}},{"name":"change/einundzwanzig","commit":{"sha":"sha21"}}]`)
		case strings.HasPrefix(p, "/repos/acme/alpha/commits/"):
			fmt.Fprint(w, `{"sha":"`+strings.TrimPrefix(p, "/repos/acme/alpha/commits/")+`","commit":{"message":"Stand","committer":{"date":"2026-09-25T09:00:00Z"}}}`)
		case p == "/repos/acme/alpha/pulls":
			fmt.Fprint(w, "["+konfliktPR(20, "change/zwanzig", "null", "")+","+konfliktPR(21, "change/einundzwanzig", "null", "")+","+
				konfliktPR(22, "change/zweiundzwanzig", "null", "")+"]")
		case p == "/repos/acme/alpha/pulls/20" && r.Method == http.MethodGet:
			fmt.Fprint(w, konfliktPR(20, "change/zwanzig", "true", "behind"))
		case p == "/repos/acme/alpha/pulls/21" && r.Method == http.MethodGet:
			fmt.Fprint(w, konfliktPR(21, "change/einundzwanzig", "false", "dirty"))
		case p == "/repos/acme/alpha/pulls/22" && r.Method == http.MethodGet:
			fmt.Fprint(w, konfliktPR(22, "change/zweiundzwanzig", "true", "clean"))
		case p == "/repos/acme/alpha/compare/main...sha20":
			fmt.Fprint(w, `{"ahead_by":1,"behind_by":3,"merge_base_commit":{"sha":"basis20"},"files":[{"filename":"x.go"}]}`)
		case p == "/repos/acme/alpha/compare/main...sha21":
			fmt.Fprint(w, `{"ahead_by":2,"behind_by":4,"merge_base_commit":{"sha":"basis21"},"files":[{"filename":"a.go"},{"filename":"b.go"}]}`)
		case p == "/repos/acme/alpha/compare/basis21...main":
			fmt.Fprint(w, `{"ahead_by":4,"behind_by":0,"merge_base_commit":{"sha":"basis21"},"files":[{"filename":"b.go"},{"filename":"c.go"}]}`)
		case p == "/repos/acme/alpha/compare/main...sha22":
			fmt.Fprint(w, `{"ahead_by":1,"behind_by":0,"merge_base_commit":{"sha":"shamain"},"files":[{"filename":"y.go"}]}`)
		case strings.HasSuffix(p, "/update-branch") && r.Method == http.MethodPut:
			w.WriteHeader(http.StatusAccepted)
			fmt.Fprint(w, `{"message":"Updating pull request branch."}`)
		default:
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"message":"Not Found"}`)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

type konfliktZeile struct {
	Nummer          int      `json:"nummer"`
	Zustand         string   `json:"zustand"`
	Voraus          int      `json:"voraus"`
	Zurueck         int      `json:"zurueck"`
	KonfliktDateien []string `json:"konflikt_dateien"`
}

func TestArbeitLiefertKonfliktzustand(t *testing.T) {
	f := neuesKonfliktGH(t)
	basis := prServer(t, f)
	var a struct {
		PRs   []konfliktZeile `json:"prs"`
		Reihe []struct {
			Nummer   int `json:"nummer"`
			Branches []struct {
				Name string `json:"name"`
				PR   *struct {
					Nummer          int      `json:"nummer"`
					Zustand         string   `json:"zustand"`
					KonfliktDateien []string `json:"konflikt_dateien"`
				} `json:"pr"`
			} `json:"branches"`
		} `json:"reihe"`
	}
	if st := holeVon(t, basis+"/api/arbeit?repo=acme/alpha", &a); st != http.StatusOK {
		t.Fatalf("Status %d", st)
	}
	je := map[int]konfliktZeile{}
	for _, p := range a.PRs {
		je[p.Nummer] = p
	}
	if z := je[20]; z.Zustand != "veraltet" || z.Zurueck != 3 || z.Voraus != 1 || len(z.KonfliktDateien) != 0 {
		t.Errorf("#20 %+v, erwartet veraltet mit zurueck 3", z)
	}
	if z := je[21]; z.Zustand != "konflikt" || !reflect.DeepEqual(z.KonfliktDateien, []string{"b.go"}) {
		t.Errorf("#21 %+v, erwartet konflikt mit [b.go]", z)
	}
	if z := je[22]; z.Zustand != "sauber" || z.Zurueck != 0 {
		t.Errorf("#22 %+v, erwartet sauber", z)
	}
	gefunden := false
	for _, e := range a.Reihe {
		for _, b := range e.Branches {
			if b.PR != nil && b.PR.Nummer == 21 {
				gefunden = true
				if b.PR.Zustand != "konflikt" || !reflect.DeepEqual(b.PR.KonfliktDateien, []string{"b.go"}) {
					t.Errorf("Branch %s: PR %+v, erwartet konflikt mit [b.go]", b.Name, *b.PR)
				}
			}
		}
	}
	if !gefunden {
		t.Errorf("kein Branch mit PR #21 in der Reihe: %+v", a.Reihe)
	}
}

func TestBaseHolenRoute(t *testing.T) {
	f := neuesKonfliktGH(t)
	basis := prServer(t, f)

	resp, rumpf := postJSON(t, basis, "/action/pr/base-holen", `{"repo":"acme/alpha","nummer":21}`, nil)
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("Konflikt: Status %d, erwartet 409 (%s)", resp.StatusCode, rumpf)
	}
	var k struct {
		Fehler  string   `json:"fehler"`
		Dateien []string `json:"dateien"`
	}
	_ = json.Unmarshal(rumpf, &k)
	if k.Fehler == "" || !reflect.DeepEqual(k.Dateien, []string{"b.go"}) {
		t.Errorf("Konflikt-Antwort %s, erwartet Hinweis und dateien [b.go]", rumpf)
	}
	if resp, rumpf := postJSON(t, basis, "/action/pr/base-holen", `{"repo":"acme/alpha","nummer":22}`, nil); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("sauber: Status %d, erwartet 400 (%s)", resp.StatusCode, rumpf)
	}
	if resp, _ := postJSON(t, basis, "/action/pr/base-holen", `{"repo":"acme/alpha","nummer":20}`, map[string]string{"Origin": "https://boese.example"}); resp.StatusCode != http.StatusForbidden {
		t.Errorf("fremde Origin: Status %d, erwartet 403", resp.StatusCode)
	}
	if s := f.schreibend(); len(s) != 0 {
		t.Fatalf("abgelehnte Anfragen haben geschrieben: %+v", s)
	}

	resp, rumpf = postJSON(t, basis, "/action/pr/base-holen", `{"repo":"acme/alpha","nummer":20}`, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("veraltet: Status %d (%s)", resp.StatusCode, rumpf)
	}
	s := f.schreibend()
	if len(s) != 1 || s[0].Methode != http.MethodPut || s[0].Pfad != "/repos/acme/alpha/pulls/20/update-branch" || s[0].Rumpf["expected_head_sha"] != "sha20" {
		t.Errorf("Aufrufe %+v, erwartet genau ein PUT update-branch mit expected_head_sha sha20", s)
	}
}

// Ueber die Netz-Adresse ohne Freigabe haelt das Netz-Gate die Route wie jede
// andere Schreibroute auf.
func TestBaseHolenUeberNetzOhneFreigabe(t *testing.T) {
	f := neuesKonfliktGH(t)
	t.Setenv("CC_AREAS_FILE", filepath.Join(t.TempDir(), "areas.json"))
	t.Setenv("CC_KNOWLEDGE_ROOTS", t.TempDir())
	t.Setenv("CC_KNOWLEDGE_CACHE", filepath.Join(t.TempDir(), "cache.json"))
	t.Setenv("GKP_GITHUB_API", f.srv.URL)
	t.Setenv("GITHUB_PAT_WORK", tokWork)
	t.Setenv("GITHUB_PAT_PERSONAL", "")
	h := server.NewHandler(server.AppOptions{
		Cwd:     t.TempDir(),
		Assets:  render.Assets{DashCSS: assets.DashCSS, BrowserCSS: assets.BrowserCSS, BrowserJS: assets.BrowserJS},
		AssetFS: assets.FS(),
	})
	netz := startOn(t, "127.0.0.2", h)
	if resp, rumpf := postJSON(t, netz, "/action/pr/base-holen", `{"repo":"acme/alpha","nummer":20}`, nil); resp.StatusCode != http.StatusForbidden {
		t.Errorf("Netz ohne Freigabe: Status %d, erwartet 403 (%s)", resp.StatusCode, rumpf)
	}
	if s := f.schreibend(); len(s) != 0 {
		t.Errorf("ueber das Netz geschrieben: %+v", s)
	}
}
