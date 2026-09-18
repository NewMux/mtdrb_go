// Command api serves the CoachPulse HTTP API.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/NewMux/mtdrb_go/internal/api"
	"github.com/NewMux/mtdrb_go/internal/auth"
	"github.com/NewMux/mtdrb_go/internal/config"
	"github.com/NewMux/mtdrb_go/internal/db"
	"github.com/NewMux/mtdrb_go/internal/ledger"
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

	wall := clock.System{}
	issuer := auth.NewTokenIssuer(cfg.JWTSigningKey, cfg.AccessTokenTTL, cfg.RefreshTokenTTL, wall)

	// Signup provisions the tenant's chart of accounts in the same transaction
	// that creates the tenant: a tenant that cannot post is not a usable one.
	ledgerSvc := ledger.NewService(wall)
	authSvc := auth.NewService(pool, issuer, ledgerSvc, wall, auth.DefaultArgon2Params())

	srv := api.New(cfg, pool, log, api.Deps{
		Auth:        auth.NewHandler(authSvc),
		TokenIssuer: issuer,
	})

	return srv.Run(ctx)
}
