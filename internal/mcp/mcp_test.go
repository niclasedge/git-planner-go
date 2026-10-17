package mcp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/niclasedge/git-planner-go/internal/reports"
)

// withReportsDir points CC_REPORTS_DIR at a fresh t.TempDir() and returns it
// — same isolation pattern as internal/reports/store_test.go.
func withReportsDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("CC_REPORTS_DIR", dir)
	return dir
}

// initGitRepo creates a minimal git repo so deriveGit (inside reports.Append)
// has something real to resolve. Duplicated from store_test.go on purpose:
// internal/reports is off-limits for this bead (owned by demo-axk.1),
// so no shared test helper can live there.
func initGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "test")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "f.txt")
	run("commit", "-q", "-m", "init")
	return dir
}

func doRPC(t *testing.T, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	req.Host = "127.0.0.1:8765"
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, req)
	return rec
}

// rpcEnvelope is the shape common to every JSON-RPC response used by these
// tests: raw Result/Error so each test can decode further as needed.
type rpcEnvelope struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *rpcError       `json:"error"`
}

func decodeEnvelope(t *testing.T, rec *httptest.ResponseRecorder) rpcEnvelope {
	t.Helper()
	var env rpcEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("response is not valid JSON-RPC: %v\nbody: %s", err, rec.Body.String())
	}
	return env
}

func TestInitialize(t *testing.T) {
	rec := doRPC(t, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	env := decodeEnvelope(t, rec)
	if env.Error != nil {
		t.Fatalf("unexpected error: %+v", env.Error)
	}
	var result map[string]any
	if err := json.Unmarshal(env.Result, &result); err != nil {
		t.Fatalf("result not an object: %v", err)
	}
	if result["protocolVersion"] != protocolVersion {
		t.Fatalf("protocolVersion = %v, want %q", result["protocolVersion"], protocolVersion)
	}
	if _, ok := result["capabilities"]; !ok {
		t.Fatalf("expected capabilities in result, got %v", result)
	}
	if _, ok := result["serverInfo"]; !ok {
		t.Fatalf("expected serverInfo in result, got %v", result)
	}
}

// TestToolsListReportTool checks the report tool's own schema. The catalog is
// no longer a single tool — the beads write API added five more — so the
// "exactly one" assertion moved to TestToolsListCatalog, which pins the full
// set instead.
func TestToolsListReportTool(t *testing.T) {
	rec := doRPC(t, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
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
	var tool struct {
		Name        string         `json:"name"`
		Description string         `json:"description"`
		InputSchema map[string]any `json:"inputSchema"`
	}
	for _, tl := range result.Tools {
		if tl.Name == "report" {
			tool = tl
		}
	}
	if tool.Name != "report" {
		t.Fatalf("no report tool in catalog: %+v", result.Tools)
	}
	if tool.Description == "" {
		t.Fatalf("expected a non-empty tool description")
	}
	props, _ := tool.InputSchema["properties"].(map[string]any)
	for _, field := range []string{"cwd", "map", "bead", "kind", "title", "impact", "proof", "notes", "supersedes"} {
		if _, ok := props[field]; !ok {
			t.Fatalf("inputSchema.properties missing field %q", field)
		}
	}
	required, _ := tool.InputSchema["required"].([]any)
	wantRequired := map[string]bool{"cwd": true, "map": true, "bead": true, "kind": true, "title": true}
	if len(required) != len(wantRequired) {
		t.Fatalf("required = %v, want exactly %v", required, wantRequired)
	}
	for _, r := range required {
		if !wantRequired[r.(string)] {
			t.Fatalf("unexpected required field %q", r)
		}
	}
}

func TestToolsCallWritesEntryAndReturnsOneLine(t *testing.T) {
	mitKartenattrappe(t, nil) // gkp-few: die Kartenprüfung hat eigene Tests, hier steht der Schreibweg im Weg
	dir := withReportsDir(t)
	repo := initGitRepo(t)

	body := map[string]any{
		"jsonrpc": "2.0",
		"id":      3,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "report",
			"arguments": map[string]any{
				"cwd":   repo,
				"map":   "demo-axk",
				"bead":  "demo-axk.3",
				"kind":  "plan",
				"title": "MCP-Endpoint gebaut",
			},
		},
	}
	raw, _ := json.Marshal(body)
	rec := doRPC(t, string(raw))
	env := decodeEnvelope(t, rec)
	if env.Error != nil {
		t.Fatalf("unexpected error: %+v", env.Error)
	}

	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(env.Result, &result); err != nil {
		t.Fatalf("result: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected isError=false, got content %+v", result.Content)
	}
	if len(result.Content) != 1 {
		t.Fatalf("expected exactly 1 content item, got %d", len(result.Content))
	}
	text := result.Content[0].Text
	if strings.Count(text, "\n") != 0 {
		t.Fatalf("expected a single line, got %q", text)
	}
	// gkp-ar7.12: the URL must be one this server actually routes. The old
	// expectation (/reports/<repo>/<map>#<id>) was a guaranteed 404 —
	// "reports" is a reserved segment served only by reportassets, which
	// demands exactly /reports/<repo>/assets/<file>. The view lives at the
	// project route /<repo>/reports instead. Asserted in full rather than by
	// prefix, so a regression back to a /reports/... shape fails here instead
	// of slipping through a loose prefix check.
	wantText := "report gespeichert - http://127.0.0.1:8765/" + url.PathEscape(filepath.Base(repo)) + "/reports"
	if text != wantText {
		t.Fatalf("text = %q, want %q", text, wantText)
	}
	if strings.Contains(text, "#") {
		t.Fatalf("url must not carry a fragment (browser.js has no hash handling), got %q", text)
	}

	// Confirm the JSONL line actually landed on disk.
	fpath := filepath.Join(dir, filepath.Base(repo), "demo-axk.jsonl")
	raw2, err := os.ReadFile(fpath)
	if err != nil {
		t.Fatalf("expected jsonl file to exist: %v", err)
	}
	if !bytes.Contains(raw2, []byte(`"bead":"demo-axk.3"`)) {
		t.Fatalf("jsonl line missing expected bead field: %s", raw2)
	}
}

func TestToolsCallMissingRequiredField(t *testing.T) {
	withReportsDir(t)
	repo := initGitRepo(t)

	body := map[string]any{
		"jsonrpc": "2.0", "id": 4, "method": "tools/call",
		"params": map[string]any{
			"name": "report",
			"arguments": map[string]any{
				"cwd": repo, "map": "m", "bead": "b", "kind": "plan",
				// title fehlt absichtlich
			},
		},
	}
	raw, _ := json.Marshal(body)
	rec := doRPC(t, string(raw))
	env := decodeEnvelope(t, rec)
	if env.Error != nil {
		t.Fatalf("expected a tool-result error, not a JSON-RPC error: %+v", env.Error)
	}
	var result struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(env.Result, &result); err != nil {
		t.Fatalf("result: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected isError=true for missing title")
	}
	if !strings.Contains(result.Content[0].Text, "title") {
		t.Fatalf("expected message to mention title, got %q", result.Content[0].Text)
	}
}

func TestToolsCallBadKind(t *testing.T) {
	withReportsDir(t)
	repo := initGitRepo(t)

	body := map[string]any{
		"jsonrpc": "2.0", "id": 5, "method": "tools/call",
		"params": map[string]any{
			"name": "report",
			"arguments": map[string]any{
				"cwd": repo, "map": "m", "bead": "b", "kind": "wat", "title": "t",
			},
		},
	}
	raw, _ := json.Marshal(body)
	rec := doRPC(t, string(raw))
	env := decodeEnvelope(t, rec)
	if env.Error != nil {
		t.Fatalf("expected a tool-result error, not a JSON-RPC error: %+v", env.Error)
	}
	var result struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(env.Result, &result); err != nil {
		t.Fatalf("result: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected isError=true for bad kind")
	}
	if !strings.Contains(result.Content[0].Text, "kind") {
		t.Fatalf("expected message to mention kind, got %q", result.Content[0].Text)
	}
}

func TestMalformedTopLevelJSONReturnsParseError(t *testing.T) {
	withReportsDir(t)
	rec := doRPC(t, `{not json`)
	env := decodeEnvelope(t, rec)
	if env.Error == nil {
		t.Fatalf("expected a JSON-RPC error for malformed body")
	}
	if env.Error.Code != codeParseError {
		t.Fatalf("code = %d, want %d", env.Error.Code, codeParseError)
	}
}

func TestToolsCallMalformedArgumentsJSON(t *testing.T) {
	withReportsDir(t)
	rec := doRPC(t, `{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"report","arguments":"not-an-object"}}`)
	env := decodeEnvelope(t, rec)
	if env.Error == nil {
		t.Fatalf("expected a JSON-RPC error for non-object arguments")
	}
	if env.Error.Code != codeInvalidParams {
		t.Fatalf("code = %d, want %d", env.Error.Code, codeInvalidParams)
	}
}

func TestToolsCallUnknownTool(t *testing.T) {
	withReportsDir(t)
	rec := doRPC(t, `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"nope","arguments":{}}}`)
	env := decodeEnvelope(t, rec)
	if env.Error == nil {
		t.Fatalf("expected a JSON-RPC error for unknown tool name")
	}
	if env.Error.Code != codeInvalidParams {
		t.Fatalf("code = %d, want %d", env.Error.Code, codeInvalidParams)
	}
}

func TestUnknownMethodReturnsCleanJSONRPCError(t *testing.T) {
	rec := doRPC(t, `{"jsonrpc":"2.0","id":8,"method":"resources/list"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (no 500 for unknown method)", rec.Code)
	}
	env := decodeEnvelope(t, rec)
	if env.Error == nil {
		t.Fatalf("expected a JSON-RPC error for unknown method")
	}
	if env.Error.Code != codeMethodNotFound {
		t.Fatalf("code = %d, want %d", env.Error.Code, codeMethodNotFound)
	}
}

func TestNotificationGetsNoResponseBody(t *testing.T) {
	rec := doRPC(t, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("expected empty body for a notification, got %q", rec.Body.String())
	}
}

func TestBodyTooLargeIsRejected(t *testing.T) {
	huge := strings.Repeat("x", maxRequestBodyBytes+1000)
	body := `{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"report","arguments":{"notes":"` + huge + `"}}}`
	rec := doRPC(t, body)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", rec.Code)
	}
}

// TestToolsCallNeutralizesPathTraversalCwdAndNonImageProof is the required
// end-to-end attack test for demo-axk.3: a cwd crafted to escape
// CC_REPORTS_DIR (demo-axk.1 finding 1's exact live repro, replayed
// through the MCP endpoint) combined with a proof.image pointing at
// non-image content (finding 2's repro). Both must be neutralized by the
// store this endpoint sits on top of — this test proves the endpoint passes
// the raw payload through to that store rather than working around it.
func TestToolsCallNeutralizesPathTraversalCwdAndNonImageProof(t *testing.T) {
	mitKartenattrappe(t, nil) // gkp-few: die Kartenprüfung hat eigene Tests, hier steht der Schreibweg im Weg
	base := withReportsDir(t)
	t.Setenv("PATH", "") // git becomes unresolvable -> deriveGit falls back to raw cwd

	nonGitDir := t.TempDir()
	evilCwd := nonGitDir + "/.."

	fakeKey := filepath.Join(t.TempDir(), "id_rsa")
	keyContent := "-----BEGIN OPENSSH PRIVATE KEY-----\nfake\n-----END OPENSSH PRIVATE KEY-----\n"
	if err := os.WriteFile(fakeKey, []byte(keyContent), 0o600); err != nil {
		t.Fatal(err)
	}

	body := map[string]any{
		"jsonrpc": "2.0", "id": 10, "method": "tools/call",
		"params": map[string]any{
			"name": "report",
			"arguments": map[string]any{
				"cwd": evilCwd, "map": "demo-axk", "bead": "demo-axk.3",
				"kind": "plan", "title": "Angriffs-Payload",
				"proof": []map[string]any{{"image": fakeKey}},
			},
		},
	}
	raw, _ := json.Marshal(body)
	rec := doRPC(t, string(raw))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (nothing must crash on this payload)", rec.Code)
	}
	env := decodeEnvelope(t, rec)
	if env.Error != nil {
		t.Fatalf("expected a normal tool result, got JSON-RPC error: %+v", env.Error)
	}
	var result struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(env.Result, &result); err != nil {
		t.Fatalf("result: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected the store to fail open (isError=false), got %+v", result.Content)
	}

	// Nothing must have escaped CC_REPORTS_DIR: every file the ingest wrote
	// stays a descendant of base.
	err := filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
		return err
	})
	if err != nil {
		t.Fatalf("walking %s: %v", base, err)
	}
	rel, err := filepath.Rel(base, base)
	if err != nil || strings.HasPrefix(rel, "..") {
		t.Fatalf("base dir itself misbehaves: rel=%q err=%v", rel, err)
	}
	// Locate the map's jsonl file via reports.LoadMap across every directory
	// actually created under base — the escaped-if-broken path would land
	// as a sibling of base, i.e. NOT be found this way, and the entry would
	// then be missing from every repo folder we can enumerate under base.
	entries, skipped, err := loadFromAnyRepoUnder(t, base, "demo-axk")
	if err != nil {
		t.Fatalf("loadFromAnyRepoUnder: %v", err)
	}
	if skipped != 0 {
		t.Fatalf("expected 0 skipped lines, got %d", skipped)
	}
	if len(entries) != 1 {
		t.Fatalf("expected exactly 1 entry written under CC_REPORTS_DIR, got %d", len(entries))
	}
	entry := entries[0]
	if entry.Repo == ".." || entry.Repo == "." || entry.Repo == "" || strings.ContainsAny(entry.Repo, `/\`) {
		t.Fatalf("unsafe repo derived from evil cwd: %q", entry.Repo)
	}
	if len(entry.Proof) != 1 {
		t.Fatalf("expected 1 proof item, got %d", len(entry.Proof))
	}
	missing, _ := entry.Proof[0]["missing"].(bool)
	if !missing {
		t.Fatalf("expected the non-image proof to be rejected as missing:true, got %v", entry.Proof[0])
	}
	if img, _ := entry.Proof[0]["image"].(string); img != fakeKey {
		t.Fatalf("rejected proof must keep the declared (uncopied) path, got %q", img)
	}
	// The private-key content must not have been copied anywhere under base.
	found := false
	_ = filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		b, _ := os.ReadFile(path)
		if bytes.Contains(b, []byte("OPENSSH PRIVATE KEY")) {
			found = true
		}
		return nil
	})
	if found {
		t.Fatalf("private key content leaked into a file under %s", base)
	}
}

// TestToolsCallDoesNotWaitForNotifyBead is finding 1's regression test
// (demo-axk.3 review, Welle 2): tools/call must answer without waiting
// for the bd notification. Reproduces the reported hang scenario with a
// stub instead of a slow real "bd", synchronized over channels — no
// time.Sleep, no flakiness from a leftover goroutine outliving the test.
func TestToolsCallDoesNotWaitForNotifyBead(t *testing.T) {
	mitKartenattrappe(t, nil) // gkp-few: die Kartenprüfung hat eigene Tests, hier steht der Schreibweg im Weg
	withReportsDir(t)
	repo := initGitRepo(t)

	release := make(chan struct{})
	done := make(chan struct{})
	orig := notifyBeadFunc
	notifyBeadFunc = func(cwd, bead, url string) {
		<-release // held open by the test until after the HTTP response is in hand
		close(done)
	}
	t.Cleanup(func() { notifyBeadFunc = orig })

	body := map[string]any{
		"jsonrpc": "2.0", "id": 11, "method": "tools/call",
		"params": map[string]any{
			"name": "report",
			"arguments": map[string]any{
				"cwd": repo, "map": "demo-axk", "bead": "demo-axk.3",
				"kind": "plan", "title": "hangender bd blockiert nicht",
			},
		},
	}
	raw, _ := json.Marshal(body)
	rec := doRPC(t, string(raw)) // must return even though release hasn't fired yet

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	select {
	case <-done:
		t.Fatalf("notifyBeadAsync's stub already completed — handleToolsCall waited for it instead of firing it off")
	default:
		// expected: the response came back while the stub is still blocked
	}

	close(release)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("stub goroutine never observed the release signal")
	}
}

// TestNotifyBeadRejectsFlagLikeBeadWithoutStartingAProcess is finding 2's
// regression test: a bead value that bd/cobra would parse as a flag (e.g.
// "-h") must never reach exec — proven by pointing PATH at a fake "bd" that
// would leave a sentinel file behind if it were ever invoked, then asserting
// that file does not exist.
func TestNotifyBeadRejectsFlagLikeBeadWithoutStartingAProcess(t *testing.T) {
	binDir := t.TempDir()
	sentinel := filepath.Join(binDir, "bd-was-invoked")
	fakeBd := filepath.Join(binDir, "bd")
	// Uses a shell builtin (">") to write the sentinel, not an external
	// "touch" — PATH below only contains binDir, so any external tool the
	// script shelled out to would silently fail to resolve and mask the
	// very invocation this test is trying to detect.
	script := "#!/bin/sh\n> '" + sentinel + "'\nexit 0\n"
	if err := os.WriteFile(fakeBd, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)

	notifyBead(t.TempDir(), "-h", "http://127.0.0.1:8765/reports/x/y#z")

	if _, err := os.Stat(sentinel); err == nil {
		t.Fatalf("bd was invoked for the flag-like bead \"-h\" — validation did not stop the process")
	} else if !os.IsNotExist(err) {
		t.Fatalf("unexpected error checking sentinel: %v", err)
	}
}

// TestIsValidBeadID pins the id shapes notifyBead accepts/rejects directly,
// independent of any subprocess. Includes the two real bd id shapes the
// coordinator's follow-up caught missing from the original pattern:
// "gt--xyz" (bd's own documented escape form for ids that look like flags,
// per `bd show --help`: "--id=gt--xyz") and "demo-axk.1.2" (a nested
// sub-bead, more than one ".N" level) — both must validate, since the "--"
// argv terminator in notifyBead already closes the flag-injection hole on
// its own and this pattern only needs to reject a leading "-".
func TestIsValidBeadID(t *testing.T) {
	valid := []string{"demo-axk", "demo-axk.3", "abc123", "a.10", "gt--xyz", "demo-axk.1.2"}
	invalid := []string{"-h", "-f", "--id", "", "--", "cc setup", "cc/setup"}
	for _, id := range valid {
		if !isValidBeadID(id) {
			t.Errorf("isValidBeadID(%q) = false, want true", id)
		}
	}
	for _, id := range invalid {
		if isValidBeadID(id) {
			t.Errorf("isValidBeadID(%q) = true, want false", id)
		}
	}
}

// loadFromAnyRepoUnder scans base's immediate subdirectories (each one a
// candidate "repo" folder) and returns the first non-empty LoadMap result
// for mapName. Mirrors what a human/operator would do to find where an
// entry actually landed, without assuming the sanitized repo name.
func loadFromAnyRepoUnder(t *testing.T, base, mapName string) ([]reports.Entry, int, error) {
	t.Helper()
	des, err := os.ReadDir(base)
	if err != nil {
		return nil, 0, err
	}
	for _, d := range des {
		if !d.IsDir() {
			continue
		}
		entries, skipped, err := reports.LoadMap(d.Name(), mapName)
		if err != nil {
			return nil, 0, err
		}
		if len(entries) > 0 {
			return entries, skipped, nil
		}
	}
	return nil, 0, nil
}
