package mcp

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/niclasedge/git-planner-go/internal/bdops"
	"github.com/niclasedge/git-planner-go/internal/closeops"
	"github.com/niclasedge/git-planner-go/internal/collectors"
	"github.com/niclasedge/git-planner-go/internal/reports"
)

// TestToolsListCatalog pins the whole tool surface. It is the replacement for
// the old "exactly one tool" assertion: the point is not that there is one
// tool, but that the set is deliberate and every entry carries a description
// an agent can act on without further docs.
func TestToolsListCatalog(t *testing.T) {
	rec := doRPC(t, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	env := decodeEnvelope(t, rec)
	if env.Error != nil {
		t.Fatalf("unexpected error: %+v", env.Error)
	}
	var result struct {
		Tools []struct {
			Name        string         `json:"name"`
			Description string         `json:"description"`
			InputSchema map[string]any `json:"inputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(env.Result, &result); err != nil {
		t.Fatalf("result: %v", err)
	}

	want := map[string]bool{
		"report": true, "beads_query": true, "beads_create": true,
		"beads_update": true, "beads_close": true, "beads_dep": true,
	}
	got := map[string]bool{}
	for _, tl := range result.Tools {
		got[tl.Name] = true
		if tl.Description == "" {
			t.Errorf("tool %q has no description", tl.Name)
		}
		if tl.InputSchema == nil {
			t.Errorf("tool %q has no inputSchema", tl.Name)
			continue
		}
		props, ok := tl.InputSchema["properties"].(map[string]any)
		if !ok || len(props) == 0 {
			t.Errorf("tool %q has no schema properties", tl.Name)
			continue
		}
		// Every field an agent fills has to be self-explanatory: the schema IS
		// the documentation for these tools.
		for name, raw := range props {
			p, _ := raw.(map[string]any)
			if desc, _ := p["description"].(string); strings.TrimSpace(desc) == "" {
				t.Errorf("tool %q field %q has no description", tl.Name, name)
			}
		}
		// Every beads tool needs cwd — it decides which database is touched.
		if strings.HasPrefix(tl.Name, "beads_") {
			if _, ok := props["cwd"]; !ok {
				t.Errorf("tool %q must require cwd", tl.Name)
			}
		}
	}
	for name := range want {
		if !got[name] {
			t.Errorf("catalog is missing tool %q", name)
		}
	}
	for name := range got {
		if !want[name] {
			t.Errorf("catalog has unexpected tool %q — add it to this test on purpose", name)
		}
	}
}

func TestQueryArgs(t *testing.T) {
	n := func(i int) *int { return &i }
	cases := []struct {
		name string
		in   beadsQueryParams
		want []string
	}{
		{
			"list mit komma-status",
			beadsQueryParams{Op: "list", Status: []string{"open", "in_progress"}},
			// bd overwrites a repeated -s silently, so the comma form is the
			// only correct way to filter on more than one status.
			[]string{"list", "--status", "open,in_progress", "--json"},
		},
		{
			"list voll",
			beadsQueryParams{Op: "list", Type: "bug", Labels: []string{"a", "b"}, Parent: "gkp-ar7", All: true, Limit: n(0)},
			[]string{"list", "--type", "bug", "--label", "a,b", "--parent", "gkp-ar7", "--all", "--limit", "0", "--json"},
		},
		{
			"show mit kindern",
			beadsQueryParams{Op: "show", ID: "gkp-ar7", Children: true},
			// --id= rather than a positional: bd documents that form for ids
			// that look like flags.
			[]string{"show", "--id=gkp-ar7", "--children", "--json"},
		},
		{"ready", beadsQueryParams{Op: "ready", Parent: "gkp-ar7"}, []string{"ready", "--parent", "gkp-ar7", "--json"}},
		{"blocked", beadsQueryParams{Op: "blocked"}, []string{"blocked", "--json"}},
		{"stats", beadsQueryParams{Op: "stats"}, []string{"stats", "--json"}},
		{"dep_tree", beadsQueryParams{Op: "dep_tree", ID: "gkp-ar7"}, []string{"dep", "tree", "--", "gkp-ar7"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := queryArgs(tc.in)
			if err != nil {
				t.Fatalf("queryArgs: %v", err)
			}
			if strings.Join(got, "\x00") != strings.Join(tc.want, "\x00") {
				t.Errorf("argv =\n  %v\nwant\n  %v", got, tc.want)
			}
		})
	}
}

func TestQueryArgsRejects(t *testing.T) {
	cases := []struct {
		name string
		in   beadsQueryParams
		want string
	}{
		{"unbekannte op", beadsQueryParams{Op: "delete-everything"}, "unbekannte op"},
		{"show ohne id", beadsQueryParams{Op: "show"}, "gültige id"},
		{"show mit flag-id", beadsQueryParams{Op: "show", ID: "--force"}, "gültige id"},
		{"dep_tree ohne id", beadsQueryParams{Op: "dep_tree"}, "gültige id"},
		{"list mit flag-parent", beadsQueryParams{Op: "list", Parent: "-rf"}, "ungültige parent-id"},
		{"ready mit flag-parent", beadsQueryParams{Op: "ready", Parent: "-rf"}, "ungültige parent-id"},
		{"blocked mit flag-parent", beadsQueryParams{Op: "blocked", Parent: "-rf"}, "ungültige parent-id"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := queryArgs(tc.in)
			if err == nil {
				t.Fatalf("expected an error containing %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want it to contain %q", err, tc.want)
			}
		})
	}
}

// fakeBdShowChildren points PATH at a "bd" that answers exactly the
// `--readonly show --id=<mapID> --children --json` shape mapChildren sends,
// regardless of args — good enough for a test with exactly one bd call.
func fakeBdShowChildren(t *testing.T, antwort string) {
	t.Helper()
	binDir := t.TempDir()
	// "echo", nicht "cat": PATH unten enthaelt nur binDir, jedes externe
	// Programm im Skript waere unauffindbar und liesse den Test aus dem
	// falschen Grund scheitern (vgl. TestNotifyBeadRejectsFlagLikeBeadWithoutStartingAProcess).
	skript := "#!/bin/sh\necho '" + antwort + "'\n"
	if err := os.WriteFile(filepath.Join(binDir, "bd"), []byte(skript), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)
}

// TestQueryGapsSiehtNurEintraegeUnterDerEigenenKarte pins the map-scoped
// evidence check inside op=gaps against gkp-6l2's own investigation: this
// diagnostic exists BECAUSE it answers "hat DIESE Karte ihre eigene
// Buchfuehrung" and not "hat der Bead irgendwo im Store einen Beleg" — gkp-few
// (docs/spec-aufgaben-dashboard.md §7.1) states literally that
// "beads_query op=gaps map=gkp-4e1 haette [den Vorfall] gemeldet". A done
// entry correctly filed under a DIFFERENT map must therefore still surface as
// a gap under the map being checked; reports.LoadBead would search across
// every map and hide exactly this. Gegenprobe gefahren: mit
// reports.ListMaps+Filter statt LoadMap wird dieser Test rot.
func TestQueryGapsSiehtNurEintraegeUnterDerEigenenKarte(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CC_REPORTS_DIR", dir)
	cwd := t.TempDir()
	repo := collectors.ReportsRepoName(cwd)
	if err := os.MkdirAll(filepath.Join(dir, repo), 0o755); err != nil {
		t.Fatal(err)
	}
	// Der Beleg fuer gkp-a.1 liegt absichtlich unter einer ANDEREN Karte.
	zeile, err := json.Marshal(reports.Entry{
		ID: "e1", At: "2026-01-01T00:00:00Z", Map: "gkp-b", Bead: "gkp-a.1", Kind: "done", Title: "x",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, repo, "gkp-b.jsonl"), append(zeile, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	fakeBdShowChildren(t, `{"gkp-a":[{"id":"gkp-a.1","status":"closed"}]}`)

	out, err := queryGaps(cwd, "gkp-a")
	if err != nil {
		t.Fatalf("queryGaps: %v", err)
	}
	var got gapReport
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("Antwort ist kein JSON: %v\n%s", err, out)
	}
	if len(got.Gaps) != 1 || got.Gaps[0] != "gkp-a.1" {
		t.Errorf("Gaps = %v, erwartet [gkp-a.1] — der Beleg unter gkp-b darf hier nicht zaehlen", got.Gaps)
	}
}

// TestBeadsToolsRequireCwd: every beads tool touches a database chosen by cwd.
// A missing cwd must be refused before any subprocess starts, rather than
// falling back to the server's own working directory and mutating the wrong
// repo's tracker.
func TestBeadsToolsRequireCwd(t *testing.T) {
	if _, err := beadsQuery(beadsQueryParams{Op: "ready"}); err == nil {
		t.Error("beads_query without cwd should fail")
	}
	if _, err := beadsCreate(beadsCreateParams{Issues: []bdops.NewIssue{{Title: "x"}}}); err == nil {
		t.Error("beads_create without cwd should fail")
	}
	if _, err := beadsUpdate(beadsUpdateParams{Changes: []bdops.Change{{IDs: []string{"a"}, Status: "open"}}}); err == nil {
		t.Error("beads_update without cwd should fail")
	}
	if _, err := beadsDep(beadsDepParams{Op: "add", Edges: []bdops.Edge{{Blocked: "a", Blocker: "b"}}}); err == nil {
		t.Error("beads_dep without cwd should fail")
	}
	if _, err := beadsClose(nil, beadsCloseParams{Map: "m", Closes: []closeItem{{Bead: "a"}}}); err == nil {
		t.Error("beads_close without cwd should fail")
	}
}

// TestBeadsCloseRejectsBadInput covers the guards that run before anything is
// written or closed.
func TestBeadsCloseRejectsBadInput(t *testing.T) {
	cases := []struct {
		name string
		in   beadsCloseParams
		want string
	}{
		{"kein cwd", beadsCloseParams{Map: "m", Closes: []closeItem{{Bead: "a"}}}, "cwd und map"},
		{"keine map", beadsCloseParams{Cwd: "/tmp", Closes: []closeItem{{Bead: "a"}}}, "cwd und map"},
		{"nichts zu schliessen", beadsCloseParams{Cwd: "/tmp", Map: "m"}, "keine beads"},
		{"flag-artige id", beadsCloseParams{Cwd: "/tmp", Map: "m", Closes: []closeItem{{Bead: "--force"}}}, "ungültige bead-id"},
		{
			"report ohne titel",
			beadsCloseParams{Cwd: "/tmp", Map: "m", Closes: []closeItem{{Bead: "a", Report: &closeReport{Impact: "x"}}}},
			"braucht einen title",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := beadsClose(nil, tc.in)
			if err == nil {
				t.Fatalf("expected an error containing %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want it to contain %q", err, tc.want)
			}
		})
	}
}

// storeDoneEntry writes one done entry into an isolated report store and
// returns the cwd the close tool should be called with. Writing the JSONL
// directly (instead of going through reports.Append) keeps the test on the
// gate: no git repo, no image files on disk, nothing that could fail for an
// unrelated reason.
func storeDoneEntry(t *testing.T, mapID string, proof ...map[string]any) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("CC_REPORTS_DIR", dir)
	cwd := t.TempDir()
	repo := collectors.ReportsRepoName(cwd)
	if err := os.MkdirAll(filepath.Join(dir, repo), 0o755); err != nil {
		t.Fatal(err)
	}
	line, err := json.Marshal(reports.Entry{
		ID: "e1", At: "2026-09-04T10:00:00Z", Repo: repo, Map: mapID,
		Bead: mapID + ".1", Kind: "done", Title: "fertig", Proof: proof,
	})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, repo, mapID+".jsonl")
	if err := os.WriteFile(path, append(line, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	return cwd
}

// TestBeadsCloseNeedsVisualProof is the user's rule of 2026-09-04 — "kein bead
// schliessen können ohne dot oder png zur validierung" — pinned at the tool
// boundary. A {cmd,out} proof is written by the same instance that claims the
// work is done; only a picture or a rendered graph forces the claim through the
// actual state.
func TestBeadsCloseNeedsVisualProof(t *testing.T) {
	mitKartenattrappe(t, nil) // gkp-few: hier geht es um den Beleg, nicht um die Karte
	cwd := storeDoneEntry(t, "gkp-zz1", map[string]any{"cmd": "go test ./...", "out": "ok"})
	_, err := beadsClose(nil, beadsCloseParams{
		Cwd: cwd, Map: "gkp-zz1", Closes: []closeItem{{Bead: "gkp-zz1.1"}},
	})
	if err == nil {
		t.Fatal("a text-only done entry must not close a bead")
	}
	if !strings.Contains(err.Error(), "nichts zum Ansehen") {
		t.Fatalf("error = %q, want the visual-proof refusal", err)
	}
	// The two levels must stay distinguishable: this bead HAS a done entry, so
	// the message must not be the "no evidence at all" one, whose fix (write a
	// report) would be the wrong advice.
	if strings.Contains(err.Error(), "kein aktueller done-Beleg") {
		t.Errorf("wrong level reported: %q", err)
	}
}

// closeWithReport runs the real write path: the proof goes through
// reports.Append (which resolves it) and the gate then re-reads what was
// stored. storeDoneEntry deliberately bypasses Append and therefore cannot see
// anything Append decides — which is exactly how gkp-2ln's gap survived in a
// green test.
func closeWithReport(t *testing.T, mapID, cwd string, proof ...reports.ProofItem) error {
	t.Helper()
	t.Setenv("CC_REPORTS_DIR", t.TempDir())
	mitKartenattrappe(t, nil) // gkp-few: diese Tests prüfen das Beleg-Tor, nicht das Kartentor
	orig := notifyBeadFunc
	notifyBeadFunc = func(cwd, bead, url string) {}
	t.Cleanup(func() { notifyBeadFunc = orig })

	_, err := beadsClose(&http.Request{}, beadsCloseParams{
		Cwd: cwd, Map: mapID, Closes: []closeItem{{
			Bead:   mapID + ".1",
			Report: &closeReport{Title: "fertig", Proof: proof},
		}},
	})
	return err
}

// TestBeadsCloseAcceptsVisualProof is the counterpart: with a picture, the gate
// lets the call through to bd. bd then fails, because the temp cwd is not a
// beads repo — that failure is the proof the gate was passed.
//
// gkp-2ln: both proofs now name a file that EXISTS and travel the real write
// path. The previous version handed in {"file": "docs/plan.dot"} inside an
// empty temp dir and was green only because file proofs were never resolved —
// a test that could not fail.
func TestBeadsCloseAcceptsVisualProof(t *testing.T) {
	cwd := initGitRepo(t)
	png := filepath.Join(cwd, "shot.png")
	if err := os.WriteFile(png, []byte("\x89PNG\r\n\x1a\nrest"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(cwd, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cwd, "docs", "plan.dot"), []byte("digraph { a -> b }"), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, proof := range []reports.ProofItem{
		{"image": png},
		{"file": "docs/plan.dot"},
	} {
		err := closeWithReport(t, "gkp-zz2", cwd, proof)
		if err != nil && closeops.GrundVon(err) == closeops.GrundKeinBild {
			t.Errorf("proof %v was refused as invisible: %v", proof, err)
		}
	}
}

// TestBeadsCloseRejectsVanishedFileProof is gkp-2ln at the tool boundary: the
// one-liner that used to walk straight through the gate.
//
// The refusal has to be its own message. "kein aktueller done-Beleg" would send
// the agent off to write a report it already wrote, and the plain "nichts zum
// Ansehen" would tell it to attach a diagram it believes it did attach — both
// produce a second failed attempt rather than the fix, which is to render the
// file (or name the right path).
func TestBeadsCloseRejectsVanishedFileProof(t *testing.T) {
	cwd := initGitRepo(t)
	err := closeWithReport(t, "gkp-zz6", cwd, reports.ProofItem{"file": "docs/gibt-es-nicht.svg"})
	if err == nil {
		t.Fatal("a file proof that does not resolve must not close a bead")
	}
	if closeops.GrundVon(err) != closeops.GrundKeinBild {
		t.Fatalf("Grund = %q, want %q (%v)", closeops.GrundVon(err), closeops.GrundKeinBild, err)
	}
	if strings.Contains(err.Error(), "kein aktueller done-Beleg") {
		t.Errorf("wrong level reported: %q", err)
	}
	if !strings.Contains(err.Error(), "nicht auflösen") {
		t.Errorf("refusal does not name the unresolvable proof: %q", err)
	}
}

// TestBeadsCloseRejectsEmptyGraphArtefact: `touch docs/x.dot` is the next
// one-liner along, and existence alone would have accepted it.
func TestBeadsCloseRejectsEmptyGraphArtefact(t *testing.T) {
	cwd := initGitRepo(t)
	if err := os.MkdirAll(filepath.Join(cwd, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cwd, "docs", "leer.dot"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	err := closeWithReport(t, "gkp-zz7", cwd, reports.ProofItem{"file": "docs/leer.dot"})
	if err == nil || closeops.GrundVon(err) != closeops.GrundKeinBild {
		t.Fatalf("empty .dot closed the bead: %v", err)
	}
}

// TestBeadsCloseForceBypassesVisualProof: the escape hatch has to keep working
// for "wird nicht gemacht" closures, or the gate would make them impossible.
func TestBeadsCloseForceBypassesVisualProof(t *testing.T) {
	cwd := storeDoneEntry(t, "gkp-zz3", map[string]any{"cmd": "true", "out": ""})
	_, err := beadsClose(nil, beadsCloseParams{
		Cwd: cwd, Map: "gkp-zz3", Force: true,
		Closes: []closeItem{{Bead: "gkp-zz3.1", Reason: "Anforderung entfallen, ersetzt durch gkp-zz4"}},
	})
	if err != nil && strings.Contains(err.Error(), "nichts zum Ansehen") {
		t.Errorf("force did not bypass the gate: %v", err)
	}
}

// TestBeadsCloseForceNeedsReason: force without a reason is not an escape
// hatch, it is a quiet way past the gate — and the reason is the only place
// that records WHY (bd's audit log). The gate lives in internal/closeops; this
// pins that the MCP door does not have its own, laxer version of it.
func TestBeadsCloseForceNeedsReason(t *testing.T) {
	cwd := storeDoneEntry(t, "gkp-zz5", map[string]any{"cmd": "true", "out": ""})
	for _, reason := range []string{"", "kein Bild zur Hand"} {
		_, err := beadsClose(nil, beadsCloseParams{
			Cwd: cwd, Map: "gkp-zz5", Force: true,
			Closes: []closeItem{{Bead: "gkp-zz5.1", Reason: reason}},
		})
		if err == nil {
			t.Fatalf("force mit reason %q wurde durchgelassen", reason)
		}
		if closeops.GrundVon(err) != closeops.GrundEingabe {
			t.Errorf("reason %q: Grund = %q, erwartet %q (%v)",
				reason, closeops.GrundVon(err), closeops.GrundEingabe, err)
		}
	}
}

// TestCloseSchemaDocumentsVisualProof: tools/list is the only documentation the
// calling agent gets. A gate the schema does not mention produces a refusal the
// agent cannot anticipate.
func TestCloseSchemaDocumentsVisualProof(t *testing.T) {
	blob, err := json.Marshal(beadsCloseSchema)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{".dot", ".svg", "image"} {
		if !strings.Contains(string(blob), want) {
			t.Errorf("close schema does not mention %q: %s", want, blob)
		}
	}
}

func TestBeadsDepRejectsBadInput(t *testing.T) {
	if _, err := beadsDep(beadsDepParams{Op: "add"}); err == nil {
		t.Error("expected an error without cwd")
	}
	if _, err := beadsDep(beadsDepParams{Cwd: "/tmp", Op: "sideways"}); err == nil {
		t.Error("expected an error for an unknown op")
	}
}

// TestRunOrErrorSurfacesMessage: a domain failure must come back as
// isError:true with the message readable, not as a JSON-RPC error code — the
// calling agent has to see bd's own refusal to act on it.
func TestRunOrErrorSurfacesMessage(t *testing.T) {
	res := runOrError(json.RawMessage(`{"op":"nope","cwd":"/tmp"}`), beadsQuery)
	m, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("unexpected result type %T", res)
	}
	if isErr, _ := m["isError"].(bool); !isErr {
		t.Errorf("expected isError:true, got %v", m)
	}
	blob, _ := json.Marshal(m)
	if !strings.Contains(string(blob), "unbekannte op") {
		t.Errorf("message lost: %s", blob)
	}
}

func TestRunOrErrorRejectsMissingArguments(t *testing.T) {
	res := runOrError(nil, beadsQuery)
	m, _ := res.(map[string]any)
	if isErr, _ := m["isError"].(bool); !isErr {
		t.Errorf("expected isError:true for missing arguments, got %v", m)
	}
	res = runOrError(json.RawMessage(`not json`), beadsQuery)
	m, _ = res.(map[string]any)
	if isErr, _ := m["isError"].(bool); !isErr {
		t.Errorf("expected isError:true for malformed arguments, got %v", m)
	}
}

// TestUnknownBeadsToolStillUnknown: handleBeadsTool must not claim names it
// does not own, or mcp.go would lose its "unknown tool" error.
func TestUnknownBeadsToolStillUnknown(t *testing.T) {
	if _, handled := handleBeadsTool(nil, "beads_delete_database", nil); handled {
		t.Error("handleBeadsTool claimed a name it does not implement")
	}
	if _, handled := handleBeadsTool(nil, "report", nil); handled {
		t.Error("handleBeadsTool must leave the report tool to mcp.go")
	}
}

// TestRunOrErrorRejectsUnknownField: a mutation API must not lose a field it
// did not recognise. encoding/json drops unknown fields by default, and the
// first live beads_create call proved how that plays out — it sent bd's own
// internal names "parent_key"/"parent_id" instead of this schema's "parent",
// and without strict decoding the two issues would have been created at top
// level, outside any map, and reported as a success.
func TestRunOrErrorRejectsUnknownField(t *testing.T) {
	res := runOrError(json.RawMessage(`{"cwd":"/tmp","op":"ready","parnet":"gkp-ar7"}`), beadsQuery)
	m, _ := res.(map[string]any)
	if isErr, _ := m["isError"].(bool); !isErr {
		t.Fatalf("expected isError:true for an unknown field, got %v", m)
	}
	blob, _ := json.Marshal(m)
	// The message has to name the offending field, or the caller cannot fix it.
	if !strings.Contains(string(blob), "parnet") {
		t.Errorf("message does not name the unknown field: %s", blob)
	}
}

// TestRunOrErrorRejectsNestedUnknownField: the same guarantee has to hold
// inside the arrays, which is where the interesting fields live — a dropped
// "acceptance" in issues[] would be silent data loss, not a typo'd query.
func TestRunOrErrorRejectsNestedUnknownField(t *testing.T) {
	res := runOrError(json.RawMessage(
		`{"cwd":"/tmp","issues":[{"title":"x","parent_id":"gkp-ar7"}]}`), beadsCreate)
	m, _ := res.(map[string]any)
	if isErr, _ := m["isError"].(bool); !isErr {
		t.Fatalf("expected isError:true for a nested unknown field, got %v", m)
	}
	blob, _ := json.Marshal(m)
	if !strings.Contains(string(blob), "parent_id") {
		t.Errorf("message does not name the unknown field: %s", blob)
	}
}

// TestRunOrErrorRejectsDateFieldTypo: the date fields are called "due" and
// "defer" here, while bd's own JSON and every later view call the columns
// "due_at" and "defer_until". That is the exact shape of the mistake that
// produced two parentless beads on the first live beads_create — a plausible
// name taken from the wrong layer. Strict decoding has to catch it by name,
// because a silently dropped deadline is indistinguishable from one that was
// never asked for.
func TestRunOrErrorRejectsDateFieldTypo(t *testing.T) {
	cases := []struct {
		args  string
		fn    func(beadsUpdateParams) (string, error)
		field string
	}{
		{`{"cwd":"/tmp","changes":[{"ids":["gkp-a"],"defer_until":"+1d"}]}`, beadsUpdate, "defer_until"},
		{`{"cwd":"/tmp","changes":[{"ids":["gkp-a"],"due_at":"2026-01-15"}]}`, beadsUpdate, "due_at"},
	}
	for _, tc := range cases {
		res := runOrError(json.RawMessage(tc.args), tc.fn)
		m, _ := res.(map[string]any)
		if isErr, _ := m["isError"].(bool); !isErr {
			t.Fatalf("%s: expected isError:true, got %v", tc.field, m)
		}
		blob, _ := json.Marshal(m)
		if !strings.Contains(string(blob), tc.field) {
			t.Errorf("message does not name the unknown field %q: %s", tc.field, blob)
		}
	}
	res := runOrError(json.RawMessage(
		`{"cwd":"/tmp","issues":[{"title":"x","defer_until":"+1d"}]}`), beadsCreate)
	m, _ := res.(map[string]any)
	if isErr, _ := m["isError"].(bool); !isErr {
		t.Fatalf("create: expected isError:true for defer_until, got %v", m)
	}
}

// TestDateFieldsDecodeThroughTheTools: the fields have to survive the tool's
// own params struct, not just bdops' — that is the whole point of the ticket,
// since bd could do this all along and only the MCP door was missing.
func TestDateFieldsDecodeThroughTheTools(t *testing.T) {
	var up beadsUpdateParams
	if err := json.Unmarshal([]byte(
		`{"cwd":"/tmp","changes":[{"ids":["gkp-a"],"due":"2026-01-15","defer":""}]}`), &up); err != nil {
		t.Fatalf("update rejected the date fields: %v", err)
	}
	c := up.Changes[0]
	if c.Due == nil || *c.Due != "2026-01-15" {
		t.Errorf("due = %v, want 2026-01-15", c.Due)
	}
	// An explicit empty string is bd's documented way to clear the field, so it
	// has to arrive as a set pointer rather than as an absence.
	if c.Defer == nil || *c.Defer != "" {
		t.Errorf("defer = %v, want a pointer to the empty string", c.Defer)
	}

	var cr beadsCreateParams
	if err := json.Unmarshal([]byte(
		`{"cwd":"/tmp","issues":[{"title":"x","due":"+1d","defer":"next monday"}]}`), &cr); err != nil {
		t.Fatalf("create rejected the date fields: %v", err)
	}
	if cr.Issues[0].Due != "+1d" || cr.Issues[0].Defer != "next monday" {
		t.Errorf("create dates = %q/%q", cr.Issues[0].Due, cr.Issues[0].Defer)
	}
}

// TestDateSchemaDocumentsFormats: tools/list is the only documentation a
// calling agent ever sees for these fields. A date grammar this layer
// deliberately does NOT validate (it is bd's) must at least be spelled out
// where the caller reads it, and so must the clearing rule — an empty string is
// a value here, not an omission.
func TestDateSchemaDocumentsFormats(t *testing.T) {
	for name, schema := range map[string]map[string]any{
		"create": beadsCreateSchema,
		"update": beadsUpdateSchema,
	} {
		blob, err := json.Marshal(schema)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"+6h", "+1d", "+2w", "tomorrow", "next monday", "2025-01-15", "ready"} {
			if !strings.Contains(string(blob), want) {
				t.Errorf("%s schema does not mention %q", name, want)
			}
		}
	}
	blob, _ := json.Marshal(beadsUpdateSchema)
	if !strings.Contains(string(blob), "String löscht") {
		t.Errorf("update schema does not document clearing by empty string: %s", blob)
	}
}

// TestCreateAcceptsNumericPriority is the counterpart: widening the JSON type
// must actually work end to end through the tool's own params struct.
func TestCreateAcceptsNumericPriority(t *testing.T) {
	var p beadsCreateParams
	if err := json.Unmarshal([]byte(`{"cwd":"/tmp","issues":[{"title":"x","priority":2}]}`), &p); err != nil {
		t.Fatalf("numeric priority rejected: %v", err)
	}
	if got := string(p.Issues[0].Priority); got != "2" {
		t.Errorf("priority = %q, want \"2\"", got)
	}
}

// TestPrioritySchemaAllowsBothTypes keeps the schema honest about it: the
// description says "0-4", so declaring only "string" invites exactly the
// payload the decoder used to reject.
func TestPrioritySchemaAllowsBothTypes(t *testing.T) {
	items, _ := beadsCreateSchema["properties"].(map[string]any)["issues"].(map[string]any)["items"].(map[string]any)
	prio, _ := items["properties"].(map[string]any)["priority"].(map[string]any)
	types, ok := prio["type"].([]string)
	if !ok {
		t.Fatalf("priority type is %#v, want a list of types", prio["type"])
	}
	seen := map[string]bool{}
	for _, tp := range types {
		seen[tp] = true
	}
	if !seen["string"] || !seen["integer"] {
		t.Errorf("priority type = %v, want both string and integer", types)
	}
}

// ------------------------------------------------------- kartentor (gkp-few)

// kartenAttrappe is the stand-in for bd on BOTH doors of this package. Close
// still goes to the real bdops (so every existing assertion about "the gate was
// passed and bd then refused" keeps meaning what it meant); only the map check
// is answered from here, because answering it for real would need a beads
// database — and that is what internal/bdops' own tests are for.
type kartenAttrappe struct {
	fehler   error
	ergebnis bdops.Result
	gefragt  []string // "<karte>/<bead>,<bead>"
}

func (k *kartenAttrappe) Close(pfad string, ids []string, grund string, erzwingen bool) (bdops.Result, error) {
	return bdops.Close(pfad, ids, grund, erzwingen)
}

func (k *kartenAttrappe) PruefeKartenzugehoerigkeit(pfad, karte string, beads []string) (bdops.Result, error) {
	k.gefragt = append(k.gefragt, karte+"/"+strings.Join(beads, ","))
	return k.ergebnis, k.fehler
}

// mitKartenattrappe hangs the stub in front of bd for the duration of one test.
func mitKartenattrappe(t *testing.T, fehler error) *kartenAttrappe {
	t.Helper()
	k := &kartenAttrappe{fehler: fehler}
	orig := bdWeg
	bdWeg = k
	t.Cleanup(func() { bdWeg = orig })
	return k
}

// jsonlImStore lists every .jsonl below CC_REPORTS_DIR — across all repo
// directories, because a wrong map creates a NEW file and the question is not
// whether the right one grew but whether anything appeared at all.
func jsonlImStore(t *testing.T) []string {
	t.Helper()
	wurzel := os.Getenv("CC_REPORTS_DIR")
	if wurzel == "" {
		t.Fatal("CC_REPORTS_DIR nicht gesetzt — dieser Test darf den echten Store nie sehen")
	}
	var gefunden []string
	if err := filepath.WalkDir(wurzel, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(p, ".jsonl") {
			gefunden = append(gefunden, p)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return gefunden
}

// TestReportSchreibtNichtUnterEineFremdeKarte is gkp-few at the OTHER door. The
// close tool is the one that got caught, but `report` writes into the same
// append-only store with the same map field and had the same hole — and it is
// the door that runs far more often, once per bead going in_progress.
func TestReportSchreibtNichtUnterEineFremdeKarte(t *testing.T) {
	withReportsDir(t)
	repo := initGitRepo(t)
	k := mitKartenattrappe(t, errors.New(`karte "gkp-yy5" gibt es in dieser beads-Datenbank nicht`))

	roh, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": "report", "arguments": map[string]any{
			"cwd": repo, "map": "gkp-yy5", "bead": "gkp-3oh", "kind": "done", "title": "fertig",
		}},
	})
	rec := doRPC(t, string(roh))

	var env struct {
		Result struct {
			Content []struct{ Text string } `json:"content"`
			IsError bool                    `json:"isError"`
		} `json:"result"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("Antwort ist kein JSON-RPC: %v\n%s", err, rec.Body.String())
	}
	if !env.Result.IsError {
		t.Fatalf("eine fremde Karte wurde durchgelassen: %s", rec.Body.String())
	}
	if len(env.Result.Content) == 0 || !strings.Contains(env.Result.Content[0].Text, "gkp-yy5") {
		t.Errorf("Meldung nennt die Karte nicht: %s", rec.Body.String())
	}
	if dateien := jsonlImStore(t); len(dateien) != 0 {
		t.Errorf("es wurde geschrieben, obwohl die Karte nicht stimmt: %v", dateien)
	}
	if len(k.gefragt) != 1 || k.gefragt[0] != "gkp-yy5/gkp-3oh" {
		t.Errorf("gefragt wurde %v, erwartet [gkp-yy5/gkp-3oh]", k.gefragt)
	}
}

// TestReportPrueftErstDieFelderDannDieKarte pins the ORDER of the two
// refusals. A payload with a broken "kind" must still be told about "kind": the
// map check is the more expensive answer and the less useful one when the
// caller has not even got the shape right, and a report that swallows the field
// error behind a bd lookup makes the cheap mistake look like the dear one.
func TestReportPrueftErstDieFelderDannDieKarte(t *testing.T) {
	withReportsDir(t)
	repo := initGitRepo(t)
	k := mitKartenattrappe(t, errors.New("darf hier gar nicht erst gefragt werden"))

	roh, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 2, "method": "tools/call",
		"params": map[string]any{"name": "report", "arguments": map[string]any{
			"cwd": repo, "map": "gkp-4e1", "bead": "gkp-4e1.1", "kind": "fertig", "title": "x",
		}},
	})
	rec := doRPC(t, string(roh))
	if !strings.Contains(rec.Body.String(), "kind") {
		t.Errorf("die Feldmeldung ging verloren: %s", rec.Body.String())
	}
	if len(k.gefragt) != 0 {
		t.Errorf("bd wurde für eine Nutzlast gefragt, die schon an der Form scheitert: %v", k.gefragt)
	}
}

// TestBeadsCloseFragtDieKarteUndSchreibtNichtsWennSieNichtPasst is the close
// door's half of the same rule, checked through the MCP tool rather than
// through closeops directly: the translation layer must hand the map down, not
// invent one.
func TestBeadsCloseFragtDieKarteUndSchreibtNichtsWennSieNichtPasst(t *testing.T) {
	withReportsDir(t)
	repo := initGitRepo(t)
	k := mitKartenattrappe(t, errors.New(`bead "gkp-3oh" hängt nicht unter der Karte "gkp-yy5"`))

	_, err := beadsClose(&http.Request{}, beadsCloseParams{
		Cwd: repo, Map: "gkp-yy5", Closes: []closeItem{{
			Bead:   "gkp-3oh",
			Report: &closeReport{Title: "fertig", Proof: []reports.ProofItem{{"image": "/gibt/es/nicht.png"}}},
		}},
	})
	if err == nil {
		t.Fatal("die falsche Karte wurde durchgelassen")
	}
	// Nicht die Belegmeldung: der Beleg ist hier absichtlich kaputt, und trotzdem
	// muss die KARTE der gemeldete Fehler sein — sonst sass die Pruefung hinter
	// dem Schreiben und der Eintrag läge schon im Store.
	if closeops.GrundVon(err) != closeops.GrundEingabe {
		t.Errorf("Grund = %q, erwartet %q (%v)", closeops.GrundVon(err), closeops.GrundEingabe, err)
	}
	if dateien := jsonlImStore(t); len(dateien) != 0 {
		t.Errorf("es wurde geschrieben, obwohl die Karte nicht stimmt: %v", dateien)
	}
	if len(k.gefragt) != 1 || k.gefragt[0] != "gkp-yy5/gkp-3oh" {
		t.Errorf("gefragt wurde %v, erwartet [gkp-yy5/gkp-3oh]", k.gefragt)
	}
}

// TestForceGehtNichtAmKartentorVorbei: force ist die Ausrede fuer einen
// FEHLENDEN Beleg ("wird nicht gemacht"), nicht fuer eine falsche Akte. Ein
// force, das auch die Karte durchwinkt, schriebe den Eintrag weiterhin in die
// Datei, die der Tippfehler erzeugt — und zwar mit der Begruendung, dass es
// keinen Beleg gibt. Beides zusammen waere die schlechteste Zeile im Store.
func TestForceGehtNichtAmKartentorVorbei(t *testing.T) {
	withReportsDir(t)
	repo := initGitRepo(t)
	k := mitKartenattrappe(t, errors.New(`karte "gkp-yy5" gibt es in dieser beads-Datenbank nicht`))

	_, err := beadsClose(&http.Request{}, beadsCloseParams{
		Cwd: repo, Map: "gkp-yy5", Force: true,
		Closes: []closeItem{{Bead: "gkp-3oh", Reason: "Anforderung entfallen, ersetzt durch gkp-4e1.9"}},
	})
	if err == nil {
		t.Fatal("force hat die Kartenprüfung umgangen")
	}
	// "es kam ein Fehler" allein misst hier NICHTS: das Wegwerf-Verzeichnis hat
	// keine beads-Datenbank, also scheitert auch ein durchgewinktes force —
	// nur eben erst an bd, nach dem Tor. Gemessen in der Gegenprobe: mit
	// `if !a.Erzwingen` um die Pruefung blieb genau diese Zeile gruen. Die
	// Behauptung muss deshalb sein, dass GEFRAGT wurde und wessen Meldung kommt.
	if len(k.gefragt) != 1 {
		t.Errorf("die Kartenprüfung wurde bei force %d mal gefragt, erwartet 1", len(k.gefragt))
	}
	if !strings.Contains(err.Error(), "gkp-yy5") {
		t.Errorf("Meldung nennt die Karte nicht: %v", err)
	}
	if dateien := jsonlImStore(t); len(dateien) != 0 {
		t.Errorf("force hat trotz falscher Karte geschrieben: %v", dateien)
	}
}

// TestSchemataNennenDasKartentor: tools/list ist die einzige Dokumentation, die
// ein aufrufender Agent bekommt. Eine Schranke, die das Schema verschweigt,
// erzeugt eine Ablehnung, mit der niemand gerechnet hat — und beim Kartentor
// waere die Ueberraschung besonders unnoetig, weil die Antwort ("welche Karte
// ist es denn") in einem Satz danebensteht.
func TestSchemataNennenDasKartentor(t *testing.T) {
	for name, schema := range map[string]map[string]any{
		"beads_close": beadsCloseSchema,
		"report":      reportInputSchema,
	} {
		blob, err := json.Marshal(schema)
		if err != nil {
			t.Fatal(err)
		}
		// "Epic" allein genuegt als Behauptung nicht — das Wort stand vorher
		// schon da, und trotzdem wurde nichts geprueft. Verlangt wird, dass das
		// Schema die PRUEFUNG nennt und sagt, wo die richtige Id herkommt.
		for _, teil := range []string{"GEPRÜFT", "existierendes Epic", "type=epic"} {
			if !strings.Contains(string(blob), teil) {
				t.Errorf("%s erwähnt %q nicht: %s", name, teil, blob)
			}
		}
	}
}
