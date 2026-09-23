// Command api serves the CoachPulse HTTP API.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/NewMux/mtdrb_go/internal/api"
	"github.com/NewMux/mtdrb_go/internal/app"
	"github.com/NewMux/mtdrb_go/internal/config"
	"github.com/NewMux/mtdrb_go/internal/db"
	"github.com/NewMux/mtdrb_go/internal/media"
	"github.com/NewMux/mtdrb_go/internal/platform/clock"
	"github.com/NewMux/mtdrb_go/internal/platform/logger"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "api: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	log := logger.New(logger.ParseLevel(cfg.LogLevel), logger.Format(cfg.LogFormat))

	// Cancelled on SIGINT/SIGTERM, which starts a graceful drain rather than
	// dropping in-flight requests — a request mid-ledger-post should finish.
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

	presigner, err := media.NewS3Presigner(media.S3Config{
		Endpoint:  cfg.StorageEndpoint,
		Region:    cfg.StorageRegion,
		Bucket:    cfg.StorageBucket,
		AccessKey: cfg.StorageAccessKey,
		SecretKey: cfg.StorageSecretKey,
		UseSSL:    cfg.StorageUseSSL,
	})
	if err != nil {
		return err
	}
	// Fail at startup rather than on the first progress photo.
	if err := presigner.EnsureBucket(ctx, cfg.StorageRegion); err != nil {
		return err
	}

	services := app.New(app.Options{
		Pool:            pool,
		Clock:           clock.System{},
		JWTSigningKey:   cfg.JWTSigningKey,
		AccessTokenTTL:  cfg.AccessTokenTTL,
		RefreshTokenTTL: cfg.RefreshTokenTTL,
		ColumnKey:       cfg.ColumnEncryptionKey,
		Presigner:       presigner,
		PresignTTL:      cfg.PresignTTL,
		PublicBaseURL:   cfg.PublicBaseURL,
	})
	srv := api.New(cfg, pool, log, services.Handlers())

	return srv.Run(ctx)
}
