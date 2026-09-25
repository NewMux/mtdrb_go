package crm

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/NewMux/mtdrb_go/internal/db"
	"github.com/NewMux/mtdrb_go/internal/httpx"
	"github.com/NewMux/mtdrb_go/internal/platform/dates"
	"github.com/NewMux/mtdrb_go/internal/platform/errs"
	"github.com/NewMux/mtdrb_go/internal/platform/ids"
	"github.com/NewMux/mtdrb_go/internal/tenancy"
)

// Handler exposes the CRM endpoints.
type Handler struct {
	svc  *Service
	pool *db.Pool
}

// NewHandler builds the CRM handler.
func NewHandler(svc *Service, pool *db.Pool) *Handler {
	return &Handler{svc: svc, pool: pool}
}

// Routes returns the trainer-facing CRM subtree.
//
// Every route here is mounted behind trainer authentication: a portal session
// reaches its own record through the portal subtree, not through these.
func (h *Handler) Routes() http.Handler {
	r := chi.NewRouter()

	r.Get("/", h.listClients)
	r.Post("/", h.createClient)

	r.Route("/tags", func(t chi.Router) {
		t.Get("/", h.listTags)
		t.Post("/", h.createTag)
		t.Delete("/{tagID}", h.deleteTag)
	})

	r.Route("/{clientID}", func(c chi.Router) {
		c.Get("/", h.getClient)
		c.Patch("/", h.updateClient)
		c.Delete("/", h.deleteClient)
		c.Put("/tags", h.setClientTags)

		c.Get("/parq", h.latestParQ)
		c.Post("/parq", h.submitParQ)

		c.Get("/waivers", h.listSignatures)
		c.Post("/waivers/{waiverID}/sign", h.signWaiver)

		c.Get("/biometrics", h.listBiometrics)
		c.Post("/biometrics", h.recordBiometrics)
	})

	return r
}

// WaiverRoutes returns the waiver document endpoints, which are tenant-level
// rather than per-client.
func (h *Handler) WaiverRoutes() http.Handler {
	r := chi.NewRouter()
	r.Get("/", h.listWaivers)
	r.Post("/", h.createWaiver)
	return r
}

// withTenant runs fn in a transaction bound to the caller's tenant.
//
// The tenant comes from the verified token, never from the request, so a
// caller cannot name a tenant they do not belong to.
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

func pathID(r *http.Request, name string) (ids.ID, error) {
	id, err := ids.Parse(chi.URLParam(r, name))
	if err != nil {
		return ids.Nil, errs.Invalid(errs.CodeValidation, "%s is not a valid identifier", name)
	}
	return id, nil
}

// ---------------------------------------------------------------------------
// Clients
// ---------------------------------------------------------------------------

type clientRequest struct {
	FullName              string      `json:"full_name"`
	Email                 *string     `json:"email"`
	Phone                 *string     `json:"phone"`
	DateOfBirth           *dates.Date `json:"date_of_birth"`
	Status                Status      `json:"status"`
	EmergencyContactName  *string     `json:"emergency_contact_name"`
	EmergencyContactPhone *string     `json:"emergency_contact_phone"`
	MedicalNotes          *string     `json:"medical_notes"`
	AllowOverdraft        *bool       `json:"allow_overdraft"`
	DefaultRateMinor      *int64      `json:"default_rate_minor"`
	Notes                 string      `json:"notes"`
	TagIDs                []ids.ID    `json:"tag_ids"`
}

func (h *Handler) createClient(w http.ResponseWriter, r *http.Request) {
	var req clientRequest
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	h.withTenant(w, r, func(tx pgx.Tx, tenantID ids.ID) error {
		// Mapped field by field rather than converted: the wire shape and the
		// service input are allowed to diverge, and a silent conversion would
		// break the moment either gains a field the other should not carry.
		//nolint:staticcheck // S1016: decoupling is deliberate.
		client, err := h.svc.Create(r.Context(), tx, tenantID, CreateInput{
			FullName:              req.FullName,
			Email:                 req.Email,
			Phone:                 req.Phone,
			DateOfBirth:           req.DateOfBirth.TimePtr(),
			Status:                req.Status,
			EmergencyContactName:  req.EmergencyContactName,
			EmergencyContactPhone: req.EmergencyContactPhone,
			MedicalNotes:          req.MedicalNotes,
			AllowOverdraft:        req.AllowOverdraft,
			DefaultRateMinor:      req.DefaultRateMinor,
			Notes:                 req.Notes,
			TagIDs:                req.TagIDs,
		})
		if err != nil {
			return err
		}
		httpx.JSON(w, r, http.StatusCreated, client)
		return nil
	})
}

// updateRequest uses double pointers so that an absent field and an explicit
// null are distinguishable: omitting "phone" leaves it alone, sending
// "phone": null clears it.
type updateRequest struct {
	FullName              *string      `json:"full_name"`
	Email                 **string     `json:"email"`
	Phone                 **string     `json:"phone"`
	DateOfBirth           **dates.Date `json:"date_of_birth"`
	Status                *Status      `json:"status"`
	EmergencyContactName  **string     `json:"emergency_contact_name"`
	EmergencyContactPhone **string     `json:"emergency_contact_phone"`
	MedicalNotes          **string     `json:"medical_notes"`
	AllowOverdraft        **bool       `json:"allow_overdraft"`
	DefaultRateMinor      **int64      `json:"default_rate_minor"`
	Notes                 *string      `json:"notes"`
}

func (h *Handler) updateClient(w http.ResponseWriter, r *http.Request) {
	clientID, err := pathID(r, "clientID")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	var req updateRequest
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	h.withTenant(w, r, func(tx pgx.Tx, _ ids.ID) error {
		// Mapped field by field rather than converted: date_of_birth crosses
		// the boundary as a calendar date and arrives as a timestamp, so the
		// two structs are deliberately no longer identical.
		client, err := h.svc.Update(r.Context(), tx, clientID, UpdateInput{
			FullName:              req.FullName,
			Email:                 req.Email,
			Phone:                 req.Phone,
			DateOfBirth:           dates.PatchPtr(req.DateOfBirth),
			Status:                req.Status,
			EmergencyContactName:  req.EmergencyContactName,
			EmergencyContactPhone: req.EmergencyContactPhone,
			MedicalNotes:          req.MedicalNotes,
			AllowOverdraft:        req.AllowOverdraft,
			DefaultRateMinor:      req.DefaultRateMinor,
			Notes:                 req.Notes,
		})
		if err != nil {
			return err
		}
		httpx.JSON(w, r, http.StatusOK, client)
		return nil
	})
}

func (h *Handler) getClient(w http.ResponseWriter, r *http.Request) {
	clientID, err := pathID(r, "clientID")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	// Medical notes are returned only when asked for explicitly, so a routine
	// profile fetch by a front-end cannot spread them around.
	withMedical := r.URL.Query().Get("include_medical") == "true"

	h.withTenant(w, r, func(tx pgx.Tx, _ ids.ID) error {
		var client Client
		var err error
		if withMedical {
			client, err = h.svc.GetWithMedical(r.Context(), tx, clientID)
		} else {
			client, err = h.svc.Get(r.Context(), tx, clientID)
		}
		if err != nil {
			return err
		}
		httpx.JSON(w, r, http.StatusOK, client)
		return nil
	})
}

func (h *Handler) listClients(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	filter := ListFilter{
		Status: Status(q.Get("status")),
		Search: q.Get("search"),
		Limit:  atoiOr(q.Get("limit"), 50),
		Offset: atoiOr(q.Get("offset"), 0),
	}
	if raw := q.Get("tag_id"); raw != "" {
		tagID, err := ids.Parse(raw)
		if err != nil {
			httpx.Error(w, r, errs.Invalid(errs.CodeValidation, "tag_id is not a valid identifier"))
			return
		}
		filter.TagID = &tagID
	}

	h.withTenant(w, r, func(tx pgx.Tx, _ ids.ID) error {
		clients, err := h.svc.List(r.Context(), tx, filter)
		if err != nil {
			return err
		}
		if clients == nil {
			clients = []Client{}
		}
		httpx.JSON(w, r, http.StatusOK, map[string]any{"clients": clients})
		return nil
	})
}

func (h *Handler) deleteClient(w http.ResponseWriter, r *http.Request) {
	clientID, err := pathID(r, "clientID")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	h.withTenant(w, r, func(tx pgx.Tx, _ ids.ID) error {
		if err := h.svc.Delete(r.Context(), tx, clientID); err != nil {
			return err
		}
		httpx.NoContent(w, r)
		return nil
	})
}

// ---------------------------------------------------------------------------
// Tags
// ---------------------------------------------------------------------------

func (h *Handler) createTag(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name   string  `json:"name"`
		Colour *string `json:"colour"`
	}
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	h.withTenant(w, r, func(tx pgx.Tx, tenantID ids.ID) error {
		tag, err := h.svc.CreateTag(r.Context(), tx, tenantID, req.Name, req.Colour)
		if err != nil {
			return err
		}
		httpx.JSON(w, r, http.StatusCreated, tag)
		return nil
	})
}

func (h *Handler) listTags(w http.ResponseWriter, r *http.Request) {
	h.withTenant(w, r, func(tx pgx.Tx, _ ids.ID) error {
		tags, err := h.svc.ListTags(r.Context(), tx)
		if err != nil {
			return err
		}
		if tags == nil {
			tags = []Tag{}
		}
		httpx.JSON(w, r, http.StatusOK, map[string]any{"tags": tags})
		return nil
	})
}

func (h *Handler) deleteTag(w http.ResponseWriter, r *http.Request) {
	tagID, err := pathID(r, "tagID")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	h.withTenant(w, r, func(tx pgx.Tx, _ ids.ID) error {
		if err := h.svc.DeleteTag(r.Context(), tx, tagID); err != nil {
			return err
		}
		httpx.NoContent(w, r)
		return nil
	})
}

func (h *Handler) setClientTags(w http.ResponseWriter, r *http.Request) {
	clientID, err := pathID(r, "clientID")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	var req struct {
		TagIDs []ids.ID `json:"tag_ids"`
	}
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	h.withTenant(w, r, func(tx pgx.Tx, tenantID ids.ID) error {
		if err := h.svc.SetTags(r.Context(), tx, tenantID, clientID, req.TagIDs); err != nil {
			return err
		}
		client, err := h.svc.Get(r.Context(), tx, clientID)
		if err != nil {
			return err
		}
		httpx.JSON(w, r, http.StatusOK, client)
		return nil
	})
}

// ---------------------------------------------------------------------------
// PAR-Q
// ---------------------------------------------------------------------------

func (h *Handler) submitParQ(w http.ResponseWriter, r *http.Request) {
	clientID, err := pathID(r, "clientID")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	var req struct {
		Answers []ParQAnswer `json:"answers"`
	}
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	h.withTenant(w, r, func(tx pgx.Tx, tenantID ids.ID) error {
		response, err := h.svc.SubmitParQ(r.Context(), tx, tenantID, clientID, req.Answers)
		if err != nil {
			return err
		}
		httpx.JSON(w, r, http.StatusCreated, response)
		return nil
	})
}

func (h *Handler) latestParQ(w http.ResponseWriter, r *http.Request) {
	clientID, err := pathID(r, "clientID")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	h.withTenant(w, r, func(tx pgx.Tx, _ ids.ID) error {
		response, err := h.svc.LatestParQ(r.Context(), tx, clientID)
		if err != nil {
			return err
		}
		httpx.JSON(w, r, http.StatusOK, response)
		return nil
	})
}

// ---------------------------------------------------------------------------
// Waivers
// ---------------------------------------------------------------------------

func (h *Handler) createWaiver(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title string `json:"title"`
		Body  string `json:"body"`
	}
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	h.withTenant(w, r, func(tx pgx.Tx, tenantID ids.ID) error {
		waiver, err := h.svc.CreateWaiver(r.Context(), tx, tenantID, req.Title, req.Body)
		if err != nil {
			return err
		}
		httpx.JSON(w, r, http.StatusCreated, waiver)
		return nil
	})
}

func (h *Handler) listWaivers(w http.ResponseWriter, r *http.Request) {
	activeOnly := r.URL.Query().Get("active") != "false"
	h.withTenant(w, r, func(tx pgx.Tx, _ ids.ID) error {
		waivers, err := h.svc.ListWaivers(r.Context(), tx, activeOnly)
		if err != nil {
			return err
		}
		if waivers == nil {
			waivers = []Waiver{}
		}
		httpx.JSON(w, r, http.StatusOK, map[string]any{"waivers": waivers})
		return nil
	})
}

func (h *Handler) signWaiver(w http.ResponseWriter, r *http.Request) {
	clientID, err := pathID(r, "clientID")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	waiverID, err := pathID(r, "waiverID")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	var req struct {
		SignedName string  `json:"signed_name"`
		MediaID    *ids.ID `json:"signature_media_id"`
	}
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	h.withTenant(w, r, func(tx pgx.Tx, tenantID ids.ID) error {
		sig, err := h.svc.Sign(r.Context(), tx, tenantID, SignInput{
			WaiverID:   waiverID,
			ClientID:   clientID,
			SignedName: req.SignedName,
			MediaID:    req.MediaID,
			IP:         httpx.ClientAddress(r),
		})
		if err != nil {
			return err
		}
		httpx.JSON(w, r, http.StatusCreated, sig)
		return nil
	})
}

func (h *Handler) listSignatures(w http.ResponseWriter, r *http.Request) {
	clientID, err := pathID(r, "clientID")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	h.withTenant(w, r, func(tx pgx.Tx, _ ids.ID) error {
		sigs, err := h.svc.SignaturesFor(r.Context(), tx, clientID)
		if err != nil {
			return err
		}
		if sigs == nil {
			sigs = []Signature{}
		}
		httpx.JSON(w, r, http.StatusOK, map[string]any{"signatures": sigs})
		return nil
	})
}

// ---------------------------------------------------------------------------
// Biometrics
// ---------------------------------------------------------------------------

func (h *Handler) recordBiometrics(w http.ResponseWriter, r *http.Request) {
	clientID, err := pathID(r, "clientID")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	var req struct {
		MeasuredOn     *dates.Date      `json:"measured_on"`
		WeightGrams    *int32           `json:"weight_grams"`
		BodyFatBP      *int32           `json:"body_fat_bp"`
		Circumferences map[string]int32 `json:"circumferences"`
		Notes          string           `json:"notes"`
	}
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}

	in := BiometricInput{
		ClientID:       clientID,
		WeightGrams:    req.WeightGrams,
		BodyFatBP:      req.BodyFatBP,
		Circumferences: req.Circumferences,
		Notes:          req.Notes,
	}
	in.MeasuredOn = req.MeasuredOn.OrElse(time.Time{})

	h.withTenant(w, r, func(tx pgx.Tx, tenantID ids.ID) error {
		entry, err := h.svc.RecordBiometrics(r.Context(), tx, tenantID, in)
		if err != nil {
			return err
		}
		httpx.JSON(w, r, http.StatusCreated, entry)
		return nil
	})
}

func (h *Handler) listBiometrics(w http.ResponseWriter, r *http.Request) {
	clientID, err := pathID(r, "clientID")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	limit := atoiOr(r.URL.Query().Get("limit"), 100)

	h.withTenant(w, r, func(tx pgx.Tx, _ ids.ID) error {
		entries, err := h.svc.BiometricHistory(r.Context(), tx, clientID, limit)
		if err != nil {
			return err
		}
		if entries == nil {
			entries = []BiometricEntry{}
		}
		httpx.JSON(w, r, http.StatusOK, map[string]any{"entries": entries})
		return nil
	})
}

func atoiOr(raw string, def int) int {
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return def
	}
	return n
}
