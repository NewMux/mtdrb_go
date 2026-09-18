package ledger

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/NewMux/mtdrb_go/internal/db"
	"github.com/NewMux/mtdrb_go/internal/platform/clock"
	"github.com/NewMux/mtdrb_go/internal/platform/errs"
	"github.com/NewMux/mtdrb_go/internal/platform/ids"
	"github.com/NewMux/mtdrb_go/internal/platform/money"
)

// SourceType names the business event an entry records.
type SourceType string

const (
	SourceInvoiceIssued    SourceType = "invoice_issued"
	SourcePaymentReceived  SourceType = "payment_received"
	SourceSessionDelivered SourceType = "session_delivered"
	SourceLateCancellation SourceType = "late_cancellation"
	SourceExpense          SourceType = "expense"
	SourcePackageExpired   SourceType = "package_expired"
	SourceAdjustment       SourceType = "adjustment"
	SourceOpeningBalance   SourceType = "opening_balance"
)

// Service posts and reads journal entries.
type Service struct {
	clock clock.Clock
}

// NewService builds the ledger service.
func NewService(c clock.Clock) *Service {
	if c == nil {
		c = clock.System{}
	}
	return &Service{clock: c}
}

// Line is one side of an entry, expressed against a named system account.
//
// Callers state a debit or a credit explicitly rather than a signed amount:
// "negative credit" is not a concept an accountant would recognise, and
// allowing it invites sign errors that balance arithmetically while recording
// the opposite of what happened.
type Line struct {
	Account Slug
	Debit   money.Money
	Credit  money.Money
	Memo    string
}

// Debit builds a debit line.
func Debit(account Slug, amount money.Money, memo string) Line {
	return Line{Account: account, Debit: amount, Memo: memo}
}

// Credit builds a credit line.
func Credit(account Slug, amount money.Money, memo string) Line {
	return Line{Account: account, Credit: amount, Memo: memo}
}

// Entry is a complete, balanced accounting event awaiting posting.
type Entry struct {
	// Date is the day the event belongs to for reporting, which is not
	// necessarily today: cash logged on Monday for Friday's session belongs in
	// Friday's figures.
	Date       time.Time
	Memo       string
	SourceType SourceType
	SourceID   *ids.ID
	Currency   string
	CreatedBy  *ids.ID
	Lines      []Line
}

// Posted is a journal entry as written.
type Posted struct {
	ID         ids.ID       `json:"id"`
	Date       time.Time    `json:"entry_date"`
	Memo       string       `json:"memo"`
	SourceType SourceType   `json:"source_type"`
	SourceID   *ids.ID      `json:"source_id,omitempty"`
	Currency   string       `json:"currency"`
	Lines      []PostedLine `json:"lines"`
	ReversedBy *ids.ID      `json:"reversed_by,omitempty"`
	ReversesID *ids.ID      `json:"reverses_id,omitempty"`
}

// PostedLine is one written line.
type PostedLine struct {
	AccountID   ids.ID `json:"account_id"`
	AccountCode string `json:"account_code"`
	AccountSlug Slug   `json:"account_slug,omitempty"`
	Debit       int64  `json:"debit_minor"`
	Credit      int64  `json:"credit_minor"`
	Memo        string `json:"memo,omitempty"`
}

// Post writes a balanced journal entry.
//
// This is the only function in CoachPulse that writes to journal_entries or
// journal_lines. Domain packages call it inside their own transaction, so the
// business change and its accounting consequence commit together or not at
// all — a burned session credit without its revenue recognition, or a recorded
// payment without its cash debit, are states that cannot exist.
func (s *Service) Post(ctx context.Context, tx pgx.Tx, e Entry) (Posted, error) {
	if err := s.validate(&e); err != nil {
		return Posted{}, err
	}

	entryID := ids.New()
	tenantID, err := currentTenant(ctx, tx)
	if err != nil {
		return Posted{}, err
	}

	slugs := make([]Slug, 0, len(e.Lines))
	for _, l := range e.Lines {
		slugs = append(slugs, l.Account)
	}
	accounts, err := resolveSlugs(ctx, tx, slugs)
	if err != nil {
		return Posted{}, err
	}

	if _, err := tx.Exec(ctx,
		`INSERT INTO journal_entries (id, tenant_id, entry_date, memo, source_type, source_id, currency, created_by)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		entryID, tenantID, e.Date, e.Memo, string(e.SourceType), e.SourceID, e.Currency, e.CreatedBy,
	); err != nil {
		return Posted{}, errs.Internal(err, "insert journal entry")
	}

	lineIDs := make([]ids.ID, len(e.Lines))
	accountIDs := make([]ids.ID, len(e.Lines))
	debits := make([]int64, len(e.Lines))
	credits := make([]int64, len(e.Lines))
	memos := make([]string, len(e.Lines))
	for i, l := range e.Lines {
		lineIDs[i] = ids.New()
		accountIDs[i] = accounts[l.Account]
		debits[i] = l.Debit.Minor
		credits[i] = l.Credit.Minor
		memos[i] = l.Memo
	}

	if err := insertLines(ctx, tx, tenantID, entryID, e.Currency,
		lineIDs, accountIDs, debits, credits, memos); err != nil {
		return Posted{}, err
	}

	return s.Get(ctx, tx, entryID)
}

// validate checks an entry before it reaches the database.
//
// The database is the authority on balance, but failing here gives a caller a
// precise, typed error instead of a constraint violation at commit — by which
// point the failing statement is long out of scope.
func (s *Service) validate(e *Entry) error {
	if len(e.Lines) < 2 {
		return errs.Invalid(errs.CodeUnbalancedEntry,
			"a journal entry needs at least two lines, got %d", len(e.Lines))
	}
	if e.Currency == "" {
		return errs.Invalid(errs.CodeValidation, "a journal entry needs a currency")
	}
	if e.SourceType == "" {
		return errs.Invalid(errs.CodeValidation, "a journal entry needs a source type")
	}
	if e.Date.IsZero() {
		e.Date = s.clock.Now()
	}
	// Normalise to the calendar day: entry_date is a date, and carrying a
	// time-of-day here only invites timezone drift in reports.
	e.Date = time.Date(e.Date.Year(), e.Date.Month(), e.Date.Day(), 0, 0, 0, 0, time.UTC)

	var debits, credits int64
	for i, l := range e.Lines {
		switch {
		case l.Debit.IsZero() && l.Credit.IsZero():
			return errs.Invalid(errs.CodeUnbalancedEntry, "line %d has no amount", i)
		case !l.Debit.IsZero() && !l.Credit.IsZero():
			return errs.Invalid(errs.CodeUnbalancedEntry, "line %d is both a debit and a credit", i)
		case l.Debit.IsNegative() || l.Credit.IsNegative():
			// A negative debit is a credit wearing a disguise. Refusing it
			// keeps the sign convention unambiguous.
			return errs.Invalid(errs.CodeUnbalancedEntry,
				"line %d has a negative amount; use the opposite side instead", i)
		case l.Account == "":
			return errs.Invalid(errs.CodeValidation, "line %d names no account", i)
		}

		lineCurrency := l.Debit.Currency
		if l.Debit.IsZero() {
			lineCurrency = l.Credit.Currency
		}
		if lineCurrency != e.Currency {
			return errs.Invalid(errs.CodeValidation,
				"line %d is in %s but the entry is in %s", i, lineCurrency, e.Currency)
		}

		debits += l.Debit.Minor
		credits += l.Credit.Minor
	}

	if debits != credits {
		return errs.Invalid(errs.CodeUnbalancedEntry,
			"entry does not balance: debits %d, credits %d", debits, credits)
	}
	return nil
}

// Reverse cancels an entry by posting its mirror image.
//
// The original is left untouched, which is the point: a trainer who marks the
// wrong session complete needs the correction to be visible, not for history
// to quietly change shape. The reversal carries the original's date so the
// correction lands in the period it belongs to rather than distorting two.
func (s *Service) Reverse(ctx context.Context, tx pgx.Tx, entryID ids.ID, memo string, by *ids.ID) (Posted, error) {
	original, err := s.Get(ctx, tx, entryID)
	if err != nil {
		return Posted{}, err
	}
	if original.ReversedBy != nil {
		return Posted{}, errs.Conflict(errs.CodeImmutableEntry,
			"this entry has already been reversed")
	}
	if original.ReversesID != nil {
		return Posted{}, errs.Conflict(errs.CodeImmutableEntry,
			"a reversing entry cannot itself be reversed; post an adjustment instead")
	}

	tenantID, err := currentTenant(ctx, tx)
	if err != nil {
		return Posted{}, err
	}

	if memo == "" {
		memo = "Reversal of " + original.Memo
	}
	reversalID := ids.New()

	if _, err := tx.Exec(ctx,
		`INSERT INTO journal_entries
		   (id, tenant_id, entry_date, memo, source_type, source_id, currency, reverses_id, created_by)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		reversalID, tenantID, original.Date, memo, string(original.SourceType),
		original.SourceID, original.Currency, original.ID, by,
	); err != nil {
		if db.IsUniqueViolation(err, "journal_entries_reverses_key") {
			return Posted{}, errs.Conflict(errs.CodeImmutableEntry, "this entry has already been reversed")
		}
		return Posted{}, errs.Internal(err, "insert reversing entry")
	}

	// Debits become credits and credits become debits, line for line.
	lineIDs := make([]ids.ID, len(original.Lines))
	accountIDs := make([]ids.ID, len(original.Lines))
	debits := make([]int64, len(original.Lines))
	credits := make([]int64, len(original.Lines))
	memos := make([]string, len(original.Lines))
	for i, l := range original.Lines {
		lineIDs[i] = ids.New()
		accountIDs[i] = l.AccountID
		debits[i] = l.Credit // swapped
		credits[i] = l.Debit // swapped
		memos[i] = "reversal"
	}

	if err := insertLines(ctx, tx, tenantID, reversalID, original.Currency,
		lineIDs, accountIDs, debits, credits, memos); err != nil {
		return Posted{}, err
	}

	// Link the original forward. The immutability trigger permits exactly this
	// one field to change, and only once.
	if _, err := tx.Exec(ctx,
		`UPDATE journal_entries SET reversed_by = $1 WHERE id = $2`, reversalID, original.ID,
	); err != nil {
		if db.IsCheckViolation(err, "") {
			return Posted{}, errs.Conflict(errs.CodeImmutableEntry, "this entry has already been reversed")
		}
		return Posted{}, errs.Internal(err, "link reversal to original")
	}

	return s.Get(ctx, tx, reversalID)
}

// Get loads one entry with its lines.
func (s *Service) Get(ctx context.Context, tx pgx.Tx, entryID ids.ID) (Posted, error) {
	var p Posted
	var sourceType string
	err := tx.QueryRow(ctx,
		`SELECT id, entry_date, memo, source_type::text, source_id, currency, reverses_id, reversed_by
		   FROM journal_entries WHERE id = $1`, entryID,
	).Scan(&p.ID, &p.Date, &p.Memo, &sourceType, &p.SourceID, &p.Currency, &p.ReversesID, &p.ReversedBy)
	if err != nil {
		if db.IsNoRows(err) {
			return Posted{}, errs.NotFound("journal entry")
		}
		return Posted{}, errs.Internal(err, "load journal entry")
	}
	p.SourceType = SourceType(sourceType)

	rows, err := tx.Query(ctx,
		`SELECT l.account_id, a.code, coalesce(a.slug, ''), l.debit_minor, l.credit_minor, l.memo
		   FROM journal_lines l
		   JOIN accounts a ON a.id = l.account_id
		  WHERE l.entry_id = $1
		  ORDER BY l.debit_minor DESC, a.code`, entryID)
	if err != nil {
		return Posted{}, errs.Internal(err, "load journal lines")
	}
	defer rows.Close()

	for rows.Next() {
		var l PostedLine
		var slug string
		if err := rows.Scan(&l.AccountID, &l.AccountCode, &slug, &l.Debit, &l.Credit, &l.Memo); err != nil {
			return Posted{}, errs.Internal(err, "scan journal line")
		}
		l.AccountSlug = Slug(slug)
		p.Lines = append(p.Lines, l)
	}
	if err := rows.Err(); err != nil {
		return Posted{}, errs.Internal(err, "read journal lines")
	}
	return p, nil
}

// currentTenant reads the tenant bound to this transaction.
//
// Taking it from the database rather than from a parameter means a caller
// cannot post into a tenant they are not bound to: the value here is the same
// one every RLS policy is evaluating against.
func currentTenant(ctx context.Context, tx pgx.Tx) (ids.ID, error) {
	var tenantID *ids.ID
	if err := tx.QueryRow(ctx, `SELECT current_tenant_id()`).Scan(&tenantID); err != nil {
		return ids.Nil, errs.Internal(err, "read tenant context")
	}
	if tenantID == nil {
		return ids.Nil, errs.Internal(nil, "no tenant context is bound to this transaction")
	}
	return *tenantID, nil
}

// insertLines writes an entry's lines as one set-based statement.
//
// COPY is unavailable here: Postgres refuses COPY FROM on a table with
// row-level security, and the app role is deliberately not exempt from it.
func insertLines(
	ctx context.Context, tx pgx.Tx,
	tenantID, entryID ids.ID, currency string,
	lineIDs, accountIDs []ids.ID, debits, credits []int64, memos []string,
) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO journal_lines
		  (id, tenant_id, entry_id, account_id, debit_minor, credit_minor, currency, memo)
		SELECT l.id, $1, $2, l.account_id, l.debit, l.credit, $3, l.memo
		  FROM unnest($4::uuid[], $5::uuid[], $6::bigint[], $7::bigint[], $8::text[])
		    AS l(id, account_id, debit, credit, memo)`,
		tenantID, entryID, currency, lineIDs, accountIDs, debits, credits, memos)
	if err != nil {
		// The deferred balance trigger fires at commit, not here, so a failure
		// at this point is a constraint on an individual line.
		if db.IsCheckViolation(err, "journal_lines_one_sided") {
			return errs.Internal(err, "journal line is neither a debit nor a credit")
		}
		return errs.Internal(err, "insert journal lines")
	}
	return nil
}
