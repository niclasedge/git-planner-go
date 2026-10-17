package assets_test

// Browser-Test des Lebenslaufs je Issue und PR (pr-lebenslauf, #83): die
// kompakte Stufenleiste mit acht Punkten in drei Zeilen in verschiedenen
// Stufen, die grosse Leiste in der Detailspalte mit Zeitpunkt je erledigter
// Stufe, die Nachsorge nach dem Merge mit "Issue schließen", "Branch
// löschen" und dem Befehl zum Kopieren, und der Kopf "PR · #N". Stufen,
// Zustaende und Nachsorge kommen fertig aus Go (appctx.LebenslaufBauen); die
// Oberflaeche zeichnet nur.
//
// Mit GKP_BELEG_PNG_LEBENSLAUF_LISTE und GKP_BELEG_PNG_LEBENSLAUF_DETAIL
// legt der Test je ein Bildschirmfoto der Liste und der Detailspalte ab.

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

// llArbeitJSON baut /api/arbeit: #4 ohne Branch, #5 gemergt mit offener
// Nachsorge, PR #8 mit verlangten Aenderungen.
func llArbeitJSON(t *testing.T) string {
	t.Helper()
	vier := appctx.LebenslaufBauen(appctx.LebenslaufEingabe{Repo: "acme/alpha", Issue: 4, IssueOffen: true})
	fuenf := appctx.LebenslaufBauen(appctx.LebenslaufEingabe{Repo: "acme/alpha", Issue: 5, IssueOffen: true,
		Branch: "change/fuenf", BranchZeit: "2026-09-20T08:00:00Z", BranchDa: true,
		PR: &ghrest.PR{Nummer: 7, Titel: "fuenf", Head: "change/fuenf", HeadRepo: "acme/alpha", Zustand: "closed", Gemergt: true,
			GemergtAm: "2026-09-24T12:00:00Z", Erstellt: "2026-09-21T09:00:00Z"},
		Change: &appctx.ChangeKurz{Kennung: "fuenf"}})
	pr := ghrest.PR{Nummer: 8, Titel: "PR acht", URL: "u8", Autor: "dd", Labels: []ghrest.Label{}, Head: "sechs", HeadRepo: "acme/alpha",
		HeadSHA: "s8", Base: "main", MergeStatus: "clean", Abgleich: "sauber", Zustand: "open", Erstellt: "2026-09-22T09:00:00Z",
		Checks: &ghrest.PRChecks{SHA: "s8", Gesamt: "gruen", Marke: "✓", Pruefungen: []ghrest.Pruefung{}},
		Reviews: &ghrest.PRReviews{Entscheidung: "aenderungen", Marke: "✎", Titel: "Änderungen verlangt von bo",
			Stimmen: []ghrest.ReviewStimme{{Login: "bo", Zustand: "aenderungen"}}, Angefordert: []string{}, AngefordertTeams: []string{},
			Threads: []ghrest.ReviewThread{}}}
	acht := appctx.LebenslaufBauen(appctx.LebenslaufEingabe{Repo: "acme/alpha", Issue: 6, IssueOffen: true, Branch: "sechs", BranchDa: true, PR: &pr})
	eintrag := func(nr int, titel string, l appctx.Lebenslauf) appctx.ArbeitsEintrag {
		return appctx.ArbeitsEintrag{Art: "issue", Nummer: nr, Titel: titel, URL: fmt.Sprintf("u%d", nr), Status: "ohne_change",
			BlockiertDurch: []int{}, Labels: []ghrest.Label{}, Zustaendig: []string{}, Branches: []appctx.IssueBranch{}, Lebenslauf: &l}
	}
	b, err := json.Marshal(map[string]any{
		"repo": "acme/alpha", "fortschritt": map[string]int{"erledigt": 0, "gesamt": 0}, "zuletzt": []any{}, "ohne_issue": []any{}, "quellen": []any{},
		"reihe":     []appctx.ArbeitsEintrag{eintrag(5, "Fünf ist gemergt", fuenf), eintrag(4, "Vier hat noch keinen Branch", vier)},
		"prs":       []ghrest.PR{pr},
		"pr_laeufe": map[int]appctx.Lebenslauf{8: acht},
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

const llTreiber = `
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
  function leiste(id) {
    var l = q('[data-eintrag="' + id + '"] .kn-ll');
    if (!l) return null;
    return {
      stufe: l.getAttribute('data-stufe'),
      punkte: Array.prototype.slice.call(l.querySelectorAll('.kn-ll-punkt')).map(function (p) {
        return p.getAttribute('data-name') + '=' + p.getAttribute('data-zustand');
      }),
      titel: (l.querySelector('.kn-ll-punkt[data-zustand="blockiert"]') || l.querySelector('.kn-ll-punkt')).getAttribute('title'),
    };
  }
  async function repo() {
    await warte('Leiste', function () { return q('[data-repo="acme/alpha"]'); });
    q('[data-repo="acme/alpha"]').click();
    await warte('Liste', function () { return q('[data-eintrag="pr-8"] .kn-ll') && q('[data-eintrag="5"] .kn-ll'); });
  }
  async function detail(id) {
    q('[data-eintrag="' + id + '"] .kn-e-details').click();
    await warte('Detail ' + id, function () { return q('.kn-detail .kn-ll-gross'); });
  }
  async function lauf() {
    var b = {};
    try {
      var foto = new URLSearchParams(location.search).get('foto');
      if (foto === 'liste') {
        await repo();
        await schlaf(100);
        return;
      }
      if (foto === 'detail') {
        await repo();
        await detail('5');
        await schlaf(100);
        return;
      }
      await repo();
      b.leisten = { vier: leiste('4'), fuenf: leiste('5'), acht: leiste('pr-8') };

      await detail('5');
      b.kopfIssue = text(q('.kn-detail .kn-d-art'));
      b.gross = qa('.kn-detail .kn-ll-gross .kn-ll-stufe').map(function (s) {
        return {
          name: s.getAttribute('data-name'), zustand: s.getAttribute('data-zustand'),
          titel: text(s.querySelector('.kn-ll-titel')), grund: text(s.querySelector('.kn-ll-grund')) || '',
          zeit: s.querySelector('.kn-ll-zeit') ? s.querySelector('.kn-ll-zeit').getAttribute('datetime') : '',
        };
      });
      b.nachsorge = qa('.kn-detail .kn-ll-nachsorge [data-art]').map(function (p) {
        return {
          art: p.getAttribute('data-art'), erledigt: p.getAttribute('data-erledigt'), text: text(p.querySelector('.kn-ll-ns-text')),
          knopf: text(p.querySelector('button.kn-ll-knopf')) || '', befehl: text(p.querySelector('.kn-schritt-befehl')) || '',
          kopieren: !!p.querySelector('.kn-schritt-kopieren'),
        };
      });

      await marke('issue');
      q('.kn-detail .kn-ll-nachsorge [data-art="issue_schliessen"] button.kn-ll-knopf').click();
      await schlaf(400);
      await marke('branch');
      q('.kn-detail .kn-ll-nachsorge [data-art="branch_loeschen"] button.kn-ll-knopf').click();
      await schlaf(400);
      await marke('ende');

      q('[data-eintrag="pr-8"] .kn-e-details').click();
      await warte('Detail pr-8', function () { return /PR/.test(text(q('.kn-detail .kn-d-art')) || ''); });
      b.kopfPR = text(q('.kn-detail .kn-d-art'));
    } catch (e) {
      b.abbruch = String(e && e.message || e);
      b.dom = document.body.innerHTML.slice(0, 4000);
    }
    berichten(b);
  }
  lauf();
})();
`

type llLeiste struct {
	Stufe  string   `json:"stufe"`
	Punkte []string `json:"punkte"`
	Titel  string   `json:"titel"`
}

func TestLebenslaufImBrowser(t *testing.T) {
	if testing.Short() {
		t.Skip("-short: der Test startet einen Browser")
	}
	chrome := chromiumFinden()
	if chrome == "" {
		t.Skip("kein Chromium gefunden — die Oberflaeche ist damit NICHT belegt")
	}
	arbeit := llArbeitJSON(t)
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
		fmt.Fprint(w, llTreiber)
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
	mux.HandleFunc("/action/", func(w http.ResponseWriter, r *http.Request) {
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
		Leisten struct {
			Vier  *llLeiste `json:"vier"`
			Fuenf *llLeiste `json:"fuenf"`
			Acht  *llLeiste `json:"acht"`
		} `json:"leisten"`
		KopfIssue string `json:"kopfIssue"`
		KopfPR    string `json:"kopfPR"`
		Gross     []struct {
			Name    string `json:"name"`
			Zustand string `json:"zustand"`
			Titel   string `json:"titel"`
			Grund   string `json:"grund"`
			Zeit    string `json:"zeit"`
		} `json:"gross"`
		Nachsorge []struct {
			Art      string `json:"art"`
			Erledigt string `json:"erledigt"`
			Text     string `json:"text"`
			Knopf    string `json:"knopf"`
			Befehl   string `json:"befehl"`
			Kopieren bool   `json:"kopieren"`
		} `json:"nachsorge"`
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

	t.Run("Stufenleisten", func(t *testing.T) {
		for _, f := range []struct {
			name  string
			l     *llLeiste
			stufe string
			wert  string
		}{
			{"#4", b.Leisten.Vier, "branch", "branch=jetzt,pr=offen,pruefung=offen,review=offen,konflikt=offen,merge=offen,aufraeumen=offen,fertig=offen"},
			{"#5", b.Leisten.Fuenf, "aufraeumen", "branch=erledigt,pr=erledigt,pruefung=erledigt,review=erledigt,konflikt=erledigt,merge=erledigt,aufraeumen=jetzt,fertig=offen"},
			{"PR #8", b.Leisten.Acht, "review", "branch=erledigt,pr=erledigt,pruefung=erledigt,review=blockiert,konflikt=erledigt,merge=offen,aufraeumen=offen,fertig=offen"},
		} {
			if f.l == nil {
				t.Errorf("%s: keine Leiste", f.name)
				continue
			}
			if f.l.Stufe != f.stufe || strings.Join(f.l.Punkte, ",") != f.wert {
				t.Errorf("%s: stufe %q punkte %v", f.name, f.l.Stufe, f.l.Punkte)
			}
		}
		if l := b.Leisten.Acht; l != nil && !strings.Contains(l.Titel, "Änderungen verlangt von bo") {
			t.Errorf("PR #8: title %q", l.Titel)
		}
	})
	t.Run("Kopf", func(t *testing.T) {
		if b.KopfIssue != "Issue · #5" {
			t.Errorf("Kopf Issue %q", b.KopfIssue)
		}
		if b.KopfPR != "PR · #8" {
			t.Errorf("Kopf PR %q", b.KopfPR)
		}
	})
	t.Run("GrosseLeiste", func(t *testing.T) {
		if len(b.Gross) != 8 {
			t.Fatalf("Stufen %+v", b.Gross)
		}
		for _, s := range b.Gross {
			if s.Titel == "" {
				t.Errorf("%s ohne Titel", s.Name)
			}
		}
		if s := b.Gross[5]; s.Name != "merge" || s.Zustand != "erledigt" || s.Zeit != "2026-09-24T12:00:00Z" {
			t.Errorf("merge %+v", s)
		}
		if s := b.Gross[0]; s.Name != "branch" || s.Zeit != "2026-09-20T08:00:00Z" {
			t.Errorf("branch %+v", s)
		}
		if s := b.Gross[6]; s.Name != "aufraeumen" || s.Zustand != "jetzt" || !strings.Contains(s.Grund, "Issue #5 schließen") || s.Zeit != "" {
			t.Errorf("aufraeumen %+v", s)
		}
	})
	t.Run("Nachsorge", func(t *testing.T) {
		if len(b.Nachsorge) != 3 {
			t.Fatalf("Nachsorge %+v", b.Nachsorge)
		}
		i, br, c := b.Nachsorge[0], b.Nachsorge[1], b.Nachsorge[2]
		if i.Art != "issue_schliessen" || i.Erledigt != "nein" || i.Knopf != "Issue schließen" {
			t.Errorf("issue %+v", i)
		}
		if br.Art != "branch_loeschen" || br.Erledigt != "nein" || br.Knopf != "Branch löschen" || !strings.Contains(br.Text, "change/fuenf") {
			t.Errorf("branch %+v", br)
		}
		if c.Art != "change_archivieren" || c.Befehl != "just archiv fuenf" || !c.Kopieren || c.Knopf != "" {
			t.Errorf("change %+v", c)
		}
	})
	rumpfVon := func(t *testing.T, abschnitt, pfad string) map[string]any {
		t.Helper()
		as := nur(ab[abschnitt], http.MethodPost, pfad)
		if len(as) != 1 {
			t.Fatalf("%s: %s %+v, erwartet genau einen Aufruf", abschnitt, pfad, ab[abschnitt])
		}
		var r map[string]any
		if err := json.Unmarshal([]byte(as[0].Rumpf), &r); err != nil {
			t.Fatalf("Rumpf %s: %v", as[0].Rumpf, err)
		}
		if r["repo"] != "acme/alpha" {
			t.Errorf("%s: repo %v", pfad, r)
		}
		if !frischGeladen(ab[abschnitt]) {
			t.Errorf("%s: danach kein /api/arbeit mit frisch=1", abschnitt)
		}
		return r
	}
	t.Run("IssueSchliessen", func(t *testing.T) {
		r := rumpfVon(t, "issue", "/action/issue/aendern")
		if fmt.Sprint(r["nummer"]) != "5" || r["zustand"] != "closed" {
			t.Errorf("Rumpf %v", r)
		}
	})
	t.Run("BranchLoeschen", func(t *testing.T) {
		r := rumpfVon(t, "branch", "/action/pr/branch-loeschen")
		if fmt.Sprint(r["nummer"]) != "7" {
			t.Errorf("Rumpf %v", r)
		}
	})

	for _, f := range []struct{ variable, foto string }{
		{"GKP_BELEG_PNG_LEBENSLAUF_LISTE", "liste"},
		{"GKP_BELEG_PNG_LEBENSLAUF_DETAIL", "detail"},
	} {
		if ziel := os.Getenv(f.variable); ziel != "" {
			if out, err := chromium(ctx, srv.URL+"/?foto="+f.foto, "--screenshot="+ziel); err != nil {
				t.Errorf("Bildschirmfoto %s: %v\n%s", f.foto, err, out)
			}
		}
	}
}
