package subscription

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/NewMux/mtdrb_go/internal/db"
	"github.com/NewMux/mtdrb_go/internal/httpx"
	"github.com/NewMux/mtdrb_go/internal/platform/clock"
	"github.com/NewMux/mtdrb_go/internal/platform/errs"
	"github.com/NewMux/mtdrb_go/internal/tenancy"
)

// RequireFeature refuses a route the caller's plan does not include.
func RequireFeature(f Feature) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			principal, err := tenancy.RequireTrainer(r.Context())
			if err != nil {
				httpx.Error(w, r, err)
				return
			}
			state := FromPrincipal(principal)
			if !state.Allows(f) {
				httpx.Error(w, r, errs.Forbidden(errs.CodeFeatureNotInPlan,
					"%s is part of the Pro plan", f).
					WithMeta("feature", string(f)).
					WithMeta("plan", string(state.Effective())))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ReadOnlyWhenLapsed refuses writes from a lapsed account.
//
// Mounted over everything a trainer changes, sync push included, and not
// over the endpoints a lapsed trainer still needs: signing in and out,
// security, settings, the subscription itself, and every read. Sync push is
// refused whole, before any operation is looked at, so the device's outbox
// keeps its operations waiting rather than parking each one as refused — they
// send themselves once the plan is renewed.
func ReadOnlyWhenLapsed(c clock.Clock) func(http.Handler) http.Handler {
	if c == nil {
		c = clock.System{}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet, http.MethodHead, http.MethodOptions:
				next.ServeHTTP(w, r)
				return
			}
			principal, ok := tenancy.FromContext(r.Context())
			// Portal clients are not the paying party; their own logging is
			// not the trainer's plan's business.
			if ok && principal.IsTrainer() && FromPrincipal(principal).Lapsed(c.Now()) {
				httpx.Error(w, r, errs.Inactive(
					"this account is read-only until its plan is renewed"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// Handler exposes the plan to its owner.
type Handler struct {
	pool  *db.Pool
	clock clock.Clock
}

// NewHandler builds the subscription handler.
func NewHandler(pool *db.Pool, c clock.Clock) *Handler {
	if c == nil {
		c = clock.System{}
	}
	return &Handler{pool: pool, clock: c}
}

// Routes returns the subscription subtree.
func (h *Handler) Routes() http.Handler {
	r := chi.NewRouter()
	r.Get("/", h.summary)
	r.Post("/cancel", h.setCancel(true))
	r.Post("/resume", h.setCancel(false))
	return r
}

func (h *Handler) summary(w http.ResponseWriter, r *http.Request) {
	principal, err := tenancy.RequireTrainer(r.Context())
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	if err := h.pool.InTenantTx(r.Context(), principal.TenantID, func(tx pgx.Tx) error {
		summary, err := Summarise(r.Context(), tx, principal.TenantID, h.clock.Now())
		if err != nil {
			return err
		}
		httpx.JSON(w, r, http.StatusOK, summary)
		return nil
	}); err != nil {
		httpx.Error(w, r, err)
	}
}

func (h *Handler) setCancel(cancel bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := tenancy.RequireOwner(r.Context())
		if err != nil {
			httpx.Error(w, r, err)
			return
		}
		if err := h.pool.InTenantTx(r.Context(), principal.TenantID, func(tx pgx.Tx) error {
			if err := SetCancelAtPeriodEnd(r.Context(), tx, principal.TenantID, cancel); err != nil {
				return err
			}
			summary, err := Summarise(r.Context(), tx, principal.TenantID, h.clock.Now())
			if err != nil {
				return err
			}
			httpx.JSON(w, r, http.StatusOK, summary)
			return nil
		}); err != nil {
			httpx.Error(w, r, err)
		}
	}
}
