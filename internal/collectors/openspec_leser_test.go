package collectors

// Dieselben Leseregeln fuer Changes aus einem beliebigen Leser (repo-leiste,
// #25): der GitHub-Baum liefert Dateien statt eines Dateisystems.

import (
	"errors"
	"path"
	"sort"
	"strings"
	"testing"
)

// mapLeser ist ein Leser ueber eine Pfad-Inhalt-Tabelle, Pfade relativ zur
// Repo-Wurzel mit Schraegstrich.
type mapLeser map[string]string

func (m mapLeser) Lesen(p string) ([]byte, error) {
	if s, ok := m[p]; ok {
		return []byte(s), nil
	}
	return nil, errors.New("fehlt: " + p)
}

func (m mapLeser) Groesse(p string) int64 {
	if s, ok := m[p]; ok {
		return int64(len(s))
	}
	return -1
}

func (m mapLeser) Ordner(dir string) []string {
	gesehen := map[string]bool{}
	for p := range m {
		if !strings.HasPrefix(p, dir+"/") {
			continue
		}
		rest := strings.TrimPrefix(p, dir+"/")
		if i := strings.Index(rest, "/"); i > 0 {
			gesehen[rest[:i]] = true
		}
	}
	out := []string{}
	for n := range gesehen {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

func TestOpenspecChangesAusLeser(t *testing.T) {
	l := mapLeser{
		"openspec/changes/bar/proposal.md":                    "# bar\n\nGeht zurueck auf GitHub-Issue #4.\n",
		"openspec/changes/bar/tasks.md":                       "- [x] eins\n- [ ] zwei\n",
		"openspec/changes/bar/gate1.md":                       "bestanden\n",
		"openspec/changes/bar/gate2.md":                       "",
		"openspec/changes/bar/belege/belege.jsonl":            `{"zeit":"z","stufe":"nachher","art":"bild","pfad":"belege/assets/0-bar.png"}` + "\n",
		"openspec/changes/archive/2026-09-01-alt/proposal.md": "Geht zurueck auf GitHub-Issue #2.\n",
		"openspec/changes/archive/2026-09-01-alt/tasks.md":    "- [x] fertig\n",
	}
	cs := OpenspecChangesAus(l, map[string]bool{"bar": true})
	if len(cs) != 2 {
		t.Fatalf("%d Changes, erwartet 2: %+v", len(cs), cs)
	}
	bar, alt := cs[0], cs[1]
	if bar.Kennung != "bar" || bar.Issue != 4 || bar.Erledigt() != 1 || len(bar.Aufgaben) != 2 {
		t.Errorf("bar %+v", bar)
	}
	if strings.Join(bar.Gates, ",") != "gate1" {
		t.Errorf("Gates %v, erwartet nur gate1 (gate2.md ist leer)", bar.Gates)
	}
	if len(bar.Belege) != 1 || bar.Belege[0].Pfad != "belege/assets/0-bar.png" {
		t.Errorf("Belege %+v", bar.Belege)
	}
	if !bar.RemoteBranch || bar.Status() != OpenspecLaeuft {
		t.Errorf("bar RemoteBranch=%v Status=%s", bar.RemoteBranch, bar.Status())
	}
	if bar.Ordner != path.Join("openspec", "changes", "bar") {
		t.Errorf("Ordner %q, erwartet den Pfad relativ zur Repo-Wurzel", bar.Ordner)
	}
	if alt.Kennung != "alt" || !alt.Archiviert || alt.Issue != 2 || alt.Status() != OpenspecErledigt {
		t.Errorf("alt %+v", alt)
	}
}

func TestOpenspecChangesAusLeerMitOhneOrdner(t *testing.T) {
	if cs := OpenspecChangesAus(mapLeser{"README.md": "x"}, nil); len(cs) != 0 {
		t.Errorf("ohne openspec/ %d Changes", len(cs))
	}
}
