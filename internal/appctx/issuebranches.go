package appctx

// Verknuepfte Branches je Issue (issue-branches, #50): welcher Branch des
// Repos an welchem offenen Issue haengt, wie sein letzter Commit aussieht,
// wie weit er gegen den Default-Branch steht, und ob an ihm gerade
// gearbeitet wird. Die Zuordnung und das Flag entstehen HIER; die
// Oberflaeche zeichnet nur.

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/niclasedge/git-planner-go/internal/collectors"
	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

// BranchAktivFenster ist das Fenster, in dem ein letzter Commit einen Branch
// aktiv macht, wenn ArbeitEingabe.AktivFenster leer ist.
const BranchAktivFenster = 3 * 24 * time.Hour

// Gruende einer Verknuepfung.
const (
	BranchGrundChange = "change"
	BranchGrundName   = "name"
	BranchGrundPR     = "pr"
)

// BranchKopf ist, was der Dienst je verknuepftem Branch geholt hat: der
// letzte Commit und der Vergleich gegen den Default-Branch.
type BranchKopf struct {
	Zeit    string // RFC 3339; leer heisst unbekannt
	Betreff string
	Voraus  int
	Zurueck int
}

// BranchPR ist der offene PR mit diesem Branch als Head.
type BranchPR struct {
	Nummer int    `json:"nummer"`
	Titel  string `json:"titel"`
	URL    string `json:"url,omitempty"`
	Draft  bool   `json:"draft"`
	// Zustand und KonfliktDateien wie am PR (konflikte-anzeigen, #62).
	Zustand         string   `json:"zustand,omitempty"`
	KonfliktDateien []string `json:"konflikt_dateien,omitempty"`
	// Checks wie am PR (pr-checks, #80).
	Checks *ghrest.PRChecks `json:"checks,omitempty"`
}

// IssueBranch ist ein Branch, der an einem Issue haengt.
type IssueBranch struct {
	Name    string    `json:"name"`
	SHA     string    `json:"sha"`
	KurzSHA string    `json:"kurz_sha"`
	Zeit    string    `json:"zeit,omitempty"`
	Betreff string    `json:"betreff,omitempty"`
	Voraus  int       `json:"voraus"`
	Zurueck int       `json:"zurueck"`
	PR      *BranchPR `json:"pr,omitempty"`
	Aktiv   bool      `json:"aktiv"`
	// Gruende nennt, warum der Branch am Issue haengt: change, name, pr.
	Gruende []string `json:"gruende"`
}

// schliesstRe findet "closes #N" und seine Formen im Text eines PRs.
var schliesstRe = regexp.MustCompile(`(?i)\b(?:close[sd]?|fix(?:e[sd])?|resolve[sd]?)\s*:?\s+#(\d+)\b`)

// titelRe findet "(#N)" im Titel eines PRs.
var titelRe = regexp.MustCompile(`\(#(\d+)\)`)

// nameVorneRe: die Nummer am Anfang eines Pfadteils ("42-titel",
// "claude/42-titel", "issue/42"). nameHintenRe: am Ende nach - oder _
// ("fix-42", "issue_42").
var (
	nameVorneRe  = regexp.MustCompile(`(?:^|/)(\d+)(?:[-_/]|$)`)
	nameHintenRe = regexp.MustCompile(`[-_](\d+)$`)
)

func nummernAus(re *regexp.Regexp, s string) []int {
	out := []int{}
	for _, m := range re.FindAllStringSubmatch(s, -1) {
		if n, err := strconv.Atoi(m[1]); err == nil && n > 0 {
			out = append(out, n)
		}
	}
	return out
}

// istStandard sagt, ob ein Branch der Default-Branch ist; der haengt an
// keinem Issue.
func istStandard(name, standard string) bool {
	return name == "main" || name == "master" || (standard != "" && name == standard)
}

// offeneIssues sind die offenen Issues ohne PRs.
func offeneIssues(e ArbeitEingabe) []ghrest.Issue {
	var out []ghrest.Issue
	for _, i := range e.Offen {
		if !i.IstPR {
			out = append(out, i)
		}
	}
	return out
}

// changesJeIssue ordnet die Changes den Issues zu: der aktive vor dem
// archivierten. Ein Change ohne Verweis wird ueber den Titel "KENNUNG: …"
// eines offenen Issues zugeordnet — dieselbe Regel wie in der
// openspec-Quelle.
func changesJeIssue(offen []ghrest.Issue, changes []collectors.OpenspecChange) (aktivJe, archivJe map[int]*collectors.OpenspecChange) {
	aktivJe = map[int]*collectors.OpenspecChange{}
	archivJe = map[int]*collectors.OpenspecChange{}
	for i := range changes {
		c := &changes[i]
		nr := c.Issue
		if nr == 0 && !c.Archiviert {
			if t, ok := titelTreffer(offen, c.Kennung); ok {
				nr = t
			}
		}
		if nr == 0 {
			continue
		}
		if c.Archiviert {
			if archivJe[nr] == nil {
				archivJe[nr] = c
			}
		} else if aktivJe[nr] == nil {
			aktivJe[nr] = c
		}
	}
	return aktivJe, archivJe
}

// branchZuordnung liefert je offenem Issue die verknuepften Branch-Namen
// samt ihren Gruenden, und je Branch-Name den offenen PR mit diesem Head.
func branchZuordnung(e ArbeitEingabe) (map[int]map[string][]string, map[string]ghrest.PR) {
	offen := offeneIssues(e)
	offenNr := map[int]bool{}
	for _, i := range offen {
		offenNr[i.Nummer] = true
	}
	vorhanden := map[string]bool{}
	for _, b := range e.Branches {
		if !istStandard(b.Name, e.Standard) {
			vorhanden[b.Name] = true
		}
	}
	je := map[int]map[string][]string{}
	dazu := func(nr int, name, grund string) {
		if !offenNr[nr] || !vorhanden[name] {
			return
		}
		if je[nr] == nil {
			je[nr] = map[string][]string{}
		}
		for _, g := range je[nr][name] {
			if g == grund {
				return
			}
		}
		je[nr][name] = append(je[nr][name], grund)
	}

	aktivJe, _ := changesJeIssue(offen, e.Changes)
	for nr, c := range aktivJe {
		dazu(nr, "change/"+c.Kennung, BranchGrundChange)
	}
	for _, b := range e.Branches {
		gesehen := map[int]bool{}
		for _, n := range append(nummernAus(nameVorneRe, b.Name), nummernAus(nameHintenRe, b.Name)...) {
			if !gesehen[n] {
				gesehen[n] = true
				dazu(n, b.Name, BranchGrundName)
			}
		}
	}
	prJe := map[string]ghrest.PR{}
	for _, p := range e.PRs {
		if p.Head == "" || (p.HeadRepo != "" && !strings.EqualFold(p.HeadRepo, e.Repo)) {
			continue
		}
		if alt, ok := prJe[p.Head]; !ok || p.Nummer < alt.Nummer {
			prJe[p.Head] = p
		}
		for _, n := range append(nummernAus(schliesstRe, p.Text), nummernAus(titelRe, p.Titel)...) {
			dazu(n, p.Head, BranchGrundPR)
		}
	}
	return je, prJe
}

// VerknuepfteBranches nennt, sortiert, die Branches, die an mindestens einem
// offenen Issue haengen — nur fuer sie holt der Dienst Commit und Vergleich.
func VerknuepfteBranches(e ArbeitEingabe) []string {
	je, _ := branchZuordnung(e)
	menge := map[string]bool{}
	for _, namen := range je {
		for n := range namen {
			menge[n] = true
		}
	}
	out := make([]string, 0, len(menge))
	for n := range menge {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// branchesJeIssue baut je offenem Issue die Branch-Liste: nach Commit-Zeit,
// neueste zuerst; ohne Zeit am Ende, dann nach Namen.
func branchesJeIssue(e ArbeitEingabe, jetzt time.Time) map[int][]IssueBranch {
	je, prJe := branchZuordnung(e)
	fenster := e.AktivFenster
	if fenster <= 0 {
		fenster = BranchAktivFenster
	}
	sha := map[string]string{}
	for _, b := range e.Branches {
		sha[b.Name] = b.SHA
	}
	out := map[int][]IssueBranch{}
	for nr, namen := range je {
		liste := []IssueBranch{}
		for name, gruende := range namen {
			b := IssueBranch{Name: name, SHA: sha[name], Gruende: sortierteGruende(gruende)}
			b.KurzSHA = b.SHA
			if len(b.KurzSHA) > 7 {
				b.KurzSHA = b.KurzSHA[:7]
			}
			if k, ok := e.BranchKoepfe[name]; ok {
				b.Zeit, b.Betreff, b.Voraus, b.Zurueck = k.Zeit, ghrest.ErsteZeile(k.Betreff), k.Voraus, k.Zurueck
				if t, err := time.Parse(time.RFC3339, k.Zeit); err == nil {
					b.Aktiv = jetzt.Sub(t) < fenster
				}
			}
			if p, ok := prJe[name]; ok {
				b.PR = &BranchPR{Nummer: p.Nummer, Titel: p.Titel, URL: p.URL, Draft: p.Draft, Zustand: p.Abgleich, KonfliktDateien: p.KonfliktDateien, Checks: p.Checks}
			}
			liste = append(liste, b)
		}
		sort.Slice(liste, func(i, j int) bool {
			ti, ei := time.Parse(time.RFC3339, liste[i].Zeit)
			tj, ej := time.Parse(time.RFC3339, liste[j].Zeit)
			switch {
			case ei == nil && ej == nil && !ti.Equal(tj):
				return ti.After(tj)
			case (ei == nil) != (ej == nil):
				return ei == nil
			}
			return liste[i].Name < liste[j].Name
		})
		out[nr] = liste
	}
	return out
}

func sortierteGruende(g []string) []string {
	rang := map[string]int{BranchGrundChange: 0, BranchGrundName: 1, BranchGrundPR: 2}
	out := append([]string{}, g...)
	sort.SliceStable(out, func(i, j int) bool { return rang[out[i]] < rang[out[j]] })
	return out
}
