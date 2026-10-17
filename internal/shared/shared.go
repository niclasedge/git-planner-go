// Package shared holds low-level helpers — Go-native ports of the Deno app's
// subprocess/filesystem primitives (shared.ts). Intentionally framework-free so
// collectors stay unit-testable in isolation (mirrors the Python _run/read_text
// split).
package shared

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"time"
	"unicode"
)

// RunOptions configures Run. Zero values disable the respective feature.
type RunOptions struct {
	Cwd       string
	TimeoutMs int
}

// Deadlines for the page's read path (gkp-80s). Every read-path subprocess is
// fail-open — Run collapses any failure into ("", false) and the card renders
// "nicht verfügbar" — so a deadline turns an unbounded hang into an ordinary
// miss. That matters more here than the raw numbers suggest: page context is
// single-flighted per project, so one stuck subprocess parks every visitor of
// that project behind it, not just the one who triggered it.
//
// The values come from measurements, not guesses (full table in
// viking://resources/tools/beads/bd-graph-kosten.md):
//
//	git rev-parse / worktree list      3–16 ms
//	dot -Tsvg                         26–43 ms
//	bd show <epic> --children        ~400 ms
//	bd list -t epic --json             736 ms
//	bd graph --dot <epic>        2 000–5 339 ms   ← the outlier
//
// bd gets by far the widest window because `bd graph` on a 16 MB embedded
// Dolt DB legitimately takes seconds, and a tight deadline would drop the
// plan graph on exactly the large repos that need it most. git and dot get
// short ones: they run three orders of magnitude below their limit, so
// anything near it is a hang rather than work in progress.
//
// These bound a SINGLE call, not a collector. A repo with twelve maps spends
// ~21 s in bd all told and that is normal; a per-collector budget large
// enough to allow that would stop being a safeguard.
//
// var rather than const purely so a test can shorten them: proving that a
// hung subprocess ends in a fail-open card is the whole point of gkp-80s, and
// a test that waits 20 s to prove it would be deleted by the first person in
// a hurry. Nothing in the running program writes these.
var (
	BdTimeoutMs  = 20_000
	GitTimeoutMs = 10_000
	DotTimeoutMs = 10_000
)

// WaitDelay is the second half of the deadline, and without it the first half
// does not work. exec.CommandContext kills only the process it started; Output
// then keeps waiting for the stdout pipe to close, and any grandchild that
// inherited that pipe holds it open. Measured while writing the gkp-80s test:
// a fake `bd` of `#!/bin/sh` + `sleep 30` sailed straight past a 150 ms
// deadline and blocked the call for the full 30 s, because the deadline killed
// the shell and `sleep` kept the pipe.
//
// WaitDelay bounds exactly those two delays — a child that ignores the
// cancellation, and a child that exits leaving its pipes open — so Wait
// returns instead of hanging.
//
// Honest residue: a grandchild can survive this. WaitDelay unblocks the
// caller, it does not reap the process tree; that would need a process group
// per call and a kill by negative pid, which is platform-specific. bd, git and
// dot do not background children holding stdout, so the leak is theoretical
// while the hang was not.
const WaitDelay = 2 * time.Second

// Run executes a command and returns its trimmed stdout on success (exit code
// 0), or ("", false) on non-zero exit, missing binary, or timeout. Replaces
// subprocess.run(..., capture_output=True) + the broad except in knowledge.py.
func Run(cmd []string, opts RunOptions) (string, bool) {
	if len(cmd) == 0 {
		return "", false
	}
	ctx := context.Background()
	if opts.TimeoutMs > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(opts.TimeoutMs)*time.Millisecond)
		defer cancel()
	}
	c := exec.CommandContext(ctx, cmd[0], cmd[1:]...)
	c.WaitDelay = WaitDelay
	if opts.Cwd != "" {
		c.Dir = opts.Cwd
	}
	out, err := c.Output() // captures stdout; stderr discarded like the Deno version
	if err != nil {
		return "", false
	}
	// Trim trailing whitespace only, matching `.replace(/\s+$/, "")`.
	return strings.TrimRightFunc(string(out), unicode.IsSpace), true
}

// ReadText reads a UTF-8 file, returning ("", false) instead of erroring.
func ReadText(path string) (string, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	return string(b), true
}

// ParseJSON parses JSON into T, returning (zero, false) on empty/malformed input
// (mirrors a guarded json.loads).
func ParseJSON[T any](text string) (T, bool) {
	var v T
	if text == "" {
		return v, false
	}
	if err := json.Unmarshal([]byte(text), &v); err != nil {
		return v, false
	}
	return v, true
}
