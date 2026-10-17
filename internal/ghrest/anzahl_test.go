package ghrest_test

// Zaehlen ueber den Link-Kopf (things-farben-fortschritt, #41): ein Abruf mit
// per_page=1, die Zahl steht in rel="last".

import (
	"context"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

func TestGeschlosseneAnzahl(t *testing.T) {
	ghrest.SpeicherLeeren()
	var unveraendert atomic.Int32
	fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("per_page") != "1" || q.Get("state") != "closed" {
			t.Errorf("Anfrage %s, erwartet state=closed&per_page=1", r.URL)
		}
		etag := `"` + r.URL.Path + `"`
		if r.Header.Get("If-None-Match") == etag {
			unveraendert.Add(1)
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", etag)
		w.Header().Set("Content-Type", "application/json")
		letzte := func(n int) {
			w.Header().Set("Link", fmt.Sprintf(`<http://x%s?state=closed&per_page=1&page=2>; rel="next", <http://x%s?state=closed&per_page=1&page=%d>; rel="last"`, r.URL.Path, r.URL.Path, n))
		}
		switch r.URL.Path {
		case "/repos/acme/alpha/issues":
			letzte(9)
			fmt.Fprint(w, `[{"number":1}]`)
		case "/repos/acme/alpha/pulls":
			letzte(3)
			fmt.Fprint(w, `[{"number":2}]`)
		case "/repos/acme/eins/issues":
			fmt.Fprint(w, `[{"number":1}]`)
		case "/repos/acme/eins/pulls", "/repos/acme/leer/issues", "/repos/acme/leer/pulls":
			fmt.Fprint(w, `[]`)
		default:
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"message":"Not Found"}`)
		}
	})
	c := ghrest.Client{Token: "tok-anzahl-lang"}
	ctx := context.Background()
	for _, f := range []struct {
		repo string
		want int
	}{{"acme/alpha", 6}, {"acme/eins", 1}, {"acme/leer", 0}, {"acme/alpha", 6}} {
		n, err := c.GeschlosseneAnzahl(ctx, f.repo)
		if err != nil || n != f.want {
			t.Errorf("%s: %d, %v; erwartet %d", f.repo, n, err, f.want)
		}
	}
	if unveraendert.Load() != 2 {
		t.Errorf("%d unveraenderte Abrufe, erwartet 2 (zweiter Durchgang fuer acme/alpha aus dem Speicher samt Link-Kopf)", unveraendert.Load())
	}
	if _, err := c.GeschlosseneAnzahl(ctx, "acme/fehlt"); err == nil {
		t.Error("404 ohne Fehler")
	}
}
