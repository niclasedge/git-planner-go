package appctx

// ohne_issue wie just lage (karte-herkunft-streng, #103): ein Change mit
// Herkunftszeile ist verknuepft, auch wenn sein Issue geschlossen ist.

import (
	"testing"

	"github.com/niclasedge/git-planner-go/internal/collectors"
	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

// Szenario: Change auf ein geschlossenes Issue.
func TestRepoArbeitChangeAufGeschlossenesIssue(t *testing.T) {
	e := ArbeitEingabe{
		Repo:        "acme/alpha",
		Offen:       []ghrest.Issue{{Nummer: 1, Titel: "eins"}},
		Geschlossen: []ghrest.Issue{{Nummer: 5, Titel: "fuenf", Geschlossen: "2026-09-20T00:00:00Z"}},
		Changes: []collectors.OpenspecChange{
			{Kennung: "zu", Issue: 5, Aufgaben: aufgaben(0, 1)},
			{Kennung: "los", Aufgaben: aufgaben(0, 1)},
		},
	}
	a := RepoArbeitBauen(e)
	if got := nummern(a.OhneIssue); got != "los" {
		t.Errorf("ohne_issue %s, erwartet nur los (zu nennt das geschlossene #5)", got)
	}
	if got := nummern(a.Reihe); got != "1" {
		t.Errorf("reihe %s, erwartet 1", got)
	}
}
