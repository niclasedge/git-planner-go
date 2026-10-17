package appctx

// CI-Pruefungen je PR (pr-checks, #80): aus Check-Runs und Commit-Statuses
// des Head-SHA je Pruefung Zustand, Dauer und Link, daraus Gesamtzustand,
// Zusammenfassung und Marke, sortiert rot vor laeuft vor gruen vor neutral.
// Dazu ein Zwischenspeicher je Repo und SHA fuer abgeschlossene Staende. Die
// Oberflaeche zeichnet nur, was hier gerechnet wird.

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

// Die Zustaende einer Pruefung und des Gesamtstands.
const (
	ChecksGruen   = "gruen"
	ChecksRot     = "rot"
	ChecksLaeuft  = "laeuft"
	ChecksNeutral = "neutral"
	ChecksKeine   = "keine"
)

var checksRang = map[string]int{ChecksRot: 0, ChecksLaeuft: 1, ChecksGruen: 2, ChecksNeutral: 3}

func checkRunZustand(r ghrest.CheckRun) string {
	if r.Status != "completed" {
		return ChecksLaeuft
	}
	switch r.Conclusion {
	case "success":
		return ChecksGruen
	case "failure", "timed_out", "cancelled", "action_required", "startup_failure":
		return ChecksRot
	}
	return ChecksNeutral
}

func statusZustand(s ghrest.CommitStatus) string {
	switch s.Zustand {
	case "success":
		return ChecksGruen
	case "failure", "error":
		return ChecksRot
	case "pending":
		return ChecksLaeuft
	}
	return ChecksNeutral
}

// sekunden rechnet die Dauer von start bis ende; ende leer heisst jetzt.
func sekunden(start, ende string, jetzt time.Time) int64 {
	a, err := time.Parse(time.RFC3339, start)
	if err != nil {
		return 0
	}
	b := jetzt
	if ende != "" {
		if b, err = time.Parse(time.RFC3339, ende); err != nil {
			return 0
		}
	}
	if d := int64(b.Sub(a).Seconds()); d > 0 {
		return d
	}
	return 0
}

// DauerText schreibt eine Dauer in Sekunden aus: "45 s", "1 min 12 s",
// "1 h 5 min"; 0 ergibt "".
func DauerText(s int64) string {
	switch {
	case s <= 0:
		return ""
	case s < 60:
		return fmt.Sprintf("%d s", s)
	case s < 3600:
		if s%60 == 0 {
			return fmt.Sprintf("%d min", s/60)
		}
		return fmt.Sprintf("%d min %d s", s/60, s%60)
	}
	if m := (s % 3600) / 60; m > 0 {
		return fmt.Sprintf("%d h %d min", s/3600, m)
	}
	return fmt.Sprintf("%d h", s/3600)
}

// ChecksRechnen fasst Check-Runs und Statuses eines SHA zusammen.
func ChecksRechnen(sha string, runs []ghrest.CheckRun, statuses []ghrest.CommitStatus, jetzt time.Time) ghrest.PRChecks {
	out := ghrest.PRChecks{SHA: sha, Pruefungen: []ghrest.Pruefung{}}
	for _, r := range runs {
		p := ghrest.Pruefung{Name: r.Name, Art: "check", Zustand: checkRunZustand(r), Ergebnis: r.Conclusion, CheckRunID: r.ID, Link: r.HTMLURL}
		if p.Zustand == ChecksLaeuft {
			p.Ergebnis = r.Status
			p.DauerS = sekunden(r.StartedAt, "", jetzt)
		} else {
			p.DauerS = sekunden(r.StartedAt, r.CompletedAt, jetzt)
		}
		if p.Link == "" {
			p.Link = r.DetailsURL
		}
		p.NeuStartbar = p.Zustand == ChecksRot && r.ID > 0
		out.Pruefungen = append(out.Pruefungen, p)
	}
	for _, s := range statuses {
		p := ghrest.Pruefung{Name: s.Kontext, Art: "status", Zustand: statusZustand(s), Ergebnis: s.Zustand, Link: s.URL}
		if s.Aktualisiert != s.Erstellt {
			p.DauerS = sekunden(s.Erstellt, s.Aktualisiert, jetzt)
		}
		out.Pruefungen = append(out.Pruefungen, p)
	}
	for i := range out.Pruefungen {
		out.Pruefungen[i].Dauer = DauerText(out.Pruefungen[i].DauerS)
	}
	sort.SliceStable(out.Pruefungen, func(a, b int) bool {
		pa, pb := out.Pruefungen[a], out.Pruefungen[b]
		if checksRang[pa.Zustand] != checksRang[pb.Zustand] {
			return checksRang[pa.Zustand] < checksRang[pb.Zustand]
		}
		return pa.Name < pb.Name
	})

	z := &out.Zusammenfassung
	for _, p := range out.Pruefungen {
		switch p.Zustand {
		case ChecksGruen:
			z.Gruen++
		case ChecksRot:
			z.Rot++
		case ChecksLaeuft:
			z.Laeuft++
		default:
			z.Neutral++
		}
	}
	z.Gesamt = len(out.Pruefungen)
	switch {
	case z.Gesamt == 0:
		out.Gesamt = ChecksKeine
	case z.Rot > 0:
		out.Gesamt, out.Marke, out.Zahl = ChecksRot, "✗", z.Rot
	case z.Laeuft > 0:
		out.Gesamt, out.Marke, out.Zahl = ChecksLaeuft, "◌", z.Laeuft
	default:
		out.Gesamt, out.Marke, out.Zahl = ChecksGruen, "✓", z.Gruen
	}
	var teile []string
	for _, t := range []struct {
		n    int
		name string
	}{{z.Rot, "rot"}, {z.Laeuft, "läuft"}, {z.Gruen, "grün"}, {z.Neutral, "neutral"}} {
		if t.n > 0 {
			teile = append(teile, fmt.Sprintf("%d %s", t.n, t.name))
		}
	}
	z.Text = strings.Join(teile, " · ")
	if z.Text == "" {
		z.Text = "keine Prüfungen"
	}
	if z.Gesamt > 0 && z.Laeuft == 0 {
		out.Ende = spaetestes(runs, statuses)
	}
	return out
}

// spaetestes nennt den spaetesten Abschluss der Check-Runs und Statuses
// (pr-lebenslauf, #83).
func spaetestes(runs []ghrest.CheckRun, statuses []ghrest.CommitStatus) string {
	var beste time.Time
	ende := ""
	pruefen := func(s string) {
		if t, err := time.Parse(time.RFC3339, s); err == nil && t.After(beste) {
			beste, ende = t, s
		}
	}
	for _, r := range runs {
		pruefen(r.CompletedAt)
	}
	for _, s := range statuses {
		pruefen(s.Aktualisiert)
	}
	return ende
}

// ChecksSpeicher haelt abgeschlossene Staende (mindestens eine Pruefung,
// keine laeuft) je Repo und SHA fuer eine Frist. Ein SHA aendert seinen Stand
// danach nur noch durch einen Neustart; der verwirft den Eintrag.
type ChecksSpeicher struct {
	frist time.Duration
	mu    sync.Mutex
	je    map[string]checksEintrag
}

type checksEintrag struct {
	wert ghrest.PRChecks
	zeit time.Time
}

// checksObergrenze deckelt den Speicher; darueber wird er geleert.
const checksObergrenze = 2000

// NeuerChecksSpeicher legt einen leeren Speicher mit der Frist an.
func NeuerChecksSpeicher(frist time.Duration) *ChecksSpeicher {
	return &ChecksSpeicher{frist: frist, je: map[string]checksEintrag{}}
}

func checksSchluessel(nwo, sha string) string { return strings.ToLower(nwo) + "@" + sha }

// Lesen liefert den gespeicherten Stand, solange die Frist laeuft.
func (s *ChecksSpeicher) Lesen(nwo, sha string, jetzt time.Time) (ghrest.PRChecks, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.je[checksSchluessel(nwo, sha)]
	if !ok || jetzt.Sub(e.zeit) >= s.frist {
		return ghrest.PRChecks{}, false
	}
	return e.wert, true
}

// Merken speichert c, wenn der Stand abgeschlossen ist.
func (s *ChecksSpeicher) Merken(nwo string, c ghrest.PRChecks, jetzt time.Time) {
	if c.SHA == "" || c.Zusammenfassung.Gesamt == 0 || c.Zusammenfassung.Laeuft > 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.je) >= checksObergrenze {
		s.je = map[string]checksEintrag{}
	}
	s.je[checksSchluessel(nwo, c.SHA)] = checksEintrag{wert: c, zeit: jetzt}
}

// Verwerfen loescht den Eintrag eines SHA.
func (s *ChecksSpeicher) Verwerfen(nwo, sha string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.je, checksSchluessel(nwo, sha))
}
