package limiter

import (
	"context"
	"time"
)

type Result struct {
	Allowed    bool
	Limit      int
	Remaining  int
	ResetAt    time.Time
	RetryAfter time.Duration
}

type Rule struct {
	Name   string
	Limit  int
	Window time.Duration
}

type Limiter interface {
	Allow(ctx context.Context, key string, rule Rule) (*Result, error)
	Reset(ctx context.Context, key string) error
	Close() error
}
