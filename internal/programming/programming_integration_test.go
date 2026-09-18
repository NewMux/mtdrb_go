//go:build integration

package programming_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/NewMux/mtdrb_go/internal/crm"
	"github.com/NewMux/mtdrb_go/internal/db"
	"github.com/NewMux/mtdrb_go/internal/platform/clock"
	"github.com/NewMux/mtdrb_go/internal/platform/errs"
	"github.com/NewMux/mtdrb_go/internal/platform/ids"
	"github.com/NewMux/mtdrb_go/internal/programming"
	"github.com/NewMux/mtdrb_go/internal/testsupport"
)

var apr1 = time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)

type fixture struct {
	svc      *programming.Service
	crm      *crm.Service
	pool     *db.Pool
	tenantID ids.ID
	clock    *clock.Fixed
}

func setup(t *testing.T) fixture {
	t.Helper()
	testsupport.RequireDB(t)
	testsupport.Reset(t)

	pool := testsupport.OpenApp(t)
	c := &clock.Fixed{T: apr1}
	tenantID := ids.New()
	ctx := context.Background()

	owner := testsupport.OpenOwner(t)
	if _, err := owner.Raw().Exec(ctx,
		`INSERT INTO tenants (id, name, default_currency) VALUES ($1, 'Iron Works', 'EUR')`,
		tenantID); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}

	f := fixture{
		svc:      programming.NewService(c),
		crm:      crm.NewService(c, []byte("test-column-encryption-key-32byt")),
		pool:     pool,
		tenantID: tenantID,
		clock:    c,
	}
	if err := f.tx(t, func(tx pgx.Tx) error {
		return f.svc.SeedLibrary(ctx, tx, tenantID)
	}); err != nil {
		t.Fatalf("seed library: %v", err)
	}
	return f
}

func (f fixture) tx(t *testing.T, fn func(tx pgx.Tx) error) error {
	t.Helper()
	return f.pool.InTenantTx(context.Background(), f.tenantID, fn)
}

func (f fixture) newClient(t *testing.T, name string) ids.ID {
	t.Helper()
	var id ids.ID
	if err := f.tx(t, func(tx pgx.Tx) error {
		c, err := f.crm.Create(context.Background(), tx, f.tenantID, crm.CreateInput{FullName: name})
		if err != nil {
			return err
		}
		id = c.ID
		return nil
	}); err != nil {
		t.Fatalf("create client: %v", err)
	}
	return id
}

// exerciseNamed finds a seeded lift by name.
func (f fixture) exerciseNamed(t *testing.T, name string) ids.ID {
	t.Helper()
	var id ids.ID
	if err := f.tx(t, func(tx pgx.Tx) error {
		list, err := f.svc.ListExercises(context.Background(), tx, programming.ExerciseFilter{Search: name})
		if err != nil {
			return err
		}
		for _, e := range list {
			if e.Name == name {
				id = e.ID
				return nil
			}
		}
		t.Fatalf("seeded library has no exercise named %q", name)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return id
}

func ptr[T any](v T) *T { return &v }

// ---------------------------------------------------------------------------
// Library
// ---------------------------------------------------------------------------

func TestSeededLibraryCoversEveryMovementPattern(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	if err := f.tx(t, func(tx pgx.Tx) error {
		all, err := f.svc.ListExercises(ctx, tx, programming.ExerciseFilter{Limit: 500})
		if err != nil {
			return err
		}
		if len(all) < 50 {
			t.Errorf("seeded %d exercises; too thin to build a real programme from", len(all))
		}

		// A trainer must be able to build a balanced session on day one, so
		// every pattern needs at least one option.
		seen := map[programming.Category]int{}
		for _, e := range all {
			seen[e.Category]++
			if e.IsCustom {
				t.Errorf("%s was seeded but marked as custom", e.Name)
			}
		}
		for _, c := range []programming.Category{
			programming.CategorySquat, programming.CategoryHinge, programming.CategoryLunge,
			programming.CategoryHorizontalPush, programming.CategoryVerticalPush,
			programming.CategoryHorizontalPull, programming.CategoryVerticalPull,
			programming.CategoryCarry, programming.CategoryCore,
			programming.CategoryOlympic, programming.CategoryConditioning,
			programming.CategoryMobility,
		} {
			if seen[c] == 0 {
				t.Errorf("no seeded exercise for the %s pattern", c)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestLibraryFiltersByCategoryAndSearch(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	if err := f.tx(t, func(tx pgx.Tx) error {
		squats, err := f.svc.ListExercises(ctx, tx, programming.ExerciseFilter{
			Category: programming.CategorySquat,
		})
		if err != nil {
			return err
		}
		if len(squats) == 0 {
			t.Fatal("category filter returned nothing")
		}
		for _, e := range squats {
			if e.Category != programming.CategorySquat {
				t.Errorf("%s is %s, not a squat", e.Name, e.Category)
			}
		}

		// Search covers equipment too, because "what can I do with a
		// kettlebell" is a real question on a gym floor.
		kettlebell, err := f.svc.ListExercises(ctx, tx, programming.ExerciseFilter{Search: "kettlebell"})
		if err != nil {
			return err
		}
		if len(kettlebell) == 0 {
			t.Error("searching by equipment returned nothing")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestCustomExerciseAndVideoAllowlist(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	if err := f.tx(t, func(tx pgx.Tx) error {
		e, err := f.svc.CreateExercise(ctx, tx, f.tenantID, programming.CreateExerciseInput{
			Name: "Zercher Squat", Category: programming.CategorySquat,
			PrimaryMuscle: "Quadriceps", Equipment: "Barbell",
			VideoURL: "https://www.youtube.com/watch?v=abc123",
		})
		if err != nil {
			return err
		}
		if !e.IsCustom {
			t.Error("a trainer's own exercise should be marked custom")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// The link is rendered into an app that will follow it, so it is
	// allowlisted rather than merely format-checked.
	rejected := []string{
		"https://evil.example.com/payload",
		"javascript:alert(1)",
		"http://youtube.com/watch?v=x", // not https
		"file:///etc/passwd",
	}
	for _, url := range rejected {
		err := f.tx(t, func(tx pgx.Tx) error {
			_, err := f.svc.CreateExercise(ctx, tx, f.tenantID, programming.CreateExerciseInput{
				Name: "Bad " + url, VideoURL: url,
			})
			return err
		})
		if err == nil {
			t.Errorf("accepted a demonstration link of %q", url)
		}
	}

	// Duplicates are refused.
	err := f.tx(t, func(tx pgx.Tx) error {
		_, err := f.svc.CreateExercise(ctx, tx, f.tenantID, programming.CreateExerciseInput{
			Name: "back squat", // the seeded library already has this
		})
		return err
	})
	if err == nil {
		t.Fatal("a duplicate exercise name was accepted")
	}
	if errs.KindOf(err) != errs.KindConflict {
		t.Errorf("kind = %q, want conflict", errs.KindOf(err))
	}
}

// ---------------------------------------------------------------------------
// Programme builder
// ---------------------------------------------------------------------------

func TestBuildMesocycleAndReadItBack(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	squat := f.exerciseNamed(t, "Back Squat")
	bench := f.exerciseNamed(t, "Bench Press")
	row := f.exerciseNamed(t, "Barbell Row")

	var programID ids.ID
	if err := f.tx(t, func(tx pgx.Tx) error {
		program, err := f.svc.CreateProgram(ctx, tx, f.tenantID, programming.CreateProgramInput{
			Name: "12-Week Strength", Description: "Linear into hypertrophy",
		})
		if err != nil {
			return err
		}
		programID = program.ID

		// Two blocks, four weeks each.
		accumulation, err := f.svc.AddBlock(ctx, tx, f.tenantID, programming.AddBlockInput{
			ProgramID: program.ID, Name: "Accumulation", Weeks: 4,
		})
		if err != nil {
			return err
		}
		intensification, err := f.svc.AddBlock(ctx, tx, f.tenantID, programming.AddBlockInput{
			ProgramID: program.ID, Name: "Intensification", Weeks: 4,
		})
		if err != nil {
			return err
		}
		if accumulation.SortOrder != 0 || intensification.SortOrder != 1 {
			t.Errorf("block order = %d, %d; want 0, 1", accumulation.SortOrder, intensification.SortOrder)
		}

		lower, err := f.svc.AddDay(ctx, tx, f.tenantID, programming.AddDayInput{
			BlockID: accumulation.ID, Name: "Day 1 — Lower",
		})
		if err != nil {
			return err
		}
		upper, err := f.svc.AddDay(ctx, tx, f.tenantID, programming.AddDayInput{
			BlockID: accumulation.ID, Name: "Day 2 — Upper",
		})
		if err != nil {
			return err
		}

		if _, err := f.svc.Prescribe(ctx, tx, f.tenantID, programming.PrescribeInput{
			DayID: lower.ID, ExerciseID: squat,
			TargetSets: 4, TargetRepsMin: ptr(6), TargetRepsMax: ptr(8),
			TargetRPETenths: ptr(80), RestSeconds: ptr(180), Tempo: "3-1-1-0",
		}); err != nil {
			return err
		}
		if _, err := f.svc.Prescribe(ctx, tx, f.tenantID, programming.PrescribeInput{
			DayID: upper.ID, ExerciseID: bench,
			TargetSets: 4, TargetRepsMin: ptr(6), TargetRepsMax: ptr(8),
			SupersetGroup: "A",
		}); err != nil {
			return err
		}
		_, err = f.svc.Prescribe(ctx, tx, f.tenantID, programming.PrescribeInput{
			DayID: upper.ID, ExerciseID: row,
			TargetSets: 4, TargetRepsMin: ptr(8), TargetRepsMax: ptr(12),
			SupersetGroup: "A",
		})
		return err
	}); err != nil {
		t.Fatalf("build programme: %v", err)
	}

	if err := f.tx(t, func(tx pgx.Tx) error {
		program, err := f.svc.GetProgram(ctx, tx, programID)
		if err != nil {
			return err
		}
		if program.TotalWeeks != 8 {
			t.Errorf("total weeks = %d, want 8", program.TotalWeeks)
		}
		if len(program.Blocks) != 2 {
			t.Fatalf("got %d blocks, want 2", len(program.Blocks))
		}
		if program.Blocks[0].Name != "Accumulation" {
			t.Errorf("blocks are out of order: %q first", program.Blocks[0].Name)
		}
		if len(program.Blocks[0].Days) != 2 {
			t.Fatalf("first block has %d days, want 2", len(program.Blocks[0].Days))
		}

		lower := program.Blocks[0].Days[0]
		if len(lower.Prescriptions) != 1 {
			t.Fatalf("lower day has %d prescriptions, want 1", len(lower.Prescriptions))
		}
		pr := lower.Prescriptions[0]
		if pr.ExerciseName != "Back Squat" {
			t.Errorf("prescription names %q", pr.ExerciseName)
		}
		if pr.TargetSets != 4 || *pr.TargetRepsMin != 6 || *pr.TargetRepsMax != 8 {
			t.Errorf("prescription = %+v", pr)
		}
		if *pr.TargetRPETenths != 80 {
			t.Errorf("RPE = %v tenths, want 80 (RPE 8.0)", pr.TargetRPETenths)
		}
		if pr.Tempo != "3-1-1-0" {
			t.Errorf("tempo = %q", pr.Tempo)
		}

		// The superset pairing survives.
		upper := program.Blocks[0].Days[1]
		if len(upper.Prescriptions) != 2 {
			t.Fatalf("upper day has %d prescriptions, want 2", len(upper.Prescriptions))
		}
		for _, p := range upper.Prescriptions {
			if p.SupersetGroup != "A" {
				t.Errorf("%s lost its superset group", p.ExerciseName)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestPrescriptionValidation(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	squat := f.exerciseNamed(t, "Back Squat")

	var dayID ids.ID
	if err := f.tx(t, func(tx pgx.Tx) error {
		program, err := f.svc.CreateProgram(ctx, tx, f.tenantID, programming.CreateProgramInput{Name: "P"})
		if err != nil {
			return err
		}
		block, err := f.svc.AddBlock(ctx, tx, f.tenantID, programming.AddBlockInput{
			ProgramID: program.ID, Name: "B", Weeks: 1,
		})
		if err != nil {
			return err
		}
		day, err := f.svc.AddDay(ctx, tx, f.tenantID, programming.AddDayInput{BlockID: block.ID, Name: "D"})
		if err != nil {
			return err
		}
		dayID = day.ID
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	cases := map[string]programming.PrescribeInput{
		"inverted rep range": {DayID: dayID, ExerciseID: squat, TargetRepsMin: ptr(12), TargetRepsMax: ptr(8)},
		"rpe above 10":       {DayID: dayID, ExerciseID: squat, TargetRPETenths: ptr(120)},
		"negative rpe":       {DayID: dayID, ExerciseID: squat, TargetRPETenths: ptr(-10)},
		"rir above 10":       {DayID: dayID, ExerciseID: squat, TargetRIR: ptr(15)},
		"absurd set count":   {DayID: dayID, ExerciseID: squat, TargetSets: 100},
	}
	for name, in := range cases {
		err := f.tx(t, func(tx pgx.Tx) error {
			_, err := f.svc.Prescribe(ctx, tx, f.tenantID, in)
			return err
		})
		if err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// A template becomes a client's plan by being copied, so tailoring one client
// never rewrites the template or anyone else's programme.
func TestDuplicateProgramIsIndependent(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	squat := f.exerciseNamed(t, "Back Squat")

	var templateID, copyID ids.ID
	if err := f.tx(t, func(tx pgx.Tx) error {
		template, err := f.svc.CreateProgram(ctx, tx, f.tenantID, programming.CreateProgramInput{
			Name: "Beginner Template", IsTemplate: true,
		})
		if err != nil {
			return err
		}
		templateID = template.ID
		block, err := f.svc.AddBlock(ctx, tx, f.tenantID, programming.AddBlockInput{
			ProgramID: template.ID, Name: "Base", Weeks: 4,
		})
		if err != nil {
			return err
		}
		day, err := f.svc.AddDay(ctx, tx, f.tenantID, programming.AddDayInput{
			BlockID: block.ID, Name: "Full Body",
		})
		if err != nil {
			return err
		}
		_, err = f.svc.Prescribe(ctx, tx, f.tenantID, programming.PrescribeInput{
			DayID: day.ID, ExerciseID: squat, TargetSets: 3, TargetRepsMin: ptr(10),
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	if err := f.tx(t, func(tx pgx.Tx) error {
		copied, err := f.svc.DuplicateProgram(ctx, tx, f.tenantID, templateID, "Dana's Plan")
		if err != nil {
			return err
		}
		copyID = copied.ID

		if copied.Name != "Dana's Plan" {
			t.Errorf("copy name = %q", copied.Name)
		}
		if copied.IsTemplate {
			t.Error("a copy of a template should not itself be a template")
		}
		if copied.TotalWeeks != 4 || len(copied.Blocks) != 1 {
			t.Fatalf("copy structure = %d weeks, %d blocks", copied.TotalWeeks, len(copied.Blocks))
		}
		if len(copied.Blocks[0].Days[0].Prescriptions) != 1 {
			t.Fatal("the copy lost its prescriptions")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// Editing the copy must not touch the template.
	if err := f.tx(t, func(tx pgx.Tx) error {
		copied, err := f.svc.GetProgram(ctx, tx, copyID)
		if err != nil {
			return err
		}
		day := copied.Blocks[0].Days[0]
		if _, err := f.svc.Prescribe(ctx, tx, f.tenantID, programming.PrescribeInput{
			DayID: day.ID, ExerciseID: f.exerciseNamed(t, "Bench Press"), TargetSets: 3,
		}); err != nil {
			return err
		}

		template, err := f.svc.GetProgram(ctx, tx, templateID)
		if err != nil {
			return err
		}
		if got := len(template.Blocks[0].Days[0].Prescriptions); got != 1 {
			t.Errorf("editing the copy changed the template: %d prescriptions", got)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// A client follows one plan at a time; assigning a new one closes the old.
func TestAssigningReplacesTheActiveProgramme(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	clientID := f.newClient(t, "Dana")

	var firstID, secondID ids.ID
	if err := f.tx(t, func(tx pgx.Tx) error {
		first, err := f.svc.CreateProgram(ctx, tx, f.tenantID, programming.CreateProgramInput{Name: "Phase 1"})
		if err != nil {
			return err
		}
		firstID = first.ID
		second, err := f.svc.CreateProgram(ctx, tx, f.tenantID, programming.CreateProgramInput{Name: "Phase 2"})
		if err != nil {
			return err
		}
		secondID = second.ID
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	if err := f.tx(t, func(tx pgx.Tx) error {
		if _, err := f.svc.Assign(ctx, tx, f.tenantID, programming.AssignInput{
			ProgramID: firstID, ClientID: clientID, StartsOn: apr1,
		}); err != nil {
			return err
		}
		active, err := f.svc.ActiveAssignment(ctx, tx, clientID)
		if err != nil {
			return err
		}
		if active.ProgramName != "Phase 1" {
			t.Errorf("active = %q, want Phase 1", active.ProgramName)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// Assigning the second replaces rather than collides.
	if err := f.tx(t, func(tx pgx.Tx) error {
		if _, err := f.svc.Assign(ctx, tx, f.tenantID, programming.AssignInput{
			ProgramID: secondID, ClientID: clientID, StartsOn: apr1.AddDate(0, 1, 0),
		}); err != nil {
			return err
		}
		active, err := f.svc.ActiveAssignment(ctx, tx, clientID)
		if err != nil {
			return err
		}
		if active.ProgramName != "Phase 2" {
			t.Errorf("active = %q, want Phase 2", active.ProgramName)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// ---------------------------------------------------------------------------
// Floor logging
// ---------------------------------------------------------------------------

func TestLogSetsAndCompleteWorkout(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	clientID := f.newClient(t, "Dana")
	squat := f.exerciseNamed(t, "Back Squat")

	var workoutID ids.ID
	if err := f.tx(t, func(tx pgx.Tx) error {
		w, err := f.svc.StartWorkout(ctx, tx, f.tenantID, programming.StartWorkoutInput{
			ClientID: clientID, PerformedOn: apr1,
		})
		if err != nil {
			return err
		}
		workoutID = w.ID

		for set := 1; set <= 3; set++ {
			if _, err := f.svc.LogSet(ctx, tx, f.tenantID, programming.LogSetInput{
				WorkoutID: w.ID, ExerciseID: squat, SetIndex: set,
				Reps: ptr(8), LoadGrams: ptr(100_000), RPETenths: ptr(80),
				RestSeconds: ptr(180),
			}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	if err := f.tx(t, func(tx pgx.Tx) error {
		w, err := f.svc.CompleteWorkout(ctx, tx, workoutID, "felt strong")
		if err != nil {
			return err
		}
		if w.Status != programming.WorkoutCompleted {
			t.Errorf("status = %q", w.Status)
		}
		if len(w.Sets) != 3 {
			t.Fatalf("logged %d sets, want 3", len(w.Sets))
		}
		if *w.Sets[0].LoadGrams != 100_000 {
			t.Errorf("load = %v grams, want 100000 (100kg exactly)", w.Sets[0].LoadGrams)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// Re-logging the same set corrects it rather than creating a second one — a
// trainer fixing a typo, and the offline outbox replaying, both land here.
func TestRelogingASetCorrectsItRatherThanDuplicating(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	clientID := f.newClient(t, "Dana")
	squat := f.exerciseNamed(t, "Back Squat")

	var workoutID ids.ID
	if err := f.tx(t, func(tx pgx.Tx) error {
		w, err := f.svc.StartWorkout(ctx, tx, f.tenantID, programming.StartWorkoutInput{
			ClientID: clientID, PerformedOn: apr1,
		})
		if err != nil {
			return err
		}
		workoutID = w.ID
		_, err = f.svc.LogSet(ctx, tx, f.tenantID, programming.LogSetInput{
			WorkoutID: w.ID, ExerciseID: squat, SetIndex: 1,
			Reps: ptr(8), LoadGrams: ptr(100_000),
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	// The trainer mistyped: it was 105kg.
	for range 3 {
		if err := f.tx(t, func(tx pgx.Tx) error {
			_, err := f.svc.LogSet(ctx, tx, f.tenantID, programming.LogSetInput{
				WorkoutID: workoutID, ExerciseID: squat, SetIndex: 1,
				Reps: ptr(8), LoadGrams: ptr(105_000),
			})
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}

	if err := f.tx(t, func(tx pgx.Tx) error {
		w, err := f.svc.GetWorkout(ctx, tx, workoutID)
		if err != nil {
			return err
		}
		if len(w.Sets) != 1 {
			t.Fatalf("re-logging produced %d sets, want 1", len(w.Sets))
		}
		if *w.Sets[0].LoadGrams != 105_000 {
			t.Errorf("load = %v, want the corrected 105000", w.Sets[0].LoadGrams)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// The PRD's single-tap cloning.
func TestClonePreviousSetsPrefillsButDoesNotClaimThemAsDone(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	clientID := f.newClient(t, "Dana")
	squat := f.exerciseNamed(t, "Back Squat")

	// Last week: three sets at 100kg.
	if err := f.tx(t, func(tx pgx.Tx) error {
		w, err := f.svc.StartWorkout(ctx, tx, f.tenantID, programming.StartWorkoutInput{
			ClientID: clientID, PerformedOn: apr1,
		})
		if err != nil {
			return err
		}
		// A warm-up, which must not be cloned as working weight.
		if _, err := f.svc.LogSet(ctx, tx, f.tenantID, programming.LogSetInput{
			WorkoutID: w.ID, ExerciseID: squat, SetIndex: 1,
			Reps: ptr(10), LoadGrams: ptr(40_000), IsWarmup: true,
		}); err != nil {
			return err
		}
		for set := 2; set <= 4; set++ {
			if _, err := f.svc.LogSet(ctx, tx, f.tenantID, programming.LogSetInput{
				WorkoutID: w.ID, ExerciseID: squat, SetIndex: set,
				Reps: ptr(8), LoadGrams: ptr(100_000), RPETenths: ptr(80),
			}); err != nil {
				return err
			}
		}
		_, err = f.svc.CompleteWorkout(ctx, tx, w.ID, "")
		return err
	}); err != nil {
		t.Fatal(err)
	}

	// This week: one tap.
	var todayID ids.ID
	if err := f.tx(t, func(tx pgx.Tx) error {
		w, err := f.svc.StartWorkout(ctx, tx, f.tenantID, programming.StartWorkoutInput{
			ClientID: clientID, PerformedOn: apr1.AddDate(0, 0, 7),
		})
		if err != nil {
			return err
		}
		todayID = w.ID

		result, err := f.svc.ClonePreviousSets(ctx, tx, f.tenantID, w.ID, squat)
		if err != nil {
			return err
		}
		if len(result.Sets) != 3 {
			t.Fatalf("cloned %d sets, want the 3 working sets (not the warm-up)", len(result.Sets))
		}
		if result.ClonedFrom == nil {
			t.Error("the clone does not say where the numbers came from")
		}
		for _, s := range result.Sets {
			if *s.LoadGrams != 100_000 {
				t.Errorf("cloned load = %v, want 100000", s.LoadGrams)
			}
			// The decisive assertion: prefilled is not performed.
			if s.Completed {
				t.Error("a cloned set was marked completed; that would record a lift that never happened")
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// Cloning twice does not stack up sets.
	if err := f.tx(t, func(tx pgx.Tx) error {
		if _, err := f.svc.ClonePreviousSets(ctx, tx, f.tenantID, todayID, squat); err != nil {
			return err
		}
		w, err := f.svc.GetWorkout(ctx, tx, todayID)
		if err != nil {
			return err
		}
		if len(w.Sets) != 3 {
			t.Errorf("cloning twice produced %d sets, want 3", len(w.Sets))
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// A first-time lift has nothing to prefill, and that is not an error.
func TestPreviousPerformanceIsEmptyForAFirstTimeLift(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	clientID := f.newClient(t, "Dana")
	snatch := f.exerciseNamed(t, "Snatch")

	if err := f.tx(t, func(tx pgx.Tx) error {
		prev, err := f.svc.PreviousPerformanceFor(ctx, tx, clientID, snatch, nil)
		if err != nil {
			return err
		}
		if len(prev.Sets) != 0 {
			t.Errorf("found %d previous sets for a lift never performed", len(prev.Sets))
		}
		return nil
	}); err != nil {
		t.Fatalf("a first-time lift should not error: %v", err)
	}
}

// Progression counts working sets only: a deload must not read as a PB.
func TestProgressionExcludesWarmupsAndIncompleteSets(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	clientID := f.newClient(t, "Dana")
	squat := f.exerciseNamed(t, "Back Squat")

	notDone := false
	if err := f.tx(t, func(tx pgx.Tx) error {
		w, err := f.svc.StartWorkout(ctx, tx, f.tenantID, programming.StartWorkoutInput{
			ClientID: clientID, PerformedOn: apr1,
		})
		if err != nil {
			return err
		}
		// Warm-up: excluded.
		if _, err := f.svc.LogSet(ctx, tx, f.tenantID, programming.LogSetInput{
			WorkoutID: w.ID, ExerciseID: squat, SetIndex: 1,
			Reps: ptr(10), LoadGrams: ptr(40_000), IsWarmup: true,
		}); err != nil {
			return err
		}
		// Two real sets: 100kg × 8 = 800,000 gram-reps each.
		for set := 2; set <= 3; set++ {
			if _, err := f.svc.LogSet(ctx, tx, f.tenantID, programming.LogSetInput{
				WorkoutID: w.ID, ExerciseID: squat, SetIndex: set,
				Reps: ptr(8), LoadGrams: ptr(100_000),
			}); err != nil {
				return err
			}
		}
		// Planned but not performed: excluded.
		_, err = f.svc.LogSet(ctx, tx, f.tenantID, programming.LogSetInput{
			WorkoutID: w.ID, ExerciseID: squat, SetIndex: 4,
			Reps: ptr(8), LoadGrams: ptr(100_000), Completed: &notDone,
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	if err := f.tx(t, func(tx pgx.Tx) error {
		points, err := f.svc.ExerciseProgression(ctx, tx, clientID, squat, 10)
		if err != nil {
			return err
		}
		if len(points) != 1 {
			t.Fatalf("got %d progression points, want 1", len(points))
		}
		if points[0].WorkingSets != 2 {
			t.Errorf("working sets = %d, want 2", points[0].WorkingSets)
		}
		if points[0].VolumeGramReps != 1_600_000 {
			t.Errorf("volume = %d gram-reps, want 1600000", points[0].VolumeGramReps)
		}
		if points[0].TopSetGrams == nil || *points[0].TopSetGrams != 100_000 {
			t.Errorf("top set = %v, want 100000", points[0].TopSetGrams)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// A portal client logs their own sets — and only their own.
func TestPortalClientCanLogOwnSetsButNotAnothersWorkout(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	alice := f.newClient(t, "Alice")
	bob := f.newClient(t, "Bob")
	squat := f.exerciseNamed(t, "Back Squat")

	var aliceWorkout, bobWorkout ids.ID
	if err := f.tx(t, func(tx pgx.Tx) error {
		a, err := f.svc.StartWorkout(ctx, tx, f.tenantID, programming.StartWorkoutInput{
			ClientID: alice, PerformedOn: apr1,
		})
		if err != nil {
			return err
		}
		aliceWorkout = a.ID
		b, err := f.svc.StartWorkout(ctx, tx, f.tenantID, programming.StartWorkoutInput{
			ClientID: bob, PerformedOn: apr1,
		})
		if err != nil {
			return err
		}
		bobWorkout = b.ID
		_, err = f.svc.LogSet(ctx, tx, f.tenantID, programming.LogSetInput{
			WorkoutID: bobWorkout, ExerciseID: squat, SetIndex: 1, Reps: ptr(5),
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	// Alice's portal session: she can log her own sets.
	//
	// Each expected refusal runs in its own transaction, because a statement
	// rejected by a policy aborts the surrounding transaction in Postgres.
	// That is also how the real handlers work — one operation per request.
	if err := f.pool.InPortalTx(ctx, f.tenantID, alice, func(tx pgx.Tx) error {
		_, err := f.svc.LogSet(ctx, tx, f.tenantID, programming.LogSetInput{
			WorkoutID: aliceWorkout, ExerciseID: squat, SetIndex: 1,
			Reps: ptr(10), LoadGrams: ptr(60_000),
		})
		return err
	}); err != nil {
		t.Fatalf("a portal client could not log their own set: %v", err)
	}

	// She cannot read another client's workout.
	if err := f.pool.InPortalTx(ctx, f.tenantID, alice, func(tx pgx.Tx) error {
		_, err := f.svc.GetWorkout(ctx, tx, bobWorkout)
		return err
	}); err == nil {
		t.Error("a portal client read another client's workout")
	}

	// Nor write into it.
	if err := f.pool.InPortalTx(ctx, f.tenantID, alice, func(tx pgx.Tx) error {
		_, err := f.svc.LogSet(ctx, tx, f.tenantID, programming.LogSetInput{
			WorkoutID: bobWorkout, ExerciseID: squat, SetIndex: 2, Reps: ptr(99),
		})
		return err
	}); err == nil {
		t.Error("a portal client logged a set into another client's workout")
	}

	// Nor see it in a history listing.
	if err := f.pool.InPortalTx(ctx, f.tenantID, alice, func(tx pgx.Tx) error {
		history, err := f.svc.WorkoutHistory(ctx, tx, bob, 10)
		if err != nil {
			return err
		}
		if len(history) != 0 {
			t.Errorf("a portal client read %d of another client's workouts", len(history))
		}
		return nil
	}); err != nil {
		t.Fatalf("portal history: %v", err)
	}

	// Bob's set survived untouched.
	if err := f.tx(t, func(tx pgx.Tx) error {
		w, err := f.svc.GetWorkout(ctx, tx, bobWorkout)
		if err != nil {
			return err
		}
		if len(w.Sets) != 1 || *w.Sets[0].Reps != 5 {
			t.Errorf("Bob's workout was altered: %+v", w.Sets)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestProgrammingIsTenantIsolated(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	if err := f.tx(t, func(tx pgx.Tx) error {
		_, err := f.svc.CreateProgram(ctx, tx, f.tenantID, programming.CreateProgramInput{Name: "Mine"})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	other := ids.New()
	owner := testsupport.OpenOwner(t)
	if _, err := owner.Raw().Exec(ctx,
		`INSERT INTO tenants (id, name) VALUES ($1, 'Other Gym')`, other); err != nil {
		t.Fatal(err)
	}

	if err := f.pool.InTenantTx(ctx, other, func(tx pgx.Tx) error {
		programs, err := f.svc.ListPrograms(ctx, tx, false)
		if err != nil {
			return err
		}
		if len(programs) != 0 {
			t.Errorf("another tenant's programmes are visible (%d)", len(programs))
		}
		// And the seeded library is per tenant, so theirs is empty until
		// signup seeds it.
		exercises, err := f.svc.ListExercises(ctx, tx, programming.ExerciseFilter{})
		if err != nil {
			return err
		}
		if len(exercises) != 0 {
			t.Errorf("another tenant sees %d of this tenant's exercises", len(exercises))
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
