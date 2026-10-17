package appctx

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/niclasedge/git-planner-go/internal/collectors"
)

// Die Tests zur openspec-Quelle (openspec-quelle, #10). Sie halten die
// Szenarien aus openspec/changes/openspec-quelle/specs/openspec-quelle fest.
// GitHub ist durchweg gefaelscht (osFakeGithub): kein Test hier fragt gh oder
// das Netz.

func osDateiA(t *testing.T, wurzel, rel, inhalt string) {
	t.Helper()
	p := filepath.Join(wurzel, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(inhalt), 0o644); err != nil {
		t.Fatal(err)
	}
}

func osChangeA(t *testing.T, wurzel, ordner, proposal, tasks string) {
	t.Helper()
	osDateiA(t, wurzel, "openspec/changes/"+ordner+"/proposal.md", proposal)
	osDateiA(t, wurzel, "openspec/changes/"+ordner+"/tasks.md", tasks)
}

// osFakeGithub ersetzt die GitHub-Abfrage der Quelle fuer die Dauer eines
// Tests. gefragt zaehlt die Aufrufe.
func osFakeGithub(t *testing.T, antwort map[string]any) *int {
	t.Helper()
	alt := openspecGithub
	n := 0
	openspecGithub = func(string) map[string]any { n++; return antwort }
	t.Cleanup(func() { openspecGithub = alt })
	return &n
}

func osIssues(issues ...collectors.GhIssue) map[string]any {
	return map[string]any{"available": true, "nwo": "o/r", "issues": issues}
}

func osKarten(t *testing.T, dir string) []ReportsMap {
	t.Helper()
	maps, err := openspecQuelle{}.Karten(dir)
	if err != nil {
		t.Fatalf("Karten: %v", err)
	}
	return maps
}

func osWurzeln(maps []ReportsMap) []ReportsBead {
	var out []ReportsBead
	for _, m := range maps {
		out = append(out, m.Beads...)
	}
	return out
}

func osBead(t *testing.T, bs []ReportsBead, id string) ReportsBead {
	t.Helper()
	for _, b := range bs {
		if b.ID == id {
			return b
		}
	}
	var ids []string
	for _, b := range bs {
		ids = append(ids, b.ID)
	}
	t.Fatalf("%q fehlt unter %v", id, ids)
	return ReportsBead{}
}

// Szenario: Projekt ohne openspec-Ordner.
func TestOpenspecQuelleOhneOrdnerLeer(t *testing.T) {
	n := osFakeGithub(t, osIssues(ghIssue(1)))
	maps, err := openspecQuelle{}.Karten(t.TempDir())
	if err != nil || len(maps) != 0 {
		t.Errorf("Karten = %v, %v — want leer ohne Fehler", maps, err)
	}
	if *n != 0 {
		t.Errorf("GitHub wurde %d-mal gefragt fuer ein Projekt ohne openspec/", *n)
	}
}

// Szenario: Faehigkeiten der Quelle.
func TestOpenspecQuelleFaehigkeitenUndAnmeldung(t *testing.T) {
	stelle := map[string]int{}
	var faeh Faehigkeiten
	for i, q := range Quellen() {
		stelle[q.Herkunft()] = i
		if q.Herkunft() == HerkunftOpenspec {
			faeh = q.Faehigkeiten()
		}
	}
	o, da := stelle[HerkunftOpenspec]
	if !da {
		t.Fatalf("Quellen() kennt %q nicht: %v", HerkunftOpenspec, stelle)
	}
	if want := (Faehigkeiten{Schreibbar: false, Termine: false, Belege: true}); faeh != want {
		t.Errorf("Faehigkeiten = %+v, want %+v", faeh, want)
	}
	if o < stelle[HerkunftBeads] || o < stelle[HerkunftGithub] {
		t.Errorf("openspec steht an Stelle %d, vor beads/github: %v", o, stelle)
	}
}

// Szenario: Issue mit Change und Aufgaben.
func TestOpenspecIssueMitChangeUndAufgaben(t *testing.T) {
	dir := t.TempDir()
	osChangeA(t, dir, "openspec-quelle", "Geht zurueck auf GitHub-Issue #10.\n",
		"- [x] eins\n- [ ] zwei\n- [ ] drei\n")
	osFakeGithub(t, osIssues(ghIssue(10)))
	w := osWurzeln(osKarten(t, dir))
	if len(w) != 1 {
		t.Fatalf("Wurzeln = %d, want 1 (#10): %+v", len(w), w)
	}
	i := osBead(t, w, "#10")
	if len(i.Children) != 1 || i.Children[0].ID != "openspec-quelle" {
		t.Fatalf("Kinder von #10 = %+v, want nur openspec-quelle", i.Children)
	}
	c := i.Children[0]
	if len(c.Children) != 3 {
		t.Fatalf("Aufgaben = %d, want 3", len(c.Children))
	}
	erledigt := 0
	for _, a := range c.Children {
		if a.Status == statusGeschlossen {
			erledigt++
		}
	}
	if erledigt != 1 {
		t.Errorf("erledigte Aufgaben = %d, want 1", erledigt)
	}
	if c.Counts == nil || c.Counts.Closed != 1 || c.Counts.Total != 3 {
		t.Errorf("Counts = %+v, want 1 von 3", c.Counts)
	}
	if c.Openspec == nil || c.Openspec.Issuelos {
		t.Errorf("Openspec = %+v, want gesetzt und nicht issuelos", c.Openspec)
	}
}

// Szenario: Zuordnung ueber den Issue-Titel.
func TestOpenspecZuordnungUeberTitel(t *testing.T) {
	dir := t.TempDir()
	osChangeA(t, dir, "foo", "Kein Verweis.\n", "- [ ] a\n")
	osFakeGithub(t, osIssues(
		ghIssue(11, func(i *collectors.GhIssue) { i.Title = "foobar: anderes" }),
		ghIssue(12, func(i *collectors.GhIssue) { i.Title = "foo: etwas bauen" }),
	))
	w := osWurzeln(osKarten(t, dir))
	i := osBead(t, w, "#12")
	if len(i.Children) != 1 || i.Children[0].ID != "foo" {
		t.Errorf("Kinder von #12 = %+v, want foo", i.Children)
	}
	if k := osBead(t, w, "#11"); len(k.Children) != 0 {
		t.Errorf("#11 hat Kinder %+v, obwohl der Titel nicht mit \"foo:\" beginnt", k.Children)
	}
}

// Szenario: Change ohne Issue.
func TestOpenspecChangeOhneIssueIstWurzel(t *testing.T) {
	dir := t.TempDir()
	osChangeA(t, dir, "bar", "Kein Verweis.\n", "- [ ] a\n")
	osFakeGithub(t, osIssues(ghIssue(1, func(i *collectors.GhIssue) { i.Title = "anderes: x" })))
	b := osBead(t, osWurzeln(osKarten(t, dir)), "bar")
	if b.Openspec == nil || !b.Openspec.Issuelos {
		t.Errorf("bar: Openspec = %+v, want issuelos", b.Openspec)
	}
}

// Szenario: Issue ohne Change.
func TestOpenspecIssueOhneChange(t *testing.T) {
	dir := t.TempDir()
	osChangeA(t, dir, "x", "Geht zurueck auf GitHub-Issue #10.\n", "- [ ] a\n")
	osFakeGithub(t, osIssues(ghIssue(10), ghIssue(13),
		ghIssue(14, func(i *collectors.GhIssue) { i.State = "CLOSED" })))
	w := osWurzeln(osKarten(t, dir))
	if i := osBead(t, w, "#13"); len(i.Children) != 0 {
		t.Errorf("#13 hat Kinder %+v", i.Children)
	}
	for _, b := range w {
		if b.ID == "#14" {
			t.Error("geschlossenes Issue #14 steht als Wurzel da")
		}
	}
}

// Szenario: Change laeuft.
func TestOpenspecStandLaeuft(t *testing.T) {
	dir := t.TempDir()
	osChangeA(t, dir, "x", "Geht zurueck auf GitHub-Issue #10.\n", "- [x] a\n- [ ] b\n- [ ] c\n")
	osDateiA(t, dir, "openspec/changes/x/gate1.md", "g")
	osFakeGithub(t, osIssues(ghIssue(10)))
	c := osBead(t, osBead(t, osWurzeln(osKarten(t, dir)), "#10").Children, "x")
	s := c.Openspec
	if s == nil {
		t.Fatal("Openspec fehlt am Change")
	}
	if s.Status != collectors.OpenspecLaeuft || s.Erledigt != 1 || s.Gesamt != 3 {
		t.Errorf("Stand = %+v, want laeuft 1/3", s)
	}
	if len(s.Gates) != 1 || s.Gates[0] != "gate1" {
		t.Errorf("Gates = %v, want [gate1]", s.Gates)
	}
	if c.Status != istInArbeit {
		t.Errorf("Status = %q, want %q fuer die Ansichten", c.Status, istInArbeit)
	}
}

// Szenario: Archivierter Change.
func TestOpenspecArchivierterChangeIstErledigt(t *testing.T) {
	dir := t.TempDir()
	osChangeA(t, dir, "archive/2026-09-24-github-pat-env", "Geht zurueck auf GitHub-Issue #2.\n", "- [x] a\n")
	osFakeGithub(t, osIssues(ghIssue(2)))
	c := osBead(t, osBead(t, osWurzeln(osKarten(t, dir)), "#2").Children, "github-pat-env")
	if c.Status != statusGeschlossen {
		t.Errorf("Status = %q, want %q", c.Status, statusGeschlossen)
	}
	if c.Openspec == nil || !c.Openspec.Archiviert || c.Openspec.Status != collectors.OpenspecErledigt {
		t.Errorf("Openspec = %+v, want archiviert und erledigt", c.Openspec)
	}
}

// Szenario: Change mit Bildbeleg.
func TestOpenspecBelegAmChange(t *testing.T) {
	dir := t.TempDir()
	osChangeA(t, dir, "b", "Geht zurueck auf GitHub-Issue #10.\n", "- [x] a\n")
	osDateiA(t, dir, "openspec/changes/b/belege/belege.jsonl",
		`{"zeit":"2026-09-24T10:00:00Z","stufe":"nachher","art":"bild","pfad":"belege/assets/0-x.png","fehlt":false,"grund":""}`+"\n")
	osFakeGithub(t, osIssues(ghIssue(10)))
	c := osBead(t, osBead(t, osWurzeln(osKarten(t, dir)), "#10").Children, "b")
	if c.Openspec == nil || len(c.Openspec.Belege) != 1 {
		t.Fatalf("Belege = %+v, want genau einen", c.Openspec)
	}
	b := c.Openspec.Belege[0]
	if b.Stufe != "nachher" || b.Art != "bild" || b.Pfad != "belege/assets/0-x.png" || b.Fehlt {
		t.Errorf("Beleg = %+v", b)
	}
}

// Szenario: gh fehlt.
func TestOpenspecOhneGhBleibenDieChanges(t *testing.T) {
	dir := t.TempDir()
	osChangeA(t, dir, "eins", "Geht zurueck auf GitHub-Issue #10.\n", "- [ ] a\n")
	osChangeA(t, dir, "zwei", "x\n", "- [ ] a\n")
	osFakeGithub(t, map[string]any{"available": false, "reason": "gh nicht installiert"})
	nurDieseQuellen(t, openspecQuelle{})

	karten := KartenAllerQuellen(dir)
	if len(karten) != 1 || karten[0].Herkunft != HerkunftOpenspec {
		t.Fatalf("Karten = %+v", karten)
	}
	qk := karten[0]
	if qk.Grund != "gh nicht installiert" {
		t.Errorf("Grund = %q, want den Grund der GitHub-Abfrage", qk.Grund)
	}
	w := osWurzeln(qk.Maps)
	osBead(t, w, "eins")
	osBead(t, w, "zwei")
	if len(w) != 2 {
		t.Errorf("Wurzeln = %d, want 2", len(w))
	}
}

// Szenario: Repo mit openspec-Ordner.
func TestOpenspecIssueNurEinmal(t *testing.T) {
	dir := t.TempDir()
	osChangeA(t, dir, "x", "Geht zurueck auf GitHub-Issue #10.\n", "- [ ] a\n")
	osFakeGithub(t, osIssues(ghIssue(10)))
	gh := &fakeQuelle{herkunft: HerkunftGithub, maps: GithubKarten("o/r", []collectors.GhIssue{ghIssue(10)})}
	nurDieseQuellen(t, gh, openspecQuelle{})

	gefunden := map[string]int{}
	var zaehle func(string, []ReportsBead)
	zaehle = func(h string, bs []ReportsBead) {
		for _, b := range bs {
			if b.ID == "#10" {
				gefunden[h]++
			}
			zaehle(h, b.Children)
		}
	}
	for _, qk := range KartenAllerQuellen(dir) {
		zaehle(qk.Herkunft, osWurzeln(qk.Maps))
	}
	if len(gefunden) != 1 || gefunden[HerkunftOpenspec] != 1 {
		t.Errorf("#10 erscheint %v, want genau einmal aus %q", gefunden, HerkunftOpenspec)
	}
	if gh.befragt[dir] != 0 {
		t.Errorf("github wurde %d-mal befragt, obwohl ersetzt", gh.befragt[dir])
	}
}

// Szenario: Repo ohne openspec-Ordner.
func TestOpenspecOhneOrdnerBleibtGithub(t *testing.T) {
	dir := t.TempDir()
	n := osFakeGithub(t, osIssues(ghIssue(3)))
	gh := &fakeQuelle{herkunft: HerkunftGithub, maps: GithubKarten("o/r", []collectors.GhIssue{ghIssue(3)})}
	nurDieseQuellen(t, gh, openspecQuelle{})

	gefunden := map[string]int{}
	for _, qk := range KartenAllerQuellen(dir) {
		for _, b := range osWurzeln(qk.Maps) {
			if b.ID == "#3" {
				gefunden[qk.Herkunft]++
			}
		}
	}
	if len(gefunden) != 1 || gefunden[HerkunftGithub] != 1 {
		t.Errorf("#3 erscheint %v, want genau einmal aus %q", gefunden, HerkunftGithub)
	}
	if *n != 0 {
		t.Errorf("openspec fragte GitHub %d-mal fuer ein Projekt ohne openspec/", *n)
	}
}

// Szenario: Sammellauf ueber Projekte.
func TestSammellaufOhneGithubUndOpenspec(t *testing.T) {
	var hs []string
	for _, q := range sammellaufQuellen() {
		hs = append(hs, q.Herkunft())
	}
	for _, h := range []string{HerkunftGithub, HerkunftOpenspec} {
		if enthaelt(hs, h) {
			t.Errorf("Sammellauf befragt %q: %v", h, hs)
		}
	}
	if !enthaelt(hs, HerkunftBeads) {
		t.Errorf("Sammellauf ohne beads: %v", hs)
	}
}

// Die Karten bleiben neben einem Fehler stehen (quellKartenAus).
func TestKartenNebenFehlerBleiben(t *testing.T) {
	q := &fakeQuelle{herkunft: "teil", maps: []ReportsMap{hKarte("k", hBead("k-1"))}, err: errors.New("halb")}
	qk := quellKartenAus(teilQuelle{q}, "/p")
	if qk.Grund != "halb" || len(qk.Maps) != 1 {
		t.Errorf("QuellKarten = %+v, want Grund und eine Karte", qk)
	}
}

// teilQuelle liefert Karten UND Fehler — fakeQuelle liefert bei einem Fehler
// keine Karten.
type teilQuelle struct{ *fakeQuelle }

func (t teilQuelle) Karten(pfad string) ([]ReportsMap, error) {
	return t.maps, t.err
}
