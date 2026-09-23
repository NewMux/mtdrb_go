//go:build integration

package sync_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/NewMux/mtdrb_go/internal/billing"
	"github.com/NewMux/mtdrb_go/internal/crm"
	"github.com/NewMux/mtdrb_go/internal/db"
	"github.com/NewMux/mtdrb_go/internal/ledger"
	"github.com/NewMux/mtdrb_go/internal/platform/clock"
	"github.com/NewMux/mtdrb_go/internal/platform/ids"
	"github.com/NewMux/mtdrb_go/internal/platform/money"
	"github.com/NewMux/mtdrb_go/internal/programming"
	"github.com/NewMux/mtdrb_go/internal/scheduling"
	"github.com/NewMux/mtdrb_go/internal/sync"
	"github.com/NewMux/mtdrb_go/internal/testsupport"
)

var may1 = time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)

type fixture struct {
	sync     *sync.Service
	deps     sync.Dependencies
	ledger   *ledger.Service
	pool     *db.Pool
	tenantID ids.ID
}

func setup(t *testing.T) fixture {
	t.Helper()
	testsupport.RequireDB(t)
	testsupport.Reset(t)

	pool := testsupport.OpenApp(t)
	c := &clock.Fixed{T: may1}
	ledgerSvc := ledger.NewService(c)
	billingSvc := billing.NewService(ledgerSvc, c)
	programmingSvc := programming.NewService(c)
	crmSvc := crm.NewService(c, []byte("test-column-encryption-key-32byt"))
	schedulingSvc := scheduling.NewService(billingSvc, ledgerSvc, c)

	tenantID := ids.New()
	ctx := context.Background()
	owner := testsupport.OpenOwner(t)
	if _, err := owner.Raw().Exec(ctx, `
		INSERT INTO tenants (id, name, default_currency, buffer_minutes)
		VALUES ($1, 'Iron Works', 'EUR', 15)`, tenantID); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}

	f := fixture{
		sync:   sync.NewService(c),
		ledger: ledgerSvc,
		deps: sync.Dependencies{
			CRM: crmSvc, Scheduling: schedulingSvc,
			Billing: billingSvc, Programming: programmingSvc,
		},
		pool:     pool,
		tenantID: tenantID,
	}
	if err := f.tx(t, func(tx pgx.Tx) error {
		if err := ledgerSvc.SeedChartOfAccounts(ctx, tx, tenantID, "EUR"); err != nil {
			return err
		}
		return programmingSvc.SeedLibrary(ctx, tx, tenantID)
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return f
}

func (f fixture) tx(t *testing.T, fn func(tx pgx.Tx) error) error {
	t.Helper()
	return f.pool.InTenantTx(context.Background(), f.tenantID, fn)
}

func op(t *testing.T, typ sync.OperationType, queuedAt time.Time, data any) sync.Operation {
	t.Helper()
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("encode operation: %v", err)
	}
	return sync.Operation{ID: ids.New(), Type: typ, QueuedAt: queuedAt, Data: raw}
}

func (f fixture) push(t *testing.T, ops ...sync.Operation) sync.PushResult {
	t.Helper()
	var result sync.PushResult
	if err := f.tx(t, func(tx pgx.Tx) error {
		var err error
		result, err = f.sync.Push(context.Background(), tx, f.tenantID, f.deps, ops, nil)
		return err
	}); err != nil {
		t.Fatalf("push: %v", err)
	}
	return result
}

// ---------------------------------------------------------------------------
// Pull
// ---------------------------------------------------------------------------

func TestPullReturnsChangesAndAdvancesTheCursor(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	// A first sync from zero picks up the seeded library.
	var first sync.PullResult
	if err := f.tx(t, func(tx pgx.Tx) error {
		var err error
		first, err = f.sync.Pull(ctx, tx, nil, 0)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if first.Cursor == "" {
		t.Fatal("the cursor did not advance on a first pull")
	}
	byCollection := map[string]int{}
	for _, c := range first.Changes {
		byCollection[c.Collection] = len(c.Rows)
	}
	if byCollection["exercises"] == 0 {
		t.Error("the seeded exercise library did not come through")
	}

	// Nothing has changed since, so a second pull is empty but keeps the
	// cursor — an idle client must not be told to replay history.
	var second sync.PullResult
	if err := f.tx(t, func(tx pgx.Tx) error {
		var err error
		cursor, decodeErr := sync.DecodeCursor(first.Cursor)
		if decodeErr != nil {
			return decodeErr
		}
		second, err = f.sync.Pull(ctx, tx, cursor, 0)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	for _, c := range second.Changes {
		if len(c.Rows) > 0 {
			t.Errorf("an unchanged pull returned %d rows in %s", len(c.Rows), c.Collection)
		}
	}
	if second.Cursor != first.Cursor {
		t.Errorf("cursor moved on an empty pull:\n  %s\n  %s", first.Cursor, second.Cursor)
	}

	// A new client shows up in the next pull.
	if err := f.tx(t, func(tx pgx.Tx) error {
		_, err := f.deps.CRM.Create(ctx, tx, f.tenantID, crm.CreateInput{FullName: "Dana"})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	var third sync.PullResult
	if err := f.tx(t, func(tx pgx.Tx) error {
		var err error
		cursor, decodeErr := sync.DecodeCursor(second.Cursor)
		if decodeErr != nil {
			return decodeErr
		}
		third, err = f.sync.Pull(ctx, tx, cursor, 0)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range third.Changes {
		if c.Collection == "clients" && len(c.Rows) == 1 {
			found = true
		}
	}
	if !found {
		t.Error("a newly created client did not appear in an incremental pull")
	}
	if third.Cursor == second.Cursor {
		t.Error("the cursor did not advance after a change")
	}
}

// Session types used to be sent whole on a first sync only, so a type created
// afterwards never reached the device and every booking against it rendered
// without a name. A device still holding the old "delivered" marker must heal
// on its next pull.
func TestSessionTypesCreatedAfterFirstSyncReachTheDevice(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	pull := func(cursor sync.Cursor) sync.PullResult {
		t.Helper()
		var result sync.PullResult
		if err := f.tx(t, func(tx pgx.Tx) error {
			var err error
			result, err = f.sync.Pull(ctx, tx, cursor, 0)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return result
	}
	typesIn := func(result sync.PullResult) int {
		for _, c := range result.Changes {
			if c.Collection == "session_types" {
				return len(c.Rows)
			}
		}
		return 0
	}
	createType := func(name string) {
		t.Helper()
		if err := f.tx(t, func(tx pgx.Tx) error {
			_, err := f.deps.Scheduling.CreateSessionType(ctx, tx, f.tenantID,
				scheduling.CreateSessionTypeInput{Name: name, DurationMinutes: 60, Capacity: 1, CreditCost: 1})
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}

	createType("1-on-1")
	first := pull(nil)
	if got := typesIn(first); got != 1 {
		t.Fatalf("first sync carried %d session types, want 1", got)
	}

	createType("Semi-private")
	cursor, err := sync.DecodeCursor(first.Cursor)
	if err != nil {
		t.Fatal(err)
	}
	if got := typesIn(pull(cursor)); got != 1 {
		t.Errorf("a session type created after the first sync arrived %d times, want once", got)
	}

	// The cursor an older server handed out marked session types as
	// delivered with a 1. Every real sequence number is higher, so that
	// device receives the whole set rather than never seeing a new type.
	cursor["session_types"] = 1
	if got := typesIn(pull(cursor)); got != 2 {
		t.Errorf("a device holding the old marker received %d session types, want 2", got)
	}
}

// Encrypted notes and share-token secrets must never reach a device.
func TestPullOmitsSensitiveColumns(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	if err := f.tx(t, func(tx pgx.Tx) error {
		notes := "severe peanut allergy"
		_, err := f.deps.CRM.Create(ctx, tx, f.tenantID, crm.CreateInput{
			FullName: "Dana", MedicalNotes: &notes,
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	var result sync.PullResult
	if err := f.tx(t, func(tx pgx.Tx) error {
		var err error
		result, err = f.sync.Pull(ctx, tx, nil, 0)
		return err
	}); err != nil {
		t.Fatal(err)
	}

	for _, c := range result.Changes {
		for _, row := range c.Rows {
			var fields map[string]any
			if err := json.Unmarshal(row, &fields); err != nil {
				t.Fatalf("row is not an object: %s", row)
			}
			for _, forbidden := range []string{
				"medical_notes_encrypted", "share_token_hash", "password_hash",
				"token_hash", "signed_body", "signed_ip",
			} {
				if _, present := fields[forbidden]; present {
					t.Errorf("%s leaked %q into a sync payload", c.Collection, forbidden)
				}
			}
		}
	}
}

func TestPullPagesAndResumesExactly(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	// Twelve clients on top of the seeded library.
	if err := f.tx(t, func(tx pgx.Tx) error {
		for i := range 12 {
			if _, err := f.deps.CRM.Create(ctx, tx, f.tenantID, crm.CreateInput{
				FullName: "Client " + string(rune('A'+i)),
			}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// Page through in small bites and count what arrives. Each row must be
	// delivered exactly once, and nothing may be skipped — the failure this
	// guards against is silent, so it is asserted by identity, not by count.
	seenIDs := map[string]bool{}
	duplicates := 0
	token := ""

	for page := 0; page < 100; page++ {
		cursor, err := sync.DecodeCursor(token)
		if err != nil {
			t.Fatalf("page %d: %v", page, err)
		}
		var result sync.PullResult
		if err := f.tx(t, func(tx pgx.Tx) error {
			var pullErr error
			result, pullErr = f.sync.Pull(ctx, tx, cursor, 10)
			return pullErr
		}); err != nil {
			t.Fatal(err)
		}

		rows := 0
		for _, c := range result.Changes {
			for _, raw := range c.Rows {
				var fields map[string]any
				if err := json.Unmarshal(raw, &fields); err != nil {
					t.Fatal(err)
				}
				key := c.Collection + ":" + fields["id"].(string)
				if seenIDs[key] {
					duplicates++
				}
				seenIDs[key] = true
				rows++
			}
		}
		token = result.Cursor
		if rows == 0 {
			break
		}
	}

	if duplicates > 0 {
		t.Errorf("paging delivered %d rows more than once", duplicates)
	}
	seen := len(seenIDs)

	// Every seeded exercise and every client, each exactly once. A single
	// global cursor silently dropped every exercise here, which is what
	// per-collection cursors exist to prevent.
	//
	// The expected total is counted from the database rather than written in,
	// so growing the seeded library does not quietly weaken this assertion.
	var want int
	if err := f.tx(t, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT (SELECT count(*) FROM exercises) + (SELECT count(*) FROM clients)`).Scan(&want)
	}); err != nil {
		t.Fatal(err)
	}
	if seen < want {
		t.Errorf("paging delivered %d distinct rows, fewer than the %d written", seen, want)
	}
}

// ---------------------------------------------------------------------------
// Push
// ---------------------------------------------------------------------------

// The PRD's offline scenario: a trainer works through a morning in a basement,
// then the phone reconnects and drains its outbox.
func TestPushDrainsAMorningOfOfflineWork(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	// Set up as if the trainer had synced before going offline.
	var clientID, attendeeID, squatID, invoiceID ids.ID
	if err := f.tx(t, func(tx pgx.Tx) error {
		client, err := f.deps.CRM.Create(ctx, tx, f.tenantID, crm.CreateInput{FullName: "Dana"})
		if err != nil {
			return err
		}
		clientID = client.ID

		if _, err := f.deps.Billing.Grant(ctx, tx, f.tenantID, billing.GrantInput{
			ClientID: clientID, Name: "10-pack", Credits: 10,
			UnitPrice: money.New(5000, "EUR"), PurchasedOn: may1,
		}); err != nil {
			return err
		}

		sessionType, err := f.deps.Scheduling.CreateSessionType(ctx, tx, f.tenantID,
			scheduling.CreateSessionTypeInput{Name: "1-on-1", DurationMinutes: 60, Capacity: 1, CreditCost: 1})
		if err != nil {
			return err
		}
		session, err := f.deps.Scheduling.Book(ctx, tx, f.tenantID, scheduling.BookInput{
			SessionTypeID: sessionType.ID,
			StartsAt:      may1.Add(9 * time.Hour),
			ClientIDs:     []ids.ID{clientID},
		})
		if err != nil {
			return err
		}
		attendeeID = session.Attendees[0].ID

		exercises, err := f.deps.Programming.ListExercises(ctx, tx, programming.ExerciseFilter{Search: "Back Squat"})
		if err != nil {
			return err
		}
		squatID = exercises[0].ID

		draft, err := f.deps.Billing.CreateDraft(ctx, tx, f.tenantID, billing.CreateDraftInput{
			ClientID: clientID,
			Lines: []billing.DraftLineInput{{
				Kind: billing.LineService, Description: "Assessment",
				Quantity: 1, UnitPriceMinor: 8000,
			}},
		})
		if err != nil {
			return err
		}
		issued, err := f.deps.Billing.Issue(ctx, tx, f.tenantID, billing.IssueInput{
			InvoiceID: draft.ID, IssueDate: may1,
		})
		if err != nil {
			return err
		}
		invoiceID = issued.ID
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// The device queues a morning's work, offline, and drains it in ONE batch
	// — which is the only thing a real outbox can do. It cannot start the
	// workout, wait for the server to name it, and only then log the sets:
	// there is no server to ask.
	//
	// So the workout id is minted on the device and every set references it
	// immediately. An earlier version of this test pushed twice and read the
	// server's own id out of the first response, which quietly hid the fact
	// that offline-minted ids were being discarded.
	workoutID := ids.New()
	clientMintedID := ids.New()
	measurementID := ids.New()
	t0 := may1.Add(9 * time.Hour)

	batch := []sync.Operation{
		op(t, sync.OpMarkAttendance, t0, map[string]any{
			"attendee_id": attendeeID, "status": "completed",
		}),
		op(t, sync.OpStartWorkout, t0.Add(time.Minute), map[string]any{
			// Dates cross the wire as calendar dates, the way the spec
			// publishes them and the way the app actually sends them.
			"id": workoutID, "client_id": clientID, "performed_on": "2026-05-01",
		}),
	}
	for set := 1; set <= 3; set++ {
		batch = append(batch, op(t, sync.OpLogSet, t0.Add(time.Duration(set)*2*time.Minute), map[string]any{
			"workout_id": workoutID, "exercise_id": squatID,
			"set_index": set, "reps": 8, "load_grams": 100000, "rpe_tenths": 80,
		}))
	}
	batch = append(batch,
		op(t, sync.OpCompleteWorkout, t0.Add(30*time.Minute), map[string]any{
			"workout_id": workoutID, "notes": "strong session",
		}),
		// Cash taken in a basement — the case ADR 0004 exists for.
		op(t, sync.OpRecordPayment, t0.Add(31*time.Minute), map[string]any{
			"invoice_id": invoiceID, "amount_minor": 8000,
			"currency": "EUR", "instrument": "cash", "reference": "envelope",
			"received_on": "2026-05-01",
		}),
		op(t, sync.OpRecordBiometrics, t0.Add(32*time.Minute), map[string]any{
			"id": measurementID, "client_id": clientID,
			"measured_on": "2026-05-01", "weight_grams": 72400,
		}),
		op(t, sync.OpCreateClient, t0.Add(33*time.Minute), map[string]any{
			"id": clientMintedID, "full_name": "Walk-in, signed up on the floor",
		}),
	)

	result := f.push(t, batch...)
	if result.Applied != len(batch) {
		t.Fatalf("applied %d of %d: %+v", result.Applied, len(batch), result.Results)
	}
	if result.Conflicts != 0 || result.Rejected != 0 {
		t.Errorf("unexpected failures: %+v", result.Results)
	}

	// The server kept the ids the device chose. Without this the sets above
	// would have been refused, and the walk-in client would arrive on the
	// next pull as a second person.
	var startedWorkout programming.Workout
	if err := json.Unmarshal(result.Results[1].Result, &startedWorkout); err != nil {
		t.Fatalf("decode workout result: %v", err)
	}
	if startedWorkout.ID != workoutID {
		t.Fatalf("workout id = %s, want the device's %s", startedWorkout.ID, workoutID)
	}

	// The walk-in is the quieter half of the same bug: a server-minted id
	// leaves the device's optimistic row orphaned, and the next pull adds the
	// same person a second time, permanently.
	var createdClient crm.Client
	if err := json.Unmarshal(result.Results[len(batch)-1].Result, &createdClient); err != nil {
		t.Fatalf("decode client result: %v", err)
	}
	if createdClient.ID != clientMintedID {
		t.Fatalf("client id = %s, want the device's %s", createdClient.ID, clientMintedID)
	}

	// Replaying the whole batch must converge rather than duplicate: this is
	// what happens when a push times out after the server committed it.
	replay := f.push(t, batch...)
	if replay.Rejected != 0 {
		t.Errorf("a replayed batch was rejected: %+v", replay.Results)
	}

	// Everything landed, and the books agree with the calendar.
	if err := f.tx(t, func(tx pgx.Tx) error {
		balance, err := f.deps.Billing.BalanceFor(ctx, tx, clientID)
		if err != nil {
			return err
		}
		if balance.Remaining != 9 {
			t.Errorf("credits = %d, want 9 — the synced attendance did not burn one", balance.Remaining)
		}

		// The attendance posted revenue, not just a status.
		pl, err := f.ledger.ProfitAndLoss(ctx, tx, may1, may1, "EUR")
		if err != nil {
			return err
		}
		// 50.00 from the delivered session plus 80.00 from the assessment.
		if pl.GrossRevenue != 13000 {
			t.Errorf("revenue = %d, want 13000", pl.GrossRevenue)
		}

		invoice, err := f.deps.Billing.GetInvoice(ctx, tx, invoiceID)
		if err != nil {
			return err
		}
		if invoice.Status != billing.InvoiceSettled {
			t.Errorf("invoice = %q, want settled by the synced cash payment", invoice.Status)
		}

		workout, err := f.deps.Programming.GetWorkout(ctx, tx, startedWorkout.ID)
		if err != nil {
			return err
		}
		if len(workout.Sets) != 3 {
			t.Errorf("logged %d sets, want 3", len(workout.Sets))
		}

		tb, err := f.ledger.TrialBalance(ctx, tx, may1, "EUR")
		if err != nil {
			return err
		}
		if !tb.InBalance() {
			t.Error("the books do not balance after a sync")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// Replaying an entire batch must converge, not duplicate. This is the property
// the whole offline design rests on.
func TestReplayingABatchConvergesRatherThanDuplicating(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	var clientID, squatID ids.ID
	if err := f.tx(t, func(tx pgx.Tx) error {
		client, err := f.deps.CRM.Create(ctx, tx, f.tenantID, crm.CreateInput{FullName: "Dana"})
		if err != nil {
			return err
		}
		clientID = client.ID
		exercises, err := f.deps.Programming.ListExercises(ctx, tx, programming.ExerciseFilter{Search: "Back Squat"})
		if err != nil {
			return err
		}
		squatID = exercises[0].ID
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	start := f.push(t, op(t, sync.OpStartWorkout, may1, map[string]any{
		"client_id": clientID, "performed_on": may1,
	}))
	var workout programming.Workout
	if err := json.Unmarshal(start.Results[0].Result, &workout); err != nil {
		t.Fatal(err)
	}

	batch := []sync.Operation{}
	for set := 1; set <= 3; set++ {
		batch = append(batch, op(t, sync.OpLogSet, may1.Add(time.Duration(set)*time.Minute), map[string]any{
			"workout_id": workout.ID, "exercise_id": squatID,
			"set_index": set, "reps": 8, "load_grams": 100000,
		}))
	}

	// The device loses its acknowledgement three times and resends.
	for attempt := range 3 {
		result := f.push(t, batch...)
		if result.Applied != 3 {
			t.Fatalf("attempt %d applied %d of 3: %+v", attempt, result.Applied, result.Results)
		}
	}

	if err := f.tx(t, func(tx pgx.Tx) error {
		w, err := f.deps.Programming.GetWorkout(ctx, tx, workout.ID)
		if err != nil {
			return err
		}
		if len(w.Sets) != 3 {
			t.Errorf("three replays produced %d sets, want 3", len(w.Sets))
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// One operation failing must not discard the rest of the batch, and must leave
// nothing behind.
func TestAFailedOperationIsIsolatedFromTheBatch(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	var fundedAttendee, brokeAttendee, fundedClient ids.ID
	if err := f.tx(t, func(tx pgx.Tx) error {
		funded, err := f.deps.CRM.Create(ctx, tx, f.tenantID, crm.CreateInput{FullName: "Funded"})
		if err != nil {
			return err
		}
		fundedClient = funded.ID
		broke, err := f.deps.CRM.Create(ctx, tx, f.tenantID, crm.CreateInput{FullName: "Broke"})
		if err != nil {
			return err
		}
		if _, err := f.deps.Billing.Grant(ctx, tx, f.tenantID, billing.GrantInput{
			ClientID: funded.ID, Name: "pack", Credits: 5,
			UnitPrice: money.New(5000, "EUR"), PurchasedOn: may1,
		}); err != nil {
			return err
		}

		sessionType, err := f.deps.Scheduling.CreateSessionType(ctx, tx, f.tenantID,
			scheduling.CreateSessionTypeInput{Name: "1-on-1", DurationMinutes: 60, Capacity: 1, CreditCost: 1})
		if err != nil {
			return err
		}
		s1, err := f.deps.Scheduling.Book(ctx, tx, f.tenantID, scheduling.BookInput{
			SessionTypeID: sessionType.ID, StartsAt: may1.Add(9 * time.Hour),
			ClientIDs: []ids.ID{funded.ID},
		})
		if err != nil {
			return err
		}
		fundedAttendee = s1.Attendees[0].ID

		s2, err := f.deps.Scheduling.Book(ctx, tx, f.tenantID, scheduling.BookInput{
			SessionTypeID: sessionType.ID, StartsAt: may1.Add(11 * time.Hour),
			ClientIDs: []ids.ID{broke.ID},
		})
		if err != nil {
			return err
		}
		brokeAttendee = s2.Attendees[0].ID
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	result := f.push(t,
		op(t, sync.OpMarkAttendance, may1.Add(9*time.Hour), map[string]any{
			"attendee_id": brokeAttendee, "status": "completed", // no credits
		}),
		op(t, sync.OpMarkAttendance, may1.Add(10*time.Hour), map[string]any{
			"attendee_id": fundedAttendee, "status": "completed",
		}),
	)

	if result.Applied != 1 {
		t.Errorf("applied %d, want 1", result.Applied)
	}
	if result.Conflicts != 1 {
		t.Errorf("conflicts %d, want 1", result.Conflicts)
	}

	// The conflict names what went wrong, so the trainer can act on it.
	var conflict *sync.OperationResult
	for i := range result.Results {
		if result.Results[i].Status == "conflict" {
			conflict = &result.Results[i]
		}
	}
	if conflict == nil {
		t.Fatal("no conflict was reported")
	}
	if conflict.Code != "insufficient_credits" {
		t.Errorf("conflict code = %q", conflict.Code)
	}

	// The funded client's session went through, and the failed one left the
	// books untouched.
	if err := f.tx(t, func(tx pgx.Tx) error {
		balance, err := f.deps.Billing.BalanceFor(ctx, tx, fundedClient)
		if err != nil {
			return err
		}
		if balance.Remaining != 4 {
			t.Errorf("funded client has %d credits, want 4", balance.Remaining)
		}
		pl, err := f.ledger.ProfitAndLoss(ctx, tx, may1, may1, "EUR")
		if err != nil {
			return err
		}
		if pl.GrossRevenue != 5000 {
			t.Errorf("revenue = %d, want one session's 5000", pl.GrossRevenue)
		}
		tb, err := f.ledger.TrialBalance(ctx, tx, may1, "EUR")
		if err != nil {
			return err
		}
		if !tb.InBalance() {
			t.Error("a partially failed batch left the books unbalanced")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// Operations apply in the order the device queued them, not the order they
// arrive in the request.
func TestOperationsApplyInQueuedOrder(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	var attendeeID ids.ID
	if err := f.tx(t, func(tx pgx.Tx) error {
		client, err := f.deps.CRM.Create(ctx, tx, f.tenantID, crm.CreateInput{FullName: "Dana"})
		if err != nil {
			return err
		}
		if _, err := f.deps.Billing.Grant(ctx, tx, f.tenantID, billing.GrantInput{
			ClientID: client.ID, Name: "pack", Credits: 5,
			UnitPrice: money.New(5000, "EUR"), PurchasedOn: may1,
		}); err != nil {
			return err
		}
		sessionType, err := f.deps.Scheduling.CreateSessionType(ctx, tx, f.tenantID,
			scheduling.CreateSessionTypeInput{Name: "1-on-1", DurationMinutes: 60, Capacity: 1, CreditCost: 1})
		if err != nil {
			return err
		}
		session, err := f.deps.Scheduling.Book(ctx, tx, f.tenantID, scheduling.BookInput{
			SessionTypeID: sessionType.ID, StartsAt: may1.Add(9 * time.Hour),
			ClientIDs: []ids.ID{client.ID},
		})
		if err != nil {
			return err
		}
		attendeeID = session.Attendees[0].ID
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// The trainer marked it completed, then changed their mind to late cancel.
	// Sent out of order, the later decision must still win.
	result := f.push(t,
		op(t, sync.OpMarkAttendance, may1.Add(11*time.Hour), map[string]any{
			"attendee_id": attendeeID, "status": "late_cancel",
		}),
		op(t, sync.OpMarkAttendance, may1.Add(10*time.Hour), map[string]any{
			"attendee_id": attendeeID, "status": "completed",
		}),
	)
	if result.Applied != 2 {
		t.Fatalf("applied %d of 2: %+v", result.Applied, result.Results)
	}

	if err := f.tx(t, func(tx pgx.Tx) error {
		var status string
		if err := tx.QueryRow(ctx,
			`SELECT status::text FROM session_attendees WHERE id = $1`, attendeeID).Scan(&status); err != nil {
			return err
		}
		if status != "late_cancel" {
			t.Errorf("final status = %q, want late_cancel — the later decision should win", status)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestUnknownOperationIsRejectedNotIgnored(t *testing.T) {
	f := setup(t)
	result := f.push(t, op(t, sync.OperationType("ledger.write_whatever"), may1, map[string]any{}))

	if result.Rejected != 1 {
		t.Fatalf("rejected %d, want 1: %+v", result.Rejected, result.Results)
	}
	if result.Results[0].Status != "rejected" {
		t.Errorf("status = %q", result.Results[0].Status)
	}
}

func TestSyncIsTenantIsolated(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	if err := f.tx(t, func(tx pgx.Tx) error {
		_, err := f.deps.CRM.Create(ctx, tx, f.tenantID, crm.CreateInput{FullName: "Dana"})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	other := ids.New()
	owner := testsupport.OpenOwner(t)
	if _, err := owner.Raw().Exec(ctx,
		`INSERT INTO tenants (id, name) VALUES ($1, 'Other Gym')`, other); err != nil {
		t.Fatal(err)
	}

	if err := f.pool.InTenantTx(ctx, other, func(tx pgx.Tx) error {
		result, err := f.sync.Pull(ctx, tx, nil, 0)
		if err != nil {
			return err
		}
		for _, c := range result.Changes {
			// A tenant's own settings row is the one thing it should see.
			if c.Collection == "settings" && len(c.Rows) == 1 && strings.Contains(string(c.Rows[0]), other.String()) {
				continue
			}
			if len(c.Rows) > 0 {
				t.Errorf("another tenant pulled %d rows from %s", len(c.Rows), c.Collection)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
