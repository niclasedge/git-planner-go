package appctx

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// lBead ist ein abgeschlossener Bead. Eigener Helfer, weil in dieser Datei der
// Normalfall der geschlossene ist und ein `func(b *ReportsBead){ b.Status =
// "closed"; b.ClosedAt = ... }` an jeder Zeile den Blick auf das verstellt,
// worum es jeweils geht.
func lBead(id, closedAt string, anpassen ...func(*ReportsBead)) ReportsBead {
	return hBead(id, append([]func(*ReportsBead){func(b *ReportsBead) {
		b.Status = "closed"
		b.ClosedAt = closedAt
	}}, anpassen...)...)
}

// TestLogbuchZeigtNurAbgeschlossenes ist Spec §5, Zeile Logbook. Die Ansicht
// ist die Umkehrung jeder anderen: sie zeigt GENAU das, was die uebrigen fuenf
// ausschliessen.
func TestLogbuchZeigtNurAbgeschlossenes(t *testing.T) {
	maps := []ReportsMap{hKarte("m",
		lBead("fertig-heute", "2026-09-14T06:00:00Z"),
		lBead("fertig-gestern", "2026-09-13T06:00:00Z"),
		lBead("fertig-ohne-stempel", ""),

		// und was NICHT hineingehoert:
		hBead("offen", func(b *ReportsBead) { b.Ready = true }),
		hBead("laeuft", func(b *ReportsBead) { b.Status = "in_progress" }),
	)}

	a, ok := AnsichtBauen(LogbuchID, maps, jetztTest())
	if !ok {
		t.Fatal(`AnsichtBauen("logbuch") kennt die Ansicht nicht`)
	}
	dabei := map[string]bool{}
	for _, e := range a.Eintraege {
		dabei[e.Bead] = true
	}
	for _, id := range []string{"fertig-heute", "fertig-gestern", "fertig-ohne-stempel"} {
		if !dabei[id] {
			t.Errorf("%s fehlt im Logbuch", id)
		}
	}
	for _, id := range []string{"offen", "laeuft"} {
		if dabei[id] {
			t.Errorf("%s steht im Logbuch, ist aber nicht abgeschlossen", id)
		}
	}
}

// TestLogbuchSortiertNeuestenTagZuerst.
//
// Die Ids laufen ABSICHTLICH gegen die Erwartung — der letzte Vergleich in
// sortiereGruppen ist die Id, und mit aufsteigend benannten Fixtures haette
// allein der die erwartete Folge hergestellt (Spec §9.1, Muster 2). Der Bead
// ohne Abschlussdatum traegt zusaetzlich die KLEINSTE Id: er muss trotzdem ganz
// nach hinten, also kann seine Stelle nicht von der Id kommen.
func TestLogbuchSortiertNeuestenTagZuerst(t *testing.T) {
	maps := []ReportsMap{hKarte("m",
		lBead("a-ohne-stempel", ""),
		lBead("b-im-august", "2026-08-02T10:00:00Z"),
		lBead("c-letzte-woche", "2026-09-09T10:00:00Z"),
		lBead("d-gestern", "2026-09-13T10:00:00Z"),
		lBead("e-heute", "2026-09-14T06:00:00Z"),
	)}
	a, _ := AnsichtBauen(LogbuchID, maps, jetztTest())
	gleichFolge(t, "Logbuch", zeilenIDs(a), []string{
		"e-heute", "d-gestern", "c-letzte-woche", "b-im-august", "a-ohne-stempel",
	})
}

// TestLogbuchSortiertInnerhalbEinesTagesNeuestenAbschlussZuerst (gkp-3oh, A).
//
// Die Tagesgrenze macht weiterhin der RANG — ein Eimer je Abschlusstag, wie
// Things' Logbook. Neu ist, was INNERHALB eines Tages entscheidet: der
// Abschlusszeitpunkt, absteigend. Vorher setzte die Regel gar keinen Zeitpunkt,
// weil sortiereGruppen nur aufsteigend konnte; sortiert wurde dann nach
// Prioritaet und LastChange — "wichtigstes zuerst" in einer Liste, die
// chronologisch ist.
//
// Der Test ist so gebaut, dass NUR die neue Regel die erwartete Folge
// herstellen kann; drei Koeder laufen dagegen (Spec §9.1):
//
//   - die IDS laufen gegen die Erwartung. Letzter Vergleich in sortiereGruppen
//     ist die Id AUFSTEIGEND — "z-…" traegt hier den spaetesten Abschluss.
//     Bis 2026-09-14 stand hier das Gegenteil im Kommentar und in den Namen:
//     erwartet wurde a-spaet-p2, m-mittag-p2, z-frueh-p0, also genau die
//     aufsteigende Id-Folge. Die Gegenprobe CP9 (ganzer Sortierschluessel durch
//     den Id-Tiebreak ersetzt) machte 17 Tests rot — diesen nicht.
//   - die PRIORITAET laeuft gegen die Erwartung. Sie steht im Schluessel vor
//     LastChange, und der frueheste Abschluss ist P0. Entschiede sie noch
//     innerhalb des Tages, stuende diese Zeile oben.
//   - die EINGABE steht in der Gegenreihenfolge. Vorher stand sie bereits in
//     der erwarteten Folge — ein blosses Durchreichen der Eingabe waere gruen
//     gewesen.
func TestLogbuchSortiertInnerhalbEinesTagesNeuestenAbschlussZuerst(t *testing.T) {
	maps := []ReportsMap{hKarte("m",
		lBead("a-frueh-p0", "2026-09-14T05:00:00Z", func(b *ReportsBead) {
			b.Priority = 0
			b.LastChange = "2026-09-14T05:00:00Z"
		}),
		lBead("m-mittag-p2", "2026-09-14T06:00:00Z", func(b *ReportsBead) {
			b.Priority = 2
			b.LastChange = "2026-09-14T06:00:00Z"
		}),
		lBead("z-spaet-p2", "2026-09-14T07:00:00Z", func(b *ReportsBead) {
			b.Priority = 2
			b.LastChange = "2026-09-14T07:00:00Z"
		}),
	)}
	a, _ := AnsichtBauen(LogbuchID, maps, jetztTest())
	gleichFolge(t, "Logbuch", zeilenIDs(a),
		[]string{"z-spaet-p2", "m-mittag-p2", "a-frueh-p0"})
}

// TestLogbuchOhneAbschlussdatumBleibtHintenAuchAbsteigend ist die Grenze des
// neuen Absteigend: gedreht wird der Terminvergleich, nicht die Stelle eines
// FEHLENDEN Termins. Ein geschlossener Bead ohne Stempel steht in seinem
// eigenen Eimer ganz hinten (rangLogbuchOhneAbschluss) und darf durch die
// Umkehr nicht nach vorn rutschen — dort saehe er aus wie das Neueste.
func TestLogbuchOhneAbschlussdatumBleibtHintenAuchAbsteigend(t *testing.T) {
	maps := []ReportsMap{hKarte("m",
		lBead("a-ohne-stempel", ""),
		lBead("b-auch-ohne-stempel", ""),
		lBead("z-heute", "2026-09-14T05:00:00Z"),
	)}
	a, _ := AnsichtBauen(LogbuchID, maps, jetztTest())
	gleichFolge(t, "Logbuch", zeilenIDs(a),
		[]string{"z-heute", "a-ohne-stempel", "b-auch-ohne-stempel"})
}

// TestLogbuchDeutetClosedAtAlsEchtesUTC ist Spec §4.4, die ANDERE Haelfte: bd
// schreibt due_at/defer_until als lokale Wanduhr mit einem Z, das luegt —
// created_at/updated_at/closed_at dagegen sind echtes UTC und muessen durch
// bdops.ParseEventStamp.
//
// Zone +02, jetzt 2026-09-14 10:00 Ortszeit. Ein Abschluss um 23:00 UTC am
// 13.09. ist ORTSZEIT der 14.09. um 01:00 — also HEUTE erledigt. Wer den
// Stempel wie eine Wanduhr liest, bekommt den 13.09. 23:00 und legt die Zeile
// in den Eimer von gestern.
//
// Ohne einen Versatz ungleich null faellt das in keinem Test auf: unter TZ=UTC
// liefern beide Deutungen dasselbe Ergebnis.
func TestLogbuchDeutetClosedAtAlsEchtesUTC(t *testing.T) {
	maps := []ReportsMap{hKarte("m",
		lBead("kurz-nach-mitternacht", "2026-09-13T23:00:00Z"),
	)}
	a, _ := AnsichtBauen(LogbuchID, maps, jetztTest())
	if len(a.Eintraege) != 1 {
		t.Fatalf("Eintraege = %d, want 1", len(a.Eintraege))
	}
	e := a.Eintraege[0]
	if e.Rang != 0 {
		t.Errorf("Rang = %d, want 0 — 23:00 UTC am 13.09. ist in Zone +02 der 14.09., "+
			"also heute", e.Rang)
	}
	if !enthaelt(e.Gruende, GrundHeuteErledigt) {
		t.Errorf("Gruende = %v, want %q", e.Gruende, GrundHeuteErledigt)
	}
}

// TestLogbuchZaehltInAltersfenstern — die Bilanz im Kopf, in der Reihenfolge
// des juengsten Fensters zuerst. Die Eingabe steht verkehrt herum.
func TestLogbuchZaehltInAltersfenstern(t *testing.T) {
	maps := []ReportsMap{hKarte("m",
		lBead("alt1", "2026-07-01T10:00:00Z"),
		lBead("woche1", "2026-09-10T10:00:00Z"),
		lBead("woche2", "2026-09-12T10:00:00Z"),
		lBead("heute1", "2026-09-14T05:00:00Z"),
	)}
	a, _ := AnsichtBauen(LogbuchID, maps, jetztTest())
	var folge []string
	for _, z := range a.Zaehler {
		folge = append(folge, z.Grund)
	}
	gleichFolge(t, "Zaehlerfolge", folge,
		[]string{GrundHeuteErledigt, GrundZuletztErledigt, GrundFrueherErledigt})
	for grund, want := range map[string]int{
		GrundHeuteErledigt:   1,
		GrundZuletztErledigt: 2,
		GrundFrueherErledigt: 1,
	} {
		if got := zaehlerWert(a, grund); got != want {
			t.Errorf("Zaehler[%q] = %d, want %d (alle: %v)", grund, got, want, a.Zaehler)
		}
	}
}

// TestLogbuchNenntDieBelegluecke. Das Logbuch ist die einzige Aufgabenliste, in
// der die Belegfrage ueberhaupt gestellt werden kann — vorher ist nichts
// abgeschlossen. ReportsBead.Gap ist "geschlossen UND unbelegt" und steht
// bereits im Baum (reports.HasCurrentDoneEntry); die Ansicht traegt ihn als
// Grund nach aussen, statt ihn zu wiederholen.
//
// Die Zahl im Kopf ist damit dieselbe Beanstandung, die beads_query op=gaps
// beantwortet — nur an der Stelle, an der man hinsieht.
func TestLogbuchNenntDieBelegluecke(t *testing.T) {
	maps := []ReportsMap{hKarte("m",
		lBead("belegt", "2026-09-14T06:00:00Z", func(b *ReportsBead) { b.Proven = true }),
		lBead("unbelegt", "2026-09-14T05:00:00Z", func(b *ReportsBead) { b.Gap = true }),
	)}
	a, _ := AnsichtBauen(LogbuchID, maps, jetztTest())
	gruende := map[string][]string{}
	for _, e := range a.Eintraege {
		gruende[e.Bead] = e.Gruende
	}
	// Die VOLLSTAENDIGE Folge und nicht bloss "ist enthalten": die Stelle ist
	// der Inhalt. Angehaengt stand die Beanstandung als letztes Abzeichen
	// unmittelbar vor der Karte (gemessen 2026-09-14, x≈470 von 1600px), und
	// ein enthaelt() haette das nie gesehen.
	gleichFolge(t, "Gruende(unbelegt)", gruende["unbelegt"],
		[]string{GrundOhneBeleg, GrundHeuteErledigt})
	if enthaelt(gruende["belegt"], GrundOhneBeleg) {
		t.Errorf("Gruende(belegt) = %v, %q gehoert dort nicht hin",
			gruende["belegt"], GrundOhneBeleg)
	}
	if zaehlerWert(a, GrundOhneBeleg) != 1 {
		t.Errorf("Zaehler[%q] = %d, want 1 (alle: %v)",
			GrundOhneBeleg, zaehlerWert(a, GrundOhneBeleg), a.Zaehler)
	}
}

// TestLogbuchBilanzNenntBeanstandungVorDemAltersfenster haelt die
// NEBENWIRKUNG des Voranstellens fest — die Stelle der Beanstandung im Kopf.
//
// Die Bilanz ordnet nach dem ERSTEN Auftreten eines Grundes (ansichtAus), und
// die Gruende einer Zeile werden in ihrer eigenen Reihenfolge durchlaufen. Die
// Fixture baut deshalb genau den gemessenen Fall nach: die erste Zeile, die die
// Luecke traegt, ist zugleich die erste "früher"-Zeile — in infra am
// 2026-09-14 war das Zeile 27 von 95. Angehaengt ergab das
// "letzte 7 Tage · früher · ohne Beleg", vorangestellt
// "letzte 7 Tage · ohne Beleg · früher".
//
// Die Zeilenfolge steht mit im Test, weil die Kopffolge sonst nicht zu deuten
// waere: sie ist eine Aussage ueber die sortierte Liste, und ohne sie koennte
// eine geaenderte Sortierung die Kopffolge unbemerkt mitdrehen.
func TestLogbuchBilanzNenntBeanstandungVorDemAltersfenster(t *testing.T) {
	maps := []ReportsMap{hKarte("m",
		lBead("z-woche-belegt", "2026-09-10T10:00:00Z", func(b *ReportsBead) { b.Proven = true }),
		lBead("m-frueher-unbelegt", "2026-07-01T10:00:00Z", func(b *ReportsBead) { b.Gap = true }),
		lBead("a-noch-frueher-belegt", "2026-06-01T10:00:00Z", func(b *ReportsBead) { b.Proven = true }),
	)}
	a, _ := AnsichtBauen(LogbuchID, maps, jetztTest())
	gleichFolge(t, "Zeilenfolge", zeilenIDs(a),
		[]string{"z-woche-belegt", "m-frueher-unbelegt", "a-noch-frueher-belegt"})

	var folge []string
	for _, z := range a.Zaehler {
		folge = append(folge, z.Grund)
	}
	gleichFolge(t, "Zaehlerfolge", folge,
		[]string{GrundZuletztErledigt, GrundOhneBeleg, GrundFrueherErledigt})
}

// TestLogbuchZeigtOffeneAufgabeAlsUeberschriftFuerIhrFertigesKind: haengt eine
// fertige Unteraufgabe unter einer noch offenen Aufgabe, ist die Aufgabe
// selbst hier nicht gewaehlt (Logbuch zeigt nur Geschlossenes) — sie steht seit
// gkp-a1d trotzdem da, als ZWISCHENUEBERSCHRIFT eine Ebene ueber ihrem fertigen
// Kind, statt spurlos zu verschwinden (Spec §5). Vor diesem Ticket ruckte das
// Kind auf Ebene 0 auf; dieser Test hielt genau das fest und ist umgedreht
// worden, weil sich das Verhalten geaendert hat, nicht weil der alte Befund
// falsch war.
//
// Die VOLLSTAENDIGE Zeilenfolge wird geprueft und nicht nur eine Karte
// id->Tiefe: in einer Karte ist eine FEHLENDE Zeile von einer Zeile auf Tiefe 0
// nicht zu unterscheiden (Spec §9.1, Muster 3 der Karte).
func TestLogbuchZeigtOffeneAufgabeAlsUeberschriftFuerIhrFertigesKind(t *testing.T) {
	maps := []ReportsMap{hKarte("m",
		hBead("offene-aufgabe", func(b *ReportsBead) {
			b.Ready = true
			b.Children = []ReportsBead{lBead("fertiges-kind", "2026-09-14T06:00:00Z")}
		}),
		lBead("fertige-aufgabe", "2026-09-13T06:00:00Z", func(b *ReportsBead) {
			b.Children = []ReportsBead{lBead("fertiges-enkelkind", "2026-09-13T07:00:00Z")}
		}),
	)}
	a, _ := AnsichtBauen(LogbuchID, maps, jetztTest())
	gleichFolge(t, "Logbuch", zeilenIDs(a),
		[]string{"offene-aufgabe", "fertiges-kind", "fertige-aufgabe", "fertiges-enkelkind"})

	tiefen := map[string]int{}
	fuerUeberschrift := map[string]bool{}
	for _, e := range a.Eintraege {
		tiefen[e.Bead] = e.Tiefe
		fuerUeberschrift[e.Bead] = e.Ueberschrift
	}
	for id, want := range map[string]int{
		"offene-aufgabe": 0, "fertiges-kind": 1,
		"fertige-aufgabe": 0, "fertiges-enkelkind": 1,
	} {
		if tiefen[id] != want {
			t.Errorf("Tiefe von %s = %d, want %d", id, tiefen[id], want)
		}
	}
	if !fuerUeberschrift["offene-aufgabe"] {
		t.Error("offene-aufgabe traegt Ueberschrift=false, sie hat aber keinen eigenen Logbuch-Grund " +
			"(sie ist offen) und steht nur wegen ihres fertigen Kindes da")
	}
	if fuerUeberschrift["fertiges-kind"] || fuerUeberschrift["fertige-aufgabe"] || fuerUeberschrift["fertiges-enkelkind"] {
		t.Errorf("eine echte Logbuch-Zeile traegt Ueberschrift=true: %+v", fuerUeberschrift)
	}

	// Die Zwischenueberschrift zaehlt NICHT in der Kopfbilanz — sie hat keinen
	// eigenen Abschluss, ihr geerbter Grund ist nur geliehen.
	if got := zaehlerWert(a, GrundHeuteErledigt); got != 1 {
		t.Errorf("Zaehler[%q] = %d, want 1 (nur fertiges-kind, nicht die geliehene Ueberschrift)",
			GrundHeuteErledigt, got)
	}
}

// TestLogbuchErklaertSeineReichweite: ein leeres Logbuch heisst nicht "nichts
// gemacht", sondern "in dem, was bd noch liefert, ist nichts abgeschlossen".
// Der Unterschied ist der ganze Hinweis — `bd list -t epic` blendet
// geschlossene Karten aus, und die Rueckschau reicht nur so weit, wie
// collectors.closedEpicsMax sie nachliefert.
func TestLogbuchErklaertSeineReichweite(t *testing.T) {
	a, _ := AnsichtBauen(LogbuchID, nil, jetztTest())
	if len(a.Eintraege) != 0 {
		t.Fatalf("Ansicht ohne Karten hat %d Eintraege", len(a.Eintraege))
	}
	h := strings.ToLower(a.Hinweis)
	if !strings.Contains(h, "geschlossene") || !strings.Contains(h, "epic") {
		t.Errorf("Hinweis = %q, erwartet die Aussage ueber die begrenzte Rueckschau", a.Hinweis)
	}
}

// TestLogbuchKapptUndSagtEsAnDerTagesgrenze ist das Abnahmekriterium von
// gkp-dop: eine begruendete Obergrenze, und die Ansicht sagt, dass sie greift.
//
// Zwoelf Abschlusstage zu je zehn Aufgaben, also 120 Zeilen gegen eine Grenze
// von 100. Die Zahl der Zeilen je Tag ist absichtlich ein Teiler der Grenze:
// so faellt die Grenze mit einer TAGESGRENZE zusammen, und das Ergebnis ist
// genau dann 100, wenn ganze Tage genommen werden. Eine Kappung nach der
// hundertsten ZEILE saehe hier gleich aus — deshalb steht der Unterschied im
// Rahmentest (TestGrenzeKapptAnDerGruppengrenze) und nicht hier; dieser Test
// nagelt die ZAHL und den SATZ fest.
func TestLogbuchKapptUndSagtEsAnDerTagesgrenze(t *testing.T) {
	// Die Ids laufen gegen die erwartete Reihenfolge: der aelteste Tag traegt
	// die kleinsten Ids (Spec §9.1, Muster 1). Bliebe die Kappung von der
	// Id-Sortierung abhaengig, stuenden die alten Tage im Ergebnis.
	var beads []ReportsBead
	for tag := 0; tag < 12; tag++ {
		stempel := time.Date(2026, 9, 14, 6, 0, 0, 0, time.UTC).
			AddDate(0, 0, -tag).Format(time.RFC3339)
		for i := 0; i < 10; i++ {
			beads = append(beads,
				lBead(fmt.Sprintf("%c%02d", 'a'+11-tag, i), stempel))
		}
	}
	maps := []ReportsMap{hKarte("m", beads...)}

	a, ok := AnsichtBauen(LogbuchID, maps, jetztTest())
	if !ok {
		t.Fatal("AnsichtBauen kennt das Logbuch nicht")
	}
	if len(a.Eintraege) != logbuchGrenze {
		t.Errorf("%d Zeilen, erwartet die Kappung auf %d", len(a.Eintraege), logbuchGrenze)
	}
	// Und es sind die JUENGSTEN Tage. Der aelteste Tag traegt die Ids "a…" —
	// steht eine davon drin, hat die Kappung von hinten statt von vorn genommen.
	for _, e := range a.Eintraege {
		if strings.HasPrefix(e.Bead, "a") || strings.HasPrefix(e.Bead, "b") {
			t.Fatalf("%s steht in der gekappten Liste — das sind die zwei aeltesten Tage", e.Bead)
		}
	}
	for _, teil := range []string{"100", "120", "10"} {
		if !strings.Contains(a.Gekappt, teil) {
			t.Errorf("Gekappt nennt %q nicht: %q", teil, a.Gekappt)
		}
	}
	// Der Satz muss die Liste von einer ZU ENDE gegangenen unterscheiden — er
	// nennt deshalb, dass es weitergeht, nicht bloss eine Zahl.
	if !strings.Contains(strings.ToLower(a.Gekappt), "historie") {
		t.Errorf("Gekappt = %q — der Satz sagt nicht, dass es jenseits der Liste weitergeht", a.Gekappt)
	}
}

// TestLogbuchUnterDerGrenzeSagtNichts: die Gegenprobe zum Satz oben. Ein
// normales Logbuch traegt ihn nicht — sonst stuende an jeder vollstaendigen
// Rueckschau eine Einschraenkung, die nicht gilt.
func TestLogbuchUnterDerGrenzeSagtNichts(t *testing.T) {
	maps := []ReportsMap{hKarte("m",
		lBead("heute", "2026-09-14T06:00:00Z"),
		lBead("gestern", "2026-09-13T06:00:00Z"),
	)}
	a, _ := AnsichtBauen(LogbuchID, maps, jetztTest())
	if a.Gekappt != "" {
		t.Errorf("Gekappt = %q, erwartet leer", a.Gekappt)
	}
}

// TestNurDasLogbuchHatEineGrenze haelt die Entscheidung fest, WO die Grenze
// sitzt (gkp-dop).
//
// Der Mechanismus steht im Rahmen — eine Zeile `if r.ID == LogbuchID` in
// ansichtAus waere eine gemeinsame Zeile, die eine einzelne Ansicht beim Namen
// nennt, also genau das, was TestSelectKenntKeinenEinzelnenAnsichtsnamen und
// TestNavigationKommtAusDerRegeltabelle auf der anderen Seite der Naht
// verbieten. Der WERT dagegen gehoert genau einer Ansicht: die fuenf
// Arbeitslisten sind durch die Menge OFFENER Arbeit begrenzt, die sich von
// selbst leert. Aus dem Logbuch geht nie etwas wieder heraus.
//
// Gemessen am 2026-09-14, beads-only: infra Heute 47, Jederzeit 47,
// Eingang 2, Demnaechst 0, Irgendwann 0 — Logbuch 95. In diesem Repo 15/15/0/0/0
// gegen 93.
func TestNurDasLogbuchHatEineGrenze(t *testing.T) {
	for _, r := range Regeln() {
		hat := r.Grenze > 0
		if (r.ID == LogbuchID) != hat {
			t.Errorf("%s: Grenze=%d — eine Grenze hat nur das Logbuch, und es hat sie",
				r.ID, r.Grenze)
		}
		if hat && r.GrenzeSatz == nil {
			t.Errorf("%s hat eine Grenze, aber keinen Satz dazu — sie wuerde still abschneiden", r.ID)
		}
	}
}
