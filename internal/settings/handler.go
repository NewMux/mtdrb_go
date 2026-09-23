package settings

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/NewMux/mtdrb_go/internal/db"
	"github.com/NewMux/mtdrb_go/internal/httpx"
	"github.com/NewMux/mtdrb_go/internal/platform/clock"
	"github.com/NewMux/mtdrb_go/internal/tenancy"
)

// Handler exposes the settings.
type Handler struct {
	pool  *db.Pool
	clock clock.Clock
}

// NewHandler builds the settings handler.
func NewHandler(pool *db.Pool, c clock.Clock) *Handler {
	if c == nil {
		c = clock.System{}
	}
	return &Handler{pool: pool, clock: c}
}

// Routes returns the settings subtree.
func (h *Handler) Routes() http.Handler {
	r := chi.NewRouter()
	r.Get("/", h.get)
	r.Patch("/", h.patch)
	r.Post("/onboarded", h.onboarded)
	return r
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	p, err := tenancy.RequireTrainer(r.Context())
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	if err := h.pool.InTenantTx(r.Context(), p.TenantID, func(tx pgx.Tx) error {
		s, err := Get(r.Context(), tx, p.TenantID)
		if err != nil {
			return err
		}
		httpx.JSON(w, r, http.StatusOK, s)
		return nil
	}); err != nil {
		httpx.Error(w, r, err)
	}
}

func (h *Handler) patch(w http.ResponseWriter, r *http.Request) {
	p, err := tenancy.RequireOwner(r.Context())
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	var req Patch
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	if err := h.pool.InTenantTx(r.Context(), p.TenantID, func(tx pgx.Tx) error {
		s, err := Update(r.Context(), tx, p.TenantID, req)
		if err != nil {
			return err
		}
		httpx.JSON(w, r, http.StatusOK, s)
		return nil
	}); err != nil {
		httpx.Error(w, r, err)
	}
}

func (h *Handler) onboarded(w http.ResponseWriter, r *http.Request) {
	p, err := tenancy.RequireOwner(r.Context())
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	if err := h.pool.InTenantTx(r.Context(), p.TenantID, func(tx pgx.Tx) error {
		s, err := MarkOnboarded(r.Context(), tx, p.TenantID, h.clock.Now())
		if err != nil {
			return err
		}
		httpx.JSON(w, r, http.StatusOK, s)
		return nil
	}); err != nil {
		httpx.Error(w, r, err)
	}
}
