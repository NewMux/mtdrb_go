// Package ratelimit is an in-process token bucket, keyed by caller.
//
// It guards the endpoints that take a secret — sign-in, the second factor,
// password reset — against guessing. In process rather than in Postgres or a
// cache because those endpoints are few, the numbers are small, and a limit
// that resets when an instance restarts still turns a million guesses into a
// handful. Several API instances each hold their own buckets, which multiplies
// the allowance by the instance count; that is a known and acceptable bound.
package ratelimit

import (
	"sync"
	"time"

	"github.com/NewMux/mtdrb_go/internal/platform/clock"
)

// Limiter allows Burst requests per key at once, refilling one every Every.
type Limiter struct {
	burst float64
	every time.Duration
	clock clock.Clock

	mu      sync.Mutex
	buckets map[string]*bucket
	calls   int
}

type bucket struct {
	tokens float64
	at     time.Time
}

// New builds a limiter.
func New(burst int, every time.Duration, c clock.Clock) *Limiter {
	if c == nil {
		c = clock.System{}
	}
	return &Limiter{burst: float64(burst), every: every, clock: c, buckets: map[string]*bucket{}}
}

// Allow spends one token for key, and reports false when there is none.
func (l *Limiter) Allow(key string) bool {
	now := l.clock.Now()
	l.mu.Lock()
	defer l.mu.Unlock()

	l.calls++
	if l.calls%1024 == 0 {
		l.prune(now)
	}

	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{tokens: l.burst, at: now}
		l.buckets[key] = b
	}
	b.tokens = min(l.burst, b.tokens+float64(now.Sub(b.at))/float64(l.every))
	b.at = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// prune forgets buckets that have refilled completely, so a scan of sign-in
// attempts from many addresses does not grow the map for ever.
func (l *Limiter) prune(now time.Time) {
	full := time.Duration(l.burst * float64(l.every))
	for key, b := range l.buckets {
		if now.Sub(b.at) >= full {
			delete(l.buckets, key)
		}
	}
}
