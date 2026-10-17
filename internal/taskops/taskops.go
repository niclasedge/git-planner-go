// Package taskops ist die zweite Tuer zu internal/bdops: die der Ansicht.
//
// internal/mcp ist die erste — sie bedient Agenten ueber JSON-RPC. Die Ansicht
// bekommt KEINEN JSON-RPC-Hop zu sich selbst, sondern geht in dieselbe
// Go-Schicht hinein (Spec §8.4, Repo-Gedaechtnis
// `mcp-transport-vs-geteilte-go-schicht`): derselbe Prozess bedient beides, ein
// Hop zum eigenen Port kostet Serialisierung und eine zweite Fehlerquelle fuer
// nichts.
//
// Was dieses Paket deshalb NICHT tut: bd-Flags bauen. Jedes Argument entsteht
// weiterhin in internal/bdops, das sie an genau einer Stelle kennt — samt
// "--"-Terminator, Id-Pruefung und Timeout. Hier wird nur uebersetzt: vom
// Vokabular der Ansicht (Aufgabe anlegen, aendern, terminieren, Bereich setzen)
// in bdops' Datentypen. Sobald hier ein String mit einem Bindestrich vorne
// entsteht, gehoert die Funktion nach bdops.
//
// Drei Dinge, die dieses Paket ueber bdops hinaus leistet:
//
//  1. Der GRUND eines Fehlers bleibt erhalten. bdops kennt ihn
//     (Result.Reason: exit | timeout | not-found), gibt ihn aus Update und
//     Create aber nur als Text zurueck. Die Ansicht muss "bd hat abgelehnt" von
//     "bd hing" unterscheiden koennen — siehe Fehler und GrundVon.
//
//  2. Der Pfad ist Pflicht. bdops.Run mit leerem cwd erbt das
//     Arbeitsverzeichnis des DIENSTES; eine Schreiboperation ohne Projekt
//     wuerde also in irgendein Repo schreiben, statt zu scheitern.
//
//  3. Die Antwort ist der neue Stand, nicht bds Erfolgsmeldung. Nach jedem
//     Schreibvorgang wird die Aufgabe zurueckgelesen (Spec §7.2) — schon weil
//     nur bd weiss, welches Datum aus "+2w" geworden ist.
package taskops

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/niclasedge/git-planner-go/internal/areas"
	"github.com/niclasedge/git-planner-go/internal/bdops"
)

// GrundEingabe steht neben bdops' drei Gruenden fuer den vierten Fall: die
// Anfrage wurde abgelehnt, BEVOR bd gestartet wurde. Der Aufrufer trennt daran
// "der Benutzer hat Unsinn geschickt" (400) von "bd konnte nicht" (502/504).
const GrundEingabe = "eingabe"

// bdTuer ist die schmale Sicht auf internal/bdops, die dieser Dienst braucht.
// Ein Interface statt direkter Aufrufe aus genau einem Grund: der Test kommt
// damit ohne bd, ohne Dolt und ohne Datenbank aus und prueft trotzdem, WAS an
// bd ginge. Die Signaturen sind absichtlich identisch zu den bdops-Funktionen —
// so bleibt echtesBd unten eine reine Weiterleitung ohne eigene Logik.
type bdTuer interface {
	Create(pfad string, issues []bdops.NewIssue, kanten []bdops.Edge, trocken bool) (bdops.CreateResult, error)
	Update(pfad string, aenderungen []bdops.Change) ([]bdops.Result, error)
	Query(pfad string, args []string) (bdops.Result, error)
}

type echtesBd struct{}

func (echtesBd) Create(pfad string, issues []bdops.NewIssue, kanten []bdops.Edge, trocken bool) (bdops.CreateResult, error) {
	return bdops.Create(pfad, issues, kanten, trocken)
}

func (echtesBd) Update(pfad string, aenderungen []bdops.Change) ([]bdops.Result, error) {
	return bdops.Update(pfad, aenderungen)
}

func (echtesBd) Query(pfad string, args []string) (bdops.Result, error) {
	return bdops.Query(pfad, args)
}

// ---------------------------------------------------------------- fehler

// Fehler traegt den Grund neben der Meldung. bdops.Update und bdops.Create
// geben bei einem Fehlschlag errors.New(res.Error()) zurueck — die Meldung ist
// vollstaendig, der Grund aber nur noch in den mitgelieferten Result-Werten zu
// finden. Genau den braucht die Route, um einen Timeout anders zu beantworten
// als eine Ablehnung.
type Fehler struct {
	Grund string
	Err   error
}

func (f *Fehler) Error() string { return f.Err.Error() }
func (f *Fehler) Unwrap() error { return f.Err }

// GrundVon liest den Grund aus einem Fehler dieses Pakets. Ein fremder Fehler
// hat keinen und ergibt "" — der Aufrufer behandelt das wie einen Serverfehler,
// nicht wie eine Benutzereingabe.
func GrundVon(err error) string {
	var f *Fehler
	if errors.As(err, &f) {
		return f.Grund
	}
	return ""
}

func eingabefehler(format string, a ...any) error {
	return &Fehler{Grund: GrundEingabe, Err: fmt.Errorf(format, a...)}
}

// bdFehler verpackt einen Fehlschlag von bdops und holt den Grund aus den
// Aufrufen zurueck, die bdops mitgibt. Wurde gar kein bd gestartet — bdops
// lehnt Eingaben ab, bevor es einen Prozess forkt —, ist es ein Eingabefehler
// und keine Stoerung: dass diese Unterscheidung hier aus der LAENGE der
// Aufrufliste faellt und nicht aus einer eigenen Pruefung, ist Absicht. Die
// Regeln, welche Eingabe bd erreichen darf, stehen in bdops; sie hier ein
// zweites Mal zu formulieren waere genau die Doppelung, die dieses Paket
// vermeidet.
func bdFehler(aufrufe []bdops.Result, err error) error {
	grund := GrundEingabe
	for _, a := range aufrufe {
		if a.Failed && a.Reason != "" {
			grund = a.Reason
		}
	}
	return &Fehler{Grund: grund, Err: err}
}

// ---------------------------------------------------------------- modell

// Aufgabe ist der Stand einer Aufgabe, wie ihn die Ansicht braucht. Die Daten
// sind bereits gedeutet (siehe lies): kein Aufrufer bekommt hier noch einen
// String, den er selbst falsch lesen koennte.
//
// Wann und Frist sind Things' When und Deadline (Spec §4.4) und heissen in bd
// defer_until und due_at. Angelegt/Geaendert/Geschlossen sind bds eigene
// Stempel — eine andere Zeitbasis, siehe lies.
//
// Ein Elternteil fuehrt diese Struktur bewusst NICHT: `bd show --json` meldet
// ihn nicht als Feld, sondern als Eintrag in "dependencies" mit
// dependency_type "parent-child". Die Hierarchie daraus zu bauen ist Aufgabe
// des Collectors (Spec §8.2), und sie hier ein zweites Mal abzuleiten hiesse,
// zwei Stellen ueber denselben Baum verhandeln zu lassen.
type Aufgabe struct {
	ID          string     `json:"id"`
	Titel       string     `json:"titel"`
	Status      string     `json:"status"`
	Typ         string     `json:"typ,omitempty"`
	Prioritaet  int        `json:"prioritaet"`
	Wann        *time.Time `json:"wann,omitempty"`
	Frist       *time.Time `json:"frist,omitempty"`
	Angelegt    *time.Time `json:"angelegt,omitempty"`
	Geaendert   *time.Time `json:"geaendert,omitempty"`
	Geschlossen *time.Time `json:"geschlossen,omitempty"`
}

// Ergebnis ist die Antwort einer Schreiboperation: der neue Stand, nicht die
// Erfolgsmeldung. Aufrufe ist die Spur der bd-Aufrufe — sie bleibt drin, weil
// ein Teilfehlschlag (angelegt, aber Datum nicht gesetzt) sonst unsichtbar
// waere.
type Ergebnis struct {
	Aufgabe *Aufgabe       `json:"aufgabe,omitempty"`
	Projekt string         `json:"projekt,omitempty"`
	Bereich string         `json:"bereich,omitempty"`
	Aufrufe []bdops.Result `json:"aufrufe,omitempty"`
}

// AnlegenWunsch legt eine Aufgabe an.
//
// Pfad ist das Verzeichnis des Repos — bd arbeitet immer gegen die Datenbank
// des Arbeitsverzeichnisses. Nicht zu verwechseln mit BereichWunsch.Projekt,
// das den NAMEN traegt; die beiden Schluessel sehen aehnlich aus und meinen
// Verschiedenes.
//
// Wann und Frist sind hier schlichte Strings, anders als in TerminWunsch: beim
// Anlegen gibt es nichts zu loeschen, "" kann also nur "nicht angegeben"
// heissen. Dieselbe Begruendung wie bei bdops.NewIssue.
type AnlegenWunsch struct {
	Pfad         string         `json:"pfad"`
	Titel        string         `json:"titel"`
	Typ          string         `json:"typ,omitempty"`
	Prioritaet   bdops.Priority `json:"prioritaet,omitempty"`
	Beschreibung string         `json:"beschreibung,omitempty"`
	Elternteil   string         `json:"elternteil,omitempty"`
	Wann         string         `json:"wann,omitempty"`
	Frist        string         `json:"frist,omitempty"`
}

// AendernWunsch aendert den Inhalt einer Aufgabe. Termine bleiben aussen vor —
// dafuer gibt es TerminWunsch, und die Trennung ist keine Kosmetik: die
// Datumsfelder brauchen den Unterschied zwischen "nicht erwaehnt" und
// "loeschen", den ein String nicht tragen kann.
type AendernWunsch struct {
	Pfad         string         `json:"pfad"`
	ID           string         `json:"id"`
	Titel        string         `json:"titel,omitempty"`
	Beschreibung string         `json:"beschreibung,omitempty"`
	Status       string         `json:"status,omitempty"`
	Prioritaet   bdops.Priority `json:"prioritaet,omitempty"`
}

// TerminWunsch setzt oder loescht die beiden Datumsfelder.
//
// Die Zeiger sind der Kern: nil heisst "nicht erwaehnt", ein Zeiger auf ""
// heisst "loeschen". bd schreibt beide Flags als "(empty to clear)", und ein
// Termin, der sich setzen aber nie entfernen laesst, waere schlimmer als
// keiner. Der Wert selbst wandert ungeprueft zu bd: dessen Grammatik (+6h,
// tomorrow, next monday, 2026-01-15) hier nachzubauen hiesse, eine Kopie zu
// pflegen, die mit jeder bd-Version auseinanderlaufen kann — bdops.checkDateSpec
// begruendet das ausfuehrlich und macht genau eine Ausnahme.
type TerminWunsch struct {
	Pfad  string  `json:"pfad"`
	ID    string  `json:"id"`
	Wann  *string `json:"wann,omitempty"`
	Frist *string `json:"frist,omitempty"`
}

// BereichWunsch ordnet ein PROJEKT einer Area zu — nicht eine Aufgabe. Der
// Schluessel ist der Projektname (collectors.ProjectRef.Name), nicht der Pfad,
// und der Speicher ist areas.json, nicht bd: ein bd-Label wuerde ueber
// `bd create --parent` an alle Kinder vererbt und die Zuordnung ist
// repo-uebergreifend, bd dagegen strikt repo-lokal (Spec §4.3).
type BereichWunsch struct {
	Projekt string `json:"projekt"`
	Bereich string `json:"bereich"`
}

// ---------------------------------------------------------------- dienst

// Dienst buendelt die beiden Schreibwege der Ansicht: bd fuer Aufgaben,
// areas.json fuer Bereiche.
type Dienst struct {
	bd       bdTuer
	bereiche *areas.Store
}

// Neu baut den Dienst gegen das echte bd und den echten Area-Speicher.
func Neu(bereiche *areas.Store) *Dienst { return neuMit(echtesBd{}, bereiche) }

func neuMit(bd bdTuer, bereiche *areas.Store) *Dienst { return &Dienst{bd: bd, bereiche: bereiche} }

// pfadPruefen sorgt dafuer, dass kein Schreibvorgang ohne Projekt losgeht.
// bdops.Run setzt cmd.Dir nur, wenn cwd nicht leer ist — ein leerer Pfad
// schreibt also in das Arbeitsverzeichnis des DIENSTES (die Unit startet mit
// --cwd auf ein festes Repo). Ein Tippfehler in der Anfrage wuerde damit
// stillschweigend das falsche Repo treffen, statt zu scheitern.
func pfadPruefen(pfad string) (string, error) {
	pfad = strings.TrimSpace(pfad)
	if pfad == "" {
		return "", eingabefehler("pfad fehlt: ohne Projektverzeichnis schriebe bd in das Arbeitsverzeichnis des Dienstes")
	}
	return pfad, nil
}

// idPruefen haelt die Zusage "kein Pfad ohne Id" unabhaengig davon, ob unter
// diesem Paket das echte bdops oder eine Attrappe haengt. bdops.checkIDs
// prueft dasselbe — das ist keine Doppelung der Argumentlogik, sondern die
// Wiederverwendung des dafuer exportierten bdops.ValidID. Der Grund fuer die
// zweite Pruefung: ohne Id trifft `bd update` das zuletzt angefasste Issue,
// und ein Schutz, der erst eine Schicht tiefer greift, ist in einem Test mit
// Attrappe gar nicht vorhanden.
func idPruefen(id string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", eingabefehler("id fehlt: ohne Id traefe bd das zuletzt angefasste Issue")
	}
	if !bdops.ValidID(id) {
		return "", eingabefehler("ungültige bead-id %q", id)
	}
	return id, nil
}

// Anlegen erzeugt eine Aufgabe. Kanten gibt es hier keine: die Ansicht legt
// eine einzelne Aufgabe an, gegebenenfalls unter einem Elternteil. Wer einen
// ganzen Graphen braucht, nimmt bdops.Create direkt — das kann es bereits, und
// eine zweite Fassung davon hier waere Doppelung ohne Gewinn.
func (d *Dienst) Anlegen(w AnlegenWunsch) (Ergebnis, error) {
	var erg Ergebnis
	pfad, err := pfadPruefen(w.Pfad)
	if err != nil {
		return erg, err
	}
	if strings.TrimSpace(w.Titel) == "" {
		return erg, eingabefehler("titel fehlt")
	}
	erg.Projekt = pfad

	issues := []bdops.NewIssue{{
		Title:       w.Titel,
		Type:        w.Typ,
		Priority:    w.Prioritaet,
		Description: w.Beschreibung,
		Parent:      w.Elternteil,
		Due:         w.Frist,
		Defer:       w.Wann,
	}}
	res, err := d.bd.Create(pfad, issues, nil, false)
	erg.Aufrufe = res.Calls
	if err != nil {
		return erg, bdFehler(res.Calls, err)
	}
	if len(res.IDs) == 0 {
		// bdops meldet das schon als Fehler; hier steht es nur, damit der
		// Zugriff unten nicht auf eine Zusage baut, die eine Attrappe brechen
		// kann.
		return erg, &Fehler{Grund: bdops.ReasonExit, Err: errors.New("bd meldete keine angelegte id")}
	}
	return d.mitStand(erg, pfad, res.IDs[0])
}

// Aendern setzt Inhaltsfelder. Leere Felder bleiben unberuehrt — bdops' add()
// laesst sie weg, und anders als bei --parent, --due und --defer gibt es hier
// nichts zu loeschen, was ein leerer Wert bedeuten koennte.
func (d *Dienst) Aendern(w AendernWunsch) (Ergebnis, error) {
	var erg Ergebnis
	pfad, err := pfadPruefen(w.Pfad)
	if err != nil {
		return erg, err
	}
	id, err := idPruefen(w.ID)
	if err != nil {
		return erg, err
	}
	erg.Projekt = pfad

	return d.schreiben(erg, pfad, id, bdops.Change{
		IDs:         []string{id},
		Title:       w.Titel,
		Description: w.Beschreibung,
		Status:      w.Status,
		Priority:    w.Prioritaet,
	})
}

// Terminieren setzt oder loescht Wann und Frist.
//
// Erwaehnt der Wunsch keines von beiden, wird hier abgelehnt, nicht erst in
// bdops. Das ist keine Doppelung der bd-Argumentlogik, sondern der eigene
// Vertrag dieser Funktion: "terminieren" ohne Datum ist nichts, was bd
// beantworten koennte. bdops' Ablehnung ("nichts zu ändern angegeben") bleibt
// als Netz darunter, benennt aber weder die Operation noch das fehlende Feld.
func (d *Dienst) Terminieren(w TerminWunsch) (Ergebnis, error) {
	var erg Ergebnis
	pfad, err := pfadPruefen(w.Pfad)
	if err != nil {
		return erg, err
	}
	id, err := idPruefen(w.ID)
	if err != nil {
		return erg, err
	}
	if w.Wann == nil && w.Frist == nil {
		return erg, eingabefehler("weder wann noch frist angegeben: " +
			`zum Löschen den leeren String schicken ("wann": ""), nicht das Feld weglassen`)
	}
	erg.Projekt = pfad

	return d.schreiben(erg, pfad, id, bdops.Change{
		IDs: []string{id},
		// Wann ist bds --defer, Frist ist --due. Die Zeiger gehen unveraendert
		// durch, damit der Unterschied zwischen "nicht erwaehnt" und "loeschen"
		// bis zum Flag erhalten bleibt.
		Defer: w.Wann,
		Due:   w.Frist,
	})
}

// schreiben fuehrt eine Aenderung aus und liest den neuen Stand nach.
func (d *Dienst) schreiben(erg Ergebnis, pfad, id string, c bdops.Change) (Ergebnis, error) {
	aufrufe, err := d.bd.Update(pfad, []bdops.Change{c})
	erg.Aufrufe = aufrufe
	if err != nil {
		return erg, bdFehler(aufrufe, err)
	}
	return d.mitStand(erg, pfad, id)
}

// mitStand haengt den frisch gelesenen Stand an das Ergebnis. Ein Lesefehler
// NACH einem geglueckten Schreibvorgang wird gemeldet, nicht verschluckt: die
// Ansicht wuerde sonst den alten Stand weiterzeigen und den Schreibvorgang fuer
// wirkungslos halten. Die bereits gemachten Aufrufe bleiben im Ergebnis, damit
// erkennbar ist, dass geschrieben wurde.
func (d *Dienst) mitStand(erg Ergebnis, pfad, id string) (Ergebnis, error) {
	auf, res, err := d.lies(pfad, id)
	erg.Aufrufe = append(erg.Aufrufe, res)
	if err != nil {
		return erg, err
	}
	erg.Aufgabe = auf
	return erg, nil
}

// ---------------------------------------------------------------- lesen

// anzeigeJSON sind die Felder aus `bd show --id=… --json`, die die Ansicht
// braucht. Die Zeitfelder bleiben hier Strings und werden erst in aufgabeAus
// gedeutet — je Feld, siehe dort.
type anzeigeJSON struct {
	ID         string `json:"id"`
	Titel      string `json:"title"`
	Status     string `json:"status"`
	Typ        string `json:"issue_type"`
	Prioritaet int    `json:"priority"`
	DueAt      string `json:"due_at"`
	DeferUntil string `json:"defer_until"`
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at"`
	ClosedAt   string `json:"closed_at"`
}

// lies holt den Stand einer Aufgabe. Der Aufruf geht ueber bdops.Query, das
// `--readonly` voranstellt — der lesende Weg bleibt damit auch dann lesend,
// wenn hier einmal ein falsches Argument landet.
func (d *Dienst) lies(pfad, id string) (*Aufgabe, bdops.Result, error) {
	res, err := d.bd.Query(pfad, []string{"show", "--id=" + id, "--json"})
	if err != nil {
		// Eine unbekannte Id kommt hier als Reason "exit" an, nicht als
		// "not-found": bd antwortet darauf mit Exit 1 (gemessen,
		// `no issue found matching "…"`). "not-found" heisst bei bdops, dass
		// gar kein bd auf dem PATH liegt.
		return nil, res, bdFehler([]bdops.Result{res}, err)
	}
	// `bd show --json` antwortet mit einem ARRAY, auch fuer eine einzelne Id.
	var roh []anzeigeJSON
	if err := json.Unmarshal([]byte(res.Stdout), &roh); err != nil {
		return nil, res, &Fehler{Grund: bdops.ReasonExit, Err: fmt.Errorf("antwort von bd show %s unlesbar: %w", id, err)}
	}
	if len(roh) == 0 {
		return nil, res, &Fehler{Grund: bdops.ReasonExit, Err: fmt.Errorf("bd show %s lieferte keine Aufgabe", id)}
	}
	auf := aufgabeAus(roh[0])
	return &auf, res, nil
}

// aufgabeAus deutet bds Antwort. Die beiden Zeitbasen sind der einzige Grund,
// warum diese Funktion existiert: bd liefert due_at und defer_until als LOKALE
// Wanduhr mit einem Z, das luegt, waehrend created_at/updated_at/closed_at
// echtes UTC sind (gemessen, bd 1.2.2 — Tabelle in bdops, Abschnitt "dates
// coming back", und Spec §4.4). Deshalb termin() fuer das eine und stempel()
// fuer das andere; die zu vertauschen verschiebt einen Termin um den
// UTC-Versatz und kippt einen Abendtermin auf den Folgetag, also in einen
// anderen Eimer von Heute und Demnaechst.
func aufgabeAus(r anzeigeJSON) Aufgabe {
	return Aufgabe{
		ID:          r.ID,
		Titel:       r.Titel,
		Status:      r.Status,
		Typ:         r.Typ,
		Prioritaet:  r.Prioritaet,
		Wann:        termin(r.DeferUntil),
		Frist:       termin(r.DueAt),
		Angelegt:    stempel(r.CreatedAt),
		Geaendert:   stempel(r.UpdatedAt),
		Geschlossen: stempel(r.ClosedAt),
	}
}

// termin liest ein Datum, das der BENUTZER gesetzt hat.
func termin(v string) *time.Time {
	t, ok := bdops.ParseUserDate(v)
	if !ok {
		return nil
	}
	return &t
}

// stempel liest einen Zeitpunkt, den BD selbst notiert hat.
func stempel(v string) *time.Time {
	t, ok := bdops.ParseEventStamp(v)
	if !ok {
		return nil
	}
	return &t
}

// ---------------------------------------------------------------- bereich

// BereichSetzen ordnet ein Projekt einer Area zu. Kein bd im Spiel — siehe
// BereichWunsch.
func (d *Dienst) BereichSetzen(w BereichWunsch) (Ergebnis, error) {
	erg := Ergebnis{Projekt: strings.TrimSpace(w.Projekt)}
	if erg.Projekt == "" {
		return erg, eingabefehler("projekt fehlt")
	}
	if d.bereiche == nil {
		return erg, &Fehler{Err: errors.New("kein Area-Speicher verdrahtet")}
	}
	if err := d.bereiche.SetArea(erg.Projekt, w.Bereich); err != nil {
		return erg, bereichsFehler(err)
	}
	// Zurueckgelesen statt zurueckgerechnet: die Datei wird von Hand gepflegt,
	// und die Ansicht soll zeigen, was drinsteht — inklusive der Sammel-Area
	// "Ohne Bereich", die beim Loeschen entsteht und nie in der Datei steht.
	st, err := d.bereiche.Load()
	if err != nil {
		return erg, &Fehler{Err: err}
	}
	erg.Bereich = st.AreaOf(erg.Projekt)
	return erg, nil
}

// BereichAnlegenWunsch legt eine Area an, OHNE ihr ein Projekt zuzuordnen.
//
// Ein eigener Wunsch neben BereichWunsch, weil die beiden verschiedene Fragen
// stellen. BereichWunsch sagt "dieses Projekt gehoert dorthin" und braucht
// deshalb ein Projekt; hier wird der Bereich selbst benannt. Sie
// zusammenzulegen hiesse, ein Pflichtfeld optional zu machen — und damit die
// Ablehnung "projekt fehlt" aufzugeben, die einen unvollstaendigen Wunsch
// heute sichtbar macht.
type BereichAnlegenWunsch struct {
	Bereich string `json:"bereich"`
}

// BereichUmbenennenWunsch benennt eine Area um — in der Liste und in jeder
// Zuordnung (areas.Store.Umbenennen).
//
// Alt UND Neu, nicht bloss Neu: ohne den alten Namen im Rumpf haette die
// Operation kein Ziel, und die Ansicht muesste es aus ihrem Zustand ergaenzen.
// Das ist genau der Fall, in dem ein veralteter Browser-Stand auf die falsche
// Area schriebe — mit dem alten Namen im Rumpf scheitert er stattdessen
// sichtbar an "bereich gibt es nicht".
type BereichUmbenennenWunsch struct {
	Alt string `json:"alt"`
	Neu string `json:"neu"`
}

// bereichsFehler uebersetzt einen Fehler des Area-Speichers.
//
// areas.ErrEingabe ist dort die Auskunft "das war keine gueltige Eingabe" —
// leerer Name, die Sammel-Area als Ziel, eine Area die es nicht gibt. Hier wird
// daraus GrundEingabe und damit ein 400 statt eines 500. Die REGELN bleiben in
// areas: es kennt die Datei und ihre Sammel-Area, und sie hier ein zweites Mal
// zu formulieren waere die Doppelung, die dieses Paket vermeidet (siehe
// Kommentarkopf). Uebersetzt wird nur die EINORDNUNG.
func bereichsFehler(err error) error {
	if errors.Is(err, areas.ErrEingabe) {
		return &Fehler{Grund: GrundEingabe, Err: err}
	}
	return &Fehler{Err: err}
}

// BereichAnlegen traegt eine Area ein. Kein bd im Spiel — siehe BereichWunsch.
func (d *Dienst) BereichAnlegen(w BereichAnlegenWunsch) (Ergebnis, error) {
	var erg Ergebnis
	if d.bereiche == nil {
		return erg, &Fehler{Err: errors.New("kein Area-Speicher verdrahtet")}
	}
	if err := d.bereiche.Anlegen(w.Bereich); err != nil {
		return erg, bereichsFehler(err)
	}
	// Der getrimmte Name ist das, was in der Datei steht — und damit das, was
	// die Ansicht als naechstes als Ziel anbieten muss. Ihn hier aus der
	// Eingabe durchzureichen hiesse, " Arbeit " anzubieten und beim Zuordnen an
	// "Arbeit" vorbeizuschreiben.
	erg.Bereich = strings.TrimSpace(w.Bereich)
	return erg, nil
}

// BereichEinklappenWunsch klappt eine Area in der Leiste ein oder aus
// (repo-leiste, #25). Der Zustand steht in areas.json, damit er ein Neuladen
// uebersteht.
type BereichEinklappenWunsch struct {
	Bereich     string `json:"bereich"`
	Eingeklappt bool   `json:"eingeklappt"`
}

// BereichEinklappen schreibt den Einklappzustand. Kein bd im Spiel.
func (d *Dienst) BereichEinklappen(w BereichEinklappenWunsch) (Ergebnis, error) {
	var erg Ergebnis
	if d.bereiche == nil {
		return erg, &Fehler{Err: errors.New("kein Area-Speicher verdrahtet")}
	}
	if err := d.bereiche.Einklappen(w.Bereich, w.Eingeklappt); err != nil {
		return erg, bereichsFehler(err)
	}
	erg.Bereich = strings.TrimSpace(w.Bereich)
	return erg, nil
}

// BereichLoeschenWunsch entfernt eine Area aus der Liste (gkp-td3).
//
// Nur der Name, kein Schalter. areas.Store.Loeschen verweigert, solange der
// Area noch ein Projekt zugeordnet ist — ein "trotzdem"-Feld hier waere der
// Weg, diese Zusage fuer einen Klick wieder aufzugeben, und die Zuordnung ist
// von Hand gepflegt (Spec §4.3, E2).
type BereichLoeschenWunsch struct {
	Bereich string `json:"bereich"`
}

// BereichLoeschen entfernt eine leere Area. Kein bd im Spiel — siehe
// BereichWunsch.
func (d *Dienst) BereichLoeschen(w BereichLoeschenWunsch) (Ergebnis, error) {
	var erg Ergebnis
	if d.bereiche == nil {
		return erg, &Fehler{Err: errors.New("kein Area-Speicher verdrahtet")}
	}
	if err := d.bereiche.Loeschen(w.Bereich); err != nil {
		return erg, bereichsFehler(err)
	}
	// Ergebnis.Bereich bleibt LEER, und das ist der Unterschied zu den drei
	// anderen Bereichsoperationen: dort benennt das Feld die Area, in der etwas
	// jetzt liegt oder die es jetzt gibt. Nach einem Loeschen gibt es sie nicht
	// mehr — den Namen hier einzutragen waere die einzige Stelle, an der das
	// Feld auf etwas zeigte, das nicht existiert. Was nach dem Schreiben gilt,
	// steht in der Gruppierung, die die Route mitschickt (bereichsAntwort).
	return erg, nil
}

// BereichUmbenennen benennt eine Area um. Kein bd im Spiel.
func (d *Dienst) BereichUmbenennen(w BereichUmbenennenWunsch) (Ergebnis, error) {
	var erg Ergebnis
	if d.bereiche == nil {
		return erg, &Fehler{Err: errors.New("kein Area-Speicher verdrahtet")}
	}
	if err := d.bereiche.Umbenennen(w.Alt, w.Neu); err != nil {
		return erg, bereichsFehler(err)
	}
	erg.Bereich = strings.TrimSpace(w.Neu)
	return erg, nil
}
