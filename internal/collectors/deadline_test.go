package collectors

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/niclasedge/git-planner-go/internal/shared"
)

// Tests for gkp-80s: the page's read path must not wait on a wedged
// subprocess. The deadlines themselves live in internal/shared; what needs
// guarding here is that the collectors actually pass them, which no amount of
// reading the constants can prove.

// fakeBinOnPath writes an executable script named `name` into a temp dir and
// puts that dir first on PATH for the duration of the test. Used to stand in
// for a bd that never returns.
func fakeBinOnPath(t *testing.T, name, script string) {
	t.Helper()
	dir := t.TempDir()
	pfad := filepath.Join(dir, name)
	if err := os.WriteFile(pfad, []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
		t.Fatalf("Fake-%s anlegen: %v", name, err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// kurzeFristen shortens the read-path deadlines for one test and restores
// them afterwards. Proving the fail-open behaviour is the point; waiting the
// real 20 s to prove it would get this test deleted.
func kurzeFristen(t *testing.T, ms int) {
	t.Helper()
	altBd, altGit, altDot := shared.BdTimeoutMs, shared.GitTimeoutMs, shared.DotTimeoutMs
	shared.BdTimeoutMs, shared.GitTimeoutMs, shared.DotTimeoutMs = ms, ms, ms
	t.Cleanup(func() {
		shared.BdTimeoutMs, shared.GitTimeoutMs, shared.DotTimeoutMs = altBd, altGit, altDot
	})
}

func TestCollectBeadsGibtBeiHaengendemBdAuf(t *testing.T) {
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".beads"), 0o755); err != nil {
		t.Fatalf(".beads anlegen: %v", err)
	}
	// 30 s ist reichlich mehr als jede Frist, die dieser Test setzt, und
	// reichlich weniger als das Go-Test-Timeout — ein haengendes bd, das den
	// Testlauf selbst nicht aufhaelt.
	fakeBinOnPath(t, "bd", "sleep 30")
	kurzeFristen(t, 150)

	start := time.Now()
	got := CollectBeads(repo)
	dauer := time.Since(start)

	// Fail open, nicht fail closed: die Karte sagt, dass etwas fehlt, statt
	// den Request offen zu halten. Available bleibt true, weil .beads/ da ist
	// — das Repo BENUTZT beads, bd antwortet nur nicht.
	if !got.Available {
		t.Errorf("Available=false, erwartet true (.beads/ existiert)")
	}
	if got.Error == "" {
		t.Errorf("Error ist leer — der Ausfall muss in der Karte stehen")
	}
	if len(got.Maps) != 0 {
		t.Errorf("Maps=%d, erwartet 0", len(got.Maps))
	}
	// Grosszuegig gegen die Frist, damit der Test auf einer belasteten
	// Maschine nicht flackert. Er faellt trotzdem, wenn die Frist fehlt:
	// dann laeuft er in die 30 s des Fakes.
	if dauer > 5*time.Second {
		t.Errorf("Dauer %v — die Frist greift nicht", dauer)
	}
}

// TestReadPfadSetztUeberallEineFrist deckt die Aufrufstellen ab, die kein
// eigener Test erreicht. Ohne das ist der naechste neue Sammler wieder
// fristlos, und man merkt es erst, wenn ein Besucher haengt.
func TestReadPfadSetztUeberallEineFrist(t *testing.T) {
	fakeBinOnPath(t, "git", "sleep 30")
	fakeBinOnPath(t, "bd", "sleep 30")
	kurzeFristen(t, 150)

	faelle := []struct {
		name string
		ruf  func()
	}{
		{"bdRun", func() { bdRun(t.TempDir(), "list", "--json") }},
		{"gitRun", func() { gitRun(t.TempDir(), "status", "--porcelain") }},
		{"RepoRoot", func() { RepoRoot(t.TempDir()) }},
	}
	for _, f := range faelle {
		t.Run(f.name, func(t *testing.T) {
			start := time.Now()
			f.ruf()
			if dauer := time.Since(start); dauer > 5*time.Second {
				t.Errorf("%s brauchte %v — keine Frist gesetzt", f.name, dauer)
			}
		})
	}
}

// TestRenderDotSVGGibtBeiHaengendemDotAuf ist der Sonderfall: RenderDotSVG
// geht nicht durch shared.Run, weil es stdin braucht, und hat deshalb seine
// eigene Frist, die kein anderer Test beruehrt.
func TestRenderDotSVGGibtBeiHaengendemDotAuf(t *testing.T) {
	fakeBinOnPath(t, "dot", "sleep 30")
	kurzeFristen(t, 150)

	start := time.Now()
	svg, ok := RenderDotSVG(t.TempDir(), `digraph { "a" -> "b" }`)
	dauer := time.Since(start)

	if ok || svg != "" {
		t.Errorf("ok=%v svg=%q — ein haengendes dot darf kein Ergebnis liefern", ok, svg)
	}
	if dauer > 5*time.Second {
		t.Errorf("Dauer %v — die Frist greift nicht", dauer)
	}
}

// kurzeHookFrist verkuerzt die Hook-Frist fuer einen Test. Gleicher Grund wie
// kurzeFristen oben: die echten 5 s zu warten, nur um zu zeigen, dass die
// Frist bindet, macht den Test zum Streichkandidaten.
func kurzeHookFrist(t *testing.T, ms int) {
	t.Helper()
	alt := hookTimeoutMs
	hookTimeoutMs = ms
	t.Cleanup(func() { hookTimeoutMs = alt })
}

// hookAufPfad legt einen Hook an der Stelle ab, an der hookPath ihn sucht, und
// zeigt CC_KNOWLEDGE_SCRIPTS_DIR dorthin. hookPath nimmt das ELTERNverzeichnis
// von ScriptsDir und haengt hooks/ an — der Test muss dieselbe Rechnung machen,
// sonst prueft er den "Hook nicht gefunden"-Zweig statt des Subprozesses.
func hookAufPfad(t *testing.T, script string) {
	t.Helper()
	basis := t.TempDir()
	hooks := filepath.Join(basis, "hooks")
	if err := os.MkdirAll(hooks, 0o755); err != nil {
		t.Fatalf("hooks/ anlegen: %v", err)
	}
	ziel := filepath.Join(hooks, "inject-project-context.sh")
	if err := os.WriteFile(ziel, []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
		t.Fatalf("Hook anlegen: %v", err)
	}
	t.Setenv("CC_KNOWLEDGE_SCRIPTS_DIR", filepath.Join(basis, "scripts"))
	if got := hookPath(); got != ziel {
		t.Fatalf("hookPath()=%q, erwartet %q — der Test zielt daneben", got, ziel)
	}
}

// TestCollectHookInjectGibtBeiHaengendemHookAuf ist die Luecke, die gkp-uc7.7
// fuer die collectors gefunden hatte und die hier offen geblieben war:
// realRunner hatte eine Frist, aber KEIN cmd.WaitDelay.
//
// Der Fall ist nicht theoretisch, sondern genau der aus shared.WaitDelay
// gemessene — und hier schaerfer als anderswo, weil der Hook per Bauart ein
// bash-Skript ist, das Kindprozesse startet. Die Frist toetet die Shell, das
// Enkelkind erbt stdout und haelt die Pipe offen; cmd.Run wartet dann auf die
// Kopiergoroutine statt auf den Prozess.
//
// Gemessen am 2026-09-14 gegen einen Hook aus `sleep 30`: ohne WaitDelay
// 30,0 s trotz 150 ms Frist, mit WaitDelay 2,15 s. Die 2 s sind
// shared.WaitDelay selbst — dieser Fall kostet sie als einziger, wie im
// Schreibpfad auch.
//
// Gegenprobe: cmd.WaitDelay in realRunner entfernen -> dieser Test laeuft 30 s
// und faellt.
func TestCollectHookInjectGibtBeiHaengendemHookAuf(t *testing.T) {
	ResetHookCache()
	t.Cleanup(ResetHookCache)
	hookAufPfad(t, "sleep 30") // ohne exec: Enkelkind haelt die Pipe
	kurzeHookFrist(t, 150)

	start := time.Now()
	got := CollectHookInject(t.TempDir())
	dauer := time.Since(start)

	if got.Ok {
		t.Errorf("Ok=true — ein haengender Hook darf kein Ergebnis liefern")
	}
	if got.Error == "" {
		t.Errorf("Error ist leer — der Ausfall muss benannt werden")
	}
	if dauer > 5*time.Second {
		t.Errorf("Dauer %v — die Frist bindet nicht (fehlt cmd.WaitDelay?)", dauer)
	}
}
