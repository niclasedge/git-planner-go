// Die Change-Ordner eines Repos, das mit OpenSpec arbeitet (openspec-quelle, #10).
//
// Gelesen wird nur, geschrieben nie: die Ordner unter openspec/changes/ gehoeren
// den Werkzeugen unter tools/ (just neu, gate1, gate2, beleg, fertig, archiv).
// Diese Datei liest, was dort liegt — Issue-Verweis aus proposal.md, Kaestchen
// aus tasks.md, die Nachweise gate1.md/gate2.md/fertig.md und das Journal
// belege/belege.jsonl — und urteilt daraus ueber den Stand. Den Baum aus Issue
// und Change baut internal/appctx/openspecquelle.go.
package collectors

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Der Stand eines Changes. Die Werte sind die Woerter, mit denen `just lage`
// dieselben Zustaende nennt.
const (
	OpenspecFrei     = "frei"
	OpenspecLaeuft   = "laeuft"
	OpenspecFertig   = "fertig"
	OpenspecImPR     = "im PR"
	OpenspecErledigt = "erledigt"
)

// OpenspecAufgabe ist ein Kaestchen aus tasks.md.
type OpenspecAufgabe struct {
	Text     string `json:"text"`
	Erledigt bool   `json:"erledigt"`
}

// OpenspecBeleg ist eine Zeile aus belege/belege.jsonl, wie tools/beleg.mjs
// sie schreibt. Pfad ist relativ zum Change-Ordner.
type OpenspecBeleg struct {
	Zeit  string `json:"zeit"`
	Stufe string `json:"stufe"`
	Art   string `json:"art"`
	Pfad  string `json:"pfad"`
	Fehlt bool   `json:"fehlt"`
	Grund string `json:"grund,omitempty"`
	// Bild ist die URL, unter der die Oberflaeche das Bild laden kann. Die
	// Leseregeln kennen sie nicht; der Aufrufer, der weiss, woher der Change
	// kam, setzt sie (repo-leiste, #25).
	Bild string `json:"bild,omitempty"`
	// SHA ist der Blob-SHA der Datei, wenn der Aufrufer ihn kennt
	// (beleg-bilder, #105: Deduplizieren gegen geaenderte Bilder).
	SHA string `json:"sha,omitempty"`
}

// OpenspecChange ist ein Change-Ordner.
type OpenspecChange struct {
	Kennung string `json:"kennung"`
	// Ordner ist der absolute Pfad des Change-Ordners.
	Ordner     string `json:"ordner"`
	Archiviert bool   `json:"archiviert"`
	// Issue ist die Nummer aus der Herkunftszeile "Geht zurueck auf
	// GitHub-Issue #N" am Zeilenanfang der proposal.md,
	// 0 = keine solche Zeile.
	Issue    int               `json:"issue,omitempty"`
	Aufgaben []OpenspecAufgabe `json:"aufgaben"`
	// Gates sind die vorhandenen, nichtleeren Nachweise in der Folge
	// gate1, gate2, fertig.
	Gates  []string        `json:"gates"`
	Belege []OpenspecBeleg `json:"belege,omitempty"`
	// RemoteBranch: es gibt eine Ref refs/remotes/*/change/KENNUNG.
	RemoteBranch bool `json:"remote_branch"`
	// Ref nennt den Branch, von dem der Change gelesen wurde, wenn er nicht
	// aus dem Dateisystem kommt (repo-leiste, #25). Leer beim lokalen Lesen.
	Ref string `json:"ref,omitempty"`
}

// OpenspecLeser ist das, was die Leseregeln von ihrer Quelle brauchen — ein
// Dateisystem oder ein GitHub-Baum (repo-leiste, #25). Pfade sind relativ zur
// Repo-Wurzel und mit Schraegstrich getrennt.
type OpenspecLeser interface {
	// Lesen liefert den Inhalt einer Datei.
	Lesen(pfad string) ([]byte, error)
	// Groesse liefert die Groesse einer Datei, -1 wenn es sie nicht gibt.
	Groesse(pfad string) int64
	// Ordner liefert die Namen der direkten Unterordner, sortiert.
	Ordner(pfad string) []string
}

// dateiLeser liest aus einem Verzeichnis auf der Platte.
type dateiLeser string

func (d dateiLeser) voll(p string) string { return filepath.Join(string(d), filepath.FromSlash(p)) }

func (d dateiLeser) Lesen(p string) ([]byte, error) { return os.ReadFile(d.voll(p)) }

func (d dateiLeser) Groesse(p string) int64 {
	st, err := os.Stat(d.voll(p))
	if err != nil || !st.Mode().IsRegular() {
		return -1
	}
	return st.Size()
}

func (d dateiLeser) Ordner(p string) []string { return osUnterordner(d.voll(p)) }

// Erledigt zaehlt die abgehakten Aufgaben.
func (c OpenspecChange) Erledigt() int {
	n := 0
	for _, a := range c.Aufgaben {
		if a.Erledigt {
			n++
		}
	}
	return n
}

// Status urteilt ueber den Stand: archiviert ist erledigt; alle Aufgaben
// abgehakt ist "im PR" mit Remote-Branch, sonst "fertig"; mindestens eine
// abgehakt ist "laeuft"; sonst "frei". Ein Change ohne jede Aufgabe ist frei —
// eine leere Liste ist kein Beweis, dass etwas getan wurde.
func (c OpenspecChange) Status() string {
	erledigt := c.Erledigt()
	switch {
	case c.Archiviert:
		return OpenspecErledigt
	case len(c.Aufgaben) > 0 && erledigt == len(c.Aufgaben):
		if c.RemoteBranch {
			return OpenspecImPR
		}
		return OpenspecFertig
	case erledigt > 0:
		return OpenspecLaeuft
	}
	return OpenspecFrei
}

// HatOpenspec sagt, ob die Projektwurzel einen Ordner openspec/ hat.
func HatOpenspec(projekt string) bool {
	st, err := os.Stat(filepath.Join(projekt, "openspec"))
	return err == nil && st.IsDir()
}

var (
	// osIssueRe liest nur die Herkunftszeile, die just neu schreibt — ein
	// Verweis im Fliesstext ordnet nichts zu (karte-herkunft-streng, #103).
	osIssueRe    = regexp.MustCompile(`(?m)^Geht zurueck auf GitHub-Issue #(\d+)\b`)
	osKaestchen  = regexp.MustCompile(`^\s*[-*] \[([ xX])\]\s+(.*\S)\s*$`)
	osArchivName = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}-(.+)$`)
	osGates      = []string{"gate1", "gate2", "fertig"}
)

// OpenspecChanges liest alle Changes des Projekts: erst die aktiven unter
// openspec/changes/, dann die archivierten unter openspec/changes/archive/,
// je nach Kennung sortiert. Ohne openspec/ ist die Liste leer. Ein
// unlesbarer Ordner faellt weg, statt die anderen mitzunehmen.
func OpenspecChanges(projekt string) []OpenspecChange {
	if !HatOpenspec(projekt) {
		return nil
	}
	cs := OpenspecChangesAus(dateiLeser(projekt), osRemoteChangeBranches(projekt))
	// Lokal ist Ordner der absolute Pfad — so wie vor dem Leser.
	for i := range cs {
		cs[i].Ordner = filepath.Join(projekt, filepath.FromSlash(cs[i].Ordner))
	}
	return cs
}

// OpenspecChangesAus liest die Changes aus einem beliebigen Leser nach
// denselben Regeln wie OpenspecChanges. remote nennt die Kennungen mit
// Remote-Branch change/KENNUNG. Ordner ist relativ zur Repo-Wurzel.
func OpenspecChangesAus(l OpenspecLeser, remote map[string]bool) []OpenspecChange {
	basis := "openspec/changes"
	var aktiv, archiv []OpenspecChange
	for _, name := range l.Ordner(basis) {
		if name == "archive" {
			continue
		}
		c := osChangeLesen(l, basis+"/"+name, name, false)
		c.RemoteBranch = remote[name]
		aktiv = append(aktiv, c)
	}
	for _, name := range l.Ordner(basis + "/archive") {
		kennung := name
		if m := osArchivName.FindStringSubmatch(name); m != nil {
			kennung = m[1]
		}
		archiv = append(archiv, osChangeLesen(l, basis+"/archive/"+name, kennung, true))
	}
	return append(aktiv, archiv...)
}

func osUnterordner(dir string) []string {
	eintraege, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range eintraege {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}

func osChangeLesen(l OpenspecLeser, ordner, kennung string, archiviert bool) OpenspecChange {
	c := OpenspecChange{Kennung: kennung, Ordner: ordner, Archiviert: archiviert,
		Aufgaben: []OpenspecAufgabe{}, Gates: []string{}}
	if b, err := l.Lesen(ordner + "/proposal.md"); err == nil {
		if m := osIssueRe.FindSubmatch(b); m != nil {
			c.Issue, _ = strconv.Atoi(string(m[1]))
		}
	}
	if b, err := l.Lesen(ordner + "/tasks.md"); err == nil {
		c.Aufgaben = OpenspecAufgabenAus(string(b))
	}
	for _, g := range osGates {
		if l.Groesse(ordner+"/"+g+".md") > 0 {
			c.Gates = append(c.Gates, g)
		}
	}
	if b, err := l.Lesen(ordner + "/belege/belege.jsonl"); err == nil {
		c.Belege = OpenspecBelegeAus(b)
	}
	return c
}

// OpenspecAufgabenAus liest die Kaestchen einer tasks.md.
func OpenspecAufgabenAus(text string) []OpenspecAufgabe {
	out := []OpenspecAufgabe{}
	for _, zeile := range strings.Split(text, "\n") {
		if m := osKaestchen.FindStringSubmatch(zeile); m != nil {
			out = append(out, OpenspecAufgabe{Text: m[2], Erledigt: m[1] != " "})
		}
	}
	return out
}

// OpenspecBelegeAus liest das Journal belege.jsonl. Leere und unlesbare Zeilen
// fallen weg: ein kaputter Eintrag soll die uebrigen nicht verdecken.
func OpenspecBelegeAus(inhalt []byte) []OpenspecBeleg {
	var out []OpenspecBeleg
	sc := bufio.NewScanner(bytes.NewReader(inhalt))
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		zeile := strings.TrimSpace(sc.Text())
		if zeile == "" {
			continue
		}
		var b OpenspecBeleg
		if json.Unmarshal([]byte(zeile), &b) != nil {
			continue
		}
		out = append(out, b)
	}
	return out
}

// osRemoteChangeBranches sammelt die Kennungen, fuer die es eine Ref
// refs/remotes/REMOTE/change/KENNUNG gibt. Ohne Git ist die Menge leer.
func osRemoteChangeBranches(projekt string) map[string]bool {
	out := map[string]bool{}
	for _, ref := range gitLines(projekt, "for-each-ref", "--format=%(refname)", "refs/remotes/") {
		teile := strings.SplitN(strings.TrimPrefix(ref, "refs/remotes/"), "/", 3)
		if len(teile) == 3 && teile[1] == "change" && teile[2] != "" {
			out[teile[2]] = true
		}
	}
	return out
}
