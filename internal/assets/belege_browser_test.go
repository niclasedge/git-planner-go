package assets

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/niclasedge/git-planner-go/internal/reports"
)

// Die Browser-Haelfte von gkp-wiz: ein Datei-Beleg, den der Store beim
// Schreiben als missing markiert hat, muss in der Reports-Ansicht genauso
// deutlich als kaputt erkennbar sein wie ein fehlendes Bild.
//
// Zwei Entscheidungen in der Bauart, beide aus Spec §9.1:
//
//  1. Die Fixture wird nicht von Hand hingelegt, sondern von reports.Append
//     ERZEUGT — dem echten Schreibweg, auf dem der Marker entsteht. Fall 3 in
//     §9.1 war genau das: ein Test, der storeDoneEntry direkt aufrief und den
//     Pfad mit der Pruefung umging, prueft eine Welt, die es nicht gibt.
//     Hoert resolveProofs auf, missing zu setzen, faellt dieser Test hier auf
//     und nicht erst in der Ansicht.
//  2. Gerendert wird mit dem AUSGELIEFERTEN browser.js, aus BrowserJS
//     herausgeschnitten — dieselbe Bauart wie TestRpTeilbaumEinsetzen. Eine
//     danebengelegte Kopie driftet und bleibt nach der Regression gruen.
func TestRpProofHtmlZeigtKaputtenDateiBeleg(t *testing.T) {
	nodePath, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node nicht installiert")
	}

	proof := belegFixture(t)
	rohJSON, err := json.Marshal(proof)
	if err != nil {
		t.Fatal(err)
	}

	script := "var BELEGE = " + string(rohJSON) + ";\n" +
		schneide(t, "function esc(s) {", "\n") +
		schneide(t, "function rpImageURL(repo, absPath) {", "function rpEntryHtml(e) {") +
		belegJSPruefungen
	pfad := filepath.Join(t.TempDir(), "belege_check.js")
	if err := os.WriteFile(pfad, []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(nodePath, pfad).CombinedOutput(); err != nil {
		t.Fatalf("node-Pruefung fehlgeschlagen: %v\n%s", err, out)
	}
}

// belegFixture schreibt einen echten done-Bericht in einen Wegwerf-Store und
// gibt dessen aufgeloeste Belege zurueck. Vier Faelle, die der Store
// unterschiedlich beantwortet:
//
//	[0] docs/da.dot        — da, wird nach artefacts/ kopiert: kein Marker
//	[1] docs/nicht-da.dot  — nirgends auffindbar:              missing
//	[2] docs/leer.dot      — da, aber 0 Byte:                  missing
//	[3] internal/x.go:41   — Codeverweis ins Leere:            missing
//
// [3] ist absichtlich dabei: resolveFileProof loest JEDEN Dateiverweis auf,
// nicht nur die .dot/.svg, an denen das Schliess-Tor haengt. Ein Codeverweis
// ins Nichts traegt denselben Marker und muss in der Ansicht dasselbe Gewicht
// bekommen.
func belegFixture(t *testing.T) []reports.ProofItem {
	t.Helper()
	cwd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cwd, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cwd, "docs", "da.dot"), []byte("digraph{a->b}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cwd, "docs", "leer.dot"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CC_REPORTS_DIR", t.TempDir()) // NIE der echte Store unter ~/.claude/reports

	entry, err := reports.Append(reports.Payload{
		Cwd:   cwd,
		Map:   "gkp-wegwerf",
		Bead:  "gkp-wegwerf.1",
		Kind:  "done",
		Title: "Fixture fuer gkp-wiz",
		Proof: []reports.ProofItem{
			{"file": "docs/da.dot"},
			{"file": "docs/nicht-da.dot"},
			{"file": "docs/leer.dot"},
			{"file": "internal/x.go:41"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Die Fixture selbst gegenpruefen. Ohne das koennte der node-Teil gruen
	// bleiben, weil gar kein Marker da ist — ein Test ueber einer leeren
	// Voraussetzung ist genau das Muster aus §9.1.
	if len(entry.Proof) != 4 {
		t.Fatalf("vier Belege erwartet, %d bekommen", len(entry.Proof))
	}
	if m, _ := entry.Proof[0]["missing"].(bool); m {
		t.Fatalf("ein vorhandener .dot darf nicht als missing gelten: %v", entry.Proof[0])
	}
	if c, _ := entry.Proof[0]["copy"].(string); c == "" {
		t.Fatalf("ein vorhandener .dot sollte unter artefacts/ kopiert sein: %v", entry.Proof[0])
	}
	for _, i := range []int{1, 2, 3} {
		if m, _ := entry.Proof[i]["missing"].(bool); !m {
			t.Fatalf("Beleg %d sollte vom Store als missing markiert sein: %v", i, entry.Proof[i])
		}
	}
	return entry.Proof
}

// schneide holt einen Abschnitt aus der ausgelieferten browser.js, von `von`
// bis AUSSCHLIESSLICH `bis` — der Endmarker ist der Anfang des naechsten
// Abschnitts und gehoert nicht mehr dazu.
func schneide(t *testing.T, von, bis string) string {
	t.Helper()
	start := strings.Index(BrowserJS, von)
	if start < 0 {
		t.Fatalf("Marker %q nicht gefunden — beim Umbauen dieses Abschnitts den Marker hier mitziehen", von)
	}
	end := strings.Index(BrowserJS[start:], bis)
	if end < 0 {
		t.Fatalf("Endmarker %q nach %q nicht gefunden", bis, von)
	}
	return BrowserJS[start:start+end] + "\n"
}

const belegJSPruefungen = `
function assert(cond, msg) { if (!cond) { throw new Error("assertion failed: " + msg); } }

var html = BELEGE.map(function (p) { return rpProofHtml(p, "git-planner"); });

// Der Massstab ist der Bild-Zweig: "dieselbe Deutlichkeit" heisst dieselbe
// Auszeichnung, nicht eine zweite, die zufaellig auch rot aussieht.
var bildKaputt = rpProofHtml({ image: "/weg/bild.png", missing: true }, "git-planner");
assert(/rp-proof-missing/.test(bildKaputt), "Voraussetzung: der Bild-Zweig zeichnet ein fehlendes Bild aus");

// 1. Der vorhandene .dot bleibt ein normaler Datei-Beleg.
assert(/rp-proof-file/.test(html[0]), "ein auffindbarer .dot muss als Datei-Beleg erscheinen: " + html[0]);
assert(!/rp-proof-missing/.test(html[0]), "ein auffindbarer .dot darf nicht als kaputt erscheinen: " + html[0]);
assert(html[0].indexOf("docs/da.dot") >= 0, "der Pfad fehlt im Beleg: " + html[0]);
assert(html[0].indexOf(esc(BELEGE[0].copy)) >= 0, "der Vermerk auf die Kopie unter artefacts/ fehlt: " + html[0]);
assert(!/rp-proof-file"><code title/.test(rpProofHtml({ file: "internal/x.go" }, "r")), "ohne Kopie kein leerer title");

// 2./3./4. Jeder markierte Beleg traegt dieselbe Auszeichnung wie ein
//          fehlendes Bild — und nennt weiterhin den Pfad, sonst weiss der
//          Leser nicht, WAS fehlt.
[1, 2, 3].forEach(function (i) {
  assert(/rp-proof-missing/.test(html[i]), "Beleg " + i + " ist nicht als kaputt ausgezeichnet: " + html[i]);
  assert(!/class="rp-proof rp-proof-file"/.test(html[i]), "Beleg " + i + " erscheint trotz Marker als heiler Datei-Beleg: " + html[i]);
  assert(html[i].indexOf(esc(BELEGE[i].file)) >= 0, "Beleg " + i + " nennt den Pfad nicht: " + html[i]);
});

// 5. Der Pfad wird maskiert. Ein Dateiname darf kein Markup oeffnen, egal in
//    welchem Zweig — der kaputte Zweig ist neu und damit der ungeprueftere.
var boes = rpProofHtml({ file: '<img src=x onerror=alert(1)>.dot', missing: true }, "r");
assert(boes.indexOf("<img") < 0, "der kaputte Zweig maskiert den Pfad nicht: " + boes);
`
