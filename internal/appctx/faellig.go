package appctx

// Faelligkeitsdatum im Issue-Text und die Ansichten Heute, Demnaechst,
// Irgendwann ueber alle Repos (faelligkeit, #39). Das Datum steht als letzte
// Zeile "Fällig: JJJJ-MM-TT" im Text des Issues — GitHub kennt kein Feld
// dafuer, und so bleibt es auch auf github.com lesbar.

import (
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

// Die Termine eines Eintrags.
const (
	TerminUeberfaellig = "ueberfaellig"
	TerminHeute        = "heute"
	TerminDemnaechst   = "demnaechst"
)

// DatumsFormat ist das Format der Faellig-Zeile und des Feldes faellig.
const DatumsFormat = "2006-01-02"

var faelligZeile = regexp.MustCompile(`^(?:Fällig|Faellig|Due):\s*(\d{4}-\d{2}-\d{2})\s*$`)

// GueltigesDatum meldet, ob s ein Datum im Format JJJJ-MM-TT ist.
func GueltigesDatum(s string) bool {
	_, err := time.Parse(DatumsFormat, s)
	return err == nil
}

// FaelligAus liest das Datum aus der letzten nicht leeren Zeile des Textes;
// "" heisst: kein Datum.
func FaelligAus(text string) string {
	zeilen := strings.Split(strings.TrimRight(strings.ReplaceAll(text, "\r\n", "\n"), " \t\n"), "\n")
	letzte := strings.TrimSpace(zeilen[len(zeilen)-1])
	m := faelligZeile.FindStringSubmatch(letzte)
	if m == nil || !GueltigesDatum(m[1]) {
		return ""
	}
	return m[1]
}

// TextMitFaellig entfernt alle Faellig-Zeilen am Textende und haengt, wenn
// datum nicht leer ist, "Fällig: datum" nach einer Leerzeile an.
func TextMitFaellig(text, datum string) string {
	zeilen := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	for len(zeilen) > 0 {
		z := strings.TrimSpace(zeilen[len(zeilen)-1])
		if z == "" || faelligZeile.MatchString(z) {
			zeilen = zeilen[:len(zeilen)-1]
			continue
		}
		break
	}
	rest := strings.Join(zeilen, "\n")
	if datum == "" {
		return rest
	}
	if rest == "" {
		return "Fällig: " + datum
	}
	return rest + "\n\nFällig: " + datum
}

// termin teilt ein Datum gegen den Tag von jetzt (Ortszeit von jetzt) ein und
// liefert den Abstand in Tagen.
func termin(datum string, jetzt time.Time) (string, int) {
	if datum == "" {
		return "", 0
	}
	d, err := time.Parse(DatumsFormat, datum)
	if err != nil {
		return "", 0
	}
	// Beide Tage als UTC-Mitternacht: der Abstand ist dann ein Vielfaches von
	// 24 Stunden, auch ueber eine Zeitumstellung hinweg.
	heute := time.Date(jetzt.Year(), jetzt.Month(), jetzt.Day(), 0, 0, 0, 0, time.UTC)
	tage := int(d.Sub(heute).Hours() / 24)
	switch {
	case tage < 0:
		return TerminUeberfaellig, tage
	case tage == 0:
		return TerminHeute, 0
	default:
		return TerminDemnaechst, tage
	}
}

// UebersichtRepo sind die offenen Issues eines Repos, bereits geholt.
type UebersichtRepo struct {
	Repo   string
	Issues []ghrest.Issue
}

// UebersichtEintrag ist eine Zeile von Heute, Demnaechst oder Irgendwann.
type UebersichtEintrag struct {
	Repo       string         `json:"repo"`
	Nummer     int            `json:"nummer"`
	Titel      string         `json:"titel"`
	URL        string         `json:"url"`
	Labels     []ghrest.Label `json:"labels"`
	Zustaendig []string       `json:"zustaendig"`
	Faellig    string         `json:"faellig"`
	Termin     string         `json:"termin"`
	Tage       int            `json:"tage"`
}

// UebersichtZaehler zaehlt die Eintraege je Termin.
type UebersichtZaehler struct {
	Ueberfaellig int `json:"ueberfaellig"`
	Heute        int `json:"heute"`
	Demnaechst   int `json:"demnaechst"`
	Irgendwann   int `json:"irgendwann"`
}

// Uebersicht ist die Antwort von GET /api/uebersicht.
type Uebersicht struct {
	Heute      []UebersichtEintrag `json:"heute"`
	Demnaechst []UebersichtEintrag `json:"demnaechst"`
	Irgendwann []UebersichtEintrag `json:"irgendwann"`
	Zaehler    UebersichtZaehler   `json:"zaehler"`
	// Fenster ist die Zahl der Tage, die demnaechst umfasst; Weitere die
	// Zahl spaeterer Eintraege (heute-logbuch, #42).
	Fenster int            `json:"fenster"`
	Weitere int            `json:"weitere"`
	Quellen []ArbeitQuelle `json:"quellen"`
	Sync    *SyncStand     `json:"sync,omitempty"`
}

// UebersichtBauen teilt die offenen Issues (ohne PRs) aller Repos ein:
// heute (ueberfaellig und heute, aeltestes Datum zuerst), demnaechst
// (aufsteigend) und irgendwann (ohne Datum, nach Repo und Nummer).
func UebersichtBauen(repos []UebersichtRepo, jetzt time.Time) Uebersicht {
	u := Uebersicht{Heute: []UebersichtEintrag{}, Demnaechst: []UebersichtEintrag{}, Irgendwann: []UebersichtEintrag{}, Quellen: []ArbeitQuelle{}}
	for _, r := range repos {
		for _, i := range r.Issues {
			if i.IstPR {
				continue
			}
			e := UebersichtEintrag{Repo: r.Repo, Nummer: i.Nummer, Titel: i.Titel, URL: i.URL,
				Labels: i.Labels, Zustaendig: i.Zustaendig, Faellig: FaelligAus(i.Text)}
			if e.Labels == nil {
				e.Labels = []ghrest.Label{}
			}
			if e.Zustaendig == nil {
				e.Zustaendig = []string{}
			}
			e.Termin, e.Tage = termin(e.Faellig, jetzt)
			switch e.Termin {
			case TerminUeberfaellig:
				u.Zaehler.Ueberfaellig++
				u.Heute = append(u.Heute, e)
			case TerminHeute:
				u.Zaehler.Heute++
				u.Heute = append(u.Heute, e)
			case TerminDemnaechst:
				u.Zaehler.Demnaechst++
				u.Demnaechst = append(u.Demnaechst, e)
			default:
				u.Zaehler.Irgendwann++
				u.Irgendwann = append(u.Irgendwann, e)
			}
		}
	}
	nachDatum := func(es []UebersichtEintrag) {
		sort.SliceStable(es, func(a, b int) bool {
			if es[a].Faellig != es[b].Faellig {
				return es[a].Faellig < es[b].Faellig
			}
			if es[a].Repo != es[b].Repo {
				return es[a].Repo < es[b].Repo
			}
			return es[a].Nummer < es[b].Nummer
		})
	}
	nachDatum(u.Heute)
	nachDatum(u.Demnaechst)
	sort.SliceStable(u.Irgendwann, func(a, b int) bool {
		if u.Irgendwann[a].Repo != u.Irgendwann[b].Repo {
			return u.Irgendwann[a].Repo < u.Irgendwann[b].Repo
		}
		return u.Irgendwann[a].Nummer < u.Irgendwann[b].Nummer
	})
	return u
}

// Das Fenster von demnaechst in Tagen (heute-logbuch, #42).
const (
	FensterVorgabe = 30
	FensterMax     = 365
)

// FensterAnwenden schneidet demnaechst auf die naechsten tage Tage (0 heisst
// Vorgabe, hoechstens FensterMax) und zaehlt, was danach kommt. Die Eingabe
// bleibt unveraendert.
func FensterAnwenden(u Uebersicht, tage int) Uebersicht {
	if tage <= 0 {
		tage = FensterVorgabe
	}
	tage = min(tage, FensterMax)
	innen := []UebersichtEintrag{}
	weitere := 0
	for _, e := range u.Demnaechst {
		if e.Tage <= tage {
			innen = append(innen, e)
		} else {
			weitere++
		}
	}
	u.Demnaechst, u.Fenster, u.Weitere = innen, tage, weitere
	return u
}
