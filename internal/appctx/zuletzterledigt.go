package appctx

// "Zuletzt erledigt" mit Uhrzeit und gemergtem PR (zuletzt-erledigt-pr, #116).
// Beides rechnet Go; die Oberflaeche zeigt den Text und den Link nur.
//
// Die Schliesszeit folgt der Regel, mit der die Ansicht "Heute" bestimmt:
// Mitternacht des Tages in der Zone des Zeitpunkts jetzt (tagesAnfang,
// kalendertage in ansicht.go). Der PR kommt aus drei Regeln in fester Folge,
// der erste Treffer gilt, und nur gemergte PRs zaehlen.

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

// GemergtMit ist der PR, mit dem die Arbeit eines Issues gemergt wurde.
type GemergtMit struct {
	Nummer    int    `json:"nummer"`
	Titel     string `json:"titel"`
	URL       string `json:"url"`
	GemergtAm string `json:"gemergt_am"`
}

// IssueSchluss ist, was die Timeline eines Issues fuer die Zuordnung
// hergibt: die Ereignisse und je commit_id eines closed-Ereignisses die PRs
// aus commits/{sha}/pulls.
type IssueSchluss struct {
	Ereignisse []ghrest.TimelineEreignis
	CommitPRs  map[string][]ghrest.PR
}

// ZuletztAuswahl sind die Issues, die in zuletzt stehen: die ersten
// zuletztAnzahl ohne PRs. Der Dienst fragt nur fuer sie die Timeline.
func ZuletztAuswahl(geschlossen []ghrest.Issue) []ghrest.Issue {
	out := []ghrest.Issue{}
	for _, i := range geschlossen {
		if i.IstPR {
			continue
		}
		if len(out) == zuletztAnzahl {
			break
		}
		out = append(out, i)
	}
	return out
}

// monateKurz sind die Monatsabkuerzungen, die der Browser bisher fuer
// "de-DE" mit month: 'short' zeigte.
var monateKurz = [...]string{"Jan.", "Feb.", "März", "Apr.", "Mai", "Juni", "Juli", "Aug.", "Sept.", "Okt.", "Nov.", "Dez."}

// ZeitpunktText liefert zu einem Zeitpunkt den Text der Zeile ("heute
// 14:32" am Tag von jetzt, sonst "27. Sept.") und die volle Zeit fuer den
// Tooltip, beides in der Zone von jetzt. Ohne lesbare Zeit sind beide leer.
// Es rechnet die Schliesszeit in zuletzt (#116) und die letzte Aenderung
// in der Reihe (naechste-geaendert, #118).
func ZeitpunktText(zeitpunkt string, jetzt time.Time) (text, voll string) {
	t, ok := zeitAus(strings.TrimSpace(zeitpunkt))
	if !ok {
		return "", ""
	}
	t = t.In(jetzt.Location())
	voll = t.Format("02.01.2006 15:04")
	if kalendertage(jetzt, t) == 0 {
		return "heute " + t.Format("15:04"), voll
	}
	return fmt.Sprintf("%d. %s", t.Day(), monateKurz[t.Month()-1]), voll
}

// spaeter sagt, ob a nach b gemergt wurde; Unlesbares zaehlt als frueh.
func spaeter(a, b string) bool {
	ta, _ := zeitAus(a)
	tb, _ := zeitAus(b)
	return ta.After(tb)
}

func gemergtMitAus(p ghrest.PR) *GemergtMit {
	return &GemergtMit{Nummer: p.Nummer, Titel: p.Titel, URL: p.URL, GemergtAm: p.GemergtAm}
}

// juengster ist unter den PRs, die passt annimmt, der zuletzt gemergte.
func juengster(prs []ghrest.PR, passt func(ghrest.PR) bool) *ghrest.PR {
	var treffer *ghrest.PR
	for i := range prs {
		p := prs[i]
		if !p.Gemergt || !passt(p) {
			continue
		}
		if treffer == nil || spaeter(p.GemergtAm, treffer.GemergtAm) {
			treffer = &prs[i]
		}
	}
	return treffer
}

// schliessPR ist Regel 1: der PR, der das Issue laut Timeline geschlossen
// hat. Erst das juengste closed-Ereignis mit commit_id, dessen Commit in
// einem gemergten PR liegt; sonst der zuletzt gemergte PR desselben Repos,
// der als connected verknuepft ist oder als cross-referenced das Issue mit
// einem Schliesswort nennt.
func schliessPR(repo string, nr int, s IssueSchluss) *ghrest.PR {
	for i := len(s.Ereignisse) - 1; i >= 0; i-- {
		e := s.Ereignisse[i]
		if e.Ereignis != "closed" || e.CommitID == "" {
			continue
		}
		if p := juengster(s.CommitPRs[e.CommitID], func(ghrest.PR) bool { return true }); p != nil {
			return p
		}
	}
	var quellen []ghrest.PR
	for _, e := range s.Ereignisse {
		if e.Quelle == nil || (e.Ereignis != "connected" && e.Ereignis != "cross-referenced") {
			continue
		}
		if e.QuelleRepo != "" && !strings.EqualFold(e.QuelleRepo, repo) {
			continue
		}
		if e.Ereignis == "cross-referenced" && !nenntSchliessend(*e.Quelle, nr) {
			continue
		}
		quellen = append(quellen, *e.Quelle)
	}
	return juengster(quellen, func(ghrest.PR) bool { return true })
}

func nenntSchliessend(p ghrest.PR, nr int) bool {
	for _, n := range nummernAus(schliesstRe, p.Titel+"\n"+p.Text) {
		if n == nr {
			return true
		}
	}
	return false
}

// GemergtMitFinden ordnet einem geschlossenen Issue den PR zu, mit dem seine
// Arbeit gemergt wurde; nil, wenn keine Regel greift. schluss ist die
// Timeline (nil: nicht geholt), kennung der Change am Issue, prs die zuletzt
// geschlossenen PRs.
func GemergtMitFinden(repo string, i ghrest.Issue, kennung string, schluss *IssueSchluss, prs []ghrest.PR) *GemergtMit {
	if schluss != nil {
		if p := schliessPR(repo, i.Nummer, *schluss); p != nil {
			return gemergtMitAus(*p)
		}
	}
	if kennung != "" {
		if p := juengster(prs, func(p ghrest.PR) bool { return p.Head == "change/"+kennung }); p != nil {
			return gemergtMitAus(*p)
		}
	}
	endung := "(#" + strconv.Itoa(i.Nummer) + ")"
	if p := juengster(prs, func(p ghrest.PR) bool {
		if !strings.HasSuffix(strings.TrimSpace(p.Titel), endung) {
			return false
		}
		// Nicht vor dem Erstellen des Issues gemergt.
		return i.Erstellt == "" || !spaeter(i.Erstellt, p.GemergtAm)
	}); p != nil {
		return gemergtMitAus(*p)
	}
	return nil
}

// zuletztErgaenzen traegt Schliesszeit und gemergten PR an einem Eintrag von
// zuletzt ein.
func zuletztErgaenzen(ein *ArbeitsEintrag, i ghrest.Issue, e ArbeitEingabe, jetzt time.Time) {
	ein.GeschlossenText, ein.GeschlossenVoll = ZeitpunktText(i.Geschlossen, jetzt)
	kennung := ""
	if ein.Change != nil {
		kennung = ein.Change.Kennung
	}
	var schluss *IssueSchluss
	if s, ok := e.Schluesse[i.Nummer]; ok {
		schluss = &s
	}
	ein.GemergtMit = GemergtMitFinden(e.Repo, i, kennung, schluss, e.GeschlossenePRs)
}
