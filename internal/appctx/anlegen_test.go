package appctx

// Vorschlag fuer Branch und PR je Issue (pr-anlegen, #79): Name, Titel,
// Text und welche Aktion ansteht, ohne Browser geprueft.

import (
	"strings"
	"testing"

	"github.com/niclasedge/git-planner-go/internal/collectors"
	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

func anlegenEingabe() ArbeitEingabe {
	return ArbeitEingabe{
		Repo:     "acme/alpha",
		Standard: "main",
		Jetzt:    branchJetzt,
		Offen: []ghrest.Issue{
			{Nummer: 79, Titel: "pr-anlegen: Branch und PR anlegen"},
			{Nummer: 80, Titel: "Knopf für Übersicht & Größe"},
			{Nummer: 81, Titel: "Einundachtzig bauen"},
			{Nummer: 82, Titel: "Zweiundachtzig"},
		},
		Changes: []collectors.OpenspecChange{
			{Kennung: "pr-anlegen", Issue: 79},
			{Kennung: "einundachtzig", Issue: 81},
		},
		Branches: []ghrest.Branch{zweig("main"), zweig("change/einundachtzig"), zweig("82-zweiundachtzig")},
		PRs: []ghrest.PR{{Nummer: 90, Titel: "Zweiundachtzig (#82)", Head: "82-zweiundachtzig", HeadRepo: "acme/alpha",
			Base: "main", Zustand: "open"}},
	}
}

func TestAnlegenBranchVorschlagen(t *testing.T) {
	a := RepoArbeitBauen(anlegenEingabe())

	e79 := eintragNr(t, a, 79)
	if e79.Anlegen == nil {
		t.Fatal("#79 ohne Vorschlag")
	}
	if v := *e79.Anlegen; v.Aktion != "branch" || v.Branch != "change/pr-anlegen" || v.Base != "main" ||
		v.Titel != "pr-anlegen: Branch und PR anlegen (#79)" || !strings.HasPrefix(v.Text, "Closes #79") {
		t.Errorf("#79 %+v", v)
	}

	e80 := eintragNr(t, a, 80)
	if e80.Anlegen == nil {
		t.Fatal("#80 ohne Vorschlag")
	}
	if v := *e80.Anlegen; v.Aktion != "branch" || v.Branch != "80-knopf-fuer-uebersicht-groesse" ||
		v.Titel != "Knopf für Übersicht & Größe (#80)" || !strings.HasPrefix(v.Text, "Closes #80") {
		t.Errorf("#80 %+v", v)
	}
}

func TestAnlegenPRVorschlagenOderNichts(t *testing.T) {
	a := RepoArbeitBauen(anlegenEingabe())

	e81 := eintragNr(t, a, 81)
	if e81.Anlegen == nil {
		t.Fatal("#81 ohne Vorschlag")
	}
	if v := *e81.Anlegen; v.Aktion != "pr" || v.Branch != "change/einundachtzig" || v.Base != "main" ||
		v.Titel != "einundachtzig: Einundachtzig bauen (#81)" || !strings.HasPrefix(v.Text, "Closes #81") {
		t.Errorf("#81 %+v", v)
	}

	if e82 := eintragNr(t, a, 82); e82.Anlegen != nil {
		t.Errorf("#82 hat einen offenen PR und doch einen Vorschlag %+v", *e82.Anlegen)
	}
}

func TestAnlegenOhneStandardIstMain(t *testing.T) {
	e := anlegenEingabe()
	e.Standard = ""
	if v := eintragNr(t, RepoArbeitBauen(e), 79).Anlegen; v == nil || v.Base != "main" {
		t.Errorf("Base ohne Default-Branch: %+v", v)
	}
	e.Standard = "trunk"
	if v := eintragNr(t, RepoArbeitBauen(e), 79).Anlegen; v == nil || v.Base != "trunk" {
		t.Errorf("Base trunk: %+v", v)
	}
}

func TestTitelSlug(t *testing.T) {
	for _, fall := range []struct{ titel, slug string }{
		{"Knopf für Übersicht & Größe", "knopf-fuer-uebersicht-groesse"},
		{"  --Hallo,   Welt!--  ", "hallo-welt"},
		{"ÄÖÜ äöü ß", "aeoeue-aeoeue-ss"},
		{"???", "issue"},
		{"", "issue"},
	} {
		if got := TitelSlug(fall.titel); got != fall.slug {
			t.Errorf("TitelSlug(%q) = %q, erwartet %q", fall.titel, got, fall.slug)
		}
	}
	lang := TitelSlug("Ein sehr langer Titel, der weit ueber die Grenze von vierzig Zeichen hinausgeht")
	if len(lang) > 40 || strings.HasSuffix(lang, "-") || !strings.HasPrefix(lang, "ein-sehr-langer-titel") {
		t.Errorf("lang: %q (%d Zeichen)", lang, len(lang))
	}
}

func TestBranchUndTitelVorschlag(t *testing.T) {
	if got := BranchVorschlag(79, "egal", "pr-anlegen"); got != "change/pr-anlegen" {
		t.Errorf("mit Change: %q", got)
	}
	if got := BranchVorschlag(7, "Sieben Sachen", ""); got != "7-sieben-sachen" {
		t.Errorf("ohne Change: %q", got)
	}
	for _, fall := range []struct {
		nr             int
		titel, kennung string
		soll           string
	}{
		{79, "pr-anlegen: Branch und PR", "pr-anlegen", "pr-anlegen: Branch und PR (#79)"},
		{79, "Branch und PR", "pr-anlegen", "pr-anlegen: Branch und PR (#79)"},
		{79, "pr-anlegen: schon mit Nummer (#79)", "pr-anlegen", "pr-anlegen: schon mit Nummer (#79)"},
		{5, "Nur ein Titel", "", "Nur ein Titel (#5)"},
	} {
		if got := PRTitelVorschlag(fall.nr, fall.titel, fall.kennung); got != fall.soll {
			t.Errorf("PRTitelVorschlag(%d, %q, %q) = %q, erwartet %q", fall.nr, fall.titel, fall.kennung, got, fall.soll)
		}
	}
	if got := PRTextVorschlag(79, "pr-anlegen"); !strings.HasPrefix(got, "Closes #79") || !strings.Contains(got, "pr-anlegen") {
		t.Errorf("Text mit Change: %q", got)
	}
	if got := PRTextVorschlag(5, ""); !strings.HasPrefix(got, "Closes #5") {
		t.Errorf("Text ohne Change: %q", got)
	}
}

func TestGueltigerBranchName(t *testing.T) {
	for _, n := range []string{"change/pr-anlegen", "79-knopf", "feature/a_b.c"} {
		if !GueltigerBranchName(n) {
			t.Errorf("%q abgelehnt", n)
		}
	}
	for _, n := range []string{"", "a..b", "/a", "a/", "a b", "a.lock", "a~1", "a^", "a:b", "a?", "a*", "a[", "a\\b", "@{x", ".a", "a//b", "-a"} {
		if GueltigerBranchName(n) {
			t.Errorf("%q angenommen", n)
		}
	}
}
