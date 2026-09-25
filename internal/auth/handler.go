package auth

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/NewMux/mtdrb_go/internal/httpx"
	"github.com/NewMux/mtdrb_go/internal/platform/errs"
	"github.com/NewMux/mtdrb_go/internal/platform/ids"
	"github.com/NewMux/mtdrb_go/internal/platform/ratelimit"
	"github.com/NewMux/mtdrb_go/internal/tenancy"
)

// HandlerOptions are the transport decisions that differ by environment.
type HandlerOptions struct {
	// SecureCookies marks the web refresh cookie Secure. Off only for local
	// development over plain http, where a Secure cookie would never be sent.
	SecureCookies bool
	// Limiters default to sensible buckets when nil.
	PerAddress *ratelimit.Limiter
	PerAccount *ratelimit.Limiter
}

// Handler exposes the authentication endpoints.
type Handler struct {
	svc  *Service
	opts HandlerOptions
}

// NewHandler builds the authentication handler.
func NewHandler(svc *Service, opts HandlerOptions) *Handler {
	if opts.PerAddress == nil {
		// Thirty attempts at once from one address, then one every ten
		// seconds: an office sharing an address signs in fine, a script does
		// not get far.
		opts.PerAddress = ratelimit.New(30, 10*time.Second, svc.clock)
	}
	if opts.PerAccount == nil {
		// Ten tries at one account, then one every ninety seconds, from
		// anywhere: the brake on a guesser with many addresses.
		opts.PerAccount = ratelimit.New(10, 90*time.Second, svc.clock)
	}
	return &Handler{svc: svc, opts: opts}
}

// Routes returns the unauthenticated authentication subtree.
func (h *Handler) Routes() http.Handler {
	r := chi.NewRouter()
	r.Use(h.limitByAddress)
	r.Post("/signup", h.signup)
	r.Post("/login", h.login)
	r.Post("/mfa", h.verifyMFA)
	r.Post("/refresh", h.refresh)
	r.Post("/password/forgot", h.forgotPassword)
	r.Post("/password/reset", h.resetPassword)
	return r
}

// AuthenticatedRoutes returns endpoints that require a valid access token.
func (h *Handler) AuthenticatedRoutes() http.Handler {
	r := chi.NewRouter()
	r.Post("/logout", h.logout)
	r.Get("/me", h.me)
	r.Get("/profile", h.profile)
	r.Patch("/profile", h.updateProfile)
	r.Post("/password", h.changePassword)
	r.Post("/mfa/setup", h.beginMFA)
	r.Post("/mfa/enable", h.enableMFA)
	r.Post("/mfa/disable", h.disableMFA)
	r.Get("/devices", h.devices)
	r.Delete("/devices/{deviceID}", h.signOutDevice)
	r.Post("/devices/sign-out-others", h.signOutOthers)
	r.Post("/delete-account", h.deleteAccount)
	return r
}

// ---------------------------------------------------------------------------
// Rate limiting

func (h *Handler) limitByAddress(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !h.opts.PerAddress.Allow(httpx.ClientAddress(r)) {
			httpx.Error(w, r, errs.RateLimited("too many attempts; wait a minute and try again"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// limitAccount spends from an account's own bucket, whoever is asking.
func (h *Handler) limitAccount(key string) error {
	if !h.opts.PerAccount.Allow(strings.ToLower(strings.TrimSpace(key))) {
		return errs.RateLimited("too many attempts for this account; wait a few minutes and try again")
	}
	return nil
}

// ---------------------------------------------------------------------------
// The web refresh cookie
//
// A native app keeps its refresh token in the keychain. A browser has nowhere
// safe to put one — localStorage is readable by any script on the page — so
// the web build asks for it as an httpOnly cookie instead, by sending
// X-Refresh-Transport: cookie. The body then carries no refresh token at all,
// and refresh and logout read it from the cookie. SameSite=Strict keeps
// another site from making the browser present it.

const (
	refreshCookie   = "coachpulse_refresh"
	transportHeader = "X-Refresh-Transport"
)

func wantsCookie(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get(transportHeader), "cookie")
}

func (h *Handler) setRefreshCookie(w http.ResponseWriter, token string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     refreshCookie,
		Value:    token,
		Path:     "/v1",
		Expires:  expires,
		HttpOnly: true,
		Secure:   h.opts.SecureCookies,
		SameSite: http.SameSiteStrictMode,
	})
}

func (h *Handler) clearRefreshCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: refreshCookie, Value: "", Path: "/v1", MaxAge: -1,
		HttpOnly: true, Secure: h.opts.SecureCookies, SameSite: http.SameSiteStrictMode,
	})
}

// presentedRefresh is the refresh token from the body, or else the cookie.
func presentedRefresh(r *http.Request, fromBody string) string {
	if strings.TrimSpace(fromBody) != "" {
		return fromBody
	}
	if c, err := r.Cookie(refreshCookie); err == nil {
		return c.Value
	}
	return ""
}

type sessionResponse struct {
	Account Account `json:"account"`
	Tokens  Tokens  `json:"tokens"`
}

func (h *Handler) respondSession(w http.ResponseWriter, r *http.Request, status int, account Account, tokens Tokens) {
	if wantsCookie(r) {
		h.setRefreshCookie(w, tokens.RefreshToken, tokens.RefreshExpiresAt)
		tokens.RefreshToken = ""
	}
	httpx.JSON(w, r, status, sessionResponse{Account: account, Tokens: tokens})
}

// ---------------------------------------------------------------------------
// Signing in

type signupRequest struct {
	Email        string `json:"email"`
	Password     string `json:"password"`
	DisplayName  string `json:"display_name"`
	BusinessName string `json:"business_name"`
	Currency     string `json:"currency"`
	Timezone     string `json:"timezone"`
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
	h.respondSession(w, r, http.StatusCreated, account, tokens)
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
	if err := h.limitAccount("login:" + req.Email); err != nil {
		httpx.Error(w, r, err)
		return
	}
	account, tokens, err := h.svc.Login(r.Context(), req.Email, req.Password, r.UserAgent())
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	h.respondSession(w, r, http.StatusOK, account, tokens)
}

type mfaRequest struct {
	MFAToken string `json:"mfa_token"`
	Code     string `json:"code"`
}

func (h *Handler) verifyMFA(w http.ResponseWriter, r *http.Request) {
	var req mfaRequest
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	// Keyed by the account the challenge names, so six digits cannot be
	// walked by spreading guesses across fresh challenges.
	_, userID, err := h.svc.issuer.ParseMFAChallenge(req.MFAToken)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	if err := h.limitAccount("mfa:" + userID.String()); err != nil {
		httpx.Error(w, r, err)
		return
	}
	account, tokens, err := h.svc.VerifyMFA(r.Context(), req.MFAToken, req.Code, r.UserAgent())
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	h.respondSession(w, r, http.StatusOK, account, tokens)
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
	account, tokens, err := h.svc.Refresh(r.Context(), presentedRefresh(r, req.RefreshToken), r.UserAgent())
	if err != nil {
		if wantsCookie(r) {
			h.clearRefreshCookie(w)
		}
		httpx.Error(w, r, err)
		return
	}
	h.respondSession(w, r, http.StatusOK, account, tokens)
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
	if err := h.svc.Logout(r.Context(), principal.TenantID, presentedRefresh(r, req.RefreshToken)); err != nil {
		httpx.Error(w, r, err)
		return
	}
	h.clearRefreshCookie(w)
	httpx.NoContent(w, r)
}

// ---------------------------------------------------------------------------
// Password reset

type forgotRequest struct {
	Email string `json:"email"`
}

func (h *Handler) forgotPassword(w http.ResponseWriter, r *http.Request) {
	var req forgotRequest
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	if err := h.limitAccount("forgot:" + req.Email); err != nil {
		httpx.Error(w, r, err)
		return
	}
	if err := h.svc.RequestPasswordReset(r.Context(), req.Email); err != nil {
		httpx.Error(w, r, err)
		return
	}
	// Accepted whether or not the address has an account.
	httpx.JSON(w, r, http.StatusAccepted, map[string]string{"status": "sent_if_registered"})
}

type resetRequest struct {
	Token    string `json:"token"`
	Password string `json:"password"`
}

func (h *Handler) resetPassword(w http.ResponseWriter, r *http.Request) {
	var req resetRequest
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	if err := h.svc.ResetPassword(r.Context(), req.Token, req.Password); err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.NoContent(w, r)
}

// ---------------------------------------------------------------------------
// Signed in

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

func (h *Handler) profile(w http.ResponseWriter, r *http.Request) {
	profile, err := h.svc.Profile(r.Context())
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, r, http.StatusOK, profile)
}

type profileRequest struct {
	DisplayName     *string `json:"display_name"`
	Email           *string `json:"email"`
	CurrentPassword string  `json:"current_password"`
}

func (h *Handler) updateProfile(w http.ResponseWriter, r *http.Request) {
	var req profileRequest
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	// Mapped field by field, as signup is: the wire shape and the service
	// input may diverge.
	//nolint:staticcheck // S1016: decoupling is deliberate.
	profile, err := h.svc.UpdateProfile(r.Context(), ProfileUpdate{
		DisplayName: req.DisplayName, Email: req.Email, CurrentPassword: req.CurrentPassword,
	})
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, r, http.StatusOK, profile)
}

type passwordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

func (h *Handler) changePassword(w http.ResponseWriter, r *http.Request) {
	var req passwordRequest
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	if err := h.svc.ChangePassword(r.Context(), req.CurrentPassword, req.NewPassword); err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.NoContent(w, r)
}

func (h *Handler) beginMFA(w http.ResponseWriter, r *http.Request) {
	setup, err := h.svc.BeginMFA(r.Context())
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, r, http.StatusOK, setup)
}

type codeRequest struct {
	Code string `json:"code"`
}

func (h *Handler) enableMFA(w http.ResponseWriter, r *http.Request) {
	var req codeRequest
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	codes, err := h.svc.EnableMFA(r.Context(), req.Code)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, r, http.StatusOK, map[string][]string{"recovery_codes": codes})
}

type disableRequest struct {
	Password string `json:"password"`
}

func (h *Handler) deleteAccount(w http.ResponseWriter, r *http.Request) {
	var req disableRequest
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	deletion, err := h.svc.DeleteAccount(r.Context(), req.Password)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	// Every session is already revoked; the web build's cookie goes too, or
	// a reload would try to refresh with it.
	h.clearRefreshCookie(w)
	httpx.JSON(w, r, http.StatusOK, deletion)
}

func (h *Handler) disableMFA(w http.ResponseWriter, r *http.Request) {
	var req disableRequest
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	if err := h.svc.DisableMFA(r.Context(), req.Password); err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.NoContent(w, r)
}

func (h *Handler) devices(w http.ResponseWriter, r *http.Request) {
	list, err := h.svc.Devices(r.Context())
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, r, http.StatusOK, map[string]any{"devices": list})
}

func (h *Handler) signOutDevice(w http.ResponseWriter, r *http.Request) {
	id, err := ids.Parse(chi.URLParam(r, "deviceID"))
	if err != nil {
		httpx.Error(w, r, errs.Invalid(errs.CodeValidation, "deviceID is not a valid identifier"))
		return
	}
	if err := h.svc.SignOutDevice(r.Context(), id); err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.NoContent(w, r)
}

func (h *Handler) signOutOthers(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.SignOutOtherDevices(r.Context()); err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.NoContent(w, r)
}
