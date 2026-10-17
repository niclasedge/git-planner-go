package server_test

// GET /api/repo-stand (things-farben-fortschritt, #41): offene Issues und PRs
// aus der offenen Liste, geschlossene ueber den Link-Kopf gezaehlt.

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

func TestApiRepoStand(t *testing.T) {
	ghrest.SpeicherLeeren()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		letzte := func(n int) {
			w.Header().Set("Link", fmt.Sprintf(`<%s?page=%d>; rel="last"`, r.URL.Path, n))
		}
		geschlossen := r.URL.Query().Get("state") == "closed"
		switch p := r.URL.Path; {
		case p == "/user/repos":
			fmt.Fprint(w, `[{"full_name":"acme/alpha","name":"alpha","owner":{"login":"acme"}},{"full_name":"acme/beta","name":"beta","owner":{"login":"acme"}},
			 {"full_name":"acme/alt","name":"alt","owner":{"login":"acme"},"archived":true}]`)
		case p == "/repos/acme/alpha/issues" && geschlossen:
			letzte(9)
			fmt.Fprint(w, `[{"number":10}]`)
		case p == "/repos/acme/alpha/pulls" && geschlossen:
			letzte(3)
			fmt.Fprint(w, `[{"number":11}]`)
		case p == "/repos/acme/alpha/issues":
			fmt.Fprint(w, `[{"number":1},{"number":2},{"number":3},{"number":4,"pull_request":{}},{"number":5,"pull_request":{}}]`)
		case p == "/repos/acme/beta/issues", p == "/repos/acme/beta/pulls":
			fmt.Fprint(w, `[]`)
		case p == "/repos/acme/alt/issues", p == "/repos/acme/alt/pulls":
			t.Errorf("archiviertes Repo abgefragt: %s", r.URL)
			fmt.Fprint(w, `[]`)
		default:
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"message":"Not Found"}`)
		}
	}))
	defer srv.Close()
	basis, _ := schreibServer(t, &schreibGH{srv: srv})

	var s struct {
		Repos []struct {
			Repo            string  `json:"repo"`
			OffeneIssues    int     `json:"offene_issues"`
			OffenePRs       int     `json:"offene_prs"`
			ErledigteIssues int     `json:"erledigte_issues"`
			Anteil          float64 `json:"anteil"`
		} `json:"repos"`
		Sync *struct {
			Zeit string `json:"zeit"`
		} `json:"sync"`
	}
	if st := holeVon(t, basis+"/api/repo-stand", &s); st != http.StatusOK {
		t.Fatalf("Status %d", st)
	}
	je := map[string]string{}
	for _, r := range s.Repos {
		je[r.Repo] = fmt.Sprintf("%d/%d/%d/%.4f", r.OffeneIssues, r.OffenePRs, r.ErledigteIssues, r.Anteil)
	}
	if je["acme/alpha"] != "3/2/6/0.6667" {
		t.Errorf("alpha %q, erwartet 3/2/6/0.6667", je["acme/alpha"])
	}
	if je["acme/beta"] != "0/0/0/0.0000" {
		t.Errorf("beta %q", je["acme/beta"])
	}
	if _, da := je["acme/alt"]; da || len(s.Repos) != 2 {
		t.Errorf("Repos %v, erwartet nur alpha und beta", je)
	}
	if s.Sync == nil || s.Sync.Zeit == "" {
		t.Errorf("ohne sync: %+v", s.Sync)
	}
}
