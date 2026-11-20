package service

import (
	"testing"

	"inventory-app/server-go/internal/testutil"
)

// A failed audit insert inside a transaction must not abort that transaction:
// the mutation it describes still has to commit.
func TestLogActivityFailureKeepsTransactionUsable(t *testing.T) {
	pool := testutil.OpenDB(t)
	ctx, tx, q := testutil.OpenTx(t, pool)

	// Postgres refuses NUL in a text column, so this insert fails server-side.
	if err := LogActivity(ctx, q, LogParams{Username: "test", Action: "create", EntityType: "test", Description: "bad\x00"}); err == nil {
		t.Fatal("expected the NUL-byte insert to fail")
	}

	var one int
	if err := tx.QueryRow(ctx, `SELECT 1`).Scan(&one); err != nil {
		t.Fatalf("transaction aborted by a failed activity log: %v", err)
	}

	if err := LogActivity(ctx, q, LogParams{Username: "test", Action: "create", EntityType: "test", Description: "savepoint ok"}); err != nil {
		t.Fatalf("successful log inside the transaction: %v", err)
	}
	var n int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM activity_log WHERE description = 'savepoint ok'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("logged row not visible in the transaction: n=%d err=%v", n, err)
	}
}
