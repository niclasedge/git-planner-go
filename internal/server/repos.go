package server

// Die Routen der Things-Oberflaeche fuer Repos (repo-leiste, #25):
//
//	GET /api/repos                 Leiste: Repos aus allen PATs + lokale Clones, nach Kategorie
//	GET /api/arbeit?repo=          Arbeitsliste: Reihenfolge, Status, Changes, die letzten fuenf
//	GET /api/detail?repo=&art=     Detailspalte: Issue-Text oder die Dateien eines Changes
//	GET /api/blob?repo=&sha=&name= Belegbilder aus dem Repo
//
// Geholt wird per REST (ghrest), die openspec-Changes ueber die Git-Trees- und
// Blob-API aus dem Default-Branch und den offenen change/*-Branches. Blobs sind
// ueber ihren sha unveraenderlich und werden ohne Ablauf zwischengespeichert;
// Baeume ebenso (sha des Commits). Was sich aendert — Repo-Liste, Issues,
// Branch-Koepfe — hat eine kurze TTL, und ?frisch=1 umgeht sie.
//
// Ausgewaehlt, gruppiert und sortiert wird in internal/appctx.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/niclasedge/git-planner-go/internal/anzeigezeit"
	"github.com/niclasedge/git-planner-go/internal/appctx"
	"github.com/niclasedge/git-planner-go/internal/areas"
	"github.com/niclasedge/git-planner-go/internal/cache"
	"github.com/niclasedge/git-planner-go/internal/collectors"
	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

// RepoLog ist das Ziel der Logzeilen je GitHub-Abruf — stderr, also das Journal
// der systemd-Unit (`just logs`). Tests lenken es in einen Puffer um.
var RepoLog io.Writer = os.Stderr

// patVariablen sind die Umgebungsvariablen mit den PATs, in dieser Folge
// befragt (dieselben Namen wie die GitHub-Karte, GitHub-Issue #2).
var patVariablen = []string{"GITHUB_PAT_WORK", "GITHUB_PAT_PERSONAL"}

const (
	kontenTTL = 5 * time.Minute
	arbeitTTL = 90 * time.Second
	// blobDeckel begrenzt den Blob-Speicher; darueber wird er geleert. Ein
	// Leeren kostet nur erneute Abrufe, nie Daten.
	blobDeckel = 64 << 20
	// parallelAbrufe begrenzt die gleichzeitigen GitHub-Aufrufe je Vorgang.
	parallelAbrufe = 8
)

type patClient struct {
	pat    string
	client ghrest.Client
}

// changeOrt merkt sich, aus welchem Baum ein Change gelesen wurde — fuer die
// Detailspalte, die seine Dateien spaeter einzeln braucht.
type changeOrt struct {
	baum   string // sha des Commits
	ordner string
	ref    string
	// standard ist der Default-Branch, haupt der sha seines Kopfes — fuer
	// den Abgleich gegen den Diff (dateien-abgleich, #53).
	standard string
	haupt    string
}

type arbeitStand struct {
	wert appctx.RepoArbeit
	orte map[string]changeOrt // Kennung -> Ort; aktive vor archivierten
	pat  patClient
	zeit time.Time
	// changes sind die gelesenen Changes samt Belegen (beleg-galerie, #54).
	changes []collectors.OpenspecChange
	// geschlossenePRs und schluesse nimmt die Karte fuer gemergt_mit ihrer
	// Gruppen (karte-gruppen, #121), ohne sie noch einmal zu holen.
	geschlossenePRs []ghrest.PR
	schluesse       map[int]appctx.IssueSchluss
}

type repoDienst struct {
	projekte func() []collectors.ProjectRef

	mu         sync.Mutex
	konten     []appctx.RepoKonto
	lokal      []appctx.LokalesRepo
	kontenZeit time.Time
	arbeit     map[string]arbeitStand
	karten     map[string]karteStand
	uebersicht *uebersichtStand
	repoStand  *repoStandStand
	logbuecher map[int]*logbuchStand
	verlauf    *zeitleistenSpeicher // zeitleiste.go, lazy
	geaendert  *geaendertSpeicher   // geaendertebilder.go, lazy
	htmlLetzte *letzteSpeicher      // htmldateien.go, lazy
	sync       map[string]*syncEintrag
	ttlKonten  time.Duration
	ttlArbeit  time.Duration
	ttlKarte   time.Duration
	baeume     map[string][]ghrest.BaumEintrag
	blobs      map[string][]byte
	blobBytes  int
	checks     *appctx.ChecksSpeicher // prchecks.go, lazy
	// mergeWahl ist je Repo (klein geschrieben) die zuletzt erfolgreich
	// gewaehlte Merge-Methode (pr-merge-optionen, #82); nur im Speicher.
	mergeWahl map[string]string
	// schluesse ist je Repo, Issue und closed_at, was die Timeline fuer
	// gemergt_mit hergab (zuletzt-erledigt-pr, #116); nur Erfolge.
	schluesse map[string]appctx.IssueSchluss
	// ciOrdner ist der Plattenspeicher der CI-Runs, ciStaende der Stand je
	// Repo (klein geschrieben; nil = auf der Platte nichts) (ci-runs, #122).
	ciOrdner  string
	ciStaende map[string]*cache.CIStand
	// commitKoepfe ist je Repo und SHA (klein geschrieben, "nwo@sha") der
	// Commit, den die Zeile "Letzter Commit" braucht (karte-einzeilig, #124);
	// nur Erfolge, je Repo nur die zuletzt gebrauchten SHAs.
	commitKoepfe map[string]ghrest.CommitKopf
	// aktivitaeten ist je Repo (klein geschrieben) die zuletzt erfolgreich
	// geholte Aktivitaet samt Commits ihrer Zeilen; letzteBilder und
	// letzteHTML das zuletzt gebaute Ergebnis von /api/geaenderte-bilder und
	// /api/html-dateien. Aus ihnen setzt /api/arbeit bei jedem Aufruf
	// letzte_aktionen zusammen (letzte-aktionen, #126).
	aktivitaeten map[string]aktivitaetStand
	letzteBilder map[string]appctx.GeaenderteBilder
	letzteHTML   map[string]appctx.HTMLDateien
	// vergleiche ist je Repo und Paar ("nwo@before...after", klein
	// geschrieben) der Vergleich eines Pushes: seine Commits und Dateien
	// (aktionen-zeitleiste, #128). Ein Paar aendert sich nie; nur Erfolge.
	vergleiche map[string]ghrest.PushVergleich
}

// vergleichDeckel begrenzt den Speicher der Vergleiche; darueber wird er
// geleert. Ein Leeren kostet nur erneute Abrufe.
const vergleichDeckel = 2000

// aktivitaetStand ist die Aktivitaet eines Repos mit den Commits je SHA und
// den Vergleichen ihrer Pushes.
type aktivitaetStand struct {
	akt        []ghrest.Aktivitaet
	commits    map[string]ghrest.CommitKopf
	vergleiche map[string]ghrest.PushVergleich
}

func neuerRepoDienst(projekte func() []collectors.ProjectRef) *repoDienst {
	return &repoDienst{projekte: projekte, arbeit: map[string]arbeitStand{}, karten: map[string]karteStand{},
		logbuecher: map[int]*logbuchStand{},
		sync:       map[string]*syncEintrag{}, ttlKonten: ttlAusUmgebung(kontenTTL), ttlArbeit: ttlAusUmgebung(arbeitTTL),
		ttlKarte: ttlAusUmgebung(arbeitTTL),
		baeume:   map[string][]ghrest.BaumEintrag{}, blobs: map[string][]byte{},
		schluesse: map[string]appctx.IssueSchluss{},
		ciOrdner:  cache.CIOrdner(), ciStaende: map[string]*cache.CIStand{},
		commitKoepfe: map[string]ghrest.CommitKopf{},
		aktivitaeten: map[string]aktivitaetStand{}, letzteBilder: map[string]appctx.GeaenderteBilder{},
		letzteHTML: map[string]appctx.HTMLDateien{}, vergleiche: map[string]ghrest.PushVergleich{}}
}

// logZeile schreibt eine Zeile ins Journal. Jeder gesetzte Tokenwert wird
// ersetzt, auch wenn er in einer Fehlermeldung von GitHub steckt.
func logZeile(format string, args ...any) {
	zeile := fmt.Sprintf(format, args...)
	for _, v := range patVariablen {
		if tok := os.Getenv(v); tok != "" {
			zeile = strings.ReplaceAll(zeile, tok, "***")
		}
	}
	zeile = strings.ReplaceAll(zeile, "\n", " ")
	// Protokolle stehen in UTC, auch wenn TZ die lokale Zone setzt
	// (letzte-aktionen, #126).
	fmt.Fprintf(RepoLog, "%s %s\n", time.Now().UTC().Format(time.RFC3339), zeile)
}

func gesetztePATs() []patClient {
	var out []patClient
	for _, v := range patVariablen {
		if tok := os.Getenv(v); tok != "" {
			out = append(out, patClient{pat: v, client: ghrest.Client{Token: tok}})
		}
	}
	return out
}

// ---------------------------------------------------------------- Leiste

func (d *repoDienst) kontenHolen(ctx context.Context) []appctx.RepoKonto {
	var out []appctx.RepoKonto
	for _, v := range patVariablen {
		tok := os.Getenv(v)
		if tok == "" {
			out = append(out, appctx.RepoKonto{PAT: v, Grund: v + " ist nicht gesetzt"})
			continue
		}
		repos, err := ghrest.Client{Token: tok}.Repos(ctx)
		if err != nil {
			logZeile("repo-leiste: pat=%s quelle=umgebung fehler=%q", v, err.Error())
			out = append(out, appctx.RepoKonto{PAT: v, Grund: err.Error()})
			continue
		}
		logZeile("repo-leiste: pat=%s quelle=umgebung repos=%d", v, len(repos))
		out = append(out, appctx.RepoKonto{PAT: v, Repos: repos})
	}
	return out
}

func (d *repoDienst) lokalHolen() []appctx.LokalesRepo {
	var out []appctx.LokalesRepo
	if d.projekte == nil {
		return out
	}
	for _, p := range d.projekte() {
		r, ok := collectors.GhOriginRemote(p.Path)
		if !ok || r.APIHost != "github.com" {
			continue
		}
		out = append(out, appctx.LokalesRepo{NWO: r.NWO(), Name: p.Name, Pfad: p.Path})
	}
	return out
}

// leiste baut die Leiste. Die Konten kommen aus dem Speicher; ist er
// abgelaufen, wird er im Hintergrund erneuert (sync-status, #38), beim ersten
// Mal und mit frisch synchron. Die Kategorien werden JEDES Mal aus areas.json
// gebaut, damit ein Einklappen oder Zuordnen sofort sichtbar ist.
func (d *repoDienst) leiste(ctx context.Context, frisch bool) appctx.RepoLeiste {
	d.mu.Lock()
	leer := d.konten == nil
	alt := time.Since(d.kontenZeit) >= d.ttlKonten
	d.mu.Unlock()
	switch {
	case leer || frisch:
		if d.syncBeginn("leiste") {
			d.kontenErneuern(ctx)
		}
	case alt:
		if d.syncBeginn("leiste") {
			go func() {
				hg, abbrechen := context.WithTimeout(context.Background(), time.Minute)
				defer abbrechen()
				d.kontenErneuern(hg)
			}()
		}
	}
	d.mu.Lock()
	konten, lokal := d.konten, d.lokal
	d.mu.Unlock()
	st, err := areas.Default().Load()
	if err != nil {
		st = areas.State{}
	}
	l := appctx.RepoLeisteBauen(konten, lokal, st, time.Now())
	l.Sync = d.syncStand("leiste")
	d.ciPunkteEintragen(&l)
	return l
}

// kontenErneuern holt Konten und lokale Clones und traegt den Sync ein. Der
// Aufrufer hat syncBeginn("leiste") schon gewonnen.
func (d *repoDienst) kontenErneuern(ctx context.Context) {
	start, vorher := time.Now(), ghrest.StatistikLesen()
	konten, lokal := d.kontenHolen(ctx), d.lokalHolen()
	d.mu.Lock()
	d.konten, d.lokal, d.kontenZeit = konten, lokal, time.Now()
	d.mu.Unlock()
	fehler := ""
	alleFehlgeschlagen := len(konten) > 0
	for _, k := range konten {
		if k.Grund == "" || len(k.Repos) > 0 {
			alleFehlgeschlagen = false
		}
	}
	if alleFehlgeschlagen {
		fehler = "kein Konto lieferte eine Repo-Liste — angezeigt werden nur lokale Clones"
	}
	d.syncEnde("leiste", start, vorher, konten, fehler)
}

// kandidaten sind die Zugaenge, mit denen ein Repo abgefragt wird: der PAT,
// der es in der Liste sieht, sonst jeder gesetzte der Reihe nach.
func kandidaten(r appctx.LeistenRepo) []patClient {
	alle := gesetztePATs()
	if r.PAT == "" {
		return alle
	}
	out := []patClient{}
	for _, p := range alle {
		if p.pat == r.PAT {
			out = append([]patClient{p}, out...)
		} else {
			out = append(out, p)
		}
	}
	return out
}

// ---------------------------------------------------------------- Baum und Blob

func (d *repoDienst) baum(ctx context.Context, c ghrest.Client, nwo, sha string) ([]ghrest.BaumEintrag, error) {
	schluessel := strings.ToLower(nwo) + "|" + sha
	d.mu.Lock()
	b, ok := d.baeume[schluessel]
	d.mu.Unlock()
	if ok {
		return b, nil
	}
	b, err := c.Baum(ctx, nwo, sha)
	if err != nil {
		return nil, err
	}
	d.mu.Lock()
	if len(d.baeume) > 256 {
		d.baeume = map[string][]ghrest.BaumEintrag{}
	}
	d.baeume[schluessel] = b
	d.mu.Unlock()
	return b, nil
}

func (d *repoDienst) blob(ctx context.Context, c ghrest.Client, nwo, sha string) ([]byte, error) {
	schluessel := strings.ToLower(nwo) + "|" + sha
	d.mu.Lock()
	b, ok := d.blobs[schluessel]
	d.mu.Unlock()
	if ok {
		return b, nil
	}
	b, err := c.Blob(ctx, nwo, sha)
	if err != nil {
		return nil, err
	}
	d.mu.Lock()
	if d.blobBytes+len(b) > blobDeckel {
		d.blobs, d.blobBytes = map[string][]byte{}, 0
	}
	if _, schon := d.blobs[schluessel]; !schon {
		d.blobs[schluessel] = b
		d.blobBytes += len(b)
	}
	d.mu.Unlock()
	return b, nil
}

// baumLeser ist ein collectors.OpenspecLeser ueber einen GitHub-Baum.
type baumLeser struct {
	d       *repoDienst
	ctx     context.Context
	c       ghrest.Client
	nwo     string
	dateien map[string]ghrest.BaumEintrag
	ordner  map[string]bool
}

func neuerBaumLeser(ctx context.Context, d *repoDienst, c ghrest.Client, nwo string, baum []ghrest.BaumEintrag) *baumLeser {
	l := &baumLeser{d: d, ctx: ctx, c: c, nwo: nwo, dateien: map[string]ghrest.BaumEintrag{}, ordner: map[string]bool{}}
	for _, e := range baum {
		if e.Typ == "blob" {
			l.dateien[e.Pfad] = e
		} else if e.Typ == "tree" {
			l.ordner[e.Pfad] = true
		}
		// Auch ein Baum ohne tree-Eintraege (nur Dateien) kennt seine Ordner.
		for dir := path.Dir(e.Pfad); dir != "." && dir != "/"; dir = path.Dir(dir) {
			l.ordner[dir] = true
		}
	}
	return l
}

func (l *baumLeser) Lesen(p string) ([]byte, error) {
	e, ok := l.dateien[p]
	if !ok {
		return nil, os.ErrNotExist
	}
	return l.d.blob(l.ctx, l.c, l.nwo, e.SHA)
}

func (l *baumLeser) Groesse(p string) int64 {
	if e, ok := l.dateien[p]; ok {
		return e.Groesse
	}
	return -1
}

func (l *baumLeser) Ordner(p string) []string {
	gesehen := map[string]bool{}
	for dir := range l.ordner {
		if path.Dir(dir) == p {
			gesehen[path.Base(dir)] = true
		}
	}
	out := make([]string, 0, len(gesehen))
	for n := range gesehen {
		if !strings.HasPrefix(n, ".") {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}

func (l *baumLeser) sha(p string) string { return l.dateien[p].SHA }

// vorladen holt die Blobs, die die Leseregeln brauchen werden, parallel —
// sonst liefe jeder Change nacheinander ueber das Netz.
func (l *baumLeser) vorladen(filter func(string) bool) {
	var shas []string
	for p, e := range l.dateien {
		if filter(p) {
			shas = append(shas, e.SHA)
		}
	}
	parallel(len(shas), func(i int) { _, _ = l.d.blob(l.ctx, l.c, l.nwo, shas[i]) })
}

func parallel(n int, f func(i int)) {
	var wg sync.WaitGroup
	sem := make(chan struct{}, parallelAbrufe)
	for i := 0; i < n; i++ {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			f(i)
		}(i)
	}
	wg.Wait()
}

func openspecDatei(p string) bool {
	if !strings.HasPrefix(p, "openspec/changes/") {
		return false
	}
	return strings.HasSuffix(p, "/proposal.md") || strings.HasSuffix(p, "/tasks.md") || strings.HasSuffix(p, "/belege/belege.jsonl")
}

var bildTypen = map[string]string{
	".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg",
	".gif": "image/gif", ".svg": "image/svg+xml", ".webp": "image/webp",
}

func blobURL(nwo, sha, name string) string {
	q := url.Values{}
	q.Set("repo", nwo)
	q.Set("sha", sha)
	q.Set("name", name)
	return "/api/blob?" + q.Encode()
}

// changesAusGitHub liest die Changes aus dem Default-Branch und ueberlagert
// jeden Change mit seinem change/KENNUNG-Branch, wenn der ihn traegt.
func (d *repoDienst) changesAusGitHub(ctx context.Context, c ghrest.Client, r appctx.LeistenRepo) ([]collectors.OpenspecChange, map[string]changeOrt, error) {
	branches, err := c.Branches(ctx, r.NWO)
	if err != nil {
		return nil, nil, err
	}
	standard := r.DefaultBranch
	var kopf string
	aenderungen := map[string]string{} // Kennung -> sha
	for _, b := range branches {
		if kennung, ok := strings.CutPrefix(b.Name, "change/"); ok && kennung != "" {
			aenderungen[kennung] = b.SHA
		}
	}
	for _, name := range []string{standard, "main", "master"} {
		for _, b := range branches {
			if name != "" && b.Name == name && kopf == "" {
				kopf, standard = b.SHA, name
			}
		}
	}
	orte := map[string]changeOrt{}
	if kopf == "" {
		return nil, orte, nil
	}
	remote := map[string]bool{}
	for k := range aenderungen {
		remote[k] = true
	}

	baum, err := d.baum(ctx, c, r.NWO, kopf)
	if err != nil {
		return nil, nil, err
	}
	haupt := neuerBaumLeser(ctx, d, c, r.NWO, baum)
	haupt.vorladen(openspecDatei)
	changes := collectors.OpenspecChangesAus(haupt, remote)
	leser := make([]*baumLeser, len(changes))
	for i := range changes {
		changes[i].Ref = standard
		leser[i] = haupt
	}

	// Die change/*-Branches parallel: je einer ein Baum. Ein Change, den der
	// Default-Branch schon archiviert hat, ist gemergt — sein Branch ist nur
	// nicht geloescht worden und zeigt einen veralteten, scheinbar offenen
	// Stand. Er bleibt draussen.
	archiviert := map[string]bool{}
	for _, c := range changes {
		if c.Archiviert {
			archiviert[c.Kennung] = true
		}
	}
	kennungen := make([]string, 0, len(aenderungen))
	for k := range aenderungen {
		if !archiviert[k] {
			kennungen = append(kennungen, k)
		}
	}
	sort.Strings(kennungen)
	ast := make([]*baumLeser, len(kennungen))
	parallel(len(kennungen), func(i int) {
		b, err := d.baum(ctx, c, r.NWO, aenderungen[kennungen[i]])
		if err != nil {
			return
		}
		l := neuerBaumLeser(ctx, d, c, r.NWO, b)
		ordner := "openspec/changes/" + kennungen[i]
		if !l.ordner[ordner] {
			return
		}
		l.vorladen(func(p string) bool { return strings.HasPrefix(p, ordner+"/") && openspecDatei(p) })
		ast[i] = l
	})
	for i, kennung := range kennungen {
		l := ast[i]
		if l == nil {
			continue
		}
		var neu *collectors.OpenspecChange
		for _, c := range collectors.OpenspecChangesAus(l, remote) {
			if c.Kennung == kennung && !c.Archiviert {
				kopie := c
				neu = &kopie
			}
		}
		if neu == nil {
			continue
		}
		neu.Ref = "change/" + kennung
		ersetzt := false
		for j := range changes {
			if changes[j].Kennung == kennung && !changes[j].Archiviert {
				changes[j], leser[j], ersetzt = *neu, l, true
			}
		}
		if !ersetzt {
			changes = append(changes, *neu)
			leser = append(leser, l)
		}
	}

	// Bild-URLs und Orte.
	for i := range changes {
		l := leser[i]
		for j, b := range changes[i].Belege {
			ext := strings.ToLower(path.Ext(b.Pfad))
			if _, bild := bildTypen[ext]; !bild || b.Fehlt {
				continue
			}
			if sha := l.sha(changes[i].Ordner + "/" + b.Pfad); sha != "" {
				changes[i].Belege[j].Bild = blobURL(r.NWO, sha, path.Base(b.Pfad))
				changes[i].Belege[j].SHA = sha
			}
		}
		kopfSHA := kopf
		if changes[i].Ref != standard {
			kopfSHA = aenderungen[strings.TrimPrefix(changes[i].Ref, "change/")]
		}
		ort := changeOrt{baum: kopfSHA, ordner: changes[i].Ordner, ref: changes[i].Ref, standard: standard, haupt: kopf}
		if alt, ok := orte[changes[i].Kennung]; !ok || (strings.Contains(alt.ordner, "/archive/") && !changes[i].Archiviert) {
			orte[changes[i].Kennung] = ort
		}
	}
	return changes, orte, nil
}

// ---------------------------------------------------------------- Arbeitsliste

func (d *repoDienst) arbeitHolen(ctx context.Context, r appctx.LeistenRepo) (arbeitStand, error) {
	kand := kandidaten(r)
	if len(kand) == 0 {
		return arbeitStand{}, fmt.Errorf("kein PAT gesetzt (%s)", strings.Join(patVariablen, ", "))
	}
	var offen []ghrest.Issue
	var pc patClient
	var err error
	for _, k := range kand {
		offen, err = k.client.OffeneIssues(ctx, r.NWO)
		if err == nil {
			pc = k
			break
		}
		logZeile("repo-arbeit: repo=%s pat=%s fehler=%q", r.NWO, k.pat, err.Error())
	}
	if err != nil {
		return arbeitStand{}, err
	}
	e := appctx.ArbeitEingabe{Repo: r.NWO, Offen: offen, Blocker: map[int][]int{}, ErledigteBlocker: map[int][]ghrest.Issue{}}
	e.Quellen = append(e.Quellen, appctx.ArbeitQuelle{Quelle: "issues"})

	var wg sync.WaitGroup
	var zuErr, osErr, prErr, gpErr, lbErr, aktErr error
	var orte map[string]changeOrt
	wg.Add(6)
	// Die Repo-Aktivitaet: Pushes und Merges ueber alle Branches, aus ihr
	// kommt der letzte Commit (karte-einzeilig, #124). Scheitert sie, gilt
	// der Rueckfall ueber die Branch-Koepfe.
	go func() {
		defer wg.Done()
		e.Aktivitaet, aktErr = pc.client.Aktivitaet(ctx, r.NWO)
	}()
	// Die Labels des Repos: nur wer Rollen-Labels angelegt hat, kennt
	// "Rolle fehlt" (karte-gruppen, #121).
	go func() {
		defer wg.Done()
		e.RepoLabels, lbErr = pc.client.RepoLabels(ctx, r.NWO)
		e.RepoLabelsGeholt = lbErr == nil
	}()
	go func() {
		defer wg.Done()
		e.PRs, prErr = d.prsHolen(ctx, pc.client, r.NWO)
	}()
	// Die zuletzt geschlossenen PRs: aus ihnen kommt der gemergte PR eines
	// Issues fuer den Lebenslauf (pr-lebenslauf, #83).
	go func() {
		defer wg.Done()
		e.GeschlossenePRs, gpErr = pc.client.GeschlossenePRs(ctx, r.NWO)
	}()
	go func() {
		defer wg.Done()
		e.Geschlossen, zuErr = pc.client.GeschlosseneIssues(ctx, r.NWO)
	}()
	go func() {
		defer wg.Done()
		e.Changes, orte, osErr = d.changesAusGitHub(ctx, pc.client, r)
	}()
	// Gefragt wird auch, wer nur geschlossene Blocker hat: sie bleiben als
	// erledigte Eintraege ueber ihm in der Reihe (reihe-erledigte-blocker, #130).
	var mitBlocker []int
	for _, i := range offen {
		if !i.IstPR && (i.Blocker > 0 || i.AlleBlocker > i.Blocker) {
			mitBlocker = append(mitBlocker, i.Nummer)
		}
	}
	// Scheitert die Abfrage eines Issues, fehlen seine Blocker in e.Blocker;
	// RepoArbeitBauen haelt es ueber die Zusammenfassung blockiert, und die
	// Quelle abhaengigkeiten meldet den ersten Grund (karte-blocker-streng, #101).
	blocker := make([][]ghrest.Issue, len(mitBlocker))
	blockerFehler := make([]error, len(mitBlocker))
	parallel(len(mitBlocker), func(i int) {
		blocker[i], blockerFehler[i] = pc.client.Blocker(ctx, r.NWO, mitBlocker[i])
	})
	wg.Wait()
	var depErr error
	for i, nr := range mitBlocker {
		if blockerFehler[i] != nil {
			if depErr == nil {
				depErr = fmt.Errorf("#%d: %v", nr, blockerFehler[i])
			}
			continue
		}
		offenNr := []int{}
		for _, b := range blocker[i] {
			if b.Zustand == "open" {
				offenNr = append(offenNr, b.Nummer)
			} else {
				e.ErledigteBlocker[nr] = append(e.ErledigteBlocker[nr], b)
			}
		}
		e.Blocker[nr] = offenNr
	}
	brErr := d.branchKoepfeHolen(ctx, pc.client, r, &e)
	if aktErr != nil {
		e.Aktivitaet = nil
		logZeile("repo-arbeit: repo=%s aktivitaet fehler=%q (Rueckfall ueber die Branch-Koepfe)", r.NWO, aktErr.Error())
	}
	cmErr := d.letzteCommitsHolen(ctx, pc.client, r.NWO, &e)
	e.Vergleiche = d.vergleicheHolen(ctx, pc.client, r.NWO, e.Aktivitaet)
	if aktErr == nil {
		d.mu.Lock()
		d.aktivitaeten[strings.ToLower(r.NWO)] = aktivitaetStand{akt: e.Aktivitaet, commits: e.Commits, vergleiche: e.Vergleiche}
		d.mu.Unlock()
	}
	d.speicherFuerAktionen(r, &e)
	var tlErr error
	e.Schluesse, tlErr = d.schluesseHolen(ctx, pc.client, r.NWO, appctx.ZuletztAuswahl(e.Geschlossen))
	grund := func(err error) string {
		if err != nil {
			return err.Error()
		}
		return ""
	}
	e.Quellen = append(e.Quellen,
		appctx.ArbeitQuelle{Quelle: "geschlossen", Grund: grund(zuErr)},
		appctx.ArbeitQuelle{Quelle: "openspec", Grund: grund(osErr)},
		appctx.ArbeitQuelle{Quelle: "prs", Grund: grund(prErr)},
		appctx.ArbeitQuelle{Quelle: "prs_geschlossen", Grund: grund(gpErr)},
		appctx.ArbeitQuelle{Quelle: "branches", Grund: grund(brErr)},
		appctx.ArbeitQuelle{Quelle: "abhaengigkeiten", Grund: grund(depErr)},
		appctx.ArbeitQuelle{Quelle: "timeline", Grund: grund(tlErr)},
		appctx.ArbeitQuelle{Quelle: "labels", Grund: grund(lbErr)},
		appctx.ArbeitQuelle{Quelle: "commits", Grund: grund(cmErr)})

	w := appctx.RepoArbeitBauen(e)
	logZeile("repo-arbeit: repo=%s pat=%s quelle=umgebung issues=%d prs=%d changes=%d zuletzt=%d%s",
		r.NWO, pc.pat, len(w.Reihe), len(w.PRs), len(e.Changes), len(w.Zuletzt), fehlerAnhang(zuErr, osErr, brErr, depErr))
	if orte == nil {
		orte = map[string]changeOrt{}
	}
	return arbeitStand{wert: w, orte: orte, changes: e.Changes, pat: pc, zeit: time.Now(),
		geschlossenePRs: e.GeschlossenePRs, schluesse: e.Schluesse}, nil
}

// schluesseHolen holt fuer die Issues von zuletzt die Timeline und je
// closed-Ereignis mit commit_id die PRs des Commits (zuletzt-erledigt-pr,
// #116). Ein Ergebnis wird je Repo, Issue und closed_at gespeichert und dann
// nicht mehr gefragt, solange das Issue in zuletzt steht; ein Fehlschlag wird nicht gespeichert, das Issue fehlt
// in der Antwort, und der erste Grund geht zurueck.
func (d *repoDienst) schluesseHolen(ctx context.Context, c ghrest.Client, nwo string, zuletzt []ghrest.Issue) (map[int]appctx.IssueSchluss, error) {
	out := map[int]appctx.IssueSchluss{}
	schluessel := func(i ghrest.Issue) string {
		return strings.ToLower(nwo) + "#" + strconv.Itoa(i.Nummer) + "@" + i.Geschlossen
	}
	var fehlend []ghrest.Issue
	d.mu.Lock()
	for _, i := range zuletzt {
		if s, ok := d.schluesse[schluessel(i)]; ok {
			out[i.Nummer] = s
		} else {
			fehlend = append(fehlend, i)
		}
	}
	d.mu.Unlock()
	ergebnis := make([]appctx.IssueSchluss, len(fehlend))
	fehler := make([]error, len(fehlend))
	parallel(len(fehlend), func(k int) {
		nr := fehlend[k].Nummer
		es, err := c.IssueTimeline(ctx, nwo, nr)
		if err != nil {
			fehler[k] = fmt.Errorf("#%d: %v", nr, err)
			return
		}
		s := appctx.IssueSchluss{Ereignisse: es, CommitPRs: map[string][]ghrest.PR{}}
		for _, e := range es {
			if e.Ereignis != "closed" || e.CommitID == "" {
				continue
			}
			if _, schon := s.CommitPRs[e.CommitID]; schon {
				continue
			}
			prs, err := c.PRsZuCommit(ctx, nwo, e.CommitID)
			if err != nil {
				fehler[k] = fmt.Errorf("#%d: %v", nr, err)
				return
			}
			s.CommitPRs[e.CommitID] = prs
		}
		ergebnis[k] = s
	})
	var erster error
	d.mu.Lock()
	for k, i := range fehlend {
		if fehler[k] != nil {
			if erster == nil {
				erster = fehler[k]
			}
			continue
		}
		d.schluesse[schluessel(i)] = ergebnis[k]
		out[i.Nummer] = ergebnis[k]
	}
	// Was nicht mehr in zuletzt steht, faellt aus dem Speicher; so bleibt er
	// je Repo bei hoechstens fuenf Eintraegen.
	aktuell := map[string]bool{}
	for _, i := range zuletzt {
		aktuell[schluessel(i)] = true
	}
	for k := range d.schluesse {
		if strings.HasPrefix(k, strings.ToLower(nwo)+"#") && !aktuell[k] {
			delete(d.schluesse, k)
		}
	}
	d.mu.Unlock()
	return out, erster
}

// letzteCommitsHolen traegt in e.Commits die Commits ein, die die Zeile
// "Letzter Commit" braucht (karte-einzeilig, #124): den des juengsten Push
// oder Merge der Aktivitaet, sonst im Rueckfall die Koepfe aus
// appctx.KoepfeFuerRueckfall. Jeder SHA wird je Repo einmal gefragt und
// gespeichert; ein Fehlschlag wird nicht gespeichert, und der erste Grund geht
// zurueck.
func (d *repoDienst) letzteCommitsHolen(ctx context.Context, c ghrest.Client, nwo string, e *appctx.ArbeitEingabe) error {
	var shas []string
	if a, ok := appctx.JuengsteAktivitaet(e.Aktivitaet); ok {
		// Der juengste zuerst, dann die uebrigen Zeilen des Zeitstroms
		// (letzte-aktionen, #126).
		shas = []string{a.Nachher}
		for _, sha := range appctx.AktivitaetsSHAs(e.Aktivitaet) {
			if sha != a.Nachher {
				shas = append(shas, sha)
			}
		}
	} else {
		gesehen := map[string]bool{}
		for _, b := range appctx.KoepfeFuerRueckfall(*e) {
			if !gesehen[b.SHA] {
				gesehen[b.SHA] = true
				shas = append(shas, b.SHA)
			}
		}
	}
	var err error
	e.Commits, err = d.commitsHolen(ctx, c, nwo, shas)
	return err
}

// commitsHolen liefert die Commits zu shas aus dem Speicher je Repo und SHA,
// sonst per commits/{sha}; ein Fehlschlag wird nicht gespeichert, der erste
// Grund geht zurueck. Was fuer das Repo nicht mehr gebraucht wird, faellt aus
// dem Speicher.
func (d *repoDienst) commitsHolen(ctx context.Context, c ghrest.Client, nwo string, shas []string) (map[string]ghrest.CommitKopf, error) {
	praefix := strings.ToLower(nwo) + "@"
	commits := map[string]ghrest.CommitKopf{}
	var fehlend []string
	d.mu.Lock()
	for _, sha := range shas {
		if k, ok := d.commitKoepfe[praefix+strings.ToLower(sha)]; ok {
			commits[sha] = k
		} else {
			fehlend = append(fehlend, sha)
		}
	}
	d.mu.Unlock()
	koepfe := make([]ghrest.CommitKopf, len(fehlend))
	fehler := make([]error, len(fehlend))
	parallel(len(fehlend), func(i int) {
		koepfe[i], fehler[i] = c.Commit(ctx, nwo, fehlend[i])
	})
	var erster error
	d.mu.Lock()
	for i, sha := range fehlend {
		if fehler[i] != nil {
			if erster == nil {
				erster = fmt.Errorf("%s: %v", sha, fehler[i])
			}
			continue
		}
		d.commitKoepfe[praefix+strings.ToLower(sha)] = koepfe[i]
		commits[sha] = koepfe[i]
	}
	// Was nicht mehr gebraucht wird, faellt aus dem Speicher; so bleibt er je
	// Repo bei hoechstens appctx.RueckfallKoepfe Koepfen oder den Zeilen einer
	// Aktivitaet.
	aktuell := map[string]bool{}
	for _, sha := range shas {
		aktuell[praefix+strings.ToLower(sha)] = true
	}
	for k := range d.commitKoepfe {
		if strings.HasPrefix(k, praefix) && !aktuell[k] {
			delete(d.commitKoepfe, k)
		}
	}
	d.mu.Unlock()
	return commits, erster
}

// branchKoepfeHolen traegt die Branches des Repos in die Eingabe ein und
// holt nur fuer die, die an einem offenen Issue haengen, den letzten Commit
// und den Vergleich gegen den Default-Branch parallel (issue-branches, #50).
// Die Branch-Liste hat changesAusGitHub eben erst geholt; der ETag macht den
// zweiten Abruf zu einem 304. Der erste Fehler wird gemeldet, die Branches
// bleiben dann ohne Kopf in der Liste.
func (d *repoDienst) branchKoepfeHolen(ctx context.Context, c ghrest.Client, r appctx.LeistenRepo, e *appctx.ArbeitEingabe) error {
	branches, err := c.Branches(ctx, r.NWO)
	if err != nil {
		return err
	}
	e.Branches = branches
	e.Standard = r.DefaultBranch
	if e.Standard == "" {
		e.Standard = "main"
		for _, b := range branches {
			if b.Name == "master" {
				e.Standard = "master"
			}
		}
		for _, b := range branches {
			if b.Name == "main" {
				e.Standard = "main"
			}
		}
	}
	sha := map[string]string{}
	for _, b := range branches {
		sha[b.Name] = b.SHA
	}
	namen := appctx.VerknuepfteBranches(*e)
	koepfe := make([]appctx.BranchKopf, len(namen))
	fehler := make([]error, len(namen))
	parallel(len(namen), func(i int) {
		k, err := c.Commit(ctx, r.NWO, sha[namen[i]])
		if err != nil {
			fehler[i] = err
			return
		}
		koepfe[i].Zeit, koepfe[i].Betreff = k.Zeit, k.Betreff
		koepfe[i].Voraus, koepfe[i].Zurueck, fehler[i] = c.Vergleich(ctx, r.NWO, e.Standard, sha[namen[i]])
	})
	e.BranchKoepfe = map[string]appctx.BranchKopf{}
	var erster error
	for i, n := range namen {
		if fehler[i] != nil && erster == nil {
			erster = fmt.Errorf("%s: %v", n, fehler[i])
		}
		if koepfe[i].Zeit != "" {
			e.BranchKoepfe[n] = koepfe[i]
		}
	}
	return erster
}

// prsHolen holt die offenen PRs und je PR den Status (mergeable) parallel
// (pr-aktionen, #32). Scheitert die Liste, bleibt sie nil und die PRs kommen
// aus den Issues.
func (d *repoDienst) prsHolen(ctx context.Context, c ghrest.Client, nwo string) ([]ghrest.PR, error) {
	prs, err := c.PullRequests(ctx, nwo)
	if err != nil {
		return nil, err
	}
	parallel(len(prs), func(i int) {
		if s, err := c.PullRequest(ctx, nwo, prs[i].Nummer); err == nil {
			prs[i].Mergebar, prs[i].MergeStatus = s.Mergebar, s.MergeStatus
		}
		// Konfliktzustand (konflikte-anzeigen, #62); ohne Vergleich bleibt
		// der Zustand aus mergeable/mergeable_state stehen.
		_ = c.PRZustandHolen(ctx, nwo, &prs[i])
		// CI-Pruefungen auf dem Head (pr-checks, #80).
		prs[i].Checks = d.checksHolen(ctx, c, nwo, prs[i].HeadSHA)
		// Reviews, Reviewer und Threads (pr-reviews, #81).
		prs[i].Reviews = d.reviewsHolen(ctx, c, nwo, prs[i])
	})
	return prs, nil
}

func fehlerAnhang(errs ...error) string {
	s := ""
	for _, err := range errs {
		if err != nil {
			s += fmt.Sprintf(" fehler=%q", err.Error())
		}
	}
	return s
}

// arbeitStand liefert die Arbeitsliste: frisch aus dem Speicher; abgelaufen
// sofort der alte Stand und genau eine Erneuerung im Hintergrund; ohne Stand
// oder mit frisch synchron (sync-status, #38).
func (d *repoDienst) arbeitStand(ctx context.Context, r appctx.LeistenRepo, frisch bool) (arbeitStand, error) {
	schluessel := strings.ToLower(r.NWO)
	quelle := "arbeit:" + r.NWO
	d.mu.Lock()
	st, ok := d.arbeit[schluessel]
	d.mu.Unlock()
	if ok && !frisch {
		if time.Since(st.zeit) >= d.ttlArbeit && d.syncBeginn(quelle) {
			go func() {
				hg, abbrechen := context.WithTimeout(context.Background(), time.Minute)
				defer abbrechen()
				_, _ = d.arbeitErneuern(hg, r, quelle)
			}()
		}
		return st, nil
	}
	if !d.syncBeginn(quelle) && ok {
		return st, nil
	}
	return d.arbeitErneuern(ctx, r, quelle)
}

func (d *repoDienst) arbeitErneuern(ctx context.Context, r appctx.LeistenRepo, quelle string) (arbeitStand, error) {
	start, vorher := time.Now(), ghrest.StatistikLesen()
	st, err := d.arbeitHolen(ctx, r)
	if err != nil {
		d.syncEnde(quelle, start, vorher, nil, err.Error())
		return st, err
	}
	d.syncEnde(quelle, start, vorher, st.wert, "")
	d.mu.Lock()
	d.arbeit[strings.ToLower(r.NWO)] = st
	d.mu.Unlock()
	return st, nil
}

// verwerfen loescht die zwischengespeicherte Arbeitsliste und Karte eines
// Repos, die Uebersicht, den Stand und das Logbuch ueber alle — nach jedem Schreiben
// (issue-verwaltung, #26), damit die naechste Anfrage den neuen Stand
// synchron holt.
func (d *repoDienst) verwerfen(nwo string) {
	d.mu.Lock()
	delete(d.arbeit, strings.ToLower(nwo))
	delete(d.karten, strings.ToLower(nwo))
	d.uebersicht = nil
	d.repoStand = nil
	d.logbuecher = map[int]*logbuchStand{}
	d.mu.Unlock()
}

// ---------------------------------------------------------------- HTTP

func (d *repoDienst) repoAusAnfrage(w http.ResponseWriter, r *http.Request) (appctx.LeistenRepo, bool) {
	nwo := strings.TrimSpace(r.URL.Query().Get("repo"))
	if !ghrest.GueltigesNWO(nwo) {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"grund": "Parameter repo fehlt oder ist kein owner/repo"})
		return appctx.LeistenRepo{}, false
	}
	repo, ok := d.leiste(r.Context(), false).Repo(nwo)
	if !ok {
		writeJSONStatus(w, http.StatusNotFound, map[string]any{"grund": "unbekanntes Repo " + nwo})
		return appctx.LeistenRepo{}, false
	}
	return repo, true
}

func (d *repoDienst) serveRepos(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, d.leiste(r.Context(), r.URL.Query().Get("frisch") == "1"))
}

func (d *repoDienst) serveArbeit(w http.ResponseWriter, r *http.Request) {
	repo, ok := d.repoAusAnfrage(w, r)
	if !ok {
		return
	}
	frisch := r.URL.Query().Get("frisch") == "1"
	vorher := time.Now()
	st, err := d.arbeitStand(r.Context(), repo, frisch)
	if err != nil {
		// Fail open wie jede Lese-Route: 200 mit Grund, damit die Seite
		// zeigen kann, was fehlt.
		leer := appctx.RepoArbeitBauen(appctx.ArbeitEingabe{Repo: repo.NWO,
			Quellen: []appctx.ArbeitQuelle{{Quelle: "issues", Grund: err.Error()}}})
		leer.Sync = d.syncStand("arbeit:" + repo.NWO)
		writeJSON(w, leer)
		return
	}
	wert := st.wert
	wert.Sync = d.syncStand("arbeit:" + repo.NWO)
	// Der Zeitstrom kommt bei jedem Aufruf aus den aktuellen Speichern; ein
	// gespeicherter Stand stoesst die Aktivitaet im Hintergrund neu an, damit
	// ein eben gemachter Merge beim naechsten Aufruf dasteht (letzte-aktionen, #126).
	wert.LetzteAktionen = d.letzteAktionen(repo, st)
	if !frisch && st.zeit.Before(vorher) {
		d.aktivitaetAnstossen(repo, st.pat)
	}
	writeJSON(w, wert)
}

type specText struct {
	Capability string `json:"capability"`
	Text       string `json:"text"`
}

func (d *repoDienst) serveDetail(w http.ResponseWriter, r *http.Request) {
	repo, ok := d.repoAusAnfrage(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	switch q.Get("art") {
	case "issue":
		nummer, err := strconv.Atoi(q.Get("nummer"))
		if err != nil || nummer <= 0 {
			writeJSONStatus(w, http.StatusBadRequest, map[string]any{"grund": "Parameter nummer fehlt"})
			return
		}
		var letzter error
		for _, k := range kandidaten(repo) {
			i, err := k.client.Issue(r.Context(), repo.NWO, nummer)
			if err != nil {
				letzter = err
				continue
			}
			// Kommentare und Blocker fuer die Bearbeitung (issue-verwaltung,
			// #26); scheitert einer der beiden, fehlt nur er.
			var kommentare []ghrest.Kommentar
			blocker := []int{}
			var wg sync.WaitGroup
			wg.Add(2)
			go func() {
				defer wg.Done()
				if ks, err := k.client.Kommentare(r.Context(), repo.NWO, nummer); err == nil {
					kommentare = ks
				}
			}()
			go func() {
				defer wg.Done()
				if bs, err := k.client.BlockiertDurch(r.Context(), repo.NWO, nummer); err == nil {
					blocker = bs
				}
			}()
			wg.Wait()
			if kommentare == nil {
				kommentare = []ghrest.Kommentar{}
			}
			writeJSON(w, map[string]any{"art": "issue", "nummer": i.Nummer, "titel": i.Titel, "url": i.URL,
				"labels": i.Labels, "zustaendig": i.Zustaendig, "autor": i.Autor, "text": i.Text,
				"zustand": i.Zustand, "aktualisiert": i.Aktualisiert, "geschlossen": i.Geschlossen,
				"faellig": appctx.FaelligAus(i.Text), "meilenstein": i.Meilenstein,
				"kommentare": kommentare, "blockiert_durch": blocker})
			return
		}
		grund := "kein PAT gesetzt"
		if letzter != nil {
			grund = letzter.Error()
		}
		writeJSON(w, map[string]any{"art": "issue", "nummer": nummer, "grund": grund})

	case "change":
		kennung := strings.TrimSpace(q.Get("kennung"))
		st, err := d.arbeitStand(r.Context(), repo, false)
		if err != nil {
			writeJSON(w, map[string]any{"art": "change", "kennung": kennung, "grund": err.Error()})
			return
		}
		ort, ok := st.orte[kennung]
		if !ok {
			writeJSONStatus(w, http.StatusNotFound, map[string]any{"grund": "unbekannter Change " + kennung})
			return
		}
		baum, err := d.baum(r.Context(), st.pat.client, repo.NWO, ort.baum)
		if err != nil {
			writeJSON(w, map[string]any{"art": "change", "kennung": kennung, "grund": err.Error()})
			return
		}
		l := neuerBaumLeser(r.Context(), d, st.pat.client, repo.NWO, baum)
		l.vorladen(func(p string) bool {
			return strings.HasPrefix(p, ort.ordner+"/") && strings.HasSuffix(p, ".md")
		})
		lies := func(name string) string {
			b, err := l.Lesen(ort.ordner + "/" + name)
			if err != nil {
				return ""
			}
			return string(b)
		}
		specs := []specText{}
		for _, cap := range l.Ordner(ort.ordner + "/specs") {
			if t := lies("specs/" + cap + "/spec.md"); t != "" {
				specs = append(specs, specText{Capability: cap, Text: t})
			}
		}
		var belege []collectors.OpenspecBeleg
		for _, e := range append(append(append([]appctx.ArbeitsEintrag{}, st.wert.Reihe...), st.wert.Zuletzt...), st.wert.OhneIssue...) {
			if e.Change != nil && e.Change.Kennung == kennung {
				belege = e.Change.Belege
				break
			}
		}
		if belege == nil {
			belege = []collectors.OpenspecBeleg{}
		}
		writeJSON(w, map[string]any{"art": "change", "kennung": kennung, "titel": kennung, "ref": ort.ref,
			"proposal": lies("proposal.md"), "tasks": lies("tasks.md"), "design": lies("design.md"),
			"specs": specs, "belege": belege,
			"abgleich": d.changeAbgleich(r.Context(), st.pat.client, repo.NWO, kennung, ort, l, lies("proposal.md"), lies("tasks.md"), specs)})

	default:
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"grund": "Parameter art muss issue oder change sein"})
	}
}

// serveRepoMeta liefert Labels und moegliche Zustaendige eines Repos fuer die
// Auswahl in der Oberflaeche (issue-verwaltung, #26).
func (d *repoDienst) serveRepoMeta(w http.ResponseWriter, r *http.Request) {
	repo, ok := d.repoAusAnfrage(w, r)
	if !ok {
		return
	}
	kand := kandidaten(repo)
	if len(kand) == 0 {
		writeJSON(w, map[string]any{"labels": []ghrest.Label{}, "zustaendige": []string{}, "grund": "kein PAT gesetzt"})
		return
	}
	c := kand[0].client
	labels, errL := c.Labels(r.Context(), repo.NWO)
	leute, errZ := c.MoeglicheZustaendige(r.Context(), repo.NWO)
	// Milestones fuer Issue und PR (pr-merge-optionen, #82).
	meilensteine, errM := c.Meilensteine(r.Context(), repo.NWO)
	if labels == nil {
		labels = []ghrest.Label{}
	}
	if leute == nil {
		leute = []string{}
	}
	if meilensteine == nil {
		meilensteine = []ghrest.Meilenstein{}
	}
	antwort := map[string]any{"labels": labels, "zustaendige": leute, "meilensteine": meilensteine}
	if err := errors.Join(errL, errZ, errM); err != nil {
		antwort["grund"] = err.Error()
	}
	writeJSON(w, antwort)
}

func (d *repoDienst) serveBlob(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	typ, ok := bildTypen[strings.ToLower(path.Ext(q.Get("name")))]
	if !ok {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"grund": "nur Bilder (png, jpg, gif, svg, webp)"})
		return
	}
	repo, ok := d.repoAusAnfrage(w, r)
	if !ok {
		return
	}
	var letzter error
	for _, k := range kandidaten(repo) {
		b, err := d.blob(r.Context(), k.client, repo.NWO, q.Get("sha"))
		if err != nil {
			letzter = err
			continue
		}
		w.Header().Set("Content-Type", typ)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "private, max-age=86400, immutable")
		if typ == "image/svg+xml" {
			w.Header().Set("Content-Security-Policy", "sandbox")
		}
		_, _ = w.Write(b)
		return
	}
	grund := "kein PAT gesetzt"
	if letzter != nil {
		grund = letzter.Error()
	}
	writeJSONStatus(w, http.StatusBadGateway, map[string]any{"grund": grund})
}

// ---------------------------------------------------------------- Letzte Aktionen

// speicherFuerAktionen traegt die Speicher der Abschnitte CI, "Geaenderte
// Bilder" und "HTML-Dateien" in die Eingabe ein, soweit sie vorliegen; es
// wird nichts geholt (letzte-aktionen, #126).
func (d *repoDienst) speicherFuerAktionen(r appctx.LeistenRepo, e *appctx.ArbeitEingabe) {
	schluessel := strings.ToLower(r.NWO)
	nwo := r.NWO
	e.BildURL = func(sha, name string) string { return blobURL(nwo, sha, name) }
	if st := d.ciStand(r.NWO); st != nil {
		e.CIRuns = st.Runs
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if g, ok := d.letzteBilder[schluessel]; ok {
		e.Bilder = &g
	}
	if h, ok := d.letzteHTML[schluessel]; ok {
		e.HTML = &h
	}
}

// letzteAktionen setzt den Zeitstrom aus den aktuellen Speichern zusammen:
// der Aktivitaet samt Commits, CI, Bildern, HTML-Dateien, den Changes und
// zuletzt der Arbeitsliste st, gegen anzeigezeit.Jetzt().
func (d *repoDienst) letzteAktionen(r appctx.LeistenRepo, st arbeitStand) []appctx.LetzteAktion {
	e := appctx.ArbeitEingabe{Repo: r.NWO, Standard: r.DefaultBranch, Changes: st.changes, GeschlossenePRs: st.geschlossenePRs}
	d.mu.Lock()
	if a, ok := d.aktivitaeten[strings.ToLower(r.NWO)]; ok {
		e.Aktivitaet, e.Commits, e.Vergleiche = a.akt, a.commits, a.vergleiche
	}
	d.mu.Unlock()
	d.speicherFuerAktionen(r, &e)
	return appctx.LetzteAktionenBauen(appctx.AktionenAusArbeit(e, st.wert, anzeigezeit.Jetzt()))
}

// aktivitaetQuelle ist die Quelle der Sync-Buchfuehrung fuer die Aktivitaet.
func aktivitaetQuelle(nwo string) string { return "aktivitaet:" + nwo }

// aktivitaetAnstossen erneuert die Aktivitaet eines Repos im Hintergrund,
// unabhaengig vom Speicher der Arbeitsliste; laeuft schon eine Erneuerung,
// geschieht nichts. Nur ein Erfolg ersetzt den Speicher.
func (d *repoDienst) aktivitaetAnstossen(r appctx.LeistenRepo, pc patClient) {
	quelle := aktivitaetQuelle(r.NWO)
	if !d.syncBeginn(quelle) {
		return
	}
	go func() {
		ctx, abbrechen := context.WithTimeout(context.Background(), time.Minute)
		defer abbrechen()
		start, vorher := time.Now(), ghrest.StatistikLesen()
		akt, err := pc.client.Aktivitaet(ctx, r.NWO)
		if err != nil {
			logZeile("aktivitaet: repo=%s pat=%s fehler=%q", r.NWO, pc.pat, err.Error())
			d.syncEnde(quelle, start, vorher, nil, err.Error())
			return
		}
		commits, cmErr := d.commitsHolen(ctx, pc.client, r.NWO, appctx.AktivitaetsSHAs(akt))
		vergleiche := d.vergleicheHolen(ctx, pc.client, r.NWO, akt)
		d.mu.Lock()
		d.aktivitaeten[strings.ToLower(r.NWO)] = aktivitaetStand{akt: akt, commits: commits, vergleiche: vergleiche}
		d.mu.Unlock()
		logZeile("aktivitaet: repo=%s pat=%s eintraege=%d%s", r.NWO, pc.pat, len(akt), fehlerAnhang(cmErr))
		d.syncEnde(quelle, start, vorher, akt, "")
	}()
}

// vergleicheHolen liefert je appctx.VergleichSchluessel die Vergleiche der
// Pushes der Aktivitaet (aktionen-zeitleiste, #128): aus dem Speicher je Repo
// und Paar, sonst per compare/{before}...{after}, parallel. Ein Fehlschlag
// wird nicht gespeichert, nur protokolliert; der Push bleibt ohne Commits.
func (d *repoDienst) vergleicheHolen(ctx context.Context, c ghrest.Client, nwo string, akt []ghrest.Aktivitaet) map[string]ghrest.PushVergleich {
	praefix := strings.ToLower(nwo) + "@"
	out := map[string]ghrest.PushVergleich{}
	var fehlend [][2]string
	d.mu.Lock()
	for _, p := range appctx.VergleichsPaare(akt) {
		schluessel := appctx.VergleichSchluessel(p[0], p[1])
		if v, ok := d.vergleiche[praefix+strings.ToLower(schluessel)]; ok {
			out[schluessel] = v
		} else {
			fehlend = append(fehlend, p)
		}
	}
	d.mu.Unlock()
	geholt := make([]ghrest.PushVergleich, len(fehlend))
	fehler := make([]error, len(fehlend))
	parallel(len(fehlend), func(i int) {
		geholt[i], fehler[i] = c.VergleichMitCommits(ctx, nwo, fehlend[i][0], fehlend[i][1])
	})
	d.mu.Lock()
	defer d.mu.Unlock()
	for i, p := range fehlend {
		schluessel := appctx.VergleichSchluessel(p[0], p[1])
		if fehler[i] != nil {
			logZeile("vergleich: repo=%s paar=%s fehler=%q", nwo, schluessel, fehler[i].Error())
			continue
		}
		if len(d.vergleiche) >= vergleichDeckel {
			d.vergleiche = map[string]ghrest.PushVergleich{}
		}
		d.vergleiche[praefix+strings.ToLower(schluessel)] = geholt[i]
		out[schluessel] = geholt[i]
	}
	return out
}
