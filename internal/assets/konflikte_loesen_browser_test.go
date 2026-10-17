package assets_test

// Browser-Test der Konfliktloesung je Datei (konflikte-loesen, #75): an der
// aufgeklappten PR-Zeile "Konflikt lösen…", je Konfliktdatei eine Karte mit
// drei Zeitpunkten, drei Optionen und der Vorschau, bei mergen mit
// ueberlappendem Hunk die Hunk-Wahl, "Lösen" erst nach der letzten Wahl, und
// derselbe Knopf in der Detailspalte. Die Vorschau rechnet Go
// (appctx.KonfliktVorschauRechnen); die Oberflaeche zeichnet nur.
//
// Mit GKP_BELEG_PNG_MEINE, GKP_BELEG_PNG_EXTERN, GKP_BELEG_PNG_MERGEN und
// GKP_BELEG_PNG_HUNK legt der Test je ein Bildschirmfoto ab (Option meine,
// extern, mergen konfliktfrei, mergen mit ueberlappendem Hunk).

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
)

func klText(s string) *string { return &s }

// klVorschau baut die Antwort von /api/pr-konflikt aus drei echten Staenden
// je Datei, gerechnet von Go.
func klVorschau(t *testing.T) string {
	t.Helper()
	type seite struct {
		SHA     string `json:"sha"`
		KurzSHA string `json:"kurz_sha"`
		Zeit    string `json:"zeit"`
		Betreff string `json:"betreff"`
	}
	type datei struct {
		appctx.KonfliktVorschau
		Seiten map[string]seite `json:"seiten"`
	}
	seiten := func(n int) map[string]seite {
		return map[string]seite{
			"basis":  {SHA: "b0a5e1100000", KurzSHA: "b0a5e11", Zeit: "2026-09-20T08:00:00Z", Betreff: "Stand vor der Abzweigung"},
			"meine":  {SHA: fmt.Sprintf("m%d11e10000", n), KurzSHA: fmt.Sprintf("m%d11e1", n), Zeit: "2026-09-25T09:20:00Z", Betreff: "PR: eigene Aenderung"},
			"extern": {SHA: fmt.Sprintf("e%d57e20000", n), KurzSHA: fmt.Sprintf("e%d57e2", n), Zeit: "2026-09-26T07:45:00Z", Betreff: "main: fremde Aenderung"},
		}
	}
	staende := []appctx.DateiStaende{
		{Pfad: "b.go",
			Basis:  klText("package b\n\nfunc A() int {\n\treturn 1\n}\n\nfunc B() int {\n\treturn 2\n}\n"),
			Meine:  klText("package b\n\nfunc A() int {\n\treturn 10\n}\n\nfunc B() int {\n\treturn 2\n}\n"),
			Extern: klText("package b\n\nfunc A() int {\n\treturn 1\n}\n\nfunc B() int {\n\treturn 20\n}\n")},
		{Pfad: "web/src/App.svelte",
			Basis:  klText("<script>\n  let titel = 'Alt';\n</script>\n\n<h1>{titel}</h1>\n"),
			Meine:  klText("<script>\n  let titel = 'Meiner';\n</script>\n\n<h1>{titel}</h1>\n"),
			Extern: klText("<script>\n  let titel = 'Von main';\n</script>\n\n<h1>{titel}</h1>\n")},
		{Pfad: "internal/assets/static/app/app.js",
			Basis:  klText("var a=1;\n"),
			Meine:  klText("var a=2;\n"),
			Extern: klText("var a=3;\n")},
	}
	var dateien []datei
	for i, st := range staende {
		dateien = append(dateien, datei{KonfliktVorschau: appctx.KonfliktVorschauRechnen(st), Seiten: seiten(i)})
	}
	b, err := json.Marshal(map[string]any{"nummer": 21, "zustand": "konflikt", "head": "change/einundzwanzig", "base": "main",
		"head_sha": "s21", "base_sha": "shamain", "merge_basis": "b0a5e1100000", "dateien": dateien})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

const klTreiber = `
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
  function karte(pfad) { return '[data-eintrag="pr-21"] .kn-kl-datei[data-pfad="' + pfad + '"]'; }
  function berichten(b) {
    b.fehler = (b.fehler || []).concat(fehler);
    var pre = document.createElement('pre');
    pre.id = 'kn-testbericht';
    pre.textContent = btoa(unescape(encodeURIComponent(JSON.stringify(b))));
    document.body.appendChild(pre);
  }
  async function oeffnen() {
    await warte('Leiste', function () { return q('[data-repo="acme/alpha"]'); });
    q('[data-repo="acme/alpha"]').click();
    await warte('Liste', function () { return q('[data-eintrag="pr-21"]'); });
    q('[data-eintrag="pr-21"] .kn-e-titel').click();
    await warte('Knopf Konflikt loesen', function () { return q('[data-eintrag="pr-21"] .kn-kl-oeffnen'); });
    q('[data-eintrag="pr-21"] .kn-kl-oeffnen').click();
    await warte('Karten', function () { return qa('[data-eintrag="pr-21"] .kn-kl-datei').length === 3; });
  }
  function waehle(pfad, wahl) { q(karte(pfad) + ' [data-wahl="' + wahl + '"]').click(); }
  async function foto(art) {
    await oeffnen();
    var pfad = art === 'hunk' ? 'web/src/App.svelte' : 'b.go';
    waehle(pfad, art === 'hunk' ? 'mergen' : art);
    await warte('Vorschau', function () { return q(karte(pfad) + ' .kn-kl-vorschau'); });
    await schlaf(100);
    q(karte(pfad)).scrollIntoView({ block: 'start' });
  }
  async function lauf() {
    var b = {};
    try {
      var art = new URLSearchParams(location.search).get('foto');
      if (art) { await foto(art); return; }
      await oeffnen();
      b.karten = qa('[data-eintrag="pr-21"] .kn-kl-datei').map(function (k) {
        return {
          pfad: k.getAttribute('data-pfad'),
          seiten: Array.prototype.map.call(k.querySelectorAll('.kn-kl-seite'), function (s) { return s.textContent.replace(/\s+/g, ' ').trim(); }),
          hinweis: k.querySelector('.kn-kl-hinweis') ? k.querySelector('.kn-kl-hinweis').textContent.trim() : '',
        };
      });
      b.loesenAnfang = q('[data-eintrag="pr-21"] .kn-kl-loesen').disabled;

      waehle('b.go', 'meine');
      await warte('Vorschau meine', function () { return q(karte('b.go') + ' .kn-kl-vorschau'); });
      b.meineWeg = qa(karte('b.go') + ' .kn-kl-vorschau .kn-kl-z-weg').map(function (x) { return x.textContent.trim(); });
      b.meineHinzu = qa(karte('b.go') + ' .kn-kl-vorschau .kn-kl-z-hinzu').map(function (x) { return x.textContent.trim(); });
      b.meineGegen = text(karte('b.go') + ' .kn-kl-gegen');

      waehle('b.go', 'mergen');
      await warte('Vorschau mergen', function () { return qa(karte('b.go') + ' .kn-kl-vorschau .kn-kl-diff').length === 2; });
      b.mergenKonfliktfrei = !!q(karte('b.go') + ' .kn-kl-konfliktfrei');
      b.mergenHunkBeiB = qa(karte('b.go') + ' .kn-kl-hunk').length;

      waehle('b.go', 'meine');
      waehle('internal/assets/static/app/app.js', 'extern');
      waehle('web/src/App.svelte', 'mergen');
      await warte('Hunk', function () { return q(karte('web/src/App.svelte') + ' .kn-kl-hunk'); });
      b.hunks = qa(karte('web/src/App.svelte') + ' .kn-kl-hunk').length;
      b.hunkMeine = text(karte('web/src/App.svelte') + ' .kn-kl-hunk .kn-kl-hunk-meine');
      b.hunkExtern = text(karte('web/src/App.svelte') + ' .kn-kl-hunk .kn-kl-hunk-extern');
      b.loesenVorHunk = q('[data-eintrag="pr-21"] .kn-kl-loesen').disabled;
      q(karte('web/src/App.svelte') + ' .kn-kl-hunk [data-hunk-wahl="extern"]').click();
      await schlaf(50);
      b.loesenNachHunk = q('[data-eintrag="pr-21"] .kn-kl-loesen').disabled;

      await marke('vor-loesen');
      q('[data-eintrag="pr-21"] .kn-kl-loesen').click();
      await schlaf(800);
      await marke('nach-loesen');

      q('[data-eintrag="62"] .kn-e-details').click();
      await warte('Branch-Liste', function () { return q('.kn-d-branches li'); });
      await warte('Detail-Knopf', function () { return q('.kn-d-branches .kn-kl-oeffnen'); });
      q('.kn-d-branches .kn-kl-oeffnen').click();
      await warte('Detail-Karten', function () { return qa('.kn-d-branches .kn-kl-datei').length === 3; });
      b.detailKarten = qa('.kn-d-branches .kn-kl-datei').length;
    } catch (e) {
      b.abbruch = String(e && e.message || e);
      b.dom = document.body.innerHTML.slice(0, 3000);
    }
    berichten(b);
  }
  lauf();
})();
`

func TestKonflikteLoesenImBrowser(t *testing.T) {
	if testing.Short() {
		t.Skip("-short: der Test startet einen Browser")
	}
	chrome := chromiumFinden()
	if chrome == "" {
		t.Skip("kein Chromium gefunden — die Oberflaeche ist damit NICHT belegt")
	}
	vorschau := klVorschau(t)
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
		fmt.Fprint(w, klTreiber)
	})
	mux.HandleFunc("/__marke", func(w http.ResponseWriter, r *http.Request) {
		api.log(anfrage{Marke: r.URL.Query().Get("name")})
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/api/repos", func(w http.ResponseWriter, r *http.Request) { protokolliere(r); json200(w, ivRepos) })
	mux.HandleFunc("/api/arbeit", func(w http.ResponseWriter, r *http.Request) { protokolliere(r); json200(w, kaArbeit) })
	mux.HandleFunc("/api/repo-meta", func(w http.ResponseWriter, r *http.Request) { protokolliere(r); json200(w, ivMeta) })
	mux.HandleFunc("/api/detail", func(w http.ResponseWriter, r *http.Request) { protokolliere(r); json200(w, kaDetail) })
	mux.HandleFunc("/api/pr-konflikt", func(w http.ResponseWriter, r *http.Request) { protokolliere(r); json200(w, vorschau) })
	mux.HandleFunc("/action/pr/", func(w http.ResponseWriter, r *http.Request) {
		protokolliere(r)
		json200(w, `{"ok":true,"commit":"mergecommit"}`)
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
		Karten  []struct {
			Pfad    string   `json:"pfad"`
			Seiten  []string `json:"seiten"`
			Hinweis string   `json:"hinweis"`
		} `json:"karten"`
		LoesenAnfang       bool     `json:"loesenAnfang"`
		MeineWeg           []string `json:"meineWeg"`
		MeineHinzu         []string `json:"meineHinzu"`
		MeineGegen         string   `json:"meineGegen"`
		MergenKonfliktfrei bool     `json:"mergenKonfliktfrei"`
		MergenHunkBeiB     int      `json:"mergenHunkBeiB"`
		Hunks              int      `json:"hunks"`
		HunkMeine          string   `json:"hunkMeine"`
		HunkExtern         string   `json:"hunkExtern"`
		LoesenVorHunk      bool     `json:"loesenVorHunk"`
		LoesenNachHunk     bool     `json:"loesenNachHunk"`
		DetailKarten       int      `json:"detailKarten"`
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

	t.Run("KartenMitZeitpunkten", func(t *testing.T) {
		if len(b.Karten) != 3 {
			t.Fatalf("Karten %+v", b.Karten)
		}
		for _, k := range b.Karten {
			if len(k.Seiten) != 3 {
				t.Errorf("%s: %d Zeitpunkte %v, erwartet 3", k.Pfad, len(k.Seiten), k.Seiten)
				continue
			}
			if !strings.Contains(k.Seiten[0], "b0a5e11") || !strings.Contains(k.Seiten[1], "PR: eigene Aenderung") || !strings.Contains(k.Seiten[2], "main: fremde Aenderung") {
				t.Errorf("%s: Zeitpunkte %v", k.Pfad, k.Seiten)
			}
		}
		if k := b.Karten[2]; k.Pfad != "internal/assets/static/app/app.js" || k.Hinweis != "generiert – nach dem Lösen neu bauen" {
			t.Errorf("app.js: %+v, erwartet den Hinweis", k)
		}
		if b.Karten[0].Hinweis != "" {
			t.Errorf("b.go traegt einen Hinweis: %q", b.Karten[0].Hinweis)
		}
		nurLesen := nur(ab[""], http.MethodGet, "/api/pr-konflikt")
		if len(nurLesen) == 0 || !strings.Contains(nurLesen[0].Query, "nummer=21") || !strings.Contains(nurLesen[0].Query, "repo=acme%2Falpha") {
			t.Errorf("/api/pr-konflikt %+v", nurLesen)
		}
	})
	t.Run("VorschauMeine", func(t *testing.T) {
		if strings.Join(b.MeineWeg, "|") != "return 1|return 20" || strings.Join(b.MeineHinzu, "|") != "return 10|return 2" {
			t.Errorf("meine: weg %q, hinzu %q", b.MeineWeg, b.MeineHinzu)
		}
		if !strings.Contains(b.MeineGegen, "extern") {
			t.Errorf("meine: Vergleich %q, erwartet gegen extern", b.MeineGegen)
		}
	})
	t.Run("MergenKonfliktfrei", func(t *testing.T) {
		if !b.MergenKonfliktfrei || b.MergenHunkBeiB != 0 {
			t.Errorf("b.go mergen: konfliktfrei %v, Hunks %d", b.MergenKonfliktfrei, b.MergenHunkBeiB)
		}
	})
	t.Run("HunkWahl", func(t *testing.T) {
		if b.Hunks != 1 || !strings.Contains(b.HunkMeine, "Meiner") || !strings.Contains(b.HunkExtern, "Von main") {
			t.Errorf("App.svelte: %d Hunks, meine %q, extern %q", b.Hunks, b.HunkMeine, b.HunkExtern)
		}
		if !b.LoesenAnfang || !b.LoesenVorHunk || b.LoesenNachHunk {
			t.Errorf("Loesen aktiv: anfangs %v, vor Hunk-Wahl %v, danach %v; erwartet inaktiv, inaktiv, aktiv", !b.LoesenAnfang, !b.LoesenVorHunk, !b.LoesenNachHunk)
		}
	})
	t.Run("Loesen", func(t *testing.T) {
		ls := nur(ab["vor-loesen"], http.MethodPost, "/action/pr/konflikt-loesen")
		if len(ls) != 1 {
			t.Fatalf("konflikt-loesen %+v, erwartet genau einen Aufruf", ls)
		}
		var r struct {
			Repo    string `json:"repo"`
			Nummer  int    `json:"nummer"`
			SHA     string `json:"expected_head_sha"`
			Dateien []struct {
				Pfad  string   `json:"pfad"`
				Wahl  string   `json:"wahl"`
				Hunks []string `json:"hunks"`
			} `json:"dateien"`
		}
		if err := json.Unmarshal([]byte(ls[0].Rumpf), &r); err != nil {
			t.Fatalf("Rumpf %s: %v", ls[0].Rumpf, err)
		}
		if r.Repo != "acme/alpha" || r.Nummer != 21 || r.SHA != "s21" || len(r.Dateien) != 3 {
			t.Fatalf("Rumpf %s", ls[0].Rumpf)
		}
		je := map[string]string{}
		for _, d := range r.Dateien {
			je[d.Pfad] = d.Wahl + ":" + strings.Join(d.Hunks, ",")
		}
		if je["b.go"] != "meine:" || je["web/src/App.svelte"] != "mergen:extern" || je["internal/assets/static/app/app.js"] != "extern:" {
			t.Errorf("Entscheidungen %v", je)
		}
		if !frischGeladen(ab["vor-loesen"]) {
			t.Error("nach dem Loesen kein /api/arbeit mit frisch=1")
		}
	})
	t.Run("Detailspalte", func(t *testing.T) {
		if b.DetailKarten != 3 {
			t.Errorf("Detailspalte: %d Karten, erwartet 3", b.DetailKarten)
		}
	})

	// Auf Wunsch je ein Bildschirmfoto der vier Optionen als Beleg.
	for art, variable := range map[string]string{"meine": "GKP_BELEG_PNG_MEINE", "extern": "GKP_BELEG_PNG_EXTERN", "mergen": "GKP_BELEG_PNG_MERGEN", "hunk": "GKP_BELEG_PNG_HUNK"} {
		if ziel := os.Getenv(variable); ziel != "" {
			if out, err := chromium(ctx, srv.URL+"/?foto="+art, "--screenshot="+ziel); err != nil {
				t.Errorf("Bildschirmfoto %s: %v\n%s", art, err, out)
			}
		}
	}
}
