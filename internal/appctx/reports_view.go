// The reports view (demo-axk.4): Repo > Map > Sub-bead > Entries. All the
// I/O (CollectBeads' subprocesses, reports.ListMaps' file reads) is gathered
// once in CollectReportsData, which becomes cards["reports"] in context.go —
// behind the same per-project cache (internal/cache) every other card hangs
// off, so a page render never re-shells bd/dot or re-reads the JSONL files.
// The tree-building below (gap detection, supersede resolution) is pure and
// deliberately lives in Go, not browser.js, so it is unit-testable without a
// browser: see reports_view_test.go.
package appctx

import (
	"sort"

	"github.com/niclasedge/git-planner-go/internal/collectors"
	"github.com/niclasedge/git-planner-go/internal/reports"
)

// ReportsEntry is one report-store entry plus a Superseded flag resolved
// against the rest of the map: an entry that some OTHER entry's Supersedes
// points at is a correction target — kept, marked, never hidden.
type ReportsEntry struct {
	reports.Entry
	Superseded bool `json:"superseded"`
}

// ReportsBead is one sub-bead under a map, with every report entry filed
// against it (newest first) plus the one thing the ingest step deliberately
// never checks: a closed bead with zero "done" entries is a gap, surfaced
// here instead of at ingest time (demo-axk decision 5).
type ReportsBead struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Status   string `json:"status"`
	ClosedAt string `json:"closed_at,omitempty"`
	// Ready is bd's own `bd ready` verdict, carried through from
	// collectors.BeadsSubBead. The startklar section is a filter over this
	// flag, not a second derivation of it (gkp-uc7.3).
	Ready bool `json:"ready"`
	// Priority and IssueType also come straight from bd, so a startklar line
	// can say what kind of work it is without the reader opening anything.
	Priority  int    `json:"priority"`
	IssueType string `json:"issue_type,omitempty"`
	// DueAt und DeferUntil sind Things' Deadline und When, roh wie bd sie
	// geschrieben hat (Spec §4.4). Sie gehen bewusst NICHT an den Browser:
	// `json:"-"`.
	//
	// Der Grund ist die Zeitbasis. Der Wert traegt ein "Z" und ist trotzdem
	// lokale Wanduhr; ein new Date(...) im Browser verschoebe ihn um den
	// UTC-Versatz und kippte einen Abendtermin auf den Folgetag. Waere er im
	// Rumpf, waere das eine Einladung, die irgendwann jemand annimmt. Gedeutet
	// wird in appctx/ansicht.go, und was der Browser bekommt, ist das Ergebnis
	// (AnsichtEintrag.Frist/Wann als Ortszeit-Text) — nie die Eingabe.
	DueAt      string `json:"-"`
	DeferUntil string `json:"-"`
	// URL ist der Ort der Aufgabe ausserhalb dieser Maschine. Bei einem Bead
	// leer — bd kennt keine —, bei einer GitHub-Aufgabe die Issue-URL
	// (gkp-pf2, githubaufgaben.go).
	//
	// `json:"-"` aus demselben Grund wie bei den beiden Feldern darueber: der
	// Browser bekommt das ERGEBNIS der Ansicht, nicht ihre Eingabe. Die URL
	// steht an der Zeile, die sie braucht (AnsichtEintrag.URL), und nicht an
	// jedem Bead in jeder Karte.
	URL string `json:"-"`
	// Proven says this bead has CURRENT done evidence in the report store
	// (reports.HasCurrentDoneEntry). Gap below is the narrower question — closed
	// AND unproven — and the two are not each other's negation: an open bead
	// with a done entry is proven without being closed, and that is exactly the
	// state the map header has to count separately (gkp-ar7.25). Deriving
	// "belegt" in the browser from Gap alone would silently equate the two.
	Proven  bool           `json:"proven"`
	Gap     bool           `json:"gap"`
	Entries []ReportsEntry `json:"entries"`
	// LastChange is the sort key computed by beadLastChange: the later of bd's
	// own updated_at and the newest report entry for this bead. Exported (and
	// shipped to the browser) so the order the view shows is inspectable
	// rather than an unexplained arrangement — browser.js renders m.beads in
	// array order and does not re-sort.
	LastChange string `json:"last_change,omitempty"`
	// Children sind die Unteraufgaben DIESER Aufgabe, beliebig tief — dieselbe
	// Form, die collectors.BeadsSubBead seit gkp-1lp liefert. Ein Enkel ist
	// keine eigene Kategorie: er steht hier genauso wie ein Kind, die Ansicht
	// rueckt ihn nur tiefer ein (Spec §4.1).
	//
	// omitempty aus demselben Grund wie im Collector: am 2026-09-13 hatte
	// dieses Repo 99 Beads und genau 3 davon Kinder, ein "children":[] an
	// jedem Blatt waere Ballast in jeder Antwort.
	Children []ReportsBead `json:"children,omitempty"`
	// Counts ist der Fortschritt dieser Aufgabe ueber ALLE Nachfahren
	// (gkp-b9n, Spec §6) — und nil, wenn es keine gibt.
	//
	// Der Zeiger ist die Regel "eine Aufgabe ohne Unteraufgaben bekommt keinen
	// Balken", im Typ ausgedrueckt statt in der Ansicht: ein Balken, der nur
	// 0 % oder 100 % zeigen kann, ist kein Fortschritt, sondern Rauschen. Mit
	// einem Wert statt eines Zeigers stuende an jedem Blatt ein 0/0, das der
	// Browser selbst wegfiltern muesste — also genau die Auswahl, die nach
	// CLAUDE.md hier gehoert und nicht dort.
	//
	// Fortschritt (Total/Open/Closed) und Belegstand (Proven/Gaps) liegen
	// absichtlich nebeneinander in derselben Struktur, ohne sich zu einer Zahl
	// zu mischen: "wie viel ist fertig" und "wie viel davon ist belegt" sind
	// zwei Fragen, und eine gemeinsame Quote macht beide unlesbar (dieselbe
	// Begruendung wie bei MapCounts).
	Counts *MapCounts `json:"counts,omitempty"`
	// Openspec ist der Stand eines OpenSpec-Changes (openspec-quelle, #10):
	// Status, Fortschritt, Gates, Issue-Losigkeit und Belege. nil an jeder
	// Zeile, die kein Change ist.
	Openspec *OpenspecStand `json:"openspec,omitempty"`
}

// ReportsMap is one wayfinder:map epic: its rendered plan.svg (from
// collectors.BeadsMap, itself cached — never re-rendered per request), its
// sub-beads with their entries, and Other — entries whose bead id is not
// among the map's current children (e.g. bd's hierarchy moved since the
// entry was written, or the entry targets the map bead itself). Other is
// never dropped silently.
type ReportsMap struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Status   string `json:"status"`
	Priority int    `json:"priority"`
	// Projekt ist der lokale Repo-Name (collectors.BeadsRepo.Name), aus dem
	// diese Karte stammt — bei einer GitHub-Karte AUSDRUECKLICH nicht die nwo,
	// die schon in ID/Title steht, sondern derselbe lokale Name wie bei den
	// beads-Karten desselben Aufrufs. Er reist in jeden AnsichtEintrag weiter
	// (ansicht.go: sammle/eintragAus), weil ReportsView.RepoName nur EINMAL pro
	// Antwort steht und eine kuenftige projektuebergreifende Sammelliste ihn
	// PRO ZEILE braucht, um nach Projekt zu gruppieren.
	Projekt string `json:"projekt,omitempty"`
	// ClosedAt beschriftet die Rueckschau: die Epics-Liste zeigt die zuletzt
	// geschlossenen Karten, und nur dieser Stempel unterscheidet "gerade fertig
	// geworden" von "liegt seit Wochen zu" (gkp-ar7.27).
	ClosedAt string `json:"closed_at,omitempty"`
	// IsMap distinguishes an epic somebody drew with /wayfinder from a plain
	// epic. Both are collected now; hiding the unlabeled ones was gkp-uc7.1.
	IsMap bool `json:"is_map"`
	// Loose markiert die synthetische Karte mit den Beads, die unter keinem
	// Epic haengen (collectors.looseMap). Sie ist kein Epic — die Ansicht
	// laesst deshalb den Plan-Graphen weg, den `bd graph` fuer diese Id nicht
	// zeichnen kann.
	Loose bool `json:"loose,omitempty"`
	// InProgress is the criterion for the "in Arbeit" section: this map's own
	// status, OR any child in progress. Both halves are needed — beads does
	// not move a parent's status when a child starts, so a map with people
	// working in it commonly sits at "open" (measured in orbit: orbit-kd3).
	InProgress bool `json:"in_progress"`
	// ReadyCount is how many children are startable, so the section heading
	// can say so without the browser counting twice.
	ReadyCount int `json:"ready_count"`
	// Counts is the status line of the map: how much work it holds, how much of
	// it is proven, how much is still open (gkp-ar7.25).
	Counts MapCounts `json:"counts"`
	// LastChange is this map's newest change: the later of bd's updated_at for
	// the epic itself and the newest LastChange among its beads. The sort key
	// for the map cards, mirroring what buildReportsMap already does one level
	// down for sub-beads (gkp-uc7.6).
	LastChange string         `json:"last_change,omitempty"`
	PlanSVG    string         `json:"plan_svg"`
	Beads      []ReportsBead  `json:"beads"`
	Other      []ReportsEntry `json:"other,omitempty"`
	// Unknown marks a synthetic ReportsMap built from a report-store file
	// whose name matched no bd epic CollectBeads returned (demo-axk.4
	// review Welle 2, finding 4): a typo'd or stale map= field in an MCP
	// payload. Every entry in such a file lands in Other (there is no known
	// child list to match against) instead of never being visited at all.
	Unknown bool `json:"unknown,omitempty"`
}

// MapCounts is one map's status in numbers (gkp-ar7.25). Bis dahin sagte die
// Ansicht zu einer Karte nur, ob sie "in Arbeit" ist — und stand keine Karte auf
// in_progress, sah man zu keiner einzigen Karte einen Status.
//
// Die vier Zahlen ueberschneiden sich absichtlich statt sich zu einer Summe zu
// addieren, denn sie beantworten verschiedene Fragen: Total ist der Umfang,
// Open der Rest an Arbeit, Proven der Anteil mit Beleg (auch an einem noch
// offenen Bead — ein Zwischenstand ist ein Beleg), und Gaps sind die
// geschlossenen ohne Beleg. Nur Gaps ist eine Beanstandung; die anderen drei
// sind Beschreibung. Eine einzige "Quote" waere kuerzer und wuerde genau die
// Unterscheidung einebnen, wegen der diese Ansicht existiert.
type MapCounts struct {
	Total  int `json:"total"`
	Open   int `json:"open"`
	Closed int `json:"closed"`
	Proven int `json:"proven"`
	Gaps   int `json:"gaps"`
}

// ReportsView is the browser-ready shape for cards["reports"]: Repo (with its
// worktrees, as badges) at the top, Maps below. Fails open at every level —
// Available=false or a non-empty Error never means the page is missing data
// it should have shown; it means the view says so instead of guessing.
type ReportsView struct {
	Available bool                       `json:"available"`
	Error     string                     `json:"error,omitempty"`
	RepoName  string                     `json:"repo_name,omitempty"`
	Worktrees []collectors.BeadsWorktree `json:"worktrees"`
	Maps      []ReportsMap               `json:"maps"`
	// Ansichten sind die Aufgabenlisten aus Spec §5 — Heute und, sobald
	// gkp-1oj sie anmeldet, ihre Geschwister. Jede ist eine AUSWAHL ueber Maps,
	// keine zweite Kopie der Aufgaben: ein Eintrag nennt die Bead-Id, den
	// Zeileninhalt holt die Ansicht aus Maps.
	//
	// Sie haengen an dieser Struktur statt an einer eigenen Route, weil sie den
	// Baum brauchen, der hier ohnehin gerade gebaut wurde — eine eigene Route
	// kostete einen zweiten Aufbau (Sekunden: CollectBeads schickt bd los), die
	// Auswahl selbst kostet Mikrosekunden. Der Preis ist der TTL des
	// Projekt-Cache: eine ueber Mitternacht offen stehende Seite zeigt bis zu
	// 15 Minuten lang den Vortag. Deshalb traegt jede Ansicht ihren Stand und
	// ihren Tag im Rumpf, statt "heute" zu behaupten.
	Ansichten map[string]AufgabenAnsicht `json:"ansichten"`
	// Overview ist der Graph ueber ALLE Karten mit offener Arbeit (gkp-3sq) —
	// das Bild, das die Ansicht zuerst zeigt. Er haengt an dieser Struktur und
	// nicht an einer eigenen Route wie /plan-svg, weil er aus den Maps oben
	// gebaut wird und keinen bd-Aufruf kostet, sondern einen dot-Prozess.
	// Aufbau und Begruendung: reports_overview.go.
	Overview MapsOverview `json:"overview"`
	// Timeline is the report-store half of the history section: every report
	// entry for this repo, newest first, across all maps, capped at
	// timelineMax (gkp-uc7.5).
	//
	// It reads the report store rather than bd on purpose. `bd list` hides
	// closed epics — measured 2026-09-04: orbit-37d was closed with six
	// finished children and vanished from `bd list -t epic` the same minute —
	// so a history built on beads.Maps would lose exactly the work that has
	// become history. The JSONL store is append-only and outlives the close.
	//
	// Not shipped to the browser since gkp-uc7.8: it goes into History below,
	// as the "report" track among four. Sending it twice would duplicate every
	// entry (proof text included) in the payload for nothing.
	Timeline []ReportsEntry `json:"-"`
	// TimelineTotal is how many entries exist in total, so a capped list can
	// say what it is not showing instead of ending without explanation.
	TimelineTotal int `json:"timeline_total"`
	// History is the same section subdivided by origin (gkp-uc7.8): report
	// entries, commits that name a bead, and bd's own status changes with
	// their actor — one chronological list, each item labelled with where it
	// came from. HistoryStats is the per-track balance (shown/total, and why a
	// track is empty). Both built by buildHistory in reports_history.go.
	History      []HistoryItem            `json:"history"`
	HistoryStats map[string]HistorySource `json:"history_stats"`
	// BeadIDs is every bead id this repo can legitimately refer to. It exists
	// for exactly one consumer: browser.js matches the pull requests it
	// fetches from GET /github against this set. The matching cannot happen
	// here because the PR data deliberately never enters this path (a network
	// call in the project-open path is what gkp-isj is about), and it must not
	// happen against an id PATTERN — that produces phantom beads (see
	// collectors.BeadRefsIn).
	BeadIDs []string `json:"bead_ids"`
	// Skipped is readEntries' corrupt/oversized-line counter, summed across
	// every map file for this repo (reports.ListMaps) — a lossy read is a
	// finding for this view to show, not a detail to swallow.
	Skipped int `json:"skipped"`
}

// timelineMax caps the history section. Without a cap it grows without bound
// and takes the page's render time with it — the store is append-only, so it
// only ever gets longer. A hundred entries is far more than the section is
// read for, and TimelineTotal names the rest.
const timelineMax = 100

// CollectReportsData is the I/O side: CollectBeads (subprocess-heavy: git
// worktree list, bd list/show/graph, dot) plus reports.ListMaps for the SAME
// repo identity (collectors.BeadsRepo.Name — demo-axk.4 integration gap
// 1), then the pure tree-build below. Called once per project behind
// cache.GetProjectContext (context.go); never per page render.
func CollectReportsData(cwd string) ReportsView {
	beads := collectors.CollectBeads(cwd)
	view := ReportsView{
		Available: beads.Available,
		Error:     beads.Error,
		RepoName:  beads.Name,
		Worktrees: beads.Worktrees,
		Maps:      []ReportsMap{},
		// Auch ohne beads angemeldet: eine Ansicht, die es gibt, aber leer ist,
		// ist eine Auskunft; eine fehlende Ansicht waere ein Loch, das die
		// Navigation nicht von "kenne ich nicht" unterscheiden kann.
		Ansichten: AnsichtenBauen(nil, jetztFuerAnsichten()),
	}
	if !beads.Available || beads.Name == "" {
		return view
	}
	mapsEntries, skipped, err := reports.ListMaps(beads.Name)
	view.Skipped = skipped
	if err != nil {
		// Fail open: a broken report store never blocks the beads side of the
		// view — Maps still get built below, just without entries.
		if view.Error == "" {
			view.Error = "Report-Store: " + err.Error()
		}
		mapsEntries = map[string][]reports.Entry{}
	}
	view.Maps = baueKarten(beads, mapsEntries)
	// Die EINE Zeile, an der alle Aufgaben-Ansichten haengen. Sie nennt keine
	// einzelne Ansicht, also kostet eine weitere Ansicht hier keine Aenderung.
	view.Ansichten = AnsichtenBauen(view.Maps, jetztFuerAnsichten())
	// Nach sortMaps: der Ueberblick uebernimmt die Reihenfolge der Karten
	// unveraendert, damit das Gitter oben und die Liste links dieselbe Folge
	// haben — sonst zeigt ein Klick im Bild an eine andere Stelle, als das Auge
	// erwartet.
	view.Overview = buildMapsOverview(cwd, view.Maps)
	view.Timeline, view.TimelineTotal = buildTimeline(mapsEntries)

	// Die drei lokalen Spuren der Historie (gkp-uc7.8). Das Audit-Log wird VOR
	// den Commits gelesen: es liefert die Ids der abgeschlossenen Arbeit, die
	// bd nicht mehr auflistet, und ohne die faende die Commit-Zuordnung genau
	// die aeltesten Commits nicht.
	changes, changesTotal := collectors.CollectTrackerChanges(cwd)
	view.BeadIDs = knownBeadIDs(beads.Maps, changes)
	commits, scanned := collectors.CollectBeadCommits(cwd, view.BeadIDs)
	view.History, view.HistoryStats = buildHistory(
		view.Timeline, view.TimelineTotal, commits, scanned, changes, changesTotal,
	)
	return view
}

// baueKarten ist der Baum der Ansicht: jede Karte mit ihren Aufgaben, die
// Waisen-Karten hinterher, sortiert. Alles, was CollectReportsData darueber
// hinaus tut — Ueberblicksgraph, Zeitleiste, Historie —, haengt an diesem
// Ergebnis und aendert es nicht mehr.
//
// Herausgeloest fuer den zweiten Aufrufer, die Schreibantwort (gkp-8a0,
// teilbaum.go): sie braucht den Baum und sonst nichts. Dass sie dafuer DIESE
// Funktion ruft und keine eigene, ist der ganze Punkt — ein zweiter Erbauer
// waere die Fehlerklasse aus gkp-1lp/gkp-b9n, nur diesmal zwischen Lesepfad und
// Schreibpfad.
func baueKarten(beads collectors.BeadsRepo, mapsEntries map[string][]reports.Entry) []ReportsMap {
	maps := make([]ReportsMap, 0, len(beads.Maps))
	knownMapIDs := make(map[string]bool, len(beads.Maps))
	for _, m := range beads.Maps {
		maps = append(maps, buildReportsMap(m, mapsEntries[m.ID]))
		knownMapIDs[m.ID] = true
	}
	maps = append(maps, orphanedMaps(mapsEntries, knownMapIDs)...)
	sortMaps(maps)
	// Projekt wird HIER gesetzt und nicht in buildReportsMap/orphanedMaps: beide
	// kennen nur ihre eigene collectors.BeadsMap, nicht das Repo, aus dem sie
	// stammen. beads.Name ist fuer JEDE Karte dieses Aufrufs derselbe Wert.
	for i := range maps {
		maps[i].Projekt = beads.Name
	}
	return maps
}

// sortMaps puts the newest change first, the same rule buildReportsMap already
// applies to sub-beads one level down (gkp-uc7.6).
//
// Before this, the cards came out in bd's order followed by the orphans, i.e.
// in an arrangement with no discernible logic that could also shift between
// two page loads. Sorting in Go rather than in browser.js for the reason this
// package exists: the tree the view renders is built and unit-tested here, and
// rpMapHtml maps over the array without a sort of its own.
func sortMaps(maps []ReportsMap) {
	sort.SliceStable(maps, func(i, j int) bool {
		if maps[i].LastChange != maps[j].LastChange {
			// Empty LastChange sorts last, which falls out of a plain string
			// compare: "" is below any RFC3339 stamp.
			return maps[i].LastChange > maps[j].LastChange
		}
		// Same instant, or both unknown: id keeps it deterministic instead of
		// leaving the order to bd's arbitrary sequence.
		return maps[i].ID < maps[j].ID
	})
}

// buildTimeline flattens every report entry of the repo into one newest-first
// list and reports how many there were in total (gkp-uc7.5).
//
// Supersede is resolved ACROSS maps here, not per map as buildReportsMap does
// it. That difference is the point: a correction can be filed under a
// different map= than the entry it retracts (a renamed epic, a typo in the
// ingest payload — the very case orphanedMaps exists for), and a history that
// resolved it per map would show a retracted entry as current. The per-map
// tree keeps its own narrower resolution because there an entry under another
// map is genuinely not part of that map's story.
func buildTimeline(mapsEntries map[string][]reports.Entry) ([]ReportsEntry, int) {
	superseded := map[string]bool{}
	total := 0
	for _, entries := range mapsEntries {
		total += len(entries)
		for _, e := range entries {
			if e.Supersedes != "" {
				superseded[e.Supersedes] = true
			}
		}
	}

	out := make([]ReportsEntry, 0, min(total, timelineMax))
	alle := make([]ReportsEntry, 0, total)
	for _, entries := range mapsEntries {
		for _, e := range entries {
			alle = append(alle, ReportsEntry{Entry: e, Superseded: superseded[e.ID]})
		}
	}
	// Map iteration order is random, so the merged list has to be re-sorted
	// even though each map's own slice arrives newest-first from ListMaps.
	// Entry.At is RFC3339 UTC with fixed-width fields, so a string compare is
	// a chronological one; ID breaks ties so two entries written in the same
	// second do not swap between page loads.
	sort.Slice(alle, func(i, j int) bool {
		if alle[i].At != alle[j].At {
			return alle[i].At > alle[j].At
		}
		return alle[i].ID < alle[j].ID
	})
	for i := range alle {
		if i >= timelineMax {
			break
		}
		out = append(out, alle[i])
	}
	return out, total
}

// orphanedMaps returns one synthetic ReportsMap per report-store file whose
// name matches no bd epic bd currently knows about (demo-axk.4 review
// Welle 2, finding 4: CollectReportsData used to iterate ONLY beads.Maps, so
// a map= typo or a renamed/deleted epic made an entire report file invisible
// — not filed under Other, not counted in Skipped, just gone). Reusing
// buildReportsMap with an empty Children list routes every entry straight
// into Other, the same "never drop it silently" path already used for a
// bead id that doesn't match a known map's children. Sorted by ID since map
// iteration order is not stable.
func orphanedMaps(mapsEntries map[string][]reports.Entry, knownMapIDs map[string]bool) []ReportsMap {
	ids := make([]string, 0, len(mapsEntries))
	for id := range mapsEntries {
		if !knownMapIDs[id] {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	out := make([]ReportsMap, 0, len(ids))
	for _, id := range ids {
		m := buildReportsMap(collectors.BeadsMap{ID: id, Status: "unknown"}, mapsEntries[id])
		m.Unknown = true
		m.Counts = countOrphanEntries(m.Other)
		out = append(out, m)
	}
	return out
}

// countOrphanEntries zaehlt die Statuszeile einer Karte, die bd nicht mehr
// kennt — aus dem Report-Store statt aus den Sub-beads.
//
// Der haeufigste Grund dafuer ist genau der Fall, den die Statusuebersicht
// abdecken soll: eine ABGESCHLOSSENE Karte. `bd list -t epic` blendet
// geschlossene Epics aus (gemessen 2026-09-04 an orbit-37d), also hat eine
// fertige Karte hier keine Children mehr, und countBeads wuerde ueberall 0
// melden — ausgerechnet fuer die Karten, deren Bilanz am meisten interessiert.
//
// Open und Closed bleiben 0, und das ist keine Luecke, sondern die ehrliche
// Antwort: der Report-Store kennt keine bd-Zustaende. Die Ansicht nennt fuer
// solche Karten deshalb nur Umfang und Belege.
func countOrphanEntries(entries []ReportsEntry) MapCounts {
	proven := map[string][]reports.Entry{}
	for _, e := range entries {
		proven[e.Bead] = append(proven[e.Bead], e.Entry)
	}
	var c MapCounts
	c.Total = len(proven)
	for _, es := range proven {
		if reports.HasCurrentDoneEntry(es) {
			c.Proven++
		}
	}
	return c
}

// buildReportsMap files entries against their bead (Other for anything that
// doesn't match a known child), resolves supersede flags, and computes the
// closed-without-proof gap per bead.
func buildReportsMap(m collectors.BeadsMap, entries []reports.Entry) ReportsMap {
	superseded := map[string]bool{}
	for _, e := range entries {
		if e.Supersedes != "" {
			superseded[e.Supersedes] = true
		}
	}
	// entries ist ausdruecklich DIE Eintraege EINER Karte (mapsEntries[m.ID] aus
	// CollectReportsData) und byBead gruppiert sie fuer ALLE Kinder dieser Karte
	// auf einmal — die andere Form als reports.LoadBead (gkp-6l2, Abgrenzung zu
	// gkp-e9h), das GENAU EINEN Bead ueber ALLE Karten des Repos sucht. Ein
	// Umbau auf LoadBead braeuchte nicht nur einen Aufruf je Kind statt eines
	// gemeinsamen Durchlaufs; er wuerde auch die Other-Spalte unten entwerten:
	// die Frage dort ist "gehoert dieser Eintrag zu einem bekannten Kind DIESER
	// Karte", und eine kartenuebergreifende Suche wuerde Belege anderer Karten
	// hier einsortieren, statt sie (wie orphanedMaps es tut) als eigenen Fund
	// sichtbar zu machen. Bestehender Test:
	// TestBuildReportsMap_UnknownBeadGoesToOtherNewestFirst.
	byBead := map[string][]ReportsEntry{}
	for _, e := range entries {
		byBead[e.Bead] = append(byBead[e.Bead], ReportsEntry{Entry: e, Superseded: superseded[e.ID]})
	}

	knownBead := map[string]bool{}
	beadsOut := buildReportsBeads(m.Children, byBead, knownBead)

	var other []ReportsEntry
	for beadID, es := range byBead {
		if !knownBead[beadID] {
			other = append(other, es...)
		}
	}
	// byBead's iteration order is random (Go map); entries within one bead's
	// slice keep reports.ListMaps' newest-first order, but Other interleaves
	// multiple beads — re-sort by At so the merged list is still newest-first.
	sort.Slice(other, func(i, j int) bool { return other[i].At > other[j].At })

	return ReportsMap{
		ID: m.ID, Title: m.Title, Status: m.Status, Priority: m.Priority, PlanSVG: m.PlanSVG,
		ClosedAt:   m.ClosedAt,
		IsMap:      m.IsMap,
		Loose:      m.Loose,
		InProgress: mapInProgress(m),
		ReadyCount: readyCount(beadsOut),
		Counts:     countBeads(beadsOut),
		LastChange: mapLastChange(m, beadsOut),
		Beads:      beadsOut, Other: other,
	}
}

// buildReportsBeads baut eine Ebene des Aufgabenbaums und steigt dabei in jede
// Unteraufgabe ab — beliebig tief, weil der Collector seit gkp-1lp beliebig
// tief liefert (Spec §4.1: eine Tiefenbegrenzung gibt es im Modell nicht).
//
// known sammelt JEDE besuchte Id, nicht nur die der ersten Ebene. Daran haengt
// die Zuordnung "gehoert zu dieser Karte": stuenden dort nur die Kinder, wanderte
// der Beleg eines Enkels aus seinem Bead heraus nach Other, und der Enkel saehe
// unbelegt aus, obwohl sein Beleg vorliegt.
//
// Zyklen sind hier kein Thema: collectors.beadsBaum schneidet die Rueckkante
// bereits beim Aufbau, dieser Baum ist also endlich.
func buildReportsBeads(subs []collectors.BeadsSubBead, byBead map[string][]ReportsEntry, known map[string]bool) []ReportsBead {
	if len(subs) == 0 {
		// nil statt einer leeren Liste, damit Children dieselbe Form hat wie
		// beim Collector: ein Blatt traegt das Feld gar nicht (omitempty).
		return nil
	}
	out := make([]ReportsBead, 0, len(subs))
	for _, sb := range subs {
		known[sb.ID] = true
		beadEntries := byBead[sb.ID]
		if beadEntries == nil {
			beadEntries = []ReportsEntry{}
		}
		belegt := hasDoneEntry(beadEntries)
		b := ReportsBead{
			ID:         sb.ID,
			Title:      sb.Title,
			Status:     sb.Status,
			ClosedAt:   sb.ClosedAt,
			Ready:      sb.Ready,
			Priority:   sb.Priority,
			IssueType:  sb.IssueType,
			DueAt:      sb.DueAt,
			DeferUntil: sb.DeferUntil,
			Proven:     belegt,
			Gap:        sb.Status == statusGeschlossen && !belegt,
			Entries:    beadEntries,
			LastChange: beadLastChange(sb, beadEntries),
			Children:   buildReportsBeads(sb.Children, byBead, known),
		}
		// Fehlendes Feld und leere Liste sind hier dasselbe und gehoeren es auch
		// zu sein: beides heisst "keine Unteraufgaben", also kein Balken. Der
		// Collector schickt nil (omitempty), ein Testaufbau schreibt manchmal
		// [] — eine Aufgabe ohne Nachfahren bleibt in beiden Faellen eine ohne.
		if len(b.Children) > 0 {
			c := countBeads(b.Children)
			b.Counts = &c
			// Der Ast bleibt zusammen: eine Aufgabe ist mindestens so neu wie
			// ihre juengste Unteraufgabe. Sonst sackte sie unter ihre eigenen
			// Kinder, sobald dort etwas passiert, und die Sortierung risse den
			// Ast in der Liste auseinander.
			for _, k := range b.Children {
				if k.LastChange > b.LastChange {
					b.LastChange = k.LastChange
				}
			}
		}
		out = append(out, b)
	}

	// Newest change first. bd returns children in its own order (neither
	// chronological nor by id), which showed up in the view as an arrangement
	// with no discernible logic. "Newest on top" is what the rest of this
	// frontend already does for file- and session-lists (browser.js sorts
	// those by mtime descending), so the reports view now matches.
	//
	// Sorted here rather than in browser.js on purpose: this package owns the
	// tree the view renders (gap detection, supersede resolution live here
	// too) precisely so the ordering is unit-testable without a browser.
	// rpMapHtml maps over m.beads in array order and adds no sort of its own.
	// Die Regel gilt auf JEDER Ebene, nicht nur auf der ersten — Geschwister
	// sind Geschwister, gleich wie tief sie haengen.
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].LastChange != out[j].LastChange {
			// Empty LastChange (no updated_at, no entries) sorts last, which
			// falls out of a plain string compare: "" is below any timestamp.
			return out[i].LastChange > out[j].LastChange
		}
		// Same instant (or both unknown): id keeps the order deterministic
		// instead of leaving it to bd's arbitrary sequence.
		return out[i].ID < out[j].ID
	})
	return out
}

// istInArbeit is the status string bd uses; kept in one place because it is
// checked for the map and for every child.
const istInArbeit = "in_progress"

// statusGeschlossen ist bds Endzustand. Alles andere — open, in_progress,
// deferred, blocked — zaehlt als offen. Bewusst so herum: bd kann Zustaende
// dazubekommen (deferred kam so), und eine Liste der offenen Zustaende wuerde
// einen neuen stillschweigend als "fertig" zaehlen. Ein Endzustand kommt nicht
// dazu, ohne dass es auffaellt.
const statusGeschlossen = "closed"

// countBeads zaehlt die Statuszeile ueber die uebergebenen Beads UND alles,
// was unter ihnen haengt — beliebig tief (gkp-b9n, Spec §6). Aufgerufen mit
// den Kindern einer Karte ergibt das die Zahlen der Karte, aufgerufen mit den
// Kindern einer Aufgabe die Zahlen dieser Aufgabe.
//
// Gezaehlt werden immer die NACHFAHREN, nie der Knoten selbst. Der Grund ist
// derselbe auf beiden Ebenen: eine Aufgabe wird geschlossen, WEIL ihre
// Unteraufgaben fertig sind — zaehlte sie sich mit, stuende im Moment der
// Fertigstellung n/(n+1) statt n/n, der Balken erreichte nie 100 %. Und die
// Karte zaehlt seit jeher ihre Kinder ohne sich selbst; zwei verschiedene
// Regeln auf zwei Ebenen waeren beim Lesen nicht zu merken.
//
// Hier und nicht in browser.js, aus demselben Grund wie der Rest dieses Pakets:
// die Ansicht zeichnet die Zahlen, sie leitet sie nicht ab — sonst haette die
// Belegregel (reports.HasCurrentDoneEntry, mit Supersede- und Reopen-Fall) eine
// zweite, unvollstaendige Fassung im Browser.
func countBeads(beads []ReportsBead) MapCounts {
	var c MapCounts
	for _, b := range beads {
		c.Total++
		if b.Status == statusGeschlossen {
			c.Closed++
		} else {
			c.Open++
		}
		if b.Proven {
			c.Proven++
		}
		if b.Gap {
			c.Gaps++
		}
		// Abstieg ueber Children und NICHT ueber das bereits gefuellte
		// b.Counts: die Zahl wird so aus dem Baum selbst gerechnet, nicht aus
		// einem Feld, das ein anderer Erbauer (etwa der Teilbaum einer
		// Schreibantwort) vielleicht nicht gesetzt hat. Bei einem Blatt ist
		// Children leer oder fehlt ganz — beides liefert den Nullwert, und
		// damit ist der Fall vollstaendig behandelt.
		sub := countBeads(b.Children)
		c.Total += sub.Total
		c.Open += sub.Open
		c.Closed += sub.Closed
		c.Proven += sub.Proven
		c.Gaps += sub.Gaps
	}
	return c
}

// mapInProgress decides whether a map belongs in the "in Arbeit" section: its
// own status, OR any child in progress (gkp-uc7.4).
//
// The second half is not redundant. beads does not move a parent's status when
// a child starts, so an epic with people working in it commonly still reads
// "open" — measured in orbit on 2026-09-04: orbit-kd3 sat at open with two open
// children while it was being worked on. Judging by the epic's status alone
// would leave the section empty on exactly the repos that have running work.
// Der Abstieg geht ueber ALLE Nachfahren, nicht nur ueber die erste Ebene: bd
// bewegt den Status eines Elternteils auf keiner Ebene, wenn ein Kind startet,
// also gilt dasselbe Argument fuer den Enkel wie fuer das Kind.
func mapInProgress(m collectors.BeadsMap) bool {
	if m.Status == istInArbeit {
		return true
	}
	return einerInArbeit(m.Children)
}

func einerInArbeit(subs []collectors.BeadsSubBead) bool {
	for _, c := range subs {
		if c.Status == istInArbeit || einerInArbeit(c.Children) {
			return true
		}
	}
	return false
}

// readyCount counts the startable descendants of a map — startklar ist
// startklar, gleich in welcher Tiefe. Zaehlte die Ueberschrift nur die erste
// Ebene, meldete eine Karte, deren startklare Arbeit unter einer Aufgabe
// haengt, null startklare Beads.
func readyCount(beads []ReportsBead) int {
	n := 0
	for _, b := range beads {
		if b.Ready {
			n++
		}
		n += readyCount(b.Children)
	}
	return n
}

// mapLastChange is the "newest change" of a whole map: the later of bd's
// updated_at for the epic itself and the newest LastChange among its beads.
// Same shape and same reasoning as beadLastChange one level down — an epic
// whose own fields moved counts as changed even when no child did, and a map
// full of fresh reports counts as changed even if bd never touched the epic.
func mapLastChange(m collectors.BeadsMap, beads []ReportsBead) string {
	newest := m.UpdatedAt
	for _, b := range beads {
		if b.LastChange > newest {
			newest = b.LastChange
		}
	}
	return newest
}

// beadLastChange is the "newest change" of one sub-bead: the later of bd's own
// updated_at and the newest report entry filed against it.
//
// Both signals are needed, and neither alone is enough. updated_at alone
// ignores a report that just landed — notifyBead's bd comment would normally
// bump it, but that call is explicitly fail-open (a missing bd binary, an
// invalid bead id or a timeout all skip it silently), so a report's arrival
// must not depend on it. The newest entry alone would collapse every bead that
// has no entries yet — most of them, in a young map — into one undifferentiated
// block at the bottom, losing the bd-side ordering that does exist.
//
// Entry.At and UpdatedAt are both RFC3339 UTC with fixed-width fields, so a
// plain string comparison is a chronological one; the same property is already
// relied on in hasDoneEntry below.
func beadLastChange(sb collectors.BeadsSubBead, entries []ReportsEntry) string {
	newest := sb.UpdatedAt
	for _, e := range entries {
		if e.At > newest {
			newest = e.At
		}
	}
	return newest
}

// hasDoneEntry decides whether a closed bead has current "done" evidence.
//
// The rule itself — and the two ways the naive version got it wrong — lives in
// reports.HasCurrentDoneEntry, because the MCP close tool builds its own gate
// on that same core. Two copies of the lifecycle logic would eventually
// disagree, and then the view would warn about a gap the API had already
// accepted (or the reverse). This wrapper only strips the view's Superseded
// decoration, which the rule deliberately does not consult.
//
// The view stays on the lenient rule on purpose: reports.HasValidatedDoneEntry
// additionally demands a visual proof, but that only governs NEW closures.
// Beads closed earlier under the text-only rule were closed legitimately, and
// judging them by today's gate would mark them all as gaps.
func hasDoneEntry(entries []ReportsEntry) bool {
	plain := make([]reports.Entry, len(entries))
	for i, e := range entries {
		plain[i] = e.Entry
	}
	return reports.HasCurrentDoneEntry(plain)
}
