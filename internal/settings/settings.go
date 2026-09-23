// Package settings reads and changes how a practice runs: its language, its
// week, its hours, its targets and its policies.
//
// They live on the tenant row (see the migration that added them) and the
// device mirrors the ones it needs offline. Only the owner changes them.
//
// Two rules here are not just validation.
//
// Currency is fixed once money has moved. The ledger is single-currency per
// tenant, and every posted amount is in the currency of the day it posted;
// relabelling it afterwards would turn 3,500 AED of receivables into 3,500
// of something else without a single number changing.
//
// The week follows the country unless told otherwise. A Saudi trainer's week
// starts on Sunday; a Dubai trainer's, since 2022, on Monday. Picking the
// country picks the week, and a trainer who disagrees can still say so.
package settings

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/NewMux/mtdrb_go/internal/platform/errs"
	"github.com/NewMux/mtdrb_go/internal/platform/ids"
)

// Settings is a practice's configuration.
type Settings struct {
	BusinessName        string       `json:"business_name"`
	Currency            string       `json:"currency"`
	CurrencyLocked      bool         `json:"currency_locked"`
	Timezone            string       `json:"timezone"`
	Country             *string      `json:"country"`
	Language            string       `json:"language"`
	DocumentLanguage    string       `json:"document_language"`
	Digits              string       `json:"digits"`
	WeekStart           int          `json:"week_start"`
	WorkingHours        WorkingHours `json:"working_hours"`
	Targets             Targets      `json:"targets"`
	Automations         Automations  `json:"automations"`
	SessionTimeoutDays  int          `json:"session_timeout_days"`
	BufferMinutes       int          `json:"buffer_minutes"`
	AllowOverdraft      bool         `json:"allow_overdraft"`
	NoShowIsBillable    bool         `json:"no_show_is_billable"`
	LowBalanceThreshold int          `json:"low_balance_threshold"`
}

// WorkingHours is the bookable week: weekday ("0" = Sunday) to intervals of
// "HH:MM" times. A day that is absent is a day off.
type WorkingHours map[string][][2]string

// Targets are what the trainer is aiming for; the dashboard measures against
// them. Absent means not set.
type Targets struct {
	MonthlyRevenueMinor *int64 `json:"monthly_revenue_minor,omitempty"`
	WeeklySessions      *int   `json:"weekly_sessions,omitempty"`
	ActiveClients       *int   `json:"active_clients,omitempty"`
}

// Automations are the follow-ups the worker creates on the trainer's behalf.
type Automations struct {
	RenewalDue      bool `json:"renewal_due"`
	OverdueInvoice  bool `json:"overdue_invoice"`
	InactiveClient  bool `json:"inactive_client"`
	ProgrammeEnding bool `json:"programme_ending"`
	// InactiveAfterDays is how long without a session makes a client
	// inactive. Zero means the default of 21.
	InactiveAfterDays int `json:"inactive_after_days,omitempty"`
}

// Patch changes some settings. A nil field is left as it is.
type Patch struct {
	BusinessName        *string       `json:"business_name"`
	Currency            *string       `json:"currency"`
	Timezone            *string       `json:"timezone"`
	Country             *string       `json:"country"`
	Language            *string       `json:"language"`
	DocumentLanguage    *string       `json:"document_language"`
	Digits              *string       `json:"digits"`
	WeekStart           *int          `json:"week_start"`
	WorkingHours        *WorkingHours `json:"working_hours"`
	Targets             *Targets      `json:"targets"`
	Automations         *Automations  `json:"automations"`
	SessionTimeoutDays  *int          `json:"session_timeout_days"`
	BufferMinutes       *int          `json:"buffer_minutes"`
	AllowOverdraft      *bool         `json:"allow_overdraft"`
	NoShowIsBillable    *bool         `json:"no_show_is_billable"`
	LowBalanceThreshold *int          `json:"low_balance_threshold"`
}

// DefaultWeekStart is the first day of the working week in a country.
func DefaultWeekStart(country string) int {
	switch strings.ToUpper(country) {
	// The Gulf states other than the UAE keep a Friday–Saturday weekend.
	case "SA", "BH", "KW", "OM", "QA":
		return int(time.Sunday)
	}
	return int(time.Monday)
}

// Get reads the practice's settings.
func Get(ctx context.Context, tx pgx.Tx, tenantID ids.ID) (Settings, error) {
	var (
		s                           Settings
		hours, targets, automations []byte
	)
	if err := tx.QueryRow(ctx, `
		SELECT name, default_currency, timezone, country, language, document_language, digits,
		       week_start, working_hours, targets, automations, session_timeout_days,
		       buffer_minutes, allow_overdraft, no_show_is_billable, low_balance_threshold,
		       EXISTS (SELECT 1 FROM journal_entries)
		  FROM tenants WHERE id = $1`, tenantID,
	).Scan(&s.BusinessName, &s.Currency, &s.Timezone, &s.Country, &s.Language, &s.DocumentLanguage,
		&s.Digits, &s.WeekStart, &hours, &targets, &automations, &s.SessionTimeoutDays,
		&s.BufferMinutes, &s.AllowOverdraft, &s.NoShowIsBillable, &s.LowBalanceThreshold,
		&s.CurrencyLocked); err != nil {
		return Settings{}, errs.Internal(err, "read settings")
	}
	s.WorkingHours = WorkingHours{}
	for dst, raw := range map[any][]byte{&s.WorkingHours: hours, &s.Targets: targets, &s.Automations: automations} {
		if err := json.Unmarshal(raw, dst); err != nil {
			return Settings{}, errs.Internal(err, "decode settings")
		}
	}
	return s, nil
}

var (
	clockTime = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d$`)
	isoCode   = regexp.MustCompile(`^[A-Z]{3}$`)
	country   = regexp.MustCompile(`^[A-Z]{2}$`)
)

func invalid(field, msg string) *errs.Error {
	return errs.Invalid(errs.CodeValidation, "%s %s", strings.ReplaceAll(field, "_", " "), msg).WithField(field, msg)
}

func oneOf(field, value string, allowed ...string) error {
	for _, a := range allowed {
		if value == a {
			return nil
		}
	}
	return invalid(field, "must be one of "+strings.Join(allowed, ", "))
}

// Validate checks a set of working hours and returns it in canonical order.
func (w WorkingHours) Validate() (WorkingHours, error) {
	out := WorkingHours{}
	for day, intervals := range w {
		if len(day) != 1 || day[0] < '0' || day[0] > '6' {
			return nil, invalid("working_hours", fmt.Sprintf("has an unknown weekday %q; use 0 (Sunday) to 6", day))
		}
		sorted := append([][2]string(nil), intervals...)
		sort.Slice(sorted, func(i, j int) bool { return sorted[i][0] < sorted[j][0] })
		for i, iv := range sorted {
			if !clockTime.MatchString(iv[0]) || !clockTime.MatchString(iv[1]) {
				return nil, invalid("working_hours", "times must be HH:MM")
			}
			if iv[0] >= iv[1] {
				return nil, invalid("working_hours", fmt.Sprintf("%s–%s ends before it starts", iv[0], iv[1]))
			}
			if i > 0 && iv[0] < sorted[i-1][1] {
				return nil, invalid("working_hours", fmt.Sprintf("%s–%s overlaps the hours before it", iv[0], iv[1]))
			}
		}
		if len(sorted) > 0 {
			out[day] = sorted
		}
	}
	return out, nil
}

// Update applies a patch and returns the settings as they now stand.
func Update(ctx context.Context, tx pgx.Tx, tenantID ids.ID, p Patch) (Settings, error) {
	current, err := Get(ctx, tx, tenantID)
	if err != nil {
		return Settings{}, err
	}
	next := current

	if p.BusinessName != nil {
		name := strings.TrimSpace(*p.BusinessName)
		if name == "" {
			return Settings{}, invalid("business_name", "is required")
		}
		next.BusinessName = name
	}
	if p.Currency != nil {
		code := strings.ToUpper(strings.TrimSpace(*p.Currency))
		if !isoCode.MatchString(code) {
			return Settings{}, invalid("currency", "must be a three-letter ISO code")
		}
		if code != current.Currency && current.CurrencyLocked {
			return Settings{}, errs.Conflict(errs.CodeCurrencyLocked,
				"the currency is fixed once money has been recorded in it").
				WithField("currency", "is fixed once money has been recorded")
		}
		next.Currency = code
	}
	if p.Timezone != nil {
		if _, err := time.LoadLocation(*p.Timezone); err != nil || strings.TrimSpace(*p.Timezone) == "" {
			return Settings{}, invalid("timezone", "must be an IANA name such as Asia/Dubai")
		}
		next.Timezone = *p.Timezone
	}
	if p.Country != nil {
		code := strings.ToUpper(strings.TrimSpace(*p.Country))
		if !country.MatchString(code) {
			return Settings{}, invalid("country", "must be a two-letter ISO code")
		}
		next.Country = &code
		// The week follows the country unless this same change says otherwise.
		if p.WeekStart == nil && (current.Country == nil || *current.Country != code) {
			next.WeekStart = DefaultWeekStart(code)
		}
	}
	if p.Language != nil {
		if err := oneOf("language", *p.Language, "en", "ar"); err != nil {
			return Settings{}, err
		}
		next.Language = *p.Language
	}
	if p.DocumentLanguage != nil {
		if err := oneOf("document_language", *p.DocumentLanguage, "en", "ar", "bilingual"); err != nil {
			return Settings{}, err
		}
		next.DocumentLanguage = *p.DocumentLanguage
	}
	if p.Digits != nil {
		if err := oneOf("digits", *p.Digits, "latn", "arab"); err != nil {
			return Settings{}, err
		}
		next.Digits = *p.Digits
	}
	if p.WeekStart != nil {
		if *p.WeekStart < 0 || *p.WeekStart > 6 {
			return Settings{}, invalid("week_start", "must be 0 (Sunday) to 6 (Saturday)")
		}
		next.WeekStart = *p.WeekStart
	}
	if p.WorkingHours != nil {
		hours, err := p.WorkingHours.Validate()
		if err != nil {
			return Settings{}, err
		}
		next.WorkingHours = hours
	}
	if p.Targets != nil {
		t := *p.Targets
		if (t.MonthlyRevenueMinor != nil && *t.MonthlyRevenueMinor < 0) ||
			(t.WeeklySessions != nil && *t.WeeklySessions < 0) ||
			(t.ActiveClients != nil && *t.ActiveClients < 0) {
			return Settings{}, invalid("targets", "cannot be negative")
		}
		next.Targets = t
	}
	if p.Automations != nil {
		if p.Automations.InactiveAfterDays < 0 || p.Automations.InactiveAfterDays > 365 {
			return Settings{}, invalid("automations", "inactive after days must be 0 to 365")
		}
		next.Automations = *p.Automations
	}
	if p.SessionTimeoutDays != nil {
		if *p.SessionTimeoutDays < 1 || *p.SessionTimeoutDays > 365 {
			return Settings{}, invalid("session_timeout_days", "must be 1 to 365")
		}
		next.SessionTimeoutDays = *p.SessionTimeoutDays
	}
	if p.BufferMinutes != nil {
		if *p.BufferMinutes < 0 || *p.BufferMinutes > 240 {
			return Settings{}, invalid("buffer_minutes", "must be 0 to 240")
		}
		next.BufferMinutes = *p.BufferMinutes
	}
	if p.AllowOverdraft != nil {
		next.AllowOverdraft = *p.AllowOverdraft
	}
	if p.NoShowIsBillable != nil {
		next.NoShowIsBillable = *p.NoShowIsBillable
	}
	if p.LowBalanceThreshold != nil {
		if *p.LowBalanceThreshold < 0 || *p.LowBalanceThreshold > 100 {
			return Settings{}, invalid("low_balance_threshold", "must be 0 to 100")
		}
		next.LowBalanceThreshold = *p.LowBalanceThreshold
	}

	hours, _ := json.Marshal(next.WorkingHours)
	targets, _ := json.Marshal(next.Targets)
	automations, _ := json.Marshal(next.Automations)
	if _, err := tx.Exec(ctx, `
		UPDATE tenants SET
			name = $2, default_currency = $3, timezone = $4, country = $5, language = $6,
			document_language = $7, digits = $8, week_start = $9, working_hours = $10,
			targets = $11, automations = $12, session_timeout_days = $13, buffer_minutes = $14,
			allow_overdraft = $15, no_show_is_billable = $16, low_balance_threshold = $17
		 WHERE id = $1`,
		tenantID, next.BusinessName, next.Currency, next.Timezone, next.Country, next.Language,
		next.DocumentLanguage, next.Digits, next.WeekStart, hours, targets, automations,
		next.SessionTimeoutDays, next.BufferMinutes, next.AllowOverdraft, next.NoShowIsBillable,
		next.LowBalanceThreshold); err != nil {
		return Settings{}, errs.Internal(err, "update settings")
	}
	return Get(ctx, tx, tenantID)
}
