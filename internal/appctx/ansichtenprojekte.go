// Die Aufgaben-Ansichten UEBER ALLE Projekte (Spec §5 E6, gkp-i90).
//
// AnsichtenMitGithub (githubaufgaben.go) und StandNachMit (teilbaum.go) bauen
// die sechs Ansichten ueber EIN Projekt — den aktiven cwd. Diese Datei baut
// sie ueber JEDES entdeckte Projekt und laesst die Ein-Projekt-Funktionen
// unveraendert: welche Route welche der beiden ruft, entscheidet gkp-92i, hier
// steht nur der Sammler.
package appctx

import (
	"errors"
	"time"

	"github.com/niclasedge/git-planner-go/internal/collectors"
	"github.com/niclasedge/git-planner-go/internal/reports"
)

// sammellaufBudget ist die Gesamtfrist um EINEN Durchlauf von
// ansichtenUeberProjekte, siehe die Begruendung an ihrer Verwendungsstelle.
// Gewaehlt als das gut Doppelte des worst case EINES Projekts (~70s) — genug
// Luft fuer ein zweites, wirklich langsames Projekt, aber weit unter der
// n-fachen Summe. Ein var und keine const, damit ein Test sie verkuerzen kann
// (dieselbe Begruendung wie an shared.BdTimeoutMs/GitTimeoutMs).
var sammellaufBudget = 150 * time.Second

// ProjektFehler ist ein Projekt, dessen Aufgabenbaum sich nicht aufbauen
// liess. Es ist KEIN Fehler des Aggregats — fail-open wie ueberall in diesem
// Paket (vgl. StandNachMit, ghLeerGrund): ein kaputtes Projekt darf n-1 andere
// nicht mit sich reissen, und der Grund reist mit, damit "dieses eine Projekt
// fehlt" von "in diesem Projekt ist nichts los" unterscheidbar bleibt.
type ProjektFehler struct {
	Projekt string `json:"projekt"`
	Grund   string `json:"grund"`
}

// AnsichtenAlleProjekte baut die sechs Aufgaben-Ansichten (ansicht.go) ueber
// den Aufgabenbaum JEDES von collectors.DiscoverProjectsIn entdeckten
// Projekts — dieselbe Quelle wie die Projekt-Sidebar, damit es nur EINE
// Definition von "Projekt" im Programm gibt.
//
// GITHUB bleibt bewusst draussen (E1-Grenze dieser Karte, Kommentarkopf
// githubaufgaben.go): schon die beads-Haelfte kostet neun bd-Laeufe (gemessen
// 14 951 ms seriell, Spec §5 E6); 2*n weitere Netzaufrufe fuer GitHub gehoeren
// nicht in denselben Durchlauf. AnsichtenBauenMit nimmt eine github-Karte
// ohnehin als eigenen, zweiten Parameter — wer sie nachtraeglich einmischen
// will (wie avGithubNachziehen es fuer EIN Projekt tut), kann das spaeter tun,
// ohne diese Funktion zu aendern.
//
// TEUER, und deshalb NICHT fuer den Requestpfad gedacht: der Aufrufer gehoert
// hinter das Hintergrund-Aggregat mit stale-serve aus internal/cache
// (gkp-i90, Spec §5 E6) — genau das Muster, das GetProjectContext
// (internal/cache/cache.go) bereits fuer den Ein-Projekt-Fall vormacht. Diese
// Funktion selbst weiss davon nichts; sie ist die reine Berechnung, die der
// Cache in Abstaenden aufruft.
func AnsichtenAlleProjekte(aktivesRepo string, jetzt time.Time) (map[string]AufgabenAnsicht, []ProjektFehler) {
	projekte := collectors.DiscoverProjectsIn(collectors.ProjectRoots(), aktivesRepo)
	return ansichtenUeberProjekte(projekte, jetzt)
}

// ansichtenUeberProjekte ist der testbare Kern von AnsichtenAlleProjekte,
// getrennt von der Projekt-ENTDECKUNG: welche Verzeichnisse als Projekt
// zaehlen, ist collectors.DiscoverProjectsIn's eigene, dort bereits geprüfte
// Frage (sidebar_test.go). Was hier zu pruefen ist, ist ausschliesslich das
// MISCHEN — und dafuer genuegt eine von Hand uebergebene Projektliste, ohne
// dass ein Test echte GitHub-Remotes oder eine Verzeichnisstruktur unter
// ProjectRoots() braucht.
//
// GEMISCHT wird auf Ebene der KARTEN (ReportsMap), nicht auf Ebene der
// fertigen Ansichten: jedes Projekt liefert seine eigenen Karten — baueKarten
// stempelt sie bereits mit ihrem Projektnamen (reports_view.go:330..335) —,
// und die Karten ALLER Projekte laufen genau EINMAL durch AnsichtenBauenMit.
// Der Rahmen (ansicht.go: ansichtAus/sammle) kennt eine Kartenliste bereits:
// innerhalb EINES Projekts kann es mehrere Epics geben, und eine Liste aus
// mehreren PROJEKTEN ist fuer ihn keine neue Form — keine Zeile in ansicht.go
// musste dafuer geaendert werden. Zwei Folgen ergeben sich daraus von selbst:
//
//  1. Die Kopfbilanz (Zaehlstand) wird korrekt SUMMIERT statt aneinandergehaengt
//     — sie entsteht aus der bereits zusammengefuehrten Eintragsliste in einem
//     einzigen Durchlauf (ansichtAus), nicht aus n Einzelbilanzen, die
//     hinterher zusammengezaehlt werden muessten.
//  2. Die Kappung (nur Logbuch traegt eine Regel.Grenze) greift ueber die
//     GESAMTE Menge aller Projekte, nicht je Projekt einzeln — ein Logbuch mit
//     50 Zeilen aus zwei Projekten kappt genauso wie eines mit 50 Zeilen aus
//     einem.
//
// REIHENFOLGE der Projekte: alphabetisch, wie DiscoverProjectsIn sie bereits
// liefert (localeLess, sidebar.go) — und NICHT nach Dringlichkeit quer ueber
// Projekte neu gemischt. Begruendung: ansichtAus haengt Karte an Karte an
// (flachAnhaengen); sortiert wird JE KARTE (sammle/sortiereGruppen), nicht
// global — das gilt bereits INNERHALB eines Projekts mit mehreren Epics (siehe
// den Kommentar an kappen, ansicht.go: die Rangfolge springt an jeder
// Kartengrenze zurueck, "1 1 1 1 1 6 6 … 13 38 38 38 39 39 … 13 13 13"). Eine
// projektuebergreifende Ansicht erbt dieses Verhalten, statt es zu verstecken
// — und weil die Karten EINES Projekts dadurch bloc­kweise beieinanderstehen,
// zerreisst eine kuenftige Gruppierung nach Projekt (gkp-s9n) keinen Block.
// Eine globale Neusortierung quer ueber alle Projekte haette exakt das
// Gegenteil bewirkt: sie haette Karten eines Projekts auseinandergerissen, nur
// um sie fuer eine Gruppierung, die es heute noch nicht gibt, wieder
// zusammensuchen zu muessen.
//
// FEHLSCHLAG EINES PROJEKTS kippt das Aggregat nicht (fail-open, wie
// StandNachMit, teilbaum.go:142): liefert CollectBeads Available=false oder
// einen leeren Namen, traegt dieses eine Projekt schlicht keine Karten bei —
// die uebrigen n-1 Projekte bleiben vollstaendig. Der Grund reist in
// []ProjektFehler mit.
func ansichtenUeberProjekte(projekte []collectors.ProjectRef, jetzt time.Time) (map[string]AufgabenAnsicht, []ProjektFehler) {
	var alleKarten []QuellKarten
	var fehler []ProjektFehler
	qs := sammellaufQuellen()
	start := time.Now()
	for _, p := range projekte {
		// Gesamtfrist um die Schleife (Review-Befund 3, gkp-92i): jeder
		// Subprozess ist einzeln begrenzt, aber ohne eine Grenze UM diese
		// Schleife wartet ein kalter Aufrufer (GetAnsichtenAggregat, avCurrent
		// == nil) im Extremfall ueber 10 Minuten (9 Projekte * rund 70s worst
		// case, shared.go BdTimeoutMs 20s / GitTimeoutMs 10s). Ein
		// unvollstaendiges Aggregat ist die bessere Antwort als eines, das
		// minutenlang ausbleibt — die uebersprungenen Projekte tragen ihren
		// eigenen Grund in []ProjektFehler, GENAU wie ein Projekt ohne
		// Aufgabenbaum (fail-open, s.u.).
		if time.Since(start) > sammellaufBudget {
			fehler = append(fehler, ProjektFehler{
				Projekt: collectors.ReportsRepoName(p.Path),
				Grund:   "Zeitbudget des Sammellaufs erschöpft — Projekt nicht mehr geprüft",
			})
			continue
		}
		karten := kartenAusQuellen(p.Path, qs)
		for _, qk := range karten {
			if qk.Grund == "" {
				continue
			}
			// NICHT p.Name. Der ist der Verzeichnisname aus
			// DiscoverProjectsIn; die geglueckten Zeilen tragen dagegen
			// beads.Name, also filepath.Base des HAUPT-Worktrees
			// (collectors.repoIdentity). Bei einem verknuepften Worktree, dessen
			// Verzeichnis anders heisst als sein Haupt-Worktree, faellt beides
			// auseinander: dasselbe Projekt erschiene einmal als Fehler unter
			// dem einen und einmal als Zeile unter dem anderen Namen. Da
			// gkp-s9n nach genau diesem Feld gruppiert, waeren das zwei Gruppen
			// fuer ein Projekt.
			//
			// ReportsRepoName loest ueber dieselbe Git-Herkunft auf wie
			// repoIdentity (deriveGit) und braucht dafuer kein .beads/ — es ist
			// die einzige Quelle, die auch dann noch antwortet, wenn
			// CollectBeads gerade gescheitert ist. Gefunden im Review zu
			// gkp-i90, Befund 1.
			fehler = append(fehler, ProjektFehler{Projekt: collectors.ReportsRepoName(p.Path), Grund: qk.Grund})
		}
		projektStempeln(karten, p.Path)
		alleKarten = append(alleKarten, karten...)
	}
	return AnsichtenBauenAus(alleKarten, jetzt), fehler
}

// sammellaufQuellen ist die Quellen-Tabelle ohne GitHub (aufgaben-quelle,
// #9) und ohne openspec (#10): das Aggregat blieb schon vorher beads-only,
// siehe quellenOhne. openspec holt die Issues ueber dasselbe Netz wie github
// und faellt deshalb aus demselben Grund weg (gkp-isj).
func sammellaufQuellen() []Quelle {
	return quellenOhne(HerkunftGithub, HerkunftOpenspec)
}

// AnsichtenAggregatAntwort ist der RUMPF der Route GET /ansichten OHNE
// project-Parameter (gkp-92i) — das Gegenstueck zu AnsichtenAntwort
// (githubaufgaben.go) fuer den projektuebergreifenden Pfad.
//
// EIGENE Funktion statt eines dritten Parameters an AnsichtenAntwort: die
// beiden Antworten haben verschiedene Blindstellen — github_leer sagt "GitHub
// fehlt komplett", projekt_fehler sagt "DIESE Projekte fehlen einzeln" — und
// AnsichtenAntwort wird von vier Testdateien mit genau zwei Argumenten
// aufgerufen. Ein dritter Parameter, der bei jedem Ein-Projekt-Aufruf leer
// bliebe, waere eine Signatur, die eine Frage traegt, die dort nie zutrifft.
//
// projekt_fehler macht sichtbar, was ansichtenUeberProjekte bereits berechnet,
// aber bislang niemand liest (Review-Befund 4, gkp-92i): ein gescheitertes
// Projekt fiel lautlos aus der Liste, und das Ergebnis sah aus wie
// Vollstaendigkeit — genau die Fehlerklasse, gegen die Regel.Hinweis bei
// fehlendem GitHub schon steht ("eine leere Sektion waere die schlechtere
// Antwort"). Der Kopf der Ansicht (gkp-s9n/gkp-dnz, Zeichnen ist nicht Teil
// dieses Beads) kann das Feld erst zeigen, wenn es hier ankommt.
func AnsichtenAggregatAntwort(ansichten map[string]AufgabenAnsicht, fehler []ProjektFehler) map[string]any {
	out := map[string]any{
		"ansichten": ansichten,
		// Begruendung fuer den dyn-Umweg: siehe AnsichtenAntwort.
		"nav": ansichtenNav(map[string]any{"ansichten": dyn(ansichten)}),
	}
	if len(fehler) > 0 {
		out["projekt_fehler"] = fehler
	}
	return out
}

// kartenEinesProjekts ist die beads-Haelfte von StandNachMit (teilbaum.go),
// fuer EIN Projekt: CollectBeads, der Report-Store dazu, baueKarten — dieselben
// drei Aufrufe, in derselben Reihenfolge, weil ein zweiter, leicht
// abweichender Nachbau genau die Fehlerklasse waere, vor der der Kommentarkopf
// von teilbaum.go warnt (gkp-1lp/gkp-b9n). Nicht als gemeinsame Funktion nach
// teilbaum.go gezogen, um dessen Schreib-Antwortpfad in dieser Arbeitsstrecke
// nicht anzufassen — beide Stellen rufen baueKarten (reports_view.go, selbes
// Paket) als den einen echten Erbauer.
func kartenEinesProjekts(pfad string) (maps []ReportsMap, grund string, ok bool) {
	beads := collectors.CollectBeads(pfad)
	if !beads.Available || beads.Name == "" {
		grund = beads.Error
		if grund == "" {
			grund = "kein Aufgabenbaum für dieses Verzeichnis"
		}
		return nil, grund, false
	}
	mapsEntries, _, err := reports.ListMaps(beads.Name)
	if err != nil {
		// Fail-open wie StandNachMit: ein kaputter Report-Store kostet die
		// Belege dieses Projekts, nicht seinen Aufgabenbaum.
		mapsEntries = map[string][]reports.Entry{}
	}
	return baueKarten(beads, mapsEntries), "", true
}

// beadsQuelle ist der beads-Adapter der Quellen-Tabelle (aufgaben-quelle, #9):
// die einzige schreibbare Quelle, mit Terminen und Belegen.
type beadsQuelle struct{}

func (beadsQuelle) Herkunft() string { return HerkunftBeads }

func (beadsQuelle) Faehigkeiten() Faehigkeiten {
	return Faehigkeiten{Schreibbar: true, Termine: true, Belege: true}
}

// Karten ist kartenEinesProjekts; der Grund eines fehlenden Aufgabenbaums wird
// zum Fehler der Quelle.
func (beadsQuelle) Karten(pfad string) ([]ReportsMap, error) {
	maps, grund, ok := kartenEinesProjekts(pfad)
	if !ok {
		return nil, errors.New(grund)
	}
	return maps, nil
}
