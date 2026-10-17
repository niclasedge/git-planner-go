package server_test

// Gruppen, Ziel und Rollen-Schalter ueber die Routen (karte-gruppen, #121):
// /api/arbeit fragt die Labels des Repos und kennt ohne Rollen-Labels kein
// "Rolle fehlt"; /api/karte liefert gruppen und das Ziel aus PROJECT.md des
// Standard-Branches, und ein Abruffehler steht in der Quelle projekt.

import (
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

type gruppenGH struct {
	mu      sync.Mutex
	pfade   []string
	projekt string // "da", "fehlt" oder "kaputt"
	srv     *httptest.Server
}

func (f *gruppenGH) zaehle(pfad string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, p := range f.pfade {
		if p == pfad {
			n++
		}
	}
	return n
}

func neuesGruppenGH(t *testing.T, projekt string) *gruppenGH {
	t.Helper()
	f := &gruppenGH{projekt: projekt}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		f.mu.Lock()
		f.pfade = append(f.pfade, r.URL.Path)
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch p := r.URL.Path; {
		case p == "/user/repos":
			fmt.Fprint(w, `[{"full_name":"acme/alpha","name":"alpha","owner":{"login":"acme"},"default_branch":"main"}]`)
		case p == "/repos/acme/alpha/issues" && r.URL.Query().Get("state") == "closed":
			fmt.Fprint(w, `[]`)
		case p == "/repos/acme/alpha/issues":
			fmt.Fprintf(w, `[{"number":2,"title":"Zwei","state":"open","created_at":%q},
			 {"number":3,"title":"Drei","state":"open","created_at":%q,"labels":[{"name":"schaetzung:2","color":"ededed"}]}]`,
				vorTagen(5), vorTagen(4))
		case p == "/repos/acme/alpha/labels":
			fmt.Fprint(w, `[{"name":"bug","color":"d73a4a"},{"name":"schaetzung:2","color":"ededed"}]`)
		case p == "/repos/acme/alpha/branches":
			fmt.Fprint(w, `[{"name":"main","commit":{"sha":"s-main"}}]`)
		case p == "/repos/acme/alpha/git/trees/s-main":
			fmt.Fprint(w, `{"sha":"s-main","truncated":false,"tree":[]}`)
		case p == "/repos/acme/alpha/contents/PROJECT.md" && r.URL.Query().Get("ref") == "main":
			switch f.projekt {
			case "da":
				fmt.Fprintf(w, `{"type":"file","encoding":"base64","content":%q}`, base64.StdEncoding.EncodeToString(
					[]byte("# alpha\n\nEin Dienst.\n\n## Ziel\n\nBlocker schnell klaeren:\nwas war, was kommt.\n\nNicht dieser.\n")))
			case "kaputt":
				w.WriteHeader(http.StatusInternalServerError)
				fmt.Fprint(w, `{"message":"Server Error"}`)
			default:
				w.WriteHeader(http.StatusNotFound)
				fmt.Fprint(w, `{"message":"Not Found"}`)
			}
		case p == "/repos/acme/alpha/pulls":
			fmt.Fprint(w, `[]`)
		default:
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"message":"Not Found"}`)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

type gruppenQuelle struct {
	Quelle string `json:"quelle"`
	Grund  string `json:"grund"`
}

func quelleVon(qs []gruppenQuelle, name string) (gruppenQuelle, bool) {
	for _, q := range qs {
		if q.Quelle == name {
			return q, true
		}
	}
	return gruppenQuelle{}, false
}

func TestApiArbeitOhneRollenLabels(t *testing.T) {
	f := neuesGruppenGH(t, "fehlt")
	basis, _ := schreibServer(t, &schreibGH{srv: f.srv})
	var a struct {
		Reihe []struct {
			Nummer int     `json:"nummer"`
			Status string  `json:"status"`
			Grund  *string `json:"grund"`
		} `json:"reihe"`
		Quellen []gruppenQuelle `json:"quellen"`
	}
	if st := holeVon(t, basis+"/api/arbeit?repo=acme/alpha", &a); st != http.StatusOK {
		t.Fatalf("Status %d", st)
	}
	for _, e := range a.Reihe {
		if e.Nummer == 2 && (e.Status != "ohne_change" || e.Grund != nil) {
			t.Errorf("#2 Status %q grund %v, erwartet ohne_change ohne grund", e.Status, e.Grund)
		}
	}
	if f.zaehle("/repos/acme/alpha/labels") == 0 {
		t.Errorf("labels nicht angefragt")
	}
	if q, ok := quelleVon(a.Quellen, "labels"); !ok || q.Grund != "" {
		t.Errorf("Quelle labels %+v da=%v, erwartet leeren Grund", q, ok)
	}
}

type gruppenKarte struct {
	Ziel    string          `json:"ziel"`
	Quellen []gruppenQuelle `json:"quellen"`
	Gruppen []struct {
		Nummer           int    `json:"nummer"`
		Titel            string `json:"titel"`
		AnzahlAusstehend int    `json:"anzahl_ausstehend"`
		Ausstehend       []struct {
			Nummer     int    `json:"nummer"`
			Rang       int    `json:"rang"`
			Status     string `json:"status"`
			Schaetzung int    `json:"schaetzung"`
		} `json:"ausstehend"`
	} `json:"gruppen"`
}

func TestApiKarteGruppenUndZiel(t *testing.T) {
	f := neuesGruppenGH(t, "da")
	basis, _ := schreibServer(t, &schreibGH{srv: f.srv})
	var k gruppenKarte
	if st := holeVon(t, basis+"/api/karte?repo=acme/alpha", &k); st != http.StatusOK {
		t.Fatalf("Status %d", st)
	}
	if k.Ziel != "Blocker schnell klaeren: was war, was kommt." {
		t.Errorf("ziel %q", k.Ziel)
	}
	if q, ok := quelleVon(k.Quellen, "projekt"); !ok || q.Grund != "" {
		t.Errorf("Quelle projekt %+v da=%v", q, ok)
	}
	if len(k.Gruppen) != 1 || k.Gruppen[0].Nummer != 0 || k.Gruppen[0].Titel != "ohne Parent" || k.Gruppen[0].AnzahlAusstehend != 2 {
		t.Fatalf("Gruppen %+v, erwartet nur ohne Parent mit zwei ausstehenden", k.Gruppen)
	}
	zs := k.Gruppen[0].Ausstehend
	if len(zs) != 2 || zs[0].Rang != 1 || zs[1].Rang != 2 {
		t.Fatalf("ausstehend %+v", zs)
	}
	for _, z := range zs {
		soll := map[int]int{2: 0, 3: 2}[z.Nummer]
		if z.Schaetzung != soll || z.Status != "offen" {
			t.Errorf("#%d schaetzung %d status %q, erwartet %d und offen", z.Nummer, z.Schaetzung, z.Status, soll)
		}
	}
}

func TestApiKarteZielFehltOderScheitert(t *testing.T) {
	for _, fall := range []struct{ projekt, grund string }{{"fehlt", ""}, {"kaputt", "500"}} {
		t.Run(fall.projekt, func(t *testing.T) {
			f := neuesGruppenGH(t, fall.projekt)
			basis, _ := schreibServer(t, &schreibGH{srv: f.srv})
			var k gruppenKarte
			if st := holeVon(t, basis+"/api/karte?repo=acme/alpha", &k); st != http.StatusOK {
				t.Fatalf("Status %d", st)
			}
			if k.Ziel != "" || len(k.Gruppen) != 1 {
				t.Errorf("ziel %q, %d Gruppen; erwartet leer und eine", k.Ziel, len(k.Gruppen))
			}
			q, ok := quelleVon(k.Quellen, "projekt")
			if !ok || (fall.grund == "" && q.Grund != "") || (fall.grund != "" && !strings.Contains(q.Grund, fall.grund)) {
				t.Errorf("Quelle projekt %+v da=%v, erwartet Grund %q", q, ok, fall.grund)
			}
			if f.zaehle("/repos/acme/alpha/contents/PROJECT.md") != 1 {
				t.Errorf("PROJECT.md %d-mal angefragt, erwartet einmal", f.zaehle("/repos/acme/alpha/contents/PROJECT.md"))
			}
		})
	}
}
