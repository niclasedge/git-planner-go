// Package closeops ist das Belegtor vor `bd close` — die eine Stelle, an der
// entschieden wird, ob ein Bead geschlossen werden darf.
//
// Warum ein eigenes Paket: es gibt zwei Tueren zu dieser Entscheidung. Die
// erste ist internal/mcp (der Agent), die zweite eine Route der Ansicht (ein
// Haekchen im Browser). Stuende die Pruefung weiter in internal/mcp, muesste
// die Route sie nachbauen — und eine nachgebaute Pruefung ist eine Pruefung,
// die auseinanderlaufen kann. Genau das waere hier der teuerste aller Fehler:
// gibt es EINEN Weg am Tor vorbei, ist nicht nur die naechste Schliessung
// unbelegt, sondern jeder done-Beleg im Store entwertet — rueckwirkend, weil
// niemand mehr unterscheiden kann, welcher geprueft wurde.
//
// Die Reihenfolge IST der Inhalt dieses Pakets:
//
//  0. Karte pruefen                  → gehoert die Akte zu diesem Bead?
//  1. mitgegebene Belege schreiben   → schlaegt das fehl, bleibt der Bead OFFEN
//  2. Store NEU lesen                → Schritt 1 wird nicht geglaubt
//  3. kein done → ablehnen           → ausser Erzwingen
//     Bild/Datei nicht auffindbar    → eigene Meldung, anderer Fix
//     done ohne Bild → ablehnen      → eigene Meldung, anderer Fix
//  4. erst dann bd close             → gruppiert nach gleichem Grund
//
// Schritt 0 kam spaeter dazu (gkp-few) und traegt die Null zu Recht: er muss
// VOR dem ersten Schreibzugriff liegen, nicht zwischen 1 und 2. Gemessen am
// 2026-09-14: ein Aufruf mit der Karte "gkp-yy5" — einer Id, die es nicht gibt
// — legte gkp-yy5.jsonl an, schrieb die Belege hinein, pruefte Schritt 3 GEGEN
// GENAU DIESE DATEI und schloss beide Beads mit Erfolgsmeldung. Ein Tippfehler
// erzeugt sich seinen eigenen Zeugen. Schwerer zu bemerken als ein fehlender
// Beleg: der blockiert, dieser geht durch. Was dort gilt, steht in
// bdops.CheckMapMembership; hier steht nur, WANN gefragt wird.
//
// Schritt 2 ist der unscheinbarste und der wichtigste. Der mitgegebene Beleg
// {"image": "/pfad.png"} sieht im Arbeitsspeicher tadellos aus; erst
// reports.Append stellt fest, ob die Datei existiert, unter der Groessengrenze
// liegt und ueberhaupt ein Bild ist — und schreibt andernfalls "missing": true
// dazu. Wer die Eingabe prueft statt das Geschriebene, prueft eine Behauptung.
// Festgehalten in TestFehlendeBilddateiZaehltNicht.
//
// Was hier NICHT entschieden wird: wie ein Beleg aussehen muss. Das steht in
// internal/reports (HasCurrentDoneEntry / HasValidatedDoneEntry /
// HasBrokenVisualProof, streng beim Schreiben, mild beim Lesen) und bleibt
// dort. Dieses Paket fragt nur, welche der drei Antworten vorliegt, und waehlt
// die Meldung. Und es werden keine bd-Flags gebaut — die entstehen weiterhin
// ausschliesslich in internal/bdops.
package closeops

import (
	"errors"
	"fmt"
	"strings"

	"github.com/niclasedge/git-planner-go/internal/bdops"
	"github.com/niclasedge/git-planner-go/internal/collectors"
	"github.com/niclasedge/git-planner-go/internal/reports"
)

// ---------------------------------------------------------------- gruende

// Die Gruende trennen die vier Faelle, die eine Tuer verschieden beantworten
// muss. GrundKeinBeleg und GrundKeinBild bleiben getrennt, weil der FIX ein
// anderer ist: dort einen Report schreiben, hier ein Bild an den bestehenden
// haengen. Eine gemeinsame Meldung gaebe die Haelfte der Faelle falsch.
const (
	GrundEingabe   = "eingabe"
	GrundKeinBeleg = "kein-beleg"
	GrundKeinBild  = "kein-bild"
	GrundStore     = "store"
)

// Fehler traegt neben der Meldung den Grund und die betroffenen Beads. Die
// Route bildet den Grund auf einen HTTP-Status ab, der Dialog faerbt die Zeilen
// in Beads ein — beides ginge nicht, wenn nur ein Satz Text zurueckkaeme.
type Fehler struct {
	Grund string
	Beads []string
	Err   error
}

func (f *Fehler) Error() string { return f.Err.Error() }
func (f *Fehler) Unwrap() error { return f.Err }

// GrundVon liest den Grund aus einem Fehler dieses Pakets; ein fremder Fehler
// hat keinen und ergibt "".
func GrundVon(err error) string {
	var f *Fehler
	if errors.As(err, &f) {
		return f.Grund
	}
	return ""
}

// BeadsVon liest die betroffenen Beads aus einem Fehler dieses Pakets.
func BeadsVon(err error) []string {
	var f *Fehler
	if errors.As(err, &f) {
		return f.Beads
	}
	return nil
}

func eingabefehler(format string, a ...any) error {
	return &Fehler{Grund: GrundEingabe, Err: fmt.Errorf(format, a...)}
}

// ---------------------------------------------------------------- modell

// Bericht ist der done-Eintrag, der im selben Aufruf geschrieben wird. Er ist
// optional: liegt bereits ein gueltiger Beleg im Store, schliesst der Wunsch
// auch ohne.
type Bericht struct {
	Titel   string              `json:"titel"`
	Wirkung string              `json:"wirkung,omitempty"`
	Notizen string              `json:"notizen,omitempty"`
	Belege  []reports.ProofItem `json:"belege,omitempty"`
	Ersetzt string              `json:"ersetzt,omitempty"`
}

// Wunsch ist ein zu schliessender Bead.
//
// Grund landet in bds Audit-Log (.beads/interactions.jsonl) und ist die einzige
// Stelle, die festhaelt, WER einen Bead warum geschlossen hat — `bd show` zeigt
// nur den Endzustand.
type Wunsch struct {
	Bead    string   `json:"bead"`
	Grund   string   `json:"grund,omitempty"`
	Bericht *Bericht `json:"bericht,omitempty"`
}

// Auftrag ist ein Schliessvorgang ueber einen oder mehrere Beads einer Karte.
//
// Pfad ist das Repo-Verzeichnis (bd arbeitet gegen die Datenbank des
// Arbeitsverzeichnisses), Karte die Epic-Id, unter der die Belege im
// Report-Store liegen.
type Auftrag struct {
	Pfad      string   `json:"pfad"`
	Karte     string   `json:"karte"`
	Wuensche  []Wunsch `json:"wuensche"`
	Erzwingen bool     `json:"erzwingen,omitempty"`
}

// Ergebnis ist der Bericht ueber das Geschehene. OhneBeleg und OhneBild sind
// absichtlich getrennt und stehen auch im Erfolgsfall drin: wurde mit
// Erzwingen geschlossen, ist das die einzige Spur davon in der Antwort.
type Ergebnis struct {
	Geschlossen []string       `json:"geschlossen"`
	Geschrieben []string       `json:"geschrieben,omitempty"`
	OhneBeleg   []string       `json:"ohne_beleg,omitempty"`
	OhneBild    []string       `json:"ohne_bild,omitempty"`
	Aufrufe     []bdops.Result `json:"aufrufe,omitempty"`
}

// ---------------------------------------------------------------- dienst

// Schliesser ist die schmale Sicht auf internal/bdops — beide Richtungen, in
// die dieses Paket bd braucht. Sie ist exportiert, damit auch ein Test
// AUSSERHALB dieses Pakets — der der Route — das Tor ohne bd, ohne Dolt und
// ohne Datenbank durchlaufen kann. Die Signaturen sind absichtlich identisch zu
// bdops.Close und bdops.CheckMapMembership, so bleibt echtesBd eine reine
// Weiterleitung.
//
// PruefeKartenzugehoerigkeit steht bewusst in DERSELBEN Schnittstelle und nicht
// in einer zweiten: waere sie ein eigener, optionaler Mitspieler, koennte ein
// Aufrufer den Dienst ohne sie bauen — und ein Tor mit einem abschaltbaren
// Fluegel ist kein Tor. So erzwingt der Compiler bei jedem Implementierer eine
// Entscheidung.
type Schliesser interface {
	Close(pfad string, ids []string, grund string, erzwingen bool) (bdops.Result, error)
	PruefeKartenzugehoerigkeit(pfad, karte string, beads []string) (bdops.Result, error)
}

type echtesBd struct{}

func (echtesBd) Close(pfad string, ids []string, grund string, erzwingen bool) (bdops.Result, error) {
	return bdops.Close(pfad, ids, grund, erzwingen)
}

func (echtesBd) PruefeKartenzugehoerigkeit(pfad, karte string, beads []string) (bdops.Result, error) {
	return bdops.CheckMapMembership(pfad, karte, beads)
}

// EchtesBd ist der Weg zum wirklichen bd. Exportiert, weil internal/mcp zwei
// Tueren auf dieselbe Datenbank hat — beads_close geht ueber diesen Dienst, das
// report-Werkzeug schreibt direkt in den Store — und beide dieselbe Attrappe
// bekommen muessen, wenn ein Test sie ohne bd durchlaufen will.
func EchtesBd() Schliesser { return echtesBd{} }

// Dienst haelt den Weg zu bd.
type Dienst struct{ bd Schliesser }

// Neu baut den Dienst gegen das echte bd.
func Neu() *Dienst { return NeuMit(echtesBd{}) }

// NeuMit baut den Dienst gegen eine andere Tuer zu bd — siehe Schliesser.
func NeuMit(s Schliesser) *Dienst { return &Dienst{bd: s} }

// Meldung wird nach JEDEM geschriebenen Beleg aufgerufen, noch bevor das Tor
// entscheidet. Das ist Absicht: der Eintrag steht dann bereits im Store, und
// die Rueckmeldung an den Bead (ein Kommentar mit der Report-URL) gehoert zum
// Eintrag, nicht zum Schliessen. Wer sie nicht braucht, uebergibt nil.
type Meldung func(bead string, eintrag reports.Entry)

// Schliessen fuehrt den Auftrag aus — die Schritte 0 bis 4 aus dem Paketkopf,
// in dieser Reihenfolge.
func (d *Dienst) Schliessen(a Auftrag, melden Meldung) (Ergebnis, error) {
	var erg Ergebnis
	if err := pruefeEingabe(a); err != nil {
		return erg, err
	}

	// 0. Steht die Akte, in die gleich geschrieben wird, ueberhaupt zu diesem
	//    Bead? Dieser Schritt sitzt VOR Schritt 1 und nicht zwischen 1 und 2,
	//    und der Unterschied ist der ganze Bead gkp-few: der Store ist
	//    append-only. Ein Eintrag, der erst geschrieben und dann als
	//    fehlplatziert erkannt wird, bleibt liegen — es gibt kein Zuruecknehmen,
	//    nur ein supersedes, und das setzt voraus, dass jemand die richtige Akte
	//    kennt. Wer hier zu spaet prueft, prueft gar nicht.
	if err := d.kartePruefen(a); err != nil {
		return erg, err
	}

	repo := collectors.ReportsRepoName(a.Pfad)

	// 1. Die mitgegebenen Belege schreiben. Scheitert das, ist der Aufruf zu
	//    Ende: ein Bead, der geschlossen wird, obwohl sein Beleg nicht
	//    gespeichert werden konnte, waere genau der unerklaerliche Zustand,
	//    gegen den dieses Paket steht.
	for i, w := range a.Wuensche {
		if w.Bericht == nil {
			continue
		}
		eintrag, err := reports.Append(reports.Payload{
			Cwd: a.Pfad, Map: a.Karte, Bead: w.Bead, Kind: "done",
			Title: w.Bericht.Titel, Impact: w.Bericht.Wirkung, Notes: w.Bericht.Notizen,
			Proof: w.Bericht.Belege, Supersedes: w.Bericht.Ersetzt,
		})
		if err != nil {
			return erg, &Fehler{Grund: GrundStore, Beads: []string{w.Bead}, Err: fmt.Errorf(
				"wuensche[%d] (%s): Beleg konnte nicht geschrieben werden, Bead bleibt offen: %w", i, w.Bead, err)}
		}
		erg.Geschrieben = append(erg.Geschrieben, w.Bead)
		if melden != nil {
			melden(w.Bead, eintrag)
		}
	}

	// 2. Den Store NEU lesen. Nicht Schritt 1 glauben: reports.Append deutet
	//    einen Bildbeleg um (fehlende Datei, zu gross, kein Bildformat →
	//    "missing": true), und nur die geschriebene Zeile sagt, was wirklich
	//    im Store steht.
	//
	//    LoadMap(repo, a.Karte) und NICHT reports.LoadBead(repo, bead) (gkp-6l2,
	//    Abgrenzung zu gkp-e9h): Schritt 0 hat a.Karte bereits als DIE richtige
	//    Akte fuer jeden dieser Beads bestaetigt, und Schritt 1 hat GENAU DORTHIN
	//    geschrieben. Die Belegpruefung bleibt deshalb auf dieselbe Akte
	//    beschraenkt statt ueber reports.LoadBead quer durch alle Karten des Repos
	//    zu suchen — sonst koennte ein alter, fehlplatzierter Eintrag unter einer
	//    ANDEREN Karte (z. B. die verwaiste gkp-yy5.jsonl aus genau dem gkp-few-
	//    Vorfall, docs/spec-aufgaben-dashboard.md §7.1) diese Schliessung
	//    durchwinken, obwohl die GEPRUEFTE Akte selbst keinen Beleg zeigt. "Alle
	//    Schritte meinen dieselbe Akte" ist die Invariante, die gkp-few gefunden
	//    hat; LoadBead ist fuer eine andere Frage gebaut (haben wir JEMALS einen
	//    Beleg fuer diesen Bead, gleich wo) und gehoert an die /belege-Route, nicht
	//    hierher. Gegenprobe: TestBelegUnterAndererKarteZaehltNicht.
	eintraege, _, err := reports.LoadMap(repo, a.Karte)
	if err != nil {
		return erg, &Fehler{Grund: GrundStore, Err: fmt.Errorf(
			"report-store für %s/%s nicht lesbar: %w", repo, a.Karte, err)}
	}
	jeBead := map[string][]reports.Entry{}
	for _, e := range eintraege {
		jeBead[e.Bead] = append(jeBead[e.Bead], e)
	}
	var verschwunden, gruende []string
	for _, w := range a.Wuensche {
		switch {
		case !reports.HasCurrentDoneEntry(jeBead[w.Bead]):
			erg.OhneBeleg = append(erg.OhneBeleg, w.Bead)
		case !reports.HasValidatedDoneEntry(jeBead[w.Bead]):
			erg.OhneBild = append(erg.OhneBild, w.Bead)
			if reports.HasBrokenVisualProof(jeBead[w.Bead]) {
				verschwunden = append(verschwunden, w.Bead)
				// Was der Store beim Schreiben festhielt, sofern er es
				// festhielt. Leer ist der Normalfall fuer alte Eintraege —
				// siehe MeldungBelegVerschwunden.
				gruende = append(gruende, reports.BrokenVisualProofReasons(jeBead[w.Bead])...)
			}
		}
	}

	// 3. Ablehnen — in drei Stufen, weil der Fix jedesmal ein anderer ist:
	//    einen Bericht schreiben / ein Bild anhaengen / die Datei herstellen.
	if !a.Erzwingen {
		if len(erg.OhneBeleg) > 0 {
			return erg, &Fehler{Grund: GrundKeinBeleg, Beads: erg.OhneBeleg, Err: fmt.Errorf(MeldungKeinBeleg, strings.Join(erg.OhneBeleg, ", "))}
		}
		if len(verschwunden) > 0 {
			return erg, &Fehler{Grund: GrundKeinBild, Beads: verschwunden, Err: errors.New(
				belegVerschwundenText(verschwunden, gruende))}
		}
		if len(erg.OhneBild) > 0 {
			return erg, &Fehler{Grund: GrundKeinBild, Beads: erg.OhneBild, Err: fmt.Errorf(MeldungKeinBild, strings.Join(erg.OhneBild, ", "))}
		}
	}

	// 4. Schliessen, nach gleichem Grund gruppiert: Beads mit derselben
	//    Begruendung kosten einen Aufruf.
	gruppen := map[string][]string{}
	var reihenfolge []string
	for _, w := range a.Wuensche {
		if _, gesehen := gruppen[w.Grund]; !gesehen {
			reihenfolge = append(reihenfolge, w.Grund)
		}
		gruppen[w.Grund] = append(gruppen[w.Grund], w.Bead)
	}
	for _, grund := range reihenfolge {
		ids := gruppen[grund]
		res, err := d.bd.Close(a.Pfad, ids, grund, a.Erzwingen)
		erg.Aufrufe = append(erg.Aufrufe, res)
		if err != nil {
			return erg, &Fehler{Grund: bdGrund(res), Beads: ids, Err: fmt.Errorf(
				"%w (geschlossen bis dahin: %s; Belege geschrieben: %s)",
				err, oderKeine(erg.Geschlossen), oderKeine(erg.Geschrieben))}
		}
		erg.Geschlossen = append(erg.Geschlossen, ids...)
	}
	return erg, nil
}

// kartePruefen fragt bd, ob Karte und Beads zusammengehoeren, und uebersetzt
// die Antwort in einen Fehler dieses Pakets.
//
// Die beiden Ausgaenge sind absichtlich verschiedene Gruende, weil sie
// verschiedene Fehler sind:
//
//   - bd hat geantwortet und die Zuordnung stimmt nicht → GrundEingabe. An der
//     Anfrage IST etwas falsch, die Route macht daraus 400, und der Fix liegt
//     beim Aufrufer: die richtige Karte nennen.
//   - bd hat nicht geantwortet (Zeitueberschreitung, kein bd auf dem PATH) →
//     bds eigener Grund, den schliessStatus in internal/server schon auf 504
//     bzw. 500 abbildet. Hier ist nichts falsch getippt, hier fehlt die
//     Auskunft.
//
// Dass der zweite Fall trotzdem ABLEHNT und nicht durchlaesst, ist die
// unbequeme Haelfte dieser Entscheidung. Sie folgt aus derselben Einbahnstrasse
// wie die Reihenfolge: ein falsch abgelegter Eintrag bleibt liegen, ein
// abgelehnter Aufruf laesst sich wiederholen. Von zwei Fehlern ist der
// wiederholbare der richtige. Konkret heisst das: steht bds Dolt-Server nicht,
// schreibt auch das report-Werkzeug nichts mehr — gemessen ist dieser Zustand
// (127.0.0.1:3308 refused), er ist nicht theoretisch.
func (d *Dienst) kartePruefen(a Auftrag) error {
	beads := make([]string, 0, len(a.Wuensche))
	for _, w := range a.Wuensche {
		beads = append(beads, w.Bead)
	}
	res, err := d.bd.PruefeKartenzugehoerigkeit(a.Pfad, a.Karte, beads)
	if err == nil {
		return nil
	}
	if res.Failed {
		return &Fehler{Grund: bdGrund(res), Beads: beads, Err: err}
	}
	return &Fehler{Grund: GrundEingabe, Beads: beads, Err: err}
}

// MeldungKeinBeleg und MeldungKeinBild sind die beiden Ablehnungen als
// Formatvorlage. Sie stehen exportiert und nicht inline, weil sie die
// eigentliche Antwort dieses Pakets sind: eine Ablehnung, die den Fix nicht
// nennt, erzeugt einen zweiten Fehlversuch.
//
// Die Vorlagen sprechen force/reason und nicht erzwingen/grund — das ist das
// Vokabular des MCP-Schemas, aus dem die weit haeufigeren Aufrufe kommen. Die
// Ansicht baut ihren Dialogtext ohnehin aus Grund und Beads, nicht aus diesem
// Satz.
const (
	MeldungKeinBeleg = "kein aktueller done-Beleg für %s — entweder einen Bericht im selben Aufruf mitgeben " +
		"oder force:true setzen und den Grund in reason schreiben. " +
		"(Ein done-Eintrag zählt nur, wenn er nach dem letzten plan-Eintrag liegt: ein Beleg aus einer " +
		"früheren Schliessung entschuldigt diese nicht.)"

	// MeldungBelegVerschwunden ist die dritte Stufe (gkp-2ln). Sie steht
	// zwischen den beiden anderen, weil sie einen Fall beschreibt, den beide
	// falsch beraten wuerden: der Aufrufer HAT etwas Sichtbares mitgegeben, und
	// der Store konnte es nicht aufloesen — die Datei ist nicht da, leer, zu
	// gross oder kein Bild. "kein aktueller done-Beleg" schickte ihn los, einen
	// Bericht zu schreiben, den er gerade geschrieben hat; "nichts zum Ansehen"
	// forderte ein Diagramm, das er anzuhaengen glaubt. Beide erzeugen einen
	// zweiten Fehlversuch statt der Reparatur.
	//
	// Der Grund bleibt bewusst GrundKeinBild und wird KEIN vierter: die
	// HTTP-Abbildung in internal/server (schliessStatus) faellt fuer einen
	// unbekannten Grund auf 500 zurueck, und ein Aufrufer, dem der Beleg fehlt,
	// hat keinen Serverfehler ausgeloest. Wer den Fall auch in der Ansicht
	// eigens faerben will, braucht dort einen Fall mehr — eine Aenderung in
	// internal/server, nicht hier.
	MeldungBelegVerschwunden = "done-Beleg für %s nennt etwas Sichtbares, das der Store nicht auflösen konnte — " +
		"die Datei war beim Schreiben nicht da, leer, zu gross oder kein erkanntes Bildformat. " +
		"Ein relativer Pfad wird gegen cwd und gegen die Wurzel des Worktrees aufgelöst, in dem cwd liegt; " +
		"alles andere muss absolut sein. " +
		"Der Fix ist nicht ein weiterer Bericht und nicht force, sondern die Datei: das Diagramm rendern " +
		"(dot -Tsvg), den Screenshot wirklich ablegen — oder den richtigen Pfad nennen."

	// MeldungGrundZusatz haengt an, WORAN es lag, sofern reports.markMissing es
	// beim Schreiben festgehalten hat. Die Aufzaehlung der vier Ursachen oben
	// bleibt trotzdem stehen: der Store ist append-only, und jeder Beleg aus der
	// Zeit vor dem reason-Feld traegt keinen — insbesondere die 163
	// Datei-Belege, die vor resolveFileProof geschrieben wurden (gkp-2ln,
	// erster Eintrag mit "copy": 2026-09-13T22:44). Ein fehlender Grund heisst
	// also NICHT "heil" und nicht "unbekannt", sondern "damals nicht gefragt".
	MeldungGrundZusatz = " Der Store hat beim Schreiben festgehalten, woran es lag: %s."

	MeldungKeinBild = "done-Beleg für %s hat nichts zum Ansehen — ein Bead schliesst nur mit " +
		"visuellem Nachweis: proof [{\"image\":\"/abs/pfad.png\"}] (Screenshot des laufenden Stands oder " +
		"gerendertes Diagramm) oder ein {\"file\":\"...dot\"} bzw. \"...svg\". " +
		"Ein {cmd,out}-Beleg zählt dafür nicht, auch ein ausführlicher nicht: er stammt von derselben " +
		"Instanz, die 'fertig' behauptet. Ein als missing markiertes Bild zählt ebenfalls nicht. " +
		"Mit force:true umgehbar, dann den Grund in reason."
)

// belegVerschwundenText baut die dritte Ablehnung: die Meldung, und dahinter
// die vom Store festgehaltenen Gruende, falls es welche gibt.
//
// Doppelte Gruende werden zusammengefasst — schliessen drei Beads ueber
// dieselbe fehlende Datei, ist der Satz einmal hilfreich und dreimal Laerm.
func belegVerschwundenText(beads, gruende []string) string {
	text := fmt.Sprintf(MeldungBelegVerschwunden, strings.Join(beads, ", "))
	var einmalig []string
	gesehen := map[string]bool{}
	for _, g := range gruende {
		if !gesehen[g] {
			gesehen[g] = true
			einmalig = append(einmalig, g)
		}
	}
	if len(einmalig) > 0 {
		text += fmt.Sprintf(MeldungGrundZusatz, strings.Join(einmalig, "; "))
	}
	return text
}

// bdGrund holt bdops' eigenen Grund (exit | timeout | not-found) aus dem
// Ergebnis. Er wird durchgereicht statt uebersetzt: nur damit kann eine Route
// "bd hat abgelehnt" von "bd hing" unterscheiden.
func bdGrund(res bdops.Result) string {
	if res.Reason != "" {
		return res.Reason
	}
	return bdops.ReasonExit
}

func oderKeine(ids []string) string {
	if len(ids) == 0 {
		return "keine"
	}
	return strings.Join(ids, ", ")
}

// ---------------------------------------------------------------- eingabe

// pruefeEingabe laeuft VOR jedem Seiteneffekt. Alles hier Abgelehnte hat weder
// einen Beleg geschrieben noch bd angefasst.
func pruefeEingabe(a Auftrag) error {
	if strings.TrimSpace(a.Pfad) == "" {
		return eingabefehler("pfad fehlt: ohne Projektverzeichnis schriebe bd in das Arbeitsverzeichnis des Dienstes")
	}
	if strings.TrimSpace(a.Karte) == "" {
		return eingabefehler("karte fehlt: ohne Epic-Id ist nicht bestimmt, in welcher Kartei der Beleg zu suchen wäre")
	}
	if len(a.Wuensche) == 0 {
		return eingabefehler("kein bead angegeben")
	}
	for i, w := range a.Wuensche {
		if !bdops.ValidID(w.Bead) {
			return &Fehler{Grund: GrundEingabe, Err: fmt.Errorf("wuensche[%d]: ungültige bead-id %q", i, w.Bead)}
		}
		if w.Bericht != nil && strings.TrimSpace(w.Bericht.Titel) == "" {
			return &Fehler{Grund: GrundEingabe, Beads: []string{w.Bead}, Err: fmt.Errorf(
				"wuensche[%d] (%s): der bericht braucht einen titel", i, w.Bead)}
		}
		if a.Erzwingen {
			if err := pruefeErzwingungsgrund(i, w); err != nil {
				return err
			}
		}
	}
	return nil
}

// ausreden sind Formulierungen, die den FEHLBETRAG benennen statt einen Grund.
//
// Das ist bewusst eine kurze, verankerte Liste und keine Inhaltspruefung: wer
// luegen will, schreibt etwas anderes hin — und hat damit eine Unwahrheit ins
// Audit-Log geschrieben, was ein anderer, sichtbarer Akt ist. Aufgehalten wird
// hier der EHRLICHE Reflex, der den wahren Satz tippt ("kein Bild zur Hand")
// und damit force zur bequemen Abkuerzung machen wuerde. Der Preis ist ein
// gelegentlicher Fehlalarm bei einem Grund, der das Wort nur beilaeufig
// enthaelt; die Meldung sagt dann, was zu tun ist.
var ausreden = []string{
	"kein bild", "keine bild", "kein beleg", "keinen beleg", "kein screenshot",
	"kein diagramm", "kein nachweis", "nichts zum ansehen",
	"no image", "no screenshot", "no evidence", "no proof",
}

func pruefeErzwingungsgrund(i int, w Wunsch) error {
	grund := strings.TrimSpace(w.Grund)
	if grund == "" {
		return &Fehler{Grund: GrundEingabe, Beads: []string{w.Bead}, Err: fmt.Errorf(
			"wuensche[%d] (%s): force ohne Grund ist nur ein stiller Weg am Beleg vorbei — "+
				"den Grund in grund/reason schreiben, er landet in bds Audit-Log", i, w.Bead)}
	}
	klein := strings.ToLower(grund)
	for _, a := range ausreden {
		if strings.Contains(klein, a) {
			return &Fehler{Grund: GrundEingabe, Beads: []string{w.Bead}, Err: fmt.Errorf(
				"wuensche[%d] (%s): %q benennt den fehlenden Beleg, nicht den Grund — "+
					"force ist für 'wird nicht gemacht' da (entfallen, ersetzt durch …, Annahme widerlegt). "+
					"Fehlt nur das Bild, ist der Weg ein Bild, kein force", i, w.Bead, grund)}
		}
	}
	return nil
}
