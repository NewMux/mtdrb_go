// Command worker runs CoachPulse's scheduled jobs.
//
// Any number of copies may run; one leads at a time (see package jobs), and
// every job is safe to run twice, so a deploy that overlaps old and new
// workers, or a crash halfway through a sweep, costs nothing.
//
//	DATABASE_URL=postgres://coachpulse_app:…@…/coachpulse go run ./cmd/worker
//
// Jobs today: package expiry, a quarter past each practice's local midnight.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/NewMux/mtdrb_go/internal/app"
	"github.com/NewMux/mtdrb_go/internal/config"
	"github.com/NewMux/mtdrb_go/internal/db"
	"github.com/NewMux/mtdrb_go/internal/jobs"
	"github.com/NewMux/mtdrb_go/internal/media"
	"github.com/NewMux/mtdrb_go/internal/platform/clock"
	"github.com/NewMux/mtdrb_go/internal/platform/errreport"
	"github.com/NewMux/mtdrb_go/internal/platform/logger"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "worker: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.LoadWorker()
	if err != nil {
		return err
	}
	log := logger.New(logger.ParseLevel(cfg.LogLevel), logger.Format(cfg.LogFormat))
	report, flush, err := errreport.Setup(cfg.SentryDSN, cfg.Env, cfg.Release, "worker")
	if err != nil {
		return err
	}
	defer flush()
	log = logger.WithReporter(log, report)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := db.Open(ctx, db.PoolConfig{
		URL:              cfg.DatabaseURL,
		MaxConns:         cfg.DBMaxConns,
		MinConns:         cfg.DBMinConns,
		MaxConnLifetime:  cfg.DBConnMaxLife,
		StatementCache:   cfg.DBStatementCache,
		StatementTimeout: cfg.DBStatementTimeout,
	})
	if err != nil {
		return err
	}
	defer pool.Close()

	wall := clock.System{}
	// Object storage is optional for the worker: only purging a deleted
	// practice touches it, and that job says so if it is missing.
	var storage jobs.Storage
	if cfg.StorageAccessKey != "" {
		presigner, err := media.NewS3Presigner(media.S3Config{
			Endpoint: cfg.StorageEndpoint, Region: cfg.StorageRegion, Bucket: cfg.StorageBucket,
			AccessKey: cfg.StorageAccessKey, SecretKey: cfg.StorageSecretKey, UseSSL: cfg.StorageUseSSL,
		})
		if err != nil {
			return err
		}
		storage = presigner
	}
	services := app.New(app.Options{Pool: pool, Clock: wall, Storage: storage, PurgeAfter: cfg.AccountPurgeAfter})
	runner := jobs.NewRunner(pool, wall, log, cfg.JobInterval, services.Jobs()...)

	log.Info("worker started", slog.Duration("interval", cfg.JobInterval))
	err = runner.Run(ctx)
	log.Info("worker stopped")
	return err
}
