// Package programming holds the workout builder and the performance log.
//
// Two facts are kept deliberately apart: what the trainer prescribed, and what
// the client actually did. Comparing them is what progression tracking *is*,
// so collapsing them into one record — a set row that gets overwritten with
// reality — would destroy the thing the product exists to show.
package programming

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/NewMux/mtdrb_go/internal/db"
	"github.com/NewMux/mtdrb_go/internal/platform/clock"
	"github.com/NewMux/mtdrb_go/internal/platform/errs"
	"github.com/NewMux/mtdrb_go/internal/platform/ids"
)

// Category is the movement pattern an exercise trains.
//
// Patterns rather than muscles, because that is how a coach checks a programme
// is balanced: two pushes and no pulls is a problem you see by pattern, not by
// muscle name.
type Category string

const (
	CategorySquat          Category = "squat"
	CategoryHinge          Category = "hinge"
	CategoryLunge          Category = "lunge"
	CategoryHorizontalPush Category = "horizontal_push"
	CategoryVerticalPush   Category = "vertical_push"
	CategoryHorizontalPull Category = "horizontal_pull"
	CategoryVerticalPull   Category = "vertical_pull"
	CategoryCarry          Category = "carry"
	CategoryCore           Category = "core"
	CategoryOlympic        Category = "olympic"
	CategoryConditioning   Category = "conditioning"
	CategoryMobility       Category = "mobility"
	CategoryOther          Category = "other"
)

// Valid reports whether the category is recognised.
func (c Category) Valid() bool {
	switch c {
	case CategorySquat, CategoryHinge, CategoryLunge, CategoryHorizontalPush,
		CategoryVerticalPush, CategoryHorizontalPull, CategoryVerticalPull,
		CategoryCarry, CategoryCore, CategoryOlympic, CategoryConditioning,
		CategoryMobility, CategoryOther:
		return true
	default:
		return false
	}
}

// Exercise is one movement in the library.
type Exercise struct {
	ID            ids.ID   `json:"id"`
	Name          string   `json:"name"`
	Category      Category `json:"category"`
	PrimaryMuscle string   `json:"primary_muscle"`
	Equipment     string   `json:"equipment"`
	Instructions  string   `json:"instructions"`
	VideoURL      string   `json:"video_url"`
	DemoMediaID   *ids.ID  `json:"demo_media_id,omitempty"`
	IsCustom      bool     `json:"is_custom"`
	ServerSeq     int64    `json:"server_seq"`
}

// Service manages the library, programmes and workout logs.
type Service struct {
	clock clock.Clock
}

// NewService builds the programming service.
func NewService(c clock.Clock) *Service {
	if c == nil {
		c = clock.System{}
	}
	return &Service{clock: c}
}

type seedExercise struct {
	Name      string
	Category  Category
	Muscle    string
	Equipment string
}

// defaultLibrary is what every new tenant starts with.
//
// Deliberately a starting point rather than an encyclopaedia: enough to build
// a real programme on day one, organised by movement pattern, with the obvious
// barbell, dumbbell, machine and bodyweight options for each. A trainer adds
// their own from there.
var defaultLibrary = []seedExercise{
	// Squat
	{"Back Squat", CategorySquat, "Quadriceps", "Barbell"},
	{"Front Squat", CategorySquat, "Quadriceps", "Barbell"},
	{"Goblet Squat", CategorySquat, "Quadriceps", "Dumbbell"},
	{"Leg Press", CategorySquat, "Quadriceps", "Machine"},
	{"Hack Squat", CategorySquat, "Quadriceps", "Machine"},
	{"Bodyweight Squat", CategorySquat, "Quadriceps", "Bodyweight"},

	// Hinge
	{"Conventional Deadlift", CategoryHinge, "Hamstrings", "Barbell"},
	{"Sumo Deadlift", CategoryHinge, "Glutes", "Barbell"},
	{"Romanian Deadlift", CategoryHinge, "Hamstrings", "Barbell"},
	{"Dumbbell Romanian Deadlift", CategoryHinge, "Hamstrings", "Dumbbell"},
	{"Hip Thrust", CategoryHinge, "Glutes", "Barbell"},
	{"Kettlebell Swing", CategoryHinge, "Glutes", "Kettlebell"},
	{"Back Extension", CategoryHinge, "Erectors", "Bodyweight"},
	{"Seated Leg Curl", CategoryHinge, "Hamstrings", "Machine"},

	// Lunge
	{"Walking Lunge", CategoryLunge, "Quadriceps", "Dumbbell"},
	{"Reverse Lunge", CategoryLunge, "Quadriceps", "Dumbbell"},
	{"Bulgarian Split Squat", CategoryLunge, "Quadriceps", "Dumbbell"},
	{"Step Up", CategoryLunge, "Quadriceps", "Dumbbell"},

	// Horizontal push
	{"Bench Press", CategoryHorizontalPush, "Chest", "Barbell"},
	{"Incline Bench Press", CategoryHorizontalPush, "Chest", "Barbell"},
	{"Dumbbell Bench Press", CategoryHorizontalPush, "Chest", "Dumbbell"},
	{"Machine Chest Press", CategoryHorizontalPush, "Chest", "Machine"},
	{"Cable Fly", CategoryHorizontalPush, "Chest", "Cable"},
	{"Push-Up", CategoryHorizontalPush, "Chest", "Bodyweight"},
	{"Dip", CategoryHorizontalPush, "Chest", "Bodyweight"},

	// Vertical push
	{"Overhead Press", CategoryVerticalPush, "Shoulders", "Barbell"},
	{"Seated Dumbbell Press", CategoryVerticalPush, "Shoulders", "Dumbbell"},
	{"Arnold Press", CategoryVerticalPush, "Shoulders", "Dumbbell"},
	{"Lateral Raise", CategoryVerticalPush, "Shoulders", "Dumbbell"},
	{"Triceps Pushdown", CategoryVerticalPush, "Triceps", "Cable"},
	{"Skull Crusher", CategoryVerticalPush, "Triceps", "Barbell"},

	// Horizontal pull
	{"Barbell Row", CategoryHorizontalPull, "Back", "Barbell"},
	{"Pendlay Row", CategoryHorizontalPull, "Back", "Barbell"},
	{"Single-Arm Dumbbell Row", CategoryHorizontalPull, "Back", "Dumbbell"},
	{"Seated Cable Row", CategoryHorizontalPull, "Back", "Cable"},
	{"Chest-Supported Row", CategoryHorizontalPull, "Back", "Machine"},
	{"Face Pull", CategoryHorizontalPull, "Rear Delts", "Cable"},
	{"Inverted Row", CategoryHorizontalPull, "Back", "Bodyweight"},

	// Vertical pull
	{"Pull-Up", CategoryVerticalPull, "Lats", "Bodyweight"},
	{"Chin-Up", CategoryVerticalPull, "Lats", "Bodyweight"},
	{"Lat Pulldown", CategoryVerticalPull, "Lats", "Cable"},
	{"Straight-Arm Pulldown", CategoryVerticalPull, "Lats", "Cable"},
	{"Barbell Curl", CategoryVerticalPull, "Biceps", "Barbell"},
	{"Dumbbell Curl", CategoryVerticalPull, "Biceps", "Dumbbell"},
	{"Hammer Curl", CategoryVerticalPull, "Biceps", "Dumbbell"},

	// Carry
	{"Farmer's Carry", CategoryCarry, "Full Body", "Dumbbell"},
	{"Suitcase Carry", CategoryCarry, "Obliques", "Kettlebell"},

	// Core
	{"Plank", CategoryCore, "Abdominals", "Bodyweight"},
	{"Side Plank", CategoryCore, "Obliques", "Bodyweight"},
	{"Hanging Leg Raise", CategoryCore, "Abdominals", "Bodyweight"},
	{"Cable Crunch", CategoryCore, "Abdominals", "Cable"},
	{"Pallof Press", CategoryCore, "Obliques", "Cable"},
	{"Dead Bug", CategoryCore, "Abdominals", "Bodyweight"},

	// Olympic
	{"Power Clean", CategoryOlympic, "Full Body", "Barbell"},
	{"Hang Clean", CategoryOlympic, "Full Body", "Barbell"},
	{"Push Press", CategoryOlympic, "Shoulders", "Barbell"},
	{"Snatch", CategoryOlympic, "Full Body", "Barbell"},

	// Conditioning
	{"Rowing Machine", CategoryConditioning, "Full Body", "Machine"},
	{"Assault Bike", CategoryConditioning, "Full Body", "Machine"},
	{"Treadmill Run", CategoryConditioning, "Legs", "Machine"},
	{"Battle Ropes", CategoryConditioning, "Full Body", "Ropes"},
	{"Box Jump", CategoryConditioning, "Legs", "Plyo Box"},

	// Mobility
	{"Couch Stretch", CategoryMobility, "Hip Flexors", "Bodyweight"},
	{"Thoracic Rotation", CategoryMobility, "Thoracic Spine", "Bodyweight"},
	{"90/90 Hip Stretch", CategoryMobility, "Hips", "Bodyweight"},
	{"Banded Shoulder Dislocate", CategoryMobility, "Shoulders", "Band"},
}

// SeedLibrary installs the default exercise library for a new tenant.
//
// Runs in the signup transaction alongside the chart of accounts: a trainer
// opening the app for the first time should be able to build a programme, not
// stare at an empty list.
func (s *Service) SeedLibrary(ctx context.Context, tx pgx.Tx, tenantID ids.ID) error {
	n := len(defaultLibrary)
	exerciseIDs := make([]ids.ID, n)
	names := make([]string, n)
	categories := make([]string, n)
	muscles := make([]string, n)
	equipment := make([]string, n)

	for i, e := range defaultLibrary {
		exerciseIDs[i] = ids.New()
		names[i] = e.Name
		categories[i] = string(e.Category)
		muscles[i] = e.Muscle
		equipment[i] = e.Equipment
	}

	// One set-based statement rather than COPY, because COPY FROM is refused
	// on tables with row-level security and the app role is not exempt.
	if _, err := tx.Exec(ctx, `
		INSERT INTO exercises (id, tenant_id, name, category, primary_muscle, equipment, is_custom)
		SELECT e.id, $1, e.name, e.category::exercise_category, e.muscle, e.equipment, false
		  FROM unnest($2::uuid[], $3::text[], $4::text[], $5::text[], $6::text[])
		    AS e(id, name, category, muscle, equipment)`,
		tenantID, exerciseIDs, names, categories, muscles, equipment,
	); err != nil {
		return errs.Internal(err, "seed exercise library")
	}
	return nil
}

// CreateExerciseInput describes a movement to add to the library.
type CreateExerciseInput struct {
	Name          string
	Category      Category
	PrimaryMuscle string
	Equipment     string
	Instructions  string
	VideoURL      string
	DemoMediaID   *ids.ID
}

// CreateExercise adds a trainer's own movement.
func (s *Service) CreateExercise(ctx context.Context, tx pgx.Tx, tenantID ids.ID, in CreateExerciseInput) (Exercise, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return Exercise{}, errs.Invalid(errs.CodeValidation, "an exercise needs a name").
			WithField("name", "is required")
	}
	if in.Category == "" {
		in.Category = CategoryOther
	}
	if !in.Category.Valid() {
		return Exercise{}, errs.Invalid(errs.CodeValidation,
			"unknown exercise category %q", in.Category).
			WithField("category", "is not a recognised movement pattern")
	}
	if err := validateVideoURL(in.VideoURL); err != nil {
		return Exercise{}, err
	}

	e := Exercise{
		ID: ids.New(), Name: name, Category: in.Category,
		PrimaryMuscle: strings.TrimSpace(in.PrimaryMuscle),
		Equipment:     strings.TrimSpace(in.Equipment),
		Instructions:  strings.TrimSpace(in.Instructions),
		VideoURL:      strings.TrimSpace(in.VideoURL),
		DemoMediaID:   in.DemoMediaID,
		IsCustom:      true,
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO exercises
			(id, tenant_id, name, category, primary_muscle, equipment, instructions,
			 video_url, demo_media_id, is_custom)
		VALUES ($1, $2, $3, $4::exercise_category, $5, $6, $7, $8, $9, true)
		RETURNING server_seq`,
		e.ID, tenantID, e.Name, string(e.Category), e.PrimaryMuscle, e.Equipment,
		e.Instructions, e.VideoURL, e.DemoMediaID,
	).Scan(&e.ServerSeq); err != nil {
		if db.IsUniqueViolation(err, "exercises_tenant_name_key") {
			return Exercise{}, errs.Conflict("exercise_exists",
				"an exercise named %q already exists", name)
		}
		return Exercise{}, errs.Internal(err, "create exercise")
	}
	return e, nil
}

// validateVideoURL keeps the embed field to hosts a client can actually play.
//
// An allowlist rather than a format check: the URL is rendered into an app
// that will follow it, and "any string starting with https" includes a great
// many things that are not an exercise demonstration.
func validateVideoURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	allowed := []string{
		"https://youtube.com/", "https://www.youtube.com/", "https://youtu.be/",
		"https://m.youtube.com/", "https://vimeo.com/", "https://player.vimeo.com/",
	}
	for _, prefix := range allowed {
		if strings.HasPrefix(raw, prefix) {
			return nil
		}
	}
	return errs.Invalid(errs.CodeValidation,
		"a demonstration link must be a YouTube or Vimeo URL").
		WithField("video_url", "must be a https YouTube or Vimeo link")
}

// ExerciseFilter narrows a library listing.
type ExerciseFilter struct {
	Category Category
	Search   string
	Limit    int
	Offset   int
}

// ListExercises returns the library, alphabetically.
func (s *Service) ListExercises(ctx context.Context, tx pgx.Tx, f ExerciseFilter) ([]Exercise, error) {
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 200
	}
	var category *string
	if f.Category != "" {
		v := string(f.Category)
		category = &v
	}
	var search *string
	if trimmed := strings.TrimSpace(f.Search); trimmed != "" {
		search = &trimmed
	}

	rows, err := tx.Query(ctx, `
		SELECT id, name, category::text, primary_muscle, equipment, instructions,
		       video_url, demo_media_id, is_custom, server_seq
		  FROM exercises
		 WHERE archived_at IS NULL
		   AND ($1::text IS NULL OR category::text = $1)
		   AND ($2::text IS NULL OR name ILIKE '%' || $2 || '%'
		        OR primary_muscle ILIKE '%' || $2 || '%'
		        OR equipment ILIKE '%' || $2 || '%')
		 ORDER BY name
		 LIMIT $3 OFFSET $4`, category, search, f.Limit, f.Offset)
	if err != nil {
		return nil, errs.Internal(err, "list exercises")
	}
	defer rows.Close()

	var out []Exercise
	for rows.Next() {
		var e Exercise
		var category string
		if err := rows.Scan(&e.ID, &e.Name, &category, &e.PrimaryMuscle, &e.Equipment,
			&e.Instructions, &e.VideoURL, &e.DemoMediaID, &e.IsCustom, &e.ServerSeq); err != nil {
			return nil, errs.Internal(err, "scan exercise")
		}
		e.Category = Category(category)
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, errs.Internal(err, "read exercises")
	}
	return out, nil
}

// ArchiveExercise retires a movement from the library.
//
// Archived rather than deleted: programmes and logged sets reference it, and
// erasing a lift a client has been doing for a year would take their history
// with it.
func (s *Service) ArchiveExercise(ctx context.Context, tx pgx.Tx, exerciseID ids.ID) error {
	tag, err := tx.Exec(ctx,
		`UPDATE exercises SET archived_at = now() WHERE id = $1 AND archived_at IS NULL`, exerciseID)
	if err != nil {
		return errs.Internal(err, "archive exercise")
	}
	if tag.RowsAffected() == 0 {
		return errs.NotFound("exercise")
	}
	return nil
}
