package appctx

// Die Kopfzahlen der Karte (karte-einzeilig, #124): die eingeklappte Karte ist
// eine Zeile, deren Zahlen und Nummern fertig aus Go kommen, als kopf an der
// Karte und als karte_kopf an der Arbeitsliste. Die Arbeitsliste baut sie ohne
// Sub-Issues; die Zahlen muessen trotzdem dieselben sein.

import (
	"encoding/json"
	"testing"

	"github.com/niclasedge/git-planner-go/internal/collectors"
	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

func kopfText(k KarteKopf) string {
	b, _ := json.Marshal(k)
	return string(b)
}

func TestKarteKopfSzenario(t *testing.T) {
	mit := KarteBauen(szenarioEingabe(t))
	ohneMaps := szenarioEingabe(t)
	ohneMaps.Maps = nil
	ohne := KarteBauen(ohneMaps)

	want := KarteKopf{Erledigt: 10, Aktiv: 3, Naechste: 10, Fragen: 3,
		P1: []KopfFrage{{Nummer: 88, Haelt: 4}, {Nummer: 91, Haelt: 2}}, Bereit: 110}
	for name, k := range map[string]Karte{"mit Maps": mit, "ohne Maps": ohne} {
		if got := KarteKopfAus(k); kopfText(got) != kopfText(want) {
			t.Errorf("%s: KarteKopfAus %s, erwartet %s", name, kopfText(got), kopfText(want))
		}
		if kopfText(k.Kopf) != kopfText(want) {
			t.Errorf("%s: Karte.Kopf %s, erwartet %s", name, kopfText(k.Kopf), kopfText(want))
		}
	}
}

func TestKarteKopfJSON(t *testing.T) {
	b, err := json.Marshal(KarteKopf{Erledigt: 2, Aktiv: 1, P1: []KopfFrage{}})
	if err != nil {
		t.Fatal(err)
	}
	if got := string(b); got != `{"erledigt":2,"aktiv":1,"naechste":0,"fragen":0,"p1":[]}` {
		t.Errorf("JSON %s: ohne bereit fehlt das Feld, p1 ist eine Liste", got)
	}
	b, _ = json.Marshal(KopfFrage{Nummer: 88, Haelt: 4})
	if string(b) != `{"nummer":88,"haelt":4}` {
		t.Errorf("KopfFrage %s", b)
	}
}

func TestArbeitKarteKopfOhneFragen(t *testing.T) {
	zu := func(nr int, am string) ghrest.Issue {
		return ghrest.Issue{Nummer: nr, Titel: "zu", URL: "u", Zustand: "closed", Geschlossen: am}
	}
	w := RepoArbeitBauen(ArbeitEingabe{Repo: "acme/alpha", Jetzt: ngJetzt,
		Offen: []ghrest.Issue{
			{Nummer: 5, Titel: "laeuft", URL: "u5", Zustand: "open"},
			{Nummer: 6, Titel: "bereit", URL: "u6", Zustand: "open", Labels: []ghrest.Label{{Name: RolleAgent}}},
		},
		Geschlossen: []ghrest.Issue{zu(2, "2026-09-27T10:00:00Z"), zu(1, "2026-09-26T10:00:00Z")},
		Changes: []collectors.OpenspecChange{{Kennung: "fuenf", Ordner: "openspec/changes/fuenf", Issue: 5,
			Aufgaben: []collectors.OpenspecAufgabe{{Text: "a", Erledigt: true}, {Text: "b"}}},
			// Bereit heisst nach den Rollen: ready-for-agent und ein Change.
			{Kennung: "sechs", Ordner: "openspec/changes/sechs", Issue: 6,
				Aufgaben: []collectors.OpenspecAufgabe{{Text: "a"}}}},
	})
	want := KarteKopf{Erledigt: 2, Aktiv: 1, Naechste: 1, Fragen: 0, P1: []KopfFrage{}, Bereit: 6}
	if kopfText(w.KarteKopf) != kopfText(want) {
		t.Errorf("karte_kopf %s, erwartet %s", kopfText(w.KarteKopf), kopfText(want))
	}
}

func TestArbeitKarteKopfMitFrage(t *testing.T) {
	w := RepoArbeitBauen(ArbeitEingabe{Repo: "acme/alpha", Jetzt: ngJetzt,
		Offen: []ghrest.Issue{
			{Nummer: 7, Titel: "Welcher PAT?", URL: "u7", Zustand: "open", Text: "Welcher PAT?", Labels: []ghrest.Label{{Name: "question"}}},
			{Nummer: 8, Titel: "wartet", URL: "u8", Zustand: "open", Blocker: 1, Labels: []ghrest.Label{{Name: RolleAgent}}},
		},
		Blocker: map[int][]int{8: {7}},
	})
	k := w.KarteKopf
	if k.Fragen != 1 || len(k.P1) != 1 || k.P1[0] != (KopfFrage{Nummer: 7, Haelt: 1}) {
		t.Errorf("karte_kopf %s, erwartet 1 Frage und p1 #7 haelt 1", kopfText(k))
	}
	if k.Bereit != 0 {
		t.Errorf("bereit %d, erwartet keins (#8 wartet auf die Frage)", k.Bereit)
	}
	if k.Naechste != 1 {
		t.Errorf("naechste %d, erwartet 1 (#8)", k.Naechste)
	}
}

func TestArbeitKarteKopfLeer(t *testing.T) {
	w := RepoArbeitBauen(ArbeitEingabe{Repo: "acme/alpha", Jetzt: ngJetzt})
	b, _ := json.Marshal(w)
	var roh map[string]json.RawMessage
	if err := json.Unmarshal(b, &roh); err != nil {
		t.Fatal(err)
	}
	if string(roh["karte_kopf"]) != `{"erledigt":0,"aktiv":0,"naechste":0,"fragen":0,"p1":[]}` {
		t.Errorf("karte_kopf leer %s", roh["karte_kopf"])
	}
	if _, da := roh["letzter_commit"]; da {
		t.Errorf("leeres Repo mit letzter_commit %s", roh["letzter_commit"])
	}
}
