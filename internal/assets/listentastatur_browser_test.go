package assets

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Die Pfeiltasten liefen an der gefilterten Liste vorbei.
//
// renderList entscheidet, welche Zeilen sichtbar sind (Filterfeld, also reiner
// Browser-Zustand — Go kennt ihn nicht). Der Keydown-Handler lief ueber
// COLL[state.coll].items.length, also ueber die UNGEFILTERTE Menge. Gemessen
// am ausgelieferten Code, Filter "alp":
//
//	sichtbare Zeilen: 0:alpha, 3:alpen
//	ArrowDown #1 -> idx=1, Inhalt "beta"   sichtbar? false
//	ArrowDown #2 -> idx=2, Inhalt "gamma"  sichtbar? false
//
// Der Inhaltsbereich zeigt dann etwas, das links nicht dasteht, und markActive
// markiert nichts — es gibt keine Zeile mit diesem Index. Erreichbar, sobald
// der Fokus das Filterfeld verlassen hat; `e.target.tagName === "INPUT"` faengt
// nur den Tippfall.
//
// Der Lauf geht durch den ECHTEN Handler: das Geruest faengt ihn ab, indem es
// document.addEventListener stubt. Ein nachgebauter Handler koennte nicht
// zeigen, dass die Registrierung noch stimmt.
func TestPfeiltastenLaufenUeberDieSichtbarenZeilen(t *testing.T) {
	nodePath, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node nicht installiert")
	}

	// Die Fixture ist hier BEWUSST von Hand gelegt und kommt nicht aus appctx:
	// der Gegenstand ist der Filter, und der ist reiner Browser-Zustand. Eine
	// Nutzlast aus Go wuerde nur Rauschen um die eine Frage legen.
	script := ltStubs +
		navzahlSchnitt(t, "  /* ---------- list (col 2) ---------- */", "  /* ---------- detail (col 3) ---------- */") +
		navzahlSchnitt(t, "  /* ---------- keyboard nav within list ---------- */",
			"  /* ---------- theme toggle") +
		ltPruefungen

	pfad := filepath.Join(t.TempDir(), "listentastatur_check.js")
	if err := os.WriteFile(pfad, []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(nodePath, pfad).CombinedOutput()
	if err != nil {
		t.Fatalf("node-Pruefung fehlgeschlagen: %v\n%s", err, out)
	}
	t.Logf("gemessen:\n%s", out)
}

// ltStubs ersetzt das DOM und nichts sonst. Die Zeilen, die renderList
// anhaengt, bleiben als Knoten stehen — die sichtbare Menge ist also die
// gezeichnete und nicht eine danebengelegte Behauptung darueber.
const ltStubs = `
"use strict";
function assert(cond, msg) { if (!cond) { throw new Error("assertion failed: " + msg); } }

var EINGABE = {
  value: "",
  addEventListener: function () {},
  focus: function () {},
  setSelectionRange: function () {},
};

function el(html) {
  var n = { html: String(html), kinder: [], dataset: {}, aktiv: false };
  var m = n.html.match(/^<div class="li" data-i="(\d+)"/);
  if (m) { n.dataset.i = m[1]; n.istZeile = true; }
  n.addEventListener = function () {};
  n.appendChild = function (c) { n.kinder.push(c); };
  n.querySelector = function (sel) { return sel === "input" ? EINGABE : null; };
  n.classList = {
    add: function () {}, remove: function () {},
    toggle: function (k, an) { if (k === "is-active") n.aktiv = !!an; },
    contains: function () { return false; },
  };
  return n;
}
function esc(s) { return String(s == null ? "" : s).replace(/[&<>"]/g, function (c) { return { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[c]; }); }
function ktok(n) { n = Number(n) || 0; return n >= 1000 ? (n / 1000).toFixed(1) + "k" : String(Math.round(n)); }
function dotCls() { return ""; }
function dotStyle() { return ""; }

var list = { kinder: [] };
list.appendChild = function (c) { list.kinder.push(c); };
list.querySelector = function (sel) { return sel === "input" ? EINGABE : null; };
list.querySelectorAll = function (sel) {
  assert(sel === ".li", "unerwarteter Selektor: " + sel);
  return list.kinder.filter(function (k) { return k.istZeile; });
};
Object.defineProperty(list, "innerHTML", {
  get: function () { return ""; },
  set: function (v) { if (v === "") { list.kinder.length = 0; } },
});

var mp = { classList: { contains: function () { return false; }, add: function () {}, remove: function () {} } };

/* Was der Inhaltsbereich zeigt — das ist die eine Haelfte des Defekts, die ein
   Blick auf die Liste allein nicht sieht. */
var GEZEIGT = null;
function renderDetail(c, it) { GEZEIGT = it; }

var HANDLER = null;
var document = {
  addEventListener: function (typ, fn) { if (typ === "keydown") HANDLER = fn; },
};

var COLL = {
  skills: {
    title: "Skills", scope: "global", type: "skill", accent: "g",
    items: [{ name: "alpha" }, { name: "beta" }, { name: "gamma" }, { name: "alpen" }],
  },
  psections: { title: "CLAUDE.md", scope: "projekt", type: "claude", accent: "c", items: [{ name: "A" }, { name: "B" }] },
};
var state = { coll: "skills", idx: 0 };
`

const ltPruefungen = `
assert(typeof HANDLER === "function",
  "der Keydown-Handler hat sich nicht registriert — der Lauf misst sonst nichts");

function taste(k) { HANDLER({ key: k, target: { tagName: "DIV" }, preventDefault: function () {} }); }
function sichtbareIndizes() {
  return list.querySelectorAll(".li").map(function (n) { return +n.dataset.i; });
}
function markiert() {
  return list.querySelectorAll(".li").filter(function (n) { return n.aktiv; }).map(function (n) { return +n.dataset.i; });
}

/* --- A: der gemeldete Fall. Filter "alp" laesst 0:alpha und 3:alpen stehen. --- */
state.coll = "skills"; state.idx = 0;
renderList("alp");
var sicht = sichtbareIndizes();
console.log("sichtbare Zeilen: " + sicht.join(", "));

/* Waechter VOR den Tasten: taugt die Fixture? Die sichtbare Menge muss echt
   kleiner sein als die ganze UND eine Luecke haben — ohne Luecke traefe
   "idx + 1" die richtige Zeile aus Versehen. */
assert(sicht.length === 2 && sicht[0] === 0 && sicht[1] === 3,
  "erwartet die Zeilen 0 und 3 als sichtbar, gezeichnet: " + JSON.stringify(sicht));
assert(sicht.length < COLL.skills.items.length,
  "der Filter hat nichts weggenommen — der Test misst nichts");

var lauf = [];
taste("ArrowDown");
lauf.push(state.idx);
taste("ArrowDown");
lauf.push(state.idx);
console.log("ArrowDown x2 -> idx " + lauf.join(", ") +
  " | Inhalt: " + lauf.map(function () { return GEZEIGT && GEZEIGT.name; }).join(", "));

lauf.forEach(function (i, n) {
  assert(sicht.indexOf(i) >= 0,
    "ArrowDown #" + (n + 1) + " landet auf Zeile " + i + " (" + COLL.skills.items[i].name +
    "), die gar nicht sichtbar ist — der Inhaltsbereich zeigt dann etwas, " +
    "das links nicht dasteht, und markActive markiert nichts. Sichtbar: " + JSON.stringify(sicht));
});
assert(lauf[0] === 3, "die erste Zeile unter 0 ist 3 (alpen), bekommen " + lauf[0]);
assert(lauf[1] === 3, "am Ende der sichtbaren Liste darf ArrowDown nicht weiterlaufen, bekommen " + lauf[1]);
assert(GEZEIGT && GEZEIGT.name === "alpen",
  "der Inhaltsbereich zeigt " + (GEZEIGT && GEZEIGT.name) + " statt alpen");

/* Und die zweite Haelfte: die Markierung. Eine Auswahl ausserhalb der
   sichtbaren Menge ist nirgends zu sehen — genau daran faellt der Defekt im
   Betrieb NICHT auf. */
assert(markiert().join(",") === "3",
  "markiert sind die Zeilen " + JSON.stringify(markiert()) + ", erwartet nur 3");

/* --- B: zurueck, ueber dieselbe Menge. --- */
taste("ArrowUp");
console.log("ArrowUp     -> idx " + state.idx + " | Inhalt: " + (GEZEIGT && GEZEIGT.name));
assert(state.idx === 0, "ArrowUp muss auf 0 (alpha) zurueck, bekommen " + state.idx);
taste("ArrowUp");
assert(state.idx === 0, "am Anfang darf ArrowUp nicht weiterlaufen, bekommen " + state.idx);

/* --- C: ohne Filter bleibt alles, wie es war. --- */
state.idx = 0;
renderList("");
assert(sichtbareIndizes().join(",") === "0,1,2,3", "ohne Filter muessen alle Zeilen dastehen");
taste("ArrowDown");
assert(state.idx === 1, "ohne Filter ist die naechste Zeile 1, bekommen " + state.idx);

/* --- D: eine Ansicht, fuer die renderList gar nicht gelaufen ist. psections
       und changelog setzen state.coll, raeumen die Liste aber leer (select()).
       Die Tastatur haette dort ueber eine unsichtbare Liste gefuehrt — derselbe
       Defekt, nur ohne Filter. --- */
list.innerHTML = "";
state.coll = "psections"; state.idx = 0;
taste("ArrowDown");
console.log("psections (Liste leer geraeumt) -> idx " + state.idx);
assert(state.idx === 0,
  "die Tastatur bewegt die Auswahl in einer Ansicht ohne gezeichnete Liste — " +
  "dort ist keine einzige Zeile zu sehen, die Bewegung waere unsichtbar");
`
