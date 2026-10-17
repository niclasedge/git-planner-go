package assets_test

// Browser-Test der PR-Aktionen und der Beleg-Anzeige (pr-aktionen, #32).
// Mechanik wie in repo_leiste_browser_test.go.

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
)

const prArbeit = `{"repo":"acme/alpha","fortschritt":{"erledigt":1,"gesamt":2},"zuletzt":[],
 "reihe":[{"art":"issue","nummer":4,"titel":"Vier","status":"in_arbeit","blockiert_durch":[],"labels":[],"zustaendig":[],"hat_text":false,
   "change":{"kennung":"vier","ref":"change/vier","status":"laeuft","aufgaben":[{"text":"A","erledigt":true}],"erledigt":1,"gesamt":1,"gates":[],
     "belege":[{"stufe":"nachher","art":"bild","pfad":"belege/assets/0-v.png","bild":"/api/blob?repo=acme%2Falpha&sha=b&name=0-v.png"}]}}],
 "ohne_issue":[{"art":"change","titel":"los","status":"bereit","blockiert_durch":[],"labels":[],"zustaendig":[],
   "change":{"kennung":"los","ref":"main","status":"frei","aufgaben":[],"erledigt":0,"gesamt":0,"gates":[],"belege":[]}}],
 "prs":[{"nummer":9,"titel":"Neun","url":"u9","autor":"cy","labels":[],"draft":true,"head":"change/neun","head_repo":"acme/alpha","head_sha":"s9","base":"main","mergebar":true,"merge_status":"draft"},
        {"nummer":10,"titel":"Zehn","url":"u10","autor":"cy","labels":[],"draft":false,"head":"change/zehn","head_repo":"acme/alpha","head_sha":"s10","base":"main","mergebar":true,"merge_status":"clean"}],
 "quellen":[]}`

const prTreiber = `
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
      await warte('Liste', function () { return q('[data-eintrag="pr-10"]'); });
      await schlaf(200);
      b.belegVier = (q('[data-eintrag="4"] [data-beleg]') || {}).getAttribute ? q('[data-eintrag="4"] [data-beleg]').getAttribute('data-beleg') : null;
      b.belegLos = (q('[data-eintrag="los"] [data-beleg]') || {}).getAttribute ? q('[data-eintrag="los"] [data-beleg]').getAttribute('data-beleg') : null;
      b.zeileNeun = q('[data-eintrag="pr-9"]').textContent;
      b.zeileZehn = q('[data-eintrag="pr-10"]').textContent;

      await marke('vor-freigeben');
      q('[data-eintrag="pr-9"] .kn-e-titel').click();
      await warte('Freigeben-Knopf', function () { return q('[data-eintrag="pr-9"] .kn-pr-freigeben'); });
      q('[data-eintrag="pr-9"] .kn-pr-freigeben').click();
      await schlaf(800);

      await marke('vor-mergen');
      q('[data-eintrag="pr-10"] .kn-e-titel').click();
      await warte('Mergen-Knopf', function () { return q('[data-eintrag="pr-10"] .kn-pr-mergen'); });
      b.haken = q('[data-eintrag="pr-10"] .kn-pr-branch input').checked;
      b.knopf = q('[data-eintrag="pr-10"] .kn-pr-mergen').textContent.trim();
      q('[data-eintrag="pr-10"] .kn-pr-mergen').click();
      await schlaf(300);
      b.rueckfrage = q('[data-eintrag="pr-10"] .kn-pr-mergen').textContent;
      q('[data-eintrag="pr-10"] .kn-pr-mergen').click();
      await schlaf(800);
      await marke('ende');
    } catch (e) {
      b.abbruch = String(e && e.message || e);
      b.dom = document.body.innerHTML.slice(0, 3000);
    }
    berichten(b);
  }
  lauf();
})();
`

func TestPRAktionenImBrowser(t *testing.T) {
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
		fmt.Fprint(w, prTreiber)
	})
	mux.HandleFunc("/__marke", func(w http.ResponseWriter, r *http.Request) {
		api.log(anfrage{Marke: r.URL.Query().Get("name")})
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/api/repos", func(w http.ResponseWriter, r *http.Request) { protokolliere(r); json200(w, ivRepos) })
	mux.HandleFunc("/api/arbeit", func(w http.ResponseWriter, r *http.Request) { protokolliere(r); json200(w, prArbeit) })
	mux.HandleFunc("/api/repo-meta", func(w http.ResponseWriter, r *http.Request) { protokolliere(r); json200(w, ivMeta) })
	mux.HandleFunc("/api/blob", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write(pngBytes)
	})
	mux.HandleFunc("/action/pr/", func(w http.ResponseWriter, r *http.Request) { protokolliere(r); json200(w, `{"ok":true}`) })
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
	var b struct {
		Abbruch, DOM         string
		Fehler               []string
		BelegVier, BelegLos  *string
		ZeileNeun, ZeileZehn string
		Haken                bool
		Rueckfrage           string
		Knopf                string
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

	t.Run("BelegAnzeige", func(t *testing.T) {
		if b.BelegVier == nil || *b.BelegVier != "ja" {
			t.Errorf("Change mit Bild: data-beleg %v, erwartet ja", b.BelegVier)
		}
		if b.BelegLos == nil || *b.BelegLos != "nein" {
			t.Errorf("Change ohne Bild: data-beleg %v, erwartet nein", b.BelegLos)
		}
	})
	t.Run("PRZeilen", func(t *testing.T) {
		if !strings.Contains(b.ZeileNeun, "Entwurf") || !strings.Contains(b.ZeileNeun, "change/neun") {
			t.Errorf("Zeile #9 %q, erwartet Entwurf und change/neun", b.ZeileNeun)
		}
		if !strings.Contains(b.ZeileZehn, "mergebar") {
			t.Errorf("Zeile #10 %q, erwartet mergebar", b.ZeileZehn)
		}
	})
	t.Run("Freigeben", func(t *testing.T) {
		fs := nur(ab["vor-freigeben"], http.MethodPost, "/action/pr/freigeben")
		if len(fs) != 1 || !strings.Contains(fs[0].Rumpf, `"nummer":9`) {
			t.Errorf("freigeben %+v", fs)
		}
		if !frischGeladen(ab["vor-freigeben"]) {
			t.Error("nach dem Freigeben kein /api/arbeit mit frisch=1")
		}
	})
	t.Run("Mergen", func(t *testing.T) {
		if !b.Haken {
			t.Error("Branch loeschen ist nicht vorausgewaehlt")
		}
		if b.Knopf != "PR mergen" {
			t.Errorf("Knopf %q, erwartet \"PR mergen\" (nicht zu verwechseln mit der Option Mergen beim Konflikt)", b.Knopf)
		}
		if !strings.Contains(b.Rueckfrage, "?") {
			t.Errorf("nach dem ersten Klick keine Rueckfrage, Knopf %q", b.Rueckfrage)
		}
		ms := nur(ab["vor-mergen"], http.MethodPost, "/action/pr/mergen")
		if len(ms) != 1 || !strings.Contains(ms[0].Rumpf, `"nummer":10`) || !strings.Contains(ms[0].Rumpf, `"branch_loeschen":true`) {
			t.Errorf("mergen %+v, erwartet genau einen Aufruf nach der Rueckfrage", ms)
		}
		if !frischGeladen(ab["vor-mergen"]) {
			t.Error("nach dem Mergen kein /api/arbeit mit frisch=1")
		}
	})
}

func frischGeladen(as []anfrage) bool {
	for _, a := range nur(as, http.MethodGet, "/api/arbeit") {
		if strings.Contains(a.Query, "frisch=1") {
			return true
		}
	}
	return false
}
