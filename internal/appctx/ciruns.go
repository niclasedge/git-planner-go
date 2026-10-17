package appctx

// Die CI-Runs eines Repos (ci-runs, #122): Punkt je Schritt, Job und Run, die
// Liste der letzten Runs samt Punktreihe, Wahl des ersten roten Jobs und
// Schritts, der Punkt der Leiste und die Aufbereitung eines Job-Logs mit den
// Schritt-Grenzen aus den ##[group]-Marken. Der Browser zeichnet nur.

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/niclasedge/git-planner-go/internal/anzeigezeit"
	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

// Die vier Punkte.
const (
	CIGruen  = "gruen"
	CIRot    = "rot"
	CIGrau   = "grau"
	CILaeuft = "laeuft"
)

// CIKeineRuns ist der Grund bei einem Repo ohne Actions.
const CIKeineRuns = "keine Workflow-Runs"

// CIPunkt ordnet Status und Ergebnis eines Schritts, Jobs oder Runs einen
// Punkt zu.
func CIPunkt(status, ergebnis string) string {
	switch ergebnis {
	case "success":
		return CIGruen
	case "failure", "cancelled", "timed_out", "startup_failure":
		return CIRot
	case "":
		switch status {
		case "in_progress", "queued", "waiting", "pending", "requested":
			return CILaeuft
		}
	}
	return CIGrau
}

// ciErgebnisText benennt ein Ergebnis fuer den Tooltip.
func ciErgebnisText(status, ergebnis string) string {
	switch ergebnis {
	case "success":
		return "erfolgreich"
	case "failure", "startup_failure":
		return "fehlgeschlagen"
	case "cancelled":
		return "abgebrochen"
	case "timed_out":
		return "Zeit überschritten"
	case "skipped":
		return "übersprungen"
	case "":
		if CIPunkt(status, "") == CILaeuft {
			return "läuft"
		}
		return "ohne Ergebnis"
	}
	return ergebnis
}

// CIAnsicht ist die Antwort von GET /api/ci.
type CIAnsicht struct {
	Repo   string     `json:"repo"`
	Runs   []CIRun    `json:"runs"`
	Punkt  string     `json:"punkt"`
	Gruen  int        `json:"gruen"`
	Gesamt int        `json:"gesamt"`
	Kopf   string     `json:"kopf"`
	Grund  string     `json:"grund,omitempty"`
	Stand  string     `json:"stand,omitempty"`
	Sync   *SyncStand `json:"sync,omitempty"`
}

// CIRun ist eine Zeile der Liste.
type CIRun struct {
	ID          int64        `json:"id"`
	Nummer      int          `json:"nummer"`
	Versuch     int          `json:"versuch"`
	Workflow    string       `json:"workflow"`
	Branch      string       `json:"branch"`
	Event       string       `json:"event"`
	Betreff     string       `json:"betreff"`
	BetreffVoll string       `json:"betreff_voll"`
	SHA         string       `json:"sha"`
	Zeit        string       `json:"zeit"`
	ZeitVoll    string       `json:"zeit_voll"`
	Dauer       string       `json:"dauer"`
	DauerS      int64        `json:"dauer_s"`
	Punkt       string       `json:"punkt"`
	Status      string       `json:"status"`
	Ergebnis    string       `json:"ergebnis"`
	URL         string       `json:"url"`
	Punkte      []CIRunPunkt `json:"punkte"`
	Jobs        []CIJob      `json:"jobs"`
	WahlJob     int64        `json:"wahl_job"`
	WahlSchritt int          `json:"wahl_schritt"`
	NeuStartbar bool         `json:"neu_startbar"`
}

// CIRunPunkt ist ein Punkt der Reihe: ein Schritt eines Jobs.
type CIRunPunkt struct {
	Punkt   string `json:"punkt"`
	JobID   int64  `json:"job_id"`
	Schritt int    `json:"schritt"`
	Titel   string `json:"titel"`
}

// CIJob ist ein Job eines Runs.
type CIJob struct {
	ID       int64       `json:"id"`
	Name     string      `json:"name"`
	Punkt    string      `json:"punkt"`
	Ergebnis string      `json:"ergebnis"`
	Dauer    string      `json:"dauer"`
	URL      string      `json:"url"`
	Schritte []CISchritt `json:"schritte"`
}

// CISchritt ist ein Schritt eines Jobs.
type CISchritt struct {
	Nummer   int    `json:"nummer"`
	Name     string `json:"name"`
	Punkt    string `json:"punkt"`
	Ergebnis string `json:"ergebnis"`
	Dauer    string `json:"dauer"`
}

const ciBetreffLaenge = 72

func ciKurz(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

// ciDauer rechnet die Dauer eines Eintrags: abgeschlossen von start bis
// ende (ohne ende keine Dauer), sonst bis jetzt.
func ciDauer(status, start, ende string, jetzt time.Time) int64 {
	if start == "" {
		return 0
	}
	if status == "completed" {
		if ende == "" {
			return 0
		}
		return sekunden(start, ende, jetzt)
	}
	return sekunden(start, "", jetzt)
}

func ciSchritteSortiert(s []ghrest.ActionsSchritt) []ghrest.ActionsSchritt {
	out := append([]ghrest.ActionsSchritt{}, s...)
	sort.SliceStable(out, func(a, b int) bool { return out[a].Nummer < out[b].Nummer })
	return out
}

// CIAnsichtBauen baut die Liste der Runs: neueste zuerst (nach Start, bei
// Gleichstand nach Id), je Run die Jobs aus jobs[run.ID].
func CIAnsichtBauen(repo string, runs []ghrest.WorkflowRun, jobs map[int64][]ghrest.ActionsJob, jetzt time.Time) CIAnsicht {
	a := CIAnsicht{Repo: repo, Runs: []CIRun{}}
	folge := append([]ghrest.WorkflowRun{}, runs...)
	sort.SliceStable(folge, func(i, j int) bool {
		if folge[i].Start != folge[j].Start {
			return folge[i].Start > folge[j].Start
		}
		return folge[i].ID > folge[j].ID
	})
	for _, r := range folge {
		a.Runs = append(a.Runs, ciRunBauen(r, jobs[r.ID], jetzt))
	}
	for _, r := range a.Runs {
		if r.Punkt == CIGruen {
			a.Gruen++
		}
	}
	a.Gesamt = len(a.Runs)
	a.Kopf = fmt.Sprintf("%d von %d grün", a.Gruen, a.Gesamt)
	if len(a.Runs) > 0 {
		a.Punkt = a.Runs[0].Punkt
	} else {
		a.Grund = CIKeineRuns
	}
	return a
}

func ciRunBauen(r ghrest.WorkflowRun, jobs []ghrest.ActionsJob, jetzt time.Time) CIRun {
	voll := strings.TrimSpace(strings.SplitN(strings.ReplaceAll(r.Betreff, "\r", ""), "\n", 2)[0])
	sha := r.HeadSHA
	if len(sha) > 7 {
		sha = sha[:7]
	}
	out := CIRun{ID: r.ID, Nummer: r.Nummer, Versuch: r.Versuch, Workflow: r.Name, Branch: r.Branch, Event: r.Event,
		Betreff: ciKurz(voll, ciBetreffLaenge), BetreffVoll: voll, SHA: sha, Punkt: CIPunkt(r.Status, r.Ergebnis),
		Status: r.Status, Ergebnis: r.Ergebnis, URL: r.URL, Punkte: []CIRunPunkt{}, Jobs: []CIJob{}}
	out.Zeit, out.ZeitVoll = ZeitpunktText(r.Start, jetzt)
	out.DauerS = ciDauer(r.Status, r.Start, r.Aktualisiert, jetzt)
	out.Dauer = DauerText(out.DauerS)
	out.NeuStartbar = r.Status == "completed" && out.Punkt == CIRot

	var ersterJob, roterJob int64
	roterSchritt := 0
	for _, j := range jobs {
		cj := CIJob{ID: j.ID, Name: j.Name, Punkt: CIPunkt(j.Status, j.Ergebnis), Ergebnis: j.Ergebnis,
			Dauer: DauerText(ciDauer(j.Status, j.Start, j.Ende, jetzt)), URL: j.URL, Schritte: []CISchritt{}}
		if ersterJob == 0 {
			ersterJob = j.ID
		}
		ersterRoter := 0
		for _, s := range ciSchritteSortiert(j.Schritte) {
			p := CIPunkt(s.Status, s.Ergebnis)
			cj.Schritte = append(cj.Schritte, CISchritt{Nummer: s.Nummer, Name: s.Name, Punkt: p, Ergebnis: s.Ergebnis,
				Dauer: DauerText(ciDauer(s.Status, s.Start, s.Ende, jetzt))})
			out.Punkte = append(out.Punkte, CIRunPunkt{Punkt: p, JobID: j.ID, Schritt: s.Nummer,
				Titel: j.Name + " › " + s.Name + " · " + ciErgebnisText(s.Status, s.Ergebnis)})
			if p == CIRot && ersterRoter == 0 {
				ersterRoter = s.Nummer
			}
		}
		if roterJob == 0 && (cj.Punkt == CIRot || ersterRoter > 0) {
			roterJob, roterSchritt = j.ID, ersterRoter
		}
		out.Jobs = append(out.Jobs, cj)
	}
	if roterJob != 0 {
		out.WahlJob, out.WahlSchritt = roterJob, roterSchritt
	} else {
		out.WahlJob = ersterJob
	}
	return out
}

// CILeistenPunkt ist der Punkt des juengsten Runs, leer ohne Runs.
func CILeistenPunkt(runs []ghrest.WorkflowRun) string {
	a := CIAnsichtBauen("", runs, nil, anzeigezeit.Jetzt())
	return a.Punkt
}

// CILog ist die Antwort von GET /api/ci-log.
type CILog struct {
	JobID    int64          `json:"job_id"`
	Name     string         `json:"name"`
	URL      string         `json:"url"`
	Zeilen   []CILogZeile   `json:"zeilen"`
	Schritte []CILogSchritt `json:"schritte"`
	Grenzen  bool           `json:"grenzen"`
	Gekuerzt bool           `json:"gekuerzt"`
	Grund    string         `json:"grund,omitempty"`
}

// CILogZeile ist eine Zeile des Logs. Text ist reiner Text: die Oberflaeche
// setzt ihn nie als HTML.
type CILogZeile struct {
	Nr     int    `json:"nr"`
	Zeit   string `json:"zeit,omitempty"`
	Text   string `json:"text"`
	Fehler bool   `json:"fehler,omitempty"`
}

// CILogSchritt nennt die Zeile, an der ein Schritt beginnt (0 = unbekannt).
type CILogSchritt struct {
	Nummer int    `json:"nummer"`
	Name   string `json:"name"`
	Zeile  int    `json:"zeile"`
}

var (
	ciZeitRe = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?Z) ?`)
	ciAnsiRe = regexp.MustCompile("\x1b\\[[0-9;?]*[A-Za-z]")
)

// CILogAufbereiten zerlegt ein Job-Log in Zeilen: Zeitstempel abgetrennt,
// ANSI-Farbfolgen entfernt, ##[error] markiert, sonst Zeichen fuer Zeichen.
// Dazu die Zeile, an der jeder Schritt beginnt (siehe ciGrenzen).
func CILogAufbereiten(text string, job ghrest.ActionsJob, gekuerzt bool) CILog {
	l := CILog{JobID: job.ID, Name: job.Name, URL: job.URL, Zeilen: []CILogZeile{}, Schritte: []CILogSchritt{}, Gekuerzt: gekuerzt}
	text = strings.TrimPrefix(text, "\ufeff")
	text = strings.TrimSuffix(text, "\n")
	if text != "" {
		for i, z := range strings.Split(text, "\n") {
			z = strings.TrimSuffix(z, "\r")
			zeile := CILogZeile{Nr: i + 1}
			if m := ciZeitRe.FindStringSubmatch(z); m != nil {
				zeile.Zeit = m[1]
				z = z[len(m[0]):]
			}
			zeile.Text = ciAnsiRe.ReplaceAllString(z, "")
			zeile.Fehler = strings.HasPrefix(zeile.Text, "##[error]")
			if strings.HasPrefix(zeile.Text, "##[group]") {
				l.Grenzen = true
			}
			l.Zeilen = append(l.Zeilen, zeile)
		}
	}
	l.Schritte = ciGrenzen(l.Zeilen, ciSchritteSortiert(job.Schritte), l.Grenzen)
	return l
}

// ciGrenzen findet je Schritt seine erste Zeile: der erste Schritt beginnt
// in Zeile 1, ein uebersprungener hat keine; jeder weitere beginnt an der
// naechsten ##[group]-Zeile nach dem vorigen Beginn, deren Titel seinem Namen
// gleicht, sonst an der naechsten "##[group]Run …"; "Post …" an der naechsten
// "Post job cleanup.", "Complete job" an "Cleaning up orphan processes".
func ciGrenzen(zeilen []CILogZeile, schritte []ghrest.ActionsSchritt, grenzen bool) []CILogSchritt {
	out := make([]CILogSchritt, 0, len(schritte))
	suche := func(ab int, passt func(string) bool) int {
		for k := ab; k < len(zeilen); k++ {
			if passt(zeilen[k].Text) {
				return k
			}
		}
		return -1
	}
	pos := 0
	for i, s := range schritte {
		e := CILogSchritt{Nummer: s.Nummer, Name: s.Name}
		if !grenzen || len(zeilen) == 0 {
			out = append(out, e)
			continue
		}
		if i == 0 {
			e.Zeile, pos = 1, 0
			out = append(out, e)
			continue
		}
		if s.Ergebnis == "skipped" {
			out = append(out, e)
			continue
		}
		k := -1
		switch {
		case s.Name == "Complete job":
			k = suche(pos+1, func(t string) bool { return strings.HasPrefix(t, "Cleaning up orphan processes") })
		case strings.HasPrefix(s.Name, "Post "):
			k = suche(pos+1, func(t string) bool { return strings.TrimSpace(t) == "Post job cleanup." })
		default:
			k = suche(pos+1, func(t string) bool { return strings.TrimSpace(t) == "##[group]"+s.Name })
			if k < 0 {
				k = suche(pos+1, func(t string) bool { return strings.HasPrefix(t, "##[group]Run ") })
			}
		}
		if k >= 0 {
			e.Zeile, pos = k+1, k
		}
		out = append(out, e)
	}
	return out
}
