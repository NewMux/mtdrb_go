package errs

import (
	"errors"
	"fmt"
	"net/http"
	"testing"
)

func TestHTTPStatusMapping(t *testing.T) {
	cases := []struct {
		err  error
		want int
	}{
		{Invalid(CodeValidation, "bad"), http.StatusBadRequest},
		{Unauthorized(CodeInvalidCredentials, "nope"), http.StatusUnauthorized},
		{Forbidden(CodeTenantMismatch, "nope"), http.StatusForbidden},
		{NotFound("client"), http.StatusNotFound},
		{Conflict(CodeInvalidTransition, "no"), http.StatusConflict},
		{Unprocessable(CodeInsufficientCredits, "no"), http.StatusUnprocessableEntity},
		{RateLimited("slow down"), http.StatusTooManyRequests},
		{Internal(errors.New("boom"), "oops"), http.StatusInternalServerError},
		{errors.New("bare"), http.StatusInternalServerError},
	}
	for _, tc := range cases {
		if got := HTTPStatus(tc.err); got != tc.want {
			t.Errorf("%v: got %d want %d", tc.err, got, tc.want)
		}
	}
}

func TestWrapPreservesKindAndCode(t *testing.T) {
	base := Unprocessable(CodeInsufficientCredits, "balance is 0")
	wrapped := Wrap(base, "completing session")
	if KindOf(wrapped) != KindUnprocessed {
		t.Errorf("kind lost: %v", KindOf(wrapped))
	}
	if CodeOf(wrapped) != CodeInsufficientCredits {
		t.Errorf("code lost: %v", CodeOf(wrapped))
	}
}

func TestWrapDoesNotMutateOriginal(t *testing.T) {
	base := Conflict(CodeInvalidTransition, "original")
	_ = Wrap(base, "context")
	if base.Message != "original" {
		t.Errorf("original mutated: %q", base.Message)
	}
}

func TestWrapNilIsNil(t *testing.T) {
	if Wrap(nil, "ctx") != nil {
		t.Error("expected nil")
	}
}

func TestUnwrapReachesCause(t *testing.T) {
	cause := errors.New("db down")
	err := Internal(cause, "query failed")
	if !errors.Is(err, cause) {
		t.Error("cause not reachable via errors.Is")
	}
}

func TestWrapBareErrorStaysWrappable(t *testing.T) {
	cause := errors.New("bare")
	wrapped := Wrap(cause, "ctx")
	if !errors.Is(wrapped, cause) {
		t.Error("bare cause not reachable")
	}
}

func TestMetaAndFields(t *testing.T) {
	err := Unprocessable(CodeInsufficientCredits, "no credits").
		WithMeta("remaining", 0).
		WithField("session_id", "required")
	if err.Meta["remaining"] != 0 {
		t.Error("meta not set")
	}
	if err.Fields["session_id"] != "required" {
		t.Error("field not set")
	}
}

func TestErrorStringIncludesCause(t *testing.T) {
	err := Internal(errors.New("boom"), "failed")
	if got := fmt.Sprint(err); got != "internal_error: failed: boom" {
		t.Errorf("got %q", got)
	}
}
