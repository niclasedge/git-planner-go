package assets

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/niclasedge/git-planner-go/internal/appctx"
	"github.com/niclasedge/git-planner-go/internal/collectors"
)

// gkp-d52: die Zahl in der Navigation und die Zahl im Kopf der geoeffneten
// Ansicht sind ZWEI Aussagen ueber denselben Inhalt — und sie standen
// auseinander. Am laufenden Stand (infra, Jederzeit) sagte die Navigation
// 47 und der Listenkopf 57; die Differenz waren genau die zehn offenen
// GitHub-Issues.
//
// Gefunden wurde das NUR, weil jemand einen Screenshot angesehen hat
// (docs/belege/pf2-jederzeit-github.png). Kein Test sah die beiden Zahlen
// zusammen: die eine entsteht in Go beim Seitenaufbau (appctx.ansichtenNav),
// die andere in browser.js beim Zeichnen (a.eintraege.length). Ein Test, der
// nur die Navigationszahl prueft, wiederholt genau diesen blinden Fleck.
//
// Dieser Test sieht beide in EINEM Lauf, und zwar am ausgelieferten Code:
// die Nutzlast baut appctx (AnsichtenAntwort, derselbe Rumpf, den GET
// /ansichten schickt), die Zahlen rendern die echten Funktionen aus
// browser.js. Gestubt ist nur das DOM.

// navzahlZeit ist derselbe Bezugspunkt wie in den appctx-Tests: Montag,
// 14.09.2026, 10:00 Ortszeit in einer Zone mit Versatz — unter TZ=UTC waere die
// Datumsdeutung unsichtbar (Spec §4.4).
func navzahlZeit() time.Time {
	return time.Date(2026, 9, 14, 10, 0, 0, 0, time.FixedZone("Test+02", 2*60*60))
}

// navzahlBeads sind die beads-Karten. Drei Aufgaben, die in Jederzeit landen
// (offen, startklar, ohne Termin) — und eine geschlossene, damit die Zahl nicht
// zufaellig die Zahl ALLER Beads ist.
func navzahlBeads() []appctx.ReportsMap {
	bead := func(id, status string, ready bool) appctx.ReportsBead {
		return appctx.ReportsBead{
			ID: id, Title: id, Status: status, Ready: ready, Priority: 2,
			LastChange: "2026-09-01T00:00:00Z",
		}
	}
	return []appctx.ReportsMap{{
		ID: "gkp-map", Title: "Eine Karte",
		Beads: []appctx.ReportsBead{
			bead("gkp-1", "open", true),
			bead("gkp-2", "open", true),
			bead("gkp-3", "in_progress", false),
			bead("gkp-4", "closed", false),
		},
	}}
}

// navzahlGithub sind die GitHub-Karten: zwei offene Issues, eines geschlossen.
// Die Zahl der GitHub-Zeilen (2) ist absichtlich eine ANDERE als die der
// beads-Zeilen (3) — waeren sie gleich, koennte eine Gegenprobe nicht zeigen,
// welche Haelfte fehlt.
func navzahlGithub() []appctx.ReportsMap {
	return appctx.GithubKarten("example-user/infra", []collectors.GhIssue{
		{Number: 209, Title: "Traefik", State: "OPEN", UpdatedAt: "2026-09-10T00:00:00Z",
			URL: "https://github.com/example-user/infra/issues/209"},
		{Number: 210, Title: "Backup", State: "OPEN", UpdatedAt: "2026-09-11T00:00:00Z",
			URL: "https://github.com/example-user/infra/issues/210"},
		{Number: 8, Title: "erledigt", State: "CLOSED", ClosedAt: "2026-09-01T00:00:00Z",
			UpdatedAt: "2026-09-01T00:00:00Z",
			URL:       "https://github.com/example-user/infra/issues/8"},
	})
}

// navzahlQuellen ist die Kartenliste der Route: beads (schreibbar) und daneben
// GitHub (nur lesend), in dieser Reihenfolge — so wie die Quellen-Tabelle sie
// fuer ein Projekt liefert (aufgaben-quelle, #9).
func navzahlQuellen(maps, github []appctx.ReportsMap) []appctx.QuellKarten {
	return []appctx.QuellKarten{
		{Herkunft: appctx.HerkunftBeads, Faehigkeiten: appctx.Faehigkeiten{Schreibbar: true, Termine: true, Belege: true}, Maps: maps},
		{Herkunft: appctx.HerkunftGithub, Maps: github},
	}
}

func navzahlJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("Nutzlast laesst sich nicht serialisieren: %v", err)
	}
	return string(b)
}

// TestNavigationszahlUndListenkopfZeigenDieselbeZahl ist das Abnahmekriterium
// von gkp-d52.
//
// Der Ablauf ist der des Browsers, in dieser Reihenfolge:
//
//  1. Die Seite kommt mit DATA.nav und REPORTS.ansichten aus Go — beides OHNE
//     GitHub, weil GitHub nach gkp-isj nicht im Projekt-Oeffnen-Pfad steht.
//  2. avZeichne("jederzeit") malt die Liste; ihr Kopf sagt 3 Aufgaben.
//  3. avGithubNachziehen holt GET /ansichten und setzt die vollstaendige Liste
//     ein; der Kopf sagt danach 5.
//  4. Die Navigation muss dann auch 5 sagen — vorher blieb sie bei 3 stehen.
//
// Zwei Gegenproben stecken im selben Lauf:
//
//   - Der Kopf MUSS gewachsen sein (5 > 3). Sonst waere die Gleichheit die
//     triviale — beide Zahlen unveraendert, weil GitHub gar nicht ankam.
//   - Antwortet GitHub NICHT (kein Token, kein Netz, unbekanntes Repo), muessen
//     beide Zahlen bei 3 stehen. Das ist der haeufigere Fall, und eine
//     Navigation, die dann auf eine GitHub-Zahl umspringt, waere derselbe
//     Defekt mit umgekehrtem Vorzeichen.
func TestNavigationszahlUndListenkopfZeigenDieselbeZahl(t *testing.T) {
	nodePath, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node nicht installiert")
	}

	jetzt := navzahlZeit()
	maps := navzahlBeads()

	// Der Stand der Seite: beads-only, so wie der Projekt-Oeffnen-Pfad ihn baut.
	ohneGithub := appctx.AnsichtenBauen(maps, jetzt)
	// Und die Antwort der Route, ueber beide Quellen — gebaut vom
	// Produktionscode, nicht hier nachgelegt.
	mitGithub := appctx.AnsichtenAntwort(
		appctx.AnsichtenBauenAus(navzahlQuellen(maps, navzahlGithub()), jetzt), "")
	// Der Fall "GitHub antwortet nicht": dieselbe Route, zweite Quelle leer.
	leerAntwort := appctx.AnsichtenAntwort(
		appctx.AnsichtenBauenAus(navzahlQuellen(maps, nil), jetzt), "GitHub nicht verfügbar")

	// Ein Waechter VOR dem Browser-Teil: taugt die Fixture ueberhaupt? Waeren
	// die beiden Zahlen gleich, koennte der Test unten nichts unterscheiden und
	// bliebe gruen, ohne etwas gesehen zu haben.
	vorher := len(ohneGithub[appctx.JederzeitID].Eintraege)
	nachher := len(appctx.AnsichtenBauenAus(navzahlQuellen(maps, navzahlGithub()), jetzt)[appctx.JederzeitID].Eintraege)
	if vorher == 0 || nachher <= vorher {
		t.Fatalf("Fixture taugt nicht: Jederzeit hat ohne GitHub %d und mit GitHub %d Zeilen — "+
			"die zweite Quelle muss die Liste sichtbar verlaengern", vorher, nachher)
	}

	// DATA.nav, wie BuildData es beim Seitenaufbau erzeugt. Der Umweg ueber
	// BuildData und nicht ueber ansichtenNav direkt ist Absicht: zwischen den
	// beiden liegt die Gruppe "Aufgaben" in builddata.go, und der Browser liest
	// genau diese Form.
	data := appctx.BuildData(map[string]any{"cards": map[string]any{
		"reports": map[string]any{"ansichten": ohneGithub},
	}}, "project")

	script := navzahlHarness(t) + `
var NAV_START = ` + navzahlJSON(t, data["nav"]) + `;
var REPORTS_START = { maps: ` + navzahlJSON(t, maps) + `, ansichten: ` + navzahlJSON(t, ohneGithub) + ` };
var ANTWORT_MIT_GITHUB = ` + navzahlJSON(t, mitGithub) + `;
var ANTWORT_OHNE_GITHUB = ` + navzahlJSON(t, leerAntwort) + `;
` + navzahlPruefungen

	pfad := filepath.Join(t.TempDir(), "navzahl_check.js")
	if err := os.WriteFile(pfad, []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(nodePath, pfad).CombinedOutput()
	if err != nil {
		t.Fatalf("node-Pruefung fehlgeschlagen: %v\n%s", err, out)
	}
	t.Logf("gemessen:\n%s", out)
}

// navzahlHarness ist das Geruest: die Stubs und die aus browser.js
// herausgeschnittenen ECHTEN Bloecke.
//
// Herausgeschnitten und nicht nachgebaut, aus demselben Grund wie bei
// TestRpTeilbaumEinsetzen: eine Kopie driftet, und ein Test an einer Kopie
// bleibt nach der Regression gruen.
func navzahlHarness(t *testing.T) string {
	t.Helper()
	navBlock := navzahlSchnitt(t, "  function navCount(it) {", "  /* ---------- select a nav entry ---------- */")
	avBlock := navzahlSchnitt(t, "  function avAnsicht(id) {", "  function avZeile(ansichtId, e, b) {")
	return navzahlStubs + navBlock + avBlock
}

func navzahlSchnitt(t *testing.T, start, ende string) string {
	t.Helper()
	i := strings.Index(BrowserJS, start)
	if i < 0 {
		t.Fatalf("%q nicht gefunden — beim Umbauen den Marker hier mitziehen", start)
	}
	j := strings.Index(BrowserJS[i:], ende)
	if j < 0 {
		t.Fatalf("Ende %q nach %q nicht gefunden", ende, start)
	}
	// Der ANFANGSanker muss eindeutig sein. Fall 8 in Spec §9.1: dort traf ein
	// strings.Index die ERSTE Fundstelle statt der gemeinten, und der gepruefte
	// Ausschnitt enthielt danach den halben Block — die halbe Behauptung war
	// immer erfuellt. Fuer das ENDE gilt das nicht: es wird ab dem Anfang
	// gesucht, ein spaeteres Vorkommen kann den Schnitt also nicht vorverlegen.
	// Deshalb darf dort die generische schliessende Klammer stehen, so wie in
	// rumpfVon nebenan.
	if n := strings.Count(BrowserJS, start); n != 1 {
		t.Fatalf("Anfangsanker %q kommt %dx vor — mehrdeutig, der Schnitt traefe die falsche Stelle", start, n)
	}
	return BrowserJS[i : i+j]
}

// navzahlStubs ersetzt das DOM und nichts sonst. Jede Zahl, um die es geht,
// laeuft durch den echten Code darueber.
const navzahlStubs = `
"use strict";

/* el() gibt statt eines DOM-Knotens ein Objekt mit dem gezeichneten HTML
   zurueck — dieselbe Bauart wie in TestFremdeZeileBietetKeinSchreibenAn. Die
   Zahlen sind damit genau das, was der Browser anzeigen wuerde, und nicht das,
   was eine Nachbildung daraus macht. */
function el(html) {
  var n = { html: String(html), kinder: [], teile: {} };
  n.addEventListener = function () {};
  n.appendChild = function (c) { n.kinder.push(c); };
  n.querySelector = function (sel) {
    if (!n.teile[sel]) { n.teile[sel] = { textContent: "", kinder: [], appendChild: n.appendChild }; }
    return n.teile[sel];
  };
  n.classList = { add: function () {}, remove: function () {}, toggle: function () {} };
  n.dataset = {};
  return n;
}
function esc(s) { return String(s == null ? "" : s).replace(/[&<>"]/g, function (c) { return { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[c]; }); }

function wirt() {
  var w = { kinder: [] };
  w.appendChild = function (c) { w.kinder.push(c); };
  w.querySelectorAll = function () { return []; };
  Object.defineProperty(w, "innerHTML", {
    get: function () { return ""; },
    set: function (v) { if (v === "") { w.kinder.length = 0; } },
  });
  return w;
}

var nav = wirt();
var list = wirt();
var GEZEICHNET = "";
var detail = wirt();
detail.appendChild = function (c) { GEZEICHNET = String(c.html || ""); };

/* gkp-dnz: die sechs Ansichten stehen seither NICHT mehr in #mp-nav (nav
   oben), sondern ausschliesslich im Kopf (.viewnav, #kn-viewnav) —
   viewNavZeichnen() braucht dafuer ein document mit genau dieser einen Zeile.
   querySelectorAll() muss trotzdem existieren: projListZeichnen (dieselbe
   Abwehr wie viewNavZeichnen fuer "kein document") ruft es ohne eigene
   Fallunterscheidung auf, sobald document ueberhaupt definiert ist. */
var viewnavHost = wirt();
var document = {
  getElementById: function (id) { return id === "kn-viewnav" ? viewnavHost : null; },
  querySelectorAll: function () { return []; },
};

var NAV = [];
var REPORTS = null;
var COLL = {};
var GIT = null;
var ACTIVE = "infra";
var AV_REG = { jederzeit: { label: "Jederzeit" }, logbuch: { label: "Logbuch" } };
var RP_TIEFE_MAX = 4;

function qs(o) {
  return Object.keys(o || {}).filter(function (k) { return o[k] != null && o[k] !== ""; })
    .map(function (k) { return encodeURIComponent(k) + "=" + encodeURIComponent(o[k]); }).join("&");
}
function rpBadge(cls, txt) { return "[" + txt + "]"; }
function rpTeilbaumEinsetzen() { return true; }
function avZeile() { return el('<div class="av-row"></div>'); }
function avBinden() {}
function select() {}
// viewToPath ist fuer diesen Test nicht der Gegenstand — die Kopf-Navigation
// (viewNavZeichnen) braucht es nur fuer das href-Attribut ihrer Zeilen.
function viewToPath(id) { return "/" + encodeURIComponent(id); }

/* Die Route liefert, was ANTWORT gerade ist — der Netzaufruf selbst ist nicht
   Gegenstand dieses Tests (das misst server/ansichten_route_test.go). */
var ANTWORT = null;
var GEHOLT = [];
function fetch(url) {
  GEHOLT.push(String(url));
  return Promise.resolve({ json: function () { return Promise.resolve(ANTWORT); } });
}

function assert(cond, msg) { if (!cond) { throw new Error("assertion failed: " + msg); } }

/* Die beiden Zahlen, jede aus dem gezeichneten HTML gelesen.

   Genau EINE Zeile je Id, und das ist keine Formsache: waere hier "die letzte
   gewinnt" erlaubt, bliebe der Test auch dann gruen, wenn das Neuzeichnen die
   alten Zeilen stehen liesse — der Benutzer saehe beide Zahlen untereinander,
   die Messung nur die neue. */
function navZahl(id) {
  var treffer = [];
  function ab(knoten) {
    (knoten || []).forEach(function (k) {
      var h = String(k.html || "");
      if (h.indexOf('data-id="' + id + '"') >= 0) { treffer.push(h); }
      ab(k.kinder);
    });
  }
  ab(nav.kinder);
  assert(treffer.length !== 0, "in der Navigation steht keine Zeile fuer " + id);
  assert(treffer.length === 1,
    "in der Navigation stehen " + treffer.length + " Zeilen fuer " + id +
    " — beim Neuzeichnen bleiben die alten stehen:\n" + treffer.join("\n"));
  var m = treffer[0].match(/class="nc">([^<]*)</);
  assert(m, "die Navigationszeile fuer " + id + " zeigt keine Zahl: " + treffer[0]);
  return m[1];
}
/* Traegt die Zeile die Markierung der geoeffneten Ansicht? Das Neuzeichnen
   baut die Navigation neu auf; eine Markierung, die nur im DOM stand, waere
   danach weg (Begruendung an AKTIV in browser.js). */
function navAktiv(id) {
  var treffer = "";
  function ab(knoten) {
    (knoten || []).forEach(function (k) {
      var h = String(k.html || "");
      if (h.indexOf('data-id="' + id + '"') >= 0) { treffer = h; }
      ab(k.kinder);
    });
  }
  ab(nav.kinder);
  return treffer.indexOf("is-active") >= 0;
}
/* Die sechs Ansichten stehen seit gkp-dnz NICHT mehr in #mp-nav (navZahl
   oben), sondern nur noch im Kopf — viewNavZeichnen() zeichnet sie flach in
   viewnavHost.kinder, ohne die Gruppen-Verschachtelung von navZeichnen(). */
function viewNavZahl(id) {
  var treffer = [];
  (viewnavHost.kinder || []).forEach(function (k) {
    var h = String(k.html || "");
    if (h.indexOf('data-id="' + id + '"') >= 0) { treffer.push(h); }
  });
  assert(treffer.length !== 0, "in der Kopf-Navigation steht keine Zeile fuer " + id);
  assert(treffer.length === 1,
    "in der Kopf-Navigation stehen " + treffer.length + " Zeilen fuer " + id +
    " — beim Neuzeichnen bleiben die alten stehen:\n" + treffer.join("\n"));
  var m = treffer[0].match(/class="vn-c">([^<]*)</);
  assert(m, "die Kopf-Navigationszeile fuer " + id + " zeigt keine Zahl: " + treffer[0]);
  return m[1];
}
function viewNavAktiv(id) {
  var treffer = "";
  (viewnavHost.kinder || []).forEach(function (k) {
    var h = String(k.html || "");
    if (h.indexOf('data-id="' + id + '"') >= 0) { treffer = h; }
  });
  return treffer.indexOf("is-active") >= 0;
}
function kopfZahl() {
  var m = GEZEICHNET.match(/([0-9]+) Aufgabe\(n\)/);
  assert(m, "der Listenkopf nennt keine Zahl: " + GEZEICHNET.slice(0, 400));
  return m[1];
}
`

// navzahlPruefungen ist der Ablauf. Async, weil die zweite Quelle ueber ein
// Promise kommt: ein `setTimeout(0)` laeuft nach ALLEN Microtasks und damit
// nach beiden .then-Stufen von avGithubNachziehen.
const navzahlPruefungen = `
function frisch(antwort) {
  NAV = JSON.parse(JSON.stringify(NAV_START));
  REPORTS = JSON.parse(JSON.stringify(REPORTS_START));
  ANTWORT = antwort;
  AKTIV = "jederzeit";   /* das tut im Betrieb select() */
  AV_GH_ANS = null;
  AV_GH_GRUND = "";
  nav.innerHTML = "";
  navZeichnen();
  /* gkp-dnz: die sechs Ansichten stehen nicht mehr in NAV (#mp-nav) — der
     Kopf baut seine eigene Grundmenge aus AV_REG + "bereiche" (Begruendung an
     viewNavGrundmenge in browser.js) und zeichnet SICH SELBST, genau wie im
     Betrieb beim Seitenaufbau (VIEWNAV_ITEMS = viewNavGrundmenge();
     viewNavZeichnen(); danach kopfNavLaden()). */
  VIEWNAV_ITEMS = viewNavGrundmenge();
  viewNavZeichnen();
}
function ruhe() { return new Promise(function (r) { setTimeout(r, 0); }); }

(async function () {
  /* --- 1. GitHub antwortet: die Liste waechst, die Kopf-Navigation muss mit. ---
     Anders als vor gkp-dnz gibt es hier kein "vorher" fuer die Kopf-Navigation
     mehr: die Zahl stand bis dahin synchron in NAV_START (ueber die Gruppe
     "Aufgaben" in builddata.go), die es seit gkp-dnz nicht mehr gibt —
     viewNavGrundmenge() faellt jetzt auf ihre eigene Platzhaltermenge OHNE
     Zahl zurueck (cnt:null), bis genau dieselbe Antwort sie nachtraegt, die
     hier gerade erst kommt. Der Listenkopf dagegen ist von diesem Umbau
     unberuehrt (er liest REPORTS.ansichten, nicht NAV) und zeigt seine Zahl
     weiterhin synchron. */
  frisch(ANTWORT_MIT_GITHUB);
  avZeichne("jederzeit");
  var kopfVorher = kopfZahl();

  await ruhe();
  var navNachher = viewNavZahl("jederzeit");
  var kopfNachher = kopfZahl();
  console.log("mit GitHub:   Kopf-Navigation -> " + navNachher +
    " | Listenkopf " + kopfVorher + " -> " + kopfNachher);

  /* Die Gegenprobe zuerst: ist die Liste ueberhaupt gewachsen? Ohne das waere
     die Gleichheit darunter die triviale — zwei unveraenderte Zahlen. */
  assert(Number(kopfNachher) > Number(kopfVorher),
    "der Listenkopf ist nicht gewachsen (" + kopfVorher + " -> " + kopfNachher +
    ") — die GitHub-Zeilen kamen gar nicht an, der Test misst nichts");

  /* Das Neuzeichnen darf die Markierung der geoeffneten Ansicht nicht
     verlieren — sie ist der einzige Hinweis darauf, wo man gerade steht. */
  assert(viewNavAktiv("jederzeit"),
    "nach dem Auffrischen ist die geoeffnete Ansicht in der Kopf-Navigation nicht mehr markiert");

  /* Und das Abnahmekriterium. */
  assert(navNachher === kopfNachher,
    "Kopf-Navigation sagt " + navNachher + ", der Listenkopf sagt " + kopfNachher +
    " — die Zahl neben einer Ansicht ist die einzige Angabe, die ein Benutzer " +
    "OHNE Oeffnen sieht; stimmt sie nicht, ist sie schlimmer als keine Zahl");

  /* --- 2. GitHub antwortet nicht — der haeufigere Fall. --- */
  frisch(ANTWORT_OHNE_GITHUB);
  avZeichne("jederzeit");
  await ruhe();
  var navLeer = viewNavZahl("jederzeit");
  var kopfLeer = kopfZahl();
  console.log("ohne GitHub:  Kopf-Navigation -> " + navLeer +
    " | Listenkopf " + kopfLeer + " | Grund: " + AV_GH_GRUND);

  assert(navLeer === kopfLeer,
    "ohne GitHub sagt die Kopf-Navigation " + navLeer + " und der Kopf " + kopfLeer);
  assert(navLeer === kopfVorher,
    "ohne GitHub zeigt die Kopf-Navigation " + navLeer + " statt der reinen beads-Zahl " +
    kopfVorher + " — sie darf nur die beads-Haelfte zeigen, wenn nur die da ist");
  assert(AV_GH_GRUND !== "",
    "kein Grund im Rumpf: 'keine GitHub-Zeilen' waere von 'GitHub nicht erreichbar' " +
    "nicht zu unterscheiden");
  assert(GEHOLT.length === 2, "GET /ansichten wurde " + GEHOLT.length + "x geholt, erwartet 2");
})().catch(function (e) {
  console.error(String(e && e.stack || e));
  process.exit(1);
});
`

// TestNavigationszahlWirdNichtImBrowserGerechnet ist die Wache neben
// TestAnsichtenBlockRechnetNicht, und sie hat denselben Anlass.
//
// In gkp-7gb entstand ein echter Defekt daraus, dass der Browser die
// Reihenfolge der Kopfbilanz selbst herstellte (Object.keys(...).sort(), und JS
// vergleicht Code-Einheiten — "überfällig" landete hinter jedem ASCII-Wort).
// Die Antwort war, die Ordnung Teil der Nutzlast zu machen. Die
// Navigationszahl ist derselbe Fall: sie kommt seit gkp-d52 fertig aus
// appctx.ansichtenNav, und der Browser setzt sie nur ein.
//
// Die Versuchung, es anders zu machen, ist hier besonders gross, weil die
// richtige Zahl im Browser GREIFBAR daliegt — `a.eintraege.length`. Genau das
// waere eine zweite Zaehlung neben der von Go, und zwei Zaehlungen sind zwei
// Gelegenheiten, verschieden zu zaehlen.
func TestNavigationszahlWirdNichtImBrowserGerechnet(t *testing.T) {
	rumpf := ohneKommentare(navzahlSchnitt(t,
		"  function navZahlenUebernehmen(items) {", "\n  }\n"))

	verboten := map[string]string{
		"eintraege": "gezaehlt wird in appctx (ansichtenNav), nicht hier",
		".length":   "dito — die Laenge der Liste ist die Zaehlung",
		".filter(":  "ausgewaehlt wird in appctx/ansicht.go (Regel.Waehlt)",
		".sort(":    "sortiert wird in appctx/ansicht.go (sortiereGruppen)",
	}
	for muster, warum := range verboten {
		if strings.Contains(rumpf, muster) {
			t.Errorf("navZahlenUebernehmen enthaelt %q — %s", muster, warum)
		}
	}
	// Die Gegenprobe zum Verbot: ohne sie waeren die Verbote oben auch dann
	// erfuellt, wenn die Funktion gar nichts einsetzte (Leerlauftest, Muster 1
	// in Spec §9.1). Die Zahl muss aus dem gelieferten Eintrag kommen.
	for _, noetig := range []string{"n.cnt", "it.cnt = ", "navZeichnen("} {
		if !strings.Contains(rumpf, noetig) {
			t.Errorf("navZahlenUebernehmen enthaelt %q nicht — setzt sie die gelieferte Zahl ein?", noetig)
		}
	}

	// Und die Aufrufstelle. Dass die Funktion EXISTIERT, sagt nichts: die
	// erste Fassung von TestAnsichtenHolenGithubUeberEigeneRoute blieb gruen,
	// als der Aufruf entfernt war.
	nachziehen := navzahlSchnitt(t, "  function avGithubNachziehen(ansichtId) {", "\n  }\n")
	if !strings.Contains(ohneKommentare(nachziehen), "navZahlenUebernehmen(j.nav)") {
		t.Error("avGithubNachziehen frischt die Navigationszahlen nicht auf — " +
			"die Navigation bleibt dann auf dem Stand des Seitenaufbaus, also ohne GitHub")
	}

	// Und die eine Zeile, die der Lauf oben NICHT sehen kann, weil select()
	// dort gestubt ist: das Neuzeichnen baut die Navigation neu auf, die
	// Markierung der geoeffneten Ansicht muss also in den DATEN stehen. Dass
	// navRow sie zeichnet, misst der Lauf; dass select sie setzt, nur das hier.
	if !strings.Contains(ohneKommentare(navzahlSchnitt(t, "  function select(id) {", "\n  }\n")), "AKTIV = id") {
		t.Error("select() fuehrt AKTIV nicht — nach einem Neuzeichnen der Navigation " +
			"waere die geoeffnete Ansicht nicht mehr markiert")
	}
}

// navzahlLogbuchBeads baut eine Rueckschau, die ueber die Grenze geht: zwoelf
// Abschlusstage zu je zehn Aufgaben, also 120 Zeilen gegen appctx' Deckel von
// 100 (gkp-dop).
//
// Die Ids laufen GEGEN die erwartete Auswahl — der aelteste Tag traegt die
// kleinsten Ids. Kappte irgendetwas nach Id statt nach Abschlusstag, stuenden
// die alten Tage in der Liste, und der Test saehe es an den Praefixen unten.
func navzahlLogbuchBeads() []appctx.ReportsMap {
	var beads []appctx.ReportsBead
	for tag := 0; tag < 12; tag++ {
		stempel := time.Date(2026, 9, 14, 6, 0, 0, 0, time.UTC).
			AddDate(0, 0, -tag).Format(time.RFC3339)
		for i := 0; i < 10; i++ {
			id := string(rune('a'+11-tag)) + itoaNav(i)
			beads = append(beads, appctx.ReportsBead{
				ID: id, Title: id, Status: "closed", ClosedAt: stempel,
				Priority: 2, LastChange: stempel,
			})
		}
	}
	return []appctx.ReportsMap{{ID: "gkp-map", Title: "Eine Karte", Beads: beads}}
}

func itoaNav(i int) string {
	if i < 10 {
		return "0" + string(rune('0'+i))
	}
	return string(rune('0'+i/10)) + string(rune('0'+i%10))
}

// TestGekapptesLogbuchSagtEsUndZaehltUeberall ist die Browser-Haelfte von
// gkp-dop, und sie prueft die beiden Aussagen, die nur ZUSAMMEN etwas wert
// sind:
//
//  1. Die Liste hoert nicht stumm auf. Am Ende steht der Satz aus Go
//     (a.gekappt) — ohne ihn ist eine gekappte Liste von einer zu Ende
//     gegangenen nicht zu unterscheiden, dasselbe Argument, aus dem Regel.Leer
//     entstanden ist.
//  2. Die Zahl neben "Logbuch" und die Zahl im Listenkopf sagen nach der
//     Kappung immer noch dasselbe. Das ist die Naht aus gkp-d52: dort standen
//     47 und 57 nebeneinander, und eine Kappung ist genau die Gelegenheit, an
//     der eine zweite Zaehlung entsteht ("die ungekappte Laenge steht doch noch
//     da").
func TestGekapptesLogbuchSagtEsUndZaehltUeberall(t *testing.T) {
	nodePath, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node nicht installiert")
	}

	jetzt := navzahlZeit()
	maps := navzahlLogbuchBeads()
	ansichten := appctx.AnsichtenBauen(maps, jetzt)
	lb := ansichten[appctx.LogbuchID]

	// Waechter VOR dem Browser-Teil, und hier gleich zwei: taugt die Fixture,
	// und greift die Kappung ueberhaupt? Ohne das koennte der Lauf unten zwei
	// uebereinstimmende Zahlen melden, weil gar nichts gekappt wurde.
	if lb.Gekappt == "" {
		t.Fatalf("Fixture taugt nicht: das Logbuch mit %d Zeilen ist nicht gekappt",
			len(lb.Eintraege))
	}
	if len(lb.Eintraege) != 100 {
		t.Fatalf("Logbuch hat %d Zeilen, erwartet die Kappung auf 100", len(lb.Eintraege))
	}

	antwort := appctx.AnsichtenAntwort(ansichten, "")
	data := appctx.BuildData(map[string]any{"cards": map[string]any{
		"reports": map[string]any{"ansichten": ansichten},
	}}, "project")

	script := navzahlHarness(t) + `
var NAV_START = ` + navzahlJSON(t, data["nav"]) + `;
var REPORTS_START = { maps: ` + navzahlJSON(t, maps) + `, ansichten: ` + navzahlJSON(t, ansichten) + ` };
var ANTWORT_MIT_GITHUB = ` + navzahlJSON(t, antwort) + `;
var ANTWORT_OHNE_GITHUB = ` + navzahlJSON(t, antwort) + `;
var SATZ = ` + navzahlJSON(t, lb.Gekappt) + `;
` + navzahlLogbuchPruefungen

	pfad := filepath.Join(t.TempDir(), "logbuch_check.js")
	if err := os.WriteFile(pfad, []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(nodePath, pfad).CombinedOutput()
	if err != nil {
		t.Fatalf("node-Pruefung fehlgeschlagen: %v\n%s", err, out)
	}
	t.Logf("gemessen:\n%s", out)
}

const navzahlLogbuchPruefungen = `
/* Der Kopf steht in wrap.html, die ZEILEN stehen als Kinder daran (el() im
   Geruest haelt das gezeichnete HTML je Knoten). Fuer diesen Test ist gerade
   das Ende der Liste der Gegenstand, also wird der Knoten festgehalten und
   nicht nur sein Kopf. */
var WRAP = null;
detail.appendChild = function (c) { WRAP = c; GEZEICHNET = String(c.html || ""); };
function listeHtml() {
  return (WRAP && WRAP.kinder || []).map(function (k) { return String(k.html || ""); }).join("\n");
}

(async function () {
  NAV = JSON.parse(JSON.stringify(NAV_START));
  REPORTS = JSON.parse(JSON.stringify(REPORTS_START));
  ANTWORT = ANTWORT_MIT_GITHUB;
  AKTIV = "logbuch";
  AV_GH_ANS = null;
  AV_GH_GRUND = "";
  nav.innerHTML = "";
  navZeichnen();
  // gkp-dnz: "logbuch" steht nicht mehr in NAV (#mp-nav) — die Kopf-Navigation
  // baut sich ihre eigene Grundmenge und bekommt die Zahl erst ueber
  // navZahlenUebernehmen(j.nav), also nach dem Nachladen unten.
  VIEWNAV_ITEMS = viewNavGrundmenge();
  viewNavZeichnen();

  avZeichne("logbuch");
  var kopf = kopfZahl();
  await new Promise(function (r) { setTimeout(r, 0); });
  var navNach = viewNavZahl("logbuch");
  console.log("Logbuch: Kopf-Navigation -> " + navNach + " | Listenkopf " + kopf);

  assert(kopf === "100",
    "der Listenkopf sagt " + kopf + ", erwartet 100 — die Kappung kommt im Browser nicht an");
  assert(navNach === kopf,
    "Kopf-Navigation sagt " + navNach + ", der Listenkopf sagt " + kopf +
    " — nach einer Kappung ist das die naheliegende Stelle fuer eine zweite Zaehlung");

  /* Der Satz steht da, woertlich so, wie Go ihn geschrieben hat. */
  var gezeichnet = listeHtml();
  assert(gezeichnet.indexOf("av-gekappt") >= 0,
    "am Ende der Liste steht kein Kappungshinweis — eine Liste, die aufhoert, " +
    "ohne zu sagen warum, ist von einer Liste, die zu Ende ist, nicht zu unterscheiden");
  assert(gezeichnet.indexOf(esc(SATZ)) >= 0,
    "der gezeichnete Satz ist nicht der aus Go:\n  Go:  " + SATZ +
    "\n  DOM: " + gezeichnet.slice(-600));

  /* Und er steht am ENDE, nicht irgendwo. Oben faellt nicht auf, dass etwas
     fehlt; unten ist die Stelle, an der die Frage entsteht. */
  var letzte = gezeichnet.lastIndexOf("av-row");
  assert(letzte >= 0, "keine Zeile gezeichnet — die Liste ist leer, der Test misst nichts");
  assert(gezeichnet.indexOf("av-gekappt") > letzte,
    "der Kappungshinweis steht vor der letzten Zeile, nicht am Ende der Liste");
  console.log("Satz am Ende der Liste: " + SATZ);

  /* Gegenprobe zur Auswahl: der aelteste Tag traegt die Ids "a.."/"b..". Steht
     eine davon in der gezeichneten Liste, hat nicht der Abschlusstag
     entschieden. Gelesen wird der gezeichnete Rumpf, nicht die Nutzlast. */
  var alt = REPORTS.ansichten.logbuch.eintraege.filter(function (e) {
    return e.bead[0] === "a" || e.bead[0] === "b";
  });
  assert(alt.length === 0,
    alt.length + " Zeilen der zwei aeltesten Tage stehen in der gekappten Liste");
})().catch(function (e) {
  console.error(String(e && e.stack || e));
  process.exit(1);
});
`
