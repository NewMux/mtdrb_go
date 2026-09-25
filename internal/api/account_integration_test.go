//go:build integration

package api_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/NewMux/mtdrb_go/internal/jobs"
	"github.com/NewMux/mtdrb_go/internal/platform/clock"
	"github.com/NewMux/mtdrb_go/internal/testsupport"
)

// removedPrefixes records what a purge asked storage to delete.
type removedPrefixes []string

func (r *removedPrefixes) RemovePrefix(_ context.Context, prefix string) error {
	*r = append(*r, prefix)
	return nil
}

// busyPractice gives a practice some of everything the purge has to get past:
// posted journal entries, a payment, credits, a logged set against the
// shared exercise library, and a stored file.
func (h *harness) busyPractice(token string) {
	h.t.Helper()
	invoiceID, _ := h.issueSharedInvoice(token)
	if status, body := h.do(http.MethodPost, "/v1/invoices/"+invoiceID+"/payments", token, map[string]any{
		"amount_minor": 50000, "instrument": "cash",
	}); status != http.StatusCreated {
		h.t.Fatalf("payment = %d %v", status, body)
	}
	_, client := h.do(http.MethodPost, "/v1/clients", token, map[string]any{"full_name": "Dana"})
	clientID := client["id"].(string)
	_, library := h.do(http.MethodGet, "/v1/exercises?search=squat", token, nil)
	squat := library["exercises"].([]any)[0].(map[string]any)["id"].(string)
	status, workout := h.do(http.MethodPost, "/v1/workouts", token, map[string]any{"client_id": clientID})
	if status != http.StatusCreated {
		h.t.Fatalf("start workout = %d %v", status, workout)
	}
	if status, body := h.do(http.MethodPost, "/v1/workouts/"+workout["id"].(string)+"/sets", token, map[string]any{
		"exercise_id": squat, "set_index": 1, "reps": 5, "load_grams": 100000,
	}); status != http.StatusOK {
		h.t.Fatalf("log set = %d %v", status, body)
	}
	if status, body := h.do(http.MethodPost, "/v1/media/uploads", token, map[string]any{
		"kind": "progress_photo", "content_type": "image/jpeg", "byte_size": 1000, "client_id": clientID,
	}); status != http.StatusCreated {
		h.t.Fatalf("upload = %d %v", status, body)
	}
}

func TestDeletingAPracticeSignsEveryoneOutThenPurgesIt(t *testing.T) {
	h := newHarness(t)
	doomed := h.signupSession("owner@doomed.test")
	h.busyPractice(doomed.access)
	kept := h.signupSession("owner@kept.test")
	h.busyPractice(kept.access)

	// The wrong password deletes nothing.
	status, body := h.do(http.MethodPost, "/v1/session/delete-account", doomed.access,
		map[string]any{"password": "not-my-password"})
	if status == http.StatusOK || errorCode(body) != "invalid_credentials" {
		t.Fatalf("delete with the wrong password = %d %v", status, body)
	}

	status, body = h.do(http.MethodPost, "/v1/session/delete-account", doomed.access,
		map[string]any{"password": password})
	if status != http.StatusOK || body["scope"] != "practice" {
		t.Fatalf("delete = %d %v", status, body)
	}
	purgeAfter, err := time.Parse(time.RFC3339Nano, body["purge_after"].(string))
	if err != nil || time.Until(purgeAfter) < 29*24*time.Hour {
		t.Errorf("purge_after = %v, want about 30 days from now", body["purge_after"])
	}

	// Nobody gets back in: not by password, not by a refresh token.
	if status, _ := h.do(http.MethodPost, "/v1/auth/login", "", map[string]any{
		"email": "owner@doomed.test", "password": password,
	}); status != http.StatusUnauthorized {
		t.Errorf("sign-in after deletion = %d, want 401", status)
	}
	if status, _ := h.do(http.MethodPost, "/v1/auth/refresh", "", map[string]any{
		"refresh_token": doomed.refresh,
	}); status != http.StatusUnauthorized {
		t.Errorf("refresh after deletion = %d, want 401", status)
	}

	pool := testsupport.OpenApp(t)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	var storage removedPrefixes
	purge := jobs.AccountPurge(30*24*time.Hour, &storage)

	// Within the grace period nothing is removed.
	early := jobs.NewRunner(pool, clock.Fixed{T: time.Now().Add(10 * 24 * time.Hour)}, log, time.Minute, purge)
	if report, err := early.Tick(context.Background()); err != nil || report.Affected != 0 || report.Failed != 0 {
		t.Fatalf("purge during the grace period: %+v %v", report, err)
	}
	if n := countRows(t, "tenants"); n != 2 {
		t.Fatalf("%d tenants after an early sweep, want 2", n)
	}

	// After it, the practice goes: every row, and its files.
	late := jobs.NewRunner(pool, clock.Fixed{T: time.Now().Add(31 * 24 * time.Hour)}, log, time.Minute, purge)
	report, err := late.Tick(context.Background())
	if err != nil || report.Failed != 0 || report.Affected != 1 {
		t.Fatalf("purge = %+v %v", report, err)
	}
	if len(storage) != 1 || !strings.HasPrefix(storage[0], doomed.tenantID+"/") {
		t.Errorf("storage asked to remove %v, want only %s/", storage, doomed.tenantID)
	}
	owner := testsupport.OpenOwner(t)
	for _, table := range []string{"tenants", "users", "journal_entries", "journal_lines", "payments",
		"invoices", "credit_transactions", "set_logs", "media_objects", "clients", "refresh_tokens"} {
		var left int
		if err := owner.Raw().QueryRow(context.Background(),
			`SELECT count(*) FROM `+table+` WHERE `+tenantColumn(table)+` = $1`, doomed.tenantID).Scan(&left); err != nil {
			t.Fatalf("%s: %v", table, err)
		}
		if left != 0 {
			t.Errorf("%s: %d rows of the purged practice remain", table, left)
		}
	}

	// The other practice is untouched and still works.
	if status, _ := h.do(http.MethodGet, "/v1/receivables", kept.access, nil); status != http.StatusOK {
		t.Errorf("the other practice broke: %d", status)
	}
	if n := countRows(t, "journal_entries"); n == 0 {
		t.Error("the other practice's journal went too")
	}
}

func TestTheAppRoleCannotDeleteThroughTheLedger(t *testing.T) {
	h := newHarness(t)
	s := h.signupSession("owner@x.test")
	h.busyPractice(s.access)

	// Even naming the purge flag, a statement the app role issues itself is
	// refused: the exception holds only inside the purge function.
	pool := testsupport.OpenApp(t)
	ctx := context.Background()
	tx, err := pool.Raw().Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for _, q := range []string{
		`SELECT set_config('app.tenant_id', '` + s.tenantID + `', true)`,
		`SELECT set_config('app.purging_tenant', '` + s.tenantID + `', true)`,
	} {
		if _, err := tx.Exec(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM journal_lines`); err == nil || !strings.Contains(err.Error(), "immutable") {
		t.Errorf("the app role deleted journal lines: %v", err)
	}
}

func TestTheAppRoleCannotDeleteATenant(t *testing.T) {
	h := newHarness(t)
	s := h.signupSession("owner@x.test")
	pool := testsupport.OpenApp(t)
	ctx := context.Background()
	tx, err := pool.Raw().Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, s.tenantID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM tenants`); err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Errorf("the app role could delete its tenant: %v", err)
	}
}

func countRows(t *testing.T, table string) int {
	t.Helper()
	var n int
	if err := testsupport.OpenOwner(t).Raw().QueryRow(context.Background(), `SELECT count(*) FROM `+table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func tenantColumn(table string) string {
	if table == "tenants" {
		return "id"
	}
	return "tenant_id"
}
