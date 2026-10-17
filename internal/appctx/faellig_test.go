package appctx

// Faelligkeitsdatum im Issue-Text und die Ansichten Heute, Demnaechst,
// Irgendwann (faelligkeit, #39).

import (
	"strings"
	"testing"
	"time"

	"github.com/niclasedge/git-planner-go/internal/collectors"
	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

func TestFaelligLesenUndSetzen(t *testing.T) {
	text := "Beschreibung\n\nFaellig: 2026-09-01"
	if got := FaelligAus(text); got != "2026-09-01" {
		t.Errorf("FaelligAus %q", got)
	}
	if got := TextMitFaellig(text, "2026-10-02"); got != "Beschreibung\n\nFällig: 2026-10-02" {
		t.Errorf("TextMitFaellig %q", got)
	}
	if got := TextMitFaellig("A\n\nFällig: 2026-09-01", ""); got != "A" {
		t.Errorf("Entfernen %q", got)
	}
	if got := FaelligAus("Kein Datum hier"); got != "" {
		t.Errorf("ohne Datum %q", got)
	}
	faelle := map[string]string{
		"x\nDue: 2026-01-02\n\n":        "2026-01-02",
		"Fällig: 2026-03-04":            "2026-03-04",
		"Fällig: 2026-03-04\nmehr Text": "",
		"Fällig: morgen":                "",
		"":                              "",
	}
	for in, want := range faelle {
		if got := FaelligAus(in); got != want {
			t.Errorf("FaelligAus(%q) = %q, erwartet %q", in, got, want)
		}
	}
	if got := TextMitFaellig("", "2026-10-02"); got != "Fällig: 2026-10-02" {
		t.Errorf("leerer Text %q", got)
	}
	// Mehrere alte Zeilen am Ende werden alle ersetzt, der Rest bleibt.
	if got := TextMitFaellig("Kopf\nFällig: 2026-01-01\nDue: 2026-01-02", "2026-02-03"); got != "Kopf\n\nFällig: 2026-02-03" {
		t.Errorf("mehrere alte Zeilen %q", got)
	}
}

func uIssue(n int, text string) ghrest.Issue {
	return ghrest.Issue{Nummer: n, Titel: "t" + string(rune('0'+n)), Text: text}
}

func TestUebersichtEinteilung(t *testing.T) {
	jetzt := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	repos := []UebersichtRepo{
		{Repo: "acme/alpha", Issues: []ghrest.Issue{
			uIssue(1, "a\n\nFällig: 2026-09-20"), uIssue(2, "Fällig: 2026-09-24"), uIssue(3, "Fällig: 2026-09-30"), uIssue(4, "ohne")}},
		{Repo: "acme/beta", Issues: []ghrest.Issue{uIssue(7, "Fällig: 2026-09-22"), {Nummer: 9, Text: "Fällig: 2026-09-01", IstPR: true}}},
	}
	u := UebersichtBauen(repos, jetzt)
	kette := func(es []UebersichtEintrag) string {
		var s []string
		for _, e := range es {
			s = append(s, strings.TrimPrefix(e.Repo, "acme/")+"#"+string(rune('0'+e.Nummer))+":"+e.Termin)
		}
		return strings.Join(s, ",")
	}
	if got := kette(u.Heute); got != "alpha#1:ueberfaellig,beta#7:ueberfaellig,alpha#2:heute" {
		t.Errorf("heute %s", got)
	}
	if got := kette(u.Demnaechst); got != "alpha#3:demnaechst" || u.Demnaechst[0].Tage != 6 {
		t.Errorf("demnaechst %s (tage %v)", got, u.Demnaechst)
	}
	if got := kette(u.Irgendwann); got != "alpha#4:" {
		t.Errorf("irgendwann %s", got)
	}
	z := u.Zaehler
	if z.Ueberfaellig != 2 || z.Heute != 1 || z.Demnaechst != 1 || z.Irgendwann != 1 {
		t.Errorf("Zaehler %+v", z)
	}
	if u.Heute[0].Tage != -4 || u.Heute[0].Faellig != "2026-09-20" {
		t.Errorf("#1 %+v", u.Heute[0])
	}
}

func TestArbeitslisteTraegtDatum(t *testing.T) {
	jetzt := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	a := RepoArbeitBauen(ArbeitEingabe{Repo: "acme/alpha", Jetzt: jetzt,
		Offen:   []ghrest.Issue{uIssue(1, "Fällig: 2026-09-23"), uIssue(2, "Fällig: 2026-09-24"), uIssue(3, "nichts")},
		Changes: []collectors.OpenspecChange{}})
	termine := map[int]string{}
	for _, e := range a.Reihe {
		termine[e.Nummer] = e.Faellig + "/" + e.Termin
	}
	if termine[1] != "2026-09-23/ueberfaellig" || termine[2] != "2026-09-24/heute" || termine[3] != "/" {
		t.Errorf("Termine %v", termine)
	}
}
