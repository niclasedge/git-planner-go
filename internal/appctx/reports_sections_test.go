package appctx

import (
	"fmt"
	"testing"

	"github.com/niclasedge/git-planner-go/internal/collectors"
	"github.com/niclasedge/git-planner-go/internal/reports"
)

// Tests for the three sections of the reports column (gkp-uc7.3 to .6): which
// map lands in which section, in what order, and what the history is built
// from. All of it is decided here rather than in browser.js, so all of it is
// testable without a browser — the same split the rest of this file relies on.

// ---------------------------------------------------------------- gkp-uc7.4

func TestMapInProgressZaehltAuchLaufendeKinder(t *testing.T) {
	faelle := []struct {
		name       string
		mapStatus  string
		kindStatus []string
		will       bool
	}{
		{"Epic in Arbeit", "in_progress", []string{"open"}, true},
		{"Epic offen, Kind in Arbeit", "open", []string{"closed", "in_progress"}, true},
		{"alles offen", "open", []string{"open", "open"}, false},
		{"alles zu", "closed", []string{"closed"}, false},
		{"Epic offen, ohne Kinder", "open", nil, false},
	}
	for _, f := range faelle {
		t.Run(f.name, func(t *testing.T) {
			m := collectors.BeadsMap{ID: "map", Status: f.mapStatus}
			for i, s := range f.kindStatus {
				m.Children = append(m.Children, collectors.BeadsSubBead{
					ID: "map." + string(rune('1'+i)), Status: s,
				})
			}
			if got := mapInProgress(m); got != f.will {
				t.Errorf("mapInProgress=%v, erwartet %v", got, f.will)
			}
		})
	}
}

// Das ist der Fall, an dem ein Kriterium "nur der Epic-Status" scheitert:
// beads zieht den Elternstatus nicht mit, wenn ein Kind anfaengt. Gemessen in
// orbit — orbit-kd3 stand auf open, waehrend daran gearbeitet wurde.
func TestBuildReportsMapMeldetInArbeitBeiOffenemEpic(t *testing.T) {
	m := collectors.BeadsMap{
		ID: "map", Status: "open", Children: []collectors.BeadsSubBead{
			{ID: "map.1", Status: "in_progress"},
		},
	}
	if got := buildReportsMap(m, nil); !got.InProgress {
		t.Error("InProgress=false — eine Karte mit laufendem Kind gehoert in die Sektion")
	}
}

// ---------------------------------------------------------------- gkp-uc7.3

func TestReadyCountUndDurchreichenDerBdFelder(t *testing.T) {
	m := collectors.BeadsMap{
		ID: "map", Children: []collectors.BeadsSubBead{
			{ID: "map.1", Status: "open", Ready: true, Priority: 1, IssueType: "bug"},
			{ID: "map.2", Status: "open", Ready: false, Priority: 3, IssueType: "task"},
			{ID: "map.3", Status: "open", Ready: true, Priority: 0, IssueType: "feature"},
		},
	}
	got := buildReportsMap(m, nil)

	if got.ReadyCount != 2 {
		t.Errorf("ReadyCount=%d, erwartet 2", got.ReadyCount)
	}
	nachID := map[string]ReportsBead{}
	for _, b := range got.Beads {
		nachID[b.ID] = b
	}
	if b := nachID["map.1"]; !b.Ready || b.Priority != 1 || b.IssueType != "bug" {
		t.Errorf("map.1 = %+v — Ready/Priority/IssueType muessen von bd durchkommen", b)
	}
	if nachID["map.2"].Ready {
		t.Error("map.2 ist als startklar markiert, obwohl bd es nicht sagt")
	}
}

// ---------------------------------------------------------------- gkp-uc7.6

func TestSortMapsNeuesteZuerstUndBeiGleichstandNachID(t *testing.T) {
	maps := []ReportsMap{
		{ID: "c", LastChange: "2026-09-04T10:00:00Z"},
		{ID: "a", LastChange: "2026-09-04T12:00:00Z"},
		{ID: "b", LastChange: "2026-09-04T12:00:00Z"},
		{ID: "d", LastChange: ""},
	}
	sortMaps(maps)

	will := []string{"a", "b", "c", "d"}
	for i, w := range will {
		if maps[i].ID != w {
			t.Fatalf("Reihenfolge %v, erwartet %v", ids(maps), will)
		}
	}
}

func ids(maps []ReportsMap) []string {
	out := make([]string, len(maps))
	for i, m := range maps {
		out[i] = m.ID
	}
	return out
}

func TestMapLastChangeNimmtDenSpaeterenDerBeidenSignale(t *testing.T) {
	// Das Epic ist aelter als sein neuester Bead: der Bead gewinnt.
	m := collectors.BeadsMap{
		ID: "map", UpdatedAt: "2026-09-04T10:00:00Z",
		Children: []collectors.BeadsSubBead{{ID: "map.1", UpdatedAt: "2026-09-04T15:00:00Z"}},
	}
	if got := buildReportsMap(m, nil).LastChange; got != "2026-09-04T15:00:00Z" {
		t.Errorf("LastChange=%q, erwartet den Bead-Zeitpunkt", got)
	}

	// Umgekehrt: ein Epic, an dem gerade etwas geaendert wurde, ohne dass ein
	// Kind sich bewegt hat.
	m2 := collectors.BeadsMap{
		ID: "map", UpdatedAt: "2026-09-04T18:00:00Z",
		Children: []collectors.BeadsSubBead{{ID: "map.1", UpdatedAt: "2026-09-04T15:00:00Z"}},
	}
	if got := buildReportsMap(m2, nil).LastChange; got != "2026-09-04T18:00:00Z" {
		t.Errorf("LastChange=%q, erwartet den Epic-Zeitpunkt", got)
	}
}

// Eine Karte ohne jedes Signal darf nicht nach oben rutschen. Leeres
// LastChange sortiert hinten, das faellt aus dem String-Vergleich.
func TestSortMapsOhneZeitstempelSortiertHinten(t *testing.T) {
	maps := []ReportsMap{
		{ID: "leer"},
		{ID: "alt", LastChange: "2020-01-01T00:00:00Z"},
	}
	sortMaps(maps)
	if maps[0].ID != "alt" {
		t.Errorf("Reihenfolge %v — eine Karte ohne Zeitstempel gehoert hinten hin", ids(maps))
	}
}

// ---------------------------------------------------------------- gkp-uc7.5

func TestBuildTimelineIstNeuesteZuerstUeberAlleKarten(t *testing.T) {
	mapsEntries := map[string][]reports.Entry{
		"m1": {entry("e3", "m1.1", "done", "2026-09-04T12:00:00Z"), entry("e1", "m1.2", "plan", "2026-09-04T09:00:00Z")},
		"m2": {entry("e4", "m2.1", "done", "2026-09-04T14:00:00Z"), entry("e2", "m2.2", "plan", "2026-09-04T10:00:00Z")},
	}
	tl, total := buildTimeline(mapsEntries)

	if total != 4 {
		t.Errorf("total=%d, erwartet 4", total)
	}
	will := []string{"e4", "e3", "e2", "e1"}
	for i, w := range will {
		if i >= len(tl) || tl[i].ID != w {
			t.Fatalf("Reihenfolge %v, erwartet %v", entryIDs(tl), will)
		}
	}
}

func entryIDs(es []ReportsEntry) []string {
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = e.ID
	}
	return out
}

// Der Grund, warum die Zeitleiste ihre eigene Supersede-Aufloesung braucht:
// eine Korrektur kann unter einer ANDEREN Karte liegen als der Eintrag, den
// sie zurueckzieht (umbenanntes Epic, Tippfehler im map-Feld beim Ingest).
// Loeste die Zeitleiste das je Karte auf, zeigte sie einen zurueckgezogenen
// Eintrag als gueltig.
func TestBuildTimelineLoestSupersedeUeberKartengrenzenAuf(t *testing.T) {
	alt := entry("alt", "b.1", "done", "2026-09-04T09:00:00Z")
	neu := entry("neu", "b.1", "done", "2026-09-04T11:00:00Z")
	neu.Supersedes = "alt"
	mapsEntries := map[string][]reports.Entry{
		"alte-map": {alt},
		"neue-map": {neu},
	}
	tl, _ := buildTimeline(mapsEntries)

	nachID := map[string]ReportsEntry{}
	for _, e := range tl {
		nachID[e.ID] = e
	}
	if !nachID["alt"].Superseded {
		t.Error("der alte Eintrag ist nicht als ersetzt markiert, obwohl die Korrektur unter einer anderen Karte liegt")
	}
	if nachID["neu"].Superseded {
		t.Error("die Korrektur selbst ist als ersetzt markiert")
	}
}

func TestBuildTimelineKapptUndNenntDieGesamtzahl(t *testing.T) {
	var viele []reports.Entry
	for i := 0; i < timelineMax+25; i++ {
		// Absteigende Zeitstempel, damit die Reihenfolge eindeutig ist.
		std := 23 - i%24
		viele = append(viele, entry(
			"e"+itoa(i), "b.1", "done",
			"2026-09-"+pad(i/24+1)+"T"+pad(std)+":00:00Z",
		))
	}
	tl, total := buildTimeline(map[string][]reports.Entry{"m": viele})

	if total != timelineMax+25 {
		t.Errorf("total=%d, erwartet %d", total, timelineMax+25)
	}
	if len(tl) != timelineMax {
		t.Errorf("len(tl)=%d, erwartet die Kappung auf %d", len(tl), timelineMax)
	}
}

func TestBuildTimelineLeerIstKeinFehler(t *testing.T) {
	tl, total := buildTimeline(map[string][]reports.Entry{})
	if len(tl) != 0 || total != 0 {
		t.Errorf("tl=%v total=%d, erwartet leer", tl, total)
	}
	// Nicht nil: die Ansicht liest .length und json.Marshal soll [] schreiben,
	// nicht null.
	if tl == nil {
		t.Error("Timeline ist nil — im JSON wird daraus null statt []")
	}
}

// pad formatiert eine Zahl zweistellig ("01"), damit die zusammengesetzten
// Zeitstempel oben gültiges RFC3339 sind — Entry.At wird als String
// verglichen, ein "2026-09-1T3:00:00Z" würde also falsch einsortieren.
func pad(n int) string { return fmt.Sprintf("%02d", n) }

// --------------------------------------------------------------- gkp-ar7.25

// Die vier Zahlen der Statusuebersicht sind bewusst keine Summe. Der Fall, der
// eine einzelne "Quote" widerlegt: ein OFFENER Bead mit done-Eintrag ist belegt
// und trotzdem offen — Proven und Open zaehlen ihn beide.
func TestCountBeadsZaehltBelegUndZustandGetrennt(t *testing.T) {
	m := collectors.BeadsMap{
		ID: "map", Children: []collectors.BeadsSubBead{
			{ID: "map.1", Status: "closed"},      // belegt, zu
			{ID: "map.2", Status: "closed"},      // zu ohne Beleg -> Luecke
			{ID: "map.3", Status: "open"},        // belegt, offen (Zwischenstand)
			{ID: "map.4", Status: "in_progress"}, // offen, kein Beleg
		},
	}
	got := buildReportsMap(m, []reports.Entry{
		entry("e1", "map.1", "done", "2026-09-04T10:00:00Z"),
		entry("e2", "map.3", "done", "2026-09-04T11:00:00Z"),
	})

	will := MapCounts{Total: 4, Open: 2, Closed: 2, Proven: 2, Gaps: 1}
	if got.Counts != will {
		t.Errorf("Counts=%+v, erwartet %+v", got.Counts, will)
	}
	nachID := map[string]ReportsBead{}
	for _, b := range got.Beads {
		nachID[b.ID] = b
	}
	if b := nachID["map.3"]; !b.Proven || b.Gap {
		t.Errorf("map.3 = Proven=%v Gap=%v — ein offener Bead mit done-Eintrag ist belegt, aber keine Luecke",
			b.Proven, b.Gap)
	}
	if b := nachID["map.2"]; b.Proven || !b.Gap {
		t.Errorf("map.2 = Proven=%v Gap=%v — zu ohne Beleg ist genau die Luecke", b.Proven, b.Gap)
	}
}

// deferred ist der Zustand, an dem eine Liste der OFFENEN Zustaende gescheitert
// waere: bd hat ihn nachtraeglich bekommen, und er darf nicht als fertig
// durchgehen. Gezaehlt wird deshalb gegen den einen Endzustand.
func TestCountBeadsZaehltUnbekannteZustaendeAlsOffen(t *testing.T) {
	m := collectors.BeadsMap{
		ID: "map", Children: []collectors.BeadsSubBead{
			{ID: "map.1", Status: "deferred"},
			{ID: "map.2", Status: "blocked"},
			{ID: "map.3", Status: ""},
		},
	}
	got := buildReportsMap(m, nil).Counts
	if got.Open != 3 || got.Closed != 0 {
		t.Errorf("Open=%d Closed=%d, erwartet 3/0", got.Open, got.Closed)
	}
	if got.Gaps != 0 {
		t.Errorf("Gaps=%d — offene Beads ohne Beleg sind keine Luecke", got.Gaps)
	}
}

// Eine geschlossene Karte kennt bd nicht mehr (`bd list -t epic` blendet sie
// aus), also hat sie keine Children — und genau ihre Bilanz interessiert am
// meisten. Gezaehlt wird dann aus dem Report-Store.
func TestOrphanedMapsZaehltAusDemReportStore(t *testing.T) {
	entries := map[string][]reports.Entry{
		"weg-1": {
			entry("e1", "weg-1.1", "done", "2026-09-04T10:00:00Z"),
			entry("e2", "weg-1.2", "plan", "2026-09-04T11:00:00Z"),
			entry("e3", "weg-1.1", "plan", "2026-09-04T09:00:00Z"),
		},
	}
	out := orphanedMaps(entries, map[string]bool{})
	if len(out) != 1 {
		t.Fatalf("len=%d, erwartet 1 verwaiste Karte", len(out))
	}
	got := out[0].Counts
	// Zwei verschiedene Beads, einer davon mit gueltigem done-Eintrag.
	if got.Total != 2 || got.Proven != 1 {
		t.Errorf("Total=%d Proven=%d, erwartet 2/1", got.Total, got.Proven)
	}
	// Der Report-Store kennt keine bd-Zustaende — hier zu raten waere schlimmer
	// als nichts zu sagen.
	if got.Open != 0 || got.Closed != 0 || got.Gaps != 0 {
		t.Errorf("%+v — ohne bd-Epic gibt es keine Zustandszahlen", got)
	}
}
