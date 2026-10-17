package collectors

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Per-MTok USD rates (Claude Sonnet-4 tier) — kept identical to tokens.go.
const (
	rateInput      = 3.0
	rateOutput     = 15.0
	rateCacheWrite = 3.75
	rateCacheRead  = 0.30
)

// EstCost computes the estimated USD cost from token counts.
func EstCost(inp, out, cacheRead, cacheCreation float64) float64 {
	return (inp*rateInput + out*rateOutput + cacheCreation*rateCacheWrite +
		cacheRead*rateCacheRead) / 1_000_000
}

var nonAlnum = regexp.MustCompile(`[^A-Za-z0-9]`)

// EncodeCwd mirrors how Claude Code encodes a project's cwd into its session dir
// name (non-alnum → '-').
func EncodeCwd(cwd string) string {
	return nonAlnum.ReplaceAllString(cwd, "-")
}

func sessionsDir(cwd string) string {
	base := os.Getenv("CLAUDE_PROJECTS_DIR")
	if base == "" {
		base = filepath.Join(Home(), ".claude", "projects")
	}
	return filepath.Join(base, EncodeCwd(cwd))
}

type sessionUsage struct {
	InputTokens         float64 `json:"input_tokens"`
	OutputTokens        float64 `json:"output_tokens"`
	CacheReadTokens     float64 `json:"cache_read_input_tokens"`
	CacheCreationTokens float64 `json:"cache_creation_input_tokens"`
}

type sessionLine struct {
	Message *struct {
		Usage *sessionUsage `json:"usage"`
	} `json:"message"`
}

// FileCost returns the estimated USD cost from one session JSONL: sum usage
// tokens over its lines.
func FileCost(path string) float64 {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	var inp, out, cr, cc float64
	for _, line := range strings.Split(string(b), "\n") {
		if line == "" {
			continue
		}
		var e sessionLine
		if json.Unmarshal([]byte(line), &e) != nil {
			continue
		}
		if e.Message == nil || e.Message.Usage == nil {
			continue
		}
		u := e.Message.Usage
		inp += u.InputTokens
		out += u.OutputTokens
		cr += u.CacheReadTokens
		cc += u.CacheCreationTokens
	}
	return EstCost(inp, out, cr, cc)
}

// SevenDayCostNative sums the cost of session JSONLs whose file mtime is within
// the last 7 days. No subprocess.
func SevenDayCostNative(cwd string) float64 {
	return sevenDayCostNativeAt(cwd, time.Now().UnixMilli())
}

func sevenDayCostNativeAt(cwd string, nowMs int64) float64 {
	dir := sessionsDir(cwd)
	cutoff := nowMs - 7*24*3600*1000
	var cost float64
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0 // no session dir for this project
	}
	for _, ent := range entries {
		if ent.IsDir() || !strings.HasSuffix(ent.Name(), ".jsonl") {
			continue
		}
		p := filepath.Join(dir, ent.Name())
		info, err := os.Stat(p)
		if err != nil {
			continue
		}
		if info.ModTime().UnixMilli() < cutoff {
			continue
		}
		cost += FileCost(p)
	}
	return math.Round(cost*100) / 100
}
