package server

// POST /action/task/* — die vier Schreiboperationen der Ansicht (gkp-t1o).
//
// Anders als /action/* heute antworten diese Routen mit JSON statt mit einer
// HTML-Ergebnisseite: die Ansicht ersetzt nach einem Schreibvorgang genau den
// betroffenen Teil, sie navigiert nicht weg (Spec §7.2).
//
// Die Routen bauen kein einziges bd-Flag und treffen keine Entscheidung ueber
// den Inhalt. Sie tun vier Dinge: den Rumpf in einen taskops-Wunsch lesen, den
// Grund eines Fehlers auf einen HTTP-Status abbilden, den Projekt-Cache
// invalidieren und den neu gebauten Ast der Aufgabe anhaengen (gkp-8a0). Alles
// andere steht in internal/taskops, die bd-Argumente entstehen weiterhin
// ausschliesslich in internal/bdops (docs/schreibpfad-taskops.dot), und der Ast
// wird nicht hier gebaut, sondern von appctx — derselbe Erbauer wie fuer die
// Seite, damit es keine zweite Fassung des Baums gibt (appctx/teilbaum.go).
//
// Der Draht-Vertrag IST der Go-Vertrag: die Routen lesen direkt in
// taskops.AnlegenWunsch/AendernWunsch/TerminWunsch/BereichWunsch. Damit gibt es
// keine zweite Feldliste, die neben der ersten veralten kann, und
// DisallowUnknownFields prueft genau den Vertrag, der auch fuer den Agenten
// gilt. Der Preis: die Ansicht schickt bei den drei bd-Operationen den PFAD des
// Projekts, nicht seinen Namen.
//
// Das ist Absicht. Der Pfad ist in taskops Pflicht, und die Route muss ihn
// fuehren — ihn hier aus einem Projektnamen aufzuloesen hiesse, getTarget zu
// benutzen, und getTarget faellt bei einem unbekannten Namen auf opts.Cwd
// zurueck. Ein Tippfehler im Projektnamen wuerde dann still in das Repo des
// DIENSTES schreiben (die Unit startet mit --cwd), statt zu scheitern: genau
// der Schaden, gegen den taskops' pfadPruefen gebaut ist. Ein falscher Pfad
// dagegen scheitert sichtbar. Eine neue Befugnis ist das nicht — POST /mcp
// nimmt seit jeher ein freies `cwd` entgegen und liegt hinter demselben
// Netz-Gate. Die Ansicht kennt den Pfad: er steht als ProjectRef.Path in der
// Seitenleiste, die sie ohnehin geladen hat.
//
// Die Bereich-Route ist die Ausnahme und traegt einen NAMEN — nicht aus
// Inkonsequenz, sondern weil areas.json auf den Projektnamen geschluesselt ist
// (Spec §4.3) und bd dort gar nicht vorkommt.
//
// Was hier bewusst NICHT steht: eine eigene Pruefung, ob die Verbindung von
// Loopback kommt. Das Netz-Gate in server.go haengt an der METHODE, nicht an
// einer Pfadliste — es deckt jede kuenftige Nicht-GET-Route mit ab, also auch
// diese vier. Eine zweite Pruefung daneben waere eine Kopie, die
// auseinanderlaufen kann; belegt wird die Deckung stattdessen durch einen Test
// (taskroutes_test.go, TestTaskRouteUeberNetzAdresseIstVerboten). csrfOk
// bleibt darunter als zweite Schicht: es faengt den Fall, den das Gate nicht
// sehen kann — einen Browser-Tab AUF DIESEM Rechner, der auf Loopback POSTet.

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/niclasedge/git-planner-go/internal/anzeigezeit"
	"github.com/niclasedge/git-planner-go/internal/appctx"
	"github.com/niclasedge/git-planner-go/internal/areas"
	"github.com/niclasedge/git-planner-go/internal/bdops"
	"github.com/niclasedge/git-planner-go/internal/cache"
	"github.com/niclasedge/git-planner-go/internal/collectors"
	"github.com/niclasedge/git-planner-go/internal/taskops"
)

// taskPraefix ist der gemeinsame Pfadanfang. Er steht auch deshalb unter
// /action/, damit die Routen im selben Netz-Gate-Schatten liegen wie die
// bestehenden Schreibrouten.
const taskPraefix = "/action/task/"

// maxRumpf deckelt den gelesenen Rumpf. Eine Aufgabe ist ein paar hundert
// Bytes; alles darueber ist keine Aufgabe, und ein Dienst, der einen
// unbegrenzten Strom in den Speicher liest, haengt an der Laune des Aufrufers.
const maxRumpf = 1 << 20

// taskRoutes buendelt den Schreibdienst. Der Area-Speicher wird EINMAL beim Bau
// des Handlers gebunden (areas.Default liest CC_AREAS_FILE), nicht je Anfrage:
// der Store serialisiert seine Zugriffe ueber einen eigenen Mutex, und je
// Anfrage einen neuen zu oeffnen hiesse, genau diese Serialisierung aufzugeben.
type taskRoutes struct {
	dienst *taskops.Dienst
	// projekte ist dieselbe Entdeckung, die getTarget auf jeder Anfrage
	// benutzt (server.go) — keine zweite Definition davon, was ein Projekt ist.
	// Sie wird JE ANFRAGE ausgewertet und nicht einmal beim Bau: die
	// Bereichsansicht zeigt die Projekte dieser Maschine, und ein Repo, das seit
	// dem Start dazugekommen ist, soll darin auftauchen.
	projekte func() []collectors.ProjectRef
}

func neueTaskRoutes(projekte func() []collectors.ProjectRef) *taskRoutes {
	return &taskRoutes{dienst: taskops.Neu(areas.Default()), projekte: projekte}
}

// bereichProjekte uebersetzt die entdeckten Projekte in die Form, die die
// Gruppierung liest. Beide Felder reisen mit — areas.json ist auf den NAMEN
// geschluesselt, die Ansicht braucht den PFAD, um das Projekt zu oeffnen.
func (t *taskRoutes) bereichProjekte() []appctx.BereichProjekt {
	if t.projekte == nil {
		return nil
	}
	refs := t.projekte()
	out := make([]appctx.BereichProjekt, 0, len(refs))
	for _, p := range refs {
		out = append(out, appctx.BereichProjekt{Name: p.Name, Pfad: p.Path})
	}
	return out
}

// fehlerAntwort ist der Rumpf jedes Fehlschlags.
//
// Grund ist taskops' eigener Grund (eingabe | exit | timeout | not-found) und
// nicht der HTTP-Status: die Ansicht soll "bd hat abgelehnt" von "bd hing"
// unterscheiden koennen, ohne aus einer Statusnummer rueckzuschliessen.
//
// Aufrufe traegt die Spur der bd-Aufrufe mit. Ohne sie waere ein
// TEILfehlschlag unsichtbar — angelegt, aber das Datum nicht gesetzt, oder
// geschrieben, aber nicht zurueckgelesen. Der Status kann diesen Unterschied
// nicht ausdruecken, die Spur schon.
type fehlerAntwort struct {
	Fehler  string         `json:"fehler"`
	Grund   string         `json:"grund,omitempty"`
	Aufrufe []bdops.Result `json:"aufrufe,omitempty"`
}

// statusFuer bildet den Grund eines taskops-Fehlers auf HTTP ab.
//
// Die Abbildung ist die heikelste Entscheidung dieser Datei, weil zwei der
// vier Gruende anders heissen, als sie klingen:
//
//   - "not-found" heisst bei bdops AUSSCHLIESSLICH "kein bd auf dem PATH"
//     (exec.ErrNotFound), NICHT "kein solcher Bead". Als 404 beantwortet
//     hiesse das, dem Benutzer die Existenz seiner Aufgabe abzusprechen,
//     obwohl in Wahrheit die Installation des Dienstes defekt ist. Also 500:
//     dieser Server kann seine Aufgabe gerade nicht erfuellen, und niemand
//     soll die Anfrage unveraendert wiederholen.
//
//   - "exit" ist der Fall einer unbekannten Bead-Id — bd antwortet darauf mit
//     Exit 1 (gemessen in gkp-gvk: `no issue found matching "…"`). Als 500
//     beantwortet wuerde aus einem Tippfehler ein Serverfehler. Es ist eine
//     abgelehnte Eingabe, aber eine, die den Weg bis zu bd geschafft hat:
//     422 statt 400. Der Unterschied ist die Grenze, an der abgelehnt wurde —
//     400 heisst "dieser Dienst hat die Anfrage gar nicht erst weitergegeben",
//     422 heisst "bd hat sie bekommen und abgelehnt". Nur mit dieser Trennung
//     sagt der Status etwas, das der Rumpf nicht ohnehin schon sagt.
//
//   - "timeout" ist 504: bd lief in seine 30 Sekunden. Der Dienst ist heil,
//     der Schritt dahinter hat nicht geantwortet, und eine Wiederholung kann
//     gelingen. Ob bd vorher noch geschrieben hat, ist unbekannt — deshalb
//     traegt die Antwort die Aufrufliste.
//
//   - "" traegt ein Fehler, der nicht aus taskops stammt (etwa ein
//     Schreibfehler an areas.json). Unbekannte Ursache, also 500.
func statusFuer(err error) int {
	switch taskops.GrundVon(err) {
	case taskops.GrundEingabe:
		return http.StatusBadRequest
	case bdops.ReasonExit:
		return http.StatusUnprocessableEntity
	case bdops.ReasonTimeout:
		return http.StatusGatewayTimeout
	case bdops.ReasonNotFound:
		return http.StatusInternalServerError
	default:
		return http.StatusInternalServerError
	}
}

func schreibeFehler(w http.ResponseWriter, status int, grund string, err error, aufrufe []bdops.Result) {
	writeJSONStatus(w, status, fehlerAntwort{Fehler: err.Error(), Grund: grund, Aufrufe: aufrufe})
}

// rumpfLesen liest den Anfragerumpf in einen Wunsch.
//
// DisallowUnknownFields ist hier nicht Sorgfalt, sondern die Lehre aus einem
// gemessenen Schaden: beim ersten echten beads_create schrieb der Aufrufer
// `parent_key` statt `parent`. Ohne diese Zeile waere das Feld still verworfen
// worden — zwei elternlose Beads ausserhalb jeder Karte, gemeldet als Erfolg.
// Ein verworfenes Feld ist eine Falschmeldung, kein fehlendes Detail; der
// Decoder nennt den Feldnamen, und genau der gehoert in die Antwort.
func rumpfLesen[T any](w http.ResponseWriter, r *http.Request) (T, error) {
	var wunsch T
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRumpf))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&wunsch); err != nil {
		return wunsch, fmt.Errorf("rumpf unlesbar: %w", err)
	}
	// Ein zweites JSON-Objekt hinter dem ersten waere sonst stillschweigend
	// ignoriert — derselbe Fehler eine Ebene hoeher.
	if dec.More() {
		return wunsch, errors.New("rumpf trägt mehr als ein JSON-Objekt")
	}
	return wunsch, nil
}

func (t *taskRoutes) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		schreibeFehler(w, http.StatusMethodNotAllowed, "",
			fmt.Errorf("%s wird hier nicht bedient: diese Routen schreiben, also nur POST", r.Method), nil)
		return
	}
	if !csrfOkFuer(r) {
		schreibeFehler(w, http.StatusForbidden, "", errors.New("CSRF: fremde Origin blockiert"), nil)
		return
	}

	switch strings.TrimPrefix(r.URL.Path, taskPraefix) {
	case "anlegen":
		wunsch, err := rumpfLesen[taskops.AnlegenWunsch](w, r)
		if err != nil {
			schreibeFehler(w, http.StatusBadRequest, taskops.GrundEingabe, err, nil)
			return
		}
		erg, err := t.dienst.Anlegen(wunsch)
		t.bdAntwort(w, erg, err)

	case "aendern":
		wunsch, err := rumpfLesen[taskops.AendernWunsch](w, r)
		if err != nil {
			schreibeFehler(w, http.StatusBadRequest, taskops.GrundEingabe, err, nil)
			return
		}
		erg, err := t.dienst.Aendern(wunsch)
		t.bdAntwort(w, erg, err)

	case "terminieren":
		wunsch, err := rumpfLesen[taskops.TerminWunsch](w, r)
		if err != nil {
			schreibeFehler(w, http.StatusBadRequest, taskops.GrundEingabe, err, nil)
			return
		}
		erg, err := t.dienst.Terminieren(wunsch)
		t.bdAntwort(w, erg, err)

	case "bereich":
		wunsch, err := rumpfLesen[taskops.BereichWunsch](w, r)
		if err != nil {
			schreibeFehler(w, http.StatusBadRequest, taskops.GrundEingabe, err, nil)
			return
		}
		erg, err := t.dienst.BereichSetzen(wunsch)
		// Weiterhin kein InvalidateProjectContext, und das ist seit gkp-8ww
		// nicht mehr offen, sondern beantwortet: die Bereichsansicht liest
		// areas.json in appctx.BereicheBlock, und der laeuft in BuildData —
		// also bei JEDEM Seitenaufbau, nicht hinter cache.GetProjectContext.
		// Es gibt damit keinen zwischengespeicherten Stand, der veralten
		// koennte. Hier trotzdem zu invalidieren waere zusaetzlich falsch
		// geschluesselt: Ergebnis.Projekt traegt bei dieser Operation den
		// NAMEN des Projekts, der Cache aber den PFAD.
		t.bereichAntwort(w, erg, err)

	case "bereich/anlegen":
		wunsch, err := rumpfLesen[taskops.BereichAnlegenWunsch](w, r)
		if err != nil {
			schreibeFehler(w, http.StatusBadRequest, taskops.GrundEingabe, err, nil)
			return
		}
		erg, err := t.dienst.BereichAnlegen(wunsch)
		t.bereichAntwort(w, erg, err)

	case "bereich/umbenennen":
		wunsch, err := rumpfLesen[taskops.BereichUmbenennenWunsch](w, r)
		if err != nil {
			schreibeFehler(w, http.StatusBadRequest, taskops.GrundEingabe, err, nil)
			return
		}
		erg, err := t.dienst.BereichUmbenennen(wunsch)
		t.bereichAntwort(w, erg, err)

	case "bereich/einklappen":
		// Einklappzustand der Leiste (repo-leiste, #25). Antwort ohne die
		// Gruppierung der lokalen Projekte: die neue Leiste laedt /api/repos
		// ohnehin neu.
		wunsch, err := rumpfLesen[taskops.BereichEinklappenWunsch](w, r)
		if err != nil {
			schreibeFehler(w, http.StatusBadRequest, taskops.GrundEingabe, err, nil)
			return
		}
		erg, err := t.dienst.BereichEinklappen(wunsch)
		antwort(w, erg, err)

	case "bereich/loeschen":
		// Eigene Route statt eines Schalters an bereich/umbenennen: die beiden
		// beantworten verschiedene Fragen, und nur diese hier kann verweigert
		// werden (areas.Store.Loeschen, solange die Area belegt ist). Als
		// Sonderfall des Umbenennens waere die Ablehnung eine Ausnahme in einer
		// Operation, die sonst immer geht.
		wunsch, err := rumpfLesen[taskops.BereichLoeschenWunsch](w, r)
		if err != nil {
			schreibeFehler(w, http.StatusBadRequest, taskops.GrundEingabe, err, nil)
			return
		}
		erg, err := t.dienst.BereichLoeschen(wunsch)
		t.bereichAntwort(w, erg, err)

	default:
		schreibeFehler(w, http.StatusNotFound, "",
			fmt.Errorf("unbekannte Schreiboperation %q", strings.TrimPrefix(r.URL.Path, taskPraefix)), nil)
	}
}

// schreibAntwort ist der Erfolgsfall der drei bd-Operationen: taskops' Stand
// plus der neu gebaute Ast (gkp-8a0, Spec §7.2).
//
// Die Felder von taskops.Ergebnis stehen weiterhin oben im Rumpf — das
// eingebettete Feld flacht in JSON aus, der Vertrag aus gkp-t1o bleibt also
// unveraendert gueltig.
//
// ACHTUNG bei den zwei "aufgabe": `aufgabe` oben ist taskops' Stand, frisch aus
// `bd show` und mit gedeuteten Datumsfeldern (wann/frist). `teilbaum.aufgabe`
// ist derselbe Bead in der Form, die die ANSICHT zeichnet (appctx.ReportsBead:
// Belege, Luecken, Unteraufgaben, Fortschritt) — andere Felder, englische
// Namen. Keins ersetzt das andere: der Teilbaum kennt die Termine nicht, und
// taskops kennt den Baum nicht.
type schreibAntwort struct {
	taskops.Ergebnis
	Teilbaum *appctx.Teilbaum `json:"teilbaum,omitempty"`
	// Ansichten ist die neu getroffene AUSWAHL der Aufgabenlisten (gkp-7gb).
	//
	// Sie liegt neben dem Teilbaum, weil die beiden verschiedene Fragen
	// beantworten: der Teilbaum sagt, wie die Zeile jetzt aussieht, die
	// Ansichten sagen, OB und WO sie noch steht. Ein gesetztes Datum kann eine
	// Aufgabe aus Heute herausnehmen oder hineinholen — das ist eine Auswahl,
	// und die gehoert nach Go (CLAUDE.md), also kann der Browser sie nach dem
	// Schreiben nicht selbst nachziehen.
	//
	// Kein omitempty: eine leere Auswahl ist ein Ergebnis ("heute nichts mehr
	// offen"), ein fehlendes Feld waere eine Nichtauskunft.
	Ansichten map[string]appctx.AufgabenAnsicht `json:"ansichten"`
	// Nachladen traegt den Grund, warum KEIN Teilbaum dabei ist, obwohl
	// geschrieben wurde. Ohne dieses Feld waere der Fall statuslos: die Ansicht
	// saehe denselben Rumpf wie bei einem Schreibvorgang ohne Wirkung und
	// liesse einen veralteten Stand stehen. Ist es gesetzt, laedt die Ansicht
	// die Karte neu, statt einen Teil zu ersetzen — dasselbe Muster wie bei
	// `aufrufe`, das den Teilfehlschlag sichtbar macht.
	Nachladen string `json:"nachladen,omitempty"`
}

// bdAntwort beantwortet die drei Operationen, die bd anfassen.
//
// Invalidiert wird AUCH im Fehlerfall, sobald ein Projekt feststeht: ein
// Teilfehlschlag hat den Stand bereits geaendert (angelegt, Datum nicht
// gesetzt), und ein Fehlschlag NACH dem Schreiben — das Ruecklesen — sowieso.
// Einmal zu viel zu invalidieren kostet einen Neuaufbau des Kontexts; einmal zu
// wenig zeigt der Ansicht einen Stand, den es nicht mehr gibt.
//
// Der Schluessel ist Ergebnis.Projekt und nicht der Pfad aus dem Wunsch: das
// ist der Pfad, mit dem taskops tatsaechlich gearbeitet hat (getrimmt), und
// derselbe, unter dem GetProjectContext den Kontext fuehrt.
//
// Invalidiert wird VOR dem Bauen des Astes, und zwar aus dem schlichten Grund,
// dass das Invalidieren Mikrosekunden kostet und der Aufbau Sekunden: dazwischen
// bekaeme jeder Seitenaufruf noch den Stand von vor dem Schreibvorgang
// ausgeliefert. Beide Schritte zusammen sind ohnehin nicht atomar — wer in
// dieselbe Luecke faellt, sieht den alten Stand und holt sich beim naechsten
// Aufruf den neuen.
func (t *taskRoutes) bdAntwort(w http.ResponseWriter, erg taskops.Ergebnis, err error) {
	if erg.Projekt != "" {
		cache.InvalidateProjectContext(erg.Projekt)
	}
	if err != nil {
		antwort(w, erg, err)
		return
	}
	a := schreibAntwort{Ergebnis: erg}
	a.Teilbaum, a.Nachladen, a.Ansichten = teilbaumNach(erg)
	writeJSONStatus(w, http.StatusOK, a)
}

// teilbaumNach baut den Ast der gerade geschriebenen Aufgabe neu.
//
// Gebaut wird nach dem Schreiben und nicht aus dem, was vor dem Schreiben im
// Cache lag: nur ein frischer Aufbau kann die Aufgabe ueberhaupt enthalten,
// wenn sie eben erst angelegt wurde, und nur er traegt die geaenderten Zahlen
// ihrer Vorfahren. Zwei Schreibvorgaenge hintereinander liefern damit
// aufeinander aufbauende Staende, weil der zweite den Stand nach dem ersten
// liest.
//
// Der Fehlerfall kommt hier nicht an: ein Schreibvorgang, der nicht geglueckt
// ist, hat keinen neuen Ast — er hat einen Fehler, und der geht mit den
// Aufrufen heraus.
// Die Aufgabenlisten kommen aus DEMSELBEN Aufbau (appctx.StandNach): den Baum
// zweimal zu bauen waere der doppelte Satz bd-Aufrufe fuer eine Auswahl, die
// Mikrosekunden kostet.
func teilbaumNach(erg taskops.Ergebnis) (*appctx.Teilbaum, string, map[string]appctx.AufgabenAnsicht) {
	if erg.Aufgabe == nil || erg.Aufgabe.ID == "" {
		// Kann nur eintreten, wenn taskops kuenftig ohne Stand erfolgreich
		// zurueckkehrt. Dann ist der fehlende Ast eine Auskunft, keine Luecke —
		// die Listen stehen trotzdem neu, sie haengen nicht an einer Id.
		return nil, "kein Stand zur geschriebenen Aufgabe — Ansicht neu laden",
			appctx.AnsichtenNach(erg.Projekt, anzeigezeit.Jetzt())
	}
	tb, grund, ansichten := appctx.StandNach(erg.Projekt, erg.Aufgabe.ID, anzeigezeit.Jetzt())
	if grund != "" {
		return nil, grund, ansichten
	}
	return &tb, "", ansichten
}

// bereichsAntwort ist der Erfolgsfall der drei Bereich-Operationen: taskops'
// Stand plus die neu gebaute Gruppierung (gkp-8ww).
//
// Sie ist das Gegenstueck zum `teilbaum` der bd-Routen (Spec §7.2) — und
// bewusst NICHT dieselbe Form. Eine Aufgabenaenderung betrifft EINEN Ast, und
// der laesst sich benennen: die Ansicht ersetzt genau ihn. Eine
// Area-Aenderung betrifft die Zuordnung als Ganzes; ein Umbenennen zieht jedes
// Projekt der Area mit, ein Zuordnen nimmt eins aus der einen Gruppe heraus und
// legt es in die andere. "Der betroffene Teil" waeren hier zwei Gruppen,
// manchmal n — und die Gruppierung IST der kleinste Schnitt, der die Frage
// vollstaendig beantwortet. Teuer ist das nicht: es sind die Projekte einer
// Maschine, ein Dutzend Zeilen ohne bd-Aufruf und ohne Netz.
type bereichsAntwort struct {
	taskops.Ergebnis
	// Kein omitempty: eine leere Gruppierung ist ein Ergebnis ("keine Projekte
	// entdeckt"), ein fehlendes Feld waere eine Nichtauskunft — dieselbe
	// Begruendung wie bei schreibAntwort.Ansichten.
	Bereiche map[string]any `json:"bereiche"`
}

// bereichAntwort beantwortet die drei Operationen an areas.json.
//
// Die Gruppierung wird NACH dem Schreiben gebaut und aus der Datei gelesen,
// nicht aus dem Wunsch zurueckgerechnet: nur so steht in der Antwort, was
// wirklich in areas.json steht — samt der Sammel-Area, die beim Loesen einer
// Zuordnung entsteht und nie in der Datei vorkommt.
func (t *taskRoutes) bereichAntwort(w http.ResponseWriter, erg taskops.Ergebnis, err error) {
	if err != nil {
		antwort(w, erg, err)
		return
	}
	writeJSONStatus(w, http.StatusOK, bereichsAntwort{
		Ergebnis: erg,
		Bereiche: appctx.BereicheBlock(t.bereichProjekte()),
	})
}

// antwort schreibt entweder den neuen Stand oder den Fehler — beides JSON.
func antwort(w http.ResponseWriter, erg taskops.Ergebnis, err error) {
	if err != nil {
		schreibeFehler(w, statusFuer(err), taskops.GrundVon(err), err, erg.Aufrufe)
		return
	}
	writeJSONStatus(w, http.StatusOK, erg)
}
