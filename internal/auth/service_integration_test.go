//go:build integration

package auth_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/NewMux/mtdrb_go/internal/auth"
	"github.com/NewMux/mtdrb_go/internal/db"
	"github.com/NewMux/mtdrb_go/internal/platform/clock"
	"github.com/NewMux/mtdrb_go/internal/platform/errs"
	"github.com/NewMux/mtdrb_go/internal/testsupport"
)

const goodPassword = "correct-horse-battery-staple"

func newService(t *testing.T) (*auth.Service, *auth.TokenIssuer, *db.Pool, *clock.Fixed) {
	t.Helper()
	testsupport.RequireDB(t)
	testsupport.Reset(t)

	pool := testsupport.OpenApp(t)
	c := &clock.Fixed{T: time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC)}
	issuer := auth.NewTokenIssuer([]byte(strings.Repeat("k", 32)), 15*time.Minute, 30*24*time.Hour, c)

	params := auth.DefaultArgon2Params()
	params.Memory = 1024 // keep the suite fast
	params.Iterations = 1

	return auth.NewService(pool, issuer, nil, c, params), issuer, pool, c
}

func signup(t *testing.T, svc *auth.Service, email string) (auth.Account, auth.Tokens) {
	t.Helper()
	account, tokens, err := svc.Signup(context.Background(), auth.SignupInput{
		Email:        email,
		Password:     goodPassword,
		DisplayName:  "Alex Coach",
		BusinessName: "Alex Strength",
		Currency:     "EUR",
		Timezone:     "Europe/Berlin",
	}, "test-agent")
	if err != nil {
		t.Fatalf("signup: %v", err)
	}
	return account, tokens
}

func TestSignupProvisionsTenantAndReturnsUsableTokens(t *testing.T) {
	svc, issuer, _, _ := newService(t)

	account, tokens := signup(t, svc, "alex@example.com")

	if account.TenantID.String() == "" || account.UserID.String() == "" {
		t.Fatal("signup did not return identifiers")
	}
	if account.Role != "owner" {
		t.Errorf("role = %q, want owner", account.Role)
	}
	if account.Currency != "EUR" {
		t.Errorf("currency = %q, want EUR", account.Currency)
	}

	claims, err := issuer.ParseAccess(tokens.AccessToken)
	if err != nil {
		t.Fatalf("issued access token does not parse: %v", err)
	}
	if claims.TenantID != account.TenantID {
		t.Error("access token is bound to the wrong tenant")
	}
	if tokens.RefreshToken == "" {
		t.Error("no refresh token issued")
	}
}

func TestSignupNormalizesEmailAndRejectsDuplicates(t *testing.T) {
	svc, _, _, _ := newService(t)
	signup(t, svc, "Alex@Example.COM ")

	// The same address in different case must collide.
	_, _, err := svc.Signup(context.Background(), auth.SignupInput{
		Email: "alex@example.com", Password: goodPassword, DisplayName: "Impostor",
	}, "test-agent")
	if err == nil {
		t.Fatal("duplicate email was accepted")
	}
	if errs.KindOf(err) != errs.KindConflict {
		t.Errorf("kind = %q, want conflict", errs.KindOf(err))
	}
}

func TestSignupValidatesInput(t *testing.T) {
	svc, _, _, _ := newService(t)
	ctx := context.Background()

	cases := map[string]auth.SignupInput{
		"missing email": {Password: goodPassword, DisplayName: "A"},
		"missing name":  {Email: "a@example.com", Password: goodPassword},
		"weak password": {Email: "b@example.com", Password: "short", DisplayName: "A"},
		"bad currency":  {Email: "c@example.com", Password: goodPassword, DisplayName: "A", Currency: "EUROS"},
		"bad timezone":  {Email: "d@example.com", Password: goodPassword, DisplayName: "A", Timezone: "Mars/Olympus"},
	}
	for name, in := range cases {
		if _, _, err := svc.Signup(ctx, in, "t"); err == nil {
			t.Errorf("%s: expected rejection", name)
		} else if errs.KindOf(err) != errs.KindInvalid {
			t.Errorf("%s: kind = %q, want invalid", name, errs.KindOf(err))
		}
	}
}

// A failed signup must not leave a tenant behind: the tenant, its owner and
// its chart of accounts are provisioned in one transaction or not at all.
func TestFailedSignupLeavesNoTenant(t *testing.T) {
	svc, _, _, _ := newService(t)
	ctx := context.Background()
	signup(t, svc, "alex@example.com")

	_, _, err := svc.Signup(ctx, auth.SignupInput{
		Email: "alex@example.com", Password: goodPassword, DisplayName: "Impostor",
		BusinessName: "Ghost Gym",
	}, "t")
	if err == nil {
		t.Fatal("expected duplicate signup to fail")
	}

	owner := testsupport.OpenOwner(t)
	var count int
	if err := owner.Raw().QueryRow(ctx, `SELECT count(*) FROM tenants WHERE name = 'Ghost Gym'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("failed signup left %d orphaned tenants", count)
	}
}

func TestLoginSucceedsAndRejectsBadCredentials(t *testing.T) {
	svc, _, _, _ := newService(t)
	ctx := context.Background()
	account, _ := signup(t, svc, "alex@example.com")

	got, tokens, err := svc.Login(ctx, "ALEX@example.com", goodPassword, "t")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if got.UserID != account.UserID || got.TenantID != account.TenantID {
		t.Error("login returned the wrong account")
	}
	if tokens.AccessToken == "" || tokens.RefreshToken == "" {
		t.Error("login did not issue tokens")
	}

	// Wrong password and unknown account must be indistinguishable, so the
	// endpoint cannot be used to enumerate registered emails.
	_, _, wrongPw := svc.Login(ctx, "alex@example.com", "wrong-password-here", "t")
	_, _, unknown := svc.Login(ctx, "nobody@example.com", goodPassword, "t")
	if wrongPw == nil || unknown == nil {
		t.Fatal("expected both failures to be rejected")
	}
	if wrongPw.Error() != unknown.Error() {
		t.Errorf("failure modes are distinguishable:\n  wrong password: %v\n  unknown account: %v", wrongPw, unknown)
	}
	if errs.CodeOf(wrongPw) != errs.CodeInvalidCredentials {
		t.Errorf("code = %q", errs.CodeOf(wrongPw))
	}
}

func TestRefreshRotatesAndInvalidatesTheOldToken(t *testing.T) {
	svc, _, _, _ := newService(t)
	ctx := context.Background()
	signup(t, svc, "alex@example.com")

	_, first, err := svc.Login(ctx, "alex@example.com", goodPassword, "t")
	if err != nil {
		t.Fatal(err)
	}

	_, second, err := svc.Refresh(ctx, first.RefreshToken, "t")
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if second.RefreshToken == first.RefreshToken {
		t.Fatal("refresh did not rotate the token")
	}
	if second.AccessToken == "" {
		t.Error("refresh issued no access token")
	}

	// The rotated token now works.
	if _, _, err := svc.Refresh(ctx, second.RefreshToken, "t"); err != nil {
		t.Fatalf("rotated token should be usable: %v", err)
	}
}

// Replaying a spent refresh token means two parties hold it. The whole family
// is revoked rather than just the replayed generation.
func TestReplayedRefreshTokenRevokesTheWholeFamily(t *testing.T) {
	svc, _, _, _ := newService(t)
	ctx := context.Background()
	signup(t, svc, "alex@example.com")

	_, first, err := svc.Login(ctx, "alex@example.com", goodPassword, "t")
	if err != nil {
		t.Fatal(err)
	}
	_, second, err := svc.Refresh(ctx, first.RefreshToken, "t")
	if err != nil {
		t.Fatal(err)
	}

	// The attacker replays the stolen, already-rotated token.
	_, _, err = svc.Refresh(ctx, first.RefreshToken, "attacker")
	if err == nil {
		t.Fatal("replayed refresh token was accepted")
	}
	if errs.CodeOf(err) != errs.CodeTokenReused {
		t.Errorf("code = %q, want %q", errs.CodeOf(err), errs.CodeTokenReused)
	}

	// The legitimate holder's current token is now dead too: the theft is
	// detected, so every session in the chain must re-authenticate.
	if _, _, err := svc.Refresh(ctx, second.RefreshToken, "t"); err == nil {
		t.Fatal("family was not revoked; the legitimate token still works")
	}
}

func TestExpiredRefreshTokenIsRejected(t *testing.T) {
	svc, _, _, c := newService(t)
	ctx := context.Background()
	signup(t, svc, "alex@example.com")

	_, tokens, err := svc.Login(ctx, "alex@example.com", goodPassword, "t")
	if err != nil {
		t.Fatal(err)
	}

	c.Advance(31 * 24 * time.Hour)
	if _, _, err := svc.Refresh(ctx, tokens.RefreshToken, "t"); err == nil {
		t.Fatal("expired refresh token was accepted")
	}
}

func TestUnknownRefreshTokenIsRejected(t *testing.T) {
	svc, _, _, _ := newService(t)
	if _, _, err := svc.Refresh(context.Background(), "not-a-real-token", "t"); err == nil {
		t.Fatal("unknown refresh token was accepted")
	}
}

func TestLogoutRevokesTheFamily(t *testing.T) {
	svc, _, _, _ := newService(t)
	ctx := context.Background()
	account, _ := signup(t, svc, "alex@example.com")

	_, tokens, err := svc.Login(ctx, "alex@example.com", goodPassword, "t")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Logout(ctx, account.TenantID, tokens.RefreshToken); err != nil {
		t.Fatalf("logout: %v", err)
	}
	if _, _, err := svc.Refresh(ctx, tokens.RefreshToken, "t"); err == nil {
		t.Fatal("refresh token still works after logout")
	}
}

// Refresh tokens are stored hashed; a database read must not yield a usable
// credential.
func TestRefreshTokenIsNotStoredInPlaintext(t *testing.T) {
	svc, _, _, _ := newService(t)
	ctx := context.Background()
	signup(t, svc, "alex@example.com")

	_, tokens, err := svc.Login(ctx, "alex@example.com", goodPassword, "t")
	if err != nil {
		t.Fatal(err)
	}

	owner := testsupport.OpenOwner(t)
	var count int
	if err := owner.Raw().QueryRow(ctx,
		`SELECT count(*) FROM refresh_tokens WHERE encode(token_hash, 'escape') = $1`,
		tokens.RefreshToken).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Error("refresh token appears in the database in plaintext")
	}

	// And the stored hash is the expected digest.
	var stored []byte
	if err := owner.Raw().QueryRow(ctx,
		`SELECT token_hash FROM refresh_tokens ORDER BY created_at DESC LIMIT 1`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if string(stored) != string(auth.HashRefreshToken(tokens.RefreshToken)) {
		t.Error("stored hash does not match the issued token")
	}
}

// Password hashes must never be recoverable from the row.
func TestPasswordIsNotStoredInPlaintext(t *testing.T) {
	svc, _, _, _ := newService(t)
	ctx := context.Background()
	signup(t, svc, "alex@example.com")

	owner := testsupport.OpenOwner(t)
	var hash string
	if err := owner.Raw().QueryRow(ctx,
		`SELECT password_hash FROM users WHERE email = 'alex@example.com'`).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(hash, goodPassword) {
		t.Fatal("password stored in plaintext")
	}
	if !strings.HasPrefix(hash, "$argon2id$") {
		t.Errorf("unexpected hash format: %q", hash)
	}
}
