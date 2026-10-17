package ghrest

// Merge-Optionen am PR (pr-merge-optionen, #82): welche Methoden das Repo
// erlaubt, mergen mit Methode und eigenem Commit-Text, Auto-Merge an und aus,
// zurueck zum Entwurf, die letzten geschlossenen PRs, Wiederoeffnen und die
// Milestones des Repos. Auto-Merge und Entwurf gibt es bei GitHub nur als
// GraphQL-Mutation, alles andere per REST.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// AutoMerge ist ein eingeschalteter Auto-Merge: Methode klein geschrieben
// (merge, squash, rebase), Von der Login, der ihn eingeschaltet hat.
type AutoMerge struct {
	Methode string `json:"methode"`
	Von     string `json:"von,omitempty"`
}

// Meilenstein ist ein Milestone des Repos.
type Meilenstein struct {
	Nummer int    `json:"nummer"`
	Titel  string `json:"titel"`
}

type rohMeilenstein struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
}

func (r *rohMeilenstein) meilenstein() *Meilenstein {
	if r == nil || r.Number <= 0 {
		return nil
	}
	return &Meilenstein{Nummer: r.Number, Titel: r.Title}
}

// MergeErlaubt sind die Merge-Methoden, die das Repo zulaesst.
type MergeErlaubt struct {
	Merge  bool
	Squash bool
	Rebase bool
}

// MergeWahl ist eine gewaehlte Methode samt optionalem Commit-Titel und -Text.
type MergeWahl struct {
	Methode string
	Titel   string
	Text    string
}

// GueltigeMethode sagt, ob m eine der drei Merge-Methoden von GitHub ist.
func GueltigeMethode(m string) bool {
	return m == "merge" || m == "squash" || m == "rebase"
}

// MergeErlaubtHolen liest allow_merge_commit, allow_squash_merge und
// allow_rebase_merge. Ohne Push-Recht laesst GitHub die Felder weg; ein
// fehlendes Feld gilt als erlaubt — ablehnen wird dann GitHub selbst.
func (c Client) MergeErlaubtHolen(ctx context.Context, nwo string) (MergeErlaubt, error) {
	if !GueltigesNWO(nwo) {
		return MergeErlaubt{}, fmt.Errorf("ungueltiges Repo %q (erwartet owner/repo)", nwo)
	}
	b, _, err := c.holen(ctx, Basis()+"/repos/"+nwo)
	if err != nil {
		return MergeErlaubt{}, err
	}
	var r struct {
		Merge  *bool `json:"allow_merge_commit"`
		Squash *bool `json:"allow_squash_merge"`
		Rebase *bool `json:"allow_rebase_merge"`
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return MergeErlaubt{}, fmt.Errorf("GitHub: unlesbare Antwort: %v", err)
	}
	ja := func(p *bool) bool { return p == nil || *p }
	return MergeErlaubt{Merge: ja(r.Merge), Squash: ja(r.Squash), Rebase: ja(r.Rebase)}, nil
}

// PRMergenMit mergt mit der gewaehlten Methode (leer: merge) auf den Head sha;
// Titel und Text gehen nur mit, wenn sie gesetzt sind.
func (c Client) PRMergenMit(ctx context.Context, nwo string, nummer int, sha string, w MergeWahl) error {
	if !GueltigesNWO(nwo) || nummer <= 0 {
		return fmt.Errorf("ungueltiger PR %s#%d", nwo, nummer)
	}
	methode := w.Methode
	if methode == "" {
		methode = "merge"
	}
	if !GueltigeMethode(methode) {
		return fmt.Errorf("unbekannte Merge-Methode %q", methode)
	}
	rumpf := map[string]any{"merge_method": methode}
	if sha != "" {
		rumpf["sha"] = sha
	}
	if strings.TrimSpace(w.Titel) != "" {
		rumpf["commit_title"] = w.Titel
	}
	if strings.TrimSpace(w.Text) != "" {
		rumpf["commit_message"] = w.Text
	}
	_, _, err := c.anfrage(ctx, http.MethodPut, fmt.Sprintf("%s/repos/%s/pulls/%d/merge", Basis(), nwo, nummer), rumpf)
	return err
}

// mutation schickt eine GraphQL-Mutation und meldet den ersten Fehler.
func (c Client) mutation(ctx context.Context, nodeID, query string, variablen map[string]any) error {
	if strings.TrimSpace(nodeID) == "" {
		return errors.New("PR ohne node_id")
	}
	if variablen == nil {
		variablen = map[string]any{}
	}
	variablen["id"] = nodeID
	b, _, err := c.anfrage(ctx, http.MethodPost, Basis()+"/graphql", map[string]any{"query": query, "variables": variablen})
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

// AutoMergeAn schaltet Auto-Merge mit der gewaehlten Methode ein.
func (c Client) AutoMergeAn(ctx context.Context, nodeID string, w MergeWahl) error {
	if !GueltigeMethode(w.Methode) {
		return fmt.Errorf("unbekannte Merge-Methode %q", w.Methode)
	}
	v := map[string]any{"methode": strings.ToUpper(w.Methode)}
	kopf, rumpf := "", ""
	if strings.TrimSpace(w.Titel) != "" {
		v["titel"] = w.Titel
		kopf = ", commitHeadline: $titel"
		rumpf += ", $titel: String"
	}
	if strings.TrimSpace(w.Text) != "" {
		v["text"] = w.Text
		kopf += ", commitBody: $text"
		rumpf += ", $text: String"
	}
	q := "mutation($id: ID!, $methode: PullRequestMergeMethod" + rumpf + ") { enablePullRequestAutoMerge(input: {pullRequestId: $id, mergeMethod: $methode" +
		kopf + "}) { pullRequest { number } } }"
	return c.mutation(ctx, nodeID, q, v)
}

// AutoMergeAus schaltet Auto-Merge aus.
func (c Client) AutoMergeAus(ctx context.Context, nodeID string) error {
	return c.mutation(ctx, nodeID, "mutation($id: ID!) { disablePullRequestAutoMerge(input: {pullRequestId: $id}) { pullRequest { number } } }", nil)
}

// PRZuEntwurf macht einen PR wieder zum Entwurf — das Gegenstueck zu
// PRFreigeben.
func (c Client) PRZuEntwurf(ctx context.Context, nodeID string) error {
	return c.mutation(ctx, nodeID, "mutation($id: ID!) { convertPullRequestToDraft(input: {pullRequestId: $id}) { pullRequest { isDraft } } }", nil)
}

// GeschlossenePRs liefert die 30 zuletzt geaenderten geschlossenen PRs, eine
// Seite, ohne weiterzublaettern.
func (c Client) GeschlossenePRs(ctx context.Context, nwo string) ([]PR, error) {
	if !GueltigesNWO(nwo) {
		return nil, fmt.Errorf("ungueltiges Repo %q (erwartet owner/repo)", nwo)
	}
	b, _, err := c.holen(ctx, Basis()+"/repos/"+nwo+"/pulls?state=closed&sort=updated&direction=desc&per_page=30")
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

// PRWiederOeffnen oeffnet einen geschlossenen PR wieder.
func (c Client) PRWiederOeffnen(ctx context.Context, nwo string, nummer int) error {
	if !GueltigesNWO(nwo) || nummer <= 0 {
		return fmt.Errorf("ungueltiger PR %s#%d", nwo, nummer)
	}
	_, _, err := c.anfrage(ctx, http.MethodPatch, fmt.Sprintf("%s/repos/%s/pulls/%d", Basis(), nwo, nummer), map[string]any{"state": "open"})
	return err
}

// Meilensteine liefert die offenen Milestones des Repos.
func (c Client) Meilensteine(ctx context.Context, nwo string) ([]Meilenstein, error) {
	if !GueltigesNWO(nwo) {
		return nil, fmt.Errorf("ungueltiges Repo %q (erwartet owner/repo)", nwo)
	}
	roh, err := seiten[rohMeilenstein](ctx, c, Basis()+"/repos/"+nwo+"/milestones?state=open&per_page=100")
	if err != nil {
		return nil, err
	}
	out := make([]Meilenstein, 0, len(roh))
	for _, r := range roh {
		if m := r.meilenstein(); m != nil {
			out = append(out, *m)
		}
	}
	return out, nil
}
