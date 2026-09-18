package billing

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/NewMux/mtdrb_go/internal/db"
	"github.com/NewMux/mtdrb_go/internal/platform/errs"
	"github.com/NewMux/mtdrb_go/internal/platform/ids"
)

// A share link is how Journey B actually reaches the client: the trainer sends
// a URL over WhatsApp, and the page at the other end shows the amount and the
// bank details. It is the only unauthenticated surface in the product, so it
// is built the same way as a refresh token — an unguessable secret of which
// only the hash is stored, revocable, and returning a deliberately narrow
// payload.

const shareTokenBytes = 32

// ShareLink is a freshly minted link to an invoice.
type ShareLink struct {
	// URL is returned once, at creation. The plaintext token is never stored
	// and cannot be recovered afterwards; losing it means minting a new one.
	URL       string    `json:"url"`
	Token     string    `json:"token"`
	CreatedAt time.Time `json:"created_at"`
}

// hashShareToken derives the stored form of a share token.
//
// A plain SHA-256 is right here for the same reason it is for refresh tokens:
// the input is 32 bytes of cryptographic randomness, not a human-chosen
// secret, so there is no dictionary to slow down.
func hashShareToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

// CreateShareLink mints a shareable link for an issued invoice.
//
// Calling it again replaces the previous token, which is how a trainer revokes
// a link they sent to the wrong number.
func (s *Service) CreateShareLink(ctx context.Context, tx pgx.Tx, invoiceID ids.ID, baseURL string) (ShareLink, error) {
	var status string
	if err := tx.QueryRow(ctx,
		`SELECT status::text FROM invoices WHERE id = $1 AND deleted_at IS NULL`,
		invoiceID).Scan(&status); err != nil {
		if db.IsNoRows(err) {
			return ShareLink{}, errs.NotFound("invoice")
		}
		return ShareLink{}, errs.Internal(err, "load invoice")
	}
	// A draft has no number and no accounting behind it; sharing one would
	// show a client a bill that does not yet exist.
	if InvoiceStatus(status) == InvoiceDraft {
		return ShareLink{}, errs.Conflict(errs.CodeInvalidTransition,
			"a draft cannot be shared; issue it first")
	}
	if InvoiceStatus(status) == InvoiceVoid {
		return ShareLink{}, errs.Conflict(errs.CodeInvalidTransition,
			"a void invoice cannot be shared")
	}

	buf := make([]byte, shareTokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return ShareLink{}, errs.Internal(err, "generate share token")
	}
	token := base64.RawURLEncoding.EncodeToString(buf)

	var createdAt time.Time
	if err := tx.QueryRow(ctx, `
		UPDATE invoices
		   SET share_token_hash = $2, share_created_at = now(), share_revoked_at = NULL
		 WHERE id = $1
		RETURNING share_created_at`,
		invoiceID, hashShareToken(token)).Scan(&createdAt); err != nil {
		return ShareLink{}, errs.Internal(err, "store share token")
	}

	return ShareLink{
		URL:       strings.TrimRight(baseURL, "/") + "/public/invoices/" + token,
		Token:     token,
		CreatedAt: createdAt,
	}, nil
}

// RevokeShareLink disables an invoice's link without issuing a new one.
func (s *Service) RevokeShareLink(ctx context.Context, tx pgx.Tx, invoiceID ids.ID) error {
	tag, err := tx.Exec(ctx,
		`UPDATE invoices SET share_revoked_at = now()
		  WHERE id = $1 AND share_token_hash IS NOT NULL AND share_revoked_at IS NULL`, invoiceID)
	if err != nil {
		return errs.Internal(err, "revoke share link")
	}
	if tag.RowsAffected() == 0 {
		return errs.NotFound("active share link")
	}
	return nil
}

// PublicInvoice is what an unauthenticated visitor is allowed to see.
//
// Deliberately narrow, and assembled field by field rather than by reusing the
// internal Invoice type: a client following a link must see their bill and how
// to pay it, and nothing else — no other invoices, no medical notes, no view
// of the trainer's finances. Reusing the richer struct would mean any field
// added to it later leaks here by default.
type PublicInvoice struct {
	Number       string               `json:"number"`
	BusinessName string               `json:"business_name"`
	ClientName   string               `json:"client_name"`
	Status       InvoiceStatus        `json:"status"`
	Currency     string               `json:"currency"`
	TotalMinor   int64                `json:"total_minor"`
	PaidMinor    int64                `json:"paid_minor"`
	BalanceMinor int64                `json:"balance_minor"`
	IssueDate    *time.Time           `json:"issue_date,omitempty"`
	DueDate      *time.Time           `json:"due_date,omitempty"`
	IsOverdue    bool                 `json:"is_overdue"`
	Notes        string               `json:"notes"`
	Lines        []PublicInvoiceLine  `json:"lines"`
	Instructions *PaymentInstructions `json:"payment_instructions,omitempty"`
}

// PublicInvoiceLine is one billed item as shown to the client.
type PublicInvoiceLine struct {
	Description    string `json:"description"`
	Quantity       int    `json:"quantity"`
	UnitPriceMinor int64  `json:"unit_price_minor"`
	AmountMinor    int64  `json:"amount_minor"`
}

// ResolveShareToken finds the invoice and tenant a token belongs to.
//
// Reading by token has the same bootstrapping problem as login: the caller is
// unauthenticated, so there is no tenant context, but row-level security needs
// one. It gets the same answer — a narrow SECURITY DEFINER function returning
// identifiers only, keyed on a secret the caller must already hold. The caller
// then binds that tenant and reads the invoice through the ordinary policies,
// so nothing is widened beyond the lookup itself.
//
// This runs on the raw pool because no tenant is known yet.
func (s *Service) ResolveShareToken(ctx context.Context, pool *db.Pool, token string) (invoiceID, tenantID ids.ID, err error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return ids.Nil, ids.Nil, errs.NotFound("invoice")
	}
	err = pool.Raw().QueryRow(ctx,
		`SELECT invoice_id, tenant_id FROM invoice_lookup_by_share_token($1)`,
		hashShareToken(token)).Scan(&invoiceID, &tenantID)
	if err != nil {
		if db.IsNoRows(err) {
			// A revoked, forged or unknown token is a 404, never a 403: the
			// difference would confirm to a guesser that a token once existed.
			return ids.Nil, ids.Nil, errs.NotFound("invoice")
		}
		return ids.Nil, ids.Nil, errs.Internal(err, "resolve share token")
	}
	return invoiceID, tenantID, nil
}

// PublicView assembles the client-facing view of a shared invoice.
func (s *Service) PublicView(ctx context.Context, tx pgx.Tx, invoiceID ids.ID) (PublicInvoice, error) {
	invoice, err := s.GetInvoice(ctx, tx, invoiceID)
	if err != nil {
		return PublicInvoice{}, err
	}

	var businessName string
	if err := tx.QueryRow(ctx,
		`SELECT name FROM tenants WHERE id = current_tenant_id()`).Scan(&businessName); err != nil {
		return PublicInvoice{}, errs.Internal(err, "load business name")
	}

	public := PublicInvoice{
		Number:       derefString(invoice.Number),
		BusinessName: businessName,
		ClientName:   invoice.ClientName,
		Status:       invoice.Status,
		Currency:     invoice.Currency,
		TotalMinor:   invoice.TotalMinor,
		PaidMinor:    invoice.PaidMinor,
		BalanceMinor: invoice.BalanceMinor,
		IssueDate:    invoice.IssueDate,
		DueDate:      invoice.DueDate,
		IsOverdue:    invoice.IsOverdue,
		Notes:        invoice.Notes,
		Instructions: invoice.PaymentInstructions,
	}

	// The reference is the invoice number, so the trainer can match an
	// incoming transfer to the bill it settles.
	if public.Instructions != nil && public.Instructions.Reference == "" {
		public.Instructions.Reference = public.Number
	}

	for _, l := range invoice.Lines {
		public.Lines = append(public.Lines, PublicInvoiceLine{
			Description:    l.Description,
			Quantity:       l.Quantity,
			UnitPriceMinor: l.UnitPriceMinor,
			AmountMinor:    l.AmountMinor,
		})
	}
	return public, nil
}
