package appctx

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/niclasedge/git-planner-go/internal/areas"
)

// bereicheText macht die Gruppierung zu einer VOLLSTAENDIGEN Zeilenfolge.
//
// Absichtlich keine Karte erwarteter Positionen: darin ist eine FEHLENDE
// Gruppe von einer leeren nicht zu unterscheiden, und genau daran ist
// TestHeuteRuecktUnteraufgabenEin schon einmal blind geworden (Spec §9.1,
// Fall 4). Hier steht jede Gruppe und jedes Projekt in der Reihenfolge, in der
// sie herauskommen — eine verschluckte Zeile faellt damit auf.
func bereicheText(gs []BereichGruppe) string {
	var b strings.Builder
	for _, g := range gs {
		b.WriteString(g.Bereich)
		if g.Ohne {
			b.WriteString(" [sammel]")
		}
		b.WriteString(":")
		for _, p := range g.Projekte {
			b.WriteString(" " + p.Name)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func projekte(namen ...string) []BereichProjekt {
	out := make([]BereichProjekt, 0, len(namen))
	for _, n := range namen {
		out = append(out, BereichProjekt{Name: n, Pfad: "/tmp/" + n})
	}
	return out
}

// Die Gruppierung: Dateiordnung der Areas, Projekte nach Namen, Sammel-Area
// zuletzt.
//
// Die Fixture laeuft ABSICHTLICH gegen jede Reihenfolge, die sich ohne die
// geprueften Regeln einstellen koennte (Spec §9.1, der Befund zu Fall 2/5):
//
//   - Die Areas stehen in der Datei als "Infrastruktur", "Arbeit" — also
//     GEGEN die alphabetische Folge. Wer sie sortiert, faellt auf.
//   - Die Projekte kommen als zeta, alpha, omega, Kappa, beta herein — also
//     weder in der erwarteten noch in einer sortierten Folge. Wer die
//     Eingabereihenfolge durchreicht, faellt auf.
//   - "alpha-tool" und "Kappa-Dienst" liegen in derselben Gruppe und trennen
//     den Byte-Vergleich vom Vergleich ohne Ruecksicht auf die Schreibweise:
//     'K' (0x4B) liegt vor 'a' (0x61), erwartet wird trotzdem alpha vor Kappa.
//   - Die erwartete Gesamtfolge ist NICHT alphabetisch. Eine Umsetzung, die
//     alles flach sortiert und dann gruppiert, kaeme auf eine andere Antwort.
func TestBereicheGruppierenFolgtDerDateiordnung(t *testing.T) {
	st := areas.State{
		Areas: []string{"Infrastruktur", "Arbeit"},
		Zuordnung: map[string]string{
			"zeta-repo":    "Infrastruktur",
			"beta-lib":     "Infrastruktur",
			"alpha-tool":   "Arbeit",
			"Kappa-Dienst": "Arbeit",
		},
	}
	got := bereicheText(BereicheGruppieren(
		projekte("zeta-repo", "alpha-tool", "omega-app", "Kappa-Dienst", "beta-lib"), st))

	want := "Infrastruktur: beta-lib zeta-repo\n" +
		"Arbeit: alpha-tool Kappa-Dienst\n" +
		"Ohne Bereich [sammel]: omega-app\n"
	if got != want {
		t.Errorf("Gruppierung\n--- bekommen ---\n%s--- erwartet ---\n%s", got, want)
	}
}

// Die Abnahme des Beads im Wortlaut: ein Projekt ohne Zuordnung ist SICHTBAR.
//
// Der Test prueft beide Haelften getrennt, weil sie verschieden scheitern: die
// Gruppe muss da sein (sonst ist das Projekt weg), und sie muss als Sammel-Area
// markiert sein (sonst boete die Ansicht an, sie umzubenennen — eine Area, die
// in keiner Datei steht).
func TestBereicheGruppierenZeigtProjektOhneZuordnung(t *testing.T) {
	st := areas.State{Areas: []string{"Arbeit"}, Zuordnung: map[string]string{"alpha-tool": "Arbeit"}}
	gs := BereicheGruppieren(projekte("alpha-tool", "omega-app"), st)

	var sammel *BereichGruppe
	for i := range gs {
		if gs[i].Bereich == areas.OhneBereich {
			sammel = &gs[i]
		}
	}
	if sammel == nil {
		t.Fatalf("keine Gruppe %q — ein Projekt ohne Zuordnung waere unsichtbar:\n%s",
			areas.OhneBereich, bereicheText(gs))
	}
	if !sammel.Ohne {
		t.Error("die Sammel-Area ist nicht als solche markiert (Ohne=false) — die Ansicht boete an, sie umzubenennen")
	}
	if len(sammel.Projekte) != 1 || sammel.Projekte[0].Name != "omega-app" {
		t.Errorf("Sammel-Area traegt %+v, erwartet nur omega-app", sammel.Projekte)
	}
	if sammel.Projekte[0].Pfad == "" {
		t.Error("der Pfad fehlt — ohne ihn kann die Ansicht das Projekt nicht oeffnen")
	}
}

// Eine Area, die nur in `zuordnung` steht und in `areas` fehlt, darf ihr
// Projekt nicht verschlucken. Die Datei wird von Hand gepflegt (Spec §4.3);
// genau so sieht ein Eintrag aus, den jemand direkt hineingeschrieben hat.
func TestBereicheGruppierenVerliertKeineHandeingetrageneArea(t *testing.T) {
	st := areas.State{
		Areas: []string{"Arbeit"},
		Zuordnung: map[string]string{
			"alpha-tool":  "Arbeit",
			"gamma-notes": "Privat", // steht NICHT in Areas
		},
	}
	got := bereicheText(BereicheGruppieren(projekte("gamma-notes", "alpha-tool"), st))

	// "Privat" wird nachgetragen, also hinter die deklarierten Areas.
	want := "Arbeit: alpha-tool\n" +
		"Privat: gamma-notes\n"
	if got != want {
		t.Errorf("Gruppierung\n--- bekommen ---\n%s--- erwartet ---\n%s", got, want)
	}
}

// Eine deklarierte Area ohne Projekte bleibt stehen: sie ist das Ziel der
// naechsten Zuweisung. Verschwaende sie, waere "Area anlegen" eine Operation
// ohne sichtbare Wirkung — und die Ansicht koennte nichts anbieten.
func TestBereicheGruppierenBehaeltLeereArea(t *testing.T) {
	st := areas.State{Areas: []string{"Arbeit", "Privat"}, Zuordnung: map[string]string{"alpha-tool": "Arbeit"}}
	gs := BereicheGruppieren(projekte("alpha-tool"), st)

	got := bereicheText(gs)
	want := "Arbeit: alpha-tool\nPrivat:\n"
	if got != want {
		t.Errorf("Gruppierung\n--- bekommen ---\n%s--- erwartet ---\n%s", got, want)
	}
	if namen := strings.Join(BereichsNamen(gs), ","); namen != "Arbeit,Privat" {
		t.Errorf("BereichsNamen = %q, erwartet \"Arbeit,Privat\" — die leere Area muss zuweisbar bleiben", namen)
	}
}

// Die Sammel-Area ist KEIN Ziel: sie steht in keiner Datei, und sie anzubieten
// hiesse, "Ohne Bereich" als echten Namen nach areas.json zu schreiben.
func TestBereichsNamenLaesstDieSammelAreaAus(t *testing.T) {
	st := areas.State{Areas: []string{"Arbeit"}, Zuordnung: map[string]string{}}
	gs := BereicheGruppieren(projekte("omega-app"), st)
	for _, n := range BereichsNamen(gs) {
		if n == areas.OhneBereich {
			t.Fatalf("BereichsNamen bietet %q als Ziel an", areas.OhneBereich)
		}
	}
}

// BereicheBlock liest die Datei — und muss dabei die aus CC_AREAS_FILE lesen,
// nicht die des Benutzers. Der Test setzt sie deshalb auf einen Wegwerfpfad und
// prueft zugleich, dass ein LESEFEHLER gemeldet und nicht zu einem leeren
// Zustand gemacht wird: eine kaputte areas.json saehe sonst aus wie "noch
// nichts zugeordnet", und die Ansicht boete an, genau darauf zu schreiben.
func TestBereicheBlockMeldetKaputteDatei(t *testing.T) {
	pfad := filepath.Join(t.TempDir(), "areas.json")
	t.Setenv("CC_AREAS_FILE", pfad)
	if err := os.WriteFile(pfad, []byte("{kein json"), 0o644); err != nil {
		t.Fatal(err)
	}

	block := BereicheBlock(projekte("omega-app"))
	if aStr(block["fehler"]) == "" {
		t.Error("kein Fehler gemeldet — eine unlesbare areas.json saehe aus wie ein leerer Zustand")
	}
	if aStr(block["pfad"]) != pfad {
		t.Errorf("pfad = %q, erwartet %q — der Block muss CC_AREAS_FILE folgen", aStr(block["pfad"]), pfad)
	}
	// Trotz Fehler benutzbar: das Projekt steht unter der Sammel-Area, die
	// Ansicht bleibt bedienbar statt leer.
	roh, _ := json.Marshal(block["gruppen"])
	if !strings.Contains(string(roh), "omega-app") {
		t.Errorf("Gruppen ohne omega-app: %s", roh)
	}
}

// ---- die Verdrahtung in die Seite ----

// buildDataMitProjekten baut DATA fuer eine Projektliste und eine
// areas.json unter t.TempDir(). Die Wegwerfdatei ist nicht Hygiene, sondern
// Bedingung: liefe der Test gegen die echte Datei des Benutzers, haenge seine
// Erwartung an dessen Zuordnung — und in dieser Karte hat ein Test schon
// einmal eine echte Cache-Datei vergiftet (gkp-06r).
func buildDataMitProjekten(t *testing.T, view string, st areas.State, namen ...string) map[string]any {
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
	return BuildData(map[string]any{"projects": ps, "cards": map[string]any{}}, view)
}

// Der Datenblock muss in DATA stehen — und in BEIDEN Navigationen. Die
// Bereichsansicht ist die einzige ueber alle Projekte; stuende sie nur in der
// Projektnavigation, waere sie im Ueberblick nicht erreichbar, obwohl sie
// genau dort hingehoert.
func TestBuildDataTraegtDieBereicheInBeideNavigationen(t *testing.T) {
	st := areas.State{Areas: []string{"Arbeit"}, Zuordnung: map[string]string{"alpha-tool": "Arbeit"}}
	for _, view := range []string{"project", "overview"} {
		data := buildDataMitProjekten(t, view, st, "alpha-tool", "omega-app")

		block := aMap(data["bereiche"])
		if block == nil {
			t.Fatalf("view %s: DATA.bereiche fehlt", view)
		}
		gs, _ := block["gruppen"].([]BereichGruppe)
		if got, want := bereicheText(gs), "Arbeit: alpha-tool\nOhne Bereich [sammel]: omega-app\n"; got != want {
			t.Errorf("view %s:\n--- bekommen ---\n%s--- erwartet ---\n%s", view, got, want)
		}
		item := navEintrag(t, data, BereicheID)
		// Die Zahl ist die der Projekte OHNE Bereich: sie steht genau dann da,
		// wenn etwas zu tun ist.
		if aStr(item["cnt"]) != "1" {
			t.Errorf("view %s: cnt = %q, erwattet \"1\" (omega-app ist ohne Bereich)", view, aStr(item["cnt"]))
		}
		if aStr(item["dot"]) != "a" {
			t.Errorf("view %s: dot = %q, erwartet \"a\" — offene Pflege ist gelb, nicht rot", view, aStr(item["dot"]))
		}
	}
}

// Ist alles zugeordnet, verschwindet die Zahl. Eine Null waere eine
// Aufforderung ohne Anlass, und der Punkt bleibt neutral statt gelb.
func TestBereicheNavOhneOffenePflegeTraegtKeineZahl(t *testing.T) {
	st := areas.State{Areas: []string{"Arbeit"}, Zuordnung: map[string]string{"alpha-tool": "Arbeit"}}
	data := buildDataMitProjekten(t, "project", st, "alpha-tool")

	item := navEintrag(t, data, BereicheID)
	if _, da := item["cnt"]; da {
		t.Errorf("cnt = %v, erwartet gar keine Zahl — es ist nichts offen", item["cnt"])
	}
	if aStr(item["dot"]) != "c" {
		t.Errorf("dot = %q, erwartet \"c\"", aStr(item["dot"]))
	}
}
