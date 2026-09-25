package catalog

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/NewMux/mtdrb_go/internal/db"
	"github.com/NewMux/mtdrb_go/internal/platform/errs"
	"github.com/NewMux/mtdrb_go/internal/platform/ids"
)

// OfferKind is what the offer sells.
type OfferKind string

const (
	OfferSessionPack     OfferKind = "session_pack"
	OfferMonthlyCoaching OfferKind = "monthly_coaching"
	OfferOnlineCoaching  OfferKind = "online_coaching"
	OfferSemiPrivate     OfferKind = "semi_private"
)

// Valid reports whether k is a known kind.
func (k OfferKind) Valid() bool {
	switch k {
	case OfferSessionPack, OfferMonthlyCoaching, OfferOnlineCoaching, OfferSemiPrivate:
		return true
	}
	return false
}

// Cycle is how often an offer is sold again to the same client.
type Cycle string

const (
	CycleOneOff  Cycle = "one_off"
	CycleWeekly  Cycle = "weekly"
	CycleMonthly Cycle = "monthly"
	CycleAnnual  Cycle = "annual"
)

// Valid reports whether c is a known cycle.
func (c Cycle) Valid() bool {
	switch c {
	case CycleOneOff, CycleWeekly, CycleMonthly, CycleAnnual:
		return true
	}
	return false
}

// Offer is a line on the practice's price list.
type Offer struct {
	ID               ids.ID     `json:"id"`
	Name             string     `json:"name"`
	Description      string     `json:"description"`
	Kind             OfferKind  `json:"kind"`
	Credits          *int       `json:"credits"`
	PriceMinor       int64      `json:"price_minor"`
	Currency         string     `json:"currency"`
	PriceIncludesVAT bool       `json:"price_includes_vat"`
	ValidityDays     *int       `json:"validity_days"`
	Cycle            Cycle      `json:"cycle"`
	SessionTypeID    *ids.ID    `json:"session_type_id"`
	SortOrder        int        `json:"sort_order"`
	ArchivedAt       *time.Time `json:"archived_at"`
}

// OfferInput creates or changes an offer. On update a nil field is left
// alone; ClearCredits, ClearValidity and ClearSessionType set those to none.
type OfferInput struct {
	Name             *string    `json:"name"`
	Description      *string    `json:"description"`
	Kind             *OfferKind `json:"kind"`
	Credits          *int       `json:"credits"`
	ClearCredits     bool       `json:"clear_credits"`
	PriceMinor       *int64     `json:"price_minor"`
	PriceIncludesVAT *bool      `json:"price_includes_vat"`
	ValidityDays     *int       `json:"validity_days"`
	ClearValidity    bool       `json:"clear_validity"`
	Cycle            *Cycle     `json:"cycle"`
	SessionTypeID    *ids.ID    `json:"session_type_id"`
	ClearSessionType bool       `json:"clear_session_type"`
	SortOrder        *int       `json:"sort_order"`
}

func invalid(field, msg string) error {
	return errs.Invalid(errs.CodeValidation, "%s %s", strings.ReplaceAll(field, "_", " "), msg).WithField(field, msg)
}

// merge applies an input to an offer and checks the result as a whole: a
// pack needs a number of sessions whichever of the two changed.
func (in OfferInput) merge(o Offer) (Offer, error) {
	if in.Name != nil {
		if strings.TrimSpace(*in.Name) == "" {
			return Offer{}, invalid("name", "is required")
		}
		o.Name = strings.TrimSpace(*in.Name)
	}
	if in.Description != nil {
		o.Description = strings.TrimSpace(*in.Description)
	}
	if in.Kind != nil {
		if !in.Kind.Valid() {
			return Offer{}, invalid("kind", "must be session_pack, monthly_coaching, online_coaching or semi_private")
		}
		o.Kind = *in.Kind
	}
	switch {
	case in.ClearCredits:
		o.Credits = nil
	case in.Credits != nil:
		if *in.Credits <= 0 || *in.Credits > 1000 {
			return Offer{}, invalid("credits", "must be 1 to 1000")
		}
		o.Credits = in.Credits
	}
	if in.PriceMinor != nil {
		if *in.PriceMinor < 0 {
			return Offer{}, invalid("price_minor", "cannot be negative")
		}
		o.PriceMinor = *in.PriceMinor
	}
	if in.PriceIncludesVAT != nil {
		o.PriceIncludesVAT = *in.PriceIncludesVAT
	}
	switch {
	case in.ClearValidity:
		o.ValidityDays = nil
	case in.ValidityDays != nil:
		if *in.ValidityDays <= 0 || *in.ValidityDays > 3650 {
			return Offer{}, invalid("validity_days", "must be 1 to 3650")
		}
		o.ValidityDays = in.ValidityDays
	}
	if in.Cycle != nil {
		if !in.Cycle.Valid() {
			return Offer{}, invalid("cycle", "must be one_off, weekly, monthly or annual")
		}
		o.Cycle = *in.Cycle
	}
	switch {
	case in.ClearSessionType:
		o.SessionTypeID = nil
	case in.SessionTypeID != nil:
		o.SessionTypeID = in.SessionTypeID
	}
	if in.SortOrder != nil {
		o.SortOrder = *in.SortOrder
	}
	if o.Kind == OfferSessionPack && o.Credits == nil {
		return Offer{}, invalid("credits", "a session pack needs a number of sessions")
	}
	return o, nil
}

// CreateOffer adds an offer, priced in the practice's currency.
func CreateOffer(ctx context.Context, tx pgx.Tx, tenantID ids.ID, in OfferInput) (Offer, error) {
	if in.Name == nil {
		return Offer{}, invalid("name", "is required")
	}
	if in.PriceMinor == nil {
		return Offer{}, invalid("price_minor", "is required")
	}
	o, err := in.merge(Offer{Kind: OfferSessionPack, Cycle: CycleOneOff, PriceIncludesVAT: true})
	if err != nil {
		return Offer{}, err
	}
	// The ledger keeps one currency per practice; an offer in another could
	// not be sold.
	if err := tx.QueryRow(ctx, `SELECT default_currency FROM tenants WHERE id = $1`, tenantID).Scan(&o.Currency); err != nil {
		return Offer{}, errs.Internal(err, "read currency")
	}
	o.ID = ids.New()
	if _, err := tx.Exec(ctx, `
		INSERT INTO package_offers
			(id, tenant_id, name, description, kind, credits, price_minor, currency, price_includes_vat,
			 validity_days, cycle, session_type_id, sort_order)
		VALUES ($1, $2, $3, $4, $5::offer_kind, $6, $7, $8, $9, $10, $11::billing_cycle, $12, $13)`,
		o.ID, tenantID, o.Name, o.Description, string(o.Kind), o.Credits, o.PriceMinor, o.Currency,
		o.PriceIncludesVAT, o.ValidityDays, string(o.Cycle), o.SessionTypeID, o.SortOrder); err != nil {
		return Offer{}, offerErr(err)
	}
	return GetOffer(ctx, tx, o.ID)
}

// UpdateOffer changes an offer. What was already sold keeps the terms it was
// sold on: an invoice line copies them.
func UpdateOffer(ctx context.Context, tx pgx.Tx, id ids.ID, in OfferInput) (Offer, error) {
	current, err := GetOffer(ctx, tx, id)
	if err != nil {
		return Offer{}, err
	}
	o, err := in.merge(current)
	if err != nil {
		return Offer{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE package_offers SET
			name = $2, description = $3, kind = $4::offer_kind, credits = $5, price_minor = $6,
			price_includes_vat = $7, validity_days = $8, cycle = $9::billing_cycle,
			session_type_id = $10, sort_order = $11
		 WHERE id = $1`,
		id, o.Name, o.Description, string(o.Kind), o.Credits, o.PriceMinor, o.PriceIncludesVAT,
		o.ValidityDays, string(o.Cycle), o.SessionTypeID, o.SortOrder); err != nil {
		return Offer{}, offerErr(err)
	}
	return GetOffer(ctx, tx, id)
}

// SetOfferArchived retires an offer from the sell screen, or brings it back.
func SetOfferArchived(ctx context.Context, tx pgx.Tx, id ids.ID, archived bool, now time.Time) (Offer, error) {
	var at *time.Time
	if archived {
		at = &now
	}
	tag, err := tx.Exec(ctx,
		`UPDATE package_offers SET archived_at = CASE WHEN $2::timestamptz IS NULL THEN NULL
		                                             ELSE COALESCE(archived_at, $2) END
		  WHERE id = $1`, id, at)
	if err != nil {
		return Offer{}, errs.Internal(err, "archive offer")
	}
	if tag.RowsAffected() == 0 {
		return Offer{}, errs.NotFound("offer")
	}
	return GetOffer(ctx, tx, id)
}

func offerErr(err error) error {
	if db.IsForeignKeyViolation(err) {
		return errs.NotFound("session type")
	}
	return errs.Internal(err, "save offer")
}

const offerColumns = `id, name, description, kind::text, credits, price_minor, currency, price_includes_vat,
	validity_days, cycle::text, session_type_id, sort_order, archived_at`

func scanOffer(row pgx.Row) (Offer, error) {
	var o Offer
	var kind, cycle string
	if err := row.Scan(&o.ID, &o.Name, &o.Description, &kind, &o.Credits, &o.PriceMinor, &o.Currency,
		&o.PriceIncludesVAT, &o.ValidityDays, &cycle, &o.SessionTypeID, &o.SortOrder, &o.ArchivedAt); err != nil {
		return Offer{}, err
	}
	o.Kind, o.Cycle = OfferKind(kind), Cycle(cycle)
	return o, nil
}

// GetOffer reads one offer.
func GetOffer(ctx context.Context, tx pgx.Tx, id ids.ID) (Offer, error) {
	o, err := scanOffer(tx.QueryRow(ctx, `SELECT `+offerColumns+` FROM package_offers WHERE id = $1`, id))
	if err != nil {
		if db.IsNoRows(err) {
			return Offer{}, errs.NotFound("offer")
		}
		return Offer{}, errs.Internal(err, "read offer")
	}
	return o, nil
}

// ListOffers returns the price list in the trainer's order, archived last.
func ListOffers(ctx context.Context, tx pgx.Tx, includeArchived bool) ([]Offer, error) {
	rows, err := tx.Query(ctx, `SELECT `+offerColumns+` FROM package_offers
		 WHERE $1 OR archived_at IS NULL
		 ORDER BY archived_at IS NOT NULL, sort_order, price_minor, lower(name)`, includeArchived)
	if err != nil {
		return nil, errs.Internal(err, "list offers")
	}
	defer rows.Close()
	out := []Offer{}
	for rows.Next() {
		o, err := scanOffer(rows)
		if err != nil {
			return nil, errs.Internal(err, "read offer")
		}
		out = append(out, o)
	}
	return out, rows.Err()
}
