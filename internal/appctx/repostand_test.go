package appctx

// Stand je Repo fuer die Leiste (things-farben-fortschritt, #41).

import (
	"testing"

	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

func TestRepoStandBauen(t *testing.T) {
	offen := []ghrest.Issue{{Nummer: 1}, {Nummer: 2}, {Nummer: 3}, {Nummer: 4, IstPR: true}, {Nummer: 5, IstPR: true}}
	s := RepoStandBauen("acme/alpha", offen, 6)
	if s.Repo != "acme/alpha" || s.OffeneIssues != 3 || s.OffenePRs != 2 || s.ErledigteIssues != 6 || s.Anteil != 0.6667 {
		t.Errorf("Stand %+v", s)
	}
	leer := RepoStandBauen("acme/beta", nil, 0)
	if leer.Anteil != 0 || leer.OffeneIssues != 0 {
		t.Errorf("leer %+v", leer)
	}
	fertig := RepoStandBauen("acme/gamma", []ghrest.Issue{{Nummer: 9, IstPR: true}}, 4)
	if fertig.Anteil != 1 || fertig.OffenePRs != 1 {
		t.Errorf("alles erledigt %+v", fertig)
	}
	// Mehr geschlossene PRs als geschlossene Eintraege (Zaehlung waehrend
	// einer Aenderung) ergibt nie eine negative Zahl.
	if s := RepoStandBauen("acme/delta", nil, -2); s.ErledigteIssues != 0 || s.Anteil != 0 {
		t.Errorf("negativ %+v", s)
	}
}
