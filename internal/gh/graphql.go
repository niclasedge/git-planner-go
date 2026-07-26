package gh

// GraphQL is used for exactly one thing: totals.
//
// REST can only count issues by listing them, so a closed-issue count over two
// hundred repos means two hundred requests and a few thousand issue bodies we
// would throw away. GraphQL answers the same question with a totalCount field
// that requests no nodes at all, and twenty repos fit in one query. That is what
// makes the "open / in progress / closed" trio in the sidebar affordable.
//
// The trade-off: POST cannot be conditional, so unlike every other call in this
// package a counts sweep is never free. It is also cheap enough not to care —
// totalCount-only queries cost ~1 point of the separate 5000/h GraphQL budget,
// so the whole sweep is a dozen points and runs on the hourly schedule rather
// than the minute one.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const (
	graphqlURL = apiBase + "/graphql"
	// graphqlBatch is how many repos share one query. The limit that bites is
	// response size and blast radius, not cost: a failed query loses a whole batch,
	// so keep batches small enough that losing one barely dents the page.
	graphqlBatch = 20
	// graphqlConcurrency is deliberately below defaultConcurrency. These are
	// writes as far as GitHub's abuse detection is concerned (POST), and there are
	// only a dozen of them.
	graphqlConcurrency = 4
)

// RepoCounts are the per-repo totals GraphQL provides. Open and PRs overlap with
// what the REST list already gives, but the REST list is truncated at
// issues.per-repo, so these are the authoritative numbers.
type RepoCounts struct {
	Open   int
	Closed int
	PRs    int
}

// CountSet is one sweep's outcome, keyed by "owner/repo".
type CountSet struct {
	Counts map[string]RepoCounts
	// Queries is how many POSTs it took. Unlike the REST sections there is no
	// cache-hit number to report, because there is no cache — this is the cost.
	Queries int
	Errors  []error
}

// Counts fetches issue and PR totals for every repo, in batches, in parallel.
//
// A repo that GraphQL cannot resolve is simply absent from the map: partial data
// is the normal case (GitHub answers 200 with both data and errors), and the
// caller falls back to counting the issues it already holds.
func (c *Client) Counts(ctx context.Context, tok *Token, repos []Repo) CountSet {
	set := CountSet{Counts: make(map[string]RepoCounts, len(repos))}
	if len(repos) == 0 {
		return set
	}

	batches := chunk(repos, graphqlBatch)
	results := fanOut(batches, graphqlConcurrency, func(b []Repo) (map[string]RepoCounts, error) {
		return c.countBatch(ctx, tok, b)
	})

	for _, r := range results {
		set.Queries++
		if r.Err != nil {
			set.Errors = append(set.Errors, r.Err)
		}
		for name, cnt := range r.Value {
			set.Counts[name] = cnt
		}
	}
	return set
}

// countNode mirrors one repository alias in the response.
type countNode struct {
	Open   struct{ TotalCount int } `json:"open"`
	Closed struct{ TotalCount int } `json:"closed"`
	PRs    struct{ TotalCount int } `json:"prs"`
}

// graphqlError is one entry from the response's errors array. GitHub names the
// offending repo in Message, which is what makes it worth surfacing.
type graphqlError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

func (c *Client) countBatch(ctx context.Context, tok *Token, repos []Repo) (map[string]RepoCounts, error) {
	// Owner and name go in as variables rather than into the query text. They come
	// from the API so they are already well-formed, but a query built by string
	// concatenation is a habit worth not having.
	var (
		decls   []string
		fields  []string
		aliases = make(map[string]string, len(repos))
		vars    = make(map[string]any, len(repos)*2)
	)
	for i, r := range repos {
		a := fmt.Sprintf("r%d", i)
		aliases[a] = r.FullName
		decls = append(decls, fmt.Sprintf("$o%d:String!,$n%d:String!", i, i))
		fields = append(fields, fmt.Sprintf(
			"%s:repository(owner:$o%d,name:$n%d){"+
				"open:issues(states:OPEN){totalCount} "+
				"closed:issues(states:CLOSED){totalCount} "+
				"prs:pullRequests(states:OPEN){totalCount}}", a, i, i))
		vars[fmt.Sprintf("o%d", i)] = r.Owner.Login
		vars[fmt.Sprintf("n%d", i)] = r.Name
	}
	query := "query(" + strings.Join(decls, ",") + "){" + strings.Join(fields, " ") + "}"

	var resp struct {
		Data   map[string]*countNode `json:"data"`
		Errors []graphqlError        `json:"errors"`
	}
	if err := c.graphql(ctx, tok, query, vars, &resp); err != nil {
		return nil, err
	}

	out := make(map[string]RepoCounts, len(repos))
	for alias, node := range resp.Data {
		// A null alias means the repo could not be resolved — renamed, deleted, or
		// not visible to this token. The matching entry in Errors says which.
		if node == nil {
			continue
		}
		name, ok := aliases[alias]
		if !ok {
			continue
		}
		out[name] = RepoCounts{
			Open:   node.Open.TotalCount,
			Closed: node.Closed.TotalCount,
			PRs:    node.PRs.TotalCount,
		}
	}

	var err error
	if len(resp.Errors) > 0 {
		// Partial failure: report the first message and keep the rows that worked.
		// Naming the repo is left to the errors' own text, which includes it.
		err = fmt.Errorf("graphql: %s (%d of %d aliases failed)",
			resp.Errors[0].Message, len(repos)-len(out), len(repos))
	}
	return out, err
}

// graphql posts one query. No conditional request and no cache: GraphQL is POST,
// so ETags do not apply.
func (c *Client) graphql(ctx context.Context, tok *Token, query string, vars map[string]any, out any) error {
	payload, err := json.Marshal(map[string]any{"query": query, "variables": vars})
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, graphqlURL, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Accept", acceptHeader)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tok.secret)
	req.Header.Set("User-Agent", "git-planner-go")

	resp, err := c.http.Do(req)
	if err != nil {
		tok.note(func() { tok.errored++ })
		return fmt.Errorf("POST /graphql: %w", err)
	}
	defer func() {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()

	// The rate-limit headers here describe the *GraphQL* budget, which is a
	// separate 5000/h pool counted in points. Folding them into the token's REST
	// numbers would corrupt the one instrument that shows the caching working, so
	// they are deliberately ignored.

	switch resp.StatusCode {
	case http.StatusOK:
		// 200 does not mean success: GraphQL reports per-field failures in the body.
		// Decoding is the caller's business.
	case http.StatusUnauthorized:
		tok.note(func() { tok.errored++ })
		return fmt.Errorf("token %s: %w", tok.Name, ErrBadCredentials)
	case http.StatusForbidden, http.StatusTooManyRequests:
		tok.note(func() { tok.errored++ })
		if resp.Header.Get("X-RateLimit-Remaining") == "0" {
			return fmt.Errorf("%w: token %s (graphql budget)", ErrRateLimited, tok.Name)
		}
		return fmt.Errorf("POST /graphql: %w (check the PAT scopes)", ErrForbidden)
	default:
		tok.note(func() { tok.errored++ })
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return fmt.Errorf("POST /graphql: %s: %s", resp.Status, strings.TrimSpace(string(snippet)))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		tok.note(func() { tok.errored++ })
		return fmt.Errorf("POST /graphql: reading body: %w", err)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("POST /graphql: decoding: %w", err)
	}
	return nil
}

func chunk[T any](items []T, size int) [][]T {
	if size <= 0 {
		size = 1
	}
	out := make([][]T, 0, (len(items)+size-1)/size)
	for i := 0; i < len(items); i += size {
		end := min(i+size, len(items))
		out = append(out, items[i:end])
	}
	return out
}
