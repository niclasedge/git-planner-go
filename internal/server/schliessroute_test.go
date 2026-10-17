package server

// Tests fuer POST /action/task/schliessen (gkp-bq9).
//
// Sie stehen IM Paket server und nicht daneben, aus einem Grund: nur so laesst
// sich die bd-Tuer durch eine Attrappe ersetzen. Ein Test, der die Route ueber
// NewHandler ansteuert, kann das Tor nur bis zur Ablehnung pruefen — was
// DAHINTER passiert, wenn der Beleg reicht, waere ohne Attrappe ein echtes
// `bd close` in einer echten Datenbank.

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/niclasedge/git-planner-go/internal/bdops"
	"github.com/niclasedge/git-planner-go/internal/closeops"
	"github.com/niclasedge/git-planner-go/internal/collectors"
	"github.com/niclasedge/git-planner-go/internal/reports"
)

// ---------------------------------------------------------------- gerüst

type schliessAttrappe struct{ ids []string }

func (s *schliessAttrappe) Close(pfad string, ids []string, grund string, erzwingen bool) (bdops.Result, error) {
	s.ids = append(s.ids, ids...)
	return bdops.Result{}, nil
}

// PruefeKartenzugehoerigkeit laesst hier jede Karte durch (gkp-few). Diese Tests
// pruefen das BELEG-Tor; die Kartenpruefung hat ihre eigenen, in
// internal/closeops und internal/bdops, und braeuchte hier eine echte
// bd-Datenbank — genau das, wogegen diese Attrappe steht.
func (s *schliessAttrappe) PruefeKartenzugehoerigkeit(pfad, karte string, beads []string) (bdops.Result, error) {
	return bdops.Result{}, nil
}

// belegStore richtet einen isolierten Report-Store ein und liefert das
// Projektverzeichnis, das die Route als "pfad" bekommt.
func belegStore(t *testing.T) (pfad, jsonl string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("CC_REPORTS_DIR", dir)
	pfad = t.TempDir()
	repo := collectors.ReportsRepoName(pfad)
	if err := os.MkdirAll(filepath.Join(dir, repo), 0o755); err != nil {
		t.Fatal(err)
	}
	return pfad, filepath.Join(dir, repo, "karte.jsonl")
}

func belegZeile(t *testing.T, jsonl, bead string, belege ...map[string]any) {
	t.Helper()
	line, err := json.Marshal(reports.Entry{
		ID: "e1", At: "2026-09-13T10:00:00Z", Map: "karte", Bead: bead,
		Kind: "done", Title: "fertig", Proof: belege,
	})
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(jsonl, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		t.Fatal(err)
	}
}

// ruf schickt eine Anfrage an die Route und gibt Antwort und gedeuteten Rumpf
// zurueck.
func ruf(t *testing.T, bd closeops.Schliesser, req *http.Request) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	rec := httptest.NewRecorder()
	(&schliessRoute{dienst: closeops.NeuMit(bd)}).ServeHTTP(rec, req)
	var rumpf map[string]any
	if rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), &rumpf); err != nil {
			t.Fatalf("Antwort ist kein JSON: %s", rec.Body.String())
		}
	}
	return rec, rumpf
}

func jsonAnfrage(rumpf string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, schliessPfad, strings.NewReader(rumpf))
	req.Header.Set("Content-Type", "application/json")
	return req
}

// bildAnfrage baut die Form, die der Dialog schickt: der Auftrag als JSON-Feld,
// das Bild als Datei daneben.
func bildAnfrage(t *testing.T, auftrag string, dateiname string, inhalt []byte) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if err := mw.WriteField("auftrag", auftrag); err != nil {
		t.Fatal(err)
	}
	if dateiname != "" {
		teil, err := mw.CreateFormFile("bild", dateiname)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := teil.Write(inhalt); err != nil {
			t.Fatal(err)
		}
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, schliessPfad, &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return req
}

var pngBytes = []byte("\x89PNG\r\n\x1a\nirgendwelche Bilddaten")

// ---------------------------------------------------------------- tor

// TestRouteOhneBelegAbgelehnt: das Haekchen im Browser geht nicht am Tor
// vorbei. 422 und nicht 400 — an der Anfrage ist nichts falsch, es fehlt ein
// Beleg im Store.
func TestRouteOhneBelegAbgelehnt(t *testing.T) {
	pfad, _ := belegStore(t)
	bd := &schliessAttrappe{}
	rec, rumpf := ruf(t, bd, jsonAnfrage(`{"pfad":`+q(pfad)+`,"karte":"karte","wuensche":[{"bead":"karte.1"}]}`))

	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("Status = %d, erwartet 422 (%s)", rec.Code, rec.Body)
	}
	if rumpf["grund"] != closeops.GrundKeinBeleg {
		t.Errorf("grund = %v, erwartet %q", rumpf["grund"], closeops.GrundKeinBeleg)
	}
	if beads, _ := rumpf["beads"].([]any); len(beads) != 1 || beads[0] != "karte.1" {
		t.Errorf("beads = %v, erwartet [karte.1]", rumpf["beads"])
	}
	if len(bd.ids) != 0 {
		t.Fatalf("bd lief trotz Ablehnung: %v", bd.ids)
	}
}

// TestRouteNurTextAbgelehnt: eigene Meldung, eigener Grund — der Dialog soll
// "Bild nachreichen" sagen koennen und nicht "Bericht schreiben".
func TestRouteNurTextAbgelehnt(t *testing.T) {
	pfad, jsonl := belegStore(t)
	belegZeile(t, jsonl, "karte.1", map[string]any{"cmd": "go test ./...", "out": "ok"})
	bd := &schliessAttrappe{}
	rec, rumpf := ruf(t, bd, jsonAnfrage(`{"pfad":`+q(pfad)+`,"karte":"karte","wuensche":[{"bead":"karte.1"}]}`))

	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("Status = %d, erwartet 422 (%s)", rec.Code, rec.Body)
	}
	if rumpf["grund"] != closeops.GrundKeinBild {
		t.Errorf("grund = %v, erwartet %q", rumpf["grund"], closeops.GrundKeinBild)
	}
	if len(bd.ids) != 0 {
		t.Fatalf("bd lief trotz Ablehnung: %v", bd.ids)
	}
}

// TestRouteMitBestehendemBildSchliesst: liegt der Beleg schon im Store, reicht
// das Haekchen allein.
func TestRouteMitBestehendemBildSchliesst(t *testing.T) {
	pfad, jsonl := belegStore(t)
	belegZeile(t, jsonl, "karte.1", map[string]any{"file": "docs/plan.dot"})
	bd := &schliessAttrappe{}
	rec, rumpf := ruf(t, bd, jsonAnfrage(
		`{"pfad":`+q(pfad)+`,"karte":"karte","wuensche":[{"bead":"karte.1","grund":"gebaut"}]}`))

	if rec.Code != http.StatusOK {
		t.Fatalf("Status = %d (%s)", rec.Code, rec.Body)
	}
	if len(bd.ids) != 1 || bd.ids[0] != "karte.1" {
		t.Fatalf("bd close = %v", bd.ids)
	}
	if g, _ := rumpf["geschlossen"].([]any); len(g) != 1 {
		t.Errorf("geschlossen = %v", rumpf["geschlossen"])
	}
}

// ---------------------------------------------------------------- bild

// TestRouteNimmtBildEntgegen ist der Teil, den der Dialog braucht: das Bild
// kommt als BYTES aus dem Browser (eingefuegt oder fallengelassen), nicht als
// Pfad. Die Route legt es ab, reicht den Pfad an den Report-Store weiter — und
// erst der entscheidet, ob es zaehlt.
func TestRouteNimmtBildEntgegen(t *testing.T) {
	pfad, _ := belegStore(t)
	bd := &schliessAttrappe{}
	auftrag := `{"pfad":` + q(pfad) + `,"karte":"karte","wuensche":[{"bead":"karte.1","grund":"gebaut",` +
		`"bericht":{"titel":"Dialog steht"}}]}`
	rec, _ := ruf(t, bd, bildAnfrage(t, auftrag, "schirm.png", pngBytes))

	if rec.Code != http.StatusOK {
		t.Fatalf("Status = %d (%s)", rec.Code, rec.Body)
	}
	if len(bd.ids) != 1 || bd.ids[0] != "karte.1" {
		t.Fatalf("bd close = %v", bd.ids)
	}
	// Der Beleg muss im Store stehen, nicht nur in der Antwort.
	e, _, err := reports.LoadMap(collectors.ReportsRepoName(pfad), "karte")
	if err != nil {
		t.Fatal(err)
	}
	if !reports.HasValidatedDoneEntry(e) {
		t.Fatalf("der abgelegte Beleg haelt der Pruefung nicht stand: %+v", e)
	}
}

// TestRouteLehntFremdeBytesAb: was der Browser hochlaedt, ist nicht schon
// deshalb ein Bild, weil es "bild" heisst. Die Pruefung dafuer steht in
// internal/reports (Magic-Bytes) — dieser Test haelt fest, dass die Route sie
// nicht umgeht, indem sie den Beleg selbst zusammensetzt.
func TestRouteLehntFremdeBytesAb(t *testing.T) {
	pfad, _ := belegStore(t)
	bd := &schliessAttrappe{}
	auftrag := `{"pfad":` + q(pfad) + `,"karte":"karte","wuensche":[{"bead":"karte.1",` +
		`"bericht":{"titel":"Dialog steht"}}]}`
	rec, rumpf := ruf(t, bd, bildAnfrage(t, auftrag, "schirm.png", []byte("PRIVATE KEY, kein Bild")))

	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("Status = %d, erwartet 422 (%s)", rec.Code, rec.Body)
	}
	if rumpf["grund"] != closeops.GrundKeinBild {
		t.Errorf("grund = %v, erwartet %q", rumpf["grund"], closeops.GrundKeinBild)
	}
	if len(bd.ids) != 0 {
		t.Fatalf("bd lief mit einem Nicht-Bild als Beleg: %v", bd.ids)
	}
}

// TestRouteBildBrauchtEinenEindeutigenBead: ein Bild gehoert zu EINEM Bead. Bei
// mehreren waere die Zuordnung geraten, und ein geratener Beleg ist keiner.
func TestRouteBildBrauchtEinenEindeutigenBead(t *testing.T) {
	pfad, _ := belegStore(t)
	bd := &schliessAttrappe{}
	auftrag := `{"pfad":` + q(pfad) + `,"karte":"karte","wuensche":[` +
		`{"bead":"karte.1","bericht":{"titel":"a"}},{"bead":"karte.2","bericht":{"titel":"b"}}]}`
	rec, _ := ruf(t, bd, bildAnfrage(t, auftrag, "schirm.png", pngBytes))

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, erwartet 400 (%s)", rec.Code, rec.Body)
	}
	if len(bd.ids) != 0 {
		t.Fatalf("bd lief: %v", bd.ids)
	}
}

// TestRouteBildBrauchtEinenBericht: ohne Bericht gibt es keinen Eintrag, an
// den das Bild gehen koennte — der Report-Store verlangt einen Titel.
func TestRouteBildBrauchtEinenBericht(t *testing.T) {
	pfad, _ := belegStore(t)
	bd := &schliessAttrappe{}
	auftrag := `{"pfad":` + q(pfad) + `,"karte":"karte","wuensche":[{"bead":"karte.1"}]}`
	rec, _ := ruf(t, bd, bildAnfrage(t, auftrag, "schirm.png", pngBytes))

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, erwartet 400 (%s)", rec.Code, rec.Body)
	}
	if len(bd.ids) != 0 {
		t.Fatalf("bd lief: %v", bd.ids)
	}
}

// TestRouteLaesstKeineBilddatei zurueck: die hochgeladenen Bytes liegen nur so
// lange auf der Platte, wie der Report-Store braucht, um sie nach assets/ zu
// kopieren.
func TestRouteLaesstKeineBilddateiZurueck(t *testing.T) {
	pfad, _ := belegStore(t)
	bd := &schliessAttrappe{}
	auftrag := `{"pfad":` + q(pfad) + `,"karte":"karte","wuensche":[{"bead":"karte.1",` +
		`"bericht":{"titel":"Dialog steht"}}]}`
	if rec, _ := ruf(t, bd, bildAnfrage(t, auftrag, "schirm.png", pngBytes)); rec.Code != http.StatusOK {
		t.Fatalf("Status = %d (%s)", rec.Code, rec.Body)
	}
	e, _, err := reports.LoadMap(collectors.ReportsRepoName(pfad), "karte")
	if err != nil || len(e) != 1 {
		t.Fatalf("Eintraege = %+v (%v)", e, err)
	}
	abgelegt, _ := e[0].Proof[0]["image"].(string)
	if abgelegt == "" {
		t.Fatalf("kein Bildpfad im Eintrag: %+v", e[0].Proof)
	}
	if !strings.Contains(abgelegt, "assets") {
		t.Errorf("Bild liegt nicht im Report-Store, sondern unter %q", abgelegt)
	}
	if _, err := os.Stat(filepath.Dir(filepath.Dir(abgelegt))); err != nil {
		t.Errorf("Store-Verzeichnis fehlt: %v", err)
	}
}

// ---------------------------------------------------------------- rahmen

func TestRouteNurPost(t *testing.T) {
	bd := &schliessAttrappe{}
	rec, _ := ruf(t, bd, httptest.NewRequest(http.MethodGet, schliessPfad, nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("Status = %d, erwartet 405", rec.Code)
	}
}

func TestRouteFremdeOriginBlockiert(t *testing.T) {
	req := jsonAnfrage(`{"pfad":"/tmp","karte":"karte","wuensche":[{"bead":"karte.1"}]}`)
	req.Header.Set("Origin", "https://boese.example")
	bd := &schliessAttrappe{}
	rec, _ := ruf(t, bd, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("Status = %d, erwartet 403", rec.Code)
	}
	if len(bd.ids) != 0 {
		t.Fatalf("bd lief: %v", bd.ids)
	}
}

// TestRouteUnbekanntesFeld: DisallowUnknownFields, aus demselben Grund wie in
// taskroutes.go — ein verworfenes Feld ist eine Falschmeldung. Hier waere der
// Schaden groesser als dort: "belege" statt "bericht" getippt, und der Aufruf
// haette gar keinen Beleg mitgeschickt.
func TestRouteUnbekanntesFeld(t *testing.T) {
	bd := &schliessAttrappe{}
	rec, _ := ruf(t, bd, jsonAnfrage(`{"pfad":"/tmp","karte":"karte","beads":["karte.1"]}`))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, erwartet 400 (%s)", rec.Code, rec.Body)
	}
}

// TestRouteIstVerdrahtet: die Route haengt am Handler und nicht nur im Test.
// Dieser Test geht ueber NewHandler, also durch das ECHTE closeops mit dem
// echten bd — eine Attrappe gibt es hier absichtlich nicht, sonst pruefte er
// die Verdrahtung nicht.
//
// Bis gkp-few war die erste Antwort die des Beleg-Tors ("kein Beleg"). Sie ist
// es nicht mehr, und das ist kein Bruch, sondern die neue Reihenfolge: seit
// gkp-few fragt der Dienst ZUERST, ob Karte und Bead zusammengehoeren, und das
// Wegwerf-Verzeichnis dieses Tests hat gar keine beads-Datenbank. Die Route
// antwortet also mit der Ablehnung des Kartentors — was sie genauso gut
// verdrahtet zeigt, denn eine nicht angehaengte Route antwortete mit 404.
//
// Die Anfrage traegt eine Loopback-Adresse im Kontext, weil das Netz-Gate in
// server.go VOR jedem Pfad greift: ohne sie waere die Antwort 403 und der Test
// wuerde die Verdrahtung gar nicht erreichen.
//
// Weil er den echten bd braucht, ueberspringt er sich ohne bd (#7): in einer
// Cloud-Sitzung ist bd nicht installiert, und die Route antwortete dort mit
// 500 "bd exit -1" — ein Befund ueber die Umgebung, nicht ueber die Route.
func TestRouteIstVerdrahtet(t *testing.T) {
	if _, err := exec.LookPath("bd"); err != nil {
		t.Skipf("bd nicht im PATH (%v) — dieser Test prueft die Verdrahtung mit dem echten bd", err)
	}
	pfad, _ := belegStore(t)
	h := NewHandler(AppOptions{Cwd: t.TempDir()})
	rec := httptest.NewRecorder()
	req := jsonAnfrage(`{"pfad":` + q(pfad) + `,"karte":"karte","wuensche":[{"bead":"karte.1"}]}`)
	ctx := context.WithValue(req.Context(), http.LocalAddrContextKey,
		net.Addr(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8765}))
	h.ServeHTTP(rec, req.WithContext(ctx))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("Status = %d, erwartet 422 — haengt die Route am Handler? (%s)", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "kartenzugehörigkeit") {
		t.Errorf("Antwort nennt den Grund nicht: %s", rec.Body)
	}
}

func q(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// TestRouteErfolgTraegtDieAufgabenAnsichten (gkp-7gb): nach dem Schliessen muss
// die Ansicht wissen, welche Zeilen noch stehen — und das ist eine AUSWAHL, die
// nach Go gehoert (CLAUDE.md). Ohne dieses Feld muesste der Browser nach jedem
// Schliessen entweder alles neu laden oder die Zeile selbst wegrechnen.
//
// Der Pfad hier ist ein Wegwerfverzeichnis ohne .beads/, die Listen sind also
// leer. Genau das ist die zweite Zusage dieses Tests: LEER und nicht FEHLEND.
// Ein fehlendes Feld waere im Browser nicht von "keine Aufgabe mehr offen" zu
// unterscheiden, und die Liste bliebe unbegruendet stehen.
func TestRouteErfolgTraegtDieAufgabenAnsichten(t *testing.T) {
	pfad, jsonl := belegStore(t)
	belegZeile(t, jsonl, "karte.1", map[string]any{"file": "docs/plan.dot"})
	bd := &schliessAttrappe{}
	rec, rumpf := ruf(t, bd, jsonAnfrage(
		`{"pfad":`+q(pfad)+`,"karte":"karte","wuensche":[{"bead":"karte.1","grund":"gebaut"}]}`))

	if rec.Code != http.StatusOK {
		t.Fatalf("Status = %d (%s)", rec.Code, rec.Body)
	}
	ansichten, ok := rumpf["ansichten"].(map[string]any)
	if !ok {
		t.Fatalf("ansichten = %v (%T), erwartet ein Objekt", rumpf["ansichten"], rumpf["ansichten"])
	}
	heute, ok := ansichten["heute"].(map[string]any)
	if !ok {
		t.Fatalf("ansichten.heute fehlt — die angemeldeten Regeln kommen nicht durch: %v", ansichten)
	}
	if _, ok := heute["eintraege"].([]any); !ok {
		t.Errorf("ansichten.heute.eintraege = %v, erwartet eine (leere) Liste", heute["eintraege"])
	}
	if heute["tag"] == "" || heute["tag"] == nil {
		t.Errorf("ansichten.heute.tag fehlt — die Ansicht muss sagen koennen, WELCHES heute sie zeigt")
	}
}
