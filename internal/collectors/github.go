package collectors

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/niclasedge/git-planner-go/internal/shared"
)

// GitHub-Karte: offene Issues und Pull Requests des origin-Remotes.
//
// Gesprochen wird direkt mit der GraphQL-API von GitHub, ohne die gh-CLI:
// der Dienst laeuft im Container, und dort gibt es weder gh noch einen
// Keyring. Das Token kommt ausschliesslich aus der Umgebung
// (GITHUB_PAT_PERSONAL bzw. GITHUB_PAT_WORK, GitHub-Issue #2). Es geht nur in
// den Authorization-Header des einen Requests, wird nie geloggt, nie in eine
// Datei geschrieben und nie in die Antwort aufgenommen.
//
// Welches Token ein Repo sehen darf, entscheiden Besitzer und Remote-Host.
// Mit dem falschen Token antwortet die API nicht mit "verboten", sondern mit
// "Could not resolve to a Repository" — gemessen an example-org/orbit. Ein
// falsches Mapping sieht also aus wie ein geloeschtes Repo, deshalb steht die
// Zuordnung an genau einer Stelle (ghAccounts, ghPATVar).
//
// Die Karte laeuft NICHT in BuildContext mit, sondern hinter GET /github:
// zwei Netzaufrufe je Projekt wuerden das Oeffnen eines Projekts weiter
// verlangsamen. Sie wird geholt, wenn jemand sie ansieht, und ist fail-open —
// ohne Netz oder ohne Token rendert die Seite weiter, die Karte nennt den Grund.

// ghTimeoutMs begrenzt einen einzelnen API-Aufruf. Netz plus API-Latenz, aber
// nicht so lang, dass ein haengender Aufruf die Ansicht blockiert. Variabel
// statt konstant aus demselben Grund wie shared.BdTimeoutMs: ein Test, der
// beweist, dass die Frist greift, darf nicht 15 s dauern.
var ghTimeoutMs = 15_000

// ghListLimit ist die Obergrenze je PR-Liste. Fuer Pull Requests bleibt die
// Karte eine Uebersicht — wer mehr braucht, klickt auf GitHub. Fuer Issues
// gilt das seit gkp-8zx NICHT mehr: ohne die geschlossenen gibt es keinen
// Fortschritt, und ein Deckel von 30 haette bei 209 Issues 179 verschwiegen.
const ghListLimit = 30

// Grenzen der Issue-Abfrage. Gemessen 2026-09-13 gegen example-user/infra
// (209 Issues, das groesste Repo auf dieser Maschine): eine Seite zu 100
// Knoten mit parent, subIssues und subIssuesSummary kostete 1,3-1,9 s, der
// ganze Lauf ueber drei Seiten 3,3-3,9 s.
//
// Daraus die Fristen: ghIssuesBudget deckt den gemessenen Lauf gut siebenfach
// ab und reicht bei gemessener Geschwindigkeit fuer rund 1500 Issues; der
// Deckel ghIssuePageCap zieht bei 2000 eine harte Grenze, damit ein Repo mit
// zehntausend Issues die Karte nicht in einen Dauerlauf schickt. Was zuerst
// greift, ist egal — beide Wege enden mit Truncated=true und einer Notiz,
// nie mit einer gekuerzten Liste, die wie eine vollstaendige aussieht.
const (
	ghIssuePageSize = 100
	ghIssuePageCap  = 20
)

var ghIssuesBudget = 30 * time.Second

// ghIssuesReserve ist der Rest, den eine weitere Seite im Budget haben muss.
// Etwas mehr als die langsamste gemessene Seite (1,9 s): so endet die
// Paginierung geordnet zwischen zwei Seiten, statt mitten in einem Aufruf in
// die Frist zu laufen und das bereits Geladene als Fehler zu verwerfen.
const ghIssuesReserve = 3 * time.Second

// ghAccounts maps a remote host to the gh account that may see it. `github-work`
// is an SSH alias (~/.ssh/config, HostName github.com) for a second identity.
var ghAccounts = map[string]string{
	"github.com":  "personal",
	"github-work": "work",
}

// GhAccountFor returns the gh account for a remote host, "" if unmapped.
// CC_GH_ACCOUNTS overrides the table ("host=account,host=account") — that is
// the injection point for a test and for a second machine with other
// accounts, so the names above stay a default and not a hard wiring.
func GhAccountFor(host string) string {
	h := strings.ToLower(strings.TrimSpace(host))
	if h == "" {
		return ""
	}
	if env := strings.TrimSpace(os.Getenv("CC_GH_ACCOUNTS")); env != "" {
		for _, pair := range strings.Split(env, ",") {
			k, v, ok := strings.Cut(pair, "=")
			if !ok {
				continue
			}
			if strings.ToLower(strings.TrimSpace(k)) == h {
				return strings.TrimSpace(v)
			}
		}
		return ""
	}
	return ghAccounts[h]
}

// ghRemoteRe matches the two remote forms git actually writes:
// scp-style (git@host:owner/repo.git) and URL-style
// (https://host/owner/repo, ssh://git@host/owner/repo.git).
var ghRemoteRe = regexp.MustCompile(`^(?:[a-zA-Z][a-zA-Z0-9+.-]*://)?(?:[^@/]+@)?([^/:]+)[:/]+([^/]+)/(.+?)(?:\.git)?/?$`)

// GhRemote is a parsed origin remote.
type GhRemote struct {
	Host    string `json:"host"`     // as written in the remote (may be an SSH alias)
	APIHost string `json:"api_host"` // the real GitHub host to talk to
	Owner   string `json:"owner"`
	Repo    string `json:"repo"`
}

// NWO is the "owner/repo" form gh's --repo flag wants.
func (r GhRemote) NWO() string { return r.Owner + "/" + r.Repo }

// ParseGhRemote splits a git remote URL into host, owner and repo.
func ParseGhRemote(url string) (GhRemote, bool) {
	m := ghRemoteRe.FindStringSubmatch(strings.TrimSpace(url))
	if m == nil {
		return GhRemote{}, false
	}
	r := GhRemote{Host: strings.ToLower(m[1]), Owner: m[2], Repo: m[3]}
	if r.Owner == "" || r.Repo == "" || strings.Contains(r.Repo, "/") {
		return GhRemote{}, false
	}
	return r, true
}

// sshAliasCache haelt die Antwort von `ssh -G` je Alias fuer die Laufzeit des
// Prozesses (gkp-ksb).
//
// Warum ueberhaupt: ein Aufruf kostet gemessen ~26 ms und die Antwort je Alias
// ist konstant, die Discovery loest denselben Alias aber einmal je Repo auf.
// Zwanzig Alias-Repos ohne .beads kosteten so 168 ms — und zwar im SYNCHRONEN
// Start (StartCache), nicht in einem Request, den jemand misst. Heute faellt
// unter ~/Work kein einziger Aufruf an, weil alle fuenf Repos .beads haben und
// die billige Pruefung in sidebar.go zuerst greift; relevant wird das genau
// dann, wenn Repos ohne beads dazukommen.
//
// BEWUSST OHNE INVALIDIERUNG — Prozesslaufzeit ist die Gueltigkeitsdauer. Drei
// Gruende, und der Preis steht darunter:
//
//  1. Eine Invalidierung ueber ~/.ssh/config waere FALSCH GENAU, nicht genauer:
//     ssh liest Include, Match und globale Configs mit, deshalb steht hier
//     `ssh -G` und kein eigener Parser. Ein Fingerabdruck auf eine einzige
//     Datei behauptete eine Vollstaendigkeit, die er nicht hat.
//  2. Eine TTL kostet eine Uhr und einen Codepfad, der nur im seltensten Fall
//     laeuft — also der, der am wenigsten getestet wird.
//  3. Ein Ausfall wird nicht gemerkt (siehe unten), damit bleibt im Cache nur,
//     was ssh tatsaechlich geantwortet hat.
//
// DER PREIS: wer ~/.ssh/config aendert, waehrend der Dienst laeuft, sieht die
// Aenderung erst nach einem Neustart des Containers. Das ist vertretbar, weil das
// Aendern eines Alias eine bewusste Handlung ist und der Fehler sichtbar wird
// (die Karte nennt Host → API-Host im Kopf), nicht stumm.
var sshAliasCache sync.Map // Alias (kleingeschrieben) -> aufgeloester Host

// resolveSSHAlias turns an SSH alias into the host it actually dials.
// `github-work` is not a hostname — it is an entry in ~/.ssh/config with
// `HostName github.com`, so gh must be told --hostname github.com or it looks
// for credentials of a host that does not exist. `ssh -G` is ssh's own
// resolver; parsing ~/.ssh/config here would mean reimplementing Include,
// Match and wildcards.
//
// Nebenlaeufigkeit: sync.Map statt Mutex, und die Sperre liegt NICHT um den
// Subprozess. Zwei Requests koennen denselben Alias gleichzeitig aufloesen und
// zahlen dann zweimal 26 ms — das ist der billigere Fehler. Die Alternative,
// den Aufruf unter einer Sperre zu serialisieren, liesse jeden Alias auf jeden
// anderen warten; `ssh -G` ist idempotent und liest nur, ein Doppelaufruf hat
// ausser der Zeit keine Folge.
func resolveSSHAlias(host string) string {
	alias := strings.ToLower(strings.TrimSpace(host))
	if v, ok := sshAliasCache.Load(alias); ok {
		return v.(string)
	}
	out, ok := shared.Run([]string{"ssh", "-G", host}, shared.RunOptions{TimeoutMs: 3000})
	if !ok {
		// Ein gescheitertes `ssh -G` ist keine Antwort, sondern ein Ausfall.
		// Ihn zu merken hiesse, einen voruebergehenden Fehler bis zum Neustart
		// des Dienstes festzuschreiben.
		return host
	}
	aufgeloest := alias
	for _, line := range strings.Split(out, "\n") {
		if rest, found := strings.CutPrefix(strings.TrimSpace(line), "hostname "); found {
			if h := strings.TrimSpace(rest); h != "" {
				aufgeloest = strings.ToLower(h)
				break
			}
		}
	}
	sshAliasCache.Store(alias, aufgeloest)
	return aufgeloest
}

// GhOriginRemote reads and parses the origin remote of the repo containing cwd.
func GhOriginRemote(cwd string) (GhRemote, bool) {
	repo := RepoRoot(cwd)
	out, ok := gitRun(repo, "remote", "get-url", "origin")
	if !ok || strings.TrimSpace(out) == "" {
		return GhRemote{}, false
	}
	r, ok := ParseGhRemote(out)
	if !ok {
		return GhRemote{}, false
	}
	if r.Host == "github.com" {
		r.APIHost = r.Host
	} else {
		r.APIHost = resolveSSHAlias(r.Host)
	}
	return r, true
}

// ghGraphQLURL ueberschreibt den Endpunkt; leer heisst "aus dem API-Host
// ableiten". Einziger Zweck: Tests zeigen damit auf einen httptest-Server.
var ghGraphQLURL = ""

// ghEndpoint liefert den GraphQL-Endpunkt eines API-Hosts. github.com hat
// eine eigene API-Domain, GitHub Enterprise Server haengt /api/graphql an.
func ghEndpoint(apiHost string) string {
	if ghGraphQLURL != "" {
		return ghGraphQLURL
	}
	if apiHost == "" || apiHost == "github.com" {
		return "https://api.github.com/graphql"
	}
	return "https://" + apiHost + "/api/graphql"
}

// ghClient ist ein Endpunkt plus Token. Das Token verlaesst die Struktur nur
// als Authorization-Header.
type ghClient struct {
	url   string
	token string
}

var ghHTTP = &http.Client{}

// ghMaxBody begrenzt eine Antwort. 100 Issues mit Kindern sind gemessen
// deutlich unter 1 MB; 32 MB schuetzen nur vor einem Proxy, der Unsinn schickt.
const ghMaxBody = 32 << 20

// graphql schickt eine Abfrage und liefert den rohen Antwortkoerper. Zwei
// Fristen: je Aufruf ghTimeoutMs, darueber die aeussere aus ctx — die
// Paginierung braucht beide, ihre Seiten sind einzeln schnell, in Summe aber
// unbegrenzt.
func (c ghClient) graphql(ctx context.Context, query string, vars map[string]any) ([]byte, error) {
	payload, err := json.Marshal(map[string]any{"query": query, "variables": vars})
	if err != nil {
		return nil, err
	}
	cctx, cancel := context.WithTimeout(ctx, time.Duration(ghTimeoutMs)*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodPost, c.url, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "git-planner")

	resp, err := ghHTTP.Do(req)
	if err != nil {
		switch {
		case ctx.Err() != nil:
			return nil, errors.New("GitHub-API hat die Gesamtfrist der Abfrage überschritten")
		case cctx.Err() == context.DeadlineExceeded:
			return nil, fmt.Errorf("GitHub-API hat nach %dms nicht geantwortet", ghTimeoutMs)
		}
		return nil, errors.New(firstLine(err.Error()))
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, ghMaxBody))
	if err != nil {
		return nil, fmt.Errorf("GitHub-API: Antwort abgebrochen: %s", firstLine(err.Error()))
	}
	if resp.StatusCode/100 != 2 {
		// GitHub schreibt den Grund als {"message": ...}; ein Proxy schreibt
		// HTML. Beides soll als eine lesbare Zeile auf der Karte landen.
		var e struct {
			Message string `json:"message"`
		}
		msg := strings.TrimSpace(string(body))
		if json.Unmarshal(body, &e) == nil && e.Message != "" {
			msg = e.Message
		}
		if len(msg) > 200 {
			msg = msg[:200]
		}
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, firstLine(msg))
	}
	return body, nil
}

// ghWorkOwners returns the GitHub owners whose repositories belong to the
// second identity (GITHUB_PAT_WORK), taken from GKP_WORK_OWNERS as a
// comma-separated list. The host alone does not decide that: work repositories
// can sit behind plain `https://github.com/<org>/...` remotes. Empty (the
// default) means every github.com owner uses GITHUB_PAT_PERSONAL.
func ghWorkOwners() map[string]bool {
	owners := map[string]bool{}
	for _, o := range strings.Split(os.Getenv("GKP_WORK_OWNERS"), ",") {
		if o = strings.ToLower(strings.TrimSpace(o)); o != "" {
			owners[o] = true
		}
	}
	return owners
}

// ghPATVar returns the name of the PAT variable for a repo: GITHUB_PAT_WORK for
// an owner listed in GKP_WORK_OWNERS or for the SSH host alias github-work,
// GITHUB_PAT_PERSONAL otherwise.
func ghPATVar(r GhRemote) string {
	if ghWorkOwners()[strings.ToLower(r.Owner)] || strings.ToLower(r.Host) == "github-work" {
		return "GITHUB_PAT_WORK"
	}
	return "GITHUB_PAT_PERSONAL"
}

// GhLabel, GhIssue and GhPR are the trimmed shapes the card renders. They are
// deliberately not gh's full JSON: the browser gets only what it draws.
type GhLabel struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

type ghAuthor struct {
	Login string `json:"login"`
}

// GhSubIssues ist der Fortschritt einer Aufgabe, so wie GitHub ihn selbst
// rechnet. Numbers traegt die Kinder, damit die Ansicht den Baum ohne einen
// zweiten Aufruf bauen kann; Truncated sagt, dass eine Seite dafuer nicht
// gereicht hat — subIssuesSummary.Total bleibt in dem Fall die Wahrheit.
//
// Achtung, und deshalb steht es hier: GitHub zaehlt nur DIREKTE Kinder. Ueber
// Enkel hinweg muss die Ansicht selbst rechnen (Spec §6).
type GhSubIssues struct {
	Total     int   `json:"total"`
	Completed int   `json:"completed"`
	Percent   int   `json:"percentCompleted"`
	Numbers   []int `json:"numbers,omitempty"`
	Truncated bool  `json:"truncated,omitempty"`
}

type GhIssue struct {
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	State     string    `json:"state"`
	UpdatedAt string    `json:"updatedAt"`
	URL       string    `json:"url"`
	Author    ghAuthor  `json:"author"`
	Labels    []GhLabel `json:"labels"`
	// Ab hier gkp-8zx. Alles optional: die Felder kommen aus GraphQL, und ein
	// Issue ohne Elternteil, ohne Kinder und ohne Schliessung soll in der
	// Antwort so schmal bleiben wie vorher.
	CreatedAt   string       `json:"createdAt,omitempty"`
	ClosedAt    string       `json:"closedAt,omitempty"`
	StateReason string       `json:"stateReason,omitempty"`
	Parent      int          `json:"parent,omitempty"` // Issue-Nummer, 0 = keiner
	SubIssues   *GhSubIssues `json:"subIssues,omitempty"`
}

// Closed sagt, ob ein Issue zum Fortschritt zaehlt. GraphQL schreibt den
// Zustand gross (OPEN/CLOSED), `gh issue list` tat das auch — verglichen wird
// trotzdem case-insensitiv, damit ein Wechsel der Quelle die Zaehlung nicht
// still auf null setzt.
func (i GhIssue) Closed() bool { return strings.EqualFold(i.State, "CLOSED") }

type GhPR struct {
	Number         int       `json:"number"`
	Title          string    `json:"title"`
	State          string    `json:"state"`
	UpdatedAt      string    `json:"updatedAt"`
	URL            string    `json:"url"`
	Author         ghAuthor  `json:"author"`
	Labels         []GhLabel `json:"labels"`
	IsDraft        bool      `json:"isDraft"`
	HeadRefName    string    `json:"headRefName"`
	ReviewDecision string    `json:"reviewDecision"`
}

// ghPRsQuery holt die offenen Pull Requests, zuletzt bewegte zuerst. Eine
// Seite genuegt: fuer PRs bleibt die Karte eine Uebersicht (ghListLimit).
const ghPRsQuery = `query($owner:String!, $name:String!, $limit:Int!) {
  repository(owner:$owner, name:$name) {
    pullRequests(first:$limit, states:OPEN, orderBy:{field:UPDATED_AT, direction:DESC}) {
      nodes {
        number title state updatedAt url isDraft headRefName reviewDecision
        author { login }
        labels(first:20) { nodes { name color } }
      }
    }
  }
}`

type ghPRNode struct {
	Number         int    `json:"number"`
	Title          string `json:"title"`
	State          string `json:"state"`
	UpdatedAt      string `json:"updatedAt"`
	URL            string `json:"url"`
	IsDraft        bool   `json:"isDraft"`
	HeadRefName    string `json:"headRefName"`
	ReviewDecision string `json:"reviewDecision"`
	Author         *struct {
		Login string `json:"login"`
	} `json:"author"`
	Labels struct {
		Nodes []GhLabel `json:"nodes"`
	} `json:"labels"`
}

type ghPRsAntwort struct {
	Data struct {
		Repository *struct {
			PullRequests struct {
				Nodes []ghPRNode `json:"nodes"`
			} `json:"pullRequests"`
		} `json:"repository"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

// ghIssuesQuery holt Issues in JEDEM Zustand, mit Elternteil und Kindern.
//
// Warum GraphQL und nicht `gh issue list`: Sub-Issues sind ueber die
// Listen-API grundsaetzlich nicht erreichbar, und `--state all --limit N`
// haette weiter einen willkuerlichen Deckel. parent, subIssues und
// subIssuesSummary brauchen gemessen WEDER einen Sonderheader NOCH ein
// Preview-Flag (2026-09-13, gh 2.100.0).
//
// Die Cursor-Variable heisst $endCursor, und zwar genau so. `gh api graphql
// --paginate` erkennt nur diesen Namen; mit jedem anderen fordert gh endlos
// Seite 1 an und haengt — in der Untersuchung zu gkp-8zx passiert. Geblaettert
// wird hier zwar selbst (siehe ghIssues), aber der Name bleibt der eine, der
// auch mit --paginate funktioniert.
//
// Ohne `states:` liefert die Verbindung offene UND geschlossene Issues. Die
// Sortierung ist nicht Kosmetik: sie macht aus einem Abbruch nach n Seiten die
// n*100 zuletzt bewegten Issues statt eines beliebigen Anfangs.
const ghIssuesQuery = `query($owner:String!, $name:String!, $endCursor:String) {
  repository(owner:$owner, name:$name) {
    issues(first:100, after:$endCursor, orderBy:{field:UPDATED_AT, direction:DESC}) {
      pageInfo { hasNextPage endCursor }
      nodes {
        number title url state stateReason createdAt updatedAt closedAt
        author { login }
        labels(first:20) { nodes { name color } }
        parent { number }
        subIssuesSummary { total completed percentCompleted }
        subIssues(first:100) { totalCount nodes { number } }
      }
    }
  }
}`

// ghIssueNode ist die Drahtform einer Antwort, GhIssue die Form der Karte.
// Beide getrennt zu halten ist kein Selbstzweck: GraphQL schachtelt Labels
// unter `nodes` und liefert `author: null` fuer ein geloeschtes Konto — die
// Karte soll von beidem nichts wissen muessen.
type ghIssueNode struct {
	Number      int    `json:"number"`
	Title       string `json:"title"`
	URL         string `json:"url"`
	State       string `json:"state"`
	StateReason string `json:"stateReason"`
	CreatedAt   string `json:"createdAt"`
	UpdatedAt   string `json:"updatedAt"`
	ClosedAt    string `json:"closedAt"`
	Author      *struct {
		Login string `json:"login"`
	} `json:"author"`
	Labels struct {
		Nodes []GhLabel `json:"nodes"`
	} `json:"labels"`
	Parent *struct {
		Number int `json:"number"`
	} `json:"parent"`
	SubIssuesSummary struct {
		Total            int `json:"total"`
		Completed        int `json:"completed"`
		PercentCompleted int `json:"percentCompleted"`
	} `json:"subIssuesSummary"`
	SubIssues struct {
		TotalCount int `json:"totalCount"`
		Nodes      []struct {
			Number int `json:"number"`
		} `json:"nodes"`
	} `json:"subIssues"`
}

// karte macht aus einem Knoten die Zeile, die die Ansicht zeichnet.
func (n ghIssueNode) karte() GhIssue {
	i := GhIssue{
		Number:      n.Number,
		Title:       n.Title,
		State:       n.State,
		UpdatedAt:   n.UpdatedAt,
		URL:         n.URL,
		Labels:      n.Labels.Nodes,
		CreatedAt:   n.CreatedAt,
		ClosedAt:    n.ClosedAt,
		StateReason: n.StateReason,
	}
	if i.Labels == nil {
		i.Labels = []GhLabel{}
	}
	if n.Author != nil {
		i.Author = ghAuthor{Login: n.Author.Login}
	}
	if n.Parent != nil {
		i.Parent = n.Parent.Number
	}
	// Ein Issue ohne Kinder bekommt kein subIssues-Objekt: die Ansicht zeichnet
	// nur dort einen Balken, wo es Unteraufgaben gibt (Spec §6).
	if n.SubIssuesSummary.Total > 0 || n.SubIssues.TotalCount > 0 {
		sub := &GhSubIssues{
			Total:     n.SubIssuesSummary.Total,
			Completed: n.SubIssuesSummary.Completed,
			Percent:   n.SubIssuesSummary.PercentCompleted,
		}
		for _, k := range n.SubIssues.Nodes {
			sub.Numbers = append(sub.Numbers, k.Number)
		}
		sub.Truncated = len(sub.Numbers) < n.SubIssues.TotalCount
		i.SubIssues = sub
	}
	return i
}

// ghIssuesAntwort ist die Huelle, die gh ausgibt. Das errors-Array steht mit
// drin, weil GraphQL auch mit HTTP 200 fehlschlagen kann: data waere dann null
// und eine Pruefung nur auf den Exit-Code machte daraus eine leere Liste, die
// wie eine vollstaendige aussieht.
type ghIssuesAntwort struct {
	Data struct {
		Repository *struct {
			Issues struct {
				PageInfo struct {
					HasNextPage bool   `json:"hasNextPage"`
					EndCursor   string `json:"endCursor"`
				} `json:"pageInfo"`
				Nodes []ghIssueNode `json:"nodes"`
			} `json:"issues"`
		} `json:"repository"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

// ghIssuesResult ist das Ergebnis einer Paginierung. Truncated und Note sind
// nicht optional zu lesen: ohne sie waere ein Abbruch nach n Seiten von einem
// vollstaendigen Lauf nicht zu unterscheiden.
type ghIssuesResult struct {
	Issues    []GhIssue
	Truncated bool
	Note      string
}

// ghIssues holt alle Issues eines Repos, Seite fuer Seite.
//
// Geblaettert wird selbst statt mit `gh api graphql --paginate`, und das ist
// eine Entscheidung, keine Umstaendlichkeit: --paginate kennt keine Obergrenze
// und keine Gesamtfrist, gibt mehrere JSON-Dokumente hintereinander aus und
// sucht sich sein pageInfo selbst — bei einer Abfrage mit einer zweiten,
// verschachtelten Verbindung (subIssues) ist das nicht die Stelle, die man
// meint. Eine eigene Schleife terminiert dagegen nachweislich: kein naechster
// Cursor, ein wiederholter Cursor, der Seitendeckel oder das Zeitbudget.
func ghIssues(c ghClient, owner, name string) (ghIssuesResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), ghIssuesBudget)
	defer cancel()

	res := ghIssuesResult{Issues: []GhIssue{}}
	gesehen := map[string]bool{}
	cursor := ""
	for seite := 1; ; seite++ {
		nodes, info, err := ghIssuesPage(ctx, c, owner, name, cursor)
		if err != nil {
			return ghIssuesResult{}, err
		}
		for _, n := range nodes {
			res.Issues = append(res.Issues, n.karte())
		}
		if !info.HasNextPage || info.EndCursor == "" {
			break
		}
		if gesehen[info.EndCursor] {
			res.Truncated = true
			res.Note = fmt.Sprintf("Paginierung nach %d Seiten abgebrochen: GitHub gab denselben Cursor erneut (%d Issues geladen)", seite, len(res.Issues))
			break
		}
		gesehen[info.EndCursor] = true
		cursor = info.EndCursor

		if seite >= ghIssuePageCap {
			res.Truncated = true
			res.Note = fmt.Sprintf("Deckel von %d Seiten erreicht — nur die %d zuletzt geänderten Issues geladen", ghIssuePageCap, len(res.Issues))
			break
		}
		if frist, ok := ctx.Deadline(); ok && time.Until(frist) < ghIssuesReserve {
			res.Truncated = true
			res.Note = fmt.Sprintf("Zeitbudget von %ds erschöpft — nur die %d zuletzt geänderten Issues geladen", int(ghIssuesBudget/time.Second), len(res.Issues))
			break
		}
	}
	sortGhIssues(res.Issues)
	return res, nil
}

type ghPageInfo struct {
	HasNextPage bool
	EndCursor   string
}

// ghIssuesPage holt genau eine Seite. cursor == "" heisst erste Seite: die
// Variable wird dann gar nicht erst mitgegeben, damit $endCursor null ist und
// nicht der leere String — `after:""` ist fuer GitHub kein gueltiger Cursor.
func ghIssuesPage(ctx context.Context, c ghClient, owner, name, cursor string) ([]ghIssueNode, ghPageInfo, error) {
	vars := map[string]any{"owner": owner, "name": name}
	if cursor != "" {
		vars["endCursor"] = cursor
	}
	out, err := c.graphql(ctx, ghIssuesQuery, vars)
	if err != nil {
		return nil, ghPageInfo{}, err
	}
	var antwort ghIssuesAntwort
	if err := json.Unmarshal(bytes.TrimSpace(out), &antwort); err != nil {
		return nil, ghPageInfo{}, fmt.Errorf("GitHub-API: unlesbares JSON: %s", firstLine(err.Error()))
	}
	if len(antwort.Errors) > 0 {
		return nil, ghPageInfo{}, fmt.Errorf("GraphQL: %s", firstLine(antwort.Errors[0].Message))
	}
	if antwort.Data.Repository == nil {
		return nil, ghPageInfo{}, fmt.Errorf("GraphQL: %s/%s hat kein repository geliefert", owner, name)
	}
	iss := antwort.Data.Repository.Issues
	return iss.Nodes, ghPageInfo{HasNextPage: iss.PageInfo.HasNextPage, EndCursor: iss.PageInfo.EndCursor}, nil
}

// sortGhIssues stellt die offenen nach oben, innerhalb dessen das zuletzt
// Bewegte. Seit die Liste auch geschlossene Issues traegt, ist das der
// Unterschied zwischen einer Karte und einem Archiv: in infra sind 199
// von 209 Issues geschlossen, die zehn offenen wuerden sonst untergehen.
func sortGhIssues(issues []GhIssue) {
	sort.SliceStable(issues, func(a, b int) bool {
		ca, cb := issues[a].Closed(), issues[b].Closed()
		if ca != cb {
			return !ca
		}
		if issues[a].UpdatedAt != issues[b].UpdatedAt {
			return issues[a].UpdatedAt > issues[b].UpdatedAt
		}
		return issues[a].Number > issues[b].Number
	})
}

// ghLeereKarteGrund erklaert eine leere Karte, wenn zum Host kein gh-Konto
// gehoert. Im Code ist das ein Zweig, fuer den Leser der Karte sind es zwei
// voellig verschiedene Geschichten — und das ist die Bedingung, unter der die
// Entscheidung aus gkp-apb ueberhaupt gilt.
//
// Entschieden wurde dort (a): die Discovery nimmt JEDES parsbare origin auf,
// nicht nur github.com. Ein Repo mit GitLab-origin ist nach jedem anderen
// Massstab des Dashboards ein Projekt (Git-Status, CLAUDE.md, Sessions, Kosten,
// beads) und darf nicht an der schwaechsten der sechs Karten scheitern. Der
// Preis ist eine leere GitHub-Karte — ein Irrtum, den man SIEHT, im Gegensatz
// zu (b), wo das Projekt gar nicht erst in der Liste steht. Bezahlbar ist er
// nur, wenn die Karte den Host nennt statt stumm zu bleiben.
//
// "kein gh-Konto hinterlegt" waere dafuer der falsche Text: er schickt den
// Leser in eine Tabelle, in der es fuer gitlab.com nichts einzutragen gibt.
// Umgekehrt bleibt genau dieser Text richtig, wenn der Host GitHub IST — dann
// ist die Tabelle die Adresse, und das ist ein Fehler, den man beheben kann.
//
// Was ausser github.com noch GitHub ist (GHES, eine eigene Enterprise-Domain),
// steht in ghAccounts bzw. CC_GH_ACCOUNTS — genau dafuer gibt es die Tabelle.
// Ein Host, den niemand dort genannt hat, ist hier deshalb kein GitHub-Host;
// geraten wird nicht.
func ghLeereKarteGrund(r GhRemote) string {
	if r.APIHost == "github.com" {
		return fmt.Sprintf("kein gh-Konto für Host %q hinterlegt (CC_GH_ACCOUNTS oder ghAccounts in internal/collectors/github.go)", r.Host)
	}
	// Beim Alias ist der geschriebene Host nicht der, der zaehlt: `git@acme-alt`
	// allein sagt niemandem, wohin das zeigt.
	if r.Host != r.APIHost && r.APIHost != "" {
		return fmt.Sprintf("kein GitHub-Remote (origin zeigt über den SSH-Alias %q auf %q)", r.Host, r.APIHost)
	}
	return fmt.Sprintf("kein GitHub-Remote (origin zeigt auf %q)", r.Host)
}

// ghLogAusgabe ist das Ziel der einen Logzeile je Abruf (GitHub-Issue #14).
// Vorgabe ist stderr, also das Journal der systemd-Unit; Tests lenken sie in
// einen Puffer um, damit sie die Zeile ohne Netz pruefen koennen.
var ghLogAusgabe io.Writer = os.Stderr

// ghLogZeile schreibt die Logzeile eines Abrufs: Repo, Name der PAT-Variablen,
// Token-Quelle und Ergebnis. Der Tokenwert steht nie darin — auch nicht, wenn
// gh ihn in eine Fehlermeldung schreibt: jedes Vorkommen wird ersetzt, bevor
// die Zeile das Paket verlaesst.
func ghLogZeile(nwo, patVar, quelle, ergebnis, token string) {
	if nwo == "" {
		nwo = "-"
	}
	if patVar == "" {
		patVar = "-"
	}
	if quelle == "" {
		quelle = "-"
	}
	zeile := fmt.Sprintf("%s github-karte: repo=%s pat=%s quelle=%s %s",
		time.Now().UTC().Format(time.RFC3339), nwo, patVar, quelle, ergebnis)
	if token != "" {
		zeile = strings.ReplaceAll(zeile, token, "***")
	}
	zeile = strings.ReplaceAll(zeile, "\n", " ")
	fmt.Fprintln(ghLogAusgabe, zeile)
}

// CollectGitHub is the whole card. Fail-open at every step: a missing remote,
// an unmapped host, no gh, no token or a refusing API each produce
// available:false with a reason a human can act on — never a broken page.
func CollectGitHub(cwd string) (out map[string]any) {
	// Genau eine Logzeile je Abruf, auf jedem Rueckweg. Der Cache-Treffer in
	// CollectGitHubCached kommt hier gar nicht erst an.
	var nwo, patVar, quelle, token string
	defer func() {
		ergebnis := ""
		if out["available"] == true {
			ergebnis = fmt.Sprintf("issues=%v prs=%v", out["issue_count"], out["pr_count"])
			// Eine gescheiterte Haelfte zaehlt sonst als 0 und sieht aus wie
			// ein leeres Repo.
			for _, k := range []string{"issues_error", "prs_error"} {
				if v, ok := out[k].(string); ok && v != "" {
					ergebnis += fmt.Sprintf(" %s=%q", k, v)
				}
			}
		} else {
			ergebnis = fmt.Sprintf("fehler=%q", fmt.Sprint(out["reason"]))
		}
		ghLogZeile(nwo, patVar, quelle, ergebnis, token)
	}()

	remote, ok := GhOriginRemote(cwd)
	if !ok {
		// Seit gkp-apb nicht mehr "keine GitHub-URL": ParseGhRemote nimmt jeden
		// Host, scheitert also nur an einem fehlenden origin oder an einer URL
		// ohne owner/repo (ein lokaler Pfad, ein verschachteltes GitLab-Projekt).
		return map[string]any{"available": false, "reason": "kein origin-Remote oder eine URL ohne owner/repo"}
	}
	nwo = remote.NWO()
	out = map[string]any{
		"available": false,
		"host":      remote.Host,
		"api_host":  remote.APIHost,
		"owner":     remote.Owner,
		"repo":      remote.Repo,
		"nwo":       remote.NWO(),
		"issues":    []GhIssue{},
		"prs":       []GhPR{},
	}

	account := GhAccountFor(remote.Host)
	if account == "" {
		out["reason"] = ghLeereKarteGrund(remote)
		return out
	}

	// Nur der PAT aus der Umgebung. Die Karte nennt den NAMEN der Variablen
	// als Konto, nie den Wert.
	patVar = ghPATVar(remote)
	out["account"] = patVar
	token = os.Getenv(patVar)
	if token == "" {
		out["reason"] = patVar + " ist nicht gesetzt"
		return out
	}
	quelle = "umgebung"
	c := ghClient{url: ghEndpoint(remote.APIHost), token: token}

	// Both lists are independent network calls — run them at the same time,
	// so the card costs one round trip instead of two.
	var (
		wg                sync.WaitGroup
		issues            ghIssuesResult
		prs               []GhPR
		issuesErr, prsErr error
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		issues, issuesErr = ghIssues(c, remote.Owner, remote.Repo)
	}()
	go func() {
		defer wg.Done()
		prs, prsErr = ghPRs(c, remote.Owner, remote.Repo)
	}()
	wg.Wait()

	// One half failing must not hide the other: a repo with issues disabled
	// still has pull requests.
	if issuesErr != nil {
		out["issues_error"] = issuesErr.Error()
	}
	if prsErr != nil {
		out["prs_error"] = prsErr.Error()
	}
	if issuesErr != nil && prsErr != nil {
		out["reason"] = issuesErr.Error()
		return out
	}
	offen := 0
	for _, i := range issues.Issues {
		if !i.Closed() {
			offen++
		}
	}
	out["available"] = true
	out["issues"] = issues.Issues
	out["prs"] = prs
	out["issue_count"] = len(issues.Issues)
	out["open_issue_count"] = offen
	out["closed_issue_count"] = len(issues.Issues) - offen
	// Abgeschnitten oder nicht steht IMMER in der Antwort, auch als false: ein
	// fehlendes Feld liest sich wie "vollstaendig", und genau diese Luege soll
	// die Karte nicht erzaehlen koennen.
	out["issues_truncated"] = issues.Truncated
	if issues.Note != "" {
		out["issues_note"] = issues.Note
	}
	out["pr_count"] = len(prs)
	out["limit"] = ghListLimit // gilt nur noch fuer die PR-Liste
	return out
}

// ghCacheTTL is how long a card answer is reused. GitHub state changes
// without anything local changing, so the mtime fingerprint the project cache
// uses (cache.reportsFingerprint) cannot see it — a plain time-based TTL is
// the only honest option here. Two minutes keeps a reload from costing a
// round trip while staying short enough that a closed issue disappears within
// the same work session.
const ghCacheTTL = 2 * time.Minute

type ghCacheEntry struct {
	at   time.Time
	data map[string]any
}

var ghCache sync.Map // repo root -> ghCacheEntry

// CollectGitHubCached is CollectGitHub behind a per-repo TTL. Keyed by repo
// root, not cwd, so a subdirectory and a worktree of the same checkout share
// the answer.
func CollectGitHubCached(cwd string) map[string]any {
	key := RepoRoot(cwd)
	if v, ok := ghCache.Load(key); ok {
		e := v.(ghCacheEntry)
		if time.Since(e.at) < ghCacheTTL {
			out := make(map[string]any, len(e.data)+1)
			for k, val := range e.data {
				out[k] = val
			}
			out["cached_age_s"] = int(time.Since(e.at).Seconds())
			return out
		}
	}
	data := CollectGitHub(cwd)
	ghCache.Store(key, ghCacheEntry{at: time.Now(), data: data})
	return data
}

// ghPRs holt die offenen Pull Requests (hoechstens ghListLimit).
func ghPRs(c ghClient, owner, name string) ([]GhPR, error) {
	out, err := c.graphql(context.Background(), ghPRsQuery,
		map[string]any{"owner": owner, "name": name, "limit": ghListLimit})
	if err != nil {
		return nil, err
	}
	var antwort ghPRsAntwort
	if err := json.Unmarshal(bytes.TrimSpace(out), &antwort); err != nil {
		return nil, fmt.Errorf("GitHub-API: unlesbares JSON: %s", firstLine(err.Error()))
	}
	if len(antwort.Errors) > 0 {
		return nil, fmt.Errorf("GraphQL: %s", firstLine(antwort.Errors[0].Message))
	}
	if antwort.Data.Repository == nil {
		return nil, fmt.Errorf("GraphQL: %s/%s hat kein repository geliefert", owner, name)
	}
	prs := []GhPR{}
	for _, n := range antwort.Data.Repository.PullRequests.Nodes {
		pr := GhPR{
			Number: n.Number, Title: n.Title, State: n.State, UpdatedAt: n.UpdatedAt,
			URL: n.URL, Labels: n.Labels.Nodes, IsDraft: n.IsDraft,
			HeadRefName: n.HeadRefName, ReviewDecision: n.ReviewDecision,
		}
		if pr.Labels == nil {
			pr.Labels = []GhLabel{}
		}
		if n.Author != nil {
			pr.Author = ghAuthor{Login: n.Author.Login}
		}
		prs = append(prs, pr)
	}
	return prs, nil
}

// firstLine keeps a card message to one line; gh likes to add usage hints.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}
