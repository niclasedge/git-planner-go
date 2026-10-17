package assets

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// rohBlock ist der Ansichten-Block MIT Kommentaren und Zeichenketten — fuer
// die Fragen, deren Antwort in einer Zeichenkette steht (eine Route etwa).
// ansichtenBlock nebenan schneidet beides heraus und taugt dafuer nicht.
func rohBlock(t *testing.T) string {
	t.Helper()
	start := strings.Index(BrowserJS, avStart)
	end := strings.Index(BrowserJS, avEnde)
	if start < 0 || end < 0 || end <= start {
		t.Fatal("Marker um den Ansichten-Block nicht gefunden")
	}
	return BrowserJS[start : end+len(avEnde)]
}

// rumpfVon schneidet eine Funktion aus dem ausgelieferten Code heraus.
func rumpfVon(t *testing.T, start, ende string) string {
	t.Helper()
	i := strings.Index(BrowserJS, start)
	if i < 0 {
		t.Fatalf("%q nicht gefunden — beim Umbauen den Marker hier mitziehen", start)
	}
	j := strings.Index(BrowserJS[i:], ende)
	if j < 0 {
		t.Fatalf("Ende von %q nicht gefunden", start)
	}
	return BrowserJS[i : i+j]
}

// Die Browser-Haelfte von gkp-pf2: eine Aufgabe, die NICHT aus beads kommt,
// steht nicht in REPORTS.maps — nach E1 laufen die beiden Quellen
// nebeneinander, und die GitHub-Seite wird bewusst nicht in den beads-Baum
// gemischt. avBeadSuchen findet sie deshalb nie.
//
// Ohne einen zweiten Weg landete jede GitHub-Zeile im Zweig "Zeile nicht im
// geladenen Baum" — die Liste waere vollstaendig und trotzdem unlesbar, und
// zwar genau in dem Fall, fuer den dieses Ticket existiert.
//
// Am AUSGELIEFERTEN Code, nicht an einer Kopie: dieselbe Bauart wie
// TestAvBeadSuchenFindetJedeTiefe nebenan.
func TestAvFremdeAufgabeBautDieZeileAusDemEintrag(t *testing.T) {
	nodePath, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node nicht installiert")
	}
	const start = "  function avFremdeAufgabe(e) {"
	const ende = "\n  }\n"
	i := strings.Index(BrowserJS, start)
	if i < 0 {
		t.Fatal("avFremdeAufgabe nicht gefunden — beim Umbauen den Marker hier mitziehen")
	}
	j := strings.Index(BrowserJS[i:], ende)
	if j < 0 {
		t.Fatal("Ende von avFremdeAufgabe nicht gefunden")
	}
	script := BrowserJS[i:i+j+len(ende)] + `
/* Eine beads-Zeile traegt keinen Titel: der Baum hat sie, sie wird hier NICHT
   ersetzt — sonst stuende in der Liste eine Zeile ohne Status und ohne
   Fortschritt, wo eine vollstaendige moeglich waere. */
if (avFremdeAufgabe({ bead: "gkp-1", herkunft: "beads" }) !== null) {
  throw new Error("beads-Zeile wird durch einen Platzhalter ersetzt");
}
var b = avFremdeAufgabe({ bead: "#209", herkunft: "github", titel: "Traefik", status: "open" });
if (!b || b.title !== "Traefik" || b.status !== "open" || b.id !== "#209") {
  throw new Error("GitHub-Zeile = " + JSON.stringify(b));
}
if (b.extern !== true) {
  throw new Error("die fremde Zeile ist nicht als lesend markiert: " + JSON.stringify(b));
}
`
	pfad := filepath.Join(t.TempDir(), "av_fremd.js")
	if err := os.WriteFile(pfad, []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(nodePath, pfad).CombinedOutput(); err != nil {
		t.Fatalf("node-Pruefung fehlgeschlagen: %v\n%s", err, out)
	}
}

// TestFremdeZeileBietetKeinSchreibenAn haelt Spec §7 fest: GitHub-Aufgaben
// sind in Ausbaustufe 1 LESEND. "Termin" und "Schließen" schreiben ueber
// bdops nach beads; an einer GitHub-Zeile waeren sie ein Angebot, das die
// Route nur ablehnen koennte.
//
// avZeile wird dafuer WIRKLICH ausgefuehrt und ihr HTML gelesen. Die erste
// Fassung dieses Tests las stattdessen die Quelle und verglich die Stellen von
// "b.extern" und "data-av-akt=" — sie blieb gruen, als die Knoepfe wieder
// bedingungslos gezeichnet wurden, weil das ERSTE b.extern (an rpPrio) weiter
// davor stand. Ein Leerlauftest nach Muster 1 in Spec §9.1, gefunden durch die
// Gegenprobe und nicht durch den Lauf.
//
// el() wird durch einen Stummel ersetzt, der das HTML durchreicht statt einen
// DOM-Knoten zu bauen: dieser Test fragt nach dem, was gezeichnet WIRD, nicht
// danach, was der Browser daraus macht.
func TestFremdeZeileBietetKeinSchreibenAn(t *testing.T) {
	nodePath, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node nicht installiert")
	}
	i := strings.Index(BrowserJS, "  function avZeile(ansichtId, e, b) {")
	if i < 0 {
		t.Fatal("avZeile nicht gefunden — beim Umbauen den Marker hier mitziehen")
	}
	j := strings.Index(BrowserJS[i:], "\n  function avBinden(")
	if j < 0 {
		t.Fatal("Ende von avZeile nicht gefunden")
	}
	script := `
var RP_TIEFE_MAX = 4;
function el(html) { return { html: String(html) }; }
function esc(s) { return String(s == null ? "" : s); }
function rpBadge(cls, txt) { return "[" + txt + "]"; }
function rpStatusCls(s) { return ""; }
function rpPrio(p) { return "[P" + p + "]"; }
function rpFortschritt(c, was) { return "[fortschritt]"; }
/* Was die Gruende kosten, misst gruende_browser_test.go; hier zaehlt nur, dass
   avZeile sie ueberhaupt zeichnen kann. */
function avGrundAbzeichen(g) { return "[" + g + "]"; }
function avTageText(n) { return ""; }
function avKarteSuchen(id) { return null; }
` + BrowserJS[i:i+j] + `
var github = avZeile("heute", {
  bead: "#209", karte: "example-user/infra", herkunft: "github",
  titel: "Traefik", status: "open", url: "https://github.com/example-user/infra/issues/209",
  tiefe: 0, gruende: [],
}, { id: "#209", title: "Traefik", status: "open", extern: true }).html;

var bead = avZeile("heute", {
  bead: "gkp-1", karte: "gkp-ar7", herkunft: "beads", tiefe: 0, gruende: [],
}, { id: "gkp-1", title: "Ein Bead", status: "open", priority: 1 }).html;

/* Spec §7: GitHub ist in Ausbaustufe 1 lesend. Ein Knopf waere ein Angebot,
   das die Schreibroute nur ablehnen koennte. */
if (github.indexOf("data-av-akt") >= 0) {
  throw new Error("die GitHub-Zeile traegt Schreibknoepfe:\n" + github);
}
/* Und die Gegenprobe im selben Lauf: an einer beads-Zeile MUESSEN sie stehen,
   sonst ist das Verbot oben auch erfuellt, wenn gar nichts gezeichnet wird. */
if (bead.indexOf('data-av-akt="termin"') < 0 || bead.indexOf('data-av-akt="schliessen"') < 0) {
  throw new Error("die beads-Zeile hat keine Schreibknoepfe mehr:\n" + bead);
}
/* GitHub kennt keine Prioritaet. Go setzt bds Mitte, damit die Sortierung
   nicht kippt — gezeichnet wird die Zahl deshalb nicht. */
if (github.indexOf("[P") >= 0) {
  throw new Error("die GitHub-Zeile zeigt ein erfundenes Prioritaets-Abzeichen:\n" + github);
}
if (bead.indexOf("[P1]") < 0) {
  throw new Error("die beads-Zeile zeigt ihre Prioritaet nicht mehr:\n" + bead);
}
/* Der Titel ist der Link — zusammengesetzt hat ihn Go, nicht der Browser. */
if (github.indexOf('href="https://github.com/example-user/infra/issues/209"') < 0) {
  throw new Error("die GitHub-Zeile verlinkt ihr Issue nicht:\n" + github);
}
if (bead.indexOf("href=") >= 0) {
  throw new Error("die beads-Zeile hat einen Link bekommen:\n" + bead);
}
/* Und das Abzeichen, um das es in diesem Ticket geht. */
if (github.indexOf("[github]") < 0 || bead.indexOf("[beads]") < 0) {
  throw new Error("Herkunfts-Abzeichen fehlt:\n" + github + "\n---\n" + bead);
}
`
	pfad := filepath.Join(t.TempDir(), "av_zeile.js")
	if err := os.WriteFile(pfad, []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(nodePath, pfad).CombinedOutput(); err != nil {
		t.Fatalf("node-Pruefung fehlgeschlagen: %v\n%s", err, out)
	}
}

// TestAnsichtenHolenGithubUeberEigeneRoute: die zweite Quelle kommt nach, nicht
// mit. Der Netzaufruf gehoert nicht in den Pfad, der ein Projekt oeffnet
// (gkp-isj) — dieselbe Begruendung, aus der GET /github und GET /plan-svg
// eigene Routen sind.
//
// Und sie wird EINGESETZT, nicht zusammengefuegt: der Browser ersetzt
// REPORTS.ansichten durch die fertige Liste. Stuende hier ein concat oder ein
// push in a.eintraege, waere die Reihenfolge zwischen beiden Quellen im
// Browser entschieden — genau die Rechnung, die TestAnsichtenBlockRechnetNicht
// nebenan verbietet.
func TestAnsichtenHolenGithubUeberEigeneRoute(t *testing.T) {
	// Die Route steht in einer Zeichenkette, also am rohen Block gemessen;
	// die Verbote unten am kommentarfreien, sonst schlaegt die Begruendung
	// daneben an (dieselbe Falle wie bei ohneKommentare nebenan).
	roh := rohBlock(t)
	if !strings.Contains(roh, `"/ansichten?"`) {
		t.Error("die Ansicht holt die zweite Quelle nicht nach — GitHub-Aufgaben erscheinen dann nie")
	}
	// Dass die Funktion EXISTIERT, sagt nichts: die erste Fassung dieses Tests
	// blieb gruen, als der Aufruf in avZeichne entfernt war (Leerlauftest nach
	// Muster 1 in Spec §9.1, gefunden durch die Gegenprobe). Gemessen wird
	// deshalb die AUFRUFSTELLE, im Rumpf von avZeichne.
	//
	// Das Ende ist die schliessende Klammer auf Einrueckung 2 und NICHT die
	// naechste Funktion: zwischen avZeichne und avZeile stehen die beiden
	// Helfer, und mit ihnen im Ausschnitt war der Test auch dann gruen, wenn
	// avZeichne sie gar nicht mehr aufrief — sie waren ja definiert. Dieselbe
	// Bauart wie in TestSelectKenntKeinenEinzelnenAnsichtsnamen nebenan.
	az := rumpfVon(t, "  function avZeichne(id) {", "\n  }\n")
	if !strings.Contains(az, "avGithubNachziehen(") {
		t.Error("avZeichne holt die zweite Quelle nicht nach — GitHub-Aufgaben erscheinen dann nie")
	}
	if !strings.Contains(az, "avFremdeAufgabe(") {
		t.Error("avZeichne kennt die fremde Zeile nicht — jede GitHub-Aufgabe faellt in den Zweig \"nicht im geladenen Baum\"")
	}
	block := ansichtenBlock(t)
	if !strings.Contains(block, "REPORTS.ansichten = j.ansichten") {
		t.Error("die nachgeholte Liste wird nicht eingesetzt")
	}
	for _, muster := range []string{".concat(", "eintraege.push("} {
		if strings.Contains(block, muster) {
			t.Errorf("der Ansichten-Block enthaelt %q — die beiden Quellen werden in Go gemischt, nicht hier", muster)
		}
	}
}
