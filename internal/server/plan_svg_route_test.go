package server_test

// E2E-Test fuer GET /plan-svg?project=&map= (gkp-3k6). Wie bei
// reportassets_route_test.go gegen server.NewHandler statt gegen den
// Collector allein: was hier schiefgehen kann, ist die Verdrahtung —
// reservedSegments und die Reihenfolge der Zweige in NewHandler. Ein
// /plan-svg, das als Projektname durchgeht, liefert eine Seite statt JSON,
// und der Collector-Test saehe davon nichts.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func getJSON(t *testing.T, h http.Handler, path string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("%s: keine JSON-Antwort (%v): %.200q", path, err, rec.Body.String())
	}
	return rec, body
}

// Der Pfad muss als eigene Route ankommen, nicht als Projekt namens
// "plan-svg". Das Cwd des Handlers ist ein leeres TempDir ohne .beads/, also
// ist "" die richtige Antwort — geprueft wird die Form, nicht der Inhalt.
func TestPlanSVGRouteAntwortetJSON(t *testing.T) {
	h := newTestHandler(t)

	rec, body := getJSON(t, h, "/plan-svg?map=x-aaa")

	if rec.Code != http.StatusOK {
		t.Fatalf("Status %d, erwartet 200", rec.Code)
	}
	svg, ok := body["svg"]
	if !ok {
		t.Fatalf("Feld svg fehlt: %v", body)
	}
	if _, ok := svg.(string); !ok {
		t.Errorf("svg ist %T, erwartet string", svg)
	}
}

// Ohne map-Parameter darf die Route nicht raten und nicht scheitern: leere
// Antwort, kein Unterprozess. Die Ansicht zeigt dann ihren Platzhalter.
func TestPlanSVGRouteOhneMapIstLeerUndOk(t *testing.T) {
	h := newTestHandler(t)

	rec, body := getJSON(t, h, "/plan-svg")

	if rec.Code != http.StatusOK {
		t.Fatalf("Status %d, erwartet 200", rec.Code)
	}
	if body["svg"] != "" {
		t.Errorf("svg=%v, erwartet leer", body["svg"])
	}
}
