// Package errreport sends error-level log records to Sentry, when a DSN is
// configured. Without one it does nothing, so development and tests need no
// account and send nothing anywhere.
package errreport

import (
	"context"
	"fmt"
	"time"

	"github.com/getsentry/sentry-go"

	"github.com/NewMux/mtdrb_go/internal/platform/logger"
)

// Setup initialises Sentry for one binary. It returns the reporter to hang
// off the logger and a flush to defer, so reports in flight when the process
// stops are not lost. Both are no-ops when dsn is empty.
func Setup(dsn, environment, release, service string) (logger.Reporter, func(), error) {
	if dsn == "" {
		return nil, func() {}, nil
	}
	if err := sentry.Init(sentry.ClientOptions{
		Dsn:         dsn,
		Environment: environment,
		Release:     release,
		ServerName:  service,
		// Nothing about a request leaves the process unless it was logged,
		// and the logger has already redacted it.
		SendDefaultPII: false,
	}); err != nil {
		return nil, nil, fmt.Errorf("sentry: %w", err)
	}
	report := func(_ context.Context, message string, attrs map[string]string) {
		sentry.WithScope(func(scope *sentry.Scope) {
			for _, tag := range []string{"request_id", "tenant_id", "job", "method", "path"} {
				if v, ok := attrs[tag]; ok {
					scope.SetTag(tag, v)
				}
			}
			extra := make(map[string]any, len(attrs))
			for k, v := range attrs {
				extra[k] = v
			}
			scope.SetContext("log", extra)
			// The message is the event's title and its grouping key, so it
			// is the log line's fixed text; the error detail goes in the
			// context, or every distinct error string would be a new issue.
			scope.SetLevel(sentry.LevelError)
			sentry.CaptureMessage(message)
		})
	}
	return report, func() { sentry.Flush(2 * time.Second) }, nil
}
