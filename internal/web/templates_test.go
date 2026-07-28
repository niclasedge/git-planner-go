package web

import (
	"bytes"
	"context"
	"html/template"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/niclasedge/git-planner-go/internal/gh"
	"github.com/niclasedge/git-planner-go/internal/panel"
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

// The log fragment is what an expanded run swaps in. Field names come from
// panel.LogView, so executing it here catches a rename on the Go side.
func TestRenderSemaphoreLog(t *testing.T) {
	out := render(t, "semaphore-log", &panel.LogView{
		Template: "Deploy", TaskID: 313, Status: "error",
		URL:   "http://example.invalid/project/1/history",
		Lines: []string{"PLAY [all]", "fatal: boom <&>"}, Dropped: 12,
	})

	for _, want := range []string{"#313", "error", "erste 12 Zeilen ausgelassen", "PLAY [all]", "in Semaphore"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q:\n%s", want, out)
		}
	}
	// Log output is foreign text; it must arrive escaped, not as markup.
	if strings.Contains(out, "boom <&>") {
		t.Fatalf("log lines must be escaped:\n%s", out)
	}
}

// A failed fetch shows the reason instead of an empty box.
func TestRenderSemaphoreLog_Error(t *testing.T) {
	out := render(t, "semaphore-log", &panel.LogView{TaskID: 7, Status: "success", Err: "GET /output: 500"})
	if !strings.Contains(out, "GET /output: 500") {
		t.Fatalf("error missing:\n%s", out)
	}
	if strings.Contains(out, "sema-log") {
		t.Fatalf("no log block expected on error:\n%s", out)
	}
}

// ollamaAPI fakes the three endpoints the widget reads. Driving the widget
// through its own Update keeps the production type free of test-only setters and
// covers the JSON decoding and the merge on the way to the template.
func ollamaAPI(t *testing.T) *httptest.Server {
	t.Helper()
	// expires_at nine minutes out, so the countdown in the template is stable.
	expires := time.Now().Add(9 * time.Minute).Format(time.RFC3339Nano)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/tags":
			io.WriteString(w, `{"models":[
				{"name":"gpt-oss:20b","size":13793441244,"modified_at":"2026-07-27T23:34:10Z",
				 "details":{"family":"gptoss","parameter_size":"20.9B","quantization_level":"MXFP4"}},
				{"name":"gemma4:12b","size":9977569963,"modified_at":"2026-06-07T18:36:22Z",
				 "details":{"family":"gemma","parameter_size":"12B","quantization_level":"MXFP4"}}]}`)
		case "/api/ps":
			io.WriteString(w, `{"models":[{"name":"gpt-oss:20b","size":13338924809,"size_vram":13338924809,
				"context_length":65536,"expires_at":"`+expires+`",
				"details":{"family":"gptoss","parameter_size":"20.9B","quantization_level":"MXFP4"}}]}`)
		case "/api/version":
			io.WriteString(w, `{"version":"0.32.5"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestRenderWidgetOllama(t *testing.T) {
	srv := ollamaAPI(t)
	o := &panel.Ollama{URL: srv.URL}
	if err := o.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	o.Update(context.Background())

	out := render(t, "widget-ollama", o)
	for _, want := range []string{"1 geladen", "2 Modelle", "v0.32.5", "gpt-oss", "20b",
		"13,3 GB im Speicher", "65536 Kontext", "entlädt ", "läuft seit", "auf der Platte", "gemma4", "10,0 GB"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q:\n%s", want, out)
		}
	}
}

// Nothing loaded is the normal resting state, not an error — and without a
// loaded block there is nothing for the divider to separate.
func TestRenderWidgetOllama_NothingLoaded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			io.WriteString(w, `{"models":[{"name":"a:7b","size":100000000,"details":{"parameter_size":"7B"}}]}`)
		default:
			io.WriteString(w, `{"models":[]}`)
		}
	}))
	defer srv.Close()

	o := &panel.Ollama{URL: srv.URL}
	if err := o.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	o.Update(context.Background())

	out := render(t, "widget-ollama", o)
	if !strings.Contains(out, "kein Modell geladen") {
		t.Fatalf("resting state missing:\n%s", out)
	}
	if strings.Contains(out, "auf der Platte") {
		t.Fatalf("divider needs a loaded block above it:\n%s", out)
	}
}
