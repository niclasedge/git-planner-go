package ghrest

// Die Labels eines Repos (karte-gruppen, #121): aus ihnen entscheidet die
// Arbeitsliste, ob das Repo Rollen-Labels nutzt. Der bedingte Abruf (ETag)
// speichert die Antwort wie bei den uebrigen Listen.

import (
	"context"
	"fmt"
)

// RepoLabels liefert die angelegten Labels eines Repos; ein Repo ohne
// Labels ergibt eine leere Liste, nicht nil.
func (c Client) RepoLabels(ctx context.Context, nwo string) ([]Label, error) {
	if !GueltigesNWO(nwo) {
		return nil, fmt.Errorf("ungueltiges Repo %q (erwartet owner/repo)", nwo)
	}
	type rohLabel struct {
		Name  string `json:"name"`
		Color string `json:"color"`
	}
	roh, err := seiten[rohLabel](ctx, c, Basis()+"/repos/"+nwo+"/labels?per_page=100")
	if err != nil {
		return nil, err
	}
	out := make([]Label, 0, len(roh))
	for _, l := range roh {
		out = append(out, Label{Name: l.Name, Farbe: l.Color})
	}
	return out, nil
}
