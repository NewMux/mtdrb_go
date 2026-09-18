package ledger

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/NewMux/mtdrb_go/internal/db"
	"github.com/NewMux/mtdrb_go/internal/platform/errs"
	"github.com/NewMux/mtdrb_go/internal/platform/ids"
	"github.com/NewMux/mtdrb_go/internal/platform/money"
)

// Every figure here is computed from journal_lines. Nothing is cached, and no
// running total is maintained anywhere, so a report cannot drift out of step
// with the entries behind it.

// AccountBalance is one account's position.
type AccountBalance struct {
	AccountID ids.ID      `json:"account_id"`
	Code      string      `json:"code"`
	Name      string      `json:"name"`
	Type      AccountType `json:"type"`
	Slug      Slug        `json:"slug,omitempty"`
	Debits    int64       `json:"debits_minor"`
	Credits   int64       `json:"credits_minor"`
	// Balance is signed against the account's normal side, so a positive
	// figure always means "more of what this account is for".
	Balance int64 `json:"balance_minor"`
}

// TrialBalance is every account's position as at a date.
//
// Its defining property is that total debits equal total credits. If they ever
// diverge, the ledger is corrupt — which is why it is asserted in tests rather
// than merely displayed.
type TrialBalance struct {
	AsOf         time.Time        `json:"as_of"`
	Currency     string           `json:"currency"`
	Accounts     []AccountBalance `json:"accounts"`
	TotalDebits  int64            `json:"total_debits_minor"`
	TotalCredits int64            `json:"total_credits_minor"`
}

// InBalance reports whether debits equal credits.
func (t TrialBalance) InBalance() bool { return t.TotalDebits == t.TotalCredits }

// TrialBalance computes every account's balance up to and including asOf.
func (s *Service) TrialBalance(ctx context.Context, tx pgx.Tx, asOf time.Time, currency string) (TrialBalance, error) {
	// The date filter lives in the aggregate's FILTER clause, not in WHERE:
	// filtering rows out would drop an account whose only entries are dated
	// after asOf, silently omitting it from the report instead of showing the
	// zero balance it correctly has on that date.
	rows, err := tx.Query(ctx, `
		SELECT a.id, a.code, a.name, a.type::text, coalesce(a.slug, ''), a.normal_balance::text,
		       coalesce(sum(l.debit_minor)  FILTER (WHERE e.id IS NOT NULL), 0),
		       coalesce(sum(l.credit_minor) FILTER (WHERE e.id IS NOT NULL), 0)
		  FROM accounts a
		  LEFT JOIN journal_lines l ON l.account_id = a.id
		  LEFT JOIN journal_entries e ON e.id = l.entry_id AND e.entry_date <= $1
		 WHERE a.archived_at IS NULL
		 GROUP BY a.id, a.code, a.name, a.type, a.slug, a.normal_balance
		 ORDER BY a.code`, asOf)
	if err != nil {
		return TrialBalance{}, errs.Internal(err, "compute trial balance")
	}
	defer rows.Close()

	tb := TrialBalance{AsOf: asOf, Currency: currency}
	for rows.Next() {
		var b AccountBalance
		var typ, slug, normal string
		if err := rows.Scan(&b.AccountID, &b.Code, &b.Name, &typ, &slug, &normal, &b.Debits, &b.Credits); err != nil {
			return TrialBalance{}, errs.Internal(err, "scan account balance")
		}
		b.Type, b.Slug = AccountType(typ), Slug(slug)
		if NormalBalance(normal) == DebitNormal {
			b.Balance = b.Debits - b.Credits
		} else {
			b.Balance = b.Credits - b.Debits
		}
		tb.TotalDebits += b.Debits
		tb.TotalCredits += b.Credits
		tb.Accounts = append(tb.Accounts, b)
	}
	if err := rows.Err(); err != nil {
		return TrialBalance{}, errs.Internal(err, "read trial balance")
	}
	return tb, nil
}

// ProfitAndLoss summarises trading over a period.
type ProfitAndLoss struct {
	From             time.Time        `json:"from"`
	To               time.Time        `json:"to"`
	Currency         string           `json:"currency"`
	Revenue          []AccountBalance `json:"revenue"`
	Expenses         []AccountBalance `json:"expenses"`
	GrossRevenue     int64            `json:"gross_revenue_minor"`
	OperatingExpense int64            `json:"operating_expenses_minor"`
	NetProfit        int64            `json:"net_profit_minor"`
}

// ProfitAndLoss computes revenue, expenses and net profit over a period.
//
// Revenue here is what was *earned* in the window — sessions actually
// delivered — not what was invoiced or collected. A trainer who sells five
// ten-packs in one week and trains nobody has earned nothing, and this says so.
func (s *Service) ProfitAndLoss(ctx context.Context, tx pgx.Tx, from, to time.Time, currency string) (ProfitAndLoss, error) {
	rows, err := tx.Query(ctx, `
		SELECT a.id, a.code, a.name, a.type::text, coalesce(a.slug, ''),
		       coalesce(sum(l.debit_minor), 0), coalesce(sum(l.credit_minor), 0)
		  FROM accounts a
		  JOIN journal_lines l ON l.account_id = a.id
		  JOIN journal_entries e ON e.id = l.entry_id
		 WHERE a.type IN ('revenue', 'expense')
		   AND e.entry_date >= $1 AND e.entry_date <= $2
		 GROUP BY a.id, a.code, a.name, a.type, a.slug
		 ORDER BY a.code`, from, to)
	if err != nil {
		return ProfitAndLoss{}, errs.Internal(err, "compute profit and loss")
	}
	defer rows.Close()

	pl := ProfitAndLoss{From: from, To: to, Currency: currency}
	for rows.Next() {
		var b AccountBalance
		var typ, slug string
		if err := rows.Scan(&b.AccountID, &b.Code, &b.Name, &typ, &slug, &b.Debits, &b.Credits); err != nil {
			return ProfitAndLoss{}, errs.Internal(err, "scan profit and loss row")
		}
		b.Type, b.Slug = AccountType(typ), Slug(slug)

		if b.Type == TypeRevenue {
			// Revenue is credit-normal; a debit against it is a refund or a
			// reversal and correctly reduces the total.
			b.Balance = b.Credits - b.Debits
			pl.GrossRevenue += b.Balance
			pl.Revenue = append(pl.Revenue, b)
		} else {
			b.Balance = b.Debits - b.Credits
			pl.OperatingExpense += b.Balance
			pl.Expenses = append(pl.Expenses, b)
		}
	}
	if err := rows.Err(); err != nil {
		return ProfitAndLoss{}, errs.Internal(err, "read profit and loss")
	}

	pl.NetProfit = pl.GrossRevenue - pl.OperatingExpense
	return pl, nil
}

// AccountBalanceAt returns a single account's balance, signed against its
// normal side.
func (s *Service) AccountBalanceAt(ctx context.Context, tx pgx.Tx, slug Slug, asOf time.Time, currency string) (money.Money, error) {
	var debits, credits int64
	var normal string
	// As above: an account with no qualifying lines must report zero, not
	// vanish. Filtering in WHERE would return no rows at all and turn an
	// ordinary zero balance into an error.
	err := tx.QueryRow(ctx, `
		SELECT a.normal_balance::text,
		       coalesce(sum(l.debit_minor)  FILTER (WHERE e.id IS NOT NULL), 0),
		       coalesce(sum(l.credit_minor) FILTER (WHERE e.id IS NOT NULL), 0)
		  FROM accounts a
		  LEFT JOIN journal_lines l ON l.account_id = a.id
		  LEFT JOIN journal_entries e ON e.id = l.entry_id AND e.entry_date <= $2
		 WHERE a.slug = $1 AND a.archived_at IS NULL
		 GROUP BY a.normal_balance`, string(slug), asOf).Scan(&normal, &debits, &credits)
	if err != nil {
		if db.IsNoRows(err) {
			return money.Money{}, errs.Internal(nil, "account %q is not in this tenant's chart of accounts", slug)
		}
		return money.Money{}, errs.Internal(err, "read balance for %s", slug)
	}
	if NormalBalance(normal) == DebitNormal {
		return money.New(debits-credits, currency), nil
	}
	return money.New(credits-debits, currency), nil
}

// UnearnedRevenue is what the trainer still owes in training they have been
// paid for — the Deferred Revenue liability.
//
// The PRD does not name this figure, but it is the one a trainer most needs
// and cannot get from a spreadsheet: how much work is already sold.
func (s *Service) UnearnedRevenue(ctx context.Context, tx pgx.Tx, asOf time.Time, currency string) (money.Money, error) {
	return s.AccountBalanceAt(ctx, tx, SlugDeferredRevenue, asOf, currency)
}

// Receivables is the total currently owed by clients.
func (s *Service) Receivables(ctx context.Context, tx pgx.Tx, asOf time.Time, currency string) (money.Money, error) {
	return s.AccountBalanceAt(ctx, tx, SlugAccountsReceivable, asOf, currency)
}
