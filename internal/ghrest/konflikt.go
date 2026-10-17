package ghrest

// Konfliktzustand je PR und "Base in den Branch holen" (konflikte-anzeigen,
// #62). Der Zustand wird hier gerechnet, nicht im Browser: aus mergeable,
// mergeable_state und dem Abstand zur Base (Compare-API). Die Konfliktdateien
// sind die Dateien, die beide Seiten seit der Merge-Basis geaendert haben.
// Alles per REST.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

// Die vier Zustaende eines offenen PRs.
const (
	PRSauber    = "sauber"
	PRVeraltet  = "veraltet"
	PRKonflikt  = "konflikt"
	PRUnbekannt = "unbekannt"
)

// PRZustand rechnet den Zustand aus GitHubs mergeable und mergeable_state
// und aus zurueck (Commits, die der Head hinter der Base liegt).
func PRZustand(mergebar *bool, status string, zurueck int) string {
	switch {
	case status == "dirty" || (mergebar != nil && !*mergebar):
		return PRKonflikt
	case status == "behind":
		return PRVeraltet
	case mergebar == nil && (status == "" || status == "unknown"):
		return PRUnbekannt
	case zurueck > 0:
		return PRVeraltet
	}
	return PRSauber
}

// KonfliktDateien ist die sortierte Schnittmenge ohne Doppelte.
func KonfliktDateien(kopfSeite, basisSeite []string) []string {
	basis := map[string]bool{}
	for _, d := range basisSeite {
		basis[d] = true
	}
	gesehen := map[string]bool{}
	out := []string{}
	for _, d := range kopfSeite {
		if basis[d] && !gesehen[d] {
			gesehen[d] = true
			out = append(out, d)
		}
	}
	sort.Strings(out)
	return out
}

// vergleich ist das, was compare/BASIS...KOPF fuer den Zustand liefert.
type vergleich struct {
	Voraus     int
	Zurueck    int
	MergeBasis string
	Dateien    []string
	// BaseSHA ist der Commit, auf dem die Basis des Vergleichs steht
	// (base_commit), Details die Dateien mit Status und Blob (#75).
	BaseSHA string
	Details []GeaenderteDatei
	// Commits sind die Commits des Vergleichs, aeltester zuerst
	// (aktionen-zeitleiste, #128).
	Commits []CommitKopf
}

// PushVergleich ist, was die Zeitleiste aus compare/VORHER...NACHHER eines
// Pushes braucht: seine Commits (aeltester zuerst, wie GitHub sie liefert)
// und die angefassten Dateien mit Status und Blob (aktionen-zeitleiste, #128).
type PushVergleich struct {
	Commits []CommitKopf      `json:"commits"`
	Dateien []GeaenderteDatei `json:"dateien"`
}

// VergleichMitCommits liefert Commits und Dateien von compare/VORHER...NACHHER.
// Beide SHAs aendern sich nie; der Aufrufer speichert das Ergebnis.
func (c Client) VergleichMitCommits(ctx context.Context, nwo, vorher, nachher string) (PushVergleich, error) {
	v, err := c.vergleichMitDateien(ctx, nwo, vorher, nachher)
	if err != nil {
		return PushVergleich{}, err
	}
	return PushVergleich{Commits: v.Commits, Dateien: v.Details}, nil
}

func (c Client) vergleichMitDateien(ctx context.Context, nwo, basis, kopf string) (vergleich, error) {
	if !GueltigesNWO(nwo) || strings.TrimSpace(basis) == "" || strings.TrimSpace(kopf) == "" {
		return vergleich{}, fmt.Errorf("ungueltiger Vergleich %q...%q in %q", basis, kopf, nwo)
	}
	b, _, err := c.holen(ctx, Basis()+"/repos/"+nwo+"/compare/"+url.PathEscape(basis)+"..."+url.PathEscape(kopf))
	if err != nil {
		return vergleich{}, err
	}
	var roh struct {
		Voraus     int `json:"ahead_by"`
		Zurueck    int `json:"behind_by"`
		MergeBasis struct {
			SHA string `json:"sha"`
		} `json:"merge_base_commit"`
		BaseCommit struct {
			SHA string `json:"sha"`
		} `json:"base_commit"`
		Files []struct {
			Filename string `json:"filename"`
			Status   string `json:"status"`
			SHA      string `json:"sha"`
			Vorher   string `json:"previous_filename"`
		} `json:"files"`
		Commits []rohCommitAutor `json:"commits"`
	}
	if err := json.Unmarshal(b, &roh); err != nil {
		return vergleich{}, fmt.Errorf("GitHub: unlesbarer Vergleich: %v", err)
	}
	v := vergleich{Voraus: roh.Voraus, Zurueck: roh.Zurueck, MergeBasis: roh.MergeBasis.SHA, BaseSHA: roh.BaseCommit.SHA}
	for _, f := range roh.Files {
		v.Dateien = append(v.Dateien, f.Filename)
		v.Details = append(v.Details, GeaenderteDatei{Pfad: f.Filename, Status: f.Status, SHA: f.SHA, Vorher: f.Vorher})
	}
	for _, k := range roh.Commits {
		v.Commits = append(v.Commits, k.kopf(""))
	}
	return v, nil
}

// PRZustandHolen fuellt Abgleich, Voraus, Zurueck und bei Konflikt die
// KonfliktDateien. p muss Mergebar, MergeStatus, Base und HeadSHA tragen.
// Scheitert der Vergleich, bleibt der Zustand aus den GitHub-Feldern stehen.
func (c Client) PRZustandHolen(ctx context.Context, nwo string, p *PR) error {
	p.Abgleich = PRZustand(p.Mergebar, p.MergeStatus, 0)
	kopf := p.HeadSHA
	if kopf == "" {
		kopf = p.Head
	}
	v, err := c.vergleichMitDateien(ctx, nwo, p.Base, kopf)
	if err != nil {
		return err
	}
	p.Voraus, p.Zurueck = v.Voraus, v.Zurueck
	p.Abgleich = PRZustand(p.Mergebar, p.MergeStatus, v.Zurueck)
	if p.Abgleich != PRKonflikt || v.MergeBasis == "" {
		return nil
	}
	gegen, err := c.vergleichMitDateien(ctx, nwo, v.MergeBasis, p.Base)
	if err != nil {
		return err
	}
	p.KonfliktDateien = KonfliktDateien(v.Dateien, gegen.Dateien)
	return nil
}

// PRBaseHolen holt die Base in den Head-Branch (update-branch), aber nur,
// wenn der Head noch sha ist.
func (c Client) PRBaseHolen(ctx context.Context, nwo string, nummer int, sha string) error {
	if !GueltigesNWO(nwo) || nummer <= 0 {
		return fmt.Errorf("ungueltiger PR %s#%d", nwo, nummer)
	}
	rumpf := map[string]any{}
	if sha != "" {
		rumpf["expected_head_sha"] = sha
	}
	_, _, err := c.anfrage(ctx, http.MethodPut, fmt.Sprintf("%s/repos/%s/pulls/%d/update-branch", Basis(), nwo, nummer), rumpf)
	return err
}
