//go:build integration

package billing_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/NewMux/mtdrb_go/internal/billing"
	"github.com/NewMux/mtdrb_go/internal/platform/ids"
)

// grant sells a client a pack of the given size, optionally expiring.
func (f fixture) grant(t *testing.T, clientID ids.ID, credits int, expires *time.Time) {
	t.Helper()
	if err := f.tx(t, func(tx pgx.Tx) error {
		_, err := f.svc.Grant(context.Background(), tx, f.tenantID, billing.GrantInput{
			ClientID:  clientID,
			Name:      "Pack",
			Credits:   credits,
			UnitPrice: eur(5000),
			ExpiresAt: expires,
		})
		return err
	}); err != nil {
		t.Fatalf("grant %d credits: %v", credits, err)
	}
}

func (f fixture) lowBalance(t *testing.T, threshold int) []billing.LowBalanceClient {
	t.Helper()
	var out []billing.LowBalanceClient
	if err := f.tx(t, func(tx pgx.Tx) error {
		var err error
		out, err = f.svc.LowBalance(context.Background(), tx, threshold)
		return err
	}); err != nil {
		t.Fatalf("low balance: %v", err)
	}
	return out
}

func names(in []billing.LowBalanceClient) []string {
	out := make([]string, 0, len(in))
	for _, c := range in {
		out = append(out, c.ClientName)
	}
	return out
}

func TestLowBalanceListsWhoNeedsRenewing(t *testing.T) {
	f := setup(t)

	full := f.newClient(t, "Full Freda")
	low := f.newClient(t, "Low Lena")
	empty := f.newClient(t, "Empty Emil")
	_ = f.newClient(t, "Fresh Fenna") // never bought anything

	f.grant(t, full, 10, nil)
	f.grant(t, low, 2, nil)
	f.grant(t, empty, 1, nil)

	// Burn Emil's only credit, so he sits on an exhausted pack at zero.
	if err := f.tx(t, func(tx pgx.Tx) error {
		in := billing.ConsumeInput{
			ClientID: empty, Credits: 1, Reason: billing.ReasonSessionCompleted,
		}
		plan, err := f.svc.PlanConsumption(context.Background(), tx, in)
		if err != nil {
			return err
		}
		return f.svc.ApplyConsumption(context.Background(), tx, f.tenantID, in, plan, nil)
	}); err != nil {
		t.Fatalf("burn credit: %v", err)
	}

	got := f.lowBalance(t, 2)

	// Ordered by how close to empty: the most urgent renewal first. A client
	// who never bought a pack belongs here too — they are the plainest
	// renewal of all, and a query built only from packages would miss them.
	want := []string{"Empty Emil", "Fresh Fenna", "Low Lena"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", names(got), want)
	}
	for i := range want {
		if got[i].ClientName != want[i] {
			t.Errorf("position %d = %q, want %q (full list %v)", i, got[i].ClientName, want[i], names(got))
		}
	}

	// Freda has ten and must not be on a renewal list.
	for _, c := range got {
		if c.ClientID == full {
			t.Error("a client with ten credits was flagged for renewal")
		}
	}
}

func TestLowBalanceShowsOverdraftAsNegative(t *testing.T) {
	f := setup(t)

	client := f.newClient(t, "Overdrawn Olu")
	f.grant(t, client, 1, nil)

	// Two sessions against one credit, with overdraft allowed.
	for i := 0; i < 2; i++ {
		if err := f.tx(t, func(tx pgx.Tx) error {
			in := billing.ConsumeInput{
				ClientID: client, Credits: 1, Reason: billing.ReasonSessionCompleted, AllowOverdraft: true,
			}
			plan, err := f.svc.PlanConsumption(context.Background(), tx, in)
			if err != nil {
				return err
			}
			return f.svc.ApplyConsumption(context.Background(), tx, f.tenantID, in, plan, nil)
		}); err != nil {
			t.Fatalf("burn credit %d: %v", i+1, err)
		}
	}

	got := f.lowBalance(t, 2)
	if len(got) != 1 {
		t.Fatalf("got %v, want one client", names(got))
	}
	// Reporting a debt as zero hides it from the person who most needs to see
	// it, which is the same bug the balance display had in M4.
	if got[0].Remaining != -1 {
		t.Errorf("remaining = %d, want -1", got[0].Remaining)
	}
}

func TestLowBalanceIgnoresExpiredCredits(t *testing.T) {
	f := setup(t)

	client := f.newClient(t, "Expired Esme")

	// Sold with a month's life, then the clock moves past it — a pack cannot
	// be sold already expired, so the only honest way to reach this state is
	// to let time pass.
	expires := mar1.AddDate(0, 0, 30)
	f.grant(t, client, 10, &expires)
	f.clock.T = expires.AddDate(0, 0, 1)

	// Ten credits that cannot be redeemed are not ten credits. A trainer who
	// saw "10" here would never have the conversation that matters.
	got := f.lowBalance(t, 2)
	if len(got) != 1 || got[0].Remaining != 0 {
		t.Fatalf("got %+v, want Esme at zero", got)
	}
}

func TestLowBalanceRespectsTheThreshold(t *testing.T) {
	f := setup(t)

	client := f.newClient(t, "Five Finn")
	f.grant(t, client, 5, nil)

	if got := f.lowBalance(t, 2); len(got) != 0 {
		t.Errorf("five credits flagged at threshold 2: %v", names(got))
	}
	if got := f.lowBalance(t, 5); len(got) != 1 {
		t.Errorf("five credits not flagged at threshold 5: %v", names(got))
	}
}

func TestLowBalanceExcludesArchivedClients(t *testing.T) {
	f := setup(t)

	client := f.newClient(t, "Gone Gia")
	if err := f.tx(t, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(),
			`UPDATE clients SET status = 'archived' WHERE id = $1`, client)
		return err
	}); err != nil {
		t.Fatalf("archive: %v", err)
	}

	// Chasing a renewal from someone who left is worse than useless.
	if got := f.lowBalance(t, 2); len(got) != 0 {
		t.Errorf("archived client flagged for renewal: %v", names(got))
	}
}
