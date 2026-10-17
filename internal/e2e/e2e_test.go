package e2e_test

// Vollstaendiger Funktionstest (sync-status, #38): der echte Handler
// (server.NewHandler) gegen die Fake-GitHub-API aus fakegithub_test.go,
// bedient in headless Chromium. Die Testseite laedt das echte Bundle und einen
// Treiber, der nacheinander alle Funktionen der Oberflaeche benutzt; danach
// prueft der Test den Zustand der Fake-API und die areas.json.
//
// Fehlt Chromium, ueberspringt sich der Test — dann ist die Oberflaeche NICHT
// belegt.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/niclasedge/git-planner-go/internal/assets"
	"github.com/niclasedge/git-planner-go/internal/ghrest"
	"github.com/niclasedge/git-planner-go/internal/render"
	"github.com/niclasedge/git-planner-go/internal/server"
)

func chromiumFinden() string {
	ausfuehrbar := func(p string) bool {
		fi, err := os.Stat(p)
		return err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0
	}
	if p := os.Getenv("GKP_CHROMIUM"); p != "" && ausfuehrbar(p) {
		return p
	}
	for _, name := range []string{"chromium", "chromium-browser", "google-chrome", "headless_shell"} {
		if p, err := exec.LookPath(name); err == nil && ausfuehrbar(p) {
			return p
		}
	}
	var wurzeln []string
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

const seite = `<!DOCTYPE html>
<html lang="de"><head><meta charset="utf-8"><title>e2e</title></head>
<body><div id="kn-svelte-root"></div>
<script src="/assets/app/app.js"></script>
<script src="/__treiber.js"></script>
</body></html>`

// Der Treiber. Jeder Schritt wartet auf ein sichtbares Ergebnis; schlaegt einer
// fehl, bricht der Lauf mit dem Namen des Schritts ab.
const treiber = `
(function () {
  var fehler = [];
  window.addEventListener('error', function (e) { fehler.push(String(e.message)); });
  window.addEventListener('unhandledrejection', function (e) { fehler.push('rejection: ' + String(e.reason)); });
  function schlaf(ms) { return new Promise(function (r) { setTimeout(r, ms); }); }
  async function warte(was, pred) {
    for (var i = 0; i < 300; i++) { try { if (pred()) return; } catch (e) {} await schlaf(25); }
    throw new Error('Zeitueberschreitung: ' + was);
  }
  function q(sel) { return document.querySelector(sel); }
  function alle(sel) { return Array.prototype.slice.call(document.querySelectorAll(sel)); }
  function tippe(el, wert) { el.value = wert; el.dispatchEvent(new Event('input', { bubbles: true })); }
  function klick(sel) { var e = q(sel); if (!e) throw new Error('fehlt: ' + sel); e.click(); }
  function laden() { try { return JSON.parse(sessionStorage.getItem('kn-e2e') || '{}'); } catch (e) { return {}; } }
  function sichern(b) { sessionStorage.setItem('kn-e2e', JSON.stringify(b)); }
  function berichten(b) {
    b.fehler = (b.fehler || []).concat(fehler);
    var pre = document.createElement('pre');
    pre.id = 'kn-testbericht';
    pre.textContent = btoa(unescape(encodeURIComponent(JSON.stringify(b))));
    document.body.appendChild(pre);
  }
  function tag(d) { var t = new Date(); t.setDate(t.getDate() + d); return t.getFullYear() + '-' + String(t.getMonth() + 1).padStart(2, '0') + '-' + String(t.getDate()).padStart(2, '0'); }
  // Die Abhaengigen eines Blockers beginnen eingeklappt (aktionen-zeitleiste,
  // #128): aufklappen, bis #nr in der Liste steht.
  async function aufklappen(nr) {
    for (var i = 0; i < 5 && !q('[data-eintrag="' + nr + '"]'); i++) {
      alle('[data-abhaengige][aria-expanded="false"]').forEach(function (k) { k.click(); });
      await schlaf(100);
    }
  }
  async function schritt(b, name, f) { b.schritt = name; await f(); b.erledigt = (b.erledigt || []).concat([name]); }

  async function lauf() {
    var b = laden();
    try {
      if (!b.phase) {
        await schritt(b, 'leiste', async function () {
          await warte('drei Repos', function () { return q('[data-repo="acme/alpha"]') && q('[data-repo="acme/beta"]') && q('[data-repo="ich/gamma"]'); });
          await warte('Sync-Anzeige', function () { return q('[data-sync]'); });
          b.syncText = q('.kn-sync-anzeige').textContent.trim();
        });
        await schritt(b, 'repo waehlen', async function () {
          klick('[data-repo="acme/alpha"]');
          await warte('Liste', function () { return q('[data-eintrag="4"]'); });
          await aufklappen('2');
          await warte('Liste', function () { return q('[data-eintrag="4"]') && q('[data-eintrag="2"]'); });
          b.statusZwei = q('[data-eintrag="2"]').getAttribute('data-status');
        });
        await schritt(b, 'kategorie', async function () {
          klick('.kn-einstellungen-knopf');
          await warte('Settings', function () { return q('.kn-neue-kategorie input'); });
          await warte('Sync-Abschnitt', function () { return alle('[data-sync-quelle]').length > 0; });
          b.syncQuellen = alle('[data-sync-quelle]').map(function (e) { return e.getAttribute('data-sync-quelle'); });
          tippe(q('.kn-neue-kategorie input'), 'Kunden');
          await schlaf(100);
          klick('.kn-neue-kategorie button');
          await warte('Kunden', function () { return q('[data-kategorie="Kunden"] [data-repo="acme/alpha"]'); });
          klick('.kn-s-zu');
        });
        await schritt(b, 'einklappen', async function () {
          klick('[data-kategorie="Ohne Bereich"] .kn-k-kopf');
          await warte('eingeklappt', function () { return !q('[data-repo="acme/beta"]'); });
        });
        await schritt(b, 'aufklappen mit beleg', async function () {
          q('[data-eintrag="4"] .kn-e-titel').click();
          await warte('Subtasks', function () { return alle('.kn-e-karte [data-subtask]').length === 2; });
          await warte('Belegbild geladen', function () { var i = q('.kn-e-karte img'); return i && i.complete && i.naturalWidth > 0; });
          b.belegSymbol = q('[data-eintrag="4"] [data-beleg]').getAttribute('data-beleg');
        });
        await schritt(b, 'detail change', async function () {
          klick('[data-eintrag="4"] .kn-e-details');
          await warte('Vorschlag', function () { var d = q('.kn-detail-inhalt'); return d && d.textContent.indexOf('Der Vorschlag fuer Vier') >= 0; });
        });
        await schritt(b, 'detail issue und kommentar', async function () {
          alle('.kn-d-reiter button').filter(function (x) { return x.textContent.trim() === 'Issue'; })[0].click();
          await warte('Kommentar', function () { return q('.kn-kommentar'); });
          tippe(q('.kn-kommentieren textarea'), 'Hallo e2e');
          await schlaf(100);
          klick('.kn-kommentieren button');
          await warte('neuer Kommentar', function () { return alle('.kn-kommentar').some(function (k) { return k.textContent.indexOf('Hallo e2e') >= 0; }); });
        });
        await schritt(b, 'label', async function () {
          klick('.kn-label-wahl [data-label="ui"]');
          await warte('Label gesetzt', function () { var l = q('.kn-label-wahl [data-label="ui"]'); return l && l.getAttribute('aria-pressed') === 'true'; });
        });
        await schritt(b, 'blocker', async function () {
          tippe(q('.kn-ib-inline input'), '#3');
          await schlaf(100);
          klick('.kn-ib-inline button');
          await warte('Blocker', function () { return alle('.kn-ib-chip').some(function (c) { return c.textContent.indexOf('#3') >= 0; }); });
          klick('.kn-d-zu');
        });
        await schritt(b, 'schliessen per checkbox', async function () {
          await aufklappen('3');
          klick('[data-eintrag="3"] .kn-e-check');
          await warte('#3 erledigt', function () { var c = q('[data-eintrag="3"] .kn-e-check'); return c && c.getAttribute('aria-checked') === 'true'; });
        });
        await schritt(b, 'neues issue', async function () {
          klick('.kn-neues-issue-knopf');
          await warte('Formular', function () { return q('.kn-neues-issue input[name=titel]'); });
          tippe(q('.kn-neues-issue input[name=titel]'), 'E2E neu');
          await schlaf(100);
          klick('.kn-neues-issue button[type=submit]');
          await warte('neues Issue in der Liste', function () { return alle('.kn-e-titel').some(function (t) { return t.textContent.indexOf('E2E neu') >= 0; }); });
        });
        await schritt(b, 'karte', async function () {
          klick('.kn-karte-kopf');
          await warte('Linien', function () { return q('[data-linie-titel="1"]') && q('[data-linie-titel="8"]'); });
          b.kanten = alle('[data-kante]').map(function (k) { return k.getAttribute('data-kante') + ':' + k.getAttribute('data-art'); });
          b.linien = alle('[data-linie]').map(function (k) { return k.getAttribute('data-linie'); });
          b.stationen = alle('[data-station]').map(function (k) { return k.getAttribute('data-zone') + ':' + k.getAttribute('data-station'); });
          q('[data-linie-titel="1"]').dispatchEvent(new MouseEvent('click', { bubbles: true }));
          await warte('Map im Detail', function () { var d = q('.kn-detail h2'); return d && d.textContent.indexOf('Karte bauen') >= 0; });
          klick('.kn-d-zu');
        });
        await schritt(b, 'pr freigeben', async function () {
          q('[data-eintrag="pr-20"] .kn-e-titel').click();
          await warte('Freigeben', function () { return q('[data-eintrag="pr-20"] .kn-pr-freigeben'); });
          klick('[data-eintrag="pr-20"] .kn-pr-freigeben');
          await warte('kein Entwurf mehr', function () { var z = q('[data-eintrag="pr-20"]'); return z && z.textContent.indexOf('Entwurf') < 0 && z.querySelector('.kn-pr-mergen'); });
        });
        await schritt(b, 'pr mergen', async function () {
          q('[data-eintrag="pr-21"] .kn-e-titel').click();
          await warte('Mergen', function () { return q('[data-eintrag="pr-21"] .kn-pr-mergen'); });
          klick('[data-eintrag="pr-21"] .kn-pr-mergen');
          await schlaf(100);
          klick('[data-eintrag="pr-21"] .kn-pr-mergen');
          await warte('PR weg', function () { return !q('[data-eintrag="pr-21"]'); });
        });
        await schritt(b, 'sync klick', async function () {
          klick('.kn-sync-anzeige');
          await warte('Sync fertig', function () { var s = q('[data-sync]'); return s && s.getAttribute('data-sync') === 'ok'; });
        });
        await schritt(b, 'neues issue mit datum', async function () {
          klick('.kn-neues-issue-knopf');
          await warte('Datumsfeld', function () { return q('.kn-neues-issue input[name=faellig]'); });
          tippe(q('.kn-neues-issue input[name=titel]'), 'E2E faellig');
          tippe(q('.kn-neues-issue input[name=faellig]'), tag(0));
          await schlaf(100);
          klick('.kn-neues-issue button[type=submit]');
          await warte('in der Liste', function () { return alle('.kn-e-titel').some(function (t) { return t.textContent.indexOf('E2E faellig') >= 0; }); });
        });
        await schritt(b, 'heute', async function () {
          klick('[data-ansicht="heute"]');
          await warte('in Heute', function () { return alle('[data-abschnitt="heute"] [data-u-eintrag]').some(function (z) { return z.textContent.indexOf('E2E faellig') >= 0; }); });
          b.heuteZahl = q('[data-ansicht="heute"] .kn-ansicht-zahl').textContent.trim();
          klick('[data-repo="acme/alpha"]');
          await warte('Liste', function () { return q('[data-eintrag="4"]'); });
        });
        await schritt(b, 'datum morgen', async function () {
          klick('[data-eintrag="4"] .kn-e-details');
          await warte('Reiter', function () { return alle('.kn-d-reiter button').length > 0; });
          alle('.kn-d-reiter button').filter(function (x) { return x.textContent.trim() === 'Issue'; })[0].click();
          await warte('Morgen', function () { return q('.kn-faellig-morgen'); });
          klick('.kn-faellig-morgen');
          await warte('Datum an #4', function () { return q('[data-eintrag="4"] .kn-faellig'); });
          klick('.kn-d-zu');
        });
        b.phase = 'neuladen';
        b.fehler = fehler.slice();
        sichern(b);
        location.reload();
        return;
      }
      await schritt(b, 'nach neuladen', async function () {
        await warte('Repo gewaehlt', function () { return q('[data-repo="acme/alpha"][aria-current="true"]') && q('[data-eintrag="4"]'); });
        b.ohneEingeklappt = q('[data-kategorie="Ohne Bereich"]').getAttribute('data-eingeklappt');
        await warte('Karte offen', function () { return q('[data-linie-titel="1"]'); });
      });
      b.fertig = true;
    } catch (e) {
      b.abbruch = String(e && e.message || e);
      b.dom = document.body.innerHTML.slice(0, 4000);
    }
    berichten(b);
  }
  lauf();
})();
`

var berichtRe = regexp.MustCompile(`<pre id="kn-testbericht">([A-Za-z0-9+/=]+)</pre>`)

func TestGanzerDurchlauf(t *testing.T) {
	if testing.Short() {
		t.Skip("-short: der Test startet einen Browser")
	}
	chrome := chromiumFinden()
	if chrome == "" {
		t.Skip("kein Chromium gefunden — die Oberflaeche ist damit NICHT belegt")
	}
	ghrest.SpeicherLeeren()
	gh := neueFakeGitHub()
	defer gh.srv.Close()

	areaDatei := filepath.Join(t.TempDir(), "areas.json")
	t.Setenv("CC_AREAS_FILE", areaDatei)
	t.Setenv("CC_KNOWLEDGE_ROOTS", t.TempDir())
	t.Setenv("CC_KNOWLEDGE_CACHE", filepath.Join(t.TempDir(), "cache.json"))
	t.Setenv("CC_REPORTS_DIR", t.TempDir())
	t.Setenv("GKP_GITHUB_API", gh.srv.URL)
	t.Setenv("GITHUB_PAT_WORK", tokWork)
	t.Setenv("GITHUB_PAT_PERSONAL", tokPrivat)
	server.RepoLog = &strings.Builder{}

	echt := server.NewHandler(server.AppOptions{
		Cwd:     t.TempDir(),
		Assets:  render.Assets{DashCSS: assets.DashCSS, BrowserCSS: assets.BrowserCSS, BrowserJS: assets.BrowserJS},
		AssetFS: assets.FS(),
	})
	mux := http.NewServeMux()
	mux.HandleFunc("/__treiber.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		fmt.Fprint(w, treiber)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, seite)
			return
		}
		echt.ServeHTTP(w, r)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ctx, abbrechen := context.WithTimeout(context.Background(), 120*time.Second)
	defer abbrechen()
	out, err := exec.CommandContext(ctx, chrome,
		"--headless", "--no-sandbox", "--disable-gpu", "--no-first-run",
		"--disable-extensions", "--disable-dev-shm-usage",
		"--user-data-dir="+t.TempDir(),
		"--virtual-time-budget=60000",
		"--dump-dom", srv.URL+"/").Output()
	if err != nil {
		t.Fatalf("chromium (%s): %v\n%s", chrome, err, out)
	}
	m := berichtRe.FindSubmatch(out)
	if m == nil {
		t.Fatalf("kein Testbericht in der Seite. DOM:\n%s", out)
	}
	roh, _ := base64.StdEncoding.DecodeString(string(m[1]))
	var b struct {
		Abbruch, DOM, Schritt     string
		Erledigt                  []string
		Fehler                    []string
		Fertig                    bool
		SyncText                  string
		StatusZwei                string
		SyncQuellen               []string
		BelegSymbol               string
		Kanten, Linien, Stationen []string
		OhneEingeklappt           string
		HeuteZahl                 string
	}
	if err := json.Unmarshal(roh, &b); err != nil {
		t.Fatalf("Bericht kein JSON: %v", err)
	}
	if b.Abbruch != "" || !b.Fertig {
		t.Fatalf("Durchlauf abgebrochen in Schritt %q: %s\nerledigt: %v\nJS-Fehler: %v\nDOM: %s", b.Schritt, b.Abbruch, b.Erledigt, b.Fehler, b.DOM)
	}
	if len(b.Fehler) > 0 {
		t.Errorf("JavaScript-Fehler: %v", b.Fehler)
	}

	// Was die Oberflaeche zeigte.
	if !strings.Contains(b.SyncText, "Sync") {
		t.Errorf("Sync-Anzeige %q", b.SyncText)
	}
	if b.StatusZwei != "blockiert" {
		t.Errorf("#2 Status %q, erwartet blockiert", b.StatusZwei)
	}
	if len(b.SyncQuellen) == 0 {
		t.Error("Settings ohne Sync-Quellen")
	}
	if b.BelegSymbol != "ja" {
		t.Errorf("Beleg-Symbol %q", b.BelegSymbol)
	}
	// Seit repo-karte-folge (#99) ist die Karte eine Folge: Map #1 und die
	// geschlossene Map #8 sind Linien, keine Stationen; erledigt sind #9, #5
	// und das im Durchlauf geschlossene #3 (aeltestes links), #4 laeuft (Change
	// vier), dann die Reihe. #3 ist zu, #9 war es schon: keine offenen
	// Blocker, also keine Kanten.
	stationen := strings.Join(b.Stationen, ",")
	if strings.Join(b.Linien, ",") != "1,8,0" || !strings.HasPrefix(stationen, "erledigt:9,erledigt:5,erledigt:3,jetzt:4,naechste:2,naechste:6") || len(b.Kanten) != 0 {
		t.Errorf("Karte: Linien %v, Stationen %v, Kanten %v; erwartet 1,8,0, erledigt 9,5,3, jetzt 4, naechste 2,6,… und keine Kanten", b.Linien, b.Stationen, b.Kanten)
	}
	if b.OhneEingeklappt != "true" {
		t.Errorf("Ohne Bereich nach dem Neuladen eingeklappt=%q", b.OhneEingeklappt)
	}

	// Was bei GitHub angekommen ist.
	gh.mu.Lock()
	defer gh.mu.Unlock()
	vier := gh.issues[4]
	if len(vier.Kommentare) != 2 || !strings.Contains(vier.Kommentare[1], "Hallo e2e") {
		t.Errorf("Kommentare #4 %v", vier.Kommentare)
	}
	if strings.Join(vier.Labels, ",") != "bug,ui" {
		t.Errorf("Labels #4 %v", vier.Labels)
	}
	if fmt.Sprint(vier.Blocker) != "[3]" {
		t.Errorf("Blocker #4 %v", vier.Blocker)
	}
	if !gh.issues[3].Zu {
		t.Error("#3 wurde nicht geschlossen")
	}
	neu := false
	for _, i := range gh.issues {
		if i.Titel == "E2E neu" {
			neu = true
		}
	}
	if !neu {
		t.Error("neues Issue fehlt bei GitHub")
	}
	if fmt.Sprint(gh.freigegeben) != "[20]" || gh.prs[20].Draft {
		t.Errorf("Freigabe %v", gh.freigegeben)
	}
	if !gh.prs[21].Gemergt || fmt.Sprint(gh.geloescht) != "[change/einundzwanzig]" {
		t.Errorf("Merge %v, geloescht %v", gh.prs[21].Gemergt, gh.geloescht)
	}
	heute, morgen := time.Now().Format("2006-01-02"), time.Now().AddDate(0, 0, 1).Format("2006-01-02")
	mitDatum := false
	for _, i := range gh.issues {
		if i.Titel == "E2E faellig" && strings.HasSuffix(i.Text, "Fällig: "+heute) {
			mitDatum = true
		}
	}
	if !mitDatum {
		t.Errorf("neues Issue ohne Faellig-Zeile %s", heute)
	}
	if !strings.HasSuffix(vier.Text, "\n\nFällig: "+morgen) || !strings.HasPrefix(vier.Text, "Vier **bauen**") {
		t.Errorf("Text #4 %q, erwartet Faellig-Zeile %s am Ende", vier.Text, morgen)
	}
	if b.HeuteZahl == "" || b.HeuteZahl == "0" {
		t.Errorf("Zaehler an Heute %q", b.HeuteZahl)
	}
	if gh.unveraendert == 0 {
		t.Error("kein einziger bedingter Abruf wurde mit 304 beantwortet")
	}
	if gh.aufrufe["GET /user/repos"] < 3 {
		t.Errorf("/user/repos %d-mal angefragt, erwartet auch den frischen Abruf nach dem Sync-Klick", gh.aufrufe["GET /user/repos"])
	}

	// Was in areas.json steht.
	inhalt, _ := os.ReadFile(areaDatei)
	var st struct {
		Areas       []string          `json:"areas"`
		Zuordnung   map[string]string `json:"zuordnung"`
		Eingeklappt []string          `json:"eingeklappt"`
	}
	_ = json.Unmarshal(inhalt, &st)
	if st.Zuordnung["acme/alpha"] != "Kunden" || fmt.Sprint(st.Eingeklappt) != "[Ohne Bereich]" {
		t.Errorf("areas.json %s", inhalt)
	}
}
