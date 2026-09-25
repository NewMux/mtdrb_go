package programming

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/NewMux/mtdrb_go/internal/db"
	"github.com/NewMux/mtdrb_go/internal/httpx"
	"github.com/NewMux/mtdrb_go/internal/platform/dates"
	"github.com/NewMux/mtdrb_go/internal/platform/errs"
	"github.com/NewMux/mtdrb_go/internal/platform/ids"
	"github.com/NewMux/mtdrb_go/internal/tenancy"
)

// Handler exposes the exercise library, the programme builder and the floor
// logger.
type Handler struct {
	svc  *Service
	pool *db.Pool
}

// NewHandler builds the programming handler.
func NewHandler(svc *Service, pool *db.Pool) *Handler {
	return &Handler{svc: svc, pool: pool}
}

// ExerciseRoutes returns the library subtree.
func (h *Handler) ExerciseRoutes() http.Handler {
	r := chi.NewRouter()
	r.Get("/", h.listExercises)
	r.Post("/", h.createExercise)
	r.Delete("/{exerciseID}", h.archiveExercise)
	return r
}

// ProgramRoutes returns the builder subtree.
func (h *Handler) ProgramRoutes() http.Handler {
	r := chi.NewRouter()
	r.Get("/", h.listPrograms)
	r.Post("/", h.createProgram)

	r.Route("/{programID}", func(p chi.Router) {
		p.Get("/", h.getProgram)
		p.Post("/duplicate", h.duplicateProgram)
		p.Post("/blocks", h.addBlock)
		p.Post("/assign", h.assignProgram)
	})

	r.Post("/blocks/{blockID}/days", h.addDay)
	r.Post("/days/{dayID}/exercises", h.prescribe)
	return r
}

// WorkoutRoutes returns the floor-logging subtree.
//
// Reachable by a portal client as well as a trainer: a client logging their
// own sets is the point of the companion app, and row-level security narrows
// them to their own rows.
func (h *Handler) WorkoutRoutes() http.Handler {
	r := chi.NewRouter()
	r.Post("/", h.startWorkout)

	r.Route("/{workoutID}", func(w chi.Router) {
		w.Get("/", h.getWorkout)
		w.Post("/complete", h.completeWorkout)
		w.Post("/sets", h.logSet)
		w.Post("/exercises/{exerciseID}/clone-previous", h.clonePrevious)
	})

	r.Get("/clients/{clientID}/history", h.workoutHistory)
	r.Get("/clients/{clientID}/exercises/{exerciseID}/previous", h.previousPerformance)
	r.Get("/clients/{clientID}/exercises/{exerciseID}/progression", h.progression)
	return r
}

// withTenant runs fn bound to the caller's tenant, and to their client id when
// the caller is a portal client.
func (h *Handler) withTenant(w http.ResponseWriter, r *http.Request, fn func(tx pgx.Tx, tenantID ids.ID) error) {
	principal, err := tenancy.Require(r.Context())
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	run := func(tx pgx.Tx) error { return fn(tx, principal.TenantID) }

	if principal.IsClient() {
		err = h.pool.InPortalTx(r.Context(), principal.TenantID, principal.SubjectID, run)
	} else {
		err = h.pool.InTenantTx(r.Context(), principal.TenantID, run)
	}
	if err != nil {
		httpx.Error(w, r, err)
	}
}

// withTrainer is the same, but refuses portal sessions outright. Used for the
// builder: a client may log their training, not rewrite their programme.
func (h *Handler) withTrainer(w http.ResponseWriter, r *http.Request, fn func(tx pgx.Tx, tenantID ids.ID) error) {
	principal, err := tenancy.RequireTrainer(r.Context())
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	if err := h.pool.InTenantTx(r.Context(), principal.TenantID, func(tx pgx.Tx) error {
		return fn(tx, principal.TenantID)
	}); err != nil {
		httpx.Error(w, r, err)
	}
}

func pathID(r *http.Request, name string) (ids.ID, error) {
	id, err := ids.Parse(chi.URLParam(r, name))
	if err != nil {
		return ids.Nil, errs.Invalid(errs.CodeValidation, "%s is not a valid identifier", name)
	}
	return id, nil
}

// ---------------------------------------------------------------------------
// Library
// ---------------------------------------------------------------------------

func (h *Handler) listExercises(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	filter := ExerciseFilter{
		Category: Category(q.Get("category")),
		Search:   q.Get("search"),
		Limit:    atoiOr(q.Get("limit"), 200),
		Offset:   atoiOr(q.Get("offset"), 0),
	}
	h.withTenant(w, r, func(tx pgx.Tx, _ ids.ID) error {
		list, err := h.svc.ListExercises(r.Context(), tx, filter)
		if err != nil {
			return err
		}
		if list == nil {
			list = []Exercise{}
		}
		httpx.JSON(w, r, http.StatusOK, map[string]any{"exercises": list})
		return nil
	})
}

func (h *Handler) createExercise(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name          string   `json:"name"`
		Category      Category `json:"category"`
		PrimaryMuscle string   `json:"primary_muscle"`
		Equipment     string   `json:"equipment"`
		Instructions  string   `json:"instructions"`
		VideoURL      string   `json:"video_url"`
		DemoMediaID   *ids.ID  `json:"demo_media_id"`
	}
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	h.withTrainer(w, r, func(tx pgx.Tx, tenantID ids.ID) error {
		e, err := h.svc.CreateExercise(r.Context(), tx, tenantID, CreateExerciseInput(req))
		if err != nil {
			return err
		}
		httpx.JSON(w, r, http.StatusCreated, e)
		return nil
	})
}

func (h *Handler) archiveExercise(w http.ResponseWriter, r *http.Request) {
	exerciseID, err := pathID(r, "exerciseID")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	h.withTrainer(w, r, func(tx pgx.Tx, _ ids.ID) error {
		if err := h.svc.ArchiveExercise(r.Context(), tx, exerciseID); err != nil {
			return err
		}
		httpx.NoContent(w, r)
		return nil
	})
}

// ---------------------------------------------------------------------------
// Builder
// ---------------------------------------------------------------------------

func (h *Handler) createProgram(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		IsTemplate  bool   `json:"is_template"`
	}
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	h.withTrainer(w, r, func(tx pgx.Tx, tenantID ids.ID) error {
		p, err := h.svc.CreateProgram(r.Context(), tx, tenantID, CreateProgramInput(req))
		if err != nil {
			return err
		}
		httpx.JSON(w, r, http.StatusCreated, p)
		return nil
	})
}

func (h *Handler) listPrograms(w http.ResponseWriter, r *http.Request) {
	templatesOnly := r.URL.Query().Get("templates") == "true"
	h.withTenant(w, r, func(tx pgx.Tx, _ ids.ID) error {
		list, err := h.svc.ListPrograms(r.Context(), tx, templatesOnly)
		if err != nil {
			return err
		}
		if list == nil {
			list = []Program{}
		}
		httpx.JSON(w, r, http.StatusOK, map[string]any{"programs": list})
		return nil
	})
}

func (h *Handler) getProgram(w http.ResponseWriter, r *http.Request) {
	programID, err := pathID(r, "programID")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	h.withTenant(w, r, func(tx pgx.Tx, _ ids.ID) error {
		p, err := h.svc.GetProgram(r.Context(), tx, programID)
		if err != nil {
			return err
		}
		httpx.JSON(w, r, http.StatusOK, p)
		return nil
	})
}

func (h *Handler) duplicateProgram(w http.ResponseWriter, r *http.Request) {
	programID, err := pathID(r, "programID")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	h.withTrainer(w, r, func(tx pgx.Tx, tenantID ids.ID) error {
		p, err := h.svc.DuplicateProgram(r.Context(), tx, tenantID, programID, req.Name)
		if err != nil {
			return err
		}
		httpx.JSON(w, r, http.StatusCreated, p)
		return nil
	})
}

func (h *Handler) addBlock(w http.ResponseWriter, r *http.Request) {
	programID, err := pathID(r, "programID")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	var req struct {
		Name  string `json:"name"`
		Weeks int    `json:"weeks"`
		Notes string `json:"notes"`
	}
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	h.withTrainer(w, r, func(tx pgx.Tx, tenantID ids.ID) error {
		b, err := h.svc.AddBlock(r.Context(), tx, tenantID, AddBlockInput{
			ProgramID: programID, Name: req.Name, Weeks: req.Weeks, Notes: req.Notes,
		})
		if err != nil {
			return err
		}
		httpx.JSON(w, r, http.StatusCreated, b)
		return nil
	})
}

func (h *Handler) addDay(w http.ResponseWriter, r *http.Request) {
	blockID, err := pathID(r, "blockID")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	var req struct {
		Name  string `json:"name"`
		Notes string `json:"notes"`
	}
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	h.withTrainer(w, r, func(tx pgx.Tx, tenantID ids.ID) error {
		d, err := h.svc.AddDay(r.Context(), tx, tenantID, AddDayInput{
			BlockID: blockID, Name: req.Name, Notes: req.Notes,
		})
		if err != nil {
			return err
		}
		httpx.JSON(w, r, http.StatusCreated, d)
		return nil
	})
}

func (h *Handler) prescribe(w http.ResponseWriter, r *http.Request) {
	dayID, err := pathID(r, "dayID")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	var req struct {
		ExerciseID      ids.ID `json:"exercise_id"`
		TargetSets      int    `json:"target_sets"`
		TargetRepsMin   *int   `json:"target_reps_min"`
		TargetRepsMax   *int   `json:"target_reps_max"`
		TargetLoadGrams *int   `json:"target_load_grams"`
		TargetRPETenths *int   `json:"target_rpe_tenths"`
		TargetRIR       *int   `json:"target_rir"`
		RestSeconds     *int   `json:"rest_seconds"`
		Tempo           string `json:"tempo"`
		SupersetGroup   string `json:"superset_group"`
		Notes           string `json:"notes"`
	}
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	h.withTrainer(w, r, func(tx pgx.Tx, tenantID ids.ID) error {
		p, err := h.svc.Prescribe(r.Context(), tx, tenantID, PrescribeInput{
			DayID: dayID, ExerciseID: req.ExerciseID, TargetSets: req.TargetSets,
			TargetRepsMin: req.TargetRepsMin, TargetRepsMax: req.TargetRepsMax,
			TargetLoadGrams: req.TargetLoadGrams, TargetRPETenths: req.TargetRPETenths,
			TargetRIR: req.TargetRIR, RestSeconds: req.RestSeconds,
			Tempo: req.Tempo, SupersetGroup: req.SupersetGroup, Notes: req.Notes,
		})
		if err != nil {
			return err
		}
		httpx.JSON(w, r, http.StatusCreated, p)
		return nil
	})
}

func (h *Handler) assignProgram(w http.ResponseWriter, r *http.Request) {
	programID, err := pathID(r, "programID")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	// Calendar dates, as the spec publishes them. Decoding into time.Time
	// refused the "2026-09-23" every conforming client sends — the same bug
	// STATUS.md records for the other date fields.
	var req struct {
		ClientID ids.ID      `json:"client_id"`
		StartsOn *dates.Date `json:"starts_on"`
		EndsOn   *dates.Date `json:"ends_on"`
		Notes    string      `json:"notes"`
	}
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	h.withTrainer(w, r, func(tx pgx.Tx, tenantID ids.ID) error {
		in := AssignInput{
			ProgramID: programID, ClientID: req.ClientID,
			EndsOn: req.EndsOn.TimePtr(), Notes: req.Notes,
		}
		if start := req.StartsOn.TimePtr(); start != nil {
			in.StartsOn = *start
		}
		a, err := h.svc.Assign(r.Context(), tx, tenantID, in)
		if err != nil {
			return err
		}
		httpx.JSON(w, r, http.StatusCreated, a)
		return nil
	})
}

// ---------------------------------------------------------------------------
// Floor logging
// ---------------------------------------------------------------------------

func (h *Handler) startWorkout(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ClientID     ids.ID  `json:"client_id"`
		AssignmentID *ids.ID `json:"assignment_id"`
		DayID        *ids.ID `json:"day_id"`
		SessionID    *ids.ID `json:"session_id"`
		WeekNumber   int     `json:"week_number"`
		// A calendar date, as the spec publishes it; a timestamp is still
		// accepted by dates.Date for callers that sent one.
		PerformedOn *dates.Date `json:"performed_on"`
		Notes       string      `json:"notes"`
	}
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	principal, err := tenancy.Require(r.Context())
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	clientID := req.ClientID
	if principal.IsClient() {
		// A portal client logs their own training. Honouring a client_id from
		// the body would let one client write into another's history.
		clientID = principal.SubjectID
	}

	h.withTenant(w, r, func(tx pgx.Tx, tenantID ids.ID) error {
		in := StartWorkoutInput{
			ClientID: clientID, AssignmentID: req.AssignmentID, DayID: req.DayID,
			SessionID: req.SessionID, WeekNumber: req.WeekNumber, Notes: req.Notes,
		}
		if day := req.PerformedOn.TimePtr(); day != nil {
			in.PerformedOn = *day
		}
		workout, err := h.svc.StartWorkout(r.Context(), tx, tenantID, in)
		if err != nil {
			return err
		}
		httpx.JSON(w, r, http.StatusCreated, workout)
		return nil
	})
}

func (h *Handler) logSet(w http.ResponseWriter, r *http.Request) {
	workoutID, err := pathID(r, "workoutID")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	var req struct {
		ExerciseID        ids.ID  `json:"exercise_id"`
		ProgramExerciseID *ids.ID `json:"program_exercise_id"`
		SetIndex          int     `json:"set_index"`
		Reps              *int    `json:"reps"`
		LoadGrams         *int    `json:"load_grams"`
		RPETenths         *int    `json:"rpe_tenths"`
		RIR               *int    `json:"rir"`
		RestSeconds       *int    `json:"rest_seconds"`
		Tempo             string  `json:"tempo"`
		IsWarmup          bool    `json:"is_warmup"`
		Completed         *bool   `json:"completed"`
		Notes             string  `json:"notes"`
		FormCheckMediaID  *ids.ID `json:"form_check_media_id"`
	}
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	h.withTenant(w, r, func(tx pgx.Tx, tenantID ids.ID) error {
		set, err := h.svc.LogSet(r.Context(), tx, tenantID, LogSetInput{
			WorkoutID: workoutID, ExerciseID: req.ExerciseID,
			ProgramExerciseID: req.ProgramExerciseID, SetIndex: req.SetIndex,
			Reps: req.Reps, LoadGrams: req.LoadGrams, RPETenths: req.RPETenths,
			RIR: req.RIR, RestSeconds: req.RestSeconds, Tempo: req.Tempo,
			IsWarmup: req.IsWarmup, Completed: req.Completed, Notes: req.Notes,
			FormCheckMediaID: req.FormCheckMediaID,
		})
		if err != nil {
			return err
		}
		httpx.JSON(w, r, http.StatusOK, set)
		return nil
	})
}

func (h *Handler) getWorkout(w http.ResponseWriter, r *http.Request) {
	workoutID, err := pathID(r, "workoutID")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	h.withTenant(w, r, func(tx pgx.Tx, _ ids.ID) error {
		workout, err := h.svc.GetWorkout(r.Context(), tx, workoutID)
		if err != nil {
			return err
		}
		httpx.JSON(w, r, http.StatusOK, workout)
		return nil
	})
}

func (h *Handler) completeWorkout(w http.ResponseWriter, r *http.Request) {
	workoutID, err := pathID(r, "workoutID")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	var req struct {
		Notes string `json:"notes"`
	}
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	h.withTenant(w, r, func(tx pgx.Tx, _ ids.ID) error {
		workout, err := h.svc.CompleteWorkout(r.Context(), tx, workoutID, req.Notes)
		if err != nil {
			return err
		}
		httpx.JSON(w, r, http.StatusOK, workout)
		return nil
	})
}

// clonePrevious is the PRD's single tap: prefill today from last time.
func (h *Handler) clonePrevious(w http.ResponseWriter, r *http.Request) {
	workoutID, err := pathID(r, "workoutID")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	exerciseID, err := pathID(r, "exerciseID")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	h.withTenant(w, r, func(tx pgx.Tx, tenantID ids.ID) error {
		result, err := h.svc.ClonePreviousSets(r.Context(), tx, tenantID, workoutID, exerciseID)
		if err != nil {
			return err
		}
		httpx.JSON(w, r, http.StatusCreated, result)
		return nil
	})
}

func (h *Handler) previousPerformance(w http.ResponseWriter, r *http.Request) {
	clientID, err := pathID(r, "clientID")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	exerciseID, err := pathID(r, "exerciseID")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	h.withTenant(w, r, func(tx pgx.Tx, _ ids.ID) error {
		prev, err := h.svc.PreviousPerformanceFor(r.Context(), tx, clientID, exerciseID, nil)
		if err != nil {
			return err
		}
		httpx.JSON(w, r, http.StatusOK, prev)
		return nil
	})
}

func (h *Handler) workoutHistory(w http.ResponseWriter, r *http.Request) {
	clientID, err := pathID(r, "clientID")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	limit := atoiOr(r.URL.Query().Get("limit"), 50)

	h.withTenant(w, r, func(tx pgx.Tx, _ ids.ID) error {
		history, err := h.svc.WorkoutHistory(r.Context(), tx, clientID, limit)
		if err != nil {
			return err
		}
		if history == nil {
			history = []Workout{}
		}
		httpx.JSON(w, r, http.StatusOK, map[string]any{"workouts": history})
		return nil
	})
}

func (h *Handler) progression(w http.ResponseWriter, r *http.Request) {
	clientID, err := pathID(r, "clientID")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	exerciseID, err := pathID(r, "exerciseID")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	limit := atoiOr(r.URL.Query().Get("limit"), 30)

	h.withTenant(w, r, func(tx pgx.Tx, _ ids.ID) error {
		points, err := h.svc.ExerciseProgression(r.Context(), tx, clientID, exerciseID, limit)
		if err != nil {
			return err
		}
		if points == nil {
			points = []VolumePoint{}
		}
		httpx.JSON(w, r, http.StatusOK, map[string]any{"points": points})
		return nil
	})
}

func atoiOr(raw string, def int) int {
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return def
	}
	return n
}
