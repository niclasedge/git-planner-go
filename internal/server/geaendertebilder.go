package server

// GET /api/geaenderte-bilder?repo= (beleg-bilder, #105): die Bilddateien der
// juengsten Commits des Standard-Branches, dedupliziert gegen die Belege der
// Galerie aus demselben Arbeitsstand wie /api/beleg-galerie. Gelesen wird die
// Commit-Liste (eine Seite, ghrest.VerlaufTiefe; der ETag-Speicher macht
// einen unveraenderten Abruf billig) und je Commit dessen Dateien. Ein Commit
// aendert sich nie: seine Dateien liegen im eigenen Speicher dieser Route und
// werden nicht erneut gefragt. Gebaut wird in internal/appctx/geaendertebilder.go.

import (
	"context"
	"net/http"
	"strings"
	"sync"

	"github.com/niclasedge/git-planner-go/internal/appctx"
	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

// gbDeckel begrenzt den Speicher; darueber wird er geleert. Ein Leeren
// kostet nur erneute Abrufe.
const gbDeckel = 2000

type geaendertSpeicher struct {
	mu      sync.Mutex
	commits map[string]ghrest.CommitMitDateien
}

func (d *repoDienst) gbSpeicher() *geaendertSpeicher {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.geaendert == nil {
		d.geaendert = &geaendertSpeicher{commits: map[string]ghrest.CommitMitDateien{}}
	}
	return d.geaendert
}

func (s *geaendertSpeicher) commit(schluessel string) (ghrest.CommitMitDateien, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.commits[schluessel]
	return c, ok
}

func (s *geaendertSpeicher) merken(schluessel string, c ghrest.CommitMitDateien) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.commits) >= gbDeckel {
		s.commits = map[string]ghrest.CommitMitDateien{}
	}
	s.commits[schluessel] = c
}

func (d *repoDienst) serveGeaenderteBilder(w http.ResponseWriter, r *http.Request) {
	repo, ok := d.repoAusAnfrage(w, r)
	if !ok {
		return
	}
	scheitern := func(grund string) {
		// Fail open wie jede Lese-Route: 200 mit leerer Liste und Grund.
		g := appctx.GeaenderteBilderBauen(repo.NWO, nil, nil, nil)
		g.Ref, g.Grund = repo.DefaultBranch, grund
		writeJSON(w, g)
	}
	st, err := d.arbeitStand(r.Context(), repo, false)
	if err != nil {
		scheitern(err.Error())
		return
	}
	commits, err := d.juengsteMitDateien(r.Context(), st.pat.client, repo)
	if err != nil {
		scheitern(err.Error())
		return
	}
	// Gegen jeden Beleg, auch eine aeltere Einlieferung, die die Galerie
	// hinter der juengsten verbirgt.
	var belege []appctx.GalerieBeleg
	for _, ch := range st.changes {
		for _, b := range ch.Belege {
			belege = append(belege, appctx.GalerieBeleg{Kennung: ch.Kennung, Pfad: b.Pfad, SHA: b.SHA})
		}
	}
	g := appctx.GeaenderteBilderBauen(repo.NWO, commits, belege, func(sha, name string) string {
		return blobURL(repo.NWO, sha, name)
	})
	g.Ref = repo.DefaultBranch
	// Das Ergebnis ist der Speicher, aus dem "Letzte Aktionen" das neueste
	// Bild nimmt (letzte-aktionen, #126).
	d.mu.Lock()
	d.letzteBilder[strings.ToLower(repo.NWO)] = g
	d.mu.Unlock()
	writeJSON(w, g)
}

// juengsteMitDateien liest die juengsten Commits des Standard-Branches samt
// ihrer Dateien; die Dateien eines Commits kommen aus dem Speicher der Route,
// sobald sie einmal geholt sind. Geteilt von /api/geaenderte-bilder und
// /api/html-dateien (html-dateien, #112).
func (d *repoDienst) juengsteMitDateien(ctx context.Context, c ghrest.Client, repo appctx.LeistenRepo) ([]ghrest.CommitMitDateien, error) {
	koepfe, err := c.JuengsteCommits(ctx, repo.NWO, repo.DefaultBranch)
	if err != nil {
		return nil, err
	}
	sp := d.gbSpeicher()
	nwo := strings.ToLower(repo.NWO)
	commits := make([]ghrest.CommitMitDateien, len(koepfe))
	fehler := make([]error, len(koepfe))
	parallel(len(koepfe), func(i int) {
		schluessel := nwo + "|" + koepfe[i].SHA
		if cm, ok := sp.commit(schluessel); ok {
			commits[i] = cm
			return
		}
		cm, err := c.CommitDateien(ctx, repo.NWO, koepfe[i].SHA)
		if err != nil {
			fehler[i] = err
			return
		}
		sp.merken(schluessel, cm)
		commits[i] = cm
	})
	for _, err := range fehler {
		if err != nil {
			return nil, err
		}
	}
	return commits, nil
}
