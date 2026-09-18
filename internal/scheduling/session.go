// Package scheduling owns the calendar and the attendance state machine.
//
// Attendance is recorded per attendee rather than per session. In a
// semi-private slot one client can complete while another no-shows, and each
// outcome has its own credit and revenue consequence; putting the status on
// the session would make that unrepresentable.
//
// Marking attendance is never just a status change. It burns or restores
// credits and posts to the ledger, all inside one transaction, so the calendar
// and the books cannot disagree.
package scheduling

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/NewMux/mtdrb_go/internal/billing"
	"github.com/NewMux/mtdrb_go/internal/db"
	"github.com/NewMux/mtdrb_go/internal/ledger"
	"github.com/NewMux/mtdrb_go/internal/platform/clock"
	"github.com/NewMux/mtdrb_go/internal/platform/errs"
	"github.com/NewMux/mtdrb_go/internal/platform/ids"
)

// SessionType is a bookable kind of session.
type SessionType struct {
	ID              ids.ID `json:"id"`
	Name            string `json:"name"`
	DurationMinutes int    `json:"duration_minutes"`
	// Capacity of 1 is one-to-one; higher is semi-private or small group.
	Capacity   int     `json:"capacity"`
	CreditCost int     `json:"credit_cost"`
	Colour     *string `json:"colour,omitempty"`
}

// Status is a session's scheduling state, distinct from attendance.
type Status string

const (
	StatusScheduled Status = "scheduled"
	StatusCancelled Status = "cancelled"
)

// Session is one slot on the calendar.
type Session struct {
	ID            ids.ID     `json:"id"`
	SessionTypeID ids.ID     `json:"session_type_id"`
	SeriesID      *ids.ID    `json:"series_id,omitempty"`
	StartsAt      time.Time  `json:"starts_at"`
	EndsAt        time.Time  `json:"ends_at"`
	Status        Status     `json:"status"`
	Location      string     `json:"location"`
	Notes         string     `json:"notes"`
	Attendees     []Attendee `json:"attendees"`
	ServerSeq     int64      `json:"server_seq"`
}

// Service manages sessions and attendance.
type Service struct {
	billing *billing.Service
	ledger  *ledger.Service
	clock   clock.Clock
}

// NewService builds the scheduling service.
func NewService(b *billing.Service, l *ledger.Service, c clock.Clock) *Service {
	if c == nil {
		c = clock.System{}
	}
	return &Service{billing: b, ledger: l, clock: c}
}

// CreateSessionTypeInput describes a bookable session kind.
type CreateSessionTypeInput struct {
	Name            string
	DurationMinutes int
	Capacity        int
	CreditCost      int
	Colour          *string
}

// CreateSessionType adds a bookable session kind.
func (s *Service) CreateSessionType(ctx context.Context, tx pgx.Tx, tenantID ids.ID, in CreateSessionTypeInput) (SessionType, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return SessionType{}, errs.Invalid(errs.CodeValidation, "a session type needs a name").
			WithField("name", "is required")
	}
	if in.DurationMinutes <= 0 || in.DurationMinutes > 600 {
		return SessionType{}, errs.Invalid(errs.CodeValidation,
			"duration must be between 1 and 600 minutes").
			WithField("duration_minutes", "must be between 1 and 600")
	}
	if in.Capacity <= 0 {
		in.Capacity = 1
	}
	if in.Capacity > 50 {
		return SessionType{}, errs.Invalid(errs.CodeValidation, "capacity must be at most 50").
			WithField("capacity", "must be at most 50")
	}
	if in.CreditCost < 0 {
		return SessionType{}, errs.Invalid(errs.CodeValidation, "credit cost cannot be negative").
			WithField("credit_cost", "must not be negative")
	}

	st := SessionType{
		ID: ids.New(), Name: name, DurationMinutes: in.DurationMinutes,
		Capacity: in.Capacity, CreditCost: in.CreditCost, Colour: in.Colour,
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO session_types (id, tenant_id, name, duration_minutes, capacity, credit_cost, colour)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		st.ID, tenantID, st.Name, st.DurationMinutes, st.Capacity, st.CreditCost, st.Colour); err != nil {
		if db.IsUniqueViolation(err, "session_types_tenant_name_key") {
			return SessionType{}, errs.Conflict("session_type_exists",
				"a session type named %q already exists", name)
		}
		return SessionType{}, errs.Internal(err, "create session type")
	}
	return st, nil
}

// ListSessionTypes returns the tenant's active session types.
func (s *Service) ListSessionTypes(ctx context.Context, tx pgx.Tx) ([]SessionType, error) {
	rows, err := tx.Query(ctx, `
		SELECT id, name, duration_minutes, capacity, credit_cost, colour
		  FROM session_types WHERE archived_at IS NULL ORDER BY name`)
	if err != nil {
		return nil, errs.Internal(err, "list session types")
	}
	defer rows.Close()

	var out []SessionType
	for rows.Next() {
		var st SessionType
		if err := rows.Scan(&st.ID, &st.Name, &st.DurationMinutes, &st.Capacity,
			&st.CreditCost, &st.Colour); err != nil {
			return nil, errs.Internal(err, "scan session type")
		}
		out = append(out, st)
	}
	if err := rows.Err(); err != nil {
		return nil, errs.Internal(err, "read session types")
	}
	return out, nil
}

// BookInput describes a session to schedule.
type BookInput struct {
	SessionTypeID ids.ID
	StartsAt      time.Time
	ClientIDs     []ids.ID
	Location      string
	Notes         string
}

// Book schedules a session and puts its attendees on the roster.
//
// The end time comes from the session type, and the buffered slot is written
// alongside so the database's exclusion constraint can refuse an overlap. That
// check is not repeated here: a read-then-write check in application code is
// exactly what two devices syncing the same booking would defeat.
func (s *Service) Book(ctx context.Context, tx pgx.Tx, tenantID ids.ID, in BookInput) (Session, error) {
	if in.StartsAt.IsZero() {
		return Session{}, errs.Invalid(errs.CodeValidation, "a session needs a start time").
			WithField("starts_at", "is required")
	}

	var duration, capacity int
	if err := tx.QueryRow(ctx,
		`SELECT duration_minutes, capacity FROM session_types
		  WHERE id = $1 AND archived_at IS NULL`, in.SessionTypeID,
	).Scan(&duration, &capacity); err != nil {
		if db.IsNoRows(err) {
			return Session{}, errs.NotFound("session type")
		}
		return Session{}, errs.Internal(err, "load session type")
	}
	if len(in.ClientIDs) > capacity {
		return Session{}, errs.Invalid(errs.CodeValidation,
			"this session type seats %d, but %d clients were given", capacity, len(in.ClientIDs)).
			WithField("client_ids", "exceeds the session type's capacity")
	}

	var bufferMinutes int
	if err := tx.QueryRow(ctx,
		`SELECT buffer_minutes FROM tenants WHERE id = $1`, tenantID).Scan(&bufferMinutes); err != nil {
		return Session{}, errs.Internal(err, "load tenant buffer")
	}

	startsAt := in.StartsAt.UTC()
	endsAt := startsAt.Add(time.Duration(duration) * time.Minute)
	// The buffer pads the end only. Padding both sides would double-count the
	// gap between two sessions and reject bookings that are legitimately far
	// enough apart.
	blockedUntil := endsAt.Add(time.Duration(bufferMinutes) * time.Minute)

	session := Session{
		ID: ids.New(), SessionTypeID: in.SessionTypeID, StartsAt: startsAt, EndsAt: endsAt,
		Status: StatusScheduled, Location: strings.TrimSpace(in.Location),
		Notes: strings.TrimSpace(in.Notes),
	}

	if err := tx.QueryRow(ctx, `
		INSERT INTO sessions
			(id, tenant_id, session_type_id, starts_at, ends_at, blocked_range, location, notes)
		VALUES ($1, $2, $3, $4, $5, tstzrange($4, $6, '[)'), $7, $8)
		RETURNING server_seq`,
		session.ID, tenantID, in.SessionTypeID, startsAt, endsAt, blockedUntil,
		session.Location, session.Notes,
	).Scan(&session.ServerSeq); err != nil {
		if isExclusionViolation(err) {
			return Session{}, errs.Conflict(errs.CodeSchedulingConflict,
				"that slot overlaps an existing session, including the %d-minute buffer", bufferMinutes).
				WithMeta("buffer_minutes", bufferMinutes)
		}
		if db.IsForeignKeyViolation(err) {
			return Session{}, errs.NotFound("session type")
		}
		return Session{}, errs.Internal(err, "create session")
	}

	for _, clientID := range in.ClientIDs {
		if _, err := s.addAttendee(ctx, tx, tenantID, session.ID, clientID); err != nil {
			return Session{}, err
		}
	}

	return s.Get(ctx, tx, session.ID)
}

// addAttendee puts one client on a session's roster.
func (s *Service) addAttendee(ctx context.Context, tx pgx.Tx, tenantID, sessionID, clientID ids.ID) (Attendee, error) {
	a := Attendee{ID: ids.New(), SessionID: sessionID, ClientID: clientID, Status: Scheduled}
	if _, err := tx.Exec(ctx,
		`INSERT INTO session_attendees (id, tenant_id, session_id, client_id) VALUES ($1, $2, $3, $4)`,
		a.ID, tenantID, sessionID, clientID); err != nil {
		if db.IsUniqueViolation(err, "session_attendees_unique") {
			return Attendee{}, errs.Conflict("already_booked",
				"that client is already on this session's roster")
		}
		if db.IsCheckViolation(err, "") {
			return Attendee{}, errs.Conflict(errs.CodeSchedulingConflict, "this session is full")
		}
		if db.IsForeignKeyViolation(err) {
			return Attendee{}, errs.NotFound("client or session")
		}
		return Attendee{}, errs.Internal(err, "add attendee")
	}
	return a, nil
}

// AddAttendee puts an additional client on an existing session's roster.
func (s *Service) AddAttendee(ctx context.Context, tx pgx.Tx, tenantID, sessionID, clientID ids.ID) (Attendee, error) {
	return s.addAttendee(ctx, tx, tenantID, sessionID, clientID)
}

// Get loads a session with its roster.
func (s *Service) Get(ctx context.Context, tx pgx.Tx, sessionID ids.ID) (Session, error) {
	var sess Session
	var status string
	err := tx.QueryRow(ctx, `
		SELECT id, session_type_id, series_id, starts_at, ends_at, status::text,
		       location, notes, server_seq
		  FROM sessions WHERE id = $1`, sessionID,
	).Scan(&sess.ID, &sess.SessionTypeID, &sess.SeriesID, &sess.StartsAt, &sess.EndsAt,
		&status, &sess.Location, &sess.Notes, &sess.ServerSeq)
	if err != nil {
		if db.IsNoRows(err) {
			return Session{}, errs.NotFound("session")
		}
		return Session{}, errs.Internal(err, "load session")
	}
	sess.Status = Status(status)

	attendees, err := s.attendeesFor(ctx, tx, sessionID)
	if err != nil {
		return Session{}, err
	}
	sess.Attendees = attendees
	return sess, nil
}

// ListRange returns sessions overlapping a window, for the calendar.
func (s *Service) ListRange(ctx context.Context, tx pgx.Tx, from, to time.Time) ([]Session, error) {
	rows, err := tx.Query(ctx, `
		SELECT id, session_type_id, series_id, starts_at, ends_at, status::text,
		       location, notes, server_seq
		  FROM sessions
		 WHERE starts_at < $2 AND ends_at > $1
		 ORDER BY starts_at`, from, to)
	if err != nil {
		return nil, errs.Internal(err, "list sessions")
	}
	defer rows.Close()

	var out []Session
	var sessionIDs []ids.ID
	for rows.Next() {
		var sess Session
		var status string
		if err := rows.Scan(&sess.ID, &sess.SessionTypeID, &sess.SeriesID, &sess.StartsAt,
			&sess.EndsAt, &status, &sess.Location, &sess.Notes, &sess.ServerSeq); err != nil {
			return nil, errs.Internal(err, "scan session")
		}
		sess.Status = Status(status)
		out = append(out, sess)
		sessionIDs = append(sessionIDs, sess.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, errs.Internal(err, "read sessions")
	}
	if len(out) == 0 {
		return out, nil
	}

	// Rosters are loaded in one query rather than per session: a week view of
	// a busy trainer is otherwise dozens of round trips on a phone.
	bySession, err := s.attendeesForMany(ctx, tx, sessionIDs)
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].Attendees = bySession[out[i].ID]
	}
	return out, nil
}

// Cancel cancels a scheduled session, freeing its slot.
//
// Attendees keep whatever attendance they already had: cancelling a session
// after the fact must not silently refund a credit that was correctly burned.
func (s *Service) Cancel(ctx context.Context, tx pgx.Tx, sessionID ids.ID) error {
	tag, err := tx.Exec(ctx,
		`UPDATE sessions SET status = 'cancelled', cancelled_at = now()
		  WHERE id = $1 AND status = 'scheduled'`, sessionID)
	if err != nil {
		return errs.Internal(err, "cancel session")
	}
	if tag.RowsAffected() == 0 {
		return errs.NotFound("scheduled session")
	}
	return nil
}

// Reschedule moves a session, re-checking the buffer at its new time.
func (s *Service) Reschedule(ctx context.Context, tx pgx.Tx, tenantID, sessionID ids.ID, startsAt time.Time) (Session, error) {
	var duration, bufferMinutes int
	if err := tx.QueryRow(ctx, `
		SELECT st.duration_minutes, t.buffer_minutes
		  FROM sessions s
		  JOIN session_types st ON st.id = s.session_type_id
		  JOIN tenants t ON t.id = s.tenant_id
		 WHERE s.id = $1`, sessionID).Scan(&duration, &bufferMinutes); err != nil {
		if db.IsNoRows(err) {
			return Session{}, errs.NotFound("session")
		}
		return Session{}, errs.Internal(err, "load session for rescheduling")
	}

	startsAt = startsAt.UTC()
	endsAt := startsAt.Add(time.Duration(duration) * time.Minute)
	blockedUntil := endsAt.Add(time.Duration(bufferMinutes) * time.Minute)

	tag, err := tx.Exec(ctx, `
		UPDATE sessions
		   SET starts_at = $2, ends_at = $3, blocked_range = tstzrange($2, $4, '[)')
		 WHERE id = $1 AND status = 'scheduled'`,
		sessionID, startsAt, endsAt, blockedUntil)
	if err != nil {
		if isExclusionViolation(err) {
			return Session{}, errs.Conflict(errs.CodeSchedulingConflict,
				"that slot overlaps an existing session, including the %d-minute buffer", bufferMinutes).
				WithMeta("buffer_minutes", bufferMinutes)
		}
		return Session{}, errs.Internal(err, "reschedule session")
	}
	if tag.RowsAffected() == 0 {
		return Session{}, errs.NotFound("scheduled session")
	}
	return s.Get(ctx, tx, sessionID)
}

// isExclusionViolation reports whether err came from the no-overlap
// constraint. Postgres reports exclusion violations as 23P01.
func isExclusionViolation(err error) bool {
	return db.IsSQLState(err, "23P01")
}
