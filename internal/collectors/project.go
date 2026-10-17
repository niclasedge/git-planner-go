package collectors

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/niclasedge/git-planner-go/internal/md"
	"github.com/niclasedge/git-planner-go/internal/shared"
)

// RepoRoot resolves the git top-level for cwd, falling back to cwd itself.
func RepoRoot(cwd string) string {
	out, ok := shared.Run([]string{"git", "-C", cwd, "rev-parse", "--show-toplevel"}, shared.RunOptions{Cwd: cwd, TimeoutMs: shared.GitTimeoutMs})
	if ok {
		return strings.TrimSpace(out)
	}
	return cwd
}

// KnowledgeEntry is one line of a knowledge/README.md index.
type KnowledgeEntry struct {
	Title string `json:"title"`
	Path  string `json:"path"`
	Desc  string `json:"desc"`
}

var linkRe = regexp.MustCompile(`^\s*-\s*\[([^\]]+)\]\(([^)]+)\)\s*(?:—|--)?\s*(.*)\s*$`)

func knowledgeIndex(repo string) []KnowledgeEntry {
	text, ok := shared.ReadText(filepath.Join(repo, "knowledge", "README.md"))
	if !ok {
		return []KnowledgeEntry{}
	}
	entries := []KnowledgeEntry{}
	for _, line := range strings.Split(text, "\n") {
		m := linkRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		path := strings.TrimSpace(m[2])
		if strings.Contains(path, "<") || strings.Contains(path, ">") || strings.Contains(path, "://") {
			continue
		}
		entries = append(entries, KnowledgeEntry{
			Title: strings.TrimSpace(m[1]),
			Path:  path,
			Desc:  strings.TrimSpace(m[3]),
		})
	}
	return entries
}

// countHooksInSettings sums all hook entries across a settings JSON blob.
func countHooksInSettings(text string) int {
	var raw map[string]any
	if json.Unmarshal([]byte(text), &raw) != nil {
		return 0
	}
	hooks, ok := raw["hooks"].(map[string]any)
	if !ok {
		return 0
	}
	n := 0
	for _, matchers := range hooks {
		arr, ok := matchers.([]any)
		if !ok {
			continue
		}
		for _, matcher := range arr {
			mm, ok := matcher.(map[string]any)
			if !ok {
				continue
			}
			inner, ok := mm["hooks"].([]any)
			if !ok {
				continue
			}
			for _, h := range inner {
				if _, ok := h.(map[string]any); ok {
					n++
				}
			}
		}
	}
	return n
}

// CollectProject inspects the cwd repo: name, branch, CLAUDE.md headers,
// knowledge/ index, and project-local .claude/ counts.
func CollectProject(cwd string) map[string]any {
	repo := RepoRoot(cwd)
	branch, branchOk := shared.Run([]string{"git", "-C", cwd, "rev-parse", "--abbrev-ref", "HEAD"}, shared.RunOptions{Cwd: cwd, TimeoutMs: shared.GitTimeoutMs})
	data := map[string]any{
		"available": true,
		"repo":      lastPathSegment(repo),
		"repo_path": repo,
		"branch":    nil,
	}
	if branchOk {
		data["branch"] = strings.TrimSpace(branch)
	}

	claudeMdPath := filepath.Join(repo, "CLAUDE.md")
	if text, ok := shared.ReadText(claudeMdPath); ok {
		var size int64
		if info, err := os.Stat(claudeMdPath); err == nil {
			size = info.Size()
		}
		data["claude_md_headers"] = sliceUpTo(md.MdHeaders(text, []int{1, 2}), 20)
		data["claude_md_tokens"] = md.EstTokens(text)
		data["claude_md_size"] = size
	} else {
		data["claude_md_headers"] = []string{}
		data["claude_md_tokens"] = 0
		data["claude_md_size"] = 0
	}

	data["knowledge_index"] = knowledgeIndex(repo)

	projClaudeDir := filepath.Join(repo, ".claude")
	data["proj_skills_count"] = countSubdirs(filepath.Join(projClaudeDir, "skills"))
	data["proj_agents_count"] = countMdFiles(filepath.Join(projClaudeDir, "agents"))

	hooks := 0
	if t, ok := shared.ReadText(filepath.Join(projClaudeDir, "settings.json")); ok && t != "" {
		hooks += countHooksInSettings(t)
	}
	if t, ok := shared.ReadText(filepath.Join(projClaudeDir, "settings.local.json")); ok && t != "" {
		hooks += countHooksInSettings(t)
	}
	data["proj_hooks_count"] = hooks

	return data
}

func countSubdirs(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if e.IsDir() {
			n++
		}
	}
	return n
}

func countMdFiles(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			n++
		}
	}
	return n
}

// sliceUpTo returns at most n elements (mirrors Array.slice(0, n)).
func sliceUpTo[T any](s []T, n int) []T {
	if len(s) > n {
		return s[:n]
	}
	return s
}
