package server_test

// Die Anzeigezone ueber die Routen (letzte-aktionen, #126): Erledigt-Texte
// und die Zeiten der HTML-Dateien in Europe/Berlin, das Protokoll in UTC,
// auch wenn die lokale Zone des Prozesses Berlin ist, und das Dockerfile
// setzt TZ und GKP_ZONE.

import (
	"bufio"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

func lsBerlin(t *testing.T) *time.Location {
	t.Helper()
	z, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}
	return z
}

func TestErledigtHeuteInBerlin(t *testing.T) {
	f := neuesLSGH(t)
	basis := lsServer(t, f)
	var a lsAntwort
	if st := holeVon(t, basis+"/api/arbeit?repo=acme/alpha", &a); st != http.StatusOK {
		t.Fatalf("Status %d", st)
	}
	zu, _ := time.Parse(time.RFC3339, f.geschlossen)
	want := "heute " + zu.In(lsBerlin(t)).Format("15:04")
	if len(a.Zuletzt) != 1 || a.Zuletzt[0].GeschlossenText != want {
		t.Errorf("zuletzt %+v, erwartet geschlossen_text %q", a.Zuletzt, want)
	}
	var issue bool
	for _, x := range a.LetzteAktionen {
		if x.Art == "issue" {
			issue = true
			if x.Text != want || x.Nummer != 3 {
				t.Errorf("Issue-Zeile %+v, erwartet text %q", x, want)
			}
		}
	}
	if !issue {
		t.Errorf("keine Issue-Zeile in letzte_aktionen: %q", a.arten())
	}
}

func TestProtokollBleibtUTC(t *testing.T) {
	alt := time.Local
	time.Local = lsBerlin(t)
	t.Cleanup(func() { time.Local = alt })
	f := neuesLSGH(t)
	t.Setenv("GKP_SYNC_TTL", "3600")
	basis, log := schreibServer(t, &schreibGH{srv: f.srv})
	if st := holeVon(t, basis+"/api/arbeit?repo=acme/alpha", nil); st != http.StatusOK {
		t.Fatalf("Status %d", st)
	}
	zeilen := 0
	s := bufio.NewScanner(strings.NewReader(log.String()))
	for s.Scan() {
		feld, _, _ := strings.Cut(s.Text(), " ")
		if _, err := time.Parse(time.RFC3339, feld); err != nil {
			continue
		}
		zeilen++
		if !strings.HasSuffix(feld, "Z") {
			t.Errorf("Zeitstempel %q im Protokoll, erwartet UTC mit Z", feld)
		}
	}
	if zeilen == 0 {
		t.Fatalf("keine Zeile mit Zeitstempel im Protokoll:\n%s", log.String())
	}
}

func TestHTMLDateienMitZeittext(t *testing.T) {
	f := neuesLSGH(t)
	basis := lsServer(t, f)
	var h struct {
		Grund   string `json:"grund"`
		Dateien []struct {
			Pfad          string `json:"pfad"`
			Zeit          string `json:"zeit"`
			GeaendertText string `json:"geaendert_text"`
			GeaendertVoll string `json:"geaendert_voll"`
			ErstelltText  string `json:"erstellt_text"`
			ErstelltVoll  string `json:"erstellt_voll"`
		} `json:"dateien"`
	}
	if st := holeVon(t, basis+"/api/html-dateien?repo=acme/alpha", &h); st != http.StatusOK {
		t.Fatalf("Status %d", st)
	}
	if len(h.Dateien) != 1 {
		t.Fatalf("Dateien %+v, Grund %q; erwartet bericht.html", h.Dateien, h.Grund)
	}
	d := h.Dateien[0]
	k1, _ := time.Parse(time.RFC3339, f.k1Zeit)
	lokal := k1.In(lsBerlin(t))
	if d.GeaendertText != "heute "+lokal.Format("15:04") || d.GeaendertVoll != lokal.Format("02.01.2006 15:04") {
		t.Errorf("geaendert %q / %q, erwartet heute %s und %s", d.GeaendertText, d.GeaendertVoll, lokal.Format("15:04"), lokal.Format("02.01.2006 15:04"))
	}
	if d.ErstelltText != d.GeaendertText || d.ErstelltVoll != d.GeaendertVoll {
		t.Errorf("erstellt %q / %q, erwartet wie geaendert (derselbe Commit legt sie an)", d.ErstelltText, d.ErstelltVoll)
	}
}

func TestDockerfileSetztZone(t *testing.T) {
	b, err := os.ReadFile("../../Dockerfile")
	if err != nil {
		t.Fatal(err)
	}
	var da bool
	for _, z := range strings.Split(string(b), "\n") {
		da = da || strings.TrimSpace(z) == "ENV TZ=Europe/Berlin GKP_ZONE=Europe/Berlin"
	}
	if !da {
		t.Errorf("Dockerfile ohne die Zeile ENV TZ=Europe/Berlin GKP_ZONE=Europe/Berlin")
	}
}
