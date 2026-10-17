package dotenv

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func schreibe(t *testing.T, inhalt string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(p, []byte(inhalt), 0o600); err != nil {
		t.Fatalf(".env schreiben: %v", err)
	}
	return p
}

// Die Umgebung geht vor: GITHUB_PAT_WORK kommt in einer Remote-Umgebung von
// aussen, eine .env im Clone darf es nicht still ueberschreiben.
func TestLadenUmgebungGehtVor(t *testing.T) {
	t.Setenv("GITHUB_PAT_WORK", "aus-der-umgebung")
	gesetzt, err := Laden(schreibe(t, "GITHUB_PAT_WORK=aus-der-datei\n"))
	if err != nil {
		t.Fatalf("Laden: %v", err)
	}
	if got := os.Getenv("GITHUB_PAT_WORK"); got != "aus-der-umgebung" {
		t.Errorf("GITHUB_PAT_WORK=%q, erwartet den Wert aus der Umgebung", got)
	}
	if slices.Contains(gesetzt, "GITHUB_PAT_WORK") {
		t.Errorf("gesetzt=%v nennt eine Variable, die nicht gesetzt wurde", gesetzt)
	}
}

func TestLadenErgaenztFehlendeVariable(t *testing.T) {
	t.Setenv("GITHUB_PAT_PERSONAL", "")
	gesetzt, err := Laden(schreibe(t, "# Kommentar\n\nexport GITHUB_PAT_PERSONAL=\"abc\"\nANDERES='x y'\n"))
	if err != nil {
		t.Fatalf("Laden: %v", err)
	}
	t.Cleanup(func() { os.Unsetenv("ANDERES") })
	if got := os.Getenv("GITHUB_PAT_PERSONAL"); got != "abc" {
		t.Errorf("GITHUB_PAT_PERSONAL=%q, erwartet abc (Anfuehrungszeichen entfernt)", got)
	}
	if got := os.Getenv("ANDERES"); got != "x y" {
		t.Errorf("ANDERES=%q, erwartet 'x y'", got)
	}
	if !slices.Equal(gesetzt, []string{"GITHUB_PAT_PERSONAL", "ANDERES"}) {
		t.Errorf("gesetzt=%v, erwartet [GITHUB_PAT_PERSONAL ANDERES]", gesetzt)
	}
}

func TestLadenLeererWertUndFehlendeDatei(t *testing.T) {
	t.Setenv("GITHUB_PAT_WORK", "")
	os.Unsetenv("GITHUB_PAT_WORK")
	gesetzt, err := Laden(schreibe(t, "GITHUB_PAT_WORK=\n"))
	if err != nil || len(gesetzt) != 0 {
		t.Errorf("leerer Wert: gesetzt=%v err=%v, erwartet nichts", gesetzt, err)
	}
	if _, da := os.LookupEnv("GITHUB_PAT_WORK"); da {
		t.Error("ein leerer Wert in der .env darf die Variable nicht anlegen")
	}

	gesetzt, err = Laden(filepath.Join(t.TempDir(), "gibt-es-nicht"))
	if err != nil || len(gesetzt) != 0 {
		t.Errorf("fehlende Datei: gesetzt=%v err=%v, erwartet nichts und keinen Fehler", gesetzt, err)
	}
}
