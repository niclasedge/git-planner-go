package ghrest

// Reviews je PR (pr-reviews, #81): Reviews und Review-Kommentare per REST
// lesen, Reviewer anfordern und entfernen, im Thread antworten. Den Zustand
// der Threads (isResolved) und das Aufloesen gibt es nur per GraphQL
// (reviewThreads, resolveReviewThread, unresolveReviewThread) — REST kennt
// keine Threads. Entscheidung, Stimmen und Gruppierung rechnet
// appctx.ReviewsRechnen; hier stehen nur die Endpunkte und die Datentypen, die
// am PR haengen.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// Review ist ein Eintrag aus pulls/{n}/reviews.
type Review struct {
	ID      int64
	Autor   string
	Zustand string // APPROVED, CHANGES_REQUESTED, COMMENTED, DISMISSED, PENDING
	Zeit    string // submitted_at
}

// ReviewKommentar ist ein Kommentar an einer Code-Stelle
// (pulls/{n}/comments). Pfad, Zeilen und Diff gehen in den Thread, nicht ins
// JSON des Kommentars.
type ReviewKommentar struct {
	ID         int64  `json:"id"`
	AntwortAuf int64  `json:"antwort_auf,omitempty"`
	Autor      string `json:"autor"`
	Text       string `json:"text"`
	Zeit       string `json:"zeit"`
	URL        string `json:"url,omitempty"`

	Pfad          string `json:"-"`
	Zeile         int    `json:"-"` // line; 0, wenn die Stelle veraltet ist
	OriginalZeile int    `json:"-"`
	StartZeile    int    `json:"-"`
	Veraltet      bool   `json:"-"` // line ist null
	Diff          string `json:"-"` // diff_hunk
}

// ThreadZustand ist der Zustand eines Review-Threads aus GraphQL.
type ThreadZustand struct {
	ID         string // Knoten-Id des Threads (PRRT_…)
	Aufgeloest bool
}

// ReviewStimme ist die letzte zaehlende Review einer Person.
type ReviewStimme struct {
	Login   string `json:"login"`
	Zustand string `json:"zustand"` // genehmigt, aenderungen, verworfen, kommentiert
	Zeit    string `json:"zeit,omitempty"`
}

// ReviewThread ist ein Thread an einer Code-Stelle.
type ReviewThread struct {
	// ID ist die Id des ersten Kommentars; Ziel von antworten und thread.
	ID         int64             `json:"id"`
	Pfad       string            `json:"pfad"`
	Zeile      int               `json:"zeile,omitempty"`
	StartZeile int               `json:"start_zeile,omitempty"`
	Veraltet   bool              `json:"veraltet"`
	Diff       string            `json:"diff,omitempty"`
	Aufgeloest bool              `json:"aufgeloest"`
	Kommentare []ReviewKommentar `json:"kommentare"`
}

// PRReviews ist das Unterobjekt reviews am PR.
type PRReviews struct {
	Entscheidung       string         `json:"entscheidung"` // genehmigt, aenderungen, offen
	Marke              string         `json:"marke"`        // ✓, ✎ oder leer
	Titel              string         `json:"titel"`
	Stimmen            []ReviewStimme `json:"stimmen"`
	Angefordert        []string       `json:"angefordert"`
	AngefordertTeams   []string       `json:"angefordert_teams"`
	Threads            []ReviewThread `json:"threads"`
	OffeneThreads      int            `json:"offene_threads"`
	AufgeloesteThreads int            `json:"aufgeloeste_threads"`
	// ThreadZustand sagt, ob GraphQL den Zustand der Threads geliefert hat;
	// ohne ihn gelten alle Threads als offen.
	ThreadZustand bool `json:"thread_zustand"`
}

func prPfad(nwo string, nummer int) (string, error) {
	if !GueltigesNWO(nwo) || nummer <= 0 {
		return "", fmt.Errorf("ungueltiger PR %s#%d", nwo, nummer)
	}
	return fmt.Sprintf("%s/repos/%s/pulls/%d", Basis(), nwo, nummer), nil
}

// Reviews liefert die Reviews eines PRs in GitHubs Reihenfolge.
func (c Client) Reviews(ctx context.Context, nwo string, nummer int) ([]Review, error) {
	basis, err := prPfad(nwo, nummer)
	if err != nil {
		return nil, err
	}
	roh, err := seiten[struct {
		ID   int64 `json:"id"`
		User *struct {
			Login string `json:"login"`
		} `json:"user"`
		State       string `json:"state"`
		SubmittedAt string `json:"submitted_at"`
	}](ctx, c, basis+"/reviews?per_page=100")
	if err != nil {
		return nil, err
	}
	out := make([]Review, 0, len(roh))
	for _, r := range roh {
		v := Review{ID: r.ID, Zustand: r.State, Zeit: r.SubmittedAt}
		if r.User != nil {
			v.Autor = r.User.Login
		}
		out = append(out, v)
	}
	return out, nil
}

type rohReviewKommentar struct {
	ID           int64                   `json:"id"`
	InReplyToID  int64                   `json:"in_reply_to_id"`
	Body         string                  `json:"body"`
	User         *struct{ Login string } `json:"user"`
	Path         string                  `json:"path"`
	Line         *int                    `json:"line"`
	OriginalLine *int                    `json:"original_line"`
	StartLine    *int                    `json:"start_line"`
	DiffHunk     string                  `json:"diff_hunk"`
	CreatedAt    string                  `json:"created_at"`
	HTMLURL      string                  `json:"html_url"`
}

func zeigerZahl(z *int) int {
	if z == nil {
		return 0
	}
	return *z
}

func (r rohReviewKommentar) kommentar() ReviewKommentar {
	k := ReviewKommentar{ID: r.ID, AntwortAuf: r.InReplyToID, Text: r.Body, Zeit: r.CreatedAt, URL: r.HTMLURL, Pfad: r.Path,
		Zeile: zeigerZahl(r.Line), OriginalZeile: zeigerZahl(r.OriginalLine), StartZeile: zeigerZahl(r.StartLine),
		Veraltet: r.Line == nil, Diff: r.DiffHunk}
	if r.User != nil {
		k.Autor = r.User.Login
	}
	return k
}

// ReviewKommentare liefert alle Review-Kommentare eines PRs.
func (c Client) ReviewKommentare(ctx context.Context, nwo string, nummer int) ([]ReviewKommentar, error) {
	basis, err := prPfad(nwo, nummer)
	if err != nil {
		return nil, err
	}
	roh, err := seiten[rohReviewKommentar](ctx, c, basis+"/comments?per_page=100")
	if err != nil {
		return nil, err
	}
	out := make([]ReviewKommentar, 0, len(roh))
	for _, r := range roh {
		out = append(out, r.kommentar())
	}
	return out, nil
}

// graphql schickt eine GraphQL-Anfrage und entpackt data nach ziel.
func (c Client) graphql(ctx context.Context, query string, variablen map[string]any, ziel any) error {
	b, _, err := c.anfrage(ctx, http.MethodPost, Basis()+"/graphql", map[string]any{"query": query, "variables": variablen})
	if err != nil {
		return err
	}
	var antwort struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(b, &antwort); err != nil {
		return fmt.Errorf("GraphQL: unlesbare Antwort: %v", err)
	}
	if len(antwort.Errors) > 0 {
		return errors.New(c.bereinigen("GraphQL: " + antwort.Errors[0].Message))
	}
	if ziel == nil || len(antwort.Data) == 0 {
		return nil
	}
	if err := json.Unmarshal(antwort.Data, ziel); err != nil {
		return fmt.Errorf("GraphQL: unlesbare Antwort: %v", err)
	}
	return nil
}

const threadsAbfrage = `query($owner: String!, $name: String!, $nummer: Int!) {
  repository(owner: $owner, name: $name) {
    pullRequest(number: $nummer) {
      reviewThreads(first: 100) { nodes { id isResolved comments(first: 1) { nodes { databaseId } } } }
    }
  }
}`

// ThreadZustaende liefert je Thread (Schluessel: Id des ersten Kommentars)
// Knoten-Id und aufgeloest. Die ersten 100 Threads eines PRs.
func (c Client) ThreadZustaende(ctx context.Context, nwo string, nummer int) (map[int64]ThreadZustand, error) {
	if _, err := prPfad(nwo, nummer); err != nil {
		return nil, err
	}
	owner, name, _ := strings.Cut(nwo, "/")
	var data struct {
		Repository *struct {
			PullRequest *struct {
				ReviewThreads struct {
					Nodes []struct {
						ID         string `json:"id"`
						IsResolved bool   `json:"isResolved"`
						Comments   struct {
							Nodes []struct {
								DatabaseID int64 `json:"databaseId"`
							} `json:"nodes"`
						} `json:"comments"`
					} `json:"nodes"`
				} `json:"reviewThreads"`
			} `json:"pullRequest"`
		} `json:"repository"`
	}
	if err := c.graphql(ctx, threadsAbfrage, map[string]any{"owner": owner, "name": name, "nummer": nummer}, &data); err != nil {
		return nil, err
	}
	if data.Repository == nil || data.Repository.PullRequest == nil {
		return nil, fmt.Errorf("GraphQL: PR %s#%d nicht gefunden", nwo, nummer)
	}
	out := map[int64]ThreadZustand{}
	for _, n := range data.Repository.PullRequest.ReviewThreads.Nodes {
		if len(n.Comments.Nodes) == 0 || n.Comments.Nodes[0].DatabaseID <= 0 {
			continue
		}
		out[n.Comments.Nodes[0].DatabaseID] = ThreadZustand{ID: n.ID, Aufgeloest: n.IsResolved}
	}
	return out, nil
}

func (c Client) reviewerSchicken(ctx context.Context, methode, nwo string, nummer int, logins []string) error {
	basis, err := prPfad(nwo, nummer)
	if err != nil {
		return err
	}
	if len(logins) == 0 {
		return errors.New("keine Reviewer genannt")
	}
	_, _, err = c.anfrage(ctx, methode, basis+"/requested_reviewers", map[string]any{"reviewers": logins})
	return err
}

// ReviewerAnfordern fragt logins als Reviewer an.
func (c Client) ReviewerAnfordern(ctx context.Context, nwo string, nummer int, logins []string) error {
	return c.reviewerSchicken(ctx, http.MethodPost, nwo, nummer, logins)
}

// ReviewerEntfernen nimmt die Anfrage an logins zurueck.
func (c Client) ReviewerEntfernen(ctx context.Context, nwo string, nummer int, logins []string) error {
	return c.reviewerSchicken(ctx, http.MethodDelete, nwo, nummer, logins)
}

// ReviewAntworten antwortet im Thread des ersten Kommentars kommentarID.
func (c Client) ReviewAntworten(ctx context.Context, nwo string, nummer int, kommentarID int64, text string) (ReviewKommentar, error) {
	basis, err := prPfad(nwo, nummer)
	if err != nil {
		return ReviewKommentar{}, err
	}
	if kommentarID <= 0 {
		return ReviewKommentar{}, errors.New("Kommentar ohne Id")
	}
	if strings.TrimSpace(text) == "" {
		return ReviewKommentar{}, errors.New("leere Antwort")
	}
	b, _, err := c.anfrage(ctx, http.MethodPost, basis+"/comments/"+strconv.FormatInt(kommentarID, 10)+"/replies", map[string]any{"body": text})
	if err != nil {
		return ReviewKommentar{}, err
	}
	var r rohReviewKommentar
	if err := json.Unmarshal(b, &r); err != nil {
		return ReviewKommentar{}, fmt.Errorf("GitHub: unlesbare Antwort: %v", err)
	}
	return r.kommentar(), nil
}

// ThreadUmstellen loest einen Thread auf (aufgeloest) oder oeffnet ihn wieder.
func (c Client) ThreadUmstellen(ctx context.Context, threadID string, aufgeloest bool) error {
	if strings.TrimSpace(threadID) == "" {
		return errors.New("Thread ohne Id")
	}
	mutation := "unresolveReviewThread"
	if aufgeloest {
		mutation = "resolveReviewThread"
	}
	query := "mutation($id: ID!) { " + mutation + "(input: {threadId: $id}) { thread { isResolved } } }"
	return c.graphql(ctx, query, map[string]any{"id": threadID}, nil)
}
