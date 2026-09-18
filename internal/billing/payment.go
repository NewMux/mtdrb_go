package billing

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/NewMux/mtdrb_go/internal/db"
	"github.com/NewMux/mtdrb_go/internal/ledger"
	"github.com/NewMux/mtdrb_go/internal/platform/errs"
	"github.com/NewMux/mtdrb_go/internal/platform/ids"
	"github.com/NewMux/mtdrb_go/internal/platform/money"
)

// Payment is the trainer's record that money arrived, settled off-platform.
type Payment struct {
	ID             ids.ID            `json:"id"`
	InvoiceID      ids.ID            `json:"invoice_id"`
	ClientID       ids.ID            `json:"client_id"`
	AmountMinor    int64             `json:"amount_minor"`
	Currency       string            `json:"currency"`
	Instrument     ledger.Instrument `json:"instrument"`
	ReceivedOn     time.Time         `json:"received_on"`
	Reference      string            `json:"reference"`
	Notes          string            `json:"notes"`
	JournalEntryID *ids.ID           `json:"journal_entry_id,omitempty"`
	ReversedBy     *ids.ID           `json:"reversed_by,omitempty"`
	ReversesID     *ids.ID           `json:"reverses_id,omitempty"`
	ServerSeq      int64             `json:"server_seq"`
}

// RecordPaymentInput describes money received against an invoice.
type RecordPaymentInput struct {
	InvoiceID  ids.ID
	Amount     money.Money
	Instrument ledger.Instrument
	ReceivedOn time.Time
	Reference  string
	Notes      string
	RecordedBy *ids.ID
}

// PaymentResult reports the payment and what it did to the invoice.
type PaymentResult struct {
	Payment Payment `json:"payment"`
	Invoice Invoice `json:"invoice"`
}

// RecordPayment records a settlement and posts it to the ledger.
//
//	DR Cash | Bank | Digital Wallet   by instrument
//	CR Accounts Receivable            the debt shrinks
//
// Revenue is deliberately untouched. Receiving money for a prepaid pack moves
// it between assets; it does not earn it. That happens session by session.
func (s *Service) RecordPayment(ctx context.Context, tx pgx.Tx, tenantID ids.ID, in RecordPaymentInput) (PaymentResult, error) {
	if !in.Instrument.Valid() {
		return PaymentResult{}, errs.Invalid(errs.CodeValidation,
			"unknown payment instrument %q", in.Instrument).
			WithField("instrument", "must be cash, bank_transfer, cheque or digital_wallet")
	}
	if in.Amount.Minor <= 0 {
		return PaymentResult{}, errs.Invalid(errs.CodeValidation, "a payment must be a positive amount").
			WithField("amount_minor", "must be greater than zero")
	}

	// Lock the invoice so two devices syncing the same payment serialise
	// rather than both seeing an unpaid balance.
	var status string
	var totalMinor, paidMinor int64
	var currency string
	var clientID ids.ID
	err := tx.QueryRow(ctx, `
		SELECT i.status::text, i.total_minor, i.currency, i.client_id,
		       coalesce((SELECT sum(p.amount_minor) FROM payments p WHERE p.invoice_id = i.id), 0)
		  FROM invoices i
		 WHERE i.id = $1 AND i.deleted_at IS NULL
		 FOR UPDATE OF i`, in.InvoiceID,
	).Scan(&status, &totalMinor, &currency, &clientID, &paidMinor)
	if err != nil {
		if db.IsNoRows(err) {
			return PaymentResult{}, errs.NotFound("invoice")
		}
		return PaymentResult{}, errs.Internal(err, "load invoice for payment")
	}

	if InvoiceStatus(status) != InvoiceIssued && InvoiceStatus(status) != InvoicePartiallyPaid {
		return PaymentResult{}, errs.Conflict(errs.CodeInvoiceNotPayable,
			"an invoice that is %s cannot take a payment", status).
			WithMeta("status", status)
	}
	if in.Amount.Currency != currency {
		return PaymentResult{}, errs.Invalid(errs.CodeValidation,
			"payment is in %s but the invoice is in %s", in.Amount.Currency, currency)
	}

	outstanding := totalMinor - paidMinor
	if in.Amount.Minor > outstanding {
		// Refused rather than absorbed. A negative receivable is not a real
		// position, and money received beyond an invoice is a new prepaid
		// pack, not an overpaid bill.
		return PaymentResult{}, errs.Unprocessable(errs.CodeOverpayment,
			"this payment of %s exceeds the %s still outstanding",
			in.Amount, money.New(outstanding, currency)).
			WithMeta("outstanding_minor", outstanding).
			WithMeta("amount_minor", in.Amount.Minor)
	}

	receivedOn := in.ReceivedOn
	if receivedOn.IsZero() {
		receivedOn = s.clock.Now()
	}
	receivedOn = truncateToDay(receivedOn)

	paymentID := ids.New()
	posted, err := s.ledger.PostPayment(ctx, tx, paymentID, in.Amount, in.Instrument,
		receivedOn, "payment received", in.RecordedBy)
	if err != nil {
		return PaymentResult{}, err
	}

	payment := Payment{
		ID: paymentID, InvoiceID: in.InvoiceID, ClientID: clientID,
		AmountMinor: in.Amount.Minor, Currency: currency, Instrument: in.Instrument,
		ReceivedOn: receivedOn, Reference: strings.TrimSpace(in.Reference),
		Notes: strings.TrimSpace(in.Notes), JournalEntryID: &posted.ID,
	}

	if err := tx.QueryRow(ctx, `
		INSERT INTO payments
			(id, tenant_id, invoice_id, client_id, amount_minor, currency, instrument,
			 received_on, reference, notes, journal_entry_id, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7::payment_instrument, $8, $9, $10, $11, $12)
		RETURNING server_seq`,
		payment.ID, tenantID, payment.InvoiceID, payment.ClientID, payment.AmountMinor,
		payment.Currency, string(payment.Instrument), payment.ReceivedOn, payment.Reference,
		payment.Notes, posted.ID, in.RecordedBy,
	).Scan(&payment.ServerSeq); err != nil {
		return PaymentResult{}, errs.Internal(err, "record payment")
	}

	// The status follows the arithmetic rather than being set by the caller,
	// so a partially paid invoice cannot be marked settled by mistake.
	nowPaid := paidMinor + in.Amount.Minor
	newStatus := InvoicePartiallyPaid
	if nowPaid >= totalMinor {
		newStatus = InvoiceSettled
	}
	if _, err := tx.Exec(ctx, `
		UPDATE invoices
		   SET status = $2::invoice_status,
		       settled_at = CASE WHEN $2 = 'settled' THEN now() ELSE settled_at END
		 WHERE id = $1`, in.InvoiceID, string(newStatus)); err != nil {
		return PaymentResult{}, errs.Internal(err, "update invoice status")
	}

	invoice, err := s.GetInvoice(ctx, tx, in.InvoiceID)
	if err != nil {
		return PaymentResult{}, err
	}
	return PaymentResult{Payment: payment, Invoice: invoice}, nil
}

// ReversePayment records a compensating payment for one entered in error.
//
// The original row is left alone — payments are append-only, like journal
// entries — so the trail shows a mistake and its correction rather than
// quietly showing neither.
func (s *Service) ReversePayment(ctx context.Context, tx pgx.Tx, tenantID, paymentID ids.ID, reason string, by *ids.ID) (PaymentResult, error) {
	var original Payment
	var instrument string
	err := tx.QueryRow(ctx, `
		SELECT id, invoice_id, client_id, amount_minor, currency, instrument::text,
		       received_on, journal_entry_id, reversed_by
		  FROM payments WHERE id = $1 FOR UPDATE`, paymentID,
	).Scan(&original.ID, &original.InvoiceID, &original.ClientID, &original.AmountMinor,
		&original.Currency, &instrument, &original.ReceivedOn, &original.JournalEntryID,
		&original.ReversedBy)
	if err != nil {
		if db.IsNoRows(err) {
			return PaymentResult{}, errs.NotFound("payment")
		}
		return PaymentResult{}, errs.Internal(err, "load payment")
	}
	if original.ReversedBy != nil {
		return PaymentResult{}, errs.Conflict(errs.CodeInvalidTransition,
			"this payment has already been reversed")
	}
	original.Instrument = ledger.Instrument(instrument)

	if original.JournalEntryID != nil {
		if _, err := s.ledger.Reverse(ctx, tx, *original.JournalEntryID,
			"payment reversed: "+reason, by); err != nil {
			return PaymentResult{}, err
		}
	}

	// The compensating row carries a negative-equivalent meaning through
	// reverses_id rather than a negative amount, because the amount column is
	// constrained positive and a negative payment is not a thing that happened.
	reversalID := ids.New()
	if _, err := tx.Exec(ctx, `
		INSERT INTO payments
			(id, tenant_id, invoice_id, client_id, amount_minor, currency, instrument,
			 received_on, reference, notes, reverses_id, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7::payment_instrument, $8, '', $9, $10, $11)`,
		reversalID, tenantID, original.InvoiceID, original.ClientID, original.AmountMinor,
		original.Currency, instrument, truncateToDay(s.clock.Now()), reason, original.ID, by,
	); err != nil {
		return PaymentResult{}, errs.Internal(err, "record reversing payment")
	}

	if _, err := tx.Exec(ctx,
		`UPDATE payments SET reversed_by = $2 WHERE id = $1`, original.ID, reversalID); err != nil {
		return PaymentResult{}, errs.Internal(err, "link payment reversal")
	}

	// Recompute the invoice's standing from what remains unreversed.
	if err := s.recomputeInvoiceStatus(ctx, tx, original.InvoiceID); err != nil {
		return PaymentResult{}, err
	}

	invoice, err := s.GetInvoice(ctx, tx, original.InvoiceID)
	if err != nil {
		return PaymentResult{}, err
	}
	return PaymentResult{Payment: original, Invoice: invoice}, nil
}

// recomputeInvoiceStatus derives status from payments actually standing.
func (s *Service) recomputeInvoiceStatus(ctx context.Context, tx pgx.Tx, invoiceID ids.ID) error {
	if _, err := tx.Exec(ctx, `
		UPDATE invoices i
		   SET status = CASE
		         WHEN paid.total >= i.total_minor THEN 'settled'::invoice_status
		         WHEN paid.total > 0 THEN 'partially_paid'::invoice_status
		         ELSE 'issued'::invoice_status END,
		       settled_at = CASE WHEN paid.total >= i.total_minor THEN coalesce(i.settled_at, now()) ELSE NULL END
		  FROM (SELECT coalesce(sum(amount_minor), 0) AS total
		          FROM payments
		         WHERE invoice_id = $1 AND reverses_id IS NULL AND reversed_by IS NULL) paid
		 WHERE i.id = $1 AND i.status <> 'void'`, invoiceID); err != nil {
		return errs.Internal(err, "recompute invoice status")
	}
	return nil
}

// PaymentsFor lists the payments recorded against an invoice.
func (s *Service) PaymentsFor(ctx context.Context, tx pgx.Tx, invoiceID ids.ID) ([]Payment, error) {
	rows, err := tx.Query(ctx, `
		SELECT id, invoice_id, client_id, amount_minor, currency, instrument::text,
		       received_on, reference, notes, journal_entry_id, reversed_by, reverses_id, server_seq
		  FROM payments WHERE invoice_id = $1 ORDER BY received_on, created_at`, invoiceID)
	if err != nil {
		return nil, errs.Internal(err, "list payments")
	}
	defer rows.Close()

	var out []Payment
	for rows.Next() {
		var p Payment
		var instrument string
		if err := rows.Scan(&p.ID, &p.InvoiceID, &p.ClientID, &p.AmountMinor, &p.Currency,
			&instrument, &p.ReceivedOn, &p.Reference, &p.Notes, &p.JournalEntryID,
			&p.ReversedBy, &p.ReversesID, &p.ServerSeq); err != nil {
			return nil, errs.Internal(err, "scan payment")
		}
		p.Instrument = ledger.Instrument(instrument)
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, errs.Internal(err, "read payments")
	}
	return out, nil
}

// AgingBucket is one band of the receivables report.
type AgingBucket struct {
	Label        string `json:"label"`
	FromDays     int    `json:"from_days"`
	ToDays       int    `json:"to_days,omitempty"`
	AmountMinor  int64  `json:"amount_minor"`
	InvoiceCount int    `json:"invoice_count"`
}

// Receivables is the PRD's outstanding-receivables report.
type Receivables struct {
	Currency         string        `json:"currency"`
	TotalMinor       int64         `json:"total_minor"`
	OverdueMinor     int64         `json:"overdue_minor"`
	NotYetDueMinor   int64         `json:"not_yet_due_minor"`
	Buckets          []AgingBucket `json:"buckets"`
	OutstandingCount int           `json:"outstanding_count"`
}

// ReceivablesReport totals what is owed, in the PRD's ageing bands.
//
// Computed from invoices and their payments rather than from a stored balance,
// so it cannot drift, and bucketed by how overdue each invoice is today rather
// than by a flag some job set overnight.
func (s *Service) ReceivablesReport(ctx context.Context, tx pgx.Tx, currency string) (Receivables, error) {
	today := truncateToDay(s.clock.Now())

	rows, err := tx.Query(ctx, `
		SELECT i.due_date, i.total_minor -
		       coalesce((SELECT sum(p.amount_minor) FROM payments p
		                  WHERE p.invoice_id = i.id
		                    AND p.reverses_id IS NULL AND p.reversed_by IS NULL), 0) AS balance
		  FROM invoices i
		 WHERE i.deleted_at IS NULL AND i.status IN ('issued', 'partially_paid')`)
	if err != nil {
		return Receivables{}, errs.Internal(err, "compute receivables")
	}
	defer rows.Close()

	report := Receivables{
		Currency: currency,
		Buckets: []AgingBucket{
			{Label: "1-30 days", FromDays: 1, ToDays: 30},
			{Label: "31-60 days", FromDays: 31, ToDays: 60},
			{Label: "60+ days", FromDays: 61},
		},
	}

	for rows.Next() {
		var dueDate *time.Time
		var balance int64
		if err := rows.Scan(&dueDate, &balance); err != nil {
			return Receivables{}, errs.Internal(err, "scan receivable")
		}
		if balance <= 0 {
			continue
		}
		report.TotalMinor += balance
		report.OutstandingCount++

		if dueDate == nil || !dueDate.Before(today) {
			report.NotYetDueMinor += balance
			continue
		}
		report.OverdueMinor += balance

		days := int(today.Sub(*dueDate).Hours() / 24)
		switch {
		case days <= 30:
			report.Buckets[0].AmountMinor += balance
			report.Buckets[0].InvoiceCount++
		case days <= 60:
			report.Buckets[1].AmountMinor += balance
			report.Buckets[1].InvoiceCount++
		default:
			report.Buckets[2].AmountMinor += balance
			report.Buckets[2].InvoiceCount++
		}
	}
	if err := rows.Err(); err != nil {
		return Receivables{}, errs.Internal(err, "read receivables")
	}
	return report, nil
}
