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

// This is the gym-floor path: it runs one-handed, between sets, often with no
// signal. Everything here is built to be fast to call and safe to replay,
// because the offline outbox will replay it.

// WorkoutStatus is how far along a logged session is.
type WorkoutStatus string

const (
	WorkoutInProgress WorkoutStatus = "in_progress"
	WorkoutCompleted  WorkoutStatus = "completed"
	WorkoutSkipped    WorkoutStatus = "skipped"
)

// SetLog is one performed set.
type SetLog struct {
	ID                ids.ID  `json:"id"`
	ExerciseID        ids.ID  `json:"exercise_id"`
	ExerciseName      string  `json:"exercise_name,omitempty"`
	ProgramExerciseID *ids.ID `json:"program_exercise_id,omitempty"`
	SetIndex          int     `json:"set_index"`

	Reps        *int   `json:"reps,omitempty"`
	LoadGrams   *int   `json:"load_grams,omitempty"`
	RPETenths   *int   `json:"rpe_tenths,omitempty"`
	RIR         *int   `json:"rir,omitempty"`
	RestSeconds *int   `json:"rest_seconds,omitempty"`
	Tempo       string `json:"tempo"`
	IsWarmup    bool   `json:"is_warmup"`
	Completed   bool   `json:"completed"`
	Notes       string `json:"notes"`

	FormCheckMediaID *ids.ID `json:"form_check_media_id,omitempty"`
	ServerSeq        int64   `json:"server_seq"`
}

// Workout is a training session as performed.
type Workout struct {
	ID           ids.ID        `json:"id"`
	ClientID     ids.ID        `json:"client_id"`
	AssignmentID *ids.ID       `json:"assignment_id,omitempty"`
	DayID        *ids.ID       `json:"day_id,omitempty"`
	DayName      string        `json:"day_name,omitempty"`
	SessionID    *ids.ID       `json:"session_id,omitempty"`
	WeekNumber   int           `json:"week_number"`
	PerformedOn  time.Time     `json:"performed_on"`
	Status       WorkoutStatus `json:"status"`
	Notes        string        `json:"notes"`
	Sets         []SetLog      `json:"sets"`
	ServerSeq    int64         `json:"server_seq"`
}

// StartWorkoutInput describes a training session being logged.
type StartWorkoutInput struct {
	ClientID     ids.ID
	AssignmentID *ids.ID
	DayID        *ids.ID
	// SessionID ties the log to a calendar session when there is one. A client
	// training alone logs a workout with no session behind it.
	SessionID   *ids.ID
	WeekNumber  int
	PerformedOn time.Time
	Notes       string
}

// StartWorkout opens a session to log sets against.
func (s *Service) StartWorkout(ctx context.Context, tx pgx.Tx, tenantID ids.ID, in StartWorkoutInput) (Workout, error) {
	if in.WeekNumber <= 0 {
		in.WeekNumber = 1
	}
	if in.PerformedOn.IsZero() {
		in.PerformedOn = s.clock.Now()
	}
	performedOn := truncateToDay(in.PerformedOn)

	w := Workout{
		ID: ids.New(), ClientID: in.ClientID, AssignmentID: in.AssignmentID,
		DayID: in.DayID, SessionID: in.SessionID, WeekNumber: in.WeekNumber,
		PerformedOn: performedOn, Status: WorkoutInProgress,
		Notes: strings.TrimSpace(in.Notes),
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO workout_sessions
			(id, tenant_id, client_id, assignment_id, day_id, session_id,
			 week_number, performed_on, notes)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING server_seq`,
		w.ID, tenantID, w.ClientID, w.AssignmentID, w.DayID, w.SessionID,
		w.WeekNumber, w.PerformedOn, w.Notes,
	).Scan(&w.ServerSeq); err != nil {
		if db.IsForeignKeyViolation(err) {
			return Workout{}, errs.NotFound("client, assignment, training day or session")
		}
		return Workout{}, errs.Internal(err, "start workout")
	}
	return w, nil
}

// LogSetInput describes one performed set.
type LogSetInput struct {
	WorkoutID         ids.ID
	ExerciseID        ids.ID
	ProgramExerciseID *ids.ID
	SetIndex          int
	Reps              *int
	LoadGrams         *int
	RPETenths         *int
	RIR               *int
	RestSeconds       *int
	Tempo             string
	IsWarmup          bool
	Completed         *bool
	Notes             string
	FormCheckMediaID  *ids.ID
}

// LogSet records a set, replacing any previous record of that same set.
//
// Upserted on (workout, exercise, set index) rather than inserted, for two
// reasons: a trainer correcting the weight they just typed should not create a
// second set three, and the offline outbox will replay the same entry more
// than once. Replaying must converge on the same state, not accumulate.
func (s *Service) LogSet(ctx context.Context, tx pgx.Tx, tenantID ids.ID, in LogSetInput) (SetLog, error) {
	if in.SetIndex <= 0 {
		return SetLog{}, errs.Invalid(errs.CodeValidation, "a set needs a positive index").
			WithField("set_index", "must be greater than zero")
	}
	if in.Reps != nil && *in.Reps < 0 {
		return SetLog{}, errs.Invalid(errs.CodeValidation, "reps cannot be negative").
			WithField("reps", "must not be negative")
	}
	if in.LoadGrams != nil && *in.LoadGrams < 0 {
		return SetLog{}, errs.Invalid(errs.CodeValidation, "load cannot be negative").
			WithField("load_grams", "must not be negative")
	}
	if in.RPETenths != nil && (*in.RPETenths < 0 || *in.RPETenths > 100) {
		return SetLog{}, errs.Invalid(errs.CodeValidation, "RPE is measured from 0 to 10").
			WithField("rpe_tenths", "must be between 0 and 100 tenths")
	}
	if in.RIR != nil && (*in.RIR < 0 || *in.RIR > 10) {
		return SetLog{}, errs.Invalid(errs.CodeValidation, "reps in reserve must be between 0 and 10").
			WithField("rir", "must be between 0 and 10")
	}

	completed := true
	if in.Completed != nil {
		completed = *in.Completed
	}

	log := SetLog{
		ID: ids.New(), ExerciseID: in.ExerciseID, ProgramExerciseID: in.ProgramExerciseID,
		SetIndex: in.SetIndex, Reps: in.Reps, LoadGrams: in.LoadGrams,
		RPETenths: in.RPETenths, RIR: in.RIR, RestSeconds: in.RestSeconds,
		Tempo: strings.TrimSpace(in.Tempo), IsWarmup: in.IsWarmup,
		Completed: completed, Notes: strings.TrimSpace(in.Notes),
		FormCheckMediaID: in.FormCheckMediaID,
	}

	if err := tx.QueryRow(ctx, `
		INSERT INTO set_logs
			(id, tenant_id, workout_session_id, exercise_id, program_exercise_id, set_index,
			 reps, load_grams, rpe_tenths, rir, rest_seconds, tempo, is_warmup, completed,
			 notes, form_check_media_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
		ON CONFLICT (workout_session_id, exercise_id, set_index) DO UPDATE SET
			program_exercise_id = EXCLUDED.program_exercise_id,
			reps = EXCLUDED.reps, load_grams = EXCLUDED.load_grams,
			rpe_tenths = EXCLUDED.rpe_tenths, rir = EXCLUDED.rir,
			rest_seconds = EXCLUDED.rest_seconds, tempo = EXCLUDED.tempo,
			is_warmup = EXCLUDED.is_warmup, completed = EXCLUDED.completed,
			notes = EXCLUDED.notes,
			-- A form-check clip is only ever added, never cleared by a later
			-- correction to the numbers.
			form_check_media_id = coalesce(EXCLUDED.form_check_media_id, set_logs.form_check_media_id)
		RETURNING id, server_seq`,
		log.ID, tenantID, in.WorkoutID, in.ExerciseID, in.ProgramExerciseID, in.SetIndex,
		log.Reps, log.LoadGrams, log.RPETenths, log.RIR, log.RestSeconds, log.Tempo,
		log.IsWarmup, log.Completed, log.Notes, log.FormCheckMediaID,
	).Scan(&log.ID, &log.ServerSeq); err != nil {
		if db.IsForeignKeyViolation(err) {
			return SetLog{}, errs.NotFound("workout, exercise or media object")
		}
		return SetLog{}, errs.Internal(err, "log set")
	}
	return log, nil
}

// CompleteWorkout marks a logged session finished.
func (s *Service) CompleteWorkout(ctx context.Context, tx pgx.Tx, workoutID ids.ID, notes string) (Workout, error) {
	tag, err := tx.Exec(ctx, `
		UPDATE workout_sessions
		   SET status = 'completed',
		       notes = CASE WHEN $2 = '' THEN notes ELSE $2 END
		 WHERE id = $1 AND deleted_at IS NULL`, workoutID, strings.TrimSpace(notes))
	if err != nil {
		return Workout{}, errs.Internal(err, "complete workout")
	}
	if tag.RowsAffected() == 0 {
		return Workout{}, errs.NotFound("workout")
	}
	return s.GetWorkout(ctx, tx, workoutID)
}

// GetWorkout loads a logged session with its sets.
func (s *Service) GetWorkout(ctx context.Context, tx pgx.Tx, workoutID ids.ID) (Workout, error) {
	var w Workout
	var status string
	var dayName *string
	if err := tx.QueryRow(ctx, `
		SELECT ws.id, ws.client_id, ws.assignment_id, ws.day_id, pd.name,
		       ws.session_id, ws.week_number, ws.performed_on, ws.status::text,
		       ws.notes, ws.server_seq
		  FROM workout_sessions ws
		  LEFT JOIN program_days pd ON pd.id = ws.day_id
		 WHERE ws.id = $1 AND ws.deleted_at IS NULL`, workoutID,
	).Scan(&w.ID, &w.ClientID, &w.AssignmentID, &w.DayID, &dayName, &w.SessionID,
		&w.WeekNumber, &w.PerformedOn, &status, &w.Notes, &w.ServerSeq); err != nil {
		if db.IsNoRows(err) {
			return Workout{}, errs.NotFound("workout")
		}
		return Workout{}, errs.Internal(err, "load workout")
	}
	w.Status = WorkoutStatus(status)
	if dayName != nil {
		w.DayName = *dayName
	}

	sets, err := s.setsFor(ctx, tx, workoutID)
	if err != nil {
		return Workout{}, err
	}
	w.Sets = sets
	return w, nil
}

func (s *Service) setsFor(ctx context.Context, tx pgx.Tx, workoutID ids.ID) ([]SetLog, error) {
	rows, err := tx.Query(ctx, `
		SELECT sl.id, sl.exercise_id, e.name, sl.program_exercise_id, sl.set_index,
		       sl.reps, sl.load_grams, sl.rpe_tenths, sl.rir, sl.rest_seconds,
		       sl.tempo, sl.is_warmup, sl.completed, sl.notes,
		       sl.form_check_media_id, sl.server_seq
		  FROM set_logs sl
		  JOIN exercises e ON e.id = sl.exercise_id
		 WHERE sl.workout_session_id = $1
		 ORDER BY e.name, sl.set_index`, workoutID)
	if err != nil {
		return nil, errs.Internal(err, "load sets")
	}
	defer rows.Close()

	var out []SetLog
	for rows.Next() {
		var l SetLog
		if err := rows.Scan(&l.ID, &l.ExerciseID, &l.ExerciseName, &l.ProgramExerciseID,
			&l.SetIndex, &l.Reps, &l.LoadGrams, &l.RPETenths, &l.RIR, &l.RestSeconds,
			&l.Tempo, &l.IsWarmup, &l.Completed, &l.Notes,
			&l.FormCheckMediaID, &l.ServerSeq); err != nil {
			return nil, errs.Internal(err, "scan set")
		}
		out = append(out, l)
	}
	if err := rows.Err(); err != nil {
		return nil, errs.Internal(err, "read sets")
	}
	return out, nil
}

// PreviousPerformance is what a client did last time on one exercise.
type PreviousPerformance struct {
	ExerciseID  ids.ID    `json:"exercise_id"`
	WorkoutID   ids.ID    `json:"workout_id"`
	PerformedOn time.Time `json:"performed_on"`
	Sets        []SetLog  `json:"sets"`
}

// PreviousPerformanceFor returns the last time this client trained this
// exercise.
//
// This is the PRD's single-tap cloning: the app prefills today's sets with
// last time's numbers so the trainer adjusts rather than types. It deliberately
// looks at the last workout where the lift was *actually performed*, skipping
// sessions where it was prescribed and not done — prefilling from a blank is
// worse than prefilling from nothing.
func (s *Service) PreviousPerformanceFor(ctx context.Context, tx pgx.Tx, clientID, exerciseID ids.ID, beforeWorkout *ids.ID) (PreviousPerformance, error) {
	var result PreviousPerformance
	result.ExerciseID = exerciseID

	// Find the most recent workout containing a completed set of this lift,
	// excluding the one in progress.
	err := tx.QueryRow(ctx, `
		SELECT ws.id, ws.performed_on
		  FROM workout_sessions ws
		  JOIN set_logs sl ON sl.workout_session_id = ws.id
		 WHERE ws.client_id = $1
		   AND sl.exercise_id = $2
		   AND sl.completed AND NOT sl.is_warmup
		   AND ws.deleted_at IS NULL
		   AND ($3::uuid IS NULL OR ws.id <> $3)
		 ORDER BY ws.performed_on DESC, ws.created_at DESC
		 LIMIT 1`, clientID, exerciseID, beforeWorkout,
	).Scan(&result.WorkoutID, &result.PerformedOn)
	if err != nil {
		if db.IsNoRows(err) {
			// A first-time lift is not an error; the app simply has nothing
			// to prefill.
			return PreviousPerformance{ExerciseID: exerciseID}, nil
		}
		return PreviousPerformance{}, errs.Internal(err, "find previous performance")
	}

	rows, err := tx.Query(ctx, `
		SELECT sl.id, sl.exercise_id, e.name, sl.program_exercise_id, sl.set_index,
		       sl.reps, sl.load_grams, sl.rpe_tenths, sl.rir, sl.rest_seconds,
		       sl.tempo, sl.is_warmup, sl.completed, sl.notes,
		       sl.form_check_media_id, sl.server_seq
		  FROM set_logs sl
		  JOIN exercises e ON e.id = sl.exercise_id
		 WHERE sl.workout_session_id = $1 AND sl.exercise_id = $2
		 ORDER BY sl.set_index`, result.WorkoutID, exerciseID)
	if err != nil {
		return PreviousPerformance{}, errs.Internal(err, "load previous sets")
	}
	defer rows.Close()

	for rows.Next() {
		var l SetLog
		if err := rows.Scan(&l.ID, &l.ExerciseID, &l.ExerciseName, &l.ProgramExerciseID,
			&l.SetIndex, &l.Reps, &l.LoadGrams, &l.RPETenths, &l.RIR, &l.RestSeconds,
			&l.Tempo, &l.IsWarmup, &l.Completed, &l.Notes,
			&l.FormCheckMediaID, &l.ServerSeq); err != nil {
			return PreviousPerformance{}, errs.Internal(err, "scan previous set")
		}
		result.Sets = append(result.Sets, l)
	}
	if err := rows.Err(); err != nil {
		return PreviousPerformance{}, errs.Internal(err, "read previous sets")
	}
	return result, nil
}

// CloneResult reports what a clone produced.
type CloneResult struct {
	WorkoutID ids.ID   `json:"workout_id"`
	Sets      []SetLog `json:"sets"`
	// ClonedFrom is the workout the numbers came from, so the app can say
	// "same as 12 March" rather than presenting them as new.
	ClonedFrom *ids.ID `json:"cloned_from,omitempty"`
}

// ClonePreviousSets copies last session's numbers into the current workout.
//
// The single tap from the PRD. The sets are written as logged-but-not-completed
// so the trainer confirms each one as it is actually done — prefilled numbers
// that silently count as performed would put lifts in a client's history that
// never happened.
func (s *Service) ClonePreviousSets(ctx context.Context, tx pgx.Tx, tenantID, workoutID, exerciseID ids.ID) (CloneResult, error) {
	var clientID ids.ID
	if err := tx.QueryRow(ctx,
		`SELECT client_id FROM workout_sessions WHERE id = $1 AND deleted_at IS NULL`,
		workoutID).Scan(&clientID); err != nil {
		if db.IsNoRows(err) {
			return CloneResult{}, errs.NotFound("workout")
		}
		return CloneResult{}, errs.Internal(err, "load workout")
	}

	previous, err := s.PreviousPerformanceFor(ctx, tx, clientID, exerciseID, &workoutID)
	if err != nil {
		return CloneResult{}, err
	}
	if len(previous.Sets) == 0 {
		return CloneResult{}, errs.NotFound("previous performance of this exercise")
	}

	result := CloneResult{WorkoutID: workoutID, ClonedFrom: &previous.WorkoutID}
	notCompleted := false
	for _, prev := range previous.Sets {
		if prev.IsWarmup {
			continue
		}
		logged, err := s.LogSet(ctx, tx, tenantID, LogSetInput{
			WorkoutID:   workoutID,
			ExerciseID:  exerciseID,
			SetIndex:    prev.SetIndex,
			Reps:        prev.Reps,
			LoadGrams:   prev.LoadGrams,
			RPETenths:   prev.RPETenths,
			RIR:         prev.RIR,
			RestSeconds: prev.RestSeconds,
			Tempo:       prev.Tempo,
			Completed:   &notCompleted,
		})
		if err != nil {
			return CloneResult{}, err
		}
		result.Sets = append(result.Sets, logged)
	}
	return result, nil
}

// WorkoutHistory returns a client's recent logged sessions, newest first.
func (s *Service) WorkoutHistory(ctx context.Context, tx pgx.Tx, clientID ids.ID, limit int) ([]Workout, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := tx.Query(ctx, `
		SELECT ws.id, ws.client_id, ws.assignment_id, ws.day_id, coalesce(pd.name, ''),
		       ws.session_id, ws.week_number, ws.performed_on, ws.status::text,
		       ws.notes, ws.server_seq
		  FROM workout_sessions ws
		  LEFT JOIN program_days pd ON pd.id = ws.day_id
		 WHERE ws.client_id = $1 AND ws.deleted_at IS NULL
		 ORDER BY ws.performed_on DESC, ws.created_at DESC
		 LIMIT $2`, clientID, limit)
	if err != nil {
		return nil, errs.Internal(err, "load workout history")
	}
	defer rows.Close()

	var out []Workout
	for rows.Next() {
		var w Workout
		var status string
		if err := rows.Scan(&w.ID, &w.ClientID, &w.AssignmentID, &w.DayID, &w.DayName,
			&w.SessionID, &w.WeekNumber, &w.PerformedOn, &status, &w.Notes, &w.ServerSeq); err != nil {
			return nil, errs.Internal(err, "scan workout")
		}
		w.Status = WorkoutStatus(status)
		out = append(out, w)
	}
	if err := rows.Err(); err != nil {
		return nil, errs.Internal(err, "read workout history")
	}
	return out, nil
}

// VolumePoint is one session's working volume for an exercise.
type VolumePoint struct {
	PerformedOn time.Time `json:"performed_on"`
	// Volume is load times reps summed over working sets, in gram-reps. The
	// single number coaches watch for progression.
	VolumeGramReps int64 `json:"volume_gram_reps"`
	TopSetGrams    *int  `json:"top_set_grams,omitempty"`
	WorkingSets    int   `json:"working_sets"`
}

// ExerciseProgression returns a client's volume history for one lift.
//
// Warm-ups and uncompleted sets are excluded: they are work that happened, but
// counting them would make a deload look like a personal best.
func (s *Service) ExerciseProgression(ctx context.Context, tx pgx.Tx, clientID, exerciseID ids.ID, limit int) ([]VolumePoint, error) {
	if limit <= 0 || limit > 200 {
		limit = 30
	}
	rows, err := tx.Query(ctx, `
		SELECT ws.performed_on,
		       coalesce(sum(coalesce(sl.load_grams, 0)::bigint * coalesce(sl.reps, 0)), 0),
		       max(sl.load_grams),
		       count(*)
		  FROM workout_sessions ws
		  JOIN set_logs sl ON sl.workout_session_id = ws.id
		 WHERE ws.client_id = $1 AND sl.exercise_id = $2
		   AND sl.completed AND NOT sl.is_warmup AND ws.deleted_at IS NULL
		 GROUP BY ws.id, ws.performed_on
		 ORDER BY ws.performed_on DESC
		 LIMIT $3`, clientID, exerciseID, limit)
	if err != nil {
		return nil, errs.Internal(err, "load progression")
	}
	defer rows.Close()

	var out []VolumePoint
	for rows.Next() {
		var p VolumePoint
		if err := rows.Scan(&p.PerformedOn, &p.VolumeGramReps, &p.TopSetGrams, &p.WorkingSets); err != nil {
			return nil, errs.Internal(err, "scan progression point")
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, errs.Internal(err, "read progression")
	}
	return out, nil
}
