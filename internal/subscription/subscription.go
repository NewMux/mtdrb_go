// Package subscription decides what a practice's plan allows.
//
// It is plan *state*, not billing. Nothing here takes a payment: cmd/admin
// sets a plan today, and a payment provider's webhook will set the same
// columns later (see Provider). What this package owns is the meaning of
// that state — which features a plan includes, how many clients it holds,
// and when an account has lapsed — so that one answer serves the route
// guards, the limit checks and the Subscription screen.
//
// Two rules shape it.
//
// A trial is the whole product. A trainer deciding whether to pay should see
// the analytics and the shop, not a greyed-out menu, so a trial carries every
// Pro entitlement for its fourteen days.
//
// A lapsed account goes read-only, never dark. A trainer's client list and
// books are theirs; locking them out of their own records over a missed
// payment would be both unkind and, for a VAT-registered business that must
// keep them, a problem. Reads and exports keep working, and writes are
// refused with a 402 the app explains.
package subscription

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/NewMux/mtdrb_go/internal/platform/errs"
	"github.com/NewMux/mtdrb_go/internal/platform/ids"
	"github.com/NewMux/mtdrb_go/internal/tenancy"
)

// Plan is a tier of the product.
type Plan string

const (
	PlanTrial   Plan = "trial"
	PlanStarter Plan = "starter"
	PlanPro     Plan = "pro"
)

// Valid reports whether p is a known plan.
func (p Plan) Valid() bool { return p == PlanTrial || p == PlanStarter || p == PlanPro }

// Status is where a paid plan stands.
type Status string

const (
	StatusActive Status = "active"
	// StatusPastDue is a failed renewal in its grace period: still fully
	// usable, so a declined card does not stop Monday's sessions being marked.
	StatusPastDue   Status = "past_due"
	StatusCancelled Status = "cancelled"
)

// Valid reports whether s is a known status.
func (s Status) Valid() bool { return s == StatusActive || s == StatusPastDue || s == StatusCancelled }

// Feature is a module a plan may or may not include.
type Feature string

const (
	FeatureShop        Feature = "shop"
	FeatureAnalytics   Feature = "analytics"
	FeatureInsights    Feature = "insights"
	FeatureAutomations Feature = "automations"
)

// Limit is a counted resource a plan may cap.
type Limit string

const (
	LimitActiveClients Limit = "active_clients"
	LimitLocations     Limit = "locations"
)

// TrialLength is how long a new practice has everything.
const TrialLength = 14 * 24 * time.Hour

// entitlements is the whole price list. A limit absent from the map is
// unlimited.
var entitlements = map[Plan]struct {
	features map[Feature]bool
	limits   map[Limit]int
}{
	PlanStarter: {
		features: map[Feature]bool{},
		limits:   map[Limit]int{LimitActiveClients: 25, LimitLocations: 1},
	},
	PlanPro: {
		features: map[Feature]bool{
			FeatureShop: true, FeatureAnalytics: true, FeatureInsights: true, FeatureAutomations: true,
		},
		limits: map[Limit]int{},
	},
}

// State is a practice's plan as stored.
type State struct {
	Plan              Plan
	Status            Status
	TrialEndsAt       *time.Time
	RenewsOn          *time.Time
	CancelAtPeriodEnd bool
}

// FromPrincipal reads the plan the caller's access token was minted with.
//
// Up to one access-token lifetime stale, which is right for route guards (a
// plan change applies at the next refresh) and wrong for limits, which
// read the tenant row instead — see Load.
func FromPrincipal(p tenancy.Principal) State {
	return State{Plan: Plan(p.Plan), Status: Status(p.PlanStatus), TrialEndsAt: p.TrialEndsAt}
}

// Effective is the plan whose entitlements apply now: Pro during a trial.
func (s State) Effective() Plan {
	if s.Plan == PlanTrial {
		return PlanPro
	}
	return s.Plan
}

// Lapsed reports whether the account has gone read-only.
func (s State) Lapsed(now time.Time) bool {
	switch {
	case s.Plan == PlanTrial:
		return s.TrialEndsAt == nil || !now.Before(*s.TrialEndsAt)
	case s.Status == StatusCancelled:
		return true
	case s.CancelAtPeriodEnd && s.RenewsOn != nil:
		// The period runs to the end of its renewal day.
		return !now.Before(s.RenewsOn.AddDate(0, 0, 1))
	}
	return false
}

// Allows reports whether the plan includes a feature.
func (s State) Allows(f Feature) bool {
	return entitlements[s.Effective()].features[f]
}

// LimitOf reports a plan's cap on a resource, and false when there is none.
func (s State) LimitOf(l Limit) (int, bool) {
	n, ok := entitlements[s.Effective()].limits[l]
	return n, ok
}

// Features lists what the plan includes, in a stable order.
func (s State) Features() []Feature {
	out := []Feature{}
	for _, f := range []Feature{FeatureShop, FeatureAnalytics, FeatureInsights, FeatureAutomations} {
		if s.Allows(f) {
			out = append(out, f)
		}
	}
	return out
}

// Load reads the tenant's plan inside its transaction.
func Load(ctx context.Context, tx pgx.Tx, tenantID ids.ID) (State, error) {
	return load(ctx, tx, `WHERE id = $1`, tenantID)
}

func load(ctx context.Context, tx pgx.Tx, where string, args ...any) (State, error) {
	var (
		s        State
		plan     string
		status   string
		renewsOn *time.Time
	)
	if err := tx.QueryRow(ctx,
		`SELECT plan, plan_status, trial_ends_at, plan_renews_on, cancel_at_period_end
		   FROM tenants `+where, args...,
	).Scan(&plan, &status, &s.TrialEndsAt, &renewsOn, &s.CancelAtPeriodEnd); err != nil {
		return State{}, errs.Internal(err, "load plan")
	}
	s.Plan, s.Status, s.RenewsOn = Plan(plan), Status(status), renewsOn
	return s, nil
}

// Counter reports how much of a limited resource a practice uses. Each
// package that owns a limited resource registers one, so this package does
// not reach into their tables.
type Counter func(ctx context.Context, tx pgx.Tx) (int, error)

var counters = map[Limit]Counter{}

// RegisterCounter installs the counter for a limit. Called from package
// init; a second registration for the same limit is a programming error.
func RegisterCounter(l Limit, c Counter) {
	if _, dup := counters[l]; dup {
		panic("subscription: counter registered twice for " + string(l))
	}
	counters[l] = c
}

// CheckRoom refuses adding one more of a limited resource when the plan is
// full, in the tenant the transaction is bound to. It reads the plan from the
// tenant row, not the token: a trainer who has just downgraded must not get
// fifteen minutes of Pro limits.
func CheckRoom(ctx context.Context, tx pgx.Tx, l Limit) error {
	state, err := load(ctx, tx, `WHERE id = current_tenant_id()`)
	if err != nil {
		return err
	}
	limit, capped := state.LimitOf(l)
	if !capped {
		return nil
	}
	count, ok := counters[l]
	if !ok {
		return nil
	}
	used, err := count(ctx, tx)
	if err != nil {
		return err
	}
	if used >= limit {
		return errs.Forbidden(errs.CodePlanLimitReached,
			"your %s plan includes %d %s", state.Effective(), limit, humanLimit(l)).
			WithMeta("limit", string(l)).
			WithMeta("max", limit).
			WithMeta("plan", string(state.Effective()))
	}
	return nil
}

func humanLimit(l Limit) string {
	switch l {
	case LimitActiveClients:
		return "active clients"
	case LimitLocations:
		return "locations"
	}
	return string(l)
}

// Summary is what the Subscription screen shows.
type Summary struct {
	Plan              Plan          `json:"plan"`
	EffectivePlan     Plan          `json:"effective_plan"`
	Status            Status        `json:"status"`
	TrialEndsAt       *time.Time    `json:"trial_ends_at"`
	TrialDaysLeft     *int          `json:"trial_days_left"`
	RenewsOn          *string       `json:"renews_on"`
	CancelAtPeriodEnd bool          `json:"cancel_at_period_end"`
	Lapsed            bool          `json:"lapsed"`
	Features          []Feature     `json:"features"`
	Limits            map[Limit]int `json:"limits"`
	Usage             map[Limit]int `json:"usage"`
}

// Summarise reports the plan with its limits and what is used of them.
func Summarise(ctx context.Context, tx pgx.Tx, tenantID ids.ID, now time.Time) (Summary, error) {
	state, err := Load(ctx, tx, tenantID)
	if err != nil {
		return Summary{}, err
	}
	out := Summary{
		Plan:              state.Plan,
		EffectivePlan:     state.Effective(),
		Status:            state.Status,
		TrialEndsAt:       state.TrialEndsAt,
		CancelAtPeriodEnd: state.CancelAtPeriodEnd,
		Lapsed:            state.Lapsed(now),
		Features:          state.Features(),
		Limits:            map[Limit]int{},
		Usage:             map[Limit]int{},
	}
	if state.RenewsOn != nil {
		on := state.RenewsOn.Format("2006-01-02")
		out.RenewsOn = &on
	}
	if state.Plan == PlanTrial && state.TrialEndsAt != nil {
		// Whole days, rounded up: "1 day left" on the last afternoon, not 0.
		left := int((state.TrialEndsAt.Sub(now) + 24*time.Hour - time.Nanosecond) / (24 * time.Hour))
		if left < 0 {
			left = 0
		}
		out.TrialDaysLeft = &left
	}
	for _, l := range []Limit{LimitActiveClients, LimitLocations} {
		if n, ok := state.LimitOf(l); ok {
			out.Limits[l] = n
		}
		if count, ok := counters[l]; ok {
			used, err := count(ctx, tx)
			if err != nil {
				return Summary{}, err
			}
			out.Usage[l] = used
		}
	}
	return out, nil
}

// SetCancelAtPeriodEnd records the owner's choice to stop, or not, at the
// end of the current period.
func SetCancelAtPeriodEnd(ctx context.Context, tx pgx.Tx, tenantID ids.ID, cancel bool) error {
	if _, err := tx.Exec(ctx,
		`UPDATE tenants SET cancel_at_period_end = $2 WHERE id = $1`, tenantID, cancel); err != nil {
		return errs.Internal(err, "update cancellation")
	}
	return nil
}

// Change is a plan change from outside the product: the admin CLI today, a
// payment provider's webhook later.
type Change struct {
	Plan        *Plan
	Status      *Status
	TrialEndsAt *time.Time
	RenewsOn    *time.Time
}

// Apply writes a plan change to a tenant.
func Apply(ctx context.Context, tx pgx.Tx, tenantID ids.ID, c Change) error {
	if c.Plan != nil && !c.Plan.Valid() {
		return errs.Invalid(errs.CodeValidation, "unknown plan %q", *c.Plan)
	}
	if c.Status != nil && !c.Status.Valid() {
		return errs.Invalid(errs.CodeValidation, "unknown status %q", *c.Status)
	}
	tag, err := tx.Exec(ctx, `
		UPDATE tenants SET
			plan           = COALESCE($2, plan),
			plan_status    = COALESCE($3, plan_status),
			trial_ends_at  = COALESCE($4, trial_ends_at),
			plan_renews_on = COALESCE($5::date, plan_renews_on),
			-- A new paid period is a fresh decision about cancelling.
			cancel_at_period_end = CASE WHEN $5::date IS NULL THEN cancel_at_period_end ELSE false END
		 WHERE id = $1`,
		tenantID, (*string)(c.Plan), (*string)(c.Status), c.TrialEndsAt, c.RenewsOn)
	if err != nil {
		return errs.Internal(err, "apply plan change")
	}
	if tag.RowsAffected() == 0 {
		return errs.NotFound("no such tenant")
	}
	return nil
}

// Provider is where a payment provider will plug in: it turns its webhook
// into a Change for the tenant it names. No implementation exists yet; the
// interface marks the seam so the columns above stay the one source of truth.
type Provider interface {
	HandleWebhook(ctx context.Context, payload []byte, signature string) (tenantID ids.ID, change Change, err error)
}
