package appctx

// Freie Wurzeln nach entsperrt (karte-reihe-entsperrt, #104): unter den
// Wurzeln ohne bekannten offenen Blocker steht zuerst, was die meisten
// Eintraege der Reihe freigibt, bei Gleichstand nach Nummer. Gerankte
// Wurzeln bleiben davor, die Tiefensuche entlang blocked_by bleibt.

import (
	"testing"

	"github.com/niclasedge/git-planner-go/internal/collectors"
	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

func TestRepoArbeitFreieWurzelnNachEntsperrt(t *testing.T) {
	offen := []ghrest.Issue{}
	for nr := 1; nr <= 9; nr++ {
		offen = append(offen, ghrest.Issue{Nummer: nr, Titel: "issue"})
	}
	a := RepoArbeitBauen(ArbeitEingabe{
		Repo:    "acme/alpha",
		Offen:   offen,
		Blocker: map[int][]int{5: {2}, 6: {3}, 7: {3}, 8: {4}},
		Changes: []collectors.OpenspecChange{
			{Kennung: "neun", Issue: 9, Aufgaben: aufgaben(1, 3)},
		},
	})
	if got := nummern(a.Reihe); got != "9,3,6,7,2,5,4,8,1" {
		t.Fatalf("Reihe %s, erwartet 9,3,6,7,2,5,4,8,1", got)
	}
	if got := baum(a.Reihe); got != "9:0/-,3:0/-,6:1/3,7:1/3,2:0/-,5:1/2,4:0/-,8:1/4,1:0/-" {
		t.Errorf("Baum %s, erwartet 9:0/-,3:0/-,6:1/3,7:1/3,2:0/-,5:1/2,4:0/-,8:1/4,1:0/-", got)
	}
	if a.Reihe[0].Status != ArbeitInArbeit {
		t.Errorf("#9 Status %q, erwartet in_arbeit", a.Reihe[0].Status)
	}
	baumInvariante(t, a.Reihe)
}
