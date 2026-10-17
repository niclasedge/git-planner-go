package appctx

// Branches aufraeumen (branch-aufraeumen, #78): jeder Remote-Branch eines
// Repos steht in genau einer Gruppe — geschuetzt, gemergt, verlassen, aktiv —
// und sagt, ob und warum er geloescht werden darf. Die Einordnung entsteht
// HIER; die Oberflaeche zeichnet nur, und die Schreibroute prueft mit
// derselben Funktion gegen einen frisch geholten Stand.

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/niclasedge/git-planner-go/internal/collectors"
	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

// Die Gruppen, in der Folge der Antwort.
const (
	GruppeGemergt    = "gemergt"
	GruppeVerlassen  = "verlassen"
	GruppeAktiv      = "aktiv"
	GruppeGeschuetzt = "geschuetzt"
)

var gruppenFolge = []string{GruppeGemergt, GruppeVerlassen, GruppeAktiv, GruppeGeschuetzt}

// Zustaende eines PRs am Branch.
const (
	PRZustandOffen       = "offen"
	PRZustandGemergt     = "gemergt"
	PRZustandGeschlossen = "geschlossen"
)

// AufraeumKopf ist, was der Dienst je Branch geholt hat: der letzte Commit
// und der Vergleich gegen den Default-Branch. Verglichen sagt, ob der
// Vergleich gelang; ohne ihn sind Voraus und Zurueck unbekannt.
type AufraeumKopf struct {
	Zeit       string
	Betreff    string
	Voraus     int
	Zurueck    int
	Verglichen bool
}

// AufraeumEingabe ist der Stand eines Repos. PRs sind offene und
// geschlossene PRs (Zustand, Gemergt); Offen und Changes dienen nur der
// Zuordnung zu Issues nach den Regeln aus issue-branches.
type AufraeumEingabe struct {
	Repo     string
	Standard string
	Branches []ghrest.Branch
	PRs      []ghrest.PR
	Koepfe   map[string]AufraeumKopf
	Offen    []ghrest.Issue
	Changes  []collectors.OpenspecChange
}

// AufraeumPR ist der PR mit dem Branch als Head.
type AufraeumPR struct {
	Nummer  int    `json:"nummer"`
	Titel   string `json:"titel"`
	URL     string `json:"url,omitempty"`
	Zustand string `json:"zustand"`
}

// AufraeumIssue ist ein offenes Issue, an dem der Branch haengt.
type AufraeumIssue struct {
	Nummer  int      `json:"nummer"`
	Titel   string   `json:"titel"`
	Gruende []string `json:"gruende"`
}

// AufraeumBranch ist ein Branch in seiner Gruppe.
type AufraeumBranch struct {
	Name       string          `json:"name"`
	SHA        string          `json:"sha"`
	KurzSHA    string          `json:"kurz_sha"`
	Zeit       string          `json:"zeit,omitempty"`
	Betreff    string          `json:"betreff,omitempty"`
	Voraus     int             `json:"voraus"`
	Zurueck    int             `json:"zurueck"`
	Verglichen bool            `json:"verglichen"`
	PR         *AufraeumPR     `json:"pr,omitempty"`
	Issues     []AufraeumIssue `json:"issues"`
	Gruppe     string          `json:"gruppe"`
	Loeschbar  bool            `json:"loeschbar"`
	Grund      string          `json:"grund"`
}

// AufraeumGruppe ist eine Gruppe samt ihren Branches.
type AufraeumGruppe struct {
	Name     string           `json:"name"`
	Branches []AufraeumBranch `json:"branches"`
}

// BranchAufraeumen ist die Antwort von /api/branches. Anzahl ist die Zahl
// der loeschbaren Branches.
type BranchAufraeumen struct {
	Repo     string           `json:"repo"`
	Standard string           `json:"standard"`
	Anzahl   int              `json:"anzahl"`
	Gruppen  []AufraeumGruppe `json:"gruppen"`
	Grund    string           `json:"grund,omitempty"`
}

// Branch sucht einen Branch ueber alle Gruppen.
func (a BranchAufraeumen) Branch(name string) (AufraeumBranch, bool) {
	for _, g := range a.Gruppen {
		for _, b := range g.Branches {
			if b.Name == name {
				return b, true
			}
		}
	}
	return AufraeumBranch{}, false
}

// prRang: offen vor gemergt vor geschlossen.
func prRang(p ghrest.PR) int {
	switch {
	case p.Zustand == "open":
		return 0
	case p.Gemergt:
		return 1
	}
	return 2
}

func prZustand(p ghrest.PR) string {
	switch prRang(p) {
	case 0:
		return PRZustandOffen
	case 1:
		return PRZustandGemergt
	}
	return PRZustandGeschlossen
}

// eigenerPR sagt, ob der PR seinen Head in diesem Repo hat. Ein offener PR
// ohne Head-Repo (Fork geloescht) schuetzt trotzdem; fuer gemergt und
// verlassen zaehlt nur ein PR, dessen Head sicher hier liegt.
func eigenerPR(p ghrest.PR, repo string) bool {
	if p.HeadRepo == "" {
		return p.Zustand == "open"
	}
	return strings.EqualFold(p.HeadRepo, repo)
}

func commits(n int) string {
	if n == 1 {
		return "1 eigener Commit geht"
	}
	return fmt.Sprintf("%d eigene Commits gehen", n)
}

// BranchAufraeumenBauen ordnet die Branches ein.
func BranchAufraeumenBauen(e AufraeumEingabe) BranchAufraeumen {
	standard := e.Standard
	if standard == "" {
		standard = "main"
	}
	out := BranchAufraeumen{Repo: e.Repo, Standard: standard}

	// Der massgebliche PR je Head.
	prJe := map[string]ghrest.PR{}
	for _, p := range e.PRs {
		if p.Head == "" || !eigenerPR(p, e.Repo) {
			continue
		}
		alt, ok := prJe[p.Head]
		if !ok || prRang(p) < prRang(alt) || (prRang(p) == prRang(alt) && p.Nummer > alt.Nummer) {
			prJe[p.Head] = p
		}
	}

	// Issues ueber die Regeln aus issue-branches.
	je, _ := branchZuordnung(ArbeitEingabe{Repo: e.Repo, Offen: e.Offen, Changes: e.Changes, PRs: e.PRs,
		Branches: e.Branches, Standard: standard})
	titel := map[int]string{}
	for _, i := range e.Offen {
		titel[i.Nummer] = i.Titel
	}
	issuesJe := map[string][]AufraeumIssue{}
	for nr, namen := range je {
		for name, gruende := range namen {
			issuesJe[name] = append(issuesJe[name], AufraeumIssue{Nummer: nr, Titel: titel[nr], Gruende: sortierteGruende(gruende)})
		}
	}

	gruppen := map[string][]AufraeumBranch{}
	for _, br := range e.Branches {
		b := AufraeumBranch{Name: br.Name, SHA: br.SHA, KurzSHA: br.SHA, Issues: issuesJe[br.Name]}
		if len(b.KurzSHA) > 7 {
			b.KurzSHA = b.KurzSHA[:7]
		}
		if b.Issues == nil {
			b.Issues = []AufraeumIssue{}
		}
		sort.Slice(b.Issues, func(i, j int) bool { return b.Issues[i].Nummer < b.Issues[j].Nummer })
		k, hatKopf := e.Koepfe[br.Name]
		if hatKopf {
			b.Zeit, b.Betreff, b.Voraus, b.Zurueck, b.Verglichen = k.Zeit, ghrest.ErsteZeile(k.Betreff), k.Voraus, k.Zurueck, k.Verglichen
		}
		p, hatPR := prJe[br.Name]
		if hatPR {
			b.PR = &AufraeumPR{Nummer: p.Nummer, Titel: p.Titel, URL: p.URL, Zustand: prZustand(p)}
		}
		zustand := ""
		if hatPR {
			zustand = b.PR.Zustand
		}

		switch {
		case br.Name == standard:
			b.Gruppe, b.Grund = GruppeGeschuetzt, "Default-Branch"
		case br.Name == "main" || br.Name == "master":
			b.Gruppe, b.Grund = GruppeGeschuetzt, br.Name+" wird nie gelöscht"
		case br.Geschuetzt:
			b.Gruppe, b.Grund = GruppeGeschuetzt, "geschützt laut GitHub (protected)"
		case zustand == PRZustandOffen:
			b.Gruppe, b.Grund = GruppeGeschuetzt, fmt.Sprintf("PR #%d ist offen", p.Nummer)
		case b.Verglichen && b.Voraus == 0:
			b.Gruppe, b.Loeschbar, b.Grund = GruppeGemergt, true, "vollständig in "+standard+" enthalten"
		case zustand == PRZustandGemergt:
			b.Gruppe, b.Loeschbar, b.Grund = GruppeGemergt, true, fmt.Sprintf("PR #%d gemergt", p.Nummer)
		case zustand == PRZustandGeschlossen:
			b.Gruppe = GruppeVerlassen
			if b.Verglichen {
				b.Loeschbar, b.Grund = true, fmt.Sprintf("PR #%d geschlossen, nicht gemergt — %s verloren", p.Nummer, commits(b.Voraus))
			} else {
				b.Grund = fmt.Sprintf("PR #%d geschlossen, Vergleich gegen %s fehlt", p.Nummer, standard)
			}
		default:
			b.Gruppe = GruppeAktiv
			if b.Verglichen {
				b.Loeschbar, b.Grund = true, "kein PR — "+commits(b.Voraus)+" verloren"
			} else {
				b.Grund = "Vergleich gegen " + standard + " fehlt"
			}
		}
		if b.Loeschbar {
			out.Anzahl++
		}
		gruppen[b.Gruppe] = append(gruppen[b.Gruppe], b)
	}

	for _, name := range gruppenFolge {
		liste := gruppen[name]
		if liste == nil {
			liste = []AufraeumBranch{}
		}
		sort.SliceStable(liste, func(i, j int) bool {
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
		out.Gruppen = append(out.Gruppen, AufraeumGruppe{Name: name, Branches: liste})
	}
	return out
}
