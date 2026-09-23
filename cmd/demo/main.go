// Command demo records the practice the demo build shows.
//
// The demo used to be seeded by hand-written SQL in the app: rows that looked
// right but that no ledger had ever produced, so any number the server
// computes — earnings, receivables, a VAT return — could not be shown at all.
// This runs a scenario through the real API instead, against a throwaway
// database with a fixed clock, and records what a device would receive: a
// full sync pull, and the responses of the read endpoints the app calls. The
// demo build replays that recording. Its numbers are real ledger output, and
// nothing about the ledger is re-implemented in TypeScript.
//
//	DEMO_OWNER_URL=postgres://postgres:postgres@localhost:5432/postgres go run ./cmd/demo
//
// The owner URL must be able to CREATE DATABASE. The coachpulse_app role must
// already exist (deploy/postgres-init creates it); its password comes from
// DEMO_APP_PASSWORD. The scratch database is dropped when the run ends.
package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/NewMux/mtdrb_go/internal/api"
	"github.com/NewMux/mtdrb_go/internal/app"
	"github.com/NewMux/mtdrb_go/internal/auth"
	"github.com/NewMux/mtdrb_go/internal/config"
	"github.com/NewMux/mtdrb_go/internal/db"
	"github.com/NewMux/mtdrb_go/internal/platform/clock"
	"github.com/NewMux/mtdrb_go/internal/platform/ids"
)

// recordedAt is the demo's "now": a Wednesday morning in Dubai, 09:30 local.
// The app shifts every date by whole weeks to the viewer's today, so the
// weekday is what matters, not the date.
var recordedAt = time.Date(2026, 6, 17, 5, 30, 0, 0, time.UTC)

func main() {
	out := flag.String("out", "app/src/demo/fixtures/recording.json", "where to write the recording")
	flag.Parse()
	if err := run(*out); err != nil {
		fmt.Fprintf(os.Stderr, "demo: %v\n", err)
		os.Exit(1)
	}
}

func run(out string) error {
	ownerURL := os.Getenv("DEMO_OWNER_URL")
	if ownerURL == "" {
		return errors.New("DEMO_OWNER_URL is required (an owner connection that can CREATE DATABASE)")
	}
	appPassword := os.Getenv("DEMO_APP_PASSWORD")
	if appPassword == "" {
		appPassword = "coachpulse_app" //nolint:gosec // the local stack's documented development password
	}
	ctx := context.Background()

	name := fmt.Sprintf("coachpulse_demo_%d", time.Now().UnixNano())
	scratchOwner, scratchApp, err := scratchURLs(ownerURL, name, appPassword)
	if err != nil {
		return err
	}

	admin, err := pgx.Connect(ctx, ownerURL)
	if err != nil {
		return fmt.Errorf("connect as owner: %w", err)
	}
	defer func() { _ = admin.Close(ctx) }()
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		return fmt.Errorf("create scratch database: %w", err)
	}
	defer func() {
		_, _ = admin.Exec(ctx, "DROP DATABASE IF EXISTS "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)")
	}()

	if err := prepare(ctx, scratchOwner, name); err != nil {
		return err
	}

	pool, err := db.Open(ctx, db.PoolConfig{URL: scratchApp, MaxConns: 8, MinConns: 1, StatementCache: true})
	if err != nil {
		return fmt.Errorf("connect as app role: %w", err)
	}
	defer pool.Close()

	cfg := config.Config{
		Env:             "development",
		ShutdownTimeout: time.Second,
		AccessTokenTTL:  24 * time.Hour,
		RefreshTokenTTL: 24 * time.Hour,
		PublicBaseURL:   "https://demo.coachpulse.io",
	}
	params := auth.DefaultArgon2Params()
	params.Memory, params.Iterations = 1024, 1
	services := app.New(app.Options{
		Pool:            pool,
		Clock:           clock.Fixed{T: recordedAt},
		JWTSigningKey:   []byte(strings.Repeat("d", 32)),
		AccessTokenTTL:  cfg.AccessTokenTTL,
		RefreshTokenTTL: cfg.RefreshTokenTTL,
		Argon2:          params,
		ColumnKey:       []byte(strings.Repeat("e", 32)),
		Presigner:       noPresigner{},
		PublicBaseURL:   cfg.PublicBaseURL,
	})
	server := httptest.NewServer(api.New(cfg, pool, slog.New(slog.DiscardHandler), services.Handlers()).Handler())
	defer server.Close()

	// Reproducible ids, so re-recording shows only what the scenario changed.
	defer ids.UseSequence(recordedAt.AddDate(0, 0, -90), 1)()

	client := &apiClient{base: server.URL}
	recording, err := runScenario(ctx, client)
	if err != nil {
		return err
	}

	raw, err := encode(recording)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o750); err != nil {
		return err
	}
	if err := os.WriteFile(out, append(raw, '\n'), 0o600); err != nil {
		return err
	}
	fmt.Printf("recorded %s (%d KB)\n", out, len(raw)/1024)
	return nil
}

// encode writes the recording one row per line: small enough to check in,
// and a re-recording's diff reads row by row.
func encode(r *Recording) ([]byte, error) {
	var b bytes.Buffer
	head := *r
	head.Pull, head.Reads = nil, nil
	meta, err := json.Marshal(head)
	if err != nil {
		return nil, err
	}
	// Everything but the two bulky fields, then those by hand.
	b.Write(meta[:len(meta)-1])
	b.WriteString(",\n\"pull\": [")
	for i, c := range r.Pull {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, "\n {\"collection\": %q, \"rows\": [", c.Collection)
		for j, row := range c.Rows {
			if j > 0 {
				b.WriteByte(',')
			}
			b.WriteString("\n  ")
			if err := json.Compact(&b, row); err != nil {
				return nil, err
			}
		}
		b.WriteString("\n ]}")
	}
	b.WriteString("\n],\n\"reads\": {")
	paths := make([]string, 0, len(r.Reads))
	for path := range r.Reads {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for i, path := range paths {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, "\n %q: ", path)
		if err := json.Compact(&b, r.Reads[path]); err != nil {
			return nil, err
		}
	}
	b.WriteString("\n}}")
	return b.Bytes(), nil
}

// scratchURLs points the owner and app-role connection strings at the
// scratch database.
func scratchURLs(ownerURL, name, appPassword string) (string, string, error) {
	u, err := url.Parse(ownerURL)
	if err != nil {
		return "", "", fmt.Errorf("parse DEMO_OWNER_URL: %w", err)
	}
	u.Path = "/" + name
	owner := u.String()
	u.User = url.UserPassword("coachpulse_app", appPassword)
	return owner, u.String(), nil
}

// prepare gives the scratch database what deploy/postgres-init gives the real
// one, then migrates it as the owner.
func prepare(ctx context.Context, ownerURL, name string) error {
	conn, err := pgx.Connect(ctx, ownerURL)
	if err != nil {
		return fmt.Errorf("connect to scratch database: %w", err)
	}
	defer func() { _ = conn.Close(ctx) }()
	for _, statement := range []string{
		"GRANT CONNECT ON DATABASE " + pgx.Identifier{name}.Sanitize() + " TO coachpulse_app",
		"GRANT USAGE ON SCHEMA public TO coachpulse_app",
		"ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO coachpulse_app",
		"ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT USAGE, SELECT ON SEQUENCES TO coachpulse_app",
		"ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT EXECUTE ON FUNCTIONS TO coachpulse_app",
		"CREATE EXTENSION IF NOT EXISTS pgcrypto",
	} {
		if _, err := conn.Exec(ctx, statement); err != nil {
			return fmt.Errorf("prepare scratch database: %w", err)
		}
	}

	sqlDB, err := sql.Open("pgx", ownerURL)
	if err != nil {
		return err
	}
	defer func() { _ = sqlDB.Close() }()
	goose.SetBaseFS(db.Migrations)
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}
	if err := goose.UpContext(ctx, sqlDB, db.MigrationsDir); err != nil {
		return fmt.Errorf("migrate scratch database: %w", err)
	}
	return nil
}

type noPresigner struct{}

func (noPresigner) PresignPut(context.Context, string, string, time.Duration) (string, error) {
	return "", errors.New("the demo records no media")
}
func (noPresigner) PresignGet(context.Context, string, time.Duration) (string, error) {
	return "", errors.New("the demo records no media")
}
func (noPresigner) Delete(context.Context, string) error { return nil }
