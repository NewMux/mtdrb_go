package money

import (
	"encoding/json"
	"os"
	"testing"
)

// The same vectors drive the client's formatter, so the app and a receipt can
// never disagree about an amount.
func TestFormatMatchesSharedVectors(t *testing.T) {
	raw, err := os.ReadFile("testdata/format_vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var vectors struct {
		Format []struct {
			Minor    int64  `json:"minor"`
			Currency string `json:"currency"`
			Want     string `json:"want"`
		} `json:"format"`
	}
	if err := json.Unmarshal(raw, &vectors); err != nil {
		t.Fatal(err)
	}
	if len(vectors.Format) == 0 {
		t.Fatal("no vectors")
	}
	for _, v := range vectors.Format {
		if got := Format(v.Minor, v.Currency); got != v.Want {
			t.Errorf("Format(%d, %s) = %q, want %q", v.Minor, v.Currency, got, v.Want)
		}
		if got := New(v.Minor, v.Currency).String(); got != v.Want {
			t.Errorf("String() = %q, want %q", got, v.Want)
		}
	}
}

func TestExponent(t *testing.T) {
	for currency, want := range map[string]int{"AED": 2, "SAR": 2, "KWD": 3, "bhd": 3, "OMR": 3, "JPY": 0, "EUR": 2} {
		if got := Exponent(currency); got != want {
			t.Errorf("Exponent(%s) = %d, want %d", currency, got, want)
		}
	}
}
