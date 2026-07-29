package handler_test

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
)

// TestMain sweeps fixture rows out of the database after the package's tests
// finish.
//
// These are integration tests against the real database, and their fixtures
// create accounts that get journal entries posted to them. A fixture's own
// t.Cleanup cannot remove such an account — journal_lines holds a foreign key to
// it — and because cleanup ignores the error, the failure is silent. Over many
// runs that accumulated hundreds of fake accounts carrying real balances in the
// live Chart of Accounts, along with the warehouses whose deletion was blocked
// by leftover stock_opname headers.
//
// The sweep is deliberately narrow. It only touches rows that carry the 8-hex
// suffix the fixtures generate AND, for accounts, have no COA number, no parent
// and no is_system flag — every real account is numbered, so the two sets cannot
// overlap. Deleting a fixture account means deleting the journal entries that
// reference it, so the balance those entries contributed to any surviving shared
// account (Selisih Persediaan, a payable, the suspense account) is rolled back
// at the same time, leaving the books balanced.
func TestMain(m *testing.M) {
	code := m.Run()
	if pool := sweepPool(); pool != nil {
		sweepFixtures(pool)
		pool.Close()
	}
	os.Exit(code)
}

// sweepPool opens a pool the same way testutil.OpenDB does. Returns nil when the
// database is unreachable, so a DB-less run stays green.
func sweepPool() *pgxpool.Pool {
	_ = godotenv.Load("../../.env")
	getenv := func(k, def string) string {
		if v := os.Getenv(k); v != "" {
			return v
		}
		return def
	}
	dsn := fmt.Sprintf("postgres://%s:%s@%s:%s/%s",
		getenv("DB_USER", "postgres"), getenv("DB_PASSWORD", "seesaw"),
		getenv("DB_HOST", "localhost"), getenv("DB_PORT", "5432"),
		getenv("DB_NAME", "inventory_app"))
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		return nil
	}
	if err := pool.Ping(context.Background()); err != nil {
		pool.Close()
		return nil
	}
	return pool
}

// fixtureAccountPredicate matches only accounts a test fixture could have made.
const fixtureAccountPredicate = `
	is_system = false AND account_number IS NULL AND parent_id IS NULL
	AND name ~ '[-_ ][0-9a-f]{8}$'`

func sweepFixtures(pool *pgxpool.Pool) {
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		return
	}
	defer tx.Rollback(ctx)

	// Warehouses first: their leftover opname headers are what blocks deletion.
	if _, err := tx.Exec(ctx, `
		CREATE TEMP TABLE sweep_wh ON COMMIT DROP AS
		SELECT id FROM warehouses WHERE name ~ '^WH[A-Za-z]*-[0-9a-f]{8}$'`); err != nil {
		return
	}
	for _, stmt := range []string{
		`DELETE FROM stock_opname_items WHERE opname_id IN (SELECT id FROM stock_opname WHERE warehouse_id IN (SELECT id FROM sweep_wh))`,
		`DELETE FROM stock_opname WHERE warehouse_id IN (SELECT id FROM sweep_wh)`,
		`DELETE FROM stock_history WHERE warehouse_id IN (SELECT id FROM sweep_wh)`,
		`DELETE FROM inventory WHERE warehouse_id IN (SELECT id FROM sweep_wh)`,
		`DELETE FROM warehouses WHERE id IN (SELECT id FROM sweep_wh)`,
	} {
		if _, err := tx.Exec(ctx, stmt); err != nil {
			return // something real still references them; leave everything alone
		}
	}

	// Accounts, the journal entries touching them, and the balance those entries
	// contributed to accounts that survive.
	if _, err := tx.Exec(ctx,
		`CREATE TEMP TABLE sweep_acct ON COMMIT DROP AS
		 SELECT id FROM accounts WHERE `+fixtureAccountPredicate); err != nil {
		return
	}
	for _, stmt := range []string{
		`CREATE TEMP TABLE sweep_entry ON COMMIT DROP AS
		 SELECT DISTINCT entry_id AS id FROM journal_lines WHERE account_id IN (SELECT id FROM sweep_acct)`,
		`CREATE TEMP TABLE sweep_restore ON COMMIT DROP AS
		 SELECT jl.account_id,
		        SUM(CASE WHEN a.account_type IN ('asset','expense') THEN jl.amount ELSE -jl.amount END)::bigint AS impact
		 FROM journal_lines jl JOIN accounts a ON a.id = jl.account_id
		 WHERE jl.entry_id IN (SELECT id FROM sweep_entry)
		   AND jl.account_id NOT IN (SELECT id FROM sweep_acct)
		 GROUP BY jl.account_id`,
		`UPDATE accounts a SET balance = a.balance - r.impact FROM sweep_restore r WHERE a.id = r.account_id`,
		`DELETE FROM journal_lines WHERE entry_id IN (SELECT id FROM sweep_entry)`,
		`DELETE FROM journal_entries WHERE id IN (SELECT id FROM sweep_entry)`,
		`UPDATE warehouses SET inventory_account_id = NULL WHERE inventory_account_id IN (SELECT id FROM sweep_acct)`,
		`DELETE FROM accounts WHERE id IN (SELECT id FROM sweep_acct)`,
	} {
		if _, err := tx.Exec(ctx, stmt); err != nil {
			return
		}
	}

	// Only commit if the books are still exactly right.
	var equation int64
	var drifted int
	if err := tx.QueryRow(ctx, `
		SELECT (COALESCE(SUM(balance) FILTER (WHERE account_type='asset'),0)
		      - COALESCE(SUM(balance) FILTER (WHERE account_type='liability'),0)
		      - COALESCE(SUM(balance) FILTER (WHERE account_type='equity'),0)
		      - COALESCE(SUM(balance) FILTER (WHERE account_type='revenue'),0)
		      + COALESCE(SUM(balance) FILTER (WHERE account_type='expense'),0)) FROM accounts`).
		Scan(&equation); err != nil {
		return
	}
	if err := tx.QueryRow(ctx, `
		SELECT COUNT(*) FROM (
		  SELECT a.id FROM accounts a LEFT JOIN journal_lines jl ON jl.account_id = a.id
		  GROUP BY a.id, a.account_type, a.balance
		  HAVING a.balance - COALESCE(SUM(CASE WHEN a.account_type IN ('asset','expense')
		         THEN jl.amount ELSE -jl.amount END),0) <> 0) d`).Scan(&drifted); err != nil {
		return
	}
	if equation != 0 || drifted != 0 {
		fmt.Fprintf(os.Stderr,
			"fixture sweep skipped: books would not balance (equation=%d, drifted=%d)\n", equation, drifted)
		return
	}
	_ = tx.Commit(ctx)
}
