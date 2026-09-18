package auth

import (
	"strings"
	"testing"
)

// testParams keep argon2 cheap so the suite stays fast; production cost comes
// from DefaultArgon2Params.
func testParams() Argon2Params {
	p := DefaultArgon2Params()
	p.Memory = 1024
	p.Iterations = 1
	return p
}

func TestHashAndVerify(t *testing.T) {
	const pw = "correct-horse-battery-staple"
	hash, err := HashPassword(pw, testParams())
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if strings.Contains(hash, pw) {
		t.Fatal("hash contains the plaintext password")
	}
	if !strings.HasPrefix(hash, "$argon2id$") {
		t.Errorf("unexpected hash format: %q", hash)
	}

	ok, err := VerifyPassword(pw, hash)
	if err != nil || !ok {
		t.Fatalf("correct password did not verify: ok=%v err=%v", ok, err)
	}

	ok, err = VerifyPassword("wrong-horse-battery-staple", hash)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if ok {
		t.Fatal("wrong password verified")
	}
}

func TestHashesAreSaltedUniquely(t *testing.T) {
	const pw = "correct-horse-battery-staple"
	a, err := HashPassword(pw, testParams())
	if err != nil {
		t.Fatal(err)
	}
	b, err := HashPassword(pw, testParams())
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("identical passwords produced identical hashes; salt is not random")
	}
}

func TestVerifyRejectsMalformedHash(t *testing.T) {
	cases := map[string]string{
		"empty":           "",
		"not phc":         "plaintext",
		"wrong algorithm": "$argon2i$v=19$m=1024,t=1,p=1$c2FsdA$aGFzaA",
		"truncated":       "$argon2id$v=19$m=1024,t=1,p=1",
		"bad params":      "$argon2id$v=19$m=x,t=y,p=z$c2FsdA$aGFzaA",
		"bad salt base64": "$argon2id$v=19$m=1024,t=1,p=1$!!!!$aGFzaA",
		"bad key base64":  "$argon2id$v=19$m=1024,t=1,p=1$c2FsdA$!!!!",
		"unknown version": "$argon2id$v=13$m=1024,t=1,p=1$c2FsdA$aGFzaA",
	}
	for name, encoded := range cases {
		ok, err := VerifyPassword("anything", encoded)
		if ok {
			t.Errorf("%s: malformed hash verified as correct", name)
		}
		if err == nil {
			t.Errorf("%s: expected an error for a malformed hash", name)
		}
	}
}

func TestPasswordPolicy(t *testing.T) {
	if err := ValidatePasswordStrength("short"); err == nil {
		t.Error("expected short password to be rejected")
	}
	if err := ValidatePasswordStrength(strings.Repeat("a", MaxPasswordLength+1)); err == nil {
		t.Error("expected oversized password to be rejected")
	}
	if err := ValidatePasswordStrength(strings.Repeat("a", MinPasswordLength)); err != nil {
		t.Errorf("minimum-length password rejected: %v", err)
	}
}

func TestHashRejectsWeakPassword(t *testing.T) {
	if _, err := HashPassword("tooshort", testParams()); err == nil {
		t.Fatal("expected hashing to enforce the password policy")
	}
}

func TestNeedsRehash(t *testing.T) {
	weak := testParams()
	hash, err := HashPassword("correct-horse-battery-staple", weak)
	if err != nil {
		t.Fatal(err)
	}
	if NeedsRehash(hash, weak) {
		t.Error("hash at current parameters should not need a rehash")
	}
	stronger := weak
	stronger.Memory = weak.Memory * 4
	if !NeedsRehash(hash, stronger) {
		t.Error("hash below current parameters should need a rehash")
	}
	if !NeedsRehash("garbage", weak) {
		t.Error("unparseable hash should need a rehash")
	}
}

func TestVerifyIsCaseSensitive(t *testing.T) {
	hash, err := HashPassword("CorrectHorseBattery", testParams())
	if err != nil {
		t.Fatal(err)
	}
	ok, _ := VerifyPassword("correcthorsebattery", hash)
	if ok {
		t.Error("verification is not case sensitive")
	}
}
