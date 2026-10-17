package appctx

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Die Szenarien von GET /api/aufgaben (projektliste-api, #11), ohne Server
// und ohne Browser.

var paJetzt = time.Date(2026, 9, 24, 10, 0, 0, 0, time.Local)

func paBeads(maps ...ReportsMap) []QuellKarten {
	return []QuellKarten{{Herkunft: HerkunftBeads, Faehigkeiten: Faehigkeiten{Schreibbar: true, Termine: true, Belege: true}, Maps: maps}}
}

func paIDs(l ProjektAufgabenListe) []string {
	out := []string{}
	for _, e := range l.Eintraege {
		out = append(out, e.ID)
	}
	return out
}

func TestProjektAufgabenWurzelnOhneKinderinhalt(t *testing.T) {
	karten := paBeads(
		ReportsMap{ID: "k1", Title: "Karte eins", Status: "open", Beads: []ReportsBead{
			{ID: "k1.1", Status: "open"},
			{ID: "k1.2", Status: "in_progress"},
			{ID: "k1.3", Status: "closed"},
		}},
		ReportsMap{ID: "k2", Status: "closed"},
	)

	l, err := ProjektAufgaben(karten, "", paJetzt)
	if err != nil {
		t.Fatal(err)
	}
	if l.Eltern != "" {
		t.Errorf("eltern %q, erwartet leer", l.Eltern)
	}
	if got := paIDs(l); !reflect.DeepEqual(got, []string{"k1"}) {
		t.Fatalf("eintraege %v, erwartet [k1]", got)
	}
	e := l.Eintraege[0]
	if !e.HatKinder || e.KinderAnzahl != 2 {
		t.Errorf("hat_kinder %v kinder_anzahl %d, erwartet true/2", e.HatKinder, e.KinderAnzahl)
	}
	if e.Fortschritt != (Fortschritt{Erledigt: 1, Gesamt: 3}) {
		t.Errorf("fortschritt %+v, erwartet {1 3}", e.Fortschritt)
	}
	if e.Titel != "Karte eins" || e.Status != "open" || e.Herkunft != HerkunftBeads || !e.Schreibbar {
		t.Errorf("Zeile %+v", e)
	}

	b, _ := json.Marshal(l)
	var roh map[string]any
	if err := json.Unmarshal(b, &roh); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"projekt", "eltern", "eintraege", "quellen"} {
		if _, ok := roh[k]; !ok {
			t.Errorf("Antwort ohne Feld %q: %s", k, b)
		}
	}
	zeile := roh["eintraege"].([]any)[0].(map[string]any)
	for _, k := range []string{"id", "titel", "status", "herkunft", "url", "hat_kinder", "kinder_anzahl", "fortschritt", "frist", "schreibbar"} {
		if _, ok := zeile[k]; !ok {
			t.Errorf("Zeile ohne Feld %q: %s", k, b)
		}
	}
	for k := range zeile {
		if k == "kinder" || k == "children" || k == "beads" || k == "eintraege" {
			t.Errorf("Zeile traegt verschachtelte Kinder unter %q: %s", k, b)
		}
	}
	f := zeile["fortschritt"].(map[string]any)
	if _, ok := f["erledigt"]; !ok {
		t.Errorf("fortschritt ohne erledigt: %s", b)
	}
	if _, ok := f["gesamt"]; !ok {
		t.Errorf("fortschritt ohne gesamt: %s", b)
	}
}

func TestProjektAufgabenSortierungInGo(t *testing.T) {
	karten := paBeads(
		ReportsMap{ID: "w1", Status: "open"},
		ReportsMap{ID: "w2", Status: "in_progress"},
		ReportsMap{ID: "k", Status: "open", Beads: []ReportsBead{
			{ID: "a", Status: "open"},
			{ID: "b", Status: "open", DueAt: "2026-09-25T00:00:00Z"},
			{ID: "c", Status: "in_progress"},
		}},
	)

	kinder, err := ProjektAufgaben(karten, "k", paJetzt)
	if err != nil {
		t.Fatal(err)
	}
	if got := paIDs(kinder); !reflect.DeepEqual(got, []string{"c", "b", "a"}) {
		t.Errorf("Kinder %v, erwartet [c b a]", got)
	}
	if kinder.Eintraege[1].Frist != "2026-09-25" {
		t.Errorf("frist von b %q, erwartet 2026-09-25", kinder.Eintraege[1].Frist)
	}

	wurzeln, err := ProjektAufgaben(karten, "", paJetzt)
	if err != nil {
		t.Fatal(err)
	}
	// w2 in Arbeit zuerst; w1 und k sind beide offen ohne Frist und gleich
	// priorisiert, dort entscheidet die Id.
	if got := paIDs(wurzeln); !reflect.DeepEqual(got, []string{"w2", "k", "w1"}) {
		t.Errorf("Wurzeln %v, erwartet [w2 k w1]", got)
	}
}

func TestProjektAufgabenSchreibbarkeitAusDerQuelle(t *testing.T) {
	karten := []QuellKarten{{
		Herkunft: "fremd", Faehigkeiten: Faehigkeiten{Schreibbar: false},
		Maps: []ReportsMap{{ID: "f1", Title: "fremd", Status: "open"}},
	}}

	l, err := ProjektAufgaben(karten, "", paJetzt)
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Eintraege) != 1 {
		t.Fatalf("eintraege %v", paIDs(l))
	}
	if e := l.Eintraege[0]; e.Schreibbar || e.Herkunft != "fremd" {
		t.Errorf("Zeile %+v, erwartet schreibbar false und herkunft fremd", e)
	}
}

func TestProjektAufgabenNurDieDirekteEbene(t *testing.T) {
	karten := paBeads(ReportsMap{ID: "k1", Status: "open", Beads: []ReportsBead{
		{ID: "k1.1", Status: "open", Children: []ReportsBead{
			{ID: "k1.1.1", Status: "open"},
		}},
	}})

	l, err := ProjektAufgaben(karten, "k1", paJetzt)
	if err != nil {
		t.Fatal(err)
	}
	if l.Eltern != "k1" {
		t.Errorf("eltern %q, erwartet k1", l.Eltern)
	}
	if got := paIDs(l); !reflect.DeepEqual(got, []string{"k1.1"}) {
		t.Fatalf("eintraege %v, erwartet [k1.1]", got)
	}
	if e := l.Eintraege[0]; !e.HatKinder || e.KinderAnzahl != 1 {
		t.Errorf("k1.1: hat_kinder %v kinder_anzahl %d", e.HatKinder, e.KinderAnzahl)
	}

	enkel, err := ProjektAufgaben(karten, "k1.1", paJetzt)
	if err != nil {
		t.Fatal(err)
	}
	if got := paIDs(enkel); !reflect.DeepEqual(got, []string{"k1.1.1"}) {
		t.Errorf("Kinder von k1.1 %v, erwartet [k1.1.1]", got)
	}
}

func TestProjektAufgabenUnbekannteElternID(t *testing.T) {
	karten := paBeads(ReportsMap{ID: "k1", Status: "open"})

	_, err := ProjektAufgaben(karten, "x-999", paJetzt)
	if err == nil {
		t.Fatal("unbekannte eltern-Id ohne Fehler")
	}
	if !errors.Is(err, ErrUnbekannterEltern) {
		t.Errorf("Fehler %v ist nicht ErrUnbekannterEltern", err)
	}
	if err.Error() == "" {
		t.Error("Fehler ohne Grund")
	}
}

func TestProjektAufgabenQuelleOhneErgebnis(t *testing.T) {
	karten := append(paBeads(ReportsMap{ID: "k1", Status: "open"}),
		QuellKarten{Herkunft: HerkunftGithub, Grund: "kein Token"})

	l, err := ProjektAufgaben(karten, "", paJetzt)
	if err != nil {
		t.Fatal(err)
	}
	gefunden := false
	for _, q := range l.Quellen {
		if q == (QuellStand{Herkunft: "github", Grund: "kein Token"}) {
			gefunden = true
		}
	}
	if !gefunden {
		t.Errorf("quellen %+v ohne {github, kein Token}", l.Quellen)
	}
	if got := paIDs(l); !reflect.DeepEqual(got, []string{"k1"}) {
		t.Errorf("eintraege %v, erwartet [k1] trotz fehlender Quelle", got)
	}
	b, _ := json.Marshal(l.Quellen)
	if !strings.Contains(string(b), `{"herkunft":"github","grund":"kein Token"}`) {
		t.Errorf("quellen-JSON %s", b)
	}
}
