package appctx

import (
	"reflect"
	"testing"

	"github.com/niclasedge/git-planner-go/internal/collectors"
)

// TestStandNachMitStempeltGithubProjektAuchOhneBeads faehrt den PRODUKTIONSWEG
// an, der den Stempel tatsaechlich setzt (teilbaum.go, StandNachMit) — nicht
// nur die Weiterleitung in ansicht.go, die TestGithubEintragTraegtDasProjekt-
// DerKarte prueft (Spec §9.1 Muster 3: "Test baut am echten Weg vorbei auf").
//
// cwd ist ein Verzeichnis OHNE .beads/ — genau das Kernszenario aus E1, das
// der fruehe Return in StandNachMit bislang unbehandelt liess: CollectBeads
// liefert Available=false, Name="", und die github-Karten liefen bislang ohne
// Projektstempel durch.
func TestStandNachMitStempeltGithubProjektAuchOhneBeads(t *testing.T) {
	cwd := t.TempDir()
	gh := GithubKarten("o/r", []collectors.GhIssue{ghIssue(7)})

	_, _, ansichten := StandNachMit(cwd, "", gh, jetztTest())

	erwartet := collectors.ReportsRepoName(cwd)
	if erwartet == "" {
		t.Fatalf("Fixture taugt nicht: ReportsRepoName(cwd) ist leer")
	}
	var gefunden bool
	for _, a := range ansichten {
		for _, e := range a.Eintraege {
			if e.Bead != "#7" {
				continue
			}
			gefunden = true
			if e.Projekt != erwartet {
				t.Errorf("Projekt der GitHub-Zeile = %q, want %q (ReportsRepoName des cwd ohne .beads/)", e.Projekt, erwartet)
			}
		}
	}
	if !gefunden {
		t.Fatalf("Fixture taugt nicht: #7 steht in keiner Ansicht")
	}
}

// Der Teilbaum einer Schreibantwort (gkp-8a0, Spec §7.2). Geprueft wird hier
// die reine Seite: dass der herausgeschnittene Ast DERSELBE ist, den ein
// Vollaufbau an dieser Stelle hat — nicht eine zweite, neben ihm gebaute
// Fassung. Die Seite mit bd steht in server/taskroutes_test.go.

// vollerBaum ist die Karte aus dreiEbenen() (reports_fortschritt_test.go),
// gebaut wie im Betrieb: map > a > (b1 > (c1, c2), b2).
func vollerBaum(t *testing.T) []ReportsMap {
	t.Helper()
	return []ReportsMap{buildReportsMap(dreiEbenen(), nil)}
}

// TestTeilbaumIstDerAstAusDemVollaufbau ist das Abnahmekriterium: der
// gelieferte Ast entspricht dem, was ein voller Neuaufbau an dieser Stelle
// ergibt. Verglichen wird gegen den Knoten AUS dem Vollaufbau, nicht gegen eine
// von Hand geschriebene Erwartung — eine handgeschriebene Erwartung waere eine
// zweite Meinung darueber, wie der Baum aussieht, und genau die soll es nicht
// geben.
func TestTeilbaumIstDerAstAusDemVollaufbau(t *testing.T) {
	maps := vollerBaum(t)

	for _, id := range []string{"a", "b1", "c1", "b2"} {
		tb, grund := TeilbaumAus(maps, id)
		if grund != "" {
			t.Fatalf("%s: %s", id, grund)
		}
		soll := findeBead(t, maps[0].Beads, id)
		if !reflect.DeepEqual(tb.Aufgabe, soll) {
			t.Errorf("%s: Teilbaum weicht vom Vollaufbau ab\n hat:  %+v\n soll: %+v", id, tb.Aufgabe, soll)
		}
	}
}

// TestTeilbaumCountsZaehlenDenGanzenAst: Counts ist ein Zeiger, und fehlt er,
// ist die Zeile ein Blatt (gkp-wsk). Ein Teilbaum, der Counts traegt, muss es
// fuer den GANZEN Ast richtig tragen — die Zahl zaehlt die Nachfahren, nie den
// gezeigten Ausschnitt.
func TestTeilbaumCountsZaehlenDenGanzenAst(t *testing.T) {
	tb, grund := TeilbaumAus(vollerBaum(t), "a")
	if grund != "" {
		t.Fatalf("a: %s", grund)
	}
	if tb.Aufgabe.Counts == nil {
		t.Fatalf("a traegt keine Counts, hat aber Unteraufgaben")
	}
	// a hat vier Nachfahren: b1, b2, c1, c2 — drei davon geschlossen.
	if got := *tb.Aufgabe.Counts; got.Total != 4 || got.Closed != 3 || got.Open != 1 {
		t.Errorf("a.counts = %+v, erwartet Total 4 / Closed 3 / Open 1 (alle Nachfahren)", got)
	}
	b1 := findeBead(t, []ReportsBead{tb.Aufgabe}, "b1")
	if b1.Counts == nil || b1.Counts.Total != 2 {
		t.Errorf("b1.counts = %+v, erwartet Total 2 — auch TIEF im Teilbaum muessen die Zahlen stimmen", b1.Counts)
	}
	c1 := findeBead(t, []ReportsBead{tb.Aufgabe}, "c1")
	if c1.Counts != nil {
		t.Errorf("c1.counts = %+v, erwartet nil — ein Blatt bekommt keinen Balken", c1.Counts)
	}
}

// TestTeilbaumWegFuehrtVonDerKarteZumElternteil: ohne den Weg weiss die Ansicht
// nicht, WO sie den Ast einsetzt — und eine neu angelegte Aufgabe hat dort noch
// gar keinen Platz. Der Weg traegt ausserdem die Zahlen der Vorfahren, die sich
// mit der Aufgabe geaendert haben.
func TestTeilbaumWegFuehrtVonDerKarteZumElternteil(t *testing.T) {
	maps := vollerBaum(t)
	tb, grund := TeilbaumAus(maps, "c1")
	if grund != "" {
		t.Fatalf("c1: %s", grund)
	}
	if tb.Karte != "map" {
		t.Errorf("karte = %q, erwartet \"map\"", tb.Karte)
	}
	if tb.Eltern != "b1" {
		t.Errorf("eltern = %q, erwartet \"b1\"", tb.Eltern)
	}
	var ids []string
	for _, s := range tb.Weg {
		ids = append(ids, s.ID)
	}
	if !reflect.DeepEqual(ids, []string{"map", "a", "b1"}) {
		t.Fatalf("weg = %v, erwartet [map a b1] — Karte zuerst, dann jeder Vorfahr", ids)
	}
	if tb.Weg[0].Counts != maps[0].Counts {
		t.Errorf("weg[0].counts = %+v, erwartet die Zahlen der Karte %+v", tb.Weg[0].Counts, maps[0].Counts)
	}
	if tb.Weg[2].Counts.Total != 2 {
		t.Errorf("weg[2].counts = %+v, erwartet Total 2 (c1, c2 unter b1)", tb.Weg[2].Counts)
	}
	// Die Kinderfolge ist Gos Sortierung, wortwoertlich weitergegeben: nur so
	// kann die Ansicht eine NEUE Aufgabe an der richtigen Stelle einhaengen,
	// ohne selbst zu sortieren (CLAUDE.md: Sortierung gehoert nach Go).
	if !reflect.DeepEqual(tb.Weg[2].Kinder, beadIDsOf(maps[0].Beads[0].Children[0].Children)) {
		t.Errorf("weg[2].kinder = %v, erwartet die Reihenfolge aus dem Vollaufbau", tb.Weg[2].Kinder)
	}
}

// TestTeilbaumEinerUnbekanntenIdNenntDenGrund: ein Schreibvorgang, der glueckte,
// dessen Ast aber nicht zu finden ist (eine Karte ist kein Ast, ein Epic hat
// keinen Elternteil), darf nicht still ohne Teilbaum antworten. Sonst sieht die
// Ansicht denselben Rumpf wie bei "nichts hat sich geaendert".
func TestTeilbaumEinerUnbekanntenIdNenntDenGrund(t *testing.T) {
	_, grund := TeilbaumAus(vollerBaum(t), "gibtesnicht")
	if grund == "" {
		t.Fatal("unbekannte Id liefert keinen Grund — der Teilfehlschlag waere statuslos")
	}
	// Die Karte selbst ist kein Teilbaum: sie hat keinen Elternteil, in den man
	// sie einsetzen koennte.
	if _, grund := TeilbaumAus(vollerBaum(t), "map"); grund == "" {
		t.Error("die Karte selbst wird als Teilbaum geliefert — sie hat keinen Platz, an dem sie eingesetzt wuerde")
	}
}

// TestTeilbaumNimmtNichtDenWegEinesGeschwisters: ein Weg, der als gemeinsamer
// Sammler beim Abstieg waechst und beim Verlassen eines Zweigs nicht wieder
// kuerzt, traegt die Vorfahren der zuvor abgesuchten Geschwister mit. Der
// Fehler faellt nur auf, wenn VOR dem gesuchten Ast ein anderer mit Kindern
// steht — an einem Baum mit nur einem Zweig ist er unsichtbar.
func TestTeilbaumNimmtNichtDenWegEinesGeschwisters(t *testing.T) {
	// Die Namen sind die Bedingung, nicht Beiwerk: buildReportsBeads sortiert
	// eine Ebene ohne Zeitstempel nach Id, der Ablenker muss also VOR dem Ast
	// mit dem Ziel stehen. Mit umgekehrten Namen liefe die Suche gleich in den
	// richtigen Zweig und der Test waere gruen, egal wie der Weg gefuehrt wird.
	m := collectors.BeadsMap{ID: "map", Children: []collectors.BeadsSubBead{
		kind("a-ablenker", "open", kind("a-kind", "open")),
		kind("b-elternteil", "open", kind("ziel", "open")),
	}}
	tb, grund := TeilbaumAus([]ReportsMap{buildReportsMap(m, nil)}, "ziel")
	if grund != "" {
		t.Fatalf("ziel: %s", grund)
	}
	var ids []string
	for _, s := range tb.Weg {
		ids = append(ids, s.ID)
	}
	if !reflect.DeepEqual(ids, []string{"map", "b-elternteil"}) {
		t.Errorf("weg = %v, erwartet [map b-elternteil] — der Weg des Ablenkers ist durchgeschlagen", ids)
	}
}

// beadIDsOf ist die Kinderfolge einer Ebene als Id-Liste.
func beadIDsOf(beads []ReportsBead) []string {
	out := make([]string, 0, len(beads))
	for _, b := range beads {
		out = append(out, b.ID)
	}
	return out
}
