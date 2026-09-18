package media

import (
	"testing"
)

func TestSensitiveKindsAreClassifiedByDefault(t *testing.T) {
	// These three can expose a body, a bank account number, or a person's
	// handwriting. None should depend on an uploader remembering to flag them.
	for _, k := range []Kind{KindProgressPhoto, KindReceipt, KindSignature} {
		if got := sensitivityFor(k); got != Sensitive {
			t.Errorf("%s classified as %q, want sensitive", k, got)
		}
	}
	for _, k := range []Kind{KindExerciseDemo, KindOther} {
		if got := sensitivityFor(k); got != Standard {
			t.Errorf("%s classified as %q, want standard", k, got)
		}
	}
}

func TestNormalizeContentType(t *testing.T) {
	cases := map[string]string{
		"image/jpeg":                  "image/jpeg",
		"IMAGE/JPEG":                  "image/jpeg",
		"  image/png  ":               "image/png",
		"image/jpeg; charset=binary":  "image/jpeg",
		"application/pdf;version=1.7": "application/pdf",
		"garbage":                     "garbage",
	}
	for in, want := range cases {
		if got := normalizeContentType(in); got != want {
			t.Errorf("normalizeContentType(%q) = %q, want %q", in, got, want)
		}
	}
}

// The allowlist must never permit a type a browser would execute. An HTML or
// SVG-script file served from the bucket origin is a stored XSS.
func TestAllowlistExcludesExecutableTypes(t *testing.T) {
	forbidden := []string{
		"text/html", "application/javascript", "text/javascript",
		"application/xhtml+xml", "text/xml", "application/x-httpd-php",
	}
	for kind, types := range allowedTypes {
		for _, bad := range forbidden {
			if _, present := types[bad]; present {
				t.Errorf("%s permits %s, which a browser may execute", kind, bad)
			}
		}
	}
}

func TestEveryAllowedTypeHasAnExtension(t *testing.T) {
	for kind, types := range allowedTypes {
		for ct, ext := range types {
			if ext == "" || ext[0] != '.' {
				t.Errorf("%s/%s: extension %q is not a dot-prefixed suffix", kind, ct, ext)
			}
		}
	}
}

func TestEveryKindHasLimitsAndTypes(t *testing.T) {
	kinds := []Kind{
		KindProgressPhoto, KindReceipt, KindSignature,
		KindFormCheckVideo, KindExerciseDemo, KindOther,
	}
	for _, k := range kinds {
		if _, ok := maxBytes[k]; !ok {
			t.Errorf("%s has no size limit; an unbounded upload would be accepted", k)
		}
		if types, ok := allowedTypes[k]; !ok || len(types) == 0 {
			t.Errorf("%s has no permitted content types", k)
		}
	}
}

// A signature is a small line drawing; allowing a 200 MiB one would be a
// free way to fill the bucket.
func TestSignatureLimitIsTighterThanVideo(t *testing.T) {
	if maxBytes[KindSignature] >= maxBytes[KindFormCheckVideo] {
		t.Error("signature limit should be far below the video limit")
	}
}
