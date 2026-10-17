package appctx

// Die Karte eines Repos (repo-karte, #36), seit repo-karte-folge (#99) als
// Folge statt als Zeitleiste: von links nach rechts die zuletzt erledigten
// Issues, die gerade laufenden und die naechsten der Reihe, jede Station auf
// der Linie ihrer Map. Offene Fragen (Label question/frage) stehen daneben als
// Ort des Staus: welche Stationen sie direkt sperren, wie viele transitiv an
// ihnen haengen (Hebel), und welche Kette die groesste haelt (kritischer
// Pfad). Auswahl, Zuordnung und Folge stehen hier fest; die Oberflaeche
// zeichnet nur, und die Folge selbst kommt aus RepoArbeitBauen.

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/niclasedge/git-planner-go/internal/collectors"
	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

// Die Zonen der Karte, von links nach rechts.
const (
	ZoneErledigt = "erledigt"
	ZoneJetzt    = "jetzt"
	ZoneNaechste = "naechste"
)

// Die Arten einer Kante.
const (
	KanteFrage = "frage"
	KanteAktiv = "aktiv"
	KanteFolge = "folge"
)

// KartenErledigtAnzahl und KartenNaechsteAnzahl sind die Zahl der Stationen
// in den Zonen erledigt und naechste.
const (
	KartenErledigtAnzahl = 10
	KartenNaechsteAnzahl = 10
)

// KartenMapEingabe ist eine Map mit ihren Sub-Issues. Subs leer heisst: nicht
// geholt — dann gilt sub_issues_summary der Map.
type KartenMapEingabe struct {
	Map  ghrest.Issue
	Subs []ghrest.Issue
}

// KartenEingabe ist alles, was die Karte braucht — bereits geholt.
type KartenEingabe struct {
	Repo  string
	Jetzt time.Time
	// Maps sind die Maps mit ihren Sub-Issues; sie geben Linien, Titel und
	// Fortschritt.
	Maps []KartenMapEingabe
	// Offen sind die offenen Issues (PRs sortiert KarteBauen aus): aus ihnen
	// kommen die Fragen samt Text, Parent-Issue und Map-Eigenschaft.
	Offen []ghrest.Issue
	// Geschlossen sind die zuletzt geschlossenen Issues, in beliebiger Folge.
	Geschlossen []ghrest.Issue
	// Reihe ist die Folge der Arbeitsliste (RepoArbeitBauen) samt Status,
	// offenen Blockern, Branches und Change.
	Reihe   []ArbeitsEintrag
	Quellen []ArbeitQuelle
	// Fuer die gemergten Zeilen der Gruppen (karte-gruppen, #121): die
	// Changes (Kennung am Issue), die zuletzt geschlossenen PRs und die
	// gespeicherten Timelines aus dem Stand der Arbeitsliste.
	Changes         []collectors.OpenspecChange
	GeschlossenePRs []ghrest.PR
	Schluesse       map[int]IssueSchluss
	// Projekt ist der Inhalt von PROJECT.md im Standard-Branch, leer ohne
	// Datei; aus ihm kommt das Ziel.
	Projekt string
}

// KartenStation ist ein Issue auf der Karte.
type KartenStation struct {
	Nummer int    `json:"nummer"`
	Titel  string `json:"titel"`
	URL    string `json:"url"`
	Zone   string `json:"zone"`
	// Slot ist die Stelle in der Zone, 0 ganz links.
	Slot  int `json:"slot"`
	Linie int `json:"linie"`
	// Rang ist 1..10 in naechste, sonst 0.
	Rang int `json:"rang"`
	// Marke steht an der Achse: Tag.Monat bei erledigt, der Rang bei
	// naechste, leer in jetzt.
	Marke      string `json:"marke"`
	ErledigtAm string `json:"erledigt_am"`
	WartetAuf  []int  `json:"wartet_auf"`
	Gesperrt   bool   `json:"gesperrt"`
	Bereit     bool   `json:"bereit"`
	Kritisch   bool   `json:"kritisch"`
	// Nur in jetzt: Branch, Stufe (gate1, gate2, im_pr), Aufgaben, PR und
	// dessen Check-Zustand.
	Branch           string `json:"branch,omitempty"`
	Stufe            string `json:"stufe,omitempty"`
	AufgabenErledigt int    `json:"aufgaben_erledigt,omitempty"`
	AufgabenGesamt   int    `json:"aufgaben_gesamt,omitempty"`
	PR               int    `json:"pr,omitempty"`
	Checks           string `json:"checks,omitempty"`
}

// KartenLinie ist eine Map (oder 0: ohne Map) mit ihren Stationen.
type KartenLinie struct {
	Nummer   int    `json:"nummer"`
	Titel    string `json:"titel"`
	URL      string `json:"url"`
	Erledigt int    `json:"erledigt"`
	Gesamt   int    `json:"gesamt"`
	Aktiv    int    `json:"aktiv"`
	Gesperrt int    `json:"gesperrt"`
}

// KartenFrage ist eine offene Frage.
type KartenFrage struct {
	Nummer    int    `json:"nummer"`
	Titel     string `json:"titel"`
	URL       string `json:"url"`
	Wortlaut  string `json:"wortlaut"`
	Wer       string `json:"wer"`
	SeitTagen int    `json:"seit_tagen"`
	Sperrt    []int  `json:"sperrt"`
	Hebel     int    `json:"hebel"`
	Prio      int    `json:"prio"`
	Kritisch  bool   `json:"kritisch"`
}

// KartenKante heisst: Nach wartet auf Von.
type KartenKante struct {
	Von      int    `json:"von"`
	Nach     int    `json:"nach"`
	Art      string `json:"art"`
	Kritisch bool   `json:"kritisch"`
}

// KartenZone nennt eine Zone und die Zahl ihrer Stationen.
type KartenZone struct {
	Name   string `json:"name"`
	Anzahl int    `json:"anzahl"`
}

// Karte ist die Antwort von GET /api/karte.
type Karte struct {
	Repo string `json:"repo"`
	// Stand ist Tag.Monat von jetzt (Kopf der Jetzt-Zone).
	Stand     string          `json:"stand"`
	Zonen     []KartenZone    `json:"zonen"`
	Linien    []KartenLinie   `json:"linien"`
	Stationen []KartenStation `json:"stationen"`
	Fragen    []KartenFrage   `json:"fragen"`
	Kanten    []KartenKante   `json:"kanten"`
	// Kopf sind die Zahlen der eingeklappten Zeile (karte-einzeilig, #124).
	Kopf KarteKopf `json:"kopf"`
	// Kritisch ist die Nummer der Frage, an der der kritische Pfad haengt;
	// 0, wenn keine Frage etwas sperrt.
	Kritisch int `json:"kritisch"`
	// Ziel ist der erste Absatz unter "## Ziel" in PROJECT.md, Gruppen die
	// Liste "Issues nach Parent" (karte-gruppen, #121).
	Ziel    string         `json:"ziel"`
	Gruppen []KartenGruppe `json:"gruppen"`
	Quellen []ArbeitQuelle `json:"quellen"`
	Sync    *SyncStand     `json:"sync,omitempty"`
}

func zeitAus(s string) (time.Time, bool) {
	t, err := time.Parse(time.RFC3339, s)
	return t, err == nil
}

// tagMonat ist Tag und Monat eines Zeitpunkts in der Zone zone (die von
// Jetzt der Karte, also die Anzeigezone; letzte-aktionen, #126); leer, wenn
// er sich nicht lesen laesst.
func tagMonat(s string, zone *time.Location) string {
	t, ok := zeitAus(s)
	if !ok {
		return ""
	}
	t = t.In(zone)
	return fmt.Sprintf("%d.%d.", t.Day(), int(t.Month()))
}

// IstFrage sagt, ob ein Issue eine offene Frage sein kann: Label question
// oder frage, Gross- und Kleinschreibung egal.
func IstFrage(labels []ghrest.Label) bool {
	for _, l := range labels {
		n := strings.ToLower(strings.TrimSpace(l.Name))
		if n == "question" || n == "frage" {
			return true
		}
	}
	return false
}

// ersteZeile ist die erste nicht leere Zeile eines Texts.
func ersteZeile(s string) string {
	for _, z := range strings.Split(s, "\n") {
		if z = strings.TrimSpace(z); z != "" {
			return z
		}
	}
	return ""
}

// stufeVon ist die Stufe eines laufenden Changes: im_pr, sonst das hoechste
// vorliegende Gate.
func stufeVon(ein ArbeitsEintrag) string {
	if ein.Status == ArbeitImPR {
		return "im_pr"
	}
	if ein.Change == nil {
		return ""
	}
	stufe := ""
	for _, g := range ein.Change.Gates {
		switch g {
		case "gate2", "fertig":
			stufe = "gate2"
		case "gate1":
			if stufe == "" {
				stufe = "gate1"
			}
		}
	}
	return stufe
}

// branchVon waehlt den Branch einer laufenden Station: der mit PR, sonst der
// erste aktive, sonst der erste.
func branchVon(bs []IssueBranch) (IssueBranch, bool) {
	for _, b := range bs {
		if b.PR != nil {
			return b, true
		}
	}
	for _, b := range bs {
		if b.Aktiv {
			return b, true
		}
	}
	if len(bs) > 0 {
		return bs[0], true
	}
	return IssueBranch{}, false
}

// KarteBauen baut die Karte.
func KarteBauen(e KartenEingabe) Karte {
	out := Karte{Repo: e.Repo, Stand: tagMonat(e.Jetzt.UTC().Format(time.RFC3339), e.Jetzt.Location()), Linien: []KartenLinie{},
		Stationen: []KartenStation{}, Fragen: []KartenFrage{}, Kanten: []KartenKante{}, Gruppen: []KartenGruppe{},
		Quellen: e.Quellen, Ziel: ZielAus(e.Projekt)}
	if out.Quellen == nil {
		out.Quellen = []ArbeitQuelle{}
	}

	// Maps, ihre Sub-Issues und die offenen Issues nachschlagbar machen.
	istMap := map[int]bool{}
	mapVon := map[int]int{} // Issue -> Map, deren Sub-Issue es ist
	mapIssue := map[int]KartenMapEingabe{}
	for _, m := range e.Maps {
		istMap[m.Map.Nummer] = true
		mapIssue[m.Map.Nummer] = m
		for _, s := range m.Subs {
			mapVon[s.Nummer] = m.Map.Nummer
		}
	}
	offen := map[int]ghrest.Issue{}
	for _, i := range e.Offen {
		if i.IstPR {
			continue
		}
		offen[i.Nummer] = i
		if i.SubGesamt > 0 {
			istMap[i.Nummer] = true
		}
	}
	linieVon := func(nr, parent int) int {
		if m, ok := mapVon[nr]; ok {
			return m
		}
		if parent > 0 {
			return parent
		}
		return 0
	}

	// Fragen: offene Issues mit Label question/frage; sie sind keine
	// Stationen, sondern stehen als eigene Liste nach Prio und Hebel.
	istFrage := map[int]bool{}
	for _, i := range e.Offen {
		if !i.IstPR && IstFrage(i.Labels) {
			istFrage[i.Nummer] = true
		}
	}

	// erledigt: die letzten zehn geschlossenen Issues ohne PRs und Maps,
	// aeltestes links.
	var zu []ghrest.Issue
	for _, i := range e.Geschlossen {
		if i.IstPR || i.SubGesamt > 0 || istMap[i.Nummer] {
			continue
		}
		zu = append(zu, i)
	}
	sort.SliceStable(zu, func(a, b int) bool { return zu[a].Geschlossen > zu[b].Geschlossen })
	if len(zu) > KartenErledigtAnzahl {
		zu = zu[:KartenErledigtAnzahl]
	}
	for slot := range zu {
		i := zu[len(zu)-1-slot]
		am := tagMonat(i.Geschlossen, e.Jetzt.Location())
		out.Stationen = append(out.Stationen, KartenStation{Nummer: i.Nummer, Titel: i.Titel, URL: i.URL, Zone: ZoneErledigt,
			Slot: slot, Linie: linieVon(i.Nummer, i.ParentNummer), Marke: am, ErledigtAm: am, WartetAuf: []int{}})
	}
	nErledigt := len(out.Stationen)

	// jetzt und naechste aus der Reihe, in deren Folge.
	var nJetzt, nNaechste int
	for _, ein := range e.Reihe {
		// Ein erledigter Blocker der Reihe ist keine offene Station
		// (reihe-erledigte-blocker, #130).
		if ein.Art != "issue" || ein.Nummer == 0 || istFrage[ein.Nummer] || istMap[ein.Nummer] || ein.Status == ArbeitErledigt {
			continue
		}
		parent := offen[ein.Nummer].ParentNummer
		s := KartenStation{Nummer: ein.Nummer, Titel: ein.Titel, URL: ein.URL, Linie: linieVon(ein.Nummer, parent),
			WartetAuf: append([]int{}, ein.BlockiertDurch...)}
		switch {
		case ein.Status == ArbeitInArbeit || ein.Status == ArbeitFertig || ein.Status == ArbeitImPR || ein.Aktiv:
			s.Zone, s.Slot = ZoneJetzt, nJetzt
			nJetzt++
			s.Stufe = stufeVon(ein)
			if ein.Change != nil {
				s.AufgabenErledigt, s.AufgabenGesamt = ein.Change.Erledigt, ein.Change.Gesamt
			}
			if b, ok := branchVon(ein.Branches); ok {
				s.Branch = b.Name
				if b.PR != nil {
					s.PR = b.PR.Nummer
					if b.PR.Checks != nil {
						s.Checks = b.PR.Checks.Gesamt
					}
				}
			}
		case nNaechste < KartenNaechsteAnzahl:
			s.Zone, s.Slot = ZoneNaechste, nNaechste
			nNaechste++
			s.Rang = nNaechste
			s.Marke = fmt.Sprint(s.Rang)
			// Bereit ist nur, was die Reihe bereit nennt (karte-rollen, #102).
			s.Bereit = ein.Status == ArbeitBereit
		default:
			continue
		}
		out.Stationen = append(out.Stationen, s)
	}
	out.Zonen = []KartenZone{{ZoneErledigt, nErledigt}, {ZoneJetzt, nJetzt}, {ZoneNaechste, nNaechste}}

	// Wer wartet auf wen: ueber alle offenen Blocker der Reihe, damit eine
	// Kette auch ueber Issues laeuft, die nicht auf der Karte stehen.
	kinder := map[int][]int{}
	for _, ein := range e.Reihe {
		for _, b := range ein.BlockiertDurch {
			kinder[b] = append(kinder[b], ein.Nummer)
		}
	}
	station := map[int]int{} // Nummer -> Index in out.Stationen
	for i, s := range out.Stationen {
		station[s.Nummer] = i
	}
	// haengtAn liefert alle Nummern, die transitiv an nr haengen.
	haengtAn := func(nr int) map[int]bool {
		gesehen := map[int]bool{}
		stapel := []int{nr}
		for len(stapel) > 0 {
			n := stapel[len(stapel)-1]
			stapel = stapel[:len(stapel)-1]
			for _, k := range kinder[n] {
				if !gesehen[k] && k != nr {
					gesehen[k] = true
					stapel = append(stapel, k)
				}
			}
		}
		return gesehen
	}

	jetzt := e.Jetzt.UTC()
	haengen := map[int]map[int]bool{}
	for _, i := range e.Offen {
		nr := i.Nummer
		if !istFrage[nr] || haengen[nr] != nil {
			continue
		}
		f := KartenFrage{Nummer: nr, Titel: i.Titel, URL: i.URL, Wortlaut: ersteZeile(i.Text), Sperrt: []int{}, Prio: 2}
		if len(i.Zustaendig) > 0 {
			f.Wer = i.Zustaendig[0]
		}
		if t, ok := zeitAus(i.Erstellt); ok && jetzt.After(t) {
			f.SeitTagen = int(jetzt.Sub(t).Hours() / 24)
		}
		for _, s := range out.Stationen {
			for _, w := range s.WartetAuf {
				if w == nr {
					f.Sperrt = append(f.Sperrt, s.Nummer)
				}
			}
		}
		sort.Ints(f.Sperrt)
		haengen[nr] = haengtAn(nr)
		for k := range haengen[nr] {
			if idx, ok := station[k]; ok {
				f.Hebel++
				out.Stationen[idx].Gesperrt = true
			}
		}
		if f.Hebel > 0 {
			f.Prio = 1
		}
		out.Fragen = append(out.Fragen, f)
	}
	sort.SliceStable(out.Fragen, func(a, b int) bool {
		fa, fb := out.Fragen[a], out.Fragen[b]
		if fa.Prio != fb.Prio {
			return fa.Prio < fb.Prio
		}
		if fa.Hebel != fb.Hebel {
			return fa.Hebel > fb.Hebel
		}
		return fa.Nummer < fb.Nummer
	})

	// Kritischer Pfad: die erste Frage mit Prio 1 und alles, was an ihr haengt.
	kritisch := map[int]bool{}
	if len(out.Fragen) > 0 && out.Fragen[0].Prio == 1 {
		f := &out.Fragen[0]
		f.Kritisch = true
		out.Kritisch = f.Nummer
		kritisch[f.Nummer] = true
		for k := range haengen[f.Nummer] {
			if idx, ok := station[k]; ok {
				out.Stationen[idx].Kritisch = true
				kritisch[k] = true
			}
		}
	}

	// Kanten: je Station und je Blocker, der auf der Karte steht.
	for _, s := range out.Stationen {
		for _, b := range s.WartetAuf {
			k := KartenKante{Von: b, Nach: s.Nummer, Kritisch: kritisch[b] && kritisch[s.Nummer]}
			if istFrage[b] {
				k.Art = KanteFrage
			} else if idx, ok := station[b]; ok {
				k.Art = KanteFolge
				if out.Stationen[idx].Zone == ZoneJetzt {
					k.Art = KanteAktiv
				}
			} else {
				continue
			}
			out.Kanten = append(out.Kanten, k)
		}
	}
	sort.Slice(out.Kanten, func(a, b int) bool {
		if out.Kanten[a].Von != out.Kanten[b].Von {
			return out.Kanten[a].Von < out.Kanten[b].Von
		}
		return out.Kanten[a].Nach < out.Kanten[b].Nach
	})

	// Linien: jede mit mindestens einer Station, nach Nummer, ohne Map zuletzt.
	linien := map[int]*KartenLinie{}
	for _, s := range out.Stationen {
		l := linien[s.Linie]
		if l == nil {
			l = &KartenLinie{Nummer: s.Linie}
			linien[s.Linie] = l
		}
		if s.Zone == ZoneJetzt {
			l.Aktiv++
		}
		if s.Gesperrt {
			l.Gesperrt++
		}
		if s.Linie == 0 {
			l.Gesamt++
			if s.Zone == ZoneErledigt {
				l.Erledigt++
			}
		}
	}
	// kopf ist Titel, URL und Fortschritt einer Map (fuer Linien und
	// Gruppen), aus ihren Sub-Issues, sonst aus sub_issues_summary.
	kopf := func(nr int) KartenLinie {
		l := KartenLinie{Nummer: nr, Titel: fmt.Sprintf("#%d", nr)}
		m, ok := mapIssue[nr]
		if !ok {
			if i, da := offen[nr]; da {
				m, ok = KartenMapEingabe{Map: i}, true
			}
		}
		if !ok {
			return l
		}
		if m.Map.Titel != "" {
			l.Titel = m.Map.Titel
		}
		l.URL = m.Map.URL
		if len(m.Subs) > 0 {
			l.Gesamt = len(m.Subs)
			for _, s := range m.Subs {
				if s.Zustand == "closed" {
					l.Erledigt++
				}
			}
		} else {
			l.Gesamt, l.Erledigt = m.Map.SubGesamt, m.Map.SubErledigt
		}
		return l
	}
	for nr, l := range linien {
		if nr == 0 {
			l.Titel = "ohne Map"
			continue
		}
		k := kopf(nr)
		l.Titel, l.URL, l.Erledigt, l.Gesamt = k.Titel, k.URL, k.Erledigt, k.Gesamt
	}
	for _, l := range linien {
		out.Linien = append(out.Linien, *l)
	}
	sort.Slice(out.Linien, func(a, b int) bool {
		na, nb := out.Linien[a].Nummer, out.Linien[b].Nummer
		if (na == 0) != (nb == 0) {
			return nb == 0
		}
		return na < nb
	})

	// Issues nach Parent (karte-gruppen, #121).
	frageLinie := map[int]int{}
	for _, f := range out.Fragen {
		frageLinie[f.Nummer] = linieVon(f.Nummer, offen[f.Nummer].ParentNummer)
	}
	out.Gruppen = gruppenBauen(e, out, gruppenHilfe{istFrage: istFrage, haengen: haengen, frageLinie: frageLinie,
		offen: offen, kopf: kopf})
	out.Kopf = KarteKopfAus(out)
	return out
}
