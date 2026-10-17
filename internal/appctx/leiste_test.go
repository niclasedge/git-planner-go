package appctx

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/niclasedge/git-planner-go/internal/areas"
)

// Die Szenarien der Leiste (projektliste-api, #11): Reihenfolge der Bereiche,
// ein Eintrag je Projekt, Zaehler je Projekt.

func TestLeisteReihenfolgeUndEindeutigkeit(t *testing.T) {
	projekte := []BereichProjekt{
		{Name: "gamma", Pfad: "/p/gamma"},
		{Name: "alpha", Pfad: "/p/alpha"},
		{Name: "beta", Pfad: "/p/beta"},
	}
	st := areas.State{
		Version:   1,
		Areas:     []string{"Arbeit", "Privat"},
		Zuordnung: map[string]string{"alpha": "Privat", "beta": "Arbeit"},
	}
	stand := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	zaehler := map[string]ProjektZaehler{"/p/alpha": {Offen: 2, InArbeit: 1}}

	l := LeisteBauen(projekte, st, zaehler, stand)

	var namen []string
	for _, b := range l.Bereiche {
		namen = append(namen, b.Name)
	}
	want := []string{"Arbeit", "Privat", areas.OhneBereich}
	if len(namen) != len(want) {
		t.Fatalf("Bereiche %v, erwartet %v", namen, want)
	}
	for i := range want {
		if namen[i] != want[i] {
			t.Fatalf("Bereiche %v, erwartet %v", namen, want)
		}
	}
	if l.Bereiche[0].Ohne || l.Bereiche[1].Ohne || !l.Bereiche[2].Ohne {
		t.Errorf("ohne-Flags falsch: %+v", l.Bereiche)
	}

	gesehen := map[string]int{}
	for _, b := range l.Bereiche {
		for _, p := range b.Projekte {
			gesehen[p.Pfad]++
		}
	}
	for _, p := range projekte {
		if gesehen[p.Pfad] != 1 {
			t.Errorf("Projekt %s steht %d mal in der Leiste, erwartet genau 1", p.Pfad, gesehen[p.Pfad])
		}
	}
	if got := l.Bereiche[1].Projekte[0]; got.Name != "alpha" || got.Zaehler.Offen != 2 || got.Zaehler.InArbeit != 1 {
		t.Errorf("alpha in Privat: %+v", got)
	}

	if _, err := time.Parse(time.RFC3339, l.Stand); err != nil {
		t.Errorf("stand %q ist kein RFC3339: %v", l.Stand, err)
	}

	// Die JSON-Form ist der Vertrag mit #12.
	b, _ := json.Marshal(l)
	var roh map[string]any
	if err := json.Unmarshal(b, &roh); err != nil {
		t.Fatal(err)
	}
	bereiche, ok := roh["bereiche"].([]any)
	if !ok || len(bereiche) != 3 {
		t.Fatalf("bereiche fehlt: %s", b)
	}
	erster := bereiche[0].(map[string]any)
	for _, k := range []string{"name", "ohne", "projekte"} {
		if _, ok := erster[k]; !ok {
			t.Errorf("Bereich ohne Feld %q: %s", k, b)
		}
	}
	p := bereiche[1].(map[string]any)["projekte"].([]any)[0].(map[string]any)
	for _, k := range []string{"name", "pfad", "zaehler"} {
		if _, ok := p[k]; !ok {
			t.Errorf("Projekt ohne Feld %q: %s", k, b)
		}
	}
	z := p["zaehler"].(map[string]any)
	for _, k := range []string{"offen", "in_arbeit", "ueberfaellig"} {
		if _, ok := z[k]; !ok {
			t.Errorf("zaehler ohne Feld %q: %s", k, b)
		}
	}
	if _, ok := roh["stand"].(string); !ok {
		t.Errorf("stand fehlt: %s", b)
	}
}

func TestLeisteZaehlerJeProjekt(t *testing.T) {
	jetzt := time.Date(2026, 9, 24, 10, 0, 0, 0, time.Local)
	gestern := "2026-09-23T09:00:00Z"
	karten := []QuellKarten{
		{Herkunft: HerkunftBeads, Maps: []ReportsMap{{
			ID: "lose", Loose: true, Status: "open",
			Beads: []ReportsBead{
				{ID: "b1", Status: "open", DueAt: gestern, Children: []ReportsBead{
					{ID: "b1.1", Status: "closed"},
				}},
			},
		}}},
		{Herkunft: HerkunftGithub, Maps: []ReportsMap{{
			ID: "org/repo", Title: "org/repo",
			Beads: []ReportsBead{
				{ID: "#1", Status: "open"},
				{ID: "#2", Status: "in_progress"},
			},
		}}},
	}

	z := LeistenZaehler(karten, jetzt)

	if z.Offen != 3 || z.InArbeit != 1 || z.Ueberfaellig != 1 {
		t.Errorf("zaehler %+v, erwartet offen 3, in_arbeit 1, ueberfaellig 1", z)
	}
}

// Eine echte Karte (ein Epic) ist selbst eine Aufgabe und zaehlt mit; die
// synthetischen Behaelter (lose Karte, GitHub-Karte ohne Status) nicht.
func TestLeisteZaehlerZaehltEchteKarten(t *testing.T) {
	jetzt := time.Date(2026, 9, 24, 10, 0, 0, 0, time.Local)
	karten := []QuellKarten{{Herkunft: HerkunftBeads, Maps: []ReportsMap{
		{ID: "epic", Status: "open", Beads: []ReportsBead{{ID: "e.1", Status: "open"}}},
		{ID: "zu", Status: "closed"},
	}}}

	z := LeistenZaehler(karten, jetzt)

	if z.Offen != 2 {
		t.Errorf("offen %d, erwartet 2 (Epic und Kind)", z.Offen)
	}
}
