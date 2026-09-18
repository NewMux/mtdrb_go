package media

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/NewMux/mtdrb_go/internal/db"
	"github.com/NewMux/mtdrb_go/internal/httpx"
	"github.com/NewMux/mtdrb_go/internal/platform/errs"
	"github.com/NewMux/mtdrb_go/internal/platform/ids"
	"github.com/NewMux/mtdrb_go/internal/tenancy"
)

// Handler exposes upload and download endpoints.
type Handler struct {
	svc  *Service
	pool *db.Pool
}

// NewHandler builds the media handler.
func NewHandler(svc *Service, pool *db.Pool) *Handler {
	return &Handler{svc: svc, pool: pool}
}

// Routes returns the media subtree.
func (h *Handler) Routes() http.Handler {
	r := chi.NewRouter()
	r.Post("/uploads", h.requestUpload)
	r.Post("/{objectID}/confirm", h.confirmUpload)
	r.Get("/{objectID}/download", h.downloadURL)
	r.Delete("/{objectID}", h.deleteObject)
	r.Get("/clients/{clientID}", h.listForClient)
	return r
}

func (h *Handler) withTenant(w http.ResponseWriter, r *http.Request, fn func(tx pgx.Tx, tenantID ids.ID) error) {
	principal, err := tenancy.Require(r.Context())
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	// A portal session binds its client id too, so row-level security narrows
	// media to that client's own objects. This is what stops a client from
	// requesting a download URL for someone else's progress photo.
	run := func(tx pgx.Tx) error { return fn(tx, principal.TenantID) }
	if principal.IsClient() {
		err = h.pool.InPortalTx(r.Context(), principal.TenantID, principal.SubjectID, run)
	} else {
		err = h.pool.InTenantTx(r.Context(), principal.TenantID, run)
	}
	if err != nil {
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

func (h *Handler) requestUpload(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Kind        Kind    `json:"kind"`
		ContentType string  `json:"content_type"`
		ByteSize    int64   `json:"byte_size"`
		ClientID    *ids.ID `json:"client_id"`
	}
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	principal, err := tenancy.Require(r.Context())
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	uploadedBy := &principal.SubjectID
	clientID := req.ClientID
	if principal.IsClient() {
		// A portal upload is always about the uploader. Honouring a
		// client_id from the body would let one client attach media to
		// another's record.
		clientID = &principal.SubjectID
		uploadedBy = nil
	}

	h.withTenant(w, r, func(tx pgx.Tx, tenantID ids.ID) error {
		upload, err := h.svc.RequestUpload(r.Context(), tx, tenantID, UploadRequest{
			Kind:        req.Kind,
			ContentType: req.ContentType,
			ByteSize:    req.ByteSize,
			ClientID:    clientID,
			UploadedBy:  uploadedBy,
		})
		if err != nil {
			return err
		}
		httpx.JSON(w, r, http.StatusCreated, upload)
		return nil
	})
}

func (h *Handler) confirmUpload(w http.ResponseWriter, r *http.Request) {
	objectID, err := pathID(r, "objectID")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	h.withTenant(w, r, func(tx pgx.Tx, _ ids.ID) error {
		obj, err := h.svc.ConfirmUpload(r.Context(), tx, objectID)
		if err != nil {
			return err
		}
		httpx.JSON(w, r, http.StatusOK, obj)
		return nil
	})
}

func (h *Handler) downloadURL(w http.ResponseWriter, r *http.Request) {
	objectID, err := pathID(r, "objectID")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	h.withTenant(w, r, func(tx pgx.Tx, _ ids.ID) error {
		download, err := h.svc.DownloadURL(r.Context(), tx, objectID)
		if err != nil {
			return err
		}
		httpx.JSON(w, r, http.StatusOK, download)
		return nil
	})
}

func (h *Handler) deleteObject(w http.ResponseWriter, r *http.Request) {
	objectID, err := pathID(r, "objectID")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	h.withTenant(w, r, func(tx pgx.Tx, _ ids.ID) error {
		if err := h.svc.Delete(r.Context(), tx, objectID); err != nil {
			return err
		}
		httpx.NoContent(w, r)
		return nil
	})
}

func (h *Handler) listForClient(w http.ResponseWriter, r *http.Request) {
	clientID, err := pathID(r, "clientID")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	kind := Kind(r.URL.Query().Get("kind"))
	if kind == "" {
		kind = KindProgressPhoto
	}
	h.withTenant(w, r, func(tx pgx.Tx, _ ids.ID) error {
		objects, err := h.svc.ListForClient(r.Context(), tx, clientID, kind)
		if err != nil {
			return err
		}
		if objects == nil {
			objects = []Object{}
		}
		httpx.JSON(w, r, http.StatusOK, map[string]any{"objects": objects})
		return nil
	})
}
