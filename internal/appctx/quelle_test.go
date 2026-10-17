package appctx

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/niclasedge/git-planner-go/internal/collectors"
)

// Die Tests zur Quellen-Tabelle (aufgaben-quelle, #9). Sie halten die
// Szenarien aus openspec/changes/aufgaben-quelle/specs/aufgaben-quellen fest.

// beadsFaehigkeiten und githubFaehigkeiten stehen hier ausgeschrieben und
// nicht als Aufruf der Adapter: der Test soll sagen, was die beiden Quellen
// koennen, statt es vom Code abzuschreiben, den er prueft.
var (
	beadsFaehigkeiten  = Faehigkeiten{Schreibbar: true, Termine: true, Belege: true}
	githubFaehigkeiten = Faehigkeiten{}
)

// beadsUndGithub ist die Kartenliste, wie sie die bestehenden Tests bisher
// als zwei feste Parameter uebergeben haben: erst beads, dann GitHub.
func beadsUndGithub(maps, github []ReportsMap) []QuellKarten {
	return []QuellKarten{
		{Herkunft: HerkunftBeads, Faehigkeiten: beadsFaehigkeiten, Maps: maps},
		{Herkunft: HerkunftGithub, Faehigkeiten: githubFaehigkeiten, Maps: github},
	}
}

// fakeQuelle ist eine Test-Quelle mit fester Antwort. befragt zaehlt je
// Projektpfad, wie oft Karten gerufen wurde.
type fakeQuelle struct {
	herkunft string
	faeh     Faehigkeiten
	maps     []ReportsMap
	err      error
	befragt  map[string]int
}

func (f *fakeQuelle) Herkunft() string           { return f.herkunft }
func (f *fakeQuelle) Faehigkeiten() Faehigkeiten { return f.faeh }
func (f *fakeQuelle) Karten(pfad string) ([]ReportsMap, error) {
	if f.befragt == nil {
		f.befragt = map[string]int{}
	}
	f.befragt[pfad]++
	if f.err != nil {
		return nil, f.err
	}
	return f.maps, nil
}

// ersetzendeQuelle ist eine fakeQuelle, die fuer einzelne Projekte andere
// Herkuenfte ersetzt.
type ersetzendeQuelle struct {
	fakeQuelle
	ersetzt map[string][]string
}

func (e *ersetzendeQuelle) Ersetzt(pfad string) []string { return e.ersetzt[pfad] }

// nurDieseQuellen ersetzt die Tabelle fuer die Dauer eines Tests. Eine
// Test-Quelle in der echten Tabelle stuende sonst in jedem anderen Test dieses
// Pakets, und beads und GitHub fragten hier bd und das Netz.
func nurDieseQuellen(t *testing.T, qs ...Quelle) {
	t.Helper()
	alt := quellen
	quellen = nil
	t.Cleanup(func() { quellen = alt })
	for _, q := range qs {
		QuellenRegistrieren(q)
	}
}

func eintragVon(a AufgabenAnsicht, id string) (AnsichtEintrag, bool) {
	for _, e := range a.Eintraege {
		if e.Bead == id {
			return e, true
		}
	}
	return AnsichtEintrag{}, false
}

// Szenario: beads und GitHub sind angemeldet.
func TestQuellenKennenBeadsUndGithub(t *testing.T) {
	faeh := map[string]Faehigkeiten{}
	for _, q := range Quellen() {
		faeh[q.Herkunft()] = q.Faehigkeiten()
	}
	for _, h := range []string{HerkunftBeads, HerkunftGithub} {
		if _, da := faeh[h]; !da {
			t.Errorf("Quellen() kennt %q nicht: %v", h, faeh)
		}
	}
	for h, f := range faeh {
		if f.Schreibbar != (h == HerkunftBeads) {
			t.Errorf("Quelle %q meldet Schreibbar = %v — nur beads ist schreibbar", h, f.Schreibbar)
		}
	}
}

// Quellen() gibt eine Kopie heraus: wer die Liste veraendert, veraendert die
// Tabelle nicht.
func TestQuellenGibtEineKopie(t *testing.T) {
	nurDieseQuellen(t, &fakeQuelle{herkunft: "a"}, &fakeQuelle{herkunft: "b"})
	kopie := Quellen()
	kopie[0] = &fakeQuelle{herkunft: "x"}
	if got := Quellen()[0].Herkunft(); got != "a" {
		t.Errorf("Quellen()[0] nach Aenderung der Kopie = %q, want %q", got, "a")
	}
}

// Szenario: Fake-Quelle in Jederzeit.
func TestFakeQuelleErscheintInJederzeit(t *testing.T) {
	fake := &fakeQuelle{herkunft: "fake", maps: []ReportsMap{hKarte("fake-karte",
		hBead("f-1", func(b *ReportsBead) {
			b.Title = "Aufgabe aus der Fake-Quelle"
			b.URL = "https://example.org/f-1"
		}))}}
	nurDieseQuellen(t, fake)

	ansichten := AnsichtenBauenAus(KartenAllerQuellen("/p/fake"), jetztTest())

	for _, r := range Regeln() {
		if _, da := ansichten[r.ID]; !da {
			t.Errorf("AnsichtenBauenAus liefert die Ansicht %q nicht", r.ID)
		}
	}
	e, da := eintragVon(ansichten[JederzeitID], "f-1")
	if !da {
		t.Fatalf("f-1 steht nicht in Jederzeit: %v", zeilenIDs(ansichten[JederzeitID]))
	}
	if e.Herkunft != "fake" || e.Karte != "fake-karte" {
		t.Errorf("Herkunft/Karte = %q/%q, want fake/fake-karte", e.Herkunft, e.Karte)
	}
	if e.Titel != "Aufgabe aus der Fake-Quelle" || e.Status != "open" || e.URL != "https://example.org/f-1" {
		t.Errorf("Eintrag traegt Titel/Status/URL = %q/%q/%q — eine nicht schreibbare Quelle schickt sie mit",
			e.Titel, e.Status, e.URL)
	}
}

// Szenarien: nicht schreibbare und schreibbare Quelle. Die Herkunft heisst in
// beiden Faellen gleich, damit der Test zeigt, dass nicht der Name entscheidet.
func TestFaehigkeitenSteuernAnfassbarUndEintrag(t *testing.T) {
	jetzt := jetztTest()
	for _, fall := range []struct {
		name       string
		herkunft   string
		faeh       Faehigkeiten
		anfassbar  bool
		mitInhalte bool
	}{
		{"nicht schreibbar", "fake", Faehigkeiten{}, true, true},
		{"nicht schreibbar, Name beads", HerkunftBeads, Faehigkeiten{}, true, true},
		{"schreibbar", "fake", Faehigkeiten{Schreibbar: true}, false, false},
		{"schreibbar, Name github", HerkunftGithub, Faehigkeiten{Schreibbar: true}, false, false},
	} {
		t.Run(fall.name, func(t *testing.T) {
			b := hBead("k-1", func(b *ReportsBead) {
				b.Ready = false
				b.URL = "https://example.org/k-1"
			})
			k := Kandidat{Bead: b, Herkunft: fall.herkunft, Faehigkeiten: fall.faeh,
				URL: b.URL, Heute: tagesAnfang(jetzt), Jetzt: jetzt}
			if got := k.Anfassbar(); got != fall.anfassbar {
				t.Errorf("Anfassbar() = %v, want %v", got, fall.anfassbar)
			}
			e := eintragAus(k, Urteil{}, 0)
			if hat := e.Titel != "" || e.Status != "" || e.URL != ""; hat != fall.mitInhalte {
				t.Errorf("Eintrag Titel/Status/URL = %q/%q/%q, mitgeschickt = %v, want %v",
					e.Titel, e.Status, e.URL, hat, fall.mitInhalte)
			}

			// Derselbe Weg durch die ganze Ansicht: Jederzeit waehlt ueber Anfassbar.
			a := AnsichtenBauenAus([]QuellKarten{{Herkunft: fall.herkunft, Faehigkeiten: fall.faeh,
				Maps: []ReportsMap{hKarte("m", b)}}}, jetzt)[JederzeitID]
			if _, da := eintragVon(a, "k-1"); da != fall.anfassbar {
				t.Errorf("k-1 in Jederzeit = %v, want %v", da, fall.anfassbar)
			}
		})
	}
}

// Szenario: eine Quelle meldet einen Fehler.
func TestFehlschlagendeQuelleBrichtDieAnderenNicht(t *testing.T) {
	kaputt := &fakeQuelle{herkunft: "kaputt", err: errors.New("kein Zugang")}
	heil := &fakeQuelle{herkunft: "heil", maps: []ReportsMap{hKarte("h", hBead("h-1"))}}
	nurDieseQuellen(t, kaputt, heil)

	karten := KartenAllerQuellen("/p/x")

	nach := map[string]QuellKarten{}
	for _, qk := range karten {
		nach[qk.Herkunft] = qk
	}
	if len(karten) != 2 {
		t.Fatalf("KartenAllerQuellen liefert %d Eintraege, want 2: %+v", len(karten), karten)
	}
	if k := nach["kaputt"]; k.Grund != "kein Zugang" || len(k.Maps) != 0 {
		t.Errorf("kaputt: Grund = %q, Karten = %d — want \"kein Zugang\" und keine", k.Grund, len(k.Maps))
	}
	if h := nach["heil"]; h.Grund != "" || len(h.Maps) != 1 {
		t.Errorf("heil: Grund = %q, Karten = %d — want leer und eine", h.Grund, len(h.Maps))
	}
	a := AnsichtenBauenAus(karten, jetztTest())[JederzeitID]
	if _, da := eintragVon(a, "h-1"); !da {
		t.Errorf("h-1 fehlt in Jederzeit, obwohl nur die andere Quelle fehlschlug: %v", zeilenIDs(a))
	}
}

// Szenario: Test-Quelle ersetzt github fuer ein Projekt.
func TestErsetzerLaesstDieErsetzteQuelleWeg(t *testing.T) {
	gh := &fakeQuelle{herkunft: HerkunftGithub, maps: []ReportsMap{hKarte("o/r", hBead("#1"))}}
	neu := &ersetzendeQuelle{
		fakeQuelle: fakeQuelle{herkunft: "neu"},
		ersetzt:    map[string][]string{"/p/alpha": {HerkunftGithub}},
	}
	nurDieseQuellen(t, gh, neu)

	herkuenfte := func(ks []QuellKarten) []string {
		var out []string
		for _, k := range ks {
			out = append(out, k.Herkunft)
		}
		return out
	}

	alpha := herkuenfte(KartenAllerQuellen("/p/alpha"))
	if enthaelt(alpha, HerkunftGithub) {
		t.Errorf("/p/alpha: github steht im Ergebnis, obwohl ersetzt: %v", alpha)
	}
	if !enthaelt(alpha, "neu") {
		t.Errorf("/p/alpha: die ersetzende Quelle fehlt: %v", alpha)
	}
	if n := gh.befragt["/p/alpha"]; n != 0 {
		t.Errorf("/p/alpha: github wurde %d-mal befragt, want 0", n)
	}

	beta := herkuenfte(KartenAllerQuellen("/p/beta"))
	if !enthaelt(beta, HerkunftGithub) {
		t.Errorf("/p/beta: github fehlt, obwohl nur fuer /p/alpha ersetzt: %v", beta)
	}
	if n := gh.befragt["/p/beta"]; n != 1 {
		t.Errorf("/p/beta: github wurde %d-mal befragt, want 1", n)
	}
}

// Szenario: beide Quellen nebeneinander. Die alten Einstiege mit zwei festen
// Parametern bleiben als Huellen stehen; ihre Nutzlast muss dieselbe sein wie
// die des neuen Wegs ueber die Kartenliste.
func TestBeadsUndGithubUeberDieKartenlisteUnveraendert(t *testing.T) {
	maps := []ReportsMap{hKarte("m",
		hBead("bd-1", func(b *ReportsBead) { b.Ready = true }),
		hBead("bd-2", func(b *ReportsBead) { b.DueAt = "2026-09-14" }),
		hBead("bd-3"),
	)}
	gh := GithubKarten("o/r", []collectors.GhIssue{ghIssue(7), ghIssue(8)})
	jetzt := jetztTest()

	alt := AnsichtenBauenMit(maps, gh, jetzt)
	neu := AnsichtenBauenAus(beadsUndGithub(maps, gh), jetzt)
	if !reflect.DeepEqual(jsonVon(t, alt), jsonVon(t, neu)) {
		t.Errorf("AnsichtenBauenMit und AnsichtenBauenAus weichen ab:\nalt: %s\nneu: %s",
			jsonVon(t, alt), jsonVon(t, neu))
	}

	// Und die fachlichen Erwartungen, damit die Gleichheit nicht die triviale ist.
	j := neu[JederzeitID]
	for _, id := range []string{"bd-1", "#7", "#8"} {
		if _, da := eintragVon(j, id); !da {
			t.Errorf("%s fehlt in Jederzeit: %v", id, zeilenIDs(j))
		}
	}
	if _, da := eintragVon(j, "bd-3"); da {
		t.Errorf("bd-3 (beads, nicht ready) steht in Jederzeit: %v", zeilenIDs(j))
	}
	if e, _ := eintragVon(j, "bd-1"); e.Titel != "" {
		t.Errorf("beads-Eintrag traegt Titel %q — beads schickt keinen Inhalt mit", e.Titel)
	}
	if e, _ := eintragVon(j, "#7"); e.Titel == "" || e.URL == "" {
		t.Errorf("GitHub-Eintrag ohne Titel/URL: %+v", e)
	}
}

func jsonVon(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
