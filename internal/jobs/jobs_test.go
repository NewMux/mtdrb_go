package jobs

import (
	"testing"
	"time"
)

func TestADailyJobIsDueOncePerLocalDay(t *testing.T) {
	dubai, _ := time.LoadLocation("Asia/Dubai")
	tenant := Tenant{Location: dubai}
	job := Daily{JobName: "x", At: 15 * time.Minute}

	// 00:10 in Dubai on the 17th is still 20:10 UTC on the 16th.
	if key := job.Key(tenant, time.Date(2026, 6, 16, 20, 10, 0, 0, time.UTC)); key != "" {
		t.Fatalf("not due before 00:15 local, got %q", key)
	}
	if key := job.Key(tenant, time.Date(2026, 6, 16, 20, 20, 0, 0, time.UTC)); key != "2026-06-17" {
		t.Fatalf("due at 00:20 local as the 17th, got %q", key)
	}
	if key := job.Key(tenant, time.Date(2026, 6, 17, 19, 59, 0, 0, time.UTC)); key != "2026-06-17" {
		t.Fatalf("23:59 local is the same run, got %q", key)
	}
}

func TestTodayIsTheTenantsDate(t *testing.T) {
	dubai, _ := time.LoadLocation("Asia/Dubai")
	got := Tenant{Location: dubai}.Today(time.Date(2026, 6, 16, 21, 0, 0, 0, time.UTC))
	if !got.Equal(time.Date(2026, 6, 17, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("today in Dubai = %v", got)
	}
}
