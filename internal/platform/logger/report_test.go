package logger

import (
	"context"
	"io"
	"log/slog"
	"testing"
)

func TestWithReporterForwardsErrorsRedacted(t *testing.T) {
	type report struct {
		msg   string
		attrs map[string]string
	}
	var got []report
	base := slog.New(slog.NewJSONHandler(io.Discard, &slog.HandlerOptions{ReplaceAttr: redact}))
	log := WithReporter(base, func(_ context.Context, msg string, attrs map[string]string) {
		got = append(got, report{msg, attrs})
	}).With(slog.String("request_id", "r-1"))

	log.Info("fine")
	log.Warn("worth a look")
	log.Error("request failed", slog.String("error", "boom"), slog.String("medical_notes", "asthma"),
		slog.Group("client", slog.String("iban", "AE07..."), slog.Int("age", 40)))

	if len(got) != 1 {
		t.Fatalf("reported %d records, want only the error", len(got))
	}
	r := got[0]
	if r.msg != "request failed" || r.attrs["error"] != "boom" || r.attrs["request_id"] != "r-1" {
		t.Errorf("report = %+v", r)
	}
	if r.attrs["medical_notes"] != redacted || r.attrs["client.iban"] != redacted {
		t.Errorf("a secret reached the reporter: %+v", r.attrs)
	}
	if r.attrs["client.age"] != "40" {
		t.Errorf("group not flattened: %+v", r.attrs)
	}
}
