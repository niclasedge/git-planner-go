package assets_test

// Browser-Test der Faelligkeit (faelligkeit, #39): Heute in der Leiste mit
// Zaehler, Abschnitte Ueberfaellig vor Heute, "Morgen" in der Detailspalte
// schreibt das Datum von morgen, das Formular Neues Issue schickt faellig mit.
// Mechanik wie in issue_verwaltung_browser_test.go.

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

func faelligUebersicht() string {
	tag := func(d int) string { return time.Now().AddDate(0, 0, d).Format("2006-01-02") }
	return `{"heute":[
  {"repo":"acme/alpha","nummer":1,"titel":"Eins","url":"u1","faellig":"` + tag(-3) + `","termin":"ueberfaellig","tage":-3},
  {"repo":"acme/beta","nummer":7,"titel":"Sieben","url":"u7","faellig":"` + tag(0) + `","termin":"heute","tage":0}],
 "demnaechst":[{"repo":"acme/alpha","nummer":3,"titel":"Drei","url":"u3","faellig":"` + tag(5) + `","termin":"demnaechst","tage":5}],
 "irgendwann":[{"repo":"acme/alpha","nummer":4,"titel":"Vier","url":"u4","faellig":"","termin":"","tage":0}],
 "zaehler":{"ueberfaellig":1,"heute":1,"demnaechst":1,"irgendwann":1},
 "sync":{"zeit":"2026-09-24T10:00:00Z"}}`
}

const faelligTreiber = `
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
  function tippe(el, wert) { el.value = wert; el.dispatchEvent(new Event('input', { bubbles: true })); el.dispatchEvent(new Event('change', { bubbles: true })); }
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
      await warte('Heute in der Leiste', function () { return q('[data-ansicht="heute"] .kn-ansicht-zahl'); });
      b.zahl = q('[data-ansicht="heute"] .kn-ansicht-zahl').textContent.trim();
      q('[data-ansicht="heute"]').click();
      await warte('Abschnitte', function () { return document.querySelectorAll('[data-abschnitt]').length >= 2; });
      await schlaf(200);
      b.abschnitte = Array.prototype.map.call(document.querySelectorAll('[data-abschnitt]'), function (e) { return e.getAttribute('data-abschnitt'); });
      b.zeilen = Array.prototype.map.call(document.querySelectorAll('[data-u-eintrag]'), function (e) { return e.getAttribute('data-u-eintrag'); });
      b.datum = q('[data-u-eintrag="acme/alpha#1"] .kn-faellig').textContent.trim();

      await marke('vor-morgen');
      q('[data-u-eintrag="acme/beta#7"] .kn-e-details').click();
      await warte('Datumsknopf', function () { return q('.kn-faellig-morgen'); });
      q('.kn-faellig-morgen').click();
      await schlaf(800);

      await marke('vor-irgendwann');
      q('[data-ansicht="irgendwann"]').click();
      await warte('Irgendwann', function () { return q('[data-u-eintrag="acme/alpha#4"]'); });

      await marke('vor-neu');
      q('[data-repo="acme/alpha"]').click();
      await warte('Knopf', function () { return q('.kn-neues-issue-knopf'); });
      q('.kn-neues-issue-knopf').click();
      await warte('Formular', function () { return q('.kn-neues-issue input[name=faellig]'); });
      tippe(q('.kn-neues-issue input[name=titel]'), 'Neu');
      tippe(q('.kn-neues-issue input[name=faellig]'), '2026-12-24');
      await schlaf(150);
      q('.kn-neues-issue button[type=submit]').click();
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

func TestFaelligkeitImBrowser(t *testing.T) {
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
		fmt.Fprint(w, faelligTreiber)
	})
	mux.HandleFunc("/__marke", func(w http.ResponseWriter, r *http.Request) {
		api.log(anfrage{Marke: r.URL.Query().Get("name")})
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/api/repos", func(w http.ResponseWriter, r *http.Request) { protokolliere(r); json200(w, ivRepos) })
	mux.HandleFunc("/api/uebersicht", func(w http.ResponseWriter, r *http.Request) { protokolliere(r); json200(w, faelligUebersicht()) })
	mux.HandleFunc("/api/arbeit", func(w http.ResponseWriter, r *http.Request) { protokolliere(r); json200(w, ivArbeit) })
	mux.HandleFunc("/api/detail", func(w http.ResponseWriter, r *http.Request) {
		protokolliere(r)
		json200(w, `{"art":"issue","nummer":7,"titel":"Sieben","url":"u7","text":"Text","faellig":"`+time.Now().Format("2006-01-02")+`",
		 "labels":[],"zustaendig":[],"blockiert_durch":[],"kommentare":[]}`)
	})
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
		Abbruch, DOM, Zahl, Datum  string
		Fehler, Abschnitte, Zeilen []string
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
	if b.Zahl != "2" {
		t.Errorf("Zaehler an Heute %q, erwartet 2 (ueberfaellig + heute)", b.Zahl)
	}
	// Heute zeigt auch die naechsten Tage (heute-logbuch, #42).
	fuenf := time.Now().AddDate(0, 0, 5).Format("2006-01-02")
	if strings.Join(b.Abschnitte, ",") != "ueberfaellig,heute,"+fuenf {
		t.Errorf("Abschnitte %v, erwartet ueberfaellig,heute,%s", b.Abschnitte, fuenf)
	}
	if strings.Join(b.Zeilen, ",") != "acme/alpha#1,acme/beta#7,acme/alpha#3" {
		t.Errorf("Zeilen %v", b.Zeilen)
	}
	if b.Datum == "" {
		t.Error("Zeile ohne Datum")
	}
	ab := api.abschnitte()
	ds := nur(ab["vor-morgen"], http.MethodGet, "/api/detail")
	if len(ds) < 1 || !strings.Contains(ds[0].Query, "repo=acme%2Fbeta") || !strings.Contains(ds[0].Query, "nummer=7") {
		t.Errorf("Detail-Anfragen %+v, erwartet acme/beta #7", ds)
	}
	morgen := time.Now().AddDate(0, 0, 1).Format("2006-01-02")
	as := nur(ab["vor-morgen"], http.MethodPost, "/action/issue/aendern")
	if len(as) != 1 || !strings.Contains(as[0].Rumpf, `"faellig":"`+morgen+`"`) || !strings.Contains(as[0].Rumpf, `"repo":"acme/beta"`) {
		t.Errorf("aendern %+v, erwartet acme/beta mit faellig %s", as, morgen)
	}
	ns := nur(ab["vor-neu"], http.MethodPost, "/action/issue/anlegen")
	if len(ns) != 1 || !strings.Contains(ns[0].Rumpf, `"faellig":"2026-12-24"`) {
		t.Errorf("anlegen %+v, erwartet faellig 2026-12-24", ns)
	}
}
