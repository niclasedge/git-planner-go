package bdops

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestInstalledBdStillWritesWallclockZ is the tripwire for the upgrade hazard
// described at "dates coming back" in bdops.go. Every other test in this file
// checks this package against a STRING that bd once wrote; this one asks the
// installed bd itself, because the hazard is not that the code is wrong today —
// it is that bd changes under it and nothing goes red.
//
// The defect is fixed upstream: PR #5295 (issue #5233) rewrites every
// `return t, nil` in bd's internal/timeparsing/parser.go to `return t.UTC(),
// nil`. The fixed bd keeps the trailing "Z" and simply starts meaning it, so a
// stored value cannot be told apart from the broken one by looking at it —
// which is precisely why the compensation in ParseUserDate would silently
// become a wrong-by-the-UTC-offset bug the day bd is upgraded.
//
// Why this is a TEST and not a runtime check (decided under gkp-62g, on these
// measurements):
//
//   - The version cannot answer it. Measured against the published sources:
//     v1.2.1 returns t.UTC() (fixed), v1.2.2 — the newer one, and the one
//     installed here — returns t (broken). CHANGELOG says why: 1.2.2 is a
//     recovery release "cut from the v1.1.2 tree ... not from main". A
//     "compensate below version X" threshold is therefore provably wrong for a
//     real pair of releases, and the fix is named in no changelog entry at all,
//     so there is no X to name in the first place.
//
//   - Probing at runtime measures the right thing, but the write has to land
//     somewhere: either in the user's own database (a probe bead per repo per
//     start, with its Dolt commit and its line in interactions.jsonl) or in a
//     throwaway one (measured: bd init 1.6 s, plus three more bd calls, on
//     every start). Caching that by version brings the version back in as a
//     key, and the whole path then runs once per bd build in production, where
//     no one ever looks at it.
//
//   - Dropping the compensation and demanding a fixed bd has no target: the
//     latest GitHub release IS v1.2.2 (2026-08-15), the broken one, and go.mod
//     retracts v1.2.1. Today the fix exists only on main, so every bd a user
//     can install still needs the compensation.
//
// So the compensation stays, and the flip is made loud where it costs nothing:
// `just deploy` runs the gate, the gate runs `go test ./...`, and this test
// fails with instructions before the service that would show shifted dates is
// ever restarted. The gap it leaves, stated plainly: a bd upgraded but never
// followed by a build keeps serving dates two hours off until the next gate.
//
// The zone is pinned to Asia/Tokyo (+9, no DST) rather than taken from the
// machine, so the test still means something on a UTC build agent, where the
// offset is zero and the defect is invisible.
func TestInstalledBdStillWritesWallclockZ(t *testing.T) {
	if testing.Short() {
		t.Skip("-short: this one drives the installed bd against a throwaway database")
	}
	if _, err := exec.LookPath("bd"); err != nil {
		t.Skipf("no bd on PATH: %v", err)
	}
	tokyo, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		t.Skipf("no tzdata for Asia/Tokyo: %v", err)
	}

	// A throwaway database in a temp directory: never the project's own. bd
	// finds a database by walking UP from its working directory, so a temp dir
	// outside any repo is also the only place where a stray bd call cannot
	// reach real issues.
	dir := t.TempDir()
	bd := func(args ...string) string {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		// -C on every call except init, which refuses it before a project
		// exists ("cannot use -C directory ...: no beads project found").
		// cmd.Dir alone would do — bd walks up from there — but naming the
		// directory twice is what keeps a stray call out of a real repo.
		full := args
		if args[0] != "init" {
			full = append([]string{"-C", dir}, args...)
		}
		cmd := exec.CommandContext(ctx, "bd", full...)
		cmd.Dir = dir
		// TZ decides how bd reads a zone-less input; everything else (HOME
		// above all) has to stay, or bd changes behaviour for other reasons.
		cmd.Env = append(os.Environ(), "TZ=Asia/Tokyo")
		var stdout, stderr strings.Builder
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		if err := cmd.Run(); err != nil {
			// Not a skip: if bd's own interface moved, that is the upgrade this
			// test exists to notice, and a silent skip would hide it.
			t.Fatalf("bd %s: %v\nstdout: %s\nstderr: %s",
				strings.Join(args, " "), err, stdout.String(), stderr.String())
		}
		return stdout.String()
	}

	bd("init", "--prefix", "zzdate", "--non-interactive")
	id := lastLine(bd("q", "wallclock probe"))
	if id == "" {
		t.Fatal("bd q returned no id")
	}

	// 12:00 on a January day: far from midnight in both directions, so neither
	// regime can be mistaken for the other by a rounding accident, and clear of
	// the DST edges of the zones this ever runs in.
	const spec = "2026-01-15T12:00:00"
	bd("update", id, "--due", spec)

	stored := dueAt(t, bd("show", id, "--json"))
	const (
		wallclock = "2026-01-15T12:00:00Z" // the digits as given: bd 1.2.2
		realUTC   = "2026-01-15T03:00:00Z" // 12:00 Tokyo converted: bd with PR #5295
	)

	switch stored {
	case wallclock:
		// The regime this package is built for. Read it the way production
		// does and check that the instant really is noon in Tokyo.
		got, ok := parseUserDate(stored, tokyo)
		if !ok {
			t.Fatalf("parseUserDate rejected %q, which is what bd just wrote", stored)
		}
		if want := time.Date(2026, 1, 15, 12, 0, 0, 0, tokyo); !got.Equal(want) {
			t.Errorf("parseUserDate(%q) = %s, want %s", stored, got, want)
		}
	case realUTC:
		t.Fatalf(`bd HAS BEEN FIXED — the compensation in this package is now the bug.

  bd --due %s (TZ=Asia/Tokyo) stored %q.
  bd 1.2.2 stored %q; %q is the instant, i.e. PR #5295 is in this build.

What to change, both halves together:
  - ParseUserDate must stop re-anchoring. due_at and defer_until are then real
    UTC like created_at, so they read the way ParseEventStamp already does.
  - checkDateSpec may take zone-bearing input again (absoluteForms exists only
    because bd threw the zone away), and its measured table needs redoing.
  - The tables in the comments here and in bdops.go are from bd 1.2.2 and are
    now folklore. Re-measure the other fields the way this test does — write a
    zone-less value under a fixed TZ into a throwaway database and read the
    field back — before believing any of them.`,
			spec, stored, wallclock, realUTC)
	default:
		t.Fatalf(`bd writes a THIRD shape now: --due %s (TZ=Asia/Tokyo) stored %q,
neither the wall clock %q nor the instant %q. Re-measure before changing anything;
if that shape carries a real offset, ParseUserDate refuses it and the date goes
missing rather than wrong — see TestParseUserDateRefusesRealOffset.`,
			spec, stored, wallclock, realUTC)
	}
}

// lastLine returns the last non-empty line, which is where bd puts the id that
// `bd q` is asked for. Warnings (a second bd on PATH, for one) ride along.
func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if v := strings.TrimSpace(lines[i]); v != "" {
			return v
		}
	}
	return ""
}

// dueAt pulls due_at out of `bd show --json`, which answers with an array even
// for a single issue.
func dueAt(t *testing.T, raw string) string {
	t.Helper()
	var issues []struct {
		DueAt string `json:"due_at"`
	}
	if err := json.Unmarshal([]byte(raw), &issues); err != nil {
		t.Fatalf("bd show --json: %v\n%s", err, raw)
	}
	if len(issues) != 1 {
		t.Fatalf("bd show --json returned %d issues, want 1\n%s", len(issues), raw)
	}
	if issues[0].DueAt == "" {
		t.Fatalf("bd show --json carries no due_at\n%s", raw)
	}
	return issues[0].DueAt
}
