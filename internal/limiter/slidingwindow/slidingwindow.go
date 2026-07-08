package slidingwindow

import (
	"context"
	_ "embed"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

//go:embed sliding_window.lua
var luaScript string

type SlidingWindow struct {
	client     *goredis.Client
	script     *goredis.Script
	windowSize int // seconds
	limit      int // max cost units per window
}

func New(client *goredis.Client, windowSize, limit int) *SlidingWindow {
	return &SlidingWindow{
		client:     client,
		script:     goredis.NewScript(luaScript),
		windowSize: windowSize,
		limit:      limit,
	}
}

func (sw *SlidingWindow) Allow(ctx context.Context, key string, cost int) (bool, error) {
	return sw.allow(ctx, key, cost, time.Now().UnixMilli())
}

func (sw *SlidingWindow) allow(ctx context.Context, key string, cost int, nowMs int64) (bool, error) {
	result, err := sw.script.Run(ctx, sw.client, []string{key},
		sw.windowSize,
		sw.limit,
		cost,
		nowMs,
	).Int()
	if err != nil {
		return false, err
	}
	return result == 1, nil
}
