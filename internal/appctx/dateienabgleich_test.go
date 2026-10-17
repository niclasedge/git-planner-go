package appctx

// Zusage gegen Diff je Change (dateien-abgleich, #53): die zugesagten Dateien
// aus proposal.md und tasks.md, die drei Listen gedeckt / ohne Zusage / ohne
// Aenderung, die Capabilities und ob die Kopie vom Default-Branch abweicht.
// Ohne Browser und ohne Netz.

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func abgleichListe(t *testing.T, was string, ist, soll []string) {
	t.Helper()
	if ist == nil {
		t.Errorf("%s ist nil, erwartet eine Liste (auch leer)", was)
		return
	}
	if !reflect.DeepEqual(ist, soll) {
		t.Errorf("%s = %q, erwartet %q", was, ist, soll)
	}
}

func TestZugesagteDateienAusVorschlagUndAufgaben(t *testing.T) {
	proposal := "# k\n\n## Warum\n\nHeute fehlt `ops.py`.\n\n## Beruehrte Dateien\n\n" +
		"- `a.go` (neu)\n- `web/`\n- `just gate`\n- `-x`\n- `a.go`\n\nNicht beruehrt: `z.go`\n\n## Danach\n\n- `n.go`\n"
	tasks := "- [ ] Test `a_test.go` schreibt die Erwartung\n- [ ] `just gate2 k` ist rot\n- [ ] `b.go` bauen\n"
	abgleichListe(t, "zugesagt", ZugesagteDateien(proposal, tasks), []string{"a.go", "a_test.go", "web/"})

	mitUmlaut := "## Berührte Dateien\n\n- `x.go`\n"
	abgleichListe(t, "mit Umlaut", ZugesagteDateien(mitUmlaut, ""), []string{"x.go"})
	abgleichListe(t, "ohne Abschnitt", ZugesagteDateien("## Warum\n\n- `a.go`\n", ""), []string{})
}

func TestDateienAbgleichDreiLagen(t *testing.T) {
	d := DateienAbgleichen([]string{"web/", "b.go", "a.go"},
		[]string{"web/x.js", "c.go", "a.go", "openspec/changes/k/tasks.md", "openspec/specs/k/spec.md"})
	abgleichListe(t, "zugesagt", d.Zugesagt, []string{"a.go", "b.go", "web/"})
	abgleichListe(t, "gedeckt", d.Gedeckt, []string{"a.go", "web/"})
	abgleichListe(t, "ohne_aenderung", d.OhneAenderung, []string{"b.go"})
	abgleichListe(t, "ohne_zusage", d.OhneZusage, []string{"c.go"})
	if d.Grund != "" {
		t.Errorf("Grund %q, erwartet leer", d.Grund)
	}

	// Die Ablaufartefakte duerfen eine Ordnerzusage einloesen.
	o := DateienAbgleichen([]string{"openspec/changes/k/"}, []string{"openspec/changes/k/tasks.md"})
	abgleichListe(t, "Ordnerzusage gedeckt", o.Gedeckt, []string{"openspec/changes/k/"})
	abgleichListe(t, "Ordnerzusage ohne_aenderung", o.OhneAenderung, []string{})

	leer := DateienAbgleichen(nil, nil)
	abgleichListe(t, "leer gedeckt", leer.Gedeckt, []string{})
	abgleichListe(t, "leer ohne_zusage", leer.OhneZusage, []string{})
	abgleichListe(t, "leer ohne_aenderung", leer.OhneAenderung, []string{})
	abgleichListe(t, "leer zugesagt", leer.Zugesagt, []string{})
}

func TestChangeAbgleichOhneDiff(t *testing.T) {
	a := ChangeAbgleichBauen(ChangeAbgleichEingabe{
		Proposal: "## Beruehrte Dateien\n\n- `a.go`\n", Standard: "main", Ref: "main",
		DiffGrund: "kein Branch change/k",
	})
	if a.Dateien.Grund != "kein Branch change/k" {
		t.Errorf("Grund %q", a.Dateien.Grund)
	}
	abgleichListe(t, "zugesagt", a.Dateien.Zugesagt, []string{"a.go"})
	abgleichListe(t, "gedeckt", a.Dateien.Gedeckt, []string{})
	abgleichListe(t, "ohne_zusage", a.Dateien.OhneZusage, []string{})
	abgleichListe(t, "ohne_aenderung", a.Dateien.OhneAenderung, []string{})
}

func TestChangeAbgleichKopien(t *testing.T) {
	t.Run("Branch weicht ab", func(t *testing.T) {
		a := ChangeAbgleichBauen(ChangeAbgleichEingabe{
			Standard: "main", Ref: "change/k",
			Haupt: map[string]string{"tasks.md": "s1", "proposal.md": "p"},
			Ast:   map[string]string{"tasks.md": "s2", "proposal.md": "p"},
		})
		if a.Unveraendert {
			t.Errorf("abweichende Kopie als unveraendert gemeldet")
		}
		abgleichListe(t, "auch_in", a.AuchIn, []string{"main"})
	})
	t.Run("Branch gleich main", func(t *testing.T) {
		m := map[string]string{"tasks.md": "s1"}
		a := ChangeAbgleichBauen(ChangeAbgleichEingabe{Standard: "main", Ref: "change/k", Haupt: m, Ast: map[string]string{"tasks.md": "s1"}})
		if !a.Unveraendert {
			t.Errorf("inhaltsgleiche Kopie nicht unveraendert")
		}
		abgleichListe(t, "auch_in", a.AuchIn, []string{})
	})
	t.Run("nur auf dem Branch", func(t *testing.T) {
		a := ChangeAbgleichBauen(ChangeAbgleichEingabe{Standard: "main", Ref: "change/k", Ast: map[string]string{"tasks.md": "s1"}})
		if a.Unveraendert {
			t.Errorf("neuer Change auf dem Branch als unveraendert gemeldet")
		}
		abgleichListe(t, "auch_in", a.AuchIn, []string{})
	})
	t.Run("nur auf main", func(t *testing.T) {
		a := ChangeAbgleichBauen(ChangeAbgleichEingabe{Standard: "main", Ref: "main",
			Capabilities: []string{"zwei", "eins"}, Haupt: map[string]string{"tasks.md": "s1"}})
		if !a.Unveraendert {
			t.Errorf("Change auf main nicht unveraendert")
		}
		abgleichListe(t, "auch_in", a.AuchIn, []string{})
		abgleichListe(t, "capabilities", a.Capabilities, []string{"eins", "zwei"})
	})
}

func TestChangeAbgleichJSON(t *testing.T) {
	a := ChangeAbgleichBauen(ChangeAbgleichEingabe{
		Proposal: "## Beruehrte Dateien\n\n- `a.go`\n- `b.go`\n", Standard: "main", Ref: "change/k",
		Capabilities: []string{"kern"}, Angefasst: []string{"a.go", "c.go"},
	})
	b, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, teil := range []string{`"capabilities":["kern"]`, `"unveraendert":false`, `"auch_in":[]`,
		`"gedeckt":["a.go"]`, `"ohne_zusage":["c.go"]`, `"ohne_aenderung":["b.go"]`, `"zugesagt":["a.go","b.go"]`} {
		if !strings.Contains(s, teil) {
			t.Errorf("JSON ohne %s: %s", teil, s)
		}
	}
}
