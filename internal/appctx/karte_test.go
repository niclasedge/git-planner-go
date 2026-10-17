package appctx

// Die Karte als Folge (repo-karte-folge, #99): erledigt, jetzt, naechste von
// links nach rechts, Linien je Map, offene Fragen als Schranke, Kanten mit Art
// und der kritische Pfad. Die Eingabe entsteht aus dem Szenario des
// freigegebenen Entwurfs G (testdata/karte_szenario.json, eine Kopie von
// docs/entwurf/repo-karte/szenario.json); was das Szenario nicht zeigt (die
// geschlossene Map #70, das elfte erledigte #40, das elfte naechste #112, die
// offenen Maps), kommt hier dazu.

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/niclasedge/git-planner-go/internal/collectors"
	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

type szenarioDaten struct {
	Jetzt   string `json:"jetzt"`
	Verlauf []struct {
		Nr       int    `json:"nr"`
		Titel    string `json:"titel"`
		Change   string `json:"change"`
		Erledigt string `json:"erledigt"`
		Map      int    `json:"map"`
	} `json:"verlauf"`
	Aktiv []struct {
		Nr       int    `json:"nr"`
		Titel    string `json:"titel"`
		Change   string `json:"change"`
		Branch   string `json:"branch"`
		Stufe    string `json:"stufe"`
		Aufgaben []int  `json:"aufgaben"`
		PR       int    `json:"pr"`
		Checks   string `json:"checks"`
		Map      int    `json:"map"`
		Commit   string `json:"letzter_commit"`
	} `json:"aktiv"`
	Klaeren []struct {
		Nr              int    `json:"nr"`
		Titel           string `json:"titel"`
		Frage           string `json:"frage"`
		Wer             string `json:"wer"`
		SeitTagen       int    `json:"seit_tagen"`
		BlockiertDirekt []int  `json:"blockiert_direkt"`
		Hebel           int    `json:"hebel"`
		Prio            int    `json:"prio"`
	} `json:"klaeren"`
	Naechste []struct {
		Nr        int    `json:"nr"`
		Titel     string `json:"titel"`
		WartetAuf []int  `json:"wartet_auf"`
		Map       int    `json:"map"`
		Bereit    bool   `json:"bereit"`
	} `json:"naechste"`
	Maps map[string]string `json:"maps"`
}

func szenarioLesen(t *testing.T) szenarioDaten {
	t.Helper()
	b, err := os.ReadFile("testdata/karte_szenario.json")
	if err != nil {
		t.Fatal(err)
	}
	var s szenarioDaten
	if err := json.Unmarshal(b, &s); err != nil {
		t.Fatal(err)
	}
	return s
}

// szenarioEingabe baut die Eingabe der Karte aus dem Szenario. Die Map #36
// kennt ihre Sub-Issues, #60 nur die Zusammenfassung (ihre Stationen nennen
// sie als Parent-Issue), #70 ist geschlossen und kennt ihre Sub-Issues. Fuer
// die Gruppen (karte-gruppen, #121) traegt jedes erledigte Issue seinen
// archivierten Change und einen gemergten PR #nr+1 auf change/KENNUNG, die
// aktiven ihre letzte Aenderung, #102 und #106 eine Schaetzung, und die
// Eingabe traegt das Ziel aus PROJECT.md.
func szenarioEingabe(t *testing.T) KartenEingabe {
	t.Helper()
	s := szenarioLesen(t)
	jetzt, err := time.Parse("2006-01-02", s.Jetzt)
	if err != nil {
		t.Fatal(err)
	}
	jetzt = jetzt.Add(12 * time.Hour)
	e := KartenEingabe{Repo: "example-org/git-planner", Jetzt: jetzt}
	url := func(nr int) string { return fmt.Sprintf("https://github.com/acme/x/issues/%d", nr) }
	parent := func(m int) int {
		if m == 60 {
			return 60
		}
		return 0
	}
	subs := map[int][]ghrest.Issue{}
	unterMap := func(m int, i ghrest.Issue) {
		if m == 36 || m == 70 {
			subs[m] = append(subs[m], i)
		}
	}

	// Geschlossen: das Szenario (neueste zuerst), dazu die Map #70 und #40.
	for _, v := range s.Verlauf {
		i := ghrest.Issue{Nummer: v.Nr, Titel: v.Titel, URL: url(v.Nr), Zustand: "closed",
			Erstellt: "2026-09-01T08:00:00Z", Geschlossen: v.Erledigt + "T10:00:00Z", ParentNummer: parent(v.Map)}
		e.Geschlossen = append(e.Geschlossen, i)
		unterMap(v.Map, i)
		e.Changes = append(e.Changes, collectors.OpenspecChange{Kennung: v.Change, Archiviert: true, Issue: v.Nr})
		e.GeschlossenePRs = append(e.GeschlossenePRs, ghrest.PR{Nummer: v.Nr + 1, Titel: v.Titel, URL: url(v.Nr + 1),
			Head: "change/" + v.Change, Gemergt: true, GemergtAm: v.Erledigt + "T09:30:00Z", Zustand: "closed"})
	}
	e.Projekt = "# git-planner\n\nDer Dienst hinter report-tool.\n\n## Ziel\n\nBlocker schnell klaeren: was war, was laeuft,\nwas blockiert, was kommt.\n\nMehr dazu spaeter.\n"
	karte70 := ghrest.Issue{Nummer: 70, Titel: s.Maps["70"], URL: url(70), Zustand: "closed", Erstellt: "2026-09-01T08:00:00Z",
		Geschlossen: "2026-09-28T09:00:00Z", SubGesamt: 4, SubErledigt: 4}
	e.Geschlossen = append([]ghrest.Issue{karte70}, e.Geschlossen...)
	e.Geschlossen = append(e.Geschlossen, ghrest.Issue{Nummer: 40, Titel: "Aelter", URL: url(40), Zustand: "closed",
		Erstellt: "2026-09-01T08:00:00Z", Geschlossen: "2026-09-15T10:00:00Z"})

	// Offen und die Reihe: erst die aktiven (im_pr vor in_arbeit, wie
	// RepoArbeitBauen sie stellt), dann die Fragen, dann die naechsten.
	offen := func(nr int, titel string, m int) ghrest.Issue {
		i := ghrest.Issue{Nummer: nr, Titel: titel, URL: url(nr), Zustand: "open", Erstellt: "2026-09-20T08:00:00Z",
			Labels: []ghrest.Label{}, Zustaendig: []string{}, ParentNummer: parent(m)}
		unterMap(m, i)
		return i
	}
	eintrag := func(i ghrest.Issue, status string, blocker []int) ArbeitsEintrag {
		ein := issueEintrag(i)
		ein.Status = status
		ein.BlockiertDurch = append([]int{}, blocker...)
		return ein
	}
	aktiv := map[int]ArbeitsEintrag{}
	for _, a := range s.Aktiv {
		i := offen(a.Nr, a.Titel, a.Map)
		e.Offen = append(e.Offen, i)
		status, gates := ArbeitInArbeit, []string{"gate1"}
		switch a.Stufe {
		case "gate2":
			gates = []string{"gate1", "gate2"}
		case "im_pr":
			status, gates = ArbeitImPR, []string{"gate1", "gate2", "fertig"}
		}
		ein := eintrag(i, status, nil)
		ein.Change = &ChangeKurz{Kennung: a.Change, Status: "laeuft", Issue: a.Nr, Erledigt: a.Aufgaben[0], Gesamt: a.Aufgaben[1], Gates: gates}
		b := IssueBranch{Name: a.Branch, Aktiv: true, Gruende: []string{"change"}}
		if a.PR > 0 {
			b.PR = &BranchPR{Nummer: a.PR, Checks: &ghrest.PRChecks{Gesamt: a.Checks}}
		}
		ein.Branches = []IssueBranch{{Name: "alt/" + a.Change, Gruende: []string{"name"}}, b}
		ein.Aktiv = true
		ein.ZuletztGeaendert = &ZuletztGeaendert{Quelle: GeaendertBranch, Text: a.Commit, Voll: "Commit " + a.Commit}
		aktiv[a.Nr] = ein
	}
	for _, nr := range []int{98, 95, 101} {
		e.Reihe = append(e.Reihe, aktiv[nr])
	}
	for _, k := range s.Klaeren {
		i := offen(k.Nr, k.Titel, 0)
		label := "question"
		if k.Nr == 91 {
			label = "Frage"
		}
		i.Labels = []ghrest.Label{{Name: "enhancement"}, {Name: label}}
		i.Text = "\n   \n" + k.Frage + "\n\nHintergrund folgt."
		if k.Wer != "offen" {
			i.Zustaendig = []string{k.Wer, "zweite"}
		}
		i.Erstellt = jetzt.AddDate(0, 0, -k.SeitTagen).Add(-3 * time.Hour).Format(time.RFC3339)
		e.Offen = append(e.Offen, i)
		e.Reihe = append(e.Reihe, eintrag(i, ArbeitOhneChange, nil))
	}
	schaetzung := map[int]string{102: "schaetzung:3", 106: "schaetzung:3", 104: "schaetzung:0", 105: "Schaetzung:x"}
	for _, n := range s.Naechste {
		i := offen(n.Nr, n.Titel, n.Map)
		if l, ok := schaetzung[n.Nr]; ok {
			i.Labels = []ghrest.Label{{Name: l}}
		}
		e.Offen = append(e.Offen, i)
		// Ohne Blocker heisst die Station in der Reihe bereit; auf der Karte
		// ist bereit nur, was dort bereit heisst (karte-rollen, #102).
		status := ArbeitBereit
		if len(n.WartetAuf) > 0 {
			status = ArbeitBlockiert
		}
		e.Reihe = append(e.Reihe, eintrag(i, status, n.WartetAuf))
	}
	elf := offen(112, "Das elfte", 0)
	e.Offen = append(e.Offen, elf)
	e.Reihe = append(e.Reihe, eintrag(elf, ArbeitOhneChange, nil))
	for _, m := range []int{36, 60} {
		i := offen(m, s.Maps[fmt.Sprint(m)], 0)
		i.SubGesamt = 4
		if m == 36 {
			i.SubErledigt = 1
		}
		e.Offen = append(e.Offen, i)
		e.Reihe = append(e.Reihe, eintrag(i, ArbeitOhneChange, nil))
		e.Maps = append(e.Maps, KartenMapEingabe{Map: i, Subs: subs[m]})
	}
	e.Maps = append(e.Maps, KartenMapEingabe{Map: karte70, Subs: subs[70]})
	return e
}

func stationVon(k Karte, nr int) (KartenStation, bool) {
	for _, s := range k.Stationen {
		if s.Nummer == nr {
			return s, true
		}
	}
	return KartenStation{}, false
}

func zoneVon(k Karte, zone string) []KartenStation {
	var out []KartenStation
	for _, s := range k.Stationen {
		if s.Zone == zone {
			out = append(out, s)
		}
	}
	// nach slot, damit der Test nicht an der Reihenfolge der Liste haengt
	for a := 1; a < len(out); a++ {
		for b := a; b > 0 && out[b].Slot < out[b-1].Slot; b-- {
			out[b], out[b-1] = out[b-1], out[b]
		}
	}
	return out
}

func nummernVon(ss []KartenStation) string {
	var n []string
	for _, s := range ss {
		n = append(n, fmt.Sprint(s.Nummer))
	}
	return strings.Join(n, ",")
}

func TestKarteZoneErledigt(t *testing.T) {
	k := KarteBauen(szenarioEingabe(t))
	erledigt := zoneVon(k, ZoneErledigt)
	if got := nummernVon(erledigt); got != "45,48,49,50,56,74,76,79,83,97" {
		t.Fatalf("erledigt %s, erwartet die letzten zehn ohne Map #70 und ohne #40, aeltestes links", got)
	}
	for i, s := range erledigt {
		if s.Slot != i || s.Rang != 0 {
			t.Errorf("#%d slot %d rang %d, erwartet slot %d und rang 0", s.Nummer, s.Slot, s.Rang, i)
		}
	}
	if erledigt[0].Marke != "16.9." || erledigt[0].ErledigtAm != "16.9." || erledigt[9].Marke != "27.9." {
		t.Errorf("Marken #45 %q/%q, #97 %q; erwartet 16.9. und 27.9.", erledigt[0].Marke, erledigt[0].ErledigtAm, erledigt[9].Marke)
	}
	for _, nr := range []int{101, 102} {
		if s, _ := stationVon(k, nr); s.ErledigtAm != "" {
			t.Errorf("#%d erledigt_am %q, erwartet leer", nr, s.ErledigtAm)
		}
	}
	for _, nr := range []int{70, 40} {
		if _, da := stationVon(k, nr); da {
			t.Errorf("#%d ist Station, erwartet nicht", nr)
		}
	}
}

func TestKarteZoneJetzt(t *testing.T) {
	k := KarteBauen(szenarioEingabe(t))
	jetzt := zoneVon(k, ZoneJetzt)
	if got := nummernVon(jetzt); got != "98,95,101" {
		t.Fatalf("jetzt %s, erwartet 98,95,101 in der Folge der Reihe", got)
	}
	acht := jetzt[0]
	if acht.Stufe != "im_pr" || acht.PR != 99 || acht.Checks != "rot" || acht.AufgabenErledigt != 6 || acht.AufgabenGesamt != 6 ||
		acht.Branch != "change/netz-retry" || acht.Linie != 0 {
		t.Errorf("#98 %+v, erwartet im_pr, PR 99, rot, 6/6, change/netz-retry, Linie 0", acht)
	}
	fuenf, eins := jetzt[1], jetzt[2]
	if fuenf.Stufe != "gate1" || fuenf.Branch != "change/logbuch-gruppen" || fuenf.PR != 0 || fuenf.Checks != "" || fuenf.AufgabenGesamt != 5 {
		t.Errorf("#95 %+v, erwartet gate1, change/logbuch-gruppen, ohne PR, 0/5", fuenf)
	}
	if eins.Stufe != "gate2" || eins.AufgabenErledigt != 2 || eins.AufgabenGesamt != 7 || eins.Linie != 36 {
		t.Errorf("#101 %+v, erwartet gate2, 2/7, Linie 36", eins)
	}

	// Ein Issue ohne Change, aber mit aktivem Branch, steht auch in jetzt.
	e := szenarioEingabe(t)
	i := ghrest.Issue{Nummer: 120, Titel: "Mit Branch", Zustand: "open", Labels: []ghrest.Label{}, Zustaendig: []string{}}
	ein := issueEintrag(i)
	ein.Status = ArbeitOhneChange
	ein.Branches = []IssueBranch{{Name: "issue-120", Aktiv: true}}
	ein.Aktiv = true
	e.Offen = append(e.Offen, i)
	e.Reihe = append(append(append([]ArbeitsEintrag{}, e.Reihe[:3]...), ein), e.Reihe[3:]...)
	k = KarteBauen(e)
	if got := nummernVon(zoneVon(k, ZoneJetzt)); got != "98,95,101,120" {
		t.Fatalf("jetzt %s, erwartet 98,95,101,120", got)
	}
	if s, _ := stationVon(k, 120); s.Branch != "issue-120" || s.Stufe != "" || s.Slot != 3 {
		t.Errorf("#120 %+v, erwartet Branch issue-120, leere Stufe, slot 3", s)
	}
}

func TestKarteZoneNaechste(t *testing.T) {
	k := KarteBauen(szenarioEingabe(t))
	naechste := zoneVon(k, ZoneNaechste)
	if got := nummernVon(naechste); got != "102,103,104,105,106,107,108,109,110,111" {
		t.Fatalf("naechste %s, erwartet #102 bis #111", got)
	}
	for i, s := range naechste {
		if s.Rang != i+1 || s.Slot != i || s.Marke != fmt.Sprint(i+1) {
			t.Errorf("#%d rang %d slot %d marke %q, erwartet %d, %d, %q", s.Nummer, s.Rang, s.Slot, s.Marke, i+1, i, fmt.Sprint(i+1))
		}
	}
	for _, nr := range []int{112, 36, 60, 88, 91, 93} {
		if _, da := stationVon(k, nr); da {
			t.Errorf("#%d ist Station, erwartet nicht (elftes, Map oder Frage)", nr)
		}
	}
	if len(k.Stationen) != 23 {
		t.Errorf("%d Stationen, erwartet 23", len(k.Stationen))
	}
	var zonen []string
	for _, z := range k.Zonen {
		zonen = append(zonen, fmt.Sprintf("%s:%d", z.Name, z.Anzahl))
	}
	if strings.Join(zonen, ",") != "erledigt:10,jetzt:3,naechste:10" {
		t.Errorf("zonen %v, erwartet erledigt:10,jetzt:3,naechste:10", zonen)
	}
	if k.Stand != "28.9." {
		t.Errorf("stand %q, erwartet 28.9.", k.Stand)
	}
}

func TestKarteLinien(t *testing.T) {
	s := szenarioLesen(t)
	k := KarteBauen(szenarioEingabe(t))
	var linien []string
	for _, l := range k.Linien {
		linien = append(linien, fmt.Sprintf("%d:%s:%d/%d:a%d:g%d", l.Nummer, l.Titel, l.Erledigt, l.Gesamt, l.Aktiv, l.Gesperrt))
	}
	want := []string{
		"36:" + s.Maps["36"] + ":1/4:a1:g0",
		"60:" + s.Maps["60"] + ":0/4:a0:g4",
		"70:" + s.Maps["70"] + ":4/4:a0:g0",
		"0:ohne Map:5/11:a2:g2",
	}
	if strings.Join(linien, " | ") != strings.Join(want, " | ") {
		t.Errorf("Linien\n  %v\nerwartet\n  %v", linien, want)
	}
	for nr, linie := range map[int]int{49: 36, 110: 36, 102: 60, 111: 60, 83: 70, 97: 0, 105: 0} {
		if st, _ := stationVon(k, nr); st.Linie != linie {
			t.Errorf("#%d auf Linie %d, erwartet %d", nr, st.Linie, linie)
		}
	}
}

func TestKarteFragen(t *testing.T) {
	s := szenarioLesen(t)
	k := KarteBauen(szenarioEingabe(t))
	if len(k.Fragen) != len(s.Klaeren) {
		t.Fatalf("%d Fragen, erwartet %d", len(k.Fragen), len(s.Klaeren))
	}
	for i, soll := range s.Klaeren {
		f := k.Fragen[i]
		if f.Nummer != soll.Nr || f.Hebel != soll.Hebel || f.Prio != soll.Prio || fmt.Sprint(f.Sperrt) != fmt.Sprint(soll.BlockiertDirekt) {
			t.Errorf("Frage %d: #%d hebel %d prio %d sperrt %v; erwartet #%d, %d, %d, %v",
				i, f.Nummer, f.Hebel, f.Prio, f.Sperrt, soll.Nr, soll.Hebel, soll.Prio, soll.BlockiertDirekt)
		}
		if f.Wortlaut != soll.Frage || f.SeitTagen != soll.SeitTagen || f.Titel != soll.Titel {
			t.Errorf("Frage #%d wortlaut %q seit %d titel %q; erwartet %q, %d, %q", f.Nummer, f.Wortlaut, f.SeitTagen, f.Titel, soll.Frage, soll.SeitTagen, soll.Titel)
		}
		wer := soll.Wer
		if wer == "offen" {
			wer = ""
		}
		if f.Wer != wer {
			t.Errorf("Frage #%d wer %q, erwartet %q", f.Nummer, f.Wer, wer)
		}
	}
	if b, _ := json.Marshal(k.Fragen[2]); !strings.Contains(string(b), `"sperrt":[]`) {
		t.Errorf("Frage #93 %s, erwartet sperrt als leere Liste", b)
	}

	var gesperrt, bereit []string
	for _, st := range k.Stationen {
		if st.Gesperrt {
			gesperrt = append(gesperrt, fmt.Sprint(st.Nummer))
		}
		if st.Bereit {
			bereit = append(bereit, fmt.Sprint(st.Nummer))
		}
	}
	if strings.Join(gesperrt, ",") != "102,104,106,107,109,111" {
		t.Errorf("gesperrt %v, erwartet 102,104,106,107,109,111", gesperrt)
	}
	for _, n := range s.Naechste {
		st, _ := stationVon(k, n.Nr)
		if st.Bereit != n.Bereit || fmt.Sprint(st.WartetAuf) != fmt.Sprint(n.WartetAuf) {
			t.Errorf("#%d bereit %v wartet_auf %v, erwartet %v und %v", n.Nr, st.Bereit, st.WartetAuf, n.Bereit, n.WartetAuf)
		}
	}
	if strings.Join(bereit, ",") != "110" {
		t.Errorf("bereit %v, erwartet nur 110", bereit)
	}
	if b, _ := json.Marshal(zoneVon(k, ZoneErledigt)[0]); !strings.Contains(string(b), `"wartet_auf":[]`) {
		t.Errorf("erledigte Station %s, erwartet wartet_auf als leere Liste", b)
	}
}

func TestKarteKantenMitArt(t *testing.T) {
	k := KarteBauen(szenarioEingabe(t))
	var kanten []string
	for _, ka := range k.Kanten {
		kanten = append(kanten, fmt.Sprintf("%d>%d:%s", ka.Von, ka.Nach, ka.Art))
	}
	want := "88>102:frage,88>104:frage,91>106:frage,95>105:aktiv,98>108:aktiv,101>103:aktiv," +
		"102>107:folge,104>111:folge,106>109:folge,107>111:folge"
	if strings.Join(kanten, ",") != want {
		t.Errorf("Kanten\n  %s\nerwartet\n  %s", strings.Join(kanten, ","), want)
	}
}

func TestKarteKritischerPfad(t *testing.T) {
	k := KarteBauen(szenarioEingabe(t))
	if k.Kritisch != 88 {
		t.Errorf("kritisch %d, erwartet 88", k.Kritisch)
	}
	var fragen, stationen, kanten []string
	for _, f := range k.Fragen {
		if f.Kritisch {
			fragen = append(fragen, fmt.Sprint(f.Nummer))
		}
	}
	for _, s := range k.Stationen {
		if s.Kritisch {
			stationen = append(stationen, fmt.Sprint(s.Nummer))
		}
	}
	for _, ka := range k.Kanten {
		if ka.Kritisch {
			kanten = append(kanten, fmt.Sprintf("%d>%d", ka.Von, ka.Nach))
		}
	}
	if strings.Join(fragen, ",") != "88" || strings.Join(stationen, ",") != "102,104,107,111" {
		t.Errorf("kritisch: Fragen %v, Stationen %v; erwartet 88 und 102,104,107,111", fragen, stationen)
	}
	if strings.Join(kanten, ",") != "88>102,88>104,102>107,104>111,107>111" {
		t.Errorf("kritische Kanten %v", kanten)
	}
}

func TestKarteOhneFrageKeinPfad(t *testing.T) {
	e := szenarioEingabe(t)
	var offen []ghrest.Issue
	for _, i := range e.Offen {
		i.Labels = []ghrest.Label{}
		offen = append(offen, i)
	}
	e.Offen = offen
	k := KarteBauen(e)
	if k.Kritisch != 0 || len(k.Fragen) != 0 {
		t.Errorf("kritisch %d, %d Fragen; erwartet 0 und keine", k.Kritisch, len(k.Fragen))
	}
	for _, s := range k.Stationen {
		if s.Kritisch || s.Gesperrt {
			t.Errorf("#%d kritisch %v gesperrt %v ohne offene Frage", s.Nummer, s.Kritisch, s.Gesperrt)
		}
	}
}

func TestKarteLeer(t *testing.T) {
	k := KarteBauen(KartenEingabe{Repo: "acme/alpha", Jetzt: time.Now()})
	b, _ := json.Marshal(k)
	for _, feld := range []string{`"zonen":[`, `"linien":[]`, `"stationen":[]`, `"fragen":[]`, `"kanten":[]`, `"quellen":[]`} {
		if !strings.Contains(string(b), feld) {
			t.Errorf("leere Karte %s, erwartet %s", b, feld)
		}
	}
}

// Die Karte des Szenarios liegt als JSON neben dem Szenario; der Browser-Test
// (internal/assets/karte_browser_test.go) zeichnet genau sie. Weicht KarteBauen
// davon ab, faellt dieser Test — neu schreiben mit GKP_KARTE_SCHREIBEN=1.
func TestKarteSzenarioAlsJSON(t *testing.T) {
	const pfad = "testdata/karte_szenario_karte.json"
	b, err := json.MarshalIndent(KarteBauen(szenarioEingabe(t)), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	b = append(b, '\n')
	if os.Getenv("GKP_KARTE_SCHREIBEN") == "1" {
		if err := os.WriteFile(pfad, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	alt, err := os.ReadFile(pfad)
	if err != nil {
		t.Fatalf("%s fehlt: %v (GKP_KARTE_SCHREIBEN=1 schreibt sie)", pfad, err)
	}
	if string(alt) != string(b) {
		t.Errorf("%s weicht von KarteBauen ab; mit GKP_KARTE_SCHREIBEN=1 neu schreiben", pfad)
	}
}
