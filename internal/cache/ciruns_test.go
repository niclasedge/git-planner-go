package cache_test

// Der Plattenspeicher der CI-Runs (ci-runs, #122): Stand je Repo, Jobs je
// Run und Versuch, Logs je Job.

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/niclasedge/git-planner-go/internal/cache"
	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

func TestCIOrdnerAusUmgebung(t *testing.T) {
	ordner := t.TempDir()
	t.Setenv("CC_KNOWLEDGE_CI_CACHE", ordner)
	if cache.CIOrdner() != ordner {
		t.Errorf("CIOrdner %q", cache.CIOrdner())
	}
	t.Setenv("CC_KNOWLEDGE_CI_CACHE", "")
	t.Setenv("CC_KNOWLEDGE_CACHE", "")
	t.Setenv("HOME", "/home/test")
	if cache.CIOrdner() != "/home/test/.cache/cc-knowledge/ci" {
		t.Errorf("CIOrdner ohne Variable %q", cache.CIOrdner())
	}
	// Ein umgelenkter Cache (jeder Test, der den Dienst startet) lenkt den
	// CI-Speicher mit um.
	t.Setenv("CC_KNOWLEDGE_CACHE", "/tmp/gkp-test/cache.json")
	if cache.CIOrdner() != "/tmp/gkp-test/ci" {
		t.Errorf("CIOrdner neben CC_KNOWLEDGE_CACHE %q", cache.CIOrdner())
	}
}

func TestCIStandAufDerPlatte(t *testing.T) {
	ordner := t.TempDir()
	if st := cache.CIStandLesen(ordner, "acme/alpha"); st != nil {
		t.Fatalf("leerer Ordner liefert %+v", st)
	}
	zeit := time.Date(2026, 9, 30, 9, 0, 0, 0, time.UTC)
	st := &cache.CIStand{Zeit: zeit,
		Runs: []ghrest.WorkflowRun{{ID: 12, Nummer: 12, Versuch: 2, Name: "CI", Status: "completed", Ergebnis: "failure"}},
		Jobs: map[string]cache.CIJobs{cache.CIJobsSchluessel(12, 2): {Abgeschlossen: true,
			Jobs: []ghrest.ActionsJob{{ID: 501, Name: "test", Schritte: []ghrest.ActionsSchritt{{Nummer: 5, Name: "Run go test ./..."}}}}}},
		Grund: ""}
	if cache.CIJobsSchluessel(12, 2) != "12@2" {
		t.Errorf("Schluessel %q", cache.CIJobsSchluessel(12, 2))
	}
	if err := cache.CIStandSchreiben(ordner, "ACME/Alpha", st); err != nil {
		t.Fatal(err)
	}
	neu := cache.CIStandLesen(ordner, "acme/alpha")
	if neu == nil || !neu.Zeit.Equal(zeit) || len(neu.Runs) != 1 || neu.Runs[0].Versuch != 2 {
		t.Fatalf("gelesen %+v", neu)
	}
	j, ok := neu.Jobs["12@2"]
	if !ok || !j.Abgeschlossen || len(j.Jobs) != 1 || j.Jobs[0].Schritte[0].Name != "Run go test ./..." {
		t.Errorf("Jobs %+v", neu.Jobs)
	}
	if err := os.WriteFile(filepath.Join(ordner, "acme__kaputt.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if st := cache.CIStandLesen(ordner, "acme/kaputt"); st != nil {
		t.Errorf("unlesbare Datei liefert %+v", st)
	}
}

func TestCILogAufDerPlatte(t *testing.T) {
	ordner := t.TempDir()
	if _, _, ok := cache.CILogLesen(ordner, "acme/alpha", 501); ok {
		t.Fatal("Log ohne Datei gefunden")
	}
	if err := cache.CILogSchreiben(ordner, "acme/alpha", 501, "a\nb\n", true); err != nil {
		t.Fatal(err)
	}
	text, gekuerzt, ok := cache.CILogLesen(ordner, "acme/alpha", 501)
	if !ok || text != "a\nb\n" || !gekuerzt {
		t.Errorf("Log %q %v %v", text, gekuerzt, ok)
	}
	if _, _, ok := cache.CILogLesen(ordner, "acme/beta", 501); ok {
		t.Error("Log eines anderen Repos gefunden")
	}
}
