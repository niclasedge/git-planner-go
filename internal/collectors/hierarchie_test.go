package collectors

// Beliebig tiefe Unteraufgaben (gkp-1lp, E5 der Spec). Der Collector endete
// vorher nach zwei Ebenen: BeadsSubBead hatte kein Kinder-Feld, und der
// Batch-Aufruf lief genau einmal ueber die Epic-Liste. Enkel waren damit nicht
// unsichtbar, sondern gar nicht erst geholt.
//
// Was hier gepruefet wird, ist nicht bd, sondern die FORM des Abstiegs:
//   - ein Enkel kommt an und haengt am richtigen Elternteil,
//   - die Zahl der bd-Aufrufe waechst mit der TIEFE, nicht mit der Zahl der
//     Beads (je Ebene ein Aufruf ueber alle Ids dieser Ebene),
//   - ein Zyklus laeuft nicht endlos,
//   - die Tiefe ist nicht begrenzt.
//
// Das Fixture muss die Tiefe kuenstlich herstellen: am echten Bestand ist sie
// nicht messbar, dieses Repo hatte am 2026-09-13 keinen einzigen Enkel (0 von
// 99 Beads).

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// bdBaumFake ist ein bd, das eine beliebig tiefe Hierarchie kennt.
//
// Anders als fakeBdMitKindern (epics_test.go, zwei Ebenen) beantwortet es auch
// `bd list --status all` — den Bestandsabruf, aus dem der Collector ablesen
// darf, WELCHE Ids ueberhaupt Kinder haben. Ohne diese Antwort muss er blind
// eine Ebene tiefer fragen; genau diesen Unterschied misst
// TestOhneBestandWirdBlindAbgestiegen.
type bdBaumFake struct {
	epics []string            // Wurzeln vom Typ epic
	baum  map[string][]string // Elternteil -> direkte Kinder, beliebig tief
	lose  []string            // Beads ohne Epic (bdListLoose)
	ready []string            // Ids, die bd als startklar meldet
	// ohneBestand laesst `bd list --status all` Muell liefern: der Hinweis
	// faellt aus, der Abstieg muss trotzdem vollstaendig bleiben.
	ohneBestand bool
}

// alleIDs sammelt jede Id, die im Fixture vorkommt — Wurzeln wie Nachfahren.
func (f bdBaumFake) alleIDs() []string {
	gesehen := map[string]bool{}
	var ids []string
	nimm := func(id string) {
		if !gesehen[id] {
			gesehen[id] = true
			ids = append(ids, id)
		}
	}
	for _, e := range f.epics {
		nimm(e)
	}
	for _, l := range f.lose {
		nimm(l)
	}
	for eltern, kinder := range f.baum {
		nimm(eltern)
		for _, k := range kinder {
			nimm(k)
		}
	}
	return ids
}

// elternVon dreht f.baum um: Kind -> Elternteil, so wie bd es im Feld `parent`
// fuehrt. Die Hierarchie steckt dort und NICHT im Punkt der Id — gkp-ko0 hat
// keinen Punkt und ist trotzdem Kind von gkp-ar7 (28 von 70 Beads wichen ab).
func (f bdBaumFake) elternVon() map[string]string {
	eltern := map[string]string{}
	for e, kinder := range f.baum {
		for _, k := range kinder {
			eltern[k] = e
		}
	}
	return eltern
}

// installieren schreibt die Fixtures und legt bd auf den PATH. Rueckgabe ist
// der Pfad des Argumentprotokolls.
func (f bdBaumFake) installieren(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "args.log")
	schreib := func(name, inhalt string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(inhalt), 0o644); err != nil {
			t.Fatalf("Fixture %s: %v", name, err)
		}
	}

	readyJSON := make([]string, 0, len(f.ready))
	for _, r := range f.ready {
		readyJSON = append(readyJSON, fmt.Sprintf(`{"id":%q}`, r))
	}
	schreib("ready.json", "["+strings.Join(readyJSON, ",")+"]")

	for eltern, kinder := range f.baum {
		arr := make([]string, 0, len(kinder))
		for _, k := range kinder {
			arr = append(arr, kind(k, "open"))
		}
		schreib("kinder-"+eltern+".json", "["+strings.Join(arr, ",")+"]")
	}

	// Seit gkp-mnd ist der Bestand die EINZIGE Liste, die der Collector holt:
	// offene Epics, Rueckschau und lose Beads leitet er daraus ab. Das Fixture
	// muss deshalb tragen, was diese Ableitung liest — issue_type und parent.
	if f.ohneBestand {
		schreib("bestand.json", "{kein JSON")
	} else {
		eltern := f.elternVon()
		istEpic := map[string]bool{}
		for _, e := range f.epics {
			istEpic[e] = true
		}
		var b []string
		for _, id := range f.alleIDs() {
			typ := "task"
			if istEpic[id] {
				typ = "epic"
			}
			b = append(b, fmt.Sprintf(
				`{"id":%q,"parent":%q,"title":%q,"issue_type":%q,"priority":2,"status":"open","updated_at":"2026-09-04T10:00:00Z","labels":["wayfinder:map"]}`,
				id, eltern[id], id, typ))
		}
		schreib("bestand.json", "["+strings.Join(b, ",")+"]")
	}

	// `list` OHNE --status all bekommt absichtlich eine kaputte Antwort statt
	// einer leeren: ein zusaetzlicher Listenaufruf soll auffallen, nicht still
	// durchgehen (Spec §9.1, zweite Ursache — eine Behauptung, die schwaecher
	// ist als gemeint).
	script := fmt.Sprintf(`printf '%%s\n' "$*" >> %s
case "$*" in
  *"--status all"*)   cat %s/bestand.json; exit 0 ;;
esac
case "$1" in
  list)  echo 'UNERWARTETER LISTENAUFRUF'; exit 1 ;;
  ready) cat %s/ready.json ;;
  show)
    shift
    out=""
    for id in "$@"; do
      case "$id" in --*) continue ;; esac
      f=%s/kinder-$id.json
      [ -f "$f" ] || continue
      out="$out\"$id\":$(cat "$f"),"
    done
    printf '{%%s"schema_version":1}\n' "$out"
    ;;
  graph) echo 'digraph {}' ;;
esac
`, log, dir, dir, dir)
	fakeBinOnPath(t, "bd", script)
	return log
}

// showRufe filtert das Argumentprotokoll auf die `bd show`-Aufrufe — die
// Stelle, an der sich "je Ebene einer" von "je Bead einer" unterscheidet.
func showRufe(t *testing.T, log string) []string {
	t.Helper()
	var rufe []string
	for _, z := range argLogWennDa(t, log) {
		if strings.HasPrefix(z, "show") {
			rufe = append(rufe, z)
		}
	}
	return rufe
}

// suchBead findet einen Bead irgendwo im Teilbaum und gibt zusaetzlich seine
// Tiefe zurueck (direktes Kind = 1).
func suchBead(kinder []BeadsSubBead, id string) (BeadsSubBead, int, bool) {
	for _, k := range kinder {
		if k.ID == id {
			return k, 1, true
		}
		if b, tiefe, ok := suchBead(k.Children, id); ok {
			return b, tiefe + 1, true
		}
	}
	return BeadsSubBead{}, 0, false
}

func karte(t *testing.T, repo BeadsRepo, id string) BeadsMap {
	t.Helper()
	for _, m := range repo.Maps {
		if m.ID == id {
			return m
		}
	}
	t.Fatalf("Karte %s fehlt: %+v", id, repo.Maps)
	return BeadsMap{}
}

// ---------------------------------------------------------------- gkp-1lp

// Der eigentliche Auftrag: eine Aufgabe, darunter beliebig tief geschachtelte
// Unteraufgaben. Enkel sind keine eigene Kategorie — sie haengen an ihrem
// Elternteil und nicht flach an der Karte.
func TestCollectBeadsLiefertEnkelUndUrenkel(t *testing.T) {
	repo := beadsRepo(t)
	bdBaumFake{
		epics: []string{"x-aaa"},
		baum: map[string][]string{
			"x-aaa": {"x-k1"},
			"x-k1":  {"x-e1"},
			"x-e1":  {"x-u1"},
		},
	}.installieren(t)

	got := CollectBeads(repo)

	m := karte(t, got, "x-aaa")
	if len(m.Children) != 1 || m.Children[0].ID != "x-k1" {
		t.Fatalf("Kinder der Karte = %+v, erwartet genau x-k1", m.Children)
	}
	for id, wollen := range map[string]int{"x-k1": 1, "x-e1": 2, "x-u1": 3} {
		b, tiefe, ok := suchBead(m.Children, id)
		if !ok {
			t.Fatalf("%s ist im Baum nicht erreichbar: %+v", id, m.Children)
		}
		if tiefe != wollen {
			t.Errorf("%s steht auf Tiefe %d, erwartet %d", id, tiefe, wollen)
		}
		if b.Title == "" {
			t.Errorf("%s ohne Titel — die Felder muessen auch tief unten ankommen: %+v", id, b)
		}
	}
	// Der Enkel darf NICHT zusaetzlich flach an der Karte haengen, sonst
	// zaehlen ihn die Fortschrittsbalken doppelt.
	if len(m.Children) != 1 {
		t.Errorf("%d direkte Kinder, erwartet 1 — Enkel gehoeren nach unten, nicht daneben", len(m.Children))
	}
}

// Die Zusage von gkp-1lp in einem Satz: ein `bd show` je EBENE, nicht je Bead.
//
// Gemessen an diesem Repo am 2026-09-13: `bd show <ids> --children --json`
// kostet 410 ms bei einer Id, 2391 ms bei 20, 6620 ms bei 62 — die Kosten
// haengen an der Zahl der Ids, nicht am Prozessstart. Ein Aufruf je Bead waere
// bei 62 Kindern also nicht 62-mal 410 ms, sondern schlicht unbrauchbar; und
// die Ebene selbst darf nicht in Einzelaufrufe zerfallen.
func TestShowWirdJeEbeneGebuendeltNichtJeBead(t *testing.T) {
	repo := beadsRepo(t)
	log := bdBaumFake{
		epics: []string{"x-aaa", "x-bbb"},
		baum: map[string][]string{
			"x-aaa": {"x-k1", "x-k2"}, // x-k2 ist ein Blatt
			"x-bbb": {"x-k3"},
			"x-k1":  {"x-e1", "x-e2"},
			"x-k3":  {"x-e3"},
			"x-e1":  {"x-u1"},
		},
	}.installieren(t)

	CollectBeads(repo)

	rufe := showRufe(t, log)
	// Vier Ebenen mit Beads, aber nur drei, die Kinder haben koennen: die
	// Urenkel-Ebene (x-u1) ist laut Bestand kinderlos und kostet keinen Aufruf.
	if len(rufe) != 3 {
		t.Fatalf("%d show-Aufrufe, erwartet 3 (eine je Ebene): %v", len(rufe), rufe)
	}
	for i, wollen := range [][]string{
		{"x-aaa", "x-bbb"},
		{"x-k1", "x-k3"},
		{"x-e1"},
	} {
		for _, id := range wollen {
			if !strings.Contains(rufe[i], id) {
				t.Errorf("Aufruf %d (%q) fragt %s nicht mit ab", i+1, rufe[i], id)
			}
		}
	}
	// x-k2 hat laut Bestand keine Kinder — es nachzufragen kostet in einem
	// echten Repo 100 ms je Id fuer eine Antwort, die schon feststeht.
	if strings.Contains(rufe[1], "x-k2") {
		t.Errorf("Aufruf %q fragt das Blatt x-k2 ab", rufe[1])
	}
}

// Der Bestand ist fuer den ABSTIEG eine Abkuerzung, keine Bedingung: gibt man
// beadsChildrenTree den Nullwert, muss die Hierarchie vollstaendig bleiben und
// nur eine Ebene teurer werden.
//
// Geprueft wird das seit gkp-mnd an beadsChildrenTree direkt und nicht mehr an
// CollectBeads. Der Grund ist eine echte Verhaltensaenderung und keine
// Testkosmetik: der Bestand IST dort jetzt die Bedingung, weil aus ihm auch die
// Epics und die losen Beads kommen. Faellt er aus, gibt es keine Karten mehr,
// die man absteigen koennte — das deckt TestOhneBestandKeineKarten ab.
func TestOhneBestandWirdBlindAbgestiegen(t *testing.T) {
	log := bdBaumFake{
		epics: []string{"x-aaa"},
		baum: map[string][]string{
			"x-aaa": {"x-k1"},
			"x-k1":  {"x-e1"},
		},
		ohneBestand: true,
	}.installieren(t)

	kinder := beadsChildrenTree(t.TempDir(), []string{"x-aaa"}, bdBestand{})

	baum := beadsBaum(kinder, "x-aaa", nil, map[string]bool{})
	if _, tiefe, ok := suchBead(baum, "x-e1"); !ok || tiefe != 2 {
		t.Fatalf("x-e1 nicht auf Tiefe 2 erreichbar (ok=%v tiefe=%d): %+v", ok, tiefe, baum)
	}
	// Ohne Bestand kann der Abstieg das Blatt nicht erkennen und fragt eine
	// Ebene zu tief: x-aaa, x-k1, x-e1.
	if rufe := showRufe(t, log); len(rufe) != 3 {
		t.Errorf("%d show-Aufrufe ohne Bestand, erwartet 3: %v", len(rufe), rufe)
	}
}

// TestOhneBestandKeineKarten haelt fest, was gkp-mnd am Ausfallverhalten
// geaendert hat — und zwar als Zusage, nicht als Nebenwirkung.
//
// Vorher trug `bd list -t epic` die Karten und der Bestand war die Abkuerzung
// darunter; ein kaputter Bestand kostete eine Ebene mehr Aufrufe. Jetzt traegt
// der Bestand beides, also ist sein Ausfall derselbe Ausfall wie frueher der
// von `bd list -t epic`: Available bleibt true, Error benennt es, die Seite
// bleibt stehen. Fail-open, nicht fail-closed.
func TestOhneBestandKeineKarten(t *testing.T) {
	repo := beadsRepo(t)
	bdBaumFake{
		epics:       []string{"x-aaa"},
		baum:        map[string][]string{"x-aaa": {"x-k1"}},
		ohneBestand: true,
	}.installieren(t)

	got := CollectBeads(repo)

	if !got.Available {
		t.Errorf("Available=false, erwartet true — .beads/ existiert, bd antwortet nur nicht")
	}
	if got.Error == "" {
		t.Errorf("Error ist leer — der Ausfall muss in der Karte stehen")
	}
	if len(got.Maps) != 0 {
		t.Errorf("Maps=%d, erwartet 0: %+v", len(got.Maps), got.Maps)
	}
}

// Mit Bestand kostet dieselbe Hierarchie einen Aufruf weniger — die Gegenprobe
// zum Test darueber, damit die Ersparnis gemessen und nicht behauptet ist.
func TestBestandSpartDieBlindeEbene(t *testing.T) {
	repo := beadsRepo(t)
	log := bdBaumFake{
		epics: []string{"x-aaa"},
		baum: map[string][]string{
			"x-aaa": {"x-k1"},
			"x-k1":  {"x-e1"},
		},
	}.installieren(t)

	CollectBeads(repo)

	if rufe := showRufe(t, log); len(rufe) != 2 {
		t.Errorf("%d show-Aufrufe mit Bestand, erwartet 2: %v", len(rufe), rufe)
	}
}

// Ein Repo ohne jede Schachtelung — der heutige Normalfall (0 Enkel von 99
// Beads) — darf durch die Rekursion NICHT teurer werden: nach wie vor ein
// einziger show-Aufruf.
func TestFlachesRepoKostetWeiterEinenAufruf(t *testing.T) {
	repo := beadsRepo(t)
	log := bdBaumFake{
		epics: []string{"x-aaa", "x-bbb"},
		baum: map[string][]string{
			"x-aaa": {"x-k1", "x-k2"},
			"x-bbb": {"x-k3"},
		},
	}.installieren(t)

	CollectBeads(repo)

	if rufe := showRufe(t, log); len(rufe) != 1 {
		t.Errorf("%d show-Aufrufe fuer ein flaches Repo, erwartet 1: %v", len(rufe), rufe)
	}
}

// bd sollte keinen Zyklus erzeugen — aber der Collector darf sich nicht darauf
// verlassen. Ein Zyklus muss enden, und zwar ohne einen Bead zu verlieren:
// weggelassen wird nur die Rueckkante.
func TestZyklusLaeuftNichtEndlos(t *testing.T) {
	repo := beadsRepo(t)
	log := bdBaumFake{
		epics: []string{"x-aaa"},
		baum: map[string][]string{
			"x-aaa": {"x-b"},
			"x-b":   {"x-aaa"}, // zurueck zur Wurzel
		},
	}.installieren(t)

	got := CollectBeads(repo)

	m := karte(t, got, "x-aaa")
	if len(m.Children) != 1 || m.Children[0].ID != "x-b" {
		t.Fatalf("Kinder = %+v, erwartet genau x-b", m.Children)
	}
	if len(m.Children[0].Children) != 0 {
		t.Errorf("x-b hat Kinder %+v — die Rueckkante zur Wurzel darf nicht verfolgt werden", m.Children[0].Children)
	}
	// Und der Abstieg darf keine Id zweimal abfragen.
	rufe := showRufe(t, log)
	if len(rufe) != 2 {
		t.Fatalf("%d show-Aufrufe, erwartet 2: %v", len(rufe), rufe)
	}
	if strings.Contains(rufe[1], "x-aaa") {
		t.Errorf("Aufruf %q fragt die Wurzel ein zweites Mal ab", rufe[1])
	}
}

// Der Zyklusschutz noch einmal ohne bd: beadsBaum allein, damit der Nachweis
// nicht an einem Unterprozess haengt. Ohne Schutz laeuft diese Funktion in eine
// Endlosschleife und der Test in seinen Timeout.
func TestBeadsBaumBrichtZyklusAb(t *testing.T) {
	kinder := map[string][]BeadsSubBead{
		"a": {{ID: "b"}},
		"b": {{ID: "c"}},
		"c": {{ID: "a"}}, // zurueck zum Anfang
	}

	baum := beadsBaum(kinder, "a", nil, map[string]bool{})

	if len(baum) != 1 || baum[0].ID != "b" {
		t.Fatalf("Kinder von a = %+v, erwartet b", baum)
	}
	if len(baum[0].Children) != 1 || baum[0].Children[0].ID != "c" {
		t.Fatalf("Kinder von b = %+v, erwartet c", baum[0].Children)
	}
	if len(baum[0].Children[0].Children) != 0 {
		t.Errorf("c hat Kinder %+v — die Rueckkante auf a gehoert weggelassen", baum[0].Children[0].Children)
	}
}

// Die Tiefe ist nicht begrenzt. bds eigene Sperre CheckHierarchyDepth zaehlt
// Punkte im Id-String statt Kanten und greift ueber den MCP-Pfad nie — der
// Collector darf also keine eigene Grenze erfinden, die es in den Daten nicht
// gibt. Zwoelf Ebenen sind mehr als jede Sperre, die bd kennt.
func TestTiefeIstNichtBegrenzt(t *testing.T) {
	const tiefe = 12
	baum := map[string][]string{}
	kette := []string{"x-aaa"}
	for i := 1; i < tiefe; i++ {
		id := fmt.Sprintf("x-n%02d", i)
		baum[kette[len(kette)-1]] = []string{id}
		kette = append(kette, id)
	}
	repo := beadsRepo(t)
	log := bdBaumFake{epics: []string{"x-aaa"}, baum: baum}.installieren(t)

	got := CollectBeads(repo)

	m := karte(t, got, "x-aaa")
	tiefster := kette[len(kette)-1]
	if _, ist, ok := suchBead(m.Children, tiefster); !ok || ist != tiefe-1 {
		t.Fatalf("%s nicht auf Tiefe %d erreichbar (ok=%v, ist=%d)", tiefster, tiefe-1, ok, ist)
	}
	// Ein Aufruf je Ebene, die Kinder haben kann: die unterste hat keine.
	if rufe := showRufe(t, log); len(rufe) != tiefe-1 {
		t.Errorf("%d show-Aufrufe bei %d Ebenen, erwartet %d", len(showRufe(t, log)), tiefe, tiefe-1)
	}
}

// Die Startklar-Markierung kam bisher aus einer Schleife ueber die direkten
// Kinder (beads.go:884). Die haette einen startklaren Enkel stumm uebergangen —
// und "startklar" ist genau die Auskunft, nach der man die Ansicht fragt.
func TestReadyMarkierungErreichtEnkel(t *testing.T) {
	repo := beadsRepo(t)
	bdBaumFake{
		epics: []string{"x-aaa"},
		baum: map[string][]string{
			"x-aaa": {"x-k1"},
			"x-k1":  {"x-e1", "x-e2"},
		},
		ready: []string{"x-e2"},
	}.installieren(t)

	got := CollectBeads(repo)

	m := karte(t, got, "x-aaa")
	e2, _, ok := suchBead(m.Children, "x-e2")
	if !ok {
		t.Fatalf("x-e2 fehlt: %+v", m.Children)
	}
	if !e2.Ready {
		t.Error("x-e2 ist laut bd startklar, die Markierung kommt beim Enkel nicht an")
	}
	if e1, _, _ := suchBead(m.Children, "x-e1"); e1.Ready {
		t.Error("x-e1 ist nicht startklar, wird aber so markiert")
	}
}

// Auch die Sammelkarte traegt Aufgaben, und eine Aufgabe ohne Epic darf
// Unteraufgaben haben. Sonst haette ein Repo ohne Epics (no epics) weiterhin nur
// eine Ebene — dieselbe Luecke, nur an einer anderen Stelle.
func TestLoseBeadsBekommenAuchUnteraufgaben(t *testing.T) {
	repo := beadsRepo(t)
	bdBaumFake{
		lose: []string{"ws-2t5"},
		baum: map[string][]string{
			"ws-2t5": {"ws-k1"},
			"ws-k1":  {"ws-e1"},
		},
	}.installieren(t)

	got := CollectBeads(repo)

	// Tiefe 3 statt 2: der lose Bead ist selbst schon ein Kind der Sammelkarte
	// (die Karte ist kein Bead), sein Kind steht also eine Ebene tiefer als bei
	// einem Epic.
	m := karte(t, got, LooseMapID)
	if _, tiefe, ok := suchBead(m.Children, "ws-e1"); !ok || tiefe != 3 {
		t.Fatalf("ws-e1 nicht auf Tiefe 3 unter der Sammelkarte (ok=%v tiefe=%d): %+v", ok, tiefe, m.Children)
	}
}

// ---------------------------------------------------------------- gkp-mnd

// alleRufe zaehlt JEDEN bd-Aufruf, nicht nur die show-Aufrufe. Das ist der
// Unterschied zwischen "der Abstieg ist gebuendelt" und "der ganze Collector
// kostet konstant viele Prozesse" — die zweite Frage stellt gkp-mnd.
func alleRufe(t *testing.T, log string) []string {
	t.Helper()
	return argLogWennDa(t, log)
}

// TestBreiteKostetKeineAufrufeTiefeSchon ist das Kriterium aus gkp-mnd, direkt
// hingeschrieben: die Zahl der bd-Aufrufe darf mit der TIEFE wachsen, nicht
// mit der Zahl der Beads.
//
// Beide Haelften stehen in EINEM Test, weil nur ihr Vergleich etwas aussagt.
// Ein Test, der nur "breit kostet nichts" behauptet, bliebe auch dann gruen,
// wenn der Collector gar nicht mehr absteigt.
//
// Gegen echtes bd nachgemessen am 2026-09-14 an einer Wegwerf-Kopie von
// infra: 156 Beads auf Tiefe 6 kosteten 11 Aufrufe, dieselbe Tiefe mit 216
// Beads ebenfalls 11 — 60 Beads mehr, kein Aufruf mehr. Nach der Umstellung
// sind es 8 statt 11, weil der konstante Anteil von 5 auf 2 gefallen ist.
func TestBreiteKostetKeineAufrufeTiefeSchon(t *testing.T) {
	// Schmal und tief: eine Kette von drei Ebenen unter einem Epic.
	schmalTief := bdBaumFake{
		epics: []string{"x-aaa"},
		baum: map[string][]string{
			"x-aaa": {"x-k1"},
			"x-k1":  {"x-e1"},
			"x-e1":  {"x-u1"},
		},
	}
	// Dieselbe TIEFE, aber sehr viel breiter: jede Ebene bekommt Geschwister
	// dazu, und ein zweites Epic obendrauf.
	breitGleichTief := bdBaumFake{
		epics: []string{"x-aaa", "x-bbb"},
		baum: map[string][]string{
			"x-aaa": {"x-k1", "x-k2", "x-k4", "x-k5", "x-k6"},
			"x-bbb": {"x-k7", "x-k8"},
			"x-k1":  {"x-e1", "x-e4", "x-e5"},
			"x-k7":  {"x-e6"},
			"x-e1":  {"x-u1", "x-u2", "x-u3"},
		},
	}

	zaehle := func(f bdBaumFake) ([]string, int) {
		repo := beadsRepo(t)
		log := f.installieren(t)
		CollectBeads(repo)
		rufe := alleRufe(t, log)
		return rufe, len(f.alleIDs())
	}

	rufeSchmal, beadsSchmal := zaehle(schmalTief)
	rufeBreit, beadsBreit := zaehle(breitGleichTief)

	if beadsBreit <= beadsSchmal {
		t.Fatalf("Das Fixture ist nicht breiter: %d gegen %d Beads", beadsBreit, beadsSchmal)
	}
	if len(rufeSchmal) != len(rufeBreit) {
		t.Errorf("%d Beads kosten %d Aufrufe, %d Beads kosten %d — die Zahl haengt an der Breite:\n  schmal: %v\n  breit:  %v",
			beadsSchmal, len(rufeSchmal), beadsBreit, len(rufeBreit), rufeSchmal, rufeBreit)
	}

	// Der konstante Anteil, ausgeschrieben: ein Listenaufruf (Bestand) plus
	// ein `bd ready`. Alles darueber ist Tiefe. Drei Ebenen mit Kindern
	// (x-aaa, x-k1, x-e1) heisst drei show-Aufrufe, also 5 insgesamt.
	if len(rufeSchmal) != 5 {
		t.Errorf("%d Aufrufe bei Tiefe 3, erwartet 5 (1 Liste + 1 ready + 3 Ebenen): %v",
			len(rufeSchmal), rufeSchmal)
	}

	// Und die andere Haelfte: eine Ebene mehr MUSS einen Aufruf mehr kosten,
	// sonst steigt der Collector nicht mehr ab.
	tiefer := schmalTief
	tiefer.baum = map[string][]string{
		"x-aaa": {"x-k1"}, "x-k1": {"x-e1"}, "x-e1": {"x-u1"}, "x-u1": {"x-uu1"},
	}
	rufeTiefer, _ := zaehle(tiefer)
	if len(rufeTiefer) != len(rufeSchmal)+1 {
		t.Errorf("Tiefe 4 kostet %d Aufrufe, Tiefe 3 kostet %d — erwartet genau einer mehr:\n  %v",
			len(rufeTiefer), len(rufeSchmal), rufeTiefer)
	}
}
