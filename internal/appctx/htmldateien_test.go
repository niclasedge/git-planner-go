package appctx

// HTML-Dateien unter "Geaenderte Bilder" (html-dateien, #112; aus dem Baum,
// html-dateien-baum, #114): Auswahl aus dem Baum der Spitze nach der letzten
// Aenderung, die neuesten 10, Obergrenze 200, Erstellt-Datum aus added und
// aus dem Verlauf, Bildzaehler.

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

func hdCommit(sha, zeit, betreff string, dateien ...ghrest.CommitDatei) ghrest.CommitMitDateien {
	return ghrest.CommitMitDateien{CommitKopf: ghrest.CommitKopf{SHA: sha, Zeit: zeit, Betreff: betreff}, Dateien: dateien}
}

// hdZeit ist die Zeit von Commit hN: h12 am 2026-09-27, h1 am 2026-09-16.
func hdZeit(n int) string { return fmt.Sprintf("2026-09-%02dT10:30:00Z", 15+n) }

func hdBlob(pfad, sha string) ghrest.BaumEintrag {
	return ghrest.BaumEintrag{Pfad: pfad, Typ: "blob", SHA: sha}
}

// hdCommits ist das Commit-Fenster (die letzten 30 Commits) des Szenarios
// "Zwoelf HTML-Dateien": h12 bis h1, je ein Tag Abstand.
func hdCommits() []ghrest.CommitMitDateien {
	var out []ghrest.CommitMitDateien
	for n := 12; n >= 1; n-- {
		status := "added"
		if n == 12 {
			status = "modified"
		}
		var dateien []ghrest.CommitDatei
		if n == 12 {
			dateien = append(dateien,
				ghrest.CommitDatei{Pfad: "alt/nachher.htm", AlterPfad: "alt/vorher.HTM", SHA: "b-nachher", Status: "renamed"},
				ghrest.CommitDatei{Pfad: "alt/weg.html", Status: "removed"})
		}
		if n == 3 {
			dateien = append(dateien, ghrest.CommitDatei{Pfad: "alt/vorher.HTM", SHA: "b-vorher", Status: "added"})
		}
		if n == 2 {
			dateien = append(dateien, ghrest.CommitDatei{Pfad: "alt/weg.html", SHA: "b-weg", Status: "added"})
		}
		dateien = append(dateien,
			ghrest.CommitDatei{Pfad: fmt.Sprintf("berichte/b-%d.html", n), SHA: fmt.Sprintf("b-%d", n), Status: status},
			ghrest.CommitDatei{Pfad: "berichte/bild.png", SHA: fmt.Sprintf("p-%d", n), Status: "modified"})
		out = append(out, hdCommit(fmt.Sprintf("h%d", n), hdZeit(n), fmt.Sprintf("Bericht %d", n), dateien...))
	}
	// Die Folge von GitHub ist nicht massgeblich, die Zeit schon.
	out[3], out[5] = out[5], out[3]
	return out
}

// hdBaum ist der Baum an der Spitze h12: die zwoelf Berichte, die
// umbenannte Datei und ein Bild; alt/weg.html und alt/vorher.HTM stehen
// nicht mehr darin.
func hdBaum() ([]ghrest.BaumEintrag, map[string]ghrest.CommitKopf) {
	baum := []ghrest.BaumEintrag{
		{Pfad: "alt", Typ: "tree", SHA: "t-alt"},
		{Pfad: "berichte", Typ: "tree", SHA: "t-berichte"},
		hdBlob("alt/nachher.htm", "b-nachher"),
		hdBlob("berichte/bild.png", "p-12"),
	}
	letzte := map[string]ghrest.CommitKopf{
		"alt/nachher.htm": {SHA: "h12", Zeit: hdZeit(12), Betreff: "Bericht 12"},
	}
	for n := 1; n <= 12; n++ {
		p := fmt.Sprintf("berichte/b-%d.html", n)
		baum = append(baum, hdBlob(p, fmt.Sprintf("b-%d", n)))
		letzte[p] = ghrest.CommitKopf{SHA: fmt.Sprintf("h%d", n), Zeit: hdZeit(n), Betreff: fmt.Sprintf("Bericht %d", n)}
	}
	return baum, letzte
}

func TestHTMLDateienAusBaumZwoelf(t *testing.T) {
	baum, letzte := hdBaum()
	d, grund := HTMLDateienAusBaum("acme/alpha", "h12", baum, letzte, hdCommits())
	if grund != "" {
		t.Errorf("Grund %q, erwartet keinen", grund)
	}
	var pfade []string
	for _, e := range d {
		pfade = append(pfade, e.Pfad)
	}
	erwartet := []string{"alt/nachher.htm", "berichte/b-12.html"}
	for n := 11; n >= 4; n-- {
		erwartet = append(erwartet, fmt.Sprintf("berichte/b-%d.html", n))
	}
	if !reflect.DeepEqual(pfade, erwartet) {
		t.Fatalf("Pfade %v, erwartet %v", pfade, erwartet)
	}
	b11 := d[2]
	if b11.Name != "b-11.html" || b11.SHA != "b-11" || b11.Commit != "h11" || b11.Zeit != hdZeit(11) || b11.Betreff != "Bericht 11" {
		t.Errorf("b-11 %+v", b11)
	}
	if b11.Erstellt != hdZeit(11) || b11.ErstelltCommit != "h11" {
		t.Errorf("b-11 erstellt %q (%q), erwartet die Zeit von h11", b11.Erstellt, b11.ErstelltCommit)
	}
	if b11.Adresse != "/api/datei/acme/alpha/h11/berichte/b-11.html" {
		t.Errorf("Adresse %q", b11.Adresse)
	}
	if b11.Link != "https://github.com/acme/alpha/blob/h11/berichte/b-11.html" {
		t.Errorf("Link %q", b11.Link)
	}
	if d[1].Erstellt != "" || d[0].Erstellt != "" {
		t.Errorf("ohne added kein erstellt aus den Commits: %q, %q", d[0].Erstellt, d[1].Erstellt)
	}
	if d[0].Commit != "h12" || d[1].Commit != "h12" || d[0].SHA != "b-nachher" {
		t.Errorf("Eintraege %+v, %+v, erwartet h12", d[0], d[1])
	}
	if !IstHTMLPfad("a/B.HTM") || !IstHTMLPfad("x.html") || IstHTMLPfad("x.xhtml") || IstHTMLPfad("html") {
		t.Error("IstHTMLPfad")
	}
}

// Die Datei, deren letzte Aenderung weit vor den letzten 30 Commits liegt,
// steht trotzdem da: die Quelle ist der Baum, nicht das Commit-Fenster.
func TestHTMLDateienAusBaumAlteDatei(t *testing.T) {
	baum := []ghrest.BaumEintrag{
		hdBlob("docs/design/alt.html", "b-alt"),
		hdBlob("berichte/b.HTM", "b-b"),
		hdBlob("docs/plan.png", "b-plan"),
		{Pfad: "docs/design", Typ: "tree", SHA: "t-design"},
	}
	letzte := map[string]ghrest.CommitKopf{
		"docs/design/alt.html": {SHA: "a0", Zeit: "2026-03-01T09:00:00Z", Betreff: "Entwurf G angelegt"},
		"berichte/b.HTM":       {SHA: "h2", Zeit: hdZeit(2), Betreff: "Bericht nachgezogen"},
	}
	fenster := []ghrest.CommitMitDateien{
		hdCommit("h2", hdZeit(2), "Bericht nachgezogen",
			ghrest.CommitDatei{Pfad: "berichte/b.HTM", SHA: "b-b", Status: "modified"},
			ghrest.CommitDatei{Pfad: "alt/weg.html", Status: "removed"}),
		hdCommit("h1", hdZeit(1), "Bericht angelegt",
			ghrest.CommitDatei{Pfad: "berichte/b.HTM", SHA: "b-b0", Status: "added"},
			ghrest.CommitDatei{Pfad: "alt/weg.html", SHA: "b-weg", Status: "added"}),
	}
	d, grund := HTMLDateienAusBaum("acme/alpha", "s", baum, letzte, fenster)
	if grund != "" {
		t.Errorf("Grund %q", grund)
	}
	if len(d) != 2 || d[0].Pfad != "berichte/b.HTM" || d[1].Pfad != "docs/design/alt.html" {
		t.Fatalf("Dateien %+v, erwartet berichte/b.HTM, docs/design/alt.html", d)
	}
	b := d[0]
	if b.Commit != "h2" || b.SHA != "b-b" || b.Erstellt != hdZeit(1) || b.ErstelltCommit != "h1" {
		t.Errorf("berichte/b.HTM %+v, erwartet aus h2, Blob b-b, erstellt von h1", b)
	}
	a := d[1]
	if a.Commit != "a0" || a.SHA != "b-alt" || a.Zeit != "2026-03-01T09:00:00Z" || a.Betreff != "Entwurf G angelegt" || a.Name != "alt.html" {
		t.Errorf("docs/design/alt.html %+v, erwartet aus a0 mit Blob b-alt", a)
	}
	if a.Erstellt != "" {
		t.Errorf("docs/design/alt.html erstellt %q, erwartet keins aus den Commits", a.Erstellt)
	}
	if a.Adresse != "/api/datei/acme/alpha/a0/docs/design/alt.html" || a.Link != "https://github.com/acme/alpha/blob/a0/docs/design/alt.html" {
		t.Errorf("Adresse %q, Link %q", a.Adresse, a.Link)
	}
}

// Ohne letzte Aenderung zeigt die Adresse auf die Spitze und die Datei
// steht hinten.
func TestHTMLDateienAusBaumOhneLetzte(t *testing.T) {
	baum := []ghrest.BaumEintrag{hdBlob("a.html", "b-a"), hdBlob("z.html", "b-z")}
	letzte := map[string]ghrest.CommitKopf{"z.html": {SHA: "h1", Zeit: hdZeit(1), Betreff: "z"}}
	d, _ := HTMLDateienAusBaum("acme/alpha", "spitze", baum, letzte, nil)
	if len(d) != 2 || d[0].Pfad != "z.html" || d[1].Pfad != "a.html" {
		t.Fatalf("Dateien %+v, erwartet z.html vor a.html", d)
	}
	if d[1].Commit != "spitze" || d[1].Zeit != "" || d[1].Adresse != "/api/datei/acme/alpha/spitze/a.html" {
		t.Errorf("a.html %+v, erwartet die Spitze und keine Zeit", d[1])
	}
}

func TestHTMLDateienAusBaumObergrenze(t *testing.T) {
	var baum []ghrest.BaumEintrag
	letzte := map[string]ghrest.CommitKopf{}
	// Absichtlich rueckwaerts: die Folge des Baums ist nicht massgeblich.
	for n := 205; n >= 1; n-- {
		p := fmt.Sprintf("seiten/s-%03d.html", n)
		baum = append(baum, hdBlob(p, fmt.Sprintf("b-%03d", n)))
		letzte[p] = ghrest.CommitKopf{SHA: fmt.Sprintf("c%03d", n), Zeit: fmt.Sprintf("2026-01-01T%02d:%02d:00Z", n/60, n%60)}
	}
	pfade, grund := HTMLPfadeAusBaum(baum)
	if len(pfade) != HTMLBaumDeckel || HTMLBaumDeckel != 200 || pfade[0] != "seiten/s-001.html" || pfade[199] != "seiten/s-200.html" {
		t.Fatalf("%d Pfade (%v … %v), erwartet seiten/s-001.html bis seiten/s-200.html", len(pfade), pfade[:1], pfade[len(pfade)-1:])
	}
	if !strings.Contains(grund, "205") || !strings.Contains(grund, "200") {
		t.Errorf("Grund %q, erwartet 205 im Baum und die ersten 200", grund)
	}
	d, grund2 := HTMLDateienAusBaum("acme/alpha", "s", baum, letzte, nil)
	if grund2 != grund {
		t.Errorf("Grund der Auswahl %q, erwartet %q", grund2, grund)
	}
	var erhalten []string
	for _, e := range d {
		erhalten = append(erhalten, e.Pfad)
	}
	var erwartet []string
	for n := 200; n >= 191; n-- {
		erwartet = append(erwartet, fmt.Sprintf("seiten/s-%03d.html", n))
	}
	if !reflect.DeepEqual(erhalten, erwartet) {
		t.Errorf("Pfade %v, erwartet %v", erhalten, erwartet)
	}
	if p, g := HTMLPfadeAusBaum([]ghrest.BaumEintrag{hdBlob("README.md", "r"), hdBlob("docs/plan.png", "p")}); len(p) != 0 || g != "" {
		t.Errorf("Baum ohne HTML: %v %q, erwartet leer ohne Grund", p, g)
	}
}

func TestHTMLDateiAdresseMaskiert(t *testing.T) {
	if a := DateiAdresse("acme/alpha", "k2", "docs/mit leer/ä#1.html"); a != "/api/datei/acme/alpha/k2/docs/mit%20leer/%C3%A4%231.html" {
		t.Errorf("Adresse %q", a)
	}
}

func TestErstelltAusVerlauf(t *testing.T) {
	verlauf := []ghrest.CommitKopf{{SHA: "h12", Zeit: hdZeit(12)}, {SHA: "v2", Zeit: "2026-08-02T08:00:00Z"}, {SHA: "v1", Zeit: "2026-08-01T08:00:00Z"}}
	zeit, commit := ErstelltAusVerlauf(verlauf, nil)
	if zeit != "2026-08-01T08:00:00Z" || commit != "v1" {
		t.Errorf("aus dem Verlauf %q %q, erwartet v1", zeit, commit)
	}
	if zeit, commit := ErstelltAusVerlauf(nil, errors.New("kaputt")); zeit != "" || commit != "" {
		t.Errorf("gescheiterter Verlauf ergibt %q %q, erwartet leer", zeit, commit)
	}
	if zeit, _ := ErstelltAusVerlauf(nil, nil); zeit != "" {
		t.Errorf("leerer Verlauf ergibt %q", zeit)
	}
}

func imBaum(pfade ...string) func(string) bool {
	m := map[string]bool{}
	for _, p := range pfade {
		m[p] = true
	}
	return func(p string) bool { return m[p] }
}

func TestHTMLBilderZaehlenVierEinesFehlt(t *testing.T) {
	html := `<!DOCTYPE html><html><head><style>.k { background: url('bilder/fehlt.png') }</style></head>
<body><!-- <img src="nie.png"> -->
<img src="bilder/a.png" alt="a">
<IMG SRC="data:image/png;base64,AAAA">
<picture><source srcset="../gemeinsam/c.png 2x"><img src="bilder/a.png?v=2#oben"></picture>
<script>var s = '<img src="skript.png">';</script>
</body></html>`
	z := HTMLBilderZaehlen("berichte/b.html", []byte(html), imBaum("berichte/bilder/a.png", "gemeinsam/c.png"))
	if z.Gesamt != 4 || z.Vorhanden != 3 || z.Fehlt != 1 || z.Extern != 0 || z.Eingebettet != 1 {
		t.Errorf("Zaehler %+v, erwartet 4 gesamt, 3 vorhanden, 1 fehlt, 1 eingebettet", z)
	}
	if !reflect.DeepEqual(z.Fehlende, []string{"berichte/bilder/fehlt.png"}) {
		t.Errorf("fehlend %v", z.Fehlende)
	}
	if z.Text != "4 Bilder · 3 vorhanden · 1 fehlt" {
		t.Errorf("Text %q", z.Text)
	}
}

func TestHTMLBilderZaehlenTexte(t *testing.T) {
	baum := imBaum("docs/plan.svg", "a.png", "x/y.png", "x/z.png")
	faelle := []struct {
		name, pfad, html, text string
	}{
		{"alle", "index.html", `<svg><image href="/docs/plan.svg"/></svg><img src="docs/plan.svg">`, "1 Bild · alle vorhanden"},
		{"extern", "index.html", `<img src="https://example.org/x.png"><img src='a.png'>`, "2 Bilder · 1 vorhanden · 1 extern"},
		{"keine", "index.html", `<p>nur Text</p>`, "keine Bilder"},
		{"srcset", "x/i.htm", `<source srcset="y.png 1x, z.png 2x">`, "2 Bilder · alle vorhanden"},
		{"xlink und style", "x/i.html", `<svg><image xlink:href="y.png"/></svg><div style="background-image: url(&quot;z.png&quot;)"></div>`, "2 Bilder · alle vorhanden"},
		{"ausbruch", "i.html", `<img src="../../geheim.png"><img src="//cdn.example.org/a.png">`, "2 Bilder · 0 vorhanden · 1 extern · 1 fehlt"},
		{"leerzeichen", "x/i.html", `<img src="mit%20leer.png">`, "1 Bild · 0 vorhanden · 1 fehlt"},
	}
	for _, f := range faelle {
		z := HTMLBilderZaehlen(f.pfad, []byte(f.html), baum)
		if z.Text != f.text {
			t.Errorf("%s: Text %q, erwartet %q (%+v)", f.name, z.Text, f.text, z)
		}
	}
	z := HTMLBilderZaehlen("x/i.html", []byte(`<img src="mit%20leer.png">`), baum)
	if len(z.Fehlende) != 1 || z.Fehlende[0] != "x/mit leer.png" {
		t.Errorf("fehlend %v, erwartet x/mit leer.png", z.Fehlende)
	}
	if z := HTMLBilderZaehlen("i.html", nil, baum); z.Fehlende == nil || !strings.Contains(z.Text, "keine") {
		t.Errorf("leere Datei %+v, erwartet keine Bilder und eine leere (nicht nil) Liste", z)
	}
}
