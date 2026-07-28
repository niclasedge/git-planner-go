package semaphore

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// fake is a Semaphore stand-in: the four endpoints the report needs, plus the
// cookie session in front of them.
type fake struct {
	logins   atomic.Int32
	requests atomic.Int32
	// rejectOnce makes the next authenticated GET answer 401, the way an
	// expired session cookie does.
	rejectOnce atomic.Bool
	bearer     string
}

const tasksJSON = `[
  {"id":313,"template_id":7,"status":"error","created":"2026-07-27T07:00:00Z","start":"2026-07-27T07:00:00Z","end":"2026-07-27T07:00:02Z"},
  {"id":300,"template_id":7,"status":"success","created":"2026-07-26T07:00:00Z","start":"2026-07-26T07:00:00Z","end":"2026-07-26T07:00:40Z"},
  {"id":312,"template_id":8,"status":"success","created":"2026-07-27T06:00:00Z","start":"2026-07-27T06:00:00Z","end":"2026-07-27T06:01:00Z"},
  {"id":311,"template_id":9,"status":"waiting","created":"2026-07-27T05:00:00Z","start":"","end":""}
]`

const outputJSON = `[
  {"output":"TASK [checkout] ***"},
  {"output":"git@github.com: Permission denied (publickey)."},
  {"output":"fatal: Could not read from remote repository."},
  {"output":"git@github.com: Permission denied (publickey)."}
]`

func (f *fake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/api/auth/login" {
		var in loginBody
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Auth != "admin" || in.Password != "s3cret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		f.logins.Add(1)
		http.SetCookie(w, &http.Cookie{Name: "semaphore", Value: "session", Path: "/"})
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// Everything else needs either the cookie or the bearer token.
	_, cookieErr := r.Cookie("semaphore")
	hasBearer := f.bearer != "" && r.Header.Get("Authorization") == "Bearer "+f.bearer
	if cookieErr != nil && !hasBearer {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if f.rejectOnce.Swap(false) {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	f.requests.Add(1)
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.URL.Path == "/api/projects":
		io.WriteString(w, `[{"id":1,"name":"Other"},{"id":2,"name":"IaC-Stack"}]`)
	case r.URL.Path == "/api/project/2/templates":
		io.WriteString(w, `[{"id":7,"name":"Claude Research Issue"},{"id":8,"name":"Backup"}]`)
	case r.URL.Path == "/api/project/2/tasks":
		if got := r.URL.Query().Get("limit"); got != "400" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		io.WriteString(w, tasksJSON)
	case r.URL.Path == "/api/project/2/tasks/313/output":
		io.WriteString(w, outputJSON)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func newFake(t *testing.T, bearer string) (*fake, string) {
	t.Helper()
	f := &fake{bearer: bearer}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return f, srv.URL
}

func TestReport(t *testing.T) {
	f, base := newFake(t, "")
	c, err := New(base, "admin", "s3cret", "")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	rep, err := c.Report(context.Background(), "iac-stack", 0) // name match is case-insensitive
	if err != nil {
		t.Fatalf("Report: %v", err)
	}

	if rep.ProjectID != 2 || rep.Total() != 3 || rep.Failing() != 1 {
		t.Fatalf("expected project 2 with 3 templates and 1 red, got %d/%d/%d",
			rep.ProjectID, rep.Total(), rep.Failing())
	}

	bad := rep.Bad[0]
	if bad.Template != "Claude Research Issue" || bad.Task.ID != 313 {
		t.Fatalf("wrong failing row: %+v", bad)
	}
	if bad.Duration() != 2e9 {
		t.Fatalf("expected 2s, got %v", bad.Duration())
	}
	if !strings.HasSuffix(bad.URL(), "/project/2/templates/7") {
		t.Fatalf("wrong deep link: %s", bad.URL())
	}
	if len(bad.Excerpt) != 2 || !strings.Contains(bad.Excerpt[0], "Permission denied") {
		t.Fatalf("expected the deduplicated excerpt, got %v", bad.Excerpt)
	}

	// Newest first, and the run of a template Semaphore no longer lists still
	// appears — under its id.
	if rep.OK[0].Task.ID != 312 || rep.OK[1].Template != "Template #9" {
		t.Fatalf("unexpected passing rows: %+v", rep.OK)
	}

	// One login for four GETs, not four logins.
	if f.logins.Load() != 1 {
		t.Fatalf("expected a single login, got %d", f.logins.Load())
	}

	// The session is reused across reports.
	if _, err := c.Report(context.Background(), "IaC-Stack", 0); err != nil {
		t.Fatalf("second Report: %v", err)
	}
	if f.logins.Load() != 1 {
		t.Fatalf("expected the session to be reused, got %d logins", f.logins.Load())
	}
}

func TestReport_BearerToken(t *testing.T) {
	f, base := newFake(t, "tok")
	c, err := New(base, "", "", "tok")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := c.Report(context.Background(), "IaC-Stack", 400); err != nil {
		t.Fatalf("Report: %v", err)
	}
	if f.logins.Load() != 0 {
		t.Fatal("a token must not log in")
	}
}

// An expired cookie costs one silent re-login, not a five-minute error banner.
func TestReport_RetriesExpiredSession(t *testing.T) {
	f, base := newFake(t, "")
	c, err := New(base, "admin", "s3cret", "")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	f.rejectOnce.Store(true)

	if _, err := c.Report(context.Background(), "IaC-Stack", 0); err != nil {
		t.Fatalf("Report: %v", err)
	}
	if f.logins.Load() != 2 {
		t.Fatalf("expected a second login, got %d", f.logins.Load())
	}
}

func TestReport_UnknownProject(t *testing.T) {
	_, base := newFake(t, "")
	c, _ := New(base, "admin", "s3cret", "")

	_, err := c.Report(context.Background(), "Nope", 0)
	if err == nil {
		t.Fatal("expected an error")
	}
	// The message has to name the candidates: the usual cause is a rename.
	if !strings.Contains(err.Error(), "IaC-Stack") {
		t.Fatalf("expected the available projects in %q", err)
	}
}

func TestReport_WrongPassword(t *testing.T) {
	_, base := newFake(t, "")
	c, _ := New(base, "admin", "wrong", "")

	_, err := c.Report(context.Background(), "IaC-Stack", 0)
	if err == nil || !strings.Contains(err.Error(), "credentials rejected") {
		t.Fatalf("expected a credential error, got %v", err)
	}
}

func TestNew(t *testing.T) {
	cases := []struct {
		name, base, user, pass, token string
		ok                            bool
	}{
		{name: "password", base: "http://h:3001", user: "admin", pass: "p", ok: true},
		{name: "token", base: "https://h", token: "t", ok: true},
		{name: "trailing slash", base: "http://h:3001/", token: "t", ok: true},
		{name: "no url", token: "t"},
		{name: "no scheme", base: "h:3001", token: "t"},
		{name: "no credential", base: "http://h:3001"},
		{name: "password without user", base: "http://h:3001", pass: "p"},
	}
	for _, tc := range cases {
		c, err := New(tc.base, tc.user, tc.pass, tc.token)
		if tc.ok != (err == nil) {
			t.Fatalf("%s: unexpected error %v", tc.name, err)
		}
		if tc.ok && strings.HasSuffix(c.BaseURL(), "/") {
			t.Fatalf("%s: the base url must not keep its slash: %s", tc.name, c.BaseURL())
		}
	}
}

func TestStripANSI(t *testing.T) {
	cases := map[string]string{
		"\x1b[0;33mlocalhost\x1b[0m : ok=3": "localhost : ok=3",
		"plain line":                        "plain line",
		"\x1b[1;31mfatal:\x1b[0m boom":      "fatal: boom",
	}
	for in, want := range cases {
		if got := stripANSI(in); got != want {
			t.Errorf("stripANSI(%q) = %q, want %q", in, got, want)
		}
	}
}
