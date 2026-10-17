package cache

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/niclasedge/git-planner-go/internal/collectors"
	"github.com/niclasedge/git-planner-go/internal/reports"
)

// TestGetProjectContext_CachesAcrossCalls is the cache-wiring contract
// cc-setup-axk.4 depends on: CollectBeads/CollectReportsData shell out to
// git/bd/dot per call, so the reports card must ride this per-project cache
// like every other card (project/git/knowledge/...) instead of the app
// re-running those subprocesses on every page render. This test does not
// touch beads/reports at all — it exercises the generic mechanism those
// packages plug into, the same way project.go/git.go/etc. already do.
func TestGetProjectContext_CachesAcrossCalls(t *testing.T) {
	SetStartedForTest(true)
	defer SetStartedForTest(false)
	ResetProjectContextCacheForTest()
	defer ResetProjectContextCacheForTest()

	calls := 0
	compute := func() map[string]any {
		calls++
		return map[string]any{"calls": calls}
	}
	const key = "/tmp/cc-setup-axk-cache-test-repo"

	first := GetProjectContext(key, compute)
	second := GetProjectContext(key, compute)

	if calls != 1 {
		t.Fatalf("expected computeFn to run once within the TTL, ran %d times", calls)
	}
	if first["calls"] != second["calls"] {
		t.Fatalf("expected the second call to return the cached result, got %v vs %v", first, second)
	}
}

// TestGetProjectContext_InvalidateForcesRecompute confirms the escape hatch
// server.go already uses after a mutating git action (InvalidateProjectContext)
// also applies to a reports card sitting alongside it in the same cached
// context — one shared cache entry per project, not one per card.
func TestGetProjectContext_InvalidateForcesRecompute(t *testing.T) {
	SetStartedForTest(true)
	defer SetStartedForTest(false)
	ResetProjectContextCacheForTest()
	defer ResetProjectContextCacheForTest()

	calls := 0
	compute := func() map[string]any {
		calls++
		return map[string]any{"calls": calls}
	}
	const key = "/tmp/cc-setup-axk-cache-test-repo-2"

	GetProjectContext(key, compute)
	InvalidateProjectContext(key)
	GetProjectContext(key, compute)

	if calls != 2 {
		t.Fatalf("expected computeFn to run again after InvalidateProjectContext, ran %d times", calls)
	}
}

// TestGetProjectContext_RecomputesWhenReportsMtimeChanges is finding 2's live
// repro: cards["reports"] used to hang behind the TTL with NO invalidation
// path for a report written via MCP (InvalidateProjectContext is only ever
// called from server.go's /action/* routes, which an MCP ingest never
// touches) — so a freshly posted report stayed invisible for up to
// TTLMs() minutes. This never calls InvalidateProjectContext at all; the
// mtime fingerprint alone must force the recompute.
func TestGetProjectContext_RecomputesWhenReportsMtimeChanges(t *testing.T) {
	SetStartedForTest(true)
	defer SetStartedForTest(false)
	ResetProjectContextCacheForTest()
	defer ResetProjectContextCacheForTest()

	key := t.TempDir() // stands in for the resolved project cwd
	t.Setenv("CC_REPORTS_DIR", t.TempDir())

	repoName := collectors.ReportsRepoName(key)
	mapDir := filepath.Join(reports.BaseDir(), repoName)
	if err := os.MkdirAll(mapDir, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(mapDir, "somemap.jsonl")
	if err := os.WriteFile(file, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	baseTime := time.Now().Add(-time.Hour)
	if err := os.Chtimes(file, baseTime, baseTime); err != nil {
		t.Fatal(err)
	}

	calls := 0
	compute := func() map[string]any {
		calls++
		return map[string]any{"calls": calls}
	}

	first := GetProjectContext(key, compute)
	second := GetProjectContext(key, compute)
	if calls != 1 {
		t.Fatalf("expected computeFn to run once while the report file's mtime is unchanged, ran %d times", calls)
	}
	if first["calls"] != second["calls"] {
		t.Fatalf("expected the second call to be served from cache, got %v vs %v", first, second)
	}

	// Simulate a fresh MCP ingest appending to the SAME map file — deliberately
	// never calling InvalidateProjectContext, since an MCP ingest has no path
	// to it today (cc-setup-axk.4 finding 2's actual gap).
	newTime := baseTime.Add(time.Minute)
	if err := os.Chtimes(file, newTime, newTime); err != nil {
		t.Fatal(err)
	}

	// gkp-n5q changed WHEN this recompute happens, not WHETHER. The changed
	// fingerprint still forces it, but the caller is no longer the one paying
	// for it: it gets the stale entry now and the fresh one on the next
	// request. Measured at the running service, that turned this exact case —
	// a report write — from 5,25 s into 0,003 s.
	third := GetProjectContext(key, compute)
	if third["calls"] != first["calls"] {
		t.Fatalf("expected the stale entry while the recompute runs, got %v (first was %v)", third, first)
	}

	WaitForProjectContextForTest(key)
	if calls != 2 {
		t.Fatalf("expected computeFn to rerun in the background once the mtime advanced, ran %d times", calls)
	}

	fourth := GetProjectContext(key, compute)
	if calls != 2 {
		t.Fatalf("expected computeFn to stay cached once the fingerprint stopped changing, ran %d times", calls)
	}
	if fourth["calls"] == first["calls"] {
		t.Fatalf("expected the fourth call to see the recomputed result, still got %v", fourth)
	}
}

// TestGetProjectContext_LiveWhenNotStarted documents the "test mode: always
// live" branch (GetProjectContext, cache.go) — with StartCache never having
// run, every call recomputes. Existing appctx tests (BuildContext with
// SkipProject=false against a bare temp dir) rely on exactly this to avoid
// stale/cross-test pollution without calling StartCache themselves.
func TestGetProjectContext_LiveWhenNotStarted(t *testing.T) {
	SetStartedForTest(false)
	ResetProjectContextCacheForTest()
	defer ResetProjectContextCacheForTest()

	calls := 0
	compute := func() map[string]any {
		calls++
		return map[string]any{"calls": calls}
	}
	const key = "/tmp/cc-setup-axk-cache-test-repo-3"

	GetProjectContext(key, compute)
	GetProjectContext(key, compute)

	if calls != 2 {
		t.Fatalf("expected computeFn to run on every call when the cache was never started, ran %d times", calls)
	}
}

// ---------------------------------------------------------------- gkp-n5q

// TestGetProjectContext_AbgelaufeneTTLWartetNicht ist die Zusage von gkp-n5q
// fuer den TTL-Fall: der Eintrag ist abgelaufen, aber er ist DA — dann wird er
// ausgeliefert und im Hintergrund erneuert, statt den Anfragenden warten zu
// lassen. Gemessen am laufenden Dienst: 4,4 s bzw. 26,7 s -> 0,01 s.
//
// Der Preis ist ein Renderdurchgang mit alten Daten. Er ist nicht neu: eine
// Millisekunde vorher haette dieselbe TTL denselben Wert ohne jede Nachfrage
// herausgegeben.
func TestGetProjectContext_AbgelaufeneTTLWartetNicht(t *testing.T) {
	SetStartedForTest(true)
	defer SetStartedForTest(false)
	ResetProjectContextCacheForTest()
	defer ResetProjectContextCacheForTest()
	t.Setenv("CC_KNOWLEDGE_TTL_MS", "1")

	fertig := make(chan struct{})
	calls := 0
	compute := func() map[string]any {
		calls++
		n := calls
		if n > 1 {
			// Der Hintergrundlauf blockiert, bis der Test ihn freigibt: nur so
			// ist beweisbar, dass der Aufrufer NICHT auf ihn gewartet hat.
			<-fertig
		}
		return map[string]any{"calls": n}
	}
	key := t.TempDir()

	first := GetProjectContext(key, compute)
	time.Sleep(5 * time.Millisecond) // TTL von 1 ms sicher ueberschreiten

	second := GetProjectContext(key, compute)
	if second["calls"] != first["calls"] {
		t.Fatalf("erwartet den abgelaufenen Eintrag sofort, bekam %v (erster war %v)", second, first)
	}

	close(fertig)
	WaitForProjectContextForTest(key)
	if calls != 2 {
		t.Fatalf("erwartet genau einen Nachlauf im Hintergrund, computeFn lief %d mal", calls)
	}
}

// Ein KALTER Schluessel muss weiter blockieren. Es gibt nichts, was man
// stattdessen zeigen koennte, und eine leere Seite ist schlechter als eine
// langsame — die Unterscheidung ist der ganze Unterschied zwischen
// stale-while-revalidate und "manchmal kein Inhalt".
func TestGetProjectContext_KalterSchluesselWartetWeiter(t *testing.T) {
	SetStartedForTest(true)
	defer SetStartedForTest(false)
	ResetProjectContextCacheForTest()
	defer ResetProjectContextCacheForTest()

	calls := 0
	compute := func() map[string]any {
		calls++
		return map[string]any{"calls": calls}
	}
	key := t.TempDir()

	got := GetProjectContext(key, compute)

	if calls != 1 {
		t.Fatalf("computeFn lief %d mal, erwartet 1", calls)
	}
	if got == nil || got["calls"] != 1 {
		t.Fatalf("kalter Schluessel lieferte %v — erwartet das Ergebnis des Laufs, nicht nil", got)
	}
}

// Ein Ansturm auf denselben abgelaufenen Schluessel darf genau EINEN Nachlauf
// ausloesen, nicht einen je Anfrage. Ohne das waere stale-while-revalidate ein
// Verstaerker: je langsamer der Nachlauf, desto mehr Nachlaeufe.
func TestGetProjectContext_NurEinNachlaufProSchluessel(t *testing.T) {
	SetStartedForTest(true)
	defer SetStartedForTest(false)
	ResetProjectContextCacheForTest()
	defer ResetProjectContextCacheForTest()
	t.Setenv("CC_KNOWLEDGE_TTL_MS", "1")

	fertig := make(chan struct{})
	var mu sync.Mutex
	calls := 0
	compute := func() map[string]any {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		if n > 1 {
			<-fertig
		}
		return map[string]any{"calls": n}
	}
	key := t.TempDir()

	GetProjectContext(key, compute)
	time.Sleep(5 * time.Millisecond)

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			GetProjectContext(key, compute)
		}()
	}
	wg.Wait()

	close(fertig)
	WaitForProjectContextForTest(key)

	mu.Lock()
	defer mu.Unlock()
	if calls != 2 {
		t.Fatalf("20 gleichzeitige Anfragen loesten %d Laeufe aus, erwartet 2 (erster + ein Nachlauf)", calls)
	}
}

// TestPrimeAllProjects waermt jedes Projekt der Seitenleiste, nicht nur das
// Startprojekt (gkp-n5q A). Vorher kostete der erste Klick auf ein anderes
// Projekt dessen volle Kaltzeit — bis zu 39,63 s gemessen.
func TestPrimeAllProjects(t *testing.T) {
	SetStartedForTest(true)
	defer SetStartedForTest(false)
	ResetProjectContextCacheForTest()
	defer ResetProjectContextCacheForTest()
	t.Setenv("CC_REPORTS_DIR", t.TempDir())

	cwd := t.TempDir()
	zweites := t.TempDir()
	drittes := t.TempDir()
	agg := &Aggregate{Projects: []collectors.SidebarProject{
		{ProjectRef: collectors.ProjectRef{Name: filepath.Base(cwd), Path: cwd}},
		{ProjectRef: collectors.ProjectRef{Name: filepath.Base(zweites), Path: zweites}},
		{ProjectRef: collectors.ProjectRef{Name: filepath.Base(drittes), Path: drittes}},
		{ProjectRef: collectors.ProjectRef{Name: "ohne-pfad", Path: ""}},
	}}

	PrimeAllProjects(cwd, t.TempDir(), agg)

	for _, p := range []string{cwd, zweites, drittes} {
		lief := false
		GetProjectContext(p, func() map[string]any {
			lief = true
			return map[string]any{}
		})
		if lief {
			t.Errorf("%s war nach dem Vorwaermen kalt", p)
		}
	}
}
