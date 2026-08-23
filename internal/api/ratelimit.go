package api

import (
	"sync"
	"time"
)

// limiter is a fixed-window counter keyed by whatever the caller decides identifies a
// request — a username, an account id, or the empty string for "everybody at once".
//
// A fixed window rather than a token bucket: the failure mode of a fixed window is that
// twice the limit can pass across a boundary, which for "slow down a password guesser"
// and "do not fetch arbitrary URLs on demand" is not a failure mode that matters. What it
// buys is a data structure somebody can hold in their head.
type limiter struct {
	mu      sync.Mutex
	windows map[string]*window

	limit  int
	period time.Duration
	now    func() time.Time
}

type window struct {
	count   int
	resetAt time.Time
}

func newLimiter(limit int, period time.Duration) *limiter {
	return &limiter{
		windows: make(map[string]*window),
		limit:   limit,
		period:  period,
		now:     time.Now,
	}
}

// allow records an attempt and reports whether it is within the limit.
func (l *limiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	w := l.windows[key]
	if w == nil || now.After(w.resetAt) {
		// Collected on write rather than by a sweeper: entries are only created by
		// attempts, and an expired one is replaced the next time its key is seen. A key
		// nobody uses again holds a struct of two words until the process restarts, which
		// is cheaper than a goroutine.
		if len(l.windows) > 4096 {
			l.windows = make(map[string]*window)
		}
		l.windows[key] = &window{count: 1, resetAt: now.Add(l.period)}
		return true
	}

	w.count++
	return w.count <= l.limit
}
