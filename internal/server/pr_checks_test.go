package server_test

// CI-Pruefungen je PR in /api/arbeit, Zwischenspeicher je Head-SHA und die
// Neustart-Route /action/pr/checks-neu gegen eine Fake-GitHub-API
// (pr-checks, #80).

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

type pcGH struct {
	mu      sync.Mutex
	aufrufe []ghAufruf
	srv     *httptest.Server
}

func (f *pcGH) zaehle(methode, pfad string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, a := range f.aufrufe {
		if a.Methode == methode && a.Pfad == pfad {
			n++
		}
	}
	return n
}

func (f *pcGH) schreibend() []ghAufruf {
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

func pcRun(id int, name, status, ergebnis, sha, app, details string) string {
	schluss := "null"
	if ergebnis != "" {
		schluss = fmt.Sprintf("%q", ergebnis)
	}
	ende := `"2026-09-26T10:01:12Z"`
	if status != "completed" {
		ende = "null"
	}
	return fmt.Sprintf(`{"id":%d,"name":%q,"status":%q,"conclusion":%s,"head_sha":%q,
	  "started_at":"2026-09-26T10:00:00Z","completed_at":%s,
	  "html_url":"https://github.com/acme/alpha/runs/%d","details_url":%q,"app":{"slug":%q}}`,
		id, name, status, schluss, sha, ende, id, details, app)
}

func neuesPCGH(t *testing.T) *pcGH {
	t.Helper()
	f := &pcGH{}
	runs := map[int]string{
		101: pcRun(101, "build", "completed", "failure", "sha9", "github-actions", "https://github.com/acme/alpha/actions/runs/555/job/101"),
		202: pcRun(202, "extern", "completed", "failure", "sha9", "andere-app", "https://ci.example/202"),
		303: pcRun(303, "test", "completed", "success", "sha9", "github-actions", "https://github.com/acme/alpha/actions/runs/556/job/303"),
		404: pcRun(404, "alt", "completed", "failure", "anders", "github-actions", "https://github.com/acme/alpha/actions/runs/1/job/404"),
		505: pcRun(505, "lint", "in_progress", "", "sha9", "github-actions", "https://github.com/acme/alpha/actions/runs/555/job/505"),
		606: pcRun(606, "flaky", "completed", "failure", "sha10", "github-actions", "https://github.com/acme/alpha/actions/runs/777/job/606"),
		707: pcRun(707, "test", "completed", "success", "sha10", "github-actions", "https://github.com/acme/alpha/actions/runs/777/job/707"),
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
		var id int
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
			fmt.Fprint(w, "["+prJSON(9, false, "change/neun", "acme/alpha", "open", false, "null", "")+","+
				prJSON(10, false, "change/zehn", "acme/alpha", "open", false, "null", "")+"]")
		case p == "/repos/acme/alpha/pulls/9":
			fmt.Fprint(w, prJSON(9, false, "change/neun", "acme/alpha", "open", false, "true", "clean"))
		case p == "/repos/acme/alpha/pulls/10":
			fmt.Fprint(w, prJSON(10, false, "change/zehn", "acme/alpha", "open", false, "true", "clean"))
		case p == "/repos/acme/alpha/commits/sha9/check-runs":
			fmt.Fprint(w, `{"total_count":2,"check_runs":[`+runs[101]+`,`+runs[505]+`]}`)
		case p == "/repos/acme/alpha/commits/sha9/status":
			fmt.Fprint(w, `{"state":"success","statuses":[{"context":"ci/legacy","state":"success","target_url":"https://ci.example/7",
			  "created_at":"2026-09-26T09:00:00Z","updated_at":"2026-09-26T09:02:00Z"}]}`)
		case p == "/repos/acme/alpha/commits/sha10/check-runs":
			fmt.Fprint(w, `{"total_count":2,"check_runs":[`+runs[707]+`,`+runs[606]+`]}`)
		case p == "/repos/acme/alpha/commits/sha10/status":
			fmt.Fprint(w, `{"state":"pending","statuses":[]}`)
		case r.Method == http.MethodGet && strings.HasPrefix(p, "/repos/acme/alpha/check-runs/"):
			fmt.Sscanf(strings.TrimPrefix(p, "/repos/acme/alpha/check-runs/"), "%d", &id)
			if s, ok := runs[id]; ok {
				fmt.Fprint(w, s)
				return
			}
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"message":"Not Found"}`)
		case r.Method == http.MethodPost && (strings.HasSuffix(p, "/rerun-failed-jobs") || strings.HasSuffix(p, "/rerequest")):
			w.WriteHeader(http.StatusCreated)
			fmt.Fprint(w, `{}`)
		default:
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"message":"Not Found"}`)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

type pcPruefung struct {
	Name        string `json:"name"`
	Art         string `json:"art"`
	Zustand     string `json:"zustand"`
	Ergebnis    string `json:"ergebnis"`
	DauerS      int64  `json:"dauer_s"`
	Dauer       string `json:"dauer"`
	Link        string `json:"link"`
	CheckRunID  int64  `json:"check_run_id"`
	NeuStartbar bool   `json:"neu_startbar"`
}

type pcArbeit struct {
	PRs []struct {
		Nummer int `json:"nummer"`
		Checks *struct {
			SHA             string `json:"sha"`
			Gesamt          string `json:"gesamt"`
			Marke           string `json:"marke"`
			Zahl            int    `json:"zahl"`
			Zusammenfassung struct {
				Gruen  int    `json:"gruen"`
				Rot    int    `json:"rot"`
				Laeuft int    `json:"laeuft"`
				Gesamt int    `json:"gesamt"`
				Text   string `json:"text"`
			} `json:"zusammenfassung"`
			Pruefungen []pcPruefung `json:"pruefungen"`
		} `json:"checks"`
	} `json:"prs"`
}

func TestArbeitLiefertChecksJePR(t *testing.T) {
	f := neuesPCGH(t)
	basis := prServer(t, &prGH{srv: f.srv})
	var a pcArbeit
	if st := holeVon(t, basis+"/api/arbeit?repo=acme/alpha", &a); st != http.StatusOK {
		t.Fatalf("Status %d", st)
	}
	if len(a.PRs) != 2 || a.PRs[0].Nummer != 9 || a.PRs[0].Checks == nil || a.PRs[1].Checks == nil {
		t.Fatalf("PRs %+v", a.PRs)
	}
	c := a.PRs[0].Checks
	if c.SHA != "sha9" || c.Gesamt != "rot" || c.Marke != "✗" || c.Zahl != 1 {
		t.Errorf("#9 checks %+v", c)
	}
	if c.Zusammenfassung.Rot != 1 || c.Zusammenfassung.Laeuft != 1 || c.Zusammenfassung.Gruen != 1 || c.Zusammenfassung.Gesamt != 3 {
		t.Errorf("#9 Zusammenfassung %+v", c.Zusammenfassung)
	}
	var namen []string
	for _, p := range c.Pruefungen {
		namen = append(namen, p.Name)
	}
	if strings.Join(namen, ",") != "build,lint,ci/legacy" {
		t.Fatalf("#9 Reihenfolge %v", namen)
	}
	if b := c.Pruefungen[0]; b.Art != "check" || b.Zustand != "rot" || b.CheckRunID != 101 || !b.NeuStartbar ||
		b.Link != "https://github.com/acme/alpha/runs/101" || b.DauerS != 72 || b.Dauer != "1 min 12 s" {
		t.Errorf("build %+v", b)
	}
	if s := c.Pruefungen[2]; s.Art != "status" || s.Zustand != "gruen" || s.Link != "https://ci.example/7" || s.NeuStartbar || s.CheckRunID != 0 {
		t.Errorf("ci/legacy %+v", s)
	}
	if z := a.PRs[1].Checks; z.SHA != "sha10" || z.Gesamt != "rot" || len(z.Pruefungen) != 2 {
		t.Errorf("#10 checks %+v", z)
	}
}

func TestChecksZwischenspeicherJeSHA(t *testing.T) {
	f := neuesPCGH(t)
	basis := prServer(t, &prGH{srv: f.srv})
	for i := 0; i < 2; i++ {
		var a pcArbeit
		if st := holeVon(t, basis+"/api/arbeit?repo=acme/alpha&frisch=1", &a); st != http.StatusOK {
			t.Fatalf("Status %d", st)
		}
		if len(a.PRs) != 2 || a.PRs[1].Checks == nil || a.PRs[1].Checks.Gesamt != "rot" {
			t.Fatalf("Lauf %d: PRs %+v", i, a.PRs)
		}
	}
	if n := f.zaehle(http.MethodGet, "/repos/acme/alpha/commits/sha10/check-runs"); n != 1 {
		t.Errorf("sha10 (abgeschlossen) %d-mal geholt, erwartet 1", n)
	}
	if n := f.zaehle(http.MethodGet, "/repos/acme/alpha/commits/sha10/status"); n != 1 {
		t.Errorf("sha10-Status %d-mal geholt, erwartet 1", n)
	}
	if n := f.zaehle(http.MethodGet, "/repos/acme/alpha/commits/sha9/check-runs"); n != 2 {
		t.Errorf("sha9 (laeuft) %d-mal geholt, erwartet 2", n)
	}
	resp, rumpf := postJSON(t, basis, "/action/pr/checks-neu", `{"repo":"acme/alpha","nummer":10,"check_run_id":606}`, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("checks-neu: Status %d: %s", resp.StatusCode, rumpf)
	}
	holeVon(t, basis+"/api/arbeit?repo=acme/alpha&frisch=1", nil)
	if n := f.zaehle(http.MethodGet, "/repos/acme/alpha/commits/sha10/check-runs"); n != 2 {
		t.Errorf("sha10 nach dem Neustart %d-mal geholt, erwartet 2", n)
	}
	if n := f.zaehle(http.MethodGet, "/repos/acme/alpha/commits/sha9/check-runs"); n != 3 {
		t.Errorf("sha9 %d-mal geholt, erwartet 3", n)
	}
}

func TestChecksNeuRoute(t *testing.T) {
	f := neuesPCGH(t)
	basis := prServer(t, &prGH{srv: f.srv})
	resp, rumpf := postJSON(t, basis, "/action/pr/checks-neu", `{"repo":"acme/alpha","nummer":9,"check_run_id":101}`, nil)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(rumpf), `"weg":"actions"`) {
		t.Fatalf("Actions: Status %d: %s", resp.StatusCode, rumpf)
	}
	resp, rumpf = postJSON(t, basis, "/action/pr/checks-neu", `{"repo":"acme/alpha","nummer":9,"check_run_id":202}`, nil)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(rumpf), `"weg":"check-run"`) {
		t.Fatalf("anderer Check-Run: Status %d: %s", resp.StatusCode, rumpf)
	}
	s := f.schreibend()
	if len(s) != 2 {
		t.Fatalf("Aufrufe %+v", s)
	}
	if s[0].Methode != http.MethodPost || s[0].Pfad != "/repos/acme/alpha/actions/runs/555/rerun-failed-jobs" {
		t.Errorf("Actions-Aufruf %+v", s[0])
	}
	if s[1].Methode != http.MethodPost || s[1].Pfad != "/repos/acme/alpha/check-runs/202/rerequest" {
		t.Errorf("Check-Run-Aufruf %+v", s[1])
	}
}

func TestChecksNeuLehntAb(t *testing.T) {
	f := neuesPCGH(t)
	basis := prServer(t, &prGH{srv: f.srv})
	for _, fall := range []struct {
		rumpf  string
		kopf   map[string]string
		status int
	}{
		{`{"repo":"acme/alpha","nummer":9,"check_run_id":303}`, nil, http.StatusBadRequest}, // gruen
		{`{"repo":"acme/alpha","nummer":9,"check_run_id":505}`, nil, http.StatusBadRequest}, // laeuft
		{`{"repo":"acme/alpha","nummer":9,"check_run_id":404}`, nil, http.StatusConflict},   // anderer SHA
		{`{"repo":"acme/alpha","nummer":9}`, nil, http.StatusBadRequest},                    // ohne Id
		{`{"repo":"acme/alpha","nummer":9,"check_run_id":101}`, map[string]string{"Origin": "https://boese.example"}, http.StatusForbidden},
	} {
		if resp, rumpf := postJSON(t, basis, "/action/pr/checks-neu", fall.rumpf, fall.kopf); resp.StatusCode != fall.status {
			t.Errorf("%s: Status %d, erwartet %d (%s)", fall.rumpf, resp.StatusCode, fall.status, rumpf)
		}
	}
	if s := f.schreibend(); len(s) != 0 {
		t.Errorf("abgelehnte Neustarts haben geschrieben: %+v", s)
	}
}

func TestChecksNeuUeberNetzAdresse(t *testing.T) {
	_, netzURL, _ := netzMitFreigabe(t, "127.0.0.3")
	status, rumpf := senden(t, clientVon(t, "127.0.0.4"), netzURL, "/action/pr/checks-neu", `{"repo":"acme/alpha","nummer":9,"check_run_id":101}`, nil)
	if status != http.StatusForbidden {
		t.Fatalf("checks-neu von fremder IP ueber die Netz-Adresse: Status %d, erwartet 403 (%s)", status, rumpf)
	}
}
