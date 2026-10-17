package cache

import (
	"sync"
	"testing"
	"time"

	"github.com/niclasedge/git-planner-go/internal/appctx"
)

// Der Takt der Leiste (projektliste-api, #11): ein abgelaufener Stand geht
// sofort raus, die Erneuerung laeuft genau einmal im Hintergrund.

func TestGetLeisteAbgelaufenerStandGehtSofortRaus(t *testing.T) {
	ResetLeisteForTest()
	defer ResetLeisteForTest()
	t.Setenv("CC_KNOWLEDGE_TTL_MS", "1")

	frei := make(chan struct{})
	var mu sync.Mutex
	calls := 0
	compute := func(jetzt time.Time) appctx.Leiste {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		if n > 1 {
			<-frei
		}
		return appctx.Leiste{Bereiche: []appctx.LeistenBereich{{Name: "lauf"}}, Stand: jetzt.Format(time.RFC3339Nano)}
	}

	erster := GetLeiste(compute)
	if erster.Stand == "" {
		t.Fatal("erster Stand ohne stand")
	}
	time.Sleep(5 * time.Millisecond)

	ergebnis := make(chan appctx.Leiste, 2)
	go func() { ergebnis <- GetLeiste(compute) }()
	go func() { ergebnis <- GetLeiste(compute) }()
	for i := 0; i < 2; i++ {
		select {
		case l := <-ergebnis:
			if l.Stand != erster.Stand {
				t.Errorf("stand %q, erwartet den alten %q", l.Stand, erster.Stand)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("GetLeiste wartet auf die blockierte Berechnung")
		}
	}

	close(frei)
	WaitForLeisteForTest()
	mu.Lock()
	n := calls
	mu.Unlock()
	if n != 2 {
		t.Fatalf("erwartet genau eine Erneuerung im Hintergrund, compute lief %d mal", n)
	}
	if neu := GetLeiste(compute); neu.Stand == erster.Stand {
		t.Error("nach der Erneuerung steht noch der alte Stand da")
	}
}

func TestGetLeisteKalterAufrufWartetEinmal(t *testing.T) {
	ResetLeisteForTest()
	defer ResetLeisteForTest()

	calls := 0
	compute := func(jetzt time.Time) appctx.Leiste {
		calls++
		return appctx.Leiste{Stand: jetzt.Format(time.RFC3339)}
	}
	a := GetLeiste(compute)
	b := GetLeiste(compute)
	if calls != 1 {
		t.Errorf("compute lief %d mal, erwartet 1 (zweiter Aufruf aus dem Stand)", calls)
	}
	if a.Stand == "" || a.Stand != b.Stand {
		t.Errorf("stand %q / %q", a.Stand, b.Stand)
	}
}
