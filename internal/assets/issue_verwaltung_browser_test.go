package assets_test

// Browser-Test der Schreibaktionen auf Issues (issue-verwaltung, #26):
// Checkbox schliesst, neues Issue, Kommentar und Label in der Detailspalte.
// Mechanik wie in repo_leiste_browser_test.go (Testseite, Treiber, Bericht,
// protokollierende Fake-API); chromiumFinden, anfrage, fakeAPI, nur,
// testseite und berichtRe stehen dort.

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

const ivRepos = `{"kategorien":[{"name":"Arbeit","ohne":false,"eingeklappt":false,"repos":[
 {"nwo":"acme/alpha","name":"alpha","owner":"acme","offene_issues":1,"pat":"GITHUB_PAT_WORK"}]}],"konten":[],"stand":"x"}`

const ivArbeit = `{"repo":"acme/alpha","fortschritt":{"erledigt":0,"gesamt":0},"zuletzt":[],
 "reihe":[{"art":"issue","nummer":4,"titel":"Vier","url":"u4","status":"ohne_change","blockiert_durch":[],
   "labels":[{"name":"bug","farbe":"d73a4a"}],"zustaendig":[],"hat_text":true}],
 "ohne_issue":[],"prs":[],"quellen":[]}`

const ivDetail = `{"art":"issue","nummer":4,"titel":"Vier","url":"u4","text":"Text",
 "labels":[{"name":"bug","farbe":"d73a4a"}],"zustaendig":[],"blockiert_durch":[],
 "kommentare":[{"autor":"ana","text":"Wann?","zeit":"2026-09-20T00:00:00Z"}]}`

const ivMeta = `{"labels":[{"name":"bug","farbe":"d73a4a"},{"name":"ui","farbe":"0e8a16"}],"zustaendige":["ana","bo"]}`

const ivTreiber = `
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
  function tippe(el, wert) { el.value = wert; el.dispatchEvent(new Event('input', { bubbles: true })); }
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
      await warte('Liste', function () { return q('[data-eintrag="4"] .kn-e-check'); });
      await schlaf(200);

      await marke('vor-check');
      q('[data-eintrag="4"] .kn-e-check').click();
      await schlaf(800);

      await marke('vor-neu');
      q('.kn-neues-issue-knopf').click();
      await warte('Formular', function () { return q('.kn-neues-issue input[name=titel]'); });
      tippe(q('.kn-neues-issue input[name=titel]'), 'Neu');
      tippe(q('.kn-neues-issue textarea[name=text]'), 'Beschreibung');
      await schlaf(150);
      q('.kn-neues-issue button[type=submit]').click();
      await schlaf(800);

      await marke('vor-detail');
      q('[data-eintrag="4"] .kn-e-details').click();
      await warte('Kommentare', function () { return q('.kn-kommentar'); });
      b.kommentar = q('.kn-kommentar').textContent;
      tippe(q('.kn-kommentieren textarea'), 'Hallo');
      await schlaf(150);
      q('.kn-kommentieren button').click();
      await schlaf(800);

      await marke('vor-label');
      await warte('Labels', function () { return q('.kn-label-wahl [data-label="ui"]'); });
      q('.kn-label-wahl [data-label="ui"]').click();
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

func TestIssueVerwaltungImBrowser(t *testing.T) {
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
		fmt.Fprint(w, ivTreiber)
	})
	mux.HandleFunc("/__marke", func(w http.ResponseWriter, r *http.Request) {
		api.log(anfrage{Marke: r.URL.Query().Get("name")})
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/api/repos", func(w http.ResponseWriter, r *http.Request) { protokolliere(r); json200(w, ivRepos) })
	mux.HandleFunc("/api/arbeit", func(w http.ResponseWriter, r *http.Request) { protokolliere(r); json200(w, ivArbeit) })
	mux.HandleFunc("/api/detail", func(w http.ResponseWriter, r *http.Request) { protokolliere(r); json200(w, ivDetail) })
	mux.HandleFunc("/api/repo-meta", func(w http.ResponseWriter, r *http.Request) { protokolliere(r); json200(w, ivMeta) })
	mux.HandleFunc("/action/issue/", func(w http.ResponseWriter, r *http.Request) {
		protokolliere(r)
		json200(w, `{"ok":true,"issue":{"nummer":31,"titel":"Neu","url":"u31"}}`)
	})
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
		Abbruch   string   `json:"abbruch"`
		DOM       string   `json:"dom"`
		Fehler    []string `json:"fehler"`
		Kommentar string   `json:"kommentar"`
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

	t.Run("Checkbox", func(t *testing.T) {
		as := nur(ab["vor-check"], http.MethodPost, "/action/issue/aendern")
		if len(as) != 1 || !strings.Contains(as[0].Rumpf, `"nummer":4`) || !strings.Contains(as[0].Rumpf, `"zustand":"closed"`) {
			t.Errorf("aendern %+v, erwartet nummer 4 mit zustand closed", as)
		}
		frisch := false
		for _, a := range nur(ab["vor-check"], http.MethodGet, "/api/arbeit") {
			if strings.Contains(a.Query, "frisch=1") {
				frisch = true
			}
		}
		if !frisch {
			t.Errorf("nach dem Schliessen keine Anfrage /api/arbeit mit frisch=1: %+v", ab["vor-check"])
		}
	})
	t.Run("NeuesIssue", func(t *testing.T) {
		as := nur(ab["vor-neu"], http.MethodPost, "/action/issue/anlegen")
		if len(as) != 1 || !strings.Contains(as[0].Rumpf, `"titel":"Neu"`) || !strings.Contains(as[0].Rumpf, `"repo":"acme/alpha"`) {
			t.Errorf("anlegen %+v", as)
		}
	})
	t.Run("Kommentar", func(t *testing.T) {
		ds := nur(ab["vor-detail"], http.MethodGet, "/api/detail")
		if len(ds) < 1 || !strings.Contains(ds[0].Query, "art=issue") {
			t.Errorf("Detail-Anfragen %+v", ds)
		}
		if !strings.Contains(b.Kommentar, "Wann?") || !strings.Contains(b.Kommentar, "ana") {
			t.Errorf("Kommentar %q", b.Kommentar)
		}
		ks := nur(ab["vor-detail"], http.MethodPost, "/action/issue/kommentieren")
		if len(ks) != 1 || !strings.Contains(ks[0].Rumpf, `"text":"Hallo"`) || !strings.Contains(ks[0].Rumpf, `"nummer":4`) {
			t.Errorf("kommentieren %+v", ks)
		}
	})
	t.Run("Label", func(t *testing.T) {
		as := nur(ab["vor-label"], http.MethodPost, "/action/issue/aendern")
		if len(as) != 1 || !strings.Contains(as[0].Rumpf, `"labels":[`) || !strings.Contains(as[0].Rumpf, `"ui"`) || !strings.Contains(as[0].Rumpf, `"bug"`) {
			t.Errorf("aendern labels %+v, erwartet bug und ui", as)
		}
	})
}
