package ghrest_test

// Merge-Optionen, Auto-Merge, Entwurf, geschlossene PRs, Wiederoeffnen und
// Milestones gegen eine Fake-GitHub-API (pr-merge-optionen, #82).

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

func schreibende(alle []anlegenAufruf) []anlegenAufruf {
	var out []anlegenAufruf
	for _, a := range alle {
		if a.Methode != http.MethodGet {
			out = append(out, a)
		}
	}
	return out
}

func TestMergeErlaubtHolen(t *testing.T) {
	anlegenGitHub(t, func(a anlegenAufruf, w http.ResponseWriter) {
		switch a.Pfad {
		case "/repos/acme/alpha":
			fmt.Fprint(w, `{"full_name":"acme/alpha","allow_merge_commit":false,"allow_squash_merge":true,"allow_rebase_merge":true}`)
		case "/repos/acme/ohne":
			// Ohne Push-Recht laesst GitHub die Felder weg.
			fmt.Fprint(w, `{"full_name":"acme/ohne"}`)
		default:
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"message":"Not Found"}`)
		}
	})
	c := ghrest.Client{Token: "tok-merge"}
	e, err := c.MergeErlaubtHolen(context.Background(), "acme/alpha")
	if err != nil || e.Merge || !e.Squash || !e.Rebase {
		t.Errorf("acme/alpha %+v %v", e, err)
	}
	e, err = c.MergeErlaubtHolen(context.Background(), "acme/ohne")
	if err != nil || !e.Merge || !e.Squash || !e.Rebase {
		t.Errorf("ohne Felder %+v %v, erwartet alles erlaubt (GitHub entscheidet)", e, err)
	}
	if _, err := c.MergeErlaubtHolen(context.Background(), "acme/weg"); err == nil {
		t.Error("404 ohne Fehler")
	}
}

func TestPRMergenMitMethodeUndText(t *testing.T) {
	alle := anlegenGitHub(t, func(a anlegenAufruf, w http.ResponseWriter) { fmt.Fprint(w, `{"merged":true}`) })
	c := ghrest.Client{Token: "tok-merge"}
	if err := c.PRMergenMit(context.Background(), "acme/alpha", 10, "abc", ghrest.MergeWahl{Methode: "squash", Titel: "Zehn (#10)", Text: "Rumpf"}); err != nil {
		t.Fatal(err)
	}
	if err := c.PRMergenMit(context.Background(), "acme/alpha", 10, "abc", ghrest.MergeWahl{}); err != nil {
		t.Fatal(err)
	}
	if err := c.PRMergenMit(context.Background(), "acme/alpha", 10, "abc", ghrest.MergeWahl{Methode: "fast"}); err == nil {
		t.Error("unbekannte Methode ohne Fehler")
	}
	s := schreibende(alle())
	if len(s) != 2 {
		t.Fatalf("Aufrufe %+v, erwartet zwei", s)
	}
	eins, zwei := s[0], s[1]
	if eins.Methode != http.MethodPut || eins.Pfad != "/repos/acme/alpha/pulls/10/merge" || eins.Rumpf["merge_method"] != "squash" ||
		eins.Rumpf["commit_title"] != "Zehn (#10)" || eins.Rumpf["commit_message"] != "Rumpf" || eins.Rumpf["sha"] != "abc" {
		t.Errorf("squash %+v", eins)
	}
	if zwei.Rumpf["merge_method"] != "merge" {
		t.Errorf("ohne Methode %+v, erwartet merge", zwei)
	}
	if _, ok := zwei.Rumpf["commit_title"]; ok {
		t.Errorf("ohne Titel trotzdem commit_title gesendet: %+v", zwei)
	}
}

func TestPRMutationen(t *testing.T) {
	alle := anlegenGitHub(t, func(a anlegenAufruf, w http.ResponseWriter) {
		q := fmt.Sprint(a.Rumpf["query"])
		if strings.Contains(q, "kaputt") || strings.Contains(fmt.Sprint(a.Rumpf["variables"]), "PR_kaputt") {
			fmt.Fprint(w, `{"errors":[{"message":"Pull request is not in the correct state"}]}`)
			return
		}
		fmt.Fprint(w, `{"data":{}}`)
	})
	c := ghrest.Client{Token: "tok-merge"}
	ctx := context.Background()
	if err := c.AutoMergeAn(ctx, "PR_10", ghrest.MergeWahl{Methode: "rebase", Titel: "T", Text: "X"}); err != nil {
		t.Fatal(err)
	}
	if err := c.AutoMergeAus(ctx, "PR_11"); err != nil {
		t.Fatal(err)
	}
	if err := c.PRZuEntwurf(ctx, "PR_12"); err != nil {
		t.Fatal(err)
	}
	if err := c.PRZuEntwurf(ctx, "PR_kaputt"); err == nil || !strings.Contains(err.Error(), "not in the correct state") {
		t.Errorf("GraphQL-Fehler nicht gemeldet: %v", err)
	}
	if err := c.AutoMergeAn(ctx, "", ghrest.MergeWahl{Methode: "merge"}); err == nil {
		t.Error("ohne node_id ohne Fehler")
	}
	if err := c.AutoMergeAn(ctx, "PR_10", ghrest.MergeWahl{Methode: "fast"}); err == nil {
		t.Error("unbekannte Methode ohne Fehler")
	}
	s := schreibende(alle())
	if len(s) != 4 {
		t.Fatalf("Aufrufe %+v, erwartet vier", s)
	}
	an := fmt.Sprint(s[0].Rumpf)
	if s[0].Pfad != "/graphql" || !strings.Contains(an, "enablePullRequestAutoMerge") || !strings.Contains(an, "PR_10") ||
		!strings.Contains(an, "REBASE") || !strings.Contains(an, "commitHeadline") {
		t.Errorf("Auto-Merge an %s", an)
	}
	if aus := fmt.Sprint(s[1].Rumpf); !strings.Contains(aus, "disablePullRequestAutoMerge") || !strings.Contains(aus, "PR_11") {
		t.Errorf("Auto-Merge aus %s", aus)
	}
	if ent := fmt.Sprint(s[2].Rumpf); !strings.Contains(ent, "convertPullRequestToDraft") || !strings.Contains(ent, "PR_12") {
		t.Errorf("zu Entwurf %s", ent)
	}
}

func TestGeschlossenePRsUndAutoMerge(t *testing.T) {
	alle := anlegenGitHub(t, func(a anlegenAufruf, w http.ResponseWriter) {
		switch {
		case a.Pfad == "/repos/acme/alpha/pulls" && strings.Contains(a.Query, "state=closed"):
			fmt.Fprint(w, `[{"number":7,"title":"Sieben","state":"closed","merged_at":"2026-09-20T10:00:00Z","closed_at":"2026-09-20T10:00:00Z",
			  "head":{"ref":"change/sieben","repo":{"full_name":"acme/alpha"}},"base":{"ref":"main"}},
			 {"number":8,"title":"Acht","state":"closed","merged_at":null,"closed_at":"2026-09-24T09:00:00Z",
			  "head":{"ref":"change/acht","repo":{"full_name":"acme/alpha"}},"base":{"ref":"main"}}]`)
		case a.Pfad == "/repos/acme/alpha/pulls":
			fmt.Fprint(w, `[{"number":11,"title":"Elf","state":"open","auto_merge":{"merge_method":"squash","enabled_by":{"login":"cy"}},
			  "assignees":[{"login":"ana"}],"milestone":{"number":3,"title":"Herbst"},
			  "head":{"ref":"change/elf","repo":{"full_name":"acme/alpha"}},"base":{"ref":"main"}},
			 {"number":12,"title":"Zwoelf","state":"open","auto_merge":null,"head":{"ref":"change/zwoelf"},"base":{"ref":"main"}}]`)
		case a.Pfad == "/repos/acme/alpha/pulls/8" && a.Methode == http.MethodPatch:
			fmt.Fprint(w, `{"number":8,"state":"open"}`)
		default:
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"message":"Not Found"}`)
		}
	})
	c := ghrest.Client{Token: "tok-merge"}
	ctx := context.Background()
	zu, err := c.GeschlossenePRs(ctx, "acme/alpha")
	if err != nil || len(zu) != 2 {
		t.Fatalf("GeschlossenePRs %+v %v", zu, err)
	}
	if !zu[0].Gemergt || zu[0].GemergtAm != "2026-09-20T10:00:00Z" || zu[1].Gemergt || zu[1].GeschlossenAm != "2026-09-24T09:00:00Z" {
		t.Errorf("gemergt/geschlossen %+v", zu)
	}
	var abfrage string
	for _, a := range alle() {
		if strings.Contains(a.Query, "state=closed") {
			abfrage = a.Query
		}
	}
	for _, teil := range []string{"state=closed", "sort=updated", "direction=desc", "per_page=30"} {
		if !strings.Contains(abfrage, teil) {
			t.Errorf("Abfrage %q ohne %s", abfrage, teil)
		}
	}

	offen, err := c.PullRequests(ctx, "acme/alpha")
	if err != nil || len(offen) != 2 {
		t.Fatalf("PullRequests %+v %v", offen, err)
	}
	if offen[0].AutoMerge == nil || offen[0].AutoMerge.Methode != "squash" || offen[0].AutoMerge.Von != "cy" || offen[1].AutoMerge != nil {
		t.Errorf("auto_merge %+v / %+v", offen[0].AutoMerge, offen[1].AutoMerge)
	}
	if strings.Join(offen[0].Zustaendig, ",") != "ana" || offen[0].Meilenstein == nil || offen[0].Meilenstein.Nummer != 3 || offen[0].Meilenstein.Titel != "Herbst" {
		t.Errorf("Zustaendige/Milestone %+v %+v", offen[0].Zustaendig, offen[0].Meilenstein)
	}

	if err := c.PRWiederOeffnen(ctx, "acme/alpha", 8); err != nil {
		t.Fatal(err)
	}
	s := schreibende(alle())
	if len(s) != 1 || s[0].Methode != http.MethodPatch || s[0].Pfad != "/repos/acme/alpha/pulls/8" || s[0].Rumpf["state"] != "open" {
		t.Errorf("Wiederoeffnen %+v", s)
	}
}

func TestMeilensteineUndIssueMilestone(t *testing.T) {
	alle := anlegenGitHub(t, func(a anlegenAufruf, w http.ResponseWriter) {
		switch {
		case a.Pfad == "/repos/acme/alpha/milestones":
			fmt.Fprint(w, `[{"number":3,"title":"Herbst","state":"open"},{"number":4,"title":"Winter","state":"open"}]`)
		case a.Pfad == "/repos/acme/alpha/issues/31" && a.Methode == http.MethodGet:
			fmt.Fprint(w, `{"number":31,"title":"Einunddreissig","state":"open","milestone":{"number":3,"title":"Herbst"}}`)
		case strings.HasPrefix(a.Pfad, "/repos/acme/alpha/issues/") && a.Methode == http.MethodPatch:
			fmt.Fprint(w, `{"number":31,"title":"Einunddreissig","state":"open","milestone":null}`)
		default:
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"message":"Not Found"}`)
		}
	})
	c := ghrest.Client{Token: "tok-merge"}
	ctx := context.Background()
	ms, err := c.Meilensteine(ctx, "acme/alpha")
	if err != nil || len(ms) != 2 || ms[0].Nummer != 3 || ms[0].Titel != "Herbst" {
		t.Fatalf("Meilensteine %+v %v", ms, err)
	}
	var abfrage string
	for _, a := range alle() {
		if a.Pfad == "/repos/acme/alpha/milestones" {
			abfrage = a.Query
		}
	}
	if !strings.Contains(abfrage, "state=open") {
		t.Errorf("Milestones ohne state=open: %q", abfrage)
	}
	i, err := c.Issue(ctx, "acme/alpha", 31)
	if err != nil || i.Meilenstein == nil || i.Meilenstein.Nummer != 3 {
		t.Errorf("Issue-Milestone %+v %v", i.Meilenstein, err)
	}
	null, drei := 0, 3
	if _, err := c.IssueAendern(ctx, "acme/alpha", 31, ghrest.IssueAenderung{Meilenstein: &null}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.IssueAendern(ctx, "acme/alpha", 10, ghrest.IssueAenderung{Meilenstein: &drei, Labels: &[]string{"bug"}}); err != nil {
		t.Fatal(err)
	}
	s := schreibende(alle())
	if len(s) != 2 {
		t.Fatalf("Aufrufe %+v", s)
	}
	if v, ok := s[0].Rumpf["milestone"]; !ok || v != nil {
		t.Errorf("Milestone entfernen %+v, erwartet milestone: null", s[0].Rumpf)
	}
	if fmt.Sprint(s[1].Rumpf["milestone"]) != "3" || fmt.Sprint(s[1].Rumpf["labels"]) != "[bug]" {
		t.Errorf("Milestone setzen %+v", s[1].Rumpf)
	}
}
