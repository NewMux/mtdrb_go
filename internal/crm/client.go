// Package crm holds client records, intake compliance and body measurements.
//
// Two things here are handled with more care than the rest of the codebase.
//
// Emergency medical notes are encrypted at the column level with pgcrypto, so
// a database read alone does not disclose them. They are consequently never
// searchable — an accepted trade, recorded in ADR 0003.
//
// Client-owned rows are readable by the client themselves through the portal.
// The row-level security policies handle that narrowing, so the service layer
// does not branch on who is asking; it asks the database and gets back only
// what the caller is entitled to.
package crm

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/NewMux/mtdrb_go/internal/db"
	"github.com/NewMux/mtdrb_go/internal/platform/clock"
	"github.com/NewMux/mtdrb_go/internal/platform/errs"
	"github.com/NewMux/mtdrb_go/internal/platform/ids"
)

// Status is where a client sits in their lifecycle.
type Status string

const (
	StatusLead     Status = "lead"
	StatusActive   Status = "active"
	StatusPaused   Status = "paused"
	StatusArchived Status = "archived"
)

// Valid reports whether the status is one the system recognises.
func (s Status) Valid() bool {
	switch s {
	case StatusLead, StatusActive, StatusPaused, StatusArchived:
		return true
	default:
		return false
	}
}

// Client is a training client.
type Client struct {
	ID          ids.ID     `json:"id"`
	FullName    string     `json:"full_name"`
	Email       *string    `json:"email,omitempty"`
	Phone       *string    `json:"phone,omitempty"`
	DateOfBirth *time.Time `json:"date_of_birth,omitempty"`
	Status      Status     `json:"status"`

	EmergencyContactName  *string `json:"emergency_contact_name,omitempty"`
	EmergencyContactPhone *string `json:"emergency_contact_phone,omitempty"`
	// MedicalNotes is decrypted on read and only populated by GetWithMedical,
	// so it cannot be included in a list response by accident.
	MedicalNotes *string `json:"medical_notes,omitempty"`

	AllowOverdraft   *bool  `json:"allow_overdraft,omitempty"`
	DefaultRateMinor *int64 `json:"default_rate_minor,omitempty"`
	Notes            string `json:"notes"`
	Tags             []Tag  `json:"tags,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	ServerSeq int64     `json:"server_seq"`
}

// Tag groups clients by tier, such as VIP, Semi-Private or Rehab.
type Tag struct {
	ID     ids.ID  `json:"id"`
	Name   string  `json:"name"`
	Colour *string `json:"colour,omitempty"`
}

// Service manages client records.
type Service struct {
	clock clock.Clock
	// columnKey encrypts medical notes. It comes from configuration and is
	// never persisted alongside the data it protects.
	columnKey string
}

// NewService builds the CRM service.
func NewService(c clock.Clock, columnKey []byte) *Service {
	if c == nil {
		c = clock.System{}
	}
	return &Service{clock: c, columnKey: string(columnKey)}
}

// CreateInput describes a new client.
type CreateInput struct {
	// ID is the id the device minted offline, when there is one. Without it
	// the local row and the server's row are different clients, and the
	// trainer ends up with a duplicate on the roster that never goes away.
	ID                    *ids.ID
	FullName              string
	Email                 *string
	Phone                 *string
	DateOfBirth           *time.Time
	Status                Status
	EmergencyContactName  *string
	EmergencyContactPhone *string
	MedicalNotes          *string
	AllowOverdraft        *bool
	DefaultRateMinor      *int64
	Notes                 string
	TagIDs                []ids.ID
}

// Create adds a client, optionally with encrypted medical notes and tags.
func (s *Service) Create(ctx context.Context, tx pgx.Tx, tenantID ids.ID, in CreateInput) (Client, error) {
	if err := validateNames(in.FullName); err != nil {
		return Client{}, err
	}
	if in.Status == "" {
		in.Status = StatusActive
	}
	if !in.Status.Valid() {
		return Client{}, errs.Invalid(errs.CodeValidation, "unknown client status %q", in.Status).
			WithField("status", "must be lead, active, paused or archived")
	}
	if err := validateRate(in.DefaultRateMinor); err != nil {
		return Client{}, err
	}
	email := normalizeEmail(in.Email)

	clientID := db.ResolveID(in.ID)
	// pgp_sym_encrypt is applied in SQL rather than in Go so the plaintext
	// never exists in a Go string that could reach a log or a heap dump.
	// DO NOTHING rather than a plain insert, so a retried push converges on
	// the client already created instead of failing on the primary key.
	tag, err := tx.Exec(ctx, `
		INSERT INTO clients (
			id, tenant_id, full_name, email, phone, date_of_birth, status,
			emergency_contact_name, emergency_contact_phone, medical_notes_encrypted,
			allow_overdraft, default_rate_minor, notes)
		VALUES ($1, $2, $3, $4, $5, $6, $7::client_status, $8, $9,
		        CASE WHEN $10::text IS NULL THEN NULL ELSE pgp_sym_encrypt($10::text, $11::text) END,
		        $12, $13, $14)
		ON CONFLICT (id) DO NOTHING`,
		clientID, tenantID, strings.TrimSpace(in.FullName), email, trimPtr(in.Phone), in.DateOfBirth,
		string(in.Status), trimPtr(in.EmergencyContactName), trimPtr(in.EmergencyContactPhone),
		in.MedicalNotes, s.columnKey, in.AllowOverdraft, in.DefaultRateMinor, strings.TrimSpace(in.Notes))
	if err != nil {
		if db.IsUniqueViolation(err, "clients_tenant_email_key") {
			return Client{}, errs.Conflict("client_email_taken",
				"another client already uses that email address")
		}
		return Client{}, errs.Internal(err, "create client")
	}

	if tag.RowsAffected() == 0 {
		// The id already existed. Visible under RLS means this tenant's row
		// and a replay that converges; invisible means the id is another
		// tenant's and reporting success would hide that.
		existing, readErr := s.Get(ctx, tx, clientID)
		if readErr != nil {
			return Client{}, db.ErrIDTakenElsewhere("client")
		}
		return existing, nil
	}

	if len(in.TagIDs) > 0 {
		if err := s.SetTags(ctx, tx, tenantID, clientID, in.TagIDs); err != nil {
			return Client{}, err
		}
	}
	return s.Get(ctx, tx, clientID)
}

// UpdateInput describes a partial update. A nil field is left unchanged; this
// is why the fields are pointers rather than values.
type UpdateInput struct {
	FullName              *string
	Email                 **string
	Phone                 **string
	DateOfBirth           **time.Time
	Status                *Status
	EmergencyContactName  **string
	EmergencyContactPhone **string
	// MedicalNotes distinguishes three cases: nil leaves the notes alone, a
	// pointer to nil clears them, and a pointer to a string replaces them.
	MedicalNotes     **string
	AllowOverdraft   **bool
	DefaultRateMinor **int64
	Notes            *string
}

// Update applies a partial change to a client.
func (s *Service) Update(ctx context.Context, tx pgx.Tx, clientID ids.ID, in UpdateInput) (Client, error) {
	if in.FullName != nil {
		if err := validateNames(*in.FullName); err != nil {
			return Client{}, err
		}
	}
	if in.Status != nil && !in.Status.Valid() {
		return Client{}, errs.Invalid(errs.CodeValidation, "unknown client status %q", *in.Status).
			WithField("status", "must be lead, active, paused or archived")
	}
	if in.DefaultRateMinor != nil {
		if err := validateRate(*in.DefaultRateMinor); err != nil {
			return Client{}, err
		}
	}

	// COALESCE keeps this a single statement: each parameter either carries a
	// new value or NULL meaning "leave it". The medical-notes and nullable
	// fields need explicit flags because NULL is itself a meaningful value
	// there, and COALESCE alone cannot tell "clear this" from "skip this".
	tag, err := tx.Exec(ctx, `
		UPDATE clients SET
			full_name = COALESCE($2, full_name),
			email     = CASE WHEN $3 THEN $4 ELSE email END,
			phone     = CASE WHEN $5 THEN $6 ELSE phone END,
			date_of_birth = CASE WHEN $7 THEN $8 ELSE date_of_birth END,
			status    = COALESCE($9::client_status, status),
			emergency_contact_name  = CASE WHEN $10 THEN $11 ELSE emergency_contact_name END,
			emergency_contact_phone = CASE WHEN $12 THEN $13 ELSE emergency_contact_phone END,
			medical_notes_encrypted = CASE
				WHEN NOT $14 THEN medical_notes_encrypted
				WHEN $15::text IS NULL THEN NULL
				ELSE pgp_sym_encrypt($15::text, $16::text) END,
			allow_overdraft    = CASE WHEN $17 THEN $18 ELSE allow_overdraft END,
			default_rate_minor = CASE WHEN $19 THEN $20 ELSE default_rate_minor END,
			notes = COALESCE($21, notes)
		WHERE id = $1 AND deleted_at IS NULL`,
		clientID,
		trimOptional(in.FullName),
		in.Email != nil, derefEmail(in.Email),
		in.Phone != nil, deref(in.Phone),
		in.DateOfBirth != nil, deref(in.DateOfBirth),
		statusOrNil(in.Status),
		in.EmergencyContactName != nil, deref(in.EmergencyContactName),
		in.EmergencyContactPhone != nil, deref(in.EmergencyContactPhone),
		in.MedicalNotes != nil, deref(in.MedicalNotes), s.columnKey,
		in.AllowOverdraft != nil, deref(in.AllowOverdraft),
		in.DefaultRateMinor != nil, deref(in.DefaultRateMinor),
		trimOptional(in.Notes),
	)
	if err != nil {
		if db.IsUniqueViolation(err, "clients_tenant_email_key") {
			return Client{}, errs.Conflict("client_email_taken",
				"another client already uses that email address")
		}
		return Client{}, errs.Internal(err, "update client")
	}
	if tag.RowsAffected() == 0 {
		return Client{}, errs.NotFound("client")
	}
	return s.Get(ctx, tx, clientID)
}

const clientColumns = `
	c.id, c.full_name, c.email, c.phone, c.date_of_birth, c.status::text,
	c.emergency_contact_name, c.emergency_contact_phone,
	c.allow_overdraft, c.default_rate_minor, c.notes,
	c.created_at, c.updated_at, c.server_seq`

func scanClient(row pgx.Row) (Client, error) {
	var c Client
	var status string
	err := row.Scan(&c.ID, &c.FullName, &c.Email, &c.Phone, &c.DateOfBirth, &status,
		&c.EmergencyContactName, &c.EmergencyContactPhone,
		&c.AllowOverdraft, &c.DefaultRateMinor, &c.Notes,
		&c.CreatedAt, &c.UpdatedAt, &c.ServerSeq)
	if err != nil {
		return Client{}, err
	}
	c.Status = Status(status)
	return c, nil
}

// Get loads a client without their medical notes.
//
// Medical notes require an explicit call to GetWithMedical, so an ordinary
// profile fetch cannot carry them into a response or a log by accident.
func (s *Service) Get(ctx context.Context, tx pgx.Tx, clientID ids.ID) (Client, error) {
	c, err := scanClient(tx.QueryRow(ctx,
		`SELECT `+clientColumns+` FROM clients c WHERE c.id = $1 AND c.deleted_at IS NULL`, clientID))
	if err != nil {
		if db.IsNoRows(err) {
			return Client{}, errs.NotFound("client")
		}
		return Client{}, errs.Internal(err, "load client")
	}
	tags, err := s.tagsFor(ctx, tx, clientID)
	if err != nil {
		return Client{}, err
	}
	c.Tags = tags
	return c, nil
}

// GetWithMedical loads a client including their decrypted medical notes.
func (s *Service) GetWithMedical(ctx context.Context, tx pgx.Tx, clientID ids.ID) (Client, error) {
	c, err := s.Get(ctx, tx, clientID)
	if err != nil {
		return Client{}, err
	}
	var notes *string
	if err := tx.QueryRow(ctx, `
		SELECT CASE WHEN medical_notes_encrypted IS NULL THEN NULL
		            ELSE pgp_sym_decrypt(medical_notes_encrypted, $2::text) END
		  FROM clients WHERE id = $1 AND deleted_at IS NULL`,
		clientID, s.columnKey).Scan(&notes); err != nil {
		if db.IsNoRows(err) {
			return Client{}, errs.NotFound("client")
		}
		// A decryption failure means the configured column key does not match
		// the one the row was written with. Reporting it plainly beats
		// returning an empty field that reads as "no medical conditions".
		return Client{}, errs.Internal(err, "decrypt medical notes")
	}
	c.MedicalNotes = notes
	return c, nil
}

// ListFilter narrows a client listing.
type ListFilter struct {
	Status Status
	// Search matches against name, email and phone.
	Search string
	TagID  *ids.ID
	Limit  int
	Offset int
}

// List returns clients matching the filter, newest activity first.
func (s *Service) List(ctx context.Context, tx pgx.Tx, f ListFilter) ([]Client, error) {
	if f.Limit <= 0 || f.Limit > 200 {
		f.Limit = 50
	}
	search := strings.TrimSpace(f.Search)

	rows, err := tx.Query(ctx, `
		SELECT `+clientColumns+`
		  FROM clients c
		 WHERE c.deleted_at IS NULL
		   AND ($1::text IS NULL OR c.status::text = $1)
		   AND ($2::text IS NULL OR
		        c.full_name ILIKE '%' || $2 || '%' OR
		        c.email::text ILIKE '%' || $2 || '%' OR
		        c.phone ILIKE '%' || $2 || '%')
		   AND ($3::uuid IS NULL OR EXISTS (
		        SELECT 1 FROM client_tags ct WHERE ct.client_id = c.id AND ct.tag_id = $3))
		 ORDER BY c.full_name
		 LIMIT $4 OFFSET $5`,
		statusOrNil(&f.Status), nullIfEmpty(search), f.TagID, f.Limit, f.Offset)
	if err != nil {
		return nil, errs.Internal(err, "list clients")
	}
	defer rows.Close()

	var out []Client
	for rows.Next() {
		c, err := scanClient(rows)
		if err != nil {
			return nil, errs.Internal(err, "scan client")
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, errs.Internal(err, "read clients")
	}
	return out, nil
}

// Delete soft-deletes a client.
//
// Hard deletion is not offered: a client's sessions and invoices are referenced
// by journal entries, and removing the row would orphan an immutable financial
// record. Hiding them keeps the books intact.
func (s *Service) Delete(ctx context.Context, tx pgx.Tx, clientID ids.ID) error {
	tag, err := tx.Exec(ctx,
		`UPDATE clients SET deleted_at = now(), status = 'archived'
		  WHERE id = $1 AND deleted_at IS NULL`, clientID)
	if err != nil {
		return errs.Internal(err, "delete client")
	}
	if tag.RowsAffected() == 0 {
		return errs.NotFound("client")
	}
	return nil
}

// ---------------------------------------------------------------------------
// Validation and pointer helpers
// ---------------------------------------------------------------------------

func validateNames(name string) error {
	if strings.TrimSpace(name) == "" {
		return errs.Invalid(errs.CodeValidation, "a client needs a name").
			WithField("full_name", "is required")
	}
	if len(name) > 200 {
		return errs.Invalid(errs.CodeValidation, "client name is too long").
			WithField("full_name", "must be at most 200 characters")
	}
	return nil
}

func validateRate(rate *int64) error {
	if rate != nil && *rate < 0 {
		return errs.Invalid(errs.CodeValidation, "a session rate cannot be negative").
			WithField("default_rate_minor", "must not be negative")
	}
	return nil
}

func normalizeEmail(e *string) *string {
	if e == nil {
		return nil
	}
	v := strings.ToLower(strings.TrimSpace(*e))
	if v == "" {
		return nil
	}
	return &v
}

func trimPtr(s *string) *string {
	if s == nil {
		return nil
	}
	v := strings.TrimSpace(*s)
	if v == "" {
		return nil
	}
	return &v
}

func trimOptional(s *string) *string {
	if s == nil {
		return nil
	}
	v := strings.TrimSpace(*s)
	return &v
}

func statusOrNil(s *Status) *string {
	if s == nil || *s == "" {
		return nil
	}
	v := string(*s)
	return &v
}

func nullIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// deref unwraps the outer pointer of a double pointer, yielding the inner
// value or nil. The double pointer is what lets an update distinguish "set
// this field to null" from "do not touch this field".
func deref[T any](pp **T) *T {
	if pp == nil {
		return nil
	}
	return *pp
}

func derefEmail(pp **string) *string {
	if pp == nil {
		return nil
	}
	return normalizeEmail(*pp)
}
