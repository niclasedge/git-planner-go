package panel

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// beads — issue trees from a repo's committed .beads/issues.jsonl
// ---------------------------------------------------------------------------

// Beads renders the beads issue tracker (github.com/gastownhall/beads) of the
// listed repos. It reads the committed JSONL export via the contents API — the
// Dolt database itself never reaches GitHub as a readable file, the export is
// the one surface a viewer may consume (`export.auto` in .beads/config.yaml).
//
// The repo list is explicit rather than discovered: probing every repo for
// .beads/issues.jsonl would pay a 404 per repo per round, and unlike a 304 a
// 404 is never free. A repo without the file shows up as "keine Beads-DB"
// instead of disappearing — a typo in the list should be visible, not silent.
type Beads struct {
	Base  `yaml:",inline"`
	Repos []string `yaml:"repos"`
	// TokenEnv names the environment variable holding the PAT, following the
	// semaphore widget: the credential lives in .env, config.yaml is committed.
	TokenEnv string `yaml:"token-env"`
	// Path within the repo, overridable for a non-default export location.
	Path string `yaml:"path"`

	token   string
	credErr error
	client  *http.Client
	// apiBase is swapped for a test server in tests; the GitHub API otherwise.
	apiBase string
	// state is one entry per configured repo, same order. Fetch bookkeeping
	// (etag) lives here so a 304 can keep the parsed tree.
	state []*BeadsRepo
}

// BeadsRepo is one repo's parsed tree plus its fetch state.
type BeadsRepo struct {
	Name string // owner/repo
	// Missing means GitHub answered 404: no committed export. Err is any other
	// fetch or parse failure; old data stays on the page alongside it.
	Missing bool
	Err     string

	Roots  []*Bead
	Open   int // open + in_progress, the number worth glancing at
	Ready  int // open, unblocked, no open children
	Closed int
	// ReadyList is the actionable set — the Ready count as rows, so the page
	// can show "was kann ich jetzt tun" at the top of the tree.
	ReadyList []*Bead

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
	GHURL     string
	Children  []*Bead
	BlockedBy []string // IDs of the open issues blocking this one
	// Waiters are the beads that wait on this one (their primary blocker is
	// this bead). The tree renders them nested underneath — same indent as
	// children — so the order reads "erst dieses, dann jenes".
	Waiters []*Bead
}

func (b *Bead) Blocked() bool { return len(b.BlockedBy) > 0 }

// Dot maps the bead onto the status vocabulary the other pages use, so the
// colours mean the same thing everywhere.
func (b *Bead) Dot() string {
	switch {
	case b.Status == "closed":
		return "success"
	case b.Blocked():
		return "queued"
	case b.Status == "in_progress":
		return "running"
	default:
		return "idle"
	}
}

// beadRecord is the JSONL line shape of `bd export` (schema/issues.jsonl).
type beadRecord struct {
	RecType      string   `json:"_type"`
	ID           string   `json:"id"`
	Title        string   `json:"title"`
	Description  string   `json:"description"`
	Status       string   `json:"status"`
	Priority     int      `json:"priority"`
	IssueType    string   `json:"issue_type"`
	Labels       []string `json:"labels"`
	ExternalRef  string   `json:"external_ref"`
	Dependencies []struct {
		DependsOnID string `json:"depends_on_id"`
		Type        string `json:"type"`
	} `json:"dependencies"`
}

func (b *Beads) Kind() string { return "beads" }

func (b *Beads) Init() error {
	if len(b.Repos) == 0 {
		return fmt.Errorf("beads needs at least one repo (owner/repo)")
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
	for i, r := range b.Repos {
		b.state[i] = &BeadsRepo{Name: r}
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

func (b *Beads) Update(ctx context.Context) {
	if b.credErr != nil {
		b.done(b.credErr)
		return
	}
	for _, st := range b.state {
		b.fetchRepo(ctx, st)
	}
	b.done(nil)
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
		b.mu.Lock()
		st.Missing, st.Err = true, ""
		st.Roots, st.ReadyList, st.Open, st.Ready, st.Closed = nil, nil, 0, 0, 0
		b.mu.Unlock()
		return
	case http.StatusOK:
	default:
		io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<10))
		b.setErr(st, fmt.Sprintf("GET %s: HTTP %d", b.Path, resp.StatusCode))
		return
	}

	roots, readyList, open, ready, closed, err := parseBeads(resp.Body, st.Name)
	if err != nil {
		b.setErr(st, err.Error())
		return
	}
	b.mu.Lock()
	st.Roots, st.ReadyList, st.Open, st.Ready, st.Closed = roots, readyList, open, ready, closed
	st.Missing, st.Err = false, ""
	st.etag = resp.Header.Get("ETag")
	b.mu.Unlock()
}

func (b *Beads) setErr(st *BeadsRepo, msg string) {
	b.mu.Lock()
	st.Err = msg
	b.mu.Unlock()
}

// URL is the repo on GitHub, for the section heading.
func (r *BeadsRepo) URL() string { return "https://github.com/" + r.Name }

// parseBeads turns the JSONL export into render-ready trees. Closed issues are
// counted but not shown: the page answers "what is there to do", and beads'
// own compaction will eventually retire them from the export anyway.
func parseBeads(body io.Reader, repo string) (roots, readyList []*Bead, open, ready, closed int, err error) {
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
			return nil, nil, 0, 0, 0, fmt.Errorf("issues.jsonl line %d: %w", line, err)
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
		byID[rec.ID] = &Bead{
			ID:          rec.ID,
			Title:       rec.Title,
			Description: rec.Description,
			Repo:        repo,
			Status:      rec.Status,
			Type:        rec.IssueType,
			Priority:    rec.Priority,
			Labels:      rec.Labels,
			GHURL:       ghIssueURL(repo, rec.ExternalRef),
		}
		order = append(order, rec.ID)
	}
	if err := sc.Err(); err != nil {
		return nil, nil, 0, 0, 0, fmt.Errorf("reading issues.jsonl: %w", err)
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
	return roots, readyList, open, ready, closed, nil
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
