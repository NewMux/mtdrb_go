package programming

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/NewMux/mtdrb_go/internal/db"
	"github.com/NewMux/mtdrb_go/internal/platform/errs"
	"github.com/NewMux/mtdrb_go/internal/platform/ids"
)

// SaveProgramInput is a whole programme as the builder edits it: the
// programme row, its blocks, their days and the prescriptions on each.
//
// Every id is the device's. A trainer builds a programme on a train with no
// signal, and the assignment queued behind it references the programme by the
// id the device chose — so the server keeps it, exactly as it does for a
// workout and its sets.
type SaveProgramInput struct {
	ID          ids.ID
	Name        string
	Description string
	IsTemplate  bool
	Archived    bool
	Blocks      []SaveBlockInput
}

// SaveBlockInput is one block in a saved programme.
type SaveBlockInput struct {
	ID        ids.ID
	Name      string
	Weeks     int
	SortOrder int
	Notes     string
	Days      []SaveDayInput
}

// SaveDayInput is one training day in a saved block.
type SaveDayInput struct {
	ID            ids.ID
	Name          string
	SortOrder     int
	Notes         string
	Prescriptions []SavePrescriptionInput
}

// SavePrescriptionInput is one exercise prescribed on a saved day.
type SavePrescriptionInput struct {
	ID              ids.ID
	ExerciseID      ids.ID
	SortOrder       int
	TargetSets      int
	TargetRepsMin   *int
	TargetRepsMax   *int
	TargetLoadGrams *int
	TargetRPETenths *int
	TargetRIR       *int
	RestSeconds     *int
	Tempo           string
	SupersetGroup   string
	Notes           string
}

// SaveProgram writes a whole programme in one transaction, creating it if it
// is new and replacing its structure if not.
//
// Saving the document whole, rather than block by block, is what the builder
// does and what an offline outbox can replay: ten minutes of edits arrive as
// one operation, and replaying it converges on the same programme. It is
// last-write-wins (ADR 0004): two devices editing the same programme offline
// is rare, and the loser's version of a training plan is recoverable in a way
// that a lost payment is not.
//
// Rows are upserted by id, so a day that keeps its id keeps its identity —
// and workouts already logged against it keep pointing at it. Rows missing
// from the document are deleted; the logs that referenced them survive with
// the reference cleared (ON DELETE SET NULL), because removing a day from a
// plan must never delete what a client actually lifted.
//
// Archiving goes through the same path, so an archive made offline queues
// behind the edits that preceded it.
func (s *Service) SaveProgram(ctx context.Context, tx pgx.Tx, tenantID ids.ID, in SaveProgramInput) (Program, error) {
	if in.ID == (ids.ID{}) {
		return Program{}, errs.Invalid(errs.CodeValidation, "a saved programme needs an id").
			WithField("id", "is required")
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return Program{}, errs.Invalid(errs.CodeValidation, "a programme needs a name").
			WithField("name", "is required")
	}
	if err := validateDocument(in); err != nil {
		return Program{}, err
	}

	if err := s.upsertProgramRow(ctx, tx, tenantID, in, name); err != nil {
		return Program{}, err
	}

	blockIDs := make([]ids.ID, 0, len(in.Blocks))
	var dayIDs, rxIDs []ids.ID
	for bi, b := range in.Blocks {
		blockIDs = append(blockIDs, b.ID)
		if err := upsertChild(ctx, tx, "block", `
			INSERT INTO program_blocks (id, tenant_id, program_id, name, weeks, sort_order, notes)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			ON CONFLICT (id) DO UPDATE
			   SET name = EXCLUDED.name, weeks = EXCLUDED.weeks,
			       sort_order = EXCLUDED.sort_order, notes = EXCLUDED.notes
			 WHERE program_blocks.program_id = EXCLUDED.program_id`,
			b.ID, tenantID, in.ID, strings.TrimSpace(b.Name), b.Weeks, bi, strings.TrimSpace(b.Notes),
		); err != nil {
			return Program{}, err
		}

		for di, d := range b.Days {
			dayIDs = append(dayIDs, d.ID)
			// A day may move between blocks of the same programme, but never
			// out of it: the guard checks the day's current block belongs here.
			if err := upsertChild(ctx, tx, "training day", `
				INSERT INTO program_days (id, tenant_id, block_id, name, sort_order, notes)
				VALUES ($1, $2, $3, $4, $5, $6)
				ON CONFLICT (id) DO UPDATE
				   SET block_id = EXCLUDED.block_id, name = EXCLUDED.name,
				       sort_order = EXCLUDED.sort_order, notes = EXCLUDED.notes
				 WHERE program_days.block_id IN (SELECT id FROM program_blocks WHERE program_id = $7)`,
				d.ID, tenantID, b.ID, strings.TrimSpace(d.Name), di, strings.TrimSpace(d.Notes), in.ID,
			); err != nil {
				return Program{}, err
			}

			for pi, p := range d.Prescriptions {
				rxIDs = append(rxIDs, p.ID)
				sets := p.TargetSets
				if sets <= 0 {
					sets = 3
				}
				if err := upsertChild(ctx, tx, "prescription", `
					INSERT INTO program_exercises
						(id, tenant_id, day_id, exercise_id, sort_order, target_sets,
						 target_reps_min, target_reps_max, target_load_grams, target_rpe_tenths,
						 target_rir, rest_seconds, tempo, superset_group, notes)
					VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
					ON CONFLICT (id) DO UPDATE
					   SET day_id = EXCLUDED.day_id, exercise_id = EXCLUDED.exercise_id,
					       sort_order = EXCLUDED.sort_order, target_sets = EXCLUDED.target_sets,
					       target_reps_min = EXCLUDED.target_reps_min, target_reps_max = EXCLUDED.target_reps_max,
					       target_load_grams = EXCLUDED.target_load_grams,
					       target_rpe_tenths = EXCLUDED.target_rpe_tenths, target_rir = EXCLUDED.target_rir,
					       rest_seconds = EXCLUDED.rest_seconds, tempo = EXCLUDED.tempo,
					       superset_group = EXCLUDED.superset_group, notes = EXCLUDED.notes
					 WHERE program_exercises.day_id IN (
					       SELECT d.id FROM program_days d JOIN program_blocks b ON b.id = d.block_id
					        WHERE b.program_id = $16)`,
					p.ID, tenantID, d.ID, p.ExerciseID, pi, sets,
					p.TargetRepsMin, p.TargetRepsMax, p.TargetLoadGrams, p.TargetRPETenths,
					p.TargetRIR, p.RestSeconds, strings.TrimSpace(p.Tempo),
					strings.TrimSpace(p.SupersetGroup), strings.TrimSpace(p.Notes), in.ID,
				); err != nil {
					return Program{}, err
				}
			}
		}
	}

	// Remove what the document no longer has, innermost first. Upserts ran
	// first, so a row moved elsewhere has already left the parent being
	// pruned and is not swept up by its cascade.
	if _, err := tx.Exec(ctx, `
		DELETE FROM program_exercises
		 WHERE day_id IN (SELECT d.id FROM program_days d JOIN program_blocks b ON b.id = d.block_id
		                   WHERE b.program_id = $1)
		   AND NOT (id = ANY($2))`, in.ID, rxIDs); err != nil {
		return Program{}, errs.Internal(err, "prune prescriptions")
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM program_days
		 WHERE block_id IN (SELECT id FROM program_blocks WHERE program_id = $1)
		   AND NOT (id = ANY($2))`, in.ID, dayIDs); err != nil {
		return Program{}, errs.Internal(err, "prune training days")
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM program_blocks WHERE program_id = $1 AND NOT (id = ANY($2))`,
		in.ID, blockIDs); err != nil {
		return Program{}, errs.Internal(err, "prune blocks")
	}

	if in.Archived {
		return Program{ID: in.ID, Name: name, Description: strings.TrimSpace(in.Description), IsTemplate: in.IsTemplate}, nil
	}
	return s.GetProgram(ctx, tx, in.ID)
}

// upsertProgramRow creates or updates the programme row itself.
//
// The update always touches updated_at, which bumps server_seq — that is how
// a change anywhere in the structure reaches other devices, since blocks,
// days and prescriptions carry no sequence of their own and travel with the
// programme row.
func (s *Service) upsertProgramRow(ctx context.Context, tx pgx.Tx, tenantID ids.ID, in SaveProgramInput, name string) error {
	tag, err := tx.Exec(ctx, `
		UPDATE programs
		   SET name = $2, description = $3, is_template = $4,
		       archived_at = CASE WHEN $5 THEN coalesce(archived_at, now()) ELSE NULL END,
		       updated_at = now()
		 WHERE id = $1`,
		in.ID, name, strings.TrimSpace(in.Description), in.IsTemplate, in.Archived)
	if err != nil {
		return errs.Internal(err, "update programme")
	}
	if tag.RowsAffected() == 1 {
		return nil
	}

	// Not visible to this tenant: either new, or someone else's id.
	tag, err = tx.Exec(ctx, `
		INSERT INTO programs (id, tenant_id, name, description, is_template, archived_at)
		VALUES ($1, $2, $3, $4, $5, CASE WHEN $6 THEN now() ELSE NULL END)
		ON CONFLICT (id) DO NOTHING`,
		in.ID, tenantID, name, strings.TrimSpace(in.Description), in.IsTemplate, in.Archived)
	if err != nil {
		return errs.Internal(err, "create programme")
	}
	if tag.RowsAffected() == 0 {
		return db.ErrIDTakenElsewhere("programme")
	}
	return nil
}

// upsertChild runs one structural upsert and turns its failure modes into
// answers a trainer can act on.
//
// An upsert that affects no row hit an id that exists but belongs to another
// programme — or, invisible under row-level security, to another tenant —
// and quietly overwriting either would be wrong.
func upsertChild(ctx context.Context, tx pgx.Tx, what, sql string, args ...any) error {
	tag, err := tx.Exec(ctx, sql, args...)
	switch {
	case db.IsForeignKeyViolation(err):
		if what == "prescription" {
			return errs.NotFound("exercise")
		}
		return errs.NotFound("%s", what)
	case db.IsSQLState(err, "42501"):
		// Row-level security refused to update a row this tenant cannot see.
		return db.ErrIDTakenElsewhere(what)
	case err != nil:
		return errs.Internal(err, "save %s", what)
	case tag.RowsAffected() == 0:
		return db.ErrIDTakenElsewhere(what)
	}
	return nil
}

// validateDocument checks the whole document before anything is written, so
// a bad prescription on the last day refuses the save rather than leaving
// half a programme behind for the rollback to undo.
func validateDocument(in SaveProgramInput) error {
	seen := map[ids.ID]bool{}
	unique := func(id ids.ID, what string) error {
		if id == (ids.ID{}) {
			return errs.Invalid(errs.CodeValidation, "every %s needs an id", what).WithField("id", "is required")
		}
		if seen[id] {
			return errs.Invalid(errs.CodeValidation, "the same id appears twice in this programme").
				WithField("id", "must be unique within the programme")
		}
		seen[id] = true
		return nil
	}
	for _, b := range in.Blocks {
		if err := unique(b.ID, "block"); err != nil {
			return err
		}
		if strings.TrimSpace(b.Name) == "" {
			return errs.Invalid(errs.CodeValidation, "a block needs a name").WithField("name", "is required")
		}
		if b.Weeks < 1 || b.Weeks > 52 {
			return errs.Invalid(errs.CodeValidation, "a block runs between 1 and 52 weeks").
				WithField("weeks", "must be between 1 and 52")
		}
		for _, d := range b.Days {
			if err := unique(d.ID, "training day"); err != nil {
				return err
			}
			if strings.TrimSpace(d.Name) == "" {
				return errs.Invalid(errs.CodeValidation, "a training day needs a name").WithField("name", "is required")
			}
			for _, p := range d.Prescriptions {
				if err := unique(p.ID, "prescription"); err != nil {
					return err
				}
				if err := validatePrescription(p.TargetSets, p.TargetRepsMin, p.TargetRepsMax, p.TargetRPETenths, p.TargetRIR); err != nil {
					return err
				}
				if (p.TargetLoadGrams != nil && *p.TargetLoadGrams < 0) || (p.RestSeconds != nil && *p.RestSeconds < 0) {
					return errs.Invalid(errs.CodeValidation, "a load or rest cannot be negative").
						WithField("target_load_grams", "must be zero or more")
				}
			}
		}
	}
	return nil
}

// validatePrescription holds the rules for one prescribed exercise, shared by
// Prescribe and SaveProgram so the two paths cannot drift apart.
func validatePrescription(sets int, repsMin, repsMax, rpeTenths, rir *int) error {
	for _, v := range []*int{repsMin, repsMax} {
		if v != nil && *v < 0 {
			return errs.Invalid(errs.CodeValidation, "a rep target cannot be negative").
				WithField("target_reps_min", "must be zero or more")
		}
	}
	if sets > 50 {
		return errs.Invalid(errs.CodeValidation, "that is too many sets").
			WithField("target_sets", "must be between 1 and 50")
	}
	if repsMin != nil && repsMax != nil && *repsMax < *repsMin {
		return errs.Invalid(errs.CodeValidation, "the rep range is inverted").
			WithField("target_reps_max", "must not be below target_reps_min")
	}
	if rpeTenths != nil && (*rpeTenths < 0 || *rpeTenths > 100) {
		return errs.Invalid(errs.CodeValidation, "RPE is measured from 0 to 10").
			WithField("target_rpe_tenths", "must be between 0 and 100 tenths")
	}
	if rir != nil && (*rir < 0 || *rir > 10) {
		return errs.Invalid(errs.CodeValidation, "reps in reserve must be between 0 and 10").
			WithField("target_rir", "must be between 0 and 10")
	}
	return nil
}
