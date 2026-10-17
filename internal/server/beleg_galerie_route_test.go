package server_test

// GET /api/beleg-galerie gegen die Fake-GitHub-API (beleg-galerie, #54): der
// archivierte Change x auf main traegt einen Bildbeleg; die Route liefert ihn
// mit /api/blob-URL, ohne einen Blob zusaetzlich zu holen.

import (
	"net/http"
	"strings"
	"testing"
)

type testGalerie struct {
	Repo   string `json:"repo"`
	Grund  string `json:"grund"`
	Belege []struct {
		Kennung    string `json:"kennung"`
		Archiviert bool   `json:"archiviert"`
		Typ        string `json:"typ"`
		Name       string `json:"name"`
		Zeigbar    bool   `json:"zeigbar"`
		Bild       string `json:"bild"`
		Link       string `json:"link"`
	} `json:"belege"`
}

func TestApiBelegGalerie(t *testing.T) {
	f := neuesFakeGH(t)
	h, _, _ := repoHandler(t, f)

	var g testGalerie
	rec := holeJSON(t, h, "/api/beleg-galerie?repo=acme/alpha", &g)
	if rec.Code != http.StatusOK {
		t.Fatalf("Status %d: %s", rec.Code, rec.Body.String())
	}
	if g.Repo != "acme/alpha" || g.Grund != "" {
		t.Errorf("repo %q grund %q", g.Repo, g.Grund)
	}
	if len(g.Belege) != 1 {
		t.Fatalf("%d Belege, erwartet 1: %s", len(g.Belege), rec.Body.String())
	}
	b := g.Belege[0]
	if b.Kennung != "x" || !b.Archiviert || b.Typ != "png" || b.Name != "0-x.png" || !b.Zeigbar {
		t.Errorf("Beleg %+v", b)
	}
	if !strings.HasPrefix(b.Bild, "/api/blob?") || !strings.Contains(b.Bild, "sha=b-png") {
		t.Errorf("Bild-URL %q", b.Bild)
	}
	if b.Link != "https://github.com/acme/alpha/blob/main/openspec/changes/archive/2026-09-10-x/belege/assets/0-x.png" {
		t.Errorf("Link %q", b.Link)
	}

	// Aus demselben Arbeitsstand: kein zweiter Baum von main.
	vorher := f.anfragen("/repos/acme/alpha/git/trees/s-main")
	holeJSON(t, h, "/api/beleg-galerie?repo=acme/alpha", nil)
	if n := f.anfragen("/repos/acme/alpha/git/trees/s-main"); n != vorher {
		t.Errorf("Baum von main %d-mal statt %d-mal geholt", n, vorher)
	}
}

func TestApiBelegGalerieFalscheAnfrage(t *testing.T) {
	f := neuesFakeGH(t)
	h, _, _ := repoHandler(t, f)
	// Unbekanntes Repo: 404 wie /api/arbeit.
	if rec := getRaw(t, h, "/api/beleg-galerie?repo=acme/unbekannt"); rec.Code != http.StatusNotFound {
		t.Errorf("unbekanntes Repo: Status %d, erwartet 404", rec.Code)
	}
	if rec := getRaw(t, h, "/api/beleg-galerie"); rec.Code != http.StatusBadRequest {
		t.Errorf("ohne repo: Status %d, erwartet 400", rec.Code)
	}
}
