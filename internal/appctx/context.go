package appctx

import (
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/niclasedge/git-planner-go/internal/collectors"
)

// RepoRoot re-exports collectors.RepoRoot for server.go (mirrors the TS re-export).
func RepoRoot(cwd string) string { return collectors.RepoRoot(cwd) }

// DiscoverProjects discovers projects under the configured roots (or a single
// explicit base, used by tests).
func DiscoverProjects(activeRepo, base string) []collectors.ProjectRef {
	roots := collectors.ProjectRoots()
	if base != "" {
		roots = []string{base}
	}
	return collectors.DiscoverProjectsIn(roots, activeRepo)
}

// ResolveProjectCwd resolves a project name to its path, falling back to defaultCwd.
func ResolveProjectCwd(project string, projects []collectors.ProjectRef, defaultCwd string) string {
	if project != "" {
		for _, p := range projects {
			if p.Name == project {
				return p.Path
			}
		}
	}
	return defaultCwd
}

// BuildOpts mirrors the optional bag passed to buildContext.
type BuildOpts struct {
	Projects      []collectors.SidebarProject
	ActiveProject string
	Global        map[string]any // nil → collect live
	SkipProject   bool
}

// BuildContext assembles the per-request context (mirrors build_context).
func BuildContext(cwd, claudeHome string, opts BuildOpts) map[string]any {
	home := claudeHome
	if home == "" {
		home = collectors.Home() + "/.claude"
	}
	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	skip := opts.SkipProject
	na := map[string]any{"available": false, "reason": "overview"}

	pluginItems := collectors.CollectPluginItems(home)
	projectAgents := []collectors.PluginItem{}
	if !skip {
		projectAgents = collectors.CollectProjectAgents(collectors.RepoRoot(cwd))
	}

	var global any
	if opts.Global != nil {
		global = opts.Global
	} else {
		global = collectors.CollectGlobal(home)
	}

	cards := map[string]any{
		"global":    global,
		"cost":      collectors.CollectCost(""),
		"project":   anyOr(skip, na, func() any { return collectors.CollectProject(cwd) }),
		"git":       anyOr(skip, na, func() any { return collectors.CollectGit(cwd) }),
		"knowledge": anyOr(skip, na, func() any { return collectors.CollectKnowledge(cwd, "") }),
		"tokens":    anyOr(skip, na, func() any { return collectors.CollectTokens(cwd) }),
		"memory_md": anyOr(skip, na, func() any { return collectors.MemoryMdTokens(cwd) }),
		// demo-axk.4: beads maps + plan.svg + report-store entries.
		// Subprocess- and file-heavy (git worktree list, bd list/show/graph,
		// dot, JSONL reads) — computed here so it rides the same
		// per-project TTL cache (cache.GetProjectContext) as every other
		// card, instead of re-running on every page render.
		"reports": anyOr(skip, na, func() any { return CollectReportsData(cwd) }),
	}

	// projects/plugin slices stored as their typed forms; the dyn round-trip in
	// BuildData normalizes them for navigation.
	return map[string]any{
		"generated_at":   now,
		"cwd":            cwd,
		"projects":       opts.Projects,
		"active_project": opts.ActiveProject,
		"plugin_skills":  pluginItems.Skills,
		"plugin_agents":  pluginItems.Agents,
		"project_agents": projectAgents,
		"cards":          cards,
	}
}

// anyOr returns na when skip, else the live collection.
func anyOr(skip bool, na map[string]any, collect func() any) any {
	if skip {
		return na
	}
	return collect()
}

// FmtCompact formats a number as a compact string (1.2k / 3.4M / 5.6B).
func FmtCompact(n any) string {
	v, ok := parseFloatJS(n)
	if !ok || math.IsNaN(v) {
		return "0"
	}
	switch {
	case v >= 1e9:
		return strconv.FormatFloat(v/1e9, 'f', 2, 64) + "B"
	case v >= 1e6:
		return strconv.FormatFloat(v/1e6, 'f', 1, 64) + "M"
	case v >= 1e3:
		return strconv.FormatFloat(v/1e3, 'f', 1, 64) + "k"
	default:
		return strconv.FormatInt(int64(math.Floor(v)), 10)
	}
}

// FmtCost formats a USD value ($0.00 / $1.23k).
func FmtCost(v any) string {
	n, ok := parseFloatJS(v)
	if !ok || math.IsNaN(n) {
		return "$0.00"
	}
	if n >= 1000 {
		return "$" + strconv.FormatFloat(n/1000, 'f', 2, 64) + "k"
	}
	return "$" + strconv.FormatFloat(n, 'f', 2, 64)
}

// FmtCostRund formatiert einen USD-Wert ohne Nachkommastellen ($145).
//
// Fuer die Projekt-Rail: dort steht die Zahl gedreht neben dem Namen und wird
// nur ueberflogen ("kostet das Projekt viel oder wenig"), nie abgelesen. Cent
// kosten dort zwei Zeichen Hoehe und tragen nichts bei. Die k-Form von FmtCost
// entfaellt bewusst mit — "$1234" ist genauso kurz wie "$1.23k" und exakt.
func FmtCostRund(v any) string {
	n, ok := parseFloatJS(v)
	if !ok || math.IsNaN(n) {
		return "$0"
	}
	return "$" + strconv.FormatInt(int64(math.Round(n)), 10)
}

// parseFloatJS mirrors parseFloat(String(v)) for the values this app passes
// (numbers or clean numeric strings). Returns ok=false on non-numeric input.
func parseFloatJS(v any) (float64, bool) {
	if f, ok := v.(float64); ok {
		return f, true
	}
	s := strings.TrimSpace(jsString(v))
	if s == "" {
		return 0, false
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return f, true
}
