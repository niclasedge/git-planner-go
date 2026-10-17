package ghrest_test

// Commits je Pfad und der Patch einer Datei in einem Commit
// (aufgaben-zeitleiste, #55).

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

func TestCommitsFuerPfadUndPatch(t *testing.T) {
	var abfragen []string
	fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/repos/acme/alpha/commits":
			abfragen = append(abfragen, r.URL.RawQuery)
			q := r.URL.Query()
			if q.Get("sha") != "s-vier" || q.Get("path") != "openspec/changes/vier/tasks.md" || q.Get("per_page") != "30" {
				w.WriteHeader(http.StatusBadRequest)
				fmt.Fprint(w, `{"message":"falsche Abfrage"}`)
				return
			}
			fmt.Fprint(w, `[
			 {"sha":"c2c2c2c2","commit":{"message":"b abhaken\n\nText","author":{"date":"2026-09-20T08:00:00Z"},"committer":{"date":"2026-09-21T09:00:00Z"}}},
			 {"sha":"c1c1c1c1","commit":{"message":"anlegen","author":{"date":"2026-09-19T08:00:00Z"},"committer":{"date":""}}}]`)
		case "/repos/acme/alpha/commits/c2c2c2c2":
			fmt.Fprint(w, `{"sha":"c2c2c2c2","commit":{"message":"b abhaken","committer":{"date":"2026-09-21T09:00:00Z"}},
			 "files":[{"filename":"README.md","patch":"@@ -1 +1 @@\n-x\n+y"},
			          {"filename":"openspec/changes/vier/tasks.md","patch":"@@ -2 +2 @@\n-- [ ] b\n+- [x] b"}]}`)
		case "/repos/acme/alpha/commits/c3c3c3c3":
			fmt.Fprint(w, `{"sha":"c3c3c3c3","commit":{"message":"archivieren","committer":{"date":"2026-09-22T09:00:00Z"}},
			 "files":[{"filename":"openspec/changes/archive/2026-09-22-vier/tasks.md","previous_filename":"openspec/changes/vier/tasks.md","status":"renamed"}]}`)
		default:
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"message":"Not Found"}`)
		}
	})
	c := ghrest.Client{Token: "t"}
	ctx := context.Background()

	ks, err := c.CommitsFuerPfad(ctx, "acme/alpha", "s-vier", "openspec/changes/vier/tasks.md")
	if err != nil {
		t.Fatalf("CommitsFuerPfad: %v (Abfragen %v)", err, abfragen)
	}
	if len(ks) != 2 || ks[0].SHA != "c2c2c2c2" || ks[0].Zeit != "2026-09-21T09:00:00Z" || ks[0].Betreff != "b abhaken" {
		t.Fatalf("Commits %+v", ks)
	}
	if ks[1].Zeit != "2026-09-19T08:00:00Z" {
		t.Errorf("ohne Committer-Datum soll das Autor-Datum gelten: %+v", ks[1])
	}

	k, patch, err := c.CommitPatch(ctx, "acme/alpha", "c2c2c2c2", "openspec/changes/vier/tasks.md")
	if err != nil {
		t.Fatalf("CommitPatch: %v", err)
	}
	if k.SHA != "c2c2c2c2" || k.Betreff != "b abhaken" || patch != "@@ -2 +2 @@\n-- [ ] b\n+- [x] b" {
		t.Errorf("CommitPatch %+v %q", k, patch)
	}
	// Eine umbenannte Datei ohne Patch wird ueber den alten Namen gefunden
	// und liefert einen leeren Patch, keinen Fehler.
	if _, patch, err := c.CommitPatch(ctx, "acme/alpha", "c3c3c3c3", "openspec/changes/vier/tasks.md"); err != nil || patch != "" {
		t.Errorf("CommitPatch umbenannt %q %v", patch, err)
	}

	if _, err := c.CommitsFuerPfad(ctx, "kein-repo", "s", "p"); err == nil {
		t.Errorf("CommitsFuerPfad mit ungueltigem Repo ohne Fehler")
	}
	if _, _, err := c.CommitPatch(ctx, "acme/alpha", "../x", "p"); err == nil {
		t.Errorf("CommitPatch mit ungueltigem sha ohne Fehler")
	}
}
