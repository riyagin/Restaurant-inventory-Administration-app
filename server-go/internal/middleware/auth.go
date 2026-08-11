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

// Roles recognised by the application. Every role check below is expressed in
// terms of these, so adding a role means touching this file and nothing else.
const (
	RoleSuperuser    = "superuser"
	RoleAdmin        = "admin"
	RoleManager      = "manager"
	RoleStaff        = "staff"
	RoleHR           = "hr"
	RoleStoreManager = "store_manager"
)

// hasRole reports whether the caller may pass a gate.
//
// superuser is deliberately handled here rather than being listed in each gate:
// it holds *every* capability, including approvals (RequireManager), and a role
// defined as "all of them" must not be able to drift out of a gate someone adds
// later. Listing it explicitly would mean remembering it every time — this way
// forgetting is impossible.
func hasRole(ctx context.Context, allowed ...string) bool {
	role := RoleFromCtx(ctx)
	if role == RoleSuperuser {
		return true
	}
	for _, a := range allowed {
		if role == a {
			return true
		}
	}
	return false
}

// HasRole is the exported form, for the handful of handlers that check a role
// inline instead of behind a gate. It carries the same superuser bypass.
func HasRole(ctx context.Context, allowed ...string) bool {
	return hasRole(ctx, allowed...)
}

func gate(allowed ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !hasRole(r.Context(), allowed...) {
				writeAuthError(w, http.StatusForbidden, "akses ditolak")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequireAdmin allows admin and manager (manager = admin permissions +
// approval responsibilities). Kept named "RequireAdmin" for backward
// compatibility with existing route wiring.
func RequireAdmin(next http.Handler) http.Handler {
	return gate(RoleAdmin, RoleManager)(next)
}

// RequireCoreAccess guards the operational (non-HR) surface for the handful of
// actions that are not simply open to every authenticated user. Staff is
// included — it runs the business day to day and has the same reach as admin
// outside reporting — while the HR-only role is not.
func RequireCoreAccess(next http.Handler) http.Handler {
	return gate(RoleAdmin, RoleManager, RoleStaff)(next)
}

// RequireHRAccess guards the HR module. Everyone who works in the app has it —
// admin, manager, staff and the HR-only role — because HR data entry is shared
// work. What stays restricted is *approving* (RequireManager), not viewing or
// recording.
func RequireHRAccess(next http.Handler) http.Handler {
	return gate(RoleAdmin, RoleManager, RoleStaff, RoleHR)(next)
}

// RequireReportsAccess guards the reporting surface (financial reports, expense
// reports, and the activity log). Staff runs the day-to-day operation but does
// not get the org-wide financial picture, and the HR role sees nothing outside
// HR at all.
func RequireReportsAccess(next http.Handler) http.Handler {
	return gate(RoleAdmin, RoleManager)(next)
}

// hrRoleAllowedPrefixes are the non-HR endpoints the HR-only role still needs:
// its own session, and the small amount of shared master data the HR screens
// read (branches for employee placement, accounts for a kasbon's fund source).
var hrRoleAllowedPrefixes = []string{
	"/api/hr/",
	"/api/auth/",
	"/api/branches",
	"/api/divisions",
	"/api/accounts",
	// The notification feed is assembled per role and hands the HR role its own
	// queues (expiring contracts, unreviewed payroll). The operational task
	// endpoints under /api/tasks/ stay closed to it.
	"/api/notifications",
}

// RestrictHRRole confines the HR-only role to the HR module.
//
// Most non-HR routes are open to every authenticated user, so scoping this role
// by adding middleware to each of them would mean touching every route and
// getting it wrong the next time one is added. Instead this sits once at the top
// of the protected group and denies anything outside the allowlist above —
// closed by default, which is the right direction to fail in.
func RestrictHRRole(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if RoleFromCtx(r.Context()) != RoleHR {
			next.ServeHTTP(w, r)
			return
		}
		for _, p := range hrRoleAllowedPrefixes {
			if strings.HasPrefix(r.URL.Path, p) {
				next.ServeHTTP(w, r)
				return
			}
		}
		writeAuthError(w, http.StatusForbidden, "akses ditolak")
	})
}

// RequireAdminOrManager allows admin or manager. Semantically identical to
// RequireAdmin today, exposed under a clearer name.
func RequireAdminOrManager(next http.Handler) http.Handler {
	return gate(RoleAdmin, RoleManager)(next)
}

// RequireAttendanceAccess allows everyone with HR access plus store_manager.
//
// store_manager is a limited role whose web-app access is scoped to HR
// attendance record entry/correction (helping visiting employees punch in/out).
// It is rejected by every other module's middleware (RequireAdmin /
// RequireHRAccess / RequireManager), so this is the only place it is granted
// access.
func RequireAttendanceAccess(next http.Handler) http.Handler {
	return gate(RoleAdmin, RoleManager, RoleStaff, RoleHR, RoleStoreManager)(next)
}

// RequireManager allows only the manager role (approval-only actions), plus
// superuser via the bypass in hasRole. Staff and the HR role can prepare a
// kasbon, a leave request or an overtime claim, but not approve it — and admin
// cannot either, which is the reason superuser exists.
func RequireManager(next http.Handler) http.Handler {
	return gate(RoleManager)(next)
}

func writeAuthError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
