// Die Quellen der Aufgaben-Ansichten (aufgaben-quelle, #9).
//
// Bis hier kannten die Ansichten genau zwei Quellen, beads und GitHub, und
// beide standen als eigene Parameter in den Signaturen (ansichtAus,
// AnsichtenBauenMit). Eine dritte Quelle haette jede dieser Stellen um einen
// weiteren Parameter verlaengert, und die Frage "ist das beads?" stand an
// mehreren Stellen einzeln, obwohl sie eigentlich eine Faehigkeit meint: ob
// eine Aufgabe schreibbar ist.
//
// Deshalb gibt es hier eine Tabelle wie die der Regeln (regelAnmelden): eine
// Quelle meldet sich in einem init an und liefert je Projektpfad Karten in der
// gemeinsamen Uebergabeform []ReportsMap. Ansichten laufen ueber eine Liste
// []QuellKarten, in der jede Quelle gleichrangig steht.
package appctx

import (
	"time"

	"github.com/niclasedge/git-planner-go/internal/collectors"
)

// Faehigkeiten sagen, was die Aufgaben einer Quelle koennen.
//
// Schreibbar wirkt: eine schreibbare Quelle urteilt selbst ueber Blocker
// (ReportsBead.Ready), und ihre Eintraege stehen im geladenen Baum, schicken
// Titel, Status und URL also nicht mit. Termine und Belege stehen im Vertrag,
// wirken aber noch nicht — erst, wenn eine Quelle sie braucht (#8).
type Faehigkeiten struct{ Schreibbar, Termine, Belege bool }

// Quelle liefert die Aufgaben eines Projekts als Karten.
//
// Karten ist fail-open gemeint: kann die Quelle fuer dieses Projekt nichts
// liefern, gibt sie einen Fehler zurueck und keine Panik. Der Fehlertext wird
// als Grund angezeigt, die anderen Quellen laufen weiter. Kann sie nur einen
// Teil liefern (openspec ohne gh, #10), gibt sie die Karten UND den Fehler
// zurueck; beide bleiben stehen.
type Quelle interface {
	Herkunft() string
	Faehigkeiten() Faehigkeiten
	Karten(projektPfad string) ([]ReportsMap, error)
}

// Ersetzer ist optional: eine Quelle, die fuer ein Projekt andere Herkuenfte
// ersetzt (fuer #10: openspec ersetzt github in Repos mit openspec/).
// KartenAllerQuellen befragt eine ersetzte Quelle fuer dieses Projekt nicht.
type Ersetzer interface {
	Ersetzt(projektPfad string) []string
}

// QuellKarten ist die Antwort einer Quelle fuer ein Projekt. Grund ist leer,
// wenn die Quelle vollstaendig geantwortet hat, sonst steht dort, warum Maps
// leer oder unvollstaendig ist.
type QuellKarten struct {
	Herkunft     string
	Faehigkeiten Faehigkeiten
	Maps         []ReportsMap
	Grund        string
}

var quellen []Quelle

// QuellenRegistrieren meldet eine Quelle an. Die Reihenfolge der Anmeldung ist
// die Reihenfolge der Karten in den Ansichten.
func QuellenRegistrieren(q Quelle) {
	quellen = append(quellen, q)
}

// Quellen gibt die angemeldeten Quellen als Kopie heraus.
func Quellen() []Quelle {
	out := make([]Quelle, len(quellen))
	copy(out, quellen)
	return out
}

// beads zuerst, dann GitHub: in dieser Reihenfolge standen die beiden festen
// Parameter, und in dieser Reihenfolge stehen ihre Zeilen in den Ansichten.
// Alle in EINER Datei angemeldet, weil die Reihenfolge der init-Funktionen
// ueber Dateien hinweg an den Dateinamen haengt. openspec (#10) steht hinter
// github: in einem Repo mit openspec/ tritt es an dessen Stelle, also auch an
// dessen Stelle in der Zeilenfolge.
func init() {
	QuellenRegistrieren(beadsQuelle{})
	QuellenRegistrieren(githubQuelle{})
	QuellenRegistrieren(openspecQuelle{})
}

// KartenAllerQuellen befragt jede angemeldete Quelle fuer projektPfad.
func KartenAllerQuellen(projektPfad string) []QuellKarten {
	return kartenAusQuellen(projektPfad, Quellen())
}

// kartenAusQuellen ist KartenAllerQuellen ueber eine gegebene Liste. Eine
// Herkunft, die eine andere Quelle der Liste fuer dieses Projekt ersetzt, wird
// nicht befragt und liefert keinen Eintrag.
func kartenAusQuellen(projektPfad string, qs []Quelle) []QuellKarten {
	ersetzt := map[string]bool{}
	for _, q := range qs {
		e, ok := q.(Ersetzer)
		if !ok {
			continue
		}
		for _, h := range e.Ersetzt(projektPfad) {
			if h != q.Herkunft() {
				ersetzt[h] = true
			}
		}
	}
	out := make([]QuellKarten, 0, len(qs))
	for _, q := range qs {
		if ersetzt[q.Herkunft()] {
			continue
		}
		out = append(out, quellKartenAus(q, projektPfad))
	}
	return out
}

func quellKartenAus(q Quelle, projektPfad string) QuellKarten {
	qk := QuellKarten{Herkunft: q.Herkunft(), Faehigkeiten: q.Faehigkeiten()}
	maps, err := q.Karten(projektPfad)
	// Karten neben einem Fehler bleiben stehen (openspec ohne gh, #10). beads
	// und github liefern bei einem Fehler keine, fuer sie ist das dasselbe
	// wie vorher.
	qk.Maps = maps
	if err != nil {
		qk.Grund = err.Error()
		if qk.Grund == "" {
			qk.Grund = q.Herkunft() + " nicht verfügbar"
		}
	}
	return qk
}

// quellenOhne ist die Tabelle ohne die genannten Herkuenfte. Der
// projektuebergreifende Pfad laesst GitHub weg wie bisher (gkp-isj): ein
// Netzaufruf je Projekt gehoert nicht in den Sammellauf.
func quellenOhne(herkuenfte ...string) []Quelle {
	weg := map[string]bool{}
	for _, h := range herkuenfte {
		weg[h] = true
	}
	var out []Quelle
	for _, q := range Quellen() {
		if !weg[q.Herkunft()] {
			out = append(out, q)
		}
	}
	return out
}

// projektStempeln traegt den Projektnamen an jede Karte, die noch keinen hat.
// Die beads-Karten tragen ihn aus baueKarten (beads.Name); die anderen Quellen
// kennen nur ihre eigene Kennung (GitHub: die nwo). Genommen wird der Name der
// ersten gestempelten Karte, sonst collectors.ReportsRepoName — derselbe Name
// ueber Git, ohne bd anzufassen (gkp-k1o).
func projektStempeln(karten []QuellKarten, projektPfad string) {
	name := ""
	fehlt := false
	for _, qk := range karten {
		for _, m := range qk.Maps {
			if m.Projekt == "" {
				fehlt = true
			} else if name == "" {
				name = m.Projekt
			}
		}
	}
	if !fehlt {
		return
	}
	if name == "" {
		name = collectors.ReportsRepoName(projektPfad)
	}
	for i := range karten {
		for j := range karten[i].Maps {
			if karten[i].Maps[j].Projekt == "" {
				karten[i].Maps[j].Projekt = name
			}
		}
	}
}

// AnsichtenBauenAus baut alle angemeldeten Ansichten aus einer Kartenliste.
// Jede QuellKarten laeuft gleichrangig durch dieselbe Regel, Deutung und
// Sortierung.
func AnsichtenBauenAus(karten []QuellKarten, jetzt time.Time) map[string]AufgabenAnsicht {
	out := make(map[string]AufgabenAnsicht, len(regeln))
	for _, r := range regeln {
		out[r.ID] = ansichtAus(r, karten, jetzt)
	}
	return out
}

// quellKartenMit ist die Kartenliste der alten Einstiege mit zwei festen
// Parametern: beads, dann GitHub.
func quellKartenMit(maps, github []ReportsMap) []QuellKarten {
	return []QuellKarten{
		{Herkunft: HerkunftBeads, Faehigkeiten: beadsQuelle{}.Faehigkeiten(), Maps: maps},
		{Herkunft: HerkunftGithub, Faehigkeiten: githubQuelle{}.Faehigkeiten(), Maps: github},
	}
}
