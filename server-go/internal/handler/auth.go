package handler

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"golang.org/x/crypto/bcrypt"

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/middleware"
)

const (
	accessTokenTTL  = 8 * time.Hour
	refreshTokenTTL = 72 * time.Hour

	// The refresh token travels only as an httpOnly cookie, so page scripts
	// (and therefore an XSS bug) can never read it. SameSite=Strict keeps the
	// browser from attaching it to requests that start on another site, and the
	// path keeps it off every request except the auth endpoints.
	refreshCookieName = "refresh_token"
	refreshCookiePath = "/api/auth"
)

type AuthHandler struct {
	queries   *db.Queries
	jwtSecret []byte
}

func NewAuthHandler(queries *db.Queries, jwtSecret string) *AuthHandler {
	return &AuthHandler{queries: queries, jwtSecret: []byte(jwtSecret)}
}

func (h *AuthHandler) issueToken(userID uuid.UUID, username, role, tokenType string, expiry time.Duration) (string, error) {
	now := time.Now()
	claims := &middleware.Claims{
		UserID:    userID,
		Username:  username,
		Role:      role,
		TokenType: tokenType,
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        uuid.New().String(),
			ExpiresAt: jwt.NewNumericDate(now.Add(expiry)),
			IssuedAt:  jwt.NewNumericDate(now),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(h.jwtSecret)
}

// parseToken verifies a token's signature and expiry. It does not check the
// blocklist or the token type; callers decide which type they accept.
func (h *AuthHandler) parseToken(raw string) (*middleware.Claims, error) {
	claims := &middleware.Claims{}
	token, err := jwt.ParseWithClaims(raw, claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, jwt.ErrSignatureInvalid
		}
		return h.jwtSecret, nil
	})
	if err != nil || !token.Valid {
		return nil, errTokenInvalid
	}
	return claims, nil
}

func (h *AuthHandler) blocklist(ctx context.Context, claims *middleware.Claims) error {
	if claims.ExpiresAt == nil {
		return nil
	}
	return h.queries.InsertTokenBlocklist(ctx, &db.InsertTokenBlocklistParams{
		Jti:       claims.ID,
		ExpiresAt: pgtype.Timestamptz{Time: claims.ExpiresAt.Time, Valid: true},
	})
}

// isHTTPS reports whether the browser reached us over TLS, directly or through
// nginx (which sets X-Forwarded-Proto). Local development runs over plain HTTP,
// where a Secure cookie would never be sent back.
func isHTTPS(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func setRefreshCookie(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     refreshCookieName,
		Value:    token,
		Path:     refreshCookiePath,
		MaxAge:   int(refreshTokenTTL / time.Second),
		HttpOnly: true,
		Secure:   isHTTPS(r),
		SameSite: http.SameSiteStrictMode,
	})
}

func clearRefreshCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     refreshCookieName,
		Path:     refreshCookiePath,
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   isHTTPS(r),
		SameSite: http.SameSiteStrictMode,
	})
}

// issueSession writes a fresh access token (in the body) and refresh token (in
// the cookie) for the user.
func (h *AuthHandler) issueSession(w http.ResponseWriter, r *http.Request, id pgtype.UUID, username, role string) {
	userID := uuid.UUID(id.Bytes)
	accessToken, err := h.issueToken(userID, username, role, "", accessTokenTTL)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membuat token")
		return
	}
	refreshToken, err := h.issueToken(userID, username, role, middleware.TokenTypeRefresh, refreshTokenTTL)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membuat refresh token")
		return
	}
	setRefreshCookie(w, r, refreshToken)
	respondJSON(w, http.StatusOK, map[string]any{
		"token": accessToken,
		"user":  h.userInfo(r.Context(), id, username, role),
	})
}

// userInfo is the user object the client keeps in localStorage. A branch
// operator's screens are labelled with, and filtered to, their branch; this is
// for display only — the server re-reads the scope on every request
// (middleware.RestrictBranchOperator).
func (h *AuthHandler) userInfo(ctx context.Context, id pgtype.UUID, username, role string) map[string]any {
	info := map[string]any{
		"id":       uuid.UUID(id.Bytes),
		"username": username,
		"role":     role,
	}
	if role == middleware.RoleBranchOperator {
		if bid, err := h.queries.GetUserBranchScope(ctx, id); err == nil && bid.Valid {
			info["branch_id"] = bid
			if b, err := h.queries.GetBranchByID(ctx, bid); err == nil {
				info["branch_name"] = b.Name
			}
		}
	}
	return info
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	username := strings.ToLower(strings.TrimSpace(body.Username))
	if username == "" || body.Password == "" {
		respondError(w, http.StatusBadRequest, "username dan password wajib diisi")
		return
	}

	user, err := h.queries.GetUserByUsername(r.Context(), username)
	if err != nil {
		respondError(w, http.StatusUnauthorized, "kredensial tidak valid")
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(body.Password)); err != nil {
		respondError(w, http.StatusUnauthorized, "kredensial tidak valid")
		return
	}

	h.issueSession(w, r, user.ID, user.Username, user.Role)
}

// Logout — POST /api/auth/logout
//
// Public on purpose: it must still work once the access token has expired, so
// it revokes whatever valid tokens it is given (the Bearer access token and/or
// the refresh cookie) and always clears the cookie.
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	if !middleware.SameSiteOrigin(r) {
		respondError(w, http.StatusForbidden, "asal permintaan tidak diizinkan")
		return
	}
	ctx := r.Context()
	var presented []*middleware.Claims
	if header := r.Header.Get("Authorization"); strings.HasPrefix(header, "Bearer ") {
		if c, err := h.parseToken(strings.TrimPrefix(header, "Bearer ")); err == nil {
			presented = append(presented, c)
		}
	}
	if cookie, err := r.Cookie(refreshCookieName); err == nil {
		if c, err := h.parseToken(cookie.Value); err == nil {
			presented = append(presented, c)
		}
	}

	clearRefreshCookie(w, r)
	for _, c := range presented {
		if err := h.blocklist(ctx, c); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal mencabut token")
			return
		}
	}
	respondJSON(w, http.StatusOK, map[string]string{"message": "berhasil logout"})
}

// Refresh — POST /api/auth/refresh
//
// Exchanges the refresh cookie for a new access token and rotates the refresh
// token. The presented token is revoked before the new one is issued, and a
// failure to revoke refuses the refresh: a rotation that leaves the old token
// usable is not a rotation.
func (h *AuthHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	if !middleware.SameSiteOrigin(r) {
		respondError(w, http.StatusForbidden, "asal permintaan tidak diizinkan")
		return
	}
	ctx := r.Context()
	cookie, err := r.Cookie(refreshCookieName)
	if err != nil || cookie.Value == "" {
		respondError(w, http.StatusUnauthorized, "sesi berakhir, silakan masuk kembali")
		return
	}

	claims, err := h.parseToken(cookie.Value)
	if err != nil || claims.TokenType != middleware.TokenTypeRefresh {
		clearRefreshCookie(w, r)
		respondError(w, http.StatusUnauthorized, "token tidak valid atau sudah kedaluwarsa")
		return
	}

	blocked, err := h.queries.IsTokenBlocked(ctx, claims.ID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memeriksa token")
		return
	}
	if blocked {
		clearRefreshCookie(w, r)
		respondError(w, http.StatusUnauthorized, "token telah dicabut")
		return
	}

	// Re-read the user so a role change or a deleted account takes effect at
	// the next refresh instead of riding on the old token's claims.
	user, err := h.queries.GetUserByID(ctx, pgtype.UUID{Bytes: claims.UserID, Valid: true})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			clearRefreshCookie(w, r)
			respondError(w, http.StatusUnauthorized, "pengguna tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal memuat pengguna")
		return
	}

	if err := h.blocklist(ctx, claims); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mencabut token lama")
		return
	}
	h.issueSession(w, r, user.ID, user.Username, user.Role)
}

var errTokenInvalid = errors.New("token tidak valid")
