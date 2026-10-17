// Package areas ist der vierte Schreibort des Dienstes (neben dem
// Report-Store, den Beleg-Bildern und dem Cache) und haelt die einzige
// Information, die nirgends sonst herkommt: welches Projekt zu welcher Area
// gehoert. Spec: docs/spec-aufgaben-dashboard.md §4.3, Entscheidung E2.
//
// Bewusst kein bd-Label: `bd create --parent` vererbt Labels an alle Kinder
// (messbar an `wayfinder:map`, 45 Beads, davon 2 Epics), und die Zuordnung ist
// repo-uebergreifend, beads dagegen strikt repo-lokal.
//
// Das Paket ist ein Blatt — es importiert nichts aus internal/. Verdrahtet
// wird es in gkp-8ww.
package areas

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Version ist die Schemaversion, die dieses Paket schreibt.
const Version = 1

// ErrEingabe markiert eine abgelehnte EINGABE — im Unterschied zu einem
// gescheiterten Schreibvorgang.
//
// Der Unterschied ist nicht akademisch: er entscheidet, ob der Aufrufer einen
// 400 oder einen 500 schickt, also ob der Benutzer erfaehrt "so geht das
// nicht" oder "der Dienst ist kaputt". Die Regeln, welche Eingabe zulaessig
// ist, stehen hier — das Paket kennt die Datei und ihre Sammel-Area. Sie in
// jeder Schicht darueber noch einmal zu formulieren waere die Doppelung, die
// taskops' Kommentarkopf ausdruecklich vermeidet.
var ErrEingabe = errors.New("ungültige Eingabe")

// OhneBereich ist die Sammel-Area fuer jedes Projekt ohne Eintrag (§4.3). Sie
// steht nie in der Datei, sondern entsteht beim Lesen — sonst muesste jedes
// neu entdeckte Projekt erst eingetragen werden, um ueberhaupt zu erscheinen.
const OhneBereich = "Ohne Bereich"

// State ist der Inhalt von areas.json.
//
// unbekannt traegt jedes Feld der Datei, das dieses Paket nicht kennt,
// unveraendert durch den Lese-Schreib-Zyklus. Ohne das waere jeder Schreibzug
// des Dienstes eine stille Amputation der Datei: sie wird von Hand gepflegt
// (§4.3), und eine neuere Version schreibt womoeglich mehr hinein, als diese
// hier liest — etwa die noch offene manuelle Reihenfolge aus O1.
type State struct {
	Version   int               `json:"version"`
	Areas     []string          `json:"areas"`
	Zuordnung map[string]string `json:"zuordnung"`
	// Eingeklappt nennt die Areas, die die Leiste eingeklappt zeigt
	// (repo-leiste, #25). Ein Ansichtszustand, aber einer, der ein Neuladen
	// und einen zweiten Browser ueberstehen soll — deshalb in der Datei und
	// nicht im localStorage. Darf auch OhneBereich enthalten.
	Eingeklappt []string `json:"eingeklappt,omitempty"`

	unbekannt map[string]json.RawMessage
}

// bekannteFelder sind die Schluessel, die State selbst besetzt; alles andere
// landet in unbekannt.
var bekannteFelder = []string{"version", "areas", "zuordnung", "eingeklappt"}

// UnmarshalJSON liest die bekannten Felder typisiert und hebt den Rest roh auf.
func (s *State) UnmarshalJSON(b []byte) error {
	var roh map[string]json.RawMessage
	if err := json.Unmarshal(b, &roh); err != nil {
		return err
	}
	out := emptyState()
	if v, ok := roh["version"]; ok {
		if err := json.Unmarshal(v, &out.Version); err != nil {
			return fmt.Errorf("feld version: %w", err)
		}
	}
	if v, ok := roh["areas"]; ok {
		if err := json.Unmarshal(v, &out.Areas); err != nil {
			return fmt.Errorf("feld areas: %w", err)
		}
	}
	if v, ok := roh["zuordnung"]; ok {
		if err := json.Unmarshal(v, &out.Zuordnung); err != nil {
			return fmt.Errorf("feld zuordnung: %w", err)
		}
	}
	if v, ok := roh["eingeklappt"]; ok {
		if err := json.Unmarshal(v, &out.Eingeklappt); err != nil {
			return fmt.Errorf("feld eingeklappt: %w", err)
		}
	}
	// null in der Datei darf den Zustand nicht unbenutzbar machen.
	if out.Areas == nil {
		out.Areas = []string{}
	}
	if out.Zuordnung == nil {
		out.Zuordnung = map[string]string{}
	}
	for _, k := range bekannteFelder {
		delete(roh, k)
	}
	if len(roh) > 0 {
		out.unbekannt = roh
	}
	*s = out
	return nil
}

// MarshalJSON schreibt die unbekannten Felder wieder mit. Die bekannten Felder
// gewinnen — sie sind der Grund, warum ueberhaupt geschrieben wird.
func (s State) MarshalJSON() ([]byte, error) {
	out := make(map[string]json.RawMessage, len(s.unbekannt)+len(bekannteFelder))
	for k, v := range s.unbekannt {
		out[k] = v
	}
	felder := map[string]any{
		"version":   s.Version,
		"areas":     s.Areas,
		"zuordnung": s.Zuordnung,
	}
	// Kein leeres Feld in eine Datei, die es vorher nicht hatte: sie wird von
	// Hand gepflegt, und ein "eingeklappt": [] nach jedem Aufklappen waere
	// Rauschen im Diff.
	if len(s.Eingeklappt) > 0 {
		felder["eingeklappt"] = s.Eingeklappt
	}
	for feld, wert := range felder {
		b, err := json.Marshal(wert)
		if err != nil {
			return nil, fmt.Errorf("feld %s: %w", feld, err)
		}
		out[feld] = b
	}
	return json.Marshal(out)
}

// AreaOf liefert die Area eines Projekts, Schluessel ist der Projektname wie
// collectors.ProjectRef.Name. Kein Eintrag — oder ein leer gebliebener —
// ergibt OhneBereich statt eines leeren Strings: der Aufrufer gruppiert danach,
// und eine Gruppe ohne Namen waere in der Ansicht nicht darstellbar.
func (s State) AreaOf(projekt string) string {
	if a := strings.TrimSpace(s.Zuordnung[projekt]); a != "" {
		return a
	}
	return OhneBereich
}

// IstEingeklappt sagt, ob die Leiste die Area eingeklappt zeigt.
func (s State) IstEingeklappt(area string) bool {
	for _, a := range s.Eingeklappt {
		if a == area {
			return true
		}
	}
	return false
}

// Einklappen traegt eine Area in Eingeklappt ein (zu) oder nimmt sie heraus.
// Anders als Anlegen darf der Name OhneBereich sein: die Sammel-Area steht in
// der Leiste wie jede andere und laesst sich genauso einklappen. Ob die Area
// in der Liste steht, prueft Einklappen nicht — die Leiste zeigt auch Areas,
// die es nur ueber eine Zuordnung gibt.
func (s *Store) Einklappen(area string, zu bool) error {
	area = strings.TrimSpace(area)
	if area == "" {
		return fmt.Errorf("%w: bereich ist Pflicht", ErrEingabe)
	}
	return s.Update(func(st *State) error {
		rest := make([]string, 0, len(st.Eingeklappt)+1)
		for _, a := range st.Eingeklappt {
			if a != area {
				rest = append(rest, a)
			}
		}
		if zu {
			rest = append(rest, area)
		}
		st.Eingeklappt = rest
		return nil
	})
}

// emptyState ist der Zustand, den eine fehlende Datei ergibt: gueltig und
// sofort benutzbar, damit der Aufrufer keinen nil-Fall behandeln muss.
func emptyState() State {
	return State{Version: Version, Areas: []string{}, Zuordnung: map[string]string{}}
}

// DefaultPath liefert den Pfad der Area-Datei (CC_AREAS_FILE ueberschreibt —
// dieselbe Loesung wie cache.CacheFile(), damit Tests nicht an der echten
// Datei des Benutzers arbeiten).
func DefaultPath() string {
	if env := os.Getenv("CC_AREAS_FILE"); env != "" {
		return env
	}
	return filepath.Join(homeDir(), ".config", "git-planner", "areas.json")
}

func homeDir() string {
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	return "/tmp"
}

// Store ist der serialisierte Zugang zu genau einer Area-Datei.
type Store struct {
	mu   sync.Mutex
	path string
}

// Open bindet einen Store an einen festen Pfad.
func Open(path string) *Store { return &Store{path: path} }

// Default bindet einen Store an DefaultPath(). Der Pfad wird bei jedem Aufruf
// neu aufgeloest, damit ein Test, der CC_AREAS_FILE setzt, auch dann greift,
// wenn der Store vorher entstanden ist.
func Default() *Store { return Open(DefaultPath()) }

// Path ist der Pfad, an dem dieser Store arbeitet.
func (s *Store) Path() string { return s.path }

// Load liest den Zustand. Eine fehlende Datei ist kein Fehler, sondern der
// leere Zustand — Vorbild reports.ListMaps: "noch nichts geschrieben" ist ein
// normaler Zustand und darf den Aufrufer nicht zu einer Fehlerbehandlung
// zwingen.
func (s *Store) Load() (State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadLocked()
}

func (s *Store) loadLocked() (State, error) {
	b, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return emptyState(), nil
		}
		return emptyState(), err
	}
	st := emptyState()
	if err := json.Unmarshal(b, &st); err != nil {
		// Kaputtes JSON wird GEMELDET, nicht stillschweigend zum leeren
		// Zustand gemacht. Im Repo gibt es beide Vorbilder, und sie
		// unterscheiden sich in genau einem Punkt — was der Verlust kostet:
		//
		//   cache.ReadCacheFile verwirft still. Richtig: der Cache ist
		//   abgeleitet, ein Neuaufbau kostet Sekunden.
		//   reports.readBoundedLine ueberspringt die kaputte ZEILE und zaehlt
		//   sie. Richtig: eine Zeile aufgeben rettet die tausend anderen.
		//
		// areas.json ist keins von beidem: von Hand gepflegt, nirgends
		// ableitbar, und ein einziges JSON-Objekt — "ueberspringen" gibt es
		// nicht, es waere immer die ganze Datei. Ein leerer Zustand saehe aus
		// wie "noch nichts zugeordnet", und der naechste Schreibzug wuerde die
		// Datei mit genau dieser Leere ueberschreiben. Aus einem Tippfehler
		// beim Handeditieren wuerde so ein endgueltiger Verlust.
		//
		// Der zurueckgegebene Zustand bleibt trotzdem benutzbar (nicht nil),
		// damit ein Aufrufer, der den Fehler nur protokolliert und die Ansicht
		// weiterbaut, nicht zusaetzlich abstuerzt.
		return emptyState(), fmt.Errorf("areas.json %s ist unlesbar: %w", s.path, err)
	}
	return st, nil
}

// Update liest, aendert und schreibt unter EINEM Lock. Das ist der einzige
// Weg, der nebenlaeufig sicher ist: Load+Save aus zwei Requests heraus waere
// ein klassisches Lost Update — beide lesen denselben Stand, beide schreiben
// ihren, der zweite Rename macht den ersten spurlos ungeschehen. Der Mutex ist
// deshalb nicht Vorsicht, sondern Bedingung; der Dienst bedient mehrere
// Requests gleichzeitig.
//
// Prozessuebergreifend gibt es bewusst keine Sperre: Schreiber ist genau
// dieser eine Dienst. Ein Mensch, der die Datei parallel editiert, verliert im
// schlimmsten Fall seine Aenderung — aber nie die Datei, dafuer sorgt der
// atomare Rename.
func (s *Store) Update(fn func(*State) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, err := s.loadLocked()
	if err != nil {
		return err // kaputte Datei: lieber nicht schreiben, siehe loadLocked
	}
	if err := fn(&st); err != nil {
		return err
	}
	return s.saveLocked(st)
}

// SetArea ordnet ein Projekt einer Area zu. Eine leere Area loescht die
// Zuordnung — das Projekt faellt damit auf OhneBereich zurueck, statt mit
// einem leeren Bereichsnamen in der Datei zu stehen.
func (s *Store) SetArea(projekt, area string) error {
	projekt = strings.TrimSpace(projekt)
	if projekt == "" {
		return fmt.Errorf("%w: projekt ist Pflicht", ErrEingabe)
	}
	area = strings.TrimSpace(area)
	// OhneBereich ist kein Name, sondern das FEHLEN eines Namens. Wer ihn
	// zuweist, meint "Zuordnung loesen" — und genau das passiert dann auch,
	// statt die Zeichenkette in die Datei zu schreiben und dort einen zweiten,
	// gleich aussehenden Eimer entstehen zu lassen.
	if area == OhneBereich {
		area = ""
	}
	return s.Update(func(st *State) error {
		if area == "" {
			delete(st.Zuordnung, projekt)
			return nil
		}
		st.Zuordnung[projekt] = area
		// Eine zugewiesene Area gehoert auch in die Liste, sonst kennt die
		// Ansicht einen Bereich, den sie nicht anbieten kann.
		st.areaEintragen(area)
		return nil
	})
}

// areaEintragen haengt eine Area an die Liste, wenn sie fehlt. Der Vergleich
// ist exakt und nicht schreibweisen-unabhaengig: "Arbeit" und "arbeit" sind in
// der Zuordnung zwei verschiedene Schluessel, und die Liste muss dasselbe
// sagen wie die Zuordnung.
func (st *State) areaEintragen(area string) {
	for _, a := range st.Areas {
		if a == area {
			return
		}
	}
	st.Areas = append(st.Areas, area)
}

// pruefeName ist die eine Stelle, die entscheidet, was ein Area-Name sein darf.
//
// Beide Ablehnungen tragen ErrEingabe, damit der Aufrufer sie von einem
// Schreibfehler unterscheiden kann: eine abgelehnte Eingabe ist ein 400, ein
// gescheiterter Rename ein 500. Ohne den Unterschied waere ein Tippfehler des
// Benutzers eine Stoerungsmeldung des Dienstes.
func pruefeName(area, feld string) (string, error) {
	area = strings.TrimSpace(area)
	if area == "" {
		return "", fmt.Errorf("%w: %s ist Pflicht", ErrEingabe, feld)
	}
	// OhneBereich entsteht beim LESEN (AreaOf) und steht nie in der Datei.
	// Duerfte man ihn anlegen, gaebe es zwei Gruppen mit demselben Titel: eine
	// echte und die synthetische — mit verschiedenem Verhalten beim Umbenennen
	// und beim Loesen der Zuordnung.
	if area == OhneBereich {
		return "", fmt.Errorf("%w: %q ist die Sammel-Area und entsteht beim Lesen — sie laesst sich nicht anlegen", ErrEingabe, OhneBereich)
	}
	return area, nil
}

// Anlegen traegt eine Area in die Liste ein, ohne ihr ein Projekt zuzuordnen.
//
// Der eigene Weg neben SetArea ist noetig, weil dort eine Area nur als
// NEBENWIRKUNG einer Zuweisung entsteht. Eine Ansicht, die erst ein Projekt
// zuordnen muesste, um einen Bereich ueberhaupt anbieten zu koennen, dreht die
// Reihenfolge um, in der ein Mensch das tut: erst den Bereich benennen, dann
// einsortieren.
//
// Eine schon vorhandene Area ist KEIN Fehler. Fuer den Aufrufer ist "gibt es
// jetzt" das Ergebnis, und ob sie vorher schon da war, aendert daran nichts —
// zwei Eintraege desselben Namens dagegen waeren zwei Gruppen mit einem Titel.
func (s *Store) Anlegen(area string) error {
	area, err := pruefeName(area, "bereich")
	if err != nil {
		return err
	}
	return s.Update(func(st *State) error {
		st.areaEintragen(area)
		return nil
	})
}

// Umbenennen benennt eine Area um — in der Liste UND in jeder Zuordnung.
//
// Die drei Eigenschaften, die nicht von selbst herauskommen:
//
//  1. Die Zuordnungen ziehen mit. Sonst zeigte die Ansicht danach eine leere
//     neue Area neben Projekten unter einem Namen, den es nicht mehr gibt.
//  2. Die STELLE in der Liste bleibt. Die Reihenfolge der Datei ist die
//     Reihenfolge der Ansicht (appctx.BereicheGruppieren); ein Umbenennen ist
//     kein Umsortieren, und die Liste ist von Hand gepflegt.
//  3. Der alte Name darf auch nur in der Zuordnung stehen. Bei einer von Hand
//     editierten Datei fehlt er in `areas` — liesse sich so eine Area nicht
//     umbenennen, waere die Operation ausgerechnet dort nicht verfuegbar, wo
//     die Pflege am noetigsten ist.
//
// Ein Ziel, das es schon gibt, VERSCHMILZT die beiden Areas. Das ist eine
// zulaessige Absicht ("die zwei waren dasselbe"), und sie zu verbieten hiesse,
// den Umweg ueber n einzelne Zuweisungen zu erzwingen. Der Name darf danach
// trotzdem nur EINMAL in der Liste stehen.
func (s *Store) Umbenennen(alt, neu string) error {
	alt = strings.TrimSpace(alt)
	if alt == "" {
		return fmt.Errorf("%w: alter bereich ist Pflicht", ErrEingabe)
	}
	neu, err := pruefeName(neu, "neuer bereich")
	if err != nil {
		return err
	}
	return s.Update(func(st *State) error {
		bekannt := false
		for _, a := range st.Areas {
			if a == alt {
				bekannt = true
			}
		}
		for _, a := range st.Zuordnung {
			if a == alt {
				bekannt = true
			}
		}
		if !bekannt {
			// Stillschweigend nichts tun waere eine Falschmeldung: ein
			// Tippfehler im alten Namen kaeme als Erfolg zurueck.
			return fmt.Errorf("%w: bereich %q gibt es nicht", ErrEingabe, alt)
		}
		if alt == neu {
			return nil
		}
		for projekt, a := range st.Zuordnung {
			if a == alt {
				st.Zuordnung[projekt] = neu
			}
		}
		// An Ort und Stelle ersetzen, dann eine etwaige Dublette entfernen —
		// das haelt die Position und traegt den Verschmelzungsfall zugleich.
		liste := make([]string, 0, len(st.Areas))
		gesehen := map[string]bool{}
		ersetzt := false
		for _, a := range st.Areas {
			if a == alt {
				a = neu
				ersetzt = true
			}
			if gesehen[a] {
				continue
			}
			gesehen[a] = true
			liste = append(liste, a)
		}
		st.Areas = liste
		for i, a := range st.Eingeklappt {
			if a == alt {
				st.Eingeklappt[i] = neu
			}
		}
		if !ersetzt {
			// alt stand nur in der Zuordnung. Wer umbenennt, pflegt — also
			// soll die Liste den neuen Namen kennen.
			st.areaEintragen(neu)
		}
		return nil
	})
}

// Loeschen entfernt eine Area aus der Liste — aber nur, wenn ihr kein Projekt
// zugeordnet ist.
//
// DIE Entscheidung dieser Operation (gkp-td3). Zur Wahl stand, die Projekte
// beim Loeschen still nach OhneBereich fallen zu lassen. Dagegen spricht, was
// diese Datei ist: die Zuordnung wird von HAND gepflegt (Spec §4.3, E2) und ist
// nirgends ableitbar — genau der Grund, aus dem loadLocked eine kaputte Datei
// meldet statt sie zum leeren Zustand zu machen. Ein Klick, der n solcher
// Entscheidungen wegnimmt, ist nicht rueckgaengig zu machen: danach weiss
// niemand mehr, welche n es waren. Der umgekehrte Weg dagegen ist bei jedem
// einzelnen Schritt umkehrbar — ein Projekt umhaengen kostet eine Auswahl, und
// wer sich vertut, waehlt zurueck.
//
// Der Preis ist ein zusaetzlicher Schritt, und der wird hier bezahlt, indem die
// Ablehnung die Projekte NENNT: sie sagt damit nicht nur "geht nicht", sondern
// auch, was zu tun ist. Wer eine ganze Area anderswohin schieben will, hat
// ausserdem Umbenennen — ein Ziel, das es schon gibt, verschmilzt die beiden.
//
// Ein bewusst NICHT eingebauter Schalter: kein "trotzdem loeschen". Er gaebe
// die Zusage fuer einen Klick wieder her und machte aus der Regel eine
// Empfehlung.
func (s *Store) Loeschen(area string) error {
	area = strings.TrimSpace(area)
	if area == "" {
		return fmt.Errorf("%w: bereich ist Pflicht", ErrEingabe)
	}
	// Kein pruefeName: dessen Ablehnung begruendet sich mit dem ANLEGEN ("sie
	// laesst sich nicht anlegen"), und das waere hier die falsche Auskunft. Der
	// Grund ist auch ein anderer — beim Anlegen entstuenden zwei Gruppen mit
	// einem Titel, hier fehlt schlicht das Ziel: OhneBereich steht in keiner
	// Datei, es gibt nichts zu entfernen. Der einzige denkbare Sinn waere "alle
	// unzugeordneten Projekte verstecken", also das Gegenteil von §4.3
	// ("sichtbar, damit die Pflege auffaellt").
	if area == OhneBereich {
		return fmt.Errorf("%w: %q ist die Sammel-Area und entsteht beim Lesen — sie steht in keiner Datei und laesst sich nicht loeschen", ErrEingabe, OhneBereich)
	}
	return s.Update(func(st *State) error {
		belegt := make([]string, 0, len(st.Zuordnung))
		for projekt, a := range st.Zuordnung {
			if a == area {
				belegt = append(belegt, projekt)
			}
		}
		// Eine Area, die NUR in der Zuordnung steht, gibt es (die Datei wird von
		// Hand editiert) — sie ist belegt, nicht unbekannt. Andersherum bekaeme
		// der Benutzer "gibt es nicht" zu einer Gruppe, die vor ihm auf dem
		// Schirm steht: BereicheGruppieren zeigt sie ausdruecklich an.
		bekannt := len(belegt) > 0
		for _, a := range st.Areas {
			if a == area {
				bekannt = true
			}
		}
		if !bekannt {
			// Stillschweigend nichts tun waere eine Falschmeldung: ein
			// Tippfehler kaeme als Erfolg zurueck, und die Area stuende beim
			// naechsten Seitenaufbau wieder da. Dieselbe Ablehnung wie in
			// Umbenennen.
			return fmt.Errorf("%w: bereich %q gibt es nicht", ErrEingabe, area)
		}
		if len(belegt) > 0 {
			sort.Strings(belegt) // eine Go-Karte iteriert gemischt; die Meldung soll reproduzierbar sein
			return fmt.Errorf("%w: bereich %q ist noch belegt — %s. Ordne die Projekte erst anders zu; ein Loeschen soll keine Zuordnung vernichten, sie wird von Hand gepflegt", ErrEingabe, area, aufzaehlung(belegt))
		}
		liste := make([]string, 0, len(st.Areas))
		for _, a := range st.Areas {
			if a == area {
				continue // die Stelle der uebrigen bleibt: Dateiordnung ist Ansichtsordnung
			}
			liste = append(liste, a)
		}
		st.Areas = liste
		return nil
	})
}

// aufzaehlung macht aus Namen eine lesbare Aufzaehlung und deckelt sie bei
// drei. Ohne Deckel waere die Meldung bei einer grossen Area eine Bildschirm
// fuellende Zeile — und der Benutzer braucht nicht die ganze Liste, sondern den
// Hinweis, WO er nachsieht; die Gruppe steht daneben in der Ansicht.
func aufzaehlung(namen []string) string {
	if len(namen) <= 3 {
		return strings.Join(namen, ", ")
	}
	return fmt.Sprintf("%s und %d weitere", strings.Join(namen[:3], ", "), len(namen)-3)
}

// renameFile ist os.Rename als Variable — der eine Schritt, der aus zwei
// Dateien eine macht, und damit die Stelle, an der ein Test den Abbruch
// nachstellt.
var renameFile = os.Rename

// Save schreibt den Zustand atomar: temporaere Datei im SELBEN Verzeichnis,
// fsync, dann os.Rename. Im selben Verzeichnis, weil Rename nur innerhalb
// eines Dateisystems atomar ist — ueber $TMPDIR waere es ein Kopieren mit
// genau dem Fenster, das hier vermieden werden soll.
//
// Konsequenz: ein Leser sieht immer entweder den alten oder den neuen Stand,
// nie einen halben. Ein Absturz mitten im Schreiben kostet hoechstens die
// neue Aenderung, nie die bestehende Datei — und die ist von Hand gepflegt
// und nirgends sonst rekonstruierbar.
func (s *Store) Save(st State) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveLocked(st)
}

func (s *Store) saveLocked(st State) error {
	if st.Version == 0 {
		st.Version = Version
	}
	if st.Areas == nil {
		st.Areas = []string{}
	}
	if st.Zuordnung == nil {
		st.Zuordnung = map[string]string{}
	}
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n') // die Datei wird von Hand editiert, also mit Zeilenende

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".areas-*.json.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	// Ab hier raeumt jeder Fehlerpfad die temporaere Datei weg; nach einem
	// geglueckten Rename existiert tmpName nicht mehr und das Remove verpufft.
	defer func() { _ = os.Remove(tmpName) }()

	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	// Sync vor dem Rename: sonst kann der Verzeichniseintrag nach einem
	// Stromausfall auf eine leere Datei zeigen — formal atomar, inhaltlich
	// verloren.
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return err
	}
	return renameFile(tmpName, s.path)
}
