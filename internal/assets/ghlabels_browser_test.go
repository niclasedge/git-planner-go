package assets

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// ghLabels kappte still bei sechs: eine Zeile mit elf Labels sah aus wie eine
// mit sechs, und nichts in der Anzeige sagte, dass etwas fehlt.
//
// Das ist dieselbe Klasse, die gkp-dop fuer die Aufgabenlisten geloest hat —
// dort sagt ein Satz aus Go (Regel.Grenze + a.gekappt), dass gekappt wurde, und
// die Begruendung war, dass eine Liste, die aufhoert, ohne zu sagen warum, von
// einer Liste, die zu Ende ist, nicht zu unterscheiden ist. Bei den Labels
// kappt der BROWSER selbst und kennt beide Laengen; die Zahl darf er deshalb
// hier bilden. Das ist Zeichnen, keine Auswahl.
func TestGhLabelsSagtDassGekapptWurde(t *testing.T) {
	nodePath, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node nicht installiert")
	}

	script := ghlStubs +
		navzahlSchnitt(t, "  function ghLabels(labels) {", "\n  }\n") + "\n}\n" +
		ghlPruefungen

	pfad := filepath.Join(t.TempDir(), "ghlabels_check.js")
	if err := os.WriteFile(pfad, []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(nodePath, pfad).CombinedOutput()
	if err != nil {
		t.Fatalf("node-Pruefung fehlgeschlagen: %v\n%s", err, out)
	}
	t.Logf("gemessen:\n%s", out)
}

const ghlStubs = `
"use strict";
function assert(cond, msg) { if (!cond) { throw new Error("assertion failed: " + msg); } }
function esc(s) { return String(s == null ? "" : s).replace(/[&<>"]/g, function (c) { return { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[c]; }); }
function labels(n) {
  var out = [];
  for (var i = 1; i <= n; i++) { out.push({ name: "label-" + i, color: "ff0000" }); }
  return out;
}
function chips(html) { return (html.match(/<span class="gh-label/g) || []).length; }
`

const ghlPruefungen = `
var elf = ghLabels(labels(11));
var sechs = ghLabels(labels(6));
var keins = ghLabels([]);
console.log("11 Labels -> " + chips(elf) + " Chips");
console.log(" 6 Labels -> " + chips(sechs) + " Chips");
console.log(" 0 Labels -> " + JSON.stringify(keins));

/* Voraussetzung: gekappt wird ueberhaupt noch. Zeigte die Zeile alle elf, waere
   die Frage nach dem Hinweis gegenstandslos — und der Test muesste
   umgeschrieben, nicht stillschweigend gruen bleiben. */
assert(elf.indexOf("label-7") < 0,
  "das siebte Label steht in der Zeile — es wird gar nicht mehr gekappt, " +
  "dieser Test misst dann etwas anderes als er sagt");
assert(elf.indexOf("label-6") >= 0, "das sechste Label fehlt — es wird zu frueh gekappt");

/* Die Aussage: die gekappte Zeile sagt, WIE VIEL fehlt. */
assert(/>\+5</.test(elf),
  "die Zeile mit elf Labels traegt keinen Hinweis auf die fuenf weggelassenen — " +
  "sie sieht aus wie eine mit sechs:\n" + elf);

/* Und die Gegenprobe: genau an der Grenze wird nichts weggelassen, also darf
   auch nichts behauptet werden. Ohne diesen Fall bliebe der Test gruen, wenn
   der Hinweis IMMER dastuende — "+0" waere die Null aus navCount noch einmal. */
assert(!/>\+/.test(sechs),
  "bei genau sechs Labels faellt nichts weg, trotzdem steht ein Hinweis da:\n" + sechs);
assert(chips(sechs) === 6, "erwartet sechs Chips, gezeichnet " + chips(sechs));
assert(chips(elf) === 7, "erwartet sechs Label-Chips plus einen Hinweis, gezeichnet " + chips(elf));

/* Keine Labels heisst gar nichts — kein leerer Chip, kein Rahmen. */
assert(keins === "", "ohne Labels muss die Zelle leer bleiben: " + JSON.stringify(keins));

/* Der Hinweis muss HINTEN stehen. Vorn saehe er aus wie ein Label und
   verschoebe die sechs, die wirklich da sind. */
assert(elf.lastIndexOf("label-6") < elf.lastIndexOf("+5"),
  "der Hinweis steht vor dem letzten Label:\n" + elf);
`
