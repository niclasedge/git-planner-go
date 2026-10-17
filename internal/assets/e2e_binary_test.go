package assets_test

// End-to-End-Beleg fuer gkp-a1k: das Svelte-Bundle wird aus einem TATSAECHLICH
// gebauten und gestarteten Binary ueber HTTP ausgeliefert — nicht bloss aus
// einem httptest-Handler im selben Prozess.
//
// Warum das zusaetzlich zum Routen-Test in internal/server noetig ist: der
// Routen-Test laeuft im Testbinary, und das Testbinary embeddet zwangslaeufig
// dasselbe, was der Test prueft. Er kann deshalb nicht ausschliessen, dass die
// ECHTE Auslieferung an etwas anderem haengt — an einer Flag-Kombination, an
// einem Arbeitsverzeichnis, an einer Datei neben dem Binary. Dieser Test baut
// `go build .` und startet das Ergebnis in einem leeren Verzeichnis: laeuft es
// dort, braucht der Dienst zur Laufzeit weder das Repo noch Node.
//
// Der Test ueberspringt sich selbst unter -short, weil er ein volles
// `go build` kostet.

import (
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/niclasedge/git-planner-go/internal/assets"
)

const marker = "gkp-a1k Svelte-Bauschiene"

// freiePortnummer laesst das Betriebssystem einen freien Port waehlen und gibt
// ihn sofort wieder frei. Ein fest verdrahteter Port waere genau die Art
// stiller Flake, die man erst in drei Monaten auf einer anderen Maschine sieht.
func freiePortnummer(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("kein freier Port: %v", err)
	}
	defer l.Close()
	_, port, err := net.SplitHostPort(l.Addr().String())
	if err != nil {
		t.Fatalf("Adresse %q nicht zerlegbar: %v", l.Addr(), err)
	}
	return port
}

func TestGebautesBinaryLiefertSvelteBundleAus(t *testing.T) {
	if testing.Short() {
		t.Skip("-short: der Test baut das Binary")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("kein go im PATH")
	}

	repo, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("Repo-Pfad: %v", err)
	}
	bin := filepath.Join(t.TempDir(), "gkp-e2e")

	build := exec.Command("go", "build", "-o", bin, ".")
	build.Dir = repo
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}

	// Startverzeichnis bewusst LEER und ausserhalb des Repos: haenge die
	// Auslieferung doch an einer Datei auf der Platte, faellt es hier auf.
	arbeitsverzeichnis := t.TempDir()
	port := freiePortnummer(t)

	ctx, abbrechen := context.WithTimeout(context.Background(), 30*time.Second)
	defer abbrechen()

	dienst := exec.CommandContext(ctx, bin,
		"--host", "127.0.0.1", "--port", port, "--cwd", arbeitsverzeichnis, "--no-open")
	dienst.Dir = arbeitsverzeichnis
	// Ein PATH ohne Node — dieselbe Randbedingung, unter der die systemd-Unit
	// laeuft (siehe den PATH-Block bei install-unit im justfile).
	//
	// CC_KNOWLEDGE_CACHE und CC_KNOWLEDGE_ROOTS muessen mit, sonst schreibt das
	// gestartete Binary in die ECHTE Cache-Datei des Benutzers
	// (~/.cache/cc-knowledge/cache.json) und traegt dort das Wegwerfverzeichnis
	// dieses Tests als einziges Projekt ein. Gemessen am 2026-09-13: danach
	// zeigte der laufende Dienst genau ein Projekt namens "002" aus einem
	// /tmp-Pfad, der zu dem Zeitpunkt schon geloescht war — und zwar hartnaeckig,
	// weil StartCache eine Datei innerhalb der TTL uebernimmt, OHNE neu zu
	// rechnen (cache.go:166). Ein Test darf den Zustand der Maschine nicht
	// veraendern, auf der er laeuft.
	cacheDatei := filepath.Join(t.TempDir(), "cache.json")
	dienst.Env = append(os.Environ(),
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin",
		"CC_KNOWLEDGE_CACHE="+cacheDatei,
		"CC_KNOWLEDGE_ROOTS="+arbeitsverzeichnis)
	if err := dienst.Start(); err != nil {
		t.Fatalf("Dienst startet nicht: %v", err)
	}
	t.Cleanup(func() {
		_ = dienst.Process.Kill()
		_ = dienst.Wait()
	})

	// Warten, bis der Listener steht. Der Dienst braucht dafuer keinen
	// Fortschrittsbericht — ein Abruf, der durchgeht, IST der Beleg.
	url := "http://127.0.0.1:" + port + "/assets/app/app.js"
	var antwort *http.Response
	for versuch := 0; versuch < 100; versuch++ {
		antwort, err = http.Get(url)
		if err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("Dienst antwortet nach 5 s nicht auf %s: %v", url, err)
	}
	defer antwort.Body.Close()

	if antwort.StatusCode != http.StatusOK {
		t.Fatalf("Status %d, erwartet 200", antwort.StatusCode)
	}
	koerper, err := io.ReadAll(antwort.Body)
	if err != nil {
		t.Fatalf("Body: %v", err)
	}
	if !strings.Contains(string(koerper), marker) {
		t.Errorf("Marker %q fehlt in den %d ausgelieferten Bytes", marker, len(koerper))
	}
	if string(koerper) != assets.AppJS {
		t.Errorf("das Binary liefert %d Bytes, assets.AppJS hat %d", len(koerper), len(assets.AppJS))
	}

	// Randbedingung 3: die alte Ansicht steht unveraendert daneben. Wenn die
	// Seite kn-shell und window.DATA verloren haette, waere sie kaputt — und
	// zwar lautlos, weil sie trotzdem 200 liefert.
	seite, err := http.Get("http://127.0.0.1:" + port + "/ueberblick")
	if err != nil {
		t.Fatalf("Startseite: %v", err)
	}
	defer seite.Body.Close()
	html, err := io.ReadAll(seite.Body)
	if err != nil {
		t.Fatalf("Startseite lesen: %v", err)
	}
	for _, muss := range []string{"kn-shell", "window.DATA"} {
		if !strings.Contains(string(html), muss) {
			t.Errorf("die alte Ansicht hat %q verloren", muss)
		}
	}

	// Randbedingung 4: der Test hat die Maschine nicht veraendert. Ohne
	// CC_KNOWLEDGE_CACHE landet der Aggregat-Schnappschuss in
	// ~/.cache/cc-knowledge/cache.json, und der naechste echte Dienststart
	// UEBERNIMMT ihn, solange er in der TTL liegt (cache.go:166) — dann zeigt
	// das Dashboard das Wegwerfverzeichnis dieses Tests als einziges Projekt.
	//
	// Geprueft wird die Umleitung, nicht die Abwesenheit: dass die echte Datei
	// unveraendert blieb, koennte auch heissen, dass gar nichts geschrieben
	// wurde. Dass die umgeleitete Datei DA ist, beweist beides auf einmal.
	if _, err := os.Stat(cacheDatei); err != nil {
		t.Errorf("keine Cache-Datei unter CC_KNOWLEDGE_CACHE (%v) — "+
			"dann hat das Binary in die echte Datei des Benutzers geschrieben", err)
	}
}
