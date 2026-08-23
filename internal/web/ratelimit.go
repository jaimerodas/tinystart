package web

import (
	"sync"
	"time"
)

// limiter is a fixed-window counter, per client address.
//
// Fixed windows are the crude choice. Someone can send the whole allowance
// at the end of one window, and again at the start of the next. So the real
// worst case is twice the limit. That is fine for what these guard: the
// point is to make a password guessing script useless, not to shape
// traffic.
//
// It is in memory, so it resets on deploy and does not exist across
// containers. There is one container.
type limiter struct {
	limit  int
	window time.Duration
	now    func() time.Time

	mu      sync.Mutex
	windows map[string]*window
}

// window is one client's count and when their current window began.
type window struct {
	count     int
	startedAt time.Time
}

func newLimiter(limit int, per time.Duration, now func() time.Time) *limiter {
	return &limiter{limit: limit, window: per, now: now, windows: map[string]*window{}}
}

// allow records an attempt by key and reports whether it is within the limit.
//
// The prune happens here rather than on a timer. A background goroutine to
// clean up a map that only grows when someone is signing in is more
// machinery than the problem deserves. Doing it on the way past means the
// map is only ever as large as the number of addresses seen inside one
// window.
func (l *limiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	l.prune(now)

	current := l.windows[key]
	if current == nil || now.Sub(current.startedAt) >= l.window {
		l.windows[key] = &window{count: 1, startedAt: now}
		return true
	}

	current.count++
	return current.count <= l.limit
}

// prune drops the windows that have run out. The caller holds the lock.
func (l *limiter) prune(now time.Time) {
	for key, w := range l.windows {
		if now.Sub(w.startedAt) >= l.window {
			delete(l.windows, key)
		}
	}
}
