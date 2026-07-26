// Package sched holds the refresh timing rule shared by the built-in pages and
// the config-driven widgets.
package sched

import "time"

// MaxRetries bounds the error backoff. After this many consecutive failures we
// stop retrying early and fall back to the regular interval.
const MaxRetries = 5

// Schedule decides when a piece of state is next due. It carries no lock —
// callers already hold one around the data it guards.
type Schedule struct {
	Interval time.Duration

	next    time.Time
	retries int
}

func New(interval time.Duration) Schedule { return Schedule{Interval: interval} }

func (s *Schedule) Due(now time.Time) bool { return now.After(s.next) }

func (s *Schedule) Next() time.Time { return s.next }

func (s *Schedule) Retries() int { return s.retries }

// Succeed schedules the next regular update.
func (s *Schedule) Succeed(now time.Time) {
	s.retries = 0
	s.next = now.Add(s.Interval)
}

// Fail retries sooner than the regular cadence, backing off quadratically:
// 1, 4, 9, 16, 25 minutes. The retry is never later than the regular update
// would have been — a transient error must not make a panel refresh less often
// than normal.
func (s *Schedule) Fail(now time.Time) {
	regular := now.Add(s.Interval)
	if s.retries >= MaxRetries {
		s.next = regular
		return
	}
	s.retries++
	backoff := now.Add(time.Duration(s.retries*s.retries) * time.Minute)
	if backoff.After(regular) {
		s.next = regular
		return
	}
	s.next = backoff
}

// Invalidate makes the next scheduler tick pick this up.
func (s *Schedule) Invalidate() { s.next = time.Time{} }
