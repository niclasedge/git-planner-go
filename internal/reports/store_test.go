package reports

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// withReportsDir points CC_REPORTS_DIR at a fresh t.TempDir() and returns it.
func withReportsDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("CC_REPORTS_DIR", dir)
	return dir
}

// initGitRepo creates a minimal git repo in a temp dir so deriveGit has
// something real to resolve (branch, toplevel).
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

func minimalPayload(cwd string) Payload {
	return Payload{
		Cwd:   cwd,
		Map:   "demo-axk",
		Bead:  "demo-axk.1",
		Kind:  "plan",
		Title: "reports-store gebaut",
	}
}

func TestValidateRequiredFields(t *testing.T) {
	withReportsDir(t)
	cases := []struct {
		name string
		p    Payload
	}{
		{"missing map", Payload{Bead: "b", Kind: "plan", Title: "t"}},
		{"missing bead", Payload{Map: "m", Kind: "plan", Title: "t"}},
		{"bad kind", Payload{Map: "m", Bead: "b", Kind: "wat", Title: "t"}},
		{"missing title", Payload{Map: "m", Bead: "b", Kind: "plan"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := Append(c.p); err == nil {
				t.Fatalf("expected validation error, got nil")
			}
		})
	}
}

func TestAppendEmptyProofIsAllowed(t *testing.T) {
	withReportsDir(t)
	repo := initGitRepo(t)
	entry, err := Append(minimalPayload(repo))
	if err != nil {
		t.Fatalf("Append: %v", err)
	}
	if len(entry.Proof) != 0 {
		t.Fatalf("expected empty proof, got %v", entry.Proof)
	}
	if entry.ID == "" || entry.At == "" {
		t.Fatalf("expected id/at to be set, got %+v", entry)
	}
}

func TestAppendDerivesGitFields(t *testing.T) {
	withReportsDir(t)
	repo := initGitRepo(t)
	entry, err := Append(minimalPayload(repo))
	if err != nil {
		t.Fatalf("Append: %v", err)
	}
	if entry.Repo != filepath.Base(repo) {
		t.Fatalf("repo = %q, want %q", entry.Repo, filepath.Base(repo))
	}
	if entry.Worktree != "main" {
		t.Fatalf("worktree = %q, want main", entry.Worktree)
	}
	if entry.Branch != "main" {
		t.Fatalf("branch = %q, want main", entry.Branch)
	}
}

func TestAppendIsAppendOnlyJSONL(t *testing.T) {
	dir := withReportsDir(t)
	repo := initGitRepo(t)
	p := minimalPayload(repo)

	if _, err := Append(p); err != nil {
		t.Fatalf("Append 1: %v", err)
	}
	p.Kind = "done"
	if _, err := Append(p); err != nil {
		t.Fatalf("Append 2: %v", err)
	}

	fpath := filepath.Join(dir, filepath.Base(repo), "demo-axk.jsonl")
	raw, err := os.ReadFile(fpath)
	if err != nil {
		t.Fatalf("read jsonl: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %q", len(lines), string(raw))
	}
	for _, l := range lines {
		var e Entry
		if err := json.Unmarshal([]byte(l), &e); err != nil {
			t.Fatalf("line not valid JSON: %v (%q)", err, l)
		}
	}
}

// fakePNG is a minimal valid PNG magic-byte prefix plus filler — enough for
// looksLikeImage to accept it without needing a real, decodable image.
var fakePNG = append([]byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}, []byte("-fake-body")...)

func TestAppendCopiesImageProof(t *testing.T) {
	dir := withReportsDir(t)
	repo := initGitRepo(t)

	srcImg := filepath.Join(t.TempDir(), "shot.png")
	if err := os.WriteFile(srcImg, fakePNG, 0o644); err != nil {
		t.Fatal(err)
	}

	p := minimalPayload(repo)
	p.Proof = []ProofItem{{"image": srcImg}}
	entry, err := Append(p)
	if err != nil {
		t.Fatalf("Append: %v", err)
	}
	if len(entry.Proof) != 1 {
		t.Fatalf("expected 1 proof item, got %d", len(entry.Proof))
	}
	dst, ok := entry.Proof[0]["image"].(string)
	if !ok {
		t.Fatalf("expected image path, got %v", entry.Proof[0])
	}
	if dst == srcImg {
		t.Fatalf("image path was not rewritten: %q", dst)
	}
	if !strings.HasPrefix(dst, filepath.Join(dir, filepath.Base(repo), "assets")) {
		t.Fatalf("image not copied under assets dir: %q", dst)
	}
	copied, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read copied image: %v", err)
	}
	if string(copied) != string(fakePNG) {
		t.Fatalf("copied image content mismatch: %q", copied)
	}
	if _, missing := entry.Proof[0]["missing"]; missing {
		t.Fatalf("existing image should not be marked missing")
	}
}

// TestAppendRejectsNonImageContent is the regression test for demo-axk.1
// finding 2, using the review's exact live repro: a proof.image pointing at
// a file with SSH-private-key content must never be copied into assets/.
func TestAppendRejectsNonImageContent(t *testing.T) {
	dir := withReportsDir(t)
	repo := initGitRepo(t)

	fakeKey := filepath.Join(t.TempDir(), "id_rsa")
	keyContent := []byte("-----BEGIN OPENSSH PRIVATE KEY-----\nfake\n-----END OPENSSH PRIVATE KEY-----\n")
	if err := os.WriteFile(fakeKey, keyContent, 0o600); err != nil {
		t.Fatal(err)
	}

	p := minimalPayload(repo)
	p.Proof = []ProofItem{{"image": fakeKey}}
	entry, err := Append(p)
	if err != nil {
		t.Fatalf("Append should fail open, got error: %v", err)
	}
	missing, _ := entry.Proof[0]["missing"].(bool)
	if !missing {
		t.Fatalf("expected non-image content to be rejected as missing:true, got %v", entry.Proof[0])
	}
	if img, _ := entry.Proof[0]["image"].(string); img != fakeKey {
		t.Fatalf("rejected proof must keep the declared (uncopied) path, got %q", img)
	}
	assetsDir := filepath.Join(dir, filepath.Base(repo), "assets")
	if des, _ := os.ReadDir(assetsDir); len(des) != 0 {
		t.Fatalf("private key content must not be copied into assets/, found: %v", des)
	}
}

// TestAppendRejectsOversizedImage is the regression test for the size half of
// finding 2: a valid-looking PNG over maxImageBytes is treated as missing,
// not copied wholesale.
func TestAppendRejectsOversizedImage(t *testing.T) {
	withReportsDir(t)
	repo := initGitRepo(t)

	big := filepath.Join(t.TempDir(), "big.png")
	content := append(append([]byte{}, fakePNG...), make([]byte, maxImageBytes+1)...)
	if err := os.WriteFile(big, content, 0o644); err != nil {
		t.Fatal(err)
	}

	p := minimalPayload(repo)
	p.Proof = []ProofItem{{"image": big}}
	entry, err := Append(p)
	if err != nil {
		t.Fatalf("Append should fail open, got error: %v", err)
	}
	missing, _ := entry.Proof[0]["missing"].(bool)
	if !missing {
		t.Fatalf("expected oversized image (%d bytes) to be rejected as missing:true", len(content))
	}
}

// TestAppendDoesNotLeaveEmptyFileForDirectorySource is the regression test
// for finding 4 (folded into this bead): a directory handed in as
// proof.image must fail open without leaving a stray empty file in assets/.
func TestAppendDoesNotLeaveEmptyFileForDirectorySource(t *testing.T) {
	dir := withReportsDir(t)
	repo := initGitRepo(t)

	dirAsImage := t.TempDir()

	p := minimalPayload(repo)
	p.Proof = []ProofItem{{"image": dirAsImage}}
	entry, err := Append(p)
	if err != nil {
		t.Fatalf("Append should fail open, got error: %v", err)
	}
	missing, _ := entry.Proof[0]["missing"].(bool)
	if !missing {
		t.Fatalf("expected directory source to be rejected as missing:true, got %v", entry.Proof[0])
	}
	assetsDir := filepath.Join(dir, filepath.Base(repo), "assets")
	if des, _ := os.ReadDir(assetsDir); len(des) != 0 {
		t.Fatalf("expected no leftover file in assets/, found: %v", des)
	}
}

// TestAppendTruncatesOverlongProofText is the write-side regression test for
// finding 3: an unbounded proof.out must not reach disk unbounded.
func TestAppendTruncatesOverlongProofText(t *testing.T) {
	withReportsDir(t)
	repo := initGitRepo(t)

	huge := strings.Repeat("x", maxProofTextBytes+1000)
	p := minimalPayload(repo)
	p.Proof = []ProofItem{{"cmd": "go test ./...", "out": huge}}
	entry, err := Append(p)
	if err != nil {
		t.Fatalf("Append: %v", err)
	}
	out, ok := entry.Proof[0]["out"].(string)
	if !ok {
		t.Fatalf("expected out to be a string, got %v", entry.Proof[0]["out"])
	}
	if len(out) > maxProofTextBytes+len(proofTruncationMarker) {
		t.Fatalf("out not truncated, got %d bytes", len(out))
	}
	if !strings.Contains(out, proofTruncationMarker) {
		t.Fatalf("expected truncation marker in out, got suffix %q", out[len(out)-60:])
	}
	if cmd, _ := entry.Proof[0]["cmd"].(string); cmd != "go test ./..." {
		t.Fatalf("short cmd field must not be touched, got %q", cmd)
	}
}

func TestAppendMissingImageFailsOpen(t *testing.T) {
	withReportsDir(t)
	repo := initGitRepo(t)

	p := minimalPayload(repo)
	p.Proof = []ProofItem{{"image": "/tmp/does-not-exist-demo-axk.png"}}
	entry, err := Append(p)
	if err != nil {
		t.Fatalf("Append should fail open, got error: %v", err)
	}
	if len(entry.Proof) != 1 {
		t.Fatalf("expected 1 proof item, got %d", len(entry.Proof))
	}
	missing, _ := entry.Proof[0]["missing"].(bool)
	if !missing {
		t.Fatalf("expected missing:true, got %v", entry.Proof[0])
	}
}

func TestAppendKeepsCmdAndFileProofUntouched(t *testing.T) {
	withReportsDir(t)
	repo := initGitRepo(t)

	p := minimalPayload(repo)
	p.Proof = []ProofItem{
		{"cmd": "go test ./...", "out": "ok"},
		{"file": "store.go:42"},
	}
	entry, err := Append(p)
	if err != nil {
		t.Fatalf("Append: %v", err)
	}
	if entry.Proof[0]["cmd"] != "go test ./..." || entry.Proof[0]["out"] != "ok" {
		t.Fatalf("cmd proof altered: %v", entry.Proof[0])
	}
	if entry.Proof[1]["file"] != "store.go:42" {
		t.Fatalf("file proof altered: %v", entry.Proof[1])
	}
}

// writeInRepo creates <repo>/<rel> with the given content and returns nothing —
// the tests below always refer to it by its repo-relative path, because that is
// the form every file proof in the live store uses.
func writeInRepo(t *testing.T, repo, rel, content string) string {
	t.Helper()
	abs := filepath.Join(repo, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return abs
}

func onlyProof(t *testing.T, repo string, proof ProofItem) ProofItem {
	t.Helper()
	p := minimalPayload(repo)
	p.Proof = []ProofItem{proof}
	entry, err := Append(p)
	if err != nil {
		t.Fatalf("Append: %v", err)
	}
	if len(entry.Proof) != 1 {
		t.Fatalf("expected 1 proof item, got %d", len(entry.Proof))
	}
	return entry.Proof[0]
}

func assertMissing(t *testing.T, item ProofItem, want bool) {
	t.Helper()
	got, _ := item["missing"].(bool)
	if got != want {
		t.Fatalf("missing = %v, want %v (%v)", got, want, item)
	}
}

// assertReason checks that a rejected proof records WHY it was rejected.
// Substring, not equality: the wording of the underlying error is not the
// contract — that the concrete cause reaches the caller is.
func assertReason(t *testing.T, item ProofItem, want string) {
	t.Helper()
	got, _ := item["reason"].(string)
	if !strings.Contains(got, want) {
		t.Fatalf("reason = %q, want a text containing %q (%v)", got, want, item)
	}
}

// TestAppendRecordsReasonForEveryMissingProof covers all three places that set
// "missing": the image branch in resolveProofs and both branches in
// resolveFileProof.
//
// Why the store keeps the cause and not just the verdict: the loudest consumer
// is not the view but closeops.MeldungBelegVerschwunden, the refusal handed to
// the closing agent. Without a reason it has to list all four possible causes
// ("not there, empty, too large or not a recognised image format") and let the
// agent guess which one it hit. Counted over the live store on 2026-09-14: 186
// file proofs and 147 image proofs, not one of them marked missing — so this is
// prospective, but it costs one assignment.
func TestAppendRecordsReasonForEveryMissingProof(t *testing.T) {
	withReportsDir(t)
	repo := initGitRepo(t)
	writeInRepo(t, repo, "docs/leer.dot", "")
	writeInRepo(t, repo, "docs/kein-bild.png", "das ist Text, kein PNG")

	cases := []struct {
		name  string
		proof ProofItem
		want  string
	}{
		// resolveProofs, image branch — source cannot be opened at all
		{"bild nicht da", ProofItem{"image": "/gibt/es/nicht/schirm.png"}, "schirm.png"},
		// resolveProofs, image branch — source is there but is not an image
		{"kein bildformat", ProofItem{"image": filepath.Join(repo, "docs", "kein-bild.png")},
			"kein erkanntes Bildformat"},
		// resolveFileProof, findFileProof branch
		{"datei nicht auffindbar", ProofItem{"file": "docs/gibt-es-nicht.svg"},
			"unter keinem der Bezugsverzeichnisse"},
		// resolveFileProof, copyArtefactFile branch
		{"artefakt leer", ProofItem{"file": "docs/leer.dot"}, "ist leer"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			item := onlyProof(t, repo, c.proof)
			assertMissing(t, item, true)
			assertReason(t, item, c.want)
		})
	}
}

// TestAppendRecordsNoReasonForIntactProof: "reason" appears only next to
// "missing". A key on a healthy proof would be read as a complaint.
func TestAppendRecordsNoReasonForIntactProof(t *testing.T) {
	withReportsDir(t)
	repo := initGitRepo(t)
	writeInRepo(t, repo, "docs/plan.dot", "digraph { a -> b }")

	for _, p := range []ProofItem{
		{"file": "docs/plan.dot"},
		{"file": "f.txt:1"},
		{"cmd": "go test ./...", "out": "ok"},
	} {
		item := onlyProof(t, repo, p)
		assertMissing(t, item, false)
		if _, ok := item["reason"]; ok {
			t.Fatalf("intact proof carries a reason: %v", item)
		}
	}
}

// TestAppendMarksUnresolvableFileProofMissing is gkp-2ln's red test: naming a
// file that is not there used to pass the close gate on the strength of its
// extension alone.
func TestAppendMarksUnresolvableFileProofMissing(t *testing.T) {
	withReportsDir(t)
	repo := initGitRepo(t)

	item := onlyProof(t, repo, ProofItem{"file": "docs/gibt-es-nicht.svg"})
	assertMissing(t, item, true)
	// The path text itself is never rewritten: it is what a human reads in the
	// view and types into an editor.
	if item["file"] != "docs/gibt-es-nicht.svg" {
		t.Fatalf("file proof text altered: %v", item)
	}
}

// TestAppendArchivesGraphArtefact: a .dot/.svg carries the close gate, so it is
// copied like an image — a proof that only points at the working tree decays.
func TestAppendArchivesGraphArtefact(t *testing.T) {
	dir := withReportsDir(t)
	repo := initGitRepo(t)
	writeInRepo(t, repo, "docs/plan.dot", "digraph { a -> b }")

	item := onlyProof(t, repo, ProofItem{"file": "docs/plan.dot:3"})
	assertMissing(t, item, false)
	if item["file"] != "docs/plan.dot:3" {
		t.Fatalf("file proof text altered: %v", item)
	}
	copyPath, ok := item["copy"].(string)
	if !ok {
		t.Fatalf("no archived copy recorded: %v", item)
	}
	wantDir := filepath.Join(dir, filepath.Base(repo), "artefacts")
	if !strings.HasPrefix(copyPath, wantDir) {
		t.Fatalf("artefact not archived under %q: %q", wantDir, copyPath)
	}
	if !strings.HasSuffix(copyPath, ".dot") {
		t.Fatalf("archived copy lost its extension: %q", copyPath)
	}
	content, err := os.ReadFile(copyPath)
	if err != nil {
		t.Fatalf("read archived artefact: %v", err)
	}
	if string(content) != "digraph { a -> b }" {
		t.Fatalf("archived content mismatch: %q", content)
	}
}

// TestAppendResolvesFileProofFromSubdirectory answers the first of gkp-2ln's
// three questions in a test: a relative proof resolves against the caller's cwd
// AND against the worktree root that cwd sits in. An agent working in
// internal/mcp writes "docs/plan.dot" and means the repo's docs/.
func TestAppendResolvesFileProofFromSubdirectory(t *testing.T) {
	withReportsDir(t)
	repo := initGitRepo(t)
	writeInRepo(t, repo, "docs/plan.dot", "digraph {}")
	writeInRepo(t, repo, "internal/mcp/beads_tools.go", "package mcp")

	sub := filepath.Join(repo, "internal", "mcp")

	// repo-relative: only the worktree root resolves it
	assertMissing(t, onlyProof(t, sub, ProofItem{"file": "docs/plan.dot"}), false)
	// cwd-relative: only cwd resolves it
	assertMissing(t, onlyProof(t, sub, ProofItem{"file": "beads_tools.go:1"}), false)
	// neither
	assertMissing(t, onlyProof(t, sub, ProofItem{"file": "docs/erfunden.dot"}), true)
}

// TestAppendResolvesAbsoluteFileProof: an absolute path needs no base at all,
// and an absolute path that is gone is still gone.
func TestAppendResolvesAbsoluteFileProof(t *testing.T) {
	withReportsDir(t)
	repo := initGitRepo(t)
	abs := writeInRepo(t, t.TempDir(), "aussen/plan.svg", "<svg/>")

	assertMissing(t, onlyProof(t, repo, ProofItem{"file": abs}), false)
	assertMissing(t, onlyProof(t, repo, ProofItem{"file": abs + ".weg"}), true)
}

// TestAppendChecksNonArtefactFileProofsToo: every file proof is resolved, not
// only the two extensions the gate looks at — a code reference that points
// nowhere is worth just as little. Only the artefact is archived, though:
// copying every referenced source file would turn the store into a second
// checkout.
func TestAppendChecksNonArtefactFileProofsToo(t *testing.T) {
	withReportsDir(t)
	repo := initGitRepo(t)

	found := onlyProof(t, repo, ProofItem{"file": "f.txt:1"})
	assertMissing(t, found, false)
	if _, archived := found["copy"]; archived {
		t.Fatalf("non-artefact file proof must not be archived: %v", found)
	}
	assertMissing(t, onlyProof(t, repo, ProofItem{"file": "nie-dagewesen.go:1"}), true)
}

// TestAppendMarksEmptyGraphArtefactMissing: `touch docs/plan.dot` is the
// cheapest possible forgery of a diagram, and existence alone would accept it.
func TestAppendMarksEmptyGraphArtefactMissing(t *testing.T) {
	withReportsDir(t)
	repo := initGitRepo(t)
	writeInRepo(t, repo, "docs/leer.dot", "")

	assertMissing(t, onlyProof(t, repo, ProofItem{"file": "docs/leer.dot"}), true)
}

// TestAppendMarksOversizedGraphArtefactMissing mirrors the image rule: above
// the limit the source is treated like a missing one instead of being copied
// into the store.
func TestAppendMarksOversizedGraphArtefactMissing(t *testing.T) {
	withReportsDir(t)
	repo := initGitRepo(t)
	writeInRepo(t, repo, "docs/riesig.svg", strings.Repeat("x", maxArtefactBytes+1))

	assertMissing(t, onlyProof(t, repo, ProofItem{"file": "docs/riesig.svg"}), true)
}

// TestAppendMarksDirectoryFileProofMissing: a directory named "docs/x.dot"
// would otherwise pass an existence check.
func TestAppendMarksDirectoryFileProofMissing(t *testing.T) {
	dir := withReportsDir(t)
	repo := initGitRepo(t)
	if err := os.MkdirAll(filepath.Join(repo, "docs", "tarn.dot"), 0o755); err != nil {
		t.Fatal(err)
	}

	assertMissing(t, onlyProof(t, repo, ProofItem{"file": "docs/tarn.dot"}), true)
	// demo-axk.1 finding 4, applied to artefacts: a failed copy leaves
	// nothing behind.
	entries, err := os.ReadDir(filepath.Join(dir, filepath.Base(repo), "artefacts"))
	if err == nil && len(entries) > 0 {
		t.Fatalf("failed artefact copy left %d file(s) behind", len(entries))
	}
}

func TestAppendSupersedesIsCarriedNotResolved(t *testing.T) {
	withReportsDir(t)
	repo := initGitRepo(t)

	p1 := minimalPayload(repo)
	first, err := Append(p1)
	if err != nil {
		t.Fatalf("Append 1: %v", err)
	}

	p2 := minimalPayload(repo)
	p2.Title = "reports-store gebaut (Korrektur)"
	p2.Supersedes = first.ID
	second, err := Append(p2)
	if err != nil {
		t.Fatalf("Append 2: %v", err)
	}
	if second.Supersedes != first.ID {
		t.Fatalf("supersedes = %q, want %q", second.Supersedes, first.ID)
	}

	entries, skipped, err := LoadMap(filepath.Base(repo), "demo-axk")
	if err != nil {
		t.Fatalf("LoadMap: %v", err)
	}
	if skipped != 0 {
		t.Fatalf("expected 0 skipped lines, got %d", skipped)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	// newest-first
	if entries[0].ID != second.ID || entries[1].ID != first.ID {
		t.Fatalf("expected newest-first order, got %v", entries)
	}
	// the superseded entry itself is untouched
	if entries[1].Title != "reports-store gebaut" || entries[1].Supersedes != "" {
		t.Fatalf("superseded entry was mutated: %+v", entries[1])
	}
}

func TestLoadMapNoFileReturnsEmpty(t *testing.T) {
	withReportsDir(t)
	entries, skipped, err := LoadMap("no-such-repo", "no-such-map")
	if err != nil {
		t.Fatalf("LoadMap: %v", err)
	}
	if skipped != 0 {
		t.Fatalf("expected 0 skipped lines, got %d", skipped)
	}
	if len(entries) != 0 {
		t.Fatalf("expected empty slice, got %v", entries)
	}
}

// TestLoadMapReadsWhatAppendWrote pins the two halves of the store to the same
// file name. Append has always guarded its map name with filepath.Base; LoadMap
// did not, so the two disagreed for any name containing a separator (gkp-bq9).
//
// The disagreement is not cosmetic. It is the close tool's re-read that goes
// through LoadMap: a map name that writes to one path and reads from another
// means the gate cannot see the evidence it just wrote — and the read side,
// unguarded, could leave the repo's own directory while doing it.
func TestLoadMapReadsWhatAppendWrote(t *testing.T) {
	base := withReportsDir(t)
	cwd := initGitRepo(t)

	p := minimalPayload(cwd)
	p.Map = "../flucht"
	entry, err := Append(p)
	if err != nil {
		t.Fatalf("Append: %v", err)
	}

	// Nothing may be written above the repo's own directory.
	if _, err := os.Stat(filepath.Join(base, "flucht.jsonl")); err == nil {
		t.Errorf("Append schrieb nach %s — ausserhalb des Repo-Verzeichnisses", base)
	}

	entries, _, err := LoadMap(entry.Repo, p.Map)
	if err != nil {
		t.Fatalf("LoadMap: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("LoadMap fand %d Einträge, Append schrieb 1 — Schreib- und Lesepfad weichen ab", len(entries))
	}
}

func TestListMapsAcrossMultipleMaps(t *testing.T) {
	withReportsDir(t)
	repo := initGitRepo(t)

	p1 := minimalPayload(repo)
	p1.Map = "demo-axk"
	if _, err := Append(p1); err != nil {
		t.Fatalf("Append map1: %v", err)
	}

	p2 := minimalPayload(repo)
	p2.Map = "demo-zzz"
	p2.Bead = "demo-zzz.2"
	if _, err := Append(p2); err != nil {
		t.Fatalf("Append map2: %v", err)
	}

	maps, skipped, err := ListMaps(filepath.Base(repo))
	if err != nil {
		t.Fatalf("ListMaps: %v", err)
	}
	if skipped != 0 {
		t.Fatalf("expected 0 skipped lines, got %d", skipped)
	}
	if len(maps) != 2 {
		t.Fatalf("expected 2 maps, got %d: %v", len(maps), maps)
	}
	if len(maps["demo-axk"]) != 1 || len(maps["demo-zzz"]) != 1 {
		t.Fatalf("unexpected map contents: %v", maps)
	}
}

// TestDeriveGitSanitizesTraversalCwd is the regression test for demo-axk.1
// finding 1. It mirrors the review's live repro: git unavailable (empty
// PATH) so deriveGit falls back to the raw cwd, and cwd is crafted so that,
// pre-fix, filepath.Base(cwd) resolves to "..".
func TestDeriveGitSanitizesTraversalCwd(t *testing.T) {
	base := withReportsDir(t)
	t.Setenv("PATH", "") // git becomes unresolvable -> deriveGit falls back to raw cwd

	nonGitDir := t.TempDir()
	cases := map[string]string{
		"absolute-cwd-ending-in-dotdot": nonGitDir + "/..", // review's exact live repro
		"relative-dotdot-cwd":           "..",
	}
	for name, evilCwd := range cases {
		t.Run(name, func(t *testing.T) {
			entry, err := Append(minimalPayload(evilCwd))
			if err != nil {
				t.Fatalf("Append: %v", err)
			}
			if entry.Repo == ".." || entry.Repo == "." || entry.Repo == "" || strings.ContainsAny(entry.Repo, `/\`) {
				t.Fatalf("unsafe repo derived from cwd %q: %q", evilCwd, entry.Repo)
			}
			dir := filepath.Join(base, entry.Repo)
			rel, err := filepath.Rel(base, dir)
			if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				t.Fatalf("repo dir escapes BaseDir: repo=%q rel=%q err=%v", entry.Repo, rel, err)
			}
		})
	}
}

// TestLoadMapSkipsBadLinesButKeepsGoodOnes is the required case for finding
// 3: good line / broken (oversized) line / good line — both good lines must
// still come back, and the skip counter must be exactly 1.
func TestLoadMapSkipsBadLinesButKeepsGoodOnes(t *testing.T) {
	dir := withReportsDir(t)
	repo := initGitRepo(t)

	p1 := minimalPayload(repo)
	p1.Title = "erste gute Zeile"
	first, err := Append(p1)
	if err != nil {
		t.Fatalf("Append 1: %v", err)
	}

	p2 := minimalPayload(repo)
	p2.Title = "dritte gute Zeile"
	third, err := Append(p2)
	if err != nil {
		t.Fatalf("Append 2: %v", err)
	}

	fpath := filepath.Join(dir, filepath.Base(repo), "demo-axk.jsonl")
	raw, err := os.ReadFile(fpath)
	if err != nil {
		t.Fatalf("read jsonl: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines before splice, got %d", len(lines))
	}
	broken := strings.Repeat("x", maxLineBytes+1000)
	spliced := lines[0] + "\n" + broken + "\n" + lines[1] + "\n"
	if err := os.WriteFile(fpath, []byte(spliced), 0o644); err != nil {
		t.Fatalf("write spliced jsonl: %v", err)
	}

	entries, skipped, err := LoadMap(filepath.Base(repo), "demo-axk")
	if err != nil {
		t.Fatalf("LoadMap: %v", err)
	}
	if skipped != 1 {
		t.Fatalf("expected 1 skipped line, got %d", skipped)
	}
	if len(entries) != 2 {
		t.Fatalf("expected both good lines to survive, got %d: %v", len(entries), entries)
	}
	ids := map[string]bool{entries[0].ID: true, entries[1].ID: true}
	if !ids[first.ID] || !ids[third.ID] {
		t.Fatalf("expected entries for %q and %q, got %v", first.ID, third.ID, entries)
	}
}

func TestIDsAreUniqueAndTimeSortable(t *testing.T) {
	withReportsDir(t)
	repo := initGitRepo(t)

	p := minimalPayload(repo)
	e1, err := Append(p)
	if err != nil {
		t.Fatalf("Append 1: %v", err)
	}
	e2, err := Append(p)
	if err != nil {
		t.Fatalf("Append 2: %v", err)
	}
	if e1.ID == e2.ID {
		t.Fatalf("expected unique ids, got duplicate %q", e1.ID)
	}
	// Compare only the timestamp prefix (before the random suffix) — the
	// suffix itself is random and not meant to be ordered.
	ts1 := e1.ID[:strings.LastIndex(e1.ID, "-")]
	ts2 := e2.ID[:strings.LastIndex(e2.ID, "-")]
	if ts1 > ts2 {
		t.Fatalf("expected non-decreasing timestamps: %q > %q", ts1, ts2)
	}
}

// TestLoadBeadNoFilesReturnsEmpty mirrors TestLoadMapNoFileReturnsEmpty: an
// unknown repo is not an error, just an empty answer.
func TestLoadBeadNoFilesReturnsEmpty(t *testing.T) {
	withReportsDir(t)
	entries, skipped, err := LoadBead("no-such-repo", "no-such-bead")
	if err != nil {
		t.Fatalf("LoadBead: %v", err)
	}
	if skipped != 0 {
		t.Fatalf("expected 0 skipped lines, got %d", skipped)
	}
	if len(entries) != 0 {
		t.Fatalf("expected empty slice, got %v", entries)
	}
}

// TestLoadBeadMergesAcrossMapsNewestFirst pins the reason LoadBead exists at
// all: a bead's entries can be scattered across several map files (an MCP
// payload's "map" field can differ per call), and LoadMap alone only ever
// sees one of them. The merge must also re-sort — each map file itself
// arrives newest-first, but interleaving two files does not.
//
// The two entries are written directly as JSONL lines (not via Append) with
// explicit, distinct "at" timestamps a full second apart: Append's own "at"
// has only whole-second resolution (time.RFC3339, no fractional part), so two
// real Append calls in the same test commonly land in the same second and
// the sort's ID tie-breaker (oldest-first, same rule as buildTimeline in
// internal/appctx/reports_view.go) would decide the order instead of "at" —
// which is a different thing than what this test means to pin down.
func TestLoadBeadMergesAcrossMapsNewestFirst(t *testing.T) {
	base := withReportsDir(t)
	bead := "demo-axk.1"
	repo := "myrepo"
	dir := filepath.Join(base, repo)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	older := Entry{ID: "id-older", At: "2026-09-16T10:00:00Z", Map: "demo-axk", Bead: bead, Kind: "plan", Title: "plan"}
	newer := Entry{ID: "id-newer", At: "2026-09-16T11:00:00Z", Map: "demo-zzz", Bead: bead, Kind: "done", Title: "fertig"}
	other := Entry{ID: "id-other", At: "2026-09-16T10:30:00Z", Map: "demo-axk", Bead: "demo-axk.2", Kind: "plan", Title: "andere Aufgabe"}
	writeJSONLLine(t, filepath.Join(dir, "demo-axk.jsonl"), older, other)
	writeJSONLLine(t, filepath.Join(dir, "demo-zzz.jsonl"), newer)

	entries, skipped, err := LoadBead(repo, bead)
	if err != nil {
		t.Fatalf("LoadBead: %v", err)
	}
	if skipped != 0 {
		t.Fatalf("expected 0 skipped lines, got %d", skipped)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries for %q, got %d: %v", bead, len(entries), entries)
	}
	if entries[0].ID != newer.ID || entries[1].ID != older.ID {
		t.Fatalf("expected newest-first [%q, %q], got [%q, %q]",
			newer.ID, older.ID, entries[0].ID, entries[1].ID)
	}
}

// writeJSONLLine appends each entry as one JSON line to path (creating any
// missing directories is the caller's job — mirrors how the real store's
// files are laid out, just without going through Append).
func writeJSONLLine(t *testing.T, path string, entries ...Entry) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for _, e := range entries {
		line, err := json.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write(append(line, '\n')); err != nil {
			t.Fatal(err)
		}
	}
}

// TestLoadBeadReturnsEntriesForBeadUnknownToBd is the real case the bead's
// description names: gkp-3sq, gkp-e58 and gkp-6am have report evidence in the
// store but no longer exist in any bd database — lost in a merge the
// append-only store survived. LoadBead never asks bd anything, so it must
// answer for such an id exactly as it would for any other: the entries are
// still there.
func TestLoadBeadReturnsEntriesForBeadUnknownToBd(t *testing.T) {
	withReportsDir(t)
	repo := initGitRepo(t)

	p := minimalPayload(repo)
	p.Bead = "gkp-3sq" // real case, no longer known to any bd database
	p.Kind = "done"
	p.Title = "Beleg ueberlebt den Datenbank-Merge"
	entry, err := Append(p)
	if err != nil {
		t.Fatalf("Append: %v", err)
	}

	entries, _, err := LoadBead(filepath.Base(repo), "gkp-3sq")
	if err != nil {
		t.Fatalf("LoadBead: %v", err)
	}
	if len(entries) != 1 || entries[0].ID != entry.ID {
		t.Fatalf("expected the one entry for a bead unknown to bd, got %v", entries)
	}
}
