package appctx

// "Letzte Aktionen" als Zeitleiste (aktionen-zeitleiste, #128): die Commits
// eines Pushes, Belege, Bilder und HTML-Dateien aus den Vergleichen auch
// anderer Branches, Branch angelegt, geschlossene PRs, Tag und Uhrzeit.

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

func aktVergleich(art, ref, zeit, vorher, nachher string) ghrest.Aktivitaet {
	a := akt(art, ref, zeit, nachher)
	a.Vorher = vorher
	return a
}

func commitsVon(l []AktionCommit) string {
	var out []string
	for _, c := range l {
		out = append(out, c.Betreff)
	}
	return strings.Join(out, ",")
}

func TestZeitleisteCommitsEinesPushes(t *testing.T) {
	a0, a1, a2, a3 := sha40("a0"), sha40("a1"), sha40("a2"), sha40("a3")
	b0 := sha40("b0")
	var bs []ghrest.CommitKopf
	for i := 1; i <= 7; i++ {
		bs = append(bs, ghrest.CommitKopf{SHA: sha40(fmt.Sprintf("b%d", i)), Betreff: fmt.Sprintf("b%d", i), Autor: "alex",
			Zeit: fmt.Sprintf("2026-09-30T10:5%dZ", i)})
	}
	e := AktionenEingabe{Repo: "acme/alpha", Standard: "main", Jetzt: time.Date(2026, 9, 30, 15, 0, 0, 0, berlin(t)),
		Aktivitaet: []ghrest.Aktivitaet{
			aktVergleich("push", "refs/heads/change/x", "2026-09-30T12:03:00Z", a2, a3),
			aktVergleich("push", "refs/heads/change/x", "2026-09-30T12:02:00Z", a0, a2),
			aktVergleich("push", "refs/heads/change/y", "2026-09-30T11:00:00Z", b0, bs[6].SHA),
		},
		Vergleiche: map[string]ghrest.PushVergleich{
			VergleichSchluessel(a2, a3):        {Commits: []ghrest.CommitKopf{{SHA: a3, Betreff: "drei", Autor: "alex"}}},
			VergleichSchluessel(a0, a2):        {Commits: []ghrest.CommitKopf{{SHA: a1, Betreff: "eins", Autor: "alex"}, {SHA: a2, Betreff: "zwei", Autor: "alex"}}},
			VergleichSchluessel(b0, bs[6].SHA): {Commits: bs},
		},
	}
	l := LetzteAktionenBauen(e)
	if arten(l) != "push,push" {
		t.Fatalf("Arten %q, erwartet push,push", arten(l))
	}
	x := l[0]
	if x.Wort != "2 Pushes" || x.CommitZahl != 3 || commitsVon(x.Commits) != "drei,zwei,eins" {
		t.Errorf("change/x %q commit_zahl %d commits %q, erwartet 2 Pushes, 3, drei,zwei,eins", x.Wort, x.CommitZahl, commitsVon(x.Commits))
	}
	if len(x.Commits) > 0 && (x.Commits[0].Kurz != a3[:7] || x.Commits[0].SHA != a3 || x.Commits[0].Autor != "alex") {
		t.Errorf("erster Commit %+v", x.Commits[0])
	}
	// Ohne commits/{sha} kommt der Betreff aus dem juengsten Commit des Vergleichs.
	if x.Betreff != "drei" {
		t.Errorf("Betreff %q, erwartet drei aus dem Vergleich", x.Betreff)
	}
	y := l[1]
	if y.CommitZahl != 7 || commitsVon(y.Commits) != "b7,b6,b5,b4,b3" {
		t.Errorf("change/y commit_zahl %d commits %q, erwartet 7 und b7..b3", y.CommitZahl, commitsVon(y.Commits))
	}
	// Ohne Vergleich fehlen beide Felder.
	e.Vergleiche = nil
	for _, z := range LetzteAktionenBauen(e) {
		if z.CommitZahl != 0 || z.Commits != nil {
			t.Errorf("ohne Vergleich %+v", z)
		}
	}
}

func TestZeitleisteDateienAusAnderemBranch(t *testing.T) {
	vorher, nachher := sha40("c0"), sha40("c9")
	bildURL := func(sha, name string) string { return "/blob/" + sha + "/" + name }
	e := AktionenEingabe{Repo: "acme/alpha", Standard: "main", Jetzt: time.Date(2026, 9, 30, 15, 0, 0, 0, berlin(t)),
		BildURL: bildURL,
		Aktivitaet: []ghrest.Aktivitaet{
			aktVergleich("push", "refs/heads/claude/x", "2026-09-30T12:00:00Z", vorher, nachher),
		},
		Vergleiche: map[string]ghrest.PushVergleich{
			VergleichSchluessel(vorher, nachher): {
				Commits: []ghrest.CommitKopf{{SHA: nachher, Betreff: "k: Beleg", Autor: "alex"}},
				Dateien: []ghrest.GeaenderteDatei{
					{Pfad: "openspec/changes/k/belege/assets/0-k.png", Status: "added", SHA: "b1"},
					{Pfad: "openspec/changes/k/belege/belege.jsonl", Status: "modified", SHA: "j1"},
					{Pfad: "docs/karte.png", Status: "modified", SHA: "b2"},
					{Pfad: "bericht.html", Status: "added", SHA: "h1"},
					{Pfad: "alt.png", Status: "removed", SHA: "x1"},
				},
			},
		},
		Belege: []GalerieBeleg{{Kennung: "k", Stufe: "nachher", Name: "0-k.png", Zeit: "2026-09-30T11:00:00Z",
			Bild: "/blob/b1/0-k.png", Zeigbar: true, SHA: "b1"}},
	}
	l := LetzteAktionenBauen(e)
	if arten(l) != "push,bild,html,beleg" {
		t.Fatalf("Arten %q, erwartet push,bild,html,beleg: %+v", arten(l), l)
	}
	for _, z := range l {
		if z.Zeit != "2026-09-30T12:00:00Z" || z.Branch != "claude/x" || z.Standard {
			t.Errorf("Zeile %s: zeit %q branch %q standard %v, erwartet 12:00Z auf claude/x", z.Art, z.Zeit, z.Branch, z.Standard)
		}
	}
	b := l[1]
	if b.Wort != "Bild" || b.Name != "karte.png" || b.Pfad != "docs/karte.png" || b.Bild != "/blob/b2/karte.png" || b.Betreff != "k: Beleg" ||
		b.Link != "https://github.com/acme/alpha/blob/claude/x/docs/karte.png" {
		t.Errorf("Bild %+v", b)
	}
	h := l[2]
	if h.Wort != "HTML" || h.Name != "bericht.html" || h.Adresse != "/api/datei/acme/alpha/"+nachher+"/bericht.html" {
		t.Errorf("HTML %+v", h)
	}
	be := l[3]
	if be.Wort != "Beleg" || be.Kennung != "k" || be.Name != "0-k.png" || be.Bild != "/blob/b1/0-k.png" ||
		be.Link != "https://github.com/acme/alpha/blob/claude/x/openspec/changes/k/belege/assets/0-k.png" {
		t.Errorf("Beleg %+v", be)
	}
}

func TestZeitleisteDateienHoechstensVierUndArchiv(t *testing.T) {
	vorher, nachher := sha40("d0"), sha40("d9")
	var dateien []ghrest.GeaenderteDatei
	for i := 0; i < 6; i++ {
		dateien = append(dateien, ghrest.GeaenderteDatei{Pfad: fmt.Sprintf("openspec/changes/archive/2026-09-30-ci-runs/belege/assets/%d-ci.png", i),
			Status: "added", SHA: fmt.Sprintf("s%d", i)})
	}
	e := AktionenEingabe{Repo: "acme/alpha", Standard: "main", Jetzt: time.Date(2026, 9, 30, 15, 0, 0, 0, berlin(t)),
		BildURL:    func(sha, name string) string { return "/blob/" + sha + "/" + name },
		Aktivitaet: []ghrest.Aktivitaet{aktVergleich("push", "refs/heads/main", "2026-09-30T12:00:00Z", vorher, nachher)},
		Vergleiche: map[string]ghrest.PushVergleich{VergleichSchluessel(vorher, nachher): {Dateien: dateien}},
	}
	l := LetzteAktionenBauen(e)
	if arten(l) != "push,beleg,beleg,beleg,beleg" {
		t.Fatalf("Arten %q, erwartet ein Push und vier Belege", arten(l))
	}
	if l[1].Kennung != "ci-runs" || !l[1].Standard || l[1].Branch != "main" {
		t.Errorf("Beleg aus dem Archiv %+v, erwartet kennung ci-runs auf main", l[1])
	}
}

func TestZeitleisteBranchAngelegt(t *testing.T) {
	neu := sha40("e5e5e5e5")
	e := AktionenEingabe{Repo: "acme/alpha", Standard: "main", Jetzt: time.Date(2026, 9, 30, 15, 0, 0, 0, berlin(t)),
		Aktivitaet: []ghrest.Aktivitaet{aktVergleich("branch_creation", "refs/heads/claude/neu", "2026-09-30T12:05:00Z", sha40("0"), neu)}}
	l := LetzteAktionenBauen(e)
	if len(l) != 1 {
		t.Fatalf("%d Zeilen, erwartet 1", len(l))
	}
	z := l[0]
	if z.Art != "branch_neu" || z.Wort != "Branch angelegt" || z.Branch != "claude/neu" || z.Kurz != neu[:7] ||
		z.Titel != z.Voll+" · "+neu {
		t.Errorf("Branch angelegt %+v", z)
	}
}

func TestZeitleisteGeschlossenePRs(t *testing.T) {
	e := gemischteEingabe(t)
	e.GeschlossenePRs = []ghrest.PR{
		{Nummer: 125, Titel: "letzte-aktionen: Zeitstrom", URL: "https://github.com/acme/alpha/pull/125", Head: "claude/upbeat-planck-cxrti4",
			Gemergt: true, GemergtAm: "2026-09-30T18:58:11Z", GeschlossenAm: "2026-09-30T18:58:11Z"},
		{Nummer: 119, Titel: "Verworfen", URL: "https://github.com/acme/alpha/pull/119", Head: "change/z", Autor: "alex",
			GeschlossenAm: "2026-09-30T18:50:00Z"},
		{Nummer: 118, Titel: "Achtzehn", URL: "https://github.com/acme/alpha/pull/118", Head: "change/w",
			Gemergt: true, GemergtAm: "2026-09-30T18:45:00Z", GeschlossenAm: "2026-09-30T18:45:00Z"},
	}
	l := LetzteAktionenBauen(e)
	if got := arten(l); !strings.HasPrefix(got, "branch_weg,merge,pr,pr,push,push,") {
		t.Fatalf("Arten %q, erwartet branch_weg,merge,pr,pr,push,push,…", got)
	}
	m := l[1]
	if m.PR == nil || m.PR.Nummer != 125 || m.PR.Titel != "letzte-aktionen: Zeitstrom" {
		t.Errorf("Merge-PR %+v, erwartet #125 mit Titel", m.PR)
	}
	zu := l[2]
	if zu.Wort != "PR geschlossen" || zu.Nummer != 119 || zu.PRTitel != "Verworfen" || zu.Branch != "change/z" ||
		zu.URL != "https://github.com/acme/alpha/pull/119" || zu.Autor != "alex" || zu.Text != "heute 20:50" {
		t.Errorf("PR geschlossen %+v", zu)
	}
	gm := l[3]
	if gm.Wort != "PR gemergt" || gm.Nummer != 118 || gm.PRTitel != "Achtzehn" || gm.Zeit != "2026-09-30T18:45:00Z" {
		t.Errorf("PR gemergt %+v", gm)
	}
	for _, z := range l {
		if z.Art == "pr" && z.Nummer == 125 {
			t.Errorf("eigene Zeile fuer #125, den die Merge-Zeile schon nennt")
		}
	}
}

func TestZeitleisteTagUndUhrzeit(t *testing.T) {
	e := AktionenEingabe{Repo: "acme/alpha", Standard: "main", Jetzt: time.Date(2026, 10, 1, 10, 0, 0, 0, berlin(t))}
	for i, z := range []string{"2026-10-01T07:00:00Z", "2026-09-30T22:30:00Z", "2026-09-30T21:30:00Z", "2026-09-29T10:00:00Z", "2025-12-24T10:00:00Z"} {
		e.Aktivitaet = append(e.Aktivitaet, akt("pr_merge", "refs/heads/main", z, sha40(fmt.Sprint(i))))
	}
	var tage, uhren []string
	for _, z := range LetzteAktionenBauen(e) {
		tage = append(tage, z.Tag)
		uhren = append(uhren, z.Uhr)
	}
	if got := strings.Join(tage, "|"); got != "Heute|Heute|Gestern|Di., 29. Sept.|Mi., 24. Dez. 2025" {
		t.Errorf("tag %q", got)
	}
	if got := strings.Join(uhren, "|"); got != "09:00|00:30|23:30|12:00|11:00" {
		t.Errorf("uhr %q", got)
	}
}

func TestZeitleisteGleicherBlobNurEinmal(t *testing.T) {
	// HTML aus dem Speicher und aus einem Vergleich mit demselben Blob: nur die
	// juengste Zeile bleibt.
	vorher, nachher := sha40("f0"), sha40("f9")
	e := AktionenEingabe{Repo: "acme/alpha", Standard: "main", Jetzt: time.Date(2026, 9, 30, 15, 0, 0, 0, berlin(t)),
		Aktivitaet: []ghrest.Aktivitaet{aktVergleich("push", "refs/heads/claude/x", "2026-09-30T12:00:00Z", vorher, nachher)},
		Vergleiche: map[string]ghrest.PushVergleich{VergleichSchluessel(vorher, nachher): {
			Dateien: []ghrest.GeaenderteDatei{{Pfad: "bericht.html", Status: "modified", SHA: "h1"}}}},
		HTML: &HTMLDateien{Dateien: []HTMLDatei{{Pfad: "bericht.html", Name: "bericht.html", SHA: "h1", Zeit: "2026-09-30T13:00:00Z"}}},
	}
	l := LetzteAktionenBauen(e)
	if arten(l) != "html,push" || l[0].Zeit != "2026-09-30T13:00:00Z" {
		t.Errorf("Arten %q (%+v), erwartet html um 13:00Z und den Push ohne zweite HTML-Zeile", arten(l), l)
	}
}

func TestVergleichsPaare(t *testing.T) {
	akt := []ghrest.Aktivitaet{
		aktVergleich("push", "refs/heads/x", "2026-09-30T12:00:00Z", sha40("1"), sha40("2")),
		aktVergleich("force_push", "refs/heads/x", "2026-09-30T11:00:00Z", sha40("3"), sha40("4")),
		aktVergleich("pr_merge", "refs/heads/main", "2026-09-30T10:00:00Z", sha40("5"), sha40("6")),
		aktVergleich("branch_creation", "refs/heads/y", "2026-09-30T09:00:00Z", strings.Repeat("0", 40), sha40("7")),
		aktVergleich("push", "refs/heads/z", "2026-09-30T08:00:00Z", strings.Repeat("0", 40), sha40("8")),
		aktVergleich("push", "refs/tags/v1", "2026-09-30T07:00:00Z", sha40("9"), sha40("a")),
	}
	got := VergleichsPaare(akt)
	if len(got) != 2 || got[0] != [2]string{sha40("1"), sha40("2")} || got[1] != [2]string{sha40("3"), sha40("4")} {
		t.Errorf("Paare %v, erwartet nur Push und Force-Push mit echtem before", got)
	}
}
