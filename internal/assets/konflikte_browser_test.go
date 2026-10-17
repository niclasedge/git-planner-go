package assets_test

// Browser-Test des Konfliktzustands (konflikte-anzeigen, #62): Marke je
// PR-Zeile, aufgeklappt voraus/zurueck, bei Konflikt die Dateien, bei veraltet
// der Knopf "Base in den Branch holen", und in der Detailspalte am PR eines
// Branches dieselbe Marke mit den Dateien. Zustand und Dateien kommen fertig
// aus der Fake-API; die Oberflaeche zeichnet nur. Mit GKP_BELEG_PNG=pfad
// legt der Test ein Bildschirmfoto ab.

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

const kaArbeit = `{"repo":"acme/alpha","fortschritt":{"erledigt":0,"gesamt":0},"zuletzt":[],
 "reihe":[
  {"art":"issue","nummer":62,"titel":"konflikte-anzeigen: Konfliktzustand je PR zeigen","url":"u62","status":"ohne_change",
   "blockiert_durch":[],"labels":[],"zustaendig":[],"hat_text":true,"tiefe":0,"aktiv":true,
   "branches":[{"name":"change/einundzwanzig","sha":"abcdef1234567890","kurz_sha":"abcdef1","zeit":"2026-09-25T09:20:00Z","betreff":"Liste",
     "voraus":2,"zurueck":4,"aktiv":true,"gruende":["pr"],
     "pr":{"nummer":21,"titel":"Einundzwanzig","url":"https://github.com/acme/alpha/pull/21","draft":false,"zustand":"konflikt","konflikt_dateien":["b.go","web/src/App.svelte"]}}]}
 ],
 "ohne_issue":[],
 "prs":[
  {"nummer":20,"titel":"Zwanzig liegt hinter main","url":"u20","autor":"cy","labels":[],"draft":false,"head":"change/zwanzig","head_repo":"acme/alpha","head_sha":"s20","base":"main",
   "mergebar":true,"merge_status":"behind","zustand":"veraltet","voraus":1,"zurueck":3},
  {"nummer":21,"titel":"Einundzwanzig mit Konflikt","url":"u21","autor":"cy","labels":[],"draft":false,"head":"change/einundzwanzig","head_repo":"acme/alpha","head_sha":"s21","base":"main",
   "mergebar":false,"merge_status":"dirty","zustand":"konflikt","voraus":2,"zurueck":4,"konflikt_dateien":["b.go","web/src/App.svelte"]},
  {"nummer":22,"titel":"Zweiundzwanzig ist sauber","url":"u22","autor":"cy","labels":[],"draft":false,"head":"change/zweiundzwanzig","head_repo":"acme/alpha","head_sha":"s22","base":"main",
   "mergebar":true,"merge_status":"clean","zustand":"sauber","voraus":1,"zurueck":0}
 ],
 "quellen":[]}`

const kaDetail = `{"art":"issue","nummer":62,"titel":"konflikte-anzeigen: Konfliktzustand je PR zeigen","url":"u62",
 "text":"Teil 1 aus #33.","labels":[],"zustaendig":[],"blockiert_durch":[],"kommentare":[]}`

const kaTreiber = `
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
  function text(sel) { var x = q(sel); return x ? x.textContent.replace(/\s+/g, ' ').trim() : null; }
  function attr(sel, a) { var x = q(sel); return x ? x.getAttribute(a) : null; }
  function liste(sel) { return Array.prototype.map.call(document.querySelectorAll(sel), function (x) { return x.textContent.trim(); }); }
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
      await warte('Liste', function () { return q('[data-eintrag="pr-22"]'); });
      await schlaf(200);
      b.zustand = {};
      b.marke = {};
      [20, 21, 22].forEach(function (n) {
        b.zustand[n] = attr('[data-eintrag="pr-' + n + '"] .kn-e-zustand', 'data-zustand');
        b.marke[n] = text('[data-eintrag="pr-' + n + '"] .kn-e-zustand');
      });

      q('[data-eintrag="pr-20"] .kn-e-titel').click();
      await warte('Knopf Base holen', function () { return q('[data-eintrag="pr-20"] .kn-pr-base'); });
      b.vzZwanzig = text('[data-eintrag="pr-20"] .kn-pr-vz');
      b.knopf = text('[data-eintrag="pr-20"] .kn-pr-base');
      await marke('vor-base');
      q('[data-eintrag="pr-20"] .kn-pr-base').click();
      await schlaf(800);
      await marke('nach-base');

      q('[data-eintrag="pr-21"] .kn-e-titel').click();
      await warte('Konfliktdateien', function () { return q('[data-eintrag="pr-21"] .kn-pr-dateien li'); });
      b.dateien = liste('[data-eintrag="pr-21"] .kn-pr-dateien li');
      b.knopfBeiKonflikt = !!q('[data-eintrag="pr-21"] .kn-pr-base');

      q('[data-eintrag="62"] .kn-e-details').click();
      await warte('Branch-Liste', function () { return q('.kn-d-branches li'); });
      await warte('Detail geladen', function () { var x = q('.kn-detail-inhalt'); return x && !/Lade/.test(x.textContent); });
      await schlaf(200);
      b.detailZustand = attr('.kn-d-branches li .kn-d-zustand', 'data-zustand');
      b.detailMarke = text('.kn-d-branches li .kn-d-zustand');
      b.detailDateien = liste('.kn-d-branches li .kn-d-dateien li');
    } catch (e) {
      b.abbruch = String(e && e.message || e);
      b.dom = document.body.innerHTML.slice(0, 3000);
    }
    berichten(b);
  }
  lauf();
})();
`

func TestKonflikteImBrowser(t *testing.T) {
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
		fmt.Fprint(w, kaTreiber)
	})
	mux.HandleFunc("/__marke", func(w http.ResponseWriter, r *http.Request) {
		api.log(anfrage{Marke: r.URL.Query().Get("name")})
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/api/repos", func(w http.ResponseWriter, r *http.Request) { protokolliere(r); json200(w, ivRepos) })
	mux.HandleFunc("/api/arbeit", func(w http.ResponseWriter, r *http.Request) { protokolliere(r); json200(w, kaArbeit) })
	mux.HandleFunc("/api/repo-meta", func(w http.ResponseWriter, r *http.Request) { protokolliere(r); json200(w, ivMeta) })
	mux.HandleFunc("/api/detail", func(w http.ResponseWriter, r *http.Request) { protokolliere(r); json200(w, kaDetail) })
	mux.HandleFunc("/action/pr/", func(w http.ResponseWriter, r *http.Request) { protokolliere(r); json200(w, `{"ok":true}`) })
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
		Abbruch          string            `json:"abbruch"`
		DOM              string            `json:"dom"`
		Fehler           []string          `json:"fehler"`
		Zustand          map[string]string `json:"zustand"`
		Marke            map[string]string `json:"marke"`
		VzZwanzig        string            `json:"vzZwanzig"`
		Knopf            string            `json:"knopf"`
		Dateien          []string          `json:"dateien"`
		KnopfBeiKonflikt bool              `json:"knopfBeiKonflikt"`
		DetailZustand    string            `json:"detailZustand"`
		DetailMarke      string            `json:"detailMarke"`
		DetailDateien    []string          `json:"detailDateien"`
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
		for n, soll := range map[string][2]string{"20": {"veraltet", "veraltet"}, "21": {"konflikt", "Konflikt"}, "22": {"sauber", "sauber"}} {
			if b.Zustand[n] != soll[0] || b.Marke[n] != soll[1] {
				t.Errorf("#%s: data-zustand %q, Marke %q, erwartet %q/%q", n, b.Zustand[n], b.Marke[n], soll[0], soll[1])
			}
		}
	})
	t.Run("VeraltetMitKnopf", func(t *testing.T) {
		if !strings.Contains(b.VzZwanzig, "↑1") || !strings.Contains(b.VzZwanzig, "↓3") {
			t.Errorf("#20 voraus/zurueck %q, erwartet ↑1 und ↓3", b.VzZwanzig)
		}
		if b.Knopf != "Base in den Branch holen" {
			t.Errorf("Knopf %q", b.Knopf)
		}
		bs := nur(ab["vor-base"], http.MethodPost, "/action/pr/base-holen")
		if len(bs) != 1 || !strings.Contains(bs[0].Rumpf, `"nummer":20`) || !strings.Contains(bs[0].Rumpf, `"repo":"acme/alpha"`) {
			t.Errorf("base-holen %+v, erwartet genau einen Aufruf mit nummer 20", bs)
		}
		if !frischGeladen(ab["vor-base"]) {
			t.Error("nach Base holen kein /api/arbeit mit frisch=1")
		}
	})
	t.Run("KonfliktMitDateien", func(t *testing.T) {
		if strings.Join(b.Dateien, ",") != "b.go,web/src/App.svelte" {
			t.Errorf("#21 Dateien %v", b.Dateien)
		}
		if b.KnopfBeiKonflikt {
			t.Error("#21 mit Konflikt bietet Base holen an")
		}
	})
	t.Run("Detailspalte", func(t *testing.T) {
		if b.DetailZustand != "konflikt" || b.DetailMarke != "Konflikt" {
			t.Errorf("Detailspalte Marke %q/%q, erwartet konflikt/Konflikt", b.DetailZustand, b.DetailMarke)
		}
		if strings.Join(b.DetailDateien, ",") != "b.go,web/src/App.svelte" {
			t.Errorf("Detailspalte Dateien %v", b.DetailDateien)
		}
	})

	// Auf Wunsch ein Bildschirmfoto derselben Seite als Beleg: Liste mit
	// Marken, #21 mit Dateien aufgeklappt, Detailspalte offen.
	if ziel := os.Getenv("GKP_BELEG_PNG"); ziel != "" {
		if out, err := chromium(ctx, "--screenshot="+ziel); err != nil {
			t.Errorf("Bildschirmfoto: %v\n%s", err, out)
		}
	}
}
