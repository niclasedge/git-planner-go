package ghrest_test

// Bedingte Abrufe mit ETag, Zaehler und Rate-Limit (sync-status, #38).

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"testing"

	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

func TestZweiterAbrufUnveraendert(t *testing.T) {
	ghrest.SpeicherLeeren()
	var mu sync.Mutex
	var gesehen []string
	fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gesehen = append(gesehen, r.Header.Get("If-None-Match"))
		mu.Unlock()
		w.Header().Set("X-RateLimit-Remaining", "4990")
		w.Header().Set("X-RateLimit-Limit", "5000")
		w.Header().Set("X-RateLimit-Reset", "1790000000")
		if r.Header.Get("If-None-Match") == `"e1"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"e1"`)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[{"number":4,"title":"vier"}]`)
	})
	c := ghrest.Client{Token: "tok-eins-lang"}
	a, err := c.OffeneIssues(context.Background(), "acme/alpha")
	if err != nil {
		t.Fatal(err)
	}
	b, err := c.OffeneIssues(context.Background(), "acme/alpha")
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != 1 || len(b) != 1 || b[0].Nummer != 4 || b[0].Titel != "vier" {
		t.Errorf("Antworten %+v / %+v, erwartet zweimal #4", a, b)
	}
	if len(gesehen) != 2 || gesehen[0] != "" || gesehen[1] != `"e1"` {
		t.Errorf("If-None-Match %q, erwartet leer und dann \"e1\"", gesehen)
	}
	s := ghrest.StatistikLesen()
	if s.Aufrufe != 2 || s.Unveraendert != 1 || s.Fehler != 0 {
		t.Errorf("Zaehler %+v, erwartet 2 Aufrufe, 1 unveraendert", s)
	}
	if s.RateVerbleibend != 4990 || s.RateLimit != 5000 || s.RateReset == "" {
		t.Errorf("Rate-Limit %+v", s)
	}

	// Ein anderer Token fragt dieselbe URL zum ersten Mal: kein If-None-Match.
	if _, err := (ghrest.Client{Token: "tok-zwei-lang"}).OffeneIssues(context.Background(), "acme/alpha"); err != nil {
		t.Fatal(err)
	}
	if gesehen[2] != "" {
		t.Errorf("zweiter Token schickte If-None-Match %q", gesehen[2])
	}
}

func TestSchreibenNieAusDemSpeicher(t *testing.T) {
	ghrest.SpeicherLeeren()
	var inm []string
	fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		inm = append(inm, r.Method+" "+r.Header.Get("If-None-Match"))
		w.Header().Set("ETag", `"e2"`)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"user":{"login":"ich"},"body":"x"}`)
	})
	c := ghrest.Client{Token: "tok-eins-lang"}
	for i := 0; i < 2; i++ {
		if _, err := c.Kommentieren(context.Background(), "acme/alpha", 4, "x"); err != nil {
			t.Fatal(err)
		}
	}
	if len(inm) != 2 || inm[0] != "POST " || inm[1] != "POST " {
		t.Errorf("Schreibaufrufe %q, erwartet zweimal POST ohne If-None-Match", inm)
	}
	if s := ghrest.StatistikLesen(); s.Unveraendert != 0 || s.Aufrufe != 2 {
		t.Errorf("Zaehler %+v", s)
	}
}

func TestFehlerWerdenGezaehlt(t *testing.T) {
	ghrest.SpeicherLeeren()
	fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"message":"nein"}`)
	})
	_, _ = ghrest.Client{Token: "tok-eins-lang"}.Repos(context.Background())
	if s := ghrest.StatistikLesen(); s.Fehler != 1 || s.Aufrufe != 1 {
		t.Errorf("Zaehler %+v, erwartet 1 Aufruf mit Fehler", s)
	}
}
