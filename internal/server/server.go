// Package server builds the HTTP request handler — Go-native port of server.ts.
// Native routing via URL path + method (no framework), mirroring the Deno routes.
package server

import (
	"context"
	"encoding/json"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/niclasedge/git-planner-go/internal/anzeigezeit"
	"github.com/niclasedge/git-planner-go/internal/appctx"
	"github.com/niclasedge/git-planner-go/internal/cache"
	"github.com/niclasedge/git-planner-go/internal/collectors"
	"github.com/niclasedge/git-planner-go/internal/mcp"
	"github.com/niclasedge/git-planner-go/internal/md"
	"github.com/niclasedge/git-planner-go/internal/render"
	"github.com/niclasedge/git-planner-go/internal/reportassets"
	"github.com/niclasedge/git-planner-go/internal/reports"
)

// AppOptions configures the handler.
type AppOptions struct {
	Cwd     string        // repo directory to inspect (--cwd)
	Assets  render.Assets // embedded asset contents for inline embedding
	AssetFS fs.FS         // static asset tree for /assets/*
	// WriteClients sind die Peer-IPs, die ueber den Netz-Listener schreiben
	// duerfen (--write-clients, #46). Leer: nur Loopback schreibt.
	WriteClients []net.IP
	// WriteAlle gibt das Schreiben fuer jede Peer-IP frei, die den
	// Netz-Listener erreicht (--write-clients alle). Die Origin-Pruefung
	// (csrfOkFuer) gilt trotzdem.
	WriteAlle bool
	// PublicOrigin ist die Origin des Reverse-Proxys vor dem Dienst
	// (--public-origin, z.B. https://git.example.com). csrfOkFuer laesst
	// fuer freigegebene Peers auch Host/Origin mit genau diesem Namen durch.
	PublicOrigin string
	// CIHintergrund startet den Tageslauf der CI-Runs (ci-runs, #122): beim
	// Start und stuendlich jedes Repo der Leiste, dessen Stand fehlt oder 24 h
	// alt ist. Nur main.go setzt es; Tests rufen den Lauf selbst.
	CIHintergrund bool
}

// ansichtSegmente sind die Pfadsegmente, die eine Ansicht ueber ALLE Projekte
// oeffnen (/heute, /jederzeit, … gkp-92i) — aus appctx.Regeln() gezogen und
// NICHT hier ein zweites Mal von Hand aufgeschrieben. Eine zweite, von Hand
// gepflegte Liste ist genau die Fehlerklasse, die der Kommentar am
// favicon.ico-Redirect unten beschreibt: ein Segment, das in KEINER Liste
// steht, wird als Projektname gedeutet.
//
// "bereiche" ist KEINE Regel (appctx.BereicheGruppieren haengt nicht an der
// Regel-Tabelle, sondern an areas.json) und steht deshalb von Hand daneben.
func ansichtSegmenteBauen() map[string]bool {
	out := map[string]bool{appctx.BereicheID: true}
	for _, r := range appctx.Regeln() {
		out[r.ID] = true
	}
	return out
}

var ansichtSegmente = ansichtSegmenteBauen()

// reservedSegments traegt zusaetzlich ALLE ansichtSegmente (gkp-92i): auch
// wenn die eigentliche Behandlung dieser Ids weiter unten VOR der
// reservedSegments-Pruefung greift und frueher zurueckkehrt, bleibt diese
// Menge die zweite Sicherung gegen "ein Segment ohne Route wird zum
// Projektnamen" — dieselbe Verteidigungslinie, die reservedSegments fuer
// /assets, /action usw. schon ist.
var reservedSegments = reservedSegmenteBauen()

func reservedSegmenteBauen() map[string]bool {
	out := map[string]bool{
		"assets": true, "read": true, "hook-inject": true, "gitdiff": true, "github": true,
		"action": true, "mcp": true, "reports": true, "plan-svg": true, "ansichten": true,
		"belege": true, "api": true, "things": true, "ueberblick": true,
		"favicon.ico": true, "favicon.svg": true,
	}
	for id := range ansichtSegmente {
		out[id] = true
	}
	return out
}

// csrfOk rejects cross-origin POSTs: Origin must be localhost if present.
// This alone is not enough to protect a networked listener: it lets
// through any request that sends no Origin at all (a script, curl, an MCP
// client) — which is exactly what allowMutationsFromNetwork below closes.
// csrfOk stays in place as defense-in-depth against a browser tab on this
// same host being tricked into cross-origin POSTs to loopback.
func csrfOk(origin string) bool {
	if origin == "" {
		return true
	}
	return strings.HasPrefix(origin, "http://127.0.0.1") || strings.HasPrefix(origin, "http://localhost")
}

// loopbackIPs is the exact, closed set of addresses this app's full-access
// listener ever binds to. It is the single source of truth shared by
// IsLoopbackHost (main.go: which --host values get the single, full-access
// listener) and requestIsLoopback below (which connections the network gate
// trusts) — see gkp-ar7.1 defect 4. Before this fix the two checks used
// different definitions (main.go's isLoopbackHost accepted the whole
// 127.0.0.0/8 band; this file trusted only these two exact addresses), so
// `--host 127.0.0.2` made main.go print "lesen + schreiben" for a listener
// this file's gate then 403'd on every POST.
var loopbackIPs = []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback}

func isLoopbackIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	for _, l := range loopbackIPs {
		if ip.Equal(l) {
			return true
		}
	}
	return false
}

// IsLoopbackHost reports whether host — as passed to --host — names only
// the local machine: exactly 127.0.0.1, ::1, "localhost", or "" (main.go's
// pre-flag-parse zero value). Exported so main.go classifies a --host value
// the same way requestIsLoopback classifies an accepted connection; see
// loopbackIPs above.
func IsLoopbackHost(host string) bool {
	if host == "" || host == "localhost" {
		return true
	}
	return isLoopbackIP(net.ParseIP(host))
}

// requestIsLoopback reports whether the TCP connection this request arrived
// on has a loopback LOCAL address — i.e. which of the two listeners (see
// main.go) accepted it. net/http's own Server attaches the connection's
// local net.Addr to every request's context under http.LocalAddrContextKey;
// that value comes from the OS socket the moment the connection was
// accepted, before a single byte of the request (headers or body) was
// read. No header the client sends — Origin, X-Forwarded-For,
// X-Forwarded-Host, a method-override header, or anything else — can
// change it. That is the point: the gate below hangs on the connection,
// not on anything the caller claims about itself.
func requestIsLoopback(r *http.Request) bool {
	addr, ok := r.Context().Value(http.LocalAddrContextKey).(net.Addr)
	if !ok {
		return false // unknown connection origin: fail closed, treat as network-facing.
	}
	host, _, err := net.SplitHostPort(addr.String())
	if err != nil {
		host = addr.String()
	}
	return isLoopbackIP(net.ParseIP(host))
}

// NewHandler builds the request handler.
func NewHandler(opts AppOptions) http.Handler {
	claudeHome := collectors.Home() + "/.claude"
	assetHandler := http.StripPrefix("/assets/", http.FileServer(http.FS(opts.AssetFS)))
	mcpHandler := mcp.Handler()
	reportAssetsHandler := reportassets.Handler()
	schliessHandler := neueSchliessRoute()

	getProjects := func() []collectors.ProjectRef {
		return appctx.DiscoverProjects(collectors.RepoRoot(opts.Cwd), "")
	}
	// Die Bereichsansicht zeigt die Projekte DIESER Maschine, nicht die eines
	// Repos — die Schreibroute braucht dieselbe Entdeckung, die getTarget
	// benutzt. Durchgereicht und nicht dort noch einmal aufgerufen: eine zweite
	// Definition davon, was ein Projekt ist, waere genau die Doppelung, an der
	// Aufnahme und Anzeige auseinanderlaufen (gkp-apb).
	taskHandler := neueTaskRoutes(getProjects)
	// Repos, Arbeitsliste und Detail der Things-Oberflaeche (repo-leiste,
	// #25). Ein Dienst je Handler: sein Speicher (Konten, Baeume, Blobs)
	// gehoert zu genau diesem Handler, Tests bekommen also jeder einen frischen.
	repoHandler := neuerRepoDienst(getProjects)
	if opts.CIHintergrund {
		go repoHandler.ciTakt(context.Background())
	}
	getTarget := func(project string) string {
		return appctx.ResolveProjectCwd(project, getProjects(), opts.Cwd)
	}

	servePage := func(w http.ResponseWriter, projectParam, initialView, initialSub string) {
		cache.EnsureFresh(opts.Cwd, claudeHome)
		agg := cache.GetAggregate()
		var sidebar []collectors.SidebarProject
		if agg != nil {
			sidebar = agg.Projects
		} else {
			for _, p := range getProjects() {
				sidebar = append(sidebar, collectors.SidebarProject{ProjectRef: p})
			}
		}
		selected := ""
		for _, p := range sidebar {
			if p.Name == projectParam {
				selected = projectParam
				break
			}
		}
		view := "overview"
		if selected != "" {
			view = "project"
		}
		refs := make([]collectors.ProjectRef, len(sidebar))
		for i, p := range sidebar {
			refs[i] = p.ProjectRef
		}
		target := appctx.ResolveProjectCwd(selected, refs, opts.Cwd)
		activeName := selected
		if activeName == "" {
			activeName = lastSeg(collectors.RepoRoot(opts.Cwd))
		}
		var globalMap map[string]any
		if agg != nil {
			globalMap = agg.Global
		}
		var context map[string]any
		if view == "overview" {
			context = appctx.BuildContext(target, claudeHome, appctx.BuildOpts{
				Projects: sidebar, ActiveProject: activeName, Global: globalMap, SkipProject: true,
			})
		} else {
			context = cache.GetProjectContext(target, func() map[string]any {
				return appctx.BuildContext(target, claudeHome, appctx.BuildOpts{
					Projects: sidebar, ActiveProject: activeName, Global: globalMap, SkipProject: false,
				})
			})
		}
		generatedAt := ""
		if agg != nil {
			generatedAt = md.FmtMtime(float64(agg.GeneratedAt) / 1000)
		}
		html := render.RenderPage(render.RenderOpts{
			Cwd: target, Context: context, Sidebar: sidebar, Active: selected,
			View: view, InitialView: initialView, InitialSub: initialSub,
			GeneratedAt: generatedAt, Loading: agg == nil,
		}, opts.Assets)
		writeHTML(w, html)
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pathname := r.URL.Path
		q := r.URL.Query()

		// Network gate (demo-5w3.2): only GET may arrive over a
		// non-loopback listener. Every mutating route this app has —
		// POST /mcp, POST /action/* (git add/commit/push/merge/branch -d)
		// — is a non-GET method, so this one check in front
		// of all routing below is equivalent to naming each of them and
		// covers any future non-GET route for free. It runs before the
		// path switch on purpose: unlike csrfOk (Origin, spoofable), this
		// is checked once, first, and cannot be bypassed by hitting a
		// route through some other path shape.
		//
		// netz-schreiben (#46): a peer IP listed in --write-clients (taken
		// from r.RemoteAddr, the socket, never from a header) may pass; the
		// request is then marked so csrfOkFuer holds its Origin to this
		// listener's own address.
		if r.Method != http.MethodGet && !requestIsLoopback(r) {
			if !opts.WriteAlle && !peerFreigegeben(opts.WriteClients, r) {
				http.Error(w, "Forbidden: schreibende Routen (POST) sind nur über 127.0.0.1 erreichbar oder von einer mit --write-clients freigegebenen IP, nicht von dieser über die Netz-Adresse.", http.StatusForbidden)
				return
			}
			r = alsNetzSchreiber(r, opts.PublicOrigin)
		}

		// Static assets
		if strings.HasPrefix(pathname, "/assets/") {
			assetHandler.ServeHTTP(w, r)
			return
		}

		// Browser fragen /favicon.ico von sich aus ab (Bookmark, alter Tab,
		// Verlauf), auch wenn jeder <head> ein rel="icon" nennt. Ohne diese
		// Zeile faellt der Pfad in die Projekt-Route unten durch und rendert
		// eine leere Projektansicht namens "favicon.ico" statt eines Icons —
		// deshalb stehen beide Namen auch in reservedSegments.
		if r.Method == http.MethodGet && (pathname == "/favicon.ico" || pathname == "/favicon.svg") {
			http.Redirect(w, r, "/assets/favicon.svg", http.StatusFound)
			return
		}

		// GET / — die Things-Oberflaeche (repo-leiste, #25). /?project=X bleibt
		// die alte Projektansicht (Links und Lesezeichen darauf), die alte
		// Uebersicht steht unter /ueberblick.
		if r.Method == http.MethodGet && pathname == "/" {
			if q.Get("project") == "" {
				writeHTML(w, render.ThingsPage(opts.Assets))
				return
			}
			servePage(w, q.Get("project"), "", "")
			return
		}

		// GET /ueberblick — die bisherige Uebersicht ueber alle Projekte.
		if r.Method == http.MethodGet && (pathname == "/ueberblick" || pathname == "/ueberblick/") {
			servePage(w, q.Get("project"), "", "")
			return
		}

		// GET /things — die Huelle der Things-Oberflaeche (things-oberflaeche,
		// #12). Sie montiert das Svelte-Bundle und holt ihre Daten aus /api/*;
		// die alte Ansicht unter / bleibt daneben unveraendert. "things" steht
		// auch in reservedSegments, damit /things/ keine Projektseite wird.
		if r.Method == http.MethodGet && (pathname == "/things" || pathname == "/things/") {
			writeHTML(w, render.ThingsPage(opts.Assets))
			return
		}

		// GET /read
		if r.Method == http.MethodGet && pathname == "/read" {
			target := getTarget(q.Get("project"))
			result := render.ReadDoc(target, claudeHome, q.Get("kind"), q.Get("name"), q.Get("path"))
			writeJSON(w, result)
			return
		}

		// GET /hook-inject
		if r.Method == http.MethodGet && pathname == "/hook-inject" {
			target := getTarget(q.Get("project"))
			writeJSON(w, collectors.CollectHookInject(target))
			return
		}

		// GET /github — Issues und PRs des origin-Remotes. Eigene Route und
		// nicht Teil von BuildContext: zwei Netzaufrufe gehoeren nicht in den
		// Pfad, der ein Projekt oeffnet (gkp-isj: das dauert schon zu lange).
		// Geholt wird, wenn jemand die Karte ansieht.
		if r.Method == http.MethodGet && pathname == "/github" {
			target := getTarget(q.Get("project"))
			writeJSON(w, collectors.CollectGitHubCached(target))
			return
		}

		// GET /ansichten?project= — die Aufgaben-Ansichten ueber BEIDE Quellen
		// (gkp-pf2). Eigene Route aus demselben Grund wie /github darunter: die
		// GitHub-Haelfte kostet einen Netzaufruf und darf den Seitenaufbau
		// nicht aufhalten. Der Rumpf der Seite bringt die beads-Zeilen mit, die
		// Ansicht holt sich hier die vollstaendige Liste nach — fertig
		// ausgewaehlt und sortiert, damit der Browser nichts zusammenfuegen
		// muss (CLAUDE.md: Auswahl und Sortierung gehoeren nach Go).
		//
		// OHNE project (gkp-92i): das Sammelaggregat ueber ALLE Projekte
		// (appctx.AnsichtenAlleProjekte / cache.GetAnsichtenAggregat, gkp-i90)
		// statt der Ein-Projekt-Antwort. GITHUB bleibt hier draussen — dieselbe
		// E1-Grenze wie in AnsichtenMitGithub, nur ueber n statt einem Projekt
		// noch teurer. cache.GetAnsichtenAggregat traegt sein eigenes
		// stale-serve + Boot-Primung (main.go), diese Route ruft nur ab.
		if r.Method == http.MethodGet && pathname == "/ansichten" {
			if q.Get("project") == "" {
				aktivesRepo := collectors.RepoRoot(opts.Cwd)
				agg := cache.GetAnsichtenAggregat(func() (map[string]appctx.AufgabenAnsicht, []appctx.ProjektFehler) {
					return appctx.AnsichtenAlleProjekte(aktivesRepo, anzeigezeit.Jetzt())
				})
				writeJSON(w, appctx.AnsichtenAggregatAntwort(agg.Ansichten, agg.ProjektFehler))
				return
			}
			target := getTarget(q.Get("project"))
			writeJSON(w, appctx.AnsichtenMitGithub(target))
			return
		}

		// GET /plan-svg?project=&map= — der Plan-Graph EINER Karte, eigene
		// Route aus demselben Grund wie /github (gkp-3k6): `bd graph --dot`
		// kostete 78-80 % der Zeit, ein Projekt zu oeffnen, und niemand sieht
		// den Graphen, bevor die Seite steht. Geholt wird, wenn jemand die
		// Karte ansieht; der Cache dahinter hat seine eigene, laengere TTL und
		// haengt nicht am reportsFingerprint.
		if r.Method == http.MethodGet && pathname == "/plan-svg" {
			target := getTarget(q.Get("project"))
			writeJSON(w, map[string]any{"svg": collectors.PlanSVGCached(target, q.Get("map"))})
			return
		}

		// GET /belege?project=&bead= — alle Report-Belege EINES Beads, ueber
		// alle Maps des Repos (gkp-e9h). Eigene Route aus demselben Grund wie
		// /github und /plan-svg daneben: sie hat einen eigenen, viel
		// billigeren I/O-Pfad und darf den, den die anderen kosten, nicht
		// erben. reports.LoadBead liest nur Dateien (reports.ListMaps) —
		// CollectBeads (und damit jeder bd-Aufruf) bleibt aussen vor. Ein
		// Bead ohne Fund in bd (verloren bei einem Datenbank-Merge: gkp-3sq,
		// gkp-e58, gkp-6am) beantwortet die Route trotzdem, weil der Store
		// nichts von bd weiss und nichts davon abfragt.
		if r.Method == http.MethodGet && pathname == "/belege" {
			target := getTarget(q.Get("project"))
			repo := collectors.ReportsRepoName(target)
			entries, skipped, err := reports.LoadBead(repo, q.Get("bead"))
			if err != nil {
				// Fail open, wie jede andere Lese-Route hier: ein kaputter
				// Store zeigt eine leere Liste plus Grund, nie einen 500.
				// skipped ist NICHT hart 0: LoadBead/ListMaps summiert sie
				// ueber bereits gelesene Dateien, bevor eine spaetere Datei den
				// Fehler ausloest (store.go ListMaps) — der Wert kann im
				// Fehlerfall > 0 sein und wird hier durchgereicht statt
				// verschluckt.
				writeJSON(w, map[string]any{"entries": []reports.Entry{}, "skipped": skipped, "error": err.Error()})
				return
			}
			writeJSON(w, map[string]any{"entries": entries, "skipped": skipped})
			return
		}

		// /api/* — die lesenden Routen der Things-Oberflaeche (projektliste-api,
		// #11). Nur GET; jede andere Methode bekommt 405 und aendert nichts.
		// Ueber den LAN-Listener kommt ein POST gar nicht bis hierher (Netz-Gate
		// oben). Ein unbekannter Pfad unter /api/ ist 404, kein Projektname.
		if pathname == "/api" || strings.HasPrefix(pathname, "/api/") {
			if r.Method != http.MethodGet {
				w.Header().Set("Allow", http.MethodGet)
				writeJSONStatus(w, http.StatusMethodNotAllowed, map[string]any{"grund": "unter /api/ ist nur GET erlaubt"})
				return
			}
			// Pfadbasiert (html-dateien, #112): OWNER/REPO/COMMIT/PFAD folgen
			// im Pfad, damit relative Verweise gegen denselben Commit laufen.
			if strings.HasPrefix(pathname, "/api/datei/") {
				repoHandler.serveDatei(w, r)
				return
			}
			switch pathname {
			case "/api/leiste":
				writeJSON(w, cache.GetLeiste(func(jetzt time.Time) appctx.Leiste {
					return leisteRechnen(leistenProjekte(getProjects), jetzt)
				}))
			case "/api/aufgaben":
				apiAufgaben(w, q.Get("project"), q.Get("eltern"), getProjects())
			case "/api/repos":
				repoHandler.serveRepos(w, r)
			case "/api/arbeit":
				repoHandler.serveArbeit(w, r)
			case "/api/detail":
				repoHandler.serveDetail(w, r)
			case "/api/blob":
				repoHandler.serveBlob(w, r)
			case "/api/sync":
				repoHandler.serveSync(w, r)
			case "/api/karte":
				repoHandler.serveKarte(w, r)
			case "/api/uebersicht":
				repoHandler.serveUebersicht(w, r)
			case "/api/repo-stand":
				repoHandler.serveRepoStand(w, r)
			case "/api/logbuch":
				repoHandler.serveLogbuch(w, r)
			case "/api/zeitleiste":
				repoHandler.serveZeitleiste(w, r)
			case "/api/repo-meta":
				repoHandler.serveRepoMeta(w, r)
			case "/api/beleg-galerie":
				repoHandler.serveBelegGalerie(w, r)
			case "/api/geaenderte-bilder":
				repoHandler.serveGeaenderteBilder(w, r)
			case "/api/html-dateien":
				repoHandler.serveHTMLDateien(w, r)
			case "/api/pr-konflikt":
				repoHandler.servePRKonflikt(w, r)
			case "/api/pr-optionen":
				repoHandler.servePROptionen(w, r)
			case "/api/pr-geschlossen":
				repoHandler.servePRGeschlossen(w, r)
			case "/api/branches":
				repoHandler.serveBranches(w, r)
			case "/api/ci":
				repoHandler.serveCI(w, r)
			case "/api/ci-log":
				repoHandler.serveCILog(w, r)
			default:
				writeJSONStatus(w, http.StatusNotFound, map[string]any{"grund": "unbekannte Route " + pathname})
			}
			return
		}

		// GET /gitdiff
		if r.Method == http.MethodGet && pathname == "/gitdiff" {
			target := getTarget(q.Get("project"))
			writeJSON(w, collectors.GitDiff(target, q.Get("path")))
			return
		}

		// POST /mcp — Streamable-HTTP MCP endpoint (demo-axk.3). Same
		// csrfOk guard as /action/*: Origin must be localhost if present;
		// an MCP client typically sends no Origin at all, which csrfOk
		// already allows.
		if r.Method == http.MethodPost && pathname == "/mcp" {
			if !csrfOkFuer(r) {
				http.Error(w, "CSRF: fremde Origin blockiert", http.StatusForbidden)
				return
			}
			mcpHandler.ServeHTTP(w, r)
			return
		}

		// GET /reports/<repo>/assets/<filename> — proof-image bytes
		// (demo-axk.8), same shape as /mcp above: own package, one route
		// here. Guard + Content-Type/nosniff rationale live in
		// internal/reportassets.
		if r.Method == http.MethodGet && strings.HasPrefix(pathname, "/reports/") {
			reportAssetsHandler.ServeHTTP(w, r)
			return
		}

		// /action/task/* — die vier Schreiboperationen der Ansicht (gkp-t1o).
		// Sie stehen VOR dem /action/-Block, weil sie mit JSON antworten statt
		// mit dessen HTML-Ergebnisseite; der Praefix ist derselbe, damit sie
		// unter demselben Netz-Gate liegen. Die Methode wird hier bewusst
		// nicht gefiltert: ein GET auf eine Schreibroute soll 405 bekommen und
		// nicht in die Projekt-Route unten fallen, die daraus eine Ansicht
		// namens "action" machen wuerde. Details: taskroutes.go.
		// /action/task/schliessen — Bead schliessen, MIT Belegpruefung
		// (gkp-bq9). Eigene Zeile VOR dem Praefix darunter, weil die Route
		// neben JSON auch ein hochgeladenes Bild entgegennimmt und damit einen
		// anderen Rumpf hat als die vier daneben; sie teilt sich mit ihnen
		// bewusst nur den Pfadanfang. Details: schliessroute.go.
		if pathname == schliessPfad {
			schliessHandler.ServeHTTP(w, r)
			return
		}

		// /action/issue/* — Schreibaktionen auf GitHub-Issues (issue-verwaltung,
		// #26). Vor dem /action/-Block, weil sie JSON antworten; Details in
		// issueroutes.go.
		if strings.HasPrefix(pathname, issuePraefix) {
			issueRoutes{d: repoHandler}.ServeHTTP(w, r)
			return
		}
		// /action/pr/* — Freigeben, Mergen, Branch loeschen (pr-aktionen, #32).
		if strings.HasPrefix(pathname, prPraefix) {
			prRoutes{issueRoutes{d: repoHandler}}.ServeHTTP(w, r)
			return
		}
		// /action/branch/* — Branch anlegen (pr-anlegen, #79) und loeschen
		// (branch-aufraeumen, #78); Details in
		// branchroutes.go.
		if strings.HasPrefix(pathname, branchPraefix) {
			branchRoutes{issueRoutes{d: repoHandler}}.ServeHTTP(w, r)
			return
		}

		// /action/ci/* — fehlgeschlagene Jobs eines Runs neu starten
		// (ci-runs, #122); Details in ciruns.go.
		if strings.HasPrefix(pathname, ciPraefix) {
			ciRoutes{issueRoutes{d: repoHandler}}.ServeHTTP(w, r)
			return
		}

		if strings.HasPrefix(pathname, taskPraefix) {
			taskHandler.ServeHTTP(w, r)
			return
		}

		// POST /action/*
		if r.Method == http.MethodPost && strings.HasPrefix(pathname, "/action/") {
			if !csrfOkFuer(r) {
				writeHTML(w, render.ResultPage("blocked", map[string]any{"ok": false, "cmd": "-", "output": "CSRF: fremde Origin blockiert"}))
				return
			}
			if err := parseForm(r); err != nil {
				http.Error(w, "Bad Request", http.StatusBadRequest)
				return
			}
			project := r.FormValue("project")
			target := getTarget(project)
			action := strings.TrimPrefix(pathname, "/action/")

			var result map[string]any
			switch action {
			case "add":
				result = collectors.GitAdd(target)
			case "commit":
				result = collectors.GitCommit(target, r.FormValue("message"))
			case "delete":
				branch := strings.TrimSpace(r.FormValue("branch"))
				confirm := strings.TrimSpace(r.FormValue("confirm"))
				if branch == "" || confirm != branch {
					result = map[string]any{"ok": false, "cmd": "git branch -d", "output": "Bestätigung: tippe den exakten Branch-Namen."}
				} else {
					result = collectors.GitDelete(target, branch)
				}
			case "merge":
				if strings.TrimSpace(r.FormValue("confirm")) != "MERGE" {
					result = map[string]any{"ok": false, "cmd": "git merge", "output": "Bestätigung: tippe MERGE."}
				} else {
					result = collectors.GitMerge(target, r.FormValue("branch"))
				}
			case "push":
				if strings.TrimSpace(r.FormValue("confirm")) != "PUSH" {
					result = map[string]any{"ok": false, "cmd": "git push", "output": "Bestätigung: tippe PUSH."}
				} else {
					result = collectors.GitPush(target, r.FormValue("branch"))
				}
			default:
				http.Error(w, "Not found", http.StatusNotFound)
				return
			}

			if result["ok"] == true {
				cache.InvalidateProjectContext(target)
			}
			writeHTML(w, render.ResultPage(action, result))
			return
		}

		// Path-based routing: GET /<view>[/<sub>] (Sammelansicht, gkp-92i) oder
		// GET /<project>[/<view>[/<sub>]] (ein Projekt).
		if r.Method == http.MethodGet {
			segments := []string{}
			for _, s := range strings.Split(strings.TrimPrefix(pathname, "/"), "/") {
				if s != "" {
					segments = append(segments, s)
				}
			}
			if len(segments) >= 1 {
				first := decodeURI(segments[0])
				// /<view> OHNE Projektsegment oeffnet die Ansicht ueber ALLE
				// Projekte (gkp-92i) — MUSS vor der Projekt-Deutung greifen,
				// sonst waere "heute" ein Projektname ohne eigene Ansicht:
				// dieselbe Fehlerklasse, die reservedSegments schon fuer
				// /favicon.ico vermeidet (Kommentar dort). project bleibt leer,
				// servePage rendert dann die Uebersicht (agg.Global/Projects)
				// mit "heute" als InitialView statt eines Projekt-Kontexts.
				if ansichtSegmente[first] {
					subHint := ""
					if len(segments) >= 2 {
						subHint = decodeURI(segments[1])
					}
					servePage(w, "", first, subHint)
					return
				}
				if !reservedSegments[segments[0]] {
					projectName := first
					viewHint, subHint := "", ""
					if len(segments) >= 2 {
						viewHint = decodeURI(segments[1])
					}
					// Drittes Segment: die Auswahl INNERHALB einer Ansicht, heute die
					// Epic-Id unter /reports (gkp-ar7.28). Es wird hier nur
					// weitergereicht, nicht geprueft — die Seite waehlt damit aus
					// den Karten aus, die sie ohnehin geladen hat, und eine
					// unbekannte Id fuehrt zu keinem Nachladen.
					if len(segments) >= 3 {
						subHint = decodeURI(segments[2])
					}
					servePage(w, projectName, viewHint, subHint)
					return
				}
			}
		}

		http.Error(w, "Not found", http.StatusNotFound)
	})
}

func parseForm(r *http.Request) error {
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
		return r.ParseMultipartForm(32 << 20)
	}
	return r.ParseForm()
}

func decodeURI(s string) string {
	if d, err := url.PathUnescape(s); err == nil {
		return d
	}
	return s
}

func lastSeg(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' {
			return p[i+1:]
		}
	}
	return p
}

// noStore marks a response as never-cacheable.
//
// gkp-ar7.18: every page this app serves is a snapshot rendered at request
// time — window.DATA is embedded into the HTML, and browser.css/browser.js are
// inlined rather than fetched from /assets/. Nothing here carried a
// Cache-Control header, nor an ETag or Last-Modified, so a browser was free to
// serve a heuristically cached copy of the page and had no validator to
// revalidate with. The visible effect: after a deploy the tab keeps showing the
// previous state, which looks exactly like a failed deploy and is impossible to
// tell apart from one by looking at the browser.
//
// no-store rather than no-cache: there is nothing worth storing. The document
// is only ever valid for the instant it was generated, and a conditional
// request would have to recompute the whole project context to answer it, so a
// validator would buy nothing.
func noStore(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
}

func writeHTML(w http.ResponseWriter, html string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	noStore(w)
	_, _ = w.Write([]byte(html))
}

func writeJSON(w http.ResponseWriter, v any) {
	writeJSONStatus(w, http.StatusOK, v)
}

// writeJSONStatus is writeJSON with an explicit status. The schreibende Routen
// under /action/task/* answer errors as JSON too (gkp-t1o), so the body shape
// must not depend on whether the request succeeded — only the status does.
func writeJSONStatus(w http.ResponseWriter, status int, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		http.Error(w, "json error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	// Same reasoning as writeHTML: /read, /gitdiff and /hook-inject answer
	// with live state that browser.js fetches on demand.
	noStore(w)
	w.WriteHeader(status)
	_, _ = w.Write(b)
}
