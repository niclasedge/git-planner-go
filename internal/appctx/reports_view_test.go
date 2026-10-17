package appctx

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"testing"

	"github.com/niclasedge/git-planner-go/internal/collectors"
	"github.com/niclasedge/git-planner-go/internal/reports"
)

func entry(id, bead, kind, at string) reports.Entry {
	return reports.Entry{ID: id, At: at, Bead: bead, Kind: kind, Title: "t", Proof: []reports.ProofItem{}}
}

func TestBuildReportsMap_GapWhenClosedWithoutDoneEntry(t *testing.T) {
	m := collectors.BeadsMap{
		ID: "map", Children: []collectors.BeadsSubBead{
			{ID: "map.1", Status: "closed"},
		},
	}
	// only a "plan" entry — never a "done" — for a bead that is closed.
	got := buildReportsMap(m, []reports.Entry{entry("e1", "map.1", "plan", "2026-09-04T10:00:00Z")})
	if len(got.Beads) != 1 {
		t.Fatalf("expected 1 bead, got %d", len(got.Beads))
	}
	if !got.Beads[0].Gap {
		t.Fatalf("expected Gap=true for a closed bead with no done entry, got %+v", got.Beads[0])
	}
}

// TestBuildReportsMap_ClosedNoEntriesAtAll is the exact real-world shape
// behind demo-axk.1 / .2: closed, zero report entries whatsoever (no
// MCP ingest happened before demo-axk.3 existed). Gap must still fire —
// "no entries" is not a different case from "no done entry" for this check.
func TestBuildReportsMap_ClosedNoEntriesAtAll(t *testing.T) {
	m := collectors.BeadsMap{
		ID: "map", Children: []collectors.BeadsSubBead{
			{ID: "map.1", Status: "closed"},
		},
	}
	got := buildReportsMap(m, nil)
	if !got.Beads[0].Gap {
		t.Fatalf("expected Gap=true for a closed bead with zero entries, got %+v", got.Beads[0])
	}
	if got.Beads[0].Entries == nil {
		t.Fatalf("Entries must be a non-nil (possibly empty) slice for the view to range over")
	}
}

func TestBuildReportsMap_NoGapWhenDoneEntryExists(t *testing.T) {
	m := collectors.BeadsMap{
		ID: "map", Children: []collectors.BeadsSubBead{{ID: "map.1", Status: "closed"}},
	}
	got := buildReportsMap(m, []reports.Entry{
		entry("e1", "map.1", "plan", "2026-09-04T09:00:00Z"),
		entry("e2", "map.1", "done", "2026-09-04T10:00:00Z"),
	})
	if got.Beads[0].Gap {
		t.Fatalf("expected Gap=false once a done entry exists, got %+v", got.Beads[0])
	}
}

func TestBuildReportsMap_NoGapWhenBeadNotClosed(t *testing.T) {
	m := collectors.BeadsMap{
		ID: "map", Children: []collectors.BeadsSubBead{{ID: "map.1", Status: "in_progress"}},
	}
	got := buildReportsMap(m, nil)
	if got.Beads[0].Gap {
		t.Fatalf("expected Gap=false for a non-closed bead with no entries, got %+v", got.Beads[0])
	}
}

func TestBuildReportsMap_SupersededEntryStaysVisibleAndMarked(t *testing.T) {
	m := collectors.BeadsMap{
		ID: "map", Children: []collectors.BeadsSubBead{{ID: "map.1", Status: "in_progress"}},
	}
	original := entry("e1", "map.1", "plan", "2026-09-04T09:00:00Z")
	correction := entry("e2", "map.1", "plan", "2026-09-04T10:00:00Z")
	correction.Supersedes = "e1"
	got := buildReportsMap(m, []reports.Entry{original, correction})

	if len(got.Beads[0].Entries) != 2 {
		t.Fatalf("expected both the original and the correction to stay visible, got %d entries", len(got.Beads[0].Entries))
	}
	byID := map[string]ReportsEntry{}
	for _, e := range got.Beads[0].Entries {
		byID[e.ID] = e
	}
	if !byID["e1"].Superseded {
		t.Errorf("expected e1 (the superseded entry) to be marked Superseded=true, got %+v", byID["e1"])
	}
	if byID["e2"].Superseded {
		t.Errorf("expected e2 (the correction) to be marked Superseded=false, got %+v", byID["e2"])
	}
}

// TestHasDoneEntry_SupersededDoneNoLongerCountsAsEvidence is the review's
// exact live repro for finding 3: a bead's only "done" entry (e1) is
// superseded by a later "plan" entry (e2) — the closure's sole evidence was
// retracted, so the gap warning must fire again, not stay silenced by e1.
func TestHasDoneEntry_SupersededDoneNoLongerCountsAsEvidence(t *testing.T) {
	m := collectors.BeadsMap{
		ID: "map", Children: []collectors.BeadsSubBead{{ID: "map.1", Status: "closed"}},
	}
	e1 := entry("e1", "map.1", "done", "2026-09-04T09:00:00Z")
	e2 := entry("e2", "map.1", "plan", "2026-09-04T10:00:00Z")
	e2.Supersedes = "e1"
	got := buildReportsMap(m, []reports.Entry{e1, e2})
	if !got.Beads[0].Gap {
		t.Fatalf("expected Gap=true once the only done entry (e1) is superseded by a later plan entry, got %+v", got.Beads[0])
	}
}

// TestHasDoneEntry_ReopenedBeadNeedsItsOwnDoneEntry is the multi-close case
// finding 3 flagged beyond the explicit-supersede repro: bd allows closing a
// bead more than once. A done entry from the FIRST closure must not silence
// the gap warning for a SECOND closure that never got its own done entry.
func TestHasDoneEntry_ReopenedBeadNeedsItsOwnDoneEntry(t *testing.T) {
	m := collectors.BeadsMap{
		ID: "map", Children: []collectors.BeadsSubBead{{ID: "map.1", Status: "closed"}},
	}
	got := buildReportsMap(m, []reports.Entry{
		entry("e1", "map.1", "plan", "2026-09-04T08:00:00Z"),
		entry("e2", "map.1", "done", "2026-09-04T09:00:00Z"), // closure #1, backed
		entry("e3", "map.1", "plan", "2026-09-04T10:00:00Z"), // reopened
		// closure #2 (current bd status: closed) never got a done entry.
	})
	if !got.Beads[0].Gap {
		t.Fatalf("expected Gap=true: the current (second) closure has no done entry of its own, got %+v", got.Beads[0])
	}
}

// TestHasDoneEntry_DoneAfterReopenClearsGap is the positive counterpart: once
// the reopened lifecycle segment gets its own done entry, the gap clears.
func TestHasDoneEntry_DoneAfterReopenClearsGap(t *testing.T) {
	m := collectors.BeadsMap{
		ID: "map", Children: []collectors.BeadsSubBead{{ID: "map.1", Status: "closed"}},
	}
	got := buildReportsMap(m, []reports.Entry{
		entry("e1", "map.1", "plan", "2026-09-04T08:00:00Z"),
		entry("e2", "map.1", "done", "2026-09-04T09:00:00Z"),
		entry("e3", "map.1", "plan", "2026-09-04T10:00:00Z"),
		entry("e4", "map.1", "done", "2026-09-04T11:00:00Z"), // closure #2, backed
	})
	if got.Beads[0].Gap {
		t.Fatalf("expected Gap=false once the reopened segment has its own done entry, got %+v", got.Beads[0])
	}
}

// TestCollectReportsData_OrphanedMapFileSurfacesInsteadOfVanishing is finding
// 4's live repro: a report file whose map= name matches no bd epic (typo,
// renamed/deleted epic) must show up as its own flagged block, not disappear.
func TestCollectReportsData_OrphanedMapFileSurfacesInsteadOfVanishing(t *testing.T) {
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
	repo := collectors.RepoRoot(wd)
	if _, err := os.Stat(filepath.Join(repo, ".beads")); err != nil {
		t.Skip("kein .beads/ im aktuellen Repo")
	}

	beadsName := collectors.ReportsRepoName(repo)
	tmpReports := t.TempDir()
	t.Setenv("CC_REPORTS_DIR", tmpReports)
	dir := filepath.Join(tmpReports, beadsName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	const orphanID = "demo-DOES-NOT-EXIST"
	line := `{"id":"x1","at":"2026-01-01T00:00:00Z","map":"` + orphanID + `","bead":"` + orphanID + `.1","kind":"plan","title":"t","proof":[]}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, orphanID+".jsonl"), []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}

	view := CollectReportsData(repo)
	var found *ReportsMap
	for i := range view.Maps {
		if view.Maps[i].ID == orphanID {
			found = &view.Maps[i]
		}
	}
	if found == nil {
		t.Fatalf("expected the orphaned map file to surface as its own ReportsMap, got maps: %+v", view.Maps)
	}
	if !found.Unknown {
		t.Errorf("expected Unknown=true for an orphaned map, got %+v", found)
	}
	if len(found.Other) != 1 {
		t.Errorf("expected the orphaned file's one entry to be filed under Other, got %+v", found.Other)
	}
}

func TestBuildReportsMap_UnknownBeadGoesToOtherNewestFirst(t *testing.T) {
	m := collectors.BeadsMap{
		ID: "map", Children: []collectors.BeadsSubBead{{ID: "map.1", Status: "open"}},
	}
	got := buildReportsMap(m, []reports.Entry{
		entry("e1", "map.99", "plan", "2026-09-04T09:00:00Z"), // map.99 is not a known child
		entry("e2", "map.98", "plan", "2026-09-04T11:00:00Z"),
	})
	if len(got.Beads[0].Entries) != 0 {
		t.Fatalf("expected the known bead to carry no entries, got %+v", got.Beads[0].Entries)
	}
	if len(got.Other) != 2 {
		t.Fatalf("expected both unmatched entries under Other, got %d", len(got.Other))
	}
	if !sort.SliceIsSorted(got.Other, func(i, j int) bool { return got.Other[i].At > got.Other[j].At }) {
		t.Fatalf("expected Other newest-first, got %+v", got.Other)
	}
}

func TestCollectReportsData_NoBeadsDirFailsOpen(t *testing.T) {
	dir := t.TempDir()
	got := CollectReportsData(dir)
	if got.Available {
		t.Errorf("expected Available=false without .beads/, got %+v", got)
	}
	if got.Error == "" {
		t.Error("expected an Error message without .beads/")
	}
	if got.Maps == nil {
		t.Error("Maps must be a non-nil (possibly empty) slice")
	}
}

// TestCollectReportsData_RealRepo exercises the real join end to end against
// this checkout's own .beads/ store and (whatever currently exists under)
// ~/.claude/reports/. Answers the question the task asked directly: as of
// this session demo-axk.1 and .2 are closed with zero report-store
// entries (no MCP ingest happened before demo-axk.3 existed) — the gap
// warning MUST fire for both. Skips cleanly if bd/dot/the map are
// unavailable, matching TestBeadsPlanSVG_RealRepo's convention.
func TestCollectReportsData_RealRepo(t *testing.T) {
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
	repo := collectors.RepoRoot(wd)
	if _, err := os.Stat(filepath.Join(repo, ".beads")); err != nil {
		t.Skip("kein .beads/ im aktuellen Repo")
	}
	const mapID = "demo-axk"
	view := CollectReportsData(repo)
	if !view.Available {
		t.Skipf("beads nicht verfuegbar: %s", view.Error)
	}
	var target *ReportsMap
	for i := range view.Maps {
		if view.Maps[i].ID == mapID {
			target = &view.Maps[i]
		}
	}
	if target == nil {
		t.Skipf("Referenz-Map %s nicht (mehr) vorhanden", mapID)
	}
	gaps := map[string]bool{}
	for _, b := range target.Beads {
		gaps[b.ID] = b.Gap
	}
	for _, closedBead := range []string{mapID + ".1", mapID + ".2"} {
		if g, ok := gaps[closedBead]; !ok {
			t.Errorf("expected %s among the map's children", closedBead)
		} else if !g {
			t.Errorf("expected Gap=true for %s (closed, no report entries yet), got false", closedBead)
		}
	}
	t.Logf("reports view for %s: %d beads, skipped=%d", mapID, len(target.Beads), view.Skipped)
}

// --- Sortierung: neueste Änderung oben ---

func beadIDs(got ReportsMap) []string {
	ids := make([]string, 0, len(got.Beads))
	for _, b := range got.Beads {
		ids = append(ids, b.ID)
	}
	return ids
}

func assertOrder(t *testing.T, got ReportsMap, want ...string) {
	t.Helper()
	ids := beadIDs(got)
	if len(ids) != len(want) {
		t.Fatalf("bead count = %d (%v), want %d (%v)", len(ids), ids, len(want), want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("order = %v, want %v", ids, want)
		}
	}
}

// bd hands back children in its own order; the view must not inherit it.
func TestBuildReportsMap_SortsByUpdatedAtNewestFirst(t *testing.T) {
	m := collectors.BeadsMap{
		ID: "map", Children: []collectors.BeadsSubBead{
			{ID: "map.1", Status: "open", UpdatedAt: "2026-09-04T10:00:00Z"},
			{ID: "map.2", Status: "open", UpdatedAt: "2026-09-04T12:00:00Z"},
			{ID: "map.3", Status: "open", UpdatedAt: "2026-09-04T11:00:00Z"},
		},
	}
	assertOrder(t, buildReportsMap(m, nil), "map.2", "map.3", "map.1")
}

// A report that just landed makes its bead the newest, even though bd's
// updated_at is older — notifyBead's comment is fail-open, so the entry itself
// has to count.
func TestBuildReportsMap_FreshEntryOutranksOlderUpdatedAt(t *testing.T) {
	m := collectors.BeadsMap{
		ID: "map", Children: []collectors.BeadsSubBead{
			{ID: "map.1", Status: "open", UpdatedAt: "2026-09-04T10:00:00Z"},
			{ID: "map.2", Status: "open", UpdatedAt: "2026-09-04T12:00:00Z"},
		},
	}
	// map.1's entry is newer than map.2's updated_at → map.1 moves to the top.
	entries := []reports.Entry{entry("e1", "map.1", "plan", "2026-09-04T13:00:00Z")}
	got := buildReportsMap(m, entries)
	assertOrder(t, got, "map.1", "map.2")
	if got.Beads[0].LastChange != "2026-09-04T13:00:00Z" {
		t.Fatalf("LastChange = %q, want the entry timestamp", got.Beads[0].LastChange)
	}
}

// The later of the two signals wins — here bd's updated_at is the newer one.
func TestBuildReportsMap_LastChangeTakesLaterOfBothSignals(t *testing.T) {
	m := collectors.BeadsMap{
		ID: "map", Children: []collectors.BeadsSubBead{
			{ID: "map.1", Status: "open", UpdatedAt: "2026-09-04T15:00:00Z"},
		},
	}
	entries := []reports.Entry{entry("e1", "map.1", "plan", "2026-09-04T09:00:00Z")}
	got := buildReportsMap(m, entries)
	if got.Beads[0].LastChange != "2026-09-04T15:00:00Z" {
		t.Fatalf("LastChange = %q, want the updated_at timestamp", got.Beads[0].LastChange)
	}
}

// A bead with neither signal must not float to the top; and equal keys fall
// back to id so the order never depends on bd's arbitrary sequence.
func TestBuildReportsMap_UnknownLastChangeSortsLastAndTiesByID(t *testing.T) {
	m := collectors.BeadsMap{
		ID: "map", Children: []collectors.BeadsSubBead{
			{ID: "map.9", Status: "open"},
			{ID: "map.2", Status: "open", UpdatedAt: "2026-09-04T10:00:00Z"},
			{ID: "map.1", Status: "open", UpdatedAt: "2026-09-04T10:00:00Z"},
			{ID: "map.3", Status: "open"},
		},
	}
	assertOrder(t, buildReportsMap(m, nil), "map.1", "map.2", "map.3", "map.9")
}
