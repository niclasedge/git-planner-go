package appctx

// Rollen-Labels entscheiden frei (karte-rollen, #102): wie lage teilt die
// Arbeitsliste ein offenes Issue nach seinen Rollen-Labels ein — frei ist nur,
// was ready-for-agent traegt und nicht genommen ist; ein Menschen-Label heisst
// wartet auf Mensch; keine, mehrere Rollen oder ein nicht offenes
// Parent-Issue heissen aufraeumen.

import (
	"fmt"
	"strings"

	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

// Die Status aus den Rollen.
const (
	ArbeitWartetAufMensch = "wartet_auf_mensch"
	ArbeitHygiene         = "hygiene"
)

// RolleAgent ist das Label, das ein Issue fuer einen Agenten freigibt;
// LabelInArbeit sagt, dass es schon genommen ist (keine Rolle).
const (
	RolleAgent    = "ready-for-agent"
	LabelInArbeit = "in-arbeit"
)

// menschenRollen ordnet die Menschen-Labels ihrer Rolle zu; question und
// frage sind dieselbe (entschieden in #100).
var menschenRollen = map[string]string{
	"ready-for-human": "ready-for-human",
	"needs-info":      "needs-info",
	"needs-triage":    "needs-triage",
	"s15-approval":    "s15-approval",
	"human-verify":    "human-verify",
	"question":        "frage",
	"frage":           "frage",
}

func labelName(l ghrest.Label) string { return strings.ToLower(strings.TrimSpace(l.Name)) }

// rollenLabels sind die Labels, deren Anlage im Repo sagt, dass es die
// Rollen nutzt (karte-gruppen, #121); question und frage zaehlen nicht.
var rollenLabels = map[string]bool{
	RolleAgent: true, "ready-for-human": true, "needs-info": true, "needs-triage": true,
	"s15-approval": true, "human-verify": true,
}

// RepoNutztRollen sagt, ob unter den Labels eines Repos eines der
// Rollen-Labels ist; nur dann gilt der Grund "Rolle fehlt".
func RepoNutztRollen(labels []ghrest.Label) bool {
	for _, l := range labels {
		if rollenLabels[labelName(l)] {
			return true
		}
	}
	return false
}

// rollenStatus teilt ein offenes Issue nach seinen Rollen ein, nachdem
// Blocker und Change-Stand nichts entschieden haben. offen sagt, ob ein
// Issue unter den offenen steht (fuer das Parent-Issue). hatChange sagt, ob
// ein Change am Issue haengt. rollenGelten sagt, ob das Repo Rollen-Labels
// nutzt; sonst heisst ein Issue ohne Rolle wie vor #102 bereit mit Change
// und ohne_change ohne (karte-gruppen, #121).
func rollenStatus(i ghrest.Issue, hatChange bool, offen map[int]bool, rollenGelten bool) (status, grund string) {
	var rollen, namen []string
	gesehen := map[string]bool{}
	inArbeit := false
	for _, l := range i.Labels {
		n := labelName(l)
		rolle := menschenRollen[n]
		if n == RolleAgent {
			rolle = RolleAgent
		}
		if n == LabelInArbeit {
			inArbeit = true
		}
		if rolle == "" || gesehen[rolle] {
			continue
		}
		gesehen[rolle] = true
		rollen = append(rollen, rolle)
		namen = append(namen, strings.TrimSpace(l.Name))
	}

	var gruende []string
	switch {
	case len(rollen) > 1:
		gruende = append(gruende, "mehrere Rollen: "+strings.Join(namen, ", "))
	case len(rollen) == 0 && rollenGelten:
		gruende = append(gruende, "Rolle fehlt")
	}
	if i.ParentNummer > 0 && !offen[i.ParentNummer] {
		gruende = append(gruende, fmt.Sprintf("Parent #%d nicht offen", i.ParentNummer))
	}
	switch {
	case len(gruende) > 0:
		return ArbeitHygiene, strings.Join(gruende, "; ")
	case len(rollen) == 0:
		// Ohne Rollen im Repo: die Regeln vor #102.
		if hatChange {
			return ArbeitBereit, ""
		}
		return ArbeitOhneChange, ""
	case rollen[0] != RolleAgent:
		return ArbeitWartetAufMensch, namen[0]
	case inArbeit:
		return ArbeitInArbeit, ""
	case hatChange:
		return ArbeitBereit, ""
	}
	return ArbeitOhneChange, ""
}
