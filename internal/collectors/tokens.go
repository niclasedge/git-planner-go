package collectors

import (
	"bytes"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/niclasedge/git-planner-go/internal/md"
)

// REPEATED_CMD_THRESHOLD: a Bash command must reach this to count as "repeated".
const repeatedCmdThreshold = 3

// orderedIntObj marshals to a JSON object preserving the slice order — used for
// tool_freq, which must stay sorted by count desc (a Go map would sort keys).
type orderedIntObj []struct {
	K string
	V int
}

func (o orderedIntObj) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, kv := range o {
		if i > 0 {
			b.WriteByte(',')
		}
		kb, _ := json.Marshal(kv.K)
		b.Write(kb)
		b.WriteByte(':')
		b.WriteString(strconv.Itoa(kv.V))
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

type usageLine struct {
	input, output, cacheRead, cacheCreation float64
	hasTurn                                 bool
}

type toolUse struct {
	name, command, sessionID string
}

type failedEntry struct {
	tool, command, errorPreview string
}

type parsedSession struct {
	usage         []usageLine
	failedEntries []failedEntry
	toolFreqOrder []string
	toolFreqCount map[string]int
	bashCommands  []string
}

func asMap(v any) map[string]any { m, _ := v.(map[string]any); return m }
func asArr(v any) []any          { a, _ := v.([]any); return a }
func numOf(v any) float64 {
	if f, ok := v.(float64); ok {
		return f
	}
	return 0
}

func parseSessionFile(path, sessionID string, toolUses map[string]toolUse) parsedSession {
	res := parsedSession{toolFreqCount: map[string]int{}}
	b, err := os.ReadFile(path)
	if err != nil {
		return res
	}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var entry map[string]any
		if json.Unmarshal([]byte(line), &entry) != nil {
			continue
		}
		entryType := asStr(entry["type"])
		msg := asMap(entry["message"])

		if entryType == "assistant" && msg != nil {
			u := asMap(msg["usage"])
			hasUsage := u != nil
			res.usage = append(res.usage, usageLine{
				input:         numOf(u["input_tokens"]),
				output:        numOf(u["output_tokens"]),
				cacheRead:     numOf(u["cache_read_input_tokens"]),
				cacheCreation: numOf(u["cache_creation_input_tokens"]),
				hasTurn:       hasUsage,
			})

			for _, item := range asArr(msg["content"]) {
				it := asMap(item)
				if it == nil || asStr(it["type"]) != "tool_use" {
					continue
				}
				tuID := asStr(it["id"])
				toolName := asStr(it["name"])
				inp := asMap(it["input"])
				var cmdVal any = inp["command"]
				if cmdVal == nil {
					cmdVal = inp["file_path"]
				}
				cmd := asStr(cmdVal)

				if tuID != "" {
					toolUses[tuID] = toolUse{name: toolName, command: cmd, sessionID: sessionID}
				}
				if _, seen := res.toolFreqCount[toolName]; !seen {
					res.toolFreqOrder = append(res.toolFreqOrder, toolName)
				}
				res.toolFreqCount[toolName]++

				if toolName == "Bash" && strings.TrimSpace(cmd) != "" {
					res.bashCommands = append(res.bashCommands, strings.TrimSpace(cmd))
				}
			}
		} else if entryType == "user" && msg != nil {
			for _, item := range asArr(msg["content"]) {
				it := asMap(item)
				if it == nil || asStr(it["type"]) != "tool_result" {
					continue
				}
				if b, _ := it["is_error"].(bool); !b {
					continue
				}
				tuID := asStr(it["tool_use_id"])
				tu := toolUses[tuID]
				res.failedEntries = append(res.failedEntries, failedEntry{
					tool:         tu.name,
					command:      tu.command,
					errorPreview: sliceRunes(errorPreview(it["content"]), 300),
				})
			}
		}
	}
	return res
}

func errorPreview(content any) string {
	if arr, ok := content.([]any); ok {
		if len(arr) == 0 {
			return ""
		}
		if m, ok := arr[0].(map[string]any); ok {
			return asStr(m["text"])
		}
		return asStr(arr[0])
	}
	return asStr(content)
}

func sessionMtimes(dir string) map[string]int64 {
	out := map[string]int64{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		info, err := os.Stat(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		out[strings.TrimSuffix(e.Name(), ".jsonl")] = info.ModTime().UnixMilli()
	}
	return out
}

type sessionStat struct {
	sessionID                               string
	turns                                   int
	input, output, cacheRead, cacheCreation float64
	failed                                  []failedEntry
	mtime                                   int64
}

// CollectTokens reads session JSONLs directly and computes token-stats, errors,
// repeats and tool_freq — native Go reimplementation (no Python).
func CollectTokens(cwd string) map[string]any {
	dir := sessionsDir(cwd)
	mtimes := sessionMtimes(dir)

	entries, err := os.ReadDir(dir)
	if err != nil {
		return map[string]any{"available": true, "last_session": nil, "week": nil, "sessions": []any{}}
	}
	files := []string{}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".jsonl") {
			files = append(files, e.Name())
		}
	}
	if len(files) == 0 {
		return map[string]any{"available": true, "last_session": nil, "week": nil, "sessions": []any{}}
	}

	allFailedCount := 0
	bashCmdCount := map[string]int{}
	bashCmdSessions := map[string]map[string]bool{}
	bashCmdOrder := []string{}
	globalToolFreq := map[string]int{}
	globalToolOrder := []string{}
	toolUses := map[string]toolUse{}
	perSession := []sessionStat{}

	for _, fname := range files {
		sessionID := strings.TrimSuffix(fname, ".jsonl")
		res := parseSessionFile(filepath.Join(dir, fname), sessionID, toolUses)

		var inp, out, cr, cc float64
		turns := 0
		for _, u := range res.usage {
			if u.hasTurn {
				turns++
			}
			inp += u.input
			out += u.output
			cr += u.cacheRead
			cc += u.cacheCreation
		}

		allFailedCount += len(res.failedEntries)

		for _, name := range res.toolFreqOrder {
			if _, seen := globalToolFreq[name]; !seen {
				globalToolOrder = append(globalToolOrder, name)
			}
			globalToolFreq[name] += res.toolFreqCount[name]
		}

		for _, cmd := range res.bashCommands {
			if _, seen := bashCmdCount[cmd]; !seen {
				bashCmdOrder = append(bashCmdOrder, cmd)
				bashCmdSessions[cmd] = map[string]bool{}
			}
			bashCmdCount[cmd]++
			bashCmdSessions[cmd][sessionID] = true
		}

		perSession = append(perSession, sessionStat{
			sessionID:     sessionID,
			turns:         turns,
			input:         inp,
			output:        out,
			cacheRead:     cr,
			cacheCreation: cc,
			failed:        res.failedEntries,
			mtime:         mtimes[sessionID],
		})
	}

	if len(perSession) == 0 {
		return map[string]any{"available": true, "last_session": nil, "week": nil, "sessions": []any{}}
	}

	// repeated_commands: count >= threshold, sorted by count desc (stable on first-seen)
	type repeatedCmd struct {
		command  string
		count    int
		sessions []string
	}
	repeated := []repeatedCmd{}
	for _, cmd := range bashCmdOrder {
		if bashCmdCount[cmd] < repeatedCmdThreshold {
			continue
		}
		sess := []string{}
		for s := range bashCmdSessions[cmd] {
			sess = append(sess, s)
		}
		sort.Strings(sess)
		repeated = append(repeated, repeatedCmd{command: cmd, count: bashCmdCount[cmd], sessions: sess})
	}
	sort.SliceStable(repeated, func(i, j int) bool { return repeated[i].count > repeated[j].count })

	// Last session = newest mtime (first-wins on ties, mirrors reduce with strict >)
	lastStat := perSession[0]
	for _, s := range perSession[1:] {
		if s.mtime > lastStat.mtime {
			lastStat = s
		}
	}
	lastSession := map[string]any{
		"session_id":     sliceStr8(lastStat.sessionID),
		"turns":          lastStat.turns,
		"input":          lastStat.input,
		"output":         lastStat.output,
		"cache_read":     lastStat.cacheRead,
		"cache_creation": lastStat.cacheCreation,
		"cost":           round4(EstCost(lastStat.input, lastStat.output, lastStat.cacheRead, lastStat.cacheCreation)),
	}

	// Rolling 7-day window
	now := time.Now().UnixMilli()
	cutoff := now - 7*24*3600*1000
	var wkIn, wkOut, wkCr, wkCc float64
	wkCount := 0
	for _, s := range perSession {
		if s.mtime < cutoff {
			continue
		}
		wkCount++
		wkIn += s.input
		wkOut += s.output
		wkCr += s.cacheRead
		wkCc += s.cacheCreation
	}
	week := map[string]any{
		"session_count":  wkCount,
		"input":          wkIn,
		"output":         wkOut,
		"cache_read":     wkCr,
		"cache_creation": wkCc,
		"total":          wkIn + wkOut + wkCr + wkCc,
	}

	// sessions[]: sort by mtime desc, take 30
	sortedSessions := make([]sessionStat, len(perSession))
	copy(sortedSessions, perSession)
	sort.SliceStable(sortedSessions, func(i, j int) bool { return sortedSessions[i].mtime > sortedSessions[j].mtime })
	sortedSessions = sliceUpTo(sortedSessions, 30)

	sessions := make([]map[string]any, 0, len(sortedSessions))
	for _, s := range sortedSessions {
		reps := []map[string]any{}
		for _, r := range repeated {
			if containsStr(r.sessions, s.sessionID) {
				reps = append(reps, map[string]any{"command": sliceRunes(r.command, 200), "count": r.count})
			}
			if len(reps) >= 12 {
				break
			}
		}
		errs := []map[string]any{}
		for _, e := range sliceUpTo(s.failed, 12) {
			errs = append(errs, map[string]any{
				"tool":    e.tool,
				"command": sliceRunes(e.command, 200),
				"preview": sliceRunes(e.errorPreview, 240),
			})
		}
		var dateSecs float64
		if s.mtime > 0 {
			dateSecs = float64(s.mtime) / 1000
		}
		sessions = append(sessions, map[string]any{
			"session_id":     sliceStr8(s.sessionID),
			"turns":          s.turns,
			"input":          s.input,
			"output":         s.output,
			"cache_read":     s.cacheRead,
			"cache_creation": s.cacheCreation,
			"total":          s.input + s.output + s.cacheRead + s.cacheCreation,
			"cost":           round4(EstCost(s.input, s.output, s.cacheRead, s.cacheCreation)),
			"date":           md.FmtMtime(dateSecs),
			"error_count":    len(s.failed),
			"errors":         errs,
			"repeat_count":   len(reps),
			"repeats":        reps,
		})
	}

	// tool_freq: ordered object sorted by count desc (stable on first-seen order)
	toolPairs := make(orderedIntObj, 0, len(globalToolOrder))
	for _, name := range globalToolOrder {
		toolPairs = append(toolPairs, struct {
			K string
			V int
		}{name, globalToolFreq[name]})
	}
	sort.SliceStable(toolPairs, func(i, j int) bool { return toolPairs[i].V > toolPairs[j].V })

	return map[string]any{
		"available":     true,
		"last_session":  lastSession,
		"week":          week,
		"sessions":      sessions,
		"errors_total":  allFailedCount,
		"repeats_total": len(repeated),
		"tool_freq":     toolPairs,
	}
}

func round4(f float64) float64 { return math.Round(f*10000) / 10000 }

func sliceStr8(s string) string {
	if len(s) >= 8 {
		return s[:8]
	}
	return s
}

func containsStr(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
