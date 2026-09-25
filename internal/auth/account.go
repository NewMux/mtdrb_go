package auth

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/NewMux/mtdrb_go/internal/platform/errs"
	"github.com/NewMux/mtdrb_go/internal/platform/ids"
)

// DefaultPurgeAfter is how long a deleted practice waits before its rows are
// removed for good. Long enough to undo a deletion made by mistake, or by
// whoever held a stolen phone; short enough that "delete" means deleted.
const DefaultPurgeAfter = 30 * 24 * time.Hour

// Deletion is what deleting an account did.
type Deletion struct {
	// Scope is "practice" when the owner deleted the whole practice, or
	// "user" when a member deleted only themselves.
	Scope string `json:"scope"`
	// PurgeAfter is when the practice's records are removed for good. Set
	// for a practice; a member's own details are removed at once.
	PurgeAfter *time.Time `json:"purge_after,omitempty"`
}

// DeleteAccount deletes the caller's account, given their password.
//
// For the owner, the account is the practice: its clients, books and
// invoices exist for no one else. It is deactivated now — nobody can sign
// in, every session is revoked — and purged by the worker once the grace
// period has passed. For any other member, the practice's records stay with
// the practice; the member's own name and email are erased now and they can
// no longer sign in.
//
// The password, not a second factor, for the same reason as turning the
// second factor off: the person who lost their phone is one who may need
// this.
func (s *Service) DeleteAccount(ctx context.Context, password string) (Deletion, error) {
	p, err := trainer(ctx)
	if err != nil {
		return Deletion{}, err
	}
	grace := s.security.PurgeAfter
	if grace <= 0 {
		grace = DefaultPurgeAfter
	}
	now := s.clock.Now()
	var out Deletion
	err = s.pool.InTenantTx(ctx, p.TenantID, func(tx pgx.Tx) error {
		if err := s.checkPassword(ctx, tx, p.SubjectID, password); err != nil {
			return err
		}
		if p.IsOwner() {
			return deletePractice(ctx, tx, p.TenantID, now, grace, &out)
		}
		return deleteMember(ctx, tx, p.SubjectID, now, &out)
	})
	return out, err
}

func deletePractice(ctx context.Context, tx pgx.Tx, tenantID ids.ID, now time.Time, grace time.Duration, out *Deletion) error {
	if _, err := tx.Exec(ctx,
		`UPDATE tenants SET deleted_at = $2 WHERE id = $1 AND deleted_at IS NULL`, tenantID, now); err != nil {
		return errs.Internal(err, "mark practice deleted")
	}
	if _, err := tx.Exec(ctx,
		`UPDATE users SET deactivated_at = $1 WHERE deactivated_at IS NULL`, now); err != nil {
		return errs.Internal(err, "deactivate members")
	}
	if _, err := tx.Exec(ctx,
		`UPDATE refresh_tokens SET revoked_at = $1 WHERE revoked_at IS NULL`, now); err != nil {
		return errs.Internal(err, "sign out every device")
	}
	var deletedAt time.Time
	if err := tx.QueryRow(ctx, `SELECT deleted_at FROM tenants`).Scan(&deletedAt); err != nil {
		return errs.Internal(err, "read deletion")
	}
	purge := deletedAt.Add(grace)
	*out = Deletion{Scope: "practice", PurgeAfter: &purge}
	return nil
}

func deleteMember(ctx context.Context, tx pgx.Tx, userID ids.ID, now time.Time, out *Deletion) error {
	// Rows elsewhere keep pointing at the user — who marked a session, who
	// recorded a payment — so the row stays and what identifies a person
	// goes. The address is unique and must stay so; a reserved domain keeps
	// it from ever matching a real one.
	if _, err := tx.Exec(ctx, `
		UPDATE users
		   SET email = $2, display_name = 'Deleted member', password_hash = '!',
		       totp_secret_encrypted = NULL, totp_enabled_at = NULL, totp_last_step = NULL,
		       deactivated_at = $3
		 WHERE id = $1`,
		userID, fmt.Sprintf("deleted-%s@deleted.invalid", userID), now); err != nil {
		return errs.Internal(err, "erase member")
	}
	if _, err := tx.Exec(ctx, `DELETE FROM mfa_recovery_codes WHERE user_id = $1`, userID); err != nil {
		return errs.Internal(err, "clear recovery codes")
	}
	if _, err := tx.Exec(ctx,
		`UPDATE refresh_tokens SET revoked_at = $2 WHERE user_id = $1 AND revoked_at IS NULL`, userID, now); err != nil {
		return errs.Internal(err, "sign out every device")
	}
	*out = Deletion{Scope: "user"}
	return nil
}
