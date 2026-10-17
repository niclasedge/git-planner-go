package ghrest

// Workflow-Runs eines Repos (ci-runs, #122): die letzten Runs, je Run die
// Jobs samt Schritten, das Log eines Jobs und der Neustart der
// fehlgeschlagenen Jobs eines Runs. Alles per REST. Punkte, Folge und die
// Aufbereitung des Logs rechnet appctx (ciruns.go); hier stehen nur die
// Endpunkte und die Datentypen, die auch der Plattenspeicher ablegt.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// WorkflowRun ist ein Eintrag aus actions/runs.
type WorkflowRun struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Event   string `json:"event"`
	Branch  string `json:"branch"`
	HeadSHA string `json:"head_sha"`
	// Betreff ist die Commit-Nachricht des Heads, ohne sie display_title.
	Betreff      string `json:"betreff"`
	Nummer       int    `json:"nummer"`
	Versuch      int    `json:"versuch"`
	Status       string `json:"status"`
	Ergebnis     string `json:"ergebnis"`
	Start        string `json:"start"`
	Aktualisiert string `json:"aktualisiert"`
	URL          string `json:"url"`
}

// ActionsSchritt ist ein Schritt eines Jobs (steps[]).
type ActionsSchritt struct {
	Nummer   int    `json:"nummer"`
	Name     string `json:"name"`
	Status   string `json:"status"`
	Ergebnis string `json:"ergebnis"`
	Start    string `json:"start"`
	Ende     string `json:"ende"`
}

// ActionsJob ist ein Job eines Runs samt Schritten.
type ActionsJob struct {
	ID       int64            `json:"id"`
	RunID    int64            `json:"run_id"`
	Versuch  int              `json:"versuch"`
	Name     string           `json:"name"`
	Status   string           `json:"status"`
	Ergebnis string           `json:"ergebnis"`
	Start    string           `json:"start"`
	Ende     string           `json:"ende"`
	URL      string           `json:"url"`
	Schritte []ActionsSchritt `json:"schritte"`
}

// WorkflowRuns liefert die letzten anzahl Runs des Repos, neueste zuerst.
// Ein Repo ohne Actions antwortet mit 404 (HTTPStatus) oder einer leeren
// Liste.
func (c Client) WorkflowRuns(ctx context.Context, nwo string, anzahl int) ([]WorkflowRun, error) {
	if !GueltigesNWO(nwo) {
		return nil, fmt.Errorf("ungueltiges Repo %q (erwartet owner/repo)", nwo)
	}
	if anzahl <= 0 || anzahl > 100 {
		anzahl = 20
	}
	b, _, err := c.holen(ctx, Basis()+"/repos/"+nwo+"/actions/runs?per_page="+strconv.Itoa(anzahl))
	if err != nil {
		return nil, err
	}
	var roh struct {
		Runs []struct {
			ID           int64   `json:"id"`
			Name         string  `json:"name"`
			Event        string  `json:"event"`
			HeadBranch   string  `json:"head_branch"`
			HeadSHA      string  `json:"head_sha"`
			DisplayTitle string  `json:"display_title"`
			RunNumber    int     `json:"run_number"`
			RunAttempt   int     `json:"run_attempt"`
			Status       string  `json:"status"`
			Conclusion   *string `json:"conclusion"`
			RunStartedAt string  `json:"run_started_at"`
			CreatedAt    string  `json:"created_at"`
			UpdatedAt    string  `json:"updated_at"`
			HTMLURL      string  `json:"html_url"`
			HeadCommit   *struct {
				Message string `json:"message"`
			} `json:"head_commit"`
		} `json:"workflow_runs"`
	}
	if err := json.Unmarshal(b, &roh); err != nil {
		return nil, fmt.Errorf("GitHub: unlesbare Antwort: %v", err)
	}
	out := make([]WorkflowRun, 0, len(roh.Runs))
	for _, r := range roh.Runs {
		w := WorkflowRun{ID: r.ID, Name: r.Name, Event: r.Event, Branch: r.HeadBranch, HeadSHA: r.HeadSHA,
			Betreff: r.DisplayTitle, Nummer: r.RunNumber, Versuch: r.RunAttempt, Status: r.Status,
			Ergebnis: zeigerText(r.Conclusion), Start: r.RunStartedAt, Aktualisiert: r.UpdatedAt, URL: r.HTMLURL}
		if r.HeadCommit != nil && strings.TrimSpace(r.HeadCommit.Message) != "" {
			w.Betreff = r.HeadCommit.Message
		}
		if w.Start == "" {
			w.Start = r.CreatedAt
		}
		if w.Versuch <= 0 {
			w.Versuch = 1
		}
		out = append(out, w)
	}
	return out, nil
}

// RunJobs liefert die Jobs des juengsten Versuchs eines Runs samt Schritten.
func (c Client) RunJobs(ctx context.Context, nwo string, runID int64) ([]ActionsJob, error) {
	if !GueltigesNWO(nwo) || runID <= 0 {
		return nil, fmt.Errorf("ungueltiger Run %d in %q", runID, nwo)
	}
	b, _, err := c.holen(ctx, Basis()+"/repos/"+nwo+"/actions/runs/"+strconv.FormatInt(runID, 10)+"/jobs?per_page=100")
	if err != nil {
		return nil, err
	}
	var roh struct {
		Jobs []struct {
			ID          int64   `json:"id"`
			RunID       int64   `json:"run_id"`
			RunAttempt  int     `json:"run_attempt"`
			Name        string  `json:"name"`
			Status      string  `json:"status"`
			Conclusion  *string `json:"conclusion"`
			StartedAt   *string `json:"started_at"`
			CompletedAt *string `json:"completed_at"`
			HTMLURL     string  `json:"html_url"`
			Steps       []struct {
				Name        string  `json:"name"`
				Status      string  `json:"status"`
				Conclusion  *string `json:"conclusion"`
				Number      int     `json:"number"`
				StartedAt   *string `json:"started_at"`
				CompletedAt *string `json:"completed_at"`
			} `json:"steps"`
		} `json:"jobs"`
	}
	if err := json.Unmarshal(b, &roh); err != nil {
		return nil, fmt.Errorf("GitHub: unlesbare Antwort: %v", err)
	}
	out := make([]ActionsJob, 0, len(roh.Jobs))
	for _, j := range roh.Jobs {
		job := ActionsJob{ID: j.ID, RunID: j.RunID, Versuch: j.RunAttempt, Name: j.Name, Status: j.Status,
			Ergebnis: zeigerText(j.Conclusion), Start: zeigerText(j.StartedAt), Ende: zeigerText(j.CompletedAt),
			URL: j.HTMLURL, Schritte: []ActionsSchritt{}}
		for _, s := range j.Steps {
			job.Schritte = append(job.Schritte, ActionsSchritt{Nummer: s.Number, Name: s.Name, Status: s.Status,
				Ergebnis: zeigerText(s.Conclusion), Start: zeigerText(s.StartedAt), Ende: zeigerText(s.CompletedAt)})
		}
		out = append(out, job)
	}
	return out, nil
}

// JobLog liefert das Log eines Jobs. GitHub antwortet mit einer Weiterleitung
// auf einen kurzlebigen Speicherort; net/http folgt ihr und laesst den
// Authorization-Kopf dabei weg, sobald der Host wechselt. Ist das Log laenger
// als hoechstens Bytes, bleibt das Ende ab der ersten vollstaendigen Zeile,
// und gekuerzt ist true. Das Log geht am ETag-Speicher vorbei: es ist gross
// und wird ohnehin je Job gespeichert.
func (c Client) JobLog(ctx context.Context, nwo string, jobID int64, hoechstens int) (string, bool, error) {
	if !GueltigesNWO(nwo) || jobID <= 0 {
		return "", false, fmt.Errorf("ungueltiger Job %d in %q", jobID, nwo)
	}
	if hoechstens <= 0 {
		hoechstens = 2 << 20
	}
	ctx, abbrechen := context.WithTimeout(ctx, 2*aufrufBudget)
	defer abbrechen()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, Basis()+"/repos/"+nwo+"/actions/jobs/"+strconv.FormatInt(jobID, 10)+"/logs", nil)
	if err != nil {
		return "", false, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	zaehlerAufrufe.Add(1)
	resp, err := c.http().Do(req)
	if err != nil {
		zaehlerFehler.Add(1)
		return "", false, errors.New(c.bereinigen(err.Error()))
	}
	defer resp.Body.Close()
	rateMerken(resp.Header)
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		zaehlerFehler.Add(1)
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		var m struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(b, &m)
		text := strings.TrimSpace(m.Message)
		if text == "" {
			text = http.StatusText(resp.StatusCode)
		}
		return "", false, StatusFehler{Status: resp.StatusCode, text: c.bereinigen(fmt.Sprintf("GitHub %d: %s", resp.StatusCode, text))}
	}
	text, gekuerzt, err := ende(resp.Body, hoechstens)
	if err != nil {
		zaehlerFehler.Add(1)
		return "", false, errors.New(c.bereinigen(err.Error()))
	}
	return text, gekuerzt, nil
}

// ende liest r ganz und behaelt hoechstens die letzten n Bytes, beginnend
// an einer Zeilengrenze.
func ende(r io.Reader, n int) (string, bool, error) {
	puffer := make([]byte, 0, 64<<10)
	stueck := make([]byte, 64<<10)
	gekuerzt := false
	for {
		k, err := r.Read(stueck)
		puffer = append(puffer, stueck[:k]...)
		if len(puffer) > 2*n {
			puffer = append(puffer[:0], puffer[len(puffer)-n:]...)
			gekuerzt = true
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", false, err
		}
	}
	if len(puffer) > n {
		puffer = puffer[len(puffer)-n:]
		gekuerzt = true
	}
	if gekuerzt {
		if i := bytes.IndexByte(puffer, '\n'); i >= 0 && i+1 < len(puffer) {
			puffer = puffer[i+1:]
		}
	}
	return string(puffer), gekuerzt, nil
}

// RunNeuStarten startet die fehlgeschlagenen Jobs eines Runs neu.
func (c Client) RunNeuStarten(ctx context.Context, nwo string, runID int64) error {
	if !GueltigesNWO(nwo) || runID <= 0 {
		return fmt.Errorf("ungueltiger Run %d in %q", runID, nwo)
	}
	_, _, err := c.anfrage(ctx, http.MethodPost, Basis()+"/repos/"+nwo+"/actions/runs/"+strconv.FormatInt(runID, 10)+"/rerun-failed-jobs", nil)
	return err
}
