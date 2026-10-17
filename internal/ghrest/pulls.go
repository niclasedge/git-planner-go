package ghrest

// Pull Requests (pr-aktionen, #32): Liste, Status, Freigeben, Mergen, Branch
// loeschen. Alles per REST bis auf das Freigeben eines Drafts — dafuer hat
// GitHub nur die GraphQL-Mutation markPullRequestReadyForReview.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// PR ist ein offener Pull Request mit dem, was die Liste und die Aktionen
// brauchen.
type PR struct {
	Nummer       int     `json:"nummer"`
	Titel        string  `json:"titel"`
	URL          string  `json:"url"`
	Autor        string  `json:"autor,omitempty"`
	Labels       []Label `json:"labels"`
	Draft        bool    `json:"draft"`
	Head         string  `json:"head,omitempty"`
	HeadRepo     string  `json:"head_repo,omitempty"`
	HeadSHA      string  `json:"head_sha,omitempty"`
	Base         string  `json:"base,omitempty"`
	Aktualisiert string  `json:"aktualisiert,omitempty"`
	// Mergebar ist GitHubs mergeable: nil, solange GitHub noch rechnet.
	Mergebar    *bool  `json:"mergebar,omitempty"`
	MergeStatus string `json:"merge_status,omitempty"`
	// Abgleich ist der Konfliktzustand (konflikte-anzeigen, #62): sauber,
	// veraltet, konflikt oder unbekannt; Voraus und Zurueck der Abstand zur
	// Base, KonfliktDateien die Dateien beider Seiten bei konflikt.
	Abgleich        string   `json:"zustand,omitempty"`
	Voraus          int      `json:"voraus"`
	Zurueck         int      `json:"zurueck"`
	KonfliktDateien []string `json:"konflikt_dateien,omitempty"`
	// Checks sind die CI-Pruefungen auf dem Head-SHA (pr-checks, #80), als
	// eigenes Unterobjekt; nil, wenn sie nicht geholt werden konnten.
	Checks *PRChecks `json:"checks,omitempty"`
	// Reviews sind Entscheidung, Reviewer und Threads (pr-reviews, #81), als
	// eigenes Unterobjekt; nil, wenn sie nicht geholt werden konnten.
	Reviews *PRReviews `json:"reviews,omitempty"`
	// Angefordert und AngefordertTeams sind requested_reviewers und
	// requested_teams; sie gehen ueber Reviews ins JSON.
	Angefordert      []string `json:"-"`
	AngefordertTeams []string `json:"-"`
	NodeID           string   `json:"-"`
	// Text ist der Rumpf des PRs; er dient der Zuordnung zu Issues
	// (issue-branches, #50) und steht nicht im JSON.
	Text    string `json:"-"`
	Zustand string `json:"-"`
	Gemergt bool   `json:"-"`
	// Auto-Merge, Zustaendige und Milestone (pr-merge-optionen, #82);
	// GemergtAm und GeschlossenAm fuer die Historie geschlossener PRs.
	AutoMerge     *AutoMerge   `json:"auto_merge,omitempty"`
	Zustaendig    []string     `json:"zustaendig,omitempty"`
	Meilenstein   *Meilenstein `json:"meilenstein,omitempty"`
	GemergtAm     string       `json:"-"`
	GeschlossenAm string       `json:"-"`
	// Erstellt ist created_at; der Lebenslauf nennt es als Zeit der Stufe
	// pr (pr-lebenslauf, #83).
	Erstellt string `json:"-"`
}

type rohPR struct {
	Number    int                    `json:"number"`
	Title     string                 `json:"title"`
	Body      string                 `json:"body"`
	HTMLURL   string                 `json:"html_url"`
	Draft     bool                   `json:"draft"`
	NodeID    string                 `json:"node_id"`
	State     string                 `json:"state"`
	Merged    bool                   `json:"merged"`
	Mergeable *bool                  `json:"mergeable"`
	MState    string                 `json:"mergeable_state"`
	UpdatedAt string                 `json:"updated_at"`
	CreatedAt string                 `json:"created_at"`
	User      struct{ Login string } `json:"user"`
	Labels    []struct {
		Name  string `json:"name"`
		Color string `json:"color"`
	} `json:"labels"`
	Head struct {
		Ref  string `json:"ref"`
		SHA  string `json:"sha"`
		Repo *struct {
			FullName string `json:"full_name"`
		} `json:"repo"`
	} `json:"head"`
	Base struct {
		Ref string `json:"ref"`
	} `json:"base"`
	MergedAt  *string `json:"merged_at"`
	ClosedAt  *string `json:"closed_at"`
	AutoMerge *struct {
		MergeMethod string                 `json:"merge_method"`
		EnabledBy   struct{ Login string } `json:"enabled_by"`
	} `json:"auto_merge"`
	Assignees          []struct{ Login string } `json:"assignees"`
	Milestone          *rohMeilenstein          `json:"milestone"`
	RequestedReviewers []struct {
		Login string `json:"login"`
	} `json:"requested_reviewers"`
	RequestedTeams []struct {
		Slug string `json:"slug"`
	} `json:"requested_teams"`
}

func (r rohPR) pr() PR {
	p := PR{Nummer: r.Number, Titel: r.Title, URL: r.HTMLURL, Autor: r.User.Login, Labels: []Label{}, Draft: r.Draft,
		Head: r.Head.Ref, HeadSHA: r.Head.SHA, Base: r.Base.Ref, Aktualisiert: r.UpdatedAt, Mergebar: r.Mergeable,
		MergeStatus: r.MState, NodeID: r.NodeID, Text: r.Body, Zustand: r.State, Gemergt: r.Merged, Erstellt: r.CreatedAt}
	if r.Head.Repo != nil {
		p.HeadRepo = r.Head.Repo.FullName
	}
	if r.MergedAt != nil && *r.MergedAt != "" {
		p.Gemergt, p.GemergtAm = true, *r.MergedAt
	}
	if r.ClosedAt != nil {
		p.GeschlossenAm = *r.ClosedAt
	}
	if r.AutoMerge != nil {
		p.AutoMerge = &AutoMerge{Methode: strings.ToLower(r.AutoMerge.MergeMethod), Von: r.AutoMerge.EnabledBy.Login}
	}
	for _, a := range r.Assignees {
		p.Zustaendig = append(p.Zustaendig, a.Login)
	}
	p.Meilenstein = r.Milestone.meilenstein()
	for _, l := range r.Labels {
		p.Labels = append(p.Labels, Label{Name: l.Name, Farbe: l.Color})
	}
	for _, u := range r.RequestedReviewers {
		p.Angefordert = append(p.Angefordert, u.Login)
	}
	for _, t := range r.RequestedTeams {
		p.AngefordertTeams = append(p.AngefordertTeams, t.Slug)
	}
	return p
}

// PullRequests liefert die offenen PRs. mergeable steht in der Liste nicht —
// das liefert erst PullRequest je PR.
func (c Client) PullRequests(ctx context.Context, nwo string) ([]PR, error) {
	if !GueltigesNWO(nwo) {
		return nil, fmt.Errorf("ungueltiges Repo %q (erwartet owner/repo)", nwo)
	}
	roh, err := seiten[rohPR](ctx, c, Basis()+"/repos/"+nwo+"/pulls?state=open&per_page=100")
	if err != nil {
		return nil, err
	}
	out := make([]PR, 0, len(roh))
	for _, r := range roh {
		out = append(out, r.pr())
	}
	return out, nil
}

// PullRequest liefert einen PR samt mergeable und merged.
func (c Client) PullRequest(ctx context.Context, nwo string, nummer int) (PR, error) {
	if !GueltigesNWO(nwo) || nummer <= 0 {
		return PR{}, fmt.Errorf("ungueltiger PR %s#%d", nwo, nummer)
	}
	b, _, err := c.holen(ctx, fmt.Sprintf("%s/repos/%s/pulls/%d", Basis(), nwo, nummer))
	if err != nil {
		return PR{}, err
	}
	var r rohPR
	if err := json.Unmarshal(b, &r); err != nil {
		return PR{}, fmt.Errorf("GitHub: unlesbare Antwort: %v", err)
	}
	return r.pr(), nil
}

// PRFreigeben stellt einen Draft auf "ready for review".
func (c Client) PRFreigeben(ctx context.Context, nodeID string) error {
	if strings.TrimSpace(nodeID) == "" {
		return errors.New("PR ohne node_id")
	}
	rumpf := map[string]any{
		"query":     "mutation($id: ID!) { markPullRequestReadyForReview(input: {pullRequestId: $id}) { pullRequest { isDraft } } }",
		"variables": map[string]any{"id": nodeID},
	}
	b, _, err := c.anfrage(ctx, http.MethodPost, Basis()+"/graphql", rumpf)
	if err != nil {
		return err
	}
	var antwort struct {
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	_ = json.Unmarshal(b, &antwort)
	if len(antwort.Errors) > 0 {
		return errors.New(c.bereinigen("GraphQL: " + antwort.Errors[0].Message))
	}
	return nil
}

// PRMergen mergt mit Merge-Commit, aber nur, wenn der Head noch sha ist —
// sonst lehnt GitHub ab, statt einen Stand zu mergen, den niemand gesehen hat.
// Andere Methoden und eigener Text: PRMergenMit (pr-merge-optionen, #82).
func (c Client) PRMergen(ctx context.Context, nwo string, nummer int, sha string) error {
	return c.PRMergenMit(ctx, nwo, nummer, sha, MergeWahl{Methode: "merge"})
}

// BranchLoeschen loescht einen Branch.
func (c Client) BranchLoeschen(ctx context.Context, nwo, branch string) error {
	branch = strings.TrimSpace(branch)
	if !GueltigesNWO(nwo) || branch == "" || strings.Contains(branch, "..") {
		return fmt.Errorf("ungueltiger Branch %q in %q", branch, nwo)
	}
	teile := strings.Split(branch, "/")
	for i, t := range teile {
		teile[i] = url.PathEscape(t)
	}
	_, _, err := c.anfrage(ctx, http.MethodDelete, Basis()+"/repos/"+nwo+"/git/refs/heads/"+strings.Join(teile, "/"), nil)
	return err
}
