package server

// Reviews je PR (pr-reviews, #81): das Holen fuer /api/arbeit und die
// Schreibrouten
//
//	POST /action/pr/reviewer  {repo, nummer, hinzufuegen, entfernen}  Reviewer anfordern/entfernen
//	POST /action/pr/antworten {repo, nummer, kommentar_id, text}      Antwort im Thread
//	POST /action/pr/thread    {repo, nummer, kommentar_id, aufgeloest} Thread aufloesen/wieder oeffnen
//
// hinter Netz-Gate (server.go) und Origin-Pruefung (prroutes.go). Jede Route
// liest den Stand bei GitHub selbst, statt dem Browser zu glauben: wer
// angefragt ist, wer zustaendig sein kann, welcher Kommentar zu welchem
// Thread gehoert und ob der Thread schon aufgeloest ist.

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/niclasedge/git-planner-go/internal/appctx"
	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

// reviewsHolen liefert Entscheidung, Reviewer und Threads eines PRs.
// Scheitert ein REST-Abruf, bleibt der PR ohne reviews; scheitert nur
// GraphQL, gelten alle Threads als offen (thread_zustand false).
func (d *repoDienst) reviewsHolen(ctx context.Context, c ghrest.Client, nwo string, p ghrest.PR) *ghrest.PRReviews {
	reviews, err := c.Reviews(ctx, nwo, p.Nummer)
	if err != nil {
		return nil
	}
	kommentare, err := c.ReviewKommentare(ctx, nwo, p.Nummer)
	if err != nil {
		return nil
	}
	var zustand map[int64]ghrest.ThreadZustand
	if len(kommentare) == 0 {
		zustand = map[int64]ghrest.ThreadZustand{}
	} else if z, err := c.ThreadZustaende(ctx, nwo, p.Nummer); err == nil {
		zustand = z
	}
	v := appctx.ReviewsRechnen(reviews, p.Angefordert, p.AngefordertTeams, kommentare, zustand)
	return &v
}

type prReviewWunsch struct {
	Repo        string   `json:"repo"`
	Nummer      int      `json:"nummer"`
	Hinzufuegen []string `json:"hinzufuegen"`
	Entfernen   []string `json:"entfernen"`
	KommentarID int64    `json:"kommentar_id"`
	Text        string   `json:"text"`
	Aufgeloest  *bool    `json:"aufgeloest"`
}

var loginRe = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,38})$`)

// reviewAktion sagt, ob aktion eine Route dieser Datei ist.
func reviewAktion(aktion string) bool {
	return aktion == "reviewer" || aktion == "antworten" || aktion == "thread"
}

func enthaelt(liste []string, s string) bool {
	for _, x := range liste {
		if strings.EqualFold(x, s) {
			return true
		}
	}
	return false
}

// loginsPruefen bereinigt eine Login-Liste: getrimmt, ohne Doppelte.
func loginsPruefen(w http.ResponseWriter, feld string, roh []string) ([]string, bool) {
	out := []string{}
	for _, l := range roh {
		l = strings.TrimSpace(l)
		if !loginRe.MatchString(l) {
			eingabeFehler(w, "%s: %q ist kein GitHub-Login", feld, l)
			return nil, false
		}
		if !enthaelt(out, l) {
			out = append(out, l)
		}
	}
	return out, true
}

func (pr prRoutes) reviewsSchreiben(w http.ResponseWriter, r *http.Request, aktion string) {
	wunsch, err := rumpfLesen[prReviewWunsch](w, r)
	if err != nil {
		eingabeFehler(w, "%v", err)
		return
	}
	if wunsch.Nummer <= 0 {
		eingabeFehler(w, "nummer ist Pflicht")
		return
	}
	switch aktion {
	case "reviewer":
		pr.reviewer(w, r, wunsch)
	case "antworten":
		pr.antworten(w, r, wunsch)
	case "thread":
		pr.thread(w, r, wunsch)
	}
}

func (pr prRoutes) reviewer(w http.ResponseWriter, r *http.Request, wunsch prReviewWunsch) {
	dazu, ok := loginsPruefen(w, "hinzufuegen", wunsch.Hinzufuegen)
	if !ok {
		return
	}
	weg, ok := loginsPruefen(w, "entfernen", wunsch.Entfernen)
	if !ok {
		return
	}
	if len(dazu) == 0 && len(weg) == 0 {
		eingabeFehler(w, "hinzufuegen oder entfernen ist Pflicht")
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
	if stand.Zustand != "open" {
		eingabeFehler(w, "PR #%d ist nicht offen", wunsch.Nummer)
		return
	}
	if len(dazu) > 0 {
		moeglich, err := pc.client.MoeglicheZustaendige(ctx, repo.NWO)
		if err != nil {
			writeJSONStatus(w, http.StatusBadGateway, map[string]any{"fehler": err.Error()})
			return
		}
		for _, l := range dazu {
			if strings.EqualFold(l, stand.Autor) {
				eingabeFehler(w, "%s hat PR #%d geschrieben und kann ihn nicht reviewen", l, wunsch.Nummer)
				return
			}
			if !enthaelt(moeglich, l) {
				eingabeFehler(w, "%s ist nicht unter den moeglichen Zustaendigen von %s", l, repo.NWO)
				return
			}
		}
	}
	for _, l := range weg {
		if !enthaelt(stand.Angefordert, l) {
			writeJSONStatus(w, http.StatusConflict, map[string]any{"fehler": fmt.Sprintf("%s ist fuer PR #%d nicht als Reviewer angefragt — die Liste ist veraltet", l, wunsch.Nummer)})
			return
		}
	}
	if len(dazu) > 0 {
		err = pc.client.ReviewerAnfordern(ctx, repo.NWO, wunsch.Nummer, dazu)
		pr.protokoll(repo, pc, "reviewer-anfordern", wunsch.Nummer, err)
		if err != nil {
			writeJSONStatus(w, http.StatusBadGateway, map[string]any{"fehler": err.Error()})
			return
		}
	}
	if len(weg) > 0 {
		err = pc.client.ReviewerEntfernen(ctx, repo.NWO, wunsch.Nummer, weg)
		pr.protokoll(repo, pc, "reviewer-entfernen", wunsch.Nummer, err)
		if err != nil {
			pr.d.verwerfen(repo.NWO)
			writeJSONStatus(w, http.StatusBadGateway, map[string]any{"fehler": err.Error()})
			return
		}
	}
	pr.d.verwerfen(repo.NWO)
	writeJSON(w, map[string]any{"ok": true, "hinzugefuegt": dazu, "entfernt": weg})
}

func (pr prRoutes) antworten(w http.ResponseWriter, r *http.Request, wunsch prReviewWunsch) {
	text := strings.TrimSpace(wunsch.Text)
	if wunsch.KommentarID <= 0 {
		eingabeFehler(w, "kommentar_id ist Pflicht")
		return
	}
	if text == "" {
		eingabeFehler(w, "text ist Pflicht")
		return
	}
	repo, pc, ok := pr.schreibRepo(w, r, wunsch.Repo)
	if !ok {
		return
	}
	ctx := r.Context()
	kommentare, err := pc.client.ReviewKommentare(ctx, repo.NWO, wunsch.Nummer)
	if err != nil {
		writeJSONStatus(w, http.StatusBadGateway, map[string]any{"fehler": err.Error()})
		return
	}
	var ziel int64
	for _, t := range appctx.ThreadsBilden(kommentare, nil) {
		for _, k := range t.Kommentare {
			if k.ID == wunsch.KommentarID {
				ziel = t.ID
			}
		}
	}
	if ziel == 0 {
		writeJSONStatus(w, http.StatusNotFound, map[string]any{"fehler": fmt.Sprintf("PR #%d hat keinen Review-Kommentar %d", wunsch.Nummer, wunsch.KommentarID)})
		return
	}
	neu, err := pc.client.ReviewAntworten(ctx, repo.NWO, wunsch.Nummer, ziel, text)
	pr.protokoll(repo, pc, "antworten", wunsch.Nummer, err)
	if err != nil {
		writeJSONStatus(w, http.StatusBadGateway, map[string]any{"fehler": err.Error()})
		return
	}
	pr.d.verwerfen(repo.NWO)
	writeJSON(w, map[string]any{"ok": true, "thread": ziel, "kommentar": neu})
}

func (pr prRoutes) thread(w http.ResponseWriter, r *http.Request, wunsch prReviewWunsch) {
	if wunsch.KommentarID <= 0 {
		eingabeFehler(w, "kommentar_id ist Pflicht")
		return
	}
	if wunsch.Aufgeloest == nil {
		eingabeFehler(w, "aufgeloest ist Pflicht (true oder false)")
		return
	}
	repo, pc, ok := pr.schreibRepo(w, r, wunsch.Repo)
	if !ok {
		return
	}
	ctx := r.Context()
	zustaende, err := pc.client.ThreadZustaende(ctx, repo.NWO, wunsch.Nummer)
	if err != nil {
		writeJSONStatus(w, http.StatusBadGateway, map[string]any{"fehler": err.Error()})
		return
	}
	z, ok := zustaende[wunsch.KommentarID]
	if !ok {
		writeJSONStatus(w, http.StatusNotFound, map[string]any{"fehler": fmt.Sprintf("PR #%d hat keinen Thread, der mit Kommentar %d beginnt", wunsch.Nummer, wunsch.KommentarID)})
		return
	}
	if z.Aufgeloest == *wunsch.Aufgeloest {
		wie := "offen"
		if z.Aufgeloest {
			wie = "aufgeloest"
		}
		writeJSONStatus(w, http.StatusConflict, map[string]any{"fehler": fmt.Sprintf("der Thread ist schon %s — die Liste ist veraltet", wie)})
		return
	}
	err = pc.client.ThreadUmstellen(ctx, z.ID, *wunsch.Aufgeloest)
	pr.protokoll(repo, pc, "thread", wunsch.Nummer, err)
	if err != nil {
		writeJSONStatus(w, http.StatusBadGateway, map[string]any{"fehler": err.Error()})
		return
	}
	pr.d.verwerfen(repo.NWO)
	writeJSON(w, map[string]any{"ok": true, "aufgeloest": *wunsch.Aufgeloest})
}
