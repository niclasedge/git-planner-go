package server

// Die CI-Runs je Repo (ci-runs, #122):
//
//	GET  /api/ci?repo=[&frisch=1]      die letzten 20 Runs samt Jobs und Schritten
//	GET  /api/ci-log?repo=&job=        das aufbereitete Log eines Jobs
//	POST /action/ci/neu-starten {repo, run_id}  fehlgeschlagene Jobs eines roten Runs neu starten
//
// Der Stand je Repo liegt im Speicher und auf der Platte (internal/cache,
// ciruns.go). Jobs eines abgeschlossenen Runs und Logs abgeschlossener Jobs
// werden nie ein zweites Mal gefragt. Erneuert wird im Tageslauf (beim Start
// und stuendlich, was 24 h oder aelter ist) und bei jeder Anfrage an /api/ci
// im Hintergrund; ohne Stand oder mit frisch=1 synchron. Die Quelle heisst
// ci:owner/repo (sync.go). Punkte, Folge, Wahl und Log-Grenzen rechnet
// internal/appctx.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/niclasedge/git-planner-go/internal/anzeigezeit"
	"github.com/niclasedge/git-planner-go/internal/appctx"
	"github.com/niclasedge/git-planner-go/internal/cache"
	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

const (
	// ciAnzahl ist die Zahl der Runs je Repo.
	ciAnzahl = 20
	// ciTagesFrist ist das Alter, ab dem der Tageslauf einen Stand erneuert.
	ciTagesFrist = 24 * time.Hour
	// ciTaktAbstand ist der Abstand, in dem der Tageslauf prueft.
	ciTaktAbstand = time.Hour
	// ciStartVerzug ist die Wartezeit vor dem ersten Tageslauf: der Start
	// holt zuerst Leiste und Aggregate, und ein kurz gestarteter Dienst (ein
	// Test, der nur ein Bundle abruft) fragt GitHub gar nicht erst.
	ciStartVerzug = 30 * time.Second
	// ciLogDeckel ist die groesste gespeicherte Log-Laenge; darueber bleibt
	// das Ende.
	ciLogDeckel = 2 << 20
	ciPraefix   = "/action/ci/"
)

func ciQuelle(nwo string) string { return "ci:" + nwo }

// ciStand liefert den Stand eines Repos aus dem Speicher, beim ersten Mal von
// der Platte; nil ohne Stand.
func (d *repoDienst) ciStand(nwo string) *cache.CIStand {
	schluessel := strings.ToLower(nwo)
	d.mu.Lock()
	st, gelesen := d.ciStaende[schluessel]
	d.mu.Unlock()
	if gelesen {
		return st
	}
	st = cache.CIStandLesen(d.ciOrdner, nwo)
	d.mu.Lock()
	if schon, ok := d.ciStaende[schluessel]; ok {
		st = schon
	} else {
		d.ciStaende[schluessel] = st
	}
	d.mu.Unlock()
	return st
}

func (d *repoDienst) ciMerken(nwo string, st *cache.CIStand) {
	d.mu.Lock()
	d.ciStaende[strings.ToLower(nwo)] = st
	d.mu.Unlock()
	if err := cache.CIStandSchreiben(d.ciOrdner, nwo, st); err != nil {
		logZeile("ci-runs: repo=%s speichern fehler=%q", nwo, err.Error())
	}
}

// ciHolen fragt die Runs und die Jobs, die noch nicht abgeschlossen
// gespeichert sind. 404 bei jedem Zugang heisst: keine Actions.
func (d *repoDienst) ciHolen(ctx context.Context, r appctx.LeistenRepo, alt *cache.CIStand) (*cache.CIStand, error) {
	kand := kandidaten(r)
	if len(kand) == 0 {
		return nil, fmt.Errorf("kein PAT gesetzt (%s)", strings.Join(patVariablen, ", "))
	}
	var runs []ghrest.WorkflowRun
	var pc patClient
	var err error
	nichtDa := true
	for _, k := range kand {
		runs, err = k.client.WorkflowRuns(ctx, r.NWO, ciAnzahl)
		if err == nil {
			pc, nichtDa = k, false
			break
		}
		if ghrest.HTTPStatus(err) != http.StatusNotFound {
			nichtDa = false
		}
		logZeile("ci-runs: repo=%s pat=%s fehler=%q", r.NWO, k.pat, err.Error())
	}
	neu := &cache.CIStand{Zeit: time.Now().UTC(), Runs: []ghrest.WorkflowRun{}, Jobs: map[string]cache.CIJobs{}}
	if err != nil {
		if nichtDa {
			neu.Grund = appctx.CIKeineRuns
			return neu, nil
		}
		return nil, err
	}
	neu.Runs = runs
	if len(runs) == 0 {
		neu.Grund = appctx.CIKeineRuns
	}
	var fehlend []ghrest.WorkflowRun
	for _, run := range runs {
		s := cache.CIJobsSchluessel(run.ID, run.Versuch)
		if alt != nil {
			if j, ok := alt.Jobs[s]; ok && j.Abgeschlossen {
				neu.Jobs[s] = j
				continue
			}
		}
		fehlend = append(fehlend, run)
	}
	jobs := make([][]ghrest.ActionsJob, len(fehlend))
	fehler := make([]error, len(fehlend))
	parallel(len(fehlend), func(i int) {
		jobs[i], fehler[i] = pc.client.RunJobs(ctx, r.NWO, fehlend[i].ID)
	})
	for i, run := range fehlend {
		if fehler[i] != nil {
			return nil, fmt.Errorf("Jobs von Run %d: %v", run.ID, fehler[i])
		}
		neu.Jobs[cache.CIJobsSchluessel(run.ID, run.Versuch)] = cache.CIJobs{Abgeschlossen: run.Status == "completed", Jobs: jobs[i]}
	}
	logZeile("ci-runs: repo=%s pat=%s runs=%d jobs_gefragt=%d", r.NWO, pc.pat, len(runs), len(fehlend))
	return neu, nil
}

// ciErneuern holt und speichert den Stand. Der Aufrufer hat
// syncBeginn(ciQuelle) gewonnen. Ein Fehler laesst den alten Stand stehen.
func (d *repoDienst) ciErneuern(ctx context.Context, r appctx.LeistenRepo) error {
	start, vorher := time.Now(), ghrest.StatistikLesen()
	neu, err := d.ciHolen(ctx, r, d.ciStand(r.NWO))
	if err != nil {
		d.syncEnde(ciQuelle(r.NWO), start, vorher, nil, err.Error())
		return err
	}
	d.ciMerken(r.NWO, neu)
	d.syncEnde(ciQuelle(r.NWO), start, vorher, struct {
		Runs []ghrest.WorkflowRun
		Jobs map[string]cache.CIJobs
	}{neu.Runs, neu.Jobs}, "")
	return nil
}

// ciTagesLauf erneuert jedes Repo der Leiste ohne Stand oder mit einem Stand
// von ciTagesFrist oder aelter.
func (d *repoDienst) ciTagesLauf(ctx context.Context, jetzt time.Time) {
	l := d.leiste(ctx, false)
	var faellig []appctx.LeistenRepo
	for _, k := range l.Kategorien {
		for _, r := range k.Repos {
			if st := d.ciStand(r.NWO); st == nil || jetzt.Sub(st.Zeit) >= ciTagesFrist {
				faellig = append(faellig, r)
			}
		}
	}
	parallel(len(faellig), func(i int) {
		if d.syncBeginn(ciQuelle(faellig[i].NWO)) {
			_ = d.ciErneuern(ctx, faellig[i])
		}
	})
}

// ciTakt laeuft den Tageslauf nach ciStartVerzug und danach je
// ciTaktAbstand, bis ctx endet. main.go startet ihn (AppOptions.CIHintergrund);
// GKP_CI_TAKT=0 schaltet ihn ab.
func (d *repoDienst) ciTakt(ctx context.Context) {
	if os.Getenv("GKP_CI_TAKT") == "0" {
		return
	}
	select {
	case <-ctx.Done():
		return
	case <-time.After(ciStartVerzug):
	}
	for {
		lauf, abbrechen := context.WithTimeout(ctx, 10*time.Minute)
		d.ciTagesLauf(lauf, time.Now())
		abbrechen()
		select {
		case <-ctx.Done():
			return
		case <-time.After(ciTaktAbstand):
		}
	}
}

// ciPunkteEintragen setzt ci an jeder Zeile der Leiste mit gespeicherten Runs.
func (d *repoDienst) ciPunkteEintragen(l *appctx.RepoLeiste) {
	for i := range l.Kategorien {
		for j := range l.Kategorien[i].Repos {
			if st := d.ciStand(l.Kategorien[i].Repos[j].NWO); st != nil {
				l.Kategorien[i].Repos[j].CI = appctx.CILeistenPunkt(st.Runs)
			}
		}
	}
}

func ciJobsJeRun(st *cache.CIStand) map[int64][]ghrest.ActionsJob {
	out := map[int64][]ghrest.ActionsJob{}
	if st == nil {
		return out
	}
	for _, r := range st.Runs {
		out[r.ID] = st.Jobs[cache.CIJobsSchluessel(r.ID, r.Versuch)].Jobs
	}
	return out
}

func (d *repoDienst) serveCI(w http.ResponseWriter, r *http.Request) {
	repo, ok := d.repoAusAnfrage(w, r)
	if !ok {
		return
	}
	quelle := ciQuelle(repo.NWO)
	st := d.ciStand(repo.NWO)
	if st == nil || r.URL.Query().Get("frisch") == "1" {
		if d.syncBeginn(quelle) {
			_ = d.ciErneuern(r.Context(), repo)
		}
		st = d.ciStand(repo.NWO)
	} else if d.syncBeginn(quelle) {
		go func() {
			hg, abbrechen := context.WithTimeout(context.Background(), 2*time.Minute)
			defer abbrechen()
			_ = d.ciErneuern(hg, repo)
		}()
	}
	var a appctx.CIAnsicht
	if st == nil {
		a = appctx.CIAnsichtBauen(repo.NWO, nil, nil, anzeigezeit.Jetzt())
		a.Grund = ""
	} else {
		a = appctx.CIAnsichtBauen(repo.NWO, st.Runs, ciJobsJeRun(st), anzeigezeit.Jetzt())
		if st.Grund != "" {
			a.Grund = st.Grund
		}
		a.Stand = st.Zeit.UTC().Format(time.RFC3339)
	}
	a.Sync = d.syncStand(quelle)
	writeJSON(w, a)
}

// ciJob sucht einen Job im gespeicherten Stand.
func ciJob(st *cache.CIStand, id int64) (ghrest.ActionsJob, bool) {
	if st == nil {
		return ghrest.ActionsJob{}, false
	}
	for _, js := range st.Jobs {
		for _, j := range js.Jobs {
			if j.ID == id {
				return j, true
			}
		}
	}
	return ghrest.ActionsJob{}, false
}

func (d *repoDienst) serveCILog(w http.ResponseWriter, r *http.Request) {
	repo, ok := d.repoAusAnfrage(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.URL.Query().Get("job"), 10, 64)
	if err != nil || id <= 0 {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"grund": "Parameter job fehlt oder ist keine Zahl"})
		return
	}
	job, ok := ciJob(d.ciStand(repo.NWO), id)
	if !ok {
		writeJSONStatus(w, http.StatusNotFound, map[string]any{"grund": fmt.Sprintf("unbekannter Job %d", id)})
		return
	}
	text, gekuerzt, gespeichert := cache.CILogLesen(d.ciOrdner, repo.NWO, id)
	if !gespeichert {
		var letzter error = errors.New("kein PAT gesetzt")
		geholt := false
		for _, k := range kandidaten(repo) {
			text, gekuerzt, err = k.client.JobLog(r.Context(), repo.NWO, id, ciLogDeckel)
			if err == nil {
				geholt = true
				break
			}
			letzter = err
		}
		if !geholt {
			l := appctx.CILogAufbereiten("", job, false)
			l.Grund = letzter.Error()
			writeJSON(w, l)
			return
		}
		if job.Status == "completed" {
			if err := cache.CILogSchreiben(d.ciOrdner, repo.NWO, id, text, gekuerzt); err != nil {
				logZeile("ci-runs: repo=%s job=%d log speichern fehler=%q", repo.NWO, id, err.Error())
			}
		}
	}
	writeJSON(w, appctx.CILogAufbereiten(text, job, gekuerzt))
}

type ciWunsch struct {
	Repo  string `json:"repo"`
	RunID int64  `json:"run_id"`
}

// ciRoutes sind die Schreibrouten unter /action/ci/: dieselben Schichten wie
// bei /action/pr/ — Netz-Gate (server.go), Origin, unbekannte Felder.
type ciRoutes struct{ issueRoutes }

func (cr ciRoutes) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeJSONStatus(w, http.StatusMethodNotAllowed, map[string]any{"fehler": r.Method + " wird hier nicht bedient: diese Routen schreiben, also nur POST"})
		return
	}
	if !csrfOkFuer(r) {
		writeJSONStatus(w, http.StatusForbidden, map[string]any{"fehler": "CSRF: fremde Origin blockiert"})
		return
	}
	if aktion := strings.TrimPrefix(r.URL.Path, ciPraefix); aktion != "neu-starten" {
		writeJSONStatus(w, http.StatusNotFound, map[string]any{"fehler": fmt.Sprintf("unbekannte Schreiboperation %q", aktion)})
		return
	}
	wunsch, err := rumpfLesen[ciWunsch](w, r)
	if err != nil {
		eingabeFehler(w, "%v", err)
		return
	}
	if wunsch.RunID <= 0 {
		eingabeFehler(w, "run_id ist Pflicht")
		return
	}
	repo, pc, ok := cr.schreibRepo(w, r, wunsch.Repo)
	if !ok {
		return
	}
	var run *ghrest.WorkflowRun
	if st := cr.d.ciStand(repo.NWO); st != nil {
		for i := range st.Runs {
			if st.Runs[i].ID == wunsch.RunID {
				run = &st.Runs[i]
			}
		}
	}
	if run == nil {
		eingabeFehler(w, "Run %d steht nicht unter den gespeicherten Runs von %s", wunsch.RunID, repo.NWO)
		return
	}
	if run.Status != "completed" || appctx.CIPunkt(run.Status, run.Ergebnis) != appctx.CIRot {
		eingabeFehler(w, "Run %d ist nicht abgeschlossen und rot (%s, %s) — nur fehlgeschlagene Runs werden neu gestartet", run.ID, run.Status, run.Ergebnis)
		return
	}
	err = pc.client.RunNeuStarten(r.Context(), repo.NWO, run.ID)
	if err != nil {
		logZeile("ci-schreiben: repo=%s pat=%s aktion=neu-starten run=%d fehler=%q", repo.NWO, pc.pat, run.ID, err.Error())
		writeJSONStatus(w, http.StatusBadGateway, map[string]any{"fehler": err.Error()})
		return
	}
	logZeile("ci-schreiben: repo=%s pat=%s aktion=neu-starten run=%d ergebnis=ok", repo.NWO, pc.pat, run.ID)
	writeJSON(w, map[string]any{"ok": true})
}
