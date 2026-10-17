// Der Plattenspeicher der CI-Runs (ci-runs, #122).
//
// Je Repo eine Datei mit dem letzten Stand der Runs und den Jobs je Run und
// Versuch, je Job eine Datei mit seinem Log. So holt ein Neustart des Dienstes
// nicht alles neu, und was sich nicht mehr aendert (Jobs eines
// abgeschlossenen Runs, das Log eines abgeschlossenen Jobs), wird nie ein
// zweites Mal gefragt. Wann erneuert wird, entscheidet internal/server;
// hier steht nur die Ablage.
package cache

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

// CIStand ist der gespeicherte Stand eines Repos.
type CIStand struct {
	// Zeit ist der letzte erfolgreiche Abruf.
	Zeit time.Time            `json:"zeit"`
	Runs []ghrest.WorkflowRun `json:"runs"`
	// Jobs je CIJobsSchluessel(run, versuch).
	Jobs  map[string]CIJobs `json:"jobs"`
	Grund string            `json:"grund,omitempty"`
}

// CIJobs sind die Jobs eines Runs. Abgeschlossen: der Run war beim Abruf
// abgeschlossen, seine Jobs aendern sich nicht mehr.
type CIJobs struct {
	Abgeschlossen bool                `json:"abgeschlossen"`
	Jobs          []ghrest.ActionsJob `json:"jobs"`
}

// CIJobsSchluessel ist der Schluessel der Jobs eines Runs und Versuchs.
func CIJobsSchluessel(runID int64, versuch int) string {
	return strconv.FormatInt(runID, 10) + "@" + strconv.Itoa(versuch)
}

// CIOrdner ist der Ablageort: CC_KNOWLEDGE_CI_CACHE, sonst der Ordner ci
// neben der Cache-Datei (CacheFile, also ~/.cache/cc-knowledge/ci). Wer
// CC_KNOWLEDGE_CACHE umlenkt — jeder Test, der den Dienst startet —, lenkt
// damit auch diesen Speicher um und schreibt nicht in den des Benutzers.
func CIOrdner() string {
	if env := os.Getenv("CC_KNOWLEDGE_CI_CACHE"); env != "" {
		return env
	}
	return filepath.Join(filepath.Dir(CacheFile()), "ci")
}

// ciName macht aus owner/repo einen Dateinamen; ghrest.GueltigesNWO laesst
// weder Pfadtrenner noch ".." durch.
func ciName(nwo string) (string, bool) {
	if !ghrest.GueltigesNWO(nwo) {
		return "", false
	}
	return strings.ReplaceAll(strings.ToLower(nwo), "/", "__"), true
}

func ciSchreiben(pfad string, wert any) error {
	if err := os.MkdirAll(filepath.Dir(pfad), 0o755); err != nil {
		return err
	}
	b, err := json.Marshal(wert)
	if err != nil {
		return err
	}
	tmp := pfad + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, pfad)
}

// CIStandLesen liest den Stand eines Repos; nil ohne oder bei unlesbarer
// Datei.
func CIStandLesen(ordner, nwo string) *CIStand {
	name, ok := ciName(nwo)
	if !ok {
		return nil
	}
	b, err := os.ReadFile(filepath.Join(ordner, name+".json"))
	if err != nil {
		return nil
	}
	var st CIStand
	if json.Unmarshal(b, &st) != nil || st.Zeit.IsZero() {
		return nil
	}
	if st.Runs == nil {
		st.Runs = []ghrest.WorkflowRun{}
	}
	if st.Jobs == nil {
		st.Jobs = map[string]CIJobs{}
	}
	return &st
}

// CIStandSchreiben legt den Stand eines Repos ab (erst in eine Nachbardatei,
// dann umbenannt, damit ein Absturz keine halbe Datei hinterlaesst).
func CIStandSchreiben(ordner, nwo string, st *CIStand) error {
	name, ok := ciName(nwo)
	if !ok {
		return os.ErrInvalid
	}
	return ciSchreiben(filepath.Join(ordner, name+".json"), st)
}

type ciLogDatei struct {
	Gekuerzt bool   `json:"gekuerzt"`
	Text     string `json:"text"`
}

func ciLogPfad(ordner, nwo string, jobID int64) (string, bool) {
	name, ok := ciName(nwo)
	if !ok || jobID <= 0 {
		return "", false
	}
	return filepath.Join(ordner, "logs", name, strconv.FormatInt(jobID, 10)+".json"), true
}

// CILogLesen liest das gespeicherte Log eines Jobs.
func CILogLesen(ordner, nwo string, jobID int64) (string, bool, bool) {
	pfad, ok := ciLogPfad(ordner, nwo, jobID)
	if !ok {
		return "", false, false
	}
	b, err := os.ReadFile(pfad)
	if err != nil {
		return "", false, false
	}
	var d ciLogDatei
	if json.Unmarshal(b, &d) != nil {
		return "", false, false
	}
	return d.Text, d.Gekuerzt, true
}

// CILogSchreiben legt das Log eines abgeschlossenen Jobs ab.
func CILogSchreiben(ordner, nwo string, jobID int64, text string, gekuerzt bool) error {
	pfad, ok := ciLogPfad(ordner, nwo, jobID)
	if !ok {
		return os.ErrInvalid
	}
	return ciSchreiben(pfad, ciLogDatei{Gekuerzt: gekuerzt, Text: text})
}
