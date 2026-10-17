package collectors

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/niclasedge/git-planner-go/internal/shared"
)

// BeadsWorktree is one entry from `git worktree list --porcelain`.
type BeadsWorktree struct {
	Path   string `json:"path"`
	Branch string `json:"branch"`
}

// BeadsSubBead is one child issue under a map (`bd show <map> --children --json`).
type BeadsSubBead struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Status    string `json:"status"`
	IssueType string `json:"issue_type"`
	Priority  int    `json:"priority"`
	ClosedAt  string `json:"closed_at"`
	// UpdatedAt is bd's own last-touched timestamp (RFC3339 UTC). Carried so
	// the reports view can order sub-beads by newest change — a bead whose
	// status/description/comments moved counts as changed even when no report
	// entry was ever filed against it. Already present in the JSON
	// beadsChildrenBatch fetches, so this costs no extra subprocess.
	UpdatedAt string `json:"updated_at"`
	// Ready mirrors `bd ready`: startable now, i.e. open with every dependency
	// closed. It is bd's own verdict, not a re-derivation — see bdReadyIDs.
	Ready bool `json:"ready"`
	// DueAt und DeferUntil sind Things' Deadline und When (Spec §4.4). Sie
	// bleiben hier ROH, so wie bd sie geschrieben hat, und werden NICHT gedeutet
	// — das ist Aufgabe genau einer Stelle (bdops.ParseUserDate, siehe
	// appctx/ansicht.go). Der Wert traegt ein "Z", das luegt: er ist lokale
	// Wanduhr. Wer ihn hier schon in eine time.Time verwandelte, brauchte dafuer
	// eine Zone, und der Collector hat keine — er kennt nur die der Maschine,
	// und genau die macht einen Test auf einem UTC-Agenten wertlos.
	//
	// Beide kommen aus `bd show <ids> --children --json` und kosten damit keinen
	// zusaetzlichen Prozess (gemessen 2026-09-14 an einer Wegwerf-Datenbank: der
	// Kinder-Aufruf traegt due_at und defer_until, sobald sie gesetzt sind).
	DueAt      string `json:"due_at,omitempty"`
	DeferUntil string `json:"defer_until,omitempty"`
	// Children sind die Unteraufgaben DIESES Beads — beliebig tief (gkp-1lp,
	// E5 der Spec). Enkel sind keine eigene Kategorie, sondern stehen hier
	// genauso wie Kinder; die Ansicht rueckt sie nur tiefer ein.
	//
	// omitempty ist Absicht: der weit ueberwiegende Teil der Beads ist ein
	// Blatt (am 2026-09-13 hatte dieses Repo 99 Beads und genau 3 davon
	// Kinder), und ein "children":[] an jedem einzelnen davon waere Ballast in
	// jeder Antwort. Die Ansicht muss deshalb mit einem fehlenden Feld
	// rechnen, nicht mit einer leeren Liste.
	Children []BeadsSubBead `json:"children,omitempty"`
}

// BeadsMap is one epic with its sub-beads and rendered plan SVG.
type BeadsMap struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Status   string `json:"status"`
	Priority int    `json:"priority"`
	// UpdatedAt is bd's own last-touched timestamp for the epic (RFC3339 UTC).
	// Carried for the same reason BeadsSubBead carries it: the view orders maps
	// by newest change, and an epic whose own fields moved counts as changed
	// even when none of its children did.
	UpdatedAt string `json:"updated_at,omitempty"`
	// ClosedAt ist gesetzt, wenn das Epic abgeschlossen ist — die Rueckschau
	// beschriftet die geschlossenen Karten damit ("vor 3d fertig"), und nur
	// dieses Feld unterscheidet "gerade fertig geworden" von "liegt seit Wochen
	// zu" (gkp-ar7.27).
	ClosedAt string `json:"closed_at,omitempty"`
	// IsMap marks an epic that carries the wayfinder:map label, i.e. a map
	// somebody deliberately drew, as opposed to a plain epic.
	//
	// This is a PROPERTY, not a filter. It used to be a filter with a
	// fallback, and that hid every unlabeled epic as soon as one labeled epic
	// existed (gkp-uc7.1). Both kinds are collected now; the view decides how
	// to present the difference, and "not a map" is never the same as "not
	// there".
	IsMap bool `json:"is_map"`
	// Loose markiert die synthetische Karte fuer Beads ohne Epic (siehe
	// looseMap). Sie ist kein Epic: es gibt sie in bd nicht, `bd graph` kennt
	// ihre Id nicht, und die Ansicht laesst deshalb den Plan-Graphen weg,
	// statt einen fehlschlagenden Aufruf zu zeigen.
	Loose    bool           `json:"loose,omitempty"`
	Children []BeadsSubBead `json:"children"`
	// PlanSVG is left empty by CollectBeads since gkp-3k6 — the graph is
	// fetched per map from /plan-svg instead. The field stays because it is
	// still the shape the view reads: a filled one is rendered directly, an
	// empty one triggers the fetch.
	PlanSVG string `json:"plan_svg"`
}

// BeadsRepo is the beads-collector result for one repo: its worktrees (own
// list, NOT a grouping level — maps hang off the repo, not the worktree,
// because .beads/ is shared across worktrees via the git root) plus its maps.
type BeadsRepo struct {
	Available bool   `json:"available"`
	Error     string `json:"error,omitempty"`
	// Name is the repo identity the reports store (internal/reports/store.go,
	// deriveGit) keys ~/.claude/reports/<Name>/ by: the MAIN worktree's
	// directory basename, sanitized the same way — so the reports view can
	// join BeadsMap entries against report-store entries even when this app
	// is launched from a linked worktree (demo-axk.4, integration gap 1).
	// "" when cwd could not be resolved to any repo at all (no .beads/ found
	// — CollectBeads returns early before repoIdentity ever runs).
	Name      string          `json:"name,omitempty"`
	Path      string          `json:"path,omitempty"` // the main worktree's root, for reference/debugging
	Worktrees []BeadsWorktree `json:"worktrees"`
	Maps      []BeadsMap      `json:"maps"`
}

// bdEpicJSON mirrors the fields we need from `bd list -t epic --json`.
type bdEpicJSON struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Status    string `json:"status"`
	Priority  int    `json:"priority"`
	UpdatedAt string `json:"updated_at"`
	ClosedAt  string `json:"closed_at"`
	// Labels is bd's own label list. Read rather than filtered on, so
	// wayfinder:map can be a property of the epic instead of a precondition
	// for seeing it at all (gkp-uc7.1).
	Labels []string `json:"labels"`
}

// bdChildJSON mirrors the fields we need from `bd show <id> --children --json`.
type bdChildJSON struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Status    string `json:"status"`
	IssueType string `json:"issue_type"`
	Priority  int    `json:"priority"`
	ClosedAt  string `json:"closed_at"`
	UpdatedAt string `json:"updated_at"`
	// Die beiden Termine — siehe BeadsSubBead.DueAt. bd laesst sie im
	// Kinder-JSON weg, wenn sie nicht gesetzt sind; "" ist also der Normalfall
	// und heisst "kein Termin", nicht "nicht gelesen".
	DueAt      string `json:"due_at"`
	DeferUntil string `json:"defer_until"`
}

func bdRun(repo string, args ...string) (string, bool) {
	return shared.Run(append([]string{"bd"}, args...), shared.RunOptions{Cwd: repo, TimeoutMs: shared.BdTimeoutMs})
}

// parseWorktreePorcelain parses `git worktree list --porcelain` output into
// path+branch pairs. Entries are separated by blank lines; a detached
// worktree simply has no "branch " line (Branch stays "").
func parseWorktreePorcelain(out string) []BeadsWorktree {
	result := []BeadsWorktree{}
	if out == "" {
		return result
	}
	var cur BeadsWorktree
	flush := func() {
		if cur.Path != "" {
			result = append(result, cur)
		}
		cur = BeadsWorktree{}
	}
	for _, line := range strings.Split(out, "\n") {
		switch {
		case line == "":
			flush()
		case strings.HasPrefix(line, "worktree "):
			flush()
			cur.Path = strings.TrimPrefix(line, "worktree ")
		case strings.HasPrefix(line, "branch "):
			cur.Branch = strings.TrimPrefix(strings.TrimPrefix(line, "branch "), "refs/heads/")
		}
	}
	flush()
	return result
}

// beadsWorktrees shells out to `git worktree list --porcelain` for repo.
func beadsWorktrees(repo string) []BeadsWorktree {
	out, ok := gitRun(repo, "worktree", "list", "--porcelain")
	if !ok {
		return []BeadsWorktree{}
	}
	return parseWorktreePorcelain(out)
}

// repoIdentity resolves (name, path) for cwd the same way
// internal/reports/store.go's deriveGit resolves its "repo" return value: the
// MAIN worktree's directory basename (not cwd's own worktree), so a call from
// a linked worktree (.claude/worktrees/<x>) and a call from the main
// checkout land on the identical name (demo-axk.4, integration gap 1).
// Reuses this package's existing git-worktree parsing (parseWorktreePorcelain)
// instead of re-deriving it — reports.deriveGit itself is unexported and
// internal/reports must not be modified from here.
func repoIdentity(cwd string) (name, path string) {
	cwd = filepath.Clean(cwd)
	mainRoot := filepath.Clean(RepoRoot(cwd)) // git rev-parse --show-toplevel, falls back to cwd
	if wtOut, ok := gitRun(cwd, "worktree", "list", "--porcelain"); ok {
		if wts := parseWorktreePorcelain(wtOut); len(wts) > 0 {
			mainRoot = filepath.Clean(wts[0].Path)
		}
	}
	return sanitizeRepoComponent(filepath.Base(mainRoot)), mainRoot
}

// reportsRepoNameCache memoizes ReportsRepoName per cwd: a repo's main
// worktree basename cannot change while this process is running, so paying
// for repoIdentity's two git subprocess calls (rev-parse, worktree list) on
// every single cached-context request — demo-axk.4 review Welle 2,
// finding 2's freshness guard calls this on EVERY request, cache hit or not
// — would reintroduce exactly the per-request subprocess cost that card was
// supposed to avoid. Memoizing it turns steady-state cost back into "one
// stat per report file" (cache.reportsFingerprint), nothing more.
var reportsRepoNameCache sync.Map // cwd string -> resolved name string

// ReportsRepoName returns the repo identity internal/reports/store.go's
// deriveGit would derive for cwd (repoIdentity's Name half), exported so
// internal/cache can build a cheap freshness fingerprint for the reports
// card (demo-axk.4 review Welle 2, finding 2) without importing
// internal/reports' unexported deriveGit or ever shelling out to bd/dot —
// repoIdentity/deriveGit only ever call git.
func ReportsRepoName(cwd string) string {
	if v, ok := reportsRepoNameCache.Load(cwd); ok {
		return v.(string)
	}
	name, _ := repoIdentity(cwd)
	reportsRepoNameCache.Store(cwd, name)
	return name
}

// sanitizeRepoComponent mirrors reports.sanitizePathComponent's guard: "",
// ".", ".." or anything containing a path separator falls back to a neutral
// placeholder instead of being used raw as a single path segment.
func sanitizeRepoComponent(s string) string {
	if s == "" || s == "." || s == ".." || strings.ContainsAny(s, `/\`) {
		return "unknown"
	}
	return s
}

// mapLabel marks an epic as a map drawn by /wayfinder. It is carried as a
// property of the epic, not used as a filter — see bdBestand.offeneEpics.
//
// Zwei Dinge waren hier Fehler (gkp-uc7.1, gkp-uc7.2), und beide waren von
// aussen unsichtbar. Sie stehen weiter hier, weil die Ableitung aus dem Bestand
// (gkp-mnd) sie beide erben koennte, wenn jemand sie vergisst:
//
// Das Label war einmal ein FILTER mit Rueckfall: erst `-l wayfinder:map`, und
// nur wenn das leer blieb, alles. Ein beschriftetes Epic genuegte damit, jedes
// unbeschriftete fuer immer zu verstecken. Gemessen in orbit am 2026-09-04:
// 2 Epics, eins beschriftet — die Seite zeigte orbit-kd3 und enthielt orbit-61z
// schlicht nicht, das laufende PR-Modul mit 12 Beads.
//
// Und `bd list` ohne --limit kappt still bei 50 und sagt es nicht. Das gilt
// weiterhin fuer den einen Aufruf, der uebrig ist (bdBestandLesen): das 51.
// Issue waere einfach weg, ohne Fehler und ohne Zaehler.
const mapLabel = "wayfinder:map"

// LooseMapID ist die Id der synthetischen Karte fuer Beads ohne Epic. Kein
// bd-Prefix und kein Punkt darin, damit sie mit keiner echten Id kollidieren
// kann (bd-Ids sind immer <prefix>-<suffix>).
const LooseMapID = "ohne-epic"

// LooseMapTitle steht als Kartentitel in der Ansicht.
const LooseMapTitle = "Ohne Epic"

// looseMap baut die synthetische Karte aus den losen Beads. Getrennt von
// bdListLoose, damit die Form ohne bd testbar ist.
//
// Status "open" statt "" ist Absicht: die Karte ist kein Epic und hat keinen
// eigenen Zustand, aber die Ansicht liest den Status, und "" faerbt sie wie
// eine geschlossene Karte. UpdatedAt bleibt leer — mapLastChange nimmt dann
// den neuesten Stempel der Kinder, was fuer diese Karte die richtige Antwort
// ist: sie hat keine eigene Geschichte.
//
// kinder ist die flache Hierarchie aus beadsChildrenTree (gkp-1lp). Ein loser
// Bead ist eine Aufgabe wie jede andere und darf Unteraufgaben haben — ohne das
// Argument haette ein Repo ohne Epics (no epics) weiterhin nur eine Ebene. nil ist
// erlaubt und heisst schlicht: keine Unteraufgaben bekannt.
func looseMap(loose []bdChildJSON, ready map[string]bool, kinder map[string][]BeadsSubBead) BeadsMap {
	children := make([]BeadsSubBead, 0, len(loose))
	for _, i := range loose {
		children = append(children, BeadsSubBead{
			ID:        i.ID,
			Title:     i.Title,
			Status:    i.Status,
			IssueType: i.IssueType,
			Priority:  i.Priority,
			ClosedAt:  i.ClosedAt,
			UpdatedAt: i.UpdatedAt,
			// Die Termine gehoeren hier genauso hin wie im Kinder-Pfad. Dass das
			// beim ersten Anlauf fehlte, ist die Fehlerklasse aus Spec §6: ein
			// Feld kommt zur Struktur dazu, die Tests bleiben gruen, und ein
			// ZWEITER Erbauer zieht nicht mit. Gefunden hat es kein Unit-Test,
			// sondern der Routen-Test gegen echtes bd — in einem frischen Repo
			// haengt jede Aufgabe unter KEINEM Epic, also lief alles durch
			// genau diesen Pfad und kein Termin kam an.
			DueAt:      i.DueAt,
			DeferUntil: i.DeferUntil,
			Ready:      ready[i.ID],
			Children:   beadsBaum(kinder, i.ID, ready, map[string]bool{}),
		})
	}
	return BeadsMap{
		ID:       LooseMapID,
		Title:    LooseMapTitle,
		Status:   "open",
		Loose:    true,
		Children: children,
	}
}

// closedEpicsMax ist die Kappung der Rueckschau: die zuletzt abgeschlossenen
// Karten, nicht alle. Ein altes Repo haette sonst Dutzende, und die Frage, die
// diese Liste beantwortet, ist "was ist gerade fertig geworden" — nicht "was
// wurde je gemacht". Fuenf, weil der Nutzer fuenf gesagt hat.
const closedEpicsMax = 5

// bdReadyIDs asks bd once for every startable bead in the repo and returns
// their ids as a set. ok=false on a failed/missing bd; an empty set with
// ok=true means "nothing is startable", which is a legitimate answer.
//
// Once for the whole repo, not once per map, and bd's verdict rather than our
// own (gkp-uc7.3). Measured 2026-09-04 in infra (12 maps, 156 issues):
// this call takes 2.3 s and returns 42 beads, where `bd ready --parent <map>`
// twelve times would cost ~28 s — process start alone is 105–153 ms.
//
// The alternative was to derive it: markDotBlocked below already computes
// "blocked" from the plan graph, exactly and for free, and open-minus-blocked
// agreed with `bd ready` on every map measured. It was still the wrong source.
// It is a second definition of a term bd already defines, built against bd's
// DOT output, and it had one corner nobody could measure: `bd ready` excludes
// deferred beads, and no repo here has one, so what glyph they get was
// unknown. A view that quietly disagrees with what the user sees in the
// terminal is worse than a view that costs one subprocess.
//
// The result carries no parent, so the caller must intersect it with each
// map's Children. That is not a shortcoming to work around — `bd graph --dot
// <map>` returns the connected COMPONENT rather than the subtree (measured:
// infra-nn8 and -qja yield an identical node set including foreign beads),
// so Children is the only trustworthy statement of membership anyway.
func bdReadyIDs(repo string) (map[string]bool, bool) {
	out, ok := bdRun(repo, "ready", "--json", "--limit", "0")
	if !ok {
		return nil, false
	}
	beads, ok := shared.ParseJSON[[]bdEpicJSON](out)
	if !ok {
		return nil, false
	}
	ids := make(map[string]bool, len(beads))
	for _, b := range beads {
		ids[b.ID] = true
	}
	return ids, true
}

// hasMapLabel reports whether an epic carries mapLabel.
func hasMapLabel(e bdEpicJSON) bool {
	for _, l := range e.Labels {
		if l == mapLabel {
			return true
		}
	}
	return false
}

// parseBdChildren extracts the sub-bead array keyed by mapID from
// `bd show <mapID> --children --json` output (shape
// `{"<mapID>": [...], "schema_version": 1}`). A missing key, empty input, or
// malformed JSON all yield an empty (never nil) slice.
func parseBdChildren(raw, mapID string) []BeadsSubBead {
	return parseBdChildrenBatch(raw, []string{mapID})[mapID]
}

// parseBdChildrenBatch is parseBdChildren for many ids at once: `bd show`
// keys its JSON by id whether it was given one id or twelve, so the batch
// shape is the single shape with more keys. Every requested id gets an entry —
// an empty (never nil) slice when bd had nothing to say about it — so callers
// need not distinguish "unknown id" from "no children".
func parseBdChildrenBatch(raw string, mapIDs []string) map[string][]BeadsSubBead {
	out := make(map[string][]BeadsSubBead, len(mapIDs))
	for _, id := range mapIDs {
		out[id] = []BeadsSubBead{}
	}
	if raw == "" {
		return out
	}
	fields, ok := shared.ParseJSON[map[string]json.RawMessage](raw)
	if !ok {
		return out
	}
	for _, id := range mapIDs {
		msg, ok := fields[id]
		if !ok {
			continue
		}
		var children []bdChildJSON
		if json.Unmarshal(msg, &children) != nil {
			continue
		}
		for _, c := range children {
			out[id] = append(out[id], BeadsSubBead{
				ID:         c.ID,
				Title:      c.Title,
				Status:     c.Status,
				IssueType:  c.IssueType,
				Priority:   c.Priority,
				ClosedAt:   c.ClosedAt,
				UpdatedAt:  c.UpdatedAt,
				DueAt:      c.DueAt,
				DeferUntil: c.DeferUntil,
			})
		}
	}
	return out
}

// beadsChildrenBatch shells out to `bd show <id> <id> … --children --json`,
// ONE call for every epic in the repo instead of one per epic (gkp-1uu).
// Measured at infra with 12 epics: 12 single calls 9775 ms, one batch
// 3201 ms — the per-call cost is bd opening its Dolt DB, not the query, so it
// is paid once instead of twelve times.
//
// The command exits 0 even for an unknown id (bd prints the error to stderr,
// which shared.Run discards), so that case is simply "no children", not an
// error — same fail-open contract as before, now for the whole batch: if the
// single call fails, every epic renders childless and the card still draws.
func beadsChildrenBatch(repo string, mapIDs []string) map[string][]BeadsSubBead {
	if len(mapIDs) == 0 {
		return map[string][]BeadsSubBead{}
	}
	args := make([]string, 0, len(mapIDs)+3)
	args = append(args, "show")
	args = append(args, mapIDs...)
	args = append(args, "--children", "--json")
	raw, ok := bdRun(repo, args...)
	if !ok {
		raw = ""
	}
	return parseBdChildrenBatch(raw, mapIDs)
}

// ---------------------------------------------------------------- gkp-1lp

// bdBestandJSON ist der Ausschnitt aus `bd list --json`, den der Bestand
// braucht. `parent` ist die einzige verlaessliche Auskunft ueber Zugehoerigkeit:
// die Hierarchie steckt NICHT im Punkt der Id. gkp-ko0 hat keinen Punkt und ist
// trotzdem Kind von gkp-ar7, weil der MCP ueber `bd create --graph` flache
// Hash-Ids anlegt und nur `bd create --parent` gepunktete vergibt — wer ueber
// Id-Punkte parst, liegt bei 28 von 70 Beads falsch.
// Seit gkp-mnd traegt der Bestand die VOLLEN Felder: er ist nicht mehr nur die
// Abkuerzung fuer den Abstieg, sondern die einzige Quelle, aus der die drei
// Listen (offene Epics, Rueckschau, lose Beads) abgeleitet werden. Die Felder
// stehen alle in derselben Antwort — gemessen am 2026-09-14 in infra und
// git-planner: `bd list --json --limit 0 --status all` liefert id,
// title, status, issue_type, priority, closed_at, updated_at, parent, labels,
// due_at und defer_until fuer JEDEN Bead.
type bdBestandJSON struct {
	ID        string `json:"id"`
	Parent    string `json:"parent"`
	UpdatedAt string `json:"updated_at"`
	Title     string `json:"title"`
	Status    string `json:"status"`
	IssueType string `json:"issue_type"`
	Priority  int    `json:"priority"`
	ClosedAt  string `json:"closed_at"`
	Labels    []string
	// Die Termine — siehe BeadsSubBead.DueAt. Sie reisen mit, weil ein loser
	// Bead sie genauso traegt wie ein Kind, und der lose Zweig sie frueher aus
	// einem eigenen Aufruf bekam.
	DueAt      string `json:"due_at"`
	DeferUntil string `json:"defer_until"`
}

// statusGeschlossen ist bds eigener Wert fuer "erledigt" und die einzige
// Trennlinie, die `bd list` ohne --status zieht.
//
// Das ist gemessen und nicht angenommen (2026-09-14, Wegwerf-Kopie von
// infra): bd kennt fuenf Status — open, in_progress, blocked, deferred,
// closed. Ein Epic in jedem der ersten vier steht in `bd list -t epic` drin,
// nur das geschlossene fehlt. "Alles ausser closed" ist damit exakt bds
// Voreinstellung und keine Naeherung.
const statusGeschlossen = "closed"

// bdBestand ist jeder Bead des Repos, offen wie geschlossen.
//
// Der Nullwert bedeutet "keine Auskunft" und ist damit die fail-open-Antwort:
// kannKinderHaben sagt dann bei jeder Id ja, der Abstieg wird eine Ebene teurer
// und bleibt vollstaendig.
type bdBestand struct {
	// alle ist der Bestand in bds eigener Reihenfolge. Die Ableitungen unten
	// lesen ihn; die Reihenfolge bleibt bds, damit sich zur frueheren Fassung
	// (drei getrennte `bd list`) nichts verschiebt.
	alle      []bdBestandJSON
	bekannt   map[string]bool
	hatKinder map[string]bool
	stand     map[string]string // id -> updated_at
}

// epicAus formt einen Bestandseintrag in die Form, die CollectBeads von einem
// Epic erwartet. Eine Stelle statt zweier, damit die offene und die
// geschlossene Liste nicht verschieden abschreiben koennen.
func epicAus(i bdBestandJSON) bdEpicJSON {
	return bdEpicJSON{
		ID: i.ID, Title: i.Title, Status: i.Status, Priority: i.Priority,
		UpdatedAt: i.UpdatedAt, ClosedAt: i.ClosedAt, Labels: i.Labels,
	}
}

// offeneEpics ersetzt `bd list -t epic --json --limit 0`.
//
// Gegengeprueft am 2026-09-14 gegen das echte bd in zwei Repos: infra
// 15 von 15, git-planner 2 von 2, jeweils dieselbe Menge.
func (b bdBestand) offeneEpics() []bdEpicJSON {
	out := []bdEpicJSON{}
	for _, i := range b.alle {
		if i.IssueType == "epic" && i.Status != statusGeschlossen {
			out = append(out, epicAus(i))
		}
	}
	return out
}

// geschlosseneEpics ersetzt
// `bd list -t epic --status closed --sort closed --limit N --json`.
//
// Absteigend nach ClosedAt, dann gekappt — die Reihenfolge der beiden Schritte
// ist der ganze Punkt: `--limit` ohne die richtige Sortierrichtung haette die
// AELTESTEN N geliefert (gkp-ar7.27). Der Id-Vergleich als zweite Stufe macht
// die Auswahl bei gleichem Stempel deterministisch, statt sie der
// Sortierstabilitaet zu ueberlassen.
//
// Gegengeprueft am 2026-09-14 gegen `--sort closed`: infra liefert exakt
// dieselben fuenf Ids in derselben Folge, git-planner dieselbe eine.
func (b bdBestand) geschlosseneEpics(max int) []bdEpicJSON {
	out := []bdEpicJSON{}
	for _, i := range b.alle {
		if i.IssueType == "epic" && i.Status == statusGeschlossen {
			out = append(out, epicAus(i))
		}
	}
	sort.SliceStable(out, func(x, y int) bool {
		if out[x].ClosedAt != out[y].ClosedAt {
			return out[x].ClosedAt > out[y].ClosedAt
		}
		return out[x].ID < out[y].ID
	})
	if max >= 0 && len(out) > max {
		out = out[:max]
	}
	return out
}

// loseBeads ersetzt `bd list --json --limit 0 --no-parent --exclude-type epic`.
//
// Die drei Bedingungen sind bds drei Schalter, eins zu eins: kein Elternteil,
// kein Epic, nicht geschlossen (das Letzte ist bds Voreinstellung, siehe
// statusGeschlossen). Gegengeprueft am 2026-09-14: infra 2 von 2,
// git-planner 0 von 0 — dort haengt alles unter einem Epic.
func (b bdBestand) loseBeads() []bdChildJSON {
	out := []bdChildJSON{}
	for _, i := range b.alle {
		if i.Parent != "" || i.IssueType == "epic" || i.Status == statusGeschlossen {
			continue
		}
		out = append(out, bdChildJSON{
			ID: i.ID, Title: i.Title, Status: i.Status, IssueType: i.IssueType,
			Priority: i.Priority, ClosedAt: i.ClosedAt, UpdatedAt: i.UpdatedAt,
			DueAt: i.DueAt, DeferUntil: i.DeferUntil,
		})
	}
	return out
}

// kannKinderHaben beantwortet die einzige Frage, fuer die es den Bestand gibt:
// muss unter dieser Id noch nachgefragt werden?
//
// Die Antwort ist bewusst schief. Nur POSITIVES Wissen darf einen Zweig
// abschneiden — bd kennt den Bead und nennt kein Kind. Eine Id, die im Bestand
// gar nicht vorkommt, gilt als moeglicher Elternteil und wird abgestiegen. Die
// Alternative (was nicht im Bestand steht, hat keine Kinder) wuerde einen
// ganzen Teilbaum still verschwinden lassen, sobald bds Listenabfrage und seine
// Kinderabfrage einmal nicht dasselbe sagen. Gegengeprueft am 2026-09-13:
// `bd list --status all` und `bd show gkp-ar7 --children` nennen exakt
// dieselben 62 Kinder.
func (b bdBestand) kannKinderHaben(id string) bool {
	if b.bekannt == nil || !b.bekannt[id] {
		return true
	}
	return b.hatKinder[id]
}

// bdBestandLesen holt `bd list --json --limit 0 --status all` — jeden Bead des
// Repos, offen wie geschlossen, mit seinem Elternteil.
//
// Warum ein Zusatzaufruf sich lohnt: `bd show <ids> --children` kostet NICHT
// einen Prozessstart, sondern pro Id. Gemessen an diesem Repo am 2026-09-13:
// 1 Id 410 ms, 5 Ids 1149 ms, 10 Ids 1526 ms, 20 Ids 2391 ms, 62 Ids 6620 ms.
// Ein rekursiver Abstieg muss aber am Ende jede Ebene einmal fragen, auch die
// unterste — und die unterste ist die breiteste. Gegengerechnet an diesem Repo
// (99 Beads, davon 3 mit Kindern, kein einziger Enkel): die blinde zweite Ebene
// ueber alle 99 Kinder kostet 10145 ms, der Bestand 574 ms. Ohne ihn wuerde das
// Oeffnen eines Projekts also um zehn Sekunden teurer, nur um zu erfahren, dass
// 99 Blaetter Blaetter sind.
//
// `--status all` ist Pflicht: ohne den Schalter zeigt bd nur offene Beads (41
// statt 99 hier), und eine geschlossene Unteraufgabe waere dann eine Id ohne
// Bestandseintrag — nach kannKinderHaben harmlos, aber ihr Datum fehlte in
// PlanSVGCached. `--limit 0` ebenso: bd kappt sonst still bei 50.
//
// Seit gkp-mnd ist dieser Aufruf nicht mehr nur die Abkuerzung, sondern die
// EINZIGE Listenabfrage von CollectBeads. Vorher standen daneben drei weitere —
// `-t epic`, `-t epic --status closed` und `--no-parent --exclude-type epic` —,
// und die kosteten nicht ihre Antwort, sondern ihren Start: gemessen am
// 2026-09-14 je 232–412 ms, unabhaengig davon, ob sie zwei Zeilen oder
// zweihundert zurueckgaben. Alle drei sind Teilmengen DIESER Antwort (siehe
// offeneEpics/geschlosseneEpics/loseBeads), also war es dreimal derselbe
// Datenbankstart fuer Daten, die schon da waren.
//
// Bei ok=false faellt die Liste jetzt nicht mehr "nur" um die Abkuerzung,
// sondern ganz aus — CollectBeads meldet denselben Fehler, den frueher ein
// fehlgeschlagenes `bd list -t epic` gemeldet hat. Der Ausfallmodus ist damit
// derselbe wie vorher, nur haengt er an einem Aufruf statt an vieren.
// bestandCached (Plan-Graphen) behaelt seine eigene fail-open-Lesart.
func bdBestandLesen(repo string) (bdBestand, bool) {
	out, ok := bdRun(repo, "list", "--json", "--limit", "0", "--status", "all")
	if !ok {
		return bdBestand{}, false
	}
	beads, ok := shared.ParseJSON[[]bdBestandJSON](out)
	if !ok {
		return bdBestand{}, false
	}
	b := bdBestand{
		alle:      beads,
		bekannt:   make(map[string]bool, len(beads)),
		hatKinder: make(map[string]bool),
		stand:     make(map[string]string, len(beads)),
	}
	for _, i := range beads {
		b.bekannt[i.ID] = true
		b.stand[i.ID] = i.UpdatedAt
		if i.Parent != "" {
			b.hatKinder[i.Parent] = true
		}
	}
	return b, true
}

// beadsChildrenTree steigt von wurzeln aus so lange weiter, bis keine neuen
// Kinder mehr kommen, und gibt die Hierarchie flach zurueck: Elternteil ->
// direkte Kinder. Den Baum daraus baut beadsBaum.
//
// Ein Aufruf je EBENE ueber alle Ids dieser Ebene — die Zahl der bd-Aufrufe
// waechst mit der Tiefe, nicht mit der Zahl der Beads. Derselbe Grund wie bei
// beadsChildrenBatch (gkp-1uu), nur eine Dimension weiter: `bd show` kostet je
// Id (gemessen 410 ms fuer eine, 6620 ms fuer 62), aber jeder Aufruf kostet
// zusaetzlich seinen Start. Die 62 Kinder einer Ebene einzeln zu fragen waeren
// 62 x 410 ms = 25 s statt 6,6 s.
//
// gesehen ist der Zyklusschutz der Ladeschleife. bd sollte keinen Zyklus
// erzeugen, aber der Collector darf sich nicht darauf verlassen: eine Id, die
// schon einmal in einer Ebene stand, wird nie erneut abgestiegen — damit endet
// die Schleife auch bei A -> B -> A. Der zweite Schutz sitzt im Aufbau
// (beadsBaum), weil die flache Karte einen Zyklus weiterhin enthaelt.
//
// Der Bestand wird MITGEGEBEN und nicht mehr selbst geholt (gkp-mnd): er ist
// dieselbe Antwort, aus der CollectBeads schon seine Wurzeln abgeleitet hat,
// und ihn hier ein zweites Mal zu lesen waere ein zweiter Datenbankstart fuer
// Daten, die der Aufrufer in der Hand haelt. Der Nullwert bleibt erlaubt und
// heisst weiterhin "keine Abkuerzung": dann steigt jeder Zweig blind ab.
func beadsChildrenTree(repo string, wurzeln []string, bestand bdBestand) map[string][]BeadsSubBead {
	kinder := map[string][]BeadsSubBead{}
	if len(wurzeln) == 0 {
		return kinder
	}

	gesehen := make(map[string]bool, len(wurzeln))
	ebene := make([]string, 0, len(wurzeln))
	for _, id := range wurzeln {
		if id == "" || gesehen[id] {
			continue
		}
		gesehen[id] = true
		ebene = append(ebene, id)
	}
	// Die Wurzeln werden NICHT am Bestand vorbeigefiltert: das ist der Aufruf,
	// den es vor gkp-1lp ohnehin gab, und eine Karte, deren Kinder an einer
	// Abkuerzung haengen, waere ein schlechter Tausch.
	for len(ebene) > 0 {
		for id, cs := range beadsChildrenBatch(repo, ebene) {
			kinder[id] = cs
		}
		var naechste []string
		for _, eltern := range ebene {
			for _, c := range kinder[eltern] {
				if gesehen[c.ID] {
					continue
				}
				gesehen[c.ID] = true
				if bestand.kannKinderHaben(c.ID) {
					naechste = append(naechste, c.ID)
				}
			}
		}
		ebene = naechste
	}
	return kinder
}

// beadsBaum baut aus der flachen Karte den Teilbaum unter id und setzt dabei
// die Startklar-Markierung — in jeder Tiefe, nicht nur auf der ersten Ebene.
//
// weg sind die Ids auf dem Weg von der Wurzel hierher. Ein Kind, das schon auf
// diesem Weg steht, wird uebersprungen: sonst liefe der Aufbau bei einem Zyklus
// endlos. Verloren geht dabei kein Bead — er steht weiter oben im selben Ast —,
// sondern nur die Rueckkante.
func beadsBaum(kinder map[string][]BeadsSubBead, id string, ready map[string]bool, weg map[string]bool) []BeadsSubBead {
	direkt := kinder[id]
	out := make([]BeadsSubBead, 0, len(direkt))
	weg[id] = true
	defer delete(weg, id)
	for _, c := range direkt {
		if weg[c.ID] {
			continue
		}
		c.Ready = ready[c.ID]
		c.Children = beadsBaum(kinder, c.ID, ready, weg)
		out = append(out, c)
	}
	return out
}

// RenderDotSVG pipes dotSrc through `dot -Tsvg`. Fails open: a missing dot
// binary, a non-zero exit or the deadline all yield ok=false, never a panic.
//
// The deadline is not for dot's own speed — it renders in 26–43 ms — but for
// the pipe: dot reads stdin to EOF, and a wedged child would otherwise hold
// the request open forever (gkp-80s). Written with exec directly rather than
// through shared.Run because this is the one read-path call that needs stdin.
//
// Exported since gkp-3sq: the maps overview (appctx.buildMapsOverview) emits
// its own DOT instead of taking it from `bd graph`, but must reach graphviz
// through the same guarded call — a second exec.Command with its own timeout
// handling is exactly how one of the two ends up without a WaitDelay.
func RenderDotSVG(repo, dotSrc string) (string, bool) {
	if strings.TrimSpace(dotSrc) == "" {
		return "", false
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(shared.DotTimeoutMs)*time.Millisecond)
	defer cancel()

	cmd := exec.CommandContext(ctx, "dot", "-Tsvg")
	cmd.WaitDelay = shared.WaitDelay // without it the deadline does not bind — see shared.WaitDelay
	cmd.Dir = repo
	cmd.Stdin = strings.NewReader(dotSrc)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return "", false
	}
	return stdout.String(), true
}

// dotNodeLabel matches the opening of a bd graph node line, capturing the node
// id: `  "gkp-ar7.10" [label="…`. It deliberately stops at `label="` — the
// label body must NOT be matched by a regex, because bd escapes quotes inside
// it (gkp-ar7.9's title carries a literal --host "") and `[^"]*` would cut the
// label short there. dotLabelEnd walks the escapes instead.
var dotNodeLabel = regexp.MustCompile(`^(\s*)"([^"]+)"( \[label=")`)

// dotLabelEnd returns the index of the DOT string's closing quote, starting
// from the first character of the label body, honouring backslash escapes.
// Returns -1 if the string is unterminated (never in bd output, but a
// truncated pipe must not panic).
func dotLabelEnd(s string) int {
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++ // skip whatever is escaped, including \" and \\
		case '"':
			return i
		}
	}
	return -1
}

// fmtDotDate shortens an RFC3339 UTC stamp to "2026-09-04 19:31" for a graph
// node. Absolute on purpose, unlike the relative badge in the reports list:
// this SVG is rendered once on the server and cached, so a "2h" baked into it
// would freeze at render time and lie in a tab left open.
func fmtDotDate(at string) string {
	if len(at) < 16 {
		return ""
	}
	return at[:10] + " " + at[11:16]
}

// annotateDotDates appends each issue's last-change stamp as an extra label
// line, keyed by node id. Nodes without a known stamp are left untouched
// rather than annotated with a blank line.
func annotateDotDates(dot string, dates map[string]string) string {
	if len(dates) == 0 {
		return dot
	}
	lines := strings.Split(dot, "\n")
	for i, line := range lines {
		m := dotNodeLabel.FindStringSubmatchIndex(line)
		if m == nil {
			continue
		}
		id := line[m[4]:m[5]]
		stamp := fmtDotDate(dates[id])
		if stamp == "" {
			continue
		}
		bodyStart := m[7] // just past `[label="`
		end := dotLabelEnd(line[bodyStart:])
		if end < 0 {
			continue
		}
		cut := bodyStart + end
		lines[i] = line[:cut] + `\n` + stamp + line[cut:]
	}
	return strings.Join(lines, "\n")
}

// dotSolidEdge matches a real dependency edge (`"a" -> "b" [style=solid…`).
// bd emits parent→child containment as style=dashed, which is NOT a blocker —
// an epic having open children does not block its siblings.
var dotSolidEdge = regexp.MustCompile(`^\s*"([^"]+)" -> "([^"]+)" \[style=solid`)

// glyph constants as bd emits them in the node label's first rune.
const (
	glyphOpen     = "○"
	glyphProgress = "◐"
	glyphBlocked  = "●"
	glyphClosed   = "✓"
)

// blockedFill is the node colour for a derived-blocked node, in the same
// family as bd's own palette (closed #d4edda, open #e8f4fd, wip #fff3cd).
const blockedFill = `fillcolor="#f8d7da"`

// markDotBlocked rewrites open nodes that have an unfinished dependency to the
// blocked glyph and colour.
//
// bd DOES respect blocking structurally — it lays the graph out in layers and
// draws dependencies as solid edges — but it does NOT mark the nodes: `bd
// graph --dot` colours by the issue's STORED status, and "blocked" is a
// DERIVED state (open + at least one unclosed dependency), so a blocked bead
// renders as plain open. `bd graph --help` advertises a ● blocked icon, but
// only the terminal renderers reach it.
//
// The state is derived from the DOT itself rather than from `bd blocked
// --json`: the graph already carries both the edges and every node's status,
// so this costs no extra subprocess and cannot disagree with the picture it
// annotates. in_progress nodes are left alone on purpose — someone is working
// on them, and overwriting ◐ with ● would drop that.
func markDotBlocked(dot string) string {
	lines := strings.Split(dot, "\n")

	status := map[string]string{}
	for _, line := range lines {
		if m := dotNodeLabel.FindStringSubmatchIndex(line); m != nil {
			body := line[m[7]:]
			status[line[m[4]:m[5]]] = firstGlyph(body)
		}
	}
	blocked := map[string]bool{}
	for _, line := range lines {
		m := dotSolidEdge.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		from, to := m[1], m[2]
		if status[to] == glyphOpen && status[from] != glyphClosed {
			blocked[to] = true
		}
	}
	if len(blocked) == 0 {
		return dot
	}
	for i, line := range lines {
		m := dotNodeLabel.FindStringSubmatchIndex(line)
		if m == nil || !blocked[line[m[4]:m[5]]] {
			continue
		}
		head, tail := line[:m[7]], line[m[7]:]
		lines[i] = head + strings.Replace(tail, glyphOpen, glyphBlocked, 1)
		lines[i] = replaceFill(lines[i], blockedFill)
	}
	return strings.Join(lines, "\n")
}

// firstGlyph returns the leading status rune of a label body, or "".
func firstGlyph(body string) string {
	for _, g := range []string{glyphOpen, glyphProgress, glyphBlocked, glyphClosed} {
		if strings.HasPrefix(body, g) {
			return g
		}
	}
	return ""
}

// replaceFill swaps an existing fillcolor="…" attribute for want, leaving a
// line without one untouched (never appending — the attribute list belongs to
// bd's format, and guessing where to insert would risk malformed DOT).
func replaceFill(line, want string) string {
	i := strings.Index(line, `fillcolor="`)
	if i < 0 {
		return line
	}
	rest := line[i+len(`fillcolor="`):]
	j := strings.IndexByte(rest, '"')
	if j < 0 {
		return line
	}
	return line[:i] + want + rest[j+1:]
}

// beadsPlanSVG runs `bd graph --dot <mapID> | dot -Tsvg`, adding each node's
// last-change stamp and marking derived-blocked nodes (bd graph has no flag
// for either). Returns "" (never an error) if any step fails, matching the
// fail-open contract of the other beads lookups.
func beadsPlanSVG(repo, mapID string, dates map[string]string) string {
	dot, ok := bdRun(repo, "graph", "--dot", mapID)
	if !ok {
		return ""
	}
	svg, ok := RenderDotSVG(repo, markDotBlocked(annotateDotDates(dot, dates)))
	if !ok {
		return ""
	}
	return svg
}

// ---------------------------------------------------------------- gkp-3k6

// planSVGTTL is deliberately longer than the project-context TTL. The SVG
// depends on bd data alone — not on the report store — so reportsFingerprint,
// which forces a context recompute on every report write, has no business
// invalidating it. Decoupling the two is the point of the separate cache: a
// report write used to drag `bd graph` along behind it.
const planSVGTTL = 30 * time.Minute

type planSVGEntry struct {
	at  time.Time
	svg string
}

// planSVGCache is keyed by repo root + "\x00" + mapID, so a subdirectory and a
// worktree of the same checkout share the answer (same reasoning as ghCache).
var planSVGCache sync.Map

// PlanSVGCached renders one map's plan graph, behind its own TTL.
//
// This used to run inside CollectBeads, once per epic, in the request that
// opens a project — and it was 78-80 % of that request. Measured with vs.
// without: infra 26671 -> 5437 ms, gkp 4374 -> 879 ms, orbit 3155 -> 1140
// ms. The suspect was the wrong one: `dot` costs 26-43 ms per call, the time
// is all in `bd graph --dot` (2765-2871 ms per call), and it is in bd's data
// loading, not its DOT emitter — --dot 3955, --compact 3915, --box 3855,
// default 3260 ms. There is no cheaper route to the same data, so the fix is
// not to make it faster but to take it out of the path that has to answer.
//
// Die Datumsbeschriftung kam frueher aus zwei Aufrufen: `bd show <mapID>
// --children` fuer die Kinder und `bd list -t epic` fuer das Epic selbst. Beide
// gehen seit gkp-1lp in einem auf, dem Bestand (bdBestandLesen) — und der
// beantwortet nebenbei zwei alte Luecken: `bd graph --dot` zeichnet die
// Zusammenhangskomponente, also auch ENKEL und fremde Beads, und fuer die gab
// es vorher gar kein Datum. Ein Knoten ohne Stempel bleibt weiterhin
// unbeschriftet statt falsch beschriftet (annotateDotDates laesst ihn in Ruhe).
//
// Fails open like everything else here: "" means the view shows its existing
// "kein plan.svg" placeholder, never an error page.
func PlanSVGCached(cwd, mapID string) string {
	if mapID == "" {
		return ""
	}
	repo := RepoRoot(cwd)
	key := repo + "\x00" + mapID
	if v, ok := planSVGCache.Load(key); ok {
		e := v.(planSVGEntry)
		if time.Since(e.at) < planSVGTTL {
			return e.svg
		}
	}

	// Ein Aufruf fuer alle Knoten des Graphen, und er ist memoisiert: die
	// Ansicht holt die Graphen nacheinander, einer je Karte, und der Bestand
	// aendert sich zwischen zwei Graphen derselben Seite nicht. Gemessen an
	// infra kostete allein die alte Epic-Liste 1403 ms — zwoelfmal waeren
	// das 17 s fuer zwoelf Beschriftungen. Die Antwort speist nur SVGs, die
	// ohnehin planSVGTTL leben, und darf deshalb deren Lebensdauer teilen.
	svg := beadsPlanSVG(repo, mapID, bestandCached(repo).stand)
	planSVGCache.Store(key, planSVGEntry{at: time.Now(), svg: svg})
	return svg
}

type bestandEntry struct {
	at      time.Time
	bestand bdBestand
}

var bestandCache sync.Map // repo root -> bestandEntry

// bestandCached ist bdBestandLesen, memoisiert je Repo fuer planSVGTTL. NUR
// fuer die Plan-Graphen: CollectBeads holt den Bestand jedes Mal frisch, weil
// er dort ueber die Vollstaendigkeit der Hierarchie mitentscheidet und ein 30
// Minuten alter Stand einen neuen Bead unsichtbar machen wuerde.
//
// Ein Fehlschlag cacht nichts und liefert den Nullwert: der einzige Effekt ist
// ein Graph ohne Datumszeilen — dieselbe fail-open-Haltung wie im Rest der
// Datei.
func bestandCached(repo string) bdBestand {
	if v, ok := bestandCache.Load(repo); ok {
		e := v.(bestandEntry)
		if time.Since(e.at) < planSVGTTL {
			return e.bestand
		}
	}
	bestand, ok := bdBestandLesen(repo)
	if !ok {
		return bdBestand{}
	}
	bestandCache.Store(repo, bestandEntry{at: time.Now(), bestand: bestand})
	return bestand
}

// InvalidatePlanSVG drops every cached SVG for a repo. Used by the tests; the
// live path relies on planSVGTTL, because nothing in this process learns about
// a bd write from outside it (gkp-ar7.19 covers that gap for the context
// cache, and the same answer will cover this one).
func InvalidatePlanSVG(cwd string) {
	repo := RepoRoot(cwd)
	bestandCache.Delete(repo)
	prefix := repo + "\x00"
	planSVGCache.Range(func(k, _ any) bool {
		if s, ok := k.(string); ok && strings.HasPrefix(s, prefix) {
			planSVGCache.Delete(k)
		}
		return true
	})
}

// CollectBeads reports the repo's worktrees plus EVERY epic it has — maps
// drawn by /wayfinder and plain epics alike, each flagged by IsMap so the view
// can tell them apart — with its sub-beads. The plan graph is NOT part of this
// (gkp-3k6): BeadsMap.PlanSVG stays empty and the view fetches it per map from
// /plan-svg, so opening a project never waits on `bd graph`. Fails open
// on any missing binary or failed subprocess: Available stays true once
// .beads/ is found, Error is set, Maps/Worktrees stay whatever was collected
// so far — never a panic, never an aborted page.
func CollectBeads(cwd string) BeadsRepo {
	repo := RepoRoot(cwd)
	result := BeadsRepo{Worktrees: []BeadsWorktree{}, Maps: []BeadsMap{}}
	if _, err := os.Stat(filepath.Join(repo, ".beads")); err != nil {
		result.Error = "kein .beads/ in diesem Repo"
		return result
	}
	result.Available = true
	result.Name, result.Path = repoIdentity(repo)
	result.Worktrees = beadsWorktrees(repo)

	// EIN Listenaufruf fuer alles (gkp-mnd). Vorher waren es vier — offene
	// Epics, geschlossene Epics, lose Beads, Bestand —, und die drei ersten sind
	// Teilmengen des vierten. Gemessen am 2026-09-14 mit einem protokollierenden
	// bd-Wrapper: die vier kosteten in git-planner 288+412+281+285 =
	// 1266 ms von 1952 ms, in infra 244+232+233+255 = 964 ms von 2254 ms.
	// Was daran teuer ist, ist nicht die Antwort, sondern der Start — bd oeffnet
	// je Aufruf seine Dolt-Datenbank, und das kostet dasselbe fuer zwei Zeilen
	// wie fuer zweihundert.
	bestand, ok := bdBestandLesen(repo)
	if !ok {
		result.Error = "bd list fehlgeschlagen (fehlt bd?)"
		return result
	}
	epics := bestand.offeneEpics()

	// Die Rueckschau steht in derselben Antwort, weil `--status all` sie
	// mitbringt — `bd list -t epic` blendet geschlossene aus (gkp-ar7.27).
	// Angehaengt statt vorangestellt: die Reihenfolge in Maps entscheidet
	// nichts, sortMaps in appctx ordnet ohnehin nach letzter Aenderung.
	//
	// Die frueher noetige Doppelpruefung entfaellt: die beiden Listen sind jetzt
	// per Konstruktion disjunkt (status == closed gegen status != closed), statt
	// sich auf bds Voreinstellung zu verlassen.
	epics = append(epics, bestand.geschlosseneEpics(closedEpicsMax)...)

	// Beads ohne Epic, VOR der ready-Abfrage: sie entscheiden mit, ob die
	// Abfrage ueberhaupt etwas zu markieren hat.
	loose := bestand.loseBeads()

	// One ready query for the whole repo, before the loop — and only when
	// there is something to mark up, since the call costs seconds on a large
	// Dolt DB and has nothing to say about an empty repo.
	//
	// Die losen Beads zaehlen hier mit: haengt das ganze Repo ohne Epic (no epics),
	// waere die Bedingung sonst falsch und die einzige Karte der Seite haette
	// keine Startklar-Markierung.
	//
	// Fails open on its own: if bd cannot answer it, every bead is simply
	// "not marked startable" and the rest of the card still renders. A
	// missing badge is a smaller loss than a missing page.
	var ready map[string]bool
	if len(epics) > 0 || len(loose) > 0 {
		var readyOK bool
		if ready, readyOK = bdReadyIDs(repo); !readyOK && result.Error == "" {
			result.Error = "bd ready fehlgeschlagen — Startklar-Markierung fehlt"
		}
	}

	// Ein `bd show` je EBENE, nicht je Epic und nicht je Bead (gkp-1uu fuer die
	// Buendelung, gkp-1lp fuer die Tiefe). Die losen Beads steigen im selben
	// Zug ab: sie sind Wurzeln wie die Epics, nur ohne Epic darueber.
	wurzeln := make([]string, 0, len(epics)+len(loose))
	for _, e := range epics {
		wurzeln = append(wurzeln, e.ID)
	}
	for _, l := range loose {
		wurzeln = append(wurzeln, l.ID)
	}
	kinder := beadsChildrenTree(repo, wurzeln, bestand)

	for _, e := range epics {
		// Der Baum statt der Liste: beadsBaum setzt die Startklar-Markierung in
		// jeder Tiefe. Die Schleife davor tat das nur auf der ersten Ebene und
		// haette einen startklaren Enkel stumm uebergangen.
		children := beadsBaum(kinder, e.ID, ready, map[string]bool{})
		// PlanSVG stays empty here on purpose (gkp-3k6): rendering it cost
		// 78-80 % of this call, and the view fetches it from /plan-svg once
		// the page is already up. See PlanSVGCached.
		result.Maps = append(result.Maps, BeadsMap{
			ID:        e.ID,
			Title:     e.Title,
			Status:    e.Status,
			Priority:  e.Priority,
			UpdatedAt: e.UpdatedAt,
			ClosedAt:  e.ClosedAt,
			IsMap:     hasMapLabel(e),
			Children:  children,
		})
	}

	// Die Karte fuer lose Beads kommt zuletzt und nur, wenn es welche gibt.
	// Reihenfolge ist gleichgueltig, sortMaps in appctx ordnet nach letzter
	// Aenderung.
	if len(loose) > 0 {
		result.Maps = append(result.Maps, looseMap(loose, ready, kinder))
	}
	return result
}
