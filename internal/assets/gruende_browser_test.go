package assets

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// Die Browser-Haelfte von gkp-2jc: jeder Auswahlgrund, den appctx erfinden kann,
// muss im Browser eine Klasse haben — und "ohne Beleg" muss als BEANSTANDUNG zu
// erkennen sein, nicht als dreizehntes graues Abzeichen.
//
// Gemessen wird am AUSGELIEFERTEN browser.js (assets.BrowserJS), und gemessen
// wird durch AUSFUEHREN unter node, nicht durch Suchen im Quelltext. Ein Test,
// der nach Zeichenketten sucht, ist in diesem Repo schon zweimal gruen gewesen,
// ohne etwas zu sichern (gkp-pf2, Spec §9.1): einmal, weil der Schnitt die
// Helfer-Definitionen mit einschloss, einmal, weil er Quelltext-Positionen
// verglich statt Ergebnisse.
//
// Die Liste der Gruende kommt aus dem GO-QUELLTEXT und steht nicht hier. Eine
// hier abgeschriebene Liste waere nach dem naechsten `const GrundX = "..."` in
// appctx still unvollstaendig — genau der Defekt, den dieser Bead behebt.

// gruendeAusGo liest jede Konstante `Grund*` mit Zeichenkettenwert aus
// internal/appctx. Der Name ist die Verabredung: appctx nennt alle Auswahlgruende
// so, und wer eine neue anlegt, wird von diesem Test eingesammelt, ohne dass
// jemand daran denken muss.
func gruendeAusGo(t *testing.T) map[string]string {
	t.Helper()
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, filepath.Join("..", "appctx"), func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("internal/appctx nicht lesbar: %v", err)
	}
	out := map[string]string{}
	for _, pkg := range pkgs {
		for _, f := range pkg.Files {
			for _, d := range f.Decls {
				gd, ok := d.(*ast.GenDecl)
				if !ok || gd.Tok != token.CONST {
					continue
				}
				for _, s := range gd.Specs {
					vs, ok := s.(*ast.ValueSpec)
					if !ok {
						continue
					}
					for i, name := range vs.Names {
						if !strings.HasPrefix(name.Name, "Grund") || i >= len(vs.Values) {
							continue
						}
						lit, ok := vs.Values[i].(*ast.BasicLit)
						if !ok || lit.Kind != token.STRING {
							continue
						}
						wert, err := strconv.Unquote(lit.Value)
						if err != nil {
							continue
						}
						out[name.Name] = wert
					}
				}
			}
		}
	}
	if len(out) < 10 {
		t.Fatalf("nur %d Gruende in appctx gefunden — heisst die Verabredung noch Grund*?", len(out))
	}
	return out
}

// jsBlock schneidet eine Deklaration aus browser.js heraus, indem es ab dem
// Anker die geschweiften Klammern zaehlt. Der Schnitt kommt damit aus der
// Sprache und nicht aus einem benachbarten Marker, der beim naechsten Umbau
// verrutscht — das war die eine Haelfte der Schnittfalle aus gkp-pf2.
//
// GRENZE, bewusst: der Zaehler kennt Zeichenketten und Kommentare, aber keine
// Regex-Literale. `/[&<>"]/g` wuerde ihn in eine Scheinzeichenkette schicken.
// Deshalb wird er nur auf Bloecke angewandt, die keine enthalten; Einzeiler wie
// esc() und rpBadge() holt jsZeile.
func jsBlock(t *testing.T, anker string) string {
	t.Helper()
	i := strings.Index(BrowserJS, anker)
	if i < 0 {
		t.Fatalf("%q steht nicht in browser.js", anker)
	}
	auf := strings.IndexByte(BrowserJS[i:], '{')
	if auf < 0 {
		t.Fatalf("hinter %q faengt kein Block an", anker)
	}
	j := i + auf
	tiefe := 0
	for ; j < len(BrowserJS); j++ {
		switch {
		case strings.HasPrefix(BrowserJS[j:], "/*"):
			e := strings.Index(BrowserJS[j+2:], "*/")
			if e < 0 {
				t.Fatalf("offener Blockkommentar hinter %q", anker)
			}
			j += 2 + e + 1
		case strings.HasPrefix(BrowserJS[j:], "//"):
			e := strings.IndexByte(BrowserJS[j:], '\n')
			if e < 0 {
				t.Fatalf("offener Zeilenkommentar hinter %q", anker)
			}
			j += e
		case BrowserJS[j] == '"' || BrowserJS[j] == '\'' || BrowserJS[j] == '`':
			q := BrowserJS[j]
			for j++; j < len(BrowserJS) && BrowserJS[j] != q; j++ {
				if BrowserJS[j] == '\\' {
					j++
				}
			}
		case BrowserJS[j] == '{':
			tiefe++
		case BrowserJS[j] == '}':
			tiefe--
			if tiefe == 0 {
				return BrowserJS[i : j+1]
			}
		}
	}
	t.Fatalf("Block hinter %q schliesst nicht", anker)
	return ""
}

// jsZeile holt einen Einzeiler samt seiner Zeile. Fuer esc() und rpBadge(), die
// beide in eine Zeile passen und beide ein Regex-Literal tragen koennen.
func jsZeile(t *testing.T, anker string) string {
	t.Helper()
	i := strings.Index(BrowserJS, anker)
	if i < 0 {
		t.Fatalf("%q steht nicht in browser.js", anker)
	}
	e := strings.IndexByte(BrowserJS[i:], '\n')
	if e < 0 {
		return BrowserJS[i:]
	}
	return BrowserJS[i : i+e]
}

type grundErgebnis struct {
	Klasse string `json:"klasse"`
	HTML   string `json:"html"`
}

// avGruendeAusfuehren laesst die ausgelieferten Helfer unter node auf jeden
// uebergebenen Grund los und gibt zurueck, was dabei herauskam.
func avGruendeAusfuehren(t *testing.T, gruende []string, mitAbzeichen bool) map[string]grundErgebnis {
	t.Helper()
	nodePath, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node nicht installiert")
	}
	eingabe, err := json.Marshal(gruende)
	if err != nil {
		t.Fatal(err)
	}
	teile := []string{
		jsZeile(t, "function esc(s) {"),
		jsZeile(t, "function rpBadge(cls, txt) {"),
	}
	// Die Tabelle ist optional, damit der rote Lauf dieses Tickets die NUETZLICHE
	// Meldung gibt ("morgen hat keine Klasse") und nicht bloss "Tabelle fehlt".
	if strings.Contains(BrowserJS, avGrundTabelle) {
		teile = append(teile, jsBlock(t, avGrundTabelle))
	}
	teile = append(teile, jsBlock(t, "function avGrundKlasse(g) {"))
	if mitAbzeichen {
		teile = append(teile, jsBlock(t, "function avGrundAbzeichen(g) {"))
	} else {
		// Wer nur nach der Klasse fragt, braucht das Abzeichen nicht — dann darf
		// sein Fehlen den Klassen-Test auch nicht rot faerben.
		teile = append(teile, "function avGrundAbzeichen(g) { return rpBadge(avGrundKlasse(g), g); }")
	}
	script := strings.Join(teile, "\n") + `
var GRUENDE = ` + string(eingabe) + `;
GRUENDE.push("dieser Grund ist frei erfunden");
var out = {};
GRUENDE.forEach(function (g) { out[g] = { klasse: avGrundKlasse(g), html: avGrundAbzeichen(g) }; });
process.stdout.write(JSON.stringify(out));
`
	pfad := filepath.Join(t.TempDir(), "av_gruende.js")
	if err := os.WriteFile(pfad, []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(nodePath, pfad)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	roh, err := cmd.Output()
	if err != nil {
		t.Fatalf("node-Lauf fehlgeschlagen: %v\n%s", err, stderr.String())
	}
	var erg map[string]grundErgebnis
	if err := json.Unmarshal(roh, &erg); err != nil {
		t.Fatalf("node-Ausgabe nicht lesbar: %v\n%s", err, roh)
	}
	return erg
}

// grundErfunden ist der Grund, den es in appctx nicht gibt. Er muss im Browser
// anders herauskommen als jeder echte — sonst waere "hat eine Klasse" nicht
// pruefbar, weil ein Rueckfall-Wert jede Luecke zudeckte.
const grundErfunden = "dieser Grund ist frei erfunden"

// avGrundTabelle ist der Anker der Zuordnungstabelle in browser.js.
const avGrundTabelle = "var AV_GRUND = "

// farbklassen sind die vier, die es vor diesem Bead schon gab. Sie sind der
// KNAPPE Teil der Gestaltung: Farbe sagt Termindruck oder Arbeitsstand, und nur
// das.
var farbklassen = map[string]bool{"r": true, "a": true, "c": true, "g": true}

func klassenTeile(k string) []string { return strings.Fields(k) }

// ohneTags laesst nur uebrig, was der Leser ohne Hinzeigen sieht.
func ohneTags(html string) string {
	var b strings.Builder
	drin := false
	for _, r := range html {
		switch {
		case r == '<':
			drin = true
		case r == '>':
			drin = false
		case !drin:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// TestAvGrundKlasseKenntJedenGrund ist das Abnahmekriterium: jeder Grund hat
// eine Klasse. Gemessen gegen die Konstanten in appctx, nicht gegen eine hier
// abgeschriebene Liste.
func TestAvGrundKlasseKenntJedenGrund(t *testing.T) {
	ausGo := gruendeAusGo(t)
	var werte []string
	for _, v := range ausGo {
		werte = append(werte, v)
	}
	sort.Strings(werte)
	erg := avGruendeAusfuehren(t, werte, false)

	unbekannt := erg[grundErfunden].Klasse
	if unbekannt == "" {
		t.Error("ein unbekannter Grund bekommt die leere Klasse — dann ist eine Luecke von einer Einordnung nicht zu unterscheiden")
	}
	for name, wert := range ausGo {
		k := erg[wert].Klasse
		switch {
		case k == "":
			t.Errorf("%s (%q) hat keine Klasse — erscheint neutral", name, wert)
		case k == unbekannt:
			t.Errorf("%s (%q) faellt auf die Unbekannt-Klasse %q zurueck — nicht eingeordnet", name, wert, k)
		}
	}
}

// TestAvGrundFarbeBleibtKnapp haelt die Entscheidung fest, die diesen Bead von
// "dreizehn bunte Abzeichen" trennt: Farbe bekommt nur, wer sie vorher schon
// hatte. Ohne diese Zusage waere der naechstliegende Fix — jedem Grund eine
// Farbe — gruen, und die Beanstandung ginge im Teppich unter.
func TestAvGrundFarbeBleibtKnapp(t *testing.T) {
	ausGo := gruendeAusGo(t)
	var werte []string
	for _, v := range ausGo {
		werte = append(werte, v)
	}
	sort.Strings(werte)
	erg := avGruendeAusfuehren(t, werte, false)

	var farbig []string
	for _, wert := range werte {
		for _, teil := range klassenTeile(erg[wert].Klasse) {
			if farbklassen[teil] {
				farbig = append(farbig, wert)
				break
			}
		}
	}
	sort.Strings(farbig)
	// Die vier Heute-Gruende plus die eine Beanstandung. Mehr Farbe als das ist
	// keine Hervorhebung mehr, sondern Tapete.
	will := []string{
		ausGo["GrundFaelligHeute"],
		ausGo["GrundInArbeit"],
		ausGo["GrundOhneBeleg"],
		ausGo["GrundStartklar"],
		ausGo["GrundUeberfaellig"],
	}
	sort.Strings(will)
	if strings.Join(farbig, "|") != strings.Join(will, "|") {
		t.Errorf("farbig ist %v, erwartet %v — Farbe ist knapp: vier Termin-/Standgruende und die Beanstandung", farbig, will)
	}
}

// TestAvGrundOhneBelegIstBeanstandung ist der Kern. "ohne Beleg" ist keine
// Einordnung, sondern ein Befund: geschlossen ohne aktuellen done-Eintrag,
// dieselbe Luecke, die beads_query op=gaps meldet.
//
// Geprueft wird der Unterschied und nicht die Farbe. Ein Abzeichen, das sich
// nur durch Farbe unterscheidet, ist fuer jeden Zehnten unsichtbar und in einem
// Ausdruck fuer alle.
func TestAvGrundOhneBelegIstBeanstandung(t *testing.T) {
	ausGo := gruendeAusGo(t)
	beleg := ausGo["GrundOhneBeleg"]
	if beleg == "" {
		t.Fatal("GrundOhneBeleg steht nicht mehr in appctx/logbuch.go")
	}
	var werte []string
	for _, v := range ausGo {
		werte = append(werte, v)
	}
	sort.Strings(werte)
	erg := avGruendeAusfuehren(t, werte, true)

	mangel := ""
	for _, teil := range klassenTeile(erg[beleg].Klasse) {
		if strings.HasPrefix(teil, "av-") {
			mangel = teil
		}
	}
	if mangel == "" {
		t.Fatalf("%q traegt keine eigene av-Klasse, sondern %q", beleg, erg[beleg].Klasse)
	}
	for _, wert := range werte {
		if wert == beleg {
			continue
		}
		if strings.Contains(" "+erg[wert].Klasse+" ", " "+mangel+" ") {
			t.Errorf("%q traegt die Beanstandungsklasse %q — dann ist sie keine Kategorie, sondern Dekoration", wert, mangel)
		}
	}

	// Der Unterschied muss auch ohne Farbe ankommen: ein Zeichen im Text und
	// ein Titel, der sagt, was beanstandet wird.
	html := erg[beleg].HTML
	if !strings.Contains(html, "title=") {
		t.Errorf("das Abzeichen fuer %q sagt nicht, was es beanstandet: %s", beleg, html)
	}
	// Echt entmarkupt und nicht bloss aufgelockert: ein ▲ im title-Attribut
	// waere sonst durchgegangen, und ein Titel sieht man erst beim Zeigen.
	if !strings.Contains(ohneTags(html), "▲") {
		t.Errorf("das Abzeichen fuer %q traegt kein Zeichen und ist damit nur durch Farbe zu erkennen: %s", beleg, html)
	}
	for _, wert := range werte {
		if wert == beleg {
			continue
		}
		if strings.Contains(erg[wert].HTML, "▲") {
			t.Errorf("auch %q traegt das Beanstandungszeichen — dann sagt es nichts mehr", wert)
		}
	}
}

// TestAvGrundKlassenSindGestaltet schliesst die Luecke zwischen Verhalten und
// Aussehen: eine Klasse, die kein Stylesheet kennt, ist ein leeres Versprechen.
// Der node-Lauf oben saehe davon nichts.
func TestAvGrundKlassenSindGestaltet(t *testing.T) {
	ausGo := gruendeAusGo(t)
	var werte []string
	for _, v := range ausGo {
		werte = append(werte, v)
	}
	erg := avGruendeAusfuehren(t, append(werte, grundErfunden), false)

	css := BrowserCSS + "\n" + DashCSS
	gesehen := map[string]bool{}
	for _, e := range erg {
		for _, teil := range klassenTeile(e.Klasse) {
			if !strings.HasPrefix(teil, "av-") || gesehen[teil] {
				continue
			}
			gesehen[teil] = true
			// ".badge."+teil und nicht "."+teil: die lose Form war gruen, obwohl
			// .badge.av-mangel geloescht war — .av-z.av-mangel (die Bilanz im Kopf)
			// enthaelt dieselbe Zeichenkette. Gefragt ist die Regel, die das
			// ABZEICHEN gestaltet, und die traegt beide Klassen.
			if !strings.Contains(css, ".badge."+teil) {
				t.Errorf("die Klasse %q wird an Abzeichen vergeben, aber keine .badge-Regel gestaltet sie", teil)
			}
		}
	}
	if len(gesehen) == 0 {
		t.Error("kein Grund traegt eine av-Klasse — dann gibt es die Kategorie nicht")
	}
}
