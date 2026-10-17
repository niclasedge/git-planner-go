package appctx

// Das Logbuch der Issues ueber alle Repos (heute-logbuch, #42) — nicht zu
// verwechseln mit der Auswahl LogbuchID der alten Ansicht in logbuch.go: die geschlossenen Issues
// der letzten Tage, nach dem Tag des Schliessens in Ortszeit gruppiert,
// neuester Tag zuerst — wie das Logbuch in Things.

import (
	"sort"
	"time"

	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

// Das Fenster des Logbuchs in Tagen.
const (
	IssueLogbuchVorgabe = 14
	IssueLogbuchMax     = 365
)

// IssueLogbuchEintrag ist ein erledigtes Issue.
type IssueLogbuchEintrag struct {
	Repo        string         `json:"repo"`
	Nummer      int            `json:"nummer"`
	Titel       string         `json:"titel"`
	URL         string         `json:"url"`
	Labels      []ghrest.Label `json:"labels"`
	Geschlossen string         `json:"geschlossen"`
}

// IssueLogbuchTag sind die Eintraege eines Tages.
type IssueLogbuchTag struct {
	Datum     string                `json:"datum"`
	Eintraege []IssueLogbuchEintrag `json:"eintraege"`
}

// IssueLogbuch ist die Antwort von GET /api/logbuch.
type IssueLogbuch struct {
	Tage    []IssueLogbuchTag `json:"tage"`
	Fenster int               `json:"fenster"`
	Anzahl  int               `json:"anzahl"`
	Quellen []ArbeitQuelle    `json:"quellen"`
	Sync    *SyncStand        `json:"sync,omitempty"`
}

// IssueLogbuchFenster begrenzt tage auf 1..IssueLogbuchMax, 0 heisst Vorgabe.
func IssueLogbuchFenster(tage int) int {
	if tage <= 0 {
		return IssueLogbuchVorgabe
	}
	return min(tage, IssueLogbuchMax)
}

// IssueLogbuchBeginn ist Mitternacht (Ortszeit von jetzt) des ersten Tages im
// Fenster. Als since bleibt die Abruf-URL damit einen Tag lang gleich, und der
// ETag greift.
func IssueLogbuchBeginn(jetzt time.Time, tage int) time.Time {
	tage = IssueLogbuchFenster(tage)
	return time.Date(jetzt.Year(), jetzt.Month(), jetzt.Day(), 0, 0, 0, 0, jetzt.Location()).AddDate(0, 0, -(tage - 1))
}

// IssueLogbuchBauen gruppiert die geschlossenen Issues (ohne PRs) im Fenster nach
// Tag, neuester Tag zuerst, im Tag neuestes zuerst.
func IssueLogbuchBauen(repos []UebersichtRepo, jetzt time.Time, tage int) IssueLogbuch {
	tage = IssueLogbuchFenster(tage)
	beginn := IssueLogbuchBeginn(jetzt, tage)
	type mitZeit struct {
		e    IssueLogbuchEintrag
		zeit time.Time
	}
	var alle []mitZeit
	for _, r := range repos {
		for _, i := range r.Issues {
			if i.IstPR || i.Geschlossen == "" {
				continue
			}
			z, err := time.Parse(time.RFC3339, i.Geschlossen)
			if err != nil || z.Before(beginn) {
				continue
			}
			e := IssueLogbuchEintrag{Repo: r.Repo, Nummer: i.Nummer, Titel: i.Titel, URL: i.URL, Labels: i.Labels, Geschlossen: i.Geschlossen}
			if e.Labels == nil {
				e.Labels = []ghrest.Label{}
			}
			alle = append(alle, mitZeit{e, z.In(jetzt.Location())})
		}
	}
	sort.SliceStable(alle, func(a, b int) bool {
		if !alle[a].zeit.Equal(alle[b].zeit) {
			return alle[a].zeit.After(alle[b].zeit)
		}
		if alle[a].e.Repo != alle[b].e.Repo {
			return alle[a].e.Repo < alle[b].e.Repo
		}
		return alle[a].e.Nummer < alle[b].e.Nummer
	})
	l := IssueLogbuch{Tage: []IssueLogbuchTag{}, Fenster: tage, Anzahl: len(alle), Quellen: []ArbeitQuelle{}}
	for _, x := range alle {
		datum := x.zeit.Format(DatumsFormat)
		if n := len(l.Tage); n == 0 || l.Tage[n-1].Datum != datum {
			l.Tage = append(l.Tage, IssueLogbuchTag{Datum: datum})
		}
		l.Tage[len(l.Tage)-1].Eintraege = append(l.Tage[len(l.Tage)-1].Eintraege, x.e)
	}
	return l
}
