package web

import (
	"bytes"
	"html/template"
	"strings"
	"testing"
	"time"

	"github.com/niclasedge/git-planner-go/internal/gh"
)

// The templates are parsed at startup, so a typo in one of them is a crash on
// boot rather than a compile error. Parsing them here turns that into a test
// failure, and executing the new fragments catches the field names too.
func testTemplates(t *testing.T) *template.Template {
	t.Helper()
	fm := template.FuncMap{}
	for k, v := range funcs {
		fm[k] = v
	}
	// render is wired to the server at runtime; here it only has to resolve.
	var set *template.Template
	fm["render"] = func(name string, data any) (template.HTML, error) {
		var buf bytes.Buffer
		if err := set.ExecuteTemplate(&buf, name, data); err != nil {
			return "", err
		}
		return template.HTML(buf.String()), nil
	}
	fm["widget"] = func(any) (template.HTML, error) { return "", nil }

	set, err := template.New("").Funcs(fm).ParseFS(templateFS, "templates/*.html")
	if err != nil {
		t.Fatalf("parsing templates: %v", err)
	}
	return set
}

func TestTemplatesParse(t *testing.T) { testTemplates(t) }

func render(t *testing.T, name string, data any) string {
	t.Helper()
	var buf bytes.Buffer
	if err := testTemplates(t).ExecuteTemplate(&buf, name, data); err != nil {
		t.Fatalf("executing %s: %v", name, err)
	}
	return buf.String()
}

func TestRenderPlannerRow_Guides(t *testing.T) {
	d := &plannerData{}
	row := issueRow{
		Issue:  gh.Issue{Number: 7, Repo: "o/r", Title: "Kind", State: "open", UpdatedAt: time.Now()},
		Depth:  2,
		Guides: []treeGuide{guideLine, guideElbow},
	}
	out := render(t, "planner-row", newRowCtx(d, row))

	if !strings.Contains(out, `<span class="tg tg-line"></span><span class="tg tg-elbow"></span>`) {
		t.Fatalf("guides missing from row:\n%s", out)
	}
	if !strings.Contains(out, "ir-child") {
		t.Fatalf("nested row should carry ir-child:\n%s", out)
	}
}

// A top-level row has no gutter at all — no empty .ir-tree to indent it.
func TestRenderPlannerRow_TopLevelHasNoTree(t *testing.T) {
	out := render(t, "planner-row", newTopRowCtx(&plannerData{},
		gh.Issue{Number: 3, Repo: "o/r", Title: "Top", State: "open", UpdatedAt: time.Now()}))

	if strings.Contains(out, "ir-tree") {
		t.Fatalf("top-level row should have no tree gutter:\n%s", out)
	}
	if strings.Contains(out, "ir-child") {
		t.Fatalf("top-level row should not be marked as a child:\n%s", out)
	}
}

func TestRenderPlannerEdit(t *testing.T) {
	d := &editData{
		Repo: "o/r", Number: 12, Query: "repo=o%2Fr&issue=12",
		Issue: gh.Issue{
			Number: 12, Repo: "o/r", Title: "Titel", State: "open",
			Milestone: &gh.Milestone{Title: "v2", Number: 4},
		},
		Due:        "2026-08-01",
		Body:       "Beschreibung",
		Assignees:  "alice",
		People:     []string{"alice", "bob"},
		Milestones: []gh.Milestone{{Title: "v2", Number: 4}},
		Labels: []labelChoice{
			{Name: "bug", Color: "ff0000", On: true},
			{Name: "wip", Color: "00ff00"},
		},
	}
	out := render(t, "planner-edit", d)

	for _, want := range []string{
		`name="title"`, `value="Titel"`,
		`type="date" name="due" value="2026-08-01"`,
		`name="labels" value="bug" checked`,
		`name="newlabel"`, `name="assignees"`,
		`<option value="4" selected>v2</option>`,
		`name="body"`, `>Beschreibung</textarea>`,
		`hx-post="/htmx/planner/edit?repo=o%2Fr&amp;issue=12"`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("edit form missing %q:\n%s", want, out)
		}
	}
}

// The no-cache path renders the error and no form — a form prefilled from a zero
// issue would offer to blank the title.
func TestRenderPlannerEdit_NoIssueNoForm(t *testing.T) {
	out := render(t, "planner-edit", &editData{Repo: "o/r", Number: 12, Err: "nicht im Cache"})
	if !strings.Contains(out, "nicht im Cache") {
		t.Fatalf("error missing:\n%s", out)
	}
	if strings.Contains(out, "<form") {
		t.Fatalf("should not render a form without an issue:\n%s", out)
	}
}

func TestRenderPlannerComments(t *testing.T) {
	d := &commentData{
		Repo: "o/r", Number: 5, Query: "repo=o%2Fr&issue=5",
		Comments: []gh.Comment{{
			Body: "Erledigt", User: gh.User{Login: "alice"},
			CreatedAt: time.Now().Add(-time.Hour), UpdatedAt: time.Now().Add(-time.Hour),
			HTMLURL: "https://example.invalid/c/1",
		}},
	}
	out := render(t, "planner-comments", d)

	for _, want := range []string{`id="cmts"`, "alice", "Erledigt", `name="comment"`} {
		if !strings.Contains(out, want) {
			t.Fatalf("comments missing %q:\n%s", want, out)
		}
	}
}

// An issue with no comments still gets the form: the point of the pane is being
// able to add one.
func TestRenderPlannerComments_EmptyStillHasForm(t *testing.T) {
	out := render(t, "planner-comments", &commentData{Repo: "o/r", Number: 5})
	if !strings.Contains(out, `name="comment"`) {
		t.Fatalf("form missing:\n%s", out)
	}
}

// The middle pane only carries the out-of-band marker when a save asked for it.
func TestRenderPlannerMid_OOB(t *testing.T) {
	plain := render(t, "planner-mid", &plannerData{})
	if strings.Contains(plain, "hx-swap-oob") {
		t.Fatalf("normal render must not be out of band:\n%s", plain)
	}
	oob := render(t, "planner-mid", &plannerData{OOB: true})
	if !strings.Contains(oob, `id="paneIssues" hx-swap-oob="true"`) {
		t.Fatalf("expected oob marker:\n%s", oob)
	}
}

// The saved response is two top-level elements: the detail pane for the target,
// and the list for the out-of-band swap.
func TestRenderPlannerSaved(t *testing.T) {
	is := gh.Issue{Number: 9, Repo: "o/r", Title: "Fertig", State: "closed", UpdatedAt: time.Now()}
	out := render(t, "planner-saved", &plannerData{Detail: &is, OOB: true, Warning: "Keine Änderung."})

	if !strings.Contains(out, "Fertig") {
		t.Fatalf("detail missing:\n%s", out)
	}
	if !strings.Contains(out, `hx-swap-oob="true"`) {
		t.Fatalf("oob list missing:\n%s", out)
	}
	if !strings.Contains(out, "Keine Änderung.") {
		t.Fatalf("note missing:\n%s", out)
	}
}

// A pull request has no editable body here, so it must not offer the button.
func TestRenderPlannerDetail_NoEditForPR(t *testing.T) {
	pr := gh.Issue{
		Number: 4, Repo: "o/r", Title: "PR", State: "open", UpdatedAt: time.Now(),
		PullRequest: &struct {
			HTMLURL string `json:"html_url"`
		}{HTMLURL: "https://example.invalid/pr/4"},
	}
	out := render(t, "planner-detail", &plannerData{Detail: &pr})
	if strings.Contains(out, "bearbeiten") {
		t.Fatalf("PR should have no edit button:\n%s", out)
	}

	is := gh.Issue{Number: 5, Repo: "o/r", Title: "Issue", State: "open", UpdatedAt: time.Now()}
	out = render(t, "planner-detail", &plannerData{Detail: &is})
	if !strings.Contains(out, "bearbeiten") {
		t.Fatalf("issue should have an edit button:\n%s", out)
	}
	if !strings.Contains(out, "/htmx/planner/comments?") {
		t.Fatalf("comments should be lazily loaded:\n%s", out)
	}
}
