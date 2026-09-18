package crm

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/NewMux/mtdrb_go/internal/db"
	"github.com/NewMux/mtdrb_go/internal/platform/errs"
	"github.com/NewMux/mtdrb_go/internal/platform/ids"
)

// Measurements are stored as integers in their smallest sensible unit —
// grams, basis points, millimetres — for the same reason money is. Kilograms
// versus pounds is a display choice; the stored value should never round.

// BiometricEntry is one set of body measurements.
type BiometricEntry struct {
	ID          ids.ID    `json:"id"`
	ClientID    ids.ID    `json:"client_id"`
	MeasuredOn  time.Time `json:"measured_on"`
	WeightGrams *int32    `json:"weight_grams,omitempty"`
	// BodyFatBP is basis points: 1550 means 15.50%.
	BodyFatBP *int32 `json:"body_fat_bp,omitempty"`
	// Circumferences are millimetres keyed by site, open-ended because
	// trainers measure different sites for different goals.
	Circumferences map[string]int32 `json:"circumferences,omitempty"`
	Notes          string           `json:"notes"`
	CreatedAt      time.Time        `json:"created_at"`
	ServerSeq      int64            `json:"server_seq"`
}

// BiometricInput describes a measurement to record.
type BiometricInput struct {
	ClientID       ids.ID
	MeasuredOn     time.Time
	WeightGrams    *int32
	BodyFatBP      *int32
	Circumferences map[string]int32
	Notes          string
}

// RecordBiometrics stores a measurement.
func (s *Service) RecordBiometrics(ctx context.Context, tx pgx.Tx, tenantID ids.ID, in BiometricInput) (BiometricEntry, error) {
	if in.WeightGrams != nil && *in.WeightGrams <= 0 {
		return BiometricEntry{}, errs.Invalid(errs.CodeValidation, "weight must be positive").
			WithField("weight_grams", "must be greater than zero")
	}
	if in.BodyFatBP != nil && (*in.BodyFatBP < 0 || *in.BodyFatBP > 10000) {
		return BiometricEntry{}, errs.Invalid(errs.CodeValidation, "body fat must be between 0 and 100 percent").
			WithField("body_fat_bp", "must be between 0 and 10000 basis points")
	}
	for site, mm := range in.Circumferences {
		if mm <= 0 {
			return BiometricEntry{}, errs.Invalid(errs.CodeValidation,
				"circumference for %q must be positive", site).
				WithField("circumferences", "measurements must be greater than zero")
		}
	}
	if in.MeasuredOn.IsZero() {
		in.MeasuredOn = s.clock.Now()
	}
	measuredOn := time.Date(in.MeasuredOn.Year(), in.MeasuredOn.Month(), in.MeasuredOn.Day(), 0, 0, 0, 0, time.UTC)

	circumferences := in.Circumferences
	if circumferences == nil {
		circumferences = map[string]int32{}
	}
	payload, err := json.Marshal(circumferences)
	if err != nil {
		return BiometricEntry{}, errs.Internal(err, "encode circumferences")
	}

	e := BiometricEntry{
		ID: ids.New(), ClientID: in.ClientID, MeasuredOn: measuredOn,
		WeightGrams: in.WeightGrams, BodyFatBP: in.BodyFatBP,
		Circumferences: circumferences, Notes: in.Notes,
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO biometric_entries
			(id, tenant_id, client_id, measured_on, weight_grams, body_fat_bp, circumferences, notes)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING created_at, server_seq`,
		e.ID, tenantID, in.ClientID, measuredOn, in.WeightGrams, in.BodyFatBP, payload, in.Notes,
	).Scan(&e.CreatedAt, &e.ServerSeq); err != nil {
		if db.IsForeignKeyViolation(err) {
			return BiometricEntry{}, errs.NotFound("client")
		}
		return BiometricEntry{}, errs.Internal(err, "record biometrics")
	}
	return e, nil
}

// BiometricHistory returns a client's measurements, most recent first.
func (s *Service) BiometricHistory(ctx context.Context, tx pgx.Tx, clientID ids.ID, limit int) ([]BiometricEntry, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := tx.Query(ctx, `
		SELECT id, client_id, measured_on, weight_grams, body_fat_bp, circumferences, notes,
		       created_at, server_seq
		  FROM biometric_entries
		 WHERE client_id = $1 AND deleted_at IS NULL
		 ORDER BY measured_on DESC, created_at DESC
		 LIMIT $2`, clientID, limit)
	if err != nil {
		return nil, errs.Internal(err, "load biometric history")
	}
	defer rows.Close()

	var out []BiometricEntry
	for rows.Next() {
		var e BiometricEntry
		var payload []byte
		if err := rows.Scan(&e.ID, &e.ClientID, &e.MeasuredOn, &e.WeightGrams, &e.BodyFatBP,
			&payload, &e.Notes, &e.CreatedAt, &e.ServerSeq); err != nil {
			return nil, errs.Internal(err, "scan biometric entry")
		}
		if err := json.Unmarshal(payload, &e.Circumferences); err != nil {
			return nil, errs.Internal(err, "decode circumferences")
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, errs.Internal(err, "read biometric history")
	}
	return out, nil
}

// DeleteBiometrics soft-deletes a measurement.
func (s *Service) DeleteBiometrics(ctx context.Context, tx pgx.Tx, entryID ids.ID) error {
	tag, err := tx.Exec(ctx,
		`UPDATE biometric_entries SET deleted_at = now() WHERE id = $1 AND deleted_at IS NULL`, entryID)
	if err != nil {
		return errs.Internal(err, "delete biometric entry")
	}
	if tag.RowsAffected() == 0 {
		return errs.NotFound("biometric entry")
	}
	return nil
}
