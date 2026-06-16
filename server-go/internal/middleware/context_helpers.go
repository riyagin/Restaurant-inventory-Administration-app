package middleware

import "context"

// ContextWithClaims returns a new context carrying the given Claims.
// Used in tests to inject auth identity without going through JWT validation.
func ContextWithClaims(ctx context.Context, c *Claims) context.Context {
	return context.WithValue(ctx, userClaimsKey, c)
}
