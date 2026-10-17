package collectors

import "testing"

// Die Herkunftszeile eines Changes (karte-herkunft-streng, #103): nur die
// Zeile, die am Zeilenanfang mit "Geht zurueck auf GitHub-Issue #N" beginnt,
// ordnet zu — nicht ein Verweis irgendwo im Text.

// Szenario: Verweis im Fliesstext ordnet nicht zu.
func TestOpenspecHerkunftNurAmZeilenanfang(t *testing.T) {
	dir := t.TempDir()
	osChange(t, dir, "lose", "# lose\n\nWie in GitHub-Issue #2 beschrieben.\n\n  Geht zurueck auf GitHub-Issue #3.\n", "- [ ] a\n")
	if c := osFinde(t, OpenspecChanges(dir), "lose"); c.Issue != 0 {
		t.Errorf("Issue = %d, want 0 (nur Fliesstext und eingerueckte Zeile)", c.Issue)
	}
}

// Szenario: Herkunftszeile nach Fliesstext.
func TestOpenspecHerkunftNachFliesstext(t *testing.T) {
	dir := t.TempDir()
	osChange(t, dir, "fest", "# fest\n\nSiehe GitHub-Issue #2.\n\nGeht zurueck auf GitHub-Issue #7.\n", "- [ ] a\n")
	if c := osFinde(t, OpenspecChanges(dir), "fest"); c.Issue != 7 {
		t.Errorf("Issue = %d, want 7 aus der Herkunftszeile", c.Issue)
	}
}

// Die Wortgrenze: "#7x" ist keine Herkunft.
func TestOpenspecHerkunftWortgrenze(t *testing.T) {
	dir := t.TempDir()
	osChange(t, dir, "krumm", "Geht zurueck auf GitHub-Issue #7x\n", "- [ ] a\n")
	if c := osFinde(t, OpenspecChanges(dir), "krumm"); c.Issue != 0 {
		t.Errorf("Issue = %d, want 0 ohne Wortgrenze", c.Issue)
	}
}
