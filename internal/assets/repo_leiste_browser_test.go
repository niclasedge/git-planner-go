package assets_test

// Browser-Test der Things-Oberflaeche mit Repos, Kategorien, Arbeitsliste und
// Detailspalte (repo-leiste, #25). Ersetzt den Test der frueheren
// Aufgabenliste (things-oberflaeche, #12).
//
// Das echte, eingebettete Bundle (assets.AppJS) laeuft in headless Chromium
// gegen eine Fake-API, die jede Anfrage protokolliert. Ein Treiber-Script, das
// NUR auf der Testseite steht, klickt sich durch die Szenarien der Spec, setzt
// Marken ins Protokoll und legt am Ende einen Bericht in die Seite. Chromium
// gibt die Seite per --dump-dom aus; der Test liest Bericht und Protokoll.
// Fehlt Chromium, ueberspringt sich der Test — dann ist die Oberflaeche NICHT
// belegt.

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
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/niclasedge/git-planner-go/internal/assets"
)

// chromiumFinden sucht ein startbares Chromium: zuerst GKP_CHROMIUM, dann
// den PATH, dann die Versionsordner einer Playwright-Installation. Ein
// Symlink, der ins Leere zeigt, zaehlt nicht.
func chromiumFinden() string {
	ausfuehrbar := func(p string) bool {
		fi, err := os.Stat(p)
		return err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0
	}
	if p := os.Getenv("GKP_CHROMIUM"); p != "" && ausfuehrbar(p) {
		return p
	}
	for _, name := range []string{"chromium", "chromium-browser", "google-chrome", "google-chrome-stable", "headless_shell"} {
		if p, err := exec.LookPath(name); err == nil && ausfuehrbar(p) {
			return p
		}
	}
	wurzeln := []string{}
	if p := os.Getenv("PLAYWRIGHT_BROWSERS_PATH"); p != "" {
		wurzeln = append(wurzeln, p)
	}
	if home, err := os.UserHomeDir(); err == nil {
		wurzeln = append(wurzeln, filepath.Join(home, ".cache", "ms-playwright"))
	}
	for _, w := range wurzeln {
		for _, muster := range []string{"chromium-*/chrome-linux/chrome", "chromium_headless_shell-*/chrome-linux/headless_shell"} {
			treffer, _ := filepath.Glob(filepath.Join(w, muster))
			sort.Sort(sort.Reverse(sort.StringSlice(treffer)))
			for _, p := range treffer {
				if ausfuehrbar(p) {
					return p
				}
			}
		}
	}
	return ""
}

// anfrage ist eine Zeile im Protokoll der Fake-API.
type anfrage struct {
	Methode string
	Pfad    string
	Query   string
	Rumpf   string
	Marke   string
}

type fakeAPI struct {
	mu        sync.Mutex
	protokoll []anfrage
	// Zustand, den die Schreibrouten veraendern.
	arbeitZu bool
	kunden   bool
}

func (f *fakeAPI) log(a anfrage) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.protokoll = append(f.protokoll, a)
}

// abschnitte zerlegt das Protokoll an den Marken: Ergebnis[name] sind die
// Anfragen NACH der Marke name bis zur naechsten Marke; "" ist der Anfang.
func (f *fakeAPI) abschnitte() map[string][]anfrage {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string][]anfrage{"": nil}
	akt := ""
	for _, a := range f.protokoll {
		if a.Marke != "" {
			akt = a.Marke
			out[akt] = []anfrage{}
			continue
		}
		out[akt] = append(out[akt], a)
	}
	return out
}

func (f *fakeAPI) repos() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	alpha := `{"nwo":"acme/alpha","name":"alpha","owner":"acme","beschreibung":"Alpha-Dienst","privat":true,"sprache":"Go",
	  "default_branch":"main","gepusht":"2026-09-23T10:00:00Z","url":"https://github.com/acme/alpha","offene_issues":4,"pat":"GITHUB_PAT_WORK"}`
	gamma := `{"nwo":"ich/gamma","name":"gamma","owner":"ich","offene_issues":1,"pat":"GITHUB_PAT_PERSONAL"}`
	arbeit := `{"name":"Arbeit","ohne":false,"eingeklappt":` + fmt.Sprint(f.arbeitZu) + `,"repos":[` + alpha + `]}`
	if f.kunden {
		arbeit = `{"name":"Arbeit","ohne":false,"eingeklappt":false,"repos":[]},{"name":"Kunden","ohne":false,"eingeklappt":false,"repos":[` + alpha + `]}`
	}
	return `{"kategorien":[` + arbeit + `,{"name":"Ohne Bereich","ohne":true,"eingeklappt":true,"repos":[` + gamma + `]}],
	  "konten":[{"pat":"GITHUB_PAT_WORK","anzahl":1},{"pat":"GITHUB_PAT_PERSONAL","anzahl":1,"grund":"GitHub 401: Bad credentials"}],"stand":"2026-09-24T08:00:00Z"}`
}

const arbeitAlpha = `{"repo":"acme/alpha","fortschritt":{"erledigt":1,"gesamt":5},
 "zuletzt":[{"art":"issue","nummer":15,"titel":"Fertig gewordenes","url":"u15","status":"erledigt","blockiert_durch":[],"labels":[],"zustaendig":[],"hat_text":false,
   "change":{"kennung":"x","ref":"main","archiviert":true,"status":"erledigt","aufgaben":[{"text":"erledigt eins","erledigt":true}],"erledigt":1,"gesamt":1,"gates":["gate1","gate2","fertig"],
     "belege":[{"stufe":"nachher","art":"bild","pfad":"belege/assets/0-x.png","bild":"/api/blob?repo=acme%2Falpha&sha=b-png&name=0-x.png"}]}}],
 "reihe":[
  {"art":"issue","nummer":4,"titel":"Vier bauen","url":"u4","status":"in_arbeit","blockiert_durch":[],"labels":[{"name":"bug","farbe":"d73a4a"}],"zustaendig":["bo"],"hat_text":true,
   "change":{"kennung":"vier","ref":"change/vier","archiviert":false,"status":"laeuft","aufgaben":[{"text":"A","erledigt":true},{"text":"B","erledigt":false}],"erledigt":1,"gesamt":2,"gates":["gate1"],
     "belege":[{"stufe":"vorher","art":"bild","pfad":"belege/assets/0-v.png","bild":"/api/blob?repo=acme%2Falpha&sha=b-png&name=0-v.png"}]}},
  {"art":"issue","nummer":1,"titel":"Eins ohne Change","url":"u1","status":"ohne_change","blockiert_durch":[],"labels":[],"zustaendig":[],"hat_text":false}
 ],
 "ohne_issue":[{"art":"change","titel":"los","status":"bereit","blockiert_durch":[],"labels":[],"zustaendig":[],
   "change":{"kennung":"los","ref":"main","status":"frei","aufgaben":[],"erledigt":0,"gesamt":0,"gates":[],"belege":[]}}],
 "prs":[{"nummer":9,"titel":"Ein PR","url":"u9","labels":[],"zustaendig":[],"autor":"cy"}],
 "quellen":[{"quelle":"issues","grund":""},{"quelle":"openspec","grund":""}]}`

const detailVier = `{"art":"change","kennung":"vier","titel":"vier","ref":"change/vier",
 "proposal":"# vier\n\nDas ist **fett**.\n\n<script>window.boese=1</script><img src=x onerror=\"window.boese=2\">",
 "tasks":"- [x] A\n- [ ] B\n","design":"","specs":[{"capability":"kern","text":"## ADDED Requirements"}],"belege":[]}`

var pngBytes = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89\x00\x00\x00\rIDATx\x9cc\xf8\xff\xff?\x00\x05\xfe\x02\xfe\xa7\x35\x81\x84\x00\x00\x00\x00IEND\xaeB`\x82")

const testseite = `<!DOCTYPE html>
<html lang="de"><head><meta charset="utf-8"><title>things-test</title></head>
<body><div id="kn-svelte-root"></div>
<script src="/app.js"></script>
<script src="/treiber.js"></script>
</body></html>`

// Der Treiber. Welcher Teil dran ist, steht in sessionStorage (ueberlebt
// location.reload()). localStorage fasst er nicht an — das ist Sache der App.
const treiber = `
(function () {
  var fehler = [];
  window.addEventListener('error', function (e) { fehler.push(String(e.message)); });
  window.addEventListener('unhandledrejection', function (e) { fehler.push('rejection: ' + String(e.reason)); });

  function schlaf(ms) { return new Promise(function (r) { setTimeout(r, ms); }); }
  async function warte(was, pred) {
    for (var i = 0; i < 200; i++) {
      try { if (pred()) return; } catch (e) {}
      await schlaf(25);
    }
    throw new Error('Zeitueberschreitung: ' + was);
  }
  function marke(name) { return fetch('/__marke?name=' + encodeURIComponent(name)); }
  function alle(sel, wurzel) { return Array.prototype.slice.call((wurzel || document).querySelectorAll(sel)); }
  function txt(e, sel) { var x = e && e.querySelector(sel); return x ? x.textContent.trim() : null; }
  function eintrag(id) { return document.querySelector('[data-eintrag="' + id + '"]'); }
  function repo(nwo) { return document.querySelector('[data-repo="' + nwo + '"]'); }
  function bild() {
    return {
      kategorien: alle('[data-kategorie]').map(function (k) {
        return { name: k.getAttribute('data-kategorie'), eingeklappt: k.getAttribute('data-eingeklappt') === 'true',
                 repos: alle('[data-repo]', k).map(function (r) { return r.getAttribute('data-repo'); }) };
      }),
      gewaehlt: alle('[data-repo][aria-current="true"]').map(function (r) { return r.getAttribute('data-repo'); }),
      eintraege: alle('[data-eintrag]').map(function (e) {
        var c = e.querySelector('.kn-e-check');
        return { id: e.getAttribute('data-eintrag'), status: e.getAttribute('data-status'),
                 abgehakt: !!c && c.getAttribute('aria-checked') === 'true', titel: txt(e, '.kn-e-titel') };
      }),
      subtasks: alle('.kn-e-karte [data-subtask]').map(function (s) {
        return { text: s.textContent.trim(), abgehakt: s.getAttribute('aria-checked') === 'true' };
      }),
      belegBilder: alle('.kn-e-karte img').length,
      detail: (function () { var d = document.querySelector('.kn-detail-inhalt'); return d ? d.innerHTML : null; })(),
      einstellungen: (function () { var d = document.querySelector('.kn-einstellungen'); return d ? d.textContent : null; })(),
      schrift: getComputedStyle(document.querySelector('#kn-svelte-root > *') || document.body).fontFamily,
      boese: window.boese || 0
    };
  }

  function laden() { try { return JSON.parse(sessionStorage.getItem('kn-test') || '{}'); } catch (e) { return {}; } }
  function sichern(b) { sessionStorage.setItem('kn-test', JSON.stringify(b)); }
  function berichten(b) {
    b.fehler = (b.fehler || []).concat(fehler);
    var pre = document.createElement('pre');
    pre.id = 'kn-testbericht';
    pre.textContent = btoa(unescape(encodeURIComponent(JSON.stringify(b))));
    document.body.appendChild(pre);
  }
  function kopf(name) { return document.querySelector('[data-kategorie="' + name + '"] .kn-k-kopf'); }

  async function lauf() {
    var b = laden();
    try {
      if (!b.phase) {
        await warte('Leiste', function () { return alle('[data-kategorie]').length >= 2; });
        await schlaf(200);
        b.leiste = bild();
        await marke('vor-einklappen');
        kopf('Arbeit').click();
        await warte('eingeklappt', function () { return !repo('acme/alpha'); });
        await schlaf(300);
        b.eingeklappt = bild();
        await marke('vor-aufklappen-kategorie');
        kopf('Arbeit').click();
        await warte('aufgeklappt', function () { return repo('acme/alpha'); });
        await schlaf(200);
        await marke('vor-repo');
        repo('acme/alpha').click();
        await warte('Liste', function () { return eintrag('15') && eintrag('4') && eintrag('1'); });
        await schlaf(300);
        b.liste = bild();
        await marke('vor-aufklappen');
        eintrag('4').querySelector('.kn-e-titel').click();
        await warte('Karte', function () { return alle('.kn-e-karte [data-subtask]').length >= 2; });
        await schlaf(300);
        b.karte = bild();
        await marke('vor-details');
        eintrag('4').querySelector('.kn-e-details').click();
        await warte('Detail', function () { var d = document.querySelector('.kn-detail-inhalt'); return d && d.innerHTML.indexOf('fett') >= 0; });
        await schlaf(300);
        b.detail = bild();
        await marke('vor-settings');
        document.querySelector('.kn-einstellungen-knopf').click();
        await warte('Settings', function () { return document.querySelector('.kn-einstellungen'); });
        await schlaf(200);
        b.settings = bild();
        var feld = document.querySelector('.kn-neue-kategorie input');
        feld.value = 'Kunden';
        feld.dispatchEvent(new Event('input', { bubbles: true }));
        // Erst nach dem naechsten Zeichnen ist der Knopf freigegeben.
        await schlaf(100);
        document.querySelector('.kn-neue-kategorie button').click();
        await warte('Kunden', function () { return document.querySelector('[data-kategorie="Kunden"] [data-repo="acme/alpha"]'); });
        await schlaf(300);
        b.kunden = bild();
        await marke('vor-neuladen');
        b.phase = 'neuladen';
        b.fehler = fehler.slice();
        sichern(b);
        location.reload();
        return;
      }
      await warte('nach Neuladen', function () { return document.querySelector('[data-repo="acme/alpha"][aria-current="true"]') && eintrag('4'); });
      await schlaf(300);
      b.neugeladen = bild();
    } catch (e) {
      b.abbruch = String(e && e.message || e);
      b.stand = bild();
    }
    berichten(b);
  }
  lauf();
})();
`

type kategorieBild struct {
	Name        string   `json:"name"`
	Eingeklappt bool     `json:"eingeklappt"`
	Repos       []string `json:"repos"`
}

type eintragBild struct {
	ID       string `json:"id"`
	Status   string `json:"status"`
	Abgehakt bool   `json:"abgehakt"`
	Titel    string `json:"titel"`
}

type seitenBild struct {
	Kategorien []kategorieBild `json:"kategorien"`
	Gewaehlt   []string        `json:"gewaehlt"`
	Eintraege  []eintragBild   `json:"eintraege"`
	Subtasks   []struct {
		Text     string `json:"text"`
		Abgehakt bool   `json:"abgehakt"`
	} `json:"subtasks"`
	BelegBilder   int     `json:"belegBilder"`
	Detail        *string `json:"detail"`
	Einstellungen *string `json:"einstellungen"`
	Schrift       string  `json:"schrift"`
	Boese         int     `json:"boese"`
}

func (b *seitenBild) kategorie(name string) *kategorieBild {
	for i := range b.Kategorien {
		if b.Kategorien[i].Name == name {
			return &b.Kategorien[i]
		}
	}
	return nil
}

func (b *seitenBild) ids() string {
	var s []string
	for _, e := range b.Eintraege {
		s = append(s, e.ID)
	}
	return strings.Join(s, ",")
}

type bericht struct {
	Abbruch     string      `json:"abbruch"`
	Stand       *seitenBild `json:"stand"`
	Fehler      []string    `json:"fehler"`
	Leiste      *seitenBild `json:"leiste"`
	Eingeklappt *seitenBild `json:"eingeklappt"`
	Liste       *seitenBild `json:"liste"`
	Karte       *seitenBild `json:"karte"`
	Detail      *seitenBild `json:"detail"`
	Settings    *seitenBild `json:"settings"`
	Kunden      *seitenBild `json:"kunden"`
	Neugeladen  *seitenBild `json:"neugeladen"`
}

func nur(as []anfrage, methode, pfad string) []anfrage {
	out := []anfrage{}
	for _, a := range as {
		if a.Methode == methode && a.Pfad == pfad {
			out = append(out, a)
		}
	}
	return out
}

var berichtRe = regexp.MustCompile(`<pre id="kn-testbericht">([A-Za-z0-9+/=]+)</pre>`)

func TestRepoLeisteImBrowser(t *testing.T) {
	if testing.Short() {
		t.Skip("-short: der Test startet einen Browser")
	}
	chrome := chromiumFinden()
	if chrome == "" {
		t.Skip("kein Chromium gefunden (GKP_CHROMIUM, PATH, PLAYWRIGHT_BROWSERS_PATH) — die Oberflaeche ist damit NICHT belegt")
	}

	api := &fakeAPI{}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, testseite)
	})
	mux.HandleFunc("/app.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		fmt.Fprint(w, assets.AppJS)
	})
	mux.HandleFunc("/treiber.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		fmt.Fprint(w, treiber)
	})
	mux.HandleFunc("/__marke", func(w http.ResponseWriter, r *http.Request) {
		api.log(anfrage{Marke: r.URL.Query().Get("name")})
		w.WriteHeader(http.StatusNoContent)
	})
	protokolliere := func(r *http.Request) anfrage {
		rumpf, _ := io.ReadAll(r.Body)
		a := anfrage{Methode: r.Method, Pfad: r.URL.Path, Query: r.URL.RawQuery, Rumpf: string(rumpf)}
		api.log(a)
		return a
	}
	json200 := func(w http.ResponseWriter, s string) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, s)
	}
	mux.HandleFunc("/api/repos", func(w http.ResponseWriter, r *http.Request) {
		protokolliere(r)
		json200(w, api.repos())
	})
	mux.HandleFunc("/api/arbeit", func(w http.ResponseWriter, r *http.Request) {
		protokolliere(r)
		if r.URL.Query().Get("repo") != "acme/alpha" {
			w.WriteHeader(http.StatusNotFound)
			json200(w, `{"grund":"unbekannt"}`)
			return
		}
		json200(w, arbeitAlpha)
	})
	mux.HandleFunc("/api/detail", func(w http.ResponseWriter, r *http.Request) {
		protokolliere(r)
		json200(w, detailVier)
	})
	mux.HandleFunc("/api/blob", func(w http.ResponseWriter, r *http.Request) {
		protokolliere(r)
		w.Header().Set("Content-Type", "image/png")
		w.Write(pngBytes)
	})
	mux.HandleFunc("/action/task/bereich/einklappen", func(w http.ResponseWriter, r *http.Request) {
		a := protokolliere(r)
		var wunsch struct {
			Bereich     string `json:"bereich"`
			Eingeklappt bool   `json:"eingeklappt"`
		}
		_ = json.Unmarshal([]byte(a.Rumpf), &wunsch)
		api.mu.Lock()
		if wunsch.Bereich == "Arbeit" {
			api.arbeitZu = wunsch.Eingeklappt
		}
		api.mu.Unlock()
		json200(w, `{}`)
	})
	mux.HandleFunc("/action/task/bereich/anlegen", func(w http.ResponseWriter, r *http.Request) {
		protokolliere(r)
		json200(w, `{"bereich":"Kunden"}`)
	})
	mux.HandleFunc("/action/task/bereich", func(w http.ResponseWriter, r *http.Request) {
		a := protokolliere(r)
		if strings.Contains(a.Rumpf, `"Kunden"`) {
			api.mu.Lock()
			api.kunden = true
			api.mu.Unlock()
		}
		json200(w, `{}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ctx, abbrechen := context.WithTimeout(context.Background(), 90*time.Second)
	defer abbrechen()
	cmd := exec.CommandContext(ctx, chrome,
		"--headless", "--no-sandbox", "--disable-gpu", "--no-first-run",
		"--disable-extensions", "--disable-dev-shm-usage",
		"--user-data-dir="+t.TempDir(),
		"--virtual-time-budget=30000",
		"--dump-dom", srv.URL+"/")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("chromium (%s): %v\n%s", chrome, err, out)
	}
	m := berichtRe.FindSubmatch(out)
	if m == nil {
		t.Fatalf("kein Testbericht in der Seite — der Treiber kam nicht ans Ende. DOM:\n%s", out)
	}
	roh, err := base64.StdEncoding.DecodeString(string(m[1]))
	if err != nil {
		t.Fatalf("Bericht nicht lesbar: %v", err)
	}
	var b bericht
	if err := json.Unmarshal(roh, &b); err != nil {
		t.Fatalf("Bericht kein JSON: %v\n%s", err, roh)
	}
	if b.Abbruch != "" {
		t.Fatalf("Treiber abgebrochen: %s\nletzter Stand: %+v\nFehler: %v", b.Abbruch, b.Stand, b.Fehler)
	}
	if len(b.Fehler) > 0 {
		t.Errorf("JavaScript-Fehler in der Seite: %v", b.Fehler)
	}
	ab := api.abschnitte()

	// Szenario "Einklappen im Browser".
	t.Run("Leiste", func(t *testing.T) {
		l := b.Leiste
		arbeit, ohne := l.kategorie("Arbeit"), l.kategorie("Ohne Bereich")
		if arbeit == nil || ohne == nil || l.Kategorien[0].Name != "Arbeit" {
			t.Fatalf("Kategorien %+v", l.Kategorien)
		}
		if arbeit.Eingeklappt || strings.Join(arbeit.Repos, ",") != "acme/alpha" {
			t.Errorf("Arbeit %+v, erwartet aufgeklappt mit acme/alpha", arbeit)
		}
		if !ohne.Eingeklappt || len(ohne.Repos) != 0 {
			t.Errorf("Ohne Bereich %+v, erwartet eingeklappt ohne sichtbare Repos", ohne)
		}
		if strings.Contains(strings.ToLower(l.Schrift), "mono") {
			t.Errorf("Schrift %q — Things-Stil heisst Systemschrift ohne Monospace", l.Schrift)
		}
		posts := nur(ab["vor-einklappen"], http.MethodPost, "/action/task/bereich/einklappen")
		if len(posts) != 1 || !strings.Contains(posts[0].Rumpf, `"bereich":"Arbeit"`) || !strings.Contains(posts[0].Rumpf, `"eingeklappt":true`) {
			t.Errorf("POST einklappen %+v", posts)
		}
		if n := len(nur(ab["vor-einklappen"], http.MethodGet, "/api/repos")); n < 1 {
			t.Error("nach dem Einklappen wurde die Leiste nicht neu angefragt")
		}
		if k := b.Eingeklappt.kategorie("Arbeit"); k == nil || !k.Eingeklappt {
			t.Errorf("Arbeit nach dem Klick %+v", k)
		}
	})

	// Szenario "Liste in gelieferter Folge".
	t.Run("Arbeitsliste", func(t *testing.T) {
		as := nur(ab["vor-repo"], http.MethodGet, "/api/arbeit")
		if len(as) != 1 || as[0].Query != "repo=acme%2Falpha" {
			t.Fatalf("Anfragen an /api/arbeit %+v, erwartet genau eine mit repo=acme/alpha", as)
		}
		l := b.Liste
		if !strings.HasPrefix(l.ids(), "15,4,1") {
			t.Fatalf("Eintraege %s, erwartet 15,4,1 am Anfang", l.ids())
		}
		if !l.Eintraege[0].Abgehakt || l.Eintraege[1].Abgehakt || l.Eintraege[2].Abgehakt {
			t.Errorf("abgehakt %+v, erwartet nur #15", l.Eintraege)
		}
		if l.Eintraege[1].Status != "in_arbeit" || l.Eintraege[1].Titel != "Vier bauen" {
			t.Errorf("#4 %+v", l.Eintraege[1])
		}
		if !strings.Contains(l.ids(), "los") || !strings.Contains(l.ids(), "pr-9") {
			t.Errorf("Eintraege %s ohne den Change los oder den PR 9", l.ids())
		}
		if len(l.Gewaehlt) != 1 || l.Gewaehlt[0] != "acme/alpha" {
			t.Errorf("gewaehlt %v", l.Gewaehlt)
		}
	})

	// Szenario "Aufklappen und Details".
	t.Run("Karte", func(t *testing.T) {
		k := b.Karte
		if len(k.Subtasks) != 2 || k.Subtasks[0].Text != "A" || !k.Subtasks[0].Abgehakt || k.Subtasks[1].Text != "B" || k.Subtasks[1].Abgehakt {
			t.Errorf("Subtasks %+v, erwartet A (abgehakt), B", k.Subtasks)
		}
		if k.BelegBilder != 1 {
			t.Errorf("%d Belegbilder in der Karte, erwartet 1", k.BelegBilder)
		}
	})
	t.Run("Detailspalte", func(t *testing.T) {
		ds := nur(ab["vor-details"], http.MethodGet, "/api/detail")
		if len(ds) != 1 || !strings.Contains(ds[0].Query, "art=change") || !strings.Contains(ds[0].Query, "kennung=vier") {
			t.Fatalf("Anfragen an /api/detail %+v", ds)
		}
		d := b.Detail.Detail
		if d == nil || !strings.Contains(*d, "<strong>fett</strong>") {
			t.Fatalf("Detail %v, erwartet gerendertes Markdown", d)
		}
		if strings.Contains(*d, "<script") || strings.Contains(*d, "onerror") || b.Detail.Boese != 0 {
			t.Errorf("ausfuehrbares HTML in der Detailspalte (boese=%d): %s", b.Detail.Boese, *d)
		}
	})

	// Szenario "Neue Kategorie ueber Settings".
	t.Run("Settings", func(t *testing.T) {
		s := b.Settings.Einstellungen
		if s == nil || !strings.Contains(*s, "Alpha-Dienst") || !strings.Contains(*s, "GITHUB_PAT_WORK") {
			t.Errorf("Settings %v, erwartet Beschreibung und PAT", s)
		}
		an := nur(ab["vor-settings"], http.MethodPost, "/action/task/bereich/anlegen")
		zu := nur(ab["vor-settings"], http.MethodPost, "/action/task/bereich")
		if len(an) != 1 || !strings.Contains(an[0].Rumpf, `"Kunden"`) {
			t.Errorf("bereich/anlegen %+v", an)
		}
		if len(zu) != 1 || !strings.Contains(zu[0].Rumpf, `"projekt":"acme/alpha"`) || !strings.Contains(zu[0].Rumpf, `"bereich":"Kunden"`) {
			t.Errorf("bereich %+v", zu)
		}
		if k := b.Kunden.kategorie("Kunden"); k == nil || strings.Join(k.Repos, ",") != "acme/alpha" {
			t.Errorf("Kunden %+v", k)
		}
	})

	t.Run("Neuladen", func(t *testing.T) {
		l := b.Neugeladen
		if len(l.Gewaehlt) != 1 || l.Gewaehlt[0] != "acme/alpha" {
			t.Errorf("nach dem Neuladen gewaehlt %v", l.Gewaehlt)
		}
		if n := len(nur(ab["vor-neuladen"], http.MethodGet, "/api/arbeit")); n != 1 {
			t.Errorf("nach dem Neuladen %d Anfragen an /api/arbeit, erwartet 1", n)
		}
	})
}
