package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/themillenniumfalcon/drl/limiter"
	"github.com/themillenniumfalcon/drl/middleware"
)

func HealthHandler(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func StatusHandler(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":    "ok",
		"timestamp": time.Now().UTC(),
		"message":   "Check X-RateLimit-* headers.",
	})
}

func InfoHandler(rule limiter.Rule) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"your_key": middleware.ByIP(r),
			"rule":     rule.Name,
			"limit":    rule.Limit,
			"window":   rule.Window.String(),
		})
	}
}

func ResetHandler(lim limiter.Limiter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Key string `json:"key"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Key == "" {
			http.Error(w, "'key' field required", http.StatusBadRequest)
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()

		if err := lim.Reset(ctx, body.Key); err != nil {
			http.Error(w, "failed to reset key", http.StatusInternalServerError)
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{
			"reset":  body.Key,
			"status": "cleared",
		})
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
