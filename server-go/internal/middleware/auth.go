package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"inventory-app/server-go/internal/db"
)

type Claims struct {
	UserID   uuid.UUID `json:"uid"`
	Username string    `json:"username"`
	Role     string    `json:"role"`
	jwt.RegisteredClaims
}

type contextKey string

const userClaimsKey contextKey = "userClaims"

func UserIDFromCtx(ctx context.Context) uuid.UUID {
	if c, ok := ctx.Value(userClaimsKey).(*Claims); ok {
		return c.UserID
	}
	return uuid.Nil
}

func UsernameFromCtx(ctx context.Context) string {
	if c, ok := ctx.Value(userClaimsKey).(*Claims); ok {
		return c.Username
	}
	return ""
}

func RoleFromCtx(ctx context.Context) string {
	if c, ok := ctx.Value(userClaimsKey).(*Claims); ok {
		return c.Role
	}
	return ""
}

func ClaimsFromCtx(ctx context.Context) (*Claims, bool) {
	c, ok := ctx.Value(userClaimsKey).(*Claims)
	return c, ok
}

func Authenticate(queries *db.Queries, jwtSecret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := r.Header.Get("Authorization")
			if !strings.HasPrefix(header, "Bearer ") {
				writeAuthError(w, http.StatusUnauthorized, "token tidak ditemukan")
				return
			}
			tokenStr := header[7:]

			claims := &Claims{}
			token, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (any, error) {
				if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
					return nil, jwt.ErrSignatureInvalid
				}
				return []byte(jwtSecret), nil
			})
			if err != nil || !token.Valid {
				writeAuthError(w, http.StatusUnauthorized, "token tidak valid atau sudah kedaluwarsa")
				return
			}

			jti := claims.RegisteredClaims.ID
			blocked, err := queries.IsTokenBlocked(r.Context(), jti)
			if err != nil || blocked {
				writeAuthError(w, http.StatusUnauthorized, "token telah dicabut")
				return
			}

			ctx := context.WithValue(r.Context(), userClaimsKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireAdmin allows admin and manager (manager = admin permissions +
// approval responsibilities). Kept named "RequireAdmin" for backward
// compatibility with existing route wiring.
func RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		role := RoleFromCtx(r.Context())
		if role != "admin" && role != "manager" {
			writeAuthError(w, http.StatusForbidden, "akses ditolak")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireAdminOrManager allows admin or manager. Semantically identical to
// RequireAdmin today, exposed under a clearer name for HR routes.
func RequireAdminOrManager(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		role := RoleFromCtx(r.Context())
		if role != "admin" && role != "manager" {
			writeAuthError(w, http.StatusForbidden, "akses ditolak")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireManager allows only the manager role (approval-only actions).
func RequireManager(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if RoleFromCtx(r.Context()) != "manager" {
			writeAuthError(w, http.StatusForbidden, "akses ditolak")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeAuthError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
