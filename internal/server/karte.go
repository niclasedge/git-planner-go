package server

// GET /api/karte?repo= — die Karte eines Repos (repo-karte, #36), seit
// repo-karte-folge (#99) als Folge: die zuletzt geschlossenen Issues, die
// laufenden und die naechsten der Reihe, je auf der Linie ihrer Map, dazu die
// offenen Fragen. Eigene Route und eigener Speicher, weil sie je Map einen
// Aufruf (sub_issues) kostet — geholt wird erst, wenn jemand die Karte
// aufklappt, nie mit der Arbeitsliste. Folge, Status, Branches und offene
// Blocker kommen aus dem Stand der Arbeitsliste; die Karte fragt sie nicht
// noch einmal ab.

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/niclasedge/git-planner-go/internal/anzeigezeit"
	"github.com/niclasedge/git-planner-go/internal/appctx"
	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

type karteStand struct {
	wert appctx.Karte
	zeit time.Time
}

func (d *repoDienst) karteHolen(ctx context.Context, repo appctx.LeistenRepo) appctx.Karte {
	kand := kandidaten(repo)
	if len(kand) == 0 {
		return appctx.KarteBauen(appctx.KartenEingabe{Repo: repo.NWO, Jetzt: anzeigezeit.Jetzt(),
			Quellen: []appctx.ArbeitQuelle{{Quelle: "issues", Grund: "kein PAT gesetzt"}}})
	}
	pc := kand[0]
	c := pc.client
	var offen, zu []ghrest.Issue
	var errOffen, errZu, errReihe, errProjekt error
	var st arbeitStand
	var projekt *string
	var wg sync.WaitGroup
	wg.Add(4)
	// Das Ziel steht in PROJECT.md des Standard-Branches (karte-gruppen, #121);
	// ohne Datei gibt es keins, ein Fehlschlag ist nur eine Quelle.
	go func() {
		defer wg.Done()
		ref := repo.DefaultBranch
		if ref == "" {
			ref = "main"
		}
		projekt, errProjekt = c.Inhalt(ctx, repo.NWO, "PROJECT.md", ref)
	}()
	go func() { defer wg.Done(); offen, errOffen = c.OffeneIssues(ctx, repo.NWO) }()
	go func() { defer wg.Done(); zu, errZu = c.GeschlosseneIssues(ctx, repo.NWO) }()
	// Die Folge steht in der Arbeitsliste; die Karte uebernimmt sie, statt
	// selbst zu ordnen.
	go func() { defer wg.Done(); st, errReihe = d.arbeitStand(ctx, repo, false) }()
	wg.Wait()

	// Maps sind die offenen und die zuletzt geschlossenen Issues mit
	// Sub-Issues; ihre Sub-Issues geben die Linie einer Station.
	var maps []appctx.KartenMapEingabe
	for _, liste := range [][]ghrest.Issue{offen, zu} {
		for _, i := range liste {
			if !i.IstPR && i.SubGesamt > 0 {
				maps = append(maps, appctx.KartenMapEingabe{Map: i})
			}
		}
	}
	parallel(len(maps), func(i int) {
		if subs, err := c.SubIssues(ctx, repo.NWO, maps[i].Map.Nummer); err == nil {
			maps[i].Subs = subs
		}
	})

	e := appctx.KartenEingabe{Repo: repo.NWO, Jetzt: anzeigezeit.Jetzt(), Maps: maps, Offen: offen, Geschlossen: zu}
	if errReihe == nil {
		// Die Karte rechnet ueber die offenen Issues in ihrer Folge, ohne die
		// erledigten Blocker der Reihe (reihe-erledigte-blocker, #130).
		e.Reihe = st.wert.OffeneReihe()
		e.Changes, e.GeschlossenePRs, e.Schluesse = st.changes, st.geschlossenePRs, st.schluesse
	}
	if errProjekt == nil && projekt != nil {
		e.Projekt = *projekt
	}
	grund := func(err error) string {
		if err != nil {
			return err.Error()
		}
		return ""
	}
	e.Quellen = []appctx.ArbeitQuelle{{Quelle: "issues", Grund: grund(errOffen)}, {Quelle: "geschlossen", Grund: grund(errZu)},
		{Quelle: "reihe", Grund: grund(errReihe)}, {Quelle: "projekt", Grund: grund(errProjekt)}}
	k := appctx.KarteBauen(e)
	zahl := func(zone string) int {
		for _, z := range k.Zonen {
			if z.Name == zone {
				return z.Anzahl
			}
		}
		return 0
	}
	logZeile("repo-karte: repo=%s pat=%s quelle=umgebung maps=%d erledigt=%d jetzt=%d naechste=%d fragen=%d kanten=%d%s",
		repo.NWO, pc.pat, len(maps), zahl(appctx.ZoneErledigt), zahl(appctx.ZoneJetzt), zahl(appctx.ZoneNaechste),
		len(k.Fragen), len(k.Kanten), fehlerAnhang(errOffen, errZu, errReihe, errProjekt))
	return k
}

func (d *repoDienst) serveKarte(w http.ResponseWriter, r *http.Request) {
	repo, ok := d.repoAusAnfrage(w, r)
	if !ok {
		return
	}
	schluessel := strings.ToLower(repo.NWO)
	quelle := "karte:" + repo.NWO
	frisch := r.URL.Query().Get("frisch") == "1"
	d.mu.Lock()
	st, da := d.karten[schluessel]
	d.mu.Unlock()
	erneuern := func(ctx context.Context) appctx.Karte {
		start, vorher := time.Now(), ghrest.StatistikLesen()
		k := d.karteHolen(ctx, repo)
		d.mu.Lock()
		d.karten[schluessel] = karteStand{wert: k, zeit: time.Now()}
		d.mu.Unlock()
		d.syncEnde(quelle, start, vorher, k, "")
		return k
	}
	var k appctx.Karte
	switch {
	case da && !frisch:
		if time.Since(st.zeit) >= d.ttlKarte && d.syncBeginn(quelle) {
			go func() {
				hg, abbrechen := context.WithTimeout(context.Background(), time.Minute)
				defer abbrechen()
				erneuern(hg)
			}()
		}
		k = st.wert
	case d.syncBeginn(quelle):
		k = erneuern(r.Context())
	case da:
		k = st.wert
	default:
		k = erneuern(r.Context())
	}
	k.Sync = d.syncStand(quelle)
	writeJSON(w, k)
}
