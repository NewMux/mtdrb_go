//go:build integration

package scheduling_test

import (
	"context"
	"math/rand"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/NewMux/mtdrb_go/internal/ledger"
	"github.com/NewMux/mtdrb_go/internal/platform/errs"
	"github.com/NewMux/mtdrb_go/internal/platform/ids"
	"github.com/NewMux/mtdrb_go/internal/scheduling"
)

// Undoing a completion must restore the credit and reverse the revenue, so a
// mistaken tap on a gym floor leaves no trace in the books.
func TestUndoCompletionRestoresCreditAndReversesRevenue(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	clientID, _ := f.newClient(t, "Client A", 10, 5000)
	typeID := f.newSessionType(t, "1-on-1", 1, 1)

	var attendeeID ids.ID
	if err := f.tx(t, func(tx pgx.Tx) error {
		session, err := f.sched.Book(ctx, tx, f.tenantID, scheduling.BookInput{
			SessionTypeID: typeID, StartsAt: at9, ClientIDs: []ids.ID{clientID},
		})
		if err != nil {
			return err
		}
		attendeeID = session.Attendees[0].ID
		_, err = f.sched.Mark(ctx, tx, f.tenantID, scheduling.MarkInput{
			AttendeeID: attendeeID, Status: scheduling.Completed,
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	var undone scheduling.MarkResult
	if err := f.tx(t, func(tx pgx.Tx) error {
		var err error
		undone, err = f.sched.Mark(ctx, tx, f.tenantID, scheduling.MarkInput{
			AttendeeID: attendeeID, Status: scheduling.Scheduled,
		})
		return err
	}); err != nil {
		t.Fatalf("undo: %v", err)
	}

	if undone.CreditsRemaining != 10 {
		t.Errorf("credits after undo = %d, want the original 10", undone.CreditsRemaining)
	}
	if undone.Attendee.CreditsCharged != 0 {
		t.Errorf("credits charged after undo = %d, want 0", undone.Attendee.CreditsCharged)
	}

	if err := f.tx(t, func(tx pgx.Tx) error {
		// Revenue nets to zero, and the original entry survives alongside its
		// reversal rather than being edited away.
		pl, err := f.ledger.ProfitAndLoss(ctx, tx, feb2, feb2, "EUR")
		if err != nil {
			return err
		}
		if pl.GrossRevenue != 0 {
			t.Errorf("revenue after undo = %d, want 0", pl.GrossRevenue)
		}

		var entries int
		if err := tx.QueryRow(ctx,
			`SELECT count(*) FROM journal_entries WHERE source_type = 'session_delivered'`).Scan(&entries); err != nil {
			return err
		}
		if entries != 2 {
			t.Errorf("found %d session entries, want 2 (the original and its reversal)", entries)
		}

		tb, err := f.ledger.TrialBalance(ctx, tx, feb2, "EUR")
		if err != nil {
			return err
		}
		if !tb.InBalance() {
			t.Error("trial balance broken after undo")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// An early cancellation refunds the credit by never burning it; a late
// cancellation bills it, to its own revenue account.
func TestEarlyCancelIsFreeAndLateCancelIsBilled(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	typeID := f.newSessionType(t, "1-on-1", 1, 1)

	earlyClient, _ := f.newClient(t, "Early", 5, 5000)
	early := f.bookAndMark(t, typeID, earlyClient, at9, scheduling.EarlyCancel)
	if early.CreditsRemaining != 5 {
		t.Errorf("early cancel consumed a credit: %d remaining, want 5", early.CreditsRemaining)
	}
	if early.RevenueRecognised != nil {
		t.Errorf("early cancel recognised revenue: %v", early.RevenueRecognised)
	}

	lateClient, _ := f.newClient(t, "Late", 5, 5000)
	late := f.bookAndMark(t, typeID, lateClient, at9.Add(4*time.Hour), scheduling.LateCancel)
	if late.CreditsRemaining != 4 {
		t.Errorf("late cancel left %d credits, want 4", late.CreditsRemaining)
	}
	if late.RevenueRecognised == nil || late.RevenueRecognised.Minor != 5000 {
		t.Fatalf("late cancel recognised %v, want 50.00", late.RevenueRecognised)
	}

	// Booked to its own account, so the trainer can see how much income comes
	// from cancellations rather than delivered training.
	if err := f.tx(t, func(tx pgx.Tx) error {
		entry, err := f.ledger.Get(ctx, tx, *late.JournalEntryID)
		if err != nil {
			return err
		}
		var found bool
		for _, l := range entry.Lines {
			if l.AccountSlug == ledger.SlugLateCancelRevenue && l.Credit == 5000 {
				found = true
			}
		}
		if !found {
			t.Errorf("late cancellation was not booked to its own revenue account: %+v", entry.Lines)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// The no-show policy is a tenant setting, not a fixed rule.
func TestNoShowBillingFollowsTenantSetting(t *testing.T) {
	billed := setup(t) // no_show_is_billable defaults to true
	typeID := billed.newSessionType(t, "1-on-1", 1, 1)
	clientID, _ := billed.newClient(t, "Client", 5, 5000)

	result := billed.bookAndMark(t, typeID, clientID, at9, scheduling.NoShow)
	if result.CreditsRemaining != 4 {
		t.Errorf("billable no-show left %d credits, want 4", result.CreditsRemaining)
	}
	if result.RevenueRecognised == nil {
		t.Error("billable no-show recognised no revenue")
	}

	free := setup(t, withNoShowFree())
	freeType := free.newSessionType(t, "1-on-1", 1, 1)
	freeClient, _ := free.newClient(t, "Client", 5, 5000)

	freeResult := free.bookAndMark(t, freeType, freeClient, at9, scheduling.NoShow)
	if freeResult.CreditsRemaining != 5 {
		t.Errorf("non-billable no-show consumed a credit: %d remaining, want 5", freeResult.CreditsRemaining)
	}
	if freeResult.RevenueRecognised != nil {
		t.Errorf("non-billable no-show recognised revenue: %v", freeResult.RevenueRecognised)
	}
}

// Moving between two billable states must not charge twice.
func TestMovingBetweenBillableStatesDoesNotDoubleCharge(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	clientID, _ := f.newClient(t, "Client A", 10, 5000)
	typeID := f.newSessionType(t, "1-on-1", 1, 1)

	var attendeeID ids.ID
	if err := f.tx(t, func(tx pgx.Tx) error {
		session, err := f.sched.Book(ctx, tx, f.tenantID, scheduling.BookInput{
			SessionTypeID: typeID, StartsAt: at9, ClientIDs: []ids.ID{clientID},
		})
		if err != nil {
			return err
		}
		attendeeID = session.Attendees[0].ID
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// completed -> late_cancel -> completed: still exactly one credit.
	for _, status := range []scheduling.AttendanceStatus{
		scheduling.Completed, scheduling.LateCancel, scheduling.Completed,
	} {
		var result scheduling.MarkResult
		if err := f.tx(t, func(tx pgx.Tx) error {
			var err error
			result, err = f.sched.Mark(ctx, tx, f.tenantID, scheduling.MarkInput{
				AttendeeID: attendeeID, Status: status,
			})
			return err
		}); err != nil {
			t.Fatalf("mark %s: %v", status, err)
		}
		if result.CreditsRemaining != 9 {
			t.Errorf("after %s: %d credits remaining, want 9", status, result.CreditsRemaining)
		}
	}

	if err := f.tx(t, func(tx pgx.Tx) error {
		pl, err := f.ledger.ProfitAndLoss(ctx, tx, feb2, feb2, "EUR")
		if err != nil {
			return err
		}
		if pl.GrossRevenue != 5000 {
			t.Errorf("revenue = %d, want exactly one session's 5000", pl.GrossRevenue)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// Re-marking the same status is a no-op, because the offline outbox replays.
func TestRemarkingTheSameStatusIsIdempotent(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	clientID, _ := f.newClient(t, "Client A", 10, 5000)
	typeID := f.newSessionType(t, "1-on-1", 1, 1)

	var attendeeID ids.ID
	if err := f.tx(t, func(tx pgx.Tx) error {
		session, err := f.sched.Book(ctx, tx, f.tenantID, scheduling.BookInput{
			SessionTypeID: typeID, StartsAt: at9, ClientIDs: []ids.ID{clientID},
		})
		if err != nil {
			return err
		}
		attendeeID = session.Attendees[0].ID
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	for i := range 3 {
		var result scheduling.MarkResult
		if err := f.tx(t, func(tx pgx.Tx) error {
			var err error
			result, err = f.sched.Mark(ctx, tx, f.tenantID, scheduling.MarkInput{
				AttendeeID: attendeeID, Status: scheduling.Completed,
			})
			return err
		}); err != nil {
			t.Fatalf("replay %d: %v", i, err)
		}
		if result.CreditsRemaining != 9 {
			t.Fatalf("replay %d left %d credits; the replay charged again", i, result.CreditsRemaining)
		}
	}
}

// In a semi-private session each attendee's outcome is independent.
func TestSemiPrivateAttendeesAreIndependent(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	alice, _ := f.newClient(t, "Alice", 5, 5000)
	bob, _ := f.newClient(t, "Bob", 5, 4000) // a different rate
	typeID := f.newSessionType(t, "Semi-Private", 2, 1)

	var aliceAttendee, bobAttendee ids.ID
	if err := f.tx(t, func(tx pgx.Tx) error {
		session, err := f.sched.Book(ctx, tx, f.tenantID, scheduling.BookInput{
			SessionTypeID: typeID, StartsAt: at9, ClientIDs: []ids.ID{alice, bob},
		})
		if err != nil {
			return err
		}
		if len(session.Attendees) != 2 {
			t.Fatalf("roster has %d, want 2", len(session.Attendees))
		}
		for _, a := range session.Attendees {
			if a.ClientID == alice {
				aliceAttendee = a.ID
			} else {
				bobAttendee = a.ID
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// Alice trains; Bob does not turn up.
	var aliceResult, bobResult scheduling.MarkResult
	if err := f.tx(t, func(tx pgx.Tx) error {
		var err error
		if aliceResult, err = f.sched.Mark(ctx, tx, f.tenantID, scheduling.MarkInput{
			AttendeeID: aliceAttendee, Status: scheduling.Completed,
		}); err != nil {
			return err
		}
		bobResult, err = f.sched.Mark(ctx, tx, f.tenantID, scheduling.MarkInput{
			AttendeeID: bobAttendee, Status: scheduling.EarlyCancel,
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	if aliceResult.CreditsRemaining != 4 {
		t.Errorf("Alice has %d credits, want 4", aliceResult.CreditsRemaining)
	}
	if bobResult.CreditsRemaining != 5 {
		t.Errorf("Bob was charged for a session he cancelled early: %d credits", bobResult.CreditsRemaining)
	}

	// Revenue reflects Alice's rate only, not a blended one.
	if err := f.tx(t, func(tx pgx.Tx) error {
		pl, err := f.ledger.ProfitAndLoss(ctx, tx, feb2, feb2, "EUR")
		if err != nil {
			return err
		}
		if pl.GrossRevenue != 5000 {
			t.Errorf("revenue = %d, want Alice's 5000 alone", pl.GrossRevenue)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestSessionCapacityIsEnforced(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	alice, _ := f.newClient(t, "Alice", 5, 5000)
	bob, _ := f.newClient(t, "Bob", 5, 5000)
	cleo, _ := f.newClient(t, "Cleo", 5, 5000)
	typeID := f.newSessionType(t, "Semi-Private", 2, 1)

	var sessionID ids.ID
	if err := f.tx(t, func(tx pgx.Tx) error {
		session, err := f.sched.Book(ctx, tx, f.tenantID, scheduling.BookInput{
			SessionTypeID: typeID, StartsAt: at9, ClientIDs: []ids.ID{alice, bob},
		})
		if err != nil {
			return err
		}
		sessionID = session.ID
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	err := f.tx(t, func(tx pgx.Tx) error {
		_, err := f.sched.AddAttendee(ctx, tx, f.tenantID, sessionID, cleo)
		return err
	})
	if err == nil {
		t.Fatal("a third client was added to a two-seat session")
	}
	if errs.KindOf(err) != errs.KindConflict {
		t.Errorf("kind = %q, want conflict", errs.KindOf(err))
	}

	// Booking more clients than the type seats is refused up front.
	err = f.tx(t, func(tx pgx.Tx) error {
		_, err := f.sched.Book(ctx, tx, f.tenantID, scheduling.BookInput{
			SessionTypeID: typeID, StartsAt: at9.Add(5 * time.Hour),
			ClientIDs: []ids.ID{alice, bob, cleo},
		})
		return err
	})
	if err == nil {
		t.Fatal("booking beyond capacity was accepted")
	}
}

// The buffer is enforced by the database, so two devices cannot both win.
func TestBufferPreventsBackToBackBookings(t *testing.T) {
	f := setup(t) // 15-minute buffer
	ctx := context.Background()

	clientID, _ := f.newClient(t, "Client A", 10, 5000)
	typeID := f.newSessionType(t, "1-on-1", 1, 1) // 60 minutes

	// 09:00-10:00, so the trainer is blocked until 10:15.
	if err := f.tx(t, func(tx pgx.Tx) error {
		_, err := f.sched.Book(ctx, tx, f.tenantID, scheduling.BookInput{
			SessionTypeID: typeID, StartsAt: at9, ClientIDs: []ids.ID{clientID},
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	// 10:10 falls inside the buffer.
	err := f.tx(t, func(tx pgx.Tx) error {
		_, err := f.sched.Book(ctx, tx, f.tenantID, scheduling.BookInput{
			SessionTypeID: typeID, StartsAt: at9.Add(70 * time.Minute), ClientIDs: []ids.ID{clientID},
		})
		return err
	})
	if err == nil {
		t.Fatal("a booking inside the buffer was accepted")
	}
	if errs.CodeOf(err) != errs.CodeSchedulingConflict {
		t.Errorf("code = %q, want %q", errs.CodeOf(err), errs.CodeSchedulingConflict)
	}

	// 10:15 exactly is fine: the buffer is satisfied.
	if err := f.tx(t, func(tx pgx.Tx) error {
		_, err := f.sched.Book(ctx, tx, f.tenantID, scheduling.BookInput{
			SessionTypeID: typeID, StartsAt: at9.Add(75 * time.Minute), ClientIDs: []ids.ID{clientID},
		})
		return err
	}); err != nil {
		t.Fatalf("a booking exactly at the buffer boundary was refused: %v", err)
	}
}

func TestCancelledSessionFreesItsSlot(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	clientID, _ := f.newClient(t, "Client A", 10, 5000)
	typeID := f.newSessionType(t, "1-on-1", 1, 1)

	var sessionID ids.ID
	if err := f.tx(t, func(tx pgx.Tx) error {
		session, err := f.sched.Book(ctx, tx, f.tenantID, scheduling.BookInput{
			SessionTypeID: typeID, StartsAt: at9, ClientIDs: []ids.ID{clientID},
		})
		if err != nil {
			return err
		}
		sessionID = session.ID
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	if err := f.tx(t, func(tx pgx.Tx) error {
		return f.sched.Cancel(ctx, tx, sessionID)
	}); err != nil {
		t.Fatal(err)
	}

	// The same slot can now be rebooked.
	if err := f.tx(t, func(tx pgx.Tx) error {
		_, err := f.sched.Book(ctx, tx, f.tenantID, scheduling.BookInput{
			SessionTypeID: typeID, StartsAt: at9, ClientIDs: []ids.ID{clientID},
		})
		return err
	}); err != nil {
		t.Fatalf("rebooking a cancelled slot was refused: %v", err)
	}
}

// The bulk check-off must report who it could not mark, not skip them quietly.
func TestMarkDayReportsWhoCouldNotBeMarked(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	typeID := f.newSessionType(t, "1-on-1", 1, 1)

	funded, _ := f.newClient(t, "Funded", 5, 5000)
	broke, _ := f.newClient(t, "Broke", 0, 0) // no package at all

	if err := f.tx(t, func(tx pgx.Tx) error {
		if _, err := f.sched.Book(ctx, tx, f.tenantID, scheduling.BookInput{
			SessionTypeID: typeID, StartsAt: at9, ClientIDs: []ids.ID{funded},
		}); err != nil {
			return err
		}
		_, err := f.sched.Book(ctx, tx, f.tenantID, scheduling.BookInput{
			SessionTypeID: typeID, StartsAt: at9.Add(2 * time.Hour), ClientIDs: []ids.ID{broke},
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	var result scheduling.DayResult
	if err := f.tx(t, func(tx pgx.Tx) error {
		var err error
		result, err = f.sched.MarkDay(ctx, tx, f.tenantID, feb2, scheduling.Completed, nil)
		return err
	}); err != nil {
		t.Fatalf("mark day: %v", err)
	}

	if len(result.Marked) != 1 {
		t.Errorf("marked %d attendees, want 1", len(result.Marked))
	}
	if len(result.Skipped) != 1 {
		t.Fatalf("skipped %d attendees, want 1", len(result.Skipped))
	}
	if result.Skipped[0].ClientName != "Broke" {
		t.Errorf("skipped %q, want Broke", result.Skipped[0].ClientName)
	}
	if result.Skipped[0].Reason != errs.CodeInsufficientCredits {
		t.Errorf("skip reason = %q", result.Skipped[0].Reason)
	}

	// The funded client was genuinely marked, and the failed one left nothing
	// behind.
	if err := f.tx(t, func(tx pgx.Tx) error {
		pl, err := f.ledger.ProfitAndLoss(ctx, tx, feb2, feb2, "EUR")
		if err != nil {
			return err
		}
		if pl.GrossRevenue != 5000 {
			t.Errorf("revenue = %d, want one session's 5000", pl.GrossRevenue)
		}
		tb, err := f.ledger.TrialBalance(ctx, tx, feb2, "EUR")
		if err != nil {
			return err
		}
		if !tb.InBalance() {
			t.Error("trial balance broken after a partially failed bulk mark")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// Credits are consumed from the pack that expires soonest, so a client never
// loses credit they could have spent.
func TestCreditsAreConsumedFromTheSoonestExpiringPack(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	clientID, _ := f.newClient(t, "Client A", 0, 0)
	typeID := f.newSessionType(t, "1-on-1", 1, 1)

	soonExpiry := feb2.AddDate(0, 0, 30)
	var expiringPack, openPack ids.ID
	if err := f.tx(t, func(tx pgx.Tx) error {
		// Bought later, but expires sooner.
		expiring, err := f.billing.Grant(ctx, tx, f.tenantID, billingGrant(clientID, 2, 4000, feb2.AddDate(0, 0, 5), &soonExpiry))
		if err != nil {
			return err
		}
		expiringPack = expiring.ID

		open, err := f.billing.Grant(ctx, tx, f.tenantID, billingGrant(clientID, 2, 5000, feb2, nil))
		if err != nil {
			return err
		}
		openPack = open.ID
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	f.bookAndMark(t, typeID, clientID, at9, scheduling.Completed)

	if err := f.tx(t, func(tx pgx.Tx) error {
		var expiringRemaining, openRemaining int
		if err := tx.QueryRow(ctx,
			`SELECT credits_remaining FROM packages WHERE id = $1`, expiringPack).Scan(&expiringRemaining); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx,
			`SELECT credits_remaining FROM packages WHERE id = $1`, openPack).Scan(&openRemaining); err != nil {
			return err
		}
		if expiringRemaining != 1 {
			t.Errorf("expiring pack has %d credits, want 1 consumed first", expiringRemaining)
		}
		if openRemaining != 2 {
			t.Errorf("non-expiring pack has %d credits, want it untouched", openRemaining)
		}

		// Revenue follows the pack that was actually consumed.
		pl, err := f.ledger.ProfitAndLoss(ctx, tx, feb2, feb2, "EUR")
		if err != nil {
			return err
		}
		if pl.GrossRevenue != 4000 {
			t.Errorf("revenue = %d, want the expiring pack's rate of 4000", pl.GrossRevenue)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// Randomised attendance sequences: whatever order a trainer marks, unmarks and
// re-marks things in, the credit log, the pack balances and the ledger must
// all still agree.
func TestCreditAndLedgerInvariantsUnderRandomAttendanceSequences(t *testing.T) {
	f := setup(t, withOverdraft())
	ctx := context.Background()

	typeID := f.newSessionType(t, "1-on-1", 1, 1)
	clientID, _ := f.newClient(t, "Client A", 20, 5000)

	statuses := []scheduling.AttendanceStatus{
		scheduling.Scheduled, scheduling.Completed, scheduling.LateCancel,
		scheduling.EarlyCancel, scheduling.NoShow,
	}

	// A day of sessions, each far enough apart to clear the buffer.
	var attendees []ids.ID
	if err := f.tx(t, func(tx pgx.Tx) error {
		for i := range 6 {
			session, err := f.sched.Book(ctx, tx, f.tenantID, scheduling.BookInput{
				SessionTypeID: typeID,
				StartsAt:      at9.Add(time.Duration(i) * 2 * time.Hour),
				ClientIDs:     []ids.ID{clientID},
			})
			if err != nil {
				return err
			}
			attendees = append(attendees, session.Attendees[0].ID)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	rng := rand.New(rand.NewSource(7))
	for range 60 {
		attendeeID := attendees[rng.Intn(len(attendees))]
		status := statuses[rng.Intn(len(statuses))]

		if err := f.tx(t, func(tx pgx.Tx) error {
			_, err := f.sched.Mark(ctx, tx, f.tenantID, scheduling.MarkInput{
				AttendeeID: attendeeID, Status: status,
			})
			return err
		}); err != nil {
			t.Fatalf("mark %s: %v", status, err)
		}
	}

	if err := f.tx(t, func(tx pgx.Tx) error {
		// The append-only credit log and each pack's cached balance must agree
		// exactly; the cache is what the gym floor reads.
		rows, err := tx.Query(ctx, `
			SELECT p.id, p.credits_total, p.credits_remaining, coalesce(sum(ct.delta), 0)
			  FROM packages p LEFT JOIN credit_transactions ct ON ct.package_id = p.id
			 GROUP BY p.id, p.credits_total, p.credits_remaining`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id ids.ID
			var total, remaining, logged int
			if err := rows.Scan(&id, &total, &remaining, &logged); err != nil {
				return err
			}
			if remaining != logged {
				t.Errorf("package %s: cached balance %d but the log sums to %d", id, remaining, logged)
			}
		}
		if err := rows.Err(); err != nil {
			return err
		}

		// Every attendee's recorded charge matches what the log took for it.
		attendeeRows, err := tx.Query(ctx, `
			SELECT sa.id, sa.status::text, sa.credits_charged,
			       coalesce((SELECT -sum(delta) FROM credit_transactions ct
			                  WHERE ct.session_attendee_id = sa.id), 0)
			  FROM session_attendees sa`)
		if err != nil {
			return err
		}
		defer attendeeRows.Close()
		for attendeeRows.Next() {
			var id ids.ID
			var status string
			var charged, netTaken int
			if err := attendeeRows.Scan(&id, &status, &charged, &netTaken); err != nil {
				return err
			}
			if charged != netTaken {
				t.Errorf("attendee %s (%s): credits_charged = %d but the log net-took %d",
					id, status, charged, netTaken)
			}
		}
		if err := attendeeRows.Err(); err != nil {
			return err
		}

		// And the books still balance.
		tb, err := f.ledger.TrialBalance(ctx, tx, feb2.AddDate(0, 0, 1), "EUR")
		if err != nil {
			return err
		}
		if !tb.InBalance() {
			t.Errorf("trial balance broken: debits %d credits %d", tb.TotalDebits, tb.TotalCredits)
		}

		// Revenue recognised must equal the value of credits actually taken.
		var creditsTaken int
		if err := tx.QueryRow(ctx, `
			SELECT coalesce(sum(credits_charged), 0) FROM session_attendees`).Scan(&creditsTaken); err != nil {
			return err
		}
		pl, err := f.ledger.ProfitAndLoss(ctx, tx, feb2, feb2.AddDate(0, 0, 1), "EUR")
		if err != nil {
			return err
		}
		if want := int64(creditsTaken) * 5000; pl.GrossRevenue != want {
			t.Errorf("revenue = %d but %d credits were charged, worth %d",
				pl.GrossRevenue, creditsTaken, want)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
