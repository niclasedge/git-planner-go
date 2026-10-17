package collectors

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ProjectRoots returns the scan roots: CC_KNOWLEDGE_ROOTS (comma-separated)
// overrides; default ~/git.
func ProjectRoots() []string {
	if env := os.Getenv("CC_KNOWLEDGE_ROOTS"); env != "" {
		parts := strings.Split(env, ",")
		out := []string{}
		for _, p := range parts {
			if t := strings.TrimSpace(p); t != "" {
				out = append(out, t)
			}
		}
		return out
	}
	return []string{filepath.Join(Home(), "git")}
}

// isProjectDir decides membership (gkp-y2d): a directory counts as a project
// when it has .beads/ OR a resolvable GitHub origin remote. Both sources of
// work run side by side and uncoupled (E1), and they are repo-disjoint in
// practice — a criterion of .beads/ alone hides every GitHub task in a repo
// without beads.
//
// The order of the tests is the price list, measured over ~/Work on this
// machine (6 directories, warm):
//
//	os.Stat(".beads")                      ~1 µs
//	os.Stat(".git")                        ~1 µs
//	git rev-parse + git remote get-url     ~2.3 ms   (two subprocesses)
//	ssh -G <alias> inside GhOriginRemote   ~26 ms    (only for alias hosts)
//
// So the expensive path is walked only by directories that failed the cheap
// one: a repo with .beads/ never pays for it. The .git probe is not redundant
// either — `git -C` walks upwards, so a plain directory inside a repo would
// otherwise inherit that repo's origin and be listed as a project of its own,
// after paying for two subprocesses to get there.
func isProjectDir(dir string) bool {
	if _, err := os.Stat(filepath.Join(dir, ".beads")); err == nil {
		return true
	}
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		return false // not a repo of its own -> no remote to find
	}
	_, ok := GhOriginRemote(dir)
	return ok
}

// DiscoverProjectsIn scans roots for project directories (see isProjectDir).
// Dedupe by name; the active repo is always included.
func DiscoverProjectsIn(roots []string, activeRepo string) []ProjectRef {
	found := map[string]string{}
	order := []string{}
	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue // root not readable
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			full := filepath.Join(root, e.Name())
			if _, ok := found[e.Name()]; ok {
				continue // dedupe by name: the first root wins, no probing twice
			}
			if !isProjectDir(full) {
				continue // neither .beads/ nor a resolvable origin remote
			}
			found[e.Name()] = full
			order = append(order, e.Name())
		}
	}
	activeName := lastPathSegment(activeRepo)
	if _, ok := found[activeName]; !ok {
		found[activeName] = activeRepo
		order = append(order, activeName)
	}
	names := make([]string, len(order))
	copy(names, order)
	sort.Slice(names, func(i, j int) bool { return localeLess(names[i], names[j]) })
	out := make([]ProjectRef, 0, len(names))
	for _, name := range names {
		out = append(out, ProjectRef{Name: name, Path: found[name]})
	}
	return out
}

// lastPathSegment mirrors `path.split("/").pop()` (the final non-empty? no — JS
// pop returns the last element even if empty; here paths have no trailing slash).
func lastPathSegment(p string) string {
	parts := strings.Split(p, "/")
	return parts[len(parts)-1]
}

// CollectSidebar builds the sidebar aggregate: every project with its 7d cost.
func CollectSidebar(activeRepo string) []SidebarProject {
	projects := DiscoverProjectsIn(ProjectRoots(), activeRepo)
	out := make([]SidebarProject, 0, len(projects))
	for _, p := range projects {
		out = append(out, SidebarProject{
			ProjectRef: p,
			Cost7d:     SevenDayCostNative(p.Path),
		})
	}
	return out
}
