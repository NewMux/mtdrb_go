package auth

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/NewMux/mtdrb_go/internal/db"
	"github.com/NewMux/mtdrb_go/internal/platform/clock"
	"github.com/NewMux/mtdrb_go/internal/platform/errs"
	"github.com/NewMux/mtdrb_go/internal/platform/ids"
)

// ChartSeeder installs a tenant's opening chart of accounts.
//
// Signup depends on it through an interface so that auth does not import the
// ledger: a tenant is not usable without accounts, and creating them in the
// same transaction as the tenant means a half-provisioned account cannot exist.
type ChartSeeder interface {
	SeedChartOfAccounts(ctx context.Context, tx pgx.Tx, tenantID ids.ID, currency string) error
}

// Service implements registration, login and token rotation.
type Service struct {
	pool   *db.Pool
	issuer *TokenIssuer
	seeder ChartSeeder
	clock  clock.Clock
	params Argon2Params
}

// NewService builds the authentication service.
func NewService(pool *db.Pool, issuer *TokenIssuer, seeder ChartSeeder, c clock.Clock, p Argon2Params) *Service {
	if c == nil {
		c = clock.System{}
	}
	return &Service{pool: pool, issuer: issuer, seeder: seeder, clock: c, params: p}
}

// Tokens is the credential set returned by login and refresh.
type Tokens struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
	TokenType    string    `json:"token_type"`
}

// Account identifies the signed-in trainer and their tenant.
type Account struct {
	UserID      ids.ID `json:"user_id"`
	TenantID    ids.ID `json:"tenant_id"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
	Role        string `json:"role"`
	Currency    string `json:"currency"`
}

// SignupInput registers a new trainer and their tenant together.
type SignupInput struct {
	Email        string
	Password     string
	DisplayName  string
	BusinessName string
	Currency     string
	Timezone     string
}

// Signup provisions a tenant, its owner and its chart of accounts atomically.
func (s *Service) Signup(ctx context.Context, in SignupInput, userAgent string) (Account, Tokens, error) {
	email := normalizeEmail(in.Email)
	if email == "" {
		return Account{}, Tokens{}, errs.Invalid(errs.CodeValidation, "an email address is required").
			WithField("email", "is required")
	}
	if strings.TrimSpace(in.DisplayName) == "" {
		return Account{}, Tokens{}, errs.Invalid(errs.CodeValidation, "a display name is required").
			WithField("display_name", "is required")
	}
	if err := ValidatePasswordStrength(in.Password); err != nil {
		return Account{}, Tokens{}, err
	}

	businessName := strings.TrimSpace(in.BusinessName)
	if businessName == "" {
		businessName = strings.TrimSpace(in.DisplayName)
	}
	currency := strings.ToUpper(strings.TrimSpace(in.Currency))
	if currency == "" {
		currency = "USD"
	}
	if len(currency) != 3 {
		return Account{}, Tokens{}, errs.Invalid(errs.CodeValidation, "currency must be a three-letter ISO code").
			WithField("currency", "must be a three-letter ISO code")
	}
	timezone := strings.TrimSpace(in.Timezone)
	if timezone == "" {
		timezone = "UTC"
	}
	if _, err := time.LoadLocation(timezone); err != nil {
		return Account{}, Tokens{}, errs.Invalid(errs.CodeValidation, "timezone is not a recognised IANA name").
			WithField("timezone", "must be an IANA name such as Europe/Berlin")
	}

	hash, err := HashPassword(in.Password, s.params)
	if err != nil {
		return Account{}, Tokens{}, err
	}

	account := Account{
		UserID:      ids.New(),
		TenantID:    ids.New(),
		Email:       email,
		DisplayName: strings.TrimSpace(in.DisplayName),
		Role:        "owner",
		Currency:    currency,
	}
	var tokens Tokens

	// Signup binds the transaction to the tenant it is about to create.
	//
	// The tenants policy checks `id = current_tenant_id()`, so an unbound
	// insert is refused — correctly, since a row nobody can claim is a row
	// nobody can read. Binding first also scopes the user insert and the
	// chart-of-accounts seed that follow, so the whole provisioning runs under
	// the same policies as every later request rather than around them.
	err = s.pool.InTenantTx(ctx, account.TenantID, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx,
			`INSERT INTO tenants (id, name, default_currency, timezone) VALUES ($1, $2, $3, $4)`,
			account.TenantID, businessName, currency, timezone,
		); err != nil {
			return errs.Internal(err, "create tenant")
		}

		if _, err := tx.Exec(ctx,
			`INSERT INTO users (id, tenant_id, email, password_hash, display_name, role)
			 VALUES ($1, $2, $3, $4, $5, 'owner')`,
			account.UserID, account.TenantID, account.Email, hash, account.DisplayName,
		); err != nil {
			if db.IsUniqueViolation(err, "users_email_key") {
				return errs.Conflict("email_taken", "that email address is already registered")
			}
			return errs.Internal(err, "create user")
		}

		if s.seeder != nil {
			if err := s.seeder.SeedChartOfAccounts(ctx, tx, account.TenantID, currency); err != nil {
				return errs.Wrap(err, "seed chart of accounts")
			}
		}

		var err error
		tokens, err = s.issueTokens(ctx, tx, account, ids.Nil, userAgent)
		return err
	})
	if err != nil {
		return Account{}, Tokens{}, err
	}
	return account, tokens, nil
}

// Login authenticates a trainer by email and password.
func (s *Service) Login(ctx context.Context, email, password, userAgent string) (Account, Tokens, error) {
	// One error for every failure mode below: a distinct "no such account"
	// would turn this endpoint into an account enumeration oracle.
	invalid := errs.Unauthorized(errs.CodeInvalidCredentials, "email or password is incorrect")

	var (
		account      Account
		passwordHash string
		deactivated  *time.Time
	)

	// Login cannot be tenant-scoped: resolving the email is how the tenant is
	// discovered. auth_lookup_user_by_email is the narrow SECURITY DEFINER
	// function that exists for exactly this, rather than granting the app role
	// a blanket read across tenants.
	err := s.pool.Raw().QueryRow(ctx,
		`SELECT user_id, tenant_id, email, password_hash, display_name, role,
		        deactivated_at, currency
		   FROM auth_lookup_user_by_email($1)`,
		normalizeEmail(email),
	).Scan(&account.UserID, &account.TenantID, &account.Email, &passwordHash,
		&account.DisplayName, &account.Role, &deactivated, &account.Currency)

	if err != nil {
		if db.IsNoRows(err) {
			// Hash a dummy password so a missing account and a wrong password
			// take comparable time; otherwise response latency enumerates
			// registered emails.
			_, _ = HashPassword("timing-equalizer-placeholder", s.params)
			return Account{}, Tokens{}, invalid
		}
		return Account{}, Tokens{}, errs.Internal(err, "look up user")
	}

	ok, err := VerifyPassword(password, passwordHash)
	if err != nil {
		return Account{}, Tokens{}, errs.Internal(err, "verify password")
	}
	if !ok || deactivated != nil {
		return Account{}, Tokens{}, invalid
	}

	var tokens Tokens
	err = s.pool.InTenantTx(ctx, account.TenantID, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE users SET last_login_at = now() WHERE id = $1`, account.UserID); err != nil {
			return errs.Internal(err, "record login")
		}
		// Upgrade the stored hash if the cost policy has since been raised.
		if NeedsRehash(passwordHash, s.params) {
			if upgraded, err := HashPassword(password, s.params); err == nil {
				if _, err := tx.Exec(ctx, `UPDATE users SET password_hash = $1 WHERE id = $2`, upgraded, account.UserID); err != nil {
					return errs.Internal(err, "upgrade password hash")
				}
			}
		}
		var err error
		tokens, err = s.issueTokens(ctx, tx, account, ids.Nil, userAgent)
		return err
	})
	if err != nil {
		return Account{}, Tokens{}, err
	}
	return account, tokens, nil
}

// Refresh exchanges a refresh token for a new pair, rotating the old one.
//
// Presenting an already-used token means two parties hold the same secret, so
// the entire family is revoked and the holder must log in again. This is the
// standard detection for a stolen refresh token.
func (s *Service) Refresh(ctx context.Context, refreshToken, userAgent string) (Account, Tokens, error) {
	invalid := errs.Unauthorized(errs.CodeInvalidCredentials, "refresh token is not valid")
	hash := HashRefreshToken(strings.TrimSpace(refreshToken))

	var (
		tokenID, tenantID, userID, familyID ids.ID
		expiresAt                           time.Time
		usedAt, revokedAt                   *time.Time
	)
	// Same bootstrapping problem as login: the token hash is the only thing
	// known, and the tenant it belongs to is what must be discovered.
	err := s.pool.Raw().QueryRow(ctx,
		`SELECT token_id, tenant_id, user_id, family_id, expires_at, used_at, revoked_at
		   FROM auth_lookup_refresh_token($1)`,
		hash,
	).Scan(&tokenID, &tenantID, &userID, &familyID, &expiresAt, &usedAt, &revokedAt)
	if err != nil {
		if db.IsNoRows(err) {
			return Account{}, Tokens{}, invalid
		}
		return Account{}, Tokens{}, errs.Internal(err, "look up refresh token")
	}

	now := s.clock.Now()

	if usedAt != nil {
		// Replay of a rotated token: revoke the whole chain.
		if err := s.revokeFamily(ctx, tenantID, familyID); err != nil {
			return Account{}, Tokens{}, err
		}
		return Account{}, Tokens{}, errs.Unauthorized(errs.CodeTokenReused,
			"this refresh token has already been used; all sessions have been signed out")
	}
	if revokedAt != nil || now.After(expiresAt) {
		return Account{}, Tokens{}, invalid
	}

	var (
		account     Account
		tokens      Tokens
		deactivated *time.Time
	)
	err = s.pool.InTenantTx(ctx, tenantID, func(tx pgx.Tx) error {
		// Claim the token by marking it used, conditional on it still being
		// unused. Two concurrent refreshes race here and exactly one wins.
		tag, err := tx.Exec(ctx,
			`UPDATE refresh_tokens SET used_at = now() WHERE id = $1 AND used_at IS NULL`, tokenID)
		if err != nil {
			return errs.Internal(err, "rotate refresh token")
		}
		if tag.RowsAffected() == 0 {
			return invalid
		}

		if err := tx.QueryRow(ctx,
			`SELECT u.id, u.tenant_id, u.email, u.display_name, u.role::text, u.deactivated_at,
			        t.default_currency
			   FROM users u JOIN tenants t ON t.id = u.tenant_id
			  WHERE u.id = $1`, userID,
		).Scan(&account.UserID, &account.TenantID, &account.Email, &account.DisplayName,
			&account.Role, &deactivated, &account.Currency); err != nil {
			if db.IsNoRows(err) {
				return invalid
			}
			return errs.Internal(err, "look up user")
		}
		if deactivated != nil {
			return invalid
		}

		tokens, err = s.issueTokens(ctx, tx, account, familyID, userAgent)
		return err
	})
	if err != nil {
		return Account{}, Tokens{}, err
	}
	return account, tokens, nil
}

// Logout revokes the presented token's whole family, ending that device's
// session chain.
func (s *Service) Logout(ctx context.Context, tenantID ids.ID, refreshToken string) error {
	hash := HashRefreshToken(strings.TrimSpace(refreshToken))
	return s.pool.InTenantTx(ctx, tenantID, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx,
			`UPDATE refresh_tokens SET revoked_at = now()
			  WHERE family_id = (SELECT family_id FROM refresh_tokens WHERE token_hash = $1)
			    AND revoked_at IS NULL`, hash); err != nil {
			return errs.Internal(err, "revoke token family")
		}
		return nil
	})
}

func (s *Service) revokeFamily(ctx context.Context, tenantID, familyID ids.ID) error {
	return s.pool.InTenantTx(ctx, tenantID, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx,
			`UPDATE refresh_tokens SET revoked_at = now()
			  WHERE family_id = $1 AND revoked_at IS NULL`, familyID); err != nil {
			return errs.Internal(err, "revoke token family")
		}
		return nil
	})
}

// issueTokens mints an access/refresh pair inside an existing transaction.
func (s *Service) issueTokens(ctx context.Context, tx pgx.Tx, a Account, family ids.ID, userAgent string) (Tokens, error) {
	access, err := s.issuer.IssueUserAccess(a.TenantID, a.UserID, a.Role)
	if err != nil {
		return Tokens{}, err
	}
	refresh, err := s.issuer.NewRefreshToken(family)
	if err != nil {
		return Tokens{}, err
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO refresh_tokens (id, tenant_id, user_id, family_id, token_hash, expires_at, user_agent)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		ids.New(), a.TenantID, a.UserID, refresh.FamilyID, refresh.Hash, refresh.ExpiresAt, truncate(userAgent, 255),
	); err != nil {
		return Tokens{}, errs.Internal(err, "store refresh token")
	}
	return Tokens{
		AccessToken:  access,
		RefreshToken: refresh.Plaintext,
		ExpiresAt:    s.clock.Now().Add(s.issuer.AccessTTL()),
		TokenType:    "Bearer",
	}, nil
}

func normalizeEmail(e string) string { return strings.ToLower(strings.TrimSpace(e)) }

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
