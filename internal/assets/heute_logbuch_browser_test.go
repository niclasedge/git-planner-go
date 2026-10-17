package assets_test

// Browser-Test von Heute mit den naechsten Tagen und dem Logbuch
// (heute-logbuch, #42): kein Demnaechst in der Leiste, Tages-Abschnitte mit
// Morgen, Mehr laden, Logbuch nach Tagen, Repo-Name unter dem Titel.
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

func hlTag(d int) string { return time.Now().AddDate(0, 0, d).Format("2006-01-02") }

func hlUebersicht() string {
	return `{"heute":[
  {"repo":"acme/alpha","nummer":1,"titel":"Eins","url":"u1","faellig":"` + hlTag(-3) + `","termin":"ueberfaellig","tage":-3},
  {"repo":"acme/beta","nummer":7,"titel":"Sieben","url":"u7","faellig":"` + hlTag(0) + `","termin":"heute","tage":0}],
 "demnaechst":[
  {"repo":"acme/alpha","nummer":3,"titel":"Drei","url":"u3","faellig":"` + hlTag(1) + `","termin":"demnaechst","tage":1},
  {"repo":"acme/alpha","nummer":5,"titel":"Fuenf","url":"u5","faellig":"` + hlTag(5) + `","termin":"demnaechst","tage":5}],
 "irgendwann":[{"repo":"acme/alpha","nummer":4,"titel":"Vier","url":"u4","faellig":"","termin":"","tage":0}],
 "zaehler":{"ueberfaellig":1,"heute":1,"demnaechst":3,"irgendwann":1},"fenster":30,"weitere":1,
 "sync":{"zeit":"2026-09-24T10:00:00Z"}}`
}

func hlLogbuch() string {
	return `{"tage":[
  {"datum":"` + hlTag(0) + `","eintraege":[{"repo":"acme/alpha","nummer":2,"titel":"Zwei","url":"u2","geschlossen":"2026-09-24T09:00:00Z"}]},
  {"datum":"` + hlTag(-1) + `","eintraege":[{"repo":"acme/beta","nummer":8,"titel":"Acht","url":"u8","geschlossen":"2026-09-23T09:00:00Z"}]}],
 "fenster":14,"anzahl":2,"quellen":[],"sync":{"zeit":"2026-09-24T10:00:00Z"}}`
}

const hlTreiber = `
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
  function alle(sel) { return Array.prototype.slice.call(document.querySelectorAll(sel)); }
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
      await warte('Leiste', function () { return q('[data-ansicht="heute"]'); });
      b.demnaechstDa = !!q('[data-ansicht="demnaechst"]');
      b.ansichten = alle('[data-ansicht]').map(function (e) { return e.getAttribute('data-ansicht'); });
      q('[data-ansicht="heute"]').click();
      await warte('Tage', function () { return alle('[data-abschnitt]').length >= 4; });
      await schlaf(200);
      b.abschnitte = alle('[data-abschnitt]').map(function (e) { return e.getAttribute('data-abschnitt'); });
      b.titel = alle('[data-abschnitt] h2').map(function (e) { return e.textContent.trim(); });
      b.projekt = q('[data-u-eintrag="acme/beta#7"] .kn-u-projekt').textContent.trim();

      await marke('vor-mehr');
      q('.kn-mehr').click();
      await schlaf(600);

      await marke('vor-logbuch');
      q('[data-ansicht="logbuch"]').click();
      await warte('Logbuch', function () { return q('[data-logbuch]') && alle('[data-abschnitt]').length >= 2; });
      await schlaf(200);
      b.logTage = alle('[data-abschnitt]').map(function (e) { return e.getAttribute('data-abschnitt'); });
      b.logProjekte = alle('[data-abschnitt] .kn-u-projekt').map(function (e) { return e.textContent.trim(); });
      await marke('vor-logbuch-mehr');
      q('.kn-mehr').click();
      await schlaf(600);
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

func TestHeuteUndLogbuchImBrowser(t *testing.T) {
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
		fmt.Fprint(w, hlTreiber)
	})
	mux.HandleFunc("/__marke", func(w http.ResponseWriter, r *http.Request) {
		api.log(anfrage{Marke: r.URL.Query().Get("name")})
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/api/repos", func(w http.ResponseWriter, r *http.Request) { protokolliere(r); json200(w, ivRepos) })
	mux.HandleFunc("/api/repo-stand", func(w http.ResponseWriter, r *http.Request) { protokolliere(r); json200(w, `{"repos":[]}`) })
	mux.HandleFunc("/api/uebersicht", func(w http.ResponseWriter, r *http.Request) { protokolliere(r); json200(w, hlUebersicht()) })
	mux.HandleFunc("/api/logbuch", func(w http.ResponseWriter, r *http.Request) { protokolliere(r); json200(w, hlLogbuch()) })
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
		Abbruch, DOM, Projekt                                      string
		DemnaechstDa                                               bool
		Fehler, Ansichten, Abschnitte, Titel, LogTage, LogProjekte []string
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
	if b.DemnaechstDa || strings.Join(b.Ansichten, ",") != "heute,irgendwann,logbuch" {
		t.Errorf("Ansichten in der Leiste %v, erwartet heute,irgendwann,logbuch", b.Ansichten)
	}
	if want := "ueberfaellig,heute," + hlTag(1) + "," + hlTag(5); strings.Join(b.Abschnitte, ",") != want {
		t.Errorf("Abschnitte %v, erwartet %s", b.Abschnitte, want)
	}
	if len(b.Titel) < 3 || b.Titel[2] != "Morgen" {
		t.Errorf("Ueberschriften %v, erwartet Morgen an dritter Stelle", b.Titel)
	}
	if b.Projekt != "beta" {
		t.Errorf("Projektname unter dem Titel %q, erwartet beta", b.Projekt)
	}
	ab := api.abschnitte()
	us := nur(ab["vor-mehr"], http.MethodGet, "/api/uebersicht")
	if len(us) != 1 || !strings.Contains(us[0].Query, "tage=60") {
		t.Errorf("Mehr laden: %+v, erwartet eine Anfrage mit tage=60", us)
	}
	if strings.Join(b.LogTage, ",") != hlTag(0)+","+hlTag(-1) || strings.Join(b.LogProjekte, ",") != "alpha,beta" {
		t.Errorf("Logbuch Tage %v, Projekte %v", b.LogTage, b.LogProjekte)
	}
	if n := len(nur(ab["vor-logbuch"], http.MethodGet, "/api/logbuch")); n != 1 {
		t.Errorf("%d Anfragen an /api/logbuch beim Oeffnen, erwartet 1", n)
	}
	ls := nur(ab["vor-logbuch-mehr"], http.MethodGet, "/api/logbuch")
	if len(ls) != 1 || !strings.Contains(ls[0].Query, "tage=28") {
		t.Errorf("Logbuch Mehr laden: %+v, erwartet tage=28", ls)
	}
}
