package service

import (
	"context"
	"strings"
	"unicode"

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
		Action:      NormalizeLogAction(p.Action),
		EntityType:  NormalizeLogEntityType(p.EntityType),
		EntityID:    pgtype.UUID{Bytes: p.EntityID, Valid: p.EntityID != uuid.Nil},
		Description: p.Description,
	})
}

// System-event constants for activity_log rows that no user triggered.
const (
	SystemActor       = "system"
	SystemEntityType  = "system"
	ActionServerStart = "start"
	ActionServerStop  = "stop"
)

// LogSystemEvent records an event the server itself performed, with no user
// behind it. user_id and entity_id are left NULL (both columns are nullable);
// the actor shows as "system" in the log UI.
//
// Errors are returned rather than swallowed so the caller can decide, but a
// failure here must never stop the server from starting or shutting down.
func LogSystemEvent(ctx context.Context, q *db.Queries, action, description string) error {
	return LogActivity(ctx, q, LogParams{
		Username:    SystemActor,
		Action:      action,
		EntityType:  SystemEntityType,
		Description: description,
	})
}

// NormalizeLogAction folds an action to the lowercase form the activity-log UI
// filters on ("CREATE" → "create").
//
// Call sites spell actions inconsistently — the legacy Node backend wrote
// lowercase, the Go handlers write uppercase — which left the log table holding
// both. Since the list query matches entity_type/action with a plain `=`, an
// unnormalized row is invisible to the UI dropdowns. Normalizing here fixes
// every call site at once rather than relying on ~100 of them to agree.
func NormalizeLogAction(action string) string {
	return strings.ToLower(strings.TrimSpace(action))
}

// NormalizeLogEntityType folds an entity type to lower_snake_case, so the
// PascalCase spellings used by some handlers collapse onto the same value as
// the snake_case ones: "StockTransfer" and "stock_transfer" both become
// "stock_transfer", "POSImport" becomes "pos_import".
func NormalizeLogEntityType(entityType string) string {
	s := strings.TrimSpace(entityType)
	if s == "" {
		return ""
	}
	// Already snake_case (or a single lowercase word) — nothing to split.
	if !strings.ContainsFunc(s, unicode.IsUpper) {
		return strings.ToLower(s)
	}

	runes := []rune(s)
	var b strings.Builder
	for i, r := range runes {
		if unicode.IsUpper(r) && i > 0 {
			prev := runes[i-1]
			// Insert a boundary at lower→Upper ("StockTransfer") and at the end
			// of an acronym run, i.e. Upper→Upper followed by lower ("POSImport").
			if !unicode.IsUpper(prev) && prev != '_' {
				b.WriteRune('_')
			} else if unicode.IsUpper(prev) && i+1 < len(runes) && unicode.IsLower(runes[i+1]) {
				b.WriteRune('_')
			}
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}
