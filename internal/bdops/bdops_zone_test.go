package bdops_test

import (
	"testing"
	"time"

	"github.com/niclasedge/git-planner-go/internal/bdops"
)

// TestParseUserDateInIsZoneExplicit is an EXTERNAL test (package bdops_test) on
// purpose: it may only touch the exported surface, which is exactly the
// position every other package is in.
//
// The seam it needs exists twice over inside this package — parseUserDate takes
// a location, and its tests pass one — but a caller outside had no way to say
// which zone it means. internal/taskops hit that: to test that a due date is
// read as wall clock and a created stamp is not (a difference that is INVISIBLE
// under TZ=UTC, so a build agent would run the check green while the reading is
// wrong), its tests assign to time.Local and put it back in t.Cleanup.
//
// That is a process-global variable. Go resolves time.Local once, so setting TZ
// from a test does not move it afterwards, and two tests that reassign it
// cannot run in parallel — not because it is slow, but because one of them
// silently reads the other's zone. This test is written the way that one wants
// to be written, and it could not compile before ParseUserDateIn existed.
func TestParseUserDateInIsZoneExplicit(t *testing.T) {
	t.Parallel()

	cases := []struct {
		zone   string
		stored string
		want   func(*time.Location) time.Time
	}{
		// 23:00 in January: read as UTC, Berlin (+1) lands on the 16th.
		{"Europe/Berlin", "2026-01-15T23:00:00Z", func(l *time.Location) time.Time {
			return time.Date(2026, 1, 15, 23, 0, 0, 0, l)
		}},
		// Tokyo is +9 all year, so the same digits mean a different instant.
		{"Asia/Tokyo", "2026-01-15T23:00:00Z", func(l *time.Location) time.Time {
			return time.Date(2026, 1, 15, 23, 0, 0, 0, l)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.zone, func(t *testing.T) {
			// Parallel is the point: neither subtest may depend on a zone the
			// other one set.
			t.Parallel()
			loc, err := time.LoadLocation(tc.zone)
			if err != nil {
				t.Skipf("no tzdata for %s: %v", tc.zone, err)
			}
			got, ok := bdops.ParseUserDateIn(tc.stored, loc)
			if !ok {
				t.Fatalf("ParseUserDateIn(%q, %s) rejected a value bd writes", tc.stored, tc.zone)
			}
			if want := tc.want(loc); !got.Equal(want) {
				t.Errorf("ParseUserDateIn(%q, %s) = %s, want %s", tc.stored, tc.zone, got, want)
			}
			if y, m, d := got.Date(); y != 2026 || m != time.January || d != 15 {
				t.Errorf("calendar day = %04d-%02d-%02d, want 2026-01-15", y, m, d)
			}
		})
	}
}

// TestParseUserDateIsParseUserDateInLocal keeps the convenient form honest: the
// zone-less spelling must stay exactly the explicit one applied to time.Local,
// or callers would be choosing between two behaviours rather than two
// spellings.
func TestParseUserDateIsParseUserDateInLocal(t *testing.T) {
	t.Parallel()
	const stored = "2026-01-15T23:00:00Z"
	a, okA := bdops.ParseUserDate(stored)
	b, okB := bdops.ParseUserDateIn(stored, time.Local)
	if okA != okB || !a.Equal(b) {
		t.Errorf("ParseUserDate = (%s, %v), ParseUserDateIn(…, time.Local) = (%s, %v)", a, okA, b, okB)
	}
}
