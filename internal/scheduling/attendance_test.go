package scheduling

import "testing"

// allStatuses is the complete attendance state set.
var allStatuses = []AttendanceStatus{Scheduled, Completed, LateCancel, EarlyCancel, NoShow}

// Every state must be reachable from every other, in both directions. A
// trainer marking the wrong outcome one-handed on a gym floor has to be able
// to correct it without deleting and rebuilding the session.
func TestEveryStatePairIsReversible(t *testing.T) {
	for _, from := range allStatuses {
		for _, to := range allStatuses {
			if !CanTransition(from, to) {
				t.Errorf("cannot move from %s to %s; a mistaken mark would be uncorrectable", from, to)
			}
		}
	}
}

func TestSameStatusIsIdempotent(t *testing.T) {
	// Re-marking the current status must be accepted, not refused: the
	// offline outbox replays, and trainers double-tap.
	for _, status := range allStatuses {
		if !CanTransition(status, status) {
			t.Errorf("%s -> %s was refused; replaying an outbox entry would fail", status, status)
		}
	}
}

func TestUnknownStatesAreRefused(t *testing.T) {
	if CanTransition(Scheduled, AttendanceStatus("cancelled_by_alien")) {
		t.Error("an unknown target status was accepted")
	}
	if CanTransition(AttendanceStatus("nonsense"), Completed) {
		t.Error("an unknown source status was accepted")
	}
}

// The transition table decides what is billable. These are the rules the money
// actually follows, so they are asserted rather than assumed.
func TestBillableTransitions(t *testing.T) {
	cases := []struct {
		from, to   AttendanceStatus
		billable   bool
		lateCancel bool
	}{
		{Scheduled, Completed, true, false},
		{Scheduled, LateCancel, true, true},
		// An early cancellation is the one outcome that costs the client
		// nothing: the credit is never burned.
		{Scheduled, EarlyCancel, false, false},
		{Completed, Scheduled, false, false},
		{Completed, EarlyCancel, false, false},
		{LateCancel, Completed, true, false},
		{EarlyCancel, Completed, true, false},
		{NoShow, Completed, true, false},
	}
	for _, tc := range cases {
		got, ok := transitions[tc.from][tc.to]
		if !ok {
			t.Errorf("%s -> %s is missing from the table", tc.from, tc.to)
			continue
		}
		if got.billable != tc.billable {
			t.Errorf("%s -> %s: billable = %v, want %v", tc.from, tc.to, got.billable, tc.billable)
		}
		if got.lateCancellation != tc.lateCancel {
			t.Errorf("%s -> %s: lateCancellation = %v, want %v",
				tc.from, tc.to, got.lateCancellation, tc.lateCancel)
		}
	}
}

// Leaving a state must restore exactly what that state charged, or moving
// between two billable states would double-charge.
func TestResolvePreviousMatchesWhatEachStateCharges(t *testing.T) {
	cases := []struct {
		state          AttendanceStatus
		noShowBillable bool
		billable       bool
	}{
		{Completed, true, true},
		{Completed, false, true},
		{LateCancel, true, true},
		{LateCancel, false, true},
		{EarlyCancel, true, false},
		{Scheduled, true, false},
		// The no-show policy is a tenant setting, so what it charged depends
		// on that setting rather than on the state alone.
		{NoShow, true, true},
		{NoShow, false, false},
	}
	for _, tc := range cases {
		got := resolvePrevious(tc.state, tc.noShowBillable)
		if got.billable != tc.billable {
			t.Errorf("resolvePrevious(%s, noShowBillable=%v).billable = %v, want %v",
				tc.state, tc.noShowBillable, got.billable, tc.billable)
		}
	}
}

func TestReasonForMapsStatusToCreditReason(t *testing.T) {
	if got := reasonFor(LateCancel); got != "late_cancellation" {
		t.Errorf("late cancel reason = %q", got)
	}
	if got := reasonFor(NoShow); got != "no_show" {
		t.Errorf("no-show reason = %q", got)
	}
	if got := reasonFor(Completed); got != "session_completed" {
		t.Errorf("completed reason = %q", got)
	}
}
