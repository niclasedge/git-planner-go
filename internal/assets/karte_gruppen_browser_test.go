package assets_test

// Browser-Test der Liste "Issues nach Parent" unter der Karte (karte-gruppen,
// #121, Entwurf H): Gruppen in der Folge aus Go, Badge der Prio-1-Frage, die
// Zeile Ziel, Chips der Fragen, Einrueckung nach blocked_by, "+ n weitere",
// der Sprung Station -> Zeile und zurueck und die Detailspalte ueber das
// Symbol und den Doppelklick. Die Fake-API liefert die Karte des Szenarios
// (internal/appctx/testdata/karte_szenario_karte.json, dort gegen KarteBauen
// geprueft).
//
// Mit GKP_BELEG_PNG_KARTE_GRUPPEN legt der Test ein Bildschirmfoto der
// aufgeklappten Karte samt Liste ab, mit GKP_BELEG_PNG_KARTE_SPRUNG eines nach
// dem Klick auf die Station #102.

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

const gruppenTreiber = `
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
  function attr(sel, name) { return qa(sel).map(function (e) { return e.getAttribute(name); }); }
  function text(e) { return e ? e.textContent.replace(/\s+/g, ' ').trim() : ''; }
  function klick(e) { e.dispatchEvent(new MouseEvent('click', { bubbles: true })); }
  function doppel(e) { e.dispatchEvent(new MouseEvent('dblclick', { bubbles: true })); }
  function imBild(e) { var r = e.getBoundingClientRect(); return r.top >= 0 && r.bottom <= window.innerHeight; }
  function links(sel) { var e = q(sel); return e ? Math.round(e.getBoundingClientRect().left) : -1; }
  function berichten(b) {
    b.fehler = (b.fehler || []).concat(fehler);
    var pre = document.createElement('pre');
    pre.id = 'kn-testbericht';
    pre.textContent = btoa(unescape(encodeURIComponent(JSON.stringify(b))));
    document.body.appendChild(pre);
  }
  async function aufklappen() {
    await warte('Leiste', function () { return q('[data-repo="acme/alpha"]'); });
    q('[data-repo="acme/alpha"]').click();
    await warte('Karte-Kopf', function () { return q('.kn-karte-kopf'); });
    await schlaf(300);
    q('.kn-karte-kopf').click();
    await warte('Gruppen', function () { return qa('[data-gruppe]').length >= 4 && qa('[data-station]').length >= 20; });
    await schlaf(300);
  }
  async function lauf() {
    var b = {};
    var foto = new URLSearchParams(location.search).get('foto');
    try {
      await aufklappen();
      if (foto) {
        if (foto === 'sprung') {
          klick(q('[data-station="102"]'));
          await schlaf(400);
        }
        window.scrollTo(0, 0);
        q('.kn-karte').scrollIntoView({ block: 'start' });
        return;
      }
      b.gruppen = attr('[data-gruppe]', 'data-gruppe');
      b.p1 = text(q('[data-gruppe="60"] [data-p1]'));
      b.p1Ohne = text(q('[data-gruppe="0"] [data-p1]'));
      b.p1Zahl = qa('[data-p1]').length;
      b.kopf60 = text(q('[data-gruppe="60"] .kn-gruppe-kopf'));
      b.ziel = text(q('[data-ziel]'));
      b.chips88 = qa('[data-frage-zeile="88"] [data-chip]').map(function (e) { return e.getAttribute('data-chip') + ':' + e.getAttribute('data-art'); });
      b.frage88 = text(q('[data-frage-zeile="88"]'));
      b.aus60 = qa('[data-gruppe="60"] [data-abschnitt="ausstehend"] [data-zeile]').map(function (e) { return e.getAttribute('data-zeile') + '/' + e.getAttribute('data-tiefe'); });
      b.x107 = links('[data-zeile="107"] .kn-zeile-titel');
      b.x111 = links('[data-zeile="111"] .kn-zeile-titel');
      b.zeile98 = text(q('[data-zeile="98"]'));
      b.zeile102 = text(q('[data-zeile="102"]'));
      b.wartet111 = qa('[data-zeile="111"] [data-wartet]').map(function (e) { return e.getAttribute('data-wartet') + ':' + e.getAttribute('data-art'); });
      b.abschnitte0 = attr('[data-gruppe="0"] [data-abschnitt]', 'data-abschnitt');
      b.gemergtVor = qa('[data-gruppe="0"] [data-abschnitt="gemergt"] [data-zeile]').length;
      b.mehr = text(q('[data-gruppe="0"] [data-mehr]'));
      b.zeile97 = text(q('[data-zeile="97"]'));

      window.scrollTo(0, 0);
      await marke('vor-station');
      klick(q('[data-station="102"]'));
      await schlaf(500);
      b.zeilenNachStation = attr('.kn-markiert[data-zeile]', 'data-zeile');
      b.stationenNachStation = attr('.kn-karte-markiert[data-station]', 'data-station');
      b.imBild102 = imBild(q('[data-zeile="102"]'));

      await marke('vor-zeile');
      klick(q('[data-zeile="105"]'));
      await schlaf(300);
      b.zeilenNachZeile = attr('.kn-markiert[data-zeile]', 'data-zeile');
      b.stationenNachZeile = attr('.kn-karte-markiert[data-station]', 'data-station');

      klick(q('[data-gruppe="0"] [data-mehr]'));
      await schlaf(200);
      b.gemergtNach = qa('[data-gruppe="0"] [data-abschnitt="gemergt"] [data-zeile]').length;
      b.mehrNach = qa('[data-gruppe="0"] [data-mehr]').length;

      await marke('vor-symbol');
      klick(q('[data-zeile="104"] [data-detail]'));
      await warte('Detail', function () { return q('.kn-detail'); });
      await schlaf(300);
      await marke('vor-doppel');
      doppel(q('[data-zeile="106"]'));
      await schlaf(500);
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

func TestKarteGruppenImBrowser(t *testing.T) {
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
		fmt.Fprint(w, gruppenTreiber)
	})
	mux.HandleFunc("/__marke", func(w http.ResponseWriter, r *http.Request) {
		api.log(anfrage{Marke: r.URL.Query().Get("name")})
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/api/repos", func(w http.ResponseWriter, r *http.Request) { protokolliere(r); json200(w, ivRepos) })
	mux.HandleFunc("/api/arbeit", func(w http.ResponseWriter, r *http.Request) { protokolliere(r); json200(w, ivArbeit) })
	mux.HandleFunc("/api/repo-meta", func(w http.ResponseWriter, r *http.Request) { protokolliere(r); json200(w, ivMeta) })
	mux.HandleFunc("/api/karte", func(w http.ResponseWriter, r *http.Request) { protokolliere(r); json200(w, string(karteJSON)) })
	mux.HandleFunc("/api/detail", func(w http.ResponseWriter, r *http.Request) { protokolliere(r); json200(w, ivDetail) })
	srv := httptest.NewServer(mux)
	defer srv.Close()

	chromium := func(ctx context.Context, adresse, fenster string, extra ...string) ([]byte, error) {
		args := append([]string{
			"--headless", "--no-sandbox", "--disable-gpu", "--no-first-run",
			"--disable-extensions", "--disable-dev-shm-usage", "--hide-scrollbars",
			"--user-data-dir=" + t.TempDir(),
			"--window-size=" + fenster,
			"--virtual-time-budget=30000",
		}, extra...)
		return exec.CommandContext(ctx, chrome, append(args, adresse)...).Output()
	}
	ctx, abbrechen := context.WithTimeout(context.Background(), 180*time.Second)
	defer abbrechen()
	out, err := chromium(ctx, srv.URL+"/", "2200,1300", "--dump-dom")
	if err != nil {
		t.Fatalf("chromium (%s): %v\n%s", chrome, err, out)
	}
	m := berichtRe.FindSubmatch(out)
	if m == nil {
		t.Fatalf("kein Testbericht in der Seite. DOM:\n%s", out)
	}
	roh, _ := base64.StdEncoding.DecodeString(string(m[1]))
	var b struct {
		Abbruch, DOM                                     string
		Fehler                                           []string
		Gruppen, Chips88, Aus60, Wartet111, Abschnitte0  []string
		P1, P1Ohne, Kopf60, Ziel, Frage88, Mehr, Zeile97 string
		Zeile98, Zeile102                                string
		P1Zahl, X107, X111, GemergtVor, GemergtNach      int
		MehrNach                                         int
		ZeilenNachStation, StationenNachStation          []string
		ZeilenNachZeile, StationenNachZeile              []string
		ImBild102                                        bool
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
	liste := func(s []string) string { return strings.Join(s, ",") }
	if liste(b.Gruppen) != "60,36,70,0" {
		t.Errorf("Gruppen %v, erwartet 60,36,70,0", b.Gruppen)
	}
	if b.P1 != "trägt P1-Frage #88" || b.P1Ohne != "trägt P1-Frage #91" || b.P1Zahl != 2 {
		t.Errorf("Badges %q, %q, %d; erwartet trägt P1-Frage #88 und #91", b.P1, b.P1Ohne, b.P1Zahl)
	}
	for _, teil := range []string{"#60", "Fork-Unterstuetzung", "0/4 erledigt", "0 aktiv", "4 ausstehend"} {
		if !strings.Contains(b.Kopf60, teil) {
			t.Errorf("Kopf 60 %q, erwartet %q", b.Kopf60, teil)
		}
	}
	if b.Ziel != "Ziel: Blocker schnell klaeren: was war, was laeuft, was blockiert, was kommt." {
		t.Errorf("Ziel %q", b.Ziel)
	}
	if liste(b.Chips88) != "102:direkt,104:direkt,107:transitiv,111:transitiv" {
		t.Errorf("Chips #88 %v", b.Chips88)
	}
	for _, teil := range []string{"P1", "Fork-Repos: PAT des Forks oder des Upstreams?", "alex", "seit 6 Tagen", "hält 4 zurück"} {
		if !strings.Contains(b.Frage88, teil) {
			t.Errorf("Frage #88 %q, erwartet %q", b.Frage88, teil)
		}
	}
	if liste(b.Aus60) != "104/0,102/0,107/1,111/2" || b.X111 <= b.X107 || b.X107 < 0 {
		t.Errorf("ausstehend 60 %v, Einzug #107 %d #111 %d; erwartet 104/0,102/0,107/1,111/2 und #111 tiefer", b.Aus60, b.X107, b.X111)
	}
	if liste(b.Wartet111) != "107:issue,104:issue" {
		t.Errorf("wartet #111 %v", b.Wartet111)
	}
	for _, teil := range []string{"change/netz-retry", "im_pr", "6/6", "PR #99", "vor 5 h"} {
		if !strings.Contains(b.Zeile98, teil) {
			t.Errorf("Zeile #98 %q, erwartet %q", b.Zeile98, teil)
		}
	}
	if !strings.Contains(b.Zeile102, "~3 T") || !strings.Contains(b.Zeile102, "gesperrt") {
		t.Errorf("Zeile #102 %q, erwartet ~3 T und gesperrt", b.Zeile102)
	}
	if liste(b.Abschnitte0) != "fragen,aktiv,ausstehend,gemergt" {
		t.Errorf("Abschnitte in 0 %v", b.Abschnitte0)
	}
	if b.GemergtVor != 3 || b.Mehr != "+ 2 weitere" || b.GemergtNach != 5 || b.MehrNach != 0 {
		t.Errorf("gemergt %d, %q, danach %d und %d Knoepfe; erwartet 3, + 2 weitere, 5, 0", b.GemergtVor, b.Mehr, b.GemergtNach, b.MehrNach)
	}
	if !strings.Contains(b.Zeile97, "27.9.") || !strings.Contains(b.Zeile97, "docker-betrieb") || !strings.Contains(b.Zeile97, "gemergt mit #98") {
		t.Errorf("Zeile #97 %q, erwartet 27.9., docker-betrieb und gemergt mit #98", b.Zeile97)
	}
	if liste(b.ZeilenNachStation) != "102" || liste(b.StationenNachStation) != "102" || !b.ImBild102 {
		t.Errorf("nach Klick auf Station #102: Zeilen %v, Stationen %v, im Bild %v", b.ZeilenNachStation, b.StationenNachStation, b.ImBild102)
	}
	if liste(b.ZeilenNachZeile) != "105" || liste(b.StationenNachZeile) != "105" {
		t.Errorf("nach Klick auf Zeile #105: Zeilen %v, Stationen %v", b.ZeilenNachZeile, b.StationenNachZeile)
	}
	ab := api.abschnitte()
	for _, abschnitt := range []string{"vor-station", "vor-zeile"} {
		if n := len(nur(ab[abschnitt], http.MethodGet, "/api/detail")); n != 0 {
			t.Errorf("%s: %d Anfragen an /api/detail, erwartet keine", abschnitt, n)
		}
	}
	for abschnitt, nr := range map[string]string{"vor-symbol": "nummer=104", "vor-doppel": "nummer=106"} {
		ds := nur(ab[abschnitt], http.MethodGet, "/api/detail")
		if len(ds) != 1 || !strings.Contains(ds[0].Query, "art=issue") || !strings.Contains(ds[0].Query, nr) {
			t.Errorf("%s: Detail-Anfragen %+v, erwartet art=issue und %s", abschnitt, ds, nr)
		}
	}

	for env, modus := range map[string]string{"GKP_BELEG_PNG_KARTE_GRUPPEN": "liste", "GKP_BELEG_PNG_KARTE_SPRUNG": "sprung"} {
		if ziel := os.Getenv(env); ziel != "" {
			if out, err := chromium(ctx, srv.URL+"/?foto="+modus, "2200,3400", "--screenshot="+ziel); err != nil {
				t.Errorf("Bildschirmfoto %s: %v\n%s", modus, err, out)
			}
		}
	}
}
