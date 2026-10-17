package e2e_test

// Eine kleine GitHub-API mit Zustand fuer den Funktionstest (sync-status,
// #38): Issues mit Kommentaren, Labels, Zustaendigen, Blockern und
// Sub-Issues, PRs, Branches, ein Git-Baum mit einem openspec-Change samt
// Bildbeleg. Schreibende Aufrufe aendern den Zustand, GET-Antworten tragen
// einen ETag und beantworten If-None-Match mit 304.

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	tokWork   = "tok-e2e-work-secret"
	tokPrivat = "tok-e2e-privat-geheim"
)

type fIssue struct {
	Nummer      int
	Titel       string
	Text        string
	Zu          bool
	Erstellt    time.Time
	Geschlossen time.Time
	Labels      []string
	Zustaendig  []string
	Kommentare  []string
	Blocker     []int
	Subs        []int
	Parent      int
}

type fPR struct {
	Nummer  int
	Titel   string
	Draft   bool
	Head    string
	Gemergt bool
	Zu      bool
}

type fakeGitHub struct {
	mu           sync.Mutex
	issues       map[int]*fIssue
	prs          map[int]*fPR
	branches     map[string]string // Name -> sha
	blobs        map[string]string // sha -> Inhalt
	baum         []map[string]any
	geloescht    []string
	freigegeben  []int
	unveraendert int
	aufrufe      map[string]int
	srv          *httptest.Server
}

func vor(tage int) time.Time { return time.Now().UTC().Add(-time.Duration(tage) * 24 * time.Hour) }

func neueFakeGitHub() *fakeGitHub {
	f := &fakeGitHub{issues: map[int]*fIssue{}, prs: map[int]*fPR{}, aufrufe: map[string]int{},
		branches: map[string]string{"main": "s-main", "change/zwanzig": "s-20", "change/einundzwanzig": "s-21"},
		blobs:    map[string]string{}}
	neu := func(i fIssue) { k := i; f.issues[i.Nummer] = &k }
	neu(fIssue{Nummer: 1, Titel: "Karte bauen", Erstellt: vor(5), Subs: []int{5, 6}})
	neu(fIssue{Nummer: 2, Titel: "Zwei", Erstellt: vor(4), Blocker: []int{3}})
	neu(fIssue{Nummer: 3, Titel: "drei: bauen", Erstellt: vor(4)})
	neu(fIssue{Nummer: 4, Titel: "Vier", Text: "Vier **bauen**", Erstellt: vor(3), Labels: []string{"bug"}, Kommentare: []string{"ana|Wann?"}})
	neu(fIssue{Nummer: 5, Titel: "fuenf", Erstellt: vor(5), Zu: true, Geschlossen: vor(4), Parent: 1})
	neu(fIssue{Nummer: 6, Titel: "sechs", Erstellt: vor(4), Parent: 1, Blocker: []int{9}})
	neu(fIssue{Nummer: 8, Titel: "Acht", Erstellt: vor(9), Zu: true, Geschlossen: vor(2), Subs: []int{9}})
	neu(fIssue{Nummer: 9, Titel: "neun", Erstellt: vor(9), Zu: true, Geschlossen: vor(6), Parent: 8})
	f.prs[20] = &fPR{Nummer: 20, Titel: "Zwanzig", Draft: true, Head: "change/zwanzig"}
	f.prs[21] = &fPR{Nummer: 21, Titel: "Fertig einundzwanzig", Head: "change/einundzwanzig"}

	datei := func(pfad, inhalt string) {
		h := sha256.Sum256([]byte(inhalt))
		sha := hex.EncodeToString(h[:8])
		f.blobs[sha] = inhalt
		f.baum = append(f.baum, map[string]any{"path": pfad, "type": "blob", "sha": sha, "size": len(inhalt)})
	}
	datei("openspec/changes/vier/proposal.md", "# vier\n\nGeht zurueck auf GitHub-Issue #4.\n\nDer Vorschlag fuer Vier.\n")
	datei("openspec/changes/vier/tasks.md", "- [x] erste Aufgabe\n- [ ] zweite Aufgabe\n")
	datei("openspec/changes/vier/gate1.md", "bestanden\n")
	datei("openspec/changes/vier/belege/belege.jsonl", `{"zeit":"z","stufe":"nachher","art":"bild","pfad":"belege/assets/0-v.png"}`+"\n")
	datei("openspec/changes/vier/belege/assets/0-v.png", pngBild)
	datei("openspec/changes/archive/2026-09-10-acht/proposal.md", "Geht zurueck auf GitHub-Issue #8.\n")
	datei("openspec/changes/archive/2026-09-10-acht/tasks.md", "- [x] fertig\n")

	f.srv = httptest.NewServer(http.HandlerFunc(f.bedienen))
	return f
}

// Ein 1x1-PNG.
var pngBild = string([]byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89\x00\x00\x00\rIDATx\x9cc\xf8\xff\xff?\x00\x05\xfe\x02\xfe\xa7\x35\x81\x84\x00\x00\x00\x00IEND\xaeB`\x82"))

func (f *fakeGitHub) issueJSON(i *fIssue) map[string]any {
	zustand := "open"
	var closed any
	if i.Zu {
		zustand = "closed"
		closed = i.Geschlossen.Format(time.RFC3339)
	}
	labels := []map[string]any{}
	for _, l := range i.Labels {
		labels = append(labels, map[string]any{"name": l, "color": "d73a4a"})
	}
	leute := []map[string]any{}
	for _, z := range i.Zustaendig {
		leute = append(leute, map[string]any{"login": z})
	}
	erledigt := 0
	for _, s := range i.Subs {
		if f.issues[s] != nil && f.issues[s].Zu {
			erledigt++
		}
	}
	// blocked_by zaehlt wie bei GitHub nur die offenen Blocker,
	// total_blocked_by alle (karte-blocker-streng, #101).
	offeneBlocker := 0
	for _, b := range i.Blocker {
		if f.issues[b] == nil || !f.issues[b].Zu {
			offeneBlocker++
		}
	}
	m := map[string]any{"number": i.Nummer, "id": 1000 + i.Nummer, "title": i.Titel, "body": i.Text, "state": zustand,
		"html_url": fmt.Sprintf("https://github.com/acme/alpha/issues/%d", i.Nummer), "user": map[string]any{"login": "ana"},
		"labels": labels, "assignees": leute, "comments": len(i.Kommentare),
		"created_at": i.Erstellt.Format(time.RFC3339), "updated_at": i.Erstellt.Format(time.RFC3339), "closed_at": closed,
		"sub_issues_summary":         map[string]any{"total": len(i.Subs), "completed": erledigt},
		"issue_dependencies_summary": map[string]any{"blocked_by": offeneBlocker, "total_blocked_by": len(i.Blocker)}}
	if i.Parent != 0 {
		m["parent_issue_url"] = fmt.Sprintf("%s/repos/acme/alpha/issues/%d", f.srv.URL, i.Parent)
	}
	return m
}

func (f *fakeGitHub) prJSON(p *fPR) map[string]any {
	zustand := "open"
	if p.Zu {
		zustand = "closed"
	}
	return map[string]any{"number": p.Nummer, "title": p.Titel, "html_url": fmt.Sprintf("u%d", p.Nummer), "draft": p.Draft,
		"node_id": fmt.Sprintf("PR_%d", p.Nummer), "state": zustand, "merged": p.Gemergt, "mergeable": true, "mergeable_state": "clean",
		"user": map[string]any{"login": "cy"}, "labels": []any{},
		"head": map[string]any{"ref": p.Head, "sha": "sha" + strconv.Itoa(p.Nummer), "repo": map[string]any{"full_name": "acme/alpha"}},
		"base": map[string]any{"ref": "main"}}
}

func (f *fakeGitHub) sortiert() []int {
	var n []int
	for k := range f.issues {
		n = append(n, k)
	}
	sort.Ints(n)
	return n
}

// antworten schreibt JSON mit ETag und beantwortet If-None-Match mit 304.
func (f *fakeGitHub) antworten(w http.ResponseWriter, r *http.Request, status int, v any) {
	b, _ := json.Marshal(v)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-RateLimit-Remaining", "4999")
	w.Header().Set("X-RateLimit-Limit", "5000")
	if r.Method == http.MethodGet && status == http.StatusOK {
		h := sha256.Sum256(b)
		etag := `"` + hex.EncodeToString(h[:6]) + `"`
		w.Header().Set("ETag", etag)
		if r.Header.Get("If-None-Match") == etag {
			f.unveraendert++
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}
	w.WriteHeader(status)
	_, _ = w.Write(b)
}

func (f *fakeGitHub) bedienen(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	rumpfRoh, _ := io.ReadAll(r.Body)
	var rumpf map[string]any
	_ = json.Unmarshal(rumpfRoh, &rumpf)
	p := r.URL.Path
	f.aufrufe[r.Method+" "+p]++
	auth := r.Header.Get("Authorization")
	if auth != "Bearer "+tokWork && auth != "Bearer "+tokPrivat {
		f.antworten(w, r, http.StatusUnauthorized, map[string]any{"message": "Bad credentials"})
		return
	}
	nichtDa := func() { f.antworten(w, r, http.StatusNotFound, map[string]any{"message": "Not Found"}) }

	if p == "/user/repos" {
		if auth == "Bearer "+tokPrivat {
			f.antworten(w, r, 200, []map[string]any{{"full_name": "ich/gamma", "name": "gamma", "owner": map[string]any{"login": "ich"}}})
			return
		}
		f.antworten(w, r, 200, []map[string]any{
			{"full_name": "acme/alpha", "name": "alpha", "owner": map[string]any{"login": "acme"}, "default_branch": "main",
				"description": "Alpha-Dienst", "open_issues_count": 6},
			{"full_name": "acme/beta", "name": "beta", "owner": map[string]any{"login": "acme"}, "default_branch": "main"}})
		return
	}
	if p == "/graphql" {
		id := fmt.Sprint(rumpf["variables"].(map[string]any)["id"])
		n, _ := strconv.Atoi(strings.TrimPrefix(id, "PR_"))
		if pr := f.prs[n]; pr != nil {
			pr.Draft = false
			f.freigegeben = append(f.freigegeben, n)
		}
		f.antworten(w, r, 200, map[string]any{"data": map[string]any{}})
		return
	}
	if !strings.HasPrefix(p, "/repos/acme/alpha/") {
		// beta und gamma: leer, aber gueltig.
		if strings.HasSuffix(p, "/issues") || strings.HasSuffix(p, "/branches") || strings.HasSuffix(p, "/pulls") {
			f.antworten(w, r, 200, []any{})
			return
		}
		nichtDa()
		return
	}
	rest := strings.TrimPrefix(p, "/repos/acme/alpha/")
	teile := strings.Split(rest, "/")

	switch {
	case rest == "issues" && r.Method == http.MethodGet:
		zu := r.URL.Query().Get("state") == "closed"
		out := []map[string]any{}
		for _, n := range f.sortiert() {
			if i := f.issues[n]; i.Zu == zu {
				out = append(out, f.issueJSON(i))
			}
		}
		if !zu {
			for _, n := range []int{20, 21} {
				if pr := f.prs[n]; !pr.Zu {
					out = append(out, map[string]any{"number": n, "title": pr.Titel, "state": "open", "pull_request": map[string]any{}})
				}
			}
		}
		f.antworten(w, r, 200, out)
	case rest == "issues" && r.Method == http.MethodPost:
		n := 100 + len(f.issues)
		i := &fIssue{Nummer: n, Titel: fmt.Sprint(rumpf["title"]), Text: fmt.Sprint(rumpf["body"]), Erstellt: time.Now().UTC()}
		f.issues[n] = i
		f.antworten(w, r, http.StatusCreated, f.issueJSON(i))
	case len(teile) >= 2 && teile[0] == "issues":
		n, _ := strconv.Atoi(teile[1])
		i := f.issues[n]
		if i == nil {
			nichtDa()
			return
		}
		switch {
		case len(teile) == 2 && r.Method == http.MethodGet:
			f.antworten(w, r, 200, f.issueJSON(i))
		case len(teile) == 2 && r.Method == http.MethodPatch:
			if v, ok := rumpf["state"]; ok {
				i.Zu = v == "closed"
				if i.Zu {
					i.Geschlossen = time.Now().UTC()
				}
			}
			if v, ok := rumpf["labels"].([]any); ok {
				i.Labels = nil
				for _, l := range v {
					i.Labels = append(i.Labels, fmt.Sprint(l))
				}
			}
			if v, ok := rumpf["title"]; ok {
				i.Titel = fmt.Sprint(v)
			}
			if v, ok := rumpf["body"]; ok {
				i.Text = fmt.Sprint(v)
			}
			f.antworten(w, r, 200, f.issueJSON(i))
		case len(teile) == 3 && teile[2] == "comments" && r.Method == http.MethodGet:
			out := []map[string]any{}
			for _, k := range i.Kommentare {
				a, t, _ := strings.Cut(k, "|")
				out = append(out, map[string]any{"user": map[string]any{"login": a}, "body": t, "created_at": i.Erstellt.Format(time.RFC3339)})
			}
			f.antworten(w, r, 200, out)
		case len(teile) == 3 && teile[2] == "comments" && r.Method == http.MethodPost:
			i.Kommentare = append(i.Kommentare, "ich|"+fmt.Sprint(rumpf["body"]))
			f.antworten(w, r, http.StatusCreated, map[string]any{"user": map[string]any{"login": "ich"}, "body": rumpf["body"]})
		case len(teile) == 3 && teile[2] == "sub_issues":
			out := []map[string]any{}
			for _, s := range i.Subs {
				out = append(out, f.issueJSON(f.issues[s]))
			}
			f.antworten(w, r, 200, out)
		case len(teile) >= 4 && teile[2] == "dependencies" && r.Method == http.MethodGet:
			out := []map[string]any{}
			for _, b := range i.Blocker {
				zustand := "open"
				if f.issues[b] != nil && f.issues[b].Zu {
					zustand = "closed"
				}
				out = append(out, map[string]any{"number": b, "state": zustand, "repository_url": f.srv.URL + "/repos/acme/alpha"})
			}
			f.antworten(w, r, 200, out)
		case len(teile) == 4 && teile[2] == "dependencies" && r.Method == http.MethodPost:
			id, _ := strconv.Atoi(fmt.Sprint(rumpf["issue_id"]))
			i.Blocker = append(i.Blocker, id-1000)
			f.antworten(w, r, http.StatusCreated, map[string]any{})
		default:
			nichtDa()
		}
	case rest == "labels":
		f.antworten(w, r, 200, []map[string]any{{"name": "bug", "color": "d73a4a"}, {"name": "ui", "color": "0e8a16"}})
	case rest == "assignees":
		f.antworten(w, r, 200, []map[string]any{{"login": "ana"}, {"login": "bo"}})
	case rest == "branches":
		out := []map[string]any{}
		for name, sha := range f.branches {
			out = append(out, map[string]any{"name": name, "commit": map[string]any{"sha": sha}})
		}
		sort.Slice(out, func(a, b int) bool { return fmt.Sprint(out[a]["name"]) < fmt.Sprint(out[b]["name"]) })
		f.antworten(w, r, 200, out)
	case len(teile) == 3 && teile[0] == "git" && teile[1] == "trees":
		if teile[2] == "s-main" {
			f.antworten(w, r, 200, map[string]any{"sha": "s-main", "tree": f.baum})
			return
		}
		f.antworten(w, r, 200, map[string]any{"sha": teile[2], "tree": []any{}})
	case len(teile) == 3 && teile[0] == "git" && teile[1] == "blobs":
		inhalt, ok := f.blobs[teile[2]]
		if !ok {
			nichtDa()
			return
		}
		f.antworten(w, r, 200, map[string]any{"sha": teile[2], "encoding": "base64", "content": base64.StdEncoding.EncodeToString([]byte(inhalt))})
	case strings.HasPrefix(rest, "git/refs/heads/") && r.Method == http.MethodDelete:
		name := strings.TrimPrefix(rest, "git/refs/heads/")
		delete(f.branches, name)
		f.geloescht = append(f.geloescht, name)
		w.WriteHeader(http.StatusNoContent)
	case rest == "pulls":
		out := []map[string]any{}
		for _, n := range []int{20, 21} {
			if pr := f.prs[n]; !pr.Zu {
				out = append(out, f.prJSON(pr))
			}
		}
		f.antworten(w, r, 200, out)
	case len(teile) == 2 && teile[0] == "pulls":
		n, _ := strconv.Atoi(teile[1])
		if pr := f.prs[n]; pr != nil {
			f.antworten(w, r, 200, f.prJSON(pr))
			return
		}
		nichtDa()
	case len(teile) == 3 && teile[0] == "pulls" && teile[2] == "merge":
		n, _ := strconv.Atoi(teile[1])
		if pr := f.prs[n]; pr != nil && !pr.Draft {
			pr.Gemergt, pr.Zu = true, true
			f.antworten(w, r, 200, map[string]any{"merged": true})
			return
		}
		f.antworten(w, r, http.StatusMethodNotAllowed, map[string]any{"message": "not mergeable"})
	default:
		nichtDa()
	}
}
