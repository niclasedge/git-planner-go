package gh

import (
	"context"
	"fmt"
)

// IssueKey identifies an issue across repos: "owner/repo#123".
// The key form matters because both the gh and web packages build and consume it,
// and a mismatch would silently break the parent-child lookup.
func IssueKey(repo string, number int) string {
	return fmt.Sprintf("%s#%d", repo, number)
}

// SubIssues returns the numbers of an issue's sub-issues in GitHub's own
// order, which is the order they appear under the parent — for a wayfinder
// map that is map order, so the UI must not re-sort them.
func (c *Client) SubIssues(ctx context.Context, tok *Token, repo string, number int) ([]int, error) {
	path := fmt.Sprintf("/repos/%s/issues/%d/sub_issues?per_page=100", repo, number)
	var raw []struct {
		Number int `json:"number"`
	}
	if _, err := c.get(ctx, tok, path, &raw); err != nil {
		return nil, fmt.Errorf("%s#%d: %w", repo, number, err)
	}
	out := make([]int, len(raw))
	for i, s := range raw {
		out[i] = s.Number
	}
	return out, nil
}

// SubIssueMap fetches the child numbers for every parent in issues and keys
// them "owner/repo#number". Only issues whose summary reports children are
// asked, so the cost is the number of parents (13 of 566 issues in practice),
// not the number of issues — and every one of those requests is conditional.
func (c *Client) SubIssueMap(ctx context.Context, tok *Token, issues []Issue) (map[string][]int, []error) {
	out := map[string][]int{}

	// Collect only the parents: an issue with Sub.Total==0 cannot have children,
	// and asking for them would waste a request.
	type parentJob struct {
		repo   string
		number int
	}
	var parents []parentJob
	for _, is := range issues {
		if is.Sub.Total > 0 {
			parents = append(parents, parentJob{repo: is.Repo, number: is.Number})
		}
	}
	if len(parents) == 0 {
		return out, nil
	}

	type fetchResult struct {
		key  string
		kids []int
	}

	results := fanOut(parents, defaultConcurrency, func(p parentJob) (fetchResult, error) {
		kids, err := c.SubIssues(ctx, tok, p.repo, p.number)
		if err != nil {
			return fetchResult{}, err
		}
		return fetchResult{key: IssueKey(p.repo, p.number), kids: kids}, nil
	})

	var errs []error
	for _, r := range results {
		if r.Err != nil {
			errs = append(errs, r.Err)
			continue
		}
		out[r.Value.key] = r.Value.kids
	}
	return out, errs
}
