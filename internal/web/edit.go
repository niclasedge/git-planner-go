package web

// Editing is the one place where the web layer talks to GitHub directly, in both
// directions. The panes stay hub-fed, but a form that offers a repo's labels has
// to read them, and a Save has to write. Every one of those reads is conditional
// and only happens while a form is open, so the cost is a handful of 304s per
// opened issue.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/niclasedge/git-planner-go/internal/gh"
)

// writeTimeout bounds one edit. Long enough for a label creation plus the patch,
// short enough that a hanging API does not hold the form open forever.
const writeTimeout = 20 * time.Second

// editData drives the edit form.
type editData struct {
	Repo   string
	Number int
	Issue  gh.Issue
	// Query is the planner filter, so the form's own URLs stay inside the current
	// view.
	Query string

	// Due is yyyy-mm-dd for the date input, empty when the body has no date. It
	// deliberately ignores the milestone fallback: prefilling from the milestone
	// would freeze its date into this issue's body on the next save.
	Due          string
	MilestoneDue string

	Body       string
	Labels     []labelChoice
	People     []string
	Milestones []gh.Milestone
	Assignees  string

	Err  string
	Note string
}

type labelChoice struct {
	Name  string
	Color string
	On    bool
}

// commentData drives the comment thread below an issue.
type commentData struct {
	Repo     string
	Number   int
	Query    string
	Comments []gh.Comment
	Err      string
	Note     string
}

// DetailBack is where "Abbrechen" goes: the read-only pane for the same issue.
func (e *editData) DetailBack() string { return "/htmx/planner/detail?" + e.Query }

func (e *editData) PostURL() string { return "/htmx/planner/edit?" + e.Query }

func (c *commentData) PostURL() string { return "/htmx/planner/comments?" + c.Query }

// tokenFor picks the identity that will act on this repo. The issue the hub
// already holds knows which token fetched it, which is the only reliable answer
// when two accounts can both see the same organisation repo.
func (s *Server) tokenFor(repo, prefer string) *gh.Token {
	view, _, _ := s.hub.Issues()
	for _, is := range view.Issues {
		if is.Repo == repo && is.TokenName != "" {
			if tok := s.api.Token(is.TokenName); tok != nil {
				return tok
			}
		}
	}
	if prefer != "" {
		if tok := s.api.Token(prefer); tok != nil {
			return tok
		}
	}
	if toks := s.api.Tokens(); len(toks) > 0 {
		return toks[0]
	}
	return nil
}

// storedIssue finds an issue in what the hub last published.
func (s *Server) storedIssue(repo string, number int) (gh.Issue, bool) {
	view, _, _ := s.hub.Issues()
	for _, is := range view.Issues {
		if is.Repo == repo && is.Number == number {
			return is, true
		}
	}
	for _, pr := range view.PRs {
		if pr.Repo == repo && pr.Number == number {
			return pr, true
		}
	}
	return gh.Issue{}, false
}

// guardWrite rejects anything that did not come from this app's own UI.
//
// The server has no login, so any page in any other tab could post here — and
// these handlers write to real repositories. The custom header is the actual
// guard: a cross-origin request cannot set it without a preflight, and the
// preflight has nowhere to succeed. That also covers DNS rebinding, which is the
// way a browser can be talked into reaching a tailnet address.
//
// What it does not cover is a device that is legitimately in the tailnet: there
// the UI itself is the authorisation.
func (s *Server) guardWrite(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get("HX-Request") != "true" {
		http.Error(w, "nur über die UI", http.StatusForbidden)
		return false
	}
	if o := r.Header.Get("Origin"); o != "" {
		u, err := url.Parse(o)
		if err != nil || u.Host != r.Host {
			http.Error(w, "fremder Origin", http.StatusForbidden)
			return false
		}
	}
	return true
}

// apiMessage turns a client error into something worth reading in the pane. The
// permission case is the one that will actually happen: a fine-grained PAT is
// read-only for issues unless it was asked for more.
func apiMessage(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, gh.ErrForbidden):
		return "Kein Schreibrecht: der PAT braucht für dieses Repo „Issues: Read and write“. (" + err.Error() + ")"
	case errors.Is(err, gh.ErrBadCredentials):
		return "Token ungültig oder abgelaufen — .env prüfen."
	case errors.Is(err, gh.ErrRateLimited):
		return "Rate-Limit erschöpft, in einigen Minuten erneut versuchen."
	case errors.Is(err, gh.ErrValidation):
		return "GitHub hat die Änderung abgelehnt: " + err.Error()
	case errors.Is(err, gh.ErrNotFound):
		return "Nicht gefunden — Repo umbenannt, oder der Token sieht es nicht."
	default:
		return err.Error()
	}
}

// handlePlannerEdit renders the edit form for one issue.
func (s *Server) handlePlannerEdit(w http.ResponseWriter, r *http.Request) {
	f := parsePlannerFilter(r)
	is, ok := s.storedIssue(f.Repo, f.Issue)
	if !ok {
		s.fragment(w, "planner-edit", &editData{
			Repo: f.Repo, Number: f.Issue, Query: f.query(f.Repo, f.Issue),
			Err: "Dieses Issue liegt nicht im Cache — nach dem nächsten Refresh nochmal versuchen.",
		})
		return
	}
	s.fragment(w, "planner-edit", s.buildEditData(r.Context(), f, is))
}

func (s *Server) buildEditData(ctx context.Context, f plannerFilter, is gh.Issue) *editData {
	d := &editData{
		Repo:      is.Repo,
		Number:    is.Number,
		Issue:     is,
		Query:     f.query(is.Repo, is.Number),
		Body:      is.BodyClean(),
		Assignees: joinLogins(is.Assignees),
	}
	if due, ok := gh.ParseDue(is.Body); ok {
		d.Due = due.Format("2006-01-02")
	}
	if is.Milestone != nil && is.Milestone.DueOn != nil {
		d.MilestoneDue = is.Milestone.DueOn.Format("2.1.2006")
	}

	tok := s.tokenFor(is.Repo, f.Token)
	if tok == nil {
		d.Err = "Kein Token konfiguriert."
		return d
	}

	// The three pickers degrade one by one: a repo whose labels cannot be read
	// still gets a usable form, it just cannot offer the existing ones.
	on := make(map[string]bool, len(is.Labels))
	for _, l := range is.Labels {
		on[strings.ToLower(l.Name)] = true
	}
	labels, err := s.api.RepoLabels(ctx, tok, is.Repo)
	if err != nil {
		s.log.Warn("labels for edit form", "repo", is.Repo, "err", err)
		// Fall back to the labels the issue itself carries, so unchecking one is
		// still possible.
		for _, l := range is.Labels {
			d.Labels = append(d.Labels, labelChoice{Name: l.Name, Color: l.Color, On: true})
		}
	} else {
		for _, l := range labels {
			d.Labels = append(d.Labels, labelChoice{Name: l.Name, Color: l.Color, On: on[strings.ToLower(l.Name)]})
		}
	}

	if people, err := s.api.Assignables(ctx, tok, is.Repo); err == nil {
		for _, p := range people {
			d.People = append(d.People, p.Login)
		}
	}
	if ms, err := s.api.Milestones(ctx, tok, is.Repo); err == nil {
		d.Milestones = ms
	}
	return d
}

// handlePlannerSave applies the form and answers with the read-only pane plus an
// out-of-band swap of the issue list.
func (s *Server) handlePlannerSave(w http.ResponseWriter, r *http.Request) {
	if !s.guardWrite(w, r) {
		return
	}
	f := parsePlannerFilter(r)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "kaputtes Formular", http.StatusBadRequest)
		return
	}

	current, ok := s.storedIssue(f.Repo, f.Issue)
	if !ok {
		s.fragment(w, "planner-edit", &editData{
			Repo: f.Repo, Number: f.Issue, Query: f.query(f.Repo, f.Issue),
			Err: "Dieses Issue liegt nicht mehr im Cache — Seite neu laden.",
		})
		return
	}
	tok := s.tokenFor(f.Repo, f.Token)
	if tok == nil {
		s.failEdit(r.Context(), w, f, current, "Kein Token konfiguriert.")
		return
	}

	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), writeTimeout)
	defer cancel()

	edit, note, err := s.buildEdit(ctx, tok, r, current)
	if err != nil {
		s.failEdit(ctx, w, f, current, apiMessage(err))
		return
	}
	if edit.Empty() {
		// Nothing to send. Answering with the detail pane is the honest outcome:
		// the form is closed and the issue is exactly as it was.
		s.renderSaved(w, f, current, "Keine Änderung.")
		return
	}

	updated, err := s.api.UpdateIssue(ctx, tok, f.Repo, f.Issue, edit)
	if err != nil {
		s.failEdit(ctx, w, f, current, apiMessage(err))
		return
	}
	s.hub.ApplyIssue(updated)
	s.log.Info("issue updated", "repo", f.Repo, "issue", f.Issue, "token", tok.Name)
	s.renderSaved(w, f, updated, note)
}

// buildEdit diffs the form against the issue and returns only what changed.
// Sending unchanged fields would work, but every field in a PATCH is a field
// GitHub records as touched.
func (s *Server) buildEdit(ctx context.Context, tok *gh.Token, r *http.Request, cur gh.Issue) (gh.IssueEdit, string, error) {
	var e gh.IssueEdit
	var notes []string

	if title := strings.TrimSpace(r.FormValue("title")); title != "" && title != cur.Title {
		e.Title = &title
	}

	if state := r.FormValue("state"); state == "open" || state == "closed" {
		if state != cur.State {
			e.State = &state
		}
	}

	// The textarea holds the body without the target-date line, and the date input
	// owns that line. Reassembling here is what makes the picker write into the
	// body, which is where every reader of this repo already looks for a due date.
	body := strings.ReplaceAll(r.FormValue("body"), "\r\n", "\n")
	var due time.Time
	if v := strings.TrimSpace(r.FormValue("due")); v != "" {
		parsed, err := time.Parse("2006-01-02", v)
		if err != nil {
			return e, "", fmt.Errorf("unlesbares Datum %q", v)
		}
		due = parsed
	}
	newBody := gh.SetDue(body, due)
	if newBody != cur.Body {
		e.Body = &newBody
	}
	if due.IsZero() {
		// Only the anchored line was removed. An inline date in a sentence still
		// parses, and cutting words out of prose would be worse than saying so.
		if left, ok := gh.ParseDue(newBody); ok {
			notes = append(notes, "Datum steht noch im Text ("+left.Format("2.1.2006")+") und muss dort manuell weg.")
		}
	}

	labels := r.Form["labels"]
	if nl := strings.TrimSpace(r.FormValue("newlabel")); nl != "" {
		created, err := s.ensureLabel(ctx, tok, cur.Repo, nl, r.FormValue("newcolor"))
		if err != nil {
			return e, "", err
		}
		if !containsFold(labels, created) {
			labels = append(labels, created)
		}
	}
	if !sameSet(labels, labelNames(cur.Labels)) {
		e.Labels = &labels
	}

	assignees := splitLogins(r.FormValue("assignees"))
	if !sameSet(assignees, logins(cur.Assignees)) {
		e.Assignees = &assignees
	}

	if v := r.FormValue("milestone"); v != "" {
		n, err := strconv.Atoi(v)
		if err == nil {
			cures := 0
			if cur.Milestone != nil {
				cures = cur.Milestone.Number
			}
			if n != cures {
				e.Milestone = &n
			}
		}
	}

	return e, strings.Join(notes, " "), nil
}

// ensureLabel returns the label name to attach, creating the label first if the
// repo does not have it yet. GitHub does accept an unknown name on an issue, but
// then invents a colour for it; asking for one is the difference between a tag
// that fits the set and a random pink.
func (s *Server) ensureLabel(ctx context.Context, tok *gh.Token, repo, name, color string) (string, error) {
	existing, err := s.api.RepoLabels(ctx, tok, repo)
	if err == nil {
		for _, l := range existing {
			if strings.EqualFold(l.Name, name) {
				return l.Name, nil // already there, keep GitHub's spelling
			}
		}
	}
	created, err := s.api.CreateLabel(ctx, tok, repo, name, color)
	if err != nil {
		// A label that exists after all is not a failure — the list above may have
		// been truncated or unreadable.
		if errors.Is(err, gh.ErrValidation) {
			return name, nil
		}
		return "", err
	}
	return created.Name, nil
}

// renderSaved answers a successful save: the read-only detail pane, plus the
// issue list swapped out of band so the row matches what was just written.
func (s *Server) renderSaved(w http.ResponseWriter, f plannerFilter, is gh.Issue, note string) {
	d := s.buildPlannerData(f)
	if d.Detail == nil {
		// Closing an issue drops it out of a view that only holds open ones. Show
		// what was written rather than an empty pane.
		saved := is
		d.Detail = &saved
	}
	d.OOB = true
	d.Warning = note
	s.fragment(w, "planner-saved", d)
}

// failEdit re-renders the form with the error and the user's input still in it.
// Status stays 200 on purpose: htmx only swaps successful responses, and a 500
// here would leave the pane showing the old form with the typing lost.
func (s *Server) failEdit(ctx context.Context, w http.ResponseWriter, f plannerFilter, cur gh.Issue, msg string) {
	d := s.buildEditData(ctx, f, cur)
	d.Err = msg
	s.fragment(w, "planner-edit", d)
}

// handlePlannerComments renders the thread. The count on the issue decides
// whether to ask at all — an issue with no comments needs no request.
func (s *Server) handlePlannerComments(w http.ResponseWriter, r *http.Request) {
	f := parsePlannerFilter(r)
	d := &commentData{Repo: f.Repo, Number: f.Issue, Query: f.query(f.Repo, f.Issue)}
	if f.Repo == "" || f.Issue == 0 {
		s.fragment(w, "planner-comments", d)
		return
	}

	is, known := s.storedIssue(f.Repo, f.Issue)
	if known && is.Comments == 0 {
		s.fragment(w, "planner-comments", d)
		return
	}
	if tok := s.tokenFor(f.Repo, f.Token); tok != nil {
		list, err := s.api.IssueComments(r.Context(), tok, f.Repo, f.Issue)
		if err != nil {
			d.Err = apiMessage(err)
		}
		d.Comments = list
	}
	s.fragment(w, "planner-comments", d)
}

// handlePlannerComment posts a comment and re-renders the thread.
func (s *Server) handlePlannerComment(w http.ResponseWriter, r *http.Request) {
	if !s.guardWrite(w, r) {
		return
	}
	f := parsePlannerFilter(r)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "kaputtes Formular", http.StatusBadRequest)
		return
	}
	d := &commentData{Repo: f.Repo, Number: f.Issue, Query: f.query(f.Repo, f.Issue)}

	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), writeTimeout)
	defer cancel()

	body := strings.TrimSpace(strings.ReplaceAll(r.FormValue("comment"), "\r\n", "\n"))
	tok := s.tokenFor(f.Repo, f.Token)
	switch {
	case body == "":
		d.Note = "Leerer Kommentar."
	case tok == nil:
		d.Err = "Kein Token konfiguriert."
	default:
		if _, err := s.api.AddComment(ctx, tok, f.Repo, f.Issue, body); err != nil {
			d.Err = apiMessage(err)
		} else {
			s.log.Info("comment added", "repo", f.Repo, "issue", f.Issue, "token", tok.Name)
		}
	}
	// The thread is re-read rather than appended to locally: posting moved the
	// ETag, so this one request leaves the cache correct for the next open — and
	// it is also what redraws the thread after a rejected empty submission.
	if tok != nil && d.Err == "" {
		if list, err := s.api.IssueComments(ctx, tok, f.Repo, f.Issue); err == nil {
			d.Comments = list
		}
	}
	s.fragment(w, "planner-comments", d)
}

// --- small helpers -------------------------------------------------------

func labelNames(ls []gh.Label) []string {
	out := make([]string, 0, len(ls))
	for _, l := range ls {
		out = append(out, l.Name)
	}
	return out
}

func logins(us []gh.User) []string {
	out := make([]string, 0, len(us))
	for _, u := range us {
		out = append(out, u.Login)
	}
	return out
}

func joinLogins(us []gh.User) string { return strings.Join(logins(us), ", ") }

// splitLogins accepts commas, spaces and a leading @, because all three are what
// people type.
func splitLogins(s string) []string {
	fields := strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n' || r == ';'
	})
	out := make([]string, 0, len(fields))
	seen := map[string]bool{}
	for _, f := range fields {
		f = strings.TrimPrefix(strings.TrimSpace(f), "@")
		if f == "" || seen[strings.ToLower(f)] {
			continue
		}
		seen[strings.ToLower(f)] = true
		out = append(out, f)
	}
	return out
}

// sameSet compares two lists as case-insensitive sets — order and duplicates in a
// form submission mean nothing.
func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	set := make(map[string]bool, len(a))
	for _, x := range a {
		set[strings.ToLower(x)] = true
	}
	for _, y := range b {
		if !set[strings.ToLower(y)] {
			return false
		}
	}
	return true
}

func containsFold(list []string, want string) bool {
	for _, x := range list {
		if strings.EqualFold(x, want) {
			return true
		}
	}
	return false
}
