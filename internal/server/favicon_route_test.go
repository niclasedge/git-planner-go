package server_test

// Routen-Test fuer das Tab-Icon. Der Punkt liegt nicht bei der SVG-Datei —
// die ist eingebettet und statisch —, sondern bei der Verdrahtung: /favicon.ico
// hat kein reservedSegments-Gegenstueck von Natur aus und faellt sonst in die
// Projekt-Route, die JEDEN unbekannten ersten Pfadteil als Projektnamen
// akzeptiert. Der Fehler waere dann eine 200 mit leerer Projektansicht statt
// eines Icons: kein Log, kein Absturz, nur ein leeres Tab.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func getRaw(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// Beide Namen, die ein Browser von sich aus probiert, muessen auf die
// eingebettete Datei zeigen — und zwar per Redirect, nicht als Projektseite.
func TestFaviconRoutenZeigenAufDasAsset(t *testing.T) {
	h := newTestHandler(t)

	for _, path := range []string{"/favicon.ico", "/favicon.svg"} {
		rec := getRaw(t, h, path)

		if rec.Code != http.StatusFound {
			t.Errorf("%s: Status %d, erwartet 302", path, rec.Code)
		}
		if loc := rec.Header().Get("Location"); loc != "/assets/favicon.svg" {
			t.Errorf("%s: Location %q, erwartet /assets/favicon.svg", path, loc)
		}
	}
}

// Das Ziel des Redirects muss auch wirklich ausgeliefert werden. Fehlte die
// Datei im Embed, waere das eine 404 — und der Redirect zeigte ins Leere.
func TestFaviconAssetWirdAusgeliefert(t *testing.T) {
	h := newTestHandler(t)

	rec := getRaw(t, h, "/assets/favicon.svg")

	if rec.Code != http.StatusOK {
		t.Fatalf("Status %d, erwartet 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "image/svg+xml") {
		t.Errorf("Content-Type %q, erwartet image/svg+xml", ct)
	}
	if body := rec.Body.String(); !strings.Contains(body, "<svg") {
		t.Errorf("Antwort ist kein SVG: %.80q", body)
	}
}
