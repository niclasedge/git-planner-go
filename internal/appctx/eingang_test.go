package appctx

import (
	"strings"
	"testing"

	"github.com/niclasedge/git-planner-go/internal/collectors"
)

// TestEingangIstDieSammelkarte ist Spec §5, Zeile Inbox: die Ansicht nimmt die
// BESTEHENDE synthetische Karte collectors.LooseMapID und baut keine zweite
// Quelle daneben.
//
// Genau das ist hier die Aussage: die Regel fragt nach der KARTE, nicht nach
// einer Eigenschaft der Aufgabe. Eine Aufgabe unter einem echten Epic gehoert
// nie hierher, auch wenn sie sonst identisch aussieht — deshalb steht in beiden
// Karten dasselbe Beadmuster.
func TestEingangIstDieSammelkarte(t *testing.T) {
	maps := []ReportsMap{
		hKarte(collectors.LooseMapID,
			hBead("lose-startklar", func(b *ReportsBead) { b.Ready = true }),
			hBead("lose-still"),
			hBead("lose-geschlossen", func(b *ReportsBead) { b.Status = "closed" }),
		),
		hKarte("gkp-4e1",
			hBead("im-epic-startklar", func(b *ReportsBead) { b.Ready = true }),
			hBead("im-epic-still"),
		),
	}

	a, ok := AnsichtBauen(EingangID, maps, jetztTest())
	if !ok {
		t.Fatal(`AnsichtBauen("eingang") kennt die Ansicht nicht`)
	}
	gleichFolge(t, "Eingang", zeilenIDs(a), []string{"lose-startklar", "lose-still"})
	for _, e := range a.Eintraege {
		if e.Karte != collectors.LooseMapID {
			t.Errorf("%s traegt die Karte %q, erwartet %q", e.Bead, e.Karte, collectors.LooseMapID)
		}
	}
}

// TestEingangNimmtAuchUnteraufgabenDerSammelkarte: `bd list --no-parent` liefert
// nur die Wurzeln, ihre Kinder haengen darunter im Baum (collectors.beadsBaum).
// Eine Ansicht, die nur die oberste Ebene der Karte nimmt, verschwiege sie.
func TestEingangNimmtAuchUnteraufgabenDerSammelkarte(t *testing.T) {
	maps := []ReportsMap{hKarte(collectors.LooseMapID,
		hBead("wurzel", func(b *ReportsBead) {
			b.Children = []ReportsBead{hBead("kind", func(k *ReportsBead) {
				k.Children = []ReportsBead{hBead("enkel")}
			})}
		}),
	)}
	a, _ := AnsichtBauen(EingangID, maps, jetztTest())
	gleichFolge(t, "Eingang", zeilenIDs(a), []string{"wurzel", "kind", "enkel"})

	tiefen := map[string]int{}
	for _, e := range a.Eintraege {
		tiefen[e.Bead] = e.Tiefe
	}
	for id, want := range map[string]int{"wurzel": 0, "kind": 1, "enkel": 2} {
		if tiefen[id] != want {
			t.Errorf("Tiefe von %s = %d, want %d", id, tiefen[id], want)
		}
	}
}

// TestEingangSortiertTerminiertesNachVorn. Der Eingang ist unsortierte Post —
// bis auf eines: was einen Termin traegt, ist bereits entschieden und liegt
// oben, chronologisch. Der Rest faellt auf den Schluessel des Rahmens zurueck
// (Prioritaet, dann letzte Aenderung, dann Id).
//
// Die Eingabe steht in der Gegenreihenfolge zur Erwartung.
func TestEingangSortiertTerminiertesNachVorn(t *testing.T) {
	maps := []ReportsMap{hKarte(collectors.LooseMapID,
		hBead("z-ohne-p2", func(b *ReportsBead) { b.Priority = 2 }),
		hBead("a-ohne-p0", func(b *ReportsBead) { b.Priority = 0 }),
		hBead("y-wann-spaet", func(b *ReportsBead) { b.DeferUntil = "2026-11-01T00:00:00Z" }),
		hBead("x-frist-nah", func(b *ReportsBead) { b.DueAt = "2026-09-20T09:00:00Z" }),
	)}
	a, _ := AnsichtBauen(EingangID, maps, jetztTest())
	gleichFolge(t, "Eingang", zeilenIDs(a),
		[]string{"x-frist-nah", "y-wann-spaet", "a-ohne-p0", "z-ohne-p2"})
}

// TestEingangNenntDenGrund: jede Zeile traegt den Grund, aus dem sie hier steht
// — sonst ist die Bilanz im Kopf leer und die Liste sagt nicht, was sie ist.
func TestEingangNenntDenGrund(t *testing.T) {
	maps := []ReportsMap{hKarte(collectors.LooseMapID,
		hBead("x", func(b *ReportsBead) { b.Ready = true }),
	)}
	a, _ := AnsichtBauen(EingangID, maps, jetztTest())
	if len(a.Eintraege) != 1 {
		t.Fatalf("Eintraege = %d, want 1", len(a.Eintraege))
	}
	if !enthaelt(a.Eintraege[0].Gruende, GrundOhneKarte) {
		t.Errorf("Gruende = %v, erwartet %q", a.Eintraege[0].Gruende, GrundOhneKarte)
	}
	if !enthaelt(a.Eintraege[0].Gruende, GrundStartklar) {
		t.Errorf("Gruende = %v, erwartet zusaetzlich %q", a.Eintraege[0].Gruende, GrundStartklar)
	}
	if zaehlerWert(a, GrundOhneKarte) != 1 {
		t.Errorf("Zaehler[%q] = %d, want 1 (alle: %v)",
			GrundOhneKarte, zaehlerWert(a, GrundOhneKarte), a.Zaehler)
	}
}

// TestEingangErklaertSichLeer: ein leerer Eingang ist die gute Nachricht —
// aber nur, wenn die Ansicht sagt, dass sie die Karte "ohne Epic" zeigt und
// nicht etwa alles.
func TestEingangErklaertSichLeer(t *testing.T) {
	a, _ := AnsichtBauen(EingangID, nil, jetztTest())
	if len(a.Eintraege) != 0 {
		t.Fatalf("Ansicht ohne Karten hat %d Eintraege", len(a.Eintraege))
	}
	if !strings.Contains(a.Hinweis, collectors.LooseMapID) {
		t.Errorf("Hinweis = %q, erwartet die Id der Sammelkarte (%q)",
			a.Hinweis, collectors.LooseMapID)
	}
}
