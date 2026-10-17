package ghrest

// Die Dateien eines Vergleichs (dateien-abgleich, #53): was KOPF gegenueber
// dem gemeinsamen Vorfahren mit BASIS angefasst hat (compare/BASIS...KOPF,
// drei Punkte). Was auf der Basis seit dem Abzweig geschah, zaehlt nicht.
// GitHub liefert hoechstens 300 Dateien je Vergleich; ein Change, der mehr
// anfasst, ist fuer den Abgleich ohnehin kein Parallelkandidat.

import (
	"context"
	"sort"
)

// VergleichDateien liefert die angefassten Dateien sortiert und ohne
// Dopplung. Abgefragt wird ueber vergleichMitDateien (konflikte-anzeigen,
// #62), damit es nur eine Leseregel fuer die Compare-Antwort gibt.
func (c Client) VergleichDateien(ctx context.Context, nwo, basis, kopf string) ([]string, error) {
	v, err := c.vergleichMitDateien(ctx, nwo, basis, kopf)
	if err != nil {
		return nil, err
	}
	gesehen := map[string]bool{}
	out := []string{}
	for _, f := range v.Dateien {
		if f != "" && !gesehen[f] {
			gesehen[f] = true
			out = append(out, f)
		}
	}
	sort.Strings(out)
	return out, nil
}
