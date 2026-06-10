package service

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"inventory-app/server-go/internal/db"
)

// UpdateBalance adds delta to the account's balance.
// Positive delta increases the balance. Must be called with a transaction-scoped *db.Queries.
func UpdateBalance(ctx context.Context, qtx *db.Queries, accountID uuid.UUID, delta int64) error {
	return qtx.AddToAccountBalance(ctx, &db.AddToAccountBalanceParams{
		Column1: delta,
		ID:      pgtype.UUID{Bytes: accountID, Valid: true},
	})
}
