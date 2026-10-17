// Zusage gegen Diff je Change (dateien-abgleich, #53).
//
// Ein Change sagt in seiner proposal.md unter "Beruehrte Dateien" zu, welche
// Dateien er anfasst, und nennt in tasks.md seine Testdateien. Auf dieser
// Zusage ruht der Parallelbetrieb: zwei Changes laufen gleichzeitig, wenn
// ihre Dateimengen disjunkt sind. Hier wird sie gegen den Diff
// DEFAULT...change/KENNUNG gehalten — in drei Listen, wie ops.py --web und
// `just abgleich` (tools/pruefungen.mjs) es tun —, dazu die Capabilities des
// Changes und ob seine Kopie auf dem Branch von der auf dem Default-Branch
// abweicht. Die Detailspalte zeichnet das nur.
package appctx

import (
	"regexp"
	"sort"
	"strings"
)

// DateienAbgleich sind Zusage und Diff eines Changes in drei Listen, jede
// sortiert. Grund sagt, warum kein Diff vorliegt; dann sind die drei Listen
// leer und nur Zugesagt ist gefuellt.
type DateienAbgleich struct {
	Zugesagt      []string `json:"zugesagt"`
	Gedeckt       []string `json:"gedeckt"`
	OhneZusage    []string `json:"ohne_zusage"`
	OhneAenderung []string `json:"ohne_aenderung"`
	Grund         string   `json:"grund,omitempty"`
}

// ChangeAbgleich ist, was die Detailspalte ueber Capabilities, Kopien und
// Dateien eines Changes zeigt.
type ChangeAbgleich struct {
	Capabilities []string `json:"capabilities"`
	// Unveraendert: gelesen vom Default-Branch oder dort inhaltsgleich.
	Unveraendert bool `json:"unveraendert"`
	// AuchIn nennt die Branches, auf denen der Change mit anderem Inhalt
	// ebenfalls liegt.
	AuchIn  []string        `json:"auch_in"`
	Dateien DateienAbgleich `json:"dateien"`
}

// ChangeAbgleichEingabe ist alles, was der Abgleich braucht; gesammelt wird
// es im Server.
type ChangeAbgleichEingabe struct {
	Proposal, Tasks string
	Capabilities    []string
	// Standard ist der Default-Branch, Ref der Branch, von dem der Change
	// gelesen wurde.
	Standard, Ref string
	// Haupt und Ast sind die Dateien des Change-Ordners (relativer Pfad ->
	// Blob-sha) auf dem Default-Branch und auf Ref; nil = der Ordner fehlt.
	Haupt, Ast map[string]string
	// Angefasst ist die Dateiliste des Diffs Standard...Ref; DiffGrund sagt,
	// warum es keinen gibt.
	Angefasst []string
	DiffGrund string
}

var (
	abBeruehrt = regexp.MustCompile(`(?im)^##\s*Ber(?:ue|ü)hrte\s+Dateien\b`)
	abKopf     = regexp.MustCompile(`(?m)^##\s`)
	abPunkt    = regexp.MustCompile(`^\s*[-*]\s`)
	abFolge    = regexp.MustCompile(`^\s+\S`)
	abBacktick = regexp.MustCompile("`([^`\n]+)`")
	// Die drei Gestalten von Testdateien aus tools/pruefungen.mjs
	// (zugesagteTests).
	abTests = []*regexp.Regexp{
		regexp.MustCompile("`(tests/[^`]+\\.test\\.mjs)`"),
		regexp.MustCompile("`([\\w./-]+/test_[\\w.-]+\\.py)`"),
		regexp.MustCompile("`([\\w./-]+_test\\.go)`"),
	}
)

// ZugesagteDateien liest die Zusage: die Pfade in Backticks aus den
// Listeneintraegen unter "Beruehrte Dateien" (ein Eintrag reicht ueber seine
// eingerueckten Folgezeilen) und die Testdateien aus tasks.md. Was ein
// Leerzeichen traegt oder mit einem Strich beginnt, ist kein Pfad.
func ZugesagteDateien(proposal, tasks string) []string {
	menge := map[string]bool{}
	if loc := abBeruehrt.FindStringIndex(proposal); loc != nil {
		rest := proposal[loc[0]:]
		if bis := abKopf.FindStringIndex(rest[1:]); bis != nil {
			rest = rest[:bis[0]+1]
		}
		imEintrag := false
		for _, zeile := range strings.Split(rest, "\n") {
			if abPunkt.MatchString(zeile) {
				imEintrag = true
			} else if !abFolge.MatchString(zeile) {
				imEintrag = false
			}
			if !imEintrag {
				continue
			}
			for _, m := range abBacktick.FindAllStringSubmatch(zeile, -1) {
				if strings.ContainsAny(m[1], " \t") || strings.HasPrefix(m[1], "-") {
					continue
				}
				menge[m[1]] = true
			}
		}
	}
	for _, re := range abTests {
		for _, m := range re.FindAllStringSubmatch(tasks, -1) {
			menge[m[1]] = true
		}
	}
	return sortierteMenge(menge)
}

// DateienAbgleichen teilt Zusage und Diff in die drei Listen. Eine Zusage mit
// "/" am Ende deckt alles darunter; die Artefakte des Ablaufs
// (openspec/changes/, openspec/specs/) stehen nie unter ohne_zusage.
func DateienAbgleichen(zugesagt, angefasst []string) DateienAbgleich {
	zu, an := mengeAus(zugesagt), mengeAus(angefasst)
	d := DateienAbgleich{Zugesagt: zu, Gedeckt: []string{}, OhneZusage: []string{}, OhneAenderung: []string{}}
	for _, z := range zu {
		gedeckt := false
		for _, p := range an {
			if deckt(z, p) {
				gedeckt = true
				break
			}
		}
		if gedeckt {
			d.Gedeckt = append(d.Gedeckt, z)
		} else {
			d.OhneAenderung = append(d.OhneAenderung, z)
		}
	}
	for _, p := range an {
		if istAblaufartefakt(p) {
			continue
		}
		gedeckt := false
		for _, z := range zu {
			if deckt(z, p) {
				gedeckt = true
				break
			}
		}
		if !gedeckt {
			d.OhneZusage = append(d.OhneZusage, p)
		}
	}
	return d
}

// ChangeAbgleichBauen setzt Capabilities, Kopien und Dateien zusammen.
func ChangeAbgleichBauen(e ChangeAbgleichEingabe) ChangeAbgleich {
	a := ChangeAbgleich{Capabilities: mengeAus(e.Capabilities), AuchIn: []string{}}
	switch {
	case e.Ref == "" || e.Ref == e.Standard:
		a.Unveraendert = true
	case e.Haupt == nil:
		// Nur auf dem Branch: neu, also nicht unveraendert, aber auch keine
		// zweite Kopie.
	case gleicheDateien(e.Haupt, e.Ast):
		a.Unveraendert = true
	default:
		a.AuchIn = append(a.AuchIn, e.Standard)
	}
	zugesagt := ZugesagteDateien(e.Proposal, e.Tasks)
	if e.DiffGrund != "" {
		a.Dateien = DateienAbgleichen(zugesagt, nil)
		a.Dateien.OhneAenderung = []string{}
		a.Dateien.Grund = e.DiffGrund
		return a
	}
	a.Dateien = DateienAbgleichen(zugesagt, e.Angefasst)
	return a
}

func deckt(zusage, pfad string) bool {
	if strings.HasSuffix(zusage, "/") {
		return strings.HasPrefix(pfad, zusage)
	}
	return pfad == zusage
}

func istAblaufartefakt(p string) bool {
	return strings.HasPrefix(p, "openspec/changes/") || strings.HasPrefix(p, "openspec/specs/")
}

func gleicheDateien(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if w, ok := b[k]; !ok || w != v {
			return false
		}
	}
	return true
}

func mengeAus(liste []string) []string {
	m := map[string]bool{}
	for _, s := range liste {
		if s = strings.TrimSpace(s); s != "" {
			m[s] = true
		}
	}
	return sortierteMenge(m)
}

func sortierteMenge(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for s := range m {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
