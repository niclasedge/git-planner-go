package server

// Sync-Buchfuehrung (sync-status, #38): je Quelle ("leiste",
// "arbeit:owner/repo", "karte:owner/repo") der letzte Abruf, ob gerade einer
// laeuft, und ob sich der Inhalt geaendert hat. Die Quellen liefern nach
// Ablauf ihrer TTL sofort den letzten Stand und erneuern genau einmal im
// Hintergrund; ?frisch=1 erneuert synchron. Dank ETag (ghrest) ist eine
// Erneuerung ohne Aenderung bei GitHub ein 304 und kostet kein Rate-Limit.

import (
	"net/http"
	"os"
	"sort"
	"strconv"
	"time"

	"github.com/niclasedge/git-planner-go/internal/appctx"
	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

type syncEintrag struct {
	stand   appctx.SyncStand
	abdruck string
}

// ttlAusUmgebung liest GKP_SYNC_TTL (Sekunden) — fuer Tests und fuer wer
// oefter oder seltener synchronisieren will. Ohne Wert gilt standard.
func ttlAusUmgebung(standard time.Duration) time.Duration {
	if v := os.Getenv("GKP_SYNC_TTL"); v != "" {
		if s, err := strconv.Atoi(v); err == nil && s >= 0 {
			return time.Duration(s) * time.Second
		}
	}
	return standard
}

// syncBeginn meldet eine Erneuerung an. false: es laeuft schon eine.
func (d *repoDienst) syncBeginn(quelle string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	e := d.sync[quelle]
	if e == nil {
		e = &syncEintrag{}
		d.sync[quelle] = e
	}
	if e.stand.Laeuft {
		return false
	}
	e.stand.Laeuft = true
	return true
}

// syncEnde schliesst eine Erneuerung ab und liefert den neuen Stand.
func (d *repoDienst) syncEnde(quelle string, start time.Time, vorher ghrest.Statistik, wert any, fehler string) appctx.SyncStand {
	nachher := ghrest.StatistikLesen()
	abdruck := appctx.InhaltFingerabdruck(wert)
	d.mu.Lock()
	defer d.mu.Unlock()
	e := d.sync[quelle]
	if e == nil {
		e = &syncEintrag{}
		d.sync[quelle] = e
	}
	e.stand.Laeuft = false
	e.stand.DauerMS = time.Since(start).Milliseconds()
	e.stand.Aufrufe = nachher.Aufrufe - vorher.Aufrufe
	e.stand.Unveraendert = nachher.Unveraendert - vorher.Unveraendert
	e.stand.Fehler = fehler
	e.stand.Zeit = time.Now().UTC().Format(time.RFC3339)
	e.stand.Geaendert = false
	if fehler == "" {
		e.stand.Geaendert = abdruck != e.abdruck
		e.abdruck = abdruck
	}
	return e.stand
}

// syncStand liefert eine Kopie des Stands einer Quelle.
func (d *repoDienst) syncStand(quelle string) *appctx.SyncStand {
	d.mu.Lock()
	defer d.mu.Unlock()
	if e := d.sync[quelle]; e != nil {
		s := e.stand
		return &s
	}
	return &appctx.SyncStand{}
}

type syncQuelle struct {
	Name string `json:"name"`
	appctx.SyncStand
}

// serveSync liefert GET /api/sync: alle Quellen, die GitHub-Zaehler und die
// Konten mit kurzem Grund.
func (d *repoDienst) serveSync(w http.ResponseWriter, r *http.Request) {
	d.mu.Lock()
	quellen := make([]syncQuelle, 0, len(d.sync))
	for name, e := range d.sync {
		quellen = append(quellen, syncQuelle{Name: name, SyncStand: e.stand})
	}
	konten := d.konten
	d.mu.Unlock()
	sort.Slice(quellen, func(a, b int) bool { return quellen[a].Name < quellen[b].Name })
	kontoStand := []appctx.KontoStand{}
	for _, k := range konten {
		ks := appctx.KontoStand{PAT: k.PAT, Anzahl: len(k.Repos), Grund: appctx.KontoGrundKurz(k.Grund)}
		if ks.Grund != k.Grund {
			ks.Detail = k.Grund
		}
		kontoStand = append(kontoStand, ks)
	}
	writeJSON(w, map[string]any{"quellen": quellen, "github": ghrest.StatistikLesen(), "konten": kontoStand,
		"ttl_s": int(d.ttlArbeit.Seconds())})
}
