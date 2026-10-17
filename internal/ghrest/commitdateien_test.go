package ghrest_test

// Die juengsten Commits eines Branches und die Dateien eines Commits
// (beleg-bilder, #105): Grundlage der "Geaenderten Bilder".

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

func TestJuengsteCommitsUndDateien(t *testing.T) {
	var abfragen []string
	fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/repos/acme/alpha/commits":
			abfragen = append(abfragen, r.URL.RawQuery)
			q := r.URL.Query()
			if q.Get("sha") != "main" || q.Get("per_page") != "30" || q.Get("path") != "" {
				w.WriteHeader(http.StatusBadRequest)
				fmt.Fprint(w, `{"message":"falsche Abfrage"}`)
				return
			}
			fmt.Fprint(w, `[
			 {"sha":"k2k2","commit":{"message":"Entwurf\n\nText","committer":{"date":"2026-09-27T09:00:00Z"}}},
			 {"sha":"k1k1","commit":{"message":"anlegen","author":{"date":"2026-09-26T08:00:00Z"}}}]`)
		case "/repos/acme/alpha/commits/k2k2":
			fmt.Fprint(w, `{"sha":"k2k2","commit":{"message":"Entwurf\n\nText","committer":{"date":"2026-09-27T09:00:00Z"}},
			 "files":[{"filename":"docs/a.png","sha":"b-a","status":"modified"},
			          {"filename":"docs/neu.svg","previous_filename":"docs/alt.svg","sha":"b-n","status":"renamed"},
			          {"filename":"docs/weg.png","sha":"0000","status":"removed"}]}`)
		default:
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"message":"Not Found"}`)
		}
	})
	c := ghrest.Client{Token: "t"}
	ctx := context.Background()

	ks, err := c.JuengsteCommits(ctx, "acme/alpha", "main")
	if err != nil {
		t.Fatalf("JuengsteCommits: %v (Abfragen %v)", err, abfragen)
	}
	if len(ks) != 2 || ks[0].SHA != "k2k2" || ks[0].Betreff != "Entwurf" || ks[0].Zeit != "2026-09-27T09:00:00Z" {
		t.Fatalf("Commits %+v", ks)
	}
	if ks[1].Zeit != "2026-09-26T08:00:00Z" {
		t.Errorf("ohne Committer-Datum soll das Autor-Datum gelten: %+v", ks[1])
	}

	cm, err := c.CommitDateien(ctx, "acme/alpha", "k2k2")
	if err != nil {
		t.Fatalf("CommitDateien: %v", err)
	}
	if cm.SHA != "k2k2" || cm.Betreff != "Entwurf" || cm.Zeit != "2026-09-27T09:00:00Z" {
		t.Errorf("Kopf %+v", cm.CommitKopf)
	}
	want := []ghrest.CommitDatei{
		{Pfad: "docs/a.png", SHA: "b-a", Status: "modified"},
		{Pfad: "docs/neu.svg", SHA: "b-n", Status: "renamed", AlterPfad: "docs/alt.svg"},
		{Pfad: "docs/weg.png", SHA: "0000", Status: "removed"},
	}
	if fmt.Sprint(cm.Dateien) != fmt.Sprint(want) {
		t.Errorf("Dateien %+v, erwartet %+v", cm.Dateien, want)
	}

	if _, err := c.CommitDateien(ctx, "acme/alpha", "../x"); err == nil {
		t.Error("ungueltiger sha ohne Fehler")
	}
	if _, err := c.JuengsteCommits(ctx, "kein-repo", "main"); err == nil {
		t.Error("ungueltiges Repo ohne Fehler")
	}
}
