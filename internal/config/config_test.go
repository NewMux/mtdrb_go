package config

import (
	"strings"
	"testing"
	"time"
)

// validEnv is the minimum set of variables needed for a successful load.
func validEnv(t *testing.T) {
	t.Helper()
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
	for _, want := range []string{"STORAGE_USE_SSL", "PUBLIC_BASE_URL", "CORS_ORIGINS"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("missing %s check: %v", want, err)
		}
	}
}

func TestProductionAcceptsHardenedValues(t *testing.T) {
	validEnv(t)
	t.Setenv("APP_ENV", "production")
	t.Setenv("STORAGE_USE_SSL", "true")
	t.Setenv("PUBLIC_BASE_URL", "https://app.coachpulse.io")
	t.Setenv("CORS_ORIGINS", "https://app.coachpulse.io, https://admin.coachpulse.io")
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
