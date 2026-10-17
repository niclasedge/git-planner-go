package server_test

// Sync-Stand, Hintergrund-Erneuerung und /api/sync (sync-status, #38).

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

// etagGH antwortet mit ETags und zaehlt bedingte Abrufe der offenen Issues.
type etagGH struct {
	srv          *httptest.Server
	offen        atomic.Int32
	bedingt      atomic.Int32
	verzoegerung time.Duration
	repos403     bool
}

func neuesEtagGH(t *testing.T, verzoegerung time.Duration, repos403 bool) *etagGH {
	t.Helper()
	f := &etagGH{verzoegerung: verzoegerung, repos403: repos403}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		etag := `"` + r.URL.Path + `"`
		if r.URL.Path == "/user/repos" && f.repos403 {
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprint(w, `{"message":"This GitHub API path is not available: sessions are bound to their configured repositories. Use repository-scoped endpoints."}`)
			return
		}
		if r.URL.Path == "/repos/acme/alpha/issues" && r.URL.Query().Get("state") == "open" {
			f.offen.Add(1)
			if r.Header.Get("If-None-Match") != "" {
				f.bedingt.Add(1)
			}
			time.Sleep(f.verzoegerung)
		}
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", etag)
		switch p := r.URL.Path; {
		case p == "/user/repos":
			fmt.Fprint(w, `[{"full_name":"acme/alpha","name":"alpha","owner":{"login":"acme"},"default_branch":"main"}]`)
		case p == "/repos/acme/alpha/issues":
			if r.URL.Query().Get("state") == "closed" {
				fmt.Fprint(w, `[]`)
				return
			}
			fmt.Fprint(w, `[{"number":4,"title":"vier"}]`)
		case p == "/repos/acme/alpha/branches", p == "/repos/acme/alpha/pulls":
			fmt.Fprint(w, `[]`)
		default:
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"message":"Not Found"}`)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

type syncStand struct {
	Zeit         string `json:"zeit"`
	DauerMS      int64  `json:"dauer_ms"`
	Aufrufe      int64  `json:"aufrufe"`
	Unveraendert int64  `json:"unveraendert"`
	Geaendert    bool   `json:"geaendert"`
	Laeuft       bool   `json:"laeuft"`
	Fehler       string `json:"fehler"`
}

func arbeitSync(t *testing.T, basis string) syncStand {
	t.Helper()
	var a struct {
		Sync *syncStand `json:"sync"`
	}
	if st := holeVon(t, basis+"/api/arbeit?repo=acme/alpha", &a); st != http.StatusOK {
		t.Fatalf("Status %d", st)
	}
	if a.Sync == nil {
		t.Fatal("Antwort ohne sync")
	}
	return *a.Sync
}

type syncUebersicht struct {
	Quellen []struct {
		Name   string `json:"name"`
		Zeit   string `json:"zeit"`
		Laeuft bool   `json:"laeuft"`
		Fehler string `json:"fehler"`
	} `json:"quellen"`
	GitHub struct {
		Aufrufe      int64 `json:"aufrufe"`
		Unveraendert int64 `json:"unveraendert"`
	} `json:"github"`
}

func warteBisRuhig(t *testing.T, basis, quelle string) syncUebersicht {
	t.Helper()
	var u syncUebersicht
	for i := 0; i < 100; i++ {
		u = syncUebersicht{}
		holeVon(t, basis+"/api/sync", &u)
		ruhig := true
		for _, q := range u.Quellen {
			if q.Name == quelle && q.Laeuft {
				ruhig = false
			}
		}
		if ruhig {
			return u
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("Quelle %s wurde nicht fertig", quelle)
	return u
}

func TestSyncSofortUndImHintergrund(t *testing.T) {
	ghrest.SpeicherLeeren()
	t.Setenv("GKP_SYNC_TTL", "0")
	f := neuesEtagGH(t, 0, false)
	basis, _ := schreibServer(t, &schreibGH{srv: f.srv})

	erst := arbeitSync(t, basis)
	if erst.Laeuft || erst.Zeit == "" || erst.Aufrufe == 0 || !erst.Geaendert {
		t.Errorf("erste Antwort %+v, erwartet synchron geholt, geaendert, mit Zeit und Aufrufen", erst)
	}
	zweit := arbeitSync(t, basis)
	if !zweit.Laeuft {
		t.Errorf("zweite Antwort %+v, erwartet laeuft (TTL 0: sofort alter Stand, Erneuerung im Hintergrund)", zweit)
	}
	warteBisRuhig(t, basis, "arbeit:acme/alpha")
	if f.bedingt.Load() == 0 {
		t.Error("die Erneuerung hat die offenen Issues nicht bedingt (If-None-Match) abgefragt")
	}
	dritt := arbeitSync(t, basis)
	if dritt.Geaendert {
		t.Errorf("dritte Antwort %+v, erwartet geaendert=false (Inhalt gleich)", dritt)
	}
	if dritt.Unveraendert == 0 {
		t.Errorf("dritte Antwort %+v, erwartet unveraenderte (304) Abrufe", dritt)
	}
}

func TestSyncErneuertNurEinmalGleichzeitig(t *testing.T) {
	ghrest.SpeicherLeeren()
	t.Setenv("GKP_SYNC_TTL", "0")
	f := neuesEtagGH(t, 300*time.Millisecond, false)
	basis, _ := schreibServer(t, &schreibGH{srv: f.srv})
	arbeitSync(t, basis)
	vorher := f.offen.Load()

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			holeVon(t, basis+"/api/arbeit?repo=acme/alpha", nil)
		}()
	}
	wg.Wait()
	warteBisRuhig(t, basis, "arbeit:acme/alpha")
	if n := f.offen.Load() - vorher; n != 1 {
		t.Errorf("%d Erneuerungen fuer 5 gleichzeitige Anfragen, erwartet genau 1", n)
	}
}

func TestSyncFrischHoltSynchron(t *testing.T) {
	ghrest.SpeicherLeeren()
	f := neuesEtagGH(t, 0, false)
	basis, _ := schreibServer(t, &schreibGH{srv: f.srv})
	arbeitSync(t, basis)
	var a struct {
		Sync syncStand `json:"sync"`
	}
	holeVon(t, basis+"/api/arbeit?repo=acme/alpha&frisch=1", &a)
	if a.Sync.Laeuft || f.offen.Load() != 2 {
		t.Errorf("frisch: sync %+v, %d Abrufe der offenen Issues, erwartet synchron und 2", a.Sync, f.offen.Load())
	}
}

func TestSyncUebersichtUndKontoGrund(t *testing.T) {
	ghrest.SpeicherLeeren()
	f := neuesEtagGH(t, 0, true)
	basis, _ := schreibServer(t, &schreibGH{srv: f.srv})

	var l struct {
		Konten []struct {
			PAT    string `json:"pat"`
			Grund  string `json:"grund"`
			Detail string `json:"detail"`
		} `json:"konten"`
		Sync *syncStand `json:"sync"`
	}
	holeVon(t, basis+"/api/repos", &l)
	var acme *struct {
		PAT    string `json:"pat"`
		Grund  string `json:"grund"`
		Detail string `json:"detail"`
	}
	for i := range l.Konten {
		if l.Konten[i].PAT == "GITHUB_PAT_WORK" {
			acme = &l.Konten[i]
		}
	}
	if acme == nil || acme.Grund != "Repo-Liste nicht verfügbar (403)" || !strings.Contains(acme.Detail, "sessions are bound") {
		t.Errorf("Konto %+v", acme)
	}
	if l.Sync == nil || l.Sync.Zeit == "" {
		t.Errorf("/api/repos ohne sync: %+v", l.Sync)
	}

	var u syncUebersicht
	holeVon(t, basis+"/api/sync", &u)
	namen := map[string]bool{}
	for _, q := range u.Quellen {
		namen[q.Name] = true
		if q.Zeit == "" {
			t.Errorf("Quelle %s ohne Zeit", q.Name)
		}
	}
	if !namen["leiste"] {
		t.Errorf("Quellen %v, erwartet leiste", namen)
	}
	for _, q := range u.Quellen {
		if q.Name == "leiste" && q.Fehler == "" {
			t.Error("Quelle leiste ohne Fehler, obwohl kein Konto Repos lieferte")
		}
	}
	if u.GitHub.Aufrufe == 0 {
		t.Errorf("GitHub-Zaehler %+v", u.GitHub)
	}
	b, _ := json.Marshal(u)
	if strings.Contains(string(b), tokWork) {
		t.Error("Tokenwert in /api/sync")
	}
}
