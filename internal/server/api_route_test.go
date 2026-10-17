package server_test

// Die Routen GET /api/leiste und GET /api/aufgaben (projektliste-api, #11)
// gegen server.NewHandler: JSON-Form, 404 mit Grund, lesend ueber das LAN,
// POST abgelehnt. Ohne bd und ohne Netz: die Quellen sind Test-Quellen, die
// Projekte liegen unter einem eigenen CC_KNOWLEDGE_ROOTS.

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/niclasedge/git-planner-go/internal/appctx"
	"github.com/niclasedge/git-planner-go/internal/assets"
	"github.com/niclasedge/git-planner-go/internal/cache"
	"github.com/niclasedge/git-planner-go/internal/render"
	"github.com/niclasedge/git-planner-go/internal/server"
)

type apiQuelle struct {
	herkunft string
	faeh     appctx.Faehigkeiten
	maps     map[string][]appctx.ReportsMap
	grund    string
}

func (q apiQuelle) Herkunft() string                  { return q.herkunft }
func (q apiQuelle) Faehigkeiten() appctx.Faehigkeiten { return q.faeh }
func (q apiQuelle) Karten(pfad string) ([]appctx.ReportsMap, error) {
	if q.grund != "" {
		return nil, &apiFehler{q.grund}
	}
	return q.maps[pfad], nil
}

type apiFehler struct{ s string }

func (e *apiFehler) Error() string { return e.s }

// apiUmgebung legt die Projekte alpha und beta unter einer eigenen Wurzel an,
// eine Area-Datei, und ersetzt die Quellen fuer die Dauer des Tests.
func apiUmgebung(t *testing.T) (http.Handler, string) {
	t.Helper()
	wurzel := t.TempDir()
	for _, n := range []string{"alpha", "beta"} {
		if err := os.MkdirAll(filepath.Join(wurzel, n, ".beads"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("CC_KNOWLEDGE_ROOTS", wurzel)
	areasDatei := filepath.Join(t.TempDir(), "areas.json")
	if err := os.WriteFile(areasDatei, []byte(`{"version":1,"areas":["Arbeit"],"zuordnung":{"alpha":"Arbeit"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CC_AREAS_FILE", areasDatei)
	t.Setenv("CC_REPORTS_DIR", t.TempDir())

	alpha := filepath.Join(wurzel, "alpha")
	zurueck := appctx.QuellenFuerTest(
		apiQuelle{herkunft: "beads", faeh: appctx.Faehigkeiten{Schreibbar: true}, maps: map[string][]appctx.ReportsMap{
			alpha: {{ID: "k1", Title: "Karte", Status: "open", Beads: []appctx.ReportsBead{
				{ID: "k1.1", Title: "Kind", Status: "open", Children: []appctx.ReportsBead{{ID: "k1.1.1", Status: "open"}}},
				{ID: "k1.2", Status: "closed"},
			}}},
		}},
		apiQuelle{herkunft: "github", grund: "kein Token"},
	)
	t.Cleanup(zurueck)
	cache.ResetLeisteForTest()
	t.Cleanup(cache.ResetLeisteForTest)

	h := server.NewHandler(server.AppOptions{
		Cwd:     t.TempDir(),
		Assets:  render.Assets{DashCSS: assets.DashCSS, BrowserCSS: assets.BrowserCSS, BrowserJS: assets.BrowserJS},
		AssetFS: assets.FS(),
	})
	return h, alpha
}

func TestAPIAufgabenWurzelnUndKinder(t *testing.T) {
	h, alpha := apiUmgebung(t)

	rec, body := getJSON(t, h, "/api/aufgaben?project="+url.QueryEscape(alpha))
	if rec.Code != http.StatusOK {
		t.Fatalf("Status %d: %v", rec.Code, body)
	}
	if body["projekt"] != alpha || body["eltern"] != "" {
		t.Errorf("projekt/eltern: %v", body)
	}
	eintraege, _ := body["eintraege"].([]any)
	if len(eintraege) != 1 {
		t.Fatalf("eintraege %v", body["eintraege"])
	}
	k1 := eintraege[0].(map[string]any)
	if k1["id"] != "k1" || k1["hat_kinder"] != true || k1["kinder_anzahl"] != float64(1) {
		t.Errorf("k1: %v", k1)
	}
	quellen, _ := body["quellen"].([]any)
	gh := false
	for _, q := range quellen {
		m := q.(map[string]any)
		if m["herkunft"] == "github" && m["grund"] == "kein Token" {
			gh = true
		}
	}
	if !gh {
		t.Errorf("quellen ohne github/kein Token: %v", body["quellen"])
	}

	rec, body = getJSON(t, h, "/api/aufgaben?project="+url.QueryEscape(alpha)+"&eltern=k1")
	if rec.Code != http.StatusOK {
		t.Fatalf("Status %d: %v", rec.Code, body)
	}
	if body["eltern"] != "k1" {
		t.Errorf("eltern %v", body["eltern"])
	}
	eintraege, _ = body["eintraege"].([]any)
	if len(eintraege) != 1 || eintraege[0].(map[string]any)["id"] != "k1.1" {
		t.Errorf("Kinder von k1: %v", body["eintraege"])
	}
}

func TestAPIAufgabenUnbekanntGibt404MitGrund(t *testing.T) {
	h, alpha := apiUmgebung(t)

	for _, pfad := range []string{
		"/api/aufgaben?project=gibtsnicht",
		"/api/aufgaben?project=" + url.QueryEscape(alpha) + "&eltern=x-999",
	} {
		rec, body := getJSON(t, h, pfad)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: Status %d, erwartet 404", pfad, rec.Code)
		}
		if g, _ := body["grund"].(string); strings.TrimSpace(g) == "" {
			t.Errorf("%s: kein Grund im Rumpf: %v", pfad, body)
		}
	}
}

func TestAPILeisteForm(t *testing.T) {
	h, alpha := apiUmgebung(t)

	rec, body := getJSON(t, h, "/api/leiste")
	if rec.Code != http.StatusOK {
		t.Fatalf("Status %d: %v", rec.Code, body)
	}
	if s, _ := body["stand"].(string); s == "" {
		t.Errorf("stand fehlt: %v", body)
	}
	bereiche, _ := body["bereiche"].([]any)
	if len(bereiche) < 2 {
		t.Fatalf("bereiche %v", body["bereiche"])
	}
	arbeit := bereiche[0].(map[string]any)
	if arbeit["name"] != "Arbeit" || arbeit["ohne"] != false {
		t.Errorf("erster Bereich %v", arbeit)
	}
	ps := arbeit["projekte"].([]any)
	if len(ps) != 1 {
		t.Fatalf("Arbeit: %v", ps)
	}
	p := ps[0].(map[string]any)
	if p["name"] != "alpha" || p["pfad"] != alpha {
		t.Errorf("alpha: %v", p)
	}
	z := p["zaehler"].(map[string]any)
	if z["offen"] != float64(3) {
		t.Errorf("zaehler von alpha %v, erwartet offen 3 (k1, k1.1, k1.1.1)", z)
	}
	letzter := bereiche[len(bereiche)-1].(map[string]any)
	if letzter["ohne"] != true {
		t.Errorf("letzter Bereich ist nicht Ohne Bereich: %v", letzter)
	}
	gesehen := 0
	for _, b := range bereiche {
		for _, p := range b.(map[string]any)["projekte"].([]any) {
			if p.(map[string]any)["name"] == "beta" {
				gesehen++
			}
		}
	}
	if gesehen != 1 {
		t.Errorf("beta steht %d mal in der Leiste", gesehen)
	}
}

func TestAPIIstKeinProjektname(t *testing.T) {
	h, _ := apiUmgebung(t)
	rec, _ := getAPI(t, h, http.MethodGet, "/api/gibtsnicht")
	if rec.StatusCode != http.StatusNotFound {
		t.Errorf("/api/gibtsnicht: Status %d, erwartet 404", rec.StatusCode)
	}
}

func TestAPIPostAufLoopbackWirdAbgelehnt(t *testing.T) {
	h, _ := apiUmgebung(t)
	rec, _ := getAPI(t, h, http.MethodPost, "/api/aufgaben")
	if rec.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("POST /api/aufgaben ueber Loopback: Status %d, erwartet 405", rec.StatusCode)
	}
}

func TestAPIGetUeberLANUndPostAbgelehnt(t *testing.T) {
	h, _ := apiUmgebung(t)
	netz := startOn(t, "127.0.0.2", h)

	resp, err := http.Get(netz + "/api/leiste")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/leiste ueber LAN: Status %d: %s", resp.StatusCode, b)
	}
	var l map[string]any
	if err := json.Unmarshal(b, &l); err != nil || l["bereiche"] == nil {
		t.Errorf("GET /api/leiste ueber LAN liefert keine Leiste: %s", b)
	}

	resp, err = http.Post(netz+"/api/aufgaben", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode < 400 || resp.StatusCode >= 500 {
		t.Errorf("POST /api/aufgaben ueber LAN: Status %d, erwartet 4xx", resp.StatusCode)
	}
}

// getAPI schickt eine Anfrage ueber einen echten Loopback-Listener, damit das
// Netz-Gate die Anfrage als lokal erkennt.
func getAPI(t *testing.T, h http.Handler, methode, pfad string) (*http.Response, string) {
	t.Helper()
	basis := startOn(t, "127.0.0.1", h)
	req, err := http.NewRequest(methode, basis+pfad, strings.NewReader(""))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, string(b)
}
