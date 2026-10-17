package assets

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Die Browser-Haelfte von gkp-dnz: zwei Klappstufen an derselben Zeile.
// Geschnitten wird am AUSGELIEFERTEN Code, dieselbe Bauart wie
// TestFremdeZeileBietetKeinSchreibenAn nebenan — eine danebengelegte Kopie
// wuerde nach einer Regression gruen bleiben.
//
// Der Ausschnitt beginnt bewusst VOR avZeile: AV_AUF/avAufOffen/avAufSetzen
// und avAnzBelege/avBelegeSec stehen im echten Code davor und werden hier
// mitgezogen, statt sie ein zweites Mal nachzubauen (Spec §9.1 Fall 3 — eine
// Fixture, die am echten Schreibweg vorbeigebaut ist, prueft eine Welt, die es
// nicht gibt).
const zweiklappStart = "  /* --- Stufe 1 (gkp-dnz): Auf-/Zuklappen von Unteraufgaben ---"
const zweiklappEnde = "\n  function avBinden("

func zweiklappBlock(t *testing.T) string {
	t.Helper()
	i := strings.Index(BrowserJS, zweiklappStart)
	if i < 0 {
		t.Fatal("Beginn des Zwei-Klappstufen-Blocks nicht gefunden — beim Umbauen den Marker hier mitziehen")
	}
	j := strings.Index(BrowserJS[i:], zweiklappEnde)
	if j < 0 {
		t.Fatal("Ende des Zwei-Klappstufen-Blocks (avBinden) nicht gefunden")
	}
	return BrowserJS[i : i+j]
}

// zweiklappStubs ist ein el(), das anders als der Stummel in
// TestFremdeZeileBietetKeinSchreibenAn (nur {html}) auch die zwei Dinge kann,
// die avZeile jetzt braucht: einen Klick binden (Stufe 1) und Kinder anhaengen
// bzw. wieder entfernen (Stufe 2, das lädt…-Zwischenbild). querySelector(".av-tw")
// gibt DENSELBEN Knoten zurueck wie das Element selbst — avZeile ruft ihn genau
// einmal je Zeile auf, ein echter Selektor waere hier Aufwand ohne Gegenwert.
const zweiklappStubs = `
"use strict";
function el(html) {
  var n = { html: String(html), kinder: [], _ev: {}, open: false, _attr: {} };
  n.addEventListener = function (typ, fn) { n._ev[typ] = fn; };
  n.appendChild = function (c) { n.kinder.push(c); };
  n.removeChild = function (c) {
    var i = n.kinder.indexOf(c);
    if (i >= 0) n.kinder.splice(i, 1);
  };
  n.querySelector = function () { return n; };
  n.setAttribute = function (k, v) { n._attr[k] = v; };
  n.getAttribute = function (k) { return n._attr[k]; };
  n.classList = { add: function () {}, remove: function () {}, toggle: function () {} };
  n.dataset = {};
  return n;
}
function esc(s) { return String(s == null ? "" : s); }
var RP_TIEFE_MAX = 4;
function rpBadge(cls, txt) { return "[" + txt + "]"; }
function rpStatusCls(s) { return ""; }
function rpPrio(p) { return "[P" + p + "]"; }
function rpFortschritt(c, was) { return "[fortschritt]"; }
function avGrundAbzeichen(g) { return "[" + g + "]"; }
function avTageText(n) { return ""; }
function avKarteSuchen(id) { return null; }
var ACTIVE = "git-planner";
function qs(o) {
  return Object.keys(o || {}).filter(function (k) { return o[k] != null && o[k] !== ""; })
    .map(function (k) { return encodeURIComponent(k) + "=" + encodeURIComponent(o[k]); }).join("&");
}
var AV_ZEICHNE_AUFRUFE = [];
function avZeichne(id) { AV_ZEICHNE_AUFRUFE.push(id); }
function rpEntryHtml(e) { return '<div class="entry">' + esc(e.title || "") + "</div>"; }
var RP_BIND_AUFRUFE = [];
function rpBindImages(n) { RP_BIND_AUFRUFE.push(n); }
var GEHOLT = [];
var ANTWORT = { entries: [] };
function fetch(url) {
  GEHOLT.push(url);
  return Promise.resolve({ json: function () { return Promise.resolve(ANTWORT); } });
}
function ruhe() { return new Promise(function (r) { setTimeout(r, 0); }); }
function assert(cond, msg) { if (!cond) { throw new Error("assertion failed: " + msg); } }
`

// TestAvZeileTwistyNurBeiKindernUndRichtungUmgekehrt haelt drei Aussagen
// zusammen, weil nur alle drei zusammen die Regel des Beads sind: der Twisty
// erscheint NUR, wenn es Kinder gibt (b.children.length) — UND seine
// Default-Richtung ist an einer Zwischenueberschrift die andere als an einer
// normalen Aufgabenzeile mit Kindern.
func TestAvZeileTwistyNurBeiKindernUndRichtungUmgekehrt(t *testing.T) {
	nodePath, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node nicht installiert")
	}
	script := zweiklappStubs + zweiklappBlock(t) + `
/* Ohne Kinder: kein Twisty, gleich ob Ueberschrift oder nicht — die Zeile
   sieht anders aus als eine mit, das ist gewollt (Randfall aus dem Bead). */
var blatt = avZeile("heute", { bead: "gkp-1", karte: "gkp-map", tiefe: 0, gruende: [] },
  { id: "gkp-1", title: "Ein Blatt", status: "open" });
if (blatt.html.indexOf("av-tw") >= 0) {
  throw new Error("ein Bead ohne Kinder zeigt trotzdem einen Twisty:\n" + blatt.html);
}

/* Zwischenueberschrift MIT Kindern: seit gkp-a1d stehen ihre Kinder bereits
   aufgeklappt da (eine Ueberschrift ohne sichtbaren Inhalt waere sinnlos) —
   der Twisty muss deshalb im OFFENEN Zustand starten. */
var kopf = avZeile("heute",
  { bead: "gkp-eltern", karte: "gkp-map", tiefe: 0, gruende: [], ueberschrift: true },
  { id: "gkp-eltern", title: "Elternteil ohne eigenen Grund", status: "open",
    children: [{ id: "gkp-kind", title: "Kind", status: "open" }] });
if (kopf.html.indexOf("av-tw") < 0) {
  throw new Error("eine Zwischenueberschrift mit Kindern zeigt keinen Twisty:\n" + kopf.html);
}
if (kopf.html.indexOf('class="av-tw is-offen"') < 0) {
  throw new Error("eine Zwischenueberschrift startet nicht aufgeklappt:\n" + kopf.html);
}

/* Normale Aufgabenzeile MIT EIGENEM Grund und Kindern: sie hat bereits einen
   wahren Grund, hier zu stehen — ihre Unteraufgaben sind ein Extra und
   starten deshalb ZU, umgekehrt zur Zwischenueberschrift oben. */
var normal = avZeile("heute",
  { bead: "gkp-eigen", karte: "gkp-map", tiefe: 0, gruende: ["startklar"] },
  { id: "gkp-eigen", title: "Aufgabe mit eigenem Grund", status: "open",
    children: [{ id: "gkp-kind2", title: "Kind", status: "open" }] });
if (normal.html.indexOf("av-tw") < 0) {
  throw new Error("eine normale Aufgabenzeile mit Kindern zeigt keinen Twisty:\n" + normal.html);
}
if (normal.html.indexOf("is-offen") >= 0) {
  throw new Error("eine normale Aufgabenzeile mit Kindern startet aufgeklappt statt zu " +
    "(umgekehrt zur Zwischenueberschrift):\n" + normal.html);
}

/* Ein Klick dreht den Zustand um UND persistiert ihn (avAufSetzen) — die
   naechste Zeile fuer DENSELBEN Bead muss den neuen Zustand zeigen, nicht den
   alten. Das ist die Eigenschaft hinter "der Aufklappzustand bleibt nach
   einem Termin-/Schliessvorgang erhalten". */
normal.querySelector(".av-tw")._ev.click({ stopPropagation: function () {} });
var normalNochmal = avZeile("heute",
  { bead: "gkp-eigen", karte: "gkp-map", tiefe: 0, gruende: ["startklar"] },
  { id: "gkp-eigen", title: "Aufgabe mit eigenem Grund", status: "open",
    children: [{ id: "gkp-kind2", title: "Kind", status: "open" }] });
if (normalNochmal.html.indexOf('class="av-tw is-offen"') < 0) {
  throw new Error("nach dem Klick bleibt der Twisty zu, obwohl AV_AUF umgeschaltet wurde:\n" +
    normalNochmal.html);
}
if (AV_ZEICHNE_AUFRUFE.length !== 1 || AV_ZEICHNE_AUFRUFE[0] !== "heute") {
  throw new Error("der Klick zeichnet die Ansicht nicht neu: " + JSON.stringify(AV_ZEICHNE_AUFRUFE));
}
`
	pfad := filepath.Join(t.TempDir(), "av_zeile_twisty.js")
	if err := os.WriteFile(pfad, []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(nodePath, pfad).CombinedOutput(); err != nil {
		t.Fatalf("node-Pruefung fehlgeschlagen: %v\n%s", err, out)
	}
}

// TestAvZeileBelegeNurAnBlaettern haelt die Randfaelle aus dem Bead fest: der
// Beleg-Twisty erscheint NUR an einem Blatt (kein b.children) mit einer
// schreibbaren Herkunft (kein b.extern) UND nur, wenn der eager gelieferte
// Zaehler ueberhaupt etwas nennt. Fehlt eine der drei Bedingungen, gibt es
// weder Abzeichen noch Twisty — eine Zeile ohne Belege sieht bewusst anders
// aus als eine mit.
func TestAvZeileBelegeNurAnBlaettern(t *testing.T) {
	nodePath, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node nicht installiert")
	}
	script := zweiklappStubs + zweiklappBlock(t) + `
function zeile(e, b) { return avZeile("heute", e, b); }

var ohneBelege = zeile({ bead: "gkp-1", karte: "gkp-map", tiefe: 0, gruende: [] },
  { id: "gkp-1", title: "Ohne Belege", status: "open" });
if (ohneBelege.html.indexOf("av-belege") >= 0) {
  throw new Error("eine Zeile ohne Eintraege zeigt trotzdem einen Beleg-Twisty:\n" + ohneBelege.html);
}

var mitBelege = zeile({ bead: "gkp-2", karte: "gkp-map", tiefe: 0, gruende: [], beleg_plan: 2, beleg_done: 1 },
  { id: "gkp-2", title: "Mit Belegen", status: "open" });
if (mitBelege.kinder.length !== 1) {
  throw new Error("ein Blatt mit Zaehlstand haengt keinen Beleg-Abschnitt an: " +
    JSON.stringify(mitBelege.kinder));
}
var abschnitt = mitBelege.kinder[0];
if (abschnitt.html.indexOf("2 plan") < 0 || abschnitt.html.indexOf("1 done") < 0) {
  throw new Error("das Abzeichen nennt nicht den gelieferten Zaehlstand (2 plan / 1 done):\n" +
    abschnitt.html);
}

/* Randfall 1: ein Bead MIT Kindern bekommt KEINEN Beleg-Twisty, auch wenn er
   (entgegen der Messung im Bead) einen Zaehlstand traegt — Stufe 2 ist NUR
   fuer Blaetter. */
var mitKindernUndZaehler = zeile(
  { bead: "gkp-3", karte: "gkp-map", tiefe: 0, gruende: [], beleg_plan: 1, beleg_done: 0 },
  { id: "gkp-3", title: "Hat Kinder", status: "open", children: [{ id: "gkp-3a", title: "Kind" }] });
if (mitKindernUndZaehler.kinder.length !== 0) {
  throw new Error("ein Bead MIT Kindern haengt trotzdem einen Beleg-Abschnitt an:\n" +
    JSON.stringify(mitKindernUndZaehler.kinder));
}

/* Randfall 2: eine GitHub-Zeile (b.extern) hat keine Belege und keine
   Schreibrouten (Spec §10) — auch dann nicht, wenn ein Zaehlstand mitkaeme. */
var github = zeile(
  { bead: "#7", karte: "example-user/infra", herkunft: "github", tiefe: 0, gruende: [],
    beleg_plan: 3, beleg_done: 0 },
  { id: "#7", title: "GitHub-Issue", status: "open", extern: true });
if (github.kinder.length !== 0) {
  throw new Error("eine GitHub-Zeile haengt trotzdem einen Beleg-Abschnitt an:\n" +
    JSON.stringify(github.kinder));
}

/* Das Aufklappen selbst: erst beim OEFFNEN kommt GET /belege, nicht vorher —
   und das Ergebnis wird mit rpEntryHtml gezeichnet und danach ZWINGEND an
   rpBindImages uebergeben (sonst oeffnet kein Bild-Modal). */
(async function () {
  if (GEHOLT.length !== 0) {
    throw new Error("der Beleg-Abschnitt hat schon vor dem Oeffnen geladen: " + JSON.stringify(GEHOLT));
  }
  ANTWORT = { entries: [{ kind: "done", title: "Beweisfoto", at: "2026-09-01T00:00:00Z" }] };
  abschnitt.open = true;
  abschnitt._ev.toggle();
  await ruhe();
  if (GEHOLT.length !== 1) {
    throw new Error("GET /belege wurde " + GEHOLT.length + "x geholt, erwartet 1x: " + JSON.stringify(GEHOLT));
  }
  if (GEHOLT[0].indexOf("/belege?") !== 0 || GEHOLT[0].indexOf("bead=gkp-2") < 0) {
    throw new Error("die falsche Route/den falschen Bead geholt: " + GEHOLT[0]);
  }
  var geladen = abschnitt.kinder.map(function (k) { return k.html; }).join("\n");
  if (geladen.indexOf("Beweisfoto") < 0) {
    throw new Error("der geladene Eintrag steht nicht im Abschnitt:\n" + geladen);
  }
  if (RP_BIND_AUFRUFE.length !== 1) {
    throw new Error("rpBindImages wurde nicht (oder mehrfach) aufgerufen: " + RP_BIND_AUFRUFE.length);
  }
})().catch(function (e) {
  console.error(String(e && e.stack || e));
  process.exit(1);
});
`
	pfad := filepath.Join(t.TempDir(), "av_zeile_belege.js")
	if err := os.WriteFile(pfad, []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(nodePath, pfad).CombinedOutput(); err != nil {
		t.Fatalf("node-Pruefung fehlgeschlagen: %v\n%s", err, out)
	}
}
