package appctx_test

// Die CI-Runs eines Repos (ci-runs, #122): Punkt je Schritt, Job und Run, der
// Aufbau der Liste samt Punktreihe und Wahl, der Punkt der Leiste und die
// Aufbereitung eines Job-Logs mit den Schritt-Grenzen aus den
// ##[group]-Marken. Alles ohne Browser.

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/niclasedge/git-planner-go/internal/appctx"
	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

func TestCIPunktJeErgebnis(t *testing.T) {
	faelle := []struct {
		status, ergebnis, punkt string
	}{
		{"completed", "success", appctx.CIGruen},
		{"completed", "failure", appctx.CIRot},
		{"completed", "cancelled", appctx.CIRot},
		{"completed", "timed_out", appctx.CIRot},
		{"completed", "startup_failure", appctx.CIRot},
		{"completed", "skipped", appctx.CIGrau},
		{"completed", "neutral", appctx.CIGrau},
		{"completed", "action_required", appctx.CIGrau},
		{"completed", "stale", appctx.CIGrau},
		{"completed", "", appctx.CIGrau},
		{"in_progress", "", appctx.CILaeuft},
		{"queued", "", appctx.CILaeuft},
		{"waiting", "", appctx.CILaeuft},
		{"pending", "", appctx.CILaeuft},
		{"requested", "", appctx.CILaeuft},
	}
	for _, f := range faelle {
		if got := appctx.CIPunkt(f.status, f.ergebnis); got != f.punkt {
			t.Errorf("CIPunkt(%q, %q) = %q, erwartet %q", f.status, f.ergebnis, got, f.punkt)
		}
	}
	if appctx.CIGruen != "gruen" || appctx.CIRot != "rot" || appctx.CIGrau != "grau" || appctx.CILaeuft != "laeuft" {
		t.Errorf("Namen der Punkte %q %q %q %q", appctx.CIGruen, appctx.CIRot, appctx.CIGrau, appctx.CILaeuft)
	}
}

var ciJetzt = time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)

func ciSchritt(n int, name, status, ergebnis, start, ende string) ghrest.ActionsSchritt {
	return ghrest.ActionsSchritt{Nummer: n, Name: name, Status: status, Ergebnis: ergebnis, Start: start, Ende: ende}
}

// ciTestSchritte sind die Schritte des Jobs test aus dem Beispiel-Log.
func ciTestSchritte() []ghrest.ActionsSchritt {
	return []ghrest.ActionsSchritt{
		ciSchritt(1, "Set up job", "completed", "success", "2026-09-30T09:00:02Z", "2026-09-30T09:00:05Z"),
		ciSchritt(2, "Run actions/checkout@v4", "completed", "success", "2026-09-30T09:00:05Z", "2026-09-30T09:00:09Z"),
		ciSchritt(3, "Set up Go", "completed", "success", "2026-09-30T09:00:09Z", "2026-09-30T09:00:20Z"),
		ciSchritt(4, "Run go vet ./...", "completed", "success", "2026-09-30T09:00:20Z", "2026-09-30T09:00:41Z"),
		ciSchritt(5, "Run go test ./...", "completed", "failure", "2026-09-30T09:00:41Z", "2026-09-30T09:01:53Z"),
		ciSchritt(6, "Upload coverage", "completed", "skipped", "", ""),
		ciSchritt(7, "Post Run actions/checkout@v4", "completed", "success", "2026-09-30T09:01:53Z", "2026-09-30T09:01:54Z"),
		ciSchritt(8, "Complete job", "completed", "success", "2026-09-30T09:01:54Z", "2026-09-30T09:01:54Z"),
	}
}

func ciBeispiel() ([]ghrest.WorkflowRun, map[int64][]ghrest.ActionsJob) {
	runs := []ghrest.WorkflowRun{
		{ID: 10, Name: "CI", Event: "push", Branch: "change/neu", HeadSHA: "aaaaaaa1111111", Betreff: "Neu: laeuft noch",
			Nummer: 10, Versuch: 1, Status: "in_progress", Start: "2026-09-29T07:00:00Z", Aktualisiert: "2026-09-29T07:05:00Z",
			URL: "https://github.com/acme/alpha/actions/runs/10"},
		{ID: 12, Name: "CI", Event: "push", Branch: "main", HeadSHA: "3f9c2e1a7b8c9d0e",
			Betreff: "ci-runs: die letzten zwanzig Runs je Repo mit Schritt-Punkten, aufklappbar mit Jobs und Logs (#122)\n\nLanger Rumpf",
			Nummer:  12, Versuch: 1, Status: "completed", Ergebnis: "failure", Start: "2026-09-30T09:00:00Z", Aktualisiert: "2026-09-30T09:02:05Z",
			URL: "https://github.com/acme/alpha/actions/runs/12"},
		{ID: 11, Name: "Pages", Event: "pull_request", Branch: "change/alt", HeadSHA: "bbbbbbb2222222", Betreff: "Alt: gruen",
			Nummer: 11, Versuch: 2, Status: "completed", Ergebnis: "success", Start: "2026-09-29T08:00:00Z", Aktualisiert: "2026-09-29T08:00:45Z",
			URL: "https://github.com/acme/alpha/actions/runs/11"},
	}
	jobs := map[int64][]ghrest.ActionsJob{
		12: {
			{ID: 500, RunID: 12, Name: "lint", Status: "completed", Ergebnis: "success", Start: "2026-09-30T09:00:01Z", Ende: "2026-09-30T09:00:31Z",
				URL: "https://github.com/acme/alpha/actions/runs/12/job/500", Schritte: []ghrest.ActionsSchritt{
					ciSchritt(1, "Set up job", "completed", "success", "2026-09-30T09:00:01Z", "2026-09-30T09:00:02Z"),
					ciSchritt(2, "Run golangci-lint", "completed", "success", "2026-09-30T09:00:02Z", "2026-09-30T09:00:31Z"),
				}},
			{ID: 501, RunID: 12, Name: "test", Status: "completed", Ergebnis: "failure", Start: "2026-09-30T09:00:02Z", Ende: "2026-09-30T09:01:54Z",
				URL: "https://github.com/acme/alpha/actions/runs/12/job/501", Schritte: ciTestSchritte()},
		},
		11: {
			{ID: 400, RunID: 11, Name: "deploy", Status: "completed", Ergebnis: "success", Start: "2026-09-29T08:00:00Z", Ende: "2026-09-29T08:00:45Z",
				Schritte: []ghrest.ActionsSchritt{ciSchritt(1, "Set up job", "completed", "success", "", "")}},
		},
		10: {
			{ID: 300, RunID: 10, Name: "test", Status: "in_progress", Start: "2026-09-29T07:00:00Z", Schritte: []ghrest.ActionsSchritt{
				ciSchritt(1, "Set up job", "completed", "success", "", ""),
				ciSchritt(2, "Run go test ./...", "in_progress", "", "2026-09-29T07:01:00Z", ""),
				ciSchritt(3, "Complete job", "queued", "", "", ""),
			}},
		},
	}
	return runs, jobs
}

func TestCIAnsichtBauen(t *testing.T) {
	runs, jobs := ciBeispiel()
	a := appctx.CIAnsichtBauen("acme/alpha", runs, jobs, ciJetzt)
	if a.Repo != "acme/alpha" || a.Grund != "" {
		t.Errorf("Kopf %+v", a)
	}
	var folge []int64
	var punkte []string
	for _, r := range a.Runs {
		folge = append(folge, r.ID)
		punkte = append(punkte, r.Punkt)
	}
	if len(folge) != 3 || folge[0] != 12 || folge[1] != 11 || folge[2] != 10 {
		t.Fatalf("Folge %v, erwartet 12, 11, 10 (neueste zuerst nach Start)", folge)
	}
	if strings.Join(punkte, ",") != "rot,gruen,laeuft" {
		t.Errorf("Punkte %v", punkte)
	}
	if a.Punkt != "rot" || a.Gruen != 1 || a.Gesamt != 3 || a.Kopf != "1 von 3 grün" {
		t.Errorf("Zusammenfassung punkt=%q gruen=%d gesamt=%d kopf=%q", a.Punkt, a.Gruen, a.Gesamt, a.Kopf)
	}

	r := a.Runs[0]
	if r.Workflow != "CI" || r.Branch != "main" || r.Event != "push" || r.Nummer != 12 || r.Versuch != 1 || r.URL != "https://github.com/acme/alpha/actions/runs/12" {
		t.Errorf("Run 12 %+v", r)
	}
	if r.SHA != "3f9c2e1" {
		t.Errorf("sha %q", r.SHA)
	}
	if r.BetreffVoll != "ci-runs: die letzten zwanzig Runs je Repo mit Schritt-Punkten, aufklappbar mit Jobs und Logs (#122)" {
		t.Errorf("betreff_voll %q", r.BetreffVoll)
	}
	if n := len([]rune(r.Betreff)); n != 72 || !strings.HasSuffix(r.Betreff, "…") || !strings.HasPrefix(r.Betreff, "ci-runs: die letzten") {
		t.Errorf("betreff %q (%d Zeichen)", r.Betreff, n)
	}
	if r.Zeit != "heute 09:00" || r.ZeitVoll != "30.09.2026 09:00" {
		t.Errorf("zeit %q / %q", r.Zeit, r.ZeitVoll)
	}
	if r.DauerS != 125 || r.Dauer != "2 min 5 s" {
		t.Errorf("dauer %d %q", r.DauerS, r.Dauer)
	}
	if r.Status != "completed" || r.Ergebnis != "failure" || !r.NeuStartbar {
		t.Errorf("status %q ergebnis %q neu_startbar %v", r.Status, r.Ergebnis, r.NeuStartbar)
	}
	if r.WahlJob != 501 || r.WahlSchritt != 5 {
		t.Errorf("wahl %d/%d, erwartet 501/5", r.WahlJob, r.WahlSchritt)
	}
	if len(r.Punkte) != 10 {
		t.Fatalf("punkte %d, erwartet 10 (2 lint + 8 test)", len(r.Punkte))
	}
	if p := r.Punkte[0]; p.Punkt != "gruen" || p.JobID != 500 || p.Schritt != 1 || p.Titel != "lint › Set up job · erfolgreich" {
		t.Errorf("erster Punkt %+v", p)
	}
	if p := r.Punkte[6]; p.Punkt != "rot" || p.JobID != 501 || p.Schritt != 5 || p.Titel != "test › Run go test ./... · fehlgeschlagen" {
		t.Errorf("roter Punkt %+v", p)
	}
	if p := r.Punkte[7]; p.Punkt != "grau" || p.Titel != "test › Upload coverage · übersprungen" {
		t.Errorf("uebersprungener Punkt %+v", p)
	}
	if len(r.Jobs) != 2 || r.Jobs[1].ID != 501 || r.Jobs[1].Name != "test" || r.Jobs[1].Punkt != "rot" || r.Jobs[1].Ergebnis != "failure" ||
		r.Jobs[1].Dauer != "1 min 52 s" || r.Jobs[1].URL != "https://github.com/acme/alpha/actions/runs/12/job/501" {
		t.Fatalf("Jobs %+v", r.Jobs)
	}
	if s := r.Jobs[1].Schritte[4]; s.Nummer != 5 || s.Name != "Run go test ./..." || s.Punkt != "rot" || s.Ergebnis != "failure" || s.Dauer != "1 min 12 s" {
		t.Errorf("Schritt 5 %+v", s)
	}
	if s := r.Jobs[1].Schritte[5]; s.Punkt != "grau" || s.Dauer != "" {
		t.Errorf("Schritt 6 %+v", s)
	}

	gruen := a.Runs[1]
	if gruen.WahlJob != 400 || gruen.WahlSchritt != 0 || gruen.NeuStartbar || gruen.Zeit != "29. Sept." || gruen.Versuch != 2 {
		t.Errorf("Run 11 wahl %d/%d neu_startbar %v zeit %q versuch %d", gruen.WahlJob, gruen.WahlSchritt, gruen.NeuStartbar, gruen.Zeit, gruen.Versuch)
	}
	laeuft := a.Runs[2]
	if laeuft.NeuStartbar || laeuft.DauerS != 97200 || laeuft.Dauer != "27 h" || laeuft.Punkte[1].Punkt != "laeuft" || laeuft.Punkte[1].Titel != "test › Run go test ./... · läuft" {
		t.Errorf("Run 10 neu_startbar %v dauer %d punkte %+v", laeuft.NeuStartbar, laeuft.DauerS, laeuft.Punkte)
	}
}

func TestCIAnsichtErgebnisTexte(t *testing.T) {
	werte := map[string]string{"success": "erfolgreich", "failure": "fehlgeschlagen", "cancelled": "abgebrochen",
		"timed_out": "Zeit überschritten", "skipped": "übersprungen", "neutral": "neutral"}
	for ergebnis, text := range werte {
		runs := []ghrest.WorkflowRun{{ID: 1, Nummer: 1, Name: "CI", Status: "completed", Ergebnis: ergebnis}}
		jobs := map[int64][]ghrest.ActionsJob{1: {{ID: 2, Name: "j", Status: "completed", Ergebnis: ergebnis,
			Schritte: []ghrest.ActionsSchritt{{Nummer: 1, Name: "s", Status: "completed", Ergebnis: ergebnis}}}}}
		a := appctx.CIAnsichtBauen("acme/alpha", runs, jobs, ciJetzt)
		if got := a.Runs[0].Punkte[0].Titel; got != "j › s · "+text {
			t.Errorf("%s: titel %q", ergebnis, got)
		}
	}
}

func TestCIAnsichtOhneRuns(t *testing.T) {
	a := appctx.CIAnsichtBauen("acme/alpha", nil, nil, ciJetzt)
	if a.Runs == nil || len(a.Runs) != 0 || a.Gesamt != 0 || a.Punkt != "" || a.Grund != "keine Workflow-Runs" {
		t.Errorf("ohne Runs %+v", a)
	}
}

func TestCILeistenPunkt(t *testing.T) {
	runs, _ := ciBeispiel()
	if p := appctx.CILeistenPunkt(runs); p != "rot" {
		t.Errorf("Leiste %q, erwartet rot (juengster Run #12)", p)
	}
	if p := appctx.CILeistenPunkt(nil); p != "" {
		t.Errorf("ohne Runs %q", p)
	}
	var leiste appctx.LeistenRepo
	leiste.CI = "gruen"
	if leiste.CI != "gruen" {
		t.Error("LeistenRepo traegt kein ci")
	}
}

func ciLog(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("testdata/ci_job_log.txt")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestCILogGrenzenAusBeispiel(t *testing.T) {
	job := ghrest.ActionsJob{ID: 501, Name: "test", Status: "completed", Ergebnis: "failure",
		URL: "https://github.com/acme/alpha/actions/runs/12/job/501", Schritte: ciTestSchritte()}
	l := appctx.CILogAufbereiten(ciLog(t), job, false)
	if l.JobID != 501 || l.Name != "test" || l.URL != job.URL || l.Gekuerzt {
		t.Errorf("Kopf %+v", l)
	}
	if !l.Grenzen {
		t.Fatal("grenzen false, das Beispiel-Log traegt ##[group]")
	}
	if len(l.Zeilen) != 62 {
		t.Fatalf("%d Zeilen, erwartet 62", len(l.Zeilen))
	}
	erwartet := map[int]int{1: 1, 2: 22, 3: 36, 4: 44, 5: 48, 6: 0, 7: 59, 8: 62}
	if len(l.Schritte) != 8 {
		t.Fatalf("Schritte %+v", l.Schritte)
	}
	for _, s := range l.Schritte {
		if s.Zeile != erwartet[s.Nummer] {
			t.Errorf("Schritt %d %q beginnt in Zeile %d, erwartet %d", s.Nummer, s.Name, s.Zeile, erwartet[s.Nummer])
		}
	}
	if l.Schritte[2].Name != "Set up Go" {
		t.Errorf("Name %q", l.Schritte[2].Name)
	}
	z1 := l.Zeilen[0]
	if z1.Nr != 1 || z1.Zeit != "2026-09-30T08:14:02.1234567Z" || z1.Text != "Current runner version: '2.319.1'" {
		t.Errorf("Zeile 1 %+v (BOM und Zeitstempel ab)", z1)
	}
	if z := l.Zeilen[44]; z.Text != "go vet ./..." {
		t.Errorf("Zeile 45 %q, ANSI-Folgen muessen weg", z.Text)
	}
	if z := l.Zeilen[56]; z.Text != "FAIL" {
		t.Errorf("Zeile 57 %q", z.Text)
	}
	if z := l.Zeilen[51]; z.Text != "ok  \tgithub.com/niclasedge/git-planner-go/internal/ghrest\t0.412s" {
		t.Errorf("Zeile 52 %q, Tabs bleiben", z.Text)
	}
	fehler := 0
	for _, z := range l.Zeilen {
		if z.Fehler {
			fehler++
			if z.Nr != 58 || z.Text != "##[error]Process completed with exit code 1." {
				t.Errorf("Fehlerzeile %+v", z)
			}
		}
		if strings.Contains(z.Text, "\x1b") {
			t.Errorf("Zeile %d traegt noch ESC: %q", z.Nr, z.Text)
		}
	}
	if fehler != 1 {
		t.Errorf("%d Fehlerzeilen, erwartet 1", fehler)
	}
}

func TestCILogOhneMarken(t *testing.T) {
	job := ghrest.ActionsJob{ID: 7, Name: "alt", Schritte: []ghrest.ActionsSchritt{
		{Nummer: 1, Name: "Set up job", Status: "completed", Ergebnis: "success"},
		{Nummer: 2, Name: "Run make", Status: "completed", Ergebnis: "success"},
	}}
	l := appctx.CILogAufbereiten("zeile eins\nRun make\nzeile drei\n", job, true)
	if l.Grenzen || !l.Gekuerzt {
		t.Errorf("grenzen %v gekuerzt %v", l.Grenzen, l.Gekuerzt)
	}
	if len(l.Zeilen) != 3 || l.Zeilen[2].Text != "zeile drei" || l.Zeilen[0].Zeit != "" {
		t.Errorf("Zeilen %+v", l.Zeilen)
	}
	for _, s := range l.Schritte {
		if s.Zeile != 0 {
			t.Errorf("Schritt %d Zeile %d, erwartet 0", s.Nummer, s.Zeile)
		}
	}
}

func TestCILogTextBleibtText(t *testing.T) {
	roh := `2026-09-30T08:00:00.0000000Z <img src=x onerror=alert(1)><script>alert(2)</script> &amp; "x" 'y'` + "\r\n" +
		"2026-09-30T08:00:01.0000000Z \x1b[1;31m<b>fett</b>\x1b[0m\n"
	l := appctx.CILogAufbereiten(roh, ghrest.ActionsJob{ID: 1}, false)
	if len(l.Zeilen) != 2 {
		t.Fatalf("Zeilen %+v", l.Zeilen)
	}
	if z := l.Zeilen[0].Text; z != `<img src=x onerror=alert(1)><script>alert(2)</script> &amp; "x" 'y'` {
		t.Errorf("Zeile 1 %q, der Text muss Zeichen fuer Zeichen bleiben", z)
	}
	if z := l.Zeilen[1].Text; z != "<b>fett</b>" {
		t.Errorf("Zeile 2 %q", z)
	}
}
