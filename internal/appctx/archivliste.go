package appctx

// Die archivierten Changes eines Repos als eigene Liste (archiv-liste, #56):
// was ueber die Zeit fertig wurde, neueste zuerst, mit Datum, Kennzahl und
// einem Vorschaubild. Wie ops.py --web unter "Abgeschlossen" (sammle_archiv,
// vorschau_beleg); die Folge entsteht hier, nicht im Browser.

import (
	"path"
	"path/filepath"
	"regexp"
	"sort"

	"github.com/niclasedge/git-planner-go/internal/collectors"
)

// ArchivEintrag ist ein archivierter Change-Ordner.
type ArchivEintrag struct {
	Kennung string `json:"kennung"`
	// Datum ist das Datum aus dem Ordnernamen JJJJ-MM-TT-kennung, leer ohne.
	Datum    string `json:"datum"`
	Issue    int    `json:"issue,omitempty"`
	Erledigt int    `json:"erledigt"`
	Gesamt   int    `json:"gesamt"`
	// Vorschau ist der erste Beleg mit Bild-URL, der nicht fehlt.
	Vorschau *collectors.OpenspecBeleg `json:"vorschau,omitempty"`
}

var archivDatumRe = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2})-`)

// archivDatum liest das Datum aus dem letzten Pfadteil des Ordners; der Ordner
// ist relativ (GitHub) oder absolut (Dateisystem).
func archivDatum(ordner string) string {
	name := path.Base(filepath.ToSlash(ordner))
	if m := archivDatumRe.FindStringSubmatch(name); m != nil {
		return m[1]
	}
	return ""
}

// archivListe baut die Liste aus allen archivierten Changes.
func archivListe(changes []collectors.OpenspecChange) []ArchivEintrag {
	out := []ArchivEintrag{}
	for _, c := range changes {
		if !c.Archiviert {
			continue
		}
		a := ArchivEintrag{Kennung: c.Kennung, Datum: archivDatum(c.Ordner), Issue: c.Issue,
			Erledigt: c.Erledigt(), Gesamt: len(c.Aufgaben)}
		for _, b := range c.Belege {
			if b.Bild != "" && !b.Fehlt {
				kopie := b
				a.Vorschau = &kopie
				break
			}
		}
		out = append(out, a)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if (a.Datum == "") != (b.Datum == "") {
			return a.Datum != ""
		}
		if a.Datum != b.Datum {
			return a.Datum > b.Datum
		}
		return a.Kennung < b.Kennung
	})
	return out
}
