//go:build integration

package api_test

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/NewMux/mtdrb_go/internal/auth"
	"github.com/NewMux/mtdrb_go/internal/mail"
	"github.com/NewMux/mtdrb_go/internal/platform/ids"
	"github.com/NewMux/mtdrb_go/internal/subscription"
	"github.com/NewMux/mtdrb_go/internal/testsupport"
)

// movingClock is a clock a journey can wind forward.
type movingClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *movingClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *movingClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// outbox keeps the mail a journey sends.
type outbox struct {
	mu   sync.Mutex
	sent []mail.Message
}

func (o *outbox) Send(_ context.Context, m mail.Message) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.sent = append(o.sent, m)
	return nil
}

func (o *outbox) messages() []mail.Message {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]mail.Message(nil), o.sent...)
}

const password = "correct-horse-battery-staple"

// session is a signed-in device.
type session struct {
	access, refresh, tenantID string
}

func (h *harness) signupSession(email string) session {
	h.t.Helper()
	status, body := h.do(http.MethodPost, "/v1/auth/signup", "", map[string]any{
		"email": email, "password": password, "display_name": "Sam Rivera",
		"business_name": "Rivera Strength", "currency": "AED", "timezone": "Asia/Dubai",
	})
	if status != http.StatusCreated {
		h.t.Fatalf("signup = %d %v", status, body)
	}
	return sessionFrom(h.t, body)
}

func sessionFrom(t *testing.T, body map[string]any) session {
	t.Helper()
	tokens := body["tokens"].(map[string]any)
	account := body["account"].(map[string]any)
	return session{
		access:   tokens["access_token"].(string),
		refresh:  tokens["refresh_token"].(string),
		tenantID: account["tenant_id"].(string),
	}
}

func (h *harness) refreshed(s session) session {
	h.t.Helper()
	status, body := h.do(http.MethodPost, "/v1/auth/refresh", "", map[string]any{"refresh_token": s.refresh})
	if status != http.StatusOK {
		h.t.Fatalf("refresh = %d %v", status, body)
	}
	return sessionFrom(h.t, body)
}

func errorCode(body map[string]any) string {
	e, _ := body["error"].(map[string]any)
	code, _ := e["code"].(string)
	return code
}

func TestSettingsFollowTheCountryAndLockTheCurrency(t *testing.T) {
	h := newHarness(t)
	s := h.signupSession("sam@rivera.test")

	status, got := h.do(http.MethodGet, "/v1/settings", s.access, nil)
	if status != http.StatusOK || got["currency"] != "AED" || got["week_start"] != float64(1) || got["currency_locked"] != false {
		t.Fatalf("defaults = %d %v", status, got)
	}

	// Picking Saudi Arabia picks a Sunday week.
	status, got = h.do(http.MethodPatch, "/v1/settings", s.access, map[string]any{
		"country": "SA", "language": "ar",
		"working_hours": map[string]any{"0": [][]string{{"16:00", "21:00"}, {"06:00", "10:00"}}},
		"targets":       map[string]any{"monthly_revenue_minor": 2500000},
	})
	if status != http.StatusOK || got["week_start"] != float64(0) || got["language"] != "ar" {
		t.Fatalf("patch = %d %v", status, got)
	}
	hours := got["working_hours"].(map[string]any)["0"].([]any)
	if hours[0].([]any)[0] != "06:00" {
		t.Errorf("working hours not in order: %v", hours)
	}

	if status, got := h.do(http.MethodPatch, "/v1/settings", s.access, map[string]any{
		"working_hours": map[string]any{"1": [][]string{{"10:00", "09:00"}}},
	}); status != http.StatusBadRequest {
		t.Errorf("backwards hours = %d %v", status, got)
	}

	// The device mirrors the practice's row.
	status, pulled := h.do(http.MethodGet, "/v1/sync/pull", s.access, nil)
	if status != http.StatusOK || !strings.Contains(fmt.Sprint(pulled["changes"]), "settings") {
		t.Fatalf("pull = %d %v", status, pulled)
	}

	// Once money has moved, the currency is fixed.
	h.issueSharedInvoice(s.access)
	status, got = h.do(http.MethodPatch, "/v1/settings", s.access, map[string]any{"currency": "SAR"})
	if status != http.StatusConflict || errorCode(got) != "currency_locked" {
		t.Fatalf("currency change after posting = %d %v", status, got)
	}
}

func TestTwoStepSignIn(t *testing.T) {
	c := &movingClock{t: time.Now().UTC()}
	h := newHarnessWith(t, harnessOptions{clock: c})
	s := h.signupSession("sam@rivera.test")

	status, setup := h.do(http.MethodPost, "/v1/session/mfa/setup", s.access, nil)
	if status != http.StatusOK || !strings.HasPrefix(setup["otpauth_uri"].(string), "otpauth://totp/") {
		t.Fatalf("setup = %d %v", status, setup)
	}
	secret := setup["secret"].(string)
	code := func() string {
		out, err := auth.TOTPCodeAt(secret, c.Now())
		if err != nil {
			t.Fatal(err)
		}
		return out
	}

	if status, body := h.do(http.MethodPost, "/v1/session/mfa/enable", s.access, map[string]any{"code": "000000"}); status != http.StatusBadRequest {
		t.Fatalf("a wrong code enabled two-step: %d %v", status, body)
	}
	status, enabled := h.do(http.MethodPost, "/v1/session/mfa/enable", s.access, map[string]any{"code": code()})
	if status != http.StatusOK {
		t.Fatalf("enable = %d %v", status, enabled)
	}
	recovery := enabled["recovery_codes"].([]any)
	if len(recovery) != auth.RecoveryCodeCount {
		t.Fatalf("got %d recovery codes", len(recovery))
	}

	login := func() string {
		t.Helper()
		status, body := h.do(http.MethodPost, "/v1/auth/login", "", map[string]any{"email": "sam@rivera.test", "password": password})
		if status != http.StatusUnauthorized || errorCode(body) != "mfa_required" {
			t.Fatalf("login with two-step = %d %v", status, body)
		}
		return body["error"].(map[string]any)["meta"].(map[string]any)["mfa_token"].(string)
	}

	// The challenge is not an access token.
	challenge := login()
	if status, _ := h.do(http.MethodGet, "/v1/clients", challenge, nil); status != http.StatusUnauthorized {
		t.Fatalf("a challenge opened the API: %d", status)
	}

	// The code that switched two-step on has been used.
	if status, body := h.do(http.MethodPost, "/v1/auth/mfa", "", map[string]any{"mfa_token": challenge, "code": code()}); status != http.StatusBadRequest {
		t.Fatalf("a used code signed in: %d %v", status, body)
	}
	c.advance(30 * time.Second)
	status, signedIn := h.do(http.MethodPost, "/v1/auth/mfa", "", map[string]any{"mfa_token": challenge, "code": code()})
	if status != http.StatusOK {
		t.Fatalf("mfa = %d %v", status, signedIn)
	}

	// A recovery code works once.
	first := recovery[0].(string)
	if status, body := h.do(http.MethodPost, "/v1/auth/mfa", "", map[string]any{"mfa_token": login(), "code": first}); status != http.StatusOK {
		t.Fatalf("recovery code = %d %v", status, body)
	}
	if status, _ := h.do(http.MethodPost, "/v1/auth/mfa", "", map[string]any{"mfa_token": login(), "code": first}); status != http.StatusBadRequest {
		t.Fatalf("a recovery code worked twice: %d", status)
	}

	// Turning it off takes the password, and sign-in is one step again.
	fresh := sessionFrom(t, signedIn)
	if status, body := h.do(http.MethodPost, "/v1/session/mfa/disable", fresh.access, map[string]any{"password": password}); status != http.StatusNoContent {
		t.Fatalf("disable = %d %v", status, body)
	}
	if status, body := h.do(http.MethodPost, "/v1/auth/login", "", map[string]any{"email": "sam@rivera.test", "password": password}); status != http.StatusOK {
		t.Fatalf("login after disable = %d %v", status, body)
	}
	status, profile := h.do(http.MethodGet, "/v1/session/profile", fresh.access, nil)
	if status != http.StatusOK || profile["mfa_enabled"] != false {
		t.Fatalf("profile = %d %v", status, profile)
	}
}

func TestChangingThePasswordSignsOutOtherDevices(t *testing.T) {
	h := newHarness(t)
	phone := h.signupSession("sam@rivera.test")

	resp, raw := h.doRaw(http.MethodPost, "/v1/auth/login", "", map[string]any{
		"email": "sam@rivera.test", "password": password,
	}, map[string]string{"User-Agent": "Mozilla/5.0 (Macintosh)"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("second login = %d %s", resp.StatusCode, raw)
	}
	laptopRefresh := regexp.MustCompile(`"refresh_token":"([^"]+)"`).FindStringSubmatch(raw)[1]

	status, listed := h.do(http.MethodGet, "/v1/session/devices", phone.access, nil)
	devices := listed["devices"].([]any)
	if status != http.StatusOK || len(devices) != 2 {
		t.Fatalf("devices = %d %v", status, listed)
	}
	current := 0
	for _, d := range devices {
		if d.(map[string]any)["current"] == true {
			current++
		}
	}
	if current != 1 {
		t.Fatalf("exactly one device should be this one, got %d", current)
	}

	if status, body := h.do(http.MethodPost, "/v1/session/password", phone.access, map[string]any{
		"current_password": "not-the-password", "new_password": "a-brand-new-passphrase",
	}); status != http.StatusBadRequest {
		t.Fatalf("wrong current password = %d %v", status, body)
	}
	if status, body := h.do(http.MethodPost, "/v1/session/password", phone.access, map[string]any{
		"current_password": password, "new_password": "a-brand-new-passphrase",
	}); status != http.StatusNoContent {
		t.Fatalf("change password = %d %v", status, body)
	}

	if status, _ := h.do(http.MethodPost, "/v1/auth/refresh", "", map[string]any{"refresh_token": laptopRefresh}); status != http.StatusUnauthorized {
		t.Fatalf("the laptop is still signed in: %d", status)
	}
	// The phone that made the change is not.
	h.refreshed(phone)
}

func TestPasswordResetByEmail(t *testing.T) {
	sent := &outbox{}
	h := newHarnessWith(t, harnessOptions{mailer: sent})
	s := h.signupSession("sam@rivera.test")

	// Nobody learns whether an address is registered.
	if status, _ := h.do(http.MethodPost, "/v1/auth/password/forgot", "", map[string]any{"email": "nobody@rivera.test"}); status != http.StatusAccepted {
		t.Fatalf("unknown address = %d", status)
	}
	if len(sent.messages()) != 0 {
		t.Fatal("mail went to an address with no account")
	}
	if status, _ := h.do(http.MethodPost, "/v1/auth/password/forgot", "", map[string]any{"email": "SAM@rivera.test"}); status != http.StatusAccepted {
		t.Fatalf("known address = %d", status)
	}
	messages := sent.messages()
	if len(messages) != 1 || messages[0].To != "sam@rivera.test" {
		t.Fatalf("mail = %+v", messages)
	}
	match := regexp.MustCompile(`https://app\.coachpulse\.test/reset-password\?token=(\S+)`).FindStringSubmatch(messages[0].Text)
	if match == nil {
		t.Fatalf("no link in %q", messages[0].Text)
	}
	token := match[1]

	if status, _ := h.do(http.MethodPost, "/v1/auth/password/reset", "", map[string]any{"token": token, "password": "short"}); status != http.StatusBadRequest {
		t.Fatalf("a weak password was accepted: %d", status)
	}
	if status, body := h.do(http.MethodPost, "/v1/auth/password/reset", "", map[string]any{"token": token, "password": "a-brand-new-passphrase"}); status != http.StatusNoContent {
		t.Fatalf("reset = %d %v", status, body)
	}
	if status, body := h.do(http.MethodPost, "/v1/auth/password/reset", "", map[string]any{"token": token, "password": "another-new-passphrase"}); status != http.StatusBadRequest || errorCode(body) != "reset_link_invalid" {
		t.Fatalf("a link worked twice: %d %v", status, body)
	}

	// Every device is signed out, and only the new password works.
	if status, _ := h.do(http.MethodPost, "/v1/auth/refresh", "", map[string]any{"refresh_token": s.refresh}); status != http.StatusUnauthorized {
		t.Fatalf("an old session survived the reset: %d", status)
	}
	if status, _ := h.do(http.MethodPost, "/v1/auth/login", "", map[string]any{"email": "sam@rivera.test", "password": password}); status != http.StatusUnauthorized {
		t.Fatalf("the old password still works: %d", status)
	}
	if status, _ := h.do(http.MethodPost, "/v1/auth/login", "", map[string]any{"email": "sam@rivera.test", "password": "a-brand-new-passphrase"}); status != http.StatusOK {
		t.Fatalf("the new password does not work: %d", status)
	}
}

func TestTheWebBuildKeepsItsRefreshTokenInACookie(t *testing.T) {
	h := newHarness(t)
	h.signupSession("sam@rivera.test")
	cookie := map[string]string{"X-Refresh-Transport": "cookie"}

	resp, raw := h.doRaw(http.MethodPost, "/v1/auth/login", "", map[string]any{
		"email": "sam@rivera.test", "password": password,
	}, cookie)
	if resp.StatusCode != http.StatusOK || !strings.Contains(raw, `"refresh_token":""`) {
		t.Fatalf("login = %d %s", resp.StatusCode, raw)
	}
	var set *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == "coachpulse_refresh" {
			set = c
		}
	}
	if set == nil || !set.HttpOnly || set.SameSite != http.SameSiteStrictMode || set.Value == "" {
		t.Fatalf("refresh cookie = %+v", set)
	}

	withCookie := map[string]string{"X-Refresh-Transport": "cookie", "Cookie": "coachpulse_refresh=" + set.Value}
	resp, raw = h.doRaw(http.MethodPost, "/v1/auth/refresh", "", map[string]any{}, withCookie)
	if resp.StatusCode != http.StatusOK || !strings.Contains(raw, `"access_token":"`) {
		t.Fatalf("refresh from cookie = %d %s", resp.StatusCode, raw)
	}
	// Rotated: the old cookie is spent.
	resp, _ = h.doRaw(http.MethodPost, "/v1/auth/refresh", "", map[string]any{}, withCookie)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("a spent cookie refreshed: %d", resp.StatusCode)
	}
}

func TestSignInIsRateLimitedPerAccount(t *testing.T) {
	h := newHarness(t)
	h.signupSession("sam@rivera.test")
	for i := 0; i < 10; i++ {
		if status, _ := h.do(http.MethodPost, "/v1/auth/login", "", map[string]any{"email": "sam@rivera.test", "password": "guess"}); status != http.StatusUnauthorized {
			t.Fatalf("guess %d = %d", i+1, status)
		}
	}
	status, body := h.do(http.MethodPost, "/v1/auth/login", "", map[string]any{"email": "sam@rivera.test", "password": password})
	if status != http.StatusTooManyRequests || errorCode(body) != "rate_limited" {
		t.Fatalf("eleventh attempt = %d %v", status, body)
	}
}

// setPlan changes a tenant's plan the way cmd/admin does.
func setPlan(t *testing.T, tenantID string, change subscription.Change) {
	t.Helper()
	owner := testsupport.OpenOwner(t)
	id, err := ids.Parse(tenantID)
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.InTenantTx(context.Background(), id, func(tx pgx.Tx) error {
		return subscription.Apply(context.Background(), tx, id, change)
	}); err != nil {
		t.Fatal(err)
	}
}

func TestALapsedTrialIsReadOnlyAndStarterHoldsTwentyFive(t *testing.T) {
	c := &movingClock{t: time.Now().UTC()}
	h := newHarnessWith(t, harnessOptions{clock: c})
	s := h.signupSession("sam@rivera.test")

	status, sub := h.do(http.MethodGet, "/v1/subscription", s.access, nil)
	if status != http.StatusOK || sub["plan"] != "trial" || sub["effective_plan"] != "pro" || sub["trial_days_left"] != float64(14) {
		t.Fatalf("subscription = %d %v", status, sub)
	}

	c.advance(15 * 24 * time.Hour)
	s = h.refreshed(s)

	// Reads still work; writes, and the whole sync batch, wait.
	if status, _ := h.do(http.MethodGet, "/v1/clients", s.access, nil); status != http.StatusOK {
		t.Fatalf("a lapsed trainer cannot read: %d", status)
	}
	status, body := h.do(http.MethodPost, "/v1/clients", s.access, map[string]any{"full_name": "Dana Rivers"})
	if status != http.StatusPaymentRequired || errorCode(body) != "subscription_inactive" {
		t.Fatalf("write while lapsed = %d %v", status, body)
	}
	if status, _ := h.do(http.MethodPost, "/v1/sync/push", s.access, map[string]any{"operations": []any{}}); status != http.StatusPaymentRequired {
		t.Fatalf("push while lapsed = %d", status)
	}
	// Settings and the plan itself stay reachable.
	if status, _ := h.do(http.MethodPatch, "/v1/settings", s.access, map[string]any{"language": "ar"}); status != http.StatusOK {
		t.Fatalf("settings while lapsed = %d", status)
	}

	starter := subscription.PlanStarter
	active := subscription.StatusActive
	setPlan(t, s.tenantID, subscription.Change{Plan: &starter, Status: &active})
	s = h.refreshed(s)

	for i := 0; i < 25; i++ {
		if status, body := h.do(http.MethodPost, "/v1/clients", s.access, map[string]any{"full_name": fmt.Sprintf("Client %02d", i)}); status != http.StatusCreated {
			t.Fatalf("client %d on Starter = %d %v", i+1, status, body)
		}
	}
	status, body = h.do(http.MethodPost, "/v1/clients", s.access, map[string]any{"full_name": "One Too Many"})
	if status != http.StatusForbidden || errorCode(body) != "plan_limit_reached" {
		t.Fatalf("26th client = %d %v", status, body)
	}
	// A lead costs nothing.
	if status, body := h.do(http.MethodPost, "/v1/clients", s.access, map[string]any{"full_name": "Maybe Later", "status": "lead"}); status != http.StatusCreated {
		t.Fatalf("lead on a full plan = %d %v", status, body)
	}

	status, sub = h.do(http.MethodGet, "/v1/subscription", s.access, nil)
	usage := sub["usage"].(map[string]any)
	limits := sub["limits"].(map[string]any)
	if status != http.StatusOK || usage["active_clients"] != float64(25) || limits["active_clients"] != float64(25) {
		t.Fatalf("subscription on Starter = %d %v", status, sub)
	}

	// Cancelling runs to the end of the period, and resuming undoes it.
	if status, body := h.do(http.MethodPost, "/v1/subscription/cancel", s.access, nil); status != http.StatusOK || body["cancel_at_period_end"] != true {
		t.Fatalf("cancel = %d %v", status, body)
	}
	if status, body := h.do(http.MethodPost, "/v1/subscription/resume", s.access, nil); status != http.StatusOK || body["cancel_at_period_end"] != false {
		t.Fatalf("resume = %d %v", status, body)
	}
}
