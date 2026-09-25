package httpx

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"slices"
	"strings"
	"time"

	"github.com/NewMux/mtdrb_go/internal/platform/errs"
	"github.com/NewMux/mtdrb_go/internal/platform/ids"
	"github.com/NewMux/mtdrb_go/internal/platform/logger"
	"github.com/NewMux/mtdrb_go/internal/platform/ratelimit"
	"github.com/NewMux/mtdrb_go/internal/tenancy"
)

type requestIDKey struct{}

// RequestIDHeader is echoed on every response so a user-reported failure can
// be traced to its log line.
const RequestIDHeader = "X-Request-Id"

// RequestID assigns each request an identifier and echoes it back.
//
// A client-supplied value is accepted only if it looks like one of ours;
// otherwise an attacker could poison logs with forged or oversized ids.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(RequestIDHeader)
		if _, err := ids.Parse(id); err != nil {
			id = ids.New().String()
		}
		w.Header().Set(RequestIDHeader, id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey{}, id)))
	})
}

// RequestIDFrom returns the request identifier, or an empty string.
func RequestIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

// Recoverer converts a panic into a 500 rather than dropping the connection.
func Recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				// A panic mid-request aborts its transaction, so the database
				// is consistent; what remains is to report and record it.
				logger.From(r.Context()).ErrorContext(r.Context(), "panic recovered",
					slog.Any("panic", rec),
					slog.String("path", r.URL.Path),
					slog.String("stack", string(debug.Stack())),
				)
				Error(w, r, errs.Internal(nil, "unhandled panic"))
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// statusRecorder captures the response status for access logging.
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	n, err := s.ResponseWriter.Write(b)
	s.bytes += n
	return n, err
}

// AccessLog records one line per request and attaches a request-scoped logger.
func AccessLog(base *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			reqLogger := base.With(
				slog.String("request_id", RequestIDFrom(r.Context())),
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
			)
			ctx := logger.Into(r.Context(), reqLogger)

			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r.WithContext(ctx))

			// Query strings can carry share tokens, so the raw URL is never
			// logged — only the path.
			reqLogger.InfoContext(ctx, "request",
				slog.Int("status", rec.status),
				slog.Int("bytes", rec.bytes),
				slog.Duration("duration", time.Since(start)),
			)
		})
	}
}

// CORS applies a strict allowlist. Wildcards are rejected in production by
// config validation, so this never has to reflect an arbitrary origin.
func CORS(allowed []string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin != "" && slices.Contains(allowed, origin) {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Credentials", "true")
				w.Header().Set("Access-Control-Allow-Headers",
					"Authorization, Content-Type, Idempotency-Key, X-Refresh-Transport, "+RequestIDHeader)
				w.Header().Set("Access-Control-Allow-Methods",
					"GET, POST, PATCH, PUT, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Max-Age", "600")
				// The response varies by Origin, so a cache must not serve one
				// origin's response to another.
				w.Header().Add("Vary", "Origin")
			}
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// SecurityHeaders sets defensive response headers.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		// An invoice share link is an unguessable URL; keeping it out of the
		// Referer header of any resource it loads is what keeps it unguessable.
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")
		next.ServeHTTP(w, r)
	})
}

// RequireTrainer rejects portal sessions at the route boundary, so a whole
// subtree of financial endpoints can be guarded in one place.
func RequireTrainer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := tenancy.RequireTrainer(r.Context()); err != nil {
			Error(w, r, err)
			return
		}
		next.ServeHTTP(w, r)
	})
}

type clientAddressKey struct{}

// RealIP records the caller's address for everything downstream: sign-in
// rate limits, the /public limiter and the address on a signed waiver.
//
// With trustProxy set, the address is the right-most X-Forwarded-For entry:
// the one the proxy in front of us appended. Everything to its left arrived
// from the caller and is theirs to invent, so taking the left-most entry, as
// is common, would let a guesser choose a fresh address for every attempt,
// and would put a forged address into an evidentiary record. Only set it
// when every request reaches the API through that proxy.
func RealIP(trustProxy bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			addr := r.RemoteAddr
			if host, _, err := net.SplitHostPort(addr); err == nil {
				addr = host
			}
			if trustProxy {
				hops := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
				if last := strings.TrimSpace(hops[len(hops)-1]); net.ParseIP(last) != nil {
					addr = last
				}
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), clientAddressKey{}, addr)))
		})
	}
}

// ClientAddress is the caller's address as RealIP decided it, or the
// connection's peer when RealIP is not mounted.
func ClientAddress(r *http.Request) string {
	if addr, ok := r.Context().Value(clientAddressKey{}).(string); ok {
		return addr
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

// RateLimit refuses a caller who has used up their bucket in limiter.
func RateLimit(limiter *ratelimit.Limiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !limiter.Allow(ClientAddress(r)) {
				Error(w, r, errs.RateLimited("too many requests; wait a minute and try again"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// StrictTransportSecurity tells browsers to reach this origin only over
// HTTPS for a year. Sent only in production, where config has already
// required https URLs: sent from a plain-http development server it would
// pin localhost to https in the developer's browser.
func StrictTransportSecurity(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		next.ServeHTTP(w, r)
	})
}
