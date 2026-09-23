// Package app wires CoachPulse together.
//
// The same graph of services is needed by the API, the worker, the admin
// command, the demo recorder and the HTTP integration harness. Before this
// package each of them built it by hand, and they had already drifted: the
// harness never wired the dashboard, so the dashboard had no HTTP-level test.
// A service added here reaches every entrypoint at once.
package app

import (
	"time"

	"github.com/NewMux/mtdrb_go/internal/api"
	"github.com/NewMux/mtdrb_go/internal/auth"
	"github.com/NewMux/mtdrb_go/internal/billing"
	"github.com/NewMux/mtdrb_go/internal/catalog"
	"github.com/NewMux/mtdrb_go/internal/crm"
	"github.com/NewMux/mtdrb_go/internal/dashboard"
	"github.com/NewMux/mtdrb_go/internal/db"
	"github.com/NewMux/mtdrb_go/internal/jobs"
	"github.com/NewMux/mtdrb_go/internal/ledger"
	"github.com/NewMux/mtdrb_go/internal/mail"
	"github.com/NewMux/mtdrb_go/internal/media"
	"github.com/NewMux/mtdrb_go/internal/platform/clock"
	"github.com/NewMux/mtdrb_go/internal/programming"
	"github.com/NewMux/mtdrb_go/internal/scheduling"
	"github.com/NewMux/mtdrb_go/internal/settings"
	"github.com/NewMux/mtdrb_go/internal/subscription"
	"github.com/NewMux/mtdrb_go/internal/sync"
)

// Options are the few things that differ between entrypoints.
type Options struct {
	Pool  *db.Pool
	Clock clock.Clock

	JWTSigningKey   []byte
	AccessTokenTTL  time.Duration
	RefreshTokenTTL time.Duration
	// Argon2 is lowered by tests; production uses auth.DefaultArgon2Params.
	Argon2 auth.Argon2Params

	// ColumnKey encrypts the handful of columns ADR 0003 names.
	ColumnKey []byte

	// Presigner may be nil for entrypoints that never touch media, such as
	// the worker. The API always supplies one.
	Presigner  media.Presigner
	PresignTTL time.Duration

	// PublicBaseURL prefixes share links.
	PublicBaseURL string

	// Mailer sends password-reset links; nil drops them. AppURL is where
	// those links point.
	Mailer mail.Sender
	AppURL string
	// SecureCookies and TrustProxy are the auth transport's environment
	// switches; see auth.HandlerOptions.
	SecureCookies bool
	TrustProxy    bool
}

// Services is every domain service, built once.
type Services struct {
	Pool   *db.Pool
	Clock  clock.Clock
	Issuer *auth.TokenIssuer

	Ledger      *ledger.Service
	Programming *programming.Service
	Auth        *auth.Service
	CRM         *crm.Service
	Media       *media.Service
	Billing     *billing.Service
	Scheduling  *scheduling.Service
	Sync        *sync.Service
	Dashboard   *dashboard.Service

	publicBaseURL string
	authOptions   auth.HandlerOptions
}

// New builds the service graph.
func New(o Options) *Services {
	wall := o.Clock
	if wall == nil {
		wall = clock.System{}
	}
	argon := o.Argon2
	if argon == (auth.Argon2Params{}) {
		argon = auth.DefaultArgon2Params()
	}

	s := &Services{
		Pool: o.Pool, Clock: wall, publicBaseURL: o.PublicBaseURL,
		authOptions: auth.HandlerOptions{SecureCookies: o.SecureCookies, TrustProxy: o.TrustProxy},
	}
	s.Issuer = auth.NewTokenIssuer(o.JWTSigningKey, o.AccessTokenTTL, o.RefreshTokenTTL, wall)

	// Signup provisions the tenant's chart of accounts and exercise library
	// in the same transaction that creates the tenant: a tenant that cannot
	// post is not a usable one.
	s.Ledger = ledger.NewService(wall)
	s.Programming = programming.NewService(wall)
	s.Auth = auth.NewService(o.Pool, s.Issuer, s.Ledger, s.Programming, wall, argon).
		WithSecurity(auth.Security{ColumnKey: o.ColumnKey, Mailer: o.Mailer, AppURL: o.AppURL})

	s.CRM = crm.NewService(wall, o.ColumnKey)
	if o.Presigner != nil {
		s.Media = media.NewService(o.Presigner, wall, o.PresignTTL)
	}
	s.Billing = billing.NewService(s.Ledger, wall)
	s.Scheduling = scheduling.NewService(s.Billing, s.Ledger, wall)
	s.Sync = sync.NewService(wall)
	s.Dashboard = dashboard.NewService(s.Billing, s.Ledger, wall)
	return s
}

// Jobs is every scheduled job, for the worker. Order is the order they run
// in within a tick.
func (s *Services) Jobs() []jobs.Job {
	return []jobs.Job{
		jobs.PackageExpiry(s.Billing),
	}
}

// Handlers assembles the HTTP handlers the router mounts.
func (s *Services) Handlers() api.Deps {
	pool := s.Pool
	deps := api.Deps{
		Auth:         auth.NewHandler(s.Auth, s.authOptions),
		Settings:     settings.NewHandler(pool),
		Catalog:      catalog.NewHandler(pool, s.Clock),
		Subscription: subscription.NewHandler(pool, s.Clock),
		Clock:        s.Clock,
		CRM:          crm.NewHandler(s.CRM, pool),
		Scheduling:   scheduling.NewHandler(s.Scheduling, s.Billing, pool),
		Billing:      billing.NewHandler(s.Billing, pool, s.publicBaseURL),
		Programming:  programming.NewHandler(s.Programming, pool),
		Sync: sync.NewHandler(s.Sync, pool, sync.Dependencies{
			CRM:         s.CRM,
			Scheduling:  s.Scheduling,
			Billing:     s.Billing,
			Programming: s.Programming,
		}),
		Dashboard:   dashboard.NewHandler(s.Dashboard, pool),
		TokenIssuer: s.Issuer,
	}
	if s.Media != nil {
		deps.Media = media.NewHandler(s.Media, pool)
	}
	return deps
}
