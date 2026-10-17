package taskops

import (
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/niclasedge/git-planner-go/internal/areas"
)

// Dieser Test ist das Gegenstueck zu taskops_test.go: dort ersetzt eine
// Attrappe internal/bdops, hier laeuft das echte bd gegen eine frisch
// angelegte Wegwerf-Datenbank unter t.TempDir().
//
// Warum beides noetig ist: die Attrappe prueft, WAS an bd ginge, kann aber per
// Konstruktion nicht wissen, ob bd damit etwas anfangen kann. Genau dort sitzen
// die teuren Fehler dieses Themas — ein Flag, das bd anders schreibt, ein
// leerer Wert, der bei bd nicht loescht, ein Datum, das in einer anderen
// Zeitzone auf den Folgetag kippt. Der Test unten faehrt deshalb unter einer
// FESTEN, von UTC verschiedenen Zone: unter TZ=UTC waere die Datumsdeutung
// auch dann gruen, wenn sie falsch waere.
//
// Er ueberspringt sich unter -short, weil `bd init` gut eine Sekunde kostet.

// zeitzoneFestlegen setzt die Zone fuer beide Seiten: TZ fuer den bd-Prozess
// (bd schreibt defer_until/due_at in der Wanduhr des AUFRUFERS) und time.Local
// fuer die Deutung auf dem Rueckweg. Nur eine von beiden zu setzen wuerde genau
// den Fehler verstecken, den der Test sucht.
func zeitzoneFestlegen(t *testing.T, name string) *time.Location {
	t.Helper()
	t.Setenv("TZ", name)
	return setzeZone(t, name)
}

func wegwerfRepo(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("-short: bd init kostet eine Sekunde")
	}
	for _, werkzeug := range []string{"bd", "git"} {
		if _, err := exec.LookPath(werkzeug); err != nil {
			t.Skipf("%s nicht installiert", werkzeug)
		}
	}
	dir := t.TempDir()
	lauf := func(name string, args ...string) {
		t.Helper()
		cmd := exec.Command(name, args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("%s %v in der Wegwerf-DB fehlgeschlagen: %v\n%s", name, args, err, out)
		}
	}
	lauf("git", "init", "-q", ".")
	lauf("bd", "init", "--prefix", "tstx", "--role", "maintainer", "--non-interactive")
	return dir
}

func TestGegenEchtesBd(t *testing.T) {
	berlin := zeitzoneFestlegen(t, "Europe/Berlin")
	repo := wegwerfRepo(t)
	d := Neu(areas.Open(filepath.Join(t.TempDir(), "areas.json")))

	// --- anlegen, mit beiden Datumsfeldern
	erg, err := d.Anlegen(AnlegenWunsch{
		Pfad:         repo,
		Titel:        "Probe aus taskops",
		Typ:          "task",
		Prioritaet:   "2",
		Beschreibung: "angelegt vom Integrationstest",
		Frist:        "2026-01-15",
		Wann:         "2026-01-10",
	})
	if err != nil {
		t.Fatalf("Anlegen: %v", err)
	}
	auf := erg.Aufgabe
	if auf == nil || auf.ID == "" {
		t.Fatalf("Anlegen ohne Aufgabe: %+v", erg)
	}
	if auf.Titel != "Probe aus taskops" || auf.Prioritaet != 2 || auf.Typ != "task" {
		t.Errorf("Aufgabe falsch angelegt: %+v", auf)
	}
	// Kalendertag und Uhrzeit muessen die sein, die bd bekommen hat. Ein als
	// UTC gelesenes "2026-01-15T00:00:00Z" waere in Berlin der 15. um 01:00 —
	// dieselbe Verschiebung, die einen Abendtermin auf den Folgetag kippt.
	tagPruefen(t, berlin, "Frist", auf.Frist, 2026, time.January, 15)
	tagPruefen(t, berlin, "Wann", auf.Wann, 2026, time.January, 10)
	// Der eigene Stempel von bd ist echtes UTC und darf NICHT verschoben
	// werden: er muss ungefaehr jetzt sein, nicht um den Zonenversatz daneben.
	if auf.Angelegt == nil || time.Since(*auf.Angelegt) > time.Minute || time.Since(*auf.Angelegt) < -time.Minute {
		t.Errorf("Angelegt = %v, erwartet ungefähr jetzt (%v) — als Wanduhr gelesen wäre es um den Zonenversatz daneben",
			auf.Angelegt, time.Now().UTC())
	}

	id := auf.ID

	// --- aendern
	erg, err = d.Aendern(AendernWunsch{Pfad: repo, ID: id, Titel: "Umbenannt", Status: "in_progress"})
	if err != nil {
		t.Fatalf("Aendern: %v", err)
	}
	if erg.Aufgabe.Titel != "Umbenannt" || erg.Aufgabe.Status != "in_progress" {
		t.Errorf("Aendern wirkungslos: %+v", erg.Aufgabe)
	}
	// Die Termine dürfen dabei nicht verloren gehen.
	tagPruefen(t, berlin, "Frist nach Aendern", erg.Aufgabe.Frist, 2026, time.January, 15)

	// --- terminieren: eines setzen, eines loeschen
	erg, err = d.Terminieren(TerminWunsch{Pfad: repo, ID: id, Frist: zeiger("2026-02-20"), Wann: zeiger("")})
	if err != nil {
		t.Fatalf("Terminieren: %v", err)
	}
	tagPruefen(t, berlin, "Frist", erg.Aufgabe.Frist, 2026, time.February, 20)
	if erg.Aufgabe.Wann != nil {
		t.Errorf("Wann = %v, erwartet gelöscht — der leere String muss bei bd ankommen", erg.Aufgabe.Wann)
	}

	// --- unbekannte Id: Fehler mit Grund "exit", nicht "not-found"
	_, err = d.Aendern(AendernWunsch{Pfad: repo, ID: "tstx-gibtesnicht", Titel: "x"})
	if err == nil {
		t.Fatal("Aendern auf unbekannte Id lief durch")
	}
	if g := GrundVon(err); g != "exit" {
		t.Errorf("Grund = %q, erwartet exit (not-found heißt: kein bd auf dem PATH)", g)
	}
}

func tagPruefen(t *testing.T, loc *time.Location, name string, ist *time.Time, jahr int, monat time.Month, tag int) {
	t.Helper()
	if ist == nil {
		t.Errorf("%s fehlt", name)
		return
	}
	got := ist.In(loc)
	if got.Year() != jahr || got.Month() != monat || got.Day() != tag || got.Hour() != 0 {
		t.Errorf("%s = %s, erwartet %04d-%02d-%02d 00:00 lokal", name, got, jahr, monat, tag)
	}
}
