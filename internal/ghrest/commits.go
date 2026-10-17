package ghrest

// Commit und Vergleich (issue-branches, #50): der Kopf eines Branches mit
// Zeit und Betreff, und wie weit er gegen den Default-Branch voraus und
// zurueck ist. Beides per REST und ueber den sha adressiert, damit der
// ETag-Speicher greift.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// CommitKopf ist der letzte Commit eines Branches.
type CommitKopf struct {
	SHA string `json:"sha"`
	// Zeit ist das Committer-Datum (RFC 3339), sonst das Autor-Datum.
	Zeit string `json:"zeit"`
	// Betreff ist die erste Zeile der Nachricht.
	Betreff string `json:"betreff"`
	// Autor ist author.login, sonst commit.author.name (karte-einzeilig, #124).
	Autor string `json:"autor,omitempty"`
}

// Commit liefert Zeit und Betreff eines Commits.
func (c Client) Commit(ctx context.Context, nwo, sha string) (CommitKopf, error) {
	if !GueltigesNWO(nwo) || !shaRe.MatchString(sha) {
		return CommitKopf{}, fmt.Errorf("ungueltiger Commit %q in %q", sha, nwo)
	}
	b, _, err := c.holen(ctx, Basis()+"/repos/"+nwo+"/commits/"+url.PathEscape(sha))
	if err != nil {
		return CommitKopf{}, err
	}
	var roh rohCommitAutor
	if err := json.Unmarshal(b, &roh); err != nil {
		return CommitKopf{}, fmt.Errorf("GitHub: unlesbarer Commit: %v", err)
	}
	return roh.kopf(sha), nil
}

// rohCommitAutor ist ein Commit mit Autor, wie ihn commits/{sha} und compare
// liefern.
type rohCommitAutor struct {
	SHA    string `json:"sha"`
	Author *struct {
		Login string `json:"login"`
	} `json:"author"`
	Commit struct {
		Message string `json:"message"`
		Author  struct {
			Name string `json:"name"`
			Date string `json:"date"`
		} `json:"author"`
		Committer struct {
			Date string `json:"date"`
		} `json:"committer"`
	} `json:"commit"`
}

// kopf liest Zeit, Betreff und Autor; sha gilt, wenn die Antwort keinen nennt.
func (roh rohCommitAutor) kopf(sha string) CommitKopf {
	k := CommitKopf{SHA: roh.SHA, Zeit: roh.Commit.Committer.Date, Betreff: ErsteZeile(roh.Commit.Message)}
	if k.SHA == "" {
		k.SHA = sha
	}
	if k.Zeit == "" {
		k.Zeit = roh.Commit.Author.Date
	}
	k.Autor = roh.Commit.Author.Name
	if roh.Author != nil && roh.Author.Login != "" {
		k.Autor = roh.Author.Login
	}
	return k
}

// Vergleich liefert, wie viele Commits kopf gegen basis voraus und zurueck
// ist (compare/BASIS...KOPF).
func (c Client) Vergleich(ctx context.Context, nwo, basis, kopf string) (voraus, zurueck int, err error) {
	if !GueltigesNWO(nwo) || strings.TrimSpace(basis) == "" || strings.TrimSpace(kopf) == "" {
		return 0, 0, fmt.Errorf("ungueltiger Vergleich %q...%q in %q", basis, kopf, nwo)
	}
	b, _, err := c.holen(ctx, Basis()+"/repos/"+nwo+"/compare/"+url.PathEscape(basis)+"..."+url.PathEscape(kopf)+"?per_page=1")
	if err != nil {
		return 0, 0, err
	}
	var roh struct {
		Voraus  int `json:"ahead_by"`
		Zurueck int `json:"behind_by"`
	}
	if err := json.Unmarshal(b, &roh); err != nil {
		return 0, 0, fmt.Errorf("GitHub: unlesbarer Vergleich: %v", err)
	}
	return roh.Voraus, roh.Zurueck, nil
}

// ErsteZeile ist die erste Zeile einer Commit-Nachricht, ohne Rand.
func ErsteZeile(s string) string {
	s, _, _ = strings.Cut(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(s)
}
