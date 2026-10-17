// GitHub als zweite Aufgabenquelle der Ansichten (gkp-pf2, Spec §4.2 / E1).
//
// Das Herkunfts-Abzeichen steht seit gkp-7gb an jeder Zeile — es las bis hier
// immer "beads", weil nie ein GitHub-Kandidat in den Strom kam. Diese Datei ist
// die fehlende Quelle, und sonst nichts: Auswahl, Reihenfolge und Einrueckung
// macht derselbe Rahmen wie fuer beads (ansicht.go), damit ein Filter oder eine
// Sortierregel nicht zweimal existiert und irgendwann auseinanderlaeuft.
//
// DREI Dinge tut diese Datei bewusst NICHT:
//
//  1. Sie mischt die Issues nicht in ReportsView.Maps. Dort zaehlen
//     Fortschritt, Belegstand (Proven/Gaps), Historie und knownBeadIDs mit —
//     ein GitHub-Issue als Bead zu fuehren hiesse, es als beleglos geschlossen
//     zu zaehlen und in die Belegluecken einzurechnen. Die Karten hier sind
//     eine getrennte Liste, die nur die Ansichten sehen.
//
//  2. Sie erkennt keine Doppelerfassung. `infra` fuehrt 199 Beads UND 209
//     GitHub-Issues (Spec §3.7); dieselbe Arbeit kann also zweimal in einer
//     Liste stehen. Das ist der bewusst in Kauf genommene Preis von E1 — die
//     Ansicht macht ihn ueber das Abzeichen sichtbar, sie raeumt ihn nicht auf
//     (O2 in §9).
//
//  3. Sie erfindet keine Termine. GitHub hat kein Gegenstueck zu When und
//     Deadline (Spec §4.4), also bleiben DueAt und DeferUntil leer. Die Folge
//     ist nicht kosmetisch: eine GitHub-Aufgabe kann damit keinen Auswahlgrund
//     von Heute oder Demnaechst erfuellen. Jede Ansicht, der das passiert, sagt
//     es im Kopf (Regel.Hinweis) — eine leere Sektion saehe nach "nichts zu
//     tun" aus statt nach "kann hier nicht stehen".
package appctx

import (
	"errors"
	"sort"

	"github.com/niclasedge/git-planner-go/internal/collectors"
)

// AnsichtenMitGithub ist die Antwort der Route GET /ansichten: dieselben
// Aufgaben-Ansichten wie im Rumpf der Seite, nur ueber BEIDE Quellen.
//
// Eine eigene, lazy geholte Route aus demselben Grund wie GET /github und
// GET /plan-svg: die GitHub-Haelfte kostet einen Netzaufruf, und der gehoert
// nicht in den Pfad, der ein Projekt oeffnet (gkp-isj). Die Seite steht also
// zuerst mit den beads-Zeilen, die GitHub-Zeilen kommen nach.
//
// Gemischt wird HIER und nicht im Browser. Das ist die Konvention aus
// CLAUDE.md, und sie ist an dieser Stelle keine Formsache: zwei Listen
// ineinanderzuschieben heisst sortieren, und eine zweite Sortierregel in
// browser.js ist genau der Defekt, der bei der Kopfbilanz schon einmal
// entstanden ist (JS vergleicht Code-Einheiten, "überfällig" landete hinter
// jedem ASCII-Wort). Der Browser bekommt die fertige Liste und ersetzt seine.
//
// Seit aufgaben-quelle (#9) kommen die Karten aus der Quellen-Tabelle
// (KartenAllerQuellen): beads und GitHub als Adapter, jede weitere
// angemeldete Quelle ohne Eingriff hier.
func AnsichtenMitGithub(cwd string) map[string]any {
	karten := KartenAllerQuellen(cwd)
	_, _, ansichten := standAus(cwd, "", karten, jetztFuerAnsichten())
	// Warum die GitHub-Haelfte leer ist, steht in derselben Antwort. Ohne das
	// ist "keine GitHub-Zeilen" von "GitHub nicht erreichbar" nicht zu
	// unterscheiden, und die Ansicht saehe vollstaendig aus, ohne es zu sein.
	// Den Text liefert githubQuelle.Karten als Grund (ghLeerGrund).
	leer := ""
	for _, qk := range karten {
		if qk.Herkunft == HerkunftGithub && len(qk.Maps) == 0 {
			leer = qk.Grund
		}
	}
	return AnsichtenAntwort(ansichten, leer)
}

// githubQuelle ist der GitHub-Adapter der Quellen-Tabelle (aufgaben-quelle,
// #9): nur lesend, ohne Termine und Belege. Die Issues kommen aus dem Cache
// von collectors.CollectGitHubCached.
type githubQuelle struct{}

func (githubQuelle) Herkunft() string { return HerkunftGithub }

func (githubQuelle) Faehigkeiten() Faehigkeiten { return Faehigkeiten{} }

// Karten liefert die Karte des Repos. Traegt sie keine Issues, ist das der
// Grund der Quelle — derselbe Text, den die Route bisher als github_leer
// zeigte (ghLeerGrund), auch fuer "keine Issues in o/r".
func (githubQuelle) Karten(pfad string) ([]ReportsMap, error) {
	gh := collectors.CollectGitHubCached(pfad)
	karten := GithubKartenAus(gh)
	if len(karten) == 0 {
		return nil, errors.New(ghLeerGrund(gh))
	}
	return karten, nil
}

// AnsichtenAntwort ist der RUMPF der Route — ohne Netz, ohne bd, ohne
// Verzeichnis. Alles, was AnsichtenMitGithub darueber hinaus tut, ist die
// beiden Quellen zu beschaffen.
//
// Die Trennung ist der Grund, aus dem dieser Rumpf ueberhaupt pruefbar ist: ein
// Test, der ihn von Hand nachbaut, prueft eine Welt, die es nicht gibt
// (Spec §9.1, Fall 3). Hier baut ihn der Produktionscode, und der Test fuettert
// nur die beiden Listen.
func AnsichtenAntwort(ansichten map[string]AufgabenAnsicht, leer string) map[string]any {
	out := map[string]any{
		"ansichten": ansichten,
		// Die Zahlen der Navigation reisen MIT (gkp-d52).
		//
		// Ohne sie stand die Navigation auf dem Stand des Seitenaufbaus — also
		// ohne GitHub — waehrend der Kopf der geoeffneten Liste die
		// vollstaendige Zahl zeigte: gemessen "Jederzeit 47" neben "57
		// Aufgabe(n)", die Differenz genau die zehn offenen Issues. Die Zahl
		// neben einer Ansicht ist die einzige Angabe, die ein Benutzer sieht,
		// OHNE sie zu oeffnen; stimmt sie nicht mit dem ueberein, was beim
		// Oeffnen dasteht, ist sie schlimmer als keine Zahl.
		//
		// Gerechnet wird sie HIER und nicht im Browser — dieselbe Regel und
		// dieselbe Begruendung wie bei der Kopfbilanz in gkp-7gb (Spec §5.0):
		// eine Zahl, die erst im Browser entsteht, ist eine Zahl, die niemand
		// testen kann. Und zwar mit DERSELBEN Funktion, die sie beim
		// Seitenaufbau erzeugt (builddata.go) — zwei Zaehlungen nebeneinander
		// waeren zwei Gelegenheiten, verschieden zu zaehlen.
		//
		// Der dyn-Umweg ist der Preis dafuer: ansichtenNav liest die dynamische
		// Form, weil BuildData den ganzen Kontext ueber einen JSON-Rundlauf
		// normalisiert. Sechs Ansichten einmal zu serialisieren kostet nichts
		// gegen den Netzaufruf, der dieser Antwort ohnehin vorausgeht.
		"nav": ansichtenNav(map[string]any{"ansichten": dyn(ansichten)}),
	}
	if leer != "" {
		out["github_leer"] = leer
	}
	return out
}

// ghLeerGrund benennt, warum keine GitHub-Aufgabe dabei ist. Die Gruende
// stammen alle aus collectors.CollectGitHub selbst — hier wird keiner neu
// erfunden, nur der zutreffende ausgewaehlt.
func ghLeerGrund(antwort map[string]any) string {
	if antwort == nil {
		return "GitHub wurde nicht abgefragt"
	}
	if s := aStr(antwort["issues_error"]); s != "" {
		return s
	}
	if !aBool(antwort["available"]) {
		if s := aStr(antwort["reason"]); s != "" {
			return s
		}
		return "GitHub nicht verfügbar"
	}
	return "keine Issues in " + aStr(antwort["nwo"])
}

// GithubKartenAus liest die Karten aus der Antwort von
// collectors.CollectGitHub — also aus genau der Form, die auch GET /github
// ausliefert, statt aus einer zweiten, hier nachgebauten.
//
// Fail-open an jeder Stelle, wie die Karte selbst: kein Remote, kein Token,
// eine ablehnende API oder ein nil ergeben keine Aufgaben und keinen Fehler.
// Eine Ansicht ohne GitHub ist die beads-Ansicht von vorher, nicht eine
// kaputte.
func GithubKartenAus(antwort map[string]any) []ReportsMap {
	if antwort == nil {
		return nil
	}
	issues, ok := antwort["issues"].([]collectors.GhIssue)
	if !ok || len(issues) == 0 {
		return nil
	}
	nwo := aStr(antwort["nwo"])
	if nwo == "" {
		nwo = "GitHub"
	}
	return GithubKarten(nwo, issues)
}

// GithubKarten macht aus den Issues EINES Repos die Karte, die die Ansichten
// neben den beads-Karten lesen.
//
// Eine Karte je Repo und nicht je Meilenstein oder Label: nach Spec §4.1 ist
// die Ebene ueber der Aufgabe das PROJEKT, und das Projekt ist das Repository.
// Ihre Id ist die nwo (`example-user/infra`) — sie kann mit keiner Epic-Id
// kollidieren, weil ein Schraegstrich in einer bd-Id nicht vorkommt, und sie
// ist zugleich der Text, den die Ansicht in der Karten-Spalte zeigt.
func GithubKarten(nwo string, issues []collectors.GhIssue) []ReportsMap {
	if len(issues) == 0 {
		return nil
	}
	// Die Schachtelung steckt im parent-Feld, nicht in der Nummer — dieselbe
	// Regel wie bei beads (Spec §3.2, §8.3).
	kinder := map[int][]collectors.GhIssue{}
	bekannt := make(map[int]bool, len(issues))
	for _, i := range issues {
		bekannt[i.Number] = true
	}
	var wurzeln []collectors.GhIssue
	for _, i := range issues {
		// Ein Elternteil, das nicht mitgeholt wurde (Paginierung abgeschnitten,
		// Issue geloescht oder in ein anderes Repo verschoben), macht aus dem
		// Kind eine Wurzel statt es verschwinden zu lassen. Verschluckt saehe
		// die Liste vollstaendig aus — und genau diese Sorte Luecke ist der
		// Grund, aus dem die Ansicht ueberhaupt existiert.
		//
		// i.Parent == i.Number faengt denselben Zweig mit ab: eine Selbstkante
		// waere sonst eine Endlosschleife im Abstieg.
		if i.Parent != 0 && i.Parent != i.Number && bekannt[i.Parent] {
			kinder[i.Parent] = append(kinder[i.Parent], i)
			continue
		}
		wurzeln = append(wurzeln, i)
	}
	beads := ghBeads(wurzeln, kinder, map[int]bool{})
	if len(beads) == 0 {
		return nil
	}
	return []ReportsMap{{
		ID:    nwo,
		Title: nwo,
		Beads: beads,
		// Counts ueber ALLE Nachfahren, wie bei einer beads-Karte (Spec §6).
		// Gerechnet mit derselben Funktion, nicht mit subIssuesSummary: die
		// zaehlt nur direkte Kinder und waere ueber einen Enkel hinweg falsch.
		Counts: countBeads(beads),
	}}
}

// ghBeads baut einen Ast. gesehen schneidet die Rueckkante, falls GitHub je
// einen Zyklus liefert — der Abstieg muss endlich sein, und ein Zyklus darf
// den Dienst nicht mitnehmen.
func ghBeads(issues []collectors.GhIssue, kinder map[int][]collectors.GhIssue, gesehen map[int]bool) []ReportsBead {
	if len(issues) == 0 {
		return nil
	}
	out := make([]ReportsBead, 0, len(issues))
	for _, i := range issues {
		if gesehen[i.Number] {
			continue
		}
		gesehen[i.Number] = true
		b := ghBead(i)
		b.Children = ghBeads(kinder[i.Number], kinder, gesehen)
		if len(b.Children) > 0 {
			c := countBeads(b.Children)
			b.Counts = &c
			// Ein Ast ist mindestens so neu wie sein juengstes Kind — dieselbe
			// Regel wie in buildReportsBeads, damit die Sortierung den Ast
			// nicht auseinanderreisst.
			for _, k := range b.Children {
				if k.LastChange > b.LastChange {
					b.LastChange = k.LastChange
				}
			}
		}
		out = append(out, b)
	}
	// Juengste Aenderung oben, dann die Nummer — dieselbe Folge wie bei beads.
	// Vollstaendig bis zum eindeutigen Schluessel, weil ein Schluessel, der
	// vorher abbricht, die Reihenfolge der API uebernimmt; die ist zwischen
	// zwei Aufrufen nicht stabil, und ein Test dagegen haelt nichts fest.
	sort.SliceStable(out, func(a, b int) bool {
		if out[a].LastChange != out[b].LastChange {
			return out[a].LastChange > out[b].LastChange
		}
		return out[a].ID < out[b].ID
	})
	return out
}

// ghBead ist die Uebersetzung eines Issues in die Form, die der Rahmen liest.
//
// Was hier NICHT gesetzt wird, ist die Aussage dieser Funktion:
//
//   - Ready bleibt false. `bd ready` ist bds eigenes Urteil ueber Blocker;
//     GitHub hat keines. Ein erfundenes true wuerde jede GitHub-Aufgabe in
//     Heute ziehen und die Einschraenkung aus E1 stillschweigend aufheben.
//   - DueAt/DeferUntil bleiben leer (Spec §4.4).
//   - Proven und Gap bleiben false. Der Report-Store kennt nur Beads; ein
//     geschlossenes Issue als "Beleglücke" zu zaehlen waere eine Beanstandung
//     gegen ein System, das dieser Belegpflicht nie unterlag.
//   - Entries bleibt leer. Kommentare eines Issues sind keine Report-Eintraege.
func ghBead(i collectors.GhIssue) ReportsBead {
	status := "open"
	closedAt := ""
	if i.Closed() {
		status = statusGeschlossen
		closedAt = i.ClosedAt
	}
	return ReportsBead{
		// "#209" und nicht "209": die Raute ist die Schreibweise, in der ein
		// Mensch ein Issue nennt, und sie kann in keiner bd-Id vorkommen — eine
		// Zeile ist damit auf einen Blick der richtigen Quelle zuzuordnen, auch
		// ohne das Abzeichen daneben.
		ID:     "#" + itoa(i.Number),
		Title:  i.Title,
		Status: status,
		// GitHub kennt keine Prioritaet. 2 ist bds Mitte und der Wert, auf den
		// auch browser.js zurueckfaellt (rpPrio); 0 waere P0 und damit eine
		// erfundene Dringlichkeit, die die Aufgabe in der Sortierung nach oben
		// zoege. Gezeigt wird das Abzeichen an einer GitHub-Zeile trotzdem
		// nicht — eine Zahl ohne Quelle ist keine Auskunft.
		Priority: 2,
		ClosedAt: closedAt,
		// UpdatedAt ist echtes UTC (GitHub, nicht bd) und braucht deshalb KEINE
		// Kompensation aus Spec §4.4 — die gilt nur fuer bds due_at und
		// defer_until. Als Sortierschluessel ist das Format dasselbe wie bei
		// LastChange eines Beads, ein Stringvergleich also zulaessig.
		LastChange: i.UpdatedAt,
		URL:        i.URL,
	}
}
