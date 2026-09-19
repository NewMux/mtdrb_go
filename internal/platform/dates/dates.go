// Package dates carries calendar dates across the wire.
//
// A date is not a timestamp. "The session was performed on 18 September" is
// true regardless of what o'clock it was or which side of the world the
// trainer is on, and `api/openapi.yaml` says so: those fields are
// `format: date`.
//
// Go's time.Time does not agree. Its UnmarshalJSON accepts RFC 3339 and
// nothing else, so a conforming client sending "2026-09-18" is rejected with
// a parse error — which is exactly what happened to every offline operation
// carrying a date. Meanwhile the sync pull serialises rows with to_jsonb, and
// Postgres renders a date column as "2026-09-18". The server was refusing to
// accept the same format it hands out.
//
// This type closes that gap in one place, so no handler has to remember.
package dates

import (
	"fmt"
	"strings"
	"time"
)

// Layout is the wire format: ISO 8601 calendar date, as the spec publishes it.
const Layout = "2006-01-02"

// Date is a calendar date that marshals as YYYY-MM-DD.
//
// Held as a time.Time at UTC midnight so it converts to what the service layer
// and pgx already expect without a second representation to keep in step.
type Date struct {
	t time.Time
}

// Of builds a Date from a timestamp, discarding the time of day.
func Of(t time.Time) Date {
	y, m, d := t.Date()
	return Date{t: time.Date(y, m, d, 0, 0, 0, 0, time.UTC)}
}

// Time returns UTC midnight on this date.
func (d Date) Time() time.Time { return d.t }

// IsZero reports whether the date was never set.
func (d Date) IsZero() bool { return d.t.IsZero() }

// String renders the wire format.
func (d Date) String() string {
	if d.t.IsZero() {
		return ""
	}
	return d.t.Format(Layout)
}

// MarshalJSON writes YYYY-MM-DD, or null for the zero value.
func (d Date) MarshalJSON() ([]byte, error) {
	if d.t.IsZero() {
		return []byte("null"), nil
	}
	return []byte(`"` + d.t.Format(Layout) + `"`), nil
}

// UnmarshalJSON accepts a calendar date, and tolerates a full RFC 3339
// timestamp.
//
// The tolerance is not politeness: the Go integration tests marshal Go
// time.Time values, and existing callers may have been sending timestamps for
// as long as that was the only thing that worked. Refusing them now would fix
// one client by breaking another.
func (d *Date) UnmarshalJSON(b []byte) error {
	s := strings.TrimSpace(string(b))
	if s == "null" || s == `""` {
		d.t = time.Time{}
		return nil
	}
	s = strings.Trim(s, `"`)

	if t, err := time.Parse(Layout, s); err == nil {
		d.t = t
		return nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		*d = Of(t)
		return nil
	}
	return fmt.Errorf("dates: %q is not a date (want YYYY-MM-DD)", s)
}

// TimePtr converts a nullable date field to the *time.Time the service layer
// takes, so a handler never writes the nil-and-zero dance by hand.
func (d *Date) TimePtr() *time.Time {
	if d == nil || d.IsZero() {
		return nil
	}
	t := d.t
	return &t
}

// OrElse returns the date, or the fallback when it was not supplied.
//
// Saves every call site the same three-line nil-and-zero dance.
func (d *Date) OrElse(fallback time.Time) time.Time {
	if d == nil || d.IsZero() {
		return fallback
	}
	return d.t
}

// PatchPtr converts a triple-state patch field to the **time.Time the service
// layer takes.
//
// The three states must survive the conversion intact: field absent (leave it
// alone), field explicitly null (clear it), field set. Collapsing the first
// two would turn "I did not mention the birthday" into "delete the birthday".
func PatchPtr(d **Date) **time.Time {
	if d == nil {
		return nil
	}
	if *d == nil {
		// Explicit null: a non-nil outer pointer to a nil inner one.
		var cleared *time.Time
		return &cleared
	}
	t := (*d).t
	inner := &t
	return &inner
}
