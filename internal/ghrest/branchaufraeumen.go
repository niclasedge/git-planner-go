package ghrest

// Geschlossene Pull Requests (branch-aufraeumen, #78): ob der PR mit einem
// Branch als Head gemergt oder nur geschlossen wurde, entscheidet, ob der
// Branch als gemergt oder als verlassen gilt. Die Liste nennt merged nicht,
// nur merged_at.

import (
	"context"
	"fmt"
)

// GeschlossenePullRequests liefert die geschlossenen PRs; Gemergt steht,
// wenn merged_at gesetzt ist.
func (c Client) GeschlossenePullRequests(ctx context.Context, nwo string) ([]PR, error) {
	if !GueltigesNWO(nwo) {
		return nil, fmt.Errorf("ungueltiges Repo %q (erwartet owner/repo)", nwo)
	}
	roh, err := seiten[struct {
		rohPR
		MergedAt *string `json:"merged_at"`
	}](ctx, c, Basis()+"/repos/"+nwo+"/pulls?state=closed&per_page=100")
	if err != nil {
		return nil, err
	}
	out := make([]PR, 0, len(roh))
	for _, r := range roh {
		p := r.pr()
		p.Gemergt = p.Gemergt || (r.MergedAt != nil && *r.MergedAt != "")
		out = append(out, p)
	}
	return out, nil
}
