// Package config loads runtime configuration from the environment.
//
// Loading fails fast and reports every problem at once: a server that starts
// with a missing signing key and only discovers it on the first login is worse
// than one that refuses to boot.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config is the fully resolved application configuration.
type Config struct {
	Env             string        // "development" | "staging" | "production"
	HTTPAddr        string        // listen address for the API
	ShutdownTimeout time.Duration // grace period for in-flight requests

	DatabaseURL      string        // pgx connection string for the RLS-bound app role
	DBMaxConns       int32         //
	DBMinConns       int32         //
	DBConnMaxLife    time.Duration //
	DBStatementCache bool          //

	JWTSigningKey   []byte        // HMAC key for access tokens
	AccessTokenTTL  time.Duration //
	RefreshTokenTTL time.Duration //

	// ColumnEncryptionKey encrypts the narrow set of sensitive free-text
	// columns (emergency medical notes) via pgcrypto.
	ColumnEncryptionKey []byte

	StorageEndpoint  string // S3-compatible endpoint (MinIO locally)
	StorageRegion    string
	StorageBucket    string
	StorageAccessKey string
	StorageSecretKey string
	StorageUseSSL    bool
	PresignTTL       time.Duration // lifetime of a presigned media URL

	PublicBaseURL string // origin used to build invoice share links
	CORSOrigins   []string
	// AppURL is where the app itself is served: the origin of the link in a
	// password-reset email.
	AppURL string
	// TrustProxy believes X-Forwarded-For when rate limiting sign-in. Only
	// behind a load balancer that sets it.
	TrustProxy bool

	// SMTP sends password-reset emails. With no host, mail is logged instead,
	// which is only allowed outside production.
	SMTPHost     string
	SMTPPort     int
	SMTPUsername string
	SMTPPassword string
	MailFrom     string

	LogLevel  string
	LogFormat string
}

// IsProduction reports whether relaxed development behaviour must be disabled.
func (c Config) IsProduction() bool { return c.Env == "production" }

type loader struct {
	problems []string
}

// Load reads configuration from the process environment.
func Load() (Config, error) {
	l := &loader{}
	cfg := Config{
		Env:             l.str("APP_ENV", "development"),
		HTTPAddr:        l.str("HTTP_ADDR", ":8080"),
		ShutdownTimeout: l.dur("SHUTDOWN_TIMEOUT", 15*time.Second),

		DatabaseURL:      l.required("DATABASE_URL"),
		DBMaxConns:       int32(l.num("DB_MAX_CONNS", 20)),
		DBMinConns:       int32(l.num("DB_MIN_CONNS", 2)),
		DBConnMaxLife:    l.dur("DB_CONN_MAX_LIFETIME", time.Hour),
		DBStatementCache: l.boolean("DB_STATEMENT_CACHE", true),

		AccessTokenTTL:  l.dur("ACCESS_TOKEN_TTL", 15*time.Minute),
		RefreshTokenTTL: l.dur("REFRESH_TOKEN_TTL", 30*24*time.Hour),

		StorageEndpoint:  l.str("STORAGE_ENDPOINT", "localhost:9000"),
		StorageRegion:    l.str("STORAGE_REGION", "us-east-1"),
		StorageBucket:    l.str("STORAGE_BUCKET", "coachpulse"),
		StorageAccessKey: l.required("STORAGE_ACCESS_KEY"),
		StorageSecretKey: l.required("STORAGE_SECRET_KEY"),
		StorageUseSSL:    l.boolean("STORAGE_USE_SSL", false),
		PresignTTL:       l.dur("PRESIGN_TTL", 5*time.Minute),

		PublicBaseURL: l.str("PUBLIC_BASE_URL", "http://localhost:8080"),
		CORSOrigins:   l.list("CORS_ORIGINS", "http://localhost:8081"),
		AppURL:        l.str("APP_URL", "http://localhost:8081"),
		TrustProxy:    l.boolean("TRUST_PROXY", false),

		SMTPHost:     l.str("SMTP_HOST", ""),
		SMTPPort:     l.num("SMTP_PORT", 587),
		SMTPUsername: l.str("SMTP_USERNAME", ""),
		SMTPPassword: l.str("SMTP_PASSWORD", ""),
		MailFrom:     l.str("MAIL_FROM", "CoachPulse <no-reply@coachpulse.io>"),

		LogLevel:  l.str("LOG_LEVEL", "info"),
		LogFormat: l.str("LOG_FORMAT", "json"),
	}

	cfg.JWTSigningKey = l.secret("JWT_SIGNING_KEY", 32)
	cfg.ColumnEncryptionKey = l.secret("COLUMN_ENCRYPTION_KEY", 32)

	switch cfg.Env {
	case "development", "staging", "production":
	default:
		l.fail("APP_ENV must be development, staging or production, got %q", cfg.Env)
	}
	if cfg.DBMinConns > cfg.DBMaxConns {
		l.fail("DB_MIN_CONNS (%d) exceeds DB_MAX_CONNS (%d)", cfg.DBMinConns, cfg.DBMaxConns)
	}
	if cfg.IsProduction() {
		if !cfg.StorageUseSSL {
			l.fail("STORAGE_USE_SSL must be true in production")
		}
		if strings.HasPrefix(cfg.PublicBaseURL, "http://") {
			l.fail("PUBLIC_BASE_URL must be https in production")
		}
		if strings.HasPrefix(cfg.AppURL, "http://") {
			l.fail("APP_URL must be https in production")
		}
		if cfg.SMTPHost == "" {
			l.fail("SMTP_HOST is required in production: password resets must be delivered, not logged")
		}
		for _, o := range cfg.CORSOrigins {
			if o == "*" {
				l.fail("CORS_ORIGINS must not be a wildcard in production")
			}
		}
	}

	if len(l.problems) > 0 {
		return Config{}, fmt.Errorf("config: %s", strings.Join(l.problems, "; "))
	}
	return cfg, nil
}

func (l *loader) fail(format string, args ...any) {
	l.problems = append(l.problems, fmt.Sprintf(format, args...))
}

func (l *loader) str(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func (l *loader) required(key string) string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		l.fail("%s is required", key)
	}
	return v
}

// secret reads a required secret and enforces a minimum length, so a
// placeholder value cannot silently weaken token signing or column encryption.
func (l *loader) secret(key string, minLen int) []byte {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		l.fail("%s is required", key)
		return nil
	}
	if len(v) < minLen {
		l.fail("%s must be at least %d bytes, got %d", key, minLen, len(v))
		return nil
	}
	return []byte(v)
}

func (l *loader) num(key string, def int) int {
	raw, ok := os.LookupEnv(key)
	if !ok || raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		l.fail("%s must be an integer, got %q", key, raw)
		return def
	}
	return n
}

func (l *loader) dur(key string, def time.Duration) time.Duration {
	raw, ok := os.LookupEnv(key)
	if !ok || raw == "" {
		return def
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		l.fail("%s must be a duration such as 15m, got %q", key, raw)
		return def
	}
	if d <= 0 {
		l.fail("%s must be positive, got %q", key, raw)
		return def
	}
	return d
}

func (l *loader) boolean(key string, def bool) bool {
	raw, ok := os.LookupEnv(key)
	if !ok || raw == "" {
		return def
	}
	b, err := strconv.ParseBool(raw)
	if err != nil {
		l.fail("%s must be a boolean, got %q", key, raw)
		return def
	}
	return b
}

func (l *loader) list(key, def string) []string {
	raw := l.str(key, def)
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
