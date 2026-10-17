package assets_test

// Browser-Test von "Letzte Aktionen" als Zeitstrom (letzte-aktionen, #126):
// sechs Zeilen gemischter Arten in der Folge von letzte_aktionen, die
// juengste farbig hinterlegt, Branch als Chip, "→ PR #N" als Link, die
// HTML-Zeile als Link in einen neuen Tab, die Bildzeile oeffnet die
// Bildansicht, Esc schliesst sie. Dazu zeigen die HTML-Dateien ihre
// Zeittexte aus Go. Mechanik wie in karte_einzeilig_browser_test.go.
//
// Mit GKP_BELEG_PNG legt der Test ein Bildschirmfoto vom Zeitstrom ab.

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

const ltAStrom = `"letzte_aktionen":[
  {"art":"merge","wort":"Merge","zeit":"2026-09-30T18:58:11Z","text":"heute 20:58","voll":"30.09.2026 20:58",
   "titel":"30.09.2026 20:58 · dc168e304bda1eac18d59b0b8fe7e2154b944999","juengste":true,
   "branch":"main","standard":true,"sha":"dc168e304bda1eac18d59b0b8fe7e2154b944999","kurz":"dc168e3",
   "betreff":"Merge pull request #125 from example-org/claude/upbeat-planck-cxrti4",
   "betreff_kurz":"Merge pull request #125 from example-org/claude/upbeat-…","autor":"work-account",
   "pr":{"nummer":125,"url":"https://github.com/acme/alpha/pull/125"}},
  {"art":"push","wort":"2 Pushes","anzahl":2,"zeit":"2026-09-30T18:40:00Z","text":"heute 20:40","voll":"30.09.2026 20:40",
   "titel":"30.09.2026 20:40 · aaaaaa2000000000000000000000000000000000","juengste":false,
   "branch":"change/x","standard":false,"sha":"aaaaaa2000000000000000000000000000000000","kurz":"aaaaaa2",
   "betreff":"x: zweiter Schritt","betreff_kurz":"x: zweiter Schritt","autor":"alex",
   "pr":{"nummer":7,"url":"https://github.com/acme/alpha/pull/7"}},
  {"art":"ci","wort":"CI-Run","zeit":"2026-09-30T18:10:00Z","text":"heute 20:10","voll":"30.09.2026 20:10","titel":"30.09.2026 20:10",
   "punkt":"gruen","ergebnis":"success","workflow":"test","branch":"main","standard":true,"url":"https://github.com/acme/alpha/actions/runs/1"},
  {"art":"bild","wort":"Bild","zeit":"2026-09-30T18:00:00Z","text":"heute 20:00","voll":"30.09.2026 20:00","titel":"30.09.2026 20:00",
   "name":"karte.png","pfad":"docs/karte.png","bild":"/bild/karte.svg","link":"https://github.com/acme/alpha/blob/main/docs/karte.png","betreff":"Karte neu"},
  {"art":"html","wort":"HTML","zeit":"2026-09-30T17:50:00Z","text":"heute 19:50","voll":"30.09.2026 19:50","titel":"30.09.2026 19:50",
   "name":"bericht.html","pfad":"bericht.html","adresse":"/api/datei/acme/alpha/abc/bericht.html","betreff":"Bericht"},
  {"art":"issue","wort":"Issue geschlossen","zeit":"2026-09-30T17:30:00Z","text":"heute 19:30","voll":"30.09.2026 19:30","titel":"30.09.2026 19:30",
   "nummer":120,"issue_titel":"Hundertzwanzig","url":"https://github.com/acme/alpha/issues/120",
   "gemergt_mit":{"nummer":121,"titel":"hundertzwanzig (#120)","url":"https://github.com/acme/alpha/pull/121","gemergt_am":"2026-09-30T17:30:00Z"}}
 ]`

const ltAArbeit = `{"repo":"acme/alpha","fortschritt":{"erledigt":0,"gesamt":0},` + keZuletzt + `,` + keLetzter + `,` + ltAStrom + `,
 "karte_kopf":{"erledigt":10,"aktiv":3,"naechste":10,"fragen":0,"p1":[],"bereit":110},
 "reihe":[{"art":"issue","nummer":110,"titel":"Naechstes","url":"u110","status":"bereit","blockiert_durch":[],
   "labels":[],"zustaendig":[],"hat_text":true,"tiefe":0,"aktiv":false,"branches":[]}],
 "ohne_issue":[],"prs":[],"quellen":[]}`

const ltAHTML = `{"repo":"acme/alpha","ref":"main","dateien":[{"pfad":"bericht.html","name":"bericht.html","sha":"b1","zeit":"2026-09-30T17:50:00Z",
  "commit":"abc","betreff":"Bericht","erstellt":"2026-09-29T08:00:00Z","erstellt_text":"29. Sept.","erstellt_voll":"29.09.2026 10:00",
  "geaendert_text":"heute 19:50","geaendert_voll":"30.09.2026 19:50",
  "adresse":"/api/datei/acme/alpha/abc/bericht.html","link":"l","bilder":{"gesamt":0,"vorhanden":0,"fehlt":0,"extern":0,"eingebettet":0,"fehlende":[],"text":"keine Bilder"}}]}`

const ltASVG = `<svg xmlns="http://www.w3.org/2000/svg" width="400" height="240"><rect width="400" height="240" fill="#4a7"/></svg>`

const ltATreiber = `
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
    await warte('Leiste', function () { return q('[data-repo="acme/alpha"]'); });
    q('[data-repo="acme/alpha"]').click();
    await warte('Zeitstrom', function () { return qa('[data-aktion]').length >= 6; });
    await schlaf(300);
  }
  async function lauf() {
    var b = {};
    try {
      await repoOeffnen();
      if (new URLSearchParams(location.search).get('foto')) {
        var s = q('[data-aktion]').closest('section');
        window.scrollTo(0, Math.max(0, s.getBoundingClientRect().top + window.scrollY - 12));
        return;
      }
      var zeilen = qa('[data-aktion]');
      var abschnitt = zeilen[0].closest('section');
      b.abschnitt = text(abschnitt.querySelector('h2'));
      b.arten = zeilen.map(function (z) { return z.getAttribute('data-aktion'); });
      b.texte = zeilen.map(function (z) { return text(z.querySelector('.kn-la-text')); });
      b.titel = zeilen.map(function (z) { return z.getAttribute('title') || ''; });
      b.symbole = zeilen.map(function (z) { return text(z.querySelector('.kn-la-symbol')); });
      b.hinterlegt = zeilen.map(hinterlegt);
      b.juengste = zeilen.map(function (z) { return z.hasAttribute('data-juengste'); });
      b.chips = qa('[data-aktion] [data-branch-chip]').map(text);
      b.links = qa('[data-aktion] a').map(function (a) { return { text: text(a), href: a.getAttribute('href') || '', ziel: a.getAttribute('target') || '' }; });
      b.punkt = (q('[data-aktion="ci"] [data-punkt]') || { getAttribute: function () { return ''; } }).getAttribute('data-punkt');
      b.letzterCommit = qa('[data-letzter-commit]').length;
      // Eintraege aus zuletzt neben dem Strom; die Issue-Zeile selbst traegt
      // data-eintrag fuer das Kaestchen und zaehlt nicht.
      b.eintraegeImAbschnitt = Array.prototype.slice.call(abschnitt.querySelectorAll('[data-eintrag]'))
        .filter(function (e) { return !e.hasAttribute('data-aktion'); }).length;
      var kasten = q('[data-aktion="issue"][data-eintrag="120"] .kn-e-check');
      b.issueKasten = kasten ? kasten.getAttribute('aria-checked') || '' : '';
      var g = q('.kn-html-geaendert');
      b.htmlGeaendert = text(g);
      b.htmlGeaendertTitel = g ? g.getAttribute('title') || '' : '';
      b.htmlErstellt = text(q('.kn-html-erstellt'));

      q('[data-aktion="bild"] [data-bild-oeffnen]').click();
      await warte('Bildansicht', function () { return q('.kn-modal img'); });
      b.modalBild = q('.kn-modal img').getAttribute('src');
      b.modalTitel = text(q('.kn-modal-titel'));
      document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
      await schlaf(200);
      b.modalNachEsc = qa('.kn-modal').length;
    } catch (e) {
      b.abbruch = String(e && e.message || e);
      b.dom = document.body.innerHTML.slice(0, 3000);
    }
    berichten(b);
  }
  lauf();
})();
`

type ltABericht struct {
	Abbruch, DOM                 string
	Fehler                       []string
	Abschnitt                    string
	Arten, Texte, Titel, Symbole []string
	Hinterlegt, Juengste         []bool
	Chips                        []string
	Links                        []struct{ Text, Href, Ziel string }
	Punkt                        string
	LetzterCommit                int
	EintraegeImAbschnitt         int
	HTMLGeaendert                string `json:"htmlGeaendert"`
	HTMLGeaendertTitel           string `json:"htmlGeaendertTitel"`
	HTMLErstellt                 string `json:"htmlErstellt"`
	ModalBild, ModalTitel        string
	ModalNachEsc                 int
	IssueKasten                  string
}

func TestLetzteAktionenImBrowser(t *testing.T) {
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
		fmt.Fprint(w, ltATreiber)
	})
	mux.HandleFunc("/bild/karte.svg", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/svg+xml")
		fmt.Fprint(w, ltASVG)
	})
	mux.HandleFunc("/api/repos", func(w http.ResponseWriter, r *http.Request) { protokolliere(r); json200(w, ivRepos) })
	mux.HandleFunc("/api/arbeit", func(w http.ResponseWriter, r *http.Request) { protokolliere(r); json200(w, ltAArbeit) })
	mux.HandleFunc("/api/repo-meta", func(w http.ResponseWriter, r *http.Request) { protokolliere(r); json200(w, ivMeta) })
	mux.HandleFunc("/api/html-dateien", func(w http.ResponseWriter, r *http.Request) { protokolliere(r); json200(w, ltAHTML) })
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
	var b ltABericht
	if err := json.Unmarshal(roh, &b); err != nil {
		t.Fatalf("Bericht kein JSON: %v", err)
	}
	if b.Abbruch != "" {
		t.Fatalf("Treiber abgebrochen: %s\nFehler: %v\nDOM: %s", b.Abbruch, b.Fehler, b.DOM)
	}
	if len(b.Fehler) > 0 {
		t.Errorf("JavaScript-Fehler: %v", b.Fehler)
	}

	if b.Abschnitt != "Letzte Aktionen" {
		t.Errorf("Abschnitt %q, erwartet Letzte Aktionen", b.Abschnitt)
	}
	if strings.Join(b.Arten, ",") != "merge,push,ci,bild,html,issue" {
		t.Errorf("Arten %v, erwartet merge,push,ci,bild,html,issue", b.Arten)
	}
	want := []string{
		"heute 20:58 · Merge auf main · „Merge pull request #125 from example-org/claude/upbeat-…“ · dc168e3 · work-account → PR #125",
		"heute 20:40 · 2 Pushes auf change/x · „x: zweiter Schritt“ · aaaaaa2 · alex → PR #7",
		"heute 20:10 · CI-Run test auf main",
		"heute 20:00 · Bild karte.png",
		"heute 19:50 · HTML bericht.html",
		"heute 19:30 · Issue geschlossen #120 Hundertzwanzig · gemergt mit #121",
	}
	if strings.Join(b.Texte, "\n") != strings.Join(want, "\n") {
		t.Errorf("Zeilen\n%s\nerwartet\n%s", strings.Join(b.Texte, "\n"), strings.Join(want, "\n"))
	}
	wantTitel := []string{"30.09.2026 20:58 · dc168e304bda1eac18d59b0b8fe7e2154b944999", "30.09.2026 20:40 · aaaaaa2000000000000000000000000000000000",
		"30.09.2026 20:10", "30.09.2026 20:00", "30.09.2026 19:50", "30.09.2026 19:30"}
	if strings.Join(b.Titel, "|") != strings.Join(wantTitel, "|") {
		t.Errorf("Tooltips %v, erwartet %v", b.Titel, wantTitel)
	}
	for i, s := range b.Symbole {
		if s == "" {
			t.Errorf("Zeile %d (%s) ohne Symbol", i, b.Arten[i])
		}
	}
	if len(b.Hinterlegt) != 6 || !b.Hinterlegt[0] || !b.Juengste[0] {
		t.Errorf("erste Zeile hinterlegt %v, juengste %v; erwartet beides", b.Hinterlegt, b.Juengste)
	}
	for i := 1; i < len(b.Hinterlegt); i++ {
		if b.Hinterlegt[i] || b.Juengste[i] {
			t.Errorf("Zeile %d hinterlegt %v juengste %v, erwartet nur die erste", i, b.Hinterlegt[i], b.Juengste[i])
		}
	}
	if strings.Join(b.Chips, ",") != "main,change/x,main" {
		t.Errorf("Chips %v, erwartet main,change/x,main", b.Chips)
	}
	links := map[string]struct{ Href, Ziel string }{}
	for _, l := range b.Links {
		links[l.Text] = struct{ Href, Ziel string }{l.Href, l.Ziel}
	}
	if links["#125"].Href != "https://github.com/acme/alpha/pull/125" || links["#7"].Href != "https://github.com/acme/alpha/pull/7" ||
		links["#121"].Href != "https://github.com/acme/alpha/pull/121" {
		t.Errorf("Links %+v, erwartet #125, #7 und #121 auf die PRs", b.Links)
	}
	if h := links["bericht.html"]; h.Href != "/api/datei/acme/alpha/abc/bericht.html" || h.Ziel != "_blank" {
		t.Errorf("HTML-Zeile %+v, erwartet Link auf die Adresse mit target _blank", h)
	}
	if b.Punkt != "gruen" {
		t.Errorf("CI-Punkt %q, erwartet gruen", b.Punkt)
	}
	if b.LetzterCommit != 0 || b.EintraegeImAbschnitt != 0 {
		t.Errorf("%d Zeilen Letzter Commit und %d Eintraege aus zuletzt im Abschnitt, erwartet 0 und 0 (der Strom ersetzt beide)", b.LetzterCommit, b.EintraegeImAbschnitt)
	}
	if b.IssueKasten != "true" {
		t.Errorf("Issue-Zeile #120 mit Kaestchen aria-checked=%q, erwartet ein abgehaktes Kaestchen (Wieder oeffnen)", b.IssueKasten)
	}
	if b.HTMLGeaendert != "geändert heute 19:50" || b.HTMLGeaendertTitel != "30.09.2026 19:50" || b.HTMLErstellt != "erstellt 29. Sept." {
		t.Errorf("HTML-Dateien: %q (%q), %q; erwartet geändert heute 19:50 mit 30.09.2026 19:50 und erstellt 29. Sept.", b.HTMLGeaendert, b.HTMLGeaendertTitel, b.HTMLErstellt)
	}
	if b.ModalBild != "/bild/karte.svg" || !strings.Contains(b.ModalTitel, "karte.png") {
		t.Errorf("Bildansicht mit %q (%q), erwartet /bild/karte.svg mit karte.png", b.ModalBild, b.ModalTitel)
	}
	if b.ModalNachEsc != 0 {
		t.Errorf("Bildansicht nach Esc noch offen")
	}

	if ziel := os.Getenv("GKP_BELEG_PNG"); ziel != "" {
		if out, err := chromium(ctx, srv.URL+"/?foto=1", "--screenshot="+ziel); err != nil {
			t.Errorf("Bildschirmfoto: %v\n%s", err, out)
		}
	}
}
