package server

// Die Schreibrouten auf Pull Requests (pr-aktionen, #32):
//
//	POST /action/pr/freigeben       {repo, nummer}                    Draft -> ready for review
//	POST /action/pr/mergen          {repo, nummer, branch_loeschen, methode, titel, text}  Merge mit Methode auf den gesehenen Head (#82, prmergeoptionen.go)
//	POST /action/pr/branch-loeschen {repo, nummer}                    Head-Branch eines gemergten/geschlossenen PRs
//	POST /action/pr/base-holen      {repo, nummer}                    Base in den Branch holen, nur bei veraltet (#62)
//	POST /action/pr/konflikt-loesen {repo, nummer, expected_head_sha, dateien}  Konflikt je Datei loesen (#75, konfliktloesen.go)
//	POST /action/pr/anlegen         {repo, head, titel, text, draft}  PR gegen den Default-Branch (#79, pranlegen.go)
//	POST /action/pr/checks-neu      {repo, nummer, check_run_id}      fehlgeschlagene Pruefung neu starten (#80, prchecks.go)
//	POST /action/pr/auto-merge, zu-entwurf, wieder-oeffnen, bearbeiten  (#82, prmergeoptionen.go)
//	POST /action/pr/reviewer|antworten|thread                         Reviewer, Antworten, Threads (#81, prreviews.go)
//
// Dieselben Schichten wie bei /action/issue/: Netz-Gate (server.go), Origin,
// unbekannte Felder. Den Stand des PRs holt jede Route selbst, statt dem
// Browser zu glauben: ob er ein Draft ist, welcher sha oben liegt, wo sein
// Branch wohnt.

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/niclasedge/git-planner-go/internal/appctx"
	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

const prPraefix = "/action/pr/"

type prWunsch struct {
	Repo   string `json:"repo"`
	Nummer int    `json:"nummer"`
	// Nur konflikt-loesen (#75).
	ExpectedHeadSHA string              `json:"expected_head_sha"`
	Dateien         []dateiEntscheidung `json:"dateien"`
	// Nur checks-neu (#80).
	CheckRunID int64 `json:"check_run_id"`
}

type prRoutes struct{ issueRoutes }

// loeschbar sagt, warum ein Head-Branch nicht geloescht werden darf ("" = darf).
func loeschbar(repo appctx.LeistenRepo, pr ghrest.PR) string {
	switch {
	case pr.Head == "":
		return "der PR nennt keinen Head-Branch"
	case !strings.EqualFold(pr.HeadRepo, repo.NWO):
		return "der Branch liegt in einem anderen Repo (" + pr.HeadRepo + ")"
	case pr.Head == repo.DefaultBranch || pr.Head == pr.Base || pr.Head == "main" || pr.Head == "master":
		return "der Default-Branch wird nie geloescht"
	}
	return ""
}

func (pr prRoutes) protokoll(repo appctx.LeistenRepo, pc patClient, aktion string, nummer int, err error) {
	if err != nil {
		logZeile("pr-schreiben: repo=%s pat=%s aktion=%s nummer=%d fehler=%q", repo.NWO, pc.pat, aktion, nummer, err.Error())
		return
	}
	logZeile("pr-schreiben: repo=%s pat=%s aktion=%s nummer=%d ergebnis=ok", repo.NWO, pc.pat, aktion, nummer)
}

func (pr prRoutes) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeJSONStatus(w, http.StatusMethodNotAllowed, map[string]any{"fehler": r.Method + " wird hier nicht bedient: diese Routen schreiben, also nur POST"})
		return
	}
	if !csrfOkFuer(r) {
		writeJSONStatus(w, http.StatusForbidden, map[string]any{"fehler": "CSRF: fremde Origin blockiert"})
		return
	}
	aktion := strings.TrimPrefix(r.URL.Path, prPraefix)
	if aktion == "anlegen" {
		pr.anlegen(w, r)
		return
	}
	if h := pr.mergeOptionenAktion(aktion); h != nil {
		h(w, r)
		return
	}
	if reviewAktion(aktion) {
		pr.reviewsSchreiben(w, r, aktion)
		return
	}
	if aktion != "freigeben" && aktion != "mergen" && aktion != "branch-loeschen" && aktion != "base-holen" && aktion != "konflikt-loesen" && aktion != "checks-neu" {
		writeJSONStatus(w, http.StatusNotFound, map[string]any{"fehler": fmt.Sprintf("unbekannte Schreiboperation %q", aktion)})
		return
	}
	wunsch, err := rumpfLesen[prWunsch](w, r)
	if err != nil {
		eingabeFehler(w, "%v", err)
		return
	}
	if wunsch.Nummer <= 0 {
		eingabeFehler(w, "nummer ist Pflicht")
		return
	}
	repo, pc, ok := pr.schreibRepo(w, r, wunsch.Repo)
	if !ok {
		return
	}
	ctx := r.Context()
	stand, err := pc.client.PullRequest(ctx, repo.NWO, wunsch.Nummer)
	if err != nil {
		writeJSONStatus(w, http.StatusBadGateway, map[string]any{"fehler": err.Error()})
		return
	}
	if aktion == "konflikt-loesen" {
		pr.konfliktLoesen(w, r, repo, pc, wunsch, stand)
		return
	}
	if aktion == "checks-neu" {
		pr.checksNeu(w, r, repo, pc, wunsch, stand)
		return
	}

	var antwort = map[string]any{}
	switch aktion {
	case "freigeben":
		if !stand.Draft {
			eingabeFehler(w, "PR #%d ist kein Entwurf", wunsch.Nummer)
			return
		}
		err = pc.client.PRFreigeben(ctx, stand.NodeID)

	case "branch-loeschen":
		if stand.Zustand == "open" {
			eingabeFehler(w, "PR #%d ist noch offen — der Branch bleibt", wunsch.Nummer)
			return
		}
		if warum := loeschbar(repo, stand); warum != "" {
			eingabeFehler(w, "%s", warum)
			return
		}
		err = pc.client.BranchLoeschen(ctx, repo.NWO, stand.Head)

	case "base-holen":
		if stand.Zustand != "open" {
			eingabeFehler(w, "PR #%d ist nicht offen", wunsch.Nummer)
			return
		}
		if zerr := pc.client.PRZustandHolen(ctx, repo.NWO, &stand); zerr != nil && stand.Abgleich != ghrest.PRKonflikt {
			writeJSONStatus(w, http.StatusBadGateway, map[string]any{"fehler": zerr.Error()})
			return
		}
		switch stand.Abgleich {
		case ghrest.PRVeraltet:
		case ghrest.PRKonflikt:
			dateien := stand.KonfliktDateien
			if dateien == nil {
				dateien = []string{}
			}
			writeJSONStatus(w, http.StatusConflict, map[string]any{
				"fehler":  fmt.Sprintf("PR #%d hat einen Konflikt mit %s — die Base laesst sich nicht einfach nachziehen, der Konflikt muss im Branch geloest werden (#33)", wunsch.Nummer, stand.Base),
				"dateien": dateien,
			})
			return
		default:
			eingabeFehler(w, "PR #%d ist %s — nur ein veralteter PR wird nachgezogen", wunsch.Nummer, stand.Abgleich)
			return
		}
		err = pc.client.PRBaseHolen(ctx, repo.NWO, wunsch.Nummer, stand.HeadSHA)
	}
	pr.protokoll(repo, pc, aktion, wunsch.Nummer, err)
	if err != nil {
		writeJSONStatus(w, http.StatusBadGateway, map[string]any{"fehler": err.Error()})
		return
	}
	pr.d.verwerfen(repo.NWO)
	antwort["ok"] = true
	writeJSON(w, antwort)
}

// branchNachMerge loescht den Head-Branch nach einem Merge, wenn er darf, und
// sagt, was passiert ist. Ein Fehlschlag hier macht den Merge nicht ungeschehen
// und wird deshalb gemeldet, nicht als Fehler der Route.
func (pr prRoutes) branchNachMerge(ctx context.Context, repo appctx.LeistenRepo, pc patClient, stand ghrest.PR) string {
	if warum := loeschbar(repo, stand); warum != "" {
		return "nicht geloescht: " + warum
	}
	err := pc.client.BranchLoeschen(ctx, repo.NWO, stand.Head)
	pr.protokoll(repo, pc, "branch-loeschen", stand.Nummer, err)
	if err != nil {
		return "nicht geloescht: " + err.Error()
	}
	return "geloescht: " + stand.Head
}
