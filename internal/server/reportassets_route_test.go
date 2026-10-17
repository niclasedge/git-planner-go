package server_test

// E2E tests for GET /reports/<repo>/assets/<filename> (demo-axk.8),
// deliberately run against server.NewHandler and not just the
// internal/reportassets handler in isolation — demo-axk.3's Welle-2
// review found that route wiring (reservedSegments, the routing order
// inside NewHandler) is exactly the kind of thing a package-level test
// alone does not exercise.

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/niclasedge/git-planner-go/internal/assets"
	"github.com/niclasedge/git-planner-go/internal/render"
	"github.com/niclasedge/git-planner-go/internal/server"
)

// pngHeader is a real PNG magic-byte prefix (8 bytes) — enough for both
// store.go's ingest-side looksLikeImage and reportassets' serve-side
// sniffImageContentType to accept it as PNG.
var pngHeader = []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}

func newTestHandler(t *testing.T) http.Handler {
	t.Helper()
	return server.NewHandler(server.AppOptions{
		Cwd:     t.TempDir(),
		Assets:  render.Assets{DashCSS: assets.DashCSS, BrowserCSS: assets.BrowserCSS, BrowserJS: assets.BrowserJS},
		AssetFS: assets.FS(),
	})
}

// withReportsDir points CC_REPORTS_DIR at a fresh t.TempDir() and returns it
// — same isolation pattern as internal/reports/store_test.go and
// internal/mcp/mcp_test.go.
func withReportsDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("CC_REPORTS_DIR", dir)
	return dir
}

func getAsset(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// TestReportAssetPolyglotServedWithNosniff is the exact case named in the
// bead: a real PNG header followed by arbitrary bytes. Ingest (store.go's
// looksLikeImage) only reads the first 12 bytes and would accept this file;
// the serve side must not compound that into a browser-exploitable
// response — it has to go through (200, image/png), but only ever paired
// with X-Content-Type-Options: nosniff.
func TestReportAssetPolyglotServedWithNosniff(t *testing.T) {
	base := withReportsDir(t)
	assetsDir := filepath.Join(base, "myrepo", "assets")
	if err := os.MkdirAll(assetsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	polyglot := append(append([]byte{}, pngHeader...), []byte("<script>alert(1)</script> arbitrary tail bytes, not real PNG stream data")...)
	if err := os.WriteFile(filepath.Join(assetsDir, "entry-0.png"), polyglot, 0o644); err != nil {
		t.Fatal(err)
	}

	rec := getAsset(t, newTestHandler(t), "/reports/myrepo/assets/entry-0.png")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/png" {
		t.Fatalf("expected Content-Type image/png (magic-byte derived), got %q", ct)
	}
	if ns := rec.Header().Get("X-Content-Type-Options"); ns != "nosniff" {
		t.Fatalf("expected X-Content-Type-Options: nosniff, got %q", ns)
	}
	if !bytes.Equal(rec.Body.Bytes(), polyglot) {
		t.Fatalf("body mismatch: served bytes must equal the file's bytes verbatim")
	}
}

// TestReportAssetTraversalDoesNotEscapeAssetsDir plants a secret file next
// to (but outside) the assets/ directory and confirms a ".." filename
// segment cannot reach it.
func TestReportAssetTraversalDoesNotEscapeAssetsDir(t *testing.T) {
	base := withReportsDir(t)
	repoDir := filepath.Join(base, "myrepo")
	assetsDir := filepath.Join(repoDir, "assets")
	if err := os.MkdirAll(assetsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	secret := []byte("TOP SECRET, must never be served")
	if err := os.WriteFile(filepath.Join(repoDir, "secret.txt"), secret, 0o644); err != nil {
		t.Fatal(err)
	}

	h := newTestHandler(t)
	for _, path := range []string{
		"/reports/myrepo/assets/..",
		"/reports/myrepo/assets/../secret.txt",
		"/reports/myrepo/assets/%2e%2e/secret.txt",
		"/reports/myrepo/assets/%2e%2e%2fsecret.txt",
	} {
		rec := getAsset(t, h, path)
		if rec.Code == http.StatusOK {
			t.Fatalf("path %q: expected traversal to be rejected, got 200 body=%q", path, rec.Body.String())
		}
		if bytes.Contains(rec.Body.Bytes(), secret) {
			t.Fatalf("path %q: secret content leaked into response body", path)
		}
	}
}

// TestReportAssetRejectsNonImageContent confirms a file without a
// recognized image signature is never delivered, regardless of its
// extension.
func TestReportAssetRejectsNonImageContent(t *testing.T) {
	base := withReportsDir(t)
	assetsDir := filepath.Join(base, "myrepo", "assets")
	if err := os.MkdirAll(assetsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(assetsDir, "entry-0.png"), []byte("-----BEGIN OPENSSH PRIVATE KEY-----"), 0o644); err != nil {
		t.Fatal(err)
	}

	rec := getAsset(t, newTestHandler(t), "/reports/myrepo/assets/entry-0.png")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for a file without a recognized image signature, got %d", rec.Code)
	}
}

// TestReportAssetMissingIs404 confirms fail-open: a missing asset is a
// plain 404, never a 500 or a panic that would take the page down with it.
func TestReportAssetMissingIs404(t *testing.T) {
	withReportsDir(t)
	rec := getAsset(t, newTestHandler(t), "/reports/myrepo/assets/does-not-exist.png")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for a missing asset, got %d", rec.Code)
	}
}

// TestReportAssetSymlinkEscapeIsRejected: a symlink placed inside assets/
// pointing outside it must not be followed.
func TestReportAssetSymlinkEscapeIsRejected(t *testing.T) {
	base := withReportsDir(t)
	repoDir := filepath.Join(base, "myrepo")
	assetsDir := filepath.Join(repoDir, "assets")
	if err := os.MkdirAll(assetsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	secret := []byte("TOP SECRET via symlink")
	secretPath := filepath.Join(base, "outside-secret.png")
	if err := os.WriteFile(secretPath, append(append([]byte{}, pngHeader...), secret...), 0o644); err != nil {
		t.Fatal(err)
	}
	linkPath := filepath.Join(assetsDir, "escape.png")
	if err := os.Symlink(secretPath, linkPath); err != nil {
		t.Skipf("symlinks not supported on this filesystem: %v", err)
	}

	rec := getAsset(t, newTestHandler(t), "/reports/myrepo/assets/escape.png")

	if rec.Code == http.StatusOK {
		t.Fatalf("expected symlink escape to be rejected, got 200")
	}
	if bytes.Contains(rec.Body.Bytes(), secret) {
		t.Fatalf("secret content reachable via symlink leaked into response body")
	}
}
