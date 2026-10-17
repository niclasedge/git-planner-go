package appctx

// Erledigte Blocker bleiben in der Reihe (reihe-erledigte-blocker, #130): ein
// geschlossener Blocker, der noch offene Issues blockiert hat, steht als
// erledigter Eintrag ueber ihnen, aufgeklappt; Karte und Kartenkopf zaehlen
// ihn nicht.

import (
	"fmt"
	"testing"
	"time"

	"github.com/niclasedge/git-planner-go/internal/collectors"
	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

func vierZu() ghrest.Issue {
	return ghrest.Issue{Nummer: 4, Titel: "vier", URL: "u4", Zustand: "closed", Geschlossen: "2026-10-01T07:12:00Z", AlleBlockt: 3}
}

func erledigteEingabe(t *testing.T) ArbeitEingabe {
	return ArbeitEingabe{
		Repo:  "acme/alpha",
		Jetzt: time.Date(2026, 10, 1, 12, 0, 0, 0, berlin(t)),
		Offen: []ghrest.Issue{
			{Nummer: 1, Titel: "eins"},
			{Nummer: 2, Titel: "zwei", Blocker: 1},
			{Nummer: 3, Titel: "drei", AlleBlocker: 1, Blockt: 1, AlleBlockt: 1},
			{Nummer: 5, Titel: "fuenf", AlleBlocker: 1},
		},
		Blocker:          map[int][]int{2: {3}},
		ErledigteBlocker: map[int][]ghrest.Issue{3: {vierZu()}, 5: {vierZu()}},
	}
}

func TestErledigterBlockerMitOffenenKindern(t *testing.T) {
	a := RepoArbeitBauen(erledigteEingabe(t))
	if got := baum(a.Reihe); got != "4:0/-,3:1/4,2:2/3,5:1/4,1:0/-" {
		t.Fatalf("Baum %s, erwartet 4:0/-,3:1/4,2:2/3,5:1/4,1:0/-", got)
	}
	baumInvariante(t, a.Reihe)
	r := reiheNach(a.Reihe)
	vier := r[4]
	if vier.Status != ArbeitErledigt || vier.GeschlossenText == "" || vier.Geschlossen != "2026-10-01T07:12:00Z" || vier.Titel != "vier" {
		t.Errorf("#4 %+v, erwartet erledigt mit Schliesszeit", vier)
	}
	if a := vier.Abhaengige; a == nil || a.Gesamt != 3 || a.Offen != 2 || a.Fertig != 1 || a.Kinder != 2 || !a.Aufgeklappt {
		t.Errorf("#4 abhaengige %+v, erwartet 3/2/1, kinder 2, aufgeklappt", vier.Abhaengige)
	}
	for _, nr := range []int{3, 5} {
		e := r[nr]
		if fmt.Sprint(e.ErledigteBlocker) != "[4]" || len(e.BlockiertDurch) != 0 || e.Status == ArbeitBlockiert {
			t.Errorf("#%d erledigte_blocker %v blockiert_durch %v status %s", nr, e.ErledigteBlocker, e.BlockiertDurch, e.Status)
		}
	}
	if r[3].Abhaengige == nil || r[3].Abhaengige.Aufgeklappt {
		t.Errorf("#3 abhaengige %+v, erwartet vorhanden und nicht aufgeklappt", r[3].Abhaengige)
	}
}

func TestErledigterBlockerLaufendesKindVorn(t *testing.T) {
	a := RepoArbeitBauen(ArbeitEingabe{
		Repo: "acme/alpha",
		Offen: []ghrest.Issue{
			{Nummer: 1, Titel: "eins"},
			{Nummer: 3, Titel: "drei", AlleBlocker: 1},
			{Nummer: 9, Titel: "neun"},
		},
		ErledigteBlocker: map[int][]ghrest.Issue{3: {vierZu()}},
		Changes: []collectors.OpenspecChange{
			{Kennung: "drei", Issue: 3, Aufgaben: aufgaben(1, 3), Ref: "change/drei", RemoteBranch: true},
			{Kennung: "neun", Issue: 9, Aufgaben: aufgaben(3, 3), Ref: "change/neun", RemoteBranch: true},
		},
	})
	if got := baum(a.Reihe); got != "9:0/-,4:0/-,3:1/4,1:0/-" {
		t.Fatalf("Baum %s, erwartet 9:0/-,4:0/-,3:1/4,1:0/-", got)
	}
	if s := reiheNach(a.Reihe)[3].Status; s != ArbeitInArbeit {
		t.Errorf("#3 status %s, erwartet in_arbeit", s)
	}
}

func TestErledigterBlockerNichtAufDerKarte(t *testing.T) {
	mit := erledigteEingabe(t)
	ohne := erledigteEingabe(t)
	ohne.ErledigteBlocker = nil
	a, b := RepoArbeitBauen(mit), RepoArbeitBauen(ohne)
	if a.KarteKopf.Naechste != b.KarteKopf.Naechste || a.KarteKopf.Aktiv != b.KarteKopf.Aktiv || a.KarteKopf.Bereit != b.KarteKopf.Bereit {
		t.Errorf("karte_kopf mit %+v, ohne %+v, erwartet gleich", a.KarteKopf, b.KarteKopf)
	}
	k := KarteBauen(KartenEingabe{Repo: "acme/alpha", Jetzt: mit.Jetzt, Offen: mit.Offen, Reihe: a.Reihe})
	for _, s := range k.Stationen {
		if s.Nummer == 4 && (s.Zone == ZoneJetzt || s.Zone == ZoneNaechste) {
			t.Errorf("Station #4 in Zone %s, erwartet nicht als offen", s.Zone)
		}
	}
}
