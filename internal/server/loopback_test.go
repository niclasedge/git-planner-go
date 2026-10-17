package server

// Tests for gkp-ar7.1 defect 4: IsLoopbackHost (used by main.go to decide
// which --host values get the single, full-access listener) and
// requestIsLoopback (used by this package's network gate to decide which
// connections are trusted) must agree on every address, because a caller
// reaching main.go's "lesen + schreiben" branch that then gets 403'd by
// this file's gate — or vice versa — is exactly the bug this fixes.
//
// This file is a white-box (package server, not server_test) test because
// requestIsLoopback is unexported; the black-box network-gate tests in
// network_gate_test.go exercise the same function indirectly over real
// listeners and stay there.

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestIsLoopbackHost pins the exact set defect 4 narrowed this to: only
// what requestIsLoopback actually trusts, not the whole 127.0.0.0/8 band
// net.IP.IsLoopback() covers.
func TestIsLoopbackHost(t *testing.T) {
	cases := map[string]bool{
		"127.0.0.1":  true,
		"":           true,
		"localhost":  true,
		"::1":        true,
		"127.0.0.2":  false, // narrowed from the whole 127.0.0.0/8 band — see defect 4
		"192.0.2.65": false,
		"0.0.0.0":    false,
		"10.0.0.5":   false,
	}
	for host, want := range cases {
		if got := IsLoopbackHost(host); got != want {
			t.Errorf("IsLoopbackHost(%q) = %v, want %v", host, got, want)
		}
	}
}

// requestWithLocalAddr builds a bare *http.Request carrying host as the
// connection's local address under http.LocalAddrContextKey — the same
// context value net/http's own Server attaches to every request the
// instant a connection is accepted (see requestIsLoopback's doc comment).
func requestWithLocalAddr(host string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	addr := &net.TCPAddr{IP: net.ParseIP(host), Port: 8765}
	ctx := context.WithValue(req.Context(), http.LocalAddrContextKey, net.Addr(addr))
	return req.WithContext(ctx)
}

// TestIsLoopbackHostAndRequestIsLoopbackAgree is the matrix the bead asks
// for: the same raw addresses run through IsLoopbackHost (as a --host
// value) and through requestIsLoopback (as an accepted connection's local
// address) must classify identically, because both are now fed by the
// same isLoopbackIP set. "localhost" and "" are --host-only tokens — a
// socket's local address is always a concrete IP by the time
// http.LocalAddrContextKey is set, never the literal string "localhost" or
// "" — so they are meaningless as connection addresses and are excluded
// from this matrix for that reason, not because the two functions might
// disagree on them.
func TestIsLoopbackHostAndRequestIsLoopbackAgree(t *testing.T) {
	addrs := []string{"127.0.0.1", "::1", "127.0.0.2", "192.0.2.65", "10.0.0.5"}
	for _, host := range addrs {
		wantHost := IsLoopbackHost(host)
		gotRequest := requestIsLoopback(requestWithLocalAddr(host))
		if wantHost != gotRequest {
			t.Errorf("mismatch for %q: IsLoopbackHost=%v requestIsLoopback=%v", host, wantHost, gotRequest)
		}
	}
}
