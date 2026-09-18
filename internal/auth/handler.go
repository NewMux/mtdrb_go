package auth

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/NewMux/mtdrb_go/internal/httpx"
	"github.com/NewMux/mtdrb_go/internal/tenancy"
)

// Handler exposes the authentication endpoints.
type Handler struct {
	svc *Service
}

// NewHandler builds the authentication handler.
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// Routes returns the unauthenticated authentication subtree.
func (h *Handler) Routes() http.Handler {
	r := chi.NewRouter()
	r.Post("/signup", h.signup)
	r.Post("/login", h.login)
	r.Post("/refresh", h.refresh)
	return r
}

// AuthenticatedRoutes returns endpoints that require a valid access token.
func (h *Handler) AuthenticatedRoutes() http.Handler {
	r := chi.NewRouter()
	r.Post("/logout", h.logout)
	r.Get("/me", h.me)
	return r
}

type signupRequest struct {
	Email        string `json:"email"`
	Password     string `json:"password"`
	DisplayName  string `json:"display_name"`
	BusinessName string `json:"business_name"`
	Currency     string `json:"currency"`
	Timezone     string `json:"timezone"`
}

type sessionResponse struct {
	Account Account `json:"account"`
	Tokens  Tokens  `json:"tokens"`
}

func (h *Handler) signup(w http.ResponseWriter, r *http.Request) {
	var req signupRequest
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	// Mapped field by field rather than converted: the wire shape and the
	// service input are allowed to diverge, and a silent conversion would
	// break the moment either gains a field the other should not carry.
	//nolint:staticcheck // S1016: decoupling is deliberate.
	account, tokens, err := h.svc.Signup(r.Context(), SignupInput{
		Email:        req.Email,
		Password:     req.Password,
		DisplayName:  req.DisplayName,
		BusinessName: req.BusinessName,
		Currency:     req.Currency,
		Timezone:     req.Timezone,
	}, r.UserAgent())
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, r, http.StatusCreated, sessionResponse{Account: account, Tokens: tokens})
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	account, tokens, err := h.svc.Login(r.Context(), req.Email, req.Password, r.UserAgent())
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, r, http.StatusOK, sessionResponse{Account: account, Tokens: tokens})
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

func (h *Handler) refresh(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	account, tokens, err := h.svc.Refresh(r.Context(), req.RefreshToken, r.UserAgent())
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, r, http.StatusOK, sessionResponse{Account: account, Tokens: tokens})
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	principal, err := tenancy.Require(r.Context())
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	var req refreshRequest
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	if err := h.svc.Logout(r.Context(), principal.TenantID, req.RefreshToken); err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.NoContent(w, r)
}

type meResponse struct {
	TenantID  string `json:"tenant_id"`
	SubjectID string `json:"subject_id"`
	Kind      string `json:"kind"`
	Role      string `json:"role,omitempty"`
}

func (h *Handler) me(w http.ResponseWriter, r *http.Request) {
	principal, err := tenancy.Require(r.Context())
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, r, http.StatusOK, meResponse{
		TenantID:  principal.TenantID.String(),
		SubjectID: principal.SubjectID.String(),
		Kind:      string(principal.Kind),
		Role:      principal.Role,
	})
}
