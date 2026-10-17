package collectors

import (
	"math"
	"os"
	"path/filepath"

	"github.com/niclasedge/git-planner-go/internal/md"
)

// Fixed, harness-dependent approximations — NOT measurable from the repo.
const (
	SysPromptTok = 3600
	SysToolsTok  = 7900
)

// CtxCalib is the calibration factor for file-based context categories (Skills,
// Agents, Memory): chars/4 underestimates Markdown/German by ~1.7-1.9×. Applied
// ONLY in the context-view aggregation — EstTokens itself stays unchanged.
const CtxCalib = 1.7

// CtxTok applies the calibration factor to an EstTokens value (context view only).
func CtxTok(est int) int {
	return int(math.Round(float64(est) * CtxCalib))
}

// CtxWindow is the Claude Code context window (≈ 1M-token tier).
const CtxWindow = 1_000_000

// ContextItem is a single token-bearing item inside a context category.
type ContextItem struct {
	Name     string `json:"name"`
	Tokens   int    `json:"tokens"`
	Desc     string `json:"desc,omitempty"`
	Read     string `json:"read,omitempty"`
	ReadPath string `json:"readPath,omitempty"`
	Group    string `json:"group,omitempty"`
}

// ContextCategory is a labelled token bucket with optional drill-down items.
type ContextCategory struct {
	Key    string        `json:"key"`
	Label  string        `json:"label"`
	Tokens int           `json:"tokens"`
	Fixed  bool          `json:"fixed,omitempty"`
	Live   bool          `json:"live,omitempty"`
	Items  []ContextItem `json:"items"`
}

// MemoryMd is the result of estimating a project's MEMORY.md tokens.
type MemoryMd struct {
	Available bool   `json:"available"`
	Tokens    int    `json:"tokens"`
	Path      string `json:"path"`
}

// MemoryMdTokens best-effort estimates this project's persistent MEMORY.md.
// CLAUDE_MEMORY_DIR overrides the default per-project memory dir.
func MemoryMdTokens(cwd string) MemoryMd {
	dir := os.Getenv("CLAUDE_MEMORY_DIR")
	if dir == "" {
		dir = filepath.Join(Home(), ".claude", "projects", EncodeCwd(cwd), "memory")
	}
	path := filepath.Join(dir, "MEMORY.md")
	b, err := os.ReadFile(path)
	if err != nil {
		return MemoryMd{Available: false, Tokens: 0, Path: path}
	}
	return MemoryMd{Available: true, Tokens: md.EstTokens(string(b)), Path: path}
}
