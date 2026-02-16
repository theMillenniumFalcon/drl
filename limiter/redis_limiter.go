package limiter

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type RedisLimiter struct {
	client        *redis.Client
	keyPrefix     string
	slidingScript *redis.Script
	resetSc       *redis.Script
}

type RedisOptions struct {
	Addr      string
	Password  string
	DB        int
	KeyPrefix string
}

func NewRedisLimiter(opts RedisOptions) (*RedisLimiter, error) {
	client := redis.NewClient(&redis.Options{
		Addr:         opts.Addr,
		Password:     opts.Password,
		DB:           opts.DB,
		PoolSize:     10,
		MinIdleConns: 2,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis ping failed: %w", err)
	}

	return &RedisLimiter{
		client:        client,
		keyPrefix:     opts.KeyPrefix,
		slidingScript: redis.NewScript(slidingWindowScript),
		resetSc:       redis.NewScript(resetScript),
	}, nil
}

func (r *RedisLimiter) Allow(ctx context.Context, key string, rule Rule) (*Result, error) {
	vals, err := r.slidingScript.Run(
		ctx, r.client,
		[]string{r.buildKey(key, rule.Name)},
		rule.Limit,
		int64(rule.Window.Seconds()),
	).Int64Slice()
	if err != nil {
		return nil, fmt.Errorf("rate limiter script error: %w", err)
	}

	current := int(vals[0])
	ttlSecs := vals[1]
	remaining := rule.Limit - current
	if remaining < 0 {
		remaining = 0
	}

	result := &Result{
		Allowed:   current <= rule.Limit,
		Limit:     rule.Limit,
		Remaining: remaining,
		ResetAt:   time.Now().Add(time.Duration(ttlSecs) * time.Second),
	}
	if !result.Allowed {
		result.RetryAfter = time.Duration(ttlSecs) * time.Second
	}
	return result, nil
}

func (r *RedisLimiter) Reset(ctx context.Context, key string) error {
	pattern := fmt.Sprintf("%s:%s:*", r.keyPrefix, key)
	var cursor uint64
	for {
		keys, nextCursor, err := r.client.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return fmt.Errorf("scan failed: %w", err)
		}
		if len(keys) > 0 {
			if err := r.client.Del(ctx, keys...).Err(); err != nil {
				return fmt.Errorf("del failed: %w", err)
			}
		}
		if nextCursor == 0 {
			break
		}
		cursor = nextCursor
	}
	return nil
}

func (r *RedisLimiter) Close() error {
	return r.client.Close()
}

// buildKey format: "<prefix>:<key>:<ruleName>"
func (r *RedisLimiter) buildKey(key, ruleName string) string {
	return fmt.Sprintf("%s:%s:%s", r.keyPrefix, key, ruleName)
}
