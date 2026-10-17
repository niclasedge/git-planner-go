package assets

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Die Browser-Haelfte von gkp-8a0: rpTeilbaumEinsetzen ersetzt nach einem
// Schreibvorgang genau den Ast der betroffenen Aufgabe in window.DATA
// (Spec §7.2).
//
// Geprueft wird der AUSGELIEFERTE Code, aus der eingebetteten browser.js
// herausgeschnitten — nicht eine von Hand danebengelegte Kopie. Dieselbe
// Bauart wie TestMdEscMdInline_NoAttributeBreakoutViaQuotes und aus demselben
// Grund: eine Kopie driftet, und ein Test an einer Kopie bleibt nach der
// Regression gruen.
//
// Ueberspringt sich ohne node, wie die uebrigen Tests dieses Repos bei
// fehlenden Fremdwerkzeugen.
func TestRpTeilbaumEinsetzen(t *testing.T) {
	nodePath, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node nicht installiert")
	}

	const startMarker = "function rpTeilbaumEinsetzen(tb) {"
	const endMarker = "/* ---------- Der Aufgabenbaum als Zeilenfolge (gkp-wsk) ----------"
	start := strings.Index(BrowserJS, startMarker)
	end := strings.Index(BrowserJS, endMarker)
	if start < 0 || end < 0 || end <= start {
		t.Fatalf("Marker um rpTeilbaumEinsetzen nicht gefunden — beim Umbauen dieses Abschnitts die Marker hier mitziehen")
	}

	script := teilbaumJSAufbau + BrowserJS[start:end] + teilbaumJSPruefungen
	pfad := filepath.Join(t.TempDir(), "teilbaum_check.js")
	if err := os.WriteFile(pfad, []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(nodePath, pfad).CombinedOutput(); err != nil {
		t.Fatalf("node-Pruefung fehlgeschlagen: %v\n%s", err, out)
	}
}

// teilbaumJSAufbau stellt das bereit, was der Helfer aus seiner Closure nimmt:
// REPORTS, also window.DATA.reports.
const teilbaumJSAufbau = `
var REPORTS = null;
function baum() {
  REPORTS = { maps: [
    { id: "m", counts: { total: 3, open: 3, closed: 0, proven: 0, gaps: 0 }, beads: [
      { id: "a", title: "a", counts: { total: 2, open: 2, closed: 0, proven: 0, gaps: 0 }, children: [
        { id: "c1", title: "c1" },
        { id: "c2", title: "c2" },
      ] },
    ] },
    { id: "andere", counts: {}, beads: [{ id: "x", title: "x" }] },
  ] };
}
function assert(cond, msg) { if (!cond) { throw new Error("assertion failed: " + msg); } }
function ids(beads) { return (beads || []).map(function (b) { return b.id; }).join(","); }
`

const teilbaumJSPruefungen = `
var zahlenA = { total: 2, open: 1, closed: 1, proven: 0, gaps: 0 };
var zahlenM = { total: 3, open: 2, closed: 1, proven: 0, gaps: 0 };

// 1. Eine bestehende Aufgabe wird ersetzt — samt der Reihenfolge, die Go
//    geliefert hat. c2 wandert nach vorn, weil es gerade angefasst wurde.
baum();
var ok = rpTeilbaumEinsetzen({
  karte: "m", eltern: "a",
  weg: [
    { id: "m", counts: zahlenM, kinder: ["a"] },
    { id: "a", counts: zahlenA, kinder: ["c2", "c1"] },
  ],
  aufgabe: { id: "c2", title: "c2, umbenannt", status: "closed" },
});
assert(ok === true, "Ersetzen sollte gelingen");
var a = REPORTS.maps[0].beads[0];
assert(ids(a.children) === "c2,c1", "Reihenfolge kommt aus weg.kinder, nicht aus dem Browser: " + ids(a.children));
assert(a.children[0].title === "c2, umbenannt", "der neue Stand steht nicht im Baum");
assert(a.counts.closed === 1, "die Zahlen des Elternteils wurden nicht nachgezogen");
assert(REPORTS.maps[0].counts.closed === 1, "die Zahlen der Karte wurden nicht nachgezogen");
assert(REPORTS.maps[1].beads[0].id === "x", "eine fremde Karte wurde angefasst");

// 2. Eine NEU angelegte Aufgabe hat im Browser noch keinen Platz. Sie kommt
//    genau dorthin, wo die Kinderfolge sie nennt.
baum();
ok = rpTeilbaumEinsetzen({
  karte: "m", eltern: "a",
  weg: [
    { id: "m", counts: zahlenM, kinder: ["a"] },
    { id: "a", counts: { total: 3, open: 3, closed: 0, proven: 0, gaps: 0 }, kinder: ["neu", "c1", "c2"] },
  ],
  aufgabe: { id: "neu", title: "frisch angelegt" },
});
assert(ok === true, "Einhaengen einer neuen Aufgabe sollte gelingen");
assert(ids(REPORTS.maps[0].beads[0].children) === "neu,c1,c2", "neue Aufgabe steht an der falschen Stelle");

// 3. Nennt die Kinderfolge eine Aufgabe, die dieser Browser nicht hat und die
//    auch nicht die geschriebene ist, ist der Ast nicht exakt einsetzbar:
//    nichts wird angefasst, der Aufrufer laedt nach. Eine halb aktualisierte
//    Liste waere schlimmer als eine veraltete.
baum();
ok = rpTeilbaumEinsetzen({
  karte: "m", eltern: "a",
  weg: [
    { id: "m", counts: zahlenM, kinder: ["a"] },
    { id: "a", counts: zahlenA, kinder: ["c1", "c2", "woandersher"] },
  ],
  aufgabe: { id: "c2", title: "c2, umbenannt" },
});
assert(ok === false, "unbekannte Geschwister muessen zum Nachladen fuehren");
assert(ids(REPORTS.maps[0].beads[0].children) === "c1,c2", "der Baum wurde trotz Fehlschlag angefasst");
assert(REPORTS.maps[0].beads[0].children[1].title === "c2", "der Stand wurde trotz Fehlschlag ersetzt");
assert(REPORTS.maps[0].counts.closed === 0, "die Zahlen wurden trotz Fehlschlag nachgezogen");

// 4. Karte unbekannt (Projekt gewechselt, Karte geschlossen): kein Einsetzen.
baum();
ok = rpTeilbaumEinsetzen({
  karte: "gibtesnicht", weg: [{ id: "gibtesnicht", counts: {}, kinder: [] }],
  aufgabe: { id: "c2" },
});
assert(ok === false, "unbekannte Karte muss zum Nachladen fuehren");

// 5. Ein Rumpf ohne Weg ist kein Teilbaum — und darf nicht wie einer wirken.
baum();
assert(rpTeilbaumEinsetzen({ karte: "m", weg: [], aufgabe: { id: "c2" } }) === false, "leerer Weg");
assert(rpTeilbaumEinsetzen(null) === false, "kein Teilbaum");
`
