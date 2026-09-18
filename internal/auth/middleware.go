package auth

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/NewMux/mtdrb_go/internal/httpx"
	"github.com/NewMux/mtdrb_go/internal/platform/errs"
	"github.com/NewMux/mtdrb_go/internal/platform/ids"
	"github.com/NewMux/mtdrb_go/internal/platform/logger"
	"github.com/NewMux/mtdrb_go/internal/tenancy"
)

// Authenticate verifies the bearer token and attaches the principal.
//
// It establishes *who* the caller is. It does not decide what they may reach:
// that is the job of tenancy.RequireTrainer at the handler and of row-level
// security in the database.
func Authenticate(issuer *TokenIssuer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw, err := bearerToken(r)
			if err != nil {
				httpx.Error(w, r, err)
				return
			}
			claims, err := issuer.ParseAccess(raw)
			if err != nil {
				httpx.Error(w, r, err)
				return
			}

			subjectID, err := ids.Parse(claims.RegisteredClaims.Subject)
			if err != nil {
				httpx.Error(w, r, errs.Unauthorized(errs.CodeInvalidCredentials,
					"access token has an unreadable subject"))
				return
			}

			principal := tenancy.Principal{
				TenantID:  claims.TenantID,
				SubjectID: subjectID,
				Kind:      claims.Subject,
				Role:      claims.Role,
			}

			ctx := tenancy.WithPrincipal(r.Context(), principal)
			ctx = logger.Into(ctx, logger.From(ctx).With(
				slog.String("tenant_id", principal.TenantID.String()),
				slog.String("subject_id", principal.SubjectID.String()),
			))
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func bearerToken(r *http.Request) (string, error) {
	header := r.Header.Get("Authorization")
	if header == "" {
		return "", errs.Unauthorized(errs.CodeInvalidCredentials, "an Authorization header is required")
	}
	scheme, token, found := strings.Cut(header, " ")
	if !found || !strings.EqualFold(scheme, "bearer") || strings.TrimSpace(token) == "" {
		return "", errs.Unauthorized(errs.CodeInvalidCredentials, "Authorization header must be a bearer token")
	}
	return strings.TrimSpace(token), nil
}
