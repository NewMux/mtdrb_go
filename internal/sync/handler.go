package sync

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/NewMux/mtdrb_go/internal/db"
	"github.com/NewMux/mtdrb_go/internal/httpx"
	"github.com/NewMux/mtdrb_go/internal/platform/ids"
	"github.com/NewMux/mtdrb_go/internal/tenancy"
)

// Handler exposes the sync endpoints.
type Handler struct {
	svc  *Service
	pool *db.Pool
	deps Dependencies
}

// NewHandler builds the sync handler.
func NewHandler(svc *Service, pool *db.Pool, deps Dependencies) *Handler {
	return &Handler{svc: svc, pool: pool, deps: deps}
}

// Routes returns the sync subtree.
func (h *Handler) Routes() http.Handler {
	r := chi.NewRouter()
	r.Get("/pull", h.pull)
	r.Post("/push", h.push)
	r.Get("/checkpoint", h.checkpoint)
	return r
}

// withCaller runs fn bound to the caller's tenant, and to their client id when
// the caller is a portal client — so a client syncs only their own rows
// without sync needing to know anything about who they are.
func (h *Handler) withCaller(w http.ResponseWriter, r *http.Request, fn func(tx pgx.Tx, p tenancy.Principal) error) {
	principal, err := tenancy.Require(r.Context())
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	run := func(tx pgx.Tx) error { return fn(tx, principal) }

	if principal.IsClient() {
		err = h.pool.InPortalTx(r.Context(), principal.TenantID, principal.SubjectID, run)
	} else {
		err = h.pool.InTenantTx(r.Context(), principal.TenantID, run)
	}
	if err != nil {
		httpx.Error(w, r, err)
	}
}

func (h *Handler) pull(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	cursor, err := DecodeCursor(q.Get("cursor"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	limit := 0
	if raw := q.Get("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			limit = parsed
		}
	}

	h.withCaller(w, r, func(tx pgx.Tx, _ tenancy.Principal) error {
		result, err := h.svc.Pull(r.Context(), tx, cursor, limit)
		if err != nil {
			return err
		}
		httpx.JSON(w, r, http.StatusOK, result)
		return nil
	})
}

func (h *Handler) push(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Operations []Operation `json:"operations"`
	}
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}

	h.withCaller(w, r, func(tx pgx.Tx, p tenancy.Principal) error {
		var actor *ids.ID
		if p.IsTrainer() {
			actor = &p.SubjectID
		}
		result, err := h.svc.Push(r.Context(), tx, p.TenantID, h.deps, req.Operations, actor)
		if err != nil {
			return err
		}
		// Always 200, even with conflicts inside: the batch was processed, and
		// each operation carries its own outcome. A blanket non-2xx would have
		// the outbox retry the eleven that succeeded.
		httpx.JSON(w, r, http.StatusOK, result)
		return nil
	})
}

func (h *Handler) checkpoint(w http.ResponseWriter, r *http.Request) {
	h.withCaller(w, r, func(tx pgx.Tx, _ tenancy.Principal) error {
		cursor, err := h.svc.Checkpoint(r.Context(), tx)
		if err != nil {
			return err
		}
		httpx.JSON(w, r, http.StatusOK, map[string]any{"cursor": cursor.Encode()})
		return nil
	})
}
