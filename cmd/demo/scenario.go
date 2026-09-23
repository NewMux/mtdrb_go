package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	mrand "math/rand"
	"net/http"
	"sort"
	"strings"
	"time"
)

// Recording is what the demo build replays.
type Recording struct {
	// RecordedAt is the scenario's "now". The app shifts every date in the
	// recording by the whole weeks between this and the viewer's today.
	RecordedAt time.Time `json:"recorded_at"`
	Timezone   string    `json:"timezone"`
	// UTCOffsetMinutes is the studio's offset, so the app can keep a 07:00
	// session at 07:00 on the viewer's clock without a time-zone database.
	UTCOffsetMinutes int             `json:"utc_offset_minutes"`
	Account          json.RawMessage `json:"account"`
	// Pull is every collection a device would receive on its first sync.
	Pull []pulledCollection `json:"pull,omitempty"`
	// Reads are server-computed answers, keyed by the path the app requests.
	Reads map[string]json.RawMessage `json:"reads,omitempty"`
}

type pulledCollection struct {
	Collection string            `json:"collection"`
	Rows       []json.RawMessage `json:"rows"`
}

const timezone = "Asia/Dubai"

// dubai is UTC+4 all year: no daylight saving, so a local hour converts by a
// constant.
var dubai = time.FixedZone("Asia/Dubai", 4*60*60)

// A client and how they train. Slots are local weekday and hour.
type person struct {
	name, email, phone string
	slots              []slot
	// startWeek is how many weeks before "now" they began; renews is whether
	// they buy another pack when one runs out.
	startWeek int
	renews    bool
	pack      pack
	// owes leaves their latest invoice unpaid; overdue makes it past due.
	owes, overdue bool
	// fading clients stop turning up in the last fortnight.
	fading    bool
	weightKg  float64
	bodyFatBP int
	lifts     []string
}

type slot struct {
	weekday time.Weekday
	hour    int
	semi    bool
}

type pack struct {
	credits int
	// priceMinor is the whole pack in fils.
	priceMinor int64
	name       string
}

var (
	tenPack    = pack{10, 350000, "10 × personal training"}
	twentyPack = pack{20, 640000, "20 × personal training"}
	fivePack   = pack{5, 185000, "5 × personal training"}
)

var roster = []person{
	{name: "Dana Rivers", email: "dana@example.com", phone: "+971 50 123 4410", slots: []slot{{time.Monday, 7, false}, {time.Wednesday, 7, false}}, startWeek: 12, renews: true, pack: twentyPack, weightKg: 71, bodyFatBP: 2450, lifts: []string{"Back Squat", "Romanian Deadlift", "Bench Press"}},
	{name: "Morgan Hale", email: "morgan@example.com", phone: "+971 55 234 1188", slots: []slot{{time.Wednesday, 11, false}}, startWeek: 12, renews: true, pack: tenPack, owes: true, weightKg: 88, bodyFatBP: 2100, lifts: []string{"Conventional Deadlift", "Overhead Press", "Barbell Row"}},
	{name: "Priya Raman", email: "priya@example.com", phone: "+971 52 887 0192", slots: []slot{{time.Tuesday, 17, false}}, startWeek: 12, renews: false, pack: tenPack, weightKg: 62, bodyFatBP: 2680, lifts: []string{"Goblet Squat", "Hip Thrust", "Push-Up"}},
	{name: "Omar Haddad", email: "omar@example.com", phone: "+971 50 443 9021", slots: []slot{{time.Wednesday, 17, false}, {time.Saturday, 9, false}}, startWeek: 12, renews: true, pack: twentyPack, weightKg: 94, bodyFatBP: 2300, lifts: []string{"Back Squat", "Bench Press", "Pull-Up"}},
	{name: "Layla Nasser", email: "layla@example.com", phone: "+971 56 310 7765", slots: []slot{{time.Wednesday, 19, true}}, startWeek: 10, renews: true, pack: tenPack, weightKg: 58, bodyFatBP: 2550, lifts: []string{"Bulgarian Split Squat", "Kettlebell Swing", "Lat Pulldown"}},
	{name: "James Whitfield", email: "james@example.com", phone: "+971 58 902 1143", slots: []slot{{time.Monday, 18, false}, {time.Thursday, 18, false}}, startWeek: 12, renews: true, pack: twentyPack, overdue: true, weightKg: 83, bodyFatBP: 1850, lifts: []string{"Front Squat", "Conventional Deadlift", "Incline Bench Press"}},
	{name: "Fatima Al Mansoori", email: "fatima@example.com", phone: "+971 50 776 4402", slots: []slot{{time.Tuesday, 8, false}}, startWeek: 11, renews: true, pack: tenPack, weightKg: 66, bodyFatBP: 2900, lifts: []string{"Goblet Squat", "Romanian Deadlift", "Seated Cable Row"}},
	{name: "Ravi Iyer", email: "ravi@example.com", phone: "+971 55 118 3390", slots: []slot{{time.Thursday, 7, false}}, startWeek: 9, renews: true, pack: tenPack, weightKg: 79, bodyFatBP: 2200, lifts: []string{"Back Squat", "Overhead Press", "Pull-Up"}},
	{name: "Sofia Marin", email: "sofia@example.com", phone: "+971 52 640 2217", slots: []slot{{time.Saturday, 11, false}}, startWeek: 3, renews: true, pack: fivePack, weightKg: 60, bodyFatBP: 2750, lifts: []string{"Walking Lunge", "Hip Thrust", "Seated Dumbbell Press"}},
	{name: "Khalid Al Suwaidi", email: "khalid@example.com", phone: "+971 50 229 8871", slots: []slot{{time.Sunday, 18, false}}, startWeek: 11, renews: false, pack: tenPack, fading: true, weightKg: 97, bodyFatBP: 2600, lifts: []string{"Conventional Deadlift", "Bench Press", "Barbell Row"}},
	{name: "Aisha Rahman", email: "aisha@example.com", phone: "+971 56 915 6608", slots: []slot{{time.Monday, 10, false}, {time.Thursday, 10, false}}, startWeek: 8, renews: true, pack: twentyPack, weightKg: 64, bodyFatBP: 2700, lifts: []string{"Front Squat", "Hip Thrust", "Lat Pulldown"}},
	{name: "Tom Becker", email: "tom@example.com", phone: "+971 58 334 0976", slots: []slot{{time.Friday, 8, false}}, startWeek: 6, renews: true, pack: tenPack, weightKg: 86, bodyFatBP: 2350, lifts: []string{"Back Squat", "Conventional Deadlift", "Bench Press"}},
}

// occurrence is one booked session for one client.
type occurrence struct {
	who      int
	startsAt time.Time
	semi     bool
}

func runScenario(ctx context.Context, c *apiClient) (*Recording, error) {
	rng := mrand.New(mrand.NewSource(17)) //nolint:gosec // a reproducible scenario is the point
	c.wallStart = time.Now()

	var session struct {
		Account json.RawMessage `json:"account"`
		Tokens  struct {
			AccessToken string `json:"access_token"`
		} `json:"tokens"`
	}
	if err := c.do(ctx, "POST", "/v1/auth/signup", map[string]any{
		"email": "sam@riverastrength.example", "password": "demo-practice-password",
		"display_name": "Sam Rivera", "business_name": "Rivera Strength",
		"currency": "AED", "timezone": timezone,
	}, &session); err != nil {
		return nil, fmt.Errorf("sign up: %w", err)
	}
	c.token = session.Tokens.AccessToken

	// Sam also signed in on the studio laptop, so the devices list has more
	// than the phone the demo is "held" on.
	c.userAgent = macChrome
	if err := c.do(ctx, "POST", "/v1/auth/login", map[string]any{
		"email": "sam@riverastrength.example", "password": "demo-practice-password",
	}, nil); err != nil {
		return nil, fmt.Errorf("second device: %w", err)
	}
	c.userAgent = iPhoneSafari

	// A Dubai week: six mornings and five evenings, Saturday mornings at the
	// beach, Sunday off.
	evening := [][]string{{"06:00", "12:00"}, {"16:00", "21:00"}}
	if err := c.do(ctx, "PATCH", "/v1/settings", map[string]any{
		"country": "AE",
		"working_hours": map[string]any{
			"1": evening, "2": evening, "3": evening, "4": evening, "5": evening,
			"6": [][]string{{"07:00", "12:00"}},
		},
		"targets": map[string]any{
			"monthly_revenue_minor": 3000000, "weekly_sessions": 24, "active_clients": 14,
		},
		"automations": map[string]any{
			"renewal_due": true, "overdue_invoice": true, "inactive_client": true, "programme_ending": false,
		},
	}, nil); err != nil {
		return nil, fmt.Errorf("settings: %w", err)
	}

	if err := c.do(ctx, "POST", "/v1/payment-methods", map[string]any{
		"kind": "bank_transfer", "label": "Emirates NBD", "is_default": true,
		"details": map[string]any{
			"account_holder": "Rivera Strength", "bank_name": "Emirates NBD",
			"iban": "AE070331234567890123456", "swift_bic": "EBILAEAD",
		},
		"instructions": "Please put the invoice number in the reference.",
	}, nil); err != nil {
		return nil, fmt.Errorf("payment method: %w", err)
	}

	var oneOnOne, semiPrivate idOnly
	if err := c.do(ctx, "POST", "/v1/sessions/session-types", map[string]any{
		"name": "1-on-1", "duration_minutes": 60, "capacity": 1, "credit_cost": 1,
	}, &oneOnOne); err != nil {
		return nil, fmt.Errorf("session type: %w", err)
	}
	if err := c.do(ctx, "POST", "/v1/sessions/session-types", map[string]any{
		"name": "Semi-private", "duration_minutes": 60, "capacity": 3, "credit_cost": 1,
	}, &semiPrivate); err != nil {
		return nil, fmt.Errorf("session type: %w", err)
	}

	// Two places and a price list.
	places := map[string]string{}
	// A slice, not a map: the order ids are minted in must not vary between
	// recordings. The first place added is the primary.
	for _, place := range []struct {
		key  string
		body map[string]any
	}{
		{"studio", map[string]any{"name": "Rivera Strength Studio", "kind": "studio", "region": "Dubai",
			"address": "Marina Plaza, Dubai Marina", "colour": "#c8ff00"}},
		{"beach", map[string]any{"name": "Kite Beach", "kind": "outdoor", "region": "Dubai",
			"address": "Jumeirah 3", "colour": "#4f8cff"}},
	} {
		var created idOnly
		if err := c.do(ctx, "POST", "/v1/locations", place.body, &created); err != nil {
			return nil, fmt.Errorf("location %s: %w", place.key, err)
		}
		places[place.key] = created.ID
	}
	for _, offer := range []map[string]any{
		{"name": fivePack.name, "kind": "session_pack", "credits": fivePack.credits, "price_minor": fivePack.priceMinor, "validity_days": 60, "sort_order": 1},
		{"name": tenPack.name, "kind": "session_pack", "credits": tenPack.credits, "price_minor": tenPack.priceMinor, "validity_days": 90, "sort_order": 2},
		{"name": twentyPack.name, "kind": "session_pack", "credits": twentyPack.credits, "price_minor": twentyPack.priceMinor, "validity_days": 150, "sort_order": 3},
		{"name": "Semi-private × 8", "kind": "semi_private", "credits": 8, "price_minor": 160000, "validity_days": 60, "sort_order": 4},
		{"name": "Online coaching", "kind": "online_coaching", "price_minor": 90000, "cycle": "monthly", "sort_order": 5,
			"description": "Programme, weekly check-in and form reviews in the app."},
	} {
		if err := c.do(ctx, "POST", "/v1/package-offers", offer, nil); err != nil {
			return nil, fmt.Errorf("offer %v: %w", offer["name"], err)
		}
	}

	exercises, err := c.exercises(ctx)
	if err != nil {
		return nil, err
	}

	clientIDs := make([]string, len(roster))
	for i, p := range roster {
		var created idOnly
		if err := c.do(ctx, "POST", "/v1/clients", map[string]any{
			"full_name": p.name, "email": p.email, "phone": p.phone,
		}, &created); err != nil {
			return nil, fmt.Errorf("client %s: %w", p.name, err)
		}
		clientIDs[i] = created.ID
	}

	// Every session in the window, in time order, so packs are bought and
	// used up in the order a real practice would see.
	var plan []occurrence
	today := recordedAt.In(dubai)
	weekStart := today.AddDate(0, 0, -int((today.Weekday()+6)%7)) // this Monday
	weekStart = time.Date(weekStart.Year(), weekStart.Month(), weekStart.Day(), 0, 0, 0, 0, dubai)
	for i, p := range roster {
		for w := -p.startWeek; w <= 2; w++ {
			for _, s := range p.slots {
				day := weekStart.AddDate(0, 0, 7*w+int((s.weekday+6)%7))
				plan = append(plan, occurrence{
					who:      i,
					startsAt: time.Date(day.Year(), day.Month(), day.Day(), s.hour, 0, 0, 0, dubai),
					semi:     s.semi,
				})
			}
		}
	}
	sort.Slice(plan, func(a, b int) bool { return plan[a].startsAt.Before(plan[b].startsAt) })

	lastInvoice := make([]string, len(roster))
	stopped := make([]bool, len(roster))
	workouts := make([]int, len(roster))

	for _, o := range plan {
		p := roster[o.who]
		id := clientIDs[o.who]
		past := o.startsAt.Before(recordedAt)

		if stopped[o.who] {
			continue
		}
		// The server decides what a session costs (a no-show may be billable),
		// so the scenario asks it rather than keeping its own count.
		remaining, err := c.balance(ctx, id)
		if err != nil {
			return nil, err
		}
		if remaining <= 0 {
			// The first pack is bought just before the first session; a
			// renewal the day after the last credit was used — unless this
			// client does not renew, in which case they stop coming.
			if lastInvoice[o.who] != "" && !p.renews {
				stopped[o.who] = true
				continue
			}
			issued := o.startsAt.AddDate(0, 0, -2)
			if issued.After(recordedAt) {
				issued = recordedAt
			}
			invoiceID, err := c.sellPack(ctx, id, p, issued)
			if err != nil {
				return nil, fmt.Errorf("sell %s a pack: %w", p.name, err)
			}
			lastInvoice[o.who] = invoiceID
			if err := c.maybePay(ctx, rng, p, invoiceID, issued, o.startsAt); err != nil {
				return nil, err
			}
		}

		typeID := oneOnOne.ID
		if o.semi {
			typeID = semiPrivate.ID
		}
		var booked struct {
			ID        string `json:"id"`
			Attendees []struct {
				ID string `json:"id"`
			} `json:"attendees"`
		}
		if err := c.do(ctx, "POST", "/v1/sessions", map[string]any{
			"session_type_id": typeID, "starts_at": o.startsAt.UTC(), "client_ids": []string{id},
			"location_id": places[placeFor(o)],
		}, &booked); err != nil {
			return nil, fmt.Errorf("book %s: %w", p.name, err)
		}
		if !past || len(booked.Attendees) == 0 {
			continue
		}

		status := outcome(rng, p, o)
		if err := c.do(ctx, "POST", "/v1/sessions/attendees/"+booked.Attendees[0].ID+"/mark", map[string]any{
			"status": status, "allow_overdraft": true,
		}, nil); err != nil {
			return nil, fmt.Errorf("mark %s: %w", p.name, err)
		}
		// The last three weeks of training are logged set by set; older
		// sessions are attendance only, as most trainers' history is.
		if status == "completed" && recordedAt.Sub(o.startsAt) < 21*24*time.Hour {
			if err := c.logWorkout(ctx, rng, p, id, booked.ID, o.startsAt, exercises, workouts[o.who]); err != nil {
				return nil, err
			}
			workouts[o.who]++
		}
	}

	// Measurements every four weeks from when each client began.
	for i, p := range roster {
		for w := -p.startWeek; w <= 0; w += 4 {
			progress := float64(p.startWeek + w)
			day := weekStart.AddDate(0, 0, 7*w)
			if day.After(recordedAt) {
				break
			}
			weight := p.weightKg - 0.35*progress + rng.Float64()*0.4
			if err := c.do(ctx, "POST", "/v1/clients/"+clientIDs[i]+"/biometrics", map[string]any{
				"measured_on":  day.Format("2006-01-02"),
				"weight_grams": int(math.Round(weight * 1000)),
				"body_fat_bp":  p.bodyFatBP - int(progress*18),
			}, nil); err != nil {
				return nil, fmt.Errorf("measure %s: %w", p.name, err)
			}
		}
	}

	return c.record(ctx, session.Account)
}

// placeFor is where a session happens: Saturday mornings at the beach,
// everything else at the studio.
func placeFor(o occurrence) string {
	if o.startsAt.Weekday() == time.Saturday {
		return "beach"
	}
	return "studio"
}

// outcome is how a past session went: nearly always delivered, with the
// occasional no-show and late cancel a real diary has.
func outcome(rng *mrand.Rand, p person, o occurrence) string {
	if p.fading && recordedAt.Sub(o.startsAt) < 15*24*time.Hour {
		return "no_show"
	}
	switch r := rng.Float64(); {
	case r < 0.05:
		return "no_show"
	case r < 0.09:
		return "late_cancel"
	case r < 0.12:
		return "early_cancel"
	default:
		return "completed"
	}
}

func (c *apiClient) sellPack(ctx context.Context, clientID string, who person, issued time.Time) (string, error) {
	p := who.pack
	// Most clients are on 14-day terms. The one who owes is a company paying
	// net-30, so their open invoice is current; the overdue one was given a
	// week and has let it pass.
	terms := 14
	switch {
	case who.owes:
		terms = 30
	case who.overdue:
		terms = 7
	}
	var draft idOnly
	if err := c.do(ctx, "POST", "/v1/invoices", map[string]any{
		"client_id": clientID, "currency": "AED",
		"lines": []map[string]any{{
			"kind": "package", "description": p.name, "quantity": 1,
			"unit_price_minor": p.priceMinor, "package_credits": p.credits,
		}},
	}, &draft); err != nil {
		return "", err
	}
	day := issued.In(dubai).Format("2006-01-02")
	due := issued.In(dubai).AddDate(0, 0, terms).Format("2006-01-02")
	if err := c.do(ctx, "POST", "/v1/invoices/"+draft.ID+"/issue", map[string]any{
		"issue_date": day, "due_date": due,
	}, nil); err != nil {
		return "", err
	}
	return draft.ID, nil
}

// maybePay records payment a few days after issue. A client who owes leaves
// their latest invoice open; one marked overdue leaves it open past its due
// date.
func (c *apiClient) maybePay(ctx context.Context, rng *mrand.Rand, p person, invoiceID string, issued, firstSession time.Time) error {
	paidOn := issued.AddDate(0, 0, 1+rng.Intn(4))
	openLatest := (p.owes || p.overdue) && recordedAt.Sub(issued) < 21*24*time.Hour
	if paidOn.After(recordedAt) || openLatest {
		return nil
	}
	var invoice struct {
		TotalMinor int64 `json:"total_minor"`
	}
	if err := c.do(ctx, "GET", "/v1/invoices/"+invoiceID, nil, &invoice); err != nil {
		return err
	}
	instrument := "bank_transfer"
	if rng.Float64() < 0.25 {
		instrument = "cash"
	}
	return c.do(ctx, "POST", "/v1/invoices/"+invoiceID+"/payments", map[string]any{
		"amount_minor": invoice.TotalMinor, "currency": "AED", "instrument": instrument,
		"received_on": paidOn.In(dubai).Format("2006-01-02"),
		"reference":   fmt.Sprintf("TRF %06X", rng.Intn(1<<24)),
	}, nil)
}

func (c *apiClient) logWorkout(ctx context.Context, rng *mrand.Rand, p person, clientID, sessionID string, at time.Time, exercises map[string]string, week int) error {
	var workout idOnly
	if err := c.do(ctx, "POST", "/v1/workouts", map[string]any{
		"client_id": clientID, "session_id": sessionID,
		"performed_on": at.In(dubai).Format("2006-01-02"),
	}, &workout); err != nil {
		return fmt.Errorf("start workout: %w", err)
	}
	for li, lift := range p.lifts {
		exerciseID, ok := exercises[lift]
		if !ok {
			return fmt.Errorf("no exercise called %q in the library", lift)
		}
		// Loads start from bodyweight and creep up session by session: the
		// progression the floor logger's "repeat last time" is built on.
		base := math.Round((p.weightKg*(0.9-0.15*float64(li))+2.5*float64(week))/2.5) * 2.5
		index := 1
		if li == 0 {
			warm := int(base*0.5) * 1000
			if err := c.do(ctx, "POST", "/v1/workouts/"+workout.ID+"/sets", map[string]any{
				"exercise_id": exerciseID, "set_index": index, "reps": 10, "load_grams": warm, "is_warmup": true,
			}, nil); err != nil {
				return fmt.Errorf("log warm-up: %w", err)
			}
			index++
		}
		for s := 0; s < 3; s++ {
			rpe := 75 + 5*s + rng.Intn(2)*5
			if err := c.do(ctx, "POST", "/v1/workouts/"+workout.ID+"/sets", map[string]any{
				"exercise_id": exerciseID, "set_index": index, "reps": 8 - s,
				"load_grams": int(base * 1000), "rpe_tenths": rpe,
			}, nil); err != nil {
				return fmt.Errorf("log set: %w", err)
			}
			index++
		}
	}
	return c.do(ctx, "POST", "/v1/workouts/"+workout.ID+"/complete", map[string]any{}, nil)
}

// record captures what a device would see: its first sync, and the answers
// the app asks the server for.
func (c *apiClient) record(ctx context.Context, account json.RawMessage) (*Recording, error) {
	byCollection := map[string][]json.RawMessage{}
	var order []string
	cursor := ""
	for page := 0; page < 200; page++ {
		var result struct {
			Cursor  string `json:"cursor"`
			HasMore bool   `json:"has_more"`
			Changes []struct {
				Collection string            `json:"collection"`
				Rows       []json.RawMessage `json:"rows"`
			} `json:"changes"`
		}
		if err := c.do(ctx, "GET", "/v1/sync/pull?limit=500&cursor="+cursor, nil, &result); err != nil {
			return nil, fmt.Errorf("pull: %w", err)
		}
		for _, change := range result.Changes {
			if _, seen := byCollection[change.Collection]; !seen {
				order = append(order, change.Collection)
			}
			byCollection[change.Collection] = append(byCollection[change.Collection], change.Rows...)
		}
		cursor = result.Cursor
		if !result.HasMore {
			break
		}
	}

	rec := &Recording{
		RecordedAt: recordedAt, Timezone: timezone, UTCOffsetMinutes: 4 * 60,
		Account: account, Reads: map[string]json.RawMessage{},
	}
	wallEnd := time.Now()
	for _, name := range order {
		rows := byCollection[name]
		for i, row := range rows {
			fixed, err := restamp(row, c.wallStart, wallEnd)
			if err != nil {
				return nil, fmt.Errorf("restamp %s: %w", name, err)
			}
			rows[i] = fixed
		}
		rec.Pull = append(rec.Pull, pulledCollection{Collection: name, Rows: rows})
	}
	// The reads the app makes. Each slice that adds a server-computed screen
	// adds its path here and re-records.
	for _, path := range []string{
		"/v1/dashboard", "/v1/receivables",
		"/v1/settings", "/v1/subscription", "/v1/session/profile", "/v1/session/devices",
	} {
		var raw json.RawMessage
		if err := c.do(ctx, "GET", path, nil, &raw); err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		fixed, err := restampRead(raw, c.wallStart, wallEnd)
		if err != nil {
			return nil, fmt.Errorf("restamp %s: %w", path, err)
		}
		rec.Reads[path] = fixed
	}
	return rec, nil
}

// ---------------------------------------------------------------------------

type idOnly struct {
	ID string `json:"id"`
}

const (
	iPhoneSafari = "Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Mobile/15E148 Safari/604.1"
	macChrome    = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0.0.0 Safari/537.36"
)

type apiClient struct {
	base      string
	token     string
	userAgent string
	// wallStart is when the run began on the real clock; see restamp.
	wallStart time.Time
}

func (c *apiClient) balance(ctx context.Context, clientID string) (int, error) {
	var b struct {
		Remaining int `json:"remaining"`
	}
	if err := c.do(ctx, "GET", "/v1/credits/clients/"+clientID+"/balance", nil, &b); err != nil {
		return 0, fmt.Errorf("balance: %w", err)
	}
	return b.Remaining, nil
}

func (c *apiClient) exercises(ctx context.Context) (map[string]string, error) {
	var list struct {
		Exercises []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"exercises"`
	}
	if err := c.do(ctx, "GET", "/v1/exercises", nil, &list); err != nil {
		return nil, fmt.Errorf("list exercises: %w", err)
	}
	byName := map[string]string{}
	for _, e := range list.Exercises {
		byName[e.Name] = e.ID
	}
	return byName, nil
}

func (c *apiClient) do(ctx context.Context, method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.userAgent != "" {
		req.Header.Set("User-Agent", c.userAgent)
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if method != "GET" {
		req.Header.Set("Idempotency-Key", hex.EncodeToString(randomBytes(16)))
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return err
	}
	if res.StatusCode >= 300 {
		return fmt.Errorf("%s %s: %d %s", method, path, res.StatusCode, strings.TrimSpace(string(raw)))
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, out)
}

func randomBytes(n int) []byte {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return b
}

// anchors are the business dates a row can carry, most specific first.
var anchors = []string{"issue_date", "received_on", "performed_on", "measured_on", "purchased_on", "starts_at"}

// restamp moves the times Postgres stamped with its own now() onto the
// scenario's timeline.
//
// The services take "now" from the fixed clock, but column defaults and
// triggers (created_at, updated_at, issued_at, settled_at) use the database's,
// so a recording made today would say every invoice was issued today, months
// after its issue date. Any time that falls inside the run's real-clock
// window is one of those; it becomes the business date the row carries, or
// the recording's "now" if that is later or there is none.
func restamp(raw json.RawMessage, wallFrom, wallTo time.Time) (json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var row map[string]any
	if err := decoder.Decode(&row); err != nil {
		return nil, err
	}

	when := recordedAt
	for _, key := range anchors {
		value, _ := row[key].(string)
		var t time.Time
		var err error
		if len(value) == len("2006-01-02") {
			t, err = time.ParseInLocation("2006-01-02", value, dubai)
			t = t.Add(9 * time.Hour)
		} else {
			t, err = time.Parse(time.RFC3339Nano, value)
		}
		if value == "" || err != nil {
			continue
		}
		if t.Before(when) {
			when = t
		}
		break
	}

	stamp := when.UTC().Format(time.RFC3339Nano)
	changed := false
	for key, value := range row {
		text, ok := value.(string)
		if !ok {
			continue
		}
		t, err := time.Parse(time.RFC3339Nano, text)
		if err != nil || t.Before(wallFrom.Add(-time.Minute)) || t.After(wallTo.Add(time.Minute)) {
			continue
		}
		row[key] = stamp
		changed = true
	}
	if !changed {
		return raw, nil
	}
	return json.Marshal(row)
}

// restampRead does for a read what restamp does for a row, at any depth: a
// read has no business date of its own, so a database stamp inside the run's
// window becomes the recording's "now" — the devices list says Sam signed in
// this morning, not on the day the file was recorded.
func restampRead(raw json.RawMessage, wallFrom, wallTo time.Time) (json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	stamp := recordedAt.UTC().Format(time.RFC3339Nano)
	var walk func(v any) any
	walk = func(v any) any {
		switch x := v.(type) {
		case map[string]any:
			for k, inner := range x {
				x[k] = walk(inner)
			}
		case []any:
			for i, inner := range x {
				x[i] = walk(inner)
			}
		case string:
			if t, err := time.Parse(time.RFC3339Nano, x); err == nil &&
				!t.Before(wallFrom.Add(-time.Minute)) && !t.After(wallTo.Add(time.Minute)) {
				return stamp
			}
		}
		return v
	}
	return json.Marshal(walk(value))
}
