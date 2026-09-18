// Package ledger implements CoachPulse's double-entry accounting.
//
// Three rules hold throughout, and the rest of the package exists to keep them
// true:
//
//  1. Every journal entry balances. The database asserts this at commit, so a
//     bug here produces a failed transaction rather than a corrupt ledger.
//  2. Entries are immutable. A correction is a reversing entry, never an edit.
//  3. Revenue is recognized when a session is delivered, not when money
//     arrives. Prepaid packs sit in Deferred Revenue — a liability — until the
//     training they paid for actually happens.
//
// Nothing outside this package writes to journal_entries or journal_lines.
// Domain packages describe what happened and call Post.
package ledger

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/NewMux/mtdrb_go/internal/platform/errs"
	"github.com/NewMux/mtdrb_go/internal/platform/ids"
)

// AccountType classifies an account for reporting.
type AccountType string

const (
	TypeAsset     AccountType = "asset"
	TypeLiability AccountType = "liability"
	TypeEquity    AccountType = "equity"
	TypeRevenue   AccountType = "revenue"
	TypeExpense   AccountType = "expense"
)

// NormalBalance is the side on which an account increases.
type NormalBalance string

const (
	// DebitNormal accounts — assets and expenses — increase on the debit side.
	DebitNormal NormalBalance = "debit"
	// CreditNormal accounts — liabilities, equity and revenue — increase on
	// the credit side.
	CreditNormal NormalBalance = "credit"
)

// Slug identifies a system account independently of its display name, which a
// trainer may rename or localise. Posting rules reference accounts by slug.
type Slug string

const (
	SlugCashOnHand         Slug = "cash_on_hand"
	SlugBank               Slug = "bank"
	SlugDigitalWallet      Slug = "digital_wallet"
	SlugAccountsReceivable Slug = "accounts_receivable"
	// SlugDeferredRevenue holds training that has been paid for but not yet
	// delivered. It is the account that makes prepaid packs honest.
	SlugDeferredRevenue   Slug = "deferred_revenue"
	SlugOwnersEquity      Slug = "owners_equity"
	SlugTrainingRevenue   Slug = "training_revenue"
	SlugLateCancelRevenue Slug = "late_cancellation_revenue"

	// Expense accounts. These are seeded in Phase 1 even though expense entry
	// arrives in Phase 2, so that work is additive rather than a migration.
	SlugExpenseRent          Slug = "expense_floor_rent"
	SlugExpenseEquipment     Slug = "expense_equipment"
	SlugExpenseCertification Slug = "expense_certifications"
	SlugExpenseSupplements   Slug = "expense_supplement_inventory"
	SlugExpenseTravel        Slug = "expense_travel_mileage"
	SlugExpenseInsurance     Slug = "expense_insurance"
	SlugExpenseOther         Slug = "expense_other"
)

// Account is one line of the chart of accounts.
type Account struct {
	ID            ids.ID        `json:"id"`
	Code          string        `json:"code"`
	Name          string        `json:"name"`
	Type          AccountType   `json:"type"`
	NormalBalance NormalBalance `json:"normal_balance"`
	IsSystem      bool          `json:"is_system"`
	Slug          Slug          `json:"slug,omitempty"`
}

// systemAccount describes a seeded account.
type systemAccount struct {
	Code string
	Name string
	Type AccountType
	Slug Slug
}

// systemChart is the opening chart of accounts every tenant receives.
//
// Codes follow the conventional ranges — 1xxx assets, 2xxx liabilities,
// 3xxx equity, 4xxx revenue, 5xxx expenses — so an accountant handed the tax
// export recognises the shape immediately.
var systemChart = []systemAccount{
	{"1000", "Cash on Hand", TypeAsset, SlugCashOnHand},
	{"1010", "Bank Account", TypeAsset, SlugBank},
	{"1020", "Digital Wallet", TypeAsset, SlugDigitalWallet},
	{"1100", "Accounts Receivable", TypeAsset, SlugAccountsReceivable},

	{"2100", "Deferred Revenue — Unearned Session Credits", TypeLiability, SlugDeferredRevenue},

	{"3000", "Owner's Equity", TypeEquity, SlugOwnersEquity},

	{"4000", "Training Revenue", TypeRevenue, SlugTrainingRevenue},
	{"4100", "Late Cancellation Revenue", TypeRevenue, SlugLateCancelRevenue},

	{"5000", "Floor Rent", TypeExpense, SlugExpenseRent},
	{"5100", "Equipment", TypeExpense, SlugExpenseEquipment},
	{"5200", "Certifications", TypeExpense, SlugExpenseCertification},
	{"5300", "Supplement Inventory", TypeExpense, SlugExpenseSupplements},
	{"5400", "Travel and Mileage", TypeExpense, SlugExpenseTravel},
	{"5500", "Insurance", TypeExpense, SlugExpenseInsurance},
	{"5900", "Other Operating Expenses", TypeExpense, SlugExpenseOther},
}

// NormalBalanceFor returns the side on which the given account type increases.
func NormalBalanceFor(t AccountType) NormalBalance {
	switch t {
	case TypeAsset, TypeExpense:
		return DebitNormal
	default:
		return CreditNormal
	}
}

// SeedChartOfAccounts installs the opening chart for a new tenant.
//
// It runs inside the signup transaction: a tenant without accounts cannot post
// anything, so a half-provisioned account must not be reachable.
func (s *Service) SeedChartOfAccounts(ctx context.Context, tx pgx.Tx, tenantID ids.ID, currency string) error {
	// Inserted as one set-based statement rather than COPY: Postgres refuses
	// COPY FROM on a table with row-level security, and the app role is
	// deliberately not exempt from it (ADR 0002).
	n := len(systemChart)
	accountIDs := make([]ids.ID, n)
	codes := make([]string, n)
	names := make([]string, n)
	types := make([]string, n)
	normals := make([]string, n)
	slugs := make([]string, n)

	for i, a := range systemChart {
		accountIDs[i] = ids.New()
		codes[i] = a.Code
		names[i] = a.Name
		types[i] = string(a.Type)
		normals[i] = string(NormalBalanceFor(a.Type))
		slugs[i] = string(a.Slug)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO accounts (id, tenant_id, code, name, type, normal_balance, is_system, slug)
		SELECT a.id, $1, a.code, a.name, a.type::account_type,
		       a.normal_balance::normal_balance, true, a.slug
		  FROM unnest($2::uuid[], $3::text[], $4::text[], $5::text[], $6::text[], $7::text[])
		    AS a(id, code, name, type, normal_balance, slug)`,
		tenantID, accountIDs, codes, names, types, normals, slugs,
	); err != nil {
		return errs.Internal(err, "seed chart of accounts")
	}
	return nil
}

// resolveSlugs loads the account ids for the given slugs in one query.
//
// Posting rules name accounts by slug; this is the single place that turns
// those names into ids, so a missing system account fails loudly at the point
// of posting rather than producing a silently misfiled entry.
func resolveSlugs(ctx context.Context, tx pgx.Tx, slugs []Slug) (map[Slug]ids.ID, error) {
	if len(slugs) == 0 {
		return map[Slug]ids.ID{}, nil
	}
	wanted := make([]string, 0, len(slugs))
	for _, s := range slugs {
		wanted = append(wanted, string(s))
	}

	rows, err := tx.Query(ctx,
		`SELECT slug, id FROM accounts WHERE slug = ANY($1) AND archived_at IS NULL`, wanted)
	if err != nil {
		return nil, errs.Internal(err, "resolve accounts")
	}
	defer rows.Close()

	out := make(map[Slug]ids.ID, len(slugs))
	for rows.Next() {
		var slug string
		var id ids.ID
		if err := rows.Scan(&slug, &id); err != nil {
			return nil, errs.Internal(err, "scan account")
		}
		out[Slug(slug)] = id
	}
	if err := rows.Err(); err != nil {
		return nil, errs.Internal(err, "read accounts")
	}

	for _, s := range slugs {
		if _, ok := out[s]; !ok {
			return nil, errs.Internal(nil, "system account %q is missing from this tenant's chart of accounts", s)
		}
	}
	return out, nil
}

// ListAccounts returns the tenant's chart of accounts in code order.
func (s *Service) ListAccounts(ctx context.Context, tx pgx.Tx) ([]Account, error) {
	rows, err := tx.Query(ctx,
		`SELECT id, code, name, type::text, normal_balance::text, is_system, coalesce(slug, '')
		   FROM accounts WHERE archived_at IS NULL ORDER BY code`)
	if err != nil {
		return nil, errs.Internal(err, "list accounts")
	}
	defer rows.Close()

	var out []Account
	for rows.Next() {
		var a Account
		var typ, nb, slug string
		if err := rows.Scan(&a.ID, &a.Code, &a.Name, &typ, &nb, &a.IsSystem, &slug); err != nil {
			return nil, errs.Internal(err, "scan account")
		}
		a.Type, a.NormalBalance, a.Slug = AccountType(typ), NormalBalance(nb), Slug(slug)
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, errs.Internal(err, "read accounts")
	}
	return out, nil
}
