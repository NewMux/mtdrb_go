package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // RFC 6238 is HMAC-SHA1; every authenticator app computes exactly this.
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// Time-based one-time passwords, RFC 6238, in the only profile authenticator
// apps agree on: HMAC-SHA1, 30-second steps, six digits.
//
// Implemented here rather than imported because it is forty lines and the
// whole of our second factor. A dependency would be more code to audit than
// the thing it replaces.

const (
	totpStep   = 30 * time.Second
	totpDigits = 6
	// totpSkew accepts the step before and after the current one: a phone
	// clock a few seconds out, or a code typed as it rolled over.
	totpSkew = 1
)

var base32NoPad = base32.StdEncoding.WithPadding(base32.NoPadding)

// NewTOTPSecret returns a fresh 160-bit secret, base32 as apps expect it.
func NewTOTPSecret() (string, error) {
	buf := make([]byte, 20)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base32NoPad.EncodeToString(buf), nil
}

// TOTPURI is what an authenticator app scans: the account label, the secret,
// and who issued it, so the app shows "CoachPulse (sam@…)" rather than a
// bare code nobody can place.
func TOTPURI(secret, account string) string {
	label := url.PathEscape("CoachPulse:" + account)
	q := url.Values{}
	q.Set("secret", secret)
	q.Set("issuer", "CoachPulse")
	q.Set("algorithm", "SHA1")
	q.Set("digits", fmt.Sprint(totpDigits))
	q.Set("period", fmt.Sprint(int(totpStep.Seconds())))
	return "otpauth://totp/" + label + "?" + q.Encode()
}

// totpCode computes the code for one step.
func totpCode(secret string, step int64) (string, error) {
	key, err := base32NoPad.DecodeString(strings.ToUpper(strings.TrimRight(secret, "=")))
	if err != nil {
		return "", err
	}
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(step)) //nolint:gosec // steps are positive
	mac := hmac.New(sha1.New, key)
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	value := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", value%1_000_000), nil
}

// VerifyTOTP checks a code and returns the step it matched.
//
// A code matching a step at or before lastStep is refused even though it is
// arithmetically right: it has been used, and accepting it again would let
// anyone who saw it over a shoulder sign in within the window.
func VerifyTOTP(secret, code string, now time.Time, lastStep int64) (int64, bool) {
	code = strings.ReplaceAll(strings.TrimSpace(code), " ", "")
	if len(code) != totpDigits {
		return 0, false
	}
	current := now.Unix() / int64(totpStep.Seconds())
	for delta := -totpSkew; delta <= totpSkew; delta++ {
		step := current + int64(delta)
		if step <= lastStep {
			continue
		}
		want, err := totpCode(secret, step)
		if err != nil {
			return 0, false
		}
		if subtle.ConstantTimeCompare([]byte(want), []byte(code)) == 1 {
			return step, true
		}
	}
	return 0, false
}

// recoveryAlphabet has no 0/O or 1/I/L: these are read off paper.
const recoveryAlphabet = "23456789ABCDEFGHJKMNPQRSTUVWXYZ"

// RecoveryCodeCount is how many single-use codes a trainer is given.
const RecoveryCodeCount = 10

// NewRecoveryCodes returns fresh codes in the form XXXXX-XXXXX.
func NewRecoveryCodes() ([]string, error) {
	codes := make([]string, RecoveryCodeCount)
	buf := make([]byte, 10)
	for i := range codes {
		if _, err := rand.Read(buf); err != nil {
			return nil, err
		}
		var b strings.Builder
		for j, c := range buf {
			if j == 5 {
				b.WriteByte('-')
			}
			b.WriteByte(recoveryAlphabet[int(c)%len(recoveryAlphabet)])
		}
		codes[i] = b.String()
	}
	return codes, nil
}

// HashRecoveryCode derives the stored form. SHA-256 for the reason refresh
// tokens use it: the codes are random, not chosen, so there is nothing for a
// slow hash to protect.
func HashRecoveryCode(code string) []byte {
	normal := strings.ToUpper(strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(code), "-", ""), " ", ""))
	sum := sha256.Sum256([]byte(normal))
	return sum[:]
}

// TOTPCodeAt is the code an authenticator app shows at a moment, for tests
// and tools that stand in for the app.
func TOTPCodeAt(secret string, at time.Time) (string, error) {
	return totpCode(secret, at.Unix()/int64(totpStep.Seconds()))
}
