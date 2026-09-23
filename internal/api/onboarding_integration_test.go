//go:build integration

package api_test

import (
	"net/http"
	"testing"
)

func TestANewPracticeIsOnboardedOnceWithItsVATRegistration(t *testing.T) {
	h := newHarness(t)
	s := h.signupSession("sam@rivera.test")

	_, got := h.do(http.MethodGet, "/v1/settings", s.access, nil)
	if got["onboarded_at"] != nil || got["vat_registered"] != false {
		t.Fatalf("a new practice = %v", got)
	}

	// Saudi Arabia: a Sunday week and 15% VAT, unless told otherwise.
	_, got = h.do(http.MethodPatch, "/v1/settings", s.access, map[string]any{"country": "SA"})
	if got["vat_rate_bp"] != float64(1500) || got["week_start"] != float64(0) {
		t.Fatalf("after choosing SA = %v", got)
	}
	status, body := h.do(http.MethodPatch, "/v1/settings", s.access, map[string]any{"vat_registered": true})
	if status != http.StatusBadRequest {
		t.Fatalf("registered without a TRN = %d %v", status, body)
	}
	status, body = h.do(http.MethodPatch, "/v1/settings", s.access, map[string]any{
		"country": "AE", "vat_registered": true, "trn": "310123456700003",
	})
	if status != http.StatusBadRequest {
		t.Fatalf("a Saudi-shaped TRN was accepted for the UAE = %d %v", status, body)
	}
	status, got = h.do(http.MethodPatch, "/v1/settings", s.access, map[string]any{
		"country": "AE", "vat_registered": true, "trn": "100-1234-5670-0003", "prices_include_vat": true,
	})
	if status != http.StatusOK || got["trn"] != "100123456700003" || got["vat_rate_bp"] != float64(500) {
		t.Fatalf("UAE registration = %d %v", status, got)
	}

	status, first := h.do(http.MethodPost, "/v1/settings/onboarded", s.access, nil)
	if status != http.StatusOK || first["onboarded_at"] == nil {
		t.Fatalf("onboarded = %d %v", status, first)
	}
	_, second := h.do(http.MethodPost, "/v1/settings/onboarded", s.access, nil)
	if second["onboarded_at"] != first["onboarded_at"] {
		t.Fatalf("onboarding again moved the date: %v then %v", first["onboarded_at"], second["onboarded_at"])
	}
}
