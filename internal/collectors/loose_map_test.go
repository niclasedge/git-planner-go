package collectors

// Die Karte fuer Beads ohne Epic (gkp-ar7.14). Getestet wird die FORM, nicht
// bd: dass die losen Beads als Kinder ankommen, dass die Startklar-Markierung
// durchgereicht wird und dass die Karte sich als "kein Epic" ausweist. Der
// Auswahl selbst (elternlos, kein Epic, offen) ist seit gkp-mnd eine Ableitung
// aus dem Bestand und kein eigener bd-Aufruf mehr — geprueft wird sie deshalb
// als Ableitung, nicht als Argumentliste.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeBdOhneEpics ist ein bd, das KEINE Epics hat — der Zustand von
// a flow-only repo. Der Bestand besteht dort ausschliesslich aus losen
// Beads, und genau den liefert die Attrappe.
//
// bestandJSON ist bds Listenform und NICHT eine zweite, eigens gebaute Welt:
// dieselben Datensaetze, aus denen der Collector seine Auswahl ableitet.
func fakeBdOhneEpics(t *testing.T, bestandJSON string) string {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "args.log")
	fixture := filepath.Join(dir, "bestand.json")
	if err := os.WriteFile(fixture, []byte(bestandJSON), 0o644); err != nil {
		t.Fatalf("Fixture schreiben: %v", err)
	}
	script := "" +
		"printf '%s\\n' \"$*\" >> " + log + "\n" +
		"case \"$*\" in\n" +
		"  *\"--status all\"*) cat " + fixture + "; exit 0 ;;\n" +
		"esac\n" +
		"case \"$1\" in\n" +
		"  list)  echo 'UNERWARTETER LISTENAUFRUF'; exit 1 ;;\n" +
		"  ready) echo '[]' ;;\n" +
		"  show)  echo '{\"schema_version\":1}' ;;\n" +
		"  graph) echo 'digraph {}' ;;\n" +
		"esac\n"
	fakeBinOnPath(t, "bd", script)
	return log
}

// Der eigentliche Defekt (gkp-ar7.14): ein Repo ohne Epics zeigte maps: 0 bei
// available: true. Sechs offene Beads waren da, die Seite war leer.
func TestCollectBeadsZeigtBeadsOhneEpicAlsEigeneKarte(t *testing.T) {
	repo := beadsRepo(t)
	log := fakeBdOhneEpics(t, `[
	  {"id":"ws-2t5","title":"Rueckfrage-Prozess","status":"open","issue_type":"task","priority":2,"updated_at":"2026-09-03T10:00:00Z"},
	  {"id":"ws-jfn","title":"Formular gliedern","status":"open","issue_type":"task","priority":3,"updated_at":"2026-09-02T09:00:00Z"}
	]`)

	got := CollectBeads(repo)

	if !got.Available {
		t.Fatalf("Available=false, Error=%q", got.Error)
	}
	if len(got.Maps) != 1 {
		t.Fatalf("Maps=%d, erwartet 1 (die Sammelkarte): %+v", len(got.Maps), got.Maps)
	}
	if got.Maps[0].ID != LooseMapID || !got.Maps[0].Loose {
		t.Errorf("Karte ist nicht die Sammelkarte: %+v", got.Maps[0])
	}
	if len(got.Maps[0].Children) != 2 {
		t.Errorf("%d Kinder, erwartet 2", len(got.Maps[0].Children))
	}

	// Der Guard sitzt weiter auf der Beschaffung, nur ist sie jetzt EINE
	// (gkp-mnd): --limit 0, weil bd sonst still bei 50 kappt, und --status all,
	// weil ohne das jeder geschlossene Bead fehlte. Die Auswahl selbst
	// (elternlos, kein Epic) prueft TestLoseBeadsSindElternlosKeinEpicUndOffen.
	if zeilen := argLog(t, log); len(zeilen) < 1 {
		t.Fatalf("kein bd-Aufruf protokolliert")
	}
	var listenRuf string
	for _, z := range argLog(t, log) {
		if strings.HasPrefix(z, "list") {
			if listenRuf != "" {
				t.Errorf("mehr als ein Listenaufruf: %v", argLog(t, log))
			}
			listenRuf = z
		}
	}
	if listenRuf == "" {
		t.Fatalf("kein Listenaufruf: %v", argLog(t, log))
	}
	for _, muss := range []string{"--status all", "--limit 0", "--json"} {
		if !strings.Contains(listenRuf, muss) {
			t.Errorf("Aufruf %q enthaelt %q nicht", listenRuf, muss)
		}
	}
}

// Ohne lose Beads darf die Karte NICHT erscheinen — sonst haette jedes Repo
// eine leere Sammelkarte, und die Ansicht behauptete Arbeit, die es nicht gibt.
func TestCollectBeadsOhneLoseBeadsKeineSammelkarte(t *testing.T) {
	repo := beadsRepo(t)
	fakeBdOhneEpics(t, `[]`)

	got := CollectBeads(repo)

	if len(got.Maps) != 0 {
		t.Errorf("Maps=%d, erwartet 0: %+v", len(got.Maps), got.Maps)
	}
}

func TestLooseMapReichtKinderUndReadyDurch(t *testing.T) {
	loose := []bdChildJSON{
		{ID: "ws-2t5", Title: "Rueckfrage-Prozess", Status: "open", IssueType: "task", Priority: 2, UpdatedAt: "2026-09-03T10:00:00Z"},
		{ID: "ws-jfn", Title: "Formular gliedern", Status: "open", IssueType: "task", Priority: 3},
	}
	ready := map[string]bool{"ws-jfn": true}

	// kinder=nil: dieser Test prueft die Abbildung der losen Beads selbst. Dass
	// auch sie Unteraufgaben tragen duerfen, steht in
	// TestLoseBeadsBekommenAuchUnteraufgaben (hierarchie_test.go).
	m := looseMap(loose, ready, nil)

	if m.ID != LooseMapID {
		t.Errorf("ID = %q, erwartet %q", m.ID, LooseMapID)
	}
	if !m.Loose {
		t.Error("Loose = false, erwartet true — sonst zeichnet die Ansicht einen Plan-Graphen, den bd graph nicht liefern kann")
	}
	if m.IsMap {
		t.Error("IsMap = true: die Sammelkarte ist keine wayfinder-Karte")
	}
	// Leerer Status faerbt die Karte in der Ansicht wie eine geschlossene.
	if m.Status != "open" {
		t.Errorf("Status = %q, erwartet open", m.Status)
	}
	if len(m.Children) != 2 {
		t.Fatalf("%d Kinder, erwartet 2", len(m.Children))
	}
	if m.Children[0].ID != "ws-2t5" || m.Children[0].Title != "Rueckfrage-Prozess" {
		t.Errorf("erstes Kind falsch abgebildet: %+v", m.Children[0])
	}
	if m.Children[0].UpdatedAt != "2026-09-03T10:00:00Z" {
		t.Errorf("UpdatedAt verloren: %q — danach sortiert die Ansicht", m.Children[0].UpdatedAt)
	}
	if m.Children[0].Ready {
		t.Error("ws-2t5 ist nicht startklar, wird aber so markiert")
	}
	if !m.Children[1].Ready {
		t.Error("ws-jfn ist startklar, die Markierung kommt nicht durch")
	}
}

// Ohne UpdatedAt am Epic-Knoten muss die Karte ihren Zeitstempel von den
// Kindern nehmen — sie hat keine eigene Geschichte im Tracker.
func TestLooseMapHatKeinenEigenenZeitstempel(t *testing.T) {
	m := looseMap([]bdChildJSON{{ID: "ws-2t5", UpdatedAt: "2026-09-03T10:00:00Z"}}, nil, nil)

	if m.UpdatedAt != "" {
		t.Errorf("UpdatedAt = %q, erwartet leer", m.UpdatedAt)
	}
}

// TestBeideErbauerTragenDieTermine (gkp-7gb) haelt eine Fehlerklasse fest, die
// dieses Repo schon dreimal getroffen hat (gkp-1lp/gkp-b9n, Spec §6): ein Feld
// kommt zur Struktur dazu, alle Tests bleiben gruen — und ein ZWEITER Erbauer
// zieht nicht mit.
//
// BeadsSubBead entsteht an genau zwei Stellen: parseBdChildrenBatch fuer die
// Kinder eines Epics und looseMap fuer die Beads ohne Epic. Die Termine wurden
// zuerst nur in der ersten gesetzt. Aufgefallen ist das nicht hier, sondern im
// Routen-Test gegen echtes bd — in einem frischen Repo haengt jede Aufgabe
// unter keinem Epic, also lief alles durch den zweiten Erbauer und kein Termin
// kam an. Dieser Test prueft deshalb BEIDE in einem Zug: wer den naechsten
// Erbauer dazuschreibt, soll ihn hier dazuschreiben muessen.
func TestBeideErbauerTragenDieTermine(t *testing.T) {
	const due = "2026-09-14T23:00:00Z"
	const defer_ = "2026-09-01T00:00:00Z"

	ausKind := parseBdChildren(
		`{"m":[{"id":"x","title":"x","due_at":"`+due+`","defer_until":"`+defer_+`"}]}`, "m")
	ausLose := looseMap([]bdChildJSON{{ID: "y", DueAt: due, DeferUntil: defer_}}, nil, nil).Children

	for name, got := range map[string][]BeadsSubBead{
		"parseBdChildren": ausKind,
		"looseMap":        ausLose,
	} {
		if len(got) != 1 {
			t.Fatalf("%s lieferte %d Aufgaben, want 1", name, len(got))
		}
		if got[0].DueAt != due {
			t.Errorf("%s: DueAt = %q, want %q", name, got[0].DueAt, due)
		}
		if got[0].DeferUntil != defer_ {
			t.Errorf("%s: DeferUntil = %q, want %q", name, got[0].DeferUntil, defer_)
		}
	}
}
