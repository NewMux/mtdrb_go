//go:build integration

package db_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/NewMux/mtdrb_go/internal/db"
	"github.com/NewMux/mtdrb_go/internal/testsupport"
)

func TestStatementTimeoutCancelsARunawayQuery(t *testing.T) {
	testsupport.RequireDB(t)
	ctx := context.Background()
	pool, err := db.Open(ctx, db.PoolConfig{URL: testsupport.AppURL(), MaxConns: 1, StatementTimeout: 200 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	start := time.Now()
	_, err = pool.Raw().Exec(ctx, "SELECT pg_sleep(5)")
	if err == nil || !strings.Contains(err.Error(), "statement timeout") {
		t.Fatalf("a five-second statement was not cancelled: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("cancelled after %v, want about 200ms", elapsed)
	}
	// The connection is still usable afterwards.
	var one int
	if err := pool.Raw().QueryRow(ctx, "SELECT 1").Scan(&one); err != nil || one != 1 {
		t.Errorf("pool unusable after a timeout: %v", err)
	}
}
