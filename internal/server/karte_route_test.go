package server_test

// GET /api/karte gegen eine Fake-GitHub-API (repo-karte, #36; als Folge seit
// repo-karte-folge, #99).

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type karteGH struct {
	mu    sync.Mutex
	pfade []string
	srv   *httptest.Server
}

func (f *karteGH) zaehle(teil string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, p := range f.pfade {
		if strings.Contains(p, teil) {
			n++
		}
	}
	return n
}

func vorTagen(n int) string { return time.Now().UTC().AddDate(0, 0, -n).Format(time.RFC3339) }

func neuesKarteGH(t *testing.T) *karteGH {
	t.Helper()
	f := &karteGH{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		f.mu.Lock()
		f.pfade = append(f.pfade, r.URL.Path)
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		repoURL := f.srv.URL + "/repos/acme/alpha"
		switch p := r.URL.Path; {
		case p == "/user/repos":
			fmt.Fprint(w, `[{"full_name":"acme/alpha","name":"alpha","owner":{"login":"acme"},"default_branch":"main"}]`)
		case p == "/repos/acme/alpha/issues" && r.URL.Query().Get("state") == "closed":
			fmt.Fprintf(w, `[{"number":6,"title":"Sechs","state":"closed","created_at":%q,"closed_at":%q},
			 {"number":3,"title":"Drei","state":"closed","created_at":%q,"closed_at":%q}]`,
				vorTagen(9), vorTagen(2), vorTagen(8), vorTagen(4))
		case p == "/repos/acme/alpha/issues":
			fmt.Fprintf(w, `[{"number":1,"title":"Eins","state":"open","created_at":%q,"sub_issues_summary":{"total":2,"completed":1}},
			 {"number":2,"title":"Zwei","state":"open","created_at":%q,"labels":[{"name":"ready-for-agent","color":"0e8a16"}]},
			 {"number":5,"title":"Fuenf","state":"open","created_at":%q,"issue_dependencies_summary":{"blocked_by":1,"total_blocked_by":1}},
			 {"number":7,"title":"Welche Quelle?","state":"open","created_at":%q,"body":"\r\n  \r\nWelche Quelle gilt?\r\nmehr dazu",
			  "labels":[{"name":"Question","color":"d876e3"}],"assignees":[{"login":"ana"},{"login":"bo"}]},
			 {"number":9,"title":"PR","state":"open","created_at":%q,"pull_request":{}}]`,
				vorTagen(10), vorTagen(6), vorTagen(6), vorTagen(3), vorTagen(1))
		case p == "/repos/acme/alpha/issues/1/sub_issues":
			fmt.Fprintf(w, `[{"number":5,"title":"Fuenf","state":"open","created_at":%q},
			 {"number":6,"title":"Sechs","state":"closed","created_at":%q,"closed_at":%q}]`, vorTagen(6), vorTagen(9), vorTagen(2))
		case p == "/repos/acme/alpha/issues/5/dependencies/blocked_by":
			fmt.Fprint(w, `[{"number":7,"state":"open","repository_url":"`+repoURL+`"}]`)
		case p == "/repos/acme/alpha/branches":
			// Der Change zwei auf main macht #2 bereit (karte-rollen, #102).
			fmt.Fprint(w, `[{"name":"main","commit":{"sha":"s-main"}}]`)
		case p == "/repos/acme/alpha/git/trees/s-main":
			fmt.Fprint(w, `{"sha":"s-main","truncated":false,"tree":[
			 {"path":"openspec/changes/zwei/proposal.md","type":"blob","sha":"b-zwei-prop","size":1},
			 {"path":"openspec/changes/zwei/tasks.md","type":"blob","sha":"b-zwei-tasks","size":1}]}`)
		case p == "/repos/acme/alpha/git/blobs/b-zwei-prop":
			fmt.Fprintf(w, `{"sha":"b-zwei-prop","encoding":"base64","content":%q}`,
				base64.StdEncoding.EncodeToString([]byte("# zwei\n\nGeht zurueck auf GitHub-Issue #2.\n")))
		case p == "/repos/acme/alpha/git/blobs/b-zwei-tasks":
			fmt.Fprintf(w, `{"sha":"b-zwei-tasks","encoding":"base64","content":%q}`,
				base64.StdEncoding.EncodeToString([]byte("- [ ] eins\n")))
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

type karteAntwort struct {
	Kritisch int `json:"kritisch"`
	Linien   []struct {
		Nummer   int    `json:"nummer"`
		Titel    string `json:"titel"`
		Erledigt int    `json:"erledigt"`
		Gesamt   int    `json:"gesamt"`
	} `json:"linien"`
	Stationen []struct {
		Nummer    int    `json:"nummer"`
		Zone      string `json:"zone"`
		Slot      int    `json:"slot"`
		Linie     int    `json:"linie"`
		Rang      int    `json:"rang"`
		WartetAuf []int  `json:"wartet_auf"`
		Gesperrt  bool   `json:"gesperrt"`
		Bereit    bool   `json:"bereit"`
		Kritisch  bool   `json:"kritisch"`
	} `json:"stationen"`
	Fragen []struct {
		Nummer   int    `json:"nummer"`
		Wortlaut string `json:"wortlaut"`
		Wer      string `json:"wer"`
		Sperrt   []int  `json:"sperrt"`
		Hebel    int    `json:"hebel"`
		Prio     int    `json:"prio"`
	} `json:"fragen"`
	Kanten []struct {
		Von  int    `json:"von"`
		Nach int    `json:"nach"`
		Art  string `json:"art"`
	} `json:"kanten"`
}

func TestApiKarte(t *testing.T) {
	f := neuesKarteGH(t)
	basis, log := schreibServer(t, &schreibGH{srv: f.srv})

	holeVon(t, basis+"/api/arbeit?repo=acme/alpha", nil)
	if n := f.zaehle("/sub_issues"); n != 0 {
		t.Errorf("/api/arbeit hat %d-mal sub_issues angefragt", n)
	}
	blockerVorher := f.zaehle("/dependencies/blocked_by")

	var k karteAntwort
	resp, err := http.Get(basis + "/api/karte?repo=acme/alpha")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Status %d: %s", resp.StatusCode, b)
	}
	if err := json.Unmarshal(b, &k); err != nil {
		t.Fatalf("kein JSON: %v\n%s", err, b)
	}
	if n := f.zaehle("/dependencies/blocked_by"); n != blockerVorher {
		t.Errorf("die Karte hat %d-mal blocked_by angefragt, erwartet keinmal (die Blocker stehen in der Reihe)", n-blockerVorher)
	}

	// Nur Parent-Issues: #1 ist Linie, keine Station; #6 genau einmal auf
	// Linie 1; #7 ist Frage, #9 als PR nirgends.
	var stationen []string
	for _, s := range k.Stationen {
		stationen = append(stationen, fmt.Sprintf("%s:%d:#%d@%d", s.Zone, s.Slot, s.Nummer, s.Linie))
	}
	// Freie Wurzeln nach entsperrt (karte-reihe-entsperrt, #104): die Frage
	// #7 gibt #5 frei und steht vor #1 und #2, #5 folgt ihr in der Reihe.
	if strings.Join(stationen, ",") != "erledigt:0:#3@0,erledigt:1:#6@1,naechste:0:#5@1,naechste:1:#2@0" {
		t.Fatalf("Stationen %v, erwartet #3 und #6 erledigt, #5 und #2 als naechste", stationen)
	}
	if len(k.Linien) != 2 || k.Linien[0].Nummer != 1 || k.Linien[0].Titel != "Eins" || k.Linien[0].Erledigt != 1 || k.Linien[0].Gesamt != 2 ||
		k.Linien[1].Nummer != 0 {
		t.Errorf("Linien %+v, erwartet 1 Eins 1/2 und 0", k.Linien)
	}
	zwei, fuenf, sechs := k.Stationen[3], k.Stationen[2], k.Stationen[1]
	if fuenf.Rang != 1 || zwei.Rang != 2 || sechs.Rang != 0 {
		t.Errorf("Rang #5 %d #2 %d #6 %d, erwartet 1, 2, 0", fuenf.Rang, zwei.Rang, sechs.Rang)
	}
	if !zwei.Bereit || zwei.Gesperrt || fmt.Sprint(fuenf.WartetAuf) != "[7]" || !fuenf.Gesperrt || !fuenf.Kritisch || fuenf.Bereit {
		t.Errorf("#2 %+v, #5 %+v; erwartet #2 bereit, #5 wartet auf 7, gesperrt, kritisch", zwei, fuenf)
	}
	if len(k.Fragen) != 1 {
		t.Fatalf("Fragen %+v, erwartet genau #7", k.Fragen)
	}
	fr := k.Fragen[0]
	if fr.Nummer != 7 || fr.Wortlaut != "Welche Quelle gilt?" || fr.Wer != "ana" || fmt.Sprint(fr.Sperrt) != "[5]" || fr.Hebel != 1 || fr.Prio != 1 {
		t.Errorf("Frage %+v, erwartet #7 mit Wortlaut, ana, sperrt [5], hebel 1, prio 1", fr)
	}
	if k.Kritisch != 7 {
		t.Errorf("kritisch %d, erwartet 7", k.Kritisch)
	}
	if len(k.Kanten) != 1 || k.Kanten[0].Von != 7 || k.Kanten[0].Nach != 5 || k.Kanten[0].Art != "frage" {
		t.Errorf("Kanten %+v, erwartet genau {7 5 frage}", k.Kanten)
	}
	if !strings.Contains(log.String(), "repo-karte:") || !strings.Contains(log.String(), "erledigt=2 jetzt=0 naechste=2 fragen=1 kanten=1") {
		t.Errorf("Log:\n%s", log)
	}
	if st := holeVon(t, basis+"/api/karte?repo=nicht/da", nil); st != http.StatusNotFound {
		t.Errorf("unbekanntes Repo: Status %d", st)
	}
}
