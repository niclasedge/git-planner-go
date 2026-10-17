package appctx

// Die Leiste der Things-Oberflaeche: Repos aus allen PATs und den lokalen
// Clones, gruppiert nach den Areas aus areas.json (repo-leiste, #25). Hier
// wird zusammengefuehrt, gruppiert und sortiert — die Oberflaeche zeichnet nur.

import (
	"strings"
	"time"

	"github.com/niclasedge/git-planner-go/internal/areas"
	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

// RepoKonto ist das Ergebnis eines PATs: seine Repos oder der Grund, warum es
// keine gibt.
type RepoKonto struct {
	PAT   string
	Repos []ghrest.Repo
	Grund string
}

// LokalesRepo ist ein lokaler Clone mit GitHub-origin.
type LokalesRepo struct {
	NWO  string `json:"nwo"`
	Name string `json:"name"`
	Pfad string `json:"pfad"`
}

// LeistenRepo ist eine Zeile der Leiste.
type LeistenRepo struct {
	ghrest.Repo
	// PAT ist der Name der Variablen, deren Token das Repo sieht. Leer bei
	// einem Repo, das nur als lokaler Clone bekannt ist.
	PAT   string       `json:"pat"`
	Lokal *LokalesRepo `json:"lokal,omitempty"`
	// CI ist der Punkt des juengsten gespeicherten Workflow-Runs (ci-runs,
	// #122); leer ohne Runs.
	CI string `json:"ci,omitempty"`
}

// RepoKategorie ist eine Area mit ihren Repos.
type RepoKategorie struct {
	Name        string        `json:"name"`
	Ohne        bool          `json:"ohne"`
	Eingeklappt bool          `json:"eingeklappt"`
	Repos       []LeistenRepo `json:"repos"`
}

// KontoStand meldet je PAT, was es geliefert hat.
type KontoStand struct {
	PAT    string `json:"pat"`
	Anzahl int    `json:"anzahl"`
	Grund  string `json:"grund,omitempty"`
	// Detail ist die volle Meldung hinter Grund (sync-status, #38).
	Detail string `json:"detail,omitempty"`
}

// RepoLeiste ist die Antwort von GET /api/repos.
type RepoLeiste struct {
	Kategorien []RepoKategorie `json:"kategorien"`
	Konten     []KontoStand    `json:"konten"`
	Stand      string          `json:"stand"`
	Sync       *SyncStand      `json:"sync,omitempty"`
}

// RepoLeisteBauen fuehrt die Konten und die lokalen Clones zusammen und
// gruppiert sie. Ein Repo erscheint einmal (owner/repo ohne
// Gross-/Kleinschreibung); das erste Konto, das es nennt, gewinnt. Die
// Gruppierung ist dieselbe wie in der Bereichsansicht (BereicheGruppieren):
// Areas in Dateiordnung, auch leere, "Ohne Bereich" zuletzt und nur mit
// Inhalt, darin nach owner/repo sortiert.
func RepoLeisteBauen(konten []RepoKonto, lokal []LokalesRepo, st areas.State, stand time.Time) RepoLeiste {
	nachSchluessel := map[string]*LeistenRepo{}
	var folge []string
	out := RepoLeiste{Kategorien: []RepoKategorie{}, Konten: []KontoStand{}, Stand: stand.UTC().Format(time.RFC3339)}

	for _, k := range konten {
		ks := KontoStand{PAT: k.PAT, Anzahl: len(k.Repos), Grund: KontoGrundKurz(k.Grund)}
		if ks.Grund != k.Grund {
			ks.Detail = k.Grund
		}
		out.Konten = append(out.Konten, ks)
		for _, r := range k.Repos {
			s := strings.ToLower(r.NWO)
			if s == "" || nachSchluessel[s] != nil {
				continue
			}
			nachSchluessel[s] = &LeistenRepo{Repo: r, PAT: k.PAT}
			folge = append(folge, s)
		}
	}
	for _, l := range lokal {
		s := strings.ToLower(l.NWO)
		if s == "" {
			continue
		}
		kopie := l
		if r := nachSchluessel[s]; r != nil {
			if r.Lokal == nil {
				r.Lokal = &kopie
			}
			continue
		}
		owner, name, _ := strings.Cut(l.NWO, "/")
		nachSchluessel[s] = &LeistenRepo{Repo: ghrest.Repo{NWO: l.NWO, Owner: owner, Name: name}, Lokal: &kopie}
		folge = append(folge, s)
	}

	projekte := make([]BereichProjekt, 0, len(folge))
	for _, s := range folge {
		projekte = append(projekte, BereichProjekt{Name: nachSchluessel[s].NWO, Pfad: s})
	}
	for _, g := range BereicheGruppieren(projekte, st) {
		k := RepoKategorie{Name: g.Bereich, Ohne: g.Ohne, Eingeklappt: st.IstEingeklappt(g.Bereich), Repos: []LeistenRepo{}}
		for _, p := range g.Projekte {
			k.Repos = append(k.Repos, *nachSchluessel[p.Pfad])
		}
		out.Kategorien = append(out.Kategorien, k)
	}
	return out
}

// Repo findet ein Repo der Leiste ueber owner/repo ohne
// Gross-/Kleinschreibung.
func (l RepoLeiste) Repo(nwo string) (LeistenRepo, bool) {
	s := strings.ToLower(strings.TrimSpace(nwo))
	for _, k := range l.Kategorien {
		for _, r := range k.Repos {
			if strings.ToLower(r.NWO) == s {
				return r, true
			}
		}
	}
	return LeistenRepo{}, false
}
