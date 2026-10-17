package appctx

// Reviews je PR (pr-reviews, #81): aus den Reviews je Person die letzte
// zaehlende Stimme, daraus Entscheidung, Marke und Titel; aus den
// Review-Kommentaren die Threads, gruppiert ueber in_reply_to_id, offen vor
// aufgeloest. Die Oberflaeche zeichnet nur, was hier gerechnet wird.

import (
	"fmt"
	"sort"
	"strings"

	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

// Die Entscheidungen und Stimmen.
const (
	ReviewGenehmigt   = "genehmigt"
	ReviewAenderungen = "aenderungen"
	ReviewOffen       = "offen"
	ReviewVerworfen   = "verworfen"
	ReviewKommentiert = "kommentiert"
)

// diffZeilen ist die Zahl der Zeilen des diff_hunk, die ein Thread zeigt.
const diffZeilen = 5

// stimmenRechnen nimmt je Person die letzte zaehlende Review.
func stimmenRechnen(reviews []ghrest.Review) []ghrest.ReviewStimme {
	geordnet := append([]ghrest.Review(nil), reviews...)
	sort.SliceStable(geordnet, func(a, b int) bool {
		if geordnet[a].Zeit != geordnet[b].Zeit {
			return geordnet[a].Zeit < geordnet[b].Zeit
		}
		return geordnet[a].ID < geordnet[b].ID
	})
	je := map[string]ghrest.ReviewStimme{}
	for _, r := range geordnet {
		if r.Autor == "" {
			continue
		}
		var zustand string
		switch r.Zustand {
		case "APPROVED":
			zustand = ReviewGenehmigt
		case "CHANGES_REQUESTED":
			zustand = ReviewAenderungen
		case "DISMISSED":
			zustand = ReviewVerworfen
		case "COMMENTED":
			if _, hat := je[r.Autor]; !hat {
				je[r.Autor] = ghrest.ReviewStimme{Login: r.Autor, Zustand: ReviewKommentiert, Zeit: r.Zeit}
			}
			continue
		default:
			continue
		}
		je[r.Autor] = ghrest.ReviewStimme{Login: r.Autor, Zustand: zustand, Zeit: r.Zeit}
	}
	out := make([]ghrest.ReviewStimme, 0, len(je))
	for _, s := range je {
		out = append(out, s)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Login < out[b].Login })
	return out
}

func letzteZeilen(hunk string, n int) string {
	if hunk == "" {
		return ""
	}
	zeilen := strings.Split(strings.TrimRight(hunk, "\n"), "\n")
	if len(zeilen) > 0 && strings.HasPrefix(zeilen[0], "@@") {
		zeilen = zeilen[1:]
	}
	if len(zeilen) > n {
		zeilen = zeilen[len(zeilen)-n:]
	}
	return strings.Join(zeilen, "\n")
}

// ThreadsBilden gruppiert Review-Kommentare zu Threads. Eine Antwort landet
// im Thread ihres ersten Kommentars, auch ueber mehrere Stufen; fehlt der
// Bezug, bildet sie einen eigenen Thread. zustand ist nach der Id des ersten
// Kommentars geschluesselt; nil heisst: unbekannt, alle offen.
func ThreadsBilden(kommentare []ghrest.ReviewKommentar, zustand map[int64]ghrest.ThreadZustand) []ghrest.ReviewThread {
	je := map[int64]ghrest.ReviewKommentar{}
	for _, k := range kommentare {
		je[k.ID] = k
	}
	wurzel := func(k ghrest.ReviewKommentar) int64 {
		id := k.ID
		for i := 0; i < len(kommentare) && k.AntwortAuf != 0; i++ {
			v, ok := je[k.AntwortAuf]
			if !ok {
				break
			}
			k, id = v, v.ID
		}
		return id
	}
	threads := map[int64]*ghrest.ReviewThread{}
	var reihe []int64
	for _, k := range kommentare {
		w := wurzel(k)
		t, ok := threads[w]
		if !ok {
			r := je[w]
			t = &ghrest.ReviewThread{ID: w, Pfad: r.Pfad, Zeile: r.Zeile, StartZeile: r.StartZeile, Veraltet: r.Veraltet,
				Diff: letzteZeilen(r.Diff, diffZeilen), Aufgeloest: zustand[w].Aufgeloest, Kommentare: []ghrest.ReviewKommentar{}}
			if t.Veraltet {
				t.Zeile = r.OriginalZeile
			}
			threads[w] = t
			reihe = append(reihe, w)
		}
		t.Kommentare = append(t.Kommentare, k)
	}
	out := make([]ghrest.ReviewThread, 0, len(reihe))
	for _, w := range reihe {
		t := threads[w]
		sort.SliceStable(t.Kommentare, func(a, b int) bool {
			if t.Kommentare[a].Zeit != t.Kommentare[b].Zeit {
				return t.Kommentare[a].Zeit < t.Kommentare[b].Zeit
			}
			return t.Kommentare[a].ID < t.Kommentare[b].ID
		})
		out = append(out, *t)
	}
	sort.SliceStable(out, func(a, b int) bool {
		x, y := out[a], out[b]
		if x.Aufgeloest != y.Aufgeloest {
			return !x.Aufgeloest
		}
		if x.Pfad != y.Pfad {
			return x.Pfad < y.Pfad
		}
		if x.Zeile != y.Zeile {
			return x.Zeile < y.Zeile
		}
		return x.ID < y.ID
	})
	return out
}

func loginsVon(stimmen []ghrest.ReviewStimme, zustand string) []string {
	var out []string
	for _, s := range stimmen {
		if s.Zustand == zustand {
			out = append(out, s.Login)
		}
	}
	return out
}

// ReviewsRechnen fasst Reviews, angefragte Reviewer und Threads eines PRs
// zusammen. zustand nil heisst: GraphQL hat nichts geliefert.
func ReviewsRechnen(reviews []ghrest.Review, angefordert, teams []string, kommentare []ghrest.ReviewKommentar,
	zustand map[int64]ghrest.ThreadZustand) ghrest.PRReviews {
	out := ghrest.PRReviews{
		Stimmen:          stimmenRechnen(reviews),
		Angefordert:      append([]string{}, angefordert...),
		AngefordertTeams: append([]string{}, teams...),
		Threads:          ThreadsBilden(kommentare, zustand),
		ThreadZustand:    zustand != nil,
	}
	for _, t := range out.Threads {
		if t.Aufgeloest {
			out.AufgeloesteThreads++
		} else {
			out.OffeneThreads++
		}
	}
	aenderungen, genehmigt := loginsVon(out.Stimmen, ReviewAenderungen), loginsVon(out.Stimmen, ReviewGenehmigt)
	var teile []string
	switch {
	case len(aenderungen) > 0:
		out.Entscheidung, out.Marke = ReviewAenderungen, "✎"
	case len(genehmigt) > 0:
		out.Entscheidung, out.Marke = ReviewGenehmigt, "✓"
	default:
		out.Entscheidung = ReviewOffen
	}
	if len(aenderungen) > 0 {
		teile = append(teile, "Änderungen verlangt von "+strings.Join(aenderungen, ", "))
	}
	if len(genehmigt) > 0 {
		teile = append(teile, "genehmigt von "+strings.Join(genehmigt, ", "))
	}
	if len(teile) == 0 {
		teile = append(teile, "noch keine Entscheidung")
	}
	switch out.OffeneThreads {
	case 0:
	case 1:
		teile = append(teile, "1 offener Thread")
	default:
		teile = append(teile, fmt.Sprintf("%d offene Threads", out.OffeneThreads))
	}
	if alle := append(append([]string{}, out.Angefordert...), out.AngefordertTeams...); len(alle) > 0 {
		teile = append(teile, "angefragt: "+strings.Join(alle, ", "))
	}
	out.Titel = strings.Join(teile, " · ")
	return out
}
