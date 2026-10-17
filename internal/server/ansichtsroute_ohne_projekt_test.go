package server_test

// E2E-Test fuer GET /<view> ohne Projektsegment (gkp-92i, Teil 1.2) —
// /heute, /jederzeit, /demnaechst, /irgendwann, /eingang, /logbuch, /bereiche
// oeffnen die Sammelansicht ueber ALLE Projekte statt als Projektname gedeutet
// zu werden. Gegen server.NewHandler und nicht gegen appctx/render allein,
// aus demselben Grund wie die Nachbartests in dieser Datei: was hier
// schiefgehen kann, ist die VERDRAHTUNG (reservedSegments, die Zweigreihenfolge
// in NewHandler) — genau die Fehlerklasse, die der Kommentar an
// favicon.ico beschreibt ("ein Aufruf von /favicon.ico wuerde als Projekt
// gedeutet und eine leere Projektansicht rendern").

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestAnsichtSegmentOeffnetDieSammelansicht ist das Kernkriterium: /heute
// liefert eine HTML-Seite mit window.INITIAL_VIEW = "heute" und behandelt
// "heute" NICHT als Projektnamen. Eine Regression, die "heute" wieder als
// Projekt deutet, wuerde stattdessen window.INITIAL_VIEW leer lassen (kein
// zweites Pfadsegment mehr vorhanden) — genau das misst dieser Test.
func TestAnsichtSegmentOeffnetDieSammelansicht(t *testing.T) {
	h := newTestHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/heute", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Status = %d, erwartet 200 (%s)", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `window.INITIAL_VIEW = "heute"`) {
		t.Errorf("Antwort traegt kein window.INITIAL_VIEW = \"heute\" — /heute wurde vermutlich als Projektname gedeutet: %.400s", body)
	}
}

// TestAlleAnsichtSegmenteSindReserviert prueft, dass JEDE der sechs
// Regel-Ids plus "bereiche" dieselbe Behandlung bekommt — nicht nur die eine,
// die TestAnsichtSegmentOeffnetDieSammelansicht exemplarisch misst. Eine Liste,
// die von Hand zweimal gepflegt wird (einmal hier, einmal in server.go),
// waere selbst wieder die Fehlerklasse, die gkp-92i vermeiden soll — deshalb
// steht die Liste der erwarteten Ids auch hier direkt und nicht als Import
// aus appctx.Regeln(), damit ein Test, der appctx.Regeln() ebenfalls direkt
// benutzt, nicht sich selbst bestaetigt (Spec §9.1, "am echten Weg vorbei").
func TestAlleAnsichtSegmenteSindReserviert(t *testing.T) {
	h := newTestHandler(t)
	for _, id := range []string{"heute", "jederzeit", "demnaechst", "irgendwann", "eingang", "logbuch", "bereiche"} {
		req := httptest.NewRequest(http.MethodGet, "/"+id, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("/%s: Status = %d, erwartet 200", id, rec.Code)
			continue
		}
		body := rec.Body.String()
		if !strings.Contains(body, `window.INITIAL_VIEW = `+`"`+id+`"`) {
			t.Errorf("/%s traegt kein window.INITIAL_VIEW = %q — als Projektname gedeutet? %.300s", id, id, body)
		}
	}
}

// TestAnsichtSegmentMitUnterauswahl prueft das dritte/zweite Segment
// (/heute/<sub>): es muss als InitialSub durchgereicht werden, genau wie beim
// Projekt-Pfad (/<project>/<view>/<sub>) — sonst waere /reports/<epic> ueber
// die Sammelansicht ein stiller Bruch gegenueber dem Projekt-Pfad.
func TestAnsichtSegmentMitUnterauswahl(t *testing.T) {
	h := newTestHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/logbuch/abc-123", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Status = %d, erwartet 200 (%s)", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `window.INITIAL_VIEW = "logbuch"`) {
		t.Errorf("INITIAL_VIEW fehlt oder falsch: %.400s", body)
	}
	if !strings.Contains(body, `window.INITIAL_SUB = "abc-123"`) {
		t.Errorf("INITIAL_SUB fehlt oder falsch — das dritte Segment wurde nicht durchgereicht: %.400s", body)
	}
}
