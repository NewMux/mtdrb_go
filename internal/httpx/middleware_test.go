package httpx

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/NewMux/mtdrb_go/internal/platform/ratelimit"
)

func addressSeenBehind(trust bool, remote string, forwarded ...string) string {
	var seen string
	h := RealIP(trust)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = ClientAddress(r)
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = remote
	for _, f := range forwarded {
		req.Header.Add("X-Forwarded-For", f)
	}
	h.ServeHTTP(httptest.NewRecorder(), req)
	return seen
}

func TestRealIP(t *testing.T) {
	cases := []struct {
		name      string
		trust     bool
		remote    string
		forwarded []string
		want      string
	}{
		{"direct", false, "203.0.113.9:5123", nil, "203.0.113.9"},
		{"untrusted header ignored", false, "203.0.113.9:5123", []string{"1.2.3.4"}, "203.0.113.9"},
		{"one proxy", true, "10.0.0.2:40000", []string{"198.51.100.7"}, "198.51.100.7"},
		// The caller sent "1.2.3.4" themselves; the proxy appended the address
		// it actually saw. Only the latter is believed.
		{"forged prefix", true, "10.0.0.2:40000", []string{"1.2.3.4, 198.51.100.7"}, "198.51.100.7"},
		{"forged header line", true, "10.0.0.2:40000", []string{"1.2.3.4", "198.51.100.7"}, "198.51.100.7"},
		{"garbage falls back to peer", true, "10.0.0.2:40000", []string{"not-an-ip"}, "10.0.0.2"},
		{"no header behind proxy", true, "10.0.0.2:40000", nil, "10.0.0.2"},
		{"ipv6", true, "[::1]:40000", []string{"2001:db8::1"}, "2001:db8::1"},
	}
	for _, tc := range cases {
		if got := addressSeenBehind(tc.trust, tc.remote, tc.forwarded...); got != tc.want {
			t.Errorf("%s: address = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestRateLimitIsPerCaller(t *testing.T) {
	limiter := ratelimit.New(2, time.Hour, nil)
	h := RealIP(true)(RateLimit(limiter)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})))
	call := func(addr string) int {
		req := httptest.NewRequest(http.MethodGet, "/public/invoices/x", nil)
		req.Header.Set("X-Forwarded-For", addr)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	for i := 0; i < 2; i++ {
		if code := call("198.51.100.7"); code != http.StatusOK {
			t.Fatalf("request %d = %d", i, code)
		}
	}
	if code := call("198.51.100.7"); code != http.StatusTooManyRequests {
		t.Errorf("third request = %d, want 429", code)
	}
	if code := call("198.51.100.8"); code != http.StatusOK {
		t.Errorf("another caller was limited too: %d", code)
	}
}
