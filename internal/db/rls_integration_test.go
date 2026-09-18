//go:build integration

package db_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/NewMux/mtdrb_go/internal/db"
	"github.com/NewMux/mtdrb_go/internal/platform/ids"
	"github.com/NewMux/mtdrb_go/internal/testsupport"
)

// tenantFixture is a tenant with one user, seeded as the owner.
type tenantFixture struct {
	TenantID ids.ID
	UserID   ids.ID
	Email    string
}

func seedTenant(t *testing.T, name, email string) tenantFixture {
	t.Helper()
	ctx := context.Background()
	owner := testsupport.OpenOwner(t)

	f := tenantFixture{TenantID: ids.New(), UserID: ids.New(), Email: email}
	if _, err := owner.Raw().Exec(ctx,
		`INSERT INTO tenants (id, name) VALUES ($1, $2)`, f.TenantID, name); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	if _, err := owner.Raw().Exec(ctx,
		`INSERT INTO users (id, tenant_id, email, password_hash, display_name)
		 VALUES ($1, $2, $3, 'x', $4)`, f.UserID, f.TenantID, f.Email, name); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return f
}

// The central guarantee of ADR 0002: a tenant-bound transaction sees its own
// rows and nothing else.
func TestTenantTxSeesOnlyItsOwnRows(t *testing.T) {
	testsupport.RequireDB(t)
	testsupport.Reset(t)

	alice := seedTenant(t, "Alice Fitness", "alice@example.com")
	bob := seedTenant(t, "Bob Strength", "bob@example.com")

	app := testsupport.OpenApp(t)
	ctx := context.Background()

	for _, tc := range []struct {
		name  string
		who   tenantFixture
		other tenantFixture
	}{
		{"alice", alice, bob},
		{"bob", bob, alice},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := app.InTenantTx(ctx, tc.who.TenantID, func(tx pgx.Tx) error {
				var count int
				if err := tx.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&count); err != nil {
					return err
				}
				if count != 1 {
					t.Errorf("sees %d users, want exactly its own 1", count)
				}

				var email string
				if err := tx.QueryRow(ctx, `SELECT email FROM users`).Scan(&email); err != nil {
					return err
				}
				if email != tc.who.Email {
					t.Errorf("sees %q, want %q", email, tc.who.Email)
				}

				// Targeting the other tenant's row by primary key must still
				// find nothing: the policy filters before the key lookup.
				var leaked string
				err := tx.QueryRow(ctx, `SELECT email FROM users WHERE id = $1`, tc.other.UserID).Scan(&leaked)
				if !db.IsNoRows(err) {
					t.Errorf("cross-tenant lookup by id returned %q (err=%v); expected no rows", leaked, err)
				}
				return nil
			})
			if err != nil {
				t.Fatalf("tenant tx: %v", err)
			}
		})
	}
}

// Without tenant context the policies match nothing, so a handler that forgets
// to scope fails closed rather than leaking everything.
func TestUnboundTxSeesNothing(t *testing.T) {
	testsupport.RequireDB(t)
	testsupport.Reset(t)
	seedTenant(t, "Alice Fitness", "alice@example.com")

	app := testsupport.OpenApp(t)
	ctx := context.Background()

	err := app.InTx(ctx, func(tx pgx.Tx) error {
		for _, table := range []string{"tenants", "users", "refresh_tokens", "idempotency_keys"} {
			var count int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM `+table).Scan(&count); err != nil {
				return err
			}
			if count != 0 {
				t.Errorf("%s: unbound transaction sees %d rows, want 0", table, count)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("unbound tx: %v", err)
	}
}

// Writing a row belonging to another tenant must be refused by WITH CHECK,
// not merely hidden afterwards.
func TestCrossTenantInsertIsRejected(t *testing.T) {
	testsupport.RequireDB(t)
	testsupport.Reset(t)

	alice := seedTenant(t, "Alice Fitness", "alice@example.com")
	bob := seedTenant(t, "Bob Strength", "bob@example.com")

	app := testsupport.OpenApp(t)
	ctx := context.Background()

	err := app.InTenantTx(ctx, alice.TenantID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			`INSERT INTO users (id, tenant_id, email, password_hash, display_name)
			 VALUES ($1, $2, 'planted@example.com', 'x', 'Planted')`,
			ids.New(), bob.TenantID)
		return err
	})
	if err == nil {
		t.Fatal("insert into another tenant succeeded; RLS WITH CHECK is not enforced")
	}
	if !db.IsRLSViolation(err) {
		t.Errorf("expected an RLS violation, got %v", err)
	}
}

// A tenant must not be able to reassign its own row to another tenant.
func TestCrossTenantUpdateIsRejected(t *testing.T) {
	testsupport.RequireDB(t)
	testsupport.Reset(t)

	alice := seedTenant(t, "Alice Fitness", "alice@example.com")
	bob := seedTenant(t, "Bob Strength", "bob@example.com")

	app := testsupport.OpenApp(t)
	ctx := context.Background()

	err := app.InTenantTx(ctx, alice.TenantID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			`UPDATE users SET tenant_id = $1 WHERE id = $2`, bob.TenantID, alice.UserID)
		return err
	})
	if err == nil {
		t.Fatal("reassigning a row to another tenant succeeded")
	}
	if !db.IsRLSViolation(err) {
		t.Errorf("expected an RLS violation, got %v", err)
	}
}

// A DELETE aimed at another tenant must affect nothing.
func TestCrossTenantDeleteAffectsNothing(t *testing.T) {
	testsupport.RequireDB(t)
	testsupport.Reset(t)

	alice := seedTenant(t, "Alice Fitness", "alice@example.com")
	bob := seedTenant(t, "Bob Strength", "bob@example.com")

	app := testsupport.OpenApp(t)
	ctx := context.Background()

	if err := app.InTenantTx(ctx, alice.TenantID, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `DELETE FROM users WHERE id = $1`, bob.UserID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 0 {
			t.Errorf("deleted %d of another tenant's rows", tag.RowsAffected())
		}
		return nil
	}); err != nil {
		t.Fatalf("tenant tx: %v", err)
	}

	// Confirm as the owner that Bob's row survived.
	owner := testsupport.OpenOwner(t)
	var count int
	if err := owner.Raw().QueryRow(ctx, `SELECT count(*) FROM users WHERE id = $1`, bob.UserID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Error("another tenant's row was deleted")
	}
}

// The tenant GUC must not survive its transaction, or a pooled connection
// would carry one tenant's identity into the next request.
func TestTenantContextDoesNotLeakAcrossTransactions(t *testing.T) {
	testsupport.RequireDB(t)
	testsupport.Reset(t)
	alice := seedTenant(t, "Alice Fitness", "alice@example.com")

	app := testsupport.OpenApp(t)
	ctx := context.Background()

	if err := app.InTenantTx(ctx, alice.TenantID, func(tx pgx.Tx) error { return nil }); err != nil {
		t.Fatal(err)
	}

	// Run enough unbound transactions to make reuse of the same pooled
	// connection overwhelmingly likely.
	for i := range 20 {
		if err := app.InTx(ctx, func(tx pgx.Tx) error {
			var count int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&count); err != nil {
				return err
			}
			if count != 0 {
				t.Fatalf("iteration %d: tenant context leaked; saw %d rows", i, count)
			}
			return nil
		}); err != nil {
			t.Fatalf("unbound tx: %v", err)
		}
	}
}

// A rolled-back transaction must leave nothing behind; the ledger depends on
// this for atomic credit-burn plus journal-post.
func TestTransactionRollsBackOnError(t *testing.T) {
	testsupport.RequireDB(t)
	testsupport.Reset(t)
	alice := seedTenant(t, "Alice Fitness", "alice@example.com")

	app := testsupport.OpenApp(t)
	ctx := context.Background()
	sentinel := ids.New()

	err := app.InTenantTx(ctx, alice.TenantID, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx,
			`INSERT INTO users (id, tenant_id, email, password_hash, display_name)
			 VALUES ($1, $2, 'rollback@example.com', 'x', 'Rollback')`,
			sentinel, alice.TenantID); err != nil {
			return err
		}
		return context.Canceled // force a rollback
	})
	if err == nil {
		t.Fatal("expected the callback error to propagate")
	}

	owner := testsupport.OpenOwner(t)
	var count int
	if err := owner.Raw().QueryRow(ctx, `SELECT count(*) FROM users WHERE id = $1`, sentinel).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Error("row survived a rolled-back transaction")
	}
}

func TestBindingAnEmptyTenantIsRefused(t *testing.T) {
	testsupport.RequireDB(t)
	app := testsupport.OpenApp(t)

	err := app.InTenantTx(context.Background(), ids.Nil, func(tx pgx.Tx) error {
		t.Error("callback must not run with an empty tenant id")
		return nil
	})
	if err == nil {
		t.Fatal("expected binding an empty tenant id to be refused")
	}
}
