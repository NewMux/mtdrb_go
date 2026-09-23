//go:build integration

package scheduling_test

import (
	"context"
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/NewMux/mtdrb_go/internal/billing"
	"github.com/NewMux/mtdrb_go/internal/ledger"
	"github.com/NewMux/mtdrb_go/internal/platform/ids"
	"github.com/NewMux/mtdrb_go/internal/scheduling"
)

// A pack's price rarely divides by its credits — 100.00 for three sessions,
// or 500 AED VAT-inclusive, which is 476.19 net for ten. Recognising floor(price
// / credits) per session used to strand the remainder in Deferred Revenue for
// ever, overstating what the trainer owes by a few minor units per pack. The
// credit that empties a pack now carries the remainder, whether it empties by
// delivery or by expiry, and an undo reverses it exactly.
//
// Asserted as a property: for any price and credit count, the liability ends
// at exactly zero and revenue at exactly the price.
func TestDeferredRevenueDrainsToZeroForAnyPriceAndCreditCount(t *testing.T) {
	f := setup(t, withOverdraft())
	ctx := context.Background()
	typeID := f.newSessionType(t, "1-on-1", 1, 1)

	type pack struct {
		value   int64
		credits int
		// expireAfter, when non-zero, delivers that many sessions and lets
		// the rest lapse instead of delivering them all.
		expireAfter int
	}
	cases := []pack{
		{10000, 3, 0},  // 33.33 x 3 = 99.99
		{47619, 10, 0}, // 500 AED inclusive of 5% VAT, net
		{50000, 10, 0}, // divides evenly: nothing to carry
		{2, 3, 0},      // worth less than one unit per credit
		{99999, 7, 3},  // part delivered, the rest lapses
		{10000, 3, 2},  // the lapsing credit is the emptying one
	}
	rng := rand.New(rand.NewSource(7))
	for range 8 {
		credits := 1 + rng.Intn(24)
		cases = append(cases, pack{
			value:       int64(1 + rng.Intn(200000)),
			credits:     credits,
			expireAfter: rng.Intn(credits),
		})
	}

	slot := at9
	nextSlot := func() time.Time {
		// Two hours apart clears a 60-minute session and the 15-minute buffer.
		slot = slot.Add(2 * time.Hour)
		return slot
	}

	var wantRevenue int64
	for i, c := range cases {
		t.Run(fmt.Sprintf("%d_%d_over_%d", i, c.value, c.credits), func(t *testing.T) {
			clientID, _ := f.newClient(t, fmt.Sprintf("Client %d", i), 0, 0)
			expires := feb2.AddDate(1, 0, 0)
			f.issuePack(t, clientID, c.value, c.credits, &expires)

			deliver := c.credits
			if c.expireAfter > 0 {
				deliver = c.expireAfter
			}
			var last ids.ID
			for range deliver {
				last = f.bookAndMark(t, typeID, clientID, nextSlot(), scheduling.Completed).Attendee.ID
			}

			// Undo the final delivery and redo it. When it was the emptying
			// credit, the reversal must take the remainder back with it and
			// the redo must recognise it once — not zero times, not twice.
			f.mark(t, last, scheduling.Scheduled)
			f.mark(t, last, scheduling.Completed)

			if c.expireAfter > 0 {
				if err := f.tx(t, func(tx pgx.Tx) error {
					_, err := f.billing.ExpirePackages(ctx, tx, f.tenantID, expires.AddDate(0, 0, 1), nil)
					return err
				}); err != nil {
					t.Fatalf("expire: %v", err)
				}
			}

			wantRevenue += c.value
			f.assertBooks(t, expires.AddDate(0, 0, 1), wantRevenue)
		})
	}
}

// issuePack sells a pack on an invoice, the way the app does.
func (f fixture) issuePack(t *testing.T, clientID ids.ID, value int64, credits int, expires *time.Time) {
	t.Helper()
	ctx := context.Background()
	if err := f.tx(t, func(tx pgx.Tx) error {
		draft, err := f.billing.CreateDraft(ctx, tx, f.tenantID, billing.CreateDraftInput{
			ClientID: clientID, Currency: "EUR",
			Lines: []billing.DraftLineInput{{
				Kind: billing.LinePackage, Description: "pack", Quantity: 1,
				UnitPriceMinor: value, PackageCredits: &credits, CreditsExpireOn: expires,
			}},
		})
		if err != nil {
			return err
		}
		_, err = f.billing.Issue(ctx, tx, f.tenantID, billing.IssueInput{InvoiceID: draft.ID, IssueDate: feb2})
		return err
	}); err != nil {
		t.Fatalf("issue pack: %v", err)
	}
}

func (f fixture) mark(t *testing.T, attendeeID ids.ID, status scheduling.AttendanceStatus) {
	t.Helper()
	if err := f.tx(t, func(tx pgx.Tx) error {
		_, err := f.sched.Mark(context.Background(), tx, f.tenantID, scheduling.MarkInput{
			AttendeeID: attendeeID, Status: status,
		})
		return err
	}); err != nil {
		t.Fatalf("mark %s: %v", status, err)
	}
}

func (f fixture) assertBooks(t *testing.T, asOf time.Time, wantRevenue int64) {
	t.Helper()
	ctx := context.Background()
	if err := f.tx(t, func(tx pgx.Tx) error {
		unearned, err := f.ledger.UnearnedRevenue(ctx, tx, asOf, "EUR")
		if err != nil {
			return err
		}
		if unearned.Minor != 0 {
			t.Errorf("deferred revenue = %d, want exactly 0", unearned.Minor)
		}
		earned, err := f.ledger.AccountBalanceAt(ctx, tx, ledger.SlugTrainingRevenue, asOf, "EUR")
		if err != nil {
			return err
		}
		if earned.Minor != wantRevenue {
			t.Errorf("training revenue = %d, want %d", earned.Minor, wantRevenue)
		}
		tb, err := f.ledger.TrialBalance(ctx, tx, asOf, "EUR")
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
