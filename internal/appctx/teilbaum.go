// Der Teilbaum einer Schreibantwort (gkp-8a0, Spec §7.2).
//
// window.DATA ist ein Schnappschuss ohne Nachladeweg. Fuer eine Editier-Flaeche
// reicht das nicht, und ein Vollreload nach jedem Tastendruck erst recht nicht:
// festgelegt ist, dass jede Schreiboperation mit dem NEU GEBAUTEN Ast der
// betroffenen Aufgabe antwortet und die Ansicht genau diesen Teil ersetzt. Kein
// Polling, kein SSE.
//
// Der Ast wird hier nicht zweitgebaut, sondern herausgeschnitten: TeilbaumBauen
// laeuft durch dieselbe Funktion wie der Seitenaufbau (baueKarten in
// reports_view.go) und nimmt den Knoten, den sie liefert, unveraendert mit.
// Das ist der Kern dieser Datei. Ein eigener, schmaler Aufbau "nur fuer die
// Schreibantwort" waere die Fehlerklasse, die dieses Repo schon zweimal
// getroffen hat (gkp-1lp/gkp-b9n): eine zweite Fassung des Baums, die gruen
// ist, bis sie irgendwo nicht mit absteigt. Solange beide Seiten denselben
// Erbauer benutzen, KANN der Ast nicht vom Vollaufbau abweichen — und der Test
// in server/taskroutes_test.go misst genau das gegen ein echtes bd.
//
// Was TeilbaumBauen gegenueber CollectReportsData weglaesst, ist alles, was
// NICHT der Baum ist: der Ueberblicksgraph (ein dot-Prozess), die Historie
// (git log ueber das Repo) und die Zeitleiste. Die haengen nicht am
// geschriebenen Bead, und sie kosten das Vielfache des Baums.
package appctx

import (
	"time"
)

// Teilbaum ist die Antwort auf eine geglueckte Schreiboperation: der Ast der
// Aufgabe plus der Weg, an dem er haengt.
//
// Der Weg ist nicht Beiwerk. Ohne ihn koennte die Ansicht eine NEU ANGELEGTE
// Aufgabe nirgends einsetzen — sie hat noch keinen Platz in der Liste —, und
// die Balken ihrer Vorfahren blieben auf dem Stand von davor: eine angelegte
// Unteraufgabe aendert den Fortschritt jeder Aufgabe ueber ihr bis zur Karte
// hinauf.
type Teilbaum struct {
	// Karte ist die Karte, unter der die Aufgabe haengt — dieselbe Id wie
	// Weg[0].ID. Doppelt genannt, weil die Ansicht ihre Abschnitte ueber die
	// Kartenid adressiert und "das erste Element des Weges" eine ungeschriebene
	// Verabredung waere.
	Karte string `json:"karte"`
	// Eltern ist der direkte Elternteil. Leer heisst: die Aufgabe haengt direkt
	// unter der Karte.
	Eltern string `json:"eltern,omitempty"`
	// Weg ist die Karte, dann jeder Vorfahr bis zum Elternteil — die Aufgabe
	// selbst steht nicht darin, die steht in Aufgabe.
	Weg []AstStand `json:"weg"`
	// Aufgabe ist der Ast: die Aufgabe mit allem, was unter ihr haengt.
	Aufgabe ReportsBead `json:"aufgabe"`
}

// AstStand ist ein Vorfahr der geschriebenen Aufgabe mit dem, was sich an ihm
// geaendert haben kann.
//
// Kinder ist die Id-Folge seiner direkten Kinder, so wie der Vollaufbau sie
// sortiert hat. Sie steht hier, damit die Ansicht eine neue oder verschobene
// Zeile an die richtige Stelle setzen kann, OHNE selbst zu sortieren: die
// Reihenfolge entsteht in Go (buildReportsBeads sortiert jede Ebene nach
// letzter Aenderung), und eine zweite Sortierregel in browser.js waere genau
// die Doppelung, die CLAUDE.md ausschliesst. Die Ansicht liest die Folge, sie
// rechnet sie nicht nach.
type AstStand struct {
	ID     string    `json:"id"`
	Counts MapCounts `json:"counts"`
	Kinder []string  `json:"kinder"`
}

// TeilbaumBauen baut den Baum dieses Projekts neu und schneidet den Ast der
// Aufgabe id heraus.
//
// Der zweite Rueckgabewert ist ein GRUND und kein Fehler: er geht unveraendert
// an die Ansicht weiter, die daraufhin neu laedt statt eine halb aktualisierte
// Liste zu zeigen. Leer heisst: der Ast steht im ersten Rueckgabewert.
//
// "Geschrieben, aber der Ast ist nicht zu finden" ist ein eigener Zustand — er
// ist weder ein Fehlschlag (bd hat geschrieben) noch ein Erfolg, den die
// Ansicht verarbeiten koennte. Ohne diesen Grund saehe sie denselben Rumpf wie
// bei "nichts hat sich geaendert", und das ist die Sorte Statuslosigkeit, die
// bei den Schreibrouten schon einmal aufgefallen ist (gkp-t1o: Teilfehlschlag
// und Ablehnung kamen beide als 422 an).
func TeilbaumBauen(cwd, id string) (Teilbaum, string) {
	tb, grund, _ := StandNach(cwd, id, jetztFuerAnsichten())
	return tb, grund
}

// StandNach ist TeilbaumBauen plus die neu ausgewaehlten Aufgaben-Ansichten
// (gkp-7gb) — aus EINEM Baumaufbau.
//
// Warum beides zusammen und nicht zwei Aufrufe: der Ast allein reicht der
// Heute-Liste nicht. Wer aus ihr heraus ein Datum setzt, aendert womoeglich
// ihre MITGLIEDSCHAFT — eine Aufgabe, deren Frist auf morgen wandert, gehoert
// nicht mehr hinein, und eine, deren When auf heute rutscht, kommt hinzu. Der
// Teilbaum kann das nicht ausdruecken: er ersetzt eine Zeile, er waehlt nicht
// aus. Und die Auswahl im Browser nachzurechnen ist genau das, was hier nicht
// passieren soll.
//
// Der zweite Aufbau waere nicht falsch, nur teuer: baueKarten haengt an
// CollectBeads, und das schickt bd mehrfach los (gemessen an diesem Repo:
// Sekunden). Die Auswahl darueber kostet Mikrosekunden.
func StandNach(cwd, id string, jetzt time.Time) (Teilbaum, string, map[string]AufgabenAnsicht) {
	return StandNachMit(cwd, id, nil, jetzt)
}

// StandNachMit ist StandNach mit der zweiten Aufgabenquelle (gkp-pf2): github
// sind die Karten aus GithubKarten, die NEBEN den beads-Karten in die Ansichten
// laufen.
//
// Getrennt, weil die beiden Quellen verschieden teuer sind. Der Baum kostet
// bd-Subprozesse und liegt hinter dem Projekt-Cache; die GitHub-Haelfte kostet
// einen Netzaufruf und darf deshalb nicht auf dem Pfad stehen, der ein Projekt
// oeffnet (github.go:41, gkp-isj). Wer kein GitHub in der Hand hat, ruft weiter
// StandNach — die Schreibrouten tun genau das, ihre Antwort ist die
// beads-Haelfte, und die Ansicht holt die andere nach.
func StandNachMit(cwd, id string, github []ReportsMap, jetzt time.Time) (Teilbaum, string, map[string]AufgabenAnsicht) {
	karten := []QuellKarten{
		quellKartenAus(beadsQuelle{}, cwd),
		{Herkunft: HerkunftGithub, Faehigkeiten: githubQuelle{}.Faehigkeiten(), Maps: github},
	}
	return standAus(cwd, id, karten, jetzt)
}

// standAus ist der gemeinsame Kern von StandNachMit und AnsichtenMitGithub
// (aufgaben-quelle, #9): die Kartenliste der Quellen, der Ast aus der
// beads-Haelfte und die Ansichten ueber alle Quellen.
//
// Den Projektnamen traegt projektStempeln an jede Karte, die keinen hat — HIER,
// vor beiden Ausgaengen (gkp-k1o-Review, Befund 1): ein Repo ohne .beads/ ist
// nach E1 das Kernszenario fuer eine GitHub-only-Karte, kein Randfall, und zwei
// getrennte Stempelstellen waeren zwei Gelegenheiten, die naechste zu
// vergessen. Bevorzugt wird der Name an den beads-Karten (beads.Name), statt
// eines zweiten CollectBeads nur fuer diesen einen String; ohne beads liefert
// collectors.ReportsRepoName denselben Namen ueber Git, und ohne Git "unknown"
// statt "" — eine stumme leere Karte waere schlechter als ein sichtbarer
// Platzhalter.
//
// Fehlt der Aufgabenbaum, kommen leere Ansichten und nicht nil: der Aufrufer
// soll "ich habe nichts gefunden" nicht von "ich habe nicht nachgesehen"
// unterscheiden muessen. Die anderen Quellen bleiben auch ohne beads stehen:
// ein Repo kann nach E1 Aufgaben nur auf einer Seite haben.
func standAus(cwd, id string, karten []QuellKarten, jetzt time.Time) (Teilbaum, string, map[string]AufgabenAnsicht) {
	projektStempeln(karten, cwd)
	ansichten := AnsichtenBauenAus(karten, jetzt)
	for _, qk := range karten {
		if qk.Herkunft != HerkunftBeads {
			continue
		}
		if qk.Grund != "" {
			return Teilbaum{}, qk.Grund, ansichten
		}
		tb, grund := TeilbaumAus(qk.Maps, id)
		return tb, grund, ansichten
	}
	return Teilbaum{}, "kein Aufgabenbaum für dieses Verzeichnis", ansichten
}

// AnsichtenNach baut nur die Ansichten neu — der Weg fuer eine Schreiboperation
// ohne einzelne betroffene Aufgabe (das Schliessen kann mehrere Beads auf
// einmal betreffen, siehe closeops.Auftrag).
func AnsichtenNach(cwd string, jetzt time.Time) map[string]AufgabenAnsicht {
	_, _, ansichten := StandNach(cwd, "", jetzt)
	return ansichten
}

// TeilbaumAus schneidet den Ast aus fertig gebauten Karten. Getrennt von
// TeilbaumBauen, damit das Schneiden ohne bd pruefbar ist.
func TeilbaumAus(maps []ReportsMap, id string) (Teilbaum, string) {
	for _, m := range maps {
		if m.ID == id {
			// Eine Karte ist kein Ast: sie haengt an keinem Elternteil, in den
			// die Ansicht sie einsetzen koennte. Das ist kein Fehler, sondern
			// eine Auskunft — die Ansicht laedt die Karte neu.
			return Teilbaum{}, id + " ist eine Karte, kein Teilbaum — Ansicht neu laden"
		}
		weg := []AstStand{{ID: m.ID, Counts: m.Counts, Kinder: kinderIDs(m.Beads)}}
		if tb, ok := astSuchen(m.Beads, id, m.ID, "", weg); ok {
			return tb, ""
		}
	}
	return Teilbaum{}, id + " steht in keinem Ast dieser Ansicht — Ansicht neu laden"
}

// astSuchen steigt in den Baum und traegt den Weg mit.
//
// Der Weg ist ein Wert je Zweig, kein fortgeschriebener Sammler: jeder Abstieg
// bekommt seine eigene Kopie. Die Alternative — ein gemeinsames Slice, das
// beim Abstieg waechst — muesste beim Verlassen eines Zweigs wieder kuerzen,
// und genau dieses Zuruecknehmen ist der Schritt, den man vergisst. Dann truege
// der Weg die Vorfahren der zuvor abgesuchten Geschwister mit
// (TestTeilbaumNimmtNichtDenWegEinesGeschwisters). Kopieren kostet hier nichts:
// der Weg ist so lang wie der Baum tief.
func astSuchen(beads []ReportsBead, id, karte, eltern string, weg []AstStand) (Teilbaum, bool) {
	for _, b := range beads {
		if b.ID == id {
			return Teilbaum{Karte: karte, Eltern: eltern, Weg: weg, Aufgabe: b}, true
		}
		if len(b.Children) == 0 {
			continue
		}
		unten := make([]AstStand, len(weg), len(weg)+1)
		copy(unten, weg)
		unten = append(unten, AstStand{ID: b.ID, Counts: astCounts(b), Kinder: kinderIDs(b.Children)})
		if tb, ok := astSuchen(b.Children, id, karte, b.ID, unten); ok {
			return tb, true
		}
	}
	return Teilbaum{}, false
}

// astCounts sind die Zahlen, die die Ansicht an diesem Knoten ZEIGT — also das
// Feld, das der Erbauer gesetzt hat, und nicht eine zweite Rechnung daneben.
// Der Zeiger ist nil, sobald ein Knoten keine Unteraufgaben hat (gkp-wsk); auf
// dem Weg kommt das nicht vor, weil nur ueber Knoten mit Kindern abgestiegen
// wird. Der Rueckfall auf countBeads steht trotzdem da: ein von Hand gebauter
// Baum (Test, kuenftiger zweiter Erbauer) muss hier keine Nullzahlen liefern.
func astCounts(b ReportsBead) MapCounts {
	if b.Counts != nil {
		return *b.Counts
	}
	return countBeads(b.Children)
}

// kinderIDs ist die Reihenfolge einer Ebene, wie der Vollaufbau sie sortiert
// hat.
func kinderIDs(beads []ReportsBead) []string {
	out := make([]string, 0, len(beads))
	for _, b := range beads {
		out = append(out, b.ID)
	}
	return out
}
