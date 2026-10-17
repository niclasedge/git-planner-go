package assets

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/niclasedge/git-planner-go/internal/appctx"
	"github.com/niclasedge/git-planner-go/internal/collectors"
)

// Der Kontext-Block ordnete seine Gruppen selbst — `orderedGroups.sort()`, das
// Konstrukt, dessentwegen gkp-7gb ueberhaupt existierte, vierhundert Zeilen von
// dem Kommentar entfernt, der erklaert, warum es entfernt wurde.
//
// Es war nicht nur eine Sortierung im Browser, es war die ZWEITE ueber
// derselben Menge: collectors.readEnabledPlugins sortiert die Plugin-Schluessel
// bereits (sort.Strings ueber "name@marketplace"), der Browser sortierte danach
// die Beschriftungen ("Plugin · " + name). Zwei verschiedene Schluessel, und sie
// laufen auseinander, sobald ein Plugin-Name den anderen als Praefix enthaelt:
// "-" ist 0x2D, "@" ist 0x40, also steht "a-b@m" vor "a@z" — die Beschriftung
// "Plugin · a" aber vor "Plugin · a-b".
//
// Die Fixture nimmt genau dieses Paar und geht den echten Weg:
// settings.json -> CollectPluginItems -> BuildData -> die Gruppenfolge im
// Browser. Ein Waechter in der Mitte prueft, dass die beiden Schluessel in
// dieser Fixture WIRKLICH auseinanderlaufen — ohne ihn waere die Gegenprobe
// untauglich, weil ein wieder eingesetztes .sort() dieselbe Folge lieferte.
func TestKontextGruppenFolgeKommtAusGo(t *testing.T) {
	nodePath, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node nicht installiert")
	}

	skills := kgPluginSkills(t)

	// Waechter 1: die Reihenfolge, die Go liefert.
	var geliefert []string
	for _, s := range skills {
		geliefert = append(geliefert, s.Group)
	}
	if len(geliefert) != 2 {
		t.Fatalf("Fixture taugt nicht: erwartet zwei Plugin-Skills, bekommen %d (%v)", len(geliefert), geliefert)
	}
	// Waechter 2: die Reihenfolge, die eine Sortierung im Browser ergaebe. Sind
	// die beiden gleich, misst der Lauf unten nichts — dieselbe Falle wie ein
	// Test ueber zwei zufaellig uebereinstimmenden Zahlen.
	nachEtikett := append([]string(nil), geliefert...)
	sort.Strings(nachEtikett)
	if strings.Join(geliefert, "|") == strings.Join(nachEtikett, "|") {
		t.Fatalf("Fixture taugt nicht: Go-Folge %v und nach Etikett sortiert %v sind gleich — "+
			"eine Gegenprobe koennte den Unterschied nicht sehen", geliefert, nachEtikett)
	}

	kategorie := kgSkillsKategorie(t, skills)

	script := kgStubs + `
var KATEGORIE = ` + navzahlJSON(t, kategorie) + `;
var GO_FOLGE = ` + navzahlJSON(t, geliefert) + `;
var NACH_ETIKETT = ` + navzahlJSON(t, nachEtikett) + `;
` + navzahlSchnitt(t, "  function ctxGruppiere(items) {", "\n  }\n") + `
}
` + kgPruefungen

	pfad := filepath.Join(t.TempDir(), "kontext_gruppen_check.js")
	if err := os.WriteFile(pfad, []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(nodePath, pfad).CombinedOutput()
	if err != nil {
		t.Fatalf("node-Pruefung fehlgeschlagen: %v\n%s", err, out)
	}
	t.Logf("gemessen:\n%s", out)
}

// kgPluginSkills legt zwei aktivierte Plugins an, deren Schluessel sich unter
// sort.Strings ANDERS ordnen als ihre Beschriftungen, und laesst den echten
// Sammler darueber laufen.
func kgPluginSkills(t *testing.T) []collectors.PluginItem {
	t.Helper()
	home := t.TempDir()

	pfade := map[string]string{}
	for _, key := range []string{"a-b@m", "a@z"} {
		name := strings.Split(key, "@")[0]
		inst := filepath.Join(home, "inst", name)
		skillDir := filepath.Join(inst, "skills", "s-"+name)
		if err := os.MkdirAll(skillDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"),
			[]byte("---\ndescription: Skill aus "+name+"\n---\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		pfade[key] = inst
	}

	kgSchreibeJSON(t, filepath.Join(home, "settings.json"), map[string]any{
		"enabledPlugins": map[string]any{"a-b@m": true, "a@z": true},
	})
	plugins := map[string]any{}
	for key, inst := range pfade {
		plugins[key] = []any{map[string]any{"installPath": inst}}
	}
	kgSchreibeJSON(t, filepath.Join(home, "plugins", "installed_plugins.json"),
		map[string]any{"plugins": plugins})

	return collectors.CollectPluginItems(home).Skills
}

func kgSchreibeJSON(t *testing.T, pfad string, v any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(pfad), 0o755); err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pfad, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

// kgSkillsKategorie fuehrt die Plugin-Skills durch BuildData und holt die
// Kategorie heraus, die der Browser zeichnet. Der Umweg ist Absicht: zwischen
// CollectPluginItems und dem Browser liegt die Schleife in builddata.go, die
// User-, Plugin- und Built-in-Eintraege aneinanderhaengt — und genau diese
// Reihenfolge ist die Aussage.
func kgSkillsKategorie(t *testing.T, skills []collectors.PluginItem) map[string]any {
	t.Helper()
	roh, err := json.Marshal(skills)
	if err != nil {
		t.Fatal(err)
	}
	var alsAny []any
	if err := json.Unmarshal(roh, &alsAny); err != nil {
		t.Fatal(err)
	}
	data := appctx.BuildData(map[string]any{
		"plugin_skills": alsAny,
		"cards":         map[string]any{},
	}, "project")

	coll, _ := data["coll"].(map[string]any)
	ctxBlock, _ := coll["context"].(map[string]any)
	kats, _ := ctxBlock["categories"].([]map[string]any)
	for _, k := range kats {
		if s, _ := k["key"].(string); s == "skills" {
			return k
		}
	}
	t.Fatalf("in COLL.context.categories steht keine Kategorie \"skills\" (%d Kategorien)", len(kats))
	return nil
}

const kgStubs = `
"use strict";
function assert(cond, msg) { if (!cond) { throw new Error("assertion failed: " + msg); } }
(function () {
`

const kgPruefungen = `
var r = ctxGruppiere(KATEGORIE.items);
console.log("Go liefert:     " + GO_FOLGE.join(", "));
console.log("nach Etikett:   " + NACH_ETIKETT.join(", "));
console.log("gezeichnet:     " + r.folge.join(", "));

/* Voraussetzung: die Kategorie traegt ueberhaupt Gruppen. Ohne das waere alles
   darunter eine Aussage ueber eine leere Liste. */
assert(r.folge.length >= 3,
  "erwartet mindestens Plugin-Gruppen und Built-in, bekommen: " + JSON.stringify(r.folge));

/* Die Plugin-Gruppen in der Folge, in der Go sie geliefert hat. */
var nurPlugins = r.folge.filter(function (g) { return g.indexOf("Plugin · ") === 0; });
assert(nurPlugins.join("|") === GO_FOLGE.join("|"),
  "die Plugin-Gruppen stehen nicht in der Reihenfolge aus Go.\n  Go:          " +
  GO_FOLGE.join(", ") + "\n  gezeichnet:  " + nurPlugins.join(", ") +
  "\n  (nach Etikett sortiert waere: " + NACH_ETIKETT.join(", ") + ")");

/* Und ausdruecklich NICHT die, die eine Sortierung im Browser ergaebe. Die
   Zeile ist redundant, solange die Waechter in Go stehen — sie steht hier,
   damit die Fehlermeldung den Unterschied benennt statt ihn zu verschweigen. */
assert(nurPlugins.join("|") !== NACH_ETIKETT.join("|"),
  "die Gruppen stehen nach ihrem Etikett sortiert — das ist die zweite Sortierung " +
  "ueber derselben Menge (gkp-7gb), und sie widerspricht der ersten in " +
  "collectors.readEnabledPlugins");

/* Built-in bleibt hinten, weil builddata.go es hinten anhaengt — abgelesen,
   nicht durch eine Sonderregel im Browser erzwungen. */
assert(r.folge[r.folge.length - 1] === "Built-in",
  "Built-in steht nicht am Ende: " + JSON.stringify(r.folge));

/* Und die Gegenprobe zur Ordnung: die Eintraege selbst muessen in den Gruppen
   angekommen sein. Eine leere Zuordnung haette jede Reihenfolge erfuellt. */
var summe = 0;
r.folge.forEach(function (g) { summe += r.gruppen[g].length; });
assert(summe === KATEGORIE.items.length,
  "in den Gruppen stehen " + summe + " von " + KATEGORIE.items.length + " Eintraegen");
})();
`
