// Package billing owns prepaid session packs and the credit ledger.
//
// A credit is a claim on future training. Selling one creates an obligation;
// burning one discharges it and earns the revenue. The credit ledger and the
// money ledger therefore move together, always inside one transaction: a
// burned credit without its revenue entry, or revenue without a burned
// credit, are states that must not be able to exist.
package billing

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/NewMux/mtdrb_go/internal/db"
	"github.com/NewMux/mtdrb_go/internal/ledger"
	"github.com/NewMux/mtdrb_go/internal/platform/clock"
	"github.com/NewMux/mtdrb_go/internal/platform/errs"
	"github.com/NewMux/mtdrb_go/internal/platform/ids"
	"github.com/NewMux/mtdrb_go/internal/platform/money"
)

// PackageStatus is a pack's lifecycle state.
type PackageStatus string

const (
	PackageActive    PackageStatus = "active"
	PackageExhausted PackageStatus = "exhausted"
	PackageExpired   PackageStatus = "expired"
	PackageVoid      PackageStatus = "void"
)

// CreditReason explains a movement in the credit ledger.
type CreditReason string

const (
	ReasonPurchase         CreditReason = "purchase"
	ReasonSessionCompleted CreditReason = "session_completed"
	ReasonLateCancellation CreditReason = "late_cancellation"
	ReasonNoShow           CreditReason = "no_show"
	ReasonUndo             CreditReason = "undo"
	ReasonExpiry           CreditReason = "expiry"
	ReasonManual           CreditReason = "manual_adjustment"
)

// Package is a prepaid block of sessions.
type Package struct {
	ID               ids.ID        `json:"id"`
	ClientID         ids.ID        `json:"client_id"`
	InvoiceID        *ids.ID       `json:"invoice_id,omitempty"`
	Name             string        `json:"name"`
	CreditsTotal     int           `json:"credits_total"`
	CreditsRemaining int           `json:"credits_remaining"`
	UnitPriceMinor   int64         `json:"unit_price_minor"`
	Currency         string        `json:"currency"`
	PurchasedOn      time.Time     `json:"purchased_on"`
	ExpiresAt        *time.Time    `json:"expires_at,omitempty"`
	Status           PackageStatus `json:"status"`
	ServerSeq        int64         `json:"server_seq"`
}

// UnitPrice is the revenue recognised per credit burned from this pack.
func (p Package) UnitPrice() money.Money { return money.New(p.UnitPriceMinor, p.Currency) }

// Service manages packs and the credit ledger.
type Service struct {
	ledger *ledger.Service
	clock  clock.Clock
}

// NewService builds the billing service.
func NewService(l *ledger.Service, c clock.Clock) *Service {
	if c == nil {
		c = clock.System{}
	}
	return &Service{ledger: l, clock: c}
}

// GrantInput describes a pack to create.
type GrantInput struct {
	ClientID    ids.ID
	InvoiceID   *ids.ID
	Name        string
	Credits     int
	UnitPrice   money.Money
	PurchasedOn time.Time
	ExpiresAt   *time.Time
}

// Grant creates a prepaid pack and records the opening credit movement.
//
// A pack is a promise of future training, so it must always be matched by a
// liability in Deferred Revenue — otherwise burning its credits would drive
// that account negative and overstate profit. Where the liability comes from
// depends on how the pack was sold:
//
//   - sold on an invoice: the invoice posts DR Receivable / CR Deferred, and
//     this does not post again, so the two cannot double-count.
//   - granted directly (comped, or migrated from the trainer's old system):
//     there is no invoice to carry it, so an opening-balance entry is posted
//     here against Owner's Equity.
//
// A zero-priced pack needs no entry: it promises training worth nothing, which
// is what "comped" means on the books.
func (s *Service) Grant(ctx context.Context, tx pgx.Tx, tenantID ids.ID, in GrantInput) (Package, error) {
	if in.Credits <= 0 {
		return Package{}, errs.Invalid(errs.CodeValidation, "a package needs at least one credit").
			WithField("credits", "must be greater than zero")
	}
	if in.UnitPrice.IsNegative() {
		return Package{}, errs.Invalid(errs.CodeValidation, "a credit price cannot be negative").
			WithField("unit_price_minor", "must not be negative")
	}
	if in.UnitPrice.Currency == "" {
		return Package{}, errs.Invalid(errs.CodeValidation, "a package needs a currency")
	}
	if in.PurchasedOn.IsZero() {
		in.PurchasedOn = s.clock.Now()
	}
	purchasedOn := truncateToDay(in.PurchasedOn)
	if in.ExpiresAt != nil {
		expires := truncateToDay(*in.ExpiresAt)
		if expires.Before(purchasedOn) {
			return Package{}, errs.Invalid(errs.CodeValidation,
				"a package cannot expire before it was purchased").
				WithField("expires_at", "must not be before the purchase date")
		}
		in.ExpiresAt = &expires
	}

	pkg := Package{
		ID: ids.New(), ClientID: in.ClientID, InvoiceID: in.InvoiceID, Name: in.Name,
		CreditsTotal: in.Credits, CreditsRemaining: in.Credits,
		UnitPriceMinor: in.UnitPrice.Minor, Currency: in.UnitPrice.Currency,
		PurchasedOn: purchasedOn, ExpiresAt: in.ExpiresAt, Status: PackageActive,
	}

	if err := tx.QueryRow(ctx, `
		INSERT INTO packages
			(id, tenant_id, client_id, invoice_id, name, credits_total, credits_remaining,
			 unit_price_minor, currency, purchased_on, expires_at, status)
		VALUES ($1, $2, $3, $4, $5, $6, $6, $7, $8, $9, $10, 'active')
		RETURNING server_seq`,
		pkg.ID, tenantID, in.ClientID, in.InvoiceID, in.Name, in.Credits,
		in.UnitPrice.Minor, in.UnitPrice.Currency, purchasedOn, in.ExpiresAt,
	).Scan(&pkg.ServerSeq); err != nil {
		if db.IsForeignKeyViolation(err) {
			return Package{}, errs.NotFound("client")
		}
		return Package{}, errs.Internal(err, "create package")
	}

	var openingEntry *ids.ID
	if in.InvoiceID == nil && !in.UnitPrice.IsZero() {
		value := in.UnitPrice.Mul(int64(in.Credits))
		posted, err := s.ledger.Post(ctx, tx, ledger.Entry{
			Date:       purchasedOn,
			Memo:       "package granted outside an invoice",
			SourceType: ledger.SourceOpeningBalance,
			SourceID:   &pkg.ID,
			Currency:   value.Currency,
			Lines: []ledger.Line{
				ledger.Debit(ledger.SlugOwnersEquity, value, "granted training"),
				ledger.Credit(ledger.SlugDeferredRevenue, value, "unearned session credits"),
			},
		})
		if err != nil {
			return Package{}, err
		}
		openingEntry = &posted.ID
	}

	if err := s.appendCredit(ctx, tx, tenantID, creditEntry{
		PackageID: pkg.ID, ClientID: in.ClientID, Delta: in.Credits,
		Reason: ReasonPurchase, JournalEntryID: openingEntry, Memo: in.Name,
	}); err != nil {
		return Package{}, err
	}
	return pkg, nil
}

// Consumption records credits taken from one pack.
type Consumption struct {
	PackageID ids.ID
	Credits   int
	UnitPrice money.Money
}

// Total is the revenue recognised for this consumption.
func (c Consumption) Total() money.Money { return c.UnitPrice.Mul(int64(c.Credits)) }

// Balance summarises a client's credit position.
type Balance struct {
	ClientID   ids.ID     `json:"client_id"`
	Remaining  int        `json:"remaining"`
	Packages   []Package  `json:"packages,omitempty"`
	NextExpiry *time.Time `json:"next_expiry,omitempty"`
}

// BalanceFor returns a client's credit position, which may be negative.
//
// Expired and void packs are excluded: a credit that cannot be redeemed is not
// a credit, and showing it would have the trainer book a session that then
// fails to burn.
//
// Exhausted packs are included, because an overdrawn client sits on one and
// their balance is negative. Reporting that as zero would hide the debt from
// the person who most needs to see it.
func (s *Service) BalanceFor(ctx context.Context, tx pgx.Tx, clientID ids.ID) (Balance, error) {
	today := truncateToDay(s.clock.Now())

	rows, err := tx.Query(ctx, `
		SELECT id, client_id, invoice_id, name, credits_total, credits_remaining,
		       unit_price_minor, currency, purchased_on, expires_at, status::text, server_seq
		  FROM packages
		 WHERE client_id = $1 AND status IN ('active', 'exhausted')
		   AND credits_remaining <> 0
		   AND (expires_at IS NULL OR expires_at >= $2)
		 ORDER BY expires_at NULLS LAST, purchased_on`, clientID, today)
	if err != nil {
		return Balance{}, errs.Internal(err, "load credit balance")
	}
	defer rows.Close()

	balance := Balance{ClientID: clientID}
	for rows.Next() {
		var p Package
		var status string
		if err := rows.Scan(&p.ID, &p.ClientID, &p.InvoiceID, &p.Name, &p.CreditsTotal,
			&p.CreditsRemaining, &p.UnitPriceMinor, &p.Currency, &p.PurchasedOn,
			&p.ExpiresAt, &status, &p.ServerSeq); err != nil {
			return Balance{}, errs.Internal(err, "scan package")
		}
		p.Status = PackageStatus(status)
		balance.Remaining += p.CreditsRemaining
		balance.Packages = append(balance.Packages, p)
		if p.ExpiresAt != nil && (balance.NextExpiry == nil || p.ExpiresAt.Before(*balance.NextExpiry)) {
			balance.NextExpiry = p.ExpiresAt
		}
	}
	if err := rows.Err(); err != nil {
		return Balance{}, errs.Internal(err, "read credit balance")
	}
	return balance, nil
}

// ConsumeInput describes credits to burn.
type ConsumeInput struct {
	ClientID   ids.ID
	Credits    int
	Reason     CreditReason
	AttendeeID *ids.ID
	Memo       string
	// AllowOverdraft permits the balance to go negative, for a trainer who
	// extends credit to a client they trust.
	AllowOverdraft bool
}

// PlanConsumption locks the client's packs and works out what burning
// in.Credits would take, without writing anything.
//
// Splitting planning from applying exists for one reason: the journal entry
// cannot be posted until the amounts are known, and credit_transactions is
// append-only, so the link between a burned credit and the revenue it earned
// has to be written when the row is inserted rather than patched in
// afterwards. The FOR UPDATE locks taken here are held for the rest of the
// transaction, so nothing can consume the same credits in between.
func (s *Service) PlanConsumption(ctx context.Context, tx pgx.Tx, in ConsumeInput) ([]Consumption, error) {
	if in.Credits < 0 {
		return nil, errs.Invalid(errs.CodeValidation, "cannot consume a negative number of credits")
	}
	if in.Credits == 0 {
		return nil, nil
	}
	today := truncateToDay(s.clock.Now())

	// Exhausted packs are included, not just active ones. A pack at zero is
	// still the right thing to draw an overdraft against: it carries the price
	// at which to recognise the revenue, and it is where the negative balance
	// belongs. Expired and void packs are excluded — a credit that cannot be
	// redeemed is not a credit.
	rows, err := tx.Query(ctx, `
		SELECT id, credits_remaining, unit_price_minor, currency
		  FROM packages
		 WHERE client_id = $1 AND status IN ('active', 'exhausted')
		   AND (expires_at IS NULL OR expires_at >= $2)
		 ORDER BY expires_at NULLS LAST, purchased_on, id
		 FOR UPDATE`, in.ClientID, today)
	if err != nil {
		return nil, errs.Internal(err, "lock packages for consumption")
	}

	type candidate struct {
		id        ids.ID
		remaining int
		unitPrice int64
		currency  string
	}
	var candidates []candidate
	var available int
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.id, &c.remaining, &c.unitPrice, &c.currency); err != nil {
			rows.Close()
			return nil, errs.Internal(err, "scan package")
		}
		candidates = append(candidates, c)
		if c.remaining > 0 {
			available += c.remaining
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, errs.Internal(err, "read packages")
	}

	if len(candidates) == 0 {
		// Overdraft draws against an existing pack, so with none at all there
		// is nothing to draw on and no price at which to recognise revenue.
		return nil, errs.Unprocessable(errs.CodeInsufficientCredits,
			"this client has no active session package").
			WithMeta("remaining", 0).
			WithMeta("required", in.Credits)
	}
	if available < in.Credits && !in.AllowOverdraft {
		return nil, errs.Unprocessable(errs.CodeInsufficientCredits,
			"this client has %d credits remaining but %d are needed", available, in.Credits).
			WithMeta("remaining", available).
			WithMeta("required", in.Credits)
	}

	var planned []Consumption
	outstanding := in.Credits

	for i := range candidates {
		if outstanding == 0 {
			break
		}
		c := &candidates[i]
		if c.remaining <= 0 {
			continue
		}
		take := min(outstanding, c.remaining)
		planned = append(planned, Consumption{
			PackageID: c.id, Credits: take,
			UnitPrice: money.New(c.unitPrice, c.currency),
		})
		outstanding -= take
	}

	// Whatever is left over is the overdraft, taken against the last pack so
	// it is priced consistently and stays visible as a negative balance.
	if outstanding > 0 {
		last := candidates[len(candidates)-1]
		planned = append(planned, Consumption{
			PackageID: last.id, Credits: outstanding,
			UnitPrice: money.New(last.unitPrice, last.currency),
		})
	}
	return planned, nil
}

// ApplyConsumption writes a plan: it moves each pack's balance and appends the
// matching credit rows, carrying the journal entry that recognised the
// revenue.
func (s *Service) ApplyConsumption(ctx context.Context, tx pgx.Tx, tenantID ids.ID, in ConsumeInput, planned []Consumption, journalEntryID *ids.ID) error {
	for _, c := range planned {
		if err := s.applyDelta(ctx, tx, tenantID, in, c.PackageID, -c.Credits, journalEntryID); err != nil {
			return err
		}
	}
	return nil
}

// Restore returns credits to the packs they came from.
//
// Undo restores to the exact packs recorded at the time, not to whatever is
// oldest now, so reversing a completion cannot quietly move credit between
// packs bought at different prices.
func (s *Service) Restore(ctx context.Context, tx pgx.Tx, tenantID ids.ID, attendeeID ids.ID, memo string, journalEntryID *ids.ID) error {
	rows, err := tx.Query(ctx, `
		SELECT package_id, client_id, sum(delta)::int
		  FROM credit_transactions
		 WHERE session_attendee_id = $1
		 GROUP BY package_id, client_id
		HAVING sum(delta) <> 0`, attendeeID)
	if err != nil {
		return errs.Internal(err, "load credits to restore")
	}

	type restoration struct {
		packageID ids.ID
		clientID  ids.ID
		delta     int
	}
	var toRestore []restoration
	for rows.Next() {
		var r restoration
		if err := rows.Scan(&r.packageID, &r.clientID, &r.delta); err != nil {
			rows.Close()
			return errs.Internal(err, "scan credit transaction")
		}
		toRestore = append(toRestore, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return errs.Internal(err, "read credit transactions")
	}

	for _, r := range toRestore {
		// Negating the net movement returns exactly what was taken.
		if err := s.applyDelta(ctx, tx, tenantID, ConsumeInput{
			ClientID:   r.clientID,
			Reason:     ReasonUndo,
			AttendeeID: &attendeeID,
			Memo:       memo,
		}, r.packageID, -r.delta, journalEntryID); err != nil {
			return err
		}
	}
	return nil
}

// applyDelta moves a pack's balance and appends the matching log row.
//
// The two happen together, always. credits_remaining is a cache of the log; if
// they can diverge, the number the trainer sees on the gym floor stops meaning
// anything.
func (s *Service) applyDelta(ctx context.Context, tx pgx.Tx, tenantID ids.ID, in ConsumeInput, packageID ids.ID, delta int, journalEntryID *ids.ID) error {
	if delta == 0 {
		return nil
	}
	var clientID ids.ID
	var remaining int
	if err := tx.QueryRow(ctx, `
		UPDATE packages
		   SET credits_remaining = credits_remaining + $2,
		       status = CASE
		         WHEN credits_remaining + $2 <= 0 AND status = 'active' THEN 'exhausted'::package_status
		         WHEN credits_remaining + $2 > 0 AND status = 'exhausted' THEN 'active'::package_status
		         ELSE status END
		 WHERE id = $1
		RETURNING client_id, credits_remaining`,
		packageID, delta).Scan(&clientID, &remaining); err != nil {
		if db.IsNoRows(err) {
			return errs.NotFound("package")
		}
		return errs.Internal(err, "adjust package balance")
	}

	return s.appendCredit(ctx, tx, tenantID, creditEntry{
		PackageID:      packageID,
		ClientID:       clientID,
		Delta:          delta,
		Reason:         in.Reason,
		AttendeeID:     in.AttendeeID,
		JournalEntryID: journalEntryID,
		Memo:           in.Memo,
	})
}

type creditEntry struct {
	PackageID      ids.ID
	ClientID       ids.ID
	Delta          int
	Reason         CreditReason
	AttendeeID     *ids.ID
	JournalEntryID *ids.ID
	Memo           string
}

func (s *Service) appendCredit(ctx context.Context, tx pgx.Tx, tenantID ids.ID, e creditEntry) error {
	if e.Reason == "" {
		e.Reason = ReasonManual
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO credit_transactions
			(id, tenant_id, package_id, client_id, delta, reason, session_attendee_id, journal_entry_id, memo)
		VALUES ($1, $2, $3, $4, $5, $6::credit_reason, $7, $8, $9)`,
		ids.New(), tenantID, e.PackageID, e.ClientID, e.Delta, string(e.Reason),
		e.AttendeeID, e.JournalEntryID, e.Memo); err != nil {
		return errs.Internal(err, "append credit transaction")
	}
	return nil
}

// ExpirePackages marks lapsed packs expired and recognises their unearned
// revenue, since the trainer no longer owes the training.
func (s *Service) ExpirePackages(ctx context.Context, tx pgx.Tx, tenantID ids.ID, asOf time.Time, by *ids.ID) (int, error) {
	today := truncateToDay(asOf)

	rows, err := tx.Query(ctx, `
		SELECT id, client_id, credits_remaining, unit_price_minor, currency
		  FROM packages
		 WHERE status = 'active' AND expires_at IS NOT NULL AND expires_at < $1
		   AND credits_remaining > 0
		 FOR UPDATE`, today)
	if err != nil {
		return 0, errs.Internal(err, "find expired packages")
	}

	type lapsed struct {
		id        ids.ID
		clientID  ids.ID
		remaining int
		unitPrice int64
		currency  string
	}
	var expired []lapsed
	for rows.Next() {
		var l lapsed
		if err := rows.Scan(&l.id, &l.clientID, &l.remaining, &l.unitPrice, &l.currency); err != nil {
			rows.Close()
			return 0, errs.Internal(err, "scan expired package")
		}
		expired = append(expired, l)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, errs.Internal(err, "read expired packages")
	}

	for _, l := range expired {
		amount := money.New(l.unitPrice, l.currency).Mul(int64(l.remaining))

		// Discharge the liability: the training will now never be delivered,
		// so leaving it in Deferred Revenue would understate profit forever
		// and overstate what the trainer still owes their clients.
		entry, err := s.ledger.PostPackageExpired(ctx, tx, l.id, amount, today,
			"package expired with unused credits", by)
		if err != nil {
			return 0, err
		}

		if _, err := tx.Exec(ctx,
			`UPDATE packages SET credits_remaining = 0, status = 'expired' WHERE id = $1`, l.id); err != nil {
			return 0, errs.Internal(err, "mark package expired")
		}
		if err := s.appendCredit(ctx, tx, tenantID, creditEntry{
			PackageID: l.id, ClientID: l.clientID, Delta: -l.remaining,
			Reason: ReasonExpiry, JournalEntryID: &entry.ID, Memo: "expired",
		}); err != nil {
			return 0, err
		}
	}
	return len(expired), nil
}

func truncateToDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// LowBalanceClient is a client close enough to empty to be worth a renewal
// conversation.
type LowBalanceClient struct {
	ClientID   ids.ID     `json:"client_id"`
	ClientName string     `json:"client_name"`
	Remaining  int        `json:"remaining"`
	NextExpiry *time.Time `json:"next_expiry,omitempty"`
	// LastSessionOn dates the last delivered session, so the trainer can tell
	// "about to run out" from "stopped coming six weeks ago". The same number
	// means very different things in those two cases.
	LastSessionOn *time.Time `json:"last_session_on,omitempty"`
}

// LowBalance lists clients at or under a credit threshold, soonest to run out
// first.
//
// Derived on every read rather than stored. A flag on the client row would be
// exactly as truthful as the last job that refreshed it, and the whole value of
// this list is that it is right at the moment the trainer looks at it — usually
// standing in front of the person it concerns.
//
// The aggregate matches BalanceFor deliberately: expired and void packs are
// excluded because a credit that cannot be redeemed is not a credit, and
// exhausted packs are included because an overdrawn client sits on one and
// their balance is negative. A client with no packs at all counts as zero and
// belongs on this list — they are the most obvious renewal of all.
func (s *Service) LowBalance(ctx context.Context, tx pgx.Tx, threshold int) ([]LowBalanceClient, error) {
	if threshold < 0 {
		return nil, errs.Invalid(errs.CodeValidation, "a credit threshold cannot be negative").
			WithField("threshold", "must be zero or more")
	}
	today := truncateToDay(s.clock.Now())

	rows, err := tx.Query(ctx, `
		SELECT c.id, c.full_name,
		       coalesce(p.remaining, 0) AS remaining,
		       p.next_expiry,
		       last.performed_on
		  FROM clients c
		  LEFT JOIN LATERAL (
		         SELECT sum(credits_remaining) AS remaining,
		                min(expires_at) FILTER (WHERE credits_remaining > 0) AS next_expiry
		           FROM packages
		          WHERE client_id = c.id
		            AND status IN ('active', 'exhausted')
		            AND credits_remaining <> 0
		            AND (expires_at IS NULL OR expires_at >= $2)
		       ) p ON true
		  LEFT JOIN LATERAL (
		         SELECT max(s.starts_at)::date AS performed_on
		           FROM session_attendees sa
		           JOIN sessions s ON s.id = sa.session_id
		          WHERE sa.client_id = c.id AND sa.status = 'completed'
		       ) last ON true
		 WHERE c.deleted_at IS NULL
		   AND c.status = 'active'
		   AND coalesce(p.remaining, 0) <= $1
		 ORDER BY coalesce(p.remaining, 0), p.next_expiry NULLS LAST, c.full_name`,
		threshold, today)
	if err != nil {
		return nil, errs.Internal(err, "list low balances")
	}
	defer rows.Close()

	out := []LowBalanceClient{}
	for rows.Next() {
		var c LowBalanceClient
		if err := rows.Scan(&c.ClientID, &c.ClientName, &c.Remaining, &c.NextExpiry, &c.LastSessionOn); err != nil {
			return nil, errs.Internal(err, "scan low balance")
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, errs.Internal(err, "read low balances")
	}
	return out, nil
}
