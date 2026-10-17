package ghrest

// Die Repo-Aktivitaet (karte-einzeilig, #124): Pushes, Merges und
// Branch-Ereignisse ueber alle Branches, neueste zuerst. Aus ihr kommt die
// Zeile "Letzter Commit" der Arbeitsliste; ein Abruf statt je Branch einer.

import (
	"context"
	"encoding/json"
	"fmt"
)

// AktivitaetTiefe ist die Zahl der Eintraege, die der Dienst fragt; dreissig
// fuer die Zeitleiste (aktionen-zeitleiste, #128).
const AktivitaetTiefe = 30

// Aktivitaet ist ein Eintrag aus GET /repos/{owner}/{repo}/activity.
type Aktivitaet struct {
	// Ref ist der volle Ref (refs/heads/main, refs/tags/v1).
	Ref string `json:"ref"`
	// Zeit ist der Zeitpunkt des Ereignisses (timestamp, RFC 3339).
	Zeit string `json:"zeit"`
	// Vorher und Nachher sind die SHAs vor und nach dem Ereignis.
	Vorher  string `json:"vorher"`
	Nachher string `json:"nachher"`
	// Art ist activity_type: push, force_push, pr_merge,
	// merge_queue_merge, branch_creation, branch_deletion.
	Art string `json:"art"`
	// Akteur ist actor.login.
	Akteur string `json:"akteur"`
}

// Aktivitaet liefert die juengsten AktivitaetTiefe Eintraege der
// Repo-Aktivitaet.
func (c Client) Aktivitaet(ctx context.Context, nwo string) ([]Aktivitaet, error) {
	if !GueltigesNWO(nwo) {
		return nil, fmt.Errorf("ungueltiges Repo %q (erwartet owner/repo)", nwo)
	}
	b, _, err := c.holen(ctx, fmt.Sprintf("%s/repos/%s/activity?per_page=%d", Basis(), nwo, AktivitaetTiefe))
	if err != nil {
		return nil, err
	}
	var roh []struct {
		Ref       string `json:"ref"`
		Timestamp string `json:"timestamp"`
		Before    string `json:"before"`
		After     string `json:"after"`
		Art       string `json:"activity_type"`
		Actor     *struct {
			Login string `json:"login"`
		} `json:"actor"`
	}
	if err := json.Unmarshal(b, &roh); err != nil {
		return nil, fmt.Errorf("GitHub: unlesbare Aktivitaet: %v", err)
	}
	out := make([]Aktivitaet, 0, len(roh))
	for _, r := range roh {
		a := Aktivitaet{Ref: r.Ref, Zeit: r.Timestamp, Vorher: r.Before, Nachher: r.After, Art: r.Art}
		if r.Actor != nil {
			a.Akteur = r.Actor.Login
		}
		out = append(out, a)
	}
	return out, nil
}
