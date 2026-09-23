// Package catalog holds what a practice offers and where: its locations and
// its price list of package offers.
//
// Both are small, changed rarely, and read everywhere — the calendar colours
// sessions by location and the sell screen starts from an offer — so both are
// mirrored to devices and changed online by the trainer. Neither is ever
// deleted: an archived location still names where last year's sessions were,
// and an archived offer still explains the invoices that sold it.
package catalog

import (
	"context"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/NewMux/mtdrb_go/internal/db"
	"github.com/NewMux/mtdrb_go/internal/platform/errs"
	"github.com/NewMux/mtdrb_go/internal/platform/ids"
	"github.com/NewMux/mtdrb_go/internal/subscription"
)

// LocationKind is the sort of place.
type LocationKind string

const (
	KindStudio     LocationKind = "studio"
	KindGym        LocationKind = "gym"
	KindOutdoor    LocationKind = "outdoor"
	KindClientHome LocationKind = "client_home"
	KindOnline     LocationKind = "online"
)

// Valid reports whether k is a known kind.
func (k LocationKind) Valid() bool {
	switch k {
	case KindStudio, KindGym, KindOutdoor, KindClientHome, KindOnline:
		return true
	}
	return false
}

// Location is a place the trainer works.
type Location struct {
	ID         ids.ID       `json:"id"`
	Name       string       `json:"name"`
	Kind       LocationKind `json:"kind"`
	Address    string       `json:"address"`
	Region     string       `json:"region"`
	Colour     *string      `json:"colour"`
	IsPrimary  bool         `json:"is_primary"`
	ArchivedAt *time.Time   `json:"archived_at"`
}

// LocationInput creates or changes a location. On update a nil field is
// left alone.
type LocationInput struct {
	Name      *string       `json:"name"`
	Kind      *LocationKind `json:"kind"`
	Address   *string       `json:"address"`
	Region    *string       `json:"region"`
	Colour    *string       `json:"colour"`
	IsPrimary *bool         `json:"is_primary"`
}

// Locations count against a plan's limit while they are in use.
func init() {
	subscription.RegisterCounter(subscription.LimitLocations, func(ctx context.Context, tx pgx.Tx) (int, error) {
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM locations WHERE archived_at IS NULL`).Scan(&n); err != nil {
			return 0, errs.Internal(err, "count locations")
		}
		return n, nil
	})
}

var hexColour = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

func (in LocationInput) validate() error {
	if in.Name != nil && strings.TrimSpace(*in.Name) == "" {
		return errs.Invalid(errs.CodeValidation, "a location needs a name").WithField("name", "is required")
	}
	if in.Kind != nil && !in.Kind.Valid() {
		return errs.Invalid(errs.CodeValidation, "unknown kind %q", *in.Kind).
			WithField("kind", "must be studio, gym, outdoor, client_home or online")
	}
	if in.Colour != nil && *in.Colour != "" && !hexColour.MatchString(*in.Colour) {
		return errs.Invalid(errs.CodeValidation, "colour must be #rrggbb").WithField("colour", "must be #rrggbb")
	}
	return nil
}

func locationErr(err error) error {
	if db.IsUniqueViolation(err, "locations_name_key") {
		return errs.Conflict("location_exists", "there is already a location with that name").
			WithField("name", "is already in use")
	}
	return errs.Internal(err, "save location")
}

// CreateLocation adds a place. The first one a practice adds is its primary.
func CreateLocation(ctx context.Context, tx pgx.Tx, tenantID ids.ID, in LocationInput) (Location, error) {
	if in.Name == nil {
		return Location{}, errs.Invalid(errs.CodeValidation, "a location needs a name").WithField("name", "is required")
	}
	if err := in.validate(); err != nil {
		return Location{}, err
	}
	if err := subscription.CheckRoom(ctx, tx, subscription.LimitLocations); err != nil {
		return Location{}, err
	}
	kind := KindStudio
	if in.Kind != nil {
		kind = *in.Kind
	}
	var others int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM locations WHERE archived_at IS NULL`).Scan(&others); err != nil {
		return Location{}, errs.Internal(err, "count locations")
	}
	primary := others == 0 || (in.IsPrimary != nil && *in.IsPrimary)
	if primary {
		if err := clearPrimary(ctx, tx); err != nil {
			return Location{}, err
		}
	}
	id := ids.New()
	if _, err := tx.Exec(ctx, `
		INSERT INTO locations (id, tenant_id, name, kind, address, region, colour, is_primary)
		VALUES ($1, $2, $3, $4::location_kind, $5, $6, $7, $8)`,
		id, tenantID, strings.TrimSpace(*in.Name), string(kind), orEmpty(in.Address), orEmpty(in.Region),
		emptyToNil(in.Colour), primary); err != nil {
		return Location{}, locationErr(err)
	}
	return GetLocation(ctx, tx, id)
}

func clearPrimary(ctx context.Context, tx pgx.Tx) error {
	if _, err := tx.Exec(ctx, `UPDATE locations SET is_primary = false WHERE is_primary`); err != nil {
		return errs.Internal(err, "clear primary location")
	}
	return nil
}

// UpdateLocation changes a place.
func UpdateLocation(ctx context.Context, tx pgx.Tx, id ids.ID, in LocationInput) (Location, error) {
	if err := in.validate(); err != nil {
		return Location{}, err
	}
	current, err := GetLocation(ctx, tx, id)
	if err != nil {
		return Location{}, err
	}
	if in.IsPrimary != nil && *in.IsPrimary && !current.IsPrimary {
		if current.ArchivedAt != nil {
			return Location{}, errs.Conflict(errs.CodeInvalidTransition, "an archived location cannot be the primary one")
		}
		if err := clearPrimary(ctx, tx); err != nil {
			return Location{}, err
		}
	}
	var kind *string
	if in.Kind != nil {
		k := string(*in.Kind)
		kind = &k
	}
	var colourSet bool
	if in.Colour != nil {
		colourSet = true
	}
	if _, err := tx.Exec(ctx, `
		UPDATE locations SET
			name       = COALESCE($2, name),
			kind       = COALESCE($3::location_kind, kind),
			address    = COALESCE($4, address),
			region     = COALESCE($5, region),
			colour     = CASE WHEN $6 THEN $7 ELSE colour END,
			is_primary = CASE WHEN $8::boolean IS TRUE THEN true ELSE is_primary END
		 WHERE id = $1`,
		id, trim(in.Name), kind, trim(in.Address), trim(in.Region), colourSet, emptyToNil(in.Colour),
		in.IsPrimary); err != nil {
		return Location{}, locationErr(err)
	}
	return GetLocation(ctx, tx, id)
}

// ArchiveLocation retires a place. Its sessions keep pointing at it. The
// primary cannot be archived while another place could take over; the
// trainer picks which.
func ArchiveLocation(ctx context.Context, tx pgx.Tx, id ids.ID, now time.Time) (Location, error) {
	current, err := GetLocation(ctx, tx, id)
	if err != nil {
		return Location{}, err
	}
	if current.ArchivedAt != nil {
		return current, nil
	}
	if current.IsPrimary {
		var others int
		if err := tx.QueryRow(ctx,
			`SELECT count(*) FROM locations WHERE archived_at IS NULL AND id <> $1`, id).Scan(&others); err != nil {
			return Location{}, errs.Internal(err, "count locations")
		}
		if others > 0 {
			return Location{}, errs.Conflict(errs.CodeInvalidTransition,
				"choose another primary location before archiving this one")
		}
	}
	if _, err := tx.Exec(ctx,
		`UPDATE locations SET archived_at = $2, is_primary = false WHERE id = $1`, id, now); err != nil {
		return Location{}, errs.Internal(err, "archive location")
	}
	return GetLocation(ctx, tx, id)
}

// RestoreLocation brings an archived place back, if the plan has room.
func RestoreLocation(ctx context.Context, tx pgx.Tx, id ids.ID) (Location, error) {
	current, err := GetLocation(ctx, tx, id)
	if err != nil {
		return Location{}, err
	}
	if current.ArchivedAt == nil {
		return current, nil
	}
	if err := subscription.CheckRoom(ctx, tx, subscription.LimitLocations); err != nil {
		return Location{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE locations SET archived_at = NULL WHERE id = $1`, id); err != nil {
		return Location{}, locationErr(err)
	}
	return GetLocation(ctx, tx, id)
}

const locationColumns = `id, name, kind::text, address, region, colour, is_primary, archived_at`

func scanLocation(row pgx.Row) (Location, error) {
	var l Location
	var kind string
	if err := row.Scan(&l.ID, &l.Name, &kind, &l.Address, &l.Region, &l.Colour, &l.IsPrimary, &l.ArchivedAt); err != nil {
		return Location{}, err
	}
	l.Kind = LocationKind(kind)
	return l, nil
}

// GetLocation reads one place.
func GetLocation(ctx context.Context, tx pgx.Tx, id ids.ID) (Location, error) {
	l, err := scanLocation(tx.QueryRow(ctx, `SELECT `+locationColumns+` FROM locations WHERE id = $1`, id))
	if err != nil {
		if db.IsNoRows(err) {
			return Location{}, errs.NotFound("location")
		}
		return Location{}, errs.Internal(err, "read location")
	}
	return l, nil
}

// ListLocations returns the practice's places, primary first, archived last.
func ListLocations(ctx context.Context, tx pgx.Tx, includeArchived bool) ([]Location, error) {
	rows, err := tx.Query(ctx, `SELECT `+locationColumns+` FROM locations
		 WHERE $1 OR archived_at IS NULL
		 ORDER BY archived_at IS NOT NULL, is_primary DESC, lower(name)`, includeArchived)
	if err != nil {
		return nil, errs.Internal(err, "list locations")
	}
	defer rows.Close()
	out := []Location{}
	for rows.Next() {
		l, err := scanLocation(rows)
		if err != nil {
			return nil, errs.Internal(err, "read location")
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// ResolveForBooking returns the name to snapshot on a session booked at a
// location, and refuses an archived or unknown one.
func ResolveForBooking(ctx context.Context, tx pgx.Tx, id ids.ID) (string, error) {
	l, err := GetLocation(ctx, tx, id)
	if err != nil {
		return "", err
	}
	if l.ArchivedAt != nil {
		return "", errs.Conflict(errs.CodeInvalidTransition, "%s is archived", l.Name).
			WithField("location_id", "is archived")
	}
	return l.Name, nil
}

func trim(s *string) *string {
	if s == nil {
		return nil
	}
	t := strings.TrimSpace(*s)
	return &t
}

func orEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return strings.TrimSpace(*s)
}

func emptyToNil(s *string) *string {
	if s == nil || strings.TrimSpace(*s) == "" {
		return nil
	}
	return s
}
