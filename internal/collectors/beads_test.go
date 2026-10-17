package collectors

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/niclasedge/git-planner-go/internal/shared"
)

func TestParseWorktreePorcelain(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []BeadsWorktree
	}{
		{
			name: "empty",
			in:   "",
			want: []BeadsWorktree{},
		},
		{
			name: "two worktrees, one detached",
			in: "worktree /home/user/git/demo\n" +
				"HEAD f123f8f1241b270726efd2d17cee6c1f34c4cbc8\n" +
				"branch refs/heads/main\n" +
				"\n" +
				"worktree /home/user/git/demo/.claude/worktrees/ga\n" +
				"HEAD 00c7bf7391a717ba9d1e68384f0fa720bffa5046\n" +
				"branch refs/heads/feat/ga-pr-verdrahtung\n",
			want: []BeadsWorktree{
				{Path: "/home/user/git/demo", Branch: "main"},
				{Path: "/home/user/git/demo/.claude/worktrees/ga", Branch: "feat/ga-pr-verdrahtung"},
			},
		},
		{
			name: "detached worktree has no branch line",
			in: "worktree /tmp/detached\n" +
				"HEAD abc123\n" +
				"detached\n",
			want: []BeadsWorktree{
				{Path: "/tmp/detached", Branch: ""},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseWorktreePorcelain(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("got %d worktrees, want %d: %+v", len(got), len(tc.want), got)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("entry %d: got %+v, want %+v", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestParseBdChildren(t *testing.T) {
	cases := []struct {
		name  string
		raw   string
		mapID string
		want  int
	}{
		{
			name:  "empty input",
			raw:   "",
			mapID: "demo-axk",
			want:  0,
		},
		{
			name:  "unknown map id (bd exits 0 with a bare schema stub)",
			raw:   `{"schema_version": 1}`,
			mapID: "demo-axk",
			want:  0,
		},
		{
			name:  "leaf with no children",
			raw:   `{"demo-axk.2": [], "schema_version": 1}`,
			mapID: "demo-axk.2",
			want:  0,
		},
		{
			name: "two children",
			raw: `{"demo-axk": [
				{"id":"demo-axk.1","title":"a","status":"in_progress","issue_type":"feature","priority":1,"closed_at":""},
				{"id":"demo-axk.2","title":"b","status":"open","issue_type":"feature","priority":1,"closed_at":""}
			], "schema_version": 1}`,
			mapID: "demo-axk",
			want:  2,
		},
		{
			name:  "malformed json",
			raw:   `not json`,
			mapID: "demo-axk",
			want:  0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseBdChildren(tc.raw, tc.mapID)
			if got == nil {
				t.Fatal("parseBdChildren must never return nil")
			}
			if len(got) != tc.want {
				t.Fatalf("got %d children, want %d: %+v", len(got), tc.want, got)
			}
		})
	}
}

func TestParseBdChildren_FieldMapping(t *testing.T) {
	raw := `{"m": [{"id":"m.1","title":"t","status":"closed","issue_type":"task","priority":2,"closed_at":"2026-09-04T00:00:00Z"}]}`
	got := parseBdChildren(raw, "m")
	if len(got) != 1 {
		t.Fatalf("got %d children, want 1", len(got))
	}
	want := BeadsSubBead{ID: "m.1", Title: "t", Status: "closed", IssueType: "task", Priority: 2, ClosedAt: "2026-09-04T00:00:00Z"}
	// reflect.DeepEqual statt !=: seit gkp-1lp traegt BeadsSubBead ein
	// Kinder-Feld und ist damit nicht mehr vergleichbar. Der Parser fuellt es
	// nicht — er sieht immer nur eine Ebene —, und genau das haelt der
	// Vergleich mit fest: Children muss hier nil sein.
	if !reflect.DeepEqual(got[0], want) {
		t.Errorf("got %+v, want %+v", got[0], want)
	}
}

// gkp-1uu: the batch call replaces one `bd show` per epic with one for the
// whole repo. What must survive the change is the per-epic split — every id
// gets its own children back, and closed_at rides along, because
// ReportsBead.Gap is computed from it.
func TestParseBdChildrenBatch(t *testing.T) {
	raw := `{
		"m1": [
			{"id":"m1.1","title":"a","status":"closed","issue_type":"task","priority":1,"closed_at":"2026-09-04T10:00:00Z","updated_at":"2026-09-04T10:00:00Z"},
			{"id":"m1.2","title":"b","status":"open","issue_type":"task","priority":2}
		],
		"m2": [
			{"id":"m2.1","title":"c","status":"in_progress","issue_type":"feature","priority":0,"updated_at":"2026-09-05T08:00:00Z"}
		],
		"m3": [],
		"schema_version": 1
	}`
	got := parseBdChildrenBatch(raw, []string{"m1", "m2", "m3", "m4"})

	// m4 was asked for but is absent from bd's answer — an unknown id must
	// still yield an entry, so the caller can index without a comma-ok.
	for _, id := range []string{"m1", "m2", "m3", "m4"} {
		if got[id] == nil {
			t.Fatalf("%s: nil slice, want empty", id)
		}
	}
	if len(got["m1"]) != 2 || len(got["m2"]) != 1 || len(got["m3"]) != 0 || len(got["m4"]) != 0 {
		t.Fatalf("wrong split: m1=%d m2=%d m3=%d m4=%d",
			len(got["m1"]), len(got["m2"]), len(got["m3"]), len(got["m4"]))
	}
	// No cross-talk: m2's child must not appear under m1.
	if got["m1"][0].ID != "m1.1" || got["m1"][1].ID != "m1.2" || got["m2"][0].ID != "m2.1" {
		t.Errorf("children landed under the wrong epic: %+v", got)
	}
	// Gap depends on this field surviving the batch parse.
	if got["m1"][0].ClosedAt != "2026-09-04T10:00:00Z" {
		t.Errorf("closed_at lost: %+v", got["m1"][0])
	}
	if got["m1"][1].ClosedAt != "" {
		t.Errorf("open bead must have empty closed_at: %+v", got["m1"][1])
	}
	if got["m2"][0].UpdatedAt != "2026-09-05T08:00:00Z" {
		t.Errorf("updated_at lost: %+v", got["m2"][0])
	}
}

// A failed batch call passes raw="" — every epic must then render childless
// rather than the page failing. Same fail-open contract as the single call.
func TestParseBdChildrenBatch_FailOpen(t *testing.T) {
	for _, raw := range []string{"", "not json", `{"schema_version": 1}`} {
		got := parseBdChildrenBatch(raw, []string{"m1", "m2"})
		if len(got) != 2 || got["m1"] == nil || got["m2"] == nil {
			t.Fatalf("raw=%q: want two empty non-nil entries, got %+v", raw, got)
		}
		if len(got["m1"]) != 0 || len(got["m2"]) != 0 {
			t.Errorf("raw=%q: want no children, got %+v", raw, got)
		}
	}
}

func TestRenderDotSVG(t *testing.T) {
	if _, err := exec.LookPath("dot"); err != nil {
		t.Skip("dot (graphviz) nicht installiert")
	}
	t.Run("empty input never shells out", func(t *testing.T) {
		svg, ok := RenderDotSVG(".", "")
		if ok || svg != "" {
			t.Fatalf("empty dot source should fail open: got svg=%q ok=%v", svg, ok)
		}
	})
	t.Run("valid dot renders svg", func(t *testing.T) {
		svg, ok := RenderDotSVG(".", "digraph{a->b}")
		if !ok {
			t.Fatal("expected ok=true for a valid dot graph")
		}
		if !strings.Contains(svg, "<svg") {
			t.Fatalf("expected <svg in output, got %d bytes: %.200s", len(svg), svg)
		}
	})
	t.Run("missing dot binary fails open, not panics", func(t *testing.T) {
		oldPath := os.Getenv("PATH")
		defer os.Setenv("PATH", oldPath)
		os.Setenv("PATH", "")
		svg, ok := RenderDotSVG(".", "digraph{a->b}")
		if ok || svg != "" {
			t.Fatalf("expected fail-open with PATH cleared, got svg=%q ok=%v", svg, ok)
		}
	})
}

// initGitRepo creates a minimal git repo in dir (mirrors
// internal/reports/store_test.go's helper of the same purpose — deriveGit's
// join key must be identical between the two packages, so the test fixtures
// stay identical too).
func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "test")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "f.txt")
	run("commit", "-q", "-m", "init")
}

// TestRepoIdentityMatchesReportsJoinKey is the regression test for demo-
// axk.4 integration gap 1: BeadsRepo.Name must equal the "repo" field
// reports.Append derives from the SAME cwd (internal/reports/store.go,
// deriveGit) — that shared string is the join key the reports view uses to
// connect a BeadsMap to its ~/.claude/reports/<repo>/<map>.jsonl entries.
// This test only re-derives the expected value via git directly (no import
// of internal/reports — that package must not gain a dependency on
// collectors, and importing it here would risk exactly the copy-paste-guess
// the task warns against); internal/reports/store_test.go carries the
// matching assertion on its own side (TestAppendDerivesGitFields).
func TestRepoIdentityMatchesReportsJoinKey(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git nicht installiert")
	}
	repo := t.TempDir()
	repo, _ = filepath.EvalSymlinks(repo) // macOS: /var -> /private/var, git reports the resolved path
	initGitRepo(t, repo)

	name, path := repoIdentity(repo)
	wantName := filepath.Base(repo)
	if name != wantName {
		t.Fatalf("repoIdentity(%q).name = %q, want %q (== reports.deriveGit's repo)", repo, name, wantName)
	}
	if path != repo {
		t.Fatalf("repoIdentity(%q).path = %q, want %q", repo, path, repo)
	}
}

// TestRepoIdentityFromLinkedWorktreeMatchesMain is the scenario "Repo statt
// Worktree" was chosen for: a call from inside a linked worktree (including a
// nested .claude/worktrees/<x> layout) must resolve to the SAME name as a
// call from the main checkout, because .beads/ (and, on the reports side,
// ~/.claude/reports/<repo>/) is shared across all of a repo's worktrees via
// the git root.
func TestRepoIdentityFromLinkedWorktreeMatchesMain(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git nicht installiert")
	}
	mainRepo := t.TempDir()
	initGitRepo(t, mainRepo)

	worktreeDir := filepath.Join(mainRepo, ".claude", "worktrees", "ga")
	if err := os.MkdirAll(filepath.Dir(worktreeDir), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "-C", mainRepo, "worktree", "add", "-b", "feat/ga", worktreeDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git worktree add: %v\n%s", err, out)
	}

	nameMain, pathMain := repoIdentity(mainRepo)
	nameLinked, pathLinked := repoIdentity(worktreeDir)

	if nameLinked != nameMain {
		t.Fatalf("repoIdentity from linked worktree = %q, want %q (same as main)", nameLinked, nameMain)
	}
	if pathLinked != pathMain {
		t.Fatalf("repoIdentity path from linked worktree = %q, want %q (same as main)", pathLinked, pathMain)
	}
	if nameMain != filepath.Base(mainRepo) {
		t.Fatalf("repoIdentity(main).name = %q, want %q", nameMain, filepath.Base(mainRepo))
	}
}

func TestSanitizeRepoComponent(t *testing.T) {
	cases := map[string]string{
		"demo": "demo",
		"":     "unknown",
		".":    "unknown",
		"..":   "unknown",
		"a/b":  "unknown",
		`a\b`:  "unknown",
	}
	for in, want := range cases {
		if got := sanitizeRepoComponent(in); got != want {
			t.Errorf("sanitizeRepoComponent(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestCollectBeads_PopulatesRepoIdentity confirms CollectBeads carries the
// join key all the way through the fail-open "no bd" path too (Name/Path
// must be set as soon as .beads/ is found, independent of bd's availability
// — the reports view needs Name even when Maps stays empty).
func TestCollectBeads_PopulatesRepoIdentity(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git nicht installiert")
	}
	dir := t.TempDir()
	dir, _ = filepath.EvalSymlinks(dir) // macOS: /var -> /private/var, git reports the resolved path
	initGitRepo(t, dir)
	if err := os.Mkdir(filepath.Join(dir, ".beads"), 0o755); err != nil {
		t.Fatal(err)
	}
	oldPath := os.Getenv("PATH")
	defer os.Setenv("PATH", oldPath)
	os.Setenv("PATH", filepath.Dir(mustLookPath(t, "git"))) // keep git, drop bd

	got := CollectBeads(dir)
	if got.Name != filepath.Base(dir) {
		t.Errorf("expected Name=%q even when bd is missing, got %+v", filepath.Base(dir), got)
	}
	if got.Path != dir {
		t.Errorf("expected Path=%q, got %q", dir, got.Path)
	}
}

func mustLookPath(t *testing.T, bin string) string {
	t.Helper()
	p, err := exec.LookPath(bin)
	if err != nil {
		t.Fatalf("%s nicht gefunden: %v", bin, err)
	}
	return p
}

func TestCollectBeads_NoBeadsDir(t *testing.T) {
	dir := t.TempDir()
	got := CollectBeads(dir)
	if got.Available {
		t.Errorf("expected Available=false without .beads/, got %+v", got)
	}
	if got.Error == "" {
		t.Error("expected an Error message without .beads/")
	}
	if len(got.Maps) != 0 || len(got.Worktrees) != 0 {
		t.Errorf("expected empty Maps/Worktrees, got %+v", got)
	}
}

// TestCollectBeads_BdMissing simulates a repo that has .beads/ but no bd
// binary on PATH — the fail-open path a fresh checkout without the tool
// installed would hit.
func TestCollectBeads_BdMissing(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".beads"), 0o755); err != nil {
		t.Fatal(err)
	}
	oldPath := os.Getenv("PATH")
	defer os.Setenv("PATH", oldPath)
	os.Setenv("PATH", "")

	got := CollectBeads(dir)
	if !got.Available {
		t.Errorf("expected Available=true once .beads/ is found, got %+v", got)
	}
	if got.Error == "" {
		t.Error("expected an Error message when bd is unavailable")
	}
	if len(got.Maps) != 0 {
		t.Errorf("expected no maps when bd list fails, got %+v", got.Maps)
	}
}

// TestBeadsPlanSVG_RealRepo exercises the real `bd graph --dot | dot -Tsvg`
// pipeline against this checkout's own .beads/ store. Skips cleanly if either
// binary, the store, or the specific reference map are unavailable —
// hermetic environments (CI without bd/graphviz) still pass the rest of the
// suite.
func TestBeadsPlanSVG_RealRepo(t *testing.T) {
	if _, err := exec.LookPath("bd"); err != nil {
		t.Skip("bd nicht installiert")
	}
	if _, err := exec.LookPath("dot"); err != nil {
		t.Skip("dot (graphviz) nicht installiert")
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	repo := RepoRoot(wd)
	if _, err := os.Stat(filepath.Join(repo, ".beads")); err != nil {
		t.Skip("kein .beads/ im aktuellen Repo")
	}
	const mapID = "demo-clr"
	if _, ok := bdRun(repo, "show", mapID, "--json"); !ok {
		t.Skipf("Referenz-Map %s nicht (mehr) vorhanden", mapID)
	}
	svg := beadsPlanSVG(repo, mapID, nil)
	if !strings.Contains(svg, "<svg") {
		t.Fatalf("expected an <svg> document for %s, got %d bytes", mapID, len(svg))
	}
	t.Logf("plan.svg fuer %s: %d Byte", mapID, len(svg))
}

// TestAnnotateDotDates covers the label rewriting that puts each bead's last
// change into the plan graph. The escaped-quote case is the reason this is not
// a regex over the label body: gkp-ar7.9's real title contains `--host ""`,
// which bd emits as \" inside the DOT string, and a `[^"]*` label pattern
// would cut the label off there and produce broken DOT.
func TestAnnotateDotDates(t *testing.T) {
	dot := strings.Join([]string{
		"digraph beads {",
		`    "gkp-ar7" [label="○ gkp-ar7\nP1 | offene Punkte", fillcolor="#e8f4fd"];`,
		`    "gkp-ar7.9" [label="○ gkp-ar7.9\nP2 | Fix: --host \"\" bindet Wildcard", fillcolor="#e8f4fd"];`,
		`    "gkp-ar7.4" [label="● gkp-ar7.4\nP3 | HEAD erlauben", fillcolor="#f8d7da"];`,
		`    "gkp-ar7.9" -> "gkp-ar7.4";`,
		"}",
	}, "\n")

	got := annotateDotDates(dot, map[string]string{
		"gkp-ar7":   "2026-09-04T19:31:07Z",
		"gkp-ar7.9": "2026-09-04T17:41:54Z",
		// gkp-ar7.4 deliberately absent: an unknown stamp must leave the node alone.
	})

	for _, want := range []string{
		`label="○ gkp-ar7\nP1 | offene Punkte\n2026-09-04 19:31"`,
		`label="○ gkp-ar7.9\nP2 | Fix: --host \"\" bindet Wildcard\n2026-09-04 17:41"`,
		`label="● gkp-ar7.4\nP3 | HEAD erlauben"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("annotated DOT missing %s\ngot:\n%s", want, got)
		}
	}
	// The edge line has no label= and must survive byte-identical.
	if !strings.Contains(got, `    "gkp-ar7.9" -> "gkp-ar7.4";`) {
		t.Errorf("edge line was rewritten:\n%s", got)
	}
	if n := strings.Count(got, "\n"); n != strings.Count(dot, "\n") {
		t.Errorf("line count changed: %d -> %d", strings.Count(dot, "\n"), n)
	}
}

// TestAnnotateDotDatesDegradesSafely: no dates, an empty stamp and a truncated
// DOT string must all return usable output rather than panic or emit a stray
// "\n" with nothing after it — beadsPlanSVG's whole contract is fail-open.
func TestAnnotateDotDatesDegradesSafely(t *testing.T) {
	line := `    "x" [label="a", fillcolor="#fff"];`
	if got := annotateDotDates(line, nil); got != line {
		t.Errorf("nil dates changed the DOT: %q", got)
	}
	if got := annotateDotDates(line, map[string]string{"x": ""}); got != line {
		t.Errorf("empty stamp changed the DOT: %q", got)
	}
	if got := annotateDotDates(line, map[string]string{"x": "2026-09"}); got != line {
		t.Errorf("too-short stamp changed the DOT: %q", got)
	}
	trunc := `    "x" [label="unterminated`
	if got := annotateDotDates(trunc, map[string]string{"x": "2026-09-04T19:31:07Z"}); got != trunc {
		t.Errorf("unterminated label changed the DOT: %q", got)
	}
}

// TestMarkDotBlocked pins the derived-blocked marking: bd colours nodes by
// stored status, so a bead with an unclosed dependency renders as plain open.
// The fixture mirrors the real gkp-ar7 shape — dashed containment edges from
// the epic (which must NOT block) plus solid dependency edges (which must).
func TestMarkDotBlocked(t *testing.T) {
	dot := strings.Join([]string{
		"digraph beads {",
		`    "m" [label="○ m\nP1 | epic", fillcolor="#e8f4fd", fontcolor="#1a1a1a"];`,
		`    "m.1" [label="◐ m.1\nP1 | laeuft", fillcolor="#fff3cd", fontcolor="#664d03"];`,
		`    "m.9" [label="○ m.9\nP2 | haengt an m.1", fillcolor="#e8f4fd", fontcolor="#1a1a1a"];`,
		`    "m.4" [label="○ m.4\nP3 | haengt an m.9", fillcolor="#e8f4fd", fontcolor="#1a1a1a"];`,
		`    "m.2" [label="✓ m.2\nP1 | fertig", fillcolor="#d4edda", fontcolor="#888888"];`,
		`    "m.8" [label="○ m.8\nP2 | haengt an m.2 (erledigt)", fillcolor="#e8f4fd", fontcolor="#1a1a1a"];`,
		`  "m" -> "m.9" [style=dashed, arrowhead=empty, color="#999999"];`,
		`  "m.1" -> "m.9" [style=solid, arrowhead=normal];`,
		`  "m.9" -> "m.4" [style=solid, arrowhead=normal];`,
		`  "m.2" -> "m.8" [style=solid, arrowhead=normal];`,
		"}",
	}, "\n")

	got := markDotBlocked(dot)

	for _, want := range []string{
		// blockiert: unerledigte Abhaengigkeit
		`"m.9" [label="● m.9\nP2 | haengt an m.1", fillcolor="#f8d7da"`,
		`"m.4" [label="● m.4\nP3 | haengt an m.9", fillcolor="#f8d7da"`,
		// NICHT blockiert: Abhaengigkeit ist geschlossen
		`"m.8" [label="○ m.8\nP2 | haengt an m.2 (erledigt)", fillcolor="#e8f4fd"`,
		// NICHT blockiert: nur eine gestrichelte Eltern-Kind-Kante
		`"m" [label="○ m\nP1 | epic", fillcolor="#e8f4fd"`,
		// in_progress bleibt in_progress
		`"m.1" [label="◐ m.1\nP1 | laeuft", fillcolor="#fff3cd"`,
		// geschlossen bleibt geschlossen
		`"m.2" [label="✓ m.2\nP1 | fertig", fillcolor="#d4edda"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("marked DOT missing %s\ngot:\n%s", want, got)
		}
	}
	if strings.Count(got, "\n") != strings.Count(dot, "\n") {
		t.Errorf("line count changed")
	}
}

// TestMarkDotBlockedNoop: a graph without solid edges must come back
// byte-identical, so a map that simply has no dependencies is never recoloured.
func TestMarkDotBlockedNoop(t *testing.T) {
	dot := strings.Join([]string{
		"digraph beads {",
		`    "m.1" [label="○ m.1\nP1 | frei", fillcolor="#e8f4fd"];`,
		`  "m" -> "m.1" [style=dashed, arrowhead=empty, color="#999999"];`,
		"}",
	}, "\n")
	if got := markDotBlocked(dot); got != dot {
		t.Errorf("dependency-free graph was rewritten:\n%s", got)
	}
}

// TestReplaceFillLeavesAttributelessLineAlone guards the deliberate choice not
// to append a fillcolor: guessing a position in bd's attribute list could emit
// malformed DOT, and a missing fill is not worth that risk.
func TestReplaceFillLeavesAttributelessLineAlone(t *testing.T) {
	line := `    "x" [label="○ x"];`
	if got := replaceFill(line, blockedFill); got != line {
		t.Errorf("line without fillcolor was changed: %q", got)
	}
}

// --------------------------------------------------------------- gkp-ar7.27

// Die Rueckschau gegen das echte bd dieses Repos: `bd list -t epic` blendet
// gkp-uc7 aus (geschlossen), der Zusatzaufruf muss es liefern — samt Kindern,
// denn die Kappung wird nach dem Anhaengen gemeinsam gebatcht.
func TestCollectBeads_LiefertGeschlosseneEpicsMit(t *testing.T) {
	if _, err := exec.LookPath("bd"); err != nil {
		t.Skip("bd nicht installiert")
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	repo := RepoRoot(wd)
	if _, err := os.Stat(filepath.Join(repo, ".beads")); err != nil {
		t.Skip("kein .beads/ im aktuellen Repo")
	}

	bestand, ok := bdBestandLesen(repo)
	if !ok {
		t.Skip("bd list --status all nicht beantwortbar")
	}
	offen := bestand.offeneEpics()
	zu := bestand.geschlosseneEpics(closedEpicsMax)
	if len(zu) == 0 {
		t.Skip("dieses Repo hat (noch) kein geschlossenes Epic")
	}
	if len(zu) > closedEpicsMax {
		t.Errorf("len=%d, die Kappung auf %d greift nicht", len(zu), closedEpicsMax)
	}

	// Der eigentliche Waechter dieser Umstellung (gkp-mnd): die Ableitung aus
	// dem Bestand muss dasselbe sagen wie bd selbst. Das kann kein Fixture
	// pruefen — die Frage ist nicht, ob der Filter tut, was er soll, sondern ob
	// bds Voreinstellungen weiterhin das sind, wogegen er gemessen wurde.
	// Gleiche Bauart wie TestInstalledBdStillWritesWallclockZ in bdops: der
	// Test fragt das installierte bd, weil die Gefahr nicht ist, dass der Code
	// heute falsch waere, sondern dass bd sich darunter aendert.
	vergleiche := func(name string, bdArgs []string, abgeleitet []string) {
		t.Helper()
		out, ok := bdRun(repo, bdArgs...)
		if !ok {
			t.Skipf("%s nicht beantwortbar", name)
		}
		roh, ok := shared.ParseJSON[[]bdEpicJSON](out)
		if !ok {
			t.Skipf("%s: Antwort nicht lesbar", name)
		}
		var bdIDs []string
		for _, e := range roh {
			bdIDs = append(bdIDs, e.ID)
		}
		if !slices.Equal(bdIDs, abgeleitet) {
			t.Errorf("%s: bd sagt %v, aus dem Bestand abgeleitet %v — die Ableitung und bds Voreinstellung laufen auseinander",
				name, bdIDs, abgeleitet)
		}
	}
	idsVon := func(es []bdEpicJSON) []string {
		var ids []string
		for _, e := range es {
			ids = append(ids, e.ID)
		}
		return ids
	}
	// Die Reihenfolge wird MITgeprueft, nicht nur die Menge: bei der Rueckschau
	// IST sie die Auswahl — `--limit` ohne die richtige Sortierrichtung haette
	// die aeltesten fuenf geliefert (gkp-ar7.27).
	vergleiche("bd list -t epic",
		[]string{"list", "-t", "epic", "--json", "--limit", "0"}, idsVon(offen))
	vergleiche("bd list -t epic --status closed --sort closed",
		[]string{"list", "-t", "epic", "--status", "closed", "--sort", "closed",
			"--limit", strconv.Itoa(closedEpicsMax), "--json"}, idsVon(zu))

	// Die beiden Listen muessen disjunkt sein — sonst stuende eine Karte
	// zweimal auf der Seite.
	offenIDs := map[string]bool{}
	for _, e := range offen {
		offenIDs[e.ID] = true
	}
	for _, e := range zu {
		if offenIDs[e.ID] {
			t.Errorf("%s steht in beiden Listen", e.ID)
		}
		if e.Status != "closed" {
			t.Errorf("%s hat Status %q, erwartet closed", e.ID, e.Status)
		}
		if e.ClosedAt == "" {
			t.Errorf("%s ohne ClosedAt — ohne den Stempel ist \"zuletzt geschlossen\" nicht beschriftbar", e.ID)
		}
	}

	// Absteigend, sonst waehlt die Kappung die AELTESTEN aus.
	for i := 1; i < len(zu); i++ {
		if zu[i-1].ClosedAt < zu[i].ClosedAt {
			t.Errorf("Reihenfolge nicht absteigend: %s (%s) vor %s (%s)",
				zu[i-1].ID, zu[i-1].ClosedAt, zu[i].ID, zu[i].ClosedAt)
		}
	}

	got := CollectBeads(repo)
	nachID := map[string]BeadsMap{}
	for _, m := range got.Maps {
		nachID[m.ID] = m
	}
	for _, e := range zu {
		m, da := nachID[e.ID]
		if !da {
			t.Fatalf("%s fehlt in CollectBeads.Maps", e.ID)
		}
		if m.ClosedAt == "" {
			t.Errorf("%s: ClosedAt kommt nicht bis BeadsMap durch", e.ID)
		}
		if len(m.Children) == 0 {
			t.Errorf("%s ohne Children — der Batch muss die geschlossenen Epics mitnehmen", e.ID)
		}
	}
}
