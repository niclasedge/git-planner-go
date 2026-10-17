package collectors

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

// Die Tests zum Lesen der Change-Ordner (openspec-quelle, #10). Sie halten die
// Szenarien aus openspec/changes/openspec-quelle/specs/openspec-quelle fest,
// soweit sie das Dateisystem betreffen; der Baum aus Issue und Change steht in
// internal/appctx/openspecquelle_test.go.

// osDatei schreibt eine Datei samt Ordnern unter wurzel.
func osDatei(t *testing.T, wurzel, rel, inhalt string) {
	t.Helper()
	p := filepath.Join(wurzel, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(inhalt), 0o644); err != nil {
		t.Fatal(err)
	}
}

// osChange legt einen Change-Ordner mit proposal.md und tasks.md an.
func osChange(t *testing.T, wurzel, ordner, proposal, tasks string) {
	t.Helper()
	osDatei(t, wurzel, "openspec/changes/"+ordner+"/proposal.md", proposal)
	osDatei(t, wurzel, "openspec/changes/"+ordner+"/tasks.md", tasks)
}

func osFinde(t *testing.T, cs []OpenspecChange, kennung string) OpenspecChange {
	t.Helper()
	for _, c := range cs {
		if c.Kennung == kennung {
			return c
		}
	}
	t.Fatalf("Change %q fehlt: %+v", kennung, cs)
	return OpenspecChange{}
}

func TestOpenspecOhneOrdner(t *testing.T) {
	dir := t.TempDir()
	if HatOpenspec(dir) {
		t.Errorf("HatOpenspec(%s) = true ohne openspec/", dir)
	}
	if cs := OpenspecChanges(dir); len(cs) != 0 {
		t.Errorf("OpenspecChanges ohne openspec/ = %+v, want leer", cs)
	}
}

// Szenario: Issue mit Change und Aufgaben — der Teil, den die Dateien tragen.
func TestOpenspecLiestIssueZeileUndKaestchen(t *testing.T) {
	dir := t.TempDir()
	osChange(t, dir, "openspec-quelle",
		"# openspec-quelle\n\n## Warum\n\nGeht zurueck auf GitHub-Issue #10.\n",
		"# Aufgaben\n\n- [x] eins\n- [ ] zwei\n  - [ ] drei eingerueckt\n\nKein Kaestchen: - [] nichts\n")
	if !HatOpenspec(dir) {
		t.Fatal("HatOpenspec = false trotz openspec/")
	}
	c := osFinde(t, OpenspecChanges(dir), "openspec-quelle")
	if c.Issue != 10 {
		t.Errorf("Issue = %d, want 10", c.Issue)
	}
	want := []OpenspecAufgabe{{Text: "eins", Erledigt: true}, {Text: "zwei"}, {Text: "drei eingerueckt"}}
	if !reflect.DeepEqual(c.Aufgaben, want) {
		t.Errorf("Aufgaben = %+v, want %+v", c.Aufgaben, want)
	}
	if c.Erledigt() != 1 || len(c.Aufgaben) != 3 {
		t.Errorf("Fortschritt = %d/%d, want 1/3", c.Erledigt(), len(c.Aufgaben))
	}
	if c.Archiviert {
		t.Error("Archiviert = true fuer einen aktiven Change")
	}
}

// Szenario: Zuordnung ueber den Issue-Titel — ohne Zeile bleibt Issue 0.
func TestOpenspecOhneIssueZeile(t *testing.T) {
	dir := t.TempDir()
	osChange(t, dir, "foo", "# foo\n\nKein Verweis.\n", "- [ ] a\n")
	if c := osFinde(t, OpenspecChanges(dir), "foo"); c.Issue != 0 {
		t.Errorf("Issue = %d, want 0 ohne Zeile", c.Issue)
	}
}

// Szenario: Change laeuft.
func TestOpenspecChangeLaeuft(t *testing.T) {
	dir := t.TempDir()
	osChange(t, dir, "x", "Geht zurueck auf GitHub-Issue #4.\n", "- [x] a\n- [ ] b\n- [ ] c\n")
	osDatei(t, dir, "openspec/changes/x/gate1.md", "# Gate 1\n")
	osDatei(t, dir, "openspec/changes/x/gate2.md", "") // leer zaehlt nicht
	c := osFinde(t, OpenspecChanges(dir), "x")
	if c.Status() != OpenspecLaeuft {
		t.Errorf("Status = %q, want %q", c.Status(), OpenspecLaeuft)
	}
	if !reflect.DeepEqual(c.Gates, []string{"gate1"}) {
		t.Errorf("Gates = %v, want [gate1]", c.Gates)
	}
}

func TestOpenspecStatusFreiUndFertig(t *testing.T) {
	dir := t.TempDir()
	osChange(t, dir, "frei", "x\n", "- [ ] a\n")
	osChange(t, dir, "fertig", "x\n", "- [x] a\n- [X] b\n")
	osDatei(t, dir, "openspec/changes/fertig/gate1.md", "g")
	osDatei(t, dir, "openspec/changes/fertig/gate2.md", "g")
	osDatei(t, dir, "openspec/changes/fertig/fertig.md", "g")
	cs := OpenspecChanges(dir)
	if s := osFinde(t, cs, "frei").Status(); s != OpenspecFrei {
		t.Errorf("frei: Status = %q, want %q", s, OpenspecFrei)
	}
	f := osFinde(t, cs, "fertig")
	if f.Status() != OpenspecFertig {
		t.Errorf("fertig: Status = %q, want %q (kein Remote-Branch)", f.Status(), OpenspecFertig)
	}
	if !reflect.DeepEqual(f.Gates, []string{"gate1", "gate2", "fertig"}) {
		t.Errorf("fertig: Gates = %v", f.Gates)
	}
}

// git fuehrt einen git-Befehl ohne die Konfiguration des Benutzers aus.
func osGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// Szenario: Alle Aufgaben erledigt mit Remote-Branch.
func TestOpenspecImPRMitRemoteRef(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git fehlt")
	}
	dir := t.TempDir()
	osChange(t, dir, "baz", "x\n", "- [x] a\n")
	osChange(t, dir, "qux", "x\n", "- [x] a\n")
	osGit(t, dir, "init", "-q")
	osGit(t, dir, "commit", "-q", "--allow-empty", "-m", "x")
	osGit(t, dir, "update-ref", "refs/remotes/origin/change/baz", "HEAD")
	// Eine Ref, die nur auf die Kennung ENDET, zaehlt nicht.
	osGit(t, dir, "update-ref", "refs/remotes/origin/change/vorqux", "HEAD")
	cs := OpenspecChanges(dir)
	if s := osFinde(t, cs, "baz").Status(); s != OpenspecImPR {
		t.Errorf("baz: Status = %q, want %q", s, OpenspecImPR)
	}
	if s := osFinde(t, cs, "qux").Status(); s != OpenspecFertig {
		t.Errorf("qux: Status = %q, want %q", s, OpenspecFertig)
	}
}

// Szenario: Archivierter Change.
func TestOpenspecArchivierterChange(t *testing.T) {
	dir := t.TempDir()
	osChange(t, dir, "archive/2026-09-24-github-pat-env", "Geht zurueck auf GitHub-Issue #2.\n", "- [ ] vergessen\n")
	c := osFinde(t, OpenspecChanges(dir), "github-pat-env")
	if !c.Archiviert {
		t.Error("Archiviert = false unter archive/")
	}
	if c.Issue != 2 {
		t.Errorf("Issue = %d, want 2", c.Issue)
	}
	if c.Status() != OpenspecErledigt {
		t.Errorf("Status = %q, want %q", c.Status(), OpenspecErledigt)
	}
	for _, x := range OpenspecChanges(dir) {
		if x.Kennung == "archive" {
			t.Error("archive/ selbst ist als Change gelesen worden")
		}
	}
}

// Szenario: Change mit Bildbeleg.
func TestOpenspecBelegeAusJournal(t *testing.T) {
	dir := t.TempDir()
	osChange(t, dir, "b", "x\n", "- [x] a\n")
	osDatei(t, dir, "openspec/changes/b/belege/belege.jsonl",
		`{"zeit":"2026-09-24T10:00:00Z","stufe":"nachher","art":"bild","pfad":"belege/assets/0-x.png","fehlt":false,"grund":""}`+"\n"+
			"\n"+
			`kaputt`+"\n"+
			`{"zeit":"2026-09-24T11:00:00Z","stufe":"vorher","art":"bild","pfad":"","fehlt":true,"grund":"Quelldatei nicht gefunden"}`+"\n")
	c := osFinde(t, OpenspecChanges(dir), "b")
	if len(c.Belege) != 2 {
		t.Fatalf("Belege = %+v, want 2 (leere und kaputte Zeilen fallen weg)", c.Belege)
	}
	b := c.Belege[0]
	if b.Stufe != "nachher" || b.Art != "bild" || b.Pfad != "belege/assets/0-x.png" || b.Fehlt {
		t.Errorf("Beleg 0 = %+v", b)
	}
	if !c.Belege[1].Fehlt || c.Belege[1].Grund == "" {
		t.Errorf("Beleg 1 = %+v, want fehlend mit Grund", c.Belege[1])
	}
}
