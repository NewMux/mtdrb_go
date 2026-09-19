package dates_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/NewMux/mtdrb_go/internal/platform/dates"
)

func TestUnmarshalAcceptsCalendarDate(t *testing.T) {
	t.Parallel()

	// The format api/openapi.yaml publishes, and the one Postgres hands back
	// from to_jsonb on a date column. Rejecting it is what broke every
	// offline operation carrying a date.
	var d dates.Date
	if err := json.Unmarshal([]byte(`"2026-09-18"`), &d); err != nil {
		t.Fatalf("unmarshal date: %v", err)
	}
	if got := d.String(); got != "2026-09-18" {
		t.Fatalf("round trip = %q, want 2026-09-18", got)
	}
	if !d.Time().Equal(time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("time = %v, want UTC midnight", d.Time())
	}
}

func TestUnmarshalToleratesTimestamp(t *testing.T) {
	t.Parallel()

	// Existing callers sent these for as long as it was the only thing that
	// worked; fixing one client must not break another.
	var d dates.Date
	if err := json.Unmarshal([]byte(`"2026-09-18T14:32:07Z"`), &d); err != nil {
		t.Fatalf("unmarshal timestamp: %v", err)
	}
	if got := d.String(); got != "2026-09-18" {
		t.Fatalf("truncated to %q, want 2026-09-18", got)
	}
}

func TestUnmarshalNullIsZero(t *testing.T) {
	t.Parallel()

	for _, in := range []string{`null`, `""`} {
		var d dates.Date
		if err := json.Unmarshal([]byte(in), &d); err != nil {
			t.Fatalf("unmarshal %s: %v", in, err)
		}
		if !d.IsZero() {
			t.Fatalf("%s did not produce the zero date", in)
		}
	}
}

func TestUnmarshalRejectsNonsense(t *testing.T) {
	t.Parallel()

	// A wrong date must fail loudly. Silently defaulting to today would write
	// a session into the wrong week and nobody would ever notice.
	for _, in := range []string{`"18/09/2026"`, `"tomorrow"`, `"2026-13-45"`, `"2026-09"`} {
		var d dates.Date
		if err := json.Unmarshal([]byte(in), &d); err == nil {
			t.Fatalf("%s was accepted as %v", in, d)
		}
	}
}

func TestMarshalIsDateOnly(t *testing.T) {
	t.Parallel()

	raw, err := json.Marshal(dates.Of(time.Date(2026, 9, 18, 23, 59, 0, 0, time.UTC)))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(raw) != `"2026-09-18"` {
		t.Fatalf("marshal = %s, want \"2026-09-18\"", raw)
	}

	zero, err := json.Marshal(dates.Date{})
	if err != nil {
		t.Fatalf("marshal zero: %v", err)
	}
	if string(zero) != "null" {
		t.Fatalf("zero marshalled as %s, want null", zero)
	}
}

func TestOrElse(t *testing.T) {
	t.Parallel()

	fallback := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	var missing *dates.Date
	if got := missing.OrElse(fallback); !got.Equal(fallback) {
		t.Fatalf("nil OrElse = %v, want the fallback", got)
	}

	empty := &dates.Date{}
	if got := empty.OrElse(fallback); !got.Equal(fallback) {
		t.Fatalf("zero OrElse = %v, want the fallback", got)
	}

	set := dates.Of(time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC))
	if got := (&set).OrElse(fallback); !got.Equal(set.Time()) {
		t.Fatalf("set OrElse = %v, want the date", got)
	}
}
