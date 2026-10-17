package assets_test

// Browser-Test der Karte (repo-karte, #36; als Folge-Linienplan seit
// repo-karte-folge, #99): eingeklappt ohne Anfrage, aufgeklappt genau eine,
// dann Zonenbaender, Linien, Stationen, Frage-Karten, Schranken, Schraffur,
// Leuchtrand, Bereit und die Jetzt-Karten; ein Klick auf eine Linie oder eine
// Station per Doppelklick oeffnet die Detailspalte. Die Fake-API liefert genau die Karte, die
// appctx.KarteBauen aus dem Szenario des Entwurfs baut
// (internal/appctx/testdata/karte_szenario_karte.json, dort gegen KarteBauen
// geprueft). Mechanik wie in repo_leiste_browser_test.go.
//
// Mit GKP_BELEG_PNG_REPO_KARTE legt der Test ein Bildschirmfoto der
// aufgeklappten Karte ab.

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

const karteTreiber = `
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
  function qa(sel) { return Array.prototype.slice.call(document.querySelectorAll(sel)); }
  function attr(sel, name) { return qa(sel).map(function (e) { return e.getAttribute(name); }); }
  function text(e) { return e ? e.textContent.replace(/\s+/g, ' ').trim() : ''; }
  function klick(e) { e.dispatchEvent(new MouseEvent('click', { bubbles: true })); }
  function berichten(b) {
    b.fehler = (b.fehler || []).concat(fehler);
    var pre = document.createElement('pre');
    pre.id = 'kn-testbericht';
    pre.textContent = btoa(unescape(encodeURIComponent(JSON.stringify(b))));
    document.body.appendChild(pre);
  }
  async function aufklappen() {
    await warte('Leiste', function () { return q('[data-repo="acme/alpha"]'); });
    q('[data-repo="acme/alpha"]').click();
    await warte('Karte-Kopf', function () { return q('.kn-karte-kopf'); });
    await schlaf(300);
  }
  async function lauf() {
    var b = {};
    try {
      if (new URLSearchParams(location.search).get('foto')) {
        await aufklappen();
        q('.kn-karte-kopf').click();
        await warte('Stationen', function () { return qa('[data-station]').length >= 20; });
        await schlaf(300);
        q('.kn-karte').scrollIntoView({ block: 'start' });
        return;
      }
      await aufklappen();
      b.vorher = qa('[data-station]').length;
      await marke('vor-aufklappen');
      q('.kn-karte-kopf').click();
      await warte('Stationen', function () { return qa('[data-station]').length >= 20; });
      await schlaf(200);
      b.zonen = attr('[data-zone-band]', 'data-zone-band');
      b.linien = attr('[data-linie]', 'data-linie');
      b.stationen = qa('[data-station]').length;
      b.fragen = attr('[data-frage]', 'data-frage');
      b.prios = attr('[data-frage]', 'data-prio');
      b.frageKanten = qa('[data-kante][data-art="frage"]').length;
      b.schranken = qa('[data-schranke]').length;
      b.gesperrt = attr('.kn-karte-gesperrt[data-station]', 'data-station');
      b.kritisch = attr('.kn-karte-kritisch[data-station]', 'data-station');
      b.frageKritisch = attr('.kn-karte-kritisch[data-frage]', 'data-frage');
      b.bereit = attr('.kn-karte-bereit[data-station]', 'data-station');
      b.karte98 = text(q('[data-jetzt-karte="98"]'));
      b.checks98 = attr('[data-jetzt-karte="98"] [data-checks]', 'data-checks');
      b.karte101 = text(q('[data-jetzt-karte="101"]'));
      b.frage88 = text(q('[data-frage="88"]'));
      b.kopf = text(q('.kn-karte-zahl'));
      await marke('vor-klick-linie');
      klick(q('[data-linie-titel="36"]'));
      await warte('Detail', function () { return q('.kn-detail'); });
      await schlaf(300);
      // Ein einfacher Klick springt zur Zeile (karte-gruppen, #121); die
      // Detailspalte oeffnet der Doppelklick.
      await marke('vor-klick-station');
      q('[data-station="102"]').dispatchEvent(new MouseEvent('dblclick', { bubbles: true }));
      await schlaf(500);
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

func TestKarteImBrowser(t *testing.T) {
	if testing.Short() {
		t.Skip("-short: der Test startet einen Browser")
	}
	karteJSON, err := os.ReadFile("../appctx/testdata/karte_szenario_karte.json")
	if err != nil {
		t.Fatalf("Karte des Szenarios fehlt: %v", err)
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
		fmt.Fprint(w, karteTreiber)
	})
	mux.HandleFunc("/__marke", func(w http.ResponseWriter, r *http.Request) {
		api.log(anfrage{Marke: r.URL.Query().Get("name")})
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/api/repos", func(w http.ResponseWriter, r *http.Request) { protokolliere(r); json200(w, ivRepos) })
	mux.HandleFunc("/api/arbeit", func(w http.ResponseWriter, r *http.Request) { protokolliere(r); json200(w, ivArbeit) })
	mux.HandleFunc("/api/repo-meta", func(w http.ResponseWriter, r *http.Request) { protokolliere(r); json200(w, ivMeta) })
	mux.HandleFunc("/api/karte", func(w http.ResponseWriter, r *http.Request) { protokolliere(r); json200(w, string(karteJSON)) })
	mux.HandleFunc("/api/detail", func(w http.ResponseWriter, r *http.Request) { protokolliere(r); json200(w, ivDetail) })
	srv := httptest.NewServer(mux)
	defer srv.Close()

	chromium := func(ctx context.Context, adresse string, extra ...string) ([]byte, error) {
		args := append([]string{
			"--headless", "--no-sandbox", "--disable-gpu", "--no-first-run",
			"--disable-extensions", "--disable-dev-shm-usage", "--hide-scrollbars",
			"--user-data-dir=" + t.TempDir(),
			"--window-size=2200,1500",
			"--virtual-time-budget=30000",
		}, extra...)
		return exec.CommandContext(ctx, chrome, append(args, adresse)...).Output()
	}
	ctx, abbrechen := context.WithTimeout(context.Background(), 120*time.Second)
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
	var b struct {
		Abbruch, DOM                              string
		Fehler                                    []string
		Vorher, Stationen, FrageKanten, Schranken int
		Zonen, Linien, Fragen, Prios              []string
		Gesperrt, Kritisch, FrageKritisch, Bereit []string
		Karte98, Karte101, Frage88, Kopf          string
		Checks98                                  []string
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
	if n := len(nur(ab[""], http.MethodGet, "/api/karte")); n != 0 || b.Vorher != 0 {
		t.Errorf("vor dem Aufklappen %d Anfragen an /api/karte und %d Stationen, erwartet 0", n, b.Vorher)
	}
	if n := len(nur(ab["vor-aufklappen"], http.MethodGet, "/api/karte")); n != 1 {
		t.Errorf("beim Aufklappen %d Anfragen an /api/karte, erwartet 1", n)
	}
	liste := func(s []string) string { return strings.Join(s, ",") }
	if liste(b.Zonen) != "erledigt,jetzt,naechste" || liste(b.Linien) != "36,60,70,0" || b.Stationen != 23 {
		t.Errorf("Zonen %v, Linien %v, %d Stationen; erwartet erledigt,jetzt,naechste, 36,60,70,0 und 23", b.Zonen, b.Linien, b.Stationen)
	}
	if liste(b.Fragen) != "88,91,93" || liste(b.Prios) != "1,1,2" {
		t.Errorf("Fragen %v Prio %v, erwartet 88,91,93 mit 1,1,2", b.Fragen, b.Prios)
	}
	if !strings.Contains(b.Frage88, "Fork-Repos: PAT des Forks oder des Upstreams?") || !strings.Contains(b.Frage88, "#102") {
		t.Errorf("Frage-Karte #88 %q, erwartet Wortlaut und sperrt #102", b.Frage88)
	}
	if b.FrageKanten != 3 || b.Schranken != 3 {
		t.Errorf("%d Frage-Kanten, %d Schranken; erwartet je 3", b.FrageKanten, b.Schranken)
	}
	if liste(b.Gesperrt) != "102,104,106,107,109,111" {
		t.Errorf("schraffiert %v, erwartet 102,104,106,107,109,111", b.Gesperrt)
	}
	if liste(b.Kritisch) != "102,104,107,111" || liste(b.FrageKritisch) != "88" || liste(b.Bereit) != "110" {
		t.Errorf("kritisch %v, Frage %v, bereit %v; erwartet 102,104,107,111, 88 und 110", b.Kritisch, b.FrageKritisch, b.Bereit)
	}
	if !strings.Contains(b.Karte98, "PR #99") || liste(b.Checks98) != "rot" || !strings.Contains(b.Karte98, "change/netz-retry") {
		t.Errorf("Karte #98 %q Checks %v, erwartet PR #99, change/netz-retry und rote Checks", b.Karte98, b.Checks98)
	}
	if !strings.Contains(b.Karte101, "2/7") || !strings.Contains(b.Karte101, "gate2") {
		t.Errorf("Karte #101 %q, erwartet 2/7 und gate2", b.Karte101)
	}
	if !strings.Contains(b.Kopf, "3 Fragen") {
		t.Errorf("Kopf %q, erwartet 3 Fragen", b.Kopf)
	}
	for abschnitt, nr := range map[string]string{"vor-klick-linie": "nummer=36", "vor-klick-station": "nummer=102"} {
		ds := nur(ab[abschnitt], http.MethodGet, "/api/detail")
		if len(ds) != 1 || !strings.Contains(ds[0].Query, "art=issue") || !strings.Contains(ds[0].Query, nr) {
			t.Errorf("%s: Detail-Anfragen %+v, erwartet art=issue und %s", abschnitt, ds, nr)
		}
	}

	if ziel := os.Getenv("GKP_BELEG_PNG_REPO_KARTE"); ziel != "" {
		if out, err := chromium(ctx, srv.URL+"/?foto=1", "--screenshot="+ziel); err != nil {
			t.Errorf("Bildschirmfoto: %v\n%s", err, out)
		}
	}
}
