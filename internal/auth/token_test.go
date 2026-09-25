package auth

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/NewMux/mtdrb_go/internal/platform/clock"
	"github.com/NewMux/mtdrb_go/internal/platform/errs"
	"github.com/NewMux/mtdrb_go/internal/platform/ids"
)

var testKey = []byte(strings.Repeat("k", 32))

func newIssuer(t *testing.T) (*TokenIssuer, *clock.Fixed) {
	t.Helper()
	c := &clock.Fixed{T: time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)}
	return NewTokenIssuer(testKey, 15*time.Minute, 30*24*time.Hour, c), c
}

func TestUserAccessRoundTrip(t *testing.T) {
	iss, _ := newIssuer(t)
	tenant, user := ids.New(), ids.New()

	raw, err := iss.IssueUserAccess(tenant, user, "owner", ids.Nil, PlanClaims{})
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	claims, err := iss.ParseAccess(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if claims.TenantID != tenant {
		t.Errorf("tenant = %v, want %v", claims.TenantID, tenant)
	}
	if claims.Subject != SubjectUser {
		t.Errorf("subject = %v", claims.Subject)
	}
	if claims.ClientID != nil {
		t.Error("trainer token must not carry a client id")
	}
	if claims.RegisteredClaims.Subject != user.String() {
		t.Errorf("sub = %q", claims.RegisteredClaims.Subject)
	}
}

func TestClientAccessCarriesClientID(t *testing.T) {
	iss, _ := newIssuer(t)
	tenant, client := ids.New(), ids.New()

	raw, err := iss.IssueClientAccess(tenant, client)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	claims, err := iss.ParseAccess(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if claims.Subject != SubjectClient {
		t.Errorf("subject = %v, want client", claims.Subject)
	}
	if claims.ClientID == nil || *claims.ClientID != client {
		t.Errorf("client id = %v, want %v", claims.ClientID, client)
	}
}

func TestExpiredTokenReportsExpiryCode(t *testing.T) {
	iss, c := newIssuer(t)
	raw, err := iss.IssueUserAccess(ids.New(), ids.New(), "owner", ids.Nil, PlanClaims{})
	if err != nil {
		t.Fatal(err)
	}
	c.Advance(16 * time.Minute)

	_, err = iss.ParseAccess(raw)
	if err == nil {
		t.Fatal("expected expired token to be rejected")
	}
	if errs.CodeOf(err) != errs.CodeTokenExpired {
		t.Errorf("code = %q, want %q", errs.CodeOf(err), errs.CodeTokenExpired)
	}
	if errs.KindOf(err) != errs.KindUnauthorized {
		t.Errorf("kind = %q", errs.KindOf(err))
	}
}

func TestTokenSignedWithAnotherKeyIsRejected(t *testing.T) {
	iss, _ := newIssuer(t)
	other := NewTokenIssuer([]byte(strings.Repeat("x", 32)), 15*time.Minute, time.Hour, clock.System{})

	raw, err := other.IssueUserAccess(ids.New(), ids.New(), "owner", ids.Nil, PlanClaims{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := iss.ParseAccess(raw); err == nil {
		t.Fatal("token signed with a foreign key was accepted")
	}
}

// A token declaring "alg":"none" must never be accepted. Without pinning the
// signing method, this is the classic JWT authentication bypass.
func TestAlgNoneIsRejected(t *testing.T) {
	iss, _ := newIssuer(t)
	tok := jwt.NewWithClaims(jwt.SigningMethodNone, Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    issuer,
			Subject:   ids.New().String(),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
		TenantID: ids.New(),
		Subject:  SubjectUser,
	})
	raw, err := tok.SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("sign none: %v", err)
	}
	if _, err := iss.ParseAccess(raw); err == nil {
		t.Fatal("alg=none token was accepted")
	}
}

func TestTamperedPayloadIsRejected(t *testing.T) {
	iss, _ := newIssuer(t)
	victim, attacker := ids.New(), ids.New()
	raw, err := iss.IssueUserAccess(victim, ids.New(), "owner", ids.Nil, PlanClaims{})
	if err != nil {
		t.Fatal(err)
	}

	// Swap the tenant claim while leaving the original signature in place.
	parts := strings.Split(raw, ".")
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(payload, &body); err != nil {
		t.Fatal(err)
	}
	body["tid"] = attacker.String()
	repacked, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	parts[1] = base64.RawURLEncoding.EncodeToString(repacked)

	if _, err := iss.ParseAccess(strings.Join(parts, ".")); err == nil {
		t.Fatal("tampered tenant claim was accepted")
	}
}

func TestMalformedTokensRejected(t *testing.T) {
	iss, _ := newIssuer(t)
	for _, raw := range []string{"", "not.a.token", "a.b", strings.Repeat("x", 100)} {
		if _, err := iss.ParseAccess(raw); err == nil {
			t.Errorf("malformed token %q was accepted", raw)
		}
	}
}

func TestRefreshTokenIsRandomAndHashed(t *testing.T) {
	iss, _ := newIssuer(t)

	a, err := iss.NewRefreshToken(ids.Nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	b, err := iss.NewRefreshToken(ids.Nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if a.Plaintext == b.Plaintext {
		t.Fatal("refresh tokens are not random")
	}
	if a.FamilyID == b.FamilyID {
		t.Error("independent logins should start distinct families")
	}
	if len(a.Hash) != 32 {
		t.Errorf("hash length = %d, want 32", len(a.Hash))
	}
	if strings.Contains(string(a.Hash), a.Plaintext) {
		t.Error("stored hash contains the plaintext token")
	}
	if got := HashRefreshToken(a.Plaintext); string(got) != string(a.Hash) {
		t.Error("HashRefreshToken is not deterministic")
	}
}

func TestRefreshTokenKeepsFamilyOnRotation(t *testing.T) {
	iss, _ := newIssuer(t)
	first, err := iss.NewRefreshToken(ids.Nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	rotated, err := iss.NewRefreshToken(first.FamilyID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if rotated.FamilyID != first.FamilyID {
		t.Error("rotation must preserve the token family so theft can revoke the chain")
	}
	if rotated.Plaintext == first.Plaintext {
		t.Error("rotation must mint a new secret")
	}
}

func TestRefreshTokenExpiryUsesInjectedClock(t *testing.T) {
	iss, c := newIssuer(t)
	tok, err := iss.NewRefreshToken(ids.Nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := c.Now().Add(30 * 24 * time.Hour)
	if !tok.ExpiresAt.Equal(want) {
		t.Errorf("expires at %v, want %v", tok.ExpiresAt, want)
	}
}

func TestAnMFAChallengeIsNotAnAccessToken(t *testing.T) {
	iss, _ := newIssuer(t)
	tenant, user := ids.New(), ids.New()
	challenge, err := iss.IssueMFAChallenge(tenant, user)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := iss.ParseAccess(challenge); err == nil {
		t.Fatal("half a sign-in must not open the API")
	}
	gotTenant, gotUser, err := iss.ParseMFAChallenge(challenge)
	if err != nil || gotTenant != tenant || gotUser != user {
		t.Fatalf("challenge round trip: %v %v %v", gotTenant, gotUser, err)
	}
	access, err := iss.IssueUserAccess(tenant, user, "owner", ids.Nil, PlanClaims{})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := iss.ParseMFAChallenge(access); err == nil {
		t.Fatal("an access token must not pass as a challenge")
	}
}

func TestAccessTokensCarryTheDeviceAndPlan(t *testing.T) {
	iss, _ := newIssuer(t)
	family := ids.New()
	ends := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	raw, err := iss.IssueUserAccess(ids.New(), ids.New(), "owner", family,
		PlanClaims{Plan: "trial", Status: "active", TrialEndsAt: &ends})
	if err != nil {
		t.Fatal(err)
	}
	claims, err := iss.ParseAccess(raw)
	if err != nil {
		t.Fatal(err)
	}
	if claims.FamilyID == nil || *claims.FamilyID != family || claims.Plan != "trial" ||
		claims.TrialEndsAt == nil || !claims.TrialEndsAt.Equal(ends) {
		t.Fatalf("claims lost the device or plan: %+v", claims)
	}
}
