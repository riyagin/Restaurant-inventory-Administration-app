package handler

import (
	"encoding/json"
	"math/big"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/middleware"
	"inventory-app/server-go/internal/service"
)

// logMutation records a mutation on the activity log, attributed to the caller
// on the request context.
//
// Master-data handlers (items, vendors, warehouses, branches, divisions, users,
// invoice templates, accounts) all need the same four lines around every write;
// this collapses them to one call. Handlers that mutate inside a transaction
// should call service.LogActivity with their qtx directly instead, so the log
// row commits or rolls back with the change it describes.
func logMutation(r *http.Request, q *db.Queries, action, entityType string, entityID uuid.UUID, description string) {
	ctx := r.Context()
	_ = service.LogActivity(ctx, q, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      action,
		EntityType:  entityType,
		EntityID:    entityID,
		Description: description,
	})
}

// changeClause renders a list of field changes as a trailing ` — a; b` clause,
// or "" when nothing changed (so the caller's sentence stays well-formed).
func changeClause(changes []string) string {
	if len(changes) == 0 {
		return ""
	}
	return " — " + strings.Join(changes, "; ")
}

func respondJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func respondError(w http.ResponseWriter, status int, msg string) {
	respondJSON(w, status, map[string]string{"error": msg})
}

func parseUUID(s string) (uuid.UUID, error) {
	return uuid.Parse(s)
}

func parseBody(r *http.Request, v any) error {
	return json.NewDecoder(r.Body).Decode(v)
}

// numericToFloat64 converts a pgtype.Numeric to float64.
func numericToFloat64(n pgtype.Numeric) float64 {
	if !n.Valid || n.NaN || n.Int == nil {
		return 0
	}
	f, _ := new(big.Float).SetInt(n.Int).Float64()
	if n.Exp > 0 {
		for i := int32(0); i < n.Exp; i++ {
			f *= 10
		}
	} else if n.Exp < 0 {
		for i := n.Exp; i < 0; i++ {
			f /= 10
		}
	}
	return f
}

// floatToNumeric converts a float64 to pgtype.Numeric.
// pgtype.Numeric.Scan only accepts string/[]byte in pgx/v5; passing a float64
// silently produces an invalid (NULL) Numeric. Use this helper everywhere a
// float64 needs to be stored in a NUMERIC column.
func floatToNumeric(f float64) pgtype.Numeric {
	var n pgtype.Numeric
	_ = n.Scan(strconv.FormatFloat(f, 'f', 10, 64))
	return n
}

// anyNumericToFloat64 handles the interface{} returned by sqlc aggregate queries.
func anyNumericToFloat64(v interface{}) float64 {
	switch n := v.(type) {
	case pgtype.Numeric:
		return numericToFloat64(n)
	case float64:
		return n
	case float32:
		return float64(n)
	case int64:
		return float64(n)
	}
	return 0
}
