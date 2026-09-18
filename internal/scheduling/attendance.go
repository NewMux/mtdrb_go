package scheduling

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/NewMux/mtdrb_go/internal/billing"
	"github.com/NewMux/mtdrb_go/internal/db"
	"github.com/NewMux/mtdrb_go/internal/ledger"
	"github.com/NewMux/mtdrb_go/internal/platform/errs"
	"github.com/NewMux/mtdrb_go/internal/platform/ids"
	"github.com/NewMux/mtdrb_go/internal/platform/money"
)

// AttendanceStatus is one attendee's outcome for a session.
type AttendanceStatus string

const (
	Scheduled   AttendanceStatus = "scheduled"
	Completed   AttendanceStatus = "completed"
	LateCancel  AttendanceStatus = "late_cancel"
	EarlyCancel AttendanceStatus = "early_cancel"
	NoShow      AttendanceStatus = "no_show"
)

// Attendee is one client's place on a session, and their outcome.
type Attendee struct {
	ID             ids.ID           `json:"id"`
	SessionID      ids.ID           `json:"session_id"`
	ClientID       ids.ID           `json:"client_id"`
	ClientName     string           `json:"client_name,omitempty"`
	Status         AttendanceStatus `json:"status"`
	CreditsCharged int              `json:"credits_charged"`
	MarkedAt       *time.Time       `json:"marked_at,omitempty"`
	Notes          string           `json:"notes"`
	ServerSeq      int64            `json:"server_seq"`
}

// effect describes what a transition does to credits and the books.
type effect struct {
	// billable means the attendance consumes credits and earns revenue.
	billable bool
	// lateCancellation books the revenue to its own account, so a trainer can
	// see how much of their income comes from cancellations.
	lateCancellation bool
}

// transitions is the complete state machine.
//
// It is a table rather than a chain of conditionals for two reasons: every
// permitted move is visible in one place, and an unlisted move is refused by
// default instead of falling through to whatever the last branch did.
//
// Every transition away from a billable state restores the credits that state
// charged, so a trainer who fat-fingers a completion on a gym floor can undo
// it without the books drifting.
var transitions = map[AttendanceStatus]map[AttendanceStatus]effect{
	Scheduled: {
		Completed:   {billable: true},
		LateCancel:  {billable: true, lateCancellation: true},
		EarlyCancel: {},
		// No-show billing is a tenant setting; resolved at transition time.
		NoShow: {},
	},
	Completed: {
		Scheduled:   {},
		LateCancel:  {billable: true, lateCancellation: true},
		EarlyCancel: {},
		NoShow:      {},
	},
	LateCancel: {
		Scheduled:   {},
		Completed:   {billable: true},
		EarlyCancel: {},
		NoShow:      {},
	},
	EarlyCancel: {
		Scheduled:  {},
		Completed:  {billable: true},
		LateCancel: {billable: true, lateCancellation: true},
		NoShow:     {},
	},
	NoShow: {
		Scheduled:   {},
		Completed:   {billable: true},
		LateCancel:  {billable: true, lateCancellation: true},
		EarlyCancel: {},
	},
}

// CanTransition reports whether a move between attendance states is allowed.
func CanTransition(from, to AttendanceStatus) bool {
	if from == to {
		return true // idempotent: re-marking the same status is a no-op
	}
	_, ok := transitions[from][to]
	return ok
}

// MarkInput describes an attendance change.
type MarkInput struct {
	AttendeeID ids.ID
	Status     AttendanceStatus
	Notes      string
	MarkedBy   *ids.ID
	// AllowOverdraft lets a completion proceed against a zero balance.
	AllowOverdraft bool
}

// MarkResult reports what a transition did, so the client can show the
// trainer the consequence rather than just a status.
type MarkResult struct {
	Attendee Attendee `json:"attendee"`
	// CreditsRemaining is the client's balance after the change. Journey A
	// turns on this number: at zero, the app offers a renewal invoice.
	CreditsRemaining int `json:"credits_remaining"`
	// RevenueRecognised is what this transition earned, if anything.
	RevenueRecognised *money.Money `json:"revenue_recognised,omitempty"`
	JournalEntryID    *ids.ID      `json:"journal_entry_id,omitempty"`
}

// Mark applies an attendance transition.
//
// Credit movement, revenue recognition and the status change all happen in the
// caller's transaction. Either the whole thing lands or none of it does: a
// burned credit with no revenue, or revenue with no burned credit, would each
// corrupt a figure the trainer relies on.
func (s *Service) Mark(ctx context.Context, tx pgx.Tx, tenantID ids.ID, in MarkInput) (MarkResult, error) {
	// Lock the attendance row first so two devices syncing the same
	// completion serialise rather than both burning a credit.
	var (
		current         AttendanceStatus
		clientID        ids.ID
		sessionID       ids.ID
		creditsCharged  int
		sessionStart    time.Time
		creditCost      int
		clientOverdraft *bool
	)
	err := tx.QueryRow(ctx, `
		SELECT sa.status::text, sa.client_id, sa.session_id, sa.credits_charged,
		       s.starts_at, st.credit_cost, c.allow_overdraft
		  FROM session_attendees sa
		  JOIN sessions s ON s.id = sa.session_id
		  JOIN session_types st ON st.id = s.session_type_id
		  JOIN clients c ON c.id = sa.client_id
		 WHERE sa.id = $1
		 FOR UPDATE OF sa`, in.AttendeeID,
	).Scan(&current, &clientID, &sessionID, &creditsCharged, &sessionStart, &creditCost, &clientOverdraft)
	if err != nil {
		if db.IsNoRows(err) {
			return MarkResult{}, errs.NotFound("session attendee")
		}
		return MarkResult{}, errs.Internal(err, "load attendance")
	}

	if !CanTransition(current, in.Status) {
		return MarkResult{}, errs.Conflict(errs.CodeInvalidTransition,
			"cannot change attendance from %s to %s", current, in.Status).
			WithMeta("from", string(current)).
			WithMeta("to", string(in.Status))
	}

	// Re-marking the same status is a no-op rather than an error, because the
	// offline outbox will replay it and a trainer will tap it twice.
	if current == in.Status {
		return s.result(ctx, tx, in.AttendeeID, clientID, nil, nil)
	}

	tenantSettings, err := s.tenantSettings(ctx, tx, tenantID)
	if err != nil {
		return MarkResult{}, err
	}

	// Resolve the no-show policy: it is a tenant setting, not a fixed rule.
	target := transitions[current][in.Status]
	if in.Status == NoShow && tenantSettings.NoShowIsBillable {
		target = effect{billable: true, lateCancellation: true}
	}
	previous := effect{}
	if current != Scheduled {
		previous = resolvePrevious(current, tenantSettings.NoShowIsBillable)
	}

	// Undo first: restore whatever the previous state charged, so moving
	// between two billable states nets out rather than double-charging.
	var reversalEntry *ids.ID
	if previous.billable && creditsCharged > 0 {
		entryID, err := s.reverseAttendanceRevenue(ctx, tx, in.AttendeeID, in.MarkedBy)
		if err != nil {
			return MarkResult{}, err
		}
		reversalEntry = entryID
		if err := s.billing.Restore(ctx, tx, tenantID, in.AttendeeID,
			"attendance changed", reversalEntry); err != nil {
			return MarkResult{}, err
		}
		creditsCharged = 0
	}

	var (
		journalEntryID *ids.ID
		recognised     *money.Money
	)

	if target.billable && creditCost > 0 {
		allowOverdraft := tenantSettings.AllowOverdraft
		if clientOverdraft != nil {
			allowOverdraft = *clientOverdraft
		}
		if in.AllowOverdraft {
			allowOverdraft = true
		}

		consumeInput := billing.ConsumeInput{
			ClientID:       clientID,
			Credits:        creditCost,
			Reason:         reasonFor(in.Status),
			AttendeeID:     &in.AttendeeID,
			Memo:           "session " + string(in.Status),
			AllowOverdraft: allowOverdraft,
		}

		// Plan, then post, then apply: the journal entry must exist before the
		// credit rows are written, because the credit log is append-only and
		// the link between the two is set at insert.
		planned, err := s.billing.PlanConsumption(ctx, tx, consumeInput)
		if err != nil {
			return MarkResult{}, err
		}

		total, err := sumConsumption(planned)
		if err != nil {
			return MarkResult{}, err
		}

		if !total.IsZero() {
			entryDate := sessionStart
			var posted ledger.Posted
			if target.lateCancellation {
				posted, err = s.ledger.PostLateCancellation(ctx, tx, in.AttendeeID, total,
					entryDate, "late cancellation", in.MarkedBy)
			} else {
				posted, err = s.ledger.PostSessionDelivered(ctx, tx, in.AttendeeID, total,
					entryDate, "session delivered", in.MarkedBy)
			}
			if err != nil {
				return MarkResult{}, err
			}
			journalEntryID = &posted.ID
			recognised = &total
		}

		if err := s.billing.ApplyConsumption(ctx, tx, tenantID, consumeInput, planned, journalEntryID); err != nil {
			return MarkResult{}, err
		}
		creditsCharged = creditCost
	}

	if _, err := tx.Exec(ctx, `
		UPDATE session_attendees
		   SET status = $2::attendance_status, credits_charged = $3,
		       marked_at = now(), marked_by = $4,
		       notes = CASE WHEN $5 = '' THEN notes ELSE $5 END
		 WHERE id = $1`,
		in.AttendeeID, string(in.Status), creditsCharged, in.MarkedBy, in.Notes); err != nil {
		return MarkResult{}, errs.Internal(err, "update attendance")
	}

	return s.result(ctx, tx, in.AttendeeID, clientID, journalEntryID, recognised)
}

// resolvePrevious reconstructs what the state being left behind had charged.
func resolvePrevious(current AttendanceStatus, noShowBillable bool) effect {
	switch current {
	case Completed:
		return effect{billable: true}
	case LateCancel:
		return effect{billable: true, lateCancellation: true}
	case NoShow:
		if noShowBillable {
			return effect{billable: true, lateCancellation: true}
		}
		return effect{}
	default:
		return effect{}
	}
}

func reasonFor(status AttendanceStatus) billing.CreditReason {
	switch status {
	case LateCancel:
		return billing.ReasonLateCancellation
	case NoShow:
		return billing.ReasonNoShow
	default:
		return billing.ReasonSessionCompleted
	}
}

// sumConsumption totals a plan, which may span packs bought at different
// prices.
func sumConsumption(planned []billing.Consumption) (money.Money, error) {
	if len(planned) == 0 {
		return money.Money{}, nil
	}
	total := money.Zero(planned[0].UnitPrice.Currency)
	for _, c := range planned {
		var err error
		if total, err = total.Add(c.Total()); err != nil {
			// Packs in different currencies cannot be summed into one entry.
			// Phase 1 is single-currency per tenant, so this is a guard
			// against a future multi-currency bug, not a reachable state.
			return money.Money{}, errs.Internal(err, "sum credit consumption")
		}
	}
	return total, nil
}

// reverseAttendanceRevenue reverses the journal entry an attendance posted.
func (s *Service) reverseAttendanceRevenue(ctx context.Context, tx pgx.Tx, attendeeID ids.ID, by *ids.ID) (*ids.ID, error) {
	var entryID *ids.ID
	err := tx.QueryRow(ctx, `
		SELECT journal_entry_id FROM credit_transactions
		 WHERE session_attendee_id = $1 AND journal_entry_id IS NOT NULL
		 ORDER BY created_at DESC, server_seq DESC LIMIT 1`, attendeeID).Scan(&entryID)
	if err != nil {
		if db.IsNoRows(err) {
			return nil, nil // nothing was posted, nothing to reverse
		}
		return nil, errs.Internal(err, "find attendance journal entry")
	}
	if entryID == nil {
		return nil, nil
	}

	var alreadyReversed *ids.ID
	if err := tx.QueryRow(ctx,
		`SELECT reversed_by FROM journal_entries WHERE id = $1`, *entryID).Scan(&alreadyReversed); err != nil {
		return nil, errs.Internal(err, "check journal entry")
	}
	if alreadyReversed != nil {
		return alreadyReversed, nil
	}

	reversal, err := s.ledger.Reverse(ctx, tx, *entryID, "attendance changed", by)
	if err != nil {
		return nil, err
	}
	return &reversal.ID, nil
}

type tenantSettings struct {
	AllowOverdraft   bool
	NoShowIsBillable bool
	Currency         string
	BufferMinutes    int
}

func (s *Service) tenantSettings(ctx context.Context, tx pgx.Tx, tenantID ids.ID) (tenantSettings, error) {
	var ts tenantSettings
	if err := tx.QueryRow(ctx,
		`SELECT allow_overdraft, no_show_is_billable, default_currency, buffer_minutes
		   FROM tenants WHERE id = $1`, tenantID,
	).Scan(&ts.AllowOverdraft, &ts.NoShowIsBillable, &ts.Currency, &ts.BufferMinutes); err != nil {
		return tenantSettings{}, errs.Internal(err, "load tenant settings")
	}
	return ts, nil
}

// result assembles the response, including the balance Journey A turns on.
func (s *Service) result(ctx context.Context, tx pgx.Tx, attendeeID, clientID ids.ID, entryID *ids.ID, recognised *money.Money) (MarkResult, error) {
	attendee, err := s.getAttendee(ctx, tx, attendeeID)
	if err != nil {
		return MarkResult{}, err
	}
	balance, err := s.billing.BalanceFor(ctx, tx, clientID)
	if err != nil {
		return MarkResult{}, err
	}
	return MarkResult{
		Attendee:          attendee,
		CreditsRemaining:  balance.Remaining,
		RevenueRecognised: recognised,
		JournalEntryID:    entryID,
	}, nil
}

func (s *Service) getAttendee(ctx context.Context, tx pgx.Tx, attendeeID ids.ID) (Attendee, error) {
	var a Attendee
	var status string
	err := tx.QueryRow(ctx, `
		SELECT sa.id, sa.session_id, sa.client_id, c.full_name, sa.status::text,
		       sa.credits_charged, sa.marked_at, sa.notes, sa.server_seq
		  FROM session_attendees sa
		  JOIN clients c ON c.id = sa.client_id
		 WHERE sa.id = $1`, attendeeID,
	).Scan(&a.ID, &a.SessionID, &a.ClientID, &a.ClientName, &status,
		&a.CreditsCharged, &a.MarkedAt, &a.Notes, &a.ServerSeq)
	if err != nil {
		if db.IsNoRows(err) {
			return Attendee{}, errs.NotFound("session attendee")
		}
		return Attendee{}, errs.Internal(err, "load attendee")
	}
	a.Status = AttendanceStatus(status)
	return a, nil
}

func (s *Service) attendeesFor(ctx context.Context, tx pgx.Tx, sessionID ids.ID) ([]Attendee, error) {
	bySession, err := s.attendeesForMany(ctx, tx, []ids.ID{sessionID})
	if err != nil {
		return nil, err
	}
	return bySession[sessionID], nil
}

func (s *Service) attendeesForMany(ctx context.Context, tx pgx.Tx, sessionIDs []ids.ID) (map[ids.ID][]Attendee, error) {
	rows, err := tx.Query(ctx, `
		SELECT sa.id, sa.session_id, sa.client_id, c.full_name, sa.status::text,
		       sa.credits_charged, sa.marked_at, sa.notes, sa.server_seq
		  FROM session_attendees sa
		  JOIN clients c ON c.id = sa.client_id
		 WHERE sa.session_id = ANY($1)
		 ORDER BY c.full_name`, sessionIDs)
	if err != nil {
		return nil, errs.Internal(err, "load attendees")
	}
	defer rows.Close()

	out := make(map[ids.ID][]Attendee, len(sessionIDs))
	for rows.Next() {
		var a Attendee
		var status string
		if err := rows.Scan(&a.ID, &a.SessionID, &a.ClientID, &a.ClientName, &status,
			&a.CreditsCharged, &a.MarkedAt, &a.Notes, &a.ServerSeq); err != nil {
			return nil, errs.Internal(err, "scan attendee")
		}
		a.Status = AttendanceStatus(status)
		out[a.SessionID] = append(out[a.SessionID], a)
	}
	if err := rows.Err(); err != nil {
		return nil, errs.Internal(err, "read attendees")
	}
	return out, nil
}

// DaySkip records a roster entry that could not be marked.
type DaySkip struct {
	AttendeeID ids.ID `json:"attendee_id"`
	ClientID   ids.ID `json:"client_id"`
	ClientName string `json:"client_name"`
	Reason     string `json:"reason"`
	Message    string `json:"message"`
}

// DayResult is the outcome of a bulk roster check-off.
type DayResult struct {
	Marked []MarkResult `json:"marked"`
	// Skipped names everyone who could not be marked and why. A trainer who
	// taps "mark everyone complete" must not be left believing it worked for
	// a client who was out of credit.
	Skipped []DaySkip `json:"skipped"`
}

// MarkDay applies one status to every scheduled attendee on a given day.
//
// This is the PRD's bulk roster check-off: at the end of a day a trainer
// confirms everyone who showed up in one action, rather than tapping through
// each session. Only attendees still Scheduled are touched, so an outcome
// already recorded by hand is never overwritten.
func (s *Service) MarkDay(ctx context.Context, tx pgx.Tx, tenantID ids.ID, day time.Time, status AttendanceStatus, by *ids.ID) (DayResult, error) {
	dayStart := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.UTC)
	dayEnd := dayStart.AddDate(0, 0, 1)

	rows, err := tx.Query(ctx, `
		SELECT sa.id, sa.client_id, c.full_name
		  FROM session_attendees sa
		  JOIN sessions s ON s.id = sa.session_id
		  JOIN clients c ON c.id = sa.client_id
		 WHERE sa.status = 'scheduled' AND s.status = 'scheduled'
		   AND s.starts_at >= $1 AND s.starts_at < $2
		 ORDER BY s.starts_at, c.full_name`, dayStart, dayEnd)
	if err != nil {
		return DayResult{}, errs.Internal(err, "load day roster")
	}

	type rosterEntry struct {
		attendeeID ids.ID
		clientID   ids.ID
		clientName string
	}
	var roster []rosterEntry
	for rows.Next() {
		var e rosterEntry
		if err := rows.Scan(&e.attendeeID, &e.clientID, &e.clientName); err != nil {
			rows.Close()
			return DayResult{}, errs.Internal(err, "scan roster")
		}
		roster = append(roster, e)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return DayResult{}, errs.Internal(err, "read day roster")
	}

	result := DayResult{
		Marked:  make([]MarkResult, 0, len(roster)),
		Skipped: []DaySkip{},
	}

	for _, entry := range roster {
		// Each attendee is marked in its own savepoint. A client who is out of
		// credit should not roll back the rest of the day's roster, but the
		// failed attempt must leave nothing behind either.
		if _, err := tx.Exec(ctx, "SAVEPOINT mark_attendee"); err != nil {
			return DayResult{}, errs.Internal(err, "open savepoint")
		}

		marked, err := s.Mark(ctx, tx, tenantID, MarkInput{
			AttendeeID: entry.attendeeID, Status: status, MarkedBy: by,
		})
		if err == nil {
			if _, relErr := tx.Exec(ctx, "RELEASE SAVEPOINT mark_attendee"); relErr != nil {
				return DayResult{}, errs.Internal(relErr, "release savepoint")
			}
			result.Marked = append(result.Marked, marked)
			continue
		}

		if _, rbErr := tx.Exec(ctx, "ROLLBACK TO SAVEPOINT mark_attendee"); rbErr != nil {
			return DayResult{}, errs.Internal(rbErr, "roll back savepoint")
		}

		// Out of credit or a refused transition is that attendee's problem and
		// is reported; anything else is a genuine fault and aborts the batch.
		code := errs.CodeOf(err)
		if code != errs.CodeInsufficientCredits && code != errs.CodeInvalidTransition {
			return DayResult{}, err
		}
		result.Skipped = append(result.Skipped, DaySkip{
			AttendeeID: entry.attendeeID,
			ClientID:   entry.clientID,
			ClientName: entry.clientName,
			Reason:     code,
			Message:    messageOf(err),
		})
	}
	return result, nil
}

// messageOf extracts the caller-facing message from a typed error.
func messageOf(err error) string {
	var appErr *errs.Error
	if errors.As(err, &appErr) {
		return appErr.Message
	}
	return err.Error()
}
