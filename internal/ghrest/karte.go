package ghrest

// Sub-Issues und alle Blocker fuer die Karte eines Repos (repo-karte, #36).

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
)

// SubIssues liefert die Sub-Issues eines Parent-Issues.
func (c Client) SubIssues(ctx context.Context, nwo string, nummer int) ([]Issue, error) {
	if !GueltigesNWO(nwo) || nummer <= 0 {
		return nil, fmt.Errorf("ungueltiges Issue %s#%d", nwo, nummer)
	}
	roh, err := seiten[rohIssue](ctx, c, fmt.Sprintf("%s/repos/%s/issues/%d/sub_issues?per_page=100", Basis(), nwo, nummer))
	if err != nil {
		return nil, err
	}
	out := make([]Issue, 0, len(roh))
	for _, r := range roh {
		out = append(out, r.issue())
	}
	return out, nil
}

// AlleBlockerVon liefert die Nummern ALLER Issues desselben Repos, die nummer
// blockieren oder blockiert haben — anders als BlockiertDurch auch die
// geschlossenen: die Karte zeigt den Verlauf, nicht nur die offene Arbeit.
func (c Client) AlleBlockerVon(ctx context.Context, nwo string, nummer int) ([]int, error) {
	if !GueltigesNWO(nwo) || nummer <= 0 {
		return nil, fmt.Errorf("ungueltiges Issue %s#%d", nwo, nummer)
	}
	b, _, err := c.holen(ctx, fmt.Sprintf("%s/repos/%s/issues/%d/dependencies/blocked_by?per_page=100", Basis(), nwo, nummer))
	if err != nil {
		return nil, err
	}
	var roh []struct {
		Number        int    `json:"number"`
		RepositoryURL string `json:"repository_url"`
	}
	if err := json.Unmarshal(b, &roh); err != nil {
		return nil, fmt.Errorf("GitHub: unlesbare Antwort: %v", err)
	}
	out := []int{}
	for _, r := range roh {
		if r.RepositoryURL != "" && !strings.HasSuffix(strings.ToLower(r.RepositoryURL), "/repos/"+strings.ToLower(nwo)) {
			continue
		}
		out = append(out, r.Number)
	}
	sort.Ints(out)
	return out, nil
}

// GeschlosseneIssuesSeit liefert alle Issues (ohne PRs), die seit seit
// geschlossen wurden — fuer die Karte der letzten 14 Tage. since filtert bei
// GitHub nach updated_at; closed_at wird hier nachgeprueft.
func (c Client) GeschlosseneIssuesSeit(ctx context.Context, nwo string, seit time.Time) ([]Issue, error) {
	if !GueltigesNWO(nwo) {
		return nil, fmt.Errorf("ungueltiges Repo %q (erwartet owner/repo)", nwo)
	}
	grenze := seit.UTC().Format(time.RFC3339)
	roh, err := seiten[rohIssue](ctx, c, Basis()+"/repos/"+nwo+"/issues?state=closed&per_page=100&since="+url.QueryEscape(grenze))
	if err != nil {
		return nil, err
	}
	out := []Issue{}
	for _, r := range roh {
		if r.PR != nil || r.ClosedAt < grenze {
			continue
		}
		out = append(out, r.issue())
	}
	return out, nil
}
