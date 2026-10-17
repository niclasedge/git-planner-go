package assets_test

// Browser-Test des Abschnitts "HTML-Dateien" unter "Geaenderte Bilder"
// (html-dateien, #112): drei Zeilen in der Folge der Route, als Liste ohne
// Kacheln, mit Name, Pfad, erstellt, letzter Aenderung und Bildzaehler; ein
// fehlendes Bild faerbt den Zaehler rot und steht im Tooltip. Jede Zeile ist
// ein Link in einen neuen Tab. Mit GKP_BELEG_PNG_HTML_DATEIEN legt der Test
// ein Bildschirmfoto ab. Seit html-dateien-baum (#114) kommen die Dateien aus
// dem Baum: die dritte Zeile ist zuletzt am 2026-03-01 geaendert, weit vor
// den geaenderten Bildern. Der Abschnitt steht auch ohne Eintrag da, mit
// "Keine HTML-Dateien im Repo" bzw. "HTML-Dateien nicht verfügbar · Grund";
// GKP_BELEG_PNG_HTML_LEER legt davon ein Bildschirmfoto ab.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/niclasedge/git-planner-go/internal/assets"
)

const hdGeaendert = `{"repo":"acme/alpha","bilder":[
 {"pfad":"docs/entwurf/a.png","name":"a.png","typ":"png","sha":"g-01","zeit":"2026-09-27T10:00:00Z","commit":"c1","betreff":"Entwurf nachgezogen",
  "bild":"/api/blob?repo=acme%2Falpha&sha=g-01&name=a.png","link":"https://github.com/acme/alpha/blob/c1/docs/entwurf/a.png"}]}`

const hdDateien = `{"repo":"acme/alpha","dateien":[
 {"pfad":"berichte/wochenbericht.html","name":"wochenbericht.html","sha":"h-1","zeit":"2026-09-27T14:05:00Z","commit":"k3","betreff":"Wochenbericht KW 39",
  "erstellt":"2026-09-20T08:30:00Z","erstellt_commit":"k0","adresse":"/api/datei/acme/alpha/k3/berichte/wochenbericht.html",
  "link":"https://github.com/acme/alpha/blob/k3/berichte/wochenbericht.html",
  "bilder":{"gesamt":4,"vorhanden":3,"fehlt":1,"extern":0,"eingebettet":1,"fehlende":["berichte/bilder/fehlt.png"],"text":"4 Bilder · 3 vorhanden · 1 fehlt"}},
 {"pfad":"docs/entwurf/karte.html","name":"karte.html","sha":"h-2","zeit":"2026-09-26T11:40:00Z","commit":"k2","betreff":"Kartenentwurf G",
  "erstellt":"2026-09-26T11:40:00Z","erstellt_commit":"k2","adresse":"/api/datei/acme/alpha/k2/docs/entwurf/karte.html",
  "link":"https://github.com/acme/alpha/blob/k2/docs/entwurf/karte.html",
  "bilder":{"gesamt":2,"vorhanden":2,"fehlt":0,"extern":0,"eingebettet":0,"fehlende":[],"text":"2 Bilder · alle vorhanden"}},
 {"pfad":"docs/design/entwurf-g.html","name":"entwurf-g.html","sha":"h-3","zeit":"2026-03-01T09:00:00Z","commit":"a0","betreff":"Entwurf G angelegt (197 Commits zurueck)",
  "erstellt":"","adresse":"/api/datei/acme/alpha/a0/docs/design/entwurf-g.html","link":"https://github.com/acme/alpha/blob/a0/docs/design/entwurf-g.html",
  "bilder":{"gesamt":0,"vorhanden":0,"fehlt":0,"extern":0,"eingebettet":0,"fehlende":[],"text":"keine Bilder"}}
]}`

const hdTreiber = `
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
  function qa(sel) { return document.querySelectorAll(sel); }
  function text(el) { return el ? el.textContent.replace(/\s+/g, ' ').trim() : ''; }
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
      await warte('HTML-Dateien', function () { return qa('.kn-html [data-html]').length >= 3; });
      await warte('Geaenderte Bilder', function () { return q('.kn-geaendert [data-geaendert]'); });
      await schlaf(100);
      b.ueberschrift = text(q('.kn-html h2'));
      var g = q('.kn-geaendert'), h = q('.kn-html');
      b.unterGeaendert = !!(g && h && (g.compareDocumentPosition(h) & Node.DOCUMENT_POSITION_FOLLOWING));
      b.kacheln = qa('.kn-html img, .kn-html .kn-g-kachel').length;
      b.hinweis = text(q('.kn-html .kn-g-hinweis'));
      b.zeilen = Array.prototype.map.call(qa('.kn-html [data-html]'), function (z) {
        var a = z.tagName === 'A' ? z : z.querySelector('a');
        var zaehler = z.querySelector('.kn-html-bilder');
        return { pfad: z.getAttribute('data-html'), name: text(z.querySelector('.kn-html-name')),
                 pfadText: text(z.querySelector('.kn-html-pfad')), erstellt: text(z.querySelector('.kn-html-erstellt')),
                 geaendert: text(z.querySelector('.kn-html-geaendert')), betreff: text(z.querySelector('.kn-html-betreff')),
                 zaehler: text(zaehler), fehlt: !!(zaehler && zaehler.classList.contains('kn-html-fehlt')),
                 farbe: zaehler ? getComputedStyle(zaehler).color : '', tooltip: zaehler ? (zaehler.getAttribute('title') || '') : '',
                 href: a ? a.getAttribute('href') : '', target: a ? a.getAttribute('target') : '', rel: a ? a.getAttribute('rel') : '' };
      });
      if (h) h.scrollIntoView({ block: 'center' });
      await schlaf(100);
    } catch (e) {
      b.abbruch = String(e && e.message || e);
      b.dom = document.body.innerHTML.slice(0, 4000);
    }
    berichten(b);
  }
  lauf();
})();
`

type hdZeile struct {
	Pfad      string `json:"pfad"`
	Name      string `json:"name"`
	PfadText  string `json:"pfadText"`
	Erstellt  string `json:"erstellt"`
	Geaendert string `json:"geaendert"`
	Betreff   string `json:"betreff"`
	Zaehler   string `json:"zaehler"`
	Fehlt     bool   `json:"fehlt"`
	Farbe     string `json:"farbe"`
	Tooltip   string `json:"tooltip"`
	Href      string `json:"href"`
	Target    string `json:"target"`
	Rel       string `json:"rel"`
}

type hdBericht struct {
	Abbruch        string    `json:"abbruch"`
	DOM            string    `json:"dom"`
	Fehler         []string  `json:"fehler"`
	Ueberschrift   string    `json:"ueberschrift"`
	UnterGeaendert bool      `json:"unterGeaendert"`
	Kacheln        int       `json:"kacheln"`
	Hinweis        string    `json:"hinweis"`
	Zeilen         []hdZeile `json:"zeilen"`
}

func TestHTMLDateienImBrowser(t *testing.T) {
	if testing.Short() {
		t.Skip("-short: der Test startet einen Browser")
	}
	chrome := chromiumFinden()
	if chrome == "" {
		t.Skip("kein Chromium gefunden — die Oberflaeche ist damit NICHT belegt")
	}
	bild := bgBild()
	mux := http.NewServeMux()
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
		fmt.Fprint(w, hdTreiber)
	})
	mux.HandleFunc("/api/repos", func(w http.ResponseWriter, r *http.Request) { json200(w, ivRepos) })
	mux.HandleFunc("/api/arbeit", func(w http.ResponseWriter, r *http.Request) { json200(w, bmArbeit) })
	mux.HandleFunc("/api/repo-meta", func(w http.ResponseWriter, r *http.Request) { json200(w, ivMeta) })
	mux.HandleFunc("/api/beleg-galerie", func(w http.ResponseWriter, r *http.Request) { json200(w, bmGalerie) })
	mux.HandleFunc("/api/geaenderte-bilder", func(w http.ResponseWriter, r *http.Request) { json200(w, hdGeaendert) })
	mux.HandleFunc("/api/html-dateien", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("repo") != "acme/alpha" {
			http.Error(w, `{"grund":"falsches Repo"}`, http.StatusBadRequest)
			return
		}
		json200(w, hdDateien)
	})
	mux.HandleFunc("/api/blob", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(bild)
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
	ctx, abbrechen := context.WithTimeout(context.Background(), 120*time.Second)
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
	var b hdBericht
	if err := json.Unmarshal(roh, &b); err != nil {
		t.Fatalf("Bericht kein JSON: %v", err)
	}
	if b.Abbruch != "" {
		t.Fatalf("Treiber abgebrochen: %s\nFehler: %v\nDOM: %s", b.Abbruch, b.Fehler, b.DOM)
	}
	if len(b.Fehler) > 0 {
		t.Errorf("JavaScript-Fehler: %v", b.Fehler)
	}
	if b.Ueberschrift != "HTML-Dateien" || !b.UnterGeaendert {
		t.Errorf("Ueberschrift %q, unter Geaenderte Bilder %v", b.Ueberschrift, b.UnterGeaendert)
	}
	if b.Kacheln != 0 {
		t.Errorf("%d Kacheln oder Bilder im Abschnitt, erwartet eine reine Liste", b.Kacheln)
	}
	if len(b.Zeilen) != 3 {
		t.Fatalf("%d Zeilen, erwartet 3", len(b.Zeilen))
	}
	for i, pfad := range []string{"berichte/wochenbericht.html", "docs/entwurf/karte.html", "docs/design/entwurf-g.html"} {
		z := b.Zeilen[i]
		if z.Pfad != pfad {
			t.Errorf("Zeile %d: %q, erwartet %q (Folge der Route)", i, z.Pfad, pfad)
		}
		if z.Target != "_blank" || z.Rel != "noopener" || !strings.HasPrefix(z.Href, "/api/datei/acme/alpha/") || !strings.HasSuffix(z.Href, "/"+pfad) {
			t.Errorf("Zeile %d: Link %q target %q rel %q", i, z.Href, z.Target, z.Rel)
		}
	}
	z := b.Zeilen[0]
	if z.Name != "wochenbericht.html" || z.PfadText != "berichte/wochenbericht.html" {
		t.Errorf("Name %q, Pfad %q", z.Name, z.PfadText)
	}
	if !strings.Contains(z.Erstellt, "erstellt") || !strings.Contains(z.Erstellt, "2026-09-20 08:30") {
		t.Errorf("erstellt %q, erwartet Datum und Uhrzeit", z.Erstellt)
	}
	if !strings.Contains(z.Geaendert, "2026-09-27 14:05") || z.Betreff != "Wochenbericht KW 39" {
		t.Errorf("geaendert %q, Betreff %q", z.Geaendert, z.Betreff)
	}
	if z.Zaehler != "4 Bilder · 3 vorhanden · 1 fehlt" || !z.Fehlt || !strings.Contains(z.Tooltip, "berichte/bilder/fehlt.png") {
		t.Errorf("Zaehler %q, rot %v, Tooltip %q", z.Zaehler, z.Fehlt, z.Tooltip)
	}
	if z.Farbe == b.Zeilen[1].Farbe {
		t.Errorf("Zaehler mit fehlendem Bild hat dieselbe Farbe %q wie ohne", z.Farbe)
	}
	if b.Zeilen[1].Fehlt || b.Zeilen[1].Zaehler != "2 Bilder · alle vorhanden" {
		t.Errorf("zweite Zeile %+v", b.Zeilen[1])
	}
	if b.Zeilen[2].Erstellt != "erstellt unbekannt" || b.Zeilen[2].Zaehler != "keine Bilder" {
		t.Errorf("dritte Zeile %+v, erwartet erstellt unbekannt und keine Bilder", b.Zeilen[2])
	}
	if !strings.Contains(b.Zeilen[2].Geaendert, "2026-03-01 09:00") {
		t.Errorf("dritte Zeile geaendert %q, erwartet die alte Aenderung vom 2026-03-01", b.Zeilen[2].Geaendert)
	}
	if b.Hinweis != "" {
		t.Errorf("Hinweis %q bei einer Liste ohne Grund", b.Hinweis)
	}

	if ziel := os.Getenv("GKP_BELEG_PNG_HTML_DATEIEN"); ziel != "" {
		if out, err := chromium(ctx, "--screenshot="+ziel); err != nil {
			t.Errorf("Bildschirmfoto: %v\n%s", err, out)
		}
	}
}

// hdLeerTreiber wartet auf den Abschnitt "HTML-Dateien" ohne Zeile und
// berichtet Ueberschrift, Hinweis und die Zahl der Zeilen.
const hdLeerTreiber = `
(function () {
  var fehler = [];
  window.addEventListener('error', function (e) { fehler.push(String(e.message)); });
  function schlaf(ms) { return new Promise(function (r) { setTimeout(r, ms); }); }
  async function warte(was, pred) {
    for (var i = 0; i < 200; i++) { try { if (pred()) return; } catch (e) {} await schlaf(25); }
    throw new Error('Zeitueberschreitung: ' + was);
  }
  function q(sel) { return document.querySelector(sel); }
  function text(el) { return el ? el.textContent.replace(/\s+/g, ' ').trim() : ''; }
  async function lauf() {
    var b = {};
    try {
      await warte('Repo in der Leiste', function () { return q('[data-repo="acme/alpha"]'); });
      q('[data-repo="acme/alpha"]').click();
      await warte('Geaenderte Bilder', function () { return q('.kn-geaendert [data-geaendert]'); });
      await warte('HTML-Dateien', function () { return q('.kn-html'); });
      await schlaf(100);
      var g = q('.kn-geaendert'), h = q('.kn-html');
      b.ueberschrift = text(q('.kn-html h2'));
      b.unterGeaendert = !!(g && h && (g.compareDocumentPosition(h) & Node.DOCUMENT_POSITION_FOLLOWING));
      b.hinweis = text(q('.kn-html .kn-g-hinweis'));
      b.zeilen = [];
      b.anzahl = document.querySelectorAll('.kn-html [data-html]').length;
      if (h) h.scrollIntoView({ block: 'center' });
      await schlaf(100);
    } catch (e) {
      b.abbruch = String(e && e.message || e);
      b.dom = document.body.innerHTML.slice(0, 4000);
    }
    b.fehler = fehler;
    var pre = document.createElement('pre');
    pre.id = 'kn-testbericht';
    pre.textContent = btoa(unescape(encodeURIComponent(JSON.stringify(b))));
    document.body.appendChild(pre);
  }
  lauf();
})();
`

type hdLeerBericht struct {
	Abbruch        string   `json:"abbruch"`
	DOM            string   `json:"dom"`
	Fehler         []string `json:"fehler"`
	Ueberschrift   string   `json:"ueberschrift"`
	UnterGeaendert bool     `json:"unterGeaendert"`
	Hinweis        string   `json:"hinweis"`
	Anzahl         int      `json:"anzahl"`
}

// Pflichttest aus #114: die leere Liste zeigt den Hinweis, ein Fehler seinen
// Grund; der Abschnitt steht beide Male da.
func TestHTMLDateienLeerImBrowser(t *testing.T) {
	if testing.Short() {
		t.Skip("-short: der Test startet einen Browser")
	}
	chrome := chromiumFinden()
	if chrome == "" {
		t.Skip("kein Chromium gefunden — die Oberflaeche ist damit NICHT belegt")
	}
	faelle := []struct {
		name, antwort, hinweis, fotoEnv string
	}{
		{"leer", `{"repo":"acme/alpha","ref":"main","dateien":[]}`, "Keine HTML-Dateien im Repo", "GKP_BELEG_PNG_HTML_LEER"},
		{"fehler", `{"repo":"acme/alpha","ref":"main","dateien":[],"grund":"GitHub: 502"}`, "HTML-Dateien nicht verfügbar · GitHub: 502", ""},
	}
	for _, fall := range faelle {
		t.Run(fall.name, func(t *testing.T) {
			bild := bgBild()
			mux := http.NewServeMux()
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
				fmt.Fprint(w, hdLeerTreiber)
			})
			mux.HandleFunc("/api/repos", func(w http.ResponseWriter, r *http.Request) { json200(w, ivRepos) })
			mux.HandleFunc("/api/arbeit", func(w http.ResponseWriter, r *http.Request) { json200(w, bmArbeit) })
			mux.HandleFunc("/api/repo-meta", func(w http.ResponseWriter, r *http.Request) { json200(w, ivMeta) })
			mux.HandleFunc("/api/beleg-galerie", func(w http.ResponseWriter, r *http.Request) { json200(w, bmGalerie) })
			mux.HandleFunc("/api/geaenderte-bilder", func(w http.ResponseWriter, r *http.Request) { json200(w, hdGeaendert) })
			mux.HandleFunc("/api/html-dateien", func(w http.ResponseWriter, r *http.Request) { json200(w, fall.antwort) })
			mux.HandleFunc("/api/blob", func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "image/png")
				_, _ = w.Write(bild)
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
			ctx, abbrechen := context.WithTimeout(context.Background(), 120*time.Second)
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
			var b hdLeerBericht
			if err := json.Unmarshal(roh, &b); err != nil {
				t.Fatalf("Bericht kein JSON: %v", err)
			}
			if b.Abbruch != "" {
				t.Fatalf("Treiber abgebrochen: %s\nFehler: %v\nDOM: %s", b.Abbruch, b.Fehler, b.DOM)
			}
			if len(b.Fehler) > 0 {
				t.Errorf("JavaScript-Fehler: %v", b.Fehler)
			}
			if b.Ueberschrift != "HTML-Dateien" || !b.UnterGeaendert {
				t.Errorf("Ueberschrift %q, unter Geaenderte Bilder %v", b.Ueberschrift, b.UnterGeaendert)
			}
			if b.Hinweis != fall.hinweis {
				t.Errorf("Hinweis %q, erwartet %q", b.Hinweis, fall.hinweis)
			}
			if b.Anzahl != 0 {
				t.Errorf("%d Zeilen, erwartet keine", b.Anzahl)
			}
			if fall.fotoEnv != "" {
				if ziel := os.Getenv(fall.fotoEnv); ziel != "" {
					if out, err := chromium(ctx, "--screenshot="+ziel); err != nil {
						t.Errorf("Bildschirmfoto: %v\n%s", err, out)
					}
				}
			}
		})
	}
}
