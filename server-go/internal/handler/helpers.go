package handler

import (
	"encoding/json"
	"math/big"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

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
