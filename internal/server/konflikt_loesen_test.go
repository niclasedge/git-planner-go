package server_test

// GET /api/pr-konflikt und POST /action/pr/konflikt-loesen gegen eine
// Fake-GitHub-API (konflikte-loesen, #75). Nie gegen echtes GitHub.

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/niclasedge/git-planner-go/internal/assets"
	"github.com/niclasedge/git-planner-go/internal/render"
	"github.com/niclasedge/git-planner-go/internal/server"
)

type loesenGH struct {
	prGH
	lock   sync.Mutex
	refSHA string
}

func neuesLoesenGH(t *testing.T) *loesenGH {
	t.Helper()
	f := &loesenGH{refSHA: "sha21"}
	inhalte := map[string]string{
		"b.go@basis21": "a\nb\nc\n", "b.go@sha21": "a\nB\nc\n", "b.go@shamain": "a\nb\nC\n",
		"c.go@basis21": "a\nb\nc\n", "c.go@sha21": "a\nb1\nc\n", "c.go@shamain": "a\nb2\nc\n",
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
		f.lock.Lock()
		ref := f.refSHA
		f.lock.Unlock()
		w.Header().Set("Content-Type", "application/json")
		p := r.URL.Path
		switch {
		case p == "/user/repos":
			fmt.Fprint(w, `[{"full_name":"acme/alpha","name":"alpha","owner":{"login":"acme"},"default_branch":"main"}]`)
		case p == "/repos/acme/alpha/pulls/21" && r.Method == http.MethodGet:
			fmt.Fprint(w, konfliktPR(21, "change/einundzwanzig", "false", "dirty"))
		case p == "/repos/acme/alpha/pulls/22" && r.Method == http.MethodGet:
			fmt.Fprint(w, konfliktPR(22, "change/zweiundzwanzig", "true", "clean"))
		case p == "/repos/acme/alpha/compare/main...sha21":
			fmt.Fprint(w, `{"ahead_by":2,"behind_by":4,"merge_base_commit":{"sha":"basis21"},"base_commit":{"sha":"shamain"},
			  "files":[{"filename":"b.go","status":"modified","sha":"kb"},{"filename":"c.go","status":"modified","sha":"kc"},{"filename":"neu.go","status":"added","sha":"blobneu"}]}`)
		case p == "/repos/acme/alpha/compare/basis21...main":
			fmt.Fprint(w, `{"ahead_by":4,"behind_by":0,"merge_base_commit":{"sha":"basis21"},"base_commit":{"sha":"shamain"},
			  "files":[{"filename":"b.go","status":"modified"},{"filename":"c.go","status":"modified"},{"filename":"d.go","status":"modified"}]}`)
		case p == "/repos/acme/alpha/compare/main...sha22":
			fmt.Fprint(w, `{"ahead_by":1,"behind_by":0,"merge_base_commit":{"sha":"shamain"},"base_commit":{"sha":"shamain"},"files":[{"filename":"y.go"}]}`)
		case strings.HasPrefix(p, "/repos/acme/alpha/contents/"):
			s, ok := inhalte[strings.TrimPrefix(p, "/repos/acme/alpha/contents/")+"@"+r.URL.Query().Get("ref")]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				fmt.Fprint(w, `{"message":"Not Found"}`)
				return
			}
			fmt.Fprintf(w, `{"type":"file","encoding":"base64","content":%q}`, base64.StdEncoding.EncodeToString([]byte(s)))
		case p == "/repos/acme/alpha/commits" && r.Method == http.MethodGet:
			sha := r.URL.Query().Get("sha")
			fmt.Fprintf(w, `[{"sha":"%s9999999","commit":{"message":"Aenderung auf %s","committer":{"date":"2026-09-25T0%d:00:00Z"}}}]`, sha, sha, len(sha)%10)
		case p == "/repos/acme/alpha/git/commits/shamain":
			fmt.Fprint(w, `{"sha":"shamain","tree":{"sha":"baummain"}}`)
		case p == "/repos/acme/alpha/git/trees/sha21" && r.Method == http.MethodGet:
			fmt.Fprint(w, `{"tree":[]}`)
		case p == "/repos/acme/alpha/git/blobs" && r.Method == http.MethodPost:
			w.WriteHeader(http.StatusCreated)
			fmt.Fprintf(w, `{"sha":"blob-%s"}`, strings.TrimSpace(strings.SplitN(decode(a.Rumpf["content"]), "\n", 3)[1]))
		case p == "/repos/acme/alpha/git/trees" && r.Method == http.MethodPost:
			w.WriteHeader(http.StatusCreated)
			fmt.Fprint(w, `{"sha":"baumneu"}`)
		case p == "/repos/acme/alpha/git/commits" && r.Method == http.MethodPost:
			w.WriteHeader(http.StatusCreated)
			fmt.Fprint(w, `{"sha":"mergecommit"}`)
		case p == "/repos/acme/alpha/git/ref/heads/change/einundzwanzig":
			fmt.Fprintf(w, `{"object":{"sha":%q}}`, ref)
		case p == "/repos/acme/alpha/git/refs/heads/change/einundzwanzig" && r.Method == http.MethodPatch:
			fmt.Fprint(w, `{"object":{"sha":"mergecommit"}}`)
		case p == "/repos/acme/alpha/issues" || p == "/repos/acme/alpha/pulls" || p == "/repos/acme/alpha/branches":
			fmt.Fprint(w, `[]`)
		default:
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"message":"Not Found"}`)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func decode(v any) string {
	s, _ := v.(string)
	b, _ := base64.StdEncoding.DecodeString(s)
	return string(b)
}

type klSeite struct {
	SHA     string `json:"sha"`
	KurzSHA string `json:"kurz_sha"`
	Zeit    string `json:"zeit"`
	Betreff string `json:"betreff"`
}

type klDatei struct {
	Pfad   string             `json:"pfad"`
	Seiten map[string]klSeite `json:"seiten"`
	Meine  struct {
		Gegen string `json:"gegen"`
		Diff  []struct {
			Art  string `json:"art"`
			Text string `json:"text"`
		} `json:"diff"`
	} `json:"meine"`
	Mergen struct {
		Konfliktfrei bool `json:"konfliktfrei"`
		Hunks        int  `json:"hunks"`
		Abschnitte   []struct {
			Art    string   `json:"art"`
			Hunk   int      `json:"hunk"`
			Meine  []string `json:"meine"`
			Extern []string `json:"extern"`
		} `json:"abschnitte"`
	} `json:"mergen"`
}

func TestPRKonfliktVorschauRoute(t *testing.T) {
	f := neuesLoesenGH(t)
	basis := prServer(t, &f.prGH)
	var v struct {
		Nummer  int       `json:"nummer"`
		Zustand string    `json:"zustand"`
		HeadSHA string    `json:"head_sha"`
		BaseSHA string    `json:"base_sha"`
		Dateien []klDatei `json:"dateien"`
	}
	if st := holeVon(t, basis+"/api/pr-konflikt?repo=acme/alpha&nummer=21", &v); st != http.StatusOK {
		t.Fatalf("Status %d", st)
	}
	if v.Nummer != 21 || v.Zustand != "konflikt" || v.HeadSHA != "sha21" || v.BaseSHA != "shamain" || len(v.Dateien) != 2 {
		t.Fatalf("Vorschau %+v", v)
	}
	b, c := v.Dateien[0], v.Dateien[1]
	if b.Pfad != "b.go" || c.Pfad != "c.go" {
		t.Fatalf("Dateien %s, %s", b.Pfad, c.Pfad)
	}
	for _, seite := range []string{"basis", "meine", "extern"} {
		s := b.Seiten[seite]
		if len(s.KurzSHA) != 7 || s.Zeit == "" || !strings.HasPrefix(s.Betreff, "Aenderung auf ") {
			t.Errorf("b.go Seite %s: %+v", seite, s)
		}
	}
	if b.Seiten["meine"].Betreff != "Aenderung auf sha21" || b.Seiten["extern"].Betreff != "Aenderung auf shamain" || b.Seiten["basis"].Betreff != "Aenderung auf basis21" {
		t.Errorf("Seiten %+v", b.Seiten)
	}
	if !b.Mergen.Konfliktfrei || b.Meine.Gegen != "extern" || len(b.Meine.Diff) == 0 {
		t.Errorf("b.go Vorschau %+v", b)
	}
	if c.Mergen.Konfliktfrei || c.Mergen.Hunks != 1 {
		t.Errorf("c.go mergen %+v, erwartet einen Hunk", c.Mergen)
	}
	if s := f.schreibend(); len(s) != 0 {
		t.Errorf("Vorschau hat geschrieben: %+v", s)
	}
	var fehler map[string]any
	if st := holeVon(t, basis+"/api/pr-konflikt?repo=acme/alpha", &fehler); st != http.StatusBadRequest {
		t.Errorf("ohne nummer: Status %d", st)
	}
}

const loesenRumpf = `{"repo":"acme/alpha","nummer":21,"expected_head_sha":"sha21","dateien":[{"pfad":"b.go","wahl":"meine"},{"pfad":"c.go","wahl":"mergen","hunks":["extern"]}]}`

func TestKonfliktLoesenRoute(t *testing.T) {
	f := neuesLoesenGH(t)
	basis := prServer(t, &f.prGH)
	resp, rumpf := postJSON(t, basis, "/action/pr/konflikt-loesen", loesenRumpf, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Status %d (%s)", resp.StatusCode, rumpf)
	}
	var antwort struct {
		OK     bool   `json:"ok"`
		Commit string `json:"commit"`
	}
	_ = json.Unmarshal(rumpf, &antwort)
	if !antwort.OK || antwort.Commit != "mergecommit" {
		t.Errorf("Antwort %s", rumpf)
	}
	s := f.schreibend()
	var art []string
	var baum, commit, ref map[string]any
	blobs := map[string]string{}
	for _, a := range s {
		art = append(art, a.Methode+" "+a.Pfad)
		switch a.Pfad {
		case "/repos/acme/alpha/git/blobs":
			blobs[decode(a.Rumpf["content"])] = a.Rumpf["encoding"].(string)
		case "/repos/acme/alpha/git/trees":
			baum = a.Rumpf
		case "/repos/acme/alpha/git/commits":
			commit = a.Rumpf
		default:
			ref = a.Rumpf
		}
	}
	soll := []string{"POST /repos/acme/alpha/git/blobs", "POST /repos/acme/alpha/git/blobs", "POST /repos/acme/alpha/git/trees",
		"POST /repos/acme/alpha/git/commits", "PATCH /repos/acme/alpha/git/refs/heads/change/einundzwanzig"}
	if !reflect.DeepEqual(art, soll) {
		t.Fatalf("schreibende Aufrufe %v, erwartet %v", art, soll)
	}
	if _, ok := blobs["a\nB\nc\n"]; !ok {
		t.Errorf("kein Blob mit meiner Fassung von b.go: %v", blobs)
	}
	if _, ok := blobs["a\nb2\nc\n"]; !ok {
		t.Errorf("kein Blob mit dem gemergten c.go (Hunk extern): %v", blobs)
	}
	if baum["base_tree"] != "baummain" {
		t.Errorf("base_tree %v", baum["base_tree"])
	}
	je := map[string]any{}
	for _, e := range baum["tree"].([]any) {
		m := e.(map[string]any)
		je[m["path"].(string)] = m["sha"]
	}
	namen := []string{}
	for n := range je {
		namen = append(namen, n)
	}
	sort.Strings(namen)
	if !reflect.DeepEqual(namen, []string{"b.go", "c.go", "neu.go"}) || je["neu.go"] != "blobneu" || je["b.go"] != "blob-B" || je["c.go"] != "blob-b2" {
		t.Errorf("Baum %v", je)
	}
	if !reflect.DeepEqual(commit["parents"], []any{"sha21", "shamain"}) || commit["tree"] != "baumneu" {
		t.Errorf("Commit %+v", commit)
	}
	if ref["sha"] != "mergecommit" || ref["force"] != false {
		t.Errorf("PATCH %+v", ref)
	}
}

func TestKonfliktLoesenVeraltet(t *testing.T) {
	f := neuesLoesenGH(t)
	basis := prServer(t, &f.prGH)
	alt := strings.Replace(loesenRumpf, `"expected_head_sha":"sha21"`, `"expected_head_sha":"alt"`, 1)
	if resp, rumpf := postJSON(t, basis, "/action/pr/konflikt-loesen", alt, nil); resp.StatusCode != http.StatusConflict {
		t.Errorf("expected_head_sha alt: Status %d, erwartet 409 (%s)", resp.StatusCode, rumpf)
	}
	if s := f.schreibend(); len(s) != 0 {
		t.Errorf("bei veraltetem Kopf geschrieben: %+v", s)
	}
	f.lock.Lock()
	f.refSHA = "inzwischen"
	f.lock.Unlock()
	if resp, rumpf := postJSON(t, basis, "/action/pr/konflikt-loesen", loesenRumpf, nil); resp.StatusCode != http.StatusConflict {
		t.Errorf("Branch inzwischen weiter: Status %d, erwartet 409 (%s)", resp.StatusCode, rumpf)
	}
	for _, a := range f.schreibend() {
		if a.Methode == http.MethodPatch {
			t.Errorf("trotz veraltetem Kopf vorgeschoben: %+v", a)
		}
	}
}

func TestKonfliktLoesenUnvollstaendig(t *testing.T) {
	f := neuesLoesenGH(t)
	basis := prServer(t, &f.prGH)
	for name, rumpf := range map[string]string{
		"nur b.go":         `{"repo":"acme/alpha","nummer":21,"expected_head_sha":"sha21","dateien":[{"pfad":"b.go","wahl":"meine"}]}`,
		"mergen ohne hunk": `{"repo":"acme/alpha","nummer":21,"expected_head_sha":"sha21","dateien":[{"pfad":"b.go","wahl":"meine"},{"pfad":"c.go","wahl":"mergen"}]}`,
		"fremde Datei":     `{"repo":"acme/alpha","nummer":21,"expected_head_sha":"sha21","dateien":[{"pfad":"b.go","wahl":"meine"},{"pfad":"c.go","wahl":"extern"},{"pfad":"d.go","wahl":"meine"}]}`,
		"unbekannte wahl":  `{"repo":"acme/alpha","nummer":21,"expected_head_sha":"sha21","dateien":[{"pfad":"b.go","wahl":"beide"},{"pfad":"c.go","wahl":"extern"}]}`,
		"ohne sha":         `{"repo":"acme/alpha","nummer":21,"dateien":[{"pfad":"b.go","wahl":"meine"},{"pfad":"c.go","wahl":"extern"}]}`,
		"kein Konflikt":    `{"repo":"acme/alpha","nummer":22,"expected_head_sha":"sha22","dateien":[]}`,
	} {
		if resp, antwort := postJSON(t, basis, "/action/pr/konflikt-loesen", rumpf, nil); resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: Status %d, erwartet 400 (%s)", name, resp.StatusCode, antwort)
		}
	}
	if s := f.schreibend(); len(s) != 0 {
		t.Errorf("abgelehnte Anfragen haben geschrieben: %+v", s)
	}
}

func TestKonfliktLoesenSchutz(t *testing.T) {
	f := neuesLoesenGH(t)
	basis := prServer(t, &f.prGH)
	if resp, _ := postJSON(t, basis, "/action/pr/konflikt-loesen", loesenRumpf, map[string]string{"Origin": "https://boese.example"}); resp.StatusCode != http.StatusForbidden {
		t.Errorf("fremde Origin: Status %d, erwartet 403", resp.StatusCode)
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
	if resp, rumpf := postJSON(t, netz, "/action/pr/konflikt-loesen", loesenRumpf, nil); resp.StatusCode != http.StatusForbidden {
		t.Errorf("Netz ohne Freigabe: Status %d, erwartet 403 (%s)", resp.StatusCode, rumpf)
	}
	if s := f.schreibend(); len(s) != 0 {
		t.Errorf("geschrieben trotz Schutz: %+v", s)
	}
}
