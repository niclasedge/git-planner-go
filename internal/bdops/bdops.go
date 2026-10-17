// Package bdops builds and runs the `bd` invocations behind the MCP beads
// write API. It is deliberately separate from internal/collectors, which only
// ever READS bd to feed the page: a mutation has different failure semantics,
// and mixing the two would put a fail-open reader next to a must-fail-loudly
// writer in the same file.
//
// Three properties this package exists to guarantee:
//
//  1. Failures are reported, not swallowed. shared.Run discards stderr and
//     collapses every failure into ("", false), which is right for the
//     collectors (a missing bd must not break the page) and wrong here — a
//     close that did not happen must never look like one that did.
//
//  2. No shell, ever. Everything goes through exec with an argv slice, and
//     "--" precedes positional ids so a value can never be read as a flag.
//
//  3. No implicit target. `bd update` and `bd close` with no id operate on
//     "the last touched issue" — a convenience for humans at a terminal and a
//     footgun for an API, where an empty id list would mutate whatever bead
//     some unrelated earlier call happened to touch. Every mutation here
//     requires at least one explicit, validated id.
package bdops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/niclasedge/git-planner-go/internal/shared"
)

// DefaultTimeoutMs bounds a single bd call. Writes go through Dolt and are
// slower than reads; measured worst case in this repo was a few seconds, so
// 30 s is head-room rather than a target.
const DefaultTimeoutMs = 30000

// idPattern mirrors internal/mcp's beadIDPattern and exists for the same
// reason: the "--" terminator in every argv below already prevents a
// leading-dash value from being parsed as a flag, so this only has to reject
// the obviously-not-an-id. Staying permissive is deliberate — an earlier,
// stricter pattern rejected legitimate bd ids like "gt--xyz" (bd's own escape
// form) and "demo-axk.1.2" (a nested sub-bead).
var idPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// ValidID reports whether id is safe to pass to bd as a positional argument.
func ValidID(id string) bool { return idPattern.MatchString(id) }

// The three values Result.Reason can take. They are constants rather than
// literals because a caller REACTS to them — internal/taskops maps them onto
// HTTP status, and a typo there would silently land in the default branch.
//
// ReasonNotFound means "no bd on PATH", NOT "no such bead": bd answers an
// unknown id with exit 1 (measured: `no issue found matching "tosp-zzz"`), so
// that case arrives as ReasonExit.
const (
	ReasonExit     = "exit"
	ReasonTimeout  = "timeout"
	ReasonNotFound = "not-found"
)

// Result is one bd invocation, in enough detail to explain a failure to the
// caller instead of just denying it.
type Result struct {
	Args     []string `json:"args"`
	Stdout   string   `json:"stdout,omitempty"`
	Stderr   string   `json:"stderr,omitempty"`
	ExitCode int      `json:"exit_code"`
	Failed   bool     `json:"failed"`
	// Reason distinguishes the failure modes a caller reacts to differently:
	// "exit" (bd ran and refused), "timeout", "not-found" (no bd on PATH).
	Reason string `json:"reason,omitempty"`
}

// Error renders a Result as a message worth showing a user: bd's own stderr
// where there is one, since bd's diagnostics are better than anything this
// layer could invent.
func (r Result) Error() string {
	if !r.Failed {
		return ""
	}
	msg := strings.TrimSpace(r.Stderr)
	if msg == "" {
		msg = strings.TrimSpace(r.Stdout)
	}
	if msg == "" {
		msg = "bd exit " + strconv.Itoa(r.ExitCode)
	}
	return "bd " + strings.Join(r.Args, " ") + ": " + msg
}

// Run executes `bd <args...>` in cwd and captures both streams. It returns a
// Result even on failure — the error detail IS the payload here, not an
// exception to be discarded.
func Run(cwd string, timeoutMs int, args ...string) Result {
	if timeoutMs <= 0 {
		timeoutMs = DefaultTimeoutMs
	}
	res := Result{Args: args}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutMs)*time.Millisecond)
	defer cancel()

	cmd := exec.CommandContext(ctx, "bd", args...)
	cmd.WaitDelay = shared.WaitDelay // ohne das bindet die Frist nicht — siehe shared.WaitDelay
	if cwd != "" {
		cmd.Dir = filepath.Clean(cwd)
	}
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	res.Stdout = strings.TrimRight(stdout.String(), " \t\r\n")
	res.Stderr = strings.TrimRight(stderr.String(), " \t\r\n")

	switch {
	case err == nil:
		return res
	case ctx.Err() != nil:
		res.Failed, res.Reason, res.ExitCode = true, ReasonTimeout, -1
	case errors.Is(err, exec.ErrNotFound):
		res.Failed, res.Reason, res.ExitCode = true, ReasonNotFound, -1
	default:
		res.Failed, res.Reason = true, ReasonExit
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			res.ExitCode = ee.ExitCode()
		} else {
			res.ExitCode = -1
		}
	}
	return res
}

// checkIDs validates a mutation's target list, enforcing property 3 above.
func checkIDs(ids []string) error {
	if len(ids) == 0 {
		return errors.New("mindestens eine id nötig: bd würde ohne id das zuletzt angefasste Issue treffen")
	}
	for _, id := range ids {
		if !ValidID(id) {
			return fmt.Errorf("ungültige bead-id %q", id)
		}
	}
	return nil
}

// ---------------------------------------------------------------- dates

// isoDateSpec matches bd's plain absolute date form and nothing else. It is the
// only shape in bd's date grammar that is fixed enough to be worth recognising
// here, and it separates the two ways an absolute date goes wrong: a real
// calendar mistake from a value that misses bd's layouts altogether.
var isoDateSpec = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// isoDateShape asks a narrower question than isoDateSpec: does this value
// CONTAIN something a human would call a date? It is the signal that the caller
// meant an absolute date rather than a relative or spoken one, and nothing
// else — deciding whether that date is usable is absoluteForms' job.
var isoDateShape = regexp.MustCompile(`\d{4}-\d{1,2}-\d{1,2}`)

// absoluteForms are the layouts bd both parses as an absolute date AND stores
// without loss. They are deliberately ZONE-LESS: bd keeps the digits it was
// given and stamps them with a "Z", so a value that names a zone has that zone
// thrown away (see checkDateSpec and the block below on reading dates back).
var absoluteForms = []string{
	"2006-01-02",
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05",
}

// checkDateSpec is deliberately almost empty, and that is the decision it
// encodes: --due and --defer are NOT validated here the way --parent is.
//
// A bead id is a simple pattern; bd's date grammar is not. It takes +6h, +1d,
// +2w, tomorrow, next monday and 2025-01-15, and rebuilding that here would
// mean owning a copy that has to stay in step with bd forever — every drift
// showing up as this API refusing what bd would have accepted. bd's own refusal
// is also the better message. Measured with bd 1.2.2, both on create and on
// update:
//
//	$ bd update --due garbage -- zzprobe-eqm
//	Error: invalid --due format "garbage". Examples: +6h, tomorrow, next monday, 2025-01-15
//	$ echo $?
//	1
//
// It names the flag, quotes the value, lists the forms, and exits non-zero, so
// Run reports Reason "exit" and Result.Error() hands that sentence to the
// caller unchanged. Nothing this layer could add would improve on it.
//
// The exception is the input bd gets silently WRONG, and the line runs exactly
// there: an ABSOLUTE date is checked here, everything else stays bd's. bd
// parses an absolute date with a closed set of layouts and hands whatever
// misses them to a natural-language parser, which reads the month and day
// digits as a clock time. The value does not fail — it becomes a timestamp
// today, usually already in the past, and looks deliberate everywhere after.
// Measured against a throwaway database, bd 1.2.2 (6c124203e), 2026-09-13,
// every line exit 0:
//
//	--due 2026-02-30            ->  due_at = 2026-09-13T02:30:00Z
//	--due 2025-13-45            ->  due_at = 2026-09-13T13:45:00Z
//	--due "2026-01-15 23:00"    ->  due_at = 2026-09-13T01:15:00Z
//	--due "2026-1-15"           ->  due_at = 2026-09-13T01:15:01Z   (one missing zero)
//	--due " 2026-01-15 "        ->  due_at = 2026-09-13T01:15:00Z   (padding alone)
//	--defer "2026-01-15 23:00"  ->  defer_until = 2026-09-13T01:15:01Z
//
// A second, quieter loss has the same root: bd stores the digits it parsed with
// a literal "Z", so an input that NAMES a zone has that zone dropped, and two
// different instants collapse onto one value.
//
//	--due 2026-01-15T23:00:00Z       ->  2026-01-15T23:00:00Z
//	--due 2026-01-15T23:00:00+01:00  ->  2026-01-15T23:00:00Z
//	--due 2026-01-15T23:00:00-05:00  ->  2026-01-15T23:00:00Z
//
// Hence absoluteForms: the three ZONE-LESS layouts are exactly the inputs whose
// round trip through bd 1.2.2 is lossless, which is the precondition
// ParseUserDate below depends on — it re-anchors bd's digits in the LOCAL zone,
// which is right for a zone-less input and wrong by the offset for any other.
// Guarding what goes in and reading what comes back are two ends of ONE
// assumption, and a bd that fixes this (see the block below) has to move both.
//
// What stays out: anything without a date shape in it. +6h, tomorrow, next
// monday and plain garbage are still passed through, because bd's grammar there
// is not fixed enough to copy and its refusal is the better message anyway.
func checkDateSpec(field, val string) error {
	// No date in it: relative or spoken, and that stays bd's business.
	if !isoDateShape.MatchString(val) {
		return nil
	}
	// One of the lossless forms? Through it goes, with no further opinion.
	for _, form := range absoluteForms {
		if _, err := time.Parse(form, val); err == nil {
			return nil
		}
	}
	// Right shape, impossible day — the likeliest date typo there is, and a
	// message of its own: here the spelling is fine and the calendar is not.
	if isoDateSpec.MatchString(val) {
		return fmt.Errorf("ungültiges datum %q für %s: diesen Tag gibt es nicht. "+
			"bd würde das nicht ablehnen, sondern stillschweigend als Uhrzeit heute lesen "+
			"(2026-02-30 wird 02:30 heute). Erwarte JJJJ-MM-TT oder relativ: +6h, +1d, +2w, tomorrow, next monday",
			val, field)
	}
	// Otherwise: a date written the one way bd does not read as a date.
	return fmt.Errorf("ungültiges datum %q für %s: diese Form liest bd nicht als Datum. "+
		"bd lehnt sie nicht ab, sondern liest die Ziffern still als Uhrzeit heute "+
		"(\"2026-01-15 23:00\" wird 01:15 heute) oder verwirft die Zonenangabe. "+
		"Erwarte ohne Zone: JJJJ-MM-TT, JJJJ-MM-TThh:mm:ss oder \"JJJJ-MM-TT hh:mm:ss\" "+
		"— oder relativ: +6h, +1d, +2w, tomorrow, next monday",
		val, field)
}

// ---------------------------------------------------------- dates coming back
//
// bd does NOT use one time base for its timestamps, and the difference decides
// how each field has to be read. Measured against a throwaway database with bd
// 1.2.2 on 2026-09-13, wall clock 22:30 CEST = 20:30 UTC, every write repeated
// under TZ=Europe/Berlin, TZ=UTC and TZ=Asia/Tokyo:
//
//	field                     TZ=Berlin(+2)           TZ=UTC(+0)            TZ=Tokyo(+9)
//	created_at                20:30:41Z               20:30:46Z             20:30:50Z
//	updated_at                20:30:45Z               20:30:49Z             20:30:54Z
//	closed_at                 20:30:45Z               20:30:50Z             20:30:55Z
//	defer_until  (--defer +2w)  2026-09-27T22:30:41Z  2026-09-27T20:30:46Z  2026-09-28T05:30:51Z
//	due_at       (--due +6h)    2026-09-14T04:30:43Z  2026-09-14T02:30:48Z  2026-09-14T11:30:52Z
//	due_at       (--due 2026-01-15) 2026-01-15T00:00:00Z  (same in all three)
//
// The first three are identical in all three zones: they are genuine UTC and
// must be read at face value. The last three track the caller's wall clock
// exactly, so they are LOCAL time wearing a "Z" that lies. The same holds for
// every input form bd accepts — +2w, +6h, tomorrow, next monday, 2026-01-15,
// 2026-01-15T23:00:00, and even 2026-01-15T23:00:00Z with an explicit Z, which
// is echoed back unchanged instead of being converted.
//
// Why this matters enough to compensate rather than live with: reading the "Z"
// at face value shifts a date by the UTC offset, so a deadline at 23:00 local
// renders on the FOLLOWING day — and grouping by calendar day is exactly what
// the Today and Upcoming views do. A one-hour error would be cosmetic; this one
// changes which bucket a task falls into.
//
// The deliberate decisions here:
//
//   - The interpretation lives at the boundary, not at each render site. bd's
//     answer is turned into a correct time.Time once, on the way in, so no
//     later code can get it wrong. There is no migration cost: nothing reads
//     defer_until or due_at yet, which is why this is settled before the views
//     that will (the beads this one blocks).
//
//   - It lives HERE rather than in internal/collectors because this package
//     already owns bd's date grammar in the other direction — checkDateSpec
//     above guards what goes in, these guard what comes back, and a split would
//     mean two files having to agree about what bd does with dates.
//
//   - It is per field, never blanket. Shifting created_at/updated_at/closed_at
//     too would BREAK values that are already right, and the existing history
//     and sorting in the reports view read exactly those three. That code is
//     correct as it stands and must not be "fixed".
//
// Two known gaps, both out of reach from here. bd's `list --json` reports the
// created_at of a DEPENDENCY EDGE in local wall clock as well (22:33:16Z next
// to the issue's correct 20:33:17Z); nothing here reads that field, and
// issueDependencyJSON in internal/collectors has no place to put it.
//
// THE UPGRADE HAZARD IS REAL, NOT HYPOTHETICAL. The root cause is upstream in
// bd's internal/timeparsing/parser.go: the relative and absolute branches
// return the parsed time without .UTC(), while created_at/updated_at are
// normalised with time.Now().UTC() in issueops.PrepareIssueForInsert — same
// INSERT, different pre-treatment, which is exactly the split measured above.
// It is filed upstream as issue #5233 and FIXED by PR #5295, merged into main
// on 2026-08-07: every `return t, nil` there is now `return t.UTC(), nil`
// (verified in the source cache). That fix is NOT in the installed binary —
// bd 1.2.2, commit 6c124203e, predates the merge despite the later release
// date, which is why the measurement above still shows the defect.
//
// So a bd upgrade will flip this silently and turn the compensation into the
// very two-hour bug it removes. The two ways bd could change are not equally
// safe: if it ever writes a real offset ("…T23:00:00+01:00") wallclockZ below
// refuses it outright — measured — and the date goes MISSING rather than
// silently wrong, which is the failure worth having. But the fix that actually
// landed keeps the Z and starts meaning it, and nothing here can tell the
// difference.
//
// WHY THERE IS NO VERSION GATE AND NO STARTUP PROBE (gkp-62g). Both were
// considered and both were measured, not guessed:
//
//   - The VERSION cannot carry the answer. Read straight out of the published
//     sources: v1.2.1 returns t.UTC() — fixed — and v1.2.2, the newer release
//     and the one installed here, returns t. The changelog says why: 1.2.2 is a
//     recovery release "cut from the v1.1.2 tree … not from main", after v1.2.1
//     went out untested. A "stop compensating from version X" threshold is
//     therefore not merely risky, it is WRONG for a pair of releases that
//     already exist. And the fix appears in no changelog entry at all, so there
//     is no X to name. `bd --version` costs 83 ms per call on top (npm wrapper).
//
//   - A PROBE measures what the version only assumes, but its write has to land
//     somewhere. In the user's own database it is a probe bead per repo per
//     start, with its Dolt commit and its line in interactions.jsonl — in a
//     tool whose whole job is to REPORT on those repos. In a throwaway database
//     it is `bd init` at 1.6 s plus three more calls on every start. Caching
//     the answer brings the version back as the cache key, and the path then
//     runs once per bd build, in production, where nobody ever looks at it.
//
//   - REMOVING the compensation and demanding a fixed bd has no target to
//     point at: GitHub's latest release IS v1.2.2 (2026-08-15), the broken one,
//     and go.mod retracts v1.2.1. Today the fix lives only on main, so every bd
//     a user can install still needs this code.
//
// The compensation therefore stays, and the flip is made loud where it is free:
// TestInstalledBdStillWritesWallclockZ drives the installed bd against a
// throwaway database and fails with instructions the moment it starts writing
// real UTC. `just deploy` runs the gate, the gate runs `go test ./...`, so the
// service cannot be redeployed past it. What that does NOT cover, said plainly:
// a bd upgraded but never followed by a build keeps serving dates two hours off
// until the next gate run.

// wallclockZ is bd's format for defer_until and due_at: RFC3339 shaped, but the
// trailing Z is decoration, not a zone. Parsing with it yields the wall clock
// digits as written, which is the whole point.
const wallclockZ = "2006-01-02T15:04:05Z"

// ParseUserDate reads a date the USER asked bd for — defer_until or due_at —
// and returns the instant actually meant, in the machine's zone. See the block
// above: the digits bd stored are local wall clock, so they are re-anchored
// instead of being converted from UTC.
//
// ok is false for absent, malformed and zero values alike; a date that is not
// there and a date that cannot be understood are the same thing to a caller.
func ParseUserDate(v string) (time.Time, bool) { return ParseUserDateIn(v, time.Local) }

// ParseUserDateIn is the same reading with the zone named out loud, and it
// exists for the callers' TESTS. The compensation is invisible under TZ=UTC —
// the offset is zero, so a wrong reading looks right — which means a test that
// means anything has to fix a zone with an offset. Without this, the only way
// to do that from another package was to assign to time.Local and put it back
// afterwards: a process-global that Go resolves once (so setting TZ later does
// not move it) and that two parallel tests cannot share. internal/taskops had
// to do exactly that.
//
// The convenient spelling above stays the default for production code, where
// the machine's zone IS the answer: server and browser are the same machine
// here (see internal/appctx/reports_history.go).
func ParseUserDateIn(v string, loc *time.Location) (time.Time, bool) {
	return parseUserDate(v, loc)
}

func parseUserDate(v string, loc *time.Location) (time.Time, bool) {
	t, err := time.Parse(wallclockZ, strings.TrimSpace(v))
	if err != nil || t.IsZero() {
		return time.Time{}, false
	}
	// Same digits, re-anchored: 23:00 stays 23:00, on the day bd named.
	return time.Date(t.Year(), t.Month(), t.Day(),
		t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), loc), true
}

// ParseEventStamp reads a timestamp bd RECORDED itself — created_at, updated_at
// or closed_at. These are genuine UTC (measured above), so they are taken at
// face value. It exists so that the two kinds are told apart by name at the
// call site rather than by memory, and so the "do not compensate these" half of
// the finding is as reachable as the other half.
func ParseEventStamp(v string) (time.Time, bool) {
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(v))
	if err != nil || t.IsZero() {
		return time.Time{}, false
	}
	return t, true
}

// ---------------------------------------------------------------- create

// Priority accepts bd's priority as a JSON string ("2", "P2") or as a JSON
// number (2), and always hands bd a string.
//
// It exists because the plain `string` field rejected the most natural payload
// there is. The schema describes the field as "0-4 bzw. P0-P4", so a caller
// reading it writes `"priority": 2` — and got back
// `json: cannot unmarshal number into Go struct field NewIssue.issues.priority
// of type string`, a Go-internal message about a mistake the caller could not
// have avoided from the description alone. Found by making the first live
// beads_create call against this API.
//
// Refusing the number and fixing the wording would have been the other option.
// Accepting both is better: bd itself takes 0-4 and P0-P4 interchangeably, so
// there is no ambiguity to resolve — only a JSON type to widen.
type Priority string

func (p *Priority) UnmarshalJSON(b []byte) error {
	trimmed := strings.TrimSpace(string(b))
	if trimmed == "" || trimmed == "null" {
		*p = ""
		return nil
	}
	if trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*p = Priority(strings.TrimSpace(s))
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(b, &n); err != nil {
		return fmt.Errorf(`priority: erwarte 0-4, "0"-"4" oder "P0"-"P4", nicht %s`, trimmed)
	}
	*p = Priority(n.String())
	return nil
}

// Level converts the priority to the integer `bd create --graph` insists on.
// The two bd surfaces disagree about the type: the CLI flag takes "2" and "P2"
// interchangeably, while the graph plan rejects a string outright —
// "parsing graph plan: json: cannot unmarshal string into Go struct field
// GraphApplyNode.nodes.priority of type int". So the string form has to be
// translated here rather than passed through, and an unusable value has to fail
// before bd sees the plan, where the message would be about JSON rather than
// about the priority.
//
// present is false for an empty priority, which is what lets the plan omit the
// field and take bd's own default; a plain omitempty could not do that, since
// 0 is a legitimate priority (P0, critical).
func (p Priority) Level() (level int, present bool, err error) {
	s := strings.TrimSpace(string(p))
	if s == "" {
		return 0, false, nil
	}
	s = strings.TrimPrefix(strings.TrimPrefix(s, "P"), "p")
	n, convErr := strconv.Atoi(s)
	if convErr != nil || n < 0 || n > 4 {
		return 0, false, fmt.Errorf(`ungültige priority %q: erwarte 0-4 oder P0-P4`, string(p))
	}
	return n, true, nil
}

// NewIssue is one issue to create. Key is only meaningful within a batch: it
// names this issue for Edge and Parent references before bd has assigned real
// ids.
type NewIssue struct {
	Key         string   `json:"key,omitempty"`
	Title       string   `json:"title"`
	Type        string   `json:"type,omitempty"`
	Priority    Priority `json:"priority,omitempty"`
	Description string   `json:"description,omitempty"`
	Parent      string   `json:"parent,omitempty"`
	Labels      []string `json:"labels,omitempty"`
	Assignee    string   `json:"assignee,omitempty"`
	Acceptance  string   `json:"acceptance,omitempty"`
	Design      string   `json:"design,omitempty"`
	Notes       string   `json:"notes,omitempty"`
	// Due and Defer are plain strings here, unlike their counterparts in
	// Change: on a create there is nothing to clear, so "" can only mean "not
	// given". They are also not part of bd's graph plan — see extrasArgs.
	Due   string `json:"due,omitempty"`
	Defer string `json:"defer,omitempty"`
}

// Edge is a blocking dependency, named from the reader's side rather than
// bd's. bd offers two opposite spellings for the same edge (`bd dep add
// <blocked> <blocker>` vs `bd dep <blocker> --blocks <blocked>`), and its
// graph-plan form is inverted again relative to how its own label reads:
// {from_key: A, to_key: B, type: "blocks"} stores "A depends on B", i.e. B
// blocks A. Verified live against `bd blocked` after a real create — the first
// attempt at this feature wired all ten edges backwards. Naming the fields
// Blocked/Blocker removes the ambiguity from the API surface entirely.
type Edge struct {
	Blocked string `json:"blocked"`
	Blocker string `json:"blocker"`
}

// graphPlan mirrors the (undocumented) `bd create --graph` schema, established
// by probing bd 1.2.2: it warns about unknown fields and names missing ones,
// so the accepted set below is measured, not guessed. Fields bd does NOT
// accept here — acceptance, design, notes, estimate — are silently dropped by
// that code path, which is why applyExtras exists.
type graphPlan struct {
	Nodes []graphNode `json:"nodes"`
	Edges []graphEdge `json:"edges"`
}

type graphNode struct {
	Key   string `json:"key"`
	Title string `json:"title"`
	Type  string `json:"type,omitempty"`
	// Priority is an int here, unlike the CLI flag: the graph plan refuses a
	// string. A pointer rather than omitempty, because 0 (P0) is a real value.
	Priority    *int     `json:"priority,omitempty"`
	Description string   `json:"description,omitempty"`
	Labels      []string `json:"labels,omitempty"`
	Assignee    string   `json:"assignee,omitempty"`
	ParentKey   string   `json:"parent_key,omitempty"`
	ParentID    string   `json:"parent_id,omitempty"`
}

type graphEdge struct {
	FromKey string `json:"from_key"`
	ToKey   string `json:"to_key"`
	Type    string `json:"type"`
}

// createdLine matches `bd create --graph`'s report of one created issue:
// "  key -> gkp-n77".
var createdLine = regexp.MustCompile(`^\s*(\S+)\s*->\s*(\S+)\s*$`)

// buildPlan turns the API's issues+edges into bd's graph-plan shape. Kept
// separate from Create so the edge orientation — the one thing here that is
// counter-intuitive and was already wired backwards once — is testable without
// touching a database.
func buildPlan(issues []NewIssue, edges []Edge) (graphPlan, error) {
	var plan graphPlan
	if len(issues) == 0 {
		return plan, errors.New("keine issues angegeben")
	}
	keys := map[string]bool{}
	for i := range issues {
		if strings.TrimSpace(issues[i].Title) == "" {
			return plan, fmt.Errorf("issue %d hat keinen title", i)
		}
		if issues[i].Key == "" {
			issues[i].Key = "n" + strconv.Itoa(i)
		}
		if keys[issues[i].Key] {
			return plan, fmt.Errorf("doppelter key %q", issues[i].Key)
		}
		keys[issues[i].Key] = true
	}
	for _, is := range issues {
		level, hasPrio, err := is.Priority.Level()
		if err != nil {
			return plan, fmt.Errorf("issue %q: %w", is.Key, err)
		}
		// The dates are checked here, before the first subprocess, although
		// they are applied after the create: a refusal must arrive while
		// nothing exists yet, not once the issues are already in the database
		// and only the follow-up update fails.
		if err := checkDateSpec("due", is.Due); err != nil {
			return plan, fmt.Errorf("issue %q: %w", is.Key, err)
		}
		if err := checkDateSpec("defer", is.Defer); err != nil {
			return plan, fmt.Errorf("issue %q: %w", is.Key, err)
		}
		n := graphNode{
			Key: is.Key, Title: is.Title, Type: is.Type,
			Description: is.Description, Labels: is.Labels, Assignee: is.Assignee,
		}
		// A parent inside this batch has no id yet, so it must go through
		// parent_key; anything else must already exist.
		if hasPrio {
			n.Priority = &level
		}
		if is.Parent != "" {
			switch {
			case keys[is.Parent]:
				n.ParentKey = is.Parent
			case ValidID(is.Parent):
				n.ParentID = is.Parent
			default:
				return plan, fmt.Errorf("ungültiger parent %q", is.Parent)
			}
		}
		plan.Nodes = append(plan.Nodes, n)
	}
	for i, e := range edges {
		if e.Blocked == "" || e.Blocker == "" {
			return plan, fmt.Errorf("kante %d braucht blocked und blocker", i)
		}
		if e.Blocked == e.Blocker {
			return plan, fmt.Errorf("kante %d: %q kann nicht von sich selbst abhängen", i, e.Blocked)
		}
		for _, ref := range []string{e.Blocked, e.Blocker} {
			if !keys[ref] && !ValidID(ref) {
				return plan, fmt.Errorf("kante %d verweist auf unbekanntes %q", i, ref)
			}
		}
		// See Edge's doc comment: from_key is the DEPENDENT side, to_key the
		// blocker. Reading the label "blocks" left-to-right gives the opposite
		// and is wrong.
		plan.Edges = append(plan.Edges, graphEdge{FromKey: e.Blocked, ToKey: e.Blocker, Type: "blocks"})
	}
	return plan, nil
}

// CreateResult reports what a batch produced. Keys maps each input Key to the
// id bd assigned, so a caller can wire follow-up work without a second query.
type CreateResult struct {
	Keys   map[string]string `json:"keys"`
	IDs    []string          `json:"ids"`
	Calls  []Result          `json:"calls"`
	DryRun bool              `json:"dry_run,omitempty"`
}

// Create adds issues and, optionally, the blocking edges between them in a
// single `bd create --graph` call. Edge endpoints may be either a Key from
// this batch or an existing bead id.
//
// Fields the graph path drops (acceptance/design/notes/due/defer) are applied
// afterwards with one `bd update` per affected issue — only for issues that
// actually set them, so the common skeleton case stays at exactly one
// subprocess.
func Create(cwd string, issues []NewIssue, edges []Edge, dryRun bool) (CreateResult, error) {
	out := CreateResult{Keys: map[string]string{}, DryRun: dryRun}
	plan, err := buildPlan(issues, edges)
	if err != nil {
		return out, err
	}
	// buildPlan fills in any missing Key; applyExtras below looks issues up by
	// Key, so it has to see the same values the plan was built from.
	for i := range issues {
		issues[i].Key = plan.Nodes[i].Key
	}

	path, cleanup, err := writeTempJSON(plan)
	if err != nil {
		return out, err
	}
	defer cleanup()

	args := []string{"create", "--graph", path}
	if dryRun {
		args = append(args, "--dry-run")
	}
	res := Run(cwd, DefaultTimeoutMs, args...)
	out.Calls = append(out.Calls, res)
	if res.Failed {
		return out, errors.New(res.Error())
	}
	if dryRun {
		return out, nil
	}
	for _, line := range strings.Split(res.Stdout, "\n") {
		m := createdLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		out.Keys[m[1]] = m[2]
		out.IDs = append(out.IDs, m[2])
	}
	if len(out.IDs) == 0 {
		return out, fmt.Errorf("bd meldete keine angelegten ids; Ausgabe war: %s", res.Stdout)
	}

	extras, err := applyExtras(cwd, issues, out.Keys)
	out.Calls = append(out.Calls, extras...)
	return out, err
}

// extrasArgs builds the `bd update` flags for the fields `bd create --graph`
// silently drops. nil means the issue is complete after the create and costs no
// second subprocess. Split out from applyExtras so the mapping is testable
// without a database, the same reason updateArgs exists.
func extrasArgs(is NewIssue) []string {
	var args []string
	add := func(flag, val string) {
		if val != "" {
			args = append(args, flag, val)
		}
	}
	add("--acceptance", is.Acceptance)
	add("--design", is.Design)
	add("--notes", is.Notes)
	// due/defer are dropped by the graph plan too — probed with a dry run, bd
	// answers `warning: graph plan node["probe1"] has unknown field(s):
	// [defer due] (silently dropped — see 'bd create --graph' schema)` and
	// creates the issue anyway, exit 0. An empty value means "not given" here:
	// on a create there is nothing to clear.
	add("--due", is.Due)
	add("--defer", is.Defer)
	return args
}

// applyExtras sets the fields `bd create --graph` cannot carry. Returns the
// calls it made so a partial failure is visible rather than implied: the
// issues DO exist at this point, so this must not read as "create failed".
func applyExtras(cwd string, issues []NewIssue, byKey map[string]string) ([]Result, error) {
	var calls []Result
	var missed []string
	for _, is := range issues {
		flags := extrasArgs(is)
		if len(flags) == 0 {
			continue
		}
		id, ok := byKey[is.Key]
		if !ok {
			missed = append(missed, is.Key)
			continue
		}
		args := append([]string{"update"}, flags...)
		args = append(args, "--", id)
		res := Run(cwd, DefaultTimeoutMs, args...)
		calls = append(calls, res)
		if res.Failed {
			missed = append(missed, id)
		}
	}
	if len(missed) > 0 {
		return calls, fmt.Errorf("issues angelegt, aber acceptance/design/notes/due/defer fehlgeschlagen für: %s", strings.Join(missed, ", "))
	}
	return calls, nil
}

func writeTempJSON(v any) (path string, cleanup func(), err error) {
	blob, err := json.Marshal(v)
	if err != nil {
		return "", func() {}, err
	}
	f, err := os.CreateTemp("", "bdops-plan-*.json")
	if err != nil {
		return "", func() {}, err
	}
	name := f.Name()
	cleanup = func() { _ = os.Remove(name) }
	if _, err := f.Write(blob); err != nil {
		f.Close()
		cleanup()
		return "", func() {}, err
	}
	if err := f.Close(); err != nil {
		cleanup()
		return "", func() {}, err
	}
	return name, cleanup, nil
}

// ---------------------------------------------------------------- update

// Change is a set of field edits applied to every id in IDs. bd applies one
// flag set to all listed ids, so differing values need separate calls — the
// MCP layer takes an array of these rather than pretending otherwise.
type Change struct {
	IDs         []string `json:"ids"`
	Status      string   `json:"status,omitempty"`
	Claim       bool     `json:"claim,omitempty"`
	Priority    Priority `json:"priority,omitempty"`
	Title       string   `json:"title,omitempty"`
	Description string   `json:"description,omitempty"`
	Notes       string   `json:"notes,omitempty"`
	AppendNotes string   `json:"append_notes,omitempty"`
	Design      string   `json:"design,omitempty"`
	Acceptance  string   `json:"acceptance,omitempty"`
	Assignee    string   `json:"assignee,omitempty"`
	Type        string   `json:"type,omitempty"`
	// Parent is a pointer where every neighbour is a plain string, and that
	// asymmetry is the point: bd documents `--parent` as "use empty string to
	// remove parent", so here "" is a VALUE, not an absence. A string cannot
	// hold both meanings, and add()'s skip-empty rule — correct for every other
	// field, where "" only ever means "not given" — swallowed the clearing case:
	// the unparent that the MCP schema promises never reached bd at all.
	// nil means "not mentioned", a pointer to "" means "clear it".
	// Due and Defer below have the same shape, for the same reason.
	Parent *string `json:"parent,omitempty"`
	// Due and Defer are pointers for exactly the reason named above: bd spells
	// both flags "(empty to clear)", and clearing is the normal case — a
	// deadline that can be set but never removed is worse than none.
	Due          *string  `json:"due,omitempty"`
	Defer        *string  `json:"defer,omitempty"`
	AddLabels    []string `json:"add_labels,omitempty"`
	RemoveLabels []string `json:"remove_labels,omitempty"`
	SetLabels    []string `json:"set_labels,omitempty"`
}

// updateArgs builds the argv for one Change. Split out so the flag mapping is
// testable without running bd.
func updateArgs(c Change) ([]string, error) {
	if err := checkIDs(c.IDs); err != nil {
		return nil, err
	}
	// --parent carries a bead id and gets the same ValidID check as every other
	// id here, with one exception: "" is bd's documented way to REMOVE a parent
	// (see Change.Parent), so it has to pass. checkIDs is deliberately not
	// reused — it would reject that empty string, and its "ungültige bead-id"
	// reads as if one of the TARGET ids were broken, which in an update whose
	// ids are all fine sends the caller looking in the wrong place. Naming the
	// field and the alternative is the same standard DisallowUnknownFields sets
	// on the MCP side: a typo must come back as a sentence, not as whatever bd
	// makes of it.
	if c.Parent != nil && *c.Parent != "" && !ValidID(*c.Parent) {
		return nil, fmt.Errorf(`ungültiger parent %q: erwarte eine bead-id oder "" zum Lösen der Elternschaft`, *c.Parent)
	}
	// The dates are the opposite case to --parent: almost everything is passed
	// through to bd, see checkDateSpec. "" is the documented way to clear the
	// field and is never checked.
	for _, d := range []struct {
		field string
		val   *string
	}{{"due", c.Due}, {"defer", c.Defer}} {
		if d.val == nil || *d.val == "" {
			continue
		}
		if err := checkDateSpec(d.field, *d.val); err != nil {
			return nil, err
		}
	}
	args := []string{"update"}
	add := func(flag, val string) {
		if val != "" {
			args = append(args, flag, val)
		}
	}
	// addPtr is add()'s counterpart for the fields bd can clear with an empty
	// value: only a nil pointer counts as "not given", so an explicit "" still
	// produces the flag.
	addPtr := func(flag string, val *string) {
		if val != nil {
			args = append(args, flag, *val)
		}
	}
	add("--status", c.Status)
	add("--priority", string(c.Priority))
	add("--title", c.Title)
	add("--description", c.Description)
	add("--notes", c.Notes)
	add("--append-notes", c.AppendNotes)
	add("--design", c.Design)
	add("--acceptance", c.Acceptance)
	add("--assignee", c.Assignee)
	add("--type", c.Type)
	addPtr("--parent", c.Parent)
	addPtr("--due", c.Due)
	addPtr("--defer", c.Defer)
	if len(c.AddLabels) > 0 {
		args = append(args, "--add-label", strings.Join(c.AddLabels, ","))
	}
	if len(c.RemoveLabels) > 0 {
		args = append(args, "--remove-label", strings.Join(c.RemoveLabels, ","))
	}
	if len(c.SetLabels) > 0 {
		args = append(args, "--set-labels", strings.Join(c.SetLabels, ","))
	}
	if c.Claim {
		args = append(args, "--claim")
	}
	if len(args) == 1 {
		return nil, errors.New("nichts zu ändern angegeben")
	}
	args = append(args, "--")
	return append(args, c.IDs...), nil
}

// Update applies each Change in order, stopping at the first failure so a
// caller is never left guessing which half of a batch went through.
func Update(cwd string, changes []Change) ([]Result, error) {
	if len(changes) == 0 {
		return nil, errors.New("keine änderungen angegeben")
	}
	var calls []Result
	for i, c := range changes {
		args, err := updateArgs(c)
		if err != nil {
			return calls, fmt.Errorf("änderung %d: %w", i, err)
		}
		res := Run(cwd, DefaultTimeoutMs, args...)
		calls = append(calls, res)
		if res.Failed {
			return calls, errors.New(res.Error())
		}
	}
	return calls, nil
}

// ---------------------------------------------------------------- close

// closeArgs builds `bd close`. One --reason applies to all ids (bd maps
// multiple reasons positionally, which this deliberately does not expose:
// positional coupling between two arrays is an easy way to attach the wrong
// reason to the wrong bead).
func closeArgs(ids []string, reason string, force bool) ([]string, error) {
	if err := checkIDs(ids); err != nil {
		return nil, err
	}
	args := []string{"close"}
	if reason != "" {
		args = append(args, "--reason", reason)
	}
	if force {
		args = append(args, "--force")
	}
	args = append(args, "--")
	return append(args, ids...), nil
}

// Close closes ids. The evidence requirement does NOT live here — it belongs
// to the MCP layer, which owns the report store; this function is the
// mechanical half.
func Close(cwd string, ids []string, reason string, force bool) (Result, error) {
	args, err := closeArgs(ids, reason, force)
	if err != nil {
		return Result{}, err
	}
	res := Run(cwd, DefaultTimeoutMs, args...)
	if res.Failed {
		return res, errors.New(res.Error())
	}
	return res, nil
}

// ---------------------------------------------------------------- deps

// Dep adds or removes blocking edges. op is "add" or "remove".
func Dep(cwd, op string, edges []Edge) ([]Result, error) {
	if op != "add" && op != "remove" {
		return nil, fmt.Errorf("op muss add oder remove sein, war %q", op)
	}
	if len(edges) == 0 {
		return nil, errors.New("keine kanten angegeben")
	}
	var calls []Result
	for i, e := range edges {
		if !ValidID(e.Blocked) || !ValidID(e.Blocker) {
			return calls, fmt.Errorf("kante %d: ungültige id (blocked=%q blocker=%q)", i, e.Blocked, e.Blocker)
		}
		if e.Blocked == e.Blocker {
			return calls, fmt.Errorf("kante %d: %q kann nicht von sich selbst abhängen", i, e.Blocked)
		}
		// `bd dep add <issue> <depends-on>`: the dependent side comes first.
		res := Run(cwd, DefaultTimeoutMs, "dep", op, "--", e.Blocked, e.Blocker)
		calls = append(calls, res)
		if res.Failed {
			return calls, errors.New(res.Error())
		}
	}
	return calls, nil
}

// ---------------------------------------------------------------- query

// Query is a read-only lookup. Args are built from a fixed whitelist in the
// MCP layer; this only runs them and never mutates.
func Query(cwd string, args []string) (Result, error) {
	if len(args) == 0 {
		return Result{}, errors.New("keine abfrage angegeben")
	}
	// --readonly makes the read-only intent enforced by bd rather than merely
	// promised by this layer.
	res := Run(cwd, DefaultTimeoutMs, append([]string{"--readonly"}, args...)...)
	if res.Failed {
		return res, errors.New(res.Error())
	}
	return res, nil
}
