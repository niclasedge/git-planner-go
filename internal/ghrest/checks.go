package ghrest

// CI-Pruefungen je PR (pr-checks, #80): Check-Runs und Commit-Statuses auf
// dem Head-SHA lesen, einen einzelnen Check-Run lesen und einen
// fehlgeschlagenen neu starten. Alles per REST. Den Zustand je Pruefung, den
// Gesamtzustand und die Marke rechnet appctx.ChecksRechnen; hier stehen nur
// die Endpunkte und die Datentypen, die am PR haengen.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
)

// CheckRun ist ein Eintrag aus commits/{sha}/check-runs oder check-runs/{id}.
type CheckRun struct {
	ID          int64
	Name        string
	Status      string // queued, in_progress, completed, …
	Conclusion  string // success, failure, …; leer, solange er laeuft
	HeadSHA     string
	StartedAt   string
	CompletedAt string
	HTMLURL     string
	DetailsURL  string
	App         string // app.slug, etwa github-actions
}

// CommitStatus ist ein Eintrag aus commits/{sha}/status.
type CommitStatus struct {
	Kontext      string
	Zustand      string // success, failure, error, pending
	Beschreibung string
	URL          string
	Erstellt     string
	Aktualisiert string
}

// Pruefung ist eine Pruefung, wie die Oberflaeche sie zeigt.
type Pruefung struct {
	Name        string `json:"name"`
	Art         string `json:"art"`     // check oder status
	Zustand     string `json:"zustand"` // gruen, rot, laeuft, neutral
	Ergebnis    string `json:"ergebnis"`
	DauerS      int64  `json:"dauer_s"`
	Dauer       string `json:"dauer"`
	Link        string `json:"link,omitempty"`
	CheckRunID  int64  `json:"check_run_id,omitempty"`
	NeuStartbar bool   `json:"neu_startbar"`
}

// ChecksZahlen ist die Zusammenfassung der Pruefungen eines SHA.
type ChecksZahlen struct {
	Gruen   int    `json:"gruen"`
	Rot     int    `json:"rot"`
	Laeuft  int    `json:"laeuft"`
	Neutral int    `json:"neutral"`
	Gesamt  int    `json:"gesamt"`
	Text    string `json:"text"`
}

// PRChecks ist das Unterobjekt checks am PR.
type PRChecks struct {
	SHA             string       `json:"sha"`
	Gesamt          string       `json:"gesamt"` // gruen, rot, laeuft, keine
	Marke           string       `json:"marke"`
	Zahl            int          `json:"zahl,omitempty"`
	Zusammenfassung ChecksZahlen `json:"zusammenfassung"`
	Pruefungen      []Pruefung   `json:"pruefungen"`
	// Ende ist der spaeteste Abschluss, wenn keine Pruefung mehr laeuft
	// (pr-lebenslauf, #83); leer, solange eine laeuft.
	Ende string `json:"ende,omitempty"`
}

type rohCheckRun struct {
	ID          int64   `json:"id"`
	Name        string  `json:"name"`
	Status      string  `json:"status"`
	Conclusion  *string `json:"conclusion"`
	HeadSHA     string  `json:"head_sha"`
	StartedAt   *string `json:"started_at"`
	CompletedAt *string `json:"completed_at"`
	HTMLURL     string  `json:"html_url"`
	DetailsURL  string  `json:"details_url"`
	App         *struct {
		Slug string `json:"slug"`
	} `json:"app"`
}

func zeigerText(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func (r rohCheckRun) run() CheckRun {
	c := CheckRun{ID: r.ID, Name: r.Name, Status: r.Status, Conclusion: zeigerText(r.Conclusion), HeadSHA: r.HeadSHA,
		StartedAt: zeigerText(r.StartedAt), CompletedAt: zeigerText(r.CompletedAt), HTMLURL: r.HTMLURL, DetailsURL: r.DetailsURL}
	if r.App != nil {
		c.App = r.App.Slug
	}
	return c
}

func checkPfad(nwo, sha string) error {
	if !GueltigesNWO(nwo) || !shaRe.MatchString(sha) {
		return fmt.Errorf("ungueltiger Stand %q in %q", sha, nwo)
	}
	return nil
}

// CheckRuns liefert die Check-Runs auf sha (die neuesten je Name, bis 100).
func (c Client) CheckRuns(ctx context.Context, nwo, sha string) ([]CheckRun, error) {
	if err := checkPfad(nwo, sha); err != nil {
		return nil, err
	}
	b, _, err := c.holen(ctx, Basis()+"/repos/"+nwo+"/commits/"+sha+"/check-runs?per_page=100")
	if err != nil {
		return nil, err
	}
	var roh struct {
		CheckRuns []rohCheckRun `json:"check_runs"`
	}
	if err := json.Unmarshal(b, &roh); err != nil {
		return nil, fmt.Errorf("GitHub: unlesbare Antwort: %v", err)
	}
	out := make([]CheckRun, 0, len(roh.CheckRuns))
	for _, r := range roh.CheckRuns {
		out = append(out, r.run())
	}
	return out, nil
}

// CommitStatuses liefert die Statuses auf sha, je Kontext der neueste.
func (c Client) CommitStatuses(ctx context.Context, nwo, sha string) ([]CommitStatus, error) {
	if err := checkPfad(nwo, sha); err != nil {
		return nil, err
	}
	b, _, err := c.holen(ctx, Basis()+"/repos/"+nwo+"/commits/"+sha+"/status?per_page=100")
	if err != nil {
		return nil, err
	}
	var roh struct {
		Statuses []struct {
			Context     string `json:"context"`
			State       string `json:"state"`
			Description string `json:"description"`
			TargetURL   string `json:"target_url"`
			CreatedAt   string `json:"created_at"`
			UpdatedAt   string `json:"updated_at"`
		} `json:"statuses"`
	}
	if err := json.Unmarshal(b, &roh); err != nil {
		return nil, fmt.Errorf("GitHub: unlesbare Antwort: %v", err)
	}
	out := make([]CommitStatus, 0, len(roh.Statuses))
	for _, s := range roh.Statuses {
		out = append(out, CommitStatus{Kontext: s.Context, Zustand: s.State, Beschreibung: s.Description, URL: s.TargetURL,
			Erstellt: s.CreatedAt, Aktualisiert: s.UpdatedAt})
	}
	return out, nil
}

// CheckRunHolen liefert einen Check-Run.
func (c Client) CheckRunHolen(ctx context.Context, nwo string, id int64) (CheckRun, error) {
	if !GueltigesNWO(nwo) || id <= 0 {
		return CheckRun{}, fmt.Errorf("ungueltiger Check-Run %d in %q", id, nwo)
	}
	b, _, err := c.holen(ctx, Basis()+"/repos/"+nwo+"/check-runs/"+strconv.FormatInt(id, 10))
	if err != nil {
		return CheckRun{}, err
	}
	var r rohCheckRun
	if err := json.Unmarshal(b, &r); err != nil {
		return CheckRun{}, fmt.Errorf("GitHub: unlesbare Antwort: %v", err)
	}
	return r.run(), nil
}

var actionsLaufRe = regexp.MustCompile(`/actions/runs/(\d+)(?:/|$)`)

// ActionsLauf liefert die Id des Actions-Laufs, zu dem ein Check-Run gehoert,
// oder 0, wenn er kein Lauf von GitHub Actions ist.
func ActionsLauf(r CheckRun) int64 {
	if r.App != "github-actions" {
		return 0
	}
	for _, u := range []string{r.DetailsURL, r.HTMLURL} {
		if m := actionsLaufRe.FindStringSubmatch(u); m != nil {
			if id, err := strconv.ParseInt(m[1], 10, 64); err == nil && id > 0 {
				return id
			}
		}
	}
	return 0
}

// PruefungNeuStarten startet einen Check-Run neu: einen Actions-Lauf ueber
// rerun-failed-jobs ("actions"), jeden anderen ueber rerequest ("check-run").
func (c Client) PruefungNeuStarten(ctx context.Context, nwo string, r CheckRun) (string, error) {
	if !GueltigesNWO(nwo) {
		return "", fmt.Errorf("ungueltiges Repo %q", nwo)
	}
	if r.ID <= 0 {
		return "", errors.New("Check-Run ohne Id")
	}
	if lauf := ActionsLauf(r); lauf > 0 {
		_, _, err := c.anfrage(ctx, http.MethodPost, Basis()+"/repos/"+nwo+"/actions/runs/"+strconv.FormatInt(lauf, 10)+"/rerun-failed-jobs", nil)
		return "actions", err
	}
	_, _, err := c.anfrage(ctx, http.MethodPost, Basis()+"/repos/"+nwo+"/check-runs/"+strconv.FormatInt(r.ID, 10)+"/rerequest", nil)
	return "check-run", err
}
