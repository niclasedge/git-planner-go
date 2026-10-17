package assets_test

// Browser-Test der letzten Aenderung unter "Als Naechstes"
// (naechste-geaendert, #118): die eingeklappte Zeile zeigt "geändert" mit
// dem fertigen Text aus Go, "· PR #N" bei der Quelle pr und "· Branch" bei
// der Quelle branch, die volle Zeit im Tooltip; der Fuss der aufgeklappten
// Zeile nennt Issue- und PR-Zeit einzeln. Mechanik wie in
// zuletzt_erledigt_browser_test.go.

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

const ngArbeit = `{"repo":"acme/alpha","fortschritt":{"erledigt":0,"gesamt":0},
 "zuletzt":[],
 "reihe":[
  {"art":"issue","nummer":201,"titel":"naechste-geaendert: letzte Änderung unter Als Nächstes","url":"u201","status":"bereit",
   "blockiert_durch":[],"labels":[],"zustaendig":[],"hat_text":true,"tiefe":0,"aktiv":false,"branches":[],
   "aktualisiert":"2026-09-20T08:00:00Z",
   "zuletzt_geaendert":{"quelle":"pr","zeit":"2026-09-28T12:32:00Z","text":"heute 14:32","voll":"28.09.2026 14:32",
    "pr":7,"issue_text":"20. Sept.","issue_voll":"20.09.2026 10:00","pr_text":"heute 14:32","pr_voll":"28.09.2026 14:32"}},
  {"art":"issue","nummer":202,"titel":"Karte zeigt die Belege","url":"u202","status":"bereit",
   "blockiert_durch":[],"labels":[],"zustaendig":[],"hat_text":true,"tiefe":0,"aktiv":false,"branches":[],
   "aktualisiert":"2026-09-27T09:00:00Z",
   "zuletzt_geaendert":{"quelle":"issue","zeit":"2026-09-27T09:00:00Z","text":"27. Sept.","voll":"27.09.2026 11:00",
    "issue_text":"27. Sept.","issue_voll":"27.09.2026 11:00"}},
  {"art":"issue","nummer":203,"titel":"Leiste merkt sich die Breite","url":"u203","status":"bereit",
   "blockiert_durch":[],"labels":[],"zustaendig":[],"hat_text":false,"tiefe":0,"aktiv":false,
   "branches":[{"name":"203-breite","sha":"abcdef1","kurz_sha":"abcdef1","zeit":"2026-09-26T09:00:00Z","voraus":1,"zurueck":0,"aktiv":false,"gruende":["name"]}],
   "aktualisiert":"2026-09-10T09:00:00Z",
   "zuletzt_geaendert":{"quelle":"branch","zeit":"2026-09-26T09:00:00Z","text":"26. Sept.","voll":"26.09.2026 11:00",
    "issue_text":"10. Sept.","issue_voll":"10.09.2026 11:00"}},
  {"art":"issue","nummer":204,"titel":"Ohne Zeitpunkt","url":"u204","status":"bereit",
   "blockiert_durch":[],"labels":[],"zustaendig":[],"hat_text":false,"tiefe":0,"aktiv":false,"branches":[]}
 ],
 "ohne_issue":[],"prs":[],"quellen":[]}`

const ngTreiber = `
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
    var g = z.querySelector('.kn-e-zeile .kn-e-geaendert');
    return { geaendert: text(g), tooltip: g ? (g.getAttribute('title') || '') : '', quelle: g ? (g.getAttribute('data-quelle') || '') : '' };
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
      await schlaf(100);
      b.z201 = zeile(201);
      b.z202 = zeile(202);
      b.z203 = zeile(203);
      b.z204 = zeile(204);
      if (location.hash !== '#foto') {
        q('[data-eintrag="201"] .kn-e-titel').click();
        await warte('Fuss von #201', function () { return q('[data-eintrag="201"] .kn-e-fuss'); });
        b.fuss = text(q('[data-eintrag="201"] .kn-e-fuss'));
      }
    } catch (e) {
      b.abbruch = String(e && e.message || e);
      b.dom = document.body.innerHTML.slice(0, 3000);
    }
    berichten(b);
  }
  lauf();
})();
`

type ngZeile struct {
	Geaendert string `json:"geaendert"`
	Tooltip   string `json:"tooltip"`
	Quelle    string `json:"quelle"`
}

func TestNaechsteGeaendertImBrowser(t *testing.T) {
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
		fmt.Fprint(w, ngTreiber)
	})
	mux.HandleFunc("/api/repos", func(w http.ResponseWriter, r *http.Request) { json200(w, ivRepos) })
	mux.HandleFunc("/api/arbeit", func(w http.ResponseWriter, r *http.Request) { json200(w, ngArbeit) })
	mux.HandleFunc("/api/repo-meta", func(w http.ResponseWriter, r *http.Request) { json200(w, ivMeta) })
	srv := httptest.NewServer(mux)
	defer srv.Close()

	chromium := func(ctx context.Context, pfad string, extra ...string) ([]byte, error) {
		args := append([]string{
			"--headless", "--no-sandbox", "--disable-gpu", "--no-first-run",
			"--disable-extensions", "--disable-dev-shm-usage", "--hide-scrollbars",
			"--user-data-dir=" + t.TempDir(),
			"--window-size=1400,900",
			"--virtual-time-budget=30000",
		}, extra...)
		return exec.CommandContext(ctx, chrome, append(args, srv.URL+pfad)...).Output()
	}

	ctx, abbrechen := context.WithTimeout(context.Background(), 90*time.Second)
	defer abbrechen()
	out, err := chromium(ctx, "/", "--dump-dom")
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
		Z201    *ngZeile `json:"z201"`
		Z202    *ngZeile `json:"z202"`
		Z203    *ngZeile `json:"z203"`
		Z204    *ngZeile `json:"z204"`
		Fuss    string   `json:"fuss"`
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
	if b.Z201 == nil || b.Z202 == nil || b.Z203 == nil || b.Z204 == nil {
		t.Fatalf("Zeilen fehlen: %+v %+v %+v %+v", b.Z201, b.Z202, b.Z203, b.Z204)
	}
	if b.Z201.Geaendert != "geändert heute 14:32 · PR #7" || b.Z201.Tooltip != "28.09.2026 14:32" || b.Z201.Quelle != "pr" {
		t.Errorf("#201 %+v, erwartet %q mit Tooltip 28.09.2026 14:32", *b.Z201, "geändert heute 14:32 · PR #7")
	}
	if b.Z202.Geaendert != "geändert 27. Sept." || b.Z202.Tooltip != "27.09.2026 11:00" {
		t.Errorf("#202 %+v, erwartet %q", *b.Z202, "geändert 27. Sept.")
	}
	if b.Z203.Geaendert != "geändert 26. Sept. · Branch" {
		t.Errorf("#203 %+v, erwartet %q", *b.Z203, "geändert 26. Sept. · Branch")
	}
	if b.Z204.Geaendert != "" {
		t.Errorf("#204 %+v, erwartet nichts", *b.Z204)
	}
	for _, teil := range []string{"Issue geändert 20. Sept.", "PR #7 geändert heute 14:32"} {
		if !strings.Contains(b.Fuss, teil) {
			t.Errorf("Fuss von #201 %q ohne %q", b.Fuss, teil)
		}
	}

	// Auf Wunsch ein Bildschirmfoto der eingeklappten Zeilen als Beleg.
	if ziel := os.Getenv("GKP_BELEG_PNG"); ziel != "" {
		if out, err := chromium(ctx, "/#foto", "--screenshot="+ziel); err != nil {
			t.Errorf("Bildschirmfoto: %v\n%s", err, out)
		}
	}
}
