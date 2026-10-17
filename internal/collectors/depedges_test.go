package collectors

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeIssues legt dir/.beads/issues.jsonl mit den gegebenen Zeilen an (eine
// pro Slice-Element, mit trailender Newline nach der letzten Zeile -- wie ein
// echter Export). t.TempDir() reicht als Repo-Root: RepoRoot faellt auf cwd
// zurueck, wenn `git rev-parse` fehlschlaegt (kein Repo hier noetig), also
// muss dieser Test kein git init anlegen.
func writeIssues(t *testing.T, dir string, lines []string) {
	t.Helper()
	beadsDir := filepath.Join(dir, ".beads")
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(beadsDir, "issues.jsonl"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestTrackerGraphCached_BlocksEdgeDirection ist der Regressionstest fuer den
// Fehler, der in diesem Repo schon einmal alle zehn Kanten verdreht hat: bd
// benennt dieselbe Kante ueber verschiedene Kommandos hinweg unterschiedlich,
// und "issue_id wartet auf depends_on_id" ist leicht mit "issue_id blockiert
// depends_on_id" zu verwechseln. Blocked/Blocker muessen exakt in dieser
// Zuordnung landen.
func TestTrackerGraphCached_BlocksEdgeDirection(t *testing.T) {
	dir := t.TempDir()
	writeIssues(t, dir, []string{
		`{"id":"a","title":"A","status":"open","issue_type":"task","updated_at":"2026-09-01T00:00:00Z","dependencies":[{"issue_id":"a","depends_on_id":"b","type":"blocks"}]}`,
		`{"id":"b","title":"B","status":"open","issue_type":"task","updated_at":"2026-09-01T00:00:00Z"}`,
	})

	got := TrackerGraphCached(dir)
	if len(got.Edges) != 1 {
		t.Fatalf("want 1 edge, got %d: %+v", len(got.Edges), got.Edges)
	}
	want := DepEdge{Blocked: "a", Blocker: "b"}
	if got.Edges[0] != want {
		t.Errorf("Kantenrichtung falsch: got %+v, want %+v (a wartet auf b, nicht umgekehrt)", got.Edges[0], want)
	}
}

// TestTrackerGraphCached_ParentChildNotEdge: eine parent-child-Abhaengigkeit
// ist Epic-Zugehoerigkeit, keine blockierende Kante -- sie darf nicht in
// Edges auftauchen, sondern nur in Parent.
func TestTrackerGraphCached_ParentChildNotEdge(t *testing.T) {
	dir := t.TempDir()
	writeIssues(t, dir, []string{
		`{"id":"tf4","title":"Kind","status":"open","issue_type":"task","updated_at":"2026-09-01T00:00:00Z","dependencies":[{"issue_id":"tf4","depends_on_id":"5nn","type":"parent-child"}]}`,
		`{"id":"5nn","title":"Epic","status":"open","issue_type":"epic","updated_at":"2026-09-01T00:00:00Z"}`,
	})

	got := TrackerGraphCached(dir)
	if len(got.Edges) != 0 {
		t.Errorf("parent-child darf keine blocks-Kante erzeugen, got Edges=%+v", got.Edges)
	}
	if got.Parent["tf4"] != "5nn" {
		t.Errorf(`Parent["tf4"] = %q, want "5nn"`, got.Parent["tf4"])
	}
}

// TestTrackerGraphCached_RelatedAndDiscoveredFromDropped: die beiden anderen
// dependency-Typen tragen fuer diesen Graphen keine Bedeutung (weder Blocker
// noch Epic-Zugehoerigkeit) und muessen verworfen werden -- sonst wuerde ein
// "related"-Verweis wie eine Blockade aussehen.
func TestTrackerGraphCached_RelatedAndDiscoveredFromDropped(t *testing.T) {
	dir := t.TempDir()
	writeIssues(t, dir, []string{
		`{"id":"a","title":"A","status":"open","issue_type":"task","updated_at":"2026-09-01T00:00:00Z","dependencies":[{"issue_id":"a","depends_on_id":"b","type":"related"},{"issue_id":"a","depends_on_id":"c","type":"discovered-from"}]}`,
		`{"id":"b","title":"B","status":"open","issue_type":"task","updated_at":"2026-09-01T00:00:00Z"}`,
		`{"id":"c","title":"C","status":"open","issue_type":"task","updated_at":"2026-09-01T00:00:00Z"}`,
	})

	got := TrackerGraphCached(dir)
	if len(got.Edges) != 0 {
		t.Errorf("related/discovered-from duerfen keine Edges erzeugen, got %+v", got.Edges)
	}
	if len(got.Parent) != 0 {
		t.Errorf("related/discovered-from duerfen kein Parent erzeugen, got %+v", got.Parent)
	}
}

// TestTrackerGraphCached_BrokenLineMidFile: eine kaputte Zeile mittendrin darf
// die restliche Datei nicht mitreissen -- fail-open gilt pro Zeile, nicht pro
// Datei. Die Note muss die Zahl nennen, damit ein Aufrufer sehen kann, dass
// etwas uebersprungen wurde.
func TestTrackerGraphCached_BrokenLineMidFile(t *testing.T) {
	dir := t.TempDir()
	writeIssues(t, dir, []string{
		`{"id":"a","title":"A","status":"open","issue_type":"task","updated_at":"2026-09-01T00:00:00Z"}`,
		`not valid json at all`,
		`{"id":"b","title":"B","status":"open","issue_type":"task","updated_at":"2026-09-02T00:00:00Z"}`,
	})

	got := TrackerGraphCached(dir)
	if !got.Available {
		t.Fatalf("want Available=true trotz einer kaputten Zeile, got %+v", got)
	}
	if got.Title["a"] != "A" || got.Title["b"] != "B" {
		t.Errorf("beide gueltigen Zeilen muessen ueberleben, got Title=%+v", got.Title)
	}
	if !strings.Contains(got.Note, "1") {
		t.Errorf("Note soll die Zahl der kaputten Zeilen nennen, got %q", got.Note)
	}
}

// TestTrackerGraphCached_MissingFile: fehlt .beads/issues.jsonl ganz (kein
// .beads/ angelegt), muss Available=false mit erklaerender Note zurueckkommen
// -- kein Panic, keine leere Note.
func TestTrackerGraphCached_MissingFile(t *testing.T) {
	dir := t.TempDir()

	got := TrackerGraphCached(dir)
	if got.Available {
		t.Errorf("want Available=false ohne .beads/issues.jsonl, got %+v", got)
	}
	if got.Note == "" {
		t.Error("want eine Note, die erklaert was fehlt und wie der Export eingeschaltet wird")
	}
}

// TestTrackerGraphCached_CacheSurvivesDeletionWithinTTL prueft beide Haelften
// der Cache-Regel: das Verschwinden der Datei allein (kein neues mtime/size,
// da gar kein Stat mehr moeglich) darf den zwischengespeicherten Stand nicht
// verwerfen; eine tatsaechliche Aenderung von Inhalt UND mtime dagegen schon.
func TestTrackerGraphCached_CacheSurvivesDeletionWithinTTL(t *testing.T) {
	dir := t.TempDir()
	issuesPath := filepath.Join(dir, ".beads", "issues.jsonl")
	writeIssues(t, dir, []string{
		`{"id":"a","title":"A","status":"open","issue_type":"task","updated_at":"2026-09-01T00:00:00Z"}`,
	})

	first := TrackerGraphCached(dir)
	if !first.Available || first.Title["a"] != "A" {
		t.Fatalf("Vorbedingung verletzt: erster Aufruf muss die Datei lesen, got %+v", first)
	}

	if err := os.Remove(issuesPath); err != nil {
		t.Fatal(err)
	}
	second := TrackerGraphCached(dir)
	if !second.Available || second.Title["a"] != "A" {
		t.Errorf("Loeschen der Datei darf den Cache innerhalb der TTL nicht invalidieren (kein neues mtime/size-Signal), got %+v", second)
	}

	// Inhalt UND mtime aendern sich jetzt tatsaechlich -- das MUSS den
	// Cache-Eintrag verwerfen.
	writeIssues(t, dir, []string{
		`{"id":"a","title":"A","status":"open","issue_type":"task","updated_at":"2026-09-01T00:00:00Z"}`,
		`{"id":"z","title":"Z","status":"open","issue_type":"task","updated_at":"2026-09-01T00:00:00Z"}`,
	})
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(issuesPath, future, future); err != nil {
		t.Fatal(err)
	}
	third := TrackerGraphCached(dir)
	if _, ok := third.Title["z"]; !ok {
		t.Errorf("Aenderung von Inhalt+mtime muss den Cache invalidieren, got %+v", third)
	}
}
