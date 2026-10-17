package appctx

// Einordnung der Remote-Branches (branch-aufraeumen, #78): jede Gruppe,
// Vorrang, Angaben je Branch, loeschbar mit Grund, Reihenfolge und Zahl.

import (
	"strings"
	"testing"

	"github.com/niclasedge/git-planner-go/internal/collectors"
	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

func aufraeumEingabe() AufraeumEingabe {
	return AufraeumEingabe{
		Repo:     "acme/alpha",
		Standard: "main",
		Branches: []ghrest.Branch{
			{Name: "main", SHA: "sha-main"},
			{Name: "schutz", SHA: "sha-schutz", Geschuetzt: true},
			{Name: "change/offen", SHA: "sha-offen"},
			{Name: "alt", SHA: "sha-alt"},
			{Name: "squash", SHA: "sha-squash"},
			{Name: "weg", SHA: "abcdef1234"},
			{Name: "neu", SHA: "sha-neu"},
		},
		PRs: []ghrest.PR{
			{Nummer: 3, Titel: "Offen", Head: "change/offen", HeadRepo: "acme/alpha", Zustand: "open"},
			{Nummer: 5, Titel: "Squash", Head: "squash", HeadRepo: "acme/alpha", Zustand: "closed", Gemergt: true, URL: "u5"},
			{Nummer: 7, Titel: "Versuch", Head: "weg", HeadRepo: "acme/alpha", Zustand: "closed", Text: "Closes #12", URL: "u7"},
			// Ein Fork mit gleichem Branch-Namen zaehlt nicht.
			{Nummer: 8, Titel: "Fork", Head: "neu", HeadRepo: "ich/alpha", Zustand: "open"},
		},
		Koepfe: map[string]AufraeumKopf{
			"schutz":       {Zeit: "2026-09-01T10:00:00Z", Betreff: "Schutz", Voraus: 0, Verglichen: true},
			"change/offen": {Zeit: "2026-09-25T10:00:00Z", Betreff: "Offen", Voraus: 0, Verglichen: true},
			"alt":          {Zeit: "2026-08-01T10:00:00Z", Betreff: "Alt\n\nText", Voraus: 0, Zurueck: 30, Verglichen: true},
			"squash":       {Zeit: "2026-09-10T10:00:00Z", Betreff: "Squash", Voraus: 3, Zurueck: 2, Verglichen: true},
			"weg":          {Zeit: "2026-09-20T10:00:00Z", Betreff: "Versuch", Voraus: 2, Zurueck: 5, Verglichen: true},
			"neu":          {Zeit: "2026-09-24T10:00:00Z", Betreff: "Neu", Voraus: 1, Verglichen: true},
		},
		Offen: []ghrest.Issue{{Nummer: 12, Titel: "Zwoelf"}, {Nummer: 13, Titel: "Dreizehn"}},
	}
}

func gruppenNamen(a BranchAufraeumen) map[string][]string {
	out := map[string][]string{}
	for _, g := range a.Gruppen {
		for _, b := range g.Branches {
			out[g.Name] = append(out[g.Name], b.Name)
		}
	}
	return out
}

func TestBranchAufraeumenVierGruppen(t *testing.T) {
	a := BranchAufraeumenBauen(aufraeumEingabe())
	if len(a.Gruppen) != 4 {
		t.Fatalf("Gruppen %+v", a.Gruppen)
	}
	folge := []string{}
	for _, g := range a.Gruppen {
		folge = append(folge, g.Name)
	}
	if strings.Join(folge, ",") != "gemergt,verlassen,aktiv,geschuetzt" {
		t.Errorf("Folge %v", folge)
	}
	n := gruppenNamen(a)
	// Innerhalb der Gruppe neueste zuerst; main ohne Zeit am Ende.
	if strings.Join(n["gemergt"], ",") != "squash,alt" {
		t.Errorf("gemergt %v", n["gemergt"])
	}
	if strings.Join(n["verlassen"], ",") != "weg" || strings.Join(n["aktiv"], ",") != "neu" {
		t.Errorf("verlassen %v, aktiv %v", n["verlassen"], n["aktiv"])
	}
	if strings.Join(n["geschuetzt"], ",") != "change/offen,schutz,main" {
		t.Errorf("geschuetzt %v", n["geschuetzt"])
	}
	if a.Anzahl != 4 {
		t.Errorf("Anzahl %d, erwartet 4", a.Anzahl)
	}
	if a.Repo != "acme/alpha" || a.Standard != "main" {
		t.Errorf("Kopf %q %q", a.Repo, a.Standard)
	}
}

func TestBranchAufraeumenLeereGruppen(t *testing.T) {
	a := BranchAufraeumenBauen(AufraeumEingabe{Repo: "acme/alpha", Standard: "main", Branches: []ghrest.Branch{{Name: "main", SHA: "s"}}})
	if len(a.Gruppen) != 4 {
		t.Fatalf("Gruppen %+v", a.Gruppen)
	}
	for _, g := range a.Gruppen {
		if g.Branches == nil {
			t.Errorf("%s: nil statt leerer Liste", g.Name)
		}
	}
	if a.Anzahl != 0 {
		t.Errorf("Anzahl %d", a.Anzahl)
	}
}

func TestBranchAufraeumenGeschuetztVorGemergt(t *testing.T) {
	a := BranchAufraeumenBauen(aufraeumEingabe())
	b, ok := a.Branch("change/offen")
	if !ok || b.Gruppe != GruppeGeschuetzt || b.Loeschbar {
		t.Fatalf("change/offen %+v", b)
	}
	if !strings.Contains(b.Grund, "#3") {
		t.Errorf("Grund %q nennt den offenen PR nicht", b.Grund)
	}
	if b.PR == nil || b.PR.Nummer != 3 || b.PR.Zustand != "offen" {
		t.Errorf("PR %+v", b.PR)
	}
	for _, name := range []string{"main", "schutz"} {
		b, _ := a.Branch(name)
		if b.Gruppe != GruppeGeschuetzt || b.Loeschbar || b.Grund == "" {
			t.Errorf("%s %+v", name, b)
		}
	}
	// Der Default-Branch heisst hier nicht main.
	e := AufraeumEingabe{Repo: "acme/alpha", Standard: "trunk", Branches: []ghrest.Branch{{Name: "trunk", SHA: "t"}, {Name: "master", SHA: "m"}},
		Koepfe: map[string]AufraeumKopf{"master": {Verglichen: true}}}
	a = BranchAufraeumenBauen(e)
	for _, name := range []string{"trunk", "master"} {
		if b, _ := a.Branch(name); b.Gruppe != GruppeGeschuetzt {
			t.Errorf("%s in %q", name, b.Gruppe)
		}
	}
}

func TestBranchAufraeumenGemergt(t *testing.T) {
	a := BranchAufraeumenBauen(aufraeumEingabe())
	alt, _ := a.Branch("alt")
	if alt.Gruppe != GruppeGemergt || !alt.Loeschbar || !strings.Contains(alt.Grund, "main") {
		t.Errorf("alt %+v", alt)
	}
	if alt.Betreff != "Alt" || alt.Zurueck != 30 || alt.PR != nil {
		t.Errorf("alt Angaben %+v", alt)
	}
	sq, _ := a.Branch("squash")
	if sq.Gruppe != GruppeGemergt || !sq.Loeschbar || !strings.Contains(sq.Grund, "#5") {
		t.Errorf("squash %+v", sq)
	}
	if sq.PR == nil || sq.PR.Zustand != "gemergt" || sq.PR.URL != "u5" {
		t.Errorf("squash PR %+v", sq.PR)
	}
}

func TestBranchAufraeumenAngabenVerlassen(t *testing.T) {
	a := BranchAufraeumenBauen(aufraeumEingabe())
	b, ok := a.Branch("weg")
	if !ok {
		t.Fatal("weg fehlt")
	}
	if b.Gruppe != GruppeVerlassen || b.SHA != "abcdef1234" || b.KurzSHA != "abcdef1" || b.Voraus != 2 || b.Zurueck != 5 ||
		b.Zeit != "2026-09-20T10:00:00Z" || b.Betreff != "Versuch" || !b.Verglichen {
		t.Errorf("weg %+v", b)
	}
	if b.PR == nil || b.PR.Nummer != 7 || b.PR.Zustand != "geschlossen" || b.PR.Titel != "Versuch" {
		t.Errorf("PR %+v", b.PR)
	}
	if len(b.Issues) != 1 || b.Issues[0].Nummer != 12 || b.Issues[0].Titel != "Zwoelf" || strings.Join(b.Issues[0].Gruende, ",") != BranchGrundPR {
		t.Errorf("Issues %+v", b.Issues)
	}
	if !b.Loeschbar || !strings.Contains(b.Grund, "2 eigene Commits") {
		t.Errorf("loeschbar %v, Grund %q", b.Loeschbar, b.Grund)
	}
}

func TestBranchAufraeumenAktiv(t *testing.T) {
	a := BranchAufraeumenBauen(aufraeumEingabe())
	b, _ := a.Branch("neu")
	if b.Gruppe != GruppeAktiv || b.PR != nil || !b.Loeschbar || !strings.Contains(b.Grund, "1 eigener Commit") {
		t.Errorf("neu %+v", b)
	}
}

func TestBranchAufraeumenIssuesUeberNameUndChange(t *testing.T) {
	e := aufraeumEingabe()
	e.Branches = append(e.Branches, ghrest.Branch{Name: "13-fix", SHA: "s13"}, ghrest.Branch{Name: "change/karte", SHA: "sk"})
	e.Koepfe["13-fix"] = AufraeumKopf{Voraus: 1, Verglichen: true}
	e.Koepfe["change/karte"] = AufraeumKopf{Voraus: 1, Verglichen: true}
	e.Changes = []collectors.OpenspecChange{{Kennung: "karte", Issue: 12}}
	a := BranchAufraeumenBauen(e)
	b, _ := a.Branch("13-fix")
	if len(b.Issues) != 1 || b.Issues[0].Nummer != 13 || strings.Join(b.Issues[0].Gruende, ",") != BranchGrundName {
		t.Errorf("13-fix Issues %+v", b.Issues)
	}
	c, _ := a.Branch("change/karte")
	if len(c.Issues) != 1 || c.Issues[0].Nummer != 12 || strings.Join(c.Issues[0].Gruende, ",") != BranchGrundChange {
		t.Errorf("change/karte Issues %+v", c.Issues)
	}
}

func TestBranchAufraeumenOhneVergleich(t *testing.T) {
	e := aufraeumEingabe()
	delete(e.Koepfe, "neu")
	a := BranchAufraeumenBauen(e)
	b, _ := a.Branch("neu")
	if b.Gruppe != GruppeAktiv || b.Loeschbar || b.Verglichen || b.Grund == "" {
		t.Errorf("neu ohne Vergleich %+v", b)
	}
	if a.Anzahl != 3 {
		t.Errorf("Anzahl %d, erwartet 3", a.Anzahl)
	}
	// Ein gemergter PR genuegt auch ohne Vergleich.
	delete(e.Koepfe, "squash")
	a = BranchAufraeumenBauen(e)
	if sq, _ := a.Branch("squash"); sq.Gruppe != GruppeGemergt || !sq.Loeschbar {
		t.Errorf("squash ohne Vergleich %+v", sq)
	}
}

func TestBranchAufraeumenPRVorrang(t *testing.T) {
	e := AufraeumEingabe{Repo: "acme/alpha", Standard: "main",
		Branches: []ghrest.Branch{{Name: "main", SHA: "m"}, {Name: "x", SHA: "sx"}},
		PRs: []ghrest.PR{
			{Nummer: 1, Head: "x", HeadRepo: "acme/alpha", Zustand: "closed", Gemergt: true},
			{Nummer: 4, Head: "x", HeadRepo: "acme/alpha", Zustand: "closed"},
			{Nummer: 2, Head: "x", HeadRepo: "acme/alpha", Zustand: "closed", Gemergt: true},
		},
		Koepfe: map[string]AufraeumKopf{"x": {Voraus: 1, Verglichen: true}}}
	b, _ := BranchAufraeumenBauen(e).Branch("x")
	if b.Gruppe != GruppeGemergt || b.PR == nil || b.PR.Nummer != 2 {
		t.Errorf("x %+v PR %+v, erwartet gemergt ueber #2", b, b.PR)
	}
}
