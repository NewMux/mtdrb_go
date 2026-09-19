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
	"github.com/NewMux/mtdrb_go/internal/billing"
	"github.com/NewMux/mtdrb_go/internal/config"
	"github.com/NewMux/mtdrb_go/internal/crm"
	"github.com/NewMux/mtdrb_go/internal/dashboard"
	"github.com/NewMux/mtdrb_go/internal/db"
	"github.com/NewMux/mtdrb_go/internal/ledger"
	"github.com/NewMux/mtdrb_go/internal/media"
	"github.com/NewMux/mtdrb_go/internal/platform/clock"
	"github.com/NewMux/mtdrb_go/internal/platform/logger"
	"github.com/NewMux/mtdrb_go/internal/programming"
	"github.com/NewMux/mtdrb_go/internal/scheduling"
	"github.com/NewMux/mtdrb_go/internal/sync"
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
	programmingSvc := programming.NewService(wall)
	authSvc := auth.NewService(pool, issuer, ledgerSvc, programmingSvc, wall, auth.DefaultArgon2Params())

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

	crmSvc := crm.NewService(wall, cfg.ColumnEncryptionKey)
	mediaSvc := media.NewService(presigner, wall, cfg.PresignTTL)
	billingSvc := billing.NewService(ledgerSvc, wall)
	schedulingSvc := scheduling.NewService(billingSvc, ledgerSvc, wall)

	srv := api.New(cfg, pool, log, api.Deps{
		Auth:        auth.NewHandler(authSvc),
		CRM:         crm.NewHandler(crmSvc, pool),
		Media:       media.NewHandler(mediaSvc, pool),
		Scheduling:  scheduling.NewHandler(schedulingSvc, billingSvc, pool),
		Billing:     billing.NewHandler(billingSvc, pool, cfg.PublicBaseURL),
		Programming: programming.NewHandler(programmingSvc, pool),
		Sync: sync.NewHandler(sync.NewService(wall), pool, sync.Dependencies{
			CRM:         crmSvc,
			Scheduling:  schedulingSvc,
			Billing:     billingSvc,
			Programming: programmingSvc,
		}),
		Dashboard:   dashboard.NewHandler(dashboard.NewService(billingSvc, ledgerSvc, wall), pool),
		TokenIssuer: issuer,
	})

	return srv.Run(ctx)
}
