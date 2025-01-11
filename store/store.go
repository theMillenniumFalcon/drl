package store

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type RedisStore struct {
	client redis.UniversalClient
	prefix string
}

type Options struct {
	Addresses []string
	KeyPrefix string
}

func NewStore(opts Options) (*RedisStore, error) {
	var client redis.UniversalClient

	client = redis.NewClient(&redis.Options{
		Addr: opts.Addresses[0],
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	return &RedisStore{
		client: client,
		prefix: opts.KeyPrefix,
	}, nil
}
