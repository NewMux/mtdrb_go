package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/NewMux/mtdrb_go/internal/db"
	"github.com/NewMux/mtdrb_go/internal/mail"
	"github.com/NewMux/mtdrb_go/internal/platform/errs"
	"github.com/NewMux/mtdrb_go/internal/platform/ids"
	"github.com/NewMux/mtdrb_go/internal/platform/logger"
	"github.com/NewMux/mtdrb_go/internal/tenancy"
)

// Security is what the account-security features need beyond signing in.
type Security struct {
	// ColumnKey encrypts two-factor secrets, as it does medical notes.
	ColumnKey []byte
	// Mailer sends password-reset links. Nil drops them, for tests.
	Mailer mail.Sender
	// AppURL is where the app is served, for the link in a reset email.
	AppURL string
	// PurgeAfter is the grace period before a deleted practice is purged.
	// Zero means DefaultPurgeAfter.
	PurgeAfter time.Duration
}

// WithSecurity equips the service for two-factor, password reset and the
// rest. Separate from NewService so the entrypoints that only sign people in
// — tests, the demo recorder — need not invent a mailer.
func (s *Service) WithSecurity(sec Security) *Service {
	s.security = sec
	return s
}

// resetTTL is how long a reset link works. Long enough to find the email,
// short enough that one sitting in an inbox for a week is useless.
const resetTTL = time.Hour

// ---------------------------------------------------------------------------
// Profile

// Profile is the signed-in trainer as they see themselves.
type Profile struct {
	UserID            ids.ID     `json:"user_id"`
	Email             string     `json:"email"`
	DisplayName       string     `json:"display_name"`
	Role              string     `json:"role"`
	MFAEnabled        bool       `json:"mfa_enabled"`
	PasswordChangedAt *time.Time `json:"password_changed_at"`
}

// ProfileUpdate changes a trainer's name or email. An email change needs the
// current password: it is where a reset link would go, so changing it is
// changing who can take the account over.
type ProfileUpdate struct {
	DisplayName     *string
	Email           *string
	CurrentPassword string
}

func trainer(ctx context.Context) (tenancy.Principal, error) {
	return tenancy.RequireTrainer(ctx)
}

// Profile reads the caller's profile.
func (s *Service) Profile(ctx context.Context) (Profile, error) {
	p, err := trainer(ctx)
	if err != nil {
		return Profile{}, err
	}
	var out Profile
	err = s.pool.InTenantTx(ctx, p.TenantID, func(tx pgx.Tx) error {
		out, err = readProfile(ctx, tx, p.SubjectID)
		return err
	})
	return out, err
}

func readProfile(ctx context.Context, tx pgx.Tx, userID ids.ID) (Profile, error) {
	var out Profile
	if err := tx.QueryRow(ctx,
		`SELECT id, email, display_name, role::text, totp_enabled_at IS NOT NULL, password_changed_at
		   FROM users WHERE id = $1`, userID,
	).Scan(&out.UserID, &out.Email, &out.DisplayName, &out.Role, &out.MFAEnabled, &out.PasswordChangedAt); err != nil {
		if db.IsNoRows(err) {
			return Profile{}, errs.NotFound("no such user")
		}
		return Profile{}, errs.Internal(err, "read profile")
	}
	return out, nil
}

// UpdateProfile changes the caller's name or email.
func (s *Service) UpdateProfile(ctx context.Context, in ProfileUpdate) (Profile, error) {
	p, err := trainer(ctx)
	if err != nil {
		return Profile{}, err
	}
	var out Profile
	err = s.pool.InTenantTx(ctx, p.TenantID, func(tx pgx.Tx) error {
		if in.DisplayName != nil {
			name := strings.TrimSpace(*in.DisplayName)
			if name == "" {
				return errs.Invalid(errs.CodeValidation, "a display name is required").
					WithField("display_name", "is required")
			}
			if _, err := tx.Exec(ctx, `UPDATE users SET display_name = $2 WHERE id = $1`, p.SubjectID, name); err != nil {
				return errs.Internal(err, "update display name")
			}
		}
		if in.Email != nil {
			email := normalizeEmail(*in.Email)
			if email == "" || !strings.Contains(email, "@") {
				return errs.Invalid(errs.CodeValidation, "an email address is required").
					WithField("email", "must be an email address")
			}
			if err := s.checkPassword(ctx, tx, p.SubjectID, in.CurrentPassword); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE users SET email = $2 WHERE id = $1`, p.SubjectID, email); err != nil {
				if db.IsUniqueViolation(err, "users_email_key") {
					return errs.Conflict("email_taken", "that email address is already registered").
						WithField("email", "is already registered")
				}
				return errs.Internal(err, "update email")
			}
		}
		out, err = readProfile(ctx, tx, p.SubjectID)
		return err
	})
	return out, err
}

// checkPassword confirms the caller knows their current password.
func (s *Service) checkPassword(ctx context.Context, tx pgx.Tx, userID ids.ID, password string) error {
	var hash string
	if err := tx.QueryRow(ctx, `SELECT password_hash FROM users WHERE id = $1`, userID).Scan(&hash); err != nil {
		return errs.Internal(err, "read password")
	}
	ok, err := VerifyPassword(password, hash)
	if err != nil {
		return errs.Internal(err, "verify password")
	}
	if !ok {
		return errs.Invalid(errs.CodeInvalidCredentials, "the current password is not right").
			WithField("current_password", "is not right")
	}
	return nil
}

// ChangePassword sets a new password and signs out every other device.
//
// Every other device, not every device: the one the trainer is holding stays
// signed in, because a password change is usually made *because* something
// else might be signed in, and making the person doing it log in again adds
// nothing.
func (s *Service) ChangePassword(ctx context.Context, current, next string) error {
	p, err := trainer(ctx)
	if err != nil {
		return err
	}
	if err := ValidatePasswordStrength(next); err != nil {
		return err
	}
	hash, err := HashPassword(next, s.params)
	if err != nil {
		return err
	}
	return s.pool.InTenantTx(ctx, p.TenantID, func(tx pgx.Tx) error {
		if err := s.checkPassword(ctx, tx, p.SubjectID, current); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx,
			`UPDATE users SET password_hash = $2, password_changed_at = $3 WHERE id = $1`,
			p.SubjectID, hash, s.clock.Now()); err != nil {
			return errs.Internal(err, "change password")
		}
		_, err := revokeFamilies(ctx, tx, p.SubjectID, p.FamilyID)
		return err
	})
}

// revokeFamilies signs out every session of a user except keep's.
func revokeFamilies(ctx context.Context, tx pgx.Tx, userID, keep ids.ID) (int64, error) {
	tag, err := tx.Exec(ctx,
		`UPDATE refresh_tokens SET revoked_at = now()
		  WHERE user_id = $1 AND family_id <> $2 AND revoked_at IS NULL`, userID, keep)
	if err != nil {
		return 0, errs.Internal(err, "sign out other devices")
	}
	return tag.RowsAffected(), nil
}

// ---------------------------------------------------------------------------
// Devices

// Device is one signed-in session chain: a phone, a laptop's browser.
type Device struct {
	ID         ids.ID    `json:"id"`
	UserAgent  string    `json:"user_agent"`
	SignedInAt time.Time `json:"signed_in_at"`
	LastSeenAt time.Time `json:"last_seen_at"`
	// Current is the device asking.
	Current bool `json:"current"`
}

// Devices lists the caller's live sessions, most recently used first.
//
// Built from refresh-token families: every rotation inserts a row, so the
// newest row in a family is when that device last came back, and a family
// with no usable token left is a device that is signed out.
func (s *Service) Devices(ctx context.Context) ([]Device, error) {
	p, err := trainer(ctx)
	if err != nil {
		return nil, err
	}
	out := []Device{}
	err = s.pool.InTenantTx(ctx, p.TenantID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT family_id, min(created_at), max(created_at),
			       coalesce((array_agg(user_agent ORDER BY created_at DESC))[1], '')
			  FROM refresh_tokens
			 WHERE user_id = $1
			 GROUP BY family_id
			HAVING bool_or(revoked_at IS NULL AND used_at IS NULL AND expires_at > $2)
			 ORDER BY max(created_at) DESC`, p.SubjectID, s.clock.Now())
		if err != nil {
			return errs.Internal(err, "list devices")
		}
		defer rows.Close()
		for rows.Next() {
			var d Device
			if err := rows.Scan(&d.ID, &d.SignedInAt, &d.LastSeenAt, &d.UserAgent); err != nil {
				return errs.Internal(err, "read device")
			}
			d.Current = d.ID == p.FamilyID
			out = append(out, d)
		}
		return rows.Err()
	})
	return out, err
}

// SignOutDevice ends one session chain.
func (s *Service) SignOutDevice(ctx context.Context, familyID ids.ID) error {
	p, err := trainer(ctx)
	if err != nil {
		return err
	}
	return s.pool.InTenantTx(ctx, p.TenantID, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx,
			`UPDATE refresh_tokens SET revoked_at = now()
			  WHERE user_id = $1 AND family_id = $2 AND revoked_at IS NULL`, p.SubjectID, familyID)
		if err != nil {
			return errs.Internal(err, "sign out device")
		}
		if tag.RowsAffected() == 0 {
			return errs.NotFound("no such device")
		}
		return nil
	})
}

// SignOutOtherDevices ends every session but the caller's own.
func (s *Service) SignOutOtherDevices(ctx context.Context) error {
	p, err := trainer(ctx)
	if err != nil {
		return err
	}
	return s.pool.InTenantTx(ctx, p.TenantID, func(tx pgx.Tx) error {
		_, err := revokeFamilies(ctx, tx, p.SubjectID, p.FamilyID)
		return err
	})
}

// ---------------------------------------------------------------------------
// Two-factor

// MFASetup is what an authenticator app needs to start producing codes.
type MFASetup struct {
	Secret string `json:"secret"`
	URI    string `json:"otpauth_uri"`
}

func (s *Service) columnKey() (string, error) {
	if len(s.security.ColumnKey) == 0 {
		return "", errs.Internal(fmt.Errorf("no column key configured"), "two-factor is unavailable")
	}
	return string(s.security.ColumnKey), nil
}

// BeginMFA generates a secret for the caller. It is stored but not yet in
// force: EnableMFA switches it on once the trainer proves their app reads it,
// so a botched scan cannot lock anyone out.
func (s *Service) BeginMFA(ctx context.Context) (MFASetup, error) {
	p, err := trainer(ctx)
	if err != nil {
		return MFASetup{}, err
	}
	key, err := s.columnKey()
	if err != nil {
		return MFASetup{}, err
	}
	secret, err := NewTOTPSecret()
	if err != nil {
		return MFASetup{}, errs.Internal(err, "generate secret")
	}
	var email string
	err = s.pool.InTenantTx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var enabled bool
		if err := tx.QueryRow(ctx,
			`SELECT email, totp_enabled_at IS NOT NULL FROM users WHERE id = $1 FOR UPDATE`, p.SubjectID,
		).Scan(&email, &enabled); err != nil {
			return errs.Internal(err, "read user")
		}
		if enabled {
			return errs.Conflict("mfa_already_enabled", "two-step sign-in is already on; turn it off first")
		}
		if _, err := tx.Exec(ctx,
			`UPDATE users SET totp_secret_encrypted = pgp_sym_encrypt($2::text, $3::text), totp_last_step = NULL
			  WHERE id = $1`, p.SubjectID, secret, key); err != nil {
			return errs.Internal(err, "store secret")
		}
		return nil
	})
	if err != nil {
		return MFASetup{}, err
	}
	return MFASetup{Secret: secret, URI: TOTPURI(secret, email)}, nil
}

// EnableMFA turns two-factor on with a code from the app, and returns the
// recovery codes — the only time they are ever shown.
func (s *Service) EnableMFA(ctx context.Context, code string) ([]string, error) {
	p, err := trainer(ctx)
	if err != nil {
		return nil, err
	}
	key, err := s.columnKey()
	if err != nil {
		return nil, err
	}
	codes, err := NewRecoveryCodes()
	if err != nil {
		return nil, errs.Internal(err, "generate recovery codes")
	}
	err = s.pool.InTenantTx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var (
			secret  *string
			enabled bool
		)
		if err := tx.QueryRow(ctx,
			`SELECT CASE WHEN totp_secret_encrypted IS NULL THEN NULL
			             ELSE pgp_sym_decrypt(totp_secret_encrypted, $2::text) END,
			        totp_enabled_at IS NOT NULL
			   FROM users WHERE id = $1 FOR UPDATE`, p.SubjectID, key,
		).Scan(&secret, &enabled); err != nil {
			return errs.Internal(err, "read secret")
		}
		if enabled {
			return errs.Conflict("mfa_already_enabled", "two-step sign-in is already on")
		}
		if secret == nil {
			return errs.Conflict(errs.CodeInvalidTransition, "start two-step setup first")
		}
		step, ok := VerifyTOTP(*secret, code, s.clock.Now(), 0)
		if !ok {
			return errs.Invalid(errs.CodeInvalidMFACode, "that code is not right; check the time on your phone").
				WithField("code", "is not right")
		}
		if _, err := tx.Exec(ctx,
			`UPDATE users SET totp_enabled_at = $2, totp_last_step = $3 WHERE id = $1`,
			p.SubjectID, s.clock.Now(), step); err != nil {
			return errs.Internal(err, "enable two-step")
		}
		return replaceRecoveryCodes(ctx, tx, p.TenantID, p.SubjectID, codes)
	})
	if err != nil {
		return nil, err
	}
	return codes, nil
}

func replaceRecoveryCodes(ctx context.Context, tx pgx.Tx, tenantID, userID ids.ID, codes []string) error {
	if _, err := tx.Exec(ctx, `DELETE FROM mfa_recovery_codes WHERE user_id = $1`, userID); err != nil {
		return errs.Internal(err, "clear recovery codes")
	}
	for _, c := range codes {
		if _, err := tx.Exec(ctx,
			`INSERT INTO mfa_recovery_codes (id, tenant_id, user_id, code_hash) VALUES ($1, $2, $3, $4)`,
			ids.New(), tenantID, userID, HashRecoveryCode(c)); err != nil {
			return errs.Internal(err, "store recovery code")
		}
	}
	return nil
}

// DisableMFA turns two-factor off. The password, not a code: the trainer
// who lost their phone is exactly the one who needs to do this.
func (s *Service) DisableMFA(ctx context.Context, password string) error {
	p, err := trainer(ctx)
	if err != nil {
		return err
	}
	return s.pool.InTenantTx(ctx, p.TenantID, func(tx pgx.Tx) error {
		if err := s.checkPassword(ctx, tx, p.SubjectID, password); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx,
			`UPDATE users SET totp_secret_encrypted = NULL, totp_enabled_at = NULL, totp_last_step = NULL
			  WHERE id = $1`, p.SubjectID); err != nil {
			return errs.Internal(err, "disable two-step")
		}
		if _, err := tx.Exec(ctx, `DELETE FROM mfa_recovery_codes WHERE user_id = $1`, p.SubjectID); err != nil {
			return errs.Internal(err, "clear recovery codes")
		}
		return nil
	})
}

// VerifyMFA completes a sign-in that stopped for a second factor. The code
// is either from the authenticator app or one of the recovery codes.
func (s *Service) VerifyMFA(ctx context.Context, challenge, code, userAgent string) (Account, Tokens, error) {
	tenantID, userID, err := s.issuer.ParseMFAChallenge(challenge)
	if err != nil {
		return Account{}, Tokens{}, err
	}
	key, err := s.columnKey()
	if err != nil {
		return Account{}, Tokens{}, err
	}
	wrong := errs.Invalid(errs.CodeInvalidMFACode, "that code is not right").WithField("code", "is not right")

	var (
		account Account
		tokens  Tokens
		passed  bool
	)
	err = s.pool.InTenantTx(ctx, tenantID, func(tx pgx.Tx) error {
		var (
			secret      *string
			lastStep    *int64
			deactivated *time.Time
		)
		if err := tx.QueryRow(ctx,
			`SELECT u.id, u.tenant_id, u.email, u.display_name, u.role::text, u.deactivated_at,
			        t.default_currency,
			        CASE WHEN u.totp_enabled_at IS NULL THEN NULL
			             ELSE pgp_sym_decrypt(u.totp_secret_encrypted, $2::text) END,
			        u.totp_last_step
			   FROM users u JOIN tenants t ON t.id = u.tenant_id
			  WHERE u.id = $1 FOR UPDATE OF u`, userID, key,
		).Scan(&account.UserID, &account.TenantID, &account.Email, &account.DisplayName, &account.Role,
			&deactivated, &account.Currency, &secret, &lastStep); err != nil {
			if db.IsNoRows(err) {
				return errs.Unauthorized(errs.CodeInvalidCredentials, "email or password is incorrect")
			}
			return errs.Internal(err, "read user")
		}
		if deactivated != nil || secret == nil {
			return errs.Unauthorized(errs.CodeInvalidCredentials, "email or password is incorrect")
		}

		var last int64
		if lastStep != nil {
			last = *lastStep
		}
		if step, ok := VerifyTOTP(*secret, code, s.clock.Now(), last); ok {
			if _, err := tx.Exec(ctx, `UPDATE users SET totp_last_step = $2 WHERE id = $1`, userID, step); err != nil {
				return errs.Internal(err, "record code")
			}
		} else {
			tag, err := tx.Exec(ctx,
				`UPDATE mfa_recovery_codes SET used_at = $3
				  WHERE user_id = $1 AND code_hash = $2 AND used_at IS NULL`,
				userID, HashRecoveryCode(code), s.clock.Now())
			if err != nil {
				return errs.Internal(err, "use recovery code")
			}
			if tag.RowsAffected() == 0 {
				return nil
			}
		}
		passed = true

		if _, err := tx.Exec(ctx, `UPDATE users SET last_login_at = now() WHERE id = $1`, userID); err != nil {
			return errs.Internal(err, "record login")
		}
		tokens, err = s.issueTokens(ctx, tx, account, ids.Nil, userAgent)
		return err
	})
	if err != nil {
		return Account{}, Tokens{}, err
	}
	if !passed {
		return Account{}, Tokens{}, wrong
	}
	return account, tokens, nil
}

// ---------------------------------------------------------------------------
// Password reset

// RequestPasswordReset emails a reset link, if the address has an account.
//
// It reports success either way. Saying "no such account" would make this
// the easiest way to find out who trains with CoachPulse; the email itself is
// the only place the answer appears.
func (s *Service) RequestPasswordReset(ctx context.Context, email string) error {
	var (
		userID, tenantID ids.ID
		deactivated      *time.Time
		address          string
	)
	err := s.pool.Raw().QueryRow(ctx,
		`SELECT user_id, tenant_id, email, deactivated_at FROM auth_lookup_user_by_email($1)`,
		normalizeEmail(email),
	).Scan(&userID, &tenantID, &address, &deactivated)
	if err != nil {
		if db.IsNoRows(err) {
			return nil
		}
		return errs.Internal(err, "look up user")
	}
	if deactivated != nil {
		return nil
	}

	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return errs.Internal(err, "generate reset token")
	}
	token := base64.RawURLEncoding.EncodeToString(buf)

	var language string
	err = s.pool.InTenantTx(ctx, tenantID, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT language FROM tenants WHERE id = $1`, tenantID).Scan(&language); err != nil {
			return errs.Internal(err, "read language")
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO password_resets (id, tenant_id, user_id, token_hash, expires_at)
			 VALUES ($1, $2, $3, $4, $5)`,
			ids.New(), tenantID, userID, HashRefreshToken(token), s.clock.Now().Add(resetTTL)); err != nil {
			return errs.Internal(err, "store reset token")
		}
		return nil
	})
	if err != nil {
		return err
	}

	if s.security.Mailer == nil {
		return nil
	}
	link := strings.TrimRight(s.security.AppURL, "/") + "/reset-password?token=" + token
	if err := s.security.Mailer.Send(ctx, resetEmail(address, language, link)); err != nil {
		// Logged, not returned: a delivery failure that only happens for
		// real accounts would answer the question this endpoint refuses to.
		logger.From(ctx).ErrorContext(ctx, "password reset email not sent", slog.Any("error", err))
	}
	return nil
}

func resetEmail(to, language, link string) mail.Message {
	if language == "ar" {
		return mail.Message{
			To:      to,
			Subject: "إعادة تعيين كلمة المرور في CoachPulse",
			Text: "طلب أحدهم إعادة تعيين كلمة المرور لحسابك في CoachPulse.\n\n" +
				"لاختيار كلمة مرور جديدة، افتح هذا الرابط خلال ساعة:\n" + link + "\n\n" +
				"إن لم تطلب ذلك فتجاهل هذه الرسالة؛ كلمة المرور الحالية ما زالت تعمل.\n",
		}
	}
	return mail.Message{
		To:      to,
		Subject: "Reset your CoachPulse password",
		Text: "Someone asked to reset the password for your CoachPulse account.\n\n" +
			"To choose a new one, open this link within the hour:\n" + link + "\n\n" +
			"If it wasn't you, ignore this email; your password still works.\n",
	}
}

// ResetPassword sets a new password from a reset link and signs out every
// device: whoever asked for the reset may not be the only one who knew the
// old password.
func (s *Service) ResetPassword(ctx context.Context, token, password string) error {
	if err := ValidatePasswordStrength(password); err != nil {
		return err
	}
	invalid := errs.Invalid("reset_link_invalid", "this reset link has expired or been used; ask for a new one")

	var (
		resetID, tenantID, userID ids.ID
		expiresAt                 time.Time
		usedAt                    *time.Time
	)
	err := s.pool.Raw().QueryRow(ctx,
		`SELECT reset_id, tenant_id, user_id, expires_at, used_at FROM auth_lookup_password_reset($1)`,
		HashRefreshToken(strings.TrimSpace(token)),
	).Scan(&resetID, &tenantID, &userID, &expiresAt, &usedAt)
	if err != nil {
		if db.IsNoRows(err) {
			return invalid
		}
		return errs.Internal(err, "look up reset")
	}
	if usedAt != nil || !s.clock.Now().Before(expiresAt) {
		return invalid
	}

	hash, err := HashPassword(password, s.params)
	if err != nil {
		return err
	}
	return s.pool.InTenantTx(ctx, tenantID, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx,
			`UPDATE password_resets SET used_at = $2 WHERE id = $1 AND used_at IS NULL`, resetID, s.clock.Now())
		if err != nil {
			return errs.Internal(err, "use reset")
		}
		if tag.RowsAffected() == 0 {
			return invalid
		}
		if _, err := tx.Exec(ctx,
			`UPDATE users SET password_hash = $2, password_changed_at = $3 WHERE id = $1`,
			userID, hash, s.clock.Now()); err != nil {
			return errs.Internal(err, "reset password")
		}
		_, err = revokeFamilies(ctx, tx, userID, ids.Nil)
		return err
	})
}
