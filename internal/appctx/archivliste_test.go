package appctx

// Die archivierten Changes als eigene Liste (archiv-liste, #56): Datum aus dem
// Ordnernamen, erledigt/gesamt, erster Bildbeleg als Vorschau, neueste
// zuerst. Ohne Browser und ohne Netz.

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/niclasedge/git-planner-go/internal/collectors"
)

func archivChange(ordner, kennung string, erledigt, gesamt int) collectors.OpenspecChange {
	return collectors.OpenspecChange{Kennung: kennung, Ordner: "openspec/changes/archive/" + ordner,
		Archiviert: true, Aufgaben: aufgaben(erledigt, gesamt), Gates: []string{"gate1", "gate2", "fertig"}}
}

func TestArchivListeNeuesteZuerstMitVorschau(t *testing.T) {
	neuB := archivChange("2026-09-25-neu-b", "neu-b", 2, 2)
	neuB.Issue = 7
	neuB.Belege = []collectors.OpenspecBeleg{
		{Stufe: "nachher", Art: "bild", Pfad: "belege/assets/0-weg.png", Fehlt: true},
		{Stufe: "nachher", Art: "bild", Pfad: "belege/assets/1-da.png", Bild: "/api/blob?sha=b2"},
		{Stufe: "nachher", Art: "bild", Pfad: "belege/assets/2-spaeter.png", Bild: "/api/blob?sha=b3"},
	}
	// Eine absolute Ordnerangabe wie beim lokalen Lesen zaehlt genauso.
	ohne := archivChange("ohne-datum", "ohne-datum", 0, 1)
	ohne.Ordner = "/home/x/repo/openspec/changes/archive/ohne-datum"
	offen := collectors.OpenspecChange{Kennung: "offen", Ordner: "openspec/changes/offen",
		Aufgaben: aufgaben(1, 3), Gates: []string{"gate1"}}

	w := RepoArbeitBauen(ArbeitEingabe{Repo: "acme/alpha", Changes: []collectors.OpenspecChange{
		archivChange("2026-09-24-alt", "alt", 1, 2), neuB, offen, ohne,
		archivChange("2026-09-25-neu-a", "neu-a", 0, 0),
	}})

	var folge []string
	for _, a := range w.Archiv {
		folge = append(folge, a.Kennung)
	}
	if strings.Join(folge, ",") != "neu-a,neu-b,alt,ohne-datum" {
		t.Fatalf("archiv = %v, erwartet neu-a,neu-b,alt,ohne-datum", folge)
	}
	b := w.Archiv[1]
	if b.Datum != "2026-09-25" || b.Issue != 7 || b.Erledigt != 2 || b.Gesamt != 2 {
		t.Errorf("neu-b = %+v", b)
	}
	if b.Vorschau == nil || b.Vorschau.Bild != "/api/blob?sha=b2" || b.Vorschau.Pfad != "belege/assets/1-da.png" {
		t.Errorf("neu-b vorschau = %+v, erwartet den Beleg mit /api/blob?sha=b2", b.Vorschau)
	}
	alt := w.Archiv[2]
	if alt.Datum != "2026-09-24" || alt.Erledigt != 1 || alt.Gesamt != 2 || alt.Vorschau != nil {
		t.Errorf("alt = %+v", alt)
	}
	od := w.Archiv[3]
	if od.Datum != "" || od.Vorschau != nil || od.Gesamt != 1 {
		t.Errorf("ohne-datum = %+v", od)
	}

	roh, err := json.Marshal(w)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(roh, &m); err != nil {
		t.Fatal(err)
	}
	liste, ok := m["archiv"].([]any)
	if !ok || len(liste) != 4 {
		t.Fatalf("JSON archiv = %v", m["archiv"])
	}
	erster := liste[1].(map[string]any)
	for _, feld := range []string{"kennung", "datum", "issue", "erledigt", "gesamt", "vorschau"} {
		if _, da := erster[feld]; !da {
			t.Errorf("JSON-Eintrag ohne Feld %q: %v", feld, erster)
		}
	}
	if _, da := liste[2].(map[string]any)["vorschau"]; da {
		t.Errorf("Eintrag ohne Bildbeleg traegt vorschau: %v", liste[2])
	}
}

func TestArchivListeLeer(t *testing.T) {
	w := RepoArbeitBauen(ArbeitEingabe{Repo: "acme/alpha", Changes: []collectors.OpenspecChange{
		{Kennung: "offen", Ordner: "openspec/changes/offen", Aufgaben: aufgaben(0, 1)},
	}})
	if w.Archiv == nil || len(w.Archiv) != 0 {
		t.Fatalf("archiv = %#v, erwartet leere Liste", w.Archiv)
	}
	roh, _ := json.Marshal(w)
	if !strings.Contains(string(roh), `"archiv":[]`) {
		t.Errorf("JSON ohne \"archiv\":[]: %s", roh)
	}
}
