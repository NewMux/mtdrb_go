// Command api serves the CoachPulse HTTP API.
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/NewMux/mtdrb_go/internal/api"
	"github.com/NewMux/mtdrb_go/internal/app"
	"github.com/NewMux/mtdrb_go/internal/config"
	"github.com/NewMux/mtdrb_go/internal/db"
	"github.com/NewMux/mtdrb_go/internal/mail"
	"github.com/NewMux/mtdrb_go/internal/media"
	"github.com/NewMux/mtdrb_go/internal/platform/clock"
	"github.com/NewMux/mtdrb_go/internal/platform/errreport"
	"github.com/NewMux/mtdrb_go/internal/platform/logger"
)

func main() {
	// The image has no shell and no curl, so the container's health check is
	// this binary asking itself.
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		if err := healthcheck(); err != nil {
			fmt.Fprintf(os.Stderr, "api: unhealthy: %v\n", err)
			os.Exit(1)
		}
		return
	}
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
	report, flush, err := errreport.Setup(cfg.SentryDSN, cfg.Env, cfg.Release, "api")
	if err != nil {
		return err
	}
	defer flush()
	log = logger.WithReporter(log, report)

	// Cancelled on SIGINT/SIGTERM, which starts a graceful drain rather than
	// dropping in-flight requests — a request mid-ledger-post should finish.
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
	if err := presigner.EnsureBucket(ctx, cfg.StorageRegion, cfg.StorageCreateBucket); err != nil {
		return err
	}

	// Password-reset links go out over SMTP; with no mail server configured
	// (development only — config refuses it in production) they are logged.
	var mailer mail.Sender = mail.LogSender{Log: log}
	if cfg.SMTPHost != "" {
		mailer = mail.SMTPSender{
			Host: cfg.SMTPHost, Port: cfg.SMTPPort,
			Username: cfg.SMTPUsername, Password: cfg.SMTPPassword, From: cfg.MailFrom,
			TLS: cfg.SMTPTLS,
		}
	}

	services := app.New(app.Options{
		Pool:            pool,
		Clock:           clock.System{},
		JWTSigningKey:   cfg.JWTSigningKey,
		AccessTokenTTL:  cfg.AccessTokenTTL,
		RefreshTokenTTL: cfg.RefreshTokenTTL,
		ColumnKey:       cfg.ColumnEncryptionKey,
		Presigner:       presigner,
		Storage:         presigner,
		PurgeAfter:      cfg.AccountPurgeAfter,
		PresignTTL:      cfg.PresignTTL,
		PublicBaseURL:   cfg.PublicBaseURL,
		Mailer:          mailer,
		AppURL:          cfg.AppURL,
		SecureCookies:   cfg.IsProduction() || strings.HasPrefix(cfg.PublicBaseURL, "https://"),
	})
	srv := api.New(cfg, pool, log, services.Handlers())

	return srv.Run(ctx)
}

// healthcheck asks the running API on this machine whether it is ready.
func healthcheck() error {
	addr := os.Getenv("HTTP_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	if strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1" + addr
	}
	client := http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://" + addr + "/readyz")
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("readyz answered %d", resp.StatusCode)
	}
	return nil
}
