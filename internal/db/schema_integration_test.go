//go:build integration

package db_test

import (
	"context"
	"strings"
	"testing"

	"github.com/NewMux/mtdrb_go/internal/testsupport"
)

// exemptTables are the tables that legitimately hold no tenant-scoped data.
// Anything not listed here must carry tenant_id and a policy.
var exemptTables = map[string]bool{
	"goose_db_version": true, // migration bookkeeping, owner-only
}

// Forgetting RLS on a new table is the single most likely way to leak one
// trainer's clients to another, and it is invisible until someone notices.
// This enumerates the live schema instead of trusting review to catch it.
func TestEveryTenantTableHasRowLevelSecurity(t *testing.T) {
	testsupport.RequireDB(t)
	owner := testsupport.OpenOwner(t)
	ctx := context.Background()

	rows, err := owner.Raw().Query(ctx, `
		SELECT c.relname,
		       c.relrowsecurity,
		       c.relforcerowsecurity,
		       EXISTS (SELECT 1 FROM pg_policy p WHERE p.polrelid = c.oid) AS has_policy,
		       EXISTS (SELECT 1 FROM information_schema.columns col
		                WHERE col.table_schema = 'public'
		                  AND col.table_name = c.relname
		                  AND col.column_name = 'tenant_id') AS has_tenant_column
		  FROM pg_class c
		  JOIN pg_namespace n ON n.oid = c.relnamespace
		 WHERE n.nspname = 'public' AND c.relkind = 'r'
		 ORDER BY c.relname`)
	if err != nil {
		t.Fatalf("inspect schema: %v", err)
	}
	defer rows.Close()

	var checked int
	for rows.Next() {
		var (
			name                          string
			rlsEnabled, rlsForced, policy bool
			tenantColumn                  bool
		)
		if err := rows.Scan(&name, &rlsEnabled, &rlsForced, &policy, &tenantColumn); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if exemptTables[name] {
			continue
		}
		checked++

		// `tenants` keys on id rather than a tenant_id column.
		if !tenantColumn && name != "tenants" {
			t.Errorf("%s: has no tenant_id column; add one or add it to exemptTables with a reason", name)
			continue
		}
		if !rlsEnabled {
			t.Errorf("%s: row-level security is not enabled", name)
		}
		// Without FORCE, the table owner silently bypasses its own policies,
		// which would make owner-run maintenance a cross-tenant read.
		if !rlsForced {
			t.Errorf("%s: row-level security is not FORCEd", name)
		}
		if !policy {
			t.Errorf("%s: row-level security is enabled but no policy is defined, so it denies everything", name)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate: %v", err)
	}
	if checked == 0 {
		t.Fatal("no tables inspected; the schema check is not actually running")
	}
	t.Logf("verified row-level security on %d tables", checked)
}

// A SECURITY DEFINER function runs with owner privileges, so an unpinned
// search_path lets a caller shadow `public` and capture those privileges.
func TestSecurityDefinerFunctionsPinSearchPath(t *testing.T) {
	testsupport.RequireDB(t)
	owner := testsupport.OpenOwner(t)
	ctx := context.Background()

	rows, err := owner.Raw().Query(ctx, `
		SELECT p.proname, coalesce(array_to_string(p.proconfig, ','), '')
		  FROM pg_proc p
		  JOIN pg_namespace n ON n.oid = p.pronamespace
		 WHERE n.nspname = 'public' AND p.prosecdef`)
	if err != nil {
		t.Fatalf("inspect functions: %v", err)
	}
	defer rows.Close()

	var found int
	for rows.Next() {
		var name, config string
		if err := rows.Scan(&name, &config); err != nil {
			t.Fatalf("scan: %v", err)
		}
		found++
		if !strings.Contains(config, "search_path=") {
			t.Errorf("%s is SECURITY DEFINER but does not pin search_path", name)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate: %v", err)
	}
	if found == 0 {
		t.Fatal("expected the auth lookup functions to be SECURITY DEFINER")
	}
}
