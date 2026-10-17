package appctx

import (
	"encoding/json"
	"testing"

	"github.com/niclasedge/git-planner-go/internal/collectors"
	"github.com/niclasedge/git-planner-go/internal/reports"
)

// Fortschritt je Aufgabe ueber ALLE Nachfahren (gkp-b9n, Spec §6). Der
// Collector liefert seit gkp-1lp beliebige Tiefe; die Ansicht muss deshalb
// rekursiv rechnen statt nur die erste Ebene zu zaehlen. Jeder Baum hier ist
// mindestens DREISTUFIG — an einem zweistufigen ist "ueber alle Nachfahren"
// von "ueber die direkten Kinder" nicht zu unterscheiden, und genau diese
// Verwechslung ist der Defekt.

// kind baut einen Sub-bead mit seinen Unteraufgaben. Ohne Kinder bleibt
// Children nil und nicht [] — genau die Form, die der Collector mit
// `json:"children,omitempty"` liefert (gkp-1lp): die Ansicht muss mit einem
// FEHLENDEN Feld rechnen, nicht mit einer leeren Liste.
func kind(id, status string, kinder ...collectors.BeadsSubBead) collectors.BeadsSubBead {
	return collectors.BeadsSubBead{ID: id, Title: id, Status: status, Children: kinder}
}

// findeBead sucht im gebauten Baum, nicht nur auf der ersten Ebene — sonst
// wuerde ein Test, der einen Enkel meint, still an dessen Elternteil pruefen.
func findeBead(t *testing.T, beads []ReportsBead, id string) ReportsBead {
	t.Helper()
	if b, ok := sucheBead(beads, id); ok {
		return b
	}
	t.Fatalf("Bead %q steht nicht im Baum", id)
	return ReportsBead{}
}

func sucheBead(beads []ReportsBead, id string) (ReportsBead, bool) {
	for _, b := range beads {
		if b.ID == id {
			return b, true
		}
		if t, ok := sucheBead(b.Children, id); ok {
			return t, true
		}
	}
	return ReportsBead{}, false
}

// dreiEbenen: map > a > (b1 > (c1, c2), b2). Vier Nachfahren unter a, davon
// zwei auf der dritten Ebene.
func dreiEbenen() collectors.BeadsMap {
	return collectors.BeadsMap{ID: "map", Children: []collectors.BeadsSubBead{
		kind("a", "in_progress",
			kind("b1", "closed",
				kind("c1", "closed"),
				kind("c2", "closed"),
			),
			kind("b2", "open"),
		),
	}}
}

func TestBuildReportsMap_BaumBleibtVerschachtelt(t *testing.T) {
	got := buildReportsMap(dreiEbenen(), nil)

	if len(got.Beads) != 1 || got.Beads[0].ID != "a" {
		t.Fatalf("erste Ebene der Karte ist %v, erwartet genau [a]", beadIDs(got))
	}
	a := got.Beads[0]
	if len(a.Children) != 2 {
		t.Fatalf("a hat %d Unteraufgaben, erwartet 2", len(a.Children))
	}
	b1 := findeBead(t, got.Beads, "b1")
	if len(b1.Children) != 2 {
		t.Fatalf("b1 hat %d Unteraufgaben, erwartet 2 (c1, c2)", len(b1.Children))
	}
	c1 := findeBead(t, got.Beads, "c1")
	if c1.Title != "c1" || c1.Status != statusGeschlossen {
		t.Errorf("Enkel kommt beschaedigt an: %+v", c1)
	}
}

func TestBuildReportsMap_FortschrittZaehltAlleNachfahren(t *testing.T) {
	got := buildReportsMap(dreiEbenen(), nil)
	a := findeBead(t, got.Beads, "a")

	if a.Counts == nil {
		t.Fatalf("a hat Unteraufgaben und braucht Zahlen, bekam nil")
	}
	// Nur die direkten Kinder waeren 2 gesamt / 1 erledigt — genau der Defekt.
	if a.Counts.Total != 4 || a.Counts.Closed != 3 || a.Counts.Open != 1 {
		t.Errorf("a zaehlt %+v, erwartet Total=4 Closed=3 Open=1 ueber alle Nachfahren", *a.Counts)
	}
	b1 := findeBead(t, got.Beads, "b1")
	if b1.Counts == nil || b1.Counts.Total != 2 || b1.Counts.Closed != 2 {
		t.Errorf("b1 zaehlt %+v, erwartet Total=2 Closed=2", b1.Counts)
	}
}

// TestBuildReportsMap_AufgabeZaehltSichSelbstNichtMit nagelt die Entscheidung
// fest: der Balken einer Aufgabe zeigt ihre NACHFAHREN, nicht sich selbst.
// Sonst koennte er nie 100 % erreichen — eine Aufgabe wird geschlossen, WEIL
// ihre Unteraufgaben fertig sind, also stuende im Moment der Fertigstellung
// n/(n+1). Und die Karte zaehlt seit jeher ihre Kinder ohne sich selbst; zwei
// verschiedene Regeln auf zwei Ebenen waeren beim Lesen nicht zu merken.
func TestBuildReportsMap_AufgabeZaehltSichSelbstNichtMit(t *testing.T) {
	m := collectors.BeadsMap{ID: "map", Children: []collectors.BeadsSubBead{
		kind("a", "closed", kind("b", "closed", kind("c", "closed"))),
	}}
	a := findeBead(t, buildReportsMap(m, nil).Beads, "a")

	if a.Counts == nil {
		t.Fatalf("a hat Unteraufgaben und braucht Zahlen, bekam nil")
	}
	if a.Counts.Total != 2 {
		t.Errorf("a zaehlt Total=%d, erwartet 2 (b und c) — a selbst zaehlt nicht mit", a.Counts.Total)
	}
	if a.Counts.Closed != 2 {
		t.Errorf("a zaehlt Closed=%d, erwartet 2", a.Counts.Closed)
	}
}

// TestBuildReportsMap_BlattBekommtKeineZahlen: ein Balken, der nur 0 % oder
// 100 % zeigen kann, ist kein Fortschritt, sondern Rauschen. Die Ansicht
// entscheidet das nicht selbst — sie bekommt gar keine Zahlen. Beide Formen
// muessen gleich behandelt werden: fehlendes Feld (nil) UND leere Liste.
func TestBuildReportsMap_BlattBekommtKeineZahlen(t *testing.T) {
	m := collectors.BeadsMap{ID: "map", Children: []collectors.BeadsSubBead{
		{ID: "nil-kinder", Status: "open"},
		{ID: "leere-liste", Status: "open", Children: []collectors.BeadsSubBead{}},
	}}
	got := buildReportsMap(m, nil)
	for _, id := range []string{"nil-kinder", "leere-liste"} {
		b := findeBead(t, got.Beads, id)
		if b.Counts != nil {
			t.Errorf("%s ist ein Blatt und darf keine Zahlen tragen, bekam %+v", id, *b.Counts)
		}
	}
}

// TestBuildReportsMap_BlattSchicktWederChildrenNochCounts haelt die
// JSON-Sparsamkeit fest, aus demselben Grund wie das omitempty im Collector:
// bei 99 Beads und 3 Elternteilen waere ein "children":[] plus leerer
// "counts" an jedem Blatt Ballast in jeder Antwort.
func TestBuildReportsMap_BlattSchicktWederChildrenNochCounts(t *testing.T) {
	m := collectors.BeadsMap{ID: "map", Children: []collectors.BeadsSubBead{{ID: "blatt", Status: "open"}}}
	roh, err := json.Marshal(buildReportsMap(m, nil).Beads[0])
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var felder map[string]any
	if err := json.Unmarshal(roh, &felder); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	for _, feld := range []string{"children", "counts"} {
		if _, da := felder[feld]; da {
			t.Errorf("Blatt schickt %q mit: %s", feld, roh)
		}
	}
}

// TestBuildReportsMap_BelegstandBleibtNebenDemFortschritt: "wie viel ist
// fertig" und "wie viel davon ist belegt" sind zwei Fragen. Ein offener Bead
// mit Beleg zaehlt als belegt und trotzdem als offen; ein geschlossener ohne
// Beleg zaehlt als erledigt und trotzdem als Luecke. Waeren die beiden
// vermischt, wuerde genau diese Unterscheidung eingeebnet.
func TestBuildReportsMap_BelegstandBleibtNebenDemFortschritt(t *testing.T) {
	m := collectors.BeadsMap{ID: "map", Children: []collectors.BeadsSubBead{
		kind("a", "open",
			kind("b", "in_progress", kind("c", "closed")),
		),
	}}
	// b ist offen und hat einen Beleg, c ist geschlossen und hat keinen.
	got := buildReportsMap(m, []reports.Entry{entry("e1", "b", "done", "2026-09-13T10:00:00Z")})
	a := findeBead(t, got.Beads, "a")

	if a.Counts == nil {
		t.Fatalf("a hat Unteraufgaben und braucht Zahlen, bekam nil")
	}
	will := MapCounts{Total: 2, Open: 1, Closed: 1, Proven: 1, Gaps: 1}
	if *a.Counts != will {
		t.Errorf("a zaehlt %+v, erwartet %+v", *a.Counts, will)
	}
	if b := findeBead(t, got.Beads, "b"); !b.Proven || b.Gap {
		t.Errorf("b (offen, mit done-Beleg) ist Proven=%v Gap=%v, erwartet true/false", b.Proven, b.Gap)
	}
	if c := findeBead(t, got.Beads, "c"); c.Proven || !c.Gap {
		t.Errorf("c (geschlossen, ohne Beleg) ist Proven=%v Gap=%v, erwartet false/true", c.Proven, c.Gap)
	}
}

func TestBuildReportsMap_KartenzahlenUeberAlleNachfahren(t *testing.T) {
	got := buildReportsMap(dreiEbenen(), nil)
	// Alle fuenf Nachfahren: a, b1, b2, c1, c2. Geschlossen sind b1, c1, c2 —
	// und weil keiner von ihnen einen Beleg hat, sind dieselben drei Luecken.
	// Vor gkp-b9n zaehlte die Karte hier 1, naemlich a.
	will := MapCounts{Total: 5, Open: 2, Closed: 3, Gaps: 3}
	if got.Counts != will {
		t.Errorf("Karte zaehlt %+v, erwartet %+v", got.Counts, will)
	}
}

// TestBuildReportsMap_EnkelEintragLandetNichtInOther: "Other" ist fuer
// Eintraege gedacht, deren Bead die Karte nicht kennt. Mit einem Baum gehoert
// ein Enkel dazu — pruefte die Zuordnung nur die erste Ebene, wanderte der
// Beleg eines Enkels aus seinem Bead heraus in den Sammelbehaelter, und der
// Enkel saehe unbelegt aus, obwohl sein Beleg da ist.
func TestBuildReportsMap_EnkelEintragLandetNichtInOther(t *testing.T) {
	got := buildReportsMap(dreiEbenen(), []reports.Entry{entry("e1", "c1", "done", "2026-09-13T10:00:00Z")})

	if len(got.Other) != 0 {
		t.Errorf("Other enthaelt %d Eintraege, erwartet 0: %+v", len(got.Other), got.Other)
	}
	c1 := findeBead(t, got.Beads, "c1")
	if len(c1.Entries) != 1 || !c1.Proven {
		t.Errorf("c1 hat %d Eintraege, Proven=%v — erwartet 1 und true", len(c1.Entries), c1.Proven)
	}
}

func TestBuildReportsMap_StartklarZaehltAlleNachfahren(t *testing.T) {
	m := collectors.BeadsMap{ID: "map", Children: []collectors.BeadsSubBead{
		{ID: "a", Status: "open", Children: []collectors.BeadsSubBead{
			{ID: "b", Status: "open", Ready: true, Children: []collectors.BeadsSubBead{
				{ID: "c", Status: "open", Ready: true},
			}},
		}},
	}}
	if got := buildReportsMap(m, nil).ReadyCount; got != 2 {
		t.Errorf("ReadyCount=%d, erwartet 2 — startklar ist startklar, egal in welcher Tiefe", got)
	}
}

// TestMapInProgress_ZaehltAuchLaufendeEnkel ergaenzt
// TestMapInProgressZaehltAuchLaufendeKinder um die Tiefe: bd bewegt den
// Status eines Elternteils nicht, wenn ein Kind startet — auf jeder Ebene
// nicht, nicht nur auf der ersten.
func TestMapInProgress_ZaehltAuchLaufendeEnkel(t *testing.T) {
	m := collectors.BeadsMap{ID: "map", Status: "open", Children: []collectors.BeadsSubBead{
		kind("a", "open", kind("b", "open", kind("c", "in_progress"))),
	}}
	if !mapInProgress(m) {
		t.Error("mapInProgress=false, erwartet true: ein Enkel ist in Arbeit")
	}
}

// TestBuildReportsMap_LastChangeSteigtMitDemEnkel: die Sortierung zeigt das
// Neueste oben. Zaehlte eine Aufgabe nur ihre eigenen Zeitstempel, sackte sie
// unter ihre eigenen Unteraufgaben, sobald dort etwas passiert — der Ast
// waere in der Liste auseinandergerissen.
func TestBuildReportsMap_LastChangeSteigtMitDemEnkel(t *testing.T) {
	m := collectors.BeadsMap{ID: "map", UpdatedAt: "2026-09-01T00:00:00Z", Children: []collectors.BeadsSubBead{
		{ID: "a", Status: "open", UpdatedAt: "2026-09-02T00:00:00Z", Children: []collectors.BeadsSubBead{
			{ID: "b", Status: "open", UpdatedAt: "2026-09-03T00:00:00Z", Children: []collectors.BeadsSubBead{
				{ID: "c", Status: "open", UpdatedAt: "2026-09-09T00:00:00Z"},
			}},
		}},
	}}
	got := buildReportsMap(m, nil)
	if a := findeBead(t, got.Beads, "a"); a.LastChange != "2026-09-09T00:00:00Z" {
		t.Errorf("a.LastChange=%q, erwartet den Stempel des Enkels c", a.LastChange)
	}
	if got.LastChange != "2026-09-09T00:00:00Z" {
		t.Errorf("Karte LastChange=%q, erwartet den Stempel des Enkels c", got.LastChange)
	}
}

// TestKnownBeadIDs_KenntAuchEnkel: die Liste ist der Abgleich fuer
// Commit-Texte und Pull Requests. Fehlt ein Enkel darin, faellt jeder Commit,
// der genau ihn nennt, aus der Historie — und zwar lautlos.
func TestKnownBeadIDs_KenntAuchEnkel(t *testing.T) {
	maps := []collectors.BeadsMap{{ID: "map", Children: []collectors.BeadsSubBead{
		kind("a", "open", kind("b", "open", kind("c", "open"))),
	}}}
	ids := knownBeadIDs(maps, nil)
	will := map[string]bool{"map": true, "a": true, "b": true, "c": true}
	if len(ids) != len(will) {
		t.Fatalf("knownBeadIDs=%v, erwartet %d Ids", ids, len(will))
	}
	for _, id := range ids {
		if !will[id] {
			t.Errorf("unerwartete Id %q in %v", id, ids)
		}
	}
}
