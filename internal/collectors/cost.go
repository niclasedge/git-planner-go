package collectors

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/niclasedge/git-planner-go/internal/shared"
)

func usageDir() string {
	if env := os.Getenv("PKM_USAGE_DIR"); env != "" {
		return env
	}
	return filepath.Join(Home(), "git", "ObsidianPKM", "skripte", "usage")
}

type dailyRow struct {
	date string
	cost float64
}

func loadDailyCosts(dir string) []dailyRow {
	rows := []dailyRow{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return rows
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		if strings.HasSuffix(e.Name(), "_rl.json") || strings.HasSuffix(e.Name(), ".migrated") {
			continue
		}
		text, ok := shared.ReadText(filepath.Join(dir, e.Name()))
		if !ok || text == "" {
			continue
		}
		var data map[string]any
		if json.Unmarshal([]byte(text), &data) != nil {
			continue
		}
		if _, has := data["ccusage_daily"]; has {
			continue
		}
		d := asStr(data["date"])
		if d == "" {
			continue
		}
		var cost float64
		if ccusage, ok := data["ccusage"].(map[string]any); ok {
			cost = parseFloatOrZero(asStr(ccusage["total_cost"]))
		} else {
			cost = parseFloatOrZero("0")
		}
		rows = append(rows, dailyRow{date: d, cost: cost})
	}
	return rows
}

// parseFloatOrZero mirrors `parseFloat(String(x ?? "0")) || 0`.
func parseFloatOrZero(s string) float64 {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || math.IsNaN(f) {
		return 0
	}
	return f
}

type rlSnapshot struct {
	Ts               string   `json:"ts"`
	FiveHourPct      *float64 `json:"five_hour_pct"`
	FiveHourResetsAt *float64 `json:"five_hour_resets_at"`
	SevenDayPct      *float64 `json:"seven_day_pct"`
	SevenDayResetsAt *float64 `json:"seven_day_resets_at"`
}

func latestRlSnapshot(dir string) rlSnapshot {
	bestTs := ""
	best := rlSnapshot{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return best
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), "_rl.json") {
			continue
		}
		text, ok := shared.ReadText(filepath.Join(dir, e.Name()))
		if !ok || text == "" {
			continue
		}
		var data rlSnapshot
		if json.Unmarshal([]byte(text), &data) != nil {
			continue
		}
		if data.Ts > bestTs {
			bestTs = data.Ts
			best = data
		}
	}
	return best
}

func rateLimitFromSnapshot(pct, resetsAt *float64, windowSecs float64, nowTs int64) map[string]any {
	if pct == nil || resetsAt == nil {
		return nil
	}
	remaining := *resetsAt - float64(nowTs)
	if remaining <= 0 || remaining > windowSecs {
		return nil
	}
	elapsed := windowSecs - remaining
	if elapsed <= 0 {
		return nil
	}
	frac := elapsed / windowSecs
	projected := 0.0
	if frac > 0 {
		projected = *pct / frac
	}
	d := math.Floor(remaining / 86400)
	rem1 := math.Mod(remaining, 86400)
	h := math.Floor(rem1 / 3600)
	m := math.Floor(math.Mod(rem1, 3600) / 60)
	var countdown string
	switch {
	case d != 0:
		countdown = fmt.Sprintf("%dd%dh", int(d), int(h))
	case h != 0:
		countdown = fmt.Sprintf("%dh%dm", int(h), int(m))
	default:
		countdown = fmt.Sprintf("%dm", int(m))
	}
	return map[string]any{
		"used_pct":      math.Round(*pct*10) / 10,
		"elapsed_pct":   math.Round(frac*1000) / 10,
		"projected_pct": math.Round(projected*10) / 10,
		"resets_in":     countdown,
	}
}

// CollectCost reports account-wide $ from usage/*.json + *_rl.json. Week starts
// Friday (Claude 7d reset Thu->Fri). Pass an empty dir to use PKM_USAGE_DIR.
func CollectCost(dir string) map[string]any {
	udir := dir
	if udir == "" {
		udir = usageDir()
	}
	if _, err := os.Stat(udir); err != nil {
		return map[string]any{"available": false, "reason": fmt.Sprintf("usage/ nicht gefunden: %s", udir)}
	}
	rows := loadDailyCosts(udir)
	if len(rows) == 0 {
		return map[string]any{"available": false, "reason": fmt.Sprintf("keine ccusage-JSON in %s", udir)}
	}

	now := time.Now()
	todayStr := now.UTC().Format("2006-01-02")
	yestStr := now.Add(-24 * time.Hour).UTC().Format("2006-01-02")
	// Week starts Friday (JS Sun=0..Sat=6, Fri=5)
	dow := int(now.Weekday())
	daysFromFri := (dow - 5 + 7) % 7
	weekStr := now.Add(-time.Duration(daysFromFri) * 24 * time.Hour).UTC().Format("2006-01-02")
	monthStr := fmt.Sprintf("%04d-%02d-01", now.Year(), int(now.Month()))

	var todayC, yestC, weekC, monthC, totalC float64
	for _, r := range rows {
		totalC += r.cost
		if r.date == todayStr {
			todayC += r.cost
		}
		if r.date == yestStr {
			yestC += r.cost
		}
		if r.date >= weekStr {
			weekC += r.cost
		}
		if r.date >= monthStr {
			monthC += r.cost
		}
	}

	result := map[string]any{
		"available": true,
		"today":     todayC,
		"yesterday": yestC,
		"week":      weekC,
		"month":     monthC,
		"total":     totalC,
		"five_hour": nil,
		"seven_day": nil,
	}

	rl := latestRlSnapshot(udir)
	if rl.Ts != "" {
		nowTs := now.Unix()
		result["five_hour"] = rateLimitFromSnapshot(rl.FiveHourPct, rl.FiveHourResetsAt, 5*3600, nowTs)
		result["seven_day"] = rateLimitFromSnapshot(rl.SevenDayPct, rl.SevenDayResetsAt, 7*24*3600, nowTs)
	}

	return result
}
