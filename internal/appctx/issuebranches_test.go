package appctx

// Verknuepfte Branches je Issue und das Flag aktiv (issue-branches, #50).
// Zuordnung, Reihenfolge und Flag werden hier ohne Browser geprueft.

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/niclasedge/git-planner-go/internal/collectors"
	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

var branchJetzt = time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)

func vor(d time.Duration) string { return branchJetzt.Add(-d).Format(time.RFC3339) }

func eintragNr(t *testing.T, a RepoArbeit, nr int) ArbeitsEintrag {
	t.Helper()
	for _, e := range a.Reihe {
		if e.Nummer == nr {
			return e
		}
	}
	t.Fatalf("#%d fehlt in der Reihe %s", nr, nummern(a.Reihe))
	return ArbeitsEintrag{}
}

// branchText schreibt je Branch "name[gruende]".
func branchText(bs []IssueBranch) string {
	teile := []string{}
	for _, b := range bs {
		teile = append(teile, b.Name+"["+strings.Join(b.Gruende, "+")+"]")
	}
	return strings.Join(teile, ",")
}

func zweig(name string) ghrest.Branch {
	return ghrest.Branch{Name: name, SHA: fmt.Sprintf("%040x", len(name)*7919+int(name[len(name)-1]))}
}

func TestIssueBranchesZuordnung(t *testing.T) {
	e := ArbeitEingabe{
		Repo:     "acme/alpha",
		Standard: "main",
		Jetzt:    branchJetzt,
		Offen: []ghrest.Issue{
			{Nummer: 4, Titel: "vier"},
			{Nummer: 7, Titel: "sieben"},
			{Nummer: 42, Titel: "zweiundvierzig"},
		},
		Changes: []collectors.OpenspecChange{{Kennung: "vier", Issue: 4, Ref: "change/vier", RemoteBranch: true}},
		Branches: []ghrest.Branch{
			zweig("main"), zweig("change/vier"), zweig("claude/42-branch-marke"), zweig("fix-7"),
			zweig("feature/login"), zweig("release-v42"),
		},
		PRs: []ghrest.PR{{Nummer: 5, Titel: "login", URL: "https://github.com/acme/alpha/pull/5", Head: "feature/login",
			HeadRepo: "acme/alpha", Text: "Etwas Text.\n\nCloses #7"}},
		BranchKoepfe: map[string]BranchKopf{
			"feature/login": {Zeit: vor(time.Hour), Betreff: "login"},
			"fix-7":         {Zeit: vor(2 * time.Hour), Betreff: "fix"},
		},
	}
	a := RepoArbeitBauen(e)
	if got := branchText(eintragNr(t, a, 4).Branches); got != "change/vier[change]" {
		t.Errorf("#4 Branches %s, erwartet change/vier[change]", got)
	}
	if got := branchText(eintragNr(t, a, 42).Branches); got != "claude/42-branch-marke[name]" {
		t.Errorf("#42 Branches %s, erwartet claude/42-branch-marke[name]", got)
	}
	sieben := eintragNr(t, a, 7)
	if got := branchText(sieben.Branches); got != "feature/login[pr],fix-7[name]" {
		t.Errorf("#7 Branches %s, erwartet feature/login[pr],fix-7[name]", got)
	}
	if len(sieben.Branches) > 0 {
		if p := sieben.Branches[0].PR; p == nil || p.Nummer != 5 || p.URL == "" {
			t.Errorf("#7 feature/login ohne PR #5: %+v", p)
		}
	}
	if got := VerknuepfteBranches(e); strings.Join(got, ",") != "change/vier,claude/42-branch-marke,feature/login,fix-7" {
		t.Errorf("angefragte Branches %v", got)
	}
}

func TestIssueBranchesPRTitelUndDoppelt(t *testing.T) {
	a := RepoArbeitBauen(ArbeitEingabe{
		Repo:     "acme/alpha",
		Standard: "main",
		Jetzt:    branchJetzt,
		Offen:    []ghrest.Issue{{Nummer: 12, Titel: "zwoelf"}},
		Branches: []ghrest.Branch{zweig("main"), zweig("issue-12")},
		PRs: []ghrest.PR{{Nummer: 13, Titel: "branch-marke (#12)", URL: "https://github.com/acme/alpha/pull/13",
			Head: "issue-12", HeadRepo: "acme/alpha", Draft: true}},
	})
	e := eintragNr(t, a, 12)
	if got := branchText(e.Branches); got != "issue-12[name+pr]" {
		t.Fatalf("#12 Branches %s, erwartet issue-12[name+pr]", got)
	}
	p := e.Branches[0].PR
	if p == nil || p.Nummer != 13 || p.Titel != "branch-marke (#12)" || p.URL == "" || !p.Draft {
		t.Errorf("#12 PR %+v", p)
	}
}

func TestIssueBranchesKeineFremdenZahlen(t *testing.T) {
	e := ArbeitEingabe{
		Repo:     "acme/alpha",
		Standard: "main",
		Jetzt:    branchJetzt,
		Offen:    []ghrest.Issue{{Nummer: 1, Titel: "eins"}, {Nummer: 2, Titel: "zwei"}},
		Branches: []ghrest.Branch{zweig("main"), zweig("v1"), zweig("release-10"), zweig("11-x"), zweig("master")},
		// Ein PR vom Default-Branch zaehlt auch nicht.
		PRs: []ghrest.PR{{Nummer: 3, Titel: "x (#2)", Head: "master", HeadRepo: "acme/alpha"}},
	}
	a := RepoArbeitBauen(e)
	for _, nr := range []int{1, 2} {
		ein := eintragNr(t, a, nr)
		if ein.Branches == nil || len(ein.Branches) != 0 || ein.Aktiv {
			t.Errorf("#%d Branches %#v aktiv=%v, erwartet leer", nr, ein.Branches, ein.Aktiv)
		}
	}
	b, _ := json.Marshal(eintragNr(t, a, 1))
	if !strings.Contains(string(b), `"branches":[]`) || !strings.Contains(string(b), `"aktiv":false`) {
		t.Errorf("JSON %s ohne branches:[] und aktiv:false", b)
	}
	if got := VerknuepfteBranches(e); len(got) != 0 {
		t.Errorf("angefragte Branches %v, erwartet keine", got)
	}
}

func TestIssueBranchesKopfUndReihenfolge(t *testing.T) {
	neu := ghrest.Branch{Name: "claude/42-neu", SHA: "abcdef1234567"}
	a := RepoArbeitBauen(ArbeitEingabe{
		Repo:     "acme/alpha",
		Standard: "main",
		Jetzt:    branchJetzt,
		Offen:    []ghrest.Issue{{Nummer: 42, Titel: "zweiundvierzig"}},
		Branches: []ghrest.Branch{zweig("main"), zweig("claude/42-alt"), neu, zweig("claude/42-ohne-kopf")},
		BranchKoepfe: map[string]BranchKopf{
			"claude/42-alt": {Zeit: vor(5 * 24 * time.Hour), Betreff: "alt", Voraus: 1, Zurueck: 9},
			"claude/42-neu": {Zeit: vor(time.Hour), Betreff: "Marke zeichnen\n\nzweite Zeile", Voraus: 3, Zurueck: 0},
		},
	})
	bs := eintragNr(t, a, 42).Branches
	if got := branchText(bs); got != "claude/42-neu[name],claude/42-alt[name],claude/42-ohne-kopf[name]" {
		t.Fatalf("Reihenfolge %s", got)
	}
	n := bs[0]
	if n.SHA != "abcdef1234567" || n.KurzSHA != "abcdef1" || n.Betreff != "Marke zeichnen" || n.Voraus != 3 || n.Zurueck != 0 || n.Zeit != vor(time.Hour) {
		t.Errorf("claude/42-neu %+v", n)
	}
	if o := bs[1]; o.Voraus != 1 || o.Zurueck != 9 || o.Betreff != "alt" {
		t.Errorf("claude/42-alt %+v", o)
	}
	if o := bs[2]; o.Aktiv || o.Zeit != "" {
		t.Errorf("claude/42-ohne-kopf %+v", o)
	}
}

func TestIssueBranchesAktiv(t *testing.T) {
	e := ArbeitEingabe{
		Repo:     "acme/alpha",
		Standard: "main",
		Jetzt:    branchJetzt,
		Offen:    []ghrest.Issue{{Nummer: 42, Titel: "a"}, {Nummer: 43, Titel: "b"}},
		Branches: []ghrest.Branch{zweig("main"), zweig("claude/42-x"), zweig("claude/43-y")},
		BranchKoepfe: map[string]BranchKopf{
			"claude/42-x": {Zeit: vor(time.Hour)},
			"claude/43-y": {Zeit: vor(4 * 24 * time.Hour)},
		},
	}
	a := RepoArbeitBauen(e)
	z, d := eintragNr(t, a, 42), eintragNr(t, a, 43)
	if !z.Aktiv || len(z.Branches) != 1 || !z.Branches[0].Aktiv {
		t.Errorf("#42 nicht aktiv: %+v", z)
	}
	if d.Aktiv || len(d.Branches) != 1 || d.Branches[0].Aktiv {
		t.Errorf("#43 aktiv mit 4 Tage altem Commit: %+v", d)
	}
	if BranchAktivFenster != 3*24*time.Hour {
		t.Errorf("Standardfenster %v, erwartet 72h", BranchAktivFenster)
	}

	e.AktivFenster = 7 * 24 * time.Hour
	if d := eintragNr(t, RepoArbeitBauen(e), 43); !d.Aktiv || !d.Branches[0].Aktiv {
		t.Errorf("#43 mit 7-Tage-Fenster nicht aktiv: %+v", d)
	}
}

func TestIssueBranchesBaumBleibt(t *testing.T) {
	a := RepoArbeitBauen(ArbeitEingabe{
		Repo:     "acme/alpha",
		Standard: "main",
		Jetzt:    branchJetzt,
		Offen:    []ghrest.Issue{{Nummer: 3, Titel: "drei"}, {Nummer: 4, Titel: "vier"}},
		Blocker:  map[int][]int{3: {4}},
		Changes: []collectors.OpenspecChange{
			{Kennung: "vier", Issue: 4, Aufgaben: aufgaben(1, 3), Ref: "change/vier", RemoteBranch: true},
		},
		Branches:     []ghrest.Branch{zweig("main"), zweig("claude/3-drei")},
		BranchKoepfe: map[string]BranchKopf{"claude/3-drei": {Zeit: vor(time.Minute)}},
	})
	if got := baum(a.Reihe); got != "4:0/-,3:1/4" {
		t.Errorf("Baum %s, erwartet 4:0/-,3:1/4", got)
	}
	baumInvariante(t, a.Reihe)
	if d := eintragNr(t, a, 3); len(d.Branches) != 1 || !d.Aktiv {
		t.Errorf("#3 %+v", d)
	}
}
