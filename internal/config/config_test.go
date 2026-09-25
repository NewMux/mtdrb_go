package config

import (
	"strings"
	"testing"
	"time"
)

// validEnv is the minimum set of variables needed for a successful load.
func validEnv(t *testing.T) {
	t.Helper()
	t.Setenv("APP_ENV", "development")
	t.Setenv("DATABASE_URL", "postgres://app:pw@localhost:5432/coachpulse")
	t.Setenv("STORAGE_ACCESS_KEY", "minioadmin")
	t.Setenv("STORAGE_SECRET_KEY", "minioadmin")
	t.Setenv("JWT_SIGNING_KEY", strings.Repeat("k", 32))
	t.Setenv("COLUMN_ENCRYPTION_KEY", strings.Repeat("c", 32))
}

func TestLoadDefaults(t *testing.T) {
	validEnv(t)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Env != "development" || cfg.HTTPAddr != ":8080" {
		t.Errorf("unexpected defaults: %+v", cfg)
	}
	if cfg.AccessTokenTTL != 15*time.Minute {
		t.Errorf("access ttl = %v", cfg.AccessTokenTTL)
	}
	if cfg.PresignTTL != 5*time.Minute {
		t.Errorf("presign ttl = %v", cfg.PresignTTL)
	}
	if cfg.IsProduction() {
		t.Error("development should not report production")
	}
}

func TestLoadReportsAllMissingSecretsAtOnce(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	t.Setenv("STORAGE_ACCESS_KEY", "")
	t.Setenv("STORAGE_SECRET_KEY", "")
	t.Setenv("JWT_SIGNING_KEY", "")
	t.Setenv("COLUMN_ENCRYPTION_KEY", "")
	_, err := Load()
	if err == nil {
		t.Fatal("expected failure")
	}
	for _, key := range []string{"DATABASE_URL", "STORAGE_ACCESS_KEY", "JWT_SIGNING_KEY", "COLUMN_ENCRYPTION_KEY"} {
		if !strings.Contains(err.Error(), key) {
			t.Errorf("error does not mention %s: %v", key, err)
		}
	}
}

func TestShortSecretsRejected(t *testing.T) {
	validEnv(t)
	t.Setenv("JWT_SIGNING_KEY", "too-short")
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "at least 32 bytes") {
		t.Fatalf("expected minimum-length failure, got %v", err)
	}
}

func TestProductionHardening(t *testing.T) {
	validEnv(t)
	t.Setenv("APP_ENV", "production")
	t.Setenv("STORAGE_USE_SSL", "false")
	t.Setenv("PUBLIC_BASE_URL", "http://insecure.example.com")
	t.Setenv("CORS_ORIGINS", "*")
	_, err := Load()
	if err == nil {
		t.Fatal("expected production checks to fail")
	}
	for _, want := range []string{"STORAGE_USE_SSL", "PUBLIC_BASE_URL", "CORS_ORIGINS", "APP_URL", "SMTP_HOST"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("missing %s check: %v", want, err)
		}
	}
}

func TestProductionAcceptsHardenedValues(t *testing.T) {
	validEnv(t)
	t.Setenv("APP_ENV", "production")
	t.Setenv("DATABASE_URL", "postgres://app:pw@db.internal:5432/coachpulse?sslmode=verify-full")
	t.Setenv("STORAGE_ENDPOINT", "s3.eu-central-1.amazonaws.com")
	t.Setenv("STORAGE_USE_SSL", "true")
	t.Setenv("PUBLIC_BASE_URL", "https://app.coachpulse.io")
	t.Setenv("CORS_ORIGINS", "https://app.coachpulse.io, https://admin.coachpulse.io")
	t.Setenv("APP_URL", "https://app.coachpulse.io")
	t.Setenv("SMTP_HOST", "smtp.example.com")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !cfg.IsProduction() {
		t.Error("expected production")
	}
	if len(cfg.CORSOrigins) != 2 || cfg.CORSOrigins[1] != "https://admin.coachpulse.io" {
		t.Errorf("origins not trimmed/split: %#v", cfg.CORSOrigins)
	}
}

func TestInvalidEnvRejected(t *testing.T) {
	validEnv(t)
	t.Setenv("APP_ENV", "prod")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "APP_ENV") {
		t.Fatalf("expected APP_ENV validation, got %v", err)
	}
}

func TestMalformedScalarsRejected(t *testing.T) {
	validEnv(t)
	t.Setenv("DB_MAX_CONNS", "many")
	t.Setenv("ACCESS_TOKEN_TTL", "fifteen")
	t.Setenv("DB_STATEMENT_CACHE", "yes-please")
	_, err := Load()
	if err == nil {
		t.Fatal("expected parse failures")
	}
	for _, want := range []string{"DB_MAX_CONNS", "ACCESS_TOKEN_TTL", "DB_STATEMENT_CACHE"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("missing %s: %v", want, err)
		}
	}
}

func TestNonPositiveDurationRejected(t *testing.T) {
	validEnv(t)
	t.Setenv("ACCESS_TOKEN_TTL", "0s")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "positive") {
		t.Fatalf("expected positive-duration check, got %v", err)
	}
}

func TestConnPoolBoundsChecked(t *testing.T) {
	validEnv(t)
	t.Setenv("DB_MIN_CONNS", "50")
	t.Setenv("DB_MAX_CONNS", "10")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "DB_MIN_CONNS") {
		t.Fatalf("expected pool bounds check, got %v", err)
	}
}

func TestTheWorkerNeedsOnlyItsDatabase(t *testing.T) {
	for _, key := range []string{"STORAGE_ACCESS_KEY", "STORAGE_SECRET_KEY", "JWT_SIGNING_KEY", "COLUMN_ENCRYPTION_KEY", "SMTP_HOST"} {
		t.Setenv(key, "")
	}
	t.Setenv("DATABASE_URL", "postgres://worker@db.internal/coachpulse?sslmode=require")
	t.Setenv("APP_ENV", "production")
	if _, err := LoadWorker(); err != nil {
		t.Fatalf("worker config: %v", err)
	}
	t.Setenv("DATABASE_URL", "")
	if _, err := LoadWorker(); err == nil || !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Fatalf("expected DATABASE_URL to be required, got %v", err)
	}
}

func TestAppEnvMustBeSet(t *testing.T) {
	validEnv(t)
	t.Setenv("APP_ENV", "")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "APP_ENV is required") {
		t.Fatalf("an unset APP_ENV must not fall back to development, got %v", err)
	}
	if _, err := LoadWorker(); err == nil || !strings.Contains(err.Error(), "APP_ENV is required") {
		t.Fatalf("worker: an unset APP_ENV must not fall back to development, got %v", err)
	}
}

// productionEnv is a production configuration that loads cleanly, for tests
// that break one thing at a time.
func productionEnv(t *testing.T) {
	t.Helper()
	validEnv(t)
	t.Setenv("APP_ENV", "production")
	t.Setenv("DATABASE_URL", "postgres://app:pw@db.internal:5432/coachpulse?sslmode=require")
	t.Setenv("STORAGE_ENDPOINT", "abc.r2.cloudflarestorage.com")
	t.Setenv("STORAGE_USE_SSL", "true")
	t.Setenv("PUBLIC_BASE_URL", "https://coachpulse.example")
	t.Setenv("APP_URL", "https://coachpulse.example")
	t.Setenv("CORS_ORIGINS", "https://coachpulse.example")
	t.Setenv("SMTP_HOST", "smtp.example.com")
}

func TestProductionRefusesWhatOnlySuitsALaptop(t *testing.T) {
	productionEnv(t)
	if _, err := Load(); err != nil {
		t.Fatalf("baseline production config should load: %v", err)
	}
	cases := []struct {
		name, key, value, want string
	}{
		{"dev signing key", "JWT_SIGNING_KEY", "dev-only-signing-key-change-me!!", "JWT_SIGNING_KEY is the development placeholder"},
		{"dev column key", "COLUMN_ENCRYPTION_KEY", "dev-only-column-enc-key-change-me", "COLUMN_ENCRYPTION_KEY is the development placeholder"},
		{"shared keys", "COLUMN_ENCRYPTION_KEY", strings.Repeat("k", 32), "must differ"},
		{"no sslmode", "DATABASE_URL", "postgres://app:pw@db.internal:5432/coachpulse", `sslmode=require, verify-ca or verify-full (got "prefer")`},
		{"sslmode disable", "DATABASE_URL", "postgres://app:pw@db.internal/coachpulse?sslmode=disable", `(got "disable")`},
		{"keyword dsn", "DATABASE_URL", "host=db.internal user=app sslmode=allow", `(got "allow")`},
		{"local storage", "STORAGE_ENDPOINT", "localhost:9000", "STORAGE_ENDPOINT must not be localhost"},
		{"loopback storage", "STORAGE_ENDPOINT", "127.0.0.1:9000", "STORAGE_ENDPOINT must not be localhost"},
		{"local cors", "CORS_ORIGINS", "https://coachpulse.example,http://localhost:8081", "CORS_ORIGINS must not include localhost"},
		{"local public url", "PUBLIC_BASE_URL", "https://localhost", "PUBLIC_BASE_URL must not be localhost"},
		{"bad log level", "LOG_LEVEL", "verbose", "LOG_LEVEL"},
		{"bad log format", "LOG_FORMAT", "xml", "LOG_FORMAT"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			productionEnv(t)
			t.Setenv(tc.key, tc.value)
			_, err := Load()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want error containing %q, got %v", tc.want, err)
			}
		})
	}
}

func TestDatabaseTLSCanBeWaivedForAPrivateNetwork(t *testing.T) {
	productionEnv(t)
	t.Setenv("DATABASE_URL", "postgres://app:pw@postgres:5432/coachpulse?sslmode=disable")
	t.Setenv("DB_REQUIRE_TLS", "false")
	if _, err := Load(); err != nil {
		t.Fatalf("an explicit waiver should load: %v", err)
	}
}

func TestDevelopmentKeepsLocalDefaults(t *testing.T) {
	validEnv(t)
	t.Setenv("JWT_SIGNING_KEY", "dev-only-signing-key-change-me!!")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("development must accept the .env.example values: %v", err)
	}
	if cfg.DBRequireTLS {
		t.Error("development should not demand database TLS")
	}
}
