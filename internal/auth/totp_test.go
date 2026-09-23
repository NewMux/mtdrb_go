package auth

import (
	"strings"
	"testing"
	"time"
)

// The RFC 6238 appendix B vectors, SHA-1 column, truncated to six digits.
// The secret is the ASCII "12345678901234567890".
func TestTOTPMatchesTheRFCVectors(t *testing.T) {
	secret := base32NoPad.EncodeToString([]byte("12345678901234567890"))
	vectors := map[int64]string{
		59:          "287082",
		1111111109:  "081804",
		1111111111:  "050471",
		1234567890:  "005924",
		2000000000:  "279037",
		20000000000: "353130",
	}
	for unix, want := range vectors {
		got, err := totpCode(secret, unix/30)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("t=%d: code %s, want %s", unix, got, want)
		}
	}
}

func TestVerifyTOTPAcceptsSkewAndRefusesReplay(t *testing.T) {
	secret, err := NewTOTPSecret()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_800_000_015, 0)
	step := now.Unix() / 30
	previous, _ := totpCode(secret, step-1)

	matched, ok := VerifyTOTP(secret, previous, now, 0)
	if !ok || matched != step-1 {
		t.Fatalf("a code from the previous step should pass: ok=%v step=%d", ok, matched)
	}
	if _, ok := VerifyTOTP(secret, previous, now, matched); ok {
		t.Fatal("a code already used must not pass again")
	}
	stale, _ := totpCode(secret, step-3)
	if _, ok := VerifyTOTP(secret, stale, now, 0); ok {
		t.Fatal("a code ninety seconds old must not pass")
	}
	if _, ok := VerifyTOTP(secret, "12345", now, 0); ok {
		t.Fatal("a short code must not pass")
	}
}

func TestRecoveryCodesAreReadableAndHashTolerantly(t *testing.T) {
	codes, err := NewRecoveryCodes()
	if err != nil {
		t.Fatal(err)
	}
	if len(codes) != RecoveryCodeCount {
		t.Fatalf("got %d codes", len(codes))
	}
	seen := map[string]bool{}
	for _, c := range codes {
		if len(c) != 11 || c[5] != '-' || strings.ContainsAny(c, "01OIL") {
			t.Fatalf("unreadable code %q", c)
		}
		if seen[c] {
			t.Fatalf("duplicate code %q", c)
		}
		seen[c] = true
	}
	typed := strings.ToLower(strings.ReplaceAll(codes[0], "-", " "))
	if string(HashRecoveryCode(typed)) != string(HashRecoveryCode(codes[0])) {
		t.Fatal("a code typed in lower case with a space should still match")
	}
}

func TestTOTPURINamesTheIssuer(t *testing.T) {
	uri := TOTPURI("JBSWY3DPEHPK3PXP", "sam@example.com")
	if !strings.HasPrefix(uri, "otpauth://totp/CoachPulse:sam@example.com?") ||
		!strings.Contains(uri, "issuer=CoachPulse") || !strings.Contains(uri, "secret=JBSWY3DPEHPK3PXP") {
		t.Fatalf("unexpected URI %s", uri)
	}
}
