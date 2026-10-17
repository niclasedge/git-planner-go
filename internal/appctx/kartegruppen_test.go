package appctx

// Issues nach Parent (karte-gruppen, #121, Entwurf H): je Map eine Gruppe und
// eine "ohne Parent", ihre Folge, die offenen Fragen je Gruppe, aktiv,
// ausstehend als Baum nach blocked_by und gemergt, dazu Schaetzung und Ziel.
// Die Eingabe ist das Szenario aus karte_test.go (szenarioEingabe).

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

func gruppeVon(t *testing.T, k Karte, nr int) KartenGruppe {
	t.Helper()
	for _, g := range k.Gruppen {
		if g.Nummer == nr {
			return g
		}
	}
	t.Fatalf("keine Gruppe %d in %+v", nr, k.Gruppen)
	return KartenGruppe{}
}

func zeilenNummern(zs []KartenZeile) string {
	var n []string
	for _, z := range zs {
		n = append(n, fmt.Sprint(z.Nummer))
	}
	return strings.Join(n, ",")
}

func zeileVon(t *testing.T, zs []KartenZeile, nr int) KartenZeile {
	t.Helper()
	for _, z := range zs {
		if z.Nummer == nr {
			return z
		}
	}
	t.Fatalf("keine Zeile #%d in %s", nr, zeilenNummern(zs))
	return KartenZeile{}
}

func chipsText(cs []KartenChip) string {
	var n []string
	for _, c := range cs {
		n = append(n, fmt.Sprintf("%d:%s", c.Nummer, c.Art))
	}
	return strings.Join(n, ",")
}

func TestKarteGruppenFolgeUndKopf(t *testing.T) {
	k := KarteBauen(szenarioEingabe(t))
	var folge []string
	for _, g := range k.Gruppen {
		folge = append(folge, fmt.Sprint(g.Nummer))
	}
	if strings.Join(folge, ",") != "60,36,70,0" {
		t.Fatalf("Gruppen %v, erwartet 60,36,70,0", folge)
	}
	var koepfe []string
	for _, g := range k.Gruppen {
		koepfe = append(koepfe, fmt.Sprintf("%d:%s:%d/%d:a%d:o%d:p%d:h%d", g.Nummer, g.Titel, g.Erledigt, g.Gesamt,
			g.AnzahlAktiv, g.AnzahlAusstehend, g.P1Frage, g.Hebel))
	}
	want := "60:Fork-Unterstuetzung:0/4:a0:o4:p88:h4 | 36:Repo-Karte:1/4:a1:o2:p0:h0 | " +
		"70:PR-Lebenslauf:4/4:a0:o0:p0:h0 | 0:ohne Parent:5/11:a2:o4:p91:h2"
	if strings.Join(koepfe, " | ") != want {
		t.Errorf("Koepfe\n  %s\nerwartet\n  %s", strings.Join(koepfe, " | "), want)
	}
	for _, g := range k.Gruppen {
		if g.GemergtAnfangs != 3 {
			t.Errorf("Gruppe %d gemergt_anfangs %d, erwartet 3", g.Nummer, g.GemergtAnfangs)
		}
	}
	if g := gruppeVon(t, k, 60); g.URL == "" {
		t.Errorf("Gruppe 60 ohne URL")
	}
	b, _ := json.Marshal(gruppeVon(t, k, 70))
	for _, feld := range []string{`"fragen":[]`, `"aktiv":[]`, `"ausstehend":[]`, `"gemergt_anfangs":3`, `"anzahl_aktiv":0`} {
		if !strings.Contains(string(b), feld) {
			t.Errorf("Gruppe 70 %s, erwartet %s", b, feld)
		}
	}
}

func TestKarteGruppenFragen(t *testing.T) {
	k := KarteBauen(szenarioEingabe(t))
	var wo []string
	for _, g := range k.Gruppen {
		for _, f := range g.Fragen {
			wo = append(wo, fmt.Sprintf("%d@%d", f.Nummer, g.Nummer))
		}
	}
	if strings.Join(wo, ",") != "88@60,91@0,93@0" {
		t.Fatalf("Fragen je Gruppe %v, erwartet 88@60, 91@0, 93@0", wo)
	}
	f88 := gruppeVon(t, k, 60).Fragen[0]
	if chipsText(f88.Chips) != "102:direkt,104:direkt,107:transitiv,111:transitiv" {
		t.Errorf("#88 Chips %s", chipsText(f88.Chips))
	}
	if f88.Prio != 1 || f88.Hebel != 4 || f88.Wer != "alex" || f88.SeitTagen != 6 ||
		f88.Wortlaut != "Fork-Repos: PAT des Forks oder des Upstreams?" || f88.Titel != "Token-Quelle fuer Forks?" || f88.URL == "" {
		t.Errorf("#88 %+v", f88)
	}
	ohne := gruppeVon(t, k, 0).Fragen
	if chipsText(ohne[0].Chips) != "106:direkt,109:transitiv" || len(ohne[1].Chips) != 0 || ohne[1].Prio != 2 {
		t.Errorf("#91 Chips %s, #93 Chips %s prio %d", chipsText(ohne[0].Chips), chipsText(ohne[1].Chips), ohne[1].Prio)
	}
	if b, _ := json.Marshal(ohne[1]); !strings.Contains(string(b), `"chips":[]`) {
		t.Errorf("#93 %s, erwartet chips als leere Liste", b)
	}
}

func TestKarteGruppenAusstehendAlsBaum(t *testing.T) {
	k := KarteBauen(szenarioEingabe(t))
	baum := func(zs []KartenZeile) string {
		var n []string
		for _, z := range zs {
			n = append(n, fmt.Sprintf("%d/t%d/u%d", z.Nummer, z.Tiefe, z.Unter))
		}
		return strings.Join(n, ",")
	}
	sechzig := gruppeVon(t, k, 60).Ausstehend
	if got := baum(sechzig); got != "104/t0/u0,102/t0/u0,107/t1/u102,111/t2/u107" {
		t.Errorf("Gruppe 60 %s, erwartet 104, 102, 107 unter 102, 111 unter 107", got)
	}
	ohne := gruppeVon(t, k, 0).Ausstehend
	if got := baum(ohne); got != "105/t0/u0,106/t0/u0,109/t1/u106,108/t0/u0" {
		t.Errorf("Gruppe 0 %s, erwartet 105, 106, 109 unter 106, 108", got)
	}
	if got := baum(gruppeVon(t, k, 36).Ausstehend); got != "103/t0/u0,110/t0/u0" {
		t.Errorf("Gruppe 36 %s, erwartet 103, 110", got)
	}

	z102, z103, z111, z110 := zeileVon(t, sechzig, 102), zeileVon(t, gruppeVon(t, k, 36).Ausstehend, 103), zeileVon(t, sechzig, 111),
		zeileVon(t, gruppeVon(t, k, 36).Ausstehend, 110)
	if chipsText(z102.WartetAuf) != "88:frage" || z102.HaengtAn != 0 || z102.Status != "gesperrt" || !z102.Gesperrt || z102.Rang != 1 {
		t.Errorf("#102 %+v", z102)
	}
	if chipsText(z103.WartetAuf) != "101:aktiv" || z103.Status != "wartet" || z103.Gesperrt {
		t.Errorf("#103 %+v", z103)
	}
	if chipsText(z111.WartetAuf) != "107:issue,104:issue" || z111.HaengtAn != 88 || z111.Status != "gesperrt" || z111.Rang != 10 {
		t.Errorf("#111 %+v", z111)
	}
	if z110.Status != "bereit" || !z110.Bereit || len(z110.WartetAuf) != 0 {
		t.Errorf("#110 %+v", z110)
	}
	if z109 := zeileVon(t, ohne, 109); z109.HaengtAn != 91 || chipsText(z109.WartetAuf) != "106:issue" {
		t.Errorf("#109 %+v", z109)
	}
	if b, _ := json.Marshal(z110); !strings.Contains(string(b), `"wartet_auf":[]`) {
		t.Errorf("#110 %s, erwartet wartet_auf als leere Liste", b)
	}
}

func TestKarteGruppenAktivUndGemergt(t *testing.T) {
	k := KarteBauen(szenarioEingabe(t))
	ohne := gruppeVon(t, k, 0)
	if got := zeilenNummern(ohne.Aktiv); got != "98,95" {
		t.Fatalf("aktiv in 0 %s, erwartet 98,95", got)
	}
	a98 := ohne.Aktiv[0]
	if a98.Status != "aktiv" || a98.Stufe != "im_pr" || a98.PR != 99 || a98.Checks != "rot" || a98.AufgabenErledigt != 6 ||
		a98.AufgabenGesamt != 6 || a98.Branch != "change/netz-retry" || a98.Geaendert != "vor 5 h" || a98.GeaendertVoll != "Commit vor 5 h" {
		t.Errorf("#98 %+v", a98)
	}
	if a101 := gruppeVon(t, k, 36).Aktiv; zeilenNummern(a101) != "101" || a101[0].Stufe != "gate2" || a101[0].AufgabenErledigt != 2 {
		t.Errorf("aktiv in 36 %+v", a101)
	}

	if got := zeilenNummern(ohne.Gemergt); got != "97,56,50,48,45" {
		t.Errorf("gemergt in 0 %s, erwartet neueste zuerst 97,56,50,48,45", got)
	}
	if got := zeilenNummern(gruppeVon(t, k, 70).Gemergt); got != "83,79,76,74" {
		t.Errorf("gemergt in 70 %s, erwartet 83,79,76,74", got)
	}
	if got := zeilenNummern(gruppeVon(t, k, 36).Gemergt); got != "49" {
		t.Errorf("gemergt in 36 %s, erwartet 49", got)
	}
	g97 := ohne.Gemergt[0]
	if g97.Status != "gemergt" || g97.ErledigtAm != "27.9." || g97.Change != "docker-betrieb" || g97.GemergtMit == nil ||
		g97.GemergtMit.Nummer != 98 || g97.GemergtMit.URL == "" || g97.Geaendert != "27. Sept." || g97.GeaendertVoll == "" {
		t.Errorf("#97 %+v gemergt_mit %+v", g97, g97.GemergtMit)
	}
	if len(gruppeVon(t, k, 60).Gemergt) != 0 {
		t.Errorf("Gruppe 60 hat Gemergtes: %s", zeilenNummern(gruppeVon(t, k, 60).Gemergt))
	}

	// Regel 1 aus einer gespeicherten Timeline geht vor dem Branch.
	e := szenarioEingabe(t)
	e.Schluesse = map[int]IssueSchluss{97: {Ereignisse: []ghrest.TimelineEreignis{{Ereignis: "closed", CommitID: "abc"}},
		CommitPRs: map[string][]ghrest.PR{"abc": {{Nummer: 200, Titel: "Docker", URL: "u200", Gemergt: true, GemergtAm: "2026-09-27T09:00:00Z"}}}}}
	if g := gruppeVon(t, KarteBauen(e), 0).Gemergt[0]; g.GemergtMit == nil || g.GemergtMit.Nummer != 200 {
		t.Errorf("#97 mit Timeline %+v, erwartet gemergt_mit #200", g.GemergtMit)
	}
}

func TestKarteGruppenSchaetzung(t *testing.T) {
	k := KarteBauen(szenarioEingabe(t))
	want := map[int]int{102: 3, 106: 3, 104: 0, 105: 0, 107: 0}
	for _, g := range k.Gruppen {
		for _, z := range g.Ausstehend {
			if soll, ok := want[z.Nummer]; ok && z.Schaetzung != soll {
				t.Errorf("#%d schaetzung %d, erwartet %d", z.Nummer, z.Schaetzung, soll)
			}
		}
	}
	for label, soll := range map[string]int{"schaetzung:3": 3, "Schaetzung: 12 ": 12, "schaetzung:0": 0, "schaetzung:-2": 0,
		"schaetzung:x": 0, "schaetzung:2.5": 0, "zeit:3": 0} {
		if got := SchaetzungAus([]ghrest.Label{{Name: "bug"}, {Name: label}}); got != soll {
			t.Errorf("SchaetzungAus(%q) %d, erwartet %d", label, got, soll)
		}
	}

	// Ohne die Schaetzungs-Labels bleiben Stationen und Folge gleich.
	e := szenarioEingabe(t)
	for i := range e.Reihe {
		e.Reihe[i].Labels = []ghrest.Label{}
	}
	ohne := KarteBauen(e)
	a, _ := json.Marshal(k.Stationen)
	b, _ := json.Marshal(ohne.Stationen)
	if string(a) != string(b) || zeilenNummern(gruppeVon(t, ohne, 60).Ausstehend) != zeilenNummern(gruppeVon(t, k, 60).Ausstehend) {
		t.Errorf("die Schaetzung aendert Stationen oder Folge")
	}
}

func TestKarteZiel(t *testing.T) {
	k := KarteBauen(szenarioEingabe(t))
	if k.Ziel != "Blocker schnell klaeren: was war, was laeuft, was blockiert, was kommt." {
		t.Errorf("ziel %q", k.Ziel)
	}
	for text, soll := range map[string]string{
		"# Projekt\n\nEinleitung.\n\n## Ziel\n\nBlocker schnell klaeren:\nwas war, was kommt.\n\nZweiter Absatz.\n": "Blocker schnell klaeren: was war, was kommt.",
		"# Projekt\r\n\r\n## Ziel\r\nDirekt darunter.\r\n## Weiter\r\n":                                             "Direkt darunter.",
		"# Projekt\n\n## Ziele und Wege\n\nNicht dieser.\n":                                                         "",
		"# Projekt\n\nOhne Abschnitt.\n":                                                                            "",
		"":                                                                                                          "",
	} {
		if got := ZielAus(text); got != soll {
			t.Errorf("ZielAus(%q) %q, erwartet %q", text, got, soll)
		}
	}
	e := szenarioEingabe(t)
	e.Projekt = ""
	if b, _ := json.Marshal(KarteBauen(e)); !strings.Contains(string(b), `"ziel":""`) {
		t.Errorf("Karte ohne PROJECT.md traegt kein leeres ziel")
	}
	if b, _ := json.Marshal(KarteBauen(KartenEingabe{Repo: "acme/alpha"})); !strings.Contains(string(b), `"gruppen":[]`) {
		t.Errorf("leere Karte %s, erwartet gruppen als leere Liste", b)
	}
}

// Eine Frage, deren Map keine Station hat, bekommt ihre eigene Gruppe; eine
// Frage, die Stationen zweier Gruppen sperrt, steht in beiden.
func TestKarteGruppenFrageOhneLinie(t *testing.T) {
	e := szenarioEingabe(t)
	for i := range e.Offen {
		if e.Offen[i].Nummer == 93 {
			e.Offen[i].ParentNummer = 77
		}
	}
	for i := range e.Reihe {
		if e.Reihe[i].Nummer == 105 {
			e.Reihe[i].BlockiertDurch = []int{88}
		}
	}
	k := KarteBauen(e)
	var wo []string
	for _, g := range k.Gruppen {
		for _, f := range g.Fragen {
			wo = append(wo, fmt.Sprintf("%d@%d", f.Nummer, g.Nummer))
		}
	}
	if strings.Join(wo, ",") != "88@60,93@77,88@0,91@0" {
		t.Errorf("Fragen je Gruppe %v, erwartet 88@60, 93@77, 88@0, 91@0", wo)
	}
	var folge []string
	for _, g := range k.Gruppen {
		folge = append(folge, fmt.Sprint(g.Nummer))
	}
	if strings.Join(folge, ",") != "60,36,70,77,0" {
		t.Errorf("Gruppen %v, erwartet 60,36,70,77,0", folge)
	}
	if g := gruppeVon(t, k, 77); g.Titel != "#77" || len(g.Ausstehend) != 0 {
		t.Errorf("Gruppe 77 %+v", g)
	}
}
