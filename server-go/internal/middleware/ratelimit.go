package middleware

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type visitorLimiter struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

type limiterStore struct {
	mu       sync.Mutex
	visitors map[string]*visitorLimiter
	r        rate.Limit
	burst    int
	ttl      time.Duration
}

func newLimiterStore(r rate.Limit, burst int, ttl time.Duration) *limiterStore {
	s := &limiterStore{
		visitors: make(map[string]*visitorLimiter),
		r:        r,
		burst:    burst,
		ttl:      ttl,
	}
	go s.cleanup()
	return s
}

func (s *limiterStore) get(key string) *rate.Limiter {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.visitors[key]
	if !ok {
		l := rate.NewLimiter(s.r, s.burst)
		s.visitors[key] = &visitorLimiter{limiter: l, lastSeen: time.Now()}
		return l
	}
	v.lastSeen = time.Now()
	return v.limiter
}

func (s *limiterStore) cleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		s.mu.Lock()
		for key, v := range s.visitors {
			if time.Since(v.lastSeen) > s.ttl {
				delete(s.visitors, key)
			}
		}
		s.mu.Unlock()
	}
}

var (
	loginStore = newLimiterStore(rate.Every(15*time.Minute/10), 10, 15*time.Minute)
	apiStore   = newLimiterStore(rate.Every(time.Minute/300), 300, 5*time.Minute)
)

func clientIP(r *http.Request) string {
	if ip := r.Header.Get("X-Forwarded-For"); ip != "" {
		return strings.Split(ip, ",")[0]
	}
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return ip
	}
	addr := r.RemoteAddr
	if i := strings.LastIndex(addr, ":"); i != -1 {
		return addr[:i]
	}
	return addr
}

func LoginRateLimit() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !loginStore.get(clientIP(r)).Allow() {
				writeAuthError(w, http.StatusTooManyRequests, "Terlalu banyak percobaan login. Coba lagi sebentar lagi.")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func APIRateLimit() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := clientIP(r)
			if claims, ok := ClaimsFromCtx(r.Context()); ok {
				key = claims.UserID.String()
			}
			if !apiStore.get(key).Allow() {
				writeAuthError(w, http.StatusTooManyRequests, "Terlalu banyak permintaan. Coba lagi sebentar lagi.")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
