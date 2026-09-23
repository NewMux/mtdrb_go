// Package api assembles the HTTP server from its component handlers.
package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/NewMux/mtdrb_go/internal/auth"
	"github.com/NewMux/mtdrb_go/internal/billing"
	"github.com/NewMux/mtdrb_go/internal/catalog"
	"github.com/NewMux/mtdrb_go/internal/config"
	"github.com/NewMux/mtdrb_go/internal/crm"
	"github.com/NewMux/mtdrb_go/internal/dashboard"
	"github.com/NewMux/mtdrb_go/internal/db"
	"github.com/NewMux/mtdrb_go/internal/httpx"
	"github.com/NewMux/mtdrb_go/internal/media"
	"github.com/NewMux/mtdrb_go/internal/platform/clock"
	"github.com/NewMux/mtdrb_go/internal/programming"
	"github.com/NewMux/mtdrb_go/internal/scheduling"
	"github.com/NewMux/mtdrb_go/internal/settings"
	"github.com/NewMux/mtdrb_go/internal/subscription"
	"github.com/NewMux/mtdrb_go/internal/sync"
)

// Server is the assembled HTTP application.
type Server struct {
	cfg    config.Config
	pool   *db.Pool
	log    *slog.Logger
	router chi.Router
}

// Deps are the collaborators the router mounts.
type Deps struct {
	Auth         *auth.Handler
	CRM          *crm.Handler
	Media        *media.Handler
	Scheduling   *scheduling.Handler
	Billing      *billing.Handler
	Programming  *programming.Handler
	Sync         *sync.Handler
	Dashboard    *dashboard.Handler
	Settings     *settings.Handler
	Catalog      *catalog.Handler
	Subscription *subscription.Handler
	// TokenIssuer is used by the authentication middleware.
	TokenIssuer *auth.TokenIssuer
	// Clock decides when a trial has lapsed.
	Clock clock.Clock
}

// New builds the server and its route table.
func New(cfg config.Config, pool *db.Pool, log *slog.Logger, deps Deps) *Server {
	s := &Server{cfg: cfg, pool: pool, log: log}
	s.router = s.routes(deps)
	return s
}

// Handler exposes the router, for tests and for embedding.
func (s *Server) Handler() http.Handler { return s.router }

func (s *Server) routes(deps Deps) chi.Router {
	r := chi.NewRouter()

	// Order matters: request ids exist before anything logs, the recoverer
	// wraps everything that can panic, and security headers apply even to
	// error responses produced further up.
	r.Use(httpx.RequestID)
	r.Use(httpx.AccessLog(s.log))
	r.Use(httpx.Recoverer)
	r.Use(httpx.SecurityHeaders)
	r.Use(httpx.CORS(s.cfg.CORSOrigins))

	r.Get("/healthz", s.healthz)
	r.Get("/readyz", s.readyz)

	// Shared invoice links. Mounted outside /v1 and outside authentication
	// entirely: the whole point is that a client with no account can open the
	// link their trainer sent them. Access is controlled by the unguessable
	// token, and the payload is deliberately narrow.
	r.Mount("/public", deps.Billing.PublicRoutes())

	r.Route("/v1", func(v1 chi.Router) {
		// Unauthenticated: obtaining credentials in the first place.
		v1.Mount("/auth", deps.Auth.Routes())

		// Everything else requires a valid access token.
		v1.Group(func(private chi.Router) {
			private.Use(auth.Authenticate(deps.TokenIssuer))

			// What a lapsed account still needs: signing in and out, its
			// security, its settings and the plan itself.
			private.Mount("/session", deps.Auth.AuthenticatedRoutes())
			private.Group(func(trainer chi.Router) {
				trainer.Use(httpx.RequireTrainer)
				trainer.Mount("/settings", deps.Settings.Routes())
				trainer.Mount("/subscription", deps.Subscription.Routes())
			})

			// Everything below is read-only once a trial or plan has lapsed:
			// the records stay readable, and writes wait for a renewal.
			private.Group(func(guarded chi.Router) {
				guarded.Use(subscription.ReadOnlyWhenLapsed(deps.Clock))

				// Media is reachable by both trainers and portal clients; the
				// handler binds the portal client id so row-level security
				// narrows each caller to what they may see.
				guarded.Mount("/media", deps.Media.Routes())

				// Trainer-only. RequireTrainer rejects portal sessions at the
				// route boundary, before any handler runs, so a client token
				// cannot reach another client's record even if a handler were
				// to forget its own check.
				guarded.Group(func(trainer chi.Router) {
					trainer.Use(httpx.RequireTrainer)
					trainer.Mount("/clients", deps.CRM.Routes())
					trainer.Mount("/waivers", deps.CRM.WaiverRoutes())
					trainer.Mount("/sessions", deps.Scheduling.Routes())
					trainer.Mount("/credits", deps.Scheduling.CreditRoutes())

					// Money endpoints carry idempotency, so the offline outbox
					// can retry a payment without recording it twice.
					trainer.Group(func(m chi.Router) {
						m.Use(httpx.Idempotent(s.pool))
						m.Mount("/invoices", deps.Billing.InvoiceRoutes())
					})
					trainer.Mount("/payment-methods", deps.Billing.PaymentMethodRoutes())
					trainer.Mount("/receivables", deps.Billing.ReceivablesRoutes())
					trainer.Mount("/programs", deps.Programming.ProgramRoutes())
					trainer.Mount("/locations", deps.Catalog.LocationRoutes())
					trainer.Mount("/package-offers", deps.Catalog.OfferRoutes())

					// Trainer-only: it carries revenue and receivables, which a
					// portal client must never see.
					trainer.Mount("/dashboard", deps.Dashboard.Routes())
				})

				// Reachable by portal clients too: logging your own sets is the
				// point of the companion app, and the library is what tells you
				// what the movement is. Row-level security narrows both.
				guarded.Group(func(shared chi.Router) {
					shared.Mount("/exercises", deps.Programming.ExerciseRoutes())
					shared.Mount("/workouts", deps.Programming.WorkoutRoutes())
					shared.Mount("/sync", deps.Sync.Routes())
				})
			})
		})
	})

	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		httpx.JSON(w, r, http.StatusNotFound, map[string]any{
			"error": map[string]string{"code": "not_found", "message": "no such endpoint"},
		})
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		httpx.JSON(w, r, http.StatusMethodNotAllowed, map[string]any{
			"error": map[string]string{"code": "method_not_allowed", "message": "method not allowed on this endpoint"},
		})
	})

	return r
}

// healthz reports that the process is alive. It deliberately does not touch
// the database: a liveness probe that fails on a database blip restarts a
// healthy process and makes an outage worse.
func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, r, http.StatusOK, map[string]string{"status": "ok"})
}

// readyz reports whether the process can serve traffic, which does require a
// working database.
func (s *Server) readyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	if err := s.pool.Ping(ctx); err != nil {
		s.log.ErrorContext(ctx, "readiness check failed", slog.Any("error", err))
		httpx.JSON(w, r, http.StatusServiceUnavailable, map[string]string{
			"status": "unavailable", "detail": "database is unreachable",
		})
		return
	}
	httpx.JSON(w, r, http.StatusOK, map[string]string{"status": "ready"})
}

// Run serves until ctx is cancelled, then drains in-flight requests.
func (s *Server) Run(ctx context.Context) error {
	srv := &http.Server{
		Addr:    s.cfg.HTTPAddr,
		Handler: s.router,
		// Generous enough for a slow mobile connection on a gym floor, tight
		// enough that a stalled peer cannot hold a connection indefinitely.
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		ErrorLog:          slog.NewLogLogger(s.log.Handler(), slog.LevelError),
	}

	errCh := make(chan error, 1)
	go func() {
		s.log.Info("http server listening", slog.String("addr", s.cfg.HTTPAddr), slog.String("env", s.cfg.Env))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		s.log.Info("shutting down", slog.Duration("grace", s.cfg.ShutdownTimeout))
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.cfg.ShutdownTimeout)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
}
