package appctx

// Die Beleg-Galerie eines Repos (beleg-galerie, #54): alle Belege aller
// Changes, offen wie archiviert, juengster zuerst, mit Typ, Name, Bild-URL
// und Link. Ohne Netz, ohne Dateisystem.

import (
	"strings"
	"testing"

	"github.com/niclasedge/git-planner-go/internal/collectors"
)

func galerieChanges() []collectors.OpenspecChange {
	return []collectors.OpenspecChange{
		{Kennung: "neu", Ordner: "openspec/changes/neu", Ref: "change/neu", Belege: []collectors.OpenspecBeleg{
			{Zeit: "2026-09-25T10:00:00.000Z", Stufe: "nachher", Art: "graph", Pfad: "belege/artefacts/0-graph.dot"},
		}},
		{Kennung: "alt", Ordner: "openspec/changes/archive/2026-09-20-alt", Ref: "main", Archiviert: true, Belege: []collectors.OpenspecBeleg{
			{Zeit: "2026-09-20T08:00:00.000Z", Stufe: "nachher", Art: "bild", Pfad: "belege/assets/0-x.png", Bild: "/api/blob?name=0-x.png&repo=acme%2Falpha&sha=b-png"},
		}},
	}
}

func TestBelegGalerieFolgeUndTyp(t *testing.T) {
	g := BelegGalerieBauen("acme/alpha", galerieChanges())
	if g.Repo != "acme/alpha" {
		t.Errorf("Repo %q", g.Repo)
	}
	if len(g.Belege) != 2 {
		t.Fatalf("%d Belege, erwartet 2: %+v", len(g.Belege), g.Belege)
	}
	a, b := g.Belege[0], g.Belege[1]
	if a.Kennung != "neu" || a.Typ != "dot" || a.Name != "0-graph.dot" || a.Archiviert || a.Stufe != "nachher" || a.Art != "graph" {
		t.Errorf("erster Beleg %+v, erwartet neu/dot/0-graph.dot", a)
	}
	if b.Kennung != "alt" || b.Typ != "png" || b.Name != "0-x.png" || !b.Archiviert {
		t.Errorf("zweiter Beleg %+v, erwartet alt/png archiviert", b)
	}
	if a.Zeit != "2026-09-25T10:00:00.000Z" {
		t.Errorf("Zeit %q", a.Zeit)
	}
}

func TestBelegGalerieTypOhneEndung(t *testing.T) {
	cs := []collectors.OpenspecChange{{Kennung: "k", Belege: []collectors.OpenspecBeleg{
		{Zeit: "1", Pfad: "belege/artefacts/LOG"},
		{Zeit: "2", Pfad: "belege/artefacts/Ausgabe.TXT"},
	}}}
	g := BelegGalerieBauen("acme/alpha", cs)
	if len(g.Belege) != 2 || g.Belege[0].Typ != "txt" || g.Belege[1].Typ != "datei" {
		t.Errorf("Typen %+v, erwartet txt dann datei", g.Belege)
	}
}

func TestBelegGalerieDoppelt(t *testing.T) {
	b := collectors.OpenspecBeleg{Zeit: "2026-09-21T00:00:00Z", Stufe: "nachher", Art: "bild", Pfad: "belege/assets/0-a.png"}
	cs := []collectors.OpenspecChange{
		{Kennung: "a", Ordner: "openspec/changes/a", Ref: "change/a", Belege: []collectors.OpenspecBeleg{b}},
		{Kennung: "a", Ordner: "openspec/changes/a", Ref: "main", Belege: []collectors.OpenspecBeleg{b}},
	}
	if g := BelegGalerieBauen("acme/alpha", cs); len(g.Belege) != 1 {
		t.Errorf("%d Belege, erwartet 1: %+v", len(g.Belege), g.Belege)
	}
}

func TestBelegGalerieFehlt(t *testing.T) {
	cs := []collectors.OpenspecChange{{Kennung: "f", Ordner: "openspec/changes/f", Ref: "main", Belege: []collectors.OpenspecBeleg{
		{Zeit: "z", Stufe: "nachher", Art: "bild", Pfad: "belege/assets/0-f.png", Fehlt: true, Grund: "nicht erzeugt", Bild: "/api/blob?x"},
	}}}
	g := BelegGalerieBauen("acme/alpha", cs)
	if len(g.Belege) != 1 {
		t.Fatalf("%d Belege, erwartet 1", len(g.Belege))
	}
	e := g.Belege[0]
	if !e.Fehlt || e.Bild != "" || e.Zeigbar || e.Grund != "nicht erzeugt" {
		t.Errorf("fehlender Beleg %+v, erwartet fehlt ohne Bild-URL", e)
	}
}

func TestBelegGalerieBildUndLink(t *testing.T) {
	ordner := "openspec/changes/archive/2026-09-10-x"
	cs := []collectors.OpenspecChange{{Kennung: "x", Ordner: ordner, Ref: "main", Archiviert: true, Belege: []collectors.OpenspecBeleg{
		{Zeit: "2", Stufe: "nachher", Art: "bild", Pfad: "belege/assets/0-x.png", Bild: "/api/blob?sha=b-png"},
		{Zeit: "1", Stufe: "nachher", Art: "graph", Pfad: "belege/artefacts/0-x.dot"},
	}}}
	g := BelegGalerieBauen("acme/alpha", cs)
	if len(g.Belege) != 2 {
		t.Fatalf("%d Belege", len(g.Belege))
	}
	bild, dot := g.Belege[0], g.Belege[1]
	if !bild.Zeigbar || bild.Bild != "/api/blob?sha=b-png" {
		t.Errorf("Bildbeleg %+v, erwartet zeigbar mit URL", bild)
	}
	if dot.Zeigbar || dot.Bild != "" {
		t.Errorf(".dot-Beleg %+v, erwartet nicht zeigbar", dot)
	}
	basis := "https://github.com/acme/alpha/blob/main/" + ordner + "/"
	if bild.Link != basis+"belege/assets/0-x.png" {
		t.Errorf("Link %q", bild.Link)
	}
	if dot.Link != basis+"belege/artefacts/0-x.dot" {
		t.Errorf("Link %q", dot.Link)
	}
}

func TestBelegGalerieOhneRefKeinLink(t *testing.T) {
	cs := []collectors.OpenspecChange{{Kennung: "l", Ordner: "/abs/openspec/changes/l", Belege: []collectors.OpenspecBeleg{
		{Zeit: "1", Pfad: "belege/assets/0-l.png"},
	}}}
	g := BelegGalerieBauen("acme/alpha", cs)
	if len(g.Belege) != 1 || g.Belege[0].Link != "" || strings.Contains(g.Belege[0].Link, "github") {
		t.Errorf("ohne Ref und mit absolutem Ordner: %+v, erwartet keinen Link", g.Belege)
	}
}

func TestBelegGalerieLeer(t *testing.T) {
	g := BelegGalerieBauen("acme/alpha", nil)
	if g.Belege == nil || len(g.Belege) != 0 {
		t.Errorf("Belege %#v, erwartet leere Liste (JSON [])", g.Belege)
	}
}

// Erneut eingeliefert (beleg-bilder, #105): `just beleg` stellt jeder
// Einlieferung eine laufende Nummer voran. Dasselbe Bild eines Changes steht
// einmal, mit der juengsten Einlieferung; ein anderer Change behaelt seins.
func TestBelegGalerieErneutEingeliefert(t *testing.T) {
	cs := []collectors.OpenspecChange{
		{Kennung: "n", Ordner: "openspec/changes/n", Ref: "change/n", Belege: []collectors.OpenspecBeleg{
			{Zeit: "2026-09-25T05:00:00.000Z", Stufe: "nachher", Art: "graph", Pfad: "belege/artefacts/0-plan.svg", SHA: "b-alt", Bild: "/api/blob?sha=b-alt"},
			{Zeit: "2026-09-25T06:00:00.000Z", Stufe: "nachher", Art: "graph", Pfad: "belege/artefacts/1-plan.svg", SHA: "b-neu", Bild: "/api/blob?sha=b-neu"},
			{Zeit: "2026-09-25T04:00:00.000Z", Stufe: "vorher", Art: "bild", Pfad: "belege/assets/2-andere.png", SHA: "b-andere"},
		}},
		{Kennung: "m", Ordner: "openspec/changes/m", Ref: "change/m", Belege: []collectors.OpenspecBeleg{
			{Zeit: "2026-09-24T06:00:00.000Z", Stufe: "nachher", Art: "graph", Pfad: "belege/artefacts/0-plan.svg", SHA: "b-m"},
		}},
	}
	g := BelegGalerieBauen("acme/alpha", cs)
	var namen []string
	for _, b := range g.Belege {
		namen = append(namen, b.Kennung+"/"+b.Name+"@"+b.SHA)
	}
	if strings.Join(namen, ",") != "n/1-plan.svg@b-neu,n/2-andere.png@b-andere,m/0-plan.svg@b-m" {
		t.Errorf("Belege %v, erwartet n/1-plan.svg, n/2-andere.png, m/0-plan.svg", namen)
	}
	if g.Belege[0].Zeit != "2026-09-25T06:00:00.000Z" {
		t.Errorf("juengste Einlieferung soll gewinnen: %+v", g.Belege[0])
	}
}

// Die Reihenfolge der Einlieferungen im Journal entscheidet nicht: auch wenn
// die juengere zuerst steht, gewinnt sie.
func TestBelegGalerieErneutUmgekehrt(t *testing.T) {
	cs := []collectors.OpenspecChange{{Kennung: "n", Belege: []collectors.OpenspecBeleg{
		{Zeit: "2026-09-25T06:00:00.000Z", Pfad: "belege/assets/3-x.png", SHA: "b-neu"},
		{Zeit: "2026-09-25T05:00:00.000Z", Pfad: "belege/assets/x.png", SHA: "b-alt"},
	}}}
	g := BelegGalerieBauen("acme/alpha", cs)
	if len(g.Belege) != 1 || g.Belege[0].SHA != "b-neu" {
		t.Errorf("Belege %+v, erwartet nur 3-x.png mit b-neu", g.Belege)
	}
}
