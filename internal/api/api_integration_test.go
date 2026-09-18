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
	"github.com/NewMux/mtdrb_go/internal/auth"
	"github.com/NewMux/mtdrb_go/internal/config"
	"github.com/NewMux/mtdrb_go/internal/crm"
	"github.com/NewMux/mtdrb_go/internal/ledger"
	"github.com/NewMux/mtdrb_go/internal/media"
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

	issuer := auth.NewTokenIssuer([]byte(strings.Repeat("k", 32)),
		cfg.AccessTokenTTL, cfg.RefreshTokenTTL, wall)

	params := auth.DefaultArgon2Params()
	params.Memory, params.Iterations = 1024, 1

	ledgerSvc := ledger.NewService(wall)
	authSvc := auth.NewService(pool, issuer, ledgerSvc, wall, params)
	crmSvc := crm.NewService(wall, []byte(strings.Repeat("c", 32)))
	mediaSvc := media.NewService(&fakePresigner{}, wall, cfg.PresignTTL)

	srv := api.New(cfg, pool, slog.New(slog.DiscardHandler), api.Deps{
		Auth:        auth.NewHandler(authSvc),
		CRM:         crm.NewHandler(crmSvc, pool),
		Media:       media.NewHandler(mediaSvc, pool),
		TokenIssuer: issuer,
	})

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
