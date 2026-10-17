package server_test

// PRs in /api/arbeit und die Routen unter /action/pr/ gegen eine
// Fake-GitHub-API (pr-aktionen, #32).

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

type prGH struct {
	mu      sync.Mutex
	aufrufe []ghAufruf
	srv     *httptest.Server
}

func (f *prGH) schreibend() []ghAufruf {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []ghAufruf
	for _, a := range f.aufrufe {
		if a.Methode != http.MethodGet {
			out = append(out, a)
		}
	}
	return out
}

func prJSON(n int, draft bool, head, headRepo, state string, merged bool, mergeable string, mstate string) string {
	return fmt.Sprintf(`{"number":%d,"title":"PR %d","html_url":"u%d","draft":%v,"node_id":"PR_%d","state":%q,"merged":%v,
	  "mergeable":%s,"mergeable_state":%q,"user":{"login":"cy"},
	  "head":{"ref":%q,"sha":"sha%d","repo":{"full_name":%q}},"base":{"ref":"main"}}`,
		n, n, n, draft, n, state, merged, mergeable, mstate, head, n, headRepo)
}

func neuesPRGH(t *testing.T) *prGH {
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
			fmt.Fprint(w, `[{"number":9,"title":"PR 9","pull_request":{}},{"number":10,"title":"PR 10","pull_request":{}}]`)
		case p == "/repos/acme/alpha/branches":
			fmt.Fprint(w, `[]`)
		case p == "/repos/acme/alpha/pulls":
			fmt.Fprint(w, "["+prJSON(9, true, "change/neun", "acme/alpha", "open", false, "null", "")+","+
				prJSON(10, false, "change/zehn", "acme/alpha", "open", false, "null", "")+"]")
		case p == "/repos/acme/alpha/pulls/9":
			fmt.Fprint(w, prJSON(9, true, "change/neun", "acme/alpha", "open", false, "true", "draft"))
		case p == "/repos/acme/alpha/pulls/10" && r.Method == http.MethodGet:
			fmt.Fprint(w, prJSON(10, false, "change/zehn", "acme/alpha", "open", false, "false", "dirty"))
		case p == "/repos/acme/alpha/pulls/11":
			fmt.Fprint(w, prJSON(11, false, "change/elf", "acme/alpha", "closed", true, "null", ""))
		case p == "/repos/acme/alpha/pulls/12":
			fmt.Fprint(w, prJSON(12, false, "feature", "ich/alpha", "closed", true, "null", ""))
		case p == "/repos/acme/alpha/pulls/13":
			fmt.Fprint(w, prJSON(13, false, "main", "acme/alpha", "closed", true, "null", ""))
		case p == "/graphql":
			fmt.Fprint(w, `{"data":{"markPullRequestReadyForReview":{"pullRequest":{"isDraft":false}}}}`)
		case strings.HasSuffix(p, "/merge") && r.Method == http.MethodPut:
			fmt.Fprint(w, `{"merged":true}`)
		case strings.HasPrefix(p, "/repos/acme/alpha/git/refs/heads/") && r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"message":"Not Found"}`)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func prServer(t *testing.T, f *prGH) string {
	t.Helper()
	basis, _ := schreibServer(t, &schreibGH{srv: f.srv})
	return basis
}

func TestArbeitLiefertPRsMitStatus(t *testing.T) {
	f := neuesPRGH(t)
	basis := prServer(t, f)
	var a struct {
		PRs []struct {
			Nummer      int    `json:"nummer"`
			Draft       bool   `json:"draft"`
			Head        string `json:"head"`
			Mergebar    *bool  `json:"mergebar"`
			MergeStatus string `json:"merge_status"`
		} `json:"prs"`
	}
	if st := holeVon(t, basis+"/api/arbeit?repo=acme/alpha", &a); st != http.StatusOK {
		t.Fatalf("Status %d", st)
	}
	if len(a.PRs) != 2 {
		t.Fatalf("PRs %+v", a.PRs)
	}
	neun, zehn := a.PRs[0], a.PRs[1]
	if !neun.Draft || neun.Head != "change/neun" || neun.Mergebar == nil || !*neun.Mergebar {
		t.Errorf("#9 %+v", neun)
	}
	if zehn.Mergebar == nil || *zehn.Mergebar || zehn.MergeStatus != "dirty" {
		t.Errorf("#10 %+v", zehn)
	}
}

func TestPRFreigebenRoute(t *testing.T) {
	f := neuesPRGH(t)
	basis := prServer(t, f)
	resp, rumpf := postJSON(t, basis, "/action/pr/freigeben", `{"repo":"acme/alpha","nummer":9}`, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Status %d: %s", resp.StatusCode, rumpf)
	}
	s := f.schreibend()
	if len(s) != 1 || s[0].Pfad != "/graphql" || !strings.Contains(fmt.Sprint(s[0].Rumpf["query"]), "markPullRequestReadyForReview") ||
		!strings.Contains(fmt.Sprint(s[0].Rumpf["variables"]), "PR_9") {
		t.Errorf("Aufrufe %+v", s)
	}
	resp, _ = postJSON(t, basis, "/action/pr/freigeben", `{"repo":"acme/alpha","nummer":10}`, nil)
	if resp.StatusCode != http.StatusBadRequest || len(f.schreibend()) != 1 {
		t.Errorf("kein Draft: Status %d, Aufrufe %+v", resp.StatusCode, f.schreibend())
	}
}

func TestPRMergenRoute(t *testing.T) {
	f := neuesPRGH(t)
	basis := prServer(t, f)
	resp, rumpf := postJSON(t, basis, "/action/pr/mergen", `{"repo":"acme/alpha","nummer":9}`, nil)
	if resp.StatusCode != http.StatusBadRequest || len(f.schreibend()) != 0 {
		t.Fatalf("Draft mergen: Status %d (%s), Aufrufe %+v", resp.StatusCode, rumpf, f.schreibend())
	}
	resp, rumpf = postJSON(t, basis, "/action/pr/mergen", `{"repo":"acme/alpha","nummer":10,"branch_loeschen":true}`, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Status %d: %s", resp.StatusCode, rumpf)
	}
	s := f.schreibend()
	if len(s) != 2 {
		t.Fatalf("Aufrufe %+v", s)
	}
	if s[0].Methode != http.MethodPut || s[0].Pfad != "/repos/acme/alpha/pulls/10/merge" || s[0].Rumpf["merge_method"] != "merge" || s[0].Rumpf["sha"] != "sha10" {
		t.Errorf("Merge %+v", s[0])
	}
	if s[1].Methode != http.MethodDelete || s[1].Pfad != "/repos/acme/alpha/git/refs/heads/change/zehn" {
		t.Errorf("Loeschen %+v", s[1])
	}
}

func TestBranchLoeschenRoute(t *testing.T) {
	f := neuesPRGH(t)
	basis := prServer(t, f)
	for _, fall := range []struct {
		rumpf  string
		status int
	}{
		{`{"repo":"acme/alpha","nummer":10}`, http.StatusBadRequest}, // offen
		{`{"repo":"acme/alpha","nummer":12}`, http.StatusBadRequest}, // Branch in einem Fork
		{`{"repo":"acme/alpha","nummer":13}`, http.StatusBadRequest}, // Default-Branch
		{`{"repo":"acme/alpha","nummer":11,"foo":1}`, http.StatusBadRequest},
	} {
		if resp, rumpf := postJSON(t, basis, "/action/pr/branch-loeschen", fall.rumpf, nil); resp.StatusCode != fall.status {
			t.Errorf("%s: Status %d, erwartet %d (%s)", fall.rumpf, resp.StatusCode, fall.status, rumpf)
		}
	}
	if s := f.schreibend(); len(s) != 0 {
		t.Fatalf("abgelehnte Anfragen haben geschrieben: %+v", s)
	}
	resp, rumpf := postJSON(t, basis, "/action/pr/branch-loeschen", `{"repo":"acme/alpha","nummer":11}`, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Status %d: %s", resp.StatusCode, rumpf)
	}
	if s := f.schreibend(); len(s) != 1 || s[0].Pfad != "/repos/acme/alpha/git/refs/heads/change/elf" {
		t.Errorf("Aufrufe %+v", s)
	}
	if resp, _ := postJSON(t, basis, "/action/pr/mergen", `{"repo":"acme/alpha","nummer":10}`, map[string]string{"Origin": "https://boese.example"}); resp.StatusCode != http.StatusForbidden {
		t.Errorf("fremde Origin: Status %d", resp.StatusCode)
	}
}
