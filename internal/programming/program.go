package programming

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/NewMux/mtdrb_go/internal/db"
	"github.com/NewMux/mtdrb_go/internal/platform/errs"
	"github.com/NewMux/mtdrb_go/internal/platform/ids"
)

// The hierarchy mirrors how coaches talk: a programme is a macrocycle, divided
// into blocks (phases), each holding training days, each prescribing
// exercises. A four-week block repeats its days four times rather than storing
// twenty-eight copies of them — so editing week one's prescription edits the
// block, which is what the trainer means.

// Prescription is what the trainer asked for on one exercise.
type Prescription struct {
	ID           ids.ID `json:"id"`
	ExerciseID   ids.ID `json:"exercise_id"`
	ExerciseName string `json:"exercise_name,omitempty"`
	SortOrder    int    `json:"sort_order"`

	TargetSets      int  `json:"target_sets"`
	TargetRepsMin   *int `json:"target_reps_min,omitempty"`
	TargetRepsMax   *int `json:"target_reps_max,omitempty"`
	TargetLoadGrams *int `json:"target_load_grams,omitempty"`
	// TargetRPETenths is RPE in tenths: 85 means RPE 8.5.
	TargetRPETenths *int   `json:"target_rpe_tenths,omitempty"`
	TargetRIR       *int   `json:"target_rir,omitempty"`
	RestSeconds     *int   `json:"rest_seconds,omitempty"`
	Tempo           string `json:"tempo"`
	SupersetGroup   string `json:"superset_group"`
	Notes           string `json:"notes"`
}

// Day is one training session in a block.
type Day struct {
	ID            ids.ID         `json:"id"`
	Name          string         `json:"name"`
	SortOrder     int            `json:"sort_order"`
	Notes         string         `json:"notes"`
	Prescriptions []Prescription `json:"prescriptions"`
}

// Block is a phase of a programme.
type Block struct {
	ID        ids.ID `json:"id"`
	Name      string `json:"name"`
	Weeks     int    `json:"weeks"`
	SortOrder int    `json:"sort_order"`
	Notes     string `json:"notes"`
	Days      []Day  `json:"days"`
}

// Program is a complete macrocycle.
type Program struct {
	ID          ids.ID  `json:"id"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	IsTemplate  bool    `json:"is_template"`
	Blocks      []Block `json:"blocks"`
	// TotalWeeks is the sum of its blocks, so a client can be told how long
	// the plan runs without the app adding it up.
	TotalWeeks int   `json:"total_weeks"`
	ServerSeq  int64 `json:"server_seq"`
}

// CreateProgramInput describes a new programme.
type CreateProgramInput struct {
	Name        string
	Description string
	IsTemplate  bool
}

// CreateProgram creates an empty macrocycle.
func (s *Service) CreateProgram(ctx context.Context, tx pgx.Tx, tenantID ids.ID, in CreateProgramInput) (Program, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return Program{}, errs.Invalid(errs.CodeValidation, "a programme needs a name").
			WithField("name", "is required")
	}

	p := Program{
		ID: ids.New(), Name: name,
		Description: strings.TrimSpace(in.Description), IsTemplate: in.IsTemplate,
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO programs (id, tenant_id, name, description, is_template)
		VALUES ($1, $2, $3, $4, $5) RETURNING server_seq`,
		p.ID, tenantID, p.Name, p.Description, p.IsTemplate).Scan(&p.ServerSeq); err != nil {
		return Program{}, errs.Internal(err, "create programme")
	}
	return p, nil
}

// AddBlockInput describes a phase to append.
type AddBlockInput struct {
	ProgramID ids.ID
	Name      string
	Weeks     int
	Notes     string
}

// AddBlock appends a phase to a programme.
func (s *Service) AddBlock(ctx context.Context, tx pgx.Tx, tenantID ids.ID, in AddBlockInput) (Block, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return Block{}, errs.Invalid(errs.CodeValidation, "a block needs a name").
			WithField("name", "is required")
	}
	if in.Weeks <= 0 {
		in.Weeks = 1
	}
	if in.Weeks > 52 {
		return Block{}, errs.Invalid(errs.CodeValidation, "a block cannot run longer than 52 weeks").
			WithField("weeks", "must be between 1 and 52")
	}

	b := Block{ID: ids.New(), Name: name, Weeks: in.Weeks, Notes: strings.TrimSpace(in.Notes)}
	// The order is derived from what is already there, so a caller never has
	// to compute it and two blocks cannot claim the same position.
	if err := tx.QueryRow(ctx, `
		INSERT INTO program_blocks (id, tenant_id, program_id, name, weeks, sort_order, notes)
		VALUES ($1, $2, $3, $4, $5,
		        (SELECT coalesce(max(sort_order) + 1, 0) FROM program_blocks WHERE program_id = $3),
		        $6)
		RETURNING sort_order`,
		b.ID, tenantID, in.ProgramID, b.Name, b.Weeks, b.Notes).Scan(&b.SortOrder); err != nil {
		if db.IsForeignKeyViolation(err) {
			return Block{}, errs.NotFound("programme")
		}
		return Block{}, errs.Internal(err, "add block")
	}
	return b, nil
}

// AddDayInput describes a training day to append to a block.
type AddDayInput struct {
	BlockID ids.ID
	Name    string
	Notes   string
}

// AddDay appends a training day to a block.
func (s *Service) AddDay(ctx context.Context, tx pgx.Tx, tenantID ids.ID, in AddDayInput) (Day, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return Day{}, errs.Invalid(errs.CodeValidation, "a training day needs a name").
			WithField("name", "is required")
	}

	d := Day{ID: ids.New(), Name: name, Notes: strings.TrimSpace(in.Notes)}
	if err := tx.QueryRow(ctx, `
		INSERT INTO program_days (id, tenant_id, block_id, name, sort_order, notes)
		VALUES ($1, $2, $3, $4,
		        (SELECT coalesce(max(sort_order) + 1, 0) FROM program_days WHERE block_id = $3),
		        $5)
		RETURNING sort_order`,
		d.ID, tenantID, in.BlockID, d.Name, d.Notes).Scan(&d.SortOrder); err != nil {
		if db.IsForeignKeyViolation(err) {
			return Day{}, errs.NotFound("block")
		}
		return Day{}, errs.Internal(err, "add training day")
	}
	return d, nil
}

// PrescribeInput describes an exercise to prescribe on a day.
type PrescribeInput struct {
	DayID           ids.ID
	ExerciseID      ids.ID
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

// Prescribe adds an exercise to a training day.
func (s *Service) Prescribe(ctx context.Context, tx pgx.Tx, tenantID ids.ID, in PrescribeInput) (Prescription, error) {
	if in.TargetSets <= 0 {
		in.TargetSets = 3
	}
	if in.TargetSets > 50 {
		return Prescription{}, errs.Invalid(errs.CodeValidation, "that is too many sets").
			WithField("target_sets", "must be between 1 and 50")
	}
	if in.TargetRepsMin != nil && in.TargetRepsMax != nil && *in.TargetRepsMax < *in.TargetRepsMin {
		return Prescription{}, errs.Invalid(errs.CodeValidation,
			"the rep range is inverted").
			WithField("target_reps_max", "must not be below target_reps_min")
	}
	if in.TargetRPETenths != nil && (*in.TargetRPETenths < 0 || *in.TargetRPETenths > 100) {
		return Prescription{}, errs.Invalid(errs.CodeValidation,
			"RPE is measured from 0 to 10").
			WithField("target_rpe_tenths", "must be between 0 and 100 tenths")
	}
	if in.TargetRIR != nil && (*in.TargetRIR < 0 || *in.TargetRIR > 10) {
		return Prescription{}, errs.Invalid(errs.CodeValidation,
			"reps in reserve must be between 0 and 10").
			WithField("target_rir", "must be between 0 and 10")
	}

	p := Prescription{
		ID: ids.New(), ExerciseID: in.ExerciseID, TargetSets: in.TargetSets,
		TargetRepsMin: in.TargetRepsMin, TargetRepsMax: in.TargetRepsMax,
		TargetLoadGrams: in.TargetLoadGrams, TargetRPETenths: in.TargetRPETenths,
		TargetRIR: in.TargetRIR, RestSeconds: in.RestSeconds,
		Tempo:         strings.TrimSpace(in.Tempo),
		SupersetGroup: strings.TrimSpace(in.SupersetGroup),
		Notes:         strings.TrimSpace(in.Notes),
	}

	if err := tx.QueryRow(ctx, `
		INSERT INTO program_exercises
			(id, tenant_id, day_id, exercise_id, sort_order, target_sets,
			 target_reps_min, target_reps_max, target_load_grams, target_rpe_tenths,
			 target_rir, rest_seconds, tempo, superset_group, notes)
		VALUES ($1, $2, $3, $4,
		        (SELECT coalesce(max(sort_order) + 1, 0) FROM program_exercises WHERE day_id = $3),
		        $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		RETURNING sort_order`,
		p.ID, tenantID, in.DayID, in.ExerciseID, p.TargetSets,
		p.TargetRepsMin, p.TargetRepsMax, p.TargetLoadGrams, p.TargetRPETenths,
		p.TargetRIR, p.RestSeconds, p.Tempo, p.SupersetGroup, p.Notes,
	).Scan(&p.SortOrder); err != nil {
		if db.IsForeignKeyViolation(err) {
			return Prescription{}, errs.NotFound("training day or exercise")
		}
		return Prescription{}, errs.Internal(err, "prescribe exercise")
	}
	return p, nil
}

// GetProgram loads a programme with its full structure.
//
// Assembled in three queries rather than one join per level: a twelve-week
// programme has hundreds of prescriptions, and a nested loop of round trips is
// what makes a programme builder feel slow on a phone.
func (s *Service) GetProgram(ctx context.Context, tx pgx.Tx, programID ids.ID) (Program, error) {
	var p Program
	if err := tx.QueryRow(ctx, `
		SELECT id, name, description, is_template, server_seq
		  FROM programs WHERE id = $1 AND archived_at IS NULL`, programID,
	).Scan(&p.ID, &p.Name, &p.Description, &p.IsTemplate, &p.ServerSeq); err != nil {
		if db.IsNoRows(err) {
			return Program{}, errs.NotFound("programme")
		}
		return Program{}, errs.Internal(err, "load programme")
	}

	blockRows, err := tx.Query(ctx, `
		SELECT id, name, weeks, sort_order, notes
		  FROM program_blocks WHERE program_id = $1 ORDER BY sort_order`, programID)
	if err != nil {
		return Program{}, errs.Internal(err, "load blocks")
	}
	blockIndex := map[ids.ID]int{}
	for blockRows.Next() {
		var b Block
		if err := blockRows.Scan(&b.ID, &b.Name, &b.Weeks, &b.SortOrder, &b.Notes); err != nil {
			blockRows.Close()
			return Program{}, errs.Internal(err, "scan block")
		}
		blockIndex[b.ID] = len(p.Blocks)
		p.Blocks = append(p.Blocks, b)
		p.TotalWeeks += b.Weeks
	}
	blockRows.Close()
	if err := blockRows.Err(); err != nil {
		return Program{}, errs.Internal(err, "read blocks")
	}
	if len(p.Blocks) == 0 {
		return p, nil
	}

	blockIDs := make([]ids.ID, 0, len(p.Blocks))
	for _, b := range p.Blocks {
		blockIDs = append(blockIDs, b.ID)
	}

	dayRows, err := tx.Query(ctx, `
		SELECT id, block_id, name, sort_order, notes
		  FROM program_days WHERE block_id = ANY($1) ORDER BY sort_order`, blockIDs)
	if err != nil {
		return Program{}, errs.Internal(err, "load training days")
	}
	type dayRef struct{ block, day int }
	dayIndex := map[ids.ID]dayRef{}
	var dayIDs []ids.ID
	for dayRows.Next() {
		var d Day
		var blockID ids.ID
		if err := dayRows.Scan(&d.ID, &blockID, &d.Name, &d.SortOrder, &d.Notes); err != nil {
			dayRows.Close()
			return Program{}, errs.Internal(err, "scan training day")
		}
		bi := blockIndex[blockID]
		dayIndex[d.ID] = dayRef{block: bi, day: len(p.Blocks[bi].Days)}
		p.Blocks[bi].Days = append(p.Blocks[bi].Days, d)
		dayIDs = append(dayIDs, d.ID)
	}
	dayRows.Close()
	if err := dayRows.Err(); err != nil {
		return Program{}, errs.Internal(err, "read training days")
	}
	if len(dayIDs) == 0 {
		return p, nil
	}

	presRows, err := tx.Query(ctx, `
		SELECT pe.id, pe.day_id, pe.exercise_id, e.name, pe.sort_order, pe.target_sets,
		       pe.target_reps_min, pe.target_reps_max, pe.target_load_grams,
		       pe.target_rpe_tenths, pe.target_rir, pe.rest_seconds,
		       pe.tempo, pe.superset_group, pe.notes
		  FROM program_exercises pe
		  JOIN exercises e ON e.id = pe.exercise_id
		 WHERE pe.day_id = ANY($1) ORDER BY pe.sort_order`, dayIDs)
	if err != nil {
		return Program{}, errs.Internal(err, "load prescriptions")
	}
	defer presRows.Close()

	for presRows.Next() {
		var pr Prescription
		var dayID ids.ID
		if err := presRows.Scan(&pr.ID, &dayID, &pr.ExerciseID, &pr.ExerciseName, &pr.SortOrder,
			&pr.TargetSets, &pr.TargetRepsMin, &pr.TargetRepsMax, &pr.TargetLoadGrams,
			&pr.TargetRPETenths, &pr.TargetRIR, &pr.RestSeconds,
			&pr.Tempo, &pr.SupersetGroup, &pr.Notes); err != nil {
			return Program{}, errs.Internal(err, "scan prescription")
		}
		ref := dayIndex[dayID]
		p.Blocks[ref.block].Days[ref.day].Prescriptions =
			append(p.Blocks[ref.block].Days[ref.day].Prescriptions, pr)
	}
	if err := presRows.Err(); err != nil {
		return Program{}, errs.Internal(err, "read prescriptions")
	}
	return p, nil
}

// ListPrograms returns the tenant's programmes.
func (s *Service) ListPrograms(ctx context.Context, tx pgx.Tx, templatesOnly bool) ([]Program, error) {
	rows, err := tx.Query(ctx, `
		SELECT p.id, p.name, p.description, p.is_template, p.server_seq,
		       coalesce((SELECT sum(b.weeks) FROM program_blocks b WHERE b.program_id = p.id), 0)
		  FROM programs p
		 WHERE p.archived_at IS NULL AND (NOT $1 OR p.is_template)
		 ORDER BY p.name`, templatesOnly)
	if err != nil {
		return nil, errs.Internal(err, "list programmes")
	}
	defer rows.Close()

	var out []Program
	for rows.Next() {
		var p Program
		if err := rows.Scan(&p.ID, &p.Name, &p.Description, &p.IsTemplate,
			&p.ServerSeq, &p.TotalWeeks); err != nil {
			return nil, errs.Internal(err, "scan programme")
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, errs.Internal(err, "read programmes")
	}
	return out, nil
}

// DuplicateProgram deep-copies a programme, structure and all.
//
// This is how a template becomes a client's plan: the copy is independent, so
// tailoring one client's week three does not rewrite the template or anyone
// else's programme.
func (s *Service) DuplicateProgram(ctx context.Context, tx pgx.Tx, tenantID, sourceID ids.ID, newName string) (Program, error) {
	source, err := s.GetProgram(ctx, tx, sourceID)
	if err != nil {
		return Program{}, err
	}
	name := strings.TrimSpace(newName)
	if name == "" {
		name = source.Name + " (copy)"
	}

	copied, err := s.CreateProgram(ctx, tx, tenantID, CreateProgramInput{
		Name: name, Description: source.Description,
	})
	if err != nil {
		return Program{}, err
	}

	for _, b := range source.Blocks {
		newBlock, err := s.AddBlock(ctx, tx, tenantID, AddBlockInput{
			ProgramID: copied.ID, Name: b.Name, Weeks: b.Weeks, Notes: b.Notes,
		})
		if err != nil {
			return Program{}, err
		}
		for _, d := range b.Days {
			newDay, err := s.AddDay(ctx, tx, tenantID, AddDayInput{
				BlockID: newBlock.ID, Name: d.Name, Notes: d.Notes,
			})
			if err != nil {
				return Program{}, err
			}
			for _, pr := range d.Prescriptions {
				if _, err := s.Prescribe(ctx, tx, tenantID, PrescribeInput{
					DayID: newDay.ID, ExerciseID: pr.ExerciseID,
					TargetSets: pr.TargetSets, TargetRepsMin: pr.TargetRepsMin,
					TargetRepsMax: pr.TargetRepsMax, TargetLoadGrams: pr.TargetLoadGrams,
					TargetRPETenths: pr.TargetRPETenths, TargetRIR: pr.TargetRIR,
					RestSeconds: pr.RestSeconds, Tempo: pr.Tempo,
					SupersetGroup: pr.SupersetGroup, Notes: pr.Notes,
				}); err != nil {
					return Program{}, err
				}
			}
		}
	}
	return s.GetProgram(ctx, tx, copied.ID)
}

// Assignment ties a programme to a client for a date range.
type Assignment struct {
	ID          ids.ID     `json:"id"`
	ProgramID   ids.ID     `json:"program_id"`
	ProgramName string     `json:"program_name,omitempty"`
	ClientID    ids.ID     `json:"client_id"`
	StartsOn    time.Time  `json:"starts_on"`
	EndsOn      *time.Time `json:"ends_on,omitempty"`
	Status      string     `json:"status"`
	Notes       string     `json:"notes"`
	ServerSeq   int64      `json:"server_seq"`
}

// AssignInput describes assigning a programme.
type AssignInput struct {
	ProgramID ids.ID
	ClientID  ids.ID
	StartsOn  time.Time
	EndsOn    *time.Time
	Notes     string
}

// Assign puts a client on a programme.
//
// Any programme they were already following is completed first: a client
// follows one plan at a time, and a partial unique index enforces it, so
// replacing rather than colliding is what the trainer means by "put them on
// this now".
func (s *Service) Assign(ctx context.Context, tx pgx.Tx, tenantID ids.ID, in AssignInput) (Assignment, error) {
	if in.StartsOn.IsZero() {
		in.StartsOn = s.clock.Now()
	}
	startsOn := truncateToDay(in.StartsOn)
	if in.EndsOn != nil {
		e := truncateToDay(*in.EndsOn)
		if e.Before(startsOn) {
			return Assignment{}, errs.Invalid(errs.CodeValidation,
				"an assignment cannot end before it starts").
				WithField("ends_on", "must not be before starts_on")
		}
		in.EndsOn = &e
	}

	if _, err := tx.Exec(ctx,
		`UPDATE program_assignments SET status = 'completed'
		  WHERE client_id = $1 AND status = 'active'`, in.ClientID); err != nil {
		return Assignment{}, errs.Internal(err, "close previous assignment")
	}

	a := Assignment{
		ID: ids.New(), ProgramID: in.ProgramID, ClientID: in.ClientID,
		StartsOn: startsOn, EndsOn: in.EndsOn, Status: "active",
		Notes: strings.TrimSpace(in.Notes),
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO program_assignments
			(id, tenant_id, program_id, client_id, starts_on, ends_on, notes)
		VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING server_seq`,
		a.ID, tenantID, a.ProgramID, a.ClientID, a.StartsOn, a.EndsOn, a.Notes,
	).Scan(&a.ServerSeq); err != nil {
		if db.IsForeignKeyViolation(err) {
			return Assignment{}, errs.NotFound("programme or client")
		}
		return Assignment{}, errs.Internal(err, "assign programme")
	}
	return a, nil
}

// ActiveAssignment returns the programme a client is currently following.
func (s *Service) ActiveAssignment(ctx context.Context, tx pgx.Tx, clientID ids.ID) (Assignment, error) {
	var a Assignment
	if err := tx.QueryRow(ctx, `
		SELECT a.id, a.program_id, p.name, a.client_id, a.starts_on, a.ends_on,
		       a.status::text, a.notes, a.server_seq
		  FROM program_assignments a
		  JOIN programs p ON p.id = a.program_id
		 WHERE a.client_id = $1 AND a.status = 'active'`, clientID,
	).Scan(&a.ID, &a.ProgramID, &a.ProgramName, &a.ClientID, &a.StartsOn,
		&a.EndsOn, &a.Status, &a.Notes, &a.ServerSeq); err != nil {
		if db.IsNoRows(err) {
			return Assignment{}, errs.NotFound("active programme assignment")
		}
		return Assignment{}, errs.Internal(err, "load assignment")
	}
	return a, nil
}

func truncateToDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}
