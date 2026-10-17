package appctx

// Letzte Aenderung je Eintrag der Reihe (naechste-geaendert, #118): der
// juengste Zeitpunkt aus Issue, offenem PR und Branch, samt Quelle und dem
// Text nach der Regel von "Zuletzt erledigt" (ZeitpunktText, #116).

import (
	"testing"
	"time"

	"github.com/niclasedge/git-planner-go/internal/collectors"
	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

var ngJetzt = time.Date(2026, 9, 28, 15, 0, 0, 0, time.FixedZone("UTC+2", 2*3600))

func ngOffen(nr int, aktualisiert string) ghrest.Issue {
	return ghrest.Issue{Nummer: nr, Titel: "Issue", URL: "u", Zustand: "open", Aktualisiert: aktualisiert}
}

func ngPR(nr int, head, aktualisiert string) ghrest.PR {
	return ghrest.PR{Nummer: nr, Titel: "PR", URL: "p", Head: head, Zustand: "open", Aktualisiert: aktualisiert}
}

func ngReihe(t *testing.T, w RepoArbeit) map[int]ArbeitsEintrag {
	t.Helper()
	out := map[int]ArbeitsEintrag{}
	for _, e := range w.Reihe {
		out[e.Nummer] = e
	}
	return out
}

func ngGeaendert(t *testing.T, e ArbeitsEintrag) ZuletztGeaendert {
	t.Helper()
	if e.ZuletztGeaendert == nil {
		t.Fatalf("#%d: kein zuletzt_geaendert", e.Nummer)
	}
	return *e.ZuletztGeaendert
}

func TestGeaendertPRJuengerAlsIssue(t *testing.T) {
	w := RepoArbeitBauen(ArbeitEingabe{Repo: "acme/alpha", Jetzt: ngJetzt,
		Offen:   []ghrest.Issue{ngOffen(10, "2026-09-20T08:00:00Z")},
		Changes: []collectors.OpenspecChange{{Kennung: "zehn", Ordner: "openspec/changes/zehn", Issue: 10}},
		PRs:     []ghrest.PR{ngPR(7, "change/zehn", "2026-09-28T12:32:00Z")},
	})
	g := ngGeaendert(t, ngReihe(t, w)[10])
	if g.Quelle != "pr" || g.PR != 7 {
		t.Errorf("quelle %q pr %d, erwartet pr #7", g.Quelle, g.PR)
	}
	if g.Zeit != "2026-09-28T12:32:00Z" || g.Text != "heute 14:32" || g.Voll != "28.09.2026 14:32" {
		t.Errorf("zeit %q text %q voll %q", g.Zeit, g.Text, g.Voll)
	}
	if g.IssueText != "20. Sept." || g.PRText != "heute 14:32" || g.PRVoll != "28.09.2026 14:32" {
		t.Errorf("issue_text %q pr_text %q pr_voll %q", g.IssueText, g.PRText, g.PRVoll)
	}
	if g.IssueVoll != "20.09.2026 10:00" {
		t.Errorf("issue_voll %q", g.IssueVoll)
	}
}

func TestGeaendertIssueJuengerAlsPR(t *testing.T) {
	w := RepoArbeitBauen(ArbeitEingabe{Repo: "acme/alpha", Jetzt: ngJetzt,
		Offen:        []ghrest.Issue{ngOffen(11, "2026-09-27T09:00:00Z")},
		Branches:     []ghrest.Branch{{Name: "11-elf", SHA: "aaaaaaaa"}},
		BranchKoepfe: map[string]BranchKopf{"11-elf": {Zeit: "2026-09-27T20:00:00Z"}},
		PRs:          []ghrest.PR{ngPR(8, "11-elf", "2026-09-25T09:00:00Z")},
	})
	e := ngReihe(t, w)[11]
	if len(e.Branches) != 1 || e.Branches[0].PR == nil || e.Branches[0].PR.Nummer != 8 {
		t.Fatalf("Branch mit PR #8 erwartet: %+v", e.Branches)
	}
	g := ngGeaendert(t, e)
	if g.Quelle != "issue" || g.Text != "27. Sept." || g.Zeit != "2026-09-27T09:00:00Z" {
		t.Errorf("quelle %q zeit %q text %q, erwartet issue 27. Sept. (der Branch zaehlt neben einem PR nicht)", g.Quelle, g.Zeit, g.Text)
	}
	if g.PR != 8 || g.PRText != "25. Sept." {
		t.Errorf("pr %d pr_text %q, erwartet #8 25. Sept.", g.PR, g.PRText)
	}
}

func TestGeaendertOhnePRGiltBranch(t *testing.T) {
	w := RepoArbeitBauen(ArbeitEingabe{Repo: "acme/alpha", Jetzt: ngJetzt,
		Offen:    []ghrest.Issue{ngOffen(12, "2026-09-10T09:00:00Z")},
		Branches: []ghrest.Branch{{Name: "12-a", SHA: "a"}, {Name: "12-b", SHA: "b"}},
		BranchKoepfe: map[string]BranchKopf{
			"12-a": {Zeit: "2026-09-26T09:00:00Z"},
			"12-b": {Zeit: "2026-09-27T10:00:00Z"},
		},
		PRs: []ghrest.PR{},
	})
	g := ngGeaendert(t, ngReihe(t, w)[12])
	if g.Quelle != "branch" || g.Zeit != "2026-09-27T10:00:00Z" || g.Text != "27. Sept." {
		t.Errorf("quelle %q zeit %q text %q, erwartet branch 2026-09-27T10:00:00Z", g.Quelle, g.Zeit, g.Text)
	}
	if g.PR != 0 || g.PRText != "" {
		t.Errorf("pr %d pr_text %q, erwartet keinen PR", g.PR, g.PRText)
	}
}

func TestGeaendertOhneQuelle(t *testing.T) {
	w := RepoArbeitBauen(ArbeitEingabe{Repo: "acme/alpha", Jetzt: ngJetzt,
		Offen: []ghrest.Issue{ngOffen(13, "")}, PRs: []ghrest.PR{}})
	if g := ngReihe(t, w)[13].ZuletztGeaendert; g != nil {
		t.Errorf("zuletzt_geaendert %+v, erwartet keins", *g)
	}
}

func TestGeaendertHeuteGegenGestern(t *testing.T) {
	w := RepoArbeitBauen(ArbeitEingabe{Repo: "acme/alpha", Jetzt: ngJetzt, PRs: []ghrest.PR{},
		Offen: []ghrest.Issue{ngOffen(14, "2026-09-27T22:05:00Z"), ngOffen(15, "2026-09-27T21:55:00Z")}})
	r := ngReihe(t, w)
	if g := ngGeaendert(t, r[14]); g.Text != "heute 00:05" || g.Quelle != "issue" {
		t.Errorf("#14 text %q quelle %q, erwartet heute 00:05 (Tagesgrenze der Zone)", g.Text, g.Quelle)
	}
	if g := ngGeaendert(t, r[15]); g.Text != "27. Sept." || g.Voll != "27.09.2026 23:55" {
		t.Errorf("#15 text %q voll %q", g.Text, g.Voll)
	}
}

func TestGeaendertAendertDieFolgeNicht(t *testing.T) {
	// #21 wurde zuletzt geaendert, #20 vor Wochen; die Folge bleibt nach Nummer.
	w := RepoArbeitBauen(ArbeitEingabe{Repo: "acme/alpha", Jetzt: ngJetzt, PRs: []ghrest.PR{},
		Offen: []ghrest.Issue{ngOffen(21, "2026-09-28T12:00:00Z"), ngOffen(20, "2026-08-01T12:00:00Z")}})
	if len(w.Reihe) != 2 || w.Reihe[0].Nummer != 20 || w.Reihe[1].Nummer != 21 {
		t.Errorf("Folge %v, erwartet #20 vor #21", []int{w.Reihe[0].Nummer, w.Reihe[1].Nummer})
	}
}
