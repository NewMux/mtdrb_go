// Package testsupport provides shared fixtures for integration tests.
//
// Tests here run against a real Postgres, not a mock. Row-level security,
// deferred constraint triggers and transaction semantics are precisely the
// things a mock would fake away, and they are the things most worth testing.
package testsupport

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/NewMux/mtdrb_go/internal/db"
)

// OwnerURL is the connection string for the schema owner, used to seed and
// inspect fixtures without row-level security in the way.
func OwnerURL() string { return os.Getenv("TEST_DATABASE_URL") }

// AppURL is the connection string for the RLS-bound application role. Tests
// that assert isolation must use this: as the owner, policies do not apply and
// the assertions would pass vacuously.
func AppURL() string { return os.Getenv("TEST_APP_DATABASE_URL") }

// RequireDB skips the test when no integration database is configured.
func RequireDB(t *testing.T) {
	t.Helper()
	if OwnerURL() == "" || AppURL() == "" {
		t.Skip("set TEST_DATABASE_URL and TEST_APP_DATABASE_URL to run integration tests")
	}
}

// OpenApp connects as the application role.
func OpenApp(t *testing.T) *db.Pool {
	t.Helper()
	return open(t, AppURL())
}

// OpenOwner connects as the schema owner.
func OpenOwner(t *testing.T) *db.Pool {
	t.Helper()
	return open(t, OwnerURL())
}

func open(t *testing.T, url string) *db.Pool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := db.Open(ctx, db.PoolConfig{URL: url, MaxConns: 8, MinConns: 1, StatementCache: true})
	if err != nil {
		t.Fatalf("connect to test database: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// Reset truncates all application data between tests.
//
// Truncation as the owner is deliberate: a test that has just proven a tenant
// cannot see another's rows must not then rely on that same restricted role to
// clean them up.
func Reset(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	owner := OpenOwner(t)
	// Tenants cascade to every tenant-scoped table, so this is sufficient and
	// stays correct as new tables are added.
	if _, err := owner.Raw().Exec(ctx, `TRUNCATE tenants CASCADE`); err != nil {
		t.Fatalf("reset database: %v", err)
	}
}
