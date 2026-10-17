package server

// GET /api/repo-stand — je nicht archiviertem Repo der Leiste die offenen
// Issues und PRs, die erledigten Issues und der Anteil fuer den
// Fortschrittskreis (things-farben-fortschritt, #41). Je Repo drei bedingte
// Abrufe: die offene Liste (dieselbe URL wie /api/uebersicht, also meist 304)
// und zwei Zaehlungen mit per_page=1. Speicher und Hintergrund-Erneuerung wie
// bei der Uebersicht.

import (
	"context"
	"net/http"
	"time"

	"github.com/niclasedge/git-planner-go/internal/appctx"
	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

const repoStandQuelle = "repo-stand"

type repoStandStand struct {
	wert appctx.RepoStaende
	zeit time.Time
}

func (d *repoDienst) repoStandHolen(ctx context.Context) appctx.RepoStaende {
	var repos []appctx.LeistenRepo
	for _, k := range d.leiste(ctx, false).Kategorien {
		for _, r := range k.Repos {
			if !r.Archiviert {
				repos = append(repos, r)
			}
		}
	}
	out := appctx.RepoStaende{Repos: make([]appctx.RepoStand, len(repos))}
	parallel(len(repos), func(i int) {
		nwo := repos[i].NWO
		kand := kandidaten(repos[i])
		if len(kand) == 0 {
			out.Repos[i] = appctx.RepoStand{Repo: nwo, Grund: "kein PAT gesetzt"}
			return
		}
		c := kand[0].client
		offen, err := c.OffeneIssues(ctx, nwo)
		if err != nil {
			out.Repos[i] = appctx.RepoStand{Repo: nwo, Grund: err.Error()}
			return
		}
		erledigt, err := c.GeschlosseneAnzahl(ctx, nwo)
		out.Repos[i] = appctx.RepoStandBauen(nwo, offen, erledigt)
		if err != nil {
			out.Repos[i].Grund = err.Error()
		}
	})
	fehler := 0
	for _, s := range out.Repos {
		if s.Grund != "" {
			fehler++
		}
	}
	logZeile("repo-stand: repos=%d fehler=%d", len(repos), fehler)
	return out
}

func (d *repoDienst) serveRepoStand(w http.ResponseWriter, r *http.Request) {
	frisch := r.URL.Query().Get("frisch") == "1"
	d.mu.Lock()
	st := d.repoStand
	d.mu.Unlock()
	erneuern := func(ctx context.Context) appctx.RepoStaende {
		start, vorher := time.Now(), ghrest.StatistikLesen()
		s := d.repoStandHolen(ctx)
		d.mu.Lock()
		d.repoStand = &repoStandStand{wert: s, zeit: time.Now()}
		d.mu.Unlock()
		fehler := ""
		for _, x := range s.Repos {
			if x.Grund != "" {
				fehler = x.Repo + ": " + x.Grund
				break
			}
		}
		d.syncEnde(repoStandQuelle, start, vorher, s, fehler)
		return s
	}
	var s appctx.RepoStaende
	switch {
	case st != nil && !frisch:
		if time.Since(st.zeit) >= d.ttlArbeit && d.syncBeginn(repoStandQuelle) {
			go func() {
				hg, abbrechen := context.WithTimeout(context.Background(), time.Minute)
				defer abbrechen()
				erneuern(hg)
			}()
		}
		s = st.wert
	case d.syncBeginn(repoStandQuelle):
		s = erneuern(r.Context())
	case st != nil:
		s = st.wert
	default:
		s = erneuern(r.Context())
	}
	s.Sync = d.syncStand(repoStandQuelle)
	writeJSON(w, s)
}
