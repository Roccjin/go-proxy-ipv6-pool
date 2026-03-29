package ratelimit

import (
	"sync"
	"time"
)

// Limiter implements a sliding-window rate limiter per key (user or IP).
type Limiter struct {
	mu         sync.Mutex
	windows    map[string]*window
	userLimits map[string]int // per-user custom limits
	limit      int
	window     time.Duration
}

type window struct {
	timestamps []time.Time
}

func New(limit int, windowDur time.Duration) *Limiter {
	l := &Limiter{
		windows:    make(map[string]*window),
		userLimits: make(map[string]int),
		limit:      limit,
		window:     windowDur,
	}
	go l.cleanupLoop()
	return l
}

// Allow checks if the key is within rate limit. Returns true if allowed.
func (l *Limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-l.window)
	keyLimit := l.effectiveLimit(key)

	w, ok := l.windows[key]
	if !ok {
		w = &window{}
		l.windows[key] = w
	}

	// Trim expired entries
	start := 0
	for start < len(w.timestamps) && w.timestamps[start].Before(cutoff) {
		start++
	}
	w.timestamps = w.timestamps[start:]

	if len(w.timestamps) >= keyLimit {
		return false
	}

	w.timestamps = append(w.timestamps, now)
	return true
}

// Remaining returns how many requests are left for the key in the current window.
func (l *Limiter) Remaining(key string) int {
	l.mu.Lock()
	defer l.mu.Unlock()

	cutoff := time.Now().Add(-l.window)
	keyLimit := l.effectiveLimit(key)
	w, ok := l.windows[key]
	if !ok {
		return keyLimit
	}

	count := 0
	for _, t := range w.timestamps {
		if !t.Before(cutoff) {
			count++
		}
	}
	return keyLimit - count
}

// effectiveLimit returns the per-user limit if set, otherwise the default. Must be called with mu held.
func (l *Limiter) effectiveLimit(key string) int {
	if ul, ok := l.userLimits[key]; ok {
		return ul
	}
	return l.limit
}

// SetUserLimit sets a custom rate limit for a specific user. 0 resets to default.
func (l *Limiter) SetUserLimit(user string, limit int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if limit <= 0 {
		delete(l.userLimits, user)
	} else {
		l.userLimits[user] = limit
	}
}

// UserLimit returns the effective limit for a user (custom or default).
func (l *Limiter) UserLimit(user string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.effectiveLimit(user)
}

// UserLimits returns a copy of all per-user custom limits.
func (l *Limiter) UserLimits() map[string]int {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make(map[string]int, len(l.userLimits))
	for k, v := range l.userLimits {
		out[k] = v
	}
	return out
}

func (l *Limiter) Limit() int           { return l.limit }
func (l *Limiter) Window() time.Duration { return l.window }

func (l *Limiter) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		l.mu.Lock()
		cutoff := time.Now().Add(-l.window)
		for key, w := range l.windows {
			start := 0
			for start < len(w.timestamps) && w.timestamps[start].Before(cutoff) {
				start++
			}
			w.timestamps = w.timestamps[start:]
			if len(w.timestamps) == 0 {
				delete(l.windows, key)
			}
		}
		l.mu.Unlock()
	}
}
