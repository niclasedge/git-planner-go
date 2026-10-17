package assets_test

// Browser-Test der Reviews je PR (pr-reviews, #81): die Marke je PR-Zeile
// (✓ / ✎ und die Zahl offener Threads), in der Detailspalte Entscheidung,
// Stimmen, angefragte Reviewer mit Entfernen, die Auswahl aus den moeglichen
// Zustaendigen, und die Threads mit Code-Stelle, Verlauf, Antwortfeld,
// „auflösen“ und „wieder öffnen“. Entscheidung, Reihenfolge und Gruppierung
// kommen fertig aus Go (appctx.ReviewsRechnen); die Oberflaeche zeichnet nur.
//
// Mit GKP_BELEG_PNG_PR_REVIEWS legt der Test ein Bildschirmfoto der
// Detailspalte ab.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/niclasedge/git-planner-go/internal/appctx"
	"github.com/niclasedge/git-planner-go/internal/assets"
	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

// rvArbeitJSON baut /api/arbeit mit zwei PRs: #9 mit Aenderungen verlangt,
// zwei offenen Threads und einem aufgeloesten, #10 genehmigt ohne Threads.
// Mit beantwortet traegt Thread 11 die neue Antwort 99.
func rvArbeitJSON(t *testing.T, beantwortet bool) string {
	t.Helper()
	hunk := "@@ -10,4 +10,4 @@ func main() {\n \tx := 1\n-\tfmt.Println(x)\n+\tlog.Println(x)\n }"
	kommentare := []ghrest.ReviewKommentar{
		{ID: 11, Autor: "bo", Text: "Bitte log statt fmt.", Zeit: "2026-09-26T10:01:00Z", Pfad: "main.go", Zeile: 12, Diff: hunk, URL: "u11"},
		{ID: 12, AntwortAuf: 11, Autor: "cy", Text: "Warum?", Zeit: "2026-09-26T10:02:00Z", Pfad: "main.go", Zeile: 12, URL: "u12"},
		{ID: 21, Autor: "bo", Text: "Hier fehlt ein Test.", Zeit: "2026-09-26T10:03:00Z", Pfad: "web/src/App.svelte", Zeile: 40, Diff: "@@ -40 +40 @@\n-alt\n+neu", URL: "u21"},
		{ID: 31, Autor: "ana", Text: "Tippfehler.", Zeit: "2026-09-26T09:00:00Z", Pfad: "README.md", Zeile: 3, Diff: "@@ -3 +3 @@\n-Tipfehler\n+Tippfehler", URL: "u31"},
	}
	if beantwortet {
		kommentare = append(kommentare, ghrest.ReviewKommentar{ID: 99, AntwortAuf: 11, Autor: "cy", Text: "Umgestellt.", Zeit: "2026-09-26T12:00:00Z",
			Pfad: "main.go", Zeile: 12, URL: "u99"})
	}
	zustand := map[int64]ghrest.ThreadZustand{11: {ID: "PRRT_11"}, 21: {ID: "PRRT_21"}, 31: {ID: "PRRT_31", Aufgeloest: true}}
	neun := appctx.ReviewsRechnen([]ghrest.Review{
		{ID: 1, Autor: "ana", Zustand: "APPROVED", Zeit: "2026-09-26T10:00:00Z"},
		{ID: 2, Autor: "bo", Zustand: "CHANGES_REQUESTED", Zeit: "2026-09-26T10:05:00Z"},
	}, []string{"cy"}, nil, kommentare, zustand)
	zehn := appctx.ReviewsRechnen([]ghrest.Review{{ID: 3, Autor: "bo", Zustand: "APPROVED", Zeit: "2026-09-26T10:00:00Z"}},
		nil, nil, nil, map[int64]ghrest.ThreadZustand{})
	pr := func(n int, r ghrest.PRReviews) ghrest.PR {
		return ghrest.PR{Nummer: n, Titel: fmt.Sprintf("PR %d", n), URL: fmt.Sprintf("u%d", n), Autor: "dd", Labels: []ghrest.Label{},
			Head: fmt.Sprintf("change/pr%d", n), HeadRepo: "acme/alpha", HeadSHA: fmt.Sprintf("s%d", n), Base: "main", MergeStatus: "clean",
			Abgleich: "sauber", Reviews: &r}
	}
	b, err := json.Marshal(map[string]any{
		"repo": "acme/alpha", "fortschritt": map[string]int{"erledigt": 0, "gesamt": 0}, "zuletzt": []any{}, "ohne_issue": []any{}, "quellen": []any{},
		"reihe": []any{},
		"prs":   []ghrest.PR{pr(9, neun), pr(10, zehn)},
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

const rvTreiber = `
(function () {
  var fehler = [];
  window.addEventListener('error', function (e) { fehler.push(String(e.message)); });
  window.addEventListener('unhandledrejection', function (e) { fehler.push('rejection: ' + String(e.reason)); });
  function schlaf(ms) { return new Promise(function (r) { setTimeout(r, ms); }); }
  async function warte(was, pred) {
    for (var i = 0; i < 200; i++) { try { if (pred()) return; } catch (e) {} await schlaf(25); }
    throw new Error('Zeitueberschreitung: ' + was);
  }
  function marke(name) { return fetch('/__marke?name=' + encodeURIComponent(name)); }
  function q(sel) { return document.querySelector(sel); }
  function qa(sel) { return Array.prototype.slice.call(document.querySelectorAll(sel)); }
  function text(x) { return x ? x.textContent.replace(/\s+/g, ' ').trim() : null; }
  function berichten(b) {
    b.fehler = (b.fehler || []).concat(fehler);
    var pre = document.createElement('pre');
    pre.id = 'kn-testbericht';
    pre.textContent = btoa(unescape(encodeURIComponent(JSON.stringify(b))));
    document.body.appendChild(pre);
  }
  function eingeben(el, wert) {
    el.value = wert;
    el.dispatchEvent(new Event('input', { bubbles: true }));
    el.dispatchEvent(new Event('change', { bubbles: true }));
  }
  function threads() {
    return qa('.kn-detail .kn-rv-thread').map(function (th) {
      return {
        id: th.getAttribute('data-thread'),
        aufgeloest: th.getAttribute('data-aufgeloest'),
        ort: text(th.querySelector('.kn-rv-ort')),
        diff: th.querySelector('.kn-rv-diff') ? th.querySelector('.kn-rv-diff').textContent : '',
        kommentare: Array.prototype.slice.call(th.querySelectorAll('.kn-rv-kommentar')).map(function (k) {
          return k.getAttribute('data-kommentar') + ':' + text(k.querySelector('.kn-rv-autor')) + ':' + text(k.querySelector('.kn-rv-text'));
        }),
        antwortfeld: !!th.querySelector('textarea.kn-rv-antwort'),
        aufloesen: !!th.querySelector('.kn-rv-aufloesen'),
        oeffnen: !!th.querySelector('.kn-rv-oeffnen'),
      };
    });
  }
  async function oeffnen() {
    await warte('Leiste', function () { return q('[data-repo="acme/alpha"]'); });
    q('[data-repo="acme/alpha"]').click();
    await warte('Liste', function () { return q('[data-eintrag="pr-9"]'); });
    q('[data-eintrag="pr-9"] .kn-e-details').click();
    await warte('Threads', function () { return qa('.kn-detail .kn-rv-thread').length === 3; });
    await warte('Auswahl', function () { return qa('.kn-detail select.kn-rv-auswahl option').length >= 2; });
  }
  async function lauf() {
    var b = {};
    try {
      if (new URLSearchParams(location.search).get('foto')) {
        await oeffnen();
        eingeben(q('.kn-rv-thread[data-thread="11"] textarea.kn-rv-antwort'), 'Ist umgestellt, bitte noch einmal ansehen.');
        await schlaf(100);
        q('.kn-detail .kn-rv').scrollIntoView({ block: 'start' });
        return;
      }
      await oeffnen();
      b.marken = ['pr-9', 'pr-10'].map(function (id) {
        var m = q('[data-eintrag="' + id + '"] .kn-e-reviews');
        var z = q('[data-eintrag="' + id + '"] .kn-e-threads');
        return {
          reviews: m ? m.getAttribute('data-reviews') : '', text: text(m), titel: m ? m.getAttribute('title') : '',
          threads: z ? text(z) : '',
        };
      });
      b.kopf = text(q('.kn-detail .kn-rv-kopf'));
      b.stimmen = qa('.kn-detail .kn-rv-stimme').map(function (s) { return s.getAttribute('data-login') + ':' + s.getAttribute('data-zustand'); });
      b.angefordert = qa('.kn-detail .kn-rv-angefordert [data-reviewer]').map(function (s) { return s.getAttribute('data-reviewer'); });
      b.auswahl = qa('.kn-detail select.kn-rv-auswahl option').map(function (o) { return o.value; }).filter(Boolean);
      b.threads = threads();

      await marke('antworten');
      eingeben(q('.kn-rv-thread[data-thread="11"] textarea.kn-rv-antwort'), 'Umgestellt.');
      await schlaf(50);
      q('.kn-rv-thread[data-thread="11"] .kn-rv-senden').click();
      await warte('neue Antwort', function () { return q('.kn-rv-thread[data-thread="11"] [data-kommentar="99"]'); });
      b.nachAntwort = threads()[0];

      await marke('aufloesen');
      q('.kn-rv-thread[data-thread="21"] .kn-rv-aufloesen').click();
      await schlaf(400);
      await marke('oeffnen');
      q('.kn-rv-thread[data-thread="31"] .kn-rv-oeffnen').click();
      await schlaf(400);

      await marke('anfordern');
      eingeben(q('.kn-detail select.kn-rv-auswahl'), 'ana');
      await schlaf(50);
      q('.kn-detail .kn-rv-anfordern').click();
      await schlaf(400);
      await marke('entfernen');
      q('.kn-detail .kn-rv-angefordert [data-reviewer="cy"] .kn-rv-entfernen').click();
      await schlaf(400);
      await marke('ende');
    } catch (e) {
      b.abbruch = String(e && e.message || e);
      b.dom = document.body.innerHTML.slice(0, 4000);
    }
    berichten(b);
  }
  lauf();
})();
`

type rvThreadBericht struct {
	ID          string   `json:"id"`
	Aufgeloest  string   `json:"aufgeloest"`
	Ort         string   `json:"ort"`
	Diff        string   `json:"diff"`
	Kommentare  []string `json:"kommentare"`
	Antwortfeld bool     `json:"antwortfeld"`
	Aufloesen   bool     `json:"aufloesen"`
	Oeffnen     bool     `json:"oeffnen"`
}

func TestPRReviewsImBrowser(t *testing.T) {
	if testing.Short() {
		t.Skip("-short: der Test startet einen Browser")
	}
	chrome := chromiumFinden()
	if chrome == "" {
		t.Skip("kein Chromium gefunden — die Oberflaeche ist damit NICHT belegt")
	}
	vorher, nachher := rvArbeitJSON(t, false), rvArbeitJSON(t, true)
	var beantwortet atomic.Bool
	api := &fakeAPI{}
	mux := http.NewServeMux()
	protokolliere := func(r *http.Request) string {
		rumpf, _ := io.ReadAll(r.Body)
		api.log(anfrage{Methode: r.Method, Pfad: r.URL.Path, Query: r.URL.RawQuery, Rumpf: string(rumpf)})
		return string(rumpf)
	}
	json200 := func(w http.ResponseWriter, s string) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, s)
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, testseite)
	})
	mux.HandleFunc("/app.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		fmt.Fprint(w, assets.AppJS)
	})
	mux.HandleFunc("/treiber.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		fmt.Fprint(w, rvTreiber)
	})
	mux.HandleFunc("/__marke", func(w http.ResponseWriter, r *http.Request) {
		api.log(anfrage{Marke: r.URL.Query().Get("name")})
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/api/repos", func(w http.ResponseWriter, r *http.Request) { protokolliere(r); json200(w, ivRepos) })
	mux.HandleFunc("/api/arbeit", func(w http.ResponseWriter, r *http.Request) {
		protokolliere(r)
		if beantwortet.Load() {
			json200(w, nachher)
			return
		}
		json200(w, vorher)
	})
	mux.HandleFunc("/api/repo-meta", func(w http.ResponseWriter, r *http.Request) { protokolliere(r); json200(w, ivMeta) })
	mux.HandleFunc("/api/detail", func(w http.ResponseWriter, r *http.Request) {
		protokolliere(r)
		json200(w, fmt.Sprintf(`{"art":"issue","nummer":%s,"titel":"PR","text":"Text"}`, r.URL.Query().Get("nummer")))
	})
	mux.HandleFunc("/action/pr/", func(w http.ResponseWriter, r *http.Request) {
		protokolliere(r)
		if r.URL.Path == "/action/pr/antworten" {
			beantwortet.Store(true)
			json200(w, `{"ok":true,"kommentar":{"id":99,"autor":"cy","text":"Umgestellt."}}`)
			return
		}
		json200(w, `{"ok":true}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	chromium := func(ctx context.Context, adresse string, extra ...string) ([]byte, error) {
		args := append([]string{
			"--headless", "--no-sandbox", "--disable-gpu", "--no-first-run",
			"--disable-extensions", "--disable-dev-shm-usage", "--hide-scrollbars",
			"--user-data-dir=" + t.TempDir(),
			"--window-size=1400,1000",
			"--virtual-time-budget=30000",
		}, extra...)
		return exec.CommandContext(ctx, chrome, append(args, adresse)...).Output()
	}

	ctx, abbrechen := context.WithTimeout(context.Background(), 180*time.Second)
	defer abbrechen()
	out, err := chromium(ctx, srv.URL+"/", "--dump-dom")
	if err != nil {
		t.Fatalf("chromium (%s): %v\n%s", chrome, err, out)
	}
	m := berichtRe.FindSubmatch(out)
	if m == nil {
		t.Fatalf("kein Testbericht in der Seite. DOM:\n%s", out)
	}
	roh, _ := base64.StdEncoding.DecodeString(string(m[1]))
	var b struct {
		Abbruch string   `json:"abbruch"`
		DOM     string   `json:"dom"`
		Fehler  []string `json:"fehler"`
		Marken  []struct {
			Reviews string `json:"reviews"`
			Text    string `json:"text"`
			Titel   string `json:"titel"`
			Threads string `json:"threads"`
		} `json:"marken"`
		Kopf        string            `json:"kopf"`
		Stimmen     []string          `json:"stimmen"`
		Angefordert []string          `json:"angefordert"`
		Auswahl     []string          `json:"auswahl"`
		Threads     []rvThreadBericht `json:"threads"`
		NachAntwort rvThreadBericht   `json:"nachAntwort"`
	}
	if err := json.Unmarshal(roh, &b); err != nil {
		t.Fatalf("Bericht kein JSON: %v", err)
	}
	if b.Abbruch != "" {
		t.Fatalf("Treiber abgebrochen: %s\nFehler: %v\nDOM: %s", b.Abbruch, b.Fehler, b.DOM)
	}
	if len(b.Fehler) > 0 {
		t.Errorf("JavaScript-Fehler: %v", b.Fehler)
	}
	ab := api.abschnitte()

	t.Run("Marken", func(t *testing.T) {
		if len(b.Marken) != 2 {
			t.Fatalf("Marken %+v", b.Marken)
		}
		if n := b.Marken[0]; n.Reviews != "aenderungen" || n.Text != "✎" || n.Threads != "2" ||
			!strings.Contains(n.Titel, "Änderungen verlangt von bo") || !strings.Contains(n.Titel, "2 offene Threads") {
			t.Errorf("#9 Marke %+v", n)
		}
		if z := b.Marken[1]; z.Reviews != "genehmigt" || z.Text != "✓" || z.Threads != "" {
			t.Errorf("#10 Marke %+v", z)
		}
	})
	t.Run("Detailspalte", func(t *testing.T) {
		if !strings.Contains(b.Kopf, "Änderungen verlangt") {
			t.Errorf("Kopf %q", b.Kopf)
		}
		if strings.Join(b.Stimmen, ",") != "ana:genehmigt,bo:aenderungen" {
			t.Errorf("Stimmen %v", b.Stimmen)
		}
		if strings.Join(b.Angefordert, ",") != "cy" {
			t.Errorf("angefordert %v", b.Angefordert)
		}
		if strings.Join(b.Auswahl, ",") != "ana,bo" {
			t.Errorf("Auswahl %v, erwartet die moeglichen Zustaendigen ana,bo", b.Auswahl)
		}
	})
	t.Run("Threads", func(t *testing.T) {
		if len(b.Threads) != 3 {
			t.Fatalf("Threads %+v", b.Threads)
		}
		eins, zwei, drei := b.Threads[0], b.Threads[1], b.Threads[2]
		if eins.ID != "11" || eins.Aufgeloest != "nein" || eins.Ort != "main.go:12" || !strings.Contains(eins.Diff, "+\tlog.Println(x)") {
			t.Errorf("Thread 11 %+v", eins)
		}
		if strings.Join(eins.Kommentare, "|") != "11:bo:Bitte log statt fmt.|12:cy:Warum?" {
			t.Errorf("Thread 11 Verlauf %v", eins.Kommentare)
		}
		if zwei.ID != "21" || zwei.Ort != "web/src/App.svelte:40" || zwei.Aufgeloest != "nein" {
			t.Errorf("Thread 21 %+v", zwei)
		}
		for _, o := range []rvThreadBericht{eins, zwei} {
			if !o.Antwortfeld || !o.Aufloesen || o.Oeffnen {
				t.Errorf("offener Thread %s: Antwortfeld %v auflösen %v öffnen %v", o.ID, o.Antwortfeld, o.Aufloesen, o.Oeffnen)
			}
		}
		if drei.ID != "31" || drei.Aufgeloest != "ja" || drei.Antwortfeld || drei.Aufloesen || !drei.Oeffnen {
			t.Errorf("aufgeloester Thread %+v", drei)
		}
	})
	rumpfVon := func(t *testing.T, abschnitt, pfad string) map[string]any {
		t.Helper()
		as := nur(ab[abschnitt], http.MethodPost, pfad)
		if len(as) != 1 {
			t.Fatalf("%s: %s %+v, erwartet genau einen Aufruf", abschnitt, pfad, as)
		}
		var r map[string]any
		if err := json.Unmarshal([]byte(as[0].Rumpf), &r); err != nil {
			t.Fatalf("Rumpf %s: %v", as[0].Rumpf, err)
		}
		if r["repo"] != "acme/alpha" || fmt.Sprint(r["nummer"]) != "9" {
			t.Errorf("%s: repo/nummer %v", pfad, r)
		}
		return r
	}
	t.Run("Antworten", func(t *testing.T) {
		r := rumpfVon(t, "antworten", "/action/pr/antworten")
		if fmt.Sprint(r["kommentar_id"]) != "11" || r["text"] != "Umgestellt." {
			t.Errorf("Rumpf %v", r)
		}
		if !frischGeladen(ab["antworten"]) {
			t.Error("nach der Antwort kein /api/arbeit mit frisch=1")
		}
		if k := b.NachAntwort.Kommentare; len(k) != 3 || k[2] != "99:cy:Umgestellt." {
			t.Errorf("nach dem Neuladen %v", k)
		}
	})
	t.Run("Aufloesen", func(t *testing.T) {
		r := rumpfVon(t, "aufloesen", "/action/pr/thread")
		if fmt.Sprint(r["kommentar_id"]) != "21" || r["aufgeloest"] != true {
			t.Errorf("aufloesen %v", r)
		}
		r = rumpfVon(t, "oeffnen", "/action/pr/thread")
		if fmt.Sprint(r["kommentar_id"]) != "31" || r["aufgeloest"] != false {
			t.Errorf("wieder oeffnen %v", r)
		}
	})
	leer := func(v any) bool {
		l, ok := v.([]any)
		return v == nil || ok && len(l) == 0
	}
	t.Run("Reviewer", func(t *testing.T) {
		r := rumpfVon(t, "anfordern", "/action/pr/reviewer")
		if fmt.Sprint(r["hinzufuegen"]) != "[ana]" || !leer(r["entfernen"]) {
			t.Errorf("anfordern %v", r)
		}
		r = rumpfVon(t, "entfernen", "/action/pr/reviewer")
		if fmt.Sprint(r["entfernen"]) != "[cy]" || !leer(r["hinzufuegen"]) {
			t.Errorf("entfernen %v", r)
		}
	})

	if ziel := os.Getenv("GKP_BELEG_PNG_PR_REVIEWS"); ziel != "" {
		beantwortet.Store(false)
		if out, err := chromium(ctx, srv.URL+"/?foto=1", "--screenshot="+ziel); err != nil {
			t.Errorf("Bildschirmfoto: %v\n%s", err, out)
		}
	}
}
