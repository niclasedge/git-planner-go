package assets_test

// Browser-Test des Abschnitts "Branches aufräumen" (branch-aufraeumen, #78):
// Zahl in der Ueberschrift, vier Gruppen in der Folge aus Go, gemergte
// vorausgewaehlt, Sammel-Loeschen mit Rueckfrage in einer Anfrage, verlassene
// und aktive nur einzeln mit Warnhinweis, geschuetzte nur angezeigt. Die
// Gruppen rechnet Go (appctx.BranchAufraeumenBauen); die Fake-API ruft genau
// das und laesst nur die geloeschten Branches weg.
//
// Mit GKP_BELEG_PNG_VORHER und GKP_BELEG_PNG_NACHHER legt der Test je ein
// Bildschirmfoto ab: die Liste mit allen vier Gruppen und die Liste nach dem
// Sammel-Loeschen.

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

const baArbeit = `{"repo":"acme/alpha","fortschritt":{"erledigt":0,"gesamt":0},"zuletzt":[],
 "reihe":[{"art":"issue","nummer":12,"titel":"Leiste zeigt alte Zahl","url":"u12","status":"ohne_change","blockiert_durch":[],"labels":[],"zustaendig":[],"hat_text":false}],
 "ohne_issue":[],"prs":[],"quellen":[]}`

type baFake struct {
	mu       sync.Mutex
	geloesch map[string]bool
}

func (f *baFake) zuruecksetzen() {
	f.mu.Lock()
	f.geloesch = map[string]bool{}
	f.mu.Unlock()
}

func (f *baFake) eingabe() appctx.AufraeumEingabe {
	f.mu.Lock()
	defer f.mu.Unlock()
	alle := []ghrest.Branch{
		{Name: "main", SHA: "a1b2c3d4e5f6", Geschuetzt: true},
		{Name: "change/branch-aufraeumen", SHA: "0ffe11a2b3c4"},
		{Name: "change/repo-karte", SHA: "c0ffee123456"},
		{Name: "claude/sync-status-38", SHA: "5ca1ab1e0000"},
		{Name: "change/alter-versuch", SHA: "dead0beef123"},
		{Name: "claude/neue-idee", SHA: "1dea0000abcd"},
	}
	e := appctx.AufraeumEingabe{Repo: "acme/alpha", Standard: "main",
		PRs: []ghrest.PR{
			{Nummer: 78, Titel: "branch-aufraeumen: Branches aufräumen", URL: "https://github.com/acme/alpha/pull/78", Head: "change/branch-aufraeumen", HeadRepo: "acme/alpha", Zustand: "open"},
			{Nummer: 38, Titel: "sync-status: Stand je Quelle", URL: "https://github.com/acme/alpha/pull/38", Head: "claude/sync-status-38", HeadRepo: "acme/alpha", Zustand: "closed", Gemergt: true},
			{Nummer: 44, Titel: "Alter Versuch", URL: "https://github.com/acme/alpha/pull/44", Head: "change/alter-versuch", HeadRepo: "acme/alpha", Zustand: "closed", Text: "Closes #12"},
		},
		Koepfe: map[string]appctx.AufraeumKopf{
			"change/branch-aufraeumen": {Zeit: "2026-09-26T09:00:00Z", Betreff: "branch-aufraeumen: Test rot (#78)", Voraus: 4, Verglichen: true},
			"change/repo-karte":        {Zeit: "2026-09-12T14:30:00Z", Betreff: "repo-karte: Change archiviert (#36)", Voraus: 0, Zurueck: 41, Verglichen: true},
			"claude/sync-status-38":    {Zeit: "2026-09-18T08:10:00Z", Betreff: "sync-status: Zaehler je Quelle", Voraus: 3, Zurueck: 22, Verglichen: true},
			"change/alter-versuch":     {Zeit: "2026-09-20T16:45:00Z", Betreff: "Versuch mit Cache", Voraus: 2, Zurueck: 9, Verglichen: true},
			"claude/neue-idee":         {Zeit: "2026-09-25T11:05:00Z", Betreff: "Skizze fuer die Leiste", Voraus: 1, Zurueck: 0, Verglichen: true},
		},
		Offen: []ghrest.Issue{{Nummer: 12, Titel: "Leiste zeigt alte Zahl"}},
	}
	for _, b := range alle {
		if !f.geloesch[b.Name] {
			e.Branches = append(e.Branches, b)
		}
	}
	return e
}

const baTreiber = `
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
  function gruppe(name) { return '.kn-ba [data-gruppe="' + name + '"]'; }
  function zweig(name) { return '.kn-ba [data-branch="' + name + '"]'; }
  function namen(g) { return qa(gruppe(g) + ' [data-branch]').map(function (x) { return x.getAttribute('data-branch'); }); }
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
    await warte('Abschnitt', function () { return qa('.kn-ba [data-gruppe]').length === 4 && q(zweig('main')); });
  }
  async function sammelLoeschen() {
    q('.kn-ba .kn-ba-sammel').click();
    await warte('Rueckfrage', function () { return q('.kn-ba .kn-ba-rueckfrage'); });
    var frage = text('.kn-ba .kn-ba-rueckfrage');
    await marke('ja');
    q('.kn-ba .kn-ba-rueckfrage .kn-ba-ja').click();
    await warte('gemergt leer', function () { return namen('gemergt').length === 0 && !q('.kn-ba .kn-ba-rueckfrage'); });
    return frage;
  }
  async function foto(art) {
    await oeffnen();
    if (art === 'nachher') await sammelLoeschen();
    await schlaf(150);
    q('.kn-ba').scrollIntoView({ block: 'start' });
  }
  async function lauf() {
    var b = {};
    try {
      var art = new URLSearchParams(location.search).get('foto');
      if (art) { await foto(art); return; }
      await oeffnen();
      b.titel = text('.kn-ba h2');
      b.zahl = text('.kn-ba .kn-ba-zahl');
      b.folge = qa('.kn-ba [data-gruppe]').map(function (g) { return g.getAttribute('data-gruppe'); });
      b.namen = {};
      ['gemergt', 'verlassen', 'aktiv', 'geschuetzt'].forEach(function (g) { b.namen[g] = namen(g); });
      b.gewaehlt = qa(gruppe('gemergt') + ' input[type=checkbox]').map(function (x) { return x.checked; });
      b.sammelText = text('.kn-ba .kn-ba-sammel');
      b.geschuetztKnoepfe = qa(gruppe('geschuetzt') + ' button, ' + gruppe('geschuetzt') + ' input').length;
      b.einzelnVerlassen = qa(gruppe('verlassen') + ' .kn-ba-einzeln').length;
      b.einzelnAktiv = qa(gruppe('aktiv') + ' .kn-ba-einzeln').length;
      b.auswahlAusserhalb = qa(gruppe('verlassen') + ' input, ' + gruppe('aktiv') + ' input').length;
      b.zeileVerlassen = text(zweig('change/alter-versuch'));
      b.zeileSquash = text(zweig('claude/sync-status-38'));

      // Einzeln: Rueckfrage mit Warnung, Abbrechen schickt nichts.
      await marke('einzeln-abbruch');
      q(zweig('change/alter-versuch') + ' .kn-ba-einzeln').click();
      await warte('Rueckfrage einzeln', function () { return q('.kn-ba .kn-ba-rueckfrage'); });
      b.frageEinzeln = text('.kn-ba .kn-ba-rueckfrage');
      q('.kn-ba .kn-ba-rueckfrage .kn-ba-nein').click();
      await schlaf(100);
      b.frageNachAbbruch = !!q('.kn-ba .kn-ba-rueckfrage');

      // Eine Auswahl abwaehlen und wieder anwaehlen aendert die Zahl im Knopf.
      q(gruppe('gemergt') + ' input[type=checkbox]').click();
      await schlaf(50);
      b.sammelEiner = text('.kn-ba .kn-ba-sammel');
      q(gruppe('gemergt') + ' input[type=checkbox]').click();
      await schlaf(50);

      await marke('sammel');
      b.frageSammel = await sammelLoeschen();
      b.zahlNachher = text('.kn-ba .kn-ba-zahl');
      b.namenNachher = {};
      ['gemergt', 'verlassen', 'aktiv', 'geschuetzt'].forEach(function (g) { b.namenNachher[g] = namen(g); });

      await marke('einzeln');
      q(zweig('change/alter-versuch') + ' .kn-ba-einzeln').click();
      await warte('Rueckfrage einzeln', function () { return q('.kn-ba .kn-ba-rueckfrage'); });
      q('.kn-ba .kn-ba-rueckfrage .kn-ba-ja').click();
      await warte('verlassen leer', function () { return namen('verlassen').length === 0; });
      b.zahlZuletzt = text('.kn-ba .kn-ba-zahl');
    } catch (e) {
      b.abbruch = String(e && e.message || e);
      b.dom = (q('.kn-ba') || document.body).innerHTML.slice(0, 4000);
    }
    berichten(b);
  }
  lauf();
})();
`

func TestBranchAufraeumenImBrowser(t *testing.T) {
	if testing.Short() {
		t.Skip("-short: der Test startet einen Browser")
	}
	chrome := chromiumFinden()
	if chrome == "" {
		t.Skip("kein Chromium gefunden — die Oberflaeche ist damit NICHT belegt")
	}
	stand := &baFake{}
	stand.zuruecksetzen()
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
		fmt.Fprint(w, baTreiber)
	})
	mux.HandleFunc("/__marke", func(w http.ResponseWriter, r *http.Request) {
		api.log(anfrage{Marke: r.URL.Query().Get("name")})
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/api/repos", func(w http.ResponseWriter, r *http.Request) { protokolliere(r); json200(w, ivRepos) })
	mux.HandleFunc("/api/arbeit", func(w http.ResponseWriter, r *http.Request) { protokolliere(r); json200(w, baArbeit) })
	mux.HandleFunc("/api/repo-meta", func(w http.ResponseWriter, r *http.Request) { protokolliere(r); json200(w, ivMeta) })
	mux.HandleFunc("/api/branches", func(w http.ResponseWriter, r *http.Request) {
		protokolliere(r)
		b, _ := json.Marshal(appctx.BranchAufraeumenBauen(stand.eingabe()))
		json200(w, string(b))
	})
	mux.HandleFunc("/action/branch/loeschen", func(w http.ResponseWriter, r *http.Request) {
		rumpf := protokolliere(r)
		var wunsch struct {
			Branch   string `json:"branch"`
			SHA      string `json:"sha"`
			Branches []struct {
				Branch string `json:"branch"`
				SHA    string `json:"sha"`
			} `json:"branches"`
		}
		_ = json.Unmarshal([]byte(rumpf), &wunsch)
		if wunsch.Branch != "" {
			wunsch.Branches = append(wunsch.Branches, struct {
				Branch string `json:"branch"`
				SHA    string `json:"sha"`
			}{wunsch.Branch, wunsch.SHA})
		}
		ergebnisse := []map[string]any{}
		stand.mu.Lock()
		for _, x := range wunsch.Branches {
			stand.geloesch[x.Branch] = true
			ergebnisse = append(ergebnisse, map[string]any{"branch": x.Branch, "status": 200, "ok": true})
		}
		stand.mu.Unlock()
		b, _ := json.Marshal(map[string]any{"ok": true, "ergebnisse": ergebnisse})
		json200(w, string(b))
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
		Abbruch           string              `json:"abbruch"`
		DOM               string              `json:"dom"`
		Fehler            []string            `json:"fehler"`
		Titel             string              `json:"titel"`
		Zahl              string              `json:"zahl"`
		Folge             []string            `json:"folge"`
		Namen             map[string][]string `json:"namen"`
		Gewaehlt          []bool              `json:"gewaehlt"`
		SammelText        string              `json:"sammelText"`
		SammelEiner       string              `json:"sammelEiner"`
		GeschuetztKnoepfe int                 `json:"geschuetztKnoepfe"`
		EinzelnVerlassen  int                 `json:"einzelnVerlassen"`
		EinzelnAktiv      int                 `json:"einzelnAktiv"`
		AuswahlAusserhalb int                 `json:"auswahlAusserhalb"`
		ZeileVerlassen    string              `json:"zeileVerlassen"`
		ZeileSquash       string              `json:"zeileSquash"`
		FrageEinzeln      string              `json:"frageEinzeln"`
		FrageNachAbbruch  bool                `json:"frageNachAbbruch"`
		FrageSammel       string              `json:"frageSammel"`
		ZahlNachher       string              `json:"zahlNachher"`
		NamenNachher      map[string][]string `json:"namenNachher"`
		ZahlZuletzt       string              `json:"zahlZuletzt"`
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

	t.Run("AbschnittUndGruppen", func(t *testing.T) {
		if !strings.Contains(b.Titel, "Branches aufräumen") || b.Zahl != "4" {
			t.Errorf("Ueberschrift %q, Zahl %q", b.Titel, b.Zahl)
		}
		if strings.Join(b.Folge, ",") != "gemergt,verlassen,aktiv,geschuetzt" {
			t.Errorf("Folge %v", b.Folge)
		}
		want := map[string]string{
			"gemergt":    "claude/sync-status-38,change/repo-karte",
			"verlassen":  "change/alter-versuch",
			"aktiv":      "claude/neue-idee",
			"geschuetzt": "change/branch-aufraeumen,main",
		}
		for g, w := range want {
			if strings.Join(b.Namen[g], ",") != w {
				t.Errorf("%s: %v, erwartet %s", g, b.Namen[g], w)
			}
		}
		if !strings.Contains(b.ZeileVerlassen, "dead0be") || !strings.Contains(b.ZeileVerlassen, "Versuch mit Cache") ||
			!strings.Contains(b.ZeileVerlassen, "#44") || !strings.Contains(b.ZeileVerlassen, "#12") {
			t.Errorf("Zeile verlassen %q", b.ZeileVerlassen)
		}
		if !strings.Contains(b.ZeileSquash, "#38") || !strings.Contains(b.ZeileSquash, "gemergt") {
			t.Errorf("Zeile squash %q", b.ZeileSquash)
		}
		nurLesen := nur(ab[""], http.MethodGet, "/api/branches")
		if len(nurLesen) == 0 || !strings.Contains(nurLesen[0].Query, "repo=acme%2Falpha") {
			t.Errorf("/api/branches %+v", nurLesen)
		}
	})
	t.Run("VorauswahlUndSchutz", func(t *testing.T) {
		if len(b.Gewaehlt) != 2 || !b.Gewaehlt[0] || !b.Gewaehlt[1] {
			t.Errorf("Auswahl gemergt %v, erwartet beide gewaehlt", b.Gewaehlt)
		}
		if b.SammelText != "2 gemergte löschen" || b.SammelEiner != "1 gemergte löschen" {
			t.Errorf("Knopf %q, nach Abwahl %q", b.SammelText, b.SammelEiner)
		}
		if b.GeschuetztKnoepfe != 0 {
			t.Errorf("geschuetzt hat %d Knoepfe oder Auswahlen", b.GeschuetztKnoepfe)
		}
		if b.EinzelnVerlassen != 1 || b.EinzelnAktiv != 1 || b.AuswahlAusserhalb != 0 {
			t.Errorf("einzeln: verlassen %d, aktiv %d, Auswahl ausserhalb gemergt %d", b.EinzelnVerlassen, b.EinzelnAktiv, b.AuswahlAusserhalb)
		}
	})
	t.Run("EinzelnMitWarnung", func(t *testing.T) {
		if !strings.Contains(b.FrageEinzeln, "change/alter-versuch") || !strings.Contains(b.FrageEinzeln, "2 Commits") {
			t.Errorf("Rueckfrage einzeln %q", b.FrageEinzeln)
		}
		if b.FrageNachAbbruch {
			t.Error("Rueckfrage nach Abbrechen noch offen")
		}
		if n := nur(ab["einzeln-abbruch"], http.MethodPost, "/action/branch/loeschen"); len(n) != 0 {
			t.Errorf("Abbrechen hat geloescht: %+v", n)
		}
		ls := nur(ab["einzeln"], http.MethodPost, "/action/branch/loeschen")
		if len(ls) != 1 || !strings.Contains(ls[0].Rumpf, `"branch":"change/alter-versuch"`) || !strings.Contains(ls[0].Rumpf, `"sha":"dead0beef123"`) ||
			strings.Contains(ls[0].Rumpf, "neue-idee") {
			t.Errorf("einzeln %+v", ls)
		}
		if b.ZahlZuletzt != "1" {
			t.Errorf("Zahl zuletzt %q, erwartet 1", b.ZahlZuletzt)
		}
	})
	t.Run("SammelLoeschen", func(t *testing.T) {
		if !strings.Contains(b.FrageSammel, "2") || !strings.Contains(b.FrageSammel, "claude/sync-status-38") || !strings.Contains(b.FrageSammel, "change/repo-karte") {
			t.Errorf("Rueckfrage sammel %q", b.FrageSammel)
		}
		if n := nur(ab["sammel"], http.MethodPost, "/action/branch/loeschen"); len(n) != 0 {
			t.Errorf("vor der Bestaetigung geloescht: %+v", n)
		}
		ls := nur(ab["ja"], http.MethodPost, "/action/branch/loeschen")
		if len(ls) != 1 {
			t.Fatalf("Sammel-Loeschen %+v, erwartet genau eine Anfrage", ls)
		}
		var r struct {
			Repo     string `json:"repo"`
			Branches []struct {
				Branch string `json:"branch"`
				SHA    string `json:"sha"`
			} `json:"branches"`
		}
		if err := json.Unmarshal([]byte(ls[0].Rumpf), &r); err != nil {
			t.Fatalf("Rumpf %s: %v", ls[0].Rumpf, err)
		}
		je := []string{}
		for _, x := range r.Branches {
			je = append(je, x.Branch+"@"+x.SHA)
		}
		if r.Repo != "acme/alpha" || strings.Join(je, ",") != "claude/sync-status-38@5ca1ab1e0000,change/repo-karte@c0ffee123456" {
			t.Errorf("Rumpf %s", ls[0].Rumpf)
		}
		if len(nur(ab["ja"], http.MethodGet, "/api/branches")) == 0 {
			t.Error("nach dem Loeschen keine neue Liste geholt")
		}
		if b.ZahlNachher != "2" || len(b.NamenNachher["gemergt"]) != 0 || len(b.NamenNachher["verlassen"]) != 1 || len(b.NamenNachher["geschuetzt"]) != 2 {
			t.Errorf("nachher Zahl %q, Gruppen %v", b.ZahlNachher, b.NamenNachher)
		}
	})

	// Auf Wunsch je ein Bildschirmfoto: vor und nach dem Sammel-Loeschen.
	for _, art := range []string{"vorher", "nachher"} {
		if ziel := os.Getenv("GKP_BELEG_PNG_" + strings.ToUpper(art)); ziel != "" {
			stand.zuruecksetzen()
			if out, err := chromium(ctx, srv.URL+"/?foto="+art, "--screenshot="+ziel); err != nil {
				t.Errorf("Bildschirmfoto %s: %v\n%s", art, err, out)
			}
		}
	}
}
