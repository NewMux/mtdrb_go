//go:build integration

package billing_test

import (
	"context"
	"strings"
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
	"github.com/NewMux/mtdrb_go/internal/testsupport"
)

var mar1 = time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

func eur(minor int64) money.Money { return money.New(minor, "EUR") }

type fixture struct {
	svc      *billing.Service
	ledger   *ledger.Service
	crm      *crm.Service
	pool     *db.Pool
	tenantID ids.ID
	clock    *clock.Fixed
}

func setup(t *testing.T) fixture {
	t.Helper()
	testsupport.RequireDB(t)
	testsupport.Reset(t)

	pool := testsupport.OpenApp(t)
	c := &clock.Fixed{T: mar1}
	ledgerSvc := ledger.NewService(c)
	tenantID := ids.New()
	ctx := context.Background()

	owner := testsupport.OpenOwner(t)
	if _, err := owner.Raw().Exec(ctx,
		`INSERT INTO tenants (id, name, default_currency) VALUES ($1, 'Iron Works', 'EUR')`,
		tenantID); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}

	f := fixture{
		svc:      billing.NewService(ledgerSvc, c),
		ledger:   ledgerSvc,
		crm:      crm.NewService(c, []byte("test-column-encryption-key-32byt")),
		pool:     pool,
		tenantID: tenantID,
		clock:    c,
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

func (f fixture) newClient(t *testing.T, name string) ids.ID {
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

func (f fixture) addBankMethod(t *testing.T) ids.ID {
	t.Helper()
	var id ids.ID
	if err := f.tx(t, func(tx pgx.Tx) error {
		m, err := f.svc.CreatePaymentMethod(context.Background(), tx, f.tenantID, billing.CreateMethodInput{
			Kind:  billing.MethodBankTransfer,
			Label: "Bank transfer",
			Details: billing.MethodDetails{
				AccountHolder: "Sam Coach",
				BankName:      "N26",
				IBAN:          "DE89370400440532013000",
				SwiftBIC:      "NTSBDEB1",
			},
			Instructions: "Please quote the invoice number.",
			IsDefault:    true,
		})
		if err != nil {
			return err
		}
		id = m.ID
		return nil
	}); err != nil {
		t.Fatalf("create payment method: %v", err)
	}
	return id
}

// packDraft is the common case: a ten-session pack at 50.00 each.
func (f fixture) packDraft(t *testing.T, clientID ids.ID) billing.Invoice {
	t.Helper()
	credits := 10
	var invoice billing.Invoice
	if err := f.tx(t, func(tx pgx.Tx) error {
		var err error
		invoice, err = f.svc.CreateDraft(context.Background(), tx, f.tenantID, billing.CreateDraftInput{
			ClientID: clientID,
			Lines: []billing.DraftLineInput{{
				Kind: billing.LinePackage, Description: "10-session pack",
				Quantity: 1, UnitPriceMinor: 50000, PackageCredits: &credits,
			}},
		})
		return err
	}); err != nil {
		t.Fatalf("create draft: %v", err)
	}
	return invoice
}

// ---------------------------------------------------------------------------
// Journey B, from the PRD
// ---------------------------------------------------------------------------

// Issue an invoice, share the link, the client pays off-platform, the trainer
// marks it paid by bank transfer, and the client's balance is 10 credits.
func TestJourneyB_IssueShareAndSettle(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	clientID := f.newClient(t, "Client A")
	methodID := f.addBankMethod(t)
	draft := f.packDraft(t, clientID)

	if draft.Status != billing.InvoiceDraft {
		t.Fatalf("status = %q, want draft", draft.Status)
	}
	if draft.Number != nil {
		t.Error("a draft must not consume an invoice number")
	}
	if draft.TotalMinor != 50000 {
		t.Errorf("total = %d, want 50000", draft.TotalMinor)
	}

	// 1. Trainer reviews the draft and taps Issue.
	due := mar1.AddDate(0, 0, 14)
	var issued billing.Invoice
	if err := f.tx(t, func(tx pgx.Tx) error {
		var err error
		issued, err = f.svc.Issue(ctx, tx, f.tenantID, billing.IssueInput{
			InvoiceID: draft.ID, IssueDate: mar1, DueDate: &due, PaymentMethodID: &methodID,
		})
		return err
	}); err != nil {
		t.Fatalf("issue: %v", err)
	}

	if issued.Status != billing.InvoiceIssued {
		t.Errorf("status = %q, want issued", issued.Status)
	}
	if issued.Number == nil || *issued.Number != "INV-2026-0001" {
		t.Errorf("number = %v, want INV-2026-0001", issued.Number)
	}
	if issued.BalanceMinor != 50000 {
		t.Errorf("balance = %d, want 50000", issued.BalanceMinor)
	}

	// Issuing a pack posts DR Receivable / CR Deferred Revenue — a liability,
	// not income. Nothing has been earned yet.
	if err := f.tx(t, func(tx pgx.Tx) error {
		entry, err := f.ledger.Get(ctx, tx, *issued.JournalEntryID)
		if err != nil {
			return err
		}
		byAccount := map[ledger.Slug]ledger.PostedLine{}
		for _, l := range entry.Lines {
			byAccount[l.AccountSlug] = l
		}
		if got := byAccount[ledger.SlugAccountsReceivable]; got.Debit != 50000 {
			t.Errorf("Receivable debit = %d, want 50000", got.Debit)
		}
		if got := byAccount[ledger.SlugDeferredRevenue]; got.Credit != 50000 {
			t.Errorf("Deferred Revenue credit = %d, want 50000", got.Credit)
		}

		pl, err := f.ledger.ProfitAndLoss(ctx, tx, mar1, mar1, "EUR")
		if err != nil {
			return err
		}
		if pl.GrossRevenue != 0 {
			t.Errorf("revenue on issue = %d, want 0 (nothing trained yet)", pl.GrossRevenue)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// The credits exist and are usable straight away.
	if err := f.tx(t, func(tx pgx.Tx) error {
		balance, err := f.svc.BalanceFor(ctx, tx, clientID)
		if err != nil {
			return err
		}
		if balance.Remaining != 10 {
			t.Errorf("credits = %d, want 10", balance.Remaining)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// 2. Trainer taps Share Link and sends it over WhatsApp.
	var link billing.ShareLink
	if err := f.tx(t, func(tx pgx.Tx) error {
		var err error
		link, err = f.svc.CreateShareLink(ctx, tx, issued.ID, "https://app.coachpulse.io")
		return err
	}); err != nil {
		t.Fatalf("share: %v", err)
	}
	if !strings.Contains(link.URL, "/public/invoices/") || link.Token == "" {
		t.Fatalf("share link looks wrong: %+v", link)
	}

	// The token is stored only as a hash: a database read must not yield a
	// working link to somebody's invoice.
	owner := testsupport.OpenOwner(t)
	var storedHash []byte
	if err := owner.Raw().QueryRow(ctx,
		`SELECT share_token_hash FROM invoices WHERE id = $1`, issued.ID).Scan(&storedHash); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(storedHash), link.Token) {
		t.Fatal("the share token is stored in plaintext")
	}

	// 3. The client opens the link, unauthenticated, and sees how to pay.
	resolvedID, resolvedTenant, err := f.svc.ResolveShareToken(ctx, f.pool, link.Token)
	if err != nil {
		t.Fatalf("resolve token: %v", err)
	}
	if resolvedID != issued.ID || resolvedTenant != f.tenantID {
		t.Fatal("token resolved to the wrong invoice")
	}

	if err := f.pool.InTenantTx(ctx, resolvedTenant, func(tx pgx.Tx) error {
		public, err := f.svc.PublicView(ctx, tx, resolvedID)
		if err != nil {
			return err
		}
		if public.Number != "INV-2026-0001" {
			t.Errorf("public number = %q", public.Number)
		}
		if public.BusinessName != "Iron Works" {
			t.Errorf("business name = %q", public.BusinessName)
		}
		if public.BalanceMinor != 50000 {
			t.Errorf("public balance = %d", public.BalanceMinor)
		}
		if public.Instructions == nil || len(public.Instructions.Methods) != 1 {
			t.Fatalf("no payment instructions on the shared page: %+v", public.Instructions)
		}
		if public.Instructions.Methods[0].Details.IBAN != "DE89370400440532013000" {
			t.Errorf("IBAN missing from the shared page")
		}
		// The reference is the invoice number, so the trainer can match the
		// incoming transfer.
		if public.Instructions.Reference != "INV-2026-0001" {
			t.Errorf("payment reference = %q, want the invoice number", public.Instructions.Reference)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// 4. The client transfers the money. The trainer taps Mark as Paid and
	//    selects Bank Transfer.
	var result billing.PaymentResult
	if err := f.tx(t, func(tx pgx.Tx) error {
		var err error
		result, err = f.svc.RecordPayment(ctx, tx, f.tenantID, billing.RecordPaymentInput{
			InvoiceID:  issued.ID,
			Amount:     eur(50000),
			Instrument: ledger.InstrumentBankTransfer,
			ReceivedOn: mar1.AddDate(0, 0, 2),
			Reference:  "TRF-99812",
		})
		return err
	}); err != nil {
		t.Fatalf("record payment: %v", err)
	}

	// 5. The invoice is Settled and the balance is clear.
	if result.Invoice.Status != billing.InvoiceSettled {
		t.Errorf("status = %q, want settled", result.Invoice.Status)
	}
	if result.Invoice.BalanceMinor != 0 {
		t.Errorf("balance = %d, want 0", result.Invoice.BalanceMinor)
	}
	if result.Invoice.SettledAt == nil {
		t.Error("settled_at was not recorded")
	}

	if err := f.tx(t, func(tx pgx.Tx) error {
		// The payment posts DR Bank / CR Receivable. Revenue is untouched:
		// money arriving does not earn it.
		entry, err := f.ledger.Get(ctx, tx, *result.Payment.JournalEntryID)
		if err != nil {
			return err
		}
		byAccount := map[ledger.Slug]ledger.PostedLine{}
		for _, l := range entry.Lines {
			byAccount[l.AccountSlug] = l
		}
		if got := byAccount[ledger.SlugBank]; got.Debit != 50000 {
			t.Errorf("Bank debit = %d, want 50000", got.Debit)
		}
		if got := byAccount[ledger.SlugAccountsReceivable]; got.Credit != 50000 {
			t.Errorf("Receivable credit = %d, want 50000", got.Credit)
		}

		receivables, err := f.ledger.Receivables(ctx, tx, mar1.AddDate(0, 0, 2), "EUR")
		if err != nil {
			return err
		}
		if receivables.Minor != 0 {
			t.Errorf("receivables = %d, want 0", receivables.Minor)
		}

		// Still unearned: the training has been paid for but not delivered.
		unearned, err := f.ledger.UnearnedRevenue(ctx, tx, mar1.AddDate(0, 0, 2), "EUR")
		if err != nil {
			return err
		}
		if unearned.Minor != 50000 {
			t.Errorf("unearned = %d, want 50000 — paid for but not yet trained", unearned.Minor)
		}

		pl, err := f.ledger.ProfitAndLoss(ctx, tx, mar1, mar1.AddDate(0, 0, 30), "EUR")
		if err != nil {
			return err
		}
		if pl.GrossRevenue != 0 {
			t.Errorf("revenue = %d, want 0 — a payment is not income", pl.GrossRevenue)
		}

		tb, err := f.ledger.TrialBalance(ctx, tx, mar1.AddDate(0, 0, 30), "EUR")
		if err != nil {
			return err
		}
		if !tb.InBalance() {
			t.Errorf("trial balance broken: debits %d credits %d", tb.TotalDebits, tb.TotalCredits)
		}

		// And the client has their 10 credits.
		balance, err := f.svc.BalanceFor(ctx, tx, clientID)
		if err != nil {
			return err
		}
		if balance.Remaining != 10 {
			t.Errorf("credits = %d, want 10", balance.Remaining)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// Numbers must form an unbroken run — a rolled-back issue must not burn one.
func TestInvoiceNumbersAreGapFree(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	clientID := f.newClient(t, "Client A")

	var numbers []string
	for range 5 {
		draft := f.packDraft(t, clientID)
		if err := f.tx(t, func(tx pgx.Tx) error {
			issued, err := f.svc.Issue(ctx, tx, f.tenantID, billing.IssueInput{
				InvoiceID: draft.ID, IssueDate: mar1,
			})
			if err != nil {
				return err
			}
			numbers = append(numbers, *issued.Number)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}

	// A transaction that allocates a number and then fails must give it back.
	rolledBack := f.packDraft(t, clientID)
	err := f.tx(t, func(tx pgx.Tx) error {
		if _, err := f.svc.Issue(ctx, tx, f.tenantID, billing.IssueInput{
			InvoiceID: rolledBack.ID, IssueDate: mar1,
		}); err != nil {
			return err
		}
		return context.Canceled // abort after the number was taken
	})
	if err == nil {
		t.Fatal("expected the rollback to propagate")
	}

	// The next issue continues the run without a hole.
	next := f.packDraft(t, clientID)
	if err := f.tx(t, func(tx pgx.Tx) error {
		issued, err := f.svc.Issue(ctx, tx, f.tenantID, billing.IssueInput{
			InvoiceID: next.ID, IssueDate: mar1,
		})
		if err != nil {
			return err
		}
		numbers = append(numbers, *issued.Number)
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	for i, n := range numbers {
		want := "INV-2026-000" + string(rune('1'+i))
		if n != want {
			t.Errorf("invoice %d numbered %q, want %q — the run has a gap", i, n, want)
		}
	}
}

func TestPartialPaymentsLeaveABalance(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	clientID := f.newClient(t, "Client A")
	draft := f.packDraft(t, clientID)

	var issued billing.Invoice
	if err := f.tx(t, func(tx pgx.Tx) error {
		var err error
		issued, err = f.svc.Issue(ctx, tx, f.tenantID, billing.IssueInput{
			InvoiceID: draft.ID, IssueDate: mar1,
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	if err := f.tx(t, func(tx pgx.Tx) error {
		result, err := f.svc.RecordPayment(ctx, tx, f.tenantID, billing.RecordPaymentInput{
			InvoiceID: issued.ID, Amount: eur(20000),
			Instrument: ledger.InstrumentCash, ReceivedOn: mar1,
		})
		if err != nil {
			return err
		}
		if result.Invoice.Status != billing.InvoicePartiallyPaid {
			t.Errorf("status = %q, want partially_paid", result.Invoice.Status)
		}
		if result.Invoice.BalanceMinor != 30000 {
			t.Errorf("balance = %d, want 30000", result.Invoice.BalanceMinor)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// The rest settles it.
	if err := f.tx(t, func(tx pgx.Tx) error {
		result, err := f.svc.RecordPayment(ctx, tx, f.tenantID, billing.RecordPaymentInput{
			InvoiceID: issued.ID, Amount: eur(30000),
			Instrument: ledger.InstrumentBankTransfer, ReceivedOn: mar1,
		})
		if err != nil {
			return err
		}
		if result.Invoice.Status != billing.InvoiceSettled {
			t.Errorf("status = %q, want settled", result.Invoice.Status)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// Paying more than is owed is refused: a negative receivable is not a real
// position.
func TestOverpaymentIsRefused(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	clientID := f.newClient(t, "Client A")
	draft := f.packDraft(t, clientID)

	var issued billing.Invoice
	if err := f.tx(t, func(tx pgx.Tx) error {
		var err error
		issued, err = f.svc.Issue(ctx, tx, f.tenantID, billing.IssueInput{
			InvoiceID: draft.ID, IssueDate: mar1,
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	err := f.tx(t, func(tx pgx.Tx) error {
		_, err := f.svc.RecordPayment(ctx, tx, f.tenantID, billing.RecordPaymentInput{
			InvoiceID: issued.ID, Amount: eur(60000),
			Instrument: ledger.InstrumentCash, ReceivedOn: mar1,
		})
		return err
	})
	if err == nil {
		t.Fatal("an overpayment was accepted")
	}
	if errs.CodeOf(err) != errs.CodeOverpayment {
		t.Errorf("code = %q, want %q", errs.CodeOf(err), errs.CodeOverpayment)
	}
}

func TestPaymentAgainstADraftIsRefused(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	clientID := f.newClient(t, "Client A")
	draft := f.packDraft(t, clientID)

	err := f.tx(t, func(tx pgx.Tx) error {
		_, err := f.svc.RecordPayment(ctx, tx, f.tenantID, billing.RecordPaymentInput{
			InvoiceID: draft.ID, Amount: eur(10000),
			Instrument: ledger.InstrumentCash, ReceivedOn: mar1,
		})
		return err
	})
	if err == nil {
		t.Fatal("a payment against a draft was accepted")
	}
	if errs.CodeOf(err) != errs.CodeInvoiceNotPayable {
		t.Errorf("code = %q, want %q", errs.CodeOf(err), errs.CodeInvoiceNotPayable)
	}
}

// Voiding an issued invoice reverses its entry and takes the credits with it.
func TestVoidingAnUnusedInvoiceReversesEverything(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	clientID := f.newClient(t, "Client A")
	draft := f.packDraft(t, clientID)

	var issued billing.Invoice
	if err := f.tx(t, func(tx pgx.Tx) error {
		var err error
		issued, err = f.svc.Issue(ctx, tx, f.tenantID, billing.IssueInput{
			InvoiceID: draft.ID, IssueDate: mar1,
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	if err := f.tx(t, func(tx pgx.Tx) error {
		voided, err := f.svc.Void(ctx, tx, f.tenantID, billing.VoidInput{
			InvoiceID: issued.ID, Reason: "sent to the wrong client",
		})
		if err != nil {
			return err
		}
		if voided.Status != billing.InvoiceVoid {
			t.Errorf("status = %q, want void", voided.Status)
		}

		// The receivable and the liability both unwind.
		receivables, err := f.ledger.Receivables(ctx, tx, mar1, "EUR")
		if err != nil {
			return err
		}
		if receivables.Minor != 0 {
			t.Errorf("receivables after void = %d, want 0", receivables.Minor)
		}
		unearned, err := f.ledger.UnearnedRevenue(ctx, tx, mar1, "EUR")
		if err != nil {
			return err
		}
		if unearned.Minor != 0 {
			t.Errorf("unearned after void = %d, want 0", unearned.Minor)
		}

		// The credits go with it.
		balance, err := f.svc.BalanceFor(ctx, tx, clientID)
		if err != nil {
			return err
		}
		if balance.Remaining != 0 {
			t.Errorf("credits after void = %d, want 0", balance.Remaining)
		}

		tb, err := f.ledger.TrialBalance(ctx, tx, mar1, "EUR")
		if err != nil {
			return err
		}
		if !tb.InBalance() {
			t.Error("trial balance broken after void")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// Once a payment has been taken, voiding is refused — the trainer records a
// refund instead, which is a real event with its own entry.
func TestVoidingAPaidInvoiceIsRefused(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	clientID := f.newClient(t, "Client A")
	draft := f.packDraft(t, clientID)

	var issued billing.Invoice
	if err := f.tx(t, func(tx pgx.Tx) error {
		var err error
		issued, err = f.svc.Issue(ctx, tx, f.tenantID, billing.IssueInput{
			InvoiceID: draft.ID, IssueDate: mar1,
		})
		if err != nil {
			return err
		}
		_, err = f.svc.RecordPayment(ctx, tx, f.tenantID, billing.RecordPaymentInput{
			InvoiceID: issued.ID, Amount: eur(20000),
			Instrument: ledger.InstrumentCash, ReceivedOn: mar1,
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	err := f.tx(t, func(tx pgx.Tx) error {
		_, err := f.svc.Void(ctx, tx, f.tenantID, billing.VoidInput{InvoiceID: issued.ID})
		return err
	})
	if err == nil {
		t.Fatal("a paid invoice was voided")
	}
	if errs.CodeOf(err) != errs.CodeInvalidTransition {
		t.Errorf("code = %q", errs.CodeOf(err))
	}
}

// Nor once its credits have been trained off: clawing those back would corrupt
// both ledgers.
func TestVoidingIsRefusedOnceCreditsAreUsed(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	clientID := f.newClient(t, "Client A")
	draft := f.packDraft(t, clientID)

	var issued billing.Invoice
	if err := f.tx(t, func(tx pgx.Tx) error {
		var err error
		issued, err = f.svc.Issue(ctx, tx, f.tenantID, billing.IssueInput{
			InvoiceID: draft.ID, IssueDate: mar1,
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	// Consume one credit, as a delivered session would.
	if err := f.tx(t, func(tx pgx.Tx) error {
		in := billing.ConsumeInput{ClientID: clientID, Credits: 1, Reason: billing.ReasonSessionCompleted}
		planned, err := f.svc.PlanConsumption(ctx, tx, in)
		if err != nil {
			return err
		}
		return f.svc.ApplyConsumption(ctx, tx, f.tenantID, in, planned, nil)
	}); err != nil {
		t.Fatal(err)
	}

	err := f.tx(t, func(tx pgx.Tx) error {
		_, err := f.svc.Void(ctx, tx, f.tenantID, billing.VoidInput{InvoiceID: issued.ID})
		return err
	})
	if err == nil {
		t.Fatal("an invoice with consumed credits was voided")
	}
	var appErr *errs.Error
	if ok := asErr(err, &appErr); ok && appErr.Meta["credits_consumed"] != 1 {
		t.Errorf("meta credits_consumed = %v, want 1", appErr.Meta["credits_consumed"])
	}
}

// A service line bills work already done, so it earns revenue immediately
// rather than creating a liability.
func TestServiceLineEarnsRevenueImmediately(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	clientID := f.newClient(t, "Client A")

	var issued billing.Invoice
	if err := f.tx(t, func(tx pgx.Tx) error {
		draft, err := f.svc.CreateDraft(ctx, tx, f.tenantID, billing.CreateDraftInput{
			ClientID: clientID,
			Lines: []billing.DraftLineInput{{
				Kind: billing.LineService, Description: "Programme design",
				Quantity: 1, UnitPriceMinor: 15000,
			}},
		})
		if err != nil {
			return err
		}
		issued, err = f.svc.Issue(ctx, tx, f.tenantID, billing.IssueInput{
			InvoiceID: draft.ID, IssueDate: mar1,
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	if err := f.tx(t, func(tx pgx.Tx) error {
		pl, err := f.ledger.ProfitAndLoss(ctx, tx, mar1, mar1, "EUR")
		if err != nil {
			return err
		}
		if pl.GrossRevenue != 15000 {
			t.Errorf("revenue = %d, want 15000 — a service is earned on issue", pl.GrossRevenue)
		}
		unearned, err := f.ledger.UnearnedRevenue(ctx, tx, mar1, "EUR")
		if err != nil {
			return err
		}
		if unearned.Minor != 0 {
			t.Errorf("unearned = %d, want 0 — a service creates no liability", unearned.Minor)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	_ = issued
}

// One invoice may legitimately mix both, and must still produce one entry.
func TestMixedInvoiceSplitsBetweenDeferredAndEarned(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	clientID := f.newClient(t, "Client A")
	credits := 5

	var issued billing.Invoice
	if err := f.tx(t, func(tx pgx.Tx) error {
		draft, err := f.svc.CreateDraft(ctx, tx, f.tenantID, billing.CreateDraftInput{
			ClientID: clientID,
			Lines: []billing.DraftLineInput{
				{Kind: billing.LinePackage, Description: "5-session pack",
					Quantity: 1, UnitPriceMinor: 25000, PackageCredits: &credits},
				{Kind: billing.LineService, Description: "Initial assessment",
					Quantity: 1, UnitPriceMinor: 8000},
			},
		})
		if err != nil {
			return err
		}
		issued, err = f.svc.Issue(ctx, tx, f.tenantID, billing.IssueInput{
			InvoiceID: draft.ID, IssueDate: mar1,
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	if issued.TotalMinor != 33000 {
		t.Errorf("total = %d, want 33000", issued.TotalMinor)
	}

	if err := f.tx(t, func(tx pgx.Tx) error {
		entry, err := f.ledger.Get(ctx, tx, *issued.JournalEntryID)
		if err != nil {
			return err
		}
		// One invoice, one entry — three lines, not two entries.
		if len(entry.Lines) != 3 {
			t.Fatalf("entry has %d lines, want 3", len(entry.Lines))
		}
		byAccount := map[ledger.Slug]ledger.PostedLine{}
		for _, l := range entry.Lines {
			byAccount[l.AccountSlug] = l
		}
		if got := byAccount[ledger.SlugAccountsReceivable]; got.Debit != 33000 {
			t.Errorf("Receivable debit = %d, want 33000", got.Debit)
		}
		if got := byAccount[ledger.SlugDeferredRevenue]; got.Credit != 25000 {
			t.Errorf("Deferred credit = %d, want 25000", got.Credit)
		}
		if got := byAccount[ledger.SlugTrainingRevenue]; got.Credit != 8000 {
			t.Errorf("Revenue credit = %d, want 8000", got.Credit)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func asErr(err error, target **errs.Error) bool {
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
