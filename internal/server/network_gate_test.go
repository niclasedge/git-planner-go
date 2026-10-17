package server_test

// Tests for the network gate added in demo-5w3.2: the same handler is
// served on two listeners (loopback = full access, a "network" address =
// GET only). The gate hangs on which listener accepted the TCP connection
// (net/http's http.LocalAddrContextKey), not on any header — so these
// tests deliberately run against two real net.Listener-backed servers, not
// httptest.NewRecorder, which has no connection to key off.
//
// The bead names a concrete LAN address (192.0.2.65) as the network address,
// but binding that here would make the test host-dependent; 127.0.0.2 is
// loopback-range-but-distinct and reachable in any sandbox/CI, so it stands
// in for "the non-loopback listener" everywhere below.

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/niclasedge/git-planner-go/internal/assets"
	"github.com/niclasedge/git-planner-go/internal/render"
	"github.com/niclasedge/git-planner-go/internal/server"
)

// startOn starts a real HTTP server bound to addr, serving handler, and
// returns its base URL plus a cleanup func. httptest.NewUnstartedServer
// already gives us a *httptest.Server wired for Close()/cleanup; we only
// swap its default 127.0.0.1:0 listener for one bound to the address the
// test actually wants.
func startOn(t *testing.T, addr string, handler http.Handler) string {
	t.Helper()
	ts := httptest.NewUnstartedServer(handler)
	if err := ts.Listener.Close(); err != nil {
		t.Fatalf("close default listener: %v", err)
	}
	l, err := net.Listen("tcp", addr+":0")
	if err != nil {
		t.Skipf("cannot bind %s (sandbox likely has no such interface): %v", addr, err)
	}
	ts.Listener = l
	ts.Start()
	t.Cleanup(ts.Close)
	return ts.URL
}

// twoListeners builds one shared handler and starts it on both a loopback
// (full access) and a "network" (GET-only) address, mirroring main.go's
// dual-listener setup for a non-loopback --host.
func twoListeners(t *testing.T) (loopbackURL, networkURL string) {
	t.Helper()
	handler := server.NewHandler(server.AppOptions{
		Cwd:     t.TempDir(),
		Assets:  render.Assets{DashCSS: assets.DashCSS, BrowserCSS: assets.BrowserCSS, BrowserJS: assets.BrowserJS},
		AssetFS: assets.FS(),
	})
	loopbackURL = startOn(t, "127.0.0.1", handler)
	networkURL = startOn(t, "127.0.0.2", handler)
	return loopbackURL, networkURL
}

func postForm(t *testing.T, base, path string, form url.Values, extraHeaders map[string]string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, base+path, strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return resp
}

// TestPostActionPushOverNetworkIsRejected is the exact case named in the
// bead: POST /action/push over the network address must be rejected with
// 403, regardless of the confirm field. It deliberately never checks what
// git did — the test asserts the gate fired before collectors.GitPush was
// ever reached.
func TestPostActionPushOverNetworkIsRejected(t *testing.T) {
	_, networkURL := twoListeners(t)

	resp := postForm(t, networkURL, "/action/push", url.Values{
		"project": {""},
		"branch":  {"main"},
		"confirm": {"PUSH"},
	}, nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 over the network address, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "127.0.0.1") {
		t.Fatalf("expected a clear message naming loopback as the allowed address, got %q", body)
	}
}

// TestPostActionPushOverLoopbackPassesGate confirms the same request over
// 127.0.0.1 is not blocked by the network gate. It stops there on purpose
// — the test asserts a non-403 response (the gate let it through to
// collectors.GitPush, which then fails harmlessly because t.TempDir() is
// not a git repo and there is no origin remote configured) and never
// asserts success, so no real `git push` ever has anywhere to go.
func TestPostActionPushOverLoopbackPassesGate(t *testing.T) {
	loopbackURL, _ := twoListeners(t)

	resp := postForm(t, loopbackURL, "/action/push", url.Values{
		"project": {""},
		"branch":  {"main"},
		"confirm": {"PUSH"},
	}, nil)
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusForbidden {
		t.Fatalf("expected the network gate to let a loopback POST through, got 403")
	}
}

// TestPostMCPOverNetworkIsRejected mirrors the push case for POST /mcp.
func TestPostMCPOverNetworkIsRejected(t *testing.T) {
	_, networkURL := twoListeners(t)

	req, err := http.NewRequest(http.MethodPost, networkURL+"/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for POST /mcp over the network address, got %d", resp.StatusCode)
	}
}

// TestPostMCPOverLoopbackIsAccepted confirms POST /mcp over 127.0.0.1
// reaches the JSON-RPC handler and returns 200 (tools/list needs no
// project/repo state, so this is safe to actually execute).
func TestPostMCPOverLoopbackIsAccepted(t *testing.T) {
	loopbackURL, _ := twoListeners(t)

	req, err := http.NewRequest(http.MethodPost, loopbackURL+"/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200 for POST /mcp over loopback, got %d: %s", resp.StatusCode, body)
	}
}

// TestGetPageAndAssetWorkOverBothAddresses: the page (GET /) and a static
// asset (GET /assets/*) must work identically over the loopback and the
// network address — only mutating routes are gated.
func TestGetPageAndAssetWorkOverBothAddresses(t *testing.T) {
	loopbackURL, networkURL := twoListeners(t)

	for _, base := range []string{loopbackURL, networkURL} {
		resp, err := http.Get(base + "/")
		if err != nil {
			t.Fatalf("GET / on %s: %v", base, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET / on %s: expected 200, got %d", base, resp.StatusCode)
		}

		resp, err = http.Get(base + "/assets/dash.css")
		if err != nil {
			t.Fatalf("GET /assets/dash.css on %s: %v", base, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET /assets/dash.css on %s: expected 200, got %d", base, resp.StatusCode)
		}
	}
}

// TestForwardedHeadersDoNotBypassGate: the bead explicitly calls out
// X-Forwarded-For / X-Forwarded-Host as headers a caller could set hoping
// to look like a loopback client. Neither is read anywhere near the gate
// (it only ever looks at http.LocalAddrContextKey), but this test pins
// that down against regression: a POST over the network address with
// spoofed loopback-looking forwarded headers must still be rejected.
func TestForwardedHeadersDoNotBypassGate(t *testing.T) {
	_, networkURL := twoListeners(t)

	resp := postForm(t, networkURL, "/action/push", url.Values{
		"project": {""},
		"branch":  {"main"},
		"confirm": {"PUSH"},
	}, map[string]string{
		"X-Forwarded-For":  "127.0.0.1",
		"X-Forwarded-Host": "127.0.0.1",
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 even with spoofed X-Forwarded-* headers, got %d", resp.StatusCode)
	}
}

// gkp-ar7.18: the pages carried no Cache-Control, no ETag and no
// Last-Modified, so a browser could keep showing the pre-deploy state with no
// validator to revalidate against — indistinguishable from a broken deploy.
// Every response is a request-time snapshot (window.DATA is embedded, CSS/JS
// are inlined), so none of them may be stored.
func TestPagesAreNotCacheable(t *testing.T) {
	loopbackURL, _ := twoListeners(t)

	for _, path := range []string{"/", "/some-project", "/some-project/reports"} {
		resp, err := http.Get(loopbackURL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s: expected 200, got %d", path, resp.StatusCode)
		}
		if got := resp.Header.Get("Cache-Control"); got != "no-store" {
			t.Errorf("GET %s: Cache-Control = %q, want %q", path, got, "no-store")
		}
	}
}
