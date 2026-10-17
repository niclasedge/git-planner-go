package appctx

// Rollen-Labels nur, wo das Repo sie nutzt (karte-gruppen, #121): "Rolle
// fehlt" gilt nur, wenn das Repo eines der Rollen-Labels angelegt hat; sonst
// gelten fuer ein Issue ohne Rolle die Regeln vor #102.

import (
	"testing"

	"github.com/niclasedge/git-planner-go/internal/collectors"
	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

func rollenSchalterEingabe(repoLabels []ghrest.Label, geholt bool) ArbeitEingabe {
	acht := mitLabels(8)
	acht.ParentNummer = 40
	return ArbeitEingabe{
		Repo:             "acme/alpha",
		Offen:            []ghrest.Issue{mitLabels(7, "bug"), mitLabels(10), acht, mitLabels(5, "question")},
		Changes:          []collectors.OpenspecChange{{Kennung: "zehn", Issue: 10, Aufgaben: aufgaben(0, 2)}},
		RepoLabels:       repoLabels,
		RepoLabelsGeholt: geholt,
	}
}

func TestRollenOhneRollenLabelsImRepo(t *testing.T) {
	je := eintragJe(RepoArbeitBauen(rollenSchalterEingabe([]ghrest.Label{{Name: "bug"}, {Name: "schaetzung:3"}}, true)))
	want := []struct {
		nr            int
		status, grund string
	}{
		{7, "ohne_change", "fehlt"},
		{10, "bereit", "fehlt"},
		{8, "hygiene", "Parent #40 nicht offen"},
		{5, "wartet_auf_mensch", "question"},
	}
	for _, w := range want {
		ein := je[w.nr]
		if ein.Status != w.status || grundImJSON(t, ein) != w.grund {
			t.Errorf("#%d Status %q grund %q, erwartet %q und %q", w.nr, ein.Status, grundImJSON(t, ein), w.status, w.grund)
		}
	}
}

func TestRollenMitRollenLabelImRepo(t *testing.T) {
	je := eintragJe(RepoArbeitBauen(rollenSchalterEingabe([]ghrest.Label{{Name: "bug"}, {Name: " Needs-Info "}}, true)))
	if ein := je[7]; ein.Status != ArbeitHygiene || grundImJSON(t, ein) != "Rolle fehlt" {
		t.Errorf("#7 Status %q grund %q, erwartet hygiene und Rolle fehlt", ein.Status, grundImJSON(t, ein))
	}
	// Nicht geholt (Abruf gescheitert oder alter Aufrufer): die Rollen-Regel gilt.
	je = eintragJe(RepoArbeitBauen(rollenSchalterEingabe(nil, false)))
	if ein := je[7]; ein.Status != ArbeitHygiene || grundImJSON(t, ein) != "Rolle fehlt" {
		t.Errorf("ohne Labels-Abruf #7 Status %q grund %q, erwartet hygiene und Rolle fehlt", ein.Status, grundImJSON(t, ein))
	}
	// Geholt, aber leer: das Repo hat keine Rollen-Labels.
	je = eintragJe(RepoArbeitBauen(rollenSchalterEingabe(nil, true)))
	if ein := je[7]; ein.Status != ArbeitOhneChange {
		t.Errorf("Repo ohne Labels #7 Status %q, erwartet ohne_change", ein.Status)
	}
}
