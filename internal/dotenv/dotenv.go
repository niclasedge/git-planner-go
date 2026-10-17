// Package dotenv laedt eine .env in die Prozess-Umgebung, ohne sie zu
// ueberschreiben.
//
// Wofuer: die GitHub-Karte nimmt ihre Tokens aus GITHUB_PAT_PERSONAL und
// GITHUB_PAT_WORK (GitHub-Issue #2). Auf der Entwicklungsmaschine stehen beide
// in der .env im --cwd des Dienstes; in einer Remote-Umgebung kommt
// GITHUB_PAT_WORK von aussen in die Umgebung. Deshalb gilt: was die Umgebung
// schon (nicht leer) traegt, bleibt stehen — die Datei ergaenzt nur.
//
// Absichtlich klein: KEY=VALUE je Zeile, optional mit `export `, Werte optional
// in "…" oder '…'. Keine Variablen-Expansion, keine mehrzeiligen Werte.
// Werte werden nie zurueckgegeben oder geloggt, nur die Namen.
package dotenv

import (
	"bufio"
	"errors"
	"io/fs"
	"os"
	"strings"
)

// Laden liest die Datei unter pfad und setzt jede Variable, die in der
// Umgebung fehlt oder leer ist. Zurueck kommen die Namen der gesetzten
// Variablen in Dateireihenfolge. Eine fehlende Datei ist kein Fehler.
func Laden(pfad string) ([]string, error) {
	f, err := os.Open(pfad)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var gesetzt []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		zeile := strings.TrimSpace(sc.Text())
		if zeile == "" || strings.HasPrefix(zeile, "#") {
			continue
		}
		zeile = strings.TrimPrefix(zeile, "export ")
		name, wert, ok := strings.Cut(zeile, "=")
		name = strings.TrimSpace(name)
		if !ok || name == "" {
			continue
		}
		wert = entquote(strings.TrimSpace(wert))
		if wert == "" || os.Getenv(name) != "" {
			continue
		}
		if err := os.Setenv(name, wert); err != nil {
			return gesetzt, err
		}
		gesetzt = append(gesetzt, name)
	}
	return gesetzt, sc.Err()
}

func entquote(s string) string {
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') && s[len(s)-1] == s[0] {
		return s[1 : len(s)-1]
	}
	return s
}
