package areas

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// withAreasFile points CC_AREAS_FILE at a fresh t.TempDir() and returns the
// (not yet existing) file path.
func withAreasFile(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "areas.json")
	t.Setenv("CC_AREAS_FILE", p)
	return p
}

// 1) Eine fehlende Datei ist kein Fehler — genau wie bei reports.ListMaps.
func TestLoadFehlendeDateiIstKeinFehler(t *testing.T) {
	path := withAreasFile(t)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("Vorbedingung verletzt: %s existiert bereits", path)
	}

	st, err := Default().Load()
	if err != nil {
		t.Fatalf("Load einer fehlenden Datei: %v", err)
	}
	if st.Version != Version {
		t.Errorf("Version = %d, erwartet %d", st.Version, Version)
	}
	if st.Zuordnung == nil {
		t.Error("Zuordnung ist nil — der leere Zustand muss benutzbar sein")
	}
	if len(st.Areas) != 0 || len(st.Zuordnung) != 0 {
		t.Errorf("leerer Zustand erwartet, bekommen %+v", st)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("Load hat die Datei angelegt — Lesen darf nicht schreiben")
	}
}

// dirEntries listet die Dateinamen im Verzeichnis von path.
func dirEntries(t *testing.T, path string) []string {
	t.Helper()
	es, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range es {
		names = append(names, e.Name())
	}
	return names
}

// 2a) Ein erfolgreicher Schreibvorgang laesst nichts liegen: am Ende steht
// genau die Zieldatei im Verzeichnis, keine temporaere Datei daneben.
func TestSaveHinterlaesstKeineReste(t *testing.T) {
	path := withAreasFile(t)
	st := emptyState()
	st.Areas = []string{"Arbeit"}
	st.Zuordnung["git-planner"] = "Arbeit"

	if err := Default().Save(st); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if names := dirEntries(t, path); len(names) != 1 || names[0] != "areas.json" {
		t.Errorf("Verzeichnisinhalt = %v, erwartet nur areas.json", names)
	}
	got, err := Default().Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Zuordnung["git-planner"] != "Arbeit" {
		t.Errorf("Zuordnung nach Rundlauf = %+v", got.Zuordnung)
	}
}

// 2b) Bricht das Schreiben ab, bevor der Rename stattgefunden hat, muss der
// alte Stand unangetastet sein und keine halbe Datei zurueckbleiben. Der
// Testhaken ersetzt os.Rename — das ist die einzige Stelle, an der aus zwei
// Dateien eine wird.
func TestSaveAbbruchLaesstAltenStandIntakt(t *testing.T) {
	path := withAreasFile(t)
	alt := emptyState()
	alt.Zuordnung["pkm"] = "Privat"
	if err := Default().Save(alt); err != nil {
		t.Fatalf("Save (Vorbereitung): %v", err)
	}
	vorher, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	boom := errors.New("rename abgebrochen")
	orig := renameFile
	renameFile = func(string, string) error { return boom }
	defer func() { renameFile = orig }()

	neu := emptyState()
	neu.Zuordnung["pkm"] = "Arbeit"
	neu.Zuordnung["infra"] = "Infrastruktur"
	if err := Default().Save(neu); !errors.Is(err, boom) {
		t.Fatalf("Save-Fehler = %v, erwartet %v", err, boom)
	}

	nachher, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("die alte Datei ist weg: %v", err)
	}
	if string(nachher) != string(vorher) {
		t.Errorf("alter Stand veraendert:\nvorher  %s\nnachher %s", vorher, nachher)
	}
	if names := dirEntries(t, path); len(names) != 1 || names[0] != "areas.json" {
		t.Errorf("Verzeichnisinhalt = %v, erwartet nur areas.json (keine halbe Datei)", names)
	}
}

// 3) Ein Lese-Schreib-Zyklus darf nichts verlieren, was dieses Paket nicht
// kennt: die Datei wird von Hand gepflegt, und eine neuere Version des
// Dienstes schreibt womoeglich mehr hinein als diese hier liest.
func TestRundlaufBehaeltUnbekannteFelder(t *testing.T) {
	path := withAreasFile(t)
	roh := `{
  "version": 1,
  "areas": ["Arbeit"],
  "zuordnung": {"pkm": "Arbeit"},
  "reihenfolge": ["pkm", "orbit"],
  "notiz": "von Hand ergaenzt",
  "zukunft": {"tief": {"verschachtelt": [1, 2, 3]}}
}`
	if err := os.WriteFile(path, []byte(roh), 0o644); err != nil {
		t.Fatal(err)
	}

	st, err := Default().Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	st.Zuordnung["infra"] = "Infrastruktur"
	if err := Default().Save(st); err != nil {
		t.Fatalf("Save: %v", err)
	}

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("geschriebene Datei ist kein JSON: %v\n%s", err, b)
	}
	for _, feld := range []string{"reihenfolge", "notiz", "zukunft"} {
		if _, ok := got[feld]; !ok {
			t.Errorf("unbekanntes Feld %q verloren; Datei:\n%s", feld, b)
		}
	}
	if reihenfolge, _ := got["reihenfolge"].([]any); len(reihenfolge) != 2 || reihenfolge[0] != "pkm" {
		t.Errorf("reihenfolge = %v, erwartet [pkm orbit]", got["reihenfolge"])
	}
	if tief, ok := got["zukunft"].(map[string]any); !ok || tief["tief"] == nil {
		t.Errorf("zukunft verflacht: %v", got["zukunft"])
	}
	zu, _ := got["zuordnung"].(map[string]any)
	if zu["pkm"] != "Arbeit" || zu["infra"] != "Infrastruktur" {
		t.Errorf("zuordnung = %v", zu)
	}
}

// 4) Ein Projekt ohne Zuordnung landet in der Sammel-Area "Ohne Bereich" —
// sonst waere jedes neu entdeckte Projekt unsichtbar, bis es jemand eintraegt.
func TestAreaOfOhneZuordnung(t *testing.T) {
	st := emptyState()
	st.Zuordnung["git-planner"] = "Arbeit"
	st.Zuordnung["halbfertig"] = "   " // von Hand angelegt, nie gefuellt

	faelle := []struct {
		projekt, will string
	}{
		{"git-planner", "Arbeit"},
		{"infra", OhneBereich},      // gar kein Eintrag
		{"halbfertig", OhneBereich}, // leerer Eintrag ist kein Bereich
		{"", OhneBereich},
	}
	for _, f := range faelle {
		if got := st.AreaOf(f.projekt); got != f.will {
			t.Errorf("AreaOf(%q) = %q, erwartet %q", f.projekt, got, f.will)
		}
	}

	// Auch der leere Zustand aus einer fehlenden Datei muss antworten.
	if got := emptyState().AreaOf("egal"); got != OhneBereich {
		t.Errorf("leerer Zustand: AreaOf = %q, erwartet %q", got, OhneBereich)
	}
}

// 5) Kaputtes JSON wird gemeldet, nicht verschluckt — und vor allem nicht
// ueberschrieben. Begruendung steht bei loadLocked in store.go.
func TestLoadKaputtesJSONMeldetFehler(t *testing.T) {
	path := withAreasFile(t)
	kaputt := []byte(`{"version": 1, "areas": ["Arbeit"`) // abgeschnitten
	if err := os.WriteFile(path, kaputt, 0o644); err != nil {
		t.Fatal(err)
	}

	st, err := Default().Load()
	if err == nil {
		t.Fatalf("Load hat kaputtes JSON stillschweigend akzeptiert: %+v", st)
	}
	// Der zurueckgegebene Zustand ist trotzdem benutzbar — ein Aufrufer, der
	// den Fehler nur protokolliert, darf nicht in einen nil-Zugriff laufen.
	if st.Zuordnung == nil {
		t.Error("Zustand im Fehlerfall ist unbenutzbar (Zuordnung nil)")
	}

	// Der entscheidende Teil: ein Schreibzug darf die kaputte, von Hand
	// gepflegte Datei nicht durch einen leeren Stand ersetzen.
	if err := Default().SetArea("pkm", "Privat"); err == nil {
		t.Error("SetArea hat auf einer kaputten Datei geschrieben")
	}
	nachher, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(nachher) != string(kaputt) {
		t.Errorf("kaputte Datei veraendert:\nvorher  %s\nnachher %s", kaputt, nachher)
	}
}

// 6) Der Dienst bedient mehrere Requests gleichzeitig. Zwanzig parallele
// Zuordnungen muessen alle zwanzig ueberleben (kein Lost Update), und ein
// paralleler Leser darf nie einen halben Stand sehen. Mit -race laufen lassen.
func TestNebenlaeufigeZuordnungenGehenNichtVerloren(t *testing.T) {
	withAreasFile(t)
	store := Default() // EIN Store — der Mutex sitzt an ihm, nicht an der Datei

	const n = 20
	stop := make(chan struct{})
	leserFehler := make(chan error, 1)
	var leser sync.WaitGroup
	leser.Add(1)
	go func() {
		defer leser.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			if _, err := store.Load(); err != nil {
				select {
				case leserFehler <- err:
				default:
				}
				return
			}
		}
	}()

	var schreiber sync.WaitGroup
	for i := 0; i < n; i++ {
		schreiber.Add(1)
		go func(i int) {
			defer schreiber.Done()
			p := fmt.Sprintf("projekt-%02d", i)
			if err := store.SetArea(p, fmt.Sprintf("area-%d", i%3)); err != nil {
				t.Errorf("SetArea(%s): %v", p, err)
			}
		}(i)
	}
	schreiber.Wait()
	close(stop)
	leser.Wait()

	select {
	case err := <-leserFehler:
		t.Fatalf("paralleler Leser sah einen kaputten Stand: %v", err)
	default:
	}

	st, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(st.Zuordnung) != n {
		t.Errorf("%d von %d Zuordnungen ueberlebt: %v", len(st.Zuordnung), n, st.Zuordnung)
	}
	for i := 0; i < n; i++ {
		p := fmt.Sprintf("projekt-%02d", i)
		if will := fmt.Sprintf("area-%d", i%3); st.AreaOf(p) != will {
			t.Errorf("AreaOf(%s) = %q, erwartet %q", p, st.AreaOf(p), will)
		}
	}
	if len(st.Areas) != 3 {
		t.Errorf("Areas = %v, erwartet genau 3 ohne Dubletten", st.Areas)
	}
}

// ---------------------------------------------------------------- gkp-8ww

// schreibe legt eine areas.json mit genau diesem Inhalt an.
func schreibe(t *testing.T, pfad string, st State) {
	t.Helper()
	if err := Open(pfad).Save(st); err != nil {
		t.Fatal(err)
	}
}

// gelesen liest den Zustand zurueck und scheitert laut, statt einen halben
// Zustand weiterzureichen.
func gelesen(t *testing.T, pfad string) State {
	t.Helper()
	st, err := Open(pfad).Load()
	if err != nil {
		t.Fatal(err)
	}
	return st
}

// Anlegen traegt eine Area ein, OHNE ihr ein Projekt zuzuordnen. Genau das
// kann SetArea nicht: dort entsteht eine Area nur als Nebenwirkung einer
// Zuweisung, und eine Ansicht, die erst zuordnen muss, um einen Bereich
// anbieten zu koennen, dreht die Reihenfolge um, in der ein Mensch denkt.
func TestAnlegenTraegtDieAreaEinOhneProjekt(t *testing.T) {
	pfad := filepath.Join(t.TempDir(), "areas.json")
	s := Open(pfad)

	if err := s.Anlegen("  Infrastruktur  "); err != nil {
		t.Fatalf("Anlegen: %v", err)
	}
	st := gelesen(t, pfad)
	if len(st.Areas) != 1 || st.Areas[0] != "Infrastruktur" {
		t.Errorf("Areas = %q, erwartet [Infrastruktur] (getrimmt)", st.Areas)
	}
	if len(st.Zuordnung) != 0 {
		t.Errorf("Zuordnung = %v, erwartet leer — Anlegen ordnet nichts zu", st.Zuordnung)
	}

	// Zweimal dasselbe ist kein Fehler und kein zweiter Eintrag: "gibt es
	// schon" und "angelegt" sind fuer den Aufrufer dieselbe Tatsache.
	if err := s.Anlegen("Infrastruktur"); err != nil {
		t.Fatalf("zweites Anlegen: %v", err)
	}
	if st = gelesen(t, pfad); len(st.Areas) != 1 {
		t.Errorf("Areas = %q, erwartet genau einen Eintrag", st.Areas)
	}
}

// Leer und "Ohne Bereich" sind keine Areas. Die Sammel-Area entsteht beim
// Lesen und steht nie in der Datei — stuende sie doch darin, gaebe es zwei
// Eimer, die gleich heissen und sich verschieden verhalten.
func TestAnlegenLehntLeerUndSammelAreaAb(t *testing.T) {
	pfad := filepath.Join(t.TempDir(), "areas.json")
	s := Open(pfad)

	for _, name := range []string{"", "   ", OhneBereich} {
		err := s.Anlegen(name)
		if err == nil {
			t.Fatalf("Anlegen(%q) wurde angenommen", name)
		}
		if !errors.Is(err, ErrEingabe) {
			t.Errorf("Anlegen(%q): %v — erwartet ErrEingabe, sonst wird daraus ein Serverfehler statt einer Ablehnung", name, err)
		}
	}
	if _, err := os.Stat(pfad); !os.IsNotExist(err) {
		t.Error("eine abgelehnte Eingabe hat die Datei angelegt")
	}
}

// Umbenennen zieht die Zuordnungen MIT — sonst zeigte die Ansicht danach eine
// leere neue Area neben verwaisten Projekten unter einem Namen, den es nicht
// mehr gibt. Und die POSITION bleibt: die Reihenfolge in der Datei ist die
// Reihenfolge der Ansicht (BereicheGruppieren), ein Umbenennen ist kein
// Umsortieren.
func TestUmbenennenZiehtDieZuordnungenMit(t *testing.T) {
	pfad := filepath.Join(t.TempDir(), "areas.json")
	schreibe(t, pfad, State{
		Version: Version,
		Areas:   []string{"Infrastruktur", "Arbeit", "Privat"},
		Zuordnung: map[string]string{
			"alpha-tool": "Arbeit", "beta-lib": "Arbeit", "zeta-repo": "Infrastruktur",
		},
	})

	if err := Open(pfad).Umbenennen("Arbeit", "  Beruf  "); err != nil {
		t.Fatalf("Umbenennen: %v", err)
	}
	st := gelesen(t, pfad)
	if got := fmt.Sprint(st.Areas); got != "[Infrastruktur Beruf Privat]" {
		t.Errorf("Areas = %s, erwartet [Infrastruktur Beruf Privat] — die Stelle in der Liste ist die Stelle in der Ansicht", got)
	}
	for projekt, will := range map[string]string{
		"alpha-tool": "Beruf", "beta-lib": "Beruf", "zeta-repo": "Infrastruktur",
	} {
		if got := st.Zuordnung[projekt]; got != will {
			t.Errorf("Zuordnung[%s] = %q, erwartet %q", projekt, got, will)
		}
	}
}

// Umbenennen auf einen vorhandenen Namen verschmilzt die beiden Areas. Das ist
// erlaubt — aber es darf den Namen nicht ZWEIMAL in die Liste schreiben, sonst
// zeichnete die Ansicht zwei Gruppen mit demselben Titel.
func TestUmbenennenAufBestehendeAreaVerschmilzt(t *testing.T) {
	pfad := filepath.Join(t.TempDir(), "areas.json")
	schreibe(t, pfad, State{
		Version:   Version,
		Areas:     []string{"Arbeit", "Beruf"},
		Zuordnung: map[string]string{"alpha-tool": "Arbeit", "beta-lib": "Beruf"},
	})

	if err := Open(pfad).Umbenennen("Arbeit", "Beruf"); err != nil {
		t.Fatalf("Umbenennen: %v", err)
	}
	st := gelesen(t, pfad)
	if got := fmt.Sprint(st.Areas); got != "[Beruf]" {
		t.Errorf("Areas = %s, erwartet [Beruf] — ein Name, ein Eintrag", got)
	}
	if st.Zuordnung["alpha-tool"] != "Beruf" || st.Zuordnung["beta-lib"] != "Beruf" {
		t.Errorf("Zuordnung = %v, erwartet beide auf Beruf", st.Zuordnung)
	}
}

// Eine Area, die es nicht gibt, laesst sich nicht umbenennen. Ohne diese
// Ablehnung wuerde ein Tippfehler im alten Namen stillschweigend nichts tun und
// als Erfolg gemeldet — dieselbe Falschmeldung, gegen die DisallowUnknownFields
// in den Routen steht.
func TestUmbenennenUnbekannterAreaIstEingabefehler(t *testing.T) {
	pfad := filepath.Join(t.TempDir(), "areas.json")
	schreibe(t, pfad, State{Version: Version, Areas: []string{"Arbeit"}, Zuordnung: map[string]string{}})
	vorher, err := os.ReadFile(pfad)
	if err != nil {
		t.Fatal(err)
	}

	err = Open(pfad).Umbenennen("Arbiet", "Beruf")
	if err == nil {
		t.Fatal("Umbenennen einer unbekannten Area wurde angenommen")
	}
	if !errors.Is(err, ErrEingabe) {
		t.Errorf("Umbenennen: %v — erwartet ErrEingabe", err)
	}
	nachher, err := os.ReadFile(pfad)
	if err != nil {
		t.Fatal(err)
	}
	if string(vorher) != string(nachher) {
		t.Errorf("die Datei wurde trotz Ablehnung geschrieben:\n%s", nachher)
	}
}

// Der alte Name darf auch NUR in der Zuordnung stehen: die Datei wird von Hand
// gepflegt (Spec §4.3), und dann fehlt er in `areas`. Ein Umbenennen, das das
// nicht kennt, liesse sich auf genau die Areas nicht anwenden, die eine Pflege
// am noetigsten haben.
func TestUmbenennenKenntAuchNurZugeordneteArea(t *testing.T) {
	pfad := filepath.Join(t.TempDir(), "areas.json")
	schreibe(t, pfad, State{
		Version:   Version,
		Areas:     []string{},
		Zuordnung: map[string]string{"gamma-notes": "Privat"},
	})

	if err := Open(pfad).Umbenennen("Privat", "Zuhause"); err != nil {
		t.Fatalf("Umbenennen: %v", err)
	}
	st := gelesen(t, pfad)
	if st.Zuordnung["gamma-notes"] != "Zuhause" {
		t.Errorf("Zuordnung = %v, erwartet gamma-notes auf Zuhause", st.Zuordnung)
	}
	if got := fmt.Sprint(st.Areas); got != "[Zuhause]" {
		t.Errorf("Areas = %s, erwartet [Zuhause] — wer umbenennt, pflegt, und die Liste soll den Namen kennen", got)
	}
}

// ---------------------------------------------------------------- gkp-td3

// Loeschen nimmt eine LEERE Area aus der Liste — und laesst die Stelle der
// uebrigen unangetastet. Die Reihenfolge der Datei ist die der Ansicht
// (BereicheGruppieren); ein Loeschen ist kein Umsortieren.
//
// Behauptet wird die ganze FOLGE, nicht die Abwesenheit eines Namens: eine
// Behauptung "Arbeit steht nicht mehr drin" bliebe auch dann gruen, wenn die
// Operation die Liste leergeraeumt haette (Spec §9.1, Ursache 2).
func TestLoeschenNimmtDieLeereAreaAusDerListe(t *testing.T) {
	pfad := filepath.Join(t.TempDir(), "areas.json")
	schreibe(t, pfad, State{
		Version:   Version,
		Areas:     []string{"Infrastruktur", "Arbeit", "Privat"},
		Zuordnung: map[string]string{"zeta-repo": "Infrastruktur", "gamma-notes": "Privat"},
	})

	if err := Open(pfad).Loeschen("  Arbeit  "); err != nil {
		t.Fatalf("Loeschen: %v", err)
	}
	st := gelesen(t, pfad)
	if got := fmt.Sprint(st.Areas); got != "[Infrastruktur Privat]" {
		t.Errorf("Areas = %s, erwartet [Infrastruktur Privat] — die Stelle der uebrigen ist die Stelle in der Ansicht", got)
	}
	if got := fmt.Sprint(st.Zuordnung); got != "map[gamma-notes:Privat zeta-repo:Infrastruktur]" {
		t.Errorf("Zuordnung = %s — eine fremde Zuordnung wurde mitgeloescht", got)
	}
}

// DIE Entscheidung dieses Beads: eine BELEGTE Area laesst sich nicht loeschen.
//
// Die Zuordnung wird von Hand gepflegt (Spec §4.3, E2) und ist nirgends
// ableitbar — sie ist genau das, wovor der Kommentarkopf von loadLocked warnt.
// Ein Loeschen, das die Projekte still nach "Ohne Bereich" schiebt, vernichtet
// in einem Klick n Entscheidungen, und danach weiss niemand mehr, welche n es
// waren. Der umgekehrte Weg kostet einen Schritt je Projekt und ist bei jedem
// einzelnen umkehrbar.
//
// Zweiter Fall: eine Area, die NUR in der Zuordnung steht (die Datei wird von
// Hand editiert). Sie ist belegt, nicht unbekannt — waere sie unbekannt,
// bekaeme der Benutzer "gibt es nicht" zu einer Gruppe, die er vor sich sieht.
func TestLoeschenEinerBelegtenAreaWirdVerweigert(t *testing.T) {
	faelle := []struct {
		name string
		st   State
	}{
		{"in Liste und Zuordnung", State{
			Version:   Version,
			Areas:     []string{"Arbeit"},
			Zuordnung: map[string]string{"alpha-tool": "Arbeit", "beta-lib": "Arbeit"},
		}},
		{"nur in der Zuordnung", State{
			Version:   Version,
			Areas:     []string{},
			Zuordnung: map[string]string{"alpha-tool": "Arbeit", "beta-lib": "Arbeit"},
		}},
	}
	for _, f := range faelle {
		t.Run(f.name, func(t *testing.T) {
			pfad := filepath.Join(t.TempDir(), "areas.json")
			schreibe(t, pfad, f.st)
			vorher, err := os.ReadFile(pfad)
			if err != nil {
				t.Fatal(err)
			}

			err = Open(pfad).Loeschen("Arbeit")
			if err == nil {
				t.Fatal("das Loeschen einer belegten Area wurde angenommen")
			}
			if !errors.Is(err, ErrEingabe) {
				t.Errorf("Loeschen: %v — erwartet ErrEingabe, sonst wird daraus ein Serverfehler statt einer Ablehnung", err)
			}
			// Die Meldung muss die Projekte NENNEN. Eine Ablehnung ohne sie
			// waere eine Sackgasse: der Benutzer sieht die Gruppe womoeglich
			// zugeklappt und weiss nicht, was er erst wegraeumen soll.
			for _, projekt := range []string{"alpha-tool", "beta-lib"} {
				if !strings.Contains(err.Error(), projekt) {
					t.Errorf("Meldung %q nennt %q nicht — die Ablehnung sagt nicht, was im Weg steht", err, projekt)
				}
			}

			nachher, err := os.ReadFile(pfad)
			if err != nil {
				t.Fatal(err)
			}
			if string(vorher) != string(nachher) {
				t.Errorf("die Datei wurde trotz Ablehnung geschrieben:\n%s", nachher)
			}
		})
	}
}

// Die Sammel-Area ist kein Loeschkandidat: sie steht in keiner Datei, sondern
// entsteht beim Lesen (AreaOf). "Loeschen" haette dort kein Ziel — und der
// einzige denkbare Sinn waere "alle unzugeordneten Projekte verstecken", also
// das Gegenteil von Spec §4.3 ("sichtbar, damit die Pflege auffaellt").
//
// Der leere Name steht daneben, weil beide dieselbe Antwort brauchen: eine
// ABLEHNUNG, kein stilles Nichts.
func TestLoeschenLehntLeerenNamenUndDieSammelAreaAb(t *testing.T) {
	pfad := withAreasFile(t)
	s := Default()
	for _, name := range []string{"", "   ", OhneBereich} {
		err := s.Loeschen(name)
		if err == nil {
			t.Fatalf("Loeschen(%q) wurde angenommen", name)
		}
		if !errors.Is(err, ErrEingabe) {
			t.Errorf("Loeschen(%q): %v — erwartet ErrEingabe", name, err)
		}
	}
	if _, err := os.Stat(pfad); !os.IsNotExist(err) {
		t.Error("eine abgelehnte Eingabe hat die Datei angelegt")
	}
}

// Ein Tippfehler im Namen darf nicht als Erfolg zurueckkommen — dieselbe
// Falschmeldung, gegen die Umbenennen und DisallowUnknownFields stehen. Ein
// stillschweigendes Nichts saehe fuer den Benutzer aus wie ein geglueckter
// Loeschvorgang, und die Area stuende beim naechsten Aufbau wieder da.
func TestLoeschenUnbekannterAreaIstEingabefehler(t *testing.T) {
	pfad := filepath.Join(t.TempDir(), "areas.json")
	schreibe(t, pfad, State{Version: Version, Areas: []string{"Arbeit"}, Zuordnung: map[string]string{}})

	err := Open(pfad).Loeschen("Arbiet")
	if err == nil {
		t.Fatal("das Loeschen einer unbekannten Area wurde angenommen")
	}
	if !errors.Is(err, ErrEingabe) {
		t.Errorf("Loeschen: %v — erwartet ErrEingabe", err)
	}
	if got := fmt.Sprint(gelesen(t, pfad).Areas); got != "[Arbeit]" {
		t.Errorf("Areas = %s, erwartet [Arbeit] — die vorhandene Area wurde angefasst", got)
	}
}
