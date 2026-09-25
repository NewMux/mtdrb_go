package jobs

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// How long each kind of spent row is kept after it stops mattering.
const (
	// A refresh token is kept a week past its expiry. Until then a replay
	// of a spent token is recognised and revokes its family; after it, the
	// token is refused as expired in any case.
	refreshTokenGrace = 7 * 24 * time.Hour
	// A reset link lasts an hour. A day of history answers "did the email
	// arrive?"; nothing needs more.
	passwordResetGrace = 24 * time.Hour
	// An idempotency key must outlive the longest a device plausibly stays
	// offline with a payment in its outbox, or the replay would record the
	// payment twice. A month is several times any real gap.
	idempotencyRetention = 30 * 24 * time.Hour
	// A claim only has to outlive its run key: a daily job never asks about
	// a date that has passed. A quarter keeps the history worth reading.
	jobRunRetention = 90 * 24 * time.Hour
)

// Housekeeping deletes rows that have stopped meaning anything: expired
// refresh tokens and reset links, idempotency records no device can still
// replay, and old job claims. Without it these tables only grow, and the
// three that authentication reads grow on the hot path.
//
// It runs at 03:00 in each practice's own time, when nobody is signing in.
func Housekeeping() Job {
	return Daily{
		JobName: "housekeeping",
		At:      3 * time.Hour,
		Do: func(ctx context.Context, tx pgx.Tx, _ Tenant, now time.Time) (int, error) {
			sweeps := []struct {
				name  string
				query string
				age   time.Duration
			}{
				{"refresh tokens", `DELETE FROM refresh_tokens WHERE expires_at < $1`, refreshTokenGrace},
				{"password resets", `DELETE FROM password_resets WHERE expires_at < $1`, passwordResetGrace},
				{"idempotency keys", `DELETE FROM idempotency_keys WHERE created_at < $1`, idempotencyRetention},
				{"job runs", `DELETE FROM job_runs WHERE finished_at < $1`, jobRunRetention},
			}
			total := 0
			for _, s := range sweeps {
				tag, err := tx.Exec(ctx, s.query, now.Add(-s.age))
				if err != nil {
					return 0, fmt.Errorf("sweep %s: %w", s.name, err)
				}
				total += int(tag.RowsAffected())
			}
			return total, nil
		},
	}
}
