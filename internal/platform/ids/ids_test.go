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

func TestUseSequenceIsReproducibleAndValid(t *testing.T) {
	start := time.Date(2026, 6, 17, 5, 30, 0, 0, time.UTC)
	mint := func() []ID {
		restore := UseSequence(start, 42)
		defer restore()
		out := make([]ID, 50)
		for i := range out {
			out[i] = New()
		}
		return out
	}
	first, second := mint(), mint()
	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("id %d differs between runs: %s and %s", i, first[i], second[i])
		}
		if first[i].Version() != 7 || first[i].Variant().String() != "RFC4122" {
			t.Fatalf("id %d is not a UUIDv7: %s", i, first[i])
		}
		if i > 0 && first[i].String() <= first[i-1].String() {
			t.Fatalf("id %d went backwards: %s then %s", i, first[i-1], first[i])
		}
	}
	sec, _ := first[0].Time().UnixTime()
	if got := time.Unix(sec, 0).UTC(); !got.Equal(start) {
		t.Errorf("first id embeds %v, want %v", got, start)
	}
	if New() == first[0] {
		t.Error("restore did not return New to random ids")
	}
}
