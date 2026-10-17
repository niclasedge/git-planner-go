package assets_test

// Browser-Test des Zeitstrahls unter "Letzte Aktionen" (aktionen-24h, #132):
// zugeklappt zehn Zeilen ohne Marken, aufgeklappt alle mit Stunden- und
// Pausenmarken auf der Linie, "weniger" klappt zurueck. Mechanik wie in
// zeitleiste_browser_test.go.
//
// Mit GKP_BELEG_PNG legt der Test ein Bildschirmfoto vom aufgeklappten
// Zeitstrahl ab.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/niclasedge/git-planner-go/internal/assets"
)

func z24Arbeit() string {
	var zeilen []string
	for i := 0; i < 14; i++ {
		stunde, pause := "19:00", ""
		uhr := fmt.Sprintf("19:%02d", 55-i*4)
		switch {
		case i < 2:
			stunde, uhr = "20:00", fmt.Sprintf("20:%02d", 40-i*10)
		case i == 13:
			stunde, uhr, pause = "15:00", "15:12", "3 Std. ohne Aktion"
		}
		wort, art := "Push", "push"
		if i%3 == 1 {
			wort, art = "Merge", "merge"
		}
		zeilen = append(zeilen, fmt.Sprintf(`{"art":%q,"wort":%q,"zeit":"2026-09-30T00:00:00Z","text":"heute %s","voll":"30.09.2026 %s",
		  "titel":"30.09.2026 %s","tag":"Heute","uhr":%q,"stunde":%q,"pause":%q,"juengste":%v,"branch":"claude/x",
		  "sha":"aaaaaa%d","kurz":"aaaaaa%d","betreff":"Schritt %d","betreff_kurz":"Schritt %d","autor":"alex"}`,
			art, wort, uhr, uhr, uhr, uhr, stunde, pause, i == 0, i, i, i, i))
	}
	return `{"repo":"acme/alpha","fortschritt":{"erledigt":0,"gesamt":0},"zuletzt":[],"letzte_aktionen":[` + strings.Join(zeilen, ",") + `],
	 "karte_kopf":{"erledigt":0,"aktiv":0,"naechste":0,"fragen":0,"p1":[],"bereit":0},
	 "reihe":[],"ohne_issue":[],"prs":[],"quellen":[]}`
}

const z24Treiber = `
(function () {
  var fehler = [];
  window.addEventListener('error', function (e) { fehler.push(String(e.message)); });
  function schlaf(ms) { return new Promise(function (r) { setTimeout(r, ms); }); }
  async function warte(was, pred) {
    for (var i = 0; i < 200; i++) { try { if (pred()) return; } catch (e) {} await schlaf(25); }
    throw new Error('Zeitueberschreitung: ' + was);
  }
  function q(sel) { return document.querySelector(sel); }
  function qa(sel) { return Array.prototype.slice.call(document.querySelectorAll(sel)); }
  function text(e) { return e ? e.textContent.replace(/\s+/g, ' ').trim() : ''; }
  function berichten(b) {
    b.fehler = (b.fehler || []).concat(fehler);
    var pre = document.createElement('pre');
    pre.id = 'kn-testbericht';
    pre.textContent = btoa(unescape(encodeURIComponent(JSON.stringify(b))));
    document.body.appendChild(pre);
  }
  function stand() {
    var marken = qa('[data-stunde], [data-pause]').map(function (m) {
      return (m.hasAttribute('data-pause') ? 'pause:' : 'stunde:') + text(m);
    });
    return { zeilen: qa('[data-aktion]').length, marken: marken };
  }
  async function lauf() {
    var b = {};
    try {
      await warte('Leiste', function () { return q('[data-repo="acme/alpha"]'); });
      q('[data-repo="acme/alpha"]').click();
      await warte('Zeitstrom', function () { return qa('[data-aktion]').length >= 10; });
      await schlaf(300);
      var foto = new URLSearchParams(location.search).get('foto');
      b.zu = stand();
      b.mehr = text(q('[data-mehr]'));
      q('[data-mehr]').click();
      await schlaf(200);
      if (foto) {
        var s = q('[data-aktion]').closest('section');
        window.scrollTo(0, Math.max(0, s.getBoundingClientRect().top + window.scrollY - 12));
        return;
      }
      b.auf = stand();
      b.weniger = text(q('[data-weniger]'));
      q('[data-weniger]').click();
      await schlaf(200);
      b.wieder = stand();
    } catch (e) {
      b.abbruch = String(e && e.message || e);
      b.dom = document.body.innerHTML.slice(0, 4000);
    }
    berichten(b);
  }
  lauf();
})();
`

type z24Stand struct {
	Zeilen int
	Marken []string
}

type z24Bericht struct {
	Abbruch, DOM    string
	Fehler          []string
	Zu, Auf, Wieder z24Stand
	Mehr, Weniger   string
}

func TestZeitstrahlBeimAufklappen(t *testing.T) {
	if testing.Short() {
		t.Skip("-short: der Test startet einen Browser")
	}
	chrome := chromiumFinden()
	if chrome == "" {
		t.Skip("kein Chromium gefunden — die Oberflaeche ist damit NICHT belegt")
	}
	arbeit := z24Arbeit()
	if !json.Valid([]byte(arbeit)) {
		t.Fatalf("Fake-Arbeitsliste ist kein JSON:\n%s", arbeit)
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
		fmt.Fprint(w, z24Treiber)
	})
	mux.HandleFunc("/api/repos", func(w http.ResponseWriter, r *http.Request) { json200(w, ivRepos) })
	mux.HandleFunc("/api/arbeit", func(w http.ResponseWriter, r *http.Request) { json200(w, arbeit) })
	mux.HandleFunc("/api/repo-meta", func(w http.ResponseWriter, r *http.Request) { json200(w, ivMeta) })
	mux.HandleFunc("/api/detail", func(w http.ResponseWriter, r *http.Request) { json200(w, ivDetail) })
	srv := httptest.NewServer(mux)
	defer srv.Close()

	chromium := func(ctx context.Context, adresse string, extra ...string) ([]byte, error) {
		args := append([]string{
			"--headless", "--no-sandbox", "--disable-gpu", "--no-first-run",
			"--disable-extensions", "--disable-dev-shm-usage", "--hide-scrollbars",
			"--user-data-dir=" + t.TempDir(),
			"--window-size=1400,900",
			"--virtual-time-budget=30000",
		}, extra...)
		return exec.CommandContext(ctx, chrome, append(args, adresse)...).Output()
	}
	ctx, abbrechen := context.WithTimeout(context.Background(), 150*time.Second)
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
	var b z24Bericht
	if err := json.Unmarshal(roh, &b); err != nil {
		t.Fatalf("Bericht kein JSON: %v", err)
	}
	if b.Abbruch != "" {
		t.Fatalf("Treiber abgebrochen: %s\nFehler: %v\nDOM: %s", b.Abbruch, b.Fehler, b.DOM)
	}
	if len(b.Fehler) > 0 {
		t.Errorf("JavaScript-Fehler: %v", b.Fehler)
	}
	if b.Zu.Zeilen != 10 || len(b.Zu.Marken) != 0 || b.Mehr != "4 weitere aus 24 Std." {
		t.Errorf("zugeklappt %+v mit Knopf %q, erwartet 10 Zeilen ohne Marken und 4 weitere aus 24 Std.", b.Zu, b.Mehr)
	}
	if got := strings.Join(b.Auf.Marken, ","); b.Auf.Zeilen != 14 || got != "stunde:20:00,stunde:19:00,pause:3 Std. ohne Aktion,stunde:15:00" {
		t.Errorf("aufgeklappt %d Zeilen mit Marken %s, erwartet 14 und stunde:20:00,stunde:19:00,pause:3 Std. ohne Aktion,stunde:15:00", b.Auf.Zeilen, got)
	}
	if b.Weniger != "weniger" || b.Wieder.Zeilen != 10 || len(b.Wieder.Marken) != 0 {
		t.Errorf("nach %q %+v, erwartet wieder 10 Zeilen ohne Marken", b.Weniger, b.Wieder)
	}

	if ziel := os.Getenv("GKP_BELEG_PNG"); ziel != "" {
		if out, err := chromium(ctx, srv.URL+"/?foto=1", "--screenshot="+ziel); err != nil {
			t.Errorf("Bildschirmfoto: %v\n%s", err, out)
		}
	}
}
