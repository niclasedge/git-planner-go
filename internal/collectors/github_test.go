package collectors

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestParseGhRemote covers the four remote forms that actually appear on this
// machine plus the shapes that must NOT be mistaken for a repo.
func TestParseGhRemote(t *testing.T) {
	cases := []struct {
		url               string
		host, owner, repo string
		ok                bool
	}{
		{"git@github.com:example-user/notes.git", "github.com", "example-user", "notes", true},
		{"git@github-work:example-org/git-planner.git", "github-work", "example-org", "git-planner", true},
		{"https://github.com/example-user/infra", "github.com", "example-user", "infra", true},
		{"https://github.com/example-user/infra.git", "github.com", "example-user", "infra", true},
		{"ssh://git@github.com/example-user/notes.git", "github.com", "example-user", "notes", true},
		{"GIT@GitHub.com:example-user/notes.git", "github.com", "example-user", "notes", true},
		// Nested paths are a GitLab shape, not GitHub's owner/repo — accepting
		// them would build an --repo argument gh cannot use.
		{"git@gitlab.example:group/sub/proj.git", "", "", "", false},
		{"/home/user/work/lokal", "", "", "", false},
		{"", "", "", "", false},
	}
	for _, c := range cases {
		got, ok := ParseGhRemote(c.url)
		if ok != c.ok {
			t.Errorf("ParseGhRemote(%q) ok = %v, want %v (got %+v)", c.url, ok, c.ok, got)
			continue
		}
		if !ok {
			continue
		}
		if got.Host != c.host || got.Owner != c.owner || got.Repo != c.repo {
			t.Errorf("ParseGhRemote(%q) = %+v, want host=%q owner=%q repo=%q",
				c.url, got, c.host, c.owner, c.repo)
		}
		if want := c.owner + "/" + c.repo; got.NWO() != want {
			t.Errorf("NWO() = %q, want %q", got.NWO(), want)
		}
	}
}

// TestGhAccountForDefaults pins the rule the user gave on 2026-09-04: the
// remote host decides the account. Getting this wrong does not look like an
// auth error — GitHub answers "Could not resolve to a Repository", i.e. the
// card would claim the repo is gone.
func TestGhAccountForDefaults(t *testing.T) {
	t.Setenv("CC_GH_ACCOUNTS", "")
	cases := map[string]string{
		"github.com":  "personal",
		"GitHub.com":  "personal",
		"github-work": "work",
		"gitlab.com":  "",
		"":            "",
	}
	for host, want := range cases {
		if got := GhAccountFor(host); got != want {
			t.Errorf("GhAccountFor(%q) = %q, want %q", host, got, want)
		}
	}
}

// TestGhAccountForEnvOverride: the env var replaces the table entirely rather
// than extending it, so a second machine cannot silently fall back to this
// machine's account names.
func TestGhAccountForEnvOverride(t *testing.T) {
	t.Setenv("CC_GH_ACCOUNTS", "github.com=someone-else, ghe.intern=work-acct")
	if got := GhAccountFor("github.com"); got != "someone-else" {
		t.Errorf("override ignored: got %q", got)
	}
	if got := GhAccountFor("ghe.intern"); got != "work-acct" {
		t.Errorf("second pair lost: got %q", got)
	}
	if got := GhAccountFor("github-work"); got != "" {
		t.Errorf("default table leaked through the override: got %q", got)
	}
}

// TestGhIssueDecoding pins that the trimmed structs still read gh's real
// output shape — gh nests author and labels, and a wrong tag would show up as
// an empty column rather than an error.
func TestGhIssueDecoding(t *testing.T) {
	raw := `[{"number":197,"title":"Error detected","state":"OPEN",
	  "updatedAt":"2026-09-04T21:20:02Z","url":"https://github.com/x/y/issues/197",
	  "author":{"login":"example-user"},
	  "labels":[{"name":"alert","color":"ededed"},{"name":"auto","color":"ededed"}]}]`
	var issues []GhIssue
	if err := json.Unmarshal([]byte(raw), &issues); err != nil {
		t.Fatalf("gh issue JSON rejected: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("got %d issues, want 1", len(issues))
	}
	i := issues[0]
	if i.Number != 197 || i.Author.Login != "example-user" || len(i.Labels) != 2 || i.Labels[0].Name != "alert" {
		t.Errorf("decoded issue = %+v", i)
	}
}

func TestGhPRDecoding(t *testing.T) {
	raw := `[{"number":12,"title":"feat: x","state":"OPEN","updatedAt":"2026-09-04T10:00:00Z",
	  "url":"https://github.com/x/y/pull/12","author":{"login":"a"},"labels":[],
	  "isDraft":true,"headRefName":"feat/x","reviewDecision":"CHANGES_REQUESTED"}]`
	var prs []GhPR
	if err := json.Unmarshal([]byte(raw), &prs); err != nil {
		t.Fatalf("gh pr JSON rejected: %v", err)
	}
	p := prs[0]
	if !p.IsDraft || p.HeadRefName != "feat/x" || p.ReviewDecision != "CHANGES_REQUESTED" {
		t.Errorf("decoded pr = %+v", p)
	}
}

// TestCollectGitHubFailsOpenWithoutRemote: a directory that is not a GitHub
// checkout must produce a card that says so, never a panic and never a page
// without a reason.
func TestCollectGitHubFailsOpenWithoutRemote(t *testing.T) {
	out := CollectGitHub(t.TempDir())
	if out["available"] != false {
		t.Fatalf("expected available:false, got %v", out)
	}
	if reason, _ := out["reason"].(string); reason == "" {
		t.Error("available:false without a reason is not actionable")
	}
}

// gkp-8zx: issues come from the GraphQL API, so a test has to stand in for
// GitHub itself. Everything below runs against RECORDED answers — a test that needs
// itself. Everything below runs against RECORDED answers — a test that needs
// GitHub is not a test, it is a network check that fails on a train.
//
// The two pages are cut from a real answer of example-user/infra, taken
// 2026-09-13 (209 issues, 3 pages of 100, 3 parents with 43 children between
// them). Shapes are verbatim: labels arrive under `nodes`, `parent` is an
// object or null, `stateReason` is null while an issue is open.
// ---------------------------------------------------------------------------

// ghCursorSeite1 is the cursor the recording really handed out. It is pinned
// here because the test checks that exactly this value comes back as
// `-f endCursor=…` — the variable name is the trap this ticket is about.
const ghCursorSeite1 = "Y3Vyc29yOnYyOpK0MjAyNi0wOC0wMVQxNDowNzo0MFrPAAAAASw7tBM="

const ghSeite1 = `{"data":{"repository":{"issues":{
  "pageInfo":{"hasNextPage":true,"endCursor":"` + ghCursorSeite1 + `"},
  "nodes":[
    {"number":209,"title":"Alertmanager meldet nichts mehr",
     "url":"https://github.com/example-user/infra/issues/209",
     "state":"OPEN","stateReason":null,
     "createdAt":"2026-09-11T08:00:00Z","updatedAt":"2026-09-12T10:00:00Z","closedAt":null,
     "author":{"login":"example-user"},
     "labels":{"nodes":[{"name":"alert","color":"ededed"}]},
     "parent":null,
     "subIssuesSummary":{"total":0,"completed":0,"percentCompleted":0},
     "subIssues":{"totalCount":0,"nodes":[]}},
    {"number":66,"title":"Wayfinder-Map: Netcup- & MacBook-Services",
     "url":"https://github.com/example-user/infra/issues/66",
     "state":"CLOSED","stateReason":"COMPLETED",
     "createdAt":"2026-07-26T11:20:34Z","updatedAt":"2026-08-04T05:55:09Z","closedAt":"2026-08-04T05:55:09Z",
     "author":{"login":"example-user"},
     "labels":{"nodes":[{"name":"wayfinder:map","color":"6f42c1"}]},
     "parent":null,
     "subIssuesSummary":{"total":22,"completed":22,"percentCompleted":100},
     "subIssues":{"totalCount":22,"nodes":[{"number":67},{"number":86},{"number":87}]}},
    {"number":87,"title":"Skill remote-deployment-ansible umstellen",
     "url":"https://github.com/example-user/infra/issues/87",
     "state":"CLOSED","stateReason":"COMPLETED",
     "createdAt":"2026-07-26T11:21:02Z","updatedAt":"2026-08-04T05:55:22Z","closedAt":"2026-08-04T05:55:22Z",
     "author":{"login":"example-user"},
     "labels":{"nodes":[{"name":"wayfinder:task","color":"0e8a16"}]},
     "parent":{"number":66},
     "subIssuesSummary":{"total":0,"completed":0,"percentCompleted":0},
     "subIssues":{"totalCount":0,"nodes":[]}}
  ]}}}}`

const ghSeite2 = `{"data":{"repository":{"issues":{
  "pageInfo":{"hasNextPage":false,"endCursor":"Y3Vyc29yOnYyOpK0MjAyNi0wNy0wMVQwMDowMDowMFrO"},
  "nodes":[
    {"number":12,"title":"Prometheus-Retention anheben",
     "url":"https://github.com/example-user/infra/issues/12",
     "state":"OPEN","stateReason":null,
     "createdAt":"2026-06-01T09:00:00Z","updatedAt":"2026-06-02T09:00:00Z","closedAt":null,
     "author":null,
     "labels":{"nodes":[]},
     "parent":null,
     "subIssuesSummary":{"total":0,"completed":0,"percentCompleted":0},
     "subIssues":{"totalCount":0,"nodes":[]}}
  ]}}}}`

// ghHandler answers one GraphQL request. n counts the calls of its kind,
// starting at 1.
type ghHandler func(n int, w http.ResponseWriter, r *http.Request)

// ghAnfrage is one recorded request: the decoded GraphQL body plus the
// Authorization header, so a test can see which token went out.
type ghAnfrage struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables"`
	Auth      string         `json:"-"`
}

// ghAPIFake is the GitHub GraphQL endpoint, in process. Issue and PR queries
// are told apart by the query text; each kind has its own handler and its own
// log.
type ghAPIFake struct {
	mu     sync.Mutex
	issues []ghAnfrage
	prs    []ghAnfrage
}

func (f *ghAPIFake) issueAnfragen() []ghAnfrage {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]ghAnfrage(nil), f.issues...)
}

func (f *ghAPIFake) alle() []ghAnfrage {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append(append([]ghAnfrage(nil), f.issues...), f.prs...)
}

const ghLeerePRs = `{"data":{"repository":{"pullRequests":{"nodes":[]}}}}`

// ghAPI starts the fake and points ghGraphQLURL at it for this test. prs nil
// answers with an empty PR list.
func ghAPI(t *testing.T, issues, prs ghHandler) *ghAPIFake {
	t.Helper()
	f := &ghAPIFake{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var a ghAnfrage
		_ = json.NewDecoder(r.Body).Decode(&a)
		a.Auth = r.Header.Get("Authorization")
		f.mu.Lock()
		istPR := strings.Contains(a.Query, "pullRequests")
		var n int
		if istPR {
			f.prs = append(f.prs, a)
			n = len(f.prs)
		} else {
			f.issues = append(f.issues, a)
			n = len(f.issues)
		}
		f.mu.Unlock()
		switch {
		case istPR && prs != nil:
			prs(n, w, r)
		case istPR:
			fmt.Fprint(w, ghLeerePRs)
		default:
			issues(n, w, r)
		}
	}))
	alt := ghGraphQLURL
	ghGraphQLURL = srv.URL
	t.Cleanup(func() {
		ghGraphQLURL = alt
		srv.Close()
	})
	return f
}

// ghSeiten serves the recorded pages in order. The LAST page repeats once the
// recording runs out: that is what lets a test simulate a connection that
// never ends without a special mode.
func ghSeiten(t *testing.T, seiten ...string) *ghAPIFake {
	t.Helper()
	return ghAPI(t, func(n int, w http.ResponseWriter, _ *http.Request) {
		if n > len(seiten) {
			n = len(seiten)
		}
		fmt.Fprint(w, seiten[n-1])
	}, nil)
}

// ghFehler answers every request with an HTTP status and a GitHub-style
// {"message": ...} body.
func ghFehler(status int, message string) ghHandler {
	return func(_ int, w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		b, _ := json.Marshal(map[string]string{"message": message})
		w.Write(b)
	}
}

// ghTestClient talks to whatever ghAPI started.
func ghTestClient() ghClient { return ghClient{url: ghEndpoint("github.com"), token: "pat-test"} }

// ghAufrufe reads the call counter a shell fake (sshFake) writes to dir.
func ghAufrufe(t *testing.T, dir string) int {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "zaehler"))
	if err != nil {
		return 0
	}
	n := 0
	fmt.Sscanf(strings.TrimSpace(string(b)), "%d", &n)
	return n
}

// ghKurzeFristen shortens the two deadlines for one test. Proving that a
// wedged gh is given up on is the point; waiting the real 15 s to prove it
// would get the test deleted.
func ghKurzeFristen(t *testing.T, callMs int, budget time.Duration) {
	t.Helper()
	altCall, altBudget := ghTimeoutMs, ghIssuesBudget
	ghTimeoutMs, ghIssuesBudget = callMs, budget
	t.Cleanup(func() { ghTimeoutMs, ghIssuesBudget = altCall, altBudget })
}

// TestGhIssuesReadsOpenAndClosed is the reason this ticket exists: without the
// closed issues there is no progress to compute, and without `parent` /
// `subIssues` there is no hierarchy — neither is reachable through
// the issue list endpoint at all.
func TestGhIssuesReadsOpenAndClosed(t *testing.T) {
	f := ghSeiten(t, ghSeite1, ghSeite2)

	res, err := ghIssues(ghTestClient(), "example-user", "infra")
	if err != nil {
		t.Fatalf("ghIssues: %v", err)
	}
	if len(res.Issues) != 4 {
		t.Fatalf("%d Issues, erwartet 4 (beide Seiten): %+v", len(res.Issues), res.Issues)
	}
	if res.Truncated {
		t.Errorf("Truncated=true, obwohl die Aufzeichnung hasNextPage:false meldet (%q)", res.Note)
	}
	if n := len(f.issueAnfragen()); n != 2 {
		t.Errorf("%d API-Aufrufe, erwartet 2 — je Seite einer, keiner mehr", n)
	}

	var offen, geschlossen int
	nach := map[int]GhIssue{}
	for _, i := range res.Issues {
		nach[i.Number] = i
		if i.State == "OPEN" {
			offen++
		} else {
			geschlossen++
		}
	}
	if offen != 2 || geschlossen != 2 {
		t.Errorf("offen=%d geschlossen=%d, erwartet 2/2", offen, geschlossen)
	}

	// Der Elternteil: die Summary kommt fertig aus der API, die Kinderliste
	// ist in dieser Aufzeichnung gekuerzt (3 von 22) — genau der Fall, den
	// SubIssues.Truncated melden muss.
	eltern := nach[66]
	if eltern.SubIssues == nil {
		t.Fatalf("#66 hat kein subIssues gelesen: %+v", eltern)
	}
	if eltern.SubIssues.Total != 22 || eltern.SubIssues.Completed != 22 || eltern.SubIssues.Percent != 100 {
		t.Errorf("subIssuesSummary von #66 = %+v, erwartet 22/22/100", eltern.SubIssues)
	}
	if len(eltern.SubIssues.Numbers) != 3 || eltern.SubIssues.Numbers[0] != 67 {
		t.Errorf("Kinder von #66 = %v, erwartet [67 86 87]", eltern.SubIssues.Numbers)
	}
	if !eltern.SubIssues.Truncated {
		t.Errorf("#66: %d von %d Kindern geladen, Truncated muss true sein",
			len(eltern.SubIssues.Numbers), eltern.SubIssues.Total)
	}

	// Das Kind: ohne parent ist die Zugehoerigkeit nicht rekonstruierbar,
	// denn eine Issue-Nummer sagt nichts ueber die Hierarchie (Spec §3.2).
	kind := nach[87]
	if kind.Parent != 66 {
		t.Errorf("parent von #87 = %d, erwartet 66", kind.Parent)
	}
	if kind.ClosedAt == "" || kind.StateReason != "COMPLETED" {
		t.Errorf("#87 closedAt=%q stateReason=%q — beides gehoert zum Fortschritt",
			kind.ClosedAt, kind.StateReason)
	}
	if eltern.Parent != 0 {
		t.Errorf("parent von #66 = %d, erwartet 0 (null in der Antwort)", eltern.Parent)
	}

	// Labels stehen im GraphQL unter nodes, nicht direkt im Array — die Karte
	// zeichnet sie aber wie bisher.
	if len(eltern.Labels) != 1 || eltern.Labels[0].Name != "wayfinder:map" || eltern.Labels[0].Color != "6f42c1" {
		t.Errorf("Labels von #66 = %+v", eltern.Labels)
	}
	// Ein geloeschtes Konto liefert author:null. Das darf keine Panik geben.
	if nach[12].Author.Login != "" {
		t.Errorf("author:null wurde zu %q", nach[12].Author.Login)
	}

	// Offene zuerst, dann nach updatedAt absteigend: sonst versinken die zwei
	// offenen Vorgaenge unter 199 geschlossenen, sobald die Karte alles zeigt.
	wollte := []int{209, 12, 87, 66}
	for i, w := range wollte {
		if res.Issues[i].Number != w {
			t.Fatalf("Reihenfolge = %v, erwartet %v", nummern(res.Issues), wollte)
		}
	}
}

func nummern(is []GhIssue) []int {
	n := make([]int, len(is))
	for i, x := range is {
		n[i] = x.Number
	}
	return n
}

// TestGhIssuesSendsTheCursorAsEndCursor pins how the cursor travels: page 1
// without the variable (null, since `after:""` is no valid cursor), page 2
// with exactly the cursor page 1 handed out.
func TestGhIssuesSendsTheCursorAsEndCursor(t *testing.T) {
	f := ghSeiten(t, ghSeite1, ghSeite2)
	if _, err := ghIssues(ghTestClient(), "example-user", "infra"); err != nil {
		t.Fatalf("ghIssues: %v", err)
	}
	a := f.issueAnfragen()
	if len(a) != 2 {
		t.Fatalf("%d Aufrufe aufgezeichnet: %+v", len(a), a)
	}
	if _, da := a[0].Variables["endCursor"]; da {
		t.Errorf("erster Aufruf schickt schon einen Cursor: %v", a[0].Variables)
	}
	if a[1].Variables["endCursor"] != ghCursorSeite1 {
		t.Errorf("zweiter Aufruf ohne endCursor=<Cursor der ersten Seite>: %v", a[1].Variables)
	}
	if a[0].Variables["owner"] != "example-user" || a[0].Variables["name"] != "infra" {
		t.Errorf("owner/name falsch: %v", a[0].Variables)
	}
	if !strings.Contains(a[0].Query, "$endCursor") {
		t.Errorf("die Query deklariert den Cursor nicht als $endCursor: %s", a[0].Query)
	}
}

// ghEinIssue is a one-node page with a chosen cursor and issue number.
func ghEinIssue(cursor string, nummer int) string {
	return fmt.Sprintf(`{"data":{"repository":{"issues":{"pageInfo":{"hasNextPage":true,"endCursor":%q},"nodes":[{"number":%d,"title":"x","url":"u","state":"OPEN","author":{"login":"a"},"labels":{"nodes":[]},"parent":null,"subIssuesSummary":{"total":0,"completed":0,"percentCompleted":0},"subIssues":{"totalCount":0,"nodes":[]}}]}}}}`, cursor, nummer)
}

// TestGhIssuesTerminatesOnARepeatedCursor: the failure to guard against is an
// endless loop, not an error. A server that hands back the cursor it was
// given must end the run, not feed it forever.
func TestGhIssuesTerminatesOnARepeatedCursor(t *testing.T) {
	// hasNextPage bleibt true, der Cursor bleibt derselbe: Seite 1 fuer immer.
	ghSeiten(t, ghEinIssue("immer-derselbe", 1))

	fertig := make(chan ghIssuesResult, 1)
	go func() {
		res, err := ghIssues(ghTestClient(), "o", "r")
		if err != nil {
			t.Errorf("ghIssues: %v", err)
		}
		fertig <- res
	}()
	select {
	case res := <-fertig:
		if !res.Truncated || res.Note == "" {
			t.Errorf("abgebrochene Paginierung muss sichtbar sein: %+v", res)
		}
		if len(res.Issues) == 0 {
			t.Errorf("das bereits Geladene darf nicht verworfen werden")
		}
	case <-time.After(20 * time.Second):
		t.Fatal("ghIssues terminiert nicht")
	}
}

// TestGhIssuesStopsAtThePageCap: same danger, other shape — every page brings a
// fresh cursor, so the repeat guard never fires. The cap is what terminates it,
// and the answer has to say that it is a prefix.
func TestGhIssuesStopsAtThePageCap(t *testing.T) {
	f := ghAPI(t, func(n int, w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, ghEinIssue(fmt.Sprintf("c%d", n), n))
	}, nil)
	res, err := ghIssues(ghTestClient(), "o", "r")
	if err != nil {
		t.Fatalf("ghIssues: %v", err)
	}
	if n := len(f.issueAnfragen()); n != ghIssuePageCap {
		t.Errorf("%d Aufrufe, erwartet den Deckel von %d", n, ghIssuePageCap)
	}
	if !res.Truncated || res.Note == "" {
		t.Errorf("am Deckel abgeschnitten, aber Truncated=%v Note=%q", res.Truncated, res.Note)
	}
	if len(res.Issues) != ghIssuePageCap {
		t.Errorf("%d Issues, erwartet %d (eines je Seite)", len(res.Issues), ghIssuePageCap)
	}
}

// TestGhIssuesFailsOpenOnEveryKindOfFailure: fail open is the load-bearing
// property of this file. Failures that look completely different on the wire
// have to end in the same place — an error a human can read, never a panic and
// never a list that looks complete.
func TestGhIssuesFailsOpenOnEveryKindOfFailure(t *testing.T) {
	faelle := []struct {
		name     string
		h        ghHandler
		enthaelt string
	}{
		{"API lehnt ab", ghFehler(http.StatusUnauthorized, "Bad credentials"), "Bad credentials"},
		{"Proxy mit HTML", func(_ int, w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadGateway)
			fmt.Fprint(w, "<html>proxy sagt nein\n</html>")
		}, "HTTP 502"},
		{"kein JSON", func(_ int, w http.ResponseWriter, _ *http.Request) {
			fmt.Fprint(w, "<html>proxy sagt nein</html>")
		}, "unlesbar"},
		{
			// GraphQL antwortet mit HTTP 200 und einem errors-Array. Ohne diese
			// Pruefung waere das eine leere Liste, die wie eine volle aussieht.
			"GraphQL-Fehler bei Status 200", func(_ int, w http.ResponseWriter, _ *http.Request) {
				fmt.Fprint(w, `{"data":{"repository":null},"errors":[{"message":"Could not resolve to a Repository with the name 'o/r'."}]}`)
			}, "Could not resolve",
		},
	}
	for _, f := range faelle {
		t.Run(f.name, func(t *testing.T) {
			ghAPI(t, f.h, nil)
			res, err := ghIssues(ghTestClient(), "o", "r")
			if err == nil {
				t.Fatalf("kein Fehler, stattdessen %+v", res)
			}
			if !strings.Contains(err.Error(), f.enthaelt) {
				t.Errorf("Fehler %q nennt %q nicht — dann kann niemand etwas tun", err, f.enthaelt)
			}
			if strings.Contains(err.Error(), "\n") {
				t.Errorf("Fehler ist mehrzeilig, die Karte hat eine Zeile: %q", err)
			}
		})
	}
}

// TestGhIssuesGivesUpOnAWedgedAPI: an endpoint that never answers must not
// hold the request open. Deadline, not hope.
func TestGhIssuesGivesUpOnAWedgedAPI(t *testing.T) {
	ghAPI(t, func(_ int, _ http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(30 * time.Second):
		}
	}, nil)
	ghKurzeFristen(t, 200, 2*time.Second)

	start := time.Now()
	_, err := ghIssues(ghTestClient(), "o", "r")
	dauer := time.Since(start)

	if err == nil || !strings.Contains(err.Error(), "nicht geantwortet") {
		t.Errorf("ein haengender Endpunkt muss einen Fristfehler geben, bekam %v", err)
	}
	if dauer > 10*time.Second {
		t.Errorf("Dauer %v — die Frist greift nicht", dauer)
	}
}

// ghTestRepo is a checkout whose origin points at GitHub, which is all
// CollectGitHub needs to get past its first fail-open gate.
func ghTestRepo(t *testing.T) string {
	t.Helper()
	return ghRepoMitOrigin(t, "https://github.com/example-user/infra.git")
}

// ghRepoMitOrigin is ghTestRepo with a freely chosen origin — since gkp-apb a
// non-GitHub remote is a case the card has to survive, so the host has to be a
// parameter rather than baked in.
func ghRepoMitOrigin(t *testing.T, origin string) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "--quiet"},
		{"remote", "add", "origin", origin},
	} {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v (%s)", args, err, out)
		}
	}
	return dir
}

// TestCollectGitHubCarriesIssuesAndCounts is the whole card against recorded
// answers: the shape the browser reads must keep working, and the counts have
// to be there.
func TestCollectGitHubCarriesIssuesAndCounts(t *testing.T) {
	t.Setenv("CC_GH_ACCOUNTS", "")
	t.Setenv("GITHUB_PAT_PERSONAL", "pat-privat-test")
	ghAPI(t, func(n int, w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, []string{ghSeite1, ghSeite2}[min(n, 2)-1])
	}, func(_ int, w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"data":{"repository":{"pullRequests":{"nodes":[
		  {"number":12,"title":"feat: x","state":"OPEN","updatedAt":"2026-09-04T10:00:00Z",
		   "url":"https://github.com/x/y/pull/12","author":null,"labels":{"nodes":[{"name":"wip","color":"fff"}]},
		   "isDraft":true,"headRefName":"feat/x","reviewDecision":"CHANGES_REQUESTED"}]}}}}`)
	})
	out := CollectGitHub(ghTestRepo(t))

	if out["available"] != true {
		t.Fatalf("available=%v, reason=%v", out["available"], out["reason"])
	}
	if out["nwo"] != "example-user/infra" || out["account"] != "GITHUB_PAT_PERSONAL" {
		t.Errorf("Kopf der Karte = %v / %v", out["nwo"], out["account"])
	}
	issues, _ := out["issues"].([]GhIssue)
	if len(issues) != 4 {
		t.Fatalf("%d Issues in der Karte", len(issues))
	}
	if out["issue_count"] != 4 || out["open_issue_count"] != 2 || out["closed_issue_count"] != 2 {
		t.Errorf("Zaehlwerte = %v/%v/%v, erwartet 4/2/2",
			out["issue_count"], out["open_issue_count"], out["closed_issue_count"])
	}
	if out["issues_truncated"] != false {
		t.Errorf("issues_truncated=%v, erwartet false", out["issues_truncated"])
	}
	if _, da := out["issues_error"]; da {
		t.Errorf("issues_error gesetzt, obwohl alles gelesen wurde: %v", out["issues_error"])
	}
	prs, _ := out["prs"].([]GhPR)
	if len(prs) != 1 || out["pr_count"] != 1 {
		t.Fatalf("prs=%+v pr_count=%v", prs, out["pr_count"])
	}
	if p := prs[0]; !p.IsDraft || p.HeadRefName != "feat/x" || p.ReviewDecision != "CHANGES_REQUESTED" ||
		len(p.Labels) != 1 || p.Labels[0].Name != "wip" || p.Author.Login != "" {
		t.Errorf("PR = %+v", p)
	}
}

// TestCollectGitHubFailsOpenWhenTheAPIFails covers the halves separately: one
// broken list must not hide the other, and a missing token or an unreachable
// API must still produce a card with a reason instead of a broken page.
func TestCollectGitHubFailsOpenWhenTheAPIFails(t *testing.T) {
	t.Run("nur die Issues scheitern", func(t *testing.T) {
		t.Setenv("CC_GH_ACCOUNTS", "")
		t.Setenv("GITHUB_PAT_PERSONAL", "pat-privat-test")
		ghAPI(t, ghFehler(http.StatusUnauthorized, "Bad credentials"), nil)
		out := CollectGitHub(ghTestRepo(t))
		if out["available"] != true {
			t.Fatalf("available=%v — die PRs waren zu lesen, die Karte bleibt da", out["available"])
		}
		if fehler, _ := out["issues_error"].(string); !strings.Contains(fehler, "401") {
			t.Errorf("issues_error=%q nennt den Grund nicht", fehler)
		}
	})

	t.Run("kein Token", func(t *testing.T) {
		t.Setenv("CC_GH_ACCOUNTS", "")
		f := ghSeiten(t, ghSeite2)
		out := CollectGitHub(ghTestRepo(t))
		if out["available"] != false {
			t.Fatalf("available=%v, erwartet false", out["available"])
		}
		if reason, _ := out["reason"].(string); reason != "GITHUB_PAT_PERSONAL ist nicht gesetzt" {
			t.Errorf("reason=%q", reason)
		}
		if n := len(f.alle()); n != 0 {
			t.Errorf("%d API-Aufrufe ohne Token, erwartet 0", n)
		}
	})

	t.Run("API nicht erreichbar", func(t *testing.T) {
		t.Setenv("CC_GH_ACCOUNTS", "")
		t.Setenv("GITHUB_PAT_PERSONAL", "pat-privat-test")
		srv := httptest.NewServer(http.NotFoundHandler())
		srv.Close() // Port ist jetzt zu: Verbindung abgelehnt
		alt := ghGraphQLURL
		ghGraphQLURL = srv.URL
		t.Cleanup(func() { ghGraphQLURL = alt })
		out := CollectGitHub(ghTestRepo(t))
		if out["available"] != false {
			t.Fatalf("available=%v, erwartet false", out["available"])
		}
		if reason, _ := out["reason"].(string); reason == "" {
			t.Error("available:false ohne Grund ist nicht handhabbar")
		}
	})
}

// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// gkp-ksb: `ssh -G` einmal je Alias, nicht einmal je Repo.
// ---------------------------------------------------------------------------

// sshCacheLeeren raeumt den Alias-Cache vor UND nach einem Test. Ein
// prozessweiter Cache haelt sonst die Antwort eines Fakes fest, der seit zwei
// Tests nicht mehr auf dem PATH liegt — und der naechste Test misst dann etwas,
// das er gar nicht aufgerufen hat.
func sshCacheLeeren(t *testing.T) {
	t.Helper()
	leeren := func() {
		sshAliasCache.Range(func(k, _ any) bool { sshAliasCache.Delete(k); return true })
	}
	leeren()
	t.Cleanup(leeren)
}

// sshFake legt ein `ssh` auf den PATH, das jeden Aufruf zaehlt und den Alias
// selbst in die Antwort schreibt. Beides ist noetig: der Zaehler beweist, dass
// ein zweiter Aufruf keinen Subprozess mehr startet, die alias-abhaengige
// Antwort beweist, dass der Cache je Alias trennt und nicht eine Antwort fuer
// alle merkt. Der Rueckgabewert ist das Verzeichnis mit `zaehler`.
func sshFake(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	fakeBinOnPath(t, "ssh", `n=$(cat `+dir+`/zaehler 2>/dev/null || echo 0)
n=$((n + 1))
printf %s "$n" > `+dir+`/zaehler
echo "hostname $2.example"`)
	return dir
}

// TestResolveSSHAliasCachtJeAlias ist gkp-ksb: gemessen kostet `ssh -G` ~26 ms,
// die Antwort je Alias ist konstant, und die Discovery loest denselben Alias
// einmal je Repo auf. Der Preis faellt im SYNCHRONEN Start (StartCache) an,
// also an der Stelle, an der ihn niemand misst.
func TestResolveSSHAliasCachtJeAlias(t *testing.T) {
	sshCacheLeeren(t)
	dir := sshFake(t)

	if got := resolveSSHAlias("github-work"); got != "github-work.example" {
		t.Fatalf("resolveSSHAlias(github-work) = %q — der Fake antwortet nicht", got)
	}
	if n := ghAufrufe(t, dir); n != 1 {
		t.Fatalf("%d ssh-Aufrufe nach dem ersten Aufloesen, erwartet 1", n)
	}

	// Der Kern des Tickets: derselbe Alias ein zweites Mal darf keinen
	// Subprozess mehr kosten.
	if got := resolveSSHAlias("github-work"); got != "github-work.example" {
		t.Errorf("zweiter Aufruf = %q, erwartet dieselbe Antwort", got)
	}
	if n := ghAufrufe(t, dir); n != 1 {
		t.Errorf("%d ssh-Aufrufe nach dem zweiten Aufloesen, erwartet weiterhin 1 — der Cache greift nicht", n)
	}

	// Ein anderer Alias ist eine andere Frage und kostet wieder einen Aufruf.
	if got := resolveSSHAlias("github-privat"); got != "github-privat.example" {
		t.Errorf("zweiter Alias = %q — der Cache antwortet fuer den falschen Schluessel", got)
	}
	if n := ghAufrufe(t, dir); n != 2 {
		t.Errorf("%d ssh-Aufrufe fuer zwei verschiedene Aliase, erwartet 2", n)
	}
}

// TestResolveSSHAliasCachtKeinenAusfall: ein gescheitertes `ssh -G` ist keine
// Antwort, sondern ein Ausfall. Ihn zu merken hiesse, einen voruebergehenden
// Fehler bis zum Neustart des Dienstes festzuschreiben — die Karte zeigte dann
// stunden- oder wochenlang den unaufgeloesten Alias als API-Host.
func TestResolveSSHAliasCachtKeinenAusfall(t *testing.T) {
	sshCacheLeeren(t)
	fakeBinOnPath(t, "ssh", `exit 255`)
	if got := resolveSSHAlias("github-work"); got != "github-work" {
		t.Fatalf("resolveSSHAlias = %q, erwartet den Alias unveraendert", got)
	}

	// Dasselbe ssh, jetzt antwortend: ohne neuen Aufruf bliebe der Ausfall
	// haengen.
	dir := sshFake(t)
	if got := resolveSSHAlias("github-work"); got != "github-work.example" {
		t.Errorf("nach dem Ausfall = %q — der Fehlschlag wurde gecacht", got)
	}
	if n := ghAufrufe(t, dir); n != 1 {
		t.Errorf("%d ssh-Aufrufe, erwartet 1", n)
	}
}

// TestResolveSSHAliasNebenlaeufig deckt den Fall ab, den der Cache erst
// moeglich macht: mehrere Requests loesen gleichzeitig auf. Unter -race ist das
// der Beweis, dass der gemeinsame Zustand traegt; ohne -race bleibt wenigstens
// die Zusicherung, dass alle dieselbe Antwort bekommen.
//
// Bewusst NICHT geprueft wird, dass genau ein Subprozess laeuft: die Sperre
// liegt absichtlich nicht um den `ssh`-Aufruf, zwei gleichzeitige Aufloeser
// duerfen doppelt zahlen. Das ist der billigere Fehler gegenueber einer Sperre,
// unter der jeder Alias auf jeden anderen wartet.
func TestResolveSSHAliasNebenlaeufig(t *testing.T) {
	sshCacheLeeren(t)
	dir := sshFake(t)

	const n = 8
	antworten := make(chan string, n)
	var start sync.WaitGroup
	start.Add(1)
	for i := 0; i < n; i++ {
		go func() {
			start.Wait() // alle zusammen losrennen lassen, nicht nacheinander
			antworten <- resolveSSHAlias("github-work")
		}()
	}
	start.Done()
	for i := 0; i < n; i++ {
		if got := <-antworten; got != "github-work.example" {
			t.Fatalf("nebenlaeufige Antwort %d = %q", i, got)
		}
	}
	// Doppelt zahlen ist erlaubt, n-mal zahlen hiesse, dass der Cache gar
	// nicht greift.
	if rufe := ghAufrufe(t, dir); rufe > n {
		t.Errorf("%d ssh-Aufrufe bei %d Aufloesern", rufe, n)
	}
}

// ---------------------------------------------------------------------------
// gkp-apb: jedes parsbare origin wird aufgenommen — dafuer muss die leere
// GitHub-Karte sagen, WARUM sie leer ist.
// ---------------------------------------------------------------------------

// TestCollectGitHubBenenntEinenNichtGitHubHost haelt die Bedingung fest, unter
// der die Entscheidung aus gkp-apb ueberhaupt gilt. Entschieden wurde (a): ein
// Repo mit GitLab-origin bleibt ein Projekt und taucht in der Liste auf. Der
// Preis dafuer ist eine leere GitHub-Karte — und der ist nur bezahlbar, wenn
// die Karte den Host nennt statt stumm zu bleiben.
//
// Der alte Text ("kein gh-Konto für Host ... hinterlegt") nennt den Host zwar,
// erzaehlt aber die falsche Geschichte: er schickt den Leser in die
// Kontotabelle, wo es nichts einzutragen gibt.
func TestCollectGitHubBenenntEinenNichtGitHubHost(t *testing.T) {
	t.Setenv("CC_GH_ACCOUNTS", "")
	sshCacheLeeren(t)
	fakeBinOnPath(t, "ssh", `echo "hostname gitlab.com"`)

	out := CollectGitHub(ghRepoMitOrigin(t, "git@gitlab.com:acme/infra.git"))

	if out["available"] != false {
		t.Fatalf("available=%v, erwartet false", out["available"])
	}
	grund, _ := out["reason"].(string)
	if !strings.Contains(grund, "gitlab.com") {
		t.Errorf("reason=%q nennt den Host nicht — genau das war die Bedingung aus gkp-apb", grund)
	}
	if !strings.Contains(grund, "kein GitHub-Remote") {
		t.Errorf("reason=%q sagt nicht, dass das gar kein GitHub-Remote ist", grund)
	}
	if strings.Contains(grund, "CC_GH_ACCOUNTS") {
		t.Errorf("reason=%q schickt den Leser in die Kontotabelle, wo nichts einzutragen ist", grund)
	}
	// Der Kopf der Karte bleibt stehen: das Projekt ist da, die Karte ist leer,
	// der Grund steht daneben. Genau das ist der SICHTBARE Irrtum aus (a).
	if out["nwo"] != "acme/infra" || out["host"] != "gitlab.com" {
		t.Errorf("Kopf der Karte = %v / %v", out["nwo"], out["host"])
	}
}

// TestCollectGitHubUnterscheidetFehlendesKontoVomFremdenHost: derselbe Zweig im
// Code, zwei voellig verschiedene Geschichten. Ein github.com-Remote ohne
// Kontoeintrag ist ein Konfigurationsfehler, den man beheben KANN — dieser Fall
// darf durch die neue Meldung nicht verloren gehen.
func TestCollectGitHubUnterscheidetFehlendesKontoVomFremdenHost(t *testing.T) {
	t.Setenv("CC_GH_ACCOUNTS", "ghe.intern=work-acct") // github.com faellt heraus
	sshCacheLeeren(t)

	out := CollectGitHub(ghTestRepo(t))

	if out["available"] != false {
		t.Fatalf("available=%v, erwartet false", out["available"])
	}
	grund, _ := out["reason"].(string)
	if !strings.Contains(grund, "CC_GH_ACCOUNTS") {
		t.Errorf("reason=%q — bei github.com ist die Kontotabelle die richtige Adresse", grund)
	}
	if strings.Contains(grund, "kein GitHub-Remote") {
		t.Errorf("reason=%q — github.com IST ein GitHub-Remote", grund)
	}
}
