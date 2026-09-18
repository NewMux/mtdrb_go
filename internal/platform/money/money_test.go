package money

import "testing"

func TestAddSubCurrencyMismatch(t *testing.T) {
	usd, eur := New(100, "USD"), New(100, "EUR")
	if _, err := usd.Add(eur); err == nil {
		t.Fatal("expected currency mismatch on Add")
	}
	if _, err := usd.Sub(eur); err == nil {
		t.Fatal("expected currency mismatch on Sub")
	}
}

func TestAllocateEvenlyConservesTotal(t *testing.T) {
	cases := []struct {
		total int64
		parts int
	}{
		{50000, 10}, {50000, 3}, {1, 3}, {-1000, 7}, {0, 4}, {99999, 13},
	}
	for _, tc := range cases {
		got, err := New(tc.total, "USD").AllocateEvenly(tc.parts)
		if err != nil {
			t.Fatalf("allocate(%d,%d): %v", tc.total, tc.parts, err)
		}
		if len(got) != tc.parts {
			t.Fatalf("allocate(%d,%d): got %d parts", tc.total, tc.parts, len(got))
		}
		var sum int64
		for _, p := range got {
			sum += p.Minor
		}
		if sum != tc.total {
			t.Errorf("allocate(%d,%d): parts sum to %d, want %d", tc.total, tc.parts, sum, tc.total)
		}
	}
}

func TestAllocateEvenlyRejectsNonPositive(t *testing.T) {
	if _, err := New(100, "USD").AllocateEvenly(0); err == nil {
		t.Fatal("expected error allocating into 0 parts")
	}
}

func TestNegAndAbs(t *testing.T) {
	m := New(-250, "usd")
	if m.Currency != "USD" {
		t.Errorf("currency not normalized: %q", m.Currency)
	}
	if !m.IsNegative() {
		t.Error("expected negative")
	}
	if m.Abs().Minor != 250 || m.Neg().Minor != 250 {
		t.Error("Abs/Neg wrong")
	}
}

func TestString(t *testing.T) {
	if got := New(50000, "USD").String(); got != "500.00 USD" {
		t.Errorf("got %q", got)
	}
	if got := New(-5, "USD").String(); got != "-0.05 USD" {
		t.Errorf("got %q", got)
	}
}
