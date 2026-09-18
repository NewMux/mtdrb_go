package scheduling

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/NewMux/mtdrb_go/internal/billing"
	"github.com/NewMux/mtdrb_go/internal/db"
	"github.com/NewMux/mtdrb_go/internal/httpx"
	"github.com/NewMux/mtdrb_go/internal/platform/errs"
	"github.com/NewMux/mtdrb_go/internal/platform/ids"
	"github.com/NewMux/mtdrb_go/internal/platform/money"
	"github.com/NewMux/mtdrb_go/internal/tenancy"
)

// Handler exposes the calendar and attendance endpoints.
type Handler struct {
	svc     *Service
	billing *billing.Service
	pool    *db.Pool
}

// NewHandler builds the scheduling handler.
func NewHandler(svc *Service, b *billing.Service, pool *db.Pool) *Handler {
	return &Handler{svc: svc, billing: b, pool: pool}
}

// Routes returns the scheduling subtree.
func (h *Handler) Routes() http.Handler {
	r := chi.NewRouter()

	r.Route("/session-types", func(t chi.Router) {
		t.Get("/", h.listSessionTypes)
		t.Post("/", h.createSessionType)
	})

	r.Get("/", h.listSessions)
	r.Post("/", h.bookSession)
	r.Post("/recurring", h.recur)
	// The bulk roster check-off: confirm a whole day in one action.
	r.Post("/mark-day", h.markDay)

	r.Route("/{sessionID}", func(s chi.Router) {
		s.Get("/", h.getSession)
		s.Delete("/", h.cancelSession)
		s.Post("/reschedule", h.reschedule)
		s.Post("/attendees", h.addAttendee)
	})

	r.Post("/attendees/{attendeeID}/mark", h.markAttendance)
	return r
}

// CreditRoutes returns the package and credit endpoints.
func (h *Handler) CreditRoutes() http.Handler {
	r := chi.NewRouter()
	r.Post("/packages", h.grantPackage)
	r.Get("/clients/{clientID}/balance", h.creditBalance)
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

// markedBy identifies the trainer recording an attendance, for the audit trail.
func markedBy(r *http.Request) *ids.ID {
	principal, ok := tenancy.FromContext(r.Context())
	if !ok {
		return nil
	}
	return &principal.SubjectID
}

func pathID(r *http.Request, name string) (ids.ID, error) {
	id, err := ids.Parse(chi.URLParam(r, name))
	if err != nil {
		return ids.Nil, errs.Invalid(errs.CodeValidation, "%s is not a valid identifier", name)
	}
	return id, nil
}

// ---------------------------------------------------------------------------
// Session types
// ---------------------------------------------------------------------------

func (h *Handler) createSessionType(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name            string  `json:"name"`
		DurationMinutes int     `json:"duration_minutes"`
		Capacity        int     `json:"capacity"`
		CreditCost      int     `json:"credit_cost"`
		Colour          *string `json:"colour"`
	}
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	h.withTenant(w, r, func(tx pgx.Tx, tenantID ids.ID) error {
		st, err := h.svc.CreateSessionType(r.Context(), tx, tenantID, CreateSessionTypeInput{
			Name:            req.Name,
			DurationMinutes: req.DurationMinutes,
			Capacity:        req.Capacity,
			CreditCost:      req.CreditCost,
			Colour:          req.Colour,
		})
		if err != nil {
			return err
		}
		httpx.JSON(w, r, http.StatusCreated, st)
		return nil
	})
}

func (h *Handler) listSessionTypes(w http.ResponseWriter, r *http.Request) {
	h.withTenant(w, r, func(tx pgx.Tx, _ ids.ID) error {
		types, err := h.svc.ListSessionTypes(r.Context(), tx)
		if err != nil {
			return err
		}
		if types == nil {
			types = []SessionType{}
		}
		httpx.JSON(w, r, http.StatusOK, map[string]any{"session_types": types})
		return nil
	})
}

// ---------------------------------------------------------------------------
// Sessions
// ---------------------------------------------------------------------------

func (h *Handler) bookSession(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SessionTypeID ids.ID    `json:"session_type_id"`
		StartsAt      time.Time `json:"starts_at"`
		ClientIDs     []ids.ID  `json:"client_ids"`
		Location      string    `json:"location"`
		Notes         string    `json:"notes"`
	}
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	h.withTenant(w, r, func(tx pgx.Tx, tenantID ids.ID) error {
		session, err := h.svc.Book(r.Context(), tx, tenantID, BookInput{
			SessionTypeID: req.SessionTypeID,
			StartsAt:      req.StartsAt,
			ClientIDs:     req.ClientIDs,
			Location:      req.Location,
			Notes:         req.Notes,
		})
		if err != nil {
			return err
		}
		httpx.JSON(w, r, http.StatusCreated, session)
		return nil
	})
}

func (h *Handler) listSessions(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	from, err := parseTime(q.Get("from"))
	if err != nil {
		httpx.Error(w, r, errs.Invalid(errs.CodeValidation, "from must be an RFC 3339 timestamp"))
		return
	}
	to, err := parseTime(q.Get("to"))
	if err != nil {
		httpx.Error(w, r, errs.Invalid(errs.CodeValidation, "to must be an RFC 3339 timestamp"))
		return
	}
	if from.IsZero() {
		from = time.Now().UTC().AddDate(0, 0, -7)
	}
	if to.IsZero() {
		to = from.AddDate(0, 0, 28)
	}

	h.withTenant(w, r, func(tx pgx.Tx, _ ids.ID) error {
		sessions, err := h.svc.ListRange(r.Context(), tx, from, to)
		if err != nil {
			return err
		}
		if sessions == nil {
			sessions = []Session{}
		}
		httpx.JSON(w, r, http.StatusOK, map[string]any{"sessions": sessions})
		return nil
	})
}

func (h *Handler) getSession(w http.ResponseWriter, r *http.Request) {
	sessionID, err := pathID(r, "sessionID")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	h.withTenant(w, r, func(tx pgx.Tx, _ ids.ID) error {
		session, err := h.svc.Get(r.Context(), tx, sessionID)
		if err != nil {
			return err
		}
		httpx.JSON(w, r, http.StatusOK, session)
		return nil
	})
}

func (h *Handler) cancelSession(w http.ResponseWriter, r *http.Request) {
	sessionID, err := pathID(r, "sessionID")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	h.withTenant(w, r, func(tx pgx.Tx, _ ids.ID) error {
		if err := h.svc.Cancel(r.Context(), tx, sessionID); err != nil {
			return err
		}
		httpx.NoContent(w, r)
		return nil
	})
}

func (h *Handler) reschedule(w http.ResponseWriter, r *http.Request) {
	sessionID, err := pathID(r, "sessionID")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	var req struct {
		StartsAt time.Time `json:"starts_at"`
	}
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	h.withTenant(w, r, func(tx pgx.Tx, tenantID ids.ID) error {
		session, err := h.svc.Reschedule(r.Context(), tx, tenantID, sessionID, req.StartsAt)
		if err != nil {
			return err
		}
		httpx.JSON(w, r, http.StatusOK, session)
		return nil
	})
}

func (h *Handler) addAttendee(w http.ResponseWriter, r *http.Request) {
	sessionID, err := pathID(r, "sessionID")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	var req struct {
		ClientID ids.ID `json:"client_id"`
	}
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	h.withTenant(w, r, func(tx pgx.Tx, tenantID ids.ID) error {
		attendee, err := h.svc.AddAttendee(r.Context(), tx, tenantID, sessionID, req.ClientID)
		if err != nil {
			return err
		}
		httpx.JSON(w, r, http.StatusCreated, attendee)
		return nil
	})
}

func (h *Handler) recur(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SessionTypeID ids.ID    `json:"session_type_id"`
		ClientIDs     []ids.ID  `json:"client_ids"`
		Weekdays      []int     `json:"weekdays"`
		StartsOn      time.Time `json:"starts_on"`
		EndsOn        time.Time `json:"ends_on"`
		TimeOfDay     string    `json:"time_of_day"`
		Timezone      string    `json:"timezone"`
		Location      string    `json:"location"`
	}
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	timeOfDay, err := parseClockTime(req.TimeOfDay)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	h.withTenant(w, r, func(tx pgx.Tx, tenantID ids.ID) error {
		result, err := h.svc.Recur(r.Context(), tx, tenantID, RecurInput{
			SessionTypeID: req.SessionTypeID,
			ClientIDs:     req.ClientIDs,
			Weekdays:      req.Weekdays,
			StartsOn:      req.StartsOn,
			EndsOn:        req.EndsOn,
			TimeOfDay:     timeOfDay,
			Timezone:      req.Timezone,
			Location:      req.Location,
		})
		if err != nil {
			return err
		}
		httpx.JSON(w, r, http.StatusCreated, result)
		return nil
	})
}

// ---------------------------------------------------------------------------
// Attendance
// ---------------------------------------------------------------------------

func (h *Handler) markAttendance(w http.ResponseWriter, r *http.Request) {
	attendeeID, err := pathID(r, "attendeeID")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	var req struct {
		Status         AttendanceStatus `json:"status"`
		Notes          string           `json:"notes"`
		AllowOverdraft bool             `json:"allow_overdraft"`
	}
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	h.withTenant(w, r, func(tx pgx.Tx, tenantID ids.ID) error {
		result, err := h.svc.Mark(r.Context(), tx, tenantID, MarkInput{
			AttendeeID:     attendeeID,
			Status:         req.Status,
			Notes:          req.Notes,
			MarkedBy:       markedBy(r),
			AllowOverdraft: req.AllowOverdraft,
		})
		if err != nil {
			return err
		}
		httpx.JSON(w, r, http.StatusOK, result)
		return nil
	})
}

func (h *Handler) markDay(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Day    time.Time        `json:"day"`
		Status AttendanceStatus `json:"status"`
	}
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	if req.Status == "" {
		req.Status = Completed
	}
	h.withTenant(w, r, func(tx pgx.Tx, tenantID ids.ID) error {
		result, err := h.svc.MarkDay(r.Context(), tx, tenantID, req.Day, req.Status, markedBy(r))
		if err != nil {
			return err
		}
		httpx.JSON(w, r, http.StatusOK, result)
		return nil
	})
}

// ---------------------------------------------------------------------------
// Packages and credits
// ---------------------------------------------------------------------------

func (h *Handler) grantPackage(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ClientID       ids.ID     `json:"client_id"`
		Name           string     `json:"name"`
		Credits        int        `json:"credits"`
		UnitPriceMinor int64      `json:"unit_price_minor"`
		Currency       string     `json:"currency"`
		PurchasedOn    *time.Time `json:"purchased_on"`
		ExpiresAt      *time.Time `json:"expires_at"`
	}
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	h.withTenant(w, r, func(tx pgx.Tx, tenantID ids.ID) error {
		currency := req.Currency
		if currency == "" {
			if err := tx.QueryRow(r.Context(),
				`SELECT default_currency FROM tenants WHERE id = $1`, tenantID).Scan(&currency); err != nil {
				return errs.Internal(err, "load tenant currency")
			}
		}
		in := billing.GrantInput{
			ClientID:  req.ClientID,
			Name:      req.Name,
			Credits:   req.Credits,
			UnitPrice: money.New(req.UnitPriceMinor, currency),
			ExpiresAt: req.ExpiresAt,
		}
		if req.PurchasedOn != nil {
			in.PurchasedOn = *req.PurchasedOn
		}
		pkg, err := h.billing.Grant(r.Context(), tx, tenantID, in)
		if err != nil {
			return err
		}
		httpx.JSON(w, r, http.StatusCreated, pkg)
		return nil
	})
}

func (h *Handler) creditBalance(w http.ResponseWriter, r *http.Request) {
	clientID, err := pathID(r, "clientID")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	h.withTenant(w, r, func(tx pgx.Tx, _ ids.ID) error {
		balance, err := h.billing.BalanceFor(r.Context(), tx, clientID)
		if err != nil {
			return err
		}
		httpx.JSON(w, r, http.StatusOK, balance)
		return nil
	})
}

func parseTime(raw string) (time.Time, error) {
	if raw == "" {
		return time.Time{}, nil
	}
	return time.Parse(time.RFC3339, raw)
}

// parseClockTime reads a wall-clock time such as "07:30".
func parseClockTime(raw string) (time.Duration, error) {
	if raw == "" {
		return 0, errs.Invalid(errs.CodeValidation, "time_of_day is required").
			WithField("time_of_day", "is required, such as 07:30")
	}
	t, err := time.Parse("15:04", raw)
	if err != nil {
		return 0, errs.Invalid(errs.CodeValidation, "time_of_day must look like 07:30").
			WithField("time_of_day", "must be HH:MM")
	}
	return time.Duration(t.Hour())*time.Hour + time.Duration(t.Minute())*time.Minute, nil
}
