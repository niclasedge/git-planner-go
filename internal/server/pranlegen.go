package server

// POST /action/pr/anlegen {repo, head, titel, text, draft} (pr-anlegen, #79):
// einen PR vom Head gegen den Default-Branch anlegen. Gibt es fuer den Head
// schon einen offenen PR, meldet die Route ihn mit 409, statt einen zweiten
// anzulegen. Netz-Gate und Origin wie bei den anderen Routen unter
// /action/pr/ (prroutes.go prueft die Origin, bevor es hierher verzweigt).

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/niclasedge/git-planner-go/internal/appctx"
	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

type prAnlegenWunsch struct {
	Repo  string `json:"repo"`
	Head  string `json:"head"`
	Titel string `json:"titel"`
	Text  string `json:"text"`
	Draft bool   `json:"draft"`
}

func prKurz(p ghrest.PR) map[string]any {
	return map[string]any{"nummer": p.Nummer, "titel": p.Titel, "url": p.URL, "draft": p.Draft}
}

func (pr prRoutes) anlegen(w http.ResponseWriter, r *http.Request) {
	wunsch, err := rumpfLesen[prAnlegenWunsch](w, r)
	if err != nil {
		eingabeFehler(w, "%v", err)
		return
	}
	head := strings.TrimSpace(wunsch.Head)
	titel := strings.TrimSpace(wunsch.Titel)
	if !appctx.GueltigerBranchName(head) {
		eingabeFehler(w, "ungueltiger Head %q", head)
		return
	}
	if titel == "" {
		eingabeFehler(w, "titel ist Pflicht")
		return
	}
	repo, pc, ok := pr.schreibRepo(w, r, wunsch.Repo)
	if !ok {
		return
	}
	standard := standardVon(repo)
	if head == standard || head == "main" || head == "master" {
		eingabeFehler(w, "%s ist der Default-Branch und kein Head", head)
		return
	}
	ctx := r.Context()
	vorhanden, err := pc.client.OffenerPR(ctx, repo.NWO, head)
	if err != nil {
		writeJSONStatus(w, http.StatusBadGateway, map[string]any{"fehler": err.Error()})
		return
	}
	if vorhanden != nil {
		writeJSONStatus(w, http.StatusConflict, map[string]any{
			"fehler": fmt.Sprintf("fuer %s ist schon PR #%d offen", head, vorhanden.Nummer), "pr": prKurz(*vorhanden)})
		return
	}
	neu, err := pc.client.PRAnlegen(ctx, repo.NWO, ghrest.NeuerPR{Titel: titel, Text: wunsch.Text, Head: head, Base: standard, Draft: wunsch.Draft})
	pr.protokoll(repo, pc, "anlegen", neu.Nummer, err)
	if err != nil {
		if errors.Is(err, ghrest.ErrVorhanden) {
			writeJSONStatus(w, http.StatusConflict, map[string]any{"fehler": "fuer " + head + " ist schon ein PR offen"})
			return
		}
		writeJSONStatus(w, http.StatusBadGateway, map[string]any{"fehler": err.Error()})
		return
	}
	pr.d.verwerfen(repo.NWO)
	writeJSON(w, map[string]any{"ok": true, "pr": prKurz(neu), "base": standard})
}
