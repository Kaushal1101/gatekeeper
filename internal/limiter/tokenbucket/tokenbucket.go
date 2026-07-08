package tokenbucket

import (
	"context"
	_ "embed"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

//go:embed token_bucket.lua
var luaScript string

type TokenBucket struct {
	client     *goredis.Client
	script     *goredis.Script
	capacity   int
	refillRate float64 // tokens per second
}

func New(client *goredis.Client, capacity int, refillRate float64) *TokenBucket {
	return &TokenBucket{
		client:     client,
		script:     goredis.NewScript(luaScript),
		capacity:   capacity,
		refillRate: refillRate,
	}
}

func (tb *TokenBucket) Allow(ctx context.Context, key string, cost int) (bool, error) {
	return tb.allow(ctx, key, cost, time.Now().UnixMilli())
}

func (tb *TokenBucket) allow(ctx context.Context, key string, cost int, nowMs int64) (bool, error) {
	result, err := tb.script.Run(ctx, tb.client, []string{key},
		tb.capacity,
		tb.refillRate,
		cost,
		nowMs,
	).Int()
	if err != nil {
		return false, err
	}
	return result == 1, nil
}
