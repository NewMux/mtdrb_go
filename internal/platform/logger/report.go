package logger

import (
	"context"
	"fmt"
	"log/slog"
)

// Reporter receives every error-level record, after redaction: the hook an
// error tracker hangs off. The attributes are flattened to strings, with
// group names joined by dots.
type Reporter func(ctx context.Context, message string, attrs map[string]string)

// WithReporter returns a logger that also hands each error-level record to
// rep. Everything worth an alert already logs at error — a failed request,
// a recovered panic, a job that will be retried — so the tracker sees what
// the logs see and no call site has to remember a second API. The same
// redaction applies: a medical note that is [REDACTED] in the log is
// [REDACTED] in the report.
func WithReporter(l *slog.Logger, rep Reporter) *slog.Logger {
	if rep == nil {
		return l
	}
	return slog.New(&reportingHandler{inner: l.Handler(), rep: rep})
}

type reportingHandler struct {
	inner  slog.Handler
	rep    Reporter
	attrs  []slog.Attr
	groups []string
}

func (h *reportingHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *reportingHandler) Handle(ctx context.Context, r slog.Record) error {
	err := h.inner.Handle(ctx, r)
	if r.Level >= slog.LevelError {
		flat := map[string]string{}
		prefix := ""
		for _, g := range h.groups {
			prefix += g + "."
		}
		for _, a := range h.attrs {
			flatten(flat, prefix, a)
		}
		r.Attrs(func(a slog.Attr) bool {
			flatten(flat, prefix, a)
			return true
		})
		h.rep(ctx, r.Message, flat)
	}
	return err
}

func (h *reportingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := *h
	next.inner = h.inner.WithAttrs(attrs)
	next.attrs = append(append([]slog.Attr{}, h.attrs...), attrs...)
	return &next
}

func (h *reportingHandler) WithGroup(name string) slog.Handler {
	next := *h
	next.inner = h.inner.WithGroup(name)
	next.groups = append(append([]string{}, h.groups...), name)
	return &next
}

func flatten(out map[string]string, prefix string, a slog.Attr) {
	a = redact(nil, a)
	v := a.Value.Resolve()
	if v.Kind() == slog.KindGroup {
		for _, g := range v.Group() {
			flatten(out, prefix+a.Key+".", g)
		}
		return
	}
	out[prefix+a.Key] = fmt.Sprint(v.Any())
}
