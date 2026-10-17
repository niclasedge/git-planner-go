package assets_test

// Browser-Test des erledigten Blockers unter "Als Nächstes"
// (reihe-erledigte-blocker, #130): er steht abgehakt mit Schliesszeit, beginnt
// aufgeklappt mit seinen Kindern darunter und klappt auf Klick zu. Mechanik
// wie in zeitleiste_browser_test.go.
//
// Mit GKP_BELEG_PNG legt der Test ein Bildschirmfoto ab.

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

func ebArbeit() string {
	reihe := []string{
		`{"art":"issue","nummer":4,"titel":"Frage geklaert","url":"u4","status":"erledigt","blockiert_durch":[],"labels":[],"zustaendig":[],
		  "hat_text":false,"tiefe":0,"aktiv":false,"branches":[],"geschlossen":"2026-10-01T07:12:00Z","geschlossen_text":"heute 09:12",
		  "geschlossen_voll":"01.10.2026 09:12",
		  "abhaengige":{"gesamt":3,"offen":2,"fertig":1,"kinder":2,"aufgeklappt":true,"text":"3 abhängige Issues · 2 offen · 1 fertig"}}`,
		`{"art":"issue","nummer":3,"titel":"Issue 3","url":"u3","status":"bereit","blockiert_durch":[],"erledigte_blocker":[4],"labels":[],
		  "zustaendig":[],"hat_text":false,"tiefe":1,"unter":4,"vorfahren":[4],"aktiv":false,"branches":[]}`,
		`{"art":"issue","nummer":5,"titel":"Issue 5","url":"u5","status":"bereit","blockiert_durch":[],"erledigte_blocker":[4],"labels":[],
		  "zustaendig":[],"hat_text":false,"tiefe":1,"unter":4,"vorfahren":[4],"aktiv":false,"branches":[]}`,
		`{"art":"issue","nummer":1,"titel":"Issue 1","url":"u1","status":"bereit","blockiert_durch":[],"labels":[],"zustaendig":[],
		  "hat_text":false,"tiefe":0,"aktiv":false,"branches":[]}`,
	}
	return `{"repo":"acme/alpha","fortschritt":{"erledigt":0,"gesamt":0},"zuletzt":[],"letzte_aktionen":[],
	 "karte_kopf":{"erledigt":1,"aktiv":0,"naechste":3,"fragen":0,"p1":[],"bereit":3},
	 "reihe":[` + strings.Join(reihe, ",") + `],"ohne_issue":[],"prs":[],"quellen":[]}`
}

const ebTreiber = `
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
    var h = qa('h2').filter(function (x) { return text(x) === 'Als Nächstes'; })[0];
    var s = h.closest('section');
    var z = s.querySelector('[data-abhaengige="4"]');
    return {
      eintraege: Array.prototype.slice.call(s.querySelectorAll('.kn-e[data-eintrag]')).map(function (e) { return e.getAttribute('data-eintrag'); }).join(','),
      zeile: text(z), offen: z ? z.getAttribute('aria-expanded') || '' : '',
    };
  }
  async function lauf() {
    var b = {};
    try {
      await warte('Leiste', function () { return q('[data-repo="acme/alpha"]'); });
      q('[data-repo="acme/alpha"]').click();
      await warte('Reihe', function () { return q('.kn-e[data-eintrag="4"]'); });
      await schlaf(300);
      if (new URLSearchParams(location.search).get('foto')) {
        var h = qa('h2').filter(function (x) { return text(x) === 'Als Nächstes'; })[0];
        window.scrollTo(0, Math.max(0, h.getBoundingClientRect().top + window.scrollY - 12));
        return;
      }
      var vier = q('.kn-e[data-eintrag="4"]');
      b.status = vier.getAttribute('data-status');
      b.kasten = (vier.querySelector('.kn-e-check') || { getAttribute: function () { return ''; } }).getAttribute('aria-checked');
      b.datum = text(vier.querySelector('.kn-e-datum'));
      b.staende = [stand()];
      q('[data-abhaengige="4"]').click();
      await schlaf(200);
      b.staende.push(stand());
    } catch (e) {
      b.abbruch = String(e && e.message || e);
      b.dom = document.body.innerHTML.slice(0, 4000);
    }
    berichten(b);
  }
  lauf();
})();
`

type ebBericht struct {
	Abbruch, DOM          string
	Fehler                []string
	Status, Kasten, Datum string
	Staende               []struct{ Eintraege, Zeile, Offen string }
}

func TestErledigterBlockerImBrowser(t *testing.T) {
	if testing.Short() {
		t.Skip("-short: der Test startet einen Browser")
	}
	chrome := chromiumFinden()
	if chrome == "" {
		t.Skip("kein Chromium gefunden — die Oberflaeche ist damit NICHT belegt")
	}
	arbeit := ebArbeit()
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
		fmt.Fprint(w, ebTreiber)
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
			"--window-size=1400,520",
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
	var b ebBericht
	if err := json.Unmarshal(roh, &b); err != nil {
		t.Fatalf("Bericht kein JSON: %v", err)
	}
	if b.Abbruch != "" {
		t.Fatalf("Treiber abgebrochen: %s\nFehler: %v\nDOM: %s", b.Abbruch, b.Fehler, b.DOM)
	}
	if len(b.Fehler) > 0 {
		t.Errorf("JavaScript-Fehler: %v", b.Fehler)
	}
	if b.Status != "erledigt" || b.Kasten != "true" || !strings.HasPrefix(b.Datum, "heute 09:12") {
		t.Errorf("#4 status %q kasten %q datum %q, erwartet erledigt, abgehakt, heute 09:12", b.Status, b.Kasten, b.Datum)
	}
	if len(b.Staende) != 2 {
		t.Fatalf("%d Staende, erwartet 2", len(b.Staende))
	}
	if s := b.Staende[0]; s.Eintraege != "4,3,5,1" || s.Zeile != "▾ 3 abhängige Issues · 2 offen · 1 fertig" || s.Offen != "true" {
		t.Errorf("zuerst %+v, erwartet 4,3,5,1 mit ▾ und aria-expanded true", s)
	}
	if s := b.Staende[1]; s.Eintraege != "4,1" || !strings.HasPrefix(s.Zeile, "▸ ") || s.Offen != "false" {
		t.Errorf("nach dem Klick %+v, erwartet 4,1 mit ▸", s)
	}

	if ziel := os.Getenv("GKP_BELEG_PNG"); ziel != "" {
		if out, err := chromium(ctx, srv.URL+"/?foto=1", "--screenshot="+ziel); err != nil {
			t.Errorf("Bildschirmfoto: %v\n%s", err, out)
		}
	}
}
