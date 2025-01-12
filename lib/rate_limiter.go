package lib

import (
	"context"
	"time"
)

type RateLimiter interface {
	Allow(ctx context.Context, key string) (bool, int, time.Duration, error)
	Reset(ctx context.Context, key string) error
}
