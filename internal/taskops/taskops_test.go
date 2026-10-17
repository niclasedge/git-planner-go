package taskops

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/niclasedge/git-planner-go/internal/areas"
	"github.com/niclasedge/git-planner-go/internal/bdops"
)

// bdAttrappe ersetzt internal/bdops im Test. Sie zeichnet auf, WAS an bdops
// ginge, und antwortet mit einer gemessenen bd-Ausgabe — damit laeuft jeder
// Test hier ohne bd, ohne Datenbank und ohne HTTP.
type bdAttrappe struct {
	createPfad    string
	createIssues  []bdops.NewIssue
	createKanten  []bdops.Edge
	createErgebis bdops.CreateResult
	createFehler  error

	updatePfad    string
	updateAenderg [][]bdops.Change
	updateAufrufe []bdops.Result
	updateFehler  error

	queryPfad    string
	queryArgs    [][]string
	queryStdout  string
	queryAufruf  bdops.Result
	queryFehler  error
	queryAufrufe int
}

func (a *bdAttrappe) Create(pfad string, issues []bdops.NewIssue, kanten []bdops.Edge, trocken bool) (bdops.CreateResult, error) {
	a.createPfad = pfad
	a.createIssues = append([]bdops.NewIssue(nil), issues...)
	a.createKanten = append([]bdops.Edge(nil), kanten...)
	return a.createErgebis, a.createFehler
}

func (a *bdAttrappe) Update(pfad string, aenderungen []bdops.Change) ([]bdops.Result, error) {
	a.updatePfad = pfad
	a.updateAenderg = append(a.updateAenderg, append([]bdops.Change(nil), aenderungen...))
	return a.updateAufrufe, a.updateFehler
}

func (a *bdAttrappe) Query(pfad string, args []string) (bdops.Result, error) {
	a.queryPfad = pfad
	a.queryArgs = append(a.queryArgs, append([]string(nil), args...))
	a.queryAufrufe++
	if a.queryFehler != nil {
		return a.queryAufruf, a.queryFehler
	}
	res := a.queryAufruf
	res.Stdout = a.queryStdout
	return res, nil
}

// aufrufe zaehlt jeden Weg, der bei bd gelandet waere. Die Pruefung "kein Pfad
// ohne Id" haengt daran: eine Ablehnung, die trotzdem bd erreicht hat, ist
// keine.
func (a *bdAttrappe) aufrufe() int {
	return a.queryAufrufe + len(a.updateAenderg) + len(a.createIssues)
}

// gemesseneAnzeige ist echte Ausgabe von `bd show --id=… --json`, bd 1.2.2,
// aufgenommen am 2026-09-13 unter TZ=Europe/Berlin gegen eine Wegwerf-DB.
// Entscheidend sind die zwei Zeitbasen: created_at/updated_at sind echtes UTC,
// due_at/defer_until sind lokale Wanduhr mit luegendem Z (§4.4 der Spec).
const gemesseneAnzeige = `[
  {
    "id": "tosp-2vf",
    "title": "Probe Termin",
    "status": "deferred",
    "priority": 2,
    "issue_type": "task",
    "created_at": "2026-09-13T20:54:21Z",
    "updated_at": "2026-09-13T20:54:21Z",
    "due_at": "2026-01-15T00:00:00Z",
    "defer_until": "2026-09-27T22:54:21Z"
  }
]`

func attrappeMitAnzeige() *bdAttrappe {
	return &bdAttrappe{queryStdout: gemesseneAnzeige}
}

func dienstMit(t *testing.T, bd bdTuer) *Dienst {
	t.Helper()
	return neuMit(bd, areas.Open(filepath.Join(t.TempDir(), "areas.json")))
}

// setzeZone macht die Zeitzone des Tests fest. Ohne das liefe die Pruefung der
// Datumsdeutung unter TZ=UTC gruen, obwohl die Deutung falsch waere — der
// Unterschied zwischen ParseUserDate und ParseEventStamp ist unter UTC
// unsichtbar.
func setzeZone(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Skipf("Zonendatenbank fehlt: %v", err)
	}
	alt := time.Local
	time.Local = loc
	t.Cleanup(func() { time.Local = alt })
	return loc
}

// ---------------------------------------------------------------- anlegen

func TestAnlegenReichtDieFelderAnBdopsDurch(t *testing.T) {
	bd := attrappeMitAnzeige()
	bd.createErgebis = bdops.CreateResult{
		Keys: map[string]string{"n0": "tosp-2vf"},
		IDs:  []string{"tosp-2vf"},
	}
	d := dienstMit(t, bd)

	erg, err := d.Anlegen(AnlegenWunsch{
		Pfad:         "/pfad/zum/repo",
		Titel:        "Probe Termin",
		Typ:          "task",
		Prioritaet:   "2",
		Beschreibung: "Text",
		Elternteil:   "tosp-9aa",
		Wann:         "+2w",
		Frist:        "2026-01-15",
	})
	if err != nil {
		t.Fatalf("Anlegen: %v", err)
	}
	if bd.createPfad != "/pfad/zum/repo" {
		t.Errorf("Pfad = %q, erwartet /pfad/zum/repo", bd.createPfad)
	}
	if len(bd.createIssues) != 1 {
		t.Fatalf("%d issues, erwartet 1", len(bd.createIssues))
	}
	is := bd.createIssues[0]
	if is.Title != "Probe Termin" || is.Type != "task" || string(is.Priority) != "2" {
		t.Errorf("issue falsch durchgereicht: %+v", is)
	}
	if is.Parent != "tosp-9aa" || is.Due != "2026-01-15" || is.Defer != "+2w" {
		t.Errorf("parent/due/defer falsch: parent=%q due=%q defer=%q", is.Parent, is.Due, is.Defer)
	}
	if erg.Aufgabe == nil || erg.Aufgabe.ID != "tosp-2vf" {
		t.Fatalf("Ergebnis ohne neu gelesene Aufgabe: %+v", erg)
	}
}

func TestAnlegenOhneTitelUndOhnePfadErreichtBdNie(t *testing.T) {
	for _, fall := range []struct {
		name  string
		wunsc AnlegenWunsch
	}{
		{"ohne Pfad", AnlegenWunsch{Titel: "x"}},
		{"ohne Titel", AnlegenWunsch{Pfad: "/repo"}},
	} {
		t.Run(fall.name, func(t *testing.T) {
			bd := attrappeMitAnzeige()
			d := dienstMit(t, bd)
			if _, err := d.Anlegen(fall.wunsc); err == nil {
				t.Fatal("kein Fehler, erwartet einer")
			} else if GrundVon(err) != GrundEingabe {
				t.Errorf("Grund = %q, erwartet %q", GrundVon(err), GrundEingabe)
			}
			if bd.aufrufe() != 0 {
				t.Errorf("%d bd-Aufrufe trotz Ablehnung", bd.aufrufe())
			}
		})
	}
}

// ---------------------------------------------------------------- aendern

func TestAendernBautGenauEineAenderungMitId(t *testing.T) {
	bd := attrappeMitAnzeige()
	d := dienstMit(t, bd)

	if _, err := d.Aendern(AendernWunsch{
		Pfad: "/repo", ID: "tosp-2vf",
		Titel: "Neuer Titel", Beschreibung: "Neuer Text", Status: "in_progress", Prioritaet: "1",
	}); err != nil {
		t.Fatalf("Aendern: %v", err)
	}
	if len(bd.updateAenderg) != 1 || len(bd.updateAenderg[0]) != 1 {
		t.Fatalf("erwartet genau eine Aenderung, war %+v", bd.updateAenderg)
	}
	c := bd.updateAenderg[0][0]
	if len(c.IDs) != 1 || c.IDs[0] != "tosp-2vf" {
		t.Errorf("IDs = %v, erwartet [tosp-2vf]", c.IDs)
	}
	if c.Title != "Neuer Titel" || c.Description != "Neuer Text" || c.Status != "in_progress" || string(c.Priority) != "1" {
		t.Errorf("Felder falsch durchgereicht: %+v", c)
	}
	// Terminfelder duerfen bei einer reinen Inhaltsaenderung NICHT mitkommen:
	// ein nicht-nil Zeiger auf "" wuerde das Datum loeschen.
	if c.Due != nil || c.Defer != nil {
		t.Errorf("Aendern fasst Termine an: due=%v defer=%v", c.Due, c.Defer)
	}
}

func TestAendernOhneIdErreichtBdNie(t *testing.T) {
	for _, id := range []string{"", "   ", "--force"} {
		bd := attrappeMitAnzeige()
		d := dienstMit(t, bd)
		_, err := d.Aendern(AendernWunsch{Pfad: "/repo", ID: id, Titel: "x"})
		if err == nil {
			t.Fatalf("id %q: kein Fehler, erwartet einer", id)
		}
		if GrundVon(err) != GrundEingabe {
			t.Errorf("id %q: Grund = %q, erwartet %q", id, GrundVon(err), GrundEingabe)
		}
		if bd.aufrufe() != 0 {
			t.Errorf("id %q: %d bd-Aufrufe trotz Ablehnung", id, bd.aufrufe())
		}
	}
}

// ---------------------------------------------------------------- terminieren

func TestTerminierenSetztUndLoeschtGetrennt(t *testing.T) {
	faelle := []struct {
		name        string
		wunsch      TerminWunsch
		wannErwart  *string
		fristErwart *string
	}{
		{
			name:       "nur Wann gesetzt",
			wunsch:     TerminWunsch{Pfad: "/repo", ID: "tosp-2vf", Wann: zeiger("+2w")},
			wannErwart: zeiger("+2w"),
		},
		{
			name:        "nur Frist geloescht",
			wunsch:      TerminWunsch{Pfad: "/repo", ID: "tosp-2vf", Frist: zeiger("")},
			fristErwart: zeiger(""),
		},
		{
			name:        "beide",
			wunsch:      TerminWunsch{Pfad: "/repo", ID: "tosp-2vf", Wann: zeiger(""), Frist: zeiger("2026-01-15")},
			wannErwart:  zeiger(""),
			fristErwart: zeiger("2026-01-15"),
		},
	}
	for _, f := range faelle {
		t.Run(f.name, func(t *testing.T) {
			bd := attrappeMitAnzeige()
			d := dienstMit(t, bd)
			if _, err := d.Terminieren(f.wunsch); err != nil {
				t.Fatalf("Terminieren: %v", err)
			}
			if len(bd.updateAenderg) != 1 || len(bd.updateAenderg[0]) != 1 {
				t.Fatalf("erwartet genau eine Aenderung, war %+v", bd.updateAenderg)
			}
			c := bd.updateAenderg[0][0]
			gleich(t, "defer", c.Defer, f.wannErwart)
			gleich(t, "due", c.Due, f.fristErwart)
			if c.Title != "" || c.Status != "" {
				t.Errorf("Terminieren fasst Inhalt an: %+v", c)
			}
		})
	}
}

func TestTerminierenOhneDatumsfeldErreichtBdNie(t *testing.T) {
	bd := attrappeMitAnzeige()
	d := dienstMit(t, bd)
	if _, err := d.Terminieren(TerminWunsch{Pfad: "/repo", ID: "tosp-2vf"}); err == nil {
		t.Fatal("kein Fehler, erwartet einer")
	} else if GrundVon(err) != GrundEingabe {
		t.Errorf("Grund = %q, erwartet %q", GrundVon(err), GrundEingabe)
	}
	if bd.aufrufe() != 0 {
		t.Errorf("%d bd-Aufrufe trotz Ablehnung", bd.aufrufe())
	}
}

// TestTerminierenDeutetDieZeitbasenGetrennt ist der Test, an dem die Zone
// haengt: unter TZ=UTC waere er auch dann gruen, wenn beide Felder mit
// derselben Funktion gelesen wuerden.
func TestTerminierenDeutetDieZeitbasenGetrennt(t *testing.T) {
	tokio := setzeZone(t, "Asia/Tokyo")
	bd := attrappeMitAnzeige()
	d := dienstMit(t, bd)

	erg, err := d.Terminieren(TerminWunsch{Pfad: "/repo", ID: "tosp-2vf", Wann: zeiger("+2w")})
	if err != nil {
		t.Fatalf("Terminieren: %v", err)
	}
	auf := erg.Aufgabe
	if auf == nil {
		t.Fatal("Ergebnis ohne Aufgabe")
	}

	// defer_until = "2026-09-27T22:54:21Z" ist LOKALE Wanduhr. Richtig gelesen
	// bleibt es der 27. um 22:54 in der Zone des Dienstes. Als UTC gelesen
	// waere es in Tokio der 28. um 07:54 — der Kalendertag kippt, und genau
	// nach Kalendertag gruppieren Heute und Demnaechst.
	if auf.Wann == nil {
		t.Fatal("Wann fehlt")
	}
	if got := auf.Wann.In(tokio); got.Day() != 27 || got.Hour() != 22 {
		t.Errorf("Wann = %s, erwartet 27. um 22:54 lokal (ParseUserDate, nicht ParseEventStamp)", got)
	}

	// created_at = "2026-09-13T20:54:21Z" ist ECHTES UTC und wird nicht
	// verschoben. Als Wanduhr gelesen waere es 20:54 Tokio = 11:54 UTC.
	if auf.Angelegt == nil {
		t.Fatal("Angelegt fehlt")
	}
	if !auf.Angelegt.Equal(time.Date(2026, 9, 13, 20, 54, 21, 0, time.UTC)) {
		t.Errorf("Angelegt = %s, erwartet 2026-09-13T20:54:21Z (ParseEventStamp, nicht ParseUserDate)", auf.Angelegt.UTC())
	}
}

// ---------------------------------------------------------------- fehler

func TestFehlerBehaeltDenGrundVonBdops(t *testing.T) {
	for _, grund := range []string{bdops.ReasonExit, bdops.ReasonTimeout, bdops.ReasonNotFound} {
		t.Run(grund, func(t *testing.T) {
			bd := attrappeMitAnzeige()
			bd.updateAufrufe = []bdops.Result{{Args: []string{"update"}, Failed: true, Reason: grund, ExitCode: 1}}
			bd.updateFehler = errors.New("bd update: kaputt")
			d := dienstMit(t, bd)

			_, err := d.Aendern(AendernWunsch{Pfad: "/repo", ID: "tosp-2vf", Titel: "x"})
			if err == nil {
				t.Fatal("kein Fehler, erwartet einer")
			}
			if GrundVon(err) != grund {
				t.Errorf("Grund = %q, erwartet %q", GrundVon(err), grund)
			}
			if !errors.Is(err, bd.updateFehler) {
				t.Errorf("bd-Meldung verloren: %v", err)
			}
		})
	}
}

func TestLesefehlerNachDemSchreibenBleibtSichtbar(t *testing.T) {
	bd := attrappeMitAnzeige()
	bd.queryAufruf = bdops.Result{Failed: true, Reason: bdops.ReasonTimeout, ExitCode: -1}
	bd.queryFehler = errors.New("bd show: Zeit abgelaufen")
	d := dienstMit(t, bd)

	_, err := d.Aendern(AendernWunsch{Pfad: "/repo", ID: "tosp-2vf", Titel: "x"})
	if err == nil {
		t.Fatal("kein Fehler, erwartet einer")
	}
	if GrundVon(err) != bdops.ReasonTimeout {
		t.Errorf("Grund = %q, erwartet %q", GrundVon(err), bdops.ReasonTimeout)
	}
}

// ---------------------------------------------------------------- bereich

func TestBereichSetzenSchreibtDieDatei(t *testing.T) {
	pfad := filepath.Join(t.TempDir(), "areas.json")
	bd := attrappeMitAnzeige()
	d := neuMit(bd, areas.Open(pfad))

	erg, err := d.BereichSetzen(BereichWunsch{Projekt: "git-planner", Bereich: "Arbeit"})
	if err != nil {
		t.Fatalf("BereichSetzen: %v", err)
	}
	if erg.Bereich != "Arbeit" {
		t.Errorf("Bereich = %q, erwartet Arbeit", erg.Bereich)
	}
	blob, err := os.ReadFile(pfad)
	if err != nil {
		t.Fatalf("areas.json fehlt: %v", err)
	}
	var st areas.State
	if err := json.Unmarshal(blob, &st); err != nil {
		t.Fatalf("areas.json unlesbar: %v", err)
	}
	if st.AreaOf("git-planner") != "Arbeit" {
		t.Errorf("Zuordnung = %+v", st.Zuordnung)
	}
	if bd.aufrufe() != 0 {
		t.Errorf("BereichSetzen hat bd angefasst (%d Aufrufe) — die Area steht in areas.json, nicht in bd", bd.aufrufe())
	}

	// Leerer Bereich loest die Zuordnung und faellt auf die Sammel-Area
	// zurueck, statt einen namenlosen Bereich zu hinterlassen.
	erg, err = d.BereichSetzen(BereichWunsch{Projekt: "git-planner", Bereich: ""})
	if err != nil {
		t.Fatalf("BereichSetzen (loeschen): %v", err)
	}
	if erg.Bereich != areas.OhneBereich {
		t.Errorf("Bereich = %q, erwartet %q", erg.Bereich, areas.OhneBereich)
	}
}

func TestBereichSetzenOhneProjektWirdAbgelehnt(t *testing.T) {
	d := dienstMit(t, attrappeMitAnzeige())
	if _, err := d.BereichSetzen(BereichWunsch{Bereich: "Arbeit"}); err == nil {
		t.Fatal("kein Fehler, erwartet einer")
	} else if GrundVon(err) != GrundEingabe {
		t.Errorf("Grund = %q, erwartet %q", GrundVon(err), GrundEingabe)
	}
}

// ---------------------------------------------------------------- helfer

func zeiger(s string) *string { return &s }

func gleich(t *testing.T, name string, ist, soll *string) {
	t.Helper()
	switch {
	case ist == nil && soll == nil:
	case ist == nil || soll == nil:
		t.Errorf("%s: ist=%s soll=%s", name, zeigerText(ist), zeigerText(soll))
	case *ist != *soll:
		t.Errorf("%s: ist=%q soll=%q", name, *ist, *soll)
	}
}

func zeigerText(p *string) string {
	if p == nil {
		return "<nicht erwaehnt>"
	}
	return `"` + *p + `"`
}

// ---------------------------------------------------------------- gkp-8ww

// Anlegen geht ohne Projekt — das ist der Unterschied zu BereichSetzen und der
// Grund, warum es die Operation gibt.
func TestBereichAnlegenBrauchtKeinProjekt(t *testing.T) {
	pfad := filepath.Join(t.TempDir(), "areas.json")
	bd := attrappeMitAnzeige()
	d := neuMit(bd, areas.Open(pfad))

	erg, err := d.BereichAnlegen(BereichAnlegenWunsch{Bereich: "  Infrastruktur  "})
	if err != nil {
		t.Fatalf("BereichAnlegen: %v", err)
	}
	if erg.Bereich != "Infrastruktur" {
		t.Errorf("Bereich = %q, erwartet Infrastruktur (getrimmt)", erg.Bereich)
	}
	st, err := areas.Open(pfad).Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Areas) != 1 || st.Areas[0] != "Infrastruktur" {
		t.Errorf("Areas = %q, erwartet [Infrastruktur]", st.Areas)
	}
	if bd.aufrufe() != 0 {
		t.Errorf("BereichAnlegen hat bd angefasst (%d Aufrufe)", bd.aufrufe())
	}
}

// Eine abgelehnte Eingabe muss als EINGABE ankommen, nicht als Stoerung: die
// Route bildet GrundEingabe auf 400 ab und alles andere auf 500. Ohne die
// Uebersetzung von areas.ErrEingabe waere ein Tippfehler des Benutzers eine
// Fehlermeldung ueber den Dienst.
func TestBereichAnlegenMeldetEingabefehlerAlsSolchen(t *testing.T) {
	d := dienstMit(t, attrappeMitAnzeige())
	for _, name := range []string{"", areas.OhneBereich} {
		_, err := d.BereichAnlegen(BereichAnlegenWunsch{Bereich: name})
		if err == nil {
			t.Fatalf("BereichAnlegen(%q) wurde angenommen", name)
		}
		if GrundVon(err) != GrundEingabe {
			t.Errorf("BereichAnlegen(%q): Grund = %q, erwartet %q", name, GrundVon(err), GrundEingabe)
		}
	}
}

func TestBereichUmbenennenZiehtDieZuordnungMit(t *testing.T) {
	pfad := filepath.Join(t.TempDir(), "areas.json")
	d := neuMit(attrappeMitAnzeige(), areas.Open(pfad))
	if _, err := d.BereichSetzen(BereichWunsch{Projekt: "alpha-tool", Bereich: "Arbeit"}); err != nil {
		t.Fatal(err)
	}

	erg, err := d.BereichUmbenennen(BereichUmbenennenWunsch{Alt: "Arbeit", Neu: "Beruf"})
	if err != nil {
		t.Fatalf("BereichUmbenennen: %v", err)
	}
	if erg.Bereich != "Beruf" {
		t.Errorf("Bereich = %q, erwartet Beruf", erg.Bereich)
	}
	st, err := areas.Open(pfad).Load()
	if err != nil {
		t.Fatal(err)
	}
	if st.AreaOf("alpha-tool") != "Beruf" {
		t.Errorf("Zuordnung = %v, erwartet alpha-tool auf Beruf", st.Zuordnung)
	}
}

func TestBereichUmbenennenUnbekanntIstEingabefehler(t *testing.T) {
	d := dienstMit(t, attrappeMitAnzeige())
	_, err := d.BereichUmbenennen(BereichUmbenennenWunsch{Alt: "Arbiet", Neu: "Beruf"})
	if err == nil {
		t.Fatal("kein Fehler, erwartet einer")
	}
	if GrundVon(err) != GrundEingabe {
		t.Errorf("Grund = %q, erwartet %q", GrundVon(err), GrundEingabe)
	}
}

// ---------------------------------------------------------------- gkp-td3

// Loeschen nimmt die leere Area aus der Datei — und faellt dabei nicht in die
// Naehe von bd. areas.json ist ein eigener Schreibort (Spec §4.3), und ein
// bd-Aufruf hier waere ein Seiteneffekt in einem fremden Repo.
func TestBereichLoeschenNimmtDieLeereAreaOhneBd(t *testing.T) {
	pfad := filepath.Join(t.TempDir(), "areas.json")
	bd := attrappeMitAnzeige()
	d := neuMit(bd, areas.Open(pfad))
	if _, err := d.BereichAnlegen(BereichAnlegenWunsch{Bereich: "Arbeit"}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.BereichAnlegen(BereichAnlegenWunsch{Bereich: "Privat"}); err != nil {
		t.Fatal(err)
	}

	if _, err := d.BereichLoeschen(BereichLoeschenWunsch{Bereich: "Arbeit"}); err != nil {
		t.Fatalf("BereichLoeschen: %v", err)
	}
	st, err := areas.Open(pfad).Load()
	if err != nil {
		t.Fatal(err)
	}
	// Die ganze Folge, nicht die Abwesenheit eines Namens: sonst bliebe der
	// Test auch dann gruen, wenn die Liste leergeraeumt wuerde (Spec §9.1).
	if got := fmt.Sprint(st.Areas); got != "[Privat]" {
		t.Errorf("Areas = %s, erwartet [Privat]", got)
	}
	if bd.aufrufe() != 0 {
		t.Errorf("BereichLoeschen hat bd angefasst (%d Aufrufe)", bd.aufrufe())
	}
}

// Die Ablehnung einer belegten Area muss als EINGABE ankommen, nicht als
// Stoerung: die Route bildet GrundEingabe auf 400 ab. Ohne die Uebersetzung
// saehe die bewusste Schutzregel aus wie ein kaputter Dienst — und der Benutzer
// bekaeme "500", wo "raeum die Projekte erst weg" gemeint ist.
func TestBereichLoeschenBelegtIstEingabefehler(t *testing.T) {
	pfad := filepath.Join(t.TempDir(), "areas.json")
	d := neuMit(attrappeMitAnzeige(), areas.Open(pfad))
	if _, err := d.BereichSetzen(BereichWunsch{Projekt: "alpha-tool", Bereich: "Arbeit"}); err != nil {
		t.Fatal(err)
	}

	_, err := d.BereichLoeschen(BereichLoeschenWunsch{Bereich: "Arbeit"})
	if err == nil {
		t.Fatal("das Loeschen einer belegten Area wurde angenommen")
	}
	if GrundVon(err) != GrundEingabe {
		t.Errorf("Grund = %q, erwartet %q", GrundVon(err), GrundEingabe)
	}
	st, err := areas.Open(pfad).Load()
	if err != nil {
		t.Fatal(err)
	}
	if st.AreaOf("alpha-tool") != "Arbeit" {
		t.Errorf("Zuordnung = %v — die Zuordnung wurde trotz Ablehnung angefasst", st.Zuordnung)
	}
}
