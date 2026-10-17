package appctx

// Einruecktiefe der Arbeitsreihe (liste-abhaengigkeiten, #48): die Reihe ist
// eine Tiefensuche ueber den Wald der Abhaengigkeiten. Jeder Eintrag steht
// direkt unter dem Teilbaum seines Blockers, mit tiefe und unter. Die
// gerankten Eintraege (im_pr, fertig, in_arbeit) bleiben oben.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/niclasedge/git-planner-go/internal/collectors"
	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

// baum schreibt je Eintrag der Reihe "nummer:tiefe/unter", "-" fuer kein unter.
func baum(es []ArbeitsEintrag) string {
	teile := []string{}
	for _, e := range es {
		unter := "-"
		if e.Unter != 0 {
			unter = fmt.Sprint(e.Unter)
		}
		teile = append(teile, fmt.Sprintf("%d:%d/%s", e.Nummer, e.Tiefe, unter))
	}
	return strings.Join(teile, ",")
}

// baumInvariante prueft die beiden Zusagen der Reihe: der naechste
// vorausgehende Eintrag mit Tiefe tiefe-1 ist genau unter, und jeder offene
// Blocker steht vor dem Eintrag, den er blockiert.
func baumInvariante(t *testing.T, reihe []ArbeitsEintrag) {
	t.Helper()
	stelle := map[int]int{}
	for i, e := range reihe {
		stelle[e.Nummer] = i
	}
	for i, e := range reihe {
		if e.Tiefe > 0 {
			eltern := 0
			for j := i - 1; j >= 0; j-- {
				if reihe[j].Tiefe == e.Tiefe-1 {
					eltern = reihe[j].Nummer
					break
				}
			}
			if eltern != e.Unter {
				t.Errorf("#%d (tiefe %d, unter %d): naechster vorausgehender Eintrag mit tiefe %d ist #%d", e.Nummer, e.Tiefe, e.Unter, e.Tiefe-1, eltern)
			}
		} else if e.Unter != 0 {
			t.Errorf("#%d mit tiefe 0 traegt unter %d", e.Nummer, e.Unter)
		}
		if i > 0 && e.Tiefe > reihe[i-1].Tiefe+1 {
			t.Errorf("#%d springt von tiefe %d auf %d", e.Nummer, reihe[i-1].Tiefe, e.Tiefe)
		}
		for _, b := range e.BlockiertDurch {
			if p, ok := stelle[b]; ok && p > i {
				t.Errorf("Blocker #%d steht nach #%d", b, e.Nummer)
			}
		}
	}
}

func TestRepoArbeitTiefeKette(t *testing.T) {
	a := RepoArbeitBauen(ArbeitEingabe{
		Repo: "acme/alpha",
		Offen: []ghrest.Issue{
			{Nummer: 1, Titel: "eins"},
			{Nummer: 2, Titel: "zwei"},
			{Nummer: 3, Titel: "drei"},
			{Nummer: 4, Titel: "vier"},
		},
		Blocker: map[int][]int{2: {3}, 3: {4}},
		Changes: []collectors.OpenspecChange{
			{Kennung: "vier", Issue: 4, Aufgaben: aufgaben(1, 3), Ref: "change/vier", RemoteBranch: true},
		},
	})
	if got := baum(a.Reihe); got != "4:0/-,3:1/4,2:2/3,1:0/-" {
		t.Errorf("Baum %s, erwartet 4:0/-,3:1/4,2:2/3,1:0/-", got)
	}
	baumInvariante(t, a.Reihe)
}

func TestRepoArbeitTiefeMehrereBlocker(t *testing.T) {
	a := RepoArbeitBauen(ArbeitEingabe{
		Repo:    "acme/alpha",
		Offen:   []ghrest.Issue{{Nummer: 1, Titel: "eins"}, {Nummer: 2, Titel: "zwei"}, {Nummer: 5, Titel: "fuenf"}},
		Blocker: map[int][]int{5: {2, 1}},
	})
	if got := baum(a.Reihe); got != "1:0/-,2:0/-,5:1/2" {
		t.Errorf("Baum %s, erwartet 1:0/-,2:0/-,5:1/2", got)
	}
	if b := a.Reihe[2].BlockiertDurch; fmt.Sprint(b) != "[1 2]" {
		t.Errorf("blockiert_durch %v, erwartet [1 2]", b)
	}
	baumInvariante(t, a.Reihe)
}

// Ein Wald: gerankter #4 mit Kette 3, 2; Wurzel #1 mit den Kindern 6 und 7
// (7 wartet auch auf 3, ist aber erst nach 1 frei); Wurzel #8 mit Kind 9.
func TestRepoArbeitTiefeWald(t *testing.T) {
	offen := []ghrest.Issue{}
	for _, n := range []int{1, 2, 3, 4, 6, 7, 8, 9} {
		offen = append(offen, ghrest.Issue{Nummer: n, Titel: fmt.Sprint("i", n)})
	}
	a := RepoArbeitBauen(ArbeitEingabe{
		Repo:    "acme/alpha",
		Offen:   offen,
		Blocker: map[int][]int{2: {3}, 3: {4}, 6: {1}, 7: {3, 1}, 9: {8}},
		Changes: []collectors.OpenspecChange{
			{Kennung: "vier", Issue: 4, Aufgaben: aufgaben(1, 3), Ref: "change/vier", RemoteBranch: true},
		},
	})
	want := "4:0/-,3:1/4,2:2/3,1:0/-,6:1/1,7:1/1,8:0/-,9:1/8"
	if got := baum(a.Reihe); got != want {
		t.Errorf("Baum %s, erwartet %s", got, want)
	}
	baumInvariante(t, a.Reihe)
}

func TestRepoArbeitTiefeKreis(t *testing.T) {
	a := RepoArbeitBauen(ArbeitEingabe{
		Repo:    "acme/alpha",
		Offen:   []ghrest.Issue{{Nummer: 1, Titel: "eins"}, {Nummer: 2, Titel: "zwei"}, {Nummer: 3, Titel: "drei"}},
		Blocker: map[int][]int{1: {2}, 2: {1}, 3: {1}},
	})
	if got := baum(a.Reihe); got != "1:0/-,2:0/-,3:0/-" {
		t.Errorf("Baum %s, erwartet 1:0/-,2:0/-,3:0/-", got)
	}
}
