package appctx

// Issues nach Parent (karte-gruppen, #121, Entwurf H): unter dem Linienplan
// je Map eine Gruppe und eine "ohne Parent", jede mit ihren offenen Fragen,
// den laufenden, den ausstehenden (als Baum nach blocked_by) und den
// gemergten Issues. Dazu die Schaetzung aus dem Label schaetzung:N und das
// Ziel aus PROJECT.md. Gruppen, Folge, Zuordnung und Baum stehen hier fest;
// die Oberflaeche zeichnet nur.

import (
	"sort"
	"strconv"
	"strings"

	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

// Die Arten eines Chips: an einer Frage direkt oder transitiv gesperrt, an
// einer ausstehenden Zeile der Blocker als Frage, aktiver Change oder Issue.
const (
	ChipDirekt    = "direkt"
	ChipTransitiv = "transitiv"
	WartetFrage   = "frage"
	WartetAktiv   = "aktiv"
	WartetIssue   = "issue"
)

// Die Status einer Zeile der Gruppen.
const (
	ZeileAktiv    = "aktiv"
	ZeileGesperrt = "gesperrt"
	ZeileBereit   = "bereit"
	ZeileWartet   = "wartet"
	ZeileOffen    = "offen"
	ZeileGemergt  = "gemergt"
)

// KartenGemergtAnfangs ist die Zahl der gemergten Zeilen, die eine Gruppe
// zeigt, bevor "+ n weitere" aufgeklappt wird.
const KartenGemergtAnfangs = 3

// KartenChip ist eine Nummer mit ihrer Art.
type KartenChip struct {
	Nummer int    `json:"nummer"`
	Art    string `json:"art"`
}

// KartenGruppenFrage ist eine offene Frage in einer Gruppe.
type KartenGruppenFrage struct {
	Nummer     int          `json:"nummer"`
	Titel      string       `json:"titel"`
	URL        string       `json:"url"`
	Wortlaut   string       `json:"wortlaut"`
	Wer        string       `json:"wer"`
	SeitTagen  int          `json:"seit_tagen"`
	Hebel      int          `json:"hebel"`
	Prio       int          `json:"prio"`
	Chips      []KartenChip `json:"chips"`
	Schaetzung int          `json:"schaetzung"`
}

// KartenZeile ist ein Issue in den Abschnitten aktiv, ausstehend und gemergt.
type KartenZeile struct {
	Nummer int    `json:"nummer"`
	Titel  string `json:"titel"`
	URL    string `json:"url"`
	Status string `json:"status"`
	Rang   int    `json:"rang"`
	// Tiefe und Unter ordnen die ausstehenden Zeilen als Baum.
	Tiefe     int          `json:"tiefe"`
	Unter     int          `json:"unter,omitempty"`
	WartetAuf []KartenChip `json:"wartet_auf"`
	// HaengtAn ist die Frage, an der die Zeile nur ueber Umwege haengt.
	HaengtAn         int         `json:"haengt_an,omitempty"`
	Bereit           bool        `json:"bereit"`
	Gesperrt         bool        `json:"gesperrt"`
	Branch           string      `json:"branch,omitempty"`
	Stufe            string      `json:"stufe,omitempty"`
	AufgabenErledigt int         `json:"aufgaben_erledigt,omitempty"`
	AufgabenGesamt   int         `json:"aufgaben_gesamt,omitempty"`
	PR               int         `json:"pr,omitempty"`
	Checks           string      `json:"checks,omitempty"`
	Geaendert        string      `json:"geaendert,omitempty"`
	GeaendertVoll    string      `json:"geaendert_voll,omitempty"`
	Schaetzung       int         `json:"schaetzung"`
	ErledigtAm       string      `json:"erledigt_am,omitempty"`
	Change           string      `json:"change,omitempty"`
	GemergtMit       *GemergtMit `json:"gemergt_mit,omitempty"`
}

// KartenGruppe ist ein Parent-Issue (oder 0: ohne Parent) mit seinen Zeilen.
type KartenGruppe struct {
	Nummer           int    `json:"nummer"`
	Titel            string `json:"titel"`
	URL              string `json:"url"`
	Erledigt         int    `json:"erledigt"`
	Gesamt           int    `json:"gesamt"`
	AnzahlAktiv      int    `json:"anzahl_aktiv"`
	AnzahlAusstehend int    `json:"anzahl_ausstehend"`
	// P1Frage ist die Prio-1-Frage der Gruppe mit dem groessten Hebel.
	P1Frage        int                  `json:"p1_frage"`
	Hebel          int                  `json:"hebel"`
	Fragen         []KartenGruppenFrage `json:"fragen"`
	Aktiv          []KartenZeile        `json:"aktiv"`
	Ausstehend     []KartenZeile        `json:"ausstehend"`
	Gemergt        []KartenZeile        `json:"gemergt"`
	GemergtAnfangs int                  `json:"gemergt_anfangs"`
}

// gruppenHilfe ist, was KarteBauen fuer die Gruppen schon ausgerechnet hat.
type gruppenHilfe struct {
	istFrage   map[int]bool
	haengen    map[int]map[int]bool
	frageLinie map[int]int
	offen      map[int]ghrest.Issue
	kopf       func(nr int) KartenLinie
}

// SchaetzungAus liest N aus dem Label schaetzung:N (N ganzzahlig, >= 1);
// sonst 0.
func SchaetzungAus(labels []ghrest.Label) int {
	for _, l := range labels {
		rest, ok := strings.CutPrefix(labelName(l), "schaetzung:")
		if !ok {
			continue
		}
		if n, err := strconv.Atoi(strings.TrimSpace(rest)); err == nil && n >= 1 {
			return n
		}
	}
	return 0
}

// ZielAus liefert den ersten Absatz unter "## Ziel", die Zeilen mit einem
// Leerzeichen verbunden; leer ohne Abschnitt.
func ZielAus(md string) string {
	zeilen := strings.Split(strings.ReplaceAll(md, "\r\n", "\n"), "\n")
	for i, z := range zeilen {
		if strings.TrimSpace(z) != "## Ziel" {
			continue
		}
		var absatz []string
		for _, w := range zeilen[i+1:] {
			w = strings.TrimSpace(w)
			switch {
			case w == "" && len(absatz) == 0:
				continue
			case w == "" || strings.HasPrefix(w, "#"):
				return strings.Join(absatz, " ")
			}
			absatz = append(absatz, w)
		}
		return strings.Join(absatz, " ")
	}
	return ""
}

func zeileAusStation(s KartenStation) KartenZeile {
	return KartenZeile{Nummer: s.Nummer, Titel: s.Titel, URL: s.URL, Rang: s.Rang, WartetAuf: []KartenChip{},
		Bereit: s.Bereit, Gesperrt: s.Gesperrt, Branch: s.Branch, Stufe: s.Stufe, AufgabenErledigt: s.AufgabenErledigt,
		AufgabenGesamt: s.AufgabenGesamt, PR: s.PR, Checks: s.Checks, ErledigtAm: s.ErledigtAm}
}

// gruppenBauen baut die Gruppen aus der fertigen Karte k.
func gruppenBauen(e KartenEingabe, k Karte, h gruppenHilfe) []KartenGruppe {
	gruppen := map[int]*KartenGruppe{}
	neu := func(l KartenLinie) *KartenGruppe {
		g := &KartenGruppe{Nummer: l.Nummer, Titel: l.Titel, URL: l.URL, Erledigt: l.Erledigt, Gesamt: l.Gesamt,
			Fragen: []KartenGruppenFrage{}, Aktiv: []KartenZeile{}, Ausstehend: []KartenZeile{}, Gemergt: []KartenZeile{},
			GemergtAnfangs: KartenGemergtAnfangs}
		if l.Nummer == 0 {
			g.Titel = "ohne Parent"
		}
		gruppen[l.Nummer] = g
		return g
	}
	for _, l := range k.Linien {
		neu(l)
	}
	gruppe := func(nr int) *KartenGruppe {
		if g := gruppen[nr]; g != nil {
			return g
		}
		if nr == 0 {
			return neu(KartenLinie{})
		}
		return neu(h.kopf(nr))
	}

	station := map[int]KartenStation{}
	for _, s := range k.Stationen {
		station[s.Nummer] = s
	}
	reihe := map[int]ArbeitsEintrag{}
	for _, ein := range e.Reihe {
		if ein.Art == "issue" && ein.Status != ArbeitErledigt {
			reihe[ein.Nummer] = ein
		}
	}
	geschlossen := map[int]ghrest.Issue{}
	for _, i := range e.Geschlossen {
		geschlossen[i.Nummer] = i
	}
	aktivJe, archivJe := changesJeIssue(nil, e.Changes)

	// Die Fragen: in jeder Gruppe, in der eine Station aus sperrt liegt,
	// sonst in der Gruppe ihrer eigenen Linie.
	for _, f := range k.Fragen {
		gf := KartenGruppenFrage{Nummer: f.Nummer, Titel: f.Titel, URL: f.URL, Wortlaut: f.Wortlaut, Wer: f.Wer,
			SeitTagen: f.SeitTagen, Hebel: f.Hebel, Prio: f.Prio, Chips: []KartenChip{},
			Schaetzung: SchaetzungAus(h.offen[f.Nummer].Labels)}
		direkt := map[int]bool{}
		for _, n := range f.Sperrt {
			direkt[n] = true
		}
		var nummern []int
		for n := range h.haengen[f.Nummer] {
			if _, ok := station[n]; ok {
				nummern = append(nummern, n)
			}
		}
		sort.Ints(nummern)
		for _, n := range nummern {
			art := ChipTransitiv
			if direkt[n] {
				art = ChipDirekt
			}
			gf.Chips = append(gf.Chips, KartenChip{Nummer: n, Art: art})
		}
		var ziele []int
		gesehen := map[int]bool{}
		for _, n := range f.Sperrt {
			if l := station[n].Linie; !gesehen[l] {
				gesehen[l] = true
				ziele = append(ziele, l)
			}
		}
		if len(ziele) == 0 {
			ziele = []int{h.frageLinie[f.Nummer]}
		}
		for _, l := range ziele {
			g := gruppe(l)
			g.Fragen = append(g.Fragen, gf)
		}
	}

	// Die Zeilen der Stationen, in der Folge der Karte.
	var erledigt []KartenStation
	for _, s := range k.Stationen {
		switch s.Zone {
		case ZoneErledigt:
			erledigt = append(erledigt, s)
		case ZoneJetzt:
			z := zeileAusStation(s)
			z.Status = ZeileAktiv
			ein := reihe[s.Nummer]
			z.Schaetzung = SchaetzungAus(ein.Labels)
			if ein.ZuletztGeaendert != nil {
				z.Geaendert, z.GeaendertVoll = ein.ZuletztGeaendert.Text, ein.ZuletztGeaendert.Voll
			}
			g := gruppe(s.Linie)
			g.Aktiv = append(g.Aktiv, z)
		case ZoneNaechste:
			z := zeileAusStation(s)
			ein := reihe[s.Nummer]
			z.Schaetzung = SchaetzungAus(ein.Labels)
			if ein.ZuletztGeaendert != nil {
				z.Geaendert, z.GeaendertVoll = ein.ZuletztGeaendert.Text, ein.ZuletztGeaendert.Voll
			}
			frageDirekt := false
			for _, w := range s.WartetAuf {
				art := WartetIssue
				if h.istFrage[w] {
					art, frageDirekt = WartetFrage, true
				} else if b, ok := station[w]; ok && b.Zone == ZoneJetzt {
					art = WartetAktiv
				}
				z.WartetAuf = append(z.WartetAuf, KartenChip{Nummer: w, Art: art})
			}
			if s.Gesperrt && !frageDirekt {
				for _, f := range k.Fragen {
					if h.haengen[f.Nummer][s.Nummer] {
						z.HaengtAn = f.Nummer
						break
					}
				}
			}
			switch {
			case s.Gesperrt:
				z.Status = ZeileGesperrt
			case s.Bereit:
				z.Status = ZeileBereit
			case len(s.WartetAuf) > 0:
				z.Status = ZeileWartet
			default:
				z.Status = ZeileOffen
			}
			g := gruppe(s.Linie)
			g.Ausstehend = append(g.Ausstehend, z)
		}
	}
	// Gemergt: neueste zuerst.
	for i := len(erledigt) - 1; i >= 0; i-- {
		s := erledigt[i]
		z := zeileAusStation(s)
		z.Status = ZeileGemergt
		issue := geschlossen[s.Nummer]
		z.Schaetzung = SchaetzungAus(issue.Labels)
		z.Geaendert, z.GeaendertVoll = ZeitpunktText(issue.Geschlossen, e.Jetzt)
		if c := archivJe[s.Nummer]; c != nil {
			z.Change = c.Kennung
		} else if c := aktivJe[s.Nummer]; c != nil {
			z.Change = c.Kennung
		}
		var schluss *IssueSchluss
		if sch, ok := e.Schluesse[s.Nummer]; ok {
			schluss = &sch
		}
		if issue.Nummer != 0 {
			z.GemergtMit = GemergtMitFinden(e.Repo, issue, z.Change, schluss, e.GeschlossenePRs)
		}
		g := gruppe(s.Linie)
		g.Gemergt = append(g.Gemergt, z)
	}

	out := make([]KartenGruppe, 0, len(gruppen))
	for _, g := range gruppen {
		g.Ausstehend = baumOrdnen(g.Ausstehend)
		g.AnzahlAktiv, g.AnzahlAusstehend = len(g.Aktiv), len(g.Ausstehend)
		for _, f := range g.Fragen {
			if f.Prio == 1 && (g.P1Frage == 0 || f.Hebel > g.Hebel) {
				g.P1Frage, g.Hebel = f.Nummer, f.Hebel
			}
		}
		out = append(out, *g)
	}
	ersterRang := func(g KartenGruppe) int {
		r := 0
		for _, z := range g.Ausstehend {
			if r == 0 || z.Rang < r {
				r = z.Rang
			}
		}
		return r
	}
	sort.Slice(out, func(a, b int) bool {
		ga, gb := out[a], out[b]
		if (ga.Nummer == 0) != (gb.Nummer == 0) {
			return gb.Nummer == 0
		}
		if (ga.P1Frage != 0) != (gb.P1Frage != 0) {
			return ga.P1Frage != 0
		}
		if ga.P1Frage != 0 && ga.Hebel != gb.Hebel {
			return ga.Hebel > gb.Hebel
		}
		if ga.P1Frage == 0 {
			ra, rb := ersterRang(ga), ersterRang(gb)
			if (ra == 0) != (rb == 0) {
				return rb == 0
			}
			if ra != rb {
				return ra < rb
			}
		}
		return ga.Nummer < gb.Nummer
	})
	return out
}

// baumOrdnen ordnet die ausstehenden Zeilen einer Gruppe als Baum ueber
// wartet_auf, beschraenkt auf die Zeilen der Gruppe: jede unter ihrem
// Blocker mit dem groessten Rang, Geschwister nach Rang. Wurzeln stehen nach
// Rang, eine aber erst nach jeder Wurzel, in deren Teilbaum ein Blocker aus
// ihrem eigenen Teilbaum liegt. Was ein Kreis uebrig laesst, folgt nach Rang
// auf Tiefe 0.
func baumOrdnen(zs []KartenZeile) []KartenZeile {
	if len(zs) == 0 {
		return zs
	}
	sort.SliceStable(zs, func(a, b int) bool { return zs[a].Rang < zs[b].Rang })
	nachNr := map[int]KartenZeile{}
	for _, z := range zs {
		nachNr[z.Nummer] = z
	}
	blocker := map[int][]int{}
	eltern := map[int]int{}
	kinder := map[int][]int{}
	var wurzeln []int
	for _, z := range zs {
		for _, w := range z.WartetAuf {
			if _, ok := nachNr[w.Nummer]; ok && w.Nummer != z.Nummer {
				blocker[z.Nummer] = append(blocker[z.Nummer], w.Nummer)
			}
		}
		if len(blocker[z.Nummer]) == 0 {
			wurzeln = append(wurzeln, z.Nummer)
			continue
		}
		p := blocker[z.Nummer][0]
		for _, b := range blocker[z.Nummer][1:] {
			if nachNr[b].Rang > nachNr[p].Rang {
				p = b
			}
		}
		eltern[z.Nummer] = p
		kinder[p] = append(kinder[p], z.Nummer) // zs ist nach Rang, also auch kinder
	}

	// Teilbaum je Wurzel.
	teil := map[int]map[int]bool{}
	var sammeln func(root, nr int)
	sammeln = func(root, nr int) {
		if teil[root][nr] {
			return
		}
		teil[root][nr] = true
		for _, k := range kinder[nr] {
			sammeln(root, k)
		}
	}
	for _, w := range wurzeln {
		teil[w] = map[int]bool{}
		sammeln(w, w)
	}
	wurzelVon := map[int]int{}
	for w, t := range teil {
		for n := range t {
			wurzelVon[n] = w
		}
	}

	var folge []int
	platziert := map[int]bool{}
	offen := append([]int{}, wurzeln...)
	for len(offen) > 0 {
		wahl := 0
		for i, w := range offen {
			frei := true
			for n := range teil[w] {
				for _, b := range blocker[n] {
					if r, ok := wurzelVon[b]; ok && r != w && !platziert[r] {
						frei = false
					}
				}
			}
			if frei {
				wahl = i
				break
			}
		}
		w := offen[wahl]
		offen = append(offen[:wahl], offen[wahl+1:]...)
		platziert[w] = true
		folge = append(folge, w)
	}

	out := make([]KartenZeile, 0, len(zs))
	ausgegeben := map[int]bool{}
	var ausgeben func(nr, tiefe, unter int)
	ausgeben = func(nr, tiefe, unter int) {
		if ausgegeben[nr] {
			return
		}
		ausgegeben[nr] = true
		z := nachNr[nr]
		z.Tiefe, z.Unter = tiefe, unter
		out = append(out, z)
		for _, k := range kinder[nr] {
			ausgeben(k, tiefe+1, nr)
		}
	}
	for _, w := range folge {
		ausgeben(w, 0, 0)
	}
	for _, z := range zs {
		if !ausgegeben[z.Nummer] {
			z.Tiefe, z.Unter = 0, 0
			ausgegeben[z.Nummer] = true
			out = append(out, z)
		}
	}
	return out
}
