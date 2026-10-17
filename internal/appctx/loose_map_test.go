package appctx

// Das Loose-Flag muss bis ins JSON durchkommen (gkp-ar7.14). Ginge es hier
// verloren, waere der Schaden nicht sichtbar leer, sondern falsch: die Ansicht
// zeichnete fuer die Sammelkarte einen Plan-Graphen, `bd graph` scheiterte an
// der erfundenen Id, und uebrig blieb "kein plan.svg (bd/dot nicht verfuegbar
// oder Map leer)" — eine Fehlermeldung ueber ein Werkzeug, das in Ordnung ist.

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/niclasedge/git-planner-go/internal/collectors"
	"github.com/niclasedge/git-planner-go/internal/reports"
)

func TestBuildReportsMapReichtLooseDurch(t *testing.T) {
	m := collectors.BeadsMap{
		ID:    collectors.LooseMapID,
		Title: collectors.LooseMapTitle,
		Loose: true,
		Children: []collectors.BeadsSubBead{
			{ID: "ws-2t5", Title: "Rueckfrage-Prozess", Status: "open", UpdatedAt: "2026-09-03T10:00:00Z"},
		},
	}

	out := buildReportsMap(m, []reports.Entry{})

	if !out.Loose {
		t.Error("Loose = false, erwartet true")
	}
	if out.Counts.Total != 1 {
		t.Errorf("Counts.Total = %d, erwartet 1 — die Zahlen zaehlen die losen Beads wie Sub-beads", out.Counts.Total)
	}
	// Die Karte hat kein updated_at, ihr Stempel kommt vom Kind.
	if out.LastChange != "2026-09-03T10:00:00Z" {
		t.Errorf("LastChange = %q, erwartet den Stempel des Kindes", out.LastChange)
	}

	b, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !strings.Contains(string(b), `"loose":true`) {
		t.Errorf(`"loose":true fehlt im JSON: %s`, b)
	}
}

// Die erfundene Id darf nicht in die Liste der bekannten Bead-Ids: gegen die
// prueft die Historie Commit-Texte, und "ohne-epic" steht in keinem Tracker.
// Die Kinder gehoeren hinein, die sind echt.
func TestKnownBeadIDsLaesstDieSammelkartenIdWeg(t *testing.T) {
	maps := []collectors.BeadsMap{{
		ID:       collectors.LooseMapID,
		Loose:    true,
		Children: []collectors.BeadsSubBead{{ID: "ws-2t5"}},
	}, {
		ID:       "ws-abc",
		Children: []collectors.BeadsSubBead{{ID: "ws-def"}},
	}}

	got := knownBeadIDs(maps, nil)

	for _, id := range got {
		if id == collectors.LooseMapID {
			t.Errorf("%q steht in den bekannten Ids: %v", collectors.LooseMapID, got)
		}
	}
	for _, muss := range []string{"ws-2t5", "ws-abc", "ws-def"} {
		found := false
		for _, id := range got {
			if id == muss {
				found = true
			}
		}
		if !found {
			t.Errorf("%q fehlt in %v", muss, got)
		}
	}
}

// Eine echte Karte darf das Flag NICHT tragen, sonst verliert jedes Epic
// seinen Plan-Graphen.
func TestBuildReportsMapEchteKarteIstNichtLoose(t *testing.T) {
	out := buildReportsMap(collectors.BeadsMap{ID: "ws-abc", IsMap: true}, []reports.Entry{})

	if out.Loose {
		t.Error("Loose = true bei einem echten Epic")
	}
	b, _ := json.Marshal(out)
	if strings.Contains(string(b), `"loose"`) {
		t.Errorf("loose steht im JSON, obwohl es false ist (omitempty fehlt?): %s", b)
	}
}
