package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const testSecret = "test-secret"

func signed(t *testing.T, tokenType string) string {
	t.Helper()
	claims := &Claims{
		UserID:    uuid.New(),
		Username:  "u",
		Role:      RoleAdmin,
		TokenType: tokenType,
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        uuid.NewString(),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}
	s, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(testSecret))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// A refresh token is rejected before the blocklist lookup, so a nil *db.Queries
// is enough to prove it never authenticates a request.
func TestAuthenticateRejectsRefreshToken(t *testing.T) {
	reached := false
	h := Authenticate(nil, testSecret)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true }))

	req := httptest.NewRequest(http.MethodGet, "/api/items", nil)
	req.Header.Set("Authorization", "Bearer "+signed(t, TokenTypeRefresh))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized || reached {
		t.Fatalf("refresh token as Bearer: status %d, handler reached %v; want 401, false", rec.Code, reached)
	}
}

func TestSameSiteOrigin(t *testing.T) {
	cases := []struct {
		name, host, origin string
		want               bool
	}{
		{"no origin (curl, kiosk)", "api.example.com", "", true},
		{"same origin", "app.example.com", "https://app.example.com", true},
		{"dev server on another port", "192.168.1.5:5000", "http://192.168.1.5:5173", true},
		{"localhost dev", "localhost:5000", "http://localhost:5173", true},
		{"case-insensitive host", "App.Example.com", "https://app.example.COM", true},
		{"other site", "app.example.com", "https://evil.example.net", false},
		{"lookalike subdomain", "example.com", "https://example.com.evil.net", false},
		{"unparseable origin", "example.com", "://", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/api/auth/refresh", nil)
			r.Host = c.host
			if c.origin != "" {
				r.Header.Set("Origin", c.origin)
			}
			if got := SameSiteOrigin(r); got != c.want {
				t.Errorf("SameSiteOrigin(host=%q, origin=%q) = %v, want %v", c.host, c.origin, got, c.want)
			}
		})
	}
}
