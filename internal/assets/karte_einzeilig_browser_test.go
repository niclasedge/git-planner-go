package assets_test

// Browser-Test der einzeiligen Karte und der "Letzten Aktionen"
// (karte-einzeilig, #124): die Karte beginnt eingeklappt als eine Zeile aus
// karte_kopf, auch wenn der alte Schluessel kn-karte-offen "offen" gemerkt hat;
// die P1-Fragen stehen rot, ein Klick auf eine Nummer oeffnet die
// Detailspalte, ein Klick auf die Zeile klappt auf und fragt genau einmal
// /api/karte an. Darunter "Letzte Aktionen": seit letzte-aktionen (#126)
// zeichnet die Oberflaeche letzter_commit nicht mehr; ohne letzte_aktionen
// stehen dort die Eintraege aus zuletzt. Mechanik wie in karte_browser_test.go.
//
// Mit GKP_BELEG_PNG legt der Test ein Bildschirmfoto vom Kopf der
// Arbeitsliste ab (eingeklappte Karte und Letzte Aktionen).

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

const keLetzter = `"letzter_commit":{"branch":"change/ci-runs","standard":false,
  "sha":"3f9c2e1a0b1c2d3e4f5a6b7c8d9e0f1a2b3c4d5e","kurz":"3f9c2e1",
  "betreff":"ci-runs: Log-Ansicht mit Zeilennummern, Suche und Sprung auf den ersten roten Schritt",
  "betreff_kurz":"ci-runs: Log-Ansicht …","autor":"alex","zeit":"2026-09-30T12:32:00Z","art":"gepusht",
  "text":"heute 14:32","voll":"30.09.2026 14:32","commit_voll":"30.09.2026 14:30",
  "titel":"gepusht 30.09.2026 14:32 · Commit 30.09.2026 14:30 · 3f9c2e1a0b1c2d3e4f5a6b7c8d9e0f1a2b3c4d5e",
  "pr":{"nummer":123,"url":"https://github.com/acme/alpha/pull/123"}}`

const keZuletzt = `"zuletzt":[
  {"art":"issue","nummer":122,"titel":"ci-runs: letzte 20 Workflow-Runs je Repo","url":"u122","status":"erledigt",
   "blockiert_durch":[],"labels":[],"zustaendig":[],"hat_text":true,"tiefe":0,"aktiv":false,"branches":[],
   "geschlossen":"2026-09-30T09:58:00Z","geschlossen_text":"heute 11:58","geschlossen_voll":"30.09.2026 11:58",
   "gemergt_mit":{"nummer":123,"titel":"ci-runs (#122)","url":"https://github.com/acme/alpha/pull/123","gemergt_am":"2026-09-30T09:58:00Z"}},
  {"art":"issue","nummer":121,"titel":"karte-gruppen: Issues nach Parent","url":"u121","status":"erledigt",
   "blockiert_durch":[],"labels":[],"zustaendig":[],"hat_text":true,"tiefe":0,"aktiv":false,"branches":[],
   "geschlossen":"2026-09-29T14:35:01Z","geschlossen_text":"29. Sept.","geschlossen_voll":"29.09.2026 16:35"}
 ]`

const keArbeit = `{"repo":"acme/alpha","fortschritt":{"erledigt":0,"gesamt":0},` + keZuletzt + `,` + keLetzter + `,
 "karte_kopf":{"erledigt":10,"aktiv":3,"naechste":10,"fragen":3,"p1":[{"nummer":88,"haelt":4},{"nummer":91,"haelt":2}],"bereit":110},
 "reihe":[{"art":"issue","nummer":110,"titel":"Naechstes","url":"u110","status":"bereit","blockiert_durch":[],
   "labels":[],"zustaendig":[],"hat_text":true,"tiefe":0,"aktiv":false,"branches":[]}],
 "ohne_issue":[],"prs":[],"quellen":[]}`

const keArbeitOhne = `{"repo":"acme/alpha","fortschritt":{"erledigt":0,"gesamt":0},` + keZuletzt + `,
 "karte_kopf":{"erledigt":2,"aktiv":1,"naechste":0,"fragen":0,"p1":[]},
 "reihe":[],"ohne_issue":[],"prs":[],"quellen":[]}`

const keTreiber = `
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
  function text(e) { return e ? e.textContent.replace(/\s+/g, ' ').trim() : ''; }
  function rot(e) {
    var m = e ? getComputedStyle(e).color.match(/\d+/g) : null;
    return !!m && Number(m[0]) > Number(m[1]) + 60 && Number(m[0]) > Number(m[2]) + 60;
  }
  function hinterlegt(e) {
    var bg = e ? getComputedStyle(e).backgroundColor : '';
    return !!bg && bg !== 'transparent' && bg !== 'rgba(0, 0, 0, 0)';
  }
  function berichten(b) {
    b.fehler = (b.fehler || []).concat(fehler);
    var pre = document.createElement('pre');
    pre.id = 'kn-testbericht';
    pre.textContent = btoa(unescape(encodeURIComponent(JSON.stringify(b))));
    document.body.appendChild(pre);
  }
  async function repoOeffnen() {
    // Der alte Schluessel sagt "offen"; er darf nicht mehr gelten.
    localStorage.setItem('kn-karte-offen', JSON.stringify(['acme/alpha']));
    localStorage.removeItem('kn-karte-offen-2');
    await warte('Leiste', function () { return q('[data-repo="acme/alpha"]'); });
    q('[data-repo="acme/alpha"]').click();
    await warte('Karte-Zeile', function () { return q('.kn-karte-kopf') && text(q('.kn-karte-zahl')); });
    await schlaf(300);
  }
  async function lauf() {
    var b = {};
    var art = new URLSearchParams(location.search);
    try {
      if (art.get('foto')) {
        await repoOeffnen();
        await warte('Letzte Aktionen', function () { return q('[data-eintrag="122"]'); });
        window.scrollTo(0, 0);
        return;
      }
      await repoOeffnen();
      var kopf = q('.kn-karte-kopf');
      b.zeile = text(kopf);
      b.hoehe = kopf.getBoundingClientRect().height;
      b.stationenVorher = qa('[data-station]').length;
      b.nummern = qa('[data-kopf-nummer]').map(function (e) { return e.getAttribute('data-kopf-nummer'); });
      b.rot = qa('[data-kopf-nummer]').filter(rot).map(function (e) { return e.getAttribute('data-kopf-nummer'); });
      var lc = q('[data-letzter-commit]');
      b.letzter = text(lc);
      b.letzterTitel = lc ? lc.getAttribute('title') : '';
      b.letzterHinterlegt = hinterlegt(lc);
      b.chip = text(q('[data-letzter-commit] [data-branch-chip]'));
      b.links = qa('[data-letzter-commit] a').map(function (a) { return { text: text(a), href: a.getAttribute('href') || '' }; });
      b.ueberschriften = qa('h2').map(text);
      var abschnitt = qa('section').filter(function (s) { return text(s.querySelector('h2')) === 'Letzte Aktionen'; })[0] || null;
      b.abschnitt = abschnitt ? text(abschnitt.querySelector('h2')) : '';
      b.folge = abschnitt ? Array.prototype.slice.call(abschnitt.querySelectorAll('[data-letzter-commit], [data-eintrag]'))
        .map(function (e) { return e.hasAttribute('data-letzter-commit') ? 'commit' : e.getAttribute('data-eintrag'); }) : [];
      if (art.get('ohne')) { berichten(b); return; }

      await marke('vor-nummer');
      q('[data-kopf-nummer="88"]').click();
      await warte('Detail', function () { return q('.kn-detail'); });
      await schlaf(300);
      b.stationenNachNummer = qa('[data-station]').length;
      await marke('vor-aufklappen');
      q('.kn-karte-kopf').click();
      await warte('Stationen', function () { return qa('[data-station]').length >= 20; });
      await schlaf(300);
      b.stationen = qa('[data-station]').length;
      b.gemerkt = localStorage.getItem('kn-karte-offen-2');
      b.zeileOffen = text(q('.kn-karte-kopf'));
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

type keBericht struct {
	Abbruch, DOM             string
	Fehler                   []string
	Zeile, ZeileOffen        string
	Hoehe                    float64
	StationenVorher          int
	StationenNachNummer      int
	Stationen                int
	Nummern, Rot             []string
	Letzter, LetzterTitel    string
	LetzterHinterlegt        bool
	Chip, Abschnitt, Gemerkt string
	Ueberschriften           []string
	Folge                    []string
	Links                    []struct{ Text, Href string }
}

func TestKarteEinzeiligImBrowser(t *testing.T) {
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
		fmt.Fprint(w, keTreiber)
	})
	mux.HandleFunc("/__marke", func(w http.ResponseWriter, r *http.Request) {
		api.log(anfrage{Marke: r.URL.Query().Get("name")})
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/api/repos", func(w http.ResponseWriter, r *http.Request) { protokolliere(r); json200(w, ivRepos) })
	mux.HandleFunc("/api/arbeit", func(w http.ResponseWriter, r *http.Request) {
		protokolliere(r)
		// Die Seite mit ?ohne=1 bekommt den Kopf ohne Fragen; fetch schickt
		// die Adresse der Seite als Referer mit.
		if strings.Contains(r.Header.Get("Referer"), "ohne=1") {
			json200(w, keArbeitOhne)
			return
		}
		json200(w, keArbeit)
	})
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
			"--window-size=1500,760",
			"--virtual-time-budget=30000",
		}, extra...)
		return exec.CommandContext(ctx, chrome, append(args, adresse)...).Output()
	}
	bericht := func(ctx context.Context, adresse string) keBericht {
		t.Helper()
		out, err := chromium(ctx, adresse, "--dump-dom")
		if err != nil {
			t.Fatalf("chromium (%s): %v\n%s", chrome, err, out)
		}
		m := berichtRe.FindSubmatch(out)
		if m == nil {
			t.Fatalf("kein Testbericht in der Seite. DOM:\n%s", out)
		}
		roh, _ := base64.StdEncoding.DecodeString(string(m[1]))
		var b keBericht
		if err := json.Unmarshal(roh, &b); err != nil {
			t.Fatalf("Bericht kein JSON: %v", err)
		}
		if b.Abbruch != "" {
			t.Fatalf("Treiber abgebrochen: %s\nFehler: %v\nDOM: %s", b.Abbruch, b.Fehler, b.DOM)
		}
		if len(b.Fehler) > 0 {
			t.Errorf("JavaScript-Fehler: %v", b.Fehler)
		}
		return b
	}
	ctx, abbrechen := context.WithTimeout(context.Background(), 150*time.Second)
	defer abbrechen()

	b := bericht(ctx, srv.URL+"/")
	ab := api.abschnitte()
	const zeile = "Karte · 10 erledigt · 3 aktiv · 10 als nächstes · 3 Fragen (2× P1: #88 hält 4, #91 hält 2) · nächste bereit: #110"
	if b.Zeile != zeile {
		t.Errorf("Zeile %q, erwartet %q", b.Zeile, zeile)
	}
	if b.Hoehe <= 0 || b.Hoehe > 34 {
		t.Errorf("Zeile %.0f px hoch, erwartet eine Zeilenhoehe", b.Hoehe)
	}
	if b.StationenVorher != 0 {
		t.Errorf("%d Stationen vor dem Aufklappen, erwartet eingeklappt trotz kn-karte-offen", b.StationenVorher)
	}
	if strings.Join(b.Nummern, ",") != "88,91,110" || strings.Join(b.Rot, ",") != "88,91" {
		t.Errorf("Nummern %v, rot %v; erwartet 88,91,110 und rot 88,91", b.Nummern, b.Rot)
	}
	if n := len(nur(ab[""], http.MethodGet, "/api/karte")); n != 0 {
		t.Errorf("vor dem Aufklappen %d Anfragen an /api/karte, erwartet 0", n)
	}
	ds := nur(ab["vor-nummer"], http.MethodGet, "/api/detail")
	if len(ds) != 1 || !strings.Contains(ds[0].Query, "art=issue") || !strings.Contains(ds[0].Query, "nummer=88") {
		t.Errorf("Klick auf #88: Detail-Anfragen %+v, erwartet art=issue und nummer=88", ds)
	}
	if n := len(nur(ab["vor-nummer"], http.MethodGet, "/api/karte")); n != 0 || b.StationenNachNummer != 0 {
		t.Errorf("Klick auf #88 klappt auf: %d Anfragen an /api/karte, %d Stationen", n, b.StationenNachNummer)
	}
	if n := len(nur(ab["vor-aufklappen"], http.MethodGet, "/api/karte")); n != 1 || b.Stationen != 23 {
		t.Errorf("Aufklappen: %d Anfragen an /api/karte und %d Stationen, erwartet 1 und 23", n, b.Stationen)
	}
	if b.Gemerkt != `["acme/alpha"]` {
		t.Errorf("kn-karte-offen-2 %q, erwartet [\"acme/alpha\"]", b.Gemerkt)
	}
	if b.ZeileOffen != zeile {
		t.Errorf("Zeile aufgeklappt %q, erwartet dieselbe aus kopf der Karte", b.ZeileOffen)
	}

	// letzter_commit steht noch im JSON, wird aber nicht mehr gezeichnet.
	if b.Letzter != "" || len(b.Links) != 0 {
		t.Errorf("Zeile Letzter Commit %q mit Links %+v, erwartet keine", b.Letzter, b.Links)
	}
	if b.Abschnitt != "Letzte Aktionen" || strings.Join(b.Folge, ",") != "122,121" {
		t.Errorf("Abschnitt %q Folge %v, erwartet Letzte Aktionen mit 122,121", b.Abschnitt, b.Folge)
	}
	for _, u := range b.Ueberschriften {
		if u == "Zuletzt erledigt" {
			t.Errorf("Ueberschrift Zuletzt erledigt steht noch da: %v", b.Ueberschriften)
		}
	}

	ohne := bericht(ctx, srv.URL+"/?ohne=1")
	if ohne.Zeile != "Karte · 2 erledigt · 1 aktiv · 0 als nächstes" || len(ohne.Nummern) != 0 {
		t.Errorf("Zeile ohne Fragen %q mit Nummern %v, erwartet %q", ohne.Zeile, ohne.Nummern, "Karte · 2 erledigt · 1 aktiv · 0 als nächstes")
	}
	if ohne.Letzter != "" || ohne.Abschnitt != "Letzte Aktionen" || strings.Join(ohne.Folge, ",") != "122,121" {
		t.Errorf("ohne letzter_commit: Zeile %q, Abschnitt %q, Folge %v; erwartet keine Zeile Letzter Commit und 122,121", ohne.Letzter, ohne.Abschnitt, ohne.Folge)
	}
	var letzteAktionen bool
	for _, u := range ohne.Ueberschriften {
		letzteAktionen = letzteAktionen || u == "Letzte Aktionen"
	}
	if !letzteAktionen {
		t.Errorf("ohne letzter_commit fehlt die Ueberschrift Letzte Aktionen: %v", ohne.Ueberschriften)
	}

	if ziel := os.Getenv("GKP_BELEG_PNG"); ziel != "" {
		if out, err := chromium(ctx, srv.URL+"/?foto=1", "--screenshot="+ziel); err != nil {
			t.Errorf("Bildschirmfoto: %v\n%s", err, out)
		}
	}
}
