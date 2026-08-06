package web

import (
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/niclasedge/git-planner-go/internal/gh"
)

// Labels that move an open issue out of the plain "open" bucket. Same list as
// the reference dashboard uses, so a repo shows the same numbers in both.
var (
	wipLabels     = []string{"in-progress", "in progress", "wip"}
	blockedLabels = []string{"blocked", "blocker"}
)

// agendaLimit caps the upcoming list. The sidebar is for what is close, not for
// everything with a date on it.
const agendaLimit = 25

type plannerFilter struct {
	Repo  string
	Token string
	Issue int
	// Empty shows repos with no open issues. Off by default: with a few hundred
	// repos the interesting ones would drown.
	Empty bool
}

// issueRow is one line in the middle pane. depth is how far it sits under its
// parent: the tree is flattened here rather than rendered recursively, so the
// template stays a plain range over rows.
type issueRow struct {
	Issue gh.Issue
	Depth int
	// Guides is one entry per gutter column left of the row, outermost first.
	// The tree is drawn from these and not from Depth, because neither "last
	// child" nor "an ancestor still has siblings below" can be derived from a
	// depth number — and without those two the lines do not join up.
	Guides []treeGuide
}

// treeGuide is what one gutter column draws.
type treeGuide uint8

const (
	guideBlank treeGuide = iota // an ancestor's line already ended: empty column
	guideLine                   // an ancestor continues past this row: │
	guideTee                    // this row, with siblings below it: ├─
	guideElbow                  // this row, last of its siblings: └─
)

// String is the CSS class suffix, so the template needs no mapping of its own.
func (g treeGuide) String() string {
	switch g {
	case guideLine:
		return "line"
	case guideTee:
		return "tee"
	case guideElbow:
		return "elbow"
	default:
		return "blank"
	}
}

// childTrail extends a parent's gutter by one column for one of its children: the
// parent's own connector becomes a line that continues down past the child (or
// nothing, if the parent was the last of its siblings), and the child's own
// connector is appended. Always a fresh slice — rows keep theirs.
func childTrail(parent []treeGuide, last bool) []treeGuide {
	out := make([]treeGuide, len(parent)+1)
	for i, g := range parent {
		if g == guideLine || g == guideTee {
			out[i] = guideLine
		}
	}
	out[len(parent)] = guideTee
	if last {
		out[len(parent)] = guideElbow
	}
	return out
}

// issueSection splits the middle pane into wayfinder work and everything
// else. Wayfinder maps come first because they are the entry point into a
// plan; a ticket read without its map is missing its context.
// The section carried an Icon string (an emoji) until the head gained its own
// colour: .sec-head.wf is already accent-coloured next to a neutral "Issues", so
// the glyph repeated a distinction the type had already made.
type issueSection struct {
	Title string
	Rows  []issueRow
}

// rowLabelMax is how many labels a row shows before folding the rest into a
// "+N" badge — one line must stay one line.
const rowLabelMax = 3

// rowLabels wraps what RowLabels returns; Go templates cannot destructure a
// multi-return, but they can range over a struct's slice.
type rowLabels struct {
	Shown []gh.Label
	Extra int
}

type plannerData struct {
	Tokens []tokenOption
	Filter plannerFilter

	Agenda  []agendaGroup
	AgendaN int

	Repos []plannerRepo

	Selected *plannerRepo
	Issues   []gh.Issue
	PRs      []gh.Issue
	Detail   *gh.Issue

	// Sections is the middle pane split into wayfinder and normal issues with
	// children inlined under their parents.
	Sections []issueSection

	// OOB marks the middle pane for an out-of-band swap. Set when the response is
	// really about the detail pane but the list has to follow along — after an
	// edit, a stale title or label in the list reads as a failed save.
	OOB bool

	UpdatedAt time.Time
	CacheHits int
	Fetched   int
	Errors    []string
	Warning   string
}

type plannerRepo struct {
	FullName string
	Name     string // the part after the slash — the sidebar has no room for more
	Token    string

	Open    int
	WIP     int
	Blocked int
	Closed  int
	Plain   int // open minus wip minus blocked: what the blue segment shows
	PRs     int
	Due     int

	Activity time.Time
	Active   bool
	URL      string
}

type agendaGroup struct {
	Title   string
	Overdue bool
	Items   []agendaItem
}

type agendaItem struct {
	Repo   string
	Short  string
	Number int
	Title  string
	Badge  string
	Red    bool
	URL    string
}

// weekdaysDE indexes by time.Weekday, so Sunday comes first.
var weekdaysDE = [...]string{"So", "Mo", "Di", "Mi", "Do", "Fr", "Sa"}

// dueBadge is the short form the sidebar shows: overdue, today, the weekday for
// the coming week, then a date. The reference stops at the weekday, which reads
// as "this week" for something three weeks out — hence the date.
func dueBadge(due, today time.Time) (string, bool) {
	diff := int(due.Sub(today).Hours() / 24)
	switch {
	case diff < 0:
		return "über", true
	case diff == 0:
		return "heute", false
	case diff <= 6:
		return weekdaysDE[int(due.Weekday())], false
	default:
		return due.Format("2.1."), false
	}
}

func parsePlannerFilter(r *http.Request) plannerFilter {
	q := r.URL.Query()
	n, _ := strconv.Atoi(strings.TrimPrefix(strings.TrimSpace(q.Get("issue")), "#"))
	return plannerFilter{
		Repo:  strings.TrimSpace(q.Get("repo")),
		Token: strings.TrimSpace(q.Get("token")),
		Issue: n,
		Empty: q.Get("empty") == "1",
	}
}

// query is the whole three-pane state. Repo and issue selection live in the URL
// rather than in JavaScript, so the back button and a copied link both work.
func (f plannerFilter) query(repo string, issue int) string {
	q := url.Values{}
	if repo != "" {
		q.Set("repo", repo)
	}
	if issue > 0 {
		q.Set("issue", strconv.Itoa(issue))
	}
	if f.Token != "" {
		q.Set("token", f.Token)
	}
	if f.Empty {
		q.Set("empty", "1")
	}
	return q.Encode()
}

func (f plannerFilter) with(repo string, issue int) string {
	if q := f.query(repo, issue); q != "" {
		return "/planner?" + q
	}
	return "/planner"
}

// TokenURL switches account and drops the repo selection: a repo from the other
// account would not be in the new list.
func (d *plannerData) TokenURL(name string) string {
	f := d.Filter
	f.Token = name
	return f.with("", 0)
}

func (d *plannerData) EmptyURL() string {
	f := d.Filter
	f.Empty = !f.Empty
	return f.with(f.Repo, 0)
}

func (d *plannerData) IssueURL(is gh.Issue) string {
	return d.Filter.with(is.Repo, is.Number)
}

// DetailURL is the same state as IssueURL, served as the right pane alone.
func (d *plannerData) DetailURL(is gh.Issue) string {
	return "/htmx/planner/detail?" + d.Filter.query(is.Repo, is.Number)
}

// EditURL and CommentsURL are the two fragments the detail pane loads on its own.
// They carry the whole filter, so whatever they post back lands in the same view.
func (d *plannerData) EditURL(is gh.Issue) string {
	return "/htmx/planner/edit?" + d.Filter.query(is.Repo, is.Number)
}

func (d *plannerData) CommentsURL(is gh.Issue) string {
	return "/htmx/planner/comments?" + d.Filter.query(is.Repo, is.Number)
}

// buildPlannerData assembles all three panes from what the hub already holds.
// No request goes out: the repo list, the counts, the pull requests and the
// agenda are all derived from the issue payloads that were fetched anyway.
func (s *Server) buildPlannerData(f plannerFilter) *plannerData {
	view, updatedAt, err := s.hub.Issues()

	d := &plannerData{
		Tokens:    s.tokenOptions(),
		Filter:    f,
		UpdatedAt: updatedAt,
		CacheHits: view.CacheHits,
		Fetched:   view.Fetched,
		Errors:    view.Errors,
	}
	if err != nil {
		d.Warning = err.Error()
	}

	// Two tokens can both see the same organisation repo, which would count its
	// issues twice. Key by repo and number and keep the first.
	seen := make(map[string]bool, len(view.Issues))
	issues := make([]gh.Issue, 0, len(view.Issues))
	for _, is := range view.Issues {
		if f.Token != "" && is.TokenName != f.Token {
			continue
		}
		key := is.Repo + "#" + strconv.Itoa(is.Number)
		if seen[key] {
			continue
		}
		seen[key] = true
		issues = append(issues, is)
	}

	agg := map[string]*plannerRepo{}
	get := func(full, token string) *plannerRepo {
		r, ok := agg[full]
		if !ok {
			name := full
			if i := strings.IndexByte(full, '/'); i >= 0 {
				name = full[i+1:]
			}
			r = &plannerRepo{FullName: full, Name: name, Token: token}
			agg[full] = r
		}
		return r
	}

	for _, is := range issues {
		r := get(is.Repo, is.TokenName)
		if is.UpdatedAt.After(r.Activity) {
			r.Activity = is.UpdatedAt
		}
		if is.State == "closed" {
			r.Closed++
			continue
		}
		r.Open++
		if hasAnyLabel(is, wipLabels) {
			r.WIP++
		}
		if hasAnyLabel(is, blockedLabels) {
			r.Blocked++
		}
		if is.Due != nil {
			r.Due++
		}
	}

	prSeen := make(map[string]bool, len(view.PRs))
	for _, pr := range view.PRs {
		if f.Token != "" && pr.TokenName != f.Token {
			continue
		}
		if pr.State != "open" {
			continue
		}
		key := pr.Repo + "#" + strconv.Itoa(pr.Number)
		if prSeen[key] {
			continue
		}
		prSeen[key] = true
		get(pr.Repo, pr.TokenName).PRs++
	}

	// A selected repo can have nothing to aggregate: its last open issue was just
	// closed, or the link is older than the issue. Without an entry it would drop
	// out of the sidebar and the middle pane would claim nothing is selected, so
	// the entry is created empty — the GraphQL totals below still fill it in.
	if f.Repo != "" {
		get(f.Repo, f.Token)
	}

	// The toggle pulls in everything the tokens can see, including repos that
	// never produced an issue.
	if f.Empty {
		if repos, _, rErr := s.hub.Repos(); rErr == nil {
			for _, r := range repos {
				if f.Token != "" && r.TokenName != f.Token {
					continue
				}
				got := get(r.FullName, r.TokenName)
				if got.Activity.IsZero() {
					got.Activity = r.PushedAt
				}
			}
		}
	}

	// Totals from the GraphQL sweep replace what the issue list could count. They
	// are authoritative in two ways the list is not: it holds only open issues (so
	// Closed would be 0) and it is truncated at issues.per-repo. Repos missing from
	// the sweep keep their counted values — a stale or failed sweep degrades the
	// numbers, it does not empty them.
	if cv, _, cErr := s.hub.Counts(); cErr == nil {
		for full, cnt := range cv.Counts {
			r, ok := agg[full]
			if !ok {
				continue // not in this view: filtered out by token, or has no issues
			}
			r.Open = cnt.Open
			r.Closed = cnt.Closed
			r.PRs = cnt.PRs
			// WIP and Blocked come from labels on the fetched issues, so they can only
			// ever describe the part of the list we hold. Clamp them to the real total
			// rather than letting a truncated list produce a negative Plain.
			if r.WIP > r.Open {
				r.WIP = r.Open
			}
			if r.Blocked > r.Open-r.WIP {
				r.Blocked = r.Open - r.WIP
			}
		}
	}

	for _, r := range agg {
		r.Plain = r.Open - r.WIP - r.Blocked
		if r.Plain < 0 {
			r.Plain = 0
		}
		d.Repos = append(d.Repos, *r)
	}
	// Most recent issue activity on top: the sidebar should lead with whatever
	// moved last, not with the alphabet.
	sort.Slice(d.Repos, func(i, j int) bool {
		a, b := d.Repos[i], d.Repos[j]
		if !a.Activity.Equal(b.Activity) {
			return a.Activity.After(b.Activity)
		}
		return a.FullName < b.FullName
	})

	// A repo the user just clicked stays selected even with no open issues, so
	// it cannot vanish from under the middle pane.
	pick := f.Repo
	if pick == "" && len(d.Repos) > 0 {
		pick = d.Repos[0].FullName
	}
	for i := range d.Repos {
		if d.Repos[i].FullName == pick {
			d.Repos[i].Active = true
			d.Selected = &d.Repos[i]
		}
		d.Repos[i].URL = f.with(d.Repos[i].FullName, 0)
	}

	if d.Selected != nil {
		for _, is := range issues {
			if is.Repo == d.Selected.FullName {
				d.Issues = append(d.Issues, is)
			}
		}
		// Dated work first and soonest first, then by number descending — a plain
		// list sorted by "last touched" buries what is actually due.
		sort.SliceStable(d.Issues, func(i, j int) bool {
			a, b := d.Issues[i], d.Issues[j]
			switch {
			case a.Due != nil && b.Due != nil && !a.Due.Equal(*b.Due):
				return a.Due.Before(*b.Due)
			case (a.Due != nil) != (b.Due != nil):
				return a.Due != nil
			}
			return a.Number > b.Number
		})
		d.Sections = buildIssueSections(d.Issues, view.Kids)
		for _, pr := range view.PRs {
			if pr.Repo == d.Selected.FullName && pr.State == "open" && prSeen[pr.Repo+"#"+strconv.Itoa(pr.Number)] {
				d.PRs = append(d.PRs, pr)
			}
		}
		if f.Issue > 0 {
			for i := range d.Issues {
				if d.Issues[i].Number == f.Issue {
					d.Detail = &d.Issues[i]
					break
				}
			}
			if d.Detail == nil {
				for i := range d.PRs {
					if d.PRs[i].Number == f.Issue {
						d.Detail = &d.PRs[i]
						break
					}
				}
			}
		}
	}

	d.Agenda, d.AgendaN = buildAgenda(issues, f)
	return d
}

// buildAgenda is the cross-repo part: every open issue with a date, regardless
// of which repo or account it came from.
func buildAgenda(issues []gh.Issue, f plannerFilter) ([]agendaGroup, int) {
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)

	type dated struct {
		is  gh.Issue
		due time.Time
	}
	var all []dated
	for _, is := range issues {
		if is.Due == nil || is.State == "closed" {
			continue
		}
		all = append(all, dated{is, *is.Due})
	}
	if len(all) == 0 {
		return nil, 0
	}
	sort.Slice(all, func(i, j int) bool {
		if !all[i].due.Equal(all[j].due) {
			return all[i].due.Before(all[j].due)
		}
		return all[i].is.Repo < all[j].is.Repo
	})

	row := func(x dated) agendaItem {
		badge, red := dueBadge(x.due, today)
		short := x.is.Repo
		if i := strings.IndexByte(short, '/'); i >= 0 {
			short = short[i+1:]
		}
		return agendaItem{
			Repo: x.is.Repo, Short: short, Number: x.is.Number,
			Title: x.is.Title, Badge: badge, Red: red,
			URL: f.with(x.is.Repo, x.is.Number),
		}
	}

	var overdue, upcoming agendaGroup
	overdue.Overdue = true
	for _, x := range all {
		if x.due.Before(today) {
			overdue.Items = append(overdue.Items, row(x))
			continue
		}
		if len(upcoming.Items) < agendaLimit {
			upcoming.Items = append(upcoming.Items, row(x))
		}
	}

	var groups []agendaGroup
	if len(overdue.Items) > 0 {
		overdue.Title = "Überfällig (" + strconv.Itoa(len(overdue.Items)) + ")"
		groups = append(groups, overdue)
	}
	if len(upcoming.Items) > 0 {
		upcoming.Title = "Anstehend"
		groups = append(groups, upcoming)
	}
	return groups, len(all)
}

func hasAnyLabel(is gh.Issue, names []string) bool {
	for _, l := range is.Labels {
		low := strings.ToLower(l.Name)
		for _, n := range names {
			if low == n {
				return true
			}
		}
	}
	return false
}

// rowCtx pairs one row with the page it belongs to. A range body cannot reach
// back to the page data, and the row needs it for the links.
type rowCtx struct {
	Data *plannerData
	Row  issueRow
}

func newRowCtx(d *plannerData, r issueRow) rowCtx {
	return rowCtx{Data: d, Row: r}
}

// newTopRowCtx is the same for a bare issue that was never part of the tree —
// the pull requests above the sections.
func newTopRowCtx(d *plannerData, is gh.Issue) rowCtx {
	return rowCtx{Data: d, Row: issueRow{Issue: is}}
}

// DueBadge is what a row in the middle pane shows next to its title.
func (d *plannerData) DueBadge(is gh.Issue) agendaItem {
	if is.Due == nil {
		return agendaItem{}
	}
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	badge, red := dueBadge(*is.Due, today)
	return agendaItem{Badge: badge, Red: red}
}

// isWayfinder reports whether the issue carries any label whose name starts with
// "wayfinder:" (case-insensitive).
func isWayfinder(is gh.Issue) bool {
	for _, l := range is.Labels {
		if strings.HasPrefix(strings.ToLower(l.Name), "wayfinder:") {
			return true
		}
	}
	return false
}

// isWayfinderMap reports whether the issue has the exact label "wayfinder:map"
// (case-insensitive).
func isWayfinderMap(is gh.Issue) bool {
	for _, l := range is.Labels {
		if strings.EqualFold(l.Name, "wayfinder:map") {
			return true
		}
	}
	return false
}

// buildIssueSections turns the selected repo's flat issue list into two
// sections of rows. kids is keyed gh.IssueKey and holds every parent's child
// numbers in map order.
func buildIssueSections(issues []gh.Issue, kids map[string][]int) []issueSection {
	if len(issues) == 0 {
		return nil
	}

	// Index every issue by key so we can look up children without scanning.
	byKey := make(map[string]int, len(issues))
	for i, is := range issues {
		byKey[gh.IssueKey(is.Repo, is.Number)] = i
	}

	// childOf maps a child to its parent, built only from parents that are in this
	// list. Deriving it up front is a correctness fix, not just a shortcut: kids
	// holds the parents of *every* repo, so matching an issue against child
	// numbers alone would hide, say, #12 here because some other repo's parent has
	// a child #12.
	childOf := make(map[string]string, len(kids))
	for _, is := range issues {
		parentKey := gh.IssueKey(is.Repo, is.Number)
		for _, childNum := range kids[parentKey] {
			childKey := gh.IssueKey(is.Repo, childNum)
			if _, ok := byKey[childKey]; ok {
				childOf[childKey] = parentKey
			}
		}
	}

	visited := make(map[string]bool, len(issues))

	// walk appends one issue and then its children recursively.
	var walk func(is gh.Issue, trail []treeGuide, rows *[]issueRow)
	walk = func(is gh.Issue, trail []treeGuide, rows *[]issueRow) {
		key := gh.IssueKey(is.Repo, is.Number)
		if visited[key] {
			return // cycle guard: if the API ever returns a loop, we terminate
		}
		visited[key] = true
		*rows = append(*rows, issueRow{Issue: is, Depth: len(trail), Guides: trail})

		// Which children get a row has to be settled before drawing any of them:
		// the closed ones are not in this list, and the elbow belongs to the last
		// child actually drawn, not to the last one GitHub knows about.
		var draw []int
		for _, childNum := range kids[key] {
			childKey := gh.IssueKey(is.Repo, childNum)
			if idx, ok := byKey[childKey]; ok && !visited[childKey] {
				draw = append(draw, idx)
			}
		}
		for i, idx := range draw {
			walk(issues[idx], childTrail(trail, i == len(draw)-1), rows)
		}
	}

	// First pass: collect top-level issues by section. Children are only emitted
	// through walk, so separating by top-level parent automatically keeps
	// children in the same section as their parent.
	var wfMaps, wfRest, normTops []gh.Issue
	for _, is := range issues {
		key := gh.IssueKey(is.Repo, is.Number)
		if visited[key] {
			continue
		}
		// A child of an issue in this list appears only under its parent.
		if _, ok := childOf[key]; ok {
			continue
		}
		if isWayfinder(is) {
			if isWayfinderMap(is) {
				wfMaps = append(wfMaps, is)
			} else {
				wfRest = append(wfRest, is)
			}
		} else {
			normTops = append(normTops, is)
		}
	}

	// Walk each group, which recursively inlines children.
	var wfRows, normRows []issueRow
	for _, is := range wfMaps {
		walk(is, nil, &wfRows)
	}
	for _, is := range wfRest {
		walk(is, nil, &wfRows)
	}
	for _, is := range normTops {
		walk(is, nil, &normRows)
	}

	// Whatever the walks did not reach is part of a cycle: with A→B and B→A both
	// look like children, so neither qualifies as top level. Render it anyway. A
	// row that silently vanishes is a worse failure than one nested oddly, and
	// this is the guarantee that every issue in the list produces exactly one row.
	for _, is := range issues {
		if visited[gh.IssueKey(is.Repo, is.Number)] {
			continue
		}
		if isWayfinder(is) {
			walk(is, nil, &wfRows)
		} else {
			walk(is, nil, &normRows)
		}
	}

	var sections []issueSection
	if len(wfRows) > 0 {
		sections = append(sections, issueSection{Title: "Wayfinder", Rows: wfRows})
	}
	if len(normRows) > 0 {
		sections = append(sections, issueSection{Title: "Issues", Rows: normRows})
	}
	return sections
}

// RowLabels is what fits on one line next to a title: at most rowLabelMax
// labels, and the "wayfinder:" prefix stripped because the section heading
// already says it.
func (d *plannerData) RowLabels(is gh.Issue) rowLabels {
	shown := make([]gh.Label, 0, len(is.Labels))
	for i, l := range is.Labels {
		if i >= rowLabelMax {
			return rowLabels{Shown: shown, Extra: len(is.Labels) - i}
		}
		// Strip the wayfinder: prefix — the section heading already provides the
		// context, and the label without it is a useful signal ("task" vs "map").
		name := l.Name
		if strings.HasPrefix(strings.ToLower(name), "wayfinder:") {
			name = name[len("wayfinder:"):]
		}
		shown = append(shown, gh.Label{Name: name, Color: l.Color})
	}
	return rowLabels{Shown: shown}
}

func (s *Server) handlePlannerPage(w http.ResponseWriter, r *http.Request) {
	data := s.buildPlannerData(parsePlannerFilter(r))
	title := "Planner"
	if data.Selected != nil {
		title = data.Selected.Name
	}
	s.render(w, pageData{
		Title:      title,
		ActiveSlug: "planner",
		Body:       "page-planner",
		Full:       true,
		Planner:    data,
	})
}

// handlePlannerDetail serves the right pane on its own. Clicking an issue must
// not reset the middle pane's scroll position, which a full page render would.
func (s *Server) handlePlannerDetail(w http.ResponseWriter, r *http.Request) {
	s.fragment(w, "planner-detail", s.buildPlannerData(parsePlannerFilter(r)))
}
