package httpx

import (
	"bytes"
	"context"
	"crypto/sha256"
	"io"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/NewMux/mtdrb_go/internal/db"
	"github.com/NewMux/mtdrb_go/internal/platform/errs"
	"github.com/NewMux/mtdrb_go/internal/platform/ids"
	"github.com/NewMux/mtdrb_go/internal/platform/logger"
	"github.com/NewMux/mtdrb_go/internal/tenancy"
)

// IdempotencyHeader carries the client-chosen key.
const IdempotencyHeader = "Idempotency-Key"

// Idempotent makes a mutating endpoint safe to retry.
//
// This is what lets the offline outbox retry aggressively, which is what makes
// flaky gym connectivity tolerable. Without it, a payment recorded in a
// basement and synced twice becomes two payments — and a trainer's books
// quietly stop matching their bank.
//
// A replayed request returns the stored response rather than doing the work
// again. A key reused with a *different* body is refused: that is a client bug,
// and silently treating it as a replay would drop a real request on the floor.
func Idempotent(pool *db.Pool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := r.Header.Get(IdempotencyHeader)
			if key == "" {
				// The header is optional: a trainer tapping a button in a live
				// session does not need one. It is the sync engine that does.
				next.ServeHTTP(w, r)
				return
			}
			if len(key) < 8 || len(key) > 255 {
				Error(w, r, errs.Invalid(errs.CodeValidation,
					"%s must be between 8 and 255 characters", IdempotencyHeader))
				return
			}

			principal, err := tenancy.Require(r.Context())
			if err != nil {
				Error(w, r, err)
				return
			}

			// The body is read up front so it can be hashed and then replayed
			// into the handler.
			body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
			if err != nil {
				Error(w, r, errs.Invalid(errs.CodeValidation, "request body could not be read"))
				return
			}
			_ = r.Body.Close()
			r.Body = io.NopCloser(bytes.NewReader(body))

			endpoint := r.Method + " " + r.URL.Path
			sum := sha256.Sum256(append([]byte(endpoint+"\n"), body...))
			requestHash := sum[:]

			var (
				replayStatus *int
				replayBody   []byte
				inFlight     bool
				mismatch     bool
			)

			// Claim the key. ON CONFLICT DO NOTHING makes this a single
			// atomic step: whoever inserts the row owns the request, and
			// everyone else reads what is already there.
			err = pool.InTenantTx(r.Context(), principal.TenantID, func(tx pgx.Tx) error {
				tag, err := tx.Exec(r.Context(), `
					INSERT INTO idempotency_keys (tenant_id, key, request_hash, endpoint)
					VALUES ($1, $2, $3, $4)
					ON CONFLICT (tenant_id, key) DO NOTHING`,
					principal.TenantID, key, requestHash, endpoint)
				if err != nil {
					return errs.Internal(err, "claim idempotency key")
				}
				if tag.RowsAffected() == 1 {
					return nil // we own it; run the handler
				}

				var storedHash []byte
				var status *int
				var response []byte
				var completedAt *string
				if err := tx.QueryRow(r.Context(), `
					SELECT request_hash, status_code, response_body, completed_at::text
					  FROM idempotency_keys WHERE tenant_id = $1 AND key = $2`,
					principal.TenantID, key,
				).Scan(&storedHash, &status, &response, &completedAt); err != nil {
					return errs.Internal(err, "read idempotency key")
				}

				if !bytes.Equal(storedHash, requestHash) {
					mismatch = true
					return nil
				}
				if completedAt == nil {
					inFlight = true
					return nil
				}
				replayStatus, replayBody = status, response
				return nil
			})
			if err != nil {
				Error(w, r, err)
				return
			}

			switch {
			case mismatch:
				Error(w, r, errs.Conflict(errs.CodeIdempotencyMismatch,
					"this idempotency key was already used for a different request"))
				return
			case inFlight:
				// The original is still running. Retrying is the right move,
				// so this is a conflict rather than a failure.
				Error(w, r, errs.Conflict("request_in_flight",
					"a request with this idempotency key is still being processed; retry shortly"))
				return
			case replayStatus != nil:
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.Header().Set("Cache-Control", "no-store")
				w.Header().Set("Idempotent-Replay", "true")
				w.WriteHeader(*replayStatus)
				if len(replayBody) > 0 {
					_, _ = w.Write(replayBody)
				}
				return
			}

			// We own the key: run the handler, capturing what it produced.
			rec := &capturingWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r)

			// Only a success is worth replaying. A failure should be retried
			// for real, so its key is released instead.
			if rec.status >= 200 && rec.status < 300 {
				storeResult(r.Context(), pool, principal.TenantID, key, rec)
			} else {
				releaseKey(r.Context(), pool, principal.TenantID, key)
			}
		})
	}
}

// capturingWriter records the response so a replay can return it verbatim.
type capturingWriter struct {
	http.ResponseWriter
	status      int
	body        bytes.Buffer
	wroteHeader bool
}

func (c *capturingWriter) WriteHeader(code int) {
	if c.wroteHeader {
		return
	}
	c.status = code
	c.wroteHeader = true
	c.ResponseWriter.WriteHeader(code)
}

func (c *capturingWriter) Write(b []byte) (int, error) {
	if !c.wroteHeader {
		c.WriteHeader(http.StatusOK)
	}
	// Bounded, so an unexpectedly large response cannot be buffered into
	// memory and then into a jsonb column.
	if c.body.Len() < maxBodyBytes {
		c.body.Write(b)
	}
	return c.ResponseWriter.Write(b)
}

// storeResult records a successful response so a replay can return it.
//
// It runs on a context detached from the request: the client may already have
// disconnected, and failing to record the result here would let a retry do the
// work a second time — which for a payment is the whole thing this exists to
// prevent.
func storeResult(ctx context.Context, pool *db.Pool, tenantID ids.ID, key string, rec *capturingWriter) {
	// Stored as raw bytes so the replay is byte-for-byte identical. Decoding
	// and re-encoding would reorder keys and lose whitespace, which turns one
	// request into two different-looking answers.
	body := rec.body.Bytes()
	bg := context.WithoutCancel(ctx)
	if err := pool.InTenantTx(bg, tenantID, func(tx pgx.Tx) error {
		_, err := tx.Exec(bg, `
			UPDATE idempotency_keys
			   SET status_code = $3, response_body = $4, completed_at = now()
			 WHERE tenant_id = $1 AND key = $2`,
			tenantID, key, rec.status, body)
		return err
	}); err != nil {
		logger.From(ctx).ErrorContext(ctx, "could not record idempotent response",
			slog.String("error", err.Error()))
	}
}

// releaseKey frees a key whose request failed, so the client can retry it
// properly rather than replaying a failure forever.
func releaseKey(ctx context.Context, pool *db.Pool, tenantID ids.ID, key string) {
	bg := context.WithoutCancel(ctx)
	if err := pool.InTenantTx(bg, tenantID, func(tx pgx.Tx) error {
		_, err := tx.Exec(bg,
			`DELETE FROM idempotency_keys WHERE tenant_id = $1 AND key = $2 AND completed_at IS NULL`,
			tenantID, key)
		return err
	}); err != nil {
		logger.From(ctx).ErrorContext(ctx, "could not release idempotency key",
			slog.String("error", err.Error()))
	}
}
