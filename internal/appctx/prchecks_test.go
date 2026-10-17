package appctx

// Zustand, Gesamtzustand, Marke, Zusammenfassung und Zwischenspeicher der
// CI-Pruefungen je PR (pr-checks, #80), ohne Browser.

import (
	"strings"
	"testing"
	"time"

	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

var pcJetzt = time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)

func pcZeit(sekundenVorher int) string {
	return pcJetzt.Add(-time.Duration(sekundenVorher) * time.Second).Format(time.RFC3339)
}

func pcNamen(c ghrest.PRChecks) string {
	var n []string
	for _, p := range c.Pruefungen {
		n = append(n, p.Name)
	}
	return strings.Join(n, ",")
}

func pcFinde(t *testing.T, c ghrest.PRChecks, name string) ghrest.Pruefung {
	t.Helper()
	for _, p := range c.Pruefungen {
		if p.Name == name {
			return p
		}
	}
	t.Fatalf("keine Pruefung %q in %+v", name, c.Pruefungen)
	return ghrest.Pruefung{}
}

func TestChecksRotGehtVorLaufend(t *testing.T) {
	runs := []ghrest.CheckRun{
		{ID: 1, Name: "test", Status: "completed", Conclusion: "success", StartedAt: pcZeit(600), CompletedAt: pcZeit(540), HTMLURL: "https://gh/test"},
		{ID: 2, Name: "build", Status: "completed", Conclusion: "failure", StartedAt: pcZeit(600), CompletedAt: pcZeit(528),
			HTMLURL: "https://gh/build", App: "github-actions", DetailsURL: "https://github.com/acme/alpha/actions/runs/555/job/2"},
		{ID: 3, Name: "lint", Status: "in_progress", StartedAt: pcZeit(30), DetailsURL: "https://gh/lint-details"},
	}
	st := []ghrest.CommitStatus{{Kontext: "ci/legacy", Zustand: "pending", URL: "https://ci.example/7"}}
	c := ChecksRechnen("sha9", runs, st, pcJetzt)

	if c.SHA != "sha9" || c.Gesamt != "rot" || c.Marke != "✗" || c.Zahl != 1 {
		t.Errorf("sha %q gesamt %q marke %q zahl %d, erwartet sha9 rot ✗ 1", c.SHA, c.Gesamt, c.Marke, c.Zahl)
	}
	z := c.Zusammenfassung
	if z.Gruen != 1 || z.Rot != 1 || z.Laeuft != 2 || z.Neutral != 0 || z.Gesamt != 4 {
		t.Errorf("Zusammenfassung %+v", z)
	}
	if z.Text != "1 rot · 2 läuft · 1 grün" {
		t.Errorf("Text %q", z.Text)
	}
	if got := pcNamen(c); got != "build,ci/legacy,lint,test" {
		t.Errorf("Reihenfolge %q", got)
	}
	b := pcFinde(t, c, "build")
	if b.Art != "check" || b.Zustand != "rot" || b.Ergebnis != "failure" || b.DauerS != 72 || b.Dauer != "1 min 12 s" ||
		b.Link != "https://gh/build" || b.CheckRunID != 2 || !b.NeuStartbar {
		t.Errorf("build %+v", b)
	}
	l := pcFinde(t, c, "lint")
	if l.Zustand != "laeuft" || l.Ergebnis != "in_progress" || l.DauerS != 30 || l.Dauer != "30 s" || l.Link != "https://gh/lint-details" || l.NeuStartbar {
		t.Errorf("lint %+v", l)
	}
	s := pcFinde(t, c, "ci/legacy")
	if s.Art != "status" || s.Zustand != "laeuft" || s.Ergebnis != "pending" || s.Link != "https://ci.example/7" || s.CheckRunID != 0 || s.NeuStartbar {
		t.Errorf("ci/legacy %+v", s)
	}
	if g := pcFinde(t, c, "test"); g.Zustand != "gruen" || g.Dauer != "1 min" || g.NeuStartbar {
		t.Errorf("test %+v", g)
	}
}

func TestChecksZustaendeJeErgebnis(t *testing.T) {
	for ergebnis, zustand := range map[string]string{
		"success": "gruen", "failure": "rot", "timed_out": "rot", "cancelled": "rot", "action_required": "rot",
		"startup_failure": "rot", "neutral": "neutral", "skipped": "neutral", "stale": "neutral",
	} {
		c := ChecksRechnen("s", []ghrest.CheckRun{{ID: 7, Name: "x", Status: "completed", Conclusion: ergebnis}}, nil, pcJetzt)
		if p := c.Pruefungen[0]; p.Zustand != zustand || p.NeuStartbar != (zustand == "rot") {
			t.Errorf("%s: %+v, erwartet %s", ergebnis, p, zustand)
		}
	}
	for _, status := range []string{"queued", "in_progress", "waiting", "pending", "requested"} {
		c := ChecksRechnen("s", []ghrest.CheckRun{{ID: 7, Name: "x", Status: status}}, nil, pcJetzt)
		if p := c.Pruefungen[0]; p.Zustand != "laeuft" {
			t.Errorf("Status %s: %+v, erwartet laeuft", status, p)
		}
	}
	for state, zustand := range map[string]string{"success": "gruen", "failure": "rot", "error": "rot", "pending": "laeuft"} {
		c := ChecksRechnen("s", nil, []ghrest.CommitStatus{{Kontext: "k", Zustand: state}}, pcJetzt)
		if p := c.Pruefungen[0]; p.Zustand != zustand || p.NeuStartbar {
			t.Errorf("Status %s: %+v, erwartet %s", state, p, zustand)
		}
	}
}

func TestChecksNeutralUndUebersprungen(t *testing.T) {
	runs := []ghrest.CheckRun{
		{ID: 1, Name: "test", Status: "completed", Conclusion: "success"},
		{ID: 2, Name: "deploy", Status: "completed", Conclusion: "skipped"},
		{ID: 3, Name: "docs", Status: "completed", Conclusion: "neutral"},
	}
	c := ChecksRechnen("s", runs, nil, pcJetzt)
	if c.Gesamt != "gruen" || c.Marke != "✓" || c.Zahl != 1 {
		t.Errorf("gesamt %q marke %q zahl %d", c.Gesamt, c.Marke, c.Zahl)
	}
	if z := c.Zusammenfassung; z.Gruen != 1 || z.Neutral != 2 || z.Gesamt != 3 || z.Text != "1 grün · 2 neutral" {
		t.Errorf("Zusammenfassung %+v", z)
	}
	if got := pcNamen(c); got != "test,deploy,docs" {
		t.Errorf("Reihenfolge %q", got)
	}
	// Nur neutrale Pruefungen: kein Fehler, also gruen.
	c = ChecksRechnen("s", runs[1:], nil, pcJetzt)
	if c.Gesamt != "gruen" || c.Marke != "✓" {
		t.Errorf("nur neutral: gesamt %q marke %q", c.Gesamt, c.Marke)
	}
}

func TestChecksKeineUndNurLaufende(t *testing.T) {
	c := ChecksRechnen("s", nil, nil, pcJetzt)
	if c.Gesamt != "keine" || c.Marke != "" || c.Zahl != 0 || c.Pruefungen == nil || len(c.Pruefungen) != 0 {
		t.Errorf("keine: %+v", c)
	}
	if c.Zusammenfassung.Text != "keine Prüfungen" {
		t.Errorf("keine: Text %q", c.Zusammenfassung.Text)
	}
	c = ChecksRechnen("s", []ghrest.CheckRun{{ID: 1, Name: "build", Status: "queued"}},
		[]ghrest.CommitStatus{{Kontext: "ci/legacy", Zustand: "pending"}}, pcJetzt)
	if c.Gesamt != "laeuft" || c.Marke != "◌" || c.Zahl != 2 {
		t.Errorf("laufend: gesamt %q marke %q zahl %d", c.Gesamt, c.Marke, c.Zahl)
	}
	if p := pcFinde(t, c, "build"); p.DauerS != 0 || p.Dauer != "" {
		t.Errorf("ohne Start keine Dauer: %+v", p)
	}
}

func TestChecksDauerText(t *testing.T) {
	for s, text := range map[int]string{0: "", 5: "5 s", 59: "59 s", 60: "1 min", 72: "1 min 12 s", 3600: "1 h", 3900: "1 h 5 min"} {
		if got := DauerText(int64(s)); got != text {
			t.Errorf("DauerText(%d) = %q, erwartet %q", s, got, text)
		}
	}
}

func TestChecksSpeicherNurAbgeschlossene(t *testing.T) {
	sp := NeuerChecksSpeicher(10 * time.Minute)
	fertig := ghrest.CheckRun{ID: 1, Name: "test", Status: "completed", Conclusion: "success"}
	kaputt := ghrest.CheckRun{ID: 2, Name: "build", Status: "completed", Conclusion: "failure"}
	offen := ghrest.CheckRun{ID: 3, Name: "lint", Status: "in_progress"}
	for _, c := range []ghrest.PRChecks{
		ChecksRechnen("a", []ghrest.CheckRun{fertig}, nil, pcJetzt),                                 // gruen
		ChecksRechnen("b", []ghrest.CheckRun{fertig, kaputt}, nil, pcJetzt),                         // rot, abgeschlossen
		ChecksRechnen("c", []ghrest.CheckRun{offen}, nil, pcJetzt),                                  // laeuft
		ChecksRechnen("d", nil, nil, pcJetzt),                                                       // keine
		ChecksRechnen("e", []ghrest.CheckRun{kaputt, offen}, nil, pcJetzt),                          // rot, aber eine laeuft noch
		ChecksRechnen("f", nil, []ghrest.CommitStatus{{Kontext: "k", Zustand: "pending"}}, pcJetzt), // laeuft
	} {
		sp.Merken("acme/alpha", c, pcJetzt)
	}
	for sha, erwartet := range map[string]bool{"a": true, "b": true, "c": false, "d": false, "e": false, "f": false} {
		if _, ok := sp.Lesen("acme/alpha", sha, pcJetzt.Add(time.Minute)); ok != erwartet {
			t.Errorf("SHA %s gespeichert %v, erwartet %v", sha, ok, erwartet)
		}
	}
	if _, ok := sp.Lesen("acme/beta", "a", pcJetzt); ok {
		t.Error("anderes Repo liest denselben SHA")
	}
	if c, ok := sp.Lesen("ACME/Alpha", "a", pcJetzt); !ok || c.Gesamt != "gruen" {
		t.Errorf("Repo ohne Gross/Klein: %+v %v", c, ok)
	}
	if _, ok := sp.Lesen("acme/alpha", "a", pcJetzt.Add(11*time.Minute)); ok {
		t.Error("nach der Frist noch gespeichert")
	}
	sp.Verwerfen("acme/alpha", "b")
	if _, ok := sp.Lesen("acme/alpha", "b", pcJetzt); ok {
		t.Error("nach Verwerfen noch gespeichert")
	}
}
