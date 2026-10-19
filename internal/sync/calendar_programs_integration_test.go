//go:build integration

package sync_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/NewMux/mtdrb_go/internal/billing"
	"github.com/NewMux/mtdrb_go/internal/crm"
	"github.com/NewMux/mtdrb_go/internal/platform/ids"
	"github.com/NewMux/mtdrb_go/internal/platform/money"
	"github.com/NewMux/mtdrb_go/internal/sync"
)

// ---------------------------------------------------------------------------
// The calendar, offline
// ---------------------------------------------------------------------------

func (f fixture) sessionType(t *testing.T) ids.ID {
	t.Helper()
	var id ids.ID
	if err := f.tx(t, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `
			INSERT INTO session_types (id, tenant_id, name, duration_minutes, capacity, credit_cost)
			VALUES ($1, $2, '1-on-1', 60, 2, 1) RETURNING id`, ids.New(), f.tenantID).Scan(&id)
	}); err != nil {
		t.Fatalf("seed session type: %v", err)
	}
	return id
}

func applied(t *testing.T, r sync.PushResult) {
	t.Helper()
	for _, res := range r.Results {
		if res.Status != "applied" {
			t.Fatalf("%s was %s: %s (%s)", res.Type, res.Status, res.Message, res.Code)
		}
	}
}

// A session booked with no signal is marked attended with no signal, and both
// reach the server in one batch naming the rows the device minted — and the
// mark burns a credit exactly as it would online.
func TestBookAndMarkInOneOfflineBatch(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	typeID := f.sessionType(t)
	sessionID, attendeeID := ids.New(), ids.New()
	at := may1.Add(9 * time.Hour)

	// The client and their pack were synced before the trainer lost signal.
	var clientID ids.ID
	if err := f.tx(t, func(tx pgx.Tx) error {
		client, err := f.deps.CRM.Create(ctx, tx, f.tenantID, crm.CreateInput{FullName: "Dana Rivers"})
		if err != nil {
			return err
		}
		clientID = client.ID
		_, err = f.deps.Billing.Grant(ctx, tx, f.tenantID, billing.GrantInput{
			ClientID: clientID, Name: "10-pack", Credits: 10,
			UnitPrice: money.New(5000, "EUR"), PurchasedOn: may1,
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	applied(t, f.push(t,
		op(t, sync.OpBookSession, may1, map[string]any{
			"id": sessionID, "session_type_id": typeID, "starts_at": at,
			"client_ids": []ids.ID{clientID}, "attendee_ids": []ids.ID{attendeeID}, "location": "Studio",
		}),
		op(t, sync.OpMarkAttendance, may1.Add(time.Second), map[string]any{
			"attendee_id": attendeeID, "status": "completed",
		}),
	))

	var status string
	var startsAt time.Time
	var remaining int
	if err := f.tx(t, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `
			SELECT sa.status::text, s.starts_at FROM session_attendees sa JOIN sessions s ON s.id = sa.session_id
			 WHERE sa.id = $1 AND s.id = $2`, attendeeID, sessionID).Scan(&status, &startsAt); err != nil {
			return err
		}
		balance, err := f.deps.Billing.BalanceFor(ctx, tx, clientID)
		remaining = balance.Remaining
		return err
	}); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if status != "completed" || !startsAt.Equal(at) || remaining != 9 {
		t.Fatalf("got %s at %v with %d credits, want completed at %v with 9", status, startsAt, remaining, at)
	}
}

func TestReplayingABookingConverges(t *testing.T) {
	f := setup(t)
	typeID := f.sessionType(t)
	clientID := ids.New()
	f.push(t, op(t, sync.OpCreateClient, may1, map[string]any{"id": clientID, "full_name": "Leo Martins"}))

	book := op(t, sync.OpBookSession, may1, map[string]any{
		"id": ids.New(), "session_type_id": typeID, "starts_at": may1.Add(10 * time.Hour),
		"client_ids": []ids.ID{clientID}, "attendee_ids": []ids.ID{ids.New()},
	})
	applied(t, f.push(t, book))
	applied(t, f.push(t, book))

	var n int
	if err := f.tx(t, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT count(*) FROM sessions`).Scan(&n)
	}); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("a replayed booking made %d sessions, want 1", n)
	}
}

// Two devices booking the same slot offline both pass their local check; the
// overlap constraint is what decides, and the loser hears about it.
func TestAClashingBookingComesBackAsAConflict(t *testing.T) {
	f := setup(t)
	typeID := f.sessionType(t)
	a, b := ids.New(), ids.New()
	f.push(t,
		op(t, sync.OpCreateClient, may1, map[string]any{"id": a, "full_name": "A"}),
		op(t, sync.OpCreateClient, may1.Add(time.Second), map[string]any{"id": b, "full_name": "B"}),
	)
	r := f.push(t,
		op(t, sync.OpBookSession, may1, map[string]any{
			"id": ids.New(), "session_type_id": typeID, "starts_at": may1.Add(9 * time.Hour),
			"client_ids": []ids.ID{a}, "attendee_ids": []ids.ID{ids.New()},
		}),
		op(t, sync.OpBookSession, may1.Add(time.Second), map[string]any{
			"id": ids.New(), "session_type_id": typeID, "starts_at": may1.Add(9*time.Hour + 30*time.Minute),
			"client_ids": []ids.ID{b}, "attendee_ids": []ids.ID{ids.New()},
		}),
	)
	if r.Results[0].Status != "applied" || r.Results[1].Status != "conflict" || r.Results[1].Code != "scheduling_conflict" {
		t.Fatalf("results = %+v", r.Results)
	}
}

func TestRescheduleThenCancelAndReplayTheCancel(t *testing.T) {
	f := setup(t)
	typeID := f.sessionType(t)
	clientID, sessionID := ids.New(), ids.New()
	f.push(t,
		op(t, sync.OpCreateClient, may1, map[string]any{"id": clientID, "full_name": "Priya"}),
		op(t, sync.OpBookSession, may1.Add(time.Second), map[string]any{
			"id": sessionID, "session_type_id": typeID, "starts_at": may1.Add(9 * time.Hour),
			"client_ids": []ids.ID{clientID}, "attendee_ids": []ids.ID{ids.New()},
		}),
	)

	moved := may1.Add(26 * time.Hour)
	cancel := op(t, sync.OpCancelSession, may1.Add(2*time.Second), map[string]any{"session_id": sessionID})
	applied(t, f.push(t,
		op(t, sync.OpRescheduleSession, may1.Add(time.Second), map[string]any{"session_id": sessionID, "starts_at": moved}),
		cancel,
	))
	// The outbox may resend a cancel whose answer it never received.
	applied(t, f.push(t, cancel))

	var status string
	var startsAt, endsAt time.Time
	if err := f.tx(t, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(),
			`SELECT status::text, starts_at, ends_at FROM sessions WHERE id = $1`, sessionID).Scan(&status, &startsAt, &endsAt)
	}); err != nil {
		t.Fatal(err)
	}
	if status != "cancelled" || !startsAt.Equal(moved) || endsAt.Sub(startsAt) != time.Hour {
		t.Fatalf("got %s %v–%v", status, startsAt, endsAt)
	}
}

// ---------------------------------------------------------------------------
// Programmes, offline
// ---------------------------------------------------------------------------

func (f fixture) exercise(t *testing.T, name string) ids.ID {
	t.Helper()
	var id ids.ID
	if err := f.tx(t, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT id FROM exercises WHERE name = $1`, name).Scan(&id)
	}); err != nil {
		t.Fatalf("exercise %s: %v", name, err)
	}
	return id
}

type rx = map[string]any

func programDoc(id, blockID ids.ID, name string, days ...map[string]any) map[string]any {
	return map[string]any{
		"id": id, "name": name, "description": "", "is_template": false,
		"blocks": []map[string]any{{"id": blockID, "name": "Block 1", "weeks": 4, "sort_order": 0, "notes": "", "days": days}},
	}
}

func day(id ids.ID, name string, prescriptions ...rx) map[string]any {
	if prescriptions == nil {
		prescriptions = []rx{}
	}
	return map[string]any{"id": id, "name": name, "sort_order": 0, "notes": "", "prescriptions": prescriptions}
}

// A programme built offline, assigned, and trained from — all in one batch —
// then edited. A day removed from the plan takes its prescriptions with it,
// but never the workouts a client logged against it.
func TestProgramBuiltAssignedAndEditedOffline(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	squat, bench := f.exercise(t, "Back Squat"), f.exercise(t, "Bench Press")
	clientID, programID, blockID := ids.New(), ids.New(), ids.New()
	dayA, dayB, rxA, rxB := ids.New(), ids.New(), ids.New(), ids.New()
	assignmentID, workoutID := ids.New(), ids.New()

	five := 5
	r := f.push(t,
		op(t, sync.OpCreateClient, may1, map[string]any{"id": clientID, "full_name": "Dana"}),
		op(t, sync.OpSaveProgram, may1.Add(time.Second), programDoc(programID, blockID, "Strength",
			day(dayA, "Day A", rx{"id": rxA, "exercise_id": squat, "target_sets": 5, "target_reps_min": five, "target_reps_max": five}),
			day(dayB, "Day B", rx{"id": rxB, "exercise_id": bench, "target_sets": 3}),
		)),
		op(t, sync.OpAssignProgram, may1.Add(2*time.Second), map[string]any{
			"id": assignmentID, "program_id": programID, "client_id": clientID, "starts_on": "2026-05-01",
		}),
		op(t, sync.OpStartWorkout, may1.Add(3*time.Second), map[string]any{
			"id": workoutID, "client_id": clientID, "assignment_id": assignmentID, "day_id": dayB, "week_number": 1,
		}),
		op(t, sync.OpLogSet, may1.Add(4*time.Second), map[string]any{
			"workout_id": workoutID, "exercise_id": bench, "program_exercise_id": rxB, "set_index": 1, "reps": 8, "load_grams": 60000,
		}),
	)
	applied(t, r)

	// Rename, and drop Day B.
	applied(t, f.push(t, op(t, sync.OpSaveProgram, may1.Add(5*time.Second), programDoc(programID, blockID, "Strength v2",
		day(dayA, "Day A", rx{"id": rxA, "exercise_id": squat, "target_sets": 5}),
	))))

	var days, prescriptions, sets int
	var name string
	var workoutDay *ids.ID
	if err := f.tx(t, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT name FROM programs WHERE id = $1`, programID).Scan(&name); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM program_days`).Scan(&days); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM program_exercises`).Scan(&prescriptions); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT day_id FROM workout_sessions WHERE id = $1`, workoutID).Scan(&workoutDay); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `SELECT count(*) FROM set_logs WHERE workout_session_id = $1`, workoutID).Scan(&sets)
	}); err != nil {
		t.Fatal(err)
	}
	if name != "Strength v2" || days != 1 || prescriptions != 1 {
		t.Fatalf("after edit: name %q, %d days, %d prescriptions", name, days, prescriptions)
	}
	if workoutDay != nil || sets != 1 {
		t.Fatalf("the logged workout must survive with its day cleared: day %v, %d sets", workoutDay, sets)
	}

	// Replaying the edit converges.
	applied(t, f.push(t, op(t, sync.OpSaveProgram, may1.Add(6*time.Second), programDoc(programID, blockID, "Strength v2",
		day(dayA, "Day A", rx{"id": rxA, "exercise_id": squat, "target_sets": 5}),
	))))
}

// The structure travels with the programme row, and an edit anywhere inside
// it moves the row's sequence so other devices hear about it.
func TestPullCarriesProgramStructure(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	squat := f.exercise(t, "Back Squat")
	programID, blockID, dayA := ids.New(), ids.New(), ids.New()

	applied(t, f.push(t, op(t, sync.OpSaveProgram, may1, programDoc(programID, blockID, "Plan",
		day(dayA, "Day A", rx{"id": ids.New(), "exercise_id": squat, "target_sets": 4, "rest_seconds": 120}),
	))))

	var cursor sync.Cursor
	pullPrograms := func() []json.RawMessage {
		var out []json.RawMessage
		if err := f.tx(t, func(tx pgx.Tx) error {
			res, err := f.sync.Pull(ctx, tx, cursor, 0)
			if err != nil {
				return err
			}
			c, err := sync.DecodeCursor(res.Cursor)
			if err != nil {
				return err
			}
			cursor = c
			for _, ch := range res.Changes {
				if ch.Collection == "programs" {
					out = append(out, ch.Rows...)
				}
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		return out
	}

	rows := pullPrograms()
	if len(rows) != 1 {
		t.Fatalf("pulled %d programmes, want 1", len(rows))
	}
	var got struct {
		Structure struct {
			Blocks []struct {
				Weeks int `json:"weeks"`
				Days  []struct {
					Name          string `json:"name"`
					Prescriptions []struct {
						ExerciseID  ids.ID `json:"exercise_id"`
						TargetSets  int    `json:"target_sets"`
						RestSeconds int    `json:"rest_seconds"`
					} `json:"prescriptions"`
				} `json:"days"`
			} `json:"blocks"`
		} `json:"structure"`
	}
	if err := json.Unmarshal(rows[0], &got); err != nil {
		t.Fatal(err)
	}
	b := got.Structure.Blocks
	if len(b) != 1 || b[0].Weeks != 4 || len(b[0].Days) != 1 || b[0].Days[0].Name != "Day A" ||
		len(b[0].Days[0].Prescriptions) != 1 || b[0].Days[0].Prescriptions[0].ExerciseID != squat ||
		b[0].Days[0].Prescriptions[0].RestSeconds != 120 {
		t.Fatalf("structure = %s", rows[0])
	}

	if again := pullPrograms(); len(again) != 0 {
		t.Fatalf("an unchanged programme came back %d times", len(again))
	}

	// Change only a prescription deep inside; the programme must come back.
	applied(t, f.push(t, op(t, sync.OpSaveProgram, may1.Add(time.Second), programDoc(programID, blockID, "Plan",
		day(dayA, "Day A", rx{"id": ids.New(), "exercise_id": squat, "target_sets": 5}),
	))))
	if again := pullPrograms(); len(again) != 1 {
		t.Fatalf("a structural edit did not reach the next pull")
	}
}

// Ids are the device's, so a device could name a row that belongs to another
// programme. That is refused, never quietly moved.
func TestProgramSaveRefusesAnotherProgrammesRows(t *testing.T) {
	f := setup(t)
	first, second, sharedBlock := ids.New(), ids.New(), ids.New()
	applied(t, f.push(t, op(t, sync.OpSaveProgram, may1, programDoc(first, sharedBlock, "First"))))

	r := f.push(t, op(t, sync.OpSaveProgram, may1.Add(time.Second), programDoc(second, sharedBlock, "Second")))
	if r.Results[0].Status != "conflict" || r.Results[0].Code != "id_already_used" {
		t.Fatalf("result = %+v", r.Results[0])
	}
}

func TestProgramSaveValidatesTheWholeDocumentFirst(t *testing.T) {
	f := setup(t)
	squat := f.exercise(t, "Back Squat")
	eight, six := 8, 6
	r := f.push(t, op(t, sync.OpSaveProgram, may1, programDoc(ids.New(), ids.New(), "Bad",
		day(ids.New(), "Day A", rx{"id": ids.New(), "exercise_id": squat, "target_reps_min": eight, "target_reps_max": six}),
	)))
	if r.Results[0].Status != "rejected" {
		t.Fatalf("an inverted rep range was %s", r.Results[0].Status)
	}
	var n int
	if err := f.tx(t, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT count(*) FROM programs`).Scan(&n)
	}); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("a refused save left %d programmes behind", n)
	}
}
