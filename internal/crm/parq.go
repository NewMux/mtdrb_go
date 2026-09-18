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

// The PAR-Q is the standard seven-question readiness screen. Answering "yes"
// to any of them means the client should speak to a doctor before starting,
// which is a safety gate, not a formality — so the flag is derived here rather
// than trusted from the client.

// ParQQuestion identifies one of the seven standard questions.
type ParQQuestion string

const (
	ParQHeartCondition   ParQQuestion = "heart_condition"
	ParQChestPainActive  ParQQuestion = "chest_pain_activity"
	ParQChestPainRest    ParQQuestion = "chest_pain_rest"
	ParQLosesBalance     ParQQuestion = "loses_balance_dizziness"
	ParQBoneJointProblem ParQQuestion = "bone_or_joint_problem"
	ParQBloodPressureMed ParQQuestion = "blood_pressure_medication"
	ParQOtherReason      ParQQuestion = "other_reason"
)

// StandardParQQuestions is the canonical ordered set, used to validate a
// submission and to render the form.
var StandardParQQuestions = []ParQQuestion{
	ParQHeartCondition,
	ParQChestPainActive,
	ParQChestPainRest,
	ParQLosesBalance,
	ParQBoneJointProblem,
	ParQBloodPressureMed,
	ParQOtherReason,
}

// ParQPrompts is the wording shown for each question.
var ParQPrompts = map[ParQQuestion]string{
	ParQHeartCondition:   "Has your doctor ever said that you have a heart condition and that you should only do physical activity recommended by a doctor?",
	ParQChestPainActive:  "Do you feel pain in your chest when you do physical activity?",
	ParQChestPainRest:    "In the past month, have you had chest pain when you were not doing physical activity?",
	ParQLosesBalance:     "Do you lose your balance because of dizziness, or do you ever lose consciousness?",
	ParQBoneJointProblem: "Do you have a bone or joint problem that could be made worse by a change in your physical activity?",
	ParQBloodPressureMed: "Is your doctor currently prescribing drugs for your blood pressure or a heart condition?",
	ParQOtherReason:      "Do you know of any other reason why you should not do physical activity?",
}

// ParQAnswer is one response, with optional detail.
type ParQAnswer struct {
	Question ParQQuestion `json:"question"`
	Yes      bool         `json:"yes"`
	Detail   string       `json:"detail,omitempty"`
}

// ParQResponse is a completed readiness questionnaire.
type ParQResponse struct {
	ID                ids.ID       `json:"id"`
	ClientID          ids.ID       `json:"client_id"`
	Answers           []ParQAnswer `json:"answers"`
	RequiresClearance bool         `json:"requires_clearance"`
	ClearedAt         *time.Time   `json:"cleared_at,omitempty"`
	CompletedAt       time.Time    `json:"completed_at"`
}

// SubmitParQ records a completed questionnaire.
func (s *Service) SubmitParQ(ctx context.Context, tx pgx.Tx, tenantID, clientID ids.ID, answers []ParQAnswer) (ParQResponse, error) {
	if err := validateParQ(answers); err != nil {
		return ParQResponse{}, err
	}

	// Derived, never taken from the request: a client app must not be able to
	// submit "no clearance needed" alongside a yes answer.
	requiresClearance := false
	for _, a := range answers {
		if a.Yes {
			requiresClearance = true
			break
		}
	}

	payload, err := json.Marshal(answers)
	if err != nil {
		return ParQResponse{}, errs.Internal(err, "encode PAR-Q answers")
	}

	r := ParQResponse{
		ID: ids.New(), ClientID: clientID, Answers: answers,
		RequiresClearance: requiresClearance, CompletedAt: s.clock.Now(),
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO parq_responses (id, tenant_id, client_id, answers, requires_clearance, completed_at)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		r.ID, tenantID, clientID, payload, requiresClearance, r.CompletedAt); err != nil {
		if db.IsForeignKeyViolation(err) {
			return ParQResponse{}, errs.NotFound("client")
		}
		return ParQResponse{}, errs.Internal(err, "record PAR-Q response")
	}
	return r, nil
}

// validateParQ requires exactly the standard question set, answered once each.
func validateParQ(answers []ParQAnswer) error {
	seen := make(map[ParQQuestion]bool, len(answers))
	for _, a := range answers {
		if _, known := ParQPrompts[a.Question]; !known {
			return errs.Invalid(errs.CodeValidation, "unknown PAR-Q question %q", a.Question)
		}
		if seen[a.Question] {
			return errs.Invalid(errs.CodeValidation, "PAR-Q question %q was answered twice", a.Question)
		}
		seen[a.Question] = true
	}
	for _, q := range StandardParQQuestions {
		if !seen[q] {
			return errs.Invalid(errs.CodeValidation, "PAR-Q is incomplete").
				WithField(string(q), "must be answered")
		}
	}
	return nil
}

// LatestParQ returns a client's most recent questionnaire.
func (s *Service) LatestParQ(ctx context.Context, tx pgx.Tx, clientID ids.ID) (ParQResponse, error) {
	var r ParQResponse
	var payload []byte
	// server_seq breaks ties on completed_at. Two submissions can legitimately
	// carry the same timestamp — a pair of offline responses synced together,
	// or a client resubmitting immediately — and "latest" must still mean the
	// one written last, not whichever row the planner happens to return.
	err := tx.QueryRow(ctx, `
		SELECT id, client_id, answers, requires_clearance, cleared_at, completed_at
		  FROM parq_responses WHERE client_id = $1
		 ORDER BY completed_at DESC, server_seq DESC LIMIT 1`, clientID,
	).Scan(&r.ID, &r.ClientID, &payload, &r.RequiresClearance, &r.ClearedAt, &r.CompletedAt)
	if err != nil {
		if db.IsNoRows(err) {
			return ParQResponse{}, errs.NotFound("PAR-Q response")
		}
		return ParQResponse{}, errs.Internal(err, "load PAR-Q response")
	}
	if err := json.Unmarshal(payload, &r.Answers); err != nil {
		return ParQResponse{}, errs.Internal(err, "decode PAR-Q answers")
	}
	return r, nil
}

// RecordMedicalClearance marks that a flagged client has been cleared by a
// doctor to train.
func (s *Service) RecordMedicalClearance(ctx context.Context, tx pgx.Tx, responseID ids.ID) error {
	tag, err := tx.Exec(ctx,
		`UPDATE parq_responses SET cleared_at = now() WHERE id = $1 AND cleared_at IS NULL`, responseID)
	if err != nil {
		return errs.Internal(err, "record medical clearance")
	}
	if tag.RowsAffected() == 0 {
		return errs.NotFound("uncleared PAR-Q response")
	}
	return nil
}
