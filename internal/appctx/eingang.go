// Die Ansicht "Eingang" (gkp-1oj, Spec §5, Things' Inbox).
//
// Eine Datei, eine Regel, ein init — der Rahmen steht in ansicht.go.
package appctx

import "github.com/niclasedge/git-planner-go/internal/collectors"

// EingangID ist der Name der Ansicht fuer Route, Navigation und Browser.
const EingangID = "eingang"

// GrundOhneKarte ist der Grund, aus dem eine Zeile hier steht: die Aufgabe
// haengt unter keinem Epic. "Karte" und nicht "Epic", weil die Ansicht ueberall
// sonst von Karten spricht.
const GrundOhneKarte = "ohne Karte"

func init() {
	regelAnmelden(Regel{
		ID:    EingangID,
		Titel: "Eingang",
		// Der Eingang ist keine Arbeitsliste, sondern die Sortierstelle davor —
		// deshalb hinter die vier, nach denen man arbeitet, und vor die
		// Rueckschau.
		Ordnung: 50,
		Leer: "Kein Posteingang — jede offene Aufgabe hängt unter einer Karte, " +
			"es liegt nichts zum Einsortieren bereit.",
		Hinweis: "Aufgaben, die unter keinem Epic hängen — die synthetische Karte »" +
			collectors.LooseMapID + "« (bd list --no-parent --exclude-type epic). " +
			"Leer heißt: alles ist einer Karte zugeordnet.",
		Waehlt: waehltEingang,
	})
}

// waehltEingang ist Spec §5, Zeile Inbox — und die Zeile sagt bereits
// "**existiert**": die Auswahl ist die BESTEHENDE synthetische Karte
// collectors.LooseMapID (beads.go:293), keine zweite Quelle daneben.
//
// Deshalb fragt die Regel nach der KARTE und nicht nach einer Eigenschaft der
// Aufgabe. Der Unterschied ist nicht kosmetisch: "hat keinen Parent" waere die
// Eigenschaft, und sie ist an dieser Stelle FALSCH — die Unteraufgaben einer
// losen Aufgabe haben sehr wohl einen Parent und gehoeren trotzdem in den
// Eingang, weil die ganze Traube unter keiner Karte haengt. Der Rahmen steigt
// ohnehin in die Kinder ab (sammle), also faellt das von selbst richtig aus,
// sobald die Frage an der Karte haengt.
//
// Sortierung: der Eingang ist unsortierte Post, mit einer Ausnahme. Was schon
// einen Termin traegt, ist bereits entschieden und steht oben, chronologisch —
// alles andere faellt auf den Schluessel des Rahmens zurueck (Prioritaet, dann
// letzte Aenderung neu→alt, dann Id). Eine manuelle Reihenfolge kommt spaeter
// aus order.json (gkp-eb0, Spec §9 O1) und liegt dann als Ueberlagerung
// darueber; bis dahin ist das hier die deterministische Grundordnung, auf die
// sie sich stuetzt.
func waehltEingang(k Kandidat) (Urteil, bool) {
	if !k.Offen() || k.Karte != collectors.LooseMapID {
		return Urteil{}, false
	}
	u := Urteil{Rang: 0, Gruende: []string{GrundOhneKarte}, Zeitpunkt: k.Frist}
	if u.Zeitpunkt == nil {
		u.Zeitpunkt = k.Wann
	}
	// Startklar und in Arbeit stehen zusaetzlich dran, weil ein Eingang, in dem
	// bereits gearbeitet wird, etwas anderes ist als einer, der nur voll ist.
	if k.Bead.Status == istInArbeit {
		u.Gruende = append(u.Gruende, GrundInArbeit)
	} else if k.Bead.Ready {
		u.Gruende = append(u.Gruende, GrundStartklar)
	}
	return u, true
}
