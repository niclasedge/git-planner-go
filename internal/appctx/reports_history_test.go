package appctx

import (
	"strings"
	"testing"
	"time"

	"github.com/niclasedge/git-planner-go/internal/collectors"
	"github.com/niclasedge/git-planner-go/internal/reports"
)

// Tests fuer die Unterteilung der Historie nach Herkunft (gkp-uc7.8): was in
// welcher Spur landet, in welcher Reihenfolge, und was die Bilanz sagt, wenn
// eine Spur leer ist.

func repEntry(id, bead, at string) ReportsEntry {
	return ReportsEntry{Entry: reports.Entry{ID: id, Bead: bead, At: at, Kind: "done"}}
}

func TestBuildHistoryMischtDieSpurenNeuesteZuerst(t *testing.T) {
	items, _ := buildHistory(
		[]ReportsEntry{repEntry("e1", "m.1", "2026-09-04T12:00:00Z")}, 1,
		[]collectors.BeadCommit{{Short: "a1b2c3d", At: "2026-09-04T13:00:00Z", Author: "Alex", Beads: []string{"m.1"}}}, 5,
		[]collectors.TrackerChange{{At: "2026-09-04T11:00:00Z", Actor: "Agent", Bead: "m.1", To: "closed"}}, 1,
	)

	will := []string{SourceCommit, SourceReport, SourceTracker}
	got := make([]string, len(items))
	for i, it := range items {
		got[i] = it.Source
	}
	if strings.Join(got, ",") != strings.Join(will, ",") {
		t.Fatalf("Reihenfolge %v, erwartet %v", got, will)
	}
	// Die Frage "von wem" muss ohne Blick in die Variante beantwortbar sein —
	// danach wird sortiert und gefiltert.
	if items[0].Actor != "Alex" || items[2].Actor != "Agent" {
		t.Errorf("Actor fehlt: %+v / %+v", items[0], items[2])
	}
	if items[1].Actor != "" {
		t.Errorf("Actor=%q — der Report-Store schreibt keinen Autor, also darf hier keiner stehen", items[1].Actor)
	}
	if items[0].Commit == nil || items[1].Report == nil || items[2].Tracker == nil {
		t.Error("je Herkunft muss genau das passende Detailfeld gesetzt sein")
	}
}

// Ein Commit ist EIN Eintrag, auch mit mehreren Ids. Der Fall, an dem die
// umgekehrte Variante zerbrach: Commit f1ec67b nennt 18 Ids und erzeugte 18
// identische Zeilen, die die halbe Zeitleiste fuellten.
func TestBuildHistoryZaehltEinenCommitEinmal(t *testing.T) {
	viele := []string{"m.1", "m.2", "m.3", "m.4", "m.5"}
	items, stats := buildHistory(nil, 0,
		[]collectors.BeadCommit{{Short: "a1b2c3d", At: "2026-09-04T13:00:00Z", Beads: viele}}, 1,
		nil, 0,
	)
	if len(items) != 1 {
		t.Fatalf("len=%d, erwartet 1 Eintrag fuer 1 Commit", len(items))
	}
	if items[0].Bead != "m.1" {
		t.Errorf("Bead=%q, erwartet den ersten genannten", items[0].Bead)
	}
	// Die vollstaendige Liste darf dabei nicht verloren gehen — die Ansicht
	// zeigt die uebrigen Ids am Eintrag.
	if len(items[0].Commit.Beads) != len(viele) {
		t.Errorf("Commit.Beads=%v, erwartet alle %d", items[0].Commit.Beads, len(viele))
	}
	if stats[SourceCommit].Shown != 1 {
		t.Errorf("Shown=%d, erwartet 1 Commit", stats[SourceCommit].Shown)
	}
}

// Jede Spur wird einzeln gekappt. Bei einem gemeinsamen Deckel wuerde die
// dichteste Spur die anderen verdraengen — in einem Repo mit vielen Commits
// bliebe von den Reports nichts uebrig, obwohl die Gegenueberstellung der
// Spuren der Zweck der Unterteilung ist.
func TestBuildHistoryKapptJedeSpurEinzeln(t *testing.T) {
	var commits []collectors.BeadCommit
	var changes []collectors.TrackerChange
	for i := 0; i < historyMax+30; i++ {
		commits = append(commits, collectors.BeadCommit{Short: "c" + itoa(i), At: "2026-09-04T13:00:00Z", Beads: []string{"m.1"}})
		changes = append(changes, collectors.TrackerChange{At: "2026-09-04T11:00:00Z", Bead: "m.1", To: "closed"})
	}
	reportItems := []ReportsEntry{repEntry("e1", "m.1", "2026-09-04T12:00:00Z")}

	items, stats := buildHistory(reportItems, 1, commits, len(commits), changes, len(changes))

	if stats[SourceCommit].Shown != historyMax || stats[SourceTracker].Shown != historyMax {
		t.Errorf("Shown commit=%d tracker=%d, erwartet je %d",
			stats[SourceCommit].Shown, stats[SourceTracker].Shown, historyMax)
	}
	if stats[SourceCommit].Total != len(commits) || stats[SourceTracker].Total != len(changes) {
		t.Errorf("Total commit=%d tracker=%d — die Gesamtzahl wird genannt, nicht verschwiegen",
			stats[SourceCommit].Total, stats[SourceTracker].Total)
	}
	// Der eine Report-Eintrag ueberlebt die dichten Spuren.
	gefunden := false
	for _, it := range items {
		if it.Source == SourceReport {
			gefunden = true
		}
	}
	if !gefunden {
		t.Error("der Report-Eintrag ist aus der Historie verdraengt worden")
	}
}

// Der wichtigste Fall in diesem Repo: es gibt Commits, aber keiner nennt eine
// Bead-Id. Ohne den Hinweis sieht das aus wie ein Defekt der Zuordnung.
func TestBuildHistoryErklaertLeereCommitSpur(t *testing.T) {
	_, stats := buildHistory(nil, 0, nil, 7, nil, 0)
	note := stats[SourceCommit].Note
	if !strings.Contains(note, "7 Commits") || !strings.Contains(note, "Bead-Id") {
		t.Errorf("Note=%q — der Hinweis muss die Zahl der durchsuchten Commits und die Konvention nennen", note)
	}

	_, stats2 := buildHistory(nil, 0, nil, 0, nil, 0)
	if !strings.Contains(stats2[SourceCommit].Note, "git log") {
		t.Errorf("Note=%q — 'keine Commits durchsucht' ist ein anderer Fall als 'keine Id gefunden'", stats2[SourceCommit].Note)
	}

	_, stats3 := buildHistory(nil, 0,
		[]collectors.BeadCommit{{Short: "a", At: "2026-09-04T13:00:00Z", Beads: []string{"m.1"}}}, 7, nil, 0)
	if stats3[SourceCommit].Note != "" {
		t.Errorf("Note=%q — bei Treffern gibt es nichts zu erklaeren", stats3[SourceCommit].Note)
	}
}

// Die PR-Spur steht schon in der Bilanz, bevor der Browser sie gefuellt hat.
// Sonst muesste die Ansicht die vierte Spur erfinden, und ein Fehlschlag des
// Netzaufrufs waere von "es gibt keine PRs" nicht zu unterscheiden.
func TestBuildHistoryNenntDiePRSpurAlsNochNichtGeholt(t *testing.T) {
	_, stats := buildHistory(nil, 0, nil, 0, nil, 0)
	pr, ok := stats[SourcePR]
	if !ok || pr.Note == "" {
		t.Errorf("PR-Spur = %+v (vorhanden: %v) — erwartet einen Hinweis auf GET /github", pr, ok)
	}
}

// Zeitgleiche Eintraege duerfen zwischen zwei Seitenaufrufen nicht die Plaetze
// tauschen: git-Zeitstempel haben Sekundenaufloesung, ein Commit und der
// bd-Statuswechsel dazu fallen regelmaessig auf dieselbe Sekunde.
func TestBuildHistoryIstBeiGleichemZeitstempelStabil(t *testing.T) {
	gleich := "2026-09-04T13:00:00Z"
	erste := ""
	for i := 0; i < 5; i++ {
		items, _ := buildHistory(
			[]ReportsEntry{repEntry("e1", "m.2", gleich)}, 1,
			[]collectors.BeadCommit{{Short: "a", At: gleich, Beads: []string{"m.1"}}}, 1,
			[]collectors.TrackerChange{{At: gleich, Bead: "m.3", To: "closed"}}, 1,
		)
		var got []string
		for _, it := range items {
			got = append(got, it.Source+":"+it.Bead)
		}
		reihe := strings.Join(got, ",")
		if i == 0 {
			erste = reihe
			continue
		}
		if reihe != erste {
			t.Fatalf("Reihenfolge wechselt: %q vs %q", reihe, erste)
		}
	}
}

// -------------------------------------------------------------- Zeitleiste

// Die Tagesgruppen der Ansicht sind Laeufe gleicher Day-Werte. Damit das traegt,
// muss Day aus der LOKALEN Zeit kommen: ein Ereignis um 01:30 lokal liegt in UTC
// noch am Vortag und stuende sonst unter dem falschen Datum.
func TestBuildHistoryBeschriftetTagUndUhrzeitLokal(t *testing.T) {
	// Fester Offset, damit der Test unabhaengig von der Zeitzone des Rechners
	// ist, auf dem er laeuft.
	alt := time.Local
	time.Local = time.FixedZone("TEST", 2*60*60)
	defer func() { time.Local = alt }()

	items, _ := buildHistory(nil, 0, nil, 0,
		[]collectors.TrackerChange{{At: "2026-09-03T23:30:00Z", Bead: "m.1", To: "closed"}}, 1)

	if len(items) != 1 {
		t.Fatalf("len=%d", len(items))
	}
	it := items[0]
	if it.Day != "2026-09-04" {
		t.Errorf("Day=%q, erwartet 2026-09-04 — 23:30Z ist bei +02:00 schon der naechste Tag", it.Day)
	}
	if it.Clock != "01:30" {
		t.Errorf("Clock=%q, erwartet 01:30", it.Clock)
	}
	if it.DayLabel != "Freitag, 04.09.2026" {
		t.Errorf("DayLabel=%q, erwartet \"Freitag, 04.09.2026\"", it.DayLabel)
	}
}

// Ein unlesbarer Stempel darf keinen Tag behaupten — die Ansicht faellt dann auf
// "ohne Datum" zurueck, und das ist eine ehrlichere Aussage als ein geratenes
// Datum, unter dem der Eintrag falsch einsortiert waere.
func TestStampLocalLaesstUnlesbaresLeer(t *testing.T) {
	for _, at := range []string{"", "gestern", "2026-09-04 12:00:00"} {
		if d, l, c := stampLocal(at); d != "" || l != "" || c != "" {
			t.Errorf("stampLocal(%q) = %q/%q/%q, erwartet dreimal leer", at, d, l, c)
		}
	}
}

// ---------------------------------------------------------------- Id-Menge

// Der Fall, auf den es ankommt: `bd list -t epic` blendet geschlossene Epics
// aus, also kennt CollectBeads die Ids der abgeschlossenen Arbeit nicht mehr —
// und genau die stehen in den aelteren Commits. Das Audit-Log ueberlebt das
// Schliessen.
func TestKnownBeadIDsNimmtAuchIdsAusDemAuditLog(t *testing.T) {
	maps := []collectors.BeadsMap{{
		ID: "gkp-uc7", Children: []collectors.BeadsSubBead{{ID: "gkp-uc7.1"}, {ID: "gkp-uc7.2"}},
	}}
	changes := []collectors.TrackerChange{
		{Bead: "gkp-37d"},   // geschlossenes Epic, aus bd verschwunden
		{Bead: "gkp-uc7.1"}, // Dopplung
		{Bead: "gkp-37d"},   // Dopplung
	}
	got := knownBeadIDs(maps, changes)

	will := "gkp-37d,gkp-uc7,gkp-uc7.1,gkp-uc7.2"
	if strings.Join(got, ",") != will {
		t.Errorf("got %v, erwartet %s (dedupliziert und sortiert)", got, will)
	}
}

func TestKnownBeadIDsLeerIstKeinFehler(t *testing.T) {
	if got := knownBeadIDs(nil, nil); len(got) != 0 {
		t.Errorf("got %v, erwartet leer", got)
	}
}
