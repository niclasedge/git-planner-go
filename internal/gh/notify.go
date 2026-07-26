package gh

import (
	"context"
	"time"
)

// Notification is one entry from the notifications inbox. We care about which
// repo it belongs to, not the content.
type Notification struct {
	ID         string    `json:"id"`
	Reason     string    `json:"reason"`
	Unread     bool      `json:"unread"`
	UpdatedAt  time.Time `json:"updated_at"`
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
	Subject struct {
		Title string `json:"title"`
		URL   string `json:"url"`
		Type  string `json:"type"`
	} `json:"subject"`
}

// NotifyResult is the answer to "did anything move?".
type NotifyResult struct {
	// Changed is false when GitHub answered 304 — nothing moved, and the poll
	// cost no rate limit. This is the common case and the whole point.
	Changed bool
	// DirtyRepos are the repos mentioned in the inbox when something did move.
	DirtyRepos map[string]bool
	// PollInterval is GitHub's own advice via X-Poll-Interval. Honour it: polling
	// the inbox faster than asked can get the token throttled.
	PollInterval time.Duration
	Count        int
}

// Notifications polls the inbox as a cheap change trigger. Rather than asking
// every repo whether it changed, ask once here — a 304 means we can skip the
// whole refresh round.
//
// The request deliberately carries no `since` parameter: a moving timestamp
// would change the URL on every poll and defeat the ETag cache.
func (c *Client) Notifications(ctx context.Context, tok *Token) (NotifyResult, error) {
	var list []Notification
	res, err := c.get(ctx, tok, "/notifications?per_page=100", &list)
	if err != nil {
		return NotifyResult{}, err
	}

	out := NotifyResult{
		Changed:      !res.fromCache,
		PollInterval: res.pollInterval,
		Count:        len(list),
		DirtyRepos:   make(map[string]bool, len(list)),
	}
	if !out.Changed {
		// 304: the body is the same one we already acted on. Reporting its repos
		// as dirty would refresh them on every poll forever.
		return out, nil
	}
	for _, n := range list {
		if n.Repository.FullName != "" {
			out.DirtyRepos[n.Repository.FullName] = true
		}
	}
	return out, nil
}
