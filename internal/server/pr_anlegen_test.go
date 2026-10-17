package server_test

// POST /action/branch/anlegen und POST /action/pr/anlegen gegen eine
// Fake-GitHub-API (pr-anlegen, #79), dazu der Vorschlag "anlegen" in
// /api/arbeit. Nie gegen echtes GitHub.

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/niclasedge/git-planner-go/internal/assets"
	"github.com/niclasedge/git-planner-go/internal/render"
	"github.com/niclasedge/git-planner-go/internal/server"
)

func neuesAnlegenGH(t *testing.T) *prGH {
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
			fmt.Fprint(w, `[{"number":79,"title":"pr-anlegen: Branch und PR","state":"open"}]`)
		case p == "/repos/acme/alpha/branches":
			fmt.Fprint(w, `[{"name":"main","commit":{"sha":"shamain"}}]`)
		case p == "/repos/acme/alpha/git/ref/heads/main" && r.Method == http.MethodGet:
			fmt.Fprint(w, `{"ref":"refs/heads/main","object":{"sha":"shamain","type":"commit"}}`)
		case p == "/repos/acme/alpha/git/refs" && r.Method == http.MethodPost:
			if a.Rumpf["ref"] == "refs/heads/change/vorhanden" {
				w.WriteHeader(http.StatusUnprocessableEntity)
				fmt.Fprint(w, `{"message":"Reference already exists"}`)
				return
			}
			w.WriteHeader(http.StatusCreated)
			fmt.Fprintf(w, `{"ref":%q,"object":{"sha":%q}}`, a.Rumpf["ref"], a.Rumpf["sha"])
		case p == "/repos/acme/alpha/pulls" && r.Method == http.MethodGet:
			if strings.Contains(r.URL.Query().Get("head"), "change/zehn") {
				fmt.Fprint(w, "["+prJSON(10, false, "change/zehn", "acme/alpha", "open", false, "null", "")+"]")
				return
			}
			fmt.Fprint(w, `[]`)
		case p == "/repos/acme/alpha/pulls" && r.Method == http.MethodPost:
			w.WriteHeader(http.StatusCreated)
			fmt.Fprintf(w, `{"number":90,"title":%q,"html_url":"https://github.com/acme/alpha/pull/90","draft":%v,"state":"open",
			  "head":{"ref":%q,"sha":"s90","repo":{"full_name":"acme/alpha"}},"base":{"ref":%q}}`, a.Rumpf["title"], a.Rumpf["draft"], a.Rumpf["head"], a.Rumpf["base"])
		default:
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"message":"Not Found"}`)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func TestBranchAnlegenRoute(t *testing.T) {
	f := neuesAnlegenGH(t)
	basis := prServer(t, f)
	resp, rumpf := postJSON(t, basis, "/action/branch/anlegen", `{"repo":"acme/alpha","nummer":79,"name":"change/pr-anlegen"}`, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Status %d: %s", resp.StatusCode, rumpf)
	}
	s := f.schreibend()
	if len(s) != 1 || s[0].Methode != http.MethodPost || s[0].Pfad != "/repos/acme/alpha/git/refs" ||
		s[0].Rumpf["ref"] != "refs/heads/change/pr-anlegen" || s[0].Rumpf["sha"] != "shamain" {
		t.Fatalf("Aufrufe %+v", s)
	}

	resp, rumpf = postJSON(t, basis, "/action/branch/anlegen", `{"repo":"acme/alpha","nummer":79,"name":"change/vorhanden"}`, nil)
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("vorhandener Branch: Status %d, erwartet 409 (%s)", resp.StatusCode, rumpf)
	}
	vorher := len(f.schreibend())
	for _, fall := range []string{
		`{"repo":"acme/alpha","nummer":79,"name":"main"}`,
		`{"repo":"acme/alpha","nummer":79,"name":"a..b"}`,
		`{"repo":"acme/alpha","nummer":79,"name":""}`,
		`{"repo":"acme/alpha","nummer":79,"name":"x","foo":1}`,
	} {
		if resp, rumpf := postJSON(t, basis, "/action/branch/anlegen", fall, nil); resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: Status %d, erwartet 400 (%s)", fall, resp.StatusCode, rumpf)
		}
	}
	if s := f.schreibend(); len(s) != vorher {
		t.Errorf("abgelehnte Anfragen haben geschrieben: %+v", s[vorher:])
	}
	if resp, _ := postJSON(t, basis, "/action/branch/unbekannt", `{}`, nil); resp.StatusCode != http.StatusNotFound {
		t.Errorf("unbekannte Aktion: Status %d, erwartet 404", resp.StatusCode)
	}
}

func TestPRAnlegenRoute(t *testing.T) {
	f := neuesAnlegenGH(t)
	basis := prServer(t, f)
	resp, rumpf := postJSON(t, basis, "/action/pr/anlegen",
		`{"repo":"acme/alpha","head":"change/pr-anlegen","titel":"pr-anlegen: Anlegen (#79)","text":"Closes #79","draft":true}`, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Status %d: %s", resp.StatusCode, rumpf)
	}
	var antwort struct {
		PR struct {
			Nummer int    `json:"nummer"`
			URL    string `json:"url"`
		} `json:"pr"`
	}
	if err := json.Unmarshal(rumpf, &antwort); err != nil || antwort.PR.Nummer != 90 {
		t.Errorf("Antwort %s (%v)", rumpf, err)
	}
	s := f.schreibend()
	if len(s) != 1 || s[0].Pfad != "/repos/acme/alpha/pulls" {
		t.Fatalf("Aufrufe %+v", s)
	}
	r := s[0].Rumpf
	if r["head"] != "change/pr-anlegen" || r["base"] != "main" || r["draft"] != true || r["body"] != "Closes #79" || r["title"] != "pr-anlegen: Anlegen (#79)" {
		t.Errorf("Rumpf %+v", r)
	}

	resp, rumpf = postJSON(t, basis, "/action/pr/anlegen", `{"repo":"acme/alpha","head":"change/zehn","titel":"x (#10)","text":"Closes #10"}`, nil)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("vorhandener PR: Status %d, erwartet 409 (%s)", resp.StatusCode, rumpf)
	}
	antwort.PR.Nummer = 0
	if err := json.Unmarshal(rumpf, &antwort); err != nil || antwort.PR.Nummer != 10 {
		t.Errorf("vorhandener PR nicht gemeldet: %s", rumpf)
	}
	if s := f.schreibend(); len(s) != 1 {
		t.Errorf("zweiter PR angelegt: %+v", s)
	}
	for _, fall := range []string{
		`{"repo":"acme/alpha","head":"change/pr-anlegen","titel":"","text":"Closes #79"}`,
		`{"repo":"acme/alpha","head":"main","titel":"x","text":""}`,
		`{"repo":"acme/alpha","head":"a b","titel":"x","text":""}`,
	} {
		if resp, rumpf := postJSON(t, basis, "/action/pr/anlegen", fall, nil); resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: Status %d, erwartet 400 (%s)", fall, resp.StatusCode, rumpf)
		}
	}
	if s := f.schreibend(); len(s) != 1 {
		t.Errorf("abgelehnte Anfragen haben geschrieben: %+v", s)
	}
}

func TestAnlegenInDerArbeitsliste(t *testing.T) {
	f := neuesAnlegenGH(t)
	basis := prServer(t, f)
	var a struct {
		Reihe []struct {
			Nummer  int `json:"nummer"`
			Anlegen *struct {
				Aktion string `json:"aktion"`
				Branch string `json:"branch"`
				Base   string `json:"base"`
				Titel  string `json:"titel"`
				Text   string `json:"text"`
			} `json:"anlegen"`
		} `json:"reihe"`
	}
	if st := holeVon(t, basis+"/api/arbeit?repo=acme/alpha", &a); st != http.StatusOK {
		t.Fatalf("Status %d", st)
	}
	if len(a.Reihe) != 1 || a.Reihe[0].Anlegen == nil {
		t.Fatalf("Reihe %+v", a.Reihe)
	}
	v := a.Reihe[0].Anlegen
	if v.Aktion != "branch" || v.Branch != "79-pr-anlegen-branch-und-pr" || v.Base != "main" || !strings.HasSuffix(v.Titel, "(#79)") || !strings.HasPrefix(v.Text, "Closes #79") {
		t.Errorf("anlegen %+v", *v)
	}
}

func TestAnlegenSchutz(t *testing.T) {
	f := neuesAnlegenGH(t)
	basis := prServer(t, f)
	routen := map[string]string{
		"/action/branch/anlegen": `{"repo":"acme/alpha","nummer":79,"name":"change/pr-anlegen"}`,
		"/action/pr/anlegen":     `{"repo":"acme/alpha","head":"change/pr-anlegen","titel":"x (#79)","text":"Closes #79"}`,
	}
	for pfad, rumpf := range routen {
		if resp, _ := postJSON(t, basis, pfad, rumpf, map[string]string{"Origin": "https://boese.example"}); resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s fremde Origin: Status %d, erwartet 403", pfad, resp.StatusCode)
		}
	}

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
	for pfad, rumpf := range routen {
		if resp, antwort := postJSON(t, netz, pfad, rumpf, nil); resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s Netz ohne Freigabe: Status %d, erwartet 403 (%s)", pfad, resp.StatusCode, antwort)
		}
	}
	if s := f.schreibend(); len(s) != 0 {
		t.Errorf("geschrieben trotz Schutz: %+v", s)
	}
}
