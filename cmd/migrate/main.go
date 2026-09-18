// Command migrate applies database schema migrations.
//
// It connects as the database owner, not as the RLS-bound application role:
// creating tables, enabling row-level security and granting rights are owner
// operations. The running API never has these privileges.
package main

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/pressly/goose/v3"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/NewMux/mtdrb_go/internal/db"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "migrate: %v\n", err)
		os.Exit(1)
	}
}

func usage() string {
	return `usage: migrate <command> [arg]

commands:
  up              apply all pending migrations
  up-by-one       apply the next pending migration
  down            roll back the most recent migration
  redo            roll back and reapply the most recent migration
  reset           roll back every migration
  status          print the state of each migration
  version         print the current schema version
  create <name>   scaffold a new timestamped migration`
}

func run() error {
	args := os.Args[1:]
	if len(args) == 0 {
		return errors.New(usage())
	}
	command := args[0]

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return errors.New("DATABASE_URL is required")
	}

	goose.SetBaseFS(migrationsFS())
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("set dialect: %w", err)
	}

	if command == "create" {
		if len(args) < 2 {
			return errors.New("create requires a migration name")
		}
		return goose.Create(nil, "internal/db/migrations", args[1], "sql")
	}

	conn, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer func() { _ = conn.Close() }()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := ping(ctx, conn); err != nil {
		return err
	}

	rest := args[1:]
	switch command {
	case "up":
		return goose.UpContext(ctx, conn, db.MigrationsDir)
	case "up-by-one":
		return goose.UpByOneContext(ctx, conn, db.MigrationsDir)
	case "up-to":
		v, err := parseVersion(rest)
		if err != nil {
			return err
		}
		return goose.UpToContext(ctx, conn, db.MigrationsDir, v)
	case "down":
		return goose.DownContext(ctx, conn, db.MigrationsDir)
	case "down-to":
		v, err := parseVersion(rest)
		if err != nil {
			return err
		}
		return goose.DownToContext(ctx, conn, db.MigrationsDir, v)
	case "redo":
		return goose.RedoContext(ctx, conn, db.MigrationsDir)
	case "reset":
		return goose.ResetContext(ctx, conn, db.MigrationsDir)
	case "status":
		return goose.StatusContext(ctx, conn, db.MigrationsDir)
	case "version":
		return goose.VersionContext(ctx, conn, db.MigrationsDir)
	default:
		return fmt.Errorf("unknown command %q\n\n%s", command, usage())
	}
}

func parseVersion(rest []string) (int64, error) {
	if len(rest) < 1 {
		return 0, errors.New("this command requires a target version")
	}
	v, err := strconv.ParseInt(rest[0], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("version must be an integer, got %q", rest[0])
	}
	return v, nil
}

// ping retries briefly so `make up` works when Postgres is still accepting
// its first connections.
func ping(ctx context.Context, conn *sql.DB) error {
	const attempts = 10
	var err error
	for i := range attempts {
		if err = conn.PingContext(ctx); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(i+1) * 250 * time.Millisecond):
		}
	}
	return fmt.Errorf("database unreachable after %d attempts: %w", attempts, err)
}

func migrationsFS() embed.FS { return db.Migrations }
