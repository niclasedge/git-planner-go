package appctx

// Letzte Aenderung je Eintrag der Reihe (naechste-geaendert, #118): der
// juengste Zeitpunkt aus dem Issue, den offenen PRs, die die Zeile schon
// kennt, und - ohne offenen PR - dem letzten Commit eines verknuepften
// Branches. Alles liegt im Abruf der Arbeitsliste schon vor; hier wird nur
// gewaehlt und der Text gebaut, die Oberflaeche zeichnet ihn.

import (
	"time"

	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

// Quellen der letzten Aenderung.
const (
	GeaendertIssue  = "issue"
	GeaendertPR     = "pr"
	GeaendertBranch = "branch"
)

// ZuletztGeaendert ist die letzte Aenderung eines Eintrags: Quelle, Zeit
// und Text der Zeile samt Tooltip, dazu Issue- und PR-Zeit fuer den Fuss.
// PR ist die Nummer des juengsten offenen PRs der Zeile, auch wenn eine
// andere Quelle gewinnt.
type ZuletztGeaendert struct {
	Quelle    string `json:"quelle"`
	Zeit      string `json:"zeit"`
	Text      string `json:"text"`
	Voll      string `json:"voll"`
	PR        int    `json:"pr,omitempty"`
	IssueText string `json:"issue_text,omitempty"`
	IssueVoll string `json:"issue_voll,omitempty"`
	PRText    string `json:"pr_text,omitempty"`
	PRVoll    string `json:"pr_voll,omitempty"`
}

// istOffen sagt, ob ein PR weder gemergt noch geschlossen ist.
func istOffen(p ghrest.PR) bool { return !p.Gemergt && p.Zustand != "closed" }

// juengsterOffenerPR ist unter den offenen PRs, die die Zeile kennt (an
// branches[].pr und auf change/KENNUNG), der mit dem juengsten lesbaren
// updated_at; nil, wenn keiner eine lesbare Zeit hat. offen sagt, ob die
// Zeile ueberhaupt einen offenen PR kennt.
func juengsterOffenerPR(ein ArbeitsEintrag, prs []ghrest.PR) (treffer *ghrest.PR, offen bool) {
	nachNummer := map[int]*ghrest.PR{}
	for i := range prs {
		nachNummer[prs[i].Nummer] = &prs[i]
	}
	var kandidaten []*ghrest.PR
	for _, b := range ein.Branches {
		if b.PR != nil {
			if p := nachNummer[b.PR.Nummer]; p != nil && istOffen(*p) {
				kandidaten = append(kandidaten, p)
			}
		}
	}
	if ein.Change != nil {
		if p := prFuerChange(prs, ein.Change.Kennung); p != nil && istOffen(*p) {
			kandidaten = append(kandidaten, p)
		}
	}
	var beste time.Time
	for _, p := range kandidaten {
		t, ok := zeitAus(p.Aktualisiert)
		if !ok {
			continue
		}
		if treffer == nil || t.After(beste) || (t.Equal(beste) && p.Nummer < treffer.Nummer) {
			treffer, beste = p, t
		}
	}
	return treffer, len(kandidaten) > 0
}

// GeaendertBauen waehlt die letzte Aenderung eines Eintrags; nil, wenn
// keine Quelle einen lesbaren Zeitpunkt hat. Bei Gleichstand gilt die
// Folge issue, pr, branch.
func GeaendertBauen(ein ArbeitsEintrag, prs []ghrest.PR, jetzt time.Time) *ZuletztGeaendert {
	g := &ZuletztGeaendert{}
	var beste time.Time
	nimm := func(quelle, zeit string) {
		t, ok := zeitAus(zeit)
		if !ok || (g.Quelle != "" && !t.After(beste)) {
			return
		}
		g.Quelle, g.Zeit, beste = quelle, zeit, t
	}
	nimm(GeaendertIssue, ein.Aktualisiert)
	g.IssueText, g.IssueVoll = ZeitpunktText(ein.Aktualisiert, jetzt)

	pr, offen := juengsterOffenerPR(ein, prs)
	if pr != nil {
		nimm(GeaendertPR, pr.Aktualisiert)
		g.PR = pr.Nummer
		g.PRText, g.PRVoll = ZeitpunktText(pr.Aktualisiert, jetzt)
	}
	if !offen {
		for _, b := range ein.Branches {
			nimm(GeaendertBranch, b.Zeit)
		}
	}
	if g.Quelle == "" {
		return nil
	}
	g.Text, g.Voll = ZeitpunktText(g.Zeit, jetzt)
	return g
}

// geaendertSetzen traegt an jedem Eintrag der Reihe die letzte Aenderung ein.
func geaendertSetzen(es []ArbeitsEintrag, prs []ghrest.PR, jetzt time.Time) {
	for i := range es {
		es[i].ZuletztGeaendert = GeaendertBauen(es[i], prs, jetzt)
	}
}
