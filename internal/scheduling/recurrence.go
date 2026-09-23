package scheduling

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/NewMux/mtdrb_go/internal/platform/errs"
	"github.com/NewMux/mtdrb_go/internal/platform/ids"
)

// Occurrences are materialised as ordinary session rows rather than computed
// on read. A recurring slot is not really a rule — it is a list of real
// appointments, any one of which the trainer will move, cancel or attend
// individually. Computing them would mean modelling every exception; storing
// them means an exception is just a row that differs.

// RecurInput describes a repeating slot.
type RecurInput struct {
	SessionTypeID ids.ID
	ClientIDs     []ids.ID
	// Weekdays uses ISO numbering, 1 = Monday.
	Weekdays   []int
	StartsOn   time.Time
	EndsOn     time.Time
	TimeOfDay  time.Duration
	Timezone   string
	LocationID *ids.ID
	Location   string
}

// RecurResult reports what a series produced.
type RecurResult struct {
	SeriesID ids.ID    `json:"series_id"`
	Sessions []Session `json:"sessions"`
	// Skipped names occurrences that clashed with an existing booking. A
	// series spanning three months will usually hit something; refusing the
	// whole series over one clash would be worse than reporting it.
	Skipped []SkippedOccurrence `json:"skipped"`
}

// SkippedOccurrence is one occurrence that could not be booked.
type SkippedOccurrence struct {
	StartsAt time.Time `json:"starts_at"`
	Reason   string    `json:"reason"`
}

// maxOccurrences bounds a series so a typo in the end date cannot create tens
// of thousands of rows.
const maxOccurrences = 366

// Recur creates a repeating series and books its occurrences.
func (s *Service) Recur(ctx context.Context, tx pgx.Tx, tenantID ids.ID, in RecurInput) (RecurResult, error) {
	if len(in.Weekdays) == 0 {
		return RecurResult{}, errs.Invalid(errs.CodeValidation, "a series needs at least one weekday").
			WithField("weekdays", "is required")
	}
	for _, d := range in.Weekdays {
		if d < 1 || d > 7 {
			return RecurResult{}, errs.Invalid(errs.CodeValidation,
				"weekday %d is out of range; use 1 (Monday) to 7 (Sunday)", d).
				WithField("weekdays", "must be between 1 and 7")
		}
	}
	if in.EndsOn.Before(in.StartsOn) {
		return RecurResult{}, errs.Invalid(errs.CodeValidation, "a series cannot end before it starts").
			WithField("ends_on", "must not be before starts_on")
	}
	if in.TimeOfDay < 0 || in.TimeOfDay >= 24*time.Hour {
		return RecurResult{}, errs.Invalid(errs.CodeValidation, "time of day must be within a single day").
			WithField("time_of_day", "must be between 00:00 and 23:59")
	}

	location := time.UTC
	if in.Timezone != "" {
		loc, err := time.LoadLocation(in.Timezone)
		if err != nil {
			return RecurResult{}, errs.Invalid(errs.CodeValidation,
				"timezone is not a recognised IANA name").
				WithField("timezone", "must be an IANA name such as Europe/Berlin")
		}
		location = loc
	}

	wanted := make(map[time.Weekday]bool, len(in.Weekdays))
	for _, d := range in.Weekdays {
		// Go's Sunday is 0; ISO's Sunday is 7.
		wanted[time.Weekday(d%7)] = true
	}

	seriesID := ids.New()
	weekdays := make([]int16, 0, len(in.Weekdays))
	for _, d := range in.Weekdays {
		weekdays = append(weekdays, int16(d))
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO session_series
			(id, tenant_id, session_type_id, weekdays, starts_on, ends_on, time_of_day, timezone)
		VALUES ($1, $2, $3, $4, $5, $6, $7 * interval '1 minute', $8)`,
		seriesID, tenantID, in.SessionTypeID, weekdays,
		in.StartsOn, in.EndsOn, int(in.TimeOfDay/time.Minute), location.String()); err != nil {
		return RecurResult{}, errs.Internal(err, "create session series")
	}

	result := RecurResult{SeriesID: seriesID, Sessions: []Session{}, Skipped: []SkippedOccurrence{}}

	day := time.Date(in.StartsOn.Year(), in.StartsOn.Month(), in.StartsOn.Day(), 0, 0, 0, 0, location)
	end := time.Date(in.EndsOn.Year(), in.EndsOn.Month(), in.EndsOn.Day(), 0, 0, 0, 0, location)

	for count := 0; !day.After(end); day = day.AddDate(0, 0, 1) {
		if !wanted[day.Weekday()] {
			continue
		}
		if count >= maxOccurrences {
			break
		}
		count++

		// Built in the series' own timezone so a slot stays at the same local
		// hour across a daylight-saving change, which is what a trainer means
		// by "every Tuesday at seven".
		startsAt := day.Add(in.TimeOfDay)

		// Each occurrence gets its own savepoint: one clash must not discard
		// the occurrences already booked.
		if _, err := tx.Exec(ctx, "SAVEPOINT recur_occurrence"); err != nil {
			return RecurResult{}, errs.Internal(err, "open savepoint")
		}

		session, err := s.Book(ctx, tx, tenantID, BookInput{
			SessionTypeID: in.SessionTypeID,
			StartsAt:      startsAt,
			ClientIDs:     in.ClientIDs,
			LocationID:    in.LocationID,
			Location:      in.Location,
		})
		if err == nil {
			if _, relErr := tx.Exec(ctx, "RELEASE SAVEPOINT recur_occurrence"); relErr != nil {
				return RecurResult{}, errs.Internal(relErr, "release savepoint")
			}
			if _, err := tx.Exec(ctx,
				`UPDATE sessions SET series_id = $1 WHERE id = $2`, seriesID, session.ID); err != nil {
				return RecurResult{}, errs.Internal(err, "link session to series")
			}
			session.SeriesID = &seriesID
			result.Sessions = append(result.Sessions, session)
			continue
		}

		if _, rbErr := tx.Exec(ctx, "ROLLBACK TO SAVEPOINT recur_occurrence"); rbErr != nil {
			return RecurResult{}, errs.Internal(rbErr, "roll back savepoint")
		}
		if errs.CodeOf(err) != errs.CodeSchedulingConflict {
			return RecurResult{}, err
		}
		result.Skipped = append(result.Skipped, SkippedOccurrence{
			StartsAt: startsAt.UTC(),
			Reason:   errs.CodeSchedulingConflict,
		})
	}

	return result, nil
}

// CancelSeries cancels every remaining scheduled occurrence from a date.
//
// Past occurrences are untouched: cancelling a series in March must not erase
// the sessions already delivered in February.
func (s *Service) CancelSeries(ctx context.Context, tx pgx.Tx, seriesID ids.ID, from time.Time) (int, error) {
	tag, err := tx.Exec(ctx, `
		UPDATE sessions SET status = 'cancelled', cancelled_at = now()
		 WHERE series_id = $1 AND status = 'scheduled' AND starts_at >= $2`,
		seriesID, from)
	if err != nil {
		return 0, errs.Internal(err, "cancel series")
	}
	return int(tag.RowsAffected()), nil
}
