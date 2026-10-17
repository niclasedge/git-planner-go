package assets_test

// Browser-Test der CI-Pruefungen je PR (pr-checks, #80): die Marke je
// PR-Zeile, aufgeklappt die Liste der Pruefungen mit Link zum Log und dem
// Knopf "neu starten" nur bei fehlgeschlagenen Check-Runs, und dieselbe Liste
// in der Detailspalte. Zustand, Marke, Zusammenfassung und Reihenfolge kommen
// fertig aus Go (appctx.ChecksRechnen); die Oberflaeche zeichnet nur.
//
// Mit GKP_BELEG_PNG_PR_CHECKS legt der Test ein Bildschirmfoto der
// aufgeklappten Zeile ab.

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

// pcArbeitJSON baut /api/arbeit mit drei PRs: #9 gemischt (rot, laeuft,
// gruen), #10 gruen, #11 ohne Pruefungen. Issue #8 haengt am Branch von #9.
func pcArbeitJSON(t *testing.T) string {
	t.Helper()
	jetzt := time.Date(2026, 9, 26, 10, 5, 0, 0, time.UTC)
	neun := appctx.ChecksRechnen("s9", []ghrest.CheckRun{
		{ID: 101, Name: "build", Status: "completed", Conclusion: "failure", StartedAt: "2026-09-26T10:00:00Z", CompletedAt: "2026-09-26T10:01:12Z",
			HTMLURL: "https://github.com/acme/alpha/runs/101", App: "github-actions", DetailsURL: "https://github.com/acme/alpha/actions/runs/555/job/101"},
		{ID: 102, Name: "lint", Status: "in_progress", StartedAt: "2026-09-26T10:04:20Z", HTMLURL: "https://github.com/acme/alpha/runs/102"},
		{ID: 103, Name: "test", Status: "completed", Conclusion: "success", StartedAt: "2026-09-26T10:00:00Z", CompletedAt: "2026-09-26T10:03:05Z",
			HTMLURL: "https://github.com/acme/alpha/runs/103"},
	}, []ghrest.CommitStatus{{Kontext: "ci/legacy", Zustand: "success", URL: "https://ci.example/7"}}, jetzt)
	zehn := appctx.ChecksRechnen("s10", []ghrest.CheckRun{
		{ID: 201, Name: "build", Status: "completed", Conclusion: "success", HTMLURL: "https://github.com/acme/alpha/runs/201"},
		{ID: 202, Name: "test", Status: "completed", Conclusion: "success", HTMLURL: "https://github.com/acme/alpha/runs/202"},
	}, nil, jetzt)
	elf := appctx.ChecksRechnen("s11", nil, nil, jetzt)
	pr := func(n int, draft bool, c ghrest.PRChecks) ghrest.PR {
		return ghrest.PR{Nummer: n, Titel: fmt.Sprintf("PR %d", n), URL: fmt.Sprintf("u%d", n), Autor: "cy", Labels: []ghrest.Label{},
			Draft: draft, Head: fmt.Sprintf("change/pr%d", n), HeadRepo: "acme/alpha", HeadSHA: c.SHA, Base: "main", MergeStatus: "clean",
			Abgleich: "sauber", Checks: &c}
	}
	type branchPR struct {
		Nummer int              `json:"nummer"`
		Titel  string           `json:"titel"`
		URL    string           `json:"url"`
		Checks *ghrest.PRChecks `json:"checks"`
	}
	type branch struct {
		Name    string   `json:"name"`
		Gruende []string `json:"gruende"`
		Voraus  int      `json:"voraus"`
		Zurueck int      `json:"zurueck"`
		PR      branchPR `json:"pr"`
	}
	b, err := json.Marshal(map[string]any{
		"repo": "acme/alpha", "fortschritt": map[string]int{"erledigt": 0, "gesamt": 0}, "zuletzt": []any{}, "ohne_issue": []any{}, "quellen": []any{},
		"reihe": []any{map[string]any{"art": "issue", "nummer": 8, "titel": "Acht: CI zeigen", "url": "u8", "status": "ohne_change",
			"blockiert_durch": []int{}, "labels": []any{}, "zustaendig": []string{}, "hat_text": true, "tiefe": 0, "aktiv": true,
			"branches": []branch{{Name: "change/pr9", Gruende: []string{"pr"}, Voraus: 2, PR: branchPR{Nummer: 9, Titel: "PR 9", URL: "u9", Checks: &neun}}}}},
		"prs": []ghrest.PR{pr(9, false, neun), pr(10, false, zehn), pr(11, true, elf)},
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

const pcTreiber = `
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
  function text(x) { return x ? x.textContent.replace(/\s+/g, ' ').trim() : null; }
  function berichten(b) {
    b.fehler = (b.fehler || []).concat(fehler);
    var pre = document.createElement('pre');
    pre.id = 'kn-testbericht';
    pre.textContent = btoa(unescape(encodeURIComponent(JSON.stringify(b))));
    document.body.appendChild(pre);
  }
  function liste(wurzel) {
    return qa(wurzel + ' .kn-pc-pruefung').map(function (p) {
      var a = p.querySelector('a.kn-pc-log');
      return {
        name: p.getAttribute('data-pruefung'),
        zustand: p.getAttribute('data-zustand'),
        text: text(p),
        link: a ? a.getAttribute('href') : '',
        knopf: !!p.querySelector('.kn-pc-neu'),
      };
    });
  }
  async function oeffnen() {
    await warte('Leiste', function () { return q('[data-repo="acme/alpha"]'); });
    q('[data-repo="acme/alpha"]').click();
    await warte('Liste', function () { return q('[data-eintrag="pr-9"]'); });
    q('[data-eintrag="pr-9"] .kn-e-titel').click();
    await warte('Pruefungen', function () { return qa('[data-eintrag="pr-9"] .kn-pc-pruefung').length === 4; });
  }
  async function lauf() {
    var b = {};
    try {
      if (new URLSearchParams(location.search).get('foto')) {
        await oeffnen();
        await schlaf(100);
        q('[data-eintrag="pr-9"]').scrollIntoView({ block: 'start' });
        return;
      }
      await oeffnen();
      b.marken = ['pr-9', 'pr-10', 'pr-11'].map(function (id) {
        var m = q('[data-eintrag="' + id + '"] .kn-e-checks');
        return m ? { checks: m.getAttribute('data-checks'), text: text(m), titel: m.getAttribute('title') } : null;
      });
      b.zusammenfassung = text(q('[data-eintrag="pr-9"] .kn-pc-kopf'));
      b.liste = liste('[data-eintrag="pr-9"]');

      q('[data-eintrag="pr-11"] .kn-e-titel').click();
      await warte('leere Liste', function () { return q('[data-eintrag="pr-11"] .kn-pc-leer'); });
      b.leer = text(q('[data-eintrag="pr-11"] .kn-pc-leer'));

      await marke('vor-neustart');
      q('[data-eintrag="pr-9"] .kn-pc-pruefung[data-pruefung="build"] .kn-pc-neu').click();
      await schlaf(800);
      await marke('nach-neustart');

      q('[data-eintrag="pr-9"] .kn-e-details').click();
      await warte('Detailspalte', function () { return qa('.kn-detail .kn-pc-pruefung').length === 4; });
      b.detail = liste('.kn-detail');
      q('.kn-detail .kn-d-zu').click();
      await schlaf(100);

      q('[data-eintrag="8"] .kn-e-details').click();
      await warte('Branch-PR', function () { return qa('.kn-d-branches .kn-pc-pruefung').length === 4; });
      b.branch = liste('.kn-d-branches');
    } catch (e) {
      b.abbruch = String(e && e.message || e);
      b.dom = document.body.innerHTML.slice(0, 3000);
    }
    berichten(b);
  }
  lauf();
})();
`

type pcEintrag struct {
	Name    string `json:"name"`
	Zustand string `json:"zustand"`
	Text    string `json:"text"`
	Link    string `json:"link"`
	Knopf   bool   `json:"knopf"`
}

func TestPRChecksImBrowser(t *testing.T) {
	if testing.Short() {
		t.Skip("-short: der Test startet einen Browser")
	}
	chrome := chromiumFinden()
	if chrome == "" {
		t.Skip("kein Chromium gefunden — die Oberflaeche ist damit NICHT belegt")
	}
	arbeit := pcArbeitJSON(t)
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
		fmt.Fprint(w, pcTreiber)
	})
	mux.HandleFunc("/__marke", func(w http.ResponseWriter, r *http.Request) {
		api.log(anfrage{Marke: r.URL.Query().Get("name")})
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/api/repos", func(w http.ResponseWriter, r *http.Request) { protokolliere(r); json200(w, ivRepos) })
	mux.HandleFunc("/api/arbeit", func(w http.ResponseWriter, r *http.Request) { protokolliere(r); json200(w, arbeit) })
	mux.HandleFunc("/api/repo-meta", func(w http.ResponseWriter, r *http.Request) { protokolliere(r); json200(w, ivMeta) })
	mux.HandleFunc("/api/detail", func(w http.ResponseWriter, r *http.Request) {
		protokolliere(r)
		json200(w, fmt.Sprintf(`{"art":"issue","nummer":%s,"titel":"Eintrag","text":"Text"}`, r.URL.Query().Get("nummer")))
	})
	mux.HandleFunc("/action/pr/", func(w http.ResponseWriter, r *http.Request) {
		protokolliere(r)
		json200(w, `{"ok":true,"weg":"actions"}`)
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
		Marken  []*struct {
			Checks string `json:"checks"`
			Text   string `json:"text"`
			Titel  string `json:"titel"`
		} `json:"marken"`
		Zusammenfassung string      `json:"zusammenfassung"`
		Liste           []pcEintrag `json:"liste"`
		Leer            string      `json:"leer"`
		Detail          []pcEintrag `json:"detail"`
		Branch          []pcEintrag `json:"branch"`
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

	t.Run("Marken", func(t *testing.T) {
		if len(b.Marken) != 3 || b.Marken[0] == nil || b.Marken[1] == nil {
			t.Fatalf("Marken %+v", b.Marken)
		}
		if n := b.Marken[0]; n.Checks != "rot" || n.Text != "✗ 1" || n.Titel != "1 rot · 1 läuft · 2 grün" {
			t.Errorf("#9 Marke %+v", n)
		}
		if z := b.Marken[1]; z.Checks != "gruen" || z.Text != "✓ 2" {
			t.Errorf("#10 Marke %+v", z)
		}
		if b.Marken[2] != nil {
			t.Errorf("#11 ohne Pruefungen traegt eine Marke: %+v", b.Marken[2])
		}
	})
	pruefeListe := func(t *testing.T, wo string, l []pcEintrag) {
		t.Helper()
		var namen []string
		for _, e := range l {
			namen = append(namen, e.Name+":"+e.Zustand)
		}
		if strings.Join(namen, ",") != "build:rot,lint:laeuft,ci/legacy:gruen,test:gruen" {
			t.Fatalf("%s: Liste %v", wo, namen)
		}
		if l[0].Link != "https://github.com/acme/alpha/runs/101" || l[2].Link != "https://ci.example/7" {
			t.Errorf("%s: Links %q %q", wo, l[0].Link, l[2].Link)
		}
		if !strings.Contains(l[0].Text, "1 min 12 s") || !strings.Contains(l[3].Text, "3 min 5 s") {
			t.Errorf("%s: Dauer fehlt: %q / %q", wo, l[0].Text, l[3].Text)
		}
		for i, e := range l {
			if e.Knopf != (i == 0) {
				t.Errorf("%s: %s traegt Knopf %v", wo, e.Name, e.Knopf)
			}
		}
	}
	t.Run("AufgeklappteListe", func(t *testing.T) {
		pruefeListe(t, "Zeile", b.Liste)
		if !strings.Contains(b.Zusammenfassung, "1 rot · 1 läuft · 2 grün") {
			t.Errorf("Zusammenfassung %q", b.Zusammenfassung)
		}
		if b.Leer != "Keine Prüfungen auf diesem Stand." {
			t.Errorf("#11: %q", b.Leer)
		}
	})
	t.Run("NeuStarten", func(t *testing.T) {
		ns := nur(ab["vor-neustart"], http.MethodPost, "/action/pr/checks-neu")
		if len(ns) != 1 {
			t.Fatalf("checks-neu %+v, erwartet genau einen Aufruf", ns)
		}
		var r struct {
			Repo       string `json:"repo"`
			Nummer     int    `json:"nummer"`
			CheckRunID int64  `json:"check_run_id"`
		}
		if err := json.Unmarshal([]byte(ns[0].Rumpf), &r); err != nil || r.Repo != "acme/alpha" || r.Nummer != 9 || r.CheckRunID != 101 {
			t.Errorf("Rumpf %s (%v)", ns[0].Rumpf, err)
		}
		if !frischGeladen(ab["vor-neustart"]) {
			t.Error("nach dem Neustart kein /api/arbeit mit frisch=1")
		}
	})
	t.Run("Detailspalte", func(t *testing.T) {
		pruefeListe(t, "Detailspalte", b.Detail)
		pruefeListe(t, "Branch-PR", b.Branch)
	})

	if ziel := os.Getenv("GKP_BELEG_PNG_PR_CHECKS"); ziel != "" {
		if out, err := chromium(ctx, srv.URL+"/?foto=1", "--screenshot="+ziel); err != nil {
			t.Errorf("Bildschirmfoto: %v\n%s", err, out)
		}
	}
}
