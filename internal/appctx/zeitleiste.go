// Zeitleiste der abgehakten Aufgaben eines Changes (aufgaben-zeitleiste, #55).
//
// Wie `git_completions` in ops.py: eine Aufgabe gilt als abgehakt, wenn im
// selben Hunk eines Commits auf tasks.md `- [ ] X` verschwindet und
// `- [x] X` erscheint. Der Schluessel ist die Nummer der Aufgabe (`2.3`),
// ohne Nummer ihr Text. Ein Anlegen der Datei mit fertigen Kaestchen ist kein
// Abhaken. Wird eine Aufgabe spaeter wieder geoeffnet, faellt sie heraus;
// wird sie erneut abgehakt, gilt das letzte Mal.
//
// Die Patches kommen ueber die REST-API (internal/server/zeitleiste.go);
// ausgewertet und sortiert wird hier, ohne Netz und ohne Browser.
package appctx

import (
	"regexp"
	"sort"
	"strings"
	"time"
)

// ZeitleistenCommit ist ein Commit auf tasks.md mit dem Patch dieser Datei.
type ZeitleistenCommit struct {
	SHA     string
	Zeit    string // RFC 3339
	Betreff string
	Patch   string
}

// AufgabeAbgehakt ist ein Eintrag der Zeitleiste.
type AufgabeAbgehakt struct {
	Aufgabe string `json:"aufgabe"`
	Nummer  string `json:"nummer"`
	Zeit    string `json:"zeit"`
	SHA     string `json:"sha"`
	KurzSHA string `json:"kurz_sha"`
	Betreff string `json:"betreff"`
}

// AufgabenZeitleiste ist die Antwort von GET /api/zeitleiste.
type AufgabenZeitleiste struct {
	Repo    string `json:"repo"`
	Kennung string `json:"kennung"`
	Ref     string `json:"ref"`
	// Commits ist die Zahl der ausgewerteten Commits.
	Commits   int               `json:"commits"`
	Eintraege []AufgabeAbgehakt `json:"eintraege"`
	Grund     string            `json:"grund,omitempty"`
}

var (
	zlKaestchenRe = regexp.MustCompile(`^\s*[-*]\s*\[([ xX])\]\s*(.*)$`)
	zlNummerRe    = regexp.MustCompile(`^(\d+(?:\.\d+)*)[.)]?\s+`)
)

type zlZeile struct {
	schluessel, nummer, text string
}

// zlKaestchen liest eine Zeile ohne Diff-Zeichen; ok ist false, wenn sie
// kein Kaestchen ist.
func zlKaestchen(zeile string) (z zlZeile, erledigt, ok bool) {
	m := zlKaestchenRe.FindStringSubmatch(zeile)
	if m == nil {
		return z, false, false
	}
	z.text = strings.Join(strings.Fields(m[2]), " ")
	if n := zlNummerRe.FindStringSubmatch(z.text); n != nil {
		z.nummer = n[1]
		z.schluessel = "n:" + n[1]
	} else {
		z.schluessel = "t:" + z.text
	}
	return z, m[1] != " ", true
}

// zlHunk sammelt die Kaestchen eines Hunks.
type zlHunk struct {
	wegOffen, wegFertig map[string]bool
	neuFertig           []zlZeile
	neuOffen            map[string]bool
}

func neuerZlHunk() zlHunk {
	return zlHunk{wegOffen: map[string]bool{}, wegFertig: map[string]bool{}, neuOffen: map[string]bool{}}
}

// AufgabenZeitleisteBauen wertet die Commits aus, in der Folge der API
// (neueste zuerst; entscheidend ist aber die Zeit), und liefert je abgehakter
// Aufgabe einen Eintrag, neueste zuerst; im selben Commit die weiter unten
// stehende Aufgabe zuerst. Nie nil.
func AufgabenZeitleisteBauen(commits []ZeitleistenCommit) []AufgabeAbgehakt {
	folge := make([]ZeitleistenCommit, len(commits))
	for i, c := range commits {
		folge[len(commits)-1-i] = c
	}
	zeit := func(s string) time.Time {
		t, _ := time.Parse(time.RFC3339, strings.TrimSpace(s))
		return t
	}
	sort.SliceStable(folge, func(i, j int) bool { return zeit(folge[i].Zeit).Before(zeit(folge[j].Zeit)) })

	type stand struct {
		e   AufgabeAbgehakt
		seq int
	}
	aktuell := map[string]stand{}
	seq := 0
	for _, c := range folge {
		abschliessen := func(h zlHunk) {
			for k := range h.wegFertig {
				if h.neuOffen[k] {
					delete(aktuell, k)
				}
			}
			for _, z := range h.neuFertig {
				if !h.wegOffen[z.schluessel] {
					continue
				}
				seq++
				kurz := c.SHA
				if len(kurz) > 7 {
					kurz = kurz[:7]
				}
				aktuell[z.schluessel] = stand{seq: seq, e: AufgabeAbgehakt{Aufgabe: z.text, Nummer: z.nummer,
					Zeit: c.Zeit, SHA: c.SHA, KurzSHA: kurz, Betreff: c.Betreff}}
			}
		}
		h := neuerZlHunk()
		for _, zeile := range strings.Split(c.Patch, "\n") {
			zeile = strings.TrimSuffix(zeile, "\r")
			if strings.HasPrefix(zeile, "@@") {
				abschliessen(h)
				h = neuerZlHunk()
				continue
			}
			if zeile == "" || (zeile[0] != '+' && zeile[0] != '-') {
				continue
			}
			z, erledigt, ok := zlKaestchen(zeile[1:])
			if !ok {
				continue
			}
			switch {
			case zeile[0] == '-' && !erledigt:
				h.wegOffen[z.schluessel] = true
			case zeile[0] == '-':
				h.wegFertig[z.schluessel] = true
			case erledigt:
				h.neuFertig = append(h.neuFertig, z)
			default:
				h.neuOffen[z.schluessel] = true
			}
		}
		abschliessen(h)
	}

	liste := make([]stand, 0, len(aktuell))
	for _, s := range aktuell {
		liste = append(liste, s)
	}
	sort.Slice(liste, func(i, j int) bool { return liste[i].seq > liste[j].seq })
	out := make([]AufgabeAbgehakt, 0, len(liste))
	for _, s := range liste {
		out = append(out, s.e)
	}
	return out
}
