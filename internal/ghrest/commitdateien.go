package ghrest

// Die juengsten Commits eines Branches und die Dateien eines Commits
// (beleg-bilder, #105): Grundlage der "Geaenderten Bilder". Eine Seite von
// VerlaufTiefe Commits, damit die Rate nicht aufgebraucht wird; ein Commit
// ist ueber seinen sha unveraenderlich, der Aufrufer darf seine Dateien ohne
// Ablauf zwischenspeichern.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// CommitDatei ist eine Datei, die ein Commit beruehrt.
type CommitDatei struct {
	Pfad string `json:"pfad"`
	// SHA ist der Blob-SHA der Datei nach dem Commit.
	SHA string `json:"sha"`
	// Status wie bei GitHub: added, modified, removed, renamed, …
	Status string `json:"status"`
	// AlterPfad ist bei einer Umbenennung der vorige Name.
	AlterPfad string `json:"alter_pfad,omitempty"`
}

// CommitMitDateien ist ein Commit samt der Dateien, die er beruehrt.
type CommitMitDateien struct {
	CommitKopf
	Dateien []CommitDatei `json:"dateien"`
}

// JuengsteCommits liefert die letzten VerlaufTiefe Commits ab ref (Branch
// oder sha; leer = Default-Branch), neueste zuerst.
func (c Client) JuengsteCommits(ctx context.Context, nwo, ref string) ([]CommitKopf, error) {
	if !GueltigesNWO(nwo) {
		return nil, fmt.Errorf("ungueltiges Repo %q (erwartet owner/repo)", nwo)
	}
	q := url.Values{}
	if strings.TrimSpace(ref) != "" {
		q.Set("sha", ref)
	}
	q.Set("per_page", fmt.Sprint(VerlaufTiefe))
	b, _, err := c.holen(ctx, Basis()+"/repos/"+nwo+"/commits?"+q.Encode())
	if err != nil {
		return nil, err
	}
	var roh []rohCommit
	if err := json.Unmarshal(b, &roh); err != nil {
		return nil, fmt.Errorf("GitHub: unlesbare Commit-Liste: %v", err)
	}
	out := make([]CommitKopf, 0, len(roh))
	for _, r := range roh {
		out = append(out, r.kopf())
	}
	return out, nil
}

// CommitDateien liefert Kopf und Dateien eines Commits.
func (c Client) CommitDateien(ctx context.Context, nwo, sha string) (CommitMitDateien, error) {
	if !GueltigesNWO(nwo) || !shaRe.MatchString(sha) {
		return CommitMitDateien{}, fmt.Errorf("ungueltiger Commit %q in %q", sha, nwo)
	}
	b, _, err := c.holen(ctx, Basis()+"/repos/"+nwo+"/commits/"+url.PathEscape(sha))
	if err != nil {
		return CommitMitDateien{}, err
	}
	var roh struct {
		rohCommit
		Files []struct {
			Filename         string `json:"filename"`
			PreviousFilename string `json:"previous_filename"`
			SHA              string `json:"sha"`
			Status           string `json:"status"`
		} `json:"files"`
	}
	if err := json.Unmarshal(b, &roh); err != nil {
		return CommitMitDateien{}, fmt.Errorf("GitHub: unlesbarer Commit: %v", err)
	}
	out := CommitMitDateien{CommitKopf: roh.kopf(), Dateien: make([]CommitDatei, 0, len(roh.Files))}
	if out.SHA == "" {
		out.SHA = sha
	}
	for _, f := range roh.Files {
		out.Dateien = append(out.Dateien, CommitDatei{Pfad: f.Filename, SHA: f.SHA, Status: f.Status, AlterPfad: f.PreviousFilename})
	}
	return out, nil
}
