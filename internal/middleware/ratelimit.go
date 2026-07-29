package middleware

import (
	"log"
	"net"
	"net/http"

	"github.com/kaushaljayapragash/gatekeeper/internal/policy"
)

// RateLimit returns middleware that enforces the limits returned by matcher.
// /health and other paths not in the policy config are handled by the matcher's
// configured default_action (allow or deny).
func RateLimit(matcher *policy.Matcher) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			apiKey := r.Header.Get("X-API-Key")
			ip := realIP(r)

			checks, defaultAllow := matcher.Match(r.URL.Path, apiKey, ip)
			if checks == nil {
				if !defaultAllow {
					writeJSON(w, http.StatusForbidden, `{"error":"no policy for this path"}`)
					return
				}
				next.ServeHTTP(w, r)
				return
			}

			for _, c := range checks {
				ok, err := c.Limiter.Allow(r.Context(), c.Key, c.Cost)
				if err != nil {
					log.Printf("rate limiter error key=%s: %v", c.Key, err)
					if !matcher.OnErrorAllow() {
						writeJSON(w, http.StatusInternalServerError, `{"error":"internal server error"}`)
						return
					}
					// fail-open: Redis is down but configured to pass through
					next.ServeHTTP(w, r)
					return
				}
				if !ok {
					writeJSON(w, http.StatusTooManyRequests, `{"error":"rate limit exceeded"}`)
					return
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}

// realIP prefers the X-Real-IP header set by nginx over the raw TCP remote address.
func realIP(r *http.Request) string {
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return ip
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func writeJSON(w http.ResponseWriter, code int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	w.Write([]byte(body))
}
