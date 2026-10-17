package assets_test

// Browser-Test der Bildansicht (beleg-bilder, #105): jedes Belegbild und jede
// Kachel unter "Geaenderte Bilder" oeffnet im selben Fenster ein Modal.
// Geprueft werden Oeffnen aus Galerie, Zeile und Detailspalte, Schliessen per
// Klick daneben, Esc und Knopf, Zoom ueber Knoepfe, Mausrad und Doppelklick,
// Verschieben und die Pfeiltasten, dazu der Abschnitt "Geaenderte Bilder" mit
// 12 von 13 Kacheln. Folge und Auswahl kommen fertig aus der Fake-API.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/niclasedge/git-planner-go/internal/assets"
)

const bmArbeit = `{"repo":"acme/alpha","fortschritt":{"erledigt":0,"gesamt":1},"zuletzt":[],
 "reihe":[{"art":"issue","nummer":105,"titel":"beleg-bilder: Belegbilder als Modal","url":"u105","status":"in_arbeit",
   "blockiert_durch":[],"labels":[],"zustaendig":[],"hat_text":true,"tiefe":0,"aktiv":false,"branches":[],
   "change":{"kennung":"beleg-bilder","ref":"change/beleg-bilder","archiviert":false,"status":"laeuft","issue":105,
    "aufgaben":[{"text":"Modal bauen","erledigt":false}],"erledigt":0,"gesamt":1,"gates":["gate1"],
    "belege":[{"zeit":"2026-09-28T09:00:00.000Z","stufe":"nachher","art":"bild","pfad":"belege/assets/0-zeile.png",
      "bild":"/api/blob?repo=acme%2Falpha&sha=b-zeile&name=0-zeile.png"}]}}],
 "ohne_issue":[],"prs":[],"quellen":[]}`

const bmDetail = `{"art":"change","kennung":"beleg-bilder","titel":"beleg-bilder","ref":"change/beleg-bilder",
 "proposal":"# beleg-bilder\n\nGeht zurueck auf GitHub-Issue #105.","tasks":"- [ ] Modal bauen","specs":[],
 "belege":[{"zeit":"2026-09-28T09:00:00.000Z","stufe":"nachher","art":"bild","pfad":"belege/assets/0-zeile.png",
   "bild":"/api/blob?repo=acme%2Falpha&sha=b-detail&name=0-zeile.png"}]}`

const bmGalerie = `{"repo":"acme/alpha","belege":[
 {"kennung":"neu","archiviert":false,"stufe":"nachher","art":"graph","zeit":"2026-09-25T10:00:00.000Z","pfad":"belege/artefacts/0-graph.dot",
  "name":"0-graph.dot","typ":"dot","zeigbar":false,"fehlt":false,"link":"https://github.com/acme/alpha/blob/change/neu/openspec/changes/neu/belege/artefacts/0-graph.dot"},
 {"kennung":"alt","archiviert":true,"stufe":"nachher","art":"bild","zeit":"2026-09-20T08:00:00.000Z","pfad":"belege/assets/0-x.png",
  "name":"0-x.png","typ":"png","zeigbar":true,"fehlt":false,"sha":"b-png","bild":"/api/blob?repo=acme%2Falpha&sha=b-png&name=0-x.png",
  "link":"https://github.com/acme/alpha/blob/main/openspec/changes/archive/2026-09-20-alt/belege/assets/0-x.png"}
]}`

// bmGeaendert sind 13 geaenderte Bilder, neueste zuerst, wie die Route sie
// liefert.
func bmGeaendert() string {
	var b []string
	for i := 1; i <= 13; i++ {
		pfad := fmt.Sprintf("docs/entwurf/bild-%02d.png", i)
		b = append(b, fmt.Sprintf(`{"pfad":%q,"name":"bild-%02d.png","typ":"png","sha":"g-%02d","zeit":"2026-09-%02dT10:00:00Z",
		 "commit":"c%02d","betreff":"Entwurf %d nachgezogen","bild":"/api/blob?repo=acme%%2Falpha&sha=g-%02d&name=bild-%02d.png",
		 "link":"https://github.com/acme/alpha/blob/c%02d/%s"}`, pfad, i, i, 28-i, i, i, i, i, i, pfad))
	}
	return `{"repo":"acme/alpha","bilder":[` + strings.Join(b, ",") + `]}`
}

const bmTreiber = `
(function () {
  var fehler = [];
  window.addEventListener('error', function (e) { fehler.push(String(e.message)); });
  window.addEventListener('unhandledrejection', function (e) { fehler.push('rejection: ' + String(e.reason)); });
  var fenster = 0;
  window.open = function () { fenster++; return null; };
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
    b.fenster = fenster;
    var pre = document.createElement('pre');
    pre.id = 'kn-testbericht';
    pre.textContent = btoa(unescape(encodeURIComponent(JSON.stringify(b))));
    document.body.appendChild(pre);
  }
  function bild() { return q('.kn-modal img.kn-modal-bild'); }
  function breite() { var i = bild(); return i ? i.getBoundingClientRect().width : 0; }
  // Stand des Modals: Zahl, Bild, Einpassung, Beschriftung.
  function modal() {
    var m = qa('.kn-modal');
    var s = { anzahl: m.length };
    if (!m.length) return s;
    var i = bild(), bu = q('.kn-modal-buehne');
    s.src = i ? i.getAttribute('src') : '';
    s.geladen = !!(i && i.complete && i.naturalWidth > 0);
    if (i && bu) {
      var r = i.getBoundingClientRect(), br = bu.getBoundingClientRect(), cs = getComputedStyle(bu);
      var bw = bu.clientWidth - parseFloat(cs.paddingLeft) - parseFloat(cs.paddingRight);
      var bh = bu.clientHeight - parseFloat(cs.paddingTop) - parseFloat(cs.paddingBottom);
      s.breite = r.width; s.hoehe = r.height; s.buehneB = bw; s.buehneH = bh;
      s.innen = r.left >= br.left - 1 && r.top >= br.top - 1 && r.right <= br.right + 1 && r.bottom <= br.bottom + 1;
    }
    s.titel = text(q('.kn-modal-titel'));
    s.meta = text(q('.kn-modal-meta'));
    var o = q('.kn-modal a.kn-modal-original');
    s.original = o ? text(o) : '';
    s.originalHref = o ? o.getAttribute('href') : '';
    return s;
  }
  async function oeffnen(el) {
    if (!el) throw new Error('kein Element zum Oeffnen');
    el.click();
    await warte('Modal offen', function () { return qa('.kn-modal').length === 1 && bild() && bild().complete && bild().naturalWidth > 0; });
    await schlaf(80);
  }
  async function zu() {
    await warte('Modal zu', function () { return qa('.kn-modal').length === 0; });
  }
  function taste(k) { document.dispatchEvent(new KeyboardEvent('keydown', { key: k, bubbles: true, cancelable: true })); }
  function zeiger(typ, el, x, y) {
    el.dispatchEvent(new PointerEvent(typ, { bubbles: true, cancelable: true, clientX: x, clientY: y,
      pointerId: 1, pointerType: 'mouse', button: 0, buttons: typ === 'pointerup' ? 0 : 1, isPrimary: true }));
  }
  async function lauf() {
    var b = {};
    try {
      await warte('Repo in der Leiste', function () { return q('[data-repo="acme/alpha"]'); });
      q('[data-repo="acme/alpha"]').click();
      await warte('Galerie', function () { return q('.kn-galerie [data-beleg="alt/0-x.png"] [data-bild-oeffnen]'); });
      await warte('Geaenderte Bilder', function () { return qa('.kn-geaendert [data-geaendert]').length >= 12; });
      await schlaf(150);

      // Geaenderte Bilder: 12 von 13, dann alle.
      b.gbUeberschrift = text(q('.kn-geaendert h2'));
      b.gbAnfangs = Array.prototype.map.call(qa('.kn-geaendert [data-geaendert]'), function (k) {
        return { pfad: k.getAttribute('data-geaendert'), name: text(k.querySelector('.kn-gb-name')),
                 pfadText: text(k.querySelector('.kn-gb-pfad')), datum: text(k.querySelector('.kn-gb-datum')),
                 betreff: text(k.querySelector('.kn-gb-betreff')), bild: !!k.querySelector('img') };
      });
      var mehr = q('.kn-geaendert .kn-gb-mehr');
      b.gbMehr = text(mehr);
      if (mehr) mehr.click();
      await schlaf(80);
      b.gbAlle = qa('.kn-geaendert [data-geaendert]').length;

      // Galerie: oeffnen, Klick daneben.
      var galerieBild = q('.kn-galerie [data-beleg="alt/0-x.png"] [data-bild-oeffnen]');
      b.galerieNeuerTab = !!galerieBild.closest('a[target]');
      var vorher = location.href;
      await oeffnen(galerieBild);
      b.galerie = modal();
      var bu = q('.kn-modal-buehne'), br = bu.getBoundingClientRect();
      var daneben = document.elementFromPoint(br.left + 4, br.top + 4);
      b.danebenZiel = daneben ? daneben.className : '';
      daneben.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true, clientX: br.left + 4, clientY: br.top + 4 }));
      await zu();
      b.nachDaneben = modal().anzahl;
      b.ortGleich = location.href === vorher;

      // Esc und Schliessen-Knopf.
      await oeffnen(q('.kn-galerie [data-beleg="alt/0-x.png"] [data-bild-oeffnen]'));
      taste('Escape');
      await zu();
      b.nachEsc = modal().anzahl;
      await oeffnen(q('.kn-galerie [data-beleg="alt/0-x.png"] [data-bild-oeffnen]'));
      var knopf = q('.kn-modal .kn-modal-zu');
      b.zuKnopf = !!knopf;
      if (knopf) knopf.click();
      await zu();
      b.nachKnopf = modal().anzahl;

      // Zoom.
      await oeffnen(q('.kn-galerie [data-beleg="alt/0-x.png"] [data-bild-oeffnen]'));
      var z = {};
      z.eingepasst = breite();
      q('.kn-modal [data-zoom="100"]').click(); await schlaf(60);
      z.hundert = breite();
      bild().dispatchEvent(new MouseEvent('dblclick', { bubbles: true, cancelable: true })); await schlaf(60);
      z.doppelklick = breite();
      q('.kn-modal [data-zoom="rein"]').click(); await schlaf(60);
      z.plus = breite();
      q('.kn-modal [data-zoom="raus"]').click(); await schlaf(60);
      z.minus = breite();
      q('.kn-modal [data-zoom="rein"]').click(); await schlaf(60);
      z.plus2 = breite();
      var ir = bild().getBoundingClientRect();
      q('.kn-modal-buehne').dispatchEvent(new WheelEvent('wheel', { bubbles: true, cancelable: true, deltaY: -120,
        clientX: ir.left + ir.width / 2, clientY: ir.top + ir.height / 2 }));
      await schlaf(60);
      z.rad = breite();
      ir = bild().getBoundingClientRect();
      var cx = ir.left + ir.width / 2, cy = ir.top + ir.height / 2;
      z.linksVor = ir.left; z.obenVor = ir.top;
      zeiger('pointerdown', bild(), cx, cy);
      zeiger('pointermove', bild(), cx + 20, cy + 10);
      zeiger('pointermove', bild(), cx + 50, cy + 30);
      zeiger('pointerup', bild(), cx + 50, cy + 30);
      await schlaf(60);
      ir = bild().getBoundingClientRect();
      z.linksNach = ir.left; z.obenNach = ir.top;
      q('.kn-modal [data-zoom="einpassen"]').click(); await schlaf(60);
      z.einpassen = breite();
      b.zoom = z;
      taste('Escape');
      await zu();

      // Geaenderte Bilder: Modal und Pfeiltasten.
      var kacheln = qa('.kn-geaendert [data-geaendert] [data-bild-oeffnen]');
      b.gbNeuerTab = !!(kacheln[0] && kacheln[0].closest('a[target]'));
      await oeffnen(kacheln[0]);
      b.gbErstes = modal();
      taste('ArrowRight');
      await warte('naechstes Bild', function () { return bild() && bild().getAttribute('src').indexOf('g-02') >= 0; });
      await schlaf(60);
      b.gbRechts = modal();
      taste('ArrowLeft');
      await warte('voriges Bild', function () { return bild() && bild().getAttribute('src').indexOf('g-01') >= 0; });
      b.gbLinks = modal();
      taste('Escape');
      await zu();

      // Zeile eines Eintrags.
      q('[data-eintrag] .kn-e-titel').click();
      await warte('Belegbild in der Zeile', function () { return q('.kn-e-belege [data-bild-oeffnen]'); });
      b.zeileNeuerTab = !!q('.kn-e-belege [data-bild-oeffnen]').closest('a[target]');
      await oeffnen(q('.kn-e-belege [data-bild-oeffnen]'));
      b.zeile = modal();
      taste('Escape');
      await zu();

      // Detailspalte.
      q('[data-eintrag] .kn-e-details').click();
      await warte('Belegbild in der Detailspalte', function () { return q('.kn-detail [data-bild-oeffnen]'); });
      b.detailNeuerTab = !!q('.kn-detail [data-bild-oeffnen]').closest('a[target]');
      await oeffnen(q('.kn-detail [data-bild-oeffnen]'));
      b.detail = modal();
      taste('Escape');
      await zu();
      var zuD = q('.kn-detail .kn-d-zu, .kn-detail [aria-label="Schließen"]');
      if (zuD) zuD.click();

      // Fuer das Bildschirmfoto: die Galerie im Blick, darueber das Modal.
      var gal = q('.kn-galerie');
      if (gal) gal.scrollIntoView({ block: 'start' });
      await schlaf(100);
      if (location.hash !== '#ohne-modal') {
        await oeffnen(q('.kn-galerie [data-beleg="alt/0-x.png"] [data-bild-oeffnen]'));
      }
    } catch (e) {
      b.abbruch = String(e && e.message || e);
      b.dom = document.body.innerHTML.slice(0, 4000);
    }
    berichten(b);
  }
  lauf();
})();
`

type bmModal struct {
	Anzahl       int     `json:"anzahl"`
	Src          string  `json:"src"`
	Geladen      bool    `json:"geladen"`
	Breite       float64 `json:"breite"`
	Hoehe        float64 `json:"hoehe"`
	BuehneB      float64 `json:"buehneB"`
	BuehneH      float64 `json:"buehneH"`
	Innen        bool    `json:"innen"`
	Titel        string  `json:"titel"`
	Meta         string  `json:"meta"`
	Original     string  `json:"original"`
	OriginalHref string  `json:"originalHref"`
}

type bmKachel struct {
	Pfad     string `json:"pfad"`
	Name     string `json:"name"`
	PfadText string `json:"pfadText"`
	Datum    string `json:"datum"`
	Betreff  string `json:"betreff"`
	Bild     bool   `json:"bild"`
}

type bmZoom struct {
	Eingepasst  float64 `json:"eingepasst"`
	Hundert     float64 `json:"hundert"`
	Doppelklick float64 `json:"doppelklick"`
	Plus        float64 `json:"plus"`
	Minus       float64 `json:"minus"`
	Plus2       float64 `json:"plus2"`
	Rad         float64 `json:"rad"`
	LinksVor    float64 `json:"linksVor"`
	ObenVor     float64 `json:"obenVor"`
	LinksNach   float64 `json:"linksNach"`
	ObenNach    float64 `json:"obenNach"`
	Einpassen   float64 `json:"einpassen"`
}

type bmBericht struct {
	Abbruch         string     `json:"abbruch"`
	DOM             string     `json:"dom"`
	Fehler          []string   `json:"fehler"`
	Fenster         int        `json:"fenster"`
	GbUeberschrift  string     `json:"gbUeberschrift"`
	GbAnfangs       []bmKachel `json:"gbAnfangs"`
	GbMehr          string     `json:"gbMehr"`
	GbAlle          int        `json:"gbAlle"`
	GalerieNeuerTab bool       `json:"galerieNeuerTab"`
	Galerie         bmModal    `json:"galerie"`
	DanebenZiel     string     `json:"danebenZiel"`
	NachDaneben     int        `json:"nachDaneben"`
	OrtGleich       bool       `json:"ortGleich"`
	NachEsc         int        `json:"nachEsc"`
	ZuKnopf         bool       `json:"zuKnopf"`
	NachKnopf       int        `json:"nachKnopf"`
	Zoom            bmZoom     `json:"zoom"`
	GbNeuerTab      bool       `json:"gbNeuerTab"`
	GbErstes        bmModal    `json:"gbErstes"`
	GbRechts        bmModal    `json:"gbRechts"`
	GbLinks         bmModal    `json:"gbLinks"`
	ZeileNeuerTab   bool       `json:"zeileNeuerTab"`
	Zeile           bmModal    `json:"zeile"`
	DetailNeuerTab  bool       `json:"detailNeuerTab"`
	Detail          bmModal    `json:"detail"`
}

func TestBildModalImBrowser(t *testing.T) {
	if testing.Short() {
		t.Skip("-short: der Test startet einen Browser")
	}
	chrome := chromiumFinden()
	if chrome == "" {
		t.Skip("kein Chromium gefunden — die Oberflaeche ist damit NICHT belegt")
	}
	bild := bgBild()
	geaendert := bmGeaendert()
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
		fmt.Fprint(w, bmTreiber)
	})
	mux.HandleFunc("/api/repos", func(w http.ResponseWriter, r *http.Request) { json200(w, ivRepos) })
	mux.HandleFunc("/api/arbeit", func(w http.ResponseWriter, r *http.Request) { json200(w, bmArbeit) })
	mux.HandleFunc("/api/repo-meta", func(w http.ResponseWriter, r *http.Request) { json200(w, ivMeta) })
	mux.HandleFunc("/api/detail", func(w http.ResponseWriter, r *http.Request) { json200(w, bmDetail) })
	mux.HandleFunc("/api/beleg-galerie", func(w http.ResponseWriter, r *http.Request) { json200(w, bmGalerie) })
	mux.HandleFunc("/api/geaenderte-bilder", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("repo") != "acme/alpha" {
			http.Error(w, `{"grund":"falsches Repo"}`, http.StatusBadRequest)
			return
		}
		json200(w, geaendert)
	})
	mux.HandleFunc("/api/blob", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(bild)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	chromium := func(ctx context.Context, adresse string, extra ...string) ([]byte, error) {
		args := append([]string{
			"--headless", "--no-sandbox", "--disable-gpu", "--no-first-run",
			"--disable-extensions", "--disable-dev-shm-usage", "--hide-scrollbars",
			"--user-data-dir=" + t.TempDir(),
			"--window-size=1400,900",
			"--virtual-time-budget=30000",
		}, extra...)
		return exec.CommandContext(ctx, chrome, append(args, adresse)...).Output()
	}

	ctx, abbrechen := context.WithTimeout(context.Background(), 120*time.Second)
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
	var b bmBericht
	if err := json.Unmarshal(roh, &b); err != nil {
		t.Fatalf("Bericht kein JSON: %v", err)
	}
	if b.Abbruch != "" {
		t.Fatalf("Treiber abgebrochen: %s\nFehler: %v\nBericht: %+v\nDOM: %s", b.Abbruch, b.Fehler, b, b.DOM)
	}
	if len(b.Fehler) > 0 {
		t.Errorf("JavaScript-Fehler: %v", b.Fehler)
	}
	if b.Fenster != 0 {
		t.Errorf("window.open %d-mal gerufen, erwartet kein neues Fenster", b.Fenster)
	}

	t.Run("GeaenderteBilder", func(t *testing.T) {
		if b.GbUeberschrift != "Geänderte Bilder" {
			t.Errorf("Ueberschrift %q", b.GbUeberschrift)
		}
		if len(b.GbAnfangs) != 12 {
			t.Fatalf("%d Kacheln anfangs, erwartet 12", len(b.GbAnfangs))
		}
		for i, k := range b.GbAnfangs {
			pfad := fmt.Sprintf("docs/entwurf/bild-%02d.png", i+1)
			if k.Pfad != pfad {
				t.Errorf("Kachel %d: Pfad %q, erwartet %q (Folge der Route)", i, k.Pfad, pfad)
			}
		}
		k := b.GbAnfangs[0]
		if !k.Bild || k.Name != "bild-01.png" || k.PfadText != "docs/entwurf/bild-01.png" || k.Betreff != "Entwurf 1 nachgezogen" || !strings.Contains(k.Datum, "2026-09-27") {
			t.Errorf("erste Kachel %+v, erwartet Bild, Name, Pfad, Datum 2026-09-27 und Betreff", k)
		}
		if b.GbMehr != "Alle 13 zeigen" {
			t.Errorf("Knopf %q, erwartet Alle 13 zeigen", b.GbMehr)
		}
		if b.GbAlle != 13 {
			t.Errorf("%d Kacheln nach Alle zeigen, erwartet 13", b.GbAlle)
		}
	})

	t.Run("OeffnenUndSchliessen", func(t *testing.T) {
		if b.GalerieNeuerTab {
			t.Error("Galeriebild oeffnet noch einen neuen Tab")
		}
		g := b.Galerie
		if g.Anzahl != 1 || !g.Geladen || !strings.Contains(g.Src, "sha=b-png") {
			t.Fatalf("Modal aus der Galerie %+v, erwartet ein Modal mit dem geladenen Bild", g)
		}
		if !g.Innen || math.Abs(g.Breite/g.Hoehe-240.0/150.0) > 0.02 {
			t.Errorf("Bild %vx%v nicht mit Seitenverhaeltnis eingepasst (Buehne %vx%v)", g.Breite, g.Hoehe, g.BuehneB, g.BuehneH)
		}
		if math.Abs(g.Breite-g.BuehneB) > 2 && math.Abs(g.Hoehe-g.BuehneH) > 2 {
			t.Errorf("Bild %vx%v nicht so gross wie moeglich (Buehne %vx%v)", g.Breite, g.Hoehe, g.BuehneB, g.BuehneH)
		}
		if !strings.Contains(g.Titel, "alt") || !strings.Contains(g.Meta, "nachher") || !strings.Contains(g.Meta, "2026-09-20 08:00") {
			t.Errorf("Beschriftung %q / %q, erwartet alt, nachher und die Zeit", g.Titel, g.Meta)
		}
		if g.Original != "Original öffnen" || g.OriginalHref != "https://github.com/acme/alpha/blob/main/openspec/changes/archive/2026-09-20-alt/belege/assets/0-x.png" {
			t.Errorf("Link %q -> %q", g.Original, g.OriginalHref)
		}
		if !strings.Contains(b.DanebenZiel, "kn-modal") {
			t.Errorf("Klick daneben traf %q, nicht den Rand des Modals", b.DanebenZiel)
		}
		if b.NachDaneben != 0 || b.NachEsc != 0 || b.NachKnopf != 0 || !b.ZuKnopf {
			t.Errorf("nach Klick daneben %d, nach Esc %d, nach Knopf %d (Knopf da: %v) Modale, erwartet je 0", b.NachDaneben, b.NachEsc, b.NachKnopf, b.ZuKnopf)
		}
		if !b.OrtGleich {
			t.Error("die Seite hat die Adresse gewechselt")
		}
	})

	t.Run("Zoom", func(t *testing.T) {
		z := b.Zoom
		if math.Abs(z.Hundert-240) > 0.6 {
			t.Errorf("100 %%: %v Pixel, erwartet 240", z.Hundert)
		}
		if math.Abs(z.Doppelklick-z.Eingepasst) > 0.6 {
			t.Errorf("Doppelklick: %v, erwartet eingepasst %v", z.Doppelklick, z.Eingepasst)
		}
		if z.Plus <= z.Doppelklick+1 || z.Minus >= z.Plus-1 || z.Plus2 <= z.Minus+1 {
			t.Errorf("Knoepfe: eingepasst %v, + %v, − %v, + %v", z.Doppelklick, z.Plus, z.Minus, z.Plus2)
		}
		if z.Rad <= z.Plus2+1 {
			t.Errorf("Mausrad: %v, erwartet breiter als %v", z.Rad, z.Plus2)
		}
		if math.Abs(z.LinksNach-z.LinksVor-50) > 2 || math.Abs(z.ObenNach-z.ObenVor-30) > 2 {
			t.Errorf("Verschieben: links %v -> %v, oben %v -> %v, erwartet +50/+30", z.LinksVor, z.LinksNach, z.ObenVor, z.ObenNach)
		}
		if math.Abs(z.Einpassen-z.Eingepasst) > 0.6 {
			t.Errorf("Einpassen: %v, erwartet %v", z.Einpassen, z.Eingepasst)
		}
	})

	t.Run("PfeiltastenUndGeaendert", func(t *testing.T) {
		if b.GbNeuerTab {
			t.Error("Kachel unter Geaenderte Bilder oeffnet einen neuen Tab")
		}
		if b.GbErstes.Anzahl != 1 || !strings.Contains(b.GbErstes.Src, "sha=g-01") || !strings.Contains(b.GbErstes.Titel, "docs/entwurf/bild-01.png") {
			t.Errorf("Modal aus Geaenderte Bilder %+v", b.GbErstes)
		}
		if b.GbErstes.OriginalHref != "https://github.com/acme/alpha/blob/c01/docs/entwurf/bild-01.png" {
			t.Errorf("Original %q", b.GbErstes.OriginalHref)
		}
		if !strings.Contains(b.GbRechts.Src, "sha=g-02") || !strings.Contains(b.GbRechts.Titel, "bild-02.png") {
			t.Errorf("nach → %+v, erwartet das zweite Bild", b.GbRechts)
		}
		if !strings.Contains(b.GbLinks.Src, "sha=g-01") {
			t.Errorf("nach ← %+v, erwartet wieder das erste Bild", b.GbLinks)
		}
	})

	t.Run("ZeileUndDetailspalte", func(t *testing.T) {
		if b.ZeileNeuerTab || b.DetailNeuerTab {
			t.Errorf("neuer Tab: Zeile %v, Detailspalte %v", b.ZeileNeuerTab, b.DetailNeuerTab)
		}
		if b.Zeile.Anzahl != 1 || !strings.Contains(b.Zeile.Src, "sha=b-zeile") || !strings.Contains(b.Zeile.Meta, "nachher") {
			t.Errorf("Modal aus der Zeile %+v", b.Zeile)
		}
		if b.Detail.Anzahl != 1 || !strings.Contains(b.Detail.Src, "sha=b-detail") {
			t.Errorf("Modal aus der Detailspalte %+v", b.Detail)
		}
	})

	// Auf Wunsch Bildschirmfotos als Beleg: das Modal ueber der Galerie und
	// die Galerie samt Geaenderten Bildern ohne Modal.
	if ziel := os.Getenv("GKP_BELEG_PNG_BILD_MODAL"); ziel != "" {
		if out, err := chromium(ctx, srv.URL+"/", "--screenshot="+ziel); err != nil {
			t.Errorf("Bildschirmfoto: %v\n%s", err, out)
		}
	}
	if ziel := os.Getenv("GKP_BELEG_PNG_BILD_MODAL_LISTE"); ziel != "" {
		if out, err := chromium(ctx, srv.URL+"/#ohne-modal", "--screenshot="+ziel); err != nil {
			t.Errorf("Bildschirmfoto: %v\n%s", err, out)
		}
	}
}
