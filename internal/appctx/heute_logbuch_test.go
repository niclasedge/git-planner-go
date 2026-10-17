package appctx

// Heute mit den naechsten 30 Tagen und das Logbuch ueber alle Repos
// (heute-logbuch, #42).

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

func TestFensterAnwenden(t *testing.T) {
	jetzt := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	var issues []ghrest.Issue
	for i, d := range []int{1, 5, 30, 31, 60} {
		issues = append(issues, uIssue(i+1, "Fällig: "+jetzt.AddDate(0, 0, d).Format(DatumsFormat)))
	}
	voll := UebersichtBauen([]UebersichtRepo{{Repo: "acme/alpha", Issues: issues}}, jetzt)
	u := FensterAnwenden(voll, 30)
	if len(u.Demnaechst) != 3 || u.Fenster != 30 || u.Weitere != 2 {
		t.Errorf("30 Tage: %d Eintraege, fenster %d, weitere %d; erwartet 3, 30, 2", len(u.Demnaechst), u.Fenster, u.Weitere)
	}
	if len(voll.Demnaechst) != 5 {
		t.Errorf("FensterAnwenden hat die Eingabe veraendert: %d", len(voll.Demnaechst))
	}
	u = FensterAnwenden(voll, 60)
	if len(u.Demnaechst) != 5 || u.Weitere != 0 {
		t.Errorf("60 Tage: %d, weitere %d", len(u.Demnaechst), u.Weitere)
	}
	if u := FensterAnwenden(voll, 0); u.Fenster != 30 {
		t.Errorf("0 Tage: fenster %d, erwartet die Vorgabe 30", u.Fenster)
	}
	if u := FensterAnwenden(voll, 9999); u.Fenster != 365 {
		t.Errorf("9999 Tage: fenster %d, erwartet 365", u.Fenster)
	}
}

func TestIssueLogbuchBauen(t *testing.T) {
	ort := time.FixedZone("MESZ", 2*3600)
	jetzt := time.Date(2026, 9, 24, 15, 0, 0, 0, ort)
	zu := func(n int, wann time.Time) ghrest.Issue {
		return ghrest.Issue{Nummer: n, Titel: fmt.Sprint("t", n), Geschlossen: wann.UTC().Format(time.RFC3339)}
	}
	heute9 := time.Date(2026, 9, 24, 9, 0, 0, 0, ort)
	repos := []UebersichtRepo{
		{Repo: "acme/alpha", Issues: []ghrest.Issue{zu(3, heute9), zu(2, heute9.Add(2*time.Hour))}},
		{Repo: "acme/beta", Issues: []ghrest.Issue{zu(7, heute9.AddDate(0, 0, -1)), zu(8, heute9.AddDate(0, 0, -20)),
			{Nummer: 9, IstPR: true, Geschlossen: heute9.UTC().Format(time.RFC3339)}}},
		// Kurz nach Mitternacht Ortszeit ist in UTC noch gestern: zaehlt zu heute.
		{Repo: "acme/gamma", Issues: []ghrest.Issue{zu(1, time.Date(2026, 9, 24, 0, 30, 0, 0, ort))}},
	}
	l := IssueLogbuchBauen(repos, jetzt, 14)
	var tage []string
	for _, tg := range l.Tage {
		var es []string
		for _, e := range tg.Eintraege {
			es = append(es, fmt.Sprintf("%s#%d", strings.TrimPrefix(e.Repo, "acme/"), e.Nummer))
		}
		tage = append(tage, tg.Datum+"="+strings.Join(es, ","))
	}
	if got := strings.Join(tage, " "); got != "2026-09-24=alpha#2,alpha#3,gamma#1 2026-09-23=beta#7" {
		t.Errorf("Tage %s", got)
	}
	if l.Anzahl != 4 || l.Fenster != 14 {
		t.Errorf("anzahl %d, fenster %d", l.Anzahl, l.Fenster)
	}
	if b := IssueLogbuchBeginn(jetzt, 14); !b.Equal(time.Date(2026, 9, 11, 0, 0, 0, 0, ort)) {
		t.Errorf("Beginn %v, erwartet Mitternacht des 11.9.", b)
	}
	if l := IssueLogbuchBauen(nil, jetzt, 0); l.Fenster != 14 || l.Tage == nil {
		t.Errorf("leer %+v", l)
	}
}
