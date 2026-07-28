package main

import (
	"fmt"
	"log/slog"
	"net"
)

// tailscaleNets are the ranges Tailscale assigns to nodes: 100.64.0.0/10 is the
// CGNAT space from RFC 6598, fd7a:115c:a1e0::/48 the matching IPv6 block. An
// address of this machine inside one of them is a tailnet address, whatever the
// interface happens to be called (utun3 today, utun5 after a reconnect).
var tailscaleNets = mustParseCIDRs("100.64.0.0/10", "fd7a:115c:a1e0::/48")

func mustParseCIDRs(cidrs ...string) []*net.IPNet {
	out := make([]*net.IPNet, 0, len(cidrs))
	for _, c := range cidrs {
		_, n, err := net.ParseCIDR(c)
		if err != nil {
			panic("bad CIDR " + c + ": " + err.Error())
		}
		out = append(out, n)
	}
	return out
}

// tailnetIPs filters interface addresses down to this machine's tailnet
// addresses. Split out from the interface lookup so the range matching is
// testable without a Tailscale install.
func tailnetIPs(addrs []net.Addr) []net.IP {
	var out []net.IP
	for _, a := range addrs {
		var ip net.IP
		switch v := a.(type) {
		case *net.IPNet:
			ip = v.IP
		case *net.IPAddr:
			ip = v.IP
		default:
			continue
		}
		for _, n := range tailscaleNets {
			if n.Contains(ip) {
				out = append(out, ip)
				break
			}
		}
	}
	return out
}

// localTailnetIPs asks the kernel for the addresses of every interface. Errors
// are the caller's to report: a machine without Tailscale running is a warning,
// not a reason to refuse to start.
func localTailnetIPs() ([]net.IP, error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil, err
	}
	return tailnetIPs(addrs), nil
}

// listenAddrs is the set of addresses to serve on: the configured bind, plus
// this machine's tailnet addresses when server.tailnet is set.
//
// Listening on the tailnet address specifically — rather than on 0.0.0.0 — is
// the point: the app has no login, so a wildcard bind would offer issue editing
// to whatever café Wi-Fi the laptop joins next.
func listenAddrs(bind string, tailnet bool, log *slog.Logger) []string {
	addrs := []string{bind}
	if !tailnet {
		return addrs
	}

	host, port, err := net.SplitHostPort(bind)
	if err != nil {
		log.Warn("tailnet listener skipped: cannot read the port from server.bind",
			"bind", bind, "err", err)
		return addrs
	}
	// A wildcard bind already covers the tailnet, and a second listener on the
	// same port would fail.
	if host == "" || host == "0.0.0.0" || host == "::" {
		log.Warn("server.tailnet ignored: server.bind already listens on every interface",
			"bind", bind,
			"hint", "set bind to 127.0.0.1:<port> so only loopback and the tailnet are served")
		return addrs
	}

	ips, err := localTailnetIPs()
	if err != nil {
		log.Warn("tailnet listener skipped: cannot list interfaces", "err", err)
		return addrs
	}
	if len(ips) == 0 {
		log.Warn("tailnet listener skipped: no Tailscale address on this machine",
			"hint", "start Tailscale and restart, or set server.tailnet: false")
		return addrs
	}
	for _, ip := range ips {
		if ip.String() == host {
			continue // already the configured bind
		}
		addrs = append(addrs, net.JoinHostPort(ip.String(), port))
	}
	return addrs
}

// listenAll opens every address up front, so a port clash is a startup error
// rather than a listener that quietly never came up. On failure the listeners
// already opened are closed again.
func listenAll(addrs []string) ([]net.Listener, error) {
	var lns []net.Listener
	for _, a := range addrs {
		ln, err := net.Listen("tcp", a)
		if err != nil {
			for _, open := range lns {
				open.Close()
			}
			return nil, fmt.Errorf("listening on %s: %w", a, err)
		}
		lns = append(lns, ln)
	}
	return lns, nil
}
