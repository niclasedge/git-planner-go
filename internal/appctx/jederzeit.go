// Die Ansicht "Jederzeit" (gkp-1oj, Spec §5, Things' Anytime).
//
// Eine Datei, eine Regel, ein init — der Rahmen steht in ansicht.go.
package appctx

// JederzeitID ist der Name der Ansicht fuer Route, Navigation und Browser.
const JederzeitID = "jederzeit"

// Die beiden Eimer: was schon laeuft, dann was anfangbar ist. Die Gruende sind
// dieselben Konstanten wie in Heute (heute.go) und absichtlich nicht eigene —
// "startklar" muss in jeder Liste dasselbe heissen, sonst ist die Bilanz im
// Kopf zwischen zwei Ansichten nicht vergleichbar.
const (
	rangJederzeitInArbeit  = 0
	rangJederzeitStartklar = 1
)

func init() {
	regelAnmelden(Regel{
		ID:    JederzeitID,
		Titel: "Jederzeit",
		// Nach den beiden terminierten Listen, vor den beiden abgelegten: das
		// ist die Folge von Things (Today, Upcoming, Anytime, Someday) und
		// zugleich die absteigende Dringlichkeit.
		Ordnung: 30,
		Leer: "Nichts Startklares und nichts in Arbeit ohne Termin — entweder trägt alles " +
			"Anfassbare einen Termin, oder es ist nichts anfassbar.",
		// Zwei Einschraenkungen, die die Liste selbst nicht zeigt.
		//
		// Die erste stand hier bis gkp-8ww als LUECKE ("die Gruppierung nach
		// Area → Projekt fehlt noch"). Sie ist jetzt eingeloest, aber ANDERSWO,
		// und der Grund gehoert in den Hinweis, weil er sonst wie eine
		// Ausrede aussieht: eine Aufgaben-Ansicht wird ueber die Karten EINES
		// Projekts gebaut (reports_view.go: AnsichtenBauen(view.Maps, …)). Eine
		// Gruppierung nach Area → Projekt haette darin genau eine Area und
		// genau ein Projekt — sie waere keine Gruppierung, sondern eine
		// Ueberschrift. Die echte Gruppierung braucht ALLE Projekte
		// nebeneinander, und die stehen in der Bereichsansicht
		// (appctx/bereiche.go, BereicheGruppieren).
		//
		// Die Liste bleibt also flach, und das ist jetzt eine Entscheidung mit
		// Begruendung statt eines offenen Postens.
		//
		// NACHGEZOGEN (gkp-92i): der Text sagte bis hierher "Diese Liste zeigt
		// EIN Projekt" — seit AnsichtenAlleProjekte (gkp-i90) kann dieselbe
		// Regel auch ueber MEHRERE Projekte gemischt laufen (die Regeltabelle
		// ist prozessweiter, geteilter Zustand fuer beide Pfade). Ein fester
		// String kann den Fall nicht unterscheiden, ohne einen zweiten
		// Feldtyp neben Regel.Hinweis einzufuehren — dieselbe Abwaegung wie an
		// Regel.Leer (heuteLeerText): fuer EINE Zeile, die in vier von sechs
		// Regeln nichts weiter tut, ist ein zweiter Feldtyp die teurere
		// Antwort. Der Satz ist deshalb so umformuliert, dass er in BEIDEN
		// Faellen stimmt: er nennt die fehlende Gruppierung, ohne eine Aussage
		// ueber die Projektzahl der aktuell gezeigten Liste zu machen.
		Hinweis: "Startklares und Laufendes ohne Termin, flach — keine Gruppierung nach " +
			"Area → Projekt (Spec §4.3); die steht in der Ansicht Bereiche, wo alle Projekte " +
			"nebeneinander liegen. " +
			"Terminiertes steht in Heute, Demnächst oder Irgendwann.",
		Waehlt: waehltJederzeit,
	})
}

// waehltJederzeit ist Spec §5: "alles Startklare ohne Datum".
//
// ABWEICHUNG, dieselbe wie in Heute und aus demselben gemessenen Grund:
// in_progress kommt als zweite Quelle dazu. `bd ready` liefert einen Bead auf
// in_progress NICHT (gemessen 2026-09-14 gegen bd 1.2.2 an einer Wegwerf-DB).
// Ohne die vierte Quelle verschwaende eine Aufgabe in dem Moment aus "was kann
// ich anfassen", in dem jemand sie anfaengt — und das ist die Aufgabe, auf die
// die Antwort am eindeutigsten zutrifft. Die Quelle ist benannt (GrundInArbeit)
// und damit wieder entfernbar, falls die Spec anders entschieden wird.
//
// "Ohne Datum" ist pro Feld verschieden zu lesen, und das ist keine Spitzfindigkeit:
//
//   - JEDE Frist schliesst aus, auch eine vergangene. Eine ueberfaellige
//     Aufgabe in einer Liste namens "Jederzeit" waere eine falsche Auskunft;
//     sie steht in Heute, eine kuenftige in Demnächst.
//   - Ein VERGANGENES defer_until schliesst nicht aus. Es hat die Aufgabe bis
//     dahin versteckt und ist seitdem abgelaufen — es terminiert nichts mehr,
//     die Aufgabe ist ab jetzt jederzeit dran. Ein kuenftiges dagegen ist ein
//     Termin und gehoert nach Demnächst oder Irgendwann.
func waehltJederzeit(k Kandidat) (Urteil, bool) {
	if !k.Offen() {
		return Urteil{}, false
	}
	if k.FristTage != nil {
		return Urteil{}, false
	}
	if k.WannTage != nil && *k.WannTage > 0 {
		return Urteil{}, false
	}
	switch {
	case k.Bead.Status == istInArbeit:
		return Urteil{Rang: rangJederzeitInArbeit, Gruende: []string{GrundInArbeit}}, true
	// Anfassbar und nicht k.Bead.Ready (gkp-pf2): `bd ready` ist bds Urteil
	// ueber Blocker, und GitHub hat keines — dort faellt die Frage mit "ist das
	// Issue offen" zusammen. Ohne diese eine Stelle erscheint eine
	// GitHub-Aufgabe in KEINER Ansicht, obwohl Spec §4.4 sagt, sie erscheine
	// nur hier. Heute fragt weiter k.Bead.Ready, und genau das haelt sie dort
	// draussen (Begruendung an Kandidat.Anfassbar).
	case k.Anfassbar():
		return Urteil{Rang: rangJederzeitStartklar, Gruende: []string{GrundStartklar}}, true
	}
	return Urteil{}, false
}
