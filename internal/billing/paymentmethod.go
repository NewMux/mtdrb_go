package billing

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/NewMux/mtdrb_go/internal/db"
	"github.com/NewMux/mtdrb_go/internal/platform/errs"
	"github.com/NewMux/mtdrb_go/internal/platform/ids"
)

// CoachPulse never touches a card. A payment method here is a set of
// instructions a client reads and acts on in their own banking app — an IBAN,
// a wallet handle, or "cash at the next session".

// MethodKind classifies how a trainer gets paid.
type MethodKind string

const (
	MethodBankTransfer  MethodKind = "bank_transfer"
	MethodDigitalWallet MethodKind = "digital_wallet"
	MethodCash          MethodKind = "cash"
	MethodCheque        MethodKind = "cheque"
	MethodOther         MethodKind = "other"
)

// Valid reports whether the kind is recognised.
func (k MethodKind) Valid() bool {
	switch k {
	case MethodBankTransfer, MethodDigitalWallet, MethodCash, MethodCheque, MethodOther:
		return true
	default:
		return false
	}
}

// MethodDetails holds the structured settlement fields.
//
// Which of these matter varies by country and by app, which is why they are
// stored as jsonb rather than as columns: a trainer taking a new local wallet
// should not require a migration.
type MethodDetails struct {
	AccountHolder string `json:"account_holder,omitempty"`
	BankName      string `json:"bank_name,omitempty"`
	IBAN          string `json:"iban,omitempty"`
	SwiftBIC      string `json:"swift_bic,omitempty"`
	AccountNumber string `json:"account_number,omitempty"`
	RoutingNumber string `json:"routing_number,omitempty"`
	// Handle covers the P2P case: a Revolut tag, a PayPal.me link, a phone
	// number for an instant-transfer app.
	Handle string `json:"handle,omitempty"`
}

// PaymentMethod is one way this trainer accepts settlement.
type PaymentMethod struct {
	ID           ids.ID        `json:"id"`
	Kind         MethodKind    `json:"kind"`
	Label        string        `json:"label"`
	Details      MethodDetails `json:"details"`
	Instructions string        `json:"instructions"`
	IsDefault    bool          `json:"is_default"`
	SortOrder    int           `json:"sort_order"`
}

// PaymentInstructions is what gets snapshotted onto an invoice and rendered on
// the share page.
//
// It is a copy, not a reference: a trainer who changes their IBAN in June must
// not retroactively change what a March invoice told a client to pay.
type PaymentInstructions struct {
	Methods []PaymentMethod `json:"methods"`
	// Reference is what the client should put on the transfer so the trainer
	// can match it up — the invoice number.
	Reference string `json:"reference,omitempty"`
	Note      string `json:"note,omitempty"`
}

// CreateMethodInput describes a settlement method to add.
type CreateMethodInput struct {
	Kind         MethodKind
	Label        string
	Details      MethodDetails
	Instructions string
	IsDefault    bool
	SortOrder    int
}

// CreatePaymentMethod adds a way for this trainer to be paid.
func (s *Service) CreatePaymentMethod(ctx context.Context, tx pgx.Tx, tenantID ids.ID, in CreateMethodInput) (PaymentMethod, error) {
	label := strings.TrimSpace(in.Label)
	if label == "" {
		return PaymentMethod{}, errs.Invalid(errs.CodeValidation, "a payment method needs a label").
			WithField("label", "is required")
	}
	if !in.Kind.Valid() {
		return PaymentMethod{}, errs.Invalid(errs.CodeValidation,
			"unknown payment method kind %q", in.Kind).
			WithField("kind", "must be bank_transfer, digital_wallet, cash, cheque or other")
	}

	m := PaymentMethod{
		ID: ids.New(), Kind: in.Kind, Label: label,
		Details: in.Details, Instructions: strings.TrimSpace(in.Instructions),
		IsDefault: in.IsDefault, SortOrder: in.SortOrder,
	}

	// Only one default may exist, enforced by a partial unique index. Clearing
	// the old one first turns a constraint violation into the behaviour the
	// trainer expects.
	if m.IsDefault {
		if _, err := tx.Exec(ctx,
			`UPDATE payment_methods SET is_default = false
			  WHERE tenant_id = $1 AND is_default AND archived_at IS NULL`, tenantID); err != nil {
			return PaymentMethod{}, errs.Internal(err, "clear previous default payment method")
		}
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO payment_methods (id, tenant_id, kind, label, details, instructions, is_default, sort_order)
		VALUES ($1, $2, $3::payment_method_kind, $4, $5, $6, $7, $8)`,
		m.ID, tenantID, string(m.Kind), m.Label, m.Details, m.Instructions, m.IsDefault, m.SortOrder,
	); err != nil {
		return PaymentMethod{}, errs.Internal(err, "create payment method")
	}
	return m, nil
}

// ListPaymentMethods returns the trainer's active settlement methods.
func (s *Service) ListPaymentMethods(ctx context.Context, tx pgx.Tx) ([]PaymentMethod, error) {
	rows, err := tx.Query(ctx, `
		SELECT id, kind::text, label, details, instructions, is_default, sort_order
		  FROM payment_methods WHERE archived_at IS NULL
		 ORDER BY is_default DESC, sort_order, label`)
	if err != nil {
		return nil, errs.Internal(err, "list payment methods")
	}
	defer rows.Close()

	var out []PaymentMethod
	for rows.Next() {
		var m PaymentMethod
		var kind string
		if err := rows.Scan(&m.ID, &kind, &m.Label, &m.Details, &m.Instructions,
			&m.IsDefault, &m.SortOrder); err != nil {
			return nil, errs.Internal(err, "scan payment method")
		}
		m.Kind = MethodKind(kind)
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, errs.Internal(err, "read payment methods")
	}
	return out, nil
}

// ArchivePaymentMethod retires a settlement method.
//
// Archived rather than deleted: invoices already issued snapshot their own
// copy, but keeping the row means a trainer can see what they used to use.
func (s *Service) ArchivePaymentMethod(ctx context.Context, tx pgx.Tx, methodID ids.ID) error {
	tag, err := tx.Exec(ctx,
		`UPDATE payment_methods SET archived_at = now(), is_default = false
		  WHERE id = $1 AND archived_at IS NULL`, methodID)
	if err != nil {
		return errs.Internal(err, "archive payment method")
	}
	if tag.RowsAffected() == 0 {
		return errs.NotFound("payment method")
	}
	return nil
}

// instructionsFor gathers the settlement instructions to print on an invoice.
//
// With a specific method named, only that one is printed. Otherwise every
// active method is, because a trainer who accepts both a transfer and cash
// wants the client to be able to choose.
func (s *Service) instructionsFor(ctx context.Context, tx pgx.Tx, methodID *ids.ID) (PaymentInstructions, error) {
	if methodID != nil {
		var m PaymentMethod
		var kind string
		err := tx.QueryRow(ctx, `
			SELECT id, kind::text, label, details, instructions, is_default, sort_order
			  FROM payment_methods WHERE id = $1 AND archived_at IS NULL`, *methodID,
		).Scan(&m.ID, &kind, &m.Label, &m.Details, &m.Instructions, &m.IsDefault, &m.SortOrder)
		if err != nil {
			if db.IsNoRows(err) {
				return PaymentInstructions{}, errs.NotFound("payment method")
			}
			return PaymentInstructions{}, errs.Internal(err, "load payment method")
		}
		m.Kind = MethodKind(kind)
		return PaymentInstructions{Methods: []PaymentMethod{m}}, nil
	}

	methods, err := s.ListPaymentMethods(ctx, tx)
	if err != nil {
		return PaymentInstructions{}, err
	}
	return PaymentInstructions{Methods: methods}, nil
}
