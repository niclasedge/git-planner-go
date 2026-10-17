package assets_test

// Browser-Test von "Zuletzt erledigt" (zuletzt-erledigt-pr, #116): die Zeile
// zeigt den fertigen Text der Schliesszeit aus Go, die volle Zeit im Tooltip
// und "gemergt mit #N" als Link auf den PR; ohne gemergt_mit steht dort
// nichts. Mechanik wie in naechster_schritt_browser_test.go.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/niclasedge/git-planner-go/internal/assets"
)

const zeArbeit = `{"repo":"acme/alpha","fortschritt":{"erledigt":0,"gesamt":0},
 "zuletzt":[
  {"art":"issue","nummer":116,"titel":"zuletzt-erledigt-pr: Uhrzeit und gemergter PR in Zuletzt erledigt","url":"u116","status":"erledigt",
   "blockiert_durch":[],"labels":[],"zustaendig":[],"hat_text":true,"tiefe":0,"aktiv":false,"branches":[],
   "geschlossen":"2026-09-28T12:32:00Z","geschlossen_text":"heute 14:32","geschlossen_voll":"28.09.2026 14:32",
   "gemergt_mit":{"nummer":117,"titel":"zuletzt-erledigt-pr: Uhrzeit und PR (#116)","url":"https://github.com/acme/alpha/pull/117","gemergt_am":"2026-09-28T12:31:00Z"}},
  {"art":"issue","nummer":112,"titel":"html-dateien: HTML-Dateien unter Geänderte Bilder","url":"u112","status":"erledigt",
   "blockiert_durch":[],"labels":[],"zustaendig":[],"hat_text":true,"tiefe":0,"aktiv":false,"branches":[],
   "geschlossen":"2026-09-27T14:35:01Z","geschlossen_text":"27. Sept.","geschlossen_voll":"27.09.2026 16:35"}
 ],
 "reihe":[],"ohne_issue":[],"prs":[],"quellen":[]}`

const zeTreiber = `
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
  function text(el) { return el ? el.textContent.replace(/\s+/g, ' ').trim() : ''; }
  function zeile(nr) {
    var z = q('[data-eintrag="' + nr + '"]');
    if (!z) return null;
    var d = z.querySelector('.kn-e-datum');
    var links = Array.prototype.map.call(z.querySelectorAll('.kn-e-datum a'), function (a) {
      return { text: text(a), href: a.getAttribute('href') || '' };
    });
    return { datum: text(d), tooltip: d ? (d.getAttribute('title') || '') : '', links: links };
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
      await warte('Repo in der Leiste', function () { return q('[data-repo="acme/alpha"]'); });
      q('[data-repo="acme/alpha"]').click();
      await warte('Zeilen', function () { return document.querySelectorAll('[data-eintrag]').length >= 2; });
      await schlaf(100);
      b.heute = zeile(116);
      b.frueher = zeile(112);
    } catch (e) {
      b.abbruch = String(e && e.message || e);
      b.dom = document.body.innerHTML.slice(0, 3000);
    }
    berichten(b);
  }
  lauf();
})();
`

type zeZeile struct {
	Datum   string `json:"datum"`
	Tooltip string `json:"tooltip"`
	Links   []struct {
		Text string `json:"text"`
		Href string `json:"href"`
	} `json:"links"`
}

func TestZuletztErledigtImBrowser(t *testing.T) {
	if testing.Short() {
		t.Skip("-short: der Test startet einen Browser")
	}
	chrome := chromiumFinden()
	if chrome == "" {
		t.Skip("kein Chromium gefunden — die Oberflaeche ist damit NICHT belegt")
	}
	json200 := func(w http.ResponseWriter, s string) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, s)
	}
	mux := http.NewServeMux()
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
		fmt.Fprint(w, zeTreiber)
	})
	mux.HandleFunc("/api/repos", func(w http.ResponseWriter, r *http.Request) { json200(w, ivRepos) })
	mux.HandleFunc("/api/arbeit", func(w http.ResponseWriter, r *http.Request) { json200(w, zeArbeit) })
	mux.HandleFunc("/api/repo-meta", func(w http.ResponseWriter, r *http.Request) { json200(w, ivMeta) })
	srv := httptest.NewServer(mux)
	defer srv.Close()

	chromium := func(ctx context.Context, extra ...string) ([]byte, error) {
		args := append([]string{
			"--headless", "--no-sandbox", "--disable-gpu", "--no-first-run",
			"--disable-extensions", "--disable-dev-shm-usage", "--hide-scrollbars",
			"--user-data-dir=" + t.TempDir(),
			"--window-size=1400,900",
			"--virtual-time-budget=30000",
		}, extra...)
		return exec.CommandContext(ctx, chrome, append(args, srv.URL+"/")...).Output()
	}

	ctx, abbrechen := context.WithTimeout(context.Background(), 90*time.Second)
	defer abbrechen()
	out, err := chromium(ctx, "--dump-dom")
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
		Heute   *zeZeile `json:"heute"`
		Frueher *zeZeile `json:"frueher"`
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
	if b.Heute == nil || b.Frueher == nil {
		t.Fatalf("Zeilen fehlen: %+v %+v", b.Heute, b.Frueher)
	}
	if b.Heute.Datum != "heute 14:32 · gemergt mit #117" {
		t.Errorf("Zeile #116 %q, erwartet %q", b.Heute.Datum, "heute 14:32 · gemergt mit #117")
	}
	if b.Heute.Tooltip != "28.09.2026 14:32" {
		t.Errorf("Tooltip #116 %q", b.Heute.Tooltip)
	}
	if len(b.Heute.Links) != 1 || b.Heute.Links[0].Text != "#117" || b.Heute.Links[0].Href != "https://github.com/acme/alpha/pull/117" {
		t.Errorf("Link #116 %+v", b.Heute.Links)
	}
	if b.Frueher.Datum != "27. Sept." || len(b.Frueher.Links) != 0 {
		t.Errorf("Zeile #112 %q mit Links %+v, erwartet nur das Datum", b.Frueher.Datum, b.Frueher.Links)
	}

	// Auf Wunsch ein Bildschirmfoto derselben Seite als Beleg.
	if ziel := os.Getenv("GKP_BELEG_PNG"); ziel != "" {
		if out, err := chromium(ctx, "--screenshot="+ziel); err != nil {
			t.Errorf("Bildschirmfoto: %v\n%s", err, out)
		}
	}
}
