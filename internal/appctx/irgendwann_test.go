package appctx

import (
	"strings"
	"testing"
)

// TestIrgendwannWaehltWeitVertagtes ist Spec §5, Zeile Someday: "defer_until
// weit in der Zukunft".
//
// "Weit" ist eine Entscheidung und steht als IrgendwannHorizont in
// irgendwann.go. Der Test prueft die Kante von beiden Seiten — genau am
// Horizont noch Demnaechst, einen Tag darueber Irgendwann —, weil eine Grenze,
// die nur von einer Seite geprueft wird, um einen Tag danebenliegen darf, ohne
// dass etwas rot wird.
func TestIrgendwannWaehltWeitVertagtes(t *testing.T) {
	maps := []ReportsMap{hKarte("m",
		hBead("weit-vertagt", func(b *ReportsBead) { b.DeferUntil = "2027-03-01T00:00:00Z" }),
		hBead("knapp-darueber", func(b *ReportsBead) { b.DeferUntil = "2026-10-15T00:00:00Z" }),

		// und was NICHT hineingehoert:
		hBead("genau-am-horizont", func(b *ReportsBead) { b.DeferUntil = "2026-10-14T00:00:00Z" }),
		hBead("naechste-woche", func(b *ReportsBead) { b.DeferUntil = "2026-09-21T00:00:00Z" }),
		hBead("wann-vorbei", func(b *ReportsBead) { b.DeferUntil = "2026-08-01T00:00:00Z" }),
		hBead("frist-fern-ohne-wann", func(b *ReportsBead) { b.DueAt = "2027-03-01T00:00:00Z" }),
		hBead("startklar", func(b *ReportsBead) { b.Ready = true }),
		hBead("geschlossen-vertagt", func(b *ReportsBead) {
			b.Status = "closed"
			b.DeferUntil = "2027-03-01T00:00:00Z"
		}),
	)}

	a, ok := AnsichtBauen(IrgendwannID, maps, jetztTest())
	if !ok {
		t.Fatal(`AnsichtBauen("irgendwann") kennt die Ansicht nicht`)
	}
	dabei := map[string]bool{}
	for _, e := range a.Eintraege {
		dabei[e.Bead] = true
	}
	for _, id := range []string{"weit-vertagt", "knapp-darueber"} {
		if !dabei[id] {
			t.Errorf("%s fehlt in Irgendwann", id)
		}
	}
	for _, id := range []string{
		"genau-am-horizont", "naechste-woche", "wann-vorbei",
		"frist-fern-ohne-wann", "startklar", "geschlossen-vertagt",
	} {
		if dabei[id] {
			t.Errorf("%s steht in Irgendwann, gehoert aber nicht hinein", id)
		}
	}
}

// TestIrgendwannUndDemnaechstTeilenSichDieMenge haelt die Abgrenzung als
// EIGENE Aussage fest: dieselbe Aufgabe darf nicht in beiden Listen stehen, und
// keine darf zwischen ihnen durchfallen.
//
// Ohne diesen Test koennte jede der beiden Regeln fuer sich richtig aussehen
// und die Paarung trotzdem eine Luecke oder eine Doppelung haben — die beiden
// Dateien kennen einander nur ueber IrgendwannHorizont.
func TestIrgendwannUndDemnaechstTeilenSichDieMenge(t *testing.T) {
	maps := []ReportsMap{hKarte("m",
		hBead("nah", func(b *ReportsBead) { b.DeferUntil = "2026-09-20T00:00:00Z" }),
		hBead("am-horizont", func(b *ReportsBead) { b.DeferUntil = "2026-10-14T00:00:00Z" }),
		hBead("jenseits", func(b *ReportsBead) { b.DeferUntil = "2026-10-15T00:00:00Z" }),
		hBead("jenseits-mit-frist", func(b *ReportsBead) {
			b.DeferUntil = "2027-01-10T00:00:00Z"
			b.DueAt = "2027-02-01T00:00:00Z"
		}),
	)}
	d, _ := AnsichtBauen(DemnaechstID, maps, jetztTest())
	i, _ := AnsichtBauen(IrgendwannID, maps, jetztTest())

	in := func(a AufgabenAnsicht) map[string]bool {
		m := map[string]bool{}
		for _, e := range a.Eintraege {
			m[e.Bead] = true
		}
		return m
	}
	dm, im := in(d), in(i)
	for _, id := range []string{"nah", "am-horizont", "jenseits", "jenseits-mit-frist"} {
		switch {
		case dm[id] && im[id]:
			t.Errorf("%s steht in BEIDEN Listen", id)
		case !dm[id] && !im[id]:
			t.Errorf("%s steht in KEINER der beiden Listen", id)
		}
	}
	// Und die Seite, auf der jede landet — sonst waere "genau eine von beiden"
	// auch dann erfuellt, wenn die Grenze spiegelverkehrt liegt.
	gleichFolge(t, "Demnaechst", zeilenIDs(d), []string{"nah", "am-horizont"})
	gleichFolge(t, "Irgendwann", zeilenIDs(i), []string{"jenseits", "jenseits-mit-frist"})
}

// TestIrgendwannSortiertNaechsteRueckkehrZuerst. Was am ehesten wieder auf den
// Tisch kommt, steht oben.
//
// Die Ids laufen gegen die Erwartung (c vor b vor a) und die Eingabe steht
// verkehrt herum. Sonst haette die Id-Sortierung am Ende von sortiereGruppen
// die erwartete Folge schon von selbst hergestellt, und der Test waere auch
// ohne Zeitpunkt im Urteil gruen — Spec §9.1, Muster 2.
func TestIrgendwannSortiertNaechsteRueckkehrZuerst(t *testing.T) {
	maps := []ReportsMap{hKarte("m",
		hBead("a-naechstes-jahr", func(b *ReportsBead) { b.DeferUntil = "2027-06-01T00:00:00Z" }),
		hBead("b-im-winter", func(b *ReportsBead) { b.DeferUntil = "2026-12-01T00:00:00Z" }),
		hBead("c-im-oktober", func(b *ReportsBead) { b.DeferUntil = "2026-10-20T00:00:00Z" }),
	)}
	a, _ := AnsichtBauen(IrgendwannID, maps, jetztTest())
	gleichFolge(t, "Irgendwann", zeilenIDs(a),
		[]string{"c-im-oktober", "b-im-winter", "a-naechstes-jahr"})
}

// TestIrgendwannErklaertSichLeer: leer heisst hier "nichts vertagt", und das
// ist eine Auskunft — aber nur, wenn die Ansicht sagt, WORAUF sie schaut und ab
// wann "weit" gilt.
func TestIrgendwannErklaertSichLeer(t *testing.T) {
	a, _ := AnsichtBauen(IrgendwannID, nil, jetztTest())
	if len(a.Eintraege) != 0 {
		t.Fatalf("Ansicht ohne Karten hat %d Eintraege", len(a.Eintraege))
	}
	h := strings.ToLower(a.Hinweis)
	if !strings.Contains(h, "defer_until") {
		t.Errorf("Hinweis = %q, erwartet das Feld, aus dem die Ansicht gebaut wird", a.Hinweis)
	}
	if !strings.Contains(a.Hinweis, itoa(IrgendwannHorizont)) {
		t.Errorf("Hinweis = %q, erwartet den Horizont (%d Tage) im Klartext — "+
			"eine Grenze, die nur im Code steht, ist fuer den Leser keine",
			a.Hinweis, IrgendwannHorizont)
	}
}
