// Package md holds pure markdown/frontmatter helpers — Go-native ports of
// knowledge.py's _md_headers / _est_tokens / _frontmatter_field / _fmt_mtime
// (via the Deno md.ts). Behavior is kept faithful to the originals so collector
// output stays parity-checkable.
package md

import (
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

var headerRe = regexp.MustCompile(`^(#{1,6})\s+(.*\S)\s*$`)

// MdHeaders returns ATX markdown headers of the given levels, in document
// order. Fenced code blocks are skipped so a `# comment` inside ``` is not
// mistaken for a heading. Pass nil levels to default to [1, 2].
func MdHeaders(text string, levels []int) []string {
	if levels == nil {
		levels = []int{1, 2}
	}
	want := make(map[int]bool, len(levels))
	for _, l := range levels {
		want[l] = true
	}
	out := []string{}
	inFence := false
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		m := headerRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		if want[len(m[1])] {
			out = append(out, strings.TrimSpace(m[2]))
		}
	}
	return out
}

// EstTokens is a rough token estimate (~chars/4). A budget gauge, not a real
// tokenizer. Counts Unicode code points to match JS `[...text].length`.
func EstTokens(text string) int {
	return (utf8.RuneCountInString(text) + 3) / 4
}

var blockScalarRe = regexp.MustCompile(`^[|>][+-]?$`)

// FrontmatterField extracts a YAML frontmatter value from a leading `---` block
// ("" if absent). Handles single-line scalars (with quote-stripping) AND block
// scalars (`>`/`|` with optional chomp `+`/`-`), whose value sits on the
// following more-indented lines. Folded (`>`) joins with spaces, literal (`|`)
// keeps newlines.
func FrontmatterField(text, field string) string {
	lead := strings.TrimLeft(text, "\uFEFF")
	if !strings.HasPrefix(lead, "---") {
		return ""
	}
	var block string
	if idx := strings.Index(lead[3:], "\n---"); idx != -1 {
		block = lead[3 : 3+idx]
	} else {
		block = lead[3:]
	}
	lines := strings.Split(block, "\n")
	re := regexp.MustCompile(`^(\s*)` + regexp.QuoteMeta(field) + `\s*:\s*(.*)$`)
	for i := 0; i < len(lines); i++ {
		m := re.FindStringSubmatch(lines[i])
		if m == nil {
			continue
		}
		indent := utf8.RuneCountInString(m[1])
		val := strings.TrimSpace(m[2])
		if blockScalarRe.MatchString(val) {
			folded := val[0] == '>'
			body := []string{}
			for j := i + 1; j < len(lines); j++ {
				if strings.TrimSpace(lines[j]) == "" {
					body = append(body, "")
					continue
				}
				li := utf8.RuneCountInString(lines[j]) - utf8.RuneCountInString(strings.TrimLeft(lines[j], " \t\n\r\f\v"))
				if li <= indent {
					break // dedent → end of the block scalar
				}
				body = append(body, strings.TrimSpace(lines[j]))
			}
			if folded {
				kept := []string{}
				for _, b := range body {
					if b != "" {
						kept = append(kept, b)
					}
				}
				return strings.TrimSpace(strings.Join(kept, " "))
			}
			return strings.TrimRight(strings.Join(body, "\n"), "\n")
		}
		if val == "" {
			return ""
		}
		if len(val) >= 2 {
			first := val[0]
			last := val[len(val)-1]
			if first == last && (first == '"' || first == '\'') {
				val = val[1 : len(val)-1]
			}
		}
		return val
	}
	return ""
}

// FmtMtime formats epoch seconds as local `YYYY-MM-DD HH:MM` ("" if falsy).
func FmtMtime(mt float64) string {
	if mt == 0 {
		return ""
	}
	d := time.UnixMilli(int64(mt * 1000)).Local()
	return fmt.Sprintf("%04d-%02d-%02d %02d:%02d",
		d.Year(), int(d.Month()), d.Day(), d.Hour(), d.Minute())
}
