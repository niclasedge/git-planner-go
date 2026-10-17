// Package anzeigezeit liefert die Zone, in der die Ansicht Zeiten nennt
// (letzte-aktionen, #126): GKP_ZONE als IANA-Name, ohne Wert Europe/Berlin.
// Die Zeitzonendaten sind eingebaut, damit die Zone auch in einem Image ohne
// tzdata ladbar ist. Gespeicherte Zeiten und Protokolle bleiben in UTC; diese
// Zone gilt nur fuer Texte, "heute" und Tagesgrenzen der Ansicht.
package anzeigezeit

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	// Eingebaute Zeitzonendaten: das Alpine-Image hat keine.
	_ "time/tzdata"
)

// Standard ist die Zone ohne GKP_ZONE und bei einem ungueltigen Namen.
const Standard = "Europe/Berlin"

// Log nimmt die Zeile auf, wenn GKP_ZONE ungueltig ist.
var Log io.Writer = os.Stderr

var (
	mu       sync.Mutex
	geladen  = map[string]*time.Location{}
	standard *time.Location
)

func standardZone() *time.Location {
	if standard == nil {
		z, err := time.LoadLocation(Standard)
		if err != nil {
			// Mit time/tzdata unmoeglich; UTC ist der letzte Halt.
			z = time.UTC
		}
		standard = z
	}
	return standard
}

// ZoneAus laedt die Zone name; leer heisst Standard. Ein ungueltiger Name
// schreibt eine Zeile nach log und liefert Standard.
func ZoneAus(name string, log io.Writer) *time.Location {
	name = strings.TrimSpace(name)
	mu.Lock()
	defer mu.Unlock()
	if name == "" || name == Standard {
		return standardZone()
	}
	z, err := time.LoadLocation(name)
	if err != nil {
		if log != nil {
			fmt.Fprintf(log, "%s anzeigezeit: GKP_ZONE=%q ungueltig (%v), es gilt %s\n",
				time.Now().UTC().Format(time.RFC3339), name, err, Standard)
		}
		return standardZone()
	}
	return z
}

// Zone ist die Anzeigezone aus GKP_ZONE. Sie wird je Name einmal geladen;
// eine ungueltige Angabe meldet Log einmal.
func Zone() *time.Location {
	name := os.Getenv("GKP_ZONE")
	mu.Lock()
	z, ok := geladen[name]
	mu.Unlock()
	if ok {
		return z
	}
	z = ZoneAus(name, Log)
	mu.Lock()
	geladen[name] = z
	mu.Unlock()
	return z
}

// Jetzt ist time.Now() in der Anzeigezone.
func Jetzt() time.Time { return time.Now().In(Zone()) }
