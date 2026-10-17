package server

// Konflikt je Datei loesen (konflikte-loesen, #75):
//
//	GET  /api/pr-konflikt?repo=&nummer=   je Konfliktdatei drei Staende, Zeitpunkte und die Vorschau
//	POST /action/pr/konflikt-loesen       {repo, nummer, expected_head_sha, dateien:[{pfad, wahl, hunks}]}
//
// Die Vorschau und das Ergebnis je Datei rechnet appctx; hier wird gelesen,
// geprueft und geschrieben. Die Schreibroute liegt unter /action/pr/ und
// damit hinter Netz-Gate und Origin-Pruefung (prroutes.go).

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/niclasedge/git-planner-go/internal/appctx"
	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

// dateiEntscheidung ist die Wahl fuer eine Konfliktdatei.
type dateiEntscheidung struct {
	Pfad  string   `json:"pfad"`
	Wahl  string   `json:"wahl"`
	Hunks []string `json:"hunks"`
}

type konfliktSeite struct {
	SHA     string `json:"sha"`
	KurzSHA string `json:"kurz_sha"`
	Zeit    string `json:"zeit"`
	Betreff string `json:"betreff"`
}

type konfliktDatei struct {
	appctx.KonfliktVorschau
	Seiten map[string]konfliktSeite `json:"seiten"`
}

func kurzSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

func seiteVon(k ghrest.CommitKopf, ref string) konfliktSeite {
	sha := k.SHA
	if sha == "" {
		sha = ref
	}
	return konfliktSeite{SHA: sha, KurzSHA: kurzSHA(sha), Zeit: k.Zeit, Betreff: k.Betreff}
}

func staendeFuerAppctx(st ghrest.DreiStaende) appctx.DateiStaende {
	return appctx.DateiStaende{Pfad: st.Pfad, Basis: st.Basis, Meine: st.Meine, Extern: st.Extern}
}

func (d *repoDienst) servePRKonflikt(w http.ResponseWriter, r *http.Request) {
	nummer, _ := strconv.Atoi(r.URL.Query().Get("nummer"))
	if nummer <= 0 {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"grund": "Parameter nummer fehlt"})
		return
	}
	repo, ok := d.repoAusAnfrage(w, r)
	if !ok {
		return
	}
	kand := kandidaten(repo)
	if len(kand) == 0 {
		writeJSONStatus(w, http.StatusServiceUnavailable, map[string]any{"grund": "kein PAT gesetzt (" + strings.Join(patVariablen, ", ") + ")"})
		return
	}
	c := kand[0].client
	ctx := r.Context()
	stand, err := c.PullRequest(ctx, repo.NWO, nummer)
	if err != nil {
		writeJSONStatus(w, http.StatusBadGateway, map[string]any{"grund": err.Error()})
		return
	}
	lage, err := c.KonfliktLageHolen(ctx, repo.NWO, &stand)
	if err != nil {
		writeJSONStatus(w, http.StatusBadGateway, map[string]any{"grund": err.Error()})
		return
	}
	dateien := []konfliktDatei{}
	if stand.Abgleich == ghrest.PRKonflikt {
		for _, pfad := range lage.Dateien {
			st, err := c.DreiStaendeHolen(ctx, repo.NWO, lage, pfad)
			if err != nil {
				writeJSONStatus(w, http.StatusBadGateway, map[string]any{"grund": err.Error()})
				return
			}
			dateien = append(dateien, konfliktDatei{
				KonfliktVorschau: appctx.KonfliktVorschauRechnen(staendeFuerAppctx(st)),
				Seiten: map[string]konfliktSeite{
					"basis":  seiteVon(st.BasisCommit, lage.MergeBasis),
					"meine":  seiteVon(st.MeineCommit, lage.HeadSHA),
					"extern": seiteVon(st.ExternCommit, lage.BaseSHA),
				},
			})
		}
	}
	writeJSON(w, map[string]any{"nummer": nummer, "zustand": stand.Abgleich, "head": stand.Head, "base": stand.Base,
		"head_sha": lage.HeadSHA, "base_sha": lage.BaseSHA, "merge_basis": lage.MergeBasis, "dateien": dateien})
}

// konfliktLoesen ist die Aktion konflikt-loesen; sie antwortet selbst.
func (pr prRoutes) konfliktLoesen(w http.ResponseWriter, r *http.Request, repo appctx.LeistenRepo, pc patClient, wunsch prWunsch, stand ghrest.PR) {
	ctx := r.Context()
	switch {
	case strings.TrimSpace(wunsch.ExpectedHeadSHA) == "":
		eingabeFehler(w, "expected_head_sha ist Pflicht")
		return
	case stand.Zustand != "open":
		eingabeFehler(w, "PR #%d ist nicht offen", wunsch.Nummer)
		return
	case !strings.EqualFold(stand.HeadRepo, repo.NWO):
		eingabeFehler(w, "der Branch liegt in einem anderen Repo (%s)", stand.HeadRepo)
		return
	case stand.HeadSHA != wunsch.ExpectedHeadSHA:
		writeJSONStatus(w, http.StatusConflict, map[string]any{"fehler": fmt.Sprintf("PR #%d steht inzwischen auf %s, gesehen war %s — bitte neu laden", wunsch.Nummer, kurzSHA(stand.HeadSHA), kurzSHA(wunsch.ExpectedHeadSHA))})
		return
	}
	lage, err := pc.client.KonfliktLageHolen(ctx, repo.NWO, &stand)
	if err != nil {
		writeJSONStatus(w, http.StatusBadGateway, map[string]any{"fehler": err.Error()})
		return
	}
	if stand.Abgleich != ghrest.PRKonflikt || len(lage.Dateien) == 0 {
		eingabeFehler(w, "PR #%d hat keinen Konflikt (%s)", wunsch.Nummer, stand.Abgleich)
		return
	}
	offen := map[string]bool{}
	for _, d := range lage.Dateien {
		offen[d] = true
	}
	je := map[string]dateiEntscheidung{}
	for _, e := range wunsch.Dateien {
		if !offen[e.Pfad] {
			eingabeFehler(w, "%q ist keine Konfliktdatei von PR #%d", e.Pfad, wunsch.Nummer)
			return
		}
		if _, doppelt := je[e.Pfad]; doppelt {
			eingabeFehler(w, "%q ist doppelt entschieden", e.Pfad)
			return
		}
		if e.Wahl != appctx.WahlMeine && e.Wahl != appctx.WahlExtern && e.Wahl != appctx.WahlMergen {
			eingabeFehler(w, "%s: unbekannte Wahl %q (meine, extern oder mergen)", e.Pfad, e.Wahl)
			return
		}
		je[e.Pfad] = e
	}
	for _, d := range lage.Dateien {
		if _, ok := je[d]; !ok {
			eingabeFehler(w, "%s ist nicht entschieden", d)
			return
		}
	}
	geloest := map[string]*string{}
	var zeilen []string
	for _, d := range lage.Dateien {
		e := je[d]
		st, err := pc.client.DreiStaendeHolen(ctx, repo.NWO, lage, d)
		if err != nil {
			writeJSONStatus(w, http.StatusBadGateway, map[string]any{"fehler": err.Error()})
			return
		}
		inhalt, err := appctx.KonfliktLoesung(staendeFuerAppctx(st), e.Wahl, e.Hunks)
		if err != nil {
			eingabeFehler(w, "%s: %v", d, err)
			return
		}
		geloest[d] = inhalt
		zeilen = append(zeilen, "- "+d+": "+e.Wahl)
	}
	nachricht := fmt.Sprintf("Merge %s in %s (Konflikt geloest)\n\n%s\n", stand.Base, stand.Head, strings.Join(zeilen, "\n"))
	commit, err := pc.client.MergeCommitSchreiben(ctx, repo.NWO, lage, geloest, nachricht)
	pr.protokoll(repo, pc, "konflikt-loesen", wunsch.Nummer, err)
	if errors.Is(err, ghrest.ErrKopfVeraltet) {
		writeJSONStatus(w, http.StatusConflict, map[string]any{"fehler": err.Error()})
		return
	}
	if err != nil {
		writeJSONStatus(w, http.StatusBadGateway, map[string]any{"fehler": err.Error()})
		return
	}
	pr.d.verwerfen(repo.NWO)
	writeJSON(w, map[string]any{"ok": true, "commit": commit})
}
