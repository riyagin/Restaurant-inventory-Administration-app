// Hand-written: the only file in this package that sqlc does not generate.

package db

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func NewPool(ctx context.Context, dbURL string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
}

// Savepoint runs fn inside a savepoint when q is bound to a transaction, so a
// statement that fails in fn rolls back to the savepoint instead of aborting
// the surrounding transaction. Outside a transaction fn simply runs on q.
func (q *Queries) Savepoint(ctx context.Context, fn func(*Queries) error) error {
	tx, ok := q.db.(pgx.Tx)
	if !ok {
		return fn(q)
	}
	sp, err := tx.Begin(ctx) // a nested Begin on a pgx.Tx is a SAVEPOINT
	if err != nil {
		return err
	}
	if err := fn(q.WithTx(sp)); err != nil {
		_ = sp.Rollback(ctx)
		return err
	}
	return sp.Commit(ctx)
}
