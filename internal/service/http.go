package service

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/themillenniumfalcon/drl/lib"
)

type HTTPService struct {
	limiter lib.RateLimiter
	router  *chi.Mux
}

type AllowResponse struct {
	Allowed    bool    `json:"allowed"`
	Remaining  int     `json:"remaining"`
	ResetAfter float64 `json:"reset_after_sec"`
	RetryAfter float64 `json:"retry_after_sec,omitempty"`
}

func NewHTTPService(limiter lib.RateLimiter) *HTTPService {
	s := &HTTPService{
		limiter: limiter,
		router:  chi.NewRouter(),
	}

	s.setupRoutes()
	return s
}

func (s *HTTPService) setupRoutes() {
	s.router.Route("/v1", func(r chi.Router) {
		r.Post("/allow/{key}", s.handleAllow)
	})
}

func (s *HTTPService) handleAllow(w http.ResponseWriter, r *http.Request) {
	key := chi.URLParam(r, "key")
	namespace := r.URL.Query().Get("namespace")

	fullKey := s.formatKey(namespace, key)

	allowed, remaining, resetAfter, err := s.limiter.Allow(r.Context(), fullKey)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	resetAfterSec := resetAfter.Seconds()
	resp := AllowResponse{
		Allowed:    allowed,
		Remaining:  remaining,
		ResetAfter: resetAfterSec,
	}

	if !allowed {
		resp.RetryAfter = resetAfterSec
		w.WriteHeader(http.StatusTooManyRequests)
		w.Header().Set("Retry-After", strconv.Itoa(int(resetAfterSec)))
	}

	w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(remaining))
	w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(time.Now().Add(resetAfter).Unix(), 10))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (s *HTTPService) formatKey(namespace, key string) string {
	if namespace == "" {
		return key
	}
	return namespace + ":" + key
}
