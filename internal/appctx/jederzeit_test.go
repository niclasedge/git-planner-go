package appctx

import (
	"strings"
	"testing"
)

// TestJederzeitWaehltStartklaresOhneTermin ist Spec §5, Zeile Anytime: "alles
// Startklare ohne Datum". Die Gruppierung nach Area kommt mit gkp-8ww; die
// AUSWAHL ist davon unabhaengig und steht hier.
//
// Zwei Grenzfaelle stehen absichtlich mit drin, weil "ohne Datum" sie nicht von
// selbst beantwortet:
//
//   - Ein defer_until in der VERGANGENHEIT terminiert nichts mehr. Es hat die
//     Aufgabe bis dahin versteckt und ist seitdem abgelaufen — die Aufgabe ist
//     ab jetzt jederzeit dran, also gehoert sie hierher.
//   - Eine Frist in der Vergangenheit ist etwas anderes: sie ist ueberfaellig.
//     "Jederzeit" waere die falsche Auskunft ueber eine Aufgabe, die zu spaet
//     ist; sie steht in Heute.
func TestJederzeitWaehltStartklaresOhneTermin(t *testing.T) {
	maps := []ReportsMap{hKarte("m",
		hBead("startklar", func(b *ReportsBead) { b.Ready = true }),
		hBead("wann-abgelaufen", func(b *ReportsBead) {
			b.Ready = true
			b.DeferUntil = "2026-08-01T00:00:00Z"
		}),

		// und was NICHT hineingehoert:
		hBead("startklar-mit-frist", func(b *ReportsBead) {
			b.Ready = true
			b.DueAt = "2026-09-30T09:00:00Z"
		}),
		hBead("startklar-ueberfaellig", func(b *ReportsBead) {
			b.Ready = true
			b.DueAt = "2026-09-10T09:00:00Z"
		}),
		hBead("startklar-vertagt", func(b *ReportsBead) {
			b.Ready = true
			b.DeferUntil = "2026-09-20T00:00:00Z"
		}),
		hBead("blockiert"),
		hBead("geschlossen-startklar", func(b *ReportsBead) {
			b.Status = "closed"
			b.Ready = true
		}),
	)}

	a, ok := AnsichtBauen(JederzeitID, maps, jetztTest())
	if !ok {
		t.Fatal(`AnsichtBauen("jederzeit") kennt die Ansicht nicht`)
	}
	dabei := map[string]bool{}
	for _, e := range a.Eintraege {
		dabei[e.Bead] = true
	}
	for _, id := range []string{"startklar", "wann-abgelaufen"} {
		if !dabei[id] {
			t.Errorf("%s fehlt in Jederzeit", id)
		}
	}
	for _, id := range []string{
		"startklar-mit-frist", "startklar-ueberfaellig", "startklar-vertagt",
		"blockiert", "geschlossen-startklar",
	} {
		if dabei[id] {
			t.Errorf("%s steht in Jederzeit, gehoert aber nicht hinein", id)
		}
	}
}

// TestJederzeitNimmtInArbeitAuf ist dieselbe Entscheidung wie in Heute, mit
// derselben Messung: `bd ready` liefert einen Bead auf in_progress NICHT
// (gemessen 2026-09-14 gegen bd 1.2.2). Ohne die benannte vierte Quelle fiele
// eine Aufgabe in dem Moment aus "Jederzeit", in dem jemand sie anfaengt —
// ausgerechnet die Aufgabe, die am eindeutigsten machbar ist.
func TestJederzeitNimmtInArbeitAuf(t *testing.T) {
	maps := []ReportsMap{hKarte("m",
		hBead("laeuft", func(b *ReportsBead) {
			b.Status = "in_progress"
			b.Ready = false
		}),
	)}
	a, _ := AnsichtBauen(JederzeitID, maps, jetztTest())
	gleichFolge(t, "Jederzeit", zeilenIDs(a), []string{"laeuft"})
	if len(a.Eintraege) == 1 && !enthaelt(a.Eintraege[0].Gruende, GrundInArbeit) {
		t.Errorf("Gruende = %v, erwartet den benannten Grund %q",
			a.Eintraege[0].Gruende, GrundInArbeit)
	}
}

// TestJederzeitSortiertInArbeitVorStartklar. Was schon laeuft, steht oben:
// diese Liste ist die Antwort auf "was kann ich jetzt anfassen", und eine
// bereits angefangene Aufgabe ist die beste davon.
//
// Die Eingabe steht in der Gegenreihenfolge zur Erwartung, und die Ids sind so
// gewaehlt, dass die Id-Sortierung des Rahmens die Erwartung NICHT von selbst
// herstellt: "z-laeuft" ist die letzte Id und muss trotzdem vorn stehen.
func TestJederzeitSortiertInArbeitVorStartklar(t *testing.T) {
	maps := []ReportsMap{hKarte("m",
		hBead("a-startklar", func(b *ReportsBead) { b.Ready = true }),
		hBead("y-laeuft", func(b *ReportsBead) { b.Status = "in_progress" }),
		hBead("z-laeuft", func(b *ReportsBead) { b.Status = "in_progress" }),
	)}
	a, _ := AnsichtBauen(JederzeitID, maps, jetztTest())
	gleichFolge(t, "Jederzeit", zeilenIDs(a),
		[]string{"y-laeuft", "z-laeuft", "a-startklar"})
}

// TestJederzeitSortiertGleichstandNachPrioritaet: ohne Termin entscheidet
// innerhalb eines Rangs die Prioritaet, dann die letzte Aenderung, dann die Id
// (sortiereGruppen). Ohne vollstaendigen Schluessel haengt die Folge an der
// Reihenfolge, in der bd geliefert hat — die ist zwischen zwei Aufrufen nicht
// stabil.
func TestJederzeitSortiertGleichstandNachPrioritaet(t *testing.T) {
	maps := []ReportsMap{hKarte("m",
		hBead("z-p2", func(b *ReportsBead) { b.Ready = true; b.Priority = 2 }),
		hBead("a-p2", func(b *ReportsBead) { b.Ready = true; b.Priority = 2 }),
		hBead("m-p0", func(b *ReportsBead) { b.Ready = true; b.Priority = 0 }),
	)}
	a, _ := AnsichtBauen(JederzeitID, maps, jetztTest())
	gleichFolge(t, "Gleichstand", zeilenIDs(a), []string{"m-p0", "a-p2", "z-p2"})
}

// TestJederzeitErklaertSichLeer: die Ansicht muss die beiden Einschraenkungen
// nennen, die sie selbst nicht zeigen kann — die fehlende Area-Gruppierung
// (gkp-8ww) und dass Terminiertes anderswo steht.
func TestJederzeitErklaertSichLeer(t *testing.T) {
	a, _ := AnsichtBauen(JederzeitID, nil, jetztTest())
	if len(a.Eintraege) != 0 {
		t.Fatalf("Ansicht ohne Karten hat %d Eintraege", len(a.Eintraege))
	}
	h := strings.ToLower(a.Hinweis)
	if !strings.Contains(h, "area") {
		t.Errorf("Hinweis = %q, erwartet die Aussage zur fehlenden Area-Gruppierung", a.Hinweis)
	}
	if !strings.Contains(h, "demnächst") {
		t.Errorf("Hinweis = %q, erwartet den Verweis auf die Liste, in der Terminiertes steht", a.Hinweis)
	}
}
