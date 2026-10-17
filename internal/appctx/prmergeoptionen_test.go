package appctx

// Merge-Methoden samt Vorgabe und die Historie geschlossener PRs
// (pr-merge-optionen, #82), ohne Browser geprueft.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

func TestMergeOptionenFolgeUndVorgabe(t *testing.T) {
	faelle := []struct {
		name     string
		erlaubt  *ghrest.MergeErlaubt
		zuletzt  string
		methoden string
		vorgabe  string
	}{
		{"nur squash und rebase", &ghrest.MergeErlaubt{Squash: true, Rebase: true}, "", "squash,rebase", "squash"},
		{"zuletzt rebase", &ghrest.MergeErlaubt{Merge: true, Squash: true, Rebase: true}, "rebase", "merge,squash,rebase", "rebase"},
		{"zuletzt nicht mehr erlaubt", &ghrest.MergeErlaubt{Squash: true, Rebase: true}, "merge", "squash,rebase", "squash"},
		{"zuletzt unbekannt", &ghrest.MergeErlaubt{Merge: true}, "fast", "merge", "merge"},
		{"Einstellungen unlesbar", nil, "", "merge,squash,rebase", "merge"},
		{"Einstellungen unlesbar, zuletzt squash", nil, "squash", "merge,squash,rebase", "squash"},
		{"nichts erlaubt", &ghrest.MergeErlaubt{}, "merge", "", ""},
	}
	for _, f := range faelle {
		t.Run(f.name, func(t *testing.T) {
			o := MergeOptionenBauen(f.erlaubt, f.zuletzt)
			if o.Methoden == nil {
				t.Fatal("methoden ist nil, erwartet eine (ggf. leere) Liste")
			}
			if got := strings.Join(o.Methoden, ","); got != f.methoden || o.Vorgabe != f.vorgabe {
				t.Errorf("methoden %q vorgabe %q, erwartet %q / %q", got, o.Vorgabe, f.methoden, f.vorgabe)
			}
		})
	}
}

func TestMergeOptionenErlaubt(t *testing.T) {
	o := MergeOptionenBauen(&ghrest.MergeErlaubt{Merge: true, Squash: true}, "")
	for m, soll := range map[string]bool{"merge": true, "squash": true, "rebase": false, "fast": false, "": false} {
		if o.Erlaubt(m) != soll {
			t.Errorf("Erlaubt(%q) = %v, erwartet %v", m, !soll, soll)
		}
	}
}

func TestGeschlossenePRsBauen(t *testing.T) {
	prs := []ghrest.PR{
		{Nummer: 7, Titel: "Sieben", URL: "u7", Head: "change/sieben", HeadRepo: "acme/alpha", Zustand: "closed", Gemergt: true,
			GemergtAm: "2026-09-20T10:00:00Z", GeschlossenAm: "2026-09-20T10:00:00Z"},
		{Nummer: 8, Titel: "Acht", URL: "u8", Head: "change/acht", HeadRepo: "acme/alpha", Zustand: "closed",
			GeschlossenAm: "2026-09-24T09:00:00Z"},
		{Nummer: 6, Titel: "Fork", URL: "u6", Head: "change/acht", HeadRepo: "ich/alpha", Zustand: "closed",
			GeschlossenAm: "2026-09-18T09:00:00Z"},
	}
	branches := []ghrest.Branch{{Name: "main", SHA: "m"}, {Name: "change/acht", SHA: "a"}}
	liste := GeschlossenePRsBauen("ACME/alpha", prs, branches)
	if len(liste) != 3 {
		t.Fatalf("Liste %+v, erwartet drei", liste)
	}
	if liste[0].Nummer != 8 || liste[1].Nummer != 7 || liste[2].Nummer != 6 {
		t.Errorf("Folge %d, %d, %d, erwartet 8, 7, 6", liste[0].Nummer, liste[1].Nummer, liste[2].Nummer)
	}
	acht, sieben, fork := liste[0], liste[1], liste[2]
	if acht.Gemergt || !acht.BranchExistiert || !acht.WiederOeffnen || !strings.HasPrefix(acht.Datum, "2026-09-24") ||
		acht.Tag != "24.09.2026" || acht.Titel != "Acht" || acht.URL != "u8" || acht.Head != "change/acht" {
		t.Errorf("#8 %+v", acht)
	}
	if !sieben.Gemergt || sieben.BranchExistiert || sieben.WiederOeffnen || !strings.HasPrefix(sieben.Datum, "2026-09-20") || sieben.Tag != "20.09.2026" {
		t.Errorf("#7 %+v", sieben)
	}
	if fork.BranchExistiert || fork.WiederOeffnen {
		t.Errorf("#6 aus dem Fork %+v: sein Branch liegt nicht in diesem Repo", fork)
	}
}

func TestGeschlossenePRsHoechstensDreissig(t *testing.T) {
	var prs []ghrest.PR
	for i := 1; i <= 35; i++ {
		prs = append(prs, ghrest.PR{Nummer: i, Titel: fmt.Sprint(i), Zustand: "closed",
			GeschlossenAm: fmt.Sprintf("2026-08-%02dT12:00:00Z", i%28+1)})
	}
	liste := GeschlossenePRsBauen("acme/alpha", prs, nil)
	if len(liste) != 30 {
		t.Fatalf("%d Eintraege, erwartet 30", len(liste))
	}
	for i := 1; i < len(liste); i++ {
		if liste[i-1].Datum < liste[i].Datum {
			t.Fatalf("nicht neueste zuerst an Stelle %d: %s vor %s", i, liste[i-1].Datum, liste[i].Datum)
		}
	}
	// Die fuenf aeltesten fallen weg; der einzige PR vom 1. August (i=28) ist
	// der aelteste von allen.
	for _, e := range liste {
		if strings.HasPrefix(e.Datum, "2026-08-01") {
			t.Errorf("aeltester Eintrag #%d ist noch in der Liste", e.Nummer)
		}
	}
	if leer := GeschlossenePRsBauen("acme/alpha", nil, nil); leer == nil || len(leer) != 0 {
		t.Errorf("ohne PRs %+v, erwartet eine leere Liste", leer)
	}
}
