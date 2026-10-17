package server_test

// Reviews je PR in /api/arbeit und die Schreibrouten /action/pr/reviewer,
// /action/pr/antworten und /action/pr/thread gegen eine Fake-GitHub-API
// (pr-reviews, #81). GraphQL (isResolved, resolveReviewThread) ist hier nur
// ein Fake.

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

type rvGH struct {
	mu        sync.Mutex
	aufrufe   []ghAufruf
	srv       *httptest.Server
	ohneGraph bool
}

// schreibend liefert jeden Aufruf ausser GET und ausser GraphQL-Abfragen
// (nur Mutationen zaehlen).
func (f *rvGH) schreibend() []ghAufruf {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []ghAufruf
	for _, a := range f.aufrufe {
		if a.Methode == http.MethodGet {
			continue
		}
		if a.Pfad == "/graphql" && !strings.HasPrefix(strings.TrimSpace(fmt.Sprint(a.Rumpf["query"])), "mutation") {
			continue
		}
		out = append(out, a)
	}
	return out
}

const rvPR9 = `{"number":9,"title":"PR 9","html_url":"u9","draft":false,"node_id":"PR_9","state":"open","merged":false,
  "mergeable":true,"mergeable_state":"clean","user":{"login":"cy"},
  "requested_reviewers":[{"login":"dd"}],"requested_teams":[{"slug":"kern","name":"Kern"}],
  "head":{"ref":"change/neun","sha":"sha9","repo":{"full_name":"acme/alpha"}},"base":{"ref":"main"}}`

func rvKommentar(id, antwortAuf int, autor, pfad string, zeile int, zeit string) string {
	bezug := ""
	if antwortAuf > 0 {
		bezug = fmt.Sprintf(`"in_reply_to_id":%d,`, antwortAuf)
	}
	return fmt.Sprintf(`{"id":%d,%s"body":"Text %d","user":{"login":%q},"path":%q,"line":%d,"original_line":%d,
	  "diff_hunk":"@@ -1 +1 @@\n-a\n+b","created_at":"2026-09-26T%s:00Z","html_url":"https://github.com/acme/alpha/pull/9#discussion_r%d"}`,
		id, bezug, id, autor, pfad, zeile, zeile, zeit, id)
}

func neuesRVGH(t *testing.T) *rvGH {
	t.Helper()
	f := &rvGH{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		a := ghAufruf{Methode: r.Method, Pfad: r.URL.Path, Query: r.URL.RawQuery}
		if len(b) > 0 {
			_ = json.Unmarshal(b, &a.Rumpf)
		}
		f.mu.Lock()
		f.aufrufe = append(f.aufrufe, a)
		ohneGraph := f.ohneGraph
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
			fmt.Fprint(w, `[{"number":9,"title":"PR 9","pull_request":{}}]`)
		case p == "/repos/acme/alpha/branches":
			fmt.Fprint(w, `[]`)
		case p == "/repos/acme/alpha/pulls":
			fmt.Fprint(w, "["+rvPR9+"]")
		case p == "/repos/acme/alpha/pulls/9":
			fmt.Fprint(w, rvPR9)
		case p == "/repos/acme/alpha/assignees":
			fmt.Fprint(w, `[{"login":"ana"},{"login":"bo"},{"login":"cy"},{"login":"dd"}]`)
		case p == "/repos/acme/alpha/pulls/9/reviews":
			fmt.Fprint(w, `[{"id":1,"user":{"login":"ana"},"state":"APPROVED","submitted_at":"2026-09-26T10:00:00Z"},
			  {"id":2,"user":{"login":"bo"},"state":"CHANGES_REQUESTED","submitted_at":"2026-09-26T10:05:00Z"}]`)
		case p == "/repos/acme/alpha/pulls/9/comments" && r.Method == http.MethodGet:
			fmt.Fprint(w, "["+strings.Join([]string{
				rvKommentar(11, 0, "bo", "web/a.go", 4, "10:01"),
				rvKommentar(12, 11, "cy", "web/a.go", 4, "10:02"),
				rvKommentar(21, 0, "bo", "web/b.go", 7, "10:03"),
				rvKommentar(31, 0, "ana", "api.go", 2, "09:00"),
			}, ",")+"]")
		case r.Method == http.MethodPost && strings.HasPrefix(p, "/repos/acme/alpha/pulls/9/comments/") && strings.HasSuffix(p, "/replies"):
			w.WriteHeader(http.StatusCreated)
			fmt.Fprint(w, `{"id":99,"in_reply_to_id":11,"body":"erledigt","user":{"login":"cy"},"path":"web/a.go","line":4,
			  "created_at":"2026-09-26T12:00:00Z","html_url":"u99"}`)
		case p == "/repos/acme/alpha/pulls/9/requested_reviewers":
			w.WriteHeader(http.StatusCreated)
			fmt.Fprint(w, rvPR9)
		case p == "/graphql":
			if ohneGraph {
				w.WriteHeader(http.StatusForbidden)
				fmt.Fprint(w, `{"message":"GraphQL gesperrt"}`)
				return
			}
			if strings.HasPrefix(strings.TrimSpace(fmt.Sprint(a.Rumpf["query"])), "mutation") {
				fmt.Fprint(w, `{"data":{}}`)
				return
			}
			fmt.Fprint(w, `{"data":{"repository":{"pullRequest":{"reviewThreads":{"nodes":[
			  {"id":"PRRT_11","isResolved":false,"comments":{"nodes":[{"databaseId":11}]}},
			  {"id":"PRRT_21","isResolved":false,"comments":{"nodes":[{"databaseId":21}]}},
			  {"id":"PRRT_31","isResolved":true,"comments":{"nodes":[{"databaseId":31}]}}]}}}}}`)
		default:
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"message":"Not Found"}`)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

type rvArbeit struct {
	PRs []struct {
		Nummer  int `json:"nummer"`
		Reviews *struct {
			Entscheidung string `json:"entscheidung"`
			Marke        string `json:"marke"`
			Titel        string `json:"titel"`
			Stimmen      []struct {
				Login   string `json:"login"`
				Zustand string `json:"zustand"`
			} `json:"stimmen"`
			Angefordert        []string `json:"angefordert"`
			AngefordertTeams   []string `json:"angefordert_teams"`
			OffeneThreads      int      `json:"offene_threads"`
			AufgeloesteThreads int      `json:"aufgeloeste_threads"`
			ThreadZustand      bool     `json:"thread_zustand"`
			Threads            []struct {
				ID         int64  `json:"id"`
				Pfad       string `json:"pfad"`
				Zeile      int    `json:"zeile"`
				Veraltet   bool   `json:"veraltet"`
				Diff       string `json:"diff"`
				Aufgeloest bool   `json:"aufgeloest"`
				Kommentare []struct {
					ID    int64  `json:"id"`
					Autor string `json:"autor"`
					Text  string `json:"text"`
					Zeit  string `json:"zeit"`
					URL   string `json:"url"`
				} `json:"kommentare"`
			} `json:"threads"`
		} `json:"reviews"`
	} `json:"prs"`
}

func TestArbeitLiefertReviewsJePR(t *testing.T) {
	f := neuesRVGH(t)
	basis := prServer(t, &prGH{srv: f.srv})
	var a rvArbeit
	if st := holeVon(t, basis+"/api/arbeit?repo=acme/alpha", &a); st != http.StatusOK {
		t.Fatalf("Status %d", st)
	}
	if len(a.PRs) != 1 || a.PRs[0].Reviews == nil {
		t.Fatalf("PRs %+v", a.PRs)
	}
	r := a.PRs[0].Reviews
	if r.Entscheidung != "aenderungen" || r.Marke != "✎" || !r.ThreadZustand {
		t.Errorf("Entscheidung %q Marke %q Zustand %v", r.Entscheidung, r.Marke, r.ThreadZustand)
	}
	if strings.Join(r.Angefordert, ",") != "dd" || strings.Join(r.AngefordertTeams, ",") != "kern" {
		t.Errorf("angefordert %v %v", r.Angefordert, r.AngefordertTeams)
	}
	if len(r.Stimmen) != 2 || r.Stimmen[0].Login != "ana" || r.Stimmen[0].Zustand != "genehmigt" || r.Stimmen[1].Zustand != "aenderungen" {
		t.Errorf("Stimmen %+v", r.Stimmen)
	}
	if r.OffeneThreads != 2 || r.AufgeloesteThreads != 1 || len(r.Threads) != 3 {
		t.Fatalf("Threads %+v", r.Threads)
	}
	if r.Threads[0].ID != 11 || r.Threads[1].ID != 21 || r.Threads[2].ID != 31 || !r.Threads[2].Aufgeloest || r.Threads[0].Aufgeloest {
		t.Errorf("Reihenfolge/Zustand %+v", r.Threads)
	}
	th := r.Threads[0]
	if th.Pfad != "web/a.go" || th.Zeile != 4 || th.Veraltet || th.Diff == "" || len(th.Kommentare) != 2 ||
		th.Kommentare[0].ID != 11 || th.Kommentare[1].ID != 12 || th.Kommentare[1].Autor != "cy" || th.Kommentare[1].Text != "Text 12" ||
		th.Kommentare[0].URL != "https://github.com/acme/alpha/pull/9#discussion_r11" {
		t.Errorf("Thread 11 %+v", th)
	}
	if !strings.Contains(r.Titel, "Änderungen verlangt von bo") || !strings.Contains(r.Titel, "2 offene Threads") {
		t.Errorf("Titel %q", r.Titel)
	}
}

func TestArbeitReviewsOhneGraphQL(t *testing.T) {
	f := neuesRVGH(t)
	f.ohneGraph = true
	basis := prServer(t, &prGH{srv: f.srv})
	var a rvArbeit
	if st := holeVon(t, basis+"/api/arbeit?repo=acme/alpha", &a); st != http.StatusOK {
		t.Fatalf("Status %d", st)
	}
	if len(a.PRs) != 1 || a.PRs[0].Reviews == nil {
		t.Fatalf("ohne GraphQL fehlen die Reviews: %+v", a.PRs)
	}
	r := a.PRs[0].Reviews
	if r.ThreadZustand || r.OffeneThreads != 3 || r.AufgeloesteThreads != 0 || len(r.Threads) != 3 || r.Entscheidung != "aenderungen" {
		t.Errorf("ohne GraphQL %+v", r)
	}
}

func TestReviewerRoute(t *testing.T) {
	f := neuesRVGH(t)
	basis := prServer(t, &prGH{srv: f.srv})
	resp, rumpf := postJSON(t, basis, "/action/pr/reviewer", `{"repo":"acme/alpha","nummer":9,"hinzufuegen":["ana"],"entfernen":["dd"]}`, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Status %d: %s", resp.StatusCode, rumpf)
	}
	s := f.schreibend()
	if len(s) != 2 {
		t.Fatalf("Aufrufe %+v", s)
	}
	if s[0].Methode != http.MethodPost || s[0].Pfad != "/repos/acme/alpha/pulls/9/requested_reviewers" || fmt.Sprint(s[0].Rumpf["reviewers"]) != "[ana]" {
		t.Errorf("anfordern %+v", s[0])
	}
	if s[1].Methode != http.MethodDelete || s[1].Pfad != "/repos/acme/alpha/pulls/9/requested_reviewers" || fmt.Sprint(s[1].Rumpf["reviewers"]) != "[dd]" {
		t.Errorf("entfernen %+v", s[1])
	}
}

func TestReviewerRouteLehntAb(t *testing.T) {
	f := neuesRVGH(t)
	basis := prServer(t, &prGH{srv: f.srv})
	for _, fall := range []struct {
		rumpf  string
		status int
	}{
		{`{"repo":"acme/alpha","nummer":9,"hinzufuegen":["fremd"]}`, http.StatusBadRequest}, // nicht zustaendig
		{`{"repo":"acme/alpha","nummer":9,"hinzufuegen":["cy"]}`, http.StatusBadRequest},    // Autor
		{`{"repo":"acme/alpha","nummer":9}`, http.StatusBadRequest},                         // nichts zu tun
		{`{"repo":"acme/alpha","nummer":9,"entfernen":["bo"]}`, http.StatusConflict},        // nicht angefragt
		{`{"repo":"acme/alpha","nummer":9,"hinzufuegen":["a b"]}`, http.StatusBadRequest},   // kein Login
	} {
		if resp, rumpf := postJSON(t, basis, "/action/pr/reviewer", fall.rumpf, nil); resp.StatusCode != fall.status {
			t.Errorf("%s: Status %d, erwartet %d (%s)", fall.rumpf, resp.StatusCode, fall.status, rumpf)
		}
	}
	if s := f.schreibend(); len(s) != 0 {
		t.Errorf("abgelehnte Reviewer haben geschrieben: %+v", s)
	}
}

func TestAntwortenRoute(t *testing.T) {
	f := neuesRVGH(t)
	basis := prServer(t, &prGH{srv: f.srv})
	resp, rumpf := postJSON(t, basis, "/action/pr/antworten", `{"repo":"acme/alpha","nummer":9,"kommentar_id":12,"text":"erledigt"}`, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Status %d: %s", resp.StatusCode, rumpf)
	}
	var antwort struct {
		OK        bool `json:"ok"`
		Kommentar struct {
			ID   int64  `json:"id"`
			Text string `json:"text"`
		} `json:"kommentar"`
	}
	if err := json.Unmarshal(rumpf, &antwort); err != nil || !antwort.OK || antwort.Kommentar.ID != 99 || antwort.Kommentar.Text != "erledigt" {
		t.Errorf("Antwort %s (%v)", rumpf, err)
	}
	s := f.schreibend()
	if len(s) != 1 || s[0].Methode != http.MethodPost || s[0].Pfad != "/repos/acme/alpha/pulls/9/comments/11/replies" || s[0].Rumpf["body"] != "erledigt" {
		t.Errorf("Aufrufe %+v", s)
	}
}

func TestAntwortenLehntAb(t *testing.T) {
	f := neuesRVGH(t)
	basis := prServer(t, &prGH{srv: f.srv})
	for _, fall := range []struct {
		rumpf  string
		kopf   map[string]string
		status int
	}{
		{`{"repo":"acme/alpha","nummer":9,"kommentar_id":999,"text":"hallo"}`, nil, http.StatusNotFound},
		{`{"repo":"acme/alpha","nummer":9,"kommentar_id":11,"text":"   "}`, nil, http.StatusBadRequest},
		{`{"repo":"acme/alpha","nummer":9,"text":"hallo"}`, nil, http.StatusBadRequest},
		{`{"repo":"acme/alpha","nummer":9,"kommentar_id":11,"text":"hallo"}`, map[string]string{"Origin": "https://boese.example"}, http.StatusForbidden},
	} {
		if resp, rumpf := postJSON(t, basis, "/action/pr/antworten", fall.rumpf, fall.kopf); resp.StatusCode != fall.status {
			t.Errorf("%s: Status %d, erwartet %d (%s)", fall.rumpf, resp.StatusCode, fall.status, rumpf)
		}
	}
	if s := f.schreibend(); len(s) != 0 {
		t.Errorf("abgelehnte Antworten haben geschrieben: %+v", s)
	}
}

func TestThreadRoute(t *testing.T) {
	f := neuesRVGH(t)
	basis := prServer(t, &prGH{srv: f.srv})
	for _, r := range []string{
		`{"repo":"acme/alpha","nummer":9,"kommentar_id":11,"aufgeloest":true}`,
		`{"repo":"acme/alpha","nummer":9,"kommentar_id":31,"aufgeloest":false}`,
	} {
		if resp, rumpf := postJSON(t, basis, "/action/pr/thread", r, nil); resp.StatusCode != http.StatusOK {
			t.Fatalf("%s: Status %d: %s", r, resp.StatusCode, rumpf)
		}
	}
	s := f.schreibend()
	if len(s) != 2 {
		t.Fatalf("Mutationen %+v", s)
	}
	for i, fall := range []struct{ mutation, id string }{{"resolveReviewThread", "PRRT_11"}, {"unresolveReviewThread", "PRRT_31"}} {
		q := fmt.Sprint(s[i].Rumpf["query"])
		v, _ := s[i].Rumpf["variables"].(map[string]any)
		if s[i].Pfad != "/graphql" || !strings.Contains(q, " "+fall.mutation+"(") || v["id"] != fall.id {
			t.Errorf("%s: %+v", fall.mutation, s[i])
		}
	}
}

func TestThreadLehntAb(t *testing.T) {
	f := neuesRVGH(t)
	basis := prServer(t, &prGH{srv: f.srv})
	for _, fall := range []struct {
		rumpf  string
		kopf   map[string]string
		status int
	}{
		{`{"repo":"acme/alpha","nummer":9,"kommentar_id":999,"aufgeloest":true}`, nil, http.StatusNotFound},
		{`{"repo":"acme/alpha","nummer":9,"kommentar_id":31,"aufgeloest":true}`, nil, http.StatusConflict},
		{`{"repo":"acme/alpha","nummer":9,"kommentar_id":11}`, nil, http.StatusBadRequest},
		{`{"repo":"acme/alpha","nummer":9,"aufgeloest":true}`, nil, http.StatusBadRequest},
		{`{"repo":"acme/alpha","nummer":9,"kommentar_id":11,"aufgeloest":true}`, map[string]string{"Origin": "https://boese.example"}, http.StatusForbidden},
	} {
		if resp, rumpf := postJSON(t, basis, "/action/pr/thread", fall.rumpf, fall.kopf); resp.StatusCode != fall.status {
			t.Errorf("%s: Status %d, erwartet %d (%s)", fall.rumpf, resp.StatusCode, fall.status, rumpf)
		}
	}
	if s := f.schreibend(); len(s) != 0 {
		t.Errorf("abgelehnte Thread-Aenderungen haben geschrieben: %+v", s)
	}
}

func TestReviewRoutenUeberNetzAdresse(t *testing.T) {
	_, netzURL, _ := netzMitFreigabe(t, "127.0.0.3")
	for _, fall := range []struct{ pfad, rumpf string }{
		{"/action/pr/reviewer", `{"repo":"acme/alpha","nummer":9,"hinzufuegen":["ana"]}`},
		{"/action/pr/antworten", `{"repo":"acme/alpha","nummer":9,"kommentar_id":11,"text":"hallo"}`},
		{"/action/pr/thread", `{"repo":"acme/alpha","nummer":9,"kommentar_id":11,"aufgeloest":true}`},
	} {
		status, rumpf := senden(t, clientVon(t, "127.0.0.4"), netzURL, fall.pfad, fall.rumpf, nil)
		if status != http.StatusForbidden {
			t.Errorf("%s von fremder IP ueber die Netz-Adresse: Status %d, erwartet 403 (%s)", fall.pfad, status, rumpf)
		}
	}
}
