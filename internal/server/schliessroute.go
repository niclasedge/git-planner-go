package server

// POST /action/task/schliessen — die Ansicht schliesst einen Bead (gkp-bq9).
//
// Diese Route ist die ZWEITE Tuer zum Belegtor; die erste ist der MCP-Endpunkt.
// Beide gehen durch dasselbe internal/closeops, und das ist der ganze Punkt
// dieser Datei: hier steht keine Zeile, die entscheidet, ob ein Beleg zaehlt.
// Gaebe es hier eine zweite, mildere Pruefung, waere nicht nur die naechste
// Schliessung unbelegt — es waere jeder done-Beleg im Store entwertet, weil
// niemand mehr unterscheiden koennte, welcher geprueft wurde.
//
// Warum eine eigene Datei und nicht ein fuenfter Fall in taskroutes.go: die
// vier Operationen dort uebersetzen einen Rumpf und reichen ihn weiter. Diese
// hier nimmt zusaetzlich BYTES entgegen (siehe bildAblegen) und hat mit
// "kein Beleg" und "kein Bild" zwei Fehlerfaelle, die es sonst nirgends gibt —
// sie tragen eine Bead-Liste, die fehlerAntwort nicht kennt. Die Helfer von
// dort (rumpfLesen, csrfOk, writeJSONStatus) werden geteilt, nicht kopiert.
//
// Zwei Rumpfformen, dieselbe Route:
//
//   - application/json  — der Auftrag allein. Der Beleg liegt entweder schon im
//     Store, oder er wird als Pfad mitgegeben. Das ist der Weg, den auch ein
//     Skript nehmen kann.
//   - multipart/form-data — Feld "auftrag" traegt dasselbe JSON, Feld "bild"
//     die Bytes eines eingefuegten oder fallengelassenen Screenshots. Der
//     Browser hat keinen Dateipfad zu bieten, den dieser Dienst oeffnen
//     koennte, also muss der Inhalt selbst mitkommen.
//
// Der DIALOG dazu (Drag-and-drop, Zwischenablage, Vorab-Anzeige) gehoert nicht
// hierher, sondern in die Ansicht (gkp-7gb). Diese Route ist seine Go-Seite.

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/niclasedge/git-planner-go/internal/anzeigezeit"
	"github.com/niclasedge/git-planner-go/internal/appctx"
	"github.com/niclasedge/git-planner-go/internal/bdops"
	"github.com/niclasedge/git-planner-go/internal/cache"
	"github.com/niclasedge/git-planner-go/internal/closeops"
	"github.com/niclasedge/git-planner-go/internal/reports"
)

// schliessPfad ist die exakte Route. Sie liegt unter demselben /action/-Dach
// wie die vier Schreiboperationen — damit im selben Schatten des Netz-Gates in
// server.go, das an der METHODE haengt und jede kuenftige Nicht-GET-Route
// mitdeckt.
const schliessPfad = taskPraefix + "schliessen"

// maxBildBytes deckelt den gesamten multipart-Rumpf. reports' eigene Grenze
// fuer ein einzelnes Bild liegt bei 10 MiB; hier steht etwas mehr, damit ein
// Bild knapp unter der Grenze samt Formular-Rahmen noch durchkommt und die
// Ablehnung DORT faellt, wo die Begruendung steht.
//
// Ein zu grosses Bild braucht hier keinen Sonderfall: der Store markiert es als
// "missing", und das Tor lehnt daraufhin ab. Der Fehlerfall faellt also auf die
// sichere Seite, ohne dass diese Datei die Grenze kennen muesste.
const maxBildBytes = 12 << 20

// schliessAntwort ist der Fehlerrumpf dieser Route.
//
// Beads ist der Grund, warum sie nicht fehlerAntwort benutzt: bei mehreren
// Beads in einem Aufruf sagt erst diese Liste, WELCHE den Beleg schuldig
// geblieben sind. Ein Dialog, der stattdessen alle Zeilen rot faerbt, waere
// falsch.
type schliessAntwort struct {
	Fehler  string         `json:"fehler"`
	Grund   string         `json:"grund,omitempty"`
	Beads   []string       `json:"beads,omitempty"`
	Aufrufe []bdops.Result `json:"aufrufe,omitempty"`
}

// schliessErfolg ist der Erfolgsrumpf: closeops' Ergebnis plus die neu
// getroffene Auswahl der Aufgabenlisten (gkp-7gb).
//
// Anders als bei den vier Operationen in taskroutes.go gibt es hier KEINEN
// Teilbaum, und das ist kein Versaeumnis: ein Auftrag schliesst bis zu mehrere
// Beads (closeops.Auftrag.Wuensche), es gibt also nicht DEN einen Ast. Die
// Listen dagegen sind ohnehin eine Auswahl ueber alles und beantworten die
// Frage, die nach dem Schliessen ansteht — die Zeile ist weg, welche stehen
// noch? Wer die geschlossene Zeile selbst neu zeichnen will, laedt die Karte
// neu; das ist der seltene Weg und kostet eine Sekunde, keine Datenluecke.
type schliessErfolg struct {
	closeops.Ergebnis
	Ansichten map[string]appctx.AufgabenAnsicht `json:"ansichten"`
}

type schliessRoute struct{ dienst *closeops.Dienst }

func neueSchliessRoute() *schliessRoute { return &schliessRoute{dienst: closeops.Neu()} }

// schliessStatus bildet closeops' Grund auf HTTP ab.
//
// Die beiden Belegfaelle sind 422 und nicht 400, und der Unterschied ist keine
// Kosmetik: an der Anfrage ist nichts falsch. Sie ist wohlgeformt, der Bead
// existiert, der Aufrufer hat alles richtig getippt — was fehlt, ist ein Bild
// im Report-Store. 400 hiesse "schick etwas anderes"; 422 heisst "schick es
// noch einmal, wenn der Beleg steht". Nur die zweite Lesart stimmt.
//
// GrundStore ist 500: der Store ist Sache dieses Dienstes, nicht des Aufrufers.
// Die bd-Gruende folgen der Abbildung aus taskroutes.go — "not-found" heisst
// dort "kein bd auf dem PATH", nicht "kein solcher Bead", und ist deshalb ein
// Serverfehler.
func schliessStatus(err error) int {
	switch closeops.GrundVon(err) {
	case closeops.GrundEingabe:
		return http.StatusBadRequest
	case closeops.GrundKeinBeleg, closeops.GrundKeinBild:
		return http.StatusUnprocessableEntity
	case bdops.ReasonExit:
		return http.StatusUnprocessableEntity
	case bdops.ReasonTimeout:
		return http.StatusGatewayTimeout
	default:
		return http.StatusInternalServerError
	}
}

func (s *schliessRoute) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeJSONStatus(w, http.StatusMethodNotAllowed, schliessAntwort{
			Fehler: fmt.Sprintf("%s wird hier nicht bedient: diese Route schreibt, also nur POST", r.Method)})
		return
	}
	if !csrfOkFuer(r) {
		writeJSONStatus(w, http.StatusForbidden, schliessAntwort{Fehler: "CSRF: fremde Origin blockiert"})
		return
	}

	auftrag, aufraeumen, err := auftragLesen(w, r)
	// aufraeumen loescht die abgelegten Bilder. Es laeuft AUCH im Fehlerfall
	// und erst NACH dem Schliessen: der Report-Store kopiert die Datei in sein
	// assets/-Verzeichnis, bis dahin muss sie da sein.
	defer aufraeumen()
	if err != nil {
		writeJSONStatus(w, http.StatusBadRequest, schliessAntwort{
			Fehler: err.Error(), Grund: closeops.GrundEingabe})
		return
	}

	erg, err := s.dienst.Schliessen(auftrag, nil)

	// Invalidiert wird auch im Fehlerfall: ein Teilfehlschlag hat den Stand
	// bereits geaendert (Beleg geschrieben, erste Gruppe geschlossen, zweite
	// nicht). Einmal zu viel kostet einen Neuaufbau, einmal zu wenig zeigt der
	// Ansicht einen Stand, den es nicht mehr gibt.
	if auftrag.Pfad != "" {
		cache.InvalidateProjectContext(auftrag.Pfad)
	}

	if err != nil {
		writeJSONStatus(w, schliessStatus(err), schliessAntwort{
			Fehler: err.Error(), Grund: closeops.GrundVon(err),
			Beads: closeops.BeadsVon(err), Aufrufe: erg.Aufrufe})
		return
	}
	writeJSONStatus(w, http.StatusOK, schliessErfolg{
		Ergebnis:  erg,
		Ansichten: appctx.AnsichtenNach(auftrag.Pfad, anzeigezeit.Jetzt()),
	})
}

// auftragLesen liest beide Rumpfformen und liefert dazu die Aufraeumfunktion
// fuer abgelegte Bilder. Die Funktion ist nie nil, auch nicht im Fehlerfall —
// sonst muesste jeder Rueckgabepfad oben daran denken.
func auftragLesen(w http.ResponseWriter, r *http.Request) (closeops.Auftrag, func(), error) {
	if typ, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err == nil && typ == "multipart/form-data" {
		return auftragMitBild(w, r)
	}
	a, err := rumpfLesen[closeops.Auftrag](w, r)
	return a, func() {}, err
}

// auftragMitBild liest die Formularform und haengt die hochgeladenen Bytes als
// Bildbeleg an den Bericht.
func auftragMitBild(w http.ResponseWriter, r *http.Request) (closeops.Auftrag, func(), error) {
	var a closeops.Auftrag
	nichts := func() {}
	r.Body = http.MaxBytesReader(w, r.Body, maxBildBytes)
	if err := r.ParseMultipartForm(maxBildBytes); err != nil {
		return a, nichts, fmt.Errorf("formular unlesbar: %w", err)
	}
	roh := r.MultipartForm.Value["auftrag"]
	if len(roh) != 1 {
		return a, nichts, errors.New(`das Feld "auftrag" fehlt oder kommt mehrfach: es trägt den Auftrag als JSON`)
	}
	dec := json.NewDecoder(strings.NewReader(roh[0]))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&a); err != nil {
		return a, nichts, fmt.Errorf("auftrag unlesbar: %w", err)
	}

	bilder := r.MultipartForm.File["bild"]
	if len(bilder) == 0 {
		return a, nichts, nil
	}
	// Ein Bild gehoert zu EINEM Bead. Bei mehreren Wuenschen waere die
	// Zuordnung geraten — und ein geratener Beleg belegt nichts. Wer mehrere
	// Beads auf einmal schliesst, gibt die Belege als Pfade mit (JSON-Form).
	if len(a.Wuensche) != 1 {
		return a, nichts, fmt.Errorf("ein Bild lässt sich nur einem einzelnen Bead zuordnen, "+
			"der Auftrag nennt aber %d", len(a.Wuensche))
	}
	if a.Wuensche[0].Bericht == nil {
		return a, nichts, errors.New(`ein Bild braucht einen Bericht, an den es gehängt werden kann ` +
			`("bericht": {"titel": "…"})`)
	}

	ordner, err := os.MkdirTemp("", "gkp-beleg-")
	if err != nil {
		return a, nichts, fmt.Errorf("kein Ablageort für das Bild: %w", err)
	}
	aufraeumen := func() { os.RemoveAll(ordner) }
	for i, kopf := range bilder {
		pfad, err := bildAblegen(ordner, i, kopf)
		if err != nil {
			aufraeumen()
			return a, nichts, err
		}
		// Angehaengt, nicht ersetzt: ein mitgegebener {cmd,out}-Beleg bleibt
		// erhalten. Er zaehlt nur nicht allein.
		a.Wuensche[0].Bericht.Belege = append(a.Wuensche[0].Bericht.Belege, reports.ProofItem{"image": pfad})
	}
	return a, aufraeumen, nil
}

// bildAblegen schreibt einen hochgeladenen Teil in eine Datei und gibt deren
// Pfad zurueck.
//
// Was hier bewusst NICHT passiert: eine Pruefung, ob die Bytes ein Bild sind.
// Die steht in internal/reports (Magic-Bytes, Groessengrenze) und entscheidet
// dort ueber "missing": true. Sie hier vorwegzunehmen hiesse, zwei Stellen
// ueber dieselbe Frage urteilen zu lassen — und die mildere von beiden waere
// dann das eigentliche Tor.
//
// Vom Dateinamen des Browsers wird nur die Endung uebernommen, und auch die
// nur, wenn sie auf einer kurzen Liste steht (siehe endungVon). Der Name kommt
// aus einem Formularfeld, und er ueberlebt diesen Dienst: reports.Append baut
// den Zielnamen in assets/ aus filepath.Ext der Quelle.
func bildAblegen(ordner string, i int, kopf *multipart.FileHeader) (string, error) {
	quelle, err := kopf.Open()
	if err != nil {
		return "", fmt.Errorf("bild %d nicht lesbar: %w", i, err)
	}
	defer quelle.Close()
	pfad := filepath.Join(ordner, fmt.Sprintf("beleg-%d%s", i, endungVon(kopf.Filename)))
	ziel, err := os.Create(pfad)
	if err != nil {
		return "", fmt.Errorf("bild %d nicht ablegbar: %w", i, err)
	}
	defer ziel.Close()
	if _, err := io.Copy(ziel, quelle); err != nil {
		return "", fmt.Errorf("bild %d nicht vollständig abgelegt: %w", i, err)
	}
	return pfad, nil
}

// endungVon liefert die Dateiendung, die aus einem hochgeladenen Namen
// uebernommen werden darf: eine der vier Formen, die der Report-Store
// ueberhaupt als Bild akzeptiert. Alles andere wird zu ".png" — die Endung
// entscheidet dort ohnehin nichts (die Magic-Bytes tun es), sie ist nur der
// Name, unter dem der Beleg spaeter im Store liegt.
func endungVon(name string) string {
	endung := strings.ToLower(filepath.Ext(filepath.Base(name)))
	switch endung {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp":
		return endung
	default:
		return ".png"
	}
}
