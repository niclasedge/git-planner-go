// OpenSpec als Aufgabenquelle (openspec-quelle, #10).
//
// In einem Repo, das mit GitHub-Issue + OpenSpec-Change arbeitet, liegt der
// Arbeitsstand an zwei Orten: die Issues auf GitHub, die Changes unter
// openspec/changes/. Diese Quelle bringt beides in einen Baum:
//
//	offenes Issue            (Wurzel, wie bei der github-Quelle: "#N")
//	└─ Change KENNUNG        (Status und Stand in ReportsBead.Openspec)
//	   └─ Kaestchen der tasks.md
//
// Ein Change, der zu keinem offenen Issue gehoert, steht als eigene Wurzel da
// und traegt Openspec.Issuelos; ein Issue ohne Change ist eine Wurzel ohne
// Kinder. In einem Repo mit openspec/ ersetzt die Quelle die github-Quelle
// (Ersetzer) — sonst stuende jedes Issue zweimal in der Liste.
//
// Die Quelle liest nur: nichts nach GitHub, nichts in die Change-Ordner.
package appctx

import (
	"strings"

	"github.com/niclasedge/git-planner-go/internal/collectors"
)

// HerkunftOpenspec ist die Herkunft der openspec-Quelle.
const HerkunftOpenspec = "openspec"

// openspecGithub holt die Issues. Dieselbe Abfrage samt Cache und
// Token-Logik wie die github-Quelle; eine Variable, damit die Tests sie ohne
// Netz ersetzen koennen.
var openspecGithub = collectors.CollectGitHubCached

// OpenspecStand ist, was ein Change ueber sich sagt. Er haengt an der Zeile
// des Changes (ReportsBead.Openspec) und fehlt an Issues und Aufgaben.
type OpenspecStand struct {
	Kennung string `json:"kennung"`
	// Status ist collectors.OpenspecFrei, …Laeuft, …Fertig, …ImPR oder
	// …Erledigt (archiviert). ReportsBead.Status traegt daneben die Sprache
	// der Ansichten (open, in_progress, closed).
	Status     string                     `json:"status"`
	Erledigt   int                        `json:"erledigt"`
	Gesamt     int                        `json:"gesamt"`
	Gates      []string                   `json:"gates"`
	Issue      int                        `json:"issue,omitempty"`
	Issuelos   bool                       `json:"issuelos"`
	Archiviert bool                       `json:"archiviert"`
	Belege     []collectors.OpenspecBeleg `json:"belege,omitempty"`
}

// openspecQuelle ist der Adapter der Quellen-Tabelle: nur lesend, ohne
// Termine, mit Belegen (belege.jsonl).
type openspecQuelle struct{}

func (openspecQuelle) Herkunft() string { return HerkunftOpenspec }

func (openspecQuelle) Faehigkeiten() Faehigkeiten {
	return Faehigkeiten{Schreibbar: false, Termine: false, Belege: true}
}

// Ersetzt nennt github fuer ein Projekt mit openspec/: dort liefert diese
// Quelle die Issues.
func (openspecQuelle) Ersetzt(pfad string) []string {
	if collectors.HatOpenspec(pfad) {
		return []string{HerkunftGithub}
	}
	return nil
}

// Karten liefert den Baum. Scheitert die GitHub-Abfrage, kommen die Changes
// trotzdem — als Wurzeln — und der Grund als Fehler daneben
// (quellKartenAus behaelt beides).
func (openspecQuelle) Karten(pfad string) ([]ReportsMap, error) {
	if !collectors.HatOpenspec(pfad) {
		return nil, nil
	}
	gh := openspecGithub(pfad)
	maps, grund := OpenspecKarten(collectors.OpenspecChanges(pfad), gh)
	if grund != "" {
		return maps, openspecGrund(grund)
	}
	return maps, nil
}

type openspecGrund string

func (g openspecGrund) Error() string { return string(g) }

// openspecIssuesGrund sagt, warum keine Issues da sind — leer, wenn GitHub
// geantwortet hat, auch mit null offenen Issues: ein Repo ohne offene Issues
// ist kein Fehler.
func openspecIssuesGrund(gh map[string]any) string {
	if gh == nil {
		return "GitHub wurde nicht abgefragt"
	}
	if s := aStr(gh["issues_error"]); s != "" {
		return s
	}
	if !aBool(gh["available"]) {
		if s := aStr(gh["reason"]); s != "" {
			return s
		}
		return "GitHub nicht verfügbar"
	}
	return ""
}

// OpenspecKarten baut aus den Changes und der Antwort von
// collectors.CollectGitHub die Karte des Repos. Der zweite Wert ist der Grund,
// wenn die Issues fehlen.
func OpenspecKarten(changes []collectors.OpenspecChange, gh map[string]any) ([]ReportsMap, string) {
	grund := openspecIssuesGrund(gh)
	var offen []collectors.GhIssue
	if issues, ok := gh["issues"].([]collectors.GhIssue); ok {
		for _, i := range issues {
			if !i.Closed() {
				offen = append(offen, i)
			}
		}
	}
	nummer := make(map[int]int, len(offen)) // Issue-Nummer -> Index in offen
	for k, i := range offen {
		nummer[i.Number] = k
	}

	kinder := make([][]ReportsBead, len(offen))
	var waisen []ReportsBead
	for _, c := range changes {
		k, gefunden := -1, false
		if c.Issue != 0 {
			k, gefunden = nummer[c.Issue]
		} else {
			k, gefunden = openspecTitelTreffer(offen, c.Kennung)
		}
		b := openspecChangeBead(c, !gefunden && c.Issue == 0)
		if gefunden {
			kinder[k] = append(kinder[k], b)
			continue
		}
		waisen = append(waisen, b)
	}

	var wurzeln []ReportsBead
	for k, i := range offen {
		b := ghBead(i)
		if len(kinder[k]) > 0 {
			b.Children = kinder[k]
			c := countBeads(b.Children)
			b.Counts = &c
		}
		wurzeln = append(wurzeln, b)
	}
	wurzeln = append(wurzeln, waisen...)
	if len(wurzeln) == 0 {
		return nil, grund
	}
	nwo := aStr(gh["nwo"])
	if nwo == "" {
		nwo = "openspec"
	}
	return []ReportsMap{{ID: nwo, Title: nwo, Beads: wurzeln, Counts: countBeads(wurzeln)}}, grund
}

// openspecTitelTreffer sucht ein offenes Issue mit dem Titel "KENNUNG: …".
func openspecTitelTreffer(offen []collectors.GhIssue, kennung string) (int, bool) {
	praefix := kennung + ":"
	for k, i := range offen {
		if strings.HasPrefix(strings.TrimSpace(i.Title), praefix) {
			return k, true
		}
	}
	return -1, false
}

// openspecChangeBead ist die Zeile eines Changes. Ohne Aufgaben ist er ein
// Blatt; mit Aufgaben traegt Counts den Fortschritt.
func openspecChangeBead(c collectors.OpenspecChange, issuelos bool) ReportsBead {
	status := c.Status()
	b := ReportsBead{
		ID:       c.Kennung,
		Title:    c.Kennung,
		Status:   openspecAnsichtStatus(status),
		Priority: 2,
		Openspec: &OpenspecStand{
			Kennung:    c.Kennung,
			Status:     status,
			Erledigt:   c.Erledigt(),
			Gesamt:     len(c.Aufgaben),
			Gates:      c.Gates,
			Issue:      c.Issue,
			Issuelos:   issuelos,
			Archiviert: c.Archiviert,
			Belege:     c.Belege,
		},
	}
	for n, a := range c.Aufgaben {
		s := "open"
		if a.Erledigt || c.Archiviert {
			s = statusGeschlossen
		}
		b.Children = append(b.Children, ReportsBead{
			ID:       c.Kennung + "/" + itoa(n+1),
			Title:    a.Text,
			Status:   s,
			Priority: 2,
		})
	}
	if len(b.Children) > 0 {
		k := countBeads(b.Children)
		b.Counts = &k
	}
	return b
}

// openspecAnsichtStatus uebersetzt den Stand in die Sprache der Ansichten:
// frei ist offen, erledigt ist geschlossen, alles dazwischen ist in Arbeit.
func openspecAnsichtStatus(s string) string {
	switch s {
	case collectors.OpenspecFrei:
		return "open"
	case collectors.OpenspecErledigt:
		return statusGeschlossen
	}
	return istInArbeit
}
