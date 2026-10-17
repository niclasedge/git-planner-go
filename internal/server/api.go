package server

// Die Rechnung hinter /api/leiste und /api/aufgaben (projektliste-api, #11).
// Hier wird nur aufgeloest und verdrahtet; ausgewaehlt, gezaehlt und sortiert
// wird in internal/appctx.

import (
	"errors"
	"net/http"
	"time"

	"github.com/niclasedge/git-planner-go/internal/anzeigezeit"
	"github.com/niclasedge/git-planner-go/internal/appctx"
	"github.com/niclasedge/git-planner-go/internal/areas"
	"github.com/niclasedge/git-planner-go/internal/cache"
	"github.com/niclasedge/git-planner-go/internal/collectors"
)

// leistenProjekte ist die Projektliste der Seitenleiste: aus dem Aggregat,
// wenn es steht, sonst frisch entdeckt — dieselbe Wahl wie servePage.
func leistenProjekte(getProjects func() []collectors.ProjectRef) []collectors.ProjectRef {
	if agg := cache.GetAggregate(); agg != nil && len(agg.Projects) > 0 {
		out := make([]collectors.ProjectRef, len(agg.Projects))
		for i, p := range agg.Projects {
			out[i] = p.ProjectRef
		}
		return out
	}
	return getProjects()
}

// leisteRechnen zaehlt jedes Projekt ueber alle Quellen. Teuer (bd je
// Projekt, GitHub je Projekt) und deshalb nur hinter cache.GetLeiste.
// Eine unlesbare areas.json kostet die Zuordnung, nicht die Leiste: alle
// Projekte stehen dann unter "Ohne Bereich".
func leisteRechnen(projekte []collectors.ProjectRef, jetzt time.Time) appctx.Leiste {
	st, err := areas.Default().Load()
	if err != nil {
		st = areas.State{}
	}
	bp := make([]appctx.BereichProjekt, 0, len(projekte))
	zaehler := make(map[string]appctx.ProjektZaehler, len(projekte))
	for _, p := range projekte {
		bp = append(bp, appctx.BereichProjekt{Name: p.Name, Pfad: p.Path})
		zaehler[p.Path] = appctx.LeistenZaehler(appctx.KartenAllerQuellen(p.Path), jetzt)
	}
	return appctx.LeisteBauen(bp, st, zaehler, jetzt)
}

// projektAufloesen findet das Projekt zu project: zuerst ueber den Pfad (so
// nennt die Leiste es), dann ueber den Namen.
func projektAufloesen(project string, projekte []collectors.ProjectRef) (collectors.ProjectRef, bool) {
	if project == "" {
		return collectors.ProjectRef{}, false
	}
	for _, p := range projekte {
		if p.Path == project {
			return p, true
		}
	}
	for _, p := range projekte {
		if p.Name == project {
			return p, true
		}
	}
	return collectors.ProjectRef{}, false
}

func apiAufgaben(w http.ResponseWriter, project, eltern string, projekte []collectors.ProjectRef) {
	p, ok := projektAufloesen(project, projekte)
	if !ok {
		grund := "unbekanntes Projekt " + project
		if project == "" {
			grund = "Parameter project fehlt"
		}
		writeJSONStatus(w, http.StatusNotFound, map[string]any{"grund": grund})
		return
	}
	l, err := appctx.ProjektAufgaben(appctx.KartenAllerQuellen(p.Path), eltern, anzeigezeit.Jetzt())
	if errors.Is(err, appctx.ErrUnbekannterEltern) {
		writeJSONStatus(w, http.StatusNotFound, map[string]any{"grund": err.Error()})
		return
	}
	l.Projekt = p.Path
	writeJSON(w, l)
}
