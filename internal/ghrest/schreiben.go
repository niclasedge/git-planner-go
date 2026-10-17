package ghrest

// Schreibaufrufe auf Issues (issue-verwaltung, #26). Dieselben Regeln wie beim
// Lesen: nur owner/repo, Fehler als "GitHub <status>: <message>", der Token
// verlaesst das Paket nie.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// NeuesIssue ist der Inhalt eines anzulegenden Issues.
type NeuesIssue struct {
	Titel      string
	Text       string
	Labels     []string
	Zustaendig []string
}

// IssueAenderung setzt genau die Felder, die nicht nil sind. Labels und
// Zustaendig ersetzen die bisherigen.
type IssueAenderung struct {
	Titel      *string
	Text       *string
	Zustand    *string // "open" oder "closed"
	Grund      *string // "completed" oder "not_planned"; nur beim Schliessen
	Labels     *[]string
	Zustaendig *[]string
	// Meilenstein ist die Nummer des Milestones; 0 entfernt ihn
	// (pr-merge-optionen, #82).
	Meilenstein *int
}

// Kommentar ist ein Kommentar unter einem Issue.
type Kommentar struct {
	Autor string `json:"autor"`
	Text  string `json:"text"`
	Zeit  string `json:"zeit"`
	URL   string `json:"url,omitempty"`
}

func (c Client) issueAntwort(b []byte) (Issue, error) {
	var r rohIssue
	if err := json.Unmarshal(b, &r); err != nil {
		return Issue{}, fmt.Errorf("GitHub: unlesbare Antwort: %v", err)
	}
	return r.issue(), nil
}

func oderLeer(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// IssueAnlegen legt ein Issue an.
func (c Client) IssueAnlegen(ctx context.Context, nwo string, n NeuesIssue) (Issue, error) {
	if !GueltigesNWO(nwo) {
		return Issue{}, fmt.Errorf("ungueltiges Repo %q (erwartet owner/repo)", nwo)
	}
	rumpf := map[string]any{"title": n.Titel, "body": n.Text, "labels": oderLeer(n.Labels), "assignees": oderLeer(n.Zustaendig)}
	b, _, err := c.anfrage(ctx, http.MethodPost, Basis()+"/repos/"+nwo+"/issues", rumpf)
	if err != nil {
		return Issue{}, err
	}
	return c.issueAntwort(b)
}

// IssueAendern setzt die gesendeten Felder eines Issues.
func (c Client) IssueAendern(ctx context.Context, nwo string, nummer int, a IssueAenderung) (Issue, error) {
	if !GueltigesNWO(nwo) || nummer <= 0 {
		return Issue{}, fmt.Errorf("ungueltiges Issue %s#%d", nwo, nummer)
	}
	rumpf := map[string]any{}
	if a.Titel != nil {
		rumpf["title"] = *a.Titel
	}
	if a.Text != nil {
		rumpf["body"] = *a.Text
	}
	if a.Zustand != nil {
		switch *a.Zustand {
		case "closed":
			rumpf["state"] = "closed"
			grund := "completed"
			if a.Grund != nil && *a.Grund != "" {
				grund = *a.Grund
			}
			rumpf["state_reason"] = grund
		case "open":
			rumpf["state"] = "open"
		default:
			return Issue{}, fmt.Errorf("unbekannter Zustand %q (open oder closed)", *a.Zustand)
		}
	}
	if a.Labels != nil {
		rumpf["labels"] = oderLeer(*a.Labels)
	}
	if a.Zustaendig != nil {
		rumpf["assignees"] = oderLeer(*a.Zustaendig)
	}
	if a.Meilenstein != nil {
		switch m := *a.Meilenstein; {
		case m < 0:
			return Issue{}, fmt.Errorf("ungueltiger Milestone %d", m)
		case m == 0:
			rumpf["milestone"] = nil
		default:
			rumpf["milestone"] = m
		}
	}
	if len(rumpf) == 0 {
		return Issue{}, errors.New("nichts zu aendern")
	}
	b, _, err := c.anfrage(ctx, http.MethodPatch, fmt.Sprintf("%s/repos/%s/issues/%d", Basis(), nwo, nummer), rumpf)
	if err != nil {
		return Issue{}, err
	}
	return c.issueAntwort(b)
}

type rohKommentar struct {
	User      struct{ Login string } `json:"user"`
	Body      string                 `json:"body"`
	CreatedAt string                 `json:"created_at"`
	HTMLURL   string                 `json:"html_url"`
}

func (r rohKommentar) kommentar() Kommentar {
	return Kommentar{Autor: r.User.Login, Text: r.Body, Zeit: r.CreatedAt, URL: r.HTMLURL}
}

// Kommentare liefert die Kommentare eines Issues, aeltester zuerst.
func (c Client) Kommentare(ctx context.Context, nwo string, nummer int) ([]Kommentar, error) {
	if !GueltigesNWO(nwo) || nummer <= 0 {
		return nil, fmt.Errorf("ungueltiges Issue %s#%d", nwo, nummer)
	}
	roh, err := seiten[rohKommentar](ctx, c, fmt.Sprintf("%s/repos/%s/issues/%d/comments?per_page=100", Basis(), nwo, nummer))
	if err != nil {
		return nil, err
	}
	out := make([]Kommentar, 0, len(roh))
	for _, r := range roh {
		out = append(out, r.kommentar())
	}
	return out, nil
}

// Kommentieren legt einen Kommentar an.
func (c Client) Kommentieren(ctx context.Context, nwo string, nummer int, text string) (Kommentar, error) {
	if !GueltigesNWO(nwo) || nummer <= 0 {
		return Kommentar{}, fmt.Errorf("ungueltiges Issue %s#%d", nwo, nummer)
	}
	b, _, err := c.anfrage(ctx, http.MethodPost, fmt.Sprintf("%s/repos/%s/issues/%d/comments", Basis(), nwo, nummer), map[string]any{"body": text})
	if err != nil {
		return Kommentar{}, err
	}
	var r rohKommentar
	if err := json.Unmarshal(b, &r); err != nil {
		return Kommentar{}, fmt.Errorf("GitHub: unlesbare Antwort: %v", err)
	}
	return r.kommentar(), nil
}

func (c Client) datenbankID(ctx context.Context, nwo string, nummer int) (int64, error) {
	i, err := c.Issue(ctx, nwo, nummer)
	if err != nil {
		return 0, err
	}
	if i.ID == 0 {
		return 0, fmt.Errorf("GitHub nannte fuer #%d keine Id", nummer)
	}
	return i.ID, nil
}

// BlockerSetzen legt an, dass blocker das Issue nummer blockiert.
func (c Client) BlockerSetzen(ctx context.Context, nwo string, nummer, blocker int) error {
	if nummer == blocker {
		return errors.New("ein Issue kann sich nicht selbst blockieren")
	}
	if !GueltigesNWO(nwo) || nummer <= 0 || blocker <= 0 {
		return fmt.Errorf("ungueltige Abhaengigkeit %s#%d <- #%d", nwo, nummer, blocker)
	}
	id, err := c.datenbankID(ctx, nwo, blocker)
	if err != nil {
		return err
	}
	_, _, err = c.anfrage(ctx, http.MethodPost, fmt.Sprintf("%s/repos/%s/issues/%d/dependencies/blocked_by", Basis(), nwo, nummer), map[string]any{"issue_id": id})
	return err
}

// BlockerLoesen hebt die Abhaengigkeit wieder auf.
func (c Client) BlockerLoesen(ctx context.Context, nwo string, nummer, blocker int) error {
	if !GueltigesNWO(nwo) || nummer <= 0 || blocker <= 0 || nummer == blocker {
		return fmt.Errorf("ungueltige Abhaengigkeit %s#%d <- #%d", nwo, nummer, blocker)
	}
	id, err := c.datenbankID(ctx, nwo, blocker)
	if err != nil {
		return err
	}
	_, _, err = c.anfrage(ctx, http.MethodDelete, fmt.Sprintf("%s/repos/%s/issues/%d/dependencies/blocked_by/%d", Basis(), nwo, nummer, id), nil)
	return err
}

// Labels liefert die Labels des Repos.
func (c Client) Labels(ctx context.Context, nwo string) ([]Label, error) {
	if !GueltigesNWO(nwo) {
		return nil, fmt.Errorf("ungueltiges Repo %q (erwartet owner/repo)", nwo)
	}
	roh, err := seiten[struct {
		Name  string `json:"name"`
		Color string `json:"color"`
	}](ctx, c, Basis()+"/repos/"+nwo+"/labels?per_page=100")
	if err != nil {
		return nil, err
	}
	out := make([]Label, 0, len(roh))
	for _, r := range roh {
		out = append(out, Label{Name: r.Name, Farbe: r.Color})
	}
	return out, nil
}

// MoeglicheZustaendige liefert die Logins, denen Issues zugewiesen werden
// koennen.
func (c Client) MoeglicheZustaendige(ctx context.Context, nwo string) ([]string, error) {
	if !GueltigesNWO(nwo) {
		return nil, fmt.Errorf("ungueltiges Repo %q (erwartet owner/repo)", nwo)
	}
	roh, err := seiten[struct {
		Login string `json:"login"`
	}](ctx, c, Basis()+"/repos/"+nwo+"/assignees?per_page=100")
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(roh))
	for _, r := range roh {
		out = append(out, r.Login)
	}
	return out, nil
}
