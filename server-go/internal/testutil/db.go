// Package testutil provides shared infrastructure for integration tests that
// require a live PostgreSQL connection. Tests are automatically skipped when the
// database is unreachable so the test suite remains green in CI without a DB.
package testutil

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"

	"inventory-app/server-go/internal/db"
)

// OpenDB connects to the database described by server-go/.env (or environment
// variables). The pool is closed automatically when the test ends.
// The test is skipped if the database is unreachable.
func OpenDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	// .env lives two directories above internal/testutil/
	_ = godotenv.Load("../../.env")

	dsn := fmt.Sprintf("postgres://%s:%s@%s:%s/%s",
		getenv("DB_USER", "postgres"),
		getenv("DB_PASSWORD", "seesaw"),
		getenv("DB_HOST", "localhost"),
		getenv("DB_PORT", "5432"),
		getenv("DB_NAME", "inventory_app"),
	)
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Skipf("skipping integration test — cannot open DB pool: %v", err)
	}
	if err := pool.Ping(context.Background()); err != nil {
		pool.Close()
		t.Skipf("skipping integration test — DB ping failed: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// OpenTx begins a savepoint-safe transaction scoped to one test. The
// transaction is always rolled back when the test ends, so every test starts
// with a clean slate without truncating tables.
//
// Returns (ctx, raw tx, tx-scoped db.Queries). Use the raw tx for fixture
// INSERT/SELECT when you need pgx directly; use db.Queries when calling
// service functions that accept *db.Queries.
func OpenTx(t *testing.T, pool *pgxpool.Pool) (context.Context, pgx.Tx, *db.Queries) {
	t.Helper()
	ctx := context.Background()
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("OpenTx: begin: %v", err)
	}
	t.Cleanup(func() {
		// Rollback is idempotent — harmless if the tx was already committed.
		_ = tx.Rollback(ctx)
	})
	return ctx, tx, db.New(tx)
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
