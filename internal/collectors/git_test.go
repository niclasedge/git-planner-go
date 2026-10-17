package collectors

import (
	"strings"
	"testing"
	"time"
)

// Tests für gkp-43s: der Schreibpfad (POST /action/*) darf nicht unbegrenzt auf
// git warten. Ein hängendes `git push` — Credential-Prompt, totes Remote —
// blockierte die Request-Goroutine über den 60-s-writeTimeout aus main.go
// hinaus, weil gitAction als einziger Subprozess-Pfad im Repo ohne Context lief.
// Gemessen vor der Umstellung: ein Fake-git mit `sleep 30` lief volle 30 s und
// kam dann mit ok=true zurück — der Hänger war nicht nur unbegrenzt, er sah am
// Ende auch noch wie ein Erfolg aus.

// kurzeGitFristen kürzt die Schreibpfad-Fristen für einen Test. Dasselbe Motiv
// wie kurzeFristen in deadline_test.go: einen 20-s-Beweis führt niemand zweimal.
func kurzeGitFristen(t *testing.T, aktionMs, pushMs int) {
	t.Helper()
	altAktion, altPush := gitActionTimeoutMs, gitPushTimeoutMs
	gitActionTimeoutMs, gitPushTimeoutMs = aktionMs, pushMs
	t.Cleanup(func() {
		gitActionTimeoutMs, gitPushTimeoutMs = altAktion, altPush
	})
}

// haengendesGit stellt ein git bereit, das nie zurückkommt. 30 s ist reichlich
// mehr als jede Frist, die diese Tests setzen, und reichlich weniger als das
// Go-Test-Timeout — der Testlauf selbst bleibt also nicht stehen.
//
// `exec` ist Absicht: so wird das sleep selbst zum git-Prozess, der Kill trifft
// es direkt, und der Fall kehrt nach der Frist zurück statt nach Frist +
// WaitDelay. Das spart in der Tabelle unten Sekunden. Dass die Frist auch dann
// bindet, wenn ein Enkelkind die Pipe offen hält, prüft
// TestGitAktionKommtTrotzPipeHaltendemEnkelkindZurueck eigens.
func haengendesGit(t *testing.T) {
	t.Helper()
	fakeBinOnPath(t, "git", "exec sleep 30")
}

// pruefeAbbruch verlangt, was ein Timeout liefern muss: kein Erfolg, ein
// nachvollziehbarer Grund, und eine Rückkehr innerhalb der Frist. Die 5 s sind
// grosszügig gegen Flackern auf einer belasteten Maschine; ohne Frist läuft der
// Fall in die 30 s des Fakes und fällt trotzdem.
func pruefeAbbruch(t *testing.T, got map[string]any, dauer time.Duration) {
	t.Helper()
	if got["ok"] == true {
		t.Errorf("ok=true — ein abgebrochenes Kommando ist kein Erfolg")
	}
	out, _ := got["output"].(string)
	if !strings.Contains(out, "Frist") {
		t.Errorf("output=%q — der Grund des Abbruchs muss drinstehen, nicht geschluckt werden", out)
	}
	if dauer > 5*time.Second {
		t.Errorf("Dauer %v — die Frist greift nicht", dauer)
	}
}

func TestGitAktionenBrechenNachDerFristAb(t *testing.T) {
	haengendesGit(t)
	kurzeGitFristen(t, 150, 150)

	faelle := []struct {
		name string
		ruf  func(cwd string) map[string]any
	}{
		{"GitAdd", func(cwd string) map[string]any { return GitAdd(cwd) }},
		{"GitCommit", func(cwd string) map[string]any { return GitCommit(cwd, "irgendwas") }},
		{"GitDelete", func(cwd string) map[string]any { return GitDelete(cwd, "zweig") }},
		{"GitMerge", func(cwd string) map[string]any { return GitMerge(cwd, "zweig") }},
		{"GitPush", func(cwd string) map[string]any { return GitPush(cwd, "zweig") }},
	}
	for _, f := range faelle {
		t.Run(f.name, func(t *testing.T) {
			start := time.Now()
			got := f.ruf(t.TempDir())
			pruefeAbbruch(t, got, time.Since(start))
		})
	}
}

// TestGitPushHatEineEigeneFrist nagelt die Verdrahtung fest: push hängt an
// gitPushTimeoutMs, nicht an der lokalen Frist. Ohne diesen Test fiele eine
// Rückkehr zu gitAction(cwd, "push", …) niemandem auf — der Test oben wäre
// weiter grün, weil dort beide Fristen kurz sind.
func TestGitPushHatEineEigeneFrist(t *testing.T) {
	haengendesGit(t)
	// Lokale Frist absichtlich länger als die 5-s-Schranke: greift sie für
	// push, läuft der Fall in sie hinein und der Test fällt.
	kurzeGitFristen(t, 30_000, 150)

	start := time.Now()
	got := GitPush(t.TempDir(), "zweig")
	pruefeAbbruch(t, got, time.Since(start))
}

// TestGitAktionKommtTrotzPipeHaltendemEnkelkindZurueck ist der Grund für
// cmd.WaitDelay. Ohne exec bleibt die Shell der git-Prozess und `sleep` ihr
// Kind: der Context killt die Shell, das sleep erbt die stdout-Pipe und hält
// sie offen, und Wait wartete ohne WaitDelay die vollen 30 s — die Frist wäre
// dann nur Dekoration. Gemessen in shared.WaitDelay, hier für den Schreibpfad
// festgenagelt. Dieser Fall kostet als einziger die 2 s WaitDelay.
func TestGitAktionKommtTrotzPipeHaltendemEnkelkindZurueck(t *testing.T) {
	fakeBinOnPath(t, "git", "sleep 30") // ohne exec: Enkelkind hält die Pipe
	kurzeGitFristen(t, 150, 150)

	start := time.Now()
	got := GitAdd(t.TempDir())
	pruefeAbbruch(t, got, time.Since(start))
}

// TestGitAktionUnterbindetPrompts prüft die erste Verteidigungslinie: git darf
// gar nicht erst auf eine Eingabe warten, die niemand geben kann.
func TestGitAktionUnterbindetPrompts(t *testing.T) {
	fakeBinOnPath(t, "git", `printf 'prompt=[%s] ssh=[%s]\n' "$GIT_TERMINAL_PROMPT" "$GIT_SSH_COMMAND"`)
	// Ohne diese Zeile misst der Test die Umgebung statt das Verhalten: gitEnv
	// setzt BatchMode nur, wenn der Aufrufer kein eigenes GIT_SSH_COMMAND
	// mitbringt — und manche Umgebungen tun das (eine Sandbox mit
	// socat-Proxy etwa). Der Test schlug dann fehl, obwohl der Code richtig ist.
	t.Setenv("GIT_SSH_COMMAND", "")

	got := GitAdd(t.TempDir())

	if got["ok"] != true {
		t.Fatalf("ok=%v — der Erfolgsfall muss unverändert durchgehen: %v", got["ok"], got["output"])
	}
	out, _ := got["output"].(string)
	if !strings.Contains(out, "prompt=[0]") {
		t.Errorf("output=%q — GIT_TERMINAL_PROMPT=0 fehlt, git würde am Terminal fragen", out)
	}
	if !strings.Contains(out, "BatchMode=yes") {
		t.Errorf("output=%q — ohne BatchMode wartet ssh auf eine Passphrase", out)
	}
}

// Ein eigenes GIT_SSH_COMMAND gehört dem Aufrufer; wir überschreiben es nicht.
func TestGitAktionLaesstEigenesSSHKommandoStehen(t *testing.T) {
	fakeBinOnPath(t, "git", `printf 'ssh=[%s]\n' "$GIT_SSH_COMMAND"`)
	t.Setenv("GIT_SSH_COMMAND", "ssh -i /pfad/zum/key")

	out, _ := GitAdd(t.TempDir())["output"].(string)
	if !strings.Contains(out, "ssh=[ssh -i /pfad/zum/key]") {
		t.Errorf("output=%q — eigenes GIT_SSH_COMMAND wurde überschrieben", out)
	}
}

// GitDiff geht nicht durch gitAction, sondern durch gitCapture — der zweite
// fristlose exec in der Datei. Es liest nur, hängt also an der Lesepfad-Frist.
func TestGitDiffBrichtNachDerFristAb(t *testing.T) {
	haengendesGit(t)
	kurzeFristen(t, 150) // shared.GitTimeoutMs, die Lesepfad-Frist

	start := time.Now()
	got := GitDiff(t.TempDir(), "")
	dauer := time.Since(start)

	diff, _ := got["diff"].(string)
	if !strings.Contains(diff, "abgebrochen") {
		t.Errorf("diff=%q — ein Abbruch darf nicht als \"(keine Änderungen)\" durchgehen", diff)
	}
	if dauer > 5*time.Second {
		t.Errorf("Dauer %v — die Frist greift nicht", dauer)
	}
}

// Der Erfolgsfall bleibt, wie er war: ok=true und der Output unverändert
// durchgereicht — die Umstellung darf nur Hänger betreffen.
func TestGitAktionErfolgsfallUnveraendert(t *testing.T) {
	fakeBinOnPath(t, "git", `echo "alles gut"`)

	got := GitAdd(t.TempDir())

	if got["ok"] != true {
		t.Errorf("ok=%v, erwartet true", got["ok"])
	}
	if got["cmd"] != "git add ." {
		t.Errorf("cmd=%v, erwartet \"git add .\"", got["cmd"])
	}
	if got["output"] != "alles gut" {
		t.Errorf("output=%v, erwartet \"alles gut\"", got["output"])
	}
}
