package cache

import (
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/niclasedge/git-planner-go/internal/appctx"
)

// TestGetAnsichtenAggregatZweiterAufrufKommtAusDemCache ist das
// Ladeweg-Kriterium aus dem Auftrag: der erste Aufruf ist KALT und blockiert
// auf compute, jeder Aufruf danach innerhalb der TTL liefert denselben Stand
// zurueck, OHNE compute ein zweites Mal aufzurufen — gemessen an der
// tatsaechlichen Aufrufzahl UND an der Wanduhr, nicht behauptet.
func TestGetAnsichtenAggregatZweiterAufrufKommtAusDemCache(t *testing.T) {
	ResetAnsichtenAggregatForTest()
	defer ResetAnsichtenAggregatForTest()

	var mu sync.Mutex
	calls := 0
	compute := func() (map[string]appctx.AufgabenAnsicht, []appctx.ProjektFehler) {
		mu.Lock()
		calls++
		mu.Unlock()
		// Steht fuer die neun seriellen bd-Laeufe aus Spec §5 E6 (dort 14 951 ms) —
		// klein genug, um den Test nicht zu verlangsamen, aber gross genug, dass
		// ein zweiter, tatsaechlich neu rechnender Aufruf messbar laenger braeuchte.
		time.Sleep(50 * time.Millisecond)
		return map[string]appctx.AufgabenAnsicht{}, nil
	}

	start := time.Now()
	GetAnsichtenAggregat(compute)
	kalt := time.Since(start)

	start = time.Now()
	GetAnsichtenAggregat(compute)
	warm := time.Since(start)

	mu.Lock()
	n := calls
	mu.Unlock()
	if n != 1 {
		t.Fatalf("compute lief %d mal, erwartet genau 1 — der zweite Aufruf haette aus dem Cache kommen muessen", n)
	}
	if warm >= kalt {
		t.Errorf("zweiter Aufruf (%v) nicht schneller als der erste (%v) — der Cache greift nicht", warm, kalt)
	}
	t.Logf("kalt: %v, aus dem Cache: %v", kalt, warm)
}

// TestGetAnsichtenAggregatAbgelaufenWirdSofortAusgeliefertUndImHintergrundErneuert
// ist das stale-serve-Kriterium, das GetProjectContext (cache.go) fuer den
// Ein-Projekt-Fall bereits vermisst: ein abgelaufener Stand geht SOFORT raus,
// der Nachlauf passiert im Hintergrund, und der Aufrufer wartet nicht auf ihn.
func TestGetAnsichtenAggregatAbgelaufenWirdSofortAusgeliefertUndImHintergrundErneuert(t *testing.T) {
	ResetAnsichtenAggregatForTest()
	defer ResetAnsichtenAggregatForTest()
	t.Setenv("CC_KNOWLEDGE_TTL_MS", "1")

	fertig := make(chan struct{})
	var mu sync.Mutex
	calls := 0
	compute := func() (map[string]appctx.AufgabenAnsicht, []appctx.ProjektFehler) {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		if n > 1 {
			// Der Hintergrundlauf haengt, bis der Test ihn freigibt — nur so ist
			// beweisbar, dass der ZWEITE GetAnsichtenAggregat-Aufruf NICHT auf ihn
			// gewartet hat.
			<-fertig
		}
		return nil, []appctx.ProjektFehler{{Projekt: "lauf", Grund: strconv.Itoa(n)}}
	}

	erster := GetAnsichtenAggregat(compute)
	time.Sleep(5 * time.Millisecond) // TTL von 1 ms sicher ueberschreiten

	zweiter := GetAnsichtenAggregat(compute)
	if len(zweiter.ProjektFehler) == 0 || zweiter.ProjektFehler[0].Grund != erster.ProjektFehler[0].Grund {
		t.Fatalf("erwartet den ABGELAUFENEN Stand sofort, bekam %+v (erster war %+v)", zweiter, erster)
	}

	close(fertig)
	WaitForAnsichtenAggregatForTest()
	mu.Lock()
	n := calls
	mu.Unlock()
	if n != 2 {
		t.Fatalf("erwartet genau einen Nachlauf im Hintergrund, compute lief %d mal", n)
	}
}

// TestStartAnsichtenAggregatNutztFrischeDateiOhneZuRechnen ist das
// Boot-Primung-Kriterium (gkp-92i, Nachtrag aus gkp-i90/dessen Review, Befund
// 3): ein FRISCHER Stand aus der Datei macht den ersten Aufruf nach einem
// Neustart beantwortbar, OHNE dass compute ueberhaupt laeuft — das ist der
// ganze Sinn der Primung, und ein Test, der nur behauptet "die Datei wurde
// gelesen", ohne zu pruefen, dass compute NICHT lief, waere Fall 4 aus Spec
// §9.1 (die schwaechere Behauptung).
func TestStartAnsichtenAggregatNutztFrischeDateiOhneZuRechnen(t *testing.T) {
	ResetAnsichtenAggregatForTest()
	defer ResetAnsichtenAggregatForTest()
	datei := t.TempDir() + "/ansichten.json"
	t.Setenv("CC_KNOWLEDGE_ANSICHTEN_CACHE", datei)
	t.Setenv("CC_KNOWLEDGE_TTL_MS", "60000")

	vorab := AnsichtenAggregat{
		GeneratedAt: nowMs(),
		Ansichten:   map[string]appctx.AufgabenAnsicht{"heute": {ID: "heute"}},
	}
	WriteAnsichtenCacheFile(&vorab, datei)

	aufgerufen := false
	compute := func() (map[string]appctx.AufgabenAnsicht, []appctx.ProjektFehler) {
		aufgerufen = true
		return nil, nil
	}

	StartAnsichtenAggregat(compute)

	if aufgerufen {
		t.Fatalf("compute lief, obwohl eine frische Datei vorlag — die Primung sollte genau das vermeiden")
	}
	got := GetAnsichtenAggregat(compute)
	if got.Ansichten == nil || got.Ansichten["heute"].ID != "heute" {
		t.Fatalf("Stand nach der Primung = %+v, erwartet den Inhalt der Datei", got)
	}
}

// TestStartAnsichtenAggregatOhneDateiRechnetEinmalUndSchreibtSie ist die
// Gegenprobe zum Test darueber: OHNE brauchbare Datei (fehlt oder abgelaufen)
// muss die Primung compute BLOCKIEREND aufrufen — sonst waere der erste
// echte Request nach einem Neustart weiterhin der kalte, blockierende Pfad
// aus GetAnsichtenAggregat, den diese Funktion gerade vermeiden soll — UND
// den Stand fuer den NAECHSTEN Neustart auf die Platte schreiben.
func TestStartAnsichtenAggregatOhneDateiRechnetEinmalUndSchreibtSie(t *testing.T) {
	ResetAnsichtenAggregatForTest()
	defer ResetAnsichtenAggregatForTest()
	datei := t.TempDir() + "/nicht-vorhanden/ansichten.json"
	t.Setenv("CC_KNOWLEDGE_ANSICHTEN_CACHE", datei)
	t.Setenv("CC_KNOWLEDGE_TTL_MS", "60000")

	var mu sync.Mutex
	calls := 0
	compute := func() (map[string]appctx.AufgabenAnsicht, []appctx.ProjektFehler) {
		mu.Lock()
		calls++
		mu.Unlock()
		return map[string]appctx.AufgabenAnsicht{"heute": {ID: "heute"}}, nil
	}

	StartAnsichtenAggregat(compute)

	mu.Lock()
	n := calls
	mu.Unlock()
	if n != 1 {
		t.Fatalf("compute lief %d mal, erwartet genau 1 — die Primung ohne Datei muss BLOCKIEREND einmal rechnen", n)
	}
	if got := GetAnsichtenAggregat(compute); got.Ansichten["heute"].ID != "heute" {
		t.Fatalf("Stand nach der Primung = %+v, erwartet das Ergebnis von compute", got)
	}
	if ReadAnsichtenCacheFile(datei) == nil {
		t.Fatalf("die Primung hat den frisch gerechneten Stand nicht auf die Platte geschrieben — der naechste Neustart zahlte wieder den vollen Preis")
	}
}
