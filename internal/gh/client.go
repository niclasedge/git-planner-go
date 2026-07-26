// Package gh talks to the GitHub API using conditional requests.
//
// Every GET carries If-None-Match when we hold an ETag. GitHub answers 304 Not
// Modified and — measured against the live API — does not charge the request
// against the rate limit. So a poll that finds no change is free, which is what
// lets the dashboard refresh often without burning through 5000/h.
package gh

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/niclasedge/git-planner-go/internal/config"
	"github.com/niclasedge/git-planner-go/internal/store"
)

const (
	apiBase        = "https://api.github.com"
	acceptHeader   = "application/vnd.github+json"
	apiVersion     = "2022-11-28"
	requestTimeout = 15 * time.Second
)

var (
	// ErrRateLimited means the token has no budget left; the caller should back
	// off rather than retry.
	ErrRateLimited = errors.New("rate limit exhausted")
	// ErrForbidden is a 403 that is not a rate limit — almost always a missing
	// PAT scope. Retrying will not help, so callers should stop asking.
	ErrForbidden = errors.New("forbidden")
	// ErrNotFound covers a repo with the feature disabled (Actions off) or one
	// the token cannot see. Also permanent for this URL.
	ErrNotFound = errors.New("not found")
	// ErrBadCredentials is a 401: the token is invalid, revoked or expired. The
	// most common cause in practice is not a wrong token but the wrong *source* —
	// a stale export in the shell shadowing the .env file.
	ErrBadCredentials = errors.New("bad credentials: token invalid, revoked or expired")
)

// Token is one identity plus its live rate-limit state.
type Token struct {
	Name  string
	Label string

	secret string

	mu        sync.Mutex
	limit     int
	remaining int
	reset     time.Time
	// hits counts responses served from cache after a 304, i.e. requests that
	// cost no rate limit. saved is the headline number for the status page.
	hits    int
	misses  int
	errored int
}

func (t *Token) Snapshot() TokenStatus {
	t.mu.Lock()
	defer t.mu.Unlock()
	return TokenStatus{
		Name:      t.Name,
		Label:     t.Label,
		Limit:     t.limit,
		Remaining: t.remaining,
		Reset:     t.reset,
		CacheHits: t.hits,
		Misses:    t.misses,
		Errors:    t.errored,
	}
}

type TokenStatus struct {
	Name      string    `json:"name"`
	Label     string    `json:"label"`
	Limit     int       `json:"limit"`
	Remaining int       `json:"remaining"`
	Reset     time.Time `json:"reset"`
	CacheHits int       `json:"cache_hits_304"`
	Misses    int       `json:"fetched_200"`
	Errors    int       `json:"errors"`
}

// hasBudget keeps a small reserve so an interactive request is never starved by
// the background refresher.
func (t *Token) hasBudget(reserve int) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.limit == 0 {
		return true // not yet known
	}
	if !t.reset.IsZero() && time.Now().After(t.reset) {
		return true // window rolled over
	}
	return t.remaining > reserve
}

type Client struct {
	tokens []*Token
	byName map[string]*Token
	store  *store.Store
	http   *http.Client
}

func New(tokens []config.Token, st *store.Store) *Client {
	c := &Client{
		store:  st,
		byName: make(map[string]*Token, len(tokens)),
		http: &http.Client{
			Timeout: requestTimeout,
			Transport: &http.Transport{
				MaxIdleConnsPerHost: 8,
				DisableCompression:  false,
			},
		},
	}
	for _, t := range tokens {
		tok := &Token{Name: t.Name, Label: t.Label, secret: t.Secret}
		c.tokens = append(c.tokens, tok)
		c.byName[t.Name] = tok
	}
	return c
}

func (c *Client) Tokens() []*Token { return c.tokens }

func (c *Client) Token(name string) *Token { return c.byName[name] }

func (c *Client) Status() []TokenStatus {
	out := make([]TokenStatus, 0, len(c.tokens))
	for _, t := range c.tokens {
		out = append(out, t.Snapshot())
	}
	return out
}

// result carries what a conditional GET produced.
type result struct {
	body []byte
	// fromCache is true when the server answered 304 and the body came from
	// SQLite — meaning the call cost no rate limit.
	fromCache bool
	// pollInterval mirrors X-Poll-Interval when GitHub sends one.
	pollInterval time.Duration
}

// get performs a conditional GET and decodes the body into out.
//
// path may be an absolute URL (for pagination) or a path relative to the API
// root. The (token, url) pair is the cache key.
func (c *Client) get(ctx context.Context, tok *Token, path string, out any) (*result, error) {
	return c.doGet(ctx, tok, path, out, true)
}

// getFresh is get without the cache, for URLs that are not worth storing.
//
// The one caller is the incremental issue poll, whose ?since= parameter moves
// with the clock: every poll is a new cache key, so caching it would add a row
// per poll forever and never produce a hit. Measured on the live API, /issues
// does not honour If-None-Match anyway — it answers 200 with a fresh ETag even
// when the body is byte-identical.
func (c *Client) getFresh(ctx context.Context, tok *Token, path string, out any) (*result, error) {
	return c.doGet(ctx, tok, path, out, false)
}

func (c *Client) doGet(ctx context.Context, tok *Token, path string, out any, useCache bool) (*result, error) {
	if !tok.hasBudget(50) {
		return nil, fmt.Errorf("%w: token %s", ErrRateLimited, tok.Name)
	}

	url := path
	if !strings.HasPrefix(url, "http") {
		url = apiBase + path
	}

	var cached *store.Entry
	if useCache {
		var err error
		cached, err = c.store.Get(tok.Name, url)
		if err != nil {
			// A broken cache must not stop us fetching; carry on unconditionally.
			cached = nil
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", acceptHeader)
	req.Header.Set("X-GitHub-Api-Version", apiVersion)
	req.Header.Set("Authorization", "Bearer "+tok.secret)
	req.Header.Set("User-Agent", "git-planner-go")

	if cached != nil {
		if cached.ETag != "" {
			req.Header.Set("If-None-Match", cached.ETag)
		} else if cached.LastModified != "" {
			req.Header.Set("If-Modified-Since", cached.LastModified)
		}
	}

	resp, err := c.http.Do(req)
	if err != nil {
		tok.note(func() { tok.errored++ })
		return nil, fmt.Errorf("GET %s: %w", url, err)
	}
	defer func() {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()

	tok.readRateHeaders(resp.Header)

	// Note: pagination deliberately does not use the Link header. GitHub omits it
	// on a 304, so anything derived from it breaks exactly when the cache works.
	res := &result{
		pollInterval: parsePollInterval(resp.Header.Get("X-Poll-Interval")),
	}

	switch resp.StatusCode {
	case http.StatusNotModified:
		if cached == nil {
			// Should not happen: 304 without a stored body. Treat as a miss so
			// the next round fetches unconditionally.
			return nil, fmt.Errorf("GET %s: 304 but nothing cached", url)
		}
		tok.note(func() { tok.hits++ })
		_ = c.store.Touch(tok.Name, url)
		res.body = cached.Body
		res.fromCache = true

	case http.StatusOK:
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			tok.note(func() { tok.errored++ })
			return nil, fmt.Errorf("GET %s: reading body: %w", url, err)
		}
		tok.note(func() { tok.misses++ })
		if useCache {
			if err := c.store.Put(tok.Name, url,
				resp.Header.Get("ETag"), resp.Header.Get("Last-Modified"), body); err != nil {
				// Cache write failure is not fatal — we still have the data.
				_ = err
			}
		}
		res.body = body

	case http.StatusUnauthorized:
		tok.note(func() { tok.errored++ })
		return nil, fmt.Errorf("token %s: %w", tok.Name, ErrBadCredentials)

	case http.StatusForbidden, http.StatusTooManyRequests:
		tok.note(func() { tok.errored++ })
		if resp.Header.Get("X-RateLimit-Remaining") == "0" {
			return nil, fmt.Errorf("%w: token %s", ErrRateLimited, tok.Name)
		}
		return nil, fmt.Errorf("GET %s: %w (check the PAT scopes)", url, ErrForbidden)

	// 410 Gone is what GitHub returns when a repo has issues switched off — the
	// same "this feature does not exist here" meaning as 404.
	case http.StatusNotFound, http.StatusGone:
		tok.note(func() { tok.errored++ })
		return nil, fmt.Errorf("GET %s: %w", url, ErrNotFound)

	default:
		tok.note(func() { tok.errored++ })
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return nil, fmt.Errorf("GET %s: %s: %s", url, resp.Status, strings.TrimSpace(string(snippet)))
	}

	if out != nil && len(res.body) > 0 {
		if err := json.Unmarshal(res.body, out); err != nil {
			return nil, fmt.Errorf("GET %s: decoding: %w", url, err)
		}
	}
	return res, nil
}

func (t *Token) note(f func()) {
	t.mu.Lock()
	defer t.mu.Unlock()
	f()
}

// readRateHeaders folds one response's rate-limit headers into the token state.
//
// Within a window GitHub's remaining only ever falls, but we read responses from
// ten concurrent workers, so they arrive out of order and the last writer is not
// the most recent value. Taking it verbatim made the number jitter by dozens —
// which matters, because /api/status is the instrument for the whole caching
// claim. So keep the lowest value seen in a window, and reset on a new one.
func (t *Token) readRateHeaders(h http.Header) {
	limit, errL := strconv.Atoi(h.Get("X-RateLimit-Limit"))
	remaining, errR := strconv.Atoi(h.Get("X-RateLimit-Remaining"))
	if errL != nil || errR != nil {
		return
	}
	var reset time.Time
	if secs, err := strconv.ParseInt(h.Get("X-RateLimit-Reset"), 10, 64); err == nil {
		reset = time.Unix(secs, 0)
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	first := t.reset.IsZero() && t.limit == 0
	t.limit = limit
	if first || reset.After(t.reset) {
		// First response, or a new window: this response is the truth.
		t.reset = reset
		t.remaining = remaining
		return
	}
	if remaining < t.remaining {
		t.remaining = remaining
	}
}

func parsePollInterval(v string) time.Duration {
	if v == "" {
		return 0
	}
	secs, err := strconv.Atoi(v)
	if err != nil || secs <= 0 {
		return 0
	}
	return time.Duration(secs) * time.Second
}
