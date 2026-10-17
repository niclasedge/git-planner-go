package assets_test

// Browser-Test des Dateien-Abgleichs (dateien-abgleich, #53): die Detailspalte
// eines Changes zeigt die Capabilities-Zeile mit "liegt abweichend auch in
// main" und die Klappe "Dateien" mit den drei Zahlen im Titel und den drei
// Listen darunter. Listen und Folge kommen fertig aus der Fake-API, die
// Oberflaeche rechnet nichts. Mechanik wie in naechster_schritt_browser_test.go.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/niclasedge/git-planner-go/internal/assets"
)

const daArbeit = `{"repo":"acme/alpha","fortschritt":{"erledigt":1,"gesamt":3},"zuletzt":[],
 "reihe":[
  {"art":"issue","nummer":53,"titel":"dateien-abgleich: Zusage gegen Diff, Capabilities und abweichende Kopien je Change","url":"u53","status":"in_arbeit",
   "blockiert_durch":[],"labels":[],"zustaendig":[],"hat_text":true,"tiefe":0,"aktiv":false,"branches":[],
   "change":{"kennung":"dateien-abgleich","ref":"change/dateien-abgleich","archiviert":false,"status":"laeuft","issue":53,
    "aufgaben":[{"text":"Tests schreiben","erledigt":true},{"text":"Route bauen","erledigt":false},{"text":"Detailspalte","erledigt":false}],
    "erledigt":1,"gesamt":3,"gates":["gate1","gate2"],"belege":[]}}
 ],
 "ohne_issue":[],"prs":[],"quellen":[]}`

// Die Folge der Listen ist absichtlich nicht alphabetisch: die Oberflaeche
// soll sie uebernehmen, nicht sortieren.
const daDetail = `{"art":"change","kennung":"dateien-abgleich","titel":"dateien-abgleich","ref":"change/dateien-abgleich",
 "proposal":"# dateien-abgleich\n\nGeht zurueck auf GitHub-Issue #53.","tasks":"- [x] Tests schreiben\n- [ ] Route bauen","specs":[],"belege":[],
 "abgleich":{"capabilities":["openspec-quelle"],"unveraendert":false,"auch_in":["main"],
  "dateien":{"zugesagt":["web/src/DateienAbgleich.svelte","internal/appctx/dateienabgleich.go","docs/belege/dateien-abgleich.png"],
   "gedeckt":["web/src/DateienAbgleich.svelte","internal/appctx/dateienabgleich.go"],
   "ohne_zusage":["internal/server/server.go"],
   "ohne_aenderung":["docs/belege/dateien-abgleich.png"]}}}`

const daTreiber = `
(function () {
  var fehler = [];
  window.addEventListener('error', function (e) { fehler.push(String(e.message)); });
  window.addEventListener('unhandledrejection', function (e) { fehler.push('rejection: ' + String(e.reason)); });
  function schlaf(ms) { return new Promise(function (r) { setTimeout(r, ms); }); }
  async function warte(was, pred) {
    for (var i = 0; i < 200; i++) { try { if (pred()) return; } catch (e) {} await schlaf(25); }
    throw new Error('Zeitueberschreitung: ' + was);
  }
  function q(sel) { return document.querySelector(sel); }
  function text(el) { return el ? el.textContent.replace(/\s+/g, ' ').trim() : ''; }
  function liste(lage) {
    return Array.prototype.map.call(document.querySelectorAll('.kn-detail [data-lage="' + lage + '"] li'), function (li) {
      return { text: text(li), sichtbar: li.offsetHeight > 0 };
    });
  }
  function berichten(b) {
    b.fehler = (b.fehler || []).concat(fehler);
    var pre = document.createElement('pre');
    pre.id = 'kn-testbericht';
    pre.textContent = btoa(unescape(encodeURIComponent(JSON.stringify(b))));
    document.body.appendChild(pre);
  }
  async function lauf() {
    var b = {};
    try {
      await warte('Repo in der Leiste', function () { return q('[data-repo="acme/alpha"]'); });
      q('[data-repo="acme/alpha"]').click();
      await warte('Zeile', function () { return q('[data-eintrag="53"]'); });
      q('[data-eintrag="53"] .kn-e-titel').click();
      await warte('Karte', function () { return q('[data-eintrag="53"] .kn-e-details'); });
      q('[data-eintrag="53"] .kn-e-details').click();
      await warte('Abgleich', function () { return q('.kn-detail .kn-abgleich'); });
      await schlaf(200);
      b.zeile = text(q('.kn-detail .kn-abgleich-caps'));
      b.titel = text(q('.kn-detail .kn-abgleich-dateien summary'));
      b.gedeckt = liste('gedeckt');
      b.ohneZusage = liste('ohne_zusage');
      b.ohneAenderung = liste('ohne_aenderung');
    } catch (e) {
      b.abbruch = String(e && e.message || e);
      b.dom = document.body.innerHTML.slice(0, 3000);
    }
    berichten(b);
  }
  lauf();
})();
`

type daPunkt struct {
	Text     string `json:"text"`
	Sichtbar bool   `json:"sichtbar"`
}

func TestDateienAbgleichImBrowser(t *testing.T) {
	if testing.Short() {
		t.Skip("-short: der Test startet einen Browser")
	}
	chrome := chromiumFinden()
	if chrome == "" {
		t.Skip("kein Chromium gefunden — die Oberflaeche ist damit NICHT belegt")
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
		fmt.Fprint(w, daTreiber)
	})
	mux.HandleFunc("/api/repos", func(w http.ResponseWriter, r *http.Request) { json200(w, ivRepos) })
	mux.HandleFunc("/api/arbeit", func(w http.ResponseWriter, r *http.Request) { json200(w, daArbeit) })
	mux.HandleFunc("/api/repo-meta", func(w http.ResponseWriter, r *http.Request) { json200(w, ivMeta) })
	mux.HandleFunc("/api/detail", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("art") != "change" || r.URL.Query().Get("kennung") != "dateien-abgleich" {
			json200(w, `{"art":"issue","nummer":53,"titel":"dateien-abgleich","text":"Issue-Text"}`)
			return
		}
		json200(w, daDetail)
	})
	// Die Zeitleiste der Detailspalte (aufgaben-zeitleiste, #55) ist hier nur
	// Nachbar; ein Eintrag, damit das Bildschirmfoto keinen Fehler zeigt.
	mux.HandleFunc("/api/zeitleiste", func(w http.ResponseWriter, r *http.Request) {
		json200(w, `{"repo":"acme/alpha","kennung":"dateien-abgleich","ref":"change/dateien-abgleich","commits":1,"eintraege":[
		 {"aufgabe":"Tests schreiben","sha":"abc1234def","kurz_sha":"abc1234","zeit":"2026-09-25T09:30:00Z","betreff":"dateien-abgleich: Tests (#53)"}]}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	chromium := func(ctx context.Context, extra ...string) ([]byte, error) {
		args := append([]string{
			"--headless", "--no-sandbox", "--disable-gpu", "--no-first-run",
			"--disable-extensions", "--disable-dev-shm-usage", "--hide-scrollbars",
			"--user-data-dir=" + t.TempDir(),
			"--window-size=1400,900",
			"--virtual-time-budget=30000",
		}, extra...)
		return exec.CommandContext(ctx, chrome, append(args, srv.URL+"/")...).Output()
	}

	ctx, abbrechen := context.WithTimeout(context.Background(), 90*time.Second)
	defer abbrechen()
	out, err := chromium(ctx, "--dump-dom")
	if err != nil {
		t.Fatalf("chromium (%s): %v\n%s", chrome, err, out)
	}
	m := berichtRe.FindSubmatch(out)
	if m == nil {
		t.Fatalf("kein Testbericht in der Seite. DOM:\n%s", out)
	}
	roh, _ := base64.StdEncoding.DecodeString(string(m[1]))
	var b struct {
		Abbruch       string    `json:"abbruch"`
		DOM           string    `json:"dom"`
		Fehler        []string  `json:"fehler"`
		Zeile         string    `json:"zeile"`
		Titel         string    `json:"titel"`
		Gedeckt       []daPunkt `json:"gedeckt"`
		OhneZusage    []daPunkt `json:"ohneZusage"`
		OhneAenderung []daPunkt `json:"ohneAenderung"`
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
	if !strings.Contains(b.Titel, "2 gedeckt · 1 ohne Zusage · 1 ohne Änderung") {
		t.Errorf("Titel %q", b.Titel)
	}
	for _, teil := range []string{"openspec-quelle", "liegt abweichend auch in main"} {
		if !strings.Contains(b.Zeile, teil) {
			t.Errorf("Capabilities-Zeile %q ohne %q", b.Zeile, teil)
		}
	}
	if strings.Contains(b.Zeile, "unverändert") {
		t.Errorf("Capabilities-Zeile %q nennt unverändert, die Route sagt nein", b.Zeile)
	}
	texte := func(ps []daPunkt) []string {
		out := []string{}
		for _, p := range ps {
			if !p.Sichtbar {
				t.Errorf("Eintrag %q nicht sichtbar", p.Text)
			}
			out = append(out, p.Text)
		}
		return out
	}
	for _, f := range []struct {
		was       string
		ist, soll []string
	}{
		{"gedeckt", texte(b.Gedeckt), []string{"web/src/DateienAbgleich.svelte", "internal/appctx/dateienabgleich.go"}},
		{"ohne Zusage", texte(b.OhneZusage), []string{"internal/server/server.go"}},
		{"ohne Änderung", texte(b.OhneAenderung), []string{"docs/belege/dateien-abgleich.png"}},
	} {
		if !reflect.DeepEqual(f.ist, f.soll) {
			t.Errorf("Liste %s = %q, erwartet %q in der Folge der Route", f.was, f.ist, f.soll)
		}
	}

	// Auf Wunsch ein Bildschirmfoto derselben Seite als Beleg: Detailspalte
	// mit Capabilities-Zeile und Dateien-Klappe.
	if ziel := os.Getenv("GKP_BELEG_PNG"); ziel != "" {
		if out, err := chromium(ctx, "--screenshot="+ziel); err != nil {
			t.Errorf("Bildschirmfoto: %v\n%s", err, out)
		}
	}
}
