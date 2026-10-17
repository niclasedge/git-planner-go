package appctx

// Der Lebenslauf je Issue und PR (pr-lebenslauf, #83): acht Stufen mit
// Zustand, Grund und Zeit, dazu die Nachsorge nach dem Merge. Abgeleitet und
// hier geprueft ohne Browser.

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/niclasedge/git-planner-go/internal/collectors"
	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

// llZustaende schreibt je Stufe "name=zustand".
func llZustaende(l Lebenslauf) string {
	teile := []string{}
	for _, s := range l.Stufen {
		teile = append(teile, s.Name+"="+s.Zustand)
	}
	return strings.Join(teile, " ")
}

func llStufe(t *testing.T, l Lebenslauf, name string) LebenslaufStufe {
	t.Helper()
	for _, s := range l.Stufen {
		if s.Name == name {
			return s
		}
	}
	t.Fatalf("Stufe %s fehlt: %s", name, llZustaende(l))
	return LebenslaufStufe{}
}

func llChecks(gesamt string, rot int) *ghrest.PRChecks {
	c := &ghrest.PRChecks{Gesamt: gesamt, Pruefungen: []ghrest.Pruefung{}, Ende: "2026-09-21T10:00:00Z"}
	c.Zusammenfassung.Rot = rot
	c.Zahl = rot
	return c
}

func llReviews(entscheidung string, stimmen ...ghrest.ReviewStimme) *ghrest.PRReviews {
	return &ghrest.PRReviews{Entscheidung: entscheidung, Stimmen: stimmen, Angefordert: []string{}}
}

// llOffenerPR ist PR #8 auf change/fuenf: kein Entwurf, gruen, genehmigt,
// sauber — die Faelle aendern je ein Feld.
func llOffenerPR() *ghrest.PR {
	return &ghrest.PR{Nummer: 8, Titel: "fuenf", Head: "change/fuenf", HeadRepo: "acme/alpha", Zustand: "open",
		Erstellt: "2026-09-21T09:00:00Z", Abgleich: "sauber", Checks: llChecks("gruen", 0),
		Reviews: llReviews("genehmigt", ghrest.ReviewStimme{Login: "ana", Zustand: "genehmigt", Zeit: "2026-09-22T09:00:00Z"})}
}

func llEingabe(pr *ghrest.PR) LebenslaufEingabe {
	return LebenslaufEingabe{Repo: "acme/alpha", Issue: 5, IssueOffen: true, Branch: "change/fuenf",
		BranchZeit: "2026-09-20T08:00:00Z", BranchDa: true, PR: pr}
}

func TestLebenslaufOhneBranch(t *testing.T) {
	l := LebenslaufBauen(LebenslaufEingabe{Repo: "acme/alpha", Issue: 4, IssueOffen: true})
	if l.Stufe != StufeBranch {
		t.Errorf("stufe %q", l.Stufe)
	}
	want := "branch=jetzt pr=offen pruefung=offen review=offen konflikt=offen merge=offen aufraeumen=offen fertig=offen"
	if got := llZustaende(l); got != want {
		t.Errorf("Zustaende\n got %s\nwant %s", got, want)
	}
	if b := llStufe(t, l, StufeBranch); b.Grund != "Noch kein Branch" || b.Titel != "Branch" {
		t.Errorf("branch %+v", b)
	}
	if len(l.Nachsorge) != 0 {
		t.Errorf("Nachsorge %+v", l.Nachsorge)
	}
	namen := []string{}
	for _, s := range l.Stufen {
		namen = append(namen, s.Name)
	}
	if strings.Join(namen, ",") != "branch,pr,pruefung,review,konflikt,merge,aufraeumen,fertig" {
		t.Errorf("Folge %v", namen)
	}

	l = LebenslaufBauen(LebenslaufEingabe{Repo: "acme/alpha", Issue: 4, IssueOffen: true, Blocker: []int{3}})
	if b := llStufe(t, l, StufeBranch); b.Zustand != StufeBlockiert || b.Grund != "Blockiert durch #3" {
		t.Errorf("blockiert %+v", b)
	}
	if l.Stufe != StufeBranch {
		t.Errorf("stufe %q", l.Stufe)
	}
}

func TestLebenslaufDraftPR(t *testing.T) {
	pr := llOffenerPR()
	pr.Draft = true
	pr.Reviews = llReviews("offen")
	l := LebenslaufBauen(llEingabe(pr))
	want := "branch=erledigt pr=jetzt pruefung=erledigt review=offen konflikt=erledigt merge=offen aufraeumen=offen fertig=offen"
	if got := llZustaende(l); got != want {
		t.Errorf("Zustaende\n got %s\nwant %s", got, want)
	}
	if b := llStufe(t, l, StufeBranch); b.Zeit != "2026-09-20T08:00:00Z" {
		t.Errorf("branch %+v", b)
	}
	if p := llStufe(t, l, StufePR); p.Grund != "PR #8 ist ein Entwurf" || p.Zeit != "" {
		t.Errorf("pr %+v", p)
	}
	if p := llStufe(t, l, StufePruefung); p.Zeit != "2026-09-21T10:00:00Z" {
		t.Errorf("pruefung %+v", p)
	}
	if r := llStufe(t, l, StufeReview); r.Grund != "Review ausstehend" {
		t.Errorf("review %+v", r)
	}
	if l.Stufe != StufePR || len(l.Nachsorge) != 0 {
		t.Errorf("stufe %q, Nachsorge %+v", l.Stufe, l.Nachsorge)
	}
}

func TestLebenslaufRotePruefungen(t *testing.T) {
	pr := llOffenerPR()
	pr.Checks = llChecks("rot", 2)
	pr.Reviews = llReviews("offen")
	l := LebenslaufBauen(llEingabe(pr))
	if p := llStufe(t, l, StufePR); p.Zustand != StufeErledigt || p.Zeit != "2026-09-21T09:00:00Z" {
		t.Errorf("pr %+v", p)
	}
	if p := llStufe(t, l, StufePruefung); p.Zustand != StufeBlockiert || p.Grund != "2 Prüfungen rot" || p.Zeit != "" {
		t.Errorf("pruefung %+v", p)
	}
	if r := llStufe(t, l, StufeReview); r.Zustand != StufeOffen {
		t.Errorf("review %+v", r)
	}
	if l.Stufe != StufePruefung {
		t.Errorf("stufe %q", l.Stufe)
	}

	pr.Checks = llChecks("laeuft", 0)
	pr.Checks.Zusammenfassung.Laeuft = 1
	l = LebenslaufBauen(llEingabe(pr))
	if p := llStufe(t, l, StufePruefung); p.Zustand != StufeJetzt || p.Grund != "1 Prüfung läuft" {
		t.Errorf("laeuft %+v", p)
	}
	pr.Checks = &ghrest.PRChecks{Gesamt: "keine", Pruefungen: []ghrest.Pruefung{}}
	l = LebenslaufBauen(llEingabe(pr))
	if p := llStufe(t, l, StufePruefung); p.Zustand != StufeErledigt || p.Grund != "Keine Prüfungen" {
		t.Errorf("keine %+v", p)
	}
}

func TestLebenslaufAenderungenVerlangt(t *testing.T) {
	pr := llOffenerPR()
	pr.Reviews = llReviews("aenderungen", ghrest.ReviewStimme{Login: "ana", Zustand: "genehmigt"},
		ghrest.ReviewStimme{Login: "bo", Zustand: "aenderungen"})
	l := LebenslaufBauen(llEingabe(pr))
	if p := llStufe(t, l, StufePruefung); p.Zustand != StufeErledigt {
		t.Errorf("pruefung %+v", p)
	}
	if r := llStufe(t, l, StufeReview); r.Zustand != StufeBlockiert || r.Grund != "Änderungen verlangt von bo" {
		t.Errorf("review %+v", r)
	}
	if l.Stufe != StufeReview {
		t.Errorf("stufe %q", l.Stufe)
	}
	// Genehmigt: die Zeit ist die letzte Genehmigung.
	l = LebenslaufBauen(llEingabe(llOffenerPR()))
	if r := llStufe(t, l, StufeReview); r.Zustand != StufeErledigt || r.Zeit != "2026-09-22T09:00:00Z" {
		t.Errorf("genehmigt %+v", r)
	}
}

func TestLebenslaufKonflikt(t *testing.T) {
	pr := llOffenerPR()
	pr.Abgleich = "konflikt"
	pr.KonfliktDateien = []string{"a.go", "b.go"}
	l := LebenslaufBauen(llEingabe(pr))
	want := "branch=erledigt pr=erledigt pruefung=erledigt review=erledigt konflikt=blockiert merge=offen aufraeumen=offen fertig=offen"
	if got := llZustaende(l); got != want {
		t.Errorf("Zustaende\n got %s\nwant %s", got, want)
	}
	if k := llStufe(t, l, StufeKonflikt); k.Grund != "Konflikt in 2 Dateien" || k.Zeit != "" {
		t.Errorf("konflikt %+v", k)
	}
	if l.Stufe != StufeKonflikt {
		t.Errorf("stufe %q", l.Stufe)
	}

	pr.Abgleich = "unbekannt"
	l = LebenslaufBauen(llEingabe(pr))
	if k := llStufe(t, l, StufeKonflikt); k.Zustand != StufeJetzt || k.Grund != "Wird geprüft" {
		t.Errorf("unbekannt %+v", k)
	}
	pr.Abgleich = "veraltet"
	l = LebenslaufBauen(llEingabe(pr))
	if k := llStufe(t, l, StufeKonflikt); k.Zustand != StufeErledigt {
		t.Errorf("veraltet %+v", k)
	}
}

func TestLebenslaufBereitZumMergen(t *testing.T) {
	pr := llOffenerPR()
	l := LebenslaufBauen(llEingabe(pr))
	if m := llStufe(t, l, StufeMerge); m.Zustand != StufeJetzt || m.Grund != "Bereit zum Mergen" {
		t.Errorf("merge %+v", m)
	}
	pr.AutoMerge = &ghrest.AutoMerge{Methode: "squash"}
	l = LebenslaufBauen(llEingabe(pr))
	if m := llStufe(t, l, StufeMerge); m.Zustand != StufeJetzt || m.Grund != "Auto-Merge eingeschaltet" {
		t.Errorf("auto-merge %+v", m)
	}
	if l.Stufe != StufeMerge {
		t.Errorf("stufe %q", l.Stufe)
	}
}

// llGemergt ist PR #7 auf change/fuenf, gemergt; Checks und Reviews sind
// in der Liste geschlossener PRs nicht geholt.
func llGemergt() *ghrest.PR {
	return &ghrest.PR{Nummer: 7, Titel: "fuenf", Head: "change/fuenf", HeadRepo: "acme/alpha", Zustand: "closed",
		Gemergt: true, GemergtAm: "2026-09-24T12:00:00Z", Erstellt: "2026-09-21T09:00:00Z"}
}

func TestLebenslaufGemergtMitNachsorge(t *testing.T) {
	e := llEingabe(llGemergt())
	e.Change = &ChangeKurz{Kennung: "fuenf"}
	l := LebenslaufBauen(e)
	want := "branch=erledigt pr=erledigt pruefung=erledigt review=erledigt konflikt=erledigt merge=erledigt aufraeumen=jetzt fertig=offen"
	if got := llZustaende(l); got != want {
		t.Errorf("Zustaende\n got %s\nwant %s", got, want)
	}
	if m := llStufe(t, l, StufeMerge); m.Zeit != "2026-09-24T12:00:00Z" {
		t.Errorf("merge %+v", m)
	}
	if a := llStufe(t, l, StufeAufraeumen); a.Grund != "Issue #5 schließen · Branch change/fuenf löschen · Change fuenf archivieren" {
		t.Errorf("aufraeumen %+v", a)
	}
	if l.Stufe != StufeAufraeumen {
		t.Errorf("stufe %q", l.Stufe)
	}
	if len(l.Nachsorge) != 3 {
		t.Fatalf("Nachsorge %+v", l.Nachsorge)
	}
	i, b, c := l.Nachsorge[0], l.Nachsorge[1], l.Nachsorge[2]
	if i.Art != NachsorgeIssue || i.Nummer != 5 || i.Erledigt || i.Text != "Issue #5 schließen" {
		t.Errorf("issue %+v", i)
	}
	if b.Art != NachsorgeBranch || b.Nummer != 7 || b.Branch != "change/fuenf" || b.Erledigt || b.Text != "Branch change/fuenf löschen" {
		t.Errorf("branch %+v", b)
	}
	if c.Art != NachsorgeChange || c.Befehl != "just archiv fuenf" || c.Erledigt || c.Text != "Change fuenf archivieren" {
		t.Errorf("change %+v", c)
	}

	// Ohne Change keine Archiv-Zeile; ein Head aus einem Fork keinen
	// Branch-Knopf.
	e.Change = nil
	e.PR.HeadRepo = "fremd/alpha"
	l = LebenslaufBauen(e)
	if len(l.Nachsorge) != 1 || l.Nachsorge[0].Art != NachsorgeIssue {
		t.Errorf("Fork ohne Change: %+v", l.Nachsorge)
	}
}

func TestLebenslaufAllesFertig(t *testing.T) {
	e := llEingabe(llGemergt())
	e.IssueOffen, e.IssueZu = false, "2026-09-25T08:00:00Z"
	e.BranchDa, e.BranchZeit = false, ""
	e.Change = &ChangeKurz{Kennung: "fuenf", Archiviert: true}
	l := LebenslaufBauen(e)
	for _, s := range l.Stufen {
		if s.Zustand != StufeErledigt {
			t.Errorf("%s ist %s (%s)", s.Name, s.Zustand, s.Grund)
		}
	}
	if l.Stufe != StufeFertig {
		t.Errorf("stufe %q", l.Stufe)
	}
	for _, n := range []string{StufeAufraeumen, StufeFertig} {
		if s := llStufe(t, l, n); s.Zeit != "2026-09-25T08:00:00Z" {
			t.Errorf("%s %+v", n, s)
		}
	}
	if len(l.Nachsorge) != 3 {
		t.Fatalf("Nachsorge %+v", l.Nachsorge)
	}
	for _, p := range l.Nachsorge {
		if !p.Erledigt {
			t.Errorf("%s nicht erledigt", p.Art)
		}
	}
}

func TestChecksEnde(t *testing.T) {
	jetzt := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	c := ChecksRechnen("s", []ghrest.CheckRun{
		{ID: 1, Name: "a", Status: "completed", Conclusion: "success", StartedAt: "2026-09-26T10:00:00Z", CompletedAt: "2026-09-26T10:05:00Z"},
		{ID: 2, Name: "b", Status: "completed", Conclusion: "success", StartedAt: "2026-09-26T10:00:00Z", CompletedAt: "2026-09-26T10:09:00Z"},
	}, []ghrest.CommitStatus{{Kontext: "c", Zustand: "success", Erstellt: "2026-09-26T10:00:00Z", Aktualisiert: "2026-09-26T10:07:00Z"}}, jetzt)
	if c.Ende != "2026-09-26T10:09:00Z" {
		t.Errorf("ende %q", c.Ende)
	}
	c = ChecksRechnen("s", []ghrest.CheckRun{{ID: 1, Name: "a", Status: "in_progress", StartedAt: "2026-09-26T10:00:00Z"}}, nil, jetzt)
	if c.Ende != "" {
		t.Errorf("laeuft: ende %q", c.Ende)
	}
}

func TestLebenslaufInDerArbeitsliste(t *testing.T) {
	e := ArbeitEingabe{
		Repo:     "acme/alpha",
		Standard: "main",
		Jetzt:    branchJetzt,
		Offen: []ghrest.Issue{
			{Nummer: 4, Titel: "vier"},
			{Nummer: 5, Titel: "fuenf"},
			{Nummer: 6, Titel: "sechs"},
		},
		Geschlossen: []ghrest.Issue{{Nummer: 2, Titel: "zwei", Geschlossen: "2026-09-23T08:00:00Z"}},
		Changes:     []collectors.OpenspecChange{{Kennung: "fuenf", Issue: 5, Ref: "change/fuenf", RemoteBranch: true}},
		Branches:    []ghrest.Branch{zweig("main"), zweig("change/fuenf"), zweig("sechs-6")},
		BranchKoepfe: map[string]BranchKopf{
			"sechs-6": {Zeit: "2026-09-20T08:00:00Z", Betreff: "sechs"},
		},
		PRs: []ghrest.PR{{Nummer: 8, Titel: "sechs", Head: "sechs-6", HeadRepo: "acme/alpha", Draft: true, Zustand: "open",
			Text: "Closes #6", Erstellt: "2026-09-21T09:00:00Z", Abgleich: "sauber", Checks: llChecks("gruen", 0), Reviews: llReviews("offen")}},
		GeschlossenePRs: []ghrest.PR{
			{Nummer: 7, Titel: "fuenf", Head: "change/fuenf", HeadRepo: "acme/alpha", Zustand: "closed", Gemergt: true, GemergtAm: "2026-09-24T12:00:00Z"},
			{Nummer: 3, Titel: "verworfen", Head: "change/fuenf", HeadRepo: "acme/alpha", Zustand: "closed", Text: "Closes #4"},
			{Nummer: 1, Titel: "zwei (#2)", Head: "zwei", HeadRepo: "acme/alpha", Zustand: "closed", Gemergt: true, GemergtAm: "2026-09-22T12:00:00Z"},
		},
	}
	a := RepoArbeitBauen(e)

	vier := eintragNr(t, a, 4)
	if vier.Lebenslauf == nil || vier.Lebenslauf.Stufe != StufeBranch {
		t.Errorf("#4 (nur ein ohne Merge geschlossener PR) %+v", vier.Lebenslauf)
	}
	fuenf := eintragNr(t, a, 5)
	if fuenf.Lebenslauf == nil || fuenf.Lebenslauf.Stufe != StufeAufraeumen || len(fuenf.Lebenslauf.Nachsorge) != 3 {
		t.Fatalf("#5 %+v", fuenf.Lebenslauf)
	}
	for _, p := range fuenf.Lebenslauf.Nachsorge {
		if p.Erledigt {
			t.Errorf("#5 %s erledigt", p.Art)
		}
	}
	if b := fuenf.Lebenslauf.Nachsorge[1]; b.Nummer != 7 || b.Branch != "change/fuenf" {
		t.Errorf("#5 branch %+v", b)
	}
	sechs := eintragNr(t, a, 6)
	if sechs.Lebenslauf == nil || sechs.Lebenslauf.Stufe != StufePR {
		t.Errorf("#6 %+v", sechs.Lebenslauf)
	} else if b := llStufe(t, *sechs.Lebenslauf, StufeBranch); b.Zeit != "2026-09-20T08:00:00Z" {
		t.Errorf("#6 branch %+v", b)
	}
	if l := a.PRLaeufe[8]; l == nil || l.Stufe != StufePR {
		t.Errorf("pr_laeufe[8] %+v", l)
	}
	if len(a.Zuletzt) != 1 || a.Zuletzt[0].Lebenslauf == nil {
		t.Fatalf("zuletzt %+v", a.Zuletzt)
	}
	zwei := *a.Zuletzt[0].Lebenslauf
	if m := llStufe(t, zwei, StufeMerge); m.Zustand != StufeErledigt || m.Zeit != "2026-09-22T12:00:00Z" {
		t.Errorf("#2 merge %+v", m)
	}
	if zwei.Stufe != StufeFertig {
		t.Errorf("#2 stufe %q (%s)", zwei.Stufe, llZustaende(zwei))
	}

	roh, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	var j struct {
		Reihe []struct {
			Nummer     int `json:"nummer"`
			Lebenslauf struct {
				Stufe  string `json:"stufe"`
				Stufen []struct {
					Name    string `json:"name"`
					Zustand string `json:"zustand"`
				} `json:"stufen"`
				Nachsorge []struct {
					Art    string `json:"art"`
					Befehl string `json:"befehl"`
				} `json:"nachsorge"`
			} `json:"lebenslauf"`
		} `json:"reihe"`
		PRLaeufe map[string]struct {
			Stufe string `json:"stufe"`
		} `json:"pr_laeufe"`
	}
	if err := json.Unmarshal(roh, &j); err != nil {
		t.Fatal(err)
	}
	if j.PRLaeufe["8"].Stufe != "pr" {
		t.Errorf("JSON pr_laeufe %+v", j.PRLaeufe)
	}
	for _, r := range j.Reihe {
		if r.Nummer == 5 && (r.Lebenslauf.Stufe != "aufraeumen" || len(r.Lebenslauf.Stufen) != 8 ||
			r.Lebenslauf.Nachsorge[2].Art != "change_archivieren" || r.Lebenslauf.Nachsorge[2].Befehl != "just archiv fuenf") {
			t.Errorf("JSON #5 %+v", r.Lebenslauf)
		}
	}
}
