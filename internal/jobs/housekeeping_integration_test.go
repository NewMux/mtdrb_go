//go:build integration

package jobs_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/NewMux/mtdrb_go/internal/jobs"
	"github.com/NewMux/mtdrb_go/internal/platform/clock"
	"github.com/NewMux/mtdrb_go/internal/platform/ids"
	"github.com/NewMux/mtdrb_go/internal/testsupport"
)

func TestHousekeepingDeletesOnlyWhatHasStoppedMattering(t *testing.T) {
	testsupport.RequireDB(t)
	testsupport.Reset(t)
	ctx := context.Background()
	owner := testsupport.OpenOwner(t)

	// 03:30 in Dubai: housekeeping is due.
	at := time.Date(2026, 6, 16, 23, 30, 0, 0, time.UTC)
	day := 24 * time.Hour

	tenantID, userID := ids.New(), ids.New()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := owner.Raw().Exec(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO tenants (id, name, default_currency, timezone) VALUES ($1, 'Studio', 'AED', 'Asia/Dubai')`, tenantID)
	exec(`INSERT INTO users (id, tenant_id, email, password_hash, display_name) VALUES ($1, $2, 'sam@x.test', 'h', 'Sam')`, userID, tenantID)

	token := func(expires time.Time) {
		exec(`INSERT INTO refresh_tokens (id, tenant_id, user_id, family_id, token_hash, expires_at)
		      VALUES ($1, $2, $3, $4, $5, $6)`, ids.New(), tenantID, userID, ids.New(), []byte(ids.New().String()), expires)
	}
	reset := func(expires time.Time) {
		exec(`INSERT INTO password_resets (id, tenant_id, user_id, token_hash, expires_at)
		      VALUES ($1, $2, $3, $4, $5)`, ids.New(), tenantID, userID, []byte(ids.New().String()), expires)
	}
	key := func(created time.Time) {
		exec(`INSERT INTO idempotency_keys (tenant_id, key, request_hash, endpoint, created_at)
		      VALUES ($1, $2, '\x00', '/v1/invoices', $3)`, tenantID, ids.New().String(), created)
	}
	claim := func(finished time.Time) {
		exec(`INSERT INTO job_runs (id, tenant_id, job, run_key, finished_at) VALUES ($1, $2, 'package_expiry', $3, $4)`,
			ids.New(), tenantID, ids.New().String(), finished)
	}

	token(at.Add(-30 * day)) // gone
	token(at.Add(-2 * day))  // expired, but a replay is still recognised
	token(at.Add(20 * day))  // live
	reset(at.Add(-3 * day))  // gone
	reset(at.Add(-time.Hour))
	key(at.Add(-45 * day)) // gone
	key(at.Add(-10 * day)) // a device a week offline can still replay
	claim(at.Add(-120 * day))
	claim(at.Add(-10 * day))

	runner := jobs.NewRunner(testsupport.OpenApp(t), clock.Fixed{T: at},
		slog.New(slog.NewTextHandler(io.Discard, nil)), time.Minute, jobs.Housekeeping())
	report, err := runner.Tick(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if report.Ran != 1 || report.Affected != 4 {
		t.Fatalf("report = %+v, want one run deleting four rows", report)
	}

	count := func(table string) int {
		var n int
		if err := owner.Raw().QueryRow(ctx, `SELECT count(*) FROM `+table).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	// job_runs also holds today's housekeeping claim.
	for table, want := range map[string]int{"refresh_tokens": 2, "password_resets": 1, "idempotency_keys": 1, "job_runs": 2} {
		if got := count(table); got != want {
			t.Errorf("%s: %d rows left, want %d", table, got, want)
		}
	}

	// A second tick the same day does nothing.
	if report, _ := runner.Tick(ctx); report.Ran != 0 {
		t.Errorf("housekeeping ran twice in one day: %+v", report)
	}
}
