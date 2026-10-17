package assets

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Die Auszeichnung "rp-proof-missing" stand dreimal woertlich in browser.js —
// zweimal als Urteil des Stores (Bild, Datei) und einmal als Rueckfall zur
// Laufzeit, wenn das Vorschaubild 404 liefert. Drei Kopien einer Zeichenkette
// sind drei Gelegenheiten, sie auseinanderlaufen zu lassen.
//
// Der Test haelt fest, dass die Zeichenkette nur noch EINMAL vorkommt. Die
// Kopien selbst waren kein Defekt; das Zusammenziehen ist die Voraussetzung
// dafuer, dass der Unterschied am Aufruf sichtbar wird — und um den geht es im
// Geschwistertest darunter.
func TestKaputterBelegWirdNurAnEinerStelleGezeichnet(t *testing.T) {
	const markup = `<div class="rp-proof rp-proof-missing"`
	if n := strings.Count(BrowserJS, markup); n != 1 {
		t.Errorf("%q kommt %dx vor, erwartet genau 1 — drei Kopien derselben Zeile "+
			"sind drei Gelegenheiten, sie auseinanderlaufen zu lassen", markup, n)
	}
	// Und die Gegenprobe zum Zaehlen: die eine Stelle muss der gemeinsame
	// Helfer sein. Ohne das waere die 1 oben auch dann erfuellt, wenn zwei der
	// drei Faelle die Auszeichnung gar nicht mehr traegen — also genau die
	// Regression, die gkp-wiz repariert hat.
	if !strings.Contains(BrowserJS, "function rpMissingHtml(") {
		t.Error("rpMissingHtml fehlt — die eine verbliebene Fundstelle ist dann kein gemeinsamer Helfer")
	}
}

// Drei Stellen sagten "fehlt/ungültig", und zwei davon meinten Verschiedenes:
//
//   - rpProofHtml (Bild, Datei): der Store hat beim SCHREIBEN festgestellt,
//     dass der Beleg kaputt ist (reports.resolveProofs setzt missing:true).
//   - rpBindImages (onerror): der Store hielt ihn fuer heil, die Route liefert
//     ihn JETZT nicht aus. Das ist eine Aussage darueber, dass Store und Route
//     sich WIDERSPRECHEN — und der Browser wusste sie (er hat den 404 selbst
//     gesehen) und warf sie weg, indem er sich als Server-Urteil tarnte.
//
// Der Unterschied ist nicht Kosmetik: im ersten Fall sucht man den Beleg, im
// zweiten die Asset-Route.
//
// Was dieser Test NICHT verlangt: eine Begruendung am fehlenden DATEI-Beleg.
// Der Eintrag kennt sie nicht (resolveFileProof schreibt missing:true fuer
// "nicht gefunden" und fuer "leer/zu gross" gleichermassen), und eine
// erfundene waere genau die Sorte plausibel klingender Behauptung, gegen die
// der Marker gebaut wurde.
func TestLaufzeit404SagtEtwasAnderesAlsDasServerUrteil(t *testing.T) {
	nodePath, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node nicht installiert")
	}

	script := blStubs +
		navzahlSchnitt(t, "  function rpImageURL(repo, absPath) {", "  function rpEntryHtml(e) {") +
		navzahlSchnitt(t, "  function rpBindImages(root) {", "\n  }\n") + "\n  }\n" +
		blPruefungen

	pfad := filepath.Join(t.TempDir(), "belege_laufzeit_check.js")
	if err := os.WriteFile(pfad, []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(nodePath, pfad).CombinedOutput()
	if err != nil {
		t.Fatalf("node-Pruefung fehlgeschlagen: %v\n%s", err, out)
	}
	t.Logf("gemessen:\n%s", out)
}

const blStubs = `
"use strict";
function assert(cond, msg) { if (!cond) { throw new Error("assertion failed: " + msg); } }
function esc(s) { return String(s == null ? "" : s).replace(/[&<>"]/g, function (c) { return { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[c]; }); }
function showImageModal() { throw new Error("das Modal darf beim 404 nicht aufgehen"); }

/* Ein Vorschaubild, das erst spaeter 404 liefert — der Knopf ist da, das Bild
   nicht. Genau die Lage, in der Store und Route sich widersprechen. */
var IMG = { addEventListener: function (typ, fn) { IMG["on" + typ] = fn; } };
var BTN = {
  werte: { "data-rp-name": "plan.png", "data-rp-image": "/reports/r/assets/plan.png" },
  outerHTML: "",
  getAttribute: function (k) { return this.werte[k]; },
  addEventListener: function () {},
  querySelector: function (sel) { return sel === "img.rp-thumb" ? IMG : null; },
};
var WURZEL = {
  querySelectorAll: function (sel) {
    assert(sel === "[data-rp-image]", "unerwarteter Selektor: " + sel);
    return [BTN];
  },
};
`

const blPruefungen = `
var serverBild = rpProofHtml({ image: "/weg/plan.png", missing: true }, "r");
var serverDatei = rpProofHtml({ file: "docs/weg.dot", missing: true }, "r");

rpBindImages(WURZEL);
assert(typeof IMG.onerror === "function",
  "das Vorschaubild hat keinen error-Hoerer — der Lauf misst sonst nichts");
IMG.onerror();
var laufzeit = BTN.outerHTML;

console.log("Store, Bild:   " + serverBild);
console.log("Store, Datei:  " + serverDatei);
console.log("Laufzeit-404:  " + laufzeit);

/* 1. Alle drei bleiben gleich DEUTLICH. Der Widerspruch darf den Marker nicht
      abschwaechen — das Gegenteil von gkp-wiz waere hier der Rueckschritt. */
[serverBild, serverDatei, laufzeit].forEach(function (h, i) {
  assert(/class="rp-proof rp-proof-missing/.test(h),
    "Fall " + i + " traegt die Auszeichnung nicht: " + h);
});

/* 2. Und sie sagen nicht mehr dasselbe. Der Laufzeit-404 ist die einzige der
      drei Aussagen, die der BROWSER selbst gemessen hat. */
assert(laufzeit !== serverBild,
  "der Laufzeit-404 sagt woertlich dasselbe wie das Server-Urteil und tarnt sich " +
  "damit als eines — der Grund, den der Browser kennt, geht verloren");
assert(laufzeit.indexOf("nicht ausgeliefert") >= 0,
  "der Laufzeit-404 benennt nicht, was passiert ist (der Store hielt den Beleg " +
  "fuer heil, die Route liefert ihn nicht aus): " + laufzeit);
assert(serverBild.indexOf("nicht ausgeliefert") < 0,
  "das Server-Urteil uebernimmt den Satz des Laufzeitfalls: " + serverBild);

/* 3. Der Name bleibt dran — ohne ihn weiss der Leser nicht, WAS fehlt. */
assert(laufzeit.indexOf("plan.png") >= 0, "der Laufzeit-404 nennt den Namen nicht: " + laufzeit);
assert(serverBild.indexOf("/weg/plan.png") >= 0, "das Server-Urteil nennt den Pfad nicht: " + serverBild);

/* 4. Der Datei-Beleg bekommt KEINE Begruendung — ausdrueckliche Vorgabe, und
      der Eintrag kennt sie auch gar nicht. Geprueft wird die Struktur: nach
      dem <code> kommt nichts mehr, und es gibt kein title. */
assert(!/title=/.test(serverDatei), "am fehlenden Datei-Beleg haengt eine Begruendung: " + serverDatei);
assert(/<\/code><\/div>$/.test(serverDatei), "hinter dem Datei-Beleg steht noch etwas: " + serverDatei);

/* 5. Maskiert wird weiterhin — der Helfer ist die EINE Stelle, an der das
      schiefgehen koennte, und damit die einzige, die es noch prueft. */
var boes = rpProofHtml({ file: '<img src=x onerror=alert(1)>.dot', missing: true }, "r");
assert(boes.indexOf("<img") < 0, "der Helfer maskiert den Pfad nicht: " + boes);
BTN.werte["data-rp-name"] = '<img src=x onerror=alert(1)>';
IMG.onerror();
assert(BTN.outerHTML.indexOf("<img") < 0, "der Laufzeit-Zweig maskiert den Namen nicht: " + BTN.outerHTML);
`
