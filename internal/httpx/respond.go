// Package httpx holds the HTTP transport concerns: response rendering, request
// decoding and the middleware chain.
package httpx

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/NewMux/mtdrb_go/internal/platform/errs"
	"github.com/NewMux/mtdrb_go/internal/platform/logger"
)

// ErrorBody is the wire format for a failed request.
//
// Clients branch on Code, never on Message: the message is for humans and may
// be reworded, while the code is part of the contract.
type ErrorBody struct {
	Code      string            `json:"code"`
	Message   string            `json:"message"`
	Fields    map[string]string `json:"fields,omitempty"`
	Meta      map[string]any    `json:"meta,omitempty"`
	RequestID string            `json:"request_id,omitempty"`
}

type errorEnvelope struct {
	Error ErrorBody `json:"error"`
}

// JSON writes a successful response.
func JSON(w http.ResponseWriter, r *http.Request, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	// Responses routinely carry client medical notes, payment instructions and
	// balances. None of it belongs in a shared cache or a browser's disk cache.
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)

	if body == nil || status == http.StatusNoContent {
		return
	}
	if err := json.NewEncoder(w).Encode(body); err != nil {
		// The status line is already sent, so this can only be logged.
		logger.From(r.Context()).ErrorContext(r.Context(), "encoding response failed", slog.Any("error", err))
	}
}

// NoContent writes an empty successful response.
func NoContent(w http.ResponseWriter, r *http.Request) { JSON(w, r, http.StatusNoContent, nil) }

// Error writes a failed response derived from a typed application error.
//
// Internal failures are logged with their cause and returned to the caller as
// a generic message: a database error must not leak schema details, and a
// cross-tenant probe must not be distinguishable from a genuine 404.
func Error(w http.ResponseWriter, r *http.Request, err error) {
	ctx := r.Context()
	status := errs.HTTPStatus(err)
	reqID := RequestIDFrom(ctx)

	body := ErrorBody{Code: errs.CodeOf(err), RequestID: reqID}

	var appErr *errs.Error
	if errors.As(err, &appErr) && appErr.Kind != errs.KindInternal {
		body.Message = appErr.Message
		body.Fields = appErr.Fields
		body.Meta = appErr.Meta
	} else {
		body.Message = "an unexpected error occurred"
		logger.From(ctx).ErrorContext(ctx, "request failed",
			slog.String("error", err.Error()),
			slog.String("path", r.URL.Path),
			slog.String("method", r.Method),
			slog.String("request_id", reqID),
		)
	}

	JSON(w, r, status, errorEnvelope{Error: body})
}

// maxBodyBytes caps request bodies. Receipt and progress-photo uploads go
// through presigned storage URLs rather than this API, so no JSON endpoint has
// a legitimate reason to be large.
const maxBodyBytes = 1 << 20 // 1 MiB

// Decode reads and validates a JSON request body.
//
// Unknown fields are rejected: silently ignoring a misspelled "amout" would
// post a zero-value amount to the ledger rather than failing loudly.
func Decode(w http.ResponseWriter, r *http.Request, dst any) error {
	if ct := r.Header.Get("Content-Type"); ct != "" && !isJSONContentType(ct) {
		return errs.Invalid(errs.CodeValidation, "Content-Type must be application/json")
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	if err := dec.Decode(dst); err != nil {
		return decodeError(err)
	}
	// A body with trailing content is malformed, not merely tolerable.
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errs.Invalid(errs.CodeValidation, "request body must contain a single JSON object")
	}
	return nil
}

func decodeError(err error) error {
	var syntaxErr *json.SyntaxError
	var typeErr *json.UnmarshalTypeError
	var maxErr *http.MaxBytesError

	switch {
	case errors.As(err, &syntaxErr):
		return errs.Invalid(errs.CodeValidation, "request body contains malformed JSON at byte %d", syntaxErr.Offset)
	case errors.As(err, &typeErr):
		e := errs.Invalid(errs.CodeValidation, "request body has a value of the wrong type")
		if typeErr.Field != "" {
			e = e.WithField(typeErr.Field, fmt.Sprintf("must be a %s", typeErr.Type))
		}
		return e
	case errors.As(err, &maxErr):
		return errs.Invalid(errs.CodeValidation, "request body must not exceed %d bytes", maxBodyBytes)
	case errors.Is(err, io.EOF):
		return errs.Invalid(errs.CodeValidation, "request body must not be empty")
	default:
		return errs.Invalid(errs.CodeValidation, "request body could not be parsed: %s", err.Error())
	}
}

func isJSONContentType(ct string) bool {
	for i := range len(ct) {
		if ct[i] == ';' {
			ct = ct[:i]
			break
		}
	}
	switch trimSpace(ct) {
	case "application/json", "application/json; charset=utf-8", "":
		return true
	default:
		return false
	}
}

func trimSpace(s string) string {
	start, end := 0, len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t') {
		end--
	}
	return s[start:end]
}
