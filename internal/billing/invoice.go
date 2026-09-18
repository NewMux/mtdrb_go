package billing

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/NewMux/mtdrb_go/internal/db"
	"github.com/NewMux/mtdrb_go/internal/ledger"
	"github.com/NewMux/mtdrb_go/internal/platform/errs"
	"github.com/NewMux/mtdrb_go/internal/platform/ids"
	"github.com/NewMux/mtdrb_go/internal/platform/money"
)

// An invoice is a document telling a client how to pay off-platform. CoachPulse
// takes no card details and moves no money; issuing an invoice records what is
// owed, and recording a payment records that it arrived.

// InvoiceStatus is where an invoice sits in its lifecycle.
type InvoiceStatus string

const (
	InvoiceDraft         InvoiceStatus = "draft"
	InvoiceIssued        InvoiceStatus = "issued"
	InvoicePartiallyPaid InvoiceStatus = "partially_paid"
	InvoiceSettled       InvoiceStatus = "settled"
	InvoiceVoid          InvoiceStatus = "void"
)

// LineKind decides how a line is accounted for, so it is explicit rather than
// inferred from whether credits happen to be set.
type LineKind string

const (
	// LinePackage sells prepaid credits: a promise of future training, so it
	// credits Deferred Revenue.
	LinePackage LineKind = "package"
	// LineService bills work already done, so it credits revenue directly.
	LineService LineKind = "service"
)

// InvoiceLine is one billed item.
type InvoiceLine struct {
	ID              ids.ID     `json:"id"`
	Kind            LineKind   `json:"kind"`
	Description     string     `json:"description"`
	Quantity        int        `json:"quantity"`
	UnitPriceMinor  int64      `json:"unit_price_minor"`
	AmountMinor     int64      `json:"amount_minor"`
	PackageCredits  *int       `json:"package_credits,omitempty"`
	CreditsExpireOn *time.Time `json:"credits_expire_on,omitempty"`
	SortOrder       int        `json:"sort_order"`
}

// Invoice is a bill issued to a client.
type Invoice struct {
	ID         ids.ID        `json:"id"`
	ClientID   ids.ID        `json:"client_id"`
	ClientName string        `json:"client_name,omitempty"`
	Number     *string       `json:"number,omitempty"`
	Status     InvoiceStatus `json:"status"`
	Currency   string        `json:"currency"`

	TotalMinor   int64 `json:"total_minor"`
	PaidMinor    int64 `json:"paid_minor"`
	BalanceMinor int64 `json:"balance_minor"`

	IssueDate *time.Time `json:"issue_date,omitempty"`
	DueDate   *time.Time `json:"due_date,omitempty"`
	Notes     string     `json:"notes"`

	// IsOverdue is derived, never stored. A stored flag would need a nightly
	// job to stay truthful, and an invoice that only becomes overdue once a
	// cron has run is a liability in a receivables report.
	IsOverdue   bool `json:"is_overdue"`
	DaysOverdue int  `json:"days_overdue,omitempty"`

	Lines               []InvoiceLine        `json:"lines"`
	PaymentInstructions *PaymentInstructions `json:"payment_instructions,omitempty"`

	JournalEntryID *ids.ID    `json:"journal_entry_id,omitempty"`
	IssuedAt       *time.Time `json:"issued_at,omitempty"`
	SettledAt      *time.Time `json:"settled_at,omitempty"`
	VoidedAt       *time.Time `json:"voided_at,omitempty"`
	VoidReason     string     `json:"void_reason,omitempty"`

	HasShareLink bool  `json:"has_share_link"`
	ServerSeq    int64 `json:"server_seq"`
}

// Total returns the invoice amount.
func (i Invoice) Total() money.Money { return money.New(i.TotalMinor, i.Currency) }

// Balance returns what is still owed.
func (i Invoice) Balance() money.Money { return money.New(i.BalanceMinor, i.Currency) }

// Payable reports whether a payment may be recorded against this invoice.
func (i Invoice) Payable() bool {
	return i.Status == InvoiceIssued || i.Status == InvoicePartiallyPaid
}

// DraftLineInput describes a line to bill.
type DraftLineInput struct {
	Kind            LineKind
	Description     string
	Quantity        int
	UnitPriceMinor  int64
	PackageCredits  *int
	CreditsExpireOn *time.Time
}

// CreateDraftInput describes a new draft invoice.
type CreateDraftInput struct {
	ClientID ids.ID
	Currency string
	DueDate  *time.Time
	Notes    string
	Lines    []DraftLineInput
}

// CreateDraft creates an unissued invoice.
//
// A draft posts nothing and takes no number: it is a working document, and a
// draft that is deleted must not leave a hole in the numbering.
func (s *Service) CreateDraft(ctx context.Context, tx pgx.Tx, tenantID ids.ID, in CreateDraftInput) (Invoice, error) {
	if len(in.Lines) == 0 {
		return Invoice{}, errs.Invalid(errs.CodeValidation, "an invoice needs at least one line").
			WithField("lines", "is required")
	}
	currency := strings.ToUpper(strings.TrimSpace(in.Currency))
	if currency == "" {
		if err := tx.QueryRow(ctx,
			`SELECT default_currency FROM tenants WHERE id = $1`, tenantID).Scan(&currency); err != nil {
			return Invoice{}, errs.Internal(err, "load tenant currency")
		}
	}

	invoiceID := ids.New()
	var total int64
	lines := make([]InvoiceLine, 0, len(in.Lines))

	for i, l := range in.Lines {
		line, err := validateLine(i, l)
		if err != nil {
			return Invoice{}, err
		}
		line.SortOrder = i
		total += line.AmountMinor
		lines = append(lines, line)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO invoices (id, tenant_id, client_id, currency, total_minor, due_date, notes)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		invoiceID, tenantID, in.ClientID, currency, total, in.DueDate, strings.TrimSpace(in.Notes),
	); err != nil {
		if db.IsForeignKeyViolation(err) {
			return Invoice{}, errs.NotFound("client")
		}
		return Invoice{}, errs.Internal(err, "create invoice")
	}

	if err := s.replaceLines(ctx, tx, tenantID, invoiceID, lines); err != nil {
		return Invoice{}, err
	}
	return s.GetInvoice(ctx, tx, invoiceID)
}

func validateLine(index int, l DraftLineInput) (InvoiceLine, error) {
	description := strings.TrimSpace(l.Description)
	if description == "" {
		return InvoiceLine{}, errs.Invalid(errs.CodeValidation, "line %d needs a description", index).
			WithField(fmt.Sprintf("lines.%d.description", index), "is required")
	}
	if l.Quantity <= 0 {
		return InvoiceLine{}, errs.Invalid(errs.CodeValidation, "line %d needs a positive quantity", index).
			WithField(fmt.Sprintf("lines.%d.quantity", index), "must be greater than zero")
	}
	if l.UnitPriceMinor < 0 {
		return InvoiceLine{}, errs.Invalid(errs.CodeValidation, "line %d has a negative price", index).
			WithField(fmt.Sprintf("lines.%d.unit_price_minor", index), "must not be negative")
	}

	kind := l.Kind
	if kind == "" {
		kind = LineService
	}
	switch kind {
	case LinePackage:
		if l.PackageCredits == nil || *l.PackageCredits <= 0 {
			return InvoiceLine{}, errs.Invalid(errs.CodeValidation,
				"line %d sells a package, so it needs a credit count", index).
				WithField(fmt.Sprintf("lines.%d.package_credits", index), "must be greater than zero")
		}
	case LineService:
		if l.PackageCredits != nil {
			return InvoiceLine{}, errs.Invalid(errs.CodeValidation,
				"line %d is a service, so it cannot carry package credits", index).
				WithField(fmt.Sprintf("lines.%d.package_credits", index), "is only valid on a package line")
		}
	default:
		return InvoiceLine{}, errs.Invalid(errs.CodeValidation, "line %d has an unknown kind %q", index, kind).
			WithField(fmt.Sprintf("lines.%d.kind", index), "must be package or service")
	}

	return InvoiceLine{
		ID:              ids.New(),
		Kind:            kind,
		Description:     description,
		Quantity:        l.Quantity,
		UnitPriceMinor:  l.UnitPriceMinor,
		AmountMinor:     int64(l.Quantity) * l.UnitPriceMinor,
		PackageCredits:  l.PackageCredits,
		CreditsExpireOn: l.CreditsExpireOn,
	}, nil
}

func (s *Service) replaceLines(ctx context.Context, tx pgx.Tx, tenantID, invoiceID ids.ID, lines []InvoiceLine) error {
	if _, err := tx.Exec(ctx, `DELETE FROM invoice_lines WHERE invoice_id = $1`, invoiceID); err != nil {
		return errs.Internal(err, "clear invoice lines")
	}
	for _, l := range lines {
		if _, err := tx.Exec(ctx, `
			INSERT INTO invoice_lines
				(id, tenant_id, invoice_id, kind, description, quantity,
				 unit_price_minor, amount_minor, package_credits, credits_expire_on, sort_order)
			VALUES ($1, $2, $3, $4::invoice_line_kind, $5, $6, $7, $8, $9, $10, $11)`,
			l.ID, tenantID, invoiceID, string(l.Kind), l.Description, l.Quantity,
			l.UnitPriceMinor, l.AmountMinor, l.PackageCredits, l.CreditsExpireOn, l.SortOrder,
		); err != nil {
			return errs.Internal(err, "insert invoice line")
		}
	}
	return nil
}

// UpdateDraft replaces a draft's lines and details.
//
// Only a draft may be edited. Once issued, an invoice is a document the client
// has been given; changing it would mean two parties holding different
// versions of the same numbered bill.
func (s *Service) UpdateDraft(ctx context.Context, tx pgx.Tx, tenantID, invoiceID ids.ID, in CreateDraftInput) (Invoice, error) {
	existing, err := s.GetInvoice(ctx, tx, invoiceID)
	if err != nil {
		return Invoice{}, err
	}
	if existing.Status != InvoiceDraft {
		return Invoice{}, errs.Conflict(errs.CodeInvalidTransition,
			"only a draft can be edited; this invoice is %s", existing.Status).
			WithMeta("status", string(existing.Status))
	}
	if len(in.Lines) == 0 {
		return Invoice{}, errs.Invalid(errs.CodeValidation, "an invoice needs at least one line").
			WithField("lines", "is required")
	}

	var total int64
	lines := make([]InvoiceLine, 0, len(in.Lines))
	for i, l := range in.Lines {
		line, err := validateLine(i, l)
		if err != nil {
			return Invoice{}, err
		}
		line.SortOrder = i
		total += line.AmountMinor
		lines = append(lines, line)
	}

	if _, err := tx.Exec(ctx, `
		UPDATE invoices SET total_minor = $2, due_date = $3, notes = $4
		 WHERE id = $1 AND status = 'draft'`,
		invoiceID, total, in.DueDate, strings.TrimSpace(in.Notes)); err != nil {
		return Invoice{}, errs.Internal(err, "update invoice")
	}
	if err := s.replaceLines(ctx, tx, tenantID, invoiceID, lines); err != nil {
		return Invoice{}, err
	}
	return s.GetInvoice(ctx, tx, invoiceID)
}

// DeleteDraft removes an unissued invoice.
func (s *Service) DeleteDraft(ctx context.Context, tx pgx.Tx, invoiceID ids.ID) error {
	tag, err := tx.Exec(ctx,
		`UPDATE invoices SET deleted_at = now()
		  WHERE id = $1 AND status = 'draft' AND deleted_at IS NULL`, invoiceID)
	if err != nil {
		return errs.Internal(err, "delete draft invoice")
	}
	if tag.RowsAffected() == 0 {
		return errs.Conflict(errs.CodeInvalidTransition,
			"only an unissued draft can be deleted; issued invoices are voided instead")
	}
	return nil
}

// IssueInput describes issuing a draft.
type IssueInput struct {
	InvoiceID ids.ID
	IssueDate time.Time
	DueDate   *time.Time
	// PaymentMethodID selects which settlement instructions to print. The
	// tenant's default is used when omitted.
	PaymentMethodID *ids.ID
	IssuedBy        *ids.ID
}

// Issue turns a draft into a numbered, posted invoice.
//
// Everything happens in the caller's transaction: the number is assigned, the
// ledger entry is posted, any package lines grant their credits, and the
// settlement instructions are snapshotted. A partially issued invoice — one
// with a number but no accounting, or credits but no liability — must not be
// able to exist.
func (s *Service) Issue(ctx context.Context, tx pgx.Tx, tenantID ids.ID, in IssueInput) (Invoice, error) {
	invoice, err := s.GetInvoice(ctx, tx, in.InvoiceID)
	if err != nil {
		return Invoice{}, err
	}
	if invoice.Status != InvoiceDraft {
		return Invoice{}, errs.Conflict(errs.CodeInvalidTransition,
			"this invoice is already %s", invoice.Status).
			WithMeta("status", string(invoice.Status))
	}
	if invoice.TotalMinor <= 0 {
		return Invoice{}, errs.Invalid(errs.CodeValidation, "an invoice must be for a positive amount")
	}

	issueDate := in.IssueDate
	if issueDate.IsZero() {
		issueDate = s.clock.Now()
	}
	issueDate = truncateToDay(issueDate)

	dueDate := in.DueDate
	if dueDate == nil {
		dueDate = invoice.DueDate
	}
	if dueDate != nil {
		d := truncateToDay(*dueDate)
		if d.Before(issueDate) {
			return Invoice{}, errs.Invalid(errs.CodeValidation,
				"an invoice cannot be due before it is issued").
				WithField("due_date", "must not be before the issue date")
		}
		dueDate = &d
	}

	number, err := s.nextInvoiceNumber(ctx, tx, tenantID, issueDate)
	if err != nil {
		return Invoice{}, err
	}

	instructions, err := s.instructionsFor(ctx, tx, in.PaymentMethodID)
	if err != nil {
		return Invoice{}, err
	}
	snapshot, err := json.Marshal(instructions)
	if err != nil {
		return Invoice{}, errs.Internal(err, "encode payment instructions")
	}

	// Split the total by how each line must be accounted for. A package line
	// is a promise of future training and credits Deferred Revenue; a service
	// line is work already done and credits revenue directly. One invoice can
	// legitimately contain both.
	var packageTotal, serviceTotal int64
	for _, l := range invoice.Lines {
		if l.Kind == LinePackage {
			packageTotal += l.AmountMinor
		} else {
			serviceTotal += l.AmountMinor
		}
	}

	// Built as a single entry rather than by calling the two ledger helpers in
	// turn, so one invoice always maps to exactly one journal entry — which is
	// what invoices.journal_entry_id assumes.
	entryLines := []ledger.Line{
		ledger.Debit(ledger.SlugAccountsReceivable,
			money.New(invoice.TotalMinor, invoice.Currency), "amount owed by client"),
	}
	if packageTotal > 0 {
		entryLines = append(entryLines, ledger.Credit(ledger.SlugDeferredRevenue,
			money.New(packageTotal, invoice.Currency), "unearned session credits"))
	}
	if serviceTotal > 0 {
		entryLines = append(entryLines, ledger.Credit(ledger.SlugTrainingRevenue,
			money.New(serviceTotal, invoice.Currency), "training delivered"))
	}

	posted, err := s.ledger.Post(ctx, tx, ledger.Entry{
		Date:       issueDate,
		Memo:       "invoice " + number,
		SourceType: ledger.SourceInvoiceIssued,
		SourceID:   &invoice.ID,
		Currency:   invoice.Currency,
		CreatedBy:  in.IssuedBy,
		Lines:      entryLines,
	})
	if err != nil {
		return Invoice{}, err
	}

	if _, err := tx.Exec(ctx, `
		UPDATE invoices
		   SET status = 'issued', number = $2, issue_date = $3, due_date = $4,
		       payment_instructions_snapshot = $5, journal_entry_id = $6, issued_at = now()
		 WHERE id = $1`,
		invoice.ID, number, issueDate, dueDate, snapshot, posted.ID); err != nil {
		if db.IsUniqueViolation(err, "invoices_tenant_number_key") {
			return Invoice{}, errs.Conflict("invoice_number_taken",
				"invoice number %s is already in use", number)
		}
		return Invoice{}, errs.Internal(err, "issue invoice")
	}

	// Package lines grant their credits now. Grant skips its own opening
	// entry when an invoice id is present, so the liability posted above is
	// not counted twice.
	for _, l := range invoice.Lines {
		if l.Kind != LinePackage || l.PackageCredits == nil {
			continue
		}
		credits := *l.PackageCredits * l.Quantity
		// The per-credit price is what drives revenue recognition on each
		// delivered session, so it is derived from this line rather than
		// from any tenant-level default.
		unitPrice := money.New(l.AmountMinor/int64(credits), invoice.Currency)
		if _, err := s.Grant(ctx, tx, tenantID, GrantInput{
			ClientID:    invoice.ClientID,
			InvoiceID:   &invoice.ID,
			Name:        l.Description,
			Credits:     credits,
			UnitPrice:   unitPrice,
			PurchasedOn: issueDate,
			ExpiresAt:   l.CreditsExpireOn,
		}); err != nil {
			return Invoice{}, err
		}
	}

	return s.GetInvoice(ctx, tx, invoice.ID)
}

// nextInvoiceNumber allocates the next number in an unbroken run.
//
// The counter row is taken FOR UPDATE rather than read from a sequence:
// sequences are non-transactional by design, so a rolled-back issue would burn
// a number and leave a hole that a tax inspector will ask about. Contention is
// nil for a single operator.
func (s *Service) nextInvoiceNumber(ctx context.Context, tx pgx.Tx, tenantID ids.ID, issueDate time.Time) (string, error) {
	year := issueDate.Year()

	var next int
	err := tx.QueryRow(ctx, `
		INSERT INTO invoice_counters (tenant_id, year, next_number)
		VALUES ($1, $2, 1)
		ON CONFLICT (tenant_id, year) DO UPDATE
		   SET next_number = invoice_counters.next_number + 1
		RETURNING next_number`, tenantID, year).Scan(&next)
	if err != nil {
		return "", errs.Internal(err, "allocate invoice number")
	}
	return fmt.Sprintf("INV-%d-%04d", year, next), nil
}

// VoidInput describes voiding an invoice.
type VoidInput struct {
	InvoiceID ids.ID
	Reason    string
	VoidedBy  *ids.ID
}

// Void cancels an invoice.
//
// A draft simply goes away; an issued invoice gets a reversing entry and its
// package voided with it.
//
// Voiding is refused once the invoice has been paid against or its credits
// consumed. Clawing back training a client has already done would corrupt both
// the credit ledger and the books, so the trainer is directed to record a
// refund instead — a real event, with its own entry.
func (s *Service) Void(ctx context.Context, tx pgx.Tx, tenantID ids.ID, in VoidInput) (Invoice, error) {
	invoice, err := s.GetInvoice(ctx, tx, in.InvoiceID)
	if err != nil {
		return Invoice{}, err
	}
	switch invoice.Status {
	case InvoiceVoid:
		return Invoice{}, errs.Conflict(errs.CodeInvalidTransition, "this invoice is already void")
	case InvoiceSettled, InvoicePartiallyPaid:
		return Invoice{}, errs.Conflict(errs.CodeInvalidTransition,
			"this invoice has payments recorded against it; record a refund instead of voiding").
			WithMeta("paid_minor", invoice.PaidMinor)
	}

	if invoice.Status == InvoiceDraft {
		if _, err := tx.Exec(ctx,
			`UPDATE invoices SET status = 'void', voided_at = now(), void_reason = $2
			  WHERE id = $1`, invoice.ID, strings.TrimSpace(in.Reason)); err != nil {
			return Invoice{}, errs.Internal(err, "void draft invoice")
		}
		return s.GetInvoice(ctx, tx, invoice.ID)
	}

	// Refuse if any of this invoice's credits have already been trained off.
	var consumed int
	if err := tx.QueryRow(ctx, `
		SELECT coalesce(sum(p.credits_total - p.credits_remaining), 0)
		  FROM packages p WHERE p.invoice_id = $1`, invoice.ID).Scan(&consumed); err != nil {
		return Invoice{}, errs.Internal(err, "check consumed credits")
	}
	if consumed > 0 {
		return Invoice{}, errs.Conflict(errs.CodeInvalidTransition,
			"%d credits from this invoice have already been used; record a refund instead of voiding", consumed).
			WithMeta("credits_consumed", consumed)
	}

	if invoice.JournalEntryID != nil {
		if _, err := s.ledger.Reverse(ctx, tx, *invoice.JournalEntryID,
			"invoice "+derefString(invoice.Number)+" voided", in.VoidedBy); err != nil {
			return Invoice{}, err
		}
	}

	// The credits go with the liability that funded them.
	if _, err := tx.Exec(ctx,
		`UPDATE packages SET status = 'void', credits_remaining = 0 WHERE invoice_id = $1`,
		invoice.ID); err != nil {
		return Invoice{}, errs.Internal(err, "void invoice packages")
	}

	if _, err := tx.Exec(ctx,
		`UPDATE invoices SET status = 'void', voided_at = now(), void_reason = $2 WHERE id = $1`,
		invoice.ID, strings.TrimSpace(in.Reason)); err != nil {
		return Invoice{}, errs.Internal(err, "void invoice")
	}
	return s.GetInvoice(ctx, tx, invoice.ID)
}

const invoiceColumns = `
	i.id, i.client_id, c.full_name, i.number, i.status::text, i.currency,
	i.total_minor, i.issue_date, i.due_date, i.notes,
	i.payment_instructions_snapshot, i.journal_entry_id,
	i.issued_at, i.settled_at, i.voided_at, coalesce(i.void_reason, ''),
	(i.share_token_hash IS NOT NULL AND i.share_revoked_at IS NULL),
	i.server_seq,
	coalesce((SELECT sum(p.amount_minor) FROM payments p WHERE p.invoice_id = i.id), 0)`

func (s *Service) scanInvoice(row pgx.Row) (Invoice, error) {
	var inv Invoice
	var status string
	var snapshot []byte
	if err := row.Scan(&inv.ID, &inv.ClientID, &inv.ClientName, &inv.Number, &status,
		&inv.Currency, &inv.TotalMinor, &inv.IssueDate, &inv.DueDate, &inv.Notes,
		&snapshot, &inv.JournalEntryID, &inv.IssuedAt, &inv.SettledAt, &inv.VoidedAt,
		&inv.VoidReason, &inv.HasShareLink, &inv.ServerSeq, &inv.PaidMinor); err != nil {
		return Invoice{}, err
	}
	inv.Status = InvoiceStatus(status)
	inv.BalanceMinor = inv.TotalMinor - inv.PaidMinor

	if len(snapshot) > 0 {
		var pi PaymentInstructions
		if err := json.Unmarshal(snapshot, &pi); err == nil {
			inv.PaymentInstructions = &pi
		}
	}

	// Overdue is computed here rather than stored, so it is correct the moment
	// the clock passes midnight without any job having run.
	if inv.DueDate != nil && inv.BalanceMinor > 0 &&
		(inv.Status == InvoiceIssued || inv.Status == InvoicePartiallyPaid) {
		today := truncateToDay(s.clock.Now())
		if inv.DueDate.Before(today) {
			inv.IsOverdue = true
			inv.DaysOverdue = int(today.Sub(*inv.DueDate).Hours() / 24)
		}
	}
	return inv, nil
}

// GetInvoice loads one invoice with its lines.
func (s *Service) GetInvoice(ctx context.Context, tx pgx.Tx, invoiceID ids.ID) (Invoice, error) {
	inv, err := s.scanInvoice(tx.QueryRow(ctx,
		`SELECT `+invoiceColumns+`
		   FROM invoices i JOIN clients c ON c.id = i.client_id
		  WHERE i.id = $1 AND i.deleted_at IS NULL`, invoiceID))
	if err != nil {
		if db.IsNoRows(err) {
			return Invoice{}, errs.NotFound("invoice")
		}
		return Invoice{}, errs.Internal(err, "load invoice")
	}
	lines, err := s.linesFor(ctx, tx, invoiceID)
	if err != nil {
		return Invoice{}, err
	}
	inv.Lines = lines
	return inv, nil
}

func (s *Service) linesFor(ctx context.Context, tx pgx.Tx, invoiceID ids.ID) ([]InvoiceLine, error) {
	rows, err := tx.Query(ctx, `
		SELECT id, kind::text, description, quantity, unit_price_minor, amount_minor,
		       package_credits, credits_expire_on, sort_order
		  FROM invoice_lines WHERE invoice_id = $1 ORDER BY sort_order`, invoiceID)
	if err != nil {
		return nil, errs.Internal(err, "load invoice lines")
	}
	defer rows.Close()

	var out []InvoiceLine
	for rows.Next() {
		var l InvoiceLine
		var kind string
		if err := rows.Scan(&l.ID, &kind, &l.Description, &l.Quantity, &l.UnitPriceMinor,
			&l.AmountMinor, &l.PackageCredits, &l.CreditsExpireOn, &l.SortOrder); err != nil {
			return nil, errs.Internal(err, "scan invoice line")
		}
		l.Kind = LineKind(kind)
		out = append(out, l)
	}
	if err := rows.Err(); err != nil {
		return nil, errs.Internal(err, "read invoice lines")
	}
	return out, nil
}

// InvoiceFilter narrows a listing.
type InvoiceFilter struct {
	ClientID *ids.ID
	Status   InvoiceStatus
	// OutstandingOnly limits results to invoices still owed.
	OutstandingOnly bool
	Limit           int
	Offset          int
}

// ListInvoices returns invoices matching the filter, newest first.
func (s *Service) ListInvoices(ctx context.Context, tx pgx.Tx, f InvoiceFilter) ([]Invoice, error) {
	if f.Limit <= 0 || f.Limit > 200 {
		f.Limit = 50
	}
	var status *string
	if f.Status != "" {
		v := string(f.Status)
		status = &v
	}

	rows, err := tx.Query(ctx, `
		SELECT `+invoiceColumns+`
		  FROM invoices i JOIN clients c ON c.id = i.client_id
		 WHERE i.deleted_at IS NULL
		   AND ($1::uuid IS NULL OR i.client_id = $1)
		   AND ($2::text IS NULL OR i.status::text = $2)
		   AND (NOT $3 OR (i.status IN ('issued', 'partially_paid')))
		 ORDER BY i.issue_date DESC NULLS FIRST, i.created_at DESC
		 LIMIT $4 OFFSET $5`,
		f.ClientID, status, f.OutstandingOnly, f.Limit, f.Offset)
	if err != nil {
		return nil, errs.Internal(err, "list invoices")
	}
	defer rows.Close()

	var out []Invoice
	for rows.Next() {
		inv, err := s.scanInvoice(rows)
		if err != nil {
			return nil, errs.Internal(err, "scan invoice")
		}
		out = append(out, inv)
	}
	if err := rows.Err(); err != nil {
		return nil, errs.Internal(err, "read invoices")
	}

	// Lines are loaded per invoice only when a single invoice is fetched; a
	// listing shows totals, so it does not pay for them.
	return out, nil
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
