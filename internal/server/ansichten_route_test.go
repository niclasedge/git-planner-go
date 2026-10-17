package server_test

// E2E-Test fuer GET /ansichten?project= (gkp-pf2). Gegen server.NewHandler und
// nicht gegen appctx allein, aus demselben Grund wie bei plan_svg_route_test.go
// nebenan: was hier schiefgehen kann, ist die VERDRAHTUNG — reservedSegments
// und die Reihenfolge der Zweige in NewHandler. Ein /ansichten, das als
// Projektname durchgeht, liefert eine HTML-Seite statt JSON, und ein Test am
// Paket appctx saehe davon nichts.

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/niclasedge/git-planner-go/internal/cache"
)

// reservedSegments und die Zweigreihenfolge sind ZWEI verschiedene Schutze, und
// nur der zweite laesst sich ueber /ansichten?project= messen: der Zweig
// pathname == "/ansichten" greift vorher, also bleibt die Route auch ohne den
// Eintrag in reservedSegments gruen. Gefunden durch die Gegenprobe — der erste
// Anlauf dieses Tests war nach Muster 1 in Spec §9.1 ein Leerlauftest.
//
// Gemessen wird deshalb die PFADform: /ansichten/irgendwas faellt ohne den
// Eintrag in die Projekt-Route durch und rendert eine leere Projektansicht
// namens "ansichten" — HTML mit Status 200 statt eines 404. Dieselbe Falle, die
// der Kommentar an favicon.ico beschreibt.
func TestAnsichtenIstKeinProjektname(t *testing.T) {
	h := newTestHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/ansichten/irgendwas", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code == http.StatusOK && strings.Contains(rec.Body.String(), "<html") {
		t.Errorf("/ansichten/irgendwas rendert eine Projektseite (Status %d) — der Name fehlt in reservedSegments", rec.Code)
	}
}

// Die Route ist der einzige Weg, auf dem GitHub-Aufgaben je in eine Ansicht
// kommen: die zweite Quelle kostet einen Netzaufruf und steht deshalb nicht auf
// dem Pfad, der ein Projekt oeffnet (gkp-isj). Kommt sie nicht als eigene Route
// an, erscheint keine GitHub-Zeile — und zwar ohne Fehlermeldung.
//
// MIT project= (gkp-92i): seit die Route ohne den Parameter das Aggregat ueber
// ALLE Projekte liefert (TestAnsichtenOhneProjectLiefertDasAggregat unten),
// braucht dieser Test den Parameter ausdruecklich, um weiterhin den
// EIN-Projekt-Pfad (samt github_leer) zu pruefen — sonst wuerde er, ohne es zu
// merken, denselben Zweig wie der Aggregat-Test messen.
//
// Das Cwd des Handlers ist ein leeres TempDir: kein .beads, kein origin-Remote,
// also kein Netzaufruf. Geprueft wird die Form und das Fail-open, nicht der
// Inhalt.
func TestAnsichtenRouteAntwortetJSON(t *testing.T) {
	cwd := t.TempDir()
	h := newTestHandlerWithCwd(t, cwd)
	projekt := filepath.Base(cwd)

	rec, body := getJSON(t, h, "/ansichten?project="+projekt)

	if rec.Code != http.StatusOK {
		t.Fatalf("Status %d, erwartet 200", rec.Code)
	}
	ans, ok := body["ansichten"].(map[string]any)
	if !ok {
		t.Fatalf("Feld ansichten fehlt oder ist %T: %v", body["ansichten"], body)
	}
	// Jede angemeldete Ansicht steht drin — auch die leeren. Eine FEHLENDE
	// Ansicht leerte im Browser die Liste ohne Begruendung; eine leere ist eine
	// Auskunft. Denselben Unterschied macht AnsichtenBauen im Seitenrumpf.
	if len(ans) == 0 {
		t.Error("keine einzige Ansicht im Rumpf — der Browser ersetzt damit seine Liste durch nichts")
	}
	for id, v := range ans {
		a, ok := v.(map[string]any)
		if !ok {
			t.Errorf("Ansicht %q ist %T", id, v)
			continue
		}
		if _, da := a["eintraege"]; !da {
			t.Errorf("Ansicht %q hat kein Feld eintraege", id)
		}
	}
	// Und die Zahlen der Navigation reisen mit (gkp-d52). Ohne sie bleibt die
	// Seitenleiste auf dem Stand des Seitenaufbaus stehen — gemessen "Jederzeit
	// 47" neben einem Listenkopf, der 57 sagte. Je Ansicht muss die Zahl die
	// Laenge IHRER Liste sein: das ist dieselbe Zahl, die der Browser in den
	// Listenkopf schreibt.
	navItems, _ := body["nav"].([]any)
	if len(navItems) != len(ans) {
		t.Fatalf("nav hat %d Eintraege, ansichten %d — die Navigation zaehlt dann eine "+
			"andere Menge als die Liste", len(navItems), len(ans))
	}
	for _, it := range navItems {
		m, _ := it.(map[string]any)
		id, _ := m["id"].(string)
		a, _ := ans[id].(map[string]any)
		if a == nil {
			t.Errorf("nav nennt %q, das in ansichten nicht vorkommt", id)
			continue
		}
		eintraege, _ := a["eintraege"].([]any)
		if want := strconv.Itoa(len(eintraege)); m["cnt"] != want {
			t.Errorf("nav %q sagt cnt=%v, die Liste hat %s Zeilen", id, m["cnt"], want)
		}
	}
	// Warum keine GitHub-Zeile dabei ist, muss in derselben Antwort stehen:
	// ohne das ist "dieses Repo hat keine Issues" von "GitHub war nicht
	// erreichbar" nicht zu unterscheiden, und die Liste saehe vollstaendig aus,
	// ohne es zu sein.
	grund, _ := body["github_leer"].(string)
	if grund == "" {
		t.Error("github_leer fehlt, obwohl keine GitHub-Aufgabe dabei ist — eine Luecke, die niemand nennt, sieht aus wie Vollstaendigkeit")
	}
}

// TestAnsichtenOhneProjectLiefertDasAggregat ist das Kernkriterium aus
// gkp-92i, Teil 1.1: OHNE project-Parameter antwortet die Route mit dem
// Aggregat ueber ALLE Projekte (appctx.AnsichtenAlleProjekte), nicht mit dem
// Ein-Projekt-Stand des aktiven cwd.
//
// CC_KNOWLEDGE_ROOTS zeigt auf ein LEERES TempDir — sonst wuerde dieser Test
// die echten Projekte unter $HOME/git dieser Maschine einsammeln: langsam,
// nicht deterministisch, und ein Test darf keine echte bd-Datenbank anfassen.
// Das aktive Projekt (cwd) selbst hat kein .beads/ — DiscoverProjectsIn nimmt
// es trotzdem auf (es ist immer dabei), und CollectBeads faellt dafuer sofort
// auf Available=false zurueck, ohne einen Subprozess zu starten (dieselbe
// Begruendung wie in ansichtenprojekte_test.go). Genau DAS liefert die
// unterscheidende Behauptung: ein leeres project_fehler waere der
// EIN-Projekt-Pfad, der hier NICHT laufen darf.
func TestAnsichtenOhneProjectLiefertDasAggregat(t *testing.T) {
	cache.ResetAnsichtenAggregatForTest()
	defer cache.ResetAnsichtenAggregatForTest()
	t.Setenv("CC_KNOWLEDGE_ROOTS", t.TempDir())

	cwd := t.TempDir()
	h := newTestHandlerWithCwd(t, cwd)

	rec, body := getJSON(t, h, "/ansichten")

	if rec.Code != http.StatusOK {
		t.Fatalf("Status %d, erwartet 200 (%v)", rec.Code, body)
	}
	if _, ok := body["ansichten"].(map[string]any); !ok {
		t.Fatalf("Feld ansichten fehlt oder ist %T: %v", body["ansichten"], body)
	}
	// github_leer gehoert NUR zum Ein-Projekt-Rumpf (AnsichtenAntwort) — seine
	// Anwesenheit hier hiesse, dass die Route trotz fehlendem project doch den
	// alten Zweig genommen hat.
	if _, da := body["github_leer"]; da {
		t.Errorf("github_leer steht in der Aggregat-Antwort — das Feld gehoert nur zum Ein-Projekt-Pfad: %v", body)
	}
	// project_fehler macht sichtbar, dass das aktive Projekt (ohne .beads/)
	// keine Karten beitragen konnte (Teil 3, Review-Befund 4) — OHNE dieses
	// Feld saehe die Antwort aus wie ein vollstaendiges, aber leeres Aggregat.
	fehler, ok := body["projekt_fehler"].([]any)
	if !ok || len(fehler) == 0 {
		t.Fatalf("projekt_fehler fehlt oder ist leer, obwohl das aktive Projekt keinen Aufgabenbaum hat: %v", body)
	}
}
