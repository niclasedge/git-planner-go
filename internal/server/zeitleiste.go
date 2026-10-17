package server

// GET /api/zeitleiste?repo=&kennung= — wann welche Aufgabe eines Changes
// abgehakt wurde (aufgaben-zeitleiste, #55). Gelesen wird ab dem Kopf-Commit
// des Orts, aus dem der Change stammt (Default-Branch oder change/KENNUNG):
// die Commits auf seiner tasks.md (eine Seite, ghrest.VerlaufTiefe) und je
// Commit der Patch. Bei einem archivierten Change zusaetzlich der alte Pfad
// unter openspec/changes/KENNUNG/, weil die REST-API Umbenennungen nicht
// folgt. Ausgewertet wird in internal/appctx.
//
// Speicher: Patches ueber repo, sha und Pfad (ein Commit aendert sich nie),
// das Ergebnis ueber repo, Kennung und Kopf-Commit — ohne neuen Commit fragt
// ein zweiter Aufruf GitHub nicht noch einmal.

import (
	"net/http"
	"strings"
	"sync"

	"github.com/niclasedge/git-planner-go/internal/appctx"
)

// zlDeckel begrenzt beide Speicher; darueber werden sie geleert. Ein Leeren
// kostet nur erneute Abrufe.
const zlDeckel = 2000

type zeitleistenSpeicher struct {
	mu         sync.Mutex
	patches    map[string]appctx.ZeitleistenCommit
	ergebnisse map[string]appctx.AufgabenZeitleiste
}

func (d *repoDienst) zlSpeicher() *zeitleistenSpeicher {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.verlauf == nil {
		d.verlauf = &zeitleistenSpeicher{patches: map[string]appctx.ZeitleistenCommit{},
			ergebnisse: map[string]appctx.AufgabenZeitleiste{}}
	}
	return d.verlauf
}

func (s *zeitleistenSpeicher) patch(schluessel string) (appctx.ZeitleistenCommit, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.patches[schluessel]
	return c, ok
}

func (s *zeitleistenSpeicher) patchMerken(schluessel string, c appctx.ZeitleistenCommit) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.patches) >= zlDeckel {
		s.patches = map[string]appctx.ZeitleistenCommit{}
	}
	s.patches[schluessel] = c
}

func (s *zeitleistenSpeicher) ergebnis(schluessel string) (appctx.AufgabenZeitleiste, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	z, ok := s.ergebnisse[schluessel]
	return z, ok
}

func (s *zeitleistenSpeicher) ergebnisMerken(schluessel string, z appctx.AufgabenZeitleiste) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.ergebnisse) >= zlDeckel {
		s.ergebnisse = map[string]appctx.AufgabenZeitleiste{}
	}
	s.ergebnisse[schluessel] = z
}

func (d *repoDienst) serveZeitleiste(w http.ResponseWriter, r *http.Request) {
	repo, ok := d.repoAusAnfrage(w, r)
	if !ok {
		return
	}
	kennung := strings.TrimSpace(r.URL.Query().Get("kennung"))
	antwort := appctx.AufgabenZeitleiste{Repo: repo.NWO, Kennung: kennung, Eintraege: []appctx.AufgabeAbgehakt{}}
	if kennung == "" {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"grund": "Parameter kennung fehlt"})
		return
	}
	st, err := d.arbeitStand(r.Context(), repo, false)
	if err != nil {
		antwort.Grund = err.Error()
		writeJSON(w, antwort)
		return
	}
	ort, ok := st.orte[kennung]
	if !ok {
		writeJSONStatus(w, http.StatusNotFound, map[string]any{"grund": "unbekannter Change " + kennung})
		return
	}
	antwort.Ref = ort.ref
	sp := d.zlSpeicher()
	nwo := strings.ToLower(repo.NWO)
	schluessel := nwo + "|" + kennung + "|" + ort.baum + "|" + ort.ordner
	if z, ok := sp.ergebnis(schluessel); ok {
		writeJSON(w, z)
		return
	}

	pfade := []string{ort.ordner + "/tasks.md"}
	if strings.Contains(ort.ordner, "/archive/") {
		pfade = append(pfade, "openspec/changes/"+kennung+"/tasks.md")
	}
	c := st.pat.client
	type ziel struct{ sha, pfad string }
	var ziele []ziel
	gesehen := map[string]bool{}
	for _, p := range pfade {
		ks, err := c.CommitsFuerPfad(r.Context(), repo.NWO, ort.baum, p)
		if err != nil {
			antwort.Grund = err.Error()
			writeJSON(w, antwort)
			return
		}
		for _, k := range ks {
			if !gesehen[k.SHA] {
				gesehen[k.SHA] = true
				ziele = append(ziele, ziel{sha: k.SHA, pfad: p})
			}
		}
	}
	commits := make([]appctx.ZeitleistenCommit, len(ziele))
	fehler := make([]error, len(ziele))
	parallel(len(ziele), func(i int) {
		pk := nwo + "|" + ziele[i].sha + "|" + ziele[i].pfad
		if z, ok := sp.patch(pk); ok {
			commits[i] = z
			return
		}
		k, patch, err := c.CommitPatch(r.Context(), repo.NWO, ziele[i].sha, ziele[i].pfad)
		if err != nil {
			fehler[i] = err
			return
		}
		commits[i] = appctx.ZeitleistenCommit{SHA: k.SHA, Zeit: k.Zeit, Betreff: k.Betreff, Patch: patch}
		sp.patchMerken(pk, commits[i])
	})
	for _, err := range fehler {
		if err != nil {
			antwort.Grund = err.Error()
			writeJSON(w, antwort)
			return
		}
	}
	antwort.Commits = len(commits)
	antwort.Eintraege = appctx.AufgabenZeitleisteBauen(commits)
	logZeile("zeitleiste: repo=%s kennung=%s ref=%s commits=%d abgehakt=%d", repo.NWO, kennung, ort.ref, antwort.Commits, len(antwort.Eintraege))
	sp.ergebnisMerken(schluessel, antwort)
	writeJSON(w, antwort)
}
