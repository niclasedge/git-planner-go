package ghrest_test

// Drei Staende je Konfliktdatei und der Merge-Commit ueber die Git-Data-API
// (konflikte-loesen, #75). Nur gegen eine Fake-API.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

type klAufruf struct {
	Methode, Pfad, Query string
	Rumpf                map[string]any
}

type klFake struct {
	mu      sync.Mutex
	aufrufe []klAufruf
	// refSHA ist der Stand, den GET git/ref/heads/... meldet.
	refSHA string
	// patchStatus ist die Antwort auf PATCH git/refs (0 = 200).
	patchStatus int
}

func (f *klFake) schreibend() []klAufruf {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []klAufruf
	for _, a := range f.aufrufe {
		if a.Methode != http.MethodGet {
			out = append(out, a)
		}
	}
	return out
}

func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

func neuerKLFake(t *testing.T) *klFake {
	t.Helper()
	f := &klFake{refSHA: "sha21"}
	inhalte := map[string]string{
		"b.go@basis21":   "a\nb\nc\n",
		"b.go@sha21":     "a\nB\nc\n",
		"b.go@shamain":   "a\nb\nC\n",
		"weg.go@basis21": "x\n",
		"weg.go@shamain": "x\ny\n",
	}
	fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		a := klAufruf{Methode: r.Method, Pfad: r.URL.Path, Query: r.URL.RawQuery}
		if len(b) > 0 {
			_ = json.Unmarshal(b, &a.Rumpf)
		}
		f.mu.Lock()
		f.aufrufe = append(f.aufrufe, a)
		ref, patch := f.refSHA, f.patchStatus
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		p := r.URL.Path
		switch {
		case p == "/repos/acme/alpha/compare/main...sha21":
			fmt.Fprint(w, `{"ahead_by":2,"behind_by":4,"merge_base_commit":{"sha":"basis21"},"base_commit":{"sha":"shamain"},
			  "files":[{"filename":"b.go","status":"modified","sha":"blobB"},{"filename":"weg.go","status":"removed","sha":"blobW"},
			  {"filename":"neu.go","status":"added","sha":"blobN"},{"filename":"ziel.go","status":"renamed","previous_filename":"quelle.go","sha":"blobZ"}]}`)
		case p == "/repos/acme/alpha/compare/basis21...main":
			fmt.Fprint(w, `{"ahead_by":4,"behind_by":0,"merge_base_commit":{"sha":"basis21"},"base_commit":{"sha":"shamain"},
			  "files":[{"filename":"b.go","status":"modified"},{"filename":"weg.go","status":"modified"},{"filename":"c.go","status":"modified"}]}`)
		case strings.HasPrefix(p, "/repos/acme/alpha/contents/"):
			pfad := strings.TrimPrefix(p, "/repos/acme/alpha/contents/")
			s, ok := inhalte[pfad+"@"+r.URL.Query().Get("ref")]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				fmt.Fprint(w, `{"message":"Not Found"}`)
				return
			}
			fmt.Fprintf(w, `{"type":"file","encoding":"base64","content":%q,"sha":"x"}`, b64(s))
		case p == "/repos/acme/alpha/commits" && r.Method == http.MethodGet:
			sha := r.URL.Query().Get("sha")
			fmt.Fprintf(w, `[{"sha":"%s0123456789","commit":{"message":"Stand %s\n\nText","committer":{"date":"2026-09-2%dT10:00:00Z"}}}]`,
				sha, sha, len(sha)%10)
		case p == "/repos/acme/alpha/git/commits/shamain" && r.Method == http.MethodGet:
			fmt.Fprint(w, `{"sha":"shamain","tree":{"sha":"baummain"}}`)
		case p == "/repos/acme/alpha/git/trees/sha21" && r.Method == http.MethodGet:
			fmt.Fprint(w, `{"tree":[{"path":"b.go","mode":"100644","type":"blob","sha":"blobB"},{"path":"neu.go","mode":"100755","type":"blob","sha":"blobN"}]}`)
		case p == "/repos/acme/alpha/git/blobs" && r.Method == http.MethodPost:
			w.WriteHeader(http.StatusCreated)
			fmt.Fprintf(w, `{"sha":"blob-%d"}`, len(f.schreibend()))
		case p == "/repos/acme/alpha/git/trees" && r.Method == http.MethodPost:
			w.WriteHeader(http.StatusCreated)
			fmt.Fprint(w, `{"sha":"baumneu"}`)
		case p == "/repos/acme/alpha/git/commits" && r.Method == http.MethodPost:
			w.WriteHeader(http.StatusCreated)
			fmt.Fprint(w, `{"sha":"mergecommit"}`)
		case p == "/repos/acme/alpha/git/ref/heads/change/einundzwanzig" && r.Method == http.MethodGet:
			fmt.Fprintf(w, `{"ref":"refs/heads/change/einundzwanzig","object":{"sha":%q}}`, ref)
		case p == "/repos/acme/alpha/git/refs/heads/change/einundzwanzig" && r.Method == http.MethodPatch:
			if patch != 0 {
				w.WriteHeader(patch)
				fmt.Fprint(w, `{"message":"Update is not a fast forward"}`)
				return
			}
			fmt.Fprint(w, `{"object":{"sha":"mergecommit"}}`)
		default:
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"message":"Not Found"}`)
		}
	})
	return f
}

func klPR() ghrest.PR {
	return ghrest.PR{Nummer: 21, Head: "change/einundzwanzig", HeadRepo: "acme/alpha", HeadSHA: "sha21", Base: "main",
		Mergebar: wahr(false), MergeStatus: "dirty", Zustand: "open"}
}

func TestInhaltUeberContentsAPI(t *testing.T) {
	neuerKLFake(t)
	c := ghrest.Client{Token: "t"}
	s, err := c.Inhalt(context.Background(), "acme/alpha", "b.go", "sha21")
	if err != nil || s == nil || *s != "a\nB\nc\n" {
		t.Fatalf("Inhalt %v %v", s, err)
	}
	s, err = c.Inhalt(context.Background(), "acme/alpha", "weg.go", "sha21")
	if err != nil || s != nil {
		t.Errorf("fehlende Datei: %v %v, erwartet nil ohne Fehler", s, err)
	}
	if _, err := c.Inhalt(context.Background(), "acme/alpha", "../geheim", "sha21"); err == nil {
		t.Error("Pfad mit .. angenommen")
	}
}

func TestLetzterCommitJePfad(t *testing.T) {
	f := neuerKLFake(t)
	k, err := ghrest.Client{Token: "t"}.LetzterCommit(context.Background(), "acme/alpha", "web/src/App.svelte", "shamain")
	if err != nil {
		t.Fatal(err)
	}
	if k.SHA != "shamain0123456789" || k.Betreff != "Stand shamain" || k.Zeit != "2026-09-27T10:00:00Z" {
		t.Errorf("Commit %+v", k)
	}
	a := f.aufrufe[len(f.aufrufe)-1]
	if a.Pfad != "/repos/acme/alpha/commits" || !strings.Contains(a.Query, "path=web%2Fsrc%2FApp.svelte") || !strings.Contains(a.Query, "sha=shamain") || !strings.Contains(a.Query, "per_page=1") {
		t.Errorf("Aufruf %+v", a)
	}
}

func TestKonfliktLageUndStaende(t *testing.T) {
	neuerKLFake(t)
	c := ghrest.Client{Token: "t"}
	p := klPR()
	lage, err := c.KonfliktLageHolen(context.Background(), "acme/alpha", &p)
	if err != nil {
		t.Fatal(err)
	}
	if p.Abgleich != "konflikt" || lage.HeadSHA != "sha21" || lage.BaseSHA != "shamain" || lage.MergeBasis != "basis21" || lage.Branch != "change/einundzwanzig" {
		t.Errorf("Lage %+v, PR-Zustand %q", lage, p.Abgleich)
	}
	if !reflect.DeepEqual(lage.Dateien, []string{"b.go", "weg.go"}) {
		t.Errorf("Konfliktdateien %v", lage.Dateien)
	}
	if len(lage.PRDateien) != 4 || lage.PRDateien[3].Vorher != "quelle.go" || lage.PRDateien[1].Status != "removed" {
		t.Errorf("PR-Dateien %+v", lage.PRDateien)
	}
	st, err := c.DreiStaendeHolen(context.Background(), "acme/alpha", lage, "b.go")
	if err != nil {
		t.Fatal(err)
	}
	if st.Basis == nil || *st.Basis != "a\nb\nc\n" || st.Meine == nil || *st.Meine != "a\nB\nc\n" || st.Extern == nil || *st.Extern != "a\nb\nC\n" {
		t.Errorf("Staende %+v", st)
	}
	if st.BasisCommit.Betreff != "Stand basis21" || st.MeineCommit.Betreff != "Stand sha21" || st.ExternCommit.Betreff != "Stand shamain" {
		t.Errorf("Commits %+v / %+v / %+v", st.BasisCommit, st.MeineCommit, st.ExternCommit)
	}
	weg, err := c.DreiStaendeHolen(context.Background(), "acme/alpha", lage, "weg.go")
	if err != nil || weg.Meine != nil || weg.Basis == nil || weg.Extern == nil {
		t.Errorf("weg.go: %+v %v, erwartet meine fehlt", weg, err)
	}
}

func TestMergeCommitSchreiben(t *testing.T) {
	f := neuerKLFake(t)
	c := ghrest.Client{Token: "t"}
	p := klPR()
	lage, err := c.KonfliktLageHolen(context.Background(), "acme/alpha", &p)
	if err != nil {
		t.Fatal(err)
	}
	geloest := "a\nB\nC\n"
	sha, err := c.MergeCommitSchreiben(context.Background(), "acme/alpha", lage, map[string]*string{"b.go": &geloest, "weg.go": nil}, "Merge main in change/einundzwanzig")
	if err != nil {
		t.Fatal(err)
	}
	if sha != "mergecommit" {
		t.Errorf("Commit %q", sha)
	}
	s := f.schreibend()
	var pfade []string
	for _, a := range s {
		pfade = append(pfade, a.Methode+" "+a.Pfad)
	}
	soll := []string{"POST /repos/acme/alpha/git/blobs", "POST /repos/acme/alpha/git/trees", "POST /repos/acme/alpha/git/commits", "PATCH /repos/acme/alpha/git/refs/heads/change/einundzwanzig"}
	if !reflect.DeepEqual(pfade, soll) {
		t.Fatalf("schreibende Aufrufe %v, erwartet %v", pfade, soll)
	}
	blob := s[0].Rumpf
	if blob["encoding"] != "base64" || blob["content"] != b64(geloest) {
		t.Errorf("Blob %+v", blob)
	}
	baum := s[1].Rumpf
	if baum["base_tree"] != "baummain" {
		t.Errorf("base_tree %v, erwartet baummain", baum["base_tree"])
	}
	eintraege := map[string]map[string]any{}
	for _, e := range baum["tree"].([]any) {
		m := e.(map[string]any)
		eintraege[m["path"].(string)] = m
	}
	namen := []string{}
	for n := range eintraege {
		namen = append(namen, n)
	}
	sort.Strings(namen)
	if !reflect.DeepEqual(namen, []string{"b.go", "neu.go", "quelle.go", "weg.go", "ziel.go"}) {
		t.Fatalf("Baum-Eintraege %v", namen)
	}
	if e := eintraege["b.go"]; e["sha"] != "blob-1" || e["mode"] != "100644" || e["type"] != "blob" {
		t.Errorf("b.go %+v, erwartet der neue Blob", e)
	}
	if e := eintraege["neu.go"]; e["sha"] != "blobN" || e["mode"] != "100755" {
		t.Errorf("neu.go %+v, erwartet Blob des PRs mit Modus aus dem Kopf-Baum", e)
	}
	if e, ok := eintraege["weg.go"]; !ok || e["sha"] != nil {
		t.Errorf("weg.go %+v, erwartet sha null (geloescht)", e)
	}
	if e := eintraege["quelle.go"]; e["sha"] != nil {
		t.Errorf("quelle.go %+v, erwartet geloescht (umbenannt)", e)
	}
	if e := eintraege["ziel.go"]; e["sha"] != "blobZ" {
		t.Errorf("ziel.go %+v", e)
	}
	commit := s[2].Rumpf
	if commit["tree"] != "baumneu" || !reflect.DeepEqual(commit["parents"], []any{"sha21", "shamain"}) || commit["message"] != "Merge main in change/einundzwanzig" {
		t.Errorf("Commit %+v", commit)
	}
	ref := s[3].Rumpf
	if ref["sha"] != "mergecommit" || ref["force"] != false {
		t.Errorf("PATCH %+v, erwartet sha mergecommit und force false", ref)
	}
}

func TestMergeCommitVeralteterKopf(t *testing.T) {
	f := neuerKLFake(t)
	c := ghrest.Client{Token: "t"}
	p := klPR()
	lage, err := c.KonfliktLageHolen(context.Background(), "acme/alpha", &p)
	if err != nil {
		t.Fatal(err)
	}
	inhalt := "x\n"
	f.mu.Lock()
	f.refSHA = "neuer"
	f.mu.Unlock()
	_, err = c.MergeCommitSchreiben(context.Background(), "acme/alpha", lage, map[string]*string{"b.go": &inhalt, "weg.go": nil}, "m")
	if !errors.Is(err, ghrest.ErrKopfVeraltet) {
		t.Errorf("Fehler %v, erwartet ErrKopfVeraltet", err)
	}
	for _, a := range f.schreibend() {
		if a.Methode == http.MethodPatch {
			t.Errorf("trotz veraltetem Kopf vorgeschoben: %+v", a)
		}
	}

	f.mu.Lock()
	f.refSHA, f.patchStatus = "sha21", http.StatusUnprocessableEntity
	f.mu.Unlock()
	_, err = c.MergeCommitSchreiben(context.Background(), "acme/alpha", lage, map[string]*string{"b.go": &inhalt, "weg.go": nil}, "m")
	if !errors.Is(err, ghrest.ErrKopfVeraltet) {
		t.Errorf("abgelehntes Vorschieben: %v, erwartet ErrKopfVeraltet", err)
	}
}
