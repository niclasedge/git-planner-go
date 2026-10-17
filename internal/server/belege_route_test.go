package server_test

// E2E-Test fuer GET /belege?project=&bead= (gkp-e9h). Gegen server.NewHandler
// und nicht gegen internal/reports allein, aus demselben Grund wie bei
// plan_svg_route_test.go und ansichten_route_test.go nebenan: was hier
// schiefgehen kann, ist die VERDRAHTUNG — reservedSegments und die
// Reihenfolge der Zweige in NewHandler.

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/niclasedge/git-planner-go/internal/assets"
	"github.com/niclasedge/git-planner-go/internal/render"
	"github.com/niclasedge/git-planner-go/internal/server"
)

// newTestHandlerWithCwd is newTestHandler (reportassets_route_test.go), just
// with a caller-chosen Cwd — /belege resolves the repo NAME from Cwd's
// basename (no git, no bd here), and the test needs to know that name in
// advance to plant a report file under it.
func newTestHandlerWithCwd(t *testing.T, cwd string) http.Handler {
	t.Helper()
	return server.NewHandler(server.AppOptions{
		Cwd:     cwd,
		Assets:  render.Assets{DashCSS: assets.DashCSS, BrowserCSS: assets.BrowserCSS, BrowserJS: assets.BrowserJS},
		AssetFS: assets.FS(),
	})
}

// writeBelegZeile plants one raw JSONL line under <reportsDir>/<repo>/<map>.jsonl
// — direct file I/O, not the MCP ingest path, since this test's whole point is
// that the route reads FILES and never shells out to bd.
func writeBelegZeile(t *testing.T, reportsDir, repo, mapName, bead, kind string) {
	t.Helper()
	dir := filepath.Join(reportsDir, repo)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	line := `{"id":"e-1","at":"2026-09-16T10:00:00Z","map":"` + mapName + `","bead":"` + bead + `","kind":"` + kind + `","title":"t"}` + "\n"
	f, err := os.OpenFile(filepath.Join(dir, mapName+".jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(line); err != nil {
		t.Fatal(err)
	}
}

// TestBelegeIstKeinProjektname ist dieselbe Gegenprobe wie
// TestAnsichtenIstKeinProjektname: reservedSegments und die Zweigreihenfolge
// sind zwei verschiedene Schutzeinrichtungen, und nur ein Pfad mit einem
// zweiten Segment misst reservedSegments allein — der Zweig
// pathname == "/belege" greift schon vorher fuer den blanken Pfad.
func TestBelegeIstKeinProjektname(t *testing.T) {
	h := newTestHandlerWithCwd(t, t.TempDir())

	req := httptest.NewRequest(http.MethodGet, "/belege/irgendwas", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code == http.StatusOK && strings.Contains(rec.Body.String(), "<html") {
		t.Errorf("/belege/irgendwas rendert eine Projektseite (Status %d) — der Name fehlt in reservedSegments", rec.Code)
	}
}

// TestBelegeRouteAntwortetMitEintraegen prueft die Form: die Route liefert
// genau die Eintraege des angefragten Beads, aus zwei verschiedenen Maps
// zusammengefuehrt, und keine fremden.
func TestBelegeRouteAntwortetMitEintraegen(t *testing.T) {
	reportsDir := t.TempDir()
	t.Setenv("CC_REPORTS_DIR", reportsDir)
	cwd := t.TempDir()
	repo := filepath.Base(cwd) // ReportsRepoName faellt ohne git auf den cwd-Basisnamen zurueck

	writeBelegZeile(t, reportsDir, repo, "map-a", "x-1", "plan")
	writeBelegZeile(t, reportsDir, repo, "map-b", "x-1", "done")
	writeBelegZeile(t, reportsDir, repo, "map-a", "x-2", "plan") // anderer Bead, darf nicht auftauchen

	h := newTestHandlerWithCwd(t, cwd)
	rec, body := getJSON(t, h, "/belege?bead=x-1")

	if rec.Code != http.StatusOK {
		t.Fatalf("Status %d, erwartet 200: %s", rec.Code, rec.Body.String())
	}
	entries, ok := body["entries"].([]any)
	if !ok {
		t.Fatalf("Feld entries fehlt oder ist %T: %v", body["entries"], body)
	}
	if len(entries) != 2 {
		t.Fatalf("erwartet 2 Eintraege fuer x-1, bekam %d: %v", len(entries), entries)
	}
	for _, e := range entries {
		m, _ := e.(map[string]any)
		if m["bead"] != "x-1" {
			t.Errorf("fremder Bead in der Antwort: %v", m)
		}
	}
}

// TestBelegeRouteUnbekanntesProjektUndBeadIstLeer prueft die Fail-open-Regel
// der Abnahme: ein unbekanntes Projekt bzw. ein unbekannter Bead antwortet
// mit einer leeren Liste, nie mit 500.
func TestBelegeRouteUnbekanntesProjektUndBeadIstLeer(t *testing.T) {
	t.Setenv("CC_REPORTS_DIR", t.TempDir())
	h := newTestHandlerWithCwd(t, t.TempDir())

	rec, body := getJSON(t, h, "/belege?project=hat-es-nicht&bead=hat-es-nicht")

	if rec.Code != http.StatusOK {
		t.Fatalf("Status %d, erwartet 200", rec.Code)
	}
	entries, ok := body["entries"].([]any)
	if !ok {
		t.Fatalf("Feld entries fehlt oder ist %T: %v", body["entries"], body)
	}
	if len(entries) != 0 {
		t.Fatalf("erwartet leere Liste, bekam %v", entries)
	}
}

// TestBelegeRouteReichtSkippedImFehlerfallDurch ist der Review-Befund 2
// (hoch->mittel, gkp-e9h): der Fehlerzweig der Route setzte "skipped" hart auf
// 0, obwohl reports.ListMaps ueber bereits gelesene Dateien summiert, BEVOR
// eine spaetere Datei mit echtem Lesefehler abbricht (store.go ListMaps) — der
// Wert kann im Fehlerfall > 0 sein.
//
// map-a liefert eine kaputte Zeile (skipped=1, kein Fehler), map-b ist ein
// Symlink auf sich selbst und loest beim Oeffnen den echten Fehler aus (ELOOP)
// — os.ReadDir listet alphabetisch, map-a wird also VOR dem Abbruch gelesen und
// ihr skipped-Wert muss im Fehlerergebnis ankommen.
//
// Bis #7 machte chmod 0000 die Datei unlesbar. Fuer root greift das nicht, und
// Cloud-Sitzungen laufen als root: der Test war dort rot, ohne dass die Route
// etwas falsch machte. Die Symlink-Schleife scheitert fuer jeden Benutzer, und
// sie ist kein "not exist" — readEntries behandelt sie also als echten Fehler.
func TestBelegeRouteReichtSkippedImFehlerfallDurch(t *testing.T) {
	reportsDir := t.TempDir()
	t.Setenv("CC_REPORTS_DIR", reportsDir)
	cwd := t.TempDir()
	repo := filepath.Base(cwd)

	dir := filepath.Join(reportsDir, repo)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "map-a.jsonl"), []byte("das ist keine JSON-Zeile\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mapB := filepath.Join(dir, "map-b.jsonl")
	if err := os.Symlink("map-b.jsonl", mapB); err != nil {
		t.Fatal(err)
	}
	if f, err := os.Open(mapB); err == nil {
		f.Close()
		t.Fatalf("Fixture taugt nicht: %s laesst sich oeffnen", mapB)
	}

	h := newTestHandlerWithCwd(t, cwd)
	rec, body := getJSON(t, h, "/belege?bead=x-1")

	if rec.Code != http.StatusOK {
		t.Fatalf("Status %d, erwartet 200 (fail open): %s", rec.Code, rec.Body.String())
	}
	if _, ok := body["error"]; !ok {
		t.Fatalf("Fixture taugt nicht: kein Fehler in der Antwort, body=%v", body)
	}
	skipped, ok := body["skipped"].(float64)
	if !ok {
		t.Fatalf("Feld skipped fehlt oder ist %T: %v", body["skipped"], body)
	}
	if skipped != 1 {
		t.Errorf("skipped = %v, erwartet 1 (aus map-a, VOR dem Fehler an map-b gelesen)", skipped)
	}
}

// TestBelegeRouteRuftKeinBdAuf ist der harte Test aus der Abnahme: mit leerem
// PATH kann weder git noch bd ausgefuehrt werden. ReportsRepoName faellt ohne
// git auf den cwd-Basisnamen zurueck (repoIdentity/RepoRoot, beide fail-open),
// und reports.LoadBead liest ausschliesslich Dateien — ruft die Route
// irgendwo CollectBeads/bd auf, bricht der Unterprozess, und die Route muss
// TROTZDEM die geplante Datei lesen, nicht mit einem Fehler antworten.
func TestBelegeRouteRuftKeinBdAuf(t *testing.T) {
	reportsDir := t.TempDir()
	t.Setenv("CC_REPORTS_DIR", reportsDir)
	cwd := t.TempDir()
	repo := filepath.Base(cwd)
	writeBelegZeile(t, reportsDir, repo, "map-a", "gkp-3sq", "done")

	t.Setenv("PATH", "") // git UND bd sind damit nicht mehr ausfuehrbar

	h := newTestHandlerWithCwd(t, cwd)
	rec, body := getJSON(t, h, "/belege?bead=gkp-3sq")

	if rec.Code != http.StatusOK {
		t.Fatalf("Status %d, erwartet 200: %s", rec.Code, rec.Body.String())
	}
	entries, ok := body["entries"].([]any)
	if !ok {
		t.Fatalf("Feld entries fehlt oder ist %T: %v", body["entries"], body)
	}
	if len(entries) != 1 {
		t.Fatalf("erwartet 1 Eintrag trotz leerem PATH, bekam %d: %v", len(entries), entries)
	}
}
