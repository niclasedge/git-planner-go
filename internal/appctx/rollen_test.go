package appctx

// Rollen-Labels entscheiden frei (karte-rollen, #102): wie lage teilt die
// Arbeitsliste nach ready-for-agent, Menschen-Labels und Hygiene ein, und die
// Karte nennt nur noch den Status bereit auch bereit.

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/niclasedge/git-planner-go/internal/collectors"
	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

func mitLabels(nr int, namen ...string) ghrest.Issue {
	i := ghrest.Issue{Nummer: nr, Titel: "issue", Zustand: "open", Labels: []ghrest.Label{}}
	for _, n := range namen {
		i.Labels = append(i.Labels, ghrest.Label{Name: n})
	}
	return i
}

// grundImJSON liest grund so, wie die Antwort ihn ausliefert; "fehlt" heisst:
// das Feld steht nicht im JSON.
func grundImJSON(t *testing.T, ein ArbeitsEintrag) string {
	t.Helper()
	b, err := json.Marshal(ein)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	g, ok := m["grund"]
	if !ok {
		return "fehlt"
	}
	s, _ := g.(string)
	return s
}

func TestRollenTeilenEin(t *testing.T) {
	acht := mitLabels(8, "ready-for-agent")
	acht.ParentNummer = 40 // geschlossen: steht nicht unter den offenen
	neun := mitLabels(9, "ready-for-agent")
	neun.ParentNummer = 1
	e := ArbeitEingabe{
		Repo: "acme/alpha",
		Offen: []ghrest.Issue{
			mitLabels(1, "ready-for-agent"),
			mitLabels(2, " Ready-For-Agent "),
			mitLabels(3, "ready-for-agent", "in-arbeit"),
			mitLabels(4, "needs-info"),
			mitLabels(5, "question"),
			mitLabels(6, "ready-for-agent", "needs-triage"),
			mitLabels(7, "bug"),
			acht,
			neun,
		},
		Changes: []collectors.OpenspecChange{{Kennung: "zwei", Issue: 2, Aufgaben: aufgaben(0, 2)}},
	}
	je := eintragJe(RepoArbeitBauen(e))
	want := []struct {
		nr            int
		status, grund string
	}{
		{1, "ohne_change", "fehlt"},
		{2, "bereit", "fehlt"},
		{3, "in_arbeit", "fehlt"},
		{4, "wartet_auf_mensch", "needs-info"},
		{5, "wartet_auf_mensch", "question"},
		{6, "hygiene", "mehrere Rollen: ready-for-agent, needs-triage"},
		{7, "hygiene", "Rolle fehlt"},
		{8, "hygiene", "Parent #40 nicht offen"},
		{9, "ohne_change", "fehlt"},
	}
	for _, w := range want {
		ein, ok := je[w.nr]
		if !ok {
			t.Errorf("#%d fehlt in der Reihe", w.nr)
			continue
		}
		if ein.Status != w.status {
			t.Errorf("#%d Status %q, erwartet %q", w.nr, ein.Status, w.status)
		}
		if g := grundImJSON(t, ein); g != w.grund {
			t.Errorf("#%d grund %q, erwartet %q", w.nr, g, w.grund)
		}
	}
}

func TestRollenMehrereGruende(t *testing.T) {
	i := mitLabels(7, "bug")
	i.ParentNummer = 40
	je := eintragJe(RepoArbeitBauen(ArbeitEingabe{Repo: "acme/alpha", Offen: []ghrest.Issue{i}}))
	if s := je[7]; s.Status != "hygiene" || grundImJSON(t, s) != "Rolle fehlt; Parent #40 nicht offen" {
		t.Errorf("#7 %q grund %q, erwartet hygiene mit Rolle fehlt; Parent #40 nicht offen", s.Status, grundImJSON(t, s))
	}
}

func TestFrageUndFrageLabelSindEineRolle(t *testing.T) {
	je := eintragJe(RepoArbeitBauen(ArbeitEingabe{Repo: "acme/alpha",
		Offen: []ghrest.Issue{mitLabels(5, "question", "Frage")}}))
	if s := je[5]; s.Status != "wartet_auf_mensch" {
		t.Errorf("#5 Status %q, erwartet wartet_auf_mensch", s.Status)
	}
}

func TestBlockerUndChangeVorDerRolle(t *testing.T) {
	e := ArbeitEingabe{
		Repo:    "acme/alpha",
		Offen:   []ghrest.Issue{mitLabels(1, "needs-info"), mitLabels(2), mitLabels(3)},
		Blocker: map[int][]int{2: {1}},
		Changes: []collectors.OpenspecChange{{Kennung: "drei", Issue: 3, Aufgaben: aufgaben(1, 2)}},
	}
	je := eintragJe(RepoArbeitBauen(e))
	if s := je[2]; s.Status != ArbeitBlockiert || grundImJSON(t, s) != "fehlt" {
		t.Errorf("#2 %q grund %q, erwartet blockiert ohne grund", s.Status, grundImJSON(t, s))
	}
	if s := je[3]; s.Status != ArbeitInArbeit || grundImJSON(t, s) != "fehlt" {
		t.Errorf("#3 %q grund %q, erwartet in_arbeit ohne grund", s.Status, grundImJSON(t, s))
	}
}

func TestKarteBereitNurMitStatusBereit(t *testing.T) {
	var e KartenEingabe
	for nr, status := range []string{"ohne_change", "hygiene", "wartet_auf_mensch", "bereit"} {
		i := mitLabels(nr + 1)
		e.Offen = append(e.Offen, i)
		ein := issueEintrag(i)
		ein.Status = status
		e.Reihe = append(e.Reihe, ein)
	}
	k := KarteBauen(e)
	var naechste, bereit []string
	for _, s := range k.Stationen {
		if s.Zone == ZoneNaechste {
			naechste = append(naechste, string(rune('0'+s.Nummer)))
		}
		if s.Bereit {
			bereit = append(bereit, string(rune('0'+s.Nummer)))
		}
	}
	if strings.Join(naechste, ",") != "1,2,3,4" || strings.Join(bereit, ",") != "4" {
		t.Errorf("naechste %v, bereit %v; erwartet 1,2,3,4 und genau 4", naechste, bereit)
	}
}
