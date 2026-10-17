package appctx

// "Letzte Aktionen" zuerst zehn, dazu alles aus den letzten 24 Stunden,
// hoechstens 150, mit Stunde und Pause fuer den Zeitstrahl (aktionen-24h, #132).

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

func TestAktionenAllesAusVierundzwanzigStunden(t *testing.T) {
	e := AktionenEingabe{Repo: "acme/alpha", Standard: "main", Jetzt: time.Date(2026, 9, 30, 21, 0, 0, 0, berlin(t))}
	start := time.Date(2026, 9, 30, 18, 50, 0, 0, time.UTC)
	for i := 0; i < 40; i++ {
		zeit := start.Add(-time.Duration(i) * 10 * time.Minute).Format(time.RFC3339)
		e.Aktivitaet = append(e.Aktivitaet, akt("pr_merge", "refs/heads/main", zeit, sha40(fmt.Sprint("m", i))))
	}
	e.Aktivitaet = append(e.Aktivitaet, akt("pr_merge", "refs/heads/main", "2026-09-29T18:00:00Z", sha40("alt")))
	l := LetzteAktionenBauen(e)
	if len(l) != 40 {
		t.Fatalf("%d Zeilen, erwartet 40", len(l))
	}
	if l[39].Zeit != "2026-09-30T12:20:00Z" {
		t.Errorf("letzte Zeile %s, erwartet 2026-09-30T12:20:00Z", l[39].Zeit)
	}
}

func TestAktionenHoechstensHundertfuenfzig(t *testing.T) {
	jetzt := time.Date(2026, 9, 30, 21, 0, 0, 0, berlin(t))
	e := AktionenEingabe{Repo: "acme/alpha", Standard: "main", Jetzt: jetzt}
	for i := 0; i < 200; i++ {
		zeit := jetzt.Add(-time.Duration(i) * 30 * time.Second).UTC().Format(time.RFC3339)
		e.Aktivitaet = append(e.Aktivitaet, akt("pr_merge", "refs/heads/main", zeit, sha40(fmt.Sprint("h", i))))
	}
	if n := len(LetzteAktionenBauen(e)); n != 150 {
		t.Errorf("%d Zeilen, erwartet 150", n)
	}
}

func TestAktionenStundeUndPause(t *testing.T) {
	e := AktionenEingabe{Repo: "acme/alpha", Standard: "main", Jetzt: time.Date(2026, 9, 30, 21, 0, 0, 0, berlin(t)),
		Aktivitaet: []ghrest.Aktivitaet{
			akt("pr_merge", "refs/heads/main", "2026-09-30T18:50:00Z", sha40("1")),
			akt("pr_merge", "refs/heads/main", "2026-09-30T18:10:00Z", sha40("2")),
			akt("pr_merge", "refs/heads/main", "2026-09-30T17:55:00Z", sha40("3")),
			akt("pr_merge", "refs/heads/main", "2026-09-30T14:05:00Z", sha40("4")),
		}}
	var stunden, pausen []string
	for _, z := range LetzteAktionenBauen(e) {
		stunden = append(stunden, z.Stunde)
		pausen = append(pausen, z.Pause)
	}
	if got := strings.Join(stunden, "|"); got != "20:00|20:00|19:00|16:00" {
		t.Errorf("stunde %q", got)
	}
	if got := strings.Join(pausen, "|"); got != "|||2 Std. ohne Aktion" {
		t.Errorf("pause %q", got)
	}
	// Eine leere Stunde heisst "1 Std. ohne Aktion".
	e.Aktivitaet[3].Zeit = "2026-09-30T15:30:00Z"
	if p := LetzteAktionenBauen(e)[3].Pause; p != "1 Std. ohne Aktion" {
		t.Errorf("pause %q, erwartet 1 Std. ohne Aktion", p)
	}
}
