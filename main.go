// Command git-planner serves the single-pane planner dashboard on
// 127.0.0.1: GitHub issues and pull requests next to beads maps and evidence reports.
package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/niclasedge/git-planner-go/internal/anzeigezeit"
	"github.com/niclasedge/git-planner-go/internal/appctx"
	"github.com/niclasedge/git-planner-go/internal/assets"
	"github.com/niclasedge/git-planner-go/internal/cache"
	"github.com/niclasedge/git-planner-go/internal/collectors"
	"github.com/niclasedge/git-planner-go/internal/demo"
	"github.com/niclasedge/git-planner-go/internal/dotenv"
	"github.com/niclasedge/git-planner-go/internal/render"
	"github.com/niclasedge/git-planner-go/internal/server"
)

// Server timeouts (demo-5w3.2, fund out of demo-axk.8's review:
// main.go had none). Tolerable on a loopback-only listener; an open flank
// the moment --host binds a LAN address. Chosen so a real client on this
// app's own LAN — reading the reports page and its proof-image assets,
// capped at 10 MiB by internal/reports' ingest guard — is never cut off:
//
//   - readHeaderTimeout: headers are a handful of lines; 5s is generous for
//     a slow client and still closes a Slowloris-style connection quickly.
//   - readTimeout: every request body this app accepts is a small HTML
//     form POST (git message, branch name, confirm string) — no uploads —
//     so 30s only needs to cover a slow client, not a large payload.
//   - writeTimeout: bounds the response write, which is what a 10 MiB
//     asset download actually hits. Even a throttled home-LAN link
//     (~1 MiB/s) clears that in ~10s; 60s leaves ample headroom without
//     holding a genuinely stuck connection open forever.
//   - idleTimeout: how long a keep-alive connection may sit idle between
//     requests before being reclaimed.
const (
	readHeaderTimeout = 5 * time.Second
	readTimeout       = 30 * time.Second
	writeTimeout      = 60 * time.Second
	idleTimeout       = 120 * time.Second
)

// rejectWildcardHost errors out on a wildcard --host ("all interfaces")
// instead of trying to bind it. gkp-ar7.1 defect 3: --host 0.0.0.0 (or
// "::") is the most obvious wrong answer for "bind on every interface",
// and it is live-reproducibly fatal — the wildcard listener and the
// hardcoded 127.0.0.1 loopback listener below both want the same port,
// and a wildcard bind always collides with a specific bind on the same
// port on Linux, regardless of which one starts first. That collision
// used to reach log.Fatal(<-errCh) in serveLoopbackAndNetwork and kill the
// whole daemon — a crash loop under Restart=always.
//
// The alternative considered was "one listener, network-gate active":
// bind only to the wildcard address and let requestIsLoopback (server.go)
// keep sorting loopback from network per request. That does not work
// without weakening the gate: requestIsLoopback deliberately keys off
// http.LocalAddrContextKey — which listener accepted the connection, not
// anything the client can influence — and a single wildcard listener has
// no such distinction to key off. Making that variant safe would mean
// switching the gate to RemoteAddr (trusting a client-observable address
// instead of the accepting socket), which is a materially different
// security boundary and not something to fold into this fix silently. An
// explicit rejection is the honest choice: it fails loudly at startup
// instead of quietly widening what "loopback" trusts.
func rejectWildcardHost(host string) error {
	if host == "0.0.0.0" || host == "::" {
		return fmt.Errorf("--host %s ist eine Wildcard-Adresse (alle Interfaces) und wird abgelehnt: sie kollidiert mit dem festen Loopback-Listener auf 127.0.0.1 (Bind-Konflikt auf demselben Port, siehe gkp-ar7.1) und würde den Netz-Gate-Vertrag aufweichen müssen, um sicher zu sein. Gib stattdessen die konkrete Adresse an, auf der du binden willst (z.B. die LAN-/VM-IP)", host)
	}
	return nil
}

// newHTTPServer builds an *http.Server with the timeouts above applied,
// factored out so both listeners (and this file's test) share one place
// that sets them.
func newHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}
}

func main() {
	cwdFlag := flag.String("cwd", ".", "repo directory to inspect")
	portFlag := flag.String("port", "8765", "listen port")
	hostFlag := flag.String("host", "127.0.0.1", "listen host; a non-loopback address (e.g. a LAN IP) exposes GET routes there while POST /mcp and POST /action/* stay on 127.0.0.1 only. 0.0.0.0 and :: (wildcard, all interfaces) are rejected — name the specific interface address instead")
	noOpen := flag.Bool("no-open", false, "do not open the browser on start")
	// netz-schreiben (#46): Vorgabe aus GKP_WRITE_CLIENTS, damit die
	// systemd-Unit die Liste ueber eine Environment-Zeile setzen kann (das
	// justfile darf nur wachsen, ExecStart bleibt, wie sie ist). Leer = aus.
	writeClientsFlag := flag.String("write-clients", os.Getenv("GKP_WRITE_CLIENTS"), "comma-separated single IPv4/IPv6 addresses that may POST over the network listener (peer IP of the socket, no headers). No CIDR, no wildcards; the single word \"alle\" allows every device that reaches the network listener. Default: $GKP_WRITE_CLIENTS, empty = loopback only")
	// Hinter Traefik/Portmapping: der oeffentliche Name, unter dem die
	// Oberflaeche geoeffnet wird. Der Proxy sichert den Zugang (Basic Auth).
	publicOriginFlag := flag.String("public-origin", os.Getenv("GKP_PUBLIC_ORIGIN"), "origin of a reverse proxy in front of the service (e.g. https://git.example.com). Write requests allowed by --write-clients may then carry this Origin with the matching Host. Default: $GKP_PUBLIC_ORIGIN, empty = off")
	demoFlag := flag.Bool("demo", false, "run against fictional data: an in-memory GitHub API plus a throw-away workspace with beads maps (needs git and bd). Reads no real repos, tokens or HOME")
	flag.Parse()

	writeAlle, writeClients, err := server.ParseWriteFreigabe(*writeClientsFlag)
	if err != nil {
		log.Fatal(err)
	}
	publicOrigin, err := server.ParsePublicOrigin(*publicOriginFlag)
	if err != nil {
		log.Fatal(err)
	}

	if *demoFlag {
		tmp, err := os.MkdirTemp("", "git-planner-demo-")
		if err != nil {
			log.Fatal(err)
		}
		demoCwd, fake, err := demo.Start(tmp)
		if err != nil {
			log.Fatal(err)
		}
		*cwdFlag = demoCwd
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
		go func() {
			<-sig
			fake.Close()
			_ = os.RemoveAll(tmp)
			os.Exit(0)
		}()
		fmt.Printf("  demo mode: fictional data in %s\n", tmp)
	}

	cwd, err := filepath.Abs(*cwdFlag)
	if err != nil {
		cwd = *cwdFlag
	}
	// .env im --cwd ergaenzt die Umgebung, ueberschreibt sie nicht (GitHub-Issue
	// #2): GITHUB_PAT_WORK darf von aussen kommen. Ins Log nur die Namen.
	if gesetzt, err := dotenv.Laden(filepath.Join(cwd, ".env")); err != nil {
		fmt.Fprintf(os.Stderr, "WARNUNG: .env nicht lesbar: %v\n", err)
	} else if len(gesetzt) > 0 {
		fmt.Printf("  .env: %s gesetzt\n", strings.Join(gesetzt, ", "))
	}
	port := *portFlag
	host := *hostFlag
	// Browser-URL: 0.0.0.0 ist keine erreichbare Ziel-Adresse → auf localhost zeigen.
	browserHost := host
	if browserHost == "0.0.0.0" || browserHost == "" {
		browserHost = "127.0.0.1"
	}
	urlStr := "http://" + browserHost + ":" + port + "/"

	if !*noOpen {
		_ = exec.Command("open", urlStr).Start() // best-effort; ignore if unavailable
	}

	claudeHome := collectors.Home() + "/.claude"

	// Prime + refresh the aggregate cache in the background so the server boots
	// instantly; the sidebar/global fill in once the first scan lands. Aggregate
	// prime first — it is what tells us which projects there are — then warm
	// every one of them, not just the start project (gkp-n5q): a cold project
	// cost up to 39,63 s on first click, and priming one of N left that bill
	// with whoever clicked next.
	go func() {
		cache.StartCache(cwd, claudeHome)
		cache.PrimeAllProjects(cwd, claudeHome, cache.GetAggregate())
		// Primt zusaetzlich das projektuebergreifende Aufgaben-Aggregat
		// (gkp-92i, Nachtrag aus gkp-i90): ohne diesen Schritt trifft der
		// erste Aufruf von GET /ansichten (Sammelansicht) nach jedem
		// Neustart den kalten, BLOCKIERENDEN Pfad in
		// cache.GetAnsichtenAggregat — siehe StartAnsichtenAggregat.
		cache.StartAnsichtenAggregat(func() (map[string]appctx.AufgabenAnsicht, []appctx.ProjektFehler) {
			return appctx.AnsichtenAlleProjekte(collectors.RepoRoot(cwd), anzeigezeit.Jetzt())
		})
	}()

	fmt.Printf("knowledge dashboard (go) → %s  (cwd=%s)\n", urlStr, cwd)

	handler := server.NewHandler(server.AppOptions{
		Cwd: cwd,
		Assets: render.Assets{
			DashCSS:    assets.DashCSS,
			BrowserCSS: assets.BrowserCSS,
			BrowserJS:  assets.BrowserJS,
		},
		AssetFS:      assets.FS(),
		WriteClients: writeClients,
		WriteAlle:    writeAlle,
		PublicOrigin: publicOrigin,
		// Die CI-Runs aller Repos der Leiste einmal am Tag (ci-runs, #122).
		CIHintergrund: true,
	})

	if server.IsLoopbackHost(host) {
		// --host is 127.0.0.1 (the default) or another loopback address
		// (::1, "localhost", "" — see server.IsLoopbackHost): one
		// listener, bound to exactly what was asked for, full access —
		// exactly today's behavior. (Not hardcoded to 127.0.0.1: a caller
		// who explicitly passed --host ::1 still gets that address, not a
		// silent substitute. 127.0.0.2 no longer takes this branch — see
		// gkp-ar7.1 defect 4 — it now falls through to the network branch
		// below, which is what internal/server's gate actually trusts it
		// as.)
		addr := host + ":" + port
		srv := newHTTPServer(addr, handler)
		fmt.Printf("  %s → lesen + schreiben (GET, POST /mcp, POST /action/*)\n", addr)
		log.Fatal(srv.ListenAndServe())
		return
	}

	if err := rejectWildcardHost(host); err != nil {
		log.Fatal(err)
	}

	// --host names a LAN address: a second, restricted listener there,
	// alongside the always-on loopback listener. The same handler serves
	// both — internal/server's network gate (requestIsLoopback) decides
	// per connection what a non-GET request on either listener gets, not
	// this file.
	loopbackAddr := "127.0.0.1:" + port
	networkAddr := host + ":" + port
	log.Fatal(serveLoopbackAndNetwork(loopbackAddr, networkAddr, handler, netzFreigabe{alle: writeAlle, ips: writeClients}))
}

// serveLoopbackAndNetwork starts the always-on loopback listener plus,
// best-effort, the GET-only network listener, then blocks serving both.
//
// gkp-ar7.1 defect 2: the network listener is optional. If host names an
// address that stops being available on this machine — the clearest case
// is a VM whose DHCP-assigned IP changed since the unit was written —
// binding it fails, but that has nothing to do with whether loopback
// still works. Previously both bind attempts happened inside
// ListenAndServe on their own goroutines, and log.Fatal(<-errCh) took
// whichever of the two errored first — including a network-bind failure —
// down with the whole process. Under Restart=always/RestartSec=2 that is
// a permanent crash loop instead of a service that degrades to
// loopback-only and keeps working. The loopback listener failing to bind,
// by contrast, IS fatal: that is the service itself, not an optional
// extra.
//
// freigabe (--write-clients) is only named in the startup line; the gate itself lives in
// internal/server and gets the same list through AppOptions.
func serveLoopbackAndNetwork(loopbackAddr, networkAddr string, handler http.Handler, freigabe ...netzFreigabe) error {
	var f netzFreigabe
	if len(freigabe) > 0 {
		f = freigabe[0]
	}
	loopbackLn, err := net.Listen("tcp", loopbackAddr)
	if err != nil {
		return fmt.Errorf("Loopback-Listener %s: %w", loopbackAddr, err)
	}
	loopbackSrv := newHTTPServer(loopbackAddr, handler)
	fmt.Printf("  %s → lesen + schreiben (GET, POST /mcp, POST /action/*)\n", loopbackAddr)

	networkLn, err := net.Listen("tcp", networkAddr)
	if err != nil {
		fmt.Printf("WARNUNG: Netz-Listener %s konnte nicht gebunden werden (%v) — Dienst läuft mit Loopback allein weiter, nur über 127.0.0.1 erreichbar.\n", networkAddr, err)
		return loopbackSrv.Serve(loopbackLn)
	}
	networkSrv := newHTTPServer(networkAddr, handler)
	fmt.Printf("  %s → %s\n", networkAddr, netzListenerText(f.alle, f.ips))

	errCh := make(chan error, 2)
	go func() { errCh <- loopbackSrv.Serve(loopbackLn) }()
	go func() { errCh <- networkSrv.Serve(networkLn) }()
	return <-errCh
}

// netzFreigabe ist, was --write-clients freigibt (#46): alle Geraete oder
// eine Liste einzelner IPs.
type netzFreigabe struct {
	alle bool
	ips  []net.IP
}

// netzListenerText beschreibt in der Startausgabe, wer ueber den
// Netz-Listener schreiben darf (#46).
func netzListenerText(writeAlle bool, writeClients []net.IP) string {
	if writeAlle {
		return "lesen + schreiben fuer JEDES Geraet, das diese Adresse erreicht (--write-clients alle); Origin muss passen"
	}
	if len(writeClients) == 0 {
		return "nur lesen (GET) — POST wird mit 403 abgewiesen (keine --write-clients)"
	}
	namen := make([]string, len(writeClients))
	for i, ip := range writeClients {
		namen[i] = ip.String()
	}
	return "lesen (GET); schreiben (POST) nur von " + strings.Join(namen, ", ") + " (--write-clients), sonst 403"
}
