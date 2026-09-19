// Package dashboard answers the five questions a trainer opens the app to ask.
//
// Five numbers, not a chart wall. Each one is either something to do today or
// money that has not arrived, and every one of them is computed from the
// ledger and the calendar at the moment it is read — none of it is stored, so
// none of it can drift.
//
// The numbers are deliberately not mirrored to the device. They are derived
// from data that changes on the server (a payment recorded on another device
// moves three of the five), and a stale dashboard is worse than an absent one
// because it is quietly wrong. The low-balance *list*, by contrast, is
// computed locally too: it drives a renewal conversation on a gym floor, which
// is exactly where the signal is worst.
package dashboard

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/NewMux/mtdrb_go/internal/billing"
	"github.com/NewMux/mtdrb_go/internal/ledger"
	"github.com/NewMux/mtdrb_go/internal/platform/clock"
	"github.com/NewMux/mtdrb_go/internal/platform/errs"
	"github.com/NewMux/mtdrb_go/internal/platform/ids"
	"github.com/NewMux/mtdrb_go/internal/platform/money"
)

// Summary is the whole dashboard.
type Summary struct {
	// SessionsToday counts attendees booked today, not sessions: a
	// semi-private slot with three people in it is three conversations and
	// three credits, which is what the trainer's day actually holds.
	SessionsToday     int `json:"sessions_today"`
	SessionsTodayLeft int `json:"sessions_today_unmarked"`

	// SessionsLeftThisWeek runs to the end of the week from now, so it shrinks
	// through the week rather than resetting at an arbitrary hour.
	SessionsLeftThisWeek int `json:"sessions_left_this_week"`

	UnpaidInvoices int         `json:"unpaid_invoices"`
	Outstanding    money.Money `json:"outstanding"`

	// LowBalance is the renewal list, and the reason this screen exists.
	LowBalanceCount int                        `json:"low_balance_count"`
	LowBalance      []billing.LowBalanceClient `json:"low_balance"`
	Threshold       int                        `json:"low_balance_threshold"`

	IncomeThisMonth money.Money `json:"income_this_month"`
	Currency        string      `json:"currency"`
}

// Service assembles the summary from the modules that own each number.
//
// It owns no tables of its own and deliberately duplicates no arithmetic: the
// receivables total comes from billing, revenue from the ledger, credits from
// the credit service. A dashboard that recomputed any of those would be a
// second answer to a question that already has one.
type Service struct {
	billing *billing.Service
	ledger  *ledger.Service
	clock   clock.Clock
}

// NewService builds the dashboard service.
func NewService(b *billing.Service, l *ledger.Service, c clock.Clock) *Service {
	if c == nil {
		c = clock.System{}
	}
	return &Service{billing: b, ledger: l, clock: c}
}

// Summarise reads every number inside one transaction, so they agree with each
// other. Assembled from separate queries at separate instants, the invoice
// count and the outstanding total could disagree by a payment.
func (s *Service) Summarise(ctx context.Context, tx pgx.Tx, tenantID ids.ID) (Summary, error) {
	var currency string
	var threshold int
	var timezone string
	if err := tx.QueryRow(ctx,
		`SELECT default_currency, low_balance_threshold, timezone
		   FROM tenants WHERE id = $1`, tenantID,
	).Scan(&currency, &threshold, &timezone); err != nil {
		return Summary{}, errs.Internal(err, "load tenant settings")
	}

	// The trainer's day, not UTC's. A 21:00 session in Dubai belongs to the
	// day the trainer is living, and an invalid stored zone must not take the
	// whole screen down.
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		loc = time.UTC
	}
	now := s.clock.Now().In(loc)
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	endOfDay := startOfDay.AddDate(0, 0, 1)
	endOfWeek := startOfDay.AddDate(0, 0, (7-int(startOfDay.Weekday())+1)%7+1)

	out := Summary{Currency: currency, Threshold: threshold}

	if err := tx.QueryRow(ctx, `
		SELECT count(*),
		       count(*) FILTER (WHERE sa.status = 'scheduled')
		  FROM session_attendees sa
		  JOIN sessions s ON s.id = sa.session_id
		 WHERE s.starts_at >= $1 AND s.starts_at < $2
		   AND s.status <> 'cancelled'`,
		startOfDay, endOfDay,
	).Scan(&out.SessionsToday, &out.SessionsTodayLeft); err != nil {
		return Summary{}, errs.Internal(err, "count today's sessions")
	}

	// From now, not from the start of today: sessions already delivered this
	// morning are not "left".
	if err := tx.QueryRow(ctx, `
		SELECT count(*)
		  FROM session_attendees sa
		  JOIN sessions s ON s.id = sa.session_id
		 WHERE s.starts_at >= $1 AND s.starts_at < $2
		   AND s.status <> 'cancelled' AND sa.status = 'scheduled'`,
		now, endOfWeek,
	).Scan(&out.SessionsLeftThisWeek); err != nil {
		return Summary{}, errs.Internal(err, "count the week's sessions")
	}

	receivables, err := s.billing.ReceivablesReport(ctx, tx, currency)
	if err != nil {
		return Summary{}, err
	}
	out.UnpaidInvoices = receivables.OutstandingCount
	out.Outstanding = money.New(receivables.TotalMinor, currency)

	low, err := s.billing.LowBalance(ctx, tx, threshold)
	if err != nil {
		return Summary{}, err
	}
	out.LowBalance = low
	out.LowBalanceCount = len(low)

	// Income is recognised revenue for the month to date — what was *earned*
	// by delivering sessions, not what was collected. Money arriving moves
	// between assets; it is not income, and a dashboard that conflated the two
	// would misreport the month every time a pack was sold.
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, loc)
	pl, err := s.ledger.ProfitAndLoss(ctx, tx, monthStart, now, currency)
	if err != nil {
		return Summary{}, err
	}
	out.IncomeThisMonth = money.New(pl.GrossRevenue, currency)

	return out, nil
}
