package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"

	"github.com/themillenniumfalcon/drl/config"
	"github.com/themillenniumfalcon/drl/handler"
	"github.com/themillenniumfalcon/drl/limiter"
	"github.com/themillenniumfalcon/drl/middleware"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, nil)))

	cfg := config.Load()

	lim, err := limiter.NewRedisLimiter(limiter.RedisOptions{
		Addr:      cfg.Redis.Addr,
		Password:  cfg.Redis.Password,
		DB:        cfg.Redis.DB,
		KeyPrefix: cfg.Limits.KeyPrefix,
	})
	if err != nil {
		slog.Error("failed to connect to Redis", "error", err)
		os.Exit(1)
	}
	defer lim.Close()

	slog.Info("connected to Redis", "addr", cfg.Redis.Addr)

	globalRule := limiter.Rule{
		Name:   "global",
		Limit:  cfg.Limits.DefaultLimit,
		Window: cfg.Limits.DefaultWindow,
	}
	strictRule := limiter.Rule{
		Name:   "strict",
		Limit:  3,
		Window: time.Minute,
	}

	r := chi.NewRouter()
	r.Use(chiMiddleware.RequestID)
	r.Use(chiMiddleware.RealIP)
	r.Use(chiMiddleware.Logger)
	r.Use(chiMiddleware.Recoverer)

	r.Get("/health", handler.HealthHandler)

	r.Group(func(r chi.Router) {
		r.Use(middleware.RateLimiter(lim, globalRule, middleware.ByIP))
		r.Get("/api/status", handler.StatusHandler)
		r.Get("/api/info", handler.InfoHandler(globalRule))
	})

	r.Group(func(r chi.Router) {
		r.Use(middleware.RateLimiter(lim, strictRule, middleware.ByAPIKey))
		r.Post("/api/sensitive", func(w http.ResponseWriter, req *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"message":"ok"}`))
		})
	})

	r.Post("/admin/reset", handler.ResetHandler(lim))

	srv := &http.Server{
		Addr:         ":" + cfg.Server.Port,
		Handler:      r,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		slog.Info("server starting", "port", cfg.Server.Port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	<-quit
	slog.Info("shutting down...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("forced shutdown", "error", err)
	}
}
