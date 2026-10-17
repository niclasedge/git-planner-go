// Die Ansicht "Demnächst" (gkp-1oj, Spec §5, Things' Upcoming).
//
// Eine Datei, eine Regel, ein init — der Rahmen steht in ansicht.go.
package appctx

import "time"

// DemnaechstID ist der Name der Ansicht fuer Route, Navigation und Browser.
// Als Konstante, damit ein Tippfehler beim Compiler auffaellt und nicht als
// leere Liste.
const DemnaechstID = "demnaechst"

// Die drei Zeitfenster. Sie sind gleichzeitig die Eimer der Liste und die
// Schluessel der Bilanz im Kopf — deshalb der Text, den der Leser sieht, und
// kein Kuerzel mit einer Uebersetzungstabelle im Browser daneben.
const (
	GrundMorgen     = "morgen"
	GrundDieseWoche = "diese Woche"
	GrundSpaeter    = "später"
)

const (
	rangMorgen     = 0
	rangDieseWoche = 1
	rangSpaeter    = 2
)

// dieseWocheTage ist die Breite des mittleren Fensters: die kommenden sieben
// Kalendertage. Sieben und nicht "bis Sonntag", weil ein Fenster, das am
// Wochenende auf einen Tag zusammenschrumpft, am Freitag etwas anderes bedeutet
// als am Montag — und die Zahl im Kopf der Ansicht dann nicht vergleichbar ist.
const dieseWocheTage = 7

func init() {
	regelAnmelden(Regel{
		ID:    DemnaechstID,
		Titel: "Demnächst",
		// Hinter Heute: dieselbe Frage, ein Feld weiter in die Zukunft.
		Ordnung: 20,
		Leer: "Nichts steht an — keine Aufgabe trägt einen Termin ab morgen " +
			"(due_at oder defer_until).",
		// Zwei Einschraenkungen, die die Liste selbst nicht zeigen kann, und
		// beide muessen laut Spec §4.4 sichtbar sein statt stillschweigend: die
		// Ansicht steht auf genau zwei Feldern, und GitHub-Aufgaben tragen
		// keines davon.
		Hinweis: "Was ab morgen ansteht — aus due_at (Frist) und defer_until (Wann), " +
			"chronologisch nach dem früheren der beiden. Nur beads: GitHub-Aufgaben " +
			"tragen keines der Felder (Spec §4.4, E1). Weit Vertagtes steht in Irgendwann.",
		Waehlt: waehltDemnaechst,
	})
}

// waehltDemnaechst ist Spec §5: "nach due_at / defer_until gruppiert,
// chronologisch".
//
// Verglichen werden KALENDERTAGE, nicht Zeitpunkte — dieselbe Begruendung wie
// in Heute: ein Termin heute um 23:00 ist nicht "morgen", auch wenn er mehr als
// 12 Stunden entfernt ist.
//
// Zwei Entscheidungen stecken darin, die die Spec offen laesst:
//
// ERSTENS ordnet der FRUEHERE der beiden kuenftigen Termine die Zeile ein. Eine
// Aufgabe kann beide tragen, und dann ist der naechste von beiden der naechste
// Zeitpunkt, an dem sie etwas von einem will. Die Zeile zeigt trotzdem beide
// Termine (AnsichtEintrag.Frist/Wann) — die Wahl betrifft nur die STELLE.
//
// ZWEITENS schneidet IrgendwannHorizont die Liste nach oben ab, aber nur am
// WANN. Ein defer_until in ferner Zukunft ist eine Parkentscheidung und gehoert
// nach Irgendwann; eine Frist in ferner Zukunft ist trotzdem eine Frist und
// bleibt hier. Die Kante ist damit genau eine — sie steht in irgendwann.go, und
// beide Regeln lesen dieselbe Zahl statt je eine eigene zu fuehren.
func waehltDemnaechst(k Kandidat) (Urteil, bool) {
	if !k.Offen() {
		return Urteil{}, false
	}
	// Vertagtes jenseits des Horizonts gehoert nach Irgendwann. Zuerst, weil es
	// die Zeile ganz herausnimmt und nicht bloss anders einordnet.
	if k.WannTage != nil && *k.WannTage > IrgendwannHorizont {
		return Urteil{}, false
	}

	termin, tage := frueheresterKuenftigerTermin(k)
	if termin == nil {
		return Urteil{}, false
	}

	u := Urteil{Zeitpunkt: termin}
	switch {
	case tage == 1:
		u.Rang, u.Gruende = rangMorgen, []string{GrundMorgen}
	case tage <= dieseWocheTage:
		u.Rang, u.Gruende = rangDieseWoche, []string{GrundDieseWoche}
	default:
		u.Rang, u.Gruende = rangSpaeter, []string{GrundSpaeter}
	}
	return u, true
}

// frueheresterKuenftigerTermin liefert den naechsten Termin AB MORGEN und
// seinen Kalendertagabstand. nil heisst: keiner der beiden Termine liegt in der
// Zukunft — was heute oder frueher faellig ist, steht in Heute.
func frueheresterKuenftigerTermin(k Kandidat) (*time.Time, int) {
	var best *time.Time
	var tage int
	nimm := func(t *time.Time, d *int) {
		if t == nil || d == nil || *d <= 0 {
			return
		}
		if best == nil || t.Before(*best) {
			best, tage = t, *d
		}
	}
	nimm(k.Frist, k.FristTage)
	nimm(k.Wann, k.WannTage)
	return best, tage
}
