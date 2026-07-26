package gh

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// ErrValidation is a 422: GitHub understood the request and refused it — a label
// that already exists, an assignee who is not a collaborator. Retrying will not
// help, but the message is worth showing verbatim, so it travels with the error.
var ErrValidation = errors.New("validation failed")

// apiError is the shape GitHub returns with a 4xx.
type apiError struct {
	Message string `json:"message"`
	Errors  []struct {
		Resource string `json:"resource"`
		Field    string `json:"field"`
		Code     string `json:"code"`
		Message  string `json:"message"`
	} `json:"errors"`
}

func (e apiError) text() string {
	parts := []string{e.Message}
	for _, x := range e.Errors {
		switch {
		case x.Message != "":
			parts = append(parts, x.Message)
		case x.Field != "":
			parts = append(parts, x.Field+": "+x.Code)
		case x.Code != "":
			parts = append(parts, x.Code)
		}
	}
	return strings.Join(parts, " — ")
}

// send performs a write request.
//
// Nothing here is cached or conditional. A write has no ETag to offer, and its
// response must never reach the read cache: the next conditional GET would then
// hold a single object where it expects a list. Writes also cost rate limit —
// they are the one part of this app that cannot be free, which is fine because
// they only happen when someone clicks Save.
func (c *Client) send(ctx context.Context, tok *Token, method, path string, in, out any) error {
	if tok == nil {
		return errors.New("no token for this repository")
	}
	// Reserve nothing: a write is the user waiting in front of the screen, and it
	// must not be starved by the background refresher's quota.
	if !tok.hasBudget(0) {
		return fmt.Errorf("%w: token %s", ErrRateLimited, tok.Name)
	}

	var body io.Reader
	if in != nil {
		enc, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("%s %s: encoding: %w", method, path, err)
		}
		body = bytes.NewReader(enc)
	}

	url := path
	if !strings.HasPrefix(url, "http") {
		url = apiBase + path
	}
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", acceptHeader)
	req.Header.Set("X-GitHub-Api-Version", apiVersion)
	req.Header.Set("Authorization", "Bearer "+tok.secret)
	req.Header.Set("User-Agent", "git-planner-go")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		tok.note(func() { tok.errored++ })
		return fmt.Errorf("%s %s: %w", method, url, err)
	}
	defer func() {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()

	tok.readRateHeaders(resp.Header)

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		raw, err := io.ReadAll(resp.Body)
		if err != nil {
			tok.note(func() { tok.errored++ })
			return fmt.Errorf("%s %s: reading body: %w", method, url, err)
		}
		tok.note(func() { tok.misses++ })
		if out != nil && len(raw) > 0 {
			if err := json.Unmarshal(raw, out); err != nil {
				return fmt.Errorf("%s %s: decoding: %w", method, url, err)
			}
		}
		return nil
	}

	tok.note(func() { tok.errored++ })
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
	var ae apiError
	_ = json.Unmarshal(raw, &ae)

	switch resp.StatusCode {
	case http.StatusUnauthorized:
		return fmt.Errorf("token %s: %w", tok.Name, ErrBadCredentials)
	case http.StatusForbidden, http.StatusTooManyRequests:
		if resp.Header.Get("X-RateLimit-Remaining") == "0" {
			return fmt.Errorf("%w: token %s", ErrRateLimited, tok.Name)
		}
		// The header names the permission the PAT is missing, which is the one
		// thing that actually tells you how to fix it.
		need := resp.Header.Get("X-Accepted-GitHub-Permissions")
		if need != "" {
			return fmt.Errorf("%s %s: %w (token %s, needs %s)", method, path, ErrForbidden, tok.Name, need)
		}
		return fmt.Errorf("%s %s: %w (token %s)", method, path, ErrForbidden, tok.Name)
	case http.StatusNotFound, http.StatusGone:
		return fmt.Errorf("%s %s: %w", method, path, ErrNotFound)
	case http.StatusUnprocessableEntity:
		return fmt.Errorf("%s %s: %w: %s", method, path, ErrValidation, ae.text())
	default:
		return fmt.Errorf("%s %s: %s: %s", method, path, resp.Status, strings.TrimSpace(ae.text()))
	}
}

// IssueEdit is a partial issue update: a nil field is left untouched. GitHub's
// PATCH has the same semantics, so the zero value is a no-op request.
type IssueEdit struct {
	Title     *string
	Body      *string
	State     *string
	Labels    *[]string
	Assignees *[]string
	// Milestone is the milestone number, where a pointer to 0 clears it — GitHub
	// numbers milestones from 1, so 0 cannot collide with a real one. nil leaves
	// the milestone alone.
	Milestone *int
}

// Empty reports whether this edit would change nothing, so the caller can skip
// the request entirely.
func (e IssueEdit) Empty() bool {
	return e.Title == nil && e.Body == nil && e.State == nil &&
		e.Labels == nil && e.Assignees == nil && e.Milestone == nil
}

func (e IssueEdit) payload() map[string]any {
	p := map[string]any{}
	if e.Title != nil {
		p["title"] = *e.Title
	}
	if e.Body != nil {
		p["body"] = *e.Body
	}
	if e.State != nil {
		p["state"] = *e.State
	}
	if e.Labels != nil {
		p["labels"] = *e.Labels
	}
	if e.Assignees != nil {
		p["assignees"] = *e.Assignees
	}
	if e.Milestone != nil {
		if *e.Milestone == 0 {
			p["milestone"] = nil
		} else {
			p["milestone"] = *e.Milestone
		}
	}
	return p
}

// UpdateIssue applies an edit and returns the issue as GitHub now has it. The
// response is what the caller should show and store — it carries the new
// updated_at, and anything the server normalised along the way.
func (c *Client) UpdateIssue(ctx context.Context, tok *Token, repo string, number int, e IssueEdit) (Issue, error) {
	var is Issue
	path := fmt.Sprintf("/repos/%s/issues/%d", repo, number)
	if err := c.send(ctx, tok, http.MethodPatch, path, e.payload(), &is); err != nil {
		return Issue{}, err
	}
	return enrich(is, repo, tok), nil
}

// AddComment posts a comment and returns it.
func (c *Client) AddComment(ctx context.Context, tok *Token, repo string, number int, body string) (Comment, error) {
	var out Comment
	path := fmt.Sprintf("/repos/%s/issues/%d/comments", repo, number)
	err := c.send(ctx, tok, http.MethodPost, path, map[string]string{"body": body}, &out)
	return out, err
}

// CreateLabel defines a new label on the repo. Colour is a six-digit hex, with
// or without the leading hash.
func (c *Client) CreateLabel(ctx context.Context, tok *Token, repo, name, color string) (Label, error) {
	color = strings.TrimPrefix(strings.TrimSpace(color), "#")
	if color == "" {
		color = "6e7681"
	}
	var out Label
	in := map[string]string{"name": name, "color": color}
	err := c.send(ctx, tok, http.MethodPost, "/repos/"+repo+"/labels", in, &out)
	return out, err
}

// enrich fills the three fields the API does not carry but every consumer
// expects. Mirrors what the list path does per issue, so an issue that came back
// from a write is indistinguishable from a fetched one.
func enrich(is Issue, repo string, tok *Token) Issue {
	is.Repo = repo
	is.TokenName = tok.Name
	is.Due = nil
	if d, ok := is.DueDate(); ok {
		is.Due = &d
	}
	return is
}
