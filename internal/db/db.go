// Package db owns the connection pool and the transaction helpers that bind
// tenant context.
//
// Nothing in this codebase queries a tenant-scoped table outside one of the
// helpers here. Row-level security only protects rows if `app.tenant_id` is
// actually set, and `SET LOCAL` only lasts for a transaction — so "open a
// transaction, set the GUC, then query" is not a convention, it is the
// mechanism. See ADR 0002.
package db

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NewMux/mtdrb_go/internal/platform/ids"
)

// Querier is the subset of pgx used by repositories. Both *pgxpool.Pool and
// pgx.Tx satisfy it, so repository code compiles against either without
// knowing whether it is inside a transaction.
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Pool wraps a pgx pool with the tenant-binding helpers.
type Pool struct {
	pool *pgxpool.Pool
}

// PoolConfig configures the connection pool.
type PoolConfig struct {
	URL             string
	MaxConns        int32
	MinConns        int32
	MaxConnLifetime time.Duration
	StatementCache  bool
	// StatementTimeout makes Postgres cancel any single statement that runs
	// longer. A runaway query then fails one request instead of holding a
	// pooled connection, and the locks it took, until every request queues
	// behind it. Zero leaves the server's setting alone.
	StatementTimeout time.Duration
}

// Open connects and verifies the database is reachable.
func Open(ctx context.Context, cfg PoolConfig) (*Pool, error) {
	pc, err := pgxpool.ParseConfig(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	if cfg.MaxConns > 0 {
		pc.MaxConns = cfg.MaxConns
	}
	if cfg.MinConns > 0 {
		pc.MinConns = cfg.MinConns
	}
	if cfg.MaxConnLifetime > 0 {
		pc.MaxConnLifetime = cfg.MaxConnLifetime
	}
	if cfg.StatementTimeout > 0 {
		pc.ConnConfig.RuntimeParams["statement_timeout"] = strconv.FormatInt(cfg.StatementTimeout.Milliseconds(), 10)
	}
	if !cfg.StatementCache {
		// Useful behind transaction-pooling proxies such as PgBouncer, which
		// cannot guarantee a prepared statement survives to its next use.
		pc.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	}

	pool, err := pgxpool.NewWithConfig(ctx, pc)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return &Pool{pool: pool}, nil
}

// Close releases all connections.
func (p *Pool) Close() { p.pool.Close() }

// Raw exposes the underlying pool for queries that are deliberately not
// tenant-scoped, such as resolving a login email to its tenant.
func (p *Pool) Raw() *pgxpool.Pool { return p.pool }

// Ping verifies connectivity for health checks.
func (p *Pool) Ping(ctx context.Context) error { return p.pool.Ping(ctx) }

// InTx runs fn inside a transaction with no tenant context established.
//
// Use it only for genuinely cross-tenant work: signup, and login before the
// tenant is known. Tenant-scoped tables return nothing here, by design.
func (p *Pool) InTx(ctx context.Context, fn func(pgx.Tx) error) error {
	return p.txWith(ctx, nil, fn)
}

// InTenantTx runs fn inside a transaction bound to tenantID.
//
// This is the helper nearly all application code uses.
func (p *Pool) InTenantTx(ctx context.Context, tenantID ids.ID, fn func(pgx.Tx) error) error {
	return p.txWith(ctx, &binding{tenant: tenantID}, fn)
}

// InPortalTx binds both a tenant and a client identity, for requests
// authenticated as a training client rather than a trainer. Portal policies
// narrow visibility further using current_client_id().
func (p *Pool) InPortalTx(ctx context.Context, tenantID, clientID ids.ID, fn func(pgx.Tx) error) error {
	return p.txWith(ctx, &binding{tenant: tenantID, client: &clientID}, fn)
}

type binding struct {
	tenant ids.ID
	client *ids.ID
}

func (p *Pool) txWith(ctx context.Context, b *binding, fn func(pgx.Tx) error) (err error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	// Roll back on panic as well as on error; a panic mid-ledger-post must not
	// leave a half-written journal entry behind.
	defer func() {
		if r := recover(); r != nil {
			_ = tx.Rollback(context.WithoutCancel(ctx))
			panic(r)
		}
		if err != nil {
			_ = tx.Rollback(context.WithoutCancel(ctx))
		}
	}()

	if b != nil {
		if err = bindTenant(ctx, tx, b); err != nil {
			return err
		}
	}

	if err = fn(tx); err != nil {
		return err
	}

	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

// bindTenant sets the transaction-local GUCs the RLS policies read.
//
// set_config with is_local=true is used rather than `SET LOCAL ...` string
// interpolation so the identifier is passed as a bound parameter. The values
// are UUIDs from validated tokens, but building SQL by concatenation around
// anything tenant-derived is a habit worth not having.
func bindTenant(ctx context.Context, tx pgx.Tx, b *binding) error {
	if b.tenant == ids.Nil {
		return errors.New("db: refusing to bind an empty tenant id")
	}
	if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, b.tenant.String()); err != nil {
		return fmt.Errorf("bind tenant context: %w", err)
	}
	if b.client != nil {
		if *b.client == ids.Nil {
			return errors.New("db: refusing to bind an empty client id")
		}
		if _, err := tx.Exec(ctx, `SELECT set_config('app.client_id', $1, true)`, b.client.String()); err != nil {
			return fmt.Errorf("bind client context: %w", err)
		}
	}
	return nil
}

// IsUniqueViolation reports whether err is a unique-constraint failure,
// optionally narrowed to a named constraint. Repositories use it to turn a
// race into a typed conflict rather than a 500.
func IsUniqueViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		return false
	}
	return constraint == "" || pgErr.ConstraintName == constraint
}

// IsForeignKeyViolation reports whether err is a foreign-key failure.
func IsForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23503"
}

// IsCheckViolation reports whether err is a check-constraint failure,
// optionally narrowed to a named constraint. The ledger's balance assertion
// surfaces this way.
func IsCheckViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	// 23514 is a plain CHECK; P0001 is a raise_exception from a trigger, which
	// is how the deferred journal-balance assertion reports itself.
	if pgErr.Code != "23514" && pgErr.Code != "P0001" {
		return false
	}
	return constraint == "" || pgErr.ConstraintName == constraint
}

// IsRLSViolation reports whether err is a row-level security policy refusal,
// which is what a cross-tenant write attempt produces.
func IsRLSViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "42501"
}

// IsNoRows reports whether a query returned nothing.
func IsNoRows(err error) bool { return errors.Is(err, pgx.ErrNoRows) }

// IsSQLState reports whether err is a Postgres error with the given SQLSTATE.
// Used for codes without a dedicated helper, such as 23P01 (exclusion
// constraint violation).
func IsSQLState(err error, code string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == code
}
