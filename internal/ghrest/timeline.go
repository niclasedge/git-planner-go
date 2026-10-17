package ghrest

// Timeline eines Issues und die PRs zu einem Commit (zuletzt-erledigt-pr,
// #116): daraus kommt der PR, der ein Issue geschlossen hat. Gelesen wird nur,
// was die Zuordnung braucht; gedeutet wird in internal/appctx.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
)

// TimelineEreignis ist ein Eintrag aus /issues/{n}/timeline. CommitID ist
// leer, wenn GitHub null liefert. Quelle ist bei cross-referenced und
// connected der verweisende PR (nil, wenn die Quelle kein PR ist), QuelleRepo
// dessen Repo.
type TimelineEreignis struct {
	Ereignis   string
	CommitID   string
	Zeit       string
	Quelle     *PR
	QuelleRepo string
}

type rohTimeline struct {
	Event     string  `json:"event"`
	CommitID  *string `json:"commit_id"`
	CreatedAt string  `json:"created_at"`
	Source    *struct {
		Issue *struct {
			Number      int    `json:"number"`
			Title       string `json:"title"`
			Body        string `json:"body"`
			HTMLURL     string `json:"html_url"`
			State       string `json:"state"`
			PullRequest *struct {
				MergedAt *string `json:"merged_at"`
				HTMLURL  string  `json:"html_url"`
			} `json:"pull_request"`
			Repository *struct {
				FullName string `json:"full_name"`
			} `json:"repository"`
		} `json:"issue"`
	} `json:"source"`
}

// IssueTimeline liefert die Ereignisse der Timeline eines Issues in der
// Folge, in der GitHub sie liefert.
func (c Client) IssueTimeline(ctx context.Context, nwo string, nummer int) ([]TimelineEreignis, error) {
	if !GueltigesNWO(nwo) || nummer <= 0 {
		return nil, fmt.Errorf("ungueltiges Issue %s#%d", nwo, nummer)
	}
	roh, err := seiten[rohTimeline](ctx, c, fmt.Sprintf("%s/repos/%s/issues/%d/timeline?per_page=100", Basis(), nwo, nummer))
	if err != nil {
		return nil, err
	}
	out := make([]TimelineEreignis, 0, len(roh))
	for _, r := range roh {
		e := TimelineEreignis{Ereignis: r.Event, Zeit: r.CreatedAt}
		if r.CommitID != nil {
			e.CommitID = *r.CommitID
		}
		if r.Source != nil && r.Source.Issue != nil && r.Source.Issue.PullRequest != nil {
			i := r.Source.Issue
			p := &PR{Nummer: i.Number, Titel: i.Title, Text: i.Body, URL: i.HTMLURL, Zustand: i.State, Labels: []Label{}}
			if p.URL == "" {
				p.URL = i.PullRequest.HTMLURL
			}
			if m := i.PullRequest.MergedAt; m != nil && *m != "" {
				p.Gemergt, p.GemergtAm = true, *m
			}
			e.Quelle = p
			if i.Repository != nil {
				e.QuelleRepo = i.Repository.FullName
			}
		}
		out = append(out, e)
	}
	return out, nil
}

// PRsZuCommit liefert die PRs, die den Commit sha enthalten
// (GET /repos/{nwo}/commits/{sha}/pulls).
func (c Client) PRsZuCommit(ctx context.Context, nwo, sha string) ([]PR, error) {
	if !GueltigesNWO(nwo) || sha == "" {
		return nil, fmt.Errorf("ungueltiger Commit %s@%s", nwo, sha)
	}
	b, _, err := c.holen(ctx, Basis()+"/repos/"+nwo+"/commits/"+url.PathEscape(sha)+"/pulls")
	if err != nil {
		return nil, err
	}
	var roh []rohPR
	if err := json.Unmarshal(b, &roh); err != nil {
		return nil, fmt.Errorf("GitHub: unlesbare Antwort: %v", err)
	}
	out := make([]PR, 0, len(roh))
	for _, r := range roh {
		out = append(out, r.pr())
	}
	return out, nil
}
