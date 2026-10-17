package assets_test

// Browser-Test der Branch-Marke und der Branch-Liste (issue-branches, #50):
// ein Issue mit aktivem Branch zeigt die gruene Marke mit Anzahl und "aktiv",
// ein Issue ohne Branch keine Marke, die Detailspalte listet die Branches mit
// Kurz-sha, voraus/zurueck und PR-Link, und die Marke ueberdeckt den Titel
// nie. Mechanik wie in liste_abhaengigkeiten_browser_test.go; Zuordnung und
// Flag kommen fertig aus der Fake-API, die Oberflaeche zeichnet nur.

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

func ibArbeit() string {
	return `{"repo":"acme/alpha","fortschritt":{"erledigt":0,"gesamt":0},"zuletzt":[],
 "reihe":[
  {"art":"issue","nummer":50,"titel":"issue-branches: verknuepfte Branches und aktive Arbeit je Issue","url":"u50","status":"ohne_change",
   "blockiert_durch":[],"labels":` + laLabels() + `,"zustaendig":[],"hat_text":true,"tiefe":0,"aktiv":true,
   "branches":[
    {"name":"claude/50-issue-branches","sha":"abcdef1234567890","kurz_sha":"abcdef1","zeit":"2026-09-25T09:20:00Z","betreff":"Marke zeichnen",
     "voraus":6,"zurueck":0,"aktiv":true,"gruende":["name","pr"],
     "pr":{"nummer":60,"titel":"issue-branches (#50)","url":"https://github.com/acme/alpha/pull/60","draft":false}},
    {"name":"fix-50","sha":"0123456789abcdef","kurz_sha":"0123456","zeit":"2026-09-16T08:00:00Z","betreff":"Erster Entwurf",
     "voraus":1,"zurueck":23,"aktiv":false,"gruende":["name"]}
   ]},
  {"art":"issue","nummer":51,"titel":"Kreis in blocked_by zeigt Eintraege doppelt","url":"u51","status":"ohne_change","blockiert_durch":[],
   "labels":[],"zustaendig":[],"hat_text":false,"tiefe":0,"aktiv":false,"branches":[]},
  {"art":"issue","nummer":52,"titel":"Archivliste mit Vorschau","url":"u52","status":"ohne_change","blockiert_durch":[],
   "labels":[],"zustaendig":[],"hat_text":false,"tiefe":0,"aktiv":false,
   "branches":[{"name":"claude/52-archiv","sha":"fedcba9876543210","kurz_sha":"fedcba9","zeit":"2026-09-10T08:00:00Z","betreff":"Liste",
     "voraus":2,"zurueck":9,"aktiv":false,"gruende":["name"]}]}
 ],
 "ohne_issue":[],"prs":[],"quellen":[]}`
}

const ibDetail = `{"art":"issue","nummer":50,"titel":"issue-branches: verknuepfte Branches und aktive Arbeit je Issue","url":"u50",
 "text":"Go ordnet jedem offenen Issue seine Branches zu.","labels":[],"zustaendig":[],"blockiert_durch":[],"kommentare":[]}`

const ibTreiber = `
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
  function marke(n) {
    var m = q('[data-eintrag="' + n + '"] .kn-e-branches');
    if (!m) return null;
    return { text: m.textContent.replace(/\s+/g, ' ').trim(), aktiv: m.classList.contains('kn-e-branches-aktiv'),
             anzahl: m.getAttribute('data-branches'), farbe: getComputedStyle(m).color,
             gruen: getComputedStyle(document.querySelector('.kn-app') || document.body).getPropertyValue('--t-gruen').trim() };
  }
  async function lauf() {
    var b = {};
    try {
      await warte('Repo in der Leiste', function () { return q('[data-repo="acme/alpha"]'); });
      q('[data-repo="acme/alpha"]').click();
      await warte('Zeilen', function () { return document.querySelectorAll('[data-eintrag]').length >= 3; });
      await schlaf(300);
      b.fenster = window.innerWidth;
      b.marke = { '50': marke('50'), '51': marke('51'), '52': marke('52') };
      b.titel = rechteck('[data-eintrag="50"] .kn-e-titel');
      b.markeRechteck = rechteck('[data-eintrag="50"] .kn-e-branches');
      q('[data-eintrag="50"] .kn-e-details').click();
      await warte('Branch-Liste', function () { return document.querySelectorAll('.kn-d-branches li').length >= 2; });
      await warte('Detail geladen', function () { var x = q('.kn-detail-inhalt'); return x && !/Lade/.test(x.textContent); });
      await schlaf(200);
      b.liste = Array.prototype.map.call(document.querySelectorAll('.kn-d-branches li'), function (li) {
        var a = li.querySelector('a.kn-d-link');
        return { name: li.getAttribute('data-branch'), aktiv: li.getAttribute('data-aktiv'),
                 text: li.textContent.replace(/\s+/g, ' ').trim(), pr: a ? a.getAttribute('href') : '', prText: a ? a.textContent.trim() : '' };
      });
    } catch (e) {
      b.abbruch = String(e && e.message || e);
      b.dom = document.body.innerHTML.slice(0, 3000);
    }
    berichten(b);
  }
  lauf();
})();
`

type ibMarke struct {
	Text   string `json:"text"`
	Aktiv  bool   `json:"aktiv"`
	Anzahl string `json:"anzahl"`
	Farbe  string `json:"farbe"`
	Gruen  string `json:"gruen"`
}

type ibZeile struct {
	Name   string `json:"name"`
	Aktiv  string `json:"aktiv"`
	Text   string `json:"text"`
	PR     string `json:"pr"`
	PRText string `json:"prText"`
}

func TestIssueBranchesImBrowser(t *testing.T) {
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
		fmt.Fprint(w, ibTreiber)
	})
	mux.HandleFunc("/api/repos", func(w http.ResponseWriter, r *http.Request) { protokolliere(r); json200(w, ivRepos) })
	mux.HandleFunc("/api/arbeit", func(w http.ResponseWriter, r *http.Request) { protokolliere(r); json200(w, ibArbeit()) })
	mux.HandleFunc("/api/repo-meta", func(w http.ResponseWriter, r *http.Request) { protokolliere(r); json200(w, ivMeta) })
	mux.HandleFunc("/api/detail", func(w http.ResponseWriter, r *http.Request) { protokolliere(r); json200(w, ibDetail) })
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
		Abbruch       string              `json:"abbruch"`
		DOM           string              `json:"dom"`
		Fehler        []string            `json:"fehler"`
		Fenster       float64             `json:"fenster"`
		Marke         map[string]*ibMarke `json:"marke"`
		Titel         laRechteck          `json:"titel"`
		MarkeRechteck laRechteck          `json:"markeRechteck"`
		Liste         []ibZeile           `json:"liste"`
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

	t.Run("AktiveMarke", func(t *testing.T) {
		mk := b.Marke["50"]
		if mk == nil {
			t.Fatalf("#50 mit zwei Branches ohne Marke")
		}
		if mk.Anzahl != "2" || !strings.HasPrefix(mk.Text, "2") || !strings.Contains(mk.Text, "aktiv") {
			t.Errorf("#50 Marke %+v, erwartet Anzahl 2 und 'aktiv'", mk)
		}
		if !mk.Aktiv {
			t.Errorf("#50 Marke nicht hervorgehoben: %+v", mk)
		}
		if mk.Gruen == "" || !farbeGleich(mk.Farbe, mk.Gruen) {
			t.Errorf("#50 Marke in %q, erwartet das Gruen %q", mk.Farbe, mk.Gruen)
		}
		if n := b.Marke["52"]; n == nil || n.Aktiv || strings.Contains(n.Text, "aktiv") || n.Anzahl != "1" {
			t.Errorf("#52 (ein alter Branch) Marke %+v, erwartet 1 ohne 'aktiv'", n)
		}
	})
	t.Run("OhneBranchKeineMarke", func(t *testing.T) {
		if mk := b.Marke["51"]; mk != nil {
			t.Errorf("#51 ohne Branch zeigt eine Marke: %+v", mk)
		}
	})
	t.Run("Detailspalte", func(t *testing.T) {
		if len(b.Liste) != 2 {
			t.Fatalf("Detailspalte listet %d Branches, erwartet 2: %+v", len(b.Liste), b.Liste)
		}
		z := b.Liste[0]
		if z.Name != "claude/50-issue-branches" || z.Aktiv != "ja" {
			t.Errorf("erster Branch %+v", z)
		}
		for _, teil := range []string{"abcdef1", "↑6", "↓0", "Marke zeichnen"} {
			if !strings.Contains(z.Text, teil) {
				t.Errorf("erster Branch ohne %q: %q", teil, z.Text)
			}
		}
		if z.PR != "https://github.com/acme/alpha/pull/60" || !strings.Contains(z.PRText, "#60") {
			t.Errorf("PR-Link %q %q", z.PR, z.PRText)
		}
		zw := b.Liste[1]
		if zw.Name != "fix-50" || zw.Aktiv != "nein" || !strings.Contains(zw.Text, "0123456") || !strings.Contains(zw.Text, "↑1") ||
			!strings.Contains(zw.Text, "↓23") || zw.PR != "" {
			t.Errorf("zweiter Branch %+v", zw)
		}
	})
	t.Run("TitelNichtVerdeckt", func(t *testing.T) {
		if b.Titel.B < 160 {
			t.Errorf("Titel %v Pixel breit, erwartet mindestens 160", b.Titel.B)
		}
		ti, mk := b.Titel, b.MarkeRechteck
		if mk.B <= 0 {
			t.Fatalf("Marke ohne Breite: %+v", mk)
		}
		if ti.L < mk.R && mk.L < ti.R && ti.O < mk.U && mk.O < ti.U {
			t.Errorf("Titel %+v und Marke %+v ueberschneiden sich", ti, mk)
		}
	})

	// Auf Wunsch ein Bildschirmfoto derselben Seite als Beleg: Liste mit
	// Marken und offener Detailspalte.
	if ziel := os.Getenv("GKP_BELEG_PNG"); ziel != "" {
		if out, err := chromium(ctx, "--screenshot="+ziel); err != nil {
			t.Errorf("Bildschirmfoto: %v\n%s", err, out)
		}
	}
}

// farbeGleich vergleicht eine berechnete Farbe (rgb(r, g, b)) mit einem
// Hex-Wert (#rrggbb) der Themenvariable.
func farbeGleich(berechnet, hex string) bool {
	hex = strings.TrimPrefix(strings.TrimSpace(hex), "#")
	if len(hex) != 6 {
		return strings.EqualFold(strings.TrimSpace(berechnet), strings.TrimSpace(hex))
	}
	var r, g, bl int
	if _, err := fmt.Sscanf(hex, "%02x%02x%02x", &r, &g, &bl); err != nil {
		return false
	}
	return strings.ReplaceAll(berechnet, " ", "") == fmt.Sprintf("rgb(%d,%d,%d)", r, g, bl)
}
