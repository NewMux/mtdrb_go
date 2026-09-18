// Package clock abstracts time so that scheduling, invoice ageing and token
// expiry can be tested deterministically.
package clock

import "time"

// Clock reports the current time.
type Clock interface {
	Now() time.Time
}

// System is the real wall clock, always in UTC.
type System struct{}

// Now returns the current UTC time.
func (System) Now() time.Time { return time.Now().UTC() }

// Fixed is a Clock frozen at a chosen instant, for tests.
type Fixed struct{ T time.Time }

// Now returns the frozen instant.
func (f Fixed) Now() time.Time { return f.T.UTC() }

// Advance moves a Fixed clock forward.
func (f *Fixed) Advance(d time.Duration) { f.T = f.T.Add(d) }
