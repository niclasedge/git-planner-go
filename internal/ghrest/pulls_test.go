package ghrest_test

// Pull Requests: Liste, Status, Freigeben, Mergen, Branch loeschen
// (pr-aktionen, #32).

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

func TestPullRequestsUndStatus(t *testing.T) {
	protokollGitHub(t, func(a aufruf, w http.ResponseWriter) {
		switch a.Pfad {
		case "/repos/acme/alpha/pulls":
			fmt.Fprint(w, `[{"number":9,"title":"Neun","html_url":"u9","draft":true,"node_id":"PR_9","user":{"login":"cy"},
			  "head":{"ref":"change/neun","sha":"s9","repo":{"full_name":"acme/alpha"}},"base":{"ref":"main"},"labels":[{"name":"x","color":"fff"}]}]`)
		case "/repos/acme/alpha/pulls/9":
			fmt.Fprint(w, `{"number":9,"draft":true,"node_id":"PR_9","state":"open","merged":false,"mergeable":false,"mergeable_state":"dirty",
			  "head":{"ref":"change/neun","sha":"s9","repo":{"full_name":"acme/alpha"}},"base":{"ref":"main"}}`)
		}
	})
	c := ghrest.Client{Token: "t"}
	prs, err := c.PullRequests(context.Background(), "acme/alpha")
	if err != nil || len(prs) != 1 {
		t.Fatalf("PullRequests %+v %v", prs, err)
	}
	p := prs[0]
	if p.Nummer != 9 || !p.Draft || p.Head != "change/neun" || p.HeadRepo != "acme/alpha" || p.HeadSHA != "s9" || p.Base != "main" || p.NodeID != "PR_9" || p.Autor != "cy" || len(p.Labels) != 1 {
		t.Errorf("PR %+v", p)
	}
	s, err := c.PullRequest(context.Background(), "acme/alpha", 9)
	if err != nil || s.Mergebar == nil || *s.Mergebar || s.MergeStatus != "dirty" || s.Zustand != "open" || s.Gemergt {
		t.Errorf("Status %+v %v", s, err)
	}
}

func TestPRFreigebenMergenBranchLoeschen(t *testing.T) {
	alle := protokollGitHub(t, func(a aufruf, w http.ResponseWriter) {
		switch {
		case a.Pfad == "/graphql":
			fmt.Fprint(w, `{"data":{"markPullRequestReadyForReview":{"pullRequest":{"isDraft":false}}}}`)
		case strings.HasSuffix(a.Pfad, "/merge"):
			fmt.Fprint(w, `{"merged":true,"sha":"m1"}`)
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	})
	c := ghrest.Client{Token: "t"}
	if err := c.PRFreigeben(context.Background(), "PR_9"); err != nil {
		t.Fatal(err)
	}
	if err := c.PRMergen(context.Background(), "acme/alpha", 10, "abc"); err != nil {
		t.Fatal(err)
	}
	if err := c.BranchLoeschen(context.Background(), "acme/alpha", "change/zehn"); err != nil {
		t.Fatal(err)
	}
	g, m, d := (*alle)[0], (*alle)[1], (*alle)[2]
	if g.Methode != http.MethodPost || g.Pfad != "/graphql" || !strings.Contains(fmt.Sprint(g.Rumpf["query"]), "markPullRequestReadyForReview") ||
		!strings.Contains(fmt.Sprint(g.Rumpf["variables"]), "PR_9") {
		t.Errorf("GraphQL %+v", g)
	}
	if m.Methode != http.MethodPut || m.Pfad != "/repos/acme/alpha/pulls/10/merge" || m.Rumpf["merge_method"] != "merge" || m.Rumpf["sha"] != "abc" {
		t.Errorf("Merge %+v", m)
	}
	if d.Methode != http.MethodDelete || d.Pfad != "/repos/acme/alpha/git/refs/heads/change/zehn" {
		t.Errorf("Loeschen %+v", d)
	}
}

func TestPRFreigebenMeldetGraphQLFehler(t *testing.T) {
	protokollGitHub(t, func(a aufruf, w http.ResponseWriter) {
		fmt.Fprint(w, `{"errors":[{"message":"Pull request is not a draft"}]}`)
	})
	if err := (ghrest.Client{Token: "tok-lang-genug"}).PRFreigeben(context.Background(), "PR_9"); err == nil || !strings.Contains(err.Error(), "not a draft") {
		t.Errorf("Fehler %v", err)
	}
	if err := (ghrest.Client{Token: "t"}).BranchLoeschen(context.Background(), "acme/alpha", ""); err == nil {
		t.Error("leerer Branch ohne Fehler")
	}
}
