// Das Hintergrund-Aggregat der projektuebergreifenden Aufgaben-Ansichten
// (Spec §5 E6, gkp-i90).
//
// Dasselbe Muster wie GetProjectContext weiter unten in cache.go, nur fuer
// GENAU EINEN Schluessel statt einen je Projekt: appctx.AnsichtenAlleProjekte
// laeuft neun bd-Aufrufe hintereinander (gemessen 14 951 ms seriell, Spec §5
// E6) — kein Request darf darauf warten. Ein abgelaufener Stand geht deshalb
// SOFORT raus, die Erneuerung laeuft im Hintergrund; nur ein kalter Start (noch
// nichts im Speicher) blockiert, weil es dann nichts zu zeigen gibt. Exakt die
// Abwaegung, mit der GetProjectContext bereits vermessen ist: 5,25 s -> 0,003 s
// bzw. 26,7 s -> 0,01 s (cache.go:239-242).
//
// Ein EIGENER, kleiner Zustand statt einer Wiederverwendung von
// projectContextCache: dessen Schluessel ist ein Projektpfad, und sein
// reportsFingerprint-Wächter (siehe cache.go) ist an GENAU EIN Projekt
// adressiert. Das Aggregat hier spannt per Definition mehrere, ein
// erfundener Schluessel dafuer waere eine Zweckentfremdung des bestehenden
// Mechanismus statt seiner Wiederverwendung.
package cache

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"github.com/niclasedge/git-planner-go/internal/appctx"
	"github.com/niclasedge/git-planner-go/internal/collectors"
)

// AnsichtenAggregat ist der projektuebergreifende Stand der sechs
// Aufgaben-Ansichten plus sein Alter. GeneratedAt ist der Teil, ohne den ein
// Aggregat eine Behauptung ueber die Gegenwart waere, die es nicht einloesen
// kann (gkp-i90-Auftrag): der Kopf der Ansicht braucht ihn, um zu sagen,
// WELCHER Stand hier steht, nicht nur dass einer da ist.
type AnsichtenAggregat struct {
	GeneratedAt   int64                             `json:"generated_at"` // epoch ms
	Ansichten     map[string]appctx.AufgabenAnsicht `json:"ansichten"`
	ProjektFehler []appctx.ProjektFehler            `json:"projekt_fehler,omitempty"`
}

// AnsichtenComputeFn ist appctx.AnsichtenAlleProjekte, an EINE Stelle
// eingesetzt statt dass cache selbst "jetzt" und den aktiven Repo-Pfad kennen
// muesste — derselbe Beweggrund wie bei GetProjectContext's computeFn: das
// Cache-Paket kennt die Berechnung nicht, nur ihren Takt.
type AnsichtenComputeFn func() (map[string]appctx.AufgabenAnsicht, []appctx.ProjektFehler)

var (
	avMu          sync.Mutex
	avCurrent     *AnsichtenAggregat
	avRefreshing  bool
	avRefreshDone chan struct{}
)

func avIsFresh(a *AnsichtenAggregat, ttl, now int64) bool {
	return a != nil && (now-a.GeneratedAt) < ttl
}

// AnsichtenCacheFile ist der Ablageort der Boot-Primung
// (CC_KNOWLEDGE_ANSICHTEN_CACHE ueberschreibt), das Gegenstueck zu CacheFile()
// fuer diesen zweiten, eigenen Zustand (Kommentarkopf: "ein erfundener
// Schluessel [in projectContextCache] waere eine Zweckentfremdung"). Eine
// EIGENE Datei statt derselben wie CacheFile() aus demselben Grund: die beiden
// Aggregate haben unterschiedliche Formen und unabhaengige TTLs, und eine
// gemeinsame Datei muesste beide Formen gleichzeitig gueltig halten.
func AnsichtenCacheFile() string {
	if env := os.Getenv("CC_KNOWLEDGE_ANSICHTEN_CACHE"); env != "" {
		return env
	}
	return filepath.Join(collectors.Home(), ".cache", "cc-knowledge", "ansichten.json")
}

// ReadAnsichtenCacheFile laedt eine abgelegte Datei, nil bei Fehlen/Unlesbarkeit
// — exakt das Verhalten von ReadCacheFile (cache.go) fuer diesen Zustand.
func ReadAnsichtenCacheFile(path string) *AnsichtenAggregat {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var agg AnsichtenAggregat
	if json.Unmarshal(b, &agg) != nil {
		return nil
	}
	if agg.Ansichten == nil {
		return nil // dieselbe Wache wie ReadCacheFile gegen Array.isArray dort: eine Datei ohne Nutzlast zaehlt nicht als Stand.
	}
	return &agg
}

// WriteAnsichtenCacheFile persistiert das Aggregat (best-effort) — dieselbe
// Form wie WriteCacheFile (cache.go).
func WriteAnsichtenCacheFile(agg *AnsichtenAggregat, path string) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	b, err := json.Marshal(agg)
	if err != nil {
		return
	}
	_ = os.WriteFile(path, b, 0o644)
}

// avStartRefreshLocked registriert einen Nachlauf SYNCHRON — der Aufrufer
// haelt avMu — und startet erst danach die Goroutine, die tatsaechlich
// rechnet. Ist bereits einer unterwegs, liefert es dessen Kanal statt einen
// zweiten zu starten (single-flight, wie startRecomputeLocked in cache.go).
//
// Die Registrierung MUSS synchron passieren und darf nicht erst in der
// Goroutine geschehen: sonst koennte ein Aufrufer von GetAnsichtenAggregat
// bereits mit dem alten Stand zurueckgekehrt sein, WAEHREND avRefreshDone noch
// nil ist — WaitForAnsichtenAggregatForTest faende dann nichts zum Warten und
// ein Test saehe den Nachlauf als "nie passiert", obwohl er nur noch nicht
// angelaufen war (gemessen: genau dieser Fehler beim ersten Entwurf dieser
// Datei, TestGetAnsichtenAggregatAbgelaufenWirdSofortAusgeliefertUndImHinter
// grundErneuert schlug mit "compute lief 1 mal" fehl).
func avStartRefreshLocked(compute AnsichtenComputeFn) chan struct{} {
	if avRefreshing {
		return avRefreshDone
	}
	avRefreshing = true
	avRefreshDone = make(chan struct{})
	ch := avRefreshDone
	go func() {
		ansichten, fehler := compute()
		next := AnsichtenAggregat{GeneratedAt: nowMs(), Ansichten: ansichten, ProjektFehler: fehler}
		avMu.Lock()
		avCurrent = &next
		avRefreshing = false
		avRefreshDone = nil
		avMu.Unlock()
		// Persistiert NACH jedem Lauf, nicht nur beim ersten (wie
		// RefreshAggregate in cache.go): nur so ueberlebt ein Neustart, der
		// Minuten nach dem letzten Nachlauf kommt, mit einem STAND, der noch
		// innerhalb der TTL liegt, statt mit dem Stand von ganz am Anfang.
		WriteAnsichtenCacheFile(&next, AnsichtenCacheFile())
		close(ch)
	}()
	return ch
}

// RefreshAnsichtenAggregat ist ein single-flight Lauf, BLOCKIEREND bis er
// fertig ist — fuer ein kuenftiges Primen beim Dienststart (StartCache-Pendant
// in cache.go), gleichzeitige Aufrufer teilen sich einen Nachlauf statt einen
// je Aufrufer auszuloesen.
func RefreshAnsichtenAggregat(compute AnsichtenComputeFn) {
	avMu.Lock()
	ch := avStartRefreshLocked(compute)
	avMu.Unlock()
	<-ch
}

// StartAnsichtenAggregat primt EINMAL beim Dienststart — das Pendant zu
// StartCache (cache.go:181-193) fuer diesen zweiten Zustand (gkp-92i,
// Nachtrag aus gkp-i90/dessen Review, Befund 3).
//
// Ohne diesen Schritt startet avCurrent bei JEDEM Neustart bei nil, und der
// ERSTE Aufruf von GetAnsichtenAggregat nach dem Start trifft den kalten Pfad
// dort: er blockiert, bis ein voller Sammellauf durch ist (gemessen ~14 951 ms
// mit neun Projekten, Spec §5 E6 — und ohne sammellaufBudget im Extremfall
// weit mehr). Ein frischer Stand aus der Datei macht genau diesen Aufruf
// sofort beantwortbar, wie beim Ein-Projekt-Aggregat.
//
// Ist die Datei fehlend oder abgelaufen, bleibt nur der kalte Pfad: EIN
// blockierender Lauf, hier und nicht im Request — main.go ruft diese Funktion
// aus derselben Hintergrund-Goroutine wie StartCache/PrimeAllProjects auf,
// nicht aus dem Anfragepfad. Ein Aufrufer, der in genau dieses Zeitfenster
// faellt, teilt sich denselben Lauf (single-flight in RefreshAnsichtenAggregat)
// statt einen zweiten auszuloesen — dieselbe Abwaegung, die StartCache fuer
// das Ein-Projekt-Aggregat schon trifft.
func StartAnsichtenAggregat(compute AnsichtenComputeFn) {
	fromFile := ReadAnsichtenCacheFile(AnsichtenCacheFile())
	if avIsFresh(fromFile, TTLMs(), nowMs()) {
		avMu.Lock()
		avCurrent = fromFile
		avMu.Unlock()
		return
	}
	RefreshAnsichtenAggregat(compute)
}

// GetAnsichtenAggregat liefert den aktuellen Stand — stale-serve wie
// GetProjectContext: ein abgelaufener Eintrag wird SOFORT ausgeliefert, die
// Erneuerung laeuft im Hintergrund. Nur ein KALTER Aufruf (noch nichts im
// Speicher) blockiert, weil es dann nichts zu zeigen gibt.
func GetAnsichtenAggregat(compute AnsichtenComputeFn) AnsichtenAggregat {
	avMu.Lock()
	cur := avCurrent
	if cur == nil {
		ch := avStartRefreshLocked(compute)
		avMu.Unlock()
		<-ch
		avMu.Lock()
		defer avMu.Unlock()
		return *avCurrent
	}
	if !avIsFresh(cur, TTLMs(), nowMs()) {
		avStartRefreshLocked(compute) // no-op, wenn schon einer laeuft
	}
	avMu.Unlock()
	return *cur
}

// ResetAnsichtenAggregatForTest leert den Zustand zwischen Tests — sonst
// traegt ein Test den Stand des vorigen weiter, weil das Aggregat
// prozessweit EIN einziger Schluessel ist (anders als projectContextCache,
// das ueber den Projektpfad ohnehin pro Test isoliert ist).
func ResetAnsichtenAggregatForTest() {
	avMu.Lock()
	avCurrent = nil
	avRefreshing = false
	avRefreshDone = nil
	avMu.Unlock()
}

// WaitForAnsichtenAggregatForTest blockiert, bis ein laufender
// Hintergrund-Nachlauf fertig ist — das Pendant zu
// WaitForProjectContextForTest, fuer Tests, die den NEUEN Stand nach einem
// Stale-Serve sehen wollen.
func WaitForAnsichtenAggregatForTest() {
	avMu.Lock()
	ch := avRefreshDone
	avMu.Unlock()
	if ch != nil {
		<-ch
	}
}
