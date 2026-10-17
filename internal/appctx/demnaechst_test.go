package appctx

import (
	"strings"
	"testing"
	"time"
)

// TestDemnaechstWaehltNurKuenftigeTermine ist Spec §5, Zeile Upcoming: "nach
// due_at / defer_until gruppiert, chronologisch".
//
// Die Ausschluesse stehen im selben Test wie die Treffer, weil eine Auswahl
// ohne ihre Grenze keine Aussage ist: eine Regel, die einfach alles waehlt,
// waere gegen die obere Haelfte allein gruen.
func TestDemnaechstWaehltNurKuenftigeTermine(t *testing.T) {
	maps := []ReportsMap{hKarte("m",
		hBead("frist-morgen", func(b *ReportsBead) { b.DueAt = "2026-09-15T09:00:00Z" }),
		hBead("wann-morgen", func(b *ReportsBead) { b.DeferUntil = "2026-09-15T00:00:00Z" }),
		hBead("frist-fern", func(b *ReportsBead) { b.DueAt = "2026-12-24T00:00:00Z" }),

		// und was NICHT hineingehoert:
		hBead("frist-heute", func(b *ReportsBead) { b.DueAt = "2026-09-14T23:00:00Z" }),
		hBead("frist-gestern", func(b *ReportsBead) { b.DueAt = "2026-09-13T09:00:00Z" }),
		hBead("wann-vorbei", func(b *ReportsBead) { b.DeferUntil = "2026-08-01T00:00:00Z" }),
		hBead("startklar-ohne-termin", func(b *ReportsBead) { b.Ready = true }),
		hBead("vertagt-weit", func(b *ReportsBead) { b.DeferUntil = "2027-03-01T00:00:00Z" }),
		hBead("geschlossen-mit-frist", func(b *ReportsBead) {
			b.Status = "closed"
			b.DueAt = "2026-09-15T09:00:00Z"
		}),
	)}

	a, ok := AnsichtBauen(DemnaechstID, maps, jetztTest())
	if !ok {
		t.Fatal(`AnsichtBauen("demnaechst") kennt die Ansicht nicht`)
	}
	dabei := map[string]bool{}
	for _, e := range a.Eintraege {
		dabei[e.Bead] = true
	}
	for _, id := range []string{"frist-morgen", "wann-morgen", "frist-fern"} {
		if !dabei[id] {
			t.Errorf("%s fehlt in Demnaechst", id)
		}
	}
	for _, id := range []string{
		"frist-heute", "frist-gestern", "wann-vorbei",
		"startklar-ohne-termin", "vertagt-weit", "geschlossen-mit-frist",
	} {
		if dabei[id] {
			t.Errorf("%s steht in Demnaechst, gehoert aber nicht hinein", id)
		}
	}
}

// TestDemnaechstSortiertChronologisch.
//
// Die Ids laufen ABSICHTLICH gegen die Erwartung: der Schluessel des Rahmens
// endet bei der Id (sortiereGruppen), und mit aufsteigend benannten Fixtures
// stellt schon dieser letzte Vergleich die erwartete Folge her — der Test waere
// dann auch ohne jede Einordnung durch die Regel gruen.
//
// Das ist nicht ausgedacht: dieser Test war in genau dieser Form einmal
// geschrieben und ueberlebte die Gegenprobe "Rang 0, kein Zeitpunkt"
// unbeschadet. Spec §9.1, Muster 2, zum vierten Mal in dieser Karte. Die
// Eingabefolge ist zusaetzlich die umgekehrte der Erwartung.
func TestDemnaechstSortiertChronologisch(t *testing.T) {
	maps := []ReportsMap{hKarte("m",
		hBead("a-in-drei-wochen", func(b *ReportsBead) { b.DueAt = "2026-10-05T09:00:00Z" }),
		hBead("b-naechste-woche", func(b *ReportsBead) { b.DueAt = "2026-09-21T09:00:00Z" }),
		hBead("c-uebermorgen", func(b *ReportsBead) { b.DeferUntil = "2026-09-16T00:00:00Z" }),
		hBead("d-morgen-spaet", func(b *ReportsBead) { b.DueAt = "2026-09-15T18:00:00Z" }),
		hBead("e-morgen-frueh", func(b *ReportsBead) { b.DueAt = "2026-09-15T07:00:00Z" }),
	)}
	a, _ := AnsichtBauen(DemnaechstID, maps, jetztTest())
	gleichFolge(t, "Demnaechst", zeilenIDs(a), []string{
		"e-morgen-frueh", "d-morgen-spaet",
		"c-uebermorgen", "b-naechste-woche", "a-in-drei-wochen",
	})
}

// TestDemnaechstNimmtDenFrueherenDerBeidenTermine: eine Aufgabe kann BEIDE
// Angaben tragen. Welche die Zeile einordnet, ist eine Entscheidung und keine
// Nebensache — der frueheste kuenftige Termin ist der naechste Zeitpunkt, an
// dem die Aufgabe etwas von einem will.
//
// Der Test steht hier, weil ein "nimm einfach die Frist" gegen jeden anderen
// Test dieser Datei gruen bliebe: nur hier stehen beide Felder gleichzeitig,
// und nur hier ist das Wann das frueheste.
func TestDemnaechstNimmtDenFrueherenDerBeidenTermine(t *testing.T) {
	maps := []ReportsMap{hKarte("m",
		hBead("frist-nah", func(b *ReportsBead) { b.DueAt = "2026-09-18T09:00:00Z" }),
		hBead("wann-naeher-frist-fern", func(b *ReportsBead) {
			b.DeferUntil = "2026-09-16T00:00:00Z"
			b.DueAt = "2026-10-20T09:00:00Z"
		}),
	)}
	a, _ := AnsichtBauen(DemnaechstID, maps, jetztTest())
	gleichFolge(t, "Demnaechst", zeilenIDs(a),
		[]string{"wann-naeher-frist-fern", "frist-nah"})
}

// TestDemnaechstZaehltInZeitfenstern: die Bilanz im Kopf ist eine LISTE, und
// ihre Reihenfolge ist Inhalt — das naechste Fenster zuerst.
func TestDemnaechstZaehltInZeitfenstern(t *testing.T) {
	maps := []ReportsMap{hKarte("m",
		// verkehrt herum eingegeben, damit die Folge der Bilanz nicht die der
		// Eingabe sein kann
		hBead("s1", func(b *ReportsBead) { b.DueAt = "2026-10-10T09:00:00Z" }),
		hBead("w1", func(b *ReportsBead) { b.DueAt = "2026-09-18T09:00:00Z" }),
		hBead("w2", func(b *ReportsBead) { b.DeferUntil = "2026-09-21T00:00:00Z" }),
		hBead("m1", func(b *ReportsBead) { b.DueAt = "2026-09-15T09:00:00Z" }),
	)}
	a, _ := AnsichtBauen(DemnaechstID, maps, jetztTest())
	var folge []string
	for _, z := range a.Zaehler {
		folge = append(folge, z.Grund)
	}
	gleichFolge(t, "Zaehlerfolge", folge,
		[]string{GrundMorgen, GrundDieseWoche, GrundSpaeter})
	for grund, want := range map[string]int{GrundMorgen: 1, GrundDieseWoche: 2, GrundSpaeter: 1} {
		if got := zaehlerWert(a, grund); got != want {
			t.Errorf("Zaehler[%q] = %d, want %d (alle: %v)", grund, got, want, a.Zaehler)
		}
	}
}

// TestDemnaechstDeutetTermineAlsWanduhrNichtAlsUTC ist Spec §4.4 fuer diese
// Ansicht: due_at/defer_until sind lokale Wanduhr mit einem Z, das luegt. Wer
// das Z ernst nimmt, schiebt einen Abendtermin um den UTC-Versatz auf den
// Folgetag — und damit aus dem Eimer "morgen" in den Eimer "diese Woche".
//
// Asia/Tokyo (+9, keine Sommerzeit): 23:00 am 15.09. ist dort MORGEN, als
// echtes UTC gelesen waere derselbe Stempel 08:00 am 16.09. — uebermorgen.
func TestDemnaechstDeutetTermineAlsWanduhrNichtAlsUTC(t *testing.T) {
	tokio, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		t.Skipf("keine tzdata fuer Asia/Tokyo: %v", err)
	}
	jetzt := time.Date(2026, 9, 14, 10, 0, 0, 0, tokio)
	maps := []ReportsMap{hKarte("m",
		hBead("abends", func(b *ReportsBead) { b.DueAt = "2026-09-15T23:00:00Z" }),
	)}
	a, _ := AnsichtBauen(DemnaechstID, maps, jetzt)
	if len(a.Eintraege) != 1 {
		t.Fatalf("Eintraege = %d, want 1 (%v)", len(a.Eintraege), zeilenIDs(a))
	}
	if !enthaelt(a.Eintraege[0].Gruende, GrundMorgen) {
		t.Errorf("Gruende = %v, want %q — der Abendtermin ist in Tokio morgen, "+
			"nicht uebermorgen", a.Eintraege[0].Gruende, GrundMorgen)
	}
}

// TestDemnaechstErklaertSichLeer: eine leere Ansicht muss sagen, WAS sie zeigen
// wuerde. Ohne das sieht "nichts terminiert" wie "kaputt" aus.
func TestDemnaechstErklaertSichLeer(t *testing.T) {
	a, _ := AnsichtBauen(DemnaechstID, nil, jetztTest())
	if len(a.Eintraege) != 0 {
		t.Fatalf("Ansicht ohne Karten hat %d Eintraege", len(a.Eintraege))
	}
	h := strings.ToLower(a.Hinweis)
	if !strings.Contains(h, "due_at") || !strings.Contains(h, "defer_until") {
		t.Errorf("Hinweis = %q, erwartet die beiden Felder, aus denen die Ansicht gebaut wird", a.Hinweis)
	}
}
