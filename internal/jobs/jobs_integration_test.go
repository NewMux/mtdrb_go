//go:build integration

package jobs_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/NewMux/mtdrb_go/internal/billing"
	"github.com/NewMux/mtdrb_go/internal/crm"
	"github.com/NewMux/mtdrb_go/internal/db"
	"github.com/NewMux/mtdrb_go/internal/jobs"
	"github.com/NewMux/mtdrb_go/internal/ledger"
	"github.com/NewMux/mtdrb_go/internal/platform/clock"
	"github.com/NewMux/mtdrb_go/internal/platform/ids"
	"github.com/NewMux/mtdrb_go/internal/platform/money"
	"github.com/NewMux/mtdrb_go/internal/testsupport"
)

// 00:30 on 17 June in Dubai.
var now = time.Date(2026, 6, 16, 20, 30, 0, 0, time.UTC)

type fixture struct {
	pool    *db.Pool
	billing *billing.Service
	ledger  *ledger.Service
	runner  *jobs.Runner
}

func setup(t *testing.T) fixture {
	t.Helper()
	testsupport.RequireDB(t)
	testsupport.Reset(t)
	c := clock.Fixed{T: now}
	l := ledger.NewService(c)
	b := billing.NewService(l, c)
	pool := testsupport.OpenApp(t)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return fixture{
		pool: pool, billing: b, ledger: l,
		runner: jobs.NewRunner(pool, c, log, time.Minute, jobs.PackageExpiry(b)),
	}
}

// practice creates a tenant in a time zone with one pack that expired
// yesterday with credits left, worth 600.00 for six sessions.
func (f fixture) practice(t *testing.T, zone string) ids.ID {
	t.Helper()
	ctx := context.Background()
	tenantID := ids.New()
	owner := testsupport.OpenOwner(t)
	if _, err := owner.Raw().Exec(ctx,
		`INSERT INTO tenants (id, name, default_currency, timezone) VALUES ($1, 'Studio', 'AED', $2)`,
		tenantID, zone); err != nil {
		t.Fatal(err)
	}
	people := crm.NewService(clock.Fixed{T: now}, []byte("test-column-encryption-key-32byt"))
	if err := f.pool.InTenantTx(ctx, tenantID, func(tx pgx.Tx) error {
		if err := f.ledger.SeedChartOfAccounts(ctx, tx, tenantID, "AED"); err != nil {
			return err
		}
		client, err := people.Create(ctx, tx, tenantID, crm.CreateInput{FullName: "Priya Raman"})
		if err != nil {
			return err
		}
		expired := time.Date(2026, 6, 16, 0, 0, 0, 0, time.UTC)
		value := money.New(60000, "AED")
		_, err = f.billing.Grant(ctx, tx, tenantID, billing.GrantInput{
			ClientID: client.ID, Name: "6 sessions", Credits: 6, Value: &value,
			PurchasedOn: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC), ExpiresAt: &expired,
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return tenantID
}

func (f fixture) deferred(t *testing.T, tenantID ids.ID) int64 {
	t.Helper()
	var out money.Money
	if err := f.pool.InTenantTx(context.Background(), tenantID, func(tx pgx.Tx) error {
		var err error
		// As of the end of the next day: the expiry is dated in the
		// practice's own day, which in Dubai is already the 17th.
		out, err = f.ledger.AccountBalanceAt(context.Background(), tx, ledger.SlugDeferredRevenue, now.Add(48*time.Hour), "AED")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return out.Minor
}

func TestExpiredPacksAreRetiredOnceInEachPracticesOwnDay(t *testing.T) {
	f := setup(t)
	dubai := f.practice(t, "Asia/Dubai")
	// 20:30 UTC is still the 16th in London: its pack, expiring on the 16th,
	// is good until the day is out there.
	london := f.practice(t, "Europe/London")

	if f.deferred(t, dubai) == 0 {
		t.Fatal("the fixture's pack should start as a liability")
	}

	report, err := f.runner.Tick(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if report.Ran != 2 || report.Failed != 0 {
		// Dubai's run retires its pack; London's run is due (00:15 has long
		// passed on the 16th there) but finds nothing expired yet.
		t.Fatalf("report = %+v", report)
	}
	if got := f.deferred(t, dubai); got != 0 {
		t.Fatalf("Dubai's deferred revenue should drain to exactly zero, has %d", got)
	}
	if got := f.deferred(t, london); got != 60000 {
		t.Fatalf("London's pack is not expired yet, deferred = %d", got)
	}

	// Again, as a restarted or second worker would: nothing happens twice.
	again, err := f.runner.Tick(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	fresh := jobs.NewRunner(f.pool, clock.Fixed{T: now}, slog.New(slog.NewTextHandler(io.Discard, nil)), time.Minute, jobs.PackageExpiry(f.billing))
	other, err := fresh.Tick(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if again.Ran != 0 || other.Ran != 0 {
		t.Fatalf("a run happened twice: %+v %+v", again, other)
	}
	if got := f.deferred(t, dubai); got != 0 {
		t.Fatalf("a second sweep changed the books: %d", got)
	}
}

func TestOnlyOneWorkerLeads(t *testing.T) {
	f := setup(t)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	other := jobs.NewRunner(testsupport.OpenApp(t), clock.System{}, log, time.Minute)

	led, release, err := f.runner.TryLead(context.Background())
	if err != nil || !led {
		t.Fatalf("the first worker should lead: %v %v", led, err)
	}
	if led2, _, err := other.TryLead(context.Background()); err != nil || led2 {
		t.Fatalf("a second worker took the lead as well: %v %v", led2, err)
	}
	release()
	led3, release3, err := other.TryLead(context.Background())
	if err != nil || !led3 {
		t.Fatalf("the lead should pass on once released: %v %v", led3, err)
	}
	release3()
}
