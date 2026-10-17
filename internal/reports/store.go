// Package reports is the append-only report store behind the MCP 'report'
// tool (demo-axk.3) and the reports view (demo-axk.4). One entry per
// bead-status-move, one line per entry, one file per beads map:
// ~/.claude/reports/<repo>/<map>.jsonl. No rewrite of the whole file, no
// locks (O_APPEND is enough), no schema/migration — the line order is the
// timeline. Corrections are new lines with supersedes set; the superseded
// line is never touched.
package reports

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/niclasedge/git-planner-go/internal/shared"
)

// ProofItem is one proof entry: {cmd,out} | {file:"pfad:zeile"} |
// {image:"/abs/pfad.png"}. A loose map keeps the variants representable
// without a tagged union — the store only ever special-cases "image".
type ProofItem = map[string]any

// Payload is the raw ingest payload (from the MCP 'report' tool). These six
// fields are caller-supplied; id, at, repo, worktree, branch are derived by
// Append.
type Payload struct {
	Cwd        string      `json:"cwd"`
	Map        string      `json:"map"`
	Bead       string      `json:"bead"`
	Kind       string      `json:"kind"` // "plan" | "done"
	Title      string      `json:"title"`
	Impact     string      `json:"impact,omitempty"`
	Proof      []ProofItem `json:"proof,omitempty"`
	Notes      string      `json:"notes,omitempty"`
	Supersedes string      `json:"supersedes,omitempty"`
}

// Entry is one JSONL line as written to disk: Payload plus derived fields.
// Supersedes is carried through unresolved — the store never mutates or
// removes the entry it points at, a viewer resolves the chain.
type Entry struct {
	ID         string      `json:"id"`
	At         string      `json:"at"` // RFC3339 UTC
	Repo       string      `json:"repo"`
	Worktree   string      `json:"worktree"`
	Branch     string      `json:"branch"`
	Map        string      `json:"map"`
	Bead       string      `json:"bead"`
	Kind       string      `json:"kind"`
	Title      string      `json:"title"`
	Impact     string      `json:"impact,omitempty"`
	Proof      []ProofItem `json:"proof"`
	Notes      string      `json:"notes,omitempty"`
	Supersedes string      `json:"supersedes,omitempty"`
}

// BaseDir returns the reports root directory (CC_REPORTS_DIR overrides —
// needed to keep tests isolated from the real ~/.claude/reports).
func BaseDir() string {
	if env := os.Getenv("CC_REPORTS_DIR"); env != "" {
		return env
	}
	return filepath.Join(homeDir(), ".claude", "reports")
}

func homeDir() string {
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	return "/tmp"
}

// Validate reports whether a payload has the shape Append requires. Append
// calls it itself, so nothing depends on a caller remembering to — it is
// exported only so a caller can ask the question EARLY.
//
// internal/mcp needs exactly that (gkp-few). It now asks bd, before appending,
// whether the payload's map is a real epic and the bead hangs under it. That
// lookup costs a subprocess and its refusal talks about beads; a payload with a
// missing title or a "kind" of "fertig" deserves the cheap answer about the
// field, not the dear one about the card. So the order is: shape first, then
// the card, then the write.
func Validate(p Payload) error { return validate(p) }

func validate(p Payload) error {
	if strings.TrimSpace(p.Map) == "" {
		return errors.New("map ist Pflicht")
	}
	if strings.TrimSpace(p.Bead) == "" {
		return errors.New("bead ist Pflicht")
	}
	if p.Kind != "plan" && p.Kind != "done" {
		return fmt.Errorf("kind muss 'plan' oder 'done' sein, nicht %q", p.Kind)
	}
	if strings.TrimSpace(p.Title) == "" {
		return errors.New("title ist Pflicht")
	}
	// proof ist bewusst NICHT Pflicht — die Luecke rechnet die Ansicht aus.
	return nil
}

// Append validates the payload, derives id/at/repo/worktree/branch from cwd,
// resolves image and file proofs (copying what it can into the repo's assets/
// and artefacts/ dirs), and appends one JSON line to <repo>/<map>.jsonl.
func Append(p Payload) (Entry, error) {
	if err := validate(p); err != nil {
		return Entry{}, err
	}
	now := time.Now()
	repo, worktree, branch, root := deriveGit(p.Cwd)
	dir := filepath.Join(BaseDir(), repo)
	id := newID(now)

	entry := Entry{
		ID:         id,
		At:         now.UTC().Format(time.RFC3339),
		Repo:       repo,
		Worktree:   worktree,
		Branch:     branch,
		Map:        p.Map,
		Bead:       p.Bead,
		Kind:       p.Kind,
		Title:      p.Title,
		Impact:     p.Impact,
		Proof:      truncateProofText(resolveProofs(dir, id, proofBases(p.Cwd, root), p.Proof)),
		Notes:      p.Notes,
		Supersedes: p.Supersedes,
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Entry{}, err
	}
	line, err := json.Marshal(entry)
	if err != nil {
		return Entry{}, err
	}
	// filepath.Base guards against a map name that tries to path-traverse
	// out of dir; the stored entry.Map keeps the caller's original value.
	fpath := filepath.Join(dir, filepath.Base(p.Map)+".jsonl")
	f, err := os.OpenFile(fpath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return Entry{}, err
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		return Entry{}, err
	}
	return entry, nil
}

// maxImageBytes caps a single proof image (demo-axk.1 finding 2). Above
// this, the source is treated like a missing one — fail open, never abort
// the ingest — instead of copying an arbitrarily large file into assets/.
const maxImageBytes = 10 * 1024 * 1024

// imageSignatures are the magic-byte prefixes accepted for a proof image.
// The declared path or its extension decide nothing (demo-axk.1 finding
// 2, explicit product decision): screenshot tools write to unpredictable
// locations, so a path allowlist (e.g. "/tmp only") would both reject
// legitimate proof and give false confidence, since a non-image dropped
// under an allowlisted path would still pass. Only PNG/JPEG/GIF/WebP
// content is accepted; anything else — e.g. an SSH key or a text log handed
// in as "image" — is rejected the same way a missing file is.
var imageSignatures = [][]byte{
	{0x89, 0x50, 0x4E, 0x47}, // PNG
	{0xFF, 0xD8, 0xFF},       // JPEG
	{0x47, 0x49, 0x46, 0x38}, // GIF ("GIF8")
	{0x52, 0x49, 0x46, 0x46}, // RIFF container, checked further for WebP below
}

func looksLikeImage(header []byte) bool {
	for _, sig := range imageSignatures {
		if len(header) < len(sig) || !bytes.Equal(header[:len(sig)], sig) {
			continue
		}
		if sig[0] == 0x52 { // RIFF — shared with WAV etc., confirm the WEBP form tag
			if len(header) < 12 || string(header[8:12]) != "WEBP" {
				continue
			}
		}
		return true
	}
	return false
}

// resolveProofs turns every proof item's claim into a recorded verdict, once,
// at write time.
//
//   - {"image": "/abs/path"} is copied into <dir>/assets/<id>-<n>.<ext> and the
//     path rewritten in place — a screenshot left in /tmp is gone the next day.
//   - {"file": "docs/plan.dot"} is looked up against the bases (see
//     proofBases). A .dot/.svg is additionally archived under
//     <dir>/artefacts/<id>-<n>.<ext>, recorded as "copy".
//
// Fail open in both cases: a missing/unreadable source, an oversized one, or
// content that isn't what it claims never fails the write — the item is kept,
// marked "missing": true instead. That marker is the whole verdict: the reading
// side (evidence.go) never touches the filesystem again, so a proof cannot rot
// out from under a bead that was closed years ago.
func resolveProofs(dir, id string, bases []string, proof []ProofItem) []ProofItem {
	out := make([]ProofItem, len(proof))
	for i, item := range proof {
		clone := make(ProofItem, len(item)+1)
		for k, v := range item {
			clone[k] = v
		}
		if src, ok := clone["image"].(string); ok && src != "" {
			if dst, err := copyImageFile(dir, id, i, src); err == nil {
				clone["image"] = dst
			} else {
				markMissing(clone, err)
			}
		}
		if ref, ok := clone["file"].(string); ok && strings.TrimSpace(ref) != "" {
			resolveFileProof(dir, id, i, bases, ref, clone)
		}
		out[i] = clone
	}
	return out
}

// proofBases are the directories a relative file proof is resolved against, in
// order. This is gkp-2ln's first decision, and it had to be made from what is
// actually available: Entry stores repo/worktree/branch as NAMES, never a path,
// so nothing downstream could resolve a relative proof even in principle. Only
// Append sees Payload.Cwd, so only Append can answer the question at all.
//
//  1. cwd itself — the value the caller declared, the same one that already
//     decides which beads database is meant and which report file is written.
//     An agent that says "I am here" is taken at its word.
//  2. the worktree root cwd sits in (deriveGit's currentRoot) — because the
//     convention in this repo's own history is repo-relative ("docs/plan.dot",
//     "internal/collectors/git.go:281"), and an agent working in a
//     subdirectory writes exactly that.
//
// Deliberately NOT a third base: the MAIN worktree, when the report comes from
// a linked one. The card's repo is shared between worktrees, so accepting it
// would let a proof written in a worktree be satisfied by a file that only
// exists in main — a diagram the closing agent never produced. The two bases
// above can only ever resolve to something in the caller's own reach.
//
// Only absolute bases are used. An empty cwd would otherwise clean to "." and
// resolve proofs against the daemon's working directory, which is nobody's
// repo; with no base at all, a relative proof simply does not resolve and is
// marked missing — the safe side.
func proofBases(cwd, root string) []string {
	var bases []string
	for _, b := range []string{filepath.Clean(cwd), root} {
		if filepath.IsAbs(b) && !slices.Contains(bases, b) {
			bases = append(bases, b)
		}
	}
	return bases
}

// maxArtefactBytes caps a single archived .dot/.svg. Same value as
// maxImageBytes and deliberately its OWN constant: the two limits answer
// different questions (how big may a screenshot be, how big may a diagram
// source be) and changing one must not silently move the other. An SVG past
// this point is not a diagram anyone reads.
const maxArtefactBytes = 10 * 1024 * 1024

// resolveFileProof looks the reference up and records the verdict on clone.
//
// Every file proof is resolved, not only the .dot/.svg the close gate looks at:
// a code reference that points nowhere is worth just as little, and a rule that
// applied to two extensions would be the same asymmetry gkp-2ln removed, one
// level down. Only the graph artefact is COPIED, though — archiving every
// referenced source file would turn the report store into a second checkout,
// and no gate hangs off those.
//
// The content is deliberately not sniffed the way an image's magic bytes are.
// There is no honest signature for "a dot file" (strict digraph, a comment, an
// XML declaration before <svg>), and a wrong guess would reject real proof.
// Existence, regular file, non-empty and under the size cap is what is checked:
// enough to stop `touch docs/plan.dot` and every path that was never there,
// which is what the gate is for. Forging a plausible diagram remains possible
// and remains work — the gate buys a human a glance, not a proof of honesty.
func resolveFileProof(dir, id string, idx int, bases []string, ref string, clone ProofItem) {
	src, err := findFileProof(bases, proofFilePath(ref))
	if err != nil {
		markMissing(clone, err)
		return
	}
	if !isGraphArtefact(ref) {
		return
	}
	if dst, err := copyArtefactFile(dir, id, idx, src); err == nil {
		clone["copy"] = dst
	} else {
		markMissing(clone, err)
	}
}

// markMissing records the verdict AND what produced it.
//
// "missing" stays the verdict — it is the only key the reading side
// (evidence.go: HasVisualProof, HasBrokenVisualProof) consults, and adding
// "reason" deliberately changes nothing there. The reason travels one way only:
// into closeops.MeldungBelegVerschwunden, the refusal the closing agent reads.
// Without it that message has to recite all four possible causes and let the
// agent guess which one it hit; with it, it names the one.
//
// NOT shown in the view. Standing product decision: a missing file proof is
// displayed as plainly as a missing image, and without a justification. The
// view's rpProofHtml reads image/file/missing/cmd by name and never walks the
// keys, so this field stays invisible there on its own.
func markMissing(clone ProofItem, err error) {
	clone["missing"] = true
	if err != nil {
		clone["reason"] = err.Error()
	}
}

// findFileProof returns the first base under which rel is a regular file. An
// absolute rel needs no base and is checked as given.
func findFileProof(bases []string, rel string) (string, error) {
	if rel == "" {
		return "", errors.New("leerer Dateiverweis")
	}
	candidates := []string{rel}
	if !filepath.IsAbs(rel) {
		candidates = candidates[:0]
		for _, b := range bases {
			candidates = append(candidates, filepath.Join(b, rel))
		}
	}
	for _, c := range candidates {
		if info, err := os.Stat(c); err == nil && info.Mode().IsRegular() {
			return c, nil
		}
	}
	return "", fmt.Errorf("%s ist unter keinem der Bezugsverzeichnisse eine Datei", rel)
}

// copyArtefactFile archives a .dot/.svg next to — not inside — assets/.
//
// The separate directory is not cosmetic: internal/reportassets serves
// <repo>/assets/<file> and sniffs magic bytes, so it would 404 an SVG anyway,
// and an assets/ that holds things the asset route does not serve invites
// someone to "fix" that. An inline-served SVG is a script host; artefacts/ is
// an archive, read by nothing but a human with a file manager.
func copyArtefactFile(dir, id string, idx int, src string) (string, error) {
	info, err := os.Stat(src)
	if err != nil {
		return "", err
	}
	if info.Size() == 0 {
		return "", fmt.Errorf("%s ist leer", src)
	}
	if info.Size() > maxArtefactBytes {
		return "", fmt.Errorf("%s überschreitet die Größengrenze von %d Byte", src, maxArtefactBytes)
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return "", err
	}
	artefactsDir := filepath.Join(dir, "artefacts")
	if err := os.MkdirAll(artefactsDir, 0o755); err != nil {
		return "", err
	}
	dst := filepath.Join(artefactsDir, fmt.Sprintf("%s-%d%s", id, idx, filepath.Ext(src)))
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		os.Remove(dst) // finding 4: never leave a partial file behind on failure
		return "", err
	}
	return dst, nil
}

func copyImageFile(dir, id string, idx int, src string) (string, error) {
	in, err := os.Open(src)
	if err != nil {
		return "", err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return "", err
	}
	// Reject directories (and anything else non-regular) before creating a
	// destination file — demo-axk.1 finding 4: no empty file left behind
	// in assets/ for a source that was never going to copy.
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%s ist keine reguläre Datei", src)
	}
	if info.Size() > maxImageBytes {
		return "", fmt.Errorf("%s überschreitet die Größengrenze von %d Byte", src, maxImageBytes)
	}
	header := make([]byte, 12)
	n, _ := io.ReadFull(in, header)
	if !looksLikeImage(header[:n]) {
		return "", fmt.Errorf("%s ist kein erkanntes Bildformat (PNG/JPEG/GIF/WebP)", src)
	}
	if _, err := in.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	assetsDir := filepath.Join(dir, "assets")
	if err := os.MkdirAll(assetsDir, 0o755); err != nil {
		return "", err
	}
	dst := filepath.Join(assetsDir, fmt.Sprintf("%s-%d%s", id, idx, filepath.Ext(src)))
	out, err := os.Create(dst)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(dst) // finding 4: never leave a partial/empty file behind on failure
		return "", err
	}
	if err := out.Close(); err != nil {
		os.Remove(dst)
		return "", err
	}
	return dst, nil
}

// maxProofTextBytes caps proof.cmd/proof.out at write time (demo-axk.1
// finding 3), mirroring the gitDiffMax pattern in collectors/git.go: an
// unbounded "out" (e.g. a verbose build log) can otherwise produce a JSONL
// line long enough to trip the reader's line-length guard (maxLineBytes in
// readEntries) and take a whole map's history down with it.
const maxProofTextBytes = 256 * 1024

const proofTruncationMarker = "\n...[gekürzt, Grenze 256 KiB überschritten]"

// truncateProofText caps the "cmd" and "out" string fields of each proof
// item in place. proof is already a fresh clone (from resolveProofs), so
// mutating it here does not touch the caller's payload.
func truncateProofText(proof []ProofItem) []ProofItem {
	for _, item := range proof {
		for _, key := range []string{"cmd", "out"} {
			if s, ok := item[key].(string); ok && len(s) > maxProofTextBytes {
				item[key] = s[:maxProofTextBytes] + proofTruncationMarker
			}
		}
	}
	return proof
}

// newID returns a time-sortable id: fixed-width UTC nanosecond timestamp plus
// a short random suffix (stdlib only, no ULID lib).
func newID(t time.Time) string {
	return t.UTC().Format("20060102T150405.000000000") + "-" + randHex(4)
}

func randHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

func gitOut(cwd string, args ...string) (string, bool) {
	return shared.Run(append([]string{"git", "-C", cwd}, args...), shared.RunOptions{Cwd: cwd, TimeoutMs: shared.GitTimeoutMs})
}

// deriveGit resolves repo (the main worktree's directory name — stable across
// all of a repo's worktrees, so reports from a linked worktree land in the
// same repo folder), worktree ("main" or the linked worktree's directory
// name), branch, and root (the absolute path of the worktree cwd sits in —
// currentRoot, which proofBases needs and no name can substitute for) from cwd.
// Best effort: any git failure falls back to non-git defaults, never blocking
// the write (fail open).
func deriveGit(cwd string) (repo, worktree, branch, root string) {
	// demo-axk.1 finding 1: cwd is caller-supplied (from .3's MCP payload)
	// and is the fallback for both currentRoot and mainRoot when git fails
	// (non-git cwd, git missing). filepath.Base does NOT resolve "..", so an
	// uncleaned cwd ending in ".." would derive repo == ".." and let
	// filepath.Join(BaseDir(), repo) escape the reports root. Clean it before
	// any use, and never use it (or anything derived from it) for git -C.
	cwd = filepath.Clean(cwd)
	currentRoot := cwd
	if top, ok := gitOut(cwd, "rev-parse", "--show-toplevel"); ok {
		currentRoot = filepath.Clean(strings.TrimSpace(top))
	}
	mainRoot := currentRoot
	if wtOut, ok := gitOut(cwd, "worktree", "list", "--porcelain"); ok {
		if first := firstWorktreePath(wtOut); first != "" {
			mainRoot = filepath.Clean(first)
		}
	}
	repo = sanitizePathComponent(filepath.Base(mainRoot))
	if currentRoot == mainRoot {
		worktree = "main"
	} else {
		worktree = sanitizePathComponent(filepath.Base(currentRoot))
	}
	if b, ok := gitOut(cwd, "rev-parse", "--abbrev-ref", "HEAD"); ok {
		branch = strings.TrimSpace(b)
	}
	return repo, worktree, branch, currentRoot
}

// sanitizePathComponent guards a value about to be used as a single path
// segment under BaseDir() (repo, worktree). Clean()ing cwd upstream resolves
// the common ".." traversal, but this is the explicit backstop: "", ".",
// "..", or anything containing a path separator falls back to a neutral
// placeholder instead of being joined into a filesystem path raw.
func sanitizePathComponent(s string) string {
	if s == "" || s == "." || s == ".." || strings.ContainsAny(s, `/\`) {
		return "unknown"
	}
	return s
}

func firstWorktreePath(porcelain string) string {
	for _, line := range strings.Split(porcelain, "\n") {
		if p, ok := strings.CutPrefix(line, "worktree "); ok {
			return strings.TrimSpace(p)
		}
	}
	return ""
}

// LoadMap reads all entries for one repo/map, newest-first. A missing file is
// not an error — the map just has no entries yet. The second return value is
// the number of lines skipped (corrupt JSON or over maxLineBytes) — surfaced
// so the view can report it instead of silently dropping history.
//
// filepath.Base mirrors Append's guard on the same value (gkp-bq9). Both ends
// have to agree on the file name, and they did not: a map name carrying a
// separator wrote to <repo>/<base>.jsonl but read from a path that could leave
// <repo> entirely. That matters most for the close gate, whose whole point is
// to re-read what it just wrote — pointed at a different file, it would find no
// evidence for evidence it had just stored.
func LoadMap(repo, mapName string) ([]Entry, int, error) {
	return readEntries(filepath.Join(BaseDir(), repo, filepath.Base(mapName)+".jsonl"))
}

// ListMaps loads every map's entries (newest-first) for a repo, keyed by map
// name, plus the total number of lines skipped across all of the repo's
// maps. The reports view (demo-axk.4) builds the repo -> map -> entries
// tree on top of this.
func ListMaps(repo string) (map[string][]Entry, int, error) {
	dir := filepath.Join(BaseDir(), repo)
	result := map[string][]Entry{}
	totalSkipped := 0
	files, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return result, 0, nil
		}
		return nil, 0, err
	}
	for _, f := range files {
		if f.IsDir() || !strings.HasSuffix(f.Name(), ".jsonl") {
			continue
		}
		mapName := strings.TrimSuffix(f.Name(), ".jsonl")
		entries, skipped, err := readEntries(filepath.Join(dir, f.Name()))
		if err != nil {
			return nil, totalSkipped, err
		}
		result[mapName] = entries
		totalSkipped += skipped
	}
	return result, totalSkipped, nil
}

// LoadBead reads every entry for one bead ACROSS all of the repo's maps,
// newest-first, with the same fail-open discipline as LoadMap/ListMaps — the
// second return value is the number of lines skipped across every map file
// read along the way (gkp-e9h, for the /belege route).
//
// bd is never consulted here, and that is the point: a bead's report
// evidence can outlive the bead itself. gkp-3sq, gkp-e58 and gkp-6am are real
// cases — they were lost in a bd database merge, but the append-only store
// still holds their entries. "no entries" and "bead unknown to bd" look
// identical from here; deciding what that means is the caller's job, not
// this function's.
func LoadBead(repo, bead string) ([]Entry, int, error) {
	maps, skipped, err := ListMaps(repo)
	if err != nil {
		return nil, skipped, err
	}
	out := []Entry{}
	for _, entries := range maps {
		for _, e := range entries {
			if e.Bead == bead {
				out = append(out, e)
			}
		}
	}
	// Each map's own slice arrives newest-first (ListMaps/readEntries), but
	// merging entries across several maps needs a re-sort — same reasoning as
	// buildTimeline in internal/appctx/reports_view.go. Entry.At is RFC3339 UTC
	// with fixed-width fields, so a string compare is a chronological one; ID
	// breaks ties between entries written in the same second.
	sort.Slice(out, func(i, j int) bool {
		if out[i].At != out[j].At {
			return out[i].At > out[j].At
		}
		return out[i].ID < out[j].ID
	})
	return out, skipped, nil
}

// maxLineBytes bounds a single JSONL line during read (demo-axk.1
// finding 3). It is a corruption guard, not the normal-case limit — normal
// lines stay far below this because proof text is capped at write time
// (maxProofTextBytes, truncateProofText). A line at or over this bound is
// skipped, not treated as a reason to give up on the rest of the file: the
// previous bufio.Scanner-based reader hit bufio.ErrTooLong on such a line
// and returned an error for the WHOLE file, silently losing every
// surrounding good line.
const maxLineBytes = 8 * 1024 * 1024

func readEntries(path string) ([]Entry, int, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []Entry{}, 0, nil
		}
		return nil, 0, err
	}
	defer f.Close()

	entries := []Entry{}
	skipped := 0
	r := bufio.NewReaderSize(f, 64*1024)
	for {
		raw, tooLong, readErr := readBoundedLine(r, maxLineBytes)
		switch {
		case tooLong:
			skipped++
		default:
			if line := strings.TrimSpace(string(raw)); line != "" {
				var e Entry
				if json.Unmarshal([]byte(line), &e) != nil {
					skipped++ // corrupt line: skip, fail open
				} else {
					entries = append(entries, e)
				}
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			return nil, skipped, readErr
		}
	}
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}
	return entries, skipped, nil
}

// readBoundedLine reads one '\n'-terminated line from r, delimiter
// stripped. If the line would exceed max bytes, it stops accumulating
// (bounding memory to roughly one internal buffer's worth over max) and
// keeps reading — discarding — up to the next newline or EOF, reporting
// tooLong=true instead of the oversized content. This is what lets
// readEntries resume at the next line instead of aborting the whole file.
func readBoundedLine(r *bufio.Reader, max int) (line []byte, tooLong bool, err error) {
	for {
		part, e := r.ReadSlice('\n')
		if !tooLong {
			if len(line)+len(part) > max {
				tooLong = true
				line = nil
			} else {
				line = append(line, part...)
			}
		}
		switch {
		case e == nil:
			// part ends with '\n' — line complete.
			if tooLong {
				return nil, true, nil
			}
			return bytes.TrimSuffix(line, []byte("\n")), false, nil
		case e == bufio.ErrBufferFull:
			continue // line longer than the buffer; keep reading the same line
		default:
			// io.EOF or a real read error: part is the trailing data without
			// a delimiter.
			if tooLong {
				return nil, true, e
			}
			if len(line) == 0 {
				return nil, false, e
			}
			return line, false, e
		}
	}
}
