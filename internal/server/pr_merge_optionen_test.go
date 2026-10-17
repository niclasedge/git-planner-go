package server_test

// Merge-Optionen, Auto-Merge, Entwurf, Historie, Wiederoeffnen und
// Bearbeiten am PR gegen eine Fake-GitHub-API (pr-merge-optionen, #82). Nie
// gegen echtes GitHub.

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

// pmPR ist ein PR in der Form der REST-API; auto ist die Methode eines
// eingeschalteten Auto-Merges oder leer.
func pmPR(n int, draft bool, state string, merged bool, auto string) string {
	am := "null"
	if auto != "" {
		am = fmt.Sprintf(`{"merge_method":%q,"enabled_by":{"login":"cy"}}`, auto)
	}
	mergedAt := "null"
	if merged {
		mergedAt = `"2026-09-20T10:00:00Z"`
	}
	closedAt := "null"
	if state == "closed" {
		closedAt = `"2026-09-24T09:00:00Z"`
		if merged {
			closedAt = mergedAt
		}
	}
	return fmt.Sprintf(`{"number":%d,"title":"PR %d","html_url":"u%d","draft":%v,"node_id":"PR_%d","state":%q,"merged":%v,
	  "merged_at":%s,"closed_at":%s,"auto_merge":%s,"mergeable":true,"mergeable_state":"clean","user":{"login":"cy"},
	  "assignees":[],"milestone":null,
	  "head":{"ref":"change/pr%d","sha":"sha%d","repo":{"full_name":"acme/alpha"}},"base":{"ref":"main"}}`,
		n, n, n, draft, n, state, merged, mergedAt, closedAt, am, n, n)
}

func neuesMergeGH(t *testing.T) *prGH {
	t.Helper()
	f := &prGH{}
	einzeln := map[string]string{
		"9":  pmPR(9, true, "open", false, ""),
		"10": pmPR(10, false, "open", false, ""),
		"11": pmPR(11, false, "open", false, "squash"),
		"7":  pmPR(7, false, "closed", true, ""),
		"8":  pmPR(8, false, "closed", false, ""),
	}
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
		case p == "/repos/acme/alpha" && r.Method == http.MethodGet:
			fmt.Fprint(w, `{"full_name":"acme/alpha","default_branch":"main","allow_merge_commit":true,"allow_squash_merge":true,"allow_rebase_merge":false}`)
		case p == "/repos/acme/alpha/issues" && r.Method == http.MethodGet:
			if r.URL.Query().Get("state") == "closed" {
				fmt.Fprint(w, `[]`)
				return
			}
			fmt.Fprint(w, `[{"number":9,"title":"PR 9","pull_request":{}},{"number":10,"title":"PR 10","pull_request":{}},{"number":11,"title":"PR 11","pull_request":{}}]`)
		case p == "/repos/acme/alpha/branches":
			fmt.Fprint(w, `[{"name":"main","commit":{"sha":"shamain"}},{"name":"change/pr8","commit":{"sha":"sha8"}}]`)
		case p == "/repos/acme/alpha/pulls" && r.Method == http.MethodGet:
			if r.URL.Query().Get("state") == "closed" {
				fmt.Fprint(w, "["+einzeln["7"]+","+einzeln["8"]+"]")
				return
			}
			fmt.Fprint(w, "["+einzeln["9"]+","+einzeln["10"]+","+einzeln["11"]+"]")
		case strings.HasPrefix(p, "/repos/acme/alpha/pulls/") && r.Method == http.MethodGet && einzeln[strings.TrimPrefix(p, "/repos/acme/alpha/pulls/")] != "":
			fmt.Fprint(w, einzeln[strings.TrimPrefix(p, "/repos/acme/alpha/pulls/")])
		case strings.HasPrefix(p, "/repos/acme/alpha/pulls/") && r.Method == http.MethodPatch:
			fmt.Fprintf(w, `{"number":%s,"state":"open"}`, strings.TrimPrefix(p, "/repos/acme/alpha/pulls/"))
		case p == "/graphql":
			fmt.Fprint(w, `{"data":{}}`)
		case strings.HasSuffix(p, "/merge") && r.Method == http.MethodPut:
			fmt.Fprint(w, `{"merged":true}`)
		case strings.HasPrefix(p, "/repos/acme/alpha/issues/") && r.Method == http.MethodPatch:
			fmt.Fprintf(w, `{"number":%s,"title":"x","state":"open"}`, strings.TrimPrefix(p, "/repos/acme/alpha/issues/"))
		case p == "/repos/acme/alpha/labels":
			fmt.Fprint(w, `[{"name":"bug","color":"d73a4a"}]`)
		case p == "/repos/acme/alpha/assignees":
			fmt.Fprint(w, `[{"login":"ana"}]`)
		case p == "/repos/acme/alpha/milestones":
			fmt.Fprint(w, `[{"number":3,"title":"Herbst","state":"open"}]`)
		default:
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"message":"Not Found"}`)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func TestPRMergenMitMethode(t *testing.T) {
	f := neuesMergeGH(t)
	basis := prServer(t, f)

	var o struct {
		Methoden []string `json:"methoden"`
		Vorgabe  string   `json:"vorgabe"`
	}
	if st := holeVon(t, basis+"/api/pr-optionen?repo=acme/alpha", &o); st != http.StatusOK {
		t.Fatalf("pr-optionen Status %d", st)
	}
	if strings.Join(o.Methoden, ",") != "merge,squash" || o.Vorgabe != "merge" {
		t.Errorf("Optionen vorher %+v", o)
	}

	for _, rumpf := range []string{`{"repo":"acme/alpha","nummer":10,"methode":"rebase"}`, `{"repo":"acme/alpha","nummer":10,"methode":"fast"}`} {
		if resp, antwort := postJSON(t, basis, "/action/pr/mergen", rumpf, nil); resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: Status %d, erwartet 400 (%s)", rumpf, resp.StatusCode, antwort)
		}
	}
	if s := f.schreibend(); len(s) != 0 {
		t.Fatalf("trotz nicht erlaubter Methode geschrieben: %+v", s)
	}

	resp, antwort := postJSON(t, basis, "/action/pr/mergen", `{"repo":"acme/alpha","nummer":10,"methode":"squash","titel":"Zehn (#10)","text":"Rumpf"}`, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("mergen Status %d: %s", resp.StatusCode, antwort)
	}
	s := f.schreibend()
	if len(s) != 1 || s[0].Pfad != "/repos/acme/alpha/pulls/10/merge" || s[0].Rumpf["merge_method"] != "squash" ||
		s[0].Rumpf["commit_title"] != "Zehn (#10)" || s[0].Rumpf["commit_message"] != "Rumpf" || s[0].Rumpf["sha"] != "sha10" {
		t.Errorf("Merge %+v", s)
	}
	if st := holeVon(t, basis+"/api/pr-optionen?repo=acme/alpha", &o); st != http.StatusOK || o.Vorgabe != "squash" {
		t.Errorf("Vorgabe nach dem Mergen %+v (Status %d), erwartet squash", o, st)
	}

	// Ohne Methode bleibt es der Merge-Commit (pr-aktionen, #32).
	if resp, antwort := postJSON(t, basis, "/action/pr/mergen", `{"repo":"acme/alpha","nummer":10}`, nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("mergen ohne Methode Status %d: %s", resp.StatusCode, antwort)
	}
	if s := f.schreibend(); len(s) != 2 || s[1].Rumpf["merge_method"] != "merge" {
		t.Errorf("ohne Methode %+v", s)
	}
}

func TestPRAutoMergeRoute(t *testing.T) {
	f := neuesMergeGH(t)
	basis := prServer(t, f)

	var a struct {
		PRs []struct {
			Nummer    int `json:"nummer"`
			AutoMerge *struct {
				Methode string `json:"methode"`
			} `json:"auto_merge"`
		} `json:"prs"`
	}
	if st := holeVon(t, basis+"/api/arbeit?repo=acme/alpha", &a); st != http.StatusOK {
		t.Fatalf("arbeit Status %d", st)
	}
	auto := map[int]string{}
	for _, p := range a.PRs {
		if p.AutoMerge != nil {
			auto[p.Nummer] = p.AutoMerge.Methode
		}
	}
	if len(auto) != 1 || auto[11] != "squash" {
		t.Errorf("auto_merge in der Arbeitsliste %v, erwartet nur #11 squash", auto)
	}

	resp, antwort := postJSON(t, basis, "/action/pr/auto-merge", `{"repo":"acme/alpha","nummer":10,"an":true,"methode":"squash","titel":"T"}`, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("auto-merge an Status %d: %s", resp.StatusCode, antwort)
	}
	s := f.schreibend()
	if len(s) != 1 || s[0].Pfad != "/graphql" {
		t.Fatalf("Aufrufe %+v", s)
	}
	if q := fmt.Sprint(s[0].Rumpf); !strings.Contains(q, "enablePullRequestAutoMerge") || !strings.Contains(q, "PR_10") || !strings.Contains(q, "SQUASH") {
		t.Errorf("Mutation %s", q)
	}

	abgelehnt := []string{
		`{"repo":"acme/alpha","nummer":10,"an":true,"methode":"rebase"}`, // nicht erlaubt
		`{"repo":"acme/alpha","nummer":9,"an":true,"methode":"merge"}`,   // Entwurf
		`{"repo":"acme/alpha","nummer":8,"an":true,"methode":"merge"}`,   // geschlossen
		`{"repo":"acme/alpha","nummer":10,"an":false}`,                   // kein Auto-Merge an
	}
	for _, rumpf := range abgelehnt {
		if resp, antwort := postJSON(t, basis, "/action/pr/auto-merge", rumpf, nil); resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: Status %d, erwartet 400 (%s)", rumpf, resp.StatusCode, antwort)
		}
	}
	if s := f.schreibend(); len(s) != 1 {
		t.Fatalf("abgelehnte Aufrufe haben geschrieben: %+v", s[1:])
	}

	if resp, antwort := postJSON(t, basis, "/action/pr/auto-merge", `{"repo":"acme/alpha","nummer":11,"an":false}`, nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("auto-merge aus Status %d: %s", resp.StatusCode, antwort)
	}
	s = f.schreibend()
	if len(s) != 2 || !strings.Contains(fmt.Sprint(s[1].Rumpf), "disablePullRequestAutoMerge") || !strings.Contains(fmt.Sprint(s[1].Rumpf), "PR_11") {
		t.Errorf("aus %+v", s)
	}
}

func TestPRZuEntwurfRoute(t *testing.T) {
	f := neuesMergeGH(t)
	basis := prServer(t, f)
	if resp, antwort := postJSON(t, basis, "/action/pr/zu-entwurf", `{"repo":"acme/alpha","nummer":10}`, nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("Status %d: %s", resp.StatusCode, antwort)
	}
	for _, n := range []int{9, 8} {
		if resp, _ := postJSON(t, basis, "/action/pr/zu-entwurf", fmt.Sprintf(`{"repo":"acme/alpha","nummer":%d}`, n), nil); resp.StatusCode != http.StatusBadRequest {
			t.Errorf("#%d: Status %d, erwartet 400", n, resp.StatusCode)
		}
	}
	s := f.schreibend()
	if len(s) != 1 || !strings.Contains(fmt.Sprint(s[0].Rumpf), "convertPullRequestToDraft") || !strings.Contains(fmt.Sprint(s[0].Rumpf), "PR_10") {
		t.Errorf("Aufrufe %+v", s)
	}
}

func TestPRGeschlosseneUndWiederOeffnen(t *testing.T) {
	f := neuesMergeGH(t)
	basis := prServer(t, f)
	var g struct {
		PRs []struct {
			Nummer          int    `json:"nummer"`
			Gemergt         bool   `json:"gemergt"`
			Datum           string `json:"datum"`
			BranchExistiert bool   `json:"branch_existiert"`
			WiederOeffnen   bool   `json:"wieder_oeffnen"`
		} `json:"prs"`
	}
	if st := holeVon(t, basis+"/api/pr-geschlossen?repo=acme/alpha", &g); st != http.StatusOK {
		t.Fatalf("Status %d", st)
	}
	if len(g.PRs) != 2 || g.PRs[0].Nummer != 8 || g.PRs[1].Nummer != 7 {
		t.Fatalf("Historie %+v, erwartet #8 vor #7", g.PRs)
	}
	if g.PRs[0].Gemergt || !g.PRs[0].BranchExistiert || !g.PRs[0].WiederOeffnen || !strings.HasPrefix(g.PRs[0].Datum, "2026-09-24") {
		t.Errorf("#8 %+v", g.PRs[0])
	}
	if !g.PRs[1].Gemergt || g.PRs[1].BranchExistiert || g.PRs[1].WiederOeffnen || !strings.HasPrefix(g.PRs[1].Datum, "2026-09-20") {
		t.Errorf("#7 %+v", g.PRs[1])
	}

	if resp, antwort := postJSON(t, basis, "/action/pr/wieder-oeffnen", `{"repo":"acme/alpha","nummer":7}`, nil); resp.StatusCode != http.StatusConflict {
		t.Errorf("gemergter #7: Status %d, erwartet 409 (%s)", resp.StatusCode, antwort)
	}
	if resp, _ := postJSON(t, basis, "/action/pr/wieder-oeffnen", `{"repo":"acme/alpha","nummer":10}`, nil); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("offener #10: Status %d, erwartet 400", resp.StatusCode)
	}
	if s := f.schreibend(); len(s) != 0 {
		t.Fatalf("geschrieben trotz Ablehnung: %+v", s)
	}
	if resp, antwort := postJSON(t, basis, "/action/pr/wieder-oeffnen", `{"repo":"acme/alpha","nummer":8}`, nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("#8: Status %d: %s", resp.StatusCode, antwort)
	}
	s := f.schreibend()
	if len(s) != 1 || s[0].Methode != http.MethodPatch || s[0].Pfad != "/repos/acme/alpha/pulls/8" || s[0].Rumpf["state"] != "open" {
		t.Errorf("Aufrufe %+v", s)
	}
}

func TestPRBearbeitenUndMilestone(t *testing.T) {
	f := neuesMergeGH(t)
	basis := prServer(t, f)
	resp, antwort := postJSON(t, basis, "/action/pr/bearbeiten", `{"repo":"acme/alpha","nummer":10,"labels":["bug"],"zustaendig":["ana"],"meilenstein":3}`, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Status %d: %s", resp.StatusCode, antwort)
	}
	if resp, _ := postJSON(t, basis, "/action/pr/bearbeiten", `{"repo":"acme/alpha","nummer":10}`, nil); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("ohne Feld: Status %d, erwartet 400", resp.StatusCode)
	}
	if resp, antwort := postJSON(t, basis, "/action/issue/aendern", `{"repo":"acme/alpha","nummer":31,"meilenstein":0}`, nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("issue/aendern Status %d: %s", resp.StatusCode, antwort)
	}
	s := f.schreibend()
	if len(s) != 2 {
		t.Fatalf("Aufrufe %+v", s)
	}
	pr := s[0]
	if pr.Methode != http.MethodPatch || pr.Pfad != "/repos/acme/alpha/issues/10" || fmt.Sprint(pr.Rumpf["labels"]) != "[bug]" ||
		fmt.Sprint(pr.Rumpf["assignees"]) != "[ana]" || fmt.Sprint(pr.Rumpf["milestone"]) != "3" {
		t.Errorf("PR bearbeiten %+v", pr)
	}
	if v, ok := s[1].Rumpf["milestone"]; s[1].Pfad != "/repos/acme/alpha/issues/31" || !ok || v != nil {
		t.Errorf("Milestone am Issue entfernen %+v", s[1])
	}

	var m struct {
		Meilensteine []struct {
			Nummer int    `json:"nummer"`
			Titel  string `json:"titel"`
		} `json:"meilensteine"`
	}
	if st := holeVon(t, basis+"/api/repo-meta?repo=acme/alpha", &m); st != http.StatusOK {
		t.Fatalf("repo-meta Status %d", st)
	}
	if len(m.Meilensteine) != 1 || m.Meilensteine[0].Nummer != 3 || m.Meilensteine[0].Titel != "Herbst" {
		t.Errorf("meilensteine %+v", m.Meilensteine)
	}
}

func TestPRMergeOptionenSchutz(t *testing.T) {
	f := neuesMergeGH(t)
	basis := prServer(t, f)
	routen := map[string]string{
		"/action/pr/mergen":         `{"repo":"acme/alpha","nummer":10,"methode":"squash"}`,
		"/action/pr/auto-merge":     `{"repo":"acme/alpha","nummer":10,"an":true,"methode":"squash"}`,
		"/action/pr/zu-entwurf":     `{"repo":"acme/alpha","nummer":10}`,
		"/action/pr/wieder-oeffnen": `{"repo":"acme/alpha","nummer":8}`,
		"/action/pr/bearbeiten":     `{"repo":"acme/alpha","nummer":10,"labels":["bug"]}`,
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
