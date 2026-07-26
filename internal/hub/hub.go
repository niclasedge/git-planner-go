// Package hub keeps the live GitHub state in memory and refreshes it in the
// background.
//
// The rule the whole design serves: never fetch on a page render. A request
// reads whatever the hub last had and returns immediately. Refreshing happens on
// a schedule in one goroutine, and because every request is conditional, a
// refresh that finds nothing new costs no rate limit.
package hub

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/niclasedge/git-planner-go/internal/config"
	"github.com/niclasedge/git-planner-go/internal/gh"
	"github.com/niclasedge/git-planner-go/internal/sched"
)

// tickInterval is how often the scheduler looks for due work. It only compares
// timestamps, so it can be cheap and frequent.
const tickInterval = 5 * time.Second

// IssueView is what page 1 renders: one flat cross-repo list plus the values the
// filter controls need.
type IssueView struct {
	Issues []gh.Issue
	// PRs come from the same responses as the issues, so the planner can show
	// them without a request of its own.
	PRs       []gh.Issue
	RepoNames []string
	Labels    []string
	// CacheHits and Fetched describe the last *full* sweep, not the last poll —
	// they are the evidence that conditional requests work, and a poll reporting
	// "1 fetched" over them would erase it.
	CacheHits int
	Fetched   int
	// Delta and Polls describe the last incremental poll: issues merged, and
	// requests it took.
	Delta  int
	Polls  int
	Errors []string
	// Kids maps a parent issue to its child numbers, keyed gh.IssueKey. Kept
	// beside the issues rather than on them: the incremental poll replaces whole
	// issue structs, and a relation that lived on the struct would be lost on
	// every merge.
	Kids map[string][]int
}

// RunView is what page 2 renders: runs grouped by repo.
type RunView struct {
	Repos     []gh.RepoRuns
	CacheHits int
	Fetched   int
	Errors    []string
}

// CountView holds the per-repo totals, keyed by "owner/repo". It is what turns
// the planner's sidebar trio from "12 / 0 / 0" into real numbers: a closed count
// cannot be derived from a list of open issues.
type CountView struct {
	Counts map[string]gh.RepoCounts
	// Queries is the cost of the last sweep. There is no 304 column here because
	// GraphQL cannot answer conditionally.
	Queries int
	Errors  []string
}

type Hub struct {
	cfg *config.Config
	api *gh.Client
	log *slog.Logger

	repos  *section[[]gh.Repo]
	issues *section[IssueView]
	runs   *section[RunView]
	counts *section[CountView]

	// The next three fields are the incremental issue state. They are read and
	// written only while holding issues.busy — the section's own lock protects the
	// published view, these protect how it is assembled.
	//
	// issueStore holds the issues per repo, so a poll can patch five repos without
	// touching the other 220. issueMeta carries the last full sweep's numbers, so
	// an incremental publish does not overwrite the 304 evidence with "1 request".
	// cursor is where the next poll starts.
	// subKids holds parent→child mappings, read and written only while holding
	// issues.busy — same lock as issueStore.
	issueStore map[string]*repoIssues
	issueMeta  issueMeta
	cursor     time.Time
	subKids    map[string][]int
	// fullSch is the hourly reconciliation clock, separate from the issues
	// section's five-minute poll clock. Also guarded by issues.busy.
	fullSch sched.Schedule

	// notify is owned by the scheduler goroutine alone, so it needs no lock.
	notify []*notifyState

	// lastErrSig remembers the errors already reported per section so a permanent
	// failure is logged once instead of every refresh. RefreshNow can run from an
	// HTTP handler while the scheduler ticks, hence the lock.
	errMu      sync.Mutex
	lastErrSig map[string]string
}

// repoIssues is one repo's slice of the issue state.
type repoIssues struct {
	Issues []gh.Issue
	PRs    []gh.Issue
}

// issueMeta is what the last full sweep cost. Kept out of repoIssues because it
// describes the sweep, not any repo.
type issueMeta struct {
	CacheHits int
	Fetched   int
	Errors    []string
	SweptAt   time.Time
	// Delta and Polls describe the most recent incremental poll: how many changed
	// issues it merged and how many requests that took.
	Delta int
	Polls int
}

// cursorOverlap is how far back a poll reaches beyond the last one. GitHub's
// since filter is exact, but our clock and theirs are not, and an issue updated
// in the same second as a poll would otherwise fall between two windows.
const cursorOverlap = 90 * time.Second

type notifyState struct {
	tok      *gh.Token
	next     time.Time
	interval time.Duration
	// disabled is set when the token's PAT lacks the notifications scope. The
	// inbox poll is an optimisation, so losing it costs nothing but the sections'
	// own schedules; retrying it forever would waste a request a minute.
	disabled bool
}

func New(cfg *config.Config, api *gh.Client, log *slog.Logger) *Hub {
	h := &Hub{
		cfg:        cfg,
		api:        api,
		log:        log,
		repos:      newSection[[]gh.Repo](cfg.GitHub.Refresh.Repos.Std()),
		issues:     newSection[IssueView](cfg.GitHub.Refresh.Issues.Std()),
		runs:       newSection[RunView](cfg.GitHub.Refresh.Actions.Std()),
		counts:     newSection[CountView](cfg.GitHub.Refresh.Full.Std()),
		fullSch:    sched.New(cfg.GitHub.Refresh.Full.Std()),
		lastErrSig: map[string]string{},
	}
	for _, tok := range api.Tokens() {
		h.notify = append(h.notify, &notifyState{
			tok:      tok,
			interval: cfg.GitHub.Refresh.Notifications.Std(),
		})
	}
	return h
}

func (h *Hub) Issues() (IssueView, time.Time, error) { return h.issues.read() }
func (h *Hub) Runs() (RunView, time.Time, error)     { return h.runs.read() }
func (h *Hub) Repos() ([]gh.Repo, time.Time, error)  { return h.repos.read() }
func (h *Hub) Counts() (CountView, time.Time, error) { return h.counts.read() }

// Invalidate forces a section to refresh on the next tick. This is what the
// manual reload button does — it does not fetch synchronously, because a
// conditional refresh is fast enough that the next tick is soon enough.
func (h *Hub) Invalidate(what string) {
	switch what {
	case "issues":
		h.issues.invalidate()
	case "actions":
		h.runs.invalidate()
	case "repos":
		h.repos.invalidate()
	case "counts":
		h.counts.invalidate()
	case "all":
		h.repos.invalidate()
		h.issues.invalidate()
		h.runs.invalidate()
		h.counts.invalidate()
	}
}

// RefreshNow refreshes a section inline and returns when it is done. The reload
// button uses this instead of Invalidate: because every request is conditional,
// a no-change refresh is a handful of 304s and finishes in well under a second,
// so the response can already show the result.
func (h *Hub) RefreshNow(ctx context.Context, what string) {
	now := time.Now()

	repos, _, _ := h.repos.read()
	if len(repos) == 0 || what == "repos" {
		h.repos.busy.Lock()
		h.refreshRepos(ctx, now)
		h.repos.busy.Unlock()
		repos, _, _ = h.repos.read()
	}
	if len(repos) == 0 {
		return
	}

	switch what {
	case "issues":
		// The reload button means "reconcile", not "poll": a full sweep is what
		// catches an issue the incremental cursor cannot see, and 304s make it cheap.
		h.issues.busy.Lock()
		defer h.issues.busy.Unlock()
		h.fullSweep(ctx, repos, now)
	case "actions":
		h.runs.busy.Lock()
		defer h.runs.busy.Unlock()
		h.refreshRuns(ctx, repos, now)
	case "counts":
		h.counts.busy.Lock()
		defer h.counts.busy.Unlock()
		h.refreshCounts(ctx, repos, now)
	case "all":
		h.issues.busy.Lock()
		h.fullSweep(ctx, repos, now)
		h.issues.busy.Unlock()
		h.runs.busy.Lock()
		h.refreshRuns(ctx, repos, now)
		h.runs.busy.Unlock()
		h.counts.busy.Lock()
		h.refreshCounts(ctx, repos, now)
		h.counts.busy.Unlock()
	}
}

// Start runs the scheduler until ctx is cancelled.
func (h *Hub) Start(ctx context.Context) {
	// Prime the rate-limit numbers so the first budget check is informed rather
	// than optimistic.
	for _, tok := range h.api.Tokens() {
		if err := h.api.RateLimit(ctx, tok); err != nil {
			h.log.Warn("rate limit probe failed", "token", tok.Name, "err", err)
		}
	}

	h.tick(ctx, time.Now())

	t := time.NewTicker(tickInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			h.tick(ctx, now)
		}
	}
}

func (h *Hub) tick(ctx context.Context, now time.Time) {
	h.pollNotifications(ctx, now)

	// Repos come first: the other two sections iterate over them.
	if h.repos.due(now) {
		h.repos.busy.Lock()
		h.refreshRepos(ctx, now)
		h.repos.busy.Unlock()
	}
	repos, _, _ := h.repos.read()
	if len(repos) == 0 {
		return
	}
	// Two clocks, one section. The hourly one asks every repo and reconciles; the
	// five-minute one asks GitHub what changed and patches. Checking the full clock
	// first means an hour boundary spends one sweep, not a sweep plus a poll.
	h.issues.busy.Lock()
	switch {
	case h.fullSch.Due(now):
		h.fullSweep(ctx, repos, now)
	case h.issues.due(now):
		h.pollIssues(ctx, repos, now)
	}
	h.issues.busy.Unlock()

	if h.runs.due(now) {
		h.runs.busy.Lock()
		h.refreshRuns(ctx, repos, now)
		h.runs.busy.Unlock()
	}
	// Counts last, and on the slowest schedule: it is the only section whose
	// refresh always costs requests.
	if h.counts.due(now) {
		h.counts.busy.Lock()
		h.refreshCounts(ctx, repos, now)
		h.counts.busy.Unlock()
	}
}

// pollNotifications asks each token's inbox whether anything moved. A 304 there
// is one free request that lets us skip an entire refresh round.
func (h *Hub) pollNotifications(ctx context.Context, now time.Time) {
	for _, ns := range h.notify {
		if ns.disabled || now.Before(ns.next) {
			continue
		}
		res, err := h.api.Notifications(ctx, ns.tok)
		if err != nil {
			if errors.Is(err, gh.ErrForbidden) {
				ns.disabled = true
				// Do not say "missing scope": for a fine-grained PAT there is no
				// permission to grant. /notifications supports classic PATs only,
				// so someone hunting the token settings for a Notifications
				// checkbox will never find one.
				h.log.Warn("inbox poll disabled: this token cannot read /notifications",
					"token", ns.tok.Name,
					"cause", "fine-grained PATs cannot — the endpoint is classic-PAT only",
					"effect", "sections refresh on their own interval instead")
				continue
			}
			h.log.Debug("notifications poll failed", "token", ns.tok.Name, "err", err)
			ns.next = now.Add(ns.interval)
			continue
		}

		// GitHub's own advice wins over our config — polling the inbox faster
		// than asked risks throttling the token.
		wait := ns.interval
		if res.PollInterval > wait {
			wait = res.PollInterval
		}
		ns.next = now.Add(wait)

		if !res.Changed {
			continue
		}
		h.log.Debug("inbox moved", "token", ns.tok.Name, "repos", len(res.DirtyRepos))
		h.issues.invalidate()
		h.runs.invalidate()
	}
}

func (h *Hub) refreshRepos(ctx context.Context, now time.Time) {
	var (
		merged []gh.Repo
		seen   = map[string]bool{}
		errs   []error
	)
	for _, tok := range h.api.Tokens() {
		// Repos returns a partial list alongside its error, so record the error
		// but keep whatever came back — one unreachable repo must not blank the
		// whole dashboard.
		list, err := h.api.Repos(ctx, tok, h.cfg.GitHub.Repos)
		if err != nil {
			errs = append(errs, err)
		}
		for _, r := range list {
			// Two tokens can see the same repo. Keep the first one — fetching it
			// twice would double the cost and duplicate every issue in the list.
			if seen[r.FullName] {
				continue
			}
			seen[r.FullName] = true
			merged = append(merged, r)
		}
	}

	if len(merged) == 0 {
		if len(errs) > 0 {
			h.repos.fail(errs[0], now)
			h.log.Warn("repo discovery failed", "err", errs[0])
		} else {
			h.repos.fail(errNoRepos, now)
		}
		return
	}
	msgs := make([]string, 0, len(errs))
	for _, err := range errs {
		msgs = append(msgs, err.Error())
	}
	h.logRepoErrors("repos", msgs)

	h.repos.succeed(merged, now)
	h.log.Info("repos refreshed", "count", len(merged), "errors", len(errs))
}

// fullSweep is refreshIssues plus the hourly clock. Any path that reconciles the
// whole state goes through here, so the clock cannot drift out of step with what
// actually happened. Callers must hold h.issues.busy.
func (h *Hub) fullSweep(ctx context.Context, repos []gh.Repo, now time.Time) {
	if h.refreshIssues(ctx, repos, now) {
		h.fullSch.Succeed(now)
		return
	}
	h.fullSch.Fail(now)
}

// refreshIssues is the full sweep: every tracked repo asked directly. It runs
// hourly and on demand, and it is the only thing that can reconcile what the
// incremental poll cannot see (see gh.IssuesSince). Every request is conditional,
// so an unchanged repo answers 304 and costs no rate limit.
//
// Returns false when the sweep failed and the previous state was kept. Callers
// must hold h.issues.busy.
func (h *Hub) refreshIssues(ctx context.Context, repos []gh.Repo, now time.Time) bool {
	store := make(map[string]*repoIssues, len(repos))
	meta := issueMeta{SweptAt: now}
	attempted, skipped := 0, 0

	for _, tok := range h.api.Tokens() {
		mine := reposFor(repos, tok.Name)
		if len(mine) == 0 {
			continue
		}
		attempted += len(mine)
		set := h.api.Issues(ctx, tok, mine, gh.IssueQuery{
			State:   h.cfg.GitHub.Issues.State,
			PerRepo: h.cfg.GitHub.Issues.PerRepo,
		})
		for _, is := range set.Issues {
			ri := storeFor(store, is.Repo)
			ri.Issues = append(ri.Issues, is)
		}
		for _, pr := range set.PRs {
			ri := storeFor(store, pr.Repo)
			ri.PRs = append(ri.PRs, pr)
		}
		meta.CacheHits += set.FromCache
		meta.Fetched += set.Fetched
		skipped += set.Skipped
		for _, err := range set.Errors {
			meta.Errors = append(meta.Errors, err.Error())
		}
	}

	h.logRepoErrors("issues", meta.Errors)

	// Only a round where *every* eligible repo errored is systemic (token
	// revoked, network down). Counting empty results as failure would be wrong:
	// a repo with no open issues is a perfectly good answer.
	if eligible := attempted - skipped; eligible > 0 && len(meta.Errors) >= eligible {
		h.issues.fail(errAllReposFailed, now)
		h.log.Warn("issue refresh failed for every repo",
			"repos", eligible, "errors", len(meta.Errors))
		return false
	}

	// Rebuild the parent→child map from scratch so removed relationships
	// disappear. Each token only fetches its own issues because tokens have
	// separate rate limits and cache namespaces. This has to happen before the
	// publish below: a view published without the fresh map would show the
	// previous round's nesting, and the very first sweep would show none at all.
	//
	// A failed child fetch is appended to the errors after the all-repos-failed
	// guard above on purpose — it degrades the nesting, it does not invalidate
	// the sweep.
	kids := map[string][]int{}
	for _, tok := range h.api.Tokens() {
		var parents []gh.Issue
		for _, ri := range store {
			for _, is := range ri.Issues {
				if is.TokenName == tok.Name && is.Sub.Total > 0 {
					parents = append(parents, is)
				}
			}
		}
		if len(parents) == 0 {
			continue
		}
		m, errs := h.api.SubIssueMap(ctx, tok, parents)
		for k, v := range m {
			kids[k] = v
		}
		for _, err := range errs {
			meta.Errors = append(meta.Errors, err.Error())
		}
	}

	h.issueStore = store
	h.issueMeta = meta
	h.cursor = now
	h.subKids = kids
	view := h.publishIssues(now)

	h.log.Info("issues refreshed (full)",
		"count", len(view.Issues), "cached_304", meta.CacheHits,
		"fetched_200", meta.Fetched, "skipped", skipped, "parents", len(kids), "errors", len(meta.Errors))
	return true
}

// pollIssues is the incremental refresh: one cross-repo request for whatever
// changed since the last poll, merged into the store in place.
//
// This is the five-minute path, and it exists because the alternative — asking
// 225 repos every five minutes — spends a few hundred requests and several
// seconds to discover that four of them moved.
//
// Callers must hold h.issues.busy.
func (h *Hub) pollIssues(ctx context.Context, repos []gh.Repo, now time.Time) {
	if h.issueStore == nil || h.cursor.IsZero() {
		// Nothing to patch yet. A poll would return changes with no baseline to
		// merge them into, which would look like "the only issues that exist are
		// the ones touched in the last five minutes".
		h.fullSweep(ctx, repos, now)
		return
	}

	// Only repos the dashboard tracks. The endpoint is user-scoped, so it also
	// returns issues from repos we deliberately do not follow (a config with an
	// explicit repos list, or somebody else's repo the user commented in). Merging
	// those would make rows appear that the next full sweep silently removes.
	tracked := make(map[string]bool, len(repos))
	for _, r := range repos {
		tracked[r.FullName] = true
	}

	since := h.cursor.Add(-cursorOverlap)
	var (
		errs      []string
		delta     int
		requests  int
		truncated bool
		// kidUpdates holds child lists for the parents this poll actually merged.
		// Applied to h.subKids after the loop, see below.
		kidUpdates map[string][]int
	)

	for _, tok := range h.api.Tokens() {
		set, err := h.api.IssuesSince(ctx, tok, since)
		requests += set.Requests
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
		if set.Truncated {
			truncated = true
			break
		}
		// Parents among the merged issues are the one case a poll can resolve on
		// its own: their own updated_at moved, so their child list is worth
		// re-reading. Usually this slice stays empty.
		var parents []gh.Issue
		for _, is := range set.Issues {
			if !tracked[is.Repo] {
				continue
			}
			if h.mergeIssue(is) {
				delta++
				if is.Sub.Total > 0 {
					parents = append(parents, is)
				}
			}
		}
		for _, pr := range set.PRs {
			if !tracked[pr.Repo] {
				continue
			}
			if h.mergePR(pr) {
				delta++
			}
		}
		if len(parents) > 0 {
			m, subErrs := h.api.SubIssueMap(ctx, tok, parents)
			if kidUpdates == nil {
				kidUpdates = make(map[string][]int, len(m))
			}
			for k, v := range m {
				kidUpdates[k] = v
			}
			for _, e := range subErrs {
				errs = append(errs, e.Error())
			}
		}
	}

	if truncated {
		// More change than a merge should be trusted with. Reconcile properly.
		h.log.Info("incremental poll truncated — falling back to a full sweep")
		h.fullSweep(ctx, repos, now)
		return
	}
	if len(errs) > 0 && delta == 0 {
		h.logRepoErrors("issues-poll", errs)
		h.issues.fail(fmt.Errorf("incremental poll: %s", errs[0]), now)
		return
	}

	h.issueMeta.Delta = delta
	h.issueMeta.Polls = requests
	h.cursor = now

	// Copy on write rather than patch in place: the map the poll would mutate is
	// the same one every already-published view still points at, and the web
	// layer reads it without a lock — a concurrent map write there is fatal, not
	// merely stale.
	//
	// What a poll cannot see: GitHub does not bump a parent's updated_at when a
	// child is attached to it, so a new child of an *old* parent stays invisible
	// until the next full sweep re-reads the list. Asking every known parent on
	// every poll would turn 13 requests an hour into 156 for that one case.
	if len(kidUpdates) > 0 {
		merged := make(map[string][]int, len(h.subKids)+len(kidUpdates))
		for k, v := range h.subKids {
			merged[k] = v
		}
		for k, v := range kidUpdates {
			merged[k] = v
		}
		h.subKids = merged
	}

	view := h.publishIssues(now)

	// Debug while nothing moved: this runs every five minutes and usually merges
	// nothing. A poll that did change something is worth a line without -v — it is
	// the only place a normal run shows the incremental path working.
	line := h.log.Debug
	if delta > 0 {
		line = h.log.Info
	}
	line("issues polled",
		"changed", delta, "requests", requests, "count", len(view.Issues), "errors", len(errs))
}

// mergeIssue applies one changed issue to the store and reports whether anything
// actually moved.
func (h *Hub) mergeIssue(is gh.Issue) bool {
	ri := storeFor(h.issueStore, is.Repo)
	if !h.keepState(is.State) {
		return removeByNumber(&ri.Issues, is.Number)
	}
	return upsertByNumber(&ri.Issues, is)
}

// mergePR is mergeIssue for pull requests. A PR leaves the list when it is
// closed or merged, both of which arrive as state "closed".
func (h *Hub) mergePR(pr gh.Issue) bool {
	ri := storeFor(h.issueStore, pr.Repo)
	if pr.State != "open" {
		return removeByNumber(&ri.PRs, pr.Number)
	}
	return upsertByNumber(&ri.PRs, pr)
}

// keepState answers whether an issue in this state belongs in the view, per
// github.issues.state in the config. The full sweep filters server-side; the
// poll always asks for everything, so it has to filter here.
func (h *Hub) keepState(state string) bool {
	switch h.cfg.GitHub.Issues.State {
	case "all":
		return true
	case "closed":
		return state == "closed"
	default:
		return state == "open"
	}
}

// publishIssues rebuilds the flat view from the store and publishes it. Sorting
// a few hundred issues is far cheaper than the request it saves, so the view is
// derived rather than maintained.
func (h *Hub) publishIssues(now time.Time) IssueView {
	view := h.buildIssueView()
	h.issues.succeed(view, now)
	return view
}

// ApplyIssue folds an issue the app just wrote itself back into the store, so the
// panes show the edit immediately instead of at the next poll. The argument must
// be GitHub's own response to the write — anything else would publish a guess.
//
// The SQLite cache still holds the pre-edit list body for this repo. That is
// self-healing rather than a problem: the body changed, so its ETag changed, and
// the next conditional GET answers 200 instead of 304.
func (h *Hub) ApplyIssue(is gh.Issue) {
	h.issues.busy.Lock()
	defer h.issues.busy.Unlock()

	if h.issueStore == nil {
		return // nothing swept yet; the first sweep will pick this up anyway
	}
	// A write response does not always carry the sub-issue rollup, and an absent
	// rollup is not an empty one — taking it verbatim would drop a parent's
	// progress bar until the next hourly sweep restored it.
	if is.Sub.Total == 0 {
		if old, ok := h.storedIssue(is.Repo, is.Number); ok {
			is.Sub = old.Sub
		}
	}
	h.mergeIssue(is)
	h.issues.replace(h.buildIssueView(), time.Now())
}

// storedIssue reads one issue out of the store. Callers must hold issues.busy.
func (h *Hub) storedIssue(repo string, number int) (gh.Issue, bool) {
	ri, ok := h.issueStore[repo]
	if !ok {
		return gh.Issue{}, false
	}
	for _, is := range ri.Issues {
		if is.Number == number {
			return is, true
		}
	}
	return gh.Issue{}, false
}

func (h *Hub) buildIssueView() IssueView {
	view := IssueView{
		CacheHits: h.issueMeta.CacheHits,
		Fetched:   h.issueMeta.Fetched,
		Errors:    h.issueMeta.Errors,
		Delta:     h.issueMeta.Delta,
		Polls:     h.issueMeta.Polls,
	}
	labels := map[string]bool{}
	for _, ri := range h.issueStore {
		view.Issues = append(view.Issues, ri.Issues...)
		view.PRs = append(view.PRs, ri.PRs...)
		for _, is := range ri.Issues {
			for _, l := range is.Labels {
				labels[l.Name] = true
			}
		}
	}

	sort.Slice(view.Issues, func(i, j int) bool {
		return view.Issues[i].UpdatedAt.After(view.Issues[j].UpdatedAt)
	})
	sort.Slice(view.PRs, func(i, j int) bool {
		return view.PRs[i].UpdatedAt.After(view.PRs[j].UpdatedAt)
	})
	view.RepoNames = repoNames(view.Issues)
	view.Labels = sortedKeys(labels)
	// The map is shared with the web layer by reference and must be treated as
	// read-only after publish — the only mutation point is under issues.busy
	// during the next sweep or poll.
	view.Kids = h.subKids
	return view
}

func storeFor(m map[string]*repoIssues, full string) *repoIssues {
	ri, ok := m[full]
	if !ok {
		ri = &repoIssues{}
		m[full] = ri
	}
	return ri
}

// upsertByNumber replaces the issue with the same number or appends it. Reports
// false when the stored copy is already at least as new — GitHub can return the
// same issue in two overlapping windows, and re-publishing an unchanged view
// would make "aktualisiert vor x" claim movement that did not happen.
func upsertByNumber(list *[]gh.Issue, is gh.Issue) bool {
	for i := range *list {
		if (*list)[i].Number != is.Number {
			continue
		}
		if !is.UpdatedAt.After((*list)[i].UpdatedAt) {
			return false
		}
		(*list)[i] = is
		return true
	}
	*list = append(*list, is)
	return true
}

func removeByNumber(list *[]gh.Issue, number int) bool {
	for i := range *list {
		if (*list)[i].Number == number {
			*list = append((*list)[:i], (*list)[i+1:]...)
			return true
		}
	}
	return false
}

func (h *Hub) refreshRuns(ctx context.Context, repos []gh.Repo, now time.Time) {
	view := RunView{}
	attempted, skipped := 0, 0

	for _, tok := range h.api.Tokens() {
		mine := reposFor(repos, tok.Name)
		if len(mine) == 0 {
			continue
		}
		attempted += len(mine)
		set := h.api.Runs(ctx, tok, mine, gh.ActionQuery{
			RunsPerRepo: h.cfg.GitHub.Actions.RunsPerRepo,
			JobsPerRepo: h.cfg.GitHub.Actions.JobsPerRepo,
		})
		view.Repos = append(view.Repos, set.Repos...)
		view.CacheHits += set.FromCache
		view.Fetched += set.Fetched
		skipped += set.Skipped
		for _, err := range set.Errors {
			view.Errors = append(view.Errors, err.Error())
		}
	}

	h.logRepoErrors("actions", view.Errors)

	// len(view.Repos) is not a health signal here: a repo can answer 200 with
	// zero runs, and repos with Actions off are counted as skipped, not failed.
	// So compare errors against the repos we could have got runs from.
	if eligible := attempted - skipped; eligible > 0 && len(view.Errors) >= eligible {
		h.runs.fail(errAllReposFailed, now)
		h.log.Warn("actions refresh failed for every repo",
			"repos", eligible, "errors", len(view.Errors))
		return
	}

	sort.SliceStable(view.Repos, func(i, j int) bool {
		a, b := view.Repos[i].LastRun(), view.Repos[j].LastRun()
		if a == nil || b == nil {
			return b == nil
		}
		return a.CreatedAt.After(b.CreatedAt)
	})

	h.runs.succeed(view, now)
	h.log.Info("actions refreshed",
		"repos", len(view.Repos), "cached_304", view.CacheHits,
		"fetched_200", view.Fetched, "skipped", skipped, "errors", len(view.Errors))
}

// refreshCounts sweeps the per-repo totals over GraphQL.
//
// This is the one refresh that always costs something, so it runs hourly and is
// kept as cheap as it can be: totalCount requests no nodes, twenty repos share a
// query, and a repo the query cannot resolve simply drops out of the map instead
// of failing the sweep.
func (h *Hub) refreshCounts(ctx context.Context, repos []gh.Repo, now time.Time) {
	view := CountView{Counts: map[string]gh.RepoCounts{}}

	for _, tok := range h.api.Tokens() {
		mine := reposFor(repos, tok.Name)
		if len(mine) == 0 {
			continue
		}
		set := h.api.Counts(ctx, tok, mine)
		view.Queries += set.Queries
		for name, cnt := range set.Counts {
			view.Counts[name] = cnt
		}
		for _, err := range set.Errors {
			view.Errors = append(view.Errors, err.Error())
		}
	}

	h.logRepoErrors("counts", view.Errors)

	// An empty map with errors means every batch failed — GraphQL scope missing,
	// or the budget is gone. Keep the previous numbers rather than blanking the
	// sidebar.
	if len(view.Counts) == 0 && len(view.Errors) > 0 {
		h.counts.fail(errAllReposFailed, now)
		h.log.Warn("count sweep failed for every repo", "queries", view.Queries)
		return
	}

	h.counts.succeed(view, now)
	h.log.Info("counts refreshed",
		"repos", len(view.Counts), "queries", view.Queries, "errors", len(view.Errors))
}

// logRepoErrors makes per-repo failures visible. Without it only the count was
// logged, which tells you that something broke but never what — the first few
// messages are enough to name the repo and the reason.
//
// It logs a given set of errors only once. Most per-repo failures are permanent
// (a fine-grained PAT not granted Actions on that repo, a repo renamed away), and
// a refresher that runs every minute would otherwise emit the same warning
// forever. Repeats drop to debug; a *changed* set warns again.
func (h *Hub) logRepoErrors(section string, errs []string) {
	const show = 3

	sig := strings.Join(errs, "\n")
	h.errMu.Lock()
	repeat := h.lastErrSig[section] == sig
	h.lastErrSig[section] = sig
	h.errMu.Unlock()

	if len(errs) == 0 {
		return
	}
	level := slog.LevelWarn
	if repeat {
		level = slog.LevelDebug
	}
	for i, e := range errs {
		if i == show {
			h.log.Log(context.Background(), level, "more refresh errors suppressed",
				"section", section, "remaining", len(errs)-show)
			break
		}
		h.log.Log(context.Background(), level, "repo refresh failed",
			"section", section, "err", e)
	}
}

func reposFor(repos []gh.Repo, tokenName string) []gh.Repo {
	out := make([]gh.Repo, 0, len(repos))
	for _, r := range repos {
		if r.TokenName == tokenName {
			out = append(out, r)
		}
	}
	return out
}

func repoNames(issues []gh.Issue) []string {
	seen := map[string]bool{}
	for _, i := range issues {
		seen[i.Repo] = true
	}
	return sortedKeys(seen)
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
