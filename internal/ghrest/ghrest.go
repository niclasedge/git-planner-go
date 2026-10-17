// Package ghrest spricht die GitHub-REST-API direkt ueber net/http
// (repo-leiste, #25).
//
// Warum nicht ueber gh wie die GitHub-Karte der alten Ansicht: gh holt Issues
// und PRs ueber GraphQL, und GraphQL ist nicht ueberall freigegeben (die
// Abnahme vom 2026-09-24 lief genau daran auf). REST braucht nur den PAT.
//
// Das Paket ist ein Blatt: es kennt weder Projekte noch Kategorien, nur
// Endpunkte. Der Tokenwert verlaesst es nie — jede Fehlermeldung wird
// bereinigt, bevor sie zurueckgeht.
package ghrest

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Basis ist der Anfang jeder URL: GKP_GITHUB_API, sonst api.github.com. Die
// Variable ist der Einstiegspunkt fuer Tests (eine Fake-API) und fuer GitHub
// Enterprise.
func Basis() string {
	if b := strings.TrimRight(strings.TrimSpace(os.Getenv("GKP_GITHUB_API")), "/"); b != "" {
		return b
	}
	return "https://api.github.com"
}

// maxSeiten deckelt jede Seitenfolge. 100 je Seite mal 10 sind 1000 Repos
// oder Issues — mehr zeigt keine Leiste sinnvoll, und ein Link-Kopf, der im
// Kreis zeigt, haelt den Dienst nicht fest.
const maxSeiten = 10

// Zeitbudget eines einzelnen HTTP-Aufrufs.
const aufrufBudget = 20 * time.Second

// Label ist ein Issue-Label mit seiner Farbe (hex ohne #).
type Label struct {
	Name  string `json:"name"`
	Farbe string `json:"farbe"`
}

// Repo ist ein Eintrag aus /user/repos.
type Repo struct {
	NWO           string `json:"nwo"`
	Name          string `json:"name"`
	Owner         string `json:"owner"`
	Beschreibung  string `json:"beschreibung,omitempty"`
	Sprache       string `json:"sprache,omitempty"`
	DefaultBranch string `json:"default_branch,omitempty"`
	Gepusht       string `json:"gepusht,omitempty"`
	URL           string `json:"url,omitempty"`
	Privat        bool   `json:"privat"`
	Archiviert    bool   `json:"archiviert"`
	// OffeneIssues ist open_issues_count — bei GitHub zaehlen PRs mit.
	OffeneIssues int `json:"offene_issues"`
}

// Issue ist ein Issue oder ein Pull Request aus /repos/{nwo}/issues.
type Issue struct {
	Nummer       int      `json:"nummer"`
	Titel        string   `json:"titel"`
	URL          string   `json:"url"`
	Autor        string   `json:"autor,omitempty"`
	Labels       []Label  `json:"labels"`
	Zustaendig   []string `json:"zustaendig"`
	Kommentare   int      `json:"kommentare"`
	Aktualisiert string   `json:"aktualisiert,omitempty"`
	Geschlossen  string   `json:"geschlossen,omitempty"`
	// Text ist der Rumpf (Markdown). In Listen reist er nicht mit — dort
	// genuegt HatText; die Detailspalte holt ihn einzeln.
	Text string `json:"-"`
	// Blocker ist issue_dependencies_summary.blocked_by: wie viele Issues
	// dieses blockieren. Nur wer mehr als 0 hat, wird nach ihnen gefragt.
	Blocker int  `json:"-"`
	IstPR   bool `json:"-"`
	// Erstellt ist created_at (repo-karte, #36).
	Erstellt string `json:"erstellt,omitempty"`
	// SubGesamt/SubErledigt sind sub_issues_summary: ein Issue mit
	// SubGesamt > 0 ist eine Map.
	SubGesamt   int `json:"-"`
	SubErledigt int `json:"-"`
	// ParentNummer ist die Nummer des Parent-Issues (0 = keins).
	ParentNummer int `json:"-"`
	// AlleBlocker ist total_blocked_by, auch geschlossene Blocker.
	AlleBlocker int `json:"-"`
	// Blockt und AlleBlockt sind blocking und total_blocking: wie viele
	// offene und wie viele Issues insgesamt dieses blockiert
	// (aktionen-zeitleiste, #128).
	Blockt     int `json:"-"`
	AlleBlockt int `json:"-"`
	// Zustand ist "open" oder "closed".
	Zustand string `json:"zustand,omitempty"`
	// ID ist die Datenbank-Id — die Abhaengigkeits-API will sie statt der
	// Nummer.
	ID int64 `json:"-"`
	// Meilenstein ist der Milestone des Issues (pr-merge-optionen, #82).
	Meilenstein *Meilenstein `json:"meilenstein,omitempty"`
}

// Branch ist ein Branch mit dem sha seines Kopfes. Geschuetzt ist GitHubs
// protected (branch-aufraeumen, #78).
type Branch struct {
	Name       string `json:"name"`
	SHA        string `json:"sha"`
	Geschuetzt bool   `json:"geschuetzt,omitempty"`
}

// BaumEintrag ist ein Eintrag aus git/trees?recursive=1.
type BaumEintrag struct {
	Pfad    string
	Typ     string // "blob" oder "tree"
	SHA     string
	Groesse int64
	// Modus ist der Git-Modus (100644, 100755, 120000 …), #75.
	Modus string
}

// Client ist ein Zugang mit genau einem Token.
type Client struct {
	Token string
	HTTP  *http.Client
}

func (c Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

// nwoRe laesst genau owner/repo zu — keine Pfadsegmente, kein "..".
var nwoRe = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

// GueltigesNWO sagt, ob s die Form owner/repo hat.
func GueltigesNWO(s string) bool {
	return nwoRe.MatchString(s) && !strings.Contains(s, "..")
}

var shaRe = regexp.MustCompile(`^[0-9A-Za-z_-]{1,64}$`)

// bereinigen ersetzt jedes Vorkommen des Tokens.
func (c Client) bereinigen(s string) string {
	if c.Token != "" {
		s = strings.ReplaceAll(s, c.Token, "***")
	}
	return s
}

// holen fragt eine URL an und liefert Rumpf und Kopf. Ein Status ausserhalb
// 2xx wird zu "GitHub <status>: <message>".
func (c Client) holen(ctx context.Context, adresse string) ([]byte, http.Header, error) {
	return c.anfrage(ctx, http.MethodGet, adresse, nil)
}

// anfrage schickt eine Anfrage mit optionalem JSON-Rumpf (issue-verwaltung,
// #26: dieselbe Fehler- und Token-Behandlung fuer Lesen und Schreiben).
func (c Client) anfrage(ctx context.Context, methode, adresse string, rumpf any) ([]byte, http.Header, error) {
	ctx, abbrechen := context.WithTimeout(ctx, aufrufBudget)
	defer abbrechen()
	var koerper io.Reader
	if rumpf != nil {
		b, err := json.Marshal(rumpf)
		if err != nil {
			return nil, nil, err
		}
		koerper = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, methode, adresse, koerper)
	if err != nil {
		return nil, nil, err
	}
	if rumpf != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	// Bedingter Abruf (sync-status, #38): nur GET, nur mit gemerktem ETag.
	lesen := methode == http.MethodGet && rumpf == nil
	schluessel := speicherSchluessel(c.Token, adresse)
	var alt gemerkt
	var hatAlt bool
	if lesen {
		if alt, hatAlt = speicherLesen(schluessel); hatAlt && alt.etag != "" {
			req.Header.Set("If-None-Match", alt.etag)
		}
	}
	zaehlerAufrufe.Add(1)
	resp, err := c.http().Do(req)
	if err != nil {
		zaehlerFehler.Add(1)
		return nil, nil, errors.New(c.bereinigen(err.Error()))
	}
	defer resp.Body.Close()
	rateMerken(resp.Header)
	if lesen && hatAlt && resp.StatusCode == http.StatusNotModified {
		zaehlerUnveraendert.Add(1)
		return alt.body, alt.kopf, nil
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		zaehlerFehler.Add(1)
		return nil, nil, errors.New(c.bereinigen(err.Error()))
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		zaehlerFehler.Add(1)
		var m struct {
			Message string `json:"message"`
			// Errors nennt bei 422 den eigentlichen Grund (pr-anlegen, #79).
			Errors []struct {
				Message string `json:"message"`
			} `json:"errors"`
		}
		_ = json.Unmarshal(b, &m)
		text := strings.TrimSpace(m.Message)
		if len(m.Errors) > 0 && strings.TrimSpace(m.Errors[0].Message) != "" {
			text = strings.TrimPrefix(text+": "+strings.TrimSpace(m.Errors[0].Message), ": ")
		}
		if text == "" {
			text = http.StatusText(resp.StatusCode)
		}
		return nil, nil, StatusFehler{Status: resp.StatusCode, text: c.bereinigen(fmt.Sprintf("GitHub %d: %s", resp.StatusCode, text))}
	}
	if lesen {
		if etag := resp.Header.Get("ETag"); etag != "" {
			kopf := http.Header{}
			if l := resp.Header.Get("Link"); l != "" {
				kopf.Set("Link", l)
			}
			speicherSchreiben(schluessel, gemerkt{etag: etag, body: b, kopf: kopf, zeit: time.Now()})
		}
	}
	return b, resp.Header, nil
}

// StatusFehler ist eine Antwort ausserhalb 2xx: der Text ist bereinigt
// ("GitHub <status>: <message>"), Status der HTTP-Status (#75).
type StatusFehler struct {
	Status int
	text   string
}

func (e StatusFehler) Error() string { return e.text }

// HTTPStatus ist der Status einer abgelehnten Antwort, sonst 0.
func HTTPStatus(err error) int {
	var s StatusFehler
	if errors.As(err, &s) {
		return s.Status
	}
	return 0
}

var naechsteRe = regexp.MustCompile(`<([^>]+)>;\s*rel="next"`)

// seiten folgt dem Link-Kopf und haengt jede Seite an out.
func seiten[T any](ctx context.Context, c Client, adresse string) ([]T, error) {
	var out []T
	for i := 0; i < maxSeiten && adresse != ""; i++ {
		b, kopf, err := c.holen(ctx, adresse)
		if err != nil {
			return nil, err
		}
		var seite []T
		if err := json.Unmarshal(b, &seite); err != nil {
			return nil, fmt.Errorf("GitHub: unlesbare Antwort: %v", err)
		}
		out = append(out, seite...)
		adresse = ""
		if m := naechsteRe.FindStringSubmatch(kopf.Get("Link")); m != nil {
			adresse = m[1]
		}
	}
	return out, nil
}

type rohRepo struct {
	FullName    string                 `json:"full_name"`
	Name        string                 `json:"name"`
	Owner       struct{ Login string } `json:"owner"`
	Description string                 `json:"description"`
	Language    string                 `json:"language"`
	Default     string                 `json:"default_branch"`
	PushedAt    string                 `json:"pushed_at"`
	HTMLURL     string                 `json:"html_url"`
	Private     bool                   `json:"private"`
	Archived    bool                   `json:"archived"`
	OpenIssues  int                    `json:"open_issues_count"`
}

// Repos liefert alle Repos, die das Token sieht (eigene, als Mitarbeiter,
// ueber eine Organisation).
func (c Client) Repos(ctx context.Context) ([]Repo, error) {
	roh, err := seiten[rohRepo](ctx, c, Basis()+"/user/repos?per_page=100&affiliation=owner,collaborator,organization_member&sort=pushed")
	if err != nil {
		return nil, err
	}
	out := make([]Repo, 0, len(roh))
	for _, r := range roh {
		out = append(out, Repo{NWO: r.FullName, Name: r.Name, Owner: r.Owner.Login, Beschreibung: r.Description,
			Sprache: r.Language, DefaultBranch: r.Default, Gepusht: r.PushedAt, URL: r.HTMLURL,
			Privat: r.Private, Archiviert: r.Archived, OffeneIssues: r.OpenIssues})
	}
	return out, nil
}

type rohIssue struct {
	ID      int64                  `json:"id"`
	State   string                 `json:"state"`
	Number  int                    `json:"number"`
	Title   string                 `json:"title"`
	HTMLURL string                 `json:"html_url"`
	Body    string                 `json:"body"`
	User    struct{ Login string } `json:"user"`
	Labels  []struct {
		Name  string `json:"name"`
		Color string `json:"color"`
	} `json:"labels"`
	Assignees []struct{ Login string } `json:"assignees"`
	Comments  int                      `json:"comments"`
	UpdatedAt string                   `json:"updated_at"`
	ClosedAt  string                   `json:"closed_at"`
	PR        *json.RawMessage         `json:"pull_request"`
	Deps      struct {
		BlockedBy      int `json:"blocked_by"`
		TotalBlockedBy int `json:"total_blocked_by"`
		Blocking       int `json:"blocking"`
		TotalBlocking  int `json:"total_blocking"`
	} `json:"issue_dependencies_summary"`
	CreatedAt string `json:"created_at"`
	Subs      struct {
		Total     int `json:"total"`
		Completed int `json:"completed"`
	} `json:"sub_issues_summary"`
	ParentURL string          `json:"parent_issue_url"`
	Milestone *rohMeilenstein `json:"milestone"`
}

func (r rohIssue) issue() Issue {
	i := Issue{Nummer: r.Number, Titel: r.Title, URL: r.HTMLURL, Autor: r.User.Login, Text: r.Body,
		Labels: []Label{}, Zustaendig: []string{}, Kommentare: r.Comments, Aktualisiert: r.UpdatedAt,
		Geschlossen: r.ClosedAt, Blocker: r.Deps.BlockedBy, IstPR: r.PR != nil, ID: r.ID, Zustand: r.State,
		Erstellt: r.CreatedAt, SubGesamt: r.Subs.Total, SubErledigt: r.Subs.Completed, AlleBlocker: r.Deps.TotalBlockedBy,
		Blockt: r.Deps.Blocking, AlleBlockt: r.Deps.TotalBlocking}
	if n := strings.LastIndex(r.ParentURL, "/"); n >= 0 && r.ParentURL != "" {
		i.ParentNummer, _ = strconv.Atoi(r.ParentURL[n+1:])
	}
	for _, l := range r.Labels {
		i.Labels = append(i.Labels, Label{Name: l.Name, Farbe: l.Color})
	}
	for _, a := range r.Assignees {
		i.Zustaendig = append(i.Zustaendig, a.Login)
	}
	i.Meilenstein = r.Milestone.meilenstein()
	return i
}

func (c Client) issues(ctx context.Context, nwo, abfrage string) ([]Issue, error) {
	if !GueltigesNWO(nwo) {
		return nil, fmt.Errorf("ungueltiges Repo %q (erwartet owner/repo)", nwo)
	}
	roh, err := seiten[rohIssue](ctx, c, Basis()+"/repos/"+nwo+"/issues?"+abfrage)
	if err != nil {
		return nil, err
	}
	out := make([]Issue, 0, len(roh))
	for _, r := range roh {
		out = append(out, r.issue())
	}
	return out, nil
}

// OffeneIssues liefert die offenen Issues samt PRs (IstPR) des Repos.
func (c Client) OffeneIssues(ctx context.Context, nwo string) ([]Issue, error) {
	return c.issues(ctx, nwo, "state=open&per_page=100")
}

// GeschlosseneIssues liefert die zuletzt geaenderten geschlossenen Issues ohne
// PRs, nach closed_at absteigend. Eine Seite genuegt: gebraucht werden die
// letzten fuenf, und ein spaet noch kommentiertes altes Issue verdraengt so
// hoechstens eines davon.
func (c Client) GeschlosseneIssues(ctx context.Context, nwo string) ([]Issue, error) {
	if !GueltigesNWO(nwo) {
		return nil, fmt.Errorf("ungueltiges Repo %q (erwartet owner/repo)", nwo)
	}
	b, _, err := c.holen(ctx, Basis()+"/repos/"+nwo+"/issues?state=closed&sort=updated&direction=desc&per_page=50")
	if err != nil {
		return nil, err
	}
	var roh []rohIssue
	if err := json.Unmarshal(b, &roh); err != nil {
		return nil, fmt.Errorf("GitHub: unlesbare Antwort: %v", err)
	}
	out := []Issue{}
	for _, r := range roh {
		if r.PR == nil {
			out = append(out, r.issue())
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Geschlossen > out[j].Geschlossen })
	return out, nil
}

// Issue liefert ein einzelnes Issue mit Text.
func (c Client) Issue(ctx context.Context, nwo string, nummer int) (Issue, error) {
	if !GueltigesNWO(nwo) {
		return Issue{}, fmt.Errorf("ungueltiges Repo %q (erwartet owner/repo)", nwo)
	}
	b, _, err := c.holen(ctx, fmt.Sprintf("%s/repos/%s/issues/%d", Basis(), nwo, nummer))
	if err != nil {
		return Issue{}, err
	}
	var r rohIssue
	if err := json.Unmarshal(b, &r); err != nil {
		return Issue{}, fmt.Errorf("GitHub: unlesbare Antwort: %v", err)
	}
	return r.issue(), nil
}

// Blocker liefert die offenen und geschlossenen Issues desselben Repos, die
// nummer blockieren oder blockiert haben, nach Nummer (reihe-erledigte-blocker,
// #130). Blocker aus anderen Repos haben in der Reihenfolge dieses Repos keinen
// Platz und fallen weg.
func (c Client) Blocker(ctx context.Context, nwo string, nummer int) ([]Issue, error) {
	if !GueltigesNWO(nwo) {
		return nil, fmt.Errorf("ungueltiges Repo %q (erwartet owner/repo)", nwo)
	}
	b, _, err := c.holen(ctx, fmt.Sprintf("%s/repos/%s/issues/%d/dependencies/blocked_by?per_page=100", Basis(), nwo, nummer))
	if err != nil {
		return nil, err
	}
	var roh []struct {
		rohIssue
		RepositoryURL string `json:"repository_url"`
	}
	if err := json.Unmarshal(b, &roh); err != nil {
		return nil, fmt.Errorf("GitHub: unlesbare Antwort: %v", err)
	}
	out := []Issue{}
	for _, r := range roh {
		if r.RepositoryURL != "" && !strings.HasSuffix(strings.ToLower(r.RepositoryURL), "/repos/"+strings.ToLower(nwo)) {
			continue
		}
		out = append(out, r.issue())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Nummer < out[j].Nummer })
	return out, nil
}

// BlockiertDurch liefert die Nummern der OFFENEN Issues desselben Repos, die
// nummer blockieren.
func (c Client) BlockiertDurch(ctx context.Context, nwo string, nummer int) ([]int, error) {
	alle, err := c.Blocker(ctx, nwo, nummer)
	if err != nil {
		return nil, err
	}
	out := []int{}
	for _, i := range alle {
		if i.Zustand == "open" {
			out = append(out, i.Nummer)
		}
	}
	return out, nil
}

// Branches liefert die Branches mit dem sha ihres Kopfes.
func (c Client) Branches(ctx context.Context, nwo string) ([]Branch, error) {
	if !GueltigesNWO(nwo) {
		return nil, fmt.Errorf("ungueltiges Repo %q (erwartet owner/repo)", nwo)
	}
	roh, err := seiten[struct {
		Name   string `json:"name"`
		Commit struct {
			SHA string `json:"sha"`
		} `json:"commit"`
		Protected bool `json:"protected"`
	}](ctx, c, Basis()+"/repos/"+nwo+"/branches?per_page=100")
	if err != nil {
		return nil, err
	}
	out := make([]Branch, 0, len(roh))
	for _, r := range roh {
		out = append(out, Branch{Name: r.Name, SHA: r.Commit.SHA, Geschuetzt: r.Protected})
	}
	return out, nil
}

// Baum liefert den ganzen Baum zu einem Commit- oder Baum-sha.
func (c Client) Baum(ctx context.Context, nwo, sha string) ([]BaumEintrag, error) {
	if !GueltigesNWO(nwo) || !shaRe.MatchString(sha) {
		return nil, fmt.Errorf("ungueltiger Baum %q in %q", sha, nwo)
	}
	b, _, err := c.holen(ctx, Basis()+"/repos/"+nwo+"/git/trees/"+url.PathEscape(sha)+"?recursive=1")
	if err != nil {
		return nil, err
	}
	var roh struct {
		Tree []struct {
			Path string `json:"path"`
			Type string `json:"type"`
			SHA  string `json:"sha"`
			Size int64  `json:"size"`
			Mode string `json:"mode"`
		} `json:"tree"`
	}
	if err := json.Unmarshal(b, &roh); err != nil {
		return nil, fmt.Errorf("GitHub: unlesbarer Baum: %v", err)
	}
	out := make([]BaumEintrag, 0, len(roh.Tree))
	for _, e := range roh.Tree {
		out = append(out, BaumEintrag{Pfad: e.Path, Typ: e.Type, SHA: e.SHA, Groesse: e.Size, Modus: e.Mode})
	}
	return out, nil
}

// Blob liefert den Inhalt eines Blobs. Ein Blob ist ueber seinen sha
// unveraenderlich; zwischenspeichern darf der Aufrufer also ohne Ablauf.
func (c Client) Blob(ctx context.Context, nwo, sha string) ([]byte, error) {
	if !GueltigesNWO(nwo) || !shaRe.MatchString(sha) {
		return nil, fmt.Errorf("ungueltiger Blob %q in %q", sha, nwo)
	}
	b, _, err := c.holen(ctx, Basis()+"/repos/"+nwo+"/git/blobs/"+url.PathEscape(sha))
	if err != nil {
		return nil, err
	}
	var roh struct {
		Encoding string `json:"encoding"`
		Content  string `json:"content"`
	}
	if err := json.Unmarshal(b, &roh); err != nil {
		return nil, fmt.Errorf("GitHub: unlesbarer Blob: %v", err)
	}
	if roh.Encoding != "base64" {
		return []byte(roh.Content), nil
	}
	inhalt, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(roh.Content, "\n", ""))
	if err != nil {
		return nil, fmt.Errorf("GitHub: Blob nicht dekodierbar: %v", err)
	}
	return inhalt, nil
}
