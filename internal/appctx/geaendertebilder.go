// Geaenderte Bilder eines Repos (beleg-bilder, #105).
//
// Die Bilddateien der juengsten Commits des Standard-Branches, je Pfad einmal
// mit der neuesten Aenderung, neueste zuerst. Geloeschte und weg-umbenannte
// Pfade fallen weg, ebenso alles, was schon als Beleg in der Galerie steht:
// jeder Pfad unter openspec/changes/**/belege/ und jede Datei mit dem
// Blob-SHA eines Belegs (etwa die Kopie docs/belege/x.png). Die Oberflaeche
// zeichnet die Liste in dieser Folge und rechnet nichts nach.
package appctx

import (
	"path"
	"sort"
	"strings"

	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

// GeaendertesBild ist eine Kachel unter "Geaenderte Bilder".
type GeaendertesBild struct {
	Pfad string `json:"pfad"`
	Name string `json:"name"`
	Typ  string `json:"typ"`
	// SHA ist der Blob-SHA der Datei nach dem Commit.
	SHA     string `json:"sha"`
	Zeit    string `json:"zeit"`
	Commit  string `json:"commit"`
	Betreff string `json:"betreff"`
	Bild    string `json:"bild"`
	Link    string `json:"link"`
}

// GeaenderteBilder ist die Antwort von /api/geaenderte-bilder.
type GeaenderteBilder struct {
	Repo   string            `json:"repo"`
	Ref    string            `json:"ref,omitempty"`
	Bilder []GeaendertesBild `json:"bilder"`
	Grund  string            `json:"grund,omitempty"`
}

// bildEndungen sind die Endungen, die als Bild gelten.
var bildEndungen = map[string]bool{"png": true, "jpg": true, "jpeg": true, "gif": true, "webp": true, "svg": true}

// IstBildPfad sagt, ob pfad nach seiner Endung ein Bild ist.
func IstBildPfad(pfad string) bool { return bildEndungen[BelegTyp(pfad)] }

// istBelegPfad: ein Pfad unter openspec/changes/ mit einem Glied belege.
func istBelegPfad(pfad string) bool {
	rest, ok := strings.CutPrefix(pfad, "openspec/changes/")
	if !ok {
		return false
	}
	glieder := strings.Split(rest, "/")
	for _, g := range glieder[:len(glieder)-1] {
		if g == "belege" {
			return true
		}
	}
	return false
}

// GeaenderteBilderBauen baut die Liste aus den Commits (neueste zuerst) und
// deren Dateien. bildURL liefert die Adresse, unter der die Oberflaeche einen
// Blob laden kann.
func GeaenderteBilderBauen(repo string, commits []ghrest.CommitMitDateien, belege []GalerieBeleg, bildURL func(sha, name string) string) GeaenderteBilder {
	g := GeaenderteBilder{Repo: repo, Bilder: []GeaendertesBild{}}
	belegSHA := map[string]bool{}
	for _, b := range belege {
		if b.SHA != "" {
			belegSHA[b.SHA] = true
		}
	}
	// Die Commits nach Zeit, neueste zuerst; bei gleicher Zeit bleibt die
	// Folge von GitHub.
	folge := append([]ghrest.CommitMitDateien(nil), commits...)
	sort.SliceStable(folge, func(i, j int) bool { return folge[i].Zeit > folge[j].Zeit })
	gesehen := map[string]bool{}
	for _, c := range folge {
		var weg []string
		for _, d := range c.Dateien {
			if d.AlterPfad != "" {
				weg = append(weg, d.AlterPfad)
			}
			if gesehen[d.Pfad] {
				continue
			}
			gesehen[d.Pfad] = true
			if d.Status == "removed" || !IstBildPfad(d.Pfad) || istBelegPfad(d.Pfad) || (d.SHA != "" && belegSHA[d.SHA]) {
				continue
			}
			name := path.Base(d.Pfad)
			e := GeaendertesBild{Pfad: d.Pfad, Name: name, Typ: BelegTyp(d.Pfad), SHA: d.SHA,
				Zeit: c.Zeit, Commit: c.SHA, Betreff: c.Betreff}
			if bildURL != nil && d.SHA != "" {
				e.Bild = bildURL(d.SHA, name)
			}
			if repo != "" && c.SHA != "" {
				e.Link = "https://github.com/" + repo + "/blob/" + c.SHA + "/" + d.Pfad
			}
			g.Bilder = append(g.Bilder, e)
		}
		// Umbenannt: der alte Name ist ab diesem Commit weg; aeltere
		// Aenderungen daran zaehlen nicht mehr.
		for _, p := range weg {
			gesehen[p] = true
		}
	}
	return g
}
