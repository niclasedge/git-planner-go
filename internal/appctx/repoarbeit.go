package appctx

// Die Arbeitsliste eines Repos (repo-leiste, #25): welche Issues offen sind,
// in welcher Folge sie abzuarbeiten sind, welcher Change an welchem haengt,
// wie weit seine Aufgaben sind, und was zuletzt fertig wurde. Die Folge
// entsteht HIER und nirgends sonst — die Oberflaeche zeichnet sie nur.

import (
	"sort"
	"strings"
	"time"

	"github.com/niclasedge/git-planner-go/internal/anzeigezeit"
	"github.com/niclasedge/git-planner-go/internal/collectors"
	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

// Die Status eines Eintrags.
const (
	ArbeitErledigt   = "erledigt"
	ArbeitImPR       = "im_pr"
	ArbeitFertig     = "fertig"
	ArbeitInArbeit   = "in_arbeit"
	ArbeitBereit     = "bereit"
	ArbeitBlockiert  = "blockiert"
	ArbeitOhneChange = "ohne_change"
)

// zuletztAnzahl ist die Zahl der erledigten Eintraege, die als Referenz oben
// stehen.
const zuletztAnzahl = 5

// ArbeitEingabe ist alles, was die Liste braucht — bereits geholt.
type ArbeitEingabe struct {
	Repo string
	// Offen sind die offenen Issues samt PRs (ghrest.Issue.IstPR).
	Offen []ghrest.Issue
	// Geschlossen sind die geschlossenen Issues ohne PRs, neueste zuerst.
	Geschlossen []ghrest.Issue
	// Blocker nennt je Issue die Nummern seiner offenen Blocker. Fehlt ein
	// Issue mit ghrest.Issue.Blocker > 0 hier, gilt es trotzdem als
	// blockiert (karte-blocker-streng, #101).
	Blocker map[int][]int
	// ErledigteBlocker nennt je offenem Issue seine geschlossenen Blocker
	// desselben Repos; sie bleiben als erledigte Eintraege ueber ihm in der
	// Reihe (reihe-erledigte-blocker, #130).
	ErledigteBlocker map[int][]ghrest.Issue
	Changes          []collectors.OpenspecChange
	// PRs ist die PR-Liste mit Draft und Mergebarkeit (pr-aktionen, #32).
	// nil heisst: nicht geholt — dann gelten die PR-Eintraege aus Offen.
	PRs []ghrest.PR
	// Quellen meldet je Abruf (issues, openspec, …) einen Grund, wenn er
	// scheiterte.
	Quellen []ArbeitQuelle
	// Jetzt ist der Zeitpunkt, gegen den die Faelligkeit gerechnet wird;
	// leer heisst anzeigezeit.Jetzt() (faelligkeit, #39; letzte-aktionen, #126).
	Jetzt time.Time
	// Standard ist der Default-Branch; Branches sind alle Branches des
	// Repos, BranchKoepfe je verknuepftem Branch Commit und Vergleich.
	// AktivFenster leer heisst BranchAktivFenster (issue-branches, #50).
	Standard     string
	Branches     []ghrest.Branch
	BranchKoepfe map[string]BranchKopf
	AktivFenster time.Duration
	// GeschlossenePRs sind die zuletzt geschlossenen PRs; aus ihnen kommt
	// der gemergte PR eines Issues (pr-lebenslauf, #83).
	GeschlossenePRs []ghrest.PR
	// Schluesse ist je Issue aus zuletzt, was seine Timeline hergab; aus
	// ihm kommt Regel 1 von gemergt_mit. Fehlt ein Issue, gelten nur die
	// Regeln 2 und 3 (zuletzt-erledigt-pr, #116).
	Schluesse map[int]IssueSchluss
	// RepoLabels sind die angelegten Labels des Repos, RepoLabelsGeholt sagt,
	// ob der Abruf gelang. Nur mit Rollen-Label im Repo gilt "Rolle fehlt";
	// nicht geholt heisst: die Rollen-Regel gilt (karte-gruppen, #121).
	RepoLabels       []ghrest.Label
	RepoLabelsGeholt bool
	// Aktivitaet ist die Repo-Aktivitaet (nil: nicht geholt), Commits je SHA
	// Zeit, Betreff und Autor der Commits, die der letzte Commit braucht
	// (karte-einzeilig, #124).
	Aktivitaet []ghrest.Aktivitaet
	Commits    map[string]ghrest.CommitKopf
	// CIRuns, Bilder und HTML sind die Speicher der Abschnitte CI,
	// "Geaenderte Bilder" und "HTML-Dateien" fuer den Zeitstrom; nil heisst:
	// liegt nicht vor (letzte-aktionen, #126).
	CIRuns []ghrest.WorkflowRun
	Bilder *GeaenderteBilder
	HTML   *HTMLDateien
	// Vergleiche sind je VergleichSchluessel Commits und Dateien eines
	// Pushes, BildURL die Bildadresse des Dienstes (aktionen-zeitleiste, #128).
	Vergleiche map[string]ghrest.PushVergleich
	BildURL    func(sha, name string) string
}

// ArbeitQuelle meldet einen Abruf.
type ArbeitQuelle struct {
	Quelle string `json:"quelle"`
	Grund  string `json:"grund"`
}

// ChangeKurz ist ein Change, wie ihn die Zeile und die Karte brauchen.
type ChangeKurz struct {
	Kennung    string                       `json:"kennung"`
	Ref        string                       `json:"ref"`
	Archiviert bool                         `json:"archiviert"`
	Status     string                       `json:"status"`
	Issue      int                          `json:"issue,omitempty"`
	Aufgaben   []collectors.OpenspecAufgabe `json:"aufgaben"`
	Erledigt   int                          `json:"erledigt"`
	Gesamt     int                          `json:"gesamt"`
	Gates      []string                     `json:"gates"`
	Belege     []collectors.OpenspecBeleg   `json:"belege"`
	// Schritt ist der naechste Schritt (naechster-schritt, #52).
	Schritt *Schritt `json:"schritt,omitempty"`
}

// ArbeitsEintrag ist eine Zeile der Arbeitsliste: ein Issue (mit oder ohne
// Change) oder ein Change ohne Issue.
type ArbeitsEintrag struct {
	Art    string `json:"art"` // "issue" oder "change"
	Nummer int    `json:"nummer,omitempty"`
	Titel  string `json:"titel"`
	URL    string `json:"url,omitempty"`
	Status string `json:"status"`
	// Grund nennt bei hygiene, was aufzuraeumen ist, und bei
	// wartet_auf_mensch das Label der Rolle (karte-rollen, #102).
	Grund          string         `json:"grund,omitempty"`
	BlockiertDurch []int          `json:"blockiert_durch"`
	Labels         []ghrest.Label `json:"labels"`
	Zustaendig     []string       `json:"zustaendig"`
	Autor          string         `json:"autor,omitempty"`
	Aktualisiert   string         `json:"aktualisiert,omitempty"`
	Geschlossen    string         `json:"geschlossen,omitempty"`
	// GeschlossenText ist die Schliesszeit als Text der Zeile ("heute
	// 14:32" oder "27. Sept."), GeschlossenVoll Datum und Uhrzeit fuer den
	// Tooltip, GemergtMit der PR, mit dem die Arbeit gemergt wurde; nur in
	// zuletzt (zuletzt-erledigt-pr, #116).
	GeschlossenText string      `json:"geschlossen_text,omitempty"`
	GeschlossenVoll string      `json:"geschlossen_voll,omitempty"`
	GemergtMit      *GemergtMit `json:"gemergt_mit,omitempty"`
	HatText         bool        `json:"hat_text"`
	Faellig         string      `json:"faellig,omitempty"`
	Termin          string      `json:"termin,omitempty"`
	Tage            int         `json:"tage,omitempty"`
	Change          *ChangeKurz `json:"change,omitempty"`
	// Tiefe ist die Einruecktiefe in der Reihe, Unter die Nummer des
	// Blockers, unter dem der Eintrag haengt (liste-abhaengigkeiten, #48).
	Tiefe int `json:"tiefe"`
	Unter int `json:"unter,omitempty"`
	// Vorfahren ist die Kette der Unter bis zur Wurzel, der naechste zuerst;
	// Abhaengige zaehlt, was der Eintrag blockiert (aktionen-zeitleiste, #128).
	Vorfahren  []int       `json:"vorfahren,omitempty"`
	Abhaengige *Abhaengige `json:"abhaengige,omitempty"`
	// ErledigteBlocker sind die geschlossenen Blocker des Eintrags; sie
	// blockieren nicht, stehen aber ueber ihm (reihe-erledigte-blocker, #130).
	ErledigteBlocker []int `json:"erledigte_blocker,omitempty"`
	// Branches sind die verknuepften Branches, Aktiv sagt, ob einer davon
	// im Fenster einen Commit hat (issue-branches, #50).
	Branches []IssueBranch `json:"branches"`
	Aktiv    bool          `json:"aktiv"`
	// Anlegen schlaegt Branch oder PR vor (pr-anlegen, #79).
	Anlegen *Anlegen `json:"anlegen,omitempty"`
	// Lebenslauf ist die Stufenleiste samt Nachsorge (pr-lebenslauf, #83).
	Lebenslauf *Lebenslauf `json:"lebenslauf,omitempty"`
	// ZuletztGeaendert ist die letzte Aenderung aus Issue, offenem PR und
	// Branch; nur in der Reihe (naechste-geaendert, #118).
	ZuletztGeaendert *ZuletztGeaendert `json:"zuletzt_geaendert,omitempty"`
}

// RepoArbeit ist die Antwort von GET /api/arbeit.
type RepoArbeit struct {
	Repo string `json:"repo"`
	// Fortschritt zaehlt die Aufgaben der nicht archivierten Changes.
	Fortschritt Fortschritt      `json:"fortschritt"`
	Zuletzt     []ArbeitsEintrag `json:"zuletzt"`
	Reihe       []ArbeitsEintrag `json:"reihe"`
	OhneIssue   []ArbeitsEintrag `json:"ohne_issue"`
	PRs         []ghrest.PR      `json:"prs"`
	// Archiv sind die archivierten Changes, neueste zuerst (archiv-liste, #56).
	Archiv  []ArchivEintrag `json:"archiv"`
	Quellen []ArbeitQuelle  `json:"quellen"`
	// PRLaeufe ist der Lebenslauf je offenem PR, nach Nummer (pr-lebenslauf, #83).
	PRLaeufe map[int]*Lebenslauf `json:"pr_laeufe"`
	// KarteKopf sind die Zahlen der eingeklappten Karte, LetzterCommit die
	// oberste Zeile von "Letzte Aktionen" (karte-einzeilig, #124).
	KarteKopf     KarteKopf      `json:"karte_kopf"`
	LetzterCommit *LetzterCommit `json:"letzter_commit,omitempty"`
	// LetzteAktionen ist der Zeitstrom von "Letzte Aktionen"; letzter_commit
	// bleibt fuer aeltere Leser (letzte-aktionen, #126).
	LetzteAktionen []LetzteAktion `json:"letzte_aktionen"`
	Sync           *SyncStand     `json:"sync,omitempty"`

	// offeneReihe ist die Reihe ohne die erledigten Blocker, in ihrer Folge;
	// aus ihr baut die Karte (reihe-erledigte-blocker, #130).
	offeneReihe []ArbeitsEintrag
}

// OffeneReihe ist die Reihe ohne die erledigten Blocker in der Folge, in der
// sie ohne sie stuende; Karte und Kartengruppen rechnen ueber sie.
func (w RepoArbeit) OffeneReihe() []ArbeitsEintrag {
	if w.offeneReihe != nil {
		return w.offeneReihe
	}
	return w.Reihe
}

func changeKurz(c collectors.OpenspecChange) *ChangeKurz {
	k := &ChangeKurz{Kennung: c.Kennung, Ref: c.Ref, Archiviert: c.Archiviert, Status: c.Status(), Issue: c.Issue,
		Aufgaben: c.Aufgaben, Erledigt: c.Erledigt(), Gesamt: len(c.Aufgaben), Gates: c.Gates, Belege: c.Belege}
	if k.Aufgaben == nil {
		k.Aufgaben = []collectors.OpenspecAufgabe{}
	}
	if k.Gates == nil {
		k.Gates = []string{}
	}
	if k.Belege == nil {
		k.Belege = []collectors.OpenspecBeleg{}
	}
	return k
}

func issueEintrag(i ghrest.Issue) ArbeitsEintrag {
	e := ArbeitsEintrag{Art: "issue", Nummer: i.Nummer, Titel: i.Titel, URL: i.URL, BlockiertDurch: []int{},
		Labels: i.Labels, Zustaendig: i.Zustaendig, Autor: i.Autor, Aktualisiert: i.Aktualisiert,
		Geschlossen: i.Geschlossen, HatText: strings.TrimSpace(i.Text) != "", Branches: []IssueBranch{}}
	if e.Labels == nil {
		e.Labels = []ghrest.Label{}
	}
	if e.Zustaendig == nil {
		e.Zustaendig = []string{}
	}
	return e
}

// statusAusChange uebersetzt den Stand eines Changes; "" heisst: der Change
// sagt nichts. Ein blockiertes Issue fragt ihn nicht (karte-blocker-streng, #101).
func statusAusChange(c *ChangeKurz) string {
	if c == nil {
		return ""
	}
	switch c.Status {
	case collectors.OpenspecImPR:
		return ArbeitImPR
	case collectors.OpenspecFertig, collectors.OpenspecErledigt:
		return ArbeitFertig
	case collectors.OpenspecLaeuft:
		return ArbeitInArbeit
	}
	return ""
}

// rang ordnet die Status, die vor der topologischen Folge stehen: was am
// naechsten am Ziel ist, zuerst.
func rang(status string) int {
	switch status {
	case ArbeitImPR:
		return 0
	case ArbeitFertig:
		return 1
	case ArbeitInArbeit:
		return 2
	}
	return 3
}

// RepoArbeitBauen baut die Arbeitsliste.
func RepoArbeitBauen(e ArbeitEingabe) RepoArbeit {
	out := RepoArbeit{Repo: e.Repo, Zuletzt: []ArbeitsEintrag{}, Reihe: []ArbeitsEintrag{},
		OhneIssue: []ArbeitsEintrag{}, PRs: []ghrest.PR{}, Quellen: e.Quellen}
	if out.Quellen == nil {
		out.Quellen = []ArbeitQuelle{}
	}

	var offen []ghrest.Issue
	offenNr := map[int]bool{}
	for _, i := range e.Offen {
		if i.IstPR {
			if e.PRs == nil {
				out.PRs = append(out.PRs, ghrest.PR{Nummer: i.Nummer, Titel: i.Titel, URL: i.URL, Autor: i.Autor,
					Labels: i.Labels, Aktualisiert: i.Aktualisiert})
			}
			continue
		}
		offen = append(offen, i)
		offenNr[i.Nummer] = true
	}
	if e.PRs != nil {
		out.PRs = append(out.PRs, e.PRs...)
	}
	for i := range out.PRs {
		if out.PRs[i].Labels == nil {
			out.PRs[i].Labels = []ghrest.Label{}
		}
	}
	sort.SliceStable(out.PRs, func(a, b int) bool { return out.PRs[a].Nummer < out.PRs[b].Nummer })

	// Changes je Issue: der aktive vor dem archivierten (changesJeIssue).
	aktivJe, archivJe := changesJeIssue(offen, e.Changes)

	// Fortschritt und Changes ohne offenes Issue.
	for i := range e.Changes {
		c := e.Changes[i]
		if c.Archiviert {
			continue
		}
		out.Fortschritt.Erledigt += c.Erledigt()
		out.Fortschritt.Gesamt += len(c.Aufgaben)
		// Mit Herkunftszeile verknuepft, auch auf ein geschlossenes Issue
		// (wie just lage; karte-herkunft-streng, #103).
		zugeordnet := c.Issue != 0
		for nr, a := range aktivJe {
			if a.Kennung == c.Kennung && a.Ordner == c.Ordner && offenNr[nr] {
				zugeordnet = true
			}
		}
		if !zugeordnet {
			k := changeKurz(c)
			status := statusAusChange(k)
			if status == "" {
				status = ArbeitBereit
			}
			out.OhneIssue = append(out.OhneIssue, ArbeitsEintrag{Art: "change", Titel: c.Kennung, Status: status,
				BlockiertDurch: []int{}, Labels: []ghrest.Label{}, Zustaendig: []string{}, Change: k,
				Branches: []IssueBranch{}})
		}
	}

	// Eintraege mit Status.
	jetzt := e.Jetzt
	if jetzt.IsZero() {
		jetzt = anzeigezeit.Jetzt()
	}
	branches := branchesJeIssue(e, jetzt)
	rollenGelten := !e.RepoLabelsGeholt || RepoNutztRollen(e.RepoLabels)
	eintraege := map[int]*ArbeitsEintrag{}
	for _, i := range offen {
		ein := issueEintrag(i)
		if bs := branches[i.Nummer]; bs != nil {
			ein.Branches = bs
			for _, b := range bs {
				ein.Aktiv = ein.Aktiv || b.Aktiv
			}
		}
		ein.Faellig = FaelligAus(i.Text)
		ein.Termin, ein.Tage = termin(ein.Faellig, jetzt)
		if c := aktivJe[i.Nummer]; c != nil {
			ein.Change = changeKurz(*c)
		} else if c := archivJe[i.Nummer]; c != nil {
			// Der Change ist schon archiviert, das Issue nur nicht
			// geschlossen (ohne "closes #N" im PR): fertig, nicht "ohne
			// Change" (issue-verwaltung, #26).
			ein.Change = changeKurz(*c)
		}
		for _, b := range e.Blocker[i.Nummer] {
			if offenNr[b] && b != i.Nummer {
				ein.BlockiertDurch = append(ein.BlockiertDurch, b)
			}
		}
		sort.Ints(ein.BlockiertDurch)
		for _, b := range e.ErledigteBlocker[i.Nummer] {
			if b.Nummer != i.Nummer && !offenNr[b.Nummer] {
				ein.ErledigteBlocker = append(ein.ErledigteBlocker, b.Nummer)
			}
		}
		sort.Ints(ein.ErledigteBlocker)
		// Blocker streng wie lage (karte-blocker-streng, #101): zaehlt die
		// Zusammenfassung mehr offene Blocker, als die Reihe kennt (Abfrage
		// gescheitert, Blocker ausserhalb der offenen Liste), bleibt das
		// Issue blockiert; und blockiert geht vor dem Stand des Changes.
		blockiert := len(ein.BlockiertDurch) > 0 || i.Blocker > len(ein.BlockiertDurch)
		switch {
		case blockiert:
			ein.Status = ArbeitBlockiert
		case statusAusChange(ein.Change) != "":
			ein.Status = statusAusChange(ein.Change)
		default:
			// Sonst entscheiden die Rollen-Labels (karte-rollen, #102).
			ein.Status, ein.Grund = rollenStatus(i, ein.Change != nil, offenNr, rollenGelten)
		}
		ein.Anlegen = AnlegenVorschlag(ein, e.Standard)
		kopie := ein
		eintraege[i.Nummer] = &kopie
	}

	out.Reihe = reiheBauen(eintraege)

	// Die letzten fuenf erledigten.
	for _, i := range ZuletztAuswahl(e.Geschlossen) {
		ein := issueEintrag(i)
		ein.Status = ArbeitErledigt
		if c := archivJe[i.Nummer]; c != nil {
			ein.Change = changeKurz(*c)
		} else if c := aktivJe[i.Nummer]; c != nil {
			ein.Change = changeKurz(*c)
		}
		zuletztErgaenzen(&ein, i, e, jetzt)
		out.Zuletzt = append(out.Zuletzt, ein)
	}
	schritteSetzen(out.Reihe, out.PRs, false)
	schritteSetzen(out.OhneIssue, out.PRs, false)
	schritteSetzen(out.Zuletzt, out.PRs, true)
	lebenslaufSetzen(&out, e)
	geaendertSetzen(out.Reihe, out.PRs, jetzt)
	out.Archiv = archivListe(e.Changes)
	out.KarteKopf = arbeitKarteKopf(e, out.Reihe, jetzt)
	out.LetzterCommit = letzterCommitBauen(e, out.PRs, jetzt)
	out.LetzteAktionen = LetzteAktionenBauen(AktionenAusArbeit(e, out, jetzt))
	// Zuletzt die erledigten Blocker: alles davor rechnet nur ueber die
	// offenen Issues (reihe-erledigte-blocker, #130).
	erledigte := erledigteBlockerEintraege(out.Reihe, e, aktivJe, archivJe, jetzt)
	if len(erledigte) > 0 {
		out.offeneReihe = append([]ArbeitsEintrag{}, out.Reihe...)
		alle := map[int]*ArbeitsEintrag{}
		for i := range out.Reihe {
			kopie := out.Reihe[i]
			alle[kopie.Nummer] = &kopie
		}
		for nr, ein := range erledigte {
			alle[nr] = ein
		}
		out.Reihe = reiheBauen(alle)
	}
	zusammen := append([]ghrest.Issue{}, offen...)
	for _, l := range e.ErledigteBlocker {
		zusammen = append(zusammen, l...)
	}
	abhaengigeSetzen(out.Reihe, zusammen)
	return out
}

// erledigteBlockerEintraege sind die geschlossenen Blocker, die ein Eintrag
// der Reihe in erledigte_blocker nennt, je Nummer einmal, als erledigte
// Eintraege mit Schliesszeit.
func erledigteBlockerEintraege(reihe []ArbeitsEintrag, e ArbeitEingabe, aktivJe, archivJe map[int]*collectors.OpenspecChange, jetzt time.Time) map[int]*ArbeitsEintrag {
	genannt := map[int]bool{}
	for _, ein := range reihe {
		for _, b := range ein.ErledigteBlocker {
			genannt[b] = true
		}
	}
	out := map[int]*ArbeitsEintrag{}
	for _, l := range e.ErledigteBlocker {
		for _, i := range l {
			if !genannt[i.Nummer] || out[i.Nummer] != nil {
				continue
			}
			ein := issueEintrag(i)
			ein.Status = ArbeitErledigt
			if c := archivJe[i.Nummer]; c != nil {
				ein.Change = changeKurz(*c)
			} else if c := aktivJe[i.Nummer]; c != nil {
				ein.Change = changeKurz(*c)
			}
			zuletztErgaenzen(&ein, i, e, jetzt)
			out[i.Nummer] = &ein
		}
	}
	return out
}

// reiheBauen gibt die Eintraege als Tiefensuche ueber den Wald aus
// blocked_by aus (liste-abhaengigkeiten, #48). Wurzeln sind erst die
// gerankten Eintraege (im_pr, fertig, in_arbeit; was am naechsten am Ziel
// ist, zuerst, dann nach Nummer), dann die uebrigen ohne offenen Blocker
// nach entsperrt absteigend, dann nach Nummer (karte-reihe-entsperrt, #104):
// entsperrt ist die Zahl der Eintraege, deren blockiert_durch ihn nennt. Nach
// jedem Eintrag X folgen sofort, nach Nummer und rekursiv, die Eintraege,
// deren offene Blocker mit X alle ausgegeben sind — als Kinder von X. Ein
// Blocker steht damit vor dem, was er blockiert, und ein blockierter Eintrag
// direkt unter dem Teilbaum seines zuletzt ausgegebenen Blockers. Was ein
// Kreis uebrig laesst, steht am Ende nach Nummer auf Tiefe 0.
func reiheBauen(eintraege map[int]*ArbeitsEintrag) []ArbeitsEintrag {
	reihe := []ArbeitsEintrag{}
	nummern := make([]int, 0, len(eintraege))
	for nr := range eintraege {
		nummern = append(nummern, nr)
	}
	sort.Ints(nummern)
	// blocker sind die offenen und, soweit sie in der Reihe stehen, die
	// erledigten Blocker eines Eintrags (reihe-erledigte-blocker, #130).
	blocker := func(nr int) []int {
		out := append([]int{}, eintraege[nr].BlockiertDurch...)
		for _, b := range eintraege[nr].ErledigteBlocker {
			if eintraege[b] != nil {
				out = append(out, b)
			}
		}
		return out
	}
	// blockiert[b] sind die Eintraege, die auf b warten, nach Nummer.
	blockiert := map[int][]int{}
	for _, nr := range nummern {
		for _, b := range blocker(nr) {
			blockiert[b] = append(blockiert[b], nr)
		}
	}
	ausgegeben := map[int]bool{}
	frei := func(nr int) bool {
		for _, b := range blocker(nr) {
			if !ausgegeben[b] {
				return false
			}
		}
		return true
	}
	// rangVon ist der Rang einer Wurzel; ein erledigter Eintrag hat den
	// besten Rang seiner Kinder.
	rangVon := func(nr int) int {
		if eintraege[nr].Status != ArbeitErledigt {
			return rang(eintraege[nr].Status)
		}
		r := 3
		for _, k := range blockiert[nr] {
			r = min(r, rang(eintraege[k].Status))
		}
		return r
	}
	var ausgeben func(nr, tiefe, unter int)
	ausgeben = func(nr, tiefe, unter int) {
		ein := *eintraege[nr]
		ein.Tiefe, ein.Unter = tiefe, unter
		reihe = append(reihe, ein)
		ausgegeben[nr] = true
		for _, k := range blockiert[nr] {
			if !ausgegeben[k] && frei(k) {
				ausgeben(k, tiefe+1, nr)
			}
		}
	}

	var vorn, wurzeln []int
	for _, nr := range nummern {
		switch {
		case len(blocker(nr)) > len(eintraege[nr].BlockiertDurch):
			// Unter einem erledigten Blocker, keine eigene Wurzel.
		case rangVon(nr) < 3:
			vorn = append(vorn, nr)
		case len(eintraege[nr].BlockiertDurch) == 0:
			wurzeln = append(wurzeln, nr)
		}
	}
	sort.SliceStable(vorn, func(a, b int) bool {
		return rangVon(vorn[a]) < rangVon(vorn[b])
	})
	// Freie Wurzeln: was die meisten freigibt, zuerst; wurzeln ist schon
	// nach Nummer, SliceStable haelt das bei Gleichstand.
	sort.SliceStable(wurzeln, func(a, b int) bool {
		return len(blockiert[wurzeln[a]]) > len(blockiert[wurzeln[b]])
	})
	for _, nr := range append(vorn, wurzeln...) {
		if !ausgegeben[nr] {
			ausgeben(nr, 0, 0)
		}
	}
	// Ein Kreis in blocked_by: der Rest in Nummernfolge, statt ihn zu
	// verschweigen.
	for _, nr := range nummern {
		if !ausgegeben[nr] {
			ein := *eintraege[nr]
			ein.Tiefe, ein.Unter = 0, 0
			reihe = append(reihe, ein)
		}
	}
	return reihe
}

func titelTreffer(offen []ghrest.Issue, kennung string) (int, bool) {
	praefix := strings.ToLower(kennung) + ":"
	for _, i := range offen {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(i.Titel)), praefix) {
			return i.Nummer, true
		}
	}
	return 0, false
}

// AktionenAusArbeit ist die Eingabe des Zeitstroms aus einer Eingabe der
// Arbeitsliste und der daraus gebauten Liste: Aktivitaet, Commits und
// Speicher, Vergleiche und geschlossene PRs aus e, offene PRs und zuletzt
// aus w, Belege aus den Changes.
func AktionenAusArbeit(e ArbeitEingabe, w RepoArbeit, jetzt time.Time) AktionenEingabe {
	return AktionenEingabe{Repo: e.Repo, Standard: e.Standard, Aktivitaet: e.Aktivitaet, Commits: e.Commits,
		Vergleiche: e.Vergleiche, OffenePRs: w.PRs, GeschlossenePRs: e.GeschlossenePRs, BildURL: e.BildURL,
		CIRuns: e.CIRuns, Bilder: e.Bilder, HTML: e.HTML,
		Belege: BelegGalerieBauen(e.Repo, e.Changes).Belege, Zuletzt: w.Zuletzt, Jetzt: jetzt}
}
