// Package jobs runs the work nobody asks for: the sweeps and follow-ups that
// happen because a day turned, not because a trainer tapped something.
//
// Three rules, each learnt from what a scheduler gets wrong.
//
// One worker leads. Any number of worker processes may be running — a deploy
// overlaps the old and the new, a replica set scales — and a job that posts to
// the ledger must not run in two of them at once. The leader holds a Postgres
// advisory lock on a connection of its own for as long as it leads; the
// others wait for it. If the leader dies, its connection goes, the lock goes
// with it, and the next one takes over. No lease table, no clock skew.
//
// Every job is idempotent through a claim. Before a job does its work for a
// tenant it inserts (job, run key) into job_runs in the same transaction as
// the work. The key is the unit the job runs in — the tenant's local date,
// for a daily job — so a second attempt finds the claim and does nothing,
// and a failed attempt rolls the claim back with its work and runs again on
// the next tick. The leader lock is the efficiency; the claim is the
// correctness.
//
// Every job runs inside the tenant's own transaction. The worker reads across
// tenants only to list them (jobs_tenant_ids); the work itself is bound by
// the same row-level security as a request, so a job cannot touch another
// practice's rows even by mistake. And one tenant's failure is logged and
// passed over, never allowed to stop the sweep for everyone else.
package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NewMux/mtdrb_go/internal/db"
	"github.com/NewMux/mtdrb_go/internal/platform/clock"
	"github.com/NewMux/mtdrb_go/internal/platform/ids"
)

// Tenant is a practice, as a job sees it: whose rows, and what time it is
// where they are.
type Tenant struct {
	ID       ids.ID
	Location *time.Location
}

// Today is the tenant's local date, as a UTC midnight: the form the ledger
// and the billing engine date things in.
func (t Tenant) Today(now time.Time) time.Time {
	local := now.In(t.Location)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)
}

// Job is one piece of scheduled work, done tenant by tenant.
type Job interface {
	// Name identifies the job in job_runs and the logs. Never renamed: a
	// new name would run the job again for every period already done.
	Name() string
	// Key is the run this moment belongs to for a tenant, or "" when the job
	// is not due. Two moments with the same key are the same run.
	Key(t Tenant, now time.Time) string
	// Run does the work inside the tenant's transaction and reports how many
	// rows it affected.
	Run(ctx context.Context, tx pgx.Tx, t Tenant, now time.Time) (int, error)
}

// Daily runs a job once per tenant-local day, at or after a time of day.
type Daily struct {
	JobName string
	// At is how long after local midnight the job becomes due.
	At time.Duration
	Do func(ctx context.Context, tx pgx.Tx, t Tenant, now time.Time) (int, error)
}

// Name implements Job.
func (d Daily) Name() string { return d.JobName }

// Key implements Job: the tenant's local date, once the day is far enough
// along.
func (d Daily) Key(t Tenant, now time.Time) string {
	local := now.In(t.Location)
	midnight := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, t.Location)
	if local.Sub(midnight) < d.At {
		return ""
	}
	return local.Format("2006-01-02")
}

// Run implements Job.
func (d Daily) Run(ctx context.Context, tx pgx.Tx, t Tenant, now time.Time) (int, error) {
	return d.Do(ctx, tx, t, now)
}

// leaderLock is the advisory lock key the leading worker holds. Any constant
// works; this one is "coachpulse" folded into 63 bits so it will not collide
// with a lock some other tool on the same database happens to take.
const leaderLock int64 = 0x636f616368707573

// Runner schedules jobs across every tenant.
type Runner struct {
	pool     *db.Pool
	clock    clock.Clock
	log      *slog.Logger
	jobs     []Job
	interval time.Duration

	// done remembers runs already claimed, so a tick does not open a
	// transaction per tenant per job just to be told so by job_runs.
	mu   sync.Mutex
	done map[string]string
}

// NewRunner builds a runner. interval is how often the leader looks for due
// work; a minute is plenty for jobs measured in days.
func NewRunner(pool *db.Pool, c clock.Clock, log *slog.Logger, interval time.Duration, jobs ...Job) *Runner {
	if c == nil {
		c = clock.System{}
	}
	if interval <= 0 {
		interval = time.Minute
	}
	return &Runner{pool: pool, clock: c, log: log, jobs: jobs, interval: interval, done: map[string]string{}}
}

// Report is what one tick did.
type Report struct {
	Ran      int // job runs that did their work
	Affected int // rows those runs touched
	Failed   int // runs that failed and will be retried
}

// Tick makes one pass over every tenant and job. Safe to call from any
// number of processes at once: the claims keep each run to one.
func (r *Runner) Tick(ctx context.Context) (Report, error) {
	tenants, err := r.tenants(ctx)
	if err != nil {
		return Report{}, err
	}
	now := r.clock.Now()
	var report Report
	for _, t := range tenants {
		for _, job := range r.jobs {
			if ctx.Err() != nil {
				return report, ctx.Err()
			}
			key := job.Key(t, now)
			if key == "" || r.claimed(t, job, key) {
				continue
			}
			ran, affected, err := r.runOne(ctx, t, job, key, now)
			if err != nil {
				report.Failed++
				r.log.ErrorContext(ctx, "job failed; it will be retried",
					slog.String("job", job.Name()), slog.String("tenant_id", t.ID.String()),
					slog.String("run", key), slog.Any("error", err))
				continue
			}
			r.remember(t, job, key)
			if ran {
				report.Ran++
				report.Affected += affected
				r.log.InfoContext(ctx, "job ran",
					slog.String("job", job.Name()), slog.String("tenant_id", t.ID.String()),
					slog.String("run", key), slog.Int("affected", affected))
			}
		}
	}
	return report, nil
}

func (r *Runner) cacheKey(t Tenant, job Job) string { return t.ID.String() + "/" + job.Name() }

func (r *Runner) claimed(t Tenant, job Job, key string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.done[r.cacheKey(t, job)] == key
}

func (r *Runner) remember(t Tenant, job Job, key string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.done[r.cacheKey(t, job)] = key
}

// runOne claims a run and does it, in one transaction. ran is false when the
// run was already claimed.
func (r *Runner) runOne(ctx context.Context, t Tenant, job Job, key string, now time.Time) (ran bool, affected int, err error) {
	err = r.pool.InTenantTx(ctx, t.ID, func(tx pgx.Tx) error {
		var claim ids.ID
		err := tx.QueryRow(ctx,
			`INSERT INTO job_runs (id, tenant_id, job, run_key) VALUES ($1, $2, $3, $4)
			 ON CONFLICT (tenant_id, job, run_key) DO NOTHING RETURNING id`,
			ids.New(), t.ID, job.Name(), key).Scan(&claim)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("claim run: %w", err)
		}
		n, err := job.Run(ctx, tx, t, now)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE job_runs SET affected = $2, finished_at = $3 WHERE id = $1`,
			claim, n, now); err != nil {
			return fmt.Errorf("record run: %w", err)
		}
		ran, affected = true, n
		return nil
	})
	return ran, affected, err
}

func (r *Runner) tenants(ctx context.Context) ([]Tenant, error) {
	rows, err := r.pool.Raw().Query(ctx, `SELECT tenant_id, timezone FROM jobs_tenant_ids()`)
	if err != nil {
		return nil, fmt.Errorf("list tenants: %w", err)
	}
	defer rows.Close()
	var out []Tenant
	for rows.Next() {
		var (
			t    Tenant
			zone string
		)
		if err := rows.Scan(&t.ID, &zone); err != nil {
			return nil, fmt.Errorf("read tenant: %w", err)
		}
		loc, err := time.LoadLocation(zone)
		if err != nil {
			// Signup validates the zone, so this is a zone database that
			// has lost one. UTC is a wrong day at worst, not a missed one.
			loc = time.UTC
		}
		t.Location = loc
		out = append(out, t)
	}
	return out, rows.Err()
}

// Run leads when it can and ticks while it leads, until ctx is done.
func (r *Runner) Run(ctx context.Context) error {
	for {
		led, err := r.lead(ctx)
		if ctx.Err() != nil {
			// Cancellation is how the worker is told to stop; whatever lead
			// returned on the way out is not a failure.
			return nil //nolint:nilerr // see above
		}
		if err != nil {
			r.log.WarnContext(ctx, "lost or could not take the lead; retrying", slog.Any("error", err))
		} else if !led {
			r.log.DebugContext(ctx, "another worker leads; waiting")
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(r.interval):
		}
	}
}

// lead takes the leader lock and ticks until ctx ends or the lock's
// connection fails. It reports false when another worker holds the lock.
func (r *Runner) lead(ctx context.Context) (bool, error) {
	conn, err := r.pool.Raw().Acquire(ctx)
	if err != nil {
		return false, err
	}
	defer conn.Release()

	var got bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, leaderLock).Scan(&got); err != nil {
		return false, err
	}
	if !got {
		return false, nil
	}
	defer unlock(conn)
	r.log.InfoContext(ctx, "leading the job schedule")

	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		if _, err := r.Tick(ctx); err != nil && ctx.Err() == nil {
			r.log.ErrorContext(ctx, "tick failed", slog.Any("error", err))
		}
		select {
		case <-ctx.Done():
			return true, nil
		case <-ticker.C:
		}
		// The lock lives and dies with this connection; if it has gone, so
		// has the lead, and another worker may already hold it.
		if err := conn.Ping(ctx); err != nil {
			return true, fmt.Errorf("leader connection lost: %w", err)
		}
	}
}

func unlock(conn *pgxpool.Conn) {
	// A fresh context: the run's has usually just been cancelled, and the
	// lock should be handed over now rather than when the pool eventually
	// closes the connection.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _ = conn.Exec(ctx, `SELECT pg_advisory_unlock($1)`, leaderLock)
}

// TryLead reports whether this process could take the lead right now, and
// releases it again. For tests and for an operator's health check.
func (r *Runner) TryLead(ctx context.Context) (bool, func(), error) {
	conn, err := r.pool.Raw().Acquire(ctx)
	if err != nil {
		return false, nil, err
	}
	var got bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, leaderLock).Scan(&got); err != nil {
		conn.Release()
		return false, nil, err
	}
	if !got {
		conn.Release()
		return false, func() {}, nil
	}
	return true, func() { unlock(conn); conn.Release() }, nil
}
