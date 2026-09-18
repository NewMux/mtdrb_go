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

// Claims is the access-token payload.
type Claims struct {
	jwt.RegisteredClaims
	TenantID ids.ID  `json:"tid"`
	Subject  Subject `json:"sub_type"`
	// ClientID is set only for portal tokens; it binds the token to a single
	// training client, which RLS then enforces via current_client_id().
	ClientID *ids.ID `json:"cid,omitempty"`
	Role     string  `json:"role,omitempty"`
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

// IssueUserAccess mints an access token for a trainer.
func (t *TokenIssuer) IssueUserAccess(tenantID, userID ids.ID, role string) (string, error) {
	return t.sign(Claims{
		RegisteredClaims: t.registered(userID.String()),
		TenantID:         tenantID,
		Subject:          SubjectUser,
		Role:             role,
	})
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
func (t *TokenIssuer) NewRefreshToken(family ids.ID) (RefreshToken, error) {
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
		ExpiresAt: t.clock.Now().Add(t.refreshTTL),
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
