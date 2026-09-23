package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/NewMux/mtdrb_go/internal/platform/clock"
	"github.com/NewMux/mtdrb_go/internal/platform/errs"
	"github.com/NewMux/mtdrb_go/internal/platform/ids"
	"github.com/NewMux/mtdrb_go/internal/tenancy"
)

// Subject is the kind of principal a token authenticates. It aliases
// tenancy.Kind so the token payload and the request principal cannot drift.
type Subject = tenancy.Kind

const (
	// SubjectUser is a trainer or staff member of a tenant.
	SubjectUser = tenancy.KindUser
	// SubjectClient is a training client using the companion portal.
	SubjectClient = tenancy.KindClient
)

const issuer = "coachpulse"

// mfaIssuer signs the short-lived proof that a password was right, which a
// second factor then exchanges for a session. A different issuer, so that
// ParseAccess refuses it: half a login must not open the API.
const mfaIssuer = "coachpulse-mfa"

// mfaChallengeTTL is how long a trainer has to type the code from their phone.
const mfaChallengeTTL = 5 * time.Minute

// Claims is the access-token payload.
type Claims struct {
	jwt.RegisteredClaims
	TenantID ids.ID  `json:"tid"`
	Subject  Subject `json:"sub_type"`
	// ClientID is set only for portal tokens; it binds the token to a single
	// training client, which RLS then enforces via current_client_id().
	ClientID *ids.ID `json:"cid,omitempty"`
	Role     string  `json:"role,omitempty"`
	// FamilyID is the refresh-token family this access token was minted
	// from — which device is asking.
	FamilyID *ids.ID `json:"fam,omitempty"`
	// The tenant's plan when the token was minted, so route guards need no
	// query. A change applies at the next refresh.
	Plan        string           `json:"plan,omitempty"`
	PlanStatus  string           `json:"pst,omitempty"`
	TrialEndsAt *jwt.NumericDate `json:"tre,omitempty"`
}

// PlanClaims is the plan state an access token carries.
type PlanClaims struct {
	Plan        string
	Status      string
	TrialEndsAt *time.Time
}

// TokenIssuer mints and verifies access tokens.
type TokenIssuer struct {
	key        []byte
	accessTTL  time.Duration
	refreshTTL time.Duration
	clock      clock.Clock
}

// NewTokenIssuer builds an issuer. The key must be at least 32 bytes; config
// enforces that at startup.
func NewTokenIssuer(key []byte, accessTTL, refreshTTL time.Duration, c clock.Clock) *TokenIssuer {
	if c == nil {
		c = clock.System{}
	}
	return &TokenIssuer{key: key, accessTTL: accessTTL, refreshTTL: refreshTTL, clock: c}
}

// AccessTTL reports the access-token lifetime, for the login response.
func (t *TokenIssuer) AccessTTL() time.Duration { return t.accessTTL }

// RefreshTTL reports the refresh-token lifetime.
func (t *TokenIssuer) RefreshTTL() time.Duration { return t.refreshTTL }

// IssueUserAccess mints an access token for a trainer on one device.
func (t *TokenIssuer) IssueUserAccess(tenantID, userID ids.ID, role string, family ids.ID, plan PlanClaims) (string, error) {
	c := Claims{
		RegisteredClaims: t.registered(userID.String()),
		TenantID:         tenantID,
		Subject:          SubjectUser,
		Role:             role,
		Plan:             plan.Plan,
		PlanStatus:       plan.Status,
	}
	if family != ids.Nil {
		c.FamilyID = &family
	}
	if plan.TrialEndsAt != nil {
		c.TrialEndsAt = jwt.NewNumericDate(*plan.TrialEndsAt)
	}
	return t.sign(c)
}

// IssueMFAChallenge mints the token a login with a correct password returns
// when the account has a second factor.
func (t *TokenIssuer) IssueMFAChallenge(tenantID, userID ids.ID) (string, error) {
	now := t.clock.Now()
	return t.sign(Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    mfaIssuer,
			Subject:   userID.String(),
			ID:        ids.New().String(),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(mfaChallengeTTL)),
		},
		TenantID: tenantID,
		Subject:  SubjectUser,
	})
}

// ParseMFAChallenge verifies a challenge and returns whose it is.
func (t *TokenIssuer) ParseMFAChallenge(raw string) (tenantID, userID ids.ID, err error) {
	invalid := errs.Unauthorized(errs.CodeInvalidCredentials, "the sign-in has expired; enter your password again")
	claims := &Claims{}
	_, err = jwt.ParseWithClaims(raw, claims, func(tok *jwt.Token) (any, error) {
		return t.key, nil
	},
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(mfaIssuer),
		jwt.WithTimeFunc(t.clock.Now),
	)
	if err != nil || claims.TenantID == ids.Nil {
		return ids.Nil, ids.Nil, invalid
	}
	userID, err = ids.Parse(claims.RegisteredClaims.Subject)
	if err != nil {
		return ids.Nil, ids.Nil, invalid
	}
	return claims.TenantID, userID, nil
}

// IssueClientAccess mints a portal access token scoped to one training client.
func (t *TokenIssuer) IssueClientAccess(tenantID, clientID ids.ID) (string, error) {
	cid := clientID
	return t.sign(Claims{
		RegisteredClaims: t.registered(clientID.String()),
		TenantID:         tenantID,
		Subject:          SubjectClient,
		ClientID:         &cid,
	})
}

func (t *TokenIssuer) registered(subject string) jwt.RegisteredClaims {
	now := t.clock.Now()
	return jwt.RegisteredClaims{
		Issuer:    issuer,
		Subject:   subject,
		ID:        ids.New().String(),
		IssuedAt:  jwt.NewNumericDate(now),
		NotBefore: jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(t.accessTTL)),
	}
}

func (t *TokenIssuer) sign(c Claims) (string, error) {
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, c)
	s, err := tok.SignedString(t.key)
	if err != nil {
		return "", errs.Internal(err, "sign access token")
	}
	return s, nil
}

// ParseAccess verifies a token's signature and expiry and returns its claims.
//
// The signing method is pinned to HS256. Without that pin, a token with
// "alg":"none" — or an RS256 token whose "public key" is our HMAC secret —
// would be accepted as valid.
func (t *TokenIssuer) ParseAccess(raw string) (*Claims, error) {
	claims := &Claims{}
	_, err := jwt.ParseWithClaims(raw, claims, func(tok *jwt.Token) (any, error) {
		if _, ok := tok.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method %v", tok.Header["alg"])
		}
		return t.key, nil
	},
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(issuer),
		jwt.WithTimeFunc(t.clock.Now),
	)
	if err != nil {
		if isExpiry(err) {
			return nil, errs.Unauthorized(errs.CodeTokenExpired, "access token has expired")
		}
		return nil, errs.Unauthorized(errs.CodeInvalidCredentials, "access token is not valid")
	}

	if claims.TenantID == ids.Nil {
		return nil, errs.Unauthorized(errs.CodeInvalidCredentials, "access token carries no tenant")
	}
	switch claims.Subject {
	case SubjectUser:
		if claims.ClientID != nil {
			return nil, errs.Unauthorized(errs.CodeInvalidCredentials, "trainer token must not carry a client id")
		}
	case SubjectClient:
		if claims.ClientID == nil || *claims.ClientID == ids.Nil {
			return nil, errs.Unauthorized(errs.CodeInvalidCredentials, "portal token carries no client id")
		}
	default:
		return nil, errs.Unauthorized(errs.CodeInvalidCredentials, "access token has an unknown subject type")
	}
	return claims, nil
}

// isExpiry distinguishes a token that was valid but has aged out from one that
// is malformed or forged. The client retries the first by refreshing and must
// re-authenticate for the second, so the two get different machine codes.
func isExpiry(err error) bool {
	return errors.Is(err, jwt.ErrTokenExpired) || errors.Is(err, jwt.ErrTokenNotValidYet)
}

// RefreshToken is a freshly minted opaque refresh credential.
//
// The plaintext is returned to the caller exactly once. Only Hash is stored,
// so a database read yields nothing usable.
type RefreshToken struct {
	Plaintext string
	Hash      []byte
	FamilyID  ids.ID
	ExpiresAt time.Time
}

const refreshTokenBytes = 32

// NewRefreshToken mints a refresh token in the given family. Passing ids.Nil
// starts a new family, which is what login does; rotation passes the existing
// family so theft of any generation can revoke the whole chain.
//
// ttl is the tenant's own session timeout; zero means the issuer's default.
func (t *TokenIssuer) NewRefreshToken(family ids.ID, ttl time.Duration) (RefreshToken, error) {
	if ttl <= 0 {
		ttl = t.refreshTTL
	}
	buf := make([]byte, refreshTokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return RefreshToken{}, errs.Internal(err, "generate refresh token")
	}
	if family == ids.Nil {
		family = ids.New()
	}
	plaintext := base64.RawURLEncoding.EncodeToString(buf)
	return RefreshToken{
		Plaintext: plaintext,
		Hash:      HashRefreshToken(plaintext),
		FamilyID:  family,
		ExpiresAt: t.clock.Now().Add(ttl),
	}, nil
}

// HashRefreshToken derives the stored form of a refresh token.
//
// A plain SHA-256 is correct here and argon2 would be wrong: the token is 32
// bytes of cryptographic randomness, not a human-chosen password, so there is
// no dictionary to slow down — only a lookup to make constant-cost.
func HashRefreshToken(plaintext string) []byte {
	sum := sha256.Sum256([]byte(plaintext))
	return sum[:]
}
