package ids

import (
	"testing"
	"time"
)

func TestNewIsUniqueAndTimeOrdered(t *testing.T) {
	const n = 1000
	seen := make(map[ID]struct{}, n)
	prev := New()
	seen[prev] = struct{}{}
	for i := 1; i < n; i++ {
		id := New()
		if _, dup := seen[id]; dup {
			t.Fatalf("duplicate id at %d", i)
		}
		seen[id] = struct{}{}
		// v7 embeds a millisecond timestamp, so ids minted in sequence must
		// never sort backwards; offline rows depend on this for index locality.
		if id.String() < prev.String() {
			t.Fatalf("id went backwards: %s then %s", prev, id)
		}
		prev = id
	}
}

func TestNewEmbedsCurrentTime(t *testing.T) {
	before := time.Now().Add(-time.Second)
	id := New()
	sec, nsec := id.Time().UnixTime()
	got := time.Unix(sec, nsec)
	if got.Before(before) || got.After(time.Now().Add(time.Second)) {
		t.Errorf("embedded time %v outside expected window", got)
	}
}

func TestParseRoundTrip(t *testing.T) {
	id := New()
	parsed, err := Parse(id.String())
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if parsed != id {
		t.Errorf("round trip mismatch")
	}
	if _, err := Parse("not-a-uuid"); err == nil {
		t.Error("expected parse error")
	}
}
