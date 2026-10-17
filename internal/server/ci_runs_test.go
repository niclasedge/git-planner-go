package server

// Die CI-Runs je Repo (ci-runs, #122) gegen eine Fake-GitHub-API: /api/ci mit
// Speicher je Run und auf der Platte, Repo ohne Actions, Tageslauf mit der
// 24-h-Regel, Erneuern beim Oeffnen, Fehler behaelt den Stand, /api/ci-log mit
// Weiterleitung und Kuerzung, der Neustart fehlgeschlagener Jobs und der
// Punkt in /api/repos. Ein interner Test, damit der Tageslauf ohne Uhr- und
// Goroutinen-Raten aufrufbar ist.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/niclasedge/git-planner-go/internal/anzeigezeit"
	"github.com/niclasedge/git-planner-go/internal/appctx"
	"github.com/niclasedge/git-planner-go/internal/cache"
	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

type ciFakeGH struct {
	mu      sync.Mutex
	aufrufe []string // "METHODE PFAD"
	runs    map[string]string
	status  map[string]int
	jobs    map[int64]string
	logs    map[int64]string
	srv     *httptest.Server
}

func (f *ciFakeGH) zaehle(methode, pfad string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, a := range f.aufrufe {
		if a == methode+" "+pfad {
			n++
		}
	}
	return n
}

func (f *ciFakeGH) setzeRuns(repo, rumpf string, status int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.runs[repo] = rumpf
	f.status[repo] = status
}

func ciZeit(vor time.Duration) string {
	return time.Now().Add(-vor).UTC().Format(time.RFC3339)
}

func ciRunJSON(id, nummer, versuch int, status, ergebnis, start, ende string) string {
	schluss := "null"
	if ergebnis != "" {
		schluss = fmt.Sprintf("%q", ergebnis)
	}
	return fmt.Sprintf(`{"id":%d,"name":"CI","event":"push","head_branch":"main","head_sha":"3f9c2e1a7b8c9d0e%d",
	  "display_title":"Titel %d","head_commit":{"message":"Betreff %d\n\nRumpf"},"run_number":%d,"run_attempt":%d,
	  "status":%q,"conclusion":%s,"run_started_at":%q,"updated_at":%q,"html_url":"https://github.com/acme/alpha/actions/runs/%d"}`,
		id, id, id, id, nummer, versuch, status, schluss, start, ende, id)
}

func ciRunsJSON(runs ...string) string {
	return fmt.Sprintf(`{"total_count":%d,"workflow_runs":[%s]}`, len(runs), strings.Join(runs, ","))
}

func ciSchrittJSON(n int, name, status, ergebnis string) string {
	schluss := "null"
	if ergebnis != "" {
		schluss = fmt.Sprintf("%q", ergebnis)
	}
	return fmt.Sprintf(`{"name":%q,"status":%q,"conclusion":%s,"number":%d,"started_at":null,"completed_at":null}`, name, status, schluss, n)
}

func ciJobJSON(id, run int, name, status, ergebnis string, schritte ...string) string {
	schluss := "null"
	if ergebnis != "" {
		schluss = fmt.Sprintf("%q", ergebnis)
	}
	return fmt.Sprintf(`{"id":%d,"run_id":%d,"run_attempt":1,"name":%q,"status":%q,"conclusion":%s,
	  "started_at":null,"completed_at":null,"html_url":"https://github.com/acme/alpha/actions/runs/%d/job/%d","steps":[%s]}`,
		id, run, name, status, schluss, run, id, strings.Join(schritte, ","))
}

func ciJobsJSON(jobs ...string) string {
	return fmt.Sprintf(`{"total_count":%d,"jobs":[%s]}`, len(jobs), strings.Join(jobs, ","))
}

// neuerCIFake: acme/alpha mit Run 12 rot, 11 gruen, 10 laufend; acme/beta und
// acme/gamma ohne Antwort, bis ein Test sie setzt.
func neuerCIFake(t *testing.T) *ciFakeGH {
	t.Helper()
	f := &ciFakeGH{runs: map[string]string{}, status: map[string]int{}, jobs: map[int64]string{}, logs: map[int64]string{}}
	f.runs["alpha"] = ciRunsJSON(
		ciRunJSON(12, 12, 1, "completed", "failure", ciZeit(10*time.Minute), ciZeit(8*time.Minute)),
		ciRunJSON(11, 11, 1, "completed", "success", ciZeit(30*time.Minute), ciZeit(28*time.Minute)),
		ciRunJSON(10, 10, 1, "in_progress", "", ciZeit(50*time.Minute), ciZeit(45*time.Minute)))
	f.jobs[12] = ciJobsJSON(
		ciJobJSON(500, 12, "lint", "completed", "success", ciSchrittJSON(1, "Set up job", "completed", "success")),
		ciJobJSON(501, 12, "test", "completed", "failure",
			ciSchrittJSON(1, "Set up job", "completed", "success"),
			ciSchrittJSON(2, "Run actions/checkout@v4", "completed", "success"),
			ciSchrittJSON(3, "Set up Go", "completed", "success"),
			ciSchrittJSON(4, "Run go vet ./...", "completed", "success"),
			ciSchrittJSON(5, "Run go test ./...", "completed", "failure"),
			ciSchrittJSON(6, "Upload coverage", "completed", "skipped"),
			ciSchrittJSON(7, "Post Run actions/checkout@v4", "completed", "success"),
			ciSchrittJSON(8, "Complete job", "completed", "success")),
		ciJobJSON(502, 12, "gross", "completed", "success", ciSchrittJSON(1, "Set up job", "completed", "success")))
	f.jobs[11] = ciJobsJSON(ciJobJSON(400, 11, "deploy", "completed", "success", ciSchrittJSON(1, "Set up job", "completed", "success")))
	f.jobs[10] = ciJobsJSON(ciJobJSON(300, 10, "test", "in_progress", "",
		ciSchrittJSON(1, "Set up job", "completed", "success"), ciSchrittJSON(2, "Run go test ./...", "in_progress", "")))
	beispiel, err := os.ReadFile("../appctx/testdata/ci_job_log.txt")
	if err != nil {
		t.Fatal(err)
	}
	f.logs[501] = string(beispiel)
	f.logs[502] = strings.Repeat("0123456789abcdef\n", 200000) + "ENDE\n"
	f.logs[300] = "laeuft noch\n"
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		f.mu.Lock()
		f.aufrufe = append(f.aufrufe, r.Method+" "+r.URL.Path)
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		p := r.URL.Path
		var repo string
		var id int64
		switch {
		case p == "/user/repos":
			fmt.Fprint(w, `[{"full_name":"acme/alpha","name":"alpha","owner":{"login":"acme"},"default_branch":"main"},
			  {"full_name":"acme/beta","name":"beta","owner":{"login":"acme"},"default_branch":"main"},
			  {"full_name":"acme/gamma","name":"gamma","owner":{"login":"acme"},"default_branch":"main"}]`)
		case strings.HasSuffix(p, "/actions/runs") && r.Method == http.MethodGet:
			fmt.Sscanf(p, "/repos/acme/%s", &repo)
			repo = strings.TrimSuffix(repo, "/actions/runs")
			f.mu.Lock()
			rumpf, ok := f.runs[repo]
			st := f.status[repo]
			f.mu.Unlock()
			if st != 0 {
				w.WriteHeader(st)
				fmt.Fprint(w, `{"message":"Server Error"}`)
				return
			}
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				fmt.Fprint(w, `{"message":"Not Found"}`)
				return
			}
			fmt.Fprint(w, rumpf)
		case strings.HasPrefix(p, "/repos/acme/alpha/actions/runs/") && strings.HasSuffix(p, "/jobs"):
			fmt.Sscanf(strings.TrimPrefix(p, "/repos/acme/alpha/actions/runs/"), "%d", &id)
			if s, ok := f.jobs[id]; ok {
				fmt.Fprint(w, s)
				return
			}
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"message":"Not Found"}`)
		case strings.HasPrefix(p, "/repos/acme/alpha/actions/jobs/") && strings.HasSuffix(p, "/logs"):
			fmt.Sscanf(strings.TrimPrefix(p, "/repos/acme/alpha/actions/jobs/"), "%d", &id)
			http.Redirect(w, r, fmt.Sprintf("/blob/%d", id), http.StatusFound)
		case strings.HasPrefix(p, "/blob/"):
			fmt.Sscanf(strings.TrimPrefix(p, "/blob/"), "%d", &id)
			if s, ok := f.logs[id]; ok {
				w.Header().Set("Content-Type", "text/plain")
				fmt.Fprint(w, s)
				return
			}
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodPost && strings.HasSuffix(p, "/rerun-failed-jobs"):
			w.WriteHeader(http.StatusCreated)
			fmt.Fprint(w, `{}`)
		default:
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"message":"Not Found"}`)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

// ciUmgebung setzt die Umgebung und liefert den CI-Ordner.
func ciUmgebung(t *testing.T, f *ciFakeGH) string {
	t.Helper()
	ordner := t.TempDir()
	t.Setenv("CC_AREAS_FILE", filepath.Join(t.TempDir(), "areas.json"))
	t.Setenv("CC_KNOWLEDGE_ROOTS", t.TempDir())
	t.Setenv("CC_KNOWLEDGE_CACHE", filepath.Join(t.TempDir(), "cache.json"))
	t.Setenv("CC_KNOWLEDGE_CI_CACHE", ordner)
	t.Setenv("GKP_GITHUB_API", f.srv.URL)
	t.Setenv("GITHUB_PAT_WORK", "tok-ci-acme")
	t.Setenv("GITHUB_PAT_PERSONAL", "")
	alt := RepoLog
	RepoLog = io.Discard
	t.Cleanup(func() { RepoLog = alt })
	return ordner
}

func ciServer(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(NewHandler(AppOptions{Cwd: t.TempDir()}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func ciHole(t *testing.T, url string, ziel any) int {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if ziel != nil && resp.StatusCode == http.StatusOK {
		if err := json.Unmarshal(b, ziel); err != nil {
			t.Fatalf("%s: kein JSON: %v\n%s", url, err, b)
		}
	}
	return resp.StatusCode
}

func ciPost(t *testing.T, url, rumpf string) (int, string) {
	t.Helper()
	resp, err := http.Post(url, "application/json", strings.NewReader(rumpf))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

// ciWarte wartet, bis pred gilt (hoechstens fuenf Sekunden).
func ciWarte(t *testing.T, was string, pred func() bool) {
	t.Helper()
	for i := 0; i < 250; i++ {
		if pred() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("Zeitueberschreitung: %s", was)
}

type ciTestAntwort struct {
	Repo   string `json:"repo"`
	Punkt  string `json:"punkt"`
	Gruen  int    `json:"gruen"`
	Gesamt int    `json:"gesamt"`
	Kopf   string `json:"kopf"`
	Grund  string `json:"grund"`
	Stand  string `json:"stand"`
	Runs   []struct {
		ID          int64  `json:"id"`
		Nummer      int    `json:"nummer"`
		Punkt       string `json:"punkt"`
		SHA         string `json:"sha"`
		Betreff     string `json:"betreff"`
		Zeit        string `json:"zeit"`
		WahlJob     int64  `json:"wahl_job"`
		WahlSchritt int    `json:"wahl_schritt"`
		NeuStartbar bool   `json:"neu_startbar"`
		Punkte      []struct {
			Punkt string `json:"punkt"`
			Titel string `json:"titel"`
		} `json:"punkte"`
		Jobs []struct {
			ID int64 `json:"id"`
		} `json:"jobs"`
	} `json:"runs"`
	Sync *struct {
		Laeuft bool   `json:"laeuft"`
		Fehler string `json:"fehler"`
	} `json:"sync"`
}

func TestCIRouteGemischteRuns(t *testing.T) {
	f := neuerCIFake(t)
	ciUmgebung(t, f)
	basis := ciServer(t)
	var a ciTestAntwort
	if st := ciHole(t, basis+"/api/ci?repo=acme/alpha", &a); st != http.StatusOK {
		t.Fatalf("Status %d", st)
	}
	if a.Repo != "acme/alpha" || len(a.Runs) != 3 || a.Runs[0].ID != 12 || a.Runs[1].ID != 11 || a.Runs[2].ID != 10 {
		t.Fatalf("Antwort %+v", a)
	}
	if a.Runs[0].Punkt != "rot" || a.Runs[1].Punkt != "gruen" || a.Runs[2].Punkt != "laeuft" {
		t.Errorf("Punkte %s %s %s", a.Runs[0].Punkt, a.Runs[1].Punkt, a.Runs[2].Punkt)
	}
	if a.Punkt != "rot" || a.Gruen != 1 || a.Gesamt != 3 || a.Kopf != "1 von 3 grün" || a.Stand == "" || a.Sync == nil {
		t.Errorf("Kopf punkt=%q gruen=%d gesamt=%d kopf=%q stand=%q sync=%v", a.Punkt, a.Gruen, a.Gesamt, a.Kopf, a.Stand, a.Sync)
	}
	r := a.Runs[0]
	if r.WahlJob != 501 || r.WahlSchritt != 5 || !r.NeuStartbar || a.Runs[2].NeuStartbar || len(r.SHA) != 7 || r.Betreff != "Betreff 12" {
		t.Errorf("Run 12 %+v", r)
	}
	if want, _ := appctx.ZeitpunktText(ciZeit(10*time.Minute), anzeigezeit.Jetzt()); r.Zeit != want {
		t.Errorf("zeit %q, erwartet %q", r.Zeit, want)
	}
	gefunden := false
	for _, p := range r.Punkte {
		if p.Titel == "test › Run go test ./... · fehlgeschlagen" && p.Punkt == "rot" {
			gefunden = true
		}
	}
	if !gefunden || len(r.Punkte) != 10 {
		t.Errorf("Punktreihe %+v", r.Punkte)
	}
	if f.zaehle("GET", "/repos/acme/alpha/actions/runs") != 1 {
		t.Errorf("Runs %d-mal gefragt", f.zaehle("GET", "/repos/acme/alpha/actions/runs"))
	}
}

func TestCIRouteOhneActions(t *testing.T) {
	f := neuerCIFake(t)
	f.setzeRuns("alpha", "", 0)
	f.mu.Lock()
	delete(f.runs, "alpha") // 404
	f.mu.Unlock()
	f.setzeRuns("beta", `{"total_count":0,"workflow_runs":[]}`, 0)
	ciUmgebung(t, f)
	basis := ciServer(t)
	for _, repo := range []string{"acme/alpha", "acme/beta"} {
		var roh map[string]any
		if st := ciHole(t, basis+"/api/ci?repo="+repo, &roh); st != http.StatusOK {
			t.Fatalf("%s: Status %d", repo, st)
		}
		runs, ok := roh["runs"].([]any)
		if !ok || len(runs) != 0 || roh["grund"] != "keine Workflow-Runs" || roh["gesamt"] != float64(0) || roh["punkt"] != "" {
			t.Errorf("%s: %v", repo, roh)
		}
	}
	var s struct {
		Quellen []struct {
			Name   string `json:"name"`
			Fehler string `json:"fehler"`
			Zeit   string `json:"zeit"`
		} `json:"quellen"`
	}
	ciHole(t, basis+"/api/sync", &s)
	gesehen := false
	for _, q := range s.Quellen {
		if q.Name == "ci:acme/alpha" {
			gesehen = true
			if q.Fehler != "" || q.Zeit == "" {
				t.Errorf("Quelle %+v", q)
			}
		}
	}
	if !gesehen {
		t.Errorf("keine Quelle ci:acme/alpha in %+v", s.Quellen)
	}
}

func TestCISpeicherJeRunUndAufDerPlatte(t *testing.T) {
	f := neuerCIFake(t)
	ciUmgebung(t, f)
	basis := ciServer(t)
	for i := 0; i < 2; i++ {
		var a ciTestAntwort
		if st := ciHole(t, basis+"/api/ci?repo=acme/alpha&frisch=1", &a); st != http.StatusOK || len(a.Runs) != 3 {
			t.Fatalf("Lauf %d: Status %d, %+v", i, st, a)
		}
	}
	if n := f.zaehle("GET", "/repos/acme/alpha/actions/runs/12/jobs"); n != 1 {
		t.Errorf("Jobs von Run 12 (abgeschlossen) %d-mal gefragt, erwartet 1", n)
	}
	if n := f.zaehle("GET", "/repos/acme/alpha/actions/runs/11/jobs"); n != 1 {
		t.Errorf("Jobs von Run 11 (abgeschlossen) %d-mal gefragt, erwartet 1", n)
	}
	if n := f.zaehle("GET", "/repos/acme/alpha/actions/runs/10/jobs"); n != 2 {
		t.Errorf("Jobs von Run 10 (laeuft) %d-mal gefragt, erwartet 2", n)
	}

	// Ein neuer Handler ueber demselben Ordner: der Stand kommt aus der Datei.
	vorher := f.zaehle("GET", "/repos/acme/alpha/actions/runs")
	zweiter := ciServer(t)
	var a ciTestAntwort
	if st := ciHole(t, zweiter+"/api/ci?repo=acme/alpha", &a); st != http.StatusOK || len(a.Runs) != 3 || len(a.Runs[0].Jobs) != 3 {
		t.Fatalf("zweiter Handler: Status %d, %+v", st, a)
	}
	if a.Sync == nil || !a.Sync.Laeuft {
		t.Errorf("zweiter Handler: sync %+v, erwartet laeuft (Stand aus der Datei, Erneuerung im Hintergrund)", a.Sync)
	}
	ciWarte(t, "Erneuerung im Hintergrund", func() bool {
		return f.zaehle("GET", "/repos/acme/alpha/actions/runs") > vorher && !ciSyncLaeuft(t, zweiter)
	})
	time.Sleep(100 * time.Millisecond)
	if n := f.zaehle("GET", "/repos/acme/alpha/actions/runs/12/jobs"); n != 1 {
		t.Errorf("nach dem Neustart Jobs von Run 12 %d-mal gefragt, erwartet weiter 1", n)
	}
}

func ciStandAblegen(t *testing.T, ordner, nwo string, alter time.Duration, runs ...ghrest.WorkflowRun) {
	t.Helper()
	st := &cache.CIStand{Zeit: time.Now().Add(-alter), Runs: runs, Jobs: map[string]cache.CIJobs{}}
	for _, r := range runs {
		st.Jobs[cache.CIJobsSchluessel(r.ID, r.Versuch)] = cache.CIJobs{Abgeschlossen: true, Jobs: []ghrest.ActionsJob{}}
	}
	if err := cache.CIStandSchreiben(ordner, nwo, st); err != nil {
		t.Fatal(err)
	}
}

func TestCITageslaufVierundzwanzigStunden(t *testing.T) {
	f := neuerCIFake(t)
	f.setzeRuns("beta", `{"total_count":0,"workflow_runs":[]}`, 0)
	f.setzeRuns("gamma", `{"total_count":0,"workflow_runs":[]}`, 0)
	ordner := ciUmgebung(t, f)
	alt := ghrest.WorkflowRun{ID: 1, Nummer: 1, Versuch: 1, Name: "CI", Status: "completed", Ergebnis: "success"}
	ciStandAblegen(t, ordner, "acme/alpha", 25*time.Hour, alt)
	ciStandAblegen(t, ordner, "acme/beta", time.Hour, alt)

	d := neuerRepoDienst(nil)
	d.ciTagesLauf(context.Background(), time.Now())
	if n := f.zaehle("GET", "/repos/acme/alpha/actions/runs"); n != 1 {
		t.Errorf("acme/alpha (25 h alt) %d-mal gefragt, erwartet 1", n)
	}
	if n := f.zaehle("GET", "/repos/acme/gamma/actions/runs"); n != 1 {
		t.Errorf("acme/gamma (ohne Stand) %d-mal gefragt, erwartet 1", n)
	}
	if n := f.zaehle("GET", "/repos/acme/beta/actions/runs"); n != 0 {
		t.Errorf("acme/beta (1 h alt) %d-mal gefragt, erwartet 0", n)
	}
	if st := cache.CIStandLesen(ordner, "acme/alpha"); st == nil || time.Since(st.Zeit) > time.Minute || len(st.Runs) != 3 {
		t.Errorf("acme/alpha nach dem Tageslauf %+v", st)
	}
	// Ein zweiter Lauf direkt danach fragt niemanden.
	d.ciTagesLauf(context.Background(), time.Now())
	if n := f.zaehle("GET", "/repos/acme/alpha/actions/runs"); n != 1 {
		t.Errorf("zweiter Tageslauf: acme/alpha %d-mal gefragt", n)
	}
	if ciTagesFrist != 24*time.Hour {
		t.Errorf("ciTagesFrist %v", ciTagesFrist)
	}
}

func TestCIOeffnenLaedtImHintergrund(t *testing.T) {
	f := neuerCIFake(t)
	ordner := ciUmgebung(t, f)
	gespeichert := []ghrest.WorkflowRun{
		{ID: 90, Nummer: 90, Versuch: 1, Name: "Alt", Status: "completed", Ergebnis: "success", Start: ciZeit(2 * time.Hour)},
	}
	ciStandAblegen(t, ordner, "acme/alpha", time.Minute, gespeichert...)
	basis := ciServer(t)
	var a ciTestAntwort
	if st := ciHole(t, basis+"/api/ci?repo=acme/alpha", &a); st != http.StatusOK {
		t.Fatalf("Status %d", st)
	}
	if len(a.Runs) != 1 || a.Runs[0].ID != 90 || a.Sync == nil || !a.Sync.Laeuft {
		t.Fatalf("erste Antwort %+v, erwartet der gespeicherte Stand mit sync.laeuft", a)
	}
	ciWarte(t, "erste Erneuerung", func() bool {
		return f.zaehle("GET", "/repos/acme/alpha/actions/runs") >= 1 && !ciSyncLaeuft(t, basis)
	})
	if n := f.zaehle("GET", "/repos/acme/alpha/actions/runs"); n != 1 {
		t.Errorf("eine Anfrage, %d Erneuerungen", n)
	}
	// Jede weitere Anfrage liefert den neuen Stand und erneuert noch einmal.
	var b ciTestAntwort
	if st := ciHole(t, basis+"/api/ci?repo=acme/alpha", &b); st != http.StatusOK || len(b.Runs) != 3 || b.Sync == nil || !b.Sync.Laeuft {
		t.Fatalf("zweite Antwort Status %d %+v", st, b)
	}
	ciWarte(t, "zweite Erneuerung", func() bool {
		return f.zaehle("GET", "/repos/acme/alpha/actions/runs") == 2 && !ciSyncLaeuft(t, basis)
	})
}

// ciSyncLaeuft sagt, ob fuer ci:acme/alpha gerade eine Erneuerung laeuft.
func ciSyncLaeuft(t *testing.T, basis string) bool {
	t.Helper()
	var s struct {
		Quellen []struct {
			Name   string `json:"name"`
			Laeuft bool   `json:"laeuft"`
		} `json:"quellen"`
	}
	ciHole(t, basis+"/api/sync", &s)
	for _, q := range s.Quellen {
		if q.Name == "ci:acme/alpha" {
			return q.Laeuft
		}
	}
	return true
}

func TestCIFehlerBehaeltStand(t *testing.T) {
	f := neuerCIFake(t)
	ordner := ciUmgebung(t, f)
	ciStandAblegen(t, ordner, "acme/alpha", time.Minute,
		ghrest.WorkflowRun{ID: 3, Nummer: 3, Versuch: 1, Name: "CI", Status: "completed", Ergebnis: "success", Start: ciZeit(3 * time.Hour)},
		ghrest.WorkflowRun{ID: 2, Nummer: 2, Versuch: 1, Name: "CI", Status: "completed", Ergebnis: "failure", Start: ciZeit(4 * time.Hour)},
		ghrest.WorkflowRun{ID: 1, Nummer: 1, Versuch: 1, Name: "CI", Status: "completed", Ergebnis: "success", Start: ciZeit(5 * time.Hour)})
	f.setzeRuns("alpha", "", http.StatusInternalServerError)
	basis := ciServer(t)
	var a ciTestAntwort
	if st := ciHole(t, basis+"/api/ci?repo=acme/alpha&frisch=1", &a); st != http.StatusOK {
		t.Fatalf("Status %d", st)
	}
	if len(a.Runs) != 3 || a.Sync == nil || !strings.Contains(a.Sync.Fehler, "500") {
		t.Fatalf("Antwort %+v sync %+v", a.Runs, a.Sync)
	}
	var s struct {
		Quellen []struct {
			Name   string `json:"name"`
			Fehler string `json:"fehler"`
			Zeit   string `json:"zeit"`
		} `json:"quellen"`
	}
	ciHole(t, basis+"/api/sync", &s)
	for _, q := range s.Quellen {
		if q.Name == "ci:acme/alpha" {
			if !strings.Contains(q.Fehler, "500") || q.Zeit == "" {
				t.Errorf("Quelle %+v", q)
			}
			return
		}
	}
	t.Errorf("keine Quelle ci:acme/alpha in %+v", s.Quellen)
}

type ciTestLog struct {
	JobID  int64  `json:"job_id"`
	Name   string `json:"name"`
	URL    string `json:"url"`
	Zeilen []struct {
		Nr     int    `json:"nr"`
		Zeit   string `json:"zeit"`
		Text   string `json:"text"`
		Fehler bool   `json:"fehler"`
	} `json:"zeilen"`
	Schritte []struct {
		Nummer int    `json:"nummer"`
		Name   string `json:"name"`
		Zeile  int    `json:"zeile"`
	} `json:"schritte"`
	Grenzen  bool   `json:"grenzen"`
	Gekuerzt bool   `json:"gekuerzt"`
	Grund    string `json:"grund"`
}

func TestCILogRoute(t *testing.T) {
	f := neuerCIFake(t)
	ordner := ciUmgebung(t, f)
	basis := ciServer(t)
	ciHole(t, basis+"/api/ci?repo=acme/alpha", nil)
	var erste, zweite ciTestLog
	for i, l := range []*ciTestLog{&erste, &zweite} {
		if st := ciHole(t, basis+"/api/ci-log?repo=acme/alpha&job=501", l); st != http.StatusOK {
			t.Fatalf("Abruf %d: Status %d", i, st)
		}
	}
	if erste.JobID != 501 || erste.Name != "test" || erste.URL != "https://github.com/acme/alpha/actions/runs/12/job/501" || !erste.Grenzen || erste.Gekuerzt {
		t.Errorf("Kopf %+v", erste)
	}
	if len(erste.Zeilen) != 62 || len(zweite.Zeilen) != 62 || erste.Zeilen[44].Text != "go vet ./..." || zweite.Zeilen[44].Text != "go vet ./..." {
		t.Fatalf("Zeilen %d/%d", len(erste.Zeilen), len(zweite.Zeilen))
	}
	if z := erste.Zeilen[57]; !z.Fehler || z.Nr != 58 {
		t.Errorf("Fehlerzeile %+v", z)
	}
	if len(erste.Schritte) != 8 || erste.Schritte[4].Zeile != 48 || erste.Schritte[2].Zeile != 36 {
		t.Errorf("Schritte %+v", erste.Schritte)
	}
	if n := f.zaehle("GET", "/blob/501"); n != 1 {
		t.Errorf("Log von Job 501 (abgeschlossen) %d-mal geliefert, erwartet 1", n)
	}
	if n := f.zaehle("GET", "/repos/acme/alpha/actions/jobs/501/logs"); n != 1 {
		t.Errorf("Log-Adresse von Job 501 %d-mal gefragt, erwartet 1", n)
	}

	// Ein laufender Job wird nicht gespeichert.
	for i := 0; i < 2; i++ {
		var l ciTestLog
		if st := ciHole(t, basis+"/api/ci-log?repo=acme/alpha&job=300", &l); st != http.StatusOK || len(l.Zeilen) != 1 {
			t.Fatalf("Job 300: Status %d %+v", st, l)
		}
	}
	if n := f.zaehle("GET", "/blob/300"); n != 2 {
		t.Errorf("Log von Job 300 (laeuft) %d-mal geliefert, erwartet 2", n)
	}

	// Ueber 2 MB: das Ende bleibt.
	var gross ciTestLog
	if st := ciHole(t, basis+"/api/ci-log?repo=acme/alpha&job=502", &gross); st != http.StatusOK {
		t.Fatalf("Job 502: Status %d", st)
	}
	if !gross.Gekuerzt || gross.Zeilen[len(gross.Zeilen)-1].Text != "ENDE" {
		t.Errorf("gekuerzt %v, letzte Zeile %+v", gross.Gekuerzt, gross.Zeilen[len(gross.Zeilen)-1])
	}
	text, gekuerzt, ok := cache.CILogLesen(ordner, "acme/alpha", 502)
	if !ok || !gekuerzt || len(text) > 2<<20 {
		t.Errorf("gespeichert ok=%v gekuerzt=%v Laenge %d", ok, gekuerzt, len(text))
	}

	if st := ciHole(t, basis+"/api/ci-log?repo=acme/alpha&job=999", nil); st != http.StatusNotFound {
		t.Errorf("unbekannter Job: Status %d", st)
	}
	if st := ciHole(t, basis+"/api/ci-log?repo=acme/alpha&job=abc", nil); st != http.StatusBadRequest {
		t.Errorf("job=abc: Status %d", st)
	}
}

func TestCINeuStarten(t *testing.T) {
	f := neuerCIFake(t)
	ciUmgebung(t, f)
	basis := ciServer(t)
	ciHole(t, basis+"/api/ci?repo=acme/alpha", nil)
	if st, rumpf := ciPost(t, basis+"/action/ci/neu-starten", `{"repo":"acme/alpha","run_id":12}`); st != http.StatusOK || !strings.Contains(rumpf, `"ok":true`) {
		t.Fatalf("Run 12: Status %d %s", st, rumpf)
	}
	if n := f.zaehle("POST", "/repos/acme/alpha/actions/runs/12/rerun-failed-jobs"); n != 1 {
		t.Errorf("rerun-failed-jobs %d-mal, erwartet 1", n)
	}
	for _, rumpf := range []string{`{"repo":"acme/alpha","run_id":11}`, `{"repo":"acme/alpha","run_id":10}`, `{"repo":"acme/alpha","run_id":77}`, `{"repo":"acme/alpha","run_id":12,"x":1}`} {
		if st, _ := ciPost(t, basis+"/action/ci/neu-starten", rumpf); st != http.StatusBadRequest {
			t.Errorf("%s: Status %d, erwartet 400", rumpf, st)
		}
	}
	if n := f.zaehle("POST", "/repos/acme/alpha/actions/runs/11/rerun-failed-jobs") + f.zaehle("POST", "/repos/acme/alpha/actions/runs/10/rerun-failed-jobs"); n != 0 {
		t.Errorf("gruener oder laufender Run neu gestartet (%d)", n)
	}
	if st := ciHole(t, basis+"/action/ci/neu-starten", nil); st != http.StatusMethodNotAllowed {
		t.Errorf("GET: Status %d", st)
	}
}

func TestCIPunktInDerLeiste(t *testing.T) {
	f := neuerCIFake(t)
	ciUmgebung(t, f)
	basis := ciServer(t)
	ciHole(t, basis+"/api/ci?repo=acme/alpha", nil)
	var roh struct {
		Kategorien []struct {
			Repos []map[string]any `json:"repos"`
		} `json:"kategorien"`
	}
	if st := ciHole(t, basis+"/api/repos", &roh); st != http.StatusOK {
		t.Fatalf("Status %d", st)
	}
	gesehen := map[string]any{}
	for _, k := range roh.Kategorien {
		for _, r := range k.Repos {
			ci, hat := r["ci"]
			if !hat {
				ci = nil
			}
			gesehen[fmt.Sprint(r["nwo"])] = ci
		}
	}
	if gesehen["acme/alpha"] != "rot" {
		t.Errorf("acme/alpha ci %v", gesehen["acme/alpha"])
	}
	if v, ok := gesehen["acme/beta"]; !ok || v != nil {
		t.Errorf("acme/beta ci %v (%v), erwartet kein Feld", v, ok)
	}
}
