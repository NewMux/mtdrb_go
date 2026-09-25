// Package money provides an exact monetary amount type.
//
// Amounts are always held as integer minor units (cents, fils, agorot) paired
// with an ISO-4217 currency code. Floating point is never used: a ledger that
// rounds is a ledger that cannot be reconciled.
package money

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"
)

// Money is an exact amount in the minor unit of Currency.
type Money struct {
	Minor    int64
	Currency string
}

// New builds an amount from minor units and an ISO-4217 code.
func New(minor int64, currency string) Money {
	return Money{Minor: minor, Currency: normalize(currency)}
}

// Zero returns a zero amount in the given currency.
func Zero(currency string) Money {
	return Money{Minor: 0, Currency: normalize(currency)}
}

func normalize(c string) string { return strings.ToUpper(strings.TrimSpace(c)) }

// IsZero reports whether the amount is exactly zero.
func (m Money) IsZero() bool { return m.Minor == 0 }

// IsNegative reports whether the amount is below zero.
func (m Money) IsNegative() bool { return m.Minor < 0 }

// Neg returns the additive inverse, used to build reversing journal lines.
func (m Money) Neg() Money { return Money{Minor: -m.Minor, Currency: m.Currency} }

// Abs returns the magnitude of the amount.
func (m Money) Abs() Money {
	if m.Minor < 0 {
		return m.Neg()
	}
	return m
}

// Add returns m+other, erroring on a currency mismatch rather than coercing.
func (m Money) Add(other Money) (Money, error) {
	if err := m.sameCurrency(other); err != nil {
		return Money{}, err
	}
	return Money{Minor: m.Minor + other.Minor, Currency: m.Currency}, nil
}

// Sub returns m-other, erroring on a currency mismatch.
func (m Money) Sub(other Money) (Money, error) {
	if err := m.sameCurrency(other); err != nil {
		return Money{}, err
	}
	return Money{Minor: m.Minor - other.Minor, Currency: m.Currency}, nil
}

// Mul scales the amount by an integer factor, as when pricing an n-session pack.
func (m Money) Mul(factor int64) Money {
	return Money{Minor: m.Minor * factor, Currency: m.Currency}
}

// Cmp returns -1, 0 or +1 comparing m against other.
func (m Money) Cmp(other Money) (int, error) {
	if err := m.sameCurrency(other); err != nil {
		return 0, err
	}
	switch {
	case m.Minor < other.Minor:
		return -1, nil
	case m.Minor > other.Minor:
		return 1, nil
	default:
		return 0, nil
	}
}

// AllocateEvenly splits the amount into n parts whose sum is exactly m.
//
// The remainder is distributed one minor unit at a time to the leading parts,
// so no fraction of a unit is ever created or destroyed. This is how a pack
// price is spread across its sessions for revenue recognition.
func (m Money) AllocateEvenly(n int) ([]Money, error) {
	if n <= 0 {
		return nil, fmt.Errorf("money: cannot allocate into %d parts", n)
	}
	parts := make([]Money, n)
	base := m.Minor / int64(n)
	rem := m.Minor % int64(n)
	step := int64(1)
	if rem < 0 {
		rem, step = -rem, -1
	}
	for i := range parts {
		parts[i] = Money{Minor: base, Currency: m.Currency}
		if int64(i) < rem {
			parts[i].Minor += step
		}
	}
	return parts, nil
}

func (m Money) sameCurrency(other Money) error {
	if m.Currency != other.Currency {
		return fmt.Errorf("money: currency mismatch %q vs %q", m.Currency, other.Currency)
	}
	return nil
}

// String renders the amount in its currency's own precision, for logs and
// receipts: "500.00 AED", "12.500 KWD", "3000 JPY".
func (m Money) String() string { return Format(m.Minor, m.Currency) }

// exponents lists the ISO-4217 currencies whose minor unit is not a
// hundredth. Most of the Gulf is two decimals, but Kuwait, Bahrain and Oman
// count in fils — thousandths — so a dinar printed with two places is off by
// a factor of ten on every line of an invoice.
var exponents = map[string]int{
	"BHD": 3, "IQD": 3, "JOD": 3, "KWD": 3, "LYD": 3, "OMR": 3, "TND": 3,
	"BIF": 0, "CLP": 0, "DJF": 0, "GNF": 0, "ISK": 0, "JPY": 0, "KMF": 0,
	"KRW": 0, "PYG": 0, "RWF": 0, "UGX": 0, "VND": 0, "VUV": 0, "XAF": 0,
	"XOF": 0, "XPF": 0,
}

// Exponent is the number of decimal places in the currency's minor unit.
func Exponent(currency string) int {
	if e, ok := exponents[normalize(currency)]; ok {
		return e
	}
	return 2
}

// Format renders minor units in the currency's precision, without grouping:
// the plain form that logs, CSV exports and the shared test vectors agree on.
// Locale-aware display belongs to the client.
func Format(minor int64, currency string) string {
	currency = normalize(currency)
	sign := ""
	if minor < 0 {
		sign, minor = "-", -minor
	}
	exp := Exponent(currency)
	if exp == 0 {
		return fmt.Sprintf("%s%d %s", sign, minor, currency)
	}
	scale := int64(1)
	for range exp {
		scale *= 10
	}
	return fmt.Sprintf("%s%d.%0*d %s", sign, minor/scale, exp, minor%scale, currency)
}

type jsonMoney struct {
	Minor    int64  `json:"minor"`
	Currency string `json:"currency"`
}

// MarshalJSON emits minor units, never a float, so clients cannot lose precision.
func (m Money) MarshalJSON() ([]byte, error) {
	return json.Marshal(jsonMoney(m))
}

// UnmarshalJSON parses the minor-unit representation.
func (m *Money) UnmarshalJSON(b []byte) error {
	var j jsonMoney
	if err := json.Unmarshal(b, &j); err != nil {
		return err
	}
	m.Minor, m.Currency = j.Minor, normalize(j.Currency)
	return nil
}

// Value stores only the minor units; the currency lives in its own column.
func (m Money) Value() (driver.Value, error) { return m.Minor, nil }
