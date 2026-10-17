package server_test

// GET /api/html-dateien und GET /api/datei/… gegen die Fake-GitHub-API aus
// repo_api_test.go (html-dateien, #112). Auf main liegen die Commits k2
// (aendert docs/bericht.html) und k1 (legt docs/bericht.html und
// docs/bilder/a.png an, aendert start.htm); der Verlauf von start.htm endet
// mit dem aeltesten Commit v0. Die HTML-Dateien kommen aus dem Baum der
// Spitze (html-dateien-baum, #114); mit hdStand.alt steht dort zusaetzlich
// docs/design/alt.html, zuletzt geaendert in a0, weit vor k1.

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

const hdBericht = `<!DOCTYPE html><html><head><meta charset="utf-8"><title>Bericht</title></head>
<body style="font-family: sans-serif"><h1>Bericht</h1>
<p>Relativ eingebunden:</p><img id="rel" src="bilder/a.png" alt="relativ" width="240" height="150">
<p>Eingebettet:</p><img id="data" alt="eingebettet" width="240" height="150" src="data:image/png;base64,%s">
<p>Vergessen:</p><img src="bilder/weg.png" alt="fehlt">
</body></html>`

// hdFakeGH erweitert die Fake-API um die Commits, Baeume, Blobs und den
// Verlauf der HTML-Dateien. bild ist der Inhalt von docs/bilder/a.png, das
// data:-Bild steht base64 im Bericht.
func hdFakeGH(t *testing.T, bild, dataBild []byte) *fakeGH {
	t.Helper()
	return hdFakeGHStand(t, bild, dataBild, &hdStand{})
}

// hdStand steuert die Fake-API: die Spitze von main (leer = k2), ob der
// Baum docs/design/alt.html traegt und ob er ueberhaupt HTML-Dateien hat.
type hdStand struct {
	mu       sync.Mutex
	spitze   string
	alt      bool
	ohneHTML bool
}

func (s *hdStand) lesen() (spitze string, alt, ohneHTML bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	spitze = s.spitze
	if spitze == "" {
		spitze = "k2"
	}
	return spitze, s.alt, s.ohneHTML
}

func (s *hdStand) setzen(spitze string) {
	s.mu.Lock()
	s.spitze = spitze
	s.mu.Unlock()
}

func hdFakeGHStand(t *testing.T, bild, dataBild []byte, stand *hdStand) *fakeGH {
	t.Helper()
	f := neuesFakeGH(t)
	f.blobs["b-alt"] = `<p>Entwurf</p><img src="../bilder/a.png">`
	f.blobs["b-bericht2"] = fmt.Sprintf(hdBericht, b64(dataBild))
	f.blobs["b-bericht1"] = "<p>alt</p>"
	f.blobs["b-start"] = "<p>ohne Bilder</p>"
	f.blobs["b-a"] = string(bild)
	f.blobs["b-stil"] = "body { color: #333 }"
	kopf := func(sha, zeit, betreff string) string {
		return fmt.Sprintf(`{"sha":%q,"commit":{"message":%q,"committer":{"date":%q}}}`, sha, betreff, zeit)
	}
	einzeln := map[string]string{
		"k2": `{"sha":"k2","commit":{"message":"Bericht nachgezogen","committer":{"date":"2026-09-27T14:05:00Z"}},
		 "files":[{"filename":"docs/bericht.html","sha":"b-bericht2","status":"modified"},{"filename":"main.go","sha":"b-go","status":"modified"}]}`,
		"k1": `{"sha":"k1","commit":{"message":"Bericht angelegt","committer":{"date":"2026-09-26T09:15:00Z"}},
		 "files":[{"filename":"docs/bericht.html","sha":"b-bericht1","status":"added"},{"filename":"docs/bilder/a.png","sha":"b-a","status":"added"},
		          {"filename":"start.htm","sha":"b-start","status":"modified"}]}`,
		"k3": `{"sha":"k3","commit":{"message":"Nur Code","committer":{"date":"2026-09-28T08:00:00Z"}},
		 "files":[{"filename":"main.go","sha":"b-go2","status":"modified"}]}`,
	}
	// Die letzte Aenderung je Pfad ab main (commits?path=&per_page=1).
	letzte := map[string]string{
		"docs/bericht.html":    kopf("k2", "2026-09-27T14:05:00Z", "Bericht nachgezogen"),
		"start.htm":            kopf("k1", "2026-09-26T09:15:00Z", "Bericht angelegt"),
		"docs/design/alt.html": kopf("a0", "2026-03-01T09:00:00Z", "Entwurf G angelegt"),
	}
	baeume := map[string]map[string]string{
		"k2": {"docs/bericht.html": "b-bericht2", "docs/bilder/a.png": "b-a", "docs/stil.css": "b-stil", "start.htm": "b-start"},
		"k1": {"docs/bericht.html": "b-bericht1", "docs/bilder/a.png": "b-a", "start.htm": "b-start"},
		"a0": {"docs/design/alt.html": "b-alt", "docs/bilder/a.png": "b-a"},
	}
	innen := f.srv.Config.Handler
	f.srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if r.Header.Get("Authorization") != "Bearer "+tokWork {
			innen.ServeHTTP(w, r)
			return
		}
		q := r.URL.Query()
		// Jede Anfrage steht im Protokoll, auch die, die hier beantwortet
		// wird (innen protokolliert seine eigenen ein zweites Mal).
		f.mu.Lock()
		f.pfade = append(f.pfade, p)
		f.mu.Unlock()
		spitze, alt, ohneHTML := stand.lesen()
		if p == "/repos/acme/alpha/commits" && q.Get("path") != "" && q.Get("per_page") == "1" {
			f.mu.Lock()
			f.pfade = append(f.pfade, "letzte:"+q.Get("path")+"@"+q.Get("sha"))
			f.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			if k, ok := letzte[q.Get("path")]; ok && q.Get("sha") == "main" {
				fmt.Fprint(w, "["+k+"]")
				return
			}
			fmt.Fprint(w, "[]")
			return
		}
		if p == "/repos/acme/alpha/commits" && q.Get("path") != "" {
			f.mu.Lock()
			f.pfade = append(f.pfade, p+"?path="+q.Get("path"))
			f.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			if q.Get("path") == "docs/design/alt.html" && q.Get("sha") == "a0" {
				fmt.Fprint(w, "["+kopf("a0", "2026-03-01T09:00:00Z", "Entwurf G angelegt")+"]")
				return
			}
			if q.Get("path") == "start.htm" && q.Get("sha") == "k1" {
				fmt.Fprint(w, "["+kopf("k1", "2026-09-26T09:15:00Z", "Bericht angelegt")+","+kopf("v0", "2026-08-01T07:45:00Z", "Start")+"]")
				return
			}
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"message":"Not Found"}`)
			return
		}
		if p == "/repos/acme/alpha/commits" {
			w.Header().Set("Content-Type", "application/json")
			liste := kopf("k2", "2026-09-27T14:05:00Z", "Bericht nachgezogen") + "," + kopf("k1", "2026-09-26T09:15:00Z", "Bericht angelegt")
			if spitze == "k3" {
				liste = kopf("k3", "2026-09-28T08:00:00Z", "Nur Code") + "," + liste
			}
			fmt.Fprint(w, "["+liste+"]")
			return
		}
		if sha, ok := strings.CutPrefix(p, "/repos/acme/alpha/commits/"); ok {
			if body, ok := einzeln[sha]; ok {
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, body)
				return
			}
		}
		if sha, ok := strings.CutPrefix(p, "/repos/acme/alpha/git/trees/"); ok {
			if sha == spitze {
				dateien := map[string]string{}
				for k, v := range baeume["k2"] {
					dateien[k] = v
				}
				if alt {
					dateien["docs/design/alt.html"] = "b-alt"
				}
				if ohneHTML {
					dateien = map[string]string{"README.md": "b-readme", "docs/plan.png": "b-a"}
				}
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, baumJSON(sha, dateien))
				return
			}
			if dateien, ok := baeume[sha]; ok {
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, baumJSON(sha, dateien))
				return
			}
		}
		innen.ServeHTTP(w, r)
	})
	return f
}

type testHTMLDateien struct {
	Repo    string `json:"repo"`
	Grund   string `json:"grund"`
	Dateien []struct {
		Pfad     string `json:"pfad"`
		Name     string `json:"name"`
		Zeit     string `json:"zeit"`
		Commit   string `json:"commit"`
		Betreff  string `json:"betreff"`
		Erstellt string `json:"erstellt"`
		Adresse  string `json:"adresse"`
		Link     string `json:"link"`
		Bilder   struct {
			Gesamt    int      `json:"gesamt"`
			Vorhanden int      `json:"vorhanden"`
			Fehlt     int      `json:"fehlt"`
			Fehlende  []string `json:"fehlende"`
			Text      string   `json:"text"`
		} `json:"bilder"`
	} `json:"dateien"`
}

func TestApiHTMLDateien(t *testing.T) {
	f := hdFakeGH(t, []byte("\x89PNG-a"), []byte("\x89PNG-d"))
	h, _, _ := repoHandler(t, f)

	var g testHTMLDateien
	rec := holeJSON(t, h, "/api/html-dateien?repo=acme/alpha", &g)
	if rec.Code != http.StatusOK {
		t.Fatalf("Status %d: %s", rec.Code, rec.Body.String())
	}
	if g.Repo != "acme/alpha" || g.Grund != "" {
		t.Errorf("repo %q grund %q", g.Repo, g.Grund)
	}
	if len(g.Dateien) != 2 || g.Dateien[0].Pfad != "docs/bericht.html" || g.Dateien[1].Pfad != "start.htm" {
		t.Fatalf("Dateien %+v, erwartet docs/bericht.html, start.htm", g.Dateien)
	}
	b := g.Dateien[0]
	if b.Name != "bericht.html" || b.Commit != "k2" || b.Zeit != "2026-09-27T14:05:00Z" || b.Betreff != "Bericht nachgezogen" {
		t.Errorf("bericht.html %+v", b)
	}
	if b.Erstellt != "2026-09-26T09:15:00Z" {
		t.Errorf("bericht.html erstellt %q, erwartet die Zeit von k1", b.Erstellt)
	}
	if b.Adresse != "/api/datei/acme/alpha/k2/docs/bericht.html" || b.Link != "https://github.com/acme/alpha/blob/k2/docs/bericht.html" {
		t.Errorf("Adresse %q, Link %q", b.Adresse, b.Link)
	}
	if b.Bilder.Text != "3 Bilder · 2 vorhanden · 1 fehlt" || len(b.Bilder.Fehlende) != 1 || b.Bilder.Fehlende[0] != "docs/bilder/weg.png" {
		t.Errorf("Bilder %+v", b.Bilder)
	}
	s := g.Dateien[1]
	if s.Erstellt != "2026-08-01T07:45:00Z" || s.Bilder.Text != "keine Bilder" || s.Commit != "k1" {
		t.Errorf("start.htm %+v, erwartet erstellt von v0 und keine Bilder", s)
	}
	if n := f.anfragen("/repos/acme/alpha/commits?path=docs/bericht.html"); n != 0 {
		t.Errorf("Verlauf von docs/bericht.html %d-mal gefragt, erwartet 0 (added liegt vor)", n)
	}
	if n := f.anfragen("/repos/acme/alpha/git/trees/k2"); n == 0 {
		t.Errorf("Baum der Spitze k2 nie gefragt")
	}

	if rec := getRaw(t, h, "/api/html-dateien?repo=acme/unbekannt"); rec.Code != http.StatusNotFound {
		t.Errorf("unbekanntes Repo: Status %d, erwartet 404", rec.Code)
	}
}

// Pflichttest aus #114: eine Datei, deren letzte Aenderung ausserhalb der
// juengsten Commits liegt, erscheint; der zweite Abruf ohne neuen Commit
// fragt nichts je Pfad.
func TestApiHTMLDateienAlteDateiAusDemBaum(t *testing.T) {
	stand := &hdStand{alt: true}
	f := hdFakeGHStand(t, []byte("\x89PNG-a"), []byte("\x89PNG-d"), stand)
	h, _, _ := repoHandler(t, f)

	pfade := []string{"docs/bericht.html", "start.htm", "docs/design/alt.html"}
	for lauf := 1; lauf <= 2; lauf++ {
		var g testHTMLDateien
		rec := holeJSON(t, h, "/api/html-dateien?repo=acme/alpha", &g)
		if rec.Code != http.StatusOK || g.Grund != "" {
			t.Fatalf("Lauf %d: Status %d, Grund %q: %s", lauf, rec.Code, g.Grund, rec.Body.String())
		}
		var erhalten []string
		for _, d := range g.Dateien {
			erhalten = append(erhalten, d.Pfad)
		}
		if strings.Join(erhalten, ",") != strings.Join(pfade, ",") {
			t.Fatalf("Lauf %d: Dateien %v, erwartet %v", lauf, erhalten, pfade)
		}
		a := g.Dateien[2]
		if a.Commit != "a0" || a.Zeit != "2026-03-01T09:00:00Z" || a.Betreff != "Entwurf G angelegt" || a.Adresse != "/api/datei/acme/alpha/a0/docs/design/alt.html" {
			t.Errorf("Lauf %d: docs/design/alt.html %+v, erwartet aus a0", lauf, a)
		}
		if a.Erstellt != "2026-03-01T09:00:00Z" || a.Bilder.Text != "1 Bild · alle vorhanden" {
			t.Errorf("Lauf %d: docs/design/alt.html erstellt %q, Bilder %q", lauf, a.Erstellt, a.Bilder.Text)
		}
	}
	for _, p := range pfade {
		if n := f.anfragen("letzte:" + p + "@main"); n != 1 {
			t.Errorf("letzte Aenderung von %s %d-mal mit sha=main und per_page=1 gefragt, erwartet genau 1 ueber zwei Abrufe", p, n)
		}
	}
	if n := f.anfragen("letzte:"); n != len(pfade) {
		t.Errorf("%d Fragen nach der letzten Aenderung, erwartet %d", n, len(pfade))
	}

	// Neuer Commit an der Spitze: Baum und letzte Aenderung je Pfad neu.
	stand.setzen("k3")
	var g testHTMLDateien
	if rec := holeJSON(t, h, "/api/html-dateien?repo=acme/alpha", &g); rec.Code != http.StatusOK || len(g.Dateien) != 3 {
		t.Fatalf("nach k3: Status %d, %d Dateien: %s", rec.Code, len(g.Dateien), rec.Body.String())
	}
	if n := f.anfragen("/repos/acme/alpha/git/trees/k3"); n == 0 {
		t.Errorf("Baum der neuen Spitze k3 nicht gefragt")
	}
	for _, p := range pfade {
		if n := f.anfragen("letzte:" + p + "@main"); n != 2 {
			t.Errorf("nach k3: letzte Aenderung von %s %d-mal gefragt, erwartet 2", p, n)
		}
	}
}

func TestApiHTMLDateienBaumOhneHTML(t *testing.T) {
	f := hdFakeGHStand(t, []byte("\x89PNG-a"), []byte("\x89PNG-d"), &hdStand{ohneHTML: true})
	h, _, _ := repoHandler(t, f)
	var g testHTMLDateien
	rec := holeJSON(t, h, "/api/html-dateien?repo=acme/alpha", &g)
	if rec.Code != http.StatusOK || g.Grund != "" || !strings.Contains(rec.Body.String(), `"dateien":[]`) {
		t.Errorf("erwartet 200, leere Liste ohne Grund: %d %s", rec.Code, rec.Body.String())
	}
	if n := f.anfragen("letzte:"); n != 0 {
		t.Errorf("%d Fragen nach der letzten Aenderung ohne HTML-Datei", n)
	}
}

func TestApiHTMLDateienFehler(t *testing.T) {
	f := neuesFakeGH(t)
	h, _, _ := repoHandler(t, f)
	var g testHTMLDateien
	rec := holeJSON(t, h, "/api/html-dateien?repo=acme/alpha", &g)
	if rec.Code != http.StatusOK || g.Grund == "" || !strings.Contains(rec.Body.String(), `"dateien":[]`) {
		t.Errorf("erwartet 200, leere Liste und Grund: %d %s", rec.Code, rec.Body.String())
	}
}

func pruefeSchutz(t *testing.T, was string, rec *httptest.ResponseRecorder) {
	t.Helper()
	csp := rec.Header().Get("Content-Security-Policy")
	if csp != "sandbox" || strings.Contains(csp, "allow-") {
		t.Errorf("%s: CSP %q, erwartet genau sandbox", was, csp)
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("%s: X-Content-Type-Options %q", was, rec.Header().Get("X-Content-Type-Options"))
	}
}

func TestApiDatei(t *testing.T) {
	f := hdFakeGH(t, []byte("\x89PNG-a"), []byte("\x89PNG-d"))
	h, _, _ := repoHandler(t, f)

	rec := getRaw(t, h, "/api/datei/acme/alpha/k2/docs/bericht.html")
	if rec.Code != http.StatusOK {
		t.Fatalf("HTML: Status %d: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Errorf("HTML: Content-Type %q", ct)
	}
	if rec.Body.String() != f.blobs["b-bericht2"] {
		t.Errorf("HTML: Inhalt %q", rec.Body.String())
	}
	pruefeSchutz(t, "HTML", rec)

	rec = getRaw(t, h, "/api/datei/acme/alpha/k2/docs/bilder/a.png")
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/png" || rec.Body.String() != "\x89PNG-a" {
		t.Errorf("Bild: Status %d, Content-Type %q, Inhalt %q", rec.Code, rec.Header().Get("Content-Type"), rec.Body.String())
	}
	pruefeSchutz(t, "Bild", rec)

	rec = getRaw(t, h, "/api/datei/acme/alpha/k2/docs/stil.css")
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "text/css; charset=utf-8" {
		t.Errorf("CSS: Status %d, Content-Type %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	pruefeSchutz(t, "CSS", rec)

	if rec := getRaw(t, h, "/api/datei/acme/alpha/k2/docs/nicht.html"); rec.Code != http.StatusNotFound {
		t.Errorf("fehlende Datei: Status %d, erwartet 404", rec.Code)
	}
}

func TestApiDateiAusbruch(t *testing.T) {
	f := hdFakeGH(t, []byte("\x89PNG-a"), []byte("\x89PNG-d"))
	h, _, _ := repoHandler(t, f)
	// Die Leiste einmal laden, damit ihr Abruf nicht mitzaehlt.
	if rec := getRaw(t, h, "/api/repos"); rec.Code != http.StatusOK {
		t.Fatalf("Leiste: %d", rec.Code)
	}
	vorher := f.anfragen("/repos/")
	for _, pfad := range []string{
		"/api/datei/acme/alpha/k2/../../etc/passwd",
		"/api/datei/acme/alpha/k2/docs/%2e%2e/x.html",
		"/api/datei/acme/alpha/k2/docs/./bericht.html",
		"/api/datei/acme/alpha/k2//etc/passwd",
		"/api/datei/acme/alpha/k2/",
		"/api/datei/acme/alpha",
		"/api/datei/fremd/repo/k2/a.html",
		"/api/datei/acme/alpha/k%2F2/a.html",
	} {
		rec := getRaw(t, h, pfad)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: Status %d, erwartet 404", pfad, rec.Code)
		}
	}
	if n := f.anfragen("/repos/") - vorher; n != 0 {
		t.Errorf("%d Anfragen an GitHub fuer abgelehnte Pfade, erwartet 0", n)
	}
	if n := f.anfragen("/repos/fremd/"); n != 0 {
		t.Errorf("fremdes Repo %d-mal bei GitHub gefragt", n)
	}

	// Ueber Loopback, sonst haelt schon das Netz-Gate den POST an.
	if resp, _ := getAPI(t, h, http.MethodPost, "/api/datei/acme/alpha/k2/docs/bericht.html"); resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("POST: Status %d, erwartet 405", resp.StatusCode)
	}
}

func b64(b []byte) string { return base64.StdEncoding.EncodeToString(b) }
