package assets

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Die Browser-Haelfte von gkp-7gb. Zwei Fragen, beide am AUSGELIEFERTEN Code
// und nicht an einer danebengelegten Kopie — eine Kopie driftet, und ein Test
// an einer Kopie bleibt nach der Regression gruen (dieselbe Bauart wie
// TestRpTeilbaumEinsetzen).

const avStart = "/* ---------- Aufgaben-Ansichten (gkp-7gb, Spec §5) ----------"
const avEnde = `avAnmelden("heute", { label: "Heute" });`

func ansichtenBlock(t *testing.T) string {
	t.Helper()
	start := strings.Index(BrowserJS, avStart)
	end := strings.Index(BrowserJS, avEnde)
	if start < 0 || end < 0 || end <= start {
		t.Fatalf("Marker um den Ansichten-Block nicht gefunden — beim Umbauen die Marker hier mitziehen")
	}
	return ohneKommentare(BrowserJS[start : end+len(avEnde)])
}

// ohneKommentare schneidet Kommentare und Zeichenketten heraus, bevor der Block
// nach Rechenoperationen durchsucht wird.
//
// Sonst schlaegt der Test bei genau dem Kommentar an, der erklaert, warum hier
// nicht sortiert wird ("frueher stand hier ein .sort()") — und der einzige
// Ausweg waere, die Erklaerung zu verstuemmeln. Ein Test, der dazu zwingt,
// schlechter zu kommentieren, wird abgeschaltet.
func ohneKommentare(js string) string {
	var b strings.Builder
	for i := 0; i < len(js); i++ {
		switch {
		case strings.HasPrefix(js[i:], "/*"):
			if e := strings.Index(js[i+2:], "*/"); e >= 0 {
				i += 2 + e + 1
				continue
			}
			return b.String()
		case strings.HasPrefix(js[i:], "//"):
			e := strings.IndexByte(js[i:], '\n')
			if e < 0 {
				return b.String()
			}
			i += e - 1
		case js[i] == '"' || js[i] == '\'':
			q := js[i]
			for i++; i < len(js) && js[i] != q; i++ {
				if js[i] == '\\' {
					i++
				}
			}
		default:
			b.WriteByte(js[i])
		}
	}
	return b.String()
}

// ohneJsKommentare schneidet NUR Kommentare heraus und laesst Zeichenketten
// stehen — die Gegenfrage zu ohneKommentare darueber.
//
// Zwei Helfer und kein Schalter, weil die beiden Fragen entgegengesetzt sind:
// wer CODE auf Rechenoperationen absucht, muss Text ausblenden; wer nach einem
// TEXT sucht, muss ihn behalten. Ein Helfer mit Flagge waere in der falschen
// Stellung lautlos wirkungslos — der Test bliebe gruen und haette nichts
// angesehen.
func ohneJsKommentare(js string) string {
	var b strings.Builder
	for i := 0; i < len(js); i++ {
		switch {
		case strings.HasPrefix(js[i:], "/*"):
			e := strings.Index(js[i+2:], "*/")
			if e < 0 {
				return b.String()
			}
			i += 2 + e + 1
		case strings.HasPrefix(js[i:], "//"):
			e := strings.IndexByte(js[i:], '\n')
			if e < 0 {
				return b.String()
			}
			i += e - 1
		case js[i] == '"' || js[i] == '\'':
			q := js[i]
			b.WriteByte(js[i])
			for i++; i < len(js) && js[i] != q; i++ {
				b.WriteByte(js[i])
				if js[i] == '\\' && i+1 < len(js) {
					i++
					b.WriteByte(js[i])
				}
			}
			if i < len(js) {
				b.WriteByte(js[i])
			}
		default:
			b.WriteByte(js[i])
		}
	}
	return b.String()
}

// TestAnsichtenBlockRechnetNicht ist die Regel aus CLAUDE.md, als Test statt
// als Bitte: "Auswahl und Sortierung gehoeren nach Go, nicht in browser.js."
//
// Bei einer Ansicht namens "Heute" ist die Versuchung am groessten, weil
// "heute" wie eine Browserfrage aussieht. Ist es nicht: die beiden Datumsfelder
// tragen ein Z, das luegt (Spec §4.4), ein `new Date(...)` darauf verschoebe
// jeden Termin um den UTC-Versatz — und die Auswahl waere ohne Browser nicht
// mehr pruefbar, was das Abnahmekriterium dieses Tickets ausdruecklich
// verlangt.
//
// Deshalb darf in diesem Block nichts davon vorkommen. Die Zeilenfolge kommt
// aus a.eintraege, so wie Go sie sortiert hat.
func TestAnsichtenBlockRechnetNicht(t *testing.T) {
	block := ansichtenBlock(t)
	verboten := map[string]string{
		".sort(":     "sortiert wird in appctx/ansicht.go (sortiereGruppen), nicht hier",
		".filter(":   "ausgewaehlt wird in appctx/ansicht.go (Regel.Waehlt), nicht hier",
		"new Date(":  "Termine kommen bereits gedeutet als Ortszeit-Text (AnsichtEintrag.Frist)",
		"Date.parse": "dito — bds Z luegt, die Deutung steht in bdops.ParseUserDateIn",
		"Date.now(":  "welcher Tag gemeint ist, sagt der Rumpf (AufgabenAnsicht.Tag)",
		// gkp-dop: die naheliegende Browser-Kappung waere `a.eintraege.slice(0, 100)`.
		// Sie waere eine zweite Auswahl neben der von Go — und eine, die mitten in
		// einem Abschlusstag schnitte, waehrend appctx an der Tagesgrenze kappt
		// (Regel.Grenze). Die Liste im Rumpf ist bereits die gekappte.
		".slice(": "gekappt wird in appctx/ansicht.go (kappen, Regel.Grenze), nicht hier",
	}
	for muster, warum := range verboten {
		if strings.Contains(block, muster) {
			t.Errorf("der Ansichten-Block enthaelt %q — %s", muster, warum)
		}
	}
	// Die Gegenprobe zum Verbot: der Block muss die Zeilen ueberhaupt aus dem
	// gelieferten Rumpf nehmen. Ohne diese Zusage waeren die Verbote oben auch
	// dann erfuellt, wenn er gar nichts zeichnet.
	for _, noetig := range []string{"a.eintraege.forEach", "REPORTS.ansichten"} {
		if !strings.Contains(block, noetig) {
			t.Errorf("der Ansichten-Block enthaelt %q nicht — zeichnet er die Auswahl aus Go?", noetig)
		}
	}
}

// TestKappungssatzKommtAusGo ist der Zwilling von TestLeersatzKommtAusGo, und
// er hat denselben Anlass eine Stufe weiter (gkp-dop).
//
// Warum eine Liste aufhoert, ist eine Aussage ueber die AUSWAHL, und die trifft
// Go. Der Satz traegt ausserdem Zahlen, die nur der Rahmen kennt: wie viele
// Zeilen es insgesamt gab und wie viele Gruppen uebrig sind. Im Browser
// nachgerechnet waeren das zwei Zaehlungen nebeneinander — genau der Defekt aus
// gkp-d52, dort mit 47 gegen 57.
//
// Die zweite Haelfte ist wieder die wichtigere: es darf kein Ersatzsatz im
// Browser stehen. Sonst bliebe der Test gruen, waehrend eine Ansicht ohne
// gesetzte Grenze eine Kappung behauptet, die es nicht gibt.
func TestKappungssatzKommtAusGo(t *testing.T) {
	if block := ansichtenBlock(t); !strings.Contains(block, "a.gekappt") {
		t.Error("der Ansichten-Block liest a.gekappt nicht — eine gekappte Liste waere " +
			"dann von einer zu Ende gegangenen nicht zu unterscheiden (appctx: Regel.Grenze)")
	}
	js := ohneJsKommentare(BrowserJS)
	for _, verboten := range []string{"Ältere Tage", "gekappt bei", "weitere Aufgaben"} {
		if strings.Contains(js, verboten) {
			t.Errorf("der Browser formuliert den Kappungssatz selbst (%q) — die Worte "+
				"gehoeren zur Regel (appctx: Regel.GrenzeSatz), hier wird nur gezeichnet", verboten)
		}
	}
}

// TestSelectKenntKeinenEinzelnenAnsichtsnamen haelt die Naht fuer die drei
// Folgetickets (gkp-1oj, gkp-pf2, gkp-8ww) fest: select() greift ueber die
// Registrierung zu, nicht ueber einen Namen. Stuende dort `id === "heute"`,
// waere die naechste Ansicht ein weiterer Zweig in derselben Zeile — und drei
// gleichzeitig laufende Agenten schrieben sich darin gegenseitig um.
func TestSelectKenntKeinenEinzelnenAnsichtsnamen(t *testing.T) {
	start := strings.Index(BrowserJS, "function select(id) {")
	if start < 0 {
		t.Fatal("select() nicht gefunden")
	}
	end := strings.Index(BrowserJS[start:], "\n  }\n")
	if end < 0 {
		t.Fatal("Ende von select() nicht gefunden")
	}
	sel := BrowserJS[start : start+end]
	if !strings.Contains(sel, "AV_REG[id]") {
		t.Error("select() greift nicht ueber AV_REG zu — dann ist die Ansicht nicht erreichbar")
	}
	if strings.Contains(sel, `"heute"`) {
		t.Error(`select() nennt "heute" beim Namen — eine weitere Ansicht braeuchte dann einen weiteren Zweig`)
	}
}

// TestLeersatzKommtAusGo ist die Browser-Haelfte von Befund D (gkp-3oh).
//
// Der Satz einer leeren Liste stand generisch hier: "Nichts offen — die Liste
// ist leer, nicht kaputt." Fuer fuenf der sechs Ansichten stimmte er, im
// Logbuch behauptete er das Gegenteil dessen, was gemeint war — dort ist nichts
// GESCHLOSSEN. Er gehoert deshalb zur Regel (Regel.Leer), so wie der Hinweis
// auch, und wird hier nur noch gezeichnet.
//
// Die zweite Haelfte des Tests ist die wichtigere, und sie verbietet weniger,
// als man beim ersten Lesen denkt: der Browser darf keinen Satz vorraetig
// halten, der eine Aussage ueber eine BESTIMMTE Liste macht. "Nichts offen"
// war so einer — im Logbuch die Umkehrung der Wahrheit. Ein
// ansichtsUNabhaengiger Ausweichtext ("Die Liste ist leer, nicht kaputt.")
// faellt nicht darunter: er behauptet nichts, was in einer Ansicht falsch sein
// koennte, und steht deshalb ausdruecklich erlaubt in avZeichne.
//
// Die Formulierung stand hier frueher als "es darf kein Ersatzsatz im Browser
// stehen". Das deckte sich nicht mit dem, was der Test tut — er sucht ein
// einzelnes Literal —, und eine Begruendung, die mehr verspricht als ihr Code
// haelt, ist schlimmer als keine: der naechste Leser haelt den vorhandenen
// Ausweichtext fuer einen uebersehenen Defekt und raeumt ihn weg.
//
// Warum der Ausweichtext ueberhaupt dasteht, obwohl Leer nach Messung nie
// fehlen kann, steht an ihm selbst in browser.js (avZeichne).
//
// Gesucht wird im Browser OHNE seine Kommentare: die Erklaerung, warum der Satz
// nicht mehr hier steht, nennt ihn zwangslaeufig — und ein Test, der die
// Begruendung seines eigenen Gegenstands verbietet, zwingt dazu, schlechter zu
// kommentieren (dieselbe Ueberlegung wie bei ohneKommentare).
func TestLeersatzKommtAusGo(t *testing.T) {
	if block := ansichtenBlock(t); !strings.Contains(block, "a.leer") {
		t.Error("der Ansichten-Block liest a.leer nicht — der Satz einer leeren Liste " +
			"kaeme dann wieder aus dem Browser und hiesse in jeder Ansicht dasselbe")
	}
	if strings.Contains(ohneJsKommentare(BrowserJS), "Nichts offen") {
		t.Error(`der generische Leersatz "Nichts offen" steht noch im Browser — ` +
			"im Logbuch ist das die falsche Aussage (appctx/logbuch.go, Regel.Leer)")
	}
}

// TestAvBeadSuchenFindetJedeTiefe fuehrt den Helfer wirklich aus. Die flache
// Zeilenliste nennt je Eintrag nur eine Bead-Id; findet dieser Helfer eine
// Aufgabe nicht, faellt die Zeile in den Zweig "nicht im geladenen Baum" — sie
// verschwindet nicht, aber sie zeigt auch nichts. Ein Abstieg, der bei Ebene 1
// aufhoert, waere in der Ansicht deshalb NICHT als Fehler zu sehen, sondern
// nur als Liste voller Platzhalter.
func TestAvBeadSuchenFindetJedeTiefe(t *testing.T) {
	nodePath, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node nicht installiert")
	}
	const start = "  function avBeadSuchen(id) {"
	const ende = "  function avKarteSuchen(id) {"
	i, j := strings.Index(BrowserJS, start), strings.Index(BrowserJS, ende)
	if i < 0 || j < 0 || j <= i {
		t.Fatal("Marker um avBeadSuchen nicht gefunden")
	}
	script := `
var REPORTS = { maps: [
  { id: "m1", beads: [ { id: "a", title: "A", children: [
      { id: "a1", title: "A1", children: [ { id: "a1x", title: "A1X" } ] } ] } ] },
  { id: "m2", beads: [ { id: "b", title: "B" } ] },
] };
` + BrowserJS[i:j] + `
function pruefe(id, titel) {
  var b = avBeadSuchen(id);
  if (!b || b.title !== titel) throw new Error(id + " -> " + JSON.stringify(b) + ", erwartet " + titel);
}
pruefe("a", "A");
pruefe("a1", "A1");
pruefe("a1x", "A1X");   // Enkel in der zweiten Karte-Ebene
pruefe("b", "B");       // zweite Karte
if (avBeadSuchen("gibt-es-nicht") !== null) throw new Error("unbekannte Id liefert nicht null");
`
	pfad := filepath.Join(t.TempDir(), "av_check.js")
	if err := os.WriteFile(pfad, []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(nodePath, pfad).CombinedOutput(); err != nil {
		t.Fatalf("node-Pruefung fehlgeschlagen: %v\n%s", err, out)
	}
}
