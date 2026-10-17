package ghrest_test

// Reviews, Review-Kommentare, Thread-Zustand, Reviewer, Antworten und
// Aufloesen gegen eine Fake-GitHub-API (pr-reviews, #81).

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

const rvKommentare = `[
 {"id":11,"body":"Bitte umbenennen","user":{"login":"bo"},"path":"web/a.go","line":4,"original_line":4,"start_line":null,
  "diff_hunk":"@@ -1,4 +1,4 @@\n-a\n+b","created_at":"2026-09-26T10:01:00Z","html_url":"https://github.com/acme/alpha/pull/9#discussion_r11"},
 {"id":12,"in_reply_to_id":11,"body":"mach ich","user":{"login":"cy"},"path":"web/a.go","line":4,"original_line":4,
  "diff_hunk":"@@ -1,4 +1,4 @@\n-a\n+b","created_at":"2026-09-26T10:02:00Z","html_url":"https://github.com/acme/alpha/pull/9#discussion_r12"},
 {"id":21,"body":"alt","user":{"login":"bo"},"path":"api.go","line":null,"original_line":7,"start_line":5,
  "diff_hunk":"@@","created_at":"2026-09-26T09:00:00Z","html_url":"u21"}]`

const rvThreadsGraphQL = `{"data":{"repository":{"pullRequest":{"reviewThreads":{"nodes":[
 {"id":"PRRT_11","isResolved":false,"comments":{"nodes":[{"databaseId":11}]}},
 {"id":"PRRT_21","isResolved":true,"comments":{"nodes":[{"databaseId":21}]}},
 {"id":"PRRT_leer","isResolved":false,"comments":{"nodes":[]}}]}}}}}`

func TestReviewsUndKommentareLesen(t *testing.T) {
	alle := protokollGitHub(t, func(a aufruf, w http.ResponseWriter) {
		switch a.Pfad {
		case "/repos/acme/alpha/pulls/9/reviews":
			fmt.Fprint(w, `[{"id":1,"user":{"login":"ana"},"state":"APPROVED","submitted_at":"2026-09-26T10:00:00Z"},
			  {"id":2,"user":{"login":"bo"},"state":"CHANGES_REQUESTED","submitted_at":"2026-09-26T10:05:00Z"},
			  {"id":3,"user":null,"state":"COMMENTED","submitted_at":"2026-09-26T10:06:00Z"}]`)
		case "/repos/acme/alpha/pulls/9/comments":
			fmt.Fprint(w, rvKommentare)
		case "/graphql":
			fmt.Fprint(w, rvThreadsGraphQL)
		default:
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"message":"Not Found"}`)
		}
	})
	c := ghrest.Client{Token: "t"}
	ctx := context.Background()
	rs, err := c.Reviews(ctx, "acme/alpha", 9)
	if err != nil || len(rs) != 3 {
		t.Fatalf("Reviews %+v %v", rs, err)
	}
	if r := rs[1]; r.ID != 2 || r.Autor != "bo" || r.Zustand != "CHANGES_REQUESTED" || r.Zeit != "2026-09-26T10:05:00Z" {
		t.Errorf("Review bo %+v", r)
	}
	if rs[2].Autor != "" {
		t.Errorf("Review ohne Nutzer %+v", rs[2])
	}
	ks, err := c.ReviewKommentare(ctx, "acme/alpha", 9)
	if err != nil || len(ks) != 3 {
		t.Fatalf("ReviewKommentare %+v %v", ks, err)
	}
	if k := ks[0]; k.ID != 11 || k.AntwortAuf != 0 || k.Autor != "bo" || k.Text != "Bitte umbenennen" || k.Pfad != "web/a.go" ||
		k.Zeile != 4 || k.Veraltet || k.Diff != "@@ -1,4 +1,4 @@\n-a\n+b" || k.Zeit != "2026-09-26T10:01:00Z" ||
		k.URL != "https://github.com/acme/alpha/pull/9#discussion_r11" {
		t.Errorf("Kommentar 11 %+v", k)
	}
	if k := ks[1]; k.AntwortAuf != 11 {
		t.Errorf("Kommentar 12 %+v", k)
	}
	if k := ks[2]; !k.Veraltet || k.Zeile != 0 || k.OriginalZeile != 7 || k.StartZeile != 5 {
		t.Errorf("Kommentar 21 (veraltet) %+v", k)
	}
	z, err := c.ThreadZustaende(ctx, "acme/alpha", 9)
	if err != nil {
		t.Fatalf("ThreadZustaende: %v", err)
	}
	if len(z) != 2 || z[11].ID != "PRRT_11" || z[11].Aufgeloest || z[21].ID != "PRRT_21" || !z[21].Aufgeloest {
		t.Errorf("ThreadZustaende %+v", z)
	}
	var gq *aufruf
	for i, a := range *alle {
		if a.Pfad == "/graphql" {
			gq = &(*alle)[i]
		}
	}
	if gq == nil || !strings.Contains(fmt.Sprint(gq.Rumpf["query"]), "reviewThreads") {
		t.Fatalf("GraphQL-Anfrage %+v", gq)
	}
	v, _ := gq.Rumpf["variables"].(map[string]any)
	if v["owner"] != "acme" || v["name"] != "alpha" || fmt.Sprint(v["nummer"]) != "9" {
		t.Errorf("GraphQL-Variablen %+v", v)
	}
	if _, err := c.Reviews(ctx, "acme/../x", 9); err == nil {
		t.Error("ungueltiges Repo ohne Fehler")
	}
	if _, err := c.ReviewKommentare(ctx, "acme/alpha", 0); err == nil {
		t.Error("PR 0 ohne Fehler")
	}
}

func TestThreadZustaendeGraphQLFehler(t *testing.T) {
	protokollGitHub(t, func(a aufruf, w http.ResponseWriter) {
		fmt.Fprint(w, `{"errors":[{"message":"Resource not accessible by integration"}]}`)
	})
	if _, err := (ghrest.Client{Token: "tok-rv"}).ThreadZustaende(context.Background(), "acme/alpha", 9); err == nil ||
		!strings.Contains(err.Error(), "not accessible") {
		t.Errorf("GraphQL-Fehler nicht gemeldet: %v", err)
	}
}

func TestReviewerAntwortenAufloesen(t *testing.T) {
	alle := protokollGitHub(t, func(a aufruf, w http.ResponseWriter) {
		switch {
		case strings.HasSuffix(a.Pfad, "/replies"):
			w.WriteHeader(http.StatusCreated)
			fmt.Fprint(w, `{"id":99,"in_reply_to_id":11,"body":"erledigt","user":{"login":"cy"},"path":"web/a.go","line":4,
			  "created_at":"2026-09-26T12:00:00Z","html_url":"u99"}`)
		case a.Pfad == "/graphql":
			fmt.Fprint(w, `{"data":{}}`)
		default:
			w.WriteHeader(http.StatusCreated)
			fmt.Fprint(w, `{}`)
		}
	})
	c := ghrest.Client{Token: "t"}
	ctx := context.Background()
	if err := c.ReviewerAnfordern(ctx, "acme/alpha", 9, []string{"ana", "bo"}); err != nil {
		t.Fatalf("ReviewerAnfordern: %v", err)
	}
	if err := c.ReviewerEntfernen(ctx, "acme/alpha", 9, []string{"dd"}); err != nil {
		t.Fatalf("ReviewerEntfernen: %v", err)
	}
	k, err := c.ReviewAntworten(ctx, "acme/alpha", 9, 11, "erledigt")
	if err != nil || k.ID != 99 || k.AntwortAuf != 11 || k.Text != "erledigt" || k.Autor != "cy" {
		t.Fatalf("ReviewAntworten %+v %v", k, err)
	}
	if err := c.ThreadUmstellen(ctx, "PRRT_11", true); err != nil {
		t.Fatalf("aufloesen: %v", err)
	}
	if err := c.ThreadUmstellen(ctx, "PRRT_21", false); err != nil {
		t.Fatalf("wieder oeffnen: %v", err)
	}
	if err := c.ReviewerAnfordern(ctx, "acme/alpha", 9, nil); err == nil {
		t.Error("leere Reviewer-Liste ohne Fehler")
	}
	if _, err := c.ReviewAntworten(ctx, "acme/alpha", 9, 11, "  "); err == nil {
		t.Error("leere Antwort ohne Fehler")
	}
	if err := c.ThreadUmstellen(ctx, "", true); err == nil {
		t.Error("Thread ohne Id ohne Fehler")
	}
	a := *alle
	if len(a) != 5 {
		t.Fatalf("Aufrufe %+v", a)
	}
	if a[0].Methode != http.MethodPost || a[0].Pfad != "/repos/acme/alpha/pulls/9/requested_reviewers" || fmt.Sprint(a[0].Rumpf["reviewers"]) != "[ana bo]" {
		t.Errorf("anfordern %+v", a[0])
	}
	if a[1].Methode != http.MethodDelete || a[1].Pfad != "/repos/acme/alpha/pulls/9/requested_reviewers" || fmt.Sprint(a[1].Rumpf["reviewers"]) != "[dd]" {
		t.Errorf("entfernen %+v", a[1])
	}
	if a[2].Methode != http.MethodPost || a[2].Pfad != "/repos/acme/alpha/pulls/9/comments/11/replies" || a[2].Rumpf["body"] != "erledigt" {
		t.Errorf("antworten %+v", a[2])
	}
	for i, fall := range []struct{ mutation, id string }{{"resolveReviewThread", "PRRT_11"}, {"unresolveReviewThread", "PRRT_21"}} {
		g := a[3+i]
		q := fmt.Sprint(g.Rumpf["query"])
		v, _ := g.Rumpf["variables"].(map[string]any)
		if g.Pfad != "/graphql" || !strings.Contains(q, fall.mutation+"(") || v["id"] != fall.id {
			t.Errorf("%s: %+v", fall.mutation, g)
		}
		if fall.mutation == "resolveReviewThread" && strings.Contains(q, "unresolveReviewThread") {
			t.Errorf("aufloesen schickt unresolve: %s", q)
		}
	}
}
