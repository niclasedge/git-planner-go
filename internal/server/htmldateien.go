package server

// HTML-Dateien eines Repos (html-dateien, #112).
//
// GET /api/html-dateien?repo= liefert die neuesten HTML-Dateien aus dem Baum
// der Spitze des Standard-Branches (html-dateien-baum, #114), je Datei mit
// letzter Aenderung, Erstellt-Datum und Bildzaehler. Die Spitze und das
// Fenster fuer status added kommen aus denselben Commits wie
// /api/geaenderte-bilder (derselbe Commit-Speicher), die letzte Aenderung je
// Pfad aus commits?path=&per_page=1, gespeichert, bis sich die Spitze
// aendert. Ausgewaehlt und gezaehlt wird in internal/appctx/htmldateien.go;
// hier wird nur gelesen: der Baum der Spitze, die letzte Aenderung je Pfad,
// der Blob der Datei, der Baum ihres Commits und, wo kein Commit sie
// angelegt hat, ihr Verlauf.
//
// GET /api/datei/OWNER/REPO/COMMIT/PFAD liefert eine Datei aus dem Baum
// eines Commits. Weil die Route pfadbasiert ist, loesen sich relative
// Verweise einer HTML-Datei gegen denselben Commit auf. Jede Antwort traegt
// Content-Security-Policy: sandbox (ohne allow-same-origin, ohne
// allow-scripts) und nosniff: das Dokument laeuft in einem opaken Ursprung,
// fuehrt kein Skript aus und kann die API nicht mit den Rechten der
// Nutzerin rufen. Pfade mit . oder .., leere Glieder und Repos ausserhalb
// der Leiste ergeben 404, ohne GitHub nach Baum oder Blob zu fragen.

import (
	"context"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"sync"

	"github.com/niclasedge/git-planner-go/internal/anzeigezeit"
	"github.com/niclasedge/git-planner-go/internal/appctx"
	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

// verlaufSpeicher haelt den Verlauf eines Pfades ab einem Commit; ab einem
// festen Commit aendert er sich nie.
type verlaufSpeicher struct {
	mu   sync.Mutex
	eint map[string][]ghrest.CommitKopf
}

var htmlVerlauf = &verlaufSpeicher{eint: map[string][]ghrest.CommitKopf{}}

func (s *verlaufSpeicher) holen(ctx context.Context, c ghrest.Client, nwo, commit, pfad string) ([]ghrest.CommitKopf, error) {
	schluessel := strings.ToLower(nwo) + "|" + commit + "|" + pfad
	s.mu.Lock()
	v, ok := s.eint[schluessel]
	s.mu.Unlock()
	if ok {
		return v, nil
	}
	v, err := c.CommitsFuerPfad(ctx, nwo, commit, pfad)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	if len(s.eint) >= gbDeckel {
		s.eint = map[string][]ghrest.CommitKopf{}
	}
	s.eint[schluessel] = v
	s.mu.Unlock()
	return v, nil
}

// letzteSpeicher haelt je Repo die letzte Aenderung je Pfad, gueltig fuer
// genau eine Spitze des Branches. Steht der Branch auf einem neuen Commit,
// wird der Eintrag des Repos verworfen.
type letzteSpeicher struct {
	mu   sync.Mutex
	repo map[string]*letzteStand
}

type letzteStand struct {
	spitze string
	pfade  map[string]ghrest.CommitKopf
}

func (d *repoDienst) letzteSp() *letzteSpeicher {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.htmlLetzte == nil {
		d.htmlLetzte = &letzteSpeicher{repo: map[string]*letzteStand{}}
	}
	return d.htmlLetzte
}

func (s *letzteSpeicher) lesen(nwo, spitze, pfad string) (ghrest.CommitKopf, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.repo[nwo]
	if !ok || st.spitze != spitze {
		return ghrest.CommitKopf{}, false
	}
	k, ok := st.pfade[pfad]
	return k, ok
}

func (s *letzteSpeicher) merken(nwo, spitze, pfad string, k ghrest.CommitKopf) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.repo[nwo]
	if !ok || st.spitze != spitze {
		st = &letzteStand{spitze: spitze, pfade: map[string]ghrest.CommitKopf{}}
		s.repo[nwo] = st
	}
	st.pfade[pfad] = k
}

// letzteAenderungen holt die letzte Aenderung je Pfad ab ref, soweit sie
// fuer diese Spitze nicht schon im Speicher liegt. Ein gescheiterter Abruf
// fehlt in der Antwort und wird beim naechsten Mal wiederholt.
func (d *repoDienst) letzteAenderungen(ctx context.Context, c ghrest.Client, nwo, ref, spitze string, pfade []string) map[string]ghrest.CommitKopf {
	sp := d.letzteSp()
	schluessel := strings.ToLower(nwo)
	out := make([]ghrest.CommitKopf, len(pfade))
	da := make([]bool, len(pfade))
	parallel(len(pfade), func(i int) {
		if k, ok := sp.lesen(schluessel, spitze, pfade[i]); ok {
			out[i], da[i] = k, true
			return
		}
		k, err := c.LetzterCommit(ctx, nwo, pfade[i], ref)
		if err != nil {
			return
		}
		sp.merken(schluessel, spitze, pfade[i], k)
		out[i], da[i] = k, true
	})
	m := make(map[string]ghrest.CommitKopf, len(pfade))
	for i, p := range pfade {
		if da[i] {
			m[p] = out[i]
		}
	}
	return m
}

func (d *repoDienst) serveHTMLDateien(w http.ResponseWriter, r *http.Request) {
	repo, ok := d.repoAusAnfrage(w, r)
	if !ok {
		return
	}
	antwort := appctx.HTMLDateien{Repo: repo.NWO, Ref: repo.DefaultBranch, Dateien: []appctx.HTMLDatei{}}
	kand := kandidaten(repo)
	if len(kand) == 0 {
		antwort.Grund = "kein PAT gesetzt"
		writeJSON(w, antwort)
		return
	}
	ctx := r.Context()
	c := kand[0].client
	commits, err := d.juengsteMitDateien(ctx, c, repo)
	if err != nil {
		// Fail open wie jede Lese-Route: 200 mit leerer Liste und Grund.
		antwort.Grund = err.Error()
		writeJSON(w, antwort)
		return
	}
	if len(commits) == 0 {
		writeJSON(w, antwort)
		return
	}
	spitze := commits[0].SHA
	baum, err := d.baum(ctx, c, repo.NWO, spitze)
	if err != nil {
		antwort.Grund = err.Error()
		writeJSON(w, antwort)
		return
	}
	// Gefragt wird ab dem Branch; ein Name, den ghrest nicht annimmt (etwa
	// mit /), faellt auf die Spitze zurueck.
	ref := repo.DefaultBranch
	if !commitRe.MatchString(ref) {
		ref = spitze
	}
	pfade, _ := appctx.HTMLPfadeAusBaum(baum)
	letzte := d.letzteAenderungen(ctx, c, repo.NWO, ref, spitze, pfade)
	dateien, grund := appctx.HTMLDateienAusBaum(repo.NWO, spitze, baum, letzte, commits)
	antwort.Grund = grund
	parallel(len(dateien), func(i int) {
		e := &dateien[i]
		if e.Erstellt == "" {
			v, err := htmlVerlauf.holen(ctx, c, repo.NWO, e.Commit, e.Pfad)
			e.Erstellt, e.ErstelltCommit = appctx.ErstelltAusVerlauf(v, err)
		}
		e.Bilder = d.htmlBilder(ctx, c, repo.NWO, *e)
	})
	// Zeittexte in der Anzeigezone; das Ergebnis ist der Speicher, aus dem
	// "Letzte Aktionen" die neueste HTML-Datei nimmt (letzte-aktionen, #126).
	appctx.HTMLZeittexte(dateien, anzeigezeit.Jetzt())
	antwort.Dateien = dateien
	d.mu.Lock()
	d.letzteHTML[strings.ToLower(repo.NWO)] = antwort
	d.mu.Unlock()
	writeJSON(w, antwort)
}

// htmlBilder liest Datei und Baum ihres Commits und zaehlt die Bilder.
func (d *repoDienst) htmlBilder(ctx context.Context, c ghrest.Client, nwo string, e appctx.HTMLDatei) appctx.HTMLBilder {
	unbekannt := func(grund string) appctx.HTMLBilder {
		return appctx.HTMLBilder{Fehlende: []string{}, Text: "Bilder unbekannt", Grund: grund}
	}
	baum, err := d.baum(ctx, c, nwo, e.Commit)
	if err != nil {
		return unbekannt(err.Error())
	}
	im := map[string]bool{}
	for _, b := range baum {
		if b.Typ == "blob" {
			im[b.Pfad] = true
		}
	}
	sha := e.SHA
	if sha == "" {
		for _, b := range baum {
			if b.Pfad == e.Pfad {
				sha = b.SHA
			}
		}
	}
	inhalt, err := d.blob(ctx, c, nwo, sha)
	if err != nil {
		return unbekannt(err.Error())
	}
	return appctx.HTMLBilderZaehlen(e.Pfad, inhalt, func(p string) bool { return im[p] })
}

// dateiTypen sind die Inhaltstypen der Datei-Route; alles andere ist
// application/octet-stream.
var dateiTypen = map[string]string{
	".html": "text/html; charset=utf-8", ".htm": "text/html; charset=utf-8",
	".css": "text/css; charset=utf-8",
	".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg",
	".gif": "image/gif", ".svg": "image/svg+xml", ".webp": "image/webp",
	".ico": "image/x-icon", ".avif": "image/avif",
	".woff": "font/woff", ".woff2": "font/woff2",
	".txt": "text/plain; charset=utf-8",
}

// commitRe ist die Form eines Commit-Glieds, wie ghrest sie annimmt.
var commitRe = regexp.MustCompile(`^[0-9A-Za-z_-]{1,64}$`)

// vollerSHARe erkennt einen vollen Commit-sha; nur der ist unveraenderlich.
var vollerSHARe = regexp.MustCompile(`^[0-9a-f]{40}$`)

// dateiAusPfad zerlegt /api/datei/OWNER/REPO/COMMIT/PFAD. Gelesen wird der
// maskierte Pfad, damit ein %2F kein Glied teilt; jedes Glied darf nach dem
// Entmaskieren weder leer, . oder .. sein noch / oder \ enthalten.
func dateiAusPfad(r *http.Request) (nwo, commit, pfad string, ok bool) {
	rest, ok := strings.CutPrefix(r.URL.EscapedPath(), "/api/datei/")
	if !ok {
		return "", "", "", false
	}
	roh := strings.Split(rest, "/")
	if len(roh) < 4 {
		return "", "", "", false
	}
	glieder := make([]string, len(roh))
	for i, g := range roh {
		u, err := url.PathUnescape(g)
		if err != nil || u == "" || u == "." || u == ".." || strings.ContainsAny(u, "/\\\x00") {
			return "", "", "", false
		}
		glieder[i] = u
	}
	nwo = glieder[0] + "/" + glieder[1]
	commit = glieder[2]
	if !ghrest.GueltigesNWO(nwo) || !commitRe.MatchString(commit) {
		return "", "", "", false
	}
	return nwo, commit, strings.Join(glieder[3:], "/"), true
}

func (d *repoDienst) serveDatei(w http.ResponseWriter, r *http.Request) {
	nicht := func() {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		http.Error(w, "nicht gefunden", http.StatusNotFound)
	}
	nwo, commit, pfad, ok := dateiAusPfad(r)
	if !ok {
		nicht()
		return
	}
	repo, ok := d.leiste(r.Context(), false).Repo(nwo)
	if !ok {
		nicht()
		return
	}
	var letzter error
	for _, k := range kandidaten(repo) {
		baum, err := d.baum(r.Context(), k.client, repo.NWO, commit)
		if err != nil {
			letzter = err
			continue
		}
		var sha string
		for _, e := range baum {
			if e.Typ == "blob" && e.Pfad == pfad {
				sha = e.SHA
				break
			}
		}
		if sha == "" {
			nicht()
			return
		}
		b, err := d.blob(r.Context(), k.client, repo.NWO, sha)
		if err != nil {
			letzter = err
			continue
		}
		typ, ok := dateiTypen[strings.ToLower(path.Ext(pfad))]
		if !ok {
			typ = "application/octet-stream"
		}
		w.Header().Set("Content-Type", typ)
		w.Header().Set("Content-Security-Policy", "sandbox")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if vollerSHARe.MatchString(commit) {
			w.Header().Set("Cache-Control", "private, max-age=86400, immutable")
		} else {
			w.Header().Set("Cache-Control", "private, no-cache")
		}
		_, _ = w.Write(b)
		return
	}
	grund := "kein PAT gesetzt"
	if letzter != nil {
		grund = letzter.Error()
	}
	w.Header().Set("Content-Security-Policy", "sandbox")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.Error(w, grund, http.StatusBadGateway)
}
