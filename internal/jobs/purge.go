package jobs

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Storage removes a purged practice's files. media.S3Presigner is one.
type Storage interface {
	RemovePrefix(ctx context.Context, prefix string) error
}

// AccountPurge removes a practice for good once its owner deleted it and the
// grace period has passed. Until then the practice is only deactivated:
// nobody can sign in, and an operator can still undo it.
//
// Files go first. A failure there leaves the rows, and the job runs again the
// next day; the other order would leave photos of a deleted practice's
// clients in the bucket with nothing left that knows they are there.
func AccountPurge(grace time.Duration, storage Storage) Job {
	return Daily{
		JobName: "account_purge",
		At:      4 * time.Hour,
		Do: func(ctx context.Context, tx pgx.Tx, t Tenant, now time.Time) (int, error) {
			var deletedAt *time.Time
			if err := tx.QueryRow(ctx, `SELECT deleted_at FROM tenants`).Scan(&deletedAt); err != nil {
				return 0, fmt.Errorf("read deletion: %w", err)
			}
			if deletedAt == nil || deletedAt.After(now.Add(-grace)) {
				return 0, nil
			}
			var files bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM media_objects)`).Scan(&files); err != nil {
				return 0, fmt.Errorf("look for files: %w", err)
			}
			if files {
				if storage == nil {
					return 0, errors.New("the practice has stored files and the worker has no object storage configured (STORAGE_*)")
				}
				if err := storage.RemovePrefix(ctx, t.ID.String()+"/"); err != nil {
					return 0, fmt.Errorf("remove files: %w", err)
				}
			}
			var purged bool
			if err := tx.QueryRow(ctx, `SELECT purge_tenant_if_due($1, $2, $3)`, t.ID, now, grace).Scan(&purged); err != nil {
				return 0, fmt.Errorf("purge: %w", err)
			}
			if !purged {
				return 0, nil
			}
			return 1, nil
		},
	}
}
