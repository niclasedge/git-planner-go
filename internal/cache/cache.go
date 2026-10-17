// Package cache is the file-backed aggregate cache + per-project context cache —
// Go-native port of cache.ts. The Deno single-flight (shared promise) becomes a
// mutex + done-channel; otherwise behavior (TTL, lazy refresh, generation-guarded
// writes) matches.
package cache

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/niclasedge/git-planner-go/internal/appctx"
	"github.com/niclasedge/git-planner-go/internal/collectors"
	"github.com/niclasedge/git-planner-go/internal/reports"
)

// Aggregate caches the expensive shared data: the global layer + the
// multi-project sidebar.
type Aggregate struct {
	GeneratedAt int64                       `json:"generated_at"` // epoch ms
	Global      map[string]any              `json:"global"`
	Projects    []collectors.SidebarProject `json:"projects"`
}

func nowMs() int64 { return time.Now().UnixMilli() }

// CacheFile returns the on-disk cache path (CC_KNOWLEDGE_CACHE overrides).
func CacheFile() string {
	if env := os.Getenv("CC_KNOWLEDGE_CACHE"); env != "" {
		return env
	}
	return filepath.Join(collectors.Home(), ".cache", "cc-knowledge", "cache.json")
}

// TTLMs returns the cache TTL (CC_KNOWLEDGE_TTL_MS overrides; default 15 min).
//
// Die 15 Minuten sind UNBEGRUENDET. Das ist kein Versaeumnis dieses Kommentars,
// sondern der Befund aus gkp-mnd: der Wert stammt aus der Deno-Fassung
// (cache.ts) und ist seither nie gegen eine Messung gehalten worden. Was
// gemessen ist, ist der Preis seines Ablaufs — ein kalter BuildContext fuer
// dieses Repo kostete am 2026-09-14 2,16 s (CollectTokens 1,26 s,
// CollectReportsData 1,42 s, der Rest zusammen 0,13 s).
//
// Was die Zahl entscheidet, ist gerade NICHT die Wartezeit: seit gkp-n5q wird
// ein abgelaufener Eintrag sofort ausgeliefert und im Hintergrund erneuert
// (GetProjectContext), ein Besucher wartet also nur auf einen KALTEN
// Schluessel. Die TTL bestimmt deshalb zweierlei — wie alt die einmal gezeigte
// Seite hoechstens ist, und wie oft der Rechner ohne Anlass 2,16 s je Projekt
// rechnet. Beides ist eine Nutzerentscheidung, keine Messgroesse; wer sie
// treffen will, hat hiermit die Zahlen dazu.
//
// Sie ist zugleich die Obergrenze fuer gkp-ar7.19: ein ueber den MCP angelegter
// Bead ist bis zum Ablauf unsichtbar, weil beads_create/update/dep den Cache
// nicht entwerten (nur `report` tut es ueber reportsFingerprint).
func TTLMs() int64 {
	if env := os.Getenv("CC_KNOWLEDGE_TTL_MS"); env != "" {
		if v, err := strconv.ParseInt(env, 10, 64); err == nil && v > 0 {
			return v
		}
		if f, err := strconv.ParseFloat(env, 64); err == nil && f > 0 {
			return int64(f)
		}
	}
	return 15 * 60 * 1000
}

// IsFresh is a pure freshness predicate.
func IsFresh(agg *Aggregate, ttl, now int64) bool {
	return agg != nil && (now-agg.GeneratedAt) < ttl
}

func isProjectContextFresh(e *projEntry, ttl, now int64) bool {
	return e != nil && (now-e.generatedAt) < ttl
}

// ReadCacheFile loads a cache file, returning nil if missing/invalid.
func ReadCacheFile(path string) *Aggregate {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var agg Aggregate
	if json.Unmarshal(b, &agg) != nil {
		return nil
	}
	if agg.Projects == nil {
		return nil // mirrors the Array.isArray(projects) guard
	}
	return &agg
}

// WriteCacheFile persists the aggregate (best-effort).
func WriteCacheFile(agg *Aggregate, path string) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	b, err := json.Marshal(agg)
	if err != nil {
		return
	}
	_ = os.WriteFile(path, b, 0o644)
}

// ComputeAggregate collects the global layer + sidebar fresh.
func ComputeAggregate(activeRepo, home string, now int64) Aggregate {
	return Aggregate{
		GeneratedAt: now,
		Global:      collectors.CollectGlobal(home),
		Projects:    collectors.CollectSidebar(activeRepo),
	}
}

var (
	mu          sync.Mutex
	current     *Aggregate
	refreshing  bool
	refreshDone chan struct{}
	started     bool
)

// GetAggregate returns the latest in-memory aggregate (or nil).
func GetAggregate() *Aggregate {
	mu.Lock()
	defer mu.Unlock()
	return current
}

// IsRefreshing reports whether an aggregate refresh is in flight.
func IsRefreshing() bool {
	mu.Lock()
	defer mu.Unlock()
	return refreshing
}

// RefreshAggregate runs a single-flight aggregate computation, blocking until it
// completes (concurrent callers wait on the same run). Call in a goroutine for
// fire-and-forget behavior.
func RefreshAggregate(activeRepo, home string) {
	mu.Lock()
	if refreshing {
		ch := refreshDone
		mu.Unlock()
		<-ch
		return
	}
	refreshing = true
	refreshDone = make(chan struct{})
	ch := refreshDone
	mu.Unlock()

	next := ComputeAggregate(activeRepo, home, nowMs())

	mu.Lock()
	current = &next
	refreshing = false
	refreshDone = nil
	mu.Unlock()

	WriteCacheFile(&next, CacheFile())
	close(ch)
}

// EnsureFresh kicks off a refresh ONLY if the cache is stale AND none is running.
// Non-blocking, fire-and-forget.
func EnsureFresh(activeRepo, home string) {
	mu.Lock()
	if !started || refreshing || IsFresh(current, TTLMs(), nowMs()) {
		mu.Unlock()
		return
	}
	mu.Unlock()
	go RefreshAggregate(activeRepo, home)
}

// StartCache primes once on boot: load a fresh cache file if present, else
// compute one aggregate (single-flight). No background timer.
func StartCache(activeRepo, home string) {
	mu.Lock()
	started = true
	mu.Unlock()
	fromFile := ReadCacheFile(CacheFile())
	if IsFresh(fromFile, TTLMs(), nowMs()) {
		mu.Lock()
		current = fromFile
		mu.Unlock()
		return
	}
	RefreshAggregate(activeRepo, home)
}

// ---------------------------------------------------------------------------
// Per-project detail cache
// ---------------------------------------------------------------------------

type projEntry struct {
	generatedAt int64
	context     map[string]any
	// reportsGuard is the reportsFingerprint() value captured when this entry
	// was computed (cc-setup-axk.4 review Welle 2, finding 2). A mismatch on
	// lookup forces a recompute even within TTLMs() — see GetProjectContext.
	reportsGuard string
}

type projCall struct {
	done   chan struct{}
	result map[string]any
}

var (
	pcMu                     sync.Mutex
	projectContextCache      = map[string]projEntry{}
	projectContextInFlight   = map[string]*projCall{}
	projectContextGeneration = map[string]int{}
)

// GetProjectContext returns a cached project context or starts a single-flight
// computation. projectKey is the resolved absolute project path.
//
// cc-setup-axk.4 review Welle 2, finding 2: cards["reports"] used to sit
// behind this cache with no invalidation path other than the /action/* routes
// (server.go) calling InvalidateProjectContext — a report posted via MCP
// (which never touches those routes) stayed invisible for up to TTLMs()
// (default 15 min). Rather than give the reports card its own cache (which
// would mean CollectBeads' bd/dot subprocesses either run on every request or
// need a second, separately-wired cache) or reach into internal/mcp from here
// (out of scope, and it doesn't know the projectKey this cache is addressed
// by anyway), every lookup now also compares reportsFingerprint(projectKey) —
// cheap: a memoized repo-name resolution (collectors.ReportsRepoName) plus a
// stat of each *.jsonl file under that repo's report dir — against the value
// captured when the entry was last computed. A mismatch forces a recompute
// exactly like an expired TTL, so a freshly written report (from MCP or
// anywhere else) is visible on the very next request, while the actually
// expensive part (bd graph, dot, inside CollectBeads) still only reruns when
// something about the project actually changed.
// gkp-n5q: a STALE entry is now served as-is and refreshed in the background,
// instead of making the caller wait for the recompute. Measured at the running
// service: the report-write case (fingerprint changed) 5,25 s -> 0,003 s, the
// expired-TTL case 4,4 s resp. 26,7 s -> 0,01 s.
//
// EnsureFresh already does exactly this for the aggregate; this is the same
// answer one level down. The price is that the user sees one render pass of
// slightly old data — for a dashboard that is the right price, and it is the
// same staleness the TTL already permitted a millisecond earlier.
//
// Only a COLD key (nothing cached at all) still blocks: there is nothing to
// show instead, and an empty page is worse than a slow one.
func GetProjectContext(projectKey string, computeFn func() map[string]any) map[string]any {
	mu.Lock()
	st := started
	mu.Unlock()
	if !st {
		return computeFn() // test mode: always live
	}

	now := nowMs()
	ttl := TTLMs()
	guard := reportsFingerprint(projectKey)
	pcMu.Lock()
	if e, ok := projectContextCache[projectKey]; ok {
		if isProjectContextFresh(&e, ttl, now) && e.reportsGuard == guard {
			pcMu.Unlock()
			return e.context
		}
		// Stale but present: hand it out now, refresh behind the caller's
		// back. startRecomputeLocked is a no-op when one is already running,
		// so a burst of requests triggers one recompute, not one each.
		stale := e.context
		startRecomputeLocked(projectKey, guard, computeFn)
		pcMu.Unlock()
		return stale
	}
	if c, ok := projectContextInFlight[projectKey]; ok {
		pcMu.Unlock()
		<-c.done
		return c.result
	}
	c := startRecomputeLocked(projectKey, guard, computeFn)
	pcMu.Unlock()
	<-c.done
	return c.result
}

// startRecomputeLocked starts a single-flight recompute for projectKey and
// returns the call to wait on. Caller must hold pcMu; the goroutine takes it
// again to store the result.
//
// The generation check is unchanged from the blocking version and matters more
// now, not less: a background recompute outlives the request that started it,
// so an InvalidateProjectContext landing in between must still be able to
// discard its result.
func startRecomputeLocked(projectKey, guard string, computeFn func() map[string]any) *projCall {
	if c, ok := projectContextInFlight[projectKey]; ok {
		return c
	}
	genAtStart := projectContextGeneration[projectKey]
	c := &projCall{done: make(chan struct{})}
	projectContextInFlight[projectKey] = c

	go func() {
		ctx := computeFn()
		pcMu.Lock()
		if projectContextGeneration[projectKey] == genAtStart {
			projectContextCache[projectKey] = projEntry{generatedAt: nowMs(), context: ctx, reportsGuard: guard}
		}
		delete(projectContextInFlight, projectKey)
		c.result = ctx
		close(c.done)
		pcMu.Unlock()
	}()
	return c
}

// reportsFingerprint is the cheap freshness signal described above: the
// resolved reports-store repo name plus the newest mtime and file count
// across ~/.claude/reports/<repo>/*.jsonl (CC_REPORTS_DIR-relative, via
// reports.BaseDir — never a hardcoded path, so tests that set that env var
// stay isolated). A cwd with no reports yet (or that isn't a beads repo at
// all) fingerprints to a stable "<name>|0|0" — no reports directory ever
// existing is itself a valid, unchanging fingerprint, so such a project never
// gets spuriously invalidated.
func reportsFingerprint(cwd string) string {
	name := collectors.ReportsRepoName(cwd)
	dir := filepath.Join(reports.BaseDir(), name)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return name + "|0|0"
	}
	var maxMtime int64
	count := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		count++
		if info, err := e.Info(); err == nil {
			if mt := info.ModTime().UnixNano(); mt > maxMtime {
				maxMtime = mt
			}
		}
	}
	return name + "|" + strconv.FormatInt(maxMtime, 10) + "|" + strconv.Itoa(count)
}

// InvalidateProjectContext drops a project's cache entry + bumps its generation
// (so a late-finishing compute discards its stale result).
func InvalidateProjectContext(projectKey string) {
	pcMu.Lock()
	delete(projectContextCache, projectKey)
	projectContextGeneration[projectKey]++
	pcMu.Unlock()
}

// PrimeProjectContext computes the start project's context once on boot.
func PrimeProjectContext(cwd, claudeHome string, agg *Aggregate) {
	mu.Lock()
	st := started
	mu.Unlock()
	if !st {
		return
	}
	pcMu.Lock()
	e, ok := projectContextCache[cwd]
	fresh := ok && isProjectContextFresh(&e, TTLMs(), nowMs())
	pcMu.Unlock()
	if fresh {
		return
	}
	var projects []collectors.SidebarProject
	var global map[string]any
	if agg != nil {
		projects = agg.Projects
		global = agg.Global
	}
	GetProjectContext(cwd, func() map[string]any {
		return appctx.BuildContext(cwd, claudeHome, appctx.BuildOpts{
			Projects:      projects,
			ActiveProject: lastSeg(cwd),
			Global:        global,
			SkipProject:   false,
		})
	})
}

// PrimeAllProjects warms EVERY project in the sidebar, not just the one the
// server was started in (gkp-n5q).
//
// Priming only the start project is why the first click on a different project
// cost its full cold time. Measured after a restart, before this: / 0,010 s,
// /pkm 1,53 s, /git-planner-go 4,39 s, /trey 4,46 s, /IaC-Stack 39,63 s
// — every one of them warm at 3-10 ms afterwards. The cold time never went
// away, it just waited for whoever clicked first.
//
// Sequential on purpose. Every one of these calls ends up in bd, and bd
// serializes on its Dolt DB anyway; running them in parallel would not finish
// sooner and would put N bd processes on the machine at boot. cwd goes first
// because it is the project someone is most likely to open.
//
// Runs in the caller's goroutine — main starts it in the background, and it is
// pure warming: everything it does, a request would otherwise do itself.
func PrimeAllProjects(cwd, claudeHome string, agg *Aggregate) {
	PrimeProjectContext(cwd, claudeHome, agg)
	if agg == nil {
		return
	}
	for _, p := range agg.Projects {
		if p.Path == "" || p.Path == cwd {
			continue
		}
		PrimeProjectContext(p.Path, claudeHome, agg)
	}
}

func lastSeg(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' {
			return p[i+1:]
		}
	}
	return p
}

// WaitForProjectContextForTest blocks until any in-flight recompute for
// projectKey has finished. Test helper only — since gkp-n5q a stale entry is
// refreshed in the background, so a test that wants to observe the NEW value
// has to wait for it; the live path never does.
func WaitForProjectContextForTest(projectKey string) {
	pcMu.Lock()
	c, ok := projectContextInFlight[projectKey]
	pcMu.Unlock()
	if ok {
		<-c.done
	}
}

// Test helpers.
func ResetProjectContextCacheForTest() {
	pcMu.Lock()
	projectContextCache = map[string]projEntry{}
	projectContextInFlight = map[string]*projCall{}
	projectContextGeneration = map[string]int{}
	pcMu.Unlock()
}

func SetStartedForTest(v bool) {
	mu.Lock()
	started = v
	mu.Unlock()
}
