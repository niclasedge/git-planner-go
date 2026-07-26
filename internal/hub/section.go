package hub

import (
	"sync"
	"time"

	"github.com/niclasedge/git-planner-go/internal/sched"
)

// section holds one refreshable piece of state behind its own lock.
//
// Per-section locks rather than one lock for the whole dashboard: a slow Actions
// refresh must not block the issues page from rendering. Readers take RLock and
// copy out, so a render never waits on a write it does not need.
type section[T any] struct {
	mu        sync.RWMutex
	data      T
	updatedAt time.Time
	err       error
	sch       sched.Schedule

	// busy serializes refreshes so a manual reload and the scheduler cannot run
	// the same fan-out twice at once.
	busy sync.Mutex
}

func newSection[T any](interval time.Duration) *section[T] {
	return &section[T]{sch: sched.New(interval)}
}

// read returns a snapshot. The caller gets whatever we last had, even if the
// most recent refresh failed — stale data beats an empty page.
func (s *section[T]) read() (data T, updatedAt time.Time, err error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.data, s.updatedAt, s.err
}

func (s *section[T]) due(now time.Time) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.sch.Due(now)
}

func (s *section[T]) succeed(data T, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data = data
	s.updatedAt = now
	s.err = nil
	s.sch.Succeed(now)
}

// replace publishes new data without touching the refresh schedule.
//
// succeed is for a refresh that talked to GitHub and may therefore rest; this is
// for data we produced ourselves — the response to our own write. Letting that
// reset the clock would postpone the next poll by a full interval every time
// someone edits an issue.
func (s *section[T]) replace(data T, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data = data
	s.updatedAt = now
	s.err = nil
}

// fail keeps the old data and lets the schedule decide when to try again.
func (s *section[T]) fail(err error, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.err = err
	s.sch.Fail(now)
}

// invalidate forces the next scheduler tick to refresh this section. Used when
// the notifications inbox reports movement.
func (s *section[T]) invalidate() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sch.Invalidate()
}
