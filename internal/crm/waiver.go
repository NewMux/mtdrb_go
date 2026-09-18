package crm

import (
	"context"
	"net/netip"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/NewMux/mtdrb_go/internal/db"
	"github.com/NewMux/mtdrb_go/internal/platform/errs"
	"github.com/NewMux/mtdrb_go/internal/platform/ids"
)

// Waiver is a liability release or training contract.
type Waiver struct {
	ID        ids.ID    `json:"id"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	Version   int       `json:"version"`
	IsActive  bool      `json:"is_active"`
	CreatedAt time.Time `json:"created_at"`
}

// Signature is a client's acceptance of a specific waiver text.
type Signature struct {
	ID         ids.ID    `json:"id"`
	WaiverID   ids.ID    `json:"waiver_id"`
	ClientID   ids.ID    `json:"client_id"`
	SignedName string    `json:"signed_name"`
	SignedAt   time.Time `json:"signed_at"`
	// MediaID points at the drawn signature image in private storage.
	MediaID *ids.ID `json:"signature_media_id,omitempty"`
}

// CreateWaiver adds a waiver document.
func (s *Service) CreateWaiver(ctx context.Context, tx pgx.Tx, tenantID ids.ID, title, body string) (Waiver, error) {
	title, body = strings.TrimSpace(title), strings.TrimSpace(body)
	if title == "" {
		return Waiver{}, errs.Invalid(errs.CodeValidation, "a waiver needs a title").
			WithField("title", "is required")
	}
	if body == "" {
		return Waiver{}, errs.Invalid(errs.CodeValidation, "a waiver needs a body").
			WithField("body", "is required")
	}

	w := Waiver{ID: ids.New(), Title: title, Body: body, Version: 1, IsActive: true}
	// The version is derived from what already exists under this title, so a
	// revision is a new row rather than an edit. A signature is only meaningful
	// against the exact wording agreed to, so previous versions must survive.
	err := tx.QueryRow(ctx, `
		INSERT INTO waivers (id, tenant_id, title, body, version, is_active)
		VALUES ($1, $2, $3, $4,
		        (SELECT coalesce(max(version), 0) + 1 FROM waivers
		          WHERE tenant_id = $2 AND lower(title) = lower($3)),
		        true)
		RETURNING version, created_at`,
		w.ID, tenantID, w.Title, w.Body).Scan(&w.Version, &w.CreatedAt)
	if err != nil {
		return Waiver{}, errs.Internal(err, "create waiver")
	}

	// Only the newest version of a title stays active for new signatures.
	if _, err := tx.Exec(ctx,
		`UPDATE waivers SET is_active = false
		  WHERE tenant_id = $1 AND lower(title) = lower($2) AND id <> $3`,
		tenantID, w.Title, w.ID); err != nil {
		return Waiver{}, errs.Internal(err, "retire previous waiver versions")
	}
	return w, nil
}

// ListWaivers returns the tenant's waivers, newest version of each first.
func (s *Service) ListWaivers(ctx context.Context, tx pgx.Tx, activeOnly bool) ([]Waiver, error) {
	rows, err := tx.Query(ctx, `
		SELECT id, title, body, version, is_active, created_at
		  FROM waivers
		 WHERE (NOT $1 OR is_active)
		 ORDER BY title, version DESC`, activeOnly)
	if err != nil {
		return nil, errs.Internal(err, "list waivers")
	}
	defer rows.Close()

	var out []Waiver
	for rows.Next() {
		var w Waiver
		if err := rows.Scan(&w.ID, &w.Title, &w.Body, &w.Version, &w.IsActive, &w.CreatedAt); err != nil {
			return nil, errs.Internal(err, "scan waiver")
		}
		out = append(out, w)
	}
	if err := rows.Err(); err != nil {
		return nil, errs.Internal(err, "read waivers")
	}
	return out, nil
}

// SignInput captures a signing event.
type SignInput struct {
	WaiverID   ids.ID
	ClientID   ids.ID
	SignedName string
	// MediaID is the uploaded signature image, if one was drawn.
	MediaID *ids.ID
	// IP is recorded for evidentiary weight. An unparseable value is dropped
	// rather than rejected: a missing address must not block a client from
	// signing a waiver on a gym floor.
	IP string
}

// Sign records a client's acceptance of a waiver.
func (s *Service) Sign(ctx context.Context, tx pgx.Tx, tenantID ids.ID, in SignInput) (Signature, error) {
	name := strings.TrimSpace(in.SignedName)
	if name == "" {
		return Signature{}, errs.Invalid(errs.CodeValidation, "a signature needs a name").
			WithField("signed_name", "is required")
	}

	var ip *string
	if addr, err := netip.ParseAddr(strings.TrimSpace(in.IP)); err == nil {
		v := addr.String()
		ip = &v
	}

	sig := Signature{
		ID: ids.New(), WaiverID: in.WaiverID, ClientID: in.ClientID,
		SignedName: name, MediaID: in.MediaID,
	}

	// signed_body snapshots the text as shown. Copying it rather than relying
	// on the waiver row is what makes the record hold up later: the waiver row
	// could be retired or its title reused, but the evidence of what this
	// person actually agreed to must not move.
	err := tx.QueryRow(ctx, `
		INSERT INTO waiver_signatures
			(id, tenant_id, waiver_id, client_id, signature_media_id, signed_body, signed_name, signed_ip)
		SELECT $1, $2, w.id, $4, $5, w.body, $6, $7::inet
		  FROM waivers w WHERE w.id = $3
		RETURNING signed_at`,
		sig.ID, tenantID, in.WaiverID, in.ClientID, in.MediaID, name, ip,
	).Scan(&sig.SignedAt)
	if err != nil {
		if db.IsNoRows(err) {
			return Signature{}, errs.NotFound("waiver")
		}
		if db.IsUniqueViolation(err, "waiver_signatures_unique") {
			return Signature{}, errs.Conflict("already_signed",
				"this client has already signed that waiver")
		}
		if db.IsForeignKeyViolation(err) {
			return Signature{}, errs.NotFound("client or waiver")
		}
		return Signature{}, errs.Internal(err, "record signature")
	}
	return sig, nil
}

// SignaturesFor returns a client's signed waivers.
func (s *Service) SignaturesFor(ctx context.Context, tx pgx.Tx, clientID ids.ID) ([]Signature, error) {
	rows, err := tx.Query(ctx, `
		SELECT id, waiver_id, client_id, signed_name, signed_at, signature_media_id
		  FROM waiver_signatures WHERE client_id = $1 ORDER BY signed_at DESC`, clientID)
	if err != nil {
		return nil, errs.Internal(err, "list signatures")
	}
	defer rows.Close()

	var out []Signature
	for rows.Next() {
		var sig Signature
		if err := rows.Scan(&sig.ID, &sig.WaiverID, &sig.ClientID, &sig.SignedName,
			&sig.SignedAt, &sig.MediaID); err != nil {
			return nil, errs.Internal(err, "scan signature")
		}
		out = append(out, sig)
	}
	if err := rows.Err(); err != nil {
		return nil, errs.Internal(err, "read signatures")
	}
	return out, nil
}
