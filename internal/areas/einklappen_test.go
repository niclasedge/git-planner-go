package areas

// Einklappzustand der Kategorien in areas.json (repo-leiste, #25).

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestEinklappenUndAufklappen(t *testing.T) {
	pfad := filepath.Join(t.TempDir(), "areas.json")
	if err := os.WriteFile(pfad, []byte(`{"version":1,"areas":["Arbeit"],"zuordnung":{},"notiz":{"x":1}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	s := Open(pfad)

	if err := s.Einklappen("Arbeit", true); err != nil {
		t.Fatalf("Einklappen: %v", err)
	}
	if err := s.Einklappen(OhneBereich, true); err != nil {
		t.Fatalf("Einklappen Ohne Bereich: %v", err)
	}
	// Doppelt einklappen legt keinen zweiten Eintrag an.
	if err := s.Einklappen("Arbeit", true); err != nil {
		t.Fatal(err)
	}
	st, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !st.IstEingeklappt("Arbeit") || !st.IstEingeklappt(OhneBereich) || st.IstEingeklappt("Privat") {
		t.Errorf("eingeklappt %v", st.Eingeklappt)
	}
	if len(st.Eingeklappt) != 2 {
		t.Errorf("eingeklappt %v, erwartet genau zwei Eintraege", st.Eingeklappt)
	}

	if err := s.Einklappen("Arbeit", false); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(pfad)
	var roh map[string]json.RawMessage
	if err := json.Unmarshal(b, &roh); err != nil {
		t.Fatal(err)
	}
	var ein []string
	_ = json.Unmarshal(roh["eingeklappt"], &ein)
	if len(ein) != 1 || ein[0] != OhneBereich {
		t.Errorf("eingeklappt in der Datei %v, erwartet nur %q", ein, OhneBereich)
	}
	var notiz bytes.Buffer
	_ = json.Compact(&notiz, roh["notiz"])
	if notiz.String() != `{"x":1}` {
		t.Errorf("unbekanntes Feld notiz veraendert: %s", roh["notiz"])
	}
}

func TestEinklappenOhneNamen(t *testing.T) {
	pfad := filepath.Join(t.TempDir(), "areas.json")
	s := Open(pfad)
	if err := s.Einklappen("  ", true); !errors.Is(err, ErrEingabe) {
		t.Fatalf("Fehler %v, erwartet ErrEingabe", err)
	}
	if _, err := os.Stat(pfad); !os.IsNotExist(err) {
		t.Error("abgelehnte Eingabe hat die Datei geschrieben")
	}
}

func TestUmbenennenNimmtEinklappzustandMit(t *testing.T) {
	s := Open(filepath.Join(t.TempDir(), "areas.json"))
	if err := s.Anlegen("Arbeit"); err != nil {
		t.Fatal(err)
	}
	if err := s.Einklappen("Arbeit", true); err != nil {
		t.Fatal(err)
	}
	if err := s.Umbenennen("Arbeit", "Job"); err != nil {
		t.Fatal(err)
	}
	st, _ := s.Load()
	if !st.IstEingeklappt("Job") || st.IstEingeklappt("Arbeit") {
		t.Errorf("nach dem Umbenennen eingeklappt %v, erwartet [Job]", st.Eingeklappt)
	}
}
