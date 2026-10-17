// depedges.go liefert den Abhaengigkeitsgraphen des Trackers aus dem passiven
// Export .beads/issues.jsonl, NICHT ueber `bd`.
//
// Grund, gemessen am 2026-09-09 in ~/git/acme-platform: `bd dep list` fuer
// 59 Epic-Ids braucht 8,08 s, fuer 494 Ids 46,1 s -- bd verarbeitet die Ids
// einzeln, der Preis ist also linear in der Anzahl der Ids. Das Lesen+Parsen
// derselben Daten aus .beads/issues.jsonl (495 Zeilen) braucht 14 ms. Der
// Export ist bds eigener passiver Export (export.auto in .beads/config.yaml,
// siehe CLAUDE.md) -- diese Datei liest ihn nur, sie schreibt nie hinein und
// ruft nie `bd` auf.
package collectors

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// DepEdge ist eine blocks-Kante: Blocked wartet auf Blocker.
//
// Die Feldnamen sind bewusst blocked/blocker statt from/to gewaehlt: die
// Kantenrichtung ist der Fehler, der in diesem Repo schon einmal alle zehn
// Kanten verdreht hat, weil bd dieselbe Kante ueber drei Kommandos hinweg auf
// entgegengesetzte Weisen benennt (siehe CLAUDE.md, Abschnitt 4).
type DepEdge struct {
	Blocked string `json:"blocked"`
	Blocker string `json:"blocker"`
}

// TrackerGraph ist der komplette Abhaengigkeitsgraph eines Repos, aus
// .beads/issues.jsonl gelesen. Faellt eigenstaendig aus wie der Rest dieses
// Pakets: eine fehlende oder kaputte Datei ergibt Available=false bzw. eine
// Note, nie einen Fehler nach aussen.
type TrackerGraph struct {
	Available bool
	Note      string            // warum leer, wenn nicht verfuegbar
	Edges     []DepEdge         // nur type == "blocks"
	Parent    map[string]string // bead -> Eltern-Id (aus type == "parent-child")
	Type      map[string]string // bead -> issue_type (task|epic|decision|...)
	Status    map[string]string // bead -> status
	Title     map[string]string // bead -> title
	// Stale ist nur eine UNTERE Schranke: sie erkennt, wenn die Datei-mtime
	// AELTER ist als der juengste updated_at, den der Export selbst enthaelt
	// -- also ein Export, der nachweislich hinterherhinkt. Ein Schreibvorgang,
	// der die Datei gar nicht angefasst hat (z.B. weil export.auto zwischendurch
	// aus war), ist damit NICHT erkennbar; Stale=false ist also kein Beweis,
	// dass der Export aktuell ist, nur die Abwesenheit des einen Befundes, den
	// dieser billige Test ueberhaupt sehen kann.
	Stale bool
}

// issueDependencyJSON ist eine Kante aus dem "dependencies"-Array einer Zeile
// in issues.jsonl. Verifizierte Richtung: bei
// {"issue_id":"acme-platform-tf4","depends_on_id":"acme-platform-5nn","type":"parent-child"}
// ist 5nn das Epic, also Parent[issue_id] = depends_on_id. Bei type=="blocks"
// wartet issue_id auf depends_on_id, also Blocked=issue_id, Blocker=depends_on_id.
type issueDependencyJSON struct {
	IssueID     string `json:"issue_id"`
	DependsOnID string `json:"depends_on_id"`
	Type        string `json:"type"`
}

// issueLineJSON ist eine Zeile aus issues.jsonl, reduziert auf die Felder, die
// dieser Graph braucht.
type issueLineJSON struct {
	ID           string                `json:"id"`
	Title        string                `json:"title"`
	Status       string                `json:"status"`
	IssueType    string                `json:"issue_type"`
	UpdatedAt    string                `json:"updated_at"`
	Dependencies []issueDependencyJSON `json:"dependencies"`
}

// maxLineBytes kappt eine einzelne Zeile gegen Speicherfraß. bufio.Scanner
// braucht dafuer einen explizit vergroesserten Buffer (scanner.Buffer unten) --
// ohne den bricht er bei einer Zeile ueber dem Default (64 KiB) mit
// bufio.ErrTooLong ab, und zwar fuer den GANZEN Scan, nicht nur fuer die eine
// Zeile: alles danach in der Datei waere stillschweigend verloren. Das ist ein
// echter Fall hier, die Beschreibungen in diesem Tracker werden sehr lang.
// scanLinesCapped unten faengt selbst den Fall ab, dass eine Zeile trotz des
// vergroesserten Buffers noch immer laenger als maxLineBytes ist: sie wird an
// dieser Grenze geschnitten statt weiter geladen, das Fragment ist kein
// gueltiges JSON mehr und faellt beim Parsen als kaputte Zeile durch -- der
// Rest der Datei bleibt lesbar.
const maxLineBytes = 1 << 20 // 1 MiB

// scanLinesCapped ist bufio.ScanLines, ergaenzt um die Kappung aus
// maxLineBytes: findet sich innerhalb der Kappung keine Newline, wird trotzdem
// ein Token zurueckgegeben (das Fragment bis zur Kappung), statt den Scanner
// weiter wachsen zu lassen, bis er mit ErrTooLong abbricht.
func scanLinesCapped(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if i := bytes.IndexByte(data, '\n'); i >= 0 {
		return i + 1, dropCR(data[0:i]), nil
	}
	if atEOF {
		if len(data) == 0 {
			return 0, nil, nil
		}
		return len(data), dropCR(data), nil
	}
	if len(data) >= maxLineBytes {
		return len(data), data, nil
	}
	return 0, nil, nil
}

// dropCR entfernt ein trailendes \r (CRLF-Dateien), spiegelt bufio.ScanLines'
// eigene (unexportierte) Hilfsfunktion gleichen Zwecks.
func dropCR(data []byte) []byte {
	if len(data) > 0 && data[len(data)-1] == '\r' {
		return data[0 : len(data)-1]
	}
	return data
}

// emptyGraph liefert ein TrackerGraph mit initialisierten (nie nil) Feldern,
// Grundlage sowohl fuer den Fail-open-Fall als auch fuer den Start des
// erfolgreichen Parse-Durchlaufs.
func emptyGraph(available bool, note string) TrackerGraph {
	return TrackerGraph{
		Available: available,
		Note:      note,
		Edges:     []DepEdge{},
		Parent:    map[string]string{},
		Type:      map[string]string{},
		Status:    map[string]string{},
		Title:     map[string]string{},
	}
}

// missingExportNote erklaert, warum Available=false ist und was dagegen zu tun
// ist -- der Export ist bds eigene Aufgabe, kein Bug in diesem Paket.
const missingExportNote = ".beads/issues.jsonl fehlt oder ist nicht lesbar -- der Export wird ueber export.auto in .beads/config.yaml eingeschaltet"

// parseIssuesJSONL liest issues.jsonl zeilenweise aus r. brokenLines zaehlt
// jede Zeile, die weder leer noch gueltiges JSON mit einer id war (inkl. eines
// durch scanLinesCapped geschnittenen Überlangen Fragments). newestUpdated ist
// der juengste erfolgreich geparste updated_at-Stempel, Grundlage fuer Stale.
func parseIssuesJSONL(r *os.File) (graph TrackerGraph, brokenLines int, newestUpdated time.Time) {
	graph = emptyGraph(true, "")

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), maxLineBytes)
	scanner.Split(scanLinesCapped)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var issue issueLineJSON
		if err := json.Unmarshal([]byte(line), &issue); err != nil || issue.ID == "" {
			brokenLines++
			continue
		}
		graph.Type[issue.ID] = issue.IssueType
		graph.Status[issue.ID] = issue.Status
		graph.Title[issue.ID] = issue.Title
		if t, err := time.Parse(time.RFC3339, issue.UpdatedAt); err == nil && t.After(newestUpdated) {
			newestUpdated = t
		}
		for _, dep := range issue.Dependencies {
			switch dep.Type {
			case "blocks":
				graph.Edges = append(graph.Edges, DepEdge{Blocked: dep.IssueID, Blocker: dep.DependsOnID})
			case "parent-child":
				graph.Parent[dep.IssueID] = dep.DependsOnID
				// "related" und "discovered-from" (und alles sonst) werden
				// bewusst verworfen -- nur diese beiden Typen tragen fuer
				// diesen Graphen eine Bedeutung.
			}
		}
	}
	return graph, brokenLines, newestUpdated
}

// loadTrackerGraph oeffnet und parst .beads/issues.jsonl. info/statErr kommen
// vom Aufrufer, der ohnehin schon stat'en musste, um die Cache-Gueltigkeit zu
// pruefen -- ein zweites os.Stat waere redundant.
func loadTrackerGraph(path string, info os.FileInfo, statErr error) TrackerGraph {
	if statErr != nil {
		return emptyGraph(false, missingExportNote)
	}
	f, err := os.Open(path)
	if err != nil {
		return emptyGraph(false, missingExportNote)
	}
	defer f.Close()

	graph, broken, newestUpdated := parseIssuesJSONL(f)
	if broken > 0 {
		graph.Note = fmt.Sprintf("%d kaputte Zeile(n) in .beads/issues.jsonl uebersprungen", broken)
	}
	if !newestUpdated.IsZero() && info.ModTime().Before(newestUpdated) {
		graph.Stale = true
	}
	return graph
}

// trackerGraphTTL ist die harte Obergrenze, unabhaengig von mtime/size: selbst
// wenn die Datei unveraendert bleibt, wird nach dieser Zeit neu gestat't.
// Der Grund traegt: ein Cache-Eintrag darf nicht beliebig lange leben, nur
// weil niemand etwas geaendert hat, denn nichts in diesem Prozess erfaehrt
// sonst von einem Schreibzugriff ausserhalb (vgl. gkp-ar7.19).
//
// Die ZAHL traegt nicht. Der Satz "gleiche Groessenordnung wie planSVGCache"
// stand hier und war falsch — planSVGTTL sind 30 Minuten, also das
// Fuenfzehnfache (Befund aus gkp-mnd). Die beiden Werte haben verschiedene
// Gruende und duerfen verschieden sein: hier kostet ein Ablauf einen stat und
// im schlechtesten Fall ein Neulesen der JSONL (Millisekunden), dort einen
// `bd graph`-Aufruf (2,0-5,3 s, gemessen in shared.go). Genau deshalb ist es
// richtig, dass diese Zahl die kleinere ist — nur nicht aus dem Grund, der
// hier stand.
const trackerGraphTTL = 2 * time.Minute

type trackerGraphEntry struct {
	at       time.Time
	haveStat bool // ob mtime/size beim letzten Aufruf ueberhaupt bekannt waren
	mtime    time.Time
	size     int64
	graph    TrackerGraph
}

// trackerGraphCache ist keyed nach Repo-Root, wie planSVGCache.
var trackerGraphCache sync.Map

// TrackerGraphCached liest den Abhaengigkeitsgraphen aus .beads/issues.jsonl,
// gecacht pro Repo-Root. Ein Eintrag verfaellt, wenn sich mtime ODER Groesse
// der Datei gegenueber dem letzten Aufruf geaendert haben, oder nach
// trackerGraphTTL, je nachdem was zuerst eintritt.
//
// Verschwindet die Datei zwischen zwei Aufrufen (statt sich zu aendern), ist
// das KEIN "mtime/size geaendert" im Sinn dieser Regel -- es gibt schlicht
// keine neuen Werte zum Vergleichen. Der zuletzt gute Stand bleibt daher
// innerhalb der TTL gueltig, statt sofort auf "nicht verfuegbar" umzuschalten;
// das vermeidet ein Flackern durch einen kurzzeitig abwesenden Export (z.B.
// waehrend bd ihn gerade neu schreibt).
func TrackerGraphCached(cwd string) TrackerGraph {
	repo := RepoRoot(cwd)
	path := filepath.Join(repo, ".beads", "issues.jsonl")

	info, statErr := os.Stat(path)
	haveStat := statErr == nil
	var mtime time.Time
	var size int64
	if haveStat {
		mtime = info.ModTime()
		size = info.Size()
	}

	if v, ok := trackerGraphCache.Load(repo); ok {
		e := v.(trackerGraphEntry)
		if time.Since(e.at) < trackerGraphTTL {
			switch {
			case haveStat && e.haveStat && mtime.Equal(e.mtime) && size == e.size:
				return e.graph
			case !haveStat:
				// Datei jetzt nicht lesbar/vorhanden -- ob sie beim letzten Mal
				// da war oder nicht, es gibt kein neues mtime/size-Signal, also
				// bleibt der Cache-Eintrag stehen.
				return e.graph
			}
		}
	}

	graph := loadTrackerGraph(path, info, statErr)
	trackerGraphCache.Store(repo, trackerGraphEntry{
		at: time.Now(), haveStat: haveStat, mtime: mtime, size: size, graph: graph,
	})
	return graph
}
