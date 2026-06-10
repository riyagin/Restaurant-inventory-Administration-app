package handler

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"golang.org/x/crypto/bcrypt"

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/middleware"
)

type AuthHandler struct {
	queries   *db.Queries
	jwtSecret []byte
}

func NewAuthHandler(queries *db.Queries, jwtSecret string) *AuthHandler {
	return &AuthHandler{queries: queries, jwtSecret: []byte(jwtSecret)}
}

func (h *AuthHandler) issueToken(userID uuid.UUID, username, role string, expiry time.Duration) (string, string, error) {
	jti := uuid.New().String()
	claims := &middleware.Claims{
		UserID:   userID,
		Username: username,
		Role:     role,
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        jti,
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(expiry)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(h.jwtSecret)
	return token, jti, err
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

	userID := uuid.UUID(user.ID.Bytes)

	accessToken, _, err := h.issueToken(userID, user.Username, user.Role, 8*time.Hour)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membuat token")
		return
	}
	refreshToken, _, err := h.issueToken(userID, user.Username, user.Role, 72*time.Hour)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membuat refresh token")
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"token":        accessToken,
		"refreshToken": refreshToken,
		"user": map[string]any{
			"id":       userID,
			"username": user.Username,
			"role":     user.Role,
		},
	})
}

func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromCtx(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "tidak terautentikasi")
		return
	}

	jti := claims.RegisteredClaims.ID
	expiresAt := claims.RegisteredClaims.ExpiresAt.Time

	_ = h.queries.InsertTokenBlocklist(r.Context(), &db.InsertTokenBlocklistParams{
		Jti:       jti,
		ExpiresAt: pgtype.Timestamptz{Time: expiresAt, Valid: true},
	})

	respondJSON(w, http.StatusOK, map[string]string{"message": "berhasil logout"})
}

func (h *AuthHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	var body struct {
		RefreshToken string `json:"refreshToken"`
	}
	if err := parseBody(r, &body); err != nil || body.RefreshToken == "" {
		respondError(w, http.StatusBadRequest, "refreshToken wajib diisi")
		return
	}

	claims := &middleware.Claims{}
	token, err := jwt.ParseWithClaims(body.RefreshToken, claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, jwt.ErrSignatureInvalid
		}
		return h.jwtSecret, nil
	})
	if err != nil || !token.Valid {
		respondError(w, http.StatusUnauthorized, "token tidak valid atau sudah kedaluwarsa")
		return
	}

	jti := claims.RegisteredClaims.ID
	blocked, err := h.queries.IsTokenBlocked(r.Context(), jti)
	if err != nil || blocked {
		respondError(w, http.StatusUnauthorized, "token telah dicabut")
		return
	}

	// Blocklist the old refresh token
	if expAt := claims.RegisteredClaims.ExpiresAt; expAt != nil {
		_ = h.queries.InsertTokenBlocklist(r.Context(), &db.InsertTokenBlocklistParams{
			Jti:       jti,
			ExpiresAt: pgtype.Timestamptz{Time: expAt.Time, Valid: true},
		})
	}

	newAccess, _, err := h.issueToken(claims.UserID, claims.Username, claims.Role, 8*time.Hour)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membuat token")
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"token": newAccess})
}

var errTokenInvalid = errors.New("token tidak valid")
