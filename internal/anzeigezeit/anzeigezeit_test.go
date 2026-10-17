package anzeigezeit_test

// Die Anzeigezone (letzte-aktionen, #126): GKP_ZONE, sonst Europe/Berlin,
// mit eingebauten Zeitzonendaten; ein ungueltiger Name faellt mit Logzeile
// zurueck. ZeitpunktText rechnet "heute" und die Tagesgrenze in dieser Zone.

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/niclasedge/git-planner-go/internal/anzeigezeit"
	"github.com/niclasedge/git-planner-go/internal/appctx"
)

func TestStandardIstBerlin(t *testing.T) {
	t.Setenv("GKP_ZONE", "")
	if z := anzeigezeit.Zone(); z.String() != "Europe/Berlin" {
		t.Fatalf("Zone %q, erwartet Europe/Berlin", z)
	}
	if j := anzeigezeit.Jetzt(); j.Location().String() != "Europe/Berlin" {
		t.Errorf("Jetzt in %q, erwartet Europe/Berlin", j.Location())
	}
	if d := time.Since(anzeigezeit.Jetzt()); d < 0 || d > time.Minute {
		t.Errorf("Jetzt weicht um %v von time.Now ab", d)
	}
}

func TestMergeZeitInBerlin(t *testing.T) {
	t.Setenv("GKP_ZONE", "")
	jetzt := time.Date(2026, 9, 30, 20, 59, 0, 0, time.UTC).In(anzeigezeit.Zone())
	text, voll := appctx.ZeitpunktText("2026-09-30T18:58:11Z", jetzt)
	if text != "heute 20:58" || voll != "30.09.2026 20:58" {
		t.Errorf("ZeitpunktText = %q, %q; erwartet heute 20:58 und 30.09.2026 20:58", text, voll)
	}
}

func TestTagesgrenzeInBerlin(t *testing.T) {
	t.Setenv("GKP_ZONE", "Europe/Berlin")
	z := anzeigezeit.Zone()
	jetzt := time.Date(2026, 10, 1, 8, 0, 0, 0, z)
	if text, _ := appctx.ZeitpunktText("2026-09-30T22:30:00Z", jetzt); text != "heute 00:30" {
		t.Errorf("ZeitpunktText = %q, erwartet heute 00:30", text)
	}
	// Und die Gegenprobe: 21:55Z ist 23:55 am Vortag.
	if text, _ := appctx.ZeitpunktText("2026-09-30T21:55:00Z", jetzt); text != "30. Sept." {
		t.Errorf("ZeitpunktText Vortag = %q, erwartet 30. Sept.", text)
	}
}

func TestUngueltigeZoneFaelltZurueck(t *testing.T) {
	var log bytes.Buffer
	z := anzeigezeit.ZoneAus("Mars/Olympus", &log)
	if z.String() != "Europe/Berlin" {
		t.Errorf("Zone %q, erwartet Europe/Berlin", z)
	}
	zeile := log.String()
	for _, teil := range []string{"GKP_ZONE", "Mars/Olympus", "Europe/Berlin"} {
		if !strings.Contains(zeile, teil) {
			t.Errorf("Logzeile %q ohne %q", zeile, teil)
		}
	}
	t.Setenv("GKP_ZONE", "Mars/Olympus")
	if z := anzeigezeit.Zone(); z.String() != "Europe/Berlin" {
		t.Errorf("Zone ueber GKP_ZONE %q, erwartet Europe/Berlin", z)
	}
}

func TestAndereGueltigeZone(t *testing.T) {
	var log bytes.Buffer
	if z := anzeigezeit.ZoneAus("America/New_York", &log); z.String() != "America/New_York" {
		t.Errorf("Zone %q, erwartet America/New_York", z)
	}
	if log.Len() != 0 {
		t.Errorf("Logzeile bei gueltiger Zone: %q", log.String())
	}
	t.Setenv("GKP_ZONE", "America/New_York")
	if z := anzeigezeit.Zone(); z.String() != "America/New_York" {
		t.Errorf("Zone ueber GKP_ZONE %q, erwartet America/New_York", z)
	}
}
