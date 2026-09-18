package crm

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/NewMux/mtdrb_go/internal/db"
	"github.com/NewMux/mtdrb_go/internal/platform/errs"
	"github.com/NewMux/mtdrb_go/internal/platform/ids"
)

// CreateTag adds a grouping label such as VIP, Semi-Private or Rehab.
func (s *Service) CreateTag(ctx context.Context, tx pgx.Tx, tenantID ids.ID, name string, colour *string) (Tag, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Tag{}, errs.Invalid(errs.CodeValidation, "a tag needs a name").
			WithField("name", "is required")
	}
	if len(name) > 60 {
		return Tag{}, errs.Invalid(errs.CodeValidation, "tag name is too long").
			WithField("name", "must be at most 60 characters")
	}

	tag := Tag{ID: ids.New(), Name: name, Colour: trimPtr(colour)}
	if _, err := tx.Exec(ctx,
		`INSERT INTO tags (id, tenant_id, name, colour) VALUES ($1, $2, $3, $4)`,
		tag.ID, tenantID, tag.Name, tag.Colour); err != nil {
		if db.IsUniqueViolation(err, "tags_tenant_name_key") {
			return Tag{}, errs.Conflict("tag_exists", "a tag named %q already exists", name)
		}
		return Tag{}, errs.Internal(err, "create tag")
	}
	return tag, nil
}

// ListTags returns the tenant's tags in name order.
func (s *Service) ListTags(ctx context.Context, tx pgx.Tx) ([]Tag, error) {
	rows, err := tx.Query(ctx, `SELECT id, name, colour FROM tags ORDER BY name`)
	if err != nil {
		return nil, errs.Internal(err, "list tags")
	}
	defer rows.Close()

	var out []Tag
	for rows.Next() {
		var t Tag
		if err := rows.Scan(&t.ID, &t.Name, &t.Colour); err != nil {
			return nil, errs.Internal(err, "scan tag")
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, errs.Internal(err, "read tags")
	}
	return out, nil
}

// DeleteTag removes a tag and its assignments.
func (s *Service) DeleteTag(ctx context.Context, tx pgx.Tx, tagID ids.ID) error {
	tag, err := tx.Exec(ctx, `DELETE FROM tags WHERE id = $1`, tagID)
	if err != nil {
		return errs.Internal(err, "delete tag")
	}
	if tag.RowsAffected() == 0 {
		return errs.NotFound("tag")
	}
	return nil
}

// SetTags replaces a client's tags with exactly the given set.
//
// Replacing rather than merging keeps the operation idempotent, which matters
// for the offline outbox: replaying the same assignment must not accumulate
// duplicates or resurrect a tag the trainer removed while offline.
func (s *Service) SetTags(ctx context.Context, tx pgx.Tx, tenantID, clientID ids.ID, tagIDs []ids.ID) error {
	if _, err := tx.Exec(ctx,
		`DELETE FROM client_tags WHERE client_id = $1 AND tag_id <> ALL($2)`,
		clientID, tagIDs); err != nil {
		return errs.Internal(err, "clear client tags")
	}
	if len(tagIDs) == 0 {
		return nil
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO client_tags (tenant_id, client_id, tag_id)
		SELECT $1, $2, t.id FROM unnest($3::uuid[]) AS t(id)
		ON CONFLICT (client_id, tag_id) DO NOTHING`,
		tenantID, clientID, tagIDs); err != nil {
		if db.IsForeignKeyViolation(err) {
			// A tag id from another tenant is invisible under RLS, so the
			// foreign key is what catches it. Reported as a validation error
			// rather than a 500, because the request is at fault.
			return errs.Invalid(errs.CodeValidation, "one or more tags do not exist")
		}
		return errs.Internal(err, "assign client tags")
	}
	return nil
}

// tagsFor loads a single client's tags.
func (s *Service) tagsFor(ctx context.Context, tx pgx.Tx, clientID ids.ID) ([]Tag, error) {
	rows, err := tx.Query(ctx, `
		SELECT t.id, t.name, t.colour
		  FROM tags t
		  JOIN client_tags ct ON ct.tag_id = t.id
		 WHERE ct.client_id = $1
		 ORDER BY t.name`, clientID)
	if err != nil {
		return nil, errs.Internal(err, "load client tags")
	}
	defer rows.Close()

	var out []Tag
	for rows.Next() {
		var t Tag
		if err := rows.Scan(&t.ID, &t.Name, &t.Colour); err != nil {
			return nil, errs.Internal(err, "scan client tag")
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, errs.Internal(err, "read client tags")
	}
	return out, nil
}
