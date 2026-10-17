// Der Pfad durch die Karten: welche Karte kann jetzt anfangen, welche wartet
// worauf, und wo wartet die Arbeit nicht auf Arbeit, sondern auf einen
// Menschen.
//
// Bis hierher zeichnete der Ueberblick Kaestchen ohne Kanten, mit der
// Begruendung, zwischen Karten gebe es keine Abhaengigkeiten. Das war falsch,
// gemessen am 2026-09-09 in acme-platform: 14 blocks-Kanten zwischen Epics,
// 11 von 19 offenen Karten haben einen Blocker, 8 koennen sofort anfangen. Die
// Ordnung war da, das Bild zeigte sie nur nicht.
//
// Zwei Sorten Kante laufen hier zusammen, und die zweite ist der Grund, warum
// es diese Datei gibt:
//
//  1. Karte blockt Karte, direkt am Epic gehaengt.
//  2. Bead blockt Bead ueber Kartengrenzen hinweg. Die haengt niemand ans Epic,
//     denn sie sitzt richtigerweise dort, wo die Arbeit stehenbleibt — und
//     genau so entsteht der Fall, um den es dem Nutzer geht: ein
//     Entscheidungs-Bead unter der Mensch-Gate-Karte blockt einen einzelnen
//     Bead in einer anderen Karte. Auf Kartenebene heisst das: diese Karte
//     wartet auf eine Entscheidung. Ohne Ableitung waere davon nichts zu sehen.
//
// Die Daten kommen aus collectors.TrackerGraphCached, also aus dem
// JSONL-Export und nicht aus `bd dep list` — der braucht 8 s fuer 59 Ids und
// 46 s fuer 494 (gemessen 2026-09-09), das Lesen derselben Kanten aus der Datei
// 14 ms.
package appctx

import (
	"sort"

	"github.com/niclasedge/git-planner-go/internal/collectors"
)

// MapWave ist eine Welle des Pfades: alles darin kann gleichzeitig laufen.
type MapWave struct {
	N    int       `json:"n"`
	Maps []WaveMap `json:"maps"`
}

// WaveMap ist eine Karte in ihrer Welle. Gate markiert den einen Knoten, der
// keine Arbeit ist, sondern eine offene Frage an einen Menschen.
type WaveMap struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Open     int      `json:"open"`
	Gate     bool     `json:"gate"`
	WaitsFor []string `json:"waits_for,omitempty"`
}

// pfadKante ist eine Kante auf KARTENebene: Von muss fertig (bzw. beantwortet)
// sein, bevor Nach laufen kann. Bewusst nicht from/to benannt, sondern in der
// Sprache der Sache — die Richtung ist die Verwechslung, die in diesem Repo
// schon einmal alle zehn Kanten gedreht hat.
type pfadKante struct {
	Von  string
	Nach string
}

// mapPfad ist das Ergebnis: Kanten, Wellen, das Gate und die zwei Zahlen, die
// die Ansicht darueber sagt.
type mapPfad struct {
	Kanten    []pfadKante
	GateID    string
	GateOffen int
	Layer     map[string]int
	Waves     []MapWave
	Startable int
	Blocked   int
	Note      string
}

// ovDecision ist bds Typ fuer eine Entscheidung. Ein Entscheidungs-Bead ist
// keine Aufgabe: er wird nicht abgearbeitet, sondern beantwortet, und nur ein
// Mensch kann ihn schliessen. Deshalb wird er im Bild nicht als Karte
// gezeichnet, sondern als das, worauf gewartet wird.
const ovDecision = "decision"

// buildMapPfad legt die Kanten auf die Karten um und schichtet sie.
//
// gezeigt sind die Karten, die der Ueberblick ohnehin malt (ovZeigen). Alles,
// was auf eine nicht gezeigte Karte zeigt, faellt weg: eine geschlossene Karte
// blockt nichts mehr, und eine Kante ins Unsichtbare waere ein Pfeil aus dem
// Nichts.
func buildMapPfad(gezeigt []ReportsMap, tg collectors.TrackerGraph) mapPfad {
	p := mapPfad{Layer: map[string]int{}}
	sichtbar := make(map[string]bool, len(gezeigt))
	reihenfolge := make(map[string]int, len(gezeigt))
	titel := make(map[string]string, len(gezeigt))
	offen := make(map[string]int, len(gezeigt))
	for i, m := range gezeigt {
		sichtbar[m.ID] = true
		reihenfolge[m.ID] = i
		titel[m.ID] = ovTitel(m)
		offen[m.ID] = m.Counts.Open
	}
	if !tg.Available {
		p.Note = tg.Note
		p.Waves = ovEineWelle(gezeigt)
		p.Startable = len(gezeigt)
		return p
	}

	p.GateID, p.GateOffen = ovGateFinden(tg, sichtbar)
	p.Kanten = ovKanten(tg, sichtbar, p.GateID)
	p.Layer, p.Waves, p.Startable, p.Blocked, p.Note = ovWellen(
		gezeigt, p.Kanten, p.GateID, p.GateOffen, titel, offen, reihenfolge,
	)
	if tg.Stale && p.Note == "" {
		p.Note = "Kanten aus einem Export, der aelter ist als die juengste Aenderung — Pfad kann veraltet sein"
	}
	return p
}

// ovGateFinden sucht die Sammelkarte der offenen Entscheidungen: die Karte mit
// den meisten offenen decision-Beads unter sich.
//
// Ueber den TYP und nicht ueber Titel oder Label, weil der Typ das ist, was bd
// selbst weiss — ein Label kann fehlen, ein Titel kann sich aendern, und beides
// waere eine Konvention, die genau dann bricht, wenn jemand die Karte in einem
// anderen Repo anders nennt. Gibt es keine offene Entscheidung, gibt es kein
// Gate; dann faellt der Knoten weg, statt leer dazustehen.
func ovGateFinden(tg collectors.TrackerGraph, sichtbar map[string]bool) (string, int) {
	zaehler := map[string]int{}
	for id, typ := range tg.Type {
		if typ != ovDecision || tg.Status[id] == statusGeschlossen {
			continue
		}
		eltern := tg.Parent[id]
		if eltern == "" {
			continue
		}
		zaehler[eltern]++
	}
	besteID, beste := "", 0
	for id, n := range zaehler {
		// Bei Gleichstand entscheidet die Id, nicht die Laune der Map-Iteration:
		// ein Bild, das zwischen zwei Aufrufen ein anderes Gate zeigt, waere
		// nicht wiederholbar.
		if n > beste || (n == beste && id < besteID) {
			besteID, beste = id, n
		}
	}
	if besteID == "" || !sichtbar[besteID] {
		// Ein Gate, das der Ueberblick nicht zeigt (geschlossene Sammelkarte,
		// Entscheidungen ohne Elternkarte), wird nicht gezeichnet. Die Kanten
		// laufen dann ueber ovKarteVon auf die Karte, unter der die Entscheidung
		// haengt — das ist immer noch wahr, nur weniger deutlich.
		return "", 0
	}
	return besteID, beste
}

// ovKarteVon uebersetzt eine Bead-Id in die Karte, in der sie steckt.
//
// Zwei Ebenen hoch, weil bd Unter-Unter-Beads kennt (lc25.1 haengt unter lc25):
// eine einzige Ebene wuerde genau die Entscheidungs-Beads verlieren, um die es
// hier geht.
func ovKarteVon(id string, sichtbar map[string]bool, tg collectors.TrackerGraph) string {
	if sichtbar[id] {
		return id
	}
	eltern := tg.Parent[id]
	if sichtbar[eltern] {
		return eltern
	}
	if eltern != "" && sichtbar[tg.Parent[eltern]] {
		return tg.Parent[eltern]
	}
	return ""
}

// ovKanten legt die Bead-Kanten auf Kartenebene um, entdoppelt sie und wirft
// weg, was nichts mehr aussagt.
func ovKanten(tg collectors.TrackerGraph, sichtbar map[string]bool, gateID string) []pfadKante {
	gesehen := map[pfadKante]bool{}
	out := []pfadKante{}
	for _, e := range tg.Edges {
		// Ein erledigter Blocker blockt nicht mehr, und eine erledigte Arbeit
		// wartet auf nichts. Beide Kanten stehen weiter im Tracker — sie sind
		// Historie, kein Pfad.
		if tg.Status[e.Blocker] == statusGeschlossen || tg.Status[e.Blocked] == statusGeschlossen {
			continue
		}
		nach := ovKarteVon(e.Blocked, sichtbar, tg)
		if nach == "" {
			continue
		}
		von := ovKarteVon(e.Blocker, sichtbar, tg)
		if tg.Type[e.Blocker] == ovDecision && gateID != "" {
			// Jede offene Entscheidung zeigt auf denselben Knoten, egal unter
			// welcher Karte sie haengt. Genau das ist der Sinn der Sammelkarte:
			// eine Stelle, an der steht, was ein Mensch beantworten muss.
			von = gateID
		}
		if von == "" || von == nach {
			continue
		}
		k := pfadKante{Von: von, Nach: nach}
		if gesehen[k] {
			continue
		}
		gesehen[k] = true
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Von != out[j].Von {
			return out[i].Von < out[j].Von
		}
		return out[i].Nach < out[j].Nach
	})
	return out
}

// ovWellen schichtet die Karten: Welle 1 ist alles ohne Blocker, jede weitere
// haengt an der davor. Laengster Pfad statt kuerzester — eine Karte, die auf
// zwei Dinge wartet, gehoert hinter das SPAETERE von beiden, sonst verspricht
// die Welle einen Start, den es nicht gibt.
func ovWellen(
	gezeigt []ReportsMap, kanten []pfadKante, gateID string, gateOffen int,
	titel map[string]string, offen map[string]int, reihenfolge map[string]int,
) (map[string]int, []MapWave, int, int, string) {
	knoten := make([]string, 0, len(gezeigt))
	for _, m := range gezeigt {
		knoten = append(knoten, m.ID)
	}
	nachfolger := map[string][]string{}
	eingang := map[string]int{}
	wartetAuf := map[string][]string{}
	for _, k := range kanten {
		nachfolger[k.Von] = append(nachfolger[k.Von], k.Nach)
		eingang[k.Nach]++
		wartetAuf[k.Nach] = append(wartetAuf[k.Nach], k.Von)
	}

	layer := map[string]int{}
	offenGrad := map[string]int{}
	queue := []string{}
	for _, id := range knoten {
		offenGrad[id] = eingang[id]
		if eingang[id] == 0 {
			layer[id] = 0
			queue = append(queue, id)
		}
	}
	sort.Slice(queue, func(i, j int) bool { return reihenfolge[queue[i]] < reihenfolge[queue[j]] })
	for i := 0; i < len(queue); i++ {
		v := queue[i]
		for _, n := range nachfolger[v] {
			if layer[v]+1 > layer[n] {
				layer[n] = layer[v] + 1
			}
			offenGrad[n]--
			if offenGrad[n] == 0 {
				queue = append(queue, n)
			}
		}
	}

	note := ""
	maxL := 0
	for _, l := range layer {
		if l > maxL {
			maxL = l
		}
	}
	// Was uebrig bleibt, steckt in einem Zyklus. Das ist ein Befund und keine
	// Anzeigefrage: bd laesst solche Kanten zu (`bd dep cycles` sucht sie
	// eigens), und ein stilles Weglassen wuerde Karten aus dem Pfad tilgen.
	for _, id := range knoten {
		if _, ok := layer[id]; !ok {
			layer[id] = maxL + 1
			note = "Zyklus in den Abhängigkeiten — betroffene Karten stehen in der letzten Welle (bd dep cycles)"
		}
	}
	// Das Gate steht immer ganz vorn: es blockt, es wird nicht geblockt, und es
	// ist der Anfang jedes Weges, der ueber eine Entscheidung fuehrt.
	if gateID != "" {
		layer[gateID] = 0
	}

	proWelle := map[int][]string{}
	maxL = 0
	for id, l := range layer {
		proWelle[l] = append(proWelle[l], id)
		if l > maxL {
			maxL = l
		}
	}
	waves := make([]MapWave, 0, maxL+1)
	for l := 0; l <= maxL; l++ {
		ids := proWelle[l]
		if len(ids) == 0 {
			continue
		}
		sort.Slice(ids, func(i, j int) bool { return reihenfolge[ids[i]] < reihenfolge[ids[j]] })
		w := MapWave{N: l + 1}
		for _, id := range ids {
			warte := append([]string(nil), wartetAuf[id]...)
			sort.Strings(warte)
			wm := WaveMap{ID: id, Title: titel[id], Open: offen[id], WaitsFor: warte}
			if id == gateID {
				wm.Gate = true
				wm.Open = gateOffen
			}
			w.Maps = append(w.Maps, wm)
		}
		waves = append(waves, w)
	}

	startbar, blockiert := 0, 0
	for _, id := range knoten {
		if id == gateID {
			continue
		}
		if eingang[id] == 0 {
			startbar++
		} else {
			blockiert++
		}
	}
	return layer, waves, startbar, blockiert, note
}

// ovEineWelle ist der Stand ohne Kantenwissen: alles in Welle 1. Ehrlicher als
// gar keine Welle — die Ansicht sagt dann "alle startbar" und die Note daneben,
// warum sie es nicht besser weiss.
func ovEineWelle(gezeigt []ReportsMap) []MapWave {
	if len(gezeigt) == 0 {
		return nil
	}
	w := MapWave{N: 1}
	for _, m := range gezeigt {
		w.Maps = append(w.Maps, WaveMap{ID: m.ID, Title: ovTitel(m), Open: m.Counts.Open})
	}
	return []MapWave{w}
}
