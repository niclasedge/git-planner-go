package assets_test

// Browser-Test der Archivliste (archiv-liste, #56): unter "Letzte Aktionen"
// steht der eingeklappte Abschnitt "Abgeschlossen" mit der Zahl der
// archivierten Changes; ein Klick auf die Ueberschrift zeigt die Zeilen in der
// Folge aus Go, je mit Datum, Kennung, erledigt/gesamt und Vorschaubild.
// Mechanik wie in naechster_schritt_browser_test.go.

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/niclasedge/git-planner-go/internal/assets"
)

const alArbeit = `{"repo":"acme/alpha","fortschritt":{"erledigt":0,"gesamt":1},
 "zuletzt":[
  {"art":"issue","nummer":52,"titel":"naechster-schritt: naechster Schritt mit Befehl je Change","url":"u52","status":"erledigt",
   "blockiert_durch":[],"labels":[],"zustaendig":[],"hat_text":false,"tiefe":0,"aktiv":false,"branches":[]}
 ],
 "reihe":[
  {"art":"issue","nummer":56,"titel":"archiv-liste: abgeschlossene Changes mit Datum und Vorschau","url":"u56","status":"ohne_change",
   "blockiert_durch":[],"labels":[],"zustaendig":[],"hat_text":false,"tiefe":0,"aktiv":false,"branches":[]}
 ],
 "archiv":[
  {"kennung":"naechster-schritt","datum":"2026-09-25","issue":52,"erledigt":2,"gesamt":2,
   "vorschau":{"zeit":"2026-09-25T10:35:37Z","stufe":"nachher","art":"bild","pfad":"belege/assets/0-naechster-schritt.png","fehlt":false,"bild":"/api/blob?repo=acme/alpha&sha=aa11&name=0-naechster-schritt.png"}},
  {"kennung":"netz-schreiben","datum":"2026-09-25","issue":46,"erledigt":8,"gesamt":8},
  {"kennung":"repo-leiste","datum":"2026-09-24","issue":25,"erledigt":5,"gesamt":6}
 ],
 "ohne_issue":[],"prs":[],"quellen":[]}`

const alTreiber = `
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
  function berichten(b) {
    b.fehler = (b.fehler || []).concat(fehler);
    var pre = document.createElement('pre');
    pre.id = 'kn-testbericht';
    pre.textContent = btoa(unescape(encodeURIComponent(JSON.stringify(b))));
    document.body.appendChild(pre);
  }
  function zeilen() {
    return Array.prototype.map.call(document.querySelectorAll('[data-archiv] [data-archiv-zeile]'), function (z) {
      var img = z.querySelector('img');
      return { kennung: z.getAttribute('data-archiv-zeile'), text: text(z),
               bild: img ? img.getAttribute('src') : '', geladen: !!(img && img.complete && img.naturalWidth > 0) };
    });
  }
  async function lauf() {
    var b = {};
    try {
      await warte('Repo in der Leiste', function () { return q('[data-repo="acme/alpha"]'); });
      q('[data-repo="acme/alpha"]').click();
      await warte('Archivabschnitt', function () { return q('[data-archiv]'); });
      b.ueberschriften = Array.prototype.map.call(document.querySelectorAll('.kn-a-abschnitt h2, [data-archiv] h2'), text);
      b.kopfVorher = text(q('[data-archiv] h2'));
      b.zeilenVorher = zeilen().length;
      var knopf = q('[data-archiv] h2 button') || q('[data-archiv] h2');
      knopf.click();
      await warte('Zeilen', function () { return zeilen().length >= 3; });
      await warte('Vorschau geladen', function () { return zeilen()[0].geladen; });
      b.zeilen = zeilen();
    } catch (e) {
      b.abbruch = String(e && e.message || e);
      b.dom = document.body.innerHTML.slice(0, 3000);
    }
    berichten(b);
  }
  lauf();
})();
`

type alZeile struct {
	Kennung string `json:"kennung"`
	Text    string `json:"text"`
	Bild    string `json:"bild"`
	Geladen bool   `json:"geladen"`
}

func TestArchivListeImBrowser(t *testing.T) {
	if testing.Short() {
		t.Skip("-short: der Test startet einen Browser")
	}
	chrome := chromiumFinden()
	if chrome == "" {
		t.Skip("kein Chromium gefunden — die Oberflaeche ist damit NICHT belegt")
	}
	bild := image.NewRGBA(image.Rect(0, 0, 160, 100))
	for x := 0; x < 160; x++ {
		for y := 0; y < 100; y++ {
			bild.Set(x, y, color.RGBA{R: 40 + uint8(x/2), G: 110, B: 200 - uint8(y), A: 255})
		}
	}
	var pngDaten bytes.Buffer
	if err := png.Encode(&pngDaten, bild); err != nil {
		t.Fatal(err)
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
		fmt.Fprint(w, alTreiber)
	})
	mux.HandleFunc("/api/repos", func(w http.ResponseWriter, r *http.Request) { json200(w, ivRepos) })
	mux.HandleFunc("/api/arbeit", func(w http.ResponseWriter, r *http.Request) { json200(w, alArbeit) })
	mux.HandleFunc("/api/repo-meta", func(w http.ResponseWriter, r *http.Request) { json200(w, ivMeta) })
	mux.HandleFunc("/api/blob", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("sha") != "aa11" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Write(pngDaten.Bytes())
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
		Abbruch        string    `json:"abbruch"`
		DOM            string    `json:"dom"`
		Fehler         []string  `json:"fehler"`
		Ueberschriften []string  `json:"ueberschriften"`
		KopfVorher     string    `json:"kopfVorher"`
		ZeilenVorher   int       `json:"zeilenVorher"`
		Zeilen         []alZeile `json:"zeilen"`
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

	t.Run("Stelle", func(t *testing.T) {
		pos := func(was string) int {
			for i, u := range b.Ueberschriften {
				if strings.HasPrefix(u, was) {
					return i
				}
			}
			return -1
		}
		z, a, n := pos("Letzte Aktionen"), pos("Abgeschlossen"), pos("Als Nächstes")
		if z < 0 || a < 0 || n < 0 || !(z < a && a < n) {
			t.Errorf("Ueberschriften %q: erwartet Letzte Aktionen < Abgeschlossen < Als Nächstes", b.Ueberschriften)
		}
	})
	t.Run("Eingeklappt", func(t *testing.T) {
		if !strings.Contains(b.KopfVorher, "Abgeschlossen") || !strings.Contains(b.KopfVorher, "3") {
			t.Errorf("Kopf %q, erwartet Abgeschlossen mit 3", b.KopfVorher)
		}
		if b.ZeilenVorher != 0 {
			t.Errorf("vor dem Klick %d Zeilen, erwartet eingeklappt", b.ZeilenVorher)
		}
	})
	t.Run("Aufgeklappt", func(t *testing.T) {
		var folge []string
		for _, z := range b.Zeilen {
			folge = append(folge, z.Kennung)
		}
		if strings.Join(folge, ",") != "naechster-schritt,netz-schreiben,repo-leiste" {
			t.Fatalf("Zeilen %v, erwartet die Folge aus archiv", folge)
		}
		e := b.Zeilen[0]
		for _, teil := range []string{"2026-09-25", "naechster-schritt", "2 / 2"} {
			if !strings.Contains(e.Text, teil) {
				t.Errorf("erste Zeile %q ohne %q", e.Text, teil)
			}
		}
		if !strings.Contains(e.Bild, "sha=aa11") || !e.Geladen {
			t.Errorf("Vorschau der ersten Zeile %q geladen=%v", e.Bild, e.Geladen)
		}
		if b.Zeilen[1].Bild != "" || !strings.Contains(b.Zeilen[2].Text, "5 / 6") {
			t.Errorf("Zeilen 2 und 3: %+v %+v", b.Zeilen[1], b.Zeilen[2])
		}
	})

	// Auf Wunsch ein Bildschirmfoto derselben Seite als Beleg: aufgeklappte
	// Archivliste mit Vorschaubild.
	if ziel := os.Getenv("GKP_BELEG_PNG"); ziel != "" {
		if out, err := chromium(ctx, "--screenshot="+ziel); err != nil {
			t.Errorf("Bildschirmfoto: %v\n%s", err, out)
		}
	}
}
