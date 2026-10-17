package server

// GET /api/beleg-galerie?repo= (beleg-galerie, #54): die Belege aller Changes
// des Repos, offen wie archiviert, juengster zuerst. Gelesen wird aus dem
// Arbeitsstand von /api/arbeit — dieselben Changes, derselbe Zwischenspeicher,
// kein zusaetzlicher Abruf. Gebaut wird in internal/appctx/beleggalerie.go.

import (
	"net/http"

	"github.com/niclasedge/git-planner-go/internal/appctx"
)

func (d *repoDienst) serveBelegGalerie(w http.ResponseWriter, r *http.Request) {
	repo, ok := d.repoAusAnfrage(w, r)
	if !ok {
		return
	}
	st, err := d.arbeitStand(r.Context(), repo, r.URL.Query().Get("frisch") == "1")
	if err != nil {
		// Fail open wie jede Lese-Route: 200 mit Grund.
		g := appctx.BelegGalerieBauen(repo.NWO, nil)
		g.Grund = err.Error()
		writeJSON(w, g)
		return
	}
	g := appctx.BelegGalerieBauen(repo.NWO, st.changes)
	g.Sync = d.syncStand("arbeit:" + repo.NWO)
	writeJSON(w, g)
}
