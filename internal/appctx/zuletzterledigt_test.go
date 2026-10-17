package appctx

// "Zuletzt erledigt" mit Uhrzeit und gemergtem PR (zuletzt-erledigt-pr,
// #116): der Text der Schliesszeit folgt der Regel von "Heute" (Mitternacht
// in der Zone von jetzt), und gemergt_mit kommt aus drei Regeln in fester
// Folge — Timeline, Branch change/KENNUNG, Titel "(#N)".

import (
	"testing"
	"time"

	"github.com/niclasedge/git-planner-go/internal/collectors"
	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

func zeZu(nr int, geschlossen string) ghrest.Issue {
	return ghrest.Issue{Nummer: nr, Titel: "Issue", URL: "u", Zustand: "closed", Geschlossen: geschlossen,
		Erstellt: "2026-09-01T08:00:00Z"}
}

func zeGemergt(nr int, titel, head, am string) ghrest.PR {
	return ghrest.PR{Nummer: nr, Titel: titel, URL: "https://github.com/acme/alpha/pull/" + itoa(nr), Head: head,
		Zustand: "closed", Gemergt: true, GemergtAm: am, GeschlossenAm: am}
}

func zeArchiv(kennung string, issue int) collectors.OpenspecChange {
	return collectors.OpenspecChange{Kennung: kennung, Ordner: "openspec/changes/archive/2026-09-20-" + kennung,
		Archiviert: true, Issue: issue, Gates: []string{"gate1", "gate2", "fertig"}}
}

func zeZuletzt(t *testing.T, w RepoArbeit) map[int]ArbeitsEintrag {
	t.Helper()
	out := map[int]ArbeitsEintrag{}
	for _, e := range w.Zuletzt {
		out[e.Nummer] = e
	}
	return out
}

func zeMitNummer(t *testing.T, wo string, e ArbeitsEintrag, nr int) {
	t.Helper()
	if nr == 0 {
		if e.GemergtMit != nil {
			t.Errorf("%s: gemergt_mit %+v, erwartet keins", wo, *e.GemergtMit)
		}
		return
	}
	if e.GemergtMit == nil {
		t.Fatalf("%s: kein gemergt_mit, erwartet #%d", wo, nr)
	}
	if e.GemergtMit.Nummer != nr {
		t.Errorf("%s: gemergt_mit #%d, erwartet #%d", wo, e.GemergtMit.Nummer, nr)
	}
}

func TestSchliesszeitHeuteGegenGestern(t *testing.T) {
	zone := time.FixedZone("UTC+2", 2*3600)
	jetzt := time.Date(2026, 9, 28, 15, 0, 0, 0, zone)
	w := RepoArbeitBauen(ArbeitEingabe{Repo: "acme/alpha", Jetzt: jetzt, Geschlossen: []ghrest.Issue{
		zeZu(3, "2026-09-28T12:32:00Z"),
		zeZu(2, "2026-09-27T22:05:00Z"),
		zeZu(1, "2026-09-27T21:55:00Z"),
	}})
	z := zeZuletzt(t, w)
	if got := z[3].GeschlossenText; got != "heute 14:32" {
		t.Errorf("#3 geschlossen_text %q, erwartet %q", got, "heute 14:32")
	}
	if got := z[2].GeschlossenText; got != "heute 00:05" {
		t.Errorf("#2 geschlossen_text %q, erwartet %q (Tagesgrenze der Zone, nicht UTC)", got, "heute 00:05")
	}
	if got := z[1].GeschlossenText; got != "27. Sept." {
		t.Errorf("#1 geschlossen_text %q, erwartet %q", got, "27. Sept.")
	}
	if got := z[1].GeschlossenVoll; got != "27.09.2026 23:55" {
		t.Errorf("#1 geschlossen_voll %q, erwartet %q", got, "27.09.2026 23:55")
	}
	if got := z[3].GeschlossenVoll; got != "28.09.2026 14:32" {
		t.Errorf("#3 geschlossen_voll %q, erwartet %q", got, "28.09.2026 14:32")
	}
}

func TestSchliesszeitUnlesbar(t *testing.T) {
	text, voll := ZeitpunktText("", time.Now())
	if text != "" || voll != "" {
		t.Errorf("leeres closed_at ergab %q / %q", text, voll)
	}
}

func TestGemergtMitRegel1Commit(t *testing.T) {
	pr21 := zeGemergt(21, "zwanzig: fertig", "irgendwas", "2026-09-25T10:00:00Z")
	w := RepoArbeitBauen(ArbeitEingabe{Repo: "acme/alpha", Geschlossen: []ghrest.Issue{zeZu(20, "2026-09-25T10:00:05Z")},
		Schluesse: map[int]IssueSchluss{20: {
			Ereignisse: []ghrest.TimelineEreignis{{Ereignis: "closed", CommitID: "abc", Zeit: "2026-09-25T10:00:05Z"}},
			CommitPRs:  map[string][]ghrest.PR{"abc": {pr21}},
		}}})
	e := zeZuletzt(t, w)[20]
	zeMitNummer(t, "#20", e, 21)
	if g := e.GemergtMit; g != nil && (g.Titel != "zwanzig: fertig" || g.URL != pr21.URL || g.GemergtAm != "2026-09-25T10:00:00Z") {
		t.Errorf("#20 gemergt_mit %+v", *g)
	}
}

func TestGemergtMitRegel1Querverweis(t *testing.T) {
	mit := zeGemergt(31, "dreissig", "x", "2026-09-25T10:00:00Z")
	mit.Text = "Setzt um.\n\nCloses #30"
	ohne := zeGemergt(32, "erwaehnt #30", "y", "2026-09-26T10:00:00Z")
	ohne.Text = "Siehe #30."
	w := RepoArbeitBauen(ArbeitEingabe{Repo: "acme/alpha", Geschlossen: []ghrest.Issue{zeZu(30, "2026-09-26T11:00:00Z")},
		Schluesse: map[int]IssueSchluss{30: {Ereignisse: []ghrest.TimelineEreignis{
			{Ereignis: "cross-referenced", Zeit: "2026-09-25T09:00:00Z", Quelle: &mit, QuelleRepo: "acme/alpha"},
			{Ereignis: "cross-referenced", Zeit: "2026-09-26T09:00:00Z", Quelle: &ohne, QuelleRepo: "acme/alpha"},
			{Ereignis: "closed", Zeit: "2026-09-26T11:00:00Z"},
		}}}})
	zeMitNummer(t, "#30", zeZuletzt(t, w)[30], 31)
}

func TestGemergtMitRegel1FremdesRepoZaehltNicht(t *testing.T) {
	fremd := zeGemergt(33, "fremd", "x", "2026-09-25T10:00:00Z")
	fremd.Text = "Closes #34"
	w := RepoArbeitBauen(ArbeitEingabe{Repo: "acme/alpha", Geschlossen: []ghrest.Issue{zeZu(34, "2026-09-26T11:00:00Z")},
		Schluesse: map[int]IssueSchluss{34: {Ereignisse: []ghrest.TimelineEreignis{
			{Ereignis: "cross-referenced", Quelle: &fremd, QuelleRepo: "ich/anderes"},
		}}}})
	zeMitNummer(t, "#34", zeZuletzt(t, w)[34], 0)
}

func TestGemergtMitRegel2Branch(t *testing.T) {
	w := RepoArbeitBauen(ArbeitEingabe{Repo: "acme/alpha",
		Geschlossen:     []ghrest.Issue{zeZu(40, "2026-09-26T11:00:00Z")},
		Changes:         []collectors.OpenspecChange{zeArchiv("vierzig", 40)},
		GeschlossenePRs: []ghrest.PR{zeGemergt(41, "andere Sache", "change/vierzig", "2026-09-26T10:00:00Z")},
		Schluesse:       map[int]IssueSchluss{40: {Ereignisse: []ghrest.TimelineEreignis{{Ereignis: "closed"}}}},
	})
	zeMitNummer(t, "#40", zeZuletzt(t, w)[40], 41)
}

func TestGemergtMitRegel3Titel(t *testing.T) {
	i := zeZu(50, "2026-09-25T11:00:00Z")
	i.Erstellt = "2026-09-20T08:00:00Z"
	w := RepoArbeitBauen(ArbeitEingabe{Repo: "acme/alpha", Geschlossen: []ghrest.Issue{i},
		GeschlossenePRs: []ghrest.PR{
			zeGemergt(49, "alt (#50)", "a", "2026-09-19T10:00:00Z"),
			zeGemergt(51, "neu (#50)", "b", "2026-09-22T10:00:00Z"),
			zeGemergt(52, "spaeter (#50)", "c", "2026-09-23T10:00:00Z"),
			zeGemergt(53, "(#50) vorne", "d", "2026-09-24T10:00:00Z"),
		}})
	zeMitNummer(t, "#50", zeZuletzt(t, w)[50], 52)
}

func TestGemergtMitRangfolge(t *testing.T) {
	pr61 := zeGemergt(61, "sechzig", "claude/xyz", "2026-09-24T10:00:00Z")
	w := RepoArbeitBauen(ArbeitEingabe{Repo: "acme/alpha",
		Geschlossen: []ghrest.Issue{zeZu(70, "2026-09-26T12:00:00Z"), zeZu(60, "2026-09-26T11:00:00Z")},
		Changes:     []collectors.OpenspecChange{zeArchiv("sechzig", 60), zeArchiv("siebzig", 70)},
		GeschlossenePRs: []ghrest.PR{
			zeGemergt(62, "zweiter", "change/sechzig", "2026-09-25T10:00:00Z"),
			zeGemergt(63, "x (#60)", "e", "2026-09-26T10:00:00Z"),
			zeGemergt(71, "dritter", "change/siebzig", "2026-09-24T10:00:00Z"),
			zeGemergt(72, "y (#70)", "f", "2026-09-26T10:00:00Z"),
		},
		Schluesse: map[int]IssueSchluss{
			60: {Ereignisse: []ghrest.TimelineEreignis{{Ereignis: "closed", CommitID: "s60"}},
				CommitPRs: map[string][]ghrest.PR{"s60": {pr61}}},
			70: {Ereignisse: []ghrest.TimelineEreignis{{Ereignis: "closed"}}},
		}})
	z := zeZuletzt(t, w)
	zeMitNummer(t, "#60", z[60], 61)
	zeMitNummer(t, "#70", z[70], 71)
}

func TestGemergtMitKeinTreffer(t *testing.T) {
	quer := ghrest.PR{Nummer: 81, Titel: "offen", Text: "Closes #80", Zustand: "open"}
	w := RepoArbeitBauen(ArbeitEingabe{Repo: "acme/alpha",
		Geschlossen: []ghrest.Issue{zeZu(80, "2026-09-26T11:00:00Z")},
		Changes:     []collectors.OpenspecChange{zeArchiv("achtzig", 80)},
		GeschlossenePRs: []ghrest.PR{{Nummer: 82, Titel: "z (#80)", Head: "change/achtzig", Zustand: "closed",
			GeschlossenAm: "2026-09-26T10:00:00Z"}},
		Schluesse: map[int]IssueSchluss{80: {Ereignisse: []ghrest.TimelineEreignis{
			{Ereignis: "cross-referenced", Quelle: &quer, QuelleRepo: "acme/alpha"},
			{Ereignis: "closed"},
		}}}})
	zeMitNummer(t, "#80", zeZuletzt(t, w)[80], 0)
}

func TestZuletztAuswahlHoechstensFuenf(t *testing.T) {
	var zu []ghrest.Issue
	for nr := 15; nr >= 10; nr-- {
		zu = append(zu, zeZu(nr, "2026-09-2"+itoa(nr-10)+"T10:00:00Z"))
	}
	zu = append([]ghrest.Issue{{Nummer: 99, IstPR: true}}, zu...)
	a := ZuletztAuswahl(zu)
	if len(a) != 5 || a[0].Nummer != 15 || a[4].Nummer != 11 {
		t.Errorf("Auswahl %+v, erwartet 15..11 ohne PR", a)
	}
}
