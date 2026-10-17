package assets

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/niclasedge/git-planner-go/internal/appctx"
	"github.com/niclasedge/git-planner-go/internal/areas"
)

// Der Gegenbeweis zu appctx.bereicheNav, an der anderen Haelfte derselben
// Aussage.
//
// In Go steht der Vorsatz woertlich (TestBereicheNavOhneOffenePflegeTraegtKeineZahl):
// ist jedes Projekt zugeordnet, traegt der Eintrag GAR KEINE Zahl — "eine Null
// waere eine Aufforderung ohne Anlass". Der Browser machte daraus trotzdem
// eine: navCount fiel bei fehlendem `cnt` auf COLL[it.id] zurueck, und
// "bereiche" steht in COLL nicht (builddata.go baut coll ohne diesen
// Schluessel). Der letzte Zweig lieferte dann 0, und navRow graute die Zeile
// mit is-empty aus.
//
// Gemessen am ausgelieferten Code, vor der Reparatur:
//
//	A eine ohne Bereich  -> nc="1"  is-empty=false
//	B ohne offene Pflege -> nc="0"  is-empty=true
//
// Die beiden Faelle stehen deshalb zusammen in EINEM Lauf: A allein bliebe
// gruen, egal was der leere Fall tut, und B allein koennte gruen bleiben, weil
// gar keine Navigation gezeichnet wurde.
//
// Der dritte Fall im selben Lauf ist der, den die Reparatur NICHT kaputtmachen
// darf: eine Ansicht, die wirklich leer ist, zeigt weiterhin die 0 und bleibt
// ausgegraut. "Die Zahl ist null" und "es gibt keine Zahl" sind zwei Aussagen,
// und nur eine davon ist eine Aufforderung.
func TestNavigationszahlErfindetKeineNullWoGoKeineZahlLiefert(t *testing.T) {
	nodePath, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node nicht installiert")
	}

	mitPflege := navzahlBereicheData(t,
		areas.State{Areas: []string{"Arbeit"}, Zuordnung: map[string]string{}},
		"alpha-tool")
	ohnePflege := navzahlBereicheData(t,
		areas.State{Areas: []string{"Arbeit"}, Zuordnung: map[string]string{"alpha-tool": "Arbeit"}},
		"alpha-tool")

	// Waechter VOR dem Browser-Teil, und beide sind der Kern der Sache:
	// traegt die eine Nutzlast eine Zahl und die andere keine? Waeren sie
	// gleich, koennte der Lauf unten nichts unterscheiden und bliebe gruen,
	// ohne etwas gesehen zu haben.
	if got := navzahlItemCnt(t, mitPflege); got != "1" {
		t.Fatalf("Fixture taugt nicht: bei einem Projekt ohne Bereich erwartet cnt=\"1\", bekommen %q", got)
	}
	if got := navzahlItemCnt(t, ohnePflege); got != "" {
		t.Fatalf("Fixture taugt nicht: ist alles zugeordnet, darf kein cnt kommen, bekommen %q", got)
	}
	// Und die Voraussetzung, aus der der Defekt ueberhaupt entstand: "bereiche"
	// hat keinen Eintrag in COLL. Bekaeme es einen, waere dieser Test ab sofort
	// die Pruefung eines anderen Wegs — und muesste umgeschrieben, nicht
	// stillschweigend gruen bleiben.
	if coll, _ := ohnePflege["coll"].(map[string]any); coll != nil {
		if _, da := coll[appctx.BereicheID]; da {
			t.Fatalf("COLL traegt inzwischen einen Eintrag %q — der Rueckfall in navCount greift dann, "+
				"und dieser Test misst nicht mehr den gemeinten Weg", appctx.BereicheID)
		}
	}

	script := navzahlStubs +
		navzahlSchnitt(t, "  function navCount(it) {", "  /* ---------- select a nav entry ---------- */") + `
var NAV_MIT = ` + navzahlJSON(t, mitPflege["nav"]) + `;
var NAV_OHNE = ` + navzahlJSON(t, ohnePflege["nav"]) + `;
var COLL_ECHT = ` + navzahlJSON(t, ohnePflege["coll"]) + `;
var BEREICHE_ID = ` + navzahlJSON(t, appctx.BereicheID) + `;
` + navzahlLeerPruefungen

	pfad := filepath.Join(t.TempDir(), "navzahl_leer_check.js")
	if err := os.WriteFile(pfad, []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(nodePath, pfad).CombinedOutput()
	if err != nil {
		t.Fatalf("node-Pruefung fehlgeschlagen: %v\n%s", err, out)
	}
	t.Logf("gemessen:\n%s", out)
}

// navzahlBereicheData baut DATA so, wie der Seitenaufbau es tut — ueber
// BuildData und nicht ueber bereicheNav direkt. Zwischen den beiden liegt die
// Gruppe "Aufgaben" in builddata.go, und der Browser liest genau diese Form.
func navzahlBereicheData(t *testing.T, st areas.State, namen ...string) map[string]any {
	t.Helper()
	pfad := filepath.Join(t.TempDir(), "areas.json")
	t.Setenv("CC_AREAS_FILE", pfad)
	if err := areas.Open(pfad).Save(st); err != nil {
		t.Fatal(err)
	}
	var ps []any
	for _, n := range namen {
		ps = append(ps, map[string]any{"name": n, "path": "/tmp/" + n})
	}
	return appctx.BuildData(map[string]any{"projects": ps, "cards": map[string]any{}}, "project")
}

// navzahlItemCnt liest das Feld cnt des Bereiche-Eintrags aus DATA.nav. Leerer
// String heisst: das Feld ist gar nicht da — genau der Fall, um den es geht.
func navzahlItemCnt(t *testing.T, data map[string]any) string {
	t.Helper()
	// Der Umweg ueber JSON ist Absicht: DATA.nav mischt []any und
	// []map[string]any, und der Browser sieht ohnehin nur das serialisierte
	// Ergebnis. Ein Typwechsel in builddata.go darf diesen Test nicht
	// stillschweigend blind machen.
	roh, err := json.Marshal(data["nav"])
	if err != nil {
		t.Fatal(err)
	}
	var gruppen []struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(roh, &gruppen); err != nil {
		t.Fatal(err)
	}
	treffer := 0
	cnt := ""
	for _, g := range gruppen {
		for _, it := range g.Items {
			if s, _ := it["id"].(string); s != appctx.BereicheID {
				continue
			}
			treffer++
			if v, da := it["cnt"]; da {
				cnt, _ = v.(string)
			}
		}
	}
	if treffer > 1 {
		t.Fatalf("der Eintrag %q steht %dx in DATA.nav — der Lauf unten verlangt genau eine Zeile",
			appctx.BereicheID, treffer)
	}
	if treffer == 0 {
		t.Fatalf("in DATA.nav steht kein Eintrag %q", appctx.BereicheID)
	}
	return cnt
}

const navzahlLeerPruefungen = `
/* Die ganze gezeichnete Zeile, nicht nur ihre Zahl: hier ist gerade die
   Ausgrau-Klasse der Gegenstand, und die steht am Rahmen, nicht in .nc. */
function navZeile(id) {
  var treffer = [];
  function ab(knoten) {
    (knoten || []).forEach(function (k) {
      var h = String(k.html || "");
      if (h.indexOf('data-id="' + id + '"') >= 0) { treffer.push(h); }
      ab(k.kinder);
    });
  }
  ab(nav.kinder);
  assert(treffer.length === 1,
    "erwartet genau eine Navigationszeile fuer " + id + ", gefunden " + treffer.length);
  return treffer[0];
}
function nc(html) {
  var m = html.match(/class="nc">([\s\S]*?)<\/span>/);
  assert(m, "die Zeile zeigt keine Zelle fuer die Zahl: " + html);
  return m[1];
}
function leer(html) { return html.indexOf("is-empty") >= 0; }

function zeichne(navDaten, coll) {
  NAV = JSON.parse(JSON.stringify(navDaten));
  COLL = JSON.parse(JSON.stringify(coll));
  nav.innerHTML = "";
  navZeichnen();
}

/* --- A: eine offene Pflege. Go traegt cnt="1", und das muss dastehen. --- */
zeichne(NAV_MIT, COLL_ECHT);
var a = navZeile(BEREICHE_ID);
console.log("A eine ohne Bereich  -> nc=" + JSON.stringify(nc(a)) + "  is-empty=" + leer(a));
assert(nc(a) === "1", "erwartet die 1 aus Go, gezeichnet: " + JSON.stringify(nc(a)));
assert(!leer(a), "eine offene Pflege darf nicht ausgegraut sein: " + a);

/* --- B: nichts offen. Go traegt KEIN cnt — also darf hier keine Zahl
       stehen, und schon gar keine 0. --- */
zeichne(NAV_OHNE, COLL_ECHT);
var b = navZeile(BEREICHE_ID);
console.log("B ohne offene Pflege -> nc=" + JSON.stringify(nc(b)) + "  is-empty=" + leer(b));
assert(nc(b).indexOf("0") < 0,
  "der Browser erfindet eine Null, wo Go absichtlich keine Zahl liefert — " +
  "eine Null waere eine Aufforderung ohne Anlass (appctx.bereicheNav): " + JSON.stringify(nc(b)));
assert(nc(b).replace(/\s/g, "") === "",
  "erwartet eine leere Zelle, gezeichnet: " + JSON.stringify(nc(b)));
assert(!leer(b),
  "die Zeile ist mit is-empty ausgegraut, obwohl gar nichts gezaehlt wurde — " +
  "ausgegraut heisst 'hier ist nichts drin', und das ist eine Behauptung ueber " +
  "einen Inhalt, den niemand gezaehlt hat: " + b);

/* --- C: die Gegenprobe zur Reparatur. Eine Ansicht, die WIRKLICH leer ist,
       zeigt weiterhin die 0 und bleibt ausgegraut. Ohne diesen Fall koennte
       navCount kuenftig ueberall die Zahl weglassen und B bliebe gruen. --- */
zeichne([{ g: "Test", items: [{ id: "leer1", label: "Leer" }, { id: "leer2", label: "Auch leer" }] }],
        { leer1: { items: [] }, leer2: { items: [] } });
var c = navZeile("leer1");
console.log("C leere Sammlung     -> nc=" + JSON.stringify(nc(c)) + "  is-empty=" + leer(c));
assert(nc(c) === "0", "eine leere Sammlung muss ihre 0 weiterhin zeigen: " + JSON.stringify(nc(c)));
assert(leer(c), "eine leere Sammlung muss weiterhin ausgegraut sein: " + c);
`
