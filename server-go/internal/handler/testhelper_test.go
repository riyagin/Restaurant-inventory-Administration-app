package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	"inventory-app/server-go/internal/middleware"
)

// createTestUser inserts a minimal user row and returns its UUID.
// The row is deleted at the end of the test via t.Cleanup.
func createTestUser(t *testing.T, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	userID := uuid.New()
	hash, err := bcrypt.GenerateFromPassword([]byte("test"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("bcrypt: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO users (id, username, password_hash, role)
		 VALUES ($1, $2, $3, 'admin')`,
		userID, "testuser-"+userID.String()[:8], string(hash)); err != nil {
		t.Fatalf("create test user: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, userID)
	})
	return userID
}

// testClaims builds a Claims value for the given user ID, suitable for
// injecting into a request context via middleware.ContextWithClaims.
func testClaims(userID uuid.UUID) *middleware.Claims {
	return &middleware.Claims{
		UserID:   userID,
		Username: "test-user",
		Role:     "admin",
	}
}

// postJSON marshals body as JSON and calls h with a POST request in ctx.
func postJSON(t *testing.T, h http.HandlerFunc, ctx context.Context, body any) *httptest.ResponseRecorder {
	t.Helper()
	bs, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(bs))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()
	h(rr, req)
	return rr
}

// getBalance reads the current balance of one account.
func getBalance(t *testing.T, pool *pgxpool.Pool, id uuid.UUID) int64 {
	t.Helper()
	var bal int64
	pool.QueryRow(context.Background(),
		`SELECT balance FROM accounts WHERE id = $1`, id).Scan(&bal)
	return bal
}
