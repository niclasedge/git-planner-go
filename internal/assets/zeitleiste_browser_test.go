package assets_test

// Browser-Test der Zeitleiste unter "Letzte Aktionen" und der eingeklappten
// Abhaengigen unter "Als Nächstes" (aktionen-zeitleiste, #128): Tageskoepfe,
// ein Punkt je Zeile, die Commits eines Pushes, die PR-Zeile, "N weitere",
// und das Auf- und Zuklappen der Abhaengigen. Mechanik wie in
// letzte_aktionen_browser_test.go.
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

func zlStrom() string {
	zeilen := []string{
		`{"art":"merge","wort":"Merge","zeit":"2026-09-30T18:58:11Z","text":"heute 20:58","voll":"30.09.2026 20:58","tag":"Heute","uhr":"20:58",
		  "titel":"30.09.2026 20:58 · dc168e304bda1eac18d59b0b8fe7e2154b944999","juengste":true,"branch":"main","standard":true,
		  "sha":"dc168e304bda1eac18d59b0b8fe7e2154b944999","kurz":"dc168e3","betreff":"Merge pull request #125","betreff_kurz":"Merge pull request #125",
		  "autor":"work-account","pr":{"nummer":125,"url":"https://github.com/acme/alpha/pull/125","titel":"letzte-aktionen"}}`,
		`{"art":"push","wort":"2 Pushes","anzahl":2,"zeit":"2026-09-30T18:40:00Z","text":"heute 20:40","voll":"30.09.2026 20:40","tag":"Heute","uhr":"20:40",
		  "titel":"30.09.2026 20:40 · aaaaaa3000000000000000000000000000000000","branch":"claude/x","sha":"aaaaaa3000000000000000000000000000000000",
		  "kurz":"aaaaaa3","betreff":"drei","betreff_kurz":"drei","autor":"alex","commit_zahl":3,
		  "commits":[{"sha":"aaaaaa3000000000000000000000000000000000","kurz":"aaaaaa3","betreff":"drei","autor":"alex","zeit":"2026-09-30T18:39:00Z"},
		             {"sha":"aaaaaa2000000000000000000000000000000000","kurz":"aaaaaa2","betreff":"zwei","autor":"alex","zeit":"2026-09-30T18:30:00Z"}]}`,
		`{"art":"branch_neu","wort":"Branch angelegt","zeit":"2026-09-30T18:30:00Z","text":"heute 20:30","voll":"30.09.2026 20:30","tag":"Heute","uhr":"20:30",
		  "titel":"30.09.2026 20:30 · aaaaaa1000000000000000000000000000000000","branch":"claude/x","sha":"aaaaaa1000000000000000000000000000000000","kurz":"aaaaaa1","autor":"alex"}`,
		`{"art":"pr","wort":"PR gemergt","zeit":"2026-09-30T18:20:00Z","text":"heute 20:20","voll":"30.09.2026 20:20","tag":"Heute","uhr":"20:20",
		  "titel":"30.09.2026 20:20","nummer":118,"pr_titel":"Achtzehn","url":"https://github.com/acme/alpha/pull/118","branch":"change/z","autor":"alex"}`,
		`{"art":"beleg","wort":"Beleg","zeit":"2026-09-30T18:10:00Z","text":"heute 20:10","voll":"30.09.2026 20:10","tag":"Heute","uhr":"20:10",
		  "titel":"30.09.2026 20:10","kennung":"k","name":"0-k.png","pfad":"openspec/changes/k/belege/assets/0-k.png","bild":"/bild/karte.svg",
		  "link":"https://github.com/acme/alpha/blob/claude/x/openspec/changes/k/belege/assets/0-k.png","branch":"claude/x"}`,
	}
	for i := 0; i < 9; i++ {
		zeilen = append(zeilen, fmt.Sprintf(`{"art":"ci","wort":"CI-Run","zeit":"2026-09-29T1%d:00:00Z","text":"29. Sept.","voll":"29.09.2026 1%d:00",
		  "tag":"Gestern","uhr":"1%d:00","titel":"29.09.2026 1%d:00","punkt":"gruen","ergebnis":"success","workflow":"test %d","branch":"main","standard":true,
		  "url":"https://github.com/acme/alpha/actions/runs/%d"}`, 8-i, 8-i, 8-i, 8-i, i, i))
	}
	return `"letzte_aktionen":[` + strings.Join(zeilen, ",") + `]`
}

func zlEintrag(nr, tiefe, unter int, vorfahren, abh string) string {
	s := fmt.Sprintf(`{"art":"issue","nummer":%d,"titel":"Issue %d","url":"u%d","status":"bereit","blockiert_durch":[],
	  "labels":[],"zustaendig":[],"hat_text":false,"tiefe":%d,"aktiv":false,"branches":[]`, nr, nr, nr, tiefe)
	if unter > 0 {
		s += fmt.Sprintf(`,"unter":%d`, unter)
	}
	if vorfahren != "" {
		s += `,"vorfahren":` + vorfahren
	}
	if abh != "" {
		s += `,"abhaengige":` + abh
	}
	return s + "}"
}

func zlArbeit() string {
	reihe := []string{
		zlEintrag(4, 0, 0, "", `{"gesamt":3,"offen":2,"fertig":1,"kinder":1,"text":"3 abhängige Issues · 2 offen · 1 fertig"}`),
		zlEintrag(3, 1, 4, "[4]", `{"gesamt":1,"offen":1,"fertig":0,"kinder":1,"text":"1 abhängiges Issue · 1 offen · 0 fertig"}`),
		zlEintrag(2, 2, 3, "[3,4]", ""),
		zlEintrag(1, 0, 0, "", ""),
		zlEintrag(9, 0, 0, "", `{"gesamt":2,"offen":0,"fertig":2,"kinder":0,"text":"2 abhängige Issues · 0 offen · 2 fertig"}`),
	}
	return `{"repo":"acme/alpha","fortschritt":{"erledigt":0,"gesamt":0},` + keZuletzt + `,` + keLetzter + `,` + zlStrom() + `,
	 "karte_kopf":{"erledigt":1,"aktiv":0,"naechste":5,"fragen":0,"p1":[],"bereit":4},
	 "reihe":[` + strings.Join(reihe, ",") + `],"ohne_issue":[],"prs":[],"quellen":[]}`
}

const zlTreiber = `
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
  function reihe() {
    var h = qa('h2').filter(function (x) { return text(x) === 'Als Nächstes'; })[0];
    var s = h.closest('section');
    return {
      eintraege: Array.prototype.slice.call(s.querySelectorAll('.kn-e[data-eintrag]')).map(function (e) { return e.getAttribute('data-eintrag'); }).join(','),
      zeilen: Array.prototype.slice.call(s.querySelectorAll('[data-abhaengige]')).map(function (e) {
        return { nr: e.getAttribute('data-abhaengige'), text: text(e), knopf: e.tagName === 'BUTTON', offen: e.getAttribute('aria-expanded') || '' };
      }),
    };
  }
  async function lauf() {
    var b = {};
    try {
      await warte('Leiste', function () { return q('[data-repo="acme/alpha"]'); });
      q('[data-repo="acme/alpha"]').click();
      await warte('Zeitleiste', function () { return qa('[data-aktion]').length >= 10; });
      await schlaf(300);
      if (new URLSearchParams(location.search).get('foto')) {
        var s = q('[data-aktion]').closest('section');
        window.scrollTo(0, Math.max(0, s.getBoundingClientRect().top + window.scrollY - 12));
        return;
      }
      b.tage = qa('[data-tag]').map(text);
      var zeilen = qa('[data-aktion]');
      b.anzahlVorher = zeilen.length;
      b.texte = zeilen.map(function (z) { return text(z.querySelector('.kn-la-text')); });
      b.punkte = zeilen.map(function (z) { var p = z.querySelector('[data-punkt-art]'); return p ? p.getAttribute('data-punkt-art') : ''; });
      b.arten = zeilen.map(function (z) { return z.getAttribute('data-aktion'); });
      b.commits = qa('[data-commit]').map(text);
      b.commitsMehr = text(q('[data-commits-mehr]'));
      var pr = q('[data-aktion="pr"] a');
      b.prLink = pr ? { text: text(pr), href: pr.getAttribute('href') } : null;
      var mehr = q('[data-mehr]');
      b.mehr = text(mehr);
      if (mehr) { mehr.click(); await schlaf(200); }
      b.anzahlNachher = qa('[data-aktion]').length;
      b.mehrNachher = qa('[data-mehr]').length;
      b.weniger = qa('[data-weniger]').length;

      b.reihe = [reihe()];
      var klick = async function (nr) {
        q('[data-abhaengige="' + nr + '"]').click();
        await schlaf(200);
        b.reihe.push(reihe());
      };
      await klick('4');
      await klick('3');
      await klick('4');
    } catch (e) {
      b.abbruch = String(e && e.message || e);
      b.dom = document.body.innerHTML.slice(0, 4000);
    }
    berichten(b);
  }
  lauf();
})();
`

type zlReihe struct {
	Eintraege string
	Zeilen    []struct {
		Nr, Text, Offen string
		Knopf           bool
	}
}

type zlBericht struct {
	Abbruch, DOM               string
	Fehler                     []string
	Tage, Texte, Punkte, Arten []string
	AnzahlVorher               int `json:"anzahlVorher"`
	AnzahlNachher              int `json:"anzahlNachher"`
	MehrNachher                int `json:"mehrNachher"`
	Weniger                    int
	Commits                    []string
	CommitsMehr                string                       `json:"commitsMehr"`
	PRLink                     *struct{ Text, Href string } `json:"prLink"`
	Mehr                       string
	Reihe                      []zlReihe
}

func TestZeitleisteUndAbhaengigeImBrowser(t *testing.T) {
	if testing.Short() {
		t.Skip("-short: der Test startet einen Browser")
	}
	chrome := chromiumFinden()
	if chrome == "" {
		t.Skip("kein Chromium gefunden — die Oberflaeche ist damit NICHT belegt")
	}
	arbeit := zlArbeit()
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
		fmt.Fprint(w, zlTreiber)
	})
	mux.HandleFunc("/bild/karte.svg", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/svg+xml")
		fmt.Fprint(w, ltASVG)
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
			"--window-size=1500,900",
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
	var b zlBericht
	if err := json.Unmarshal(roh, &b); err != nil {
		t.Fatalf("Bericht kein JSON: %v", err)
	}
	if b.Abbruch != "" {
		t.Fatalf("Treiber abgebrochen: %s\nFehler: %v\nDOM: %s", b.Abbruch, b.Fehler, b.DOM)
	}
	if len(b.Fehler) > 0 {
		t.Errorf("JavaScript-Fehler: %v", b.Fehler)
	}

	if strings.Join(b.Tage, ",") != "Heute,Gestern" {
		t.Errorf("Tageskoepfe %v, erwartet Heute,Gestern", b.Tage)
	}
	if len(b.Texte) == 0 || !strings.HasPrefix(b.Texte[0], "20:58 · Merge") {
		t.Errorf("erste Zeile %v, erwartet Beginn 20:58 · Merge", b.Texte)
	}
	for i, p := range b.Punkte {
		if p != b.Arten[i] {
			t.Errorf("Zeile %d (%s) mit Punkt %q, erwartet data-punkt-art gleich der Art", i, b.Arten[i], p)
		}
	}
	if strings.Join(b.Commits, "|") != "aaaaaa3 drei · alex|aaaaaa2 zwei · alex" || b.CommitsMehr != "+ 1 weiterer Commit" {
		t.Errorf("Commits %v und %q, erwartet aaaaaa3 drei · alex, aaaaaa2 zwei · alex und + 1 weiterer Commit", b.Commits, b.CommitsMehr)
	}
	prZeile := ""
	for i, a := range b.Arten {
		if a == "pr" {
			prZeile = b.Texte[i]
		}
	}
	if prZeile != "20:20 · PR gemergt #118 Achtzehn auf change/z" {
		t.Errorf("PR-Zeile %q, erwartet 20:20 · PR gemergt #118 Achtzehn auf change/z", prZeile)
	}
	if b.PRLink == nil || b.PRLink.Text != "#118" || b.PRLink.Href != "https://github.com/acme/alpha/pull/118" {
		t.Errorf("PR-Link %+v, erwartet #118 auf den PR", b.PRLink)
	}
	// Seit aktionen-24h (#132) zuerst zehn, aufgeklappt mit "weniger".
	if b.AnzahlVorher != 10 || b.Mehr != "4 weitere aus 24 Std." || b.AnzahlNachher != 14 || b.MehrNachher != 0 || b.Weniger != 1 {
		t.Errorf("sichtbar %d mit Knopf %q, danach %d mit %d Knoepfen und %d weniger; erwartet 10, 4 weitere aus 24 Std., 14, 0, 1", b.AnzahlVorher, b.Mehr, b.AnzahlNachher, b.MehrNachher, b.Weniger)
	}

	want := []string{"4,1,9", "4,3,1,9", "4,3,2,1,9", "4,1,9"}
	if len(b.Reihe) != 4 {
		t.Fatalf("%d Staende der Reihe, erwartet 4: %+v", len(b.Reihe), b.Reihe)
	}
	for i, w := range want {
		if b.Reihe[i].Eintraege != w {
			t.Errorf("Stand %d: Eintraege %s, erwartet %s", i, b.Reihe[i].Eintraege, w)
		}
	}
	zeile := func(r zlReihe, nr string) (string, bool, string) {
		for _, z := range r.Zeilen {
			if z.Nr == nr {
				return z.Text, z.Knopf, z.Offen
			}
		}
		return "", false, ""
	}
	if txt, knopf, offen := zeile(b.Reihe[0], "4"); txt != "▸ 3 abhängige Issues · 2 offen · 1 fertig" || !knopf || offen != "false" {
		t.Errorf("Zeile unter #4 %q knopf %v offen %q", txt, knopf, offen)
	}
	if txt, knopf, _ := zeile(b.Reihe[0], "9"); txt != "2 abhängige Issues · 0 offen · 2 fertig" || knopf {
		t.Errorf("Zeile unter #9 %q knopf %v, erwartet ohne Knopf", txt, knopf)
	}
	if txt, _, offen := zeile(b.Reihe[1], "4"); !strings.HasPrefix(txt, "▾ ") || offen != "true" {
		t.Errorf("Zeile unter #4 nach dem Klick %q offen %q, erwartet ▾ und aria-expanded true", txt, offen)
	}
	if len(b.Reihe[0].Zeilen) != 2 {
		t.Errorf("eingeklappt %d Zeilen, erwartet 2 (die unter #3 ist mit #3 verborgen): %+v", len(b.Reihe[0].Zeilen), b.Reihe[0].Zeilen)
	}

	if ziel := os.Getenv("GKP_BELEG_PNG"); ziel != "" {
		if out, err := chromium(ctx, srv.URL+"/?foto=1", "--screenshot="+ziel); err != nil {
			t.Errorf("Bildschirmfoto: %v\n%s", err, out)
		}
	}
}
