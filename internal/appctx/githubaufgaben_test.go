package appctx

import (
	"strings"
	"testing"

	"github.com/niclasedge/git-planner-go/internal/collectors"
)

// ghIssue baut ein Issue mit den Feldern, auf die die Auswahl schaut.
func ghIssue(nr int, anpassen ...func(*collectors.GhIssue)) collectors.GhIssue {
	i := collectors.GhIssue{
		Number:    nr,
		Title:     "Issue " + itoa(nr),
		State:     "OPEN",
		UpdatedAt: "2026-09-01T00:00:00Z",
		URL:       "https://github.com/o/r/issues/" + itoa(nr),
	}
	for _, f := range anpassen {
		f(&i)
	}
	return i
}

// alleRegel waehlt jede offene Aufgabe. Sie ist NICHT angemeldet: eine
// Testregel in der globalen Tabelle stuende in jeder Navigation und in jeder
// anderen Ansicht dieses Pakets.
func alleRegel() Regel {
	return Regel{ID: "alle", Titel: "Alle", Waehlt: func(k Kandidat) (Urteil, bool) {
		if !k.Offen() {
			return Urteil{}, false
		}
		return Urteil{Rang: 0, Gruende: []string{"offen"}}, true
	}}
}

// TestGithubEintraegeTragenHerkunftGithub ist der Kern von gkp-pf2: eine
// GitHub-Aufgabe kommt mit ihrer eigenen Herkunft in den Strom, eine
// beads-Aufgabe behaelt ihre. Ohne das liest das Abzeichen aus gkp-7gb an
// jeder Zeile "beads", auch an denen, die keine sind.
func TestGithubEintraegeTragenHerkunftGithub(t *testing.T) {
	maps := []ReportsMap{hKarte("m", hBead("bd-1"))}
	gh := GithubKarten("o/r", []collectors.GhIssue{ghIssue(7)})

	a := ansichtAus(alleRegel(), beadsUndGithub(maps, gh), jetztTest())

	herkunft := map[string]string{}
	for _, e := range a.Eintraege {
		herkunft[e.Bead] = e.Herkunft
	}
	if herkunft["bd-1"] != HerkunftBeads {
		t.Errorf("Herkunft von bd-1 = %q, want %q", herkunft["bd-1"], HerkunftBeads)
	}
	if herkunft["#7"] != HerkunftGithub {
		t.Errorf("Herkunft von #7 = %q, want %q", herkunft["#7"], HerkunftGithub)
	}
}

// TestGithubEintragTraegtSeinenInhalt: die Regel aus ansicht.go — "Eintraege
// nennen die Aufgabe mit ihrer ID und NICHT als eingebettete ReportsBead" —
// steht auf einer Voraussetzung, die fuer GitHub NICHT gilt: die Aufgaben
// stehen eben nicht in ReportsView.Maps. Ohne Titel im Eintrag zeichnet
// browser.js "Zeile nicht im geladenen Baum". Der Inhalt reist deshalb genau
// dort mit, wo der Baum ihn nicht liefern kann — und nur dort.
func TestGithubEintragTraegtSeinenInhalt(t *testing.T) {
	maps := []ReportsMap{hKarte("m", hBead("bd-1"))}
	gh := GithubKarten("o/r", []collectors.GhIssue{
		ghIssue(7, func(i *collectors.GhIssue) { i.Title = "Docker-Compose aufräumen" }),
	})

	a := ansichtAus(alleRegel(), beadsUndGithub(maps, gh), jetztTest())

	var g, b AnsichtEintrag
	for _, e := range a.Eintraege {
		if e.Bead == "#7" {
			g = e
		}
		if e.Bead == "bd-1" {
			b = e
		}
	}
	if g.Titel != "Docker-Compose aufräumen" {
		t.Errorf("Titel des GitHub-Eintrags = %q, want %q", g.Titel, "Docker-Compose aufräumen")
	}
	if g.Status != "open" {
		t.Errorf("Status des GitHub-Eintrags = %q, want %q", g.Status, "open")
	}
	if g.URL != "https://github.com/o/r/issues/7" {
		t.Errorf("URL des GitHub-Eintrags = %q, want die Issue-URL", g.URL)
	}
	if g.Karte != "o/r" {
		t.Errorf("Karte des GitHub-Eintrags = %q, want %q", g.Karte, "o/r")
	}
	// Und die andere Haelfte: eine beads-Zeile bleibt schmal, der Baum hat sie.
	if b.Titel != "" || b.Status != "" || b.URL != "" {
		t.Errorf("beads-Eintrag traegt Inhalt doppelt: titel=%q status=%q url=%q", b.Titel, b.Status, b.URL)
	}
}

// TestGithubEintragTraegtDasProjektDerKarte ist die HerkunftGithub-Haelfte von
// gkp-k1o: der sinnvolle Wert von AnsichtEintrag.Projekt ist bei GitHub der
// lokale Repo-Name — NICHT die nwo, die schon in Karte steht.
//
// NUR die Weiterleitung: der Test setzt ReportsMap.Projekt von Hand und prueft
// bloss, dass ansichtAus ihn unveraendert durchreicht. Ob der Stempel selbst
// gesetzt wird — auch fuer ein Repo ganz ohne .beads/ —, prueft NICHT dieser
// Test, sondern TestStandNachMitStempeltGithubProjektAuchOhneBeads
// (teilbaum_test.go), der den echten Weg durch StandNachMit faehrt (Spec §9.1
// Muster 3: "Test baut am echten Weg vorbei auf").
func TestGithubEintragTraegtDasProjektDerKarte(t *testing.T) {
	maps := []ReportsMap{hKarte("m", hBead("bd-1"))}
	gh := GithubKarten("o/r", []collectors.GhIssue{ghIssue(7)})
	for i := range gh {
		gh[i].Projekt = "mein-repo"
	}

	a := ansichtAus(alleRegel(), beadsUndGithub(maps, gh), jetztTest())

	projekt := map[string]string{}
	for _, e := range a.Eintraege {
		projekt[e.Bead] = e.Projekt
	}
	if projekt["#7"] != "mein-repo" {
		t.Errorf("Projekt der GitHub-Zeile = %q, want %q (nicht die nwo %q)", projekt["#7"], "mein-repo", "o/r")
	}
}

// TestGithubKartenBauenDenBaumAusParent: die Schachtelung steckt im
// parent-Feld, nicht in der Nummer (Spec §3.2 fuer beads, §8.3 fuer GitHub).
// Gepruefte Folgen: Kind unter Elternteil eingerueckt, Enkel eine Stufe
// tiefer, und der Fortschritt zaehlt ueber ALLE Nachfahren (Spec §6) — nicht
// nur die direkten, die subIssuesSummary allein liefern wuerde.
func TestGithubKartenBauenDenBaumAusParent(t *testing.T) {
	gh := GithubKarten("o/r", []collectors.GhIssue{
		ghIssue(1),
		ghIssue(2, func(i *collectors.GhIssue) { i.Parent = 1 }),
		ghIssue(3, func(i *collectors.GhIssue) { i.Parent = 2; i.State = "CLOSED" }),
	})
	if len(gh) != 1 {
		t.Fatalf("Karten = %d, want 1", len(gh))
	}
	if len(gh[0].Beads) != 1 || gh[0].Beads[0].ID != "#1" {
		t.Fatalf("Wurzeln = %v, want nur #1", kinderIDs(gh[0].Beads))
	}
	wurzel := gh[0].Beads[0]
	if len(wurzel.Children) != 1 || wurzel.Children[0].ID != "#2" {
		t.Fatalf("Kinder von #1 = %v, want [#2]", kinderIDs(wurzel.Children))
	}
	if len(wurzel.Children[0].Children) != 1 || wurzel.Children[0].Children[0].ID != "#3" {
		t.Fatalf("Kinder von #2 = %v, want [#3]", kinderIDs(wurzel.Children[0].Children))
	}
	if wurzel.Counts == nil {
		t.Fatal("#1 hat Nachfahren und trotzdem keine Counts")
	}
	if wurzel.Counts.Total != 2 || wurzel.Counts.Closed != 1 {
		t.Errorf("Counts von #1 = %+v, want Total 2 / Closed 1 (Enkel zaehlt mit)", *wurzel.Counts)
	}

	// Die Ansicht rueckt den Ast in derselben Tiefe ein wie einen beads-Ast.
	a := ansichtAus(alleRegel(), beadsUndGithub(nil, gh), jetztTest())
	tiefe := map[string]int{}
	for _, e := range a.Eintraege {
		tiefe[e.Bead] = e.Tiefe
	}
	if tiefe["#1"] != 0 || tiefe["#2"] != 1 {
		t.Errorf("Tiefen = %v, want #1 auf 0 und #2 auf 1", tiefe)
	}
	if _, da := tiefe["#3"]; da {
		t.Errorf("#3 ist geschlossen und steht trotzdem in der Liste: %v", tiefe)
	}
}

// TestGithubWaiseStehtTrotzdemDa: `subIssues` kann auf ein Issue zeigen, das
// nicht mitgeholt wurde (Paginierung, geloescht, uebertragen). Eine Aufgabe,
// deren Elternteil fehlt, muss auf Ebene 0 stehen statt zu verschwinden —
// verschluckt saehe die Liste vollstaendig aus.
func TestGithubWaiseStehtTrotzdemDa(t *testing.T) {
	gh := GithubKarten("o/r", []collectors.GhIssue{
		ghIssue(9, func(i *collectors.GhIssue) { i.Parent = 4711 }),
	})
	if len(gh) != 1 || len(gh[0].Beads) != 1 || gh[0].Beads[0].ID != "#9" {
		t.Fatalf("Waise verschwunden: %+v", gh)
	}
}

// TestGithubAufgabeStehtNichtInHeute ist die Einschraenkung aus E1, als Test:
// GitHub hat kein Gegenstueck zu When/Deadline (Spec §4.4), also kann eine
// GitHub-Aufgabe keinen Auswahlgrund von Heute erfuellen. Der zweite Teil ist
// der wichtigere: die Ansicht SAGT das, statt es stillschweigend zu tun.
func TestGithubAufgabeStehtNichtInHeute(t *testing.T) {
	gh := GithubKarten("o/r", []collectors.GhIssue{ghIssue(7)})
	a, ok := AnsichtBauenMit(HeuteID, nil, gh, jetztTest())
	if !ok {
		t.Fatalf("AnsichtBauenMit(%q) kennt die Ansicht nicht", HeuteID)
	}
	if len(a.Eintraege) != 0 {
		t.Errorf("Heute = %v, want leer — GitHub traegt kein When/Deadline", zeilenIDs(a))
	}
	if !strings.Contains(strings.ToLower(a.Hinweis), "github") {
		t.Errorf("Hinweis = %q — die Einschraenkung muss in der Ansicht stehen, nicht nur in der Spec", a.Hinweis)
	}
}

// TestGithubAufgabeStehtInJederzeit ist die andere Haelfte derselben Regel und
// das Abnahmekriterium dieses Tickets: sie erscheint NUR in Jederzeit.
//
// Gemessen, nicht angenommen — und die Messung war der Grund fuer die eine
// Zeile in jederzeit.go: waehltJederzeit fragte `k.Bead.Ready`, und das ist bds
// Urteil ueber Blocker. Eine GitHub-Aufgabe hat es nicht, ist nicht in_progress
// und traegt keinen Termin; sie fiel damit durch JEDE der sechs Ansichten
// hindurch und stand in keiner. Das Abzeichen aus gkp-7gb waere gebaut und nie
// zu sehen gewesen.
//
// Der Test steht hier und nicht in jederzeit_test.go, weil er zu diesem Ticket
// gehoert: er haelt die Einschraenkung aus E1 als GANZES fest — drin in
// Jederzeit, draussen in den vier anderen offenen Listen.
func TestGithubAufgabeStehtInJederzeit(t *testing.T) {
	gh := GithubKarten("o/r", []collectors.GhIssue{ghIssue(7)})

	a, ok := AnsichtBauenMit(JederzeitID, nil, gh, jetztTest())
	if !ok {
		t.Fatalf("AnsichtBauenMit(%q) kennt die Ansicht nicht", JederzeitID)
	}
	gleichFolge(t, "Jederzeit", zeilenIDs(a), []string{"#7"})

	// Und nirgends sonst. Die geschlossene Seite ist ausgenommen: das Logbuch
	// ist die Umkehrung jeder anderen Ansicht und nimmt ein geschlossenes
	// Issue zu Recht auf — hier ist #7 offen, steht also auch dort nicht.
	for _, r := range Regeln() {
		if r.ID == JederzeitID {
			continue
		}
		b, _ := AnsichtBauenMit(r.ID, nil, gh, jetztTest())
		if len(b.Eintraege) != 0 {
			t.Errorf("%s = %v, want leer — eine GitHub-Aufgabe ohne Termin steht nur in Jederzeit (Spec §4.4)",
				r.ID, zeilenIDs(b))
		}
	}
}

// TestGithubGeschlossenesElternIssueWirdUeberschriftFuerOffenesKind
// beantwortet die Frage aus gkp-a1d, ob die Zwischenueberschrift (Spec §5)
// auch fuer HerkunftGithub gilt: JA, weil sammle herkunftsblind ist — sie
// fragt r.Waehlt(k) fuer jeden Kandidaten, unabhaengig davon, woher er kommt.
//
// Der konkrete Fall entsteht anders als bei beads, aber genauso zwangslaeufig:
// eine GitHub-Aufgabe waehlt sich in Jederzeit ueber k.Anfassbar(), und das
// faellt fuer HerkunftGithub mit k.Offen() zusammen (Begruendung an
// Kandidat.Anfassbar — GitHub kennt keinen Blocker). Ein GESCHLOSSENES
// Eltern-Issue mit einem noch OFFENEN Kind ist damit selbst nicht gewaehlt
// (nicht offen), sein Kind schon — genau die Konstellation, die eine
// Zwischenueberschrift entstehen laesst.
func TestGithubGeschlossenesElternIssueWirdUeberschriftFuerOffenesKind(t *testing.T) {
	gh := GithubKarten("o/r", []collectors.GhIssue{
		ghIssue(1, func(i *collectors.GhIssue) { i.State = "CLOSED" }),
		ghIssue(2, func(i *collectors.GhIssue) { i.Parent = 1 }),
	})

	a, ok := AnsichtBauenMit(JederzeitID, nil, gh, jetztTest())
	if !ok {
		t.Fatalf("AnsichtBauenMit(%q) kennt die Ansicht nicht", JederzeitID)
	}
	gleichFolge(t, "Jederzeit", zeilenIDs(a), []string{"#1", "#2"})

	zeilen := map[string]AnsichtEintrag{}
	for _, e := range a.Eintraege {
		zeilen[e.Bead] = e
	}
	if !zeilen["#1"].Ueberschrift {
		t.Error("#1 ist geschlossen und hat keinen eigenen Jederzeit-Grund — es muss als " +
			"Zwischenueberschrift stehen, nicht als Aufgabenzeile")
	}
	if zeilen["#1"].FristHerkunft != "#2" {
		t.Errorf("FristHerkunft(#1) = %q, want %q", zeilen["#1"].FristHerkunft, "#2")
	}
	if zeilen["#2"].Ueberschrift {
		t.Error("#2 ist offen und startklar — eine echte Aufgabenzeile, keine Ueberschrift")
	}
	// Die Kopfbilanz zaehlt nur die echte Aufgabe, nicht die geliehene
	// Ueberschrift.
	if got := zaehlerWert(a, GrundStartklar); got != 1 {
		t.Errorf("Zaehler[%q] = %d, want 1", GrundStartklar, got)
	}
}

// TestGithubDoppelerfassungBleibtStehen: E1 nimmt sie in Kauf, die Ansicht
// loest sie NICHT auf (Spec §3.7, O2). Zwei gleichlautende Aufgaben aus beiden
// Quellen stehen beide da, unterschieden allein durch das Abzeichen. Der Test
// haelt die Entscheidung fest, damit ein spaeterer "Aufraeumer" sie nicht
// versehentlich umdreht.
func TestGithubDoppelerfassungBleibtStehen(t *testing.T) {
	maps := []ReportsMap{hKarte("m", hBead("bd-1", func(b *ReportsBead) { b.Title = "Reverse-Proxy härten" }))}
	gh := GithubKarten("o/r", []collectors.GhIssue{
		ghIssue(7, func(i *collectors.GhIssue) { i.Title = "Reverse-Proxy härten" }),
	})
	a := ansichtAus(alleRegel(), beadsUndGithub(maps, gh), jetztTest())
	if len(a.Eintraege) != 2 {
		t.Fatalf("Eintraege = %v, want beide — Doppelerfassung wird sichtbar gemacht, nicht aufgeloest", zeilenIDs(a))
	}
}

// TestGithubKartenAusAntwort liest die Karten aus genau der Form, die
// collectors.CollectGitHub liefert — nicht aus einer im Test nachgebauten.
// Fall 3 in Spec §9.1: ein Test, der die Vorbereitung am echten Weg vorbei
// aufbaut, prueft eine Welt, die es nicht gibt.
func TestGithubKartenAusAntwort(t *testing.T) {
	antwort := map[string]any{
		"available": true,
		"nwo":       "example-user/infra",
		"issues":    []collectors.GhIssue{ghIssue(7)},
	}
	karten := GithubKartenAus(antwort)
	if len(karten) != 1 || karten[0].ID != "example-user/infra" {
		t.Fatalf("Karten = %+v, want eine mit der nwo als Id", karten)
	}
	if len(karten[0].Beads) != 1 || karten[0].Beads[0].ID != "#7" {
		t.Errorf("Beads = %v, want [#7]", kinderIDs(karten[0].Beads))
	}

	// Fail-open: eine Karte ohne GitHub liefert keine Aufgaben und keinen Panik.
	if k := GithubKartenAus(map[string]any{"available": false, "reason": "kein origin-Remote"}); len(k) != 0 {
		t.Errorf("Karten ohne GitHub = %+v, want keine", k)
	}
	if k := GithubKartenAus(nil); len(k) != 0 {
		t.Errorf("Karten aus nil = %+v, want keine", k)
	}
}

// TestAnsichtenAntwortZaehltDieZweiteQuelleMit ist die Go-Haelfte von gkp-d52.
//
// Der Rumpf der Route traegt seit diesem Ticket auch die Zahlen der
// Navigation, und zwar fuer JEDE angemeldete Ansicht — nicht nur fuer die
// gerade geoeffnete. Das ist der Punkt: der Browser hat sechs Zahlen in der
// Seitenleiste und sieht immer nur eine Liste; die fuenf anderen waeren sonst
// unsichtbar falsch.
//
// Geprueft wird die Gleichheit mit der Zahl, die der Listenkopf zeigt
// (len(Eintraege)). Dass der Browser wirklich diese Zahl zeichnet, misst
// assets/navzahl_browser_test.go — hier steht die Zusage der Nutzlast.
func TestAnsichtenAntwortZaehltDieZweiteQuelleMit(t *testing.T) {
	maps := []ReportsMap{hKarte("m",
		hBead("bd-1", func(b *ReportsBead) { b.Ready = true }),
		hBead("bd-2", func(b *ReportsBead) { b.Ready = true }),
	)}
	gh := GithubKarten("o/r", []collectors.GhIssue{ghIssue(7), ghIssue(8), ghIssue(9)})

	navZahlen := func(rumpf map[string]any) map[string]string {
		out := map[string]string{}
		items := aArr(rumpf["nav"])
		if len(items) == 0 {
			t.Fatal("der Rumpf traegt keine Navigationszahlen — die Navigation bleibt dann " +
				"auf dem Stand des Seitenaufbaus stehen, also ohne GitHub")
		}
		for _, it := range items {
			m := aMap(it)
			out[aStr(m["id"])] = aStr(m["cnt"])
		}
		return out
	}

	// 1. Beide Quellen: die Zahl der Navigation ist die Zahl des Listenkopfs —
	//    und zwar in JEDER Ansicht, nicht nur in der, die gerade offen ist.
	ansichten := AnsichtenBauenAus(beadsUndGithub(maps, gh), jetztTest())
	mit := navZahlen(AnsichtenAntwort(ansichten, ""))
	for _, r := range Regeln() {
		want := itoa(len(ansichten[r.ID].Eintraege))
		if mit[r.ID] != want {
			t.Errorf("%s: Navigation sagt %q, der Listenkopf zeigt %q Aufgaben",
				r.ID, mit[r.ID], want)
		}
	}

	// 2. Die Gegenprobe, ohne die Punkt 1 auch dann gruen waere, wenn GitHub
	//    gar nicht mitgezaehlt wuerde: dieselbe Ansicht ohne die zweite Quelle
	//    muss eine KLEINERE Zahl tragen. 2 Beads, 3 Issues — die Fixture ist
	//    absichtlich ungleich gross, sonst waere die Differenz nicht zu sehen.
	ohne := navZahlen(AnsichtenAntwort(AnsichtenBauenAus(beadsUndGithub(maps, nil), jetztTest()), "kein Token"))
	if ohne[JederzeitID] != "2" || mit[JederzeitID] != "5" {
		t.Errorf("Jederzeit: ohne GitHub %q (want 2), mit GitHub %q (want 5)",
			ohne[JederzeitID], mit[JederzeitID])
	}

	// 3. Antwortet GitHub nicht, sagt der Rumpf warum — und die Zahl ist die
	//    der beads-Haelfte, nicht eine aus einem frueheren Lauf.
	leer := AnsichtenAntwort(AnsichtenBauenAus(beadsUndGithub(maps, nil), jetztTest()), "kein Token")
	if aStr(leer["github_leer"]) != "kein Token" {
		t.Errorf("github_leer = %q — ohne Grund ist \"keine Issues\" von \"nicht erreichbar\" "+
			"nicht zu unterscheiden", aStr(leer["github_leer"]))
	}
	if _, da := AnsichtenAntwort(AnsichtenBauenAus(beadsUndGithub(maps, gh), jetztTest()), "")["github_leer"]; da {
		t.Error("github_leer steht auch dann im Rumpf, wenn GitHub geantwortet hat")
	}
}
