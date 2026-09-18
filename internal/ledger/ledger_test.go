package ledger

import (
	"testing"
	"time"

	"github.com/NewMux/mtdrb_go/internal/platform/clock"
	"github.com/NewMux/mtdrb_go/internal/platform/errs"
	"github.com/NewMux/mtdrb_go/internal/platform/money"
)

func eur(minor int64) money.Money { return money.New(minor, "EUR") }

func testService() *Service {
	return NewService(clock.Fixed{T: time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)})
}

// Validation runs before the database sees anything, so a caller gets a precise
// typed error rather than a constraint violation at commit.
func TestValidateRejectsUnbalancedEntries(t *testing.T) {
	s := testService()
	cases := map[string]Entry{
		"debits exceed credits": {
			Currency: "EUR", SourceType: SourceAdjustment,
			Lines: []Line{
				Debit(SlugCashOnHand, eur(50000), ""),
				Credit(SlugTrainingRevenue, eur(49999), ""),
			},
		},
		"credits exceed debits": {
			Currency: "EUR", SourceType: SourceAdjustment,
			Lines: []Line{
				Debit(SlugCashOnHand, eur(100), ""),
				Credit(SlugTrainingRevenue, eur(101), ""),
			},
		},
	}
	for name, e := range cases {
		err := s.validate(&e)
		if err == nil {
			t.Errorf("%s: unbalanced entry passed validation", name)
			continue
		}
		if errs.CodeOf(err) != errs.CodeUnbalancedEntry {
			t.Errorf("%s: code = %q, want %q", name, errs.CodeOf(err), errs.CodeUnbalancedEntry)
		}
	}
}

func TestValidateRejectsMalformedLines(t *testing.T) {
	s := testService()
	cases := map[string]Entry{
		"single line": {
			Currency: "EUR", SourceType: SourceAdjustment,
			Lines: []Line{Debit(SlugCashOnHand, eur(100), "")},
		},
		"no lines": {Currency: "EUR", SourceType: SourceAdjustment},
		"line with no amount": {
			Currency: "EUR", SourceType: SourceAdjustment,
			Lines: []Line{
				{Account: SlugCashOnHand, Debit: eur(0), Credit: eur(0)},
				Credit(SlugTrainingRevenue, eur(100), ""),
			},
		},
		"line that is both sides": {
			Currency: "EUR", SourceType: SourceAdjustment,
			Lines: []Line{
				{Account: SlugCashOnHand, Debit: eur(100), Credit: eur(100)},
				Credit(SlugTrainingRevenue, eur(100), ""),
			},
		},
		"negative amount": {
			Currency: "EUR", SourceType: SourceAdjustment,
			Lines: []Line{
				Debit(SlugCashOnHand, eur(-100), ""),
				Credit(SlugTrainingRevenue, eur(-100), ""),
			},
		},
		"mixed currency": {
			Currency: "EUR", SourceType: SourceAdjustment,
			Lines: []Line{
				Debit(SlugCashOnHand, money.New(100, "USD"), ""),
				Credit(SlugTrainingRevenue, eur(100), ""),
			},
		},
		"missing account": {
			Currency: "EUR", SourceType: SourceAdjustment,
			Lines: []Line{
				{Debit: eur(100)},
				Credit(SlugTrainingRevenue, eur(100), ""),
			},
		},
		"missing currency": {
			SourceType: SourceAdjustment,
			Lines: []Line{
				Debit(SlugCashOnHand, eur(100), ""),
				Credit(SlugTrainingRevenue, eur(100), ""),
			},
		},
		"missing source type": {
			Currency: "EUR",
			Lines: []Line{
				Debit(SlugCashOnHand, eur(100), ""),
				Credit(SlugTrainingRevenue, eur(100), ""),
			},
		},
	}
	for name, e := range cases {
		if err := s.validate(&e); err == nil {
			t.Errorf("%s: passed validation but should not have", name)
		}
	}
}

func TestValidateAcceptsBalancedMultiLineEntry(t *testing.T) {
	s := testService()
	e := Entry{
		Currency: "EUR", SourceType: SourcePaymentReceived,
		Lines: []Line{
			Debit(SlugCashOnHand, eur(30000), "cash"),
			Debit(SlugBank, eur(20000), "transfer"),
			Credit(SlugAccountsReceivable, eur(50000), "settles invoice"),
		},
	}
	if err := s.validate(&e); err != nil {
		t.Fatalf("balanced split entry rejected: %v", err)
	}
}

// entry_date is a date; carrying a time-of-day invites reports that disagree
// across timezones.
func TestValidateNormalizesDateToCalendarDay(t *testing.T) {
	s := testService()
	e := Entry{
		Date:     time.Date(2026, 3, 9, 23, 45, 12, 500, time.FixedZone("UTC+5", 5*3600)),
		Currency: "EUR", SourceType: SourceAdjustment,
		Lines: []Line{
			Debit(SlugCashOnHand, eur(100), ""),
			Credit(SlugTrainingRevenue, eur(100), ""),
		},
	}
	if err := s.validate(&e); err != nil {
		t.Fatal(err)
	}
	if h, m, sec := e.Date.Clock(); h != 0 || m != 0 || sec != 0 {
		t.Errorf("date not truncated to midnight: %v", e.Date)
	}
	if e.Date.Location() != time.UTC {
		t.Errorf("date not normalised to UTC: %v", e.Date.Location())
	}
}

func TestValidateDefaultsDateToNow(t *testing.T) {
	s := testService()
	e := Entry{
		Currency: "EUR", SourceType: SourceAdjustment,
		Lines: []Line{
			Debit(SlugCashOnHand, eur(100), ""),
			Credit(SlugTrainingRevenue, eur(100), ""),
		},
	}
	if err := s.validate(&e); err != nil {
		t.Fatal(err)
	}
	if e.Date.IsZero() {
		t.Fatal("date was left unset")
	}
	if y, m, d := e.Date.Date(); y != 2026 || m != time.January || d != 15 {
		t.Errorf("date = %v, want the injected clock's day", e.Date)
	}
}

func TestInstrumentMapsToAssetAccount(t *testing.T) {
	cases := map[Instrument]Slug{
		InstrumentCash:          SlugCashOnHand,
		InstrumentBankTransfer:  SlugBank,
		InstrumentCheque:        SlugBank,
		InstrumentDigitalWallet: SlugDigitalWallet,
	}
	for instrument, want := range cases {
		got, err := instrument.AssetAccount()
		if err != nil {
			t.Errorf("%s: %v", instrument, err)
			continue
		}
		if got != want {
			t.Errorf("%s: account = %q, want %q", instrument, got, want)
		}
		if !instrument.Valid() {
			t.Errorf("%s: reported invalid", instrument)
		}
	}

	if _, err := Instrument("crypto").AssetAccount(); err == nil {
		t.Error("unknown instrument was accepted")
	}
	if Instrument("").Valid() {
		t.Error("empty instrument reported valid")
	}
}

func TestNormalBalanceFollowsAccountType(t *testing.T) {
	cases := map[AccountType]NormalBalance{
		TypeAsset:     DebitNormal,
		TypeExpense:   DebitNormal,
		TypeLiability: CreditNormal,
		TypeEquity:    CreditNormal,
		TypeRevenue:   CreditNormal,
	}
	for typ, want := range cases {
		if got := NormalBalanceFor(typ); got != want {
			t.Errorf("%s: got %q, want %q", typ, got, want)
		}
	}
}

// Every system account referenced by a posting rule must exist in the seeded
// chart, or that rule would fail at runtime for every tenant.
func TestSystemChartCoversEveryPostingRuleAccount(t *testing.T) {
	seeded := make(map[Slug]bool, len(systemChart))
	for _, a := range systemChart {
		if seeded[a.Slug] {
			t.Errorf("duplicate slug in chart: %q", a.Slug)
		}
		seeded[a.Slug] = true
	}

	used := []Slug{
		SlugCashOnHand, SlugBank, SlugDigitalWallet,
		SlugAccountsReceivable, SlugDeferredRevenue, SlugOwnersEquity,
		SlugTrainingRevenue, SlugLateCancelRevenue,
		SlugExpenseRent, SlugExpenseEquipment, SlugExpenseCertification,
		SlugExpenseSupplements, SlugExpenseTravel, SlugExpenseInsurance,
		SlugExpenseOther,
	}
	for _, slug := range used {
		if !seeded[slug] {
			t.Errorf("posting rules reference %q but the chart does not seed it", slug)
		}
	}
}

func TestSystemChartCodesAreUniqueAndWellFormed(t *testing.T) {
	seen := map[string]bool{}
	for _, a := range systemChart {
		if seen[a.Code] {
			t.Errorf("duplicate account code %q", a.Code)
		}
		seen[a.Code] = true

		if len(a.Code) != 4 {
			t.Errorf("%s: code %q must be four digits to satisfy the schema check", a.Slug, a.Code)
		}
		// Conventional ranges keep the tax export legible to an accountant.
		wantPrefix := map[AccountType]byte{
			TypeAsset: '1', TypeLiability: '2', TypeEquity: '3',
			TypeRevenue: '4', TypeExpense: '5',
		}[a.Type]
		if a.Code[0] != wantPrefix {
			t.Errorf("%s: code %q does not match the %s range (%c000s)", a.Slug, a.Code, a.Type, wantPrefix)
		}
	}
}

func TestTrialBalanceInBalance(t *testing.T) {
	if !(TrialBalance{TotalDebits: 100, TotalCredits: 100}).InBalance() {
		t.Error("equal totals reported out of balance")
	}
	if (TrialBalance{TotalDebits: 100, TotalCredits: 99}).InBalance() {
		t.Error("unequal totals reported in balance")
	}
}
