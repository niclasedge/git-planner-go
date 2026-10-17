package bdops

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestBuildPlanEdgeOrientation is the regression test for the mistake this
// package's Edge type exists to prevent. bd's graph plan stores
// {from_key: A, to_key: B, type: "blocks"} as "A depends on B" — the reverse
// of how the label reads. Wiring it the intuitive way inverted a real
// ten-edge graph: the foundation task came out blocked by the five tools that
// were supposed to depend on IT.
func TestBuildPlanEdgeOrientation(t *testing.T) {
	plan, err := buildPlan(
		[]NewIssue{{Key: "base", Title: "Fundament"}, {Key: "tool", Title: "Werkzeug"}},
		[]Edge{{Blocked: "tool", Blocker: "base"}},
	)
	if err != nil {
		t.Fatalf("buildPlan: %v", err)
	}
	if len(plan.Edges) != 1 {
		t.Fatalf("got %d edges, want 1", len(plan.Edges))
	}
	e := plan.Edges[0]
	if e.FromKey != "tool" {
		t.Errorf("from_key = %q, want the BLOCKED side %q", e.FromKey, "tool")
	}
	if e.ToKey != "base" {
		t.Errorf("to_key = %q, want the BLOCKER side %q", e.ToKey, "base")
	}
	if e.Type != "blocks" {
		t.Errorf("type = %q, want %q", e.Type, "blocks")
	}
}

// TestBuildPlanParentKeyVsParentID: a parent named inside the same batch has
// no id yet and must use parent_key; an existing bead must use parent_id.
// Getting this backwards makes bd drop the link silently.
func TestBuildPlanParentKeyVsParentID(t *testing.T) {
	plan, err := buildPlan([]NewIssue{
		{Key: "epic", Title: "Karte"},
		{Key: "child", Title: "Kind", Parent: "epic"},
		{Key: "other", Title: "Anderes", Parent: "gkp-ar7"},
	}, nil)
	if err != nil {
		t.Fatalf("buildPlan: %v", err)
	}
	if got := plan.Nodes[1]; got.ParentKey != "epic" || got.ParentID != "" {
		t.Errorf("in-batch parent: parent_key=%q parent_id=%q, want key only", got.ParentKey, got.ParentID)
	}
	if got := plan.Nodes[2]; got.ParentID != "gkp-ar7" || got.ParentKey != "" {
		t.Errorf("existing parent: parent_key=%q parent_id=%q, want id only", got.ParentKey, got.ParentID)
	}
}

// TestBuildPlanOmitsUnsupportedFields pins the measured schema boundary:
// acceptance/design/notes/due/defer are NOT part of the graph node, and bd
// drops unknown fields with only a warning. Serialising them anyway would look
// like it worked while losing the caller's data — applyExtras carries them
// instead.
func TestBuildPlanOmitsUnsupportedFields(t *testing.T) {
	plan, err := buildPlan([]NewIssue{{
		Key: "a", Title: "T", Acceptance: "ac", Design: "de", Notes: "no",
		Due: "2026-01-15", Defer: "+2w",
	}}, nil)
	if err != nil {
		t.Fatalf("buildPlan: %v", err)
	}
	blob, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{
		"acceptance", "design", "notes", "\"ac\"", "\"de\"", "\"no\"",
		"due", "defer", "2026-01-15", "+2w",
	} {
		if strings.Contains(string(blob), forbidden) {
			t.Errorf("plan JSON must not carry %s: %s", forbidden, blob)
		}
	}
}

func TestBuildPlanRejects(t *testing.T) {
	cases := []struct {
		name   string
		issues []NewIssue
		edges  []Edge
		want   string
	}{
		{"keine issues", nil, nil, "keine issues"},
		{"titel fehlt", []NewIssue{{Key: "a"}}, nil, "keinen title"},
		{"doppelter key", []NewIssue{{Key: "a", Title: "x"}, {Key: "a", Title: "y"}}, nil, "doppelter key"},
		{"parent existiert nicht als id", []NewIssue{{Key: "a", Title: "x", Parent: "-nope"}}, nil, "ungültiger parent"},
		{"kante halb leer", []NewIssue{{Key: "a", Title: "x"}}, []Edge{{Blocked: "a"}}, "braucht blocked und blocker"},
		{"kante auf unbekannt", []NewIssue{{Key: "a", Title: "x"}}, []Edge{{Blocked: "a", Blocker: "--x"}}, "unbekanntes"},
		{"selbstbezug", []NewIssue{{Key: "a", Title: "x"}}, []Edge{{Blocked: "a", Blocker: "a"}}, "von sich selbst"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := buildPlan(tc.issues, tc.edges)
			if err == nil {
				t.Fatalf("expected an error containing %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want it to contain %q", err, tc.want)
			}
		})
	}
}

// TestBuildPlanFillsMissingKeys: keys are optional in the API, but the plan
// needs them (bd rejects "node 0 has empty key") and applyExtras looks issues
// up by key afterwards.
func TestBuildPlanFillsMissingKeys(t *testing.T) {
	plan, err := buildPlan([]NewIssue{{Title: "a"}, {Title: "b"}}, nil)
	if err != nil {
		t.Fatalf("buildPlan: %v", err)
	}
	if plan.Nodes[0].Key == "" || plan.Nodes[1].Key == "" {
		t.Fatalf("keys not filled: %+v", plan.Nodes)
	}
	if plan.Nodes[0].Key == plan.Nodes[1].Key {
		t.Errorf("generated keys collide: %q", plan.Nodes[0].Key)
	}
}

// TestUpdateArgsRequiresIDs is the guard against bd's "last touched issue"
// default: an update with no id would silently mutate whichever bead some
// earlier, unrelated call happened to touch.
func TestUpdateArgsRequiresIDs(t *testing.T) {
	if _, err := updateArgs(Change{Status: "open"}); err == nil {
		t.Fatal("expected an error for an update without ids")
	} else if !strings.Contains(err.Error(), "zuletzt angefasste") {
		t.Errorf("error should explain WHY ids are required, got %q", err)
	}
}

func TestUpdateArgs(t *testing.T) {
	got, err := updateArgs(Change{
		IDs: []string{"gkp-a", "gkp-b"}, Status: "in_progress", Priority: "1",
		AddLabels: []string{"x", "y"}, Claim: true,
	})
	if err != nil {
		t.Fatalf("updateArgs: %v", err)
	}
	want := []string{
		"update", "--status", "in_progress", "--priority", "1",
		"--add-label", "x,y", "--claim", "--", "gkp-a", "gkp-b",
	}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Errorf("argv =\n  %v\nwant\n  %v", got, want)
	}
}

// TestUpdateArgsTerminatorBeforeIDs: "--" must precede the positionals, so a
// value that looks like a flag can never be parsed as one.
func TestUpdateArgsTerminatorBeforeIDs(t *testing.T) {
	got, err := updateArgs(Change{IDs: []string{"gt--xyz"}, Status: "open"})
	if err != nil {
		t.Fatalf("updateArgs: %v", err)
	}
	last := got[len(got)-2:]
	if last[0] != "--" || last[1] != "gt--xyz" {
		t.Errorf("expected [-- gt--xyz] at the end, got %v", got)
	}
}

// TestUpdateArgsClearsParent pins the one field where "" is a value rather than
// an absence. bd 1.2.2 documents `--parent` as "use empty string to remove
// parent", and the MCP schema promises exactly that ("leerer String löst die
// Elternschaft"). A plain string field cannot keep that promise: in it, "not
// given" and "deliberately emptied" look identical, and the skip-empty rule
// wins — so the flag never reached bd and the documented way to unparent a bead
// silently did nothing.
func TestUpdateArgsClearsParent(t *testing.T) {
	var c Change
	if err := json.Unmarshal([]byte(`{"ids":["gkp-a"],"parent":""}`), &c); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	got, err := updateArgs(c)
	if err != nil {
		t.Fatalf("updateArgs: %v", err)
	}
	want := []string{"update", "--parent", "", "--", "gkp-a"}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Errorf("argv =\n  %#v\nwant\n  %#v", got, want)
	}
}

// TestUpdateArgsOmitsUnsetParent is the other half, and the reason the fix is a
// pointer rather than "let --parent through when empty": a change that never
// mentions parent must not emit the flag at all, or every status update would
// unparent the bead as a side effect. null counts as not mentioned.
func TestUpdateArgsOmitsUnsetParent(t *testing.T) {
	for _, in := range []string{
		`{"ids":["gkp-a"],"status":"open"}`,
		`{"ids":["gkp-a"],"status":"open","parent":null}`,
	} {
		var c Change
		if err := json.Unmarshal([]byte(in), &c); err != nil {
			t.Fatalf("%s: unmarshal: %v", in, err)
		}
		got, err := updateArgs(c)
		if err != nil {
			t.Fatalf("%s: updateArgs: %v", in, err)
		}
		if slices.Contains(got, "--parent") {
			t.Errorf("%s: argv carries --parent although none was set: %#v", in, got)
		}
	}
}

// TestUpdateArgsParentOnlyIsAChange: clearing the parent is the entire edit in
// the natural case, so the "nothing to change" guard must not swallow it.
func TestUpdateArgsParentOnlyIsAChange(t *testing.T) {
	var c Change
	if err := json.Unmarshal([]byte(`{"ids":["gkp-a"],"parent":""}`), &c); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, err := updateArgs(c); err != nil {
		t.Errorf("an explicit empty parent is a change, got %v", err)
	}
}

// TestUpdateArgsChecksParentID pins both directions of the parent check at
// once, because they pull against each other: --parent carries a bead id and
// must be validated like every other id in this package, yet "" is bd's
// documented way to REMOVE a parent and has to stay allowed. A ValidID call
// dropped in without that exception would take the unparent case away again —
// the very thing TestUpdateArgsClearsParent exists to protect. Goes through
// json.Unmarshal rather than a struct literal so the decoder is covered too:
// only the decoder can tell "" from "not mentioned".
func TestUpdateArgsChecksParentID(t *testing.T) {
	for _, tc := range []struct {
		in      string
		wantErr bool
	}{
		{`{"ids":["gkp-a"],"parent":"-rf"}`, true},        // leading dash
		{`{"ids":["gkp-a"],"parent":"gkp a"}`, true},      // space
		{`{"ids":["gkp-a"],"parent":".hidden"}`, true},    // bad first char
		{`{"ids":["gkp-a"],"parent":""}`, false},          // clears the parent
		{`{"ids":["gkp-a"],"parent":"gkp-uc7.8"}`, false}, // nested sub-bead
		{`{"ids":["gkp-a"],"parent":"gt--xyz"}`, false},   // bd's own escape form
	} {
		var c Change
		if err := json.Unmarshal([]byte(tc.in), &c); err != nil {
			t.Fatalf("%s: unmarshal: %v", tc.in, err)
		}
		got, err := updateArgs(c)
		switch {
		case tc.wantErr && err == nil:
			t.Errorf("%s: expected a rejection, got argv %#v", tc.in, got)
		case !tc.wantErr && err != nil:
			t.Errorf("%s: expected acceptance, got %v", tc.in, err)
		case tc.wantErr:
			// The caller set one field wrong; the message has to say which,
			// and show the value it refused. "ungültige bead-id" alone reads
			// as if one of the target ids were broken.
			if !strings.Contains(err.Error(), "parent") {
				t.Errorf("%s: error should name the parent field, got %q", tc.in, err)
			}
			if c.Parent != nil && !strings.Contains(err.Error(), *c.Parent) {
				t.Errorf("%s: error should quote the offending value, got %q", tc.in, err)
			}
		}
	}
}

// TestUpdateArgsDates: --due and --defer are the two fields the dashboard is
// built on (defer_until = Things' "When", due_at = its "Deadline"), and until
// now they existed in bd but had no door through this API at all — measured: 0
// of 72 beads had either set. Goes through the decoder, like the parent tests,
// because for these fields the decoder is half the feature.
func TestUpdateArgsDates(t *testing.T) {
	var c Change
	if err := json.Unmarshal([]byte(`{"ids":["gkp-a"],"due":"2026-01-15","defer":"+2w"}`), &c); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	got, err := updateArgs(c)
	if err != nil {
		t.Fatalf("updateArgs: %v", err)
	}
	want := []string{"update", "--due", "2026-01-15", "--defer", "+2w", "--", "gkp-a"}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Errorf("argv =\n  %#v\nwant\n  %#v", got, want)
	}
}

// TestUpdateArgsClearsDates is the same shape as the parent case and exists for
// the same measured reason: bd 1.2.2 spells both flags "(empty to clear)", and
// clearing works — `bd update --due "" --defer ""` put a deferred probe bead
// back to status open with both fields gone. A plain string field cannot reach
// that behaviour, since add()'s skip-empty rule cannot tell "not mentioned"
// from "deliberately emptied"; a due date would be write-once.
func TestUpdateArgsClearsDates(t *testing.T) {
	var c Change
	if err := json.Unmarshal([]byte(`{"ids":["gkp-a"],"due":"","defer":""}`), &c); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	got, err := updateArgs(c)
	if err != nil {
		t.Fatalf("updateArgs: %v", err)
	}
	want := []string{"update", "--due", "", "--defer", "", "--", "gkp-a"}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Errorf("argv =\n  %#v\nwant\n  %#v", got, want)
	}
}

// TestUpdateArgsOmitsUnsetDates: the other half of the pointer. A change that
// never mentions the dates must not emit the flags, or every status update
// would wipe a bead's deadline as a side effect.
func TestUpdateArgsOmitsUnsetDates(t *testing.T) {
	for _, in := range []string{
		`{"ids":["gkp-a"],"status":"open"}`,
		`{"ids":["gkp-a"],"status":"open","due":null,"defer":null}`,
	} {
		var c Change
		if err := json.Unmarshal([]byte(in), &c); err != nil {
			t.Fatalf("%s: unmarshal: %v", in, err)
		}
		got, err := updateArgs(c)
		if err != nil {
			t.Fatalf("%s: updateArgs: %v", in, err)
		}
		for _, flag := range []string{"--due", "--defer"} {
			if slices.Contains(got, flag) {
				t.Errorf("%s: argv carries %s although none was set: %#v", in, flag, got)
			}
		}
	}
}

// TestUpdateArgsDateOnlyIsAChange: clearing a deadline is the whole edit in the
// natural case, so the "nothing to change" guard must not swallow it.
func TestUpdateArgsDateOnlyIsAChange(t *testing.T) {
	for _, in := range []string{
		`{"ids":["gkp-a"],"due":""}`,
		`{"ids":["gkp-a"],"defer":"tomorrow"}`,
	} {
		var c Change
		if err := json.Unmarshal([]byte(in), &c); err != nil {
			t.Fatalf("%s: unmarshal: %v", in, err)
		}
		if _, err := updateArgs(c); err != nil {
			t.Errorf("%s: an explicit date is a change, got %v", in, err)
		}
	}
}

// TestDateSpecPassesGrammarThroughToBD pins the design decision, which is the
// OPPOSITE of the one for --parent: bd's date grammar is rebuilt here as little
// as possible. Measured with bd 1.2.2, a value it cannot read comes back as
//
//	Error: invalid --due format "garbage". Examples: +6h, tomorrow, next monday, 2025-01-15
//
// on stderr with exit 1 — it names the flag, quotes the value and lists the
// forms, which is a better message than this layer could write and one that
// Result.Error() already carries through verbatim. Copying "+6h | +1d | +2w |
// tomorrow | next monday | <date>" into a regexp here would only mean keeping
// that copy in sync with bd forever, and every drift would show up as this API
// refusing something bd accepts.
//
// So "garbage" is deliberately ACCEPTED by this layer and left to bd.
func TestDateSpecPassesGrammarThroughToBD(t *testing.T) {
	for _, ok := range []string{"+6h", "+1d", "+2w", "tomorrow", "next monday", "2025-01-15", "garbage"} {
		var c Change
		if err := json.Unmarshal([]byte(`{"ids":["gkp-a"],"due":`+quote(ok)+`}`), &c); err != nil {
			t.Fatalf("%s: unmarshal: %v", ok, err)
		}
		if _, err := updateArgs(c); err != nil {
			t.Errorf("due %q should be left to bd, got %v", ok, err)
		}
	}
}

// TestDateSpecRejectsImpossibleCalendarDate is the one exception, and it is
// there because bd gets this case silently WRONG rather than loudly. Measured
// against a throwaway bd database:
//
//	--due 2026-02-30  ->  exit 0, due_at = 2026-09-13T02:30:00Z
//	--due 2025-13-45  ->  exit 0, due_at = 2026-09-13T13:45:00Z
//
// When the date parse fails, bd falls back to reading the trailing groups as a
// clock time, so a mistyped date does not fail — it becomes a timestamp TODAY,
// usually in the past, which looks entirely deliberate in every later view. bd
// reports success and nothing downstream can tell the difference, so this is
// exactly the case the caller cannot recover from on its own.
//
// The check stays narrow on purpose: only a value SHAPED like bd's absolute
// form is verified to be a real calendar date. That shape is fixed and will not
// drift; the relative forms are still bd's business.
func TestDateSpecRejectsImpossibleCalendarDate(t *testing.T) {
	for _, field := range []string{"due", "defer"} {
		for _, bad := range []string{"2026-02-30", "2025-13-45", "0000-13-45"} {
			var c Change
			in := `{"ids":["gkp-a"],"` + field + `":` + quote(bad) + `}`
			if err := json.Unmarshal([]byte(in), &c); err != nil {
				t.Fatalf("%s: unmarshal: %v", in, err)
			}
			got, err := updateArgs(c)
			if err == nil {
				t.Errorf("%s: expected a rejection, got argv %#v", in, got)
				continue
			}
			// Same standard as the parent check: name the field and quote the
			// value, or the caller looks in the wrong place.
			if !strings.Contains(err.Error(), field) || !strings.Contains(err.Error(), bad) {
				t.Errorf("%s: error should name field and value, got %q", in, err)
			}
		}
	}
}

// TestDateSpecRejectsAbsoluteFormsBdMisreads is the same defect one step wider,
// and it says where the line between "we check" and "bd checks" actually runs.
// bd parses an absolute date with a CLOSED set of layouts (date-only,
// RFC3339, "2006-01-02T15:04:05", "2006-01-02 15:04:05") and hands everything
// else to a natural-language parser, which reads the month and day digits as a
// clock time. So a value that merely LOOKS like a date but misses those layouts
// by one character does not fail — it becomes a timestamp today.
//
// Measured against a throwaway database, bd 1.2.2 (6c124203e), 2026-09-13 22:54
// CEST, every line exit 0:
//
//	--due "2026-01-15 23:00"            ->  2026-09-13T01:15:00Z   (month:day read as hh:mm)
//	--due "2026-01-15  23:00"           ->  2026-09-13T01:15:01Z
//	--due "2026-01-15 xyz"              ->  2026-09-13T01:15:00Z
//	--due " 2026-01-15 "                ->  2026-09-13T01:15:00Z   (padding alone is enough)
//	--due "2026-1-15"                   ->  2026-09-13T01:15:01Z   (one missing zero)
//	--due "2026-02-30 23:00"            ->  2026-09-13T02:30:01Z
//	--defer "2026-01-15 23:00"          ->  2026-09-13T01:15:01Z   (same on the other field)
//
// The three lines with a zone are a second, quieter loss: bd keeps the digits
// and THROWS THE ZONE AWAY, so two different instants land on the same value.
//
//	--due "2026-01-15T23:00:00Z"        ->  2026-01-15T23:00:00Z
//	--due "2026-01-15T23:00:00+01:00"   ->  2026-01-15T23:00:00Z
//	--due "2026-01-15T23:00:00-05:00"   ->  2026-01-15T23:00:00Z
//
// ParseUserDate below then re-anchors those digits in the LOCAL zone, which is
// right for a zone-less input and wrong by the offset for one that named a
// zone. That is why the accepted set is the three ZONE-LESS layouts and nothing
// else: they are exactly the inputs whose round trip through bd 1.2.2 is
// lossless, which is the precondition the compensation in ParseUserDate rests
// on. Checking input and reading it back are two ends of one assumption.
func TestDateSpecRejectsAbsoluteFormsBdMisreads(t *testing.T) {
	bad := []string{
		"2026-01-15 23:00", "2026-01-15  23:00", "2026-01-15 xyz",
		" 2026-01-15 ", "2026-1-15", "2026-02-30 23:00",
		"2026-01-15T23:00", "2026-01-15T23:00:00Z",
		"2026-01-15T23:00:00+01:00", "2026-01-15T23:00:00-05:00",
	}
	for _, field := range []string{"due", "defer"} {
		for _, v := range bad {
			var c Change
			in := `{"ids":["gkp-a"],"` + field + `":` + quote(v) + `}`
			if err := json.Unmarshal([]byte(in), &c); err != nil {
				t.Fatalf("%s: unmarshal: %v", in, err)
			}
			got, err := updateArgs(c)
			if err == nil {
				t.Errorf("%s: expected a rejection, got argv %#v", in, got)
				continue
			}
			if !strings.Contains(err.Error(), field) || !strings.Contains(err.Error(), strings.TrimSpace(v)) {
				t.Errorf("%s: error should name field and value, got %q", in, err)
			}
		}
	}
}

// TestDateSpecKeepsBdsLosslessForms is the other half: the check must not cost
// a single input bd gets RIGHT. The three zone-less absolute layouts survive
// the round trip verbatim (measured, same session as above) —
//
//	--due "2026-01-15"            ->  2026-01-15T00:00:00Z
//	--due "2026-01-15T23:00:00"   ->  2026-01-15T23:00:00Z
//	--due "2026-01-15 23:00:00"   ->  2026-01-15T23:00:00Z
//
// — and everything without a date shape in it stays bd's business, including
// the values bd itself refuses with a usable message.
func TestDateSpecKeepsBdsLosslessForms(t *testing.T) {
	good := []string{
		"2026-01-15", "2026-01-15T23:00:00", "2026-01-15 23:00:00",
		"+6h", "+1d", "+2w", "tomorrow", "next monday", "23:00", "garbage", "15.01.2026",
	}
	for _, v := range good {
		var c Change
		if err := json.Unmarshal([]byte(`{"ids":["gkp-a"],"due":`+quote(v)+`}`), &c); err != nil {
			t.Fatalf("%s: unmarshal: %v", v, err)
		}
		if _, err := updateArgs(c); err != nil {
			t.Errorf("due %q should be accepted, got %v", v, err)
		}
	}
}

func quote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// TestExtrasArgsCarriesDates: `bd create --graph` does not know due/defer
// either. Probed with a dry run, it answers
//
//	warning: graph plan node["probe1"] has unknown field(s): [defer due]
//	         (silently dropped — see 'bd create --graph' schema)
//
// and creates the issue anyway, exit 0 — the same silent drop that acceptance,
// design and notes get. So the dates have to ride on the follow-up update.
func TestExtrasArgsCarriesDates(t *testing.T) {
	var is NewIssue
	if err := json.Unmarshal([]byte(`{"title":"x","notes":"n","due":"2026-01-15","defer":"+2w"}`), &is); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	got := extrasArgs(is)
	want := []string{"--notes", "n", "--due", "2026-01-15", "--defer", "+2w"}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Errorf("argv =\n  %#v\nwant\n  %#v", got, want)
	}
	if extrasArgs(NewIssue{Title: "x"}) != nil {
		t.Error("an issue with no extra fields must not cost a second bd call")
	}
}

// TestBuildPlanRejectsImpossibleDate: a create must fail BEFORE any issue
// exists. buildPlan runs before the first subprocess, so that is where the
// calendar check belongs on this path.
func TestBuildPlanRejectsImpossibleDate(t *testing.T) {
	for _, is := range []NewIssue{
		{Key: "a", Title: "x", Due: "2026-02-30"},
		{Key: "a", Title: "x", Defer: "2026-02-30"},
	} {
		if _, err := buildPlan([]NewIssue{is}, nil); err == nil {
			t.Errorf("%+v: expected a rejection before anything is created", is)
		}
	}
}

func TestUpdateArgsRejectsEmptyChange(t *testing.T) {
	if _, err := updateArgs(Change{IDs: []string{"gkp-a"}}); err == nil {
		t.Fatal("expected an error when no field is set")
	}
}

func TestUpdateArgsRejectsBadID(t *testing.T) {
	if _, err := updateArgs(Change{IDs: []string{"-rf"}, Status: "open"}); err == nil {
		t.Fatal("expected an error for a leading-dash id")
	}
}

func TestCloseArgs(t *testing.T) {
	got, err := closeArgs([]string{"gkp-a", "gkp-b"}, "fertig", false)
	if err != nil {
		t.Fatalf("closeArgs: %v", err)
	}
	want := []string{"close", "--reason", "fertig", "--", "gkp-a", "gkp-b"}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Errorf("argv = %v, want %v", got, want)
	}
	if _, err := closeArgs(nil, "x", false); err == nil {
		t.Fatal("expected an error for a close without ids")
	}
}

func TestValidID(t *testing.T) {
	for _, ok := range []string{"gkp-ar7", "gkp-ar7.1", "demo-axk.1.2", "gt--xyz", "a"} {
		if !ValidID(ok) {
			t.Errorf("ValidID(%q) = false, want true", ok)
		}
	}
	for _, bad := range []string{"", "-rf", "--force", "a b", "a/b", "a;b", ".hidden"} {
		if ValidID(bad) {
			t.Errorf("ValidID(%q) = true, want false", bad)
		}
	}
}

// TestRunReportsFailureDetail is the property that made this package necessary:
// shared.Run collapses every failure into ("", false), which would make a close
// that never happened indistinguishable from one that did.
func TestRunReportsFailureDetail(t *testing.T) {
	if _, err := exec.LookPath("bd"); err != nil {
		t.Skip("bd nicht installiert")
	}
	res := Run(t.TempDir(), 20000, "show", "--", "gkp-gibt-es-nicht-xyz")
	if !res.Failed {
		t.Skip("bd akzeptierte die Abfrage unerwartet; nichts zu prüfen")
	}
	if res.Reason != "exit" {
		t.Errorf("Reason = %q, want %q", res.Reason, "exit")
	}
	if res.Error() == "" {
		t.Error("Error() must carry bd's own message, got empty")
	}
	if !strings.Contains(res.Error(), "bd show") {
		t.Errorf("Error() should name the invocation, got %q", res.Error())
	}
}

// TestRunMissingBinaryIsDistinguishable: "no bd on PATH" must not look like
// "bd refused", because the two need different messages to the user — the
// first was a real, hard-to-spot outage in this project (gkp-ar7.10).
func TestRunMissingBinaryIsDistinguishable(t *testing.T) {
	oldPath := os.Getenv("PATH")
	t.Cleanup(func() { os.Setenv("PATH", oldPath) })
	os.Setenv("PATH", "")

	res := Run("", 5000, "ready")
	if !res.Failed {
		t.Fatal("expected failure with PATH cleared")
	}
	if res.Reason != "not-found" {
		t.Errorf("Reason = %q, want %q", res.Reason, "not-found")
	}
}

// TestRunTimeout: a hanging bd must be reported as a timeout, not as a refusal.
//
// Deliberately against a FAKE bd, not the installed one. Die alte Fassung rief
// das echte bd mit 1 ms Frist und übersprang sich, wenn bd schneller war — sie
// prüfte damit nur das Etikett und nie, ob die Frist überhaupt bindet.
func TestRunTimeout(t *testing.T) {
	withFakeBd(t, "#!/bin/sh\nsleep 30\n")
	res := Run("", 150, "ready")
	if !res.Failed {
		t.Fatal("erwartet: Fehlschlag, das Fake-bd schläft 30 s")
	}
	if res.Reason != "timeout" {
		t.Errorf("Reason = %q, want %q", res.Reason, "timeout")
	}
}

// TestRunFristBindetAuchGegenEinenEnkel ist die Gegenprobe zu shared.WaitDelay,
// eine Ebene tiefer: exec.CommandContext tötet nur das Kind, das es gestartet
// hat. Ein Enkel, der die geerbte stdout-Pipe hält, lässt cmd.Run weiterwarten —
// die Frist bindet dann nicht.
//
// Das ist hier kein konstruierter Fall: `bd` ist auf dieser Maschine ein
// Node-Wrapper (@beads/bd/bin/bd.js), der die echte Binary als Enkel mit
// stdio:'inherit' startet. Gemessen ohne WaitDelay 30,0 s, mit 2,2 s.
//
// Gegenprobe: cmd.WaitDelay in Run entfernen -> dieser Test läuft 30 s und
// schlägt an der Schranke unten fehl.
func TestRunFristBindetAuchGegenEinenEnkel(t *testing.T) {
	// sleep im Hintergrund = Enkel, der stdout erbt und den Kill des Kindes
	// überlebt; sleep im Vordergrund hält das Kind bis zur Frist am Leben.
	withFakeBd(t, "#!/bin/sh\nsleep 30 &\nsleep 30\n")

	start := time.Now()
	res := Run("", 150, "ready")
	elapsed := time.Since(start)

	if !res.Failed || res.Reason != "timeout" {
		t.Fatalf("erwartet: timeout, bekommen Failed=%v Reason=%q", res.Failed, res.Reason)
	}
	// Frist 150 ms + WaitDelay 2 s. Ohne WaitDelay wären es die vollen 30 s des
	// Enkels; die Schranke sitzt weit genug von beiden Werten entfernt, dass
	// sie keine langsame Maschine bestraft und den Defekt trotzdem fängt.
	if grenze := 10 * time.Second; elapsed > grenze {
		t.Errorf("Run brauchte %v, erlaubt sind %v — die Frist bindet nicht, "+
			"der Enkel hält die stdout-Pipe (fehlt cmd.WaitDelay?)", elapsed, grenze)
	}
}

// withFakeBd stellt ein ausführbares `bd` mit dem gegebenen Skriptinhalt an den
// Anfang von PATH und nimmt das nach dem Test zurück.
func withFakeBd(t *testing.T, script string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bd"), []byte(script), 0o755); err != nil {
		t.Fatalf("Fake-bd schreiben: %v", err)
	}
	alt := os.Getenv("PATH")
	t.Cleanup(func() { os.Setenv("PATH", alt) })
	// Voranstellen, nicht ersetzen: das Skript selbst braucht PATH, um `sleep`
	// zu finden. Mit PATH=dir bricht es sofort mit 127 ab und "misst" 0 ms.
	os.Setenv("PATH", dir+string(os.PathListSeparator)+alt)
}

func TestDepRejectsBadInput(t *testing.T) {
	if _, err := Dep("", "sideways", []Edge{{Blocked: "a", Blocker: "b"}}); err == nil {
		t.Error("expected an error for an unknown op")
	}
	if _, err := Dep("", "add", nil); err == nil {
		t.Error("expected an error for no edges")
	}
	if _, err := Dep("", "add", []Edge{{Blocked: "-x", Blocker: "b"}}); err == nil {
		t.Error("expected an error for an invalid id")
	}
	if _, err := Dep("", "add", []Edge{{Blocked: "a", Blocker: "a"}}); err == nil {
		t.Error("expected an error for a self-dependency")
	}
}

func TestCreatedLineParsing(t *testing.T) {
	out := "Created 7 issues\n  bdops -> gkp-n77\n  close -> gkp-077\nsonstiger Text\n"
	got := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		if m := createdLine.FindStringSubmatch(line); m != nil {
			got[m[1]] = m[2]
		}
	}
	if len(got) != 2 || got["bdops"] != "gkp-n77" || got["close"] != "gkp-077" {
		t.Errorf("parsed %v, want the two key->id pairs only", got)
	}
}

// TestPriorityAcceptsNumberAndString: the schema documents priority as "0-4",
// so the natural payload is a JSON number. It used to be rejected with a
// Go-internal unmarshal message (found on the first live beads_create call), so
// both forms are pinned here — and the value handed to bd stays a string.
func TestPriorityAcceptsNumberAndString(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{`{"priority": 2}`, "2"},
		{`{"priority": 0}`, "0"},
		{`{"priority": "3"}`, "3"},
		{`{"priority": "P1"}`, "P1"},
		{`{"priority": " P4 "}`, "P4"},
		{`{"priority": null}`, ""},
		{`{}`, ""},
	}
	for _, tc := range cases {
		var is NewIssue
		if err := json.Unmarshal([]byte(tc.in), &is); err != nil {
			t.Errorf("%s: %v", tc.in, err)
			continue
		}
		if string(is.Priority) != tc.want {
			t.Errorf("%s: priority = %q, want %q", tc.in, is.Priority, tc.want)
		}
	}

	// The widening must not swallow nonsense: a value bd could not interpret
	// still has to fail at the parse boundary rather than reach the argv.
	var is NewIssue
	if err := json.Unmarshal([]byte(`{"priority": {"n": 2}}`), &is); err == nil {
		t.Error("an object priority should be rejected")
	}
	if err := json.Unmarshal([]byte(`{"priority": true}`), &is); err == nil {
		t.Error("a boolean priority should be rejected")
	}
}

// TestPriorityReachesArgvAsString covers the other half: whichever JSON form
// came in, updateArgs must emit a plain --priority value.
func TestPriorityReachesArgvAsString(t *testing.T) {
	var c Change
	if err := json.Unmarshal([]byte(`{"ids":["gkp-1"],"priority":1}`), &c); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	args, err := updateArgs(c)
	if err != nil {
		t.Fatalf("updateArgs: %v", err)
	}
	if got := strings.Join(args, " "); !strings.Contains(got, "--priority 1") {
		t.Errorf("argv = %v, want it to carry --priority 1", args)
	}
}

// TestPlanPriorityIsANumber pins the type mismatch between bd's two surfaces.
// The CLI flag takes "P2" as a string; `bd create --graph` refuses a string and
// wants an int — it failed with "parsing graph plan: json: cannot unmarshal
// string into Go struct field GraphApplyNode.nodes.priority of type int" on the
// first real call. So the plan JSON has to carry a bare number, and both input
// spellings have to arrive there identically.
func TestPlanPriorityIsANumber(t *testing.T) {
	plan, err := buildPlan([]NewIssue{
		{Key: "a", Title: "a", Priority: "P0"},
		{Key: "b", Title: "b", Priority: "2"},
		{Key: "c", Title: "c"},
	}, nil)
	if err != nil {
		t.Fatalf("buildPlan: %v", err)
	}
	blob, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(blob)
	// P0 must survive as 0 rather than being dropped: omitempty on a plain int
	// would silently turn the most urgent priority into bd's default.
	if !strings.Contains(got, `"priority":0`) {
		t.Errorf(`P0 lost: %s`, got)
	}
	if !strings.Contains(got, `"priority":2`) {
		t.Errorf(`"2" did not become a number: %s`, got)
	}
	if strings.Contains(got, `"priority":"`) {
		t.Errorf("a priority is still a string: %s", got)
	}
	// An unset priority must be absent entirely, so bd applies its own default.
	if n := strings.Count(got, `"priority"`); n != 2 {
		t.Errorf(`want exactly 2 priority fields, got %d: %s`, n, got)
	}
}

func TestPriorityLevelRejectsNonsense(t *testing.T) {
	for _, in := range []Priority{"5", "P9", "hoch", "-1", "P", "2.5"} {
		if _, _, err := in.Level(); err == nil {
			t.Errorf("priority %q should be rejected", in)
		}
	}
	// And a bad priority must stop buildPlan, not reach bd as broken JSON.
	if _, err := buildPlan([]NewIssue{{Key: "a", Title: "a", Priority: "hoch"}}, nil); err == nil {
		t.Error("buildPlan accepted an unusable priority")
	}
}

// TestParseUserDateKeepsCalendarDay is the regression test for a bd defect
// measured against a throwaway database with bd 1.2.2, on 2026-09-13 at 22:30
// CEST (= 20:30 UTC). bd writes defer_until and due_at as the LOCAL wallclock
// time, stamped with a "Z" that claims UTC:
//
//	--defer +2w          ->  2026-09-27T22:30:41Z   (true UTC: 20:30:41Z)
//	--due   +6h          ->  2026-09-14T04:30:43Z   (true UTC: 02:30:43Z)
//	--due   2026-01-15   ->  2026-01-15T00:00:00Z   (local midnight)
//
// Reading that "Z" at face value shifts every date by the UTC offset, and a
// deadline late in the evening then lands on the WRONG CALENDAR DAY — which is
// the one thing the Today and Upcoming views are built on. The case below is
// exactly that: 23:00 local in January (Europe/Berlin, CET = UTC+1). Read as
// UTC it becomes 00:00 on the 16th.
//
// The location is fixed rather than taken from the machine so the test means
// the same thing on a UTC build agent, where the offset is zero and the defect
// is invisible.
func TestParseUserDateKeepsCalendarDay(t *testing.T) {
	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Skipf("no tzdata for Europe/Berlin: %v", err)
	}
	// What bd wrote when the user asked for 23:00 on 2026-01-15.
	got, ok := parseUserDate("2026-01-15T23:00:00Z", berlin)
	if !ok {
		t.Fatal("parseUserDate rejected a value bd actually writes")
	}
	if y, m, d := got.Date(); y != 2026 || m != time.January || d != 15 {
		t.Errorf("calendar day = %04d-%02d-%02d, want 2026-01-15 — the UTC offset moved the deadline to another day", y, m, d)
	}
	if h, min := got.Hour(), got.Minute(); h != 23 || min != 0 {
		t.Errorf("clock = %02d:%02d, want 23:00 — bd's value is local wallclock, not UTC", h, min)
	}
}

// TestParseEventStampIsNotShifted is the other half of the measurement, and it
// guards against the obvious over-correction: created_at, updated_at and
// closed_at came back identical under TZ=Europe/Berlin, TZ=UTC and
// TZ=Asia/Tokyo, so they are real UTC and must be read at face value. Applying
// the defer_until/due_at compensation to them would break values that are
// already right — and the reports view's history and sorting read exactly these
// three fields.
func TestParseEventStampIsNotShifted(t *testing.T) {
	// Measured: bd wrote this for a create at 22:30:41 CEST.
	got, ok := ParseEventStamp("2026-09-13T20:30:41Z")
	if !ok {
		t.Fatal("ParseEventStamp rejected a value bd actually writes")
	}
	if want := time.Date(2026, 9, 13, 20, 30, 41, 0, time.UTC); !got.Equal(want) {
		t.Errorf("got %s, want %s — a recorded stamp must not be re-anchored", got, want)
	}
}

// TestParseUserDateCoversBdsDateForms runs the values actually observed coming
// back out of bd, so the table in the comment above is executable rather than
// folklore. Berlin is fixed for the same reason as in the test above.
func TestParseUserDateCoversBdsDateForms(t *testing.T) {
	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Skipf("no tzdata for Europe/Berlin: %v", err)
	}
	cases := []struct {
		spec, stored string
		want         time.Time
	}{
		// --defer +2w, run 2026-09-13 22:30:41 CEST
		{"+2w", "2026-09-27T22:30:41Z", time.Date(2026, 9, 27, 22, 30, 41, 0, berlin)},
		// --due +6h, run 2026-09-13 22:30:43 CEST
		{"+6h", "2026-09-14T04:30:43Z", time.Date(2026, 9, 14, 4, 30, 43, 0, berlin)},
		// --due 2026-01-15: bd stores local midnight, so midnight is what it means
		{"2026-01-15", "2026-01-15T00:00:00Z", time.Date(2026, 1, 15, 0, 0, 0, 0, berlin)},
		// an explicit Z on the way IN is echoed unchanged, not converted
		{"2026-01-15T23:00:00Z", "2026-01-15T23:00:00Z", time.Date(2026, 1, 15, 23, 0, 0, 0, berlin)},
	}
	for _, tc := range cases {
		got, ok := parseUserDate(tc.stored, berlin)
		if !ok {
			t.Errorf("%s: parseUserDate(%q) rejected", tc.spec, tc.stored)
			continue
		}
		if !got.Equal(tc.want) {
			t.Errorf("%s: parseUserDate(%q) = %s, want %s", tc.spec, tc.stored, got, tc.want)
		}
	}
}

// TestParseDatesRejectAbsentAndBroken: a bead with no date at all is the common
// case, not an error, and every caller has to be able to tell "no date" from
// "some date" without a second check. The zero stamp is in here because bd
// leaves cleared fields behind in that shape rather than omitting them.
func TestParseDatesRejectAbsentAndBroken(t *testing.T) {
	for _, bad := range []string{"", "   ", "null", "0001-01-01T00:00:00Z", "2026-01-15", "garbage"} {
		if _, ok := ParseUserDate(bad); ok {
			t.Errorf("ParseUserDate(%q) accepted", bad)
		}
		if _, ok := ParseEventStamp(bad); ok {
			t.Errorf("ParseEventStamp(%q) accepted", bad)
		}
	}
}

// TestParseUserDateRefusesRealOffset pins the safe half of the forward-
// compatibility story. wallclockZ accepts only bd 1.2.2's literal trailing Z;
// a value carrying a genuine offset is refused, so if a later bd starts writing
// one the date goes MISSING instead of being read as wall clock and landing two
// hours — possibly a day — off. A visible gap is recoverable, a plausible wrong
// date is not.
func TestParseUserDateRefusesRealOffset(t *testing.T) {
	for _, v := range []string{"2026-01-15T23:00:00+01:00", "2026-01-15T23:00:00-05:00", "2026-01-15T23:00:00"} {
		if got, ok := ParseUserDate(v); ok {
			t.Errorf("ParseUserDate(%q) = %s, want refusal: that shape is not what bd 1.2.2 writes", v, got)
		}
	}
}
