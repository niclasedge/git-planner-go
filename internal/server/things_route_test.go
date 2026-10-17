package server_test

// Routen-Test fuer die Things-Oberflaeche (things-oberflaeche, #12): die
// Huelle unter /things montiert das Svelte-Bundle, und die alte Ansicht unter
// / bleibt, wie sie ist. Gegen server.NewHandler statt gegen render allein,
// weil gerade die Verdrahtung (Reihenfolge der Routen, reservedSegments) die
// Stelle ist, an der ein unbekanntes Segment zum Projektnamen wird.

import (
	"net/http"
	"strings"
	"testing"

	"github.com/niclasedge/git-planner-go/internal/assets"
)

// Szenario "Huelle wird ausgeliefert".
func TestThingsHuelleWirdAusgeliefert(t *testing.T) {
	h := newTestHandler(t)

	rec := getRaw(t, h, "/things")

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /things: Status %d, erwartet 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Errorf("Content-Type %q, erwartet text/html", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `id="kn-svelte-root"`) {
		t.Errorf("Huelle ohne Knoten id=\"kn-svelte-root\":\n%s", body)
	}
	if !strings.Contains(body, `src="/assets/app/app.js"`) {
		t.Errorf("Huelle laedt /assets/app/app.js nicht:\n%s", body)
	}
	// Die Huelle ist nicht die alte Ansicht: browser.js gehoert nicht hinein.
	if strings.Contains(body, "window.DATA") {
		t.Error("Huelle traegt window.DATA — das ist die alte Ansicht, nicht die neue Seite")
	}
}

// Auch mit abschliessendem Schraegstrich ist /things keine Projektseite.
func TestThingsIstKeinProjektname(t *testing.T) {
	h := newTestHandler(t)

	rec := getRaw(t, h, "/things/")
	body := rec.Body.String()
	if strings.Contains(body, "window.DATA") {
		t.Errorf("GET /things/ rendert eine Projektseite (Status %d) — things fehlt in reservedSegments", rec.Code)
	}
}

// Szenario "Alte Ansicht bleibt": sie steht jetzt unter /ueberblick und
// unter /?project=X (repo-leiste, #25).
func TestAlteAnsichtBleibtOhneSvelteRoot(t *testing.T) {
	h := newTestHandler(t)

	for _, pfad := range []string{"/ueberblick", "/?project=irgendwas"} {
		rec := getRaw(t, h, pfad)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: Status %d, erwartet 200", pfad, rec.Code)
		}
		body := rec.Body.String()
		if !strings.Contains(body, assets.BrowserJS) {
			t.Errorf("GET %s enthaelt browser.js nicht — die alte Ansicht waere weg", pfad)
		}
		if strings.Contains(body, "kn-svelte-root") {
			t.Errorf("GET %s enthaelt kn-svelte-root — die neue Oberflaeche ist in die alte Ansicht geraten", pfad)
		}
	}
}

// Szenario "Startseite und Uebersicht": / ohne Parameter ist die Huelle.
func TestStartseiteIstDieThingsOberflaeche(t *testing.T) {
	h := newTestHandler(t)

	rec := getRaw(t, h, "/")
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, `id="kn-svelte-root"`) {
		t.Fatalf("GET /: Status %d, keine Huelle:\n%s", rec.Code, body)
	}
	if strings.Contains(body, "window.DATA") {
		t.Error("GET / traegt window.DATA — das ist die alte Ansicht")
	}
}
