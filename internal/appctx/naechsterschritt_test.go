package appctx

// Der naechste Schritt je Change (naechster-schritt, #52): Lage, Satz und
// Befehl aus Archivstand, Gates, offenen Aufgaben, Pull Request und Issue.
// Ohne Browser und ohne Netz.

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/niclasedge/git-planner-go/internal/collectors"
	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

func TestNaechsterSchrittStufen(t *testing.T) {
	alle := []string{"gate1", "gate2", "fertig"}
	offen := []collectors.OpenspecAufgabe{{Text: "Test schreiben", Erledigt: true}, {Text: "Route bauen"}, {Text: "Doku"}}
	faelle := []struct {
		c      ChangeKurz
		pr     *ghrest.PR
		lage   string
		befehl string
		text   string
	}{
		{ChangeKurz{Kennung: "a", Aufgaben: aufgaben(0, 2)}, nil, "tun", "just gate1 a", ""},
		{ChangeKurz{Kennung: "b", Gates: []string{"gate1"}, Aufgaben: aufgaben(0, 2)}, nil, "tun", "just gate2 b", ""},
		{ChangeKurz{Kennung: "c", Gates: []string{"gate1", "gate2"}, Aufgaben: offen}, nil, "tun", "", "Route bauen"},
		{ChangeKurz{Kennung: "d", Gates: []string{"gate1", "gate2"}, Aufgaben: aufgaben(2, 2)}, nil, "tun", "just fertig d", ""},
		{ChangeKurz{Kennung: "e", Gates: alle, Aufgaben: aufgaben(2, 2)}, nil, "tun", "just change-pr e", ""},
		{ChangeKurz{Kennung: "f", Gates: alle, Aufgaben: aufgaben(2, 2)}, &ghrest.PR{Nummer: 70, Head: "change/f", Zustand: "open"},
			"warten", "", "#70"},
		{ChangeKurz{Kennung: "g", Gates: alle, Aufgaben: aufgaben(2, 2)}, &ghrest.PR{Nummer: 71, Head: "change/g", Zustand: "closed", Gemergt: true},
			"tun", "just archiv g", ""},
		{ChangeKurz{Kennung: "h", Archiviert: true, Gates: alle, Aufgaben: aufgaben(2, 2)}, nil, "ruhe", "", ""},
	}
	for _, f := range faelle {
		c := f.c
		s := NaechsterSchritt(&c, f.pr, false)
		if s == nil {
			t.Fatalf("%s: kein Schritt", c.Kennung)
		}
		if s.Lage != f.lage || s.Befehl != f.befehl {
			t.Errorf("%s: Lage %q Befehl %q, erwartet %q %q", c.Kennung, s.Lage, s.Befehl, f.lage, f.befehl)
		}
		if !strings.Contains(s.Text, c.Kennung) {
			t.Errorf("%s: Text %q nennt die Kennung nicht", c.Kennung, s.Text)
		}
		if f.text != "" && !strings.Contains(s.Text, f.text) {
			t.Errorf("%s: Text %q ohne %q", c.Kennung, s.Text, f.text)
		}
	}
}

func TestNaechsterSchrittLeererBefehlFehltImJSON(t *testing.T) {
	c := ChangeKurz{Kennung: "c", Gates: []string{"gate1", "gate2"}, Aufgaben: aufgaben(0, 1)}
	roh, err := json.Marshal(NaechsterSchritt(&c, nil, false))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(roh), "befehl") || !strings.Contains(string(roh), `"lage":"tun"`) {
		t.Errorf("JSON %s", roh)
	}
}

func TestNaechsterSchrittInDerArbeitsliste(t *testing.T) {
	e := ArbeitEingabe{
		Repo:  "acme/alpha",
		Offen: []ghrest.Issue{{Nummer: 3, Titel: "drei"}},
		Geschlossen: []ghrest.Issue{
			{Nummer: 4, Titel: "vier", Geschlossen: "2026-09-25T08:00:00Z"},
		},
		PRs: []ghrest.PR{{Nummer: 9, Titel: "drei (#3)", Head: "change/drei", Zustand: "open"}},
		Changes: []collectors.OpenspecChange{
			{Kennung: "drei", Issue: 3, Aufgaben: aufgaben(2, 2), Gates: []string{"gate1", "gate2", "fertig"}, RemoteBranch: true},
			{Kennung: "los", Aufgaben: aufgaben(0, 1)},
			{Kennung: "vier", Issue: 4, Aufgaben: aufgaben(1, 1), Gates: []string{"gate1", "gate2", "fertig"}},
		},
	}
	a := RepoArbeitBauen(e)
	if len(a.Reihe) != 1 || a.Reihe[0].Change == nil || a.Reihe[0].Change.Schritt == nil {
		t.Fatalf("Reihe %+v ohne Schritt", a.Reihe)
	}
	s := a.Reihe[0].Change.Schritt
	if s.Lage != "warten" || s.Befehl != "" || !strings.Contains(s.Text, "#9") {
		t.Errorf("#3: Schritt %+v, erwartet warten mit #9 und ohne Befehl", s)
	}
	var los *ChangeKurz
	for _, o := range a.OhneIssue {
		if o.Change != nil && o.Change.Kennung == "los" {
			los = o.Change
		}
	}
	if los == nil || los.Schritt == nil || los.Schritt.Befehl != "just gate1 los" {
		t.Errorf("los in ohne_issue: %+v", los)
	}
	if len(a.Zuletzt) != 1 || a.Zuletzt[0].Change == nil || a.Zuletzt[0].Change.Schritt == nil ||
		a.Zuletzt[0].Change.Schritt.Befehl != "just archiv vier" {
		t.Errorf("zuletzt: %+v", a.Zuletzt)
	}
}
