package catalog

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/NewMux/mtdrb_go/internal/db"
	"github.com/NewMux/mtdrb_go/internal/httpx"
	"github.com/NewMux/mtdrb_go/internal/platform/clock"
	"github.com/NewMux/mtdrb_go/internal/platform/errs"
	"github.com/NewMux/mtdrb_go/internal/platform/ids"
	"github.com/NewMux/mtdrb_go/internal/tenancy"
)

// Handler exposes locations and offers.
type Handler struct {
	pool  *db.Pool
	clock clock.Clock
}

// NewHandler builds the catalogue handler.
func NewHandler(pool *db.Pool, c clock.Clock) *Handler {
	if c == nil {
		c = clock.System{}
	}
	return &Handler{pool: pool, clock: c}
}

// LocationRoutes returns /v1/locations.
func (h *Handler) LocationRoutes() http.Handler {
	r := chi.NewRouter()
	r.Get("/", h.listLocations)
	r.Post("/", h.createLocation)
	r.Patch("/{locationID}", h.updateLocation)
	r.Post("/{locationID}/archive", h.archiveLocation)
	r.Post("/{locationID}/restore", h.restoreLocation)
	return r
}

// OfferRoutes returns /v1/package-offers.
func (h *Handler) OfferRoutes() http.Handler {
	r := chi.NewRouter()
	r.Get("/", h.listOffers)
	r.Post("/", h.createOffer)
	r.Patch("/{offerID}", h.updateOffer)
	r.Post("/{offerID}/archive", h.archiveOffer(true))
	r.Post("/{offerID}/restore", h.archiveOffer(false))
	return r
}

func (h *Handler) withTenant(w http.ResponseWriter, r *http.Request, fn func(tx pgx.Tx, tenantID ids.ID) error) {
	p, err := tenancy.RequireTrainer(r.Context())
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	if err := h.pool.InTenantTx(r.Context(), p.TenantID, func(tx pgx.Tx) error {
		return fn(tx, p.TenantID)
	}); err != nil {
		httpx.Error(w, r, err)
	}
}

func pathID(r *http.Request, name string) (ids.ID, error) {
	id, err := ids.Parse(chi.URLParam(r, name))
	if err != nil {
		return ids.Nil, errs.Invalid(errs.CodeValidation, "%s is not a valid identifier", name)
	}
	return id, nil
}

func archivedWanted(r *http.Request) bool { return r.URL.Query().Get("include_archived") == "true" }

func (h *Handler) listLocations(w http.ResponseWriter, r *http.Request) {
	h.withTenant(w, r, func(tx pgx.Tx, _ ids.ID) error {
		list, err := ListLocations(r.Context(), tx, archivedWanted(r))
		if err != nil {
			return err
		}
		httpx.JSON(w, r, http.StatusOK, map[string]any{"locations": list})
		return nil
	})
}

func (h *Handler) createLocation(w http.ResponseWriter, r *http.Request) {
	var in LocationInput
	if err := httpx.Decode(w, r, &in); err != nil {
		httpx.Error(w, r, err)
		return
	}
	h.withTenant(w, r, func(tx pgx.Tx, tenantID ids.ID) error {
		l, err := CreateLocation(r.Context(), tx, tenantID, in)
		if err != nil {
			return err
		}
		httpx.JSON(w, r, http.StatusCreated, l)
		return nil
	})
}

func (h *Handler) updateLocation(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "locationID")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	var in LocationInput
	if err := httpx.Decode(w, r, &in); err != nil {
		httpx.Error(w, r, err)
		return
	}
	h.withTenant(w, r, func(tx pgx.Tx, _ ids.ID) error {
		l, err := UpdateLocation(r.Context(), tx, id, in)
		if err != nil {
			return err
		}
		httpx.JSON(w, r, http.StatusOK, l)
		return nil
	})
}

func (h *Handler) archiveLocation(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "locationID")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	h.withTenant(w, r, func(tx pgx.Tx, _ ids.ID) error {
		l, err := ArchiveLocation(r.Context(), tx, id, h.clock.Now())
		if err != nil {
			return err
		}
		httpx.JSON(w, r, http.StatusOK, l)
		return nil
	})
}

func (h *Handler) restoreLocation(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "locationID")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	h.withTenant(w, r, func(tx pgx.Tx, _ ids.ID) error {
		l, err := RestoreLocation(r.Context(), tx, id)
		if err != nil {
			return err
		}
		httpx.JSON(w, r, http.StatusOK, l)
		return nil
	})
}

func (h *Handler) listOffers(w http.ResponseWriter, r *http.Request) {
	h.withTenant(w, r, func(tx pgx.Tx, _ ids.ID) error {
		list, err := ListOffers(r.Context(), tx, archivedWanted(r))
		if err != nil {
			return err
		}
		httpx.JSON(w, r, http.StatusOK, map[string]any{"offers": list})
		return nil
	})
}

func (h *Handler) createOffer(w http.ResponseWriter, r *http.Request) {
	var in OfferInput
	if err := httpx.Decode(w, r, &in); err != nil {
		httpx.Error(w, r, err)
		return
	}
	h.withTenant(w, r, func(tx pgx.Tx, tenantID ids.ID) error {
		o, err := CreateOffer(r.Context(), tx, tenantID, in)
		if err != nil {
			return err
		}
		httpx.JSON(w, r, http.StatusCreated, o)
		return nil
	})
}

func (h *Handler) updateOffer(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "offerID")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	var in OfferInput
	if err := httpx.Decode(w, r, &in); err != nil {
		httpx.Error(w, r, err)
		return
	}
	h.withTenant(w, r, func(tx pgx.Tx, _ ids.ID) error {
		o, err := UpdateOffer(r.Context(), tx, id, in)
		if err != nil {
			return err
		}
		httpx.JSON(w, r, http.StatusOK, o)
		return nil
	})
}

func (h *Handler) archiveOffer(archived bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := pathID(r, "offerID")
		if err != nil {
			httpx.Error(w, r, err)
			return
		}
		h.withTenant(w, r, func(tx pgx.Tx, _ ids.ID) error {
			o, err := SetOfferArchived(r.Context(), tx, id, archived, h.clock.Now())
			if err != nil {
				return err
			}
			httpx.JSON(w, r, http.StatusOK, o)
			return nil
		})
	}
}
