package sync

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/NewMux/mtdrb_go/internal/billing"
	"github.com/NewMux/mtdrb_go/internal/crm"
	"github.com/NewMux/mtdrb_go/internal/ledger"
	"github.com/NewMux/mtdrb_go/internal/platform/errs"
	"github.com/NewMux/mtdrb_go/internal/platform/ids"
	"github.com/NewMux/mtdrb_go/internal/platform/money"
	"github.com/NewMux/mtdrb_go/internal/programming"
	"github.com/NewMux/mtdrb_go/internal/scheduling"
)

// Push carries operations, not rows.
//
// This is the load-bearing decision of the whole sync design. A generic row
// upsert would be far less code: the client sends a session_attendees row with
// status 'completed', the server writes it, done. It would also be wrong.
// Marking attendance burns a credit and posts revenue; writing the row
// directly does neither, so the calendar would say the session happened while
// the books said nothing was earned and the client's balance never moved.
//
// So the outbox replays *intents* — "mark this attendance", "log this set",
// "record this payment" — and each dispatches to the same service the online
// path uses. The rules cannot be bypassed because there is no route around
// them.

// OperationType names an intent the outbox can replay.
type OperationType string

const (
	// The PRD's offline set: log workouts, mark attendance, record cash.
	OpMarkAttendance  OperationType = "attendance.mark"
	OpStartWorkout    OperationType = "workout.start"
	OpLogSet          OperationType = "workout.log_set"
	OpCompleteWorkout OperationType = "workout.complete"
	OpRecordPayment   OperationType = "payment.record"

	// Cheap to support offline and routinely done on a gym floor.
	OpCreateClient     OperationType = "client.create"
	OpUpdateClient     OperationType = "client.update"
	OpRecordBiometrics OperationType = "biometrics.record"
)

// Operation is one queued intent from a client's outbox.
type Operation struct {
	// ID is minted by the client, so a replay is recognisable as the same
	// operation rather than a new one.
	ID   ids.ID          `json:"id"`
	Type OperationType   `json:"type"`
	Data json.RawMessage `json:"data"`
	// QueuedAt is when the device recorded it, used to apply a batch in the
	// order the trainer actually did things rather than the order they synced.
	QueuedAt time.Time `json:"queued_at"`
}

// OperationResult reports what became of one operation.
type OperationResult struct {
	ID      ids.ID          `json:"id"`
	Type    OperationType   `json:"type"`
	Status  string          `json:"status"` // applied | conflict | rejected
	Result  json.RawMessage `json:"result,omitempty"`
	Code    string          `json:"code,omitempty"`
	Message string          `json:"message,omitempty"`
}

// PushResult is the outcome of a batch.
type PushResult struct {
	Results []OperationResult `json:"results"`
	Applied int               `json:"applied"`
	// Conflicts are operations the server refused on business grounds — out of
	// credit, invoice already settled. The client surfaces these to the
	// trainer rather than resolving them, because a machine guessing at what
	// someone meant to do with money is worse than asking.
	Conflicts int `json:"conflicts"`
	Rejected  int `json:"rejected"`
	// Cursor is the opaque pull cursor as of this push, so a client can pull
	// its own writes back without an extra round trip.
	Cursor string `json:"cursor"`
}

// Dependencies are the services push dispatches into.
type Dependencies struct {
	CRM         *crm.Service
	Scheduling  *scheduling.Service
	Billing     *billing.Service
	Programming *programming.Service
}

// maxBatchSize bounds one push. A device that has been offline for a week
// syncs in several batches rather than one request the server must hold whole.
const maxBatchSize = 200

// Push applies a batch of queued operations.
//
// Each operation runs in its own savepoint. One client being out of credit
// must not discard the eleven other sessions the trainer logged that morning,
// and a failed operation must leave nothing behind.
//
// Ordering matters and is respected: operations apply in the order the device
// queued them, because "start workout" then "log set" is not the same as the
// reverse, and two attendance marks on one session resolve to the last thing
// the trainer actually decided.
func (s *Service) Push(ctx context.Context, tx pgx.Tx, tenantID ids.ID, deps Dependencies, ops []Operation, actor *ids.ID) (PushResult, error) {
	if len(ops) > maxBatchSize {
		return PushResult{}, errs.Invalid(errs.CodeValidation,
			"a push may carry at most %d operations, got %d", maxBatchSize, len(ops)).
			WithField("operations", "too many in one batch")
	}

	sorted := make([]Operation, len(ops))
	copy(sorted, ops)
	stableSortByQueuedAt(sorted)

	result := PushResult{Results: make([]OperationResult, 0, len(sorted))}

	for _, op := range sorted {
		if _, err := tx.Exec(ctx, "SAVEPOINT sync_op"); err != nil {
			return PushResult{}, errs.Internal(err, "open savepoint")
		}

		payload, err := s.apply(ctx, tx, tenantID, deps, op, actor)
		if err == nil {
			if _, relErr := tx.Exec(ctx, "RELEASE SAVEPOINT sync_op"); relErr != nil {
				return PushResult{}, errs.Internal(relErr, "release savepoint")
			}
			result.Results = append(result.Results, OperationResult{
				ID: op.ID, Type: op.Type, Status: "applied", Result: payload,
			})
			result.Applied++
			continue
		}

		if _, rbErr := tx.Exec(ctx, "ROLLBACK TO SAVEPOINT sync_op"); rbErr != nil {
			return PushResult{}, errs.Internal(rbErr, "roll back savepoint")
		}

		// An internal failure is the server's problem, not the client's, so it
		// aborts the batch rather than being reported back as if the trainer
		// had done something wrong.
		kind := errs.KindOf(err)
		if kind == errs.KindInternal {
			return PushResult{}, err
		}

		status := "rejected"
		if kind == errs.KindConflict || kind == errs.KindUnprocessed {
			status = "conflict"
			result.Conflicts++
		} else {
			result.Rejected++
		}
		result.Results = append(result.Results, OperationResult{
			ID: op.ID, Type: op.Type, Status: status,
			Code: errs.CodeOf(err), Message: messageOf(err),
		})
	}

	cursor, err := s.Checkpoint(ctx, tx)
	if err != nil {
		return PushResult{}, err
	}
	result.Cursor = cursor.Encode()
	return result, nil
}

// apply dispatches one operation to the service that owns it.
func (s *Service) apply(ctx context.Context, tx pgx.Tx, tenantID ids.ID, deps Dependencies, op Operation, actor *ids.ID) (json.RawMessage, error) {
	switch op.Type {

	case OpMarkAttendance:
		var in struct {
			AttendeeID     ids.ID                      `json:"attendee_id"`
			Status         scheduling.AttendanceStatus `json:"status"`
			Notes          string                      `json:"notes"`
			AllowOverdraft bool                        `json:"allow_overdraft"`
		}
		if err := decode(op, &in); err != nil {
			return nil, err
		}
		out, err := deps.Scheduling.Mark(ctx, tx, tenantID, scheduling.MarkInput{
			AttendeeID: in.AttendeeID, Status: in.Status, Notes: in.Notes,
			MarkedBy: actor, AllowOverdraft: in.AllowOverdraft,
		})
		return encode(out, err)

	case OpStartWorkout:
		var in struct {
			ID           *ids.ID    `json:"id"`
			ClientID     ids.ID     `json:"client_id"`
			AssignmentID *ids.ID    `json:"assignment_id"`
			DayID        *ids.ID    `json:"day_id"`
			SessionID    *ids.ID    `json:"session_id"`
			WeekNumber   int        `json:"week_number"`
			PerformedOn  *time.Time `json:"performed_on"`
			Notes        string     `json:"notes"`
		}
		if err := decode(op, &in); err != nil {
			return nil, err
		}
		start := programming.StartWorkoutInput{
			ClientID: in.ClientID, AssignmentID: in.AssignmentID, DayID: in.DayID,
			SessionID: in.SessionID, WeekNumber: in.WeekNumber, Notes: in.Notes,
		}
		if in.PerformedOn != nil {
			start.PerformedOn = *in.PerformedOn
		}
		out, err := deps.Programming.StartWorkout(ctx, tx, tenantID, start)
		return encode(out, err)

	case OpLogSet:
		var in struct {
			WorkoutID         ids.ID  `json:"workout_id"`
			ExerciseID        ids.ID  `json:"exercise_id"`
			ProgramExerciseID *ids.ID `json:"program_exercise_id"`
			SetIndex          int     `json:"set_index"`
			Reps              *int    `json:"reps"`
			LoadGrams         *int    `json:"load_grams"`
			RPETenths         *int    `json:"rpe_tenths"`
			RIR               *int    `json:"rir"`
			RestSeconds       *int    `json:"rest_seconds"`
			Tempo             string  `json:"tempo"`
			IsWarmup          bool    `json:"is_warmup"`
			Completed         *bool   `json:"completed"`
			Notes             string  `json:"notes"`
			FormCheckMediaID  *ids.ID `json:"form_check_media_id"`
		}
		if err := decode(op, &in); err != nil {
			return nil, err
		}
		// LogSet upserts on (workout, exercise, set index), so replaying this
		// converges on the same set rather than stacking duplicates.
		out, err := deps.Programming.LogSet(ctx, tx, tenantID, programming.LogSetInput{
			WorkoutID: in.WorkoutID, ExerciseID: in.ExerciseID,
			ProgramExerciseID: in.ProgramExerciseID, SetIndex: in.SetIndex,
			Reps: in.Reps, LoadGrams: in.LoadGrams, RPETenths: in.RPETenths,
			RIR: in.RIR, RestSeconds: in.RestSeconds, Tempo: in.Tempo,
			IsWarmup: in.IsWarmup, Completed: in.Completed, Notes: in.Notes,
			FormCheckMediaID: in.FormCheckMediaID,
		})
		return encode(out, err)

	case OpCompleteWorkout:
		var in struct {
			WorkoutID ids.ID `json:"workout_id"`
			Notes     string `json:"notes"`
		}
		if err := decode(op, &in); err != nil {
			return nil, err
		}
		out, err := deps.Programming.CompleteWorkout(ctx, tx, in.WorkoutID, in.Notes)
		return encode(out, err)

	case OpRecordPayment:
		var in struct {
			InvoiceID   ids.ID            `json:"invoice_id"`
			AmountMinor int64             `json:"amount_minor"`
			Currency    string            `json:"currency"`
			Instrument  ledger.Instrument `json:"instrument"`
			ReceivedOn  *time.Time        `json:"received_on"`
			Reference   string            `json:"reference"`
			Notes       string            `json:"notes"`
		}
		if err := decode(op, &in); err != nil {
			return nil, err
		}
		// Cash recorded in a basement is the case ADR 0004 is about. It is
		// never last-write-wins: an invoice already settled by another device
		// comes back as a conflict for the trainer to look at.
		pay := billing.RecordPaymentInput{
			InvoiceID:  in.InvoiceID,
			Amount:     money.New(in.AmountMinor, in.Currency),
			Instrument: in.Instrument, Reference: in.Reference,
			Notes: in.Notes, RecordedBy: actor,
		}
		if in.ReceivedOn != nil {
			pay.ReceivedOn = *in.ReceivedOn
		}
		out, err := deps.Billing.RecordPayment(ctx, tx, tenantID, pay)
		return encode(out, err)

	case OpCreateClient:
		var in struct {
			FullName    string     `json:"full_name"`
			Email       *string    `json:"email"`
			Phone       *string    `json:"phone"`
			DateOfBirth *time.Time `json:"date_of_birth"`
			Notes       string     `json:"notes"`
		}
		if err := decode(op, &in); err != nil {
			return nil, err
		}
		out, err := deps.CRM.Create(ctx, tx, tenantID, crm.CreateInput{
			FullName: in.FullName, Email: in.Email, Phone: in.Phone,
			DateOfBirth: in.DateOfBirth, Notes: in.Notes,
		})
		return encode(out, err)

	case OpUpdateClient:
		var in struct {
			ClientID ids.ID  `json:"client_id"`
			FullName *string `json:"full_name"`
			Notes    *string `json:"notes"`
		}
		if err := decode(op, &in); err != nil {
			return nil, err
		}
		// Profile edits are the one place ADR 0004's last-write-wins applies:
		// losing a redundant note is acceptable, blocking the trainer is not.
		out, err := deps.CRM.Update(ctx, tx, in.ClientID, crm.UpdateInput{
			FullName: in.FullName, Notes: in.Notes,
		})
		return encode(out, err)

	case OpRecordBiometrics:
		var in struct {
			ClientID       ids.ID           `json:"client_id"`
			MeasuredOn     *time.Time       `json:"measured_on"`
			WeightGrams    *int32           `json:"weight_grams"`
			BodyFatBP      *int32           `json:"body_fat_bp"`
			Circumferences map[string]int32 `json:"circumferences"`
			Notes          string           `json:"notes"`
		}
		if err := decode(op, &in); err != nil {
			return nil, err
		}
		bio := crm.BiometricInput{
			ClientID: in.ClientID, WeightGrams: in.WeightGrams,
			BodyFatBP: in.BodyFatBP, Circumferences: in.Circumferences, Notes: in.Notes,
		}
		if in.MeasuredOn != nil {
			bio.MeasuredOn = *in.MeasuredOn
		}
		out, err := deps.CRM.RecordBiometrics(ctx, tx, tenantID, bio)
		return encode(out, err)

	default:
		return nil, errs.Invalid(errs.CodeValidation,
			"unknown sync operation %q", op.Type).
			WithField("type", "is not an operation this server knows how to apply")
	}
}

func decode(op Operation, dst any) error {
	if len(op.Data) == 0 {
		return errs.Invalid(errs.CodeValidation, "operation %s carries no data", op.Type)
	}
	if err := json.Unmarshal(op.Data, dst); err != nil {
		return errs.Invalid(errs.CodeValidation,
			"operation %s has a malformed payload: %s", op.Type, err.Error())
	}
	return nil
}

func encode(out any, err error) (json.RawMessage, error) {
	if err != nil {
		return nil, err
	}
	raw, marshalErr := json.Marshal(out)
	if marshalErr != nil {
		return nil, errs.Internal(marshalErr, "encode operation result")
	}
	return raw, nil
}

// messageOf extracts the caller-facing message, so a conflict tells the
// trainer what actually went wrong rather than "operation failed".
func messageOf(err error) string {
	var appErr *errs.Error
	if errors.As(err, &appErr) {
		return appErr.Message
	}
	return err.Error()
}

// stableSortByQueuedAt orders a batch the way the device recorded it.
//
// Stable, so operations queued in the same millisecond keep the order the
// client sent them: "start workout" then "log set" must not be reversed by a
// tie in the timestamp.
func stableSortByQueuedAt(ops []Operation) {
	for i := 1; i < len(ops); i++ {
		for j := i; j > 0 && ops[j].QueuedAt.Before(ops[j-1].QueuedAt); j-- {
			ops[j], ops[j-1] = ops[j-1], ops[j]
		}
	}
}
