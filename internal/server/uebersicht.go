package server

// GET /api/uebersicht?tage= — Heute, Demnaechst und Irgendwann ueber alle Repos der
// Leiste (faelligkeit, #39). Je nicht archiviertem Repo ein bedingter Abruf der
// offenen Issues (ETag, 304 kostet kein Kontingent); eingeteilt wird in
// appctx.UebersichtBauen. Speicher und Hintergrund-Erneuerung wie bei der Karte.

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/niclasedge/git-planner-go/internal/anzeigezeit"
	"github.com/niclasedge/git-planner-go/internal/appctx"
	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

const uebersichtQuelle = "uebersicht"

type uebersichtStand struct {
	wert appctx.Uebersicht
	zeit time.Time
}

func (d *repoDienst) uebersichtHolen(ctx context.Context) appctx.Uebersicht {
	var repos []appctx.LeistenRepo
	for _, k := range d.leiste(ctx, false).Kategorien {
		for _, r := range k.Repos {
			if !r.Archiviert {
				repos = append(repos, r)
			}
		}
	}
	eingaben := make([]appctx.UebersichtRepo, len(repos))
	gruende := make([]string, len(repos))
	parallel(len(repos), func(i int) {
		eingaben[i].Repo = repos[i].NWO
		kand := kandidaten(repos[i])
		if len(kand) == 0 {
			gruende[i] = "kein PAT gesetzt"
			return
		}
		issues, err := kand[0].client.OffeneIssues(ctx, repos[i].NWO)
		if err != nil {
			gruende[i] = err.Error()
			return
		}
		eingaben[i].Issues = issues
	})
	u := appctx.UebersichtBauen(eingaben, anzeigezeit.Jetzt())
	for i, g := range gruende {
		if g != "" {
			u.Quellen = append(u.Quellen, appctx.ArbeitQuelle{Quelle: repos[i].NWO, Grund: g})
		}
	}
	logZeile("uebersicht: repos=%d ueberfaellig=%d heute=%d demnaechst=%d irgendwann=%d fehler=%d",
		len(repos), u.Zaehler.Ueberfaellig, u.Zaehler.Heute, u.Zaehler.Demnaechst, u.Zaehler.Irgendwann, len(u.Quellen))
	return u
}

func (d *repoDienst) serveUebersicht(w http.ResponseWriter, r *http.Request) {
	frisch := r.URL.Query().Get("frisch") == "1"
	d.mu.Lock()
	st := d.uebersicht
	d.mu.Unlock()
	erneuern := func(ctx context.Context) appctx.Uebersicht {
		start, vorher := time.Now(), ghrest.StatistikLesen()
		u := d.uebersichtHolen(ctx)
		d.mu.Lock()
		d.uebersicht = &uebersichtStand{wert: u, zeit: time.Now()}
		d.mu.Unlock()
		fehler := ""
		if len(u.Quellen) > 0 {
			fehler = u.Quellen[0].Quelle + ": " + u.Quellen[0].Grund
		}
		d.syncEnde(uebersichtQuelle, start, vorher, u, fehler)
		return u
	}
	var u appctx.Uebersicht
	switch {
	case st != nil && !frisch:
		if time.Since(st.zeit) >= d.ttlArbeit && d.syncBeginn(uebersichtQuelle) {
			go func() {
				hg, abbrechen := context.WithTimeout(context.Background(), time.Minute)
				defer abbrechen()
				erneuern(hg)
			}()
		}
		u = st.wert
	case d.syncBeginn(uebersichtQuelle):
		u = erneuern(r.Context())
	case st != nil:
		u = st.wert
	default:
		u = erneuern(r.Context())
	}
	// Das Fenster von demnaechst je Anfrage (heute-logbuch, #42); gespeichert
	// wird die volle Einteilung.
	tage, _ := strconv.Atoi(r.URL.Query().Get("tage"))
	u = appctx.FensterAnwenden(u, tage)
	u.Sync = d.syncStand(uebersichtQuelle)
	writeJSON(w, u)
}
