package ghrest_test

// Workflow-Runs, Jobs, Job-Logs und der Neustart fehlgeschlagener Jobs gegen
// eine Fake-GitHub-API (ci-runs, #122).

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

const akRuns = `{"total_count":2,"workflow_runs":[
 {"id":12,"name":"CI","event":"push","head_branch":"main","head_sha":"3f9c2e1a7b8c9d0e","display_title":"Titel",
  "head_commit":{"message":"ci-runs: Betreff\n\nRumpf"},
  "run_number":12,"run_attempt":1,"status":"completed","conclusion":"failure",
  "run_started_at":"2026-09-30T09:00:00Z","updated_at":"2026-09-30T09:02:05Z",
  "html_url":"https://github.com/acme/alpha/actions/runs/12"},
 {"id":10,"name":"CI","event":"pull_request","head_branch":"change/neu","head_sha":"aaaa","display_title":"Nur Titel",
  "head_commit":null,"run_number":10,"run_attempt":3,"status":"in_progress","conclusion":null,
  "run_started_at":"2026-09-30T09:50:00Z","updated_at":"2026-09-30T09:55:00Z",
  "html_url":"https://github.com/acme/alpha/actions/runs/10"}]}`

const akJobs = `{"total_count":1,"jobs":[
 {"id":501,"run_id":12,"run_attempt":1,"name":"test","status":"completed","conclusion":"failure",
  "started_at":"2026-09-30T09:00:02Z","completed_at":"2026-09-30T09:01:54Z",
  "html_url":"https://github.com/acme/alpha/actions/runs/12/job/501",
  "steps":[
   {"name":"Set up job","status":"completed","conclusion":"success","number":1,
    "started_at":"2026-09-30T09:00:02Z","completed_at":"2026-09-30T09:00:05Z"},
   {"name":"Run go test ./...","status":"completed","conclusion":"failure","number":5,
    "started_at":"2026-09-30T09:00:41Z","completed_at":"2026-09-30T09:01:53Z"},
   {"name":"Upload coverage","status":"completed","conclusion":"skipped","number":6,"started_at":null,"completed_at":null}]}]}`

func TestWorkflowRunsUndJobs(t *testing.T) {
	var abfragen []string
	fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		abfragen = append(abfragen, r.URL.Path+"?"+r.URL.RawQuery)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/repos/acme/alpha/actions/runs":
			fmt.Fprint(w, akRuns)
		case "/repos/acme/alpha/actions/runs/12/jobs":
			fmt.Fprint(w, akJobs)
		default:
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"message":"Not Found"}`)
		}
	})
	c := ghrest.Client{Token: "t"}
	ctx := context.Background()
	runs, err := c.WorkflowRuns(ctx, "acme/alpha", 20)
	if err != nil || len(runs) != 2 {
		t.Fatalf("WorkflowRuns %+v %v", runs, err)
	}
	if abfragen[0] != "/repos/acme/alpha/actions/runs?per_page=20" {
		t.Errorf("Abfrage %q", abfragen[0])
	}
	r := runs[0]
	if r.ID != 12 || r.Name != "CI" || r.Event != "push" || r.Branch != "main" || r.HeadSHA != "3f9c2e1a7b8c9d0e" ||
		r.Betreff != "ci-runs: Betreff\n\nRumpf" || r.Nummer != 12 || r.Versuch != 1 || r.Status != "completed" ||
		r.Ergebnis != "failure" || r.Start != "2026-09-30T09:00:00Z" || r.Aktualisiert != "2026-09-30T09:02:05Z" ||
		r.URL != "https://github.com/acme/alpha/actions/runs/12" {
		t.Errorf("Run 12 %+v", r)
	}
	if l := runs[1]; l.Betreff != "Nur Titel" || l.Ergebnis != "" || l.Versuch != 3 {
		t.Errorf("Run 10 %+v (ohne head_commit gilt display_title)", l)
	}

	jobs, err := c.RunJobs(ctx, "acme/alpha", 12)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("RunJobs %+v %v", jobs, err)
	}
	if !strings.HasPrefix(abfragen[1], "/repos/acme/alpha/actions/runs/12/jobs?") || !strings.Contains(abfragen[1], "per_page=100") {
		t.Errorf("Abfrage %q", abfragen[1])
	}
	j := jobs[0]
	if j.ID != 501 || j.RunID != 12 || j.Versuch != 1 || j.Name != "test" || j.Status != "completed" || j.Ergebnis != "failure" ||
		j.Start != "2026-09-30T09:00:02Z" || j.Ende != "2026-09-30T09:01:54Z" || j.URL != "https://github.com/acme/alpha/actions/runs/12/job/501" {
		t.Errorf("Job %+v", j)
	}
	if len(j.Schritte) != 3 {
		t.Fatalf("Schritte %+v", j.Schritte)
	}
	if s := j.Schritte[1]; s.Nummer != 5 || s.Name != "Run go test ./..." || s.Status != "completed" || s.Ergebnis != "failure" ||
		s.Start != "2026-09-30T09:00:41Z" || s.Ende != "2026-09-30T09:01:53Z" {
		t.Errorf("Schritt 5 %+v", s)
	}
	if s := j.Schritte[2]; s.Ergebnis != "skipped" || s.Start != "" {
		t.Errorf("Schritt 6 %+v", s)
	}

	if _, err := c.WorkflowRuns(ctx, "acme/leer", 20); ghrest.HTTPStatus(err) != http.StatusNotFound {
		t.Errorf("404 kommt als StatusFehler zurueck: %v", err)
	}
	if _, err := c.WorkflowRuns(ctx, "../x", 20); err == nil {
		t.Error("ungueltiges Repo angenommen")
	}
}

func TestJobLogFolgtWeiterleitungUndKuerzt(t *testing.T) {
	gross := strings.Repeat("0123456789abcdef\n", 200000) + "ENDE\n" // gut 3 MB
	var pfade []string
	fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		pfade = append(pfade, r.URL.Path)
		switch r.URL.Path {
		case "/repos/acme/alpha/actions/jobs/501/logs":
			http.Redirect(w, r, "/blob/501", http.StatusFound)
		case "/blob/501":
			w.Header().Set("Content-Type", "text/plain")
			fmt.Fprint(w, "zeile eins\nzeile zwei\n")
		case "/repos/acme/alpha/actions/jobs/502/logs":
			http.Redirect(w, r, "/blob/502", http.StatusFound)
		case "/blob/502":
			fmt.Fprint(w, gross)
		default:
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"message":"Not Found"}`)
		}
	})
	c := ghrest.Client{Token: "t"}
	ctx := context.Background()
	text, gekuerzt, err := c.JobLog(ctx, "acme/alpha", 501, 2<<20)
	if err != nil || text != "zeile eins\nzeile zwei\n" || gekuerzt {
		t.Fatalf("JobLog %q %v %v", text, gekuerzt, err)
	}
	if strings.Join(pfade, ",") != "/repos/acme/alpha/actions/jobs/501/logs,/blob/501" {
		t.Errorf("Pfade %v", pfade)
	}
	text, gekuerzt, err = c.JobLog(ctx, "acme/alpha", 502, 2<<20)
	if err != nil || !gekuerzt {
		t.Fatalf("grosses Log: gekuerzt %v %v", gekuerzt, err)
	}
	if len(text) > 2<<20 || !strings.HasSuffix(text, "ENDE\n") || !strings.HasPrefix(text, "0123456789abcdef\n") {
		t.Errorf("gekuerztes Log: %d Bytes, Anfang %q, Ende %q", len(text), text[:20], text[len(text)-10:])
	}
	if _, _, err := c.JobLog(ctx, "acme/alpha", 999, 2<<20); ghrest.HTTPStatus(err) != http.StatusNotFound {
		t.Errorf("unbekannter Job: %v", err)
	}
}

func TestRunNeuStarten(t *testing.T) {
	alle := protokollGitHub(t, func(a aufruf, w http.ResponseWriter) {
		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, `{}`)
	})
	if err := (ghrest.Client{Token: "t"}).RunNeuStarten(context.Background(), "acme/alpha", 12); err != nil {
		t.Fatal(err)
	}
	if len(*alle) != 1 || (*alle)[0].Methode != http.MethodPost || (*alle)[0].Pfad != "/repos/acme/alpha/actions/runs/12/rerun-failed-jobs" {
		t.Errorf("Aufrufe %+v", *alle)
	}
	if err := (ghrest.Client{Token: "t"}).RunNeuStarten(context.Background(), "acme/alpha", 0); err == nil {
		t.Error("Run 0 angenommen")
	}
}
