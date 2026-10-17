package appctx

// Der letzte Commit ueber alle Branches (karte-einzeilig, #124): zuerst aus
// der Repo-Aktivitaet (juengster Push oder Merge auf refs/heads/), sonst aus
// den Branch-Koepfen; dazu Branch, PR aus den offenen PRs und die Texte nach
// ZeitpunktText. Gewaehlt wird hier, nicht im Browser.

import (
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

var lcJetzt = time.Date(2026, 9, 30, 15, 0, 0, 0, time.FixedZone("UTC+2", 2*3600))

const lcSHA = "3f9c2e1a0b1c2d3e4f5a6b7c8d9e0f1a2b3c4d5e"

func lcAkt(art, ref, zeit, sha string) ghrest.Aktivitaet {
	return ghrest.Aktivitaet{Art: art, Ref: ref, Zeit: zeit, Nachher: sha, Vorher: strings.Repeat("0", 40), Akteur: "actor-login"}
}

func lcFeature() ArbeitEingabe {
	return ArbeitEingabe{Repo: "acme/alpha", Jetzt: lcJetzt, Standard: "main",
		Aktivitaet: []ghrest.Aktivitaet{
			lcAkt("branch_deletion", "refs/heads/alt", "2026-09-30T12:40:00Z", strings.Repeat("0", 40)),
			lcAkt("push", "refs/tags/v1", "2026-09-30T12:35:00Z", strings.Repeat("b", 40)),
			lcAkt("push", "refs/heads/change/ci-runs", "2026-09-30T12:32:00Z", lcSHA),
			lcAkt("pr_merge", "refs/heads/main", "2026-09-30T11:00:00Z", strings.Repeat("c", 40)),
		},
		Commits: map[string]ghrest.CommitKopf{
			lcSHA: {SHA: lcSHA, Zeit: "2026-09-30T12:30:00Z", Autor: "alex",
				Betreff: "ci-runs: Log-Ansicht mit Zeilennummern, Suche und Sprung auf den ersten roten Schritt"},
			strings.Repeat("c", 40): {SHA: strings.Repeat("c", 40), Zeit: "2026-09-30T10:59:00Z", Autor: "alex", Betreff: "Merge"},
		},
		PRs: []ghrest.PR{
			{Nummer: 120, Titel: "anderes", URL: "https://github.com/acme/alpha/pull/120", Head: "change/anderes", Zustand: "open"},
			{Nummer: 123, Titel: "ci-runs", URL: "https://github.com/acme/alpha/pull/123", Head: "change/ci-runs", Zustand: "open"},
		},
	}
}

func TestLetzterCommitFeatureBranchMitPR(t *testing.T) {
	l := RepoArbeitBauen(lcFeature()).LetzterCommit
	if l == nil {
		t.Fatal("kein letzter_commit")
	}
	if l.Branch != "change/ci-runs" || l.Standard || l.SHA != lcSHA || l.Kurz != "3f9c2e1" {
		t.Errorf("branch %q standard %v sha %q kurz %q", l.Branch, l.Standard, l.SHA, l.Kurz)
	}
	if l.Art != "gepusht" || l.Zeit != "2026-09-30T12:32:00Z" || l.Text != "heute 14:32" || l.Voll != "30.09.2026 14:32" {
		t.Errorf("art %q zeit %q text %q voll %q", l.Art, l.Zeit, l.Text, l.Voll)
	}
	if l.CommitVoll != "30.09.2026 14:30" || l.Autor != "alex" {
		t.Errorf("commit_voll %q autor %q", l.CommitVoll, l.Autor)
	}
	if !strings.HasPrefix(l.Betreff, "ci-runs: Log-Ansicht mit") || utf8.RuneCountInString(l.BetreffKurz) > 60 ||
		!strings.HasSuffix(l.BetreffKurz, "…") || !strings.HasPrefix(l.BetreffKurz, "ci-runs: Log-Ansicht") {
		t.Errorf("betreff %q betreff_kurz %q (%d Zeichen)", l.Betreff, l.BetreffKurz, utf8.RuneCountInString(l.BetreffKurz))
	}
	if want := "gepusht 30.09.2026 14:32 · Commit 30.09.2026 14:30 · " + lcSHA; l.Titel != want {
		t.Errorf("titel %q, erwartet %q", l.Titel, want)
	}
	if l.PR == nil || l.PR.Nummer != 123 || l.PR.URL != "https://github.com/acme/alpha/pull/123" {
		t.Errorf("pr %+v, erwartet #123", l.PR)
	}
}

func TestLetzterCommitKurzerBetreffBleibt(t *testing.T) {
	e := lcFeature()
	k := e.Commits[lcSHA]
	k.Betreff = "kurz"
	e.Commits[lcSHA] = k
	if l := RepoArbeitBauen(e).LetzterCommit; l == nil || l.BetreffKurz != "kurz" {
		t.Errorf("letzter_commit %+v, erwartet betreff_kurz kurz", l)
	}
}

func TestLetzterCommitMergeAufMain(t *testing.T) {
	e := lcFeature()
	e.Aktivitaet = append([]ghrest.Aktivitaet{lcAkt("pr_merge", "refs/heads/main", "2026-09-30T12:50:00Z", strings.Repeat("c", 40))}, e.Aktivitaet...)
	l := RepoArbeitBauen(e).LetzterCommit
	if l == nil {
		t.Fatal("kein letzter_commit")
	}
	if l.Branch != "main" || !l.Standard || l.Art != "gemergt" || l.PR != nil {
		t.Errorf("branch %q standard %v art %q pr %+v, erwartet main, true, gemergt, ohne PR", l.Branch, l.Standard, l.Art, l.PR)
	}
	if !strings.HasPrefix(l.Titel, "gemergt 30.09.2026 14:50 · Commit 30.09.2026 12:59 · ") {
		t.Errorf("titel %q", l.Titel)
	}
}

func TestLetzterCommitNachZeitNichtNachFolge(t *testing.T) {
	// Die Aktivitaet kommt neueste zuerst; gewaehlt wird trotzdem nach
	// timestamp, nicht nach der Stelle in der Liste.
	e := lcFeature()
	e.Aktivitaet = append(e.Aktivitaet, lcAkt("force_push", "refs/heads/main", "2026-09-30T12:45:00Z", strings.Repeat("c", 40)))
	if l := RepoArbeitBauen(e).LetzterCommit; l == nil || l.Branch != "main" || l.Art != "gepusht" {
		t.Errorf("letzter_commit %+v, erwartet der force_push auf main um 12:45", l)
	}
}

func TestLetzterCommitAutorAusAkteur(t *testing.T) {
	e := lcFeature()
	k := e.Commits[lcSHA]
	k.Autor = ""
	e.Commits[lcSHA] = k
	if l := RepoArbeitBauen(e).LetzterCommit; l == nil || l.Autor != "actor-login" {
		t.Errorf("letzter_commit %+v, erwartet autor actor-login", l)
	}
}

func TestLetzterCommitOhneCommitFaelltWeg(t *testing.T) {
	e := lcFeature()
	delete(e.Commits, lcSHA)
	if l := RepoArbeitBauen(e).LetzterCommit; l != nil {
		t.Errorf("letzter_commit %+v, erwartet keins (Commit nicht abrufbar)", *l)
	}
}

func TestJuengsteAktivitaet(t *testing.T) {
	a, ok := JuengsteAktivitaet(lcFeature().Aktivitaet)
	if !ok || a.Nachher != lcSHA {
		t.Errorf("JuengsteAktivitaet %+v %v, erwartet der Push auf change/ci-runs", a, ok)
	}
	if _, ok := JuengsteAktivitaet([]ghrest.Aktivitaet{lcAkt("branch_creation", "refs/heads/x", "2026-09-30T12:00:00Z", lcSHA),
		lcAkt("push", "refs/tags/v2", "2026-09-30T12:00:00Z", lcSHA)}); ok {
		t.Errorf("JuengsteAktivitaet ohne Push oder Merge auf refs/heads/ liefert einen Eintrag")
	}
	for _, art := range []string{"push", "force_push", "pr_merge", "merge_queue_merge"} {
		if _, ok := JuengsteAktivitaet([]ghrest.Aktivitaet{lcAkt(art, "refs/heads/x", "2026-09-30T12:00:00Z", lcSHA)}); !ok {
			t.Errorf("%s nicht gewaehlt", art)
		}
	}
}

func TestLetzterCommitRueckfallBranchKoepfe(t *testing.T) {
	sm, sa := strings.Repeat("a", 40), strings.Repeat("d", 40)
	e := ArbeitEingabe{Repo: "acme/alpha", Jetzt: lcJetzt, Standard: "main",
		Branches: []ghrest.Branch{{Name: "12-a", SHA: sa}, {Name: "main", SHA: sm}},
		Commits: map[string]ghrest.CommitKopf{
			sm: {SHA: sm, Zeit: "2026-09-30T09:00:00Z", Betreff: "auf main", Autor: "alex"},
			sa: {SHA: sa, Zeit: "2026-09-30T10:00:00Z", Betreff: "auf 12-a", Autor: "alex"},
		},
		PRs: []ghrest.PR{{Nummer: 7, URL: "https://github.com/acme/alpha/pull/7", Head: "12-a", Zustand: "open"}},
	}
	for name, akt := range map[string][]ghrest.Aktivitaet{"nicht geholt": nil, "leer": {}} {
		e.Aktivitaet = akt
		l := RepoArbeitBauen(e).LetzterCommit
		if l == nil {
			t.Fatalf("%s: kein letzter_commit", name)
		}
		if l.Branch != "12-a" || l.Standard || l.Art != "Commit" || l.PR == nil || l.PR.Nummer != 7 {
			t.Errorf("%s: branch %q standard %v art %q pr %+v, erwartet 12-a, Commit, #7", name, l.Branch, l.Standard, l.Art, l.PR)
		}
		if l.Zeit != "2026-09-30T10:00:00Z" || l.Text != "heute 12:00" || l.Titel != "Commit 30.09.2026 12:00 · "+sa {
			t.Errorf("%s: zeit %q text %q titel %q", name, l.Zeit, l.Text, l.Titel)
		}
	}
}

func TestLetzterCommitRueckfallZeitAusBranchKopf(t *testing.T) {
	// Die Zeit eines verknuepften Branches kennt die Arbeitsliste schon; der
	// Kopf ohne Zeit zaehlt nicht mit.
	sm, sb, sc := strings.Repeat("a", 40), strings.Repeat("e", 40), strings.Repeat("f", 40)
	e := ArbeitEingabe{Repo: "acme/alpha", Jetzt: lcJetzt, Standard: "main",
		Offen:        []ghrest.Issue{{Nummer: 11, Titel: "elf", URL: "u", Zustand: "open"}},
		Branches:     []ghrest.Branch{{Name: "11-elf", SHA: sb}, {Name: "main", SHA: sm}, {Name: "ohne-zeit", SHA: sc}},
		BranchKoepfe: map[string]BranchKopf{"11-elf": {Zeit: "2026-09-29T08:00:00Z", Betreff: "elf"}},
		Commits:      map[string]ghrest.CommitKopf{sm: {SHA: sm, Zeit: "2026-09-28T08:00:00Z", Betreff: "main"}},
	}
	l := RepoArbeitBauen(e).LetzterCommit
	if l == nil || l.Branch != "11-elf" || l.Betreff != "elf" || l.Text != "29. Sept." {
		t.Errorf("letzter_commit %+v, erwartet 11-elf aus dem Branch-Kopf", l)
	}
}

func TestLetzterCommitGleichstandStandard(t *testing.T) {
	sm, sa := strings.Repeat("a", 40), strings.Repeat("d", 40)
	e := ArbeitEingabe{Repo: "acme/alpha", Jetzt: lcJetzt, Standard: "main",
		Branches: []ghrest.Branch{{Name: "12-a", SHA: sa}, {Name: "main", SHA: sm}},
		Commits: map[string]ghrest.CommitKopf{
			sm: {SHA: sm, Zeit: "2026-09-30T10:00:00Z"}, sa: {SHA: sa, Zeit: "2026-09-30T10:00:00Z"},
		},
	}
	if l := RepoArbeitBauen(e).LetzterCommit; l == nil || l.Branch != "main" || !l.Standard {
		t.Errorf("letzter_commit %+v, erwartet main bei Gleichstand", l)
	}
}

func TestLetzterCommitLeeresRepo(t *testing.T) {
	if l := RepoArbeitBauen(ArbeitEingabe{Repo: "acme/alpha", Jetzt: lcJetzt, Aktivitaet: []ghrest.Aktivitaet{}}).LetzterCommit; l != nil {
		t.Errorf("leeres Repo mit letzter_commit %+v", *l)
	}
}

func TestLetzterCommitPRHoechsteNummer(t *testing.T) {
	e := lcFeature()
	e.PRs = append(e.PRs, ghrest.PR{Nummer: 99, URL: "https://github.com/acme/alpha/pull/99", Head: "change/ci-runs", Zustand: "open"},
		ghrest.PR{Nummer: 130, URL: "https://github.com/acme/alpha/pull/130", Head: "change/ci-runs", Zustand: "open"})
	if l := RepoArbeitBauen(e).LetzterCommit; l == nil || l.PR == nil || l.PR.Nummer != 130 {
		t.Errorf("letzter_commit %+v, erwartet PR #130", l)
	}
}

func TestKoepfeFuerRueckfall(t *testing.T) {
	// Betrachtet werden die ersten 10 Branches ohne Zeit aus den
	// Branch-Koepfen, main zuerst; ob ein Kopf schon gespeichert ist, aendert
	// die Auswahl nicht, sonst holte jeder weitere Aufbau die naechsten 10.
	var branches []ghrest.Branch
	for i := 1; i <= 12; i++ {
		branches = append(branches, ghrest.Branch{Name: fmt.Sprintf("f%02d", i), SHA: fmt.Sprintf("%040d", i)})
	}
	branches = append(branches, ghrest.Branch{Name: "main", SHA: strings.Repeat("a", 40)}, ghrest.Branch{Name: "ohne-sha"})
	e := ArbeitEingabe{Repo: "acme/alpha", Standard: "main", Branches: branches,
		BranchKoepfe: map[string]BranchKopf{"f01": {Zeit: "2026-09-29T08:00:00Z"}},
		Commits:      map[string]ghrest.CommitKopf{fmt.Sprintf("%040d", 2): {Zeit: "2026-09-29T08:00:00Z"}},
	}
	var namen []string
	for _, b := range KoepfeFuerRueckfall(e) {
		namen = append(namen, b.Name)
	}
	if got := strings.Join(namen, ","); got != "main,f02,f03,f04,f05,f06,f07,f08,f09,f10" {
		t.Errorf("KoepfeFuerRueckfall %s, erwartet main zuerst, ohne f01 (Branch-Kopf), hoechstens 10", got)
	}
}
