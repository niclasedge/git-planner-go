package appctx

// "Letzte Aktionen" als Zeitstrom (letzte-aktionen, #126): Pushes und Merges
// aller Branches, CI-Runs, Bilder, HTML-Dateien, Belege und die geschlossenen
// Issues, neueste zuerst, aufeinanderfolgende Pushes eines Branches als eine
// Zeile, die juengste Zeile markiert. Seit aktionen-zeitleiste (#128) jede
// Zeile jeder Quelle und hoechstens dreissig; die neuen Quellen haelt
// zeitleiste_test.go fest.

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

func berlin(t *testing.T) *time.Location {
	t.Helper()
	z, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}
	return z
}

func sha40(s string) string { return (s + strings.Repeat("0", 40))[:40] }

func akt(art, ref, zeit, after string) ghrest.Aktivitaet {
	return ghrest.Aktivitaet{Art: art, Ref: ref, Zeit: zeit, Nachher: after, Akteur: "work-account"}
}

func arten(l []LetzteAktion) string {
	var a []string
	for _, x := range l {
		a = append(a, x.Art)
	}
	return strings.Join(a, ",")
}

// gemischteEingabe ist die Eingabe des Szenarios "Gemischter Strom".
func gemischteEingabe(t *testing.T) AktionenEingabe {
	jetzt := time.Date(2026, 9, 30, 21, 0, 0, 0, berlin(t))
	merge, x2, x1, y := sha40("dc168e3"), sha40("aaaaaa2"), sha40("aaaaaa1"), sha40("bbbbbb1")
	return AktionenEingabe{
		Repo: "acme/alpha", Standard: "main", Jetzt: jetzt,
		Aktivitaet: []ghrest.Aktivitaet{
			akt("pr_merge", "refs/heads/main", "2026-09-30T18:58:11Z", merge),
			akt("push", "refs/heads/change/x", "2026-09-30T18:40:00Z", x2),
			akt("push", "refs/heads/change/x", "2026-09-30T18:30:00Z", x1),
			akt("push", "refs/heads/change/y", "2026-09-30T18:20:00Z", y),
			akt("branch_deletion", "refs/heads/alt", "2026-09-30T18:59:00Z", sha40("0")),
			akt("push", "refs/tags/v1", "2026-09-30T18:59:30Z", sha40("c")),
		},
		Commits: map[string]ghrest.CommitKopf{
			merge: {SHA: merge, Betreff: "Merge pull request #125 from example-org/claude/upbeat-planck-cxrti4", Autor: "work-account", Zeit: "2026-09-30T18:58:11Z"},
			x2:    {SHA: x2, Betreff: "x: zweiter Schritt", Autor: "alex", Zeit: "2026-09-30T18:39:00Z"},
			x1:    {SHA: x1, Betreff: "x: erster Schritt", Autor: "alex", Zeit: "2026-09-30T18:29:00Z"},
			y:     {SHA: y, Betreff: "y: Anfang", Autor: "alex", Zeit: "2026-09-30T18:19:00Z"},
		},
		OffenePRs: []ghrest.PR{{Nummer: 7, Head: "change/x", URL: "https://github.com/acme/alpha/pull/7"}},
		CIRuns: []ghrest.WorkflowRun{
			{ID: 2, Name: "test", Branch: "main", Status: "in_progress", Start: "2026-09-30T18:15:00Z", Aktualisiert: "2026-09-30T18:16:00Z"},
			{ID: 1, Name: "test", Branch: "main", Status: "completed", Ergebnis: "success", Start: "2026-09-30T18:05:00Z", Aktualisiert: "2026-09-30T18:10:00Z", URL: "https://github.com/acme/alpha/actions/runs/1"},
		},
		Bilder: &GeaenderteBilder{Bilder: []GeaendertesBild{
			{Pfad: "docs/alt.png", Name: "alt.png", Zeit: "2026-09-29T10:00:00Z"},
			{Pfad: "docs/karte.png", Name: "karte.png", Zeit: "2026-09-30T18:00:00Z", Bild: "/api/blob?b=1", Link: "https://github.com/acme/alpha/blob/main/docs/karte.png", Betreff: "Karte neu"},
		}},
		HTML: &HTMLDateien{Dateien: []HTMLDatei{
			{Pfad: "bericht.html", Name: "bericht.html", Zeit: "2026-09-30T17:50:00Z", Adresse: "/api/datei/acme/alpha/abc/bericht.html", Betreff: "Bericht"},
			{Pfad: "alt.html", Name: "alt.html", Zeit: "2026-09-20T17:50:00Z"},
		}},
		Belege: []GalerieBeleg{
			{Kennung: "k", Stufe: "nachher", Name: "k.png", Zeit: "2026-09-30T17:40:00.123Z", Bild: "/api/blob?b=2", Zeigbar: true, Link: "l"},
			{Kennung: "j", Stufe: "nachher", Name: "j.png", Zeit: "2026-09-01T17:40:00Z"},
		},
		Zuletzt: []ArbeitsEintrag{
			{Art: "issue", Nummer: 120, Titel: "Hundertzwanzig", URL: "u120", Geschlossen: "2026-09-30T17:30:00Z",
				GemergtMit: &GemergtMit{Nummer: 121, URL: "https://github.com/acme/alpha/pull/121"}},
		},
	}
}

func TestLetzteAktionenGemischterStrom(t *testing.T) {
	alle := LetzteAktionenBauen(gemischteEingabe(t))
	// Die zehn juengsten; alt.png, alt.html und der Beleg j liegen mehr als 24
	// Stunden zurueck (aktionen-24h, #132).
	if got := arten(alle); got != "branch_weg,merge,push,push,ci,ci,bild,html,beleg,issue" {
		t.Fatalf("Arten %q, erwartet branch_weg,merge,push,push,ci,ci,bild,html,beleg,issue", got)
	}
	if w := alle[0]; !w.Juengste || w.Wort != "Branch gelöscht" || w.Branch != "alt" {
		t.Errorf("Branch geloescht %+v", w)
	}
	if l := alle[4]; l.Wort != "CI läuft" || l.Punkt != CILaeuft || l.Text != "heute 20:15" || l.Juengste {
		t.Errorf("laufender CI-Run %+v", l)
	}
	// Ab hier die Zeilen ohne Branch geloescht und laufenden Run, in der Folge
	// des Stroms.
	l := append([]LetzteAktion{}, alle[1:4]...)
	l = append(l, alle[5:]...)
	m := l[0]
	if m.Juengste || m.Branch != "main" || !m.Standard || m.Text != "heute 20:58" || m.Wort != "Merge" || m.Kurz != "dc168e3" {
		t.Errorf("Merge %+v", m)
	}
	if m.PR == nil || m.PR.Nummer != 125 || m.PR.URL != "https://github.com/acme/alpha/pull/125" {
		t.Errorf("Merge-PR %+v, erwartet #125", m.PR)
	}
	if m.Voll != "30.09.2026 20:58" || m.Titel != "30.09.2026 20:58 · "+sha40("dc168e3") {
		t.Errorf("Merge voll %q titel %q", m.Voll, m.Titel)
	}
	if m.Autor != "work-account" || !strings.HasPrefix(m.Betreff, "Merge pull request #125") || m.BetreffKurz == "" {
		t.Errorf("Merge autor %q betreff %q kurz %q", m.Autor, m.Betreff, m.BetreffKurz)
	}
	x := l[1]
	if x.Wort != "2 Pushes" || x.Anzahl != 2 || x.Branch != "change/x" || x.Standard || x.Text != "heute 20:40" ||
		x.SHA != sha40("aaaaaa2") || x.Betreff != "x: zweiter Schritt" || x.PR == nil || x.PR.Nummer != 7 {
		t.Errorf("Pushes auf change/x %+v", x)
	}
	y := l[2]
	if y.Wort != "Push" || y.Branch != "change/y" || y.PR != nil || y.Anzahl > 1 {
		t.Errorf("Push auf change/y %+v", y)
	}
	ci := l[3]
	if ci.Wort != "CI-Run" || ci.Punkt != CIGruen || ci.Ergebnis != "success" || ci.Workflow != "test" ||
		ci.Branch != "main" || !ci.Standard || ci.URL != "https://github.com/acme/alpha/actions/runs/1" || ci.Text != "heute 20:10" {
		t.Errorf("CI %+v", ci)
	}
	b := l[4]
	if b.Wort != "Bild" || b.Name != "karte.png" || b.Pfad != "docs/karte.png" || b.Bild != "/api/blob?b=1" || b.Link == "" || b.Betreff != "Karte neu" {
		t.Errorf("Bild %+v", b)
	}
	h := l[5]
	if h.Wort != "HTML" || h.Name != "bericht.html" || h.Adresse != "/api/datei/acme/alpha/abc/bericht.html" || h.Text != "heute 19:50" {
		t.Errorf("HTML %+v", h)
	}
	be := l[6]
	if be.Wort != "Beleg" || be.Kennung != "k" || be.Stufe != "nachher" || be.Name != "k.png" || be.Bild != "/api/blob?b=2" || be.Text != "heute 19:40" {
		t.Errorf("Beleg %+v", be)
	}
	i := l[7]
	if i.Wort != "Issue geschlossen" || i.Nummer != 120 || i.IssueTitel != "Hundertzwanzig" || i.URL != "u120" ||
		i.GemergtMit == nil || i.GemergtMit.Nummer != 121 || i.Text != "heute 19:30" || i.Titel != "30.09.2026 19:30" {
		t.Errorf("Issue %+v", i)
	}
	for n, z := range alle[1:] {
		if z.Juengste {
			t.Errorf("Zeile %d (%s) traegt juengste", n+1, z.Art)
		}
	}
}

func TestLetzteAktionenPushesZusammengefasst(t *testing.T) {
	e := AktionenEingabe{Repo: "acme/alpha", Standard: "main", Jetzt: time.Date(2026, 9, 30, 15, 0, 0, 0, berlin(t)),
		Aktivitaet: []ghrest.Aktivitaet{
			akt("push", "refs/heads/change/x", "2026-09-30T12:01:00Z", sha40("1")),
			akt("push", "refs/heads/change/x", "2026-09-30T12:03:00Z", sha40("3")),
			akt("pr_merge", "refs/heads/main", "2026-09-30T12:00:00Z", sha40("m")),
			akt("push", "refs/heads/change/x", "2026-09-30T12:02:00Z", sha40("2")),
			akt("push", "refs/heads/change/x", "2026-09-30T11:59:00Z", sha40("0")),
		}}
	l := LetzteAktionenBauen(e)
	if len(l) != 3 {
		t.Fatalf("%d Zeilen %+v, erwartet 3", len(l), l)
	}
	if l[0].Wort != "3 Pushes" || l[0].Anzahl != 3 || l[0].Zeit != "2026-09-30T12:03:00Z" || l[0].SHA != sha40("3") {
		t.Errorf("erste Zeile %+v, erwartet 3 Pushes mit der Zeit und dem SHA des juengsten", l[0])
	}
	// Ohne Commit steht der Akteur der Aktivitaet als Autor da, ohne Betreff.
	if l[0].Autor != "work-account" || l[0].Betreff != "" {
		t.Errorf("ohne Commit: autor %q betreff %q", l[0].Autor, l[0].Betreff)
	}
	if l[1].Art != "merge" || l[2].Wort != "Push" || l[2].Zeit != "2026-09-30T11:59:00Z" {
		t.Errorf("Folge %+v", l)
	}
}

func TestLetzteAktionenForcePushNichtZusammengefasst(t *testing.T) {
	e := AktionenEingabe{Repo: "acme/alpha", Standard: "main", Jetzt: time.Date(2026, 9, 30, 15, 0, 0, 0, berlin(t)),
		Aktivitaet: []ghrest.Aktivitaet{
			akt("push", "refs/heads/change/x", "2026-09-30T12:03:00Z", sha40("3")),
			akt("force_push", "refs/heads/change/x", "2026-09-30T12:02:00Z", sha40("2")),
		}}
	l := LetzteAktionenBauen(e)
	if arten(l) != "push,force_push" || l[1].Wort != "Force-Push" {
		t.Errorf("Zeilen %+v, erwartet Push und Force-Push getrennt", l)
	}
}

// echteAktivitaet liest das gekuerzte echte Beispiel im Format von GitHub.
func echteAktivitaet(t *testing.T) []ghrest.Aktivitaet {
	t.Helper()
	b, err := os.ReadFile("testdata/aktivitaet_126.json")
	if err != nil {
		t.Fatal(err)
	}
	var roh []struct {
		Ref, Timestamp, Before, After string
		Art                           string `json:"activity_type"`
		Actor                         struct{ Login string }
	}
	if err := json.Unmarshal(b, &roh); err != nil {
		t.Fatal(err)
	}
	var out []ghrest.Aktivitaet
	for _, r := range roh {
		out = append(out, ghrest.Aktivitaet{Ref: r.Ref, Zeit: r.Timestamp, Vorher: r.Before, Nachher: r.After, Art: r.Art, Akteur: r.Actor.Login})
	}
	return out
}

func TestLetzteAktionenEchtesBeispiel(t *testing.T) {
	a := echteAktivitaet(t)
	if len(a) != 10 {
		t.Fatalf("Beispiel mit %d Eintraegen, erwartet 10", len(a))
	}
	e := AktionenEingabe{Repo: "example-org/git-planner", Standard: "main", Aktivitaet: a,
		Jetzt: time.Date(2026, 9, 30, 21, 0, 0, 0, berlin(t))}
	l := LetzteAktionenBauen(e)
	var zeilen []string
	for _, x := range l {
		zeilen = append(zeilen, x.Text+" "+x.Wort+" "+x.Branch)
	}
	want := []string{
		"heute 20:58 Push claude/upbeat-planck-cxrti4",
		"heute 20:58 Merge main",
		"heute 20:58 2 Pushes claude/upbeat-planck-cxrti4",
		"heute 19:58 Merge main",
		"heute 19:58 2 Pushes claude/upbeat-planck-cxrti4",
		"29. Sept. Merge main",
		"29. Sept. 2 Pushes claude/upbeat-planck-cxrti4",
	}
	if strings.Join(zeilen, "\n") != strings.Join(want, "\n") {
		t.Errorf("Zeilen\n%s\nerwartet\n%s", strings.Join(zeilen, "\n"), strings.Join(want, "\n"))
	}
	if len(l) > 1 && l[1].Kurz != "dc168e3" {
		t.Errorf("Merge kurz %q, erwartet dc168e3", l[1].Kurz)
	}
	if len(l) > 0 && (!l[0].Juengste || l[0].Autor != "work-account") {
		t.Errorf("erste Zeile %+v", l[0])
	}
}

func TestLetzteAktionenFehlenderSpeicher(t *testing.T) {
	e := gemischteEingabe(t)
	e.CIRuns, e.Bilder, e.HTML = nil, nil, nil
	if got := arten(LetzteAktionenBauen(e)); got != "branch_weg,merge,push,push,beleg,issue,beleg" {
		t.Errorf("Arten %q, erwartet branch_weg,merge,push,push,beleg,issue,beleg", got)
	}
	// Leere Listen geben ebenso keine Zeile; ein laufender Run ist seit
	// aktionen-zeitleiste (#128) eine Zeile.
	e = gemischteEingabe(t)
	e.CIRuns = e.CIRuns[:1]
	e.Bilder, e.HTML = &GeaenderteBilder{}, &HTMLDateien{}
	if got := arten(LetzteAktionenBauen(e)); got != "branch_weg,merge,push,push,ci,beleg,issue,beleg" {
		t.Errorf("Arten ohne passende Eintraege %q", got)
	}
}

func TestLetzteAktionenHoechstensZehn(t *testing.T) {
	e := AktionenEingabe{Repo: "acme/alpha", Standard: "main", Jetzt: time.Date(2026, 9, 30, 21, 0, 0, 0, berlin(t))}
	// Alles aelter als 24 Stunden: es bleiben die zehn juengsten
	// (aktionen-24h, #132).
	for i := 0; i < 32; i++ {
		e.Aktivitaet = append(e.Aktivitaet, akt("pr_merge", "refs/heads/main", fmt.Sprintf("2026-09-28T10:%02d:00Z", i), sha40(fmt.Sprint(i))))
	}
	e.Zuletzt = []ArbeitsEintrag{{Art: "issue", Nummer: 9, Titel: "neu", Geschlossen: "2026-09-29T11:00:00Z"}}
	l := LetzteAktionenBauen(e)
	if len(l) != 10 {
		t.Fatalf("%d Zeilen, erwartet 10", len(l))
	}
	if l[0].Art != "issue" || !l[0].Juengste {
		t.Errorf("erste Zeile %+v, erwartet das Issue mit juengste", l[0])
	}
	for i, z := range l[1:] {
		if z.Juengste {
			t.Errorf("Zeile %d traegt juengste", i+1)
		}
	}
	if l[1].Zeit != "2026-09-28T10:31:00Z" || l[9].Zeit != "2026-09-28T10:23:00Z" {
		t.Errorf("Merges von %s bis %s, erwartet 10:31 bis 10:23", l[1].Zeit, l[9].Zeit)
	}
}

func TestLetzteAktionenGleicheZeitNachQuelle(t *testing.T) {
	e := gemischteEingabe(t)
	gleich := "2026-09-30T18:58:11Z"
	e.CIRuns[1].Aktualisiert = gleich
	e.Zuletzt[0].Geschlossen = gleich
	if got := arten(LetzteAktionenBauen(e))[:len("branch_weg,merge,ci")]; got != "branch_weg,merge,ci" {
		t.Errorf("Folge bei gleicher Zeit %q, erwartet Merge vor CI", got)
	}
}

func TestRepoArbeitLiefertLetzteAktionen(t *testing.T) {
	g := gemischteEingabe(t)
	e := ArbeitEingabe{Repo: g.Repo, Standard: g.Standard, Jetzt: g.Jetzt, Aktivitaet: g.Aktivitaet, Commits: g.Commits,
		PRs: g.OffenePRs, CIRuns: g.CIRuns, Bilder: g.Bilder, HTML: g.HTML,
		Geschlossen: []ghrest.Issue{{Nummer: 120, Titel: "Hundertzwanzig", Geschlossen: "2026-09-30T17:30:00Z"}}}
	w := RepoArbeitBauen(e)
	if got := arten(w.LetzteAktionen); got != "branch_weg,merge,push,push,ci,ci,bild,html,issue,bild" {
		t.Errorf("letzte_aktionen der Arbeitsliste %q", got)
	}
	if w.LetzterCommit == nil || w.LetzterCommit.Branch != "main" {
		t.Errorf("letzter_commit %+v, erwartet weiter geliefert", w.LetzterCommit)
	}
	b, _ := json.Marshal(w)
	if !strings.Contains(string(b), `"letzte_aktionen":[`) || !strings.Contains(string(b), `"juengste":true`) {
		t.Errorf("JSON ohne letzte_aktionen oder juengste: %.300s", b)
	}
}
