package dashboard

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/NewMux/mtdrb_go/internal/db"
	"github.com/NewMux/mtdrb_go/internal/httpx"
	"github.com/NewMux/mtdrb_go/internal/platform/errs"
	"github.com/NewMux/mtdrb_go/internal/platform/ids"
	"github.com/NewMux/mtdrb_go/internal/tenancy"
)

// Handler exposes the dashboard.
type Handler struct {
	svc  *Service
	pool *db.Pool
}

// NewHandler builds the dashboard handler.
func NewHandler(svc *Service, pool *db.Pool) *Handler {
	return &Handler{svc: svc, pool: pool}
}

// Routes returns the dashboard subtree.
func (h *Handler) Routes() http.Handler {
	r := chi.NewRouter()
	r.Get("/", h.summary)
	// The renewal list on its own, for a client that wants it without the
	// money numbers — the portal-free case of "who do I need to talk to".
	r.Get("/low-balance", h.lowBalance)
	return r
}

func (h *Handler) withTenant(w http.ResponseWriter, r *http.Request, fn func(tx pgx.Tx, tenantID ids.ID) error) {
	principal, err := tenancy.RequireTrainer(r.Context())
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	if err := h.pool.InTenantTx(r.Context(), principal.TenantID, func(tx pgx.Tx) error {
		return fn(tx, principal.TenantID)
	}); err != nil {
		httpx.Error(w, r, err)
	}
}

func (h *Handler) summary(w http.ResponseWriter, r *http.Request) {
	h.withTenant(w, r, func(tx pgx.Tx, tenantID ids.ID) error {
		summary, err := h.svc.Summarise(r.Context(), tx, tenantID)
		if err != nil {
			return err
		}
		httpx.JSON(w, r, http.StatusOK, summary)
		return nil
	})
}

func (h *Handler) lowBalance(w http.ResponseWriter, r *http.Request) {
	h.withTenant(w, r, func(tx pgx.Tx, tenantID ids.ID) error {
		threshold := -1
		if raw := r.URL.Query().Get("threshold"); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil {
				return errs.Invalid(errs.CodeValidation, "threshold must be a whole number").
					WithField("threshold", "must be a whole number")
			}
			threshold = parsed
		}
		if threshold < 0 {
			if err := tx.QueryRow(r.Context(),
				`SELECT low_balance_threshold FROM tenants WHERE id = $1`, tenantID,
			).Scan(&threshold); err != nil {
				return err
			}
		}

		clients, err := h.svc.billing.LowBalance(r.Context(), tx, threshold)
		if err != nil {
			return err
		}
		httpx.JSON(w, r, http.StatusOK, map[string]any{
			"clients":   clients,
			"threshold": threshold,
		})
		return nil
	})
}
