// Der Kartenueberblick der Reports-Ansicht (gkp-3sq): EIN Graph mit einem
// Knoten je Karte, die noch offene Arbeit haelt, jeder Knoten mit seinem
// Fortschrittsbalken.
//
// Warum ueberhaupt ein zweiter Graph, wo doch jede Karte ihren Plan-Graphen
// hat: der Plan-Graph beantwortet "wie haengt die Arbeit INNERHALB einer Karte
// zusammen". Die Frage beim Oeffnen der Ansicht ist die andere — "wo steht
// dieses Repo, welche Karten leben noch". Die beantwortete bisher niemand: man
// sah die erste Karte im Detail und musste scrollen, um zu erfahren, wieviele
// es sind.
//
// Der DOT-Text entsteht HIER und nicht in browser.js, aus demselben Grund wie
// der Rest dieses Pakets: die Auswahl (welche Karte kommt vor) und die Bilanz
// (welches Segment wird wie breit) sind Rechnung, keine Zeichnung, und sie sind
// hier ohne Browser testbar — siehe reports_overview_test.go.
//
// Er kommt OHNE bd aus. `bd graph --dot` kostet 2765-2871 ms je Aufruf (siehe
// collectors.PlanSVGCached) und ist deshalb aus dem Projekt-Oeffnen-Pfad
// herausgezogen worden; ein Ueberblick, der bd noch einmal befragt, wuerde
// genau das wieder einreissen. Alles, was dieser Graph zeigt, steht bereits in
// den ReportsMaps, die CollectReportsData ohnehin gebaut hat. Bleibt ein
// dot-Prozess, gemessene 26-43 ms, hinter demselben Projekt-Cache.
package appctx

import (
	"fmt"
	"html"
	"strings"

	"github.com/niclasedge/git-planner-go/internal/collectors"
)

// MapsOverview ist der Ueberblick als fertiges SVG plus die zwei Zahlen, die
// die Ansicht dazu sagen muss.
//
// Hidden ist nicht Kosmetik: ein Graph, der Karten weglaesst, ohne zu sagen
// wieviele, behauptet Vollstaendigkeit. Note traegt den Grund, wenn gar kein
// Bild da ist — leer und wortlos waere von "es gibt nichts" nicht zu
// unterscheiden.
type MapsOverview struct {
	SVG    string `json:"svg,omitempty"`
	Shown  int    `json:"shown"`
	Hidden int    `json:"hidden"`
	Note   string `json:"note,omitempty"`
	// Startable/Blocked/Waves sind derselbe Pfad noch einmal, in Zahlen und
	// Wellen statt als Bild (reports_path.go). Beides, weil ein SVG nicht
	// durchsuchbar, nicht anklickbar-je-Zeile und auf einem schmalen Fenster
	// kaum lesbar ist — die Wellen darunter sind der Text zum Bild, nicht seine
	// Wiederholung.
	Startable int       `json:"startable"`
	Blocked   int       `json:"blocked"`
	Waves     []MapWave `json:"waves,omitempty"`
}

const (
	// ovSpalten ist die Gitterbreite. Ohne Kanten legt dot alle Knoten auf EINE
	// Reihe; bei zwoelf Karten waere das ein Bild, das keine Spalte der Welt
	// traegt. Drei Karten a 250pt passen in die Detailspalte, ohne dass
	// .rp-svg svg{max-width:100%} die Schrift unlesbar herunterskaliert.
	ovSpalten = 3
	// ovKartenBreite ist eine MINDESTbreite (WIDTH ohne FIXEDSIZE): darunter
	// stuenden verschieden breite Kaertchen nebeneinander und das Gitter saehe
	// aus wie ein Zufall. Ein langer Titel darf darueber hinaus.
	ovKartenBreite = 250
	ovBalkenBreite = 226
	ovBalkenHoehe  = 7
	// ovTitelMax kuerzt den Titel auf eine Zeile. Der volle Text steht im
	// tooltip des Knotens, geht also nicht verloren.
	ovTitelMax = 34
)

// Die Farben sind die der Plan-Graphen aus `bd graph --dot` (helle Fuellung,
// dunkle Schrift) und nicht die der Seite: beide SVGs stehen im selben
// .rp-svg-Kasten untereinander, und graphviz malt fuer SVG ohnehin einen
// weissen Hintergrund darunter. Ein dunkel eingefaerbter Ueberblick neben
// hellen Plan-Graphen saehe aus wie ein Fehler.
const (
	ovFgText   = "#1a1a1a"
	ovFgMatt   = "#5c5f77"
	ovGruen    = "#40a02b" // geschlossen und belegt
	ovRot      = "#d20f39" // geschlossen ohne Beleg
	ovGrau     = "#ccd0da" // offen
	ovFuellOff = "#e8f4fd"
	ovRandOff  = "#7ea6c9"
	ovFuellArb = "#fdf0d5"
	ovRandArb  = "#df8e1d"
	ovFuellUnb = "#fde8ea"
	ovRandUnb  = "#d20f39"
)

// buildMapsOverview baut den DOT-Text und laesst ihn durch graphviz laufen.
// Faellt an jeder Stelle offen aus: keine Karte, kein dot-Binary, ein
// abgebrochener Prozess — jedes Mal ein leeres SVG mit einer Note, nie ein
// Fehler, der die Reports-Ansicht mitnimmt.
func buildMapsOverview(cwd string, maps []ReportsMap) MapsOverview {
	// Der Abhaengigkeitsgraph kommt aus dem JSONL-Export, nicht aus bd (Grund
	// und Messwerte: reports_path.go). Faellt er aus, bleibt der Ueberblick das,
	// was er vorher war — ein Gitter ohne Kanten — statt gar nichts zu zeigen.
	tg := collectors.TrackerGraphCached(cwd)
	src, shown, hidden, pfad := buildMapsOverviewDot(maps, tg)
	ov := MapsOverview{
		Shown: shown, Hidden: hidden,
		Startable: pfad.Startable, Blocked: pfad.Blocked, Waves: pfad.Waves,
		Note: pfad.Note,
	}
	if shown == 0 {
		ov.Note = "keine Karte mit offener Arbeit"
		return ov
	}
	svg, ok := collectors.RenderDotSVG(collectors.RepoRoot(cwd), src)
	if !ok {
		ov.Note = "graphviz (dot) nicht verfügbar — kein Übersichtsgraph"
		return ov
	}
	ov.SVG = svg
	return ov
}

// ovZeigen entscheidet, welche Karte in den Ueberblick kommt: die mit offener
// Arbeit — und jede, die bd nicht mehr kennt.
//
// Die zweite Haelfte ist kein Versehen. Eine unbekannte Karte hat per
// countOrphanEntries immer Open == 0 und fiele durch die erste Bedingung
// heraus; sie ist aber nicht "geloest", sondern ein Befund (die Report-Datei
// verweist auf kein Epic mehr). Genau der gehoert in ein Bild, das den Stand
// des Repos zeigen soll.
func ovZeigen(m ReportsMap) bool {
	return m.Unknown || m.Counts.Open > 0
}

// buildMapsOverviewDot liefert den DOT-Text, die Zahl der gezeigten und der
// ausgeblendeten Karten sowie den berechneten Pfad. Rein bis auf tg, das der
// Aufrufer besorgt: keine I/O hier, deterministisch in der Reihenfolge, die
// sortMaps festgelegt hat (neueste Aenderung zuerst).
//
// Zwei Bilder in einer Funktion, und das ist Absicht:
//
//   - MIT Kanten wird gezeichnet, was der Tracker weiss: rankdir=LR, links was
//     jetzt anfangen kann, rechts was darauf wartet. Die Schichtung macht dot
//     selbst — sie ist genau das, was buildMapPfad als Wellen ausrechnet.
//   - OHNE Kanten bleibt das Gitter aus drei Spalten. Ein kantenloser Graph
//     legt in dot ALLE Knoten auf eine Reihe; bei zwoelf Karten ist das ein
//     Bild, das keine Spalte traegt. Das Gitter ist kein Pfad und behauptet
//     auch keinen — es ist die ehrliche Form fuer "hier gibt es keine Ordnung".
func buildMapsOverviewDot(maps []ReportsMap, tg collectors.TrackerGraph) (string, int, int, mapPfad) {
	gezeigt := make([]ReportsMap, 0, len(maps))
	versteckt := 0
	for _, m := range maps {
		if ovZeigen(m) {
			gezeigt = append(gezeigt, m)
			continue
		}
		versteckt++
	}
	pfad := buildMapPfad(gezeigt, tg)
	if len(gezeigt) == 0 {
		return "", 0, versteckt, pfad
	}

	idx := make(map[string]int, len(gezeigt))
	for i, m := range gezeigt {
		idx[m.ID] = i
	}

	var b strings.Builder
	b.WriteString("digraph karten {\n")
	if len(pfad.Kanten) > 0 {
		// LR und nicht TB: die Wellen werden dann zu Spalten. Eine Welle mit acht
		// startbaren Karten waere in TB eine Reihe von acht mal 250pt — 2000pt
		// breit, auf die Spalte skaliert unlesbar. Untereinander passt sie.
		b.WriteString("  graph [rankdir=LR, nodesep=0.22, ranksep=0.55];\n")
	} else {
		b.WriteString("  graph [nodesep=0.30, ranksep=0.34];\n")
	}
	b.WriteString("  node [shape=plaintext, fontname=\"Helvetica\", fontsize=11, margin=0];\n")
	for i, m := range gezeigt {
		// Knotennamen sind n0..nN und nicht die bead-Id: eine Id landet damit
		// nur in href, tooltip und Label, wo sie maskiert wird, und nie in der
		// DOT-Grammatik selbst.
		label := ovLabel(m)
		if m.ID == pfad.GateID {
			label = ovGateLabel(m, pfad.GateOffen)
		}
		fmt.Fprintf(&b, "  n%d [href=%q, tooltip=%q, label=<%s>];\n",
			i, "#epic:"+m.ID, ovTooltip(m), label)
	}

	if len(pfad.Kanten) > 0 {
		// Die Kante vom Gate ist gestrichelt und rot: sie sagt etwas anderes als
		// die anderen. "Wartet auf Arbeit" und "wartet auf eine Antwort" gleich
		// zu zeichnen waere die Verwechslung, wegen der die Sammelkarte
		// ueberhaupt angelegt wurde.
		for _, k := range pfad.Kanten {
			von, ok1 := idx[k.Von]
			nach, ok2 := idx[k.Nach]
			if !ok1 || !ok2 {
				continue
			}
			if k.Von == pfad.GateID {
				fmt.Fprintf(&b, "  n%d -> n%d [color=\"%s\", style=dashed, penwidth=1.3, arrowsize=0.7];\n",
					von, nach, ovRot)
			} else {
				fmt.Fprintf(&b, "  n%d -> n%d [color=\"#8a8f98\", penwidth=1.1, arrowsize=0.7];\n", von, nach)
			}
		}
		b.WriteString("}\n")
		return b.String(), len(gezeigt), versteckt, pfad
	}

	// Das Gitter: je Zeile ein rank=same, die Reihenfolge INNERHALB der Zeile
	// ueber flache Kanten (ohne sie ordnet dot die Knoten einer Ebene beliebig),
	// die Zeilen untereinander ueber je eine Kante vom Anfang der vorigen Zeile.
	// Unsichtbar, weil sie ordnen und nichts behaupten.
	b.WriteString("  edge [style=invis];\n")
	for start := 0; start < len(gezeigt); start += ovSpalten {
		ende := start + ovSpalten
		if ende > len(gezeigt) {
			ende = len(gezeigt)
		}
		b.WriteString("  { rank=same;")
		for i := start; i < ende; i++ {
			fmt.Fprintf(&b, " n%d;", i)
		}
		b.WriteString(" }\n")
		if ende-start > 1 {
			b.WriteString("  ")
			for i := start; i < ende; i++ {
				if i > start {
					b.WriteString(" -> ")
				}
				fmt.Fprintf(&b, "n%d", i)
			}
			b.WriteString(";\n")
		}
		if start > 0 {
			fmt.Fprintf(&b, "  n%d -> n%d;\n", start-ovSpalten, start)
		}
	}
	b.WriteString("}\n")
	return b.String(), len(gezeigt), versteckt, pfad
}

// ovGateLabel zeichnet die Sammelkarte der Entscheidungen als das, was sie ist:
// kein Arbeitspaket mit Fortschritt, sondern eine Wartestelle. Deshalb kein
// Balken — ein Fortschrittsbalken an einer Frage an einen Menschen wuerde
// behaupten, sie sei zu 40 Prozent beantwortet.
func ovGateLabel(m ReportsMap, offen int) string {
	var b strings.Builder
	fmt.Fprintf(&b, `<TABLE BORDER="2" COLOR="%s" BGCOLOR="%s" CELLBORDER="0" CELLSPACING="0" CELLPADDING="3" WIDTH="%d">`,
		ovRandUnb, ovFuellUnb, ovKartenBreite)
	fmt.Fprintf(&b, `<TR><TD ALIGN="LEFT"><FONT COLOR="%s" POINT-SIZE="11"><B>%s</B></FONT></TD></TR>`,
		ovFgText, ovEsc(ovKuerzen(ovTitel(m), ovTitelMax)))
	fmt.Fprintf(&b, `<TR><TD ALIGN="LEFT"><FONT COLOR="%s" POINT-SIZE="9">%s</FONT></TD></TR>`,
		ovFgMatt, ovEsc(m.ID+" · wartet auf einen Menschen"))
	fmt.Fprintf(&b, `<TR><TD ALIGN="LEFT"><FONT COLOR="%s" POINT-SIZE="9"><B>%s</B></FONT></TD></TR>`,
		ovRandUnb, ovEsc(ovAnzEntscheidungen(offen)))
	b.WriteString(`</TABLE>`)
	return b.String()
}

func ovAnzEntscheidungen(n int) string {
	if n == 1 {
		return "1 offene Entscheidung"
	}
	return fmt.Sprintf("%d offene Entscheidungen", n)
}

// ovLabel ist die Karte als HTML-Label: Titel, Kennzeile, Balken, Zahlen.
func ovLabel(m ReportsMap) string {
	fuell, rand := ovFarben(m)
	var b strings.Builder
	fmt.Fprintf(&b, `<TABLE BORDER="1" COLOR="%s" BGCOLOR="%s" CELLBORDER="0" CELLSPACING="0" CELLPADDING="3" WIDTH="%d">`,
		rand, fuell, ovKartenBreite)
	fmt.Fprintf(&b, `<TR><TD ALIGN="LEFT"><FONT COLOR="%s" POINT-SIZE="11"><B>%s</B></FONT></TD></TR>`,
		ovFgText, ovEsc(ovKuerzen(ovTitel(m), ovTitelMax)))
	fmt.Fprintf(&b, `<TR><TD ALIGN="LEFT"><FONT COLOR="%s" POINT-SIZE="9">%s</FONT></TD></TR>`,
		ovFgMatt, ovEsc(ovKennzeile(m)))
	if balken := ovBalken(m.Counts); balken != "" {
		fmt.Fprintf(&b, `<TR><TD>%s</TD></TR>`, balken)
	}
	fmt.Fprintf(&b, `<TR><TD ALIGN="LEFT"><FONT COLOR="%s" POINT-SIZE="9">%s</FONT></TD></TR>`,
		ovFgMatt, ovEsc(ovZahlen(m)))
	b.WriteString(`</TABLE>`)
	return b.String()
}

// ovBalken ist der Fortschritt als drei Segmente: geschlossen-mit-Beleg,
// geschlossen-ohne-Beleg, offen.
//
// Drei und nicht eins, weil die Ansicht sonst zwei verschiedene Dinge in
// derselben Flaeche behaupten muesste. Die Liste zeigt den Fortschritt als
// EINE Quote (rpFortschritt: geschlossen/gesamt) und die Belegluecke daneben
// als Text — hier ist Platz, sie im Balken selbst zu trennen, und genau diese
// Trennung ist der Grund, warum es diese Ansicht gibt: geschlossen ohne Beleg
// ist kein Fortschritt, den man einfach mitzaehlt.
//
// Ohne Sub-beads gar kein Balken. Ein leerer laese sich als "0 % erledigt"
// lesen, und das waere eine Behauptung ueber eine Karte ohne Inhalt — dieselbe
// Regel, die rpFortschritt im Browser schon anwendet.
func ovBalken(c MapCounts) string {
	if c.Total <= 0 {
		return ""
	}
	belegtZu := c.Closed - c.Gaps
	if belegtZu < 0 {
		belegtZu = 0
	}
	teile := []struct {
		n     int
		farbe string
	}{
		{belegtZu, ovGruen},
		{c.Gaps, ovRot},
		{c.Total - belegtZu - c.Gaps, ovGrau},
	}

	var b strings.Builder
	fmt.Fprintf(&b, `<TABLE BORDER="0" CELLBORDER="0" CELLSPACING="0" CELLPADDING="0" FIXEDSIZE="TRUE" WIDTH="%d" HEIGHT="%d"><TR>`,
		ovBalkenBreite, ovBalkenHoehe)
	// Die Restbreite geht an das LETZTE nicht-leere Segment, statt sie
	// wegzurunden: sonst ist der Balken bei krummen Verhaeltnissen ein bis zwei
	// Punkte kuerzer als der Kasten, den FIXEDSIZE aufspannt, und dot bricht das
	// Layout nicht ab, sondern zeichnet still eine Luecke.
	rest := ovBalkenBreite
	letzte := -1
	for i, t := range teile {
		if t.n > 0 {
			letzte = i
		}
	}
	for i, t := range teile {
		if t.n <= 0 {
			continue
		}
		w := t.n * ovBalkenBreite / c.Total
		if i == letzte {
			w = rest
		}
		if w <= 0 {
			w = 1
		}
		rest -= w
		fmt.Fprintf(&b, `<TD FIXEDSIZE="TRUE" WIDTH="%d" HEIGHT="%d" BGCOLOR="%s"></TD>`, w, ovBalkenHoehe, t.farbe)
	}
	b.WriteString(`</TR></TABLE>`)
	return b.String()
}

// ovTitel: die Sammelkarte und eine verwaiste Report-Datei haben keinen Titel
// aus bd, also bekommen sie hier einen, der sagt was sie sind.
func ovTitel(m ReportsMap) string {
	if m.Unknown {
		return "Unbekannte Map"
	}
	if m.Title != "" {
		return m.Title
	}
	return m.ID
}

// ovKennzeile ist die kleine Zeile unter dem Titel: Id, Prioritaet, Status.
func ovKennzeile(m ReportsMap) string {
	teile := []string{m.ID}
	// Die Sammelkarte ist kein Ticket und hat keine Prioritaet — die 0 als "P0"
	// zu zeichnen hiesse "kritisch" ueber ein Feld zu behaupten, das niemand
	// gesetzt hat (dieselbe Ueberlegung wie rpTlMapKopf im Browser).
	if !m.Loose && !m.Unknown {
		teile = append(teile, fmt.Sprintf("P%d", m.Priority))
	}
	teile = append(teile, ovStatusText(m))
	// Die Farbe der Karte folgt InProgress, der Text bds eigenem Status — und
	// die beiden gehen regelmaessig auseinander: beads bewegt den Status des
	// Epics nicht, wenn ein Kind startet (siehe mapInProgress). Ohne diesen
	// Zusatz stuende eine bernsteinfarbene Karte da, die sich selbst "offen"
	// nennt, und der Leser haette keine Erklaerung fuer die Farbe.
	if m.InProgress && m.Status != istInArbeit {
		teile = append(teile, "Arbeit läuft")
	}
	return strings.Join(teile, " · ")
}

// ovStatusText uebersetzt bds Status. Unbekannte Zustaende werden
// durchgereicht statt auf "offen" abgebildet: bd kann Zustaende dazubekommen,
// und ein stiller Fallback wuerde einen neuen als bekannt ausgeben.
func ovStatusText(m ReportsMap) string {
	if m.Unknown {
		return "kein bd-Epic"
	}
	switch m.Status {
	case "open":
		return "offen"
	case istInArbeit:
		return "in Arbeit"
	case statusGeschlossen:
		return "geschlossen"
	case "blocked":
		return "blockiert"
	case "deferred":
		return "zurückgestellt"
	case "":
		return "ohne Status"
	}
	return m.Status
}

// ovZahlen ist die Bilanzzeile. Fuer eine unbekannte Karte nennt sie nur
// Umfang und Belege — bd-Zustaende kennt der Report-Store nicht, und "0 offen"
// waere dort eine Auskunft, die niemand hat (siehe countOrphanEntries).
func ovZahlen(m ReportsMap) string {
	c := m.Counts
	if m.Unknown {
		return fmt.Sprintf("%d Bead(s) · %d belegt", c.Total, c.Proven)
	}
	if c.Total == 0 {
		return "keine Sub-beads"
	}
	s := fmt.Sprintf("%d/%d zu · %d offen", c.Closed, c.Total, c.Open)
	if c.Gaps > 0 {
		s += fmt.Sprintf(" · %d ohne Beleg", c.Gaps)
	}
	if m.ReadyCount > 0 {
		s += fmt.Sprintf(" · %d startklar", m.ReadyCount)
	}
	return s
}

// ovTooltip traegt den ungekuerzten Titel: der Knoten zeigt nur ovTitelMax
// Zeichen, und ein gekuerzter Titel ohne Weg zum vollen waere ein Datenverlust
// im Bild.
func ovTooltip(m ReportsMap) string {
	return m.ID + " — " + ovTitel(m)
}

func ovFarben(m ReportsMap) (fuell, rand string) {
	if m.Unknown {
		return ovFuellUnb, ovRandUnb
	}
	if m.InProgress {
		return ovFuellArb, ovRandArb
	}
	return ovFuellOff, ovRandOff
}

// ovEsc maskiert fuer ein HTML-Label. html.EscapeString erzeugt fuer ' die
// numerische Entitaet &#39;, die graphviz kennt — das ist der Grund, hier
// nicht von Hand zu maskieren.
func ovEsc(s string) string { return html.EscapeString(s) }

// ovKuerzen schneidet nach Runen, nicht nach Bytes: ein Titel mit Umlaut wuerde
// sonst mitten in einem Zeichen enden und graphviz bekaeme kaputtes UTF-8.
func ovKuerzen(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	if max <= 1 {
		return "…"
	}
	return strings.TrimRight(string(r[:max-1]), " ") + "…"
}
