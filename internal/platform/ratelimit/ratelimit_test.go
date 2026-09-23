package ratelimit

import (
	"testing"
	"time"
)

type manual struct{ t time.Time }

func (m *manual) Now() time.Time { return m.t }

func TestBurstThenRefill(t *testing.T) {
	c := &manual{t: time.Unix(0, 0)}
	l := New(3, 10*time.Second, c)
	for i := 0; i < 3; i++ {
		if !l.Allow("ip") {
			t.Fatalf("request %d of the burst was refused", i+1)
		}
	}
	if l.Allow("ip") {
		t.Fatal("the fourth request in a burst of three should be refused")
	}
	if !l.Allow("other") {
		t.Fatal("another key has its own bucket")
	}
	c.t = c.t.Add(10 * time.Second)
	if !l.Allow("ip") {
		t.Fatal("one token should have refilled")
	}
	if l.Allow("ip") {
		t.Fatal("only one token should have refilled")
	}
}

func TestPruneForgetsFullBuckets(t *testing.T) {
	c := &manual{t: time.Unix(0, 0)}
	l := New(2, time.Second, c)
	l.Allow("a")
	c.t = c.t.Add(time.Minute)
	l.prune(c.t)
	if len(l.buckets) != 0 {
		t.Fatalf("expected an empty map, got %d buckets", len(l.buckets))
	}
}
