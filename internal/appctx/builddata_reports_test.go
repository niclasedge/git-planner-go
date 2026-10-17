package appctx

import (
	"strings"
	"testing"

	"github.com/niclasedge/git-planner-go/internal/collectors"
	"github.com/niclasedge/git-planner-go/internal/reports"
)

// Die Zahl neben "Reports" in der Navigation (Belege und Luecken) ist ein
// VERBRAUCHER des Aufgabenbaums — und sie las nur die erste Ebene.
//
// Das ist die Fehlerklasse, die dieses Repo schon zweimal getroffen hat: seit
// gkp-1lp ist der Baum beliebig tief, die Struktur war gruen, und die
// Verbraucher stiegen nicht mit ab (knownBeadIDs, Other, ReadyCount,
// mapInProgress waren betroffen). Gefunden beim Bau des Schreib-Teilbaums
// (gkp-8a0), weil dessen Hinweis genau dazu auffordert, die Verbraucher zu
// suchen statt sich auf gruene Tests zu verlassen.
//
// Der Baum ist dreistufig, und die Luecke sitzt beim ENKEL: an einem
// zweistufigen Baum ist "alle Nachfahren" von "die direkten Kinder" nicht zu
// unterscheiden.
func TestNavZahlZaehltAuchEnkel(t *testing.T) {
	m := collectors.BeadsMap{ID: "map", Children: []collectors.BeadsSubBead{
		kind("a", "open",
			kind("b", "closed",
				// Der Enkel ist geschlossen und unbelegt: eine Luecke.
				kind("c", "closed"),
			),
		),
	}}
	// Ein Beleg fuer b, damit b selbst KEINE Luecke ist — die einzige Luecke
	// im Baum ist damit der Enkel c, und nur ein Abstieg findet sie.
	entries := []reports.Entry{{ID: "e1", Bead: "b", Kind: "done", At: "2026-09-13T10:00:00Z"}}
	view := ReportsView{Available: true, Maps: []ReportsMap{buildReportsMap(m, entries)}}

	data := BuildData(map[string]any{"cards": map[string]any{"reports": view}}, "project")

	item := navEintrag(t, data, "reports")
	cnt := aStr(item["cnt"])
	if !strings.Contains(cnt, "1 Lücke") {
		t.Errorf("cnt = %q, erwartet eine gemeldete Luecke — der geschlossene Enkel c ist unbelegt", cnt)
	}
	if item["dot"] != "r" {
		t.Errorf("dot = %v, erwartet \"r\" — eine Luecke faerbt die Navigation rot", item["dot"])
	}
	// Der Beleg von b haengt eine Ebene unter der ersten und muss mitgezaehlt
	// werden, sonst meldet die Navigation 0 Eintraege fuer eine Karte mit
	// Belegen.
	if !strings.HasPrefix(cnt, "1 ") {
		t.Errorf("cnt = %q, erwartet 1 Eintrag (der Beleg von b) vor der Luecke", cnt)
	}
}

// navEintrag sucht einen Navigationseintrag ueber alle Gruppen.
func navEintrag(t *testing.T, data map[string]any, id string) map[string]any {
	t.Helper()
	for _, g := range aArr(data["nav"]) {
		for _, raw := range aArr(aMap(g)["items"]) {
			it := aMap(raw)
			if aStr(it["id"]) == id {
				return it
			}
		}
	}
	t.Fatalf("Navigationseintrag %q fehlt", id)
	return nil
}
