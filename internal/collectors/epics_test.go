package collectors

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/niclasedge/git-planner-go/internal/shared"
)

// Tests for gkp-uc7.1 and gkp-uc7.2: which epics reach the page, and how they
// are asked for. Both defects were invisible from the outside — an epic that
// is silently absent looks exactly like an epic that does not exist — so the
// guard has to be on the invocation and on the mixed-label case, neither of
// which any previous test touched.

// fakeBd installs a `bd` on PATH that logs every invocation and answers
// `bd list --status all` — den BESTAND — mit epicsJSON und `ready` mit
// readyJSON. `show --children` und `graph --dot` bekommen minimale gueltige
// Antworten, damit CollectBeads ohne echte Datenbank durchlaeuft. Rueckgabe ist
// der Pfad des Argumentprotokolls.
//
// Der Bestand ist seit gkp-mnd die einzige Liste, die der Collector holt: er
// leitet offene Epics, Rueckschau und lose Beads daraus ab. Die Fixtures tragen
// deshalb issue_type — ohne das waere jedes Epic eine Aufgabe und die Karte
// verschwaende.
func fakeBd(t *testing.T, epicsJSON, readyJSON string) string {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "args.log")
	fixture := filepath.Join(dir, "epics.json")
	readyFixture := filepath.Join(dir, "ready.json")
	if err := os.WriteFile(fixture, []byte(epicsJSON), 0o644); err != nil {
		t.Fatalf("Fixture schreiben: %v", err)
	}
	if err := os.WriteFile(readyFixture, []byte(readyJSON), 0o644); err != nil {
		t.Fatalf("Ready-Fixture schreiben: %v", err)
	}
	// Jeder andere Listenaufruf ist ein Fehlschlag und kein Leerergebnis: er
	// soll auffallen. Frueher antwortete die Attrappe auf JEDES `list` mit der
	// Epic-Liste und log damit ueber bd — die Epics kamen auch als lose Beads
	// zurueck und CollectBeads baute eine Sammelkarte, die es in echt nicht
	// gaebe.
	script := "" +
		"printf '%s\\n' \"$*\" >> " + log + "\n" +
		"case \"$*\" in\n" +
		"  *\"--status all\"*) cat " + fixture + "; exit 0 ;;\n" +
		"esac\n" +
		"case \"$1\" in\n" +
		"  list)  echo 'UNERWARTETER LISTENAUFRUF'; exit 1 ;;\n" +
		"  ready) cat " + readyFixture + " ;;\n" +
		"  show)  echo '{\"schema_version\":1}' ;;\n" +
		"  graph) echo 'digraph {}' ;;\n" +
		"esac\n"
	fakeBinOnPath(t, "bd", script)
	return log
}

func argLog(t *testing.T, pfad string) []string {
	t.Helper()
	b, err := os.ReadFile(pfad)
	if err != nil {
		t.Fatalf("Argumentprotokoll lesen: %v", err)
	}
	var zeilen []string
	for _, l := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(l) != "" {
			zeilen = append(zeilen, l)
		}
	}
	return zeilen
}

func beadsRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".beads"), 0o755); err != nil {
		t.Fatalf(".beads anlegen: %v", err)
	}
	return repo
}

// gemischteEpics ist der Fall, den es in orbit gibt und den vorher kein Test
// abgedeckt hat: mindestens eins beschriftet UND mindestens eins nicht.
const gemischteEpics = `[
  {"id":"x-aaa","issue_type":"epic","title":"Karte","status":"open","priority":2,"updated_at":"2026-09-04T10:00:00Z","labels":["wayfinder:map"]},
  {"id":"x-bbb","issue_type":"epic","title":"Epic ohne Karte","status":"in_progress","priority":1,"updated_at":"2026-09-04T11:00:00Z","labels":null}
]`

func TestCollectBeadsZeigtBeschrifteteUndUnbeschrifteteEpics(t *testing.T) {
	repo := beadsRepo(t)
	fakeBd(t, gemischteEpics, `[]`)

	got := CollectBeads(repo)

	if len(got.Maps) != 2 {
		t.Fatalf("Maps=%d, erwartet 2 — ein beschriftetes Epic darf kein unbeschriftetes verdecken: %+v", len(got.Maps), got.Maps)
	}
	nachID := map[string]BeadsMap{}
	for _, m := range got.Maps {
		nachID[m.ID] = m
	}
	if m, da := nachID["x-aaa"]; !da || !m.IsMap {
		t.Errorf("x-aaa: da=%v IsMap=%v, erwartet da=true IsMap=true", da, m.IsMap)
	}
	if m, da := nachID["x-bbb"]; !da || m.IsMap {
		t.Errorf("x-bbb: da=%v IsMap=%v, erwartet da=true IsMap=false", da, m.IsMap)
	}
	// UpdatedAt wird fuer die Sortierung der Kacheln gebraucht (gkp-uc7.6) und
	// war vorher nicht durchgereicht.
	if got := nachID["x-bbb"].UpdatedAt; got != "2026-09-04T11:00:00Z" {
		t.Errorf("UpdatedAt=%q, erwartet durchgereicht", got)
	}
}

// TestBestandFragtOhneLabelfilterUndOhneDeckel haelt die Form des Aufrufs
// fest. Der stille --limit-Default von 50 laesst sich nicht am Ergebnis
// pruefen, ohne 51 Issues anzulegen — die Argumentliste ist die pruefbare
// Stelle. Und ein Labelfilter darf hier gar nicht auftauchen: er war die
// Ursache von gkp-uc7.1.
//
// Seit gkp-mnd gilt das fuer den Bestandsaufruf, weil er der einzige
// Listenaufruf ist. Die Gefahr ist dieselbe geblieben und trifft jetzt sogar
// haerter: das 51. Issue fehlte frueher in einer Liste, heute im Bestand — und
// damit in allen dreien.
func TestBestandFragtOhneLabelfilterUndOhneDeckel(t *testing.T) {
	log := fakeBd(t, gemischteEpics, `[]`)

	if _, ok := bdBestandLesen(t.TempDir()); !ok {
		t.Fatal("bdBestandLesen meldet Fehlschlag")
	}

	zeilen := argLog(t, log)
	if len(zeilen) != 1 {
		t.Fatalf("%d bd-Aufrufe, erwartet genau 1: %v", len(zeilen), zeilen)
	}
	ruf := zeilen[0]
	if !strings.Contains(ruf, "--limit 0") {
		t.Errorf("Aufruf %q enthaelt kein --limit 0 — bd kappt sonst still bei 50", ruf)
	}
	if !strings.Contains(ruf, "--status all") {
		t.Errorf("Aufruf %q ohne --status all — dann fehlte jeder geschlossene Bead", ruf)
	}
	if strings.Contains(ruf, mapLabel) {
		t.Errorf("Aufruf %q filtert auf %s — das Label ist eine Eigenschaft, kein Filter", ruf, mapLabel)
	}
}

// TestCollectBeadsHoltGenauEineListe ist die Zusage von gkp-mnd: die Zahl der
// bd-Aufrufe waechst mit der TIEFE, nicht mit der Zahl der Beads — und der
// konstante Anteil davon ist EIN Listenaufruf, nicht vier.
//
// Vorher waren es `list -t epic`, `list -t epic --status closed`,
// `list --no-parent --exclude-type epic` und der Bestand. Gemessen am
// 2026-09-14 mit einem protokollierenden bd-Wrapper kosteten die vier je
// 232-412 ms, unabhaengig von der Antwortgroesse — es ist bds Datenbankstart,
// nicht die Abfrage. Die ersten drei sind Teilmengen des vierten.
//
// Gezaehlt wird am Argumentprotokoll und nicht an der Zeit: eine Zeitschranke
// waere auf einer belasteten Maschine flatterhaft, und der Rueckfall waere
// nicht "langsamer", sondern "wieder vier Aufrufe".
func TestCollectBeadsHoltGenauEineListe(t *testing.T) {
	repo := beadsRepo(t)
	log := fakeBd(t, gemischteEpics, `[]`)

	CollectBeads(repo)

	var listenRufe []string
	for _, z := range argLog(t, log) {
		if strings.HasPrefix(z, "list") {
			listenRufe = append(listenRufe, z)
		}
	}
	if len(listenRufe) != 1 {
		t.Fatalf("%d Listenaufrufe, erwartet genau 1: %v", len(listenRufe), listenRufe)
	}
}

func TestHasMapLabel(t *testing.T) {
	faelle := []struct {
		name   string
		labels []string
		will   bool
	}{
		{"kein Label", nil, false},
		{"leere Liste", []string{}, false},
		{"nur das Label", []string{mapLabel}, true},
		{"neben anderen", []string{"frontend", mapLabel, "bug"}, true},
		{"aehnlich, aber nicht gleich", []string{"wayfinder", "wayfinder:maps"}, false},
	}
	for _, f := range faelle {
		t.Run(f.name, func(t *testing.T) {
			if got := hasMapLabel(bdEpicJSON{Labels: f.labels}); got != f.will {
				t.Errorf("hasMapLabel(%v)=%v, erwartet %v", f.labels, got, f.will)
			}
		})
	}
}

// TestCollectBeadsOhneEpicsIstKeinFehler grenzt den Leerfall vom Fehlerfall
// ab. Vorher waren beide dieselbe Codezeile, und genau deshalb war der Deckel
// bei 50 nicht zu bemerken.
func TestCollectBeadsOhneEpicsIstKeinFehler(t *testing.T) {
	repo := beadsRepo(t)
	fakeBd(t, `[]`, `[]`)

	got := CollectBeads(repo)

	if !got.Available {
		t.Errorf("Available=false, erwartet true")
	}
	if got.Error != "" {
		t.Errorf("Error=%q, erwartet leer — ein Repo ohne Epics ist kein Fehlschlag", got.Error)
	}
	if len(got.Maps) != 0 {
		t.Errorf("Maps=%d, erwartet 0", len(got.Maps))
	}
}

// ---------------------------------------------------------------- gkp-uc7.3

// fakeBdMitKindern erweitert fakeBd um echte Kinder-Antworten je Epic-Id.
//
// `bd show` nimmt seit gkp-1uu ALLE Epic-Ids in einem Aufruf und antwortet mit
// einem Objekt, das jede angefragte Id als Schluessel traegt. Das Fake bildet
// genau das ab: es sammelt die Ids bis zum ersten Flag und setzt die Antwort
// aus kinder-<id>.json zusammen. Fehlt eine Datei, fehlt der Schluessel — so
// verhaelt sich bd bei einer unbekannten Id auch.
//
// Die Ids einzeln zu beantworten waere hier der eigentliche Fehler: ein Fake,
// das nur $2 ansieht, laesst den Batch-Aufruf gruen aussehen und liefert doch
// nur das erste Epic.
func fakeBdMitKindern(t *testing.T, epicsJSON, readyJSON string, kinder map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "args.log")
	schreib := func(name, inhalt string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(inhalt), 0o644); err != nil {
			t.Fatalf("Fixture %s: %v", name, err)
		}
		return p
	}
	ready := schreib("ready.json", readyJSON)
	for id, arr := range kinder {
		schreib("kinder-"+id+".json", arr)
	}
	// `bd list --status all` ist der Bestand (gkp-1lp): jeder Bead des Repos
	// mit seinem Elternteil. Die Attrappe muss ihn beantworten, sonst steigt
	// der Collector blind eine Ebene tiefer und der Aufrufzaehler misst den
	// Ausfall der Attrappe statt das Verhalten. Zusammengesetzt wird er aus
	// denselben Kinder-Fixtures — hier ist alles zwei Ebenen tief, also hat
	// kein Kind selbst Kinder.
	bestand := schreib("bestand.json", bestandAus(t, epicsJSON, kinder))
	// Seit gkp-mnd gibt es genau EINE Listenabfrage. Jede andere bekommt hier
	// absichtlich einen Fehlschlag statt einer leeren Antwort: ein
	// zusaetzlicher Listenaufruf soll auffallen, nicht still durchgehen.
	script := fmt.Sprintf(`printf '%%s\n' "$*" >> %s
case "$*" in
  *"--status all"*) cat %s;     exit 0 ;;
esac
case "$1" in
  list)  echo 'UNERWARTETER LISTENAUFRUF'; exit 1 ;;
  ready) cat %s ;;
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
`, log, bestand, ready, dir)
	fakeBinOnPath(t, "bd", script)
	return log
}

// bestandAus baut die Antwort auf `bd list --status all` aus den uebrigen
// Fixtures: jedes Epic ohne Elternteil, jedes Kind mit seinem. Aus denselben
// Daten zusammengesetzt statt daneben gepflegt — eine Attrappe, die zwei
// Wahrheiten kennt, prueft am Ende nur noch sich selbst.
func bestandAus(t *testing.T, epicsJSON string, kinder map[string]string) string {
	t.Helper()
	var eintraege []string
	epics, ok := shared.ParseJSON[[]bdEpicJSON](epicsJSON)
	if !ok {
		t.Fatalf("Epic-Fixture ist kein JSON: %s", epicsJSON)
	}
	// Seit gkp-mnd ist der Bestand die einzige Liste, die der Collector holt —
	// er muss deshalb die VOLLEN Felder tragen, nicht nur id/parent/updated_at.
	// Die Epics behalten dabei exakt die Werte ihres eigenen Fixtures: der
	// Bestand ist hier kein zweiter Erbauer, sondern dieselbe Angabe in bds
	// Listenform (Spec §9.1, dritte Ursache — eine Fixture, die am echten Weg
	// vorbei aufbaut, prueft eine Welt, die es nicht gibt).
	for _, e := range epics {
		labels, err := json.Marshal(e.Labels)
		if err != nil {
			t.Fatalf("Labels von %s: %v", e.ID, err)
		}
		eintraege = append(eintraege, fmt.Sprintf(
			`{"id":%q,"parent":"","issue_type":"epic","title":%q,"status":%q,"priority":%d,"updated_at":%q,"closed_at":%q,"labels":%s}`,
			e.ID, e.Title, e.Status, e.Priority, e.UpdatedAt, e.ClosedAt, labels))
	}
	for eltern, arr := range kinder {
		ks, ok := shared.ParseJSON[[]bdChildJSON](arr)
		if !ok {
			t.Fatalf("Kinder-Fixture fuer %s ist kein JSON: %s", eltern, arr)
		}
		for _, k := range ks {
			eintraege = append(eintraege, fmt.Sprintf(
				`{"id":%q,"parent":%q,"issue_type":%q,"title":%q,"status":%q,"priority":%d,"updated_at":%q}`,
				k.ID, eltern, k.IssueType, k.Title, k.Status, k.Priority, k.UpdatedAt))
		}
	}
	return "[" + strings.Join(eintraege, ",") + "]"
}

// kindArray ist der Kinder-Array einer Epic-Id, so wie er in bds Antwort unter
// dem Id-Schluessel steht. Den Schluessel setzt das Fake, weil erst der
// Batch-Aufruf entscheidet, welche Ids ueberhaupt darin vorkommen.
func kindArray(kinder ...string) string {
	return `[` + strings.Join(kinder, ",") + `]`
}

func kind(id, status string) string {
	return `{"id":"` + id + `","title":"` + id + `","status":"` + status + `","issue_type":"task","priority":2,"updated_at":"2026-09-04T10:00:00Z"}`
}

// TestReadyMarkierungKommtVonBdUndTrifftNurEigeneKinder deckt beide Haelften
// von gkp-uc7.3 ab: die Markierung ist bds Antwort, und sie landet
// ausschliesslich an Beads, die ueber Children zu dieser Map gehoeren.
//
// Der zweite Teil ist der wichtige. Die repo-weite Ready-Liste kennt keine
// Eltern, und `bd graph --dot <map>` liefert die Zusammenhangskomponente statt
// des Teilbaums — gemessen liefern infra-nn8 und -qja denselben
// Knotensatz. Wer die Zugehoerigkeit aus dem Graphen zieht, schreibt zwei
// Maps dieselben Beads zu. Hier steht deshalb ein startklarer Bead in der
// Ready-Liste, der zu KEINER der beiden Maps gehoert.
func TestReadyMarkierungKommtVonBdUndTrifftNurEigeneKinder(t *testing.T) {
	repo := beadsRepo(t)
	epics := `[
	  {"id":"x-aaa","issue_type":"epic","title":"A","status":"open","priority":2,"updated_at":"2026-09-04T10:00:00Z","labels":["wayfinder:map"]},
	  {"id":"x-bbb","issue_type":"epic","title":"B","status":"open","priority":2,"updated_at":"2026-09-04T10:00:00Z","labels":["wayfinder:map"]}
	]`
	// startklar laut bd: ein Kind von A, ein Kind von B, und x-fremd, das
	// nirgends Kind ist.
	ready := `[{"id":"x-aaa.1"},{"id":"x-bbb.2"},{"id":"x-fremd"}]`
	kinder := map[string]string{
		"x-aaa": kindArray(kind("x-aaa.1", "open"), kind("x-aaa.2", "closed")),
		"x-bbb": kindArray(kind("x-bbb.1", "closed"), kind("x-bbb.2", "open")),
	}
	fakeBdMitKindern(t, epics, ready, kinder)

	got := CollectBeads(repo)

	if len(got.Maps) != 2 {
		t.Fatalf("Maps=%d, erwartet 2", len(got.Maps))
	}
	markiert := map[string][]string{}
	alle := map[string]bool{}
	for _, m := range got.Maps {
		for _, c := range m.Children {
			alle[c.ID] = true
			if c.Ready {
				markiert[m.ID] = append(markiert[m.ID], c.ID)
			}
		}
	}
	if will, ist := []string{"x-aaa.1"}, markiert["x-aaa"]; !gleich(ist, will) {
		t.Errorf("x-aaa startklar=%v, erwartet %v", ist, will)
	}
	if will, ist := []string{"x-bbb.2"}, markiert["x-bbb"]; !gleich(ist, will) {
		t.Errorf("x-bbb startklar=%v, erwartet %v", ist, will)
	}
	if alle["x-fremd"] {
		t.Error("x-fremd ist in einer Map aufgetaucht — Zugehoerigkeit darf nur aus Children kommen")
	}
}

func gleich(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestReadyWirdGenauEinmalGefragt: einmal fuers Repo, nicht einmal je Karte.
// Bei zwoelf Karten ist das der Unterschied zwischen 2,3 s und ~28 s.
func TestReadyWirdGenauEinmalGefragt(t *testing.T) {
	repo := beadsRepo(t)
	epics := `[
	  {"id":"x-aaa","issue_type":"epic","title":"A","status":"open","priority":2,"labels":["wayfinder:map"]},
	  {"id":"x-bbb","issue_type":"epic","title":"B","status":"open","priority":2,"labels":["wayfinder:map"]},
	  {"id":"x-ccc","issue_type":"epic","title":"C","status":"open","priority":2,"labels":["wayfinder:map"]}
	]`
	log := fakeBd(t, epics, `[]`)

	CollectBeads(repo)

	var readyRufe []string
	for _, z := range argLog(t, log) {
		if strings.HasPrefix(z, "ready") {
			readyRufe = append(readyRufe, z)
		}
	}
	if len(readyRufe) != 1 {
		t.Fatalf("%d ready-Aufrufe bei 3 Karten, erwartet 1: %v", len(readyRufe), readyRufe)
	}
	if !strings.Contains(readyRufe[0], "--limit 0") {
		t.Errorf("Aufruf %q ohne --limit 0 — bd kappt still bei 50", readyRufe[0])
	}
	if strings.Contains(readyRufe[0], "--parent") {
		t.Errorf("Aufruf %q ist auf eine Karte eingeschraenkt — dann waere es einer je Karte", readyRufe[0])
	}
}

// TestShowWirdGebuendelt haelt die Zusage von gkp-1uu fest: ein `bd show` je
// Projekt, nicht eines je Epic. Der Aufruf kostet auf einer grossen Dolt-DB
// Sekunden, und was er kostet, ist bds Start, nicht die Abfrage — gemessen an
// infra mit 12 Epics: zwoelf Einzelaufrufe 9775 ms, ein Batch 3201 ms.
//
// Geprueft wird zusaetzlich, dass die Kinder danach WEITER an der richtigen
// Karte haengen. Ein Batch, der alles unter das erste Epic kippt, waere schnell
// und falsch.
func TestShowWirdGebuendelt(t *testing.T) {
	repo := beadsRepo(t)
	epics := `[
	  {"id":"x-aaa","issue_type":"epic","title":"A","status":"open","priority":2,"labels":["wayfinder:map"]},
	  {"id":"x-bbb","issue_type":"epic","title":"B","status":"open","priority":2,"labels":["wayfinder:map"]},
	  {"id":"x-ccc","issue_type":"epic","title":"C","status":"open","priority":2,"labels":["wayfinder:map"]}
	]`
	kinder := map[string]string{
		"x-aaa": kindArray(kind("x-aaa.1", "open")),
		"x-bbb": kindArray(kind("x-bbb.1", "open"), kind("x-bbb.2", "closed")),
		"x-ccc": kindArray(kind("x-ccc.1", "closed")),
	}
	log := fakeBdMitKindern(t, epics, `[]`, kinder)

	got := CollectBeads(repo)

	var showRufe []string
	for _, z := range argLog(t, log) {
		if strings.HasPrefix(z, "show") {
			showRufe = append(showRufe, z)
		}
	}
	if len(showRufe) != 1 {
		t.Fatalf("%d show-Aufrufe bei 3 Epics, erwartet 1: %v", len(showRufe), showRufe)
	}
	for _, id := range []string{"x-aaa", "x-bbb", "x-ccc"} {
		if !strings.Contains(showRufe[0], id) {
			t.Errorf("Aufruf %q fragt %s nicht mit ab", showRufe[0], id)
		}
	}

	wollen := map[string][]string{
		"x-aaa": {"x-aaa.1"},
		"x-bbb": {"x-bbb.1", "x-bbb.2"},
		"x-ccc": {"x-ccc.1"},
	}
	if len(got.Maps) != 3 {
		t.Fatalf("Maps=%d, erwartet 3", len(got.Maps))
	}
	for _, m := range got.Maps {
		var ist []string
		for _, c := range m.Children {
			ist = append(ist, c.ID)
		}
		if !gleich(ist, wollen[m.ID]) {
			t.Errorf("%s: Kinder=%v, erwartet %v", m.ID, ist, wollen[m.ID])
		}
	}
}

// ---------------------------------------------------------------- gkp-3k6

// TestCollectBeadsRuehrtBdGraphNichtAn ist die Zusage von gkp-3k6 in einem
// Satz: der Pfad, der ein Projekt oeffnet, fasst `bd graph` nicht mehr an.
//
// Gemessen an infra (12 Epics): CollectBeads 49767/54076/48668 ms mit
// Graph, 4817/5805/4838 ms ohne. Ein Test auf die Zeit waere flatterig, ein
// Test auf den Aufruf nicht — und der Aufruf ist die Ursache.
func TestCollectBeadsRuehrtBdGraphNichtAn(t *testing.T) {
	repo := beadsRepo(t)
	epics := `[
	  {"id":"x-aaa","issue_type":"epic","title":"A","status":"open","priority":2,"labels":["wayfinder:map"]},
	  {"id":"x-bbb","issue_type":"epic","title":"B","status":"open","priority":2,"labels":["wayfinder:map"]}
	]`
	log := fakeBd(t, epics, `[]`)

	got := CollectBeads(repo)

	for _, z := range argLog(t, log) {
		if strings.HasPrefix(z, "graph") {
			t.Errorf("CollectBeads ruft weiter bd graph: %q", z)
		}
	}
	for _, m := range got.Maps {
		if m.PlanSVG != "" {
			t.Errorf("%s: PlanSVG gefuellt, erwartet leer — der Graph kommt aus /plan-svg", m.ID)
		}
	}
}

// TestPlanSVGCachedRendertUndCacht deckt die andere Haelfte ab: die SVG gibt es
// weiterhin, nur eben auf Abruf — und ein zweiter Abruf kostet keinen
// Unterprozess mehr.
func TestPlanSVGCachedRendertUndCacht(t *testing.T) {
	if _, err := exec.LookPath("dot"); err != nil {
		t.Skip("dot (graphviz) nicht installiert")
	}
	repo := beadsRepo(t)
	epics := `[{"id":"x-aaa","issue_type":"epic","title":"A","status":"open","priority":2,"labels":["wayfinder:map"]}]`
	log := fakeBd(t, epics, `[]`)
	InvalidatePlanSVG(repo)
	t.Cleanup(func() { InvalidatePlanSVG(repo) })

	svg := PlanSVGCached(repo, "x-aaa")
	if !strings.Contains(svg, "<svg") {
		t.Fatalf("kein SVG zurueck: %.120q", svg)
	}
	graphRufe := func() int {
		n := 0
		for _, z := range argLog(t, log) {
			if strings.HasPrefix(z, "graph") {
				n++
			}
		}
		return n
	}
	if n := graphRufe(); n != 1 {
		t.Fatalf("%d graph-Aufrufe beim ersten Abruf, erwartet 1", n)
	}

	if zweite := PlanSVGCached(repo, "x-aaa"); zweite != svg {
		t.Error("zweiter Abruf liefert etwas anderes als der erste")
	}
	if n := graphRufe(); n != 1 {
		t.Errorf("%d graph-Aufrufe nach zwei Abrufen, erwartet 1 — der Cache greift nicht", n)
	}
}

// Die Ansicht holt die Graphen nacheinander, einer je Karte. Die Epic-Liste
// darf dabei nicht je Karte neu geholt werden: sie kostet an infra 1403 ms
// und traegt zu jedem Graphen genau ein Datum bei — zwoelfmal waeren das 17 s
// fuer zwoelf Beschriftungen.
func TestPlanSVGCachedHoltEpicListeNurEinmal(t *testing.T) {
	if _, err := exec.LookPath("dot"); err != nil {
		t.Skip("dot (graphviz) nicht installiert")
	}
	repo := beadsRepo(t)
	epics := `[
	  {"id":"x-aaa","issue_type":"epic","title":"A","status":"open","priority":2,"updated_at":"2026-09-04T10:00:00Z","labels":["wayfinder:map"]},
	  {"id":"x-bbb","issue_type":"epic","title":"B","status":"open","priority":2,"updated_at":"2026-09-04T11:00:00Z","labels":["wayfinder:map"]},
	  {"id":"x-ccc","issue_type":"epic","title":"C","status":"open","priority":2,"updated_at":"2026-09-04T12:00:00Z","labels":["wayfinder:map"]}
	]`
	log := fakeBd(t, epics, `[]`)
	InvalidatePlanSVG(repo)
	t.Cleanup(func() { InvalidatePlanSVG(repo) })

	for _, id := range []string{"x-aaa", "x-bbb", "x-ccc"} {
		if svg := PlanSVGCached(repo, id); !strings.Contains(svg, "<svg") {
			t.Fatalf("%s: kein SVG", id)
		}
	}

	listRufe := 0
	for _, z := range argLog(t, log) {
		if strings.HasPrefix(z, "list") {
			listRufe++
		}
	}
	if listRufe != 1 {
		t.Errorf("%d list-Aufrufe fuer 3 Graphen, erwartet 1", listRufe)
	}
}

// Eine leere Map-Id darf keinen Unterprozess kosten: die Route reicht durch,
// was im Query steht, und ein fehlender Parameter ist kein Grund, bd zu starten.
func TestPlanSVGCachedOhneIdRuftNichts(t *testing.T) {
	repo := beadsRepo(t)
	log := fakeBd(t, `[]`, `[]`)

	if svg := PlanSVGCached(repo, ""); svg != "" {
		t.Errorf("leere Id lieferte %.60q, erwartet \"\"", svg)
	}
	if zeilen := argLogWennDa(t, log); len(zeilen) != 0 {
		t.Errorf("bd wurde gerufen: %v", zeilen)
	}
}

// argLogWennDa ist argLog fuer den Fall, dass bd nie lief — dann existiert die
// Logdatei nicht, und das ist genau das erwartete Ergebnis, kein Fehler.
func argLogWennDa(t *testing.T, pfad string) []string {
	t.Helper()
	if _, err := os.Stat(pfad); err != nil {
		return nil
	}
	return argLog(t, pfad)
}

// TestReadyOhneEpicsWirdNichtGefragt: der Aufruf kostet auf einer grossen
// Dolt-DB Sekunden und hat ueber ein Repo ohne Karten nichts zu sagen.
func TestReadyOhneEpicsWirdNichtGefragt(t *testing.T) {
	repo := beadsRepo(t)
	log := fakeBd(t, `[]`, `[]`)

	CollectBeads(repo)

	for _, z := range argLog(t, log) {
		if strings.HasPrefix(z, "ready") {
			t.Errorf("ready wurde gefragt, obwohl es keine Epics gibt: %q", z)
		}
	}
}

// TestReadyFehlschlagLaesstDieKarteStehen: fail open. Eine fehlende
// Markierung ist ein kleinerer Verlust als eine fehlende Seite.
func TestReadyFehlschlagLaesstDieKarteStehen(t *testing.T) {
	repo := beadsRepo(t)
	epics := `[{"id":"x-aaa","issue_type":"epic","title":"A","status":"open","priority":2,"labels":["wayfinder:map"]}]`
	// kaputtes JSON auf ready, alles andere in Ordnung
	fakeBd(t, epics, `{nicht mal JSON`)

	got := CollectBeads(repo)

	if len(got.Maps) != 1 {
		t.Fatalf("Maps=%d, erwartet 1 — ein ready-Fehlschlag darf die Karte nicht kosten", len(got.Maps))
	}
	if got.Error == "" {
		t.Error("Error ist leer — der Ausfall der Markierung muss benannt werden")
	}
}

// ---------------------------------------------------------------- gkp-mnd

// Die drei Ableitungen aus dem Bestand. Sie haben je einen bd-Aufruf ersetzt,
// und das macht sie zur einzigen Stelle, an der die Auswahl noch definiert ist
// — vorher tat es bd mit seinen Schaltern.
//
// Gegen das echte bd gemessen wird oben in
// TestCollectBeads_LiefertGeschlosseneEpicsMit; hier steht die Form.

func bestandAusJSON(t *testing.T, roh string) bdBestand {
	t.Helper()
	beads, ok := shared.ParseJSON[[]bdBestandJSON](roh)
	if !ok {
		t.Fatalf("Bestandsfixture ist kein JSON: %s", roh)
	}
	return bdBestand{alle: beads}
}

// TestOffeneEpicsSindAllesAusserGeschlossen prueft die Trennlinie an ALLEN
// fuenf Status, die bd kennt, und nicht nur an open/closed.
//
// Der Unterschied ist nicht akademisch: "offen" waere die naheliegende und
// falsche Lesart gewesen — dann verschwaenden blockierte, zurueckgestellte und
// laufende Karten von der Seite. Gemessen am 2026-09-14 an einer Wegwerf-Kopie
// von infra: ein Epic in jedem der vier Nicht-closed-Status steht in
// `bd list -t epic` drin, nur das geschlossene fehlt.
func TestOffeneEpicsSindAllesAusserGeschlossen(t *testing.T) {
	b := bestandAusJSON(t, `[
	  {"id":"x-1","issue_type":"epic","status":"open"},
	  {"id":"x-2","issue_type":"epic","status":"in_progress"},
	  {"id":"x-3","issue_type":"epic","status":"blocked"},
	  {"id":"x-4","issue_type":"epic","status":"deferred"},
	  {"id":"x-5","issue_type":"epic","status":"closed"},
	  {"id":"x-6","issue_type":"task","status":"open"}
	]`)

	var ids []string
	for _, e := range b.offeneEpics() {
		ids = append(ids, e.ID)
	}
	will := []string{"x-1", "x-2", "x-3", "x-4"}
	if !gleich(ids, will) {
		t.Errorf("offeneEpics()=%v, erwartet %v — geschlossen ist die einzige Trennlinie, und eine Aufgabe ist kein Epic", ids, will)
	}
}

// TestGeschlosseneEpicsNeuesteZuerstUndGekappt ist die Ableitung von
// `--sort closed --limit N`, und der Reihenfolge-Teil ist der wichtige: mit
// der falschen Richtung haette die Kappung die AELTESTEN geliefert
// (gkp-ar7.27), also einen Ausschnitt, der wie ein Zufall aussieht.
//
// Die Fixture-Namen laufen ABSICHTLICH gegen die Erwartung (Spec §9.1, erste
// Ursache): aufsteigend nach Id waere x-aaa, x-bbb, x-ccc — erwartet ist
// x-bbb, x-ccc. Auch die Eingabereihenfolge ist die Id-Reihenfolge. Nur der
// Vergleich auf ClosedAt kann das Ergebnis noch herstellen.
func TestGeschlosseneEpicsNeuesteZuerstUndGekappt(t *testing.T) {
	b := bestandAusJSON(t, `[
	  {"id":"x-aaa","issue_type":"epic","status":"closed","closed_at":"2026-01-01T00:00:00Z"},
	  {"id":"x-bbb","issue_type":"epic","status":"closed","closed_at":"2026-03-01T00:00:00Z"},
	  {"id":"x-ccc","issue_type":"epic","status":"closed","closed_at":"2026-02-01T00:00:00Z"},
	  {"id":"x-ddd","issue_type":"task","status":"closed","closed_at":"2026-04-01T00:00:00Z"}
	]`)

	var ids []string
	for _, e := range b.geschlosseneEpics(2) {
		ids = append(ids, e.ID)
	}
	will := []string{"x-bbb", "x-ccc"}
	if !gleich(ids, will) {
		t.Errorf("geschlosseneEpics(2)=%v, erwartet %v — die Kappung muss die NEUESTEN nehmen, und x-ddd ist kein Epic", ids, will)
	}
}

// TestLoseBeadsSindElternlosKeinEpicUndOffen bildet bds drei Schalter ab:
// --no-parent, --exclude-type epic und die Voreinstellung "nicht geschlossen".
// Jede Zeile der Fixture verletzt genau eine Bedingung, damit ein vergessener
// Filter nicht von einem anderen verdeckt wird.
func TestLoseBeadsSindElternlosKeinEpicUndOffen(t *testing.T) {
	b := bestandAusJSON(t, `[
	  {"id":"x-los","issue_type":"task","status":"open","parent":"","due_at":"2026-09-20T00:00:00Z"},
	  {"id":"x-kind","issue_type":"task","status":"open","parent":"x-epic"},
	  {"id":"x-epic","issue_type":"epic","status":"open","parent":""},
	  {"id":"x-zu","issue_type":"task","status":"closed","parent":""}
	]`)

	got := b.loseBeads()
	var ids []string
	for _, l := range got {
		ids = append(ids, l.ID)
	}
	if !gleich(ids, []string{"x-los"}) {
		t.Fatalf("loseBeads()=%v, erwartet [x-los]", ids)
	}
	// Die Termine muessen mitreisen: sie kamen frueher aus dem eigenen
	// --no-parent-Aufruf, und ein Feld, das beim Umbau liegen bleibt, ist die
	// Fehlerklasse aus Spec §6 (siehe TestBeideErbauerTragenDieTermine).
	if got[0].DueAt != "2026-09-20T00:00:00Z" {
		t.Errorf("DueAt=%q — der Termin kommt nicht durch", got[0].DueAt)
	}
}
