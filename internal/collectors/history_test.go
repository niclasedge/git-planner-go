package collectors

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------- Zuordnung

// Der Grund, warum die Zuordnung gegen die bekannten Ids laeuft und nicht
// gegen ein Muster, steht in zwei Faellen hier: "orbit-eigene" (ein Regex auf
// prefix-[a-z0-9]{3,} trifft das) und "gkp-ar7.14" (ein Praefix-Vergleich
// haengt den Commit an den Elternbead).
func TestBeadRefsInTrifftNurEchteIds(t *testing.T) {
	ids := []string{"gkp-80s", "gkp-ar7", "gkp-ar7.14", "orbit-61z", "orbit-7ys", "orbit-7ys.1"}
	faelle := []struct {
		name string
		text string
		will []string
	}{
		{"Praefix im Subject", "gkp-ar7.14: Bootstrap behoben", []string{"gkp-ar7.14"}},
		{"Elternbead nicht mitzaehlen", "gkp-ar7.14 fertig", []string{"gkp-ar7.14"}},
		{"Elternbead allein genannt", "gkp-ar7: Karte geschnitten", []string{"gkp-ar7"}},
		{"zwei Ids komma-getrennt", "orbit-61z, orbit-7ys.1: TUI verdrahten", []string{"orbit-61z", "orbit-7ys.1"}},
		{"Repo-Name ist keine Id", "orbit: Prozessdiagramme und just-Rezepte", nil},
		{"Phantom-Bead", "orbit-eigene Konfliktaufloesung", nil},
		{"Trailer im Body", "irgendwas\n\nRefs: gkp-80s\n", []string{"gkp-80s"}},
		{"in Klammern", "Frist gesetzt (gkp-80s)", []string{"gkp-80s"}},
		{"angeklebt links", "xgkp-80s", nil},
		{"angeklebt rechts", "gkp-80sx", nil},
		{"mit Unterstrich rechts", "gkp-80s_alt", nil},
		{"leerer Text", "", nil},
	}
	for _, f := range faelle {
		t.Run(f.name, func(t *testing.T) {
			got := BeadRefsIn(f.text, ids)
			if strings.Join(got, ",") != strings.Join(f.will, ",") {
				t.Errorf("BeadRefsIn(%q) = %v, erwartet %v", f.text, got, f.will)
			}
		})
	}
}

func TestBeadRefsInOhneIdsFindetNichts(t *testing.T) {
	if got := BeadRefsIn("gkp-80s: irgendwas", nil); got != nil {
		t.Errorf("got %v — ohne bekannte Ids darf nichts zugeordnet werden", got)
	}
}

// ---------------------------------------------------------------- git log

func rec(hash, at, autor, subject, body string) string {
	return gitRecordSep + strings.Join([]string{hash, at, autor, subject, body}, gitFieldSep)
}

func TestParseBeadCommits(t *testing.T) {
	raw := rec("a1b2c3d4e5f6a7b8", "2026-09-04T19:11:22+02:00", "Alex", "orbit-61z, orbit-7ys.1: TUI verdrahten", "") +
		rec("ffffffffffffffff", "2026-09-04T19:09:11+02:00", "Alex Example", "orbit: Prozessdiagramme", "kein Bead hier") +
		rec("0123456789abcdef", "2026-09-02T22:47:34+02:00", "Alex Example", "Aufraeumen", "mehrzeiliger\nBody\n\nRefs: orbit-61z\n")

	got, scanned := parseBeadCommits(raw, []string{"orbit-61z", "orbit-7ys", "orbit-7ys.1"})

	if scanned != 3 {
		t.Errorf("scanned=%d, erwartet 3 — durchsucht werden alle Commits, nicht nur die Treffer", scanned)
	}
	if len(got) != 2 {
		t.Fatalf("len=%d, erwartet 2 Treffer, bekommen: %+v", len(got), got)
	}
	if got[0].Short != "a1b2c3d" {
		t.Errorf("Short=%q, erwartet die ersten 7 Stellen", got[0].Short)
	}
	// Die Normalisierung auf UTC ist der Punkt: die Historie sortiert ueber den
	// String, und "+02:00" gegen "Z" waere ein falscher Vergleich.
	if got[0].At != "2026-09-04T17:11:22Z" {
		t.Errorf("At=%q, erwartet UTC", got[0].At)
	}
	if strings.Join(got[0].Beads, ",") != "orbit-61z,orbit-7ys.1" {
		t.Errorf("Beads=%v — ein Commit mit zwei Ids gehoert unter beide", got[0].Beads)
	}
	if got[1].Subject != "Aufraeumen" || strings.Join(got[1].Beads, ",") != "orbit-61z" {
		t.Errorf("zweiter Treffer = %+v — die Id stand im Body", got[1])
	}
}

func TestParseBeadCommitsLeereAusgabe(t *testing.T) {
	got, scanned := parseBeadCommits("", []string{"gkp-80s"})
	if len(got) != 0 || scanned != 0 {
		t.Errorf("got=%v scanned=%d, erwartet leer", got, scanned)
	}
}

// Eine abgeschnittene Zeile (Timeout mitten im Schreiben der Pipe) darf die
// Spur nicht kosten, sondern nur diesen einen Datensatz.
func TestParseBeadCommitsUeberspringtUnvollstaendigenDatensatz(t *testing.T) {
	raw := gitRecordSep + "abc" + gitFieldSep + "2026-09-04T10:00:00Z" +
		rec("0123456789abcdef", "2026-09-04T09:00:00Z", "Wer", "gkp-80s: Frist", "")
	got, scanned := parseBeadCommits(raw, []string{"gkp-80s"})
	if scanned != 1 || len(got) != 1 {
		t.Errorf("scanned=%d len=%d, erwartet 1/1 — der halbe Datensatz zaehlt nicht mit", scanned, len(got))
	}
}

func TestToUTCLaesstUnparsbaresStehen(t *testing.T) {
	if got := toUTC("gestern"); got != "gestern" {
		t.Errorf("toUTC=%q — ein unparsbarer Wert wird nicht weggeworfen", got)
	}
	// Bruchsekunden aus dem bd-Log fallen weg, damit alle Zeitstempel dieselbe
	// Breite haben — sonst sortiert ".928Z" gegen "Z" in derselben Sekunde
	// falsch ('.' < 'Z').
	if got := toUTC("2026-09-04T18:16:21.928494928Z"); got != "2026-09-04T18:16:21Z" {
		t.Errorf("toUTC=%q, erwartet auf Sekunden gekuerzt", got)
	}
}

// ---------------------------------------------------------------- bd-Audit

func schreibeAudit(t *testing.T, zeilen ...string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".beads"), 0o755); err != nil {
		t.Fatal(err)
	}
	inhalt := strings.Join(zeilen, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(dir, ".beads", "interactions.jsonl"), []byte(inhalt), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestCollectTrackerChanges(t *testing.T) {
	dir := schreibeAudit(t,
		`{"kind":"field_change","created_at":"2026-09-04T18:16:21.928494928Z","actor":"Alex Example","issue_id":"gkp-ar7.14","extra":{"field":"status","old_value":"open","new_value":"closed","reason":"Per bd bootstrap behoben"}}`,
		`{"kind":"field_change","created_at":"2026-09-04T18:20:00Z","actor":"Agent","issue_id":"gkp-ar7.10","extra":{"field":"title","old_value":"alt","new_value":"neu"}}`,
		`{kaputt`,
		`{"kind":"field_change","created_at":"2026-09-04T19:00:00Z","actor":"Agent","issue_id":"gkp-80s","extra":{"field":"status","old_value":"open","new_value":"in_progress"}}`,
	)

	got, total := CollectTrackerChanges(dir)

	if total != 2 || len(got) != 2 {
		t.Fatalf("total=%d len=%d, erwartet 2/2 — nur Statuswechsel, kaputte Zeile uebersprungen: %+v", total, len(got), got)
	}
	// Die Datei ist append-only, die Ansicht will neueste zuerst.
	if got[0].Bead != "gkp-80s" {
		t.Errorf("erster Eintrag = %q, erwartet den neuesten (gkp-80s)", got[0].Bead)
	}
	letzter := got[1]
	if letzter.At != "2026-09-04T18:16:21Z" || letzter.Actor != "Alex Example" ||
		letzter.From != "open" || letzter.To != "closed" ||
		letzter.Reason != "Per bd bootstrap behoben" {
		t.Errorf("Eintrag = %+v — actor, Wechsel und Begruendung muessen durchkommen", letzter)
	}
}

// Ein Repo ohne bd hat kein Audit-Log. Das ist kein Fehler und darf die
// Reports-Ansicht nicht kosten.
func TestCollectTrackerChangesOhneDateiIstKeinFehler(t *testing.T) {
	got, total := CollectTrackerChanges(t.TempDir())
	if got != nil || total != 0 {
		t.Errorf("got=%v total=%d, erwartet leer", got, total)
	}
}

func TestCollectTrackerChangesKapptAufLimit(t *testing.T) {
	var zeilen []string
	for i := 0; i < HistoryTrackerLimit+5; i++ {
		zeilen = append(zeilen,
			`{"kind":"field_change","created_at":"2026-09-04T18:00:00Z","actor":"A","issue_id":"gkp-x","extra":{"field":"status","new_value":"closed"}}`)
	}
	got, total := CollectTrackerChanges(schreibeAudit(t, zeilen...))
	if total != HistoryTrackerLimit+5 {
		t.Errorf("total=%d, erwartet %d — die Gesamtzahl wird genannt, nicht verschwiegen", total, HistoryTrackerLimit+5)
	}
	if len(got) != HistoryTrackerLimit {
		t.Errorf("len=%d, erwartet die Kappung auf %d", len(got), HistoryTrackerLimit)
	}
}
