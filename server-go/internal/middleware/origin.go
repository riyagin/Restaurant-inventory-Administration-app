package middleware

import (
	"net"
	"net/http"
	"net/url"
	"strings"
)

// SameSiteOrigin reports whether a browser request came from a page on the
// API's own host (any port: the dev server on :5173 calls the API on :5000).
// Requests without an Origin header — curl, the kiosk app — pass; it is the
// browser's cross-site requests this guards against, on top of SameSite=Strict.
func SameSiteOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return strings.EqualFold(u.Hostname(), host)
}
