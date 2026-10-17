package assets_test

// Browser-Test der Rollen in der Arbeitsliste (karte-rollen, #102): die Zeile
// zeigt `Wartet auf Mensch` und `Aufräumen` samt dem Grund aus Go. Mechanik
// wie in repo_leiste_browser_test.go (Testseite, Treiber, Bericht);
// chromiumFinden, fakeAPI, testseite und berichtRe stehen dort, ivRepos in
// issue_verwaltung_browser_test.go.
//
// Mit GKP_BELEG_PNG_KARTE_ROLLEN legt der Test ein Bildschirmfoto der Liste ab.

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
	"testing"
	"time"

	"github.com/niclasedge/git-planner-go/internal/assets"
)

const krArbeit = `{"repo":"acme/alpha","fortschritt":{"erledigt":0,"gesamt":0},"zuletzt":[],
 "reihe":[
  {"art":"issue","nummer":21,"titel":"Welche Quelle gilt fuer die Karte?","url":"u21","status":"wartet_auf_mensch","grund":"needs-info",
   "blockiert_durch":[],"labels":[{"name":"needs-info","farbe":"d876e3"}],"zustaendig":[],"hat_text":true,"tiefe":0},
  {"art":"issue","nummer":22,"titel":"Alter Befund ohne Einordnung","url":"u22","status":"hygiene","grund":"Rolle fehlt",
   "blockiert_durch":[],"labels":[{"name":"bug","farbe":"d73a4a"}],"zustaendig":[],"hat_text":true,"tiefe":0},
  {"art":"issue","nummer":23,"titel":"karte-reihe-entsperrt: freie Issues nach entsperrt","url":"u23","status":"bereit",
   "blockiert_durch":[],"labels":[{"name":"ready-for-agent","farbe":"0e8a16"}],"zustaendig":[],"hat_text":true,"tiefe":0}],
 "ohne_issue":[],"prs":[],"quellen":[],"archiv":[]}`

const krTreiber = `
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
  function chip(nr) {
    var el = q('[data-eintrag="' + nr + '"] .kn-e-status');
    return el ? el.textContent.replace(/\s+/g, ' ').trim() : '';
  }
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
      await warte('Leiste', function () { return q('[data-repo="acme/alpha"]'); });
      q('[data-repo="acme/alpha"]').click();
      await warte('Liste', function () { return q('[data-eintrag="23"] .kn-e-status'); });
      await schlaf(300);
      if (new URLSearchParams(location.search).get('foto')) return;
      b.c21 = chip(21);
      b.c22 = chip(22);
      b.c23 = chip(23);
    } catch (e) {
      b.abbruch = String(e && e.message || e);
      b.dom = document.body.innerHTML.slice(0, 3000);
    }
    berichten(b);
  }
  lauf();
})();
`

func TestRollenImBrowser(t *testing.T) {
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
		fmt.Fprint(w, krTreiber)
	})
	mux.HandleFunc("/api/repos", func(w http.ResponseWriter, r *http.Request) { protokolliere(r); json200(w, ivRepos) })
	mux.HandleFunc("/api/arbeit", func(w http.ResponseWriter, r *http.Request) { protokolliere(r); json200(w, krArbeit) })
	mux.HandleFunc("/api/repo-meta", func(w http.ResponseWriter, r *http.Request) { protokolliere(r); json200(w, ivMeta) })
	srv := httptest.NewServer(mux)
	defer srv.Close()

	chromium := func(ctx context.Context, adresse string, extra ...string) ([]byte, error) {
		args := append([]string{
			"--headless", "--no-sandbox", "--disable-gpu", "--no-first-run",
			"--disable-extensions", "--disable-dev-shm-usage", "--hide-scrollbars",
			"--user-data-dir=" + t.TempDir(),
			"--window-size=1400,700",
			"--virtual-time-budget=30000",
		}, extra...)
		return exec.CommandContext(ctx, chrome, append(args, adresse)...).Output()
	}
	ctx, abbrechen := context.WithTimeout(context.Background(), 90*time.Second)
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
		Abbruch, DOM  string
		Fehler        []string
		C21, C22, C23 string
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
	if !strings.Contains(b.C21, "Wartet auf Mensch") || !strings.Contains(b.C21, "needs-info") {
		t.Errorf("#21 zeigt %q, erwartet Wartet auf Mensch und needs-info", b.C21)
	}
	if !strings.Contains(b.C22, "Aufräumen") || !strings.Contains(b.C22, "Rolle fehlt") {
		t.Errorf("#22 zeigt %q, erwartet Aufräumen und Rolle fehlt", b.C22)
	}
	if b.C23 != "Bereit" {
		t.Errorf("#23 zeigt %q, erwartet genau Bereit", b.C23)
	}

	if ziel := os.Getenv("GKP_BELEG_PNG_KARTE_ROLLEN"); ziel != "" {
		if out, err := chromium(ctx, srv.URL+"/?foto=1", "--screenshot="+ziel); err != nil {
			t.Errorf("Bildschirmfoto: %v\n%s", err, out)
		}
	}
}
