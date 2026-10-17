package server

// Merge-Optionen am PR (pr-merge-optionen, #82):
//
//	POST /action/pr/mergen         {repo, nummer, branch_loeschen, methode?, titel?, text?}  mergen mit Methode
//	POST /action/pr/auto-merge     {repo, nummer, an, methode?, titel?, text?}               Auto-Merge an/aus
//	POST /action/pr/zu-entwurf     {repo, nummer}                                            zurueck zum Entwurf
//	POST /action/pr/wieder-oeffnen {repo, nummer}                                            geschlossenen PR oeffnen
//	POST /action/pr/bearbeiten     {repo, nummer, labels?, zustaendig?, meilenstein?}        ueber die Issues-API
//	GET  /api/pr-optionen?repo=    erlaubte Methoden und Vorgabe
//	GET  /api/pr-geschlossen?repo= die letzten 30 geschlossenen PRs
//
// Die Schreibrouten haengen an prroutes.go und liegen damit hinter demselben
// Netz-Gate und derselben Origin-Pruefung. Den Stand des PRs und die
// erlaubten Methoden holt jede Route selbst, statt dem Browser zu glauben.

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/niclasedge/git-planner-go/internal/appctx"
	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

type prMergeWunsch struct {
	Repo           string `json:"repo"`
	Nummer         int    `json:"nummer"`
	BranchLoeschen bool   `json:"branch_loeschen"`
	Methode        string `json:"methode"`
	Titel          string `json:"titel"`
	Text           string `json:"text"`
}

type prAutoMergeWunsch struct {
	Repo    string `json:"repo"`
	Nummer  int    `json:"nummer"`
	An      bool   `json:"an"`
	Methode string `json:"methode"`
	Titel   string `json:"titel"`
	Text    string `json:"text"`
}

type prNummerWunsch struct {
	Repo   string `json:"repo"`
	Nummer int    `json:"nummer"`
}

type prBearbeitenWunsch struct {
	Repo        string    `json:"repo"`
	Nummer      int       `json:"nummer"`
	Labels      *[]string `json:"labels"`
	Zustaendig  *[]string `json:"zustaendig"`
	Meilenstein *int      `json:"meilenstein"`
}

// mergeOptionenAktion liefert den Handler einer Aktion aus dieser Datei, sonst nil.
func (pr prRoutes) mergeOptionenAktion(aktion string) func(http.ResponseWriter, *http.Request) {
	switch aktion {
	case "mergen":
		return pr.mergen
	case "auto-merge":
		return pr.autoMerge
	case "zu-entwurf":
		return pr.zuEntwurf
	case "wieder-oeffnen":
		return pr.wiederOeffnen
	case "bearbeiten":
		return pr.bearbeiten
	}
	return nil
}

// ------------------------------------------------------------ Vorgabe

func (d *repoDienst) letzteMethode(nwo string) string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.mergeWahl[strings.ToLower(nwo)]
}

func (d *repoDienst) methodeMerken(nwo, methode string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.mergeWahl == nil {
		d.mergeWahl = map[string]string{}
	}
	d.mergeWahl[strings.ToLower(nwo)] = methode
}

// mergeOptionen liest die Einstellungen des Repos; sind sie nicht lesbar,
// bietet der Dienst alle drei an und nennt den Grund.
func (d *repoDienst) mergeOptionen(ctx context.Context, c ghrest.Client, nwo string) appctx.MergeOptionen {
	erlaubt, err := c.MergeErlaubtHolen(ctx, nwo)
	if err != nil {
		o := appctx.MergeOptionenBauen(nil, d.letzteMethode(nwo))
		o.Grund = err.Error()
		return o
	}
	return appctx.MergeOptionenBauen(&erlaubt, d.letzteMethode(nwo))
}

// ------------------------------------------------------------ Schreiben

// prStand liest Rumpf, Repo und den Stand des PRs; false heisst: beantwortet.
func prStand[T any](pr prRoutes, w http.ResponseWriter, r *http.Request, nummer func(T) (string, int)) (T, appctx.LeistenRepo, patClient, ghrest.PR, bool) {
	wunsch, err := rumpfLesen[T](w, r)
	if err != nil {
		eingabeFehler(w, "%v", err)
		return wunsch, appctx.LeistenRepo{}, patClient{}, ghrest.PR{}, false
	}
	nwo, n := nummer(wunsch)
	if n <= 0 {
		eingabeFehler(w, "nummer ist Pflicht")
		return wunsch, appctx.LeistenRepo{}, patClient{}, ghrest.PR{}, false
	}
	repo, pc, ok := pr.schreibRepo(w, r, nwo)
	if !ok {
		return wunsch, repo, pc, ghrest.PR{}, false
	}
	stand, err := pc.client.PullRequest(r.Context(), repo.NWO, n)
	if err != nil {
		writeJSONStatus(w, http.StatusBadGateway, map[string]any{"fehler": err.Error()})
		return wunsch, repo, pc, stand, false
	}
	return wunsch, repo, pc, stand, true
}

// methodePruefen lehnt eine unbekannte oder im Repo nicht erlaubte Methode
// mit 400 ab.
func (pr prRoutes) methodePruefen(w http.ResponseWriter, ctx context.Context, repo appctx.LeistenRepo, pc patClient, methode string) bool {
	if !ghrest.GueltigeMethode(methode) {
		eingabeFehler(w, "methode muss merge, squash oder rebase sein, nicht %q", methode)
		return false
	}
	o := pr.d.mergeOptionen(ctx, pc.client, repo.NWO)
	if !o.Erlaubt(methode) {
		eingabeFehler(w, "%s erlaubt %s nicht (erlaubt: %s)", repo.NWO, methode, strings.Join(o.Methoden, ", "))
		return false
	}
	return true
}

// fertig protokolliert, verwirft bei Erfolg den Speicher und antwortet.
func (pr prRoutes) fertig(w http.ResponseWriter, repo appctx.LeistenRepo, pc patClient, aktion string, nummer int, err error, antwort map[string]any) {
	pr.protokoll(repo, pc, aktion, nummer, err)
	if err != nil {
		writeJSONStatus(w, http.StatusBadGateway, map[string]any{"fehler": err.Error()})
		return
	}
	pr.d.verwerfen(repo.NWO)
	antwort["ok"] = true
	writeJSON(w, antwort)
}

func (pr prRoutes) mergen(w http.ResponseWriter, r *http.Request) {
	wunsch, err := rumpfLesen[prMergeWunsch](w, r)
	if err != nil {
		eingabeFehler(w, "%v", err)
		return
	}
	methode := strings.TrimSpace(wunsch.Methode)
	if methode == "" {
		methode = "merge"
	}
	if !ghrest.GueltigeMethode(methode) {
		eingabeFehler(w, "methode muss merge, squash oder rebase sein, nicht %q", methode)
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
	if stand.Draft {
		eingabeFehler(w, "PR #%d ist ein Entwurf — erst fuer Review freigeben", wunsch.Nummer)
		return
	}
	if stand.Zustand != "open" {
		eingabeFehler(w, "PR #%d ist nicht offen", wunsch.Nummer)
		return
	}
	if !pr.methodePruefen(w, ctx, repo, pc, methode) {
		return
	}
	wahl := ghrest.MergeWahl{Methode: methode, Titel: wunsch.Titel, Text: wunsch.Text}
	if methode == "rebase" {
		wahl.Titel, wahl.Text = "", ""
	}
	antwort := map[string]any{"methode": methode}
	err = pc.client.PRMergenMit(ctx, repo.NWO, wunsch.Nummer, stand.HeadSHA, wahl)
	if err == nil {
		pr.d.methodeMerken(repo.NWO, methode)
		if wunsch.BranchLoeschen {
			antwort["branch"] = pr.branchNachMerge(ctx, repo, pc, stand)
		}
	}
	pr.fertig(w, repo, pc, "mergen", wunsch.Nummer, err, antwort)
}

func (pr prRoutes) autoMerge(w http.ResponseWriter, r *http.Request) {
	wunsch, repo, pc, stand, ok := prStand(pr, w, r, func(x prAutoMergeWunsch) (string, int) { return x.Repo, x.Nummer })
	if !ok {
		return
	}
	ctx := r.Context()
	if stand.Zustand != "open" {
		eingabeFehler(w, "PR #%d ist nicht offen", wunsch.Nummer)
		return
	}
	if !wunsch.An {
		if stand.AutoMerge == nil {
			eingabeFehler(w, "PR #%d hat keinen Auto-Merge eingeschaltet", wunsch.Nummer)
			return
		}
		pr.fertig(w, repo, pc, "auto-merge-aus", wunsch.Nummer, pc.client.AutoMergeAus(ctx, stand.NodeID), map[string]any{})
		return
	}
	if stand.Draft {
		eingabeFehler(w, "PR #%d ist ein Entwurf — Auto-Merge erst nach dem Freigeben", wunsch.Nummer)
		return
	}
	methode := strings.TrimSpace(wunsch.Methode)
	if methode == "" {
		methode = "merge"
	}
	if !pr.methodePruefen(w, ctx, repo, pc, methode) {
		return
	}
	wahl := ghrest.MergeWahl{Methode: methode, Titel: wunsch.Titel, Text: wunsch.Text}
	if methode == "rebase" {
		wahl.Titel, wahl.Text = "", ""
	}
	err := pc.client.AutoMergeAn(ctx, stand.NodeID, wahl)
	if err == nil {
		pr.d.methodeMerken(repo.NWO, methode)
	}
	pr.fertig(w, repo, pc, "auto-merge-an", wunsch.Nummer, err, map[string]any{"methode": methode})
}

func (pr prRoutes) zuEntwurf(w http.ResponseWriter, r *http.Request) {
	wunsch, repo, pc, stand, ok := prStand(pr, w, r, func(x prNummerWunsch) (string, int) { return x.Repo, x.Nummer })
	if !ok {
		return
	}
	if stand.Zustand != "open" {
		eingabeFehler(w, "PR #%d ist nicht offen", wunsch.Nummer)
		return
	}
	if stand.Draft {
		eingabeFehler(w, "PR #%d ist schon ein Entwurf", wunsch.Nummer)
		return
	}
	pr.fertig(w, repo, pc, "zu-entwurf", wunsch.Nummer, pc.client.PRZuEntwurf(r.Context(), stand.NodeID), map[string]any{})
}

func (pr prRoutes) wiederOeffnen(w http.ResponseWriter, r *http.Request) {
	wunsch, repo, pc, stand, ok := prStand(pr, w, r, func(x prNummerWunsch) (string, int) { return x.Repo, x.Nummer })
	if !ok {
		return
	}
	if stand.Gemergt {
		writeJSONStatus(w, http.StatusConflict, map[string]any{"fehler": fmt.Sprintf("PR #%d ist gemergt und laesst sich nicht wieder oeffnen", wunsch.Nummer)})
		return
	}
	if stand.Zustand == "open" {
		eingabeFehler(w, "PR #%d ist schon offen", wunsch.Nummer)
		return
	}
	pr.fertig(w, repo, pc, "wieder-oeffnen", wunsch.Nummer, pc.client.PRWiederOeffnen(r.Context(), repo.NWO, wunsch.Nummer), map[string]any{})
}

func (pr prRoutes) bearbeiten(w http.ResponseWriter, r *http.Request) {
	wunsch, err := rumpfLesen[prBearbeitenWunsch](w, r)
	if err != nil {
		eingabeFehler(w, "%v", err)
		return
	}
	if wunsch.Nummer <= 0 {
		eingabeFehler(w, "nummer ist Pflicht")
		return
	}
	if wunsch.Labels == nil && wunsch.Zustaendig == nil && wunsch.Meilenstein == nil {
		eingabeFehler(w, "nichts zu aendern: labels, zustaendig oder meilenstein")
		return
	}
	if m := wunsch.Meilenstein; m != nil && *m < 0 {
		eingabeFehler(w, "meilenstein muss eine Milestone-Nummer oder 0 sein, nicht %d", *m)
		return
	}
	repo, pc, ok := pr.schreibRepo(w, r, wunsch.Repo)
	if !ok {
		return
	}
	_, err = pc.client.IssueAendern(r.Context(), repo.NWO, wunsch.Nummer,
		ghrest.IssueAenderung{Labels: wunsch.Labels, Zustaendig: wunsch.Zustaendig, Meilenstein: wunsch.Meilenstein})
	pr.fertig(w, repo, pc, "bearbeiten", wunsch.Nummer, err, map[string]any{})
}

// ------------------------------------------------------------ Lesen

func (d *repoDienst) servePROptionen(w http.ResponseWriter, r *http.Request) {
	repo, ok := d.repoAusAnfrage(w, r)
	if !ok {
		return
	}
	kand := kandidaten(repo)
	if len(kand) == 0 {
		o := appctx.MergeOptionenBauen(nil, d.letzteMethode(repo.NWO))
		o.Grund = "kein PAT gesetzt"
		writeJSON(w, o)
		return
	}
	writeJSON(w, d.mergeOptionen(r.Context(), kand[0].client, repo.NWO))
}

func (d *repoDienst) servePRGeschlossen(w http.ResponseWriter, r *http.Request) {
	repo, ok := d.repoAusAnfrage(w, r)
	if !ok {
		return
	}
	kand := kandidaten(repo)
	if len(kand) == 0 {
		writeJSON(w, map[string]any{"prs": []appctx.GeschlossenerPR{}, "grund": "kein PAT gesetzt"})
		return
	}
	c := kand[0].client
	prs, err := c.GeschlossenePRs(r.Context(), repo.NWO)
	if err != nil {
		writeJSON(w, map[string]any{"prs": []appctx.GeschlossenerPR{}, "grund": err.Error()})
		return
	}
	antwort := map[string]any{}
	branches, err := c.Branches(r.Context(), repo.NWO)
	if err != nil {
		antwort["grund"] = "Branches: " + err.Error()
	}
	antwort["prs"] = appctx.GeschlossenePRsBauen(repo.NWO, prs, branches)
	writeJSON(w, antwort)
}
