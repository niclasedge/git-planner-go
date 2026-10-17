package appctx

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/niclasedge/git-planner-go/internal/collectors"
)

// wegwerfProjekt legt unter root/name ein git+bd-Repo an und liefert seinen
// Pfad — dieselbe Bauweise wie server_test.wegwerfRepoFuerRouten und
// bdops.buildProbeDB. Eine dritte, selbst erfundene Art, eine Wegwerf-DB
// aufzusetzen, waere genau die Streuung, die Spec §9.1 Fall 3 anprangert
// ("Test baut am echten Weg vorbei auf").
func wegwerfProjekt(t *testing.T, root, name string) string {
	t.Helper()
	if testing.Short() {
		t.Skip("-short: bd init kostet eine Sekunde")
	}
	for _, werkzeug := range []string{"bd", "git"} {
		if _, err := exec.LookPath(werkzeug); err != nil {
			t.Skipf("%s nicht installiert", werkzeug)
		}
	}
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	lauf := func(bin string, args ...string) {
		t.Helper()
		cmd := exec.Command(bin, args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("%s %v fehlgeschlagen: %v\n%s", bin, args, err, out)
		}
	}
	lauf("git", "init", "-q", ".")
	lauf("git", "config", "user.email", "test@example.invalid")
	lauf("git", "config", "user.name", "Test")
	lauf("bd", "init", "--prefix", strings.ReplaceAll(name, "-", ""), "--role", "maintainer", "--non-interactive")
	return dir
}

// beadAnlegen legt einen Bead per `bd q` (quick capture) an und liefert seine
// Id — dieselbe Primitive wie bdops/mapmembership_test.go.
func beadAnlegen(t *testing.T, dir, titel string) string {
	t.Helper()
	cmd := exec.Command("bd", "q", titel)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		// Skip, nicht Fatal: dieselbe Behandlung wie fuer git und bd init
		// darueber, und dieselbe wie in den beiden zitierten Vorbildern
		// (server_test.wegwerfRepoFuerRouten, bdops.buildProbeDB). Ein bd, das
		// direkt nach `bd init` an der Dolt-Sperre haengt, ist eine Luecke der
		// Umgebung und keine Aussage ueber den Code — rot waere hier eine
		// Falschmeldung. Gefunden im Review zu gkp-i90, Befund 2.
		t.Skipf("bd q %q nicht ausfuehrbar: %v", titel, err)
	}
	felder := strings.Fields(strings.TrimSpace(string(out)))
	if len(felder) == 0 {
		t.Skipf("bd q %q lieferte keine Id: %q", titel, out)
	}
	return felder[len(felder)-1]
}

// TestAnsichtenUeberProjekteMischtZweiProjekteMitKorrektemStempel ist das
// Kernkriterium aus dem Auftrag: zwei Projekte mit Aufgaben ergeben EINE
// Liste mit Eintraegen aus BEIDEN, jeder mit seinem korrekten Projekt.
//
// Real gegen bd gebaut (nicht mit einer von Hand geschriebenen ReportsMap),
// weil genau diese Sorte Abkuerzung Spec §9.1 Fall 3 war: ein Test, der am
// echten Schreib-/Leseweg vorbeibaut, prueft eine Welt, die es nicht gibt.
func TestAnsichtenUeberProjekteMischtZweiProjekteMitKorrektemStempel(t *testing.T) {
	root := t.TempDir()
	dirA := wegwerfProjekt(t, root, "proja")
	dirB := wegwerfProjekt(t, root, "projb")
	idA := beadAnlegen(t, dirA, "Aufgabe in A")
	idB := beadAnlegen(t, dirB, "Aufgabe in B")

	ansichten, fehler := ansichtenUeberProjekte([]collectors.ProjectRef{
		{Name: "proja", Path: dirA},
		{Name: "projb", Path: dirB},
	}, jetztTest())

	if len(fehler) != 0 {
		t.Fatalf("unerwartete Projektfehler: %+v", fehler)
	}

	j, ok := ansichten[JederzeitID]
	if !ok {
		t.Fatalf("Fixture taugt nicht: keine Ansicht %q", JederzeitID)
	}
	projektVon := map[string]string{}
	for _, e := range j.Eintraege {
		projektVon[e.Bead] = e.Projekt
	}
	if projektVon[idA] != "proja" {
		t.Errorf("Projekt von %s (aus proja) = %q, want %q", idA, projektVon[idA], "proja")
	}
	if projektVon[idB] != "projb" {
		t.Errorf("Projekt von %s (aus projb) = %q, want %q", idB, projektVon[idB], "projb")
	}
	if len(j.Eintraege) != 2 {
		t.Fatalf("Jederzeit hat %d Eintraege, want 2 (einer je Projekt): %+v", len(j.Eintraege), j.Eintraege)
	}

	// Die Kopfbilanz muss SUMMIEREN, nicht ANHAENGEN: zwei startklare Beads aus
	// zwei Projekten sind EIN Zaehlstand{Grund:"startklar", Anzahl:2} — nicht
	// zwei Eintraege mit Anzahl 1 (das waere die Signatur eines Merges, der
	// fertige Ansichten statt Karten zusammenfuehrt).
	var startklarGefunden bool
	for _, z := range j.Zaehler {
		if z.Grund != GrundStartklar {
			continue
		}
		startklarGefunden = true
		if z.Anzahl != 2 {
			t.Errorf("Zaehlstand %q = %d, want 2 (summiert ueber beide Projekte)", GrundStartklar, z.Anzahl)
		}
	}
	if !startklarGefunden {
		t.Fatalf("Fixture taugt nicht: kein Zaehlstand %q in Jederzeit (%+v)", GrundStartklar, j.Zaehler)
	}
}

// TestAnsichtenUeberProjekteEinFehlschlagKapptDasAggregatNicht ist das
// Fail-open-Kriterium: ein Projekt ohne .beads/ (das Kernszenario aus E1,
// s. teilbaum.go) darf die Aufgaben der uebrigen Projekte nicht verschlucken —
// es traegt nur seinen eigenen Grund in []ProjektFehler bei.
func TestAnsichtenUeberProjekteEinFehlschlagKapptDasAggregatNicht(t *testing.T) {
	root := t.TempDir()
	dirGut := wegwerfProjekt(t, root, "gutesprojekt")
	idGut := beadAnlegen(t, dirGut, "Laeuft trotzdem")

	dirKaputt := filepath.Join(root, "kaputt")
	if err := os.MkdirAll(dirKaputt, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	// Bewusst weder .git noch .beads/: CollectBeads faellt sofort auf
	// Available=false zurueck (beads.go:1069), ohne einen Subprozess zu
	// starten — das ist der Fall, den kartenEinesProjekts fail-open behandeln
	// muss.

	ansichten, fehler := ansichtenUeberProjekte([]collectors.ProjectRef{
		{Name: "gutesprojekt", Path: dirGut},
		{Name: "kaputt", Path: dirKaputt},
	}, jetztTest())

	if len(fehler) != 1 || fehler[0].Projekt != "kaputt" || fehler[0].Grund == "" {
		t.Fatalf("ProjektFehler = %+v, want genau einen benannten Eintrag fuer %q", fehler, "kaputt")
	}

	j, ok := ansichten[JederzeitID]
	if !ok {
		t.Fatalf("Fixture taugt nicht: keine Ansicht %q", JederzeitID)
	}
	var gefunden bool
	for _, e := range j.Eintraege {
		if e.Bead == idGut {
			gefunden = true
		}
	}
	if !gefunden {
		t.Errorf("Aufgabe %s des GUTEN Projekts fehlt, obwohl nur das ANDERE Projekt scheiterte (%+v)", idGut, j.Eintraege)
	}
}

// TestAnsichtenUeberProjekteGesamtfristUeberspringtDenRest ist das
// Gesamtfrist-Kriterium aus dem Review zu gkp-i90 (Befund 3): ohne eine Frist
// UM die Schleife kostet ein pathologischer Fall bis zu 9 * ~70s. Mit
// sammellaufBudget auf 0 gesetzt ist bereits das ERSTE Projekt "zu spaet" —
// beide Projekte werden uebersprungen, KEINS liefert Karten, und beide tragen
// ihren eigenen ProjektFehler. Ein Projekt, das trotzdem durchkaeme, wuerde
// die Frist als wirkungslos entlarven (Spec §9.1, gkp-3oh: eine Gegenprobe,
// die nichts kaputt macht, sagt nichts).
func TestAnsichtenUeberProjekteGesamtfristUeberspringtDenRest(t *testing.T) {
	alt := sammellaufBudget
	sammellaufBudget = -1 * time.Second // bereits VOR dem ersten Projekt ueberschritten
	defer func() { sammellaufBudget = alt }()

	root := t.TempDir()
	dirA := wegwerfProjekt(t, root, "budgeta")
	dirB := wegwerfProjekt(t, root, "budgetb")
	beadAnlegen(t, dirA, "Wuerde ohne Frist gefunden")
	beadAnlegen(t, dirB, "Wuerde ohne Frist gefunden")

	ansichten, fehler := ansichtenUeberProjekte([]collectors.ProjectRef{
		{Name: "budgeta", Path: dirA},
		{Name: "budgetb", Path: dirB},
	}, jetztTest())

	if len(fehler) != 2 {
		t.Fatalf("ProjektFehler = %+v, want 2 (BEIDE uebersprungen, das Budget war schon vor dem ersten ueberschritten)", fehler)
	}
	for _, f := range fehler {
		if !strings.Contains(f.Grund, "Zeitbudget") {
			t.Errorf("Grund = %q, erwartet einen Hinweis auf das Zeitbudget", f.Grund)
		}
	}
	if j, ok := ansichten[JederzeitID]; ok && len(j.Eintraege) != 0 {
		t.Errorf("Jederzeit hat %d Eintraege, erwartet 0 — beide Projekte wurden uebersprungen, bevor sie Karten beitragen konnten: %+v", len(j.Eintraege), j.Eintraege)
	}
}
