package server_test

// /api/uebersicht?tage= und /api/logbuch (heute-logbuch, #42).

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/niclasedge/git-planner-go/internal/anzeigezeit"
	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

func TestUebersichtFenster(t *testing.T) {
	ghrest.SpeicherLeeren()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/user/repos":
			fmt.Fprint(w, `[{"full_name":"acme/alpha","name":"alpha","owner":{"login":"acme"}}]`)
		case "/repos/acme/alpha/issues":
			var out []map[string]any
			for i, d := range []int{1, 5, 30, 31, 60} {
				out = append(out, map[string]any{"number": i + 1, "title": "t", "body": "Fällig: " + tagVon(d)})
			}
			json.NewEncoder(w).Encode(out)
		default:
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"message":"Not Found"}`)
		}
	}))
	defer srv.Close()
	basis, _ := schreibServer(t, &schreibGH{srv: srv})
	var u struct {
		Demnaechst []struct {
			Nummer int `json:"nummer"`
		} `json:"demnaechst"`
		Fenster int `json:"fenster"`
		Weitere int `json:"weitere"`
	}
	holeVon(t, basis+"/api/uebersicht", &u)
	if len(u.Demnaechst) != 3 || u.Fenster != 30 || u.Weitere != 2 {
		t.Errorf("Vorgabe: %d Eintraege, fenster %d, weitere %d; erwartet 3, 30, 2", len(u.Demnaechst), u.Fenster, u.Weitere)
	}
	u.Demnaechst = nil
	holeVon(t, basis+"/api/uebersicht?tage=60", &u)
	if len(u.Demnaechst) != 5 || u.Fenster != 60 || u.Weitere != 0 {
		t.Errorf("tage=60: %d Eintraege, fenster %d, weitere %d", len(u.Demnaechst), u.Fenster, u.Weitere)
	}
}

func TestApiLogbuch(t *testing.T) {
	ghrest.SpeicherLeeren()
	jetzt := anzeigezeit.Jetzt()
	// Zwei Zeiten von heute, beide vor jetzt, die spaetere als heute9+seit/3.
	mitternacht := time.Date(jetzt.Year(), jetzt.Month(), jetzt.Day(), 0, 0, 0, 0, anzeigezeit.Zone())
	seit := jetzt.Sub(mitternacht)
	heute9 := mitternacht.Add(seit / 3)
	spaeter := mitternacht.Add(2 * seit / 3)
	iso := func(t time.Time) string { return t.UTC().Format(time.RFC3339) }
	var mu sync.Mutex
	var since []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		zu := func(n int, wann time.Time) map[string]any {
			return map[string]any{"number": n, "title": fmt.Sprint("Issue ", n), "state": "closed", "closed_at": iso(wann), "updated_at": iso(wann)}
		}
		q := r.URL.Query()
		switch p := r.URL.Path; {
		case p == "/user/repos":
			fmt.Fprint(w, `[{"full_name":"acme/alpha","name":"alpha","owner":{"login":"acme"}},{"full_name":"acme/beta","name":"beta","owner":{"login":"acme"}},
			 {"full_name":"acme/alt","name":"alt","owner":{"login":"acme"},"archived":true}]`)
		case p == "/repos/acme/alt/issues":
			t.Errorf("archiviertes Repo abgefragt: %s", r.URL)
			fmt.Fprint(w, `[]`)
		case q.Get("state") != "closed":
			fmt.Fprint(w, `[]`)
		case p == "/repos/acme/alpha/issues":
			mu.Lock()
			since = append(since, q.Get("since"))
			mu.Unlock()
			json.NewEncoder(w).Encode([]map[string]any{zu(3, heute9), zu(2, spaeter)})
		case p == "/repos/acme/beta/issues":
			pr := zu(9, heute9)
			pr["pull_request"] = map[string]any{}
			// #8 wurde vor 20 Tagen geschlossen und nur neulich kommentiert:
			// since (updated_at) laesst es durch, closed_at nicht.
			alt := zu(8, heute9.AddDate(0, 0, -20))
			alt["updated_at"] = iso(heute9)
			json.NewEncoder(w).Encode([]map[string]any{zu(7, heute9.AddDate(0, 0, -1)), alt, pr})
		default:
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"message":"Not Found"}`)
		}
	}))
	defer srv.Close()
	basis, _ := schreibServer(t, &schreibGH{srv: srv})

	var l struct {
		Tage []struct {
			Datum     string `json:"datum"`
			Eintraege []struct {
				Repo        string `json:"repo"`
				Nummer      int    `json:"nummer"`
				Titel       string `json:"titel"`
				Geschlossen string `json:"geschlossen"`
			} `json:"eintraege"`
		} `json:"tage"`
		Fenster int `json:"fenster"`
		Anzahl  int `json:"anzahl"`
		Sync    *struct {
			Zeit string `json:"zeit"`
		} `json:"sync"`
	}
	if st := holeVon(t, basis+"/api/logbuch", &l); st != http.StatusOK {
		t.Fatalf("Status %d", st)
	}
	var tage []string
	for _, tg := range l.Tage {
		s := tg.Datum + "="
		for i, e := range tg.Eintraege {
			if i > 0 {
				s += ","
			}
			s += fmt.Sprintf("%s#%d", e.Repo, e.Nummer)
		}
		tage = append(tage, s)
	}
	heute := heute9.Format("2006-01-02")
	gestern := heute9.AddDate(0, 0, -1).Format("2006-01-02")
	want := fmt.Sprintf("[%s=acme/alpha#2,acme/alpha#3 %s=acme/beta#7]", heute, gestern)
	if fmt.Sprint(tage) != want {
		t.Errorf("Tage %v, erwartet %s", tage, want)
	}
	if l.Anzahl != 3 || l.Fenster != 14 {
		t.Errorf("anzahl %d, fenster %d", l.Anzahl, l.Fenster)
	}
	if l.Sync == nil || l.Sync.Zeit == "" {
		t.Errorf("ohne sync")
	}
	beginn := time.Date(jetzt.Year(), jetzt.Month(), jetzt.Day(), 0, 0, 0, 0, anzeigezeit.Zone()).AddDate(0, 0, -13)
	if len(since) != 1 || since[0] != beginn.UTC().Format(time.RFC3339) {
		t.Errorf("since %v, erwartet %s (Mitternacht vor 13 Tagen)", since, beginn.UTC().Format(time.RFC3339))
	}
	var l28 struct {
		Fenster int `json:"fenster"`
	}
	holeVon(t, basis+"/api/logbuch?tage=28", &l28)
	if l28.Fenster != 28 {
		t.Errorf("tage=28: fenster %d", l28.Fenster)
	}
}
