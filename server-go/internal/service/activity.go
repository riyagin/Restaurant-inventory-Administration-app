package service

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"inventory-app/server-go/internal/db"
)

type LogParams struct {
	UserID      uuid.UUID
	Username    string
	Action      string
	EntityType  string
	EntityID    uuid.UUID
	Description string
}

func LogActivity(ctx context.Context, qtx *db.Queries, p LogParams) error {
	return qtx.InsertActivityLog(ctx, &db.InsertActivityLogParams{
		UserID:      pgtype.UUID{Bytes: p.UserID, Valid: p.UserID != uuid.Nil},
		Username:    p.Username,
		Action:      p.Action,
		EntityType:  p.EntityType,
		EntityID:    pgtype.UUID{Bytes: p.EntityID, Valid: p.EntityID != uuid.Nil},
		Description: p.Description,
	})
}
