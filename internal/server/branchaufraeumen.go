package server

// Branches aufraeumen (branch-aufraeumen, #78):
//
//	GET  /api/branches?repo=         die Remote-Branches in vier Gruppen (appctx.BranchAufraeumenBauen)
//	POST /action/branch/loeschen     {repo, branch, sha} oder {repo, branches: [{branch, sha}]} (case in branchroutes.go)
//
// Die Leseroute holt Branches, PRs (offen und geschlossen) und je Branch
// Commit und Vergleich parallel ueber parallel(); die Abrufe laufen ueber den
// ETag-Speicher von ghrest. Die Schreibroute glaubt dem Browser nichts: je
// Branch holt sie den Stand frisch, ordnet ihn mit derselben Funktion ein und
// loescht nur, was loeschbar ist und noch auf dem gesehenen sha steht.
// Netz-Gate (server.go) und Origin wie unter /action/pr/.

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/niclasedge/git-planner-go/internal/appctx"
	"github.com/niclasedge/git-planner-go/internal/collectors"
	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

// aufraeumStandard ist der Default-Branch: laut Repo, sonst main vor master.
func aufraeumStandard(r appctx.LeistenRepo, branches []ghrest.Branch) string {
	if r.DefaultBranch != "" {
		return r.DefaultBranch
	}
	standard := "main"
	for _, b := range branches {
		if b.Name == "master" {
			standard = "master"
		}
	}
	for _, b := range branches {
		if b.Name == "main" {
			standard = "main"
		}
	}
	return standard
}

// aufraeumLage holt den Stand und ordnet ihn ein. nur != "" beschraenkt
// Commit und Vergleich auf diesen Branch und laesst die Issues weg (die
// Schreibroute). streng heisst: jeder Fehler ist ein Fehler — ohne PR-Liste
// koennte ein Branch mit offenem PR als gemergt gelten.
func (d *repoDienst) aufraeumLage(ctx context.Context, c ghrest.Client, r appctx.LeistenRepo, nur string, streng bool) (appctx.BranchAufraeumen, error) {
	branches, err := c.Branches(ctx, r.NWO)
	if err != nil {
		return appctx.BranchAufraeumen{}, err
	}
	e := appctx.AufraeumEingabe{Repo: r.NWO, Standard: aufraeumStandard(r, branches), Koepfe: map[string]appctx.AufraeumKopf{}}
	if nur != "" {
		for _, b := range branches {
			if b.Name == nur {
				e.Branches = append(e.Branches, b)
			}
		}
	} else {
		e.Branches = branches
	}

	var wg sync.WaitGroup
	var offenErr, zuErr, issueErr error
	var offen, zu []ghrest.PR
	wg.Add(2)
	go func() {
		defer wg.Done()
		offen, offenErr = c.PullRequests(ctx, r.NWO)
	}()
	go func() {
		defer wg.Done()
		zu, zuErr = c.GeschlossenePullRequests(ctx, r.NWO)
	}()
	if nur == "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var alle []ghrest.Issue
			alle, issueErr = c.OffeneIssues(ctx, r.NWO)
			for _, i := range alle {
				if !i.IstPR {
					e.Offen = append(e.Offen, i)
				}
			}
		}()
		d.mu.Lock()
		if st, ok := d.arbeit[strings.ToLower(r.NWO)]; ok {
			e.Changes = append([]collectors.OpenspecChange{}, st.changes...)
		}
		d.mu.Unlock()
	}

	var ziele []ghrest.Branch
	for _, b := range e.Branches {
		if b.Name != e.Standard {
			ziele = append(ziele, b)
		}
	}
	koepfe := make([]appctx.AufraeumKopf, len(ziele))
	fehler := make([]error, len(ziele))
	parallel(len(ziele), func(i int) {
		k, err := c.Commit(ctx, r.NWO, ziele[i].SHA)
		if err == nil {
			koepfe[i].Zeit, koepfe[i].Betreff = k.Zeit, k.Betreff
		}
		v, z, verr := c.Vergleich(ctx, r.NWO, e.Standard, ziele[i].SHA)
		if verr == nil {
			koepfe[i].Voraus, koepfe[i].Zurueck, koepfe[i].Verglichen = v, z, true
		}
		if verr != nil {
			fehler[i] = verr
		} else {
			fehler[i] = err
		}
	})
	wg.Wait()
	e.PRs = append(offen, zu...)
	var erster error
	for i, b := range ziele {
		e.Koepfe[b.Name] = koepfe[i]
		if fehler[i] != nil && erster == nil {
			erster = fmt.Errorf("%s: %v", b.Name, fehler[i])
		}
	}
	for _, f := range []error{offenErr, zuErr, issueErr} {
		if f != nil {
			erster = f
			break
		}
	}
	if streng && (offenErr != nil || zuErr != nil) {
		return appctx.BranchAufraeumen{}, fmt.Errorf("PR-Liste: %v", firstErr(offenErr, zuErr))
	}
	a := appctx.BranchAufraeumenBauen(e)
	if erster != nil {
		a.Grund = erster.Error()
	}
	return a, nil
}

func firstErr(errs ...error) error {
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}

func (d *repoDienst) serveBranches(w http.ResponseWriter, r *http.Request) {
	repo, ok := d.repoAusAnfrage(w, r)
	if !ok {
		return
	}
	kand := kandidaten(repo)
	if len(kand) == 0 {
		writeJSONStatus(w, http.StatusServiceUnavailable, map[string]any{"grund": "kein PAT gesetzt (" + strings.Join(patVariablen, ", ") + ")"})
		return
	}
	a, err := d.aufraeumLage(r.Context(), kand[0].client, repo, "", false)
	if err != nil {
		// Fail open wie jede Lese-Route: 200 mit Grund und leeren Gruppen.
		a = appctx.BranchAufraeumenBauen(appctx.AufraeumEingabe{Repo: repo.NWO, Standard: repo.DefaultBranch})
		a.Grund = err.Error()
	}
	logZeile("branches: repo=%s pat=%s anzahl=%d%s", repo.NWO, kand[0].pat, a.Anzahl, fehlerAnhang(err))
	writeJSON(w, a)
}

type branchZiel struct {
	Branch string `json:"branch"`
	SHA    string `json:"sha"`
}

type branchLoeschWunsch struct {
	Repo     string       `json:"repo"`
	Branch   string       `json:"branch"`
	SHA      string       `json:"sha"`
	Branches []branchZiel `json:"branches"`
}

type branchErgebnis struct {
	Branch string `json:"branch"`
	Status int    `json:"status"`
	OK     bool   `json:"ok"`
	Fehler string `json:"fehler,omitempty"`
}

// loeschen ist die Aktion loeschen unter /action/branch/ (branchroutes.go);
// Methode und Origin prueft dort ServeHTTP.
func (br branchRoutes) loeschen(w http.ResponseWriter, r *http.Request) {
	wunsch, err := rumpfLesen[branchLoeschWunsch](w, r)
	if err != nil {
		eingabeFehler(w, "%v", err)
		return
	}
	einzeln := len(wunsch.Branches) == 0
	ziele := wunsch.Branches
	if einzeln {
		ziele = []branchZiel{{Branch: wunsch.Branch, SHA: wunsch.SHA}}
	} else if wunsch.Branch != "" || wunsch.SHA != "" {
		eingabeFehler(w, "entweder branch und sha oder branches, nicht beides")
		return
	}
	for _, z := range ziele {
		if strings.TrimSpace(z.Branch) == "" || strings.TrimSpace(z.SHA) == "" {
			eingabeFehler(w, "branch und sha sind Pflicht")
			return
		}
	}
	repo, pc, ok := br.schreibRepo(w, r, wunsch.Repo)
	if !ok {
		return
	}
	ergebnisse := make([]branchErgebnis, 0, len(ziele))
	geloescht := false
	for _, z := range ziele {
		e := br.einenLoeschen(r.Context(), repo, pc, z)
		geloescht = geloescht || e.OK
		ergebnisse = append(ergebnisse, e)
	}
	if geloescht {
		br.d.verwerfen(repo.NWO)
	}
	if einzeln {
		e := ergebnisse[0]
		antwort := map[string]any{"ok": e.OK, "branch": e.Branch}
		if e.Fehler != "" {
			antwort["fehler"] = e.Fehler
		}
		writeJSONStatus(w, e.Status, antwort)
		return
	}
	alle := true
	for _, e := range ergebnisse {
		alle = alle && e.OK
	}
	writeJSON(w, map[string]any{"ok": alle, "ergebnisse": ergebnisse})
}

// einenLoeschen prueft einen Branch gegen den frischen Stand und loescht ihn.
func (br branchRoutes) einenLoeschen(ctx context.Context, repo appctx.LeistenRepo, pc patClient, z branchZiel) branchErgebnis {
	e := branchErgebnis{Branch: z.Branch}
	nein := func(status int, format string, args ...any) branchErgebnis {
		e.Status, e.Fehler = status, fmt.Sprintf(format, args...)
		logZeile("branch-schreiben: repo=%s pat=%s aktion=branch-loeschen branch=%s status=%d fehler=%q", repo.NWO, pc.pat, z.Branch, status, e.Fehler)
		return e
	}
	lage, err := br.d.aufraeumLage(ctx, pc.client, repo, z.Branch, true)
	if err != nil {
		return nein(http.StatusBadGateway, "%v", err)
	}
	b, ok := lage.Branch(z.Branch)
	if !ok {
		return nein(http.StatusConflict, "Branch %s gibt es nicht mehr", z.Branch)
	}
	if b.Gruppe == appctx.GruppeGeschuetzt {
		return nein(http.StatusForbidden, "%s ist geschützt: %s", z.Branch, b.Grund)
	}
	if warum := loeschbar(repo, ghrest.PR{Head: z.Branch, HeadRepo: repo.NWO, Base: lage.Standard}); warum != "" {
		return nein(http.StatusForbidden, "%s", warum)
	}
	if b.SHA != z.SHA {
		return nein(http.StatusConflict, "%s steht jetzt auf %s, gesehen war %s — neu laden", z.Branch, b.KurzSHA, z.SHA)
	}
	if !b.Loeschbar {
		return nein(http.StatusConflict, "%s ist nicht löschbar: %s", z.Branch, b.Grund)
	}
	if err := pc.client.BranchLoeschen(ctx, repo.NWO, z.Branch); err != nil {
		return nein(http.StatusBadGateway, "%v", err)
	}
	logZeile("branch-schreiben: repo=%s pat=%s aktion=branch-loeschen branch=%s gruppe=%s ergebnis=ok", repo.NWO, pc.pat, z.Branch, b.Gruppe)
	e.Status, e.OK = http.StatusOK, true
	return e
}
