// Der Takt der Leiste (projektliste-api, #11).
//
// Dasselbe Muster wie ansichtenaggregat.go: EIN Schluessel, stale-serve. Die
// Leiste zaehlt ueber alle Projekte und alle Quellen; das gehoert nicht in
// den Anfragepfad. Ein abgelaufener Stand geht sofort raus und wird im
// Hintergrund erneuert, nur ohne jeden Stand wartet der Aufrufer.
//
// Keine Datei wie beim Ansichten-Aggregat: der kalte Aufruf nach einem
// Neustart rechnet einmal. Wer das aendern will, zieht die Boot-Primung aus
// StartAnsichtenAggregat nach.
package cache

import (
	"sync"
	"time"

	"github.com/niclasedge/git-planner-go/internal/anzeigezeit"
	"github.com/niclasedge/git-planner-go/internal/appctx"
)

// LeisteComputeFn rechnet die Leiste zum Zeitpunkt jetzt; jetzt wird zum
// stand der Antwort und zum Alter des Stands.
type LeisteComputeFn func(jetzt time.Time) appctx.Leiste

type leistenStand struct {
	generatedAt int64
	leiste      appctx.Leiste
}

var (
	lsMu          sync.Mutex
	lsCurrent     *leistenStand
	lsRefreshing  bool
	lsRefreshDone chan struct{}
)

// lsStartRefreshLocked registriert den Nachlauf synchron (Aufrufer haelt
// lsMu) und startet ihn dann; laeuft schon einer, gibt es dessen Kanal
// zurueck (single-flight, wie avStartRefreshLocked).
func lsStartRefreshLocked(compute LeisteComputeFn) chan struct{} {
	if lsRefreshing {
		return lsRefreshDone
	}
	lsRefreshing = true
	lsRefreshDone = make(chan struct{})
	ch := lsRefreshDone
	go func() {
		jetzt := anzeigezeit.Jetzt()
		l := compute(jetzt)
		next := leistenStand{generatedAt: jetzt.UnixMilli(), leiste: l}
		lsMu.Lock()
		lsCurrent = &next
		lsRefreshing = false
		lsRefreshDone = nil
		lsMu.Unlock()
		close(ch)
	}()
	return ch
}

// GetLeiste liefert den aktuellen Stand der Leiste, stale-serve.
func GetLeiste(compute LeisteComputeFn) appctx.Leiste {
	lsMu.Lock()
	cur := lsCurrent
	if cur == nil {
		ch := lsStartRefreshLocked(compute)
		lsMu.Unlock()
		<-ch
		lsMu.Lock()
		defer lsMu.Unlock()
		return lsCurrent.leiste
	}
	if nowMs()-cur.generatedAt >= TTLMs() {
		lsStartRefreshLocked(compute) // no-op, wenn schon einer laeuft
	}
	lsMu.Unlock()
	return cur.leiste
}

// ResetLeisteForTest leert den Stand zwischen Tests.
func ResetLeisteForTest() {
	lsMu.Lock()
	lsCurrent = nil
	lsRefreshing = false
	lsRefreshDone = nil
	lsMu.Unlock()
}

// WaitForLeisteForTest wartet auf einen laufenden Nachlauf.
func WaitForLeisteForTest() {
	lsMu.Lock()
	ch := lsRefreshDone
	lsMu.Unlock()
	if ch != nil {
		<-ch
	}
}
