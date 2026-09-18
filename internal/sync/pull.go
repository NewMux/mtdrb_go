// Package sync moves data between the server and the offline clients.
//
// The two directions are deliberately asymmetric.
//
// Pull is rows: the client mirrors server state into local SQLite, so a
// trainer standing in a basement can read their roster and today's programme.
//
// Push is *operations*, not rows — see push.go. A generic row upsert would
// bypass every business rule in the system: a synced attendance row that did
// not burn a credit or post revenue would leave the calendar and the books
// disagreeing, which is exactly the failure this product exists to prevent.
package sync

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/NewMux/mtdrb_go/internal/platform/clock"
	"github.com/NewMux/mtdrb_go/internal/platform/errs"
)

// collection is one syncable table and the columns a client is allowed to see.
//
// The projection is explicit rather than `SELECT *` for the same reason the
// public invoice payload is assembled by hand: a column added later must not
// start flowing to every device by default. Encrypted medical notes, share
// token hashes and signed waiver bodies are all absent on purpose.
type collection struct {
	Name    string
	Table   string
	Columns string
}

// collections is the full set a client mirrors.
//
// Rows are never hard-deleted from these tables; a removal sets deleted_at,
// which bumps server_seq, so the client sees the row again and drops it
// locally. That is why no separate tombstone table exists.
var collections = []collection{
	{"clients", "clients", `id, full_name, email, phone, date_of_birth, status::text AS status,
		emergency_contact_name, emergency_contact_phone, allow_overdraft,
		default_rate_minor, notes, created_at, updated_at, deleted_at`},

	{"biometric_entries", "biometric_entries", `id, client_id, measured_on, weight_grams,
		body_fat_bp, circumferences, notes, created_at, updated_at, deleted_at`},

	{"parq_responses", "parq_responses", `id, client_id, answers, requires_clearance,
		cleared_at, completed_at`},

	// signed_body and signed_ip are omitted: the body is large and the address
	// is evidentiary detail no device needs to hold.
	{"waiver_signatures", "waiver_signatures", `id, waiver_id, client_id, signed_name,
		signed_at, signature_media_id`},

	{"packages", "packages", `id, client_id, invoice_id, name, credits_total,
		credits_remaining, unit_price_minor, currency, purchased_on, expires_at,
		status::text AS status`},

	{"credit_transactions", "credit_transactions", `id, package_id, client_id, delta,
		reason::text AS reason, session_attendee_id, journal_entry_id, memo, created_at`},

	{"session_types", "session_types", `id, name, duration_minutes, capacity, credit_cost,
		colour, archived_at`},

	{"sessions", "sessions", `id, session_type_id, series_id, starts_at, ends_at,
		status::text AS status, location, notes, created_at, updated_at`},

	{"session_attendees", "session_attendees", `id, session_id, client_id,
		status::text AS status, credits_charged, marked_at, notes, created_at, updated_at`},

	// share_token_hash is absent: it is the secret behind a public link and
	// has no business on a device.
	{"invoices", "invoices", `id, client_id, number, status::text AS status, currency,
		total_minor, issue_date, due_date, notes, payment_instructions_snapshot,
		issued_at, settled_at, voided_at, created_at, updated_at, deleted_at`},

	{"payments", "payments", `id, invoice_id, client_id, amount_minor, currency,
		instrument::text AS instrument, received_on, reference, notes,
		journal_entry_id, reversed_by, reverses_id, created_at`},

	{"exercises", "exercises", `id, name, category::text AS category, primary_muscle,
		equipment, instructions, video_url, demo_media_id, is_custom, archived_at`},

	{"programs", "programs", `id, name, description, is_template, archived_at,
		created_at, updated_at`},

	{"program_assignments", "program_assignments", `id, program_id, client_id, starts_on,
		ends_on, status::text AS status, notes, created_at, updated_at`},

	{"workout_sessions", "workout_sessions", `id, client_id, assignment_id, day_id,
		session_id, week_number, performed_on, status::text AS status, notes,
		created_at, updated_at, deleted_at`},

	{"set_logs", "set_logs", `id, workout_session_id, exercise_id, program_exercise_id,
		set_index, reps, load_grams, rpe_tenths, rir, rest_seconds, tempo,
		is_warmup, completed, notes, form_check_media_id, created_at, updated_at`},
}

// session_types, program_blocks, program_days and program_exercises carry no
// server_seq of their own. The first is small enough to send whole; the
// programme structure is fetched as an aggregate when its programme row
// changes, because a block edited in isolation is not a thing a trainer does.
var wholeTableCollections = map[string]bool{"session_types": true}

// Cursor tracks progress per collection.
//
// A single global sequence number cannot work here, and the paging test is
// what proved it: collections are read in order, so a page that fills its
// budget on `clients` would advance one shared cursor past every `exercises`
// row behind it, and those rows would never be delivered again. The data loss
// is silent, which is the worst kind.
//
// So progress is per collection. The client treats the encoded form as opaque
// and hands it back unchanged.
type Cursor map[string]int64

// Encode renders a cursor as an opaque token.
func (c Cursor) Encode() string {
	if len(c) == 0 {
		return ""
	}
	raw, err := json.Marshal(c)
	if err != nil {
		// Marshalling a map[string]int64 cannot fail.
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

// DecodeCursor parses a token from a client.
//
// An unreadable cursor is an error rather than a silent reset: quietly
// restarting from zero would have a device re-download everything and look
// like a performance problem instead of the bug it is.
func DecodeCursor(token string) (Cursor, error) {
	if strings.TrimSpace(token) == "" {
		return Cursor{}, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return nil, errs.Invalid(errs.CodeValidation, "the sync cursor is not readable").
			WithField("cursor", "must be a token returned by a previous pull")
	}
	var c Cursor
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, errs.Invalid(errs.CodeValidation, "the sync cursor is not readable").
			WithField("cursor", "must be a token returned by a previous pull")
	}
	for collection, seq := range c {
		if seq < 0 {
			return nil, errs.Invalid(errs.CodeValidation,
				"the sync cursor for %s is negative", collection)
		}
	}
	return c, nil
}

// Changes is one collection's slice of a pull.
type Changes struct {
	Collection string            `json:"collection"`
	Rows       []json.RawMessage `json:"rows"`
}

// PullResult is everything that changed since a cursor.
type PullResult struct {
	// Cursor is opaque; the client stores it and sends it back untouched.
	Cursor  string    `json:"cursor"`
	Changes []Changes `json:"changes"`
	// HasMore tells the client to pull again immediately rather than waiting
	// for the next sync interval.
	HasMore    bool   `json:"has_more"`
	ServerTime string `json:"server_time"`
}

// Service reads and applies sync traffic.
type Service struct {
	clock clock.Clock
}

// NewService builds the sync service.
func NewService(c clock.Clock) *Service {
	if c == nil {
		c = clock.System{}
	}
	return &Service{clock: c}
}

// maxPullRows bounds one page across all collections, so a first sync on a
// large account arrives in chunks rather than as one enormous response a
// phone has to hold in memory.
const maxPullRows = 500

// Pull returns everything visible to the caller that changed since the cursor.
//
// Visibility is not decided here: the query runs inside the caller's
// tenant-bound transaction, so row-level security narrows a trainer to their
// tenant and a portal client to their own rows. Sync gets the same isolation
// as every other read, for free, because it does not try to be clever.
func (s *Service) Pull(ctx context.Context, tx pgx.Tx, cursor Cursor, limit int) (PullResult, error) {
	if limit <= 0 || limit > maxPullRows {
		limit = maxPullRows
	}
	if cursor == nil {
		cursor = Cursor{}
	}

	next := make(Cursor, len(collections))
	for k, v := range cursor {
		next[k] = v
	}

	result := PullResult{
		Changes:    []Changes{},
		ServerTime: s.clock.Now().Format(time.RFC3339),
	}
	remaining := limit

	for _, c := range collections {
		if remaining <= 0 {
			// Budget spent. Collections not reached keep their existing
			// cursor, so nothing is skipped — they are simply read next time.
			result.HasMore = true
			break
		}
		since := cursor[c.Name]

		var query string
		var args []any
		if wholeTableCollections[c.Name] {
			// Sent in full on a first sync only; these tables are small and
			// change rarely, and giving them a sequence is not worth a
			// migration.
			if since > 0 {
				continue
			}
			query = `SELECT to_jsonb(r) FROM (SELECT ` + c.Columns + ` FROM ` + c.Table + `) r`
		} else {
			query = `SELECT to_jsonb(r), r.server_seq
			           FROM (SELECT ` + c.Columns + `, server_seq FROM ` + c.Table + `
			                  WHERE server_seq > $1 ORDER BY server_seq LIMIT $2) r`
			args = []any{since, remaining}
		}

		rows, err := tx.Query(ctx, query, args...)
		if err != nil {
			return PullResult{}, errs.Internal(err, "pull %s", c.Name)
		}

		changes := Changes{Collection: c.Name}
		highest := since
		for rows.Next() {
			var raw []byte
			if wholeTableCollections[c.Name] {
				if err := rows.Scan(&raw); err != nil {
					rows.Close()
					return PullResult{}, errs.Internal(err, "scan %s", c.Name)
				}
				highest = 1 // marks it as delivered; it is sent once
			} else {
				var seq int64
				if err := rows.Scan(&raw, &seq); err != nil {
					rows.Close()
					return PullResult{}, errs.Internal(err, "scan %s", c.Name)
				}
				if seq > highest {
					highest = seq
				}
			}
			changes.Rows = append(changes.Rows, json.RawMessage(raw))
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return PullResult{}, errs.Internal(err, "read %s", c.Name)
		}

		// The cursor advances only as far as this collection's own rows, so a
		// truncated page resumes exactly where it stopped.
		next[c.Name] = highest

		if len(changes.Rows) > 0 {
			if len(changes.Rows) == remaining {
				result.HasMore = true
			}
			remaining -= len(changes.Rows)
			result.Changes = append(result.Changes, changes)
		}
	}

	result.Cursor = next.Encode()
	return result, nil
}

// Checkpoint is the current high-water mark, for a client that wants to start
// syncing from "now" rather than replaying history.
func (s *Service) Checkpoint(ctx context.Context, tx pgx.Tx) (Cursor, error) {
	var seq int64
	if err := tx.QueryRow(ctx, `SELECT last_value FROM sync_seq`).Scan(&seq); err != nil {
		return nil, errs.Internal(err, "read sync checkpoint")
	}
	c := make(Cursor, len(collections))
	for _, col := range collections {
		c[col.Name] = seq
	}
	return c, nil
}
