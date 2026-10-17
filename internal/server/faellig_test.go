package server_test

// /api/uebersicht und das Feld faellig in den Schreibrouten (faelligkeit, #39).

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

type faelligGH struct {
	mu      sync.Mutex
	patches []map[string]any
	posts   []map[string]any
	srv     *httptest.Server
}

func tagVon(delta int) string { return time.Now().AddDate(0, 0, delta).Format("2006-01-02") }

func neuesFaelligGH(t *testing.T) *faelligGH {
	t.Helper()
	f := &faelligGH{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var rumpf map[string]any
		_ = json.Unmarshal(b, &rumpf)
		w.Header().Set("Content-Type", "application/json")
		issue := func(n int, text string) map[string]any {
			return map[string]any{"number": n, "title": fmt.Sprint("Issue ", n), "body": text, "state": "open"}
		}
		switch p := r.URL.Path; {
		case p == "/user/repos":
			fmt.Fprint(w, `[{"full_name":"acme/alpha","name":"alpha","owner":{"login":"acme"}},{"full_name":"acme/beta","name":"beta","owner":{"login":"acme"}},
			 {"full_name":"acme/alt","name":"alt","owner":{"login":"acme"},"archived":true}]`)
		case p == "/repos/acme/alpha/issues" && r.Method == http.MethodGet:
			json.NewEncoder(w).Encode([]map[string]any{
				issue(1, "a\n\nFällig: "+tagVon(-4)), issue(2, "Fällig: "+tagVon(0)), issue(3, "Fällig: "+tagVon(6)), issue(4, "ohne Datum")})
		case p == "/repos/acme/beta/issues" && r.Method == http.MethodGet:
			pr := issue(9, "Fällig: "+tagVon(-9))
			pr["pull_request"] = map[string]any{}
			json.NewEncoder(w).Encode([]map[string]any{issue(7, "Fällig: "+tagVon(-2)), pr})
		case p == "/repos/acme/alt/issues":
			t.Errorf("archiviertes Repo abgefragt")
			fmt.Fprint(w, `[]`)
		case p == "/repos/acme/alpha/issues/4" && r.Method == http.MethodGet:
			json.NewEncoder(w).Encode(issue(4, "Vier bauen\n\nFällig: 2026-09-01"))
		case p == "/repos/acme/alpha/issues/4" && r.Method == http.MethodPatch:
			f.mu.Lock()
			f.patches = append(f.patches, rumpf)
			f.mu.Unlock()
			json.NewEncoder(w).Encode(issue(4, fmt.Sprint(rumpf["body"])))
		case p == "/repos/acme/alpha/issues" && r.Method == http.MethodPost:
			f.mu.Lock()
			f.posts = append(f.posts, rumpf)
			f.mu.Unlock()
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(issue(50, fmt.Sprint(rumpf["body"])))
		default:
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"message":"Not Found"}`)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

type testUebersicht struct {
	Heute, Demnaechst, Irgendwann []struct {
		Repo    string `json:"repo"`
		Nummer  int    `json:"nummer"`
		Faellig string `json:"faellig"`
		Termin  string `json:"termin"`
		Tage    int    `json:"tage"`
	}
	Zaehler struct {
		Ueberfaellig int `json:"ueberfaellig"`
		Heute        int `json:"heute"`
		Demnaechst   int `json:"demnaechst"`
		Irgendwann   int `json:"irgendwann"`
	} `json:"zaehler"`
	Sync *struct {
		Zeit string `json:"zeit"`
	} `json:"sync"`
}

func TestApiUebersicht(t *testing.T) {
	ghrest.SpeicherLeeren()
	f := neuesFaelligGH(t)
	basis, _ := schreibServer(t, &schreibGH{srv: f.srv})
	var u testUebersicht
	if st := holeVon(t, basis+"/api/uebersicht", &u); st != http.StatusOK {
		t.Fatalf("Status %d", st)
	}
	var heute []string
	for _, e := range u.Heute {
		heute = append(heute, fmt.Sprintf("%s#%d:%s", e.Repo, e.Nummer, e.Termin))
	}
	if fmt.Sprint(heute) != "[acme/alpha#1:ueberfaellig acme/beta#7:ueberfaellig acme/alpha#2:heute]" {
		t.Errorf("heute %v", heute)
	}
	if len(u.Demnaechst) != 1 || u.Demnaechst[0].Nummer != 3 || u.Demnaechst[0].Tage != 6 {
		t.Errorf("demnaechst %+v", u.Demnaechst)
	}
	if len(u.Irgendwann) != 1 || u.Irgendwann[0].Nummer != 4 {
		t.Errorf("irgendwann %+v", u.Irgendwann)
	}
	if z := u.Zaehler; z.Ueberfaellig != 2 || z.Heute != 1 || z.Demnaechst != 1 || z.Irgendwann != 1 {
		t.Errorf("Zaehler %+v", z)
	}
	if u.Sync == nil || u.Sync.Zeit == "" {
		t.Errorf("ohne sync: %+v", u.Sync)
	}
}

func TestFaelligSchreiben(t *testing.T) {
	ghrest.SpeicherLeeren()
	f := neuesFaelligGH(t)
	basis, _ := schreibServer(t, &schreibGH{srv: f.srv})

	resp, rumpf := postJSON(t, basis, "/action/issue/aendern", `{"repo":"acme/alpha","nummer":4,"faellig":"2026-10-02"}`, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Status %d: %s", resp.StatusCode, rumpf)
	}
	if len(f.patches) != 1 || f.patches[0]["body"] != "Vier bauen\n\nFällig: 2026-10-02" {
		t.Errorf("PATCH %+v", f.patches)
	}
	resp, _ = postJSON(t, basis, "/action/issue/aendern", `{"repo":"acme/alpha","nummer":4,"faellig":"morgen"}`, nil)
	if resp.StatusCode != http.StatusBadRequest || len(f.patches) != 1 {
		t.Errorf("falsches Format: Status %d, PATCHes %d", resp.StatusCode, len(f.patches))
	}
	resp, rumpf = postJSON(t, basis, "/action/issue/anlegen", `{"repo":"acme/alpha","titel":"Neu","text":"Text","faellig":"2026-11-03"}`, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("anlegen Status %d: %s", resp.StatusCode, rumpf)
	}
	if len(f.posts) != 1 || f.posts[0]["body"] != "Text\n\nFällig: 2026-11-03" {
		t.Errorf("POST %+v", f.posts)
	}
}
