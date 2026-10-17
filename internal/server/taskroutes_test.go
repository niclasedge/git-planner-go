package server_test

// Tests fuer POST /action/task/* (gkp-t1o): die vier Schreiboperationen der
// Ansicht — anlegen, aendern, terminieren, Bereich setzen.
//
// Zwei Eigenheiten gegenueber den anderen Tests dieses Pakets:
//
//  1. Sie laufen gegen echte net.Listener, nicht gegen httptest.NewRecorder.
//     Das Netz-Gate haengt an der Verbindung (http.LocalAddrContextKey), und
//     ein Recorder hat keine — die Zusicherung "LAN bleibt lesend" waere damit
//     gar nicht pruefbar. startOn/twoListeners stehen in network_gate_test.go.
//
//  2. Kein Test schreibt in eine echte Datenbank oder in die Area-Datei des
//     Benutzers: CC_AREAS_FILE zeigt auf t.TempDir(), und wo bd wirklich laufen
//     soll, legt wegwerfRepoFuerRouten eine eigene DB an.

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/niclasedge/git-planner-go/internal/appctx"
	"github.com/niclasedge/git-planner-go/internal/assets"
	"github.com/niclasedge/git-planner-go/internal/cache"
	"github.com/niclasedge/git-planner-go/internal/collectors"
	"github.com/niclasedge/git-planner-go/internal/render"
	"github.com/niclasedge/git-planner-go/internal/server"
)

// taskHandler baut den Handler mit einer Area-Datei unter t.TempDir(). Das
// t.Setenv muss VOR NewHandler stehen: der Handler bindet seinen Area-Speicher
// einmal beim Bau an den dann gueltigen Pfad.
func taskHandler(t *testing.T) (http.Handler, string) {
	t.Helper()
	areaDatei := filepath.Join(t.TempDir(), "areas.json")
	t.Setenv("CC_AREAS_FILE", areaDatei)
	h := server.NewHandler(server.AppOptions{
		Cwd:     t.TempDir(),
		Assets:  render.Assets{DashCSS: assets.DashCSS, BrowserCSS: assets.BrowserCSS, BrowserJS: assets.BrowserJS},
		AssetFS: assets.FS(),
	})
	return h, areaDatei
}

func taskServer(t *testing.T) (basis, areaDatei string) {
	t.Helper()
	h, areaDatei := taskHandler(t)
	return startOn(t, "127.0.0.1", h), areaDatei
}

// postJSON schickt einen JSON-Rumpf und gibt Antwort und Rumpf zurueck.
func postJSON(t *testing.T, basis, pfad, rumpf string, kopf map[string]string) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, basis+pfad, strings.NewReader(rumpf))
	if err != nil {
		t.Fatalf("Anfrage bauen: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range kopf {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Anfrage schicken: %v", err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return resp, b
}

// alsJSON besteht darauf, dass die Antwort ein JSON-Objekt ist — Kopfzeile und
// Rumpf. Genau das ist der Unterschied zu /action/* heute, das eine
// HTML-Ergebnisseite liefert.
func alsJSON(t *testing.T, resp *http.Response, rumpf []byte) map[string]any {
	t.Helper()
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type = %q, erwartet application/json (Rumpf: %s)", ct, rumpf)
	}
	var m map[string]any
	if err := json.Unmarshal(rumpf, &m); err != nil {
		t.Fatalf("Rumpf ist kein JSON-Objekt: %v (%s)", err, rumpf)
	}
	return m
}

// TestTaskRoutenAntwortenMitJSON ist das erste Abnahmekriterium: jede der vier
// Routen antwortet mit JSON. Geprueft wird hier der Fehlerweg, weil er ohne bd
// auskommt und damit deterministisch ist — die Faelle unten decken den
// Erfolgsweg ab.
func TestTaskRoutenAntwortenMitJSON(t *testing.T) {
	basis, _ := taskServer(t)

	faelle := []struct{ pfad, rumpf string }{
		{"/action/task/anlegen", `{"titel":"ohne Pfad"}`},
		{"/action/task/aendern", `{"id":"tstx-1","titel":"ohne Pfad"}`},
		{"/action/task/terminieren", `{"id":"tstx-1","frist":"2026-01-15"}`},
		{"/action/task/bereich", `{"bereich":"Arbeit"}`},
	}
	for _, f := range faelle {
		resp, rumpf := postJSON(t, basis, f.pfad, f.rumpf, nil)
		m := alsJSON(t, resp, rumpf)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("POST %s: Status = %d, erwartet 400 (Eingabe wurde vor bd abgelehnt)", f.pfad, resp.StatusCode)
		}
		if m["grund"] != "eingabe" {
			t.Errorf("POST %s: grund = %v, erwartet \"eingabe\"", f.pfad, m["grund"])
		}
		if s, _ := m["fehler"].(string); s == "" {
			t.Errorf("POST %s: fehler fehlt oder ist leer (%s)", f.pfad, rumpf)
		}
	}
}

// TestBereichSetzenAntwortetMitNeuemStand ist der eine Erfolgsweg, der ohne bd
// auskommt: die Area steht in areas.json, nicht in der Datenbank (Spec §4.3).
func TestBereichSetzenAntwortetMitNeuemStand(t *testing.T) {
	basis, areaDatei := taskServer(t)

	resp, rumpf := postJSON(t, basis, "/action/task/bereich", `{"projekt":"demo","bereich":"Arbeit"}`, nil)
	m := alsJSON(t, resp, rumpf)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Status = %d, erwartet 200 (%s)", resp.StatusCode, rumpf)
	}
	if m["projekt"] != "demo" || m["bereich"] != "Arbeit" {
		t.Errorf("Antwort = %s, erwartet projekt=demo bereich=Arbeit", rumpf)
	}
	if _, err := os.Stat(areaDatei); err != nil {
		t.Errorf("areas.json wurde nicht geschrieben: %v", err)
	}
}

// TestUnbekanntesFeldWirdAbgewiesen ist das dritte Abnahmekriterium und der
// teuerste Fehler, den dieses Repo dazu schon gemacht hat: `parent_key` statt
// `parent` erzeugte beim ersten echten beads_create zwei elternlose Beads
// ausserhalb jeder Karte — als Erfolg gemeldet. Ein still verworfenes Feld ist
// eine Falschmeldung, kein fehlendes Detail.
func TestUnbekanntesFeldWirdAbgewiesen(t *testing.T) {
	basis, _ := taskServer(t)

	faelle := []struct{ pfad, rumpf, feld string }{
		{"/action/task/anlegen", `{"pfad":"/nirgends","titel":"x","prioritaet_falsch":"1"}`, "prioritaet_falsch"},
		{"/action/task/aendern", `{"pfad":"/nirgends","id":"tstx-1","titel_neu":"x"}`, "titel_neu"},
		{"/action/task/terminieren", `{"pfad":"/nirgends","id":"tstx-1","deadline":"2026-01-15"}`, "deadline"},
		{"/action/task/bereich", `{"projekt":"demo","area":"Arbeit"}`, "area"},
	}
	for _, f := range faelle {
		resp, rumpf := postJSON(t, basis, f.pfad, f.rumpf, nil)
		m := alsJSON(t, resp, rumpf)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("POST %s mit %q: Status = %d, erwartet 400", f.pfad, f.feld, resp.StatusCode)
		}
		if s, _ := m["fehler"].(string); !strings.Contains(s, f.feld) {
			t.Errorf("POST %s: Meldung %q benennt das abgewiesene Feld %q nicht", f.pfad, s, f.feld)
		}
	}
}

// TestTaskRouteUeberNetzAdresseIstVerboten belegt, dass das BESTEHENDE
// Netz-Gate (server.go:191) die neuen Routen mit abdeckt — E4 verlangt dort
// keine Aenderung, und eine zweite Pruefung daneben waere eine Kopie, die
// auseinanderlaufen kann. Geprueft wird nicht nur der Status: die
// Bereich-Route ist die einzige, die ohne bd schreibt, also ist die NICHT
// entstandene areas.json der Beleg, dass das Gate vor dem Handler gegriffen hat.
func TestTaskRouteUeberNetzAdresseIstVerboten(t *testing.T) {
	h, areaDatei := taskHandler(t)
	netzURL := startOn(t, "127.0.0.2", h)

	for _, pfad := range []string{
		"/action/task/anlegen", "/action/task/aendern",
		"/action/task/terminieren", "/action/task/bereich",
	} {
		resp, rumpf := postJSON(t, netzURL, pfad, `{"projekt":"demo","bereich":"Arbeit"}`, nil)
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("POST %s ueber die Netz-Adresse: Status = %d, erwartet 403 (%s)", pfad, resp.StatusCode, rumpf)
		}
	}
	if _, err := os.Stat(areaDatei); !os.IsNotExist(err) {
		t.Errorf("areas.json existiert nach abgewiesenen Anfragen (%v) — das Gate hat zu spaet gegriffen", err)
	}
}

// TestTaskRouteUeberLoopbackPassiertDasGate ist die Gegenprobe: dieselbe
// Anfrage ueber 127.0.0.1 darf nicht am Gate haengenbleiben. Ein Test, der nur
// die Abweisung prueft, waere auch gruen, wenn die Route gar nicht existierte.
func TestTaskRouteUeberLoopbackPassiertDasGate(t *testing.T) {
	h, _ := taskHandler(t)
	basis := startOn(t, "127.0.0.1", h)

	resp, rumpf := postJSON(t, basis, "/action/task/bereich", `{"projekt":"demo","bereich":"Arbeit"}`, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Status = %d, erwartet 200 ueber Loopback (%s)", resp.StatusCode, rumpf)
	}
}

// TestTaskRouteNurPost: die Routen schreiben, ein GET darauf ist ein Irrtum
// des Aufrufers und kein stiller Nichts-Tuer.
func TestTaskRouteNurPost(t *testing.T) {
	basis, _ := taskServer(t)

	resp, err := http.Get(basis + "/action/task/bereich")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	rumpf, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("GET: Status = %d, erwartet 405 (%s)", resp.StatusCode, rumpf)
	}
}

// TestTaskRouteFremdeOrigin: csrfOk bleibt als zweite Schicht unter dem
// Netz-Gate — es faengt den Fall, den das Gate nicht sieht, naemlich einen
// Browser-Tab AUF DIESEM Rechner, der auf Loopback POSTet.
func TestTaskRouteFremdeOrigin(t *testing.T) {
	basis, areaDatei := taskServer(t)

	resp, rumpf := postJSON(t, basis, "/action/task/bereich", `{"projekt":"demo","bereich":"Arbeit"}`,
		map[string]string{"Origin": "https://boese.example"})
	alsJSON(t, resp, rumpf)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("Status = %d, erwartet 403 (%s)", resp.StatusCode, rumpf)
	}
	if _, err := os.Stat(areaDatei); !os.IsNotExist(err) {
		t.Errorf("areas.json existiert trotz fremder Origin (%v)", err)
	}
}

// TestUnbekannteTaskRoute: ein Tippfehler im Pfad ist ein 404 mit JSON, nicht
// ein Durchfallen in die HTML-Ergebnisseite von /action/*.
func TestUnbekannteTaskRoute(t *testing.T) {
	basis, _ := taskServer(t)

	resp, rumpf := postJSON(t, basis, "/action/task/quatsch", `{}`, nil)
	alsJSON(t, resp, rumpf)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("Status = %d, erwartet 404 (%s)", resp.StatusCode, rumpf)
	}
}

// TestFehlendesBdIstServerfehlerUndInvalidiertDenCache prueft die zweite
// Haelfte der Fehlerabbildung und das vierte Abnahmekriterium in einem Zug.
//
// "not-found" heisst bei bdops ausschliesslich: kein bd auf dem PATH. Das ist
// ein Defekt der Installation, kein fehlender Bead — es als 404 zu beantworten
// hiesse, dem Benutzer zu sagen, seine Aufgabe existiere nicht.
func TestFehlendesBdIstServerfehlerUndInvalidiertDenCache(t *testing.T) {
	leer := t.TempDir()
	t.Setenv("PATH", leer) // kein bd erreichbar
	basis, _ := taskServer(t)

	repo := t.TempDir()
	rufe := zaehlenderCache(t, repo)

	resp, rumpf := postJSON(t, basis, "/action/task/aendern", `{"pfad":`+jsonStr(repo)+`,"id":"tstx-1","titel":"x"}`, nil)
	m := alsJSON(t, resp, rumpf)
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("Status = %d, erwartet 500 (%s)", resp.StatusCode, rumpf)
	}
	if m["grund"] != "not-found" {
		t.Errorf("grund = %v, erwartet \"not-found\" (kein bd auf dem PATH)", m["grund"])
	}
	if n := rufe(); n != 2 {
		t.Errorf("Cache-Berechnungen = %d, erwartet 2 — der Projekt-Cache wurde nicht invalidiert", n)
	}
}

// ---------------------------------------------------------------- teilbaum

// TestSchreibantwortIstDerAstAusEinemVollaufbau ist das Abnahmekriterium von
// gkp-8a0 (Spec §7.2): die Antwort einer Schreiboperation traegt den NEU
// GEBAUTEN Ast der betroffenen Aufgabe, und der Ast entspricht dem, was ein
// voller Neuaufbau an dieser Stelle ergeben haette.
//
// Verglichen wird deshalb gegen appctx.CollectReportsData — den Aufbau, aus dem
// die Seite entsteht — und nicht gegen eine handgeschriebene Erwartung. Eine
// handgeschriebene Erwartung waere eine zweite Meinung darueber, wie der Baum
// aussieht; genau die soll es nicht geben, und genau an so einer Doppelung ist
// dieses Repo schon zweimal vorbeigeschrammt (gkp-1lp/gkp-b9n: die Struktur war
// gruen, die Verbraucher stiegen nicht ab).
//
// Der Baum wird dreistufig aufgebaut (Karte > Elternteil > zwei Kinder), weil
// an einem zweistufigen "ueber alle Nachfahren" von "ueber die direkten Kinder"
// nicht zu unterscheiden ist.
func TestSchreibantwortIstDerAstAusEinemVollaufbau(t *testing.T) {
	// Eigener Report-Store: der Baum liest Belege aus ~/.claude/reports, und
	// ein Test hat im Store des Benutzers nichts zu suchen.
	t.Setenv("CC_REPORTS_DIR", t.TempDir())
	repo := wegwerfRepoFuerRouten(t)
	basis, _ := taskServer(t)

	eltern := anlegenUeberRoute(t, basis, repo, `"titel":"Elternteil"`)
	kindA := anlegenUeberRoute(t, basis, repo, `"titel":"Kind A","elternteil":`+jsonStr(eltern))
	kindB := anlegenUeberRoute(t, basis, repo, `"titel":"Kind B","elternteil":`+jsonStr(eltern))

	// Der vierte Schreibvorgang trifft den ELTERNTEIL: nur so traegt der
	// gelieferte Ast selbst Unteraufgaben, und nur dann sagt der Vergleich
	// etwas ueber mehr als ein einzelnes Blatt.
	resp, rumpf := postJSON(t, basis, "/action/task/aendern",
		`{"pfad":`+jsonStr(repo)+`,"id":`+jsonStr(eltern)+`,"titel":"Elternteil, umbenannt"}`, nil)
	m := alsJSON(t, resp, rumpf)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Aendern: Status = %d, erwartet 200 (%s)", resp.StatusCode, rumpf)
	}
	tb, _ := m["teilbaum"].(map[string]any)
	if tb == nil {
		t.Fatalf("Antwort ohne teilbaum (%s)", rumpf)
	}

	// --- der Ast selbst, gegen den Vollaufbau
	view := appctx.CollectReportsData(repo)
	soll, ok := beadImVollaufbau(view.Maps, eltern)
	if !ok {
		t.Fatalf("%s steht nach dem Schreiben in keinem Vollaufbau — dann ist der Vergleich sinnlos", eltern)
	}
	if got, want := tb["aufgabe"], alsWert(t, soll); !reflect.DeepEqual(got, want) {
		t.Errorf("gelieferter Ast weicht vom Vollaufbau ab\n hat:  %v\n soll: %v", got, want)
	}

	// --- die Zusagen, die der Vergleich allein nicht sichtbar macht
	auf, _ := tb["aufgabe"].(map[string]any)
	if auf["title"] != "Elternteil, umbenannt" {
		t.Errorf("Ast traegt %v, erwartet den gerade geschriebenen Titel", auf["title"])
	}
	// Counts ist ein Zeiger (gkp-wsk): fehlt er, ist die Zeile ein Blatt. Hier
	// darf er nicht fehlen, und er zaehlt BEIDE Kinder — den ganzen Teilbaum,
	// nicht den gezeigten Ausschnitt.
	counts, _ := auf["counts"].(map[string]any)
	if counts == nil || counts["total"] != float64(2) || counts["open"] != float64(2) {
		t.Errorf("counts am Ast = %v, erwartet total 2 / open 2 (beide Kinder)", auf["counts"])
	}
	kinder, _ := auf["children"].([]any)
	if len(kinder) != 2 {
		t.Fatalf("Ast hat %d Unteraufgaben, erwartet 2 — zwei Schreibvorgaenge, zwei Kinder", len(kinder))
	}
	gesehen := map[string]bool{}
	for _, k := range kinder {
		kb, _ := k.(map[string]any)
		gesehen[aStr(kb["id"])] = true
		if kb["counts"] != nil {
			t.Errorf("Blatt %v traegt counts %v, erwartet keine — ein Balken, der nur 0 oder 100 %% kann, ist Rauschen", kb["id"], kb["counts"])
		}
	}
	if !gesehen[kindA] || !gesehen[kindB] {
		t.Errorf("Ast traegt %v, erwartet %s und %s — ein Schreibvorgang hat den anderen verloren", gesehen, kindA, kindB)
	}

	// --- der Weg: ohne ihn hat eine neu angelegte Aufgabe keinen Platz
	if tb["karte"] != collectors.LooseMapID {
		t.Errorf("karte = %v, erwartet %q (die Aufgaben haengen unter keinem Epic)", tb["karte"], collectors.LooseMapID)
	}
	weg, _ := tb["weg"].([]any)
	if len(weg) != 1 {
		t.Fatalf("weg = %v, erwartet genau die Karte — der Elternteil haengt direkt darunter", tb["weg"])
	}
	karteSoll, ok := karteImVollaufbau(view.Maps, collectors.LooseMapID)
	if !ok {
		t.Fatalf("Karte %s fehlt im Vollaufbau", collectors.LooseMapID)
	}
	w0, _ := weg[0].(map[string]any)
	if got, want := w0["counts"], alsWert(t, karteSoll.Counts); !reflect.DeepEqual(got, want) {
		t.Errorf("weg[0].counts = %v, erwartet die Zahlen der Karte aus dem Vollaufbau %v", got, want)
	}
	if got, want := w0["kinder"], alsWert(t, kinderIDsAus(karteSoll)); !reflect.DeepEqual(got, want) {
		t.Errorf("weg[0].kinder = %v, erwartet %v — die Reihenfolge entsteht in Go, nicht im Browser", got, want)
	}
}

// TestTeilbaumEinerKarteNenntDenGrund: eine Karte hat keinen Elternteil, in den
// die Ansicht sie einsetzen koennte. Der Schreibvorgang glueckt trotzdem — und
// die Antwort muss sagen, dass sie den Ast nicht liefern kann. Sonst sieht die
// Ansicht denselben Rumpf wie bei "nichts hat sich geaendert" und laesst einen
// veralteten Stand stehen.
func TestTeilbaumEinerKarteNenntDenGrund(t *testing.T) {
	t.Setenv("CC_REPORTS_DIR", t.TempDir())
	repo := wegwerfRepoFuerRouten(t)
	basis, _ := taskServer(t)

	epic := anlegenUeberRoute(t, basis, repo, `"titel":"Eine Karte","typ":"epic"`)
	resp, rumpf := postJSON(t, basis, "/action/task/aendern",
		`{"pfad":`+jsonStr(repo)+`,"id":`+jsonStr(epic)+`,"titel":"Karte, umbenannt"}`, nil)
	m := alsJSON(t, resp, rumpf)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Aendern: Status = %d, erwartet 200 (%s)", resp.StatusCode, rumpf)
	}
	if m["teilbaum"] != nil {
		t.Errorf("teilbaum = %v, erwartet keinen — eine Karte ist kein Ast", m["teilbaum"])
	}
	if s := aStr(m["nachladen"]); s == "" || !strings.Contains(s, epic) {
		t.Errorf("nachladen = %q, erwartet einen Grund, der %s benennt", s, epic)
	}
}

// anlegenUeberRoute legt eine Aufgabe ueber die Route an und gibt ihre Id
// zurueck. Ueber die ROUTE und nicht mit `bd create` daneben: der Test soll den
// Weg gehen, den die Ansicht geht.
func anlegenUeberRoute(t *testing.T, basis, repo, felder string) string {
	t.Helper()
	resp, rumpf := postJSON(t, basis, "/action/task/anlegen",
		`{"pfad":`+jsonStr(repo)+`,`+felder+`}`, nil)
	m := alsJSON(t, resp, rumpf)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Anlegen (%s): Status = %d (%s)", felder, resp.StatusCode, rumpf)
	}
	auf, _ := m["aufgabe"].(map[string]any)
	id := aStr(auf["id"])
	if id == "" {
		t.Fatalf("Anlegen (%s) ohne Id: %s", felder, rumpf)
	}
	return id
}

// beadImVollaufbau sucht eine Aufgabe im frisch gebauten Baum — in JEDER Tiefe.
// Nur auf der ersten Ebene zu suchen hiesse, einen Enkel still an seinem
// Elternteil zu pruefen.
func beadImVollaufbau(maps []appctx.ReportsMap, id string) (appctx.ReportsBead, bool) {
	for _, m := range maps {
		if b, ok := sucheImAst(m.Beads, id); ok {
			return b, true
		}
	}
	return appctx.ReportsBead{}, false
}

func sucheImAst(beads []appctx.ReportsBead, id string) (appctx.ReportsBead, bool) {
	for _, b := range beads {
		if b.ID == id {
			return b, true
		}
		if t, ok := sucheImAst(b.Children, id); ok {
			return t, true
		}
	}
	return appctx.ReportsBead{}, false
}

func karteImVollaufbau(maps []appctx.ReportsMap, id string) (appctx.ReportsMap, bool) {
	for _, m := range maps {
		if m.ID == id {
			return m, true
		}
	}
	return appctx.ReportsMap{}, false
}

func kinderIDsAus(m appctx.ReportsMap) []string {
	out := make([]string, 0, len(m.Beads))
	for _, b := range m.Beads {
		out = append(out, b.ID)
	}
	return out
}

// alsWert schickt einen Go-Wert durch JSON und zurueck. Ohne diesen Umweg
// verglichen man ReportsBead gegen map[string]any und bekaeme immer "ungleich"
// — der Vergleich muss auf der Seite stattfinden, die auch beim Browser
// ankommt.
func alsWert(t *testing.T, v any) any {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("JSON: %v", err)
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("JSON zurueck: %v", err)
	}
	return out
}

func aStr(v any) string {
	s, _ := v.(string)
	return s
}

// jsonStr macht aus einem Pfad ein JSON-String-Literal.
func jsonStr(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// zaehlenderCache legt einen Eintrag fuer projektSchluessel an und gibt eine
// Funktion zurueck, die zaehlt, wie oft der Kontext berechnet wurde. Der
// zweite Aufruf hier darf NICHT zaehlen (frischer Eintrag) — steigt die Zahl
// nach dem Schreibvorgang, wurde invalidiert.
func zaehlenderCache(t *testing.T, projektSchluessel string) func() int {
	t.Helper()
	cache.ResetProjectContextCacheForTest()
	cache.SetStartedForTest(true)
	t.Cleanup(func() {
		cache.SetStartedForTest(false)
		cache.ResetProjectContextCacheForTest()
	})
	var n atomic.Int32
	berechne := func() map[string]any {
		n.Add(1)
		return map[string]any{}
	}
	cache.GetProjectContext(projektSchluessel, berechne)
	cache.GetProjectContext(projektSchluessel, berechne)
	if got := n.Load(); got != 1 {
		t.Fatalf("Aufbau: %d Berechnungen, erwartet 1 — der Cache greift nicht", got)
	}
	return func() int {
		cache.WaitForProjectContextForTest(projektSchluessel)
		cache.GetProjectContext(projektSchluessel, berechne)
		cache.WaitForProjectContextForTest(projektSchluessel)
		return int(n.Load())
	}
}

// wegwerfRepoFuerRouten legt eine eigene beads-DB unter t.TempDir() an —
// niemals gegen die Projekt-Datenbank. Gleiches Vorgehen wie
// taskops/taskops_bd_test.go, eigener Prefix, damit beide nebeneinander
// laufen koennen.
func wegwerfRepoFuerRouten(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("-short: bd init kostet eine Sekunde")
	}
	for _, werkzeug := range []string{"bd", "git"} {
		if _, err := exec.LookPath(werkzeug); err != nil {
			t.Skipf("%s nicht installiert", werkzeug)
		}
	}
	dir := t.TempDir()
	lauf := func(name string, args ...string) {
		t.Helper()
		cmd := exec.Command(name, args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("%s %v in der Wegwerf-DB fehlgeschlagen: %v\n%s", name, args, err, out)
		}
	}
	lauf("git", "init", "-q", ".")
	lauf("bd", "init", "--prefix", "tsrv", "--role", "maintainer", "--non-interactive")
	return dir
}

// TestAnlegenGegenEchtesBd faehrt die ganze Kette einmal durch: HTTP-Anfrage,
// taskops, bdops, bd, Ruecklesen, JSON-Antwort. Und er pinnt die Falle aus
// gkp-gvk fest: eine unbekannte Bead-Id ergibt bei bd Exit 1 und kommt als
// Grund "exit" an — das ist eine abgelehnte Eingabe (422), kein Serverfehler
// und kein 404.
func TestAnlegenGegenEchtesBd(t *testing.T) {
	repo := wegwerfRepoFuerRouten(t)
	basis, _ := taskServer(t)
	rufe := zaehlenderCache(t, repo)

	resp, rumpf := postJSON(t, basis, "/action/task/anlegen",
		`{"pfad":`+jsonStr(repo)+`,"titel":"Probe aus der Route","typ":"task","prioritaet":"2","frist":"2026-01-15"}`, nil)
	m := alsJSON(t, resp, rumpf)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Anlegen: Status = %d, erwartet 200 (%s)", resp.StatusCode, rumpf)
	}
	auf, _ := m["aufgabe"].(map[string]any)
	if auf == nil || auf["titel"] != "Probe aus der Route" {
		t.Fatalf("Antwort traegt nicht den neuen Stand: %s", rumpf)
	}
	if id, _ := auf["id"].(string); id == "" {
		t.Fatalf("Antwort ohne Id: %s", rumpf)
	}
	if n := rufe(); n != 2 {
		t.Errorf("Cache-Berechnungen = %d, erwartet 2 — der Projekt-Cache wurde nicht invalidiert", n)
	}

	if m["teilbaum"] == nil {
		t.Errorf("Anlegen antwortet ohne teilbaum (%s)", rumpf)
	}

	// Unbekannte Id: bd laeuft, bd lehnt ab.
	resp, rumpf = postJSON(t, basis, "/action/task/aendern",
		`{"pfad":`+jsonStr(repo)+`,"id":"tsrv-gibtesnicht","titel":"x"}`, nil)
	m = alsJSON(t, resp, rumpf)
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("unbekannte Id: Status = %d, erwartet 422 (%s)", resp.StatusCode, rumpf)
	}
	if m["grund"] != "exit" {
		t.Errorf("unbekannte Id: grund = %v, erwartet \"exit\" — not-found hiesse: kein bd auf dem PATH", m["grund"])
	}
}

// ---------------------------------------------------------------- ansichten

// TestTerminierenAendertDieHeuteAuswahl (gkp-7gb) faehrt die Kette durch, um
// die es in dieser Ansicht geht: HTTP → taskops → bd → Baum neu → AUSWAHL neu.
//
// Der Teilbaum allein reicht der Heute-Liste nicht, und genau das misst dieser
// Test. Ein Datum zu setzen aendert womoeglich die MITGLIEDSCHAFT: eine
// Aufgabe, die bis in vier Wochen versteckt wird, ist nicht mehr startklar und
// gehoert nicht mehr in Heute; eine mit Frist auf heute gehoert hinein, und
// zwar in den Eimer "fällig heute". Der Teilbaum kann das nicht ausdruecken —
// er ersetzt eine Zeile, er waehlt nicht aus.
//
// Gegen ECHTES bd und nicht gegen eine Attrappe, weil die Zeitbasis der Kern
// der Sache ist: bd schreibt due_at als lokale Wanduhr mit einem Z, das luegt
// (Spec §4.4). Eine Attrappe wuerde genau die Stelle ueberspringen, an der das
// schiefgehen kann.
//
// NACHGEZOGEN 2026-09-16 (gkp-89g, zweite Korrektur): "startklar" zaehlt seit
// dieser Korrektur nicht mehr in Heute, nur noch in Jederzeit. Der letzte
// Teil des Tests pruefte bis dahin, dass das Loeschen des When die Aufgabe
// wieder in Heute zeigt — das waere jetzt falsch. Er prueft seitdem das
// Gegenteil: die Aufgabe bleibt nach dem Loeschen des When aus Heute
// draussen und taucht dafuer in Jederzeit auf.
func TestTerminierenAendertDieHeuteAuswahl(t *testing.T) {
	t.Setenv("CC_REPORTS_DIR", t.TempDir())
	repo := wegwerfRepoFuerRouten(t)
	basis, _ := taskServer(t)

	id := anlegenUeberRoute(t, basis, repo, `"titel":"Aufgabe mit Termin"`)

	// --- Frist auf HEUTE: drin, und im Eimer "fällig heute".
	// Das Datum kommt aus der Uhr dieses Rechners und nicht aus einer Konstante:
	// bd deutet ein blosses Datum als lokale Mitternacht, und der Test muss
	// gegen denselben Kalendertag laufen wie der Dienst.
	heuteTag := time.Now().Format("2006-01-02")
	m := terminierenUeberRoute(t, basis, repo, id, `"frist":`+jsonStr(heuteTag))
	e := heuteEintrag(t, m, id)
	if e == nil {
		t.Fatalf("%s steht nach einer Frist auf heute nicht in Heute: %v", id, m["ansichten"])
	}
	if e["rang"] != float64(1) {
		t.Errorf("rang = %v, erwartet 1 (fällig heute) — 0 waere ueberfaellig, 2 der Rest", e["rang"])
	}
	if !enthaeltString(e["gruende"], "fällig heute") {
		t.Errorf("gruende = %v, erwartet \"fällig heute\"", e["gruende"])
	}
	if e["frist"] != heuteTag {
		// Die gedeutete Ortszeit, nicht bds Rohwert mit dem luegenden Z.
		t.Errorf("frist = %v, erwartet %q — der Rumpf traegt die DEUTUNG, nicht bds Rohwert", e["frist"], heuteTag)
	}
	if e["frist_tage"] != float64(0) {
		t.Errorf("frist_tage = %v, erwartet 0", e["frist_tage"])
	}

	// --- Frist weg, dafuer vier Wochen verstecken: raus. Nicht mehr startklar
	// (bd blendet ein deferred Issue aus `bd ready` aus), keine Frist, offen.
	m = terminierenUeberRoute(t, basis, repo, id, `"frist":"","wann":"+4w"`)
	if e := heuteEintrag(t, m, id); e != nil {
		t.Errorf("%s steht nach --defer +4w immer noch in Heute: %v", id, e)
	}

	// --- und wieder hervorholen: das Loeschen des When macht sie wieder
	// startklar — und "startklar" zaehlt seit gkp-89g NICHT mehr in Heute,
	// nur noch in Jederzeit. Ohne die Heute-Gegenprobe waere der Test auch
	// dann gruen, wenn die Auswahl schlicht immer alles auswaehlt.
	m = terminierenUeberRoute(t, basis, repo, id, `"wann":""`)
	if e := heuteEintrag(t, m, id); e != nil {
		t.Errorf("%s steht nach dem Loeschen des When wieder in Heute, ist aber nur noch startklar: %v", id, e)
	}
	e = ansichtEintrag(t, m, "jederzeit", id)
	if e == nil {
		t.Fatalf("%s fehlt nach dem Loeschen des When in Jederzeit: %v", id, m["ansichten"])
	}
	if !enthaeltString(e["gruende"], "startklar") {
		t.Errorf("gruende(jederzeit) = %v, erwartet \"startklar\"", e["gruende"])
	}
}

func terminierenUeberRoute(t *testing.T, basis, repo, id, felder string) map[string]any {
	t.Helper()
	resp, rumpf := postJSON(t, basis, "/action/task/terminieren",
		`{"pfad":`+jsonStr(repo)+`,"id":`+jsonStr(id)+`,`+felder+`}`, nil)
	m := alsJSON(t, resp, rumpf)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Terminieren (%s): Status = %d (%s)", felder, resp.StatusCode, rumpf)
	}
	return m
}

// ansichtEintrag sucht die Zeile einer Aufgabe in einer benannten Ansicht der
// Antwort. nil heisst "steht nicht darin" — und der Test unterscheidet das
// ausdruecklich von "steht mit Tiefe 0 darin", weil ein fehlender Eintrag
// sonst wie ein harmloser Nullwert aussaehe.
func ansichtEintrag(t *testing.T, m map[string]any, ansicht, id string) map[string]any {
	t.Helper()
	ansichten, ok := m["ansichten"].(map[string]any)
	if !ok {
		t.Fatalf("Antwort ohne ansichten: %v", m)
	}
	a, ok := ansichten[ansicht].(map[string]any)
	if !ok {
		t.Fatalf("ansichten ohne %s: %v", ansicht, ansichten)
	}
	for _, roh := range a["eintraege"].([]any) {
		e, _ := roh.(map[string]any)
		if e != nil && aStr(e["bead"]) == id {
			return e
		}
	}
	return nil
}

// heuteEintrag ist ansichtEintrag fuer "heute" — die weitaus haeufigste
// Abfrage in dieser Datei.
func heuteEintrag(t *testing.T, m map[string]any, id string) map[string]any {
	t.Helper()
	return ansichtEintrag(t, m, "heute", id)
}

func enthaeltString(v any, gesucht string) bool {
	arr, _ := v.([]any)
	for _, x := range arr {
		if aStr(x) == gesucht {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------- gkp-8ww

// bereicheProbe baut einen Server, dessen Projekt-Entdeckung auf ein
// Wegwerfverzeichnis zeigt. Ohne das liefe der Test gegen die echten Repos
// dieser Maschine — die Antwort haenge dann davon ab, was gerade unter ~/git
// liegt, und ein Test, dessen Erwartung die Maschine stellt, haelt nichts fest.
//
// Cwd zeigt auf das ERSTE Fixture-Projekt, nicht auf ein eigenes t.TempDir().
// Grund, beim ersten Lauf gemessen: DiscoverProjectsIn nimmt das aktive Repo
// IMMER auf, auch wenn es unter keiner Wurzel liegt. Ein fremdes Cwd taucht
// damit als zusaetzliches Projekt namens "003" in der Gruppierung auf — eine
// echte Eigenschaft der Entdeckung, aber eine, die hier nur Rauschen waere.
func bereicheProbe(t *testing.T, projekte ...string) (basis, areaDatei string) {
	t.Helper()
	wurzel := t.TempDir()
	for _, p := range projekte {
		if err := os.MkdirAll(filepath.Join(wurzel, p, ".beads"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("CC_KNOWLEDGE_ROOTS", wurzel)

	areaDatei = filepath.Join(t.TempDir(), "areas.json")
	t.Setenv("CC_AREAS_FILE", areaDatei) // VOR NewHandler: der Handler bindet einmal
	h := server.NewHandler(server.AppOptions{
		Cwd:     filepath.Join(wurzel, projekte[0]),
		Assets:  render.Assets{DashCSS: assets.DashCSS, BrowserCSS: assets.BrowserCSS, BrowserJS: assets.BrowserJS},
		AssetFS: assets.FS(),
	})
	return startOn(t, "127.0.0.1", h), areaDatei
}

// gruppenText macht den `bereiche`-Block einer Antwort zu einer vergleichbaren
// Zeilenfolge — dieselbe Form wie bereicheText in appctx, damit eine FEHLENDE
// Gruppe von einer leeren zu unterscheiden ist (Spec §9.1, Fall 4).
func gruppenText(t *testing.T, m map[string]any) string {
	t.Helper()
	block, ok := m["bereiche"].(map[string]any)
	if !ok {
		t.Fatalf("Antwort ohne bereiche-Block: %v", m)
	}
	gs, _ := block["gruppen"].([]any)
	var b strings.Builder
	for _, roh := range gs {
		g, _ := roh.(map[string]any)
		b.WriteString(jsStr(g["bereich"]) + ":")
		ps, _ := g["projekte"].([]any)
		for _, rohP := range ps {
			p, _ := rohP.(map[string]any)
			b.WriteString(" " + jsStr(p["name"]))
		}
		b.WriteString("\n")
	}
	return b.String()
}

func jsStr(v any) string { s, _ := v.(string); return s }

// Die vier Operationen des Beads, hintereinander am laufenden Server, und
// jedesmal die neu gebaute Gruppierung als Antwort.
//
// Das ist das Gegenstueck zum `teilbaum` der drei bd-Routen (Spec §7.2): eine
// Aufgabenaenderung betrifft EINEN Ast, eine Area-Aenderung betrifft die ganze
// Zuordnung — ein Umbenennen zieht jedes Projekt der Area mit. Ein Teilbaum
// waere hier nicht ein Teil, sondern n Teile.
func TestBereichRoutenAntwortenMitDerNeuenGruppierung(t *testing.T) {
	basis, areaDatei := bereicheProbe(t, "alpha-tool", "omega-app")

	// 1. anlegen — ohne Projekt, das ist der Sinn der eigenen Route.
	resp, rumpf := postJSON(t, basis, "/action/task/bereich/anlegen", `{"bereich":"Arbeit"}`, nil)
	m := alsJSON(t, resp, rumpf)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("anlegen: Status = %d (%s)", resp.StatusCode, rumpf)
	}
	// Die leere Area steht da, beide Projekte liegen noch in der Sammel-Area.
	if got, want := gruppenText(t, m), "Arbeit:\nOhne Bereich: alpha-tool omega-app\n"; got != want {
		t.Errorf("nach anlegen:\n--- bekommen ---\n%s--- erwartet ---\n%s", got, want)
	}

	// 2. zuordnen
	resp, rumpf = postJSON(t, basis, "/action/task/bereich", `{"projekt":"omega-app","bereich":"Arbeit"}`, nil)
	m = alsJSON(t, resp, rumpf)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("bereich: Status = %d (%s)", resp.StatusCode, rumpf)
	}
	if got, want := gruppenText(t, m), "Arbeit: omega-app\nOhne Bereich: alpha-tool\n"; got != want {
		t.Errorf("nach zuordnen:\n--- bekommen ---\n%s--- erwartet ---\n%s", got, want)
	}

	// 3. umbenennen — zieht die Zuordnung mit
	resp, rumpf = postJSON(t, basis, "/action/task/bereich/umbenennen", `{"alt":"Arbeit","neu":"Beruf"}`, nil)
	m = alsJSON(t, resp, rumpf)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("umbenennen: Status = %d (%s)", resp.StatusCode, rumpf)
	}
	if got, want := gruppenText(t, m), "Beruf: omega-app\nOhne Bereich: alpha-tool\n"; got != want {
		t.Errorf("nach umbenennen:\n--- bekommen ---\n%s--- erwartet ---\n%s", got, want)
	}

	// 4. Zuordnung loesen — das Projekt bleibt SICHTBAR, unter der Sammel-Area
	resp, rumpf = postJSON(t, basis, "/action/task/bereich", `{"projekt":"omega-app","bereich":""}`, nil)
	m = alsJSON(t, resp, rumpf)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("loesen: Status = %d (%s)", resp.StatusCode, rumpf)
	}
	if got, want := gruppenText(t, m), "Beruf:\nOhne Bereich: alpha-tool omega-app\n"; got != want {
		t.Errorf("nach loesen:\n--- bekommen ---\n%s--- erwartet ---\n%s", got, want)
	}
	if _, err := os.Stat(areaDatei); err != nil {
		t.Errorf("areas.json fehlt: %v", err)
	}
}

// Eine abgelehnte Eingabe ist ein 400, keine Stoerungsmeldung. Der Weg dahin
// fuehrt ueber areas.ErrEingabe -> taskops.GrundEingabe -> statusFuer; reisst
// er an einer Stelle, wird aus einem Tippfehler ein 500.
func TestBereichUmbenennenUnbekanntIst400(t *testing.T) {
	basis, _ := bereicheProbe(t, "alpha-tool")

	resp, rumpf := postJSON(t, basis, "/action/task/bereich/umbenennen", `{"alt":"Arbiet","neu":"Beruf"}`, nil)
	m := alsJSON(t, resp, rumpf)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("Status = %d, erwartet 400 (%s)", resp.StatusCode, rumpf)
	}
	if m["grund"] != "eingabe" {
		t.Errorf("grund = %v, erwartet \"eingabe\"", m["grund"])
	}
}

// Auch die neuen Routen lesen mit DisallowUnknownFields: ein still verworfenes
// Feld waere eine Falschmeldung (siehe TestUnbekanntesFeldWirdAbgewiesen).
func TestBereichRoutenWeisenUnbekanntesFeldAb(t *testing.T) {
	basis, _ := bereicheProbe(t, "alpha-tool")

	faelle := []struct{ pfad, rumpf, feld string }{
		{"/action/task/bereich/anlegen", `{"name":"Arbeit"}`, "name"},
		{"/action/task/bereich/umbenennen", `{"alt":"Arbeit","new":"Beruf"}`, "new"},
		{"/action/task/bereich/loeschen", `{"name":"Arbeit"}`, "name"},
	}
	for _, f := range faelle {
		resp, rumpf := postJSON(t, basis, f.pfad, f.rumpf, nil)
		m := alsJSON(t, resp, rumpf)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("POST %s: Status = %d, erwartet 400 (%s)", f.pfad, resp.StatusCode, rumpf)
		}
		if s, _ := m["fehler"].(string); !strings.Contains(s, f.feld) {
			t.Errorf("POST %s: Meldung %q benennt %q nicht", f.pfad, s, f.feld)
		}
	}
}

// ---------------------------------------------------------------- gkp-td3

// Die fuenfte Bereichsoperation am laufenden Server: loeschen. Sie antwortet
// wie die vier anderen mit der GANZEN neu gebauten Gruppierung — eine
// Area-Aenderung trifft keinen benennbaren Ast (siehe bereichsAntwort).
//
// Der Ablauf ist der, den der Bead beschreibt: eine Area versehentlich anlegen
// und wieder herauskommen. Behauptet wird jedesmal die ganze Zeilenfolge, damit
// eine FEHLENDE Gruppe von einer leeren zu unterscheiden ist (Spec §9.1).
func TestBereichLoeschenRouteNimmtDieVersehentlicheAreaZurueck(t *testing.T) {
	basis, areaDatei := bereicheProbe(t, "alpha-tool", "omega-app")

	// 1. Tippfehler beim Anlegen — genau der Fall, der bisher blieb.
	resp, rumpf := postJSON(t, basis, "/action/task/bereich/anlegen", `{"bereich":"Arbiet"}`, nil)
	m := alsJSON(t, resp, rumpf)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("anlegen: Status = %d (%s)", resp.StatusCode, rumpf)
	}
	if got, want := gruppenText(t, m), "Arbiet:\nOhne Bereich: alpha-tool omega-app\n"; got != want {
		t.Fatalf("nach anlegen:\n--- bekommen ---\n%s--- erwartet ---\n%s", got, want)
	}

	// 2. und wieder weg. Danach ist der Zustand exakt der von vorher.
	resp, rumpf = postJSON(t, basis, "/action/task/bereich/loeschen", `{"bereich":"Arbiet"}`, nil)
	m = alsJSON(t, resp, rumpf)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("loeschen: Status = %d (%s)", resp.StatusCode, rumpf)
	}
	if got, want := gruppenText(t, m), "Ohne Bereich: alpha-tool omega-app\n"; got != want {
		t.Errorf("nach loeschen:\n--- bekommen ---\n%s--- erwartet ---\n%s", got, want)
	}
	if _, err := os.Stat(areaDatei); err != nil {
		t.Errorf("areas.json fehlt: %v", err)
	}
}

// Eine BELEGTE Area zu loeschen ist ein 400, kein 500 und kein stiller Erfolg:
// die Entscheidung dieses Beads (areas.Store.Loeschen) muss den ganzen Weg
// ueberstehen — areas.ErrEingabe -> taskops.GrundEingabe -> statusFuer.
//
// Und die Meldung muss das Projekt NENNEN, sonst steht der Benutzer vor einer
// Ablehnung ohne Ausweg.
func TestBereichLoeschenBelegtIst400UndNenntDasProjekt(t *testing.T) {
	basis, _ := bereicheProbe(t, "alpha-tool", "omega-app")

	resp, rumpf := postJSON(t, basis, "/action/task/bereich", `{"projekt":"omega-app","bereich":"Arbeit"}`, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("zuordnen: Status = %d (%s)", resp.StatusCode, rumpf)
	}

	resp, rumpf = postJSON(t, basis, "/action/task/bereich/loeschen", `{"bereich":"Arbeit"}`, nil)
	m := alsJSON(t, resp, rumpf)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("Status = %d, erwartet 400 (%s)", resp.StatusCode, rumpf)
	}
	if m["grund"] != "eingabe" {
		t.Errorf("grund = %v, erwartet \"eingabe\"", m["grund"])
	}
	if s, _ := m["fehler"].(string); !strings.Contains(s, "omega-app") {
		t.Errorf("Meldung %q nennt das Projekt nicht, das im Weg steht", s)
	}

	// Die Zuordnung steht noch — eine abgelehnte Operation darf nichts
	// angefasst haben.
	resp, rumpf = postJSON(t, basis, "/action/task/bereich/anlegen", `{"bereich":"Arbeit"}`, nil)
	m = alsJSON(t, resp, rumpf)
	if got, want := gruppenText(t, m), "Arbeit: omega-app\nOhne Bereich: alpha-tool\n"; got != want {
		t.Errorf("nach der Ablehnung:\n--- bekommen ---\n%s--- erwartet ---\n%s", got, want)
	}
}

// Die Sammel-Area ist kein Loeschziel. Der Browser bietet sie nicht an — die
// Route muss es trotzdem ablehnen: ein veralteter Stand oder ein Aufruf von
// Hand darf nicht dazu fuehren, dass "Ohne Bereich" als echter Name in der
// Datei landet (derselbe wartende Defekt, den gkp-8ww fuer SetArea geheilt hat).
func TestBereichLoeschenLehntDieSammelAreaAb(t *testing.T) {
	basis, _ := bereicheProbe(t, "alpha-tool")

	resp, rumpf := postJSON(t, basis, "/action/task/bereich/loeschen", `{"bereich":"Ohne Bereich"}`, nil)
	m := alsJSON(t, resp, rumpf)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("Status = %d, erwartet 400 (%s)", resp.StatusCode, rumpf)
	}
	if m["grund"] != "eingabe" {
		t.Errorf("grund = %v, erwartet \"eingabe\"", m["grund"])
	}
}
