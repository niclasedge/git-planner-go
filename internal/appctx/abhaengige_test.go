package appctx

// Abhaengige je Eintrag der Reihe (aktionen-zeitleiste, #128): wie viele
// Issues ein Eintrag blockiert, wie viele davon offen und fertig sind, und
// die Kette der Vorfahren, an der die Oberflaeche einklappt.

import (
	"fmt"
	"testing"

	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

func reiheNach(r []ArbeitsEintrag) map[int]ArbeitsEintrag {
	out := map[int]ArbeitsEintrag{}
	for _, e := range r {
		out[e.Nummer] = e
	}
	return out
}

func abhText(a *Abhaengige) string {
	if a == nil {
		return "-"
	}
	return fmt.Sprintf("%d/%d/%d/%d %s", a.Gesamt, a.Offen, a.Fertig, a.Kinder, a.Text)
}

func TestAbhaengigeAusZusammenfassungUndReihe(t *testing.T) {
	a := RepoArbeitBauen(ArbeitEingabe{
		Repo: "acme/alpha",
		Offen: []ghrest.Issue{
			{Nummer: 1, Titel: "eins", Blockt: 1, AlleBlockt: 1},
			{Nummer: 2, Titel: "zwei", Blocker: 1},
			{Nummer: 3, Titel: "drei", Blocker: 1, Blockt: 1, AlleBlockt: 1},
			{Nummer: 4, Titel: "vier", Blockt: 2, AlleBlockt: 3},
			{Nummer: 5, Titel: "fuenf", Blocker: 2},
		},
		Blocker: map[int][]int{2: {3}, 3: {4}, 5: {4, 1}},
	})
	if got := baum(a.Reihe); got != "4:0/-,3:1/4,2:2/3,1:0/-,5:1/1" {
		t.Fatalf("Baum %s, erwartet 4:0/-,3:1/4,2:2/3,1:0/-,5:1/1", got)
	}
	r := reiheNach(a.Reihe)
	if got := abhText(r[4].Abhaengige); got != "3/2/1/1 3 abhängige Issues · 2 offen · 1 fertig" {
		t.Errorf("#4 abhaengige %s", got)
	}
	if got := abhText(r[3].Abhaengige); got != "1/1/0/1 1 abhängiges Issue · 1 offen · 0 fertig" {
		t.Errorf("#3 abhaengige %s", got)
	}
	if got := abhText(r[1].Abhaengige); got != "1/1/0/1 1 abhängiges Issue · 1 offen · 0 fertig" {
		t.Errorf("#1 abhaengige %s", got)
	}
	if r[2].Abhaengige != nil || r[5].Abhaengige != nil {
		t.Errorf("#2 %s und #5 %s, erwartet kein abhaengige", abhText(r[2].Abhaengige), abhText(r[5].Abhaengige))
	}
	for nr, want := range map[int]string{4: "[]", 3: "[4]", 2: "[3 4]", 1: "[]", 5: "[1]"} {
		if got := fmt.Sprint(r[nr].Vorfahren); got != want {
			t.Errorf("#%d vorfahren %s, erwartet %s", nr, got, want)
		}
		if want == "[]" && r[nr].Vorfahren != nil {
			t.Errorf("#%d traegt vorfahren %v, erwartet das Feld leer", nr, r[nr].Vorfahren)
		}
	}
}

func TestAbhaengigeNurErledigte(t *testing.T) {
	a := RepoArbeitBauen(ArbeitEingabe{Repo: "acme/alpha",
		Offen: []ghrest.Issue{{Nummer: 9, Titel: "neun", AlleBlockt: 2}, {Nummer: 1, Titel: "eins"}}})
	r := reiheNach(a.Reihe)
	if got := abhText(r[9].Abhaengige); got != "2/0/2/0 2 abhängige Issues · 0 offen · 2 fertig" {
		t.Errorf("#9 abhaengige %s", got)
	}
	if r[1].Abhaengige != nil {
		t.Errorf("#1 abhaengige %s, erwartet keins", abhText(r[1].Abhaengige))
	}
}

func TestAbhaengigeOhneZusammenfassung(t *testing.T) {
	a := RepoArbeitBauen(ArbeitEingabe{Repo: "acme/alpha",
		Offen:   []ghrest.Issue{{Nummer: 3, Titel: "drei"}, {Nummer: 4, Titel: "vier"}},
		Blocker: map[int][]int{3: {4}}})
	r := reiheNach(a.Reihe)
	if got := abhText(r[4].Abhaengige); got != "1/1/0/1 1 abhängiges Issue · 1 offen · 0 fertig" {
		t.Errorf("#4 abhaengige %s", got)
	}
	if r[3].Abhaengige != nil {
		t.Errorf("#3 abhaengige %s, erwartet keins", abhText(r[3].Abhaengige))
	}
}
