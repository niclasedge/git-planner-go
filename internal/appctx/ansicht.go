// Der Rahmen der Aufgaben-Ansichten (Spec §5).
//
// Things kennt Today, Upcoming, Anytime, Someday, Inbox und Logbook. Das sind
// SECHS Listen ueber denselben Aufgabenbaum, die sich in genau drei Dingen
// unterscheiden: welche Aufgabe hineingehoert, in welchen Eimer sie faellt und
// wonach innerhalb eines Eimers sortiert wird. Alles andere — den Baum
// absteigen, Termine deuten, Unteraufgaben unter ihre Aufgabe einruecken,
// zaehlen, flach machen — ist bei allen sechs dasselbe.
//
// Deshalb steht hier der Rahmen und in je einer eigenen Datei die Regel. Eine
// weitere Ansicht ist ein neuer Eintrag in der Tabelle (regelAnmelden in einem
// init), kein Eingriff in eine bestehende Zeile. Das ist nicht Vorratsbau: die
// drei Folgetickets gkp-1oj (fuenf Ansichten), gkp-pf2 (GitHub-Herkunft) und
// gkp-8ww (Areas) haengen alle an gkp-7gb und laufen danach GLEICHZEITIG durch
// dieselben Dateien. Ein wachsendes switch waere die Zeile, an der sie sich
// gegenseitig ueberschreiben.
//
// Was hier bewusst NICHT steht: das Zeichnen. Die Ansicht liefert Auswahl,
// Reihenfolge, Tiefe und Zaehlung — browser.js filtert und zeichnet, es rechnet
// nicht (CLAUDE.md). "Heute" sieht aus wie eine Browserfrage, ist aber keine:
// haengt die Auswahl am Browser, ist sie nicht ohne Browser pruefbar.
package appctx

import (
	"sort"
	"time"

	"github.com/niclasedge/git-planner-go/internal/bdops"
)

// HerkunftBeads ist die Quelle einer Aufgabe. Zwei Aufgabenarten stehen nach E1
// nebeneinander ohne Kopplung (Spec §4.2); das Abzeichen gehoert deshalb an
// JEDE Zeile und nicht nur an die zweite Sorte, sonst heisst "kein Abzeichen"
// je nach Liste etwas anderes. Die GitHub-Seite ist seit gkp-pf2 da
// (githubaufgaben.go).
const (
	HerkunftBeads  = "beads"
	HerkunftGithub = "github"
)

// AufgabenAnsicht ist eine fertige Liste: was drinsteht, in welcher Reihenfolge
// und wie tief eingerueckt.
//
// Eintraege nennen die Aufgabe mit ihrer ID und NICHT als eingebettete
// ReportsBead. Der Grund ist gemessen an dieser Antwort: die Aufgaben stehen
// bereits vollstaendig in ReportsView.Maps, und ein zweites Mal mitgeschickt
// waeren es dieselben Titel, Belege und Eintragstexte noch einmal. Die Ansicht
// liefert die AUSWAHL, den Zeileninhalt holt die Ansicht sich aus dem Baum, den
// sie ohnehin hat. Dass rpTeilbaumEinsetzen nach einem Schreibvorgang genau
// diesen Baum auffrischt, faellt dabei als Nebenwirkung an: die Zeilen der
// Heute-Liste sind danach aktuell, ohne dass hier etwas nachgezogen wird.
type AufgabenAnsicht struct {
	ID    string `json:"id"`
	Titel string `json:"titel"`
	// Hinweis ist die Einschraenkung, die die Liste selbst nicht zeigen kann —
	// etwa dass GitHub-Aufgaben hier gar nicht vorkommen KOENNEN. Er steht in
	// der Ansicht, weil eine Luecke, die niemand nennt, wie Vollstaendigkeit
	// aussieht (Spec §4.4: "muss in der Ansicht sichtbar sein, nicht
	// stillschweigend").
	Hinweis string `json:"hinweis,omitempty"`
	// Stand ist der Zeitpunkt, gegen den ausgewaehlt wurde, Tag sein
	// Kalendertag. Beide stehen im Rumpf, damit die Ansicht sagen kann, WELCHES
	// "heute" sie zeigt: der Projektkontext haengt hinter einem TTL-Cache
	// (cache.TTLMs, 15 min), eine ueber Mitternacht offen stehende Seite zeigt
	// also bis zu einer Viertelstunde lang den Vortag.
	Stand string `json:"stand"`
	Tag   string `json:"tag"`
	// Zaehler ist die Bilanz nach Grund — "überfällig" 3, "startklar" 7. Keine
	// festen Felder, weil jede Ansicht andere Gruende kennt und feste Felder
	// die Tabelle wieder zu einem gemeinsamen Typ zusammenzoegen, den jede neue
	// Ansicht anfassen muesste.
	//
	// LISTE und nicht Karte, weil die REIHENFOLGE Inhalt ist: dringend zuerst.
	// Als Karte muesste der Browser sie sortieren, und genau das ging schief —
	// die JS-Vorgabesortierung vergleicht Code-Einheiten, "überfällig" faengt
	// mit U+00FC an und landet damit hinter jedem ASCII-Wort. Der dringendste
	// Eimer stand ganz rechts. Hier steht er vorn, weil die Eintraege bereits
	// nach Rang sortiert sind und die Gruende in der Reihenfolge ihres ersten
	// Auftretens gezaehlt werden.
	Zaehler   []Zaehlstand     `json:"zaehler"`
	Eintraege []AnsichtEintrag `json:"eintraege"`
	// Leer ist der Satz, den diese Liste zeigt, wenn sie leer ist — die Kopie
	// von Regel.Leer. Er reist im Rumpf mit und steht nicht im Browser, weil
	// "leer" in jeder Ansicht etwas anderes heisst (Begruendung an Regel.Leer).
	Leer string `json:"leer,omitempty"`
	// Gekappt ist der Satz, der sagt, dass die Liste an Regel.Grenze endet —
	// leer, solange sie vollstaendig ist (gkp-dop).
	//
	// Er steht hier und nicht im Browser, weil er ZAHLEN traegt, die nur der
	// Rahmen kennt: wie viele Zeilen es insgesamt gab und wie viele Gruppen
	// uebrig sind. Der Browser zeichnet ihn ans ENDE der Liste, also genau an
	// die Stelle, an der ohne ihn nicht zu unterscheiden waere, ob die Liste
	// gekappt ist oder zu Ende — dasselbe Argument, aus dem Leer daneben steht.
	Gekappt string `json:"gekappt,omitempty"`
}

// Zaehlstand ist ein Eimer der Bilanz.
type Zaehlstand struct {
	Grund  string `json:"grund"`
	Anzahl int    `json:"anzahl"`
}

// AnsichtEintrag ist eine Zeile.
type AnsichtEintrag struct {
	Bead     string `json:"bead"`
	Karte    string `json:"karte"`
	Herkunft string `json:"herkunft"`
	// Projekt ist der Repo-Name, aus dessen Baum diese Zeile stammt — bei
	// HerkunftGithub derselbe lokale Name, ausdruecklich NICHT die nwo (die
	// steht schon in Karte). Er haengt an der ZEILE und nicht nur an der
	// Antwort (ReportsView.RepoName), weil eine kommende projektuebergreifende
	// Sammelliste mehrere Antworten mischt: ohne dieses Feld koennte weder die
	// Gruppierung nach Projekt entscheiden, wohin eine Zeile gehoert, noch
	// POST /action/task/schliessen wissen, gegen welche bd-Datenbank es
	// schreiben soll.
	Projekt string `json:"projekt"`
	// Tiefe ist die Einrueckung IN DIESER LISTE, nicht die Tiefe im Baum. Ein
	// Enkel, dessen Elternteil KEINEN eigenen Grund hat, haengt trotzdem unter
	// diesem Elternteil — der steht dann als Zwischenueberschrift da
	// (AnsichtEintrag.Ueberschrift, sammle) und traegt seine Nachfahren eine
	// Ebene tiefer. Erst wenn auch das nicht hilft, weil KEIN Nachfahre etwas
	// beitraegt, faellt der ganze Ast weg — dann gibt es auch keine Tiefe, ueber
	// die man etwas behaupten koennte. Die Tiefe zeigt also immer auf eine
	// Zeile, die wirklich dasteht (gewaehlt oder Ueberschrift), nie auf einen
	// Ast, den die Liste verschweigt — dieselbe Regel, die rpBaumAuswahl im
	// Browser fuer den Filter anwendet.
	Tiefe int `json:"tiefe"`
	// Rang ist der Eimer innerhalb der Liste (bei Heute: 0 ueberfaellig,
	// 1 heute faellig, 2 der Rest). Die Ansicht darf danach gruppieren; sie
	// bestimmt ihn nicht.
	Rang int `json:"rang"`
	// Gruende sagt, WARUM die Zeile hier steht. Ohne das ist eine Liste aus
	// drei vereinigten Quellen nicht lesbar: einer Aufgabe ist nicht anzusehen,
	// ob sie ueberfaellig ist oder bloss startklar.
	Gruende []string `json:"gruende"`
	// Titel, Status und URL reisen NUR mit, wenn der Baum die Zeile nicht
	// liefern kann — also bei jeder Herkunft ausser beads (gkp-pf2).
	//
	// Das ist kein Widerspruch zum Kommentarkopf von AufgabenAnsicht, sondern
	// seine Bedingung, beim Wort genommen: dort steht "die Aufgaben stehen
	// bereits vollstaendig in ReportsView.Maps", und genau das gilt fuer
	// GitHub-Aufgaben NICHT. Sie kommen nach E1 aus einer zweiten Quelle, die
	// bewusst nicht in den beads-Baum gemischt wird (sonst zaehlten Fortschritt,
	// Historie und Belegstand sie mit). Der Browser schlaegt die Zeile im Baum
	// nach und faende sie nicht — die Zeile hiesse dann "nicht im geladenen
	// Baum". Doppelt gesendet wird trotzdem nichts: bei HerkunftBeads bleiben
	// die drei Felder leer.
	//
	// URL statt einer Nummer, weil die Nummer ohne das Repo nichts oeffnet und
	// das Repo schon in Karte steht — den Link daraus im Browser
	// zusammenzusetzen waere genau die Rechnung, die dort nicht hingehoert.
	Titel  string `json:"titel,omitempty"`
	Status string `json:"status,omitempty"`
	URL    string `json:"url,omitempty"`
	// Frist und Wann sind die GEDEUTETEN Termine als Ortszeit-Text — bewusst
	// kein RFC3339 mit Z. Der rohe Wert von bd traegt ein Z, das luegt
	// (Spec §4.4); ihn an den Browser zu schicken hiesse, dort eine zweite
	// Gelegenheit zu schaffen, ihn falsch zu lesen. Was hier steht, ist bereits
	// die Antwort.
	//
	// Bei HerkunftGithub sind beide IMMER leer: GitHub hat kein Gegenstueck zu
	// When/Deadline (Spec §4.4). Das ist die Einschraenkung von E1, und sie
	// steht als Hinweis im Kopf jeder Ansicht, die daran scheitert.
	Frist string `json:"frist,omitempty"`
	Wann  string `json:"wann,omitempty"`
	// FristTage/WannTage sind KALENDERtage bis zum Termin, negativ wenn er
	// vorbei ist. Zeiger, weil 0 ("heute") und "kein Termin" sonst dasselbe
	// waeren.
	FristTage *int `json:"frist_tage,omitempty"`
	WannTage  *int `json:"wann_tage,omitempty"`
	// Ueberschrift markiert eine ZWISCHENUEBERSCHRIFT: ein Elternteil OHNE
	// eigenen Aufnahmegrund (r.Waehlt sagt nein), das trotzdem in der Liste
	// steht, weil mindestens einer seiner Nachfahren gewaehlt ist (Spec §5,
	// gemessen: ueber 8 Repos sind 131 von 250 startklaren Aufgaben Kinder,
	// ein Filter auf "nur Wurzeln" machte mehr als die Haelfte unsichtbar).
	//
	// Das Feld reist bis zum Browser, weil er es NICHT raten darf — er filtert
	// und zeichnet, er waehlt nicht aus (CLAUDE.md). Zwei Folgen haengen daran,
	// und beide sind Sache des Browsers, nicht dieses Pakets: gedaempfte
	// Darstellung und keine Aktionsknoepfe. Die EINE Folge, die hier schon
	// gilt, ist die Kopfbilanz: ansichtAus zaehlt eine Zwischenueberschrift
	// nicht mit, sonst stuenden dort mehr Aufgaben, als ein Mensch als Arbeit
	// sieht — eine Ueberschrift ist Struktur, keine Aufgabe.
	Ueberschrift bool `json:"ueberschrift,omitempty"`
	// FristHerkunft nennt die Bead-Id, aus der Rang, Gruende und die
	// Frist/Wann-Anzeige dieser Zeile geerbt sind — leer, wenn es ihre eigenen
	// sind. Ohne den Vermerk waere eine geerbte Zahl eine ERFUNDENE Angabe: sie
	// steht nicht an DIESEM Bead (siehe die Begruendung an zwischenUeberschrift).
	//
	// Geerbt wird NUR von einer Zwischenueberschrift (Ueberschrift == true).
	// Eine normale Aufgabenzeile — eine mit eigenem Grund — behaelt IMMER ihre
	// eigene Frist, auch wenn ein Kind dringender ist: eine geliehene
	// Dringlichkeit in einer echten Aufgabenzeile zaehlte in der Kopfbilanz
	// doppelt — einmal an dem Kind, das sie wirklich hat, einmal an der Zeile,
	// die sie sich nur geliehen haette. Eine Zwischenueberschrift hat dieses
	// Problem nicht: sie traegt keinen eigenen Grund und zaehlt ohnehin nicht
	// in der Bilanz (Ueberschrift-Kommentar oben).
	FristHerkunft string `json:"frist_herkunft,omitempty"`
	// BelegPlan/BelegDone sind die Zaehlstaende der Report-Eintraege DIESES
	// Beads nach Kind (gkp-dnz, Spec: "Stufe 2 — Belege"). EAGER mitgeliefert,
	// die Eintraege selbst NICHT: reports.ListMaps hat sie fuer den Baum, den
	// diese Ansicht ohnehin schon durchlaeuft, bereits gelesen (baueKarten /
	// ReportsBead.Entries) — ein Zaehlstand kostet dort nur eine Schleife ueber
	// eine bereits im Speicher stehende Liste, keinen zweiten Dateizugriff.
	// Die Eintraege selbst (inklusive Beleg-Text, bis zu 256 KiB je proof.out)
	// bleiben aussen vor und werden nur bei Bedarf ueber GET /belege geholt
	// (gkp-e9h) — ein Zaehler ist billig, ein Bild-Volltext quer ueber 250
	// Aufgaben ist es nicht.
	//
	// Zwei ints statt einer Struktur, damit der Browser sie ohne Umweg lesen
	// kann; omitempty haelt eine Zeile ohne jeden Eintrag (gemessen: 239 von
	// 250) frei von zwei Nullen.
	BelegPlan int `json:"beleg_plan,omitempty"`
	BelegDone int `json:"beleg_done,omitempty"`
}

// Kandidat ist eine Aufgabe, deren Termine bereits gedeutet sind. Die Deutung
// passiert EINMAL, hier, vor jeder Regel — nicht in jeder Regel noch einmal.
// Genau das war die Fehlerklasse aus Spec §4.4: due_at/defer_until sind lokale
// Wanduhr mit einem Z, created_at/updated_at/closed_at echtes UTC, und die
// Kompensation gilt pro Feld. Eine zweite Regel, die selbst parst, ist die
// zweite Gelegenheit, die beiden zu vertauschen.
type Kandidat struct {
	Bead     ReportsBead
	Karte    string
	Herkunft string
	// Faehigkeiten sind die der Quelle, aus der die Aufgabe kommt
	// (aufgaben-quelle, #9). Anfassbar und eintragAus lesen sie, statt den
	// Namen der Herkunft zu vergleichen.
	Faehigkeiten Faehigkeiten
	// Projekt reist aus der Karte mit, die den Baum liefert (ReportsMap.Projekt),
	// damit eintragAus es unveraendert an die Zeile weitergeben kann —
	// Begruendung an AnsichtEintrag.Projekt.
	Projekt string
	// URL ist der Ort der Aufgabe ausserhalb dieser Maschine — bei GitHub die
	// Issue-URL, bei beads leer (ein Bead hat keine). Sie kommt aus
	// ReportsBead.URL und geht in AnsichtEintrag.URL weiter.
	URL string
	// Heute ist Mitternacht des Tages, gegen den ausgewaehlt wird, in der Zone
	// des uebergebenen Zeitpunkts. Jetzt ist der Zeitpunkt selbst.
	Heute time.Time
	Jetzt time.Time
	// Frist ist due_at (Things' Deadline), Wann ist defer_until (Things' When)
	// — beide nil, wenn bd nichts stehen hat.
	Frist     *time.Time
	Wann      *time.Time
	FristTage *int
	WannTage  *int
	// Abschluss ist closed_at, und es steht hier aus demselben Grund wie die
	// beiden darueber: damit keine Regel es selbst deutet. Die Zeitbasis ist
	// aber die ANDERE — echtes UTC ueber bdops.ParseEventStamp, nicht die
	// Wanduhr mit dem luegenden Z (Spec §4.4). Genau diese Trennung meint der
	// Kommentarkopf: die Gelegenheit, die beiden zu vertauschen, gibt es nur
	// einmal, wenn die Deutung nur einmal stattfindet.
	//
	// nil heisst "nicht geschlossen ODER ohne Stempel" — beides ist "kein
	// Abschlusszeitpunkt", und die Nullzeit waere davon nicht zu unterscheiden.
	Abschluss *time.Time
}

// Offen ist die Frage, die jede Regel zuerst stellt: eine geschlossene Aufgabe
// steht in keiner Arbeitsliste. Hier und nicht in jeder Regel, damit der
// Endzustand an EINER Stelle benannt ist (statusGeschlossen, reports_view.go).
func (k Kandidat) Offen() bool { return k.Bead.Status != statusGeschlossen }

// Anfassbar ist die quellenuebergreifende Fassung von "kann jetzt begonnen
// werden" (gkp-pf2).
//
// Bei beads ist das `bd ready`: bds eigenes Urteil darueber, ob ein Blocker im
// Weg steht. GitHub kennt keinen Blockerbegriff — es gibt dort nichts, was ein
// offenes Issue am Begonnenwerden hindern koennte —, also faellt die Frage fuer
// eine GitHub-Aufgabe mit "ist sie offen" zusammen. Ein erfundenes Ready am
// Bead selbst waere die falsche Stelle: `Ready` heisst "bd sagt ja", und bd hat
// zu einem GitHub-Issue nichts gesagt.
//
// DIESE Methode gehoert in "Jederzeit" und ausdruecklich NICHT in "Heute".
// Heute fragt weiter `k.Bead.Ready`, und das ist kein Versehen, sondern
// Spec §4.4: eine GitHub-Aufgabe traegt kein When und keine Deadline, kann
// also keinen Auswahlgrund von Heute erfuellen und erscheint NUR in Jederzeit.
// Wer die beiden Stellen "vereinheitlicht", hebt diese Einschraenkung
// stillschweigend auf — TestGithubAufgabeStehtNichtInHeute faengt das.
//
// Welche der beiden Fassungen gilt, entscheidet seit #9 die Faehigkeit
// Schreibbar der Quelle und nicht ihr Name: eine schreibbare Quelle urteilt
// selbst ueber Blocker (Ready), eine nur lesende kennt keine.
func (k Kandidat) Anfassbar() bool {
	if k.Faehigkeiten.Schreibbar {
		return k.Bead.Ready
	}
	return k.Offen()
}

// Urteil ist die Antwort einer Regel auf eine Aufgabe.
type Urteil struct {
	Rang    int
	Gruende []string
	// Zeitpunkt sortiert INNERHALB eines Rangs. nil sortiert hinter jeden
	// gesetzten Wert — eine Aufgabe ohne Termin steht hinter denen mit.
	Zeitpunkt *time.Time
}

// Regel ist eine Ansicht.
type Regel struct {
	ID      string
	Titel   string
	Hinweis string
	// Ordnung ist die Stelle in der Navigation, klein zuerst.
	//
	// Sie steht hier, weil sie vorher am DATEINAMEN hing: die Tabelle fuellte
	// sich in der Reihenfolge der init-Funktionen, und die ist die der Dateien
	// im Paket. Gemessen ergab das demnaechst, eingang, heute, irgendwann,
	// jederzeit, logbuch — Heute an dritter Stelle, und die Leserfolge war nur
	// erreichbar, indem man heute.go umbenennt. Eine Anzeigeentscheidung an
	// einem Dateinamen ist keine Entscheidung, sondern ein Zufall, der sich
	// beim naechsten Umbenennen aendert.
	//
	// Eine ZAHL IN DER REGEL und kein switch in der Navigation: sonst naehme
	// ansichtenNav wieder einzelne Ansichtsnamen in den Mund, und die Naht, die
	// TestSelectKenntKeinenEinzelnenAnsichtsnamen haelt, waere auf der Go-Seite
	// aufgetrennt. In Zehnerschritten, damit eine neue Ansicht dazwischenpasst,
	// ohne dass sechs bestehende Zeilen neu durchnummeriert werden.
	Ordnung int
	// Absteigend dreht das Vorzeichen des Terminvergleichs in sortiereGruppen:
	// spaetester Zeitpunkt zuerst. Fuer eine Rueckschau (Logbuch) ist das die
	// natuerliche Richtung, fuer jede Arbeitsliste die falsche — was zuerst
	// faellig ist, steht dort oben.
	//
	// Was NICHT mitgedreht wird, ist die Stelle eines fehlenden Termins: ohne
	// Termin sortiert in beiden Richtungen hinter jedem gesetzten. Das ist eine
	// Aussage ueber Vollstaendigkeit und keine ueber Richtung — ein naives
	// `return !kleiner` haette die Terminlosen nach vorn geholt, wo sie wie das
	// Neueste aussaehen.
	Absteigend bool
	// Grenze ist die Obergrenze in ZEILEN; 0 heisst "keine" (gkp-dop).
	//
	// Gekappt wird an einer GRUPPENgrenze, nicht nach der n-ten Zeile: Grenze
	// ist ein Budget, kein Schnitt. Das Logbuch ist seit gkp-3oh tageweise
	// gruppiert (ein Rang-Eimer je Abschlusstag), und ein Tag, von dem drei von
	// elf Zeilen zu sehen sind, behauptet, an diesem Tag seien drei Aufgaben
	// fertig geworden. Ein Tag, der GANZ fehlt, behauptet nichts — und der Satz
	// darunter sagt, dass es ihn gibt.
	//
	// Der MECHANISMUS steht deshalb im Rahmen, der WERT in genau einer Regel.
	// Eine Zeile `if r.ID == LogbuchID` in ansichtAus waere eine gemeinsame
	// Zeile, die eine einzelne Ansicht beim Namen nennt — dieselbe Naht, die
	// TestSelectKenntKeinenEinzelnenAnsichtsnamen auf der Browserseite haelt.
	// Dass nur das Logbuch eine Grenze hat, ist trotzdem kein Zufall: die fuenf
	// Arbeitslisten sind durch die Menge OFFENER Arbeit begrenzt, und die leert
	// sich von selbst. Aus dem Logbuch geht nie etwas wieder heraus.
	Grenze int
	// GrenzeSatz macht aus den Zahlen des Rahmens den Satz der Ansicht. Die
	// Zahlen kennt nur der Rahmen, die WORTE nur die Ansicht: "Abschlusstag"
	// ist die Vokabel des Logbuchs, und ein Rang bedeutet in jeder Ansicht
	// etwas anderes (in Heute: ueberfaellig / faellig / Rest). Ein fertiger
	// Satz in ansicht.go waere in fuenf von sechs Listen falsch — genau der
	// Fehler, den Regel.Leer schon einmal korrigiert hat.
	//
	// gruppen ist die Zahl der Raenge, die ueberlebt haben.
	GrenzeSatz func(gezeigt, gesamt, gruppen int) string
	// Leer ist der Satz, den die leere Liste zeigt.
	//
	// Er gehoert zur Regel und nicht in den Browser, aus demselben Grund wie
	// Hinweis: "leer" heisst in jeder Ansicht etwas anderes. Der Browser sagte
	// generisch "Nichts offen" — im Logbuch ist das die falsche Aussage, dort
	// ist nichts GESCHLOSSEN. Ein Satz, der in fuenf von sechs Listen zufaellig
	// stimmt, ist in der sechsten eine Falschauskunft, und gerade eine leere
	// Liste hat nichts ausser diesem Satz, woran man sie pruefen koennte.
	Leer string
	// Waehlt entscheidet ueber genau eine Aufgabe. false heisst "nicht in
	// dieser Liste" — ihre Unteraufgaben werden trotzdem geprueft und stehen
	// dann eine Ebene weiter oben (siehe AnsichtEintrag.Tiefe).
	Waehlt func(Kandidat) (Urteil, bool)
}

// regeln ist die Tabelle. Ein Slice und keine Karte, weil die Navigation die
// Ansichten in einer Reihenfolge zeigt und Go die Iteration ueber eine Karte
// absichtlich mischt.
//
// Die Reihenfolge ist die der Regel.Ordnung und NICHT mehr die der
// init-Funktionen: die war die der Dateinamen und stellte Heute an die dritte
// Stelle (gkp-3oh, Begruendung an Regel.Ordnung).
var regeln []Regel

// regelAnmelden traegt eine Ansicht ein. Aufgerufen aus dem init der Datei, die
// die Regel enthaelt.
//
// Sortiert wird HIER und nicht erst in Regeln(): sonst gaebe es zwei
// Reihenfolgen im Paket — die der Tabelle und die der Navigation —, und jede
// kuenftige Schleife ueber regeln muesste wissen, welche von beiden sie gerade
// hat. Sechs Eintraege einmal beim Start zu sortieren kostet nichts.
//
// SliceStable, damit zwei Regeln mit derselben Ordnung wenigstens
// deterministisch bleiben. Dass das ein Rueckfall in die Dateinamenfolge waere,
// faengt TestJedeRegelHatEineEigeneOrdnung.
func regelAnmelden(r Regel) {
	regeln = append(regeln, r)
	sort.SliceStable(regeln, func(i, j int) bool { return regeln[i].Ordnung < regeln[j].Ordnung })
}

// Regeln sind die angemeldeten Ansichten. Eine Kopie, damit ein Aufrufer (die
// Navigation in builddata.go) die Tabelle nicht umsortieren kann.
func Regeln() []Regel {
	out := make([]Regel, len(regeln))
	copy(out, regeln)
	return out
}

// AnsichtBauen baut eine Ansicht. ok=false heisst: diese Ansicht gibt es nicht
// — eine Auskunft, kein Fehler, und der Unterschied zu einer leeren Liste ist
// genau der, den ein Tippfehler im Ansichtsnamen sonst verschluckt.
func AnsichtBauen(id string, maps []ReportsMap, jetzt time.Time) (AufgabenAnsicht, bool) {
	return AnsichtBauenMit(id, maps, nil, jetzt)
}

// AnsichtBauenMit ist AnsichtBauen ueber BEIDE Quellen (gkp-pf2): maps sind die
// beads-Karten, github die Karten aus GithubKarten.
//
// Zwei Funktionen statt einer geaenderten Signatur, und das ist Absicht: die
// GitHub-Haelfte kostet einen Netzaufruf und steht deshalb nicht auf dem Pfad,
// der ein Projekt oeffnet (github.go:41, gkp-isj). Jeder Aufrufer ohne GitHub
// in der Hand ruft weiter AnsichtBauen — er sagt damit "beads only", statt ein
// nil zu uebergeben, dessen Bedeutung man raten muss.
func AnsichtBauenMit(id string, maps, github []ReportsMap, jetzt time.Time) (AufgabenAnsicht, bool) {
	for _, r := range regeln {
		if r.ID == id {
			return ansichtAus(r, quellKartenMit(maps, github), jetzt), true
		}
	}
	return AufgabenAnsicht{}, false
}

// AnsichtenBauen baut JEDE angemeldete Ansicht ueber demselben Baum.
//
// Ein Aufruf statt einer je Ansicht, und zwar weil das Teuerste — der Baum —
// bereits gebaut ist: was hier dazukommt, ist ein Durchlauf ueber die Beads je
// Regel, ohne bd-Aufruf und ohne Datei. Der Gegenentwurf, jede Ansicht erst
// beim Ansehen zu bauen, braeuchte eine Route je Ansicht und einen zweiten
// Baumaufbau — Sekunden statt Mikrosekunden.
func AnsichtenBauen(maps []ReportsMap, jetzt time.Time) map[string]AufgabenAnsicht {
	return AnsichtenBauenMit(maps, nil, jetzt)
}

// AnsichtenBauenMit ist AnsichtenBauen ueber beide Quellen (gkp-pf2).
func AnsichtenBauenMit(maps, github []ReportsMap, jetzt time.Time) map[string]AufgabenAnsicht {
	return AnsichtenBauenAus(quellKartenMit(maps, github), jetzt)
}

// ansichtenNav baut die Navigationseintraege der Aufgaben-Ansichten aus der
// Regeltabelle — nicht aus einer zweiten Liste in builddata.go. Eine neue
// Ansicht steht damit in der Navigation, sobald sie angemeldet ist; eine
// vergessene Zeile dort kann es nicht geben.
//
// Die Zahl wird ABGELESEN (Laenge der Eintraege im schon gebauten Rumpf) und
// nicht nachgerechnet — die Auswahl gehoert nach ansichtAus, hier steht sie nur
// dran. Rot wird der Punkt, sobald ueberfaellige Arbeit darin liegt: das ist
// die einzige Zahl in einer Aufgabenliste, die eine BEANSTANDUNG ist, und nicht
// bloss eine Beschreibung.
func ansichtenNav(reportsc map[string]any) []any {
	alle := aMap(reportsc["ansichten"])
	items := make([]any, 0, len(regeln))
	for _, r := range Regeln() {
		a := aMap(alle[r.ID])
		anzahl := len(aArr(a["eintraege"]))
		punkt := "c"
		if anzahl == 0 {
			punkt = ""
		}
		for _, z := range aArr(a["zaehler"]) {
			e := aMap(z)
			if aStr(e["grund"]) == GrundUeberfaellig && aBool(e["anzahl"]) {
				punkt = "r"
			}
		}
		items = append(items, map[string]any{
			"id": r.ID, "label": r.Titel, "dot": punkt, "cnt": itoa(anzahl),
		})
	}
	return items
}

// ansichtAus baut eine Ansicht ueber beide Quellen. Die Reihenfolge der
// Quellen ist KEINE Rangfolge: sortiert wird je Karte (sortiereGruppen), und
// beide Quellen durchlaufen dieselbe Regel, dieselbe Deutung und dieselbe
// Sortierung. Nach E1 stehen sie nebeneinander, nicht uebereinander.
//
// Seit #9 laeuft sie ueber eine Liste von Quellen statt ueber zwei feste
// Parameter; die Reihenfolge der Liste ist die der Karten.
func ansichtAus(r Regel, karten []QuellKarten, jetzt time.Time) AufgabenAnsicht {
	heute := tagesAnfang(jetzt)
	a := AufgabenAnsicht{
		ID: r.ID, Titel: r.Titel, Hinweis: r.Hinweis, Leer: r.Leer,
		Stand:     jetzt.Format(time.RFC3339),
		Tag:       heute.Format("2006-01-02"),
		Zaehler:   []Zaehlstand{},
		Eintraege: []AnsichtEintrag{},
	}
	for _, qk := range karten {
		// Eine Karte, die bd nicht mehr kennt (orphanedMaps), hat keine Beads
		// und faellt hier von selbst weg — ihre Eintraege sind Historie, keine
		// Arbeit.
		for _, m := range qk.Maps {
			for _, g := range sammle(r, m.Beads, m.ID, qk.Herkunft, qk.Faehigkeiten, m.Projekt, 0, heute, jetzt) {
				a.Eintraege = flachAnhaengen(a.Eintraege, g)
			}
		}
	}
	// Die Kappung steht VOR der Bilanz, und das ist keine Reihenfolge aus
	// Bequemlichkeit: die Bilanz im Kopf beschreibt die Liste, die darunter
	// steht. Zaehlte sie ueber die ungekappte Menge, sagte der Kopf "69 ohne
	// Beleg" ueber eine Liste mit sechs solchen Zeilen — zwei Zahlen ueber
	// denselben Gegenstand, die auseinanderlaufen, und genau das war der Defekt
	// aus gkp-d52. Was jenseits der Grenze liegt, sagt der Satz, nicht die Zahl.
	gesamt := len(a.Eintraege)
	if gekappt, gruppen, greift := kappen(a.Eintraege, r.Grenze); greift {
		a.Eintraege = gekappt
		if r.GrenzeSatz != nil {
			a.Gekappt = r.GrenzeSatz(len(gekappt), gesamt, gruppen)
		}
	}
	// Erstes Auftreten bestimmt die Stelle. Die Eintraege sind zu diesem
	// Zeitpunkt schon sortiert (sortiereGruppen), der dringendste Grund kommt
	// deshalb zuerst vor und steht damit vorn in der Bilanz.
	//
	// Eine Zwischenueberschrift zaehlt NICHT mit (Begruendung an
	// AnsichtEintrag.Ueberschrift): sie ist Struktur, keine Aufgabe, und ihre
	// Gruende sind ohnehin nur geliehen (zwischenUeberschrift) — sie doppelt
	// zu zaehlen zaehlte dieselbe Dringlichkeit zweimal.
	stelle := map[string]int{}
	for _, e := range a.Eintraege {
		if e.Ueberschrift {
			continue
		}
		for _, g := range e.Gruende {
			if i, da := stelle[g]; da {
				a.Zaehler[i].Anzahl++
				continue
			}
			stelle[g] = len(a.Zaehler)
			a.Zaehler = append(a.Zaehler, Zaehlstand{Grund: g, Anzahl: 1})
		}
	}
	return a
}

// kappen kuerzt eine fertig sortierte Zeilenliste auf hoechstens grenze Zeilen
// — an einer GRUPPENgrenze (Begruendung an Regel.Grenze). greift=false heisst
// "nichts weggenommen", und nur dann darf die Ansicht schweigen.
//
// Gefiltert statt abgeschnitten, und das ist der Punkt: die Liste ist NICHT
// durchgehend chronologisch. ansichtAus haengt Karte an Karte, und sortiert
// wird je Karte (sortiereGruppen) — gemessen an infra am 2026-09-14 lief
// die Rangfolge 1 1 1 1 1 6 6 … 13 38 38 38 39 39 … 13 13 13 …, sprang also
// mit jeder neuen Karte zurueck. Ein `eintraege[:grenze]` nimmt darin nicht die
// juengsten Zeilen, sondern die der ersten Karten und wirft die juengsten der
// letzten weg. Ein Filter ueber die Raenge braucht keine Gesamtordnung: er
// entscheidet je Zeile und laesst die Reihenfolge, wie sie war.
//
// Die Auswahl der Raenge ist global ueber alle Karten — sie ist die eine
// Stelle, an der diese Ansicht doch einen Gesamtblick braucht.
func kappen(es []AnsichtEintrag, grenze int) ([]AnsichtEintrag, int, bool) {
	if grenze <= 0 || len(es) <= grenze {
		return es, 0, false
	}
	proRang := map[int]int{}
	raenge := make([]int, 0, 8)
	for _, e := range es {
		if _, da := proRang[e.Rang]; !da {
			raenge = append(raenge, e.Rang)
		}
		proRang[e.Rang]++
	}
	// Kleiner Rang = juengere Gruppe; sortiereGruppen ordnet Raenge ueberall
	// aufsteigend (Urteil.Rang ist eine Eimernummer, keine Zeit).
	sort.Ints(raenge)
	behalten := make(map[int]bool, len(raenge))
	summe := 0
	for i, r := range raenge {
		// Der erste Eimer geht IMMER ganz durch. Ist schon er groesser als das
		// Budget, waere die Alternative eine leere Liste — und die sagt "nichts
		// abgeschlossen", wo an einem einzigen Tag mehr fertig wurde, als die
		// Grenze erlaubt. Eine Grenze ist ein Budget, keine Zusage.
		if i > 0 && summe+proRang[r] > grenze {
			break
		}
		behalten[r] = true
		summe += proRang[r]
	}

	out := make([]AnsichtEintrag, 0, summe)
	// Die Tiefe wird neu gefuehrt: AnsichtEintrag.Tiefe ist die Einrueckung IN
	// DIESER LISTE, und eine Zeile, deren Elternteil weggekappt ist, haengt
	// sonst unter nichts. sammle haelt dieselbe Zusage fuer eine nicht
	// GEWAEHLTE Aufgabe; die Kappung ist der zweite Weg, auf dem eine Zeile
	// herausfaellt, und muss sie ebenso halten.
	//
	// Der Stapel traegt die URSPRUENGLICHEN Tiefen der behaltenen Vorfahren.
	// Eine weggekappte Zeile kommt nicht darauf — damit findet ihr Kind von
	// selbst den naechsten behaltenen Vorfahren und dessen Ebene.
	stapel := make([]int, 0, 8)
	for _, e := range es {
		for len(stapel) > 0 && stapel[len(stapel)-1] >= e.Tiefe {
			stapel = stapel[:len(stapel)-1]
		}
		if !behalten[e.Rang] {
			continue
		}
		alt := e.Tiefe
		e.Tiefe = len(stapel)
		out = append(out, e)
		stapel = append(stapel, alt)
	}
	return out, len(behalten), true
}

// gruppe ist eine gewaehlte Aufgabe mit den gewaehlten Aufgaben unter ihr. Der
// Ast bleibt beim Sortieren zusammen: sortiert werden GRUPPEN, nicht Zeilen.
// Sortierte man die flache Liste, risse eine Unteraufgabe mit frueherem Termin
// ihre Aufgabe auseinander, und die Einrueckung zeigte auf die falsche Zeile.
type gruppe struct {
	eintrag AnsichtEintrag
	kand    Kandidat
	urteil  Urteil
	unten   []gruppe
	// ast ist der SORTIER-Schluessel dieser Gruppe: der dringlichste Rang (und,
	// aus DERSELBEN Quelle, der dazugehoerige Zeitpunkt) im gesamten Ast —
	// Elternteil und alle gewaehlten Nachfahren, nicht bloss der Elternteil
	// selbst (gkp-a1d, Reviewer-Befund: "eltern-in-arbeit" mit ueberfaelligem
	// Kind stand komplett hinter einer bloss heute faelligen Standalone-Zeile,
	// weil flachAnhaengen die ganze Familie an die Stelle des Elternteils
	// haengt, sortiereGruppen aber nur den Elternteil ansah).
	//
	// ast ist ABSICHTLICH ein eigenes Feld und nicht urteil selbst: urteil
	// bleibt die ANZEIGE (eintragAus, zwischenUeberschrift) und haelt die
	// Nicht-Vererbungs-Regel unangetastet — eine Aufgabenzeile MIT eigenem
	// Grund zeigt IMMER ihre eigene Frist, auch wenn sie wegen eines Kindes
	// weiter vorn steht. Sortierung und Anzeige sind zwei verschiedene Fragen
	// (Aufgabentext), ast beantwortet nur die erste.
	ast Urteil
}

// sammle laeuft EINEN Aufgabenbaum ab. herkunft reist mit, statt aus dem Bead
// abgeleitet zu werden: die Quelle ist eine Eigenschaft des BAUMS, nicht der
// einzelnen Zeile — ein Feld je Bead waere dieselbe Angabe n-mal, und n-mal die
// Gelegenheit, sie falsch zu setzen.
//
// ZWISCHENUEBERSCHRIFT (Spec §5, gemessen: 131 von 250 startklaren Aufgaben
// ueber 8 Repos sind Kinder). Vor diesem Ticket ruecken die Nachfahren einer
// nicht gewaehlten Aufgabe auf DIESELBE Ebene auf — ihr Elternteil verschwindet
// spurlos, auch wenn er der einzige Hinweis ist, WESSEN Kinder das sind. Jetzt
// bekommt jedes Kind IMMER die naechsttiefere Ebene (tiefe+1), unabhaengig
// davon, ob der Elternteil selbst gewaehlt ist: ist er es nicht, aber
// mindestens ein Nachfahre schon, steht er trotzdem da — gedaempft, ohne
// Aktionsknoepfe (beides Sache des Browsers, AnsichtEintrag.Ueberschrift) und
// nicht in der Kopfbilanz gezaehlt (ansichtAus). Traegt KEIN Nachfahre etwas
// bei, faellt der Elternteil weiterhin ganz weg — eine Ueberschrift ohne
// Kinder waere Struktur ohne Inhalt.
//
// RANDFALL, entschieden: eine normale Aufgabenzeile — eine MIT eigenem Grund —
// erbt NIE von ihren Kindern, auch wenn eines dringender ist. Sie bleibt
// Aufgabenzeile mit ihrer EIGENEN Frist, nicht der ihres Kindes. Zwei Gruende:
// erstens hat sie bereits einen wahren, eigenen Grund, hier zu stehen — ihn
// durch den eines fremden Beads zu ersetzen waere keine Vererbung, sondern ein
// Tausch. Zweitens zaehlte eine geliehene Dringlichkeit in der Kopfbilanz
// DOPPELT: einmal am Kind, das sie tatsaechlich hat, einmal an der Zeile, die
// sie sich nur geliehen haette — und Zwischenueberschriften sind von der
// Bilanz nur deshalb ausgenommen, weil sie NICHTS Eigenes beitragen. Eine
// Aufgabenzeile mit eigenem Grund tut das sehr wohl. Vererbt wird deshalb
// ausschliesslich in den Zweig OHNE eigenen Grund (zwischenUeberschrift
// unten) — Regel 3 der Spec ("die Elternzeile zeigt die dringendste Frist")
// gilt damit fuer Zwischenueberschriften woertlich und für normale
// Aufgabenzeilen gar nicht: eine Aufgabenzeile mit eigener, dringenderer ODER
// weniger dringender Frist behaelt in jedem Fall ihre eigene Angabe.
func sammle(r Regel, beads []ReportsBead, karte, herkunft string, faeh Faehigkeiten, projekt string, tiefe int, heute, jetzt time.Time) []gruppe {
	var out []gruppe
	for _, b := range beads {
		k := kandidatAus(b, karte, herkunft, projekt, heute, jetzt)
		k.Faehigkeiten = faeh
		u, ok := r.Waehlt(k)
		unten := sammle(r, b.Children, karte, herkunft, faeh, projekt, tiefe+1, heute, jetzt)
		if !ok && len(unten) == 0 {
			// Weder ein eigener Grund noch ein Ast darunter: unsichtbar, bis
			// hinunter. Das ist der Randfall aus Spec §5 ("Ein Elternteil, dessen
			// Kinder alle wegfallen") in seiner einfachsten Form — er tritt hier
			// ein, BEVOR es je eine Zeile gab, und braucht deshalb keinen
			// Sonderfall in kappen(): eine Ueberschrift entsteht unten erst gar
			// nicht ohne mindestens ein Kind.
			continue
		}
		e := eintragAus(k, u, tiefe)
		if !ok {
			e.Ueberschrift = true
			// zwischenUeberschrift ersetzt u vollstaendig durch das geerbte
			// Urteil — ohne eigenen Grund gibt es nichts, womit es kollidieren
			// koennte.
			u = zwischenUeberschrift(&e, unten)
		}
		out = append(out, gruppe{eintrag: e, kand: k, urteil: u, unten: unten, ast: astUrteilVon(u, ok, unten, r.Absteigend)})
	}
	sortiereGruppen(out, r.Absteigend)
	return out
}

// astUrteilVon ist der SORTIER-Schluessel einer Gruppe (gruppe.ast) — der
// dringlichste Rang im ganzen Ast, samt seinem eigenen Zeitpunkt (nicht dem
// einer anderen Quelle, sonst sortierten zwei Stufen desselben Schluessels aus
// zwei verschiedenen Groessen, Spec §9.1 gkp-9dp).
//
// Zwei Faelle brauchen KEINE Rechnung, aus verschiedenen Gruenden:
//
//   - Zwischenueberschrift (ok=false): u ist bereits das geerbte Urteil
//     (zwischenUeberschrift hat es aus unten[0] uebernommen). Es hier noch
//     einmal gegen unten zu vergleichen waere die Vererbung ein zweites Mal —
//     genau die Falle, vor der die Aufgabe warnt.
//   - keine Nachfahren: u ist der einzige Kandidat.
//
// Nur eine normale Aufgabenzeile MIT eigenem Grund und mindestens einem
// Nachfahren vergleicht: unten ist zu diesem Zeitpunkt schon nach ast
// sortiert (sortiereGruppen laeuft am Ende des rekursiven sammle-Aufrufs, der
// unten gebaut hat), unten[0] traegt also bereits den dringlichsten Wert im
// GESAMTEN Ast darunter. Ist er dringlicher als der eigene, dringt er nach
// oben durch — die ANZEIGE (u selbst, in eintrag) bleibt davon unberuehrt, das
// ist die Nicht-Vererbungs-Regel (Kommentar an sammle).
func astUrteilVon(u Urteil, ok bool, unten []gruppe, absteigend bool) Urteil {
	if !ok || len(unten) == 0 {
		return u
	}
	if kleiner, entschieden := rangZeitVergleich(unten[0].ast, u, absteigend); entschieden && kleiner {
		return unten[0].ast
	}
	return u
}

// zwischenUeberschrift baut Rang, Gruende und die Frist/Wann-Anzeige eines
// Elternteils OHNE eigenen Aufnahmegrund aus dem dringendsten Ast darunter
// (Spec §5, Randfall: "Warum der Herkunftsvermerk Pflicht ist — die Zahl
// steht nicht am Bead"). unten ist an dieser Stelle NICHT leer (der Aufrufer
// hat das schon geprueft) und bereits sortiert: sortiereGruppen laeuft am Ende
// jedes rekursiven sammle-Aufrufs, unten[0] ist darin also die dringendste
// Gruppe UND traegt bereits das Ergebnis DERSELBEN Vererbung aus ihrer eigenen
// Tiefe — die Rekursion ist bottom-up, ein zweiter Abstieg durch den ganzen
// Ast waere dieselbe Rechnung ein zweites Mal.
//
// Geerbt wird EN BLOC (Rang, Gruende, Frist, Wann, Herkunft zusammen), nie ein
// einzelnes Feld fuer sich: eine Ueberschrift hat keinen eigenen Grund, mit dem
// ein geerbtes Feld kollidieren koennte, aber ein Rang aus einer Quelle neben
// Gruenden aus einer anderen waere in sich widerspruechlich.
//
// FristHerkunft zeigt auf den WIRKLICHEN Ursprung, nicht bloss das direkte
// Kind: haengt unten[0] seinerseits schon eine geerbte Frist (weil es selbst
// eine Zwischenueberschrift ist), reist dessen FristHerkunft unveraendert
// weiter — sonst zeigte der Vermerk auf eine Zwischenstation statt auf den
// Bead, an dem die Frist wirklich steht.
func zwischenUeberschrift(e *AnsichtEintrag, unten []gruppe) Urteil {
	kind := unten[0]
	quelle := kind.eintrag.Bead
	if kind.eintrag.FristHerkunft != "" {
		quelle = kind.eintrag.FristHerkunft
	}
	e.Rang = kind.urteil.Rang
	e.Gruende = kind.urteil.Gruende
	e.Frist = kind.eintrag.Frist
	e.FristTage = kind.eintrag.FristTage
	e.Wann = kind.eintrag.Wann
	e.WannTage = kind.eintrag.WannTage
	e.FristHerkunft = quelle
	return kind.urteil
}

// sortiereGruppen ist die EINE Sortierregel aller Ansichten. Eine Regel liefert
// Rang und Zeitpunkt; der Rest des Schluessels steht hier, damit keine Ansicht
// ihn vergessen kann.
//
// Vollstaendig bis zur Id, und das ist der Punkt: bricht der Schluessel vorher
// ab, entscheidet die Reihenfolge, in der bd die Beads geliefert hat — die ist
// weder chronologisch noch zwischen zwei Aufrufen stabil. Ein Test gegen so
// eine Folge kann nicht fehlschlagen, weil er nichts festhaelt.
// absteigend (Regel.Absteigend) dreht GENAU EINE Stufe dieses Schluessels, den
// Terminvergleich. Der Rang bleibt aufsteigend — er ist eine Eimernummer und
// keine Zeit —, LastChange bleibt neu→alt, die Id bleibt der stabile Abschluss.
// Eine Ansicht, die alles umdreht, bekaeme sonst ihre Eimer verkehrt herum.
func sortiereGruppen(gs []gruppe, absteigend bool) {
	sort.SliceStable(gs, func(i, j int) bool {
		a, b := gs[i], gs[j]
		// Sortiert wird nach ast, nicht nach urteil: ast ist der dringlichste
		// Rang im GANZEN Ast (gruppe.ast, astUrteilVon), urteil bleibt die
		// Anzeige dieser einen Zeile. Fuer die meisten Gruppen sind beide
		// gleich (kein Nachfahre dringlicher als der eigene Grund, oder gar
		// keine Nachfahren) — der Unterschied greift genau in dem Fall, den
		// gkp-a1d gefunden hat.
		if kleiner, entschieden := rangZeitVergleich(a.ast, b.ast, absteigend); entschieden {
			return kleiner
		}
		if a.kand.Bead.Priority != b.kand.Bead.Priority {
			// P0 ist die hoechste Prioritaet und die kleinste Zahl.
			return a.kand.Bead.Priority < b.kand.Bead.Priority
		}
		if a.kand.Bead.LastChange != b.kand.Bead.LastChange {
			// Neueste Aenderung oben, wie ueberall sonst in dieser Ansicht.
			// Leeres LastChange faellt nach hinten, weil "" unter jedem Stempel
			// liegt.
			return a.kand.Bead.LastChange > b.kand.Bead.LastChange
		}
		return a.kand.Bead.ID < b.kand.Bead.ID
	})
}

// rangZeitVergleich sind die ERSTEN ZWEI Stufen des Sortierschluessels — Rang,
// dann (aus demselben Urteil) der Zeitpunkt. Gemeinsam genutzt von
// sortiereGruppen (dort mit dem Bead-Tiebreak dahinter) und astUrteilVon (das
// nur zwischen zwei Quellen waehlt und keinen Tiebreak braucht).
// entschieden=false heisst "gleich", nicht "b gewinnt" — ein Aufrufer, der das
// verwechselt, haette einen stillen Vorrang statt eines echten Gleichstands.
func rangZeitVergleich(a, b Urteil, absteigend bool) (kleiner, entschieden bool) {
	if a.Rang != b.Rang {
		return a.Rang < b.Rang, true
	}
	if v, e := vergleicheZeit(a.Zeitpunkt, b.Zeitpunkt, absteigend); e {
		return v, true
	}
	return false, false
}

// vergleicheZeit ordnet zwei Termine; entschieden=false heisst "beide gleich
// oder beide ohne Termin". Ein fehlender Termin sortiert hinter jeden
// vorhandenen.
//
// absteigend betrifft NUR den letzten Zweig, den Vergleich zweier gesetzter
// Termine. Die drei nil-Zweige stehen bewusst davor und bleiben, wie sie sind:
// "ohne Termin nach hinten" ist keine Richtung, sondern die Aussage, dass eine
// Zeile ohne Angabe keine Behauptung ueber ihre Stelle macht. Wer statt dessen
// das Ergebnis des ganzen Aufrufs negiert, dreht das mit — und holt in einer
// Rueckschau ausgerechnet die undatierten Zeilen an die Spitze, wo sie wie das
// Neueste aussehen (TestAbsteigendDrehtNurDieTermineNichtDieFehlenden).
func vergleicheZeit(a, b *time.Time, absteigend bool) (kleiner, entschieden bool) {
	switch {
	case a == nil && b == nil:
		return false, false
	case a == nil:
		return false, true
	case b == nil:
		return true, true
	case a.Equal(*b):
		return false, false
	case absteigend:
		return a.After(*b), true
	default:
		return a.Before(*b), true
	}
}

func flachAnhaengen(out []AnsichtEintrag, g gruppe) []AnsichtEintrag {
	out = append(out, g.eintrag)
	for _, u := range g.unten {
		out = flachAnhaengen(out, u)
	}
	return out
}

// kandidatAus deutet die Datumsfelder — jedes auf seiner eigenen Zeitbasis.
//
// bdops.ParseUserDateIn statt ParseUserDate, weil die Zone hier aus dem
// uebergebenen Zeitpunkt kommt und nicht aus der Maschine: nur so laesst sich
// die Auswahl mit einem Versatz ungleich null pruefen. Unter TZ=UTC ist die
// Kompensation unsichtbar, ein Test dort haelt also nichts fest.
// Die Deutung laeuft fuer BEIDE Herkuenfte durch dieselbe Zeile. Bei GitHub
// sind DueAt und DeferUntil leer, ParseUserDateIn sagt ok=false, und Frist/Wann
// bleiben nil — dieselbe Antwort, die ein Bead ohne Termin bekommt. Ein
// Sonderweg "wenn github, dann ueberspringen" waere eine zweite Stelle, an der
// die Regel "kein Termin heisst nil" stehen und irgendwann abweichen koennte.
// closed_at laeuft ausdruecklich NICHT durch terminAus: es ist echtes UTC und
// geht durch bdops.ParseEventStamp. Die beiden Deutungen stehen deshalb hier
// nebeneinander in drei Zeilen — wer sie verwechselt, sieht es an dieser Stelle
// und nicht verstreut in sechs Regeldateien (gkp-3oh, Befund C).
func kandidatAus(b ReportsBead, karte, herkunft, projekt string, heute, jetzt time.Time) Kandidat {
	k := Kandidat{Bead: b, Karte: karte, Herkunft: herkunft, Projekt: projekt, URL: b.URL, Heute: heute, Jetzt: jetzt}
	k.Frist, k.FristTage = terminAus(b.DueAt, heute)
	k.Wann, k.WannTage = terminAus(b.DeferUntil, heute)
	if t, ok := bdops.ParseEventStamp(b.ClosedAt); ok {
		k.Abschluss = &t
	}
	return k
}

func terminAus(roh string, heute time.Time) (*time.Time, *int) {
	t, ok := bdops.ParseUserDateIn(roh, heute.Location())
	if !ok {
		return nil, nil
	}
	d := kalendertage(heute, t)
	return &t, &d
}

func eintragAus(k Kandidat, u Urteil, tiefe int) AnsichtEintrag {
	belegPlan, belegDone := belegZaehlstand(k.Bead.Entries)
	e := AnsichtEintrag{
		Bead: k.Bead.ID, Karte: k.Karte, Herkunft: k.Herkunft, Projekt: k.Projekt,
		Tiefe: tiefe, Rang: u.Rang, Gruende: u.Gruende,
		Frist: terminText(k.Frist), Wann: terminText(k.Wann),
		FristTage: k.FristTage, WannTage: k.WannTage,
		BelegPlan: belegPlan, BelegDone: belegDone,
	}
	// Der Zeileninhalt reist nur mit, wenn der Baum des Browsers ihn nicht hat.
	// Die Bedingung haengt an der Faehigkeit Schreibbar der Quelle (#9) und
	// nicht an einem "steht die Zeile im Baum"-Flag, weil beides heute
	// dasselbe ist: nach E1 kommt alles ausser der schreibbaren Quelle (beads)
	// aus einer Quelle, die bewusst nicht in ReportsView.Maps gemischt wird.
	if !k.Faehigkeiten.Schreibbar {
		e.Titel = k.Bead.Title
		e.Status = k.Bead.Status
		e.URL = k.URL
	}
	return e
}

// belegZaehlstand zaehlt die Report-Eintraege EINES Beads nach Kind — roh,
// ohne Supersede-Aufloesung. Der Punkt der Zahl ist "gibt es hier ueberhaupt
// etwas zu oeffnen", nicht "was gilt noch"; das beantwortet erst der Inhalt,
// den GET /belege liefert, wenn der Twisty tatsaechlich aufgeht (Spec-Notiz zu
// gkp-dnz).
func belegZaehlstand(entries []ReportsEntry) (plan, done int) {
	for _, e := range entries {
		switch e.Kind {
		case "plan":
			plan++
		case "done":
			done++
		}
	}
	return plan, done
}

// terminText ist die Ortszeit-Schreibweise eines Termins. Mitternacht wird zum
// blossen Datum: `bd update --due 2026-01-15` speichert 00:00, und ein " 00:00"
// an jedem zweiten Termin waere Rauschen, das nichts unterscheidet.
func terminText(t *time.Time) string {
	if t == nil {
		return ""
	}
	if t.Hour() == 0 && t.Minute() == 0 {
		return t.Format("2006-01-02")
	}
	return t.Format("2006-01-02 15:04")
}

// tagesAnfang ist Mitternacht des Tages von t, in dessen eigener Zone.
func tagesAnfang(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

// kalendertage ist der Abstand in KALENDERtagen — nicht die Stundendifferenz
// durch 24. Der Unterschied ist nicht theoretisch: an einer Zeitumstellung hat
// ein Tag 23 oder 25 Stunden, und ein Termin um 09:00 liegt selbst ohne
// Umstellung nicht 24 Stunden von Mitternacht entfernt. Beide Enden werden
// deshalb erst auf Mitternacht gezogen; die verbleibende Abweichung von einer
// Stunde faengt das Runden auf halbe Tage ab.
//
// Das Vorzeichen wird von Hand gefuehrt, weil Go bei negativen Dauern zur Null
// hin rundet: -24h waere sonst 0 statt -1, und "gestern faellig" saehe aus wie
// "heute faellig" — also genau der Eimerfehler, um den es hier geht.
func kalendertage(von, bis time.Time) int {
	a := tagesAnfang(von)
	b := tagesAnfang(bis.In(von.Location()))
	h := b.Sub(a).Hours()
	if h < 0 {
		return -int((-h + 12) / 24)
	}
	return int((h + 12) / 24)
}
