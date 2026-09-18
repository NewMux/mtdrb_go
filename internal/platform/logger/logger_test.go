package logger

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

func newCapture(t *testing.T) (*slog.Logger, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	h := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug, ReplaceAttr: redact})
	return slog.New(h), &buf
}

func TestRedactsSensitiveKeys(t *testing.T) {
	l, buf := newCapture(t)
	l.Info("auth",
		slog.String("password", "hunter2"),
		slog.String("refresh_token", "rt_secret"),
		slog.String("IBAN", "DE89370400440532013000"),
		slog.String("medical_notes", "asthma"),
		slog.String("email", "coach@example.com"),
	)
	out := buf.String()
	for _, secret := range []string{"hunter2", "rt_secret", "DE89370400440532013000", "asthma"} {
		if strings.Contains(out, secret) {
			t.Errorf("secret leaked into log: %q", secret)
		}
	}
	if !strings.Contains(out, "coach@example.com") {
		t.Error("non-sensitive field was dropped")
	}
	var rec map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatalf("log line is not valid json: %v", err)
	}
	if rec["password"] != redacted {
		t.Errorf("password not redacted, got %v", rec["password"])
	}
}

func TestRedactionIsCaseInsensitive(t *testing.T) {
	l, buf := newCapture(t)
	l.Info("m", slog.String("Authorization", "Bearer abc123"))
	if strings.Contains(buf.String(), "abc123") {
		t.Error("case-variant key was not redacted")
	}
}

func TestRedactsInsideGroups(t *testing.T) {
	l, buf := newCapture(t)
	l.Info("m", slog.Group("client", slog.String("medical_notes", "diabetic")))
	if strings.Contains(buf.String(), "diabetic") {
		t.Error("nested sensitive key was not redacted")
	}
}

func TestParseLevel(t *testing.T) {
	cases := map[string]slog.Level{
		"debug": slog.LevelDebug, "DEBUG": slog.LevelDebug,
		"warn": slog.LevelWarn, "warning": slog.LevelWarn,
		"error": slog.LevelError, "info": slog.LevelInfo,
		"": slog.LevelInfo, "nonsense": slog.LevelInfo,
	}
	for in, want := range cases {
		if got := ParseLevel(in); got != want {
			t.Errorf("ParseLevel(%q) = %v, want %v", in, got, want)
		}
	}
}
