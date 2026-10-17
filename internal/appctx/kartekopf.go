package appctx

// Die Kopfzahlen der Karte (karte-einzeilig, #124): eingeklappt ist die Karte
// eine Zeile, deren Zahlen und Nummern hier fertig werden. Die Karte traegt sie
// als kopf; die Arbeitsliste als karte_kopf, gebaut aus ihrem eigenen Stand
// ohne Sub-Issues, damit die Zeile ohne Abruf von /api/karte steht. Sub-Issues
// geben nur die Linie einer Station; Zonen, Fragen und Hebel haengen nicht an
// ihnen, deshalb sind die Zahlen beide Male dieselben.

import (
	"time"

	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

// KopfFrage ist eine Frage mit Prio 1 in der Kopfzeile: ihre Nummer und wie
// viele Stationen an ihr haengen.
type KopfFrage struct {
	Nummer int `json:"nummer"`
	Haelt  int `json:"haelt"`
}

// KarteKopf sind die Zahlen der eingeklappten Karte.
type KarteKopf struct {
	Erledigt int         `json:"erledigt"`
	Aktiv    int         `json:"aktiv"`
	Naechste int         `json:"naechste"`
	Fragen   int         `json:"fragen"`
	P1       []KopfFrage `json:"p1"`
	// Bereit ist die erste bereite Station der Zone naechste; 0: keine.
	Bereit int `json:"bereit,omitempty"`
}

// KarteKopfAus zaehlt den Kopf einer gebauten Karte.
func KarteKopfAus(k Karte) KarteKopf {
	out := KarteKopf{P1: []KopfFrage{}, Fragen: len(k.Fragen)}
	for _, z := range k.Zonen {
		switch z.Name {
		case ZoneErledigt:
			out.Erledigt = z.Anzahl
		case ZoneJetzt:
			out.Aktiv = z.Anzahl
		case ZoneNaechste:
			out.Naechste = z.Anzahl
		}
	}
	for _, f := range k.Fragen {
		if f.Prio == 1 {
			out.P1 = append(out.P1, KopfFrage{Nummer: f.Nummer, Haelt: f.Hebel})
		}
	}
	slot := -1
	for _, s := range k.Stationen {
		if s.Zone == ZoneNaechste && s.Bereit && (slot < 0 || s.Slot < slot) {
			out.Bereit, slot = s.Nummer, s.Slot
		}
	}
	return out
}

// arbeitKarteKopf baut den Kopf aus dem Stand der Arbeitsliste: dieselben
// offenen und geschlossenen Issues und die gebaute Reihe, ohne Maps.
func arbeitKarteKopf(e ArbeitEingabe, reihe []ArbeitsEintrag, jetzt time.Time) KarteKopf {
	var offen []ghrest.Issue
	for _, i := range e.Offen {
		if !i.IstPR {
			offen = append(offen, i)
		}
	}
	return KarteBauen(KartenEingabe{Repo: e.Repo, Jetzt: jetzt, Offen: offen, Geschlossen: e.Geschlossen, Reihe: reihe}).Kopf
}
