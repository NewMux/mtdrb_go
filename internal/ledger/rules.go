package ledger

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/NewMux/mtdrb_go/internal/platform/errs"
	"github.com/NewMux/mtdrb_go/internal/platform/ids"
	"github.com/NewMux/mtdrb_go/internal/platform/money"
)

// This file is the complete set of posting rules — every way money can move in
// CoachPulse. Keeping them together makes the financial model reviewable in
// one sitting rather than scattered across the domain packages that trigger
// them.

// Instrument is how a payment was actually settled. CoachPulse processes no
// cards; the trainer records what happened off-platform, and the instrument
// decides which asset account is debited.
type Instrument string

const (
	InstrumentCash          Instrument = "cash"
	InstrumentBankTransfer  Instrument = "bank_transfer"
	InstrumentCheque        Instrument = "cheque"
	InstrumentDigitalWallet Instrument = "digital_wallet"
)

// AssetAccount maps a settlement instrument to the account it lands in.
func (i Instrument) AssetAccount() (Slug, error) {
	switch i {
	case InstrumentCash:
		return SlugCashOnHand, nil
	// A cheque is recorded against the bank account it will clear into.
	// Treating uncleared cheques as a separate asset is a refinement the
	// target trainers have not asked for.
	case InstrumentBankTransfer, InstrumentCheque:
		return SlugBank, nil
	case InstrumentDigitalWallet:
		return SlugDigitalWallet, nil
	default:
		return "", errs.Invalid(errs.CodeValidation, "unknown payment instrument %q", i)
	}
}

// Valid reports whether the instrument is one CoachPulse records.
func (i Instrument) Valid() bool {
	_, err := i.AssetAccount()
	return err == nil
}

// PostPrepaidInvoice records an invoice for training not yet delivered.
//
//	DR Accounts Receivable    the client owes this
//	CR Deferred Revenue       the trainer owes this much training
//
// The credit is a liability, not revenue. This is the single most important
// rule in the system: a ten-pack sold in January is not January's income, it
// is January's obligation, and it becomes income one session at a time.
func (s *Service) PostPrepaidInvoice(ctx context.Context, tx pgx.Tx, invoiceID ids.ID, amount money.Money, date time.Time, memo string, by *ids.ID) (Posted, error) {
	return s.Post(ctx, tx, Entry{
		Date:       date,
		Memo:       memo,
		SourceType: SourceInvoiceIssued,
		SourceID:   &invoiceID,
		Currency:   amount.Currency,
		CreatedBy:  by,
		Lines: []Line{
			Debit(SlugAccountsReceivable, amount, "amount owed by client"),
			Credit(SlugDeferredRevenue, amount, "unearned session credits"),
		},
	})
}

// PostServiceInvoice records an invoice for training already delivered, or for
// anything billed outright rather than prepaid.
//
//	DR Accounts Receivable
//	CR Training Revenue       earned immediately, nothing is owed in kind
func (s *Service) PostServiceInvoice(ctx context.Context, tx pgx.Tx, invoiceID ids.ID, amount money.Money, date time.Time, memo string, by *ids.ID) (Posted, error) {
	return s.Post(ctx, tx, Entry{
		Date:       date,
		Memo:       memo,
		SourceType: SourceInvoiceIssued,
		SourceID:   &invoiceID,
		Currency:   amount.Currency,
		CreatedBy:  by,
		Lines: []Line{
			Debit(SlugAccountsReceivable, amount, "amount owed by client"),
			Credit(SlugTrainingRevenue, amount, "training delivered"),
		},
	})
}

// PostPayment records money actually arriving.
//
//	DR Cash / Bank / Wallet   by instrument
//	CR Accounts Receivable    the debt is settled
//
// Note what this does not touch: revenue. Receiving money for a prepaid pack
// changes which asset holds it, not whether it has been earned.
func (s *Service) PostPayment(ctx context.Context, tx pgx.Tx, paymentID ids.ID, amount money.Money, instrument Instrument, date time.Time, memo string, by *ids.ID) (Posted, error) {
	asset, err := instrument.AssetAccount()
	if err != nil {
		return Posted{}, err
	}
	if amount.IsNegative() || amount.IsZero() {
		return Posted{}, errs.Invalid(errs.CodeValidation, "a payment must be a positive amount")
	}
	return s.Post(ctx, tx, Entry{
		Date:       date,
		Memo:       memo,
		SourceType: SourcePaymentReceived,
		SourceID:   &paymentID,
		Currency:   amount.Currency,
		CreatedBy:  by,
		Lines: []Line{
			Debit(asset, amount, string(instrument)),
			Credit(SlugAccountsReceivable, amount, "settles outstanding balance"),
		},
	})
}

// PostSessionDelivered recognises revenue for one completed session.
//
//	DR Deferred Revenue       the obligation shrinks
//	CR Training Revenue       it is now earned
//
// This fires when the trainer marks a session Completed — the moment the
// service is actually rendered, which is what accrual accounting means by
// earning it. No cash moves here; the cash arrived, or will arrive, separately.
func (s *Service) PostSessionDelivered(ctx context.Context, tx pgx.Tx, sessionID ids.ID, amount money.Money, date time.Time, memo string, by *ids.ID) (Posted, error) {
	return s.Post(ctx, tx, Entry{
		Date:       date,
		Memo:       memo,
		SourceType: SourceSessionDelivered,
		SourceID:   &sessionID,
		Currency:   amount.Currency,
		CreatedBy:  by,
		Lines: []Line{
			Debit(SlugDeferredRevenue, amount, "session delivered"),
			Credit(SlugTrainingRevenue, amount, "earned"),
		},
	})
}

// PostLateCancellation recognises revenue for a billable cancellation.
//
//	DR Deferred Revenue
//	CR Late Cancellation Revenue
//
// Economically identical to a delivered session — the credit is consumed and
// the obligation discharged — but booked to its own revenue account so a
// trainer can see how much of their income comes from cancellations.
func (s *Service) PostLateCancellation(ctx context.Context, tx pgx.Tx, sessionID ids.ID, amount money.Money, date time.Time, memo string, by *ids.ID) (Posted, error) {
	return s.Post(ctx, tx, Entry{
		Date:       date,
		Memo:       memo,
		SourceType: SourceLateCancellation,
		SourceID:   &sessionID,
		Currency:   amount.Currency,
		CreatedBy:  by,
		Lines: []Line{
			Debit(SlugDeferredRevenue, amount, "late cancellation"),
			Credit(SlugLateCancelRevenue, amount, "earned"),
		},
	})
}

// PostExpense records an operating cost.
//
//	DR <expense account>
//	CR Cash / Bank            paid now
//
// Wired up in Phase 2 when expense entry ships; the rule and its accounts
// exist now so that work adds a screen rather than a migration.
func (s *Service) PostExpense(ctx context.Context, tx pgx.Tx, expenseID ids.ID, category Slug, amount money.Money, instrument Instrument, date time.Time, memo string, by *ids.ID) (Posted, error) {
	asset, err := instrument.AssetAccount()
	if err != nil {
		return Posted{}, err
	}
	return s.Post(ctx, tx, Entry{
		Date:       date,
		Memo:       memo,
		SourceType: SourceExpense,
		SourceID:   &expenseID,
		Currency:   amount.Currency,
		CreatedBy:  by,
		Lines: []Line{
			Debit(category, amount, memo),
			Credit(asset, amount, string(instrument)),
		},
	})
}

// PostPackageExpired recognises revenue for credits that lapsed unused.
//
//	DR Deferred Revenue
//	CR Training Revenue
//
// When a pack expires the trainer no longer owes the training, so the
// liability must be discharged; leaving it on the books would understate
// profit forever and overstate what the trainer still owes their clients.
func (s *Service) PostPackageExpired(ctx context.Context, tx pgx.Tx, packageID ids.ID, amount money.Money, date time.Time, memo string, by *ids.ID) (Posted, error) {
	return s.Post(ctx, tx, Entry{
		Date:       date,
		Memo:       memo,
		SourceType: SourcePackageExpired,
		SourceID:   &packageID,
		Currency:   amount.Currency,
		CreatedBy:  by,
		Lines: []Line{
			Debit(SlugDeferredRevenue, amount, "credits expired unused"),
			Credit(SlugTrainingRevenue, amount, "recognised on expiry"),
		},
	})
}
