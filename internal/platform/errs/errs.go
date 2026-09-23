// Package errs defines the application's typed error vocabulary.
//
// Handlers never translate a bare error into a status code by inspecting its
// text. Domain packages return an *Error carrying a Kind and a stable machine
// code; the HTTP layer maps Kind to a status and renders Code for clients that
// must branch on the failure (an offline outbox deciding whether to retry, or
// the app deciding whether to show the "0 sessions remaining" prompt).
package errs

import (
	"errors"
	"fmt"
	"net/http"
)

// Kind classifies a failure for transport mapping.
type Kind string

const (
	KindInvalid      Kind = "invalid"       // malformed or failed validation
	KindUnauthorized Kind = "unauthorized"  // missing or bad credentials
	KindForbidden    Kind = "forbidden"     // authenticated but not permitted
	KindNotFound     Kind = "not_found"     // no such resource in this tenant
	KindConflict     Kind = "conflict"      // state precondition violated
	KindUnprocessed  Kind = "unprocessable" // understood but rules forbid it
	KindRateLimited  Kind = "rate_limited"
	// KindInactive is a request the account's plan no longer allows: a lapsed
	// trial is read-only until it is renewed.
	KindInactive Kind = "inactive"
	KindInternal Kind = "internal"
)

// Stable machine codes. These are part of the client contract: the Expo app
// and the sync outbox branch on them, so they must not be renamed casually.
//
// #nosec G101 -- these are error identifiers returned to clients, not credentials.
const (
	CodeValidation          = "validation_failed"
	CodeInvalidCredentials  = "invalid_credentials"
	CodeTokenExpired        = "token_expired"
	CodeTokenReused         = "refresh_token_reused"
	CodeTenantMismatch      = "tenant_mismatch"
	CodeNotFound            = "not_found"
	CodeIdempotencyMismatch = "idempotency_key_reused"
	CodeInsufficientCredits = "insufficient_credits"
	CodeInvalidTransition   = "invalid_state_transition"
	CodeUnbalancedEntry     = "unbalanced_journal_entry"
	CodeImmutableEntry      = "journal_entry_immutable"
	CodeInvoiceNotPayable   = "invoice_not_payable"
	CodeOverpayment         = "overpayment"
	CodeSchedulingConflict  = "scheduling_conflict"
	CodeBufferViolation     = "buffer_violation"
	CodeInternal            = "internal_error"

	// Plans. The app shows an upgrade prompt for the first two and a
	// read-only banner for the third; the outbox holds its operations on the
	// third rather than parking them as failed.
	CodeFeatureNotInPlan     = "feature_not_in_plan"
	CodePlanLimitReached     = "plan_limit_reached"
	CodeSubscriptionInactive = "subscription_inactive"
	CodeMFARequired          = "mfa_required"
	CodeInvalidMFACode       = "invalid_mfa_code"
	CodeCurrencyLocked       = "currency_locked"
)

// Error is an application error with a transport-mappable Kind.
type Error struct {
	Kind    Kind
	Code    string
	Message string
	// Fields carries per-field validation detail, keyed by field name.
	Fields map[string]string
	// Meta carries structured context a client may need to act on, such as the
	// remaining credit balance that triggered an insufficient-credits refusal.
	Meta map[string]any
	err  error
}

func (e *Error) Error() string {
	if e.err != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.err)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Unwrap exposes the wrapped cause to errors.Is and errors.As.
func (e *Error) Unwrap() error { return e.err }

// WithMeta attaches structured context and returns the same error.
func (e *Error) WithMeta(k string, v any) *Error {
	if e.Meta == nil {
		e.Meta = map[string]any{}
	}
	e.Meta[k] = v
	return e
}

// WithField attaches a per-field validation message.
func (e *Error) WithField(field, msg string) *Error {
	if e.Fields == nil {
		e.Fields = map[string]string{}
	}
	e.Fields[field] = msg
	return e
}

func newf(kind Kind, code, format string, args ...any) *Error {
	return &Error{Kind: kind, Code: code, Message: fmt.Sprintf(format, args...)}
}

// Invalid reports malformed input.
func Invalid(code, format string, args ...any) *Error {
	return newf(KindInvalid, code, format, args...)
}

// Unauthorized reports missing or bad credentials.
func Unauthorized(code, format string, args ...any) *Error {
	return newf(KindUnauthorized, code, format, args...)
}

// Forbidden reports an authenticated but unpermitted request.
func Forbidden(code, format string, args ...any) *Error {
	return newf(KindForbidden, code, format, args...)
}

// NotFound reports an absent resource. Because every read is RLS-scoped, this
// is also what a cross-tenant probe receives: existence is not leaked.
func NotFound(format string, args ...any) *Error {
	return newf(KindNotFound, CodeNotFound, format, args...)
}

// Conflict reports a violated state precondition.
func Conflict(code, format string, args ...any) *Error {
	return newf(KindConflict, code, format, args...)
}

// Unprocessable reports a well-formed request that business rules refuse.
func Unprocessable(code, format string, args ...any) *Error {
	return newf(KindUnprocessed, code, format, args...)
}

// RateLimited reports a throttled caller.
func RateLimited(format string, args ...any) *Error {
	return newf(KindRateLimited, "rate_limited", format, args...)
}

// Inactive reports a write the account's plan no longer allows.
func Inactive(format string, args ...any) *Error {
	return newf(KindInactive, CodeSubscriptionInactive, format, args...)
}

// Internal wraps an unexpected failure. The cause is logged, never returned
// to the caller.
func Internal(err error, format string, args ...any) *Error {
	e := newf(KindInternal, CodeInternal, format, args...)
	e.err = err
	return e
}

// Wrap adds context while preserving an existing *Error's Kind and Code.
func Wrap(err error, format string, args ...any) error {
	if err == nil {
		return nil
	}
	var appErr *Error
	if errors.As(err, &appErr) {
		clone := *appErr
		clone.Message = fmt.Sprintf("%s: %s", fmt.Sprintf(format, args...), appErr.Message)
		clone.err = appErr.err
		return &clone
	}
	return fmt.Errorf("%s: %w", fmt.Sprintf(format, args...), err)
}

// KindOf extracts the Kind of an error, defaulting to KindInternal.
func KindOf(err error) Kind {
	var appErr *Error
	if errors.As(err, &appErr) {
		return appErr.Kind
	}
	return KindInternal
}

// CodeOf extracts the machine code of an error.
func CodeOf(err error) string {
	var appErr *Error
	if errors.As(err, &appErr) {
		return appErr.Code
	}
	return CodeInternal
}

// HTTPStatus maps an error to its response status.
func HTTPStatus(err error) int {
	switch KindOf(err) {
	case KindInvalid:
		return http.StatusBadRequest
	case KindUnauthorized:
		return http.StatusUnauthorized
	case KindForbidden:
		return http.StatusForbidden
	case KindNotFound:
		return http.StatusNotFound
	case KindConflict:
		return http.StatusConflict
	case KindUnprocessed:
		return http.StatusUnprocessableEntity
	case KindRateLimited:
		return http.StatusTooManyRequests
	case KindInactive:
		return http.StatusPaymentRequired
	default:
		return http.StatusInternalServerError
	}
}
