package appctx

// Der letzte Commit ueber alle Branches (karte-einzeilig, #124), die oberste
// Zeile von "Letzte Aktionen". Quelle ist zuerst die Repo-Aktivitaet: das
// juengste Push- oder Merge-Ereignis auf einem Branch, seine Zeit ist die des
// Ereignisses, Betreff und Autor kommen aus dem Commit. Fehlt die Aktivitaet
// oder nennt sie nichts Passendes, gilt der Rueckfall ueber die Branch-Koepfe.
// Gewaehlt und beschriftet wird hier; der Browser zeichnet.

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

// RueckfallKoepfe ist die Zahl der Branches, deren Kopf der Rueckfall
// hoechstens per commits/{sha} nachholt.
const RueckfallKoepfe = 10

// betreffKurzZeichen ist die Laenge von betreff_kurz samt Auslassung.
const betreffKurzZeichen = 60

// aktivitaetArten sind die Ereignisse, die als Commit zaehlen, mit dem Wort
// fuer den Tooltip.
var aktivitaetArten = map[string]string{
	"push": "gepusht", "force_push": "gepusht",
	"pr_merge": "gemergt", "merge_queue_merge": "gemergt",
}

// LetzterPR ist der offene PR des Branches.
type LetzterPR struct {
	Nummer int    `json:"nummer"`
	URL    string `json:"url"`
	// Titel ist der Titel des PRs, wenn er bekannt ist (aktionen-zeitleiste, #128).
	Titel string `json:"titel,omitempty"`
}

// LetzterCommit ist die Zeile "Letzter Commit".
type LetzterCommit struct {
	Branch string `json:"branch"`
	// Standard sagt, ob Branch der Standard-Branch ist.
	Standard    bool   `json:"standard"`
	SHA         string `json:"sha"`
	Kurz        string `json:"kurz"`
	Betreff     string `json:"betreff"`
	BetreffKurz string `json:"betreff_kurz"`
	Autor       string `json:"autor"`
	// Zeit ist der Zeitpunkt des Ereignisses (Push oder Merge), im Rueckfall
	// die Commit-Zeit; Art ist gepusht, gemergt oder Commit.
	Zeit string `json:"zeit"`
	Art  string `json:"art"`
	// Text und Voll sind Zeit nach ZeitpunktText, CommitVoll die
	// Commit-Zeit ebenso, Titel der Tooltip.
	Text       string     `json:"text"`
	Voll       string     `json:"voll"`
	CommitVoll string     `json:"commit_voll,omitempty"`
	Titel      string     `json:"titel"`
	PR         *LetzterPR `json:"pr,omitempty"`
}

// JuengsteAktivitaet waehlt aus der Aktivitaet den juengsten Push oder Merge
// auf einem Branch (refs/heads/); bei gleicher Zeit gilt der fruehere Eintrag
// der Liste.
func JuengsteAktivitaet(akt []ghrest.Aktivitaet) (ghrest.Aktivitaet, bool) {
	var treffer ghrest.Aktivitaet
	var zeit time.Time
	ok := false
	for _, a := range akt {
		if _, zaehlt := aktivitaetArten[a.Art]; !zaehlt || !strings.HasPrefix(a.Ref, "refs/heads/") || a.Nachher == "" {
			continue
		}
		t, _ := zeitAus(a.Zeit)
		if !ok || t.After(zeit) {
			treffer, zeit, ok = a, t, true
		}
	}
	return treffer, ok
}

// KoepfeFuerRueckfall sind die Branches, deren Kopf der Rueckfall aus dem
// Speicher oder per commits/{sha} braucht: die ersten RueckfallKoepfe ohne
// Zeit aus den Branch-Koepfen, der Standard-Branch zuerst, dann in der Folge
// der Branch-Liste.
func KoepfeFuerRueckfall(e ArbeitEingabe) []ghrest.Branch {
	var folge []ghrest.Branch
	for _, b := range e.Branches {
		if b.Name == e.Standard {
			folge = append([]ghrest.Branch{b}, folge...)
		} else {
			folge = append(folge, b)
		}
	}
	out := []ghrest.Branch{}
	for _, b := range folge {
		if len(out) == RueckfallKoepfe {
			break
		}
		if b.SHA == "" || e.BranchKoepfe[b.Name].Zeit != "" {
			continue
		}
		out = append(out, b)
	}
	return out
}

func betreffKurz(s string) string {
	if utf8.RuneCountInString(s) <= betreffKurzZeichen {
		return s
	}
	r := []rune(s)
	return strings.TrimSpace(string(r[:betreffKurzZeichen-1])) + "…"
}

// letzterCommitBauen baut die Zeile; nil, wenn es nichts zu zeigen gibt.
// prs sind die offenen PRs der Arbeitsliste.
func letzterCommitBauen(e ArbeitEingabe, prs []ghrest.PR, jetzt time.Time) *LetzterCommit {
	var l LetzterCommit
	var commitZeit string
	if a, ok := JuengsteAktivitaet(e.Aktivitaet); ok {
		k, da := e.Commits[a.Nachher]
		if !da {
			// Der Commit war nicht abrufbar; die Quelle commits nennt den Grund.
			return nil
		}
		l = LetzterCommit{Branch: strings.TrimPrefix(a.Ref, "refs/heads/"), SHA: a.Nachher, Betreff: k.Betreff,
			Autor: k.Autor, Zeit: a.Zeit, Art: aktivitaetArten[a.Art]}
		if l.Autor == "" {
			l.Autor = a.Akteur
		}
		commitZeit = k.Zeit
	} else {
		var beste *ghrest.Branch
		var besteZeit time.Time
		for i := range e.Branches {
			b := e.Branches[i]
			zeit, betreff, autor := "", "", ""
			if k := e.BranchKoepfe[b.Name]; k.Zeit != "" {
				zeit, betreff = k.Zeit, k.Betreff
			} else if k, da := e.Commits[b.SHA]; da && b.SHA != "" {
				zeit, betreff, autor = k.Zeit, k.Betreff, k.Autor
			}
			t, lesbar := zeitAus(zeit)
			if !lesbar {
				continue
			}
			if beste == nil || t.After(besteZeit) || (t.Equal(besteZeit) && b.Name == e.Standard) {
				beste, besteZeit = &e.Branches[i], t
				l = LetzterCommit{Branch: b.Name, SHA: b.SHA, Betreff: betreff, Autor: autor, Zeit: zeit, Art: "Commit"}
			}
		}
		if beste == nil {
			return nil
		}
		commitZeit = l.Zeit
	}
	l.Standard = l.Branch == e.Standard
	l.Kurz = l.SHA
	if len(l.Kurz) > 7 {
		l.Kurz = l.Kurz[:7]
	}
	l.BetreffKurz = betreffKurz(l.Betreff)
	l.Text, l.Voll = ZeitpunktText(l.Zeit, jetzt)
	_, l.CommitVoll = ZeitpunktText(commitZeit, jetzt)
	teile := []string{strings.TrimSpace(l.Art + " " + l.Voll)}
	if l.Art != "Commit" && l.CommitVoll != "" {
		teile = append(teile, "Commit "+l.CommitVoll)
	}
	l.Titel = strings.Join(append(teile, l.SHA), " · ")
	for _, p := range prs {
		if p.Head == l.Branch && (l.PR == nil || p.Nummer > l.PR.Nummer) {
			l.PR = &LetzterPR{Nummer: p.Nummer, URL: p.URL}
		}
	}
	return &l
}
