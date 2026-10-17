package server

// Die Schreibrouten auf Branches, je Aktion ein case:
//
//	POST /action/branch/anlegen {repo, nummer, name}   Branch vom Kopf des Default-Branches (pr-anlegen, #79)
//	POST /action/branch/loeschen {repo, branch, sha} oder {repo, branches}  Remote-Branches aufraeumen (branch-aufraeumen, #78, branchaufraeumen.go)
//
// Dieselben Schichten wie unter /action/pr/: Netz-Gate (server.go), Origin,
// unbekannte Felder. Der Default-Branch kommt aus der Leiste, nicht vom
// Browser.

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/niclasedge/git-planner-go/internal/appctx"
	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

const branchPraefix = "/action/branch/"

type branchAnlegenWunsch struct {
	Repo   string `json:"repo"`
	Nummer int    `json:"nummer"`
	Name   string `json:"name"`
}

type branchRoutes struct{ issueRoutes }

// standardVon ist der Default-Branch eines Repos, ohne Angabe main.
func standardVon(repo appctx.LeistenRepo) string {
	if repo.DefaultBranch != "" {
		return repo.DefaultBranch
	}
	return "main"
}

func (br branchRoutes) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeJSONStatus(w, http.StatusMethodNotAllowed, map[string]any{"fehler": r.Method + " wird hier nicht bedient: diese Routen schreiben, also nur POST"})
		return
	}
	if !csrfOkFuer(r) {
		writeJSONStatus(w, http.StatusForbidden, map[string]any{"fehler": "CSRF: fremde Origin blockiert"})
		return
	}
	switch aktion := strings.TrimPrefix(r.URL.Path, branchPraefix); aktion {
	case "anlegen":
		br.anlegen(w, r)
	case "loeschen":
		br.loeschen(w, r)
	default:
		writeJSONStatus(w, http.StatusNotFound, map[string]any{"fehler": fmt.Sprintf("unbekannte Schreiboperation %q", aktion)})
	}
}

func (br branchRoutes) anlegen(w http.ResponseWriter, r *http.Request) {
	wunsch, err := rumpfLesen[branchAnlegenWunsch](w, r)
	if err != nil {
		eingabeFehler(w, "%v", err)
		return
	}
	name := strings.TrimSpace(wunsch.Name)
	if !appctx.GueltigerBranchName(name) {
		eingabeFehler(w, "ungueltiger Branch-Name %q", name)
		return
	}
	repo, pc, ok := br.schreibRepo(w, r, wunsch.Repo)
	if !ok {
		return
	}
	standard := standardVon(repo)
	if name == standard || name == "main" || name == "master" {
		eingabeFehler(w, "%s ist der Default-Branch", name)
		return
	}
	sha, err := pc.client.BranchAnlegen(r.Context(), repo.NWO, name, standard)
	if err != nil {
		logZeile("branch-schreiben: repo=%s pat=%s aktion=anlegen nummer=%d fehler=%q", repo.NWO, pc.pat, wunsch.Nummer, err.Error())
		if errors.Is(err, ghrest.ErrVorhanden) {
			writeJSONStatus(w, http.StatusConflict, map[string]any{"fehler": "Branch " + name + " existiert schon", "branch": name})
			return
		}
		writeJSONStatus(w, http.StatusBadGateway, map[string]any{"fehler": err.Error()})
		return
	}
	logZeile("branch-schreiben: repo=%s pat=%s aktion=anlegen nummer=%d ergebnis=ok", repo.NWO, pc.pat, wunsch.Nummer)
	br.d.verwerfen(repo.NWO)
	writeJSON(w, map[string]any{"ok": true, "branch": name, "sha": sha, "base": standard})
}
