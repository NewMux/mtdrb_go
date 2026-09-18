// Package logger builds the application's structured logger.
//
// Log records routinely pass near client medical notes, payment instructions
// and progress photo keys, so the handler installed here redacts a fixed set
// of attribute keys rather than trusting every call site to remember.
package logger

import (
	"context"
	"log/slog"
	"os"
	"strings"
)

// Format selects the log encoding.
type Format string

const (
	FormatJSON Format = "json" // production: machine-parseable
	FormatText Format = "text" // local development: human-readable
)

// redactedKeys are never emitted in full, wherever they appear in the tree.
var redactedKeys = map[string]struct{}{
	"password":             {},
	"password_hash":        {},
	"token":                {},
	"access_token":         {},
	"refresh_token":        {},
	"share_token":          {},
	"authorization":        {},
	"signature":            {},
	"medical_notes":        {},
	"payment_instructions": {},
	"iban":                 {},
	"swift":                {},
	"account_number":       {},
}

const redacted = "[REDACTED]"

// New builds a logger at the given level and format.
func New(level slog.Level, format Format) *slog.Logger {
	opts := &slog.HandlerOptions{Level: level, ReplaceAttr: redact}
	var h slog.Handler
	if format == FormatText {
		h = slog.NewTextHandler(os.Stdout, opts)
	} else {
		h = slog.NewJSONHandler(os.Stdout, opts)
	}
	return slog.New(h)
}

func redact(_ []string, a slog.Attr) slog.Attr {
	if _, secret := redactedKeys[strings.ToLower(a.Key)]; secret {
		return slog.String(a.Key, redacted)
	}
	return a
}

// ParseLevel maps a configuration string to a level, defaulting to info.
func ParseLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

type ctxKey struct{}

// Into returns a context carrying the logger, so request-scoped attributes
// such as tenant and request id travel with the call.
func Into(ctx context.Context, l *slog.Logger) context.Context {
	return context.WithValue(ctx, ctxKey{}, l)
}

// From retrieves the context logger, falling back to the default.
func From(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(ctxKey{}).(*slog.Logger); ok {
		return l
	}
	return slog.Default()
}
