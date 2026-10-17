// Package reportassets serves the proof-image bytes that
// internal/reports.Append (demo-axk.1) copies into
// ~/.claude/reports/<repo>/assets/. Wired at GET /reports/<repo>/assets/
// <filename> in internal/server/server.go (demo-axk.8) — same shape as
// internal/mcp: its own package, one route line in server.go.
//
// internal/reports is frozen for this bead (owned by demo-axk.1, review
// already closed) — read-only. Its sanitizePathComponent and imageSignatures/
// looksLikeImage helpers are unexported, so the guards below are deliberate,
// commented duplicates rather than shared code. They also aren't identical
// in behavior, because this package sits on the opposite side of the ingest
// guard:
//
//   - sanitizePathComponent (store.go) is a WRITE-side guard: Append must
//     never fail an ingest, so an invalid repo/worktree component is
//     replaced with a neutral "unknown" placeholder rather than rejected.
//     sanitizeSegment below is the READ-side mirror of the same rule, but
//     "fail open" here means "this asset does not exist" (404), so an
//     invalid segment is rejected outright — there is no "unknown" asset to
//     fall back to.
//   - looksLikeImage (store.go) only reads the first 12 bytes of a proof
//     image before accepting it into assets/. demo-axk.1's re-review
//     named the resulting gap explicitly: a real image header followed by
//     arbitrary bytes (a polyglot) passes that check. sniffImageContentType
//     below re-derives the same four-format check independently at SERVE
//     time and is paired with an explicit Content-Type (never the file
//     extension, never http.DetectContentType's own sniffing) plus
//     X-Content-Type-Options: nosniff — the combination that keeps a
//     browser from ever interpreting the arbitrary tail of such a file as
//     anything but opaque image bytes.
package reportassets

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/niclasedge/git-planner-go/internal/reports"
)

// Handler serves GET /reports/<repo>/assets/<filename>. Any other method or
// path shape is a 404 (or 405 for a non-GET verb) — never a 500, matching
// the fail-open discipline the rest of this app already follows.
func Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}
		repo, filename, ok := parsePath(r.URL.Path)
		if !ok {
			http.NotFound(w, r)
			return
		}
		serveAsset(w, r, repo, filename)
	})
}

// parsePath requires exactly four non-empty path segments —
// "reports"/<repo>/"assets"/<filename> — and rejects anything else. This
// also closes a slash-smuggling angle for free: net/http already decodes
// percent-escapes into r.URL.Path before this runs, so a segment containing
// an encoded "%2f" becomes an extra "/" and therefore an extra segment,
// which fails the length check below rather than being treated as part of
// repo or filename.
func parsePath(p string) (repo, filename string, ok bool) {
	segments := strings.Split(strings.TrimPrefix(p, "/"), "/")
	if len(segments) != 4 || segments[0] != "reports" || segments[2] != "assets" {
		return "", "", false
	}
	repo, repoOK := sanitizeSegment(segments[1])
	filename, fileOK := sanitizeSegment(segments[3])
	if !repoOK || !fileOK {
		return "", "", false
	}
	return repo, filename, true
}

// sanitizeSegment rejects "", ".", "..", anything containing a path
// separator (so a segment can never itself smuggle a nested path), and any
// embedded NUL byte. See the package doc comment for why this rejects
// instead of substituting a placeholder the way store.go's
// sanitizePathComponent does.
func sanitizeSegment(s string) (string, bool) {
	if s == "" || s == "." || s == ".." {
		return "", false
	}
	if strings.ContainsAny(s, "/\\") || strings.ContainsRune(s, 0) {
		return "", false
	}
	return s, true
}

// serveAsset resolves <repo>/assets/<filename> under reports.BaseDir(),
// guards against a symlink inside assets/ leading outside it, sniffs the
// file's magic bytes, and streams it with an explicit Content-Type plus
// nosniff — or 404s, never a 500, at every failure point.
func serveAsset(w http.ResponseWriter, r *http.Request, repo, filename string) {
	assetsDir := filepath.Join(reports.BaseDir(), repo, "assets")
	target := filepath.Join(assetsDir, filename)

	// Symlink guard (demo-axk.8, explicit requirement): repo and
	// filename are both slash-free basenames (sanitizeSegment), so the
	// filepath.Join above cannot itself escape assetsDir — but a symlink
	// placed inside assets/ could still point outside it, and os.Open
	// follows symlinks. Resolve both sides to their real path and require
	// the file to still live under the resolved assets root.
	resolvedDir, err := filepath.EvalSymlinks(assetsDir)
	if err != nil {
		http.NotFound(w, r) // no assets dir for this repo yet — fail open
		return
	}
	resolvedTarget, err := filepath.EvalSymlinks(target)
	if err != nil {
		http.NotFound(w, r) // missing asset — fail open (demo-axk.8 req. 5)
		return
	}
	if resolvedTarget != resolvedDir && !strings.HasPrefix(resolvedTarget, resolvedDir+string(os.PathSeparator)) {
		http.NotFound(w, r) // symlink escaped assetsDir
		return
	}

	f, err := os.Open(resolvedTarget)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}

	header := make([]byte, 12)
	n, _ := io.ReadFull(f, header)
	contentType, ok := sniffImageContentType(header[:n])
	if !ok {
		// Not a recognized image signature — never deliver it, regardless of
		// what the filename or a stray extension claims (demo-axk.8 req. 3).
		http.NotFound(w, r)
		return
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		http.NotFound(w, r)
		return
	}

	// Set explicitly, before ServeContent: http.ServeContent only sniffs its
	// own Content-Type when the header is not already set, so this is what
	// makes the type magic-byte-derived instead of extension- or
	// DetectContentType-derived. nosniff is what closes the ingest-side
	// polyglot gap (see package doc comment) at the browser.
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, filename, info.ModTime(), f)
}

// sniffImageContentType mirrors internal/reports/store.go's own (unexported)
// magic-byte table — see the package doc comment for why this is a
// deliberate, commented duplicate rather than shared code.
func sniffImageContentType(header []byte) (string, bool) {
	switch {
	case hasPrefix(header, []byte{0x89, 0x50, 0x4E, 0x47}):
		return "image/png", true
	case hasPrefix(header, []byte{0xFF, 0xD8, 0xFF}):
		return "image/jpeg", true
	case hasPrefix(header, []byte{0x47, 0x49, 0x46, 0x38}): // "GIF8" — covers GIF87a and GIF89a
		return "image/gif", true
	case hasPrefix(header, []byte{0x52, 0x49, 0x46, 0x46}) && len(header) >= 12 && string(header[8:12]) == "WEBP":
		return "image/webp", true
	default:
		return "", false
	}
}

func hasPrefix(header, sig []byte) bool {
	return len(header) >= len(sig) && bytes.Equal(header[:len(sig)], sig)
}
