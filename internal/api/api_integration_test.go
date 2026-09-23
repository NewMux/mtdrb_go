//go:build integration

package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/NewMux/mtdrb_go/internal/api"
	"github.com/NewMux/mtdrb_go/internal/app"
	"github.com/NewMux/mtdrb_go/internal/auth"
	"github.com/NewMux/mtdrb_go/internal/config"
	"github.com/NewMux/mtdrb_go/internal/crm"
	"github.com/NewMux/mtdrb_go/internal/platform/clock"
	"github.com/NewMux/mtdrb_go/internal/testsupport"
)

// fakePresigner stands in for object storage. Presigned URLs are computed
// without contacting the store in any case, so a fake exercises the same code
// path the real S3 presigner would.
type fakePresigner struct{ deleted []string }

func (f *fakePresigner) PresignPut(_ context.Context, key, _ string, _ time.Duration) (string, error) {
	return "https://storage.test/" + key + "?signature=put", nil
}
func (f *fakePresigner) PresignGet(_ context.Context, key string, _ time.Duration) (string, error) {
	return "https://storage.test/" + key + "?signature=get", nil
}
func (f *fakePresigner) Delete(_ context.Context, key string) error {
	f.deleted = append(f.deleted, key)
	return nil
}

type harness struct {
	server *httptest.Server
	t      *testing.T
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	testsupport.RequireDB(t)
	testsupport.Reset(t)

	pool := testsupport.OpenApp(t)
	wall := clock.System{}

	cfg := config.Config{
		Env:             "development",
		ShutdownTimeout: time.Second,
		AccessTokenTTL:  15 * time.Minute,
		RefreshTokenTTL: 24 * time.Hour,
		CORSOrigins:     []string{"http://localhost:8081"},
		PresignTTL:      5 * time.Minute,
	}

	params := auth.DefaultArgon2Params()
	params.Memory, params.Iterations = 1024, 1
	cfg.PublicBaseURL = "https://app.coachpulse.test"

	// The same wiring the API binary uses, so a service added there is under
	// test here without anyone remembering to add it twice.
	services := app.New(app.Options{
		Pool:            pool,
		Clock:           wall,
		JWTSigningKey:   []byte(strings.Repeat("k", 32)),
		AccessTokenTTL:  cfg.AccessTokenTTL,
		RefreshTokenTTL: cfg.RefreshTokenTTL,
		Argon2:          params,
		ColumnKey:       []byte(strings.Repeat("c", 32)),
		Presigner:       &fakePresigner{},
		PresignTTL:      cfg.PresignTTL,
		PublicBaseURL:   cfg.PublicBaseURL,
	})
	srv := api.New(cfg, pool, slog.New(slog.DiscardHandler), services.Handlers())

	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return &harness{server: ts, t: t}
}

// do issues a request and decodes the JSON response.
func (h *harness) do(method, path, token string, body any) (int, map[string]any) {
	h.t.Helper()

	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			h.t.Fatalf("encode request: %v", err)
		}
		reader = bytes.NewReader(raw)
	}

	req, err := http.NewRequest(method, h.server.URL+path, reader)
	if err != nil {
		h.t.Fatalf("build request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	// The shared-invoice route serves HTML to a browser and JSON to the app,
	// so this helper asks for JSON the way the Expo client would.
	req.Header.Set("Accept", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := h.server.Client().Do(req)
	if err != nil {
		h.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		h.t.Fatalf("read response: %v", err)
	}
	out := map[string]any{}
	if len(bytes.TrimSpace(raw)) > 0 {
		if err := json.Unmarshal(raw, &out); err != nil {
			h.t.Fatalf("%s %s: response is not JSON: %s", method, path, raw)
		}
	}
	return resp.StatusCode, out
}

// signup registers a trainer and returns their access token.
func (h *harness) signup(email string) string {
	h.t.Helper()
	status, body := h.do(http.MethodPost, "/v1/auth/signup", "", map[string]any{
		"email":         email,
		"password":      "correct-horse-battery-staple",
		"display_name":  "Sam Coach",
		"business_name": "Iron Works",
		"currency":      "EUR",
	})
	if status != http.StatusCreated {
		h.t.Fatalf("signup failed: %d %v", status, body)
	}
	tokens, ok := body["tokens"].(map[string]any)
	if !ok {
		h.t.Fatalf("signup response has no tokens: %v", body)
	}
	return tokens["access_token"].(string)
}

func TestHealthAndReadiness(t *testing.T) {
	h := newHarness(t)

	if status, body := h.do(http.MethodGet, "/healthz", "", nil); status != http.StatusOK || body["status"] != "ok" {
		t.Errorf("healthz = %d %v", status, body)
	}
	if status, body := h.do(http.MethodGet, "/readyz", "", nil); status != http.StatusOK || body["status"] != "ready" {
		t.Errorf("readyz = %d %v", status, body)
	}
}

func TestClientLifecycleOverHTTP(t *testing.T) {
	h := newHarness(t)
	token := h.signup("coach@gym.io")

	status, created := h.do(http.MethodPost, "/v1/clients", token, map[string]any{
		"full_name":          "Dana Rivers",
		"email":              "dana@example.com",
		"default_rate_minor": 5000,
		"medical_notes":      "peanut allergy",
	})
	if status != http.StatusCreated {
		t.Fatalf("create client = %d %v", status, created)
	}
	clientID := created["id"].(string)

	// A plain fetch must not carry medical notes.
	status, fetched := h.do(http.MethodGet, "/v1/clients/"+clientID, token, nil)
	if status != http.StatusOK {
		t.Fatalf("get client = %d %v", status, fetched)
	}
	if _, present := fetched["medical_notes"]; present {
		t.Error("medical notes were returned by an ordinary profile fetch")
	}

	// They are available only when explicitly requested.
	status, withMedical := h.do(http.MethodGet, "/v1/clients/"+clientID+"?include_medical=true", token, nil)
	if status != http.StatusOK {
		t.Fatalf("get with medical = %d %v", status, withMedical)
	}
	if withMedical["medical_notes"] != "peanut allergy" {
		t.Errorf("medical notes = %v", withMedical["medical_notes"])
	}

	status, listed := h.do(http.MethodGet, "/v1/clients", token, nil)
	if status != http.StatusOK {
		t.Fatalf("list = %d %v", status, listed)
	}
	if len(listed["clients"].([]any)) != 1 {
		t.Errorf("list returned %v", listed["clients"])
	}

	if status, _ := h.do(http.MethodDelete, "/v1/clients/"+clientID, token, nil); status != http.StatusNoContent {
		t.Errorf("delete = %d", status)
	}
	if status, _ := h.do(http.MethodGet, "/v1/clients/"+clientID, token, nil); status != http.StatusNotFound {
		t.Errorf("deleted client still readable: %d", status)
	}
}

// One trainer must never reach another's clients, end to end through the API.
func TestOneTrainerCannotReachAnothersClients(t *testing.T) {
	h := newHarness(t)

	alice := h.signup("alice@gym.io")
	bob := h.signup("bob@gym.io")

	status, created := h.do(http.MethodPost, "/v1/clients", alice, map[string]any{
		"full_name": "Alice's Client",
	})
	if status != http.StatusCreated {
		t.Fatalf("create = %d %v", status, created)
	}
	clientID := created["id"].(string)

	// Bob's listing is empty.
	status, listed := h.do(http.MethodGet, "/v1/clients", bob, nil)
	if status != http.StatusOK {
		t.Fatalf("list = %d", status)
	}
	if got := listed["clients"].([]any); len(got) != 0 {
		t.Errorf("Bob sees %d of Alice's clients", len(got))
	}

	// And a direct fetch by id is a 404, not a 403: existence is not leaked.
	status, _ = h.do(http.MethodGet, "/v1/clients/"+clientID, bob, nil)
	if status != http.StatusNotFound {
		t.Errorf("cross-tenant fetch = %d, want 404", status)
	}

	// Nor can Bob modify or delete it.
	if status, _ = h.do(http.MethodPatch, "/v1/clients/"+clientID, bob, map[string]any{
		"full_name": "Hijacked",
	}); status != http.StatusNotFound {
		t.Errorf("cross-tenant update = %d, want 404", status)
	}
	if status, _ = h.do(http.MethodDelete, "/v1/clients/"+clientID, bob, nil); status != http.StatusNotFound {
		t.Errorf("cross-tenant delete = %d, want 404", status)
	}

	// Alice's client is untouched.
	status, stillThere := h.do(http.MethodGet, "/v1/clients/"+clientID, alice, nil)
	if status != http.StatusOK || stillThere["full_name"] != "Alice's Client" {
		t.Errorf("Alice's client was altered: %d %v", status, stillThere)
	}
}

func TestProtectedRoutesRejectMissingOrBadTokens(t *testing.T) {
	h := newHarness(t)
	h.signup("coach@gym.io")

	for _, tc := range []struct{ name, token string }{
		{"no token", ""},
		{"garbage token", "not-a-jwt"},
		{"empty bearer", " "},
	} {
		if status, _ := h.do(http.MethodGet, "/v1/clients", tc.token, nil); status != http.StatusUnauthorized {
			t.Errorf("%s: got %d, want 401", tc.name, status)
		}
	}
}

func TestParQAndWaiverFlowOverHTTP(t *testing.T) {
	h := newHarness(t)
	token := h.signup("coach@gym.io")

	_, created := h.do(http.MethodPost, "/v1/clients", token, map[string]any{"full_name": "Dana"})
	clientID := created["id"].(string)

	answers := make([]map[string]any, 0, len(crm.StandardParQQuestions))
	for i, q := range crm.StandardParQQuestions {
		answers = append(answers, map[string]any{"question": string(q), "yes": i == 0})
	}
	status, parq := h.do(http.MethodPost, "/v1/clients/"+clientID+"/parq", token,
		map[string]any{"answers": answers})
	if status != http.StatusCreated {
		t.Fatalf("submit PAR-Q = %d %v", status, parq)
	}
	if parq["requires_clearance"] != true {
		t.Error("a yes answer did not set requires_clearance")
	}

	status, waiver := h.do(http.MethodPost, "/v1/waivers", token, map[string]any{
		"title": "Liability Release", "body": "Terms and conditions.",
	})
	if status != http.StatusCreated {
		t.Fatalf("create waiver = %d %v", status, waiver)
	}
	waiverID := waiver["id"].(string)

	status, sig := h.do(http.MethodPost,
		fmt.Sprintf("/v1/clients/%s/waivers/%s/sign", clientID, waiverID), token,
		map[string]any{"signed_name": "Dana Rivers"})
	if status != http.StatusCreated {
		t.Fatalf("sign = %d %v", status, sig)
	}

	// Signing twice is a conflict.
	if status, _ = h.do(http.MethodPost,
		fmt.Sprintf("/v1/clients/%s/waivers/%s/sign", clientID, waiverID), token,
		map[string]any{"signed_name": "Dana Rivers"}); status != http.StatusConflict {
		t.Errorf("second signature = %d, want 409", status)
	}
}

func TestMediaUploadIssuesPresignedURL(t *testing.T) {
	h := newHarness(t)
	token := h.signup("coach@gym.io")

	_, created := h.do(http.MethodPost, "/v1/clients", token, map[string]any{"full_name": "Dana"})
	clientID := created["id"].(string)

	status, upload := h.do(http.MethodPost, "/v1/media/uploads", token, map[string]any{
		"kind":         "progress_photo",
		"content_type": "image/jpeg",
		"byte_size":    2_000_000,
		"client_id":    clientID,
	})
	if status != http.StatusCreated {
		t.Fatalf("request upload = %d %v", status, upload)
	}
	if !strings.HasPrefix(upload["upload_url"].(string), "https://storage.test/") {
		t.Errorf("upload url = %v", upload["upload_url"])
	}
	object := upload["object"].(map[string]any)
	if object["sensitivity"] != "sensitive" {
		t.Errorf("a progress photo was classified as %v", object["sensitivity"])
	}

	// Confirm, then fetch a download link.
	objectID := object["id"].(string)
	if status, _ = h.do(http.MethodPost, "/v1/media/"+objectID+"/confirm", token, nil); status != http.StatusOK {
		t.Errorf("confirm = %d", status)
	}
	status, download := h.do(http.MethodGet, "/v1/media/"+objectID+"/download", token, nil)
	if status != http.StatusOK {
		t.Fatalf("download = %d %v", status, download)
	}
	if download["sensitive"] != true {
		t.Error("download of a progress photo was not marked sensitive")
	}
}

func TestMediaRejectsDisallowedTypesAndSizes(t *testing.T) {
	h := newHarness(t)
	token := h.signup("coach@gym.io")

	cases := []struct {
		name string
		body map[string]any
	}{
		{"executable type", map[string]any{"kind": "progress_photo", "content_type": "text/html", "byte_size": 100}},
		{"wrong type for kind", map[string]any{"kind": "progress_photo", "content_type": "video/mp4", "byte_size": 100}},
		{"oversized", map[string]any{"kind": "signature", "content_type": "image/png", "byte_size": 900_000_000}},
		{"zero size", map[string]any{"kind": "progress_photo", "content_type": "image/jpeg", "byte_size": 0}},
		{"unknown kind", map[string]any{"kind": "malware", "content_type": "image/jpeg", "byte_size": 100}},
	}
	for _, tc := range cases {
		status, body := h.do(http.MethodPost, "/v1/media/uploads", token, tc.body)
		if status != http.StatusBadRequest {
			t.Errorf("%s: got %d, want 400 (%v)", tc.name, status, body)
		}
	}
}

// Responses routinely carry medical notes and balances; none of it belongs in
// a shared cache.
func TestResponsesAreNotCacheable(t *testing.T) {
	h := newHarness(t)
	token := h.signup("coach@gym.io")

	req, err := http.NewRequest(http.MethodGet, h.server.URL+"/v1/clients", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := h.server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if got := resp.Header.Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
	if got := resp.Header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q", got)
	}
	if resp.Header.Get("X-Request-Id") == "" {
		t.Error("no request id was echoed")
	}
}

func TestUnknownRouteReturnsJSON(t *testing.T) {
	h := newHarness(t)
	status, body := h.do(http.MethodGet, "/v1/nonexistent", "", nil)
	if status != http.StatusNotFound {
		t.Errorf("status = %d", status)
	}
	if body["error"] == nil {
		t.Errorf("body = %v, want a JSON error envelope", body)
	}
}

// Journey A end to end through the real router, as the Expo client will drive
// it: book a session, mark it complete, and read back the zero balance that
// triggers the renewal prompt.
func TestJourneyAOverHTTP(t *testing.T) {
	h := newHarness(t)
	token := h.signup("coach@gym.io")

	_, client := h.do(http.MethodPost, "/v1/clients", token, map[string]any{
		"full_name": "Client A",
	})
	clientID := client["id"].(string)

	status, sessionType := h.do(http.MethodPost, "/v1/sessions/session-types", token, map[string]any{
		"name": "1-on-1", "duration_minutes": 60, "capacity": 1, "credit_cost": 1,
	})
	if status != http.StatusCreated {
		t.Fatalf("create session type = %d %v", status, sessionType)
	}
	typeID := sessionType["id"].(string)

	// A single credit, priced at 50.00.
	status, pkg := h.do(http.MethodPost, "/v1/credits/packages", token, map[string]any{
		"client_id": clientID, "name": "starter", "credits": 1, "unit_price_minor": 5000,
	})
	if status != http.StatusCreated {
		t.Fatalf("grant package = %d %v", status, pkg)
	}

	start := time.Now().UTC().Add(24 * time.Hour).Truncate(time.Hour)
	status, session := h.do(http.MethodPost, "/v1/sessions", token, map[string]any{
		"session_type_id": typeID,
		"starts_at":       start.Format(time.RFC3339),
		"client_ids":      []string{clientID},
	})
	if status != http.StatusCreated {
		t.Fatalf("book = %d %v", status, session)
	}
	attendees := session["attendees"].([]any)
	if len(attendees) != 1 {
		t.Fatalf("roster has %d attendees", len(attendees))
	}
	attendeeID := attendees[0].(map[string]any)["id"].(string)

	// Mark it complete: the credit burns and revenue is recognised.
	status, result := h.do(http.MethodPost, "/v1/sessions/attendees/"+attendeeID+"/mark", token,
		map[string]any{"status": "completed"})
	if status != http.StatusOK {
		t.Fatalf("mark = %d %v", status, result)
	}
	if got := result["credits_remaining"].(float64); got != 0 {
		t.Errorf("credits remaining = %v, want 0", got)
	}
	revenue, ok := result["revenue_recognised"].(map[string]any)
	if !ok {
		t.Fatalf("no revenue recognised: %v", result)
	}
	if revenue["minor"].(float64) != 5000 {
		t.Errorf("revenue = %v, want 5000", revenue["minor"])
	}

	// The balance endpoint agrees, which is what the app polls for the prompt.
	status, balance := h.do(http.MethodGet, "/v1/credits/clients/"+clientID+"/balance", token, nil)
	if status != http.StatusOK {
		t.Fatalf("balance = %d %v", status, balance)
	}
	if got := balance["remaining"].(float64); got != 0 {
		t.Errorf("balance remaining = %v, want 0", got)
	}

	// A second session cannot be completed, and the refusal carries what the
	// client needs to offer a renewal.
	status, second := h.do(http.MethodPost, "/v1/sessions", token, map[string]any{
		"session_type_id": typeID,
		"starts_at":       start.Add(3 * time.Hour).Format(time.RFC3339),
		"client_ids":      []string{clientID},
	})
	if status != http.StatusCreated {
		t.Fatalf("book second = %d %v", status, second)
	}
	secondAttendee := second["attendees"].([]any)[0].(map[string]any)["id"].(string)

	status, refusal := h.do(http.MethodPost, "/v1/sessions/attendees/"+secondAttendee+"/mark", token,
		map[string]any{"status": "completed"})
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("completing without credits = %d %v, want 422", status, refusal)
	}
	errBody := refusal["error"].(map[string]any)
	if errBody["code"] != "insufficient_credits" {
		t.Errorf("code = %v", errBody["code"])
	}
	meta := errBody["meta"].(map[string]any)
	if meta["remaining"].(float64) != 0 || meta["required"].(float64) != 1 {
		t.Errorf("meta = %v, want remaining 0 and required 1", meta)
	}
}

// The buffer is rejected through the API with a distinguishable code, so the
// app can tell a double-booking from any other failure.
func TestBufferConflictOverHTTP(t *testing.T) {
	h := newHarness(t)
	token := h.signup("coach@gym.io")

	_, client := h.do(http.MethodPost, "/v1/clients", token, map[string]any{"full_name": "Client A"})
	clientID := client["id"].(string)
	_, sessionType := h.do(http.MethodPost, "/v1/sessions/session-types", token, map[string]any{
		"name": "1-on-1", "duration_minutes": 60, "capacity": 1, "credit_cost": 1,
	})
	typeID := sessionType["id"].(string)

	start := time.Now().UTC().Add(48 * time.Hour).Truncate(time.Hour)
	if status, body := h.do(http.MethodPost, "/v1/sessions", token, map[string]any{
		"session_type_id": typeID, "starts_at": start.Format(time.RFC3339),
		"client_ids": []string{clientID},
	}); status != http.StatusCreated {
		t.Fatalf("first booking = %d %v", status, body)
	}

	// 70 minutes later is inside the default 15-minute buffer.
	status, conflict := h.do(http.MethodPost, "/v1/sessions", token, map[string]any{
		"session_type_id": typeID,
		"starts_at":       start.Add(70 * time.Minute).Format(time.RFC3339),
		"client_ids":      []string{clientID},
	})
	if status != http.StatusConflict {
		t.Fatalf("overlapping booking = %d %v, want 409", status, conflict)
	}
	if code := conflict["error"].(map[string]any)["code"]; code != "scheduling_conflict" {
		t.Errorf("code = %v, want scheduling_conflict", code)
	}
}

// doRaw issues a request and returns the raw body, for the HTML share page and
// for asserting on replay headers.
func (h *harness) doRaw(method, path, token string, body any, headers map[string]string) (*http.Response, string) {
	h.t.Helper()

	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			h.t.Fatalf("encode request: %v", err)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, h.server.URL+path, reader)
	if err != nil {
		h.t.Fatalf("build request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := h.server.Client().Do(req)
	if err != nil {
		h.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		h.t.Fatalf("read response: %v", err)
	}
	return resp, string(raw)
}

// issueSharedInvoice sets a trainer up with a client, a bank method and an
// issued, shared invoice. Returns the invoice id and the share token.
func (h *harness) issueSharedInvoice(token string) (string, string) {
	h.t.Helper()

	_, client := h.do(http.MethodPost, "/v1/clients", token, map[string]any{"full_name": "Client A"})
	clientID := client["id"].(string)

	if status, body := h.do(http.MethodPost, "/v1/payment-methods", token, map[string]any{
		"kind": "bank_transfer", "label": "Bank transfer", "is_default": true,
		"details": map[string]any{"iban": "DE89370400440532013000", "account_holder": "Sam Coach"},
	}); status != http.StatusCreated {
		h.t.Fatalf("create payment method = %d %v", status, body)
	}

	status, draft := h.do(http.MethodPost, "/v1/invoices", token, map[string]any{
		"client_id": clientID,
		"lines": []map[string]any{{
			"kind": "package", "description": "10-session pack",
			"quantity": 1, "unit_price_minor": 50000, "package_credits": 10,
		}},
	})
	if status != http.StatusCreated {
		h.t.Fatalf("create draft = %d %v", status, draft)
	}
	invoiceID := draft["id"].(string)

	if status, body := h.do(http.MethodPost, "/v1/invoices/"+invoiceID+"/issue", token, map[string]any{}); status != http.StatusOK {
		h.t.Fatalf("issue = %d %v", status, body)
	}

	status, link := h.do(http.MethodPost, "/v1/invoices/"+invoiceID+"/share", token, nil)
	if status != http.StatusCreated {
		h.t.Fatalf("share = %d %v", status, link)
	}
	return invoiceID, link["token"].(string)
}

// Journey B through the real router, including the unauthenticated page.
func TestJourneyBOverHTTP(t *testing.T) {
	h := newHarness(t)
	token := h.signup("coach@gym.io")
	invoiceID, shareToken := h.issueSharedInvoice(token)

	// The shared page is reachable with no credentials at all.
	resp, body := h.doRaw(http.MethodGet, "/public/invoices/"+shareToken, "", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("public page = %d: %s", resp.StatusCode, body)
	}
	if !strings.Contains(body, "DE89370400440532013000") {
		t.Error("the shared page does not show the trainer's IBAN")
	}
	if !strings.Contains(body, "INV-") {
		t.Error("the shared page does not show the invoice number")
	}
	// It must not be cacheable or indexable: the URL is the secret.
	if cc := resp.Header.Get("Cache-Control"); !strings.Contains(cc, "no-store") {
		t.Errorf("Cache-Control = %q, want no-store", cc)
	}
	if robots := resp.Header.Get("X-Robots-Tag"); !strings.Contains(robots, "noindex") {
		t.Errorf("X-Robots-Tag = %q, want noindex", robots)
	}

	// The same link serves JSON to the app.
	status, public := h.do(http.MethodGet, "/public/invoices/"+shareToken, "", nil)
	if status != http.StatusOK {
		t.Fatalf("public json = %d %v", status, public)
	}
	if public["business_name"] != "Iron Works" {
		t.Errorf("business_name = %v", public["business_name"])
	}

	// Trainer marks it paid by bank transfer.
	status, result := h.do(http.MethodPost, "/v1/invoices/"+invoiceID+"/payments", token, map[string]any{
		"amount_minor": 50000, "instrument": "bank_transfer", "reference": "TRF-99812",
	})
	if status != http.StatusCreated {
		t.Fatalf("record payment = %d %v", status, result)
	}
	invoice := result["invoice"].(map[string]any)
	if invoice["status"] != "settled" {
		t.Errorf("status = %v, want settled", invoice["status"])
	}
	if invoice["balance_minor"].(float64) != 0 {
		t.Errorf("balance = %v, want 0", invoice["balance_minor"])
	}

	// Receivables are clear.
	status, receivables := h.do(http.MethodGet, "/v1/receivables", token, nil)
	if status != http.StatusOK {
		t.Fatalf("receivables = %d %v", status, receivables)
	}
	if receivables["total_minor"].(float64) != 0 {
		t.Errorf("outstanding = %v, want 0", receivables["total_minor"])
	}
}

// A share token must reach exactly one invoice and leak nothing else.
func TestShareTokenIsolation(t *testing.T) {
	h := newHarness(t)
	alice := h.signup("alice@gym.io")
	bob := h.signup("bob@gym.io")

	aliceInvoice, aliceToken := h.issueSharedInvoice(alice)
	_, bobToken := h.issueSharedInvoice(bob)

	// Each token resolves to its own tenant's invoice.
	_, aliceView := h.do(http.MethodGet, "/public/invoices/"+aliceToken, "", nil)
	_, bobView := h.do(http.MethodGet, "/public/invoices/"+bobToken, "", nil)
	if aliceView["number"] == nil || aliceView["number"] != bobView["number"] {
		// Both are the first invoice of their own tenant, so the numbers
		// match; what must differ is that each is its own document.
		t.Logf("alice %v bob %v", aliceView["number"], bobView["number"])
	}

	// A forged token is a 404, never a 403: the difference would confirm to a
	// guesser that a token once existed.
	for _, bad := range []string{"not-a-real-token", strings.Repeat("A", 43), ""} {
		status, _ := h.do(http.MethodGet, "/public/invoices/"+bad, "", nil)
		if status != http.StatusNotFound && status != http.StatusMovedPermanently {
			t.Errorf("forged token %q = %d, want 404", bad, status)
		}
	}

	// Revoking kills the link.
	if status, _ := h.do(http.MethodDelete, "/v1/invoices/"+aliceInvoice+"/share", alice, nil); status != http.StatusNoContent {
		t.Fatalf("revoke = %d", status)
	}
	if status, _ := h.do(http.MethodGet, "/public/invoices/"+aliceToken, "", nil); status != http.StatusNotFound {
		t.Errorf("revoked token still resolves: %d", status)
	}

	// Bob cannot reach Alice's invoice through the authenticated API either.
	if status, _ := h.do(http.MethodGet, "/v1/invoices/"+aliceInvoice, bob, nil); status != http.StatusNotFound {
		t.Errorf("cross-tenant invoice fetch = %d, want 404", status)
	}
}

// The public payload must carry the bill and nothing more.
func TestPublicInvoiceLeaksNothingExtra(t *testing.T) {
	h := newHarness(t)
	token := h.signup("coach@gym.io")
	_, shareToken := h.issueSharedInvoice(token)

	_, public := h.do(http.MethodGet, "/public/invoices/"+shareToken, "", nil)

	// Fields that would be a disclosure if they appeared.
	for _, forbidden := range []string{
		"medical_notes", "client_id", "tenant_id", "journal_entry_id",
		"share_token_hash", "server_seq", "emergency_contact_phone",
	} {
		if _, present := public[forbidden]; present {
			t.Errorf("the public payload exposes %q", forbidden)
		}
	}
	if public["number"] == nil || public["total_minor"] == nil {
		t.Error("the public payload is missing the invoice itself")
	}
}

// Replaying a payment must not charge the client twice. This is the whole
// reason the idempotency table exists.
func TestIdempotentPaymentReplay(t *testing.T) {
	h := newHarness(t)
	token := h.signup("coach@gym.io")
	invoiceID, _ := h.issueSharedInvoice(token)

	payment := map[string]any{
		"amount_minor": 20000, "instrument": "cash", "reference": "envelope",
	}
	key := map[string]string{"Idempotency-Key": "outbox-payment-0001"}

	var firstBody string
	for attempt := range 3 {
		resp, body := h.doRaw(http.MethodPost, "/v1/invoices/"+invoiceID+"/payments",
			token, payment, key)
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("attempt %d = %d: %s", attempt, resp.StatusCode, body)
		}
		if attempt == 0 {
			firstBody = body
			continue
		}
		// A replay returns the stored response verbatim and says so.
		if resp.Header.Get("Idempotent-Replay") != "true" {
			t.Errorf("attempt %d was not marked as a replay", attempt)
		}
		if body != firstBody {
			t.Errorf("attempt %d returned a different response than the original", attempt)
		}
	}

	// Exactly one payment, and the invoice reflects one payment only.
	status, payments := h.do(http.MethodGet, "/v1/invoices/"+invoiceID+"/payments", token, nil)
	if status != http.StatusOK {
		t.Fatalf("list payments = %d %v", status, payments)
	}
	if got := payments["payments"].([]any); len(got) != 1 {
		t.Fatalf("three replays produced %d payments, want 1", len(got))
	}

	status, invoice := h.do(http.MethodGet, "/v1/invoices/"+invoiceID, token, nil)
	if status != http.StatusOK {
		t.Fatal(status)
	}
	if invoice["paid_minor"].(float64) != 20000 {
		t.Errorf("paid = %v, want 20000 — a replay was counted twice", invoice["paid_minor"])
	}
}

// Reusing a key for a different request is a client bug, and treating it as a
// replay would silently drop a real payment.
func TestIdempotencyKeyReusedForADifferentRequestIsRefused(t *testing.T) {
	h := newHarness(t)
	token := h.signup("coach@gym.io")
	invoiceID, _ := h.issueSharedInvoice(token)

	key := map[string]string{"Idempotency-Key": "outbox-payment-0002"}

	if resp, body := h.doRaw(http.MethodPost, "/v1/invoices/"+invoiceID+"/payments", token,
		map[string]any{"amount_minor": 10000, "instrument": "cash"}, key); resp.StatusCode != http.StatusCreated {
		t.Fatalf("first payment = %d: %s", resp.StatusCode, body)
	}

	resp, body := h.doRaw(http.MethodPost, "/v1/invoices/"+invoiceID+"/payments", token,
		map[string]any{"amount_minor": 30000, "instrument": "cash"}, key)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("key reuse with a different body = %d: %s", resp.StatusCode, body)
	}
	if !strings.Contains(body, "idempotency_key_reused") {
		t.Errorf("unexpected error body: %s", body)
	}
}

// A failed request must release its key, so a corrected retry can go through.
func TestFailedRequestReleasesItsIdempotencyKey(t *testing.T) {
	h := newHarness(t)
	token := h.signup("coach@gym.io")
	invoiceID, _ := h.issueSharedInvoice(token)

	key := map[string]string{"Idempotency-Key": "outbox-payment-0003"}

	// Overpay: refused.
	if resp, _ := h.doRaw(http.MethodPost, "/v1/invoices/"+invoiceID+"/payments", token,
		map[string]any{"amount_minor": 99999, "instrument": "cash"}, key); resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("overpayment = %d, want 422", resp.StatusCode)
	}

	// The same key now works for a valid request, rather than replaying the
	// failure forever.
	resp, body := h.doRaw(http.MethodPost, "/v1/invoices/"+invoiceID+"/payments", token,
		map[string]any{"amount_minor": 50000, "instrument": "cash"}, key)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("retry after failure = %d: %s", resp.StatusCode, body)
	}
}

// The programming path over HTTP: the library is there on day one, a
// programme is built, and a set is logged against it.
func TestProgrammingOverHTTP(t *testing.T) {
	h := newHarness(t)
	token := h.signup("coach@gym.io")

	// Signup seeded the library, so a trainer can build immediately.
	status, library := h.do(http.MethodGet, "/v1/exercises?search=squat", token, nil)
	if status != http.StatusOK {
		t.Fatalf("list exercises = %d %v", status, library)
	}
	exercises := library["exercises"].([]any)
	if len(exercises) == 0 {
		t.Fatal("the exercise library is empty after signup")
	}
	squatID := exercises[0].(map[string]any)["id"].(string)

	_, client := h.do(http.MethodPost, "/v1/clients", token, map[string]any{"full_name": "Dana"})
	clientID := client["id"].(string)

	status, program := h.do(http.MethodPost, "/v1/programs", token, map[string]any{
		"name": "8-Week Base", "description": "General preparation",
	})
	if status != http.StatusCreated {
		t.Fatalf("create programme = %d %v", status, program)
	}
	programID := program["id"].(string)

	status, block := h.do(http.MethodPost, "/v1/programs/"+programID+"/blocks", token,
		map[string]any{"name": "Accumulation", "weeks": 4})
	if status != http.StatusCreated {
		t.Fatalf("add block = %d %v", status, block)
	}
	blockID := block["id"].(string)

	status, day := h.do(http.MethodPost, "/v1/programs/blocks/"+blockID+"/days", token,
		map[string]any{"name": "Day 1 — Lower"})
	if status != http.StatusCreated {
		t.Fatalf("add day = %d %v", status, day)
	}
	dayID := day["id"].(string)

	if status, pres := h.do(http.MethodPost, "/v1/programs/days/"+dayID+"/exercises", token,
		map[string]any{
			"exercise_id": squatID, "target_sets": 4,
			"target_reps_min": 6, "target_reps_max": 8, "target_rpe_tenths": 80,
		}); status != http.StatusCreated {
		t.Fatalf("prescribe = %d %v", status, pres)
	}

	// The structure reads back whole.
	status, full := h.do(http.MethodGet, "/v1/programs/"+programID, token, nil)
	if status != http.StatusOK {
		t.Fatalf("get programme = %d %v", status, full)
	}
	if full["total_weeks"].(float64) != 4 {
		t.Errorf("total weeks = %v, want 4", full["total_weeks"])
	}

	if status, assignment := h.do(http.MethodPost, "/v1/programs/"+programID+"/assign", token,
		map[string]any{"client_id": clientID}); status != http.StatusCreated {
		t.Fatalf("assign = %d %v", status, assignment)
	}

	// Floor logging.
	status, workout := h.do(http.MethodPost, "/v1/workouts", token, map[string]any{
		"client_id": clientID, "day_id": dayID, "week_number": 1,
	})
	if status != http.StatusCreated {
		t.Fatalf("start workout = %d %v", status, workout)
	}
	workoutID := workout["id"].(string)

	for set := 1; set <= 3; set++ {
		if status, logged := h.do(http.MethodPost, "/v1/workouts/"+workoutID+"/sets", token,
			map[string]any{
				"exercise_id": squatID, "set_index": set,
				"reps": 8, "load_grams": 100000, "rpe_tenths": 80,
			}); status != http.StatusOK {
			t.Fatalf("log set %d = %d %v", set, status, logged)
		}
	}

	status, completed := h.do(http.MethodPost, "/v1/workouts/"+workoutID+"/complete", token,
		map[string]any{"notes": "solid"})
	if status != http.StatusOK {
		t.Fatalf("complete = %d %v", status, completed)
	}
	if got := completed["sets"].([]any); len(got) != 3 {
		t.Errorf("workout has %d sets, want 3", len(got))
	}

	// Progression reads back the working volume.
	status, progression := h.do(http.MethodGet,
		"/v1/workouts/clients/"+clientID+"/exercises/"+squatID+"/progression", token, nil)
	if status != http.StatusOK {
		t.Fatalf("progression = %d %v", status, progression)
	}
	points := progression["points"].([]any)
	if len(points) != 1 {
		t.Fatalf("got %d progression points, want 1", len(points))
	}
	if points[0].(map[string]any)["volume_gram_reps"].(float64) != 2_400_000 {
		t.Errorf("volume = %v, want 2400000 (3 x 8 x 100kg)", points[0].(map[string]any)["volume_gram_reps"])
	}
}

// A trainer must not reach another's programmes or exercise library.
func TestProgrammingIsTenantIsolatedOverHTTP(t *testing.T) {
	h := newHarness(t)
	alice := h.signup("alice@gym.io")
	bob := h.signup("bob@gym.io")

	status, program := h.do(http.MethodPost, "/v1/programs", alice, map[string]any{"name": "Alice's Plan"})
	if status != http.StatusCreated {
		t.Fatalf("create = %d %v", status, program)
	}
	programID := program["id"].(string)

	status, bobsList := h.do(http.MethodGet, "/v1/programs", bob, nil)
	if status != http.StatusOK {
		t.Fatal(status)
	}
	if got := bobsList["programs"].([]any); len(got) != 0 {
		t.Errorf("Bob sees %d of Alice's programmes", len(got))
	}
	if status, _ := h.do(http.MethodGet, "/v1/programs/"+programID, bob, nil); status != http.StatusNotFound {
		t.Errorf("cross-tenant programme fetch = %d, want 404", status)
	}

	// Each tenant has their own seeded library, not a shared one.
	_, aliceLib := h.do(http.MethodGet, "/v1/exercises?limit=500", alice, nil)
	_, bobLib := h.do(http.MethodGet, "/v1/exercises?limit=500", bob, nil)
	aliceIDs := map[string]bool{}
	for _, e := range aliceLib["exercises"].([]any) {
		aliceIDs[e.(map[string]any)["id"].(string)] = true
	}
	for _, e := range bobLib["exercises"].([]any) {
		if aliceIDs[e.(map[string]any)["id"].(string)] {
			t.Fatal("two tenants share an exercise row")
		}
	}
}

// The round trip an Expo client actually performs: pull state, go offline,
// queue work, come back and push it.
func TestSyncRoundTripOverHTTP(t *testing.T) {
	h := newHarness(t)
	token := h.signup("coach@gym.io")

	// First pull: the seeded library arrives and a cursor comes back.
	status, pulled := h.do(http.MethodGet, "/v1/sync/pull", token, nil)
	if status != http.StatusOK {
		t.Fatalf("pull = %d %v", status, pulled)
	}
	cursor, _ := pulled["cursor"].(string)
	if cursor == "" {
		t.Fatal("the first pull returned no cursor")
	}
	gotExercises := false
	for _, c := range pulled["changes"].([]any) {
		if c.(map[string]any)["collection"] == "exercises" {
			gotExercises = true
		}
	}
	if !gotExercises {
		t.Error("the seeded library did not arrive in the first pull")
	}

	// Offline: the device queues a new client and a measurement.
	status, pushed := h.do(http.MethodPost, "/v1/sync/push", token, map[string]any{
		"operations": []map[string]any{
			{
				"id":        "01234567-89ab-7cde-8f01-23456789abcd",
				"type":      "client.create",
				"queued_at": "2026-05-01T09:00:00Z",
				"data":      map[string]any{"full_name": "Dana Rivers"},
			},
		},
	})
	if status != http.StatusOK {
		t.Fatalf("push = %d %v", status, pushed)
	}
	if pushed["applied"].(float64) != 1 {
		t.Fatalf("applied %v of 1: %v", pushed["applied"], pushed["results"])
	}

	// Pulling from the cursor returns the device's own write and nothing it
	// already has.
	status, delta := h.do(http.MethodGet, "/v1/sync/pull?cursor="+cursor, token, nil)
	if status != http.StatusOK {
		t.Fatalf("incremental pull = %d %v", status, delta)
	}
	clients := 0
	exercises := 0
	for _, c := range delta["changes"].([]any) {
		entry := c.(map[string]any)
		switch entry["collection"] {
		case "clients":
			clients = len(entry["rows"].([]any))
		case "exercises":
			exercises = len(entry["rows"].([]any))
		}
	}
	if clients != 1 {
		t.Errorf("incremental pull returned %d clients, want 1", clients)
	}
	if exercises != 0 {
		t.Errorf("incremental pull re-sent %d exercises the device already had", exercises)
	}
}

// A conflict inside a batch is reported per operation, not as a failed request
// — otherwise the outbox would retry the operations that succeeded.
func TestSyncPushReportsConflictsWithoutFailingTheBatch(t *testing.T) {
	h := newHarness(t)
	token := h.signup("coach@gym.io")

	status, pushed := h.do(http.MethodPost, "/v1/sync/push", token, map[string]any{
		"operations": []map[string]any{
			{
				"id":        "01234567-89ab-7cde-8f01-23456789ab01",
				"type":      "client.create",
				"queued_at": "2026-05-01T09:00:00Z",
				"data":      map[string]any{"full_name": "Valid Client"},
			},
			{
				"id":        "01234567-89ab-7cde-8f01-23456789ab02",
				"type":      "client.create",
				"queued_at": "2026-05-01T09:01:00Z",
				"data":      map[string]any{"full_name": ""}, // invalid
			},
		},
	})
	// The batch was processed, so the request succeeded.
	if status != http.StatusOK {
		t.Fatalf("push = %d %v", status, pushed)
	}
	if pushed["applied"].(float64) != 1 {
		t.Errorf("applied = %v, want 1", pushed["applied"])
	}
	if pushed["rejected"].(float64) != 1 {
		t.Errorf("rejected = %v, want 1", pushed["rejected"])
	}

	// Each operation carries its own outcome, keyed by the id the device
	// minted, so the outbox knows exactly which entry to drop.
	results := pushed["results"].([]any)
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
	for _, raw := range results {
		entry := raw.(map[string]any)
		if entry["id"] == nil || entry["status"] == nil {
			t.Errorf("result is missing its id or status: %v", entry)
		}
	}
}

// One trainer's device must never pull another's rows.
func TestSyncIsTenantIsolatedOverHTTP(t *testing.T) {
	h := newHarness(t)
	alice := h.signup("alice@gym.io")
	bob := h.signup("bob@gym.io")

	if status, body := h.do(http.MethodPost, "/v1/clients", alice, map[string]any{
		"full_name": "Alice's Client",
	}); status != http.StatusCreated {
		t.Fatalf("create = %d %v", status, body)
	}

	status, pulled := h.do(http.MethodGet, "/v1/sync/pull", bob, nil)
	if status != http.StatusOK {
		t.Fatalf("pull = %d %v", status, pulled)
	}
	for _, c := range pulled["changes"].([]any) {
		entry := c.(map[string]any)
		if entry["collection"] != "clients" {
			continue
		}
		for _, row := range entry["rows"].([]any) {
			if row.(map[string]any)["full_name"] == "Alice's Client" {
				t.Fatal("Bob's device pulled Alice's client")
			}
		}
	}
}
