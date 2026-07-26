package gh

import (
	"regexp"
	"strings"
	"time"
)

// GitHub has no due date for issues — only milestones have one. So the date
// lives in the body by convention, and the convention that matters is the one
// the existing issues already use:
//
//	target date: 2026-08-01
//
// That form is checked first and is the only one anchored to a whole line, which
// is what makes it safe to strip from the rendered body. The rest are accepted
// because they are what people actually type; first match wins.
var duePatterns = []*regexp.Regexp{
	// The canonical line. Anchored, so "target date:" inside a sentence is not a
	// due date and does not get cut out of the text.
	regexp.MustCompile(`(?im)^[ \t]*target(?:[ \t_-]*date)?[ \t]*:[ \t]*(\d{4})-(\d{2})-(\d{2})[ \t]*\r?$`),
	regexp.MustCompile(`(?i)\bdue(?:[ \t_-]*date)?[ \t]*:[ \t]*(\d{4})-(\d{2})-(\d{2})`),
	regexp.MustCompile(`(?i)@due\([ \t]*(\d{4})-(\d{2})-(\d{2})[ \t]*\)`),
	regexp.MustCompile(`\x{1F4C5}[ \t]*(\d{4})-(\d{2})-(\d{2})`), // 📅 2026-08-01
}

// dueDatesDE matches the day-first form people type in German. Kept separate
// because the capture order is reversed, not because the parsing differs.
var dueDatesDE = regexp.MustCompile(
	`(?i)\b(?:target|due|f(?:ä|ae)llig)(?:[ \t_-]*date)?[ \t]*:[ \t]*(\d{2})\.(\d{2})\.(\d{4})`)

// ParseDue reads a target date out of an issue body. The returned time is a
// plain calendar day in UTC: a due date is a day, not an instant, and pinning it
// to local midnight would make it shift for anyone in another timezone.
func ParseDue(body string) (time.Time, bool) {
	if body == "" {
		return time.Time{}, false
	}
	for _, re := range duePatterns {
		if m := re.FindStringSubmatch(body); m != nil {
			if t, ok := day(m[1], m[2], m[3]); ok {
				return t, true
			}
		}
	}
	if m := dueDatesDE.FindStringSubmatch(body); m != nil {
		if t, ok := day(m[3], m[2], m[1]); ok {
			return t, true
		}
	}
	return time.Time{}, false
}

// day builds a date and rejects the ones that only look like dates. Go's parser
// does the real work: it refuses 2026-02-31, which a regexp happily matches.
func day(y, m, d string) (time.Time, bool) {
	t, err := time.Parse("2006-01-02", y+"-"+m+"-"+d)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// StripDue removes the canonical target-date line so it is not repeated in the
// rendered body. Only the anchored pattern is stripped — the inline forms are
// part of a sentence, and cutting them would leave the text mangled.
func StripDue(body string) string {
	out := duePatterns[0].ReplaceAllString(body, "")
	return strings.Trim(out, "\n\r \t")
}

// dueLinePrefix is what SetDue writes. "target date" rather than the shorter
// "target" because that is the form the existing issues use, so an edited body
// keeps the convention it arrived with.
const dueLinePrefix = "target date: "

// SetDue rewrites the canonical target-date line in a body. A zero date removes
// it. The line goes to the top: it is metadata about the issue, and the top is
// the one position that is stable no matter what the body ends with.
//
// Only the anchored form is touched, so a date written into a sentence survives —
// which also means it keeps being parsed. Callers that clear a date should check
// with ParseDue whether one is left and say so, rather than cutting prose.
func SetDue(body string, due time.Time) string {
	var keep []string
	for _, line := range strings.Split(body, "\n") {
		// The anchored pattern is a whole line by definition, so matching it
		// line by line is the same test — and it takes the newline with it,
		// which a ReplaceAll on the whole body would leave behind as a blank.
		if duePatterns[0].MatchString(line) {
			continue
		}
		keep = append(keep, line)
	}

	rest := strings.Trim(strings.Join(keep, "\n"), "\n\r \t")
	if due.IsZero() {
		return rest
	}
	line := dueLinePrefix + due.Format("2006-01-02")
	if rest == "" {
		return line
	}
	return line + "\n\n" + rest
}

// DueDate is the issue's target date: the body convention first, the milestone's
// own due date as a fallback. Milestones come second because they describe a
// batch — the line in the body is about this one issue.
func (i Issue) DueDate() (time.Time, bool) {
	if t, ok := ParseDue(i.Body); ok {
		return t, true
	}
	if i.Milestone != nil && i.Milestone.DueOn != nil {
		d := *i.Milestone.DueOn
		return time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.UTC), true
	}
	return time.Time{}, false
}

// BodyClean is the body as it should be displayed.
func (i Issue) BodyClean() string { return StripDue(i.Body) }
