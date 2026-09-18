// Package api assembles the HTTP server from its component handlers.
package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/NewMux/mtdrb_go/internal/auth"
	"github.com/NewMux/mtdrb_go/internal/config"
	"github.com/NewMux/mtdrb_go/internal/crm"
	"github.com/NewMux/mtdrb_go/internal/db"
	"github.com/NewMux/mtdrb_go/internal/httpx"
	"github.com/NewMux/mtdrb_go/internal/media"
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
	Auth  *auth.Handler
	CRM   *crm.Handler
	Media *media.Handler
	// TokenIssuer is used by the authentication middleware.
	TokenIssuer *auth.TokenIssuer
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

	r.Route("/v1", func(v1 chi.Router) {
		// Unauthenticated: obtaining credentials in the first place.
		v1.Mount("/auth", deps.Auth.Routes())

		// Everything else requires a valid access token.
		v1.Group(func(private chi.Router) {
			private.Use(auth.Authenticate(deps.TokenIssuer))
			private.Mount("/session", deps.Auth.AuthenticatedRoutes())

			// Media is reachable by both trainers and portal clients; the
			// handler binds the portal client id so row-level security
			// narrows each caller to what they may see.
			private.Mount("/media", deps.Media.Routes())

			// Trainer-only. RequireTrainer rejects portal sessions at the
			// route boundary, before any handler runs, so a client token
			// cannot reach another client's record even if a handler were
			// to forget its own check.
			private.Group(func(trainer chi.Router) {
				trainer.Use(httpx.RequireTrainer)
				trainer.Mount("/clients", deps.CRM.Routes())
				trainer.Mount("/waivers", deps.CRM.WaiverRoutes())
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
