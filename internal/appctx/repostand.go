package appctx

// Stand je Repo fuer die Leiste (things-farben-fortschritt, #41): offene
// Issues und PRs, erledigte Issues und der Anteil fuer den Fortschrittskreis.

import (
	"math"

	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

// RepoStand ist eine Zeile von GET /api/repo-stand.
type RepoStand struct {
	Repo            string  `json:"repo"`
	OffeneIssues    int     `json:"offene_issues"`
	OffenePRs       int     `json:"offene_prs"`
	ErledigteIssues int     `json:"erledigte_issues"`
	Anteil          float64 `json:"anteil"`
	Grund           string  `json:"grund,omitempty"`
}

// RepoStaende ist die Antwort von GET /api/repo-stand.
type RepoStaende struct {
	Repos []RepoStand `json:"repos"`
	Sync  *SyncStand  `json:"sync,omitempty"`
}

// RepoStandBauen zaehlt die offenen Eintraege nach Issue und PR und rechnet den
// Anteil erledigt / (erledigt + offen), auf vier Stellen gerundet.
func RepoStandBauen(repo string, offen []ghrest.Issue, erledigt int) RepoStand {
	s := RepoStand{Repo: repo, ErledigteIssues: max(erledigt, 0)}
	for _, i := range offen {
		if i.IstPR {
			s.OffenePRs++
		} else {
			s.OffeneIssues++
		}
	}
	if gesamt := s.ErledigteIssues + s.OffeneIssues; gesamt > 0 {
		s.Anteil = math.Round(float64(s.ErledigteIssues)/float64(gesamt)*10000) / 10000
	}
	return s
}
