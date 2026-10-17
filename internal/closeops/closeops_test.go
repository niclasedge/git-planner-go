package closeops

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/niclasedge/git-planner-go/internal/bdops"
	"github.com/niclasedge/git-planner-go/internal/collectors"
	"github.com/niclasedge/git-planner-go/internal/reports"
)

// ---------------------------------------------------------------- attrappe

// bdAttrappe ersetzt bd. Sie haelt fest, WAS geschlossen worden waere, und —
// wichtiger — sie darf beim Aufruf in den Report-Store sehen. Genau daran
// haengt der Reihenfolge-Beweis unten: wenn bd laeuft, muss der Beleg bereits
// auf der Platte liegen.
type bdAttrappe struct {
	aufrufe []bdAufruf
	beim    func(a bdAufruf) // optional: laeuft IM Moment des close
	fehler  error

	// kartenfehler ist das Urteil der Kartenpruefung. nil heisst "die Karte
	// passt"; beim gepruefte merkt sich, WOMIT gefragt wurde, und beimPruefen
	// laeuft IM Moment der Pruefung — daran haengt der Reihenfolge-Beweis, dass
	// noch nichts geschrieben ist.
	kartenfehler   error
	kartenErgebnis bdops.Result
	geprueft       []kartenfrage
	beimPruefen    func()
}

type kartenfrage struct {
	Pfad  string
	Karte string
	Beads []string
}

func (b *bdAttrappe) PruefeKartenzugehoerigkeit(pfad, karte string, beads []string) (bdops.Result, error) {
	b.geprueft = append(b.geprueft, kartenfrage{Pfad: pfad, Karte: karte, Beads: append([]string(nil), beads...)})
	if b.beimPruefen != nil {
		b.beimPruefen()
	}
	return b.kartenErgebnis, b.kartenfehler
}

type bdAufruf struct {
	Pfad      string
	IDs       []string
	Grund     string
	Erzwingen bool
}

func (b *bdAttrappe) Close(pfad string, ids []string, grund string, erzwingen bool) (bdops.Result, error) {
	a := bdAufruf{Pfad: pfad, IDs: append([]string(nil), ids...), Grund: grund, Erzwingen: erzwingen}
	b.aufrufe = append(b.aufrufe, a)
	if b.beim != nil {
		b.beim(a)
	}
	if b.fehler != nil {
		return bdops.Result{Failed: true, Reason: bdops.ReasonExit}, b.fehler
	}
	return bdops.Result{}, nil
}

func (b *bdAttrappe) geschlossen() []string {
	var ids []string
	for _, a := range b.aufrufe {
		ids = append(ids, a.IDs...)
	}
	return ids
}

// ---------------------------------------------------------------- gerüst

// store richtet einen leeren, isolierten Report-Store ein und liefert den Pfad
// (cwd), mit dem der Dienst aufgerufen werden muss.
func store(t *testing.T) (pfad, jsonl string) {
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

// eintrag schreibt eine JSONL-Zeile direkt. Absichtlich an reports.Append
// vorbei: so haengt der Test am Tor und nicht an git, Bilddateien oder
// Magic-Bytes.
func eintrag(t *testing.T, jsonl, bead, kind, at string, belege ...map[string]any) {
	t.Helper()
	line, err := json.Marshal(reports.Entry{
		ID: at, At: at, Map: "karte", Bead: bead, Kind: kind, Title: "x", Proof: belege,
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

// pngDatei legt eine Datei an, die reports.copyImageProofs als Bild durchgehen
// laesst — die ersten vier Bytes entscheiden.
func pngDatei(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "schirm.png")
	if err := os.WriteFile(p, []byte("\x89PNG\r\n\x1a\nirgendwas"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func auftrag(pfad string, w ...Wunsch) Auftrag {
	return Auftrag{Pfad: pfad, Karte: "karte", Wuensche: w}
}

func grundVon(t *testing.T, err error) string {
	t.Helper()
	if err == nil {
		t.Fatal("ein Fehler wurde erwartet, kam aber keiner")
	}
	var f *Fehler
	if !errors.As(err, &f) {
		t.Fatalf("Fehler %v traegt keinen Grund", err)
	}
	return f.Grund
}

// ---------------------------------------------------------------- belegtor

// TestOhneBelegAbgelehnt: gar kein done-Eintrag — die Ansicht darf daraus kein
// geschlossenes Bead machen.
func TestOhneBelegAbgelehnt(t *testing.T) {
	pfad, _ := store(t)
	bd := &bdAttrappe{}
	_, err := NeuMit(bd).Schliessen(auftrag(pfad, Wunsch{Bead: "karte.1"}), nil)
	if g := grundVon(t, err); g != GrundKeinBeleg {
		t.Errorf("Grund = %q, erwartet %q (%v)", g, GrundKeinBeleg, err)
	}
	if len(bd.aufrufe) != 0 {
		t.Fatalf("bd wurde trotz Ablehnung aufgerufen: %+v", bd.aufrufe)
	}
}

// TestNurTextAbgelehnt: ein {cmd,out}-Beleg stammt von derselben Instanz, die
// "fertig" behauptet. Die Meldung muss eine EIGENE sein — der Fix ist ein
// anderer als bei "gar kein Beleg" (Bild nachreichen statt Report schreiben),
// und eine gemeinsame Meldung gaebe dem Aufrufer den falschen Rat.
func TestNurTextAbgelehnt(t *testing.T) {
	pfad, jsonl := store(t)
	eintrag(t, jsonl, "karte.1", "done", "2026-09-13T10:00:00Z",
		map[string]any{"cmd": "go test ./...", "out": "ok"})

	bd := &bdAttrappe{}
	_, err := NeuMit(bd).Schliessen(auftrag(pfad, Wunsch{Bead: "karte.1"}), nil)
	if g := grundVon(t, err); g != GrundKeinBild {
		t.Errorf("Grund = %q, erwartet %q (%v)", g, GrundKeinBild, err)
	}
	if strings.Contains(err.Error(), "kein aktueller done-Beleg") {
		t.Errorf("falsche Ebene gemeldet, der Bead HAT einen Beleg: %v", err)
	}
	if len(bd.aufrufe) != 0 {
		t.Fatalf("bd wurde trotz Ablehnung aufgerufen: %+v", bd.aufrufe)
	}
}

// TestMitBildAkzeptiert ist die Gegenprobe zu den beiden Ablehnungen.
func TestMitBildAkzeptiert(t *testing.T) {
	for _, beleg := range []map[string]any{
		{"image": "/irgendwo/schirm.png"},
		{"file": "docs/plan.dot"},
		{"file": "docs/plan.svg:12"},
	} {
		pfad, jsonl := store(t)
		eintrag(t, jsonl, "karte.1", "done", "2026-09-13T10:00:00Z", beleg)

		bd := &bdAttrappe{}
		erg, err := NeuMit(bd).Schliessen(auftrag(pfad, Wunsch{Bead: "karte.1", Grund: "fertig"}), nil)
		if err != nil {
			t.Fatalf("Beleg %v wurde abgelehnt: %v", beleg, err)
		}
		if got := bd.geschlossen(); len(got) != 1 || got[0] != "karte.1" {
			t.Fatalf("bd close = %v, erwartet [karte.1]", got)
		}
		if len(erg.Geschlossen) != 1 {
			t.Errorf("Ergebnis meldet %v als geschlossen", erg.Geschlossen)
		}
	}
}

// TestFehlendeBilddateiZaehltNicht ist zugleich der Beweis, dass Schritt 2 den
// Store WIRKLICH neu liest.
//
// Der mitgegebene Beleg sieht im Arbeitsspeicher tadellos aus:
// {"image": "/gibt/es/nicht.png"}. Erst reports.Append stellt fest, dass die
// Datei fehlt, und schreibt "missing": true dazu. Ein Tor, das Schritt 1
// glaubt, laesst diesen Aufruf durch; nur eines, das die gerade geschriebene
// Zeile zurueckliest, sieht die Markierung.
func TestFehlendeBilddateiZaehltNicht(t *testing.T) {
	pfad, _ := store(t)
	bd := &bdAttrappe{}
	_, err := NeuMit(bd).Schliessen(auftrag(pfad, Wunsch{
		Bead:    "karte.1",
		Bericht: &Bericht{Titel: "fertig", Belege: []reports.ProofItem{{"image": "/gibt/es/nicht.png"}}},
	}), nil)
	if g := grundVon(t, err); g != GrundKeinBild {
		t.Errorf("Grund = %q, erwartet %q (%v)", g, GrundKeinBild, err)
	}
	if len(bd.aufrufe) != 0 {
		t.Fatalf("ein Bild ohne Datei hat den Bead geschlossen: %+v", bd.aufrufe)
	}
}

// TestFehlendeDiagrammdateiZaehltNicht ist die Schwester von
// TestFehlendeBilddateiZaehltNicht — fuer den Datei-Beleg.
//
// Sie steht hier, weil genau diese Haelfte am TOR gefehlt hat. Gemessen mit
// einer Gegenprobe (CP17): nimmt man resolveFileProof die Markierung weg —
// ein nicht aufloesbarer {file}-Beleg wird nicht mehr als missing gefuehrt —,
// werden sieben Tests in reports/assets/mcp rot und closeops bleibt GRUEN. Der
// Bild-Fall war belegt, der Datei-Fall nicht, und das ist die ungleiche
// Behandlung, gegen die gkp-2ln gebaut wurde: {image} wurde kopiert und
// geprueft, {file} nur auf die Endung angesehen, also passierte
// {"file": "docs/gibt-es-nicht.svg"} das Tor auf die eigene Behauptung hin.
//
// Behauptet wird nicht nur "abgelehnt", sondern die DRITTE Meldung: der
// Aufrufer HAT etwas Sichtbares mitgegeben, der Store konnte es nur nicht
// aufloesen. "nichts zum Ansehen" schickte ihn los, ein Diagramm anzuhaengen,
// das er anzuhaengen glaubt.
func TestFehlendeDiagrammdateiZaehltNicht(t *testing.T) {
	for _, ref := range []string{"docs/gibt-es-nicht.dot", "docs/gibt-es-nicht.svg", "docs/gibt-es-nicht.svg:12"} {
		pfad, _ := store(t)
		bd := &bdAttrappe{}
		_, err := NeuMit(bd).Schliessen(auftrag(pfad, Wunsch{
			Bead:    "karte.1",
			Bericht: &Bericht{Titel: "fertig", Belege: []reports.ProofItem{{"file": ref}}},
		}), nil)
		if g := grundVon(t, err); g != GrundKeinBild {
			t.Errorf("%s: Grund = %q, erwartet %q (%v)", ref, g, GrundKeinBild, err)
		}
		if !strings.Contains(err.Error(), "nicht auflösen konnte") {
			t.Errorf("%s: nicht die Meldung fuer den verschwundenen Beleg: %v", ref, err)
		}
		if len(bd.aufrufe) != 0 {
			t.Errorf("%s: ein Diagramm ohne Datei hat den Bead geschlossen: %+v", ref, bd.aufrufe)
		}
	}
}

// TestAblehnungNenntDenKonkretenGrund: hat der Store beim Schreiben
// festgehalten, WORAN der Beleg scheiterte, sagt die Ablehnung es.
//
// MeldungBelegVerschwunden zaehlt sonst alle vier Ursachen auf ("nicht da,
// leer, zu gross oder kein erkanntes Bildformat") und laesst den abgelehnten
// Agenten raten, welche er getroffen hat. Zwei davon fuehren zu voellig
// verschiedenen Handgriffen: einen Pfad korrigieren oder ein leeres Diagramm
// wirklich rendern.
func TestAblehnungNenntDenKonkretenGrund(t *testing.T) {
	pfad, _ := store(t)
	bd := &bdAttrappe{}
	_, err := NeuMit(bd).Schliessen(auftrag(pfad, Wunsch{
		Bead:    "karte.1",
		Bericht: &Bericht{Titel: "fertig", Belege: []reports.ProofItem{{"file": "docs/gibt-es-nicht.dot"}}},
	}), nil)
	if g := grundVon(t, err); g != GrundKeinBild {
		t.Fatalf("Grund = %q, erwartet %q (%v)", g, GrundKeinBild, err)
	}
	if !strings.Contains(err.Error(), "unter keinem der Bezugsverzeichnisse") {
		t.Errorf("die Ablehnung nennt den festgehaltenen Grund nicht: %v", err)
	}
	if len(bd.aufrufe) != 0 {
		t.Fatalf("trotz Ablehnung geschlossen: %+v", bd.aufrufe)
	}
}

// TestAblehnungOhneFestgehaltenenGrundBleibtVollstaendig ist die andere Haelfte:
// der Store ist append-only, und jeder Beleg aus der Zeit vor dem reason-Feld
// traegt keinen. Dann muss die Aufzaehlung der vier Ursachen stehenbleiben —
// eine Meldung, die den Grund nur noch als Leerstelle zeigt, waere schlechter
// als die alte.
func TestAblehnungOhneFestgehaltenenGrundBleibtVollstaendig(t *testing.T) {
	pfad, jsonl := store(t)
	// Direkt in die JSONL, an reports.Append vorbei: genau die Form, die ein
	// alter Eintrag hat — missing ohne reason.
	eintrag(t, jsonl, "karte.1", "done", "2026-09-13T10:00:00Z",
		map[string]any{"file": "docs/alt.svg", "missing": true})

	bd := &bdAttrappe{}
	_, err := NeuMit(bd).Schliessen(auftrag(pfad, Wunsch{Bead: "karte.1", Grund: "fertig"}), nil)
	if g := grundVon(t, err); g != GrundKeinBild {
		t.Fatalf("Grund = %q, erwartet %q (%v)", g, GrundKeinBild, err)
	}
	if !strings.Contains(err.Error(), "nicht da, leer, zu gross oder kein erkanntes Bildformat") {
		t.Errorf("ohne festgehaltenen Grund muss die Aufzaehlung bleiben: %v", err)
	}
	if len(bd.aufrufe) != 0 {
		t.Fatalf("trotz Ablehnung geschlossen: %+v", bd.aufrufe)
	}
}

// ------------------------------------------------------- lebenszyklus (reopen)

// TestReopenEntwertetDenFruehererBeleg haelt die Lebenszyklus-Grenze AM TOR
// fest.
//
// Die Regel steht woertlich in MeldungKeinBeleg — "ein done-Eintrag zaehlt nur,
// wenn er nach dem letzten plan-Eintrag liegt" —, gesichert war sie bisher nur
// eine Schicht tiefer (reports.hasCurrentDone) und in der Ansicht. Gemessene
// Gegenprobe (CP3): entfernt man in evidence.go die Grenze `e.At >= lastPlanAt`,
// werden drei Tests in internal/reports und zwei in internal/appctx rot —
// closeops und mcp bleiben komplett GRUEN. Man haette die Reopen-Regel also aus
// dem Tor herausloesen koennen, ohne dass ein einziger Torwaerter-Test es
// bemerkt.
//
// Der Test geht deshalb den vollen Weg (Dienst.Schliessen), nicht die
// Hilfsfunktion, und behauptet ueber den Doppel, dass wirklich die Beleg-Regel
// gegriffen hat: bd wurde NICHT gerufen. Nur auf `err != nil` zu pruefen waere
// wertlos — in einem Wegwerf-Verzeichnis scheitert bd ohnehin (gkp-few,
// Gegenprobe E). Hier gibt es kein echtes bd, aber der Grund wird trotzdem
// benannt, damit ein Store- oder Eingabefehler nicht als Erfolg durchgeht.
//
// Der zweite Fall ist die Gegenprobe im Test selbst: DIESELBE Vorgeschichte
// ohne den nachgestellten plan muss schliessen. Ohne ihn koennte der Test auch
// gruen sein, weil ueberhaupt nie etwas geschlossen wird.
func TestReopenEntwertetDenFruehererBeleg(t *testing.T) {
	faelle := []struct {
		name      string
		reopen    bool
		willGrund string
	}{
		{"plan, dann done mit Bild", false, ""},
		{"plan, done mit Bild, dann plan — der Bead ist wieder offen", true, GrundKeinBeleg},
	}
	for _, f := range faelle {
		t.Run(f.name, func(t *testing.T) {
			pfad, jsonl := store(t)
			eintrag(t, jsonl, "karte.1", "plan", "2026-09-13T09:00:00Z")
			eintrag(t, jsonl, "karte.1", "done", "2026-09-13T10:00:00Z",
				map[string]any{"image": "/irgendwo/schirm.png"})
			if f.reopen {
				eintrag(t, jsonl, "karte.1", "plan", "2026-09-13T11:00:00Z")
			}

			bd := &bdAttrappe{}
			_, err := NeuMit(bd).Schliessen(auftrag(pfad, Wunsch{Bead: "karte.1", Grund: "fertig"}), nil)

			if f.willGrund == "" {
				if err != nil {
					t.Fatalf("ein done nach dem einzigen plan muss schliessen: %v", err)
				}
				if got := bd.geschlossen(); len(got) != 1 || got[0] != "karte.1" {
					t.Fatalf("bd close = %v, erwartet [karte.1]", got)
				}
				return
			}
			if g := grundVon(t, err); g != f.willGrund {
				t.Errorf("Grund = %q, erwartet %q (%v)", g, f.willGrund, err)
			}
			// Nicht GrundKeinBild: der Beleg ist nicht unsichtbar, er gehoert zu
			// einem abgeschlossenen Zyklus. Der Fix ist ein neuer Bericht, kein
			// nachgereichtes Bild — zwei verschiedene Meldungen.
			if !strings.Contains(err.Error(), "nach dem letzten plan-Eintrag") {
				t.Errorf("die Meldung nennt die Lebenszyklus-Grenze nicht: %v", err)
			}
			if len(bd.aufrufe) != 0 {
				t.Fatalf("ein Beleg aus dem vorigen Zyklus hat den Bead geschlossen: %+v", bd.aufrufe)
			}
		})
	}
}

// TestBelegUnterAndererKarteZaehltNicht ist die Lese-Gegenprobe zu gkp-few
// (dessen Fix, bdops.CheckMapMembership, VOR Schritt 1 sitzt und den Bead hier
// per Attrappe ohnehin durchwinkt — geprueft wird Schritt 2, nicht Schritt 0).
// Ein gueltiger done-Beleg fuer denselben Bead liegt absichtlich unter einer
// ANDEREN Akte (etwa weil eine alte, laengst nachgebesserte Fehlablage wie
// gkp-yy5.jsonl liegen bleibt — der Store ist append-only, docs/spec-
// aufgaben-dashboard.md §7.1). Schliessen darf das nicht als Beleg fuer DIESE
// Karte zaehlen: Schritt 2 liest gezielt reports.LoadMap(repo, a.Karte), nicht
// reports.LoadBead ueber alle Karten hinweg (siehe Kommentar an der Aufrufstelle
// in closeops.go). Gegenprobe gefahren: mit reports.ListMaps+Filter statt
// LoadMap wird dieser Test rot.
func TestBelegUnterAndererKarteZaehltNicht(t *testing.T) {
	pfad, jsonl := store(t)
	fremdeAkte := filepath.Join(filepath.Dir(jsonl), "andere-karte.jsonl")
	eintrag(t, fremdeAkte, "karte.1", "done", "2026-01-01T00:00:00Z",
		map[string]any{"image": "/irgendwo/schirm.png"})

	bd := &bdAttrappe{}
	_, err := NeuMit(bd).Schliessen(auftrag(pfad, Wunsch{Bead: "karte.1"}), nil)

	if g := grundVon(t, err); g != GrundKeinBeleg {
		t.Errorf("Grund = %q, erwartet %q — der Beleg lag unter einer anderen Karte und darf hier nicht zaehlen: %v",
			g, GrundKeinBeleg, err)
	}
	if len(bd.aufrufe) != 0 {
		t.Fatalf("bd wurde trotz fehlplatziertem Beleg aufgerufen: %+v", bd.aufrufe)
	}
}

// ---------------------------------------------------------------- reihenfolge

// TestBelegWirdVorDerPruefungGeschrieben haelt Schritt 1 VOR Schritt 3 fest:
// der Store ist leer, der Beleg kommt im selben Aufruf mit. Wer zuerst pruefte
// und dann schriebe, muesste diesen Aufruf ablehnen — es gibt ja noch nichts
// zu sehen.
func TestBelegWirdVorDerPruefungGeschrieben(t *testing.T) {
	pfad, jsonl := store(t)
	bd := &bdAttrappe{}
	erg, err := NeuMit(bd).Schliessen(auftrag(pfad, Wunsch{
		Bead:    "karte.1",
		Grund:   "gebaut",
		Bericht: &Bericht{Titel: "fertig", Belege: []reports.ProofItem{{"image": pngDatei(t)}}},
	}), nil)
	if err != nil {
		t.Fatalf("ein mitgegebener Bildbeleg muss reichen: %v", err)
	}
	if len(erg.Geschrieben) != 1 || erg.Geschrieben[0] != "karte.1" {
		t.Errorf("Geschrieben = %v", erg.Geschrieben)
	}
	if _, err := os.Stat(jsonl); err != nil {
		t.Errorf("kein Beleg auf der Platte: %v", err)
	}
}

// TestBdSiehtDenBelegBereits ist der eigentliche Reihenfolge-Test.
//
// Die Attrappe liest IM Moment des bd-Aufrufs den Store von der Platte. Steht
// die done-Zeile dann schon da, kann der Beleg nicht nach dem Schliessen
// geschrieben worden sein — und zwar unabhaengig davon, was der Code behauptet.
// Ein Test, der nur das Ergebnis prueft, waere auch bei vertauschter
// Reihenfolge gruen.
func TestBdSiehtDenBelegBereits(t *testing.T) {
	pfad, jsonl := store(t)
	var beimSchliessen []reports.Entry
	bd := &bdAttrappe{beim: func(bdAufruf) {
		e, _, err := reports.LoadMap(collectors.ReportsRepoName(pfad), "karte")
		if err != nil {
			t.Errorf("Store beim Schliessen nicht lesbar: %v", err)
		}
		beimSchliessen = e
	}}
	_, err := NeuMit(bd).Schliessen(auftrag(pfad, Wunsch{
		Bead:    "karte.1",
		Grund:   "gebaut",
		Bericht: &Bericht{Titel: "fertig", Belege: []reports.ProofItem{{"image": pngDatei(t)}}},
	}), nil)
	if err != nil {
		t.Fatalf("Schliessen fehlgeschlagen: %v", err)
	}
	if len(beimSchliessen) != 1 {
		t.Fatalf("bd lief mit %d Belegen im Store, erwartet 1 — der Beleg wurde nicht VOR dem Schliessen geschrieben (%s)",
			len(beimSchliessen), jsonl)
	}
	if !reports.HasValidatedDoneEntry(beimSchliessen) {
		t.Errorf("der Beleg, den bd vorfand, haelt der Pruefung nicht stand: %+v", beimSchliessen)
	}
}

// TestBelegschreibenScheitertLaesstBeadOffen haelt die andere Haelfte derselben
// Reihenfolge fest: schlaegt Schritt 1 fehl, darf Schritt 3 nicht laufen. Sonst
// stuende ein geschlossener Bead ohne jede Erklaerung da — genau der Zustand,
// gegen den dieses Paket gebaut ist.
//
// Der Fehlschlag wird erzwungen, indem das Verzeichnis des Stores eine DATEI
// ist: reports.Append scheitert dann beim MkdirAll.
func TestBelegschreibenScheitertLaesstBeadOffen(t *testing.T) {
	basis := filepath.Join(t.TempDir(), "basis")
	if err := os.WriteFile(basis, []byte("keine Verzeichnis"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CC_REPORTS_DIR", basis)
	pfad := t.TempDir()

	bd := &bdAttrappe{}
	_, err := NeuMit(bd).Schliessen(auftrag(pfad, Wunsch{
		Bead:    "karte.1",
		Bericht: &Bericht{Titel: "fertig", Belege: []reports.ProofItem{{"image": pngDatei(t)}}},
	}), nil)
	if err == nil {
		t.Fatal("ein nicht schreibbarer Store muss den Aufruf scheitern lassen")
	}
	if !strings.Contains(err.Error(), "bleibt offen") {
		t.Errorf("die Meldung sagt nicht, dass der Bead offen bleibt: %v", err)
	}
	if len(bd.aufrufe) != 0 {
		t.Fatalf("bd lief, obwohl kein Beleg geschrieben werden konnte: %+v", bd.aufrufe)
	}
}

// ---------------------------------------------------------------- force

// TestErzwingenBrauchtGrund: force ist der Ausweg fuer "wird nicht gemacht" —
// ohne Grund ist es nur ein stiller Weg am Tor vorbei.
func TestErzwingenBrauchtGrund(t *testing.T) {
	pfad, _ := store(t)
	bd := &bdAttrappe{}
	a := auftrag(pfad, Wunsch{Bead: "karte.1"})
	a.Erzwingen = true
	_, err := NeuMit(bd).Schliessen(a, nil)
	if g := grundVon(t, err); g != GrundEingabe {
		t.Errorf("Grund = %q, erwartet %q (%v)", g, GrundEingabe, err)
	}
	if len(bd.aufrufe) != 0 {
		t.Fatalf("bd lief ohne Grund: %+v", bd.aufrufe)
	}
}

// TestErzwingenLehntAusredeAb: "kein Bild zur Hand" benennt den Fehlbetrag,
// nicht den Grund. Das haelt keinen Luegner auf — es haelt den ehrlichen
// Reflex auf, und genau der ist der haeufige Fall.
func TestErzwingenLehntAusredeAb(t *testing.T) {
	for _, grund := range []string{"kein Bild zur Hand", "no screenshot available", "habe keinen Beleg"} {
		pfad, _ := store(t)
		bd := &bdAttrappe{}
		a := auftrag(pfad, Wunsch{Bead: "karte.1", Grund: grund})
		a.Erzwingen = true
		_, err := NeuMit(bd).Schliessen(a, nil)
		if g := grundVon(t, err); g != GrundEingabe {
			t.Errorf("%q: Grund = %q, erwartet %q (%v)", grund, g, GrundEingabe, err)
		}
		if len(bd.aufrufe) != 0 {
			t.Errorf("%q: bd lief trotzdem: %+v", grund, bd.aufrufe)
		}
	}
}

// TestErzwingenMitGrundSchliesst: der Ausweg muss offen bleiben, sonst waere
// ein "wird nicht gemacht" nicht mehr abzuschliessen.
func TestErzwingenMitGrundSchliesst(t *testing.T) {
	pfad, _ := store(t)
	bd := &bdAttrappe{}
	a := auftrag(pfad, Wunsch{Bead: "karte.1", Grund: "Anforderung entfallen, siehe gkp-4e1"})
	a.Erzwingen = true
	erg, err := NeuMit(bd).Schliessen(a, nil)
	if err != nil {
		t.Fatalf("force mit Grund muss schliessen: %v", err)
	}
	if len(bd.aufrufe) != 1 || !bd.aufrufe[0].Erzwingen {
		t.Fatalf("bd-Aufrufe = %+v", bd.aufrufe)
	}
	if len(erg.OhneBeleg) != 1 {
		t.Errorf("das Ergebnis verschweigt, dass ohne Beleg geschlossen wurde: %+v", erg)
	}
}

// ---------------------------------------------------------------- eingaben

func TestEingabenAbgelehnt(t *testing.T) {
	faelle := []struct {
		name string
		a    Auftrag
		will string
	}{
		{"kein pfad", Auftrag{Karte: "karte", Wuensche: []Wunsch{{Bead: "karte.1"}}}, "pfad"},
		{"keine karte", Auftrag{Pfad: "/tmp", Wuensche: []Wunsch{{Bead: "karte.1"}}}, "karte"},
		{"nichts zu schliessen", Auftrag{Pfad: "/tmp", Karte: "karte"}, "kein bead"},
		{"flag-artige id", auftrag("/tmp", Wunsch{Bead: "--force"}), "ungültige"},
		{"bericht ohne titel", auftrag("/tmp", Wunsch{Bead: "karte.1", Bericht: &Bericht{}}), "titel"},
	}
	for _, f := range faelle {
		t.Run(f.name, func(t *testing.T) {
			bd := &bdAttrappe{}
			_, err := NeuMit(bd).Schliessen(f.a, nil)
			if g := grundVon(t, err); g != GrundEingabe {
				t.Errorf("Grund = %q, erwartet %q", g, GrundEingabe)
			}
			if !strings.Contains(strings.ToLower(err.Error()), f.will) {
				t.Errorf("Meldung %q nennt %q nicht", err, f.will)
			}
			if len(bd.aufrufe) != 0 {
				t.Errorf("bd lief trotz abgelehnter Eingabe: %+v", bd.aufrufe)
			}
		})
	}
}

// TestNachGrundGruppiert: gleiche Begruendung, ein Aufruf. Zwei Begruendungen,
// zwei Aufrufe — bd bildet mehrere --reason positionell ab, und diese Kopplung
// bleibt bewusst aussen vor (bdops.closeArgs).
func TestNachGrundGruppiert(t *testing.T) {
	pfad, jsonl := store(t)
	for _, b := range []string{"karte.1", "karte.2", "karte.3"} {
		eintrag(t, jsonl, b, "done", "2026-09-13T10:00:00Z", map[string]any{"file": "docs/x.dot"})
	}
	bd := &bdAttrappe{}
	_, err := NeuMit(bd).Schliessen(auftrag(pfad,
		Wunsch{Bead: "karte.1", Grund: "a"},
		Wunsch{Bead: "karte.2", Grund: "b"},
		Wunsch{Bead: "karte.3", Grund: "a"},
	), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(bd.aufrufe) != 2 {
		t.Fatalf("%d bd-Aufrufe, erwartet 2: %+v", len(bd.aufrufe), bd.aufrufe)
	}
	if strings.Join(bd.aufrufe[0].IDs, ",") != "karte.1,karte.3" {
		t.Errorf("erste Gruppe = %v", bd.aufrufe[0].IDs)
	}
}

// TestMeldungNachJedemGeschriebenenBeleg: die Rueckmeldung an bd (Kommentar mit
// der Report-URL) haengt am geschriebenen Eintrag, nicht am geglueckten
// Schliessen — der Eintrag steht ja bereits, auch wenn das Tor danach ablehnt.
func TestMeldungNachJedemGeschriebenenBeleg(t *testing.T) {
	pfad, _ := store(t)
	var gemeldet []string
	bd := &bdAttrappe{}
	_, err := NeuMit(bd).Schliessen(auftrag(pfad, Wunsch{
		Bead:    "karte.1",
		Bericht: &Bericht{Titel: "fertig", Belege: []reports.ProofItem{{"cmd": "go test", "out": "ok"}}},
	}), func(bead string, e reports.Entry) {
		gemeldet = append(gemeldet, bead+"/"+e.Kind)
	})
	if grundVon(t, err) != GrundKeinBild {
		t.Fatalf("erwartet wurde die Ablehnung ohne Bild, kam: %v", err)
	}
	if len(gemeldet) != 1 || gemeldet[0] != "karte.1/done" {
		t.Errorf("Meldungen = %v, erwartet [karte.1/done]", gemeldet)
	}
}

// ------------------------------------------------------- kartentor (gkp-few)

// dateienImStore zaehlt die JSONL-Dateien, die unter CC_REPORTS_DIR liegen —
// ueber ALLE Repo-Verzeichnisse, nicht nur das erwartete. Genau darum geht es:
// eine falsche Karte legt eine NEUE Datei an, und die Frage ist nicht, ob die
// richtige gewachsen ist, sondern ob ueberhaupt irgendetwas entstand.
func dateienImStore(t *testing.T) []string {
	t.Helper()
	wurzel := os.Getenv("CC_REPORTS_DIR")
	if wurzel == "" {
		t.Fatal("CC_REPORTS_DIR ist nicht gesetzt — dieser Test darf den echten Store nie sehen")
	}
	var gefunden []string
	err := filepath.WalkDir(wurzel, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(p, ".jsonl") {
			gefunden = append(gefunden, p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return gefunden
}

// TestErfundeneKarteSchreibtNichtsUndSchliesstNichts ist gkp-few, gemessen am
// 2026-09-14: ein Aufruf mit map "gkp-yy5" — einer Id, die es nicht gibt —
// legte die Datei gkp-yy5.jsonl an, schrieb die Belege hinein, prueffte GEGEN
// GENAU DIESE DATEI und schloss beide Beads. Erfolgsmeldung inklusive.
//
// Die zwei Behauptungen sind bewusst beide da und nicht nur eine:
//
//   - bd wurde NICHT gerufen, der Bead bleibt also offen. Das ist der Teil, den
//     ein Mensch bemerkt.
//   - im Store liegt KEINE Datei. Das ist der Teil, der nicht mehr zu
//     reparieren waere: der Store ist append-only, eine falsch abgelegte Zeile
//     laesst sich nicht zuruecknehmen. Deshalb muss die Pruefung vor dem
//     Schreiben sitzen und nicht davor, dass geschlossen wird.
func TestErfundeneKarteSchreibtNichtsUndSchliesstNichts(t *testing.T) {
	pfad, _ := store(t)
	bd := &bdAttrappe{kartenfehler: errors.New(
		`karte "gkp-yy5" gibt es in dieser Datenbank nicht`)}

	_, err := NeuMit(bd).Schliessen(Auftrag{
		Pfad: pfad, Karte: "gkp-yy5",
		Wuensche: []Wunsch{{
			Bead:    "gkp-3oh",
			Bericht: &Bericht{Titel: "fertig", Belege: []reports.ProofItem{{"image": pngDatei(t)}}},
		}},
	}, nil)

	if err == nil {
		t.Fatal("eine erfundene Karte wurde durchgelassen")
	}
	if g := grundVon(t, err); g != GrundEingabe {
		t.Errorf("Grund = %q, erwartet %q (%v)", g, GrundEingabe, err)
	}
	if len(bd.aufrufe) != 0 {
		t.Errorf("bd close wurde trotz falscher Karte gerufen: %+v", bd.aufrufe)
	}
	if dateien := dateienImStore(t); len(dateien) != 0 {
		t.Errorf("es wurde in den Store geschrieben, obwohl die Karte nicht stimmt: %v", dateien)
	}
}

// TestKartenpruefungLaeuftVorDemErstenSchreibzugriff haelt die REIHENFOLGE
// fest, nicht nur das Ergebnis. Die Attrappe sieht im Moment der Pruefung in
// den Store: liegt dort schon etwas, sass die Pruefung zu spaet — und zu spaet
// heisst in einem append-only Store "gar nicht", weil der Eintrag bleibt.
//
// Deshalb laesst dieser Fall die Pruefung BESTEHEN: eine Pruefung, die ablehnt,
// kann auch dann noch richtig aussehen, wenn sie nach dem Schreiben liegt.
func TestKartenpruefungLaeuftVorDemErstenSchreibzugriff(t *testing.T) {
	pfad, _ := store(t)
	var beimPruefenGefunden []string
	bd := &bdAttrappe{beimPruefen: func() {
		beimPruefenGefunden = dateienImStore(t)
	}}

	_, err := NeuMit(bd).Schliessen(Auftrag{
		Pfad: pfad, Karte: "karte",
		Wuensche: []Wunsch{{
			Bead:    "karte.1",
			Grund:   "fertig",
			Bericht: &Bericht{Titel: "fertig", Belege: []reports.ProofItem{{"image": pngDatei(t)}}},
		}},
	}, nil)
	if err != nil {
		t.Fatalf("bestandene Kartenpruefung hat trotzdem abgelehnt: %v", err)
	}
	if len(bd.geprueft) != 1 {
		t.Fatalf("die Kartenpruefung lief %d mal, erwartet genau einmal: %+v", len(bd.geprueft), bd.geprueft)
	}
	if len(beimPruefenGefunden) != 0 {
		t.Errorf("beim Pruefen lag bereits im Store: %v — die Pruefung sitzt hinter dem Schreiben", beimPruefenGefunden)
	}
	// Und danach muss sehr wohl etwas dastehen, sonst misst der Test oben nur,
	// dass ueberhaupt nie geschrieben wird.
	if danach := dateienImStore(t); len(danach) != 1 {
		t.Errorf("nach dem Lauf liegen %d Dateien im Store, erwartet 1: %v", len(danach), danach)
	}
}

// TestKartenpruefungSiehtAlleBeadsDesAuftrags: gefragt wird einmal, aber mit
// allen Beads. Ein Auftrag, bei dem nur der erste Bead geprueft wird, liesse
// den zweiten durch — und der zweite ist der, den man beim Tippen vergisst.
func TestKartenpruefungSiehtAlleBeadsDesAuftrags(t *testing.T) {
	pfad, jsonl := store(t)
	for _, b := range []string{"karte.1", "karte.2"} {
		eintrag(t, jsonl, b, "done", "2026-09-13T10:00:00Z", map[string]any{"file": "docs/plan.dot"})
	}
	bd := &bdAttrappe{}
	if _, err := NeuMit(bd).Schliessen(auftrag(pfad,
		Wunsch{Bead: "karte.1", Grund: "fertig"},
		Wunsch{Bead: "karte.2", Grund: "fertig"},
	), nil); err != nil {
		t.Fatalf("unerwartete Ablehnung: %v", err)
	}
	if len(bd.geprueft) != 1 {
		t.Fatalf("Pruefungen = %d, erwartet 1: %+v", len(bd.geprueft), bd.geprueft)
	}
	f := bd.geprueft[0]
	if f.Pfad != pfad || f.Karte != "karte" {
		t.Errorf("gefragt wurde mit Pfad %q / Karte %q", f.Pfad, f.Karte)
	}
	if strings.Join(f.Beads, ",") != "karte.1,karte.2" {
		t.Errorf("gefragte Beads = %v, erwartet [karte.1 karte.2]", f.Beads)
	}
}

// TestUnpruefbareKarteLehntAbUndBehaeltBdsGrund: antwortet bd gar nicht
// (Zeitueberschreitung, kein bd auf dem PATH), ist die Karte nicht bestaetigt —
// und unbestaetigt heisst hier NICHT "durchlassen". Der Grund bleibt bds
// eigener, damit die Route ihn weiter auf 504 bzw. 500 abbilden kann statt auf
// 400: an der Anfrage war nichts falsch, die Auskunft fehlt.
func TestUnpruefbareKarteLehntAbUndBehaeltBdsGrund(t *testing.T) {
	pfad, _ := store(t)
	bd := &bdAttrappe{
		kartenfehler:   errors.New("bd show: Zeitueberschreitung"),
		kartenErgebnis: bdops.Result{Failed: true, Reason: bdops.ReasonTimeout},
	}
	_, err := NeuMit(bd).Schliessen(Auftrag{
		Pfad: pfad, Karte: "karte",
		Wuensche: []Wunsch{{Bead: "karte.1",
			Bericht: &Bericht{Titel: "fertig", Belege: []reports.ProofItem{{"image": pngDatei(t)}}}}},
	}, nil)
	if err == nil {
		t.Fatal("eine unpruefbare Karte wurde durchgelassen")
	}
	if g := grundVon(t, err); g != bdops.ReasonTimeout {
		t.Errorf("Grund = %q, erwartet %q (%v)", g, bdops.ReasonTimeout, err)
	}
	if dateien := dateienImStore(t); len(dateien) != 0 {
		t.Errorf("unpruefbar, aber geschrieben: %v", dateien)
	}
}
