package main

import (
	"bytes"
	"log/slog"
	"net"
	"strings"
	"testing"
)

func ipnet(t *testing.T, cidr string) *net.IPNet {
	t.Helper()
	ip, n, err := net.ParseCIDR(cidr)
	if err != nil {
		t.Fatalf("bad test CIDR %s: %v", cidr, err)
	}
	return &net.IPNet{IP: ip, Mask: n.Mask}
}

// The CGNAT range is the whole test: 100.64.x and 100.127.x are tailnet
// addresses, 100.63.x and 100.128.x are outside 100.64.0.0/10 and must not be
// mistaken for one.
func TestTailnetIPs(t *testing.T) {
	addrs := []net.Addr{
		ipnet(t, "127.0.0.1/8"),
		ipnet(t, "192.168.8.42/24"),
		ipnet(t, "100.123.180.46/32"),             // tailnet
		ipnet(t, "100.63.255.1/24"),               // just below the range
		ipnet(t, "100.128.0.1/24"),                // just above it
		ipnet(t, "fd7a:115c:a1e0::f701:b42f/128"), // tailnet v6
		ipnet(t, "fe80::1/64"),
		ipnet(t, "2a03:4000:53:27f::1/64"),
	}

	got := tailnetIPs(addrs)
	want := []string{"100.123.180.46", "fd7a:115c:a1e0::f701:b42f"}
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	for i, w := range want {
		if got[i].String() != w {
			t.Fatalf("expected %v, got %v", want, got)
		}
	}

	if len(tailnetIPs(nil)) != 0 {
		t.Fatal("no interfaces must yield no tailnet addresses")
	}
}

func testLog() (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	return slog.New(slog.NewTextHandler(&buf, nil)), &buf
}

func TestListenAddrs_TailnetOff(t *testing.T) {
	log, buf := testLog()
	got := listenAddrs("127.0.0.1:8092", false, log)
	if len(got) != 1 || got[0] != "127.0.0.1:8092" {
		t.Fatalf("expected the configured bind only, got %v", got)
	}
	if buf.Len() != 0 {
		t.Fatalf("nothing to warn about: %s", buf)
	}
}

// A wildcard bind already serves the tailnet; a second listener on the same port
// would fail, so the flag is ignored with a warning that says what to do.
func TestListenAddrs_WildcardBind(t *testing.T) {
	for _, bind := range []string{"0.0.0.0:8092", ":8092", "[::]:8092"} {
		log, buf := testLog()
		got := listenAddrs(bind, true, log)
		if len(got) != 1 || got[0] != bind {
			t.Fatalf("%s: expected the bind unchanged, got %v", bind, got)
		}
		if !strings.Contains(buf.String(), "every interface") {
			t.Fatalf("%s: expected a warning, got %s", bind, buf)
		}
	}
}

func TestListenAddrs_UnparseableBind(t *testing.T) {
	log, buf := testLog()
	got := listenAddrs("127.0.0.1", true, log)
	if len(got) != 1 || got[0] != "127.0.0.1" {
		t.Fatalf("expected the bind unchanged, got %v", got)
	}
	if !strings.Contains(buf.String(), "cannot read the port") {
		t.Fatalf("expected a warning, got %s", buf)
	}
}

// Whatever this machine has, the result starts with the configured bind and
// every extra address carries the same port. Not asserting a tailnet address
// exists: the test has to pass on a machine without Tailscale.
func TestListenAddrs_TailnetOn(t *testing.T) {
	log, buf := testLog()
	got := listenAddrs("127.0.0.1:8092", true, log)

	if got[0] != "127.0.0.1:8092" {
		t.Fatalf("the configured bind must stay first, got %v", got)
	}
	if len(got) == 1 {
		if !strings.Contains(buf.String(), "no Tailscale address") &&
			!strings.Contains(buf.String(), "cannot list interfaces") {
			t.Fatalf("a skipped tailnet listener must say why: %s", buf)
		}
		return
	}
	for _, a := range got[1:] {
		host, port, err := net.SplitHostPort(a)
		if err != nil {
			t.Fatalf("%q is not a valid address: %v", a, err)
		}
		if port != "8092" {
			t.Fatalf("%q should use the configured port", a)
		}
		if ip := net.ParseIP(host); ip == nil || len(tailnetIPs([]net.Addr{&net.IPAddr{IP: ip}})) != 1 {
			t.Fatalf("%q is not a tailnet address", a)
		}
	}
}

// The listeners are opened before serving so a clash is a startup error, and a
// partial failure must not leave the earlier ports held open.
func TestListenAll_ClosesOnFailure(t *testing.T) {
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("cannot listen: %v", err)
	}
	defer busy.Close()

	free, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("cannot listen: %v", err)
	}
	spare := free.Addr().String()
	free.Close() // now free again, and ours to reopen

	if _, err := listenAll([]string{spare, busy.Addr().String()}); err == nil {
		t.Fatal("expected the busy address to fail")
	}
	// If the first listener had been leaked, this would fail.
	again, err := net.Listen("tcp", spare)
	if err != nil {
		t.Fatalf("the first listener was not closed: %v", err)
	}
	again.Close()
}
