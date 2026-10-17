// Die Beleg-Galerie eines Repos (beleg-galerie, #54).
//
// Alle Belege aller Changes, offen wie archiviert, in einer Liste — juengster
// zuerst, wie `sammle_belege` in ops.py. Ein Beleg ueberdauert seinen Change:
// gerade der abgeschlossene Lauf ist der, dessen Ergebnis man sehen will.
// Ein Beleg, der kein Bild ist (.dot, Text, …), bleibt drin und bekommt in der
// Oberflaeche eine Kachel mit Typ und Namen; Go liefert dafuer Typ, Name und
// Link fertig, BelegGalerie.svelte zeichnet nur.
package appctx

import (
	"path"
	"sort"
	"strings"

	"github.com/niclasedge/git-planner-go/internal/collectors"
)

// GalerieBeleg ist eine Kachel der Galerie.
type GalerieBeleg struct {
	Kennung    string `json:"kennung"`
	Archiviert bool   `json:"archiviert"`
	Stufe      string `json:"stufe"`
	Art        string `json:"art"`
	Zeit       string `json:"zeit"`
	// Pfad ist relativ zum Change-Ordner, Name sein letztes Glied.
	Pfad string `json:"pfad"`
	Name string `json:"name"`
	// Typ ist die kleingeschriebene Endung ohne Punkt, "datei" ohne Endung.
	Typ string `json:"typ"`
	// Zeigbar: es gibt eine Bild-URL und der Beleg fehlt nicht.
	Zeigbar bool   `json:"zeigbar"`
	Bild    string `json:"bild,omitempty"`
	// Link zeigt auf die Datei im GitHub-Repo, leer ohne Branch oder mit
	// absolutem (lokalem) Ordner.
	Link  string `json:"link,omitempty"`
	Fehlt bool   `json:"fehlt"`
	Grund string `json:"grund,omitempty"`
	// SHA ist der Blob-SHA der Datei, leer, wenn er nicht bekannt ist
	// (beleg-bilder, #105).
	SHA string `json:"sha,omitempty"`
}

// BelegGalerie ist die Antwort von /api/beleg-galerie.
type BelegGalerie struct {
	Repo   string         `json:"repo"`
	Belege []GalerieBeleg `json:"belege"`
	Grund  string         `json:"grund,omitempty"`
	Sync   *SyncStand     `json:"sync,omitempty"`
}

// BelegTyp nennt den Typ eines Belegs nach seiner Endung.
func BelegTyp(pfad string) string {
	ext := strings.TrimPrefix(strings.ToLower(path.Ext(pfad)), ".")
	if ext == "" {
		return "datei"
	}
	return ext
}

// BelegGalerieBauen sammelt die Belege der Changes eines Repos. Dasselbe
// Bild eines Changes (gleiche Kennung, gleicher Name ohne die laufende Nummer
// "N-", die `just beleg` voranstellt) steht einmal, mit der juengsten
// Einlieferung; bei gleicher Zeit gewinnt der erste Fund (beleg-bilder, #105).
func BelegGalerieBauen(repo string, changes []collectors.OpenspecChange) BelegGalerie {
	g := BelegGalerie{Repo: repo, Belege: []GalerieBeleg{}}
	platz := map[[2]string]int{}
	for _, c := range changes {
		for _, b := range c.Belege {
			e := GalerieBeleg{
				Kennung: c.Kennung, Archiviert: c.Archiviert,
				Stufe: b.Stufe, Art: b.Art, Zeit: b.Zeit,
				Pfad: b.Pfad, Name: path.Base(b.Pfad), Typ: BelegTyp(b.Pfad),
				Fehlt: b.Fehlt, Grund: b.Grund, SHA: b.SHA,
			}
			if !b.Fehlt && b.Bild != "" {
				e.Bild, e.Zeigbar = b.Bild, true
			}
			e.Link = belegLink(repo, c, b.Pfad)
			schluessel := [2]string{c.Kennung, BelegGrundname(b.Pfad)}
			if i, ok := platz[schluessel]; ok {
				if e.Zeit > g.Belege[i].Zeit {
					g.Belege[i] = e
				}
				continue
			}
			platz[schluessel] = len(g.Belege)
			g.Belege = append(g.Belege, e)
		}
	}
	sort.SliceStable(g.Belege, func(i, j int) bool {
		a, b := g.Belege[i], g.Belege[j]
		if a.Zeit != b.Zeit {
			return a.Zeit > b.Zeit
		}
		if a.Kennung != b.Kennung {
			return a.Kennung > b.Kennung
		}
		return a.Name > b.Name
	})
	return g
}

// BelegGrundname ist der Dateiname eines Belegs ohne die laufende Nummer
// "N-", die `just beleg` jeder Einlieferung voranstellt.
func BelegGrundname(pfad string) string {
	name := path.Base(pfad)
	i := 0
	for i < len(name) && name[i] >= '0' && name[i] <= '9' {
		i++
	}
	if i > 0 && i < len(name)-1 && name[i] == '-' {
		return name[i+1:]
	}
	return name
}

// belegLink ist die Adresse der Datei auf GitHub. Ein lokal gelesener Change
// (absoluter Ordner) oder einer ohne Branch hat keine.
func belegLink(repo string, c collectors.OpenspecChange, pfad string) string {
	if repo == "" || c.Ref == "" || c.Ordner == "" || strings.HasPrefix(c.Ordner, "/") || pfad == "" {
		return ""
	}
	return "https://github.com/" + repo + "/blob/" + c.Ref + "/" + strings.Trim(c.Ordner, "/") + "/" + strings.TrimPrefix(pfad, "/")
}
