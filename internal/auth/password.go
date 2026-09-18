package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"

	"github.com/NewMux/mtdrb_go/internal/platform/errs"
)

// Argon2Params are the cost parameters for password hashing.
//
// Each hash embeds the parameters it was produced with, so these can be raised
// later without invalidating existing passwords: an old hash still verifies
// against its own settings, and NeedsRehash reports when to upgrade it.
type Argon2Params struct {
	Memory      uint32 // KiB
	Iterations  uint32
	Parallelism uint8
	SaltLength  uint32
	KeyLength   uint32
}

// DefaultArgon2Params follows the OWASP argon2id recommendation of 19 MiB and
// two iterations.
func DefaultArgon2Params() Argon2Params {
	return Argon2Params{
		Memory:      19 * 1024,
		Iterations:  2,
		Parallelism: 1,
		SaltLength:  16,
		KeyLength:   32,
	}
}

// HashPassword derives an argon2id hash in the standard PHC string format.
func HashPassword(password string, p Argon2Params) (string, error) {
	if err := ValidatePasswordStrength(password); err != nil {
		return "", err
	}
	salt := make([]byte, p.SaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", errs.Internal(err, "generate password salt")
	}
	key := argon2.IDKey([]byte(password), salt, p.Iterations, p.Memory, p.Parallelism, p.KeyLength)
	return fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, p.Memory, p.Iterations, p.Parallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

// VerifyPassword reports whether password matches the encoded hash.
//
// Comparison is constant-time. A malformed stored hash is reported as a
// mismatch rather than an error, so a corrupted row cannot become an auth
// bypass or leak its shape through differing responses.
func VerifyPassword(password, encoded string) (bool, error) {
	p, salt, want, err := decodeHash(encoded)
	if err != nil {
		return false, err
	}
	got := argon2.IDKey([]byte(password), salt, p.Iterations, p.Memory, p.Parallelism, p.KeyLength)
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

// NeedsRehash reports whether a stored hash was produced with weaker
// parameters than the current policy, so it can be upgraded on next login.
func NeedsRehash(encoded string, p Argon2Params) bool {
	stored, _, _, err := decodeHash(encoded)
	if err != nil {
		return true
	}
	return stored.Memory < p.Memory ||
		stored.Iterations < p.Iterations ||
		stored.KeyLength < p.KeyLength
}

func decodeHash(encoded string) (p Argon2Params, salt, key []byte, err error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return p, nil, nil, errs.Internal(nil, "stored password hash is malformed")
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return p, nil, nil, errs.Internal(err, "stored password hash has an unreadable version")
	}
	if version != argon2.Version {
		return p, nil, nil, errs.Internal(nil, "stored password hash uses argon2 version %d", version)
	}
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.Memory, &p.Iterations, &p.Parallelism); err != nil {
		return p, nil, nil, errs.Internal(err, "stored password hash has unreadable parameters")
	}
	if salt, err = base64.RawStdEncoding.Strict().DecodeString(parts[4]); err != nil {
		return p, nil, nil, errs.Internal(err, "stored password hash has an unreadable salt")
	}
	if key, err = base64.RawStdEncoding.Strict().DecodeString(parts[5]); err != nil {
		return p, nil, nil, errs.Internal(err, "stored password hash has an unreadable key")
	}
	p.SaltLength = uint32(len(salt))
	p.KeyLength = uint32(len(key))
	return p, salt, key, nil
}

// Password policy bounds. The upper bound exists because argon2 hashes the
// full input: without it, a multi-megabyte password is a free denial of
// service against the login endpoint.
const (
	MinPasswordLength = 12
	MaxPasswordLength = 1024
)

// ValidatePasswordStrength enforces the length policy.
//
// Length is the requirement that actually correlates with strength; composition
// rules mostly push people toward "Password1!" and a sticky note.
func ValidatePasswordStrength(password string) error {
	switch {
	case len(password) < MinPasswordLength:
		return errs.Invalid(errs.CodeValidation,
			"password must be at least %d characters", MinPasswordLength).
			WithField("password", fmt.Sprintf("must be at least %d characters", MinPasswordLength))
	case len(password) > MaxPasswordLength:
		return errs.Invalid(errs.CodeValidation,
			"password must be at most %d characters", MaxPasswordLength).
			WithField("password", fmt.Sprintf("must be at most %d characters", MaxPasswordLength))
	}
	return nil
}
