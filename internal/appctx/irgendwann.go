// Die Ansicht "Irgendwann" (gkp-1oj, Spec §5, Things' Someday).
//
// Eine Datei, eine Regel, ein init — der Rahmen steht in ansicht.go.
package appctx

// IrgendwannID ist der Name der Ansicht fuer Route, Navigation und Browser.
const IrgendwannID = "irgendwann"

// GrundVertagt ist der einzige Grund dieser Liste — und trotzdem benannt, weil
// die Bilanz im Kopf die Gruende zaehlt und eine Liste ohne Grund dort keine
// Zahl bekaeme.
const GrundVertagt = "vertagt"

// IrgendwannHorizont ist die Kante zwischen "Demnächst" und "Irgendwann", in
// Kalendertagen ab heute. Sie steht EINMAL und wird von beiden Regeln gelesen;
// zwei Zahlen waeren zwei Gelegenheiten, dass eine Aufgabe in beiden Listen
// oder in keiner steht.
//
// Things kennt Someday als FLAGGE, nicht als Datum — die Entscheidung "nicht
// jetzt" wird dort ausgesprochen. bd hat dafuer kein Feld; das Naechste ist ein
// defer_until, das so weit weg liegt, dass es keine Planung mehr ist, sondern
// ein Parkplatz. Spec §5 sagt "weit in der Zukunft" und laesst offen, wie weit.
//
// 30 Tage, mit Begruendung und nicht nur als runde Zahl: der Monat ist die
// laengste Spanne, in der ein Termin noch eine Verabredung ist. Was vier Wochen
// weit weg liegt, wird nicht eingeplant, sondern beiseitegelegt — und eine
// Liste namens "Demnächst", in der Dinge aus dem naechsten Quartal stehen,
// beantwortet die Frage nicht mehr, fuer die man sie oeffnet.
//
// Exportiert, weil der Hinweistext der Ansicht die Zahl im Klartext nennt: eine
// Grenze, die nur im Code steht, ist fuer den Leser keine.
const IrgendwannHorizont = 30

func init() {
	regelAnmelden(Regel{
		ID:    IrgendwannID,
		Titel: "Irgendwann",
		// Hinter Jederzeit: was beiseitegelegt ist, sucht man seltener.
		Ordnung: 40,
		Leer: "Nichts beiseitegelegt — kein defer_until liegt mehr als " +
			itoa(IrgendwannHorizont) + " Tage in der Zukunft.",
		Hinweis: "Aufgaben mit defer_until mehr als " + itoa(IrgendwannHorizont) +
			" Tage in der Zukunft — beiseitegelegt, nicht eingeplant. " +
			"Nur beads: GitHub-Aufgaben tragen kein defer_until (Spec §4.4, E1).",
		Waehlt: waehltIrgendwann,
	})
}

// waehltIrgendwann ist Spec §5: "defer_until weit in der Zukunft".
//
// Nur das Wann entscheidet, auch wenn zusaetzlich eine Frist gesetzt ist. Eine
// Aufgabe, die erst in einem halben Jahr sichtbar wird, ist bis dahin nicht zu
// bearbeiten — ihre Frist aendert daran nichts und wuerde sie bloss in
// "Demnächst" einreihen, wo man sie ein halbes Jahr lang vor sich her schoebe.
// Genau diese Doppelung prueft
// TestIrgendwannUndDemnaechstTeilenSichDieMenge.
//
// Sortiert wird chronologisch aufsteigend (der Rahmen ordnet Zeitpunkte so):
// was am ehesten zurueckkommt, steht oben. Das ist die Frage, fuer die man
// diese Liste oeffnet — nicht "was ist am weitesten weg".
func waehltIrgendwann(k Kandidat) (Urteil, bool) {
	if !k.Offen() {
		return Urteil{}, false
	}
	if k.WannTage == nil || *k.WannTage <= IrgendwannHorizont {
		return Urteil{}, false
	}
	return Urteil{Rang: 0, Gruende: []string{GrundVertagt}, Zeitpunkt: k.Wann}, true
}
