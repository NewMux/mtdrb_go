//go:build integration

package ledger_test

import (
	"context"
	"math/rand"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/NewMux/mtdrb_go/internal/db"
	"github.com/NewMux/mtdrb_go/internal/ledger"
	"github.com/NewMux/mtdrb_go/internal/platform/clock"
	"github.com/NewMux/mtdrb_go/internal/platform/errs"
	"github.com/NewMux/mtdrb_go/internal/platform/ids"
	"github.com/NewMux/mtdrb_go/internal/platform/money"
	"github.com/NewMux/mtdrb_go/internal/testsupport"
)

var jan15 = time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)

func eur(minor int64) money.Money { return money.New(minor, "EUR") }

type fixture struct {
	svc      *ledger.Service
	pool     *db.Pool
	tenantID ids.ID
}

func setup(t *testing.T) fixture {
	t.Helper()
	testsupport.RequireDB(t)
	testsupport.Reset(t)

	pool := testsupport.OpenApp(t)
	svc := ledger.NewService(clock.Fixed{T: jan15})
	tenantID := ids.New()
	ctx := context.Background()

	owner := testsupport.OpenOwner(t)
	if _, err := owner.Raw().Exec(ctx,
		`INSERT INTO tenants (id, name, default_currency) VALUES ($1, 'Test Gym', 'EUR')`, tenantID); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	if err := pool.InTenantTx(ctx, tenantID, func(tx pgx.Tx) error {
		return svc.SeedChartOfAccounts(ctx, tx, tenantID, "EUR")
	}); err != nil {
		t.Fatalf("seed chart: %v", err)
	}
	return fixture{svc: svc, pool: pool, tenantID: tenantID}
}

func (f fixture) tx(t *testing.T, fn func(tx pgx.Tx) error) error {
	t.Helper()
	return f.pool.InTenantTx(context.Background(), f.tenantID, fn)
}

func TestSeedChartOfAccounts(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	if err := f.tx(t, func(tx pgx.Tx) error {
		accounts, err := f.svc.ListAccounts(ctx, tx)
		if err != nil {
			return err
		}
		if len(accounts) != 15 {
			t.Errorf("seeded %d accounts, want 15", len(accounts))
		}

		bySlug := map[ledger.Slug]bool{}
		for _, a := range accounts {
			bySlug[a.Slug] = true
			if !a.IsSystem {
				t.Errorf("%s: seeded account is not marked as a system account", a.Slug)
			}
		}
		// The account the whole accrual model depends on.
		if !bySlug[ledger.SlugDeferredRevenue] {
			t.Error("Deferred Revenue was not seeded")
		}
		// Expense accounts exist now so Phase 2 is additive.
		if !bySlug[ledger.SlugExpenseRent] {
			t.Error("expense accounts were not seeded")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// The heart of the product: a prepaid pack is a liability until trained off.
func TestPrepaidPackLifecycleRecognisesRevenueOnDelivery(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	invoiceID, paymentID := ids.New(), ids.New()

	// A ten-session pack at 50.00 each.
	const packTotal = 50000
	const perSession = 5000

	if err := f.tx(t, func(tx pgx.Tx) error {
		// 1. Invoice issued: the client owes, the trainer owes training.
		if _, err := f.svc.PostPrepaidInvoice(ctx, tx, invoiceID, eur(packTotal), jan15, "10-session pack", nil); err != nil {
			return err
		}

		receivable, err := f.svc.Receivables(ctx, tx, jan15, "EUR")
		if err != nil {
			return err
		}
		if receivable.Minor != packTotal {
			t.Errorf("after invoice: receivables = %d, want %d", receivable.Minor, packTotal)
		}
		unearned, err := f.svc.UnearnedRevenue(ctx, tx, jan15, "EUR")
		if err != nil {
			return err
		}
		if unearned.Minor != packTotal {
			t.Errorf("after invoice: unearned = %d, want %d", unearned.Minor, packTotal)
		}

		// Nothing has been earned yet — this is the assertion that separates
		// CoachPulse from a tool that treats a sale as income.
		pl, err := f.svc.ProfitAndLoss(ctx, tx, jan15, jan15, "EUR")
		if err != nil {
			return err
		}
		if pl.GrossRevenue != 0 {
			t.Errorf("after invoice: revenue = %d, want 0 (nothing delivered yet)", pl.GrossRevenue)
		}
		return nil
	}); err != nil {
		t.Fatalf("invoice stage: %v", err)
	}

	// 2. Client pays by bank transfer.
	if err := f.tx(t, func(tx pgx.Tx) error {
		if _, err := f.svc.PostPayment(ctx, tx, paymentID, eur(packTotal), ledger.InstrumentBankTransfer, jan15, "bank transfer", nil); err != nil {
			return err
		}
		receivable, err := f.svc.Receivables(ctx, tx, jan15, "EUR")
		if err != nil {
			return err
		}
		if receivable.Minor != 0 {
			t.Errorf("after payment: receivables = %d, want 0", receivable.Minor)
		}
		bank, err := f.svc.AccountBalanceAt(ctx, tx, ledger.SlugBank, jan15, "EUR")
		if err != nil {
			return err
		}
		if bank.Minor != packTotal {
			t.Errorf("after payment: bank = %d, want %d", bank.Minor, packTotal)
		}

		// Money in hand, still nothing earned.
		pl, err := f.svc.ProfitAndLoss(ctx, tx, jan15, jan15, "EUR")
		if err != nil {
			return err
		}
		if pl.GrossRevenue != 0 {
			t.Errorf("after payment: revenue = %d, want 0 (payment is not income)", pl.GrossRevenue)
		}
		return nil
	}); err != nil {
		t.Fatalf("payment stage: %v", err)
	}

	// 3. Ten sessions delivered, one at a time.
	for i := range 10 {
		day := jan15.AddDate(0, 0, i)
		if err := f.tx(t, func(tx pgx.Tx) error {
			_, err := f.svc.PostSessionDelivered(ctx, tx, ids.New(), eur(perSession), day, "session", nil)
			return err
		}); err != nil {
			t.Fatalf("session %d: %v", i, err)
		}
	}

	if err := f.tx(t, func(tx pgx.Tx) error {
		end := jan15.AddDate(0, 0, 9)

		unearned, err := f.svc.UnearnedRevenue(ctx, tx, end, "EUR")
		if err != nil {
			return err
		}
		if unearned.Minor != 0 {
			t.Errorf("after all sessions: unearned = %d, want 0", unearned.Minor)
		}

		pl, err := f.svc.ProfitAndLoss(ctx, tx, jan15, end, "EUR")
		if err != nil {
			return err
		}
		if pl.GrossRevenue != packTotal {
			t.Errorf("revenue = %d, want %d", pl.GrossRevenue, packTotal)
		}
		if pl.NetProfit != packTotal {
			t.Errorf("net profit = %d, want %d", pl.NetProfit, packTotal)
		}

		tb, err := f.svc.TrialBalance(ctx, tx, end, "EUR")
		if err != nil {
			return err
		}
		if !tb.InBalance() {
			t.Errorf("trial balance does not balance: debits %d, credits %d", tb.TotalDebits, tb.TotalCredits)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// Revenue must land in the period the session happened, not the period it was
// invoiced or paid.
func TestRevenueLandsInThePeriodItWasEarned(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	january := time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC)
	march := time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC)

	if err := f.tx(t, func(tx pgx.Tx) error {
		if _, err := f.svc.PostPrepaidInvoice(ctx, tx, ids.New(), eur(50000), january, "pack", nil); err != nil {
			return err
		}
		_, err := f.svc.PostSessionDelivered(ctx, tx, ids.New(), eur(5000), march, "session", nil)
		return err
	}); err != nil {
		t.Fatal(err)
	}

	if err := f.tx(t, func(tx pgx.Tx) error {
		janEnd := time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC)
		janPL, err := f.svc.ProfitAndLoss(ctx, tx, january, janEnd, "EUR")
		if err != nil {
			return err
		}
		if janPL.GrossRevenue != 0 {
			t.Errorf("January revenue = %d, want 0; the pack was sold but not trained", janPL.GrossRevenue)
		}

		marEnd := time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC)
		marPL, err := f.svc.ProfitAndLoss(ctx, tx, time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), marEnd, "EUR")
		if err != nil {
			return err
		}
		if marPL.GrossRevenue != 5000 {
			t.Errorf("March revenue = %d, want 5000", marPL.GrossRevenue)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestPartialPaymentsLeaveTheRemainderReceivable(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	invoiceID := ids.New()

	if err := f.tx(t, func(tx pgx.Tx) error {
		if _, err := f.svc.PostPrepaidInvoice(ctx, tx, invoiceID, eur(50000), jan15, "pack", nil); err != nil {
			return err
		}
		if _, err := f.svc.PostPayment(ctx, tx, ids.New(), eur(20000), ledger.InstrumentCash, jan15, "deposit", nil); err != nil {
			return err
		}
		if _, err := f.svc.PostPayment(ctx, tx, ids.New(), eur(15000), ledger.InstrumentBankTransfer, jan15, "part two", nil); err != nil {
			return err
		}

		owing, err := f.svc.Receivables(ctx, tx, jan15, "EUR")
		if err != nil {
			return err
		}
		if owing.Minor != 15000 {
			t.Errorf("outstanding = %d, want 15000", owing.Minor)
		}

		cash, err := f.svc.AccountBalanceAt(ctx, tx, ledger.SlugCashOnHand, jan15, "EUR")
		if err != nil {
			return err
		}
		if cash.Minor != 20000 {
			t.Errorf("cash = %d, want 20000", cash.Minor)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestReverseCancelsAnEntryWithoutMutatingIt(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	var original ledger.Posted
	if err := f.tx(t, func(tx pgx.Tx) error {
		var err error
		original, err = f.svc.PostSessionDelivered(ctx, tx, ids.New(), eur(5000), jan15, "session marked in error", nil)
		return err
	}); err != nil {
		t.Fatal(err)
	}

	if err := f.tx(t, func(tx pgx.Tx) error {
		reversal, err := f.svc.Reverse(ctx, tx, original.ID, "undo mistaken completion", nil)
		if err != nil {
			return err
		}
		if reversal.ReversesID == nil || *reversal.ReversesID != original.ID {
			t.Error("reversal does not point back at the original")
		}
		// The correction belongs in the original's period, not today's.
		if !reversal.Date.Equal(original.Date) {
			t.Errorf("reversal date = %v, want the original's %v", reversal.Date, original.Date)
		}

		// The original is untouched but now marked as reversed.
		refetched, err := f.svc.Get(ctx, tx, original.ID)
		if err != nil {
			return err
		}
		if refetched.ReversedBy == nil || *refetched.ReversedBy != reversal.ID {
			t.Error("original was not linked to its reversal")
		}
		if refetched.Memo != original.Memo {
			t.Error("original entry was mutated")
		}

		// Net effect is zero.
		pl, err := f.svc.ProfitAndLoss(ctx, tx, jan15, jan15, "EUR")
		if err != nil {
			return err
		}
		if pl.GrossRevenue != 0 {
			t.Errorf("revenue after reversal = %d, want 0", pl.GrossRevenue)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestEntryCannotBeReversedTwice(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	var original ledger.Posted
	if err := f.tx(t, func(tx pgx.Tx) error {
		var err error
		original, err = f.svc.PostSessionDelivered(ctx, tx, ids.New(), eur(5000), jan15, "session", nil)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := f.tx(t, func(tx pgx.Tx) error {
		_, err := f.svc.Reverse(ctx, tx, original.ID, "first", nil)
		return err
	}); err != nil {
		t.Fatal(err)
	}

	err := f.tx(t, func(tx pgx.Tx) error {
		_, err := f.svc.Reverse(ctx, tx, original.ID, "second", nil)
		return err
	})
	if err == nil {
		t.Fatal("entry was reversed twice; the revenue would be cancelled out twice over")
	}
	if errs.CodeOf(err) != errs.CodeImmutableEntry {
		t.Errorf("code = %q, want %q", errs.CodeOf(err), errs.CodeImmutableEntry)
	}
}

func TestReversalCannotItselfBeReversed(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	var reversal ledger.Posted
	if err := f.tx(t, func(tx pgx.Tx) error {
		original, err := f.svc.PostSessionDelivered(ctx, tx, ids.New(), eur(5000), jan15, "session", nil)
		if err != nil {
			return err
		}
		reversal, err = f.svc.Reverse(ctx, tx, original.ID, "undo", nil)
		return err
	}); err != nil {
		t.Fatal(err)
	}

	err := f.tx(t, func(tx pgx.Tx) error {
		_, err := f.svc.Reverse(ctx, tx, reversal.ID, "un-undo", nil)
		return err
	})
	if err == nil {
		t.Fatal("a reversal was itself reversed; corrections must not chain")
	}
}

// The database refuses an unbalanced entry even if application validation is
// somehow bypassed. This is the backstop that makes the ledger trustworthy.
func TestDatabaseRefusesUnbalancedEntryAtCommit(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	var accountA, accountB ids.ID
	if err := f.tx(t, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT (SELECT id FROM accounts WHERE slug = 'cash_on_hand'),
			        (SELECT id FROM accounts WHERE slug = 'training_revenue')`).Scan(&accountA, &accountB)
	}); err != nil {
		t.Fatal(err)
	}

	entryID := ids.New()
	err := f.tx(t, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx,
			`INSERT INTO journal_entries (id, tenant_id, entry_date, source_type, currency)
			 VALUES ($1, $2, $3, 'adjustment', 'EUR')`, entryID, f.tenantID, jan15); err != nil {
			return err
		}
		// One cent out.
		if _, err := tx.Exec(ctx,
			`INSERT INTO journal_lines (id, tenant_id, entry_id, account_id, debit_minor, credit_minor, currency)
			 VALUES ($1, $2, $3, $4, 50000, 0, 'EUR'), ($5, $2, $3, $6, 0, 49999, 'EUR')`,
			ids.New(), f.tenantID, entryID, accountA, ids.New(), accountB); err != nil {
			return err
		}
		return nil
	})
	if err == nil {
		t.Fatal("an unbalanced entry was committed")
	}
	if !db.IsCheckViolation(err, "") {
		t.Errorf("expected a check violation from the balance trigger, got %v", err)
	}
}

// Randomised sequences of real operations, asserting the invariants that must
// hold no matter what order a trainer does things in.
func TestLedgerInvariantsHoldUnderRandomOperationSequences(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	const seeds = 12
	for seed := range seeds {
		rng := rand.New(rand.NewSource(int64(seed) + 1))

		var (
			invoiced   int64 // total put on invoices
			collected  int64 // total money received
			delivered  int64 // total recognised as revenue
			expensed   int64
			reversible []ids.ID
		)

		ops := 25 + rng.Intn(25)
		for range ops {
			day := jan15.AddDate(0, 0, rng.Intn(60))
			amount := int64(rng.Intn(20)+1) * 500 // 5.00 to 100.00

			err := f.tx(t, func(tx pgx.Tx) error {
				switch rng.Intn(6) {
				case 0: // sell a prepaid pack
					_, err := f.svc.PostPrepaidInvoice(ctx, tx, ids.New(), eur(amount), day, "pack", nil)
					if err == nil {
						invoiced += amount
					}
					return err

				case 1: // collect money, but never more than is owed
					owing, err := f.svc.Receivables(ctx, tx, day, "EUR")
					if err != nil {
						return err
					}
					if owing.Minor <= 0 {
						return nil
					}
					pay := min(amount, owing.Minor)
					instruments := []ledger.Instrument{
						ledger.InstrumentCash, ledger.InstrumentBankTransfer,
						ledger.InstrumentCheque, ledger.InstrumentDigitalWallet,
					}
					_, err = f.svc.PostPayment(ctx, tx, ids.New(), eur(pay),
						instruments[rng.Intn(len(instruments))], day, "payment", nil)
					if err == nil {
						collected += pay
					}
					return err

				case 2, 3: // deliver a session, never more than is owed in kind
					unearned, err := f.svc.UnearnedRevenue(ctx, tx, day, "EUR")
					if err != nil {
						return err
					}
					if unearned.Minor <= 0 {
						return nil
					}
					burn := min(amount, unearned.Minor)
					posted, err := f.svc.PostSessionDelivered(ctx, tx, ids.New(), eur(burn), day, "session", nil)
					if err == nil {
						delivered += burn
						reversible = append(reversible, posted.ID)
					}
					return err

				case 4: // log an expense
					_, err := f.svc.PostExpense(ctx, tx, ids.New(), ledger.SlugExpenseRent,
						eur(amount), ledger.InstrumentBankTransfer, day, "rent", nil)
					if err == nil {
						expensed += amount
					}
					return err

				default: // undo a delivery
					if len(reversible) == 0 {
						return nil
					}
					i := rng.Intn(len(reversible))
					id := reversible[i]
					reversible = append(reversible[:i], reversible[i+1:]...)

					entry, err := f.svc.Get(ctx, tx, id)
					if err != nil {
						return err
					}
					if _, err := f.svc.Reverse(ctx, tx, id, "undo", nil); err != nil {
						return err
					}
					for _, l := range entry.Lines {
						if l.AccountSlug == ledger.SlugTrainingRevenue {
							delivered -= l.Credit
						}
					}
					return nil
				}
			})
			if err != nil {
				t.Fatalf("seed %d: operation failed: %v", seed, err)
			}
		}

		// Invariants, checked against the database rather than the counters.
		if err := f.tx(t, func(tx pgx.Tx) error {
			end := jan15.AddDate(0, 0, 60)

			tb, err := f.svc.TrialBalance(ctx, tx, end, "EUR")
			if err != nil {
				return err
			}
			if !tb.InBalance() {
				t.Errorf("seed %d: trial balance broken: debits %d, credits %d",
					seed, tb.TotalDebits, tb.TotalCredits)
			}

			pl, err := f.svc.ProfitAndLoss(ctx, tx, jan15, end, "EUR")
			if err != nil {
				return err
			}
			if pl.GrossRevenue != delivered {
				t.Errorf("seed %d: revenue = %d, want %d (only delivered sessions are income)",
					seed, pl.GrossRevenue, delivered)
			}
			if pl.OperatingExpense != expensed {
				t.Errorf("seed %d: expenses = %d, want %d", seed, pl.OperatingExpense, expensed)
			}
			if pl.NetProfit != delivered-expensed {
				t.Errorf("seed %d: net profit = %d, want %d", seed, pl.NetProfit, delivered-expensed)
			}

			// Revenue can never exceed what was invoiced: a trainer cannot
			// earn more than they sold.
			if pl.GrossRevenue > invoiced {
				t.Errorf("seed %d: recognised %d revenue against only %d invoiced",
					seed, pl.GrossRevenue, invoiced)
			}

			// Deferred Revenue is exactly what was sold but not yet trained.
			unearned, err := f.svc.UnearnedRevenue(ctx, tx, end, "EUR")
			if err != nil {
				return err
			}
			if unearned.Minor != invoiced-delivered {
				t.Errorf("seed %d: unearned = %d, want %d (invoiced %d - delivered %d)",
					seed, unearned.Minor, invoiced-delivered, invoiced, delivered)
			}
			if unearned.Minor < 0 {
				t.Errorf("seed %d: unearned revenue went negative (%d)", seed, unearned.Minor)
			}

			// Receivables are exactly what was invoiced but not yet collected.
			owing, err := f.svc.Receivables(ctx, tx, end, "EUR")
			if err != nil {
				return err
			}
			if owing.Minor != invoiced-collected {
				t.Errorf("seed %d: receivables = %d, want %d", seed, owing.Minor, invoiced-collected)
			}
			return nil
		}); err != nil {
			t.Fatalf("seed %d: invariant check failed: %v", seed, err)
		}

		testsupport.Reset(t)
		owner := testsupport.OpenOwner(t)
		if _, err := owner.Raw().Exec(ctx,
			`INSERT INTO tenants (id, name, default_currency) VALUES ($1, 'Test Gym', 'EUR')`, f.tenantID); err != nil {
			t.Fatal(err)
		}
		if err := f.tx(t, func(tx pgx.Tx) error {
			return f.svc.SeedChartOfAccounts(ctx, tx, f.tenantID, "EUR")
		}); err != nil {
			t.Fatal(err)
		}
	}
}

// Journal entries are tenant-scoped like everything else.
func TestJournalEntriesAreTenantIsolated(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	if err := f.tx(t, func(tx pgx.Tx) error {
		_, err := f.svc.PostSessionDelivered(ctx, tx, ids.New(), eur(5000), jan15, "session", nil)
		return err
	}); err != nil {
		t.Fatal(err)
	}

	other := ids.New()
	owner := testsupport.OpenOwner(t)
	if _, err := owner.Raw().Exec(ctx,
		`INSERT INTO tenants (id, name, default_currency) VALUES ($1, 'Other Gym', 'EUR')`, other); err != nil {
		t.Fatal(err)
	}
	if err := f.pool.InTenantTx(ctx, other, func(tx pgx.Tx) error {
		return f.svc.SeedChartOfAccounts(ctx, tx, other, "EUR")
	}); err != nil {
		t.Fatal(err)
	}

	if err := f.pool.InTenantTx(ctx, other, func(tx pgx.Tx) error {
		tb, err := f.svc.TrialBalance(ctx, tx, jan15, "EUR")
		if err != nil {
			return err
		}
		if tb.TotalDebits != 0 || tb.TotalCredits != 0 {
			t.Errorf("another tenant's ledger is visible: debits %d, credits %d",
				tb.TotalDebits, tb.TotalCredits)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
