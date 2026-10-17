package appctx

// Geaenderte Bilder (beleg-bilder, #105): die Bilddateien der juengsten
// Commits, je Pfad einmal mit der neuesten Aenderung, ohne Geloeschte und
// ohne das, was schon als Beleg in der Galerie steht. Ohne Netz.

import (
	"strings"
	"testing"

	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

func gbBildURL(sha, name string) string { return "/api/blob?name=" + name + "&sha=" + sha }

func gbCommit(sha, zeit, betreff string, dateien ...ghrest.CommitDatei) ghrest.CommitMitDateien {
	return ghrest.CommitMitDateien{CommitKopf: ghrest.CommitKopf{SHA: sha, Zeit: zeit, Betreff: betreff}, Dateien: dateien}
}

func gbPfade(g GeaenderteBilder) string {
	var p []string
	for _, b := range g.Bilder {
		p = append(p, b.Pfad+"@"+b.Commit)
	}
	return strings.Join(p, ",")
}

func TestGeaenderteBilderNeuesteJePfad(t *testing.T) {
	commits := []ghrest.CommitMitDateien{
		gbCommit("c3", "2026-09-27T10:00:00Z", "a nachgezogen",
			ghrest.CommitDatei{Pfad: "docs/a.png", SHA: "b-a3", Status: "modified"},
			ghrest.CommitDatei{Pfad: "README.md", SHA: "b-r", Status: "modified"}),
		gbCommit("c2", "2026-09-26T10:00:00Z", "aufraeumen",
			ghrest.CommitDatei{Pfad: "docs/weg.png", SHA: "b-w0", Status: "removed"},
			ghrest.CommitDatei{Pfad: "docs/neu.svg", SHA: "b-n", Status: "renamed", AlterPfad: "docs/alt.svg"}),
		gbCommit("c1", "2026-09-25T10:00:00Z", "anlegen",
			ghrest.CommitDatei{Pfad: "docs/a.png", SHA: "b-a1", Status: "added"},
			ghrest.CommitDatei{Pfad: "docs/weg.png", SHA: "b-w", Status: "added"},
			ghrest.CommitDatei{Pfad: "docs/alt.svg", SHA: "b-n", Status: "added"},
			ghrest.CommitDatei{Pfad: "docs/B.JPG", SHA: "b-b", Status: "added"},
			ghrest.CommitDatei{Pfad: "docs/c.jpeg", SHA: "b-c", Status: "added"},
			ghrest.CommitDatei{Pfad: "docs/d.gif", SHA: "b-d", Status: "added"},
			ghrest.CommitDatei{Pfad: "docs/e.webp", SHA: "b-e", Status: "added"},
			ghrest.CommitDatei{Pfad: "docs/plan.dot", SHA: "b-dot", Status: "added"},
			ghrest.CommitDatei{Pfad: "docs/bild.png.txt", SHA: "b-t", Status: "added"}),
	}
	g := GeaenderteBilderBauen("acme/alpha", commits, nil, gbBildURL)
	if g.Repo != "acme/alpha" {
		t.Errorf("Repo %q", g.Repo)
	}
	if got := gbPfade(g); got != "docs/a.png@c3,docs/neu.svg@c2,docs/B.JPG@c1,docs/c.jpeg@c1,docs/d.gif@c1,docs/e.webp@c1" {
		t.Errorf("Folge %s", got)
	}
	a := g.Bilder[0]
	if a.Name != "a.png" || a.Typ != "png" || a.SHA != "b-a3" || a.Zeit != "2026-09-27T10:00:00Z" || a.Betreff != "a nachgezogen" {
		t.Errorf("a.png %+v", a)
	}
	if a.Bild != "/api/blob?name=a.png&sha=b-a3" {
		t.Errorf("Bild-URL %q", a.Bild)
	}
	if a.Link != "https://github.com/acme/alpha/blob/c3/docs/a.png" {
		t.Errorf("Link %q", a.Link)
	}
	if b := g.Bilder[2]; b.Typ != "jpg" || b.Name != "B.JPG" {
		t.Errorf("B.JPG %+v", b)
	}
}

func TestGeaenderteBilderOhneBelege(t *testing.T) {
	commits := []ghrest.CommitMitDateien{
		gbCommit("k1", "2026-09-27T10:00:00Z", "Belege",
			ghrest.CommitDatei{Pfad: "openspec/changes/archive/2026-09-20-alt/belege/assets/0-x.png", SHA: "b-x", Status: "added"},
			ghrest.CommitDatei{Pfad: "openspec/changes/neu/belege/artefacts/0-plan.svg", SHA: "b-irgendwas", Status: "added"},
			ghrest.CommitDatei{Pfad: "docs/belege/x.png", SHA: "b-x", Status: "added"},
			ghrest.CommitDatei{Pfad: "docs/entwurf.png", SHA: "b-e", Status: "added"},
			ghrest.CommitDatei{Pfad: "openspec/changes/neu/bild.png", SHA: "b-o", Status: "added"}),
	}
	belege := []GalerieBeleg{{Kennung: "alt", Name: "0-x.png", SHA: "b-x"}}
	g := GeaenderteBilderBauen("acme/alpha", commits, belege, gbBildURL)
	if got := gbPfade(g); got != "docs/entwurf.png@k1,openspec/changes/neu/bild.png@k1" {
		t.Errorf("Bilder %s, erwartet nur docs/entwurf.png und das Bild ausserhalb von belege/", got)
	}
}

func TestGeaenderteBilderLeer(t *testing.T) {
	g := GeaenderteBilderBauen("acme/alpha", nil, nil, gbBildURL)
	if g.Bilder == nil || len(g.Bilder) != 0 {
		t.Errorf("leere Liste erwartet, nicht nil: %+v", g.Bilder)
	}
}
