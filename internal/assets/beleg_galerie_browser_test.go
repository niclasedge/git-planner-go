package assets_test

// Browser-Test der Beleg-Galerie (beleg-galerie, #54): unter der Arbeitsliste
// steht der Abschnitt "Belegt" mit einer Kachel je Beleg in der Folge der
// Route. Ein zeigbarer Beleg erscheint als Bild, ein .dot- und ein Textbeleg
// als Kachel mit Typ und Namen; der archivierte Change ist gekennzeichnet.
// Folge und Typ kommen fertig aus der Fake-API, die Oberflaeche zeichnet nur.

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

const bgArbeit = `{"repo":"acme/alpha","fortschritt":{"erledigt":0,"gesamt":0},"zuletzt":[],
 "reihe":[{"art":"issue","nummer":54,"titel":"beleg-galerie: Galerie aller Belege samt Nicht-Bild-Kacheln","url":"u54","status":"ohne_change",
   "blockiert_durch":[],"labels":[],"zustaendig":[],"hat_text":false,"branches":[]}],
 "ohne_issue":[],"prs":[],"quellen":[]}`

const bgGalerie = `{"repo":"acme/alpha","belege":[
 {"kennung":"neu","archiviert":false,"stufe":"nachher","art":"graph","zeit":"2026-09-25T10:00:00.000Z","pfad":"belege/artefacts/0-graph.dot",
  "name":"0-graph.dot","typ":"dot","zeigbar":false,"fehlt":false,"link":"https://github.com/acme/alpha/blob/change/neu/openspec/changes/neu/belege/artefacts/0-graph.dot"},
 {"kennung":"alt","archiviert":true,"stufe":"nachher","art":"bild","zeit":"2026-09-20T08:00:00.000Z","pfad":"belege/assets/0-x.png",
  "name":"0-x.png","typ":"png","zeigbar":true,"fehlt":false,"bild":"/api/blob?repo=acme%2Falpha&sha=b-png&name=0-x.png",
  "link":"https://github.com/acme/alpha/blob/main/openspec/changes/archive/2026-09-20-alt/belege/assets/0-x.png"},
 {"kennung":"alt","archiviert":true,"stufe":"vorher","art":"text","zeit":"2026-09-19T08:00:00.000Z","pfad":"belege/artefacts/0-lauf.txt",
  "name":"0-lauf.txt","typ":"txt","zeigbar":false,"fehlt":false,
  "link":"https://github.com/acme/alpha/blob/main/openspec/changes/archive/2026-09-20-alt/belege/artefacts/0-lauf.txt"}
]}`

const bgTreiber = `
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
      await warte('Galerie', function () { return document.querySelectorAll('.kn-galerie [data-beleg]').length >= 3; });
      await warte('Bild geladen', function () { var i = q('.kn-galerie img'); return i && i.complete && i.naturalWidth > 0; });
      await schlaf(200);
      var h = q('.kn-galerie h2');
      b.ueberschrift = h ? h.textContent.trim() : '';
      b.kacheln = Array.prototype.map.call(document.querySelectorAll('.kn-galerie [data-beleg]'), function (k) {
        var img = k.querySelector('img');
        var typ = k.querySelector('.kn-g-typ'), name = k.querySelector('.kn-g-name');
        var a = k.querySelector('a');
        return { beleg: k.getAttribute('data-beleg'), bild: !!img, breite: img ? img.naturalWidth : 0,
                 typ: typ ? typ.textContent.trim() : '', name: name ? name.textContent.trim() : '',
                 archiv: !!k.querySelector('.kn-g-archiv'), link: a ? a.getAttribute('href') : '',
                 text: k.textContent.replace(/\s+/g, ' ').trim() };
      });
    } catch (e) {
      b.abbruch = String(e && e.message || e);
      b.dom = document.body.innerHTML.slice(0, 3000);
    }
    berichten(b);
  }
  lauf();
})();
`

type bgKachel struct {
	Beleg  string `json:"beleg"`
	Bild   bool   `json:"bild"`
	Breite int    `json:"breite"`
	Typ    string `json:"typ"`
	Name   string `json:"name"`
	Archiv bool   `json:"archiv"`
	Link   string `json:"link"`
	Text   string `json:"text"`
}

// bgBild ist ein kleines, sichtbares PNG fuer die Bildkachel.
func bgBild() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 240, 150))
	for y := 0; y < 150; y++ {
		for x := 0; x < 240; x++ {
			c := color.RGBA{R: 40, G: 110, B: 200, A: 255}
			if (x/30+y/30)%2 == 0 {
				c = color.RGBA{R: 90, G: 170, B: 110, A: 255}
			}
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

func TestBelegGalerieImBrowser(t *testing.T) {
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
		fmt.Fprint(w, bgTreiber)
	})
	mux.HandleFunc("/api/repos", func(w http.ResponseWriter, r *http.Request) { json200(w, ivRepos) })
	mux.HandleFunc("/api/arbeit", func(w http.ResponseWriter, r *http.Request) { json200(w, bgArbeit) })
	mux.HandleFunc("/api/repo-meta", func(w http.ResponseWriter, r *http.Request) { json200(w, ivMeta) })
	mux.HandleFunc("/api/beleg-galerie", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("repo") != "acme/alpha" {
			http.Error(w, `{"grund":"falsches Repo"}`, http.StatusBadRequest)
			return
		}
		json200(w, bgGalerie)
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
		Abbruch      string     `json:"abbruch"`
		DOM          string     `json:"dom"`
		Fehler       []string   `json:"fehler"`
		Ueberschrift string     `json:"ueberschrift"`
		Kacheln      []bgKachel `json:"kacheln"`
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
	if b.Ueberschrift != "Belegt" {
		t.Errorf("Ueberschrift %q, erwartet Belegt", b.Ueberschrift)
	}
	if len(b.Kacheln) != 3 {
		t.Fatalf("%d Kacheln, erwartet 3: %+v", len(b.Kacheln), b.Kacheln)
	}
	folge := []string{b.Kacheln[0].Beleg, b.Kacheln[1].Beleg, b.Kacheln[2].Beleg}
	if strings.Join(folge, ",") != "neu/0-graph.dot,alt/0-x.png,alt/0-lauf.txt" {
		t.Errorf("Folge %v, erwartet die der Route", folge)
	}

	t.Run("Bildkachel", func(t *testing.T) {
		k := b.Kacheln[1]
		if !k.Bild || k.Breite != 240 {
			t.Errorf("Bildkachel %+v, erwartet ein geladenes Bild von 240 Pixeln", k)
		}
		if !k.Archiv {
			t.Errorf("Bildkachel des archivierten Changes nicht gekennzeichnet: %+v", k)
		}
		for _, teil := range []string{"alt", "nachher"} {
			if !strings.Contains(k.Text, teil) {
				t.Errorf("Bildkachel ohne %q: %q", teil, k.Text)
			}
		}
	})
	t.Run("NichtBildKacheln", func(t *testing.T) {
		for i, w := range []struct{ typ, name, kennung, stufe string }{
			{"dot", "0-graph.dot", "neu", "nachher"}, {"txt", "0-lauf.txt", "alt", "vorher"},
		} {
			k := b.Kacheln[[]int{0, 2}[i]]
			if k.Bild {
				t.Errorf("%s als Bild gezeichnet: %+v", w.name, k)
			}
			if k.Typ != w.typ || k.Name != w.name {
				t.Errorf("Kachel %+v, erwartet Typ %q und Name %q", k, w.typ, w.name)
			}
			if !strings.Contains(k.Text, w.kennung) || !strings.Contains(k.Text, w.stufe) {
				t.Errorf("Kachel %+v ohne Change %q oder Stufe %q", k, w.kennung, w.stufe)
			}
			if !strings.HasPrefix(k.Link, "https://github.com/acme/alpha/blob/") {
				t.Errorf("Kachel %s ohne Link auf GitHub: %q", w.name, k.Link)
			}
		}
		if b.Kacheln[0].Archiv {
			t.Errorf("offener Change als archiviert gekennzeichnet: %+v", b.Kacheln[0])
		}
		if !b.Kacheln[2].Archiv {
			t.Errorf("Textbeleg des archivierten Changes nicht gekennzeichnet: %+v", b.Kacheln[2])
		}
	})

	// Auf Wunsch ein Bildschirmfoto derselben Seite als Beleg: Arbeitsliste
	// mit dem Abschnitt Belegt.
	if ziel := os.Getenv("GKP_BELEG_PNG"); ziel != "" {
		if out, err := chromium(ctx, "--screenshot="+ziel); err != nil {
			t.Errorf("Bildschirmfoto: %v\n%s", err, out)
		}
	}
}
