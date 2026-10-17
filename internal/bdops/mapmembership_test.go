package bdops

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// These tests drive the INSTALLED bd against a throwaway database, for the
// reason spelled out as case 3 in docs/spec-aufgaben-dashboard.md §9.1: a test
// that builds its fixture beside the real path checks a world that does not
// exist. The question here — "is this id an epic, and does that bead hang under
// it" — is answered by bd's own JSON and by nothing else, so a hand-written
// fixture would only pin this file's idea of what bd returns.
//
// The database is built once per package run and never leaves the test
// binary's temp directory: embedded Dolt, no shared server, no prefix
// registered anywhere, and nothing here ever touches the user's own databases.
// TestProbeDBStaysOutOfTheRealStore is the guard on that promise.

type probe struct {
	dir       string
	epic      string // an epic
	child     string //   child of epic
	grandkid  string //     grandchild of epic — the multi-level case
	unrelated string // a task with no parent at all
	notAnEpic string // exists, is a task, and gets used AS a map id
	err       error
}

var (
	probeOnce sync.Once
	probeVal  probe
)

// TestMain exists for the cleanup. probeDB cannot use t.TempDir(): the database
// is shared by every test in this file through sync.Once, and t.TempDir() is
// removed when the test that asked for it ends — the second caller would find
// an empty directory. So it is created with os.MkdirTemp and removed here, once
// the package is done. Measured: an embedded-Dolt database is ~1.7 MiB, which
// is small enough to forget about and big enough to notice after fifty runs.
func TestMain(m *testing.M) {
	code := m.Run()
	if probeVal.dir != "" {
		_ = os.RemoveAll(probeVal.dir)
	}
	os.Exit(code)
}

func probeDB(t *testing.T) probe {
	t.Helper()
	if _, err := exec.LookPath("bd"); err != nil {
		t.Skip("bd nicht installiert")
	}
	probeOnce.Do(func() { probeVal = buildProbeDB() })
	if probeVal.err != nil {
		t.Skipf("Wegwerf-Datenbank nicht baubar: %v", probeVal.err)
	}
	return probeVal
}

func buildProbeDB() probe {
	p := probe{}
	dir, err := os.MkdirTemp("", "bdops-map-*")
	if err != nil {
		p.err = err
		return p
	}
	p.dir = dir
	run := func(name string, args ...string) bool {
		cmd := exec.Command(name, args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			p.err = &runFailure{what: name + " " + strings.Join(args, " "), out: string(out), err: err}
			return false
		}
		return true
	}
	if !run("git", "init", "-q", ".") ||
		!run("git", "config", "user.email", "probe@example.invalid") ||
		!run("git", "config", "user.name", "Probe") ||
		!run("bd", "init", "--prefix", "zzmap", "--role", "maintainer", "--non-interactive") {
		return p
	}
	// `bd q` is quick capture: it prints the new id and nothing else.
	create := func(title string, extra ...string) string {
		args := append([]string{"q", title}, extra...)
		cmd := exec.Command("bd", args...)
		cmd.Dir = dir
		out, err := cmd.Output()
		if err != nil {
			p.err = &runFailure{what: "bd " + strings.Join(args, " "), out: string(out), err: err}
			return ""
		}
		fields := strings.Fields(strings.TrimSpace(string(out)))
		if len(fields) == 0 {
			p.err = &runFailure{what: "bd q", out: string(out)}
			return ""
		}
		return fields[len(fields)-1]
	}
	p.epic = create("Karte", "--type", "epic")
	p.child = create("Kind")
	p.grandkid = create("Enkel")
	p.unrelated = create("Fremd")
	p.notAnEpic = create("Kein Epic")
	if p.err != nil {
		return p
	}
	if !run("bd", "update", "--parent", p.epic, "--", p.child) ||
		!run("bd", "update", "--parent", p.child, "--", p.grandkid) {
		return p
	}
	return p
}

type runFailure struct {
	what string
	out  string
	err  error
}

func (r *runFailure) Error() string {
	msg := r.what + ": " + r.out
	if r.err != nil {
		msg += " " + r.err.Error()
	}
	return msg
}

// TestMapMustExist is gkp-few at the root: "gkp-yy5" never existed, and that is
// precisely why the call created its own file to be judged against.
func TestMapMustExist(t *testing.T) {
	p := probeDB(t)
	res, err := CheckMapMembership(p.dir, "zzmap-nosuchthing", []string{p.child})
	if err == nil {
		t.Fatal("eine nicht existierende Karte wurde durchgelassen")
	}
	if res.Failed {
		t.Errorf("bd hat geantwortet, das Ergebnis darf nicht als bd-Fehler gelten: %+v", res)
	}
	for _, want := range []string{"zzmap-nosuchthing", p.child} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Meldung nennt %q nicht: %v", want, err)
		}
	}
}

// TestMapMustBeAnEpic is the first of the two edge cases. An id that exists but
// is not an epic is refused — see the reasoning at the head of
// mapmembership.go.
func TestMapMustBeAnEpic(t *testing.T) {
	p := probeDB(t)
	_, err := CheckMapMembership(p.dir, p.notAnEpic, []string{p.notAnEpic})
	if err == nil {
		t.Fatalf("Bead %s ist kein Epic und wurde trotzdem als Karte akzeptiert", p.notAnEpic)
	}
	if !strings.Contains(strings.ToLower(err.Error()), "epic") {
		t.Errorf("Meldung sagt nicht, dass eine Karte ein Epic sein muss: %v", err)
	}
	if !strings.Contains(err.Error(), p.notAnEpic) {
		t.Errorf("Meldung nennt die Id nicht: %v", err)
	}
}

// TestBeadMustHangUnderTheMap: the bead exists, the map exists, and they have
// nothing to do with each other. The refusal has to name BOTH ids — a reader
// who gets only one searches at the wrong end.
func TestBeadMustHangUnderTheMap(t *testing.T) {
	p := probeDB(t)
	_, err := CheckMapMembership(p.dir, p.epic, []string{p.unrelated})
	if err == nil {
		t.Fatalf("Bead %s haengt nicht unter %s und wurde durchgelassen", p.unrelated, p.epic)
	}
	for _, want := range []string{p.epic, p.unrelated} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Meldung nennt %q nicht: %v", want, err)
		}
	}
}

// TestMultiLevelChainIsAccepted is the second edge case and the actual trap: a
// check that looks only at direct parenthood would refuse the grandchild. That
// would be a cure worse than the disease — it makes a legitimate close
// impossible in order to catch a typo.
func TestMultiLevelChainIsAccepted(t *testing.T) {
	p := probeDB(t)
	if _, err := CheckMapMembership(p.dir, p.epic, []string{p.grandkid}); err != nil {
		t.Fatalf("Enkel %s unter Karte %s wurde abgelehnt: %v", p.grandkid, p.epic, err)
	}
}

// TestDirectChildAndTheMapItselfAreAccepted. The map counts as a member of its
// own card — measured: gkp-uc7.jsonl carries an entry whose bead IS gkp-uc7. An
// epic is closed like any other bead.
func TestDirectChildAndTheMapItselfAreAccepted(t *testing.T) {
	p := probeDB(t)
	if _, err := CheckMapMembership(p.dir, p.epic, []string{p.child, p.grandkid, p.epic}); err != nil {
		t.Fatalf("Kind/Enkel/Karte selbst wurden abgelehnt: %v", err)
	}
}

// TestUnknownBeadIsRefused: a bead that does not exist can hang under no card
// at all. bd reports it on stderr only and returns the remaining hits with exit
// 0 — a caller reading just the exit code never sees it.
func TestUnknownBeadIsRefused(t *testing.T) {
	p := probeDB(t)
	_, err := CheckMapMembership(p.dir, p.epic, []string{"zzmap-neverwas"})
	if err == nil {
		t.Fatal("ein nicht existierender Bead wurde durchgelassen")
	}
	if !strings.Contains(err.Error(), "zzmap-neverwas") {
		t.Errorf("Meldung nennt den Bead nicht: %v", err)
	}
}

// TestUnanswerableCountsAsABdFailure: with no database bd cannot answer. The
// Result must be marked as a bd failure, or internal/server maps it to 400 and
// thereby claims the caller mistyped something.
func TestUnanswerableCountsAsABdFailure(t *testing.T) {
	if _, err := exec.LookPath("bd"); err != nil {
		t.Skip("bd nicht installiert")
	}
	empty := t.TempDir()
	res, err := CheckMapMembership(empty, "zzmap-whatever", []string{"zzmap-whatever.1"})
	if err == nil {
		t.Fatal("ohne Datenbank wurde durchgelassen")
	}
	if !res.Failed {
		t.Errorf("Ergebnis ist nicht als bd-Fehler markiert: %+v", res)
	}
}

// TestInvalidIDsNeverReachBd: the same guard as everywhere else in this
// package, and it has to bite before the subprocess.
func TestInvalidIDsNeverReachBd(t *testing.T) {
	for _, tc := range []struct{ mapID, bead string }{
		{"--force", "a"},
		{"map", "--force"},
		{"", "a"},
		{"map", ""},
	} {
		if _, err := CheckMapMembership(t.TempDir(), tc.mapID, []string{tc.bead}); err == nil {
			t.Errorf("map=%q bead=%q wurde durchgelassen", tc.mapID, tc.bead)
		}
	}
}

// TestProbeDBStaysOutOfTheRealStore is a guard, not a feature test: these tests
// create a real beads database, and it must never land in the repo or under
// ~/.beads.
func TestProbeDBStaysOutOfTheRealStore(t *testing.T) {
	p := probeDB(t)
	home, _ := os.UserHomeDir()
	for _, forbidden := range []string{filepath.Join(home, ".beads"), filepath.Join(home, "Work")} {
		if strings.HasPrefix(p.dir, forbidden) {
			t.Fatalf("Wegwerf-Datenbank liegt unter %s: %s", forbidden, p.dir)
		}
	}
}
