package server_test

// /api/repos, /api/arbeit, /api/detail, /api/blob und bereich/einklappen
// gegen eine Fake-GitHub-API (repo-leiste, #25). Kein Netz, kein gh.

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/niclasedge/git-planner-go/internal/assets"
	"github.com/niclasedge/git-planner-go/internal/render"
	"github.com/niclasedge/git-planner-go/internal/server"
)

const (
	tokWork   = "tok-work-secret"
	tokPrivat = "tok-privat-geheim"
)

// fakeGH ist eine kleine GitHub-API mit Protokoll.
type fakeGH struct {
	mu        sync.Mutex
	pfade     []string
	blobs     map[string]string
	blobZaehl map[string]int
	privat401 bool
	// blockerFehler laesst dependencies/blocked_by scheitern (karte-blocker-streng, #101).
	blockerFehler bool
	srv           *httptest.Server
}

func (f *fakeGH) anfragen(praefix string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, p := range f.pfade {
		if strings.HasPrefix(p, praefix) {
			n++
		}
	}
	return n
}

func baumJSON(sha string, dateien map[string]string) string {
	var ein []string
	for pfad, blob := range dateien {
		ein = append(ein, fmt.Sprintf(`{"path":%q,"type":"blob","sha":%q,"size":1}`, pfad, blob))
	}
	return fmt.Sprintf(`{"sha":%q,"truncated":false,"tree":[%s]}`, sha, strings.Join(ein, ","))
}

func neuesFakeGH(t *testing.T) *fakeGH {
	t.Helper()
	f := &fakeGH{blobZaehl: map[string]int{}, blobs: map[string]string{
		"b-drei-prop":  "# drei\n\nGeht zurueck auf GitHub-Issue #3.\n",
		"b-drei-tasks": "- [ ] eins\n- [ ] zwei\n",
		"b-drei-spec":  "# kern\n\n## ADDED Requirements\n",
		"b-vier-main":  "- [ ] a\n- [ ] b\n- [ ] c\n",
		"b-vier-prop":  "Geht zurueck auf GitHub-Issue #4.\n",
		"b-vier-ast":   "- [x] a\n- [ ] b\n- [ ] c\n",
		"b-los-prop":   "# los\n\nohne Verweis\n",
		"b-x-prop":     "Geht zurueck auf GitHub-Issue #15.\n",
		"b-x-tasks":    "- [x] fertig\n",
		"b-x-belege":   `{"zeit":"z","stufe":"nachher","art":"bild","pfad":"belege/assets/0-x.png"}` + "\n",
		"b-png":        "\x89PNG\r\n\x1a\n-bild",
	}}
	baumMain := baumJSON("s-main", map[string]string{
		"openspec/changes/drei/proposal.md":                           "b-drei-prop",
		"openspec/changes/drei/tasks.md":                              "b-drei-tasks",
		"openspec/changes/drei/specs/kern/spec.md":                    "b-drei-spec",
		"openspec/changes/vier/tasks.md":                              "b-vier-main",
		"openspec/changes/los/proposal.md":                            "b-los-prop",
		"openspec/changes/archive/2026-09-10-x/proposal.md":           "b-x-prop",
		"openspec/changes/archive/2026-09-10-x/tasks.md":              "b-x-tasks",
		"openspec/changes/archive/2026-09-10-x/belege/belege.jsonl":   "b-x-belege",
		"openspec/changes/archive/2026-09-10-x/belege/assets/0-x.png": "b-png",
	})
	baumVier := baumJSON("s-vier", map[string]string{
		"openspec/changes/vier/proposal.md": "b-vier-prop",
		"openspec/changes/vier/tasks.md":    "b-vier-ast",
	})
	var geschlossen []string
	for n := 15; n >= 10; n-- {
		geschlossen = append(geschlossen, fmt.Sprintf(`{"number":%d,"title":"zu %d","closed_at":"2026-09-%02dT00:00:00Z"}`, n, n, n))
	}

	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.pfade = append(f.pfade, r.URL.Path)
		f.mu.Unlock()
		auth := r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		p := r.URL.Path
		switch {
		case p == "/user/repos" && auth == "Bearer "+tokWork:
			if r.URL.Query().Get("page") == "2" {
				fmt.Fprint(w, `[{"full_name":"acme/beta","name":"beta","owner":{"login":"acme"}}]`)
				return
			}
			w.Header().Set("Link", fmt.Sprintf(`<%s/user/repos?per_page=100&page=2>; rel="next"`, f.srv.URL))
			fmt.Fprint(w, `[{"full_name":"acme/alpha","name":"alpha","owner":{"login":"acme"},"description":"Alpha-Dienst","default_branch":"main","open_issues_count":5}]`)
		case p == "/user/repos" && auth == "Bearer "+tokPrivat:
			if f.privat401 {
				w.WriteHeader(http.StatusUnauthorized)
				fmt.Fprintf(w, `{"message":"Bad credentials %s"}`, tokPrivat)
				return
			}
			fmt.Fprint(w, `[{"full_name":"ACME/Alpha","name":"Alpha","owner":{"login":"ACME"}},{"full_name":"ich/gamma","name":"gamma","owner":{"login":"ich"}}]`)
		case auth != "Bearer "+tokWork:
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"message":"Not Found"}`)
		case p == "/repos/acme/alpha/issues" && r.URL.Query().Get("state") == "open":
			fmt.Fprint(w, `[
			 {"number":1,"title":"eins","updated_at":"2026-09-20T00:00:00Z","labels":[{"name":"ready-for-agent","color":"0e8a16"}]},
			 {"number":2,"title":"zwei","issue_dependencies_summary":{"blocked_by":1}},
			 {"number":3,"title":"drei: bauen","labels":[{"name":"bug","color":"d73a4a"},{"name":"ready-for-agent","color":"0e8a16"}]},
			 {"number":4,"title":"vier","body":"Vier bauen","labels":[{"name":"needs-info","color":"d876e3"}]},
			 {"number":9,"title":"ein PR","pull_request":{"url":"x"}}]`)
		case p == "/repos/acme/alpha/issues" && r.URL.Query().Get("state") == "closed":
			fmt.Fprint(w, "["+strings.Join(geschlossen, ",")+"]")
		case p == "/repos/acme/alpha/issues/2/dependencies/blocked_by" && f.blockerFehler:
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprint(w, `{"message":"kaputt"}`)
		case p == "/repos/acme/alpha/issues/2/dependencies/blocked_by":
			fmt.Fprint(w, `[{"number":3,"state":"open","repository_url":"`+f.srv.URL+`/repos/acme/alpha"}]`)
		case p == "/repos/acme/alpha/issues/4":
			fmt.Fprint(w, `{"number":4,"title":"vier","body":"Vier **bauen**","html_url":"https://github.com/acme/alpha/issues/4","labels":[]}`)
		case p == "/repos/acme/alpha/branches":
			fmt.Fprint(w, `[{"name":"main","commit":{"sha":"s-main"}},{"name":"change/vier","commit":{"sha":"s-vier"}},{"name":"feature/x","commit":{"sha":"s-x"}},{"name":"change/x","commit":{"sha":"s-alt"}}]`)
		case p == "/repos/acme/alpha/git/trees/s-main":
			fmt.Fprint(w, baumMain)
		case p == "/repos/acme/alpha/git/trees/s-alt":
			// Der Branch eines laengst archivierten Changes, nach dem Merge
			// nicht geloescht: er traegt x noch als offenen Change.
			fmt.Fprint(w, baumJSON("s-alt", map[string]string{"openspec/changes/x/proposal.md": "b-los-prop", "openspec/changes/x/tasks.md": "b-x-tasks"}))
		case p == "/repos/acme/alpha/git/trees/s-vier":
			fmt.Fprint(w, baumVier)
		case strings.HasPrefix(p, "/repos/acme/alpha/git/blobs/"):
			sha := strings.TrimPrefix(p, "/repos/acme/alpha/git/blobs/")
			inhalt, ok := f.blobs[sha]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			f.mu.Lock()
			f.blobZaehl[sha]++
			f.mu.Unlock()
			fmt.Fprintf(w, `{"sha":%q,"encoding":"base64","content":%q}`, sha, base64.StdEncoding.EncodeToString([]byte(inhalt)))
		default:
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"message":"Not Found"}`)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

// repoHandler baut den Handler gegen die Fake-API. Alle Umgebungsvariablen
// stehen VOR NewHandler.
func repoHandler(t *testing.T, f *fakeGH) (http.Handler, string, *bytes.Buffer) {
	t.Helper()
	areaDatei := filepath.Join(t.TempDir(), "areas.json")
	t.Setenv("CC_AREAS_FILE", areaDatei)
	t.Setenv("CC_KNOWLEDGE_ROOTS", t.TempDir())
	t.Setenv("CC_KNOWLEDGE_CACHE", filepath.Join(t.TempDir(), "cache.json"))
	t.Setenv("GKP_GITHUB_API", f.srv.URL)
	t.Setenv("GITHUB_PAT_WORK", tokWork)
	t.Setenv("GITHUB_PAT_PERSONAL", tokPrivat)
	log := &bytes.Buffer{}
	alt := server.RepoLog
	server.RepoLog = log
	t.Cleanup(func() { server.RepoLog = alt })
	h := server.NewHandler(server.AppOptions{
		Cwd:     t.TempDir(),
		Assets:  render.Assets{DashCSS: assets.DashCSS, BrowserCSS: assets.BrowserCSS, BrowserJS: assets.BrowserJS},
		AssetFS: assets.FS(),
	})
	return h, areaDatei, log
}

func holeJSON(t *testing.T, h http.Handler, pfad string, ziel any) *httptest.ResponseRecorder {
	t.Helper()
	rec := getRaw(t, h, pfad)
	if ziel != nil && rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), ziel); err != nil {
			t.Fatalf("%s: kein JSON: %v\n%s", pfad, err, rec.Body.String())
		}
	}
	return rec
}

type testLeiste struct {
	Kategorien []struct {
		Name        string `json:"name"`
		Ohne        bool   `json:"ohne"`
		Eingeklappt bool   `json:"eingeklappt"`
		Repos       []struct {
			NWO          string `json:"nwo"`
			PAT          string `json:"pat"`
			Beschreibung string `json:"beschreibung"`
		} `json:"repos"`
	} `json:"kategorien"`
	Konten []struct {
		PAT    string `json:"pat"`
		Anzahl int    `json:"anzahl"`
		Grund  string `json:"grund"`
	} `json:"konten"`
}

func (l testLeiste) nwos() map[string]string {
	out := map[string]string{}
	for _, k := range l.Kategorien {
		for _, r := range k.Repos {
			out[r.NWO] = r.PAT
		}
	}
	return out
}

func TestApiReposZweiPATs(t *testing.T) {
	f := neuesFakeGH(t)
	h, _, log := repoHandler(t, f)

	var l testLeiste
	if rec := holeJSON(t, h, "/api/repos", &l); rec.Code != http.StatusOK {
		t.Fatalf("Status %d: %s", rec.Code, rec.Body.String())
	}
	repos := l.nwos()
	if len(repos) != 3 || repos["acme/alpha"] != "GITHUB_PAT_WORK" || repos["acme/beta"] == "" || repos["ich/gamma"] != "GITHUB_PAT_PERSONAL" {
		t.Errorf("Repos %v, erwartet acme/alpha (ACME), acme/beta, ich/gamma", repos)
	}
	if len(l.Konten) != 2 || l.Konten[0].Anzahl != 2 || l.Konten[1].Anzahl != 2 || l.Konten[0].Grund != "" || l.Konten[1].Grund != "" {
		t.Errorf("Konten %+v", l.Konten)
	}
	if !strings.Contains(log.String(), "pat=GITHUB_PAT_WORK") || !strings.Contains(log.String(), "repos=2") {
		t.Errorf("Log ohne pat=GITHUB_PAT_WORK repos=2:\n%s", log)
	}
}

func TestApiReposEinPATScheitert(t *testing.T) {
	f := neuesFakeGH(t)
	f.privat401 = true
	h, _, log := repoHandler(t, f)

	rec := holeJSON(t, h, "/api/repos", nil)
	var l testLeiste
	_ = json.Unmarshal(rec.Body.Bytes(), &l)
	if _, ok := l.nwos()["acme/alpha"]; !ok {
		t.Errorf("Repos des ACME-PATs fehlen: %v", l.nwos())
	}
	var grund string
	for _, k := range l.Konten {
		if k.PAT == "GITHUB_PAT_PERSONAL" {
			grund = k.Grund
		}
	}
	if !strings.Contains(grund, "401") {
		t.Errorf("Grund %q, erwartet 401", grund)
	}
	for _, tok := range []string{tokWork, tokPrivat} {
		if strings.Contains(rec.Body.String(), tok) || strings.Contains(log.String(), tok) {
			t.Errorf("Tokenwert in Antwort oder Log:\n%s\n%s", rec.Body.String(), log)
		}
	}
}

func TestApiReposKategorienAusAreas(t *testing.T) {
	f := neuesFakeGH(t)
	h, areaDatei, _ := repoHandler(t, f)
	if err := os.WriteFile(areaDatei, []byte(`{"version":1,"areas":["Arbeit","Privat"],"zuordnung":{"acme/beta":"Arbeit","acme/alpha":"Arbeit"},"eingeklappt":["Arbeit"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	var l testLeiste
	holeJSON(t, h, "/api/repos", &l)
	var namen []string
	for _, k := range l.Kategorien {
		namen = append(namen, k.Name)
	}
	if strings.Join(namen, "|") != "Arbeit|Privat|Ohne Bereich" {
		t.Fatalf("Kategorien %v", namen)
	}
	if !l.Kategorien[0].Eingeklappt || len(l.Kategorien[0].Repos) != 2 || l.Kategorien[0].Repos[0].NWO != "acme/alpha" {
		t.Errorf("Arbeit %+v", l.Kategorien[0])
	}
}

type testEintrag struct {
	Art            string  `json:"art"`
	Nummer         int     `json:"nummer"`
	Status         string  `json:"status"`
	Grund          *string `json:"grund"`
	BlockiertDurch []int   `json:"blockiert_durch"`
	Change         *struct {
		Kennung  string `json:"kennung"`
		Ref      string `json:"ref"`
		Erledigt int    `json:"erledigt"`
		Gesamt   int    `json:"gesamt"`
		Belege   []struct {
			Bild string `json:"bild"`
		} `json:"belege"`
	} `json:"change"`
}

type testArbeit struct {
	Zuletzt   []testEintrag `json:"zuletzt"`
	Reihe     []testEintrag `json:"reihe"`
	OhneIssue []testEintrag `json:"ohne_issue"`
	PRs       []struct {
		Nummer int `json:"nummer"`
	} `json:"prs"`
}

func folge(es []testEintrag) string {
	var s []string
	for _, e := range es {
		if e.Art == "change" && e.Change != nil {
			s = append(s, e.Change.Kennung)
		} else {
			s = append(s, fmt.Sprint(e.Nummer))
		}
	}
	return strings.Join(s, ",")
}

func TestApiArbeit(t *testing.T) {
	f := neuesFakeGH(t)
	h, _, log := repoHandler(t, f)

	var a testArbeit
	if rec := holeJSON(t, h, "/api/arbeit?repo=acme/alpha", &a); rec.Code != http.StatusOK {
		t.Fatalf("Status %d: %s", rec.Code, rec.Body.String())
	}
	// Freie Wurzeln nach entsperrt (karte-reihe-entsperrt, #104): #3 gibt
	// #2 frei und steht vor #1.
	if got := folge(a.Reihe); got != "4,3,2,1" {
		t.Fatalf("reihe %s, erwartet 4,3,2,1", got)
	}
	for i, w := range []string{"in_arbeit", "bereit", "blockiert", "ohne_change"} {
		if a.Reihe[i].Status != w {
			t.Errorf("#%d Status %q, erwartet %q", a.Reihe[i].Nummer, a.Reihe[i].Status, w)
		}
	}
	if b := a.Reihe[2].BlockiertDurch; len(b) != 1 || b[0] != 3 {
		t.Errorf("#2 blockiert_durch %v", b)
	}
	// Rollen ueber die Route (karte-rollen, #102): #4 traegt needs-info,
	// bleibt aber in_arbeit, weil sein Change laeuft; keiner hat einen grund.
	for _, e := range a.Reihe {
		if e.Grund != nil {
			t.Errorf("#%d grund %q, erwartet keinen", e.Nummer, *e.Grund)
		}
	}
	vier := a.Reihe[0].Change
	if vier == nil || vier.Ref != "change/vier" || vier.Erledigt != 1 || vier.Gesamt != 3 {
		t.Errorf("#4 Change %+v, erwartet vom Branch change/vier mit 1/3", vier)
	}
	if got := folge(a.OhneIssue); got != "los" {
		t.Errorf("ohne_issue %s, erwartet nur los (x ist auf main archiviert, sein Branch veraltet)", got)
	}
	if len(a.PRs) != 1 || a.PRs[0].Nummer != 9 {
		t.Errorf("prs %+v", a.PRs)
	}
	if got := folge(a.Zuletzt); got != "15,14,13,12,11" {
		t.Errorf("zuletzt %s", got)
	}
	if z := a.Zuletzt; len(z) > 0 {
		if z[0].Change == nil || len(z[0].Change.Belege) != 1 || !strings.HasPrefix(z[0].Change.Belege[0].Bild, "/api/blob?") ||
			!strings.Contains(z[0].Change.Belege[0].Bild, "sha=b-png") {
			t.Errorf("#15 ohne Bildbeleg mit /api/blob-URL: %+v", z[0].Change)
		}
	}
	if !strings.Contains(log.String(), "repo=acme/alpha") || !strings.Contains(log.String(), "pat=GITHUB_PAT_WORK") {
		t.Errorf("Log ohne Zeile zum Abruf von acme/alpha:\n%s", log)
	}

	// Ein zweiter Abruf holt keinen Blob erneut.
	holeJSON(t, h, "/api/arbeit?repo=acme/alpha&frisch=1", nil)
	for sha, n := range f.blobZaehl {
		if n > 1 {
			t.Errorf("Blob %s %d-mal geholt", sha, n)
		}
	}
}

func TestApiArbeitUnbekanntesRepo(t *testing.T) {
	f := neuesFakeGH(t)
	h, _, _ := repoHandler(t, f)
	rec := getRaw(t, h, "/api/arbeit?repo=nicht/da")
	if rec.Code != http.StatusNotFound {
		t.Errorf("Status %d, erwartet 404", rec.Code)
	}
	if n := f.anfragen("/repos/nicht/"); n != 0 {
		t.Errorf("%d Anfragen an GitHub fuer nicht/da", n)
	}
}

func TestApiDetail(t *testing.T) {
	f := neuesFakeGH(t)
	h, _, _ := repoHandler(t, f)

	var c struct {
		Proposal string `json:"proposal"`
		Tasks    string `json:"tasks"`
		Specs    []struct {
			Capability string `json:"capability"`
			Text       string `json:"text"`
		} `json:"specs"`
	}
	if rec := holeJSON(t, h, "/api/detail?repo=acme/alpha&art=change&kennung=drei", &c); rec.Code != http.StatusOK {
		t.Fatalf("Status %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(c.Proposal, "GitHub-Issue #3") || !strings.Contains(c.Tasks, "- [ ] eins") {
		t.Errorf("proposal %q tasks %q", c.Proposal, c.Tasks)
	}
	if len(c.Specs) != 1 || c.Specs[0].Capability != "kern" || !strings.Contains(c.Specs[0].Text, "ADDED") {
		t.Errorf("specs %+v", c.Specs)
	}

	var i struct {
		Titel string `json:"titel"`
		Text  string `json:"text"`
		URL   string `json:"url"`
	}
	if rec := holeJSON(t, h, "/api/detail?repo=acme/alpha&art=issue&nummer=4", &i); rec.Code != http.StatusOK {
		t.Fatalf("Status %d: %s", rec.Code, rec.Body.String())
	}
	if i.Titel != "vier" || i.Text != "Vier **bauen**" || i.URL == "" {
		t.Errorf("Issue-Detail %+v", i)
	}
}

func TestApiBlobNurBilder(t *testing.T) {
	f := neuesFakeGH(t)
	h, _, _ := repoHandler(t, f)

	rec := getRaw(t, h, "/api/blob?repo=acme/alpha&sha=b-png&name=0-x.png")
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/png" || !strings.HasPrefix(rec.Body.String(), "\x89PNG") {
		t.Errorf("png: Status %d, Typ %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if rec := getRaw(t, h, "/api/blob?repo=acme/alpha&sha=b-png&name=x.html"); rec.Code != http.StatusBadRequest {
		t.Errorf("html: Status %d, erwartet 400", rec.Code)
	}
	svg := getRaw(t, h, "/api/blob?repo=acme/alpha&sha=b-png&name=x.svg")
	if !strings.Contains(svg.Header().Get("Content-Security-Policy"), "sandbox") {
		t.Errorf("svg ohne CSP sandbox: %q", svg.Header().Get("Content-Security-Policy"))
	}
}

func TestEinklappRoute(t *testing.T) {
	f := neuesFakeGH(t)
	h, areaDatei, _ := repoHandler(t, f)
	if err := os.WriteFile(areaDatei, []byte(`{"version":1,"areas":["Arbeit"],"zuordnung":{},"notiz":"bleibt"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	basis := startOn(t, "127.0.0.1", h)

	lies := func() map[string]json.RawMessage {
		b, _ := os.ReadFile(areaDatei)
		var m map[string]json.RawMessage
		_ = json.Unmarshal(b, &m)
		return m
	}
	resp, rumpf := postJSON(t, basis, "/action/task/bereich/einklappen", `{"bereich":"Arbeit","eingeklappt":true}`, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Status %d: %s", resp.StatusCode, rumpf)
	}
	if m := lies(); !strings.Contains(string(m["eingeklappt"]), `"Arbeit"`) || string(m["notiz"]) != `"bleibt"` {
		t.Errorf("nach dem Einklappen: %v", m)
	}
	postJSON(t, basis, "/action/task/bereich/einklappen", `{"bereich":"Arbeit","eingeklappt":false}`, nil)
	if m := lies(); strings.Contains(string(m["eingeklappt"]), "Arbeit") {
		t.Errorf("nach dem Aufklappen noch eingeklappt: %s", m["eingeklappt"])
	}
	vorher, _ := os.ReadFile(areaDatei)
	resp, _ = postJSON(t, basis, "/action/task/bereich/einklappen", `{"bereich":"  ","eingeklappt":true}`, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("leerer Name: Status %d, erwartet 400", resp.StatusCode)
	}
	if nachher, _ := os.ReadFile(areaDatei); !bytes.Equal(vorher, nachher) {
		t.Error("abgelehnte Eingabe hat die Datei veraendert")
	}
}

type testQuellen struct {
	Reihe   []testEintrag `json:"reihe"`
	Quellen []struct {
		Quelle string `json:"quelle"`
		Grund  string `json:"grund"`
	} `json:"quellen"`
}

func (a testQuellen) grund(t *testing.T, quelle string) string {
	t.Helper()
	for _, q := range a.Quellen {
		if q.Quelle == quelle {
			return q.Grund
		}
	}
	t.Fatalf("Quelle %s fehlt: %+v", quelle, a.Quellen)
	return ""
}

// Die Blocker-Abfrage von #2 scheitert: #2 bleibt blockiert, und die Quelle
// abhaengigkeiten meldet den Grund (karte-blocker-streng, #101).
func TestApiArbeitBlockerAbfrageScheitert(t *testing.T) {
	f := neuesFakeGH(t)
	f.blockerFehler = true
	h, _, _ := repoHandler(t, f)
	var a testQuellen
	if rec := holeJSON(t, h, "/api/arbeit?repo=acme/alpha", &a); rec.Code != http.StatusOK {
		t.Fatalf("Status %d: %s", rec.Code, rec.Body.String())
	}
	gesehen := false
	for _, e := range a.Reihe {
		if e.Nummer == 2 {
			gesehen = true
			if e.Status != "blockiert" || len(e.BlockiertDurch) != 0 {
				t.Errorf("#2 %q %v, erwartet blockiert ohne bekannten Blocker", e.Status, e.BlockiertDurch)
			}
		}
	}
	if !gesehen {
		t.Fatalf("#2 fehlt in der Reihe")
	}
	if g := a.grund(t, "abhaengigkeiten"); g == "" {
		t.Errorf("Quelle abhaengigkeiten ohne Grund, obwohl die Abfrage scheiterte")
	}
}

func TestApiArbeitAbhaengigkeitenOhneFehler(t *testing.T) {
	f := neuesFakeGH(t)
	h, _, _ := repoHandler(t, f)
	var a testQuellen
	if rec := holeJSON(t, h, "/api/arbeit?repo=acme/alpha", &a); rec.Code != http.StatusOK {
		t.Fatalf("Status %d: %s", rec.Code, rec.Body.String())
	}
	if g := a.grund(t, "abhaengigkeiten"); g != "" {
		t.Errorf("Quelle abhaengigkeiten mit Grund %q", g)
	}
}
