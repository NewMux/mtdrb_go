// Command admin changes the one thing the product does not let a trainer
// change: their plan.
//
// Until a payment provider writes plan state, this is how a practice moves
// from trial to paid, gets more trial, or is cancelled. It writes the same
// columns a provider's webhook will (subscription.Apply), so nothing about
// that switch-over changes what the product reads.
//
//	OWNER_DATABASE_URL=postgres://… go run ./cmd/admin list-tenants
//	OWNER_DATABASE_URL=postgres://… go run ./cmd/admin set-plan <tenant-id> pro [-renews-on 2026-12-01] [-status active]
//	OWNER_DATABASE_URL=postgres://… go run ./cmd/admin extend-trial <tenant-id> <days>
//
// It connects as the database owner, not the app role: listing tenants is
// reading across them, which is exactly what the app role cannot do. The
// owner is the role migrations run as, which row-level security does not
// bind (it is the superuser locally; see deploy/postgres-init).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"text/tabwriter"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/NewMux/mtdrb_go/internal/platform/ids"
	"github.com/NewMux/mtdrb_go/internal/subscription"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "admin: %v\n", err)
		os.Exit(1)
	}
}

const usage = `usage:
  admin list-tenants
  admin set-plan <tenant-id> <trial|starter|pro> [-status active|past_due|cancelled] [-renews-on YYYY-MM-DD]
  admin extend-trial <tenant-id> <days>
  admin restore-tenant <tenant-id>    undo an owner's deletion, within the grace period`

func run(args []string) error {
	if len(args) == 0 {
		return errors.New(usage)
	}
	url := os.Getenv("OWNER_DATABASE_URL")
	if url == "" {
		return errors.New("OWNER_DATABASE_URL is required (the database owner, not the app role)")
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer func() { _ = conn.Close(ctx) }()

	// Every table forces row-level security, on the owner too, so an operator
	// reads across tenants the one sanctioned way: as the role that owns the
	// cross-tenant functions and holds the policy for it.
	if _, err := conn.Exec(ctx, `SET ROLE coachpulse_definer`); err != nil {
		return fmt.Errorf("act as coachpulse_definer (is OWNER_DATABASE_URL the migrating role?): %w", err)
	}

	switch args[0] {
	case "list-tenants":
		return listTenants(ctx, conn)
	case "set-plan":
		return setPlan(ctx, conn, args[1:])
	case "extend-trial":
		return extendTrial(ctx, conn, args[1:])
	case "restore-tenant":
		return restoreTenant(ctx, conn, args[1:])
	}
	return errors.New(usage)
}

func listTenants(ctx context.Context, conn *pgx.Conn) error {
	rows, err := conn.Query(ctx, `
		SELECT t.id, t.name, t.plan, t.plan_status, t.trial_ends_at, t.plan_renews_on,
		       t.cancel_at_period_end, t.created_at, t.deleted_at,
		       coalesce((SELECT email FROM users u WHERE u.tenant_id = t.id AND u.role = 'owner'
		                  ORDER BY u.created_at LIMIT 1), '')
		  FROM tenants t ORDER BY t.created_at`)
	if err != nil {
		return err
	}
	defer rows.Close()

	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "TENANT\tNAME\tOWNER\tPLAN\tSTATUS\tTRIAL ENDS\tRENEWS\tCANCELLING\tCREATED\tDELETED")
	for rows.Next() {
		var (
			id                 ids.ID
			name, plan, status string
			owner              string
			trialEnds          *time.Time
			renews             *time.Time
			cancelling         bool
			created            time.Time
			deleted            *time.Time
		)
		if err := rows.Scan(&id, &name, &plan, &status, &trialEnds, &renews, &cancelling, &created, &deleted, &owner); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%v\t%s\t%s\n",
			id, name, owner, plan, status, day(trialEnds), day(renews), cancelling, created.Format("2006-01-02"), day(deleted))
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return w.Flush()
}

func day(t *time.Time) string {
	if t == nil {
		return "-"
	}
	return t.Format("2006-01-02")
}

func setPlan(ctx context.Context, conn *pgx.Conn, args []string) error {
	if len(args) < 2 {
		return errors.New(usage)
	}
	tenantID, err := ids.Parse(args[0])
	if err != nil {
		return fmt.Errorf("tenant id: %w", err)
	}
	plan := subscription.Plan(args[1])
	flags := flag.NewFlagSet("set-plan", flag.ContinueOnError)
	status := flags.String("status", "active", "active, past_due or cancelled")
	renewsOn := flags.String("renews-on", "", "the date the paid period ends, YYYY-MM-DD")
	if err := flags.Parse(args[2:]); err != nil {
		return err
	}

	change := subscription.Change{Plan: &plan}
	st := subscription.Status(*status)
	change.Status = &st
	if *renewsOn != "" {
		on, err := time.Parse("2006-01-02", *renewsOn)
		if err != nil {
			return fmt.Errorf("renews-on: %w", err)
		}
		change.RenewsOn = &on
	}
	if plan == subscription.PlanTrial {
		// Back to trial needs an end; give it a fresh one.
		ends := time.Now().Add(subscription.TrialLength)
		change.TrialEndsAt = &ends
	}
	return apply(ctx, conn, tenantID, change)
}

func extendTrial(ctx context.Context, conn *pgx.Conn, args []string) error {
	if len(args) != 2 {
		return errors.New(usage)
	}
	tenantID, err := ids.Parse(args[0])
	if err != nil {
		return fmt.Errorf("tenant id: %w", err)
	}
	days, err := strconv.Atoi(args[1])
	if err != nil || days <= 0 || days > 365 {
		return errors.New("days must be a whole number from 1 to 365")
	}

	var current *time.Time
	if err := conn.QueryRow(ctx, `SELECT trial_ends_at FROM tenants WHERE id = $1`, tenantID).Scan(&current); err != nil {
		return fmt.Errorf("read tenant: %w", err)
	}
	// From whichever is later, so extending a lapsed trial gives the days in
	// full rather than some of them in the past.
	from := time.Now()
	if current != nil && current.After(from) {
		from = *current
	}
	ends := from.Add(time.Duration(days) * 24 * time.Hour)
	plan := subscription.PlanTrial
	return apply(ctx, conn, tenantID, subscription.Change{Plan: &plan, TrialEndsAt: &ends})
}

func apply(ctx context.Context, conn *pgx.Conn, tenantID ids.ID, change subscription.Change) error {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// Bound like any request, so the update runs under the same policies and
	// the sync sequence bump reaches the tenant's devices.
	if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, tenantID.String()); err != nil {
		return err
	}
	if err := subscription.Apply(ctx, tx, tenantID, change); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	fmt.Printf("updated %s\n", tenantID)
	return nil
}

// restoreTenant undoes an owner's deletion before the purge runs: the
// practice and the members its deletion deactivated come back. Sessions do
// not; everyone signs in again.
func restoreTenant(ctx context.Context, conn *pgx.Conn, args []string) error {
	if len(args) != 1 {
		return errors.New(usage)
	}
	tenantID, err := ids.Parse(args[0])
	if err != nil {
		return fmt.Errorf("tenant id: %w", err)
	}
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var deleted *time.Time
	if err := tx.QueryRow(ctx, `SELECT deleted_at FROM tenants WHERE id = $1 FOR UPDATE`, tenantID).Scan(&deleted); err != nil {
		return fmt.Errorf("read tenant (already purged?): %w", err)
	}
	if deleted == nil {
		return errors.New("the tenant is not deleted")
	}
	if _, err := tx.Exec(ctx,
		`UPDATE users SET deactivated_at = NULL WHERE tenant_id = $1 AND deactivated_at = $2`, tenantID, *deleted); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE tenants SET deleted_at = NULL WHERE id = $1`, tenantID); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	fmt.Printf("restored %s\n", tenantID)
	return nil
}
