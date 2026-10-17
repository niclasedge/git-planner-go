package appctx

// Der Lebenslauf je Issue und PR (pr-lebenslauf, #83): acht Stufen vom
// Branch bis zum Aufraeumen, je mit Zustand, Grund und — wo die Quelle ihn
// kennt — dem Zeitpunkt der Erledigung, dazu die Nachsorge nach dem Merge.
// Gelesen wird nur, was issue-branches, pr-checks, pr-reviews,
// konflikte-anzeigen, pr-merge-optionen und naechster-schritt schon liefern.
// Die Oberflaeche zeichnet nur, was hier gerechnet wird.

import (
	"fmt"
	"sort"
	"strings"

	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

// Die Stufen in ihrer Folge.
const (
	StufeBranch     = "branch"
	StufePR         = "pr"
	StufePruefung   = "pruefung"
	StufeReview     = "review"
	StufeKonflikt   = "konflikt"
	StufeMerge      = "merge"
	StufeAufraeumen = "aufraeumen"
	StufeFertig     = "fertig"
)

// Die Zustaende einer Stufe.
const (
	StufeErledigt  = "erledigt"
	StufeJetzt     = "jetzt"
	StufeOffen     = "offen"
	StufeBlockiert = "blockiert"
)

// Die Arten der Nachsorge.
const (
	NachsorgeIssue  = "issue_schliessen"
	NachsorgeBranch = "branch_loeschen"
	NachsorgeChange = "change_archivieren"
)

var stufenFolge = []struct{ name, titel string }{
	{StufeBranch, "Branch"}, {StufePR, "PR"}, {StufePruefung, "Prüfung"}, {StufeReview, "Review"},
	{StufeKonflikt, "Konflikt"}, {StufeMerge, "Merge"}, {StufeAufraeumen, "Aufräumen"}, {StufeFertig, "Fertig"},
}

// LebenslaufStufe ist eine Stufe des Lebenslaufs.
type LebenslaufStufe struct {
	Name    string `json:"name"`
	Titel   string `json:"titel"`
	Zustand string `json:"zustand"`
	Grund   string `json:"grund,omitempty"`
	// Zeit ist der Zeitpunkt der Erledigung (RFC 3339); leer, wenn die
	// Quelle ihn nicht kennt oder die Stufe nicht erledigt ist.
	Zeit string `json:"zeit,omitempty"`
}

// NachsorgePunkt ist ein Punkt der Pruefliste nach dem Merge. Nummer ist
// beim Issue die Issue-, beim Branch die PR-Nummer (fuer
// /action/pr/branch-loeschen); Befehl steht nur beim Archivieren.
type NachsorgePunkt struct {
	Art      string `json:"art"`
	Text     string `json:"text"`
	Erledigt bool   `json:"erledigt"`
	Nummer   int    `json:"nummer,omitempty"`
	Branch   string `json:"branch,omitempty"`
	Befehl   string `json:"befehl,omitempty"`
}

// Lebenslauf ist die Stufenleiste eines Issues oder PRs. Stufe nennt die
// erste nicht erledigte Stufe, fertig, wenn alle erledigt sind.
type Lebenslauf struct {
	Stufe     string            `json:"stufe"`
	Stufen    []LebenslaufStufe `json:"stufen"`
	Nachsorge []NachsorgePunkt  `json:"nachsorge"`
}

// LebenslaufEingabe ist, was der Lebenslauf braucht — bereits geholt und
// zugeordnet. PR ist der offene oder der gemergte PR (nil: keiner).
type LebenslaufEingabe struct {
	Repo       string
	Issue      int
	IssueOffen bool
	IssueZu    string // Zeitpunkt des Schliessens
	Blocker    []int
	Branch     string
	BranchZeit string // letzter Commit
	BranchDa   bool
	PR         *ghrest.PR
	Change     *ChangeKurz
}

// roh ist eine Stufe vor der Vergabe von jetzt und offen.
type roh struct {
	erfuellt, blockiert bool
	grund, zeit         string
}

func erfuellt(zeit, grund string) roh { return roh{erfuellt: true, zeit: zeit, grund: grund} }
func steht(grund string) roh          { return roh{grund: grund} }
func blockiert(grund string) roh      { return roh{blockiert: true, grund: grund} }

func mehrzahl(n int, eins, viele string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, eins)
	}
	return fmt.Sprintf("%d %s", n, viele)
}

func pruefungStufe(c *ghrest.PRChecks) roh {
	if c == nil {
		return steht("Prüfungen nicht abrufbar")
	}
	switch c.Gesamt {
	case ChecksRot:
		return blockiert(mehrzahl(c.Zusammenfassung.Rot, "Prüfung rot", "Prüfungen rot"))
	case ChecksLaeuft:
		return steht(mehrzahl(c.Zusammenfassung.Laeuft, "Prüfung läuft", "Prüfungen laufen"))
	case ChecksKeine:
		return erfuellt("", "Keine Prüfungen")
	}
	return erfuellt(c.Ende, "")
}

func letzteGenehmigung(r *ghrest.PRReviews) string {
	zeit := ""
	for _, s := range r.Stimmen {
		if s.Zustand == ReviewGenehmigt && s.Zeit > zeit {
			zeit = s.Zeit
		}
	}
	return zeit
}

func reviewStufe(r *ghrest.PRReviews) roh {
	if r == nil {
		return steht("Reviews nicht abrufbar")
	}
	switch r.Entscheidung {
	case ReviewGenehmigt:
		return erfuellt(letzteGenehmigung(r), "")
	case ReviewAenderungen:
		return blockiert("Änderungen verlangt von " + strings.Join(loginsVon(r.Stimmen, ReviewAenderungen), ", "))
	}
	if len(r.Angefordert) > 0 {
		return steht("Review ausstehend von " + strings.Join(r.Angefordert, ", "))
	}
	return steht("Review ausstehend")
}

func konfliktStufe(p *ghrest.PR) roh {
	switch p.Abgleich {
	case ghrest.PRKonflikt:
		return blockiert("Konflikt in " + mehrzahl(len(p.KonfliktDateien), "Datei", "Dateien"))
	case ghrest.PRVeraltet:
		if p.Zurueck > 0 {
			return erfuellt("", mehrzahl(p.Zurueck, "Commit hinter der Base", "Commits hinter der Base"))
		}
		return erfuellt("", "Hinter der Base")
	case ghrest.PRSauber:
		return erfuellt("", "")
	}
	return steht("Wird geprüft")
}

// nachsorgeBauen nennt die Punkte nach dem Merge.
func nachsorgeBauen(e LebenslaufEingabe) []NachsorgePunkt {
	out := []NachsorgePunkt{}
	p := e.PR
	if p == nil || !p.Gemergt {
		return out
	}
	if e.Issue > 0 {
		out = append(out, NachsorgePunkt{Art: NachsorgeIssue, Text: fmt.Sprintf("Issue #%d schließen", e.Issue),
			Erledigt: !e.IssueOffen, Nummer: e.Issue})
	}
	if p.Head != "" && (p.HeadRepo == "" || strings.EqualFold(p.HeadRepo, e.Repo)) {
		out = append(out, NachsorgePunkt{Art: NachsorgeBranch, Text: "Branch " + p.Head + " löschen",
			Erledigt: !e.BranchDa, Nummer: p.Nummer, Branch: p.Head})
	}
	if c := e.Change; c != nil {
		out = append(out, NachsorgePunkt{Art: NachsorgeChange, Text: "Change " + c.Kennung + " archivieren",
			Erledigt: c.Archiviert, Befehl: "just archiv " + c.Kennung})
	}
	return out
}

// LebenslaufBauen leitet die acht Stufen und die Nachsorge ab.
func LebenslaufBauen(e LebenslaufEingabe) Lebenslauf {
	p := e.PR
	gemergt := p != nil && p.Gemergt
	stufen := make([]roh, len(stufenFolge))

	switch {
	case e.BranchDa || p != nil:
		stufen[0] = erfuellt(e.BranchZeit, "")
	case len(e.Blocker) > 0:
		teile := make([]string, len(e.Blocker))
		for i, b := range e.Blocker {
			teile[i] = fmt.Sprintf("#%d", b)
		}
		stufen[0] = blockiert("Blockiert durch " + strings.Join(teile, ", "))
	default:
		stufen[0] = steht("Noch kein Branch")
	}

	nachsorge := nachsorgeBauen(e)
	switch {
	case p == nil:
		if e.BranchDa {
			stufen[1] = steht("Noch kein PR")
		} else {
			stufen[1] = steht("")
		}
		for i := 2; i < len(stufen); i++ {
			stufen[i] = steht("")
		}
	case gemergt:
		stufen[1] = erfuellt(p.Erstellt, "")
		ende := ""
		if p.Checks != nil {
			ende = p.Checks.Ende
		}
		stufen[2] = erfuellt(ende, "")
		stufen[3] = erfuellt("", "")
		if p.Reviews != nil {
			stufen[3].zeit = letzteGenehmigung(p.Reviews)
		}
		stufen[4] = erfuellt("", "")
		stufen[5] = erfuellt(p.GemergtAm, "")
		var offen []string
		for _, n := range nachsorge {
			if !n.Erledigt {
				offen = append(offen, n.Text)
			}
		}
		if len(offen) == 0 {
			stufen[6] = erfuellt(e.IssueZu, "")
		} else {
			stufen[6] = steht(strings.Join(offen, " · "))
		}
	default:
		if p.Draft {
			stufen[1] = steht(fmt.Sprintf("PR #%d ist ein Entwurf", p.Nummer))
		} else {
			stufen[1] = erfuellt(p.Erstellt, "")
		}
		stufen[2] = pruefungStufe(p.Checks)
		stufen[3] = reviewStufe(p.Reviews)
		stufen[4] = konfliktStufe(p)
		switch {
		case p.Zustand == "closed":
			stufen[5] = blockiert("PR ohne Merge geschlossen")
		case p.AutoMerge != nil:
			stufen[5] = steht("Auto-Merge eingeschaltet")
		default:
			stufen[5] = steht("Bereit zum Mergen")
		}
		stufen[6] = steht("")
	}
	alle := true
	for _, s := range stufen[:7] {
		alle = alle && s.erfuellt
	}
	if alle {
		stufen[7] = erfuellt(stufen[6].zeit, "")
	} else {
		stufen[7] = steht("")
	}

	out := Lebenslauf{Stufe: StufeFertig, Stufen: make([]LebenslaufStufe, len(stufen)), Nachsorge: nachsorge}
	erste := true
	for i, s := range stufen {
		st := LebenslaufStufe{Name: stufenFolge[i].name, Titel: stufenFolge[i].titel, Grund: s.grund}
		switch {
		case s.erfuellt:
			st.Zustand, st.Zeit = StufeErledigt, s.zeit
		case s.blockiert:
			st.Zustand = StufeBlockiert
		case erste:
			st.Zustand = StufeJetzt
		default:
			st.Zustand = StufeOffen
		}
		if !s.erfuellt && erste {
			out.Stufe, erste = st.Name, false
		}
		out.Stufen[i] = st
	}
	return out
}

// issueNummernVon nennt die Issues, die ein PR in Text oder Titel nennt, in
// der Folge ihres Auftretens.
func issueNummernVon(p ghrest.PR) []int {
	return append(nummernAus(schliesstRe, p.Text), nummernAus(titelRe, p.Titel)...)
}

// gemergtFuerIssue sucht unter den geschlossenen PRs den zuletzt gemergten,
// der zum Issue gehoert: er nennt es, sein Head ist change/KENNUNG, oder der
// Head-Name traegt die Nummer.
func gemergtFuerIssue(prs []ghrest.PR, nr int, kennung string) *ghrest.PR {
	var treffer []ghrest.PR
	for _, p := range prs {
		if !p.Gemergt {
			continue
		}
		passt := kennung != "" && p.Head == "change/"+kennung
		for _, n := range append(issueNummernVon(p), append(nummernAus(nameVorneRe, p.Head), nummernAus(nameHintenRe, p.Head)...)...) {
			passt = passt || n == nr
		}
		if passt {
			treffer = append(treffer, p)
		}
	}
	if len(treffer) == 0 {
		return nil
	}
	sort.SliceStable(treffer, func(a, b int) bool { return treffer[a].GemergtAm > treffer[b].GemergtAm })
	return &treffer[0]
}

// lebenslaufSetzen traegt den Lebenslauf an Reihe und zuletzt und je
// offenem PR in PRLaeufe ein.
func lebenslaufSetzen(out *RepoArbeit, e ArbeitEingabe) {
	vorhanden := map[string]bool{}
	for _, b := range e.Branches {
		vorhanden[b.Name] = true
	}
	offenePR := map[int]*ghrest.PR{}
	for i := range out.PRs {
		offenePR[out.PRs[i].Nummer] = &out.PRs[i]
	}
	kopfZeit := func(name string) string { return e.BranchKoepfe[name].Zeit }
	offenNr := map[int]bool{}
	for _, ein := range out.Reihe {
		offenNr[ein.Nummer] = true
	}

	fuerIssue := func(ein *ArbeitsEintrag, offen bool) *Lebenslauf {
		le := LebenslaufEingabe{Repo: e.Repo, Issue: ein.Nummer, IssueOffen: offen, IssueZu: ein.Geschlossen,
			Blocker: ein.BlockiertDurch, Change: ein.Change}
		for _, b := range ein.Branches {
			if b.PR != nil && offenePR[b.PR.Nummer] != nil {
				le.PR, le.Branch, le.BranchZeit, le.BranchDa = offenePR[b.PR.Nummer], b.Name, b.Zeit, true
				break
			}
		}
		if le.PR == nil {
			kennung := ""
			if ein.Change != nil {
				kennung = ein.Change.Kennung
			}
			le.PR = gemergtFuerIssue(e.GeschlossenePRs, ein.Nummer, kennung)
			if le.PR != nil {
				le.Branch = le.PR.Head
				le.BranchDa = vorhanden[le.Branch] && (le.PR.HeadRepo == "" || strings.EqualFold(le.PR.HeadRepo, e.Repo))
				le.BranchZeit = kopfZeit(le.Branch)
			} else if !offen {
				return nil
			} else if len(ein.Branches) > 0 {
				b := ein.Branches[0]
				le.Branch, le.BranchZeit, le.BranchDa = b.Name, b.Zeit, true
			}
		}
		l := LebenslaufBauen(le)
		return &l
	}
	for i := range out.Reihe {
		if out.Reihe[i].Art == "issue" {
			out.Reihe[i].Lebenslauf = fuerIssue(&out.Reihe[i], true)
		}
	}
	for i := range out.Zuletzt {
		out.Zuletzt[i].Lebenslauf = fuerIssue(&out.Zuletzt[i], false)
	}

	out.PRLaeufe = map[int]*Lebenslauf{}
	for i := range out.PRs {
		p := &out.PRs[i]
		le := LebenslaufEingabe{Repo: e.Repo, PR: p, Branch: p.Head, BranchDa: vorhanden[p.Head], BranchZeit: kopfZeit(p.Head)}
		for _, n := range issueNummernVon(*p) {
			le.Issue, le.IssueOffen = n, offenNr[n]
			break
		}
		for _, c := range e.Changes {
			if p.Head == "change/"+c.Kennung {
				le.Change = changeKurz(c)
				break
			}
		}
		l := LebenslaufBauen(le)
		out.PRLaeufe[p.Nummer] = &l
	}
}
