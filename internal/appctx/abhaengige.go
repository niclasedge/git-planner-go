package appctx

// Abhaengige je Eintrag der Reihe (aktionen-zeitleiste, #128): wie viele
// Issues ein Eintrag blockiert, wie viele davon offen und fertig sind, und je
// eingeruecktem Eintrag die Kette seiner Vorfahren. Die Oberflaeche klappt
// daran ein; gezaehlt wird hier.

import (
	"fmt"

	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

// Abhaengige sind die Zahlen unter einem Blocker der Reihe.
type Abhaengige struct {
	// Gesamt zaehlt alle Issues, die der Eintrag blockiert oder blockierte,
	// Offen die offenen, Fertig die geschlossenen; Kinder die Eintraege, die
	// in der Reihe unter ihm stehen.
	Gesamt int    `json:"gesamt"`
	Offen  int    `json:"offen"`
	Fertig int    `json:"fertig"`
	Kinder int    `json:"kinder"`
	Text   string `json:"text"`
	// Aufgeklappt: die Oberflaeche beginnt mit sichtbaren Kindern; so bei
	// einem erledigten Blocker (reihe-erledigte-blocker, #130).
	Aufgeklappt bool `json:"aufgeklappt,omitempty"`
}

// AbhaengigeText ist die Zeile unter dem Blocker.
func AbhaengigeText(a Abhaengige) string {
	wort := "abhängige Issues"
	if a.Gesamt == 1 {
		wort = "abhängiges Issue"
	}
	return fmt.Sprintf("%d %s · %d offen · %d fertig", a.Gesamt, wort, a.Offen, a.Fertig)
}

// abhaengigeSetzen traegt in der Reihe je Blocker abhaengige und je
// eingeruecktem Eintrag vorfahren ein. issues sind die Issues der Reihe mit
// ihrer issue_dependencies_summary, auch die erledigten Blocker.
func abhaengigeSetzen(reihe []ArbeitsEintrag, issues []ghrest.Issue) {
	zusammen := map[int]ghrest.Issue{}
	for _, i := range issues {
		zusammen[i.Nummer] = i
	}
	bekannt, kinder := map[int]int{}, map[int]int{}
	unter := map[int]int{}
	for _, e := range reihe {
		for _, b := range e.BlockiertDurch {
			bekannt[b]++
		}
		for _, b := range e.ErledigteBlocker {
			bekannt[b]++
		}
		if e.Unter != 0 {
			kinder[e.Unter]++
			unter[e.Nummer] = e.Unter
		}
	}
	for i := range reihe {
		e := &reihe[i]
		if e.Tiefe > 0 {
			gesehen := map[int]bool{e.Nummer: true}
			for u := e.Unter; u != 0 && !gesehen[u]; u = unter[u] {
				gesehen[u] = true
				e.Vorfahren = append(e.Vorfahren, u)
			}
		}
		z := zusammen[e.Nummer]
		a := Abhaengige{Offen: max(z.Blockt, bekannt[e.Nummer]), Kinder: kinder[e.Nummer]}
		a.Gesamt = max(z.AlleBlockt, a.Offen)
		if a.Gesamt == 0 {
			continue
		}
		a.Fertig = a.Gesamt - a.Offen
		a.Text = AbhaengigeText(a)
		a.Aufgeklappt = e.Status == ArbeitErledigt
		e.Abhaengige = &a
	}
}
