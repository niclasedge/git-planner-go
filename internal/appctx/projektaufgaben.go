// Die Aufgaben eines Projekts als flache Zeilen (projektliste-api, #11): die
// offenen Wurzeln ueber alle Quellen oder die offenen direkten Kinder eines
// Eintrags, fertig sortiert.
//
// Flach heisst: keine Zeile traegt ihre Kinder. Die Oberflaeche laedt eine
// Ebene erst, wenn jemand sie aufklappt (Vorbild GET /belege), und sie
// entscheidet weder, was eine Wurzel ist, noch in welcher Folge die Zeilen
// stehen (CLAUDE.md: Auswahl und Sortierung gehoeren nach Go).
package appctx

import (
	"errors"
	"fmt"
	"sort"
	"time"
)

// Fortschritt ist erledigt/gesamt ueber ALLE Nachfahren eines Eintrags —
// dieselbe Zaehlweise wie MapCounts (Spec §6); hat_kinder und kinder_anzahl
// dagegen sagen, was beim Aufklappen erscheint, also nur die offene direkte
// Ebene.
type Fortschritt struct {
	Erledigt int `json:"erledigt"`
	Gesamt   int `json:"gesamt"`
}

// AufgabenZeile ist eine Zeile von GET /api/aufgaben.
type AufgabenZeile struct {
	ID           string      `json:"id"`
	Titel        string      `json:"titel"`
	Status       string      `json:"status"`
	Herkunft     string      `json:"herkunft"`
	URL          string      `json:"url"`
	HatKinder    bool        `json:"hat_kinder"`
	KinderAnzahl int         `json:"kinder_anzahl"`
	Fortschritt  Fortschritt `json:"fortschritt"`
	// Frist ist das Datum (YYYY-MM-DD, Ortszeit) oder leer. Eine Karte traegt
	// keinen eigenen Termin, ihre Zeile hat deshalb immer eine leere Frist.
	Frist      string `json:"frist"`
	Schreibbar bool   `json:"schreibbar"`

	prio  int
	frist *time.Time
}

// QuellStand nennt eine befragte Quelle; Grund ist leer, wenn sie geantwortet
// hat.
type QuellStand struct {
	Herkunft string `json:"herkunft"`
	Grund    string `json:"grund"`
}

// ProjektAufgabenListe ist die Antwort von GET /api/aufgaben. Projekt setzt
// der Aufrufer, der den Pfad aufgeloest hat.
type ProjektAufgabenListe struct {
	Projekt   string          `json:"projekt"`
	Eltern    string          `json:"eltern"`
	Eintraege []AufgabenZeile `json:"eintraege"`
	Quellen   []QuellStand    `json:"quellen"`
}

// ErrUnbekannterEltern: die eltern-Id kommt in keiner Quelle des Projekts vor.
var ErrUnbekannterEltern = errors.New("unbekannte eltern-Id")

// ProjektAufgaben baut die Zeilen: ohne eltern die offenen Karten aller
// Quellen, mit eltern die offenen direkten Kinder des Eintrags mit dieser Id
// (Karte oder Aufgabe, in jeder Tiefe).
func ProjektAufgaben(karten []QuellKarten, eltern string, jetzt time.Time) (ProjektAufgabenListe, error) {
	heute := tagesAnfang(jetzt)
	out := ProjektAufgabenListe{Eltern: eltern, Eintraege: []AufgabenZeile{}, Quellen: make([]QuellStand, 0, len(karten))}
	for _, qk := range karten {
		out.Quellen = append(out.Quellen, QuellStand{Herkunft: qk.Herkunft, Grund: qk.Grund})
	}

	if eltern == "" {
		for _, qk := range karten {
			for _, m := range qk.Maps {
				if m.Status == statusGeschlossen {
					continue
				}
				out.Eintraege = append(out.Eintraege, zeileAusKarte(m, qk))
			}
		}
		sortiereZeilen(out.Eintraege)
		return out, nil
	}

	kinder, qk, ok := kinderVon(karten, eltern)
	if !ok {
		return out, fmt.Errorf("%w: %q kommt in keiner Quelle dieses Projekts vor", ErrUnbekannterEltern, eltern)
	}
	for _, b := range kinder {
		if b.Status == statusGeschlossen {
			continue
		}
		out.Eintraege = append(out.Eintraege, zeileAusBead(b, qk, heute))
	}
	sortiereZeilen(out.Eintraege)
	return out, nil
}

// kinderVon sucht den Eintrag mit der Id eltern: zuerst unter den Karten, dann
// in den Baeumen, und liefert seine direkten Kinder samt Quelle.
func kinderVon(karten []QuellKarten, eltern string) ([]ReportsBead, QuellKarten, bool) {
	for _, qk := range karten {
		for _, m := range qk.Maps {
			if m.ID == eltern {
				return m.Beads, qk, true
			}
		}
	}
	for _, qk := range karten {
		for _, m := range qk.Maps {
			if b, ok := beadFinden(m.Beads, eltern); ok {
				return b.Children, qk, true
			}
		}
	}
	return nil, QuellKarten{}, false
}

func beadFinden(beads []ReportsBead, id string) (ReportsBead, bool) {
	for _, b := range beads {
		if b.ID == id {
			return b, true
		}
		if f, ok := beadFinden(b.Children, id); ok {
			return f, true
		}
	}
	return ReportsBead{}, false
}

func zeileAusKarte(m ReportsMap, qk QuellKarten) AufgabenZeile {
	status := m.Status
	if status == "" {
		status = "open" // die GitHub-Karte benennt nur das Repo und traegt keinen Status
	}
	z := AufgabenZeile{
		ID: m.ID, Titel: m.Title, Status: status, Herkunft: qk.Herkunft,
		Schreibbar: qk.Faehigkeiten.Schreibbar, prio: m.Priority,
	}
	kinderZaehlen(&z, m.Beads)
	return z
}

func zeileAusBead(b ReportsBead, qk QuellKarten, heute time.Time) AufgabenZeile {
	z := AufgabenZeile{
		ID: b.ID, Titel: b.Title, Status: b.Status, Herkunft: qk.Herkunft, URL: b.URL,
		Schreibbar: qk.Faehigkeiten.Schreibbar, prio: b.Priority,
	}
	if t, _ := terminAus(b.DueAt, heute); t != nil {
		z.frist = t
		z.Frist = t.Format("2006-01-02")
	}
	kinderZaehlen(&z, b.Children)
	return z
}

func kinderZaehlen(z *AufgabenZeile, kinder []ReportsBead) {
	for _, k := range kinder {
		if k.Status != statusGeschlossen {
			z.KinderAnzahl++
		}
	}
	z.HatKinder = z.KinderAnzahl > 0
	fortschrittZaehlen(&z.Fortschritt, kinder)
}

func fortschrittZaehlen(f *Fortschritt, beads []ReportsBead) {
	for _, b := range beads {
		f.Gesamt++
		if b.Status == statusGeschlossen {
			f.Erledigt++
		}
		fortschrittZaehlen(f, b.Children)
	}
}

// sortiereZeilen: in Arbeit vor offen und blockiert, dann Frist aufsteigend
// mit leerer Frist zuletzt, dann Prioritaet, dann Id — bis zum eindeutigen
// Schluessel, damit die Folge nicht an der Reihenfolge der Quelle haengt.
func sortiereZeilen(zs []AufgabenZeile) {
	sort.SliceStable(zs, func(i, j int) bool {
		a, b := zs[i], zs[j]
		if ai, bi := a.Status == istInArbeit, b.Status == istInArbeit; ai != bi {
			return ai
		}
		switch {
		case a.frist != nil && b.frist == nil:
			return true
		case a.frist == nil && b.frist != nil:
			return false
		case a.frist != nil && b.frist != nil && !a.frist.Equal(*b.frist):
			return a.frist.Before(*b.frist)
		}
		if a.prio != b.prio {
			return a.prio < b.prio
		}
		return a.ID < b.ID
	})
}

// QuellenFuerTest ersetzt die Quellen-Tabelle durch qs und gibt die Funktion
// zurueck, die die alte wiederherstellt. Fuer Tests ausserhalb dieses Pakets
// (Routen-Tests in internal/server), die ohne bd und ohne Netz laufen sollen.
func QuellenFuerTest(qs ...Quelle) func() {
	alt := quellen
	quellen = append([]Quelle(nil), qs...)
	return func() { quellen = alt }
}
