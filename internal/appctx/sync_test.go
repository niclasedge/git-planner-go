package appctx

// Kurze Fehlergruende und Inhalts-Fingerabdruck (sync-status, #38).

import (
	"testing"
	"time"

	"github.com/niclasedge/git-planner-go/internal/areas"
)

func TestKontoGrund(t *testing.T) {
	faelle := []struct{ roh, kurz string }{
		{"GitHub 403: This GitHub API path is not available: sessions are bound to their configured repositories.", "Repo-Liste nicht verfügbar (403)"},
		{"GitHub 401: Bad credentials", "Token ungültig (401)"},
		{"GitHub 404: Not Found", "Repo-Liste nicht gefunden (404)"},
		{"GITHUB_PAT_PERSONAL ist nicht gesetzt", "nicht gesetzt"},
		{`Get "https://api.github.com/user/repos": dial tcp: lookup api.github.com: no such host`, "GitHub nicht erreichbar"},
		{"", ""},
	}
	for _, f := range faelle {
		if got := KontoGrundKurz(f.roh); got != f.kurz {
			t.Errorf("KontoGrundKurz(%q) = %q, erwartet %q", f.roh, got, f.kurz)
		}
	}
}

func TestRepoLeisteKurzerGrundUndDetail(t *testing.T) {
	l := RepoLeisteBauen([]RepoKonto{{PAT: "GITHUB_PAT_WORK", Grund: "GitHub 403: This GitHub API path is not available"}}, nil, areas.State{}, time.Time{})
	if len(l.Konten) != 1 || l.Konten[0].Grund != "Repo-Liste nicht verfügbar (403)" || l.Konten[0].Detail != "GitHub 403: This GitHub API path is not available" {
		t.Errorf("Konto %+v", l.Konten)
	}
}

func TestInhaltFingerabdruck(t *testing.T) {
	a := InhaltFingerabdruck(map[string]any{"x": 1, "y": []int{1, 2}})
	b := InhaltFingerabdruck(map[string]any{"y": []int{1, 2}, "x": 1})
	c := InhaltFingerabdruck(map[string]any{"x": 2, "y": []int{1, 2}})
	if a == "" || a != b || a == c {
		t.Errorf("Fingerabdruecke %q %q %q", a, b, c)
	}
}
