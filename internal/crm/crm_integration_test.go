//go:build integration

package crm_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/NewMux/mtdrb_go/internal/crm"
	"github.com/NewMux/mtdrb_go/internal/db"
	"github.com/NewMux/mtdrb_go/internal/platform/clock"
	"github.com/NewMux/mtdrb_go/internal/platform/errs"
	"github.com/NewMux/mtdrb_go/internal/platform/ids"
	"github.com/NewMux/mtdrb_go/internal/testsupport"
)

var jan15 = time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)

const columnKey = "test-column-encryption-key-32byt"

type fixture struct {
	svc      *crm.Service
	pool     *db.Pool
	tenantID ids.ID
}

func setup(t *testing.T) fixture {
	t.Helper()
	testsupport.RequireDB(t)
	testsupport.Reset(t)

	pool := testsupport.OpenApp(t)
	tenantID := ids.New()

	owner := testsupport.OpenOwner(t)
	if _, err := owner.Raw().Exec(context.Background(),
		`INSERT INTO tenants (id, name, default_currency) VALUES ($1, 'Test Gym', 'EUR')`, tenantID); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	return fixture{
		svc:      crm.NewService(clock.Fixed{T: jan15}, []byte(columnKey)),
		pool:     pool,
		tenantID: tenantID,
	}
}

func (f fixture) tx(t *testing.T, fn func(tx pgx.Tx) error) error {
	t.Helper()
	return f.pool.InTenantTx(context.Background(), f.tenantID, fn)
}

func ptr[T any](v T) *T { return &v }

func TestCreateAndGetClient(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	var created crm.Client
	if err := f.tx(t, func(tx pgx.Tx) error {
		var err error
		created, err = f.svc.Create(ctx, tx, f.tenantID, crm.CreateInput{
			FullName:              "  Dana Rivers  ",
			Email:                 ptr("  Dana@Example.COM "),
			Phone:                 ptr("+49 30 123456"),
			DateOfBirth:           ptr(time.Date(1991, 4, 7, 0, 0, 0, 0, time.UTC)),
			EmergencyContactName:  ptr("Sam Rivers"),
			EmergencyContactPhone: ptr("+49 30 654321"),
			DefaultRateMinor:      ptr(int64(5000)),
			Notes:                 "prefers early mornings",
		})
		return err
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	if created.FullName != "Dana Rivers" {
		t.Errorf("name not trimmed: %q", created.FullName)
	}
	if created.Email == nil || *created.Email != "dana@example.com" {
		t.Errorf("email not normalised: %v", created.Email)
	}
	if created.Status != crm.StatusActive {
		t.Errorf("status = %q, want active by default", created.Status)
	}
	if created.ServerSeq == 0 {
		t.Error("server_seq was not assigned; incremental sync would never see this row")
	}
}

// Medical notes are the one field encrypted at the column level. A database
// read must not disclose them.
func TestMedicalNotesAreEncryptedAtRest(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	const secret = "severe peanut allergy, carries an epipen"

	var client crm.Client
	if err := f.tx(t, func(tx pgx.Tx) error {
		var err error
		client, err = f.svc.Create(ctx, tx, f.tenantID, crm.CreateInput{
			FullName:     "Dana Rivers",
			MedicalNotes: ptr(secret),
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	// Reading the raw column as the owner must not reveal the plaintext.
	owner := testsupport.OpenOwner(t)
	var raw []byte
	if err := owner.Raw().QueryRow(ctx,
		`SELECT medical_notes_encrypted FROM clients WHERE id = $1`, client.ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if len(raw) == 0 {
		t.Fatal("medical notes were not stored at all")
	}
	if strings.Contains(string(raw), "peanut") {
		t.Fatal("medical notes are readable directly from the column")
	}

	// An ordinary profile fetch must not carry them either.
	if err := f.tx(t, func(tx pgx.Tx) error {
		plain, err := f.svc.Get(ctx, tx, client.ID)
		if err != nil {
			return err
		}
		if plain.MedicalNotes != nil {
			t.Error("Get returned medical notes; they require an explicit call")
		}

		withMedical, err := f.svc.GetWithMedical(ctx, tx, client.ID)
		if err != nil {
			return err
		}
		if withMedical.MedicalNotes == nil || *withMedical.MedicalNotes != secret {
			t.Errorf("decrypted notes = %v, want %q", withMedical.MedicalNotes, secret)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// A wrong column key must fail loudly. Returning an empty field would read as
// "no medical conditions", which is the dangerous failure mode.
func TestWrongColumnKeyFailsLoudly(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	var client crm.Client
	if err := f.tx(t, func(tx pgx.Tx) error {
		var err error
		client, err = f.svc.Create(ctx, tx, f.tenantID, crm.CreateInput{
			FullName: "Dana Rivers", MedicalNotes: ptr("asthma"),
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	wrongKey := crm.NewService(clock.Fixed{T: jan15}, []byte("a-completely-different-key-32byt"))
	err := f.tx(t, func(tx pgx.Tx) error {
		c, err := wrongKey.GetWithMedical(ctx, tx, client.ID)
		if err == nil && c.MedicalNotes != nil && *c.MedicalNotes == "asthma" {
			t.Fatal("notes decrypted with the wrong key")
		}
		return err
	})
	if err == nil {
		t.Fatal("decrypting with the wrong key silently succeeded")
	}
}

func TestUpdateDistinguishesClearingFromSkipping(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	var client crm.Client
	if err := f.tx(t, func(tx pgx.Tx) error {
		var err error
		client, err = f.svc.Create(ctx, tx, f.tenantID, crm.CreateInput{
			FullName:     "Dana Rivers",
			Phone:        ptr("+49 30 123456"),
			MedicalNotes: ptr("asthma"),
			Notes:        "original",
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	// Updating only the name must leave phone and medical notes untouched.
	if err := f.tx(t, func(tx pgx.Tx) error {
		updated, err := f.svc.Update(ctx, tx, client.ID, crm.UpdateInput{
			FullName: ptr("Dana R. Rivers"),
		})
		if err != nil {
			return err
		}
		if updated.FullName != "Dana R. Rivers" {
			t.Errorf("name = %q", updated.FullName)
		}
		if updated.Phone == nil || *updated.Phone != "+49 30 123456" {
			t.Error("phone was cleared by an unrelated update")
		}
		if updated.Notes != "original" {
			t.Error("notes were cleared by an unrelated update")
		}

		withMedical, err := f.svc.GetWithMedical(ctx, tx, client.ID)
		if err != nil {
			return err
		}
		if withMedical.MedicalNotes == nil || *withMedical.MedicalNotes != "asthma" {
			t.Error("medical notes were cleared by an unrelated update")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// Explicitly clearing must actually clear.
	if err := f.tx(t, func(tx pgx.Tx) error {
		var nilNotes *string
		var nilPhone *string
		if _, err := f.svc.Update(ctx, tx, client.ID, crm.UpdateInput{
			MedicalNotes: &nilNotes,
			Phone:        &nilPhone,
		}); err != nil {
			return err
		}
		c, err := f.svc.GetWithMedical(ctx, tx, client.ID)
		if err != nil {
			return err
		}
		if c.MedicalNotes != nil {
			t.Errorf("medical notes were not cleared: %v", *c.MedicalNotes)
		}
		if c.Phone != nil {
			t.Errorf("phone was not cleared: %v", *c.Phone)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestDuplicateEmailIsRejected(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	if err := f.tx(t, func(tx pgx.Tx) error {
		_, err := f.svc.Create(ctx, tx, f.tenantID, crm.CreateInput{
			FullName: "Dana", Email: ptr("dana@example.com"),
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	err := f.tx(t, func(tx pgx.Tx) error {
		_, err := f.svc.Create(ctx, tx, f.tenantID, crm.CreateInput{
			FullName: "Dana Again", Email: ptr("DANA@EXAMPLE.COM"),
		})
		return err
	})
	if err == nil {
		t.Fatal("duplicate email accepted")
	}
	if errs.KindOf(err) != errs.KindConflict {
		t.Errorf("kind = %q, want conflict", errs.KindOf(err))
	}
}

func TestSoftDeleteHidesClientButKeepsTheRow(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	var client crm.Client
	if err := f.tx(t, func(tx pgx.Tx) error {
		var err error
		client, err = f.svc.Create(ctx, tx, f.tenantID, crm.CreateInput{FullName: "Dana"})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	if err := f.tx(t, func(tx pgx.Tx) error {
		return f.svc.Delete(ctx, tx, client.ID)
	}); err != nil {
		t.Fatal(err)
	}

	if err := f.tx(t, func(tx pgx.Tx) error {
		if _, err := f.svc.Get(ctx, tx, client.ID); err == nil {
			t.Error("deleted client is still readable")
		}
		list, err := f.svc.List(ctx, tx, crm.ListFilter{})
		if err != nil {
			return err
		}
		if len(list) != 0 {
			t.Errorf("deleted client still appears in listings (%d rows)", len(list))
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// The row survives, because journal entries reference this client's
	// sessions and invoices.
	owner := testsupport.OpenOwner(t)
	var count int
	if err := owner.Raw().QueryRow(ctx, `SELECT count(*) FROM clients WHERE id = $1`, client.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Error("soft delete removed the row; financial history would be orphaned")
	}
}

func TestTagsAreReplacedNotAccumulated(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	var client crm.Client
	var vip, rehab, offPeak crm.Tag
	if err := f.tx(t, func(tx pgx.Tx) error {
		var err error
		if vip, err = f.svc.CreateTag(ctx, tx, f.tenantID, "VIP", ptr("#d4af37")); err != nil {
			return err
		}
		if rehab, err = f.svc.CreateTag(ctx, tx, f.tenantID, "Rehab", nil); err != nil {
			return err
		}
		if offPeak, err = f.svc.CreateTag(ctx, tx, f.tenantID, "Off-Peak", nil); err != nil {
			return err
		}
		client, err = f.svc.Create(ctx, tx, f.tenantID, crm.CreateInput{
			FullName: "Dana", TagIDs: []ids.ID{vip.ID, rehab.ID},
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if len(client.Tags) != 2 {
		t.Fatalf("got %d tags, want 2", len(client.Tags))
	}

	// Replaying the same assignment must be idempotent — the offline outbox
	// depends on this.
	if err := f.tx(t, func(tx pgx.Tx) error {
		return f.svc.SetTags(ctx, tx, f.tenantID, client.ID, []ids.ID{vip.ID, rehab.ID})
	}); err != nil {
		t.Fatal(err)
	}
	if err := f.tx(t, func(tx pgx.Tx) error {
		c, err := f.svc.Get(ctx, tx, client.ID)
		if err != nil {
			return err
		}
		if len(c.Tags) != 2 {
			t.Errorf("replaying the same tags produced %d tags, want 2", len(c.Tags))
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// Replacing with a different set removes the old ones.
	if err := f.tx(t, func(tx pgx.Tx) error {
		if err := f.svc.SetTags(ctx, tx, f.tenantID, client.ID, []ids.ID{offPeak.ID}); err != nil {
			return err
		}
		c, err := f.svc.Get(ctx, tx, client.ID)
		if err != nil {
			return err
		}
		if len(c.Tags) != 1 || c.Tags[0].Name != "Off-Peak" {
			t.Errorf("tags = %+v, want only Off-Peak", c.Tags)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestListFiltersBySearchStatusAndTag(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	var vip crm.Tag
	if err := f.tx(t, func(tx pgx.Tx) error {
		var err error
		if vip, err = f.svc.CreateTag(ctx, tx, f.tenantID, "VIP", nil); err != nil {
			return err
		}
		if _, err = f.svc.Create(ctx, tx, f.tenantID, crm.CreateInput{
			FullName: "Dana Rivers", Email: ptr("dana@example.com"), TagIDs: []ids.ID{vip.ID},
		}); err != nil {
			return err
		}
		if _, err = f.svc.Create(ctx, tx, f.tenantID, crm.CreateInput{
			FullName: "Morgan Hale", Status: crm.StatusPaused,
		}); err != nil {
			return err
		}
		_, err = f.svc.Create(ctx, tx, f.tenantID, crm.CreateInput{FullName: "Robin Vale"})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	if err := f.tx(t, func(tx pgx.Tx) error {
		all, err := f.svc.List(ctx, tx, crm.ListFilter{})
		if err != nil {
			return err
		}
		if len(all) != 3 {
			t.Errorf("unfiltered list returned %d, want 3", len(all))
		}

		byName, err := f.svc.List(ctx, tx, crm.ListFilter{Search: "riv"})
		if err != nil {
			return err
		}
		if len(byName) != 1 || byName[0].FullName != "Dana Rivers" {
			t.Errorf("name search returned %+v", byName)
		}

		byEmail, err := f.svc.List(ctx, tx, crm.ListFilter{Search: "example.com"})
		if err != nil {
			return err
		}
		if len(byEmail) != 1 {
			t.Errorf("email search returned %d, want 1", len(byEmail))
		}

		paused, err := f.svc.List(ctx, tx, crm.ListFilter{Status: crm.StatusPaused})
		if err != nil {
			return err
		}
		if len(paused) != 1 || paused[0].FullName != "Morgan Hale" {
			t.Errorf("status filter returned %+v", paused)
		}

		tagged, err := f.svc.List(ctx, tx, crm.ListFilter{TagID: &vip.ID})
		if err != nil {
			return err
		}
		if len(tagged) != 1 || tagged[0].FullName != "Dana Rivers" {
			t.Errorf("tag filter returned %+v", tagged)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestParQDerivesClearanceAndRejectsIncompleteSubmissions(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	var client crm.Client
	if err := f.tx(t, func(tx pgx.Tx) error {
		var err error
		client, err = f.svc.Create(ctx, tx, f.tenantID, crm.CreateInput{FullName: "Dana"})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	allNo := make([]crm.ParQAnswer, 0, len(crm.StandardParQQuestions))
	for _, q := range crm.StandardParQQuestions {
		allNo = append(allNo, crm.ParQAnswer{Question: q, Yes: false})
	}

	if err := f.tx(t, func(tx pgx.Tx) error {
		r, err := f.svc.SubmitParQ(ctx, tx, f.tenantID, client.ID, allNo)
		if err != nil {
			return err
		}
		if r.RequiresClearance {
			t.Error("all-no questionnaire flagged as needing clearance")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// One yes means medical clearance is needed, whatever the client claims.
	oneYes := make([]crm.ParQAnswer, len(allNo))
	copy(oneYes, allNo)
	oneYes[0].Yes = true
	oneYes[0].Detail = "arrhythmia diagnosed 2024"

	if err := f.tx(t, func(tx pgx.Tx) error {
		r, err := f.svc.SubmitParQ(ctx, tx, f.tenantID, client.ID, oneYes)
		if err != nil {
			return err
		}
		if !r.RequiresClearance {
			t.Error("a yes answer did not trigger the clearance flag")
		}

		latest, err := f.svc.LatestParQ(ctx, tx, client.ID)
		if err != nil {
			return err
		}
		if !latest.RequiresClearance {
			t.Error("latest questionnaire lost the clearance flag")
		}
		if len(latest.Answers) != len(crm.StandardParQQuestions) {
			t.Errorf("stored %d answers, want %d", len(latest.Answers), len(crm.StandardParQQuestions))
		}
		if latest.Answers[0].Detail != "arrhythmia diagnosed 2024" {
			t.Error("answer detail was not preserved")
		}

		// Clearance can then be recorded.
		if err := f.svc.RecordMedicalClearance(ctx, tx, latest.ID); err != nil {
			return err
		}
		cleared, err := f.svc.LatestParQ(ctx, tx, client.ID)
		if err != nil {
			return err
		}
		if cleared.ClearedAt == nil {
			t.Error("clearance was not recorded")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// An incomplete questionnaire is not a valid screen.
	err := f.tx(t, func(tx pgx.Tx) error {
		_, err := f.svc.SubmitParQ(ctx, tx, f.tenantID, client.ID, allNo[:3])
		return err
	})
	if err == nil {
		t.Fatal("incomplete PAR-Q was accepted")
	}
	if errs.KindOf(err) != errs.KindInvalid {
		t.Errorf("kind = %q, want invalid", errs.KindOf(err))
	}
}

func TestWaiverVersionsAndSignatureSnapshot(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	var client crm.Client
	var v1 crm.Waiver
	if err := f.tx(t, func(tx pgx.Tx) error {
		var err error
		if client, err = f.svc.Create(ctx, tx, f.tenantID, crm.CreateInput{FullName: "Dana"}); err != nil {
			return err
		}
		v1, err = f.svc.CreateWaiver(ctx, tx, f.tenantID, "Liability Release", "Original terms.")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if v1.Version != 1 || !v1.IsActive {
		t.Errorf("first waiver: version %d active %v", v1.Version, v1.IsActive)
	}

	// The client signs version 1.
	if err := f.tx(t, func(tx pgx.Tx) error {
		sig, err := f.svc.Sign(ctx, tx, f.tenantID, crm.SignInput{
			WaiverID: v1.ID, ClientID: client.ID, SignedName: "Dana Rivers", IP: "203.0.113.7",
		})
		if err != nil {
			return err
		}
		if sig.SignedName != "Dana Rivers" {
			t.Errorf("signed name = %q", sig.SignedName)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// A revision creates version 2 and retires version 1.
	var v2 crm.Waiver
	if err := f.tx(t, func(tx pgx.Tx) error {
		var err error
		v2, err = f.svc.CreateWaiver(ctx, tx, f.tenantID, "Liability Release", "Revised terms, stricter.")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if v2.Version != 2 {
		t.Errorf("revision version = %d, want 2", v2.Version)
	}

	if err := f.tx(t, func(tx pgx.Tx) error {
		active, err := f.svc.ListWaivers(ctx, tx, true)
		if err != nil {
			return err
		}
		if len(active) != 1 || active[0].Version != 2 {
			t.Errorf("active waivers = %+v, want only version 2", active)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// The signature still records the text that was actually agreed to.
	owner := testsupport.OpenOwner(t)
	var signedBody string
	if err := owner.Raw().QueryRow(ctx,
		`SELECT signed_body FROM waiver_signatures WHERE client_id = $1`, client.ID).Scan(&signedBody); err != nil {
		t.Fatal(err)
	}
	if signedBody != "Original terms." {
		t.Errorf("signature snapshot = %q, want the version 1 text", signedBody)
	}
}

func TestSigningTheSameWaiverTwiceIsRejected(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	var client crm.Client
	var waiver crm.Waiver
	if err := f.tx(t, func(tx pgx.Tx) error {
		var err error
		if client, err = f.svc.Create(ctx, tx, f.tenantID, crm.CreateInput{FullName: "Dana"}); err != nil {
			return err
		}
		if waiver, err = f.svc.CreateWaiver(ctx, tx, f.tenantID, "Release", "Terms."); err != nil {
			return err
		}
		_, err = f.svc.Sign(ctx, tx, f.tenantID, crm.SignInput{
			WaiverID: waiver.ID, ClientID: client.ID, SignedName: "Dana",
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	err := f.tx(t, func(tx pgx.Tx) error {
		_, err := f.svc.Sign(ctx, tx, f.tenantID, crm.SignInput{
			WaiverID: waiver.ID, ClientID: client.ID, SignedName: "Dana",
		})
		return err
	})
	if err == nil {
		t.Fatal("the same waiver was signed twice")
	}
	if errs.KindOf(err) != errs.KindConflict {
		t.Errorf("kind = %q, want conflict", errs.KindOf(err))
	}
}

func TestBiometricsStoreIntegersAndValidateRanges(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	var client crm.Client
	if err := f.tx(t, func(tx pgx.Tx) error {
		var err error
		client, err = f.svc.Create(ctx, tx, f.tenantID, crm.CreateInput{FullName: "Dana"})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	if err := f.tx(t, func(tx pgx.Tx) error {
		e, err := f.svc.RecordBiometrics(ctx, tx, f.tenantID, crm.BiometricInput{
			ClientID:       client.ID,
			MeasuredOn:     jan15,
			WeightGrams:    ptr(int32(72400)),
			BodyFatBP:      ptr(int32(1550)),
			Circumferences: map[string]int32{"waist": 810, "hips": 950},
			Notes:          "morning, fasted",
		})
		if err != nil {
			return err
		}
		if *e.WeightGrams != 72400 || *e.BodyFatBP != 1550 {
			t.Errorf("values not stored exactly: %+v", e)
		}
		if e.Circumferences["waist"] != 810 {
			t.Errorf("circumferences = %+v", e.Circumferences)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// Out-of-range values are refused.
	bad := []crm.BiometricInput{
		{ClientID: client.ID, WeightGrams: ptr(int32(0))},
		{ClientID: client.ID, WeightGrams: ptr(int32(-500))},
		{ClientID: client.ID, BodyFatBP: ptr(int32(-1))},
		{ClientID: client.ID, BodyFatBP: ptr(int32(10001))},
		{ClientID: client.ID, Circumferences: map[string]int32{"waist": -10}},
	}
	for i, in := range bad {
		err := f.tx(t, func(tx pgx.Tx) error {
			_, err := f.svc.RecordBiometrics(ctx, tx, f.tenantID, in)
			return err
		})
		if err == nil {
			t.Errorf("case %d: out-of-range measurement accepted", i)
		}
	}
}

func TestBiometricHistoryIsNewestFirst(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	var client crm.Client
	if err := f.tx(t, func(tx pgx.Tx) error {
		var err error
		client, err = f.svc.Create(ctx, tx, f.tenantID, crm.CreateInput{FullName: "Dana"})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	weights := []int32{75000, 74000, 73000}
	for i, w := range weights {
		day := jan15.AddDate(0, 0, i*7)
		if err := f.tx(t, func(tx pgx.Tx) error {
			_, err := f.svc.RecordBiometrics(ctx, tx, f.tenantID, crm.BiometricInput{
				ClientID: client.ID, MeasuredOn: day, WeightGrams: &weights[i],
			})
			return err
		}); err != nil {
			t.Fatalf("record %d (%d g): %v", i, w, err)
		}
	}

	if err := f.tx(t, func(tx pgx.Tx) error {
		history, err := f.svc.BiometricHistory(ctx, tx, client.ID, 10)
		if err != nil {
			return err
		}
		if len(history) != 3 {
			t.Fatalf("history has %d entries, want 3", len(history))
		}
		if *history[0].WeightGrams != 73000 {
			t.Errorf("newest entry = %d g, want 73000", *history[0].WeightGrams)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// The PRD requires that a client can see zero of another client's records.
// The policy enforces it; this proves the policy is actually in force.
func TestPortalSessionSeesOnlyItsOwnClientRecord(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	var alice, bob crm.Client
	if err := f.tx(t, func(tx pgx.Tx) error {
		var err error
		if alice, err = f.svc.Create(ctx, tx, f.tenantID, crm.CreateInput{FullName: "Alice"}); err != nil {
			return err
		}
		if bob, err = f.svc.Create(ctx, tx, f.tenantID, crm.CreateInput{FullName: "Bob"}); err != nil {
			return err
		}
		if _, err = f.svc.RecordBiometrics(ctx, tx, f.tenantID, crm.BiometricInput{
			ClientID: alice.ID, MeasuredOn: jan15, WeightGrams: ptr(int32(70000)),
		}); err != nil {
			return err
		}
		_, err = f.svc.RecordBiometrics(ctx, tx, f.tenantID, crm.BiometricInput{
			ClientID: bob.ID, MeasuredOn: jan15, WeightGrams: ptr(int32(80000)),
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	// A portal session bound to Alice.
	if err := f.pool.InPortalTx(ctx, f.tenantID, alice.ID, func(tx pgx.Tx) error {
		list, err := f.svc.List(ctx, tx, crm.ListFilter{})
		if err != nil {
			return err
		}
		if len(list) != 1 || list[0].ID != alice.ID {
			t.Errorf("portal session sees %d clients, want only itself", len(list))
		}

		if _, err := f.svc.Get(ctx, tx, bob.ID); err == nil {
			t.Error("portal session read another client's record")
		}

		bobHistory, err := f.svc.BiometricHistory(ctx, tx, bob.ID, 10)
		if err != nil {
			return err
		}
		if len(bobHistory) != 0 {
			t.Errorf("portal session read %d of another client's measurements", len(bobHistory))
		}

		ownHistory, err := f.svc.BiometricHistory(ctx, tx, alice.ID, 10)
		if err != nil {
			return err
		}
		if len(ownHistory) != 1 {
			t.Errorf("portal session cannot read its own measurements (%d)", len(ownHistory))
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestClientsAreTenantIsolated(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	if err := f.tx(t, func(tx pgx.Tx) error {
		_, err := f.svc.Create(ctx, tx, f.tenantID, crm.CreateInput{FullName: "Dana"})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	other := ids.New()
	owner := testsupport.OpenOwner(t)
	if _, err := owner.Raw().Exec(ctx,
		`INSERT INTO tenants (id, name) VALUES ($1, 'Other Gym')`, other); err != nil {
		t.Fatal(err)
	}

	if err := f.pool.InTenantTx(ctx, other, func(tx pgx.Tx) error {
		list, err := f.svc.List(ctx, tx, crm.ListFilter{})
		if err != nil {
			return err
		}
		if len(list) != 0 {
			t.Errorf("another tenant's clients are visible (%d)", len(list))
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
