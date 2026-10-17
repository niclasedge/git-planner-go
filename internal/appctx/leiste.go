// Die Leiste der Things-Oberflaeche (projektliste-api, #11): Bereiche, darin
// je Projekt genau eine Zeile mit drei Zaehlern.
//
// Die Reihenfolge ist die von BereicheGruppieren und wird hier nicht ein
// zweites Mal entschieden; diese Datei haengt nur die Zaehler an. Gezaehlt
// wird in Go, damit die Oberflaeche `offen` ohne Rechnung als die eine Zahl
// der Projektzeile zeigen kann (CLAUDE.md: Auswahl und Sortierung gehoeren
// nach Go).
package appctx

import (
	"time"

	"github.com/niclasedge/git-planner-go/internal/areas"
)

// ProjektZaehler sind die Zahlen einer Projektzeile. InArbeit und
// Ueberfaellig sind Teilmengen von Offen: Offen ist alles nicht Geschlossene.
type ProjektZaehler struct {
	Offen        int `json:"offen"`
	InArbeit     int `json:"in_arbeit"`
	Ueberfaellig int `json:"ueberfaellig"`
}

// LeistenProjekt ist eine Projektzeile der Leiste.
type LeistenProjekt struct {
	Name    string         `json:"name"`
	Pfad    string         `json:"pfad"`
	Zaehler ProjektZaehler `json:"zaehler"`
}

// LeistenBereich ist ein Bereich der Leiste. Ohne markiert "Ohne Bereich" und
// steht immer im Rumpf, auch als false: die Oberflaeche soll nicht zwischen
// einem fehlenden und einem falschen Feld unterscheiden muessen.
type LeistenBereich struct {
	Name     string           `json:"name"`
	Ohne     bool             `json:"ohne"`
	Projekte []LeistenProjekt `json:"projekte"`
}

// Leiste ist die Antwort von GET /api/leiste. Stand ist der Zeitpunkt, zu dem
// dieser Stand berechnet wurde, als RFC3339.
type Leiste struct {
	Bereiche []LeistenBereich `json:"bereiche"`
	Stand    string           `json:"stand"`
}

// LeisteBauen ordnet die Projekte nach BereicheGruppieren und haengt an jedes
// die Zaehler seines Pfads. Ein Projekt ohne Eintrag in zaehler bekommt
// Nullen, keine Luecke.
func LeisteBauen(projekte []BereichProjekt, st areas.State, zaehler map[string]ProjektZaehler, stand time.Time) Leiste {
	gruppen := BereicheGruppieren(projekte, st)
	out := Leiste{Bereiche: make([]LeistenBereich, 0, len(gruppen)), Stand: stand.Format(time.RFC3339)}
	for _, g := range gruppen {
		b := LeistenBereich{Name: g.Bereich, Ohne: g.Ohne, Projekte: make([]LeistenProjekt, 0, len(g.Projekte))}
		for _, p := range g.Projekte {
			b.Projekte = append(b.Projekte, LeistenProjekt{Name: p.Name, Pfad: p.Pfad, Zaehler: zaehler[p.Pfad]})
		}
		out.Bereiche = append(out.Bereiche, b)
	}
	return out
}

// LeistenZaehler zaehlt die nicht geschlossenen Eintraege eines Projekts ueber
// alle Quellen und Ebenen.
//
// Eine Karte zaehlt selbst mit, wenn sie eine Aufgabe ist (ein Epic). Die
// Behaelter ohne eigene Aufgabe zaehlen nicht: die lose Karte "ohne Epic", die
// Karte aus unbekannten Report-Dateien und die GitHub-Karte, die nur das Repo
// benennt und keinen Status traegt.
func LeistenZaehler(karten []QuellKarten, jetzt time.Time) ProjektZaehler {
	heute := tagesAnfang(jetzt)
	var z ProjektZaehler
	for _, qk := range karten {
		for _, m := range qk.Maps {
			if karteIstAufgabe(m) {
				zaehleStatus(&z, m.Status, "", heute)
			}
			zaehleBeads(&z, m.Beads, heute)
		}
	}
	return z
}

func karteIstAufgabe(m ReportsMap) bool {
	return !m.Loose && !m.Unknown && m.Status != ""
}

func zaehleBeads(z *ProjektZaehler, beads []ReportsBead, heute time.Time) {
	for _, b := range beads {
		zaehleStatus(z, b.Status, b.DueAt, heute)
		zaehleBeads(z, b.Children, heute)
	}
}

func zaehleStatus(z *ProjektZaehler, status, dueAt string, heute time.Time) {
	if status == statusGeschlossen {
		return
	}
	z.Offen++
	if status == istInArbeit {
		z.InArbeit++
	}
	if _, tage := terminAus(dueAt, heute); tage != nil && *tage < 0 {
		z.Ueberfaellig++
	}
}
