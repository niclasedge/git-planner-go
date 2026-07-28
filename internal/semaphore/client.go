// Package semaphore reads run history from an Ansible Semaphore instance.
//
// Semaphore keeps a flat task history, so a template that failed yesterday and
// succeeded today appears twice. "What is broken right now" therefore means
// keeping only the newest task per template — that reduction, plus picking the
// lines of a failed job's output that explain the failure, is what this package
// adds on top of the four REST calls it makes.
package semaphore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	requestTimeout = 20 * time.Second
	// maxBody caps every response. A task output is the only large one and
	// megabytes of it would be pointless: the excerpt reads the tail.
	maxBody = 4 << 20
)

// Client talks to one Semaphore instance. It authenticates either with an API
// token or, like the justfile recipes do, by logging in and keeping the session
// cookie. Safe for concurrent use.
type Client struct {
	base  string
	user  string
	pass  string
	token string

	hc *http.Client

	mu     sync.Mutex // serialises login, so a burst of calls authenticates once
	authed bool
}

// New validates the connection details. Either token or pass must be set.
func New(base, user, pass, token string) (*Client, error) {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		return nil, errors.New("no url")
	}
	u, err := url.Parse(base)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("%q is not an http url", base)
	}
	if token == "" && pass == "" {
		return nil, errors.New("neither a token nor a password")
	}
	if token == "" && user == "" {
		return nil, errors.New("a password needs a user")
	}

	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	return &Client{
		base:  base,
		user:  user,
		pass:  pass,
		token: token,
		hc:    &http.Client{Jar: jar, Timeout: requestTimeout},
	}, nil
}

// BaseURL is the instance root, for links into the web UI.
func (c *Client) BaseURL() string { return c.base }

// StatusError is any non-2xx answer. The code carries the diagnosis: 401/403
// means the credential is wrong, 404 usually means a renamed project.
type StatusError struct {
	Method string
	Path   string
	Code   int
	Body   string
}

func (e *StatusError) Error() string {
	msg := fmt.Sprintf("%s %s: %d %s", e.Method, e.Path, e.Code, http.StatusText(e.Code))
	if e.Code == http.StatusUnauthorized || e.Code == http.StatusForbidden {
		return msg + " — credentials rejected"
	}
	if e.Body != "" {
		return msg + ": " + e.Body
	}
	return msg
}

func (c *Client) do(ctx context.Context, method, path string, body any) (int, []byte, error) {
	var rdr io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		rdr = bytes.NewReader(buf)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.base+"/api"+path, rdr)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "git-planner-go")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.hc.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, data, nil
}

type loginBody struct {
	Auth     string `json:"auth"`
	Password string `json:"password"`
}

func (c *Client) ensureSession(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.authed {
		return nil
	}
	code, body, err := c.do(ctx, http.MethodPost, "/auth/login", loginBody{Auth: c.user, Password: c.pass})
	if err != nil {
		return fmt.Errorf("login: %w", err)
	}
	if code < 200 || code > 299 {
		return &StatusError{Method: http.MethodPost, Path: "/auth/login", Code: code, Body: snippet(body)}
	}
	c.authed = true
	return nil
}

func (c *Client) forget() {
	c.mu.Lock()
	c.authed = false
	c.mu.Unlock()
}

// get decodes one endpoint into out. A rejected session cookie is retried once
// with a fresh login — cookies expire, and losing a whole refresh cycle to that
// would show a stale-looking error for five minutes.
func (c *Client) get(ctx context.Context, path string, out any) error {
	code, body, err := c.attempt(ctx, path)
	if err != nil {
		return err
	}
	if code == http.StatusUnauthorized && c.token == "" {
		c.forget()
		if code, body, err = c.attempt(ctx, path); err != nil {
			return err
		}
	}
	if code < 200 || code > 299 {
		return &StatusError{Method: http.MethodGet, Path: path, Code: code, Body: snippet(body)}
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("GET %s: %w", path, err)
	}
	return nil
}

func (c *Client) attempt(ctx context.Context, path string) (int, []byte, error) {
	if c.token == "" {
		if err := c.ensureSession(ctx); err != nil {
			return 0, nil, err
		}
	}
	return c.do(ctx, http.MethodGet, path, nil)
}

func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return strings.ReplaceAll(s, "\n", " ")
}

// ---------------------------------------------------------------------------
// endpoints
// ---------------------------------------------------------------------------

type project struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type template struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// Task is one run. Semaphore sends the timestamps as strings that are empty for
// a task that never started, hence apiTime.
type Task struct {
	ID         int     `json:"id"`
	TemplateID int     `json:"template_id"`
	Status     string  `json:"status"`
	Created    apiTime `json:"created"`
	Start      apiTime `json:"start"`
	End        apiTime `json:"end"`
}

// apiTime is a time.Time that tolerates "", null and a couple of layouts. A
// timestamp we cannot parse stays zero instead of failing the whole report: a
// missing date is a cosmetic problem, a hidden red job is not.
type apiTime struct{ time.Time }

var timeLayouts = []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05", "2006-01-02 15:04:05"}

func (t *apiTime) UnmarshalJSON(b []byte) error {
	s := strings.Trim(strings.TrimSpace(string(b)), `"`)
	if s == "" || s == "null" {
		return nil
	}
	for _, layout := range timeLayouts {
		if v, err := time.Parse(layout, s); err == nil {
			t.Time = v
			return nil
		}
	}
	return nil
}

// When is the time to show: a queued task has no start yet.
func (t Task) When() time.Time {
	if !t.Start.IsZero() {
		return t.Start.Time
	}
	return t.Created.Time
}

// Sortable is the ordering key for "which run is the newest". Created, not
// Start: a queued task has no Start, and it is still the most recent attempt.
func (t Task) Sortable() time.Time {
	if !t.Created.IsZero() {
		return t.Created.Time
	}
	return t.Start.Time
}

func (t Task) Duration() time.Duration {
	if t.Start.IsZero() || t.End.IsZero() {
		return 0
	}
	d := t.End.Sub(t.Start.Time)
	if d < 0 {
		return 0
	}
	return d
}

func (c *Client) projectID(ctx context.Context, name string) (int, error) {
	var ps []project
	if err := c.get(ctx, "/projects", &ps); err != nil {
		return 0, err
	}
	for _, p := range ps {
		if strings.EqualFold(p.Name, name) {
			return p.ID, nil
		}
	}
	var names []string
	for _, p := range ps {
		names = append(names, p.Name)
	}
	return 0, fmt.Errorf("no project %q (have: %s)", name, strings.Join(names, ", "))
}

func (c *Client) templateNames(ctx context.Context, pid int) (map[int]string, error) {
	var ts []template
	if err := c.get(ctx, fmt.Sprintf("/project/%d/templates", pid), &ts); err != nil {
		return nil, err
	}
	out := make(map[int]string, len(ts))
	for _, t := range ts {
		out[t.ID] = t.Name
	}
	return out, nil
}

func (c *Client) tasks(ctx context.Context, pid, limit int) ([]Task, error) {
	var ts []Task
	err := c.get(ctx, fmt.Sprintf("/project/%d/tasks?limit=%d", pid, limit), &ts)
	return ts, err
}

// Output returns one task's log lines.
func (c *Client) Output(ctx context.Context, pid, taskID int) ([]string, error) {
	var rows []struct {
		Output string `json:"output"`
	}
	if err := c.get(ctx, fmt.Sprintf("/project/%d/tasks/%d/output", pid, taskID), &rows); err != nil {
		return nil, err
	}
	lines := make([]string, 0, len(rows))
	for _, r := range rows {
		// Some records carry their own trailing newline, some do not. The
		// renderer adds one per line, so leaving them in doubles the spacing of
		// half the log.
		lines = append(lines, stripANSI(strings.TrimRight(r.Output, "\r\n")))
	}
	return lines, nil
}

var ansiSeq = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

// stripANSI removes the colour codes Ansible writes. Rendering them as colour
// would mean building markup out of foreign text; dropping them costs the
// green/red tint and keeps the log readable either way. It also keeps the
// escapes out of the error-marker matching.
func stripANSI(s string) string {
	if !strings.Contains(s, "\x1b") {
		return s
	}
	return ansiSeq.ReplaceAllString(s, "")
}
