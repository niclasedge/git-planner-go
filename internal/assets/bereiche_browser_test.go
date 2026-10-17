package assets

import (
	"strings"
	"testing"
)

// Die Browser-Haelfte von gkp-8ww, am AUSGELIEFERTEN Code und nicht an einer
// danebengelegten Kopie — eine Kopie driftet, und ein Test an einer Kopie
// bleibt nach der Regression gruen (dieselbe Bauart wie in
// ansichten_browser_test.go).

const bzStart = "/* ---------- Bereiche (gkp-8ww, Spec §4.3 / E2, §5) ----------"
const bzEnde = "/* Die Anmeldung. Genau hier, am Ende, kommt eine weitere Ansicht dazu. */"

func bereicheBlockJS(t *testing.T) string {
	t.Helper()
	start := strings.Index(BrowserJS, bzStart)
	end := strings.Index(BrowserJS, bzEnde)
	if start < 0 || end < 0 || end <= start {
		t.Fatalf("Marker um den Bereiche-Block nicht gefunden — beim Umbauen die Marker hier mitziehen")
	}
	return BrowserJS[start:end]
}

// TestBereicheBlockSortiertNicht ist die Regel aus CLAUDE.md fuer diese
// Ansicht — und sie greift hier SCHAERFER als bei den Aufgabenlisten.
//
// Dort ist die Reihenfolge gerechnet (Rang, Termin, Prioritaet), hier ist sie
// AUTORISIERT: die Folge der Areas ist die der Datei, und areas.json wird von
// Hand gepflegt (Spec §4.3). Ein `.sort()` im Browser wuerfe damit nicht eine
// Rechnung weg, sondern die Reihenfolge, die jemand aufgeschrieben hat — und
// niemand koennte es sehen, weil sie erst im Browser entstuende (Spec §5.0).
func TestBereicheBlockSortiertNicht(t *testing.T) {
	block := ohneKommentare(bereicheBlockJS(t))
	verboten := map[string]string{
		".sort(":        "die Reihenfolge der Areas ist die der Datei, gebaut in appctx.BereicheGruppieren",
		"Object.keys(":  "genau dieser Weg hat die Kopfbilanz schon einmal falsch geordnet (Spec §5.0)",
		".filter(":      "welche Gruppe es gibt, entscheidet Go (BereicheGruppieren), nicht der Browser",
		"localeCompare": "ein Vergleich hier waere eine zweite Sortierregel neben der in Go",
	}
	for muster, warum := range verboten {
		if strings.Contains(block, muster) {
			t.Errorf("der Bereiche-Block enthaelt %q — %s", muster, warum)
		}
	}
	// Die Gegenprobe zum Verbot: der Block muss die Gruppen ueberhaupt aus dem
	// gelieferten Rumpf nehmen. Ohne diese Zusage waeren die Verbote oben auch
	// dann erfuellt, wenn er gar nichts zeichnet.
	for _, noetig := range []string{"gruppen.forEach", "DATA.bereiche"} {
		if !strings.Contains(block, noetig) {
			t.Errorf("der Bereiche-Block enthaelt %q nicht — zeichnet er die Gruppierung aus Go?", noetig)
		}
	}
}

// TestBereicheBlockKenntAlleVierOperationen ist die Abnahme des Beads, am
// Browser nachgesehen: anlegen, umbenennen, zuordnen, Zuordnung loesen —
// jeweils AUS DER ANSICHT heraus. Fehlt eine Route, ist die Operation nur in
// Go vorhanden und fuer den Benutzer nicht erreichbar.
func TestBereicheBlockKenntAlleVierOperationen(t *testing.T) {
	block := bereicheBlockJS(t)
	for _, route := range []string{
		"/action/task/bereich/anlegen",
		"/action/task/bereich/umbenennen",
		"/action/task/bereich",
	} {
		if !strings.Contains(block, route) {
			t.Errorf("der Bereiche-Block ruft %q nicht auf", route)
		}
	}
	// Das Loesen hat keinen eigenen Knopf: es ist der leere Wert der
	// Auswahlliste. Steht der nicht da, laesst sich eine Zuordnung nur noch
	// setzen und nie mehr entfernen — schlimmer als keine Zuordnung.
	if !strings.Contains(block, `<option value=""`) {
		t.Error("die Auswahlliste hat keinen leeren Wert — das Loesen der Zuordnung waere nicht erreichbar")
	}
}

// bzTernaerOhne ist die Stelle der Fallunterscheidung im Gruppenkopf — die
// EINE, an der der Aktionsbereich einer Gruppe entsteht.
//
// Nicht `strings.Index(block, "(g.ohne")`: das traf die erste Erwaehnung, und
// die ist das `if (g.ohne)` der Kopfbilanz weiter oben. Der gepruefte Abstand
// enthielt damit den halben Block — samt eines Fragezeichens aus einem ganz
// anderen Bedingungsoperator, womit die Haelfte der Behauptung immer erfuellt
// war (Spec §9.1, Ursache 2: "die Behauptung ist schwaecher als gemeint").
// Gefunden bei der Gegenprobe zum Loeschen-Knopf (gkp-td3), repariert fuer
// BEIDE Tests — die Lehre aus §9.1, dass ein Einzelfix sich nicht von selbst
// ueberträgt.
func bzTernaerOhne(t *testing.T, block string) int {
	t.Helper()
	i := strings.Index(block, "(g.ohne\n")
	if i < 0 {
		t.Fatal("die Fallunterscheidung des Gruppenkopfs fehlt — `(g.ohne` am Zeilenende nicht gefunden")
	}
	return i
}

// Die Sammel-Area darf keinen Umbenennen-Knopf tragen: sie steht in keiner
// Datei, sondern entsteht beim Lesen (areas.OhneBereich). Ein Knopf waere ein
// Angebot, das die Route nur ablehnen koennte.
// Absichtlich OHNE ohneKommentare: das schneidet Zeichenketten heraus, und
// genau in einer Zeichenkette steht der Knopf. Hier wird die Struktur um ihn
// herum geprueft, nicht der Code daneben.
func TestBereicheBlockBietetDieSammelAreaNichtZumUmbenennenAn(t *testing.T) {
	block := bereicheBlockJS(t)
	i := bzTernaerOhne(t, block)
	j := strings.Index(block, `data-bz-akt="umbenennen"`)
	if j < 0 {
		t.Fatal("kein Umbenennen-Knopf im Bereiche-Block")
	}
	if j < i {
		t.Fatal("der Umbenennen-Knopf steht VOR der Fallunterscheidung — er kann nicht an ihr haengen")
	}
	// Zwischen der Bedingung und dem Knopf muss der JA-Zweig liegen: das
	// Fragezeichen des Bedingungsoperators und der Ersatztext fuer die
	// Sammel-Area. Steht der Knopf unbedingt da, fehlt beides.
	zwischen := block[i:j]
	if !strings.Contains(zwischen, "?") || !strings.Contains(zwischen, "nicht umbenennbar") {
		t.Errorf("der Umbenennen-Knopf haengt nicht am JA-Zweig von g.ohne — er stuende auch an der Sammel-Area.\nDazwischen: %q", zwischen)
	}
}

// ---------------------------------------------------------------- gkp-td3

// Die fuenfte Operation, am Browser nachgesehen: loeschen. Fehlt der Aufruf,
// ist sie nur in Go vorhanden — und genau darum geht dieser Bead, dass eine
// versehentlich angelegte Area AUS DER ANSICHT wieder verschwindet.
func TestBereicheBlockKenntDasLoeschen(t *testing.T) {
	block := bereicheBlockJS(t)
	if !strings.Contains(block, "/action/task/bereich/loeschen") {
		t.Error("der Bereiche-Block ruft /action/task/bereich/loeschen nicht auf — die Area liesse sich nur in Go entfernen")
	}
}

// Die Sammel-Area darf keinen Loeschen-Knopf tragen — derselbe Grund wie beim
// Umbenennen: sie steht in keiner Datei, sondern entsteht beim Lesen
// (areas.OhneBereich). Ein Knopf waere ein Angebot, das die Route nur ablehnen
// koennte, und der einzige denkbare Sinn waere, die unzugeordneten Projekte zu
// verstecken — das Gegenteil von Spec §4.3.
//
// Absichtlich OHNE ohneKommentare, wie beim Geschwistertest: der Knopf steht in
// einer Zeichenkette, und die wuerde herausgeschnitten.
func TestBereicheBlockBietetDieSammelAreaNichtZumLoeschenAn(t *testing.T) {
	block := bereicheBlockJS(t)
	i := bzTernaerOhne(t, block)
	j := strings.Index(block, `data-bz-akt="loeschen"`)
	if j < 0 {
		t.Fatal("kein Loeschen-Knopf im Bereiche-Block")
	}
	if j < i {
		t.Fatal("der Loeschen-Knopf steht VOR der Fallunterscheidung — er kann nicht an ihr haengen")
	}
	zwischen := block[i:j]
	if !strings.Contains(zwischen, "?") || !strings.Contains(zwischen, "nicht löschbar") {
		t.Errorf("der Loeschen-Knopf haengt nicht am JA-Zweig von g.ohne — er stuende auch an der Sammel-Area.\nDazwischen: %q", zwischen)
	}
}
