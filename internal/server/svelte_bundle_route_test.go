package server_test

// Routen-Test fuer das Svelte-Bundle (gkp-a1k). Bewiesen wird hier die
// BAUSCHIENE, nicht die Ansicht: dass das von `just frontend` erzeugte
// Artefakt eingecheckt ist, per go:embed im Binary landet und ueber die
// bestehende /assets/-Route ausgeliefert wird.
//
// Warum ein Routen-Test und kein Blick ins Dateisystem: der Fehlerfall, den
// wir fuerchten, ist kein fehlendes File auf der Platte, sondern ein Binary,
// das ohne das Bundle gebaut wurde und trotzdem startet — die Seite laedt,
// nur der neue Teil bleibt leer. Genau die stille Halb-Funktion. Ein Test
// gegen den laufenden Handler schliesst das aus, ein os.Stat im Repo nicht.
//
// Das eingebettete Gegenstueck dazu ist assets.AppJS: ein //go:embed auf einen
// fehlenden Pfad ist ein COMPILE-Fehler, damit kann ein Binary ohne Bundle gar
// nicht erst entstehen. Der Test hier deckt die zweite Haelfte ab — dass die
// Datei auch verdrahtet ist.

import (
	"net/http"
	"strings"
	"testing"

	"github.com/niclasedge/git-planner-go/internal/assets"
)

// Der Marker steht als Klartext-Literal in der Svelte-Quelle (web/src/Hello.svelte)
// und ueberlebt das Minifizieren. Er belegt, dass die ausgelieferten Bytes
// wirklich aus dem Svelte-Bau stammen und nicht aus einer leeren Platzhalterdatei.
const svelteMarker = "gkp-a1k Svelte-Bauschiene"

func TestSvelteBundleWirdUeberAssetsRouteAusgeliefert(t *testing.T) {
	h := newTestHandler(t)

	rec := getRaw(t, h, "/assets/app/app.js")

	if rec.Code != http.StatusOK {
		t.Fatalf("Status %d, erwartet 200 — Bundle fehlt im Embed oder die Route greift nicht", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "javascript") {
		t.Errorf("Content-Type %q, erwartet ein JavaScript-MIME", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, svelteMarker) {
		t.Errorf("Marker %q fehlt im ausgelieferten Bundle (%d Bytes) — kommt das Artefakt wirklich aus dem Svelte-Bau?", svelteMarker, len(body))
	}
}

// Zweite Haelfte: die Bytes der Route und die Bytes der Konstante sind
// dieselben. Ohne das koennte assets.AppJS auf eine andere Datei zeigen als
// die, die /assets/app/app.js aus dem FS() liest — beide kaemen aus demselben
// Embed, aber aus verschiedenen Pfaden, und niemand faende es.
func TestAppJSUndRouteLiefernDieselbenBytes(t *testing.T) {
	h := newTestHandler(t)

	rec := getRaw(t, h, "/assets/app/app.js")
	if rec.Code != http.StatusOK {
		t.Fatalf("Status %d, erwartet 200", rec.Code)
	}
	if rec.Body.String() != assets.AppJS {
		t.Errorf("Route liefert %d Bytes, assets.AppJS hat %d — verschiedene Dateien", rec.Body.Len(), len(assets.AppJS))
	}
}

// Randbedingung 3 des Tickets: die alte Welt bleibt unveraendert erreichbar.
// browser.js ist eine IIFE ohne Abhaengigkeiten und wird weiterhin inline in
// die Seite geschrieben — der Svelte-Bau darf daran nichts aendern.
func TestAlteBrowserJSBleibtUnberuehrt(t *testing.T) {
	if len(assets.BrowserJS) == 0 {
		t.Fatal("assets.BrowserJS ist leer — die alte Ansicht waere weg")
	}
	if strings.Contains(assets.BrowserJS, svelteMarker) {
		t.Error("browser.js enthaelt den Svelte-Marker — die beiden Welten wurden vermischt")
	}
}
