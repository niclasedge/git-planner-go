package ghrest_test

// Check-Runs, Commit-Statuses und Neustart gegen eine Fake-GitHub-API
// (pr-checks, #80).

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

const ckRuns = `{"total_count":2,"check_runs":[
 {"id":101,"name":"build","status":"completed","conclusion":"failure","head_sha":"sha9",
  "started_at":"2026-09-26T10:00:00Z","completed_at":"2026-09-26T10:01:12Z",
  "html_url":"https://github.com/acme/alpha/runs/101","details_url":"https://github.com/acme/alpha/actions/runs/555/job/101",
  "app":{"slug":"github-actions"}},
 {"id":202,"name":"extern","status":"in_progress","conclusion":null,"head_sha":"sha9",
  "started_at":"2026-09-26T10:00:00Z","completed_at":null,
  "html_url":"https://github.com/acme/alpha/runs/202","details_url":"https://ci.example/202",
  "app":{"slug":"andere-app"}}]}`

func TestCheckRunsUndStatuses(t *testing.T) {
	alle := protokollGitHub(t, func(a aufruf, w http.ResponseWriter) {
		switch a.Pfad {
		case "/repos/acme/alpha/commits/sha9/check-runs":
			fmt.Fprint(w, ckRuns)
		case "/repos/acme/alpha/commits/sha9/status":
			fmt.Fprint(w, `{"state":"pending","statuses":[{"context":"ci/legacy","state":"success","description":"ok",
			  "target_url":"https://ci.example/7","created_at":"2026-09-26T09:00:00Z","updated_at":"2026-09-26T09:02:00Z"}]}`)
		case "/repos/acme/alpha/check-runs/101":
			fmt.Fprint(w, `{"id":101,"name":"build","status":"completed","conclusion":"failure","head_sha":"sha9",
			  "details_url":"https://github.com/acme/alpha/actions/runs/555/job/101","app":{"slug":"github-actions"}}`)
		default:
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"message":"Not Found"}`)
		}
	})
	c := ghrest.Client{Token: "t"}
	ctx := context.Background()
	runs, err := c.CheckRuns(ctx, "acme/alpha", "sha9")
	if err != nil || len(runs) != 2 {
		t.Fatalf("CheckRuns %+v %v", runs, err)
	}
	b := runs[0]
	if b.ID != 101 || b.Name != "build" || b.Status != "completed" || b.Conclusion != "failure" || b.HeadSHA != "sha9" ||
		b.StartedAt != "2026-09-26T10:00:00Z" || b.CompletedAt != "2026-09-26T10:01:12Z" ||
		b.HTMLURL != "https://github.com/acme/alpha/runs/101" || b.App != "github-actions" {
		t.Errorf("build %+v", b)
	}
	if e := runs[1]; e.Conclusion != "" || e.CompletedAt != "" || e.App != "andere-app" || e.DetailsURL != "https://ci.example/202" {
		t.Errorf("extern %+v", e)
	}
	st, err := c.CommitStatuses(ctx, "acme/alpha", "sha9")
	if err != nil || len(st) != 1 {
		t.Fatalf("CommitStatuses %+v %v", st, err)
	}
	if s := st[0]; s.Kontext != "ci/legacy" || s.Zustand != "success" || s.URL != "https://ci.example/7" || s.Beschreibung != "ok" ||
		s.Erstellt != "2026-09-26T09:00:00Z" || s.Aktualisiert != "2026-09-26T09:02:00Z" {
		t.Errorf("Status %+v", s)
	}
	r, err := c.CheckRunHolen(ctx, "acme/alpha", 101)
	if err != nil || r.ID != 101 || r.HeadSHA != "sha9" || r.App != "github-actions" {
		t.Errorf("CheckRunHolen %+v %v", r, err)
	}
	if _, err := c.CheckRuns(ctx, "acme/../x", "sha9"); err == nil {
		t.Error("ungueltiges Repo ohne Fehler")
	}
	if _, err := c.CheckRuns(ctx, "acme/alpha", "sha/../x"); err == nil {
		t.Error("ungueltiger SHA ohne Fehler")
	}
	for _, a := range *alle {
		if a.Methode != http.MethodGet {
			t.Errorf("Lesen hat geschrieben: %+v", a)
		}
	}
}

func TestActionsLauf(t *testing.T) {
	for _, fall := range []struct {
		run ghrest.CheckRun
		id  int64
	}{
		{ghrest.CheckRun{App: "github-actions", DetailsURL: "https://github.com/acme/alpha/actions/runs/555/job/101"}, 555},
		{ghrest.CheckRun{App: "github-actions", HTMLURL: "https://github.com/acme/alpha/actions/runs/556/job/9"}, 556},
		{ghrest.CheckRun{App: "andere-app", DetailsURL: "https://github.com/acme/alpha/actions/runs/557/job/1"}, 0},
		{ghrest.CheckRun{App: "github-actions", DetailsURL: "https://ci.example/1"}, 0},
	} {
		if got := ghrest.ActionsLauf(fall.run); got != fall.id {
			t.Errorf("ActionsLauf(%+v) = %d, erwartet %d", fall.run, got, fall.id)
		}
	}
}

func TestPruefungNeuStarten(t *testing.T) {
	alle := protokollGitHub(t, func(a aufruf, w http.ResponseWriter) {
		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, `{}`)
	})
	c := ghrest.Client{Token: "t"}
	ctx := context.Background()
	weg, err := c.PruefungNeuStarten(ctx, "acme/alpha", ghrest.CheckRun{ID: 101, App: "github-actions",
		DetailsURL: "https://github.com/acme/alpha/actions/runs/555/job/101"})
	if err != nil || weg != "actions" {
		t.Fatalf("Actions: %q %v", weg, err)
	}
	weg, err = c.PruefungNeuStarten(ctx, "acme/alpha", ghrest.CheckRun{ID: 202, App: "andere-app", DetailsURL: "https://ci.example/202"})
	if err != nil || weg != "check-run" {
		t.Fatalf("anderer Check-Run: %q %v", weg, err)
	}
	if _, err := c.PruefungNeuStarten(ctx, "acme/alpha", ghrest.CheckRun{}); err == nil {
		t.Error("Check-Run ohne Id ohne Fehler")
	}
	if len(*alle) != 2 {
		t.Fatalf("Aufrufe %+v", *alle)
	}
	if a := (*alle)[0]; a.Methode != http.MethodPost || a.Pfad != "/repos/acme/alpha/actions/runs/555/rerun-failed-jobs" {
		t.Errorf("Actions-Aufruf %+v", a)
	}
	if a := (*alle)[1]; a.Methode != http.MethodPost || a.Pfad != "/repos/acme/alpha/check-runs/202/rerequest" {
		t.Errorf("Check-Run-Aufruf %+v", a)
	}
}
