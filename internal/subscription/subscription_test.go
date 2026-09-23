package subscription

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/NewMux/mtdrb_go/internal/platform/clock"
	"github.com/NewMux/mtdrb_go/internal/platform/ids"
	"github.com/NewMux/mtdrb_go/internal/tenancy"
)

var now = time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC)

func at(d time.Duration) *time.Time { t := now.Add(d); return &t }

func TestATrialIsTheWholeProductUntilItEnds(t *testing.T) {
	trial := State{Plan: PlanTrial, Status: StatusActive, TrialEndsAt: at(time.Hour)}
	if trial.Effective() != PlanPro || !trial.Allows(FeatureAnalytics) {
		t.Fatal("a trial should carry every Pro feature")
	}
	if _, capped := trial.LimitOf(LimitActiveClients); capped {
		t.Fatal("a trial should not cap clients")
	}
	if trial.Lapsed(now) {
		t.Fatal("a trial with an hour left has not lapsed")
	}
	if !trial.Lapsed(now.Add(time.Hour)) {
		t.Fatal("a trial lapses the instant it ends")
	}
}

func TestStarterLimitsAndFeatures(t *testing.T) {
	starter := State{Plan: PlanStarter, Status: StatusActive}
	if starter.Allows(FeatureShop) || starter.Allows(FeatureInsights) {
		t.Fatal("Starter has no shop or insights")
	}
	if n, capped := starter.LimitOf(LimitActiveClients); !capped || n != 25 {
		t.Fatalf("Starter holds 25 active clients, got %d (capped %v)", n, capped)
	}
	if len(starter.Features()) != 0 {
		t.Fatalf("Starter features: %v", starter.Features())
	}
}

func TestWhenAPaidPlanLapses(t *testing.T) {
	cases := []struct {
		name  string
		state State
		want  bool
	}{
		{"active", State{Plan: PlanPro, Status: StatusActive}, false},
		{"past due is a grace period", State{Plan: PlanPro, Status: StatusPastDue}, false},
		{"cancelled", State{Plan: PlanPro, Status: StatusCancelled}, true},
		{"cancelling, period not over", State{Plan: PlanPro, Status: StatusActive, CancelAtPeriodEnd: true,
			RenewsOn: at(0)}, false},
		{"cancelling, period over", State{Plan: PlanPro, Status: StatusActive, CancelAtPeriodEnd: true,
			RenewsOn: at(-48 * time.Hour)}, true},
	}
	for _, c := range cases {
		if got := c.state.Lapsed(now); got != c.want {
			t.Errorf("%s: lapsed = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestReadOnlyWhenLapsedRefusesWritesOnly(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	guard := ReadOnlyWhenLapsed(clock.Fixed{T: now})(ok)
	lapsed := tenancy.Principal{
		TenantID: ids.New(), SubjectID: ids.New(), Kind: tenancy.KindUser, Role: "owner",
		Plan: string(PlanTrial), PlanStatus: string(StatusActive), TrialEndsAt: at(-time.Minute),
	}

	for method, want := range map[string]int{
		http.MethodGet:  http.StatusNoContent,
		http.MethodPost: http.StatusPaymentRequired,
	} {
		req := httptest.NewRequest(method, "/v1/sync/push", nil)
		req = req.WithContext(tenancy.WithPrincipal(req.Context(), lapsed))
		rec := httptest.NewRecorder()
		guard.ServeHTTP(rec, req)
		if rec.Code != want {
			t.Errorf("%s: status %d, want %d", method, rec.Code, want)
		}
	}
}

func TestRequireFeature(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	guard := RequireFeature(FeatureShop)(ok)
	for plan, want := range map[Plan]int{PlanStarter: http.StatusForbidden, PlanPro: http.StatusNoContent} {
		p := tenancy.Principal{TenantID: ids.New(), SubjectID: ids.New(), Kind: tenancy.KindUser,
			Role: "owner", Plan: string(plan), PlanStatus: string(StatusActive)}
		req := httptest.NewRequest(http.MethodGet, "/v1/shop", nil)
		req = req.WithContext(tenancy.WithPrincipal(req.Context(), p))
		rec := httptest.NewRecorder()
		guard.ServeHTTP(rec, req)
		if rec.Code != want {
			t.Errorf("%s: status %d, want %d", plan, rec.Code, want)
		}
	}
}
