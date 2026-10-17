// Die Ansicht "Logbuch" (gkp-1oj, Spec §5, Things' Logbook).
//
// Eine Datei, eine Regel, ein init — der Rahmen steht in ansicht.go.
package appctx

import "fmt"

// LogbuchID ist der Name der Ansicht fuer Route, Navigation und Browser.
const LogbuchID = "logbuch"

// Die Altersfenster. Sie sind die Eimer der Bilanz im Kopf; die REIHENFOLGE der
// Liste macht der Rang, der feiner ist (siehe waehltLogbuch).
const (
	GrundHeuteErledigt       = "heute erledigt"
	GrundZuletztErledigt     = "letzte 7 Tage"
	GrundFrueherErledigt     = "früher"
	GrundOhneAbschlussdatum  = "ohne Abschlussdatum"
	logbuchZuletztTage       = 7
	rangLogbuchOhneAbschluss = 1 << 30
)

// GrundOhneBeleg ist die einzige BEANSTANDUNG dieser Liste — alles andere darin
// ist Beschreibung. ReportsBead.Gap ist "geschlossen UND ohne aktuellen
// done-Eintrag" (reports.HasCurrentDoneEntry) und steht bereits im Baum; die
// Ansicht traegt ihn nach aussen, statt die Frage ein zweites Mal zu stellen.
//
// Die Zahl im Kopf des Logbuchs ist damit dieselbe, die beads_query op=gaps
// beantwortet — nur an der Stelle, an der man ohnehin hinsieht.
//
// Seit gkp-dop mit EINER Einschraenkung, und sie steht nicht umsonst im
// Kappungssatz: greift logbuchGrenze, zaehlt die Bilanz nur noch innerhalb der
// gezeigten Tage. Gemessen an infra am 2026-09-14 waeren das 69 Zeilen
// "ohne Beleg" in 95, von denen ein gekapptes Logbuch nur die juengsten
// naehme. Verbindlich bleibt deshalb beads_query op=gaps; das Logbuch ist die
// Stelle, an der die Luecke AUFFAELLT, nicht die, an der sie gezaehlt wird.
const GrundOhneBeleg = "ohne Beleg"

func init() {
	regelAnmelden(Regel{
		ID:    LogbuchID,
		Titel: "Logbuch",
		// Zuletzt: die einzige der sechs Listen, die nach hinten sieht. Wer sie
		// oeffnet, sucht nicht Arbeit, sondern eine Bestaetigung.
		Ordnung: 60,
		// Und die einzige, in der der frueher generische Satz "Nichts offen"
		// das Gegenteil dessen behauptete, was gemeint war: hier ist nichts
		// GESCHLOSSEN. Der Leersatz nennt deshalb die Reichweite gleich mit —
		// leer heisst in dieser Liste nicht "nichts gemacht".
		Leer: "Nichts abgeschlossen — in der Rückschau, die bd noch liefert, " +
			"ist keine Aufgabe geschlossen.",
		// Rueckwaerts-Chronologie UNTERHALB der Tagesgrenze (gkp-3oh, Befund A).
		// Die Tagesgrenze selbst macht weiter der Rang; siehe waehltLogbuch.
		Absteigend: true,
		// Die Reichweite ist eine LUECKE und muss deshalb dastehen (Spec §4.4):
		// `bd list -t epic` blendet geschlossene Epics aus, und die Rueckschau
		// haengt an collectors.closedEpicsMax (5 zuletzt geschlossene Karten).
		// Ein leeres Logbuch heisst also nicht "nichts gemacht", sondern "in dem,
		// was bd noch liefert, ist nichts abgeschlossen" — und ohne den Satz sieht
		// das eine wie das andere aus.
		Hinweis: "Abgeschlossene Aufgaben, neuester Tag zuerst. Die Rückschau ist begrenzt: " +
			"bd blendet geschlossene Epics aus, nachgeliefert werden nur die zuletzt " +
			"geschlossenen Karten (collectors.closedEpicsMax). Die vollständige, " +
			"append-only Historie steht in der Reports-Ansicht unter Historie.",
		Grenze:     logbuchGrenze,
		GrenzeSatz: logbuchGrenzeSatz,
		Waehlt:     waehltLogbuch,
	})
}

// logbuchGrenze ist die Obergrenze der Rueckschau in Zeilen (gkp-dop).
//
// GEMESSEN am 2026-09-14, nicht geschaetzt. Das Logbuch war die einzige der
// sechs Listen ohne Deckel, und als einzige waechst es monoton: eine offene
// Aufgabe verlaesst ihre Liste, wenn sie fertig ist — aus dem Logbuch geht nie
// etwas wieder heraus. Die Zahlen desselben Tages, beads-only:
//
//	Repo                   Heute  Jederzeit  Eingang  Logbuch
//	infra                 47         47        2       95
//	git-planner      15         15        0       93
//
// In der Navigation stand am laufenden Stand 294 — die Differenz zu den 95
// sind exakt die 199 geschlossenen GitHub-Issues von infra, die ueber
// GET /ansichten nachkommen (209 Issues, 10 davon offen, Spec §3.7/§9-O2).
// Der Rumpf der Seite traegt also 95 Zeilen, die Antwort der Route 294, und
// beide Zahlen wachsen mit jedem geschlossenen Vorgang weiter.
//
// HUNDERT, aus drei Gruenden:
//
//   - Bei 100 wird KEINE der beiden beads-Rueckschauen angetastet (95 und 93).
//     Die Grenze kostet im Normalfall also nichts und bindet den Fall, der
//     gemessen aus dem Ruder lief.
//   - Es ist die Zahl, die dieselbe Seite fuer dieselbe Frage schon benutzt
//     (timelineMax/historyMax in reports_view.go: "der Speicher wird nur
//     laenger"). Zwei verschiedene Deckel fuer dieselbe Frage auf einer Seite
//     braeuchten einen eigenen Grund, und es gibt keinen.
//   - Es ist ein BUDGET, kein Schnitt: gekappt wird an der Tagesgrenze, ein
//     angezeigter Tag ist immer vollstaendig. Die gemessenen Tage sind
//     ungleichmaessig (infra: 5, 10, 11, 8, 6, 28, 4, 2, 3, 18), ein
//     Zeitfenster "letzte N Tage" haette den Rumpf deshalb gar nicht begrenzt —
//     ein einziger Tag mit 28 Abschluessen faehrt in jedem Fenster ganz mit.
const logbuchGrenze = 100

// logbuchGrenzeSatz ist der Satz, der sagt, dass die Grenze greift.
//
// Er muss eine gekappte Liste von einer ZU ENDE gegangenen unterscheiden —
// ohne ihn sieht das eine aus wie das andere, dasselbe Argument, aus dem
// Regel.Leer entstanden ist. Er nennt deshalb drei Dinge, die eine blosse
// Auslassung nicht nennen kann: wie viele Tage zu sehen sind, wie viele Zeilen
// von wie vielen, und dass die Bilanz im Kopf nur diese Auswahl zaehlt.
//
// Der letzte Teil ist der wichtigste. Im Logbuch ist "ohne Beleg" die einzige
// Zahl, die eine BEANSTANDUNG ist (GrundOhneBeleg), und sie zaehlt seit der
// Kappung nur noch innerhalb des gezeigten Fensters — gemessen an infra
// sind es dort 69 von 95 Zeilen. Die verbindliche Antwort bleibt
// `beads_query op=gaps`; ohne diesen Halbsatz saehe eine kleine Zahl im Kopf
// wie eine kleine Luecke aus.
func logbuchGrenzeSatz(gezeigt, gesamt, tage int) string {
	was := fmt.Sprintf("die %d jüngsten Abschlusstage", tage)
	if tage == 1 {
		was = "der jüngste Abschlusstag"
	}
	return fmt.Sprintf(
		"Hier hört die Liste auf, nicht die Historie: gezeigt sind %s mit %d von %d "+
			"abgeschlossenen Aufgaben, und die Bilanz oben zählt nur diese. Ältere Tage "+
			"bleiben ganz weg, damit die Seite nicht mit der Historie wächst.",
		was, gezeigt, gesamt)
}

// waehltLogbuch ist Spec §5, Zeile Logbook: die Umkehrung jeder anderen
// Ansicht — genau das, was die uebrigen fuenf ausschliessen.
//
// DATUMSDEUTUNG, und hier liegt die Falle: closed_at ist echtes UTC und geht
// durch bdops.ParseEventStamp, NICHT durch ParseUserDateIn. Nur due_at und
// defer_until sind lokale Wanduhr mit einem luegenden Z (Spec §4.4). Die Regel
// deutet das nicht mehr selbst — seit gkp-3oh steht der Abschluss als
// Kandidat.Abschluss neben Frist und Wann, und die Deutung findet fuer alle
// drei Felder an EINER Stelle statt (kandidatAus). Eine Regel, die selbst
// parst, waere die zweite Gelegenheit, die beiden Zeitbasen zu vertauschen.
// Unter TZ=UTC faellt eine Verwechslung in keinem Test auf —
// TestLogbuchDeutetClosedAtAlsEchtesUTC nagelt die Zone deshalb auf einen
// Versatz ungleich null.
//
// REIHENFOLGE, in zwei Stufen, und beide braucht es:
//
//   - die TAGESGRENZE macht der RANG — ein Eimer je Abschlusstag, 0 = heute,
//     1 = gestern, und der Rahmen sortiert Raenge aufsteigend. Das ist Things'
//     Logbook, das nach Abschlusstag gruppiert, und bleibt deshalb so.
//   - INNERHALB eines Tages entscheidet der Zeitpunkt, und zwar absteigend
//     (Regel.Absteigend im init). Vorher setzte die Regel gar keinen Zeitpunkt,
//     weil sortiereGruppen nur aufsteigend konnte; sortiert wurde dann nach
//     Prioritaet und LastChange — "wichtigstes zuerst" mitten in einer Liste,
//     die chronologisch ist.
//
// WIRKUNGSLOS ist die erste dieser beiden Stufen hier trotzdem — fuer die
// SORTIERUNG, und das ist gemessen, nicht vermutet. Rang (Alter in Tagen) und
// Zeitpunkt (Abschlusszeit, absteigend verglichen) sind aus DERSELBEN Groesse
// abgeleitet und strikt gegenlaeufig monoton: je aelter der Abschluss, desto
// groesser der Rang und desto frueher der Zeitpunkt. Die beiden koennen gar
// nicht verschieden entscheiden, der Zeitvergleich allein ergibt schon jede
// Folge, die auch der Rang ergibt.
//
// Zwei Gegenproben vom 2026-09-14 zeigen das von beiden Seiten:
//
//   - CP10, nur die Rang-Stufe in sortiereGruppen abgeschaltet: in der GANZEN
//     Karte wurde genau EIN Test rot, und der steht in Jederzeit. Kein einziger
//     Logbuch-Test.
//   - CP19, die Tages-Eimer hier kollabiert (u.Rang = 0 fuer jeden datierten
//     Abschluss): alle Sortier- und Bilanztests des Logbuchs blieben GRUEN, rot
//     wurde allein TestLogbuchKapptUndSagtEsAnDerTagesgrenze.
//
// Das ist ein Befund ueber den CODE und keine Testluecke: eine Fixture, die
// TestLogbuchSortiertNeuestenTagZuerst bei kollabierten Eimern rot macht, kann
// es wegen der Gegenlaeufigkeit nicht geben.
//
// Wer den Rang deshalb "aufraeumt", zieht der KAPPUNG den Boden weg. kappen()
// schneidet an einer RANGgrenze (Regel.Grenze) — ohne einen Eimer je
// Abschlusstag gibt es keine Tagesgrenze, an der ein angezeigter Tag noch
// vollstaendig waere, und logbuchGrenzeSatz haette keine Tageszahl zu nennen.
// Die Bilanz im Kopf haengt mit daran, aber nur mittelbar: die Zaehler kommen
// aus den GRUENDEN (also aus `alter`, nicht aus u.Rang) — sie zaehlen aber
// ueber die bereits gekappte Liste, und genau das ist die Einschraenkung am
// Kopf von GrundOhneBeleg.
//
// Ein gespiegelter Zeitstempel (etwa die negierte Unixzeit) haette dasselbe
// ohne neues Feld erreicht und war der naheliegende Trick. Er bleibt draussen:
// Urteil.Zeitpunkt ist ein Zeitpunkt und keine Sortierzahl, und ein Feld, das
// gelegentlich das eine und gelegentlich das andere bedeutet, ist genau die
// Sorte Falle, gegen die der Kandidat oben seine Deutung zentralisiert.
func waehltLogbuch(k Kandidat) (Urteil, bool) {
	if k.Offen() {
		return Urteil{}, false
	}
	u := Urteil{Zeitpunkt: k.Abschluss}
	switch {
	case k.Abschluss == nil:
		// Ein geschlossener Bead ohne Stempel ist Historie ohne Datum. Nicht
		// wegwerfen — dann fehlte er spurlos —, sondern ans Ende.
		u.Rang = rangLogbuchOhneAbschluss
		u.Gruende = []string{GrundOhneAbschlussdatum}
	default:
		alter := -kalendertage(k.Heute, *k.Abschluss)
		if alter < 0 {
			// Ein Abschluss in der Zukunft gibt es nicht; eine krumme Uhr schon.
			// Dann ist die Antwort "heute" und nicht ein negativer Rang, der die
			// Zeile vor den heutigen einsortierte.
			alter = 0
		}
		u.Rang = alter
		switch {
		case alter == 0:
			u.Gruende = []string{GrundHeuteErledigt}
		case alter <= logbuchZuletztTage:
			u.Gruende = []string{GrundZuletztErledigt}
		default:
			u.Gruende = []string{GrundFrueherErledigt}
		}
	}
	if k.Bead.Gap {
		// VORANgestellt, nicht angehaengt — die Beanstandung steht vor der
		// Beschreibung. Angehaengt landete "ohne Beleg" als viertes von vier
		// Abzeichen unmittelbar vor der Karte; gemessen am 2026-09-14 in ALLEN
		// 46 beanstandeten Zeilen von /infra/logbuch und in beiden von
		// /git-planner/logbuch, absolut bei x≈470 von 1600px, also rund
		// 203px in die Meta-Zeile hinein. Der Browser sortiert die Gruende
		// ausdruecklich NICHT um (Kommentar an AV_GRUND) — die Reihenfolge
		// entsteht hier und nur hier.
		//
		// Die NEBENWIRKUNG gehoert dazu und ist gewollt: die Bilanz im Kopf
		// bestimmt ihre Reihenfolge ueber das ERSTE Auftreten eines Grundes
		// (ansichtAus). Die erste "früher"-Zeile in infra ist Zeile 27, und
		// die traegt auch die Luecke — der Kopf geht damit von
		// "34 letzte 7 Tage · 66 früher · 46 ohne Beleg" ueber zu
		// "34 letzte 7 Tage · 46 ohne Beleg · 66 früher". Auch dort gehoert die
		// Beanstandung nach vorn und nicht ans Ende; festgehalten von
		// TestLogbuchBilanzNenntBeanstandungVorDemAltersfenster.
		u.Gruende = append([]string{GrundOhneBeleg}, u.Gruende...)
	}
	return u, true
}
