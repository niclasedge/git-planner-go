package appctx

// Merge-Optionen und Historie geschlossener PRs (pr-merge-optionen, #82):
// welche Methoden der Merge-Dialog anbietet und welche vorausgewaehlt ist,
// und wie die Liste "Geschlossene PRs" aussieht. Die Oberflaeche zeichnet nur.

import (
	"sort"
	"strings"
	"time"

	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

// MergeMethodenFolge ist die feste Folge der Methoden im Dialog.
var MergeMethodenFolge = []string{"merge", "squash", "rebase"}

// geschlosseneHoechstens begrenzt die Historie.
const geschlosseneHoechstens = 30

// MergeOptionen sind die Methoden, die der Merge-Dialog anbietet, und die
// vorausgewaehlte. Grund nennt, warum die Repo-Einstellungen fehlen.
type MergeOptionen struct {
	Methoden []string `json:"methoden"`
	Vorgabe  string   `json:"vorgabe"`
	Grund    string   `json:"grund,omitempty"`
}

// MergeOptionenBauen bildet die erlaubten Methoden in fester Folge; ohne
// Einstellungen (nil) alle drei. Die Vorgabe ist zuletzt, wenn erlaubt, sonst
// die erste erlaubte.
func MergeOptionenBauen(erlaubt *ghrest.MergeErlaubt, zuletzt string) MergeOptionen {
	o := MergeOptionen{Methoden: []string{}}
	for _, m := range MergeMethodenFolge {
		if erlaubt == nil ||
			(m == "merge" && erlaubt.Merge) || (m == "squash" && erlaubt.Squash) || (m == "rebase" && erlaubt.Rebase) {
			o.Methoden = append(o.Methoden, m)
		}
	}
	if o.Erlaubt(zuletzt) {
		o.Vorgabe = zuletzt
	} else if len(o.Methoden) > 0 {
		o.Vorgabe = o.Methoden[0]
	}
	return o
}

// Erlaubt sagt, ob m angeboten wird.
func (o MergeOptionen) Erlaubt(m string) bool {
	for _, x := range o.Methoden {
		if x == m {
			return true
		}
	}
	return false
}

// GeschlossenerPR ist eine Zeile der Historie.
type GeschlossenerPR struct {
	Nummer  int    `json:"nummer"`
	Titel   string `json:"titel"`
	URL     string `json:"url"`
	Head    string `json:"head,omitempty"`
	Gemergt bool   `json:"gemergt"`
	// Datum ist merged_at bei gemergten, sonst closed_at; Tag dasselbe als
	// TT.MM.JJJJ.
	Datum           string `json:"datum"`
	Tag             string `json:"tag"`
	BranchExistiert bool   `json:"branch_existiert"`
	// WiederOeffnen: nicht gemergt und der Branch steht noch — ohne Branch
	// laesst GitHub einen PR nicht wieder oeffnen.
	WiederOeffnen bool `json:"wieder_oeffnen"`
}

// GeschlossenePRsBauen baut die Historie: neueste zuerst, hoechstens 30. Ein
// Branch zaehlt nur, wenn der Head in diesem Repo liegt.
func GeschlossenePRsBauen(nwo string, prs []ghrest.PR, branches []ghrest.Branch) []GeschlossenerPR {
	da := map[string]bool{}
	for _, b := range branches {
		da[b.Name] = true
	}
	out := make([]GeschlossenerPR, 0, len(prs))
	for _, p := range prs {
		g := GeschlossenerPR{Nummer: p.Nummer, Titel: p.Titel, URL: p.URL, Head: p.Head, Gemergt: p.Gemergt, Datum: p.GeschlossenAm}
		if p.Gemergt && p.GemergtAm != "" {
			g.Datum = p.GemergtAm
		}
		if t, err := time.Parse(time.RFC3339, g.Datum); err == nil {
			g.Tag = t.Format("02.01.2006")
		}
		imRepo := p.HeadRepo == "" || strings.EqualFold(p.HeadRepo, nwo)
		g.BranchExistiert = imRepo && p.Head != "" && da[p.Head]
		g.WiederOeffnen = !g.Gemergt && g.BranchExistiert
		out = append(out, g)
	}
	sort.SliceStable(out, func(a, b int) bool {
		if out[a].Datum != out[b].Datum {
			return out[a].Datum > out[b].Datum
		}
		return out[a].Nummer > out[b].Nummer
	})
	if len(out) > geschlosseneHoechstens {
		out = out[:geschlosseneHoechstens]
	}
	return out
}
