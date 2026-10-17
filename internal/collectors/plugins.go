package collectors

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/niclasedge/git-planner-go/internal/md"
	"github.com/niclasedge/git-planner-go/internal/shared"
)

// PluginItem is a skill/agent entry from a plugin or project, with a source group.
type PluginItem struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	MetaTokens  int    `json:"meta_tokens"`
	Group       string `json:"group"`
	Read        string `json:"read"`
	ReadPath    string `json:"readPath,omitempty"`
}

// readEnabledPlugins reads enabledPlugins keys (only true values) from settings.json.
func readEnabledPlugins(claudeHome string) []string {
	text, ok := shared.ReadText(filepath.Join(claudeHome, "settings.json"))
	if !ok || text == "" {
		return nil
	}
	var raw map[string]any
	if json.Unmarshal([]byte(text), &raw) != nil {
		return nil
	}
	ep, _ := raw["enabledPlugins"].(map[string]any)
	enabled := []string{}
	for k, v := range ep {
		if b, ok := v.(bool); ok && b {
			enabled = append(enabled, k)
		}
	}
	sort.Strings(enabled) // deterministic order (JS used Set insertion order)
	return enabled
}

// readInstallPaths reads installPath for all known plugins from installed_plugins.json.
func readInstallPaths(claudeHome string) map[string]string {
	text, ok := shared.ReadText(filepath.Join(claudeHome, "plugins", "installed_plugins.json"))
	if !ok || text == "" {
		return map[string]string{}
	}
	var raw map[string]any
	if json.Unmarshal([]byte(text), &raw) != nil {
		return map[string]string{}
	}
	plugins, _ := raw["plugins"].(map[string]any)
	paths := map[string]string{}
	for key, entriesAny := range plugins {
		entries, ok := entriesAny.([]any)
		if !ok || len(entries) == 0 {
			continue
		}
		first, ok := entries[0].(map[string]any)
		if !ok {
			continue
		}
		ip := asStr(first["installPath"])
		if ip != "" {
			paths[key] = ip
		}
	}
	return paths
}

func readSkillMeta(skillDir, skillName, group string) (PluginItem, bool) {
	text, ok := shared.ReadText(filepath.Join(skillDir, "SKILL.md"))
	if !ok {
		return PluginItem{}, false
	}
	description := md.FrontmatterField(text, "description")
	return PluginItem{
		Name:        skillName,
		Description: description,
		MetaTokens:  CtxTok(md.EstTokens(skillName + "\n" + description)),
		Group:       group,
		Read:        "homefile",
		ReadPath:    filepath.Join(skillDir, "SKILL.md"),
	}, true
}

func readAgentMeta(agentMdPath, agentName, group, read string) (PluginItem, bool) {
	text, ok := shared.ReadText(agentMdPath)
	if !ok {
		return PluginItem{}, false
	}
	description := md.FrontmatterField(text, "description")
	return PluginItem{
		Name:        agentName,
		Description: description,
		MetaTokens:  CtxTok(md.EstTokens(agentName + "\n" + description)),
		Group:       group,
		Read:        read,
		ReadPath:    agentMdPath,
	}, true
}

// PluginItems holds plugin skills + agents (mirrors the TS {skills, agents}).
type PluginItems struct {
	Skills []PluginItem `json:"skills"`
	Agents []PluginItem `json:"agents"`
}

// CollectPluginItems gathers plugin skills and plugin agents from all enabled
// plugins. Returns ONLY plugin entries (User items come from CollectGlobal).
func CollectPluginItems(claudeHome string) PluginItems {
	skills := []PluginItem{}
	agents := []PluginItem{}

	enabled := readEnabledPlugins(claudeHome)
	if len(enabled) == 0 {
		return PluginItems{Skills: skills, Agents: agents}
	}
	installPaths := readInstallPaths(claudeHome)

	for _, pluginKey := range enabled {
		installPath, ok := installPaths[pluginKey]
		if !ok {
			continue
		}
		pluginName := strings.Split(pluginKey, "@")[0]
		group := "Plugin · " + pluginName

		// Plugin-Skills aus <installPath>/skills/<skill-dir>/SKILL.md
		if entries, err := os.ReadDir(filepath.Join(installPath, "skills")); err == nil {
			for _, entry := range entries {
				if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
					continue
				}
				skillDir := filepath.Join(installPath, "skills", entry.Name())
				if item, ok := readSkillMeta(skillDir, entry.Name(), group); ok {
					skills = append(skills, item)
				}
			}
		}

		// Plugin-Agents aus <installPath>/agents/*.md
		if entries, err := os.ReadDir(filepath.Join(installPath, "agents")); err == nil {
			for _, entry := range entries {
				if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
					continue
				}
				name := strings.TrimSuffix(entry.Name(), ".md")
				mdPath := filepath.Join(installPath, "agents", entry.Name())
				if item, ok := readAgentMeta(mdPath, name, group, "homefile"); ok {
					agents = append(agents, item)
				}
			}
		}
	}

	return PluginItems{Skills: skills, Agents: agents}
}

var agentNameWhitelist = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// CollectProjectAgents gathers project agents from <repoRoot>/.claude/agents/*.md.
func CollectProjectAgents(cwd string) []PluginItem {
	agents := []PluginItem{}
	agentsDir := filepath.Join(cwd, ".claude", "agents")
	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		return agents
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ".md")
		if !agentNameWhitelist.MatchString(name) {
			continue
		}
		mdPath := filepath.Join(agentsDir, entry.Name())
		if item, ok := readAgentMeta(mdPath, name, "Project", "project-agent"); ok {
			agents = append(agents, item)
		}
	}
	return agents
}
