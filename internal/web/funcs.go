package web

import (
	"fmt"
	"html/template"
	"math"
	"strings"
	"time"
)

// funcs are the template helpers. render is added separately in newServer
// because it needs a reference to the parsed set.
var funcs = template.FuncMap{
	"static":     staticURL,
	"relTime":    relTime,
	"dur":        humanDuration,
	"size":       humanSize,
	"barHeight":  barHeight,
	"labelStyle": labelStyle,
	"initials":   initials,
	"pctText":    pctText,
	"truncate":   truncate,
	"lower":      strings.ToLower,
	"add":        func(a, b int) int { return a + b },
	"due":        dueChip,
	"markdown":   renderMarkdown,
	"row":        newRowCtx,
	"topRow":     newTopRowCtx,
	"subPct":     subPct,
}

// dueChip is the date chip for a template that holds a *time.Time rather than a
// gh.Issue — the beads rows, whose type lives in package panel and so cannot
// reach plannerData.DueBadge. It returns nil for a bead without a date, so
// `{{ with due .Due }}` renders nothing at all.
//
// It exists so the overdue/heute/weekday/date thresholds keep exactly one
// definition. A second copy for beads would drift from the planner's the first
// time either side is tuned, and the two pages would then disagree about what
// "diese Woche" means.
// An overdue chip carries the date rather than dueBadge's "über": under a group
// head that already reads "Überfällig" the word is the same statement twice,
// while "1.8." adds the one thing the head cannot say — how long. The planner
// solves the same redundancy by dropping the chip entirely, which it can afford
// because its rows are grouped by nothing else; a bead row also appears in the
// tree, where no head announces the state.
func dueChip(t *time.Time) *agendaItem {
	if t == nil {
		return nil
	}
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	badge, red := dueBadge(*t, today)
	if red {
		badge = t.Format("2.1.")
	}
	return &agendaItem{Badge: badge, Red: red}
}

// relTime renders a timestamp the way you read it at a glance: "4m", "3h", "6d".
func relTime(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	d := time.Since(t)
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	default:
		return t.Format("2 Jan 2006")
	}
}

// humanSize formats a byte count the way a model list reads: "13,3 GB". Decimal
// units, matching what `ollama list` prints — not the binary ones, or the same
// model would appear to have two sizes.
func humanSize(b int64) string {
	if b <= 0 {
		return "—"
	}
	const k = 1000.0
	switch {
	case b < 1_000_000:
		return fmt.Sprintf("%.0f kB", float64(b)/k)
	case b < 1_000_000_000:
		return fmt.Sprintf("%.0f MB", float64(b)/(k*k))
	default:
		// Comma as the decimal separator: the UI is German.
		s := fmt.Sprintf("%.1f", float64(b)/(k*k*k))
		return strings.Replace(s, ".", ",", 1) + " GB"
	}
}

// humanDuration formats a run length compactly: "42s", "3m 12s", "1h 4m".
func humanDuration(d time.Duration) string {
	if d <= 0 {
		return "—"
	}
	switch {
	// Below a second, seconds round to "0s" and say nothing — which is exactly
	// the range every local uptime probe lands in.
	case d < time.Second:
		return fmt.Sprintf("%dms", max(1, d.Milliseconds()))
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		s := int(d.Seconds()) % 60
		if s == 0 {
			return fmt.Sprintf("%dm", int(d.Minutes()))
		}
		return fmt.Sprintf("%dm %ds", int(d.Minutes()), s)
	default:
		m := int(d.Minutes()) % 60
		return fmt.Sprintf("%dh %dm", int(d.Hours()), m)
	}
}

// Sparkline geometry. The bar chart reads as a shape, so very short runs still
// need a visible stub — hence the floor.
const (
	sparkHeight  = 36
	sparkMinBar  = 3
	sparkSqrtCap = 2 // sqrt scaling keeps one outlier from flattening the rest
)

// barHeight scales a run duration to pixels. Square-root rather than linear:
// one 40-minute run should not squash twenty 30-second runs into invisible
// slivers.
func barHeight(d, max time.Duration) int {
	if d <= 0 || max <= 0 {
		return sparkMinBar
	}
	ratio := float64(d) / float64(max)
	if ratio > 1 {
		ratio = 1
	}
	scaled := math.Pow(ratio, 1.0/sparkSqrtCap)
	h := int(scaled * float64(sparkHeight))
	if h < sparkMinBar {
		return sparkMinBar
	}
	return h
}

// labelStyle turns a GitHub label colour into inline CSS, choosing readable text
// via relative luminance.
func labelStyle(hex string) template.CSS {
	hex = strings.TrimPrefix(strings.TrimSpace(hex), "#")
	if len(hex) != 6 {
		hex = "6e7681"
	}
	var r, g, b int
	if _, err := fmt.Sscanf(hex, "%02x%02x%02x", &r, &g, &b); err != nil {
		r, g, b = 110, 118, 129
	}
	// Perceived brightness, the usual WCAG-ish approximation.
	lum := (0.299*float64(r) + 0.587*float64(g) + 0.114*float64(b)) / 255
	fg := "#ffffff"
	if lum > 0.6 {
		fg = "#0d1117"
	}
	return template.CSS(fmt.Sprintf(
		"background:#%s;color:%s;border-color:rgba(255,255,255,.14)", hex, fg))
}

func initials(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "?"
	}
	return strings.ToUpper(s[:1])
}

// pctText renders a percentage, where -1 means "no data" rather than zero.
func pctText(p int) string {
	if p < 0 {
		return "—"
	}
	return fmt.Sprintf("%d%%", p)
}

func truncate(n int, s string) string {
	if len(s) <= n {
		return s
	}
	return strings.TrimSpace(s[:n]) + "…"
}

// subPct is the parent's completion as a percentage. Computed here rather
// than taken from GitHub's percent_completed so the bar can never disagree
// with the "2/7" beside it.
func subPct(done, total int) int {
	if total <= 0 {
		return 0
	}
	p := done * 100 / total
	if p < 0 {
		return 0
	}
	if p > 100 {
		return 100
	}
	return p
}
