package middleware

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"golang.org/x/time/rate"

	"inventory-app/server-go/internal/db"
)

// Device API key hashing.
//
// We store ONLY the SHA-256 hex digest of a device's API key (never the raw key).
// SHA-256 (rather than bcrypt) is chosen deliberately so the hash can be indexed
// and looked up directly: the middleware hashes the presented key and queries
// attendance_devices by api_key_hash. The raw key is returned exactly once at
// device-creation time and never persisted.

// HashDeviceKey returns the lowercase SHA-256 hex digest of a device API key.
func HashDeviceKey(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

type deviceContextKey string

const deviceKey deviceContextKey = "attendanceDevice"

// DeviceContext carries the authenticated device + its branch into handlers.
type DeviceContext struct {
	DeviceID pgtype.UUID
	BranchID pgtype.UUID
	Name     string
}

// DeviceFromCtx returns the authenticated device, if any.
func DeviceFromCtx(ctx context.Context) (*DeviceContext, bool) {
	d, ok := ctx.Value(deviceKey).(*DeviceContext)
	return d, ok
}

// deviceStore is a dedicated rate limiter for the unauthenticated device API.
// Keyed by device-key-hash so a misbehaving device can't exhaust others.
var deviceStore = newLimiterStore(rate.Every(time.Minute/120), 120, 5*time.Minute)

// DeviceAuth authenticates requests via the X-Device-Key header. It hashes the
// presented key with the same SHA-256 function used at creation, looks up an
// active device by that hash, and attaches it (plus its branch) to the context.
// Requests are rate-limited per device key.
func DeviceAuth(queries *db.Queries) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw := r.Header.Get("X-Device-Key")
			if raw == "" {
				writeAuthError(w, http.StatusUnauthorized, "kunci perangkat tidak ditemukan")
				return
			}

			hash := HashDeviceKey(raw)

			// Rate limit keyed by the key hash.
			if !deviceStore.get(hash).Allow() {
				writeAuthError(w, http.StatusTooManyRequests, "terlalu banyak permintaan dari perangkat ini")
				return
			}

			device, err := queries.GetActiveDeviceByKeyHash(r.Context(), hash)
			if err != nil || device == nil {
				writeAuthError(w, http.StatusUnauthorized, "kunci perangkat tidak valid")
				return
			}

			dc := &DeviceContext{
				DeviceID: device.ID,
				BranchID: device.BranchID,
				Name:     device.Name,
			}
			ctx := context.WithValue(r.Context(), deviceKey, dc)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
