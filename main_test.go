package main

// Tests for the pieces of gkp-ar7.1 that live in main.go itself:
// - newHTTPServer actually sets every timeout on the *http.Server object
//   (the fund from demo-axk.8's review was that none were set at all).
// - rejectWildcardHost (defect 3): 0.0.0.0/:: are rejected outright rather
//   than attempted, so they can never reach the bind-conflict crash.
// - serveLoopbackAndNetwork (defect 2): a network-listener bind failure
//   degrades to loopback-only instead of killing the process.
//
// isLoopbackHost itself moved to internal/server.IsLoopbackHost (defect 4,
// single source of truth with requestIsLoopback) — its tests, including the
// matrix that checks both functions agree, live in
// internal/server/loopback_test.go.

import (
	"net"
	"net/http"
	"testing"
	"time"
)

func TestNewHTTPServerSetsAllTimeouts(t *testing.T) {
	srv := newHTTPServer("127.0.0.1:0", http.NotFoundHandler())

	checks := []struct {
		name string
		got  time.Duration
		want time.Duration
	}{
		{"ReadHeaderTimeout", srv.ReadHeaderTimeout, readHeaderTimeout},
		{"ReadTimeout", srv.ReadTimeout, readTimeout},
		{"WriteTimeout", srv.WriteTimeout, writeTimeout},
		{"IdleTimeout", srv.IdleTimeout, idleTimeout},
	}
	for _, c := range checks {
		if c.got <= 0 {
			t.Errorf("%s: expected a positive timeout, got %v (unset)", c.name, c.got)
		}
		if c.got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, c.got, c.want)
		}
	}
}

// TestRejectWildcardHostRejectsWildcardAddresses is the gkp-ar7.1 defect 3
// regression test: without this fix, "0.0.0.0" and "::" fell through to
// serveLoopbackAndNetwork and reliably crashed the whole daemon on the
// resulting bind conflict (a wildcard bind and the hardcoded 127.0.0.1
// bind on the same port always collide on Linux). Rejecting them here
// means that conflict is never attempted in the first place — this test
// asserts the rejection directly and needs no listener at all, which is
// itself evidence that 0.0.0.0 can no longer reach the crash.
func TestRejectWildcardHostRejectsWildcardAddresses(t *testing.T) {
	cases := map[string]bool{ // host -> wantErr
		"0.0.0.0":    true,
		"::":         true,
		"127.0.0.1":  false,
		"192.0.2.65": false,
		"127.0.0.2":  false,
		"":           false,
	}
	for host, wantErr := range cases {
		err := rejectWildcardHost(host)
		if (err != nil) != wantErr {
			t.Errorf("rejectWildcardHost(%q) error = %v, wantErr %v", host, err, wantErr)
		}
	}
}

// TestServeLoopbackAndNetworkSurvivesBusyNetworkPort is the gkp-ar7.1
// defect 2 regression test. It occupies the network address itself before
// calling serveLoopbackAndNetwork, forcing net.Listen(networkAddr) to fail
// with "address already in use" — the same bind-time failure a moved VM
// DHCP lease would produce — and asserts the loopback listener comes up
// and serves anyway instead of the whole function/process dying with it.
func TestServeLoopbackAndNetworkSurvivesBusyNetworkPort(t *testing.T) {
	loopbackPort := freePort(t)
	networkPort := freePort(t)

	busyLn, err := net.Listen("tcp", "127.0.0.2:"+networkPort)
	if err != nil {
		t.Skipf("cannot bind 127.0.0.2 (sandbox likely has no such interface): %v", err)
	}
	defer busyLn.Close()

	loopbackAddr := "127.0.0.1:" + loopbackPort
	networkAddr := "127.0.0.2:" + networkPort

	done := make(chan error, 1)
	// Deliberately not shut down at the end of this test: serveLoopbackAndNetwork
	// blocks forever on the loopback listener in the success path (that is
	// the point — it degraded, it didn't return), and it has no exported
	// way to ask it to stop short of closing the process. The goroutine
	// sits idle in Accept() until the test binary exits.
	go func() {
		done <- serveLoopbackAndNetwork(loopbackAddr, networkAddr, http.NotFoundHandler())
	}()

	deadline := time.After(2 * time.Second)
	for {
		select {
		case perr := <-done:
			t.Fatalf("serveLoopbackAndNetwork returned early (should have degraded to loopback-only, not exited): %v", perr)
		case <-deadline:
			t.Fatalf("loopback listener on %s never came up despite the network port being busy", loopbackAddr)
		default:
		}
		resp, err := http.Get("http://" + loopbackAddr + "/")
		if err == nil {
			resp.Body.Close()
			return // loopback is up despite the busy network port: defect 2 fixed.
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func freePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve free port: %v", err)
	}
	defer l.Close()
	_, port, err := net.SplitHostPort(l.Addr().String())
	if err != nil {
		t.Fatalf("split addr: %v", err)
	}
	return port
}
