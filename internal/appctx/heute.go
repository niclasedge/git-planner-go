// Die Ansicht "Heute" (gkp-7gb, Spec §5).
//
// Eine Datei, eine Regel, ein init — der Rahmen steht in ansicht.go. Wer eine
// weitere Ansicht baut (gkp-1oj), legt eine Datei wie diese daneben und fasst
// keine bestehende Zeile an.
package appctx

import (
	"strings"
	"time"
)

// Die Gruende, aus denen eine Aufgabe heute auf dem Tisch liegt. Sie sind
// gleichzeitig die Schluessel der Zaehlung im Kopf der Ansicht — deshalb der
// Text, den der Leser sieht, und keine Kuerzel mit einer Uebersetzungstabelle
// im Browser daneben.
const (
	GrundUeberfaellig = "überfällig"
	GrundFaelligHeute = "fällig heute"
	GrundStartklar    = "startklar"
	GrundInArbeit     = "in Arbeit"
	GrundGeplant      = "geplant"
)

// Die drei Eimer. Sie sind die Reihenfolge der Liste: was zu spaet ist, zuerst.
const (
	rangUeberfaellig = 0
	rangHeute        = 1
	rangSonst        = 2
)

// gruendeHeute sind die VIER Aufnahmegruende, die waehltHeute tatsaechlich
// vergibt — GrundStartklar zaehlt seit gkp-89g nicht mehr dazu und fehlt hier
// deshalb absichtlich. Diese Liste ist die einzige Stelle, aus der der
// Leer-Text gebaut wird (heuteLeerText); TestHeuteLeerTextNenntJedenGrund
// prueft unabhaengig davon, dass die vier Gruende, die waehltHeute wirklich
// zurueckgibt, im Text stehen.
//
// gkp-9di: der vorherige Text nannte drei von vier Gruenden von Hand
// (GrundGeplant fehlte) — ein Bead, der einzig ueber defer_until <= heute
// nach Heute kommt, wurde von der Leer-Erklaerung verschwiegen. Eine generisch
// gebaute Aufzaehlung kann diesen Fehler nicht mehr wiederholen: ein neuer
// Grund fehlt im Text nur noch, wenn er auch hier in der Liste fehlt, und
// genau das bewacht der Test.
var gruendeHeute = []string{GrundUeberfaellig, GrundFaelligHeute, GrundInArbeit, GrundGeplant}

// heuteLeerText baut den Satz, den die leere Liste zeigt, aus gruendeHeute.
//
// Regel.Leer ist ein fester string und kein func() string: ansichtAus kopiert
// ihn unveraendert in jede gebaute Ansicht (`Leer: r.Leer`), und alle sechs
// Regeln teilen sich denselben Feldtyp — eine Ausnahme fuer Heute allein waere
// ein zweiter Feldtyp fuer dieselbe Sache, nur um eine Zeile Bauzeit-Logik zu
// sparen. Der Satz wird deshalb EINMAL in init() aus gruendeHeute zusammengesetzt
// und danach als fertiger String abgelegt, wie in jeder anderen Regel auch.
// Die Kopplung an die vier Gruende haengt damit an dieser einen Funktion und
// nicht mehr an vier Woertern, die von Hand im Text nachgefuehrt werden
// muessten.
//
// Anredefrei formuliert (gkp-9di): "setzen Sie" war der einzige Satz im Paket,
// der siezt — alle anderen Leer-Texte und die Meldungen in browser.js sind
// unpersoenlich oder im Infinitiv.
func heuteLeerText() string {
	teile := make([]string, len(gruendeHeute))
	for i, g := range gruendeHeute {
		teile[i] = "nichts " + g
	}
	// Die beiden kurzen Faelle stehen hier nicht der Vollstaendigkeit halber:
	// diese Funktion laeuft in init(), also beim Dienststart. Ein
	// teile[len(teile)-1] auf eine leere Liste waere kein falscher Satz,
	// sondern ein Panic, bevor der erste Listener steht — ein Ausfall des
	// ganzen Dienstes, ausgeloest von einem Textbaustein. Bei genau einem Grund
	// klebte ein fuehrendes " und " am Satz.
	var aufzaehlung string
	switch len(teile) {
	case 0:
		aufzaehlung = "nichts"
	case 1:
		aufzaehlung = teile[0]
	default:
		aufzaehlung = strings.Join(teile[:len(teile)-1], ", ") + " und " + teile[len(teile)-1]
	}
	return "Für heute ist nichts verabredet — " + aufzaehlung + ". Die Liste ist leer, nicht kaputt: " +
		"an einer Aufgabe in Jederzeit einen Termin setzen, dann taucht sie hier auf."
}

func init() {
	regelAnmelden(Regel{
		ID:    "heute",
		Titel: "Heute",
		// Erste Stelle in der Navigation, und das ist die eine Ordnung, die
		// nicht bloss Geschmack ist: Things oeffnet auf Today, und eine
		// Aufgabenliste, die mit "Demnächst" beginnt, beantwortet die Frage
		// nicht, mit der man sie aufschlaegt. Vor gkp-3oh stand Heute an
		// dritter Stelle — nicht entschieden, sondern weil "heute.go"
		// alphabetisch hinter "demnaechst.go" und "eingang.go" liegt.
		Ordnung: 10,
		Leer:    heuteLeerText(),
		// Der Hinweis ist die Auflage aus Spec §4.4: GitHub-Aufgaben haben kein
		// Gegenstueck zu When/Deadline, koennen also in dieser Liste gar nicht
		// vorkommen — und das MUSS sichtbar sein, nicht stillschweigend. Eine
		// leere GitHub-Sektion waere die schlechtere Antwort: sie sieht nach
		// "heute nichts zu tun" aus statt nach "kann hier nicht stehen".
		Hinweis: "Nur beads: GitHub-Aufgaben tragen kein When/Deadline (Spec §4.4, E1) " +
			"und können deshalb nicht in Heute stehen — sie erscheinen in Jederzeit.",
		Waehlt: waehltHeute,
	})
}

// waehltHeute ist Spec §5, Formel nach der zweiten Korrektur (gkp-89g,
// 2026-09-16): due_at <= heute VEREINIGT mit defer_until <= heute VEREINIGT
// mit in_progress.
//
// Verglichen werden KALENDERTAGE, nicht Zeitpunkte. Eine Frist auf heute 09:00
// ist um 10:00 nicht "vorbei" — sie ist heute faellig. Und die Gruppierung nach
// Kalendertag ist genau das, wofuer die Kompensation in Spec §4.4 existiert:
// ein Termin um 23:00 darf nicht durch den UTC-Versatz auf morgen kippen.
//
// ABWEICHUNG von der urspruenglichen Formel der Spec, bewusst und
// nachgemessen: in_progress kam am 2026-09-14 als vierte Quelle dazu. `bd
// ready` liefert einen Bead auf in_progress NICHT (gemessen gegen bd 1.2.2 an
// einer Wegwerf-Datenbank: von drei Beads nannte `bd ready --json` nur den
// offenen). Ohne die vierte Quelle zeigte eine Liste namens "Heute"
// ausgerechnet die Aufgabe nicht, an der gerade gearbeitet wird — gkp-7gb
// selbst waere beim Bauen nicht darin vorgekommen. Die Quelle ist benannt
// (GrundInArbeit) und damit wieder entfernbar, falls die Spec anders
// entschieden wird.
//
// ZWEITE KORREKTUR, 2026-09-16 (gkp-89g): "startklar" (`bd ready`) faellt
// wieder heraus. Gemessen: 0 von 1232 Beads ueber 8 Repos tragen due_at oder
// defer_until. Heute bestand damit zu 100 % aus "startklar" und war
// identisch mit Jederzeit — 250 Aufgaben ueber 9 Projekte. Eine Heute-Liste,
// die sich ohne Zutun mit 250 Eintraegen fuellt, ist ein Backlog mit anderem
// Namen; der Wert der Liste liegt darin, dass ein Mensch sie fuellt.
// Startklares steht in Jederzeit bereits vollstaendig, der Wegfall verliert
// also nichts, was dort nicht ohnehin steht. Die bewusst in Kauf genommene
// Folge: die Ansicht ist leer, bis jemand Termine setzt oder eine Aufgabe in
// Arbeit nimmt — das ist die ehrliche Aussage statt 250 Zeilen, die keine
// sind. GrundStartklar bleibt als Konstante bestehen, weil Jederzeit und
// Eingang sie weiter benutzen.
func waehltHeute(k Kandidat) (Urteil, bool) {
	if !k.Offen() {
		return Urteil{}, false
	}
	var u Urteil
	u.Rang = rangSonst

	// Die Frist entscheidet ueber den Eimer, weil sie die einzige der vier
	// Quellen ist, die zu SPAET sein kann.
	if k.FristTage != nil {
		switch {
		case *k.FristTage < 0:
			u.Rang = rangUeberfaellig
			u.Gruende = append(u.Gruende, GrundUeberfaellig)
			u.Zeitpunkt = k.Frist
		case *k.FristTage == 0:
			u.Rang = rangHeute
			u.Gruende = append(u.Gruende, GrundFaelligHeute)
			u.Zeitpunkt = k.Frist
		}
	}
	if k.Bead.Status == istInArbeit {
		u.Gruende = append(u.Gruende, GrundInArbeit)
	}
	// defer_until in der Vergangenheit heisst: die Aufgabe war bis dahin
	// versteckt und ist jetzt sichtbar. Das ist Things' "When", kein Termin —
	// deshalb kein eigener Rang, sondern nur die Begruendung, warum die Zeile
	// ueberhaupt da ist.
	if k.WannTage != nil && *k.WannTage <= 0 {
		u.Gruende = append(u.Gruende, GrundGeplant)
	}
	if len(u.Gruende) == 0 {
		return Urteil{}, false
	}
	// Ohne Frist sortiert innerhalb des Rests der Termin des When, damit die
	// laenger Wartenden oben stehen. Kein Termin: nil, und dann entscheiden
	// Prioritaet und letzte Aenderung (sortiereGruppen).
	if u.Zeitpunkt == nil && k.Wann != nil {
		u.Zeitpunkt = k.Wann
	}
	return u, true
}

// HeuteID ist der Name der Ansicht, wie ihn Route, Navigation und Browser
// benutzen. Als Konstante, damit ein Tippfehler beim Compiler auffaellt und
// nicht als leere Liste.
const HeuteID = "heute"

// jetztFuerAnsichten ist der Zeitpunkt, gegen den die Ansichten im Betrieb
// gebaut werden. Eine Variable, damit ein Test sie ersetzen kann, ohne die Uhr
// der Maschine zu stellen.
var jetztFuerAnsichten = time.Now
