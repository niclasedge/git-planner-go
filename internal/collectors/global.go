package collectors

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/niclasedge/git-planner-go/internal/md"
	"github.com/niclasedge/git-planner-go/internal/shared"
)

var scriptExts = map[string]bool{
	".py": true, ".sh": true, ".bash": true, ".zsh": true, ".js": true,
	".mjs": true, ".cjs": true, ".ts": true, ".rb": true, ".pl": true, ".lua": true,
}

// ScriptMeta describes one script discovered inside a skill directory.
type ScriptMeta struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
	Lang string `json:"lang"`
}

// SkillMeta is one global skill's metadata.
type SkillMeta struct {
	Name        string       `json:"name"`
	Description string       `json:"description"`
	Tokens      int          `json:"tokens"`
	MetaTokens  int          `json:"meta_tokens"`
	SizeBytes   int64        `json:"size_bytes"`
	HasMd       bool         `json:"has_md"`
	Scripts     []ScriptMeta `json:"scripts"`
}

// AgentMeta is one global agent's metadata.
type AgentMeta struct {
	Name        string `json:"name"`
	Tokens      int    `json:"tokens"`
	MetaTokens  int    `json:"meta_tokens"`
	SizeBytes   int64  `json:"size_bytes"`
	Description string `json:"description"`
}

func scanScripts(root string, limit int) []ScriptMeta {
	out := []ScriptMeta{}
	entries, err := os.ReadDir(root)
	if err != nil {
		return out
	}
	for _, entry := range entries {
		if len(out) >= limit {
			break
		}
		fullPath := filepath.Join(root, entry.Name())
		if entry.IsDir() {
			out = append(out, scanScripts(fullPath, limit-len(out))...)
		} else {
			ext := ""
			if dot := strings.LastIndex(entry.Name(), "."); dot >= 0 {
				ext = entry.Name()[dot:]
			}
			if !scriptExts[strings.ToLower(ext)] {
				continue
			}
			info, err := os.Stat(fullPath)
			if err != nil {
				continue
			}
			rel := fullPath[len(root)+1:]
			out = append(out, ScriptMeta{Path: rel, Size: info.Size(), Lang: ext[1:]})
		}
	}
	return out
}

func skillMeta(skillDir string) SkillMeta {
	name := lastPathSegment(skillDir)
	mdPath := filepath.Join(skillDir, "SKILL.md")
	scripts := scanScripts(skillDir, 40)
	base := SkillMeta{Name: name, Scripts: scripts}
	info, err := os.Stat(mdPath)
	if err != nil {
		return base
	}
	text, ok := shared.ReadText(mdPath)
	if !ok || text == "" {
		return base
	}
	description := md.FrontmatterField(text, "description")
	return SkillMeta{
		Name:        name,
		Description: description,
		Tokens:      md.EstTokens(text),
		MetaTokens:  md.EstTokens(name + "\n" + description),
		SizeBytes:   info.Size(),
		HasMd:       true,
		Scripts:     scripts,
	}
}

func agentMeta(agentMd string) AgentMeta {
	filename := lastPathSegment(agentMd)
	name := strings.TrimSuffix(filename, ".md")
	info, err := os.Stat(agentMd)
	if err != nil {
		return AgentMeta{Name: name}
	}
	text, ok := shared.ReadText(agentMd)
	if !ok || text == "" {
		return AgentMeta{Name: name}
	}
	description := md.FrontmatterField(text, "description")
	return AgentMeta{
		Name:        name,
		Tokens:      md.EstTokens(text),
		MetaTokens:  md.EstTokens(name + "\n" + description),
		SizeBytes:   info.Size(),
		Description: description,
	}
}

var beginCcSetupRe = regexp.MustCompile(`<!--\s*BEGIN toolbox\s*-->`)
var endCcSetupRe = regexp.MustCompile(`<!--\s*END toolbox\s*-->`)

// CollectGlobal inspects ~/.claude: CLAUDE.md, skills/, settings.json hooks
// (keys only, no env values), agents/.
func CollectGlobal(claudeHome string) map[string]any {
	info, err := os.Stat(claudeHome)
	if err != nil || !info.IsDir() {
		return map[string]any{"available": false, "reason": fmt.Sprintf("~/.claude not found: %s", claudeHome)}
	}

	data := map[string]any{"available": true, "home": claudeHome}

	// CLAUDE.md
	claudeMdPath := filepath.Join(claudeHome, "CLAUDE.md")
	if text, ok := shared.ReadText(claudeMdPath); ok {
		var size int64
		if st, err := os.Stat(claudeMdPath); err == nil {
			size = st.Size()
		}
		managed := beginCcSetupRe.MatchString(text) && endCcSetupRe.MatchString(text)
		data["claude_md"] = map[string]any{
			"size_bytes":    size,
			"tokens":        md.EstTokens(text),
			"headers":       md.MdHeaders(text, []int{2}),
			"managed_block": managed,
		}
	} else {
		data["claude_md"] = nil
	}

	// skills/
	skillsDir := filepath.Join(claudeHome, "skills")
	skillDirs := []string{}
	if entries, err := os.ReadDir(skillsDir); err == nil {
		for _, e := range entries {
			if e.IsDir() && !strings.HasPrefix(e.Name(), ".") && !strings.HasPrefix(e.Name(), "_") {
				skillDirs = append(skillDirs, filepath.Join(skillsDir, e.Name()))
			}
		}
		sort.Strings(skillDirs)
	}
	skillItems := make([]SkillMeta, 0, len(skillDirs))
	skillNames := make([]string, 0, len(skillDirs))
	for _, d := range skillDirs {
		skillItems = append(skillItems, skillMeta(d))
		skillNames = append(skillNames, lastPathSegment(d))
	}
	data["skills"] = map[string]any{"count": len(skillNames), "names": skillNames, "items": skillItems}

	// settings.json hooks — event names, matcher, type, command. env NEVER read.
	if settingsText, ok := shared.ReadText(filepath.Join(claudeHome, "settings.json")); ok {
		var raw map[string]any
		if json.Unmarshal([]byte(settingsText), &raw) != nil {
			data["settings"] = map[string]any{"hook_events": map[string]int{}, "hook_detail": map[string]any{}, "parse_error": true}
		} else {
			events := map[string]int{}
			detail := map[string]any{}
			if hooks, ok := raw["hooks"].(map[string]any); ok {
				for eventName, matchers := range hooks {
					n := 0
					entries := []map[string]string{}
					if arr, ok := matchers.([]any); ok {
						for _, matcher := range arr {
							mm, ok := matcher.(map[string]any)
							if !ok {
								continue
							}
							pattern := asStr(mm["matcher"])
							inner, ok := mm["hooks"].([]any)
							if !ok {
								continue
							}
							for _, h := range inner {
								hm, ok := h.(map[string]any)
								if !ok {
									continue
								}
								n++
								entries = append(entries, map[string]string{
									"matcher": pattern,
									"type":    asStr(hm["type"]),
									"command": asStr(hm["command"]),
								})
							}
						}
					}
					events[eventName] = n
					detail[eventName] = entries
				}
			}
			data["settings"] = map[string]any{"hook_events": events, "hook_detail": detail}
		}
	} else {
		data["settings"] = nil
	}

	// agents/*.md
	agentsDir := filepath.Join(claudeHome, "agents")
	agentFiles := []string{}
	if entries, err := os.ReadDir(agentsDir); err == nil {
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
				agentFiles = append(agentFiles, filepath.Join(agentsDir, e.Name()))
			}
		}
		sort.Strings(agentFiles)
	}
	agentNames := make([]string, 0, len(agentFiles))
	agentItems := make([]AgentMeta, 0, len(agentFiles))
	for _, f := range agentFiles {
		agentNames = append(agentNames, strings.TrimSuffix(lastPathSegment(f), ".md"))
		agentItems = append(agentItems, agentMeta(f))
	}
	data["agents"] = agentNames
	data["agent_items"] = agentItems

	return data
}

// asStr mirrors JS String(x ?? "") for the scalar values read from settings.
func asStr(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}
