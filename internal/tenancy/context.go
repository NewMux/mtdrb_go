// Package tenancy carries the authenticated principal through a request.
//
// The principal is set once by authentication middleware and read by handlers.
// It is deliberately the only way to learn the caller's tenant: handlers never
// accept a tenant id from a path, query or body, because a caller-supplied
// tenant is a caller-chosen tenant.
package tenancy

import (
	"context"

	"github.com/NewMux/mtdrb_go/internal/platform/errs"
	"github.com/NewMux/mtdrb_go/internal/platform/ids"
)

// Kind distinguishes who a principal is.
//
// Trainers and training clients are separate kinds rather than two values of a
// role field: a client must never be able to reach trainer financial data, and
// keeping the distinction in its own type means a handler cannot confuse a
// privileged role string with an unprivileged one.
type Kind string

const (
	// KindUser is a trainer or staff member of a tenant.
	KindUser Kind = "user"
	// KindClient is a training client using the companion portal.
	KindClient Kind = "client"
)

// Principal is the authenticated caller.
type Principal struct {
	TenantID ids.ID
	// SubjectID is the user id for a trainer, or the client id for a portal
	// session.
	SubjectID ids.ID
	Kind      Kind
	Role      string
}

// IsTrainer reports whether the caller is trainer-side staff.
func (p Principal) IsTrainer() bool { return p.Kind == KindUser }

// IsClient reports whether the caller is a training client on the portal.
func (p Principal) IsClient() bool { return p.Kind == KindClient }

// IsOwner reports whether the caller owns the tenant.
func (p Principal) IsOwner() bool { return p.IsTrainer() && p.Role == "owner" }

type principalKey struct{}

// WithPrincipal returns a context carrying the authenticated caller.
func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

// FromContext returns the authenticated caller, or false on an unauthenticated
// request.
func FromContext(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(Principal)
	return p, ok
}

// Require returns the authenticated caller or an unauthorized error. Handlers
// mounted behind authentication middleware use this; the error path only
// triggers if a route is misconfigured.
func Require(ctx context.Context) (Principal, error) {
	p, ok := FromContext(ctx)
	if !ok {
		return Principal{}, errs.Unauthorized(errs.CodeInvalidCredentials, "authentication is required")
	}
	return p, nil
}

// RequireTrainer returns the caller only if they are trainer-side staff.
//
// This is the guard on every financial and cross-client endpoint. The PRD is
// explicit that clients have zero access to trainer financial summaries,
// expenses or other clients' records; RLS enforces that at the row level and
// this enforces it at the route level.
func RequireTrainer(ctx context.Context) (Principal, error) {
	p, err := Require(ctx)
	if err != nil {
		return Principal{}, err
	}
	if !p.IsTrainer() {
		return Principal{}, errs.Forbidden(errs.CodeTenantMismatch,
			"this resource is not available to client portal sessions")
	}
	return p, nil
}

// RequireOwner returns the caller only if they own the tenant.
func RequireOwner(ctx context.Context) (Principal, error) {
	p, err := RequireTrainer(ctx)
	if err != nil {
		return Principal{}, err
	}
	if !p.IsOwner() {
		return Principal{}, errs.Forbidden(errs.CodeTenantMismatch,
			"this action requires the account owner")
	}
	return p, nil
}
