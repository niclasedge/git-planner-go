package assets_test

// Browser-Test des Abschnitts CI (ci-runs, #122): Kopf mit Punkt und
// "n von m grün", je Run die Punktreihe, ein aufgeklappter Run mit Jobs und
// Schritten links und dem Log rechts, der erste rote Job und Schritt schon
// gewaehlt, der Sprung auf einen Schritt, Zeitstempel per Schalter, die rote
// Fehlerzeile, die Suche, Markup im Log als Text, der Neustart und der Punkt
// in der Leiste. Punkte, Folge, Wahl und Grenzen kommen fertig aus Go
// (appctx.CIAnsichtBauen, appctx.CILogAufbereiten).
//
// Mit GKP_BELEG_PNG_CI_RUNS und GKP_BELEG_PNG_CI_LOG legt der Test
// Bildschirmfotos der Liste und des aufgeklappten Runs ab.

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

	"github.com/niclasedge/git-planner-go/internal/appctx"
	"github.com/niclasedge/git-planner-go/internal/assets"
	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

const ciRepos = `{"kategorien":[{"name":"Arbeit","ohne":false,"eingeklappt":false,"repos":[
 {"nwo":"acme/alpha","name":"alpha","owner":"acme","offene_issues":1,"pat":"GITHUB_PAT_WORK","ci":"rot"},
 {"nwo":"acme/beta","name":"beta","owner":"acme","offene_issues":0,"pat":"GITHUB_PAT_WORK"}]}],"konten":[],"stand":"x"}`

const ciArbeit = `{"repo":"acme/alpha","fortschritt":{"erledigt":0,"gesamt":0},"zuletzt":[],"reihe":[],"ohne_issue":[],"prs":[],"quellen":[]}`

func ciSchritteTest() []ghrest.ActionsSchritt {
	s := func(n int, name, ergebnis string, von, bis int) ghrest.ActionsSchritt {
		basis := time.Now().Add(-10 * time.Minute)
		st := ghrest.ActionsSchritt{Nummer: n, Name: name, Status: "completed", Ergebnis: ergebnis}
		if von >= 0 {
			st.Start = basis.Add(time.Duration(von) * time.Second).UTC().Format(time.RFC3339)
			st.Ende = basis.Add(time.Duration(bis) * time.Second).UTC().Format(time.RFC3339)
		}
		return st
	}
	return []ghrest.ActionsSchritt{
		s(1, "Set up job", "success", 0, 3),
		s(2, "Run actions/checkout@v4", "success", 3, 7),
		s(3, "Set up Go", "success", 7, 18),
		s(4, "Run go vet ./...", "success", 18, 39),
		s(5, "Run go test ./...", "failure", 39, 111),
		s(6, "Upload coverage", "skipped", -1, -1),
		s(7, "Post Run actions/checkout@v4", "success", 111, 112),
		s(8, "Complete job", "success", 112, 112),
	}
}

func ciJobsTest() map[int64][]ghrest.ActionsJob {
	z := func(d time.Duration) string { return time.Now().Add(-d).UTC().Format(time.RFC3339) }
	return map[int64][]ghrest.ActionsJob{
		12: {
			{ID: 500, RunID: 12, Name: "lint", Status: "completed", Ergebnis: "success", Start: z(10 * time.Minute), Ende: z(9 * time.Minute),
				URL: "https://github.com/acme/alpha/actions/runs/12/job/500", Schritte: []ghrest.ActionsSchritt{
					{Nummer: 1, Name: "Set up job", Status: "completed", Ergebnis: "success"},
					{Nummer: 2, Name: "Run golangci-lint", Status: "completed", Ergebnis: "success"},
				}},
			{ID: 501, RunID: 12, Name: "test", Status: "completed", Ergebnis: "failure", Start: z(10 * time.Minute), Ende: z(8 * time.Minute),
				URL: "https://github.com/acme/alpha/actions/runs/12/job/501", Schritte: ciSchritteTest()},
		},
		11: {{ID: 400, RunID: 11, Name: "deploy", Status: "completed", Ergebnis: "success", Schritte: []ghrest.ActionsSchritt{
			{Nummer: 1, Name: "Set up job", Status: "completed", Ergebnis: "success"},
			{Nummer: 2, Name: "Run ./deploy.sh", Status: "completed", Ergebnis: "success"},
			{Nummer: 3, Name: "Notify", Status: "completed", Ergebnis: "skipped"},
		}}},
		10: {{ID: 300, RunID: 10, Name: "test", Status: "in_progress", Schritte: []ghrest.ActionsSchritt{
			{Nummer: 1, Name: "Set up job", Status: "completed", Ergebnis: "success"},
			{Nummer: 2, Name: "Run go test ./...", Status: "in_progress"},
			{Nummer: 3, Name: "Complete job", Status: "queued"},
		}}},
	}
}

func ciAnsichtJSON(t *testing.T) string {
	t.Helper()
	z := func(d time.Duration) string { return time.Now().Add(-d).UTC().Format(time.RFC3339) }
	runs := []ghrest.WorkflowRun{
		{ID: 12, Name: "CI", Event: "push", Branch: "main", HeadSHA: "3f9c2e1a7b8c9d0e", Betreff: "ci-runs: Runs je Repo mit Schritt-Punkten (#122)",
			Nummer: 12, Versuch: 1, Status: "completed", Ergebnis: "failure", Start: z(10 * time.Minute), Aktualisiert: z(8 * time.Minute),
			URL: "https://github.com/acme/alpha/actions/runs/12"},
		{ID: 11, Name: "Deploy", Event: "workflow_dispatch", Branch: "main", HeadSHA: "bbbbbbb2222222", Betreff: "karte-gruppen: Issues nach Parent (#121)",
			Nummer: 11, Versuch: 1, Status: "completed", Ergebnis: "success", Start: z(3 * time.Hour), Aktualisiert: z(3*time.Hour - 45*time.Second),
			URL: "https://github.com/acme/alpha/actions/runs/11"},
		{ID: 10, Name: "CI", Event: "pull_request", Branch: "change/ci-runs", HeadSHA: "ccccccc3333333", Betreff: "ci-runs: Log-Ansicht",
			Nummer: 10, Versuch: 1, Status: "in_progress", Start: z(26 * time.Hour), Aktualisiert: z(26 * time.Hour),
			URL: "https://github.com/acme/alpha/actions/runs/10"},
	}
	a := appctx.CIAnsichtBauen("acme/alpha", runs, ciJobsTest(), time.Now())
	a.Sync = &appctx.SyncStand{Zeit: time.Now().UTC().Format(time.RFC3339)}
	b, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func ciLogJSON(t *testing.T, job int64) string {
	t.Helper()
	var l appctx.CILog
	switch job {
	case 501:
		roh, err := os.ReadFile("../appctx/testdata/ci_job_log.txt")
		if err != nil {
			t.Fatal(err)
		}
		l = appctx.CILogAufbereiten(string(roh), ciJobsTest()[12][1], false)
	default:
		l = appctx.CILogAufbereiten("lint ok\nfertig\n", ciJobsTest()[12][0], false)
	}
	b, err := json.Marshal(l)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

const ciTreiber = `
(function () {
  var fehler = [];
  window.addEventListener('error', function (e) { fehler.push(String(e.message)); });
  window.addEventListener('unhandledrejection', function (e) { fehler.push('rejection: ' + String(e.reason)); });
  window.alert = function (x) { window.boese = 'alert:' + x; };
  function schlaf(ms) { return new Promise(function (r) { setTimeout(r, ms); }); }
  async function warte(was, pred) {
    for (var i = 0; i < 200; i++) { try { if (pred()) return; } catch (e) {} await schlaf(25); }
    throw new Error('Zeitueberschreitung: ' + was);
  }
  function marke(name) { return fetch('/__marke?name=' + encodeURIComponent(name)); }
  function q(sel) { return document.querySelector(sel); }
  function qa(sel) { return Array.prototype.slice.call(document.querySelectorAll(sel)); }
  function text(x) { return x ? x.textContent.replace(/\s+/g, ' ').trim() : null; }
  function berichten(b) {
    b.fehler = (b.fehler || []).concat(fehler);
    var pre = document.createElement('pre');
    pre.id = 'kn-testbericht';
    pre.textContent = btoa(unescape(encodeURIComponent(JSON.stringify(b))));
    document.body.appendChild(pre);
  }
  function sichtbar(zeile) {
    var box = q('.kn-log-zeilen');
    if (!zeile || !box) return false;
    var a = zeile.getBoundingClientRect(), b = box.getBoundingClientRect();
    return a.top >= b.top - 1 && a.bottom <= b.bottom + 1;
  }
  function ziel() { var z = q('.kn-log-zeile[data-ziel="ja"]'); return z ? Number(z.getAttribute('data-nr')) : 0; }
  async function oeffnen() {
    await warte('Leiste', function () { return q('[data-repo="acme/alpha"]'); });
    q('[data-repo="acme/alpha"]').click();
    await warte('Kopf CI', function () { return text(q('.kn-ci-zahl')) === '1 von 3 grün'; });
    q('.kn-ci-kopf').click();
    await warte('Runs', function () { return qa('.kn-ci-run').length === 3; });
  }
  async function runOeffnen() {
    q('.kn-ci-run[data-run="12"] .kn-ci-run-zeile').click();
    await warte('Log', function () { return qa('.kn-log-zeile').length === 62 && ziel() === 48; });
    await schlaf(150);
  }
  async function lauf() {
    var b = {};
    try {
      var foto = new URLSearchParams(location.search).get('foto');
      if (foto) {
        await oeffnen();
        if (foto === 'log') {
          await runOeffnen();
          q('.kn-ci-run[data-run="12"]').scrollIntoView({ block: 'start' });
        } else {
          q('.kn-ci').scrollIntoView({ block: 'start' });
        }
        await schlaf(150);
        return;
      }
      await warte('Leiste', function () { return q('[data-repo="acme/alpha"] .kn-r-ci'); });
      b.leiste = qa('[data-repo]').map(function (r) {
        var p = r.querySelector('.kn-r-ci');
        return { repo: r.getAttribute('data-repo'), ci: p ? p.getAttribute('data-ci') : '' };
      });
      await marke('vor-oeffnen');
      await oeffnen();
      b.kopf = { punkt: q('.kn-ci-kopf .kn-ci-punkt').getAttribute('data-punkt'), text: text(q('.kn-ci-zahl')),
                 offen: q('.kn-ci-kopf').getAttribute('aria-expanded') };
      b.runs = qa('.kn-ci-run').map(function (r) {
        return { run: r.getAttribute('data-run'), punkt: r.getAttribute('data-punkt'), text: text(r),
                 neu: !!r.querySelector('.kn-ci-neu'),
                 reihe: Array.prototype.slice.call(r.querySelectorAll('.kn-ci-reihe .kn-ci-p')).map(function (p) {
                   return { punkt: p.getAttribute('data-punkt'), titel: p.getAttribute('title') };
                 }) };
      });

      await marke('vor-run');
      await runOeffnen();
      var jobs = q('.kn-ci-offen .kn-ci-jobs'), log = q('.kn-ci-offen .kn-ci-log');
      b.offen = {
        job: (q('.kn-ci-job[data-gewaehlt="ja"]') || { getAttribute: function () { return ''; } }).getAttribute('data-job'),
        schritt: (q('.kn-ci-schritt[data-gewaehlt="ja"]') || { getAttribute: function () { return ''; } }).getAttribute('data-schritt'),
        schritte: qa('.kn-ci-job[data-job="501"] .kn-ci-schritt').map(function (s) {
          return { nr: s.getAttribute('data-schritt'), punkt: s.getAttribute('data-punkt'), text: text(s) };
        }),
        ziel: ziel(), sichtbar: sichtbar(q('.kn-log-zeile[data-ziel="ja"]')), scroll: q('.kn-log-zeilen').scrollTop,
        links: jobs && log ? jobs.getBoundingClientRect().right <= log.getBoundingClientRect().left + 1 : false,
        zeiten: qa('.kn-log-zeit').length,
        nummer: text(q('.kn-log-zeile[data-nr="48"] .kn-log-nr')),
        schrift: getComputedStyle(q('.kn-log-zeile .kn-log-text')).fontFamily,
        fehler: qa('.kn-log-zeile.kn-log-fehler').map(function (z) { return z.getAttribute('data-nr'); }),
        fehlerFarbe: getComputedStyle(q('.kn-log-zeile.kn-log-fehler .kn-log-text')).color,
        normalFarbe: getComputedStyle(q('.kn-log-zeile[data-nr="47"] .kn-log-text')).color,
        markup: text(q('.kn-log-zeile[data-nr="54"] .kn-log-text')),
        elemente: log ? log.querySelectorAll('img, script, b').length : -1,
        github: (q('.kn-log-github') || { getAttribute: function () { return ''; } }).getAttribute('href'),
        text: text(q('.kn-log-zeile[data-nr="45"] .kn-log-text')),
      };

      q('.kn-ci-job[data-job="501"] .kn-ci-schritt[data-schritt="3"]').click();
      await warte('Sprung', function () { return ziel() === 36; });
      await schlaf(100);
      b.sprung = { ziel: ziel(), sichtbar: sichtbar(q('.kn-log-zeile[data-ziel="ja"]')),
                   schritt: q('.kn-ci-schritt[data-gewaehlt="ja"]').getAttribute('data-schritt') };

      q('.kn-log-zeiten').click();
      await warte('Zeitstempel', function () { return qa('.kn-log-zeit').length === 62; });
      b.zeit = text(q('.kn-log-zeile[data-nr="1"] .kn-log-zeit'));

      var suche = q('.kn-log-suche');
      suche.value = 'fail';
      suche.dispatchEvent(new Event('input', { bubbles: true }));
      await warte('Treffer', function () { return qa('.kn-log-zeile[data-treffer="ja"]').length === 4; });
      b.treffer = { zahl: text(q('.kn-log-trefferzahl')),
                    zeilen: qa('.kn-log-zeile[data-treffer="ja"]').map(function (z) { return z.getAttribute('data-nr'); }) };

      await marke('vor-job');
      q('.kn-ci-job[data-job="500"] .kn-ci-job-name').click();
      await warte('Log von lint', function () { return qa('.kn-log-zeile').length === 2; });
      b.lint = { job: q('.kn-ci-job[data-gewaehlt="ja"]').getAttribute('data-job'),
                 ganz: !!q('.kn-log-ganz') };

      await marke('vor-neustart');
      q('.kn-ci-run[data-run="12"] .kn-ci-neu').click();
      await schlaf(600);
      await marke('ende');
      b.boese = window.boese || '';
    } catch (e) {
      b.abbruch = String(e && e.message || e);
      b.dom = document.body.innerHTML.slice(0, 4000);
    }
    berichten(b);
  }
  lauf();
})();
`

type ciPunktBericht struct {
	Punkt string `json:"punkt"`
	Titel string `json:"titel"`
}

func TestCIRunsImBrowser(t *testing.T) {
	if testing.Short() {
		t.Skip("-short: der Test startet einen Browser")
	}
	chrome := chromiumFinden()
	if chrome == "" {
		t.Skip("kein Chromium gefunden — die Oberflaeche ist damit NICHT belegt")
	}
	ansicht := ciAnsichtJSON(t)
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
		if r.URL.Path != "/" {
			w.WriteHeader(http.StatusNotFound)
			json200(w, `{"grund":"nicht im Test"}`)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, testseite)
	})
	mux.HandleFunc("/app.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		fmt.Fprint(w, assets.AppJS)
	})
	mux.HandleFunc("/treiber.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		fmt.Fprint(w, ciTreiber)
	})
	mux.HandleFunc("/__marke", func(w http.ResponseWriter, r *http.Request) {
		api.log(anfrage{Marke: r.URL.Query().Get("name")})
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/api/repos", func(w http.ResponseWriter, r *http.Request) { protokolliere(r); json200(w, ciRepos) })
	mux.HandleFunc("/api/arbeit", func(w http.ResponseWriter, r *http.Request) { protokolliere(r); json200(w, ciArbeit) })
	mux.HandleFunc("/api/ci", func(w http.ResponseWriter, r *http.Request) { protokolliere(r); json200(w, ansicht) })
	mux.HandleFunc("/api/ci-log", func(w http.ResponseWriter, r *http.Request) {
		protokolliere(r)
		var job int64
		fmt.Sscanf(r.URL.Query().Get("job"), "%d", &job)
		json200(w, ciLogJSON(t, job))
	})
	mux.HandleFunc("/action/ci/", func(w http.ResponseWriter, r *http.Request) {
		protokolliere(r)
		json200(w, `{"ok":true}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	chromium := func(ctx context.Context, adresse string, extra ...string) ([]byte, error) {
		args := append([]string{
			"--headless", "--no-sandbox", "--disable-gpu", "--no-first-run",
			"--disable-extensions", "--disable-dev-shm-usage", "--hide-scrollbars",
			"--user-data-dir=" + t.TempDir(),
			"--window-size=1400,1000",
			"--virtual-time-budget=30000",
		}, extra...)
		return exec.CommandContext(ctx, chrome, append(args, adresse)...).Output()
	}

	ctx, abbrechen := context.WithTimeout(context.Background(), 180*time.Second)
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
		Abbruch string   `json:"abbruch"`
		DOM     string   `json:"dom"`
		Fehler  []string `json:"fehler"`
		Leiste  []struct {
			Repo string `json:"repo"`
			CI   string `json:"ci"`
		} `json:"leiste"`
		Kopf struct {
			Punkt string `json:"punkt"`
			Text  string `json:"text"`
			Offen string `json:"offen"`
		} `json:"kopf"`
		Runs []struct {
			Run   string           `json:"run"`
			Punkt string           `json:"punkt"`
			Text  string           `json:"text"`
			Neu   bool             `json:"neu"`
			Reihe []ciPunktBericht `json:"reihe"`
		} `json:"runs"`
		Offen struct {
			Job      string `json:"job"`
			Schritt  string `json:"schritt"`
			Schritte []struct {
				Nr    string `json:"nr"`
				Punkt string `json:"punkt"`
				Text  string `json:"text"`
			} `json:"schritte"`
			Ziel        int      `json:"ziel"`
			Sichtbar    bool     `json:"sichtbar"`
			Scroll      float64  `json:"scroll"`
			Links       bool     `json:"links"`
			Zeiten      int      `json:"zeiten"`
			Nummer      string   `json:"nummer"`
			Schrift     string   `json:"schrift"`
			Fehler      []string `json:"fehler"`
			FehlerFarbe string   `json:"fehlerFarbe"`
			NormalFarbe string   `json:"normalFarbe"`
			Markup      string   `json:"markup"`
			Elemente    int      `json:"elemente"`
			GitHub      string   `json:"github"`
			Text        string   `json:"text"`
		} `json:"offen"`
		Sprung struct {
			Ziel     int    `json:"ziel"`
			Sichtbar bool   `json:"sichtbar"`
			Schritt  string `json:"schritt"`
		} `json:"sprung"`
		Zeit    string `json:"zeit"`
		Treffer struct {
			Zahl   string   `json:"zahl"`
			Zeilen []string `json:"zeilen"`
		} `json:"treffer"`
		Lint struct {
			Job  string `json:"job"`
			Ganz bool   `json:"ganz"`
		} `json:"lint"`
		Boese string `json:"boese"`
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

	t.Run("Leiste", func(t *testing.T) {
		punkte := map[string]string{}
		for _, r := range b.Leiste {
			punkte[r.Repo] = r.CI
		}
		if punkte["acme/alpha"] != "rot" || punkte["acme/beta"] != "" {
			t.Errorf("Leiste %+v", b.Leiste)
		}
	})
	t.Run("Kopf", func(t *testing.T) {
		if b.Kopf.Punkt != "rot" || b.Kopf.Text != "1 von 3 grün" || b.Kopf.Offen != "true" {
			t.Errorf("Kopf %+v", b.Kopf)
		}
		if n := len(nur(ab["vor-oeffnen"], http.MethodGet, "/api/ci")); n != 1 {
			t.Errorf("/api/ci beim Oeffnen %d-mal, erwartet 1 (schon vor dem Aufklappen)", n)
		}
	})
	t.Run("Runs", func(t *testing.T) {
		if len(b.Runs) != 3 || b.Runs[0].Run != "12" || b.Runs[1].Run != "11" || b.Runs[2].Run != "10" {
			t.Fatalf("Runs %+v", b.Runs)
		}
		if b.Runs[0].Punkt != "rot" || b.Runs[1].Punkt != "gruen" || b.Runs[2].Punkt != "laeuft" {
			t.Errorf("Punkte %s %s %s", b.Runs[0].Punkt, b.Runs[1].Punkt, b.Runs[2].Punkt)
		}
		r := b.Runs[0]
		for _, teil := range []string{"CI", "main", "push", "ci-runs: Runs je Repo", "3f9c2e1", "heute", "2 min"} {
			if !strings.Contains(r.Text, teil) {
				t.Errorf("Zeile 12 ohne %q: %q", teil, r.Text)
			}
		}
		if len(r.Reihe) != 10 || r.Reihe[6].Punkt != "rot" || r.Reihe[6].Titel != "test › Run go test ./... · fehlgeschlagen" ||
			r.Reihe[7].Punkt != "grau" || r.Reihe[0].Punkt != "gruen" {
			t.Errorf("Punktreihe %+v", r.Reihe)
		}
		if l := b.Runs[2].Reihe; len(l) != 3 || l[1].Punkt != "laeuft" {
			t.Errorf("Punktreihe Run 10 %+v", l)
		}
		if !r.Neu || b.Runs[1].Neu || b.Runs[2].Neu {
			t.Errorf("Knopf neu starten: %v %v %v", r.Neu, b.Runs[1].Neu, b.Runs[2].Neu)
		}
	})
	t.Run("Aufgeklappt", func(t *testing.T) {
		o := b.Offen
		if o.Job != "501" || o.Schritt != "5" {
			t.Errorf("gewaehlt Job %q Schritt %q, erwartet 501/5", o.Job, o.Schritt)
		}
		if len(o.Schritte) != 8 || o.Schritte[4].Punkt != "rot" || !strings.Contains(o.Schritte[4].Text, "Run go test ./...") ||
			!strings.Contains(o.Schritte[4].Text, "1 min 12 s") || o.Schritte[5].Punkt != "grau" {
			t.Errorf("Schritte %+v", o.Schritte)
		}
		if !o.Links {
			t.Error("Jobs stehen nicht links vom Log")
		}
		if o.Ziel != 48 || !o.Sichtbar || o.Scroll <= 0 {
			t.Errorf("Ziel %d sichtbar %v scroll %v", o.Ziel, o.Sichtbar, o.Scroll)
		}
		ls := nur(ab["vor-run"], http.MethodGet, "/api/ci-log")
		if len(ls) != 1 || !strings.Contains(ls[0].Query, "job=501") || !strings.Contains(ls[0].Query, "repo=acme%2Falpha") {
			t.Errorf("Log-Anfragen %+v", ls)
		}
		if o.Zeiten != 0 || o.Nummer != "48" || !strings.Contains(strings.ToLower(o.Schrift), "mono") {
			t.Errorf("zeiten %d nummer %q schrift %q", o.Zeiten, o.Nummer, o.Schrift)
		}
		if strings.Join(o.Fehler, ",") != "58" || o.FehlerFarbe == o.NormalFarbe {
			t.Errorf("Fehlerzeilen %v Farbe %q gegen %q", o.Fehler, o.FehlerFarbe, o.NormalFarbe)
		}
		if o.Text != "go vet ./..." {
			t.Errorf("Zeile 45 %q", o.Text)
		}
		if o.GitHub != "https://github.com/acme/alpha/actions/runs/12/job/501" {
			t.Errorf("Link %q", o.GitHub)
		}
	})
	t.Run("MarkupAlsText", func(t *testing.T) {
		if !strings.Contains(b.Offen.Markup, `<img src=x onerror=alert(1)><script>alert(2)</script>`) {
			t.Errorf("Zeile 54 %q", b.Offen.Markup)
		}
		if b.Offen.Elemente != 0 || b.Boese != "" {
			t.Errorf("Log erzeugt Elemente (%d) oder Skript lief (%q)", b.Offen.Elemente, b.Boese)
		}
	})
	t.Run("Sprung", func(t *testing.T) {
		if b.Sprung.Ziel != 36 || !b.Sprung.Sichtbar || b.Sprung.Schritt != "3" {
			t.Errorf("Sprung %+v", b.Sprung)
		}
	})
	t.Run("ZeitstempelUndSuche", func(t *testing.T) {
		if b.Zeit != "2026-09-30T08:14:02.1234567Z" {
			t.Errorf("Zeitstempel %q", b.Zeit)
		}
		if strings.Join(b.Treffer.Zeilen, ",") != "53,55,56,57" || !strings.Contains(b.Treffer.Zahl, "4") {
			t.Errorf("Treffer %+v", b.Treffer)
		}
	})
	t.Run("AndererJob", func(t *testing.T) {
		ls := nur(ab["vor-job"], http.MethodGet, "/api/ci-log")
		if len(ls) != 1 || !strings.Contains(ls[0].Query, "job=500") {
			t.Errorf("Log-Anfragen %+v", ls)
		}
		if b.Lint.Job != "500" || !b.Lint.Ganz {
			t.Errorf("lint %+v", b.Lint)
		}
	})
	t.Run("NeuStarten", func(t *testing.T) {
		ns := nur(ab["vor-neustart"], http.MethodPost, "/action/ci/neu-starten")
		if len(ns) != 1 {
			t.Fatalf("neu-starten %+v", ns)
		}
		var r struct {
			Repo  string `json:"repo"`
			RunID int64  `json:"run_id"`
		}
		if err := json.Unmarshal([]byte(ns[0].Rumpf), &r); err != nil || r.Repo != "acme/alpha" || r.RunID != 12 {
			t.Errorf("Rumpf %s (%v)", ns[0].Rumpf, err)
		}
		frisch := false
		for _, a := range nur(ab["vor-neustart"], http.MethodGet, "/api/ci") {
			if strings.Contains(a.Query, "frisch=1") {
				frisch = true
			}
		}
		if !frisch {
			t.Error("nach dem Neustart kein /api/ci mit frisch=1")
		}
	})

	if ziel := os.Getenv("GKP_BELEG_PNG_CI_RUNS"); ziel != "" {
		if out, err := chromium(ctx, srv.URL+"/?foto=liste", "--screenshot="+ziel); err != nil {
			t.Errorf("Bildschirmfoto: %v\n%s", err, out)
		}
	}
	if ziel := os.Getenv("GKP_BELEG_PNG_CI_LOG"); ziel != "" {
		if out, err := chromium(ctx, srv.URL+"/?foto=log", "--screenshot="+ziel, "--window-size=1500,1250"); err != nil {
			t.Errorf("Bildschirmfoto: %v\n%s", err, out)
		}
	}
}
