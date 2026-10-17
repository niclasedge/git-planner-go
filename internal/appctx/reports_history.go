// Die Historie nach Herkunft (gkp-uc7.8). Die Zeitleiste war bisher eine
// Liste von Report-Store-Eintraegen; sie beantwortet damit "was wurde
// berichtet", aber nicht "was ist passiert und von wem". Vier Spuren, ein
// gemeinsamer Zeitstrahl:
//
//	report   der Report-Store  — was ein Agent als Plan/Beleg eingeliefert hat
//	commit   git log           — was tatsaechlich im Code gelandet ist, mit Autor
//	tracker  .beads/interactions.jsonl — wer den Bead bewegt hat, mit Begruendung
//	pr       GET /github       — kommt erst im Browser dazu (siehe unten)
//
// Die PR-Spur fehlt hier absichtlich. Sie kostet zwei Netzaufrufe, und die
// gehoeren nicht in den Pfad, der ein Projekt oeffnet (derselbe Grund, aus dem
// die GitHub-Karte eine eigene Route hat). Der Browser holt sie beim Ansehen
// aus GET /github und ordnet sie den Beads zu — dafuer und nur dafuer schickt
// ReportsView die Liste der bekannten Bead-Ids mit.
package appctx

import (
	"sort"
	"time"

	"github.com/niclasedge/git-planner-go/internal/anzeigezeit"
	"github.com/niclasedge/git-planner-go/internal/collectors"
)

// Die vier Herkunftsarten. Als Konstanten, weil der Wert im JSON steht und
// browser.js danach filtert — ein Tippfehler waere eine leere Spur, kein
// Compile-Fehler.
const (
	SourceReport  = "report"
	SourceCommit  = "commit"
	SourceTracker = "tracker"
	SourcePR      = "pr"
)

// HistoryItem ist ein Schritt auf dem Zeitstrahl. Genau eines der Detailfelder
// ist gesetzt, passend zu Source; At, Bead und Actor sind hochgezogen, damit
// Sortierung und Filterung nicht in jede Variante schauen muessen.
type HistoryItem struct {
	Source string `json:"source"`
	At     string `json:"at"`
	Bead   string `json:"bead,omitempty"`
	// Actor ist die Antwort auf "von wem" — Commit-Autor bzw. bd-actor. Beim
	// Report-Store bleibt es leer: der Store schreibt keinen Autor, weil dort
	// immer der Agent selbst einliefert.
	Actor   string                    `json:"actor,omitempty"`
	Report  *ReportsEntry             `json:"report,omitempty"`
	Commit  *collectors.BeadCommit    `json:"commit,omitempty"`
	Tracker *collectors.TrackerChange `json:"tracker,omitempty"`
	// Tagesschluessel, Tagesbeschriftung und Uhrzeit, aus At in LOKALER Zeit
	// abgeleitet (gkp-ar7.24). Hier und nicht im Browser: die Ansicht gruppiert
	// nach Day, beschriftet den Trenner mit DayLabel und setzt Clock an die
	// Schiene — sie zeichnet, sie rechnet nicht. Leer, wenn At nicht lesbar
	// ist; die Ansicht zeigt dann "ohne Datum", statt einen Tag zu behaupten.
	Day      string `json:"day,omitempty"`
	DayLabel string `json:"day_label,omitempty"`
	Clock    string `json:"clock,omitempty"`
}

// Go kennt nur englische Wochentagsnamen und keine Locale. Ein fester Array ist
// ehrlicher als eine Abhaengigkeit, die auf einer anderen Maschine anders
// ausfaellt. Reihenfolge wie time.Weekday: Sonntag ist 0.
var wochentage = [...]string{"Sonntag", "Montag", "Dienstag", "Mittwoch", "Donnerstag", "Freitag", "Samstag"}

// stampLocal zerlegt einen RFC3339-Zeitstempel in Tagesschluessel,
// Tagesbeschriftung und Uhrzeit — in LOKALER Zeit.
//
// At ist ueberall UTC (dafuer sorgt collectors.toUTC), aber "an welchem Tag war
// das" ist eine Frage in der Zeitzone dessen, der hinsieht: ein Ereignis um
// 01:30 lokal liegt in UTC noch am Vortag und stuende sonst unter dem falschen
// Datum. Die Zone des Lesers ist die Anzeigezone (GKP_ZONE, sonst
// Europe/Berlin; letzte-aktionen, #126), nicht die des Containers.
func stampLocal(at string) (day, label, clock string) {
	t, err := time.Parse(time.RFC3339, at)
	if err != nil {
		return "", "", ""
	}
	l := t.In(anzeigezeit.Zone())
	return l.Format("2006-01-02"),
		wochentage[int(l.Weekday())] + ", " + l.Format("02.01.2006"),
		l.Format("15:04")
}

// HistorySource ist die Bilanz einer Spur: was gezeigt wird, wie viel es
// insgesamt gibt, und im Zweifel warum nichts da ist.
//
// Note ist der wichtigste Teil davon. Eine leere Spur hat zwei sehr
// verschiedene Gruende — es gibt nichts, oder die Zuordnung greift nicht —,
// und ohne den Unterschied sieht ein Repo ohne Bead-Ids in den
// Commit-Subjects genauso aus wie ein Repo ohne Commits.
type HistorySource struct {
	Shown int    `json:"shown"`
	Total int    `json:"total"`
	Note  string `json:"note,omitempty"`
}

// historyMax kappt jede Spur einzeln, nicht die Summe. Bei einem gemeinsamen
// Deckel wuerde die dichteste Spur die anderen verdraengen — in einem Repo mit
// vielen Commits und wenigen Reports bliebe von den Reports nichts uebrig,
// obwohl gerade die Gegenueberstellung der Spuren der Zweck der Unterteilung
// ist. Derselbe Wert wie timelineMax, aus demselben Grund.
const historyMax = timelineMax

// buildHistory mischt die drei lokalen Spuren zu einem Zeitstrahl.
//
// Die Report-Spur kommt fertig herein (buildTimeline: schon gekappt, schon
// supersede-aufgeloest, schon neueste-zuerst) statt hier ein zweites Mal
// gebaut zu werden — die Aufloesung ueber Kartengrenzen hinweg ist knifflig
// genug, dass sie genau eine Stelle haben muss.
//
// Ein Commit ist EIN Eintrag, auch wenn er mehrere Beads nennt; die Id-Liste
// haengt als Eigenschaft daran (BeadCommit.Beads).
//
// Zuerst stand hier das Gegenteil — ein Eintrag je genanntem Bead, damit ein
// Commit unter jedem seiner Beads auffindbar ist. Das ist am ersten echten
// Fall zerbrochen: Commit f1ec67b nennt 18 Ids (ein Sammel-Commit mit
// Refs-Zeilen) und erzeugte 18 identische Zeilen hintereinander, die die halbe
// Zeitleiste fuellten. Der Nutzen waere ein Filter je Bead gewesen, den diese
// Sektion gar nicht hat: gefiltert wird nach HERKUNFT, gelesen wird
// chronologisch. In dieser Lesart ist ein Commit ein Ereignis, nicht ein
// Ereignis je Bead.
func buildHistory(
	reportItems []ReportsEntry, reportTotal int,
	commits []collectors.BeadCommit, commitsScanned int,
	changes []collectors.TrackerChange, changesTotal int,
) ([]HistoryItem, map[string]HistorySource) {
	out := make([]HistoryItem, 0, len(reportItems)+len(commits)+len(changes))

	for i := range reportItems {
		e := reportItems[i]
		out = append(out, HistoryItem{
			Source: SourceReport,
			At:     e.At,
			Bead:   e.Bead,
			Report: &e,
		})
	}

	commitsShown := 0
	for i := range commits {
		if commitsShown >= historyMax {
			break
		}
		c := commits[i]
		commitsShown++
		// Bead ist der erste genannte; die vollstaendige Liste steht in
		// Commit.Beads und wird von der Ansicht dort gelesen.
		bead := ""
		if len(c.Beads) > 0 {
			bead = c.Beads[0]
		}
		out = append(out, HistoryItem{
			Source: SourceCommit,
			At:     c.At,
			Bead:   bead,
			Actor:  c.Author,
			Commit: &c,
		})
	}

	changesShown := 0
	for i := range changes {
		if changesShown >= historyMax {
			break
		}
		ch := changes[i]
		changesShown++
		out = append(out, HistoryItem{
			Source:  SourceTracker,
			At:      ch.At,
			Bead:    ch.Bead,
			Actor:   ch.Actor,
			Tracker: &ch,
		})
	}

	// Neueste zuerst. At ist ueberall RFC3339 UTC mit festen Feldbreiten (dafuer
	// sorgt collectors.toUTC auf der git- und der bd-Seite), also ist der
	// String-Vergleich der chronologische. Die Nachsortierung nach Source und
	// Bead ist keine Kosmetik: git-Zeitstempel haben Sekundenaufloesung, ein
	// Commit und der bd-Statuswechsel dazu fallen regelmaessig auf dieselbe
	// Sekunde, und ohne einen zweiten Schluessel tauschen sie zwischen zwei
	// Seitenaufrufen die Plaetze.
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].At != out[j].At {
			return out[i].At > out[j].At
		}
		if out[i].Source != out[j].Source {
			return out[i].Source < out[j].Source
		}
		return out[i].Bead < out[j].Bead
	})

	// Erst nach dem Sortieren beschriften: die Tagesgruppen der Ansicht sind
	// Laeufe gleicher Day-Werte in dieser Reihenfolge, nicht eine eigene
	// Gruppierung, die auseinanderlaufen koennte.
	for i := range out {
		out[i].Day, out[i].DayLabel, out[i].Clock = stampLocal(out[i].At)
	}

	stats := map[string]HistorySource{
		SourceReport:  {Shown: len(reportItems), Total: reportTotal},
		SourceCommit:  {Shown: commitsShown, Total: len(commits), Note: commitNote(commits, commitsScanned)},
		SourceTracker: {Shown: changesShown, Total: changesTotal},
		// Die PR-Spur faellt hier bewusst mit 0 heraus; der Browser ersetzt den
		// Eintrag, sobald GET /github geantwortet hat. Er steht trotzdem schon
		// hier, damit die Ansicht die vierte Spur nicht erst erfinden muss.
		SourcePR: {Note: "wird beim Ansehen aus GET /github geholt"},
	}
	return out, stats
}

// commitNote erklaert eine leere Commit-Spur.
//
// Der haeufige Fall ist nicht "keine Commits", sondern "die Commits nennen
// keine Bead-Id" — dieses Repo selbst war am 2026-09-05 genau so: sieben
// Commits, keine einzige Id, waehrend orbit die Konvention durchgehend
// benutzt. Ohne diesen Hinweis sieht das aus wie ein Defekt der Zuordnung.
func commitNote(commits []collectors.BeadCommit, scanned int) string {
	if len(commits) > 0 {
		return ""
	}
	if scanned == 0 {
		return "kein git log lesbar"
	}
	return "keiner der letzten " + itoa(scanned) +
		" Commits nennt eine Bead-Id — Konvention: Id als Präfix im Subject, z.B. \"gkp-uc7.8: …\""
}

// knownBeadIDs sammelt jede Bead-Id, die in diesem Repo vorkommen kann: die
// Epics, ihre Unteraufgaben in JEDER Tiefe, und die Ids aus dem Audit-Log.
//
// Die Tiefe ist nicht kosmetisch: seit gkp-1lp liefert der Collector einen
// Baum statt zwei Ebenen, und ein Enkel, der hier fehlt, faellt lautlos aus
// der Historie — BeadRefsIn prueft Commit-Texte gegen genau diese Liste, und
// ein Commit, der nur einen Enkel nennt, gehoerte dann zu niemandem.
//
// Der dritte Teil ist der, auf den es ankommt. `bd list -t epic` blendet
// geschlossene Epics aus (gemessen: orbit-37d verschwand in derselben Minute,
// in der es geschlossen wurde), also kennt CollectBeads die Ids der
// abgeschlossenen Arbeit nicht mehr — und genau die stehen in den aelteren
// Commits. Das Audit-Log ist append-only und ueberlebt das Schliessen.
func knownBeadIDs(maps []collectors.BeadsMap, changes []collectors.TrackerChange) []string {
	gesehen := map[string]bool{}
	var out []string
	add := func(id string) {
		if id == "" || gesehen[id] {
			return
		}
		gesehen[id] = true
		out = append(out, id)
	}
	for _, m := range maps {
		// Die Id der Sammelkarte ist erfunden (collectors.LooseMapID) und darf
		// nicht in die Commit-Zuordnung: hier stehen die BEKANNTEN Ids, gegen
		// die Commit-Texte geprueft werden, und ein Wort, das in keinem Tracker
		// steht, kann dort nur falsch treffen. Ihre Kinder sind echt und kommen
		// mit.
		if !m.Loose {
			add(m.ID)
		}
		var absteigen func([]collectors.BeadsSubBead)
		absteigen = func(subs []collectors.BeadsSubBead) {
			for _, c := range subs {
				add(c.ID)
				absteigen(c.Children)
			}
		}
		absteigen(m.Children)
	}
	for _, ch := range changes {
		add(ch.Bead)
	}
	// Sortiert, damit die Zuordnung eines Commits nicht von bds Reihenfolge
	// abhaengt: BeadRefsIn gibt die Treffer in der Reihenfolge dieser Liste
	// zurueck, und die landet so wie sie ist im JSON und in der Ansicht.
	sort.Strings(out)
	return out
}
