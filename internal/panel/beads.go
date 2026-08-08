package panel

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/niclasedge/git-planner-go/internal/config"
)

// ---------------------------------------------------------------------------
// beads — issue trees from a repo's committed .beads/issues.jsonl
// ---------------------------------------------------------------------------

// Beads renders the beads issue tracker (github.com/gastownhall/beads) of the
// listed repos. It reads the committed JSONL export via the contents API — the
// Dolt database itself never reaches GitHub as a readable file, the export is
// the one surface a viewer may consume (`export.auto` in .beads/config.yaml).
//
// Repos come from two places. `repos:` pins them: always on the page, in config
// order, and a pinned repo without beads shows up as "keine Beads-DB" instead of
// disappearing — a typo in the list should be visible, not silent. `discover:
// true` adds every repo the token can see that carries a .beads/ directory.
//
// Discovery is a probe with a remembered result, the same trade git-planner-ios
// makes in Repository.beadsState: a 404 is never free the way a 304 is, so
// asking 240 repos every five minutes is out of the question, while asking them
// once a day costs nothing worth counting. Repos already known to carry an
// export are skipped entirely — their conditional export fetch is the liveness
// check.
type Beads struct {
	Base  `yaml:",inline"`
	Repos []string `yaml:"repos"`
	// TokenEnv names the environment variable holding the PAT, following the
	// semaphore widget: the credential lives in .env, config.yaml is committed.
	TokenEnv string `yaml:"token-env"`
	// Path within the repo, overridable for a non-default export location.
	Path string `yaml:"path"`
	// Discover turns on the repo sweep described above.
	Discover bool `yaml:"discover"`
	// DiscoverEvery is how often that sweep repeats; 24h when unset.
	DiscoverEvery config.Duration `yaml:"discover-interval"`

	token   string
	credErr error
	client  *http.Client
	// apiBase is swapped for a test server in tests; the GitHub API otherwise.
	apiBase string
	// state is one entry per rendered repo: the pinned ones in config order
	// first, discovered ones after them by name. Fetch bookkeeping (etag) lives
	// here so a 304 can keep the parsed tree.
	state []*BeadsRepo
	// pinned is the `repos:` set, for the flag on each section.
	pinned map[string]bool
	// lastDiscover is zero until the first sweep; discoverErr is reported as the
	// widget error without touching the sections a previous sweep found.
	lastDiscover time.Time
	discoverErr  string
}

// BeadsRepo is one repo's parsed tree plus its fetch state.
type BeadsRepo struct {
	Name string // owner/repo
	// Pinned marks a repo named in `repos:` — it stays on the page whatever the
	// probe says.
	Pinned bool
	// Missing means there is no beads database here at all. NoExport is the
	// other half of that answer and the reason the probe looks at the directory
	// rather than at the file: .beads/ exists, so this repo *does* track its work
	// in beads, but the Dolt database is all there is and no viewer can read it.
	// Err is any other fetch or parse failure; old data stays on the page
	// alongside it.
	Missing  bool
	NoExport bool
	Err      string

	Roots  []*Bead
	Open   int // open + in_progress, the number worth glancing at
	Ready  int // open, unblocked, no open children
	Closed int
	// ReadyList is the actionable set — the Ready count as rows, so the page
	// can show "was kann ich jetzt tun" at the top of the tree.
	ReadyList []*Bead
	// All is every open bead in export order, flat. It is what the detail pane
	// renders from: walking Roots reaches only two levels deep, and a bead in a
	// blocker cycle is dropped from Roots entirely (each of the two ends up as
	// the other's waiter), so a tree walk cannot promise every bead has a detail
	// article. The agenda links across repos and needs that promise.
	All []*Bead

	etag string
}

// Bead is one issue as the template renders it.
type Bead struct {
	ID       string
	Title    string
	Status   string // open | in_progress | closed
	Type     string // task | bug | feature | epic | chore | decision
	Priority int
	Labels   []string
	// Description is the issue body, rendered in the detail pane.
	Description string
	// Repo is the owner/repo this bead came from, so a detail article does not
	// need the enclosing repo's scope.
	Repo string
	// GHURL links the migrated GitHub issue, derived from an external_ref of
	// the form "gh-<n>". Other ref forms are kept as text only.
	GHURL string
	// Due is the target date from `bd --due`, nil when the bead carries none.
	// A bead without a date must look exactly as it did before this field existed.
	Due       *time.Time
	Children  []*Bead
	BlockedBy []string // IDs of the open issues blocking this one
	// Waiters are the beads that wait on this one (their primary blocker is
	// this bead). The tree renders them nested underneath — same indent as
	// children — so the order reads "erst dieses, dann jenes".
	Waiters []*Bead
}

// hasExport is "this repo answered with a parsed export at least once". It is
// what lets a discovery round skip a repo: the conditional fetch already tells
// us every round whether the file is still there.
func (r *BeadsRepo) hasExport() bool {
	return !r.Missing && !r.NoExport && r.etag != ""
}

func (b *Bead) Blocked() bool { return len(b.BlockedBy) > 0 }

// RepoShort drops the owner, for the agenda row that has to name its repo in the
// width of a chip. Same reduction as BeadsRepo.Short, on the bead itself because
// an agenda row is not inside a repo's scope.
func (b *Bead) RepoShort() string {
	if i := strings.LastIndex(b.Repo, "/"); i >= 0 && i+1 < len(b.Repo) {
		return b.Repo[i+1:]
	}
	return b.Repo
}

// Dot maps the bead onto the status vocabulary the other pages use, so the
// colours mean the same thing everywhere.
//
// Blocked is its own state, not "queued". Borrowing o-queued gave a waiting bead
// a filled light dot, which next to an actionable bead's hollow ring read as
// *more* finished — the opposite of the truth. o-blocked is a hollow amber ring:
// hollow says "not done", amber says "waiting on something else".
func (b *Bead) Dot() string {
	switch {
	case b.Status == "closed":
		return "success"
	case b.Blocked():
		return "blocked"
	case b.Status == "in_progress":
		return "running"
	default:
		return "idle"
	}
}

// beadRecord is the JSONL line shape of `bd export` (schema/issues.jsonl).
type beadRecord struct {
	RecType     string   `json:"_type"`
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Status      string   `json:"status"`
	Priority    int      `json:"priority"`
	IssueType   string   `json:"issue_type"`
	Labels      []string `json:"labels"`
	ExternalRef string   `json:"external_ref"`
	// DueAt is `bd create --due` as RFC3339. Its sibling `--defer` is stored in
	// the Dolt DB but omitted by `bd export` 1.1.2, so a deferred date cannot be
	// shown here at all — the DB is off-limits to consumers by design.
	DueAt        string `json:"due_at"`
	Dependencies []struct {
		DependsOnID string `json:"depends_on_id"`
		Type        string `json:"type"`
	} `json:"dependencies"`
}

func (b *Beads) Kind() string { return "beads" }

func (b *Beads) Init() error {
	if len(b.Repos) == 0 && !b.Discover {
		return fmt.Errorf("beads needs at least one repo (owner/repo) or discover: true")
	}
	for _, r := range b.Repos {
		if strings.Count(r, "/") != 1 {
			return fmt.Errorf("beads repo %q is not owner/repo", r)
		}
	}
	if b.WTitle == "" {
		b.WTitle = "Beads"
	}
	if b.Path == "" {
		b.Path = ".beads/issues.jsonl"
	}
	b.client = &http.Client{Timeout: 15 * time.Second}
	if b.apiBase == "" {
		b.apiBase = "https://api.github.com"
	}
	b.state = make([]*BeadsRepo, len(b.Repos))
	b.pinned = make(map[string]bool, len(b.Repos))
	for i, r := range b.Repos {
		b.state[i] = &BeadsRepo{Name: r, Pinned: true}
		b.pinned[r] = true
	}

	// Same contract as the semaphore widget: a missing credential is a banner
	// naming the variable, not a widget that silently disappeared. Private
	// repos are the normal case here, so the token is required up front.
	if b.TokenEnv == "" {
		return fmt.Errorf("beads needs token-env")
	}
	if b.token = os.Getenv(b.TokenEnv); b.token == "" {
		b.credErr = fmt.Errorf("%s ist leer — Token in .env eintragen", b.TokenEnv)
	}
	return nil
}

// Update fetches before it discovers, so the pinned repos are on the page while
// a first sweep is still walking a few hundred 404s. Sections found by that
// sweep are fetched in the same round — otherwise a new repo would sit there
// empty until the next tick.
func (b *Beads) Update(ctx context.Context) {
	if b.credErr != nil {
		b.done(b.credErr)
		return
	}
	b.fetchAll(ctx)
	if b.Discover && b.discoverDue(time.Now()) {
		b.discover(ctx)
		b.fetchAll(ctx)
	}
	if b.discoverErr != "" {
		b.done(fmt.Errorf("%s", b.discoverErr))
		return
	}
	b.done(nil)
}

// fetchAll refreshes every section that can carry an export. Missing and
// NoExport are probe results, not fetch results — re-asking them here would put
// back exactly the per-round 404 the discovery interval exists to avoid.
func (b *Beads) fetchAll(ctx context.Context) {
	for _, st := range b.Sections() {
		if st.Missing || st.NoExport {
			continue
		}
		b.fetchRepo(ctx, st)
	}
}

// beadsDiscoverInterval is the default sweep period. A day is short enough to
// notice a new beads repo without being asked and long enough that the 404s it
// pays disappear against a 5000/h budget.
const beadsDiscoverInterval = 24 * time.Hour

func (b *Beads) discoverEvery() time.Duration {
	if d := b.DiscoverEvery.Std(); d > 0 {
		return d
	}
	return beadsDiscoverInterval
}

func (b *Beads) discoverDue(now time.Time) bool {
	b.mu.RLock()
	last := b.lastDiscover
	b.mu.RUnlock()
	return last.IsZero() || now.Sub(last) >= b.discoverEvery()
}

// Sections is what the template ranges over. The slice itself is fixed at
// Init; entries are replaced whole under the lock, never mutated in place.
func (b *Beads) Sections() []*BeadsRepo {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.state
}

// fetchRepo is one conditional request. 200 parses and stores a new etag, 304
// keeps everything, 404 means "no beads db here", anything else keeps the old
// tree and reports the error beside it.
func (b *Beads) fetchRepo(ctx context.Context, st *BeadsRepo) {
	url := b.apiBase + "/repos/" + st.Name + "/contents/" + b.Path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		b.setErr(st, err.Error())
		return
	}
	// The raw media type skips the base64 JSON wrapper and is the documented
	// way to read files above 1 MB.
	req.Header.Set("Accept", "application/vnd.github.raw+json")
	req.Header.Set("Authorization", "Bearer "+b.token)
	req.Header.Set("User-Agent", "git-planner-go/beads")
	if st.etag != "" {
		req.Header.Set("If-None-Match", st.etag)
	}

	resp, err := b.client.Do(req)
	if err != nil {
		b.setErr(st, err.Error())
		return
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusNotModified:
		io.Copy(io.Discard, resp.Body)
		b.setErr(st, "")
		return
	case http.StatusNotFound:
		// With discovery on, this repo only got here because a probe saw its
		// .beads/ directory — so a vanished export is "DB ohne Export", not
		// "kein beads". Without discovery there is nothing to tell the two
		// apart, and the older, blunter answer stands.
		b.mu.Lock()
		st.Missing, st.NoExport, st.Err = !b.Discover, b.Discover, ""
		st.Roots, st.ReadyList, st.All = nil, nil, nil
		st.Open, st.Ready, st.Closed = 0, 0, 0
		st.etag = ""
		b.mu.Unlock()
		return
	case http.StatusOK:
	default:
		io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<10))
		b.setErr(st, fmt.Sprintf("GET %s: HTTP %d", b.Path, resp.StatusCode))
		return
	}

	roots, readyList, all, open, ready, closed, err := parseBeads(resp.Body, st.Name)
	if err != nil {
		b.setErr(st, err.Error())
		return
	}
	b.mu.Lock()
	st.Roots, st.ReadyList, st.All = roots, readyList, all
	st.Open, st.Ready, st.Closed = open, ready, closed
	st.Missing, st.Err = false, ""
	st.etag = resp.Header.Get("ETag")
	b.mu.Unlock()
}

func (b *Beads) setErr(st *BeadsRepo, msg string) {
	b.mu.Lock()
	st.Err = msg
	b.mu.Unlock()
}

// ---------------------------------------------------------------------------
// discovery — which repos track their work in beads at all
// ---------------------------------------------------------------------------

// beadsProbe is what one look at a repo's .beads/ directory can tell us.
type beadsProbe int

const (
	beadsUnknown  beadsProbe = iota // the request failed; say nothing
	beadsNone                       // no .beads/ — this repo does not use beads
	beadsDBOnly                     // .beads/ exists, but no committed export
	beadsExported                   // .beads/ exists and carries the export
)

// discover rebuilds the section list: pinned repos in config order, then every
// discovered repo that carries a .beads/ directory, by name.
//
// A failed listing keeps the previous sections rather than emptying the page —
// the same rule the fetch follows for a 500.
func (b *Beads) discover(ctx context.Context) {
	names, err := b.listRepos(ctx)
	if err != nil {
		b.mu.Lock()
		b.discoverErr = "Repo-Discovery: " + err.Error()
		b.mu.Unlock()
		return
	}

	b.mu.RLock()
	prev := make(map[string]*BeadsRepo, len(b.state))
	for _, st := range b.state {
		prev[st.Name] = st
	}
	b.mu.RUnlock()

	// Pinned first and in config order: the first section is the page's default
	// tab, and a tab that reorders itself between refreshes is not a tab.
	order := append([]string{}, b.Repos...)
	rest := make([]string, 0, len(names))
	for _, n := range names {
		if !b.pinned[n] {
			rest = append(rest, n)
		}
	}
	sort.Strings(rest)
	order = append(order, rest...)

	next := make([]*BeadsRepo, 0, len(order))
	for _, name := range order {
		st := prev[name]
		if st == nil {
			st = &BeadsRepo{Name: name}
		}
		st.Pinned = b.pinned[name]

		if st.hasExport() {
			next = append(next, st) // its conditional fetch is the check
			continue
		}

		switch b.probeBeadsDir(ctx, name) {
		case beadsExported:
			b.mu.Lock()
			st.Missing, st.NoExport = false, false
			b.mu.Unlock()
			next = append(next, st)
		case beadsDBOnly:
			b.mu.Lock()
			st.Missing, st.NoExport = false, true
			st.Roots, st.ReadyList, st.All = nil, nil, nil
			st.Open, st.Ready, st.Closed = 0, 0, 0
			st.etag = ""
			b.mu.Unlock()
			next = append(next, st)
		case beadsNone:
			b.mu.Lock()
			st.Missing, st.NoExport = true, false
			st.Roots, st.ReadyList, st.All = nil, nil, nil
			st.Open, st.Ready, st.Closed = 0, 0, 0
			st.etag = ""
			b.mu.Unlock()
			if st.Pinned {
				next = append(next, st) // a typo must stay visible
			}
		default: // beadsUnknown — keep whatever we already believed
			if st.Pinned || prev[name] != nil {
				next = append(next, st)
			}
		}
	}

	b.mu.Lock()
	b.state = next
	b.lastDiscover = time.Now()
	b.discoverErr = ""
	b.mu.Unlock()
}

// probeBeadsDir asks for the *directory* listing, not the export. One request
// then answers both questions at once: does this repo keep a beads (Dolt)
// database, and is there something a viewer can read? Probing the file alone
// would collapse "kein beads" and "beads ohne Export" into the same 404, and
// those two want different words on the page.
func (b *Beads) probeBeadsDir(ctx context.Context, repo string) beadsProbe {
	dir, file := path.Split(b.Path)
	dir = strings.TrimSuffix(dir, "/")
	if dir == "" {
		// An export configured at the repo root has no directory to ask about.
		return beadsUnknown
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.apiBase+"/repos/"+repo+"/contents/"+dir, nil)
	if err != nil {
		return beadsUnknown
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+b.token)
	req.Header.Set("User-Agent", "git-planner-go/beads")

	resp, err := b.client.Do(req)
	if err != nil {
		return beadsUnknown
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusNotFound:
		io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<10))
		return beadsNone
	case http.StatusOK:
	default:
		io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<10))
		return beadsUnknown
	}

	var entries []struct {
		Name string `json:"name"`
		Type string `json:"type"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&entries); err != nil {
		return beadsUnknown
	}
	for _, e := range entries {
		if e.Name == file && e.Type == "file" {
			return beadsExported
		}
	}
	return beadsDBOnly
}

// beadsRepoPages caps discovery at 300 repos, the same ceiling gh.discoverRepos
// uses. Past that, name repos explicitly.
const beadsRepoPages = 3

// listRepos returns every non-archived repo the token can see, as owner/repo.
//
// It pages with an explicit &page=N rather than following the Link header, for
// the reason gh.discoverRepos records: GitHub sends no Link on a 304, so a
// header-driven chain silently shrinks to the first hundred once a cache is warm.
func (b *Beads) listRepos(ctx context.Context) ([]string, error) {
	const perPage = 100
	var out []string

	for page := 1; page <= beadsRepoPages; page++ {
		url := fmt.Sprintf("%s/user/repos?per_page=%d&page=%d&sort=pushed&direction=desc"+
			"&affiliation=owner,collaborator,organization_member", b.apiBase, perPage, page)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("Authorization", "Bearer "+b.token)
		req.Header.Set("User-Agent", "git-planner-go/beads")

		resp, err := b.client.Do(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<10))
			resp.Body.Close()
			return nil, fmt.Errorf("GET /user/repos: HTTP %d", resp.StatusCode)
		}

		var batch []struct {
			FullName string `json:"full_name"`
			Archived bool   `json:"archived"`
			Disabled bool   `json:"disabled"`
		}
		err = json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&batch)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("GET /user/repos: %w", err)
		}
		for _, r := range batch {
			if r.Archived || r.Disabled || strings.Count(r.FullName, "/") != 1 {
				continue
			}
			out = append(out, r.FullName)
		}
		if len(batch) < perPage {
			break
		}
	}
	return out, nil
}

// URL is the repo on GitHub, for the section heading.
func (r *BeadsRepo) URL() string { return "https://github.com/" + r.Name }

// Short drops the owner. The rail has room for "niclasedge/IaC-Stack"; the phone
// tab strip does not, and the owner is the same for every repo listed anyway.
func (r *BeadsRepo) Short() string {
	if i := strings.LastIndex(r.Name, "/"); i >= 0 && i+1 < len(r.Name) {
		return r.Name[i+1:]
	}
	return r.Name
}

// beadAgendaLimit caps the upcoming list for the same reason the planner's
// agenda does: the section answers "what is close", not "everything dated".
const beadAgendaLimit = 25

// BeadAgenda is every dated bead of every configured repo, grouped into overdue
// and upcoming.
//
// It is deliberately cross-repo and sits outside the repo switch: a date is only
// a delivery if it reaches you without being asked for, and an agenda scoped to
// the selected repo would hide an overdue bead in the other one. beads stores
// dates but tells nobody — this section is the telling.
type BeadAgenda struct {
	Groups []BeadAgendaGroup
	Total  int
}

// BeadAgendaGroup holds beads, not a flattened copy of them, so a click reaches
// the same detail article the tree opens.
type BeadAgendaGroup struct {
	Title   string
	Overdue bool
	Items   []*Bead
}

// Agenda returns nil when nothing carries a date — the page then looks exactly
// as it did before the section existed.
func (b *Beads) Agenda() *BeadAgenda {
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)

	b.mu.RLock()
	var dated []*Bead
	for _, st := range b.state {
		for _, bd := range st.All {
			if bd.Due != nil {
				dated = append(dated, bd)
			}
		}
	}
	b.mu.RUnlock()

	if len(dated) == 0 {
		return nil
	}
	sort.SliceStable(dated, func(i, j int) bool {
		if !dated[i].Due.Equal(*dated[j].Due) {
			return dated[i].Due.Before(*dated[j].Due)
		}
		if dated[i].Repo != dated[j].Repo {
			return dated[i].Repo < dated[j].Repo
		}
		return dated[i].ID < dated[j].ID
	})

	ag := &BeadAgenda{Total: len(dated)}
	var overdue, upcoming BeadAgendaGroup
	overdue.Overdue = true
	for _, bd := range dated {
		if bd.Due.Before(today) {
			overdue.Items = append(overdue.Items, bd)
			continue
		}
		if len(upcoming.Items) < beadAgendaLimit {
			upcoming.Items = append(upcoming.Items, bd)
		}
	}
	if len(overdue.Items) > 0 {
		overdue.Title = "Überfällig"
		ag.Groups = append(ag.Groups, overdue)
	}
	if len(upcoming.Items) > 0 {
		upcoming.Title = "Anstehend"
		ag.Groups = append(ag.Groups, upcoming)
	}
	return ag
}

// parseDue normalises bd's RFC3339 stamp to midnight of the local calendar day.
// Only the day is a deadline here: kept as a clock time, a bead due 08:03 would
// read as overdue for the rest of that same day. UTC midnight matches how the
// planner's agenda builds "today", so both use one date arithmetic.
func parseDue(s string) *time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return nil // a date we cannot read is not a reason to drop the bead
	}
	t = t.Local()
	day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	return &day
}

// parseBeads turns the JSONL export into render-ready trees. Closed issues are
// counted but not shown: the page answers "what is there to do", and beads'
// own compaction will eventually retire them from the export anyway.
func parseBeads(body io.Reader, repo string) (roots, readyList, all []*Bead, open, ready, closed int, err error) {
	byID := map[string]*Bead{}
	parent := map[string]string{}     // child id → parent id
	blockers := map[string][]string{} // id → ids it depends on (type blocks)
	status := map[string]string{}     // id → status, blockers need it for closed ones too
	var order []string                // export order, the stable tiebreak

	sc := bufio.NewScanner(body)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024) // one issue per line, bodies included
	line := 0
	for sc.Scan() {
		line++
		raw := strings.TrimSpace(sc.Text())
		if raw == "" {
			continue
		}
		var rec beadRecord
		if err := json.Unmarshal([]byte(raw), &rec); err != nil {
			return nil, nil, nil, 0, 0, 0, fmt.Errorf("issues.jsonl line %d: %w", line, err)
		}
		if rec.RecType != "" && rec.RecType != "issue" {
			continue // messages, agents and other infrastructure beads
		}
		status[rec.ID] = rec.Status
		for _, d := range rec.Dependencies {
			switch d.Type {
			case "parent-child":
				parent[rec.ID] = d.DependsOnID
			case "blocks":
				blockers[rec.ID] = append(blockers[rec.ID], d.DependsOnID)
			}
		}
		if rec.Status == "closed" {
			closed++
			continue
		}
		open++
		bd := &Bead{
			ID:          rec.ID,
			Title:       rec.Title,
			Description: rec.Description,
			Repo:        repo,
			Status:      rec.Status,
			Type:        rec.IssueType,
			Priority:    rec.Priority,
			Labels:      rec.Labels,
			GHURL:       ghIssueURL(repo, rec.ExternalRef),
			Due:         parseDue(rec.DueAt),
		}
		byID[rec.ID] = bd
		all = append(all, bd)
		order = append(order, rec.ID)
	}
	if err := sc.Err(); err != nil {
		return nil, nil, nil, 0, 0, 0, fmt.Errorf("reading issues.jsonl: %w", err)
	}

	for _, id := range order {
		bd := byID[id]
		for _, dep := range blockers[id] {
			// A closed blocker blocks nothing; a blocker missing from the
			// export (compacted away) cannot be open either.
			if s, ok := status[dep]; ok && s != "closed" {
				bd.BlockedBy = append(bd.BlockedBy, dep)
			}
		}
		if p, ok := byID[parent[id]]; ok {
			p.Children = append(p.Children, bd)
			continue
		}
		roots = append(roots, bd)
	}

	// Nest each blocked bead under its primary blocker and take it out of its
	// normal tree spot, so every bead shows up once and the dependency reads
	// as an order: the blocker row sits above, the waiting row indented with a
	// down-right arrow below it. A blocked bead that still has no visible
	// blocker stays where it is.
	for _, id := range order {
		bd := byID[id]
		if len(bd.BlockedBy) == 0 {
			continue
		}
		blocker := byID[bd.BlockedBy[0]]
		if blocker == nil || blocker == bd {
			continue
		}
		blocker.Waiters = append(blocker.Waiters, bd)
		if p := byID[parent[id]]; p != nil {
			p.Children = removeBead(p.Children, id)
		} else {
			roots = removeBead(roots, id)
		}
	}

	sortBeads(roots)
	for _, r := range roots {
		sortBeads(r.Children)
	}
	for _, id := range order {
		bd := byID[id]
		sortBeads(bd.Waiters)
		if !bd.Blocked() && bd.Status == "open" && len(bd.Children) == 0 {
			ready++
			readyList = append(readyList, bd)
		}
	}
	sortBeads(readyList)
	return roots, readyList, all, open, ready, closed, nil
}

// removeBead drops the first bead with the given id from the slice. It is what
// lets a blocked bead move from its parent-child spot to its waiter nest.
func removeBead(list []*Bead, id string) []*Bead {
	for i, b := range list {
		if b.ID == id {
			return append(list[:i], list[i+1:]...)
		}
	}
	return list
}

// sortBeads orders siblings: epics first (they carry their tasks), then by
// priority, then by ID for a stable order between refreshes.
func sortBeads(list []*Bead) {
	sort.SliceStable(list, func(i, j int) bool {
		ei, ej := list[i].Type == "epic", list[j].Type == "epic"
		if ei != ej {
			return ei
		}
		if list[i].Priority != list[j].Priority {
			return list[i].Priority < list[j].Priority
		}
		return list[i].ID < list[j].ID
	})
}

// ghIssueURL resolves the "gh-<n>" external_ref convention this stack uses for
// migrated issues. Anything else is not a URL we can vouch for.
func ghIssueURL(repo, ref string) string {
	n, ok := strings.CutPrefix(ref, "gh-")
	if !ok || n == "" {
		return ""
	}
	for _, r := range n {
		if r < '0' || r > '9' {
			return ""
		}
	}
	return "https://github.com/" + repo + "/issues/" + n
}
