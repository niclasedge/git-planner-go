package assets_test

// Browser-Test der Merge-Optionen (pr-merge-optionen, #82): Merge-Dialog mit
// Methodenwahl, Commit-Titel und -Text, Auto-Merge an und aus samt Marke,
// Zurueck zum Draft, Labels am PR, die Historie "Geschlossene PRs" mit
// "wieder öffnen" und der Milestone am Issue. Methoden, Vorgabe und Historie
// rechnet Go (appctx.MergeOptionenBauen, appctx.GeschlossenePRsBauen); die
// Fake-API reicht nur weiter, was Go daraus macht.
//
// Mit GKP_BELEG_PNG_MERGE_DIALOG und GKP_BELEG_PNG_GESCHLOSSEN legt der Test
// je ein Bildschirmfoto ab (offener Merge-Dialog neben der Auto-Merge-Marke,
// aufgeklappte Historie).

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

func pmArbeit(t *testing.T) string {
	t.Helper()
	pr := func(n int, titel string) ghrest.PR {
		m := true
		return ghrest.PR{Nummer: n, Titel: titel, URL: fmt.Sprintf("https://github.com/acme/alpha/pull/%d", n), Autor: "cy",
			Labels: []ghrest.Label{}, Head: fmt.Sprintf("change/pr%d", n), HeadRepo: "acme/alpha", HeadSHA: fmt.Sprintf("s%d", n),
			Base: "main", Mergebar: &m, MergeStatus: "clean", Abgleich: ghrest.PRSauber, Zustand: "open"}
	}
	elf := pr(11, "pr-checks: Checks am PR (#80)")
	elf.AutoMerge = &ghrest.AutoMerge{Methode: "squash", Von: "cy"}
	e := appctx.ArbeitEingabe{
		Repo:     "acme/alpha",
		Standard: "main",
		Jetzt:    time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC),
		Offen:    []ghrest.Issue{{Nummer: 31, Titel: "Milestone setzen", URL: "u31"}},
		Branches: []ghrest.Branch{{Name: "main", SHA: "shamain"}},
		PRs:      []ghrest.PR{pr(10, "pr-merge-optionen: Merge-Optionen (#82)"), elf, pr(12, "pr-reviews: Reviews (#81)")},
	}
	b, err := json.Marshal(appctx.RepoArbeitBauen(e))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func pmOptionen(t *testing.T) string {
	t.Helper()
	b, err := json.Marshal(appctx.MergeOptionenBauen(&ghrest.MergeErlaubt{Squash: true, Rebase: true}, "rebase"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func pmGeschlossen(t *testing.T) string {
	t.Helper()
	prs := []ghrest.PR{
		{Nummer: 7, Titel: "konflikte-loesen: Konflikt je Datei (#75)", URL: "u7", Head: "change/konflikte-loesen", HeadRepo: "acme/alpha",
			Zustand: "closed", Gemergt: true, GemergtAm: "2026-09-20T10:00:00Z", GeschlossenAm: "2026-09-20T10:00:00Z"},
		{Nummer: 8, Titel: "Versuch: Knopf verschieben", URL: "u8", Head: "change/knopf", HeadRepo: "acme/alpha",
			Zustand: "closed", GeschlossenAm: "2026-09-24T09:00:00Z"},
	}
	branches := []ghrest.Branch{{Name: "main", SHA: "shamain"}, {Name: "change/knopf", SHA: "s8"}}
	b, err := json.Marshal(map[string]any{"prs": appctx.GeschlossenePRsBauen("acme/alpha", prs, branches)})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

const pmMeta = `{"labels":[{"name":"bug","farbe":"d73a4a"},{"name":"ui","farbe":"0e8a16"}],"zustaendige":["ana","bo"],
 "meilensteine":[{"nummer":3,"titel":"Herbst"},{"nummer":4,"titel":"Winter"}]}`

const pmDetail = `{"art":"issue","nummer":31,"titel":"Milestone setzen","url":"u31","text":"Ein Issue ohne Milestone.",
 "labels":[],"zustaendig":[],"blockiert_durch":[],"kommentare":[],"zustand":"open"}`

const pmTreiber = `
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
  function text(sel) { var x = q(sel); return x ? x.textContent.replace(/\s+/g, ' ').trim() : null; }
  function setze(sel, wert) {
    var x = q(sel);
    x.value = wert;
    x.dispatchEvent(new Event('input', { bubbles: true }));
  }
  function berichten(b) {
    b.fehler = (b.fehler || []).concat(fehler);
    var pre = document.createElement('pre');
    pre.id = 'kn-testbericht';
    pre.textContent = btoa(unescape(encodeURIComponent(JSON.stringify(b))));
    document.body.appendChild(pre);
  }
  function zeile(n) { return '[data-eintrag="pr-' + n + '"]'; }
  async function aufklappen(n) {
    if (!q(zeile(n) + ' .kn-pr-mergen')) q(zeile(n) + ' .kn-e-titel').click();
    await warte('PR-Aktionen #' + n, function () { return q(zeile(n) + ' .kn-pr-mergen'); });
  }
  async function dialog(n) {
    await aufklappen(n);
    q(zeile(n) + ' .kn-pr-mergen').click();
    await warte('Merge-Dialog #' + n, function () { return qa(zeile(n) + ' .kn-pm-dialog input[data-methode]').length > 0; });
  }
  async function oeffnen() {
    await warte('Leiste', function () { return q('[data-repo="acme/alpha"]'); });
    q('[data-repo="acme/alpha"]').click();
    await warte('Liste', function () { return q(zeile(12)); });
  }
  async function foto(art) {
    await oeffnen();
    if (art === 'dialog') {
      await dialog(10);
      q(zeile(10) + ' .kn-pm-dialog input[data-methode="squash"]').click();
      await warte('Commit-Titel', function () { return q(zeile(10) + ' .kn-pm-dialog [name="commit_titel"]'); });
      setze(zeile(10) + ' .kn-pm-dialog [name="commit_titel"]', 'pr-merge-optionen: Merge-Optionen (#82)');
      await schlaf(150);
      q(zeile(10)).scrollIntoView({ block: 'start' });
      return;
    }
    q('.kn-gp-kopf').click();
    await warte('Historie', function () { return q('[data-geschlossen="8"]'); });
    await schlaf(150);
    q('.kn-gp').scrollIntoView({ block: 'center' });
  }
  async function lauf() {
    var b = {};
    try {
      var art = new URLSearchParams(location.search).get('foto');
      if (art) { await foto(art); return; }
      await oeffnen();
      await schlaf(200);
      b.marke11 = (q(zeile(11) + ' [data-auto-merge]') || { getAttribute: function () { return null; } }).getAttribute('data-auto-merge');
      b.markeText11 = text(zeile(11) + ' [data-auto-merge]');
      b.marke10 = !!q(zeile(10) + ' [data-auto-merge]');

      await aufklappen(10);
      b.knopf = text(zeile(10) + ' .kn-pr-mergen');
      b.dialogVorher = !!q(zeile(10) + ' .kn-pm-dialog');
      await dialog(10);
      b.methoden = qa(zeile(10) + ' .kn-pm-dialog input[data-methode]').map(function (x) { return x.getAttribute('data-methode'); });
      b.gewaehlt = (qa(zeile(10) + ' .kn-pm-dialog input[data-methode]').filter(function (x) { return x.checked; })[0] || {}).value || '';
      b.rueckfrage = text(zeile(10) + ' .kn-pr-mergen');
      b.textBeiRebase = !!q(zeile(10) + ' .kn-pm-dialog [name="commit_titel"]');
      q(zeile(10) + ' .kn-pm-dialog input[data-methode="squash"]').click();
      await warte('Commit-Titel', function () { return q(zeile(10) + ' .kn-pm-dialog [name="commit_titel"]'); });
      setze(zeile(10) + ' .kn-pm-dialog [name="commit_titel"]', 'Zehn (#10)');
      setze(zeile(10) + ' .kn-pm-dialog [name="commit_text"]', 'Rumpf');
      await marke('vor-mergen');
      q(zeile(10) + ' .kn-pr-mergen').click();
      await schlaf(700);

      await marke('vor-auto-an');
      await dialog(12);
      q(zeile(12) + ' .kn-pm-auto-an').click();
      await schlaf(700);

      await marke('vor-entwurf');
      await aufklappen(12);
      b.entwurfText = text(zeile(12) + ' .kn-pr-entwurf');
      q(zeile(12) + ' .kn-pr-entwurf').click();
      await schlaf(700);

      await marke('vor-label');
      q(zeile(12) + ' .kn-prb-knopf').click();
      await warte('Label-Auswahl', function () { return q(zeile(12) + ' .kn-prb [data-label="bug"]'); });
      b.meilensteinAmPR = !!q(zeile(12) + ' .kn-prb select[name="meilenstein"]');
      q(zeile(12) + ' .kn-prb [data-label="bug"]').click();
      await schlaf(700);

      await marke('vor-auto-aus');
      await aufklappen(11);
      await warte('Auto-Merge aus', function () { return q(zeile(11) + ' .kn-pm-auto-aus'); });
      q(zeile(11) + ' .kn-pm-auto-aus').click();
      await schlaf(700);

      await marke('vor-historie');
      b.historieVorher = !!q('[data-geschlossen]');
      q('.kn-gp-kopf').click();
      await warte('Historie', function () { return q('[data-geschlossen="8"]'); });
      b.folge = qa('[data-geschlossen]').map(function (x) { return x.getAttribute('data-geschlossen'); });
      b.g7 = text('[data-geschlossen="7"]');
      b.g8 = text('[data-geschlossen="8"]');
      b.knopf7 = !!q('[data-geschlossen="7"] .kn-gp-oeffnen');
      b.knopf8 = text('[data-geschlossen="8"] .kn-gp-oeffnen');

      await marke('vor-oeffnen');
      q('[data-geschlossen="8"] .kn-gp-oeffnen').click();
      await schlaf(700);

      await marke('vor-meilenstein');
      q('[data-eintrag="31"] .kn-e-details').click();
      await warte('Milestone-Auswahl', function () { return q('.kn-detail select[name="meilenstein"]'); });
      var s = q('.kn-detail select[name="meilenstein"]');
      b.meilensteinOptionen = Array.prototype.map.call(s.options, function (o) { return o.textContent.trim(); });
      s.value = '3';
      s.dispatchEvent(new Event('change', { bubbles: true }));
      await schlaf(700);
      await marke('ende');
    } catch (e) {
      b.abbruch = String(e && e.message || e);
      b.dom = document.body.innerHTML.slice(0, 4000);
    }
    berichten(b);
  }
  lauf();
})();
`

func TestPRMergeOptionenImBrowser(t *testing.T) {
	if testing.Short() {
		t.Skip("-short: der Test startet einen Browser")
	}
	chrome := chromiumFinden()
	if chrome == "" {
		t.Skip("kein Chromium gefunden — die Oberflaeche ist damit NICHT belegt")
	}
	arbeit, optionen, geschlossen := pmArbeit(t), pmOptionen(t), pmGeschlossen(t)
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
		fmt.Fprint(w, pmTreiber)
	})
	mux.HandleFunc("/__marke", func(w http.ResponseWriter, r *http.Request) {
		api.log(anfrage{Marke: r.URL.Query().Get("name")})
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/api/repos", func(w http.ResponseWriter, r *http.Request) { protokolliere(r); json200(w, ivRepos) })
	mux.HandleFunc("/api/arbeit", func(w http.ResponseWriter, r *http.Request) { protokolliere(r); json200(w, arbeit) })
	mux.HandleFunc("/api/repo-meta", func(w http.ResponseWriter, r *http.Request) { protokolliere(r); json200(w, pmMeta) })
	mux.HandleFunc("/api/detail", func(w http.ResponseWriter, r *http.Request) { protokolliere(r); json200(w, pmDetail) })
	mux.HandleFunc("/api/pr-optionen", func(w http.ResponseWriter, r *http.Request) { protokolliere(r); json200(w, optionen) })
	mux.HandleFunc("/api/pr-geschlossen", func(w http.ResponseWriter, r *http.Request) { protokolliere(r); json200(w, geschlossen) })
	mux.HandleFunc("/action/pr/", func(w http.ResponseWriter, r *http.Request) { protokolliere(r); json200(w, `{"ok":true}`) })
	mux.HandleFunc("/action/issue/", func(w http.ResponseWriter, r *http.Request) {
		protokolliere(r)
		json200(w, `{"ok":true,"issue":{"nummer":31}}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	chromium := func(ctx context.Context, adresse string, extra ...string) ([]byte, error) {
		args := append([]string{
			"--headless", "--no-sandbox", "--disable-gpu", "--no-first-run",
			"--disable-extensions", "--disable-dev-shm-usage", "--hide-scrollbars",
			"--user-data-dir=" + t.TempDir(),
			"--window-size=1400,1000",
			"--virtual-time-budget=40000",
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
		Abbruch             string   `json:"abbruch"`
		DOM                 string   `json:"dom"`
		Fehler              []string `json:"fehler"`
		Marke11             *string  `json:"marke11"`
		MarkeText11         string   `json:"markeText11"`
		Marke10             bool     `json:"marke10"`
		Knopf               string   `json:"knopf"`
		DialogVorher        bool     `json:"dialogVorher"`
		Methoden            []string `json:"methoden"`
		Gewaehlt            string   `json:"gewaehlt"`
		Rueckfrage          string   `json:"rueckfrage"`
		TextBeiRebase       bool     `json:"textBeiRebase"`
		EntwurfText         string   `json:"entwurfText"`
		MeilensteinAmPR     bool     `json:"meilensteinAmPR"`
		HistorieVorher      bool     `json:"historieVorher"`
		Folge               []string `json:"folge"`
		G7                  string   `json:"g7"`
		G8                  string   `json:"g8"`
		Knopf7              bool     `json:"knopf7"`
		Knopf8              string   `json:"knopf8"`
		MeilensteinOptionen []string `json:"meilensteinOptionen"`
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

	// rumpf liest den einen Aufruf von pfad im Abschnitt und prueft das Neuladen.
	rumpf := func(t *testing.T, abschnitt, pfad string) map[string]any {
		t.Helper()
		as := nur(ab[abschnitt], http.MethodPost, pfad)
		if len(as) != 1 {
			t.Fatalf("%s: %s %+v, erwartet genau einen Aufruf", abschnitt, pfad, as)
		}
		var r map[string]any
		if err := json.Unmarshal([]byte(as[0].Rumpf), &r); err != nil {
			t.Fatalf("Rumpf %s: %v", as[0].Rumpf, err)
		}
		if r["repo"] != "acme/alpha" {
			t.Errorf("repo %v", r["repo"])
		}
		if !frischGeladen(ab[abschnitt]) {
			t.Errorf("%s: danach kein /api/arbeit mit frisch=1", abschnitt)
		}
		return r
	}

	t.Run("AutoMergeMarke", func(t *testing.T) {
		if b.Marke11 == nil || *b.Marke11 != "squash" || !strings.Contains(b.MarkeText11, "Auto-Merge") {
			t.Errorf("#11: data-auto-merge %v, Text %q", b.Marke11, b.MarkeText11)
		}
		if b.Marke10 {
			t.Error("#10 traegt eine Auto-Merge-Marke, obwohl kein Auto-Merge an ist")
		}
	})
	t.Run("MergeDialog", func(t *testing.T) {
		if b.Knopf != "PR mergen" || b.DialogVorher {
			t.Errorf("Knopf %q, Dialog vor dem Klick %v", b.Knopf, b.DialogVorher)
		}
		if strings.Join(b.Methoden, ",") != "squash,rebase" || b.Gewaehlt != "rebase" {
			t.Errorf("Methoden %v, gewaehlt %q, erwartet squash,rebase mit rebase", b.Methoden, b.Gewaehlt)
		}
		if !strings.Contains(b.Rueckfrage, "?") {
			t.Errorf("keine Rueckfrage nach dem ersten Klick: %q", b.Rueckfrage)
		}
		if b.TextBeiRebase {
			t.Error("bei rebase steht ein Commit-Titel im Dialog")
		}
	})
	t.Run("Mergen", func(t *testing.T) {
		r := rumpf(t, "vor-mergen", "/action/pr/mergen")
		if fmt.Sprint(r["nummer"]) != "10" || r["methode"] != "squash" || r["titel"] != "Zehn (#10)" || r["text"] != "Rumpf" || r["branch_loeschen"] != true {
			t.Errorf("mergen %+v", r)
		}
	})
	t.Run("AutoMergeAn", func(t *testing.T) {
		r := rumpf(t, "vor-auto-an", "/action/pr/auto-merge")
		if fmt.Sprint(r["nummer"]) != "12" || r["an"] != true || r["methode"] != "rebase" {
			t.Errorf("auto-merge an %+v", r)
		}
		if len(nur(ab["vor-auto-an"], http.MethodPost, "/action/pr/mergen")) != 0 {
			t.Error("Auto-Merge hat zusaetzlich gemergt")
		}
	})
	t.Run("ZurueckZumEntwurf", func(t *testing.T) {
		if b.EntwurfText != "Zurück zum Draft" {
			t.Errorf("Knopf %q", b.EntwurfText)
		}
		if r := rumpf(t, "vor-entwurf", "/action/pr/zu-entwurf"); fmt.Sprint(r["nummer"]) != "12" {
			t.Errorf("zu-entwurf %+v", r)
		}
	})
	t.Run("LabelAmPR", func(t *testing.T) {
		if !b.MeilensteinAmPR {
			t.Error("am PR keine Milestone-Auswahl")
		}
		r := rumpf(t, "vor-label", "/action/pr/bearbeiten")
		if fmt.Sprint(r["nummer"]) != "12" || fmt.Sprint(r["labels"]) != "[bug]" {
			t.Errorf("bearbeiten %+v", r)
		}
	})
	t.Run("AutoMergeAus", func(t *testing.T) {
		r := rumpf(t, "vor-auto-aus", "/action/pr/auto-merge")
		if fmt.Sprint(r["nummer"]) != "11" || r["an"] != false {
			t.Errorf("auto-merge aus %+v", r)
		}
	})
	t.Run("Historie", func(t *testing.T) {
		if b.HistorieVorher {
			t.Error("Historie vor dem Aufklappen schon gezeigt")
		}
		if len(nur(ab["vor-historie"], http.MethodGet, "/api/pr-geschlossen")) != 1 {
			t.Errorf("pr-geschlossen %+v, erwartet genau einen Abruf beim Aufklappen", ab["vor-historie"])
		}
		if strings.Join(b.Folge, ",") != "8,7" {
			t.Errorf("Folge %v, erwartet 8,7", b.Folge)
		}
		if !strings.Contains(b.G7, "gemergt") || !strings.Contains(b.G7, "20.09.") || b.Knopf7 {
			t.Errorf("#7 %q, Knopf %v", b.G7, b.Knopf7)
		}
		if !strings.Contains(b.G8, "geschlossen") || !strings.Contains(b.G8, "24.09.") || b.Knopf8 != "wieder öffnen" {
			t.Errorf("#8 %q, Knopf %q", b.G8, b.Knopf8)
		}
	})
	t.Run("WiederOeffnen", func(t *testing.T) {
		r := rumpf(t, "vor-oeffnen", "/action/pr/wieder-oeffnen")
		if fmt.Sprint(r["nummer"]) != "8" {
			t.Errorf("wieder-oeffnen %+v", r)
		}
		if len(nur(ab["vor-oeffnen"], http.MethodGet, "/api/pr-geschlossen")) == 0 {
			t.Error("Historie nach dem Oeffnen nicht neu geladen")
		}
	})
	t.Run("MilestoneAmIssue", func(t *testing.T) {
		if !strings.Contains(strings.Join(b.MeilensteinOptionen, "|"), "Herbst") {
			t.Errorf("Optionen %v", b.MeilensteinOptionen)
		}
		as := nur(ab["vor-meilenstein"], http.MethodPost, "/action/issue/aendern")
		if len(as) != 1 || !strings.Contains(as[0].Rumpf, `"meilenstein":3`) || !strings.Contains(as[0].Rumpf, `"nummer":31`) {
			t.Errorf("issue/aendern %+v", as)
		}
	})

	for _, foto := range []struct{ art, variable string }{{"dialog", "GKP_BELEG_PNG_MERGE_DIALOG"}, {"geschlossen", "GKP_BELEG_PNG_GESCHLOSSEN"}} {
		if ziel := os.Getenv(foto.variable); ziel != "" {
			if out, err := chromium(ctx, srv.URL+"/?foto="+foto.art, "--screenshot="+ziel); err != nil {
				t.Errorf("Bildschirmfoto %s: %v\n%s", foto.art, err, out)
			}
		}
	}
}
