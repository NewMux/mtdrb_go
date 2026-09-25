package legal

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPagesNameTheOperator(t *testing.T) {
	h := Routes(Operator{
		Entity: "CoachPulse Ltd", Address: "1 Example Street, Dubai", ContactEmail: "privacy@coachpulse.example",
		Jurisdiction: "the Emirate of Dubai", Effective: "1 October 2026", PurgeDays: 30,
	})
	for _, path := range []string{"/privacy", "/terms"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		body := rec.Body.String()
		if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") {
			t.Fatalf("%s = %d %s", path, rec.Code, rec.Header().Get("Content-Type"))
		}
		for _, want := range []string{"CoachPulse Ltd", "1 Example Street, Dubai", "privacy@coachpulse.example", "1 October 2026", "30 days"} {
			if !strings.Contains(body, want) {
				t.Errorf("%s does not say %q", path, want)
			}
		}
		if strings.Contains(body, "[") {
			t.Errorf("%s shows a placeholder though every value was given", path)
		}
	}
}

func TestDevelopmentShowsPlaceholders(t *testing.T) {
	rec := httptest.NewRecorder()
	Routes(Operator{}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/terms", nil))
	if !strings.Contains(rec.Body.String(), "[Operator legal name]") {
		t.Error("an unconfigured page should show plainly that it is unconfigured")
	}
}
