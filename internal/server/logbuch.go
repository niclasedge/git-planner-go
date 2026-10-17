package server

// GET /api/logbuch?tage= — die geschlossenen Issues aller nicht archivierten
// Repos der Leiste, nach Tag gruppiert (heute-logbuch, #42). Je Repo ein
// bedingter Abruf issues?state=closed&since=…; since ist Mitternacht des
// ersten Tages, die URL bleibt einen Tag lang gleich und der ETag greift.
// Speicher je Fenster, Hintergrund-Erneuerung wie bei der Uebersicht.

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/niclasedge/git-planner-go/internal/anzeigezeit"
	"github.com/niclasedge/git-planner-go/internal/appctx"
	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

type logbuchStand struct {
	wert appctx.IssueLogbuch
	zeit time.Time
}

func (d *repoDienst) logbuchHolen(ctx context.Context, tage int) appctx.IssueLogbuch {
	jetzt := anzeigezeit.Jetzt()
	seit := appctx.IssueLogbuchBeginn(jetzt, tage)
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
		issues, err := kand[0].client.GeschlosseneIssuesSeit(ctx, repos[i].NWO, seit)
		if err != nil {
			gruende[i] = err.Error()
			return
		}
		eingaben[i].Issues = issues
	})
	l := appctx.IssueLogbuchBauen(eingaben, jetzt, tage)
	for i, g := range gruende {
		if g != "" {
			l.Quellen = append(l.Quellen, appctx.ArbeitQuelle{Quelle: repos[i].NWO, Grund: g})
		}
	}
	logZeile("logbuch: repos=%d tage=%d erledigt=%d fehler=%d", len(repos), l.Fenster, l.Anzahl, len(l.Quellen))
	return l
}

func (d *repoDienst) serveLogbuch(w http.ResponseWriter, r *http.Request) {
	tage, _ := strconv.Atoi(r.URL.Query().Get("tage"))
	tage = appctx.IssueLogbuchFenster(tage)
	frisch := r.URL.Query().Get("frisch") == "1"
	quelle := fmt.Sprintf("logbuch:%d", tage)
	d.mu.Lock()
	st := d.logbuecher[tage]
	d.mu.Unlock()
	erneuern := func(ctx context.Context) appctx.IssueLogbuch {
		start, vorher := time.Now(), ghrest.StatistikLesen()
		l := d.logbuchHolen(ctx, tage)
		d.mu.Lock()
		d.logbuecher[tage] = &logbuchStand{wert: l, zeit: time.Now()}
		d.mu.Unlock()
		fehler := ""
		if len(l.Quellen) > 0 {
			fehler = l.Quellen[0].Quelle + ": " + l.Quellen[0].Grund
		}
		d.syncEnde(quelle, start, vorher, l, fehler)
		return l
	}
	var l appctx.IssueLogbuch
	switch {
	case st != nil && !frisch:
		if time.Since(st.zeit) >= d.ttlArbeit && d.syncBeginn(quelle) {
			go func() {
				hg, abbrechen := context.WithTimeout(context.Background(), time.Minute)
				defer abbrechen()
				erneuern(hg)
			}()
		}
		l = st.wert
	case d.syncBeginn(quelle):
		l = erneuern(r.Context())
	case st != nil:
		l = st.wert
	default:
		l = erneuern(r.Context())
	}
	l.Sync = d.syncStand(quelle)
	writeJSON(w, l)
}
