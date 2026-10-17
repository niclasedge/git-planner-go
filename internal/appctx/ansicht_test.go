package appctx

import (
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/niclasedge/git-planner-go/internal/reports"
)

// Die Tests am RAHMEN der Aufgaben-Ansichten (gkp-3oh). Die Auswahl je Ansicht
// steht in heute_test.go und seinen fuenf Geschwistern; hier steht, was die
// Regeltabelle selbst zusagt — Reihenfolge, Leersatz, Vordeutung, Sortierung.

// navFolge ist die Folge der Ansichtsnamen, wie die Navigation sie zeigt.
// Gelesen wird der ECHTE Erzeuger (ansichtenNav); builddata.go haengt hinter
// diese Liste nur noch die Bereiche-Karte. Eine danebengelegte Nachbildung
// waere genau der Test, der nach dem Umbau gruen bleibt.
func navFolge() []string {
	var out []string
	for _, it := range ansichtenNav(map[string]any{}) {
		out = append(out, aStr(aMap(it)["id"]))
	}
	return out
}

// navFolgeAusPayload liest dieselbe Folge aus dem FERTIGEN Rumpf, den der
// Browser bekommt — eine Ebene hoeher als navFolge, quer durch AnsichtenAntwort.
//
// Bis gkp-dnz lag zwischen ansichtenNav und dem Rumpf noch die Gruppe
// "Aufgaben" in builddata.go (#mp-nav, links), und der Umweg pruefte GENAU
// diese zweite Stelle. Die Gruppe ist seither weg: die sechs Ansichten stehen
// nur noch im Kopf (.viewnav), gespeist aus GET /ansichten — und AnsichtenAntwort
// ist der Rumpf DIESER Route, nicht bloss eine Nachbildung von ansichtenNav
// (Spec §9.1, Fall 3: eine Nachbildung waere gruen geblieben, waere die echte
// Route je auseinandergelaufen).
func navFolgeAusPayload(t *testing.T) []string {
	t.Helper()
	antwort := AnsichtenAntwort(map[string]AufgabenAnsicht{}, "")
	var out []string
	for _, it := range aArr(antwort["nav"]) {
		out = append(out, aStr(aMap(it)["id"]))
	}
	if len(out) == 0 {
		t.Fatal("im Rumpf steht keine einzige Aufgaben-Ansicht — dann misst dieser Test nichts")
	}
	return out
}

// TestNavigationsfolgeStehtInDerRegelNichtImDateinamen ist Befund B aus
// gkp-3oh, gemessen: vorher war die Folge die der init-Funktionen, also die der
// DATEINAMEN — demnaechst, eingang, heute, irgendwann, jederzeit, logbuch, mit
// Heute an dritter Stelle. Die Leserfolge (Heute zuerst) war damit nur zu
// erreichen, indem man heute.go umbenennt: eine Anzeigeentscheidung, die an
// einem Dateinamen haengt.
//
// Jetzt sagt Regel.Ordnung sie an. Das ist eine ZAHL IN DER REGEL und kein
// switch in der Navigation — TestSelectKenntKeinenEinzelnenAnsichtsnamen und
// TestNavigationKommtAusDerRegeltabelle halten die Naht generisch.
func TestNavigationsfolgeStehtInDerRegelNichtImDateinamen(t *testing.T) {
	want := []string{HeuteID, DemnaechstID, JederzeitID, IrgendwannID, EingangID, LogbuchID}
	got := navFolge()
	gleichFolge(t, "Navigationsfolge", got, want)

	// Und dasselbe am Rumpf, den der Browser wirklich bekommt: dass ein Feld
	// GELESEN wird, ist nicht dasselbe wie dass die Folge ankommt.
	gleichFolge(t, "Navigationsfolge im Rumpf", navFolgeAusPayload(t), want)

	// Die Gegenprobe steht IM Test, weil sie sonst niemand wiederholt: die
	// erwartete Folge darf nicht zufaellig die alphabetische sein. Waere sie es,
	// haette die Dateinamenfolge sie ebenso hergestellt und der Test oben
	// spraeche ueber nichts (Spec §9.1, Ursache "etwas anderes stellt das
	// Ergebnis her").
	alpha := append([]string(nil), got...)
	sort.Strings(alpha)
	if strings.Join(alpha, ",") == strings.Join(got, ",") {
		t.Errorf("die erwartete Folge %v ist die alphabetische — dieser Test haelt dann "+
			"nichts fest, weil die Reihenfolge der Dateinamen sie auch ergibt", got)
	}
}

// TestJedeRegelHatEineEigeneOrdnung: gleiche Ordnung heisst gleicher Rang, und
// dann entscheidet wieder die Anmeldefolge — also der Dateiname. Zwei Ansichten
// mit derselben Zahl waeren deshalb kein kleiner Schoenheitsfehler, sondern der
// stillschweigende Rueckfall in genau den Zustand, den Befund B beschreibt.
func TestJedeRegelHatEineEigeneOrdnung(t *testing.T) {
	wer := map[int]string{}
	for _, r := range Regeln() {
		if vorher, da := wer[r.Ordnung]; da {
			t.Errorf("%s und %s tragen beide Ordnung %d — dann entscheidet wieder die "+
				"Anmeldefolge, also der Dateiname", vorher, r.ID, r.Ordnung)
		}
		wer[r.Ordnung] = r.ID
	}
}

// TestJedeAnsichtNenntIhrenEigenenLeersatz ist Befund D aus gkp-3oh: der Satz
// "Nichts offen — die Liste ist leer, nicht kaputt." stand GENERISCH im Browser
// und war damit im Logbuch falsch — dort ist nichts GESCHLOSSEN, nicht nichts
// offen.
//
// Der Leersatz gehoert deshalb zur Regel, so wie der Hinweis auch. Verlangt
// wird nicht nur, dass er da ist, sondern dass er SICH UNTERSCHEIDET: sechs
// gleiche Saetze waeren der generische Satz mit mehr Schritten.
func TestJedeAnsichtNenntIhrenEigenenLeersatz(t *testing.T) {
	wer := map[string]string{}
	for _, r := range Regeln() {
		if strings.TrimSpace(r.Leer) == "" {
			t.Errorf("%s nennt keinen Leersatz — eine leere Liste sagt dann nichts darueber, "+
				"was ihre Leere bedeutet", r.ID)
			continue
		}
		if vorher, da := wer[r.Leer]; da {
			t.Errorf("%s und %s tragen denselben Leersatz %q", vorher, r.ID, r.Leer)
		}
		wer[r.Leer] = r.ID

		// Und er muss im Rumpf ankommen: in der Regel stehen und nicht
		// mitgeschickt werden waere dieselbe Luecke eine Ebene tiefer.
		a, ok := AnsichtBauen(r.ID, nil, jetztTest())
		if !ok {
			t.Fatalf("AnsichtBauen(%q) kennt die Ansicht nicht", r.ID)
		}
		if a.Leer != r.Leer {
			t.Errorf("%s: Rumpf traegt Leer=%q, die Regel sagt %q", r.ID, a.Leer, r.Leer)
		}
	}
}

// TestLogbuchLeersatzSprichtNichtVonOffenem ist der Fall, an dem Befund D
// aufgefallen ist. Der generische Satz behauptete im Logbuch das Gegenteil
// dessen, was gemeint war.
func TestLogbuchLeersatzSprichtNichtVonOffenem(t *testing.T) {
	a, _ := AnsichtBauen(LogbuchID, nil, jetztTest())
	if strings.Contains(strings.ToLower(a.Leer), "offen") {
		t.Errorf("Leersatz des Logbuchs = %q — im Logbuch ist nichts GESCHLOSSEN, "+
			"nicht nichts offen", a.Leer)
	}
	if !strings.Contains(strings.ToLower(a.Leer), "geschlossen") &&
		!strings.Contains(strings.ToLower(a.Leer), "abgeschlossen") {
		t.Errorf("Leersatz des Logbuchs = %q — er sagt nicht, dass nichts abgeschlossen ist", a.Leer)
	}
}

// TestKandidatDeutetDenAbschlussVor ist Befund C aus gkp-3oh: closed_at war im
// Kandidat nicht vorgedeutet, also parste logbuch.go selbst — genau die zweite
// Gelegenheit, die beiden Zeitbasen zu vertauschen, vor der der Kommentarkopf
// von Kandidat warnt.
//
// Der Unterschied ist messbar und in diesem Test das Ganze: closed_at ist
// ECHTES UTC und geht durch bdops.ParseEventStamp. Wer es wie due_at behandelt
// (lokale Wanduhr, ParseUserDateIn), bekommt aus "23:00Z" in Zone +02 den
// Zeitpunkt 21:00 UTC — zwei Stunden daneben, und in diesem Fall ein anderer
// Kalendertag.
func TestKandidatDeutetDenAbschlussVor(t *testing.T) {
	jetzt := jetztTest()
	heute := tagesAnfang(jetzt)

	k := kandidatAus(hBead("fertig", func(b *ReportsBead) {
		b.Status = "closed"
		b.ClosedAt = "2026-09-13T23:00:00Z"
	}), "m", HerkunftBeads, "p", heute, jetzt)
	if k.Abschluss == nil {
		t.Fatal("Kandidat.Abschluss ist nil — closed_at wird nicht vorgedeutet")
	}
	want := time.Date(2026, 9, 13, 23, 0, 0, 0, time.UTC)
	if !k.Abschluss.Equal(want) {
		t.Errorf("Abschluss = %v, want %v — closed_at ist echtes UTC (ParseEventStamp) "+
			"und keine Wanduhr (ParseUserDateIn)", k.Abschluss.UTC(), want)
	}

	// Ohne Stempel nil und nicht die Nullzeit: sonst waere "nie geschlossen"
	// von "am 01.01.0001 geschlossen" nicht zu unterscheiden.
	if ohne := kandidatAus(hBead("offen"), "m", HerkunftBeads, "p", heute, jetzt); ohne.Abschluss != nil {
		t.Errorf("Abschluss ohne closed_at = %v, want nil", ohne.Abschluss)
	}
}

// TestAbsteigendDrehtNurDieTermineNichtDieFehlenden ist Befund A aus gkp-3oh,
// samt seiner Grenze.
//
// Gedreht wird das Vorzeichen des TERMINVERGLEICHS. Nicht gedreht wird, wo ein
// FEHLENDER Termin landet: "kein Termin sortiert hinter jeden vorhandenen" ist
// eine Aussage ueber Vollstaendigkeit und nicht ueber Richtung. Ein naives
// `return !kleiner` haette genau das mitgedreht und die Terminlosen nach vorn
// geholt — das ist die Falle dieses Felds.
//
// Die Fixture-Namen laufen GEGEN die Erwartung: der letzte Vergleich in
// sortiereGruppen ist die Id, aufsteigend. Mit "a-…" fuer den spaetesten Termin
// haette allein sie die erwartete Folge hergestellt (Spec §9.1).
func TestAbsteigendDrehtNurDieTermineNichtDieFehlenden(t *testing.T) {
	frueh := time.Date(2026, 9, 14, 5, 0, 0, 0, testZone)
	spaet := time.Date(2026, 9, 14, 7, 0, 0, 0, testZone)

	bauen := func() []gruppe {
		mach := func(id string, t *time.Time) gruppe {
			// ast und nicht bloss urteil: sortiereGruppen liest seit gkp-a1d den
			// dringlichsten Rang IM AST (gruppe.ast), nicht mehr die Anzeige der
			// einzelnen Zeile. Ohne Nachfahren sind beide Werte gleich — hier
			// wird trotzdem ast gesetzt, weil DAS der Schluessel ist, den dieser
			// Test wirklich pruefen will.
			u := Urteil{Zeitpunkt: t}
			return gruppe{
				eintrag: AnsichtEintrag{Bead: id},
				kand:    Kandidat{Bead: hBead(id)},
				urteil:  u,
				ast:     u,
			}
		}
		return []gruppe{
			mach("a-ohne-termin", nil),
			mach("m-frueh", &frueh),
			mach("z-spaet", &spaet),
		}
	}

	folge := func(gs []gruppe) []string {
		out := make([]string, 0, len(gs))
		for _, g := range gs {
			out = append(out, g.eintrag.Bead)
		}
		return out
	}

	auf := bauen()
	sortiereGruppen(auf, false)
	gleichFolge(t, "aufsteigend", folge(auf),
		[]string{"m-frueh", "z-spaet", "a-ohne-termin"})

	ab := bauen()
	sortiereGruppen(ab, true)
	gleichFolge(t, "absteigend", folge(ab),
		[]string{"z-spaet", "m-frueh", "a-ohne-termin"})
}

// gGrenzeRegel ist eine Wegwerf-Ansicht fuer die Rahmen-Haelfte von gkp-dop.
// Sie waehlt JEDE Aufgabe und nimmt die Prioritaet als Rang — damit haengt der
// Eimer an einem Feld, das die Fixture frei setzt, und der Test misst die
// Kappung statt einer Auswahlregel.
//
// Eine eigene Regel und nicht die des Logbuchs: der Rahmen kennt keinen
// Abschlusstag, er kennt Raenge. Was ein Rang BEDEUTET, entscheidet die Regel,
// und ein Rahmentest, der das Logbuch nimmt, misst beide zugleich.
func gGrenzeRegel(grenze int) Regel {
	return Regel{
		ID: "wegwerf", Titel: "Wegwerf", Grenze: grenze,
		GrenzeSatz: func(gezeigt, gesamt, gruppen int) string {
			return fmt.Sprintf("gezeigt=%d gesamt=%d gruppen=%d", gezeigt, gesamt, gruppen)
		},
		Waehlt: func(k Kandidat) (Urteil, bool) {
			return Urteil{Rang: k.Bead.Priority, Gruende: []string{"da"}}, true
		},
	}
}

// gRang ist eine Aufgabe in einem bestimmten Eimer.
func gRang(id string, rang int) ReportsBead {
	return hBead(id, func(b *ReportsBead) { b.Priority = rang })
}

// TestJedeZeileTraegtDasProjektIhrerKarte ist die Grundzusage von gkp-k1o:
// eine Zeile traegt den Projektnamen der Karte, unter der sie steht — auch
// eine Unteraufgabe, die durch sammle (Rekursion) und flachAnhaengen laeuft,
// und auch dann, wenn mehrere Karten aus verschiedenen Projekten nebeneinander
// stehen.
//
// NUR die Weiterleitung: m1.Projekt/m2.Projekt stehen von Hand auf der
// Fixture, geprueft wird nur, dass ansichtAus sie an jede Zeile darunter
// durchreicht. Wer den Stempel selbst setzt (teilbaum.go StandNachMit,
// baueKarten in reports_view.go), faehrt TestStandNachMitStempelt-
// GithubProjektAuchOhneBeads (teilbaum_test.go) an (Spec §9.1 Muster 3).
func TestJedeZeileTraegtDasProjektIhrerKarte(t *testing.T) {
	eltern := hBead("eltern")
	eltern.Children = []ReportsBead{hBead("kind")}
	m1 := hKarte("m1", eltern)
	m1.Projekt = "repo-a"
	m2 := hKarte("m2", hBead("solo"))
	m2.Projekt = "repo-b"

	a := ansichtAus(alleRegel(), beadsUndGithub([]ReportsMap{m1, m2}, nil), jetztTest())
	projekt := map[string]string{}
	for _, e := range a.Eintraege {
		projekt[e.Bead] = e.Projekt
	}
	if projekt["eltern"] != "repo-a" || projekt["kind"] != "repo-a" {
		t.Errorf("m1 (Eltern+Kind): Projekt = %+v, want beide %q", projekt, "repo-a")
	}
	if projekt["solo"] != "repo-b" {
		t.Errorf("solo: Projekt = %q, want %q", projekt["solo"], "repo-b")
	}
}

// TestZeileTraegtDenEagerBelegZaehlstand ist die Go-Haelfte von gkp-dnz,
// Stufe 2: die Zeile bekommt vorab einen Zaehlstand nach Kind ("n plan · m
// done"), OHNE die Eintraege selbst mitzuschicken — die kommen erst lazy ueber
// GET /belege, wenn der Browser den Twisty tatsaechlich aufklappt.
func TestZeileTraegtDenEagerBelegZaehlstand(t *testing.T) {
	entry := func(kind, id string) ReportsEntry {
		return ReportsEntry{Entry: reports.Entry{ID: id, Bead: "mit-belegen", Kind: kind, At: "2026-09-01T00:00:00Z"}}
	}
	mitBelegen := hBead("mit-belegen", func(b *ReportsBead) {
		b.Ready = true
		b.Entries = []ReportsEntry{entry("plan", "e1"), entry("plan", "e2"), entry("done", "e3")}
	})
	ohneBelege := hBead("ohne-belege", func(b *ReportsBead) { b.Ready = true })
	m := hKarte("m", mitBelegen, ohneBelege)

	a := ansichtAus(alleRegel(), beadsUndGithub([]ReportsMap{m}, nil), jetztTest())
	nach := map[string]AnsichtEintrag{}
	for _, e := range a.Eintraege {
		nach[e.Bead] = e
	}

	if got := nach["mit-belegen"]; got.BelegPlan != 2 || got.BelegDone != 1 {
		t.Errorf("mit-belegen: BelegPlan=%d BelegDone=%d, want 2/1", got.BelegPlan, got.BelegDone)
	}
	// Die Eintraege selbst reisen NICHT mit — nur der Zaehlstand. AnsichtEintrag
	// hat dafuer gar kein Feld; die Gegenprobe waere also ein spaeteres Feld,
	// das versehentlich die Entries mitschickt. Hier genuegt zu zeigen, dass
	// das Blatt ohne Eintraege beim Zaehlstand bei null bleibt und nicht etwa
	// von "mit-belegen" mitzaehlt.
	if got := nach["ohne-belege"]; got.BelegPlan != 0 || got.BelegDone != 0 {
		t.Errorf("ohne-belege: BelegPlan=%d BelegDone=%d, want 0/0 — Zaehlstand ist PRO Bead, "+
			"nicht ueber die Karte gemischt", got.BelegPlan, got.BelegDone)
	}
}

// TestGrenzeKapptAnDerGruppengrenze ist der Kern von gkp-dop.
//
// Gekappt wird NICHT nach der n-ten Zeile, sondern nach dem letzten Eimer, der
// noch ganz hineinpasst. Der Grund steht an Regel.Grenze: das Logbuch ist seit
// gkp-3oh tageweise gruppiert, und ein Tag, von dem drei von elf Zeilen zu
// sehen sind, behauptet, an diesem Tag seien drei Aufgaben fertig geworden.
// Ein Tag, der GANZ fehlt, behauptet nichts.
//
// Die Fixture-Namen laufen gegen die Erwartung (Spec §9.1): die Zeilen des
// juengsten Eimers heissen "z…", die des aeltesten "a…". Kappte die
// Implementierung nach Id oder liesse sie die Liste bloss vorn abschneiden,
// stuenden die a-Zeilen im Ergebnis.
func TestGrenzeKapptAnDerGruppengrenze(t *testing.T) {
	maps := []ReportsMap{hKarte("m",
		gRang("z1", 0), gRang("z2", 0),
		gRang("m1", 1), gRang("m2", 1),
		gRang("a1", 2), gRang("a2", 2), gRang("a3", 2),
	)}

	// Gegenprobe zur Fixture: ohne Grenze sind es sieben. Waere die Liste schon
	// vorher kuerzer als die Grenze, koennte der Test unten nichts sehen.
	ohne := ansichtAus(gGrenzeRegel(0), beadsUndGithub(maps, nil), jetztTest())
	if len(ohne.Eintraege) != 7 {
		t.Fatalf("Fixture taugt nicht: ohne Grenze %d Zeilen, erwartet 7", len(ohne.Eintraege))
	}
	if ohne.Gekappt != "" {
		t.Errorf("ohne Grenze steht ein Kappungssatz da: %q", ohne.Gekappt)
	}

	// Grenze 5: Eimer 0 (2) und 1 (2) passen, Eimer 2 (3) wuerde auf 7 fuehren.
	// Es bleiben VIER Zeilen — nicht fuenf. Genau darin unterscheidet sich die
	// Kappung an der Gruppengrenze von einem Schnitt nach der fuenften Zeile.
	a := ansichtAus(gGrenzeRegel(5), beadsUndGithub(maps, nil), jetztTest())
	gleichFolge(t, "gekappt", zeilenIDs(a), []string{"z1", "z2", "m1", "m2"})
	if a.Gekappt != "gezeigt=4 gesamt=7 gruppen=2" {
		t.Errorf("Gekappt = %q, erwartet gezeigt=4 gesamt=7 gruppen=2", a.Gekappt)
	}
	// Und die Bilanz zaehlt, was dasteht — sonst saehe der Kopf eine Liste, die
	// die Ansicht gar nicht zeigt.
	if len(a.Zaehler) != 1 || a.Zaehler[0].Anzahl != 4 {
		t.Errorf("Zaehler = %+v, erwartet einen Eimer mit 4", a.Zaehler)
	}
}

// TestGrenzeLaesstDenJuengstenEimerGanzDurch: passt schon der erste Eimer nicht
// in die Grenze, wird er trotzdem ganz gezeigt.
//
// Die Alternative waere eine LEERE Liste — und die ist die schlechteste aller
// Antworten: sie sagt "nichts abgeschlossen", wo in Wahrheit an einem einzigen
// Tag mehr fertig wurde, als die Grenze erlaubt. Eine Grenze ist ein Budget,
// keine Zusage.
func TestGrenzeLaesstDenJuengstenEimerGanzDurch(t *testing.T) {
	maps := []ReportsMap{hKarte("m",
		gRang("z1", 0), gRang("z2", 0), gRang("z3", 0),
		gRang("a1", 1),
	)}
	a := ansichtAus(gGrenzeRegel(2), beadsUndGithub(maps, nil), jetztTest())
	gleichFolge(t, "erster Eimer ganz", zeilenIDs(a), []string{"z1", "z2", "z3"})
	if a.Gekappt != "gezeigt=3 gesamt=4 gruppen=1" {
		t.Errorf("Gekappt = %q, erwartet gezeigt=3 gesamt=4 gruppen=1", a.Gekappt)
	}
}

// TestGrenzeHebtZeileOhneGezeigtesElternteilAufEbeneNull.
//
// Die Kappung darf die Einrueckung nicht verwaisen lassen. AnsichtEintrag.Tiefe
// ist die Tiefe IN DIESER LISTE — steht die Aufgabe nicht drin, ruecken ihre
// Nachfahren auf, und genau das tut sammle bereits fuer eine nicht gewaehlte
// Aufgabe. Die Kappung ist der zweite Weg, auf dem eine Zeile herausfaellt; sie
// muss dieselbe Zusage halten, sonst haengt eine eingerueckte Zeile unter
// nichts.
func TestGrenzeHebtZeileOhneGezeigtesElternteilAufEbeneNull(t *testing.T) {
	eltern := gRang("m-eltern", 2)
	eltern.Children = []ReportsBead{gRang("z-kind", 0)}
	maps := []ReportsMap{hKarte("m", gRang("z-fuellung", 0), eltern)}

	ohne := ansichtAus(gGrenzeRegel(0), beadsUndGithub(maps, nil), jetztTest())
	gleichFolge(t, "ohne Grenze", zeilenIDs(ohne),
		[]string{"z-fuellung", "m-eltern", "z-kind"})
	tiefen := map[string]int{}
	for _, e := range ohne.Eintraege {
		tiefen[e.Bead] = e.Tiefe
	}
	if tiefen["z-kind"] != 1 {
		t.Fatalf("Fixture taugt nicht: z-kind steht ohne Grenze auf Tiefe %d, erwartet 1 — "+
			"ohne Einrueckung kann die Kappung sie nicht verlieren", tiefen["z-kind"])
	}

	a := ansichtAus(gGrenzeRegel(2), beadsUndGithub(maps, nil), jetztTest())
	gleichFolge(t, "mit Grenze", zeilenIDs(a), []string{"z-fuellung", "z-kind"})
	for _, e := range a.Eintraege {
		if e.Bead == "z-kind" && e.Tiefe != 0 {
			t.Errorf("z-kind steht auf Tiefe %d, erwartet 0 — sein Elternteil ist "+
				"weggekappt, die Einrueckung zeigt auf eine Zeile, die es nicht gibt", e.Tiefe)
		}
	}
}

// TestGrenzeHaeltProjektUeberDieKappungHinweg ist die Gegenprobe zu gkp-k1o:
// AnsichtEintrag.Projekt muss dieselben zwei Wege ueberleben wie Tiefe — die
// Kappung baut die Liste per Struct-Kopie neu auf (kappen), und eine
// weggekappte Zeile darf ihr Kind nicht nur der Tiefe nach, sondern auch dem
// Projekt nach im Regen stehen lassen.
//
// NUR die Weiterleitung durch die Kappung — m.Projekt steht von Hand auf der
// Fixture, siehe TestJedeZeileTraegtDasProjektIhrerKarte.
func TestGrenzeHaeltProjektUeberDieKappungHinweg(t *testing.T) {
	m := hKarte("m", gRang("z1", 0), gRang("z2", 0), gRang("a1", 1))
	m.Projekt = "mein-repo"
	maps := []ReportsMap{m}

	a := ansichtAus(gGrenzeRegel(2), beadsUndGithub(maps, nil), jetztTest())
	gleichFolge(t, "gekappt", zeilenIDs(a), []string{"z1", "z2"})
	for _, e := range a.Eintraege {
		if e.Projekt != "mein-repo" {
			t.Errorf("%s: Projekt = %q nach der Kappung, want %q", e.Bead, e.Projekt, "mein-repo")
		}
	}
}

// TestGrenzeHebtZeileOhneGezeigtesElternteilAufEbeneNullTraegtProjekt haengt
// an TestGrenzeHebtZeileOhneGezeigtesElternteilAufEbeneNull: die Tiefen-
// Neuberechnung ueber den Stapel in kappen darf das Projekt einer Zeile nicht
// verlieren, deren Elternteil weggekappt wurde.
//
// NUR die Weiterleitung — m.Projekt steht von Hand auf der Fixture, siehe
// TestJedeZeileTraegtDasProjektIhrerKarte.
func TestGrenzeHebtZeileOhneGezeigtesElternteilAufEbeneNullTraegtProjekt(t *testing.T) {
	eltern := gRang("m-eltern", 2)
	eltern.Children = []ReportsBead{gRang("z-kind", 0)}
	m := hKarte("m", gRang("z-fuellung", 0), eltern)
	m.Projekt = "mein-repo"
	maps := []ReportsMap{m}

	a := ansichtAus(gGrenzeRegel(2), beadsUndGithub(maps, nil), jetztTest())
	gleichFolge(t, "mit Grenze", zeilenIDs(a), []string{"z-fuellung", "z-kind"})
	for _, e := range a.Eintraege {
		if e.Bead == "z-kind" {
			if e.Tiefe != 0 {
				t.Errorf("z-kind: Tiefe = %d, want 0", e.Tiefe)
			}
			if e.Projekt != "mein-repo" {
				t.Errorf("z-kind: Projekt = %q nach der Kappung, want %q", e.Projekt, "mein-repo")
			}
		}
	}
}

// TestGrenzeGreiftNurWennSieUeberschrittenIst: eine Liste, die unter der Grenze
// bleibt, traegt keinen Kappungssatz. Sonst stuende an jeder vollstaendigen
// Liste eine Warnung, und eine Warnung, die immer dasteht, sagt nichts.
func TestGrenzeGreiftNurWennSieUeberschrittenIst(t *testing.T) {
	maps := []ReportsMap{hKarte("m", gRang("z1", 0), gRang("a1", 1))}
	a := ansichtAus(gGrenzeRegel(2), beadsUndGithub(maps, nil), jetztTest())
	if len(a.Eintraege) != 2 {
		t.Errorf("%d Zeilen, erwartet 2", len(a.Eintraege))
	}
	if a.Gekappt != "" {
		t.Errorf("Gekappt = %q, erwartet leer — die Liste ist vollstaendig", a.Gekappt)
	}
}

// gUeberschriftGrenzeRegel waehlt nur Beads mit Ready=true (Rang = Priority) —
// NICHT ihr Elternteil. Das ist die Form, in der eine Zwischenueberschrift in
// dieser Karte tatsaechlich entsteht (dieselbe wie in Jederzeit: ein Epic ist
// selten selbst `bd ready`), und die einzige, an der sich pruefen laesst, ob
// die Kappung eine Ueberschrift OHNE verbleibende Kinder stehen laesst.
func gUeberschriftGrenzeRegel(grenze int) Regel {
	return Regel{
		ID: "wegwerf-ueberschrift", Titel: "Wegwerf", Grenze: grenze,
		GrenzeSatz: func(gezeigt, gesamt, gruppen int) string {
			return fmt.Sprintf("gezeigt=%d gesamt=%d gruppen=%d", gezeigt, gesamt, gruppen)
		},
		Waehlt: func(k Kandidat) (Urteil, bool) {
			if !k.Bead.Ready {
				return Urteil{}, false
			}
			return Urteil{Rang: k.Bead.Priority, Gruende: []string{"da"}}, true
		},
	}
}

// gBereit ist eine Aufgabe mit einem gesetzten Ready-Flag und einem Rang
// (hier: Priority, die gUeberschriftGrenzeRegel als Rang liest).
func gBereit(id string, ready bool, rang int) ReportsBead {
	return hBead(id, func(b *ReportsBead) { b.Ready = ready; b.Priority = rang })
}

// TestUeberschriftVerschwindetMitIhremGekapptenAnker ist der Randfall aus
// Spec §5 ("Ein Elternteil, dessen Kinder alle wegfallen — z.B. durch
// kappen"): "eltern-ohne-grund" hat keinen eigenen Grund und erbt den Rang
// seines einzigen Kindes (zwischenUeberschrift, sammle). Faellt dieses Kind
// der Kappung zum Opfer, faellt mit ihm der EINZIGE Grund, den die
// Ueberschrift je hatte — sie darf nicht als leere Zeile stehen bleiben.
//
// Das braucht keinen Sonderfall in kappen(): die Ueberschrift traegt
// DENSELBEN Rang wie ihr dringendstes Kind (per Konstruktion, zwischenUeber-
// schrift kopiert ihn), und kappen entscheidet pro RANG global — Ueberschrift
// und Anker stehen im selben Eimer und fallen zusammen.
func TestUeberschriftVerschwindetMitIhremGekapptenAnker(t *testing.T) {
	eltern := gBereit("eltern-ohne-grund", false, 0)
	eltern.Children = []ReportsBead{gBereit("kind-dringend", true, 0)}
	maps := []ReportsMap{hKarte("m",
		gBereit("filler", true, -1),
		eltern,
	)}

	// Gegenprobe zur Fixture: ohne Grenze stehen alle drei Zeilen da, darunter
	// die Ueberschrift — sonst koennte der Test unten nichts zeigen.
	ohne := ansichtAus(gUeberschriftGrenzeRegel(0), beadsUndGithub(maps, nil), jetztTest())
	gleichFolge(t, "ohne Grenze", zeilenIDs(ohne), []string{"filler", "eltern-ohne-grund", "kind-dringend"})
	for _, e := range ohne.Eintraege {
		if e.Bead == "eltern-ohne-grund" && !e.Ueberschrift {
			t.Fatalf("Fixture taugt nicht: eltern-ohne-grund ist keine Ueberschrift")
		}
	}

	// Grenze 1: der Eimer -1 (nur "filler") passt und geht immer ganz durch;
	// der Eimer 0 (Ueberschrift + Kind, beide geerbt auf denselben Rang) waere
	// der ZWEITE Eimer und passt nicht mehr ins Budget.
	a := ansichtAus(gUeberschriftGrenzeRegel(1), beadsUndGithub(maps, nil), jetztTest())
	gleichFolge(t, "gekappt", zeilenIDs(a), []string{"filler"})
	if a.Gekappt == "" {
		t.Error("Gekappt ist leer, obwohl die Grenze gegriffen hat")
	}
}
