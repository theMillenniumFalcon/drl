package middleware

import (
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/themillenniumfalcon/drl/limiter"
)

type KeyExtractorFunc func(r *http.Request) string

func RateLimiter(lim limiter.Limiter, rule limiter.Rule, extractor KeyExtractorFunc) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := extractor(r)
			result, err := lim.Allow(r.Context(), key, rule)
			if err != nil {
				// Fail open: log and allow through rather than blocking on Redis errors.
				slog.Error("rate limiter error", "error", err, "key", key)
				next.ServeHTTP(w, r)
				return
			}

			setRateLimitHeaders(w, result)

			if !result.Allowed {
				w.Header().Set("Retry-After", strconv.Itoa(int(result.RetryAfter.Seconds())))
				http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// ByIP keys on the client IP, respecting X-Real-IP and X-Forwarded-For.
func ByIP(r *http.Request) string {
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return "ip:" + ip
	}
	if ip := r.Header.Get("X-Forwarded-For"); ip != "" {
		return "ip:" + ip
	}
	return "ip:" + r.RemoteAddr
}

// ByAPIKey keys on X-API-Key, falling back to IP.
func ByAPIKey(r *http.Request) string {
	if key := r.Header.Get("X-API-Key"); key != "" {
		return "apikey:" + key
	}
	return ByIP(r)
}

func setRateLimitHeaders(w http.ResponseWriter, result *limiter.Result) {
	w.Header().Set("X-RateLimit-Limit", strconv.Itoa(result.Limit))
	w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(result.Remaining))
	w.Header().Set("X-RateLimit-Reset", fmt.Sprintf("%d", result.ResetAt.Unix()))
	w.Header().Set("X-RateLimit-Reset-Human", result.ResetAt.UTC().Format(time.RFC3339))
}
