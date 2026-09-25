//go:build integration

package api_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/NewMux/mtdrb_go/internal/subscription"
)

func TestLocationsHaveOnePrimaryAndCarryTheirNameOntoSessions(t *testing.T) {
	h := newHarness(t)
	s := h.signupSession("sam@rivera.test")

	status, studio := h.do(http.MethodPost, "/v1/locations", s.access, map[string]any{
		"name": "Rivera Strength Studio", "kind": "studio", "region": "Dubai", "colour": "#c8ff00",
	})
	if status != http.StatusCreated || studio["is_primary"] != true {
		t.Fatalf("first location = %d %v", status, studio)
	}
	status, beach := h.do(http.MethodPost, "/v1/locations", s.access, map[string]any{"name": "Kite Beach", "kind": "outdoor"})
	if status != http.StatusCreated || beach["is_primary"] != false {
		t.Fatalf("second location = %d %v", status, beach)
	}
	if status, body := h.do(http.MethodPost, "/v1/locations", s.access, map[string]any{"name": " kite beach "}); status != http.StatusConflict {
		t.Fatalf("a duplicate name was accepted: %d %v", status, body)
	}
	if status, body := h.do(http.MethodPost, "/v1/locations", s.access, map[string]any{"name": "Online", "kind": "the moon"}); status != http.StatusBadRequest {
		t.Fatalf("an unknown kind was accepted: %d %v", status, body)
	}

	studioID, beachID := studio["id"].(string), beach["id"].(string)
	if status, body := h.do(http.MethodPost, "/v1/locations/"+studioID+"/archive", s.access, nil); status != http.StatusConflict {
		t.Fatalf("the primary was archived with another place to take over: %d %v", status, body)
	}
	if status, body := h.do(http.MethodPatch, "/v1/locations/"+beachID, s.access, map[string]any{"is_primary": true}); status != http.StatusOK || body["is_primary"] != true {
		t.Fatalf("make primary = %d %v", status, body)
	}

	// A session booked at a place carries its name as it was.
	_, sessionType := h.do(http.MethodPost, "/v1/sessions/session-types", s.access, map[string]any{
		"name": "1-on-1", "duration_minutes": 60, "capacity": 1, "credit_cost": 1,
	})
	startsAt := time.Now().Add(48 * time.Hour).UTC().Truncate(time.Hour)
	status, session := h.do(http.MethodPost, "/v1/sessions", s.access, map[string]any{
		"session_type_id": sessionType["id"], "starts_at": startsAt, "location_id": studioID,
	})
	if status != http.StatusCreated || session["location"] != "Rivera Strength Studio" || session["location_id"] != studioID {
		t.Fatalf("book at a location = %d %v", status, session)
	}

	if status, body := h.do(http.MethodPost, "/v1/locations/"+studioID+"/archive", s.access, nil); status != http.StatusOK || body["archived_at"] == nil {
		t.Fatalf("archive = %d %v", status, body)
	}
	status, body := h.do(http.MethodPost, "/v1/sessions", s.access, map[string]any{
		"session_type_id": sessionType["id"], "starts_at": startsAt.Add(4 * time.Hour), "location_id": studioID,
	})
	if status != http.StatusConflict {
		t.Fatalf("booked at an archived location: %d %v", status, body)
	}

	_, listed := h.do(http.MethodGet, "/v1/locations", s.access, nil)
	if n := len(listed["locations"].([]any)); n != 1 {
		t.Fatalf("archived locations should be hidden by default, got %d", n)
	}
	_, all := h.do(http.MethodGet, "/v1/locations?include_archived=true", s.access, nil)
	if n := len(all["locations"].([]any)); n != 2 {
		t.Fatalf("include_archived should list both, got %d", n)
	}

	// The device mirrors both, and the session's link.
	_, pulled := h.do(http.MethodGet, "/v1/sync/pull", s.access, nil)
	text := fmt.Sprint(pulled["changes"])
	if !strings.Contains(text, "locations") || !strings.Contains(text, studioID) {
		t.Fatalf("pull did not carry the locations: %v", text)
	}
}

func TestStarterHoldsOneLocation(t *testing.T) {
	h := newHarness(t)
	s := h.signupSession("sam@rivera.test")
	starter, active := subscription.PlanStarter, subscription.StatusActive
	setPlan(t, s.tenantID, subscription.Change{Plan: &starter, Status: &active})

	_, first := h.do(http.MethodPost, "/v1/locations", s.access, map[string]any{"name": "Studio"})
	status, body := h.do(http.MethodPost, "/v1/locations", s.access, map[string]any{"name": "Beach"})
	if status != http.StatusForbidden || errorCode(body) != "plan_limit_reached" {
		t.Fatalf("second location on Starter = %d %v", status, body)
	}
	// Archiving frees the place; restoring takes it back.
	h.do(http.MethodPost, "/v1/locations/"+first["id"].(string)+"/archive", s.access, nil)
	if status, body := h.do(http.MethodPost, "/v1/locations", s.access, map[string]any{"name": "Beach"}); status != http.StatusCreated {
		t.Fatalf("location after archiving = %d %v", status, body)
	}
	if status, body := h.do(http.MethodPost, "/v1/locations/"+first["id"].(string)+"/restore", s.access, nil); status != http.StatusForbidden {
		t.Fatalf("restore past the limit = %d %v", status, body)
	}
}

func TestPackageOffersAreThePriceList(t *testing.T) {
	h := newHarness(t)
	s := h.signupSession("sam@rivera.test")

	if status, body := h.do(http.MethodPost, "/v1/package-offers", s.access, map[string]any{
		"name": "Pack", "kind": "session_pack", "price_minor": 350000,
	}); status != http.StatusBadRequest {
		t.Fatalf("a pack without sessions was accepted: %d %v", status, body)
	}
	status, pack := h.do(http.MethodPost, "/v1/package-offers", s.access, map[string]any{
		"name": "10 × personal training", "kind": "session_pack", "credits": 10,
		"price_minor": 350000, "validity_days": 90,
	})
	if status != http.StatusCreated || pack["currency"] != "AED" || pack["price_includes_vat"] != true || pack["cycle"] != "one_off" {
		t.Fatalf("create pack = %d %v", status, pack)
	}
	status, coaching := h.do(http.MethodPost, "/v1/package-offers", s.access, map[string]any{
		"name": "Online coaching", "kind": "online_coaching", "price_minor": 90000, "cycle": "monthly",
	})
	if status != http.StatusCreated || coaching["credits"] != nil {
		t.Fatalf("create coaching = %d %v", status, coaching)
	}

	id := pack["id"].(string)
	if status, body := h.do(http.MethodPatch, "/v1/package-offers/"+id, s.access, map[string]any{"clear_credits": true}); status != http.StatusBadRequest {
		t.Fatalf("a pack lost its sessions: %d %v", status, body)
	}
	if status, body := h.do(http.MethodPatch, "/v1/package-offers/"+id, s.access, map[string]any{"price_minor": 375000, "clear_validity": true}); status != http.StatusOK || body["price_minor"] != float64(375000) || body["validity_days"] != nil {
		t.Fatalf("update = %d %v", status, body)
	}
	if status, body := h.do(http.MethodPost, "/v1/package-offers/"+id+"/archive", s.access, nil); status != http.StatusOK || body["archived_at"] == nil {
		t.Fatalf("archive = %d %v", status, body)
	}
	_, listed := h.do(http.MethodGet, "/v1/package-offers", s.access, nil)
	offers := listed["offers"].([]any)
	if len(offers) != 1 || offers[0].(map[string]any)["name"] != "Online coaching" {
		t.Fatalf("list = %v", offers)
	}
}
