package appctx

// Review-Entscheidung, Stimmen, Marke, Titel und die Gruppierung der
// Review-Kommentare zu Threads (pr-reviews, #81), ohne Browser.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

func rvReview(id int64, autor, zustand, zeit string) ghrest.Review {
	return ghrest.Review{ID: id, Autor: autor, Zustand: zustand, Zeit: "2026-09-26T" + zeit + ":00Z"}
}

func rvStimmen(r ghrest.PRReviews) string {
	var s []string
	for _, st := range r.Stimmen {
		s = append(s, st.Login+":"+st.Zustand)
	}
	return strings.Join(s, ",")
}

func rvThreadIDs(ts []ghrest.ReviewThread) string {
	var s []string
	for _, t := range ts {
		s = append(s, fmt.Sprint(t.ID))
	}
	return strings.Join(s, ",")
}

func rvKommentarIDs(t ghrest.ReviewThread) string {
	var s []string
	for _, k := range t.Kommentare {
		s = append(s, fmt.Sprint(k.ID))
	}
	return strings.Join(s, ",")
}

func TestReviewsLetzteJePerson(t *testing.T) {
	// Absichtlich nicht nach Zeit geordnet: die Reihenfolge rechnet appctx.
	reviews := []ghrest.Review{
		rvReview(5, "bo", "APPROVED", "10:05"),
		rvReview(1, "ana", "APPROVED", "10:01"),
		rvReview(2, "bo", "CHANGES_REQUESTED", "10:02"),
		rvReview(3, "ana", "COMMENTED", "10:03"),
		rvReview(4, "cy", "DISMISSED", "10:04"),
		rvReview(6, "dd", "COMMENTED", "10:06"),
		rvReview(7, "ee", "PENDING", "10:07"),
	}
	r := ReviewsRechnen(reviews, nil, nil, nil, nil)
	if r.Entscheidung != ReviewGenehmigt || r.Marke != "✓" {
		t.Errorf("Entscheidung %q Marke %q", r.Entscheidung, r.Marke)
	}
	if got := rvStimmen(r); got != "ana:genehmigt,bo:genehmigt,cy:verworfen,dd:kommentiert" {
		t.Errorf("Stimmen %s", got)
	}
	if r.Stimmen[1].Zeit != "2026-09-26T10:05:00Z" {
		t.Errorf("bo: Zeit der letzten zaehlenden Review %q", r.Stimmen[1].Zeit)
	}
}

func TestReviewsVerworfenHebtAuf(t *testing.T) {
	// cy hat erst Aenderungen verlangt, die Review wurde danach verworfen:
	// GitHub fuehrt sie dann als DISMISSED an ihrer alten Stelle.
	r := ReviewsRechnen([]ghrest.Review{
		rvReview(1, "cy", "APPROVED", "09:00"),
		rvReview(2, "cy", "DISMISSED", "09:30"),
	}, nil, nil, nil, nil)
	if r.Entscheidung != ReviewOffen || r.Marke != "" || rvStimmen(r) != "cy:verworfen" {
		t.Errorf("verworfen: %q %q %s", r.Entscheidung, r.Marke, rvStimmen(r))
	}
}

func TestReviewsAenderungenGehenVor(t *testing.T) {
	r := ReviewsRechnen([]ghrest.Review{
		rvReview(1, "ana", "APPROVED", "10:00"),
		rvReview(2, "bo", "CHANGES_REQUESTED", "10:01"),
	}, []string{"cy"}, []string{"kern"}, []ghrest.ReviewKommentar{
		{ID: 11, Pfad: "a.go", Zeile: 1},
		{ID: 21, Pfad: "b.go", Zeile: 2},
	}, map[int64]ghrest.ThreadZustand{})
	if r.Entscheidung != ReviewAenderungen || r.Marke != "✎" {
		t.Errorf("Entscheidung %q Marke %q", r.Entscheidung, r.Marke)
	}
	if strings.Join(r.Angefordert, ",") != "cy" || strings.Join(r.AngefordertTeams, ",") != "kern" {
		t.Errorf("angefordert %v %v", r.Angefordert, r.AngefordertTeams)
	}
	if r.OffeneThreads != 2 || r.AufgeloesteThreads != 0 || !r.ThreadZustand {
		t.Errorf("Threads offen %d aufgeloest %d zustand %v", r.OffeneThreads, r.AufgeloesteThreads, r.ThreadZustand)
	}
	if want := "Änderungen verlangt von bo · genehmigt von ana · 2 offene Threads · angefragt: cy, kern"; r.Titel != want {
		t.Errorf("Titel %q, erwartet %q", r.Titel, want)
	}

	nur := ReviewsRechnen([]ghrest.Review{rvReview(1, "ana", "COMMENTED", "10:00")}, nil, nil, nil, nil)
	if nur.Entscheidung != ReviewOffen || nur.Marke != "" {
		t.Errorf("nur Kommentare: %q %q", nur.Entscheidung, nur.Marke)
	}
	if nur.ThreadZustand || nur.Threads == nil || nur.Stimmen == nil || nur.Angefordert == nil || nur.AngefordertTeams == nil {
		t.Errorf("leere Listen muessen [] sein und ohne Zustand thread_zustand false: %+v", nur)
	}
	if nur.Titel != "noch keine Entscheidung" {
		t.Errorf("Titel %q", nur.Titel)
	}
}

func TestThreadsBilden(t *testing.T) {
	hunk := "@@ -1,9 +1,9 @@\n z1\n z2\n z3\n z4\n z5\n z6\n z7\n-alt\n+neu"
	kommentare := []ghrest.ReviewKommentar{
		{ID: 13, AntwortAuf: 12, Autor: "ana", Text: "dritte", Zeit: "2026-09-26T10:03:00Z", Pfad: "b.go", Zeile: 9},
		{ID: 11, Autor: "bo", Text: "erste", Zeit: "2026-09-26T10:01:00Z", Pfad: "b.go", Zeile: 9, Diff: hunk},
		{ID: 12, AntwortAuf: 11, Autor: "cy", Text: "zweite", Zeit: "2026-09-26T10:02:00Z", Pfad: "b.go", Zeile: 9},
		{ID: 21, Autor: "bo", Text: "alt", Zeit: "2026-09-26T09:00:00Z", Pfad: "a.go", Veraltet: true, OriginalZeile: 3},
		{ID: 40, AntwortAuf: 39, Autor: "dd", Text: "Waise", Zeit: "2026-09-26T11:00:00Z", Pfad: "a.go", Zeile: 1},
	}
	zustand := map[int64]ghrest.ThreadZustand{
		11: {ID: "PRRT_11"},
		21: {ID: "PRRT_21", Aufgeloest: true},
	}
	ts := ThreadsBilden(kommentare, zustand)
	if got := rvThreadIDs(ts); got != "40,11,21" {
		t.Fatalf("Threads %s, erwartet 40,11,21", got)
	}
	if got := rvKommentarIDs(ts[1]); got != "11,12,13" {
		t.Errorf("Thread 11: Kommentare %s", got)
	}
	if ts[1].Pfad != "b.go" || ts[1].Zeile != 9 || ts[1].Veraltet || ts[1].Aufgeloest {
		t.Errorf("Thread 11 %+v", ts[1])
	}
	if want := " z5\n z6\n z7\n-alt\n+neu"; ts[1].Diff != want {
		t.Errorf("Diff %q, erwartet die letzten Zeilen %q", ts[1].Diff, want)
	}
	if a := ts[2]; a.ID != 21 || !a.Veraltet || a.Zeile != 3 || !a.Aufgeloest {
		t.Errorf("Thread 21 %+v", a)
	}
	if w := ts[0]; w.ID != 40 || rvKommentarIDs(w) != "40" || w.Aufgeloest {
		t.Errorf("Waise %+v", w)
	}

	r := ReviewsRechnen(nil, nil, nil, kommentare, zustand)
	if r.OffeneThreads != 2 || r.AufgeloesteThreads != 1 || rvThreadIDs(r.Threads) != "40,11,21" {
		t.Errorf("ReviewsRechnen: offen %d aufgeloest %d Threads %s", r.OffeneThreads, r.AufgeloesteThreads, rvThreadIDs(r.Threads))
	}
	if !strings.Contains(r.Titel, "2 offene Threads") {
		t.Errorf("Titel %q", r.Titel)
	}
	if eins := ReviewsRechnen(nil, nil, nil, kommentare[1:2], nil); !strings.Contains(eins.Titel, "1 offener Thread") {
		t.Errorf("Titel %q", eins.Titel)
	}
}
