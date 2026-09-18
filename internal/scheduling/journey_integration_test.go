//go:build integration

package scheduling_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/NewMux/mtdrb_go/internal/billing"
	"github.com/NewMux/mtdrb_go/internal/crm"
	"github.com/NewMux/mtdrb_go/internal/db"
	"github.com/NewMux/mtdrb_go/internal/ledger"
	"github.com/NewMux/mtdrb_go/internal/platform/clock"
	"github.com/NewMux/mtdrb_go/internal/platform/errs"
	"github.com/NewMux/mtdrb_go/internal/platform/ids"
	"github.com/NewMux/mtdrb_go/internal/platform/money"
	"github.com/NewMux/mtdrb_go/internal/scheduling"
	"github.com/NewMux/mtdrb_go/internal/testsupport"
)

var (
	feb2 = time.Date(2026, 2, 2, 0, 0, 0, 0, time.UTC)
	at9  = time.Date(2026, 2, 2, 9, 0, 0, 0, time.UTC)
)

func eur(minor int64) money.Money { return money.New(minor, "EUR") }

type fixture struct {
	sched    *scheduling.Service
	billing  *billing.Service
	ledger   *ledger.Service
	crm      *crm.Service
	pool     *db.Pool
	tenantID ids.ID
}

func setup(t *testing.T, opts ...func(*tenantOptions)) fixture {
	t.Helper()
	testsupport.RequireDB(t)
	testsupport.Reset(t)

	cfg := tenantOptions{BufferMinutes: 15, NoShowIsBillable: true}
	for _, o := range opts {
		o(&cfg)
	}

	pool := testsupport.OpenApp(t)
	fixed := clock.Fixed{T: feb2}
	ledgerSvc := ledger.NewService(fixed)
	billingSvc := billing.NewService(ledgerSvc, fixed)

	tenantID := ids.New()
	ctx := context.Background()
	owner := testsupport.OpenOwner(t)
	if _, err := owner.Raw().Exec(ctx, `
		INSERT INTO tenants (id, name, default_currency, buffer_minutes, allow_overdraft, no_show_is_billable)
		VALUES ($1, 'Test Gym', 'EUR', $2, $3, $4)`,
		tenantID, cfg.BufferMinutes, cfg.AllowOverdraft, cfg.NoShowIsBillable); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}

	f := fixture{
		sched:    scheduling.NewService(billingSvc, ledgerSvc, fixed),
		billing:  billingSvc,
		ledger:   ledgerSvc,
		crm:      crm.NewService(fixed, []byte("test-column-encryption-key-32byt")),
		pool:     pool,
		tenantID: tenantID,
	}
	if err := f.tx(t, func(tx pgx.Tx) error {
		return ledgerSvc.SeedChartOfAccounts(ctx, tx, tenantID, "EUR")
	}); err != nil {
		t.Fatalf("seed chart: %v", err)
	}
	return f
}

type tenantOptions struct {
	BufferMinutes    int
	AllowOverdraft   bool
	NoShowIsBillable bool
}

func withOverdraft() func(*tenantOptions) {
	return func(o *tenantOptions) { o.AllowOverdraft = true }
}
func withNoShowFree() func(*tenantOptions) {
	return func(o *tenantOptions) { o.NoShowIsBillable = false }
}

func (f fixture) tx(t *testing.T, fn func(tx pgx.Tx) error) error {
	t.Helper()
	return f.pool.InTenantTx(context.Background(), f.tenantID, fn)
}

// newClient creates a client with an optional prepaid pack.
func (f fixture) newClient(t *testing.T, name string, credits int, unitPrice int64) (ids.ID, ids.ID) {
	t.Helper()
	ctx := context.Background()
	var clientID, packageID ids.ID

	if err := f.tx(t, func(tx pgx.Tx) error {
		client, err := f.crm.Create(ctx, tx, f.tenantID, crm.CreateInput{FullName: name})
		if err != nil {
			return err
		}
		clientID = client.ID
		if credits == 0 {
			return nil
		}
		pkg, err := f.billing.Grant(ctx, tx, f.tenantID, billing.GrantInput{
			ClientID: clientID, Name: "10-session pack", Credits: credits,
			UnitPrice: eur(unitPrice), PurchasedOn: feb2,
		})
		if err != nil {
			return err
		}
		packageID = pkg.ID
		return nil
	}); err != nil {
		t.Fatalf("create client %s: %v", name, err)
	}
	return clientID, packageID
}

func (f fixture) newSessionType(t *testing.T, name string, capacity, creditCost int) ids.ID {
	t.Helper()
	var id ids.ID
	if err := f.tx(t, func(tx pgx.Tx) error {
		st, err := f.sched.CreateSessionType(context.Background(), tx, f.tenantID,
			scheduling.CreateSessionTypeInput{
				Name: name, DurationMinutes: 60, Capacity: capacity, CreditCost: creditCost,
			})
		if err != nil {
			return err
		}
		id = st.ID
		return nil
	}); err != nil {
		t.Fatalf("create session type: %v", err)
	}
	return id
}

// ---------------------------------------------------------------------------
// Journey A, from the PRD
// ---------------------------------------------------------------------------

// Trainer marks a session Completed; the credit burns, revenue is recognised,
// the balance reaches zero, and the app has what it needs to offer a renewal.
func TestJourneyA_SessionDeliveryToBalanceBurn(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	// A client with a single credit left, bought at 50.00 a session.
	clientID, packageID := f.newClient(t, "Client A", 1, 5000)
	typeID := f.newSessionType(t, "1-on-1", 1, 1)

	// The trainer's calendar shows the session.
	var attendeeID ids.ID
	if err := f.tx(t, func(tx pgx.Tx) error {
		session, err := f.sched.Book(ctx, tx, f.tenantID, scheduling.BookInput{
			SessionTypeID: typeID, StartsAt: at9, ClientIDs: []ids.ID{clientID},
		})
		if err != nil {
			return err
		}
		if len(session.Attendees) != 1 {
			t.Fatalf("roster has %d attendees, want 1", len(session.Attendees))
		}
		attendeeID = session.Attendees[0].ID
		return nil
	}); err != nil {
		t.Fatalf("book: %v", err)
	}

	// Trainer marks it Completed.
	var result scheduling.MarkResult
	if err := f.tx(t, func(tx pgx.Tx) error {
		var err error
		result, err = f.sched.Mark(ctx, tx, f.tenantID, scheduling.MarkInput{
			AttendeeID: attendeeID, Status: scheduling.Completed,
		})
		return err
	}); err != nil {
		t.Fatalf("mark completed: %v", err)
	}

	// The balance decrements from 1 to 0 — the trigger for the PRD's prompt.
	if result.CreditsRemaining != 0 {
		t.Errorf("credits remaining = %d, want 0", result.CreditsRemaining)
	}
	if result.Attendee.Status != scheduling.Completed {
		t.Errorf("status = %q", result.Attendee.Status)
	}
	if result.Attendee.CreditsCharged != 1 {
		t.Errorf("credits charged = %d, want 1", result.Attendee.CreditsCharged)
	}

	// Revenue is recognised at the pack's per-credit price.
	if result.RevenueRecognised == nil || result.RevenueRecognised.Minor != 5000 {
		t.Fatalf("revenue recognised = %v, want 50.00 EUR", result.RevenueRecognised)
	}
	if result.JournalEntryID == nil {
		t.Fatal("no journal entry was posted for a delivered session")
	}

	if err := f.tx(t, func(tx pgx.Tx) error {
		// The journal entry is exactly DR Deferred Revenue / CR Training
		// Revenue: the obligation shrinks and the income is earned.
		entry, err := f.ledger.Get(ctx, tx, *result.JournalEntryID)
		if err != nil {
			return err
		}
		if len(entry.Lines) != 2 {
			t.Fatalf("entry has %d lines, want 2", len(entry.Lines))
		}
		byAccount := map[ledger.Slug]ledger.PostedLine{}
		for _, l := range entry.Lines {
			byAccount[l.AccountSlug] = l
		}
		if got := byAccount[ledger.SlugDeferredRevenue]; got.Debit != 5000 {
			t.Errorf("Deferred Revenue debit = %d, want 5000", got.Debit)
		}
		if got := byAccount[ledger.SlugTrainingRevenue]; got.Credit != 5000 {
			t.Errorf("Training Revenue credit = %d, want 5000", got.Credit)
		}
		// The entry is dated to the session, not to when it was tapped.
		if !entry.Date.Equal(feb2) {
			t.Errorf("entry date = %v, want the session's day %v", entry.Date, feb2)
		}

		// The credit log agrees with the pack's cached balance.
		var logged, cached int
		if err := tx.QueryRow(ctx,
			`SELECT coalesce(sum(delta),0), max(p.credits_remaining)
			   FROM credit_transactions ct JOIN packages p ON p.id = ct.package_id
			  WHERE ct.package_id = $1`, packageID).Scan(&logged, &cached); err != nil {
			return err
		}
		if logged != cached {
			t.Errorf("credit log sums to %d but the package caches %d", logged, cached)
		}

		// Nothing is left unearned, and the books balance.
		unearned, err := f.ledger.UnearnedRevenue(ctx, tx, feb2, "EUR")
		if err != nil {
			return err
		}
		if unearned.Minor != 0 {
			t.Errorf("unearned revenue = %d, want 0", unearned.Minor)
		}
		tb, err := f.ledger.TrialBalance(ctx, tx, feb2, "EUR")
		if err != nil {
			return err
		}
		if !tb.InBalance() {
			t.Errorf("trial balance broken: debits %d credits %d", tb.TotalDebits, tb.TotalCredits)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// Completing with no credits left is refused, and the error carries what the
// app needs to prompt for a renewal.
func TestCompletingWithoutCreditsIsRefused(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	clientID, _ := f.newClient(t, "Client A", 1, 5000)
	typeID := f.newSessionType(t, "1-on-1", 1, 1)

	// Burn the only credit.
	first := f.bookAndMark(t, typeID, clientID, at9, scheduling.Completed)
	if first.CreditsRemaining != 0 {
		t.Fatalf("expected the balance to reach 0, got %d", first.CreditsRemaining)
	}

	// The next session cannot be completed.
	var attendeeID ids.ID
	if err := f.tx(t, func(tx pgx.Tx) error {
		session, err := f.sched.Book(ctx, tx, f.tenantID, scheduling.BookInput{
			SessionTypeID: typeID, StartsAt: at9.Add(3 * time.Hour), ClientIDs: []ids.ID{clientID},
		})
		if err != nil {
			return err
		}
		attendeeID = session.Attendees[0].ID
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	err := f.tx(t, func(tx pgx.Tx) error {
		_, err := f.sched.Mark(ctx, tx, f.tenantID, scheduling.MarkInput{
			AttendeeID: attendeeID, Status: scheduling.Completed,
		})
		return err
	})
	if err == nil {
		t.Fatal("completing with no credits was allowed")
	}
	if errs.CodeOf(err) != errs.CodeInsufficientCredits {
		t.Fatalf("code = %q, want %q", errs.CodeOf(err), errs.CodeInsufficientCredits)
	}

	var appErr *errs.Error
	if !asError(err, &appErr) {
		t.Fatal("expected a typed error")
	}
	if appErr.Meta["remaining"] != 0 {
		t.Errorf("meta remaining = %v, want 0", appErr.Meta["remaining"])
	}
	if appErr.Meta["required"] != 1 {
		t.Errorf("meta required = %v, want 1", appErr.Meta["required"])
	}
}

// With overdraft permitted, the same completion goes through and the balance
// goes negative rather than being refused.
func TestOverdraftAllowsCompletionAndGoesNegative(t *testing.T) {
	f := setup(t, withOverdraft())
	ctx := context.Background()

	clientID, _ := f.newClient(t, "Client A", 1, 5000)
	typeID := f.newSessionType(t, "1-on-1", 1, 1)

	f.bookAndMark(t, typeID, clientID, at9, scheduling.Completed)
	second := f.bookAndMark(t, typeID, clientID, at9.Add(3*time.Hour), scheduling.Completed)

	if second.CreditsRemaining != -1 {
		t.Errorf("credits remaining = %d, want -1", second.CreditsRemaining)
	}
	if second.RevenueRecognised == nil || second.RevenueRecognised.Minor != 5000 {
		t.Errorf("overdrawn session recognised %v, want 50.00", second.RevenueRecognised)
	}

	// The overdrawn session still earns revenue, so Deferred Revenue goes
	// negative: the trainer has delivered more than was paid for.
	if err := f.tx(t, func(tx pgx.Tx) error {
		unearned, err := f.ledger.UnearnedRevenue(ctx, tx, feb2, "EUR")
		if err != nil {
			return err
		}
		if unearned.Minor != -5000 {
			t.Errorf("unearned revenue = %d, want -5000 (over-delivered)", unearned.Minor)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// bookAndMark books a session and marks its single attendee.
func (f fixture) bookAndMark(t *testing.T, typeID, clientID ids.ID, start time.Time, status scheduling.AttendanceStatus) scheduling.MarkResult {
	t.Helper()
	ctx := context.Background()
	var result scheduling.MarkResult

	if err := f.tx(t, func(tx pgx.Tx) error {
		session, err := f.sched.Book(ctx, tx, f.tenantID, scheduling.BookInput{
			SessionTypeID: typeID, StartsAt: start, ClientIDs: []ids.ID{clientID},
		})
		if err != nil {
			return err
		}
		result, err = f.sched.Mark(ctx, tx, f.tenantID, scheduling.MarkInput{
			AttendeeID: session.Attendees[0].ID, Status: status,
		})
		return err
	}); err != nil {
		t.Fatalf("book and mark %s: %v", status, err)
	}
	return result
}

// billingGrant builds a grant input, used where a test needs packs with
// specific purchase and expiry dates.
func billingGrant(clientID ids.ID, credits int, unitPrice int64, purchasedOn time.Time, expiresAt *time.Time) billing.GrantInput {
	return billing.GrantInput{
		ClientID: clientID, Name: "pack", Credits: credits,
		UnitPrice: eur(unitPrice), PurchasedOn: purchasedOn, ExpiresAt: expiresAt,
	}
}

func asError(err error, target **errs.Error) bool {
	for err != nil {
		if e, ok := err.(*errs.Error); ok {
			*target = e
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}
