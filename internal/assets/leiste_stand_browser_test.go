package assets_test

// Browser-Test der Leiste mit Stand je Repo und der Farbthemen
// (things-farben-fortschritt, #41): Kreis als Tortenstueck, Zahl offener
// Issues, PR-Symbol; der Themen-Knopf schaltet Hell, Dunkel, Automatisch
// reihum mit den Farben von Things 3. Mechanik wie in
// issue_verwaltung_browser_test.go.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/niclasedge/git-planner-go/internal/assets"
	"github.com/niclasedge/git-planner-go/internal/render"
)

const standJSON = `{"repos":[{"repo":"acme/alpha","offene_issues":3,"offene_prs":2,"erledigte_issues":1,"anteil":0.25}],
 "sync":{"zeit":"2026-09-24T10:00:00Z"}}`

const standTreiber = `
(function () {
  var fehler = [];
  window.addEventListener('error', function (e) { fehler.push(String(e.message)); });
  window.addEventListener('unhandledrejection', function (e) { fehler.push('rejection: ' + String(e.reason)); });
  function schlaf(ms) { return new Promise(function (r) { setTimeout(r, ms); }); }
  async function warte(was, pred) {
    for (var i = 0; i < 200; i++) { try { if (pred()) return; } catch (e) {} await schlaf(25); }
    throw new Error('Zeitueberschreitung: ' + was);
  }
  function q(sel) { return document.querySelector(sel); }
  function farbe(sel) { return getComputedStyle(q(sel)).backgroundColor; }
  function berichten(b) {
    b.fehler = (b.fehler || []).concat(fehler);
    var pre = document.createElement('pre');
    pre.id = 'kn-testbericht';
    pre.textContent = btoa(unescape(encodeURIComponent(JSON.stringify(b))));
    document.body.appendChild(pre);
  }
  async function lauf() {
    var b = {};
    try {
      await warte('Kreis mit Stand', function () { return q('[data-repo="acme/alpha"] [data-anteil="0.25"]'); });
      b.anteil = q('[data-repo="acme/alpha"] [data-anteil]').getAttribute('data-anteil');
      b.stueck = !!q('[data-repo="acme/alpha"] [data-anteil] path');
      b.zahl = q('[data-repo="acme/alpha"] .kn-r-zahl').textContent.trim();
      b.prs = q('[data-repo="acme/alpha"] .kn-r-prs').textContent.trim();

      b.anfang = localStorage.getItem('kn-theme');
      q('.kn-thema').click();
      await schlaf(200);
      function stand() {
        return { wahl: localStorage.getItem('kn-theme'), thema: document.documentElement.dataset.theme, leiste: farbe('.kn-leiste'), mitte: farbe('.kn-mitte'),
                 system: matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light' };
      }
      b.eins = stand();
      q('.kn-thema').click();
      await schlaf(200);
      b.zwei = stand();
      q('.kn-thema').click();
      await schlaf(200);
      b.drei = stand();
    } catch (e) {
      b.abbruch = String(e && e.message || e);
      b.dom = document.body.innerHTML.slice(0, 3000);
    }
    berichten(b);
  }
  lauf();
})();
`

func TestLeisteStandUndThemenImBrowser(t *testing.T) {
	if !strings.Contains(render.ThingsPage(render.Assets{}), "prefers-color-scheme") {
		t.Error("die Huelle setzt data-theme nicht nach prefers-color-scheme, bevor das Bundle laeuft")
	}
	if testing.Short() {
		t.Skip("-short: der Test startet einen Browser")
	}
	chrome := chromiumFinden()
	if chrome == "" {
		t.Skip("kein Chromium gefunden — die Oberflaeche ist damit NICHT belegt")
	}
	api := &fakeAPI{}
	mux := http.NewServeMux()
	protokolliere := func(r *http.Request) {
		rumpf, _ := io.ReadAll(r.Body)
		api.log(anfrage{Methode: r.Method, Pfad: r.URL.Path, Query: r.URL.RawQuery, Rumpf: string(rumpf)})
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
		fmt.Fprint(w, standTreiber)
	})
	mux.HandleFunc("/api/repos", func(w http.ResponseWriter, r *http.Request) { protokolliere(r); json200(w, ivRepos) })
	mux.HandleFunc("/api/repo-stand", func(w http.ResponseWriter, r *http.Request) { protokolliere(r); json200(w, standJSON) })
	mux.HandleFunc("/api/uebersicht", func(w http.ResponseWriter, r *http.Request) {
		protokolliere(r)
		json200(w, `{"heute":[],"demnaechst":[],"irgendwann":[],"zaehler":{"ueberfaellig":0,"heute":0,"demnaechst":0,"irgendwann":0}}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ctx, abbrechen := context.WithTimeout(context.Background(), 90*time.Second)
	defer abbrechen()
	out, err := exec.CommandContext(ctx, chrome,
		"--headless", "--no-sandbox", "--disable-gpu", "--no-first-run",
		"--disable-extensions", "--disable-dev-shm-usage",
		"--user-data-dir="+t.TempDir(),
		"--virtual-time-budget=30000",
		"--dump-dom", srv.URL+"/").Output()
	if err != nil {
		t.Fatalf("chromium (%s): %v\n%s", chrome, err, out)
	}
	m := berichtRe.FindSubmatch(out)
	if m == nil {
		t.Fatalf("kein Testbericht in der Seite. DOM:\n%s", out)
	}
	roh, _ := base64.StdEncoding.DecodeString(string(m[1]))
	type stand struct{ Wahl, Thema, Leiste, Mitte, System string }
	var b struct {
		Abbruch, DOM, Anteil, Zahl, Prs string
		Anfang                          *string
		Stueck                          bool
		Fehler                          []string
		Eins, Zwei, Drei                stand
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
	if b.Anteil != "0.25" || !b.Stueck || b.Zahl != "3" || b.Prs != "2" {
		t.Errorf("Leiste: anteil %q, Tortenstueck %v, Zahl %q, PRs %q; erwartet 0.25, true, 3, 2", b.Anteil, b.Stueck, b.Zahl, b.Prs)
	}
	if b.Anfang != nil {
		t.Errorf("kn-theme vor dem ersten Klick %q, erwartet keine Wahl", *b.Anfang)
	}
	if b.Eins.Wahl != "light" || b.Eins.Thema != "light" || b.Eins.Leiste != "rgb(245, 245, 247)" {
		t.Errorf("nach dem ersten Klick %+v, erwartet light mit Leiste rgb(245, 245, 247)", b.Eins)
	}
	if b.Zwei.Wahl != "dark" || b.Zwei.Thema != "dark" || b.Zwei.Leiste != "rgb(42, 42, 44)" || b.Zwei.Mitte != "rgb(31, 31, 33)" {
		t.Errorf("nach dem zweiten Klick %+v, erwartet dark mit Leiste rgb(42, 42, 44) und Mitte rgb(31, 31, 33)", b.Zwei)
	}
	if b.Drei.Wahl != "auto" || b.Drei.Thema != b.Drei.System {
		t.Errorf("nach dem dritten Klick %+v, erwartet auto und data-theme wie das System", b.Drei)
	}
	if n := len(nur(api.abschnitte()[""], http.MethodGet, "/api/repo-stand")); n < 1 {
		t.Errorf("%d Anfragen an /api/repo-stand", n)
	}
}
