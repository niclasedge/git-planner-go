package appctx

// Leiste der Repos in Kategorien (repo-leiste, #25).

import (
	"strings"
	"testing"
	"time"

	"github.com/niclasedge/git-planner-go/internal/areas"
	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

func repo(nwo string) ghrest.Repo {
	teile := strings.SplitN(nwo, "/", 2)
	return ghrest.Repo{NWO: nwo, Owner: teile[0], Name: teile[1]}
}

func TestRepoLeisteGruppiertUndKlapptEin(t *testing.T) {
	st := areas.State{
		Areas:       []string{"Arbeit", "Privat"},
		Zuordnung:   map[string]string{"acme/beta": "Arbeit", "acme/alpha": "Arbeit"},
		Eingeklappt: []string{"Arbeit"},
	}
	konten := []RepoKonto{
		{PAT: "GITHUB_PAT_WORK", Repos: []ghrest.Repo{repo("acme/beta"), repo("acme/alpha")}},
		{PAT: "GITHUB_PAT_PERSONAL", Repos: []ghrest.Repo{repo("ACME/Alpha"), repo("ich/gamma")}},
	}
	l := RepoLeisteBauen(konten, nil, st, time.Unix(0, 0))

	var namen []string
	for _, k := range l.Kategorien {
		namen = append(namen, k.Name)
	}
	if strings.Join(namen, "|") != "Arbeit|Privat|"+areas.OhneBereich {
		t.Fatalf("Kategorien %v", namen)
	}
	arbeit, privat, ohne := l.Kategorien[0], l.Kategorien[1], l.Kategorien[2]
	if !arbeit.Eingeklappt || privat.Eingeklappt || ohne.Eingeklappt || !ohne.Ohne {
		t.Errorf("Flags arbeit=%v privat=%v ohne=%v/%v", arbeit.Eingeklappt, privat.Eingeklappt, ohne.Eingeklappt, ohne.Ohne)
	}
	if len(arbeit.Repos) != 2 || arbeit.Repos[0].NWO != "acme/alpha" || arbeit.Repos[1].NWO != "acme/beta" {
		t.Errorf("Arbeit %+v", arbeit.Repos)
	}
	if arbeit.Repos[0].PAT != "GITHUB_PAT_WORK" {
		t.Errorf("acme/alpha PAT %q, erwartet den ersten PAT", arbeit.Repos[0].PAT)
	}
	if len(privat.Repos) != 0 || privat.Repos == nil {
		t.Errorf("Privat %v, erwartet leere Liste (nicht null)", privat.Repos)
	}
	if len(ohne.Repos) != 1 || ohne.Repos[0].NWO != "ich/gamma" {
		t.Errorf("Ohne Bereich %+v", ohne.Repos)
	}
	if len(l.Konten) != 2 || l.Konten[0].Anzahl != 2 || l.Konten[1].Anzahl != 2 {
		t.Errorf("Konten %+v", l.Konten)
	}
}

func TestRepoLeisteNimmtLokaleRepos(t *testing.T) {
	konten := []RepoKonto{{PAT: "GITHUB_PAT_WORK", Repos: []ghrest.Repo{repo("acme/alpha")}},
		{PAT: "GITHUB_PAT_PERSONAL", Grund: "GitHub 401"}}
	lokal := []LokalesRepo{
		{NWO: "ACME/alpha", Name: "alpha", Pfad: "/w/alpha"},
		{NWO: "acme/nur-lokal", Name: "nur-lokal", Pfad: "/w/nur-lokal"},
	}
	l := RepoLeisteBauen(konten, lokal, areas.State{}, time.Unix(0, 0))
	a, ok := l.Repo("acme/ALPHA")
	if !ok || a.Lokal == nil || a.Lokal.Pfad != "/w/alpha" || a.PAT != "GITHUB_PAT_WORK" {
		t.Errorf("acme/alpha %+v", a)
	}
	n, ok := l.Repo("acme/nur-lokal")
	if !ok || n.Lokal == nil || n.Name != "nur-lokal" || n.Owner != "acme" || n.PAT != "" {
		t.Errorf("nur-lokal %+v ok=%v", n, ok)
	}
	// Seit sync-status (#38) kurz, der Rohtext steht in Detail.
	if l.Konten[1].Grund != "Token ungültig (401)" || l.Konten[1].Detail != "GitHub 401" {
		t.Errorf("Grund %q Detail %q", l.Konten[1].Grund, l.Konten[1].Detail)
	}
	if _, ok := l.Repo("nicht/da"); ok {
		t.Error("nicht/da gefunden")
	}
}
