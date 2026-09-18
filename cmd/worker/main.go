// Command worker runs CoachPulse's scheduled jobs: recurring invoice drafts,
// overdue transitions and package expiry.
//
// The job handlers arrive with the billing engine (M5). This entrypoint
// establishes the process shape — configuration, pool, signal handling — so
// that the deployment target exists before the work that fills it.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/NewMux/mtdrb_go/internal/config"
	"github.com/NewMux/mtdrb_go/internal/db"
	"github.com/NewMux/mtdrb_go/internal/platform/logger"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "worker: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := logger.New(logger.ParseLevel(cfg.LogLevel), logger.Format(cfg.LogFormat))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := db.Open(ctx, db.PoolConfig{
		URL:             cfg.DatabaseURL,
		MaxConns:        cfg.DBMaxConns,
		MinConns:        cfg.DBMinConns,
		MaxConnLifetime: cfg.DBConnMaxLife,
		StatementCache:  cfg.DBStatementCache,
	})
	if err != nil {
		return err
	}
	defer pool.Close()

	log.Info("worker started; no job handlers registered yet")
	<-ctx.Done()
	log.Info("worker stopped")
	return nil
}
