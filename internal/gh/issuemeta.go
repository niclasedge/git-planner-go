package gh

import (
	"context"
	"fmt"
	"time"
)

// The lists below are what the edit form offers to pick from. They are read on
// demand — when someone opens the form or a comment thread — rather than kept in
// the hub: they are needed for one repo at a time, and every one of them is a
// conditional GET, so re-opening the same form costs a 304.
//
// per_page=100 without pagination is deliberate. A repo with more than a hundred
// labels or milestones would truncate, which degrades a picker; following the
// pages would cost a request per opened form for a case that does not exist here.

// Comment is one issue comment.
type Comment struct {
	ID        int64     `json:"id"`
	Body      string    `json:"body"`
	User      User      `json:"user"`
	HTMLURL   string    `json:"html_url"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Edited reports whether the comment was changed after it was written.
func (c Comment) Edited() bool { return c.UpdatedAt.After(c.CreatedAt.Add(time.Minute)) }

// IssueComments lists one issue's comments, oldest first.
func (c *Client) IssueComments(ctx context.Context, tok *Token, repo string, number int) ([]Comment, error) {
	var out []Comment
	path := fmt.Sprintf("/repos/%s/issues/%d/comments?per_page=100", repo, number)
	if _, err := c.get(ctx, tok, path, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// RepoLabels lists the labels defined on a repo.
func (c *Client) RepoLabels(ctx context.Context, tok *Token, repo string) ([]Label, error) {
	var out []Label
	if _, err := c.get(ctx, tok, "/repos/"+repo+"/labels?per_page=100", &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Assignables lists the users an issue in this repo can be assigned to.
func (c *Client) Assignables(ctx context.Context, tok *Token, repo string) ([]User, error) {
	var out []User
	if _, err := c.get(ctx, tok, "/repos/"+repo+"/assignees?per_page=100", &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Milestones lists the open milestones of a repo.
func (c *Client) Milestones(ctx context.Context, tok *Token, repo string) ([]Milestone, error) {
	var out []Milestone
	if _, err := c.get(ctx, tok, "/repos/"+repo+"/milestones?state=open&per_page=100", &out); err != nil {
		return nil, err
	}
	return out, nil
}
