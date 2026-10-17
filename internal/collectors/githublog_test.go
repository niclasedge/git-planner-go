package collectors

import (
	"bytes"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// ghLogFake startet den API-Fake je nach modus: "leer" liefert eine leere
// Issue- und PR-Liste, "fehler" laesst beide Abfragen mit 401 scheitern und
// nennt dabei das Token, das es bekommen hat — genau der Fall, in dem der
// Tokenwert sonst ins Log wandern wuerde.
func ghLogFake(t *testing.T, modus string) {
	t.Helper()
	switch modus {
	case "leer":
		ghSeiten(t, `{"data":{"repository":{"issues":{"nodes":[],"pageInfo":{"hasNextPage":false,"endCursor":""}}}}}`)
	case "fehler":
		h := func(_ int, w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprintf(w, `{"message":"Bad credentials fuer %s"}`, strings.TrimPrefix(r.Header.Get("Authorization"), "bearer "))
		}
		ghAPI(t, h, h)
	default:
		t.Fatalf("unbekannter Modus %q", modus)
	}
}

// ghLogMitschnitt lenkt die Log-Ausgabe des Pakets fuer einen Test in einen
// Puffer um.
func ghLogMitschnitt(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	alt := ghLogAusgabe
	ghLogAusgabe = &buf
	t.Cleanup(func() { ghLogAusgabe = alt })
	return &buf
}

func ghLogZeilen(buf *bytes.Buffer) []string {
	s := strings.TrimSpace(buf.String())
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func ghLogErwarte(t *testing.T, zeile string, teile ...string) {
	t.Helper()
	for _, teil := range teile {
		if !strings.Contains(zeile, teil) {
			t.Errorf("Logzeile enthaelt %q nicht:\n%s", teil, zeile)
		}
	}
}

func TestGitHubLogTokenAusDerUmgebung(t *testing.T) {
	t.Setenv("GKP_WORK_OWNERS", "example-org")
	t.Setenv("CC_GH_ACCOUNTS", "")
	t.Setenv("GITHUB_PAT_WORK", "pat-work-secret")
	ghLogFake(t, "leer")
	buf := ghLogMitschnitt(t)

	CollectGitHub(ghRepoMitOrigin(t, "https://github.com/example-org/orbit.git"))

	zeilen := ghLogZeilen(buf)
	if len(zeilen) != 1 {
		t.Fatalf("erwartet genau eine Logzeile, bekam %d:\n%s", len(zeilen), buf.String())
	}
	ghLogErwarte(t, zeilen[0], "repo=example-org/orbit", "pat=GITHUB_PAT_WORK", "quelle=umgebung", "issues=0 prs=0")
	if strings.Contains(buf.String(), "pat-work-secret") {
		t.Errorf("der Tokenwert steht im Log:\n%s", buf.String())
	}
}

func TestGitHubLogOhneToken(t *testing.T) {
	t.Setenv("GKP_WORK_OWNERS", "example-org")
	t.Setenv("CC_GH_ACCOUNTS", "")
	t.Setenv("GITHUB_PAT_WORK", "")
	ghLogFake(t, "leer")
	buf := ghLogMitschnitt(t)

	CollectGitHub(ghRepoMitOrigin(t, "https://github.com/example-org/orbit.git"))

	zeilen := ghLogZeilen(buf)
	if len(zeilen) != 1 {
		t.Fatalf("erwartet genau eine Logzeile, bekam %d:\n%s", len(zeilen), buf.String())
	}
	ghLogErwarte(t, zeilen[0], "repo=example-org/orbit", "pat=GITHUB_PAT_WORK", "quelle=-", "GITHUB_PAT_WORK ist nicht gesetzt")
}

func TestGitHubLogNenntFehlergrundOhneTokenwert(t *testing.T) {
	t.Setenv("GKP_WORK_OWNERS", "example-org")
	t.Setenv("CC_GH_ACCOUNTS", "")
	t.Setenv("GITHUB_PAT_WORK", "pat-work-secret")
	ghLogFake(t, "fehler")
	buf := ghLogMitschnitt(t)

	CollectGitHub(ghRepoMitOrigin(t, "https://github.com/example-org/orbit.git"))

	zeilen := ghLogZeilen(buf)
	if len(zeilen) != 1 {
		t.Fatalf("erwartet genau eine Logzeile, bekam %d:\n%s", len(zeilen), buf.String())
	}
	ghLogErwarte(t, zeilen[0], "repo=example-org/orbit", "pat=GITHUB_PAT_WORK", "quelle=umgebung", "fehler=", "Bad credentials")
	if strings.Contains(buf.String(), "pat-work-secret") {
		t.Errorf("der Tokenwert steht im Log:\n%s", buf.String())
	}
}

func TestGitHubLogSchweigtBeimCacheTreffer(t *testing.T) {
	t.Setenv("CC_GH_ACCOUNTS", "")
	t.Setenv("GITHUB_PAT_PERSONAL", "pat-privat-geheim")
	ghLogFake(t, "leer")
	buf := ghLogMitschnitt(t)

	repo := ghRepoMitOrigin(t, "https://github.com/example-user/infra.git")
	t.Cleanup(func() { ghCache.Delete(RepoRoot(repo)) })
	CollectGitHubCached(repo)
	CollectGitHubCached(repo)

	zeilen := ghLogZeilen(buf)
	if len(zeilen) != 1 {
		t.Fatalf("erwartet genau eine Logzeile fuer zwei Abrufe mit Cache-Treffer, bekam %d:\n%s", len(zeilen), buf.String())
	}
	ghLogErwarte(t, zeilen[0], "repo=example-user/infra", "pat=GITHUB_PAT_PERSONAL", "quelle=umgebung")
}
