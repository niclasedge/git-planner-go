package server

// CI-Pruefungen je PR (pr-checks, #80): das Holen fuer /api/arbeit samt
// Zwischenspeicher je Head-SHA und die Neustart-Route
//
//	POST /action/pr/checks-neu {repo, nummer, check_run_id}
//
// hinter Netz-Gate (server.go) und Origin-Pruefung (prroutes.go). Den Stand
// des Check-Runs holt die Route selbst: er muss zum Head des PRs gehoeren
// und fehlgeschlagen sein.

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/niclasedge/git-planner-go/internal/appctx"
	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

// checksFrist ist die Frist, fuer die ein abgeschlossener Stand gilt.
const checksFrist = 10 * time.Minute

func (d *repoDienst) checksSpeicher() *appctx.ChecksSpeicher {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.checks == nil {
		d.checks = appctx.NeuerChecksSpeicher(checksFrist)
	}
	return d.checks
}

// checksHolen liefert die Pruefungen auf sha, aus dem Speicher oder frisch.
// Scheitert ein Abruf, bleibt der PR ohne checks.
func (d *repoDienst) checksHolen(ctx context.Context, c ghrest.Client, nwo, sha string) *ghrest.PRChecks {
	if sha == "" {
		return nil
	}
	sp := d.checksSpeicher()
	if v, ok := sp.Lesen(nwo, sha, time.Now()); ok {
		return &v
	}
	runs, err := c.CheckRuns(ctx, nwo, sha)
	if err != nil {
		return nil
	}
	statuses, err := c.CommitStatuses(ctx, nwo, sha)
	if err != nil {
		return nil
	}
	jetzt := time.Now()
	v := appctx.ChecksRechnen(sha, runs, statuses, jetzt)
	sp.Merken(nwo, v, jetzt)
	return &v
}

func (pr prRoutes) checksNeu(w http.ResponseWriter, r *http.Request, repo appctx.LeistenRepo, pc patClient, wunsch prWunsch, stand ghrest.PR) {
	if wunsch.CheckRunID <= 0 {
		eingabeFehler(w, "check_run_id ist Pflicht")
		return
	}
	ctx := r.Context()
	run, err := pc.client.CheckRunHolen(ctx, repo.NWO, wunsch.CheckRunID)
	if err != nil {
		writeJSONStatus(w, http.StatusBadGateway, map[string]any{"fehler": err.Error()})
		return
	}
	if run.HeadSHA != stand.HeadSHA {
		writeJSONStatus(w, http.StatusConflict, map[string]any{"fehler": fmt.Sprintf("der Check-Run gehoert nicht zum Head von PR #%d — die Liste ist veraltet", wunsch.Nummer)})
		return
	}
	if p := appctx.ChecksRechnen(stand.HeadSHA, []ghrest.CheckRun{run}, nil, time.Now()).Pruefungen[0]; !p.NeuStartbar {
		eingabeFehler(w, "%s ist nicht fehlgeschlagen (%s) — nur fehlgeschlagene Pruefungen werden neu gestartet", run.Name, p.Zustand)
		return
	}
	weg, err := pc.client.PruefungNeuStarten(ctx, repo.NWO, run)
	pr.protokoll(repo, pc, "checks-neu", wunsch.Nummer, err)
	if err != nil {
		writeJSONStatus(w, http.StatusBadGateway, map[string]any{"fehler": err.Error()})
		return
	}
	pr.d.checksSpeicher().Verwerfen(repo.NWO, stand.HeadSHA)
	pr.d.verwerfen(repo.NWO)
	writeJSON(w, map[string]any{"ok": true, "weg": weg})
}
