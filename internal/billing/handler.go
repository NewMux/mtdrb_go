package billing

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/NewMux/mtdrb_go/internal/db"
	"github.com/NewMux/mtdrb_go/internal/httpx"
	"github.com/NewMux/mtdrb_go/internal/ledger"
	"github.com/NewMux/mtdrb_go/internal/platform/dates"
	"github.com/NewMux/mtdrb_go/internal/platform/errs"
	"github.com/NewMux/mtdrb_go/internal/platform/ids"
	"github.com/NewMux/mtdrb_go/internal/platform/money"
	"github.com/NewMux/mtdrb_go/internal/tenancy"
)

// Handler exposes invoicing, payments and sharing.
type Handler struct {
	svc     *Service
	pool    *db.Pool
	baseURL string
}

// NewHandler builds the billing handler.
func NewHandler(svc *Service, pool *db.Pool, baseURL string) *Handler {
	return &Handler{svc: svc, pool: pool, baseURL: baseURL}
}

// InvoiceRoutes returns the trainer-facing invoicing subtree.
func (h *Handler) InvoiceRoutes() http.Handler {
	r := chi.NewRouter()
	r.Get("/", h.listInvoices)
	r.Post("/", h.createDraft)

	r.Route("/{invoiceID}", func(i chi.Router) {
		i.Get("/", h.getInvoice)
		i.Patch("/", h.updateDraft)
		i.Delete("/", h.deleteDraft)
		i.Post("/issue", h.issue)
		i.Post("/void", h.void)
		i.Post("/share", h.createShare)
		i.Delete("/share", h.revokeShare)
		i.Get("/payments", h.listPayments)
		i.Post("/payments", h.recordPayment)
	})
	return r
}

// PaymentMethodRoutes returns the settlement-instruction endpoints.
func (h *Handler) PaymentMethodRoutes() http.Handler {
	r := chi.NewRouter()
	r.Get("/", h.listPaymentMethods)
	r.Post("/", h.createPaymentMethod)
	r.Delete("/{methodID}", h.archivePaymentMethod)
	return r
}

// ReceivablesRoutes returns the outstanding-receivables report.
func (h *Handler) ReceivablesRoutes() http.Handler {
	r := chi.NewRouter()
	r.Get("/", h.receivables)
	return r
}

// PublicRoutes returns the unauthenticated share-link subtree.
//
// Mounted outside the authenticated group: the whole point is that a client
// with no account can open it.
func (h *Handler) PublicRoutes() http.Handler {
	r := chi.NewRouter()
	r.Get("/invoices/{token}", h.publicInvoice)
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

func actor(r *http.Request) *ids.ID {
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
// Invoices
// ---------------------------------------------------------------------------

type lineRequest struct {
	Kind            LineKind    `json:"kind"`
	Description     string      `json:"description"`
	Quantity        int         `json:"quantity"`
	UnitPriceMinor  int64       `json:"unit_price_minor"`
	PackageCredits  *int        `json:"package_credits"`
	CreditsExpireOn *dates.Date `json:"credits_expire_on"`
}

type draftRequest struct {
	ClientID ids.ID        `json:"client_id"`
	Currency string        `json:"currency"`
	DueDate  *dates.Date   `json:"due_date"`
	Notes    string        `json:"notes"`
	Lines    []lineRequest `json:"lines"`
}

func (d draftRequest) toInput() CreateDraftInput {
	lines := make([]DraftLineInput, 0, len(d.Lines))
	for _, l := range d.Lines {
		// Mapped rather than converted: credits_expire_on arrives as a
		// calendar date and crosses into the service as a timestamp, so the
		// wire and service shapes are deliberately not identical.
		lines = append(lines, DraftLineInput{
			Kind:            l.Kind,
			Description:     l.Description,
			Quantity:        l.Quantity,
			UnitPriceMinor:  l.UnitPriceMinor,
			PackageCredits:  l.PackageCredits,
			CreditsExpireOn: l.CreditsExpireOn.TimePtr(),
		})
	}
	return CreateDraftInput{
		ClientID: d.ClientID, Currency: d.Currency,
		DueDate: d.DueDate.TimePtr(), Notes: d.Notes, Lines: lines,
	}
}

func (h *Handler) createDraft(w http.ResponseWriter, r *http.Request) {
	var req draftRequest
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	h.withTenant(w, r, func(tx pgx.Tx, tenantID ids.ID) error {
		invoice, err := h.svc.CreateDraft(r.Context(), tx, tenantID, req.toInput())
		if err != nil {
			return err
		}
		httpx.JSON(w, r, http.StatusCreated, invoice)
		return nil
	})
}

func (h *Handler) updateDraft(w http.ResponseWriter, r *http.Request) {
	invoiceID, err := pathID(r, "invoiceID")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	var req draftRequest
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	h.withTenant(w, r, func(tx pgx.Tx, tenantID ids.ID) error {
		invoice, err := h.svc.UpdateDraft(r.Context(), tx, tenantID, invoiceID, req.toInput())
		if err != nil {
			return err
		}
		httpx.JSON(w, r, http.StatusOK, invoice)
		return nil
	})
}

func (h *Handler) getInvoice(w http.ResponseWriter, r *http.Request) {
	invoiceID, err := pathID(r, "invoiceID")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	h.withTenant(w, r, func(tx pgx.Tx, _ ids.ID) error {
		invoice, err := h.svc.GetInvoice(r.Context(), tx, invoiceID)
		if err != nil {
			return err
		}
		httpx.JSON(w, r, http.StatusOK, invoice)
		return nil
	})
}

func (h *Handler) listInvoices(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	filter := InvoiceFilter{
		Status:          InvoiceStatus(q.Get("status")),
		OutstandingOnly: q.Get("outstanding") == "true",
		Limit:           atoiOr(q.Get("limit"), 50),
		Offset:          atoiOr(q.Get("offset"), 0),
	}
	if raw := q.Get("client_id"); raw != "" {
		clientID, err := ids.Parse(raw)
		if err != nil {
			httpx.Error(w, r, errs.Invalid(errs.CodeValidation, "client_id is not a valid identifier"))
			return
		}
		filter.ClientID = &clientID
	}

	h.withTenant(w, r, func(tx pgx.Tx, _ ids.ID) error {
		invoices, err := h.svc.ListInvoices(r.Context(), tx, filter)
		if err != nil {
			return err
		}
		if invoices == nil {
			invoices = []Invoice{}
		}
		httpx.JSON(w, r, http.StatusOK, map[string]any{"invoices": invoices})
		return nil
	})
}

func (h *Handler) deleteDraft(w http.ResponseWriter, r *http.Request) {
	invoiceID, err := pathID(r, "invoiceID")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	h.withTenant(w, r, func(tx pgx.Tx, _ ids.ID) error {
		if err := h.svc.DeleteDraft(r.Context(), tx, invoiceID); err != nil {
			return err
		}
		httpx.NoContent(w, r)
		return nil
	})
}

func (h *Handler) issue(w http.ResponseWriter, r *http.Request) {
	invoiceID, err := pathID(r, "invoiceID")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	var req struct {
		IssueDate       *dates.Date `json:"issue_date"`
		DueDate         *dates.Date `json:"due_date"`
		PaymentMethodID *ids.ID     `json:"payment_method_id"`
	}
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	h.withTenant(w, r, func(tx pgx.Tx, tenantID ids.ID) error {
		in := IssueInput{
			InvoiceID:       invoiceID,
			DueDate:         req.DueDate.TimePtr(),
			PaymentMethodID: req.PaymentMethodID,
			IssuedBy:        actor(r),
		}
		in.IssueDate = req.IssueDate.OrElse(time.Time{})
		invoice, err := h.svc.Issue(r.Context(), tx, tenantID, in)
		if err != nil {
			return err
		}
		httpx.JSON(w, r, http.StatusOK, invoice)
		return nil
	})
}

func (h *Handler) void(w http.ResponseWriter, r *http.Request) {
	invoiceID, err := pathID(r, "invoiceID")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	h.withTenant(w, r, func(tx pgx.Tx, tenantID ids.ID) error {
		invoice, err := h.svc.Void(r.Context(), tx, tenantID, VoidInput{
			InvoiceID: invoiceID, Reason: req.Reason, VoidedBy: actor(r),
		})
		if err != nil {
			return err
		}
		httpx.JSON(w, r, http.StatusOK, invoice)
		return nil
	})
}

// ---------------------------------------------------------------------------
// Sharing
// ---------------------------------------------------------------------------

func (h *Handler) createShare(w http.ResponseWriter, r *http.Request) {
	invoiceID, err := pathID(r, "invoiceID")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	h.withTenant(w, r, func(tx pgx.Tx, _ ids.ID) error {
		link, err := h.svc.CreateShareLink(r.Context(), tx, invoiceID, h.baseURL)
		if err != nil {
			return err
		}
		// The plaintext token exists in this response and nowhere else.
		httpx.JSON(w, r, http.StatusCreated, link)
		return nil
	})
}

func (h *Handler) revokeShare(w http.ResponseWriter, r *http.Request) {
	invoiceID, err := pathID(r, "invoiceID")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	h.withTenant(w, r, func(tx pgx.Tx, _ ids.ID) error {
		if err := h.svc.RevokeShareLink(r.Context(), tx, invoiceID); err != nil {
			return err
		}
		httpx.NoContent(w, r)
		return nil
	})
}

// publicInvoice serves a shared invoice to an unauthenticated visitor.
//
// The token is resolved to a tenant first, then the invoice is read inside a
// transaction bound to that tenant, so the ordinary row-level policies still
// apply to everything after the lookup.
func (h *Handler) publicInvoice(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")

	invoiceID, tenantID, err := h.svc.ResolveShareToken(r.Context(), h.pool, token)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	var public PublicInvoice
	if err := h.pool.InTenantTx(r.Context(), tenantID, func(tx pgx.Tx) error {
		var err error
		public, err = h.svc.PublicView(r.Context(), tx, invoiceID)
		return err
	}); err != nil {
		httpx.Error(w, r, err)
		return
	}

	// A client opening the link in WhatsApp gets the page; the Expo client
	// asks for JSON.
	if wantsJSON(r) {
		httpx.JSON(w, r, http.StatusOK, public)
		return
	}
	renderInvoicePage(w, r, public)
}

// wantsJSON distinguishes the app from a browser.
//
// A client tapping the link in WhatsApp gets the rendered page; the Expo
// client asks for JSON and gets the data. HTML is the default because the
// link exists to be opened by a person.
func wantsJSON(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "json")
}

// ---------------------------------------------------------------------------
// Payments
// ---------------------------------------------------------------------------

func (h *Handler) recordPayment(w http.ResponseWriter, r *http.Request) {
	invoiceID, err := pathID(r, "invoiceID")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	var req struct {
		AmountMinor int64             `json:"amount_minor"`
		Currency    string            `json:"currency"`
		Instrument  ledger.Instrument `json:"instrument"`
		ReceivedOn  *dates.Date       `json:"received_on"`
		Reference   string            `json:"reference"`
		Notes       string            `json:"notes"`
	}
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}

	h.withTenant(w, r, func(tx pgx.Tx, tenantID ids.ID) error {
		currency := req.Currency
		if currency == "" {
			if err := tx.QueryRow(r.Context(),
				`SELECT currency FROM invoices WHERE id = $1`, invoiceID).Scan(&currency); err != nil {
				if db.IsNoRows(err) {
					return errs.NotFound("invoice")
				}
				return errs.Internal(err, "load invoice currency")
			}
		}
		in := RecordPaymentInput{
			InvoiceID:  invoiceID,
			Amount:     money.New(req.AmountMinor, currency),
			Instrument: req.Instrument,
			Reference:  req.Reference,
			Notes:      req.Notes,
			RecordedBy: actor(r),
		}
		in.ReceivedOn = req.ReceivedOn.OrElse(time.Time{})
		result, err := h.svc.RecordPayment(r.Context(), tx, tenantID, in)
		if err != nil {
			return err
		}
		httpx.JSON(w, r, http.StatusCreated, result)
		return nil
	})
}

func (h *Handler) listPayments(w http.ResponseWriter, r *http.Request) {
	invoiceID, err := pathID(r, "invoiceID")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	h.withTenant(w, r, func(tx pgx.Tx, _ ids.ID) error {
		payments, err := h.svc.PaymentsFor(r.Context(), tx, invoiceID)
		if err != nil {
			return err
		}
		if payments == nil {
			payments = []Payment{}
		}
		httpx.JSON(w, r, http.StatusOK, map[string]any{"payments": payments})
		return nil
	})
}

func (h *Handler) receivables(w http.ResponseWriter, r *http.Request) {
	h.withTenant(w, r, func(tx pgx.Tx, tenantID ids.ID) error {
		var currency string
		if err := tx.QueryRow(r.Context(),
			`SELECT default_currency FROM tenants WHERE id = $1`, tenantID).Scan(&currency); err != nil {
			return errs.Internal(err, "load tenant currency")
		}
		report, err := h.svc.ReceivablesReport(r.Context(), tx, currency)
		if err != nil {
			return err
		}
		httpx.JSON(w, r, http.StatusOK, report)
		return nil
	})
}

// ---------------------------------------------------------------------------
// Payment methods
// ---------------------------------------------------------------------------

func (h *Handler) createPaymentMethod(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Kind         MethodKind    `json:"kind"`
		Label        string        `json:"label"`
		Details      MethodDetails `json:"details"`
		Instructions string        `json:"instructions"`
		IsDefault    bool          `json:"is_default"`
		SortOrder    int           `json:"sort_order"`
	}
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	h.withTenant(w, r, func(tx pgx.Tx, tenantID ids.ID) error {
		method, err := h.svc.CreatePaymentMethod(r.Context(), tx, tenantID, CreateMethodInput(req))
		if err != nil {
			return err
		}
		httpx.JSON(w, r, http.StatusCreated, method)
		return nil
	})
}

func (h *Handler) listPaymentMethods(w http.ResponseWriter, r *http.Request) {
	h.withTenant(w, r, func(tx pgx.Tx, _ ids.ID) error {
		methods, err := h.svc.ListPaymentMethods(r.Context(), tx)
		if err != nil {
			return err
		}
		if methods == nil {
			methods = []PaymentMethod{}
		}
		httpx.JSON(w, r, http.StatusOK, map[string]any{"payment_methods": methods})
		return nil
	})
}

func (h *Handler) archivePaymentMethod(w http.ResponseWriter, r *http.Request) {
	methodID, err := pathID(r, "methodID")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	h.withTenant(w, r, func(tx pgx.Tx, _ ids.ID) error {
		if err := h.svc.ArchivePaymentMethod(r.Context(), tx, methodID); err != nil {
			return err
		}
		httpx.NoContent(w, r)
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
