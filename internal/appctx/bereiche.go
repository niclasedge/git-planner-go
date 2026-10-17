// Die Bereichs-Ebene der Ansicht (gkp-8ww, Spec §4.3 / E2, §5).
//
// Sie ist der SONDERFALL dieser Karte. Alles andere hier zeigt etwas, das
// anderswo entsteht: bd haelt die Beads, GitHub die Issues, git die Repos. Die
// Area ist die einzige Ebene, die es nur gibt, weil jemand sie festlegt —
// deshalb war sie eine ausdrueckliche Entscheidung (E2: eigene Datei,
// Repo → Area) und kein abgeleitetes Feld.
//
// Was hier steht, ist die GRUPPIERUNG und sonst nichts: aus der Liste der
// entdeckten Projekte und dem Inhalt von areas.json wird die Folge
// Area → Projekt. Nach CLAUDE.md gehoert genau das nach Go — eine Reihenfolge,
// die erst im Browser entsteht, ist eine Reihenfolge, die niemand testen kann
// (Spec §5.0).
package appctx

import (
	"sort"
	"strings"

	"github.com/niclasedge/git-planner-go/internal/areas"
)

// BereichProjekt ist ein Projekt in der Bereichsansicht: Name und Pfad, also
// genau collectors.ProjectRef.
//
// Beide Felder reisen mit, weil die Ansicht beide braucht und sie VERSCHIEDENE
// Schluessel sind: areas.json ist auf den NAMEN geschluesselt (Spec §4.3), die
// Aufgaben-Schreibrouten verlangen den PFAD (taskroutes.go). Den einen aus dem
// anderen abzuleiten waere genau die Aufloesung, gegen die taskops' pfadPruefen
// gebaut ist.
type BereichProjekt struct {
	Name string `json:"name"`
	Pfad string `json:"pfad"`
}

// BereichGruppe ist eine Area mit den Projekten, die ihr zugeordnet sind.
type BereichGruppe struct {
	Bereich string `json:"bereich"`
	// Ohne markiert die Sammel-Area "Ohne Bereich". Sie entsteht beim Lesen und
	// steht nie in der Datei (areas.OhneBereich), laesst sich also weder
	// umbenennen noch als Ziel einer Zuweisung benutzen. Ein Flag statt eines
	// Namensvergleichs im Browser: der Vergleich waere eine zweite Stelle, an
	// der die Zeichenkette stehen muesste.
	Ohne     bool             `json:"ohne,omitempty"`
	Projekte []BereichProjekt `json:"projekte"`
}

// BereicheGruppieren ordnet die entdeckten Projekte unter ihre Area.
//
// Die REIHENFOLGE der Areas ist die der Datei und nicht alphabetisch. Das ist
// eine Entscheidung, keine Bequemlichkeit: areas.json wird von Hand gepflegt
// (Spec §4.3), ihre `areas`-Liste traegt damit eine Reihenfolge, die jemand
// aufgeschrieben hat. Sie beim Anzeigen umzusortieren hiesse, genau diese
// Information wegzuwerfen — und die manuelle Ordnung ist als O1 ohnehin ein
// eigener Punkt.
//
// Drei Faelle, die nicht offensichtlich sind und alle am selben Ziel haengen,
// dass die Ansicht nichts verschweigt:
//
//  1. Eine Area, die nur in `zuordnung` vorkommt und in `areas` fehlt, ist
//     moeglich — die Datei wird von Hand editiert. Sie bekommt trotzdem eine
//     Gruppe. Ohne das verschwaende ein Projekt aus der Ansicht, obwohl es
//     zugeordnet IST: der stille Verlust, vor dem der Kommentarkopf von
//     areas.loadLocked warnt, nur eine Ebene hoeher.
//  2. Eine Area OHNE Projekte bleibt stehen. Sie ist das Ziel der naechsten
//     Zuweisung; verschwaende sie, waere "Area anlegen" eine Operation ohne
//     sichtbare Wirkung.
//  3. "Ohne Bereich" erscheint nur, wenn etwas darin liegt, dann aber immer —
//     sichtbar, nicht versteckt, damit die Pflege auffaellt. Sie steht ZULETZT:
//     eine Sammelgruppe, die bei jedem neu entdeckten Projekt nach oben
//     springt, schoebe die gepflegten Areas nach unten. Sichtbar ist sie
//     dadurch, dass es sie gibt und sie zaehlt, nicht dadurch, dass sie draengt.
func BereicheGruppieren(projekte []BereichProjekt, st areas.State) []BereichGruppe {
	// Erst einsammeln, wer wohin gehoert — EINMAL ueber die Projekte, damit die
	// Zuordnung nicht je Area erneut befragt wird.
	drin := map[string][]BereichProjekt{}
	for _, p := range projekte {
		if strings.TrimSpace(p.Name) == "" {
			continue // ein Projekt ohne Namen hat keinen Schluessel in areas.json
		}
		a := st.AreaOf(p.Name)
		drin[a] = append(drin[a], p)
	}

	// Die Reihenfolge der Areas: erst die deklarierten in Dateiordnung, dann
	// die, die es nur ueber eine Zuordnung gibt (nach Namen, damit die Folge
	// ueberhaupt bestimmt ist — eine Go-Karte iteriert absichtlich gemischt).
	gesehen := map[string]bool{}
	var folge []string
	for _, a := range st.Areas {
		a = strings.TrimSpace(a)
		if a == "" || a == areas.OhneBereich || gesehen[a] {
			continue
		}
		gesehen[a] = true
		folge = append(folge, a)
	}
	var nachgetragen []string
	for a := range drin {
		if a == areas.OhneBereich || gesehen[a] {
			continue
		}
		gesehen[a] = true
		nachgetragen = append(nachgetragen, a)
	}
	sortiereNamen(nachgetragen)
	folge = append(folge, nachgetragen...)

	out := make([]BereichGruppe, 0, len(folge)+1)
	for _, a := range folge {
		ps := drin[a]
		sortiereProjekte(ps)
		out = append(out, BereichGruppe{Bereich: a, Projekte: orLeer(ps)})
	}
	if ohne := drin[areas.OhneBereich]; len(ohne) > 0 {
		sortiereProjekte(ohne)
		out = append(out, BereichGruppe{Bereich: areas.OhneBereich, Ohne: true, Projekte: ohne})
	}
	return out
}

// orLeer macht aus einem nil-Slice ein leeres. In JSON ist das der Unterschied
// zwischen `null` und `[]`, und der Browser laeuft ueber die Liste.
func orLeer(ps []BereichProjekt) []BereichProjekt {
	if ps == nil {
		return []BereichProjekt{}
	}
	return ps
}

// sortiereProjekte ordnet die Projekte einer Gruppe nach Namen — ohne
// Ruecksicht auf Gross- und Kleinschreibung, mit dem Byte-Vergleich als
// Stichentscheid.
//
// Nicht der blosse Byte-Vergleich, und das ist an echten Daten zu sehen: unter
// ~/Work stehen `infra` und `git-planner` nebeneinander, und 'I'
// (0x49) liegt vor 'g' (0x67). Eine Liste, in der alle grossgeschriebenen
// Namen vorn stehen, liest sich wie zwei Listen. Der Stichentscheid bleibt
// trotzdem noetig: ohne ihn waeren zwei Namen, die sich nur in der
// Schreibweise unterscheiden, gleichwertig — und die Reihenfolge damit die der
// Eingabe, also die des Dateisystems.
func sortiereProjekte(ps []BereichProjekt) {
	sort.SliceStable(ps, func(i, j int) bool {
		a, b := strings.ToLower(ps[i].Name), strings.ToLower(ps[j].Name)
		if a != b {
			return a < b
		}
		return ps[i].Name < ps[j].Name
	})
}

func sortiereNamen(ns []string) {
	sort.SliceStable(ns, func(i, j int) bool {
		a, b := strings.ToLower(ns[i]), strings.ToLower(ns[j])
		if a != b {
			return a < b
		}
		return ns[i] < ns[j]
	})
}

// BereichsNamen sind die Areas, denen sich ein Projekt zuordnen laesst — in
// derselben Reihenfolge, in der die Gruppen stehen.
//
// Abgeleitet aus den GRUPPEN und nicht noch einmal aus dem Zustand: sonst
// gaebe es zwei Stellen, an denen entschieden wird, welche Area es gibt, und
// die Auswahlliste koennte einen Namen anbieten, den die Gruppierung nicht
// kennt (oder umgekehrt).
func BereichsNamen(gruppen []BereichGruppe) []string {
	out := make([]string, 0, len(gruppen))
	for _, g := range gruppen {
		if g.Ohne {
			continue // die Sammel-Area ist kein Ziel, sie ist das Fehlen eines Ziels
		}
		out = append(out, g.Bereich)
	}
	return out
}

// BereichProjekteAus liest die Projektliste aus dem dynamischen Kontext —
// derselbe Block, den die Seite als DATA.projects bekommt.
//
// Aus DEM Block und nicht aus einer zweiten Entdeckung: was die
// Bereichsansicht anbietet, muss dasselbe sein, was die Projektleiste zeigt.
// Zwei Aufrufe von DiscoverProjects koennten sich unterscheiden (ein Repo
// kommt dazu, eine Wurzel ist kurz nicht lesbar), und dann boete die eine
// Ansicht ein Projekt an, das die andere nicht kennt.
func BereichProjekteAus(roh []any) []BereichProjekt {
	out := make([]BereichProjekt, 0, len(roh))
	for _, r := range roh {
		m := aMap(r)
		name := strings.TrimSpace(aStr(m["name"]))
		if name == "" {
			continue
		}
		out = append(out, BereichProjekt{Name: name, Pfad: aStr(m["path"])})
	}
	return out
}

// bereicheNav ist der Navigationseintrag der Bereichsansicht.
//
// Die Zahl ist die der Projekte OHNE Bereich, nicht die der Projekte oder der
// Areas — sie steht also genau dann da, wenn etwas zu tun ist, und
// verschwindet, wenn alles zugeordnet ist. Eine Gesamtzahl waere eine
// Beschreibung; das hier ist die einzige Zahl, die dieser Eintrag als
// AUFFORDERUNG tragen kann (Spec §4.3: sichtbar, nicht versteckt, damit die
// Pflege auffaellt).
//
// Der Punkt wird gelb und nicht rot: rot ist in dieser Navigation fuer
// ueberfaellige ARBEIT reserviert (ansichtenNav). Ein Projekt ohne Bereich ist
// eine offene Pflege, keine versaeumte Frist — die beiden gleich zu faerben
// naehme dem Rot seine Bedeutung.
func bereicheNav(block map[string]any) map[string]any {
	ohne := 0
	for _, g := range bereicheAus(block) {
		if g.Ohne {
			ohne = len(g.Projekte)
		}
	}
	item := map[string]any{"id": BereicheID, "label": "Bereiche", "dot": "c"}
	if ohne > 0 {
		item["dot"] = "a"
		item["cnt"] = itoa(ohne)
	}
	return item
}

// bereicheAus holt die Gruppen aus dem Block zurueck. Der Block ist eine
// map[string]any, weil er so in DATA landet; der Typ darin bleibt aber der
// echte, also kostet das keinen Umweg ueber JSON.
func bereicheAus(block map[string]any) []BereichGruppe {
	gs, _ := block["gruppen"].([]BereichGruppe)
	return gs
}

// BereicheID ist der Name der Ansicht fuer Navigation, URL und Browser.
const BereicheID = "bereiche"

// BereicheBlock ist der Datenblock der Bereichsansicht.
//
// Der Zustand wird HIER gelesen und nicht in BuildContext, und das ist der
// Punkt, an dem die Frische haengt: BuildContext liegt hinter
// cache.GetProjectContext (TTL 15 min), BuildData laeuft bei jedem Seitenaufbau
// neu. Eine Area-Aenderung betrifft ausserdem alle Projekte auf einmal — in
// einem Cache, der PRO PROJEKT geschluesselt ist, muesste sie jeden Schluessel
// einzeln entwerten. Genau das hat der Kommentar an der Bereich-Route (gkp-t1o)
// offengelassen: "gehoert an den Speicher, der sie dann haelt". Er haelt sie
// nicht — deshalb gibt es nichts zu entwerten.
//
// Ein Lesefehler wird GEMELDET, nicht zu einem leeren Zustand gemacht: eine
// kaputte areas.json saehe sonst aus wie "noch nichts zugeordnet", und die
// Ansicht boete an, genau darauf zu schreiben.
//
// Der Store hier ist ein EIGENER, nicht der der Schreibroute — ihre Mutexe
// haben also nichts miteinander zu tun. Das ist zulaessig, weil dieser Weg nur
// LIEST: areas.Store.Save schreibt in eine temporaere Datei und macht daraus
// per os.Rename einen atomaren Tausch. Ein Leser sieht deshalb immer den alten
// oder den neuen Stand, nie einen halben. Ein gemeinsamer Mutex wuerde die
// Seitenerzeugung an die Schreibvorgaenge binden, ohne etwas zu gewinnen, das
// der Rename nicht schon garantiert.
func BereicheBlock(projekte []BereichProjekt) map[string]any {
	store := areas.Default()
	st, err := store.Load()
	gruppen := BereicheGruppieren(projekte, st)
	out := map[string]any{
		"gruppen":   gruppen,
		"bereiche":  BereichsNamen(gruppen),
		"ohne_name": areas.OhneBereich,
		"pfad":      store.Path(),
	}
	if err != nil {
		out["fehler"] = err.Error()
	}
	return out
}
