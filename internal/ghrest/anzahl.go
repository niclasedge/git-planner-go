package ghrest

// Zaehlen ohne Liste (things-farben-fortschritt, #41): ein Abruf mit
// per_page=1, die Zahl der Seiten und damit der Eintraege steht im Link-Kopf
// unter rel="last". Der Kopf wird mit dem ETag gemerkt, ein 304 liefert
// dieselbe Zahl.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
)

var letzteRe = regexp.MustCompile(`<([^>]+)>;\s*rel="last"`)

// anzahl zaehlt die Eintraege hinter adresse (die per_page=1 traegt).
func (c Client) anzahl(ctx context.Context, adresse string) (int, error) {
	b, kopf, err := c.holen(ctx, adresse)
	if err != nil {
		return 0, err
	}
	if m := letzteRe.FindStringSubmatch(kopf.Get("Link")); m != nil {
		if u, err := url.Parse(m[1]); err == nil {
			if n, err := strconv.Atoi(u.Query().Get("page")); err == nil {
				return n, nil
			}
		}
	}
	// Ohne rel="last" gibt es hoechstens eine Seite: 0 oder 1 Eintrag.
	var seite []json.RawMessage
	if err := json.Unmarshal(b, &seite); err != nil {
		return 0, fmt.Errorf("GitHub: unlesbare Antwort: %v", err)
	}
	return len(seite), nil
}

// GeschlosseneAnzahl zaehlt die geschlossenen Issues ohne PRs: die Issue-Liste
// fuehrt PRs mit, also geschlossene Eintraege minus geschlossene PRs.
func (c Client) GeschlosseneAnzahl(ctx context.Context, nwo string) (int, error) {
	if !GueltigesNWO(nwo) {
		return 0, fmt.Errorf("ungueltiges Repo %q (erwartet owner/repo)", nwo)
	}
	alle, err := c.anzahl(ctx, Basis()+"/repos/"+nwo+"/issues?state=closed&per_page=1")
	if err != nil {
		return 0, err
	}
	prs, err := c.anzahl(ctx, Basis()+"/repos/"+nwo+"/pulls?state=closed&per_page=1")
	if err != nil {
		return 0, err
	}
	if alle < prs {
		return 0, nil
	}
	return alle - prs, nil
}
