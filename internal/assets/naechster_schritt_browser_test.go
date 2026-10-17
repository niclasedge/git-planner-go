package assets_test

// Browser-Test des naechsten Schritts (naechster-schritt, #52): die
// aufgeklappte Karte und die Detailspalte zeigen Satz und Befehl, der Knopf
// "Kopieren" schreibt genau den Befehl in die Zwischenablage und meldet
// "kopiert"; ein Schritt ohne Befehl zeigt keinen Knopf. Der Schritt kommt
// fertig aus der Fake-API, die Oberflaeche leitet nichts ab. Mechanik wie in
// issue_branches_browser_test.go.

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

const nsArbeit = `{"repo":"acme/alpha","fortschritt":{"erledigt":1,"gesamt":6},"zuletzt":[],
 "reihe":[
  {"art":"issue","nummer":52,"titel":"naechster-schritt: naechster Schritt mit Befehl je Change","url":"u52","status":"in_arbeit",
   "blockiert_durch":[],"labels":[],"zustaendig":[],"hat_text":true,"tiefe":0,"aktiv":false,"branches":[],
   "change":{"kennung":"naechster-schritt","ref":"change/naechster-schritt","archiviert":false,"status":"laeuft","issue":52,
    "aufgaben":[{"text":"Test schreiben","erledigt":true},{"text":"Route bauen","erledigt":false}],"erledigt":1,"gesamt":2,
    "gates":["gate1"],"belege":[],
    "schritt":{"lage":"tun","text":"Der Plan von naechster-schritt steht. Jetzt fehlt der rote Test.","befehl":"just gate2 naechster-schritt"}}},
  {"art":"issue","nummer":53,"titel":"zeitleiste: Aufgaben auf der Zeitleiste","url":"u53","status":"in_arbeit",
   "blockiert_durch":[],"labels":[],"zustaendig":[],"hat_text":false,"tiefe":0,"aktiv":false,"branches":[],
   "change":{"kennung":"zeitleiste","ref":"change/zeitleiste","archiviert":false,"status":"laeuft","issue":53,
    "aufgaben":[{"text":"Leiste zeichnen","erledigt":false}],"erledigt":0,"gesamt":1,"gates":["gate1","gate2"],"belege":[],
    "schritt":{"lage":"tun","text":"Als Nächstes in zeitleiste: Leiste zeichnen"}}}
 ],
 "ohne_issue":[],"prs":[],"quellen":[]}`

const nsDetail = `{"art":"change","kennung":"naechster-schritt","titel":"naechster-schritt","ref":"change/naechster-schritt",
 "proposal":"# naechster-schritt\n\nGeht zurueck auf GitHub-Issue #52.","tasks":"- [x] Test schreiben\n- [ ] Route bauen","specs":[],"belege":[]}`

const nsTreiber = `
(function () {
  var fehler = [];
  var ablage = [];
  window.addEventListener('error', function (e) { fehler.push(String(e.message)); });
  window.addEventListener('unhandledrejection', function (e) { fehler.push('rejection: ' + String(e.reason)); });
  try {
    Object.defineProperty(navigator, 'clipboard', { configurable: true,
      value: { writeText: function (t) { ablage.push(String(t)); return Promise.resolve(); } } });
  } catch (e) { fehler.push('clipboard: ' + e.message); }
  function schlaf(ms) { return new Promise(function (r) { setTimeout(r, ms); }); }
  async function warte(was, pred) {
    for (var i = 0; i < 200; i++) { try { if (pred()) return; } catch (e) {} await schlaf(25); }
    throw new Error('Zeitueberschreitung: ' + was);
  }
  function q(sel) { return document.querySelector(sel); }
  function text(el) { return el ? el.textContent.replace(/\s+/g, ' ').trim() : ''; }
  function schritt(sel) {
    var s = q(sel);
    if (!s) return null;
    var k = s.querySelector('.kn-schritt-kopieren');
    return { lage: s.getAttribute('data-lage') || '', text: text(s.querySelector('.kn-schritt-text')),
             befehl: text(s.querySelector('.kn-schritt-befehl')), knopf: k ? text(k) : null };
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
      var z52 = document.querySelector('[data-eintrag="52"]') ? '[data-eintrag="52"]' : null;
      var z53 = '[data-eintrag="53"]';
      q(z52 + ' .kn-e-titel').click();
      q(z53 + ' .kn-e-titel').click();
      await warte('Karte 52', function () { return q(z52 + ' .kn-e-karte .kn-schritt'); });
      await warte('Karte 53', function () { return q(z53 + ' .kn-e-karte .kn-schritt'); });
      b.karte = schritt(z52 + ' .kn-e-karte .kn-schritt');
      b.ohne = schritt(z53 + ' .kn-e-karte .kn-schritt');
      q(z52 + ' .kn-e-karte .kn-schritt-kopieren').click();
      await warte('kopiert', function () { return /kopiert/.test(text(q(z52 + ' .kn-e-karte .kn-schritt-kopieren'))); });
      b.nachKlick = schritt(z52 + ' .kn-e-karte .kn-schritt');
      b.ablage = ablage.slice();
      q(z52 + ' .kn-e-details').click();
      await warte('Detailspalte', function () { return q('.kn-detail .kn-schritt'); });
      await warte('Detail geladen', function () { var x = q('.kn-detail-inhalt'); return x && !/Lade/.test(x.textContent); });
      await schlaf(200);
      b.detail = schritt('.kn-detail .kn-schritt');
    } catch (e) {
      b.abbruch = String(e && e.message || e);
      b.dom = document.body.innerHTML.slice(0, 3000);
    }
    berichten(b);
  }
  lauf();
})();
`

type nsSchritt struct {
	Lage   string  `json:"lage"`
	Text   string  `json:"text"`
	Befehl string  `json:"befehl"`
	Knopf  *string `json:"knopf"`
}

func TestNaechsterSchrittImBrowser(t *testing.T) {
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
		fmt.Fprint(w, nsTreiber)
	})
	mux.HandleFunc("/api/repos", func(w http.ResponseWriter, r *http.Request) { json200(w, ivRepos) })
	mux.HandleFunc("/api/arbeit", func(w http.ResponseWriter, r *http.Request) { json200(w, nsArbeit) })
	mux.HandleFunc("/api/repo-meta", func(w http.ResponseWriter, r *http.Request) { json200(w, ivMeta) })
	mux.HandleFunc("/api/detail", func(w http.ResponseWriter, r *http.Request) { json200(w, nsDetail) })
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
		Abbruch   string     `json:"abbruch"`
		DOM       string     `json:"dom"`
		Fehler    []string   `json:"fehler"`
		Karte     *nsSchritt `json:"karte"`
		Ohne      *nsSchritt `json:"ohne"`
		NachKlick *nsSchritt `json:"nachKlick"`
		Ablage    []string   `json:"ablage"`
		Detail    *nsSchritt `json:"detail"`
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

	const befehl = "just gate2 naechster-schritt"
	pruefe := func(t *testing.T, wo string, s *nsSchritt) {
		if s == nil {
			t.Fatalf("%s ohne Schritt", wo)
		}
		if s.Lage != "tun" || !strings.Contains(s.Text, "Jetzt fehlt der rote Test") || s.Befehl != befehl {
			t.Errorf("%s: %+v", wo, s)
		}
		if s.Knopf == nil || !strings.Contains(*s.Knopf, "Kopieren") {
			t.Errorf("%s: Knopf %v, erwartet Kopieren", wo, s.Knopf)
		}
	}
	t.Run("Karte", func(t *testing.T) { pruefe(t, "Karte #52", b.Karte) })
	t.Run("Kopieren", func(t *testing.T) {
		if len(b.Ablage) != 1 || b.Ablage[0] != befehl {
			t.Errorf("Zwischenablage %q, erwartet genau %q", b.Ablage, befehl)
		}
		if b.NachKlick == nil || b.NachKlick.Knopf == nil || !strings.Contains(*b.NachKlick.Knopf, "kopiert") {
			t.Errorf("nach dem Klick %+v, erwartet 'kopiert'", b.NachKlick)
		}
	})
	t.Run("OhneBefehlKeinKnopf", func(t *testing.T) {
		if b.Ohne == nil || !strings.Contains(b.Ohne.Text, "Leiste zeichnen") {
			t.Fatalf("Karte #53: %+v", b.Ohne)
		}
		if b.Ohne.Knopf != nil || b.Ohne.Befehl != "" {
			t.Errorf("Karte #53 ohne Befehl zeigt Befehl %q oder Knopf %v", b.Ohne.Befehl, b.Ohne.Knopf)
		}
	})
	t.Run("Detailspalte", func(t *testing.T) { pruefe(t, "Detailspalte #52", b.Detail) })

	// Auf Wunsch ein Bildschirmfoto derselben Seite als Beleg: aufgeklappte
	// Karten und offene Detailspalte mit dem Schritt.
	if ziel := os.Getenv("GKP_BELEG_PNG"); ziel != "" {
		if out, err := chromium(ctx, "--screenshot="+ziel); err != nil {
			t.Errorf("Bildschirmfoto: %v\n%s", err, out)
		}
	}
}
