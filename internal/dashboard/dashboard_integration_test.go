//go:build integration

package dashboard_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/NewMux/mtdrb_go/internal/billing"
	"github.com/NewMux/mtdrb_go/internal/crm"
	"github.com/NewMux/mtdrb_go/internal/dashboard"
	"github.com/NewMux/mtdrb_go/internal/db"
	"github.com/NewMux/mtdrb_go/internal/ledger"
	"github.com/NewMux/mtdrb_go/internal/platform/clock"
	"github.com/NewMux/mtdrb_go/internal/platform/ids"
	"github.com/NewMux/mtdrb_go/internal/platform/money"
	"github.com/NewMux/mtdrb_go/internal/scheduling"
	"github.com/NewMux/mtdrb_go/internal/testsupport"
)

// A Wednesday, so "left this week" has days either side of it to be wrong about.
var wed = time.Date(2026, 3, 11, 8, 0, 0, 0, time.UTC)

type fixture struct {
	svc        *dashboard.Service
	billing    *billing.Service
	scheduling *scheduling.Service
	crm        *crm.Service
	pool       *db.Pool
	tenantID   ids.ID
	clock      *clock.Fixed
}

func setup(t *testing.T) fixture {
	t.Helper()
	testsupport.RequireDB(t)
	testsupport.Reset(t)

	pool := testsupport.OpenApp(t)
	c := &clock.Fixed{T: wed}
	ledgerSvc := ledger.NewService(c)
	billingSvc := billing.NewService(ledgerSvc, c)
	tenantID := ids.New()
	ctx := context.Background()

	owner := testsupport.OpenOwner(t)
	if _, err := owner.Raw().Exec(ctx,
		`INSERT INTO tenants (id, name, default_currency) VALUES ($1, 'Iron Works', 'EUR')`,
		tenantID); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}

	f := fixture{
		svc:        dashboard.NewService(billingSvc, ledgerSvc, c),
		billing:    billingSvc,
		scheduling: scheduling.NewService(billingSvc, ledgerSvc, c),
		crm:        crm.NewService(c, []byte("test-column-encryption-key-32byt")),
		pool:       pool,
		tenantID:   tenantID,
		clock:      c,
	}
	if err := f.tx(t, func(tx pgx.Tx) error {
		return ledgerSvc.SeedChartOfAccounts(ctx, tx, tenantID, "EUR")
	}); err != nil {
		t.Fatalf("seed chart: %v", err)
	}
	return f
}

func (f fixture) tx(t *testing.T, fn func(tx pgx.Tx) error) error {
	t.Helper()
	return f.pool.InTenantTx(context.Background(), f.tenantID, fn)
}

func (f fixture) summary(t *testing.T) dashboard.Summary {
	t.Helper()
	var s dashboard.Summary
	if err := f.tx(t, func(tx pgx.Tx) error {
		var err error
		s, err = f.svc.Summarise(context.Background(), tx, f.tenantID)
		return err
	}); err != nil {
		t.Fatalf("summarise: %v", err)
	}
	return s
}

func (f fixture) client(t *testing.T, name string) ids.ID {
	t.Helper()
	var id ids.ID
	if err := f.tx(t, func(tx pgx.Tx) error {
		c, err := f.crm.Create(context.Background(), tx, f.tenantID, crm.CreateInput{FullName: name})
		if err != nil {
			return err
		}
		id = c.ID
		return nil
	}); err != nil {
		t.Fatalf("create client: %v", err)
	}
	return id
}

func (f fixture) sessionType(t *testing.T) ids.ID {
	t.Helper()
	var id ids.ID
	if err := f.tx(t, func(tx pgx.Tx) error {
		st, err := f.scheduling.CreateSessionType(context.Background(), tx, f.tenantID,
			scheduling.CreateSessionTypeInput{
				Name: "1-on-1", DurationMinutes: 60, Capacity: 1, CreditCost: 1,
			})
		if err != nil {
			return err
		}
		id = st.ID
		return nil
	}); err != nil {
		t.Fatalf("session type: %v", err)
	}
	return id
}

func (f fixture) book(t *testing.T, typeID ids.ID, at time.Time, clients ...ids.ID) scheduling.Session {
	t.Helper()
	var s scheduling.Session
	if err := f.tx(t, func(tx pgx.Tx) error {
		var err error
		s, err = f.scheduling.Book(context.Background(), tx, f.tenantID, scheduling.BookInput{
			SessionTypeID: typeID, StartsAt: at, ClientIDs: clients,
		})
		return err
	}); err != nil {
		t.Fatalf("book at %s: %v", at, err)
	}
	return s
}

func TestSummaryCountsTheDayAndTheWeek(t *testing.T) {
	f := setup(t)
	typeID := f.sessionType(t)
	alice := f.client(t, "Alice")

	day := time.Date(2026, 3, 11, 0, 0, 0, 0, time.UTC)
	f.book(t, typeID, day.Add(7*time.Hour), alice)  // earlier today, already past
	f.book(t, typeID, day.Add(17*time.Hour), alice) // later today
	f.book(t, typeID, day.AddDate(0, 0, 1).Add(9*time.Hour), alice)
	f.book(t, typeID, day.AddDate(0, 0, 9).Add(9*time.Hour), alice) // next week

	s := f.summary(t)

	if s.SessionsToday != 2 {
		t.Errorf("sessions today = %d, want 2", s.SessionsToday)
	}
	// "Left this week" runs from now, so the 07:00 that already happened is
	// not left, and next week's is not this week.
	if s.SessionsLeftThisWeek != 2 {
		t.Errorf("left this week = %d, want 2 (17:00 today and tomorrow)", s.SessionsLeftThisWeek)
	}
}

func TestSummaryCountsAttendeesNotSlots(t *testing.T) {
	f := setup(t)

	var typeID ids.ID
	if err := f.tx(t, func(tx pgx.Tx) error {
		st, err := f.scheduling.CreateSessionType(context.Background(), tx, f.tenantID,
			scheduling.CreateSessionTypeInput{
				Name: "Semi-private", DurationMinutes: 60, Capacity: 3, CreditCost: 1,
			})
		if err != nil {
			return err
		}
		typeID = st.ID
		return nil
	}); err != nil {
		t.Fatalf("session type: %v", err)
	}

	a, b, c := f.client(t, "A"), f.client(t, "B"), f.client(t, "C")
	f.book(t, typeID, wed.Add(4*time.Hour), a, b, c)

	// One slot, three people: three credits and three conversations. Counting
	// slots would tell the trainer their day is a third as full as it is.
	if got := f.summary(t).SessionsToday; got != 3 {
		t.Errorf("sessions today = %d, want 3 attendees", got)
	}
}

func TestSummaryReportsEarnedRevenueNotCollectedCash(t *testing.T) {
	f := setup(t)
	typeID := f.sessionType(t)
	alice := f.client(t, "Alice")

	// Sell a ten-pack for 500. Nothing has been earned yet.
	if err := f.tx(t, func(tx pgx.Tx) error {
		_, err := f.billing.Grant(context.Background(), tx, f.tenantID, billing.GrantInput{
			ClientID: alice, Name: "10-pack", Credits: 10, UnitPrice: money.New(5000, "EUR"),
		})
		return err
	}); err != nil {
		t.Fatalf("grant: %v", err)
	}

	if got := f.summary(t).IncomeThisMonth; got.Minor != 0 {
		t.Fatalf("income after selling a pack = %s, want zero — selling is not earning", got)
	}

	// Deliver one session. Now 50 is earned.
	session := f.book(t, typeID, wed.Add(time.Hour), alice)
	if err := f.tx(t, func(tx pgx.Tx) error {
		_, err := f.scheduling.Mark(context.Background(), tx, f.tenantID, scheduling.MarkInput{
			AttendeeID: session.Attendees[0].ID, Status: scheduling.Completed,
		})
		return err
	}); err != nil {
		t.Fatalf("mark: %v", err)
	}

	s := f.summary(t)
	if s.IncomeThisMonth.Minor != 5000 {
		t.Errorf("income = %s, want 50.00 — one delivered session", s.IncomeThisMonth)
	}
}

func TestSummaryCarriesTheRenewalList(t *testing.T) {
	f := setup(t)
	alice := f.client(t, "Alice")

	if err := f.tx(t, func(tx pgx.Tx) error {
		_, err := f.billing.Grant(context.Background(), tx, f.tenantID, billing.GrantInput{
			ClientID: alice, Name: "Taster", Credits: 1, UnitPrice: money.New(5000, "EUR"),
		})
		return err
	}); err != nil {
		t.Fatalf("grant: %v", err)
	}

	s := f.summary(t)
	if s.Threshold != 2 {
		t.Errorf("default threshold = %d, want 2", s.Threshold)
	}
	if s.LowBalanceCount != 1 || len(s.LowBalance) != 1 {
		t.Fatalf("low balance = %d/%v, want one client", s.LowBalanceCount, s.LowBalance)
	}
	if s.LowBalance[0].Remaining != 1 {
		t.Errorf("remaining = %d, want 1", s.LowBalance[0].Remaining)
	}
}

func TestSummaryIsEmptyForANewPractice(t *testing.T) {
	f := setup(t)

	// A brand new account must render, not divide by zero or report nulls as
	// errors — it is the first screen anyone sees.
	s := f.summary(t)
	if s.SessionsToday != 0 || s.UnpaidInvoices != 0 || s.LowBalanceCount != 0 {
		t.Errorf("new practice is not empty: %+v", s)
	}
	if s.Currency != "EUR" {
		t.Errorf("currency = %q, want EUR", s.Currency)
	}
	if s.IncomeThisMonth.Minor != 0 {
		t.Errorf("income = %s, want zero", s.IncomeThisMonth)
	}
}
