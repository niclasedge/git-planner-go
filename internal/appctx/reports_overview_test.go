package appctx

import (
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/niclasedge/git-planner-go/internal/collectors"
)

func ovKarte(id string, total, closed, gaps int) ReportsMap {
	return ReportsMap{
		ID: id, Title: id + " Titel", Status: "open",
		Counts: MapCounts{Total: total, Closed: closed, Open: total - closed, Gaps: gaps, Proven: closed - gaps},
	}
}

// Die Auswahlregel ist der Kern dieser Ansicht: geloeste Karten bleiben
// draussen, offene kommen vor, und die Zahl der weggelassenen wird gemeldet
// statt verschwiegen.
func TestBuildMapsOverviewDot_NurKartenMitOffenerArbeit(t *testing.T) {
	maps := []ReportsMap{
		ovKarte("a", 4, 1, 0), // 3 offen -> gezeigt
		ovKarte("b", 5, 5, 0), // geloest -> versteckt
		ovKarte("c", 2, 2, 1), // geloest, aber mit Belegluecke -> versteckt
		{ID: "d", Unknown: true, Counts: MapCounts{Total: 3, Proven: 1}}, // kein bd-Epic -> gezeigt
	}
	src, shown, hidden, _ := buildMapsOverviewDot(maps, collectors.TrackerGraph{})
	if shown != 2 || hidden != 2 {
		t.Fatalf("shown/hidden = %d/%d, erwartet 2/2", shown, hidden)
	}
	if !strings.Contains(src, "#epic:a") || !strings.Contains(src, "#epic:d") {
		t.Fatalf("a und d muessen als Knoten vorkommen:\n%s", src)
	}
	if strings.Contains(src, "#epic:b") || strings.Contains(src, "#epic:c") {
		t.Fatalf("geloeste Karten duerfen nicht gezeichnet werden:\n%s", src)
	}
}

// Ohne eine einzige offene Karte gibt es kein Bild — und keinen DOT-Text, der
// dot beschaeftigt, um einen leeren Rahmen zu malen.
func TestBuildMapsOverviewDot_LeerWennAllesGeschlossen(t *testing.T) {
	src, shown, hidden, _ := buildMapsOverviewDot([]ReportsMap{ovKarte("a", 3, 3, 0)}, collectors.TrackerGraph{})
	if src != "" || shown != 0 || hidden != 1 {
		t.Fatalf("erwartet leerer DOT, 0 gezeigt, 1 versteckt — bekommen %q, %d, %d", src, shown, hidden)
	}
}

// Das Gitter haengt an beiden Kantenarten: rank=same macht die Zeile, die
// flachen Kanten ordnen sie, und eine Kante je Zeilenanfang stapelt die Zeilen.
// Fehlt eine davon, legt dot alle Knoten nebeneinander — das Bild ist dann
// dreimal so breit wie die Spalte.
func TestBuildMapsOverviewDot_GitterInZeilen(t *testing.T) {
	maps := make([]ReportsMap, 0, 7)
	for i := 0; i < 7; i++ {
		maps = append(maps, ovKarte("m"+strconv.Itoa(i), 2, 0, 0))
	}
	src, shown, _, _ := buildMapsOverviewDot(maps, collectors.TrackerGraph{})
	if shown != 7 {
		t.Fatalf("shown = %d, erwartet 7", shown)
	}
	if n := strings.Count(src, "rank=same"); n != 3 {
		t.Fatalf("erwartet 3 Zeilen bei 7 Karten und %d Spalten, gefunden %d:\n%s", ovSpalten, n, src)
	}
	for _, want := range []string{"n0 -> n1 -> n2;", "n3 -> n4 -> n5;", "n0 -> n3;", "n3 -> n6;"} {
		if !strings.Contains(src, want) {
			t.Fatalf("Kante %q fehlt:\n%s", want, src)
		}
	}
	if !strings.Contains(src, "edge [style=invis]") {
		t.Fatalf("die Kanten muessen unsichtbar sein — sie ordnen, sie behaupten nichts:\n%s", src)
	}
}

// Der Balken traegt drei Aussagen nebeneinander. Die Breiten muessen den
// Kasten exakt fuellen: FIXEDSIZE spannt ihn auf, und was fehlt, malt dot
// kommentarlos als Luecke.
func TestOvBalken_DreiSegmenteFuellenDieBreite(t *testing.T) {
	// 10 Beads: 6 zu, davon 2 ohne Beleg -> 4 gruen, 2 rot, 4 grau.
	got := ovBalken(MapCounts{Total: 10, Closed: 6, Open: 4, Gaps: 2})
	for _, farbe := range []string{ovGruen, ovRot, ovGrau} {
		if !strings.Contains(got, farbe) {
			t.Fatalf("Farbe %s fehlt im Balken: %s", farbe, got)
		}
	}
	if summe := ovBreitenSumme(t, got); summe != ovBalkenBreite {
		t.Fatalf("Segmentbreiten summieren zu %d, erwartet %d: %s", summe, ovBalkenBreite, got)
	}
	// Reihenfolge: erledigt und belegt zuerst, dann die Luecke, dann der Rest.
	iG, iR, iX := strings.Index(got, ovGruen), strings.Index(got, ovRot), strings.Index(got, ovGrau)
	if !(iG < iR && iR < iX) {
		t.Fatalf("Segmentreihenfolge gruen<rot<grau verletzt: %s", got)
	}
}

// Ein Segment mit 0 Beads bekommt keine Zelle: eine Zelle der Breite 0 ist
// entweder unsichtbar oder — schlimmer — ein 1px-Strich in einer Farbe, die
// etwas behauptet, das es nicht gibt.
func TestOvBalken_KeineLeerenSegmente(t *testing.T) {
	got := ovBalken(MapCounts{Total: 4, Closed: 0, Open: 4})
	if strings.Contains(got, ovGruen) || strings.Contains(got, ovRot) {
		t.Fatalf("ohne geschlossene Beads darf weder gruen noch rot vorkommen: %s", got)
	}
	if summe := ovBreitenSumme(t, got); summe != ovBalkenBreite {
		t.Fatalf("Balken fuellt %d statt %d: %s", summe, ovBalkenBreite, got)
	}
}

// Kein Balken ohne Sub-beads: ein leerer laese sich als "0 % erledigt" lesen,
// und das waere eine Behauptung ueber eine Karte ohne Inhalt.
func TestOvBalken_OhneSubbeadsGarNichts(t *testing.T) {
	if got := ovBalken(MapCounts{}); got != "" {
		t.Fatalf("erwartet leer, bekommen %q", got)
	}
}

var ovBreiteRe = regexp.MustCompile(`<TD FIXEDSIZE="TRUE" WIDTH="(\d+)"`)

func ovBreitenSumme(t *testing.T, s string) int {
	t.Helper()
	summe := 0
	for _, m := range ovBreiteRe.FindAllStringSubmatch(s, -1) {
		n, err := strconv.Atoi(m[1])
		if err != nil {
			t.Fatalf("Breite %q nicht lesbar: %v", m[1], err)
		}
		summe += n
	}
	return summe
}

// Ein Titel mit spitzen Klammern oder Kaufmanns-Und darf das HTML-Label nicht
// zerlegen — dot bricht dann nicht ab, es zeichnet gar nichts mehr.
func TestOvLabel_MaskiertTitel(t *testing.T) {
	m := ovKarte("x", 2, 1, 0)
	m.Title = `Fix: --host "" & <script>`
	got := ovLabel(m)
	if strings.Contains(got, "<script>") {
		t.Fatalf("spitze Klammern muessen maskiert sein: %s", got)
	}
	if !strings.Contains(got, "&amp;") || !strings.Contains(got, "&lt;script&gt;") {
		t.Fatalf("erwartete Maskierung fehlt: %s", got)
	}
}

// Die Sammelkarte (Beads ohne Epic) hat keine Prioritaet, und eine unbekannte
// Karte kennt keine bd-Zustaende. Beides darf die Kennzeile nicht erfinden.
func TestOvKennzeile_KeineErfundenenFelder(t *testing.T) {
	lose := ReportsMap{ID: "lose", Loose: true, Status: "open"}
	if got := ovKennzeile(lose); strings.Contains(got, "P0") {
		t.Fatalf("die Sammelkarte darf keine Prioritaet behaupten: %s", got)
	}
	unb := ReportsMap{ID: "weg", Unknown: true}
	if got := ovKennzeile(unb); !strings.Contains(got, "kein bd-Epic") {
		t.Fatalf("unbekannte Karte muss als solche benannt sein: %s", got)
	}
	if got := ovZahlen(unb); strings.Contains(got, "offen") {
		t.Fatalf("der Report-Store kennt keine bd-Zustaende, %q behauptet welche", got)
	}
}

func TestOvKuerzen_SchneidetNachRunen(t *testing.T) {
	if got := ovKuerzen("ökonomisch überprüft", 6); got != "ökono…" {
		t.Fatalf("gekuerzt zu %q", got)
	}
	if got := ovKuerzen("kurz", 10); got != "kurz" {
		t.Fatalf("kurzer Titel darf nicht angefasst werden: %q", got)
	}
}

// Der Ende-zu-Ende-Fall: dot muss den erzeugten Text tatsaechlich uebersetzen.
// Ein HTML-Label ist keine Zeichenkette, die "ungefaehr stimmt" — ein fehlendes
// </TD> laesst dot mit einem Syntaxfehler abbrechen, und der faellt sonst erst
// im Browser auf, wo die Ansicht bloss keinen Graphen zeigt.
func TestBuildMapsOverviewDot_IstFuerDotUebersetzbar(t *testing.T) {
	if _, err := exec.LookPath("dot"); err != nil {
		t.Skip("graphviz nicht installiert")
	}
	maps := []ReportsMap{
		ovKarte("a", 9, 4, 1),
		func() ReportsMap { m := ovKarte("b", 3, 0, 0); m.InProgress = true; m.ReadyCount = 2; return m }(),
		{ID: "c", Unknown: true, Counts: MapCounts{Total: 2, Proven: 1}},
		{ID: "d", Loose: true, Status: "open", Counts: MapCounts{Total: 5, Closed: 1, Open: 4}},
	}
	src, shown, _, _ := buildMapsOverviewDot(maps, collectors.TrackerGraph{})
	if shown != 4 {
		t.Fatalf("shown = %d, erwartet 4", shown)
	}
	cmd := exec.Command("dot", "-Tsvg")
	cmd.Stdin = strings.NewReader(src)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("dot lehnt den erzeugten Graphen ab: %v\n%s\n--- DOT ---\n%s", err, out, src)
	}
	if !strings.Contains(string(out), "<svg") {
		t.Fatalf("kein SVG in dots Ausgabe:\n%s", out)
	}
	// Die Knoten sind anklickbar: ohne das <a> haengt der Klickweg in die
	// Epic-Ansicht an nichts.
	if !strings.Contains(string(out), "#epic:a") {
		t.Fatalf("href fehlt im SVG:\n%s", out)
	}
}
