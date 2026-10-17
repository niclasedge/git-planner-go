package appctx

// Drei-Wege-Merge und Vorschau je Konfliktdatei (konflikte-loesen, #75).
// Alles ohne Browser und ohne Netz: drei Staende rein, Hunks und Diffs raus.

import (
	"reflect"
	"strings"
	"testing"
)

func txt(zeilen ...string) *string {
	s := strings.Join(zeilen, "\n") + "\n"
	return &s
}

func zeilenVon(t *testing.T, s *string) []string {
	t.Helper()
	if s == nil {
		t.Fatal("Ergebnis ist die Loeschung, erwartet ein Inhalt")
	}
	return Zeilen(*s)
}

func diffText(d []DiffZeile) []string {
	out := make([]string, 0, len(d))
	for _, z := range d {
		out = append(out, z.Art+" "+z.Text)
	}
	return out
}

func TestZeilen(t *testing.T) {
	for _, fall := range []struct {
		ein  string
		soll []string
	}{
		{"", []string{}},
		{"a\n", []string{"a"}},
		{"a\nb", []string{"a", "b"}},
		{"a\n\nb\n", []string{"a", "", "b"}},
	} {
		if ist := Zeilen(fall.ein); !reflect.DeepEqual(ist, fall.soll) {
			t.Errorf("Zeilen(%q) = %q, erwartet %q", fall.ein, ist, fall.soll)
		}
	}
}

func TestZeilenDiffWegVorHinzu(t *testing.T) {
	ist := diffText(ZeilenDiff([]string{"a", "b", "C"}, []string{"a", "B", "c"}))
	soll := []string{"gleich a", "weg b", "weg C", "hinzu B", "hinzu c"}
	if !reflect.DeepEqual(ist, soll) {
		t.Errorf("Diff %q, erwartet %q", ist, soll)
	}
	ist = diffText(ZeilenDiff([]string{"x", "a", "y"}, []string{"x", "y", "z"}))
	soll = []string{"gleich x", "weg a", "gleich y", "hinzu z"}
	if !reflect.DeepEqual(ist, soll) {
		t.Errorf("Diff %q, erwartet %q", ist, soll)
	}
	if d := ZeilenDiff(nil, nil); len(d) != 0 {
		t.Errorf("leerer Diff %q", diffText(d))
	}
}

// Ein Diff ueber lange Dateien bleibt korrekt: aus alt und dem Diff laesst
// sich neu wieder zusammensetzen.
func TestZeilenDiffLangRekonstruiert(t *testing.T) {
	var alt, neu []string
	for i := 0; i < 3000; i++ {
		alt = append(alt, "z"+strings.Repeat("x", i%7))
		if i%97 == 0 {
			neu = append(neu, "neu")
		}
		if i%13 != 0 {
			neu = append(neu, "z"+strings.Repeat("x", i%7))
		}
	}
	d := ZeilenDiff(alt, neu)
	var a, n []string
	for _, z := range d {
		switch z.Art {
		case "gleich":
			a, n = append(a, z.Text), append(n, z.Text)
		case "weg":
			a = append(a, z.Text)
		case "hinzu":
			n = append(n, z.Text)
		default:
			t.Fatalf("unbekannte Art %q", z.Art)
		}
	}
	if !reflect.DeepEqual(a, alt) || !reflect.DeepEqual(n, neu) {
		t.Fatal("Diff setzt alt und neu nicht wieder zusammen")
	}
}

func TestDiffKuerzen(t *testing.T) {
	var d []DiffZeile
	for i := 0; i < 20; i++ {
		d = append(d, DiffZeile{Art: "gleich", Text: "g"})
	}
	d = append(d, DiffZeile{Art: "hinzu", Text: "neu"})
	for i := 0; i < 20; i++ {
		d = append(d, DiffZeile{Art: "gleich", Text: "h"})
	}
	k := DiffKuerzen(d, 3)
	arten := []string{}
	for _, z := range k {
		arten = append(arten, z.Art)
	}
	soll := []string{"ausgelassen", "gleich", "gleich", "gleich", "hinzu", "gleich", "gleich", "gleich", "ausgelassen"}
	if !reflect.DeepEqual(arten, soll) {
		t.Fatalf("gekuerzt %v, erwartet %v", arten, soll)
	}
	if !strings.Contains(k[0].Text, "17") || !strings.Contains(k[len(k)-1].Text, "17") {
		t.Errorf("Auslassungen %q / %q nennen die Zahl 17 nicht", k[0].Text, k[len(k)-1].Text)
	}
	// Kurze Strecken bleiben ganz.
	kurz := []DiffZeile{{"gleich", "a"}, {"weg", "b"}, {"gleich", "c"}}
	if ist := DiffKuerzen(kurz, 3); !reflect.DeepEqual(ist, kurz) {
		t.Errorf("kurzer Diff gekuerzt: %v", ist)
	}
}

func TestMergeNichtUeberlappend(t *testing.T) {
	basis := txt("a", "b", "c", "d", "e", "f", "g", "h")
	meine := txt("a", "B", "c", "d", "e", "f", "g", "h")
	extern := txt("a", "b", "c", "d", "e", "f", "G", "h")
	m := DreiWegeMerge(basis, meine, extern)
	if m.Hunks != 0 {
		t.Fatalf("%d Konflikt-Hunks, erwartet 0: %+v", m.Hunks, m.Abschnitte)
	}
	erg, err := m.Ergebnis(nil)
	if err != nil {
		t.Fatal(err)
	}
	if ist, soll := zeilenVon(t, erg), []string{"a", "B", "c", "d", "e", "f", "G", "h"}; !reflect.DeepEqual(ist, soll) {
		t.Errorf("Ergebnis %q, erwartet %q", ist, soll)
	}
	// Einfuegen und Loeschen an verschiedenen Stellen.
	m = DreiWegeMerge(txt("a", "b", "c", "d", "e"), txt("neu", "a", "b", "c", "d", "e"), txt("a", "b", "c", "e"))
	erg, err = m.Ergebnis(nil)
	if err != nil || m.Hunks != 0 {
		t.Fatalf("Hunks %d, Fehler %v", m.Hunks, err)
	}
	if ist, soll := zeilenVon(t, erg), []string{"neu", "a", "b", "c", "e"}; !reflect.DeepEqual(ist, soll) {
		t.Errorf("Ergebnis %q, erwartet %q", ist, soll)
	}
}

func TestMergeUeberlappend(t *testing.T) {
	basis := txt("a", "b", "c", "d", "e")
	m := DreiWegeMerge(basis, txt("a", "b", "c1", "d", "e"), txt("a", "b", "c2", "d", "e"))
	if m.Hunks != 1 {
		t.Fatalf("%d Konflikt-Hunks, erwartet 1: %+v", m.Hunks, m.Abschnitte)
	}
	var k *MergeAbschnitt
	for i := range m.Abschnitte {
		if m.Abschnitte[i].Art == "konflikt" {
			k = &m.Abschnitte[i]
		}
	}
	if k == nil || k.Hunk != 0 || !reflect.DeepEqual(k.Basis, []string{"c"}) || !reflect.DeepEqual(k.Meine, []string{"c1"}) || !reflect.DeepEqual(k.Extern, []string{"c2"}) {
		t.Fatalf("Konflikt-Abschnitt %+v, erwartet Hunk 0 mit c / c1 / c2", k)
	}
	if _, err := m.Ergebnis(nil); err == nil {
		t.Error("Ergebnis ohne Hunk-Wahl gebildet")
	}
	if _, err := m.Ergebnis([]string{"vielleicht"}); err == nil {
		t.Error("Ergebnis mit ungueltiger Wahl gebildet")
	}
	erg, err := m.Ergebnis([]string{"extern"})
	if err != nil {
		t.Fatal(err)
	}
	if ist, soll := zeilenVon(t, erg), []string{"a", "b", "c2", "d", "e"}; !reflect.DeepEqual(ist, soll) {
		t.Errorf("extern: %q, erwartet %q", ist, soll)
	}
	erg, _ = m.Ergebnis([]string{"meine"})
	if ist, soll := zeilenVon(t, erg), []string{"a", "b", "c1", "d", "e"}; !reflect.DeepEqual(ist, soll) {
		t.Errorf("meine: %q, erwartet %q", ist, soll)
	}
}

// Zwei Konflikte und dazwischen eine Aenderung nur einer Seite: die Hunks
// sind der Reihe nach nummeriert, die einseitige Aenderung kommt immer mit.
func TestMergeZweiHunks(t *testing.T) {
	basis := txt("1", "2", "3", "4", "5", "6", "7", "8", "9")
	meine := txt("1", "2m", "3", "4", "5m", "6", "7", "8m", "9")
	extern := txt("1", "2e", "3", "4", "5", "6", "7", "8e", "9")
	m := DreiWegeMerge(basis, meine, extern)
	if m.Hunks != 2 {
		t.Fatalf("%d Hunks, erwartet 2", m.Hunks)
	}
	if _, err := m.Ergebnis([]string{"meine"}); err == nil {
		t.Error("Ergebnis mit nur einer von zwei Wahlen gebildet")
	}
	erg, err := m.Ergebnis([]string{"meine", "extern"})
	if err != nil {
		t.Fatal(err)
	}
	if ist, soll := zeilenVon(t, erg), []string{"1", "2m", "3", "4", "5m", "6", "7", "8e", "9"}; !reflect.DeepEqual(ist, soll) {
		t.Errorf("Ergebnis %q, erwartet %q", ist, soll)
	}
}

func TestMergeGleicheAenderung(t *testing.T) {
	basis := txt("a", "b", "c")
	beide := txt("a", "B", "c")
	m := DreiWegeMerge(basis, beide, txt("a", "B", "c"))
	if m.Hunks != 0 {
		t.Fatalf("%d Hunks bei gleicher Aenderung", m.Hunks)
	}
	erg, err := m.Ergebnis(nil)
	if err != nil {
		t.Fatal(err)
	}
	if ist := zeilenVon(t, erg); !reflect.DeepEqual(ist, []string{"a", "B", "c"}) {
		t.Errorf("Ergebnis %q", ist)
	}
}

func TestMergeNurEineSeite(t *testing.T) {
	basis := txt("a", "b", "c")
	// Nur meine geaendert.
	m := DreiWegeMerge(basis, txt("a", "x", "c"), basis)
	erg, err := m.Ergebnis(nil)
	if err != nil || m.Hunks != 0 {
		t.Fatalf("Hunks %d, Fehler %v", m.Hunks, err)
	}
	if ist := zeilenVon(t, erg); !reflect.DeepEqual(ist, []string{"a", "x", "c"}) {
		t.Errorf("nur meine: %q", ist)
	}
	// Nur extern geaendert.
	m = DreiWegeMerge(basis, basis, txt("a", "b", "c", "d"))
	erg, _ = m.Ergebnis(nil)
	if ist := zeilenVon(t, erg); !reflect.DeepEqual(ist, []string{"a", "b", "c", "d"}) {
		t.Errorf("nur extern: %q", ist)
	}
	// Meine geloescht, extern unveraendert: Loeschung ohne Konflikt.
	m = DreiWegeMerge(basis, nil, basis)
	erg, err = m.Ergebnis(nil)
	if err != nil || m.Hunks != 0 || erg != nil {
		t.Errorf("meine geloescht: Hunks %d, Fehler %v, Ergebnis %v, erwartet die Loeschung", m.Hunks, err, erg)
	}
	// Extern geloescht, meine unveraendert.
	m = DreiWegeMerge(basis, basis, nil)
	if erg, err = m.Ergebnis(nil); err != nil || erg != nil || m.Hunks != 0 {
		t.Errorf("extern geloescht: Hunks %d, Fehler %v, Ergebnis %v", m.Hunks, err, erg)
	}
	// Beide geloescht.
	m = DreiWegeMerge(basis, nil, nil)
	if erg, err = m.Ergebnis(nil); err != nil || erg != nil || m.Hunks != 0 {
		t.Errorf("beide geloescht: Hunks %d, Fehler %v, Ergebnis %v", m.Hunks, err, erg)
	}
}

func TestMergeLoeschenGegenAendern(t *testing.T) {
	basis := txt("a", "b", "c")
	m := DreiWegeMerge(basis, nil, txt("a", "B", "c"))
	if m.Hunks != 1 {
		t.Fatalf("%d Hunks, erwartet 1 ueber die ganze Datei", m.Hunks)
	}
	var k MergeAbschnitt
	for _, a := range m.Abschnitte {
		if a.Art == "konflikt" {
			k = a
		}
	}
	if !k.MeineGeloescht || k.ExternGeloescht || !reflect.DeepEqual(k.Extern, []string{"a", "B", "c"}) {
		t.Errorf("Hunk %+v, erwartet meine geloescht und extern a B c", k)
	}
	if _, err := m.Ergebnis(nil); err == nil {
		t.Error("ohne Wahl entschieden")
	}
	if erg, err := m.Ergebnis([]string{"meine"}); err != nil || erg != nil {
		t.Errorf("meine: %v %v, erwartet die Loeschung", erg, err)
	}
	erg, err := m.Ergebnis([]string{"extern"})
	if err != nil {
		t.Fatal(err)
	}
	if ist := zeilenVon(t, erg); !reflect.DeepEqual(ist, []string{"a", "B", "c"}) {
		t.Errorf("extern: %q", ist)
	}
	// Spiegelbildlich: extern geloescht, meine geaendert.
	m = DreiWegeMerge(basis, txt("a", "b", "c", "d"), nil)
	if m.Hunks != 1 {
		t.Fatalf("gespiegelt: %d Hunks", m.Hunks)
	}
	if erg, _ := m.Ergebnis([]string{"extern"}); erg != nil {
		t.Errorf("extern gewaehlt: %v, erwartet die Loeschung", erg)
	}
}

// Beide Seiten legen dieselbe Datei neu an (keine Merge-Basis).
func TestMergeOhneBasis(t *testing.T) {
	m := DreiWegeMerge(nil, txt("x"), txt("x"))
	if m.Hunks != 0 {
		t.Errorf("gleich neu angelegt: %d Hunks", m.Hunks)
	}
	m = DreiWegeMerge(nil, txt("x"), txt("y"))
	if m.Hunks != 1 {
		t.Errorf("verschieden neu angelegt: %d Hunks, erwartet 1", m.Hunks)
	}
}

func TestMergeLetzteZeileOhneUmbruch(t *testing.T) {
	basis := "a\nb"
	meine := "a\nB"
	m := DreiWegeMerge(&basis, &meine, &basis)
	erg, err := m.Ergebnis(nil)
	if err != nil || erg == nil || *erg != "a\nB" {
		t.Errorf("Ergebnis %v %v, erwartet \"a\\nB\" ohne Umbruch am Ende", erg, err)
	}
}

func TestVorschauDiffsJeOption(t *testing.T) {
	v := KonfliktVorschauRechnen(DateiStaende{Pfad: "b.go", Basis: txt("a", "b", "c"), Meine: txt("a", "B", "c"), Extern: txt("a", "b", "C")})
	if v.Pfad != "b.go" || v.Generiert || v.Hinweis != "" {
		t.Errorf("Kopf %+v", v)
	}
	if v.Meine.Gegen != "extern" || v.Meine.Geloescht {
		t.Errorf("meine: gegen %q", v.Meine.Gegen)
	}
	if ist, soll := diffText(v.Meine.Diff), []string{"gleich a", "weg b", "weg C", "hinzu B", "hinzu c"}; !reflect.DeepEqual(ist, soll) {
		t.Errorf("meine: %q, erwartet %q", ist, soll)
	}
	if v.Extern.Gegen != "meine" {
		t.Errorf("extern: gegen %q", v.Extern.Gegen)
	}
	if ist, soll := diffText(v.Extern.Diff), []string{"gleich a", "weg B", "weg c", "hinzu b", "hinzu C"}; !reflect.DeepEqual(ist, soll) {
		t.Errorf("extern: %q, erwartet %q", ist, soll)
	}
	if !v.Mergen.Konfliktfrei || v.Mergen.Hunks != 0 {
		t.Fatalf("mergen: %+v, erwartet konfliktfrei", v.Mergen)
	}
	if ist, soll := diffText(v.Mergen.GegenMeine), []string{"gleich a", "gleich B", "weg c", "hinzu C"}; !reflect.DeepEqual(ist, soll) {
		t.Errorf("mergen gegen meine: %q, erwartet %q", ist, soll)
	}
	if ist, soll := diffText(v.Mergen.GegenExtern), []string{"gleich a", "weg b", "hinzu B", "gleich C"}; !reflect.DeepEqual(ist, soll) {
		t.Errorf("mergen gegen extern: %q, erwartet %q", ist, soll)
	}
	var ergebnis []string
	for _, a := range v.Mergen.Abschnitte {
		if a.Art != "gleich" {
			t.Errorf("konfliktfreie Vorschau mit Abschnitt %q", a.Art)
		}
		for _, z := range a.Zeilen {
			ergebnis = append(ergebnis, z.Text)
		}
	}
	if !reflect.DeepEqual(ergebnis, []string{"a", "B", "C"}) {
		t.Errorf("mergen: Ergebnis %q, erwartet a B C", ergebnis)
	}
}

func TestVorschauMitKonfliktHunk(t *testing.T) {
	v := KonfliktVorschauRechnen(DateiStaende{Pfad: "web/src/App.svelte", Basis: txt("a", "b", "c"), Meine: txt("a", "b1", "c"), Extern: txt("a", "b2", "c")})
	if v.Mergen.Konfliktfrei || v.Mergen.Hunks != 1 {
		t.Fatalf("mergen %+v, erwartet einen Konflikt-Hunk", v.Mergen)
	}
	if len(v.Mergen.GegenMeine) != 0 || len(v.Mergen.GegenExtern) != 0 {
		t.Error("Diffs gegen beide Seiten trotz Konflikt")
	}
	arten := []string{}
	for _, a := range v.Mergen.Abschnitte {
		arten = append(arten, a.Art)
		if a.Art == "konflikt" && (a.Hunk != 0 || !reflect.DeepEqual(a.Meine, []string{"b1"}) || !reflect.DeepEqual(a.Extern, []string{"b2"}) || !reflect.DeepEqual(a.Basis, []string{"b"})) {
			t.Errorf("Hunk %+v", a)
		}
	}
	if !reflect.DeepEqual(arten, []string{"gleich", "konflikt", "gleich"}) {
		t.Errorf("Abschnitte %v", arten)
	}
}

func TestVorschauGeloescht(t *testing.T) {
	v := KonfliktVorschauRechnen(DateiStaende{Pfad: "alt.go", Basis: txt("a"), Meine: nil, Extern: txt("a", "b")})
	if !v.Meine.Geloescht || v.Extern.Geloescht {
		t.Errorf("meine geloescht %v, extern geloescht %v", v.Meine.Geloescht, v.Extern.Geloescht)
	}
	if ist := diffText(v.Meine.Diff); !reflect.DeepEqual(ist, []string{"weg a", "weg b"}) {
		t.Errorf("meine (Loeschung) gegen extern: %q", ist)
	}
	if v.Mergen.Konfliktfrei || v.Mergen.Hunks != 1 {
		t.Errorf("mergen %+v, erwartet einen Hunk Loeschen gegen Aendern", v.Mergen)
	}
}

func TestVorschauKuerztLangeStrecken(t *testing.T) {
	var basis, meine, extern []string
	for i := 0; i < 40; i++ {
		basis = append(basis, "z")
	}
	meine = append(append([]string{}, basis...), "m")
	extern = append([]string{"e"}, basis...)
	v := KonfliktVorschauRechnen(DateiStaende{Pfad: "lang.txt", Basis: txt(basis...), Meine: txt(meine...), Extern: txt(extern...)})
	if n := len(v.Meine.Diff); n > 12 {
		t.Errorf("meine: %d Diff-Zeilen, erwartet gekuerzt", n)
	}
	gefunden := false
	for _, z := range v.Meine.Diff {
		if z.Art == "ausgelassen" {
			gefunden = true
		}
	}
	if !gefunden {
		t.Error("keine Zeile ausgelassen")
	}
	zeilen := 0
	for _, a := range v.Mergen.Abschnitte {
		zeilen += len(a.Zeilen)
	}
	if zeilen > 14 {
		t.Errorf("mergen: %d Zeilen in den Abschnitten, erwartet gekuerzt", zeilen)
	}
}

func TestGenerierteDatei(t *testing.T) {
	v := KonfliktVorschauRechnen(DateiStaende{Pfad: "internal/assets/static/app/app.js", Basis: txt("a"), Meine: txt("b"), Extern: txt("c")})
	if !v.Generiert || v.Hinweis != "generiert – nach dem Lösen neu bauen" {
		t.Errorf("app.js: generiert %v, Hinweis %q", v.Generiert, v.Hinweis)
	}
	if GenerierteDatei("web/src/App.svelte") || GenerierteDatei("internal/assets/assets.go") {
		t.Error("Quelldatei als generiert erkannt")
	}
}

func TestKonfliktLoesung(t *testing.T) {
	st := DateiStaende{Pfad: "c.go", Basis: txt("a", "b", "c"), Meine: txt("a", "b1", "c"), Extern: txt("a", "b2", "c")}
	if erg, err := KonfliktLoesung(st, "meine", nil); err != nil || *erg != *st.Meine {
		t.Errorf("meine: %v %v", erg, err)
	}
	if erg, err := KonfliktLoesung(st, "extern", nil); err != nil || *erg != *st.Extern {
		t.Errorf("extern: %v %v", erg, err)
	}
	if _, err := KonfliktLoesung(st, "mergen", nil); err == nil {
		t.Error("mergen ohne Hunk-Wahl geloest")
	}
	if erg, err := KonfliktLoesung(st, "mergen", []string{"meine"}); err != nil || *erg != *st.Meine {
		t.Errorf("mergen mit meine: %v %v", erg, err)
	}
	if _, err := KonfliktLoesung(st, "beides", nil); err == nil {
		t.Error("unbekannte Wahl angenommen")
	}
	weg := DateiStaende{Pfad: "d.go", Basis: txt("a"), Meine: nil, Extern: txt("b")}
	if erg, err := KonfliktLoesung(weg, "meine", nil); err != nil || erg != nil {
		t.Errorf("meine (geloescht): %v %v, erwartet die Loeschung", erg, err)
	}
}

// Aenderungen, die sich nur beruehren, und eine Einfuegung direkt vor einer
// geaenderten Zeile der anderen Seite sind kein Konflikt; zwei verschiedene
// Einfuegungen an derselben Stelle sind einer.
func TestMergeBeruehrendeAenderungen(t *testing.T) {
	basis := txt("a", "b", "c")
	for _, fall := range []struct {
		name          string
		meine, extern *string
		soll          []string
		hunks         int
	}{
		{"einfuegen vor geaendert", txt("a", "x", "b", "c"), txt("a", "B", "c"), []string{"a", "x", "B", "c"}, 0},
		{"geaendert vor einfuegen", txt("a", "B", "c"), txt("a", "x", "b", "c"), []string{"a", "x", "B", "c"}, 0},
		{"benachbarte Zeilen", txt("A", "b", "c"), txt("a", "B", "c"), []string{"A", "B", "c"}, 0},
		{"zwei Einfuegungen", txt("a", "x", "b", "c"), txt("a", "y", "b", "c"), nil, 1},
	} {
		m := DreiWegeMerge(basis, fall.meine, fall.extern)
		if m.Hunks != fall.hunks {
			t.Errorf("%s: %d Hunks, erwartet %d", fall.name, m.Hunks, fall.hunks)
			continue
		}
		if fall.hunks > 0 {
			continue
		}
		erg, err := m.Ergebnis(nil)
		if err != nil {
			t.Errorf("%s: %v", fall.name, err)
			continue
		}
		if ist := zeilenVon(t, erg); !reflect.DeepEqual(ist, fall.soll) {
			t.Errorf("%s: %q, erwartet %q", fall.name, ist, fall.soll)
		}
	}
}
