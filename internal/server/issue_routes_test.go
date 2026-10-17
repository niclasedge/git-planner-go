package server_test

// Die Schreibrouten unter /action/issue/ und /api/repo-meta gegen eine
// Fake-GitHub-API (issue-verwaltung, #26).

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
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

type ghAufruf struct {
	Methode, Pfad, Query string
	Rumpf                map[string]any
}

type schreibGH struct {
	mu      sync.Mutex
	aufrufe []ghAufruf
	srv     *httptest.Server
}

func (f *schreibGH) alle() []ghAufruf {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]ghAufruf{}, f.aufrufe...)
}

func (f *schreibGH) zaehle(methode, pfad, query string) int {
	n := 0
	for _, a := range f.alle() {
		if a.Methode == methode && a.Pfad == pfad && strings.Contains(a.Query, query) {
			n++
		}
	}
	return n
}

func (f *schreibGH) schreibend() []ghAufruf {
	var out []ghAufruf
	for _, a := range f.alle() {
		if a.Methode != http.MethodGet {
			out = append(out, a)
		}
	}
	return out
}

func neuesSchreibGH(t *testing.T) *schreibGH {
	t.Helper()
	f := &schreibGH{}
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
		if r.Header.Get("Authorization") != "Bearer "+tokWork {
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprint(w, `{"message":"Bad credentials"}`)
			return
		}
		p := r.URL.Path
		switch {
		case p == "/user/repos":
			fmt.Fprint(w, `[{"full_name":"acme/alpha","name":"alpha","owner":{"login":"acme"},"default_branch":"main"}]`)
		case p == "/repos/acme/alpha/issues" && r.Method == http.MethodGet:
			if r.URL.Query().Get("state") == "closed" {
				fmt.Fprint(w, `[]`)
				return
			}
			fmt.Fprint(w, `[{"number":4,"title":"vier","labels":[{"name":"bug","color":"d73a4a"}]}]`)
		case p == "/repos/acme/alpha/issues" && r.Method == http.MethodPost:
			w.WriteHeader(http.StatusCreated)
			fmt.Fprint(w, `{"number":31,"title":"Neu","html_url":"https://github.com/acme/alpha/issues/31","id":3131}`)
		case p == "/repos/acme/alpha/issues/4" && r.Method == http.MethodGet:
			fmt.Fprint(w, `{"number":4,"id":404,"title":"vier","body":"Text","html_url":"u4","labels":[{"name":"bug","color":"d73a4a"}],"assignees":[{"login":"bo"}]}`)
		case p == "/repos/acme/alpha/issues/4" && r.Method == http.MethodPatch:
			fmt.Fprint(w, `{"number":4,"title":"vier","state":"closed","html_url":"u4"}`)
		case p == "/repos/acme/alpha/issues/3":
			fmt.Fprint(w, `{"number":3,"id":303,"title":"drei"}`)
		case p == "/repos/acme/alpha/issues/4/comments" && r.Method == http.MethodGet:
			fmt.Fprint(w, `[{"user":{"login":"ana"},"body":"Wann?","created_at":"2026-09-20T00:00:00Z","html_url":"k1"}]`)
		case p == "/repos/acme/alpha/issues/4/comments" && r.Method == http.MethodPost:
			w.WriteHeader(http.StatusCreated)
			fmt.Fprint(w, `{"user":{"login":"ich"},"body":"Erledigt?","created_at":"2026-09-24T00:00:00Z"}`)
		case p == "/repos/acme/alpha/issues/4/dependencies/blocked_by":
			fmt.Fprint(w, `[]`)
		case p == "/repos/acme/alpha/issues/2/dependencies/blocked_by" && r.Method == http.MethodPost:
			w.WriteHeader(http.StatusCreated)
			fmt.Fprint(w, `{}`)
		case strings.HasPrefix(p, "/repos/acme/alpha/issues/2/dependencies/blocked_by/") && r.Method == http.MethodDelete:
			fmt.Fprint(w, `{}`)
		case p == "/repos/acme/alpha/labels":
			fmt.Fprint(w, `[{"name":"bug","color":"d73a4a"},{"name":"ui","color":"0e8a16"}]`)
		case p == "/repos/acme/alpha/assignees":
			fmt.Fprint(w, `[{"login":"ana"},{"login":"bo"}]`)
		case p == "/repos/acme/alpha/branches":
			fmt.Fprint(w, `[]`)
		default:
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"message":"Not Found"}`)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

// schreibServer startet den Dienst auf Loopback gegen die Fake-API.
func schreibServer(t *testing.T, f *schreibGH) (string, *bytes.Buffer) {
	t.Helper()
	t.Setenv("CC_AREAS_FILE", filepath.Join(t.TempDir(), "areas.json"))
	t.Setenv("CC_KNOWLEDGE_ROOTS", t.TempDir())
	t.Setenv("CC_KNOWLEDGE_CACHE", filepath.Join(t.TempDir(), "cache.json"))
	t.Setenv("GKP_GITHUB_API", f.srv.URL)
	t.Setenv("GITHUB_PAT_WORK", tokWork)
	t.Setenv("GITHUB_PAT_PERSONAL", "")
	log := &bytes.Buffer{}
	alt := server.RepoLog
	server.RepoLog = log
	t.Cleanup(func() { server.RepoLog = alt })
	h := server.NewHandler(server.AppOptions{
		Cwd:     t.TempDir(),
		Assets:  render.Assets{DashCSS: assets.DashCSS, BrowserCSS: assets.BrowserCSS, BrowserJS: assets.BrowserJS},
		AssetFS: assets.FS(),
	})
	return startOn(t, "127.0.0.1", h), log
}

func holeVon(t *testing.T, url string, ziel any) int {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if ziel != nil {
		if err := json.Unmarshal(b, ziel); err != nil {
			t.Fatalf("%s: kein JSON: %v\n%s", url, err, b)
		}
	}
	return resp.StatusCode
}

func TestIssueAnlegenRoute(t *testing.T) {
	f := neuesSchreibGH(t)
	basis, log := schreibServer(t, f)

	resp, rumpf := postJSON(t, basis, "/action/issue/anlegen", `{"repo":"acme/alpha","titel":"Neu","text":"Text","labels":["bug"],"zustaendig":["bo"]}`, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Status %d: %s", resp.StatusCode, rumpf)
	}
	m := alsJSON(t, resp, rumpf)
	if issue, _ := m["issue"].(map[string]any); issue == nil || fmt.Sprint(issue["nummer"]) != "31" {
		t.Errorf("Antwort %s, erwartet issue.nummer 31", rumpf)
	}
	s := f.schreibend()
	if len(s) != 1 || s[0].Methode != http.MethodPost || s[0].Pfad != "/repos/acme/alpha/issues" ||
		s[0].Rumpf["title"] != "Neu" || s[0].Rumpf["body"] != "Text" ||
		fmt.Sprint(s[0].Rumpf["labels"]) != "[bug]" || fmt.Sprint(s[0].Rumpf["assignees"]) != "[bo]" {
		t.Errorf("Schreibaufrufe %+v", s)
	}
	if !strings.Contains(log.String(), "aktion=anlegen") || !strings.Contains(log.String(), "pat=GITHUB_PAT_WORK") ||
		strings.Contains(log.String(), tokWork) {
		t.Errorf("Log:\n%s", log)
	}
}

func TestIssueRoutenLehnenAb(t *testing.T) {
	f := neuesSchreibGH(t)
	basis, _ := schreibServer(t, f)

	faelle := []struct {
		pfad, rumpf string
		kopf        map[string]string
		status      int
	}{
		{"/action/issue/anlegen", `{"repo":"acme/alpha","titel":"  "}`, nil, http.StatusBadRequest},
		{"/action/issue/anlegen", `{"repo":"nicht/da","titel":"x"}`, nil, http.StatusNotFound},
		{"/action/issue/aendern", `{"repo":"acme/alpha","nummer":4,"zustand":"kaputt"}`, nil, http.StatusBadRequest},
		{"/action/issue/aendern", `{"repo":"acme/alpha","nummer":4,"foo":1}`, nil, http.StatusBadRequest},
		{"/action/issue/kommentieren", `{"repo":"acme/alpha","nummer":4,"text":" "}`, nil, http.StatusBadRequest},
		{"/action/issue/blocker", `{"repo":"acme/alpha","nummer":2,"blocker":2}`, nil, http.StatusBadRequest},
		{"/action/issue/aendern", `{"repo":"acme/alpha","nummer":4,"zustand":"closed"}`, map[string]string{"Origin": "https://boese.example"}, http.StatusForbidden},
	}
	for _, fall := range faelle {
		resp, rumpf := postJSON(t, basis, fall.pfad, fall.rumpf, fall.kopf)
		if resp.StatusCode != fall.status {
			t.Errorf("%s %s: Status %d, erwartet %d (%s)", fall.pfad, fall.rumpf, resp.StatusCode, fall.status, rumpf)
		}
	}
	if s := f.schreibend(); len(s) != 0 {
		t.Errorf("abgelehnte Anfragen haben GitHub beschrieben: %+v", s)
	}
	if st := holeVon(t, basis+"/action/issue/aendern", nil); st != http.StatusMethodNotAllowed {
		t.Errorf("GET auf eine Schreibroute: Status %d, erwartet 405", st)
	}
}

func TestIssueAendernUndCacheVerwurf(t *testing.T) {
	f := neuesSchreibGH(t)
	basis, log := schreibServer(t, f)

	holeVon(t, basis+"/api/arbeit?repo=acme/alpha", nil)
	vorher := f.zaehle(http.MethodGet, "/repos/acme/alpha/issues", "state=open")

	resp, rumpf := postJSON(t, basis, "/action/issue/aendern", `{"repo":"acme/alpha","nummer":4,"zustand":"closed"}`, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Status %d: %s", resp.StatusCode, rumpf)
	}
	postJSON(t, basis, "/action/issue/aendern", `{"repo":"acme/alpha","nummer":4,"labels":["bug","ui"]}`, nil)
	s := f.schreibend()
	if len(s) != 2 || s[0].Methode != http.MethodPatch || s[0].Pfad != "/repos/acme/alpha/issues/4" {
		t.Fatalf("Schreibaufrufe %+v", s)
	}
	if len(s[0].Rumpf) != 2 || s[0].Rumpf["state"] != "closed" || s[0].Rumpf["state_reason"] != "completed" {
		t.Errorf("Schliessen %v, erwartet genau state und state_reason", s[0].Rumpf)
	}
	if len(s[1].Rumpf) != 1 || fmt.Sprint(s[1].Rumpf["labels"]) != "[bug ui]" {
		t.Errorf("Labels %v", s[1].Rumpf)
	}

	holeVon(t, basis+"/api/arbeit?repo=acme/alpha", nil)
	if nachher := f.zaehle(http.MethodGet, "/repos/acme/alpha/issues", "state=open"); nachher <= vorher {
		t.Errorf("nach dem Schliessen kam die Arbeitsliste aus dem Speicher (%d -> %d Abrufe)", vorher, nachher)
	}
	if !strings.Contains(log.String(), "aktion=aendern") || !strings.Contains(log.String(), "repo=acme/alpha") {
		t.Errorf("Log ohne aktion=aendern:\n%s", log)
	}
}

func TestKommentareImDetailUndKommentieren(t *testing.T) {
	f := neuesSchreibGH(t)
	basis, _ := schreibServer(t, f)

	var d struct {
		Kommentare []struct {
			Autor string `json:"autor"`
			Text  string `json:"text"`
		} `json:"kommentare"`
		BlockiertDurch []int    `json:"blockiert_durch"`
		Zustaendig     []string `json:"zustaendig"`
	}
	if st := holeVon(t, basis+"/api/detail?repo=acme/alpha&art=issue&nummer=4", &d); st != http.StatusOK {
		t.Fatalf("Status %d", st)
	}
	if len(d.Kommentare) != 1 || d.Kommentare[0].Autor != "ana" || d.Kommentare[0].Text != "Wann?" {
		t.Errorf("Kommentare %+v", d.Kommentare)
	}
	if d.BlockiertDurch == nil {
		t.Error("blockiert_durch fehlt im Detail (erwartet [] statt null)")
	}

	resp, rumpf := postJSON(t, basis, "/action/issue/kommentieren", `{"repo":"acme/alpha","nummer":4,"text":"Erledigt?"}`, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Status %d: %s", resp.StatusCode, rumpf)
	}
	s := f.schreibend()
	if len(s) != 1 || s[0].Pfad != "/repos/acme/alpha/issues/4/comments" || s[0].Rumpf["body"] != "Erledigt?" {
		t.Errorf("Schreibaufrufe %+v", s)
	}
}

func TestBlockerRoute(t *testing.T) {
	f := neuesSchreibGH(t)
	basis, _ := schreibServer(t, f)

	resp, rumpf := postJSON(t, basis, "/action/issue/blocker", `{"repo":"acme/alpha","nummer":2,"blocker":3}`, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Status %d: %s", resp.StatusCode, rumpf)
	}
	postJSON(t, basis, "/action/issue/blocker", `{"repo":"acme/alpha","nummer":2,"blocker":3,"entfernen":true}`, nil)
	s := f.schreibend()
	if len(s) != 2 || s[0].Methode != http.MethodPost || s[0].Pfad != "/repos/acme/alpha/issues/2/dependencies/blocked_by" ||
		fmt.Sprint(s[0].Rumpf["issue_id"]) != "303" {
		t.Errorf("Anlegen %+v", s)
	}
	if len(s) == 2 && (s[1].Methode != http.MethodDelete || s[1].Pfad != "/repos/acme/alpha/issues/2/dependencies/blocked_by/303") {
		t.Errorf("Loesen %+v", s[1])
	}
}

func TestRepoMeta(t *testing.T) {
	f := neuesSchreibGH(t)
	basis, _ := schreibServer(t, f)
	var m struct {
		Labels []struct {
			Name  string `json:"name"`
			Farbe string `json:"farbe"`
		} `json:"labels"`
		Zustaendige []string `json:"zustaendige"`
	}
	if st := holeVon(t, basis+"/api/repo-meta?repo=acme/alpha", &m); st != http.StatusOK {
		t.Fatalf("Status %d", st)
	}
	if len(m.Labels) != 2 || m.Labels[1].Name != "ui" || m.Labels[1].Farbe != "0e8a16" || strings.Join(m.Zustaendige, ",") != "ana,bo" {
		t.Errorf("repo-meta %+v", m)
	}
}
