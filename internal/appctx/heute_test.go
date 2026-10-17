package appctx

import (
	"strings"
	"testing"
	"time"
)

// Die Zone ist in JEDEM Test dieser Datei festgenagelt, und zwar mit einem
// Versatz ungleich null. Unter TZ=UTC ist die Kompensation aus
// bdops.ParseUserDateIn unsichtbar — der Versatz ist dann null, also sieht eine
// falsche Deutung genauso aus wie die richtige. Ein Test, der die Zone der
// Maschine nimmt, ist auf einem UTC-Bauagenten wertlos.
//
// FixedZone statt LoadLocation, damit der Test auch ohne tzdata laeuft; die
// echte Zone mit Sommerzeit kommt in TestHeuteDeutetFristAlsWanduhrNichtAlsUTC
// dazu.
var testZone = time.FixedZone("Test+02", 2*60*60)

// jetztTest ist der Bezugspunkt aller Tests hier: Montag, 14.09.2026, 10:00
// Ortszeit. Weit genug von Mitternacht in beide Richtungen, damit kein Test
// zufaellig richtig liegt, weil der Tageswechsel gerade mitspielt.
func jetztTest() time.Time { return time.Date(2026, 9, 14, 10, 0, 0, 0, testZone) }

// bead baut eine Aufgabe mit den Feldern, auf die die Auswahl schaut.
func hBead(id string, anpassen ...func(*ReportsBead)) ReportsBead {
	b := ReportsBead{ID: id, Title: id, Status: "open", Priority: 2,
		LastChange: "2026-09-01T00:00:00Z"}
	for _, f := range anpassen {
		f(&b)
	}
	return b
}

func hKarte(id string, beads ...ReportsBead) ReportsMap {
	return ReportsMap{ID: id, Title: id, Beads: beads}
}

// ids ist die Zeilenfolge einer Ansicht — genau das, was die Auswahl
// entscheidet, und genau das, was ein Test festnageln muss.
func zeilenIDs(a AufgabenAnsicht) []string {
	out := make([]string, 0, len(a.Eintraege))
	for _, e := range a.Eintraege {
		out = append(out, e.Bead)
	}
	return out
}

func gleichFolge(t *testing.T, was string, got, want []string) {
	t.Helper()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("%s = %v, want %v", was, got, want)
	}
}

// TestHeuteVereinigtFristUndWannOhneStartklar ist Spec §5 nach der zweiten
// Korrektur (gkp-89g, 2026-09-16): due_at <= heute VEREINIGT mit
// defer_until <= heute VEREINIGT mit in_progress — "startklar" (bd ready)
// gehoert seitdem NICHT mehr dazu.
//
// Der Ausschluss ist der Kern dieses Tests, nicht ein Nebeneffekt: ohne ihn
// wuerde jeder startklare Bead die Liste fuellen, genau die Beobachtung
// (0 von 1232 Beads mit due_at/defer_until), die zur Korrektur gefuehrt hat.
// Deshalb steht "startklar" absichtlich zweimal im Test — einmal als Beleg,
// dass er aus Heute VERSCHWINDET, einmal als Beleg, dass er in Jederzeit
// bleibt. Ein Test, der nur die erste Haelfte prueft, koennte auch gruen
// sein, weil die ganze Auswahl kaputt ist.
func TestHeuteVereinigtFristUndWannOhneStartklar(t *testing.T) {
	maps := []ReportsMap{hKarte("m",
		hBead("startklar", func(b *ReportsBead) { b.Ready = true }),
		hBead("frist-heute", func(b *ReportsBead) { b.DueAt = "2026-09-14T23:00:00Z" }),
		hBead("frist-gestern", func(b *ReportsBead) { b.DueAt = "2026-09-13T09:00:00Z" }),
		hBead("wann-heute", func(b *ReportsBead) { b.DeferUntil = "2026-09-14T00:00:00Z" }),
		hBead("wann-frueher", func(b *ReportsBead) { b.DeferUntil = "2026-08-01T00:00:00Z" }),

		// und was NICHT hineingehoert:
		hBead("frist-morgen", func(b *ReportsBead) { b.DueAt = "2026-09-15T00:00:00Z" }),
		hBead("wann-morgen", func(b *ReportsBead) { b.DeferUntil = "2026-09-15T00:00:00Z" }),
		hBead("ohne-alles"),
		hBead("geschlossen-mit-frist", func(b *ReportsBead) {
			b.Status = "closed"
			b.DueAt = "2026-09-13T09:00:00Z"
		}),
	)}

	a, ok := AnsichtBauen("heute", maps, jetztTest())
	if !ok {
		t.Fatal(`AnsichtBauen("heute") kennt die Ansicht nicht`)
	}
	dabei := map[string]bool{}
	gruende := map[string][]string{}
	for _, e := range a.Eintraege {
		dabei[e.Bead] = true
		gruende[e.Bead] = e.Gruende
	}
	for _, id := range []string{"frist-heute", "frist-gestern", "wann-heute", "wann-frueher"} {
		if !dabei[id] {
			t.Errorf("%s fehlt in Heute", id)
		}
	}
	for _, id := range []string{"startklar", "frist-morgen", "wann-morgen", "ohne-alles", "geschlossen-mit-frist"} {
		if dabei[id] {
			t.Errorf("%s steht in Heute, gehoert aber nicht hinein", id)
		}
	}
	if !enthaelt(gruende["wann-frueher"], GrundGeplant) {
		t.Errorf("Gruende(wann-frueher) = %v, erwartet den Grund %q", gruende["wann-frueher"], GrundGeplant)
	}

	// Startklares faellt aus Heute heraus, bleibt aber vollstaendig in
	// Jederzeit stehen — der Wegfall verliert nichts, was dort nicht ohnehin
	// steht.
	j, ok := AnsichtBauen("jederzeit", maps, jetztTest())
	if !ok {
		t.Fatal(`AnsichtBauen("jederzeit") kennt die Ansicht nicht`)
	}
	jDabei := map[string]bool{}
	for _, e := range j.Eintraege {
		jDabei[e.Bead] = true
	}
	if !jDabei["startklar"] {
		t.Error("startklar fehlt in Jederzeit — es darf dort nicht mitverschwinden")
	}
}

// TestHeuteNimmtInArbeitAuf haelt die EINE bewusste Abweichung von der Formel
// in Spec §5 fest, samt ihrer Messung: `bd ready` liefert einen Bead auf
// in_progress NICHT (gemessen 2026-09-14 gegen bd 1.2.2 an einer Wegwerf-DB —
// von drei Beads meldete ready nur den offenen, nicht den auf in_progress).
// Ohne diese vierte Quelle zeigte eine Ansicht namens "Heute" ausgerechnet die
// Aufgabe nicht, an der gerade gearbeitet wird.
func TestHeuteNimmtInArbeitAuf(t *testing.T) {
	maps := []ReportsMap{hKarte("m", hBead("laeuft", func(b *ReportsBead) {
		b.Status = "in_progress"
		b.Ready = false
	}))}
	a, _ := AnsichtBauen("heute", maps, jetztTest())
	gleichFolge(t, "Heute", zeilenIDs(a), []string{"laeuft"})
	if len(a.Eintraege) == 1 && !enthaelt(a.Eintraege[0].Gruende, "in Arbeit") {
		t.Errorf("Gruende = %v, erwartet den benannten Grund \"in Arbeit\"", a.Eintraege[0].Gruende)
	}
}

func enthaelt(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// TestHeuteDeutetFristAlsWanduhrNichtAlsUTC ist der Test, um den es bei
// gkp-lng ging, eine Ebene hoeher: bd schreibt due_at als LOKALE Wanduhr mit
// einem Z, das luegt (Spec §4.4). Wer das Z ernst nimmt, verschiebt einen
// Abendtermin um den UTC-Versatz auf den Folgetag — und damit in einen anderen
// Eimer.
//
// Zone ist Asia/Tokyo (+9, keine Sommerzeit): 23:00 am 14.09. ist dort HEUTE,
// als echtes UTC gelesen waere derselbe Stempel 08:00 am 15.09. und die Aufgabe
// faellt aus der Liste. Der Versatz muss gross genug sein, um einen Tag zu
// ueberspringen — mit +2 schafft es nur ein Termin nach 22:00.
func TestHeuteDeutetFristAlsWanduhrNichtAlsUTC(t *testing.T) {
	tokio, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		t.Skipf("keine tzdata fuer Asia/Tokyo: %v", err)
	}
	jetzt := time.Date(2026, 9, 14, 10, 0, 0, 0, tokio)
	maps := []ReportsMap{hKarte("m",
		hBead("abends", func(b *ReportsBead) { b.DueAt = "2026-09-14T23:00:00Z" }),
	)}
	a, _ := AnsichtBauen("heute", maps, jetzt)
	gleichFolge(t, "Heute in Tokio", zeilenIDs(a), []string{"abends"})
}

// TestHeuteDeutetNichtDieEreignisstempel ist die andere Haelfte derselben
// Regel: created_at/updated_at/closed_at sind echtes UTC und duerfen NICHT
// durch dieselbe Kompensation. Sie kommen in der Auswahl gar nicht vor — der
// Test haelt fest, dass ein last_change von heute abend keine Aufgabe in die
// Liste hebt, und faellt damit auf, wenn jemand die beiden Zeitbasen
// zusammenwirft.
func TestHeuteDeutetNichtDieEreignisstempel(t *testing.T) {
	maps := []ReportsMap{hKarte("m",
		hBead("nur-angefasst", func(b *ReportsBead) { b.LastChange = "2026-09-14T08:00:00Z" }),
	)}
	a, _ := AnsichtBauen("heute", maps, jetztTest())
	gleichFolge(t, "Heute", zeilenIDs(a), nil)
}

// TestHeuteSortiertUeberfaelligVorFaelligVorRest nagelt die Reihenfolge fest.
//
// ZWEI Koeder laufen gegen die Erwartung, und beide braucht es:
//
//   - die EINGABE steht in der Gegenreihenfolge. Eine Auswahl, die nur filtert
//     und die Eingabefolge durchreicht, ist damit rot.
//   - die IDS stehen ebenfalls in der Gegenreihenfolge (z… vor v…). Letzter
//     Vergleich in sortiereGruppen ist die Id AUFSTEIGEND; mit den frueheren
//     Namen a-laenger-ueberfaellig … d-startklar war die Erwartung exakt die
//     aufsteigende Id-Folge, und damit war der Kern des Tests unbewacht: eine
//     Gegenprobe, die den GANZEN Sortierschluessel durch den Id-Tiebreak
//     ersetzte, machte 17 Tests rot — diesen nicht (CP9, 2026-09-14).
//
// v-in-arbeit war bis gkp-89g ein startklarer Bead (Ready=true); "startklar"
// zaehlt seit der zweiten Korrektur nicht mehr in Heute, deshalb traegt die
// Rest-Zeile hier in_progress statt Ready — der Rang (rangSonst) und damit
// die geprueften Reihenfolge bleiben unveraendert.
func TestHeuteSortiertUeberfaelligVorFaelligVorRest(t *testing.T) {
	maps := []ReportsMap{hKarte("m",
		hBead("v-in-arbeit", func(b *ReportsBead) { b.Status = istInArbeit }),
		hBead("w-heute-spaet", func(b *ReportsBead) { b.DueAt = "2026-09-14T18:00:00Z" }),
		hBead("x-heute-frueh", func(b *ReportsBead) { b.DueAt = "2026-09-14T07:00:00Z" }),
		hBead("y-ueberfaellig", func(b *ReportsBead) { b.DueAt = "2026-09-10T12:00:00Z" }),
		hBead("z-laenger-ueberfaellig", func(b *ReportsBead) { b.DueAt = "2026-09-02T12:00:00Z" }),
	)}
	a, _ := AnsichtBauen("heute", maps, jetztTest())
	gleichFolge(t, "Reihenfolge", zeilenIDs(a), []string{
		"z-laenger-ueberfaellig", "y-ueberfaellig",
		"x-heute-frueh", "w-heute-spaet",
		"v-in-arbeit",
	})
}

// TestHeuteRangSchlaegtDenAelterenZeitpunkt ist die Fixture, die die
// RANG-Stufe des Sortierschluessels tragend macht — und bis 2026-09-14 fehlte
// sie in dieser Karte.
//
// Gemessen (CP10): schaltet man allein die Rang-Stufe in sortiereGruppen ab,
// wird in der ganzen Karte genau EIN Test rot, und der steht in Jederzeit. Der
// Grund ist, dass die uebrigen Ansichten Rang und Zeitpunkt aus derselben
// Groesse ableiten — im Logbuch sind beide sogar strikt gegenlaeufig monoton
// (Begruendung an waehltLogbuch). In HEUTE ist das anders, und genau das haelt
// dieser Test fest: waehltHeute setzt fuer Rang 0/1 den Zeitpunkt aus der
// FRIST, fuer den Rest (GrundGeplant) aber aus dem WANN. Die beiden Felder sind
// unabhaengig, also kann der Rest einen AELTEREN Zeitpunkt tragen als die
// ueberfaellige Zeile — hier: defer_until vor sechs Wochen gegen eine Frist von
// gestern. Ohne die Rang-Stufe stuende die Rest-Zeile oben, und eine Liste
// namens "Heute" begaenne mit dem, was gerade erst sichtbar geworden ist.
//
// Die Ids laufen zusaetzlich gegen die Erwartung (z… vor a…), damit der
// Id-Tiebreak die Folge nicht von sich aus herstellt (Spec §9.1, Muster 2).
func TestHeuteRangSchlaegtDenAelterenZeitpunkt(t *testing.T) {
	maps := []ReportsMap{hKarte("m",
		hBead("a-rest-wann-vor-sechs-wochen", func(b *ReportsBead) {
			b.DeferUntil = "2026-08-03T00:00:00Z"
		}),
		hBead("z-ueberfaellig-seit-gestern", func(b *ReportsBead) {
			b.DueAt = "2026-09-13T09:00:00Z"
		}),
	)}
	a, _ := AnsichtBauen("heute", maps, jetztTest())
	gleichFolge(t, "Reihenfolge", zeilenIDs(a),
		[]string{"z-ueberfaellig-seit-gestern", "a-rest-wann-vor-sechs-wochen"})

	// Die Raenge stehen mit im Test, weil die Folge sonst auch aus einem
	// zufaellig passenden Zeitvergleich kommen koennte: 0 vor 2 ist die
	// Behauptung, und nur der Rang traegt sie.
	raenge := map[string]int{}
	for _, e := range a.Eintraege {
		raenge[e.Bead] = e.Rang
	}
	for id, want := range map[string]int{
		"z-ueberfaellig-seit-gestern":  rangUeberfaellig,
		"a-rest-wann-vor-sechs-wochen": rangSonst,
	} {
		if raenge[id] != want {
			t.Errorf("Rang von %s = %d, want %d", id, raenge[id], want)
		}
	}
}

// TestHeuteSortiertGleichstandNachPrioritaetDannId: ohne einen vollstaendigen
// Schluessel haengt die Folge zweier gleichwertiger Zeilen an der Reihenfolge,
// in der bd sie geliefert hat — und die ist weder chronologisch noch stabil.
// Die Eingabe steht wieder verkehrt herum. Die drei Beads tragen in_progress
// statt Ready=true (seit gkp-89g zaehlt "startklar" nicht mehr in Heute); am
// Rang (rangSonst) und am fehlenden Zeitpunkt aendert das nichts, der
// Gleichstand entsteht wie zuvor ueber Prioritaet und Id.
func TestHeuteSortiertGleichstandNachPrioritaetDannId(t *testing.T) {
	maps := []ReportsMap{hKarte("m",
		hBead("z-p2", func(b *ReportsBead) { b.Status = istInArbeit; b.Priority = 2 }),
		hBead("a-p2", func(b *ReportsBead) { b.Status = istInArbeit; b.Priority = 2 }),
		hBead("m-p0", func(b *ReportsBead) { b.Status = istInArbeit; b.Priority = 0 }),
	)}
	a, _ := AnsichtBauen("heute", maps, jetztTest())
	gleichFolge(t, "Gleichstand", zeilenIDs(a), []string{"m-p0", "a-p2", "z-p2"})
}

// TestHeuteRuecktUnteraufgabenEin: eine ausgewaehlte Unteraufgabe folgt ihrer
// ausgewaehlten Aufgabe und wird eingerueckt. Ein Enkel, dessen Elternteil
// NICHT ausgewaehlt ist, steht seit gkp-a1d NICHT MEHR auf Ebene 0 — der
// Elternteil erscheint stattdessen als ZWISCHENUEBERSCHRIFT und traegt den
// Nachfahren eine Ebene tiefer (Spec §5). Das war vor diesem Ticket anders
// (der Elternteil verschwand spurlos, die Nachfahren ruckten auf), und die
// zweite Haelfte dieses Tests haelt jetzt genau den neuen Befund fest, statt
// den alten.
//
// Die ausgewaehlten Knoten tragen in_progress statt Ready=true: seit gkp-89g
// waehlt Heute "startklar" nicht mehr, und dieser Test prueft die Einrueckung,
// nicht den Aufnahmegrund — jeder andere Heute-Grund taete es hier ebenso.
func TestHeuteRuecktUnteraufgabenEin(t *testing.T) {
	maps := []ReportsMap{hKarte("m",
		hBead("eltern", func(b *ReportsBead) {
			b.Status = istInArbeit
			b.Children = []ReportsBead{
				hBead("kind", func(k *ReportsBead) {
					k.DueAt = "2026-09-14T09:00:00Z"
					k.Children = []ReportsBead{hBead("enkel", func(e *ReportsBead) { e.Status = istInArbeit })}
				}),
			}
		}),
		hBead("fremder-enkel-traeger", func(b *ReportsBead) {
			b.Children = []ReportsBead{
				hBead("stiller", func(k *ReportsBead) {
					k.Children = []ReportsBead{hBead("tiefer-treffer", func(e *ReportsBead) { e.Status = istInArbeit })}
				}),
			}
		}),
	)}
	a, _ := AnsichtBauen("heute", maps, jetztTest())

	// Erst die VOLLSTAENDIGE Folge, dann die Tiefen. Die Reihenfolge dieser
	// beiden Pruefungen ist nicht egal, und der Grund steht in der Gegenprobe,
	// die diesen Test einmal ueberlebt hat: mit einer Karte id->Tiefe und einem
	// erwarteten Wert ist eine FEHLENDE Zeile von einer vorhandenen nicht zu
	// unterscheiden — der Nullwert der Karte beantwortet beides gleich. Die
	// Folge unten nennt jede Zeile, also faellt eine verschwundene sofort auf.
	//
	// "fremder-enkel-traeger" und "stiller" haben KEINEN eigenen Grund (kein
	// Termin, nicht in Arbeit) — vor diesem Ticket waeren beide spurlos
	// verschwunden und "tiefer-treffer" auf Ebene 0 aufgestiegen. Jetzt stehen
	// beide als Zwischenueberschrift da, jede eine Ebene ueber ihrem
	// dringendsten Nachfahren.
	gleichFolge(t, "Zeilenfolge", zeilenIDs(a),
		[]string{"eltern", "kind", "enkel", "fremder-enkel-traeger", "stiller", "tiefer-treffer"})

	tiefen := map[string]int{}
	fuerUeberschrift := map[string]bool{}
	for _, e := range a.Eintraege {
		tiefen[e.Bead] = e.Tiefe
		fuerUeberschrift[e.Bead] = e.Ueberschrift
	}
	for id, want := range map[string]int{
		"eltern": 0, "kind": 1, "enkel": 2,
		"fremder-enkel-traeger": 0, "stiller": 1, "tiefer-treffer": 2,
	} {
		if tiefen[id] != want {
			t.Errorf("Tiefe von %s = %d, want %d", id, tiefen[id], want)
		}
	}
	for id, want := range map[string]bool{
		"eltern": false, "kind": false, "enkel": false,
		"fremder-enkel-traeger": true, "stiller": true, "tiefer-treffer": false,
	} {
		if fuerUeberschrift[id] != want {
			t.Errorf("Ueberschrift(%s) = %v, want %v", id, fuerUeberschrift[id], want)
		}
	}
}

// TestHeuteNenntKarteUndHerkunft. Das Abzeichen ist die Vorleistung fuer
// gkp-pf2; es steht hier, damit die Zeile es zeichnen kann, bevor es eine
// zweite Herkunft gibt.
func TestHeuteNenntKarteUndHerkunft(t *testing.T) {
	maps := []ReportsMap{hKarte("m", hBead("x", func(b *ReportsBead) { b.Status = istInArbeit }))}
	a, _ := AnsichtBauen("heute", maps, jetztTest())
	if len(a.Eintraege) != 1 {
		t.Fatalf("Eintraege = %d, want 1", len(a.Eintraege))
	}
	e := a.Eintraege[0]
	if e.Karte != "m" {
		t.Errorf("Karte = %q, want %q", e.Karte, "m")
	}
	if e.Herkunft != HerkunftBeads {
		t.Errorf("Herkunft = %q, want %q", e.Herkunft, HerkunftBeads)
	}
}

// TestHeuteBeschreibtDieGithubLuecke: GitHub-Aufgaben haben kein Gegenstueck zu
// When/Deadline (Spec §4.4), koennen also nie in Heute stehen. Das MUSS in der
// Ansicht sichtbar sein, "nicht stillschweigend" — deshalb ein Hinweistext und
// keine leere Sektion.
func TestHeuteBeschreibtDieGithubLuecke(t *testing.T) {
	a, _ := AnsichtBauen("heute", nil, jetztTest())
	if !strings.Contains(strings.ToLower(a.Hinweis), "github") {
		t.Errorf("Hinweis = %q, erwartet eine Aussage ueber GitHub", a.Hinweis)
	}
}

// TestHeuteZaehltDieEimer — die Zahlen im Kopf der Ansicht kommen aus Go, nicht
// aus einer zweiten Zaehlung im Browser. Der dritte Eimer traegt seit gkp-89g
// in_progress statt Ready=true, weil "startklar" nicht mehr in Heute zaehlt.
func TestHeuteZaehltDieEimer(t *testing.T) {
	maps := []ReportsMap{hKarte("m",
		hBead("u1", func(b *ReportsBead) { b.DueAt = "2026-09-10T12:00:00Z" }),
		hBead("u2", func(b *ReportsBead) { b.DueAt = "2026-09-11T12:00:00Z" }),
		hBead("f1", func(b *ReportsBead) { b.DueAt = "2026-09-14T12:00:00Z" }),
		hBead("p1", func(b *ReportsBead) { b.Status = istInArbeit }),
	)}
	a, _ := AnsichtBauen("heute", maps, jetztTest())
	for name, want := range map[string]int{"überfällig": 2, "fällig heute": 1, "in Arbeit": 1} {
		if got := zaehlerWert(a, name); got != want {
			t.Errorf("Zaehler[%q] = %d, want %d (alle: %v)", name, got, want, a.Zaehler)
		}
	}
}

// TestHeuteZaehltInDringlichkeitsfolge: die Bilanz im Kopf ist eine LISTE, und
// ihre Reihenfolge ist Inhalt — dringend zuerst.
//
// Das war eine Karte, die der Browser alphabetisch sortiert hat. Unsichtbar
// falsch: die JS-Vorgabesortierung vergleicht Code-Einheiten, das "ü" in
// "überfällig" ist U+00FC und damit groesser als jeder ASCII-Buchstabe — der
// dringendste Eimer stand ganz rechts, hinter dem dritten Grund. Kein
// Go-Test konnte das sehen, solange die Reihenfolge erst im Browser entstand.
// Jetzt entsteht sie hier und wird hier geprueft. Der dritte Bead traegt seit
// gkp-89g in_progress statt Ready=true.
func TestHeuteZaehltInDringlichkeitsfolge(t *testing.T) {
	maps := []ReportsMap{hKarte("m",
		hBead("p1", func(b *ReportsBead) { b.Status = istInArbeit }),
		hBead("f1", func(b *ReportsBead) { b.DueAt = "2026-09-14T12:00:00Z" }),
		hBead("u1", func(b *ReportsBead) { b.DueAt = "2026-09-10T12:00:00Z" }),
	)}
	a, _ := AnsichtBauen("heute", maps, jetztTest())
	var folge []string
	for _, z := range a.Zaehler {
		folge = append(folge, z.Grund)
	}
	gleichFolge(t, "Zaehlerfolge", folge,
		[]string{GrundUeberfaellig, GrundFaelligHeute, GrundInArbeit})
}

// TestHeuteTraegtTageUndTermintext: die Zeile zeigt "seit 4 Tagen" bzw. "heute"
// ohne selbst zu rechnen. Der Tagesabstand ist ein KALENDERtagabstand, nicht
// eine durch 24 geteilte Stundenzahl — sonst kippt er an jeder Zeitumstellung
// und bei jedem Termin, der nicht auf Mitternacht liegt.
func TestHeuteTraegtTageUndTermintext(t *testing.T) {
	maps := []ReportsMap{hKarte("m",
		hBead("gestern", func(b *ReportsBead) { b.DueAt = "2026-09-13T23:30:00Z" }),
		hBead("heute", func(b *ReportsBead) { b.DueAt = "2026-09-14T00:30:00Z" }),
	)}
	a, _ := AnsichtBauen("heute", maps, jetztTest())
	tage := map[string]int{}
	text := map[string]string{}
	for _, e := range a.Eintraege {
		if e.FristTage == nil {
			t.Fatalf("%s ohne FristTage", e.Bead)
		}
		tage[e.Bead] = *e.FristTage
		text[e.Bead] = e.Frist
	}
	if tage["gestern"] != -1 {
		t.Errorf("FristTage(gestern) = %d, want -1", tage["gestern"])
	}
	if tage["heute"] != 0 {
		t.Errorf("FristTage(heute) = %d, want 0", tage["heute"])
	}
	if text["heute"] != "2026-09-14 00:30" {
		t.Errorf("Frist(heute) = %q, want %q", text["heute"], "2026-09-14 00:30")
	}
}

// TestAnsichtenSindEineTabelle: die Naht fuer gkp-1oj/gkp-pf2/gkp-8ww. Eine
// weitere Ansicht ist ein weiterer Eintrag, kein Eingriff in bestehende Zeilen.
func TestAnsichtenSindEineTabelle(t *testing.T) {
	if _, ok := AnsichtBauen("gibt-es-nicht", nil, jetztTest()); ok {
		t.Error("AnsichtBauen meldet eine unbekannte Ansicht als gebaut")
	}
	gefunden := false
	for _, r := range Regeln() {
		if r.ID == "heute" {
			gefunden = true
			if r.Titel == "" {
				t.Error("Regel heute ohne Titel — die Navigation braucht ihn")
			}
		}
	}
	if !gefunden {
		t.Error("Regeln() nennt heute nicht")
	}
}

// TestAnsichtenBauenLiefertJedeRegel: der eine Aufruf, den CollectReportsData
// und die Schreibrouten machen. Er darf keine Ansicht auslassen, sonst haengt
// eine neue Ansicht an einer zweiten Registrierung, die jemand vergisst.
func TestAnsichtenBauenLiefertJedeRegel(t *testing.T) {
	alle := AnsichtenBauen(nil, jetztTest())
	for _, r := range Regeln() {
		if _, ok := alle[r.ID]; !ok {
			t.Errorf("AnsichtenBauen laesst %q aus", r.ID)
		}
	}
}

// TestNavigationKommtAusDerRegeltabelle: die Navigationseintraege der
// Aufgaben-Ansichten werden ABGELEITET, nicht danebengeschrieben. Sonst waere
// jede neue Ansicht (gkp-1oj) zwei Eintragungen weit entfernt, von denen man
// die zweite vergisst — und die Ansicht existierte dann, ohne erreichbar zu
// sein.
func TestNavigationKommtAusDerRegeltabelle(t *testing.T) {
	reportsc := map[string]any{"ansichten": map[string]any{
		"heute": map[string]any{
			"eintraege": []any{map[string]any{"bead": "a"}, map[string]any{"bead": "b"}},
			"zaehler":   []any{map[string]any{"grund": GrundUeberfaellig, "anzahl": 1.0}},
		},
	}}
	items := ansichtenNav(reportsc)
	if len(items) != len(Regeln()) {
		t.Fatalf("Navigation hat %d Eintraege, die Tabelle %d", len(items), len(Regeln()))
	}
	var heute map[string]any
	for _, it := range items {
		m := aMap(it)
		if m["id"] == "heute" {
			heute = m
		}
	}
	if heute == nil {
		t.Fatal("kein Navigationseintrag fuer heute")
	}
	if heute["label"] != "Heute" {
		t.Errorf("label = %v, want Heute", heute["label"])
	}
	if heute["cnt"] != "2" {
		t.Errorf("cnt = %v, want \"2\" — die Zahl wird abgelesen, nicht nachgerechnet", heute["cnt"])
	}
	if heute["dot"] != "r" {
		t.Errorf("dot = %v, want r — ueberfaellige Arbeit ist die einzige Zahl in einer "+
			"Aufgabenliste, die eine Beanstandung ist", heute["dot"])
	}
}

// TestNavigationOhneUeberfaelligesIstNichtRot ist die Gegenseite: waere der
// Punkt immer rot, sagte er nichts.
func TestNavigationOhneUeberfaelligesIstNichtRot(t *testing.T) {
	items := ansichtenNav(map[string]any{"ansichten": map[string]any{
		"heute": map[string]any{"eintraege": []any{map[string]any{"bead": "a"}}, "zaehler": []any{}},
	}})
	for _, it := range items {
		if m := aMap(it); m["id"] == "heute" && m["dot"] != "c" {
			t.Errorf("dot = %v, want c (Arbeit da, nichts ueberfaellig)", m["dot"])
		}
	}
}

// zaehlerWert liest einen Eimer aus der Bilanz. Die Bilanz ist eine Liste, weil
// ihre Reihenfolge Inhalt ist; fuer die Frage "wie viele" ist das unhandlich,
// also genau hier einmal uebersetzen statt in jedem Test.
func zaehlerWert(a AufgabenAnsicht, grund string) int {
	for _, z := range a.Zaehler {
		if z.Grund == grund {
			return z.Anzahl
		}
	}
	return 0
}

// TestHeuteLeerTextNenntJedenGrund ist die Kopplung, die gkp-9di gefehlt hat:
// der Leer-Text nannte drei von vier Gruenden, GrundGeplant fehlte. Statt die
// vier Woerter im Text nachzutragen, bewacht dieser Test unabhaengig von
// heuteLeerText, dass JEDER Grund, den waehltHeute tatsaechlich zurueckgibt,
// im Text auftaucht — er sammelt die Gruende ueber vier Kandidaten, die je
// GENAU einen der vier Zweige in waehltHeute ausloesen, und nicht ueber die
// interne Liste gruendeHeute selbst (sonst pruefte der Test nur sich selbst).
//
// Rot wird er, wenn ein Grund aus dem Text entfernt wird, waehrend waehltHeute
// ihn weiter vergibt — probiert und bestaetigt fuer gkp-9di.
func TestHeuteLeerTextNenntJedenGrund(t *testing.T) {
	heute := jetztTest()
	tagAnfang := tagesAnfang(heute)
	gestern := -1
	null := 0
	kandidaten := []Kandidat{
		// ueberfaellig: FristTage < 0
		{Bead: hBead("k-ueberfaellig"), Heute: tagAnfang, Jetzt: heute, FristTage: &gestern},
		// faellig heute: FristTage == 0
		{Bead: hBead("k-faellig"), Heute: tagAnfang, Jetzt: heute, FristTage: &null},
		// in Arbeit: Status == in_progress
		{Bead: hBead("k-arbeit", func(b *ReportsBead) { b.Status = istInArbeit }), Heute: tagAnfang, Jetzt: heute},
		// geplant: WannTage <= 0
		{Bead: hBead("k-geplant"), Heute: tagAnfang, Jetzt: heute, WannTage: &null},
	}
	gesehen := map[string]bool{}
	for _, k := range kandidaten {
		u, ok := waehltHeute(k)
		if !ok {
			t.Fatalf("Kandidat %s wird nicht gewaehlt, kann also seinen Grund nicht zeigen", k.Bead.ID)
		}
		for _, g := range u.Gruende {
			gesehen[g] = true
		}
	}
	if len(gesehen) == 0 {
		t.Fatal("kein einziger Grund gesammelt — die Fixtures oben treffen keinen Zweig von waehltHeute")
	}
	leer := heuteLeerText()
	for g := range gesehen {
		if !strings.Contains(leer, g) {
			t.Errorf("Leer-Text nennt den Grund %q nicht: %q", g, leer)
		}
	}
}

// TestHeuteUeberfaelligUndInArbeitBehaeltDenRang ist die Gegenprobe, die der
// Reviewer von Hand gemacht hat, jetzt als Test: ein Bead, das gleichzeitig
// ueberfaellig UND in_progress ist, muss im UEBERFAELLIG-Eimer bleiben (Rang 0)
// und BEIDE Gruende tragen. Der if-Block fuer in_progress haengt hinter dem
// switch auf FristTage und ueberschreibt u.Rang nicht mehr — dieser Test waere
// bisher der einzige gewesen, der das belegt.
//
// Gegenprobe gemacht (gkp-9di): u.Rang im in_progress-Zweig testweise auf
// rangSonst gesetzt -> dieser Test wurde rot ("Rang = 2, want 0"), danach
// zurueckgedreht -> wieder gruen.
func TestHeuteUeberfaelligUndInArbeitBehaeltDenRang(t *testing.T) {
	maps := []ReportsMap{hKarte("m", hBead("ueberfaellig-und-in-arbeit", func(b *ReportsBead) {
		b.Status = istInArbeit
		b.DueAt = "2026-09-10T12:00:00Z"
	}))}
	a, _ := AnsichtBauen("heute", maps, jetztTest())
	if len(a.Eintraege) != 1 {
		t.Fatalf("Eintraege = %d, want 1", len(a.Eintraege))
	}
	e := a.Eintraege[0]
	if e.Rang != rangUeberfaellig {
		t.Errorf("Rang = %d, want %d (rangUeberfaellig) — der in_progress-Zweig darf ihn nicht verschieben",
			e.Rang, rangUeberfaellig)
	}
	for _, want := range []string{GrundUeberfaellig, GrundInArbeit} {
		if !enthaelt(e.Gruende, want) {
			t.Errorf("Gruende = %v, erwartet auch %q", e.Gruende, want)
		}
	}
}

// TestHeuteZeigtUeberfaelligesKindSamtGrundlosemElternteil ist der Kernfall
// von gkp-a1d (Spec §5): "eltern" traegt selbst keinen Heute-Grund (kein
// Termin, nicht in Arbeit) und waere vor diesem Ticket spurlos verschwunden —
// "kind" waere auf Ebene 0 aufgestiegen und der Zusammenhang waere verloren.
// Jetzt steht "eltern" als Zwischenueberschrift da, mit der Frist des Kindes
// UND einem Herkunftsvermerk, der sagt, wessen Frist das ist.
func TestHeuteZeigtUeberfaelligesKindSamtGrundlosemElternteil(t *testing.T) {
	maps := []ReportsMap{hKarte("m",
		hBead("eltern", func(b *ReportsBead) {
			b.Children = []ReportsBead{
				hBead("kind", func(k *ReportsBead) { k.DueAt = "2026-09-13T09:00:00Z" }),
			}
		}),
	)}
	a, _ := AnsichtBauen("heute", maps, jetztTest())
	gleichFolge(t, "Zeilenfolge", zeilenIDs(a), []string{"eltern", "kind"})

	zeilen := map[string]AnsichtEintrag{}
	for _, e := range a.Eintraege {
		zeilen[e.Bead] = e
	}

	kind := zeilen["kind"]
	if kind.Ueberschrift {
		t.Error("kind traegt Ueberschrift=true — es hat einen eigenen Grund und ist eine echte Aufgabenzeile")
	}
	if kind.FristHerkunft != "" {
		t.Errorf("kind.FristHerkunft = %q, want leer — die Frist ist seine eigene", kind.FristHerkunft)
	}

	eltern := zeilen["eltern"]
	if !eltern.Ueberschrift {
		t.Fatal("eltern traegt Ueberschrift=false, hat aber keinen eigenen Heute-Grund — sonst wuerden im " +
			"Browser Aktionsknoepfe an einer Zeile ohne echten Aufnahmegrund stehen")
	}
	if eltern.Rang != rangUeberfaellig {
		t.Errorf("Rang(eltern) = %d, want %d (rangUeberfaellig, geerbt von kind)", eltern.Rang, rangUeberfaellig)
	}
	if !enthaelt(eltern.Gruende, GrundUeberfaellig) {
		t.Errorf("Gruende(eltern) = %v, erwartet den geerbten Grund %q", eltern.Gruende, GrundUeberfaellig)
	}
	if eltern.FristHerkunft != "kind" {
		t.Errorf("FristHerkunft(eltern) = %q, want %q — ohne den Vermerk waere die Frist eine "+
			"erfundene Angabe (sie steht nicht an diesem Bead)", eltern.FristHerkunft, "kind")
	}
	if eltern.FristTage == nil || *eltern.FristTage != -1 {
		t.Errorf("FristTage(eltern) = %v, want -1 (1 Tag ueberfaellig, wie kind)", eltern.FristTage)
	}
	if eltern.Frist != kind.Frist {
		t.Errorf("Frist(eltern) = %q, want dieselbe wie kind: %q", eltern.Frist, kind.Frist)
	}

	// Die Kopfbilanz zaehlt die Zwischenueberschrift NICHT mit: nur kind hat
	// wirklich einen Aufnahmegrund, eltern hat ihn sich nur geliehen. Zwei
	// Zeilen mit demselben Grund duerften sonst wie zwei ueberfaellige
	// Aufgaben aussehen, wo es nur eine gibt.
	if got := zaehlerWert(a, GrundUeberfaellig); got != 1 {
		t.Errorf("Zaehler[%q] = %d, want 1 — die Zwischenueberschrift darf nicht mitzaehlen", GrundUeberfaellig, got)
	}
}

// TestHeuteElternMitEigenerDringlichererFristBehaeltEigeneAngabe ist die eine
// Haelfte des Randfalls aus Spec §5, in der Vererbung ohnehin nicht greifen
// wuerde: eltern ist selbst laenger ueberfaellig als kind (heute faellig).
func TestHeuteElternMitEigenerDringlichererFristBehaeltEigeneAngabe(t *testing.T) {
	maps := []ReportsMap{hKarte("m",
		hBead("eltern", func(b *ReportsBead) {
			b.DueAt = "2026-09-09T09:00:00Z" // laenger ueberfaellig als das Kind
			b.Children = []ReportsBead{
				hBead("kind", func(k *ReportsBead) { k.DueAt = "2026-09-14T09:00:00Z" }), // heute faellig
			}
		}),
	)}
	a, _ := AnsichtBauen("heute", maps, jetztTest())

	var eltern AnsichtEintrag
	for _, e := range a.Eintraege {
		if e.Bead == "eltern" {
			eltern = e
		}
	}
	if eltern.Ueberschrift {
		t.Error("eltern traegt Ueberschrift=true — es hat einen eigenen, sogar dringenderen Grund")
	}
	if eltern.FristHerkunft != "" {
		t.Errorf("FristHerkunft(eltern) = %q, want leer — die Frist ist seine eigene", eltern.FristHerkunft)
	}
	if !enthaelt(eltern.Gruende, GrundUeberfaellig) {
		t.Errorf("Gruende(eltern) = %v, erwartet die EIGENE Frist (%q)", eltern.Gruende, GrundUeberfaellig)
	}
	if eltern.FristTage == nil || *eltern.FristTage != -5 {
		t.Errorf("FristTage(eltern) = %v, want -5 (die eigene, nicht die des Kindes)", eltern.FristTage)
	}
}

// TestHeuteElternMitEigenemGrundLeihtSichNichtsVomDringlicherenKind ist die
// ANDERE Haelfte desselben Randfalls, und die einzig interessante: eltern hat
// einen eigenen, aber SCHWAECHEREN Grund (in Arbeit) als das Kind (ueberfaellig).
// Entschieden (Kommentar an sammle, Spec §5): eine Aufgabenzeile MIT eigenem
// Grund erbt NIE — sie behaelt ihre eigene Angabe, auch wenn ein Kind
// dringender ist. Nur eine Zwischenueberschrift OHNE eigenen Grund leiht sich
// eine (vorheriger Test).
func TestHeuteElternMitEigenemGrundLeihtSichNichtsVomDringlicherenKind(t *testing.T) {
	maps := []ReportsMap{hKarte("m",
		hBead("eltern", func(b *ReportsBead) {
			b.Status = istInArbeit
			b.Children = []ReportsBead{
				hBead("kind", func(k *ReportsBead) { k.DueAt = "2026-09-10T09:00:00Z" }), // deutlich ueberfaellig
			}
		}),
	)}
	a, _ := AnsichtBauen("heute", maps, jetztTest())

	var eltern AnsichtEintrag
	for _, e := range a.Eintraege {
		if e.Bead == "eltern" {
			eltern = e
		}
	}
	if eltern.Ueberschrift {
		t.Error("eltern traegt Ueberschrift=true — es hat einen eigenen Grund (in Arbeit)")
	}
	if eltern.Rang != rangSonst {
		t.Errorf("Rang(eltern) = %d, want %d (rangSonst, die EIGENE Einordnung) — ein dringenderes Kind "+
			"darf eine echte Aufgabenzeile nicht verschieben", eltern.Rang, rangSonst)
	}
	if !enthaelt(eltern.Gruende, GrundInArbeit) || enthaelt(eltern.Gruende, GrundUeberfaellig) {
		t.Errorf("Gruende(eltern) = %v, want nur den eigenen Grund %q, nicht den des Kindes",
			eltern.Gruende, GrundInArbeit)
	}
	if eltern.FristHerkunft != "" {
		t.Errorf("FristHerkunft(eltern) = %q, want leer", eltern.FristHerkunft)
	}
}

// TestHeuteFamilieMitUeberfaelligemKindStehtVorStandaloneHeute ist der
// Reviewer-Befund zu gkp-a1d, nachgebaut: sortiereGruppen ordnete Geschwister-
// GRUPPEN nach dem Rang des ELTERNTEILS, und flachAnhaengen haengt danach die
// ganze Unterstruktur direkt hinter dessen Zeile. "eltern-in-arbeit" traegt
// selbst nur rangSonst (eigener Grund "in Arbeit"), sein Kind
// "kind-ueberfaellig" ist rangUeberfaellig (0) — die dringendste Aufgabe im
// ganzen Baum stand deshalb HINTER einer bloss heute faelligen Standalone-
// Zeile, obwohl ihr direkter Vorfahre eine eigene Zeile in der Liste hat.
//
// Erwartet: die FAMILIE (eltern-in-arbeit + kind-ueberfaellig) steht VORN, weil
// der dringendste Rang IRGENDWO in ihrem Ast (0, bei kind-ueberfaellig) ueber
// die Position der ganzen Gruppe entscheidet — die Elternzeile behaelt aber
// ihre EIGENE Anzeige ("in Arbeit", rangSonst): die Nicht-Vererbungs-Regel
// (Kommentar an sammle, TestHeuteElternMitEigenemGrundLeihtSichNichtsVom-
// DringlicherenKind) gilt fuer die ANZEIGE unveraendert, nur die SORTIERUNG
// schaut in den Ast.
func TestHeuteFamilieMitUeberfaelligemKindStehtVorStandaloneHeute(t *testing.T) {
	maps := []ReportsMap{hKarte("m",
		hBead("eltern-in-arbeit", func(b *ReportsBead) {
			b.Status = istInArbeit
			b.Children = []ReportsBead{
				hBead("kind-ueberfaellig", func(k *ReportsBead) { k.DueAt = "2026-09-13T09:00:00Z" }), // gestern
			}
		}),
		hBead("stand-alone-heute", func(b *ReportsBead) { b.DueAt = "2026-09-14T12:00:00Z" }), // heute
	)}
	a, _ := AnsichtBauen("heute", maps, jetztTest())
	gleichFolge(t, "Zeilenfolge", zeilenIDs(a),
		[]string{"eltern-in-arbeit", "kind-ueberfaellig", "stand-alone-heute"})

	var eltern AnsichtEintrag
	for _, e := range a.Eintraege {
		if e.Bead == "eltern-in-arbeit" {
			eltern = e
		}
	}
	if eltern.Rang != rangSonst {
		t.Errorf("Rang(eltern-in-arbeit) = %d, want %d (rangSonst) — die Elternzeile behaelt "+
			"ihre EIGENE Einordnung, nur die Position der Familie darf sich am Kind orientieren",
			eltern.Rang, rangSonst)
	}
	if !enthaelt(eltern.Gruende, GrundInArbeit) || enthaelt(eltern.Gruende, GrundUeberfaellig) {
		t.Errorf("Gruende(eltern-in-arbeit) = %v, want nur %q, nicht den Grund des Kindes",
			eltern.Gruende, GrundInArbeit)
	}
}

// TestHeuteUeberschriftOhneVerbleibendeKinderVerschwindet ist der zweite
// Randfall aus Spec §5: ein Elternteil ohne eigenen Grund, dessen Kind
// EBENFALLS keinen hat, wird nicht zur leeren Zwischenueberschrift — es
// verschwindet ganz, so wie vor diesem Ticket auch.
func TestHeuteUeberschriftOhneVerbleibendeKinderVerschwindet(t *testing.T) {
	maps := []ReportsMap{hKarte("m",
		hBead("eltern-ohne-grund", func(b *ReportsBead) {
			b.Children = []ReportsBead{hBead("kind-ohne-grund")}
		}),
	)}
	a, _ := AnsichtBauen("heute", maps, jetztTest())
	if len(a.Eintraege) != 0 {
		t.Errorf("Eintraege = %v, want keine — eine Ueberschrift ohne verbleibende Kinder ist "+
			"Struktur ohne Inhalt", zeilenIDs(a))
	}
}
