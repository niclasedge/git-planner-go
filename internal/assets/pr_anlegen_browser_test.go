package assets_test

// Browser-Test "Branch anlegen" und "PR anlegen" in der Detailspalte
// (pr-anlegen, #79): Knopf je nach Vorschlag, vorbelegtes Formular, 409 mit
// Grund im Formular, Neuladen mit frisch=1, danach der neue PR in der Liste.
// Den Vorschlag rechnet Go (appctx.RepoArbeitBauen); die Fake-API reicht in
// drei Stufen weiter, was Go aus dem jeweiligen Stand macht.
//
// Mit GKP_BELEG_PNG_FORMULAR und GKP_BELEG_PNG_LISTE legt der Test je ein
// Bildschirmfoto ab (vorbelegtes Formular "PR anlegen", neuer PR in der
// Liste).

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
	"sync"
	"testing"
	"time"

	"github.com/niclasedge/git-planner-go/internal/appctx"
	"github.com/niclasedge/git-planner-go/internal/assets"
	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

const paBranch = "79-pr-anlegen-branch-und-pr-anlegen"

// paArbeit baut /api/arbeit fuer die Stufe: 0 ohne Branch, 1 mit Branch,
// 2 mit Branch und PR #90.
func paArbeit(t *testing.T, stufe int) string {
	t.Helper()
	jetzt := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	e := appctx.ArbeitEingabe{
		Repo:     "acme/alpha",
		Standard: "main",
		Jetzt:    jetzt,
		Offen: []ghrest.Issue{{Nummer: 79, Titel: "pr-anlegen: Branch und PR anlegen", URL: "u79",
			Text: "Branch und PR aus einem Issue anlegen."}},
		Branches: []ghrest.Branch{{Name: "main", SHA: "shamain"}},
		PRs:      []ghrest.PR{},
	}
	if stufe >= 1 {
		e.Branches = append(e.Branches, ghrest.Branch{Name: paBranch, SHA: "shamain"})
		e.BranchKoepfe = map[string]appctx.BranchKopf{paBranch: {Zeit: jetzt.Add(-time.Minute).Format(time.RFC3339), Betreff: "Stand von main"}}
	}
	if stufe >= 2 {
		e.PRs = append(e.PRs, ghrest.PR{Nummer: 90, Titel: "pr-anlegen: Branch und PR anlegen (#79)", URL: "https://github.com/acme/alpha/pull/90",
			Autor: "cy", Labels: []ghrest.Label{}, Draft: true, Head: paBranch, HeadRepo: "acme/alpha", HeadSHA: "shamain", Base: "main",
			Text: "Closes #79", Zustand: "open"})
	}
	b, err := json.Marshal(appctx.RepoArbeitBauen(e))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

const paDetail = `{"art":"issue","nummer":79,"titel":"pr-anlegen: Branch und PR anlegen","url":"u79",
 "text":"Branch und PR aus einem Issue anlegen.","labels":[],"zustaendig":[],"blockiert_durch":[],"kommentare":[]}`

const paTreiber = `
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
  async function oeffnen() {
    await warte('Leiste', function () { return q('[data-repo="acme/alpha"]'); });
    q('[data-repo="acme/alpha"]').click();
    await warte('Liste', function () { return q('[data-eintrag="79"]'); });
    q('[data-eintrag="79"] .kn-e-details').click();
    await warte('Detailspalte', function () { return q('.kn-detail'); });
  }
  async function prFormular() {
    await warte('Knopf PR anlegen', function () { return q('.kn-detail .kn-an-knopf[data-aktion="pr"]'); });
    q('.kn-detail .kn-an-knopf[data-aktion="pr"]').click();
    await warte('PR-Formular', function () { return q('.kn-detail .kn-an-form [name="titel"]'); });
  }
  async function foto(art) {
    await oeffnen();
    if (art === 'formular') {
      await prFormular();
      q('.kn-detail .kn-an-form [name="draft"]').click();
      await schlaf(100);
      q('.kn-detail .kn-an-form').scrollIntoView({ block: 'center' });
      return;
    }
    await warte('neuer PR', function () { return q('[data-eintrag="pr-90"]'); });
    await schlaf(100);
    q('[data-eintrag="pr-90"]').scrollIntoView({ block: 'center' });
  }
  async function lauf() {
    var b = {};
    try {
      var art = new URLSearchParams(location.search).get('foto');
      if (art) { await foto(art); return; }
      await oeffnen();
      await warte('Knopf Branch anlegen', function () { return q('.kn-detail .kn-an-knopf'); });
      b.knopfAnfang = q('.kn-detail .kn-an-knopf').getAttribute('data-aktion');
      b.knopfText = text('.kn-detail .kn-an-knopf');
      q('.kn-detail .kn-an-knopf').click();
      await warte('Branch-Formular', function () { return q('.kn-detail .kn-an-form [name="branch"]'); });
      b.branchVorbelegt = q('.kn-detail .kn-an-form [name="branch"]').value;
      b.base = text('.kn-detail .kn-an-form .kn-an-base');

      setze('.kn-detail .kn-an-form [name="branch"]', 'vorhanden');
      await marke('vor-vorhanden');
      q('.kn-detail .kn-an-senden').click();
      await warte('Fehler 409', function () { return q('.kn-detail .kn-an-fehler'); });
      b.fehler409 = text('.kn-detail .kn-an-fehler');

      setze('.kn-detail .kn-an-form [name="branch"]', b.branchVorbelegt);
      await marke('vor-branch');
      q('.kn-detail .kn-an-senden').click();
      await prFormular();
      await marke('nach-branch');
      b.prTitel = q('.kn-detail .kn-an-form [name="titel"]').value;
      b.prText = q('.kn-detail .kn-an-form [name="text"]').value;
      b.prHead = text('.kn-detail .kn-an-form .kn-an-head');
      b.draftAnfang = q('.kn-detail .kn-an-form [name="draft"]').checked;
      q('.kn-detail .kn-an-form [name="draft"]').click();
      await marke('vor-pr');
      q('.kn-detail .kn-an-senden').click();
      await warte('neuer PR in der Liste', function () { return q('[data-eintrag="pr-90"]'); });
      await warte('PR an der Detailspalte', function () { return q('.kn-d-branches .kn-d-branch-pr'); });
      await schlaf(200);
      await marke('nach-pr');
      b.prZeile = text('[data-eintrag="pr-90"]');
      b.detailPR = text('.kn-d-branches .kn-d-branch-pr');
      b.knopfDanach = !!q('.kn-detail .kn-an-knopf');
    } catch (e) {
      b.abbruch = String(e && e.message || e);
      b.dom = document.body.innerHTML.slice(0, 3000);
    }
    berichten(b);
  }
  lauf();
})();
`

func TestPRAnlegenImBrowser(t *testing.T) {
	if testing.Short() {
		t.Skip("-short: der Test startet einen Browser")
	}
	chrome := chromiumFinden()
	if chrome == "" {
		t.Skip("kein Chromium gefunden — die Oberflaeche ist damit NICHT belegt")
	}
	stufen := []string{paArbeit(t, 0), paArbeit(t, 1), paArbeit(t, 2)}
	var mu sync.Mutex
	stufe := 0
	api := &fakeAPI{}
	mux := http.NewServeMux()
	protokolliere := func(r *http.Request) string {
		rumpf, _ := io.ReadAll(r.Body)
		api.log(anfrage{Methode: r.Method, Pfad: r.URL.Path, Query: r.URL.RawQuery, Rumpf: string(rumpf)})
		return string(rumpf)
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
		fmt.Fprint(w, paTreiber)
	})
	mux.HandleFunc("/__marke", func(w http.ResponseWriter, r *http.Request) {
		api.log(anfrage{Marke: r.URL.Query().Get("name")})
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/api/repos", func(w http.ResponseWriter, r *http.Request) { protokolliere(r); json200(w, ivRepos) })
	mux.HandleFunc("/api/arbeit", func(w http.ResponseWriter, r *http.Request) {
		protokolliere(r)
		mu.Lock()
		s := stufen[stufe]
		mu.Unlock()
		json200(w, s)
	})
	mux.HandleFunc("/api/repo-meta", func(w http.ResponseWriter, r *http.Request) { protokolliere(r); json200(w, ivMeta) })
	mux.HandleFunc("/api/detail", func(w http.ResponseWriter, r *http.Request) { protokolliere(r); json200(w, paDetail) })
	mux.HandleFunc("/action/branch/anlegen", func(w http.ResponseWriter, r *http.Request) {
		rumpf := protokolliere(r)
		if strings.Contains(rumpf, `"vorhanden"`) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			fmt.Fprint(w, `{"fehler":"Branch vorhanden existiert schon"}`)
			return
		}
		mu.Lock()
		stufe = 1
		mu.Unlock()
		json200(w, `{"ok":true,"branch":"`+paBranch+`","sha":"shamain"}`)
	})
	mux.HandleFunc("/action/pr/anlegen", func(w http.ResponseWriter, r *http.Request) {
		protokolliere(r)
		mu.Lock()
		stufe = 2
		mu.Unlock()
		json200(w, `{"ok":true,"pr":{"nummer":90,"url":"https://github.com/acme/alpha/pull/90","titel":"pr-anlegen: Branch und PR anlegen (#79)","draft":true}}`)
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
		Abbruch         string   `json:"abbruch"`
		DOM             string   `json:"dom"`
		Fehler          []string `json:"fehler"`
		KnopfAnfang     string   `json:"knopfAnfang"`
		KnopfText       string   `json:"knopfText"`
		BranchVorbelegt string   `json:"branchVorbelegt"`
		Base            string   `json:"base"`
		Fehler409       string   `json:"fehler409"`
		PRTitel         string   `json:"prTitel"`
		PRText          string   `json:"prText"`
		PRHead          string   `json:"prHead"`
		DraftAnfang     bool     `json:"draftAnfang"`
		PRZeile         string   `json:"prZeile"`
		DetailPR        string   `json:"detailPR"`
		KnopfDanach     bool     `json:"knopfDanach"`
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

	t.Run("BranchFormular", func(t *testing.T) {
		if b.KnopfAnfang != "branch" || !strings.Contains(b.KnopfText, "Branch anlegen") {
			t.Errorf("Knopf %q %q, erwartet branch / Branch anlegen", b.KnopfAnfang, b.KnopfText)
		}
		if b.BranchVorbelegt != paBranch || !strings.Contains(b.Base, "main") {
			t.Errorf("Formular: Name %q, Base %q", b.BranchVorbelegt, b.Base)
		}
	})
	t.Run("Ablehnung409", func(t *testing.T) {
		if !strings.Contains(b.Fehler409, "existiert schon") {
			t.Errorf("Fehler im Formular %q", b.Fehler409)
		}
	})
	t.Run("BranchAnlegen", func(t *testing.T) {
		bs := nur(ab["vor-branch"], http.MethodPost, "/action/branch/anlegen")
		if len(bs) != 1 {
			t.Fatalf("branch/anlegen %+v, erwartet genau einen Aufruf", bs)
		}
		var r struct {
			Repo   string `json:"repo"`
			Nummer int    `json:"nummer"`
			Name   string `json:"name"`
		}
		if err := json.Unmarshal([]byte(bs[0].Rumpf), &r); err != nil || r.Repo != "acme/alpha" || r.Nummer != 79 || r.Name != paBranch {
			t.Errorf("Rumpf %s (%v)", bs[0].Rumpf, err)
		}
		if !frischGeladen(ab["vor-branch"]) {
			t.Error("nach dem Branch kein /api/arbeit mit frisch=1")
		}
	})
	t.Run("PRFormularVorbelegt", func(t *testing.T) {
		if b.PRTitel != "pr-anlegen: Branch und PR anlegen (#79)" || !strings.HasPrefix(b.PRText, "Closes #79") {
			t.Errorf("Titel %q, Text %q", b.PRTitel, b.PRText)
		}
		if !strings.Contains(b.PRHead, paBranch) || !strings.Contains(b.PRHead, "main") || b.DraftAnfang {
			t.Errorf("Head %q, Draft anfangs %v", b.PRHead, b.DraftAnfang)
		}
	})
	t.Run("PRAnlegen", func(t *testing.T) {
		ps := nur(ab["vor-pr"], http.MethodPost, "/action/pr/anlegen")
		if len(ps) != 1 {
			t.Fatalf("pr/anlegen %+v, erwartet genau einen Aufruf", ps)
		}
		var r struct {
			Repo  string `json:"repo"`
			Head  string `json:"head"`
			Titel string `json:"titel"`
			Text  string `json:"text"`
			Draft bool   `json:"draft"`
		}
		if err := json.Unmarshal([]byte(ps[0].Rumpf), &r); err != nil || r.Repo != "acme/alpha" || r.Head != paBranch || !r.Draft ||
			!strings.HasPrefix(r.Text, "Closes #79") || !strings.HasSuffix(r.Titel, "(#79)") {
			t.Errorf("Rumpf %s (%v)", ps[0].Rumpf, err)
		}
		if !frischGeladen(ab["vor-pr"]) {
			t.Error("nach dem PR kein /api/arbeit mit frisch=1")
		}
	})
	t.Run("NeuerPRInDerListe", func(t *testing.T) {
		if !strings.Contains(b.PRZeile, "pr-anlegen: Branch und PR anlegen (#79)") {
			t.Errorf("PR-Zeile %q", b.PRZeile)
		}
		if !strings.Contains(b.DetailPR, "#90") || b.KnopfDanach {
			t.Errorf("Detailspalte: PR %q, Knopf noch da %v", b.DetailPR, b.KnopfDanach)
		}
	})

	// Auf Wunsch je ein Bildschirmfoto: Formular (Stufe 1), Liste (Stufe 2).
	for _, foto := range []struct {
		art, variable string
		stufe         int
	}{{"formular", "GKP_BELEG_PNG_FORMULAR", 1}, {"liste", "GKP_BELEG_PNG_LISTE", 2}} {
		if ziel := os.Getenv(foto.variable); ziel != "" {
			mu.Lock()
			stufe = foto.stufe
			mu.Unlock()
			if out, err := chromium(ctx, srv.URL+"/?foto="+foto.art, "--screenshot="+ziel); err != nil {
				t.Errorf("Bildschirmfoto %s: %v\n%s", foto.art, err, out)
			}
		}
	}
}
