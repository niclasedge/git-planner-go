package appctx

// Arbeitsliste eines Repos: Reihenfolge ueber blocked_by, Status aus dem
// Change, die letzten fuenf erledigten (repo-leiste, #25).

import (
	"fmt"
	"testing"

	"github.com/niclasedge/git-planner-go/internal/collectors"
	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

func nummern(es []ArbeitsEintrag) string {
	s := ""
	for i, e := range es {
		if i > 0 {
			s += ","
		}
		if e.Art == "change" {
			s += e.Change.Kennung
		} else {
			s += fmt.Sprint(e.Nummer)
		}
	}
	return s
}

func aufgaben(erledigt, gesamt int) []collectors.OpenspecAufgabe {
	out := []collectors.OpenspecAufgabe{}
	for i := 0; i < gesamt; i++ {
		out = append(out, collectors.OpenspecAufgabe{Text: fmt.Sprint("A", i), Erledigt: i < erledigt})
	}
	return out
}

func TestRepoArbeitReihenfolgeUndStatus(t *testing.T) {
	e := ArbeitEingabe{
		Repo: "acme/alpha",
		Offen: []ghrest.Issue{
			// Rollen-Labels (karte-rollen, #102): ohne ready-for-agent waeren
			// #1 und #3 aufzuraeumen.
			{Nummer: 1, Titel: "eins", Labels: []ghrest.Label{{Name: "ready-for-agent"}}},
			{Nummer: 2, Titel: "zwei"},
			{Nummer: 3, Titel: "drei: bauen", Labels: []ghrest.Label{{Name: "ready-for-agent"}}},
			{Nummer: 4, Titel: "vier"},
			{Nummer: 9, Titel: "ein PR", IstPR: true},
		},
		Blocker: map[int][]int{2: {3}},
		Changes: []collectors.OpenspecChange{
			{Kennung: "drei", Aufgaben: aufgaben(0, 2)},
			{Kennung: "vier", Issue: 4, Aufgaben: aufgaben(1, 3), Ref: "change/vier", RemoteBranch: true},
			{Kennung: "los", Aufgaben: aufgaben(0, 1)},
			// Archiviert und an ein geschlossenes Issue gebunden: gehoert in
			// keine Zeile der Reihe (den Fall "offenes Issue mit archiviertem
			// Change" prueft TestOffenesIssueMitArchiviertemChange).
			{Kennung: "alt", Issue: 99, Archiviert: true},
		},
	}
	a := RepoArbeitBauen(e)
	// Freie Wurzeln nach entsperrt (karte-reihe-entsperrt, #104): #3 gibt
	// #2 frei und steht vor #1, das nichts freigibt.
	if got := nummern(a.Reihe); got != "4,3,2,1" {
		t.Fatalf("Reihe %s, erwartet 4,3,2,1", got)
	}
	want := []string{"in_arbeit", "bereit", "blockiert", "ohne_change"}
	for i, w := range want {
		if a.Reihe[i].Status != w {
			t.Errorf("#%d Status %q, erwartet %q", a.Reihe[i].Nummer, a.Reihe[i].Status, w)
		}
	}
	if b := a.Reihe[2].BlockiertDurch; len(b) != 1 || b[0] != 3 {
		t.Errorf("blockiert_durch %v", b)
	}
	if c := a.Reihe[1].Change; c == nil || c.Kennung != "drei" {
		t.Errorf("#3 ohne Change drei (Titel-Zuordnung): %+v", c)
	}
	if c := a.Reihe[0].Change; c == nil || c.Ref != "change/vier" || c.Erledigt != 1 || c.Gesamt != 3 {
		t.Errorf("#4 Change %+v", c)
	}
	if a.Reihe[3].Change != nil {
		t.Errorf("#1 traegt einen fremden Change: %+v", a.Reihe[3].Change)
	}
	if got := nummern(a.OhneIssue); got != "los" {
		t.Errorf("ohne_issue %s, erwartet los", got)
	}
	if len(a.PRs) != 1 || a.PRs[0].Nummer != 9 {
		t.Errorf("PRs %+v", a.PRs)
	}
	// Fortschritt ueber die nicht archivierten Changes: 0/2 + 1/3 + 0/1.
	if a.Fortschritt.Erledigt != 1 || a.Fortschritt.Gesamt != 6 {
		t.Errorf("Fortschritt %+v", a.Fortschritt)
	}
}

func TestRepoArbeitImPRVorFertigVorLaufend(t *testing.T) {
	e := ArbeitEingabe{
		Repo:  "acme/alpha",
		Offen: []ghrest.Issue{{Nummer: 1}, {Nummer: 2}, {Nummer: 3}},
		Changes: []collectors.OpenspecChange{
			{Kennung: "a", Issue: 1, Aufgaben: aufgaben(1, 2)},
			{Kennung: "b", Issue: 2, Aufgaben: aufgaben(2, 2)},
			{Kennung: "c", Issue: 3, Aufgaben: aufgaben(2, 2), RemoteBranch: true},
		},
	}
	a := RepoArbeitBauen(e)
	if got := nummern(a.Reihe); got != "3,2,1" {
		t.Errorf("Reihe %s, erwartet 3 (im_pr), 2 (fertig), 1 (in_arbeit)", got)
	}
}

func TestRepoArbeitZuletztFuenf(t *testing.T) {
	var zu []ghrest.Issue
	for n := 15; n >= 10; n-- {
		zu = append(zu, ghrest.Issue{Nummer: n, Geschlossen: fmt.Sprintf("2026-09-%02dT00:00:00Z", n)})
	}
	e := ArbeitEingabe{
		Repo:        "acme/alpha",
		Geschlossen: zu,
		Changes: []collectors.OpenspecChange{{Kennung: "x", Issue: 15, Archiviert: true, Aufgaben: aufgaben(1, 1),
			Belege: []collectors.OpenspecBeleg{{Stufe: "nachher", Art: "bild", Pfad: "belege/assets/0-x.png", Bild: "/api/blob?repo=acme%2Falpha&sha=b9&name=0-x.png"}}}},
	}
	a := RepoArbeitBauen(e)
	if got := nummern(a.Zuletzt); got != "15,14,13,12,11" {
		t.Fatalf("zuletzt %s", got)
	}
	z := a.Zuletzt[0]
	if z.Status != "erledigt" || z.Change == nil || len(z.Change.Belege) != 1 || z.Change.Belege[0].Bild == "" {
		t.Errorf("#15 %+v", z)
	}
	if len(a.OhneIssue) != 0 {
		t.Errorf("archivierter Change unter ohne_issue: %s", nummern(a.OhneIssue))
	}
}

func TestOffenesIssueMitArchiviertemChange(t *testing.T) {
	e := ArbeitEingabe{
		Repo:    "acme/alpha",
		Offen:   []ghrest.Issue{{Nummer: 25, Titel: "repo-leiste: bauen"}},
		Changes: []collectors.OpenspecChange{{Kennung: "repo-leiste", Issue: 25, Archiviert: true, Aufgaben: aufgaben(7, 7)}},
	}
	a := RepoArbeitBauen(e)
	if len(a.Reihe) != 1 {
		t.Fatalf("Reihe %s", nummern(a.Reihe))
	}
	z := a.Reihe[0]
	if z.Change == nil || z.Change.Kennung != "repo-leiste" || z.Status != ArbeitFertig {
		t.Errorf("#25 Status %q Change %+v, erwartet fertig mit repo-leiste", z.Status, z.Change)
	}
}

func TestRepoArbeitNimmtPRListe(t *testing.T) {
	wahr := true
	e := ArbeitEingabe{
		Repo:  "acme/alpha",
		Offen: []ghrest.Issue{{Nummer: 9, IstPR: true}},
		PRs:   []ghrest.PR{{Nummer: 9, Draft: true, Head: "change/neun", Mergebar: &wahr}},
	}
	a := RepoArbeitBauen(e)
	if len(a.PRs) != 1 || !a.PRs[0].Draft || a.PRs[0].Head != "change/neun" {
		t.Errorf("PRs %+v, erwartet die PR-Liste statt der Issue-Eintraege", a.PRs)
	}
	ohne := RepoArbeitBauen(ArbeitEingabe{Repo: "acme/alpha", Offen: []ghrest.Issue{{Nummer: 9, Titel: "neun", IstPR: true}}})
	if len(ohne.PRs) != 1 || ohne.PRs[0].Nummer != 9 || ohne.PRs[0].Titel != "neun" {
		t.Errorf("Rueckfall %+v, erwartet den PR aus den Issues", ohne.PRs)
	}
}

// Blocker streng wie lage (karte-blocker-streng, #101): was GitHub in der
// Zusammenfassung als offenen Blocker zaehlt, gilt, auch wenn die
// Detail-Abfrage scheiterte oder der Blocker nicht in der offenen Liste steht;
// und blockiert geht vor dem Stand des Changes. Die Faelle aus
// acme-platform/tests/lage-issue-kette.test.mjs liegen nicht in diesem
// Repo; sie sind aus der Abnahme von #101 abgeleitet.

func eintragJe(a RepoArbeit) map[int]ArbeitsEintrag {
	je := map[int]ArbeitsEintrag{}
	for _, e := range a.Reihe {
		je[e.Nummer] = e
	}
	return je
}

func TestBlockerAbfrageGescheitertBleibtBlockiert(t *testing.T) {
	// #2 meldet einen offenen Blocker, die Detail-Abfrage lieferte nichts
	// (kein Eintrag in Blocker) — mit und ohne Change.
	e := ArbeitEingabe{
		Repo: "acme/alpha",
		Offen: []ghrest.Issue{
			{Nummer: 1, Titel: "eins", Labels: []ghrest.Label{{Name: "ready-for-agent"}}},
			{Nummer: 2, Titel: "zwei", Blocker: 1},
			{Nummer: 3, Titel: "drei", Blocker: 1},
		},
		Blocker: map[int][]int{},
		Changes: []collectors.OpenspecChange{{Kennung: "drei", Issue: 3, Aufgaben: aufgaben(0, 2)}},
	}
	je := eintragJe(RepoArbeitBauen(e))
	for _, nr := range []int{2, 3} {
		if je[nr].Status != ArbeitBlockiert {
			t.Errorf("#%d Status %q, erwartet blockiert (Zusammenfassung meldet einen Blocker)", nr, je[nr].Status)
		}
		if len(je[nr].BlockiertDurch) != 0 {
			t.Errorf("#%d blockiert_durch %v, erwartet leer", nr, je[nr].BlockiertDurch)
		}
	}
	if je[1].Status != ArbeitOhneChange {
		t.Errorf("#1 Status %q, erwartet ohne_change", je[1].Status)
	}
}

func TestBlockerAusserhalbDerOffenenListe(t *testing.T) {
	e := ArbeitEingabe{
		Repo: "acme/alpha",
		Offen: []ghrest.Issue{
			{Nummer: 1, Titel: "eins", Labels: []ghrest.Label{{Name: "ready-for-agent"}}},
			{Nummer: 5, Titel: "fuenf", Blocker: 2},
			{Nummer: 6, Titel: "sechs", Blocker: 1},
			{Nummer: 8, Titel: "acht", Blocker: 1},
		},
		// #7 steht nicht in der offenen Liste; #8 ist voll bekannt.
		Blocker: map[int][]int{5: {1}, 6: {7}, 8: {1}},
		Changes: []collectors.OpenspecChange{{Kennung: "acht", Issue: 8, Aufgaben: aufgaben(0, 1)}},
	}
	je := eintragJe(RepoArbeitBauen(e))
	if s := je[5]; s.Status != ArbeitBlockiert || fmt.Sprint(s.BlockiertDurch) != "[1]" {
		t.Errorf("#5 %q %v, erwartet blockiert [1]", s.Status, s.BlockiertDurch)
	}
	if s := je[6]; s.Status != ArbeitBlockiert || len(s.BlockiertDurch) != 0 {
		t.Errorf("#6 %q %v, erwartet blockiert ohne bekannten Blocker", s.Status, s.BlockiertDurch)
	}
	if s := je[8]; s.Status != ArbeitBlockiert || fmt.Sprint(s.BlockiertDurch) != "[1]" {
		t.Errorf("#8 %q %v, erwartet blockiert [1]", s.Status, s.BlockiertDurch)
	}
	if je[1].Status != ArbeitOhneChange {
		t.Errorf("#1 Status %q", je[1].Status)
	}
}

func TestBlockiertVorStandDesChanges(t *testing.T) {
	e := ArbeitEingabe{
		Repo: "acme/alpha",
		Offen: []ghrest.Issue{
			{Nummer: 1, Titel: "eins"},
			{Nummer: 2, Titel: "zwei", Blocker: 1},
			{Nummer: 3, Titel: "drei", Blocker: 1},
			{Nummer: 4, Titel: "vier", Blocker: 1},
			{Nummer: 5, Titel: "fuenf"},
		},
		Blocker: map[int][]int{2: {1}, 3: {1}, 4: {1}},
		Changes: []collectors.OpenspecChange{
			{Kennung: "zwei", Issue: 2, Aufgaben: aufgaben(1, 3)},
			{Kennung: "drei", Issue: 3, Aufgaben: aufgaben(2, 2)},
			{Kennung: "vier", Issue: 4, Aufgaben: aufgaben(2, 2), RemoteBranch: true},
			{Kennung: "fuenf", Issue: 5, Aufgaben: aufgaben(1, 2)},
		},
	}
	a := RepoArbeitBauen(e)
	je := eintragJe(a)
	for _, nr := range []int{2, 3, 4} {
		if je[nr].Status != ArbeitBlockiert {
			t.Errorf("#%d Status %q, erwartet blockiert vor dem Stand des Changes", nr, je[nr].Status)
		}
	}
	if je[5].Status != ArbeitInArbeit {
		t.Errorf("#5 Status %q, erwartet in_arbeit", je[5].Status)
	}
	// Die Blockierten haengen unter ihrem Blocker, nicht vorn.
	if got := nummern(a.Reihe); got != "5,1,2,3,4" {
		t.Errorf("Reihe %s, erwartet 5,1,2,3,4", got)
	}
}
