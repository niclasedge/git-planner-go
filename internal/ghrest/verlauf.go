package ghrest

// Verlauf einer Datei (aufgaben-zeitleiste, #55): die Commits, die einen Pfad
// beruehrt haben, und je Commit der Patch dieser Datei. Beides per REST; ein
// Commit ist ueber seinen sha unveraenderlich, der Aufrufer darf ihn ohne
// Ablauf zwischenspeichern.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// VerlaufTiefe ist die Zahl der Commits, die CommitsFuerPfad hoechstens
// liefert — eine Seite, damit ein langer Verlauf die Rate nicht aufbraucht.
const VerlaufTiefe = 30

type rohCommit struct {
	SHA    string `json:"sha"`
	Commit struct {
		Message string `json:"message"`
		Author  struct {
			Date string `json:"date"`
		} `json:"author"`
		Committer struct {
			Date string `json:"date"`
		} `json:"committer"`
	} `json:"commit"`
}

func (r rohCommit) kopf() CommitKopf {
	k := CommitKopf{SHA: r.SHA, Zeit: r.Commit.Committer.Date, Betreff: ErsteZeile(r.Commit.Message)}
	if k.Zeit == "" {
		k.Zeit = r.Commit.Author.Date
	}
	return k
}

// CommitsFuerPfad liefert die letzten VerlaufTiefe Commits ab dem Commit (oder
// Branch) ab, die pfad beruehrt haben, neueste zuerst.
func (c Client) CommitsFuerPfad(ctx context.Context, nwo, ab, pfad string) ([]CommitKopf, error) {
	if !GueltigesNWO(nwo) || strings.TrimSpace(ab) == "" || strings.TrimSpace(pfad) == "" {
		return nil, fmt.Errorf("ungueltiger Verlauf %q ab %q in %q", pfad, ab, nwo)
	}
	q := url.Values{}
	q.Set("sha", ab)
	q.Set("path", pfad)
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

// CommitPatch liefert Kopf eines Commits und den Patch der Datei pfad darin
// (auch, wenn sie in diesem Commit umbenannt wurde und pfad ihr alter Name
// ist). Beruehrt der Commit die Datei nicht oder hat GitHub keinen Patch
// (binaer, zu gross, reine Umbenennung), ist der Patch leer.
func (c Client) CommitPatch(ctx context.Context, nwo, sha, pfad string) (CommitKopf, string, error) {
	if !GueltigesNWO(nwo) || !shaRe.MatchString(sha) {
		return CommitKopf{}, "", fmt.Errorf("ungueltiger Commit %q in %q", sha, nwo)
	}
	b, _, err := c.holen(ctx, Basis()+"/repos/"+nwo+"/commits/"+url.PathEscape(sha))
	if err != nil {
		return CommitKopf{}, "", err
	}
	var roh struct {
		rohCommit
		Files []struct {
			Filename         string `json:"filename"`
			PreviousFilename string `json:"previous_filename"`
			Patch            string `json:"patch"`
		} `json:"files"`
	}
	if err := json.Unmarshal(b, &roh); err != nil {
		return CommitKopf{}, "", fmt.Errorf("GitHub: unlesbarer Commit: %v", err)
	}
	k := roh.kopf()
	if k.SHA == "" {
		k.SHA = sha
	}
	for _, f := range roh.Files {
		if f.Filename == pfad || f.PreviousFilename == pfad {
			return k, f.Patch, nil
		}
	}
	return k, "", nil
}
