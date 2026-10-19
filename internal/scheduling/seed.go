package scheduling

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/NewMux/mtdrb_go/internal/platform/errs"
	"github.com/NewMux/mtdrb_go/internal/platform/ids"
)

// defaultSessionTypes are what most independent trainers sell: an hour one to
// one, a shared hour for two, and a shorter first-meeting assessment.
var defaultSessionTypes = []struct {
	name     string
	minutes  int
	capacity int
	colour   string
}{
	{"1-on-1", 60, 1, "#007AFF"},
	{"Duo", 60, 2, "#AF52DE"},
	{"Assessment", 45, 1, "#34C759"},
}

// SeedSessionTypes installs the opening session types for a new tenant.
//
// Run at signup, in the tenant's own transaction, for the same reason as the
// exercise library: a trainer who opens the calendar for the first time
// should be able to book someone, not configure a product catalogue first.
// The app's booking sheet needs at least one type to offer.
//
// Idempotent on the tenant's unique name, so re-running it is harmless.
func (s *Service) SeedSessionTypes(ctx context.Context, tx pgx.Tx, tenantID ids.ID) error {
	for _, t := range defaultSessionTypes {
		if _, err := tx.Exec(ctx, `
			INSERT INTO session_types (id, tenant_id, name, duration_minutes, capacity, credit_cost, colour)
			VALUES ($1, $2, $3, $4, $5, 1, $6)
			ON CONFLICT DO NOTHING`,
			ids.New(), tenantID, t.name, t.minutes, t.capacity, t.colour); err != nil {
			return errs.Internal(err, "seed session types")
		}
	}
	return nil
}

// SeedLibrary lets the scheduling service sit in signup's list of seeders.
func (s *Service) SeedLibrary(ctx context.Context, tx pgx.Tx, tenantID ids.ID) error {
	return s.SeedSessionTypes(ctx, tx, tenantID)
}
