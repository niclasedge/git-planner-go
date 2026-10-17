package assets_test

// Browser-Test der eingerueckten Arbeitsliste (liste-abhaengigkeiten, #48):
// jede Zeile der Reihe rueckt um ihre tiefe ein, der Mittelbereich waechst mit
// dem Fenster, und die Chips rechts ueberdecken den Titel nie. Mechanik wie in
// issue_verwaltung_browser_test.go; gemessen wird mit getBoundingClientRect in
// einem Fenster von 1400 x 900 Pixel.

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

// laLabels sind zwoelf Labels mit langen Namen: zusammen breiter als jede
// Zeile, damit die Chip-Gruppe umbrechen muss.
func laLabels() string {
	teile := []string{}
	for i := 1; i <= 12; i++ {
		teile = append(teile, fmt.Sprintf(`{"name":"bereich:langes-label-%02d","farbe":"0e8a16"}`, i))
	}
	return "[" + strings.Join(teile, ",") + "]"
}

func laArbeit() string {
	return `{"repo":"acme/alpha","fortschritt":{"erledigt":0,"gesamt":0},"zuletzt":[],
 "reihe":[
  {"art":"issue","nummer":4,"titel":"Vier laeuft","url":"u4","status":"in_arbeit","blockiert_durch":[],"labels":[],"zustaendig":[],"hat_text":false,"tiefe":0},
  {"art":"issue","nummer":3,"titel":"Drei baut auf Vier","url":"u3","status":"blockiert","blockiert_durch":[4],"labels":[],"zustaendig":[],"hat_text":false,"tiefe":1,"unter":4},
  {"art":"issue","nummer":2,"titel":"Zwei baut auf Drei","url":"u2","status":"blockiert","blockiert_durch":[3],"labels":[],"zustaendig":[],"hat_text":false,"tiefe":2,"unter":3},
  {"art":"issue","nummer":1,"titel":"Eins mit einem langen Titel, der neben zwoelf Labels lesbar bleiben muss","url":"u1","status":"ohne_change","blockiert_durch":[],
   "labels":` + laLabels() + `,"zustaendig":[],"hat_text":true,"tiefe":0}
 ],
 "ohne_issue":[],"prs":[],"quellen":[]}`
}

const laTreiber = `
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
  function rechteck(sel) {
    var r = q(sel).getBoundingClientRect();
    return { l: r.left, r: r.right, o: r.top, u: r.bottom, b: r.width };
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
      await warte('Zeilen', function () { return document.querySelectorAll('[data-eintrag]').length >= 4; });
      await schlaf(300);
      b.fenster = window.innerWidth;
      b.zeilen = Array.prototype.map.call(document.querySelectorAll('[data-eintrag]'), function (e) { return e.getAttribute('data-eintrag'); });
      b.check = {};
      ['4', '1', '3', '2'].forEach(function (n) { b.check[n] = rechteck('[data-eintrag="' + n + '"] .kn-e-check').l; });
      b.abschnitt = rechteck('.kn-a-abschnitt').b;
      b.titel = rechteck('[data-eintrag="1"] .kn-e-titel');
      b.rechts = rechteck('[data-eintrag="1"] .kn-e-rechts');
    } catch (e) {
      b.abbruch = String(e && e.message || e);
      b.dom = document.body.innerHTML.slice(0, 3000);
    }
    berichten(b);
  }
  lauf();
})();
`

type laRechteck struct {
	L float64 `json:"l"`
	R float64 `json:"r"`
	O float64 `json:"o"`
	U float64 `json:"u"`
	B float64 `json:"b"`
}

func TestListeAbhaengigkeitenImBrowser(t *testing.T) {
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
		fmt.Fprint(w, laTreiber)
	})
	mux.HandleFunc("/api/repos", func(w http.ResponseWriter, r *http.Request) { protokolliere(r); json200(w, ivRepos) })
	mux.HandleFunc("/api/arbeit", func(w http.ResponseWriter, r *http.Request) { protokolliere(r); json200(w, laArbeit()) })
	mux.HandleFunc("/api/repo-meta", func(w http.ResponseWriter, r *http.Request) { protokolliere(r); json200(w, ivMeta) })
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
		Abbruch   string             `json:"abbruch"`
		DOM       string             `json:"dom"`
		Fehler    []string           `json:"fehler"`
		Fenster   float64            `json:"fenster"`
		Zeilen    []string           `json:"zeilen"`
		Check     map[string]float64 `json:"check"`
		Abschnitt float64            `json:"abschnitt"`
		Titel     laRechteck         `json:"titel"`
		Rechts    laRechteck         `json:"rechts"`
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
	if b.Fenster < 1300 {
		t.Fatalf("Fenster %v Pixel breit, erwartet 1400 — --window-size greift nicht", b.Fenster)
	}

	t.Run("Einrueckung", func(t *testing.T) {
		if got := strings.Join(b.Zeilen, ","); got != "4,3,2,1" {
			t.Errorf("Zeilen %s, erwartet 4,3,2,1 (gelieferte Folge)", got)
		}
		if b.Check["1"] != b.Check["4"] {
			t.Errorf("#1 (tiefe 0) steht bei %v, #4 (tiefe 0) bei %v — gleiche Tiefe, gleicher Versatz erwartet", b.Check["1"], b.Check["4"])
		}
		if b.Check["3"] <= b.Check["4"] {
			t.Errorf("#3 (unter #4) steht bei %v, nicht rechts von #4 bei %v", b.Check["3"], b.Check["4"])
		}
		if b.Check["2"] <= b.Check["3"] {
			t.Errorf("#2 (unter #3) steht bei %v, nicht rechts von #3 bei %v", b.Check["2"], b.Check["3"])
		}
	})
	t.Run("Breite", func(t *testing.T) {
		if b.Abschnitt <= 820 {
			t.Errorf("Liste %v Pixel breit bei 1400 Pixel Fenster, erwartet mehr als 820", b.Abschnitt)
		}
	})
	t.Run("TitelNichtVerdeckt", func(t *testing.T) {
		if b.Titel.B < 160 {
			t.Errorf("Titel %v Pixel breit, erwartet mindestens 160", b.Titel.B)
		}
		ueberlapp := b.Titel.L < b.Rechts.R && b.Rechts.L < b.Titel.R && b.Titel.O < b.Rechts.U && b.Rechts.O < b.Titel.U
		if ueberlapp {
			t.Errorf("Titel %+v und Chips %+v ueberschneiden sich", b.Titel, b.Rechts)
		}
	})

	// Auf Wunsch ein Bildschirmfoto derselben Seite als Beleg.
	if ziel := os.Getenv("GKP_BELEG_PNG"); ziel != "" {
		if out, err := chromium(ctx, "--screenshot="+ziel); err != nil {
			t.Errorf("Bildschirmfoto: %v\n%s", err, out)
		}
	}
}
