package server

// Die Schreibrouten auf Issues (issue-verwaltung, #26):
//
//	POST /action/issue/anlegen       {repo, titel, text, labels, zustaendig, faellig?}
//	POST /action/issue/aendern       {repo, nummer, titel?, text?, zustand?, grund?, labels?, zustaendig?, faellig?, meilenstein?}
//	POST /action/issue/kommentieren  {repo, nummer, text}
//	POST /action/issue/blocker       {repo, nummer, blocker, entfernen}
//
// Sie liegen unter /action/ und damit unter dem Netz-Gate in server.go (nur
// ueber 127.0.0.1); csrfOk prueft zusaetzlich die Origin, rumpfLesen lehnt
// unbekannte Felder ab — dieselben zwei Schichten wie bei den Task-Routen.
// Geschrieben wird mit dem PAT, der das Repo in der Leiste sieht. Nach jedem
// Erfolg verwirft der Dienst die zwischengespeicherte Arbeitsliste des Repos.
//
// faellig (JJJJ-MM-TT, leer entfernt) steht als letzte Zeile "Fällig: …" im
// Text (faelligkeit, #39); beim Aendern ohne text liest der Dienst den
// aktuellen Text bei GitHub und ersetzt nur diese Zeile.

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/niclasedge/git-planner-go/internal/appctx"
	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

const issuePraefix = "/action/issue/"

type issueAnlegenWunsch struct {
	Repo       string   `json:"repo"`
	Titel      string   `json:"titel"`
	Text       string   `json:"text"`
	Labels     []string `json:"labels"`
	Zustaendig []string `json:"zustaendig"`
	Faellig    string   `json:"faellig"`
}

type issueAendernWunsch struct {
	Repo       string    `json:"repo"`
	Nummer     int       `json:"nummer"`
	Titel      *string   `json:"titel"`
	Text       *string   `json:"text"`
	Zustand    *string   `json:"zustand"`
	Grund      *string   `json:"grund"`
	Labels     *[]string `json:"labels"`
	Zustaendig *[]string `json:"zustaendig"`
	Faellig    *string   `json:"faellig"`
	// Meilenstein ist die Nummer des Milestones, 0 entfernt ihn (#82).
	Meilenstein *int `json:"meilenstein"`
}

type issueKommentarWunsch struct {
	Repo   string `json:"repo"`
	Nummer int    `json:"nummer"`
	Text   string `json:"text"`
}

type issueBlockerWunsch struct {
	Repo      string `json:"repo"`
	Nummer    int    `json:"nummer"`
	Blocker   int    `json:"blocker"`
	Entfernen bool   `json:"entfernen"`
}

type issueRoutes struct{ d *repoDienst }

func eingabeFehler(w http.ResponseWriter, format string, args ...any) {
	writeJSONStatus(w, http.StatusBadRequest, map[string]any{"fehler": fmt.Sprintf(format, args...), "grund": "eingabe"})
}

// schreibRepo findet das Repo und den Zugang, mit dem geschrieben wird.
func (ir issueRoutes) schreibRepo(w http.ResponseWriter, r *http.Request, nwo string) (appctx.LeistenRepo, patClient, bool) {
	nwo = strings.TrimSpace(nwo)
	if !ghrest.GueltigesNWO(nwo) {
		eingabeFehler(w, "repo fehlt oder ist kein owner/repo")
		return appctx.LeistenRepo{}, patClient{}, false
	}
	repo, ok := ir.d.leiste(r.Context(), false).Repo(nwo)
	if !ok {
		writeJSONStatus(w, http.StatusNotFound, map[string]any{"fehler": "unbekanntes Repo " + nwo})
		return appctx.LeistenRepo{}, patClient{}, false
	}
	kand := kandidaten(repo)
	if len(kand) == 0 {
		writeJSONStatus(w, http.StatusServiceUnavailable, map[string]any{"fehler": "kein PAT gesetzt (" + strings.Join(patVariablen, ", ") + ")"})
		return appctx.LeistenRepo{}, patClient{}, false
	}
	return repo, kand[0], true
}

// ergebnis protokolliert, verwirft bei Erfolg den Speicher und antwortet.
func (ir issueRoutes) ergebnis(w http.ResponseWriter, repo appctx.LeistenRepo, pc patClient, aktion string, nummer int, err error, antwort map[string]any) {
	if err != nil {
		logZeile("issue-schreiben: repo=%s pat=%s aktion=%s nummer=%d fehler=%q", repo.NWO, pc.pat, aktion, nummer, err.Error())
		writeJSONStatus(w, http.StatusBadGateway, map[string]any{"fehler": err.Error()})
		return
	}
	logZeile("issue-schreiben: repo=%s pat=%s aktion=%s nummer=%d ergebnis=ok", repo.NWO, pc.pat, aktion, nummer)
	ir.d.verwerfen(repo.NWO)
	antwort["ok"] = true
	writeJSON(w, antwort)
}

// faelligPruefen lehnt jedes Datum ausser JJJJ-MM-TT und leer ab.
func faelligPruefen(w http.ResponseWriter, f string) bool {
	if f != "" && !appctx.GueltigesDatum(f) {
		eingabeFehler(w, "faellig muss JJJJ-MM-TT oder leer sein, nicht %q", f)
		return false
	}
	return true
}

func issueKurz(i ghrest.Issue) map[string]any {
	return map[string]any{"nummer": i.Nummer, "titel": i.Titel, "url": i.URL, "zustand": i.Zustand}
}

func (ir issueRoutes) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeJSONStatus(w, http.StatusMethodNotAllowed, map[string]any{"fehler": r.Method + " wird hier nicht bedient: diese Routen schreiben, also nur POST"})
		return
	}
	if !csrfOkFuer(r) {
		writeJSONStatus(w, http.StatusForbidden, map[string]any{"fehler": "CSRF: fremde Origin blockiert"})
		return
	}
	ctx := r.Context()
	switch aktion := strings.TrimPrefix(r.URL.Path, issuePraefix); aktion {
	case "anlegen":
		wunsch, err := rumpfLesen[issueAnlegenWunsch](w, r)
		if err != nil {
			eingabeFehler(w, "%v", err)
			return
		}
		titel := strings.TrimSpace(wunsch.Titel)
		if titel == "" {
			eingabeFehler(w, "titel ist Pflicht")
			return
		}
		if !faelligPruefen(w, wunsch.Faellig) {
			return
		}
		text := wunsch.Text
		if wunsch.Faellig != "" {
			text = appctx.TextMitFaellig(text, wunsch.Faellig)
		}
		repo, pc, ok := ir.schreibRepo(w, r, wunsch.Repo)
		if !ok {
			return
		}
		i, err := pc.client.IssueAnlegen(ctx, repo.NWO, ghrest.NeuesIssue{Titel: titel, Text: text, Labels: wunsch.Labels, Zustaendig: wunsch.Zustaendig})
		ir.ergebnis(w, repo, pc, aktion, i.Nummer, err, map[string]any{"issue": issueKurz(i)})

	case "aendern":
		wunsch, err := rumpfLesen[issueAendernWunsch](w, r)
		if err != nil {
			eingabeFehler(w, "%v", err)
			return
		}
		if wunsch.Nummer <= 0 {
			eingabeFehler(w, "nummer ist Pflicht")
			return
		}
		if z := wunsch.Zustand; z != nil && *z != "open" && *z != "closed" {
			eingabeFehler(w, "zustand muss open oder closed sein, nicht %q", *z)
			return
		}
		if g := wunsch.Grund; g != nil && *g != "" && *g != "completed" && *g != "not_planned" {
			eingabeFehler(w, "grund muss completed oder not_planned sein, nicht %q", *g)
			return
		}
		if wunsch.Titel != nil && strings.TrimSpace(*wunsch.Titel) == "" {
			eingabeFehler(w, "titel darf nicht leer sein")
			return
		}
		if wunsch.Faellig != nil && !faelligPruefen(w, *wunsch.Faellig) {
			return
		}
		if m := wunsch.Meilenstein; m != nil && *m < 0 {
			eingabeFehler(w, "meilenstein muss eine Milestone-Nummer oder 0 sein, nicht %d", *m)
			return
		}
		a := ghrest.IssueAenderung{Titel: wunsch.Titel, Text: wunsch.Text, Zustand: wunsch.Zustand, Grund: wunsch.Grund,
			Labels: wunsch.Labels, Zustaendig: wunsch.Zustaendig, Meilenstein: wunsch.Meilenstein}
		if a == (ghrest.IssueAenderung{}) && wunsch.Faellig == nil {
			eingabeFehler(w, "nichts zu aendern")
			return
		}
		repo, pc, ok := ir.schreibRepo(w, r, wunsch.Repo)
		if !ok {
			return
		}
		if wunsch.Faellig != nil {
			basis := ""
			if wunsch.Text != nil {
				basis = *wunsch.Text
			} else {
				alt, err := pc.client.Issue(ctx, repo.NWO, wunsch.Nummer)
				if err != nil {
					ir.ergebnis(w, repo, pc, aktion, wunsch.Nummer, err, nil)
					return
				}
				basis = alt.Text
			}
			neu := appctx.TextMitFaellig(basis, *wunsch.Faellig)
			a.Text = &neu
		}
		i, err := pc.client.IssueAendern(ctx, repo.NWO, wunsch.Nummer, a)
		ir.ergebnis(w, repo, pc, aktion, wunsch.Nummer, err, map[string]any{"issue": issueKurz(i)})

	case "kommentieren":
		wunsch, err := rumpfLesen[issueKommentarWunsch](w, r)
		if err != nil {
			eingabeFehler(w, "%v", err)
			return
		}
		if wunsch.Nummer <= 0 || strings.TrimSpace(wunsch.Text) == "" {
			eingabeFehler(w, "nummer und text sind Pflicht")
			return
		}
		repo, pc, ok := ir.schreibRepo(w, r, wunsch.Repo)
		if !ok {
			return
		}
		k, err := pc.client.Kommentieren(ctx, repo.NWO, wunsch.Nummer, wunsch.Text)
		ir.ergebnis(w, repo, pc, aktion, wunsch.Nummer, err, map[string]any{"kommentar": k})

	case "blocker":
		wunsch, err := rumpfLesen[issueBlockerWunsch](w, r)
		if err != nil {
			eingabeFehler(w, "%v", err)
			return
		}
		if wunsch.Nummer <= 0 || wunsch.Blocker <= 0 {
			eingabeFehler(w, "nummer und blocker sind Pflicht")
			return
		}
		if wunsch.Nummer == wunsch.Blocker {
			eingabeFehler(w, "ein Issue kann sich nicht selbst blockieren")
			return
		}
		repo, pc, ok := ir.schreibRepo(w, r, wunsch.Repo)
		if !ok {
			return
		}
		if wunsch.Entfernen {
			err = pc.client.BlockerLoesen(ctx, repo.NWO, wunsch.Nummer, wunsch.Blocker)
		} else {
			err = pc.client.BlockerSetzen(ctx, repo.NWO, wunsch.Nummer, wunsch.Blocker)
		}
		ir.ergebnis(w, repo, pc, aktion, wunsch.Nummer, err, map[string]any{})

	default:
		writeJSONStatus(w, http.StatusNotFound, map[string]any{"fehler": fmt.Sprintf("unbekannte Schreiboperation %q", aktion)})
	}
}
